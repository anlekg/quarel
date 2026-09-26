// Package ratelimit provides in-memory token buckets keyed by client IP,
// user or anything else, plus client IP extraction behind trusted proxies.
package ratelimit

import (
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
)

// Limiter allows at most N events per interval for each key, with bursts up to N.
// A nil *Limiter allows everything.
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

var maxBuckets = 100_000 // variable for tests

// New returns a limiter allowing n events per interval per key; n <= 0 returns nil (no limit).
func New(n int, interval time.Duration) *Limiter {
	if n <= 0 {
		return nil
	}
	return &Limiter{rate: float64(n) / interval.Seconds(), burst: float64(n), buckets: map[string]*bucket{}, now: time.Now}
}

// Allow consumes one token for key. When refused, it returns how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxBuckets {
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// evict drops buckets that are full again (their keys have been idle long
// enough). If that is not enough (a flood of distinct keys), the oldest
// buckets go too, so memory stays bounded: one of those keys may then start
// again with a full bucket, which only matters under such a flood.
func (l *Limiter) evict(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < maxBuckets*9/10 {
		return
	}
	type aged struct {
		key  string
		last time.Time
	}
	all := make([]aged, 0, len(l.buckets))
	for k, b := range l.buckets {
		all = append(all, aged{k, b.last})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].last.Before(all[j].last) })
	for _, a := range all[:len(all)-maxBuckets*9/10] {
		delete(l.buckets, a.key)
	}
}

// Error is the API error for a refused request.
func Error(wait time.Duration) error {
	secs := int64(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	e := httpapi.Errf(http.StatusTooManyRequests, "rate_limited", "too many requests; retry in %d second(s)", secs)
	e.RetryAfter = secs
	return e
}

// Check returns an API error if key is over its limit.
func (l *Limiter) Check(key string) error {
	if ok, wait := l.Allow(key); !ok {
		return Error(wait)
	}
	return nil
}

// Wrap limits h per key(r); a nil limiter returns h unchanged.
func (l *Limiter) Wrap(key func(*http.Request) string, h http.Handler) http.Handler {
	if l == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := l.Check(key(r)); err != nil {
			httpapi.WriteErr(w, r, err)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Proxies is a list of trusted reverse-proxy networks.
type Proxies []*net.IPNet

// ParseProxies parses a comma-separated list of CIDRs or IPs.
func ParseProxies(s string) (Proxies, error) {
	var out Proxies
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if strings.Contains(part, ":") {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (p Proxies) trusted(ip net.IP) bool {
	for _, n := range p {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the client address as a rate-limit key. X-Forwarded-For is
// honoured only when the direct peer is a trusted proxy (the rightmost
// untrusted address wins). IPv6 clients are grouped by /64, since one
// subscriber usually owns a whole /64.
func (p Proxies) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && p.trusted(ip) {
		hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop := net.ParseIP(strings.TrimSpace(hops[i]))
			if hop == nil {
				break
			}
			ip = hop
			if !p.trusted(hop) {
				break
			}
		}
	}
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
