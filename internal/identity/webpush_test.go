package identity

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto/ed25519"
)

// A fake push service: records the pushes and checks their VAPID signature.
type fakePushService struct {
	mu     sync.Mutex
	got    []*http.Request
	bodies []int
	status int
}

func (f *fakePushService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.got = append(f.got, r)
	f.bodies = append(f.bodies, len(b))
	st := f.status
	f.mu.Unlock()
	if st == 0 {
		st = http.StatusCreated
	}
	w.WriteHeader(st)
}

func (f *fakePushService) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func verifyVAPID(t *testing.T, header, pubB64, audience string) {
	t.Helper()
	var tok, k string
	for _, part := range strings.Split(strings.TrimPrefix(header, "vapid "), ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "t="); ok {
			tok = v
		}
		if v, ok := strings.CutPrefix(part, "k="); ok {
			k = v
		}
	}
	if k != pubB64 {
		t.Fatalf("k = %q, want %q", k, pubB64)
	}
	raw, _ := b64url.DecodeString(k)
	pk, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := pk.Bytes()
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(b[1:33]), Y: new(big.Int).SetBytes(b[33:])}
	parts := strings.Split(tok, ".")
	sig, _ := b64url.DecodeString(parts[2])
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("VAPID signature does not verify")
	}
	var claims struct {
		Aud string
		Exp int64
	}
	c, _ := b64url.DecodeString(parts[1])
	json.Unmarshal(c, &claims)
	if claims.Aud != audience {
		t.Fatalf("aud = %q, want %q", claims.Aud, audience)
	}
}

func TestWebPush(t *testing.T) {
	e := newEnv(t)
	push := &fakePushService{}
	ps := httptest.NewTLSServer(push)
	defer ps.Close()
	e.srv.webpush.client = ps.Client() // trusts the fake service's certificate

	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.befriend(alice, bob, "bob")
	_, master, _ := ed25519.GenerateKey(nil)
	e.expect(200, "", e.upload(newTestDevice(bob), master, true))

	var key struct{ Key string }
	e.expect(200, "", e.call("GET", "/v1/push/key", "", nil, &key))
	e.expect(400, "invalid_endpoint", e.call("PUT", "/v1/me/push", bob.SessionToken, map[string]string{"endpoint": "http://push.example/x"}, nil))
	// Only the browsers' push services (port 443), or those the operator adds:
	// an endpoint cannot make this service call anything else.
	for _, bad := range []string{"https://evil.example/x", "https://fcm.googleapis.com.evil.example/x", "https://fcm.googleapis.com:8443/x", ps.URL + "/push/bob"} {
		e.expect(400, "invalid_endpoint", e.call("PUT", "/v1/me/push", bob.SessionToken, map[string]string{"endpoint": bad}, nil))
	}
	for _, ok := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://updates.push.services.mozilla.com/wpush/v2/abc", "https://web.push.apple.com/abc"} {
		if !e.srv.pushEndpointOK(ok) {
			t.Fatalf("%s refused", ok)
		}
	}
	e.srv.cfg.PushHosts = []string{"127.0.0.1"} // the fake service of this test
	e.expect(204, "", e.call("PUT", "/v1/me/push", bob.SessionToken, map[string]string{"endpoint": ps.URL + "/push/bob"}, nil))

	send := func(payload string) {
		msg := map[string]any{"messages": []map[string]string{{"device_id": bob.SessionID, "payload": payload}}}
		e.expect(204, "", e.call("POST", "/v1/to-device", alice.SessionToken, msg, nil))
	}
	waitFor := func(n int) {
		t.Helper()
		for i := 0; i < 100 && push.count() < n; i++ {
			time.Sleep(20 * time.Millisecond)
		}
		if push.count() != n {
			t.Fatalf("%d pushes, want %d", push.count(), n)
		}
	}
	// What bob's own devices send (history, list of servers…) wakes nothing.
	bob2 := e.login2("bob@example.com", "mot-de-passe-bob", "Téléphone")
	own := map[string]any{"messages": []map[string]string{{"device_id": bob.SessionID, "payload": "sync"}}}
	e.expect(204, "", e.call("POST", "/v1/to-device", bob2.SessionToken, own, nil))
	time.Sleep(100 * time.Millisecond)
	waitFor(0)
	// Something arrives from alice for bob's offline device: an empty push, signed.
	send("secret-olm-message")
	waitFor(1)
	push.mu.Lock()
	r, size := push.got[0], push.bodies[0]
	push.mu.Unlock()
	if size != 0 || r.URL.Path != "/push/bob" || r.Header.Get("TTL") == "" {
		t.Fatalf("push: %d bytes, %s, TTL %q", size, r.URL.Path, r.Header.Get("TTL"))
	}
	verifyVAPID(t, r.Header.Get("Authorization"), key.Key, ps.URL)
	// At most one per 30 s.
	send("another")
	time.Sleep(100 * time.Millisecond)
	waitFor(1)
	e.clock = e.clock.Add(31 * time.Second)
	// The push service forgot the subscription: it is deleted.
	push.mu.Lock()
	push.status = http.StatusGone
	push.mu.Unlock()
	send("third")
	waitFor(2)
	time.Sleep(100 * time.Millisecond)
	var n int
	e.srv.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions`).Scan(&n)
	if n != 0 {
		t.Fatal("subscription kept after 410")
	}
}
