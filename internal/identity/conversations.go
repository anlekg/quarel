package identity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Conversations: direct (two friends) or group (up to cfg.GroupMaxMembers people,
// created among friends). Messages are Megolm ciphertexts the server only
// relays; edits and deletions are encrypted events too, applied by clients.

const (
	maxGroupNameLen = 100
	convKindDirect  = "direct"
	convKindGroup   = "group"
)

type convJSON struct {
	ID        string       `json:"id"`
	Kind      string       `json:"kind"`
	Name      string       `json:"name"`
	OwnerID   *string      `json:"owner_id"`
	Members   []publicUser `json:"members"`
	User      *publicUser  `json:"user,omitempty"` // direct: the other participant
	CreatedAt time.Time    `json:"created_at"`
}

// convMembers returns the members of a conversation the requester belongs to.
func (s *Server) convMembers(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, convID, me string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id = ? ORDER BY joined_at, rowid`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []string
	in := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		members = append(members, id)
		in = in || id == me
	}
	if !in {
		return nil, errf(http.StatusNotFound, "not_found", "no such conversation")
	}
	return members, rows.Err()
}

func (s *Server) conversation(ctx context.Context, convID, viewer string) (convJSON, error) {
	var c convJSON
	var owner sql.NullString
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, name, owner_id, created_at FROM conversations WHERE id = ?`, convID).
		Scan(&c.ID, &c.Kind, &c.Name, &owner, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return c, errf(http.StatusNotFound, "not_found", "no such conversation")
	}
	if err != nil {
		return c, err
	}
	if owner.Valid {
		c.OwnerID = &owner.String
	}
	c.CreatedAt = time.Unix(created, 0).UTC()
	ids, err := s.convMembers(ctx, s.db, convID, viewer)
	if err != nil {
		return c, err
	}
	c.Members = []publicUser{}
	for _, id := range ids {
		u, err := s.userBy(ctx, "id", id)
		if err != nil || u == nil {
			continue
		}
		pu := s.publicUser(u)
		c.Members = append(c.Members, pu)
		if c.Kind == convKindDirect && id != viewer {
			c.User = &pu
		}
	}
	return c, nil
}

// requireContact allows exchanging keys and messages with friends and with
// the members of a shared conversation (group members need not be friends).
func (s *Server) requireContact(ctx context.Context, me, other string) error {
	if me == other {
		return nil
	}
	rel, err := s.relation(ctx, s.db, me, other)
	if err != nil {
		return err
	}
	if rel == relFriends {
		return nil
	}
	var shared bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM conversation_members a JOIN conversation_members b
		ON a.conversation_id = b.conversation_id WHERE a.user_id = ? AND b.user_id = ?)`, me, other).Scan(&shared); err != nil {
		return err
	}
	if !shared {
		return errf(http.StatusForbidden, "not_friends", "only friends and conversation members can do this")
	}
	return nil
}

// contacts returns the friends and co-members of a user.
func (s *Server) contacts(ctx context.Context, userID string) (map[string]bool, error) {
	out, err := s.friendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT b.user_id FROM conversation_members a JOIN conversation_members b
		ON a.conversation_id = b.conversation_id WHERE a.user_id = ? AND b.user_id != ?`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// convChanged tells each member their view of a conversation (DM_UPDATE),
