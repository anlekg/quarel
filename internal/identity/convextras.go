package identity

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Typing indicators and read receipts (live only, never stored, each can be
// turned off), and encrypted files of conversations.

// privacy returns the user's sharing settings.
func (s *Server) privacy(ctx context.Context, userID string) (typing, receipts bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT share_typing, share_read_receipts FROM users WHERE id = ?`, userID).Scan(&typing, &receipts)
	return
}

// handlePrivacy: GET/PATCH /v1/me/privacy {typing?, read_receipts?}.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := sessionFrom(r).UserID
	if r.Method == http.MethodPatch {
		var req struct {
			Typing       *bool `json:"typing"`
			ReadReceipts *bool `json:"read_receipts"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, r, err)
			return
		}
		if req.Typing != nil {
			if _, err := s.db.ExecContext(ctx, `UPDATE users SET share_typing = ? WHERE id = ?`, *req.Typing, me); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		if req.ReadReceipts != nil {
			if _, err := s.db.ExecContext(ctx, `UPDATE users SET share_read_receipts = ? WHERE id = ?`, *req.ReadReceipts, me); err != nil {
				writeErr(w, r, err)
				return
			}
		}
	}
	typing, receipts, err := s.privacy(ctx, me)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"typing": typing, "read_receipts": receipts})
}

// broadcastConv sends an ephemeral event to the connected devices of a
// conversation's members (except the sender's own device).
func (s *Server) broadcastConv(members []string, t string, d any, exceptDevice string) {
	in := map[string]bool{}
	for _, id := range members {
		in[id] = true
	}
	s.hub.BroadcastTo(t, d, func(key, group string) bool { return in[group] && key != exceptDevice })
}

// handleTyping: POST /v1/dms/{id}/typing → DM_TYPING {dm_id, user_id} (≤ 1 per 3 s).
func (s *Server) handleTyping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(r)
	members, err := s.convMembers(ctx, s.db, r.PathValue("id"), sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	typing, _, err := s.privacy(ctx, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if ok, _ := s.limit.typing.Allow(sess.UserID + "|" + r.PathValue("id")); typing && ok {
		s.broadcastConv(members, "DM_TYPING", map[string]string{"dm_id": r.PathValue("id"), "user_id": sess.UserID}, sess.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRead: POST /v1/dms/{id}/read {event_id} → DM_READ {dm_id, user_id,
// event_id} to the members' connected devices (the reader's other devices
// included, to sync what was read). Nothing is stored; with read receipts
// off, only the reader's own devices are told.
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventID int64 `json:"event_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	convID := r.PathValue("id")
	members, err := s.convMembers(ctx, s.db, convID, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	_, receipts, err := s.privacy(ctx, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !receipts {
		members = []string{sess.UserID}
	}
	s.broadcastConv(members, "DM_READ", map[string]any{"dm_id": convID, "user_id": sess.UserID, "event_id": req.EventID}, sess.ID)
	w.WriteHeader(http.StatusNoContent)
}

// --- encrypted files ---

func (s *Server) filesDir() string { return filepath.Join(s.cfg.DataDir, "dm-files") }

// handleUploadFile: POST /v1/dms/{id}/files, body = ciphertext (the key
// travels inside the encrypted message that references the file).
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(r)
	convID := r.PathValue("id")
	if _, err := s.convMembers(ctx, s.db, convID, sess.UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.files.Check(sess.UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := os.MkdirAll(s.filesDir(), 0o700); err != nil {
		writeErr(w, r, err)
		return
	}
	id := newID()
	path := filepath.Join(s.filesDir(), id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	size, err := io.Copy(f, http.MaxBytesReader(w, r.Body, s.cfg.DMFileMaxBytes))
	f.Close()
	if err == nil && size == 0 {
		err = errf(http.StatusBadRequest, "empty_file", "the file is empty")
	}
	if err != nil {
		os.Remove(path)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			err = errf(http.StatusRequestEntityTooLarge, "file_too_large", "files are limited to %d MB", s.cfg.DMFileMaxBytes>>20)
		}
		writeErr(w, r, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO conv_attachments (id, conversation_id, uploader_id, size, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, convID, sess.UserID, size, s.now().Unix()); err != nil {
		os.Remove(path)
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "size": size,
		"expires_at": s.now().Add(s.cfg.DMFileTTL).UTC().Truncate(time.Second)})
}

func (s *Server) convFile(r *http.Request) (uploader string, err error) {
	ctx := r.Context()
	if _, err := s.convMembers(ctx, s.db, r.PathValue("id"), sessionFrom(r).UserID); err != nil {
		return "", err
	}
	err = s.db.QueryRowContext(ctx, `SELECT uploader_id FROM conv_attachments WHERE id = ? AND conversation_id = ?`,
		r.PathValue("file"), r.PathValue("id")).Scan(&uploader)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(http.StatusNotFound, "not_found", "no such file (files expire after a while: keep them on your devices)")
	}
	return uploader, err
}

// handleDownloadFile: GET /v1/dms/{id}/files/{file}, members only.
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.convFile(r); err != nil {
		writeErr(w, r, err)
		return
	}
	f, err := os.Open(filepath.Join(s.filesDir(), r.PathValue("file")))
	if err != nil {
		writeErr(w, r, errf(http.StatusNotFound, "not_found", "no such file"))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// handleDeleteFile: DELETE /v1/dms/{id}/files/{file}, by its uploader.
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	uploader, err := s.convFile(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if uploader != sessionFrom(r).UserID {
		writeErr(w, r, errf(http.StatusForbidden, "forbidden", "only the sender can delete a file"))
		return
	}
	s.db.ExecContext(r.Context(), `DELETE FROM conv_attachments WHERE id = ?`, r.PathValue("file"))
	os.Remove(filepath.Join(s.filesDir(), r.PathValue("file")))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeConvFiles(ctx context.Context, convID string) {
	// The rows went with the conversation (cascade); the files are orphans now.
	s.CleanupFiles(ctx)
}

// CleanupFiles deletes expired files and files whose conversation is gone.
func (s *Server) CleanupFiles(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conv_attachments WHERE created_at < ?`, s.now().Add(-s.cfg.DMFileTTL).Unix()); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.filesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	known := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM conv_attachments`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			known[id] = true
		}
	}
	rows.Close()
	removed := 0
	for _, e := range entries {
		if !known[e.Name()] {
			if os.Remove(filepath.Join(s.filesDir(), e.Name())) == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		slog.Info("conversation files cleaned up", "removed", removed)
	}
	return nil
}
