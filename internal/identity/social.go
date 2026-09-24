package identity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Friendship statuses as seen by one of the two users.
const (
	relNone     = "none"
	relFriends  = "friends"
	relIncoming = "incoming" // they asked me
	relOutgoing = "outgoing" // I asked them
)

type publicUser struct {
	ID       string `json:"id"`
	Handle   string `json:"handle"`
	Pseudo   string `json:"pseudo"`
	Presence string `json:"presence,omitempty"` // friends only: online, idle, dnd, offline
}

func (s *Server) publicUser(u *user) publicUser {
	return publicUser{ID: u.ID, Handle: s.handle(u), Pseudo: u.Pseudo}
}

func pair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// relation returns how other relates to me.
func (s *Server) relation(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, me, other string) (string, error) {
	a, b := pair(me, other)
	var status, requester string
	err := q.QueryRowContext(ctx, `SELECT status, requester FROM friendships WHERE user_a = ? AND user_b = ?`, a, b).Scan(&status, &requester)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return relNone, nil
	case err != nil:
		return "", err
	case status == "accepted":
		return relFriends, nil
	case requester == me:
		return relOutgoing, nil
	default:
		return relIncoming, nil
	}
}

// requireFriendOrSelf allows access to another user's keys and mailbox only to friends.
func (s *Server) requireFriendOrSelf(ctx context.Context, me, other string) error {
	if me == other {
		return nil
	}
	rel, err := s.relation(ctx, s.db, me, other)
	if err != nil {
		return err
	}
	if rel != relFriends {
		return errf(http.StatusForbidden, "not_friends", "only friends can do this")
	}
	return nil
}

