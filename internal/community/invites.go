package community

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/anlekg/quarel/internal/secret"
)

const defaultInviteTTL = 7 * 24 * time.Hour

type invite struct {
	Code      string     `json:"code"`
	ServerID  string     `json:"server_id"`
	CreatorID *string    `json:"creator_id"`
	MaxUses   *int64     `json:"max_uses"` // null: unlimited
	Uses      int64      `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"` // null: never
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxUses   int64  `json:"max_uses"`   // 0: unlimited
		ExpiresIn *int64 `json:"expires_in"` // seconds; absent: 7 days; 0: never
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.MaxUses < 0 || req.ExpiresIn != nil && *req.ExpiresIn < 0 {
		writeErr(w, r, errf(http.StatusBadRequest, "bad_request", "max_uses and expires_in must be positive"))
		return
	}
	m := memberFrom(r)
	now := s.now()
	inv := invite{Code: secret.Code(6), ServerID: s.id, CreatorID: &m.ID, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	var maxUses, expires sql.NullInt64
	if req.MaxUses > 0 {
		inv.MaxUses = &req.MaxUses
		maxUses = sql.NullInt64{Int64: req.MaxUses, Valid: true}
	}
	ttl := defaultInviteTTL
	if req.ExpiresIn != nil {
		ttl = time.Duration(*req.ExpiresIn) * time.Second
	}
	if ttl > 0 {
		exp := now.Add(ttl).UTC().Truncate(time.Millisecond)
		inv.ExpiresAt = &exp
		expires = sql.NullInt64{Int64: exp.UnixMilli(), Valid: true}
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO invites (code, creator_id, max_uses, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		inv.Code, m.ID, maxUses, expires, now.UnixMilli())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

// handleListInvites shows every invite to the owner, and their own to other members.
func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	m := memberFrom(r)
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT code, creator_id, max_uses, uses, expires_at, created_at FROM invites
		WHERE ? OR creator_id = ? ORDER BY created_at`, m.IsOwner, m.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []invite{}
	for rows.Next() {
		inv := invite{ServerID: s.id}
		var creator sql.NullString
		var maxUses, expires sql.NullInt64
		var created int64
		if err := rows.Scan(&inv.Code, &creator, &maxUses, &inv.Uses, &expires, &created); err != nil {
			writeErr(w, r, err)
			return
		}
		if creator.Valid {
			inv.CreatorID = &creator.String
		}
		if maxUses.Valid {
			inv.MaxUses = &maxUses.Int64
		}
		inv.ExpiresAt = nullTime(expires)
		inv.CreatedAt = fromMs(created)
		list = append(list, inv)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	m := memberFrom(r)
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM invites WHERE code = ? AND (? OR creator_id = ?)`,
		r.PathValue("code"), m.IsOwner, m.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such invite (or not yours)"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
