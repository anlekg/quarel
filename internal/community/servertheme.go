package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/anlekg/quarel/internal/theme"
)

// The server's theme (cosmetics block): colours, a gradient, a font of the
// app and filtered CSS (internal/theme), plus a background image. The apps
// apply it to the server's area only (channel list, messages, members,
// voice), and filter the CSS again. Set with manage_server; THEME_UPDATE
// {theme, background_v} goes to everyone, and READY carries it.

const themeSettingsKey = "theme"

type serverTheme struct {
	Theme       json.RawMessage `json:"theme"`        // null: none
	BackgroundV int64           `json:"background_v"` // 0: no background image
}

func (s *Server) serverTheme(ctx context.Context) (serverTheme, error) {
	var t serverTheme
	raw, err := s.setting(ctx, themeSettingsKey)
	if err != nil {
		return t, err
	}
	if raw != "" {
		t.Theme = json.RawMessage(raw)
	}
	err = s.db.QueryRowContext(ctx, `SELECT updated_at FROM server_images WHERE kind = 'background'`).Scan(&t.BackgroundV)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return t, err
}

func (s *Server) themeChanged(w http.ResponseWriter, r *http.Request) {
	t, err := s.serverTheme(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.Broadcast("THEME_UPDATE", t)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	t, err := s.serverTheme(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleSetTheme: PUT /v1/server/theme {theme} ({} or null removes it).
func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Theme json.RawMessage `json:"theme"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	th, err := theme.Normalize(req.Theme)
	if err != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_theme", "%s", strings.TrimPrefix(err.Error(), "invalid theme: ")))
		return
	}
	ctx := r.Context()
	if err := setSetting(ctx, s.db, themeSettingsKey, th); err != nil {
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, s.db, memberFrom(r).ID, auditServerTheme, "", "", nil)
	s.themeChanged(w, r)
}

// handleSetBackground: PUT /v1/server/theme/background (image as body), DELETE to remove it.
func (s *Server) handleSetBackground(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodDelete {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM server_images WHERE kind = 'background'`); err != nil {
			writeErr(w, r, err)
			return
		}
	} else {
		data, ct, err := readImage(w, r, maxBackgroundSize)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO server_images (kind, content_type, data, updated_at) VALUES ('background', ?, ?, ?)
			ON CONFLICT (kind) DO UPDATE SET content_type = excluded.content_type, data = excluded.data, updated_at = excluded.updated_at`,
			ct, data, s.now().UnixMilli()); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	s.audit(ctx, s.db, memberFrom(r).ID, auditServerTheme, "", "", map[string]any{"background": r.Method != http.MethodDelete})
	s.themeChanged(w, r)
}

// handleBackground: GET /v1/server/theme/background (session).
func (s *Server) handleBackground(w http.ResponseWriter, r *http.Request) {
	var ct string
	var data []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT content_type, data FROM server_images WHERE kind = 'background'`).Scan(&ct, &data)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no background image"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	serveImage(w, ct, data)
}
