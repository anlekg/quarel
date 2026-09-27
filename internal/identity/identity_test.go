package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

func init() {
	// Cheap hashing keeps the test suite fast; production parameters are unchanged.
	argonParams.time, argonParams.memory, argonParams.threads = 1, 1024, 1
}

type captureMailer struct {
	mu   sync.Mutex
	last map[string]string
}

func (m *captureMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last[to] = body
	return nil
}

var codeRe = regexp.MustCompile(`\b(\d{6})\b`)

func (m *captureMailer) code(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	match := codeRe.FindStringSubmatch(m.last[to])
	if match == nil {
		t.Fatalf("no code mailed to %s", to)
	}
	return match[1]
}

type testEnv struct {
	t     *testing.T
	srv   *Server
	http  *httptest.Server
	mail  *captureMailer
	clock time.Time
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	mail := &captureMailer{last: map[string]string{}}
	e := &testEnv{t: t, mail: mail, clock: time.Now()}
	cfg := Config{Issuer: "id.test", TokenTTL: time.Hour, DataDir: t.TempDir(), Limits: Limits{AuthFailuresPerIP: maxAuthFailures, AuthFailuresTotal: maxAuthFailuresAll}}
	e.srv = New(cfg, db, key, mail)
	e.srv.now = func() time.Time { return e.clock }
	e.http = httptest.NewServer(e.srv.Handler())
	t.Cleanup(func() { e.http.Close(); db.Close() })
	return e
}

// call sends a JSON request and decodes the JSON response into out (if non-nil).
// It returns the status and the API error code, if any.
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
		var er struct{ Error apiError }
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

type loginResp struct {
	SessionID    string   `json:"session_id"`
	SessionToken string   `json:"session_token"`
	User         userResp `json:"user"`
}

func deviceKey(t *testing.T) (ed25519.PrivateKey, string) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv, idtoken.EncodeKey(pub)
}

// registerVerified creates a verified account and returns a logged-in session.
func (e *testEnv) registerVerified(email, pseudo, password string) (loginResp, ed25519.PrivateKey) {
	e.t.Helper()
	e.expect(201, "", e.call("POST", "/v1/auth/register", "", map[string]string{"email": email, "pseudo": pseudo, "password": password}, nil))
	e.expect(204, "", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": email, "code": e.mail.code(e.t, email)}, nil))
	priv, pub := deviceKey(e.t)
	var lr loginResp
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", map[string]string{"login": email, "password": password, "device_name": "test", "device_key": pub}, &lr))
	return lr, priv
}

func TestRegistrationAndLogin(t *testing.T) {
	e := newEnv(t)
	_, pub := deviceKey(t)
	reg := map[string]string{"email": "Alice@Example.com", "pseudo": "Alice", "password": "correct horse battery"}

	e.expect(201, "", e.call("POST", "/v1/auth/register", "", reg, nil))
	// Registering again over an unverified account replaces it (nobody could sign in with it).
	e.expect(201, "", e.call("POST", "/v1/auth/register", "", reg, nil))
	e.expect(409, "pseudo_taken", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "b@example.com", "pseudo": "ALICE", "password": "correct horse battery"}, nil))
	e.expect(400, "weak_password", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "c@example.com", "pseudo": "carol", "password": "short"}, nil))
	e.expect(400, "invalid_pseudo", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "c@example.com", "pseudo": "a b", "password": "correct horse battery"}, nil))
	e.expect(400, "invalid_email", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "nope", "pseudo": "carol", "password": "correct horse battery"}, nil))

	login := map[string]string{"login": "alice", "password": "correct horse battery", "device_key": pub}
	e.expect(403, "email_not_verified", e.call("POST", "/v1/auth/login", "", login, nil))

	e.expect(400, "invalid_code", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "alice@example.com", "code": "000000x"}, nil))
	code := e.mail.code(t, "alice@example.com")
	e.expect(204, "", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "alice@example.com", "code": code}, nil))

	// A verified address: same answer as for a new account (nothing to
	// enumerate), no account created, and a notice to the owner.
	var fake struct {
		UserID string `json:"user_id"`
		Handle string `json:"handle"`
	}
	e.expect(201, "", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "alice@example.com", "pseudo": "alice2", "password": "correct horse battery"}, &fake))
	if fake.UserID == "" || fake.Handle != "alice2@id.test" {
		t.Fatalf("answer for a taken address: %+v", fake)
	}
	if !strings.Contains(e.mail.last["alice@example.com"], "déjà un") {
		t.Fatalf("no notice to the owner: %q", e.mail.last["alice@example.com"])
	}
	if u, _ := e.srv.userBy(context.Background(), "pseudo_norm", "alice2"); u != nil {
		t.Fatal("an account was created for a taken address")
	}

	e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "wrong password!", "device_key": pub}, nil))
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "nobody", "password": "wrong password!", "device_key": pub}, nil))
	e.expect(400, "invalid_device_key", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "correct horse battery", "device_key": "xx"}, nil))

	var lr loginResp
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", login, &lr))
	if lr.User.Handle != "Alice@id.test" || !lr.User.EmailVerified || lr.User.Email != "alice@example.com" {
		t.Fatalf("unexpected user %+v", lr.User)
	}

	var me userResp
	e.expect(200, "", e.call("GET", "/v1/me", lr.SessionToken, nil, &me))
	if me.ID != lr.User.ID {
		t.Fatalf("me = %+v", me)
	}
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", "bogus", nil, nil))

	e.expect(204, "", e.call("POST", "/v1/auth/logout", lr.SessionToken, nil, nil))
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", lr.SessionToken, nil, nil))
}

