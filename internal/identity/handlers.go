package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

const (
	emailCodeTTL         = 15 * time.Minute
	emailCodeMaxAttempts = 5
	emailResendCooldown  = time.Minute
	backupCodeCount      = 10
	minPasswordLen       = 10
	maxPasswordLen       = 256
)

var pseudoRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{1,30}[A-Za-z0-9_]$`)

// --- users ---

type user struct {
	ID, Email, Pseudo, PasswordHash string
	EmailVerifiedAt                 sql.NullInt64
	TOTPSecret                      sql.NullString
	TOTPEnabledAt                   sql.NullInt64
	TOTPLastStep                    int64
	DisabledAt                      sql.NullInt64
	CreatedAt                       int64
}

// userBy loads the user matching column = value, or nil if none.
func (s *Server) userBy(ctx context.Context, column, value string) (*user, error) {
	var u user
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, pseudo, password_hash, email_verified_at, totp_secret,
		       totp_enabled_at, totp_last_step, disabled_at, created_at
		FROM users WHERE `+column+` = ?`, value).Scan(
		&u.ID, &u.Email, &u.Pseudo, &u.PasswordHash, &u.EmailVerifiedAt, &u.TOTPSecret,
		&u.TOTPEnabledAt, &u.TOTPLastStep, &u.DisabledAt, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (s *Server) handle(u *user) string { return u.Pseudo + "@" + s.cfg.Issuer }

type userResp struct {
	ID            string    `json:"id"`
	Handle        string    `json:"handle"`
	Pseudo        string    `json:"pseudo"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	TOTPEnabled   bool      `json:"totp_enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Server) userResp(u *user) userResp {
	return userResp{
		ID: u.ID, Handle: s.handle(u), Pseudo: u.Pseudo, Email: u.Email,
		EmailVerified: u.EmailVerifiedAt.Valid, TOTPEnabled: u.TOTPEnabledAt.Valid,
		CreatedAt: time.Unix(u.CreatedAt, 0).UTC(),
	}
}

func normalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || addr.Name != "" || len(e) > 254 {
		return "", errf(http.StatusBadRequest, "invalid_email", "invalid email address")
	}
	return e, nil
}

func checkPassword(p string) error {
	if n := len([]rune(p)); n < minPasswordLen || len(p) > maxPasswordLen {
		return errf(http.StatusBadRequest, "weak_password", "password must be %d to %d characters", minPasswordLen, maxPasswordLen)
	}
	return nil
}

// --- registration & email verification ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email, Pseudo, Password string
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !pseudoRe.MatchString(req.Pseudo) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_pseudo",
			"pseudo must be 3-32 characters: letters, digits, '_', '.', '-' (not at the edges)"))
		return
	}
	if err := checkPassword(req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	for _, c := range []struct{ col, val, code string }{
		{"email", email, "email_taken"},
		{"pseudo_norm", strings.ToLower(req.Pseudo), "pseudo_taken"},
	} {
		if u, err := s.userBy(ctx, c.col, c.val); err != nil {
			writeErr(w, r, err)
			return
		} else if u != nil {
			writeErr(w, r, errf(http.StatusConflict, c.code, "%s is already in use", strings.TrimSuffix(c.col, "_norm")))
			return
		}
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	u := &user{ID: newID(), Email: email, Pseudo: req.Pseudo, CreatedAt: s.now().Unix()}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, pseudo, pseudo_norm, password_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, u.ID, u.Email, u.Pseudo, strings.ToLower(u.Pseudo), hash, u.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") { // lost a race with a concurrent registration
			writeErr(w, r, errf(http.StatusConflict, "already_in_use", "email or pseudo is already in use"))
			return
		}
		writeErr(w, r, err)
		return
	}
	if err := s.sendVerificationCode(ctx, u); err != nil {
		slog.Error("sending verification email", "user", u.ID, "err", err)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": u.ID, "handle": s.handle(u)})
}

