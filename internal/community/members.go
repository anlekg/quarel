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
}

const memberCols = `id, issuer, subject, handle, nickname, is_owner, joined_at, left_at`

func scanMember(sc interface{ Scan(...any) error }) (*member, error) {
	var m member
	err := sc.Scan(&m.ID, &m.Issuer, &m.Subject, &m.Handle, &m.Nickname, &m.IsOwner, &m.JoinedAt, &m.LeftAt)
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
	JoinedAt    time.Time `json:"joined_at"`
}

func (m *member) json() memberJSON {
	j := memberJSON{ID: m.ID, Handle: m.Handle, Issuer: m.Issuer, Subject: m.Subject, Owner: m.IsOwner, JoinedAt: fromMs(m.JoinedAt)}
	j.DisplayName, _, _ = strings.Cut(m.Handle, "@")
	if m.Nickname.Valid {
		j.Nickname = &m.Nickname.String
		j.DisplayName = m.Nickname.String
	}
	return j
}

func (s *Server) activeMembers(ctx context.Context) ([]memberJSON, error) {
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
		list = append(list, m.json())
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
	writeJSON(w, http.StatusOK, memberFrom(r).json())
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname *string `json:"nickname"` // "" or null clears it
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	m := memberFrom(r)
	m.Nickname = sql.NullString{}
	if req.Nickname != nil {
		if nick := strings.TrimSpace(*req.Nickname); nick != "" {
			if len([]rune(nick)) > 32 {
				writeErr(w, r, errf(http.StatusBadRequest, "invalid_nickname", "nickname must be at most 32 characters"))
				return
			}
			m.Nickname = sql.NullString{String: nick, Valid: true}
		}
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE members SET nickname = ? WHERE id = ?`, m.Nickname, m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.broadcast("MEMBER_UPDATE", m.json())
	writeJSON(w, http.StatusOK, m.json())
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
	if _, err := tx.ExecContext(ctx, `UPDATE members SET left_at = ? WHERE id = ?`, s.nowMs(), m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE member_id = ?`, m.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.disconnectMember(m.ID)
	s.hub.broadcast("MEMBER_LEAVE", map[string]string{"id": m.ID})
	w.WriteHeader(http.StatusNoContent)
}
