package identity

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Passkeys (P2, decided by the PM): WebAuthn security keys and passkeys as a
// second factor, next to TOTP (which stays required: its backup codes are the
// way back in). A passkey belongs to this service's domain, which the desktop
// app (app://) and the web app (another domain) cannot use: the ceremony
// runs on a page of this service (/passkey/), opened in the browser with a
// single-use ticket in its fragment (never sent in a URL to any server).
//
//   - Adding one: POST /v1/me/passkeys {password, name} → {ticket, url}.
//   - Signing in: POST /v1/auth/login {…, passkey: true} → 202 {passkey_ticket, url}
//     once the password is right; the app then polls POST /v1/auth/login/passkey
//     {ticket} until the page has verified the key.
//   - The page: POST /v1/passkeys/options {ticket} then /v1/passkeys/finish {ticket, credential}.

const passkeyTicketTTL = 5 * time.Minute

//go:embed passkeypage
var passkeyPage embed.FS

type passkeyTicket struct {
	kind       string // "register" | "login"
	userID     string
	name       string // register: the key's name
	deviceName string // login: the session to open
	deviceKey  string
	expires    time.Time
	session    *webauthn.SessionData
	done       bool
}

type passkeyTickets struct {
	mu sync.Mutex
	m  map[string]*passkeyTicket
}

func (t *passkeyTickets) put(now time.Time, pt *passkeyTicket) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]*passkeyTicket{}
	}
	for k, v := range t.m {
		if now.After(v.expires) {
			delete(t.m, k)
		}
	}
	id := newSecret()
	pt.expires = now.Add(passkeyTicketTTL)
	t.m[id] = pt
	return id
}

// get returns a copy of a live ticket.
func (t *passkeyTickets) get(now time.Time, id string) (passkeyTicket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	pt := t.m[id]
	if pt == nil || now.After(pt.expires) {
		return passkeyTicket{}, false
	}
	return *pt, true
}

func (t *passkeyTickets) update(id string, fn func(*passkeyTicket)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pt := t.m[id]; pt != nil {
		fn(pt)
	}
}

func (t *passkeyTickets) drop(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}

// origin is where this service's pages are served: https, except on this machine.
func (s *Server) origin() string {
	host := s.cfg.Issuer
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1" {
		return "http://" + host
	}
	return "https://" + host
}

func (s *Server) webAuthn() (*webauthn.WebAuthn, error) {
	host := s.cfg.Issuer
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "Quarel (" + s.cfg.Issuer + ")",
		RPID:          host,
		RPOrigins:     []string{s.origin()},
	})
}

type passkeyUser struct {
	u     *user
	creds []webauthn.Credential
}

