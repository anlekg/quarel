package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/secret"
)

// Bots are members created by the server's managers, with no Identity
// account: they authenticate with a long-lived token of this server only
// ("Authorization: Bearer qb_…") and use the same REST API and gateway as
// members. They get permissions through roles, like everyone.

const (
	botIssuer      = "#bot" // cannot be a domain, so never clashes with an Identity service
	botTokenPrefix = "qb_"
)

type botJSON struct {
	Member memberJSON `json:"member"`
	Token  string     `json:"token,omitempty"` // only when created or reset: store it, it is not shown again
}

func validBotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 32 || strings.ContainsAny(name, "@#<>\n\r\t") {
		return "", errf(http.StatusBadRequest, "invalid_name", "bot names are 1-32 characters, without @ # < >")
	}
	return name, nil
}

func (s *Server) handleCreateBot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	name, err := validBotName(req.Name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	m := &member{ID: secret.NewID(), Issuer: botIssuer, Subject: secret.NewID(), Handle: name, JoinedAt: s.nowMs(), Bot: true}
	token := botTokenPrefix + secret.NewToken()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO members (id, issuer, subject, handle, joined_at, bot) VALUES (?, ?, ?, ?, ?, 1)`,
		m.ID, m.Issuer, m.Subject, m.Handle, m.JoinedAt); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO bot_tokens (member_id, token_hash, created_at) VALUES (?, ?, ?)`,
		m.ID, secret.SHA256Hex(token), s.nowMs()); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, memberFrom(r).ID, auditBotCreate, m.ID, "", map[string]any{"name": name})
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	view := m.json()
	s.hub.Broadcast("MEMBER_JOIN", view)
	writeJSON(w, http.StatusCreated, botJSON{Member: view, Token: token})
}

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	list, err := s.activeMembers(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	bots := []botJSON{}
	for _, m := range list {
		if m.Bot {
			bots = append(bots, botJSON{Member: m})
		}
	}
	writeJSON(w, http.StatusOK, bots)
}

// botTarget loads the {id} bot for management: manage_server, and the bot's
// roles must be below the actor's (its token carries its permissions).
func (s *Server) botTarget(r *http.Request) (*member, error) {
	ps, err := s.requirePerm(r, permManageServer)
	if err != nil {
		return nil, err
	}
	m, err := memberBy(r.Context(), s.db, `id = ? AND bot = 1 AND left_at IS NULL`, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errf(http.StatusNotFound, "not_found", "no such bot")
	}
	if !ps.outranks(memberFrom(r).ID, m.ID) {
		return nil, errf(http.StatusForbidden, "role_hierarchy", "this bot's highest role is not below yours")
	}
	return m, nil
}

func (s *Server) handleResetBotToken(w http.ResponseWriter, r *http.Request) {
	m, err := s.botTarget(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	token := botTokenPrefix + secret.NewToken()
	if _, err := s.db.ExecContext(ctx, `UPDATE bot_tokens SET token_hash = ?, created_at = ? WHERE member_id = ?`,
		secret.SHA256Hex(token), s.nowMs(), m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, s.db, memberFrom(r).ID, auditBotTokenReset, m.ID, "", nil)
	// Connections opened with the old token end now.
	s.hub.Disconnect(func(key, _ string) bool { return key == m.ID }, "token reset")
	view, err := s.memberView(ctx, m)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, botJSON{Member: view, Token: token})
}

func (s *Server) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	m, err := s.botTarget(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if err := s.removeMember(ctx, tx, m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bot_tokens WHERE member_id = ?`, m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, memberFrom(r).ID, auditBotDelete, m.ID, "", map[string]any{"name": m.Handle})
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.afterRemoval(m.ID, leftVoluntarily)
	w.WriteHeader(http.StatusNoContent)
}

// botForToken returns the active bot owning a token, or nil.
func (s *Server) botForToken(ctx context.Context, token string) (*member, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT member_id FROM bot_tokens WHERE token_hash = ?`, secret.SHA256Hex(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return memberBy(ctx, s.db, `id = ? AND left_at IS NULL AND bot = 1`, id)
}

// botExpiry: bot tokens do not expire (zero time for the gateway).
var botExpiry time.Time
