package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(3, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("event %d refused", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait < 19*time.Second || wait > 21*time.Second {
		t.Fatalf("4th event: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("keys are not independent")
	}
	now = now.Add(20 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("token not refilled")
	}
	var none *Limiter
	if ok, _ := none.Allow("x"); !ok || New(0, time.Second) != nil {
		t.Fatal("nil limiter must allow everything")
	}
}

func TestClientIP(t *testing.T) {
	proxies, err := ParseProxies("10.0.0.0/8, 192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ remote, xff, want string }{
		{"203.0.113.5:1234", "", "203.0.113.5"},
		{"203.0.113.5:1234", "1.2.3.4", "203.0.113.5"},              // untrusted peer: header ignored
		{"10.1.2.3:80", "1.2.3.4", "1.2.3.4"},                       // trusted proxy
		{"10.1.2.3:80", "6.6.6.6, 1.2.3.4, 192.168.1.1", "1.2.3.4"}, // rightmost untrusted hop
		{"[2001:db8:1:2:3:4:5:6]:443", "", "2001:db8:1:2::/64"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := proxies.ClientIP(r); got != tc.want {
			t.Errorf("%s + %q: got %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}
