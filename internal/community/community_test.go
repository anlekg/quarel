package community

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/realtime"
	"github.com/anlekg/quarel/internal/tlsconf"
	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const testIssuer = "id.test"

type staticKeys map[string]idtoken.KeySet

func (k staticKeys) KeySet(_ context.Context, issuer string, _ bool) (idtoken.KeySet, error) {
	ks, ok := k[issuer]
	if !ok {
		return ks, fmt.Errorf("unknown issuer %s", issuer)
	}
	return ks, nil
}

type testEnv struct {
	t      *testing.T
	srv    *Server
	http   *httptest.Server
	signer *idtoken.Signer
	clock  time.Time
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, serverKey, _ := ed25519.GenerateKey(nil)
	_, idKey, _ := ed25519.GenerateKey(nil)
	e := &testEnv{t: t, signer: idtoken.NewSigner(testIssuer, idKey), clock: time.Now()}
	cfg := Config{Name: "Test", TrustedIssuers: []string{testIssuer}, DataDir: t.TempDir()}
	e.srv = New(cfg, db, serverKey, staticKeys{testIssuer: e.signer.KeySet()})
	e.srv.now = func() time.Time { return e.clock }
	if err := e.srv.seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.http = httptest.NewServer(e.srv.Handler())
	t.Cleanup(func() { e.http.Close(); e.srv.Close() })
	return e
}

type result struct {
	status int
	code   string
}

func (e *testEnv) call(method, path, token string, body, out any) result {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.http.URL+path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var er struct{ Error httpapi.Error }
		json.NewDecoder(resp.Body).Decode(&er)
		return result{resp.StatusCode, er.Error.Code}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: decoding response: %v", method, path, err)
		}
	}
	return result{resp.StatusCode, ""}
}

func (e *testEnv) expect(wantStatus int, wantCode string, got result) {
	e.t.Helper()
	if got.status != wantStatus || got.code != wantCode {
		e.t.Fatalf("got %d %q, want %d %q", got.status, got.code, wantStatus, wantCode)
	}
}

func TestHostNames(t *testing.T) {
	s := &Server{names: hostNames(tlsconf.Config{Mode: tlsconf.Off, Hosts: []string{"Chez-Moi.example.", "*.dyn.example", "[2001:db8::1]"}})}
	for host, want := range map[string]bool{
		"chez-moi.example": true, "CHEZ-MOI.EXAMPLE:8090": true, "localhost": true, "127.0.0.1": true, "::1": true,
		"a.dyn.example": true, "a.b.dyn.example": false, "dyn.example": false, "2001:db8::1": true, "[2001:db8::1]:443": true,
		"evil.example": false, "": false,
	} {
		if got := s.servesHost(host); got != want {
			t.Errorf("servesHost(%q) = %v, want %v", host, got, want)
		}
	}
	acme := &Server{names: hostNames(tlsconf.Config{Mode: tlsconf.ACME, Domain: "quarel.example"})}
	if !acme.servesHost("quarel.example") || acme.servesHost("other.example") {
		t.Error("acme domain")
	}
}

// user is a portable identity with a device key, as the Identity service would issue.
type user struct {
	sub, handle string
	device      ed25519.PrivateKey
	signer      *idtoken.Signer
}

func (e *testEnv) newUser(pseudo string) *user {
	_, device, _ := ed25519.GenerateKey(nil)
	return &user{sub: "sub-" + pseudo, handle: pseudo + "@" + testIssuer, device: device, signer: e.signer}
}

func (u *user) token(now time.Time, ttl time.Duration) string {
	tok, _, err := u.signer.Issue(u.sub, u.handle, u.device.Public().(ed25519.PublicKey), ttl, now)
	if err != nil {
		panic(err)
	}
	return tok
}

type loginOpts struct{ invite, claim, token string }

type loginResp struct {
	SessionToken string     `json:"session_token"`
	Member       memberJSON `json:"member"`
	Joined       bool       `json:"joined"`
}

