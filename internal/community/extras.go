package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Message extras: reactions, pins, replies (excerpts), threads, typing
// indicators, read states and unread counts, notification settings, search.

const (
	maxPinsPerChannel     = 50
	maxReactionsPerMsg    = 20 // distinct emoji
	maxSearchResults      = 50
	defaultSearchResults  = 25
	unreadCap             = 100 // unread counts stop at 100 ("99+")
	referencedExcerptRune = 200
)

type reactionCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Me    bool   `json:"me"`
}

func idsArgs(msgs []*message) (string, []any, map[int64]*message) {
	args := make([]any, len(msgs))
	byID := map[int64]*message{}
	for i, m := range msgs {
		args[i] = m.ID
		byID[m.ID] = m
	}
	return `(?` + strings.Repeat(",?", len(msgs)-1) + `)`, args, byID
}

// enrich fills everything around messages: mentions, reply excerpts,
// attachments, link previews, reactions (with "me" for viewer), threads.
func (s *Server) enrich(ctx context.Context, viewer string, msgs []*message) error {
	if len(msgs) == 0 {
		return nil
	}
	for _, m := range msgs { // everything below is (re)loaded from the database
		m.Mentions, m.MentionRoles, m.Attachments, m.Embeds, m.Reactions = []string{}, []int64{}, []attachmentJSON{}, []linkEmbed{}, []reactionCount{}
		m.Referenced, m.ThreadID, m.Webhook, m.Interaction = nil, nil, nil, nil
	}
	if err := s.loadMentions(ctx, msgs); err != nil {
		return err
	}
	if err := s.loadWebhookAuthors(ctx, msgs); err != nil {
		return err
	}
	in, args, byID := idsArgs(msgs)
	if err := s.loadInteractions(ctx, in, args, byID); err != nil {
		return err
	}

	// Reply excerpts.
	var refIDs []any
	for _, m := range msgs {
		if m.ReplyTo != nil {
			refIDs = append(refIDs, *m.ReplyTo)
		}
	}
	if len(refIDs) > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT id, author_id, content FROM messages WHERE id IN (?`+strings.Repeat(",?", len(refIDs)-1)+`)`, refIDs...)
		if err != nil {
			return err
		}
		refs := map[int64]*referenced{}
		for rows.Next() {
			var r referenced
			if err := rows.Scan(&r.ID, &r.AuthorID, &r.Content); err != nil {
				rows.Close()
				return err
			}
			if rs := []rune(r.Content); len(rs) > referencedExcerptRune {
				r.Content = string(rs[:referencedExcerptRune]) + "…"
			}
			refs[r.ID] = &r
		}
		rows.Close()
		for _, m := range msgs {
			if m.ReplyTo != nil {
				m.Referenced = refs[*m.ReplyTo] // nil if the original was deleted
			}
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, message_id, filename, content_type, size FROM attachments WHERE message_id IN `+in+` ORDER BY created_at, id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a attachmentJSON
		var mid int64
		if err := rows.Scan(&a.ID, &mid, &a.Filename, &a.ContentType, &a.Size); err != nil {
			rows.Close()
			return err
		}
		a.URL = attachmentURL(a.ID, a.Filename)
		byID[mid].Attachments = append(byID[mid].Attachments, a)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT message_id, url, title, description, site_name, image_url FROM link_previews WHERE message_id IN `+in+` ORDER BY rowid`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var e linkEmbed
		var mid int64
		if err := rows.Scan(&mid, &e.URL, &e.Title, &e.Description, &e.SiteName, &e.ImageURL); err != nil {
			rows.Close()
			return err
		}
		byID[mid].Embeds = append(byID[mid].Embeds, e)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT message_id, emoji, COUNT(*), MAX(member_id = ?), MIN(created_at) AS first
		FROM reactions WHERE message_id IN `+in+` GROUP BY message_id, emoji ORDER BY first, MIN(rowid)`, append([]any{viewer}, args...)...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rc reactionCount
		var mid int64
		var first int64
		if err := rows.Scan(&mid, &rc.Emoji, &rc.Count, &rc.Me, &first); err != nil {
			rows.Close()
			return err
		}
		byID[mid].Reactions = append(byID[mid].Reactions, rc)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT thread_starter, id FROM channels WHERE thread = 1 AND thread_starter IN `+in, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, tid int64
		if err := rows.Scan(&mid, &tid); err != nil {
			return err
		}
		byID[mid].ThreadID = &tid
	}
	return rows.Err()
}

