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
	"strings"
	"time"
)

// Account management: forgotten password, changes of password, email and
// pseudo, and deletion of the account with all its data.

const pseudoChangeInterval = 24 * time.Hour

// sendCode stores a fresh 6-digit code for purpose and emails it to address.
func (s *Server) sendCode(ctx context.Context, u *user, purpose, address, data, subject, text string) error {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO account_codes (user_id, purpose, code_hash, data, attempts, created_at, expires_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`, u.ID, purpose, sha256Hex(u.ID+":"+purpose+":"+code), data, now.Unix(), now.Add(emailCodeTTL).Unix()); err != nil {
		return err
	}
	return s.mailer.Send(address, subject, fmt.Sprintf(text, u.Pseudo, code, int(emailCodeTTL.Minutes())))
}

// checkCode consumes a valid code and returns its data. Five wrong attempts
// burn the code.
func (s *Server) checkCode(ctx context.Context, u *user, purpose, code string) (string, error) {
	invalid := errf(http.StatusBadRequest, "invalid_code", "invalid or expired code; request a new one if needed")
	var hash, data string
	var attempts int
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT code_hash, data, attempts, expires_at FROM account_codes WHERE user_id = ? AND purpose = ?`,
		u.ID, purpose).Scan(&hash, &data, &attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (attempts >= emailCodeMaxAttempts || s.now().Unix() > expires) {
		return "", invalid
	}
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(sha256Hex(u.ID+":"+purpose+":"+strings.TrimSpace(code)))) != 1 {
		s.db.ExecContext(ctx, `UPDATE account_codes SET attempts = attempts + 1 WHERE user_id = ? AND purpose = ?`, u.ID, purpose)
		return "", invalid
	}
	s.db.ExecContext(ctx, `DELETE FROM account_codes WHERE user_id = ? AND purpose = ?`, u.ID, purpose)
	return data, nil
}

// --- forgotten password ---

