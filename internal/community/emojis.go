package community

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/anlekg/quarel/internal/diskspace"
	"github.com/anlekg/quarel/internal/secret"
)

// Custom emojis (P2): small images (PNG, GIF, WebP; maxEmojiBytes) named by
// the server's managers (manage_server), maxEmojis per server. Messages and
// reactions write them <:name:id>: clients show the image, or ":name:" once
// the emoji is gone. Images are served to members only (GET
// /v1/emojis/{id}), cached for good (an ID never changes image). Files live
// in data/emojis (backed up).

const (
	maxEmojis     = 100
	maxEmojiBytes = 256 << 10
	emojiDir      = "emojis"
)

var (
	emojiNameRe = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)
	emojiRefRe  = regexp.MustCompile(`^<:([a-z0-9_]{2,32}):([a-z0-9]{8,64})>$`)
)

type emojiJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) emojis(ctx context.Context) ([]emojiJSON, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM emojis ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []emojiJSON{}
	for rows.Next() {
		var e emojiJSON
		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (s *Server) emojisChanged(ctx context.Context) {
	if list, err := s.emojis(ctx); err == nil {
		s.hub.Broadcast("EMOJIS_UPDATE", list)
	}
}

func (s *Server) handleListEmojis(w http.ResponseWriter, r *http.Request) {
	list, err := s.emojis(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateEmoji: POST /v1/emojis?name=… with the image as the body.
func (s *Server) handleCreateEmoji(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !emojiNameRe.MatchString(name) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_name", "emoji names are 2-32 characters: a-z, 0-9 and _"))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEmojiBytes+1))
	if err != nil || len(data) > maxEmojiBytes {
		writeErr(w, r, errf(http.StatusRequestEntityTooLarge, "too_large", "emoji images are at most %d KB", maxEmojiBytes>>10))
		return
	}
	ct := http.DetectContentType(data)
	if ct != "image/png" && ct != "image/gif" && ct != "image/webp" {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_image", "emoji images are PNG, GIF or WebP"))
		return
	}
	ctx := r.Context()
	var n int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM emojis`).Scan(&n)
	if n >= maxEmojis {
		writeErr(w, r, errf(http.StatusBadRequest, "too_many_emojis", "at most %d emojis per server", maxEmojis))
		return
	}
	var taken int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM emojis WHERE name = ?`, name).Scan(&taken)
	if taken > 0 {
		writeErr(w, r, errf(http.StatusConflict, "name_taken", "an emoji already has this name"))
		return
	}
	if err := diskspace.Check(s.cfg.DataDir, s.cfg.MinFreeBytes, int64(len(data))); err != nil {
		writeErr(w, r, err)
		return
	}
	e := emojiJSON{ID: secret.NewID(), Name: name}
	dir := filepath.Join(s.cfg.DataDir, emojiDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, e.ID), data, 0o600); err != nil {
		writeErr(w, r, err)
		return
	}
	actor := memberFrom(r).ID
	if _, err := s.db.ExecContext(ctx, `INSERT INTO emojis (id, name, content_type, size, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.Name, ct, len(data), actor, s.nowMs()); err != nil {
		os.Remove(filepath.Join(dir, e.ID))
		writeErr(w, r, err)
		return
	}
	s.audit(ctx, s.db, actor, auditEmojiCreate, "", "", map[string]any{"name": name})
	s.emojisChanged(ctx)
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleDeleteEmoji(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM emojis WHERE id = ?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such emoji"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM emojis WHERE id = ?`, id); err != nil {
		writeErr(w, r, err)
		return
	}
	os.Remove(filepath.Join(s.cfg.DataDir, emojiDir, id))
	s.audit(ctx, s.db, memberFrom(r).ID, auditEmojiDelete, "", "", map[string]any{"name": name})
	s.emojisChanged(ctx)
	w.WriteHeader(http.StatusNoContent)
}

// handleEmojiImage: GET /v1/emojis/{id} (members).
func (s *Server) handleEmojiImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var ct string
	var size int64
	err := s.db.QueryRowContext(ctx, `SELECT content_type, size FROM emojis WHERE id = ?`, id).Scan(&ct, &size)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such emoji"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.DataDir, emojiDir, id))
	if err != nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such emoji"))
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	io.Copy(w, f)
}

// validReaction accepts a Unicode emoji or one of the server's own <:name:id>.
func (s *Server) validReaction(ctx context.Context, e string) bool {
	m := emojiRefRe.FindStringSubmatch(e)
	if m == nil {
		return validEmoji(e)
	}
	var n int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM emojis WHERE id = ? AND name = ?`, m[2], m[1]).Scan(&n)
	return n == 1
}
