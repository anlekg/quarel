package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/theme"
)

// Public profiles (pseudo, bio, avatar, banner, theme of the card), blocking,
// and presence between friends.

const (
	maxBioLength    = 500
	maxAvatarBytes  = 1 << 20
	maxBannerBytes  = 2 << 20
	presenceOffline = "offline"
)

var presenceStates = map[string]bool{"online": true, "idle": true, "dnd": true, "invisible": true}

type profileJSON struct {
	ID        string          `json:"id"`
	Handle    string          `json:"handle"`
	Pseudo    string          `json:"pseudo"`
	Bio       string          `json:"bio"`
	AvatarURL *string         `json:"avatar_url"` // relative to the Identity service; changes when the avatar does
	BannerURL *string         `json:"banner_url"` // same
	Theme     json.RawMessage `json:"theme,omitempty"`
}

func (s *Server) profile(ctx context.Context, u *user) (profileJSON, error) {
	p := profileJSON{ID: u.ID, Handle: s.handle(u), Pseudo: u.Pseudo}
	var avatar, banner sql.NullInt64
	var th string
	err := s.db.QueryRowContext(ctx, `SELECT u.bio, u.profile_theme, a.updated_at, b.updated_at FROM users u
		LEFT JOIN avatars a ON a.user_id = u.id LEFT JOIN banners b ON b.user_id = u.id WHERE u.id = ?`, u.ID).
		Scan(&p.Bio, &th, &avatar, &banner)
	if avatar.Valid {
		url := fmt.Sprintf("/v1/users/%s/avatar?v=%d", u.ID, avatar.Int64)
		p.AvatarURL = &url
	}
	if banner.Valid {
		url := fmt.Sprintf("/v1/users/%s/banner?v=%d", u.ID, banner.Int64)
		p.BannerURL = &url
	}
	if th != "" {
		p.Theme = json.RawMessage(th)
	}
	return p, err
}

// handleProfile: GET /v1/users/{id}/profile, public (community servers' clients
// show the avatars of members who are not their friends). Disabled accounts
// have no public profile.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	u, err := s.userBy(r.Context(), "id", r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if u == nil || u.DisabledAt.Valid || !u.EmailVerifiedAt.Valid {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such user"))
		return
	}
	p, err := s.profile(r.Context(), u)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, p)
}

// handleUpdateProfile: PATCH /v1/me/profile {bio?, theme?} (theme absent or null: unchanged; {}: removed).
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bio   *string         `json:"bio"`
		Theme json.RawMessage `json:"theme"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if req.Bio != nil {
		bio := strings.TrimSpace(*req.Bio)
		if len([]rune(bio)) > maxBioLength {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_bio", "bio must be at most %d characters", maxBioLength))
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET bio = ? WHERE id = ?`, bio, u.ID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if req.Theme != nil {
		th, err := theme.Normalize(req.Theme)
		if err != nil {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_theme", "%s", strings.TrimPrefix(err.Error(), "invalid theme: ")))
			return
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET profile_theme = ? WHERE id = ?`, th, u.ID); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	s.userChanged(ctx, u)
	p, err := s.profile(ctx, u)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// profileImages: the avatar and the banner, each in its own table (same columns).
var profileImages = map[string]struct {
	table string
	max   int64
}{"avatar": {"avatars", maxAvatarBytes}, "banner": {"banners", maxBannerBytes}}

// handleSetImage: PUT /v1/me/avatar or /v1/me/banner with the image as body
// (PNG, JPEG, GIF or WebP; avatars 1 MB, banners 2 MB).
func (s *Server) handleSetImage(kind string) http.HandlerFunc {
	img := profileImages[kind]
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, img.max))
		if err != nil {
			writeErr(w, r, errf(http.StatusRequestEntityTooLarge, "file_too_large", "%ss are limited to %d MB", kind, img.max>>20))
			return
		}
		ct := http.DetectContentType(data)
		if !imageTypes[ct] {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_image", "%ss must be PNG, JPEG, GIF or WebP images", kind))
			return
		}
		u, err := s.currentUser(r)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		ctx := r.Context()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO `+img.table+` (user_id, content_type, data, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (user_id) DO UPDATE SET content_type = excluded.content_type, data = excluded.data, updated_at = excluded.updated_at`,
			u.ID, ct, data, s.now().UnixNano()); err != nil {
			writeErr(w, r, err)
			return
		}
		s.userChanged(ctx, u)
		p, err := s.profile(ctx, u)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

func (s *Server) handleDeleteImage(kind string) http.HandlerFunc {
	table := profileImages[kind].table
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.currentUser(r)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if _, err := s.db.ExecContext(r.Context(), `DELETE FROM `+table+` WHERE user_id = ?`, u.ID); err != nil {
			writeErr(w, r, err)
			return
		}
		s.userChanged(r.Context(), u)
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleImage: GET /v1/users/{id}/avatar or /banner, public.
func (s *Server) handleImage(kind string) http.HandlerFunc {
	table := profileImages[kind].table
	return func(w http.ResponseWriter, r *http.Request) {
		var ct string
		var data []byte
		err := s.db.QueryRowContext(r.Context(), `SELECT a.content_type, a.data FROM `+table+` a JOIN users u ON u.id = a.user_id
			WHERE a.user_id = ? AND u.disabled_at IS NULL`, r.PathValue("id")).Scan(&ct, &data)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, r, errf(http.StatusNotFound, "not_found", "no %s", kind))
			return
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", ct)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Cache-Control", "public, max-age=86400") // the URL carries a version
		w.Write(data)
	}
}

