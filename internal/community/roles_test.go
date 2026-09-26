package community

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestEveryoneDefaultMatchesMigration(t *testing.T) {
	e := newEnv(t)
	var stored perm
	if err := e.srv.db.QueryRow(`SELECT permissions FROM roles WHERE id = ?`, everyoneRoleID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != permEveryoneDefault {
		t.Fatalf("@everyone has %v, want %v", stored.names(), permEveryoneDefault.names())
	}
}

// community is a server with an owner and a few members.
type community struct {
	*testEnv
	owner   string
	members map[string]loginResp // by pseudo
	users   map[string]*user
}

func newCommunity(t *testing.T, pseudos ...string) *community {
	e := newEnv(t)
	_, owner := e.setupOwner()
	c := &community{testEnv: e, owner: owner, members: map[string]loginResp{}, users: map[string]*user{}}
	for _, p := range pseudos {
		u := e.newUser(p)
		c.users[p] = u
		c.members[p] = e.mustLogin(u, loginOpts{invite: e.invite(owner, nil)})
	}
	return c
}

func (c *community) tok(pseudo string) string { return c.members[pseudo].SessionToken }
func (c *community) id(pseudo string) string  { return c.members[pseudo].Member.ID }

func (c *community) createRole(token, name string, perms ...string) int64 {
	c.t.Helper()
	var rl role
	c.expect(201, "", c.call("POST", "/v1/roles", token, map[string]any{"name": name, "permissions": perms}, &rl))
	return rl.ID
}

func (c *community) assign(token, memberID string, roleID int64) result {
	return c.call("PUT", fmt.Sprintf("/v1/members/%s/roles/%d", memberID, roleID), token, nil, nil)
}

func (c *community) override(token string, channelID int64, typ, target string, allow, deny []string) result {
	return c.call("PUT", fmt.Sprintf("/v1/channels/%d/overrides/%s/%s", channelID, typ, target), token,
		map[string]any{"allow": allow, "deny": deny}, nil)
}

func (c *community) channelID(token, name string) int64 {
	c.t.Helper()
	var list []channel
	c.expect(200, "", c.call("GET", "/v1/channels", token, nil, &list))
	return channelNamed(c.t, list, name).ID
}

func (c *community) createChannel(body map[string]any) int64 {
	c.t.Helper()
	var ch channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, body, &ch))
	return ch.ID
}

func visibleNames(t *testing.T, c *community, token string) []string {
	t.Helper()
	var list []channel
	c.expect(200, "", c.call("GET", "/v1/channels", token, nil, &list))
	names := []string{}
	for _, ch := range list {
		names = append(names, ch.Name)
	}
	return names
}