func (s *Server) handleListFriends(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r).UserID
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END, status, requester
		FROM friendships WHERE user_a = ? OR user_b = ? ORDER BY created_at`, me, me, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	type entry struct{ id, status, requester string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.status, &e.requester); err != nil {
			rows.Close()
			writeErr(w, r, err)
			return
		}
		entries = append(entries, e)
	}
	rows.Close()
	out := map[string][]publicUser{"friends": {}, "incoming": {}, "outgoing": {}}
	for _, e := range entries {
		u, err := s.userBy(r.Context(), "id", e.id)
		if err != nil || u == nil {
			continue
		}
		key := "friends"
		if e.status == "pending" {
			key = "incoming"
			if e.requester == me {
				key = "outgoing"
			}
		}
		pu := s.publicUser(u)
		if key == "friends" {
			pu.Presence = s.presenceOf(r.Context(), u.ID)
		}
		out[key] = append(out[key], pu)
	}
	writeJSON(w, http.StatusOK, out)
}

// friendsChanged tells both users' devices about their new relation.
func (s *Server) friendsChanged(ctx context.Context, a, b *user) {
	for _, pairUsers := range [][2]*user{{a, b}, {b, a}} {
		me, other := pairUsers[0], pairUsers[1]
		rel, err := s.relation(ctx, s.db, me.ID, other.ID)
		if err != nil {
			continue
		}
		s.hub.BroadcastTo("FRIENDS_UPDATE", map[string]any{"user": s.publicUser(other), "status": rel},
			func(_, group string) bool { return group == me.ID })
	}
}

// handleAddFriend sends a friend request by pseudo, or accepts theirs if they already asked.
func (s *Server) handleAddFriend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pseudo string `json:"pseudo"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	me, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.friends.Check(me.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	other, err := s.userBy(ctx, "pseudo_norm", strings.ToLower(strings.TrimSpace(req.Pseudo)))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if other == nil || other.DisabledAt.Valid || !other.EmailVerifiedAt.Valid {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such user"))
		return
	}
	if other.ID == me.ID {
		writeErr(w, r, errf(http.StatusBadRequest, "self_friend", "you cannot befriend yourself"))
		return
	}
	var blocker string
	err = s.db.QueryRowContext(ctx, `SELECT user_id FROM blocks WHERE (user_id = ? AND blocked_id = ?) OR (user_id = ? AND blocked_id = ?)`,
		me.ID, other.ID, other.ID, me.ID).Scan(&blocker)
	switch {
	case err == nil && blocker == me.ID:
		writeErr(w, r, errf(http.StatusForbidden, "blocked", "you blocked this user: unblock them first"))
		return
	case err == nil: // they blocked me: answer as if they did not exist
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such user"))
		return
	case !errors.Is(err, sql.ErrNoRows):
		writeErr(w, r, err)
		return
	}
	rel, err := s.relation(ctx, s.db, me.ID, other.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	a, b := pair(me.ID, other.ID)
	switch rel {
	case relFriends:
		writeErr(w, r, errf(http.StatusConflict, "already_friends", "you are already friends"))
		return
	case relOutgoing:
		writeErr(w, r, errf(http.StatusConflict, "already_requested", "friend request already sent"))
		return
	case relIncoming:
		_, err = s.db.ExecContext(ctx, `UPDATE friendships SET status = 'accepted' WHERE user_a = ? AND user_b = ?`, a, b)
	default:
		_, err = s.db.ExecContext(ctx, `INSERT INTO friendships (user_a, user_b, status, requester, created_at) VALUES (?, ?, 'pending', ?, ?)`,
			a, b, me.ID, s.now().Unix())
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.friendsChanged(ctx, me, other)
	rel, _ = s.relation(ctx, s.db, me.ID, other.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": s.publicUser(other), "status": rel})
}

func (s *Server) pathOtherUser(r *http.Request) (*user, *user, error) {
	me, err := s.currentUser(r)
	if err != nil {
		return nil, nil, err
	}
	other, err := s.userBy(r.Context(), "id", r.PathValue("id"))
	if err != nil {
		return nil, nil, err
	}
	if other == nil {
		return nil, nil, errf(http.StatusNotFound, "not_found", "no such user")
	}
	return me, other, nil
}

func (s *Server) handleAcceptFriend(w http.ResponseWriter, r *http.Request) {
	me, other, err := s.pathOtherUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	rel, err := s.relation(ctx, s.db, me.ID, other.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if rel != relIncoming {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no pending request from this user"))
		return
	}
	a, b := pair(me.ID, other.ID)
	if _, err := s.db.ExecContext(ctx, `UPDATE friendships SET status = 'accepted' WHERE user_a = ? AND user_b = ?`, a, b); err != nil {
		writeErr(w, r, err)
		return
	}
	s.friendsChanged(ctx, me, other)
	writeJSON(w, http.StatusOK, map[string]any{"user": s.publicUser(other), "status": relFriends})
}

// handleRemoveFriend removes a friend, declines their request or cancels mine.
func (s *Server) handleRemoveFriend(w http.ResponseWriter, r *http.Request) {
	me, other, err := s.pathOtherUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	a, b := pair(me.ID, other.ID)
	res, err := s.db.ExecContext(ctx, `DELETE FROM friendships WHERE user_a = ? AND user_b = ?`, a, b)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no friendship or request with this user"))
		return
	}
	s.friendsChanged(ctx, me, other)
	w.WriteHeader(http.StatusNoContent)
}

// --- direct message conversations ---

type dmJSON struct {
	ID        string     `json:"id"`
	User      publicUser `json:"user"` // the other participant
	CreatedAt time.Time  `json:"created_at"`
}

// handleOpenDM returns the conversation with a friend, creating it on first use.
func (s *Server) handleOpenDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
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
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO dms (id, user_a, user_b, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (user_a, user_b) DO NOTHING`,
		newID(), a, b, now); err != nil {
		writeErr(w, r, err)
		return
	}
	var dm dmJSON
	var created int64
	if err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM dms WHERE user_a = ? AND user_b = ?`, a, b).Scan(&dm.ID, &created); err != nil {
		writeErr(w, r, err)
		return
	}
	dm.User, dm.CreatedAt = s.publicUser(other), time.Unix(created, 0).UTC()
	writeJSON(w, http.StatusOK, dm)
}

func (s *Server) handleListDMs(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r).UserID
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, CASE WHEN user_a = ? THEN user_b ELSE user_a END, created_at
		FROM dms WHERE user_a = ? OR user_b = ? ORDER BY created_at`, me, me, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	type row struct {
		id, other string
		created   int64
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.other, &x.created); err != nil {
			rows.Close()
			writeErr(w, r, err)
			return
		}
		list = append(list, x)
	}
	rows.Close()
	out := []dmJSON{}
	for _, x := range list {
		u, err := s.userBy(r.Context(), "id", x.other)
		if err != nil || u == nil {
			continue
		}
		out = append(out, dmJSON{ID: x.id, User: s.publicUser(u), CreatedAt: time.Unix(x.created, 0).UTC()})
	}
	writeJSON(w, http.StatusOK, out)
}

// dmMembers returns the two users of a DM the requester belongs to.
func (s *Server) dmMembers(ctx context.Context, dmID, me string) (string, string, error) {
	var a, b string
	err := s.db.QueryRowContext(ctx, `SELECT user_a, user_b FROM dms WHERE id = ?`, dmID).Scan(&a, &b)
	if errors.Is(err, sql.ErrNoRows) || err == nil && me != a && me != b {
		return "", "", errf(http.StatusNotFound, "not_found", "no such conversation")
	}
	return a, b, err
}
