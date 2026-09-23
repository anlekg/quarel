package identity

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
)

// Encrypted account backup (see pkg/recovery): the client encrypts its master
// key and message history with a key derived from the recovery phrase; the
// server keeps the ciphertext only. Versions prevent two devices from
// overwriting each other's backups: a write must name the version it replaces.

const maxBackupBody = 16 << 20

func (s *Server) handleGetBackup(w http.ResponseWriter, r *http.Request) {
	var version, updated int64
	var data string
	err := s.db.QueryRowContext(r.Context(), `SELECT version, data, updated_at FROM backups WHERE user_id = ?`, sessionFrom(r).UserID).
		Scan(&version, &data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, r, errf(http.StatusNotFound, "no_backup", "no backup for this account"))
		return
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "data": data, "updated_at": time.Unix(updated, 0).UTC()})
}

func (s *Server) handlePutBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int64  `json:"version"` // version being replaced; 0 when creating
		Data    string `json:"data"`
	}
	if err := httpapi.DecodeLimit(r, &req, maxBackupBody); err != nil {
		writeErr(w, r, err)
		return
	}
	if req.Data == "" {
		writeErr(w, r, errf(http.StatusBadRequest, "empty_backup", "data is required"))
		return
	}
	ctx := r.Context()
	me := sessionFrom(r).UserID
	now := s.now().Unix()
	var res sql.Result
	var err error
	if req.Version == 0 {
		res, err = s.db.ExecContext(ctx, `INSERT INTO backups (user_id, version, data, updated_at) VALUES (?, 1, ?, ?) ON CONFLICT (user_id) DO NOTHING`, me, req.Data, now)
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE backups SET version = version + 1, data = ?, updated_at = ? WHERE user_id = ? AND version = ?`, req.Data, now, me, req.Version)
	}
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var current int64
		s.db.QueryRowContext(ctx, `SELECT version FROM backups WHERE user_id = ?`, me).Scan(&current)
		writeErr(w, r, errf(http.StatusConflict, "version_conflict", "the backup is at version %d: download it, merge, and retry", current))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"version": req.Version + 1})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM backups WHERE user_id = ?`, sessionFrom(r).UserID); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
