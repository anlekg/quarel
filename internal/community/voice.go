package community

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/voice"
)

// Voice channels are LiveKit rooms named "channel-<id>". The community server
// never carries media: it hands out join tokens according to permissions
// (connect to join, speak for the microphone, stream for camera and screen),
// learns who is connected and what they share from LiveKit webhooks, and
// removes participants or changes their rights when permissions or server
// mute/deafen change.

// VoiceBackend is the SFU. *voice.LiveKit implements it.
type VoiceBackend interface {
	JoinToken(room, identity, name string, g voice.Grant, ttl time.Duration) (string, error)
	RemoveParticipant(ctx context.Context, room, identity string) error
	SetPermissions(ctx context.Context, room, identity string, g voice.Grant) error
	Participants(ctx context.Context) (map[string][]string, error)
	ReceiveWebhook(r *http.Request) (voice.Event, error)
}

// VoiceOptions enables voice on a Server.
type VoiceOptions struct {
	Backend VoiceBackend
	// Proxy forwards /lk/ to an embedded livekit-server (nil for an external one).
	Proxy http.Handler
	// PublicURL is the LiveKit URL given to clients; empty means this server's /lk.
	PublicURL string
}

const voiceTokenTTL = time.Hour

type voiceState struct {
	MemberID   string    `json:"member_id"`
	ChannelID  int64     `json:"channel_id"`
	SelfMute   bool      `json:"self_mute"`   // set by the member (informative)
	SelfDeaf   bool      `json:"self_deaf"`   // set by the member (informative)
	ServerMute bool      `json:"server_mute"` // by a moderator: microphone blocked
	ServerDeaf bool      `json:"server_deaf"` // by a moderator: hears nothing
	CanSpeak   bool      `json:"can_speak"`   // speak permission and not server-muted
	CanStream  bool      `json:"can_stream"`  // stream permission: camera and screen
	Video      bool      `json:"video"`       // camera on
	Screen     bool      `json:"screen"`      // sharing their screen
	JoinedAt   time.Time `json:"joined_at"`

	grant voice.Grant // what LiveKit currently allows
}

// voiceMod is a member's server mute/deafen, set by moderators.
type voiceMod struct{ mute, deaf bool }

func (s *Server) voiceMods(ctx context.Context) (map[string]voiceMod, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, voice_mute, voice_deaf FROM members WHERE voice_mute = 1 OR voice_deaf = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]voiceMod{}
	for rows.Next() {
		var id string
		var m voiceMod
		if err := rows.Scan(&id, &m.mute, &m.deaf); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// voiceGrant is what a member may do in a voice channel.
func voiceGrant(ps *permSnapshot, mod voiceMod, memberID string, chID int64) voice.Grant {
	p := ps.inChannel(memberID, chID)
	return voice.Grant{
		Microphone: p&permSpeak != 0 && !mod.mute,
		Camera:     p&permStream != 0,
		Screen:     p&permStream != 0,
		Listen:     !mod.deaf,
	}
}

// apply records a grant in the state; shares no longer allowed stop.
func (st *voiceState) apply(g voice.Grant, mod voiceMod) {
	st.grant = g
	st.CanSpeak, st.CanStream = g.Microphone, g.Camera
	st.ServerMute, st.ServerDeaf = mod.mute, mod.deaf
	st.Video = st.Video && g.Camera
	st.Screen = st.Screen && g.Screen
}

// voiceRegistry is the in-memory list of who is in which voice channel.
// LiveKit is the source of truth; the registry is rebuilt from it at startup.
type voiceRegistry struct {
	mu     sync.Mutex
	states map[string]*voiceState // by member ID
}

func roomName(channelID int64) string { return fmt.Sprint("channel-", channelID) }

func roomChannel(room string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(room, "channel-"), 10, 64)
	return id, err == nil && strings.HasPrefix(room, "channel-")
}

// EnableVoice turns voice channels on.
func (s *Server) EnableVoice(o VoiceOptions) {
	s.voiceOpts = &o
	s.voice = &voiceRegistry{states: map[string]*voiceState{}}
}

