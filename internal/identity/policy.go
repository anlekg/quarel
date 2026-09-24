package identity

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/pkg/idtoken"
	"github.com/anlekg/quarel/pkg/tlsbind"
)

// Registration and community-server policy of this Identity service.

const userInviteTTL = 30 * 24 * time.Hour

// handlePolicy is public: apps show the right sign-up form and know whether
// tokens must name the server they are for.
func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	domains := s.cfg.RegistrationDomains
	if domains == nil {
		domains = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":        s.cfg.Issuer,
		"registration":  s.cfg.Registration,
		"email_domains": domains,
		"server_policy": s.cfg.ServerPolicy,
	})
}

// checkRegistration applies the registration rules to a new account; it
// returns the invitation to consume ("" when none is needed).
func (s *Server) checkRegistration(ctx context.Context, email, invite string) (string, error) {
	switch s.cfg.Registration {
	case "closed":
		return "", errf(http.StatusForbidden, "registration_closed", "this service does not accept new accounts")
	}
	if len(s.cfg.RegistrationDomains) > 0 {
		_, domain, _ := strings.Cut(email, "@")
		if !slices.Contains(s.cfg.RegistrationDomains, domain) {
			return "", errf(http.StatusForbidden, "email_domain_not_allowed", "accounts are limited to some email domains")
		}
	}
	if s.cfg.MaxAccounts > 0 {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
			return "", err
		}
		if n >= s.cfg.MaxAccounts {
			return "", errf(http.StatusForbidden, "account_limit_reached", "this service has reached its number of accounts")
		}
	}
	if s.cfg.Registration != "invite" {
		return "", nil
	}
	invite = strings.ToLower(strings.TrimSpace(invite))
	if invite == "" {
		return "", errf(http.StatusForbidden, "invite_required", "an invitation code is required")
	}
	ok, err := s.inviteUsable(ctx, s.db, invite)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errf(http.StatusForbidden, "invalid_invite", "unknown, expired or used invitation code")
	}
	return invite, nil
}

func (s *Server) inviteUsable(ctx context.Context, q querier, code string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM registration_invites
		WHERE code = ? AND (max_uses = 0 OR uses < max_uses) AND (expires_at IS NULL OR expires_at > ?)`,
		code, s.now().Unix()).Scan(&n)
	return n == 1, err
}

// consumeInvite uses one use of code inside tx and returns who created it.
func (s *Server) consumeInvite(ctx context.Context, tx *sql.Tx, code string) (string, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE registration_invites SET uses = uses + 1
		WHERE code = ? AND (max_uses = 0 OR uses < max_uses) AND (expires_at IS NULL OR expires_at > ?)`, code, s.now().Unix())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", errf(http.StatusForbidden, "invalid_invite", "unknown, expired or used invitation code")
	}
	var by sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT created_by FROM registration_invites WHERE code = ?`, code).Scan(&by); err != nil {
		return "", err
	}
	if !by.Valid {
		return "operator", nil
	}
	return by.String, nil
}

// Invite is a registration invitation.
type Invite struct {
	Code      string     `json:"code"`
	CreatedBy string     `json:"created_by,omitempty"` // pseudo; "" for the operator
	Note      string     `json:"note,omitempty"`
	MaxUses   int        `json:"max_uses"`
	Uses      int        `json:"uses"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func createInvite(ctx context.Context, q querier, createdBy, note string, maxUses int, expires *time.Time, now time.Time) (Invite, error) {
	inv := Invite{Code: secret.Code(10), Note: note, MaxUses: maxUses, CreatedAt: now.UTC().Truncate(time.Second), ExpiresAt: expires}
	var by, exp any
	if createdBy != "" {
		by = createdBy
	}
	if expires != nil {
		exp = expires.Unix()
	}
	_, err := q.ExecContext(ctx, `INSERT INTO registration_invites (code, created_by, note, max_uses, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		inv.Code, by, note, maxUses, now.Unix(), exp)
	return inv, err
}

func listInvites(ctx context.Context, q querier, createdBy *string) ([]Invite, error) {
	query := `SELECT i.code, COALESCE(u.pseudo, ''), i.note, i.max_uses, i.uses, i.created_at, i.expires_at
		FROM registration_invites i LEFT JOIN users u ON u.id = i.created_by`
	var args []any
	if createdBy != nil {
		query += ` WHERE i.created_by = ?`
		args = append(args, *createdBy)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY i.created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		var created int64
		var exp sql.NullInt64
		if err := rows.Scan(&inv.Code, &inv.CreatedBy, &inv.Note, &inv.MaxUses, &inv.Uses, &created, &exp); err != nil {
			return nil, err
		}
		inv.CreatedAt = time.Unix(created, 0).UTC()
		if exp.Valid {
			t := time.Unix(exp.Int64, 0).UTC()
			inv.ExpiresAt = &t
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// --- a user's own invitations (quota QUAREL_USER_INVITES) ---

func (s *Server) handleMyInvites(w http.ResponseWriter, r *http.Request) {
	uid := sessionFrom(r).UserID
	list, err := listInvites(r.Context(), s.db, &uid)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": s.cfg.UserInvites, "created": len(list), "registration": s.cfg.Registration, "invites": list})
}

func (s *Server) handleCreateMyInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := sessionFrom(r).UserID
	if s.cfg.Registration == "closed" {
		writeErr(w, r, errf(http.StatusForbidden, "registration_closed", "this service does not accept new accounts"))
		return
	}
	var created int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM registration_invites WHERE created_by = ?`, uid).Scan(&created); err != nil {
		writeErr(w, r, err)
		return
	}
	if created >= s.cfg.UserInvites {
		writeErr(w, r, errf(http.StatusForbidden, "invite_quota_reached", "no invitations left (%d allowed)", s.cfg.UserInvites))
		return
	}
	exp := s.now().Add(userInviteTTL)
	inv, err := createInvite(ctx, s.db, uid, "", 1, &exp, s.now())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) handleDeleteMyInvite(w http.ResponseWriter, r *http.Request) {
	// Revoking does not give the invitation back: the quota counts created ones.
	_, err := s.db.ExecContext(r.Context(), `UPDATE registration_invites SET expires_at = ? WHERE code = ? AND created_by = ?`,
		s.now().Unix(), r.PathValue("code"), sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- community servers ---

// handleBlockedServers is public: apps refuse to connect to these servers.
func (s *Server) handleBlockedServers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT server_id, reason FROM blocked_servers ORDER BY server_id`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID     string `json:"id"`
		Reason string `json:"reason,omitempty"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		list = append(list, it)
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"issuer": s.cfg.Issuer, "servers": list})
}

