package community

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type member struct {
	ID, Issuer, Subject, Handle string
	Nickname                    sql.NullString
	IsOwner                     bool
	JoinedAt                    int64
	LeftAt                      sql.NullInt64
	TimeoutUntil                sql.NullInt64
	RulesAcceptedAt             sql.NullInt64
	PhoneVerified               bool
	Bot                         bool
	ProfileAt, AvatarAt         int64 // versions of the profile on this server (see profiles.go)
	BannerAt                    int64
}

// deletedName stands for a member whose account was deleted on its identity service.
const deletedName = "Ancien compte"

const memberCols = `id, issuer, subject, handle, nickname, is_owner, joined_at, left_at, timeout_until, rules_accepted_at, phone_hash IS NOT NULL, bot,
	profile_at, avatar_at, banner_at`

func scanMember(sc interface{ Scan(...any) error }) (*member, error) {
	var m member
	err := sc.Scan(&m.ID, &m.Issuer, &m.Subject, &m.Handle, &m.Nickname, &m.IsOwner, &m.JoinedAt, &m.LeftAt,
		&m.TimeoutUntil, &m.RulesAcceptedAt, &m.PhoneVerified, &m.Bot, &m.ProfileAt, &m.AvatarAt, &m.BannerAt)
	return &m, err
}

// memberBy loads the member matching where, or nil if none.
func memberBy(ctx context.Context, q querier, where string, args ...any) (*member, error) {
	m, err := scanMember(q.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

type memberJSON struct {
	ID          string    `json:"id"`
	Handle      string    `json:"handle"` // pseudo@identity-service, may change
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"` // stable identity: (issuer, subject)
	Nickname    *string   `json:"nickname,omitempty"`
	DisplayName string    `json:"display_name"`
	Owner       bool      `json:"owner"`
	Roles       []int64   `json:"roles"` // role IDs, @everyone implied
	JoinedAt    time.Time `json:"joined_at"`
	Bot         bool      `json:"bot"`
	// TimeoutUntil: reading only until then (compare with the current time).
	TimeoutUntil  *time.Time `json:"timeout_until"`
	RulesAccepted bool       `json:"rules_accepted"`
	PhoneVerified bool       `json:"phone_verified"`
	// Profile on this server (profiles.go): versions, 0 = none. The bio and
	// theme come from GET /v1/members/{id}/profile, the images from
	// GET /v1/members/{id}/avatar|banner.
	ProfileV int64 `json:"profile_v,omitempty"`
	AvatarV  int64 `json:"avatar_v,omitempty"`
	BannerV  int64 `json:"banner_v,omitempty"`
}

func (m *member) json() memberJSON {
	j := memberJSON{ID: m.ID, Handle: m.Handle, Issuer: m.Issuer, Subject: m.Subject, Owner: m.IsOwner, Roles: []int64{}, JoinedAt: fromMs(m.JoinedAt),
		Bot: m.Bot, TimeoutUntil: nullTime(m.TimeoutUntil), RulesAccepted: m.RulesAcceptedAt.Valid, PhoneVerified: m.PhoneVerified,
		ProfileV: m.ProfileAt, AvatarV: m.AvatarAt, BannerV: m.BannerAt}
	j.DisplayName, _, _ = strings.Cut(m.Handle, "@")
	if m.Handle == "" { // account deleted on its identity service (see ApplyDeleted)
		j.DisplayName = deletedName
	}
	if m.Nickname.Valid {
		j.Nickname = &m.Nickname.String
		j.DisplayName = m.Nickname.String
	}
	return j
}

// memberView is the member's JSON with its roles.
func (s *Server) memberView(ctx context.Context, m *member) (memberJSON, error) {
	j := m.json()
	rows, err := s.db.QueryContext(ctx, `SELECT role_id FROM member_roles WHERE member_id = ? ORDER BY role_id`, m.ID)
	if err != nil {
		return j, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return j, err
		}
		j.Roles = append(j.Roles, id)
	}
	return j, rows.Err()
}

func (s *Server) activeMembers(ctx context.Context) ([]memberJSON, error) {
	roles, err := allMemberRoles(ctx, s.db)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE left_at IS NULL ORDER BY joined_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []memberJSON{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		j := m.json()
		if r := roles[m.ID]; r != nil {
			j.Roles = r
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	list, err := s.activeMembers(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	view, err := s.memberView(r.Context(), memberFrom(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request) {
	m := memberFrom(r)
	if m.IsOwner {
		writeErr(w, r, errf(http.StatusBadRequest, "owner_cannot_leave", "the owner cannot leave the server"))
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if err := s.removeMember(ctx, tx, m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.afterRemoval(m.ID, leftVoluntarily)
	w.WriteHeader(http.StatusNoContent)
}
