package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestPasswordReset(t *testing.T) {
	e := newEnv(t)
	lr, _ := e.registerVerified("alice@example.com", "alice", "ancien-mot-de-passe")
	other := e.login2("alice@example.com", "ancien-mot-de-passe", "second")

	// Unknown addresses get the same answer.
	e.expect(202, "", e.call("POST", "/v1/auth/forgot-password", "", map[string]string{"email": "personne@example.com"}, nil))
	e.expect(202, "", e.call("POST", "/v1/auth/forgot-password", "", map[string]string{"email": "alice@example.com"}, nil))
	code := e.mail.code(t, "alice@example.com")
	reset := func(code, password string) result {
		return e.call("POST", "/v1/auth/reset-password", "", map[string]string{"email": "alice@example.com", "code": code, "password": password}, nil)
	}
	e.expect(400, "weak_password", reset(code, "court"))
	e.expect(400, "invalid_code", reset("000000", "nouveau-mot-de-passe"))
	e.expect(204, "", reset(code, "nouveau-mot-de-passe"))
	e.expect(400, "invalid_code", reset(code, "encore-un-autre-mdp")) // single use

	// Every session is closed; only the new password works.
	for _, tok := range []string{lr.SessionToken, other.SessionToken} {
		e.expect(401, "unauthorized", e.call("GET", "/v1/me", tok, nil, nil))
	}
	_, pub := deviceKey(t)
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "ancien-mot-de-passe", "device_key": pub}, nil))
	e.login2("alice@example.com", "nouveau-mot-de-passe", "après")
}

func TestPasswordResetNeeds2FA(t *testing.T) {
	e := newEnv(t)
	lr, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	var setup struct{ Secret string }
	e.expect(200, "", e.call("POST", "/v1/me/2fa/setup", lr.SessionToken, map[string]string{"password": "mot-de-passe-bob"}, &setup))
	c, _ := totpCode(setup.Secret, totpStep(e.clock))
	e.expect(200, "", e.call("POST", "/v1/me/2fa/enable", lr.SessionToken, map[string]string{"code": c}, nil))
	e.clock = e.clock.Add(totpPeriod * time.Second)

	e.expect(202, "", e.call("POST", "/v1/auth/forgot-password", "", map[string]string{"email": "bob@example.com"}, nil))
	code := e.mail.code(t, "bob@example.com")
	body := map[string]string{"email": "bob@example.com", "code": code, "password": "nouveau-mdp-bob"}
	e.expect(401, "mfa_required", e.call("POST", "/v1/auth/reset-password", "", body, nil))
	body["totp_code"], _ = totpCode(setup.Secret, totpStep(e.clock))
	e.expect(204, "", e.call("POST", "/v1/auth/reset-password", "", body, nil))
}

