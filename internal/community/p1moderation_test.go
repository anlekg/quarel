package community

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// join makes a new user join the community (invite from the owner).
func (c *community) join(pseudo string) {
	c.t.Helper()
	u := c.newUser(pseudo)
	c.users[pseudo] = u
	c.members[pseudo] = c.mustLogin(u, loginOpts{invite: c.invite(c.owner, nil)})
}

func (c *community) auditActions(token string, query string) []string {
	c.t.Helper()
	var list []auditEntry
	c.expect(200, "", c.call("GET", "/v1/audit-log"+query, token, nil, &list))
	out := []string{}
	for _, e := range list {
		out = append(out, e.Action)
	}
	return out
}

func TestTimeout(t *testing.T) {
	c, fv := newVoiceCommunity(t, "mod", "bob", "admin")
	gen := c.channelID(c.owner, "général")
	voiceCh := c.channelID(c.owner, "Général")
	modRole := c.createRole(c.owner, "Modo", "moderate_members", "view_audit_log")
	c.expect(200, "", c.assign(c.owner, c.id("mod"), modRole))
	adminRole := c.createRole(c.owner, "Admin", "administrator")
	c.expect(200, "", c.assign(c.owner, c.id("admin"), adminRole))
	msg := c.post(c.tok("bob"), gen, map[string]any{"content": "avant"})
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", voiceCh, "/voice/join"), c.tok("bob"), nil, nil))
	c.webhook("participant_joined", voiceCh, c.id("bob"))

	timeout := func(token, who string, secs int64) result {
		return c.call("PUT", "/v1/members/"+who+"/timeout", token, map[string]any{"duration": secs, "reason": "spam"}, nil)
	}
	c.expect(403, "missing_permissions", timeout(c.tok("bob"), c.id("mod"), 60))
	c.expect(403, "role_hierarchy", timeout(c.tok("mod"), c.owner0(), 60))
	c.expect(400, "invalid_duration", timeout(c.tok("mod"), c.id("bob"), 29*24*3600))
	c.expect(400, "cannot_timeout_admin", timeout(c.tok("mod"), c.id("admin"), 60)) // Admin is below Modo, but immune

	var view memberJSON
	c.expect(200, "", c.call("PUT", "/v1/members/"+c.id("bob")+"/timeout", c.tok("mod"), map[string]any{"duration": 600}, &view))
	if view.TimeoutUntil == nil {
		t.Fatal("timeout_until not set")
	}
	if !fv.wasRemoved(fmt.Sprint("channel-", voiceCh), c.id("bob")) {
		t.Fatal("timed-out member kept in voice")
	}
	// Reading only.
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), nil, nil))
	c.expect(403, "timed_out", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), map[string]any{"content": "x"}, nil))
	c.expect(403, "timed_out", c.call("PATCH", fmt.Sprintf("/v1/channels/%d/messages/%d", gen, msg.ID), c.tok("bob"), map[string]any{"content": "réécrit"}, nil))
	c.expect(403, "timed_out", c.call("PUT", fmt.Sprintf("/v1/channels/%d/messages/%d/reactions/%s", gen, msg.ID, url.PathEscape("👍")), c.tok("bob"), nil, nil))
	c.expect(403, "timed_out", c.call("POST", fmt.Sprint("/v1/channels/", voiceCh, "/voice/join"), c.tok("bob"), nil, nil))
	c.expect(403, "timed_out", c.call("POST", "/v1/invites", c.tok("bob"), map[string]any{}, nil))

	// It ends by itself…
	c.clock = c.clock.Add(601 * time.Second)
	c.post(c.tok("bob"), gen, map[string]any{"content": "de retour"})
	// …or is lifted.
	c.expect(200, "", timeout(c.tok("mod"), c.id("bob"), 60))
	c.expect(200, "", c.call("DELETE", "/v1/members/"+c.id("bob")+"/timeout", c.tok("mod"), nil, nil))
	c.post(c.tok("bob"), gen, map[string]any{"content": "encore là"})

	got := c.auditActions(c.tok("mod"), "?target_id="+c.id("bob"))
	want := []string{auditMemberTimeoutEnd, auditMemberTimeout, auditMemberTimeout}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("audit = %v, want %v", got, want)
	}
}

// owner0 is the owner's member ID.
func (c *community) owner0() string {
	c.t.Helper()
	var me memberJSON
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.owner, nil, &me))
	return me.ID
}

