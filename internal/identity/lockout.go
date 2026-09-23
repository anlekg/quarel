package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Brute-force protection. Failed attempts (wrong password or wrong 2FA code)
// are counted twice, over a sliding window:
//   - per account and client IP: Limits.AuthFailuresPerIP locks that account
//     for that IP only, so someone typing wrong passwords from their own
//     address cannot lock the real owner out;
//   - per account, all IPs together: Limits.AuthFailuresTotal stops attacks
//     spread over many addresses.
//
// A lock lifts when the oldest counted failure leaves the window.
const (
	maxAuthFailures    = 15  // default per account and IP
	maxAuthFailuresAll = 100 // default per account, all IPs
	lockoutWindow      = time.Hour
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
	key string
	max int
}

func (s *Server) failureCounters(r *http.Request, base string) []failureCounter {
	return []failureCounter{
		{base + "|ip:" + s.proxies.ClientIP(r), s.cfg.Limits.AuthFailuresPerIP},
		{base, s.cfg.Limits.AuthFailuresTotal},
	}
}

// guarded runs check unless the account is locked out for this client, and
// records a failure if check rejects a guessed secret.
func (s *Server) guarded(r *http.Request, base string, check func() error) error {
	ctx := r.Context()
	counters := s.failureCounters(r, base)
	for _, c := range counters {
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

// clearFailures resets this client's counter after a fully successful login.
// The all-IPs counter is left to expire, so an attacker cannot reset it by
// logging into their own account.
func (s *Server) clearFailures(r *http.Request, base string) {
	s.db.ExecContext(r.Context(), `DELETE FROM auth_failures WHERE key = ?`, s.failureCounters(r, base)[0].key)
}
