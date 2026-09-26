package community

import (
	"context"
	"net/http"
)

// Ownership: the owner hands the server over to another member, or the host
// (administration page) takes it back when the owner can no longer sign in
// (account lost or deleted) and gets a new claim link.

// handleTransferOwnership: POST /v1/members/{id}/transfer-ownership, by the owner.
func (s *Server) handleTransferOwnership(w http.ResponseWriter, r *http.Request) {
	me := memberFrom(r)
	if !me.IsOwner {
		writeErr(w, r, errf(http.StatusForbidden, "not_owner", "only the owner can hand the server over"))
		return
	}
	ctx := r.Context()
	target, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if target == nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such member"))
		return
	}
	if target.ID == me.ID || target.Bot {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_target", "the server goes to another person (not a bot)"))
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE members SET is_owner = (id = ?) WHERE is_owner = 1 OR id = ?`, target.ID, target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, me.ID, auditOwnerTransfer, target.ID, "", nil)
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.ownersChanged(ctx, me.ID, target.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ResetOwnership removes the owner(s) and returns a new one-time claim code
// (see PrepareClaim). For the host, from the administration page.
func (s *Server) ResetOwnership(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM members WHERE is_owner = 1`)
	if err != nil {
		return "", err
	}
	var former []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			former = append(former, id)
		}
	}
	rows.Close()
	if _, err := s.db.ExecContext(ctx, `UPDATE members SET is_owner = 0 WHERE is_owner = 1`); err != nil {
		return "", err
	}
	s.audit(ctx, s.db, "", auditOwnerReset, "", "", map[string]any{"former": former})
	s.ownersChanged(ctx, former...)
	return s.PrepareClaim(ctx)
}

func (s *Server) ownersChanged(ctx context.Context, ids ...string) {
	for _, id := range ids {
		if m, err := memberBy(ctx, s.db, `id = ?`, id); err == nil && m != nil {
			if view, err := s.memberView(ctx, m); err == nil {
				s.hub.Broadcast("MEMBER_UPDATE", view)
			}
		}
	}
	s.syncPermissions(ctx)
}
