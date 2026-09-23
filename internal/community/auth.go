package community

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// --- login challenges ---

const (
	nonceTTL  = 2 * time.Minute
	maxNonces = 10000
)

// nonceStore holds outstanding login challenges; each can be consumed once.
type nonceStore struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func newNonceStore() *nonceStore { return &nonceStore{m: map[string]time.Time{}} }

func (n *nonceStore) issue(now time.Time) (string, time.Time, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.m) >= maxNonces/2 {
		for k, exp := range n.m {
			if now.After(exp) {
				delete(n.m, k)
			}
		}
	}
	if len(n.m) >= maxNonces {
		return "", time.Time{}, false
	}
	nonce := secret.NewToken()
	exp := now.Add(nonceTTL)
	n.m[nonce] = exp
	return nonce, exp, true
}

func (n *nonceStore) consume(nonce string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	exp, ok := n.m[nonce]
	delete(n.m, nonce)
	return ok && !now.After(exp)
}

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	nonce, exp, ok := s.nonces.issue(s.now())
	if !ok {
		writeErr(w, r, errf(http.StatusTooManyRequests, "busy", "too many pending logins, retry shortly"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server_id": s.id, "nonce": nonce, "expires_at": exp.UTC()})
}

// --- login ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdentityToken string `json:"identity_token"`
		Nonce         string `json:"nonce"`
		Proof         string `json:"proof"`  // idtoken.SignProof(device, server_id, nonce)
		Invite        string `json:"invite"` // required to join a private server
		Claim         string `json:"claim"`  // one-time owner claim code
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if !s.nonces.consume(req.Nonce, s.now()) {
		writeErr(w, r, errf(http.StatusUnauthorized, "invalid_nonce", "unknown or expired challenge; request a new one"))
		return
	}
	claims, err := s.verifyIdentity(ctx, req.IdentityToken)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := idtoken.VerifyProof(claims, s.id, req.Nonce, req.Proof); err != nil {
		writeErr(w, r, errf(http.StatusUnauthorized, "invalid_proof", "device proof does not match the identity token"))
		return
	}

	m, joined, err := s.admit(ctx, claims, strings.TrimSpace(req.Invite), strings.TrimSpace(req.Claim))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	token := secret.NewToken()
	expires := claims.ExpiresAt.Time
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		secret.SHA256Hex(token), m.ID, s.nowMs(), expires.UnixMilli()); err != nil {
		writeErr(w, r, err)
		return
	}
	s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.nowMs())
	if joined {
		s.hub.broadcast("MEMBER_JOIN", m.json())
	}
	info, err := s.info(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": token,
		"expires_at":    expires.UTC(),
		"member":        m.json(),
		"server":        info,
		"joined":        joined,
	})
}

// verifyIdentity checks an identity token against its issuer's published keys.
func (s *Server) verifyIdentity(ctx context.Context, token string) (*idtoken.Claims, error) {
	issuer, err := idtoken.PeekIssuer(token)
	if err != nil {
		return nil, errf(http.StatusUnauthorized, "invalid_token", "malformed identity token")
	}
	if !s.cfg.trusts(issuer) {
		return nil, errf(http.StatusForbidden, "untrusted_issuer", "this server does not accept identities from %s", issuer)
	}
	unavailable := errf(http.StatusServiceUnavailable, "issuer_unavailable", "cannot fetch the keys of %s", issuer)
	ks, err := s.keys.KeySet(ctx, issuer, false)
	if err != nil {
		return nil, unavailable
	}
	claims, err := idtoken.Verify(token, ks, s.now())
	if err != nil && strings.Contains(err.Error(), "unknown key id") {
		// The issuer may have rotated its key since we cached it.
		if ks, err = s.keys.KeySet(ctx, issuer, true); err != nil {
			return nil, unavailable
		}
		claims, err = idtoken.Verify(token, ks, s.now())
	}
	if err != nil {
		return nil, errf(http.StatusUnauthorized, "invalid_token", "identity token rejected: %v", err)
	}
	return claims, nil
}