func TestAuditLog(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	c.expect(403, "missing_permissions", c.call("GET", "/v1/audit-log", c.tok("bob"), nil, nil))
	rl := c.createRole(c.owner, "Lecteur", "view_audit_log")
	c.expect(200, "", c.assign(c.owner, c.id("bob"), rl))
	ch := c.createChannel(map[string]any{"name": "tmp"})
	c.expect(204, "", c.call("DELETE", fmt.Sprint("/v1/channels/", ch), c.owner, nil, nil))
	c.expect(204, "", c.call("POST", "/v1/members/"+c.id("carol")+"/kick", c.owner, map[string]any{"reason": "calme-toi"}, nil))
	c.expect(204, "", c.call("PUT", "/v1/bans/"+c.id("carol"), c.owner, map[string]any{"reason": "récidive"}, nil))
	c.expect(204, "", c.call("DELETE", "/v1/bans/"+c.id("carol"), c.owner, nil, nil))
	c.expect(200, "", c.call("PATCH", "/v1/server", c.owner, map[string]any{"name": "Nouveau nom"}, nil))

	got := c.auditActions(c.tok("bob"), "")
	want := []string{auditServerUpdate, auditMemberUnban, auditMemberBan, auditMemberKick, auditChannelDelete, auditChannelCreate, auditMemberRoleAdd, auditRoleCreate}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("audit = %v\nwant %v", got, want)
	}
	var list []auditEntry
	c.expect(200, "", c.call("GET", "/v1/audit-log?action=member_ban", c.tok("bob"), nil, &list))
	if len(list) != 1 || list[0].Reason != "récidive" || *list[0].TargetID != c.id("carol") || list[0].ActorID == nil {
		t.Fatalf("ban entry = %+v", list)
	}
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/audit-log?limit=2&before=", list[0].ID), c.tok("bob"), nil, &list))
	if len(list) != 2 || list[0].Action != auditMemberKick {
		t.Fatalf("pagination = %+v", list)
	}
	// Old entries are purged.
	c.clock = c.clock.Add(auditRetention + time.Hour)
	if err := c.srv.Housekeeping(context.Background()); err != nil {
		t.Fatal(err)
	}
	var left int
	c.srv.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d expired entries kept", left)
	}
}

func TestBulkDeleteAndPurge(t *testing.T) {
	c := newCommunity(t, "mod", "bob")
	gen := c.channelID(c.owner, "général")
	other := c.createChannel(map[string]any{"name": "autre"})
	modRole := c.createRole(c.owner, "Modo", "manage_messages")
	c.expect(200, "", c.assign(c.owner, c.id("mod"), modRole))
	var ids []int64
	for i := range 4 {
		ids = append(ids, c.post(c.tok("bob"), gen, map[string]any{"content": fmt.Sprint("spam ", i)}).ID)
	}
	c.post(c.tok("bob"), other, map[string]any{"content": "ailleurs"})
	// Those are two hours old.
	c.srv.db.Exec(`UPDATE messages SET created_at = created_at - ?`, (2 * time.Hour).Milliseconds())
	c.post(c.tok("bob"), gen, map[string]any{"content": "récent"})
	c.post(c.tok("bob"), other, map[string]any{"content": "récent ailleurs"})
	count := func(ch int64) int {
		var msgs []message
		c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", ch, "/messages"), c.owner, nil, &msgs))
		return len(msgs)
	}

	bulk := fmt.Sprintf("/v1/channels/%d/messages/bulk-delete", gen)
	c.expect(403, "missing_permissions", c.call("POST", bulk, c.tok("bob"), map[string]any{"ids": ids[:2]}, nil))
	var res struct{ Deleted int }
	c.expect(200, "", c.call("POST", bulk, c.tok("mod"), map[string]any{"ids": append(ids[:2], 999999)}, &res))
	if res.Deleted != 2 || count(gen) != 3 {
		t.Fatalf("bulk delete: %d deleted, %d left", res.Deleted, count(gen))
	}

	// Purge the last hour only, in the channels the moderator manages (here: all).
	c.expect(200, "", c.call("POST", "/v1/members/"+c.id("bob")+"/purge", c.tok("mod"), map[string]any{"window": 3600}, &res))
	if res.Deleted != 2 || count(gen) != 2 || count(other) != 1 {
		t.Fatalf("purge: %d deleted, général %d, autre %d", res.Deleted, count(gen), count(other))
	}
	c.expect(403, "role_hierarchy", c.call("POST", "/v1/members/"+c.owner0()+"/purge", c.tok("mod"), map[string]any{"window": -1}, nil))

	// A ban can delete everything the member wrote.
	c.expect(204, "", c.call("PUT", "/v1/bans/"+c.id("bob"), c.owner, map[string]any{"delete_messages": -1}, nil))
	if count(gen)+count(other) != 0 {
		t.Fatal("ban kept the banned member's messages")
	}
	if got := c.auditActions(c.owner, "?action=messages_delete"); len(got) != 3 {
		t.Fatalf("messages_delete entries = %v", got)
	}
}

