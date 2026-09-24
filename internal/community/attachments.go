package community

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/anlekg/quarel/internal/secret"
)

// Attachments are uploaded first (POST /v1/channels/{id}/attachments), then
// referenced by the message that carries them. Files live on disk under
// data/attachments/<id>; the database keeps their metadata. Uploads never
// attached to a message are removed after an hour.

const (
	maxAttachmentsPerMessage = 10
	pendingUploadTTL         = time.Hour
)

type attachmentJSON struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"` // relative to the server; needs the session token
}

func attachmentURL(id, name string) string {
	return "/v1/attachments/" + id + "/" + url.PathEscape(name)
}

func (s *Server) attachmentsDir() string { return filepath.Join(s.cfg.DataDir, "attachments") }

// cleanFilename keeps a safe, readable file name.
func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if rs := []rune(name); len(rs) > 120 {
		ext := filepath.Ext(name)
		name = string(rs[:120-len([]rune(ext))]) + ext
	}
	if name == "" {
		name = "fichier"
	}
	return name
}

// handleUpload stores a file sent as multipart/form-data (field "file").
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	c, ps, err := s.textChannel(r, 0)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	me := memberFrom(r).ID
	if err := requirePost(ps, me, c, permAttachFiles); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.uploads.Check(me); err != nil {
		writeErr(w, r, err)
		return
	}
	limit := s.cfg.MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, r, errf(http.StatusBadRequest, "bad_request", "expected multipart/form-data with a \"file\" field"))
		return
	}
	var part interface {
		io.Reader
		FileName() string
		FormName() string
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			writeErr(w, r, errf(http.StatusBadRequest, "bad_request", "no \"file\" field"))
			return
		}
		if p.FormName() == "file" {
			part = p
			break
		}
	}
	if err := os.MkdirAll(s.attachmentsDir(), 0o700); err != nil {
		writeErr(w, r, err)
		return
	}
	id := secret.NewID()
	path := filepath.Join(s.attachmentsDir(), id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(part, head)
	head = head[:n]
	size, err := io.Copy(f, io.MultiReader(bytes.NewReader(head), io.LimitReader(part, limit+1-int64(n))))
	f.Close()
	if err == nil && size > limit {
		err = errf(http.StatusRequestEntityTooLarge, "file_too_large", "files are limited to %d MB on this server", limit>>20)
	}
	if err == nil && size == 0 {
		err = errf(http.StatusBadRequest, "empty_file", "the file is empty")
	}
	if err != nil {
		os.Remove(path)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			err = errf(http.StatusRequestEntityTooLarge, "file_too_large", "files are limited to %d MB on this server", limit>>20)
		}
		writeErr(w, r, err)
		return
	}
	a := attachmentJSON{ID: id, Filename: cleanFilename(part.FileName()), ContentType: sniff(head), Size: size}
	a.URL = attachmentURL(a.ID, a.Filename)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO attachments (id, channel_id, uploader_id, filename, content_type, size, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, c.ID, me, a.Filename, a.ContentType, a.Size, s.nowMs()); err != nil {
		os.Remove(path)
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// sniff detects the content type from the bytes, never from the client's claim.
func sniff(head []byte) string {
	ct := http.DetectContentType(head)
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		return mt
	}
	return "application/octet-stream"
}

// inlineTypes may be displayed by browsers; everything else is downloaded.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wave": true, "video/mp4": true, "video/webm": true,
	"text/plain": true,
}

// handleDownload serves an attachment to members who can see its channel.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var a attachmentJSON
	var channelID int64
	var uploader string
	var messageID *int64
	err := s.db.QueryRowContext(ctx, `SELECT id, filename, content_type, size, channel_id, uploader_id, message_id FROM attachments WHERE id = ?`,
		r.PathValue("id")).Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &channelID, &uploader, &messageID)
	notFound := errf(http.StatusNotFound, "not_found", "no such file")
	if err != nil {
		writeErr(w, r, notFound)
		return
	}
	me := memberFrom(r).ID
	if messageID == nil && uploader != me {
		writeErr(w, r, notFound) // not sent yet: only its uploader sees it
		return
	}
	c, err := channelByID(ctx, s.db, channelID)
	if err != nil {
		writeErr(w, r, notFound)
		return
	}
	if _, err := s.requireChannelPerm(r, c, 0); err != nil {
		writeErr(w, r, notFound)
		return
	}
	f, err := os.Open(filepath.Join(s.attachmentsDir(), a.ID))
	if err != nil {
		writeErr(w, r, notFound)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	disposition := "attachment"
	if inlineTypes[a.ContentType] {
		disposition = "inline"
		ct := a.ContentType
		if ct == "text/plain" {
			ct += "; charset=utf-8"
		}
		h.Set("Content-Type", ct)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *Server) attachmentIDs(ctx context.Context, where string, args ...any) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM attachments WHERE `+where, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Server) removeFiles(ids []string) {
	for _, id := range ids {
		if err := os.Remove(filepath.Join(s.attachmentsDir(), id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("removing attachment file", "id", id, "err", err)
		}
	}
}

// CleanupAttachments deletes uploads never sent and files whose record is
// gone (e.g. their channel was deleted).
func (s *Server) CleanupAttachments(ctx context.Context) error {
	stale := s.attachmentIDs(ctx, `message_id IS NULL AND created_at < ?`, s.now().Add(-pendingUploadTTL).UnixMilli())
	if len(stale) > 0 {
		args := make([]any, len(stale))
		for i, id := range stale {
			args[i] = id
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE id IN (?`+strings.Repeat(",?", len(stale)-1)+`)`, args...); err != nil {
			return err
		}
		s.removeFiles(stale)
	}
	entries, err := os.ReadDir(s.attachmentsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, id := range s.attachmentIDs(ctx, `1`) {
		known[id] = true
	}
	var orphans []string
	for _, e := range entries {
		if !known[e.Name()] {
			orphans = append(orphans, e.Name())
		}
	}
	s.removeFiles(orphans)
	if len(stale)+len(orphans) > 0 {
		slog.Info("attachments cleaned up", "unsent", len(stale), "orphans", len(orphans))
	}
	return nil
}
