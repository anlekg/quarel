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
	"slices"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/diskspace"
)

// Typing indicators and read receipts (live only, never stored, each can be
// turned off), and encrypted files of conversations.

// privacy returns the user's sharing settings.
func (s *Server) privacy(ctx context.Context, userID string) (typing, receipts bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT share_typing, share_read_receipts FROM users WHERE id = ?`, userID).Scan(&typing, &receipts)
	return
}

// Who may send a friend request (users.friend_requests): anyone; only the
// friends of my friends and the members of my conversations; nobody.
var friendRequestModes = map[string]bool{"everyone": true, "friends_of_friends": true, "nobody": true}

// handlePrivacy: GET/PATCH /v1/me/privacy {typing?, read_receipts?, friend_requests?}.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := sessionFrom(r).UserID
	if r.Method == http.MethodPatch {
		var req struct {
			Typing         *bool   `json:"typing"`
			ReadReceipts   *bool   `json:"read_receipts"`
			FriendRequests *string `json:"friend_requests"`
		}
		if err := decode(r, &req); err != nil {
			writeErr(w, r, err)
			return
		}
		if req.FriendRequests != nil {
			if !friendRequestModes[*req.FriendRequests] {
				writeErr(w, r, errf(http.StatusBadRequest, "bad_request", "friend_requests: everyone, friends_of_friends or nobody"))
				return
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE users SET friend_requests = ? WHERE id = ?`, *req.FriendRequests, me); err != nil {
				writeErr(w, r, err)
				return
			}
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
	var requests string
	if err == nil {
		err = s.db.QueryRowContext(ctx, `SELECT friend_requests FROM users WHERE id = ?`, me).Scan(&requests)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"typing": typing, "read_receipts": receipts, "friend_requests": requests})
}

// mayRequest says whether from may send a friend request to to, by to's setting.
func (s *Server) mayRequest(ctx context.Context, from, to string) (bool, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, `SELECT friend_requests FROM users WHERE id = ?`, to).Scan(&mode); err != nil {
		return false, err
	}
	switch mode {
	case "nobody":
		return false, nil
	case "friends_of_friends":
		var n int
		err := s.db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM friendships f1 JOIN friendships f2
          ON (CASE WHEN f1.user_a = ?1 THEN f1.user_b ELSE f1.user_a END) = (CASE WHEN f2.user_a = ?2 THEN f2.user_b ELSE f2.user_a END)
        WHERE f1.status = 'accepted' AND f2.status = 'accepted' AND ?1 IN (f1.user_a, f1.user_b) AND ?2 IN (f2.user_a, f2.user_b))
     + (SELECT COUNT(*) FROM conversation_members m1 JOIN conversation_members m2 ON m1.conversation_id = m2.conversation_id
        WHERE m1.user_id = ?1 AND m2.user_id = ?2)`, from, to).Scan(&n)
		return n > 0, err
	}
	return true, nil
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

// handleUploadFile: POST /v1/dms/{id}/files?for=<device ids>, body = ciphertext
// (the key travels inside the encrypted message that references the file).
// The copy is kept for the listed devices only (default: every device of the
// conversation but the uploader's) and deleted once each has acknowledged it.
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(r)
	convID := r.PathValue("id")
	members, err := s.convMembers(ctx, s.db, convID, sess.UserID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if err := s.limit.files.Check(sess.UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	devices, err := s.convDevices(ctx, members)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var pending []string
	if list := r.URL.Query().Get("for"); list != "" {
		for _, d := range strings.Split(list, ",") {
			if !devices[d] {
				writeErr(w, r, errf(http.StatusBadRequest, "invalid_device", "device %q is not in this conversation", d))
				return
			}
			if d != sess.ID && !slices.Contains(pending, d) { // the sending device has the file
				pending = append(pending, d)
			}
		}
	} else {
		for d := range devices {
			if d != sess.ID {
				pending = append(pending, d)
			}
		}
	}
	if len(pending) == 0 {
		writeErr(w, r, errf(http.StatusBadRequest, "no_recipients", "no device needs this file"))
		return
	}
	// Server copies are bounded per user and by the disk's free space.
	if err := s.fileRoom(ctx, sess.UserID, r.ContentLength); err != nil {
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
	if err == nil && r.ContentLength < 0 { // size unknown in advance: checked now
		err = s.fileRoom(ctx, sess.UserID, size)
	}
	if err != nil {
		os.Remove(path)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			err = errf(http.StatusRequestEntityTooLarge, "file_too_large", "files kept by the server are limited to %d MB (larger ones go peer to peer)", s.cfg.DMFileMaxBytes>>20)
		}
		writeErr(w, r, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		os.Remove(path)
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO conv_attachments (id, conversation_id, uploader_id, size, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, convID, sess.UserID, size, s.now().Unix()); err != nil {
		os.Remove(path)
		writeErr(w, r, err)
		return
	}
	for _, d := range pending {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO conv_file_pending (file_id, session_id) VALUES (?, ?)`, id, d); err != nil {
			os.Remove(path)
			writeErr(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		os.Remove(path)
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "size": size, "pending_devices": len(pending),
		"expires_at": s.now().Add(s.cfg.DMFileTTL).UTC().Truncate(time.Second)})
}

