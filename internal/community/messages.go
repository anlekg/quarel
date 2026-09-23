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

// Mention syntax in message content: <@member_id>, <@&role_id> and @everyone.
// @everyone only pings with mention_everyone; a role pings if it is
// mentionable or the author has mention_everyone. Otherwise the text stays
// but nobody is notified.
var (
	mentionRe     = regexp.MustCompile(`<@([a-z2-7]{26})>`)
	roleMentionRe = regexp.MustCompile(`<@&(\d+)>`)
	everyoneRe    = regexp.MustCompile(`(^|[^\w])@everyone\b`)
)

type message struct {
	ID              int64      `json:"id"`
	ChannelID       int64      `json:"channel_id"`
	AuthorID        string     `json:"author_id"`
	Content         string     `json:"content"`
	Mentions        []string   `json:"mentions"`      // member IDs
	MentionRoles    []int64    `json:"mention_roles"` // role IDs
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
	m.CreatedAt, m.EditedAt, m.Mentions, m.MentionRoles = fromMs(created), nullTime(edited), []string{}, []int64{}
	return &m, nil
}

// textChannel loads the {id} channel, checks the member can see it with perms p,
// and that it accepts messages.
func (s *Server) textChannel(r *http.Request, p perm) (*channel, *permSnapshot, error) {
	c, err := s.pathChannel(r)
	if err != nil {
		return nil, nil, err
	}
	ps, err := s.requireChannelPerm(r, c, p)
	if err != nil {
		return nil, nil, err
	}
	if c.Type != chanText {
		return nil, nil, errf(http.StatusBadRequest, "not_text_channel", "only text channels hold messages")
	}
	return c, ps, nil
}

func validContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > maxMessageLen {
		return "", errf(http.StatusBadRequest, "invalid_content", "message must be 1-%d characters", maxMessageLen)
	}
	return content, nil
}

// resolveMentions fills msg's mentions from its content, as written by an
// author holding perms p in the channel.
func resolveMentions(ctx context.Context, q querier, ps *permSnapshot, msg *message, p perm) error {
	canEveryone := p&permMentionEveryone != 0
	msg.Mentions, msg.MentionRoles = []string{}, []int64{}
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(msg.Content, -1) {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		var exists bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM members WHERE id = ? AND left_at IS NULL)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			msg.Mentions = append(msg.Mentions, id)
		}
	}
	seenRole := map[int64]bool{}
	for _, m := range roleMentionRe.FindAllStringSubmatch(msg.Content, -1) {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		rl := ps.roles[id]
		if seenRole[id] || rl == nil || id == everyoneRoleID || !rl.Mentionable && !canEveryone {
			continue
		}
		seenRole[id] = true
		msg.MentionRoles = append(msg.MentionRoles, id)
	}
	msg.MentionEveryone = canEveryone && everyoneRe.MatchString(msg.Content)
	return nil
}

func saveMentions(ctx context.Context, q querier, msg *message) error {
	for _, stmt := range []string{
		`DELETE FROM message_mentions WHERE message_id = ?`,
		`DELETE FROM message_role_mentions WHERE message_id = ?`,
	} {
		if _, err := q.ExecContext(ctx, stmt, msg.ID); err != nil {
			return err
		}
	}
	for _, id := range msg.Mentions {
		if _, err := q.ExecContext(ctx, `INSERT INTO message_mentions (message_id, member_id) VALUES (?, ?)`, msg.ID, id); err != nil {
			return err
		}
	}
	for _, id := range msg.MentionRoles {
		if _, err := q.ExecContext(ctx, `INSERT INTO message_role_mentions (message_id, role_id) VALUES (?, ?)`, msg.ID, id); err != nil {
			return err
		}
	}
	return nil
}

// loadMentions fills the mentions of msgs.
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
	in := `(?` + strings.Repeat(",?", len(msgs)-1) + `)`
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, member_id FROM message_mentions WHERE message_id IN `+in+` ORDER BY rowid`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var msgID int64
		var memberID string
		if err := rows.Scan(&msgID, &memberID); err != nil {
			rows.Close()
			return err
		}
		byID[msgID].Mentions = append(byID[msgID].Mentions, memberID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT message_id, role_id FROM message_role_mentions WHERE message_id IN `+in+` ORDER BY rowid`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID, roleID int64
		if err := rows.Scan(&msgID, &roleID); err != nil {
			return err
		}
		byID[msgID].MentionRoles = append(byID[msgID].MentionRoles, roleID)
	}
	return rows.Err()
}

// handleListMessages returns one page of history in chronological order:
// the latest messages by default, those just before ?before=ID, or those just after ?after=ID.
func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	c, _, err := s.textChannel(r, 0)
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
	c, ps, err := s.textChannel(r, permSendMessages)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	author := memberFrom(r).ID
	msg := &message{ChannelID: c.ID, AuthorID: author}
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
	if err := resolveMentions(ctx, tx, ps, msg, ps.inChannel(author, c.ID)); err != nil {
		writeErr(w, r, err)
		return
	}
	now := s.nowMs()
	res, err := tx.ExecContext(ctx, `INSERT INTO messages (channel_id, author_id, content, mention_everyone, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, author, msg.Content, msg.MentionEveryone, now)
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
	s.broadcastChannel(ctx, "MESSAGE_CREATE", c.ID, msg)
	writeJSON(w, http.StatusCreated, msg)
}

// pathMessage loads the {mid} message of the {id} text channel the member can see.
func (s *Server) pathMessage(r *http.Request) (*channel, *permSnapshot, *message, error) {
	c, ps, err := s.textChannel(r, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		return nil, nil, nil, errf(http.StatusNotFound, "not_found", "no such message")
	}
	m, err := scanMessage(s.db.QueryRowContext(r.Context(), `SELECT `+messageCols+` FROM messages WHERE id = ? AND channel_id = ?`, mid, c.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, errf(http.StatusNotFound, "not_found", "no such message")
	}
	return c, ps, m, err
}

// handleEditMessage lets authors edit their own messages; mentions are recomputed.
func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	c, ps, msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if msg.AuthorID != me {
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
	if err := resolveMentions(ctx, tx, ps, msg, ps.inChannel(me, c.ID)); err != nil {
		writeErr(w, r, err)
		return
	}
	now := s.nowMs()
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET content = ?, mention_everyone = ?, edited_at = ? WHERE id = ?`,
		msg.Content, msg.MentionEveryone, now, msg.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := saveMentions(ctx, tx, msg); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	edited := fromMs(now)
	msg.EditedAt = &edited
	s.broadcastChannel(ctx, "MESSAGE_UPDATE", c.ID, msg)
	writeJSON(w, http.StatusOK, msg)
}

// handleDeleteMessage lets authors delete their messages, and members with
// manage_messages in the channel delete any.
func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	c, ps, msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if msg.AuthorID != me && ps.inChannel(me, c.ID)&permManageMessages == 0 {
		writeErr(w, r, missing(permManageMessages))
		return
	}
	ctx := r.Context()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, msg.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "MESSAGE_DELETE", c.ID, map[string]int64{"id": msg.ID, "channel_id": msg.ChannelID})
	w.WriteHeader(http.StatusNoContent)
}