// --- blocking ---

// blocked reports whether either user blocked the other.
func (s *Server) blocked(ctx context.Context, a, b string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blocks WHERE (user_id = ? AND blocked_id = ?) OR (user_id = ? AND blocked_id = ?)`,
		a, b, b, a).Scan(&n)
	return n > 0, err
}

// handleBlock: PUT /v1/blocks/{id}, or POST /v1/blocks {pseudo}. Blocking ends
// any friendship or request, and the blocked user can no longer send requests.
// Hiding their messages on community servers is up to the client (GET /v1/blocks).
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me, err := s.currentUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var other *user
	if id := r.PathValue("id"); id != "" {
		other, err = s.userBy(ctx, "id", id)
	} else {
		var req struct{ Pseudo string }
		if err := decode(r, &req); err != nil {
			writeErr(w, r, err)
			return
		}
		other, err = s.userBy(ctx, "pseudo_norm", strings.ToLower(strings.TrimSpace(req.Pseudo)))
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if other == nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such user"))
		return
	}
	if other.ID == me.ID {
		writeErr(w, r, errf(http.StatusBadRequest, "self_block", "you cannot block yourself"))
		return
	}
	a, b := pair(me.ID, other.ID)
	res, err := s.db.ExecContext(ctx, `DELETE FROM friendships WHERE user_a = ? AND user_b = ?`, a, b)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO blocks (user_id, blocked_id, created_at) VALUES (?, ?, ?)`,
		me.ID, other.ID, s.now().Unix()); err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.friendsChanged(ctx, me, other)
	}
	writeJSON(w, http.StatusOK, s.publicUser(other))
}

func (s *Server) handleUnblock(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM blocks WHERE user_id = ? AND blocked_id = ?`, sessionFrom(r).UserID, r.PathValue("id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "this user is not blocked"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListBlocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT u.id, u.pseudo FROM blocks b JOIN users u ON u.id = b.blocked_id
		WHERE b.user_id = ? ORDER BY b.created_at`, sessionFrom(r).UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []publicUser{}
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Pseudo); err != nil {
			writeErr(w, r, err)
			return
		}
		list = append(list, s.publicUser(&u))
	}
	writeJSON(w, http.StatusOK, list)
}

// --- presence ---

// presenceOf is what friends see: offline without any connected device, or
// when invisible; otherwise the chosen status.
func (s *Server) presenceOf(ctx context.Context, userID string) string {
	var setting string
	if err := s.db.QueryRowContext(ctx, `SELECT presence FROM users WHERE id = ?`, userID).Scan(&setting); err != nil {
		return presenceOffline
	}
	if setting == "invisible" || !s.hub.GroupOnline(userID) {
		return presenceOffline
	}
	return setting
}

// presenceChanged tells the user's friends (and their own devices) their presence.
func (s *Server) presenceChanged(ctx context.Context, userID string) {
	friends, err := s.friendIDs(ctx, userID)
	if err != nil {
		return
	}
	status := s.presenceOf(ctx, userID)
	s.hub.BroadcastTo("PRESENCE_UPDATE", map[string]string{"user_id": userID, "status": status},
		func(_, group string) bool { return friends[group] })
}

// handleSetPresence: PUT /v1/me/presence {status: online|idle|dnd|invisible}.
func (s *Server) handleSetPresence(w http.ResponseWriter, r *http.Request) {
	var req struct{ Status string }
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if !presenceStates[req.Status] {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_status", "status must be online, idle, dnd or invisible"))
		return
	}
	me := sessionFrom(r).UserID
	if _, err := s.db.ExecContext(r.Context(), `UPDATE users SET presence = ? WHERE id = ?`, req.Status, me); err != nil {
		writeErr(w, r, err)
		return
	}
	s.presenceChanged(r.Context(), me)
	s.hub.BroadcastTo("PRESENCE_SETTING", map[string]string{"status": req.Status}, func(_, group string) bool { return group == me })
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// presenceHook is called by the gateway hub when a user's first device
// connects or their last one disconnects.
func (s *Server) presenceHook(userID string, _ bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.presenceChanged(ctx, userID)
}