func (s *Server) serverBlocked(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocked_servers WHERE server_id = ?`, id).Scan(&n)
	return n > 0, err
}

// handleServerRequest records a community server's request to be approved
// (signed with its identity key; the operator decides).
func (s *Server) handleServerRequest(w http.ResponseWriter, r *http.Request) {
	var req idtoken.ApprovalRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	req.Name, req.URL, req.Contact = strings.TrimSpace(req.Name), strings.TrimSpace(req.URL), strings.TrimSpace(req.Contact)
	if req.Name == "" || len(req.Name) > 100 || len(req.URL) > 200 || len(req.Contact) > 200 {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_request", "name (1-100), url and contact (200 max) expected"))
		return
	}
	pub, err := idtoken.VerifyApproval(s.cfg.Issuer, req)
	if err != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_signature", "%v", err))
		return
	}
	id := serverIDOf(pub)
	ctx := r.Context()
	now := s.now().Unix()
	var status, encKey string
	err = s.db.QueryRowContext(ctx, `SELECT status, enc_key FROM server_approvals WHERE server_id = ?`, id).Scan(&status, &encKey)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.db.ExecContext(ctx, `INSERT INTO server_approvals (server_id, server_key, enc_key, name, url, contact, status, requested_at) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
			id, req.ServerKey, req.EncKey, req.Name, req.URL, req.Contact, now)
		status = "pending"
	case err == nil:
		// Details may change; a new encryption key needs a new decision.
		if encKey != req.EncKey {
			status = "pending"
		}
		_, err = s.db.ExecContext(ctx, `UPDATE server_approvals SET enc_key = ?, name = ?, url = ?, contact = ?, status = ?, requested_at = ? WHERE server_id = ?`,
			req.EncKey, req.Name, req.URL, req.Contact, status, now, id)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"server_id": id, "status": status})
}

// handleServerRequestStatus is public: a server's administration page shows it.
func (s *Server) handleServerRequestStatus(w http.ResponseWriter, r *http.Request) {
	var status string
	err := s.db.QueryRowContext(r.Context(), `SELECT status FROM server_approvals WHERE server_id = ?`, r.PathValue("id")).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		status = "none"
	} else if err != nil {
		writeErr(w, r, err)
		return
	}
	blocked, err := s.serverBlocked(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "blocked": blocked, "server_policy": s.cfg.ServerPolicy})
}

// tokenFor checks that a token may be issued for the community server
// audience and returns the key to seal it to (nil: plain token).
func (s *Server) tokenFor(ctx context.Context, audience string) ([]byte, error) {
	if audience != "" {
		if blocked, err := s.serverBlocked(ctx, audience); err != nil {
			return nil, err
		} else if blocked {
			return nil, errf(http.StatusForbidden, "server_blocked", "this community server is blocked by the identity service")
		}
	}
	if s.cfg.ServerPolicy != "approved" {
		return nil, nil
	}
	if audience == "" {
		return nil, errf(http.StatusBadRequest, "audience_required", "this service only issues tokens for approved servers: give the server ID")
	}
	var enc string
	err := s.db.QueryRowContext(ctx, `SELECT enc_key FROM server_approvals WHERE server_id = ? AND status = 'approved'`, audience).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusForbidden, "server_not_approved", "this community server is not approved by the identity service")
	}
	if err != nil {
		return nil, err
	}
	return b64url.DecodeString(enc)
}

var b64url = base64.RawURLEncoding

// serverIDOf is the public ID of a community server (as in its invite links).
func serverIDOf(pub ed25519.PublicKey) string { return tlsbind.ServerID(pub) }