func (s *Server) sendVerificationCode(ctx context.Context, u *user) error {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	now := s.now()
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO email_codes (user_id, code_hash, attempts, created_at, expires_at)
		VALUES (?, ?, 0, ?, ?)`, u.ID, sha256Hex(u.ID+":"+code), now.Unix(), now.Add(emailCodeTTL).Unix())
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Bonjour %s,\n\nVotre code de vérification Quarel : %s\n\nIl expire dans %d minutes.\n",
		u.Pseudo, code, int(emailCodeTTL.Minutes()))
	return s.mailer.Send(u.Email, "Quarel — code de vérification", body)
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Code string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	invalid := errf(http.StatusBadRequest, "invalid_code", "invalid or expired code; request a new one if needed")
	ctx := r.Context()
	email, _ := normalizeEmail(req.Email)
	u, err := s.userBy(ctx, "email", email)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if u == nil {
		writeErr(w, r, invalid)
		return
	}
	if u.EmailVerifiedAt.Valid {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var hash string
	var attempts int
	var expires int64
	err = s.db.QueryRowContext(ctx, `SELECT code_hash, attempts, expires_at FROM email_codes WHERE user_id = ?`, u.ID).
		Scan(&hash, &attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (attempts >= emailCodeMaxAttempts || s.now().Unix() > expires)) {
		writeErr(w, r, invalid)
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(sha256Hex(u.ID+":"+strings.TrimSpace(req.Code)))) != 1 {
		s.db.ExecContext(ctx, `UPDATE email_codes SET attempts = attempts + 1 WHERE user_id = ?`, u.ID)
		writeErr(w, r, invalid)
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET email_verified_at = ? WHERE id = ?`, s.now().Unix(), u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.db.ExecContext(ctx, `DELETE FROM email_codes WHERE user_id = ?`, u.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleResendVerification always answers 202 so it cannot be used to probe which emails exist.
func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	email, _ := normalizeEmail(req.Email)
	u, err := s.userBy(ctx, "email", email)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if u != nil && !u.EmailVerifiedAt.Valid {
		var created int64
		err := s.db.QueryRowContext(ctx, `SELECT created_at FROM email_codes WHERE user_id = ?`, u.ID).Scan(&created)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && s.now().Unix()-created >= int64(emailResendCooldown.Seconds())) {
			if err := s.sendVerificationCode(ctx, u); err != nil {
				slog.Error("resending verification email", "user", u.ID, "err", err)
			}
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// --- login & sessions ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login      string `json:"login"` // email or pseudo
		Password   string `json:"password"`
		TOTPCode   string `json:"totp_code"`
		DeviceName string `json:"device_name"`
		DeviceKey  string `json:"device_key"` // base64url Ed25519 public key generated by the client
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := idtoken.DecodeKey(req.DeviceKey); err != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_device_key", "device_key must be a base64url Ed25519 public key"))
		return
	}
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	if req.DeviceName == "" {
		req.DeviceName = "unknown device"
	}
	if len(req.DeviceName) > 64 {
		req.DeviceName = req.DeviceName[:64]
	}

	ctx := r.Context()
	login := strings.ToLower(strings.TrimSpace(req.Login))
	column := "pseudo_norm"
	if strings.Contains(login, "@") {
		column = "email"
	}
	u, err := s.userBy(ctx, column, login)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	key := lockoutKey(u, login)
	err = s.guarded(ctx, key, func() error {
		badCreds := errf(http.StatusUnauthorized, "invalid_credentials", "invalid login or password")
		if u == nil {
			verifyPassword(req.Password, dummyHash())
			return badCreds
		}
		if ok, err := verifyPassword(req.Password, u.PasswordHash); err != nil {
			return err
		} else if !ok {
			return badCreds
		}
		if u.DisabledAt.Valid {
			return errf(http.StatusForbidden, "account_disabled", "this account has been disabled")
		}
		if !u.EmailVerifiedAt.Valid {
			return errf(http.StatusForbidden, "email_not_verified", "verify your email address before logging in")
		}
		if u.TOTPEnabledAt.Valid {
			if req.TOTPCode == "" {
				return errf(http.StatusUnauthorized, "mfa_required", "a 2FA code (or backup code) is required")
			}
			return s.check2FA(ctx, u, req.TOTPCode)
		}
		return nil
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.clearFailures(ctx, key)

	token := newSecret()
	sessID := newID()
	now := s.now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, device_name, device_key, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, sessID, u.ID, sha256Hex(token), req.DeviceName, req.DeviceKey, now, now)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":    sessID,
		"session_token": token,
		"user":          s.userResp(u),
	})
}