func (s *Server) requireVoice() error {
	if s.voiceOpts == nil {
		return errf(http.StatusServiceUnavailable, "voice_disabled", "voice is disabled on this server")
	}
	return nil
}

// voiceStates returns a copy of the states in channels the member can see.
func (s *Server) voiceStates(ps *permSnapshot, memberID string) []voiceState {
	out := []voiceState{}
	if s.voice == nil {
		return out
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	for _, st := range s.voice.states {
		if ps.inChannel(memberID, st.ChannelID)&permViewChannel != 0 {
			out = append(out, *st)
		}
	}
	return out
}

// broadcastVoice announces st to the members who can see its channel, and a
// departure (channel_id null) to those who could only see previous.
func (s *Server) broadcastVoice(ctx context.Context, memberID string, st *voiceState, previous int64) {
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for VOICE_STATE_UPDATE", err)
		return
	}
	left := map[string]any{"member_id": memberID, "channel_id": nil}
	s.hub.SendEachIf("VOICE_STATE_UPDATE", func(viewer, _ string) (any, bool) {
		if st != nil && ps.inChannel(viewer, st.ChannelID)&permViewChannel != 0 {
			return st, true
		}
		if previous != 0 && ps.inChannel(viewer, previous)&permViewChannel != 0 {
			return left, true
		}
		return nil, false
	})
}

func (s *Server) voiceURL(r *http.Request) string {
	if s.voiceOpts.PublicURL != "" {
		return s.voiceOpts.PublicURL
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/lk"
}

// handleVoiceJoin issues a LiveKit token for a voice channel. The member
// appears in the channel once their client actually connects to LiveKit.
func (s *Server) handleVoiceJoin(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := s.pathChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requireChannelPerm(r, c, permConnect)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if c.Type != chanVoice {
		writeErr(w, r, errf(http.StatusBadRequest, "not_voice_channel", "only voice channels can be joined"))
		return
	}
	m := memberFrom(r)
	mods, err := s.voiceMods(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	mod := mods[m.ID]
	g := voiceGrant(ps, mod, m.ID, c.ID)
	token, err := s.voiceOpts.Backend.JoinToken(roomName(c.ID), m.ID, m.json().DisplayName, g, voiceTokenTTL)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": s.voiceURL(r), "token": token, "room": roomName(c.ID),
		"can_speak": g.Microphone, "can_stream": g.Camera, "server_mute": mod.mute, "server_deaf": mod.deaf})
}

func (s *Server) handleVoiceStates(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.voiceStates(ps, memberFrom(r).ID))
}