// and the removed members that they left it (DM_REMOVED).
func (s *Server) convChanged(ctx context.Context, convID string, removed ...string) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id = ?`, convID)
	if err != nil {
		return
	}
	var members []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			members = append(members, id)
		}
	}
	rows.Close()
	for _, id := range members {
		c, err := s.conversation(ctx, convID, id)
		if err != nil {
			continue
		}
		s.hub.BroadcastTo("DM_UPDATE", c, func(_, group string) bool { return group == id })
	}
	for _, id := range removed {
		s.hub.BroadcastTo("DM_REMOVED", map[string]string{"id": convID}, func(_, group string) bool { return group == id })
	}
}

// handleOpenDM: POST /v1/dms {user_id} opens (or returns) the direct
// conversation with a friend; {user_ids, name} creates a group of friends.
func (s *Server) handleOpenDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string   `json:"user_id"`
		UserIDs []string `json:"user_ids"`
		Name    string   `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.UserIDs != nil {
		s.createGroup(w, r, req.UserIDs, req.Name)
		return
	}
	ctx := r.Context()
	me := sessionFrom(r).UserID
	other, err := s.userBy(ctx, "id", req.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if other == nil || other.ID == me {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such user"))
		return
	}
	if err := s.requireFriendOrSelf(ctx, me, other.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	a, b := pair(me, other.ID)
	var convID string
	err = s.db.QueryRowContext(ctx, `SELECT conversation_id FROM direct_pairs WHERE user_a = ? AND user_b = ?`, a, b).Scan(&convID)
	if errors.Is(err, sql.ErrNoRows) {
		convID = newID()
		now := s.now().Unix()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		defer tx.Rollback()
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO conversations (id, kind, created_at) VALUES (?, 'direct', ?)`, []any{convID, now}},
			{`INSERT INTO conversation_members (conversation_id, user_id, joined_at) VALUES (?, ?, ?), (?, ?, ?)`, []any{convID, a, now, convID, b, now}},
			{`INSERT INTO direct_pairs (user_a, user_b, conversation_id) VALUES (?, ?, ?)`, []any{a, b, convID}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, r, err)
			return
		}
		s.convChanged(ctx, convID) // the other person sees it appear
	} else if err != nil {
		writeErr(w, r, err)
		return
	}
	c, err := s.conversation(ctx, convID, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func validGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxGroupNameLen {
		return "", errf(http.StatusBadRequest, "invalid_name", "group names are 1-%d characters", maxGroupNameLen)
	}
	return name, nil
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request, userIDs []string, name string) {
	name, err := validGroupName(name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me := sessionFrom(r).UserID
	members := []string{me}
	seen := map[string]bool{me: true}
	for _, id := range userIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := s.requireFriendOrSelf(ctx, me, id); err != nil {
			writeErr(w, r, errf(http.StatusForbidden, "not_friends", "a group can only be created with friends"))
			return
		}
		members = append(members, id)
	}
	if len(members) < 2 || len(members) > s.cfg.GroupMaxMembers {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_members", "a group has 2 to %d members", s.cfg.GroupMaxMembers))
		return
	}
	convID, now := newID(), s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations (id, kind, name, owner_id, created_at) VALUES (?, 'group', ?, ?, ?)`, convID, name, me, now); err != nil {
		writeErr(w, r, err)
		return
	}
	for _, id := range members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_members (conversation_id, user_id, joined_at) VALUES (?, ?, ?)`, convID, id, now); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.convChanged(ctx, convID)
	c, err := s.conversation(ctx, convID, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListDMs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := sessionFrom(r).UserID
	rows, err := s.db.QueryContext(ctx, `SELECT c.id FROM conversations c JOIN conversation_members m ON m.conversation_id = c.id
		WHERE m.user_id = ? ORDER BY c.created_at, c.id`, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []convJSON{}
	for _, id := range ids {
		c, err := s.conversation(ctx, id, me)
		if err != nil {
			continue
		}
		if c.Kind == convKindDirect && c.User == nil {
			continue // the other participant deleted their account
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// groupFor loads a group the requester belongs to.
func (s *Server) groupFor(r *http.Request) (convJSON, []string, error) {
	me := sessionFrom(r).UserID
	c, err := s.conversation(r.Context(), r.PathValue("id"), me)
	if err != nil {
		return c, nil, err
	}
	if c.Kind != convKindGroup {
		return c, nil, errf(http.StatusBadRequest, "not_a_group", "direct conversations have no member management")
	}
	ids := make([]string, len(c.Members))
	for i, m := range c.Members {
		ids[i] = m.ID
	}
	return c, ids, nil
}

// handleRenameGroup: PATCH /v1/dms/{id} {name}, by any member.
func (s *Server) handleRenameGroup(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	name, err := validGroupName(req.Name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	c, _, err := s.groupFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE conversations SET name = ? WHERE id = ?`, name, c.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.convChanged(r.Context(), c.ID)
	c, err = s.conversation(r.Context(), c.ID, sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleAddGroupMember: PUT /v1/dms/{id}/members/{user}. Any member may add
// one of their friends. The newcomer only reads messages sent after joining:
// senders renew their conversation key and share it with them.
func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := sessionFrom(r).UserID
	c, ids, err := s.groupFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	target := r.PathValue("user")
	for _, id := range ids {
		if id == target {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	if err := s.requireFriendOrSelf(ctx, me, target); err != nil {
		writeErr(w, r, errf(http.StatusForbidden, "not_friends", "you can only add your friends"))
		return
	}
	if len(ids) >= s.cfg.GroupMaxMembers {
		writeErr(w, r, errf(http.StatusBadRequest, "group_full", "a group has at most %d members", s.cfg.GroupMaxMembers))
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO conversation_members (conversation_id, user_id, joined_at) VALUES (?, ?, ?)`,
		c.ID, target, s.now().Unix()); err != nil {
		writeErr(w, r, err)
		return
	}
	s.convChanged(ctx, c.ID)
	c, err = s.conversation(ctx, c.ID, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleRemoveGroupMember: DELETE /v1/dms/{id}/members/{user} — leave (self)
// or remove someone (owner only). An owner who leaves hands the group to the
// longest-standing member; the last one to leave deletes it.
func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := sessionFrom(r).UserID
	c, ids, err := s.groupFor(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	target := r.PathValue("user")
	if target == "@me" {
		target = me
	}
	if target != me && (c.OwnerID == nil || *c.OwnerID != me) {
		writeErr(w, r, errf(http.StatusForbidden, "not_owner", "only the group owner can remove members"))
		return
	}
	found := false
	var rest []string
	for _, id := range ids {
		if id == target {
			found = true
		} else {
			rest = append(rest, id)
		}
	}
	if !found {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "not a member of this group"))
		return
	}
	if len(rest) == 0 {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, c.ID); err != nil {
			writeErr(w, r, err)
			return
		}
		s.removeConvFiles(ctx, c.ID)
		s.convChanged(ctx, c.ID, target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_members WHERE conversation_id = ? AND user_id = ?`, c.ID, target); err != nil {
		writeErr(w, r, err)
		return
	}
	if c.OwnerID != nil && *c.OwnerID == target { // rest is in join order
		if _, err := s.db.ExecContext(ctx, `UPDATE conversations SET owner_id = ? WHERE id = ?`, rest[0], c.ID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	// Senders see the new member list and renew their conversation key without them.
	s.convChanged(ctx, c.ID, target)
	w.WriteHeader(http.StatusNoContent)
}