func (p passkeyUser) WebAuthnID() []byte                         { return []byte(p.u.ID) }
func (p passkeyUser) WebAuthnName() string                       { return p.u.Pseudo }
func (p passkeyUser) WebAuthnDisplayName() string                { return p.u.Pseudo }
func (p passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

func (s *Server) passkeyUser(ctx context.Context, u *user) (passkeyUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM webauthn_credentials WHERE user_id = ?`, u.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	defer rows.Close()
	pu := passkeyUser{u: u}
	for rows.Next() {
		var data string
		var c webauthn.Credential
		if err := rows.Scan(&data); err != nil {
			return pu, err
		}
		if json.Unmarshal([]byte(data), &c) == nil {
			pu.creds = append(pu.creds, c)
		}
	}
	return pu, rows.Err()
}

func (s *Server) passkeyURL(ticket string) string { return s.origin() + "/passkey/#" + ticket }

// --- my passkeys ---

type passkeyJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at`, sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []passkeyJSON{}
	for rows.Next() {
		var p passkeyJSON
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsedAt); err != nil {
			writeErr(w, r, err)
			return
		}
		list = append(list, p)
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAddPasskey: POST /v1/me/passkeys {password, name} → {ticket, url} to open in a browser.
func (s *Server) handleAddPasskey(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Name string }
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
		writeErr(w, r, errf(http.StatusBadRequest, "2fa_not_enabled", "turn two-factor authentication on first: its backup codes are the way back in"))
		return
	}
	if err := s.requirePassword(r, u, req.Password); err != nil {
		writeErr(w, r, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Clé d'accès"
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	var n int
	s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = ?`, u.ID).Scan(&n)
	if n >= 10 {
		writeErr(w, r, errf(http.StatusBadRequest, "too_many_passkeys", "at most 10 passkeys"))
		return
	}
	ticket := s.passkeys.put(s.now(), &passkeyTicket{kind: "register", userID: u.ID, name: name})
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket, "url": s.passkeyURL(ticket)})
}

func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, r.PathValue("id"), sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such passkey"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- the ceremony page ---

// handlePasskeyOptions: POST /v1/passkeys/options {ticket} → {kind, options} for navigator.credentials.
func (s *Server) handlePasskeyOptions(w http.ResponseWriter, r *http.Request) {
	var req struct{ Ticket string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	pt, ok := s.passkeys.get(s.now(), req.Ticket)
	if !ok || pt.done {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "this link expired: start again from the app"))
		return
	}
	ctx := r.Context()
	u, err := s.userBy(ctx, "id", pt.userID)
	if err != nil || u == nil {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "this link expired: start again from the app"))
		return
	}
	wa, err := s.webAuthn()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	pu, err := s.passkeyUser(ctx, u)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var options any
	var session *webauthn.SessionData
	if pt.kind == "register" {
		exclude := make([]protocol.CredentialDescriptor, 0, len(pu.creds))
		for _, c := range pu.creds {
			exclude = append(exclude, c.Descriptor())
		}
		options, session, err = wa.BeginRegistration(pu, webauthn.WithExclusions(exclude),
			webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred))
	} else {
		options, session, err = wa.BeginLogin(pu)
	}
	if err != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "no_passkey", "no passkey on this account"))
		return
	}
	s.passkeys.update(req.Ticket, func(t *passkeyTicket) { t.session = session })
	writeJSON(w, http.StatusOK, map[string]any{"kind": pt.kind, "name": u.Pseudo, "options": options})
}

// handlePasskeyFinish: POST /v1/passkeys/finish {ticket, credential}.
func (s *Server) handlePasskeyFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket     string          `json:"ticket"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	pt, ok := s.passkeys.get(s.now(), req.Ticket)
	if !ok || pt.done || pt.session == nil {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "this link expired: start again from the app"))
		return
	}
	ctx := r.Context()
	u, err := s.userBy(ctx, "id", pt.userID)
	if err != nil || u == nil {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "this link expired: start again from the app"))
		return
	}
	wa, err := s.webAuthn()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	pu, err := s.passkeyUser(ctx, u)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	failed := errf(http.StatusUnauthorized, "invalid_passkey", "the key could not be verified")
	if pt.kind == "register" {
		parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
		if err != nil {
			writeErr(w, r, failed)
			return
		}
		cred, err := wa.CreateCredential(pu, *pt.session, parsed)
		if err != nil {
			s.passkeys.drop(req.Ticket)
			writeErr(w, r, failed)
			return
		}
		data, _ := json.Marshal(cred)
		if _, err := s.db.ExecContext(ctx, `INSERT INTO webauthn_credentials (id, user_id, name, data, created_at) VALUES (?, ?, ?, ?, ?)`,
			b64url.EncodeToString(cred.ID), u.ID, pt.name, string(data), s.now().Unix()); err != nil {
			writeErr(w, r, err)
			return
		}
		s.passkeys.drop(req.Ticket)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		writeErr(w, r, failed)
		return
	}
	cred, err := wa.ValidateLogin(pu, *pt.session, parsed)
	if err != nil {
		s.passkeys.drop(req.Ticket) // one try per ticket: the app starts again
		writeErr(w, r, failed)
		return
	}
	data, _ := json.Marshal(cred)
	s.db.ExecContext(ctx, `UPDATE webauthn_credentials SET data = ?, last_used_at = ? WHERE id = ? AND user_id = ?`,
		string(data), s.now().Unix(), b64url.EncodeToString(cred.ID), u.ID)
	s.passkeys.update(req.Ticket, func(t *passkeyTicket) { t.done = true })
	w.WriteHeader(http.StatusNoContent)
}

// handlePasskeyLogin: POST /v1/auth/login/passkey {ticket} → 202 while the
// key is not verified, then the session (once).
func (s *Server) handlePasskeyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Ticket string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	pt, ok := s.passkeys.get(s.now(), req.Ticket)
	if !ok || pt.kind != "login" {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "the passkey check expired or failed: sign in again"))
		return
	}
	if !pt.done {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	s.passkeys.drop(req.Ticket)
	u, err := s.userBy(r.Context(), "id", pt.userID)
	if err != nil || u == nil || u.DisabledAt.Valid {
		writeErr(w, r, errf(http.StatusNotFound, "ticket_expired", "the passkey check expired or failed: sign in again"))
		return
	}
	s.rememberDevice(r.Context(), u.ID, pt.deviceKey)
	s.openSession(w, r, u, pt.deviceName, pt.deviceKey)
}

func (s *Server) hasPasskeys(ctx context.Context, userID string) bool {
	var n int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = ?`, userID).Scan(&n)
	return n > 0
}

// servePasskeyPage serves /passkey/ (HTML and script of this service, strict CSP).
func servePasskeyPage() http.Handler {
	sub, _ := fs.Sub(passkeyPage, "passkeypage")
	files := http.FileServer(http.FS(sub))
	return http.StripPrefix("/passkey/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
}