// login runs challenge → proof → login and returns the result.
func (e *testEnv) login(u *user, o loginOpts, out *loginResp) result {
	e.t.Helper()
	var ch struct {
		ServerID string `json:"server_id"`
		Nonce    string `json:"nonce"`
	}
	e.expect(200, "", e.call("POST", "/v1/auth/challenge", "", nil, &ch))
	if ch.ServerID != e.srv.id {
		e.t.Fatalf("challenge server_id = %q", ch.ServerID)
	}
	tok := o.token
	if tok == "" {
		tok = u.token(e.clock, time.Hour)
	}
	return e.call("POST", "/v1/auth/login", "", map[string]string{
		"identity_token": tok,
		"nonce":          ch.Nonce,
		"proof":          idtoken.SignProofV2(u.device, ch.ServerID, ch.Nonce, "127.0.0.1", idtoken.TLSAuthority),
		"host":           "127.0.0.1",
		"tls":            idtoken.TLSAuthority,
		"invite":         o.invite,
		"claim":          o.claim,
	}, out)
}

func (e *testEnv) mustLogin(u *user, o loginOpts) loginResp {
	e.t.Helper()
	var lr loginResp
	e.expect(200, "", e.login(u, o, &lr))
	return lr
}

// setupOwner claims the server and returns the owner's session.
func (e *testEnv) setupOwner() (*user, string) {
	e.t.Helper()
	code, err := e.srv.PrepareClaim(context.Background())
	if err != nil || code == "" {
		e.t.Fatalf("PrepareClaim: %q %v", code, err)
	}
	alice := e.newUser("alice")
	lr := e.mustLogin(alice, loginOpts{claim: code})
	if !lr.Member.Owner {
		e.t.Fatal("claimer is not owner")
	}
	return alice, lr.SessionToken
}

func (e *testEnv) invite(token string, body map[string]any) string {
	e.t.Helper()
	var inv struct{ Code string }
	e.expect(201, "", e.call("POST", "/v1/invites", token, body, &inv))
	return inv.Code
}