func TestChangePasswordEmailPseudo(t *testing.T) {
	e := newEnv(t)
	lr, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	other := e.login2("carol@example.com", "mot-de-passe-carol", "second")
	tok := lr.SessionToken

	// Password: the current one is required; other sessions close, this one stays.
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/me/password", tok, map[string]string{"current_password": "faux-mot-de-passe", "new_password": "nouveau-mdp-carol"}, nil))
	e.expect(204, "", e.call("POST", "/v1/me/password", tok, map[string]string{"current_password": "mot-de-passe-carol", "new_password": "nouveau-mdp-carol"}, nil))
	e.expect(200, "", e.call("GET", "/v1/me", tok, nil, nil))
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", other.SessionToken, nil, nil))

	// Email: confirmed with a code sent to the new address; the old one is told.
	e.registerVerified("pris@example.com", "pris", "mot-de-passe-pris")
	e.expect(409, "email_taken", e.call("POST", "/v1/me/email", tok, map[string]string{"password": "nouveau-mdp-carol", "new_email": "pris@example.com"}, nil))
	e.expect(202, "", e.call("POST", "/v1/me/email", tok, map[string]string{"password": "nouveau-mdp-carol", "new_email": "Carol.New@Example.com"}, nil))
	var me userResp
	e.expect(200, "", e.call("POST", "/v1/me/email/confirm", tok, map[string]string{"code": e.mail.code(t, "carol.new@example.com")}, &me))
	if me.Email != "carol.new@example.com" || !me.EmailVerified {
		t.Fatalf("me = %+v", me)
	}
	if !strings.Contains(e.mail.last["carol@example.com"], "remplacée") {
		t.Fatal("old address not warned")
	}
	e.login2("carol.new@example.com", "nouveau-mdp-carol", "nouvelle adresse")

	// Pseudo: unique, once a day; the account ID (identity) does not change.
	e.expect(409, "pseudo_taken", e.call("PATCH", "/v1/me", tok, map[string]string{"pseudo": "PRIS"}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/me", tok, map[string]string{"pseudo": "caroline"}, &me))
	if me.Handle != "caroline@id.test" || me.ID != lr.User.ID {
		t.Fatalf("me = %+v", me)
	}
	e.expect(429, "pseudo_change_too_soon", e.call("PATCH", "/v1/me", tok, map[string]string{"pseudo": "caro"}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/me", tok, map[string]string{"pseudo": "Caroline"}, nil)) // same name, other case
	e.clock = e.clock.Add(25 * time.Hour)
	e.expect(200, "", e.call("PATCH", "/v1/me", tok, map[string]string{"pseudo": "caro"}, nil))
	// The old pseudo is free again.
	e.registerVerified("autre@example.com", "caroline", "mot-de-passe-autre")
}

func TestDeleteAccount(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.befriend(alice, bob, "bob")
	e.expect(200, "", e.call("PUT", "/v1/me/presence", alice.SessionToken, map[string]string{"status": "dnd"}, nil))
	e.expect(200, "", e.call("POST", "/v1/dms", alice.SessionToken, map[string]string{"user_id": bob.User.ID}, nil))

	e.expect(401, "invalid_credentials", e.call("DELETE", "/v1/me", alice.SessionToken, map[string]string{"password": "mauvais"}, nil))
	e.expect(204, "", e.call("DELETE", "/v1/me", alice.SessionToken, map[string]string{"password": "mot-de-passe-alice"}, nil))
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", alice.SessionToken, nil, nil))

	// Nothing is left of alice.
	for _, table := range []string{"users", "sessions", "friendships", "conversation_members", "direct_pairs"} {
		var n int
		e.srv.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+map[string]string{
			"users": "id = ?", "sessions": "user_id = ?", "friendships": "user_a = ?1 OR user_b = ?1",
			"conversation_members": "user_id = ?", "direct_pairs": "user_a = ?1 OR user_b = ?1"}[table],
			alice.User.ID).Scan(&n)
		if n != 0 {
			t.Fatalf("%d row(s) of alice left in %s", n, table)
		}
	}
	var friends map[string][]publicUser
	e.expect(200, "", e.call("GET", "/v1/friends", bob.SessionToken, nil, &friends))
	if len(friends["friends"]) != 0 {
		t.Fatal("deleted account still a friend")
	}
	// Email and pseudo can be used again (by a new identity).
	again, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-neuf")
	if again.User.ID == alice.User.ID {
		t.Fatal("a new account reused the old identity")
	}
}

func TestProfileAndAvatar(t *testing.T) {
	e := newEnv(t)
	lr, _ := e.registerVerified("dave@example.com", "dave", "mot-de-passe-dave")
	var p profileJSON
	e.expect(200, "", e.call("PATCH", "/v1/me/profile", lr.SessionToken, map[string]string{"bio": "  Joueur de tarot.  "}, &p))
	if p.Bio != "Joueur de tarot." || p.AvatarURL != nil {
		t.Fatalf("profile = %+v", p)
	}
	e.expect(400, "invalid_bio", e.call("PATCH", "/v1/me/profile", lr.SessionToken, map[string]string{"bio": strings.Repeat("x", 501)}, nil))

	put := func(data []byte) (*http.Response, []byte) {
		req, _ := http.NewRequest("PUT", e.http.URL+"/v1/me/avatar", bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+lr.SessionToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}
	if resp, _ := put([]byte("<svg onload=alert(1)>")); resp.StatusCode != 400 {
		t.Fatalf("non-image accepted: %d", resp.StatusCode)
	}
	if resp, _ := put(bytes.Repeat([]byte{0}, maxAvatarBytes+1)); resp.StatusCode != 413 {
		t.Fatalf("huge avatar: %d", resp.StatusCode)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...)
	resp, body := put(png)
	json.Unmarshal(body, &p)
	if resp.StatusCode != 200 || p.AvatarURL == nil {
		t.Fatalf("avatar upload: %d %s", resp.StatusCode, body)
	}
	// Public: no session needed.
	e.expect(200, "", e.call("GET", "/v1/users/"+lr.User.ID+"/profile", "", nil, &p))
	got, err := http.Get(e.http.URL + *p.AvatarURL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if got.Header.Get("Content-Type") != "image/png" || !bytes.Equal(data, png) || got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("avatar served as %v", got.Header)
	}
	e.expect(204, "", e.call("DELETE", "/v1/me/avatar", lr.SessionToken, nil, nil))
	e.expect(404, "not_found", e.call("GET", "/v1/users/"+lr.User.ID+"/avatar", "", nil, nil))
}

func TestBlocks(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.befriend(alice, bob, "bob")

	e.expect(200, "", e.call("POST", "/v1/blocks", alice.SessionToken, map[string]string{"pseudo": "bob"}, nil))
	var friends map[string][]publicUser
	e.expect(200, "", e.call("GET", "/v1/friends", bob.SessionToken, nil, &friends))
	if len(friends["friends"]) != 0 {
		t.Fatal("blocking kept the friendship")
	}
	// bob cannot tell he is blocked: alice looks like she does not exist.
	e.expect(404, "not_found", e.call("POST", "/v1/friends", bob.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	e.expect(403, "blocked", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "bob"}, nil))
	e.expect(403, "not_friends", e.call("POST", "/v1/dms", bob.SessionToken, map[string]string{"user_id": alice.User.ID}, nil))
	var blocks []publicUser
	e.expect(200, "", e.call("GET", "/v1/blocks", alice.SessionToken, nil, &blocks))
	if len(blocks) != 1 || blocks[0].ID != bob.User.ID {
		t.Fatalf("blocks = %+v", blocks)
	}
	e.expect(204, "", e.call("DELETE", "/v1/blocks/"+bob.User.ID, alice.SessionToken, nil, nil))
	e.befriend(bob, alice, "alice")
}

func TestPresence(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.befriend(alice, bob, "bob")
	presenceOfAlice := func() string {
		var friends map[string][]publicUser
		e.expect(200, "", e.call("GET", "/v1/friends", bob.SessionToken, nil, &friends))
		return friends["friends"][0].Presence
	}
	if got := presenceOfAlice(); got != presenceOffline {
		t.Fatalf("no device connected: %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(tok string) *websocket.Conn {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.http.URL, "http")+"/v1/gateway", nil)
		if err != nil {
			t.Fatal(err)
		}
		wsjson.Write(ctx, c, map[string]string{"op": "auth", "token": tok})
		var ready struct{ T string }
		if err := wsjson.Read(ctx, c, &ready); err != nil || ready.T != "READY" { // registered: events from now on reach us
			t.Fatalf("READY: %v %v", ready.T, err)
		}
		return c
	}
	next := func(c *websocket.Conn, typ string) map[string]any {
		for {
			var ev struct {
				T string
				D map[string]any
			}
			if err := wsjson.Read(ctx, c, &ev); err != nil {
				t.Fatalf("waiting for %s: %v", typ, err)
			}
			if ev.T == typ {
				return ev.D
			}
		}
	}
	bobConn := dial(bob.SessionToken)
	defer bobConn.CloseNow()
	aliceConn := dial(alice.SessionToken)
	if d := next(bobConn, "PRESENCE_UPDATE"); d["user_id"] != alice.User.ID || d["status"] != "online" {
		t.Fatalf("PRESENCE_UPDATE = %v", d)
	}
	e.expect(200, "", e.call("PUT", "/v1/me/presence", alice.SessionToken, map[string]string{"status": "dnd"}, nil))
	if d := next(bobConn, "PRESENCE_UPDATE"); d["status"] != "dnd" || presenceOfAlice() != "dnd" {
		t.Fatalf("dnd: %v", d)
	}
	e.expect(200, "", e.call("PUT", "/v1/me/presence", alice.SessionToken, map[string]string{"status": "invisible"}, nil))
	if d := next(bobConn, "PRESENCE_UPDATE"); d["status"] != presenceOffline {
		t.Fatalf("invisible shows %v", d)
	}
	e.expect(200, "", e.call("PUT", "/v1/me/presence", alice.SessionToken, map[string]string{"status": "online"}, nil))
	next(bobConn, "PRESENCE_UPDATE")
	aliceConn.Close(websocket.StatusNormalClosure, "")
	if d := next(bobConn, "PRESENCE_UPDATE"); d["status"] != presenceOffline {
		t.Fatalf("after disconnecting: %v", d)
	}
	e.expect(400, "invalid_status", e.call("PUT", "/v1/me/presence", alice.SessionToken, map[string]string{"status": "absent"}, nil))
}

func TestAdminDisableAndDisabledList(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	a := NewAdmin(e.srv.db)
	ctx := context.Background()
	if _, err := a.SetDisabled(ctx, "alice", true, "", "opérateur"); err == nil {
		t.Fatal("disabled without a reason")
	}
	if _, err := a.SetDisabled(ctx, "ALICE@example.com", true, "réquisition 2026-42", "opérateur"); err != nil {
		t.Fatal(err)
	}
	e.expect(403, "account_disabled", e.call("GET", "/v1/me", alice.SessionToken, nil, nil))
	e.expect(404, "not_found", e.call("GET", "/v1/users/"+alice.User.ID+"/profile", "", nil, nil))
	var list struct {
		Issuer   string
		Accounts []struct {
			H string `json:"h"`
		}
	}
	e.expect(200, "", e.call("GET", "/v1/disabled-accounts", "", nil, &list))
	if list.Issuer != "id.test" || len(list.Accounts) != 1 || list.Accounts[0].H != idtoken.AccountHash(alice.User.ID) {
		t.Fatalf("list = %+v", list)
	}
	if _, err := a.SetDisabled(ctx, alice.User.ID, false, "levée 2026-43", "opérateur"); err != nil {
		t.Fatal(err)
	}
	e.expect(200, "", e.call("GET", "/v1/me", alice.SessionToken, nil, nil)) // devices kept
	log, err := a.Log(ctx, 10)
	if err != nil || len(log) != 2 || log[0].Action != "account_enable" || log[1].Reason != "réquisition 2026-42" {
		t.Fatalf("log = %+v, %v", log, err)
	}
}

func TestSigningKeyRotation(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, _ := secret.LoadOrCreateKey(filepath.Join(dir, "signing.key"))
	oldToken, _, _ := idtoken.NewSigner("id.test", old).Issue("u1", "alice@id.test", old.Public().(ed25519.PublicKey), time.Hour, time.Now())

	oldKID, newKID, err := NewAdmin(db).RotateSigningKey(context.Background(), dir, "rotation annuelle", "opérateur")
	if err != nil || oldKID == newKID {
		t.Fatalf("rotate: %v %v %v", oldKID, newKID, err)
	}
	key, _ := secret.LoadOrCreateKey(filepath.Join(dir, "signing.key"))
	if idtoken.KeyID(key.Public().(ed25519.PublicKey)) != newKID {
		t.Fatal("signing.key not replaced")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, retiredKeysFile)); bytes.Contains(data, []byte(base64Seed(old))) {
		t.Fatal("the retired private key was kept")
	}
	// Tokens signed with the old key stay valid while they can be alive, then the key is dropped.
	retired, err := liveRetiredKeys(dir, time.Hour, time.Now())
	if err != nil || len(retired) != 1 {
		t.Fatalf("retired = %v, %v", retired, err)
	}
	ks := idtoken.NewSigner("id.test", key, retired...).KeySet()
	if _, err := idtoken.Verify(oldToken, ks, time.Now()); err != nil {
		t.Fatalf("old token rejected after rotation: %v", err)
	}
	if later, _ := liveRetiredKeys(dir, time.Hour, time.Now().Add(3*time.Hour)); len(later) != 0 {
		t.Fatal("retired key published forever")
	}
}

func base64Seed(k ed25519.PrivateKey) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.Encode(k.Seed())
	return strings.Trim(strings.TrimSpace(b.String()), `"`)
}

// Every ended session (logout, revocation, idle expiry, account deletion)
// is published, hashed, so that community servers end the sessions its
// device opened there; unused sessions end after QUAREL_SESSION_IDLE.
func TestEndedDevicesAndIdleSessions(t *testing.T) {
	e := newEnv(t)
	ended := func() map[string]bool {
		var list struct {
			Devices []struct {
				H string `json:"h"`
			} `json:"ended_devices"`
		}
		e.expect(200, "", e.call("GET", "/v1/disabled-accounts", "", nil, &list))
		out := map[string]bool{}
		for _, d := range list.Devices {
			out[d.H] = true
		}
		return out
	}
	deviceOf := func(token string) string {
		var key string
		e.srv.db.QueryRow(`SELECT device_key FROM sessions WHERE token_hash = ?`, sha256Hex(token)).Scan(&key)
		return key
	}

	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	key := deviceOf(alice.SessionToken)
	if ended()[idtoken.DeviceHash(key)] {
		t.Fatal("a live session is listed as ended")
	}
	e.expect(204, "", e.call("POST", "/v1/auth/logout", alice.SessionToken, nil, nil))
	if !ended()[idtoken.DeviceHash(key)] {
		t.Fatal("logout not published")
	}

	// Idle sessions stop working, then are removed and published.
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	bobKey := deviceOf(bob.SessionToken)
	e.expect(200, "", e.call("GET", "/v1/me", bob.SessionToken, nil, nil))
	e.clock = e.clock.Add(e.srv.cfg.SessionIdle + time.Minute)
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", bob.SessionToken, nil, nil))
	e.srv.cleanup(context.Background())
	var n int
	e.srv.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, sha256Hex(bob.SessionToken)).Scan(&n)
	if n != 0 {
		t.Fatal("idle session not removed")
	}
	if !ended()[idtoken.DeviceHash(bobKey)] {
		t.Fatal("idle expiry not published")
	}

	// Deleting an account ends its sessions too (by cascade).
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	carolKey := deviceOf(carol.SessionToken)
	e.expect(204, "", e.call("DELETE", "/v1/me", carol.SessionToken, map[string]string{"password": "mot-de-passe-carol"}, nil))
	if !ended()[idtoken.DeviceHash(carolKey)] {
		t.Fatal("account deletion not published")
	}
}