// messageByID loads and enriches one message of a channel.
func (s *Server) messageByID(ctx context.Context, viewer string, channelID, id int64) (*message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages WHERE id = ? AND channel_id = ?`, id, channelID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusNotFound, "not_found", "no such message")
	}
	if err != nil {
		return nil, err
	}
	return m, s.enrich(ctx, viewer, []*message{m})
}

// --- reactions ---

// validEmoji accepts a short Unicode emoji (custom server emoji are P2): no
// letters, no spaces, at least one symbol-range character.
func validEmoji(e string) bool {
	if e == "" || len(e) > 32 || !utf8.ValidString(e) {
		return false
	}
	symbol := false
	for _, r := range e {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r) || unicode.IsLetter(r) && r < 0x2000:
			return false
		case r >= 0x2000:
			symbol = true
		}
	}
	return symbol
}

func (s *Server) handleReaction(w http.ResponseWriter, r *http.Request) {
	c, ps, msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me := memberFrom(r).ID
	emoji := r.PathValue("emoji")
	target := me
	if other := r.PathValue("member"); other != "" {
		target = other
	}
	if !s.validReaction(ctx, emoji) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_emoji", "not a single emoji"))
		return
	}
	event := "REACTION_ADD"
	if r.Method == http.MethodPut {
		if ps.inChannel(me, c.ID)&permAddReactions == 0 {
			writeErr(w, r, ps.deny(me, permAddReactions))
			return
		}
		var distinct int
		var exists bool
		s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT emoji), MAX(emoji = ?) FROM reactions WHERE message_id = ?`, emoji, msg.ID).Scan(&distinct, &exists)
		if !exists && distinct >= maxReactionsPerMsg {
			writeErr(w, r, errf(http.StatusBadRequest, "too_many_reactions", "at most %d different reactions per message", maxReactionsPerMsg))
			return
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reactions (message_id, member_id, emoji, created_at) VALUES (?, ?, ?, ?)`,
			msg.ID, me, emoji, s.nowMs()); err != nil {
			writeErr(w, r, err)
			return
		}
	} else {
		event = "REACTION_REMOVE"
		if target != me && ps.inChannel(me, c.ID)&permManageMessages == 0 {
			writeErr(w, r, missing(permManageMessages))
			return
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM reactions WHERE message_id = ? AND member_id = ? AND emoji = ?`, msg.ID, target, emoji); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	s.broadcastChannel(ctx, event, c.ID, map[string]any{"channel_id": c.ID, "message_id": msg.ID, "emoji": emoji, "member_id": target})
	w.WriteHeader(http.StatusNoContent)
}

// --- pins ---

