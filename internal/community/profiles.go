package community

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/anlekg/quarel/internal/theme"
)

// Each member's profile on this server (cosmetics block, decided by the PM):
// a bio, the theme of their profile card (internal/theme, filtered again by
// the apps), an avatar and a banner, all optional (the apps fall back on the
// profile of the identity service). The moderation can reset it
// (moderate_members and the hierarchy). members.profile_at, avatar_at and
// banner_at are versions for the apps' caches.

const (
	maxMemberBio      = 500
	maxMemberAvatar   = 1 << 20
	maxMemberBanner   = 2 << 20
	maxBackgroundSize = 4 << 20
)

var profileImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// handleUpdateMe: PATCH /v1/members/@me {nickname?, bio?, theme?}; an absent
// field is unchanged, "" clears the nickname or the bio, {} the theme.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname *string         `json:"nickname"`
		Bio      *string         `json:"bio"`
		Theme    json.RawMessage `json:"theme"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	m := memberFrom(r)
	var ps *permSnapshot
	automod := func(text string) error {
		if ps == nil {
			var err error
			if ps, err = s.loadPerms(ctx, s.db); err != nil {
				return err
			}
		}
		return s.checkAutoModName(ctx, ps, m.ID, 0, text)
	}
	sets, args := []string{}, []any{}
	if req.Nickname != nil {
		nick := sql.NullString{}
		if n := strings.TrimSpace(*req.Nickname); n != "" {
			if len([]rune(n)) > 32 {
				writeErr(w, r, errf(http.StatusBadRequest, "invalid_nickname", "nickname must be at most 32 characters"))
				return
			}
			if err := automod(n); err != nil {
				writeErr(w, r, err)
				return
			}
			nick = sql.NullString{String: n, Valid: true}
		}
		sets, args = append(sets, "nickname = ?"), append(args, nick)
	}
	profileChanged := false
	if req.Bio != nil {
		bio := strings.TrimSpace(*req.Bio)
		if len([]rune(bio)) > maxMemberBio {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_bio", "bio must be at most %d characters", maxMemberBio))
			return
		}
		if bio != "" {
			if err := automod(bio); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		sets, args, profileChanged = append(sets, "bio = ?"), append(args, bio), true
	}
	if req.Theme != nil {
		th, err := theme.Normalize(req.Theme)
		if err != nil {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_theme", "%s", strings.TrimPrefix(err.Error(), "invalid theme: ")))
			return
		}
		sets, args, profileChanged = append(sets, "theme = ?"), append(args, th), true
	}
	if profileChanged {
		sets, args = append(sets, "profile_at = ?"), append(args, s.now().UnixMilli())
	}
	if len(sets) > 0 {
		if _, err := s.db.ExecContext(ctx, `UPDATE members SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, m.ID)...); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	s.profileChanged(w, r, m.ID, true)
}

// profileChanged sends MEMBER_UPDATE and, if reply, the member's JSON.
func (s *Server) profileChanged(w http.ResponseWriter, r *http.Request, id string, reply bool) {
	m, err := memberBy(r.Context(), s.db, `id = ?`, id)
	if err == nil && m == nil {
		err = errf(http.StatusNotFound, "not_found", "no such member")
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := s.memberView(r.Context(), m)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !m.LeftAt.Valid {
		s.hub.Broadcast("MEMBER_UPDATE", view)
	}
	if reply {
		writeJSON(w, http.StatusOK, view)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMemberProfile: GET /v1/members/{id}/profile → {bio, theme, profile_v, avatar_v, banner_v}.
func (s *Server) handleMemberProfile(w http.ResponseWriter, r *http.Request) {
	var bio, th string
	var p, a, b int64
	err := s.db.QueryRowContext(r.Context(), `SELECT bio, theme, profile_at, avatar_at, banner_at FROM members WHERE id = ?`,
		r.PathValue("id")).Scan(&bio, &th, &p, &a, &b)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such member"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out := map[string]any{"bio": bio, "profile_v": p, "avatar_v": a, "banner_v": b}
	if th != "" {
		out["theme"] = json.RawMessage(th)
	}
	writeJSON(w, http.StatusOK, out)
}

var memberImageMax = map[string]int64{"avatar": maxMemberAvatar, "banner": maxMemberBanner}

// handleSetMemberImage: PUT /v1/members/@me/avatar|banner (image as body), DELETE to remove it.
func (s *Server) handleSetMemberImage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		m := memberFrom(r)
		if r.Method == http.MethodDelete {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM member_images WHERE member_id = ? AND kind = ?`, m.ID, kind); err != nil {
				writeErr(w, r, err)
				return
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE members SET `+kind+`_at = 0 WHERE id = ?`, m.ID); err != nil {
				writeErr(w, r, err)
				return
			}
			s.profileChanged(w, r, m.ID, false)
			return
		}
		data, ct, err := readImage(w, r, memberImageMax[kind])
		if err != nil {
			writeErr(w, r, err)
			return
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `INSERT INTO member_images (member_id, kind, content_type, data) VALUES (?, ?, ?, ?)
			ON CONFLICT (member_id, kind) DO UPDATE SET content_type = excluded.content_type, data = excluded.data`, m.ID, kind, ct, data); err != nil {
			writeErr(w, r, err)
			return
		}
		if _, err := tx.ExecContext(ctx, `UPDATE members SET `+kind+`_at = ? WHERE id = ?`, s.now().UnixMilli(), m.ID); err != nil {
			writeErr(w, r, err)
			return
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, r, err)
			return
		}
		s.profileChanged(w, r, m.ID, true)
	}
}

// readImage reads a PNG, JPEG, GIF or WebP image body of at most max bytes.
func readImage(w http.ResponseWriter, r *http.Request, max int64) ([]byte, string, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		return nil, "", errf(http.StatusRequestEntityTooLarge, "file_too_large", "images are limited to %d MB here", max>>20)
	}
	ct := http.DetectContentType(data)
	if !profileImageTypes[ct] {
		return nil, "", errf(http.StatusBadRequest, "invalid_image", "images must be PNG, JPEG, GIF or WebP")
	}
	return data, ct, nil
}

// handleMemberImage: GET /v1/members/{id}/avatar|banner (session).
func (s *Server) handleMemberImage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ct string
		var data []byte
		err := s.db.QueryRowContext(r.Context(), `SELECT content_type, data FROM member_images WHERE member_id = ? AND kind = ?`,
			r.PathValue("id"), kind).Scan(&ct, &data)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no %s", kind))
			return
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
		serveImage(w, ct, data)
	}
}

func serveImage(w http.ResponseWriter, ct string, data []byte) {
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "private, max-age=86400") // the apps ask with the version (?v=)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Write(data)
}

// handleResetProfile: DELETE /v1/members/{id}/profile — the moderation clears
// someone's profile on this server (bio, theme, avatar, banner).
func (s *Server) handleResetProfile(w http.ResponseWriter, r *http.Request) {
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
	ps, err := s.requirePerm(r, permModerateMembers)
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
	if _, err := tx.ExecContext(ctx, `UPDATE members SET bio = '', theme = '', profile_at = ?, avatar_at = 0, banner_at = 0 WHERE id = ?`,
		s.now().UnixMilli(), target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM member_images WHERE member_id = ?`, target.ID); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, tx, memberFrom(r).ID, auditProfileReset, target.ID, reason, nil)
	if err := tx.Commit(); err != nil {
		writeErr(w, r, err)
		return
	}
	s.profileChanged(w, r, target.ID, false)
}
