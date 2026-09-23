package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// everyoneRoleID is the @everyone role (migration 2): every member has it
// implicitly, it sits at position 0 and cannot be renamed, moved or deleted.
const everyoneRoleID = 1

// role positions: @everyone is 0, other roles are numbered 1..n, higher = more powerful.
type role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Color       int64    `json:"color"` // 0xRRGGBB, 0 = none
	Position    int64    `json:"position"`
	Permissions []string `json:"permissions"`
	Mentionable bool     `json:"mentionable"` // anyone may ping it with <@&id>
	perms       perm
}

func allRoles(ctx context.Context, q querier) ([]*role, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name, color, position, permissions, mentionable FROM roles ORDER BY position DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*role{}
	for rows.Next() {
		var r role
		if err := rows.Scan(&r.ID, &r.Name, &r.Color, &r.Position, &r.perms, &r.Mentionable); err != nil {
			return nil, err
		}
		r.Permissions = r.perms.names()
		list = append(list, &r)
	}
	return list, rows.Err()
}

func roleByID(ctx context.Context, q querier, id int64) (*role, error) {
	var r role
	err := q.QueryRowContext(ctx, `SELECT id, name, color, position, permissions, mentionable FROM roles WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.Color, &r.Position, &r.perms, &r.Mentionable)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusNotFound, "not_found", "no such role")
	}
	r.Permissions = r.perms.names()
	return &r, err
}

// allMemberRoles maps member IDs to their role IDs (@everyone excluded, it is implicit).
func allMemberRoles(ctx context.Context, q querier) (map[string][]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT member_id, role_id FROM member_roles ORDER BY role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var m string
		var r int64
		if err := rows.Scan(&m, &r); err != nil {
			return nil, err
		}
		out[m] = append(out[m], r)
	}
	return out, rows.Err()
}

// renumberRoles reorders roles as given (lowest first) at positions 1..n.
func renumberRoles(ctx context.Context, tx *sql.Tx, ordered []int64) error {
	for i, id := range ordered {
		if _, err := tx.ExecContext(ctx, `UPDATE roles SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// orderedRoleIDs returns the non-@everyone role IDs, lowest position first.
func orderedRoleIDs(ctx context.Context, q querier) ([]int64, error) {
	roles, err := allRoles(ctx, q)
	if err != nil {
		return nil, err
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position < roles[j].Position })
	ids := []int64{}
	for _, r := range roles {
		if r.ID != everyoneRoleID {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

func pathRoleID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, errf(http.StatusNotFound, "not_found", "no such role")
	}
	return id, nil
}

// checkRoleRank ensures the actor may manage role r (strictly below their top role).
func checkRoleRank(ps *permSnapshot, actor string, r *role) error {
	if r.Position >= ps.top(actor) {
		return errf(http.StatusForbidden, "role_hierarchy", "you can only manage roles below your highest role")
	}
	return nil
}

// checkGrant ensures the actor only adds or removes permissions they have themselves.
func checkGrant(ps *permSnapshot, actor string, changed perm) error {
	if extra := changed &^ ps.base(actor); extra != 0 {
		return errf(http.StatusForbidden, "missing_permissions", "you cannot grant or revoke permissions you do not have: %s",
			strings.Join(extra.names(), ", "))
	}
	return nil
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := allRoles(r.Context(), s.db)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func validRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 || strings.HasPrefix(name, "@") {
		return "", errf(http.StatusBadRequest, "invalid_name", "role name must be 1-100 characters and not start with @")
	}
	return name, nil
}

func validColor(c int64) error {
	if c < 0 || c > 0xFFFFFF {
		return errf(http.StatusBadRequest, "invalid_color", "color must be 0xRRGGBB (0-16777215)")
	}
	return nil
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Color       int64    `json:"color"`
		Permissions []string `json:"permissions"`
		Mentionable bool     `json:"mentionable"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requirePerm(r, permManageRoles)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	name, err := validRoleName(req.Name)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := validColor(req.Color); err != nil {
		writeErr(w, r, err)
		return
	}
	p, err := parsePerms(req.Permissions)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := checkGrant(ps, memberFrom(r).ID, p); err != nil {
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
	// New roles start at the bottom, just above @everyone.
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET position = position + 1 WHERE id != ?`, everyoneRoleID); err != nil {
		writeErr(w, r, err)
		return
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO roles (name, color, position, permissions, mentionable, created_at) VALUES (?, ?, 1, ?, ?, ?)`,
		name, req.Color, p, req.Mentionable, s.nowMs())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.rolesChanged(ctx)
	rl, err := roleByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rl)
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        *string   `json:"name"`
		Color       *int64    `json:"color"`
		Permissions *[]string `json:"permissions"`
		Mentionable *bool     `json:"mentionable"`
		Position    *int64    `json:"position"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ps, err := s.requirePerm(r, permManageRoles)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	id, err := pathRoleID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	rl, err := roleByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	actor := memberFrom(r).ID
	if err := checkRoleRank(ps, actor, rl); err != nil {
		writeErr(w, r, err)
		return
	}
	if id == everyoneRoleID && (req.Name != nil || req.Position != nil) {
		writeErr(w, r, errf(http.StatusBadRequest, "everyone_role", "@everyone can only have its permissions, color and mentionability changed"))
		return
	}
	if req.Name != nil {
		if rl.Name, err = validRoleName(*req.Name); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if req.Color != nil {
		if err := validColor(*req.Color); err != nil {
			writeErr(w, r, err)
			return
		}
		rl.Color = *req.Color
	}
	if req.Permissions != nil {
		p, err := parsePerms(*req.Permissions)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if err := checkGrant(ps, actor, p^rl.perms); err != nil {
			writeErr(w, r, err)
			return
		}
		rl.perms = p
	}
	if req.Mentionable != nil {
		rl.Mentionable = *req.Mentionable
	}
	if req.Position != nil && (*req.Position < 1 || *req.Position >= ps.top(actor)) {
		writeErr(w, r, errf(http.StatusForbidden, "role_hierarchy", "position must be at least 1 and below your highest role"))
		return
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET name = ?, color = ?, permissions = ?, mentionable = ? WHERE id = ?`,
		rl.Name, rl.Color, rl.perms, rl.Mentionable, id); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.Position != nil {
		ids, err := orderedRoleIDs(ctx, tx)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		moved := []int64{}
		for _, x := range ids {
			if x != id {
				moved = append(moved, x)
			}
		}
		at := min(int(*req.Position-1), len(moved))
		moved = append(moved[:at], append([]int64{id}, moved[at:]...)...)
		if err := renumberRoles(ctx, tx, moved); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.rolesChanged(ctx)
	if rl, err = roleByID(ctx, s.db, id); err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rl)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	ps, err := s.requirePerm(r, permManageRoles)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	id, err := pathRoleID(r, "id")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if id == everyoneRoleID {
		writeErr(w, r, errf(http.StatusBadRequest, "everyone_role", "@everyone cannot be deleted"))
		return
	}
	rl, err := roleByID(ctx, s.db, id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := checkRoleRank(ps, memberFrom(r).ID, rl); err != nil {
		writeErr(w, r, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	// Overrides have no foreign key (their target is a role or a member).
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_overrides WHERE target_type = 'role' AND target_id = ?`, strconv.FormatInt(id, 10)); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id); err != nil {
		writeErr(w, r, err)
		return
	}
	ids, err := orderedRoleIDs(ctx, tx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := renumberRoles(ctx, tx, ids); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("ROLE_DELETE", map[string]int64{"id": id})
	s.rolesChanged(ctx)
	w.WriteHeader(http.StatusNoContent)
}

// rolesChanged tells clients about the new role list and their new permissions.
func (s *Server) rolesChanged(ctx context.Context) {
	if roles, err := allRoles(ctx, s.db); err == nil {
		s.hub.Broadcast("ROLES_UPDATE", roles)
	}
	s.syncPermissions(ctx)
}

// --- member roles ---

func (s *Server) handleMemberRole(w http.ResponseWriter, r *http.Request) {
	ps, err := s.requirePerm(r, permManageRoles)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	roleID, err := pathRoleID(r, "role")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if roleID == everyoneRoleID {
		writeErr(w, r, errf(http.StatusBadRequest, "everyone_role", "every member has @everyone"))
		return
	}
	rl, err := roleByID(ctx, s.db, roleID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := checkRoleRank(ps, memberFrom(r).ID, rl); err != nil {
		writeErr(w, r, err)
		return
	}
	target, err := memberBy(ctx, s.db, `id = ? AND left_at IS NULL`, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if target == nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such member"))
		return
	}
	q := `INSERT OR IGNORE INTO member_roles (member_id, role_id) VALUES (?, ?)`
	if r.Method == http.MethodDelete {
		q = `DELETE FROM member_roles WHERE member_id = ? AND role_id = ?`
	}
	if _, err := s.db.ExecContext(ctx, q, target.ID, roleID); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := s.memberView(ctx, target)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("MEMBER_UPDATE", view)
	s.syncPermissions(ctx)
	writeJSON(w, http.StatusOK, view)
}