func TestRulesScreen(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	c.expect(400, "no_rules", c.call("POST", "/v1/members/@me/accept-rules", c.tok("bob"), nil, nil))
	c.expect(200, "", c.call("PATCH", "/v1/server", c.owner, map[string]any{"rules": "1. Pas de spam.\n2. Soyez gentils."}, nil))

	var info serverInfo
	c.expect(200, "", c.call("GET", "/v1/server", "", nil, &info)) // public: shown before joining
	if info.Rules == "" {
		t.Fatal("rules not public")
	}
	// Members present when the rules appeared are not locked out.
	c.post(c.tok("bob"), gen, map[string]any{"content": "toujours là"})

	c.join("carol")
	if c.members["carol"].Member.RulesAccepted {
		t.Fatal("newcomer marked as having accepted")
	}
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), nil, nil))
	c.expect(403, "rules_not_accepted", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), map[string]any{"content": "salut"}, nil))
	var view memberJSON
	c.expect(200, "", c.call("POST", "/v1/members/@me/accept-rules", c.tok("carol"), nil, &view))
	if !view.RulesAccepted {
		t.Fatal("acceptance not recorded")
	}
	c.post(c.tok("carol"), gen, map[string]any{"content": "salut"})
}

func TestPhoneVerification(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	gen := c.channelID(c.owner, "général")
	c.expect(503, "phone_verification_unavailable", c.call("POST", "/v1/members/@me/phone", c.tok("bob"), map[string]any{"phone": "+33612345678"}, nil))
	c.expect(400, "phone_verification_unavailable", c.call("PATCH", "/v1/server", c.owner, map[string]any{"require_phone": true}, nil))

	v := newLogVerifier()
	var mu sync.Mutex
	codes := map[string]string{}
	v.send = func(_ context.Context, phone, code string) error {
		mu.Lock()
		codes[phone] = code
		mu.Unlock()
		return nil
	}
	c.srv.phone = v
	c.expect(200, "", c.call("PATCH", "/v1/server", c.owner, map[string]any{"require_phone": true}, nil))
	c.expect(403, "phone_not_verified", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), map[string]any{"content": "x"}, nil))

	verify := func(pseudo, phone string) result {
		c.expect(202, "", c.call("POST", "/v1/members/@me/phone", c.tok(pseudo), map[string]any{"phone": phone}, nil))
		norm, _ := normalizePhone(phone)
		return c.call("POST", "/v1/members/@me/phone/verify", c.tok(pseudo), map[string]any{"phone": phone, "code": codes[norm]}, nil)
	}
	c.expect(400, "invalid_phone", c.call("POST", "/v1/members/@me/phone", c.tok("bob"), map[string]any{"phone": "06 12 34 56 78"}, nil))
	c.expect(202, "", c.call("POST", "/v1/members/@me/phone", c.tok("bob"), map[string]any{"phone": "+33 6 12 34 56 78"}, nil))
	c.expect(400, "invalid_code", c.call("POST", "/v1/members/@me/phone/verify", c.tok("bob"), map[string]any{"phone": "+33612345678", "code": "000000"}, nil))
	c.expect(200, "", verify("bob", "+33 6 12 34 56 78"))
	c.post(c.tok("bob"), gen, map[string]any{"content": "vérifié"})

	// The number is stored as a keyed hash only.
	var stored string
	c.srv.db.QueryRow(`SELECT phone_hash FROM members WHERE id = ?`, c.id("bob")).Scan(&stored)
	if stored == "" || stored == "+33612345678" || len(stored) != 64 {
		t.Fatalf("stored phone = %q", stored)
	}

	c.expect(409, "phone_in_use", verify("carol", "0033 6 12 34 56 78"))
	c.expect(204, "", c.call("PUT", "/v1/bans/"+c.id("bob"), c.owner, map[string]any{}, nil))
	c.expect(403, "phone_banned", verify("carol", "+33612345678")) // no ban evasion with the same number
	c.expect(200, "", verify("carol", "+33700000000"))
	c.post(c.tok("carol"), gen, map[string]any{"content": "moi aussi"})

	// A former member's number (not banned) moves to its new owner.
	c.join("dave")
	c.expect(200, "", verify("dave", "+33711111111"))
	c.expect(204, "", c.call("DELETE", "/v1/members/@me", c.tok("dave"), nil, nil))
	c.join("erin")
	c.expect(200, "", verify("erin", "+33711111111"))
}