// handleVoiceSelfState updates the member's own mute/deafen flags (informative:
// the client mutes itself; deafened implies muted).
func (s *Server) handleVoiceSelfState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SelfMute *bool `json:"self_mute"`
		SelfDeaf *bool `json:"self_deaf"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	id := memberFrom(r).ID
	s.voice.mu.Lock()
	st := s.voice.states[id]
	if st != nil {
		if req.SelfMute != nil {
			st.SelfMute = *req.SelfMute
		}
		if req.SelfDeaf != nil {
			st.SelfDeaf = *req.SelfDeaf
		}
		if st.SelfDeaf {
			st.SelfMute = true
		}
	}
	var snapshot voiceState
	if st != nil {
		snapshot = *st
	}
	s.voice.mu.Unlock()
	if st == nil {
		writeErr(w, r, errf(http.StatusConflict, "not_in_voice", "you are not in a voice channel"))
		return
	}
	s.broadcastVoice(r.Context(), id, &snapshot, 0)
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleVoiceLeave(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.disconnectVoice(r.Context(), memberFrom(r).ID)
	w.WriteHeader(http.StatusNoContent)
}

// disconnectVoice removes a member from voice, in LiveKit and locally.
func (s *Server) disconnectVoice(ctx context.Context, memberID string) {
	if s.voice == nil {
		return
	}
	s.voice.mu.Lock()
	st := s.voice.states[memberID]
	delete(s.voice.states, memberID)
	s.voice.mu.Unlock()
	if st == nil {
		return
	}
	if err := s.voiceOpts.Backend.RemoveParticipant(ctx, roomName(st.ChannelID), memberID); err != nil && err != voice.ErrNotFound {
		s.logErr("removing voice participant", err)
	}
	s.broadcastVoice(ctx, memberID, nil, st.ChannelID)
}

// handleLiveKitWebhook receives room events from LiveKit (signed with the API secret).
func (s *Server) handleLiveKitWebhook(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	ev, err := s.voiceOpts.Backend.ReceiveWebhook(r)
	if err != nil {
		writeErr(w, r, errf(http.StatusUnauthorized, "invalid_webhook", "%v", err))
		return
	}
	s.handleVoiceEvent(r.Context(), ev)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleVoiceEvent(ctx context.Context, ev voice.Event) {
	chID, ok := roomChannel(ev.Room)
	if !ok {
		return
	}
	switch ev.Type {
	case "participant_joined":
		s.voiceJoined(ctx, chID, ev.Identity)
	case "participant_left":
		s.voice.mu.Lock()
		st := s.voice.states[ev.Identity]
		if st != nil && st.ChannelID == chID {
			delete(s.voice.states, ev.Identity)
		}
		s.voice.mu.Unlock()
		if st != nil && st.ChannelID == chID {
			s.broadcastVoice(ctx, ev.Identity, nil, chID)
		}
	case "room_finished":
		for _, id := range s.voiceMembersIn(chID) {
			s.handleVoiceEvent(ctx, voice.Event{Type: "participant_left", Room: ev.Room, Identity: id})
		}
	case "track_published", "track_unpublished":
		on := ev.Type == "track_published"
		s.voice.mu.Lock()
		st := s.voice.states[ev.Identity]
		changed := false
		if st != nil && st.ChannelID == chID {
			switch ev.Source {
			case voice.SourceCamera:
				changed, st.Video = st.Video != (on && st.grant.Camera), on && st.grant.Camera
			case voice.SourceScreen:
				changed, st.Screen = st.Screen != (on && st.grant.Screen), on && st.grant.Screen
			}
		}
		var snapshot voiceState
		if changed {
			snapshot = *st
		}
		s.voice.mu.Unlock()
		if changed {
			s.broadcastVoice(ctx, ev.Identity, &snapshot, 0)
		}
	}
}

func (s *Server) voiceMembersIn(chID int64) []string {
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	ids := []string{}
	for id, st := range s.voice.states {
		if st.ChannelID == chID {
			ids = append(ids, id)
		}
	}
	return ids
}

// voiceJoined records a participant LiveKit reports as connected, after
// re-checking their rights (a token may outlive a permission change).
// Joining another channel moves the member: one voice channel at a time.
func (s *Server) voiceJoined(ctx context.Context, chID int64, memberID string) {
	backend := s.voiceOpts.Backend
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for voice join", err)
		return
	}
	m, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, memberID)
	p := ps.inChannel(memberID, chID)
	if err != nil || m == nil || p&permConnect == 0 {
		backend.RemoveParticipant(ctx, roomName(chID), memberID)
		return
	}
	mods, err := s.voiceMods(ctx)
	if err != nil {
		s.logErr("loading voice moderation", err)
		return
	}
	st := &voiceState{MemberID: memberID, ChannelID: chID, JoinedAt: s.now().UTC().Truncate(time.Millisecond)}
	g := voiceGrant(ps, mods[memberID], memberID, chID)
	st.apply(g, mods[memberID])
	s.voice.mu.Lock()
	prev := s.voice.states[memberID]
	s.voice.states[memberID] = st
	snapshot := *st
	s.voice.mu.Unlock()

	var previous int64
	if prev != nil && prev.ChannelID != chID {
		previous = prev.ChannelID
		if err := backend.RemoveParticipant(ctx, roomName(prev.ChannelID), memberID); err != nil && err != voice.ErrNotFound {
			s.logErr("moving voice participant", err)
		}
	}
	s.broadcastVoice(ctx, memberID, &snapshot, previous)
}

// reconcileVoice applies permission and server mute/deafen changes to
// connected participants: removes those who lost connect (or membership, or
// the channel), and updates what LiveKit lets the others publish and hear.
func (s *Server) reconcileVoice(ctx context.Context) {
	if s.voice == nil {
		return
	}
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		s.logErr("loading permissions for voice", err)
		return
	}
	mods, err := s.voiceMods(ctx)
	if err != nil {
		s.logErr("loading voice moderation", err)
		return
	}
	active, err := s.activeMemberIDs(ctx)
	if err != nil {
		s.logErr("listing members for voice", err)
		return
	}
	type change struct {
		st     voiceState
		remove bool
	}
	var changes []change
	s.voice.mu.Lock()
	for id, st := range s.voice.states {
		p := ps.inChannel(id, st.ChannelID)
		g, mod := voiceGrant(ps, mods[id], id, st.ChannelID), mods[id]
		switch {
		case !active[id] || p&permConnect == 0:
			delete(s.voice.states, id)
			changes = append(changes, change{*st, true})
		case g != st.grant || mod.mute != st.ServerMute || mod.deaf != st.ServerDeaf:
			st.apply(g, mod)
			changes = append(changes, change{*st, false})
		}
	}
	s.voice.mu.Unlock()

	for _, ch := range changes {
		room := roomName(ch.st.ChannelID)
		if ch.remove {
			if err := s.voiceOpts.Backend.RemoveParticipant(ctx, room, ch.st.MemberID); err != nil && err != voice.ErrNotFound {
				s.logErr("removing voice participant", err)
			}
			s.broadcastVoice(ctx, ch.st.MemberID, nil, ch.st.ChannelID)
			continue
		}
		if err := s.voiceOpts.Backend.SetPermissions(ctx, room, ch.st.MemberID, ch.st.grant); err != nil {
			s.logErr("updating voice permission", err)
		}
		s.broadcastVoice(ctx, ch.st.MemberID, &ch.st, 0)
	}
}

func (s *Server) activeMemberIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM members WHERE left_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SyncVoice rebuilds the voice registry from LiveKit (after a restart of the
// community server while LiveKit kept running) and applies current permissions.
func (s *Server) SyncVoice(ctx context.Context) error {
	if s.voice == nil {
		return nil
	}
	rooms, err := s.voiceOpts.Backend.Participants(ctx)
	if err != nil {
		return err
	}
	for room, ids := range rooms {
		chID, ok := roomChannel(room)
		if !ok {
			continue
		}
		for _, id := range ids {
			s.voiceJoined(ctx, chID, id)
		}
	}
	return nil
}

// --- voice moderation ---

// voiceStateOf returns a copy of a member's voice state, or nil.
func (s *Server) voiceStateOf(memberID string) *voiceState {
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	if st := s.voice.states[memberID]; st != nil {
		c := *st
		return &c
	}
	return nil
}

// handleVoiceModerate: PATCH /v1/voice/states/{member} {mute?, deaf?, channel_id?, reason?}.
// Server mute and deafen persist until lifted, even across voice sessions.
// The permissions are checked in the member's current voice channel (or
// server-wide when they are not in voice).
func (s *Server) handleVoiceModerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mute      *bool  `json:"mute"`
		Deaf      *bool  `json:"deaf"`
		ChannelID *int64 `json:"channel_id"` // move them to this voice channel
		Reason    string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	ps, me, target, st, err := s.voiceTarget(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	has := func(p perm) bool {
		if st != nil {
			return ps.inChannel(me, st.ChannelID)&p != 0
		}
		return ps.base(me)&p != 0
	}
	for _, c := range []struct {
		set  bool
		perm perm
	}{{req.Mute != nil, permMuteMembers}, {req.Deaf != nil, permDeafenMembers}, {req.ChannelID != nil, permMoveMembers}} {
		if c.set && !has(c.perm) {
			writeErr(w, r, ps.deny(me, c.perm))
			return
		}
	}
	if err := checkVoiceRank(ps, me, target); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.ChannelID != nil {
		if st == nil {
			writeErr(w, r, errf(http.StatusConflict, "not_in_voice", "this member is not in a voice channel"))
			return
		}
		dest := ps.channels[*req.ChannelID]
		if dest == nil || ps.inChannel(me, dest.ID)&permViewChannel == 0 {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such channel"))
			return
		}
		if dest.Type != chanVoice {
			writeErr(w, r, errf(http.StatusBadRequest, "not_voice_channel", "members can only be moved to a voice channel"))
			return
		}
		if ps.inChannel(me, dest.ID)&permMoveMembers == 0 {
			writeErr(w, r, ps.deny(me, permMoveMembers))
			return
		}
		if ps.inChannel(target, dest.ID)&permConnect == 0 {
			writeErr(w, r, errf(http.StatusForbidden, "target_cannot_connect", "this member may not join that channel"))
			return
		}
	}
	for _, c := range []struct {
		set    *bool
		col    string
		action string
	}{{req.Mute, "voice_mute", auditVoiceMute}, {req.Deaf, "voice_deaf", auditVoiceDeafen}} {
		if c.set == nil {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE members SET `+c.col+` = ? WHERE id = ?`, *c.set, target); err != nil {
			writeErr(w, r, err)
			return
		}
		s.audit(ctx, s.db, me, c.action, target, reason, map[string]any{"enabled": *c.set})
	}
	if req.Mute != nil || req.Deaf != nil {
		s.reconcileVoice(ctx)
	}
	if req.ChannelID != nil && *req.ChannelID != st.ChannelID {
		s.audit(ctx, s.db, me, auditVoiceMove, target, reason, map[string]any{"from": st.ChannelID, "to": *req.ChannelID})
		// LiveKit cannot move a participant between rooms: the member leaves
		// this one now, and their client joins the new one on VOICE_MOVE.
		s.disconnectVoice(ctx, target)
		s.hub.BroadcastTo("VOICE_MOVE", map[string]any{"channel_id": *req.ChannelID, "from_channel_id": st.ChannelID},
			func(key, _ string) bool { return key == target })
	}
	mods, err := s.voiceMods(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member_id": target, "server_mute": mods[target].mute, "server_deaf": mods[target].deaf})
}

