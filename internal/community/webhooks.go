package community

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/secret"
)

// Incoming webhooks (P2): an address with a secret that posts messages to
// one channel, for services that cannot hold a bot connection (monitoring,
// CI, forms…). Managed with manage_channels in that channel. Each webhook
// posts as its own member (issuer "#webhook", bot, marked as having left
// when created: never listed, cannot sign in); messages carry
// {webhook: {id, name}} so clients show its name. Its messages cannot
// notify @everyone or non-mentionable roles, and are limited like a
// member's (10 per 10 s).
//
//	POST /v1/webhooks/{id}/{token}  {"content": "…"}  (no session)

const (
	webhookIssuer      = "#webhook"
	webhookTokenPrefix = "qw_"
	maxWebhooksPerChan = 10
	auditWebhookCreate = "webhook_create"
	auditWebhookDelete = "webhook_delete"
)

type webhookJSON struct {
	ID        string    `json:"id"`
	ChannelID int64     `json:"channel_id"`
	Name      string    `json:"name"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Token     string    `json:"token,omitempty"` // only when created: the secret part of its address
}

type webhookRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// webhookChannel loads the {id} channel for managing its webhooks.
func (s *Server) webhookChannel(r *http.Request) (*channel, error) {
	c, err := s.pathChannel(r)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireChannelPerm(r, c, permManageChannels); err != nil {
		return nil, err
	}
	if c.Type != chanText && c.Type != chanAnnouncement {
		return nil, errf(http.StatusBadRequest, "not_text_channel", "webhooks post in text and announcement channels")
	}
	return c, nil
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	c, err := s.webhookChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	list, err := s.webhooks(r.Context(), `channel_id = ?`, c.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) webhooks(ctx context.Context, where string, args ...any) ([]webhookJSON, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, channel_id, name, created_by, created_at FROM webhooks WHERE `+where+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []webhookJSON{}
	for rows.Next() {
		var h webhookJSON
		var by sql.NullString
		var at int64
		if err := rows.Scan(&h.ID, &h.ChannelID, &h.Name, &by, &at); err != nil {
			return nil, err
		}
		if by.Valid {
			h.CreatedBy = &by.String
		}
		h.CreatedAt = fromMs(at)
		list = append(list, h)
	}
	return list, rows.Err()
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
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
	c, err := s.webhookChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	var n int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhooks WHERE channel_id = ?`, c.ID).Scan(&n)
	if n >= maxWebhooksPerChan {
		writeErr(w, r, errf(http.StatusBadRequest, "too_many_webhooks", "at most %d webhooks per channel", maxWebhooksPerChan))
		return
	}
	actor := memberFrom(r).ID
	now := s.nowMs()
	h := webhookJSON{ID: secret.NewID(), ChannelID: c.ID, Name: name, CreatedBy: &actor, CreatedAt: fromMs(now), Token: webhookTokenPrefix + secret.NewToken()}
	memberID := secret.NewID()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO members (id, issuer, subject, handle, joined_at, left_at, bot) VALUES (?, ?, ?, ?, ?, ?, 1)`,
		memberID, webhookIssuer, h.ID, name, now, now); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO webhooks (id, channel_id, member_id, name, token_hash, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		h.ID, c.ID, memberID, name, secret.SHA256Hex(h.Token), actor, now); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, actor, auditWebhookCreate, "", "", map[string]any{"name": name, "channel_id": c.ID})
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.webhooks(ctx, `id = ?`, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if len(list) == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such webhook"))
		return
	}
	h := list[0]
	c, err := channelByID(ctx, s.db, h.ChannelID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.requireChannelPerm(r, c, permManageChannels); err != nil {
		writeErr(w, r, err)
		return
	}
	// The member row stays: its messages keep their author and name.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, h.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, s.db, memberFrom(r).ID, auditWebhookDelete, "", "", map[string]any{"name": h.Name, "channel_id": h.ChannelID})
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhookPost: POST /v1/webhooks/{id}/{token} {content}, without a session.
func (s *Server) handleWebhookPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	var memberID, hash string
	var channelID int64
	err := s.db.QueryRowContext(ctx, `SELECT member_id, channel_id, token_hash FROM webhooks WHERE id = ?`, r.PathValue("id")).Scan(&memberID, &channelID, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(secret.SHA256Hex(r.PathValue("token")))) != 1) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such webhook"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.messages.Check(memberID); err != nil {
		writeErr(w, r, err)
		return
	}
	content, err := validContent(req.Content, false)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := channelByID(ctx, s.db, channelID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	msg := &message{ChannelID: c.ID, AuthorID: memberID, Content: content}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if err := resolveMentions(ctx, tx, ps, msg, permSendMessages); err != nil { // no @everyone, no protected roles
		writeErr(w, r, err)
		return
	}
	now := s.nowMs()
	res, err := tx.ExecContext(ctx, `INSERT INTO messages (channel_id, author_id, content, mention_everyone, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, memberID, msg.Content, msg.MentionEveryone, now)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	msg.ID, _ = res.LastInsertId()
	msg.CreatedAt = fromMs(now)
	if err := saveMentions(ctx, tx, msg); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.enrich(ctx, "", []*message{msg}); err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "MESSAGE_CREATE", c.ID, msg)
	s.schedulePreviews(msg)
	writeJSON(w, http.StatusCreated, msg)
}

// loadWebhookAuthors marks the messages posted by webhooks with their name.
func (s *Server) loadWebhookAuthors(ctx context.Context, msgs []*message) error {
	authors := map[string]bool{}
	var args []any
	for _, m := range msgs {
		if !authors[m.AuthorID] {
			authors[m.AuthorID] = true
			args = append(args, m.AuthorID)
		}
	}
	if len(args) == 0 {
		return nil
	}
	args = append(args, webhookIssuer)
	rows, err := s.db.QueryContext(ctx, `SELECT id, subject, handle FROM members WHERE id IN (?`+strings.Repeat(",?", len(args)-2)+`) AND issuer = ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	refs := map[string]*webhookRef{}
	for rows.Next() {
		var id string
		var ref webhookRef
		if err := rows.Scan(&id, &ref.ID, &ref.Name); err != nil {
			return err
		}
		refs[id] = &ref
	}
	for _, m := range msgs {
		m.Webhook = refs[m.AuthorID]
	}
	return rows.Err()
}