func TestClaimAndJoin(t *testing.T) {
	e := newEnv(t)
	code, _ := e.srv.PrepareClaim(context.Background())
	bob := e.newUser("bob")
	e.expect(403, "invalid_claim", e.login(bob, loginOpts{claim: "wrong"}, nil))

	alice := e.newUser("alice")
	owner := e.mustLogin(alice, loginOpts{claim: code})
	if !owner.Member.Owner || !owner.Joined {
		t.Fatalf("owner = %+v", owner)
	}
	e.expect(403, "invalid_claim", e.login(bob, loginOpts{claim: code}, nil))
	if again, _ := e.srv.PrepareClaim(context.Background()); again != "" {
		t.Fatal("new claim code issued although the server has an owner")
	}

	// Private by default: joining needs an invite.
	e.expect(403, "invite_required", e.login(bob, loginOpts{}, nil))
	e.expect(403, "invalid_invite", e.login(bob, loginOpts{invite: "nope"}, nil))
	single := e.invite(owner.SessionToken, map[string]any{"max_uses": 1})
	lr := e.mustLogin(bob, loginOpts{invite: single})
	if !lr.Joined || lr.Member.Owner || lr.Member.DisplayName != "bob" {
		t.Fatalf("bob = %+v", lr)
	}
	// Members log in again without an invite.
	if lr := e.mustLogin(bob, loginOpts{}); lr.Joined {
		t.Fatal("existing member reported as joining again")
	}
	carol := e.newUser("carol")
	e.expect(403, "invalid_invite", e.login(carol, loginOpts{invite: single}, nil))

	short := e.invite(owner.SessionToken, map[string]any{"expires_in": 60})
	e.clock = e.clock.Add(2 * time.Minute)
	e.expect(403, "invalid_invite", e.login(carol, loginOpts{invite: short}, nil))

	// Invites: members see their own, the owner sees all; only creator/owner can revoke.
	bobSess := e.mustLogin(bob, loginOpts{}).SessionToken
	bobInv := e.invite(bobSess, map[string]any{"expires_in": 0})
	var list []invite
	e.expect(200, "", e.call("GET", "/v1/invites", bobSess, nil, &list))
	if len(list) != 1 || list[0].Code != bobInv || list[0].ExpiresAt != nil {
		t.Fatalf("bob's invites = %+v", list)
	}
	e.expect(200, "", e.call("GET", "/v1/invites", owner.SessionToken, nil, &list))
	if len(list) != 3 {
		t.Fatalf("owner sees %d invites", len(list))
	}
	e.expect(404, "not_found", e.call("DELETE", "/v1/invites/"+single, bobSess, nil, nil))
	e.expect(204, "", e.call("DELETE", "/v1/invites/"+bobInv, owner.SessionToken, nil, nil))

	// Public server: no invite needed.
	e.expect(403, "missing_permissions", e.call("PATCH", "/v1/server", bobSess, map[string]string{"access": "public"}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/server", owner.SessionToken, map[string]string{"access": "public", "name": "Chez Alice"}, nil))
	e.mustLogin(carol, loginOpts{})
	var info serverInfo
	e.expect(200, "", e.call("GET", "/v1/server", "", nil, &info))
	if info.Name != "Chez Alice" || info.Access != "public" || info.MemberCount != 3 || info.ID != e.srv.id {
		t.Fatalf("info = %+v", info)
	}
}

func TestLoginSecurity(t *testing.T) {
	e := newEnv(t)
	e.setupOwner()
	e.srv.db.Exec(`UPDATE settings SET value = 'public' WHERE key = 'access'`)
	bob := e.newUser("bob")

	challenge := func() string {
		var ch struct{ Nonce string }
		e.expect(200, "", e.call("POST", "/v1/auth/challenge", "", nil, &ch))
		return ch.Nonce
	}
	login := func(token, nonce, proof string) result {
		return e.call("POST", "/v1/auth/login", "", map[string]string{"identity_token": token, "nonce": nonce, "proof": proof, "host": "127.0.0.1", "tls": idtoken.TLSAuthority}, nil)
	}
	sign := func(device ed25519.PrivateKey, sid, nonce string) string {
		return idtoken.SignProofV2(device, sid, nonce, "127.0.0.1", idtoken.TLSAuthority)
	}

	// An app older than 0.3.0 (v1 proof, no host) is told to update.
	n := challenge()
	e.expect(426, "client_outdated", e.call("POST", "/v1/auth/login", "", map[string]string{"identity_token": bob.token(e.clock, time.Hour), "nonce": n, "proof": sign(bob.device, e.srv.id, n)}, nil))

	// A challenge can only be used once.
	n = challenge()
	e.expect(200, "", login(bob.token(e.clock, time.Hour), n, sign(bob.device, e.srv.id, n)))
	e.expect(401, "invalid_nonce", login(bob.token(e.clock, time.Hour), n, sign(bob.device, e.srv.id, n)))

	// A proof made for another server (relay attack) is refused.
	n = challenge()
	e.expect(401, "invalid_proof", login(bob.token(e.clock, time.Hour), n, sign(bob.device, "other-server", n)))

	// v2 proofs name the host the client connected to. A malicious server
	// with an ordinary certificate for its own name relaying the login here
	// is refused; so is a proof whose host was changed on the way.
	loginV2 := func(nonce, proof, host, mode string) result {
		return e.call("POST", "/v1/auth/login", "", map[string]string{"identity_token": bob.token(e.clock, time.Hour), "nonce": nonce, "proof": proof, "host": host, "tls": mode}, nil)
	}
	n = challenge()
	e.expect(403, "wrong_host", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "evil.example", idtoken.TLSAuthority), "evil.example", idtoken.TLSAuthority))
	n = challenge()
	e.expect(401, "invalid_proof", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "evil.example", idtoken.TLSAuthority), "127.0.0.1", idtoken.TLSAuthority))
	n = challenge()
	e.expect(401, "invalid_proof", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "evil.example", idtoken.TLSAuthority), "evil.example", idtoken.TLSBinding))
	// This test server has no certificate bound to its identity: a client
	// cannot have checked one, the login came through someone else.
	n = challenge()
	e.expect(403, "wrong_host", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "evil.example", idtoken.TLSBinding), "evil.example", idtoken.TLSBinding))
	n = challenge()
	e.expect(200, "", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "LOCALHOST:8090", idtoken.TLSAuthority), "localhost", idtoken.TLSAuthority))
	// With its bound certificate, any address will do: only this server can present it.
	e.srv.cfg.TLS.Mode = tlsconf.SelfSigned
	n = challenge()
	e.expect(200, "", loginV2(n, idtoken.SignProofV2(bob.device, e.srv.id, n, "203.0.113.7", idtoken.TLSBinding), "203.0.113.7", idtoken.TLSBinding))
	e.srv.cfg.TLS.Mode = ""

	// A stolen token without the device key is useless.
	_, thief, _ := ed25519.GenerateKey(nil)
	n = challenge()
	e.expect(401, "invalid_proof", login(bob.token(e.clock, time.Hour), n, sign(thief, e.srv.id, n)))

	// Expired challenge.
	n = challenge()
	e.clock = e.clock.Add(nonceTTL + time.Second)
	e.expect(401, "invalid_nonce", login(bob.token(e.clock, time.Hour), n, sign(bob.device, e.srv.id, n)))

	// Expired identity token.
	n = challenge()
	e.expect(401, "invalid_token", login(bob.token(e.clock.Add(-2*time.Hour), time.Hour), n, sign(bob.device, e.srv.id, n)))

	// Token from an Identity service this server does not trust.
	_, otherKey, _ := ed25519.GenerateKey(nil)
	mallory := &user{sub: "m", handle: "m@evil.test", device: bob.device, signer: idtoken.NewSigner("evil.test", otherKey)}
	n = challenge()
	e.expect(403, "untrusted_issuer", login(mallory.token(e.clock, time.Hour), n, sign(bob.device, e.srv.id, n)))

	// Token claiming a trusted issuer but signed with another key.
	forger := &user{sub: "sub-alice", handle: "alice@" + testIssuer, device: bob.device, signer: idtoken.NewSigner(testIssuer, otherKey)}
	n = challenge()
	e.expect(401, "invalid_token", login(forger.token(e.clock, time.Hour), n, sign(bob.device, e.srv.id, n)))

	// The server session ends when the identity token expires.
	sess := e.mustLogin(bob, loginOpts{}).SessionToken
	e.expect(200, "", e.call("GET", "/v1/members/@me", sess, nil, nil))
	e.clock = e.clock.Add(time.Hour + time.Second)
	e.expect(401, "unauthorized", e.call("GET", "/v1/members/@me", sess, nil, nil))

	e.expect(401, "unauthorized", e.call("GET", "/v1/channels", "", nil, nil))
}