func (s *Server) handlePin(w http.ResponseWriter, r *http.Request) {
	c, ps, msg, err := s.pathMessage(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if ps.inChannel(me, c.ID)&permManageMessages == 0 {
		writeErr(w, r, missing(permManageMessages))
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodPut {
		var pins int
		s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE channel_id = ? AND pinned_at IS NOT NULL`, c.ID).Scan(&pins)
		if msg.PinnedAt == nil && pins >= maxPinsPerChannel {
			writeErr(w, r, errf(http.StatusBadRequest, "too_many_pins", "at most %d pinned messages per channel", maxPinsPerChannel))
			return
		}
		_, err = s.db.ExecContext(ctx, `UPDATE messages SET pinned_at = COALESCE(pinned_at, ?), pinned_by = COALESCE(pinned_by, ?) WHERE id = ?`, s.nowMs(), me, msg.ID)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE messages SET pinned_at = NULL, pinned_by = NULL WHERE id = ?`, msg.ID)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if msg, err = s.messageByID(ctx, "", c.ID, msg.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "MESSAGE_UPDATE", c.ID, msg)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	c, _, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.writeMessages(w, r, `SELECT `+messageCols+` FROM messages WHERE channel_id = ? AND pinned_at IS NOT NULL ORDER BY pinned_at DESC`, c.ID)
}

// writeMessages runs a message query and writes the enriched result.
func (s *Server) writeMessages(w http.ResponseWriter, r *http.Request, query string, args ...any) {
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
	if err := s.enrich(ctx, memberFrom(r).ID, msgs); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- threads ---

// handleCreateThread starts a thread (a sub-channel) from a message.
func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
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
	if c.Type == chanThread {
		writeErr(w, r, errf(http.StatusBadRequest, "nested_thread", "threads cannot contain threads"))
		return
	}
	if err := requirePost(ps, memberFrom(r).ID, c, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.allowThread(ps, memberFrom(r).ID, c); err != nil {
		writeErr(w, r, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(strings.SplitN(msg.Content, "\n", 2)[0])
		if rs := []rune(name); len(rs) > 40 {
			name = string(rs[:40]) + "…"
		}
		if name == "" {
			name = "Fil de discussion"
		}
	}
	if name, err = validName(name); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (type, name, topic, parent_id, position, created_at, thread, thread_starter) VALUES ('text', ?, '', ?, 0, ?, 1, ?)`,
		name, c.ID, s.nowMs(), msg.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			err = errf(http.StatusConflict, "thread_exists", "this message already has a thread")
		}
		writeErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	th, err := channelByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.broadcastChannel(ctx, "CHANNEL_CREATE", th.ID, th)
	if m, err := s.messageByID(ctx, "", c.ID, msg.ID); err == nil {
		s.broadcastChannel(ctx, "MESSAGE_UPDATE", c.ID, m)
	}
	writeJSON(w, http.StatusCreated, th)
}

// --- typing ---

func (s *Server) handleTyping(w http.ResponseWriter, r *http.Request) {
	c, ps, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if err := requirePost(ps, me, c, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	// At most one event per member and channel every few seconds; extra calls are silently dropped.
	if ok, _ := s.limit.typing.Allow(me + "|" + strconv.FormatInt(c.ID, 10)); ok {
		s.broadcastChannel(r.Context(), "TYPING_START", c.ID, map[string]any{"channel_id": c.ID, "member_id": me, "at": s.now().UTC()})
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- read states and unread counts ---

type readState struct {
	ChannelID     int64 `json:"channel_id"`
	LastRead      int64 `json:"last_read"`       // last message read
	LastMessageID int64 `json:"last_message_id"` // 0: empty channel
	Unread        int   `json:"unread"`          // capped at 100
	Mentions      int   `json:"mentions"`        // unread messages notifying the member
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MessageID int64 `json:"message_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	c, _, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me := memberFrom(r)
	if req.MessageID == 0 { // no id: everything currently in the channel is read
		s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM messages WHERE channel_id = ?`, c.ID).Scan(&req.MessageID)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO read_states (member_id, channel_id, last_read) VALUES (?, ?, ?)
		ON CONFLICT (member_id, channel_id) DO UPDATE SET last_read = MAX(last_read, excluded.last_read)`, me.ID, c.ID, req.MessageID); err != nil {
		writeErr(w, r, err)
		return
	}
	st, err := s.readStates(ctx, me, []int64{c.ID})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	// Other devices of the member update their unread badges.
	s.hub.BroadcastTo("READ_STATE_UPDATE", st[0], func(key, _ string) bool { return key == me.ID })
	writeJSON(w, http.StatusOK, st[0])
}

// readStates computes unread and mention counts. Messages sent before the
// member joined count as read.
func (s *Server) readStates(ctx context.Context, m *member, channels []int64) ([]readState, error) {
	out := []readState{}
	var roles []any
	rows, err := s.db.QueryContext(ctx, `SELECT role_id FROM member_roles WHERE member_id = ?`, m.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		roles = append(roles, id)
	}
	rows.Close()
	roleIn := `(NULL)`
	if len(roles) > 0 {
		roleIn = `(?` + strings.Repeat(",?", len(roles)-1) + `)`
	}
	for _, ch := range channels {
		st := readState{ChannelID: ch}
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT last_read FROM read_states WHERE member_id = ? AND channel_id = ?), 0),
			COALESCE((SELECT MAX(id) FROM messages WHERE channel_id = ?), 0)`, m.ID, ch, ch).Scan(&st.LastRead, &st.LastMessageID); err != nil {
			return nil, err
		}
		if st.LastMessageID > st.LastRead {
			base := ` FROM messages m WHERE m.channel_id = ? AND m.id > ? AND m.created_at >= ? AND m.author_id != ?`
			args := []any{ch, st.LastRead, m.JoinedAt, m.ID}
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1`+base+` LIMIT ?)`, append(args, unreadCap)...).Scan(&st.Unread); err != nil {
				return nil, err
			}
			margs := append(append(append([]any{}, args...), m.ID), roles...)
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+base+` AND (m.mention_everyone
				OR EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.member_id = ?)
				OR EXISTS (SELECT 1 FROM message_role_mentions rm WHERE rm.message_id = m.id AND rm.role_id IN `+roleIn+`))`, margs...).Scan(&st.Mentions); err != nil {
				return nil, err
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// messagingChannels lists the channels holding messages that a member can
// see (archived threads too, for search).
func messagingChannels(ps *permSnapshot, memberID string, archived bool) []int64 {
	var ids []int64
	for _, c := range ps.channelsSeen(memberID, archived) {
		if c.messaging() {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func (s *Server) handleReadStates(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r)
	st, err := s.readStates(r.Context(), me, messagingChannels(ps, me.ID, false))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- notification settings ---

type notificationSetting struct {
	ChannelID  int64      `json:"channel_id"` // 0: whole server
	Level      string     `json:"level"`      // default | all | mentions | none
	MutedUntil *time.Time `json:"muted_until"`
}

// mutedForever is the highest date JSON (RFC 3339) can carry.
var mutedForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC).UnixMilli()

func (s *Server) notificationSettings(ctx context.Context, memberID string) ([]notificationSetting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, level, muted_until FROM notification_settings WHERE member_id = ? ORDER BY channel_id`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notificationSetting{}
	for rows.Next() {
		var n notificationSetting
		var muted sql.NullInt64
		if err := rows.Scan(&n.ChannelID, &n.Level, &muted); err != nil {
			return nil, err
		}
		n.MutedUntil = nullTime(muted)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Server) handleListNotificationSettings(w http.ResponseWriter, r *http.Request) {
	list, err := s.notificationSettings(r.Context(), memberFrom(r).ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSetNotification sets what notifies the member in a channel (or the
// whole server with channel 0). Clients apply it; the server only stores it.
func (s *Server) handleSetNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level      string `json:"level"`
		MuteForSec int64  `json:"mute_for"` // seconds; 0: not muted, -1: muted until unmuted
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if !slices.Contains([]string{"default", "all", "mentions", "none"}, req.Level) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_level", "level must be default, all, mentions or none"))
		return
	}
	ctx := r.Context()
	me := memberFrom(r).ID
	chID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such channel"))
		return
	}
	if chID != 0 {
		c, err := channelByID(ctx, s.db, chID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if _, err := s.requireChannelPerm(r, c, 0); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	var muted sql.NullInt64
	switch {
	case req.MuteForSec == -1:
		muted = sql.NullInt64{Int64: mutedForever, Valid: true}
	case req.MuteForSec > 0:
		muted = sql.NullInt64{Int64: s.now().Add(time.Duration(req.MuteForSec) * time.Second).UnixMilli(), Valid: true}
	}
	if req.Level == "default" && !muted.Valid {
		_, err = s.db.ExecContext(ctx, `DELETE FROM notification_settings WHERE member_id = ? AND channel_id = ?`, me, chID)
	} else {
		_, err = s.db.ExecContext(ctx, `INSERT INTO notification_settings (member_id, channel_id, level, muted_until) VALUES (?, ?, ?, ?)
			ON CONFLICT (member_id, channel_id) DO UPDATE SET level = excluded.level, muted_until = excluded.muted_until`, me, chID, req.Level, muted)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	list, _ := s.notificationSettings(ctx, me)
	s.hub.BroadcastTo("NOTIFICATION_SETTINGS_UPDATE", list, func(key, _ string) bool { return key == me })
	writeJSON(w, http.StatusOK, list)
}

// --- search ---

// ftsQuery turns free text into an FTS5 query: every word must appear
// (prefix match), accents and case ignored; nothing of the input is
// interpreted as FTS syntax.
func ftsQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	var parts []string
	for _, w := range words {
		parts = append(parts, `"`+w+`"*`)
		if len(parts) == 10 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// handleSearch searches the messages of the channels the member can see.
// Filters: channel_id, author_id; pagination: before (message ID).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	match := ftsQuery(q.Get("q"))
	if match == "" {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_query", "search needs at least one word"))
		return
	}
	ctx := r.Context()
	ps, err := s.loadPerms(ctx, s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	channels := messagingChannels(ps, me, true)
	if cid, err := strconv.ParseInt(q.Get("channel_id"), 10, 64); err == nil {
		if !slices.Contains(channels, cid) {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such channel"))
			return
		}
		channels = []int64{cid}
	}
	if len(channels) == 0 {
		writeJSON(w, http.StatusOK, []*message{})
		return
	}
	limit := defaultSearchResults
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= maxSearchResults {
		limit = v
	}
	args := []any{match}
	for _, c := range channels {
		args = append(args, c)
	}
	query := `SELECT ` + prefixCols("m.", messageCols) + ` FROM messages_fts JOIN messages m ON m.id = messages_fts.rowid
		WHERE messages_fts MATCH ? AND m.channel_id IN (?` + strings.Repeat(",?", len(channels)-1) + `)`
	if a := q.Get("author_id"); a != "" {
		query += ` AND m.author_id = ?`
		args = append(args, a)
	}
	if b, err := strconv.ParseInt(q.Get("before"), 10, 64); err == nil {
		query += ` AND m.id < ?`
		args = append(args, b)
	}
	query += ` ORDER BY m.id DESC LIMIT ?`
	s.writeMessages(w, r, query, append(args, limit)...)
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
