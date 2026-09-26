package community

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/internal/tlsconf"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// --- login challenges ---

const (
	nonceTTL = 2 * time.Minute
	maxUsed  = 100_000
)

// nonceStore issues login challenges without keeping them: a nonce carries
// its expiry and a MAC by a key of this process, so asking for challenges
// costs nothing and cannot exhaust anything. Only nonces actually used by a
// valid login are remembered (until they expire), so each works once.
type nonceStore struct {
	key  []byte
	mu   sync.Mutex
	used map[string]time.Time
}

func newNonceStore() *nonceStore {
	key := make([]byte, 32)
	rand.Read(key)
	return &nonceStore{key: key, used: map[string]time.Time{}}
}

func (n *nonceStore) mac(payload []byte) []byte {
	m := hmac.New(sha256.New, n.key)
	m.Write(payload)
	return m.Sum(nil)[:16]
}

func (n *nonceStore) issue(now time.Time) (string, time.Time) {
	exp := now.Add(nonceTTL)
	payload := make([]byte, 24)
	rand.Read(payload[:16])
	binary.BigEndian.PutUint64(payload[16:], uint64(exp.UnixMilli()))
	return base64.RawURLEncoding.EncodeToString(append(payload, n.mac(payload)...)), exp
}

// valid reports whether nonce was issued here and has not expired.
func (n *nonceStore) valid(nonce string, now time.Time) bool {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != 40 || !hmac.Equal(raw[24:], n.mac(raw[:24])) {
		return false
	}
	return now.UnixMilli() <= int64(binary.BigEndian.Uint64(raw[16:24]))
}

// consume marks a valid nonce as used; false if it already was (or, in the
// unlikely event of a flood of valid logins, if too many are remembered).
func (n *nonceStore) consume(nonce string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, seen := n.used[nonce]; seen {
		return false
	}
	if len(n.used) >= maxUsed {
		for k, exp := range n.used {
			if now.After(exp) {
				delete(n.used, k)
			}
		}
		if len(n.used) >= maxUsed {
			return false
		}
	}
	n.used[nonce] = now.Add(nonceTTL)
	return true
}

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	nonce, exp := s.nonces.issue(s.now())
	writeJSON(w, http.StatusOK, map[string]any{"server_id": s.id, "nonce": nonce, "expires_at": exp.UTC()})
}

// --- login ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdentityToken string `json:"identity_token"`
		Nonce         string `json:"nonce"`
		Proof         string `json:"proof"`  // idtoken.SignProofV2(device, server_id, nonce, host, tls)
		Host          string `json:"host"`   // the host the client connected to ("" with a legacy v1 proof)
		TLS           string `json:"tls"`    // how the client checked it: "binding" or "authority"
		Invite        string `json:"invite"` // required to join a private server
		Claim         string `json:"claim"`  // one-time owner claim code
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	badNonce := errf(http.StatusUnauthorized, "invalid_nonce", "unknown, expired or already used challenge; request a new one")
	if !s.nonces.valid(req.Nonce, s.now()) {
		writeErr(w, r, badNonce)
		return
	}
	claims, err := s.verifyIdentity(ctx, req.IdentityToken)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.checkProof(claims, req.Nonce, req.Proof, req.Host, req.TLS); err != nil {
		writeErr(w, r, err)
		return
	}
	if !s.nonces.consume(req.Nonce, s.now()) {
		writeErr(w, r, badNonce)
		return
	}
	if s.isDisabled(claims.Issuer, claims.Subject) {
		writeErr(w, r, errf(http.StatusForbidden, "account_disabled", "this account has been disabled by its identity service"))
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
	view, err := s.memberView(ctx, m)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if joined {
		s.hub.Broadcast("MEMBER_JOIN", view)
	}
	info, err := s.info(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": token,
		"expires_at":    expires.UTC(),
		"member":        view,
		"server":        info,
		"joined":        joined,
	})
}

// checkProof verifies the device proof of a login. A v2 proof names the host
// the client connected to and how it checked it:
//   - "binding": every connection presented this server's certificate bound
//     to its identity (pkg/tlsbind), which only this server can present;
//   - "authority": an ordinary certificate (or plain HTTP on the same
//     machine), which proves nothing about the server ID: the host must then
//     be one of this server's names. Otherwise another server, holding a
//     valid certificate for its own name, relayed the login here.
//
// Legacy v1 proofs (older clients) are still accepted for now.
func (s *Server) checkProof(claims *idtoken.Claims, nonce, proof, host, mode string) error {
	invalid := errf(http.StatusUnauthorized, "invalid_proof", "device proof does not match the identity token")
	if host == "" && mode == "" {
		if idtoken.VerifyProof(claims, s.id, nonce, proof) != nil {
			return invalid
		}
		slog.Debug("login with a legacy (v1) proof", "subject", claims.Subject)
		return nil
	}
	if idtoken.VerifyProofV2(claims, s.id, nonce, host, mode, proof) != nil {
		return invalid
	}
	switch mode {
	case idtoken.TLSBinding:
		if s.cfg.TLS.Mode != tlsconf.SelfSigned {
			return errf(http.StatusForbidden, "wrong_host", "this server does not use a certificate bound to its identity: the login went through another server")
		}
	case idtoken.TLSAuthority:
		if !s.servesHost(host) {
			slog.Warn("login refused: the client connected to another address (relayed login, or QUAREL_TLS_HOSTS to complete)", "host", host)
			return errf(http.StatusForbidden, "wrong_host", "this server is not reachable as %s: the login went through another server, or its host must add this name to QUAREL_TLS_HOSTS", host)
		}
	}
	return nil
}

// verifyIdentity checks an identity token against its issuer's published keys.
func (s *Server) verifyIdentity(ctx context.Context, token string) (*idtoken.Claims, error) {
	if idtoken.IsSealed(token) { // from an Identity service that approved this server
		plain, err := idtoken.Open(token, s.enc, s.id)
		if err != nil {
			return nil, errf(http.StatusUnauthorized, "invalid_token", "sealed identity token not meant for this server")
		}
		token = plain
	}
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
	if !claims.ForAudience(s.id) {
		return nil, errf(http.StatusUnauthorized, "wrong_audience", "this identity token is for another server")
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
	if m != nil {
		var banned bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM bans WHERE member_id = ?)`, m.ID).Scan(&banned); err != nil {
			return nil, false, err
		}
		if banned {
			return nil, false, errf(http.StatusForbidden, "banned", "you are banned from this server")
		}
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
	if strings.HasPrefix(token, botTokenPrefix) {
		m, err := s.botForToken(ctx, token)
		return m, botExpiry, err
	}
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