func TestEmailCodeAttemptsAndExpiry(t *testing.T) {
	e := newEnv(t)
	e.expect(201, "", e.call("POST", "/v1/auth/register", "", map[string]string{"email": "a@example.com", "pseudo": "alice", "password": "correct horse battery"}, nil))
	code := e.mail.code(t, "a@example.com")
	for range emailCodeMaxAttempts {
		e.expect(400, "invalid_code", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "a@example.com", "code": "999999x"}, nil))
	}
	// Locked out after too many attempts, even with the right code.
	e.expect(400, "invalid_code", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "a@example.com", "code": code}, nil))

	// Resend is throttled, then issues a fresh code.
	e.expect(202, "", e.call("POST", "/v1/auth/resend-verification", "", map[string]string{"email": "a@example.com"}, nil))
	e.clock = e.clock.Add(emailResendCooldown)
	e.expect(202, "", e.call("POST", "/v1/auth/resend-verification", "", map[string]string{"email": "a@example.com"}, nil))
	code = e.mail.code(t, "a@example.com")

	e.clock = e.clock.Add(emailCodeTTL + time.Second)
	e.expect(400, "invalid_code", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "a@example.com", "code": code}, nil))

	e.expect(202, "", e.call("POST", "/v1/auth/resend-verification", "", map[string]string{"email": "a@example.com"}, nil))
	e.expect(204, "", e.call("POST", "/v1/auth/verify-email", "", map[string]string{"email": "a@example.com", "code": e.mail.code(t, "a@example.com")}, nil))

	// Unknown emails get the same answer.
	e.expect(202, "", e.call("POST", "/v1/auth/resend-verification", "", map[string]string{"email": "ghost@example.com"}, nil))
}

func TestTwoFactor(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	tok := lr.SessionToken

	e.expect(401, "invalid_credentials", e.call("POST", "/v1/me/2fa/setup", tok, map[string]string{"password": "nope nope nope"}, nil))
	var setup struct{ Secret, OtpauthURI string }
	e.expect(200, "", e.call("POST", "/v1/me/2fa/setup", tok, map[string]string{"password": pw}, &setup))

	e.expect(400, "invalid_mfa_code", e.call("POST", "/v1/me/2fa/enable", tok, map[string]string{"code": "000000"}, nil))
	code, _ := totpCode(setup.Secret, totpStep(e.clock))
	var enabled struct {
		BackupCodes []string `json:"backup_codes"`
	}
	e.expect(200, "", e.call("POST", "/v1/me/2fa/enable", tok, map[string]string{"code": code}, &enabled))
	if len(enabled.BackupCodes) != backupCodeCount {
		t.Fatalf("got %d backup codes", len(enabled.BackupCodes))
	}

	_, pub := deviceKey(t)
	login := map[string]string{"login": "alice", "password": pw, "device_key": pub}
	e.expect(401, "mfa_required", e.call("POST", "/v1/auth/login", "", login, nil))

	// The code that enabled 2FA cannot be replayed.
	login["totp_code"] = code
	e.expect(401, "invalid_mfa_code", e.call("POST", "/v1/auth/login", "", login, nil))

	e.clock = e.clock.Add(totpPeriod * time.Second)
	login["totp_code"], _ = totpCode(setup.Secret, totpStep(e.clock))
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", login, nil))
	e.expect(401, "invalid_mfa_code", e.call("POST", "/v1/auth/login", "", login, nil))

	// Backup codes work once, with or without dashes, any case.
	login["totp_code"] = enabled.BackupCodes[0]
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", login, nil))
	e.expect(401, "invalid_mfa_code", e.call("POST", "/v1/auth/login", "", login, nil))
	login["totp_code"] = normalizeBackupCode(enabled.BackupCodes[1])
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", login, nil))

	e.clock = e.clock.Add(totpPeriod * time.Second)
	code, _ = totpCode(setup.Secret, totpStep(e.clock))
	e.expect(204, "", e.call("POST", "/v1/me/2fa/disable", tok, map[string]string{"password": pw, "code": code}, nil))
	delete(login, "totp_code")
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", login, nil))
}