func channelNamed(t *testing.T, list []channel, name string) channel {
	t.Helper()
	for _, c := range list {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no channel %q in %+v", name, list)
	return channel{}
}

func TestChannels(t *testing.T) {
	e := newEnv(t)
	_, owner := e.setupOwner()
	bob := e.mustLogin(e.newUser("bob"), loginOpts{invite: e.invite(owner, nil)}).SessionToken

	var list []channel
	e.expect(200, "", e.call("GET", "/v1/channels", bob, nil, &list))
	if len(list) != 4 {
		t.Fatalf("default channels = %+v", list)
	}
	general := channelNamed(t, list, "général")
	textCat := channelNamed(t, list, "Salons textuels")
	if general.Type != "text" || general.ParentID == nil || *general.ParentID != textCat.ID {
		t.Fatalf("général = %+v", general)
	}

	e.expect(403, "missing_permissions", e.call("POST", "/v1/channels", bob, map[string]any{"name": "x"}, nil))
	var cat, dev channel
	e.expect(201, "", e.call("POST", "/v1/channels", owner, map[string]any{"type": "category", "name": "Projets"}, &cat))
	e.expect(201, "", e.call("POST", "/v1/channels", owner, map[string]any{"name": "dev", "topic": "code", "parent_id": cat.ID}, &dev))
	if dev.Type != "text" || *dev.ParentID != cat.ID || dev.Position != 0 {
		t.Fatalf("dev = %+v", dev)
	}
	e.expect(400, "invalid_parent", e.call("POST", "/v1/channels", owner, map[string]any{"type": "category", "name": "sub", "parent_id": cat.ID}, nil))
	e.expect(400, "invalid_parent", e.call("POST", "/v1/channels", owner, map[string]any{"name": "x", "parent_id": general.ID}, nil))
	e.expect(400, "invalid_type", e.call("POST", "/v1/channels", owner, map[string]any{"type": "stage", "name": "x"}, nil))
	e.expect(400, "invalid_name", e.call("POST", "/v1/channels", owner, map[string]any{"name": "  "}, nil))

	e.expect(200, "", e.call("PATCH", fmt.Sprint("/v1/channels/", dev.ID), owner, map[string]any{"name": "développement", "parent_id": 0}, &dev))
	if dev.ParentID != nil || dev.Name != "développement" {
		t.Fatalf("moved dev = %+v", dev)
	}
	e.expect(200, "", e.call("PATCH", fmt.Sprint("/v1/channels/", dev.ID), owner, map[string]any{"parent_id": cat.ID}, nil))

	// Deleting a category keeps its channels, at the top level.
	e.expect(204, "", e.call("DELETE", fmt.Sprint("/v1/channels/", cat.ID), owner, nil, nil))
	e.expect(200, "", e.call("GET", "/v1/channels", bob, nil, &list))
	if d := channelNamed(t, list, "développement"); d.ParentID != nil {
		t.Fatalf("orphan = %+v", d)
	}
	e.expect(404, "not_found", e.call("DELETE", fmt.Sprint("/v1/channels/", cat.ID), owner, nil, nil))
}

func TestMessages(t *testing.T) {
	e := newEnv(t)
	_, owner := e.setupOwner()
	bobLogin := e.mustLogin(e.newUser("bob"), loginOpts{invite: e.invite(owner, nil)})
	bob, bobID := bobLogin.SessionToken, bobLogin.Member.ID

	var list []channel
	e.expect(200, "", e.call("GET", "/v1/channels", bob, nil, &list))
	general := channelNamed(t, list, "général")
	voice := channelNamed(t, list, "Général")
	path := fmt.Sprint("/v1/channels/", general.ID, "/messages")

	e.expect(400, "not_text_channel", e.call("POST", fmt.Sprint("/v1/channels/", voice.ID, "/messages"), bob, map[string]string{"content": "hi"}, nil))
	e.expect(400, "invalid_content", e.call("POST", path, bob, map[string]string{"content": "   "}, nil))
	e.expect(400, "invalid_content", e.call("POST", path, bob, map[string]string{"content": strings.Repeat("a", maxMessageLen+1)}, nil))

	var first message
	e.expect(201, "", e.call("POST", path, owner, map[string]string{"content": fmt.Sprintf("salut <@%s> et <@%s> @everyone <@aaaaaaaaaaaaaaaaaaaaaaaaaa>", bobID, bobID)}, &first))
	if len(first.Mentions) != 1 || first.Mentions[0] != bobID || !first.MentionEveryone {
		t.Fatalf("mentions = %+v everyone=%v", first.Mentions, first.MentionEveryone)
	}
	var plain message
	e.expect(201, "", e.call("POST", path, bob, map[string]string{"content": "email@everyone.com n'est pas une mention"}, &plain))
	if plain.MentionEveryone {
		t.Fatal("@everyone inside a word counted as a mention")
	}
	for i := range 8 {
		e.expect(201, "", e.call("POST", path, bob, map[string]string{"content": fmt.Sprint("msg ", i)}, nil))
	}

	// Pagination: latest page, then older, then newer, always chronological.
	var page []message
	e.expect(200, "", e.call("GET", path+"?limit=4", bob, nil, &page))
	if len(page) != 4 || page[0].Content != "msg 4" || page[3].Content != "msg 7" {
		t.Fatalf("latest page = %v", contents(page))
	}
	e.expect(200, "", e.call("GET", fmt.Sprint(path, "?limit=4&before=", page[0].ID), bob, nil, &page))
	if len(page) != 4 || page[0].Content != "msg 0" || page[3].Content != "msg 3" {
		t.Fatalf("older page = %v", contents(page))
	}
	e.expect(200, "", e.call("GET", fmt.Sprint(path, "?limit=4&before=", page[0].ID), bob, nil, &page))
	if len(page) != 2 || page[0].ID != first.ID || page[1].ID != plain.ID {
		t.Fatalf("oldest page = %v", contents(page))
	}
	if len(page[0].Mentions) != 1 {
		t.Fatalf("mentions not loaded in history: %+v", page[0])
	}
	e.expect(200, "", e.call("GET", fmt.Sprint(path, "?limit=2&after=", first.ID), bob, nil, &page))
	if len(page) != 2 || page[0].ID != plain.ID {
		t.Fatalf("newer page = %v", contents(page))
	}
	e.expect(400, "bad_request", e.call("GET", path+"?limit=1000", bob, nil, nil))

	// Edit: author only; mentions are recomputed.
	mpath := fmt.Sprint(path, "/", first.ID)
	e.expect(403, "forbidden", e.call("PATCH", mpath, bob, map[string]string{"content": "hack"}, nil))
	var edited message
	e.expect(200, "", e.call("PATCH", mpath, owner, map[string]string{"content": "corrigé"}, &edited))
	if edited.EditedAt == nil || len(edited.Mentions) != 0 || edited.MentionEveryone {
		t.Fatalf("edited = %+v", edited)
	}

	// Delete: author, or the owner for anyone's message.
	e.expect(403, "missing_permissions", e.call("DELETE", mpath, bob, nil, nil))
	e.expect(204, "", e.call("DELETE", fmt.Sprint(path, "/", plain.ID), owner, nil, nil))
	e.expect(204, "", e.call("DELETE", mpath, owner, nil, nil))
	e.expect(404, "not_found", e.call("DELETE", mpath, owner, nil, nil))
	e.expect(404, "not_found", e.call("GET", "/v1/channels/999/messages", bob, nil, nil))
}

func contents(msgs []message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

func TestMembersAndLeave(t *testing.T) {
	e := newEnv(t)
	_, owner := e.setupOwner()
	bobUser := e.newUser("bob")
	bob := e.mustLogin(bobUser, loginOpts{invite: e.invite(owner, nil)}).SessionToken

	var me memberJSON
	e.expect(200, "", e.call("PATCH", "/v1/members/@me", bob, map[string]any{"nickname": "Bobby"}, &me))
	if me.DisplayName != "Bobby" || me.Subject != bobUser.sub || me.Issuer != testIssuer {
		t.Fatalf("me = %+v", me)
	}
	e.expect(400, "invalid_nickname", e.call("PATCH", "/v1/members/@me", bob, map[string]any{"nickname": strings.Repeat("x", 33)}, nil))
	var members []memberJSON
	e.expect(200, "", e.call("GET", "/v1/members", bob, nil, &members))
	if len(members) != 2 || members[1].DisplayName != "Bobby" {
		t.Fatalf("members = %+v", members)
	}

	var list []channel
	e.expect(200, "", e.call("GET", "/v1/channels", bob, nil, &list))
	path := fmt.Sprint("/v1/channels/", channelNamed(t, list, "général").ID, "/messages")
	e.expect(201, "", e.call("POST", path, bob, map[string]string{"content": "au revoir"}, nil))

	e.expect(400, "owner_cannot_leave", e.call("DELETE", "/v1/members/@me", owner, nil, nil))
	e.expect(204, "", e.call("DELETE", "/v1/members/@me", bob, nil, nil))
	e.expect(401, "unauthorized", e.call("GET", "/v1/members/@me", bob, nil, nil))
	e.expect(200, "", e.call("GET", "/v1/members", owner, nil, &members))
	if len(members) != 1 {
		t.Fatalf("members after leave = %+v", members)
	}
	// Their messages stay, still attributed.
	var msgs []message
	e.expect(200, "", e.call("GET", path, owner, nil, &msgs))
	if len(msgs) != 1 || msgs[0].AuthorID != me.ID {
		t.Fatalf("messages after leave = %+v", msgs)
	}

	// Rejoining a private server needs a new invite, and keeps the same member ID.
	e.expect(403, "invite_required", e.login(bobUser, loginOpts{}, nil))
	again := e.mustLogin(bobUser, loginOpts{invite: e.invite(owner, nil)})
	if again.Member.ID != me.ID || !again.Joined || again.Member.Nickname != nil {
		t.Fatalf("rejoined = %+v", again.Member)
	}
}

func TestGateway(t *testing.T) {
	e := newEnv(t)
	_, owner := e.setupOwner()
	bobLogin := e.mustLogin(e.newUser("bob"), loginOpts{invite: e.invite(owner, nil)})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/v1/gateway"

	// Invalid session: closed with 4001.
	bad, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	wsjson.Write(ctx, bad, map[string]string{"op": "auth", "token": "nope"})
	if _, _, err := bad.Read(ctx); websocket.CloseStatus(err) != realtime.CloseInvalidSession {
		t.Fatalf("bad session: %v", err)
	}

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": bobLogin.SessionToken})
	var ready struct {
		T string
		D struct {
			Member   memberJSON
			Channels []channel
			Members  []memberJSON
		}
	}
	if err := wsjson.Read(ctx, conn, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.T != "READY" || ready.D.Member.ID != bobLogin.Member.ID || len(ready.D.Channels) != 4 || len(ready.D.Members) != 2 {
		t.Fatalf("READY = %+v", ready)
	}

	general := channelNamed(t, ready.D.Channels, "général")
	e.expect(201, "", e.call("POST", fmt.Sprint("/v1/channels/", general.ID, "/messages"), owner, map[string]string{"content": "bonjour bob"}, nil))
	var ev struct {
		T string
		D message
	}
	if err := wsjson.Read(ctx, conn, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.T != "MESSAGE_CREATE" || ev.D.Content != "bonjour bob" {
		t.Fatalf("event = %+v", ev)
	}

	// Leaving the server closes the connection.
	e.expect(204, "", e.call("DELETE", "/v1/members/@me", bobLogin.SessionToken, nil, nil))
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("close after leave: %v", err)
			}
			break
		}
	}
}
