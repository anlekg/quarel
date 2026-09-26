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
	_, own := deviceKey(t)      // the owner's device
	_, attacker := deviceKey(t) // someone else's
	login := func(ip, device, password string) result {
		return e.callFrom(ip, "POST", "/v1/auth/login", map[string]string{"login": "alice", "password": password, "device_key": device})
	}
	e.expect(200, "", login("1.1.1.1", own, pw)) // the owner's device is now known

	// An attacker locks the account only for their own address.
	for range 3 {
		e.expect(401, "invalid_credentials", login("6.6.6.6", attacker, "wrong password"))
	}
	e.expect(429, "account_locked", login("6.6.6.6", attacker, pw))
	e.expect(200, "", login("1.1.1.1", own, pw)) // the owner still gets in

	// Spreading attempts over many addresses hits the per-account ceiling:
	// 3 failures above + 4 here = 7, then new devices are locked out, even the owner's...
	for _, ip := range []string{"7.0.0.1", "7.0.0.2", "7.0.0.3", "7.0.0.4"} {
		e.expect(401, "invalid_credentials", login(ip, attacker, "wrong password"))
	}
	e.expect(429, "account_locked", login("7.0.0.5", attacker, "wrong password"))
	_, newDevice := deviceKey(t)
	e.expect(429, "account_locked", login("1.1.1.1", newDevice, pw))
	// ...but not a device that already signed in: the attack cannot keep the owner out.
	e.expect(200, "", login("1.1.1.1", own, pw))

	// Someone who learnt that device's key is held by its own counter.
	for _, ip := range []string{"8.0.0.1", "8.0.0.2", "8.0.0.3"} {
		e.expect(401, "invalid_credentials", login(ip, own, "wrong password"))
	}
	e.expect(429, "account_locked", login("8.0.0.4", own, pw))
	e.clock = e.clock.Add(lockoutWindow)
	e.expect(200, "", login("1.1.1.1", newDevice, pw))
}