func TestRolesAndHierarchy(t *testing.T) {
	c := newCommunity(t, "bob", "carol", "dave")

	c.expect(403, "missing_permissions", c.call("POST", "/v1/roles", c.tok("bob"), map[string]any{"name": "x"}, nil))
	c.expect(400, "invalid_permission", c.call("POST", "/v1/roles", c.owner, map[string]any{"name": "x", "permissions": []string{"fly"}}, nil))
	c.expect(400, "invalid_name", c.call("POST", "/v1/roles", c.owner, map[string]any{"name": "@admin"}, nil))

	mod := c.createRole(c.owner, "Modérateur", "manage_messages", "kick_members", "ban_members", "manage_roles")
	vip := c.createRole(c.owner, "VIP")
	// New roles start at the bottom: VIP (1) under Modérateur (2).
	var roles []role
	c.expect(200, "", c.call("GET", "/v1/roles", c.tok("carol"), nil, &roles))
	if len(roles) != 3 || roles[0].ID != mod || roles[0].Position != 2 || roles[1].ID != vip || roles[2].ID != everyoneRoleID {
		t.Fatalf("roles = %+v", roles)
	}

	c.expect(200, "", c.assign(c.owner, c.id("bob"), mod))
	var me memberJSON
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, &me))
	if len(me.Roles) != 1 || me.Roles[0] != mod {
		t.Fatalf("bob roles = %v", me.Roles)
	}

	// Bob can create roles, but only with permissions he has.
	helper := c.createRole(c.tok("bob"), "Helper", "manage_messages")
	c.expect(403, "missing_permissions", c.call("POST", "/v1/roles", c.tok("bob"), map[string]any{"name": "Admin", "permissions": []string{"administrator"}}, nil))

	// He manages roles strictly below his own, and cannot grant his own role.
	c.expect(200, "", c.assign(c.tok("bob"), c.id("carol"), helper))
	c.expect(403, "role_hierarchy", c.assign(c.tok("bob"), c.id("carol"), mod))
	c.expect(403, "role_hierarchy", c.call("PATCH", fmt.Sprint("/v1/roles/", mod), c.tok("bob"), map[string]any{"permissions": []string{"administrator"}}, nil))
	c.expect(403, "role_hierarchy", c.call("PATCH", fmt.Sprint("/v1/roles/", helper), c.tok("bob"), map[string]any{"position": 3}, nil))
	c.expect(403, "missing_permissions", c.call("PATCH", fmt.Sprint("/v1/roles/", helper), c.tok("bob"), map[string]any{"permissions": []string{"manage_server"}}, nil))

	// Kicks follow the hierarchy too.
	c.expect(403, "missing_permissions", c.call("POST", "/v1/members/"+c.id("bob")+"/kick", c.tok("carol"), map[string]any{}, nil))
	c.expect(200, "", c.assign(c.owner, c.id("dave"), mod))
	c.expect(403, "role_hierarchy", c.call("POST", "/v1/members/"+c.id("dave")+"/kick", c.tok("bob"), map[string]any{}, nil))
	ownerID := c.srv.mustOwnerID(t)
	c.expect(403, "role_hierarchy", c.call("POST", "/v1/members/"+ownerID+"/kick", c.tok("bob"), map[string]any{}, nil))
	c.expect(400, "self_moderation", c.call("POST", "/v1/members/"+c.id("bob")+"/kick", c.tok("bob"), map[string]any{}, nil))
	c.expect(204, "", c.call("POST", "/v1/members/"+c.id("carol")+"/kick", c.tok("bob"), map[string]any{"reason": "spam"}, nil))
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("carol"), nil, nil))
	// A kicked member can come back with an invite, without their old roles.
	back := c.mustLogin(c.users["carol"], loginOpts{invite: c.invite(c.owner, nil)})
	if len(back.Member.Roles) != 0 {
		t.Fatalf("carol kept roles %v", back.Member.Roles)
	}

	// Reordering and deleting.
	var moved role
	c.expect(200, "", c.call("PATCH", fmt.Sprint("/v1/roles/", vip), c.owner, map[string]any{"position": 3, "color": 0xff0000, "name": "V.I.P."}, &moved))
	if moved.Position != 3 || moved.Color != 0xff0000 || moved.Name != "V.I.P." {
		t.Fatalf("moved = %+v", moved)
	}
	c.expect(400, "everyone_role", c.call("PATCH", fmt.Sprint("/v1/roles/", everyoneRoleID), c.owner, map[string]any{"name": "tous"}, nil))
	c.expect(400, "everyone_role", c.call("DELETE", fmt.Sprint("/v1/roles/", everyoneRoleID), c.owner, nil, nil))
	c.expect(204, "", c.call("DELETE", fmt.Sprint("/v1/roles/", mod), c.owner, nil, nil))
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, &me))
	if len(me.Roles) != 0 {
		t.Fatalf("bob still has deleted role: %v", me.Roles)
	}
	c.expect(200, "", c.call("GET", "/v1/roles", c.owner, nil, &roles))
	for i, r := range roles[:len(roles)-1] {
		if r.Position != int64(len(roles)-1-i) {
			t.Fatalf("positions not renumbered: %+v", roles)
		}
	}
	// Without the role, bob lost his powers.
	c.expect(403, "missing_permissions", c.call("POST", "/v1/roles", c.tok("bob"), map[string]any{"name": "x"}, nil))
}

