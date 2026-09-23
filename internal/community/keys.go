package community

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

// KeySource provides the public keys of Identity services.
type KeySource interface {
	// KeySet returns issuer's keys; refresh forces a refetch (e.g. after a key rotation).
	KeySet(ctx context.Context, issuer string, refresh bool) (idtoken.KeySet, error)
}

const (
	keyCacheTTL   = time.Hour
	keyRefreshMin = time.Minute // floor between forced refetches, so bad tokens cannot hammer the issuer
)

// HTTPKeySource fetches key sets from each issuer's well-known URL and caches
// them. If an issuer is unreachable, the last known keys keep being used, so
// members can still join while the Identity service is down.
type HTTPKeySource struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedKeys
}

type cachedKeys struct {
	ks      idtoken.KeySet
	fetched time.Time
}

func NewHTTPKeySource() *HTTPKeySource {
	return &HTTPKeySource{client: &http.Client{Timeout: 10 * time.Second}, cache: map[string]cachedKeys{}}
}

func (h *HTTPKeySource) KeySet(ctx context.Context, issuer string, refresh bool) (idtoken.KeySet, error) {
	h.mu.Lock()
	c, ok := h.cache[issuer]
	h.mu.Unlock()
	age := time.Since(c.fetched)
	if ok && (age < keyCacheTTL && !refresh || refresh && age < keyRefreshMin) {
		return c.ks, nil
	}
	ks, err := h.fetch(ctx, issuer)
	if err != nil {
		if ok {
			return c.ks, nil
		}
		return idtoken.KeySet{}, err
	}
	h.mu.Lock()
	h.cache[issuer] = cachedKeys{ks: ks, fetched: time.Now()}
	h.mu.Unlock()
	return ks, nil
}

func (h *HTTPKeySource) fetch(ctx context.Context, issuer string) (idtoken.KeySet, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", issuerBaseURL(issuer)+idtoken.WellKnownPath, nil)
	if err != nil {
		return idtoken.KeySet{}, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return idtoken.KeySet{}, fmt.Errorf("fetching keys of %s: %w", issuer, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return idtoken.KeySet{}, fmt.Errorf("fetching keys of %s: HTTP %d", issuer, resp.StatusCode)
	}
	var ks idtoken.KeySet
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&ks); err != nil {
		return idtoken.KeySet{}, fmt.Errorf("keys of %s: %w", issuer, err)
	}
	if ks.Issuer != issuer {
		return idtoken.KeySet{}, fmt.Errorf("keys of %s claim issuer %q", issuer, ks.Issuer)
	}
	return ks, nil
}
