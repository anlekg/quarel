package community

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

// Forum channels (P2): no messages of their own, only posts. A post is a
// thread of the forum (same permissions: send_messages in the forum to
// post or answer) with a title, started by its first message. The forum
// lists its posts by latest activity.

type forumPost struct {
	Channel       *channel  `json:"channel"`   // the post's thread
	AuthorID      string    `json:"author_id"` // author of the first message
	Excerpt       string    `json:"excerpt"`   // first 200 characters of the first message
	MessageCount  int       `json:"message_count"`
	LastMessageAt time.Time `json:"last_message_at"`
}

// forumChannel loads the {id} forum the member can see.
func (s *Server) forumChannel(r *http.Request) (*channel, *permSnapshot, error) {
	c, err := s.pathChannel(r)
	if err != nil {
		return nil, nil, err
	}
	ps, err := s.requireChannelPerm(r, c, 0)
	if err != nil {
		return nil, nil, err
	}
	if c.Type != chanForum {
		return nil, nil, errf(http.StatusBadRequest, "not_forum", "this channel is not a forum")
	}
	return c, ps, nil
}

// handleCreatePost: POST /v1/channels/{id}/posts {title, content}.
func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	forum, ps, err := s.forumChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if err := requirePost(ps, me, forum, 0); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.allowThread(ps, me, forum); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.messages.Check(me); err != nil {
		writeErr(w, r, err)
		return
	}
	title, err := validName(req.Title)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	content, err := validContent(req.Content, false)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if err := s.checkAutoMod(ctx, ps, me, forum.ID, title+"\n"+content, true); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (type, name, topic, parent_id, position, created_at, thread) VALUES ('text', ?, '', ?, 0, ?, 1)`,
		title, forum.ID, s.nowMs())
	if err != nil {
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
	msg, err := s.storeMessage(ctx, ps, th, me, content, ps.inChannel(me, forum.ID), func(tx *sql.Tx, mid int64) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO read_states (member_id, channel_id, last_read) VALUES (?, ?, ?)
			ON CONFLICT (member_id, channel_id) DO UPDATE SET last_read = MAX(last_read, excluded.last_read)`, me, th.ID, mid)
		return err
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, forumPost{Channel: th, AuthorID: me, Excerpt: excerpt(msg.Content), MessageCount: 1, LastMessageAt: msg.CreatedAt})
}

func excerpt(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// handleListPosts: GET /v1/channels/{id}/posts?limit=&before=<last_message_at ms>, latest activity first.
func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	forum, _, err := s.forumChannel(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < limit {
		limit = v
	}
	before := int64(1<<62 - 1)
	if v, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && v > 0 {
		before = v
	}
	posts, err := s.forumPosts(r.Context(), forum.ID, before, limit)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, posts)
}

func (s *Server) forumPosts(ctx context.Context, forumID, before int64, limit int) ([]forumPost, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, COUNT(m.id), MAX(m.created_at),
       (SELECT author_id FROM messages WHERE channel_id = c.id ORDER BY id LIMIT 1),
       (SELECT content FROM messages WHERE channel_id = c.id ORDER BY id LIMIT 1)
FROM channels c JOIN messages m ON m.channel_id = c.id
WHERE c.parent_id = ? AND c.thread = 1
GROUP BY c.id HAVING MAX(m.created_at) < ?
ORDER BY MAX(m.created_at) DESC LIMIT ?`, forumID, before, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, last int64
		count    int
		author   sql.NullString
		content  sql.NullString
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.count, &x.last, &x.author, &x.content); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	posts := []forumPost{}
	for _, x := range list {
		th, err := channelByID(ctx, s.db, x.id)
		if err != nil {
			continue
		}
		posts = append(posts, forumPost{Channel: th, AuthorID: x.author.String, Excerpt: excerpt(x.content.String), MessageCount: x.count, LastMessageAt: fromMs(x.last)})
	}
	return posts, nil
}