// handleForgotPassword always answers 202: it must not reveal which emails exist.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
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
	if u != nil && u.EmailVerifiedAt.Valid && !u.DisabledAt.Valid {
		var created int64
		err := s.db.QueryRowContext(ctx, `SELECT created_at FROM account_codes WHERE user_id = ? AND purpose = 'reset'`, u.ID).Scan(&created)
		if errors.Is(err, sql.ErrNoRows) || err == nil && s.now().Unix()-created >= int64(emailResendCooldown.Seconds()) {
			if err := s.sendCode(ctx, u, "reset", u.Email, "", "Quarel — réinitialisation du mot de passe",
				"Bonjour %s,\n\nVotre code pour choisir un nouveau mot de passe Quarel : %s\n\nIl expire dans %d minutes. "+
					"Si vous n'avez rien demandé, ignorez ce message : votre mot de passe reste inchangé.\n"); err != nil {
				slog.Error("sending password reset email", "user", u.ID, "err", err)
			}
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleResetPassword sets a new password with the emailed code (and the 2FA
// code when 2FA is on: a mailbox alone must not be enough). Every session is
// closed.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := checkPassword(req.Password); err != nil {
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
	if u == nil || u.DisabledAt.Valid {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_code", "invalid or expired code; request a new one if needed"))
		return
	}
	if u.TOTPEnabledAt.Valid {
		if req.TOTPCode == "" {
			writeErr(w, r, errf(http.StatusUnauthorized, "mfa_required", "a 2FA code (or backup code) is required"))
			return
		}
		if err := s.guarded(r, lockoutKey(u, ""), func() error { return s.check2FA(ctx, u, req.TOTPCode) }); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if _, err := s.checkCode(ctx, u, "reset", req.Code); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.setPassword(ctx, u, req.Password, ""); err != nil {
		writeErr(w, r, err)
		return
	}
	s.clearFailures(r, lockoutKey(u, ""))
	s.mailer.Send(u.Email, "Quarel — mot de passe modifié", fmt.Sprintf(
		"Bonjour %s,\n\nLe mot de passe de votre compte Quarel vient d'être réinitialisé et tous vos appareils ont été déconnectés.\n", u.Pseudo))
	w.WriteHeader(http.StatusNoContent)
}

// setPassword stores a new password and closes every session except keep.
func (s *Server) setPassword(ctx context.Context, u *user, password, keep string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, u.ID); err != nil {
		return err
	}
	return s.closeSessions(ctx, u.ID, keep)
}

// closeSessions ends every session of a user except keep (their devices,
// keys and mailboxes go with them).
func (s *Server) closeSessions(ctx context.Context, userID, keep string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions WHERE user_id = ? AND id != ?`, userID, keep)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keep); err != nil {
		return err
	}
	for _, id := range ids {
		s.hub.Disconnect(func(key, _ string) bool { return key == id }, "session closed")
	}
	if len(ids) > 0 {
		s.devicesChanged(ctx, userID)
	}
	return nil
}

// --- changes while logged in ---

// handleChangePassword: POST /v1/me/password {current_password, new_password}.
// Other sessions are closed; this one stays.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := checkPassword(req.NewPassword); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requirePassword(r, u, req.CurrentPassword); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.setPassword(r.Context(), u, req.NewPassword, sessionFrom(r).ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.mailer.Send(u.Email, "Quarel — mot de passe modifié", fmt.Sprintf(
		"Bonjour %s,\n\nLe mot de passe de votre compte Quarel vient d'être changé ; vos autres appareils ont été déconnectés.\n"+
			"Si ce n'est pas vous, réinitialisez-le immédiatement.\n", u.Pseudo))
	w.WriteHeader(http.StatusNoContent)
}

// handleChangeEmail: POST /v1/me/email {password, new_email} sends a code to the new address.
func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		NewEmail string `json:"new_email"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	email, err := normalizeEmail(req.NewEmail)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requirePassword(r, u, req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if other, err := s.userBy(ctx, "email", email); err != nil {
		writeErr(w, r, err)
		return
	} else if other != nil {
		writeErr(w, r, errf(http.StatusConflict, "email_taken", "email is already in use"))
		return
	}
	if err := s.sendCode(ctx, u, "email", email, email, "Quarel — confirmation de votre nouvelle adresse",
		"Bonjour %s,\n\nVotre code pour confirmer cette adresse sur Quarel : %s\n\nIl expire dans %d minutes.\n"); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleConfirmEmail: POST /v1/me/email/confirm {code}. The old address is told.
func (s *Server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
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
	ctx := r.Context()
	email, err := s.checkCode(ctx, u, "email", req.Code)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET email = ?, email_verified_at = ? WHERE id = ?`, email, s.now().Unix(), u.ID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			err = errf(http.StatusConflict, "email_taken", "email is already in use")
		}
		writeErr(w, r, err)
		return
	}
	s.mailer.Send(u.Email, "Quarel — adresse email modifiée", fmt.Sprintf(
		"Bonjour %s,\n\nL'adresse email de votre compte Quarel a été remplacée par une autre. Si ce n'est pas vous, contactez le support de votre service Quarel.\n", u.Pseudo))
	u.Email = email
	writeJSON(w, http.StatusOK, s.userResp(u))
}

// handleChangePseudo: PATCH /v1/me {pseudo}, at most once a day. The handle
// changes; the identity (issuer, subject) does not, so bans and memberships
// on community servers still apply.
func (s *Server) handleChangePseudo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pseudo string `json:"pseudo"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if !pseudoRe.MatchString(req.Pseudo) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_pseudo",
			"pseudo must be 3-32 characters: letters, digits, '_', '.', '-' (not at the edges)"))
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	var changed sql.NullInt64
	s.db.QueryRowContext(ctx, `SELECT pseudo_changed_at FROM users WHERE id = ?`, u.ID).Scan(&changed)
	sameName := strings.EqualFold(req.Pseudo, u.Pseudo)
	if changed.Valid && !sameName && s.now().Sub(time.Unix(changed.Int64, 0)) < pseudoChangeInterval {
		writeErr(w, r, errf(http.StatusTooManyRequests, "pseudo_change_too_soon", "the pseudo can be changed once a day"))
		return
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET pseudo = ?, pseudo_norm = ?, pseudo_changed_at = CASE WHEN ? THEN pseudo_changed_at ELSE ? END WHERE id = ?`,
		req.Pseudo, strings.ToLower(req.Pseudo), sameName, s.now().Unix(), u.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			err = errf(http.StatusConflict, "pseudo_taken", "pseudo is already in use")
		}
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writeErr(w, r, errors.New("pseudo update affected no row"))
		return
	}
	u.Pseudo = req.Pseudo
	s.userChanged(ctx, u)
	writeJSON(w, http.StatusOK, s.userResp(u))
}

// userChanged tells the user's devices and friends about their new public profile.
func (s *Server) userChanged(ctx context.Context, u *user) {
	friends, err := s.friendIDs(ctx, u.ID)
	if err != nil {
		slog.Error("listing friends", "err", err)
		return
	}
	friends[u.ID] = true
	profile, err := s.profile(ctx, u)
	if err != nil {
		slog.Error("loading profile", "err", err)
		return
	}
	s.hub.BroadcastTo("USER_UPDATE", profile, func(_, group string) bool { return friends[group] })
}

// friendIDs returns the user's accepted friends.
func (s *Server) friendIDs(ctx context.Context, me string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END FROM friendships
		WHERE (user_a = ? OR user_b = ?) AND status = 'accepted'`, me, me, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// handleDeleteAccount: DELETE /v1/me {password, totp_code?} erases the account
// and everything attached to it (sessions, device keys, friends, conversations,
// mailboxes, backup, profile). There is no undo.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.requirePassword(r, u, req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if u.TOTPEnabledAt.Valid {
		if req.TOTPCode == "" {
			writeErr(w, r, errf(http.StatusUnauthorized, "mfa_required", "a 2FA code (or backup code) is required"))
			return
		}
		if err := s.guarded(r, lockoutKey(u, ""), func() error { return s.check2FA(ctx, u, req.TOTPCode) }); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	friends, err := s.friendIDs(ctx, u.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	// Foreign keys cascade to every table holding the user's data.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, u.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.db.ExecContext(ctx, `DELETE FROM auth_failures WHERE key = ? OR key LIKE ?`, "user:"+u.ID, "user:"+u.ID+"|%")
	s.hub.Disconnect(func(_, group string) bool { return group == u.ID }, "account deleted")
	gone := s.publicUser(u)
	for id := range friends {
		s.hub.BroadcastTo("FRIENDS_UPDATE", map[string]any{"user": gone, "status": relNone},
			func(_, group string) bool { return group == id })
	}
	w.WriteHeader(http.StatusNoContent)
}