func (s *Server) mustOwnerID(t *testing.T) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(`SELECT id FROM members WHERE is_owner = 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChannelOverrides(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	modo := c.createRole(c.owner, "Modo", "manage_roles", "manage_messages")
	staff := c.createChannel(map[string]any{"type": "category", "name": "Staff"})
	c.expect(200, "", c.override(c.owner, staff, "role", fmt.Sprint(everyoneRoleID), nil, []string{"view_channel"}))
	c.expect(200, "", c.override(c.owner, staff, "role", fmt.Sprint(modo), []string{"view_channel"}, nil))
	secret := c.createChannel(map[string]any{"name": "modération", "parent_id": staff})

	// The category's overrides apply to its channels.
	for _, n := range visibleNames(t, c, c.tok("bob")) {
		if n == "Staff" || n == "modération" {
			t.Fatalf("bob sees %q", n)
		}
	}
	path := fmt.Sprint("/v1/channels/", secret, "/messages")
	c.expect(404, "not_found", c.call("GET", path, c.tok("bob"), nil, nil))
	c.expect(404, "not_found", c.call("POST", path, c.tok("bob"), map[string]string{"content": "coucou"}, nil))

	c.expect(200, "", c.assign(c.owner, c.id("bob"), modo))
	if names := strings.Join(visibleNames(t, c, c.tok("bob")), ","); !strings.Contains(names, "modération") {
		t.Fatalf("bob with Modo sees %s", names)
	}
	c.expect(201, "", c.call("POST", path, c.tok("bob"), map[string]string{"content": "réunion"}, nil))

	// A member override beats role overrides: carol may read but not write in général.
	general := c.channelID(c.owner, "général")
	c.expect(200, "", c.override(c.tok("bob"), general, "member", c.id("carol"), nil, []string{"send_messages"}))
	gpath := fmt.Sprint("/v1/channels/", general, "/messages")
	c.expect(403, "missing_permissions", c.call("POST", gpath, c.tok("carol"), map[string]string{"content": "hello"}, nil))
	c.expect(200, "", c.call("GET", gpath, c.tok("carol"), nil, nil))
	var perms struct {
		Server   []string            `json:"server"`
		Channels map[string][]string `json:"channels"`
	}
	c.expect(200, "", c.call("GET", "/v1/members/@me/permissions", c.tok("carol"), nil, &perms))
	if got := strings.Join(perms.Channels[fmt.Sprint(general)], ","); strings.Contains(got, "send_messages") || !strings.Contains(got, "view_channel") {
		t.Fatalf("carol in général: %s", got)
	}
	c.expect(204, "", c.call("DELETE", fmt.Sprintf("/v1/channels/%d/overrides/member/%s", general, c.id("carol")), c.tok("bob"), nil, nil))
	c.expect(201, "", c.call("POST", gpath, c.tok("carol"), map[string]string{"content": "hello"}, nil))

	// Validation and limits.
	c.expect(400, "invalid_permission", c.override(c.owner, general, "role", fmt.Sprint(modo), []string{"kick_members"}, nil))
	c.expect(400, "invalid_permission", c.override(c.owner, general, "role", fmt.Sprint(modo), []string{"send_messages"}, []string{"send_messages"}))
	c.expect(404, "not_found", c.override(c.owner, general, "role", "999", nil, []string{"send_messages"}))
	c.expect(403, "missing_permissions", c.override(c.tok("carol"), general, "role", fmt.Sprint(everyoneRoleID), nil, []string{"send_messages"}))
	// Bob has manage_roles but not mention_everyone: he cannot hand it out.
	c.expect(403, "missing_permissions", c.override(c.tok("bob"), general, "member", c.id("carol"), []string{"mention_everyone"}, nil))
	// Nor touch overrides of his own role.
	c.expect(403, "role_hierarchy", c.override(c.tok("bob"), general, "role", fmt.Sprint(modo), nil, []string{"send_messages"}))

	// Deleting the category moves its channel to the top level, without the category's overrides.
	c.expect(204, "", c.call("DELETE", fmt.Sprint("/v1/channels/", staff), c.owner, nil, nil))
	if names := strings.Join(visibleNames(t, c, c.tok("carol")), ","); !strings.Contains(names, "modération") {
		t.Fatalf("after deleting the category carol sees %s", names)
	}
}

func TestMentionPermissions(t *testing.T) {
	c := newCommunity(t, "bob")
	quiet := c.createRole(c.owner, "Discret")
	var loud role
	c.expect(201, "", c.call("POST", "/v1/roles", c.owner, map[string]any{"name": "Annonces", "mentionable": true}, &loud))
	path := fmt.Sprint("/v1/channels/", c.channelID(c.owner, "général"), "/messages")
	content := fmt.Sprintf("@everyone <@&%d> <@&%d> <@&%d>", quiet, loud.ID, everyoneRoleID)

	var m message
	c.expect(201, "", c.call("POST", path, c.tok("bob"), map[string]string{"content": content}, &m))
	if m.MentionEveryone || len(m.MentionRoles) != 1 || m.MentionRoles[0] != loud.ID {
		t.Fatalf("bob's mentions: everyone=%v roles=%v", m.MentionEveryone, m.MentionRoles)
	}
	c.expect(201, "", c.call("POST", path, c.owner, map[string]string{"content": content}, &m))
	if !m.MentionEveryone || len(m.MentionRoles) != 2 {
		t.Fatalf("owner's mentions: everyone=%v roles=%v", m.MentionEveryone, m.MentionRoles)
	}
	var history []message
	c.expect(200, "", c.call("GET", path, c.tok("bob"), nil, &history))
	if len(history) != 2 || len(history[1].MentionRoles) != 2 {
		t.Fatalf("history role mentions = %+v", history)
	}
}

func TestBans(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	mod := c.createRole(c.owner, "Modo", "ban_members")
	c.expect(200, "", c.assign(c.owner, c.id("carol"), mod))

	c.expect(403, "missing_permissions", c.call("PUT", "/v1/bans/"+c.id("carol"), c.tok("bob"), map[string]any{}, nil))
	c.expect(403, "role_hierarchy", c.call("PUT", "/v1/bans/"+c.srv.mustOwnerID(t), c.tok("carol"), map[string]any{}, nil))
	c.expect(204, "", c.call("PUT", "/v1/bans/"+c.id("bob"), c.tok("carol"), map[string]any{"reason": "insultes"}, nil))
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, nil))

	// The ban follows the identity: no invite gets bob back in.
	c.expect(403, "banned", c.login(c.users["bob"], loginOpts{invite: c.invite(c.owner, nil)}, nil))
	c.srv.db.Exec(`UPDATE settings SET value = 'public' WHERE key = 'access'`)
	c.expect(403, "banned", c.login(c.users["bob"], loginOpts{}, nil))

	var bans []banJSON
	c.expect(200, "", c.call("GET", "/v1/bans", c.tok("carol"), nil, &bans))
	if len(bans) != 1 || bans[0].Member.ID != c.id("bob") || bans[0].Reason != "insultes" || bans[0].BannedBy == nil || *bans[0].BannedBy != c.id("carol") {
		t.Fatalf("bans = %+v", bans)
	}

	c.expect(204, "", c.call("DELETE", "/v1/bans/"+c.id("bob"), c.tok("carol"), nil, nil))
	c.expect(404, "not_found", c.call("DELETE", "/v1/bans/"+c.id("bob"), c.tok("carol"), nil, nil))
	c.mustLogin(c.users["bob"], loginOpts{})

	// Members who already left can be banned pre-emptively.
	dave := c.newUser("dave")
	daveID := c.mustLogin(dave, loginOpts{}).Member.ID
	daveTok := c.mustLogin(dave, loginOpts{}).SessionToken
	c.expect(204, "", c.call("DELETE", "/v1/members/@me", daveTok, nil, nil))
	c.expect(204, "", c.call("PUT", "/v1/bans/"+daveID, c.owner, map[string]any{}, nil))
	c.expect(403, "banned", c.login(dave, loginOpts{}, nil))
	c.expect(404, "not_found", c.call("PUT", "/v1/bans/nobody", c.owner, map[string]any{}, nil))
}

func TestGatewayPermissions(t *testing.T) {
	c := newCommunity(t, "bob")
	hidden := c.createChannel(map[string]any{"name": "secret"})
	c.expect(200, "", c.override(c.owner, hidden, "role", fmt.Sprint(everyoneRoleID), nil, []string{"view_channel"}))
	seer := c.createRole(c.owner, "Initié")
	c.expect(200, "", c.override(c.owner, hidden, "role", fmt.Sprint(seer), []string{"view_channel"}, nil))
	general := c.channelID(c.owner, "général")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.http.URL, "http")+"/v1/gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": c.tok("bob")})
	type ev struct {
		T string
		D struct {
			Content  string
			Channels []channel
		}
	}
	var ready ev
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready.T != "READY" {
		t.Fatalf("READY: %v %+v", err, ready)
	}
	for _, ch := range ready.D.Channels {
		if ch.ID == hidden {
			t.Fatal("READY includes a hidden channel")
		}
	}

	// A message in the hidden channel is not delivered; the next visible one is.
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", hidden, "/messages"), c.owner, map[string]string{"content": "chut"}, nil))
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", general, "/messages"), c.owner, map[string]string{"content": "public"}, nil))
	var got ev
	if err := wsjson.Read(ctx, conn, &got); err != nil || got.T != "MESSAGE_CREATE" || got.D.Content != "public" {
		t.Fatalf("expected the public message first, got %v %+v", err, got)
	}

	// Gaining a role pushes the new channel list.
	c.expect(200, "", c.assign(c.owner, c.id("bob"), seer))
	for {
		if err := wsjson.Read(ctx, conn, &got); err != nil {
			t.Fatal(err)
		}
		if got.T == "CHANNELS_SYNC" {
			break
		}
	}
	found := false
	for _, ch := range got.D.Channels {
		found = found || ch.ID == hidden
	}
	if !found {
		t.Fatal("CHANNELS_SYNC after gaining the role does not include the channel")
	}

	// Being kicked closes the connection.
	c.expect(204, "", c.call("POST", "/v1/members/"+c.id("bob")+"/kick", c.owner, map[string]any{}, nil))
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("close after kick: %v", err)
			}
			break
		}
	}
}

// The owner hands the server over; the host can take it back when the owner
// can no longer sign in, and gets a new claim code.
func TestOwnership(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	ownerID := c.srv.mustOwner(t)
	c.expect(403, "not_owner", c.call("POST", "/v1/members/"+c.id("carol")+"/transfer-ownership", c.tok("bob"), nil, nil))
	c.expect(400, "invalid_target", c.call("POST", "/v1/members/"+ownerID+"/transfer-ownership", c.owner, nil, nil))
	c.expect(204, "", c.call("POST", "/v1/members/"+c.id("bob")+"/transfer-ownership", c.owner, nil, nil))
	if got := c.srv.mustOwner(t); got != c.id("bob") {
		t.Fatalf("owner is %s, want bob", got)
	}
	// The former owner stays, without special rights; the new one can leave no more.
	c.expect(403, "missing_permissions", c.call("PATCH", "/v1/server", c.owner, map[string]any{"name": "Pris"}, nil))
	c.expect(400, "owner_cannot_leave", c.call("DELETE", "/v1/members/@me", c.tok("bob"), nil, nil))

	code, err := c.srv.ResetOwnership(context.Background())
	if err != nil || code == "" {
		t.Fatalf("reset: %q %v", code, err)
	}
	var owners int
	c.srv.db.QueryRow(`SELECT COUNT(*) FROM members WHERE is_owner = 1`).Scan(&owners)
	if owners != 0 {
		t.Fatalf("%d owners after reset", owners)
	}
	lr := c.mustLogin(c.users["carol"], loginOpts{claim: code})
	if !lr.Member.Owner {
		t.Fatal("claim after reset did not make carol the owner")
	}
}

func (s *Server) mustOwner(t *testing.T) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(`SELECT id FROM members WHERE is_owner = 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
