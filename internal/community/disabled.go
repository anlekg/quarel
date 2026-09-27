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

	"github.com/anlekg/quarel/pkg/idtoken"
)

// Each Identity service publishes at /v1/disabled-accounts the accounts it
// disabled (on legal order) and the devices whose session recently ended
// there (logout, revoked device, password change, idle expiry, deleted
// account), both hashed (idtoken.AccountHash, DeviceHash). The server polls
// it for every trusted issuer:
//   - disabled members' sessions end right away (instead of when their
//     identity token expires) and their logins are refused; they stay
//     members, and can come back if the account is re-enabled;
//   - a session opened here by an ended device before it ended goes too.

type disabledSet struct {
	mu    sync.Mutex
	set   map[string]map[string]bool      // issuer → account hashes
	ended map[string]map[string]time.Time // issuer → device hash → when its session there ended
}

func (s *Server) isDisabled(issuer, subject string) bool {
	s.disabled.mu.Lock()
	defer s.disabled.mu.Unlock()
	return s.disabled.set[issuer][idtoken.AccountHash(subject)]
}

// deviceEnded reports whether the Identity session of a device ended after
// issued: a token it obtained before a logout or a revocation (still valid
// for a few hours) must not open a new session here either.
func (s *Server) deviceEnded(issuer, deviceKey string, issued time.Time) bool {
	s.disabled.mu.Lock()
	defer s.disabled.mu.Unlock()
	at, ok := s.disabled.ended[issuer][idtoken.DeviceHash(deviceKey)]
	return ok && issued.Before(at)
}

// issuerList is what an Identity service publishes.
type issuerList struct {
	Issuer   string `json:"issuer"`
	Accounts []struct {
		Hash string `json:"h"`
	} `json:"accounts"`
	Devices []endedDevice `json:"ended_devices"`
	Deleted []endedDevice `json:"deleted_accounts"` // {h: account hash, at}
}

type endedDevice struct {
	Hash string    `json:"h"`
	At   time.Time `json:"at"`
}

// ApplyDisabled replaces the disabled accounts of an issuer (hashes) and
// disconnects the members newly on it.
func (s *Server) ApplyDisabled(ctx context.Context, issuer string, hashes []string) error {
	next := map[string]bool{}
	for _, h := range hashes {
		next[h] = true
	}
	s.disabled.mu.Lock()
	if s.disabled.set == nil {
		s.disabled.set = map[string]map[string]bool{}
	}
	prev := s.disabled.set[issuer]
	s.disabled.set[issuer] = next
	s.disabled.mu.Unlock()

	fresh := false
	for h := range next {
		if !prev[h] {
			fresh = true
		}
	}
	if !fresh {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, subject FROM members WHERE issuer = ? AND left_at IS NULL`, issuer)
	if err != nil {
		return err
	}
	var hit []string
	for rows.Next() {
		var id, sub string
		if rows.Scan(&id, &sub) == nil {
			if h := idtoken.AccountHash(sub); next[h] && !prev[h] {
				hit = append(hit, id)
			}
		}
	}
	rows.Close()
	for _, id := range hit {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE member_id = ?`, id); err != nil {
			return err
		}
		s.disconnectVoice(ctx, id)
		s.hub.Disconnect(func(key, _ string) bool { return key == id }, "account disabled by its identity service")
		slog.Info("member's account disabled by its identity service", "member", id, "issuer", issuer)
	}
	return nil
}

