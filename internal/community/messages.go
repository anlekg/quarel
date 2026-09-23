package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxMessageLen   = 4000
	defaultPageSize = 50
	maxPageSize     = 100
)

// Mention syntax in message content: <@member_id> and @everyone.
// Role mentions (<@&role_id>) arrive with roles in milestone 3.
var (
	mentionRe  = regexp.MustCompile(`<@([a-z2-7]{26})>`)
	everyoneRe = regexp.MustCompile(`(^|[^\w])@everyone\b`)
)

type message struct {
	ID              int64      `json:"id"`
	ChannelID       int64      `json:"channel_id"`
	AuthorID        string     `json:"author_id"`
	Content         string     `json:"content"`
	Mentions        []string   `json:"mentions"` // member IDs
	MentionEveryone bool       `json:"mention_everyone"`
	CreatedAt       time.Time  `json:"created_at"`
	EditedAt        *time.Time `json:"edited_at"`
}

const messageCols = `id, channel_id, author_id, content, mention_everyone, created_at, edited_at`

func scanMessage(sc interface{ Scan(...any) error }) (*message, error) {
	var m message
	var created int64
	var edited sql.NullInt64
	if err := sc.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.Content, &m.MentionEveryone, &created, &edited); err != nil {
		return nil, err
	}
	m.CreatedAt, m.EditedAt, m.Mentions = fromMs(created), nullTime(edited), []string{}
	return &m, nil
}

// textChannel loads the {id} channel and checks it accepts messages.
func (s *Server) textChannel(r *http.Request) (*channel, error) {
	c, err := s.pathChannel(r)
	if err != nil {
		return nil, err
	}
	if c.Type != chanText {
		return nil, errf(http.StatusBadRequest, "not_text_channel", "only text channels hold messages")
	}
	return c, nil
}

func validContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > maxMessageLen {
		return "", errf(http.StatusBadRequest, "invalid_content", "message must be 1-%d characters", maxMessageLen)
	}
	return content, nil
}

// resolveMentions returns the distinct active members mentioned in content.
func (s *Server) resolveMentions(ctx context.Context, q querier, content string) ([]string, bool, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(content, -1) {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		var exists bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM members WHERE id = ? AND left_at IS NULL)`, id).Scan(&exists); err != nil {
			return nil, false, err
		}
		if exists {
			ids = append(ids, id)
		}
	}
	return ids, everyoneRe.MatchString(content), nil
}

func saveMentions(ctx context.Context, q querier, msgID int64, ids []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM message_mentions WHERE message_id = ?`, msgID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `INSERT INTO message_mentions (message_id, member_id) VALUES (?, ?)`, msgID, id); err != nil {
			return err
		}
	}
	return nil
}

// loadMentions fills Mentions for msgs.
func (s *Server) loadMentions(ctx context.Context, msgs []*message) error {
	if len(msgs) == 0 {
		return nil
	}
	byID := map[int64]*message{}
	args := make([]any, len(msgs))
	for i, m := range msgs {
		byID[m.ID] = m
		args[i] = m.ID
	}
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, member_id FROM message_mentions WHERE message_id IN (?`+
		strings.Repeat(",?", len(msgs)-1)+`) ORDER BY rowid`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID int64
		var memberID string
		if err := rows.Scan(&msgID, &memberID); err != nil {
			return err
		}
		byID[msgID].Mentions = append(byID[msgID].Mentions, memberID)
	}
	return rows.Err()
}

// handleListMessages returns one page of history in chronological order:
// the latest messages by default, those just before ?before=ID, or those just after ?after=ID.
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	c, err := s.textChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	q := r.URL.Query()
	limit := defaultPageSize
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > maxPageSize {
			writeErr(w, r, errf(http.StatusBadRequest, "bad_request", "limit must be 1-%d", maxPageSize))
			return
		}
	}
	cursor := func(name string) (int64, bool) {
		v := q.Get(name)
		id, err := strconv.ParseInt(v, 10, 64)
		return id, v != "" && err == nil
	}
	var query string
	args := []any{c.ID}
	if after, ok := cursor("after"); ok {
		query = `SELECT ` + messageCols + ` FROM messages WHERE channel_id = ? AND id > ? ORDER BY id ASC LIMIT ?`
		args = append(args, after, limit)
	} else if before, ok := cursor("before"); ok {
		query = `SELECT ` + messageCols + ` FROM messages WHERE channel_id = ? AND id < ? ORDER BY id DESC LIMIT ?`
		args = append(args, before, limit)
	} else {
		query = `SELECT ` + messageCols + ` FROM messages WHERE channel_id = ? ORDER BY id DESC LIMIT ?`
		args = append(args, limit)
	}
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	msgs := []*message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			writeErr(w, r, err)
			return
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	if strings.Contains(query, "DESC") {
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}
	if err := s.loadMentions(ctx, msgs); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := s.textChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	content, err := validContent(req.Content)
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
	mentions, everyone, err := s.resolveMentions(ctx, tx, content)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	now := s.nowMs()
	author := memberFrom(r).ID
	res, err := tx.ExecContext(ctx, `INSERT INTO messages (channel_id, author_id, content, mention_everyone, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, author, content, everyone, now)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	if err := saveMentions(ctx, tx, id, mentions); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	msg := &message{ID: id, ChannelID: c.ID, AuthorID: author, Content: content, Mentions: mentions, MentionEveryone: everyone, CreatedAt: fromMs(now)}
	s.hub.broadcast("MESSAGE_CREATE", msg)
	writeJSON(w, http.StatusCreated, msg)
}

// pathMessage loads the {mid} message of the {id} channel.
func (s *Server) pathMessage(r *http.Request) (*message, error) {
	c, err := s.textChannel(r)
	if err != nil {
		return nil, err
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		return nil, errf(http.StatusNotFound, "not_found", "no such message")
	}
	m, err := scanMessage(s.db.QueryRowContext(r.Context(), `SELECT `+messageCols+` FROM messages WHERE id = ? AND channel_id = ?`, mid, c.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusNotFound, "not_found", "no such message")
	}
	return m, err
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if msg.AuthorID != memberFrom(r).ID {
		writeErr(w, r, errf(http.StatusForbidden, "forbidden", "you can only edit your own messages"))
		return
	}
	if msg.Content, err = validContent(req.Content); err != nil {
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
	if msg.Mentions, msg.MentionEveryone, err = s.resolveMentions(ctx, tx, msg.Content); err != nil {
		writeErr(w, r, err)
		return
	}
	now := s.nowMs()
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET content = ?, mention_everyone = ?, edited_at = ? WHERE id = ?`,
		msg.Content, msg.MentionEveryone, now, msg.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := saveMentions(ctx, tx, msg.ID, msg.Mentions); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	edited := fromMs(now)
	msg.EditedAt = &edited
	s.hub.broadcast("MESSAGE_UPDATE", msg)
	writeJSON(w, http.StatusOK, msg)
}

// handleDeleteMessage lets authors delete their messages, and the owner delete any.
func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if me := memberFrom(r); msg.AuthorID != me.ID && !me.IsOwner {
		writeErr(w, r, errf(http.StatusForbidden, "forbidden", "you can only delete your own messages"))
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM messages WHERE id = ?`, msg.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.broadcast("MESSAGE_DELETE", map[string]int64{"id": msg.ID, "channel_id": msg.ChannelID})
	w.WriteHeader(http.StatusNoContent)
}