func TestTwilioVerifier(t *testing.T) {
	var seen []url.Values
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "AC1" || pass != "secret" {
			w.WriteHeader(401)
			return
		}
		r.ParseForm()
		seen = append(seen, r.PostForm)
		switch r.URL.Path {
		case "/v2/Services/VA1/Verifications":
			w.WriteHeader(201)
			w.Write([]byte(`{"status":"pending"}`))
		case "/v2/Services/VA1/VerificationCheck":
			status := "pending"
			if r.PostForm.Get("Code") == "123456" {
				status = "approved"
			}
			w.Write([]byte(`{"status":"` + status + `"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	v := &twilioVerifier{accountSID: "AC1", authToken: "secret", serviceSID: "VA1", baseURL: fake.URL, client: fake.Client()}
	ctx := context.Background()
	if err := v.Start(ctx, "+33612345678"); err != nil {
		t.Fatal(err)
	}
	if seen[0].Get("To") != "+33612345678" || seen[0].Get("Channel") != "sms" {
		t.Fatalf("start form = %v", seen[0])
	}
	if ok, err := v.Check(ctx, "+33612345678", "000000"); ok || err != nil {
		t.Fatalf("wrong code: %v %v", ok, err)
	}
	if ok, err := v.Check(ctx, "+33612345678", "123456"); !ok || err != nil {
		t.Fatalf("right code: %v %v", ok, err)
	}
	v.authToken = "wrong"
	if err := v.Start(ctx, "+33612345678"); err == nil {
		t.Fatal("provider error hidden")
	}
}

func TestBots(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	c.expect(403, "missing_permissions", c.call("POST", "/v1/bots", c.tok("bob"), map[string]any{"name": "Robot"}, nil))
	c.expect(400, "invalid_name", c.call("POST", "/v1/bots", c.owner, map[string]any{"name": "a@b"}, nil))
	var bot botJSON
	c.expect(201, "", c.call("POST", "/v1/bots", c.owner, map[string]any{"name": "Robot"}, &bot))
	if !bot.Member.Bot || bot.Member.DisplayName != "Robot" || len(bot.Token) < 20 {
		t.Fatalf("bot = %+v", bot)
	}
	// Bots use the same API with their token, and skip the rules screen.
	c.expect(200, "", c.call("PATCH", "/v1/server", c.owner, map[string]any{"rules": "Soyez sages."}, nil))
	var me memberJSON
	c.expect(200, "", c.call("GET", "/v1/members/@me", bot.Token, nil, &me))
	if me.ID != bot.Member.ID || !me.Bot {
		t.Fatalf("bot @me = %+v", me)
	}
	c.post(bot.Token, gen, map[string]any{"content": "bip bip"})
	var list []botJSON
	c.expect(200, "", c.call("GET", "/v1/bots", c.owner, nil, &list))
	if len(list) != 1 || list[0].Token != "" {
		t.Fatalf("bots = %+v", list)
	}
	// Roles give bots their powers; a manager cannot take over a bot ranked above them.
	high := c.createRole(c.owner, "Haut", "manage_server")
	low := c.createRole(c.owner, "Bas", "manage_server")
	c.expect(200, "", c.call("PATCH", fmt.Sprint("/v1/roles/", high), c.owner, map[string]any{"position": 2}, nil))
	c.expect(200, "", c.assign(c.owner, bot.Member.ID, high))
	c.expect(200, "", c.assign(c.owner, c.id("bob"), low))
	c.expect(403, "role_hierarchy", c.call("POST", "/v1/bots/"+bot.Member.ID+"/token", c.tok("bob"), nil, nil))

	var reset botJSON
	c.expect(200, "", c.call("POST", "/v1/bots/"+bot.Member.ID+"/token", c.owner, nil, &reset))
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", bot.Token, nil, nil))
	c.expect(200, "", c.call("GET", "/v1/members/@me", reset.Token, nil, nil))
	c.expect(204, "", c.call("DELETE", "/v1/bots/"+bot.Member.ID, c.owner, nil, nil))
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", reset.Token, nil, nil))
	if got := c.auditActions(c.owner, "?target_id="+bot.Member.ID); fmt.Sprint(got) != fmt.Sprint([]string{auditBotDelete, auditBotTokenReset, auditMemberRoleAdd, auditBotCreate}) {
		t.Fatalf("bot audit = %v", got)
	}
}
