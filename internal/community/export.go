package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// handleExport: GET /v1/members/@me/export, what this server keeps about the
// member (right of access and portability): profile here, roles, settings,
// reactions, files and every message they wrote (streamed: there may be many),
// wherever they are now. The phone number is never stored (only a keyed hash):
// the export says whether one was verified.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	me := memberFrom(r)
	if err := s.limit.export.Check(me.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	x := &exporter{ctx: ctx, db: s.db}
	info, err := s.info(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := s.memberView(ctx, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	head := map[string]any{
		"format":      "quarel-server-export-1",
		"exported_at": s.now().UTC(),
		"server":      map[string]any{"id": info.ID, "name": info.Name},
		"member":      view,
		"member_since": first(x.rows(`SELECT joined_at, rules_accepted_at, phone_hash IS NOT NULL AS phone_verified, timeout_until,
			voice_mute AS muted_by_moderation, voice_deaf AS deafened_by_moderation FROM members WHERE id = ?`, me.ID)),
		"roles":                 x.rows(`SELECT r.name, r.color FROM member_roles mr JOIN roles r ON r.id = mr.role_id WHERE mr.member_id = ? ORDER BY r.position DESC`, me.ID),
		"notification_settings": x.rows(`SELECT channel_id, level, muted_until FROM notification_settings WHERE member_id = ?`, me.ID),
		"read_states":           x.rows(`SELECT channel_id, last_read AS last_read_message FROM read_states WHERE member_id = ?`, me.ID),
		"reactions":             x.rows(`SELECT message_id, emoji, created_at FROM reactions WHERE member_id = ? ORDER BY created_at`, me.ID),
		"files": x.rows(`SELECT id, message_id, channel_id, filename, content_type, size, created_at FROM attachments
			WHERE uploader_id = ? AND message_id IS NOT NULL ORDER BY created_at`, me.ID),
		"invites_created": x.rows(`SELECT code, uses, max_uses, created_at, expires_at FROM invites WHERE creator_id = ? ORDER BY created_at`, me.ID),
	}
	if x.err != nil {
		writeErr(w, r, x.err)
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.channel_id, COALESCE(c.name, ''), m.content, m.created_at, m.edited_at, m.reply_to
		FROM messages m LEFT JOIN channels c ON c.id = m.channel_id WHERE m.author_id = ? ORDER BY m.id`, me.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	// {…head, "messages": [one per row]}
	start, _ := json.Marshal(head)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="quarel-serveur-donnees.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(start[:len(start)-1])
	w.Write([]byte(`,"messages":[`))
	enc := json.NewEncoder(w)
	n := 0
	for rows.Next() {
		var m struct {
			ID      int64  `json:"id"`
			Channel int64  `json:"channel_id"`
			Name    string `json:"channel"`
			Content string `json:"content"`
			Created any    `json:"created_at"`
			Edited  any    `json:"edited_at,omitempty"`
			ReplyTo *int64 `json:"reply_to,omitempty"`
		}
		var created int64
		var edited sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Channel, &m.Name, &m.Content, &created, &edited, &m.ReplyTo); err != nil {
			s.logErr("export", err)
			break
		}
		m.Created = fromMs(created)
		if edited.Valid {
			m.Edited = fromMs(edited.Int64)
		}
		if n > 0 {
			w.Write([]byte(","))
		}
		enc.Encode(m)
		n++
	}
	w.Write([]byte("]}\n"))
}

// exporter runs the export's queries, keeping the first error.
type exporter struct {
	ctx context.Context
	db  *sql.DB
	err error
}

func first(rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// rows reads rows as JSON objects (column name → value); *_at, *_until and
// last_read columns are Unix milliseconds turned into dates.
func (x *exporter) rows(query string, args ...any) []map[string]any {
	if x.err != nil {
		return nil
	}
	rows, err := x.db.QueryContext(x.ctx, query, args...)
	if err != nil {
		x.err = err
		return nil
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		x.err = err
		return nil
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			x.err = err
			return nil
		}
		m := map[string]any{}
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if n, ok := v.(int64); ok && (strings.HasSuffix(c, "_at") || strings.HasSuffix(c, "_until")) {
				v = fromMs(n)
			}
			m[c] = v
		}
		out = append(out, m)
	}
	x.err = rows.Err()
	return out
}
