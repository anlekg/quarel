package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Brute-force protection. Failed attempts (wrong password or wrong 2FA code)
// are counted over a sliding window:
//   - per account and client IP: Limits.AuthFailuresPerIP locks that account
//     for that IP only, so someone typing wrong passwords from their own
//     address cannot lock the real owner out;
//   - per account, all IPs together: Limits.AuthFailuresTotal stops attacks
//     spread over many addresses;
//   - per account and known device (a device key that already signed in to
//     the account): such a device is not held by the all-IPs lock, which
//     anyone knowing the pseudo could otherwise keep on forever, but by its
//     own counter (Limits.AuthFailuresPerIP).
//
// A lock lifts when the oldest counted failure leaves the window.
const (
	maxAuthFailures    = 15  // default per account and IP
	maxAuthFailuresAll = 100 // default per account, all IPs
	lockoutWindow      = time.Hour
	knownDeviceTTL     = 400 * 24 * time.Hour // a device unseen this long is no longer "known"
)

// lockoutKey identifies whose failures are counted. Existing accounts are
// keyed by ID (so email and pseudo logins share one counter); unknown logins
// are keyed by the name tried, so they lock the same way and the lockout does
// not reveal whether an account exists.
func lockoutKey(u *user, login string) string {
	if u != nil {
		return "user:" + u.ID
	}
	return "login:" + login
}

// isGuessFailure reports whether err is a wrong secret, as opposed to a
// state error (unverified email, 2FA required…) that proves nothing.
func isGuessFailure(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Code == "invalid_credentials" || ae.Code == "invalid_mfa_code")
}

type failureCounter struct {
	key     string
	max     int
	enforce bool // false: only recorded
}

// failureCounters lists the counters of an attempt; device is the key of a
// known device ("" otherwise).
func (s *Server) failureCounters(r *http.Request, base, device string) []failureCounter {
	out := []failureCounter{{base + "|ip:" + s.proxies.ClientIP(r), s.cfg.Limits.AuthFailuresPerIP, true}}
	if device != "" {
		return append(out, failureCounter{base + "|dev:" + device, s.cfg.Limits.AuthFailuresPerIP, true},
			failureCounter{base, s.cfg.Limits.AuthFailuresTotal, false})
	}
	return append(out, failureCounter{base, s.cfg.Limits.AuthFailuresTotal, true})
}

// guarded runs check unless the account is locked out for this client, and
// records a failure if check rejects a guessed secret. A signed-in request
// comes from a known device: its session's.
func (s *Server) guarded(r *http.Request, base string, check func() error) error {
	device := ""
	if sess, ok := r.Context().Value(sessionKey{}).(*session); ok {
		device = sess.DeviceKey
	}
	return s.guardedDevice(r, base, device, check)
}

func (s *Server) guardedDevice(r *http.Request, base, device string, check func() error) error {
	ctx := r.Context()
	counters := s.failureCounters(r, base, device)
	for _, c := range counters {
		if !c.enforce {
			continue
		}
		if err := s.checkLockout(ctx, c); err != nil {
			return err
		}
	}
	err := check()
	if isGuessFailure(err) {
		for _, c := range counters {
			if c.max <= 0 {
				continue
			}
			if _, dbErr := s.db.ExecContext(ctx, `INSERT INTO auth_failures (key, at) VALUES (?, ?)`, c.key, s.now().Unix()); dbErr != nil {
				slog.Error("recording auth failure", "err", dbErr)
			}
		}
	}
	return err
}

// knownDevice reports whether deviceKey signed in to the account before.
func (s *Server) knownDevice(ctx context.Context, userID, deviceKey string) bool {
	var n int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM known_devices WHERE user_id = ? AND device_key = ? AND last_login > ?`,
		userID, deviceKey, s.now().Add(-knownDeviceTTL).Unix()).Scan(&n)
	return n > 0
}

func (s *Server) rememberDevice(ctx context.Context, userID, deviceKey string) {
	s.db.ExecContext(ctx, `INSERT INTO known_devices (user_id, device_key, last_login) VALUES (?, ?, ?)
		ON CONFLICT (user_id, device_key) DO UPDATE SET last_login = excluded.last_login`, userID, deviceKey, s.now().Unix())
}

func (s *Server) checkLockout(ctx context.Context, c failureCounter) error {
	if c.max <= 0 {
		return nil
	}
	now := s.now().Unix()
	since := now - int64(lockoutWindow.Seconds())
	// Opportunistic cleanup keeps the table small.
	s.db.ExecContext(ctx, `DELETE FROM auth_failures WHERE at <= ?`, since)

	var count int
	var oldest int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(at), 0) FROM (
			SELECT at FROM auth_failures WHERE key = ? AND at > ? ORDER BY at DESC LIMIT ?
		)`, c.key, since, c.max).Scan(&count, &oldest)
	if err != nil {
		return err
	}
	if count < c.max {
		return nil
	}
	retry := oldest + int64(lockoutWindow.Seconds()) - now
	if retry < 1 {
		retry = 1
	}
	ae := errf(http.StatusTooManyRequests, "account_locked",
		"too many failed attempts; try again in %d minute(s)", (retry+59)/60)
	ae.RetryAfter = retry
	return ae
}

// clearFailures resets this client's counters (IP, and known device) after a
// fully successful login. The all-IPs counter is left to expire, so an
// attacker cannot reset it by logging into their own account.
func (s *Server) clearFailures(r *http.Request, base, device string) {
	for _, c := range s.failureCounters(r, base, device) {
		if c.enforce && c.key != base {
			s.db.ExecContext(r.Context(), `DELETE FROM auth_failures WHERE key = ?`, c.key)
		}
	}
}
