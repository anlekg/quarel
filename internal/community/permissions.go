package community

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
)

// perm is a permission bit set. Values are stored in the database: never
// renumber an existing permission, only add new ones.
type perm int64

const (
	permViewChannel     perm = 1 << 0  // see a channel and read its history
	permSendMessages    perm = 1 << 1  // post in a text channel
	permManageMessages  perm = 1 << 2  // delete others' messages
	permMentionEveryone perm = 1 << 3  // @everyone, and mention non-mentionable roles
	permCreateInvite    perm = 1 << 4  // create invites
	permManageChannels  perm = 1 << 5  // create, edit, delete channels
	permManageRoles     perm = 1 << 6  // manage roles below one's own, member roles, channel overrides
	permKickMembers     perm = 1 << 7  // kick members below one's own top role
	permBanMembers      perm = 1 << 8  // ban members below one's own top role
	permManageServer    perm = 1 << 9  // server settings, every invite
	permConnect         perm = 1 << 10 // join a voice channel (milestone 4)
	permSpeak           perm = 1 << 11 // speak in a voice channel (milestone 4)
	permAdministrator   perm = 1 << 30 // every permission, ignores channel overrides

	permAll perm = permViewChannel | permSendMessages | permManageMessages | permMentionEveryone |
		permCreateInvite | permManageChannels | permManageRoles | permKickMembers | permBanMembers |
		permManageServer | permConnect | permSpeak | permAdministrator

	// permChannelScoped are the permissions a channel override may allow or deny.
	permChannelScoped perm = permViewChannel | permSendMessages | permManageMessages |
		permMentionEveryone | permManageChannels | permConnect | permSpeak

	// permEveryoneDefault is granted to the @everyone role on a new server
	// (value written in migration 2).
	permEveryoneDefault perm = permViewChannel | permSendMessages | permCreateInvite | permConnect | permSpeak
)

var permNames = []struct {
	p    perm
	name string
}{
	{permViewChannel, "view_channel"},
	{permSendMessages, "send_messages"},
	{permManageMessages, "manage_messages"},
	{permMentionEveryone, "mention_everyone"},
	{permCreateInvite, "create_invite"},
	{permManageChannels, "manage_channels"},
	{permManageRoles, "manage_roles"},
	{permKickMembers, "kick_members"},
	{permBanMembers, "ban_members"},
	{permManageServer, "manage_server"},
	{permConnect, "connect"},
	{permSpeak, "speak"},
	{permAdministrator, "administrator"},
}

// names lists the permissions in p; the API exchanges permissions as names.
func (p perm) names() []string {
	out := []string{}
	for _, pn := range permNames {
		if p&pn.p != 0 {
			out = append(out, pn.name)
		}
	}
	return out
}

func parsePerms(names []string) (perm, error) {
	var p perm
	for _, n := range names {
		found := false
		for _, pn := range permNames {
			if pn.name == n {
				p |= pn.p
				found = true
				break
			}
		}
		if !found {
			return 0, errf(http.StatusBadRequest, "invalid_permission", "unknown permission %q", n)
		}
	}
	return p, nil
}

func missing(p perm) error {
	return errf(http.StatusForbidden, "missing_permissions", "missing permission(s): %s", strings.Join(p.names(), ", "))
}

// --- permission snapshot ---

// permSnapshot is the whole permission state of the server, loaded at once.
// Home servers are small, so this stays cheap and keeps the rules in one place.
type permSnapshot struct {
	owners      map[string]bool
	roles       map[int64]*role
	memberRoles map[string][]int64
	channels    map[int64]*channel
}