// convDevices returns the devices (with keys) of the given users.
func (s *Server) convDevices(ctx context.Context, members []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(members) == 0 {
		return out, nil
	}
	args := make([]any, len(members))
	for i, m := range members {
		args[i] = m
	}
	rows, err := s.db.QueryContext(ctx, `SELECT session_id FROM device_keys WHERE user_id IN (?`+strings.Repeat(",?", len(members)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// handleAckFile: POST /v1/dms/{id}/files/{file}/ack — this device has the
// file; once no device is waiting for it, the server copy is deleted.
func (s *Server) handleAckFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.convFile(r); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	file := r.PathValue("file")
	s.db.ExecContext(ctx, `DELETE FROM conv_file_pending WHERE file_id = ? AND session_id = ?`, file, sessionFrom(r).ID)
	var left int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conv_file_pending WHERE file_id = ?`, file).Scan(&left)
	if left == 0 {
		s.db.ExecContext(ctx, `DELETE FROM conv_attachments WHERE id = ?`, file)
		os.Remove(filepath.Join(s.filesDir(), file))
	}
	writeJSON(w, http.StatusOK, map[string]int{"pending_devices": left})
}

func (s *Server) convFile(r *http.Request) (uploader string, err error) {
	ctx := r.Context()
	if _, err := s.convMembers(ctx, s.db, r.PathValue("id"), sessionFrom(r).UserID); err != nil {
		return "", err
	}
	err = s.db.QueryRowContext(ctx, `SELECT uploader_id FROM conv_attachments WHERE id = ? AND conversation_id = ?`,
		r.PathValue("file"), r.PathValue("id")).Scan(&uploader)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(http.StatusNotFound, "not_found", "no such file on the server (it is deleted once every device has it: ask a device that holds it)")
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

// fileRoom checks that size more bytes of server copies fit in the user's
// quota and on the disk.
func (s *Server) fileRoom(ctx context.Context, userID string, size int64) error {
	if size < 0 {
		size = 0
	}
	if s.cfg.DMFileQuota > 0 {
		var used int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM conv_attachments WHERE uploader_id = ?`, userID).Scan(&used); err != nil {
			return err
		}
		if used+size > s.cfg.DMFileQuota {
			return errf(http.StatusInsufficientStorage, "file_quota_exceeded",
				"your files waiting on the server already take %d MB (limit %d MB): they go once received, or after %s", used>>20, s.cfg.DMFileQuota>>20, s.cfg.DMFileTTL)
		}
	}
	return diskspace.Check(s.cfg.DataDir, s.cfg.MinFreeBytes, size)
}

// CleanupFiles deletes expired files and files whose conversation is gone.
func (s *Server) CleanupFiles(ctx context.Context) error {
	// Expired copies, and copies no device waits for any more (revoked devices).
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conv_attachments WHERE created_at < ?
		OR NOT EXISTS (SELECT 1 FROM conv_file_pending p WHERE p.file_id = conv_attachments.id)`, s.now().Add(-s.cfg.DMFileTTL).Unix()); err != nil {
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