// ApplyEnded ends the sessions opened here by devices whose session ended
// on their Identity service since.
func (s *Server) ApplyEnded(ctx context.Context, issuer string, devices []endedDevice) error {
	ended := map[string]time.Time{}
	for _, d := range devices {
		ended[d.Hash] = d.At
	}
	s.disabled.mu.Lock() // kept for logins (see deviceEnded): the list covers the tokens still valid
	if s.disabled.ended == nil {
		s.disabled.ended = map[string]map[string]time.Time{}
	}
	s.disabled.ended[issuer] = ended
	s.disabled.mu.Unlock()
	if len(devices) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.token_hash, s.member_id, s.device_key, s.created_at FROM sessions s
		JOIN members m ON m.id = s.member_id WHERE m.issuer = ? AND s.device_key IS NOT NULL`, issuer)
	if err != nil {
		return err
	}
	type victim struct{ token, member string }
	var hit []victim
	for rows.Next() {
		var v victim
		var key string
		var created int64
		if rows.Scan(&v.token, &v.member, &key, &created) != nil {
			continue
		}
		// Only sessions opened before the device's session ended: the same
		// device may have signed in again since.
		if at, ok := ended[idtoken.DeviceHash(key)]; ok && created < at.UnixMilli() {
			hit = append(hit, v)
		}
	}
	rows.Close()
	for _, v := range hit {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, v.token); err != nil {
			return err
		}
		s.hub.Disconnect(func(_, group string) bool { return group == v.token }, "session ended on its identity service")
		s.disconnectVoice(ctx, v.member) // the voice connection may be this device's
		slog.Info("session ended by its identity service", "member", v.member, "issuer", issuer)
	}
	return nil
}

// WatchDisabled polls the trusted issuers every interval until ctx ends.
func (s *Server) WatchDisabled(ctx context.Context, every time.Duration) {
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		for _, issuer := range s.cfg.TrustedIssuers {
			list, err := fetchDisabled(ctx, client, issuer)
			if err != nil {
				slog.Debug("disabled accounts", "issuer", issuer, "err", err)
				continue // keep the last known list
			}
			hashes := make([]string, len(list.Accounts))
			for i, a := range list.Accounts {
				hashes[i] = a.Hash
			}
			if err := s.ApplyDisabled(ctx, issuer, hashes); err != nil {
				s.logErr("applying disabled accounts", err)
			}
			if err := s.ApplyEnded(ctx, issuer, list.Devices); err != nil {
				s.logErr("applying ended sessions", err)
			}
			if err := s.ApplyDeleted(ctx, issuer, list.Deleted); err != nil {
				s.logErr("applying deleted accounts", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func fetchDisabled(ctx context.Context, client *http.Client, issuer string) (*issuerList, error) {
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
	var doc issuerList
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Issuer != issuer {
		return nil, fmt.Errorf("list of %s claims issuer %q", issuer, doc.Issuer)
	}
	return &doc, nil
}

// ApplyDeleted anonymises the members whose account was deleted on their
// identity service: their name becomes "Ancien compte" (their messages stay,
// like any former member's), nickname and roles go, and they leave. A
// verified phone number goes too, unless they are banned (it keeps their
// number out). A deleted owner leaves the server without one: a new claim
// code is issued (see ResetOwnership).
func (s *Server) ApplyDeleted(ctx context.Context, issuer string, accounts []endedDevice) error {
	if len(accounts) == 0 {
		return nil
	}
	gone := map[string]bool{}
	for _, a := range accounts {
		gone[a.Hash] = true
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, subject, is_owner, left_at IS NULL FROM members WHERE issuer = ? AND handle != ''`, issuer)
	if err != nil {
		return err
	}
	type hit struct {
		id            string
		owner, active bool
	}
	var hits []hit
	for rows.Next() {
		var h hit
		var sub string
		if rows.Scan(&h.id, &sub, &h.owner, &h.active) == nil && gone[idtoken.AccountHash(sub)] {
			hits = append(hits, h)
		}
	}
	rows.Close()
	for _, h := range hits {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := s.removeMember(ctx, tx, h.id); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE members SET handle = '', nickname = NULL, is_owner = 0, bio = '', theme = '', profile_at = 0, avatar_at = 0, banner_at = 0,
			phone_hash = CASE WHEN EXISTS (SELECT 1 FROM bans WHERE member_id = members.id) THEN phone_hash END WHERE id = ?`, h.id); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM member_images WHERE member_id = ?`, h.id); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if h.active {
			s.afterRemoval(h.id, "deleted")
		}
		slog.Info("member's account deleted on its identity service: anonymised", "member", h.id, "issuer", issuer)
		if h.owner {
			if _, err := s.ResetOwnership(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