func (s *Server) loadPerms(ctx context.Context, q querier) (*permSnapshot, error) {
	ps := &permSnapshot{owners: map[string]bool{}, roles: map[int64]*role{}, memberRoles: map[string][]int64{}, channels: map[int64]*channel{}}
	rows, err := q.QueryContext(ctx, `SELECT id FROM members WHERE is_owner = 1`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ps.owners[id] = true
	}
	rows.Close()

	roles, err := allRoles(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		ps.roles[r.ID] = r
	}
	if ps.memberRoles, err = allMemberRoles(ctx, q); err != nil {
		return nil, err
	}
	chans, err := allChannels(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, c := range chans {
		ps.channels[c.ID] = c
	}
	return ps, nil
}

// base returns the server-level permissions of a member.
func (ps *permSnapshot) base(memberID string) perm {
	if ps.owners[memberID] {
		return permAll
	}
	p := ps.roles[everyoneRoleID].perms
	for _, id := range ps.memberRoles[memberID] {
		if r := ps.roles[id]; r != nil {
			p |= r.perms
		}
	}
	if p&permAdministrator != 0 {
		return permAll
	}
	return p
}

// top returns the position of the member's highest role (owner: above all).
func (ps *permSnapshot) top(memberID string) int64 {
	if ps.owners[memberID] {
		return math.MaxInt64
	}
	var top int64
	for _, id := range ps.memberRoles[memberID] {
		if r := ps.roles[id]; r != nil && r.Position > top {
			top = r.Position
		}
	}
	return top
}

// applyOverrides applies one channel's overrides for a member: @everyone,
// then the member's roles combined, then the member itself.
func (ps *permSnapshot) applyOverrides(p perm, c *channel, memberID string) perm {
	has := map[int64]bool{}
	for _, id := range ps.memberRoles[memberID] {
		has[id] = true
	}
	var roleAllow, roleDeny perm
	for _, o := range c.Overrides {
		switch {
		case o.Type == targetRole && o.roleID() == everyoneRoleID:
			p = p&^o.deny | o.allow
		case o.Type == targetRole && has[o.roleID()]:
			roleAllow |= o.allow
			roleDeny |= o.deny
		}
	}
	p = p&^roleDeny | roleAllow
	for _, o := range c.Overrides {
		if o.Type == targetMember && o.TargetID == memberID {
			p = p&^o.deny | o.allow
		}
	}
	return p
}

// inChannel returns a member's permissions in a channel: server permissions,
// then the parent category's overrides, then the channel's own overrides.
// Without view_channel a member has no permission at all in the channel.
func (ps *permSnapshot) inChannel(memberID string, channelID int64) perm {
	c := ps.channels[channelID]
	if c == nil {
		return 0
	}
	p := ps.base(memberID)
	if p&permAdministrator != 0 {
		return permAll
	}
	if c.ParentID != nil {
		if parent := ps.channels[*c.ParentID]; parent != nil {
			p = ps.applyOverrides(p, parent, memberID)
		}
	}
	p = ps.applyOverrides(p, c, memberID)
	if p&permViewChannel == 0 {
		return 0
	}
	return p
}

// visibleChannels lists the channels a member can see, in display order.
func (ps *permSnapshot) visibleChannels(memberID string) []*channel {
	out := []*channel{}
	for _, c := range ps.channels {
		if ps.inChannel(memberID, c.ID)&permViewChannel != 0 {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// channelPerms maps each visible channel ID to the member's channel-scoped permission names in it.
func (ps *permSnapshot) channelPerms(memberID string) map[int64][]string {
	out := map[int64][]string{}
	for _, c := range ps.visibleChannels(memberID) {
		out[c.ID] = (ps.inChannel(memberID, c.ID) & permChannelScoped).names()
	}
	return out
}

// outranks reports whether actor may act on target (kick, ban, manage roles):
// the owner outranks everyone; otherwise actor's top role must be strictly higher.
func (ps *permSnapshot) outranks(actor, target string) bool {
	if ps.owners[target] {
		return false
	}
	return ps.top(actor) > ps.top(target)
}

// --- request helpers ---

// requirePerm checks server-level permissions for the requesting member.
func (s *Server) requirePerm(r *http.Request, p perm) (*permSnapshot, error) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	if have := ps.base(memberFrom(r).ID); have&p != p {
		return nil, missing(p &^ have)
	}
	return ps, nil
}

// requireChannelPerm checks permissions in a channel. A channel the member
// cannot see is reported as not found, so its existence does not leak.
func (s *Server) requireChannelPerm(r *http.Request, c *channel, p perm) (*permSnapshot, error) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	have := ps.inChannel(memberFrom(r).ID, c.ID)
	if have&permViewChannel == 0 {
		return nil, errf(http.StatusNotFound, "not_found", "no such channel")
	}
	if have&p != p {
		return nil, missing(p &^ have)
	}
	return ps, nil
}

// needPerm wraps a handler with a server-level permission check.
func (s *Server) needPerm(p perm, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.requirePerm(r, p); err != nil {
			writeErr(w, r, err)
			return
		}
		h(w, r)
	}
}

func (s *Server) handleMyPermissions(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	id := memberFrom(r).ID
	writeJSON(w, http.StatusOK, map[string]any{"server": ps.base(id).names(), "channels": ps.channelPerms(id)})
}
