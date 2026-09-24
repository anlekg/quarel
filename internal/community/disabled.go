package community

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Accounts disabled by their Identity service (on legal order) are published
// at /v1/disabled-accounts. The server polls it for every trusted issuer, ends
// the sessions of disabled members right away (instead of when their identity
// token expires) and refuses their logins. They stay members: if the account
// is re-enabled, they can come back.

type disabledSet struct {
	mu  sync.Mutex
	set map[string]map[string]bool // issuer → subjects
}

func (s *Server) isDisabled(issuer, subject string) bool {
	s.disabled.mu.Lock()
	defer s.disabled.mu.Unlock()
	return s.disabled.set[issuer][subject]
}

// ApplyDisabled replaces the list of disabled subjects of an issuer and
// disconnects the members newly on it.
func (s *Server) ApplyDisabled(ctx context.Context, issuer string, subjects []string) error {
	next := map[string]bool{}
	for _, sub := range subjects {
		next[sub] = true
	}
	s.disabled.mu.Lock()
	if s.disabled.set == nil {
		s.disabled.set = map[string]map[string]bool{}
	}
	prev := s.disabled.set[issuer]
	s.disabled.set[issuer] = next
	s.disabled.mu.Unlock()

	for sub := range next {
		if prev[sub] {
			continue
		}
		m, err := memberBy(ctx, s.db, `issuer = ? AND subject = ? AND left_at IS NULL`, issuer, sub)
		if err != nil {
			return err
		}
		if m == nil {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE member_id = ?`, m.ID); err != nil {
			return err
		}
		s.disconnectVoice(ctx, m.ID)
		s.hub.Disconnect(func(key, _ string) bool { return key == m.ID }, "account disabled by its identity service")
		slog.Info("member's account disabled by its identity service", "member", m.ID, "issuer", issuer)
	}
	return nil
}

// WatchDisabled polls the trusted issuers every interval until ctx ends.
func (s *Server) WatchDisabled(ctx context.Context, every time.Duration) {
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		for _, issuer := range s.cfg.TrustedIssuers {
			subjects, err := fetchDisabled(ctx, client, issuer)
			if err != nil {
				slog.Debug("disabled accounts", "issuer", issuer, "err", err)
				continue // keep the last known list
			}
			if err := s.ApplyDisabled(ctx, issuer, subjects); err != nil {
				s.logErr("applying disabled accounts", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func fetchDisabled(ctx context.Context, client *http.Client, issuer string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", issuerBaseURL(issuer)+"/v1/disabled-accounts", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Issuer   string `json:"issuer"`
		Accounts []struct {
			Subject string `json:"sub"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Issuer != issuer {
		return nil, fmt.Errorf("list of %s claims issuer %q", issuer, doc.Issuer)
	}
	out := make([]string, len(doc.Accounts))
	for i, a := range doc.Accounts {
		out[i] = a.Subject
	}
	return out, nil
}
