package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/ratelimit"
)

// newLimitedEnv is a test environment with limits, where requests can claim
// any client IP through X-Forwarded-For (the test client is a trusted proxy).
func newLimitedEnv(t *testing.T, limits Limits) *testEnv {
	e := newEnv(t)
	proxies, _ := ratelimit.ParseProxies("127.0.0.1")
	db, err := OpenDB(filepath.Join(t.TempDir(), "limited.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, key, _ := ed25519.GenerateKey(nil)
	e.srv = New(Config{Issuer: "id.test", TokenTTL: time.Hour, Limits: limits, TrustedProxies: proxies}, db, key, e.mail)
	e.srv.now = func() time.Time { return e.clock }
	e.http.Config.Handler = e.srv.Handler()
	return e
}

func (e *testEnv) callFrom(ip, method, path string, body any) result {
	e.t.Helper()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(method, e.http.URL+path, &buf)
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var er struct{ Error httpapi.Error }
	json.NewDecoder(resp.Body).Decode(&er)
	return result{resp.StatusCode, er.Error.Code}
}

func TestRegisterAndGlobalLimits(t *testing.T) {
	e := newLimitedEnv(t, Limits{Register: 2, Global: 8})
	reg := func(ip, n string) result {
		return e.callFrom(ip, "POST", "/v1/auth/register", map[string]string{"email": n + "@example.com", "pseudo": n, "password": "correct horse battery"})
	}
	e.expect(201, "", reg("1.1.1.1", "user1"))
	e.expect(201, "", reg("1.1.1.1", "user2"))
	e.expect(429, "rate_limited", reg("1.1.1.1", "user3"))
	e.expect(201, "", reg("2.2.2.2", "user3")) // another address is not affected

	// Global ceiling per address, all endpoints (8 per minute).
	for range 5 {
		e.callFrom("3.3.3.3", "GET", "/v1/health", nil)
	}
	e.expect(200, "", e.callFrom("3.3.3.3", "GET", "/v1/health", nil))
	e.expect(200, "", e.callFrom("3.3.3.3", "GET", "/v1/health", nil))
	e.expect(200, "", e.callFrom("3.3.3.3", "GET", "/v1/health", nil))
	e.expect(429, "rate_limited", e.callFrom("3.3.3.3", "GET", "/v1/health", nil))
}

func TestLockoutIsPerIP(t *testing.T) {
	e := newLimitedEnv(t, Limits{AuthFailuresPerIP: 3, AuthFailuresTotal: 7})
	const pw = "correct horse battery"
	e.registerVerified("a@example.com", "alice", pw)
	_, pub := deviceKey(t)
	login := func(ip, password string) result {
		return e.callFrom(ip, "POST", "/v1/auth/login", map[string]string{"login": "alice", "password": password, "device_key": pub})
	}

	// An attacker locks the account only for their own address.
	for range 3 {
		e.expect(401, "invalid_credentials", login("6.6.6.6", "wrong password"))
	}
	e.expect(429, "account_locked", login("6.6.6.6", pw))
	e.expect(200, "", login("1.1.1.1", pw)) // the owner still gets in

	// Spreading attempts over many addresses hits the per-account ceiling:
	// 3 failures above + 4 here = 7, then everyone is locked out, owner included.
	for _, ip := range []string{"7.0.0.1", "7.0.0.2", "7.0.0.3", "7.0.0.4"} {
		e.expect(401, "invalid_credentials", login(ip, "wrong password"))
	}
	e.expect(429, "account_locked", login("7.0.0.5", "wrong password"))
	e.expect(429, "account_locked", login("1.1.1.1", pw))
	e.clock = e.clock.Add(lockoutWindow)
	e.expect(200, "", login("1.1.1.1", pw))
}