// check2FA accepts a current TOTP code (each usable once) or an unused backup code.
func (s *Server) check2FA(ctx context.Context, u *user, code string) error {
	code = strings.TrimSpace(code)
	invalid := errf(http.StatusUnauthorized, "invalid_mfa_code", "invalid 2FA code")
	if len(code) == totpDigits {
		step, ok := checkTOTP(u.TOTPSecret.String, code, s.now(), u.TOTPLastStep)
		if !ok {
			return invalid
		}
		res, err := s.db.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, u.ID, step)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return invalid // same code used concurrently
		}
		return nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE backup_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		s.now().Unix(), u.ID, sha256Hex(normalizeBackupCode(code)))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return invalid
	}
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sessionFrom(r).ID); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) currentUser(r *http.Request) (*user, error) {
	u, err := s.userBy(r.Context(), "id", sessionFrom(r).UserID)
	if err == nil && u == nil {
		err = errors.New("session user vanished")
	}
	return u, err
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.userResp(u))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, device_name, created_at, last_seen_at FROM sessions
		WHERE user_id = ? ORDER BY created_at`, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID         string    `json:"id"`
		DeviceName string    `json:"device_name"`
		CreatedAt  time.Time `json:"created_at"`
		LastSeenAt time.Time `json:"last_seen_at"`
		Current    bool      `json:"current"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		var created, seen int64
		if err := rows.Scan(&it.ID, &it.DeviceName, &created, &seen); err != nil {
			writeErr(w, r, err)
			return
		}
		it.CreatedAt, it.LastSeenAt = time.Unix(created, 0).UTC(), time.Unix(seen, 0).UTC()
		it.Current = it.ID == sess.ID
		list = append(list, it)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ? AND user_id = ?`,
		r.PathValue("id"), sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such session"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- 2FA ---

// requirePassword re-checks the password of a logged-in user; failures count
// towards the account lockout like login failures.
func (s *Server) requirePassword(ctx context.Context, u *user, password string) error {
	return s.guarded(ctx, lockoutKey(u, ""), func() error {
		ok, err := verifyPassword(password, u.PasswordHash)
		if err != nil {
			return err
		}
		if !ok {
			return errf(http.StatusUnauthorized, "invalid_credentials", "wrong password")
		}
		return nil
	})
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requirePassword(r.Context(), u, req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	if u.TOTPEnabledAt.Valid {
		writeErr(w, r, errf(http.StatusConflict, "2fa_already_enabled", "2FA is already enabled"))
		return
	}
	secret := newTOTPSecret()
	if _, err := s.db.ExecContext(r.Context(), `UPDATE users SET totp_secret = ?, totp_last_step = 0 WHERE id = ?`, secret, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_uri": otpauthURI("Quarel ("+s.cfg.Issuer+")", s.handle(u), secret),
	})
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if u.TOTPEnabledAt.Valid {
		writeErr(w, r, errf(http.StatusConflict, "2fa_already_enabled", "2FA is already enabled"))
		return
	}
	if !u.TOTPSecret.Valid {
		writeErr(w, r, errf(http.StatusBadRequest, "2fa_not_setup", "call 2FA setup first"))
		return
	}
	step, ok := checkTOTP(u.TOTPSecret.String, strings.TrimSpace(req.Code), s.now(), 0)
	if !ok {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_mfa_code", "invalid 2FA code; check your authenticator app's clock"))
		return
	}

	codes := make([]string, backupCodeCount)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM backup_codes WHERE user_id = ?`, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	for i := range codes {
		codes[i] = newBackupCode()
		if _, err := tx.Exec(`INSERT INTO backup_codes (user_id, code_hash) VALUES (?, ?)`, u.ID, sha256Hex(normalizeBackupCode(codes[i]))); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(`UPDATE users SET totp_enabled_at = ?, totp_last_step = ? WHERE id = ?`, s.now().Unix(), step, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup_codes": codes})
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !u.TOTPEnabledAt.Valid {
		writeErr(w, r, errf(http.StatusBadRequest, "2fa_not_enabled", "2FA is not enabled"))
		return
	}
	if err := s.requirePassword(r.Context(), u, req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.guarded(r.Context(), lockoutKey(u, ""), func() error { return s.check2FA(r.Context(), u, req.Code) }); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL, totp_last_step = 0 WHERE id = ?`, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.db.ExecContext(ctx, `DELETE FROM backup_codes WHERE user_id = ?`, u.ID)
	w.WriteHeader(http.StatusNoContent)
}

// --- identity tokens ---

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	deviceKey, err := idtoken.DecodeKey(sessionFrom(r).DeviceKey)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	token, exp, err := s.signer.Issue(u.ID, s.handle(u), deviceKey, s.cfg.TokenTTL, s.now())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": exp.UTC()})
}