func TestSessions(t *testing.T) {
	e := newEnv(t)
	first, _ := e.registerVerified("a@example.com", "alice", "correct horse battery")
	_, pub := deviceKey(t)
	var second loginResp
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "correct horse battery", "device_name": "phone", "device_key": pub}, &second))

	var list []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	e.expect(200, "", e.call("GET", "/v1/me/sessions", first.SessionToken, nil, &list))
	if len(list) != 2 || !list[0].Current || list[1].Current {
		t.Fatalf("sessions = %+v", list)
	}

	// Another user cannot revoke alice's sessions.
	bob, _ := e.registerVerified("b@example.com", "bob", "correct horse battery")
	e.expect(404, "not_found", e.call("DELETE", "/v1/me/sessions/"+second.SessionID, bob.SessionToken, nil, nil))

	e.expect(204, "", e.call("DELETE", "/v1/me/sessions/"+second.SessionID, first.SessionToken, nil, nil))
	e.expect(401, "unauthorized", e.call("GET", "/v1/me", second.SessionToken, nil, nil))
}

func TestIdentityTokenFlow(t *testing.T) {
	e := newEnv(t)
	lr, device := e.registerVerified("a@example.com", "alice", "correct horse battery")

	var ks idtoken.KeySet
	e.expect(200, "", e.call("GET", idtoken.WellKnownPath, "", nil, &ks))

	var issued struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	e.expect(200, "", e.call("POST", "/v1/identity/token", lr.SessionToken, nil, &issued))

	// What a community server does: verify offline, then check the device proof.
	claims, err := idtoken.Verify(issued.Token, ks, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != lr.User.ID || claims.Handle != "alice@id.test" {
		t.Fatalf("claims = %+v", claims)
	}
	if err := idtoken.VerifyProofV2(claims, "my-server", "n1", "chat.example", idtoken.TLSAuthority, idtoken.SignProofV2(device, "my-server", "n1", "chat.example", idtoken.TLSAuthority)); err != nil {
		t.Fatal(err)
	}

	// Disabled accounts lose access immediately.
	e.srv.db.Exec(`UPDATE users SET disabled_at = 1 WHERE id = ?`, lr.User.ID)
	e.expect(403, "account_disabled", e.call("POST", "/v1/identity/token", lr.SessionToken, nil, nil))
}

func TestTOTPVectors(t *testing.T) {
	// RFC 6238 appendix B (SHA1), truncated to 6 digits.
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	for _, tc := range []struct {
		unix int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		got, err := totpCode(secret, tc.unix/totpPeriod)
		if err != nil || got != tc.want {
			t.Errorf("t=%d: got %s, want %s (err %v)", tc.unix, got, tc.want, err)
		}
	}
}

func TestLockout(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	_, pub := deviceKey(t)
	good := map[string]string{"login": "alice", "password": pw, "device_key": pub}

	// Failures by pseudo and by email share one counter.
	for i := range maxAuthFailures {
		login := "alice"
		if i%2 == 1 {
			login = "a@example.com"
		}
		e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": login, "password": "wrong password", "device_key": pub}, nil))
	}
	// Locked: even the right password is refused, and the re-check endpoints are locked too.
	e.expect(429, "account_locked", e.call("POST", "/v1/auth/login", "", good, nil))
	e.expect(429, "account_locked", e.call("POST", "/v1/me/2fa/setup", lr.SessionToken, map[string]string{"password": pw}, nil))

	// The lock lifts once the oldest failure is an hour old.
	e.clock = e.clock.Add(lockoutWindow)
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", good, nil))

	// A successful login resets the counter.
	for range maxAuthFailures - 1 {
		e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "wrong password", "device_key": pub}, nil))
	}
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", good, nil))
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]string{"login": "alice", "password": "wrong password", "device_key": pub}, nil))
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", good, nil))

	// Unknown logins lock the same way, so lockout does not reveal which accounts exist.
	ghost := map[string]string{"login": "ghost", "password": "wrong password", "device_key": pub}
	for range maxAuthFailures {
		e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", ghost, nil))
	}
	e.expect(429, "account_locked", e.call("POST", "/v1/auth/login", "", ghost, nil))
}

func TestLockout2FA(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	var setup struct{ Secret string }
	e.expect(200, "", e.call("POST", "/v1/me/2fa/setup", lr.SessionToken, map[string]string{"password": pw}, &setup))
	code, _ := totpCode(setup.Secret, totpStep(e.clock))
	e.expect(200, "", e.call("POST", "/v1/me/2fa/enable", lr.SessionToken, map[string]string{"code": code}, nil))

	// Someone who knows the password cannot brute-force the 2FA code.
	_, pub := deviceKey(t)
	login := map[string]string{"login": "alice", "password": pw, "device_key": pub, "totp_code": "000000"}
	for range maxAuthFailures {
		e.expect(401, "invalid_mfa_code", e.call("POST", "/v1/auth/login", "", login, nil))
	}
	e.clock = e.clock.Add(totpPeriod * time.Second)
	login["totp_code"], _ = totpCode(setup.Secret, totpStep(e.clock))
	e.expect(429, "account_locked", e.call("POST", "/v1/auth/login", "", login, nil))
}
