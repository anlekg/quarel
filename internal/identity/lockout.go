package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Brute-force protection: after maxAuthFailures failed attempts (wrong
// password or wrong 2FA code) within lockoutWindow, the account is locked
// until the oldest of those failures leaves the window.
const (
	maxAuthFailures = 15
	lockoutWindow   = time.Hour
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

// guarded runs check unless key is locked out, and records a failure if
// check rejects a guessed secret.
func (s *Server) guarded(ctx context.Context, key string, check func() error) error {
	if err := s.checkLockout(ctx, key); err != nil {
		return err
	}
	err := check()
	if isGuessFailure(err) {
		if _, dbErr := s.db.ExecContext(ctx, `INSERT INTO auth_failures (key, at) VALUES (?, ?)`, key, s.now().Unix()); dbErr != nil {
			slog.Error("recording auth failure", "err", dbErr)
		}
	}
	return err
}

func (s *Server) checkLockout(ctx context.Context, key string) error {
	now := s.now().Unix()
	since := now - int64(lockoutWindow.Seconds())
	// Opportunistic cleanup keeps the table small.
	s.db.ExecContext(ctx, `DELETE FROM auth_failures WHERE at <= ?`, since)

	var count int
	var oldest int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(at), 0) FROM (
			SELECT at FROM auth_failures WHERE key = ? AND at > ? ORDER BY at DESC LIMIT ?
		)`, key, since, maxAuthFailures).Scan(&count, &oldest)
	if err != nil {
		return err
	}
	if count < maxAuthFailures {
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

// clearFailures resets the counter after a fully successful login.
func (s *Server) clearFailures(ctx context.Context, key string) {
	s.db.ExecContext(ctx, `DELETE FROM auth_failures WHERE key = ?`, key)
}
