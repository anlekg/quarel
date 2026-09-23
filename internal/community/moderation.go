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
	s.hub.disconnectMember(memberID)
	s.hub.broadcast("MEMBER_LEAVE", map[string]string{"id": memberID, "reason": reason})
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
	if _, err := validReason(req.Reason); err != nil {
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
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	if wasActive {
		s.afterRemoval(target.ID, leftBanned)
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