// admit returns the member for claims, making them join (or claim ownership) if needed.
// joined reports whether they just (re)joined.
func (s *Server) admit(ctx context.Context, claims *idtoken.Claims, invite, claim string) (m *member, joined bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	m, err = memberBy(ctx, tx, `issuer = ? AND subject = ?`, claims.Issuer, claims.Subject)
	if err != nil {
		return nil, false, err
	}
	now := s.nowMs()
	active := m != nil && !m.LeftAt.Valid

	makeOwner := false
	if claim != "" {
		var hash string
		err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'claim_code_hash'`).Scan(&hash)
		if errors.Is(err, sql.ErrNoRows) || err == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(secret.SHA256Hex(claim))) != 1 {
			return nil, false, errf(http.StatusForbidden, "invalid_claim", "invalid or already used claim code")
		}
		if err != nil {
			return nil, false, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'claim_code_hash'`); err != nil {
			return nil, false, err
		}
		makeOwner = true
	} else if !active {
		// Read through tx: the pool has a single connection, which tx holds.
		var access string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'access'`).Scan(&access); err != nil {
			return nil, false, err
		}
		if access != accessPublic || invite != "" {
			if invite == "" {
				return nil, false, errf(http.StatusForbidden, "invite_required", "this server is private: an invite is required to join")
			}
			res, err := tx.ExecContext(ctx, `
				UPDATE invites SET uses = uses + 1
				WHERE code = ? AND (expires_at IS NULL OR expires_at > ?) AND (max_uses IS NULL OR uses < max_uses)`,
				strings.ToLower(invite), now)
			if err != nil {
				return nil, false, err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return nil, false, errf(http.StatusForbidden, "invalid_invite", "invalid, expired or exhausted invite")
			}
		}
	}

	switch {
	case m == nil:
		m = &member{ID: secret.NewID(), Issuer: claims.Issuer, Subject: claims.Subject, Handle: claims.Handle, IsOwner: makeOwner, JoinedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO members (id, issuer, subject, handle, is_owner, joined_at) VALUES (?, ?, ?, ?, ?, ?)`,
			m.ID, m.Issuer, m.Subject, m.Handle, m.IsOwner, m.JoinedAt)
		joined = true
	case !active:
		m.Handle, m.JoinedAt, m.LeftAt, m.Nickname = claims.Handle, now, sql.NullInt64{}, sql.NullString{}
		m.IsOwner = m.IsOwner || makeOwner
		_, err = tx.ExecContext(ctx, `UPDATE members SET handle = ?, nickname = NULL, joined_at = ?, left_at = NULL, is_owner = ? WHERE id = ?`,
			m.Handle, now, m.IsOwner, m.ID)
		joined = true
	default:
		m.Handle = claims.Handle
		m.IsOwner = m.IsOwner || makeOwner
		_, err = tx.ExecContext(ctx, `UPDATE members SET handle = ?, is_owner = ? WHERE id = ?`, m.Handle, m.IsOwner, m.ID)
	}
	if err != nil {
		return nil, false, err
	}
	return m, joined, tx.Commit()
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = ?`, secret.SHA256Hex(bearer(r))); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- session middleware ---

type memberKey struct{}

func bearer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token
}

// memberForToken returns the active member owning a session token, and the session expiry.
func (s *Server) memberForToken(ctx context.Context, token string) (*member, time.Time, error) {
	var memberID string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT member_id, expires_at FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		secret.SHA256Hex(token), s.nowMs()).Scan(&memberID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	m, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, memberID)
	return m, fromMs(expires), err
}

func (s *Server) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeErr(w, r, errf(http.StatusUnauthorized, "unauthorized", "missing bearer token"))
			return
		}
		m, _, err := s.memberForToken(r.Context(), token)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if m == nil {
			writeErr(w, r, errf(http.StatusUnauthorized, "unauthorized", "invalid or expired session; log in again"))
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), memberKey{}, m)))
	}
}

func memberFrom(r *http.Request) *member { return r.Context().Value(memberKey{}).(*member) }

// ownerOnly restricts an endpoint to the owner until roles exist (milestone 3).
func (s *Server) ownerOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !memberFrom(r).IsOwner {
			writeErr(w, r, errf(http.StatusForbidden, "forbidden", "only the server owner can do this"))
			return
		}
		h(w, r)
	}
}
