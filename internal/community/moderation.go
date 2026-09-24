package community

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

// Removal reasons sent in MEMBER_LEAVE events.
const (
	leftVoluntarily = "left"
	leftKicked      = "kicked"
	leftBanned      = "banned"
)

// removeMember ends a membership: sessions, roles and live connections go;
// the member row stays so messages keep their author and bans keep their target.
func (s *Server) removeMember(ctx context.Context, tx *sql.Tx, memberID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE members SET left_at = COALESCE(left_at, ?) WHERE id = ?`, s.nowMs(), memberID); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM sessions WHERE member_id = ?`,
		`DELETE FROM member_roles WHERE member_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, memberID); err != nil {
			return err
		}
	}
	return nil
}

// afterRemoval notifies clients once removeMember's transaction is committed.
func (s *Server) afterRemoval(memberID, reason string) {
	s.disconnectVoice(context.Background(), memberID)
	s.hub.Disconnect(func(key, _ string) bool { return key == memberID }, "no longer a member")
	s.hub.Broadcast("MEMBER_LEAVE", map[string]string{"id": memberID, "reason": reason})
}

func validReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 512 {
		return "", errf(http.StatusBadRequest, "invalid_reason", "reason must be at most 512 characters")
	}
	return reason, nil
}

// moderationTarget loads the {id} member and checks the actor outranks them.
func (s *Server) moderationTarget(r *http.Request, ps *permSnapshot, activeOnly bool) (*member, error) {
	where := `id = ?`
	if activeOnly {
		where += ` AND left_at IS NULL`
	}
	target, err := memberBy(r.Context(), s.db, where, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errf(http.StatusNotFound, "not_found", "no such member")
	}
	actor := memberFrom(r).ID
	if target.ID == actor {
		return nil, errf(http.StatusBadRequest, "self_moderation", "you cannot do this to yourself")
	}
	if !ps.outranks(actor, target.ID) {
		return nil, errf(http.StatusForbidden, "role_hierarchy", "this member's highest role is not below yours")
	}
	return target, nil
}

func (s *Server) handleKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requirePerm(r, permKickMembers)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	target, err := s.moderationTarget(r, ps, true)
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
	if err := s.removeMember(ctx, tx, target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, memberFrom(r).ID, auditMemberKick, target.ID, reason, nil)
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.afterRemoval(target.ID, leftKicked)
	w.WriteHeader(http.StatusNoContent)
}

type banJSON struct {
	Member    memberJSON `json:"member"`
	Reason    string     `json:"reason"`
	BannedBy  *string    `json:"banned_by"`
	CreatedAt time.Time  `json:"created_at"`
}

// handleBan bans a member's portable identity (issuer, subject): they are
// removed and can never rejoin, whatever invite they use, until unbanned.
// Members who already left can be banned too.
func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason         string `json:"reason"`
		DeleteMessages int64  `json:"delete_messages"` // seconds of recent messages to delete; -1: all; 0: none
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requirePerm(r, permBanMembers)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	target, err := s.moderationTarget(r, ps, false)
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO bans (member_id, reason, banned_by, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (member_id) DO UPDATE SET reason = excluded.reason, banned_by = excluded.banned_by`,
		target.ID, reason, memberFrom(r).ID, s.nowMs()); err != nil {
		writeErr(w, r, err)
		return
	}
	wasActive := !target.LeftAt.Valid
	if err := s.removeMember(ctx, tx, target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, memberFrom(r).ID, auditMemberBan, target.ID, reason, map[string]any{"delete_messages": req.DeleteMessages})
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	if wasActive {
		s.afterRemoval(target.ID, leftBanned)
	}
	if req.DeleteMessages != 0 {
		if _, err := s.purge(ctx, memberFrom(r).ID, target.ID, nil, req.DeleteMessages, "ban"); err != nil {
			s.logErr("deleting a banned member's messages", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnban(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requirePerm(r, permBanMembers); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM bans WHERE member_id = ?`, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "this member is not banned"))
		return
	}
	s.audit(r.Context(), s.db, memberFrom(r).ID, auditMemberUnban, r.PathValue("id"), "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListBans(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requirePerm(r, permBanMembers); err != nil {
		writeErr(w, r, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT m.id, m.issuer, m.subject, m.handle, m.nickname, m.is_owner, m.joined_at, m.left_at,
		       b.reason, b.banned_by, b.created_at
		FROM bans b JOIN members m ON m.id = b.member_id ORDER BY b.created_at`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []banJSON{}
	for rows.Next() {
		var m member
		var b banJSON
		var by sql.NullString
		var created int64
		if err := rows.Scan(&m.ID, &m.Issuer, &m.Subject, &m.Handle, &m.Nickname, &m.IsOwner, &m.JoinedAt, &m.LeftAt,
			&b.Reason, &by, &created); err != nil {
			writeErr(w, r, err)
			return
		}
		b.Member, b.CreatedAt = m.json(), fromMs(created)
		if by.Valid {
			b.BannedBy = &by.String
		}
		list = append(list, b)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// --- timeout ---

const maxTimeout = 28 * 24 * time.Hour

// handleTimeout (PUT) makes a member read-only for a while; DELETE lifts it.
func (s *Server) handleTimeout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Duration int64  `json:"duration"` // seconds
		Reason   string `json:"reason"`
	}
	if r.Method == http.MethodPut {
		if err := decode(r, &req); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	d := time.Duration(req.Duration) * time.Second
	if r.Method == http.MethodPut && (d < time.Second || d > maxTimeout) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_duration", "duration must be 1 second to 28 days"))
		return
	}
	ps, err := s.requirePerm(r, permModerateMembers)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	target, err := s.moderationTarget(r, ps, true)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	actor := memberFrom(r).ID
	var until sql.NullInt64
	action := auditMemberTimeoutEnd
	if r.Method == http.MethodPut {
		if ps.base(target.ID)&permAdministrator != 0 {
			writeErr(w, r, errf(http.StatusBadRequest, "cannot_timeout_admin", "administrators cannot be timed out"))
			return
		}
		until = sql.NullInt64{Int64: s.now().Add(d).UnixMilli(), Valid: true}
		action = auditMemberTimeout
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE members SET timeout_until = ? WHERE id = ?`, until, target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, s.db, actor, action, target.ID, reason, map[string]any{"duration": req.Duration})
	target.TimeoutUntil = until
	view, err := s.memberView(ctx, target)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("MEMBER_UPDATE", view)
	s.syncPermissions(ctx) // read-only now: also leaves voice
	if until.Valid {
		// Give the member their permissions back on time (a restart loses this
		// timer: the member then gets them at their next reconnection).
		time.AfterFunc(d+time.Second, func() { s.syncPermissions(context.Background()) })
	}
	writeJSON(w, http.StatusOK, view)
}

// --- deleting many messages ---

// deleteMessages removes messages (with their files) and tells clients, one
// MESSAGE_DELETE_BULK event per channel.
func (s *Server) deleteMessages(ctx context.Context, byChannel map[int64][]int64) (int, error) {
	total := 0
	for ch, ids := range byChannel {
		if len(ids) == 0 {
			continue
		}
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		in := `(?` + strings.Repeat(",?", len(ids)-1) + `)`
		files := s.attachmentIDs(ctx, `message_id IN `+in, args...)
		if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id IN `+in, args...); err != nil {
			return total, err
		}
		s.removeFiles(files)
		total += len(ids)
		s.broadcastChannel(ctx, "MESSAGE_DELETE_BULK", ch, map[string]any{"channel_id": ch, "ids": ids})
	}
	return total, nil
}

// purge deletes a member's messages sent in the last window seconds (-1: all),
// in the given channels (nil: all channels).
func (s *Server) purge(ctx context.Context, actor, target string, channels []int64, window int64, reason string) (int, error) {
	where, args := `author_id = ?`, []any{target}
	if window > 0 {
		where, args = where+` AND created_at >= ?`, append(args, s.now().Add(-time.Duration(window)*time.Second).UnixMilli())
	}
	if channels != nil {
		if len(channels) == 0 {
			return 0, nil
		}
		where += ` AND channel_id IN (?` + strings.Repeat(",?", len(channels)-1) + `)`
		for _, c := range channels {
			args = append(args, c)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, channel_id FROM messages WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	byChannel := map[int64][]int64{}
	for rows.Next() {
		var id, ch int64
		if err := rows.Scan(&id, &ch); err != nil {
			rows.Close()
			return 0, err
		}
		byChannel[ch] = append(byChannel[ch], id)
	}
	rows.Close()
	n, err := s.deleteMessages(ctx, byChannel)
	if n > 0 {
		s.audit(ctx, s.db, actor, auditMessagesDelete, target, reason, map[string]any{"count": n, "window": window})
	}
	return n, err
}

// handlePurge: POST /v1/members/{id}/purge {window, channel_id?, reason} deletes a
// member's recent messages in every channel where the actor has manage_messages.
func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Window    int64  `json:"window"` // seconds; -1: everything
		ChannelID *int64 `json:"channel_id"`
		Reason    string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if req.Window == 0 || req.Window < -1 {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_window", "window must be a number of seconds, or -1 for all messages"))
		return
	}
	ps, err := s.loadPerms(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	target, err := s.moderationTarget(r, ps, false)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	channels := []int64{}
	for _, c := range ps.channels {
		if c.messaging() && ps.inChannel(me, c.ID)&permManageMessages != 0 && (req.ChannelID == nil || *req.ChannelID == c.ID) {
			channels = append(channels, c.ID)
		}
	}
	if len(channels) == 0 {
		writeErr(w, r, ps.deny(me, permManageMessages))
		return
	}
	n, err := s.purge(r.Context(), me, target.ID, channels, req.Window, reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

// handleBulkDelete: POST /v1/channels/{id}/messages/bulk-delete {ids, reason}.
func (s *Server) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Reason string  `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	reason, err := validReason(req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 100 {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_ids", "give 1 to 100 message IDs"))
		return
	}
	c, ps, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if ps.inChannel(me, c.ID)&permManageMessages == 0 {
		writeErr(w, r, ps.deny(me, permManageMessages))
		return
	}
	ctx := r.Context()
	args := []any{c.ID}
	for _, id := range req.IDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, author_id FROM messages WHERE channel_id = ? AND id IN (?`+strings.Repeat(",?", len(req.IDs)-1)+`)`, args...)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var ids []int64
	authors := map[string]int{}
	for rows.Next() {
		var id int64
		var author string
		if err := rows.Scan(&id, &author); err != nil {
			rows.Close()
			writeErr(w, r, err)
			return
		}
		ids = append(ids, id)
		authors[author]++
	}
	rows.Close()
	n, err := s.deleteMessages(ctx, map[int64][]int64{c.ID: ids})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	delete(authors, me)
	if len(authors) > 0 {
		s.audit(ctx, s.db, me, auditMessagesDelete, "", reason, map[string]any{"count": n, "channel_id": c.ID, "authors": authors})
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}