// handleVoiceKick: DELETE /v1/voice/states/{member} disconnects them from voice.
func (s *Server) handleVoiceKick(w http.ResponseWriter, r *http.Request) {
	if err := s.requireVoice(); err != nil {
		writeErr(w, r, err)
		return
	}
	ps, me, target, st, err := s.voiceTarget(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if st == nil {
		writeErr(w, r, errf(http.StatusConflict, "not_in_voice", "this member is not in a voice channel"))
		return
	}
	if ps.inChannel(me, st.ChannelID)&permMoveMembers == 0 {
		writeErr(w, r, ps.deny(me, permMoveMembers))
		return
	}
	if err := checkVoiceRank(ps, me, target); err != nil {
		writeErr(w, r, err)
		return
	}
	s.disconnectVoice(r.Context(), target)
	s.audit(r.Context(), s.db, me, auditVoiceDisconnect, target, r.URL.Query().Get("reason"), map[string]any{"channel_id": st.ChannelID})
	w.WriteHeader(http.StatusNoContent)
}

// voiceTarget loads the {member} of a voice moderation request (an active member).
func (s *Server) voiceTarget(r *http.Request) (*permSnapshot, string, string, *voiceState, error) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		return nil, "", "", nil, err
	}
	me := memberFrom(r).ID
	m, err := memberBy(r.Context(), s.db, `id = ? AND left_at IS NULL`, r.PathValue("member"))
	if err != nil {
		return nil, "", "", nil, err
	}
	if m == nil {
		return nil, "", "", nil, errf(http.StatusNotFound, "not_found", "no such member")
	}
	return ps, me, m.ID, s.voiceStateOf(m.ID), nil
}

// checkVoiceRank: moderators act on members below them, or on themselves.
func checkVoiceRank(ps *permSnapshot, me, target string) error {
	if target != me && !ps.outranks(me, target) {
		return errf(http.StatusForbidden, "role_hierarchy", "this member's highest role is not below yours")
	}
	return nil
}
