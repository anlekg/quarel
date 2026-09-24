package identity

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// Operator tools (run as "quarel-identity admin …" on the host, never over
// the network): disabling accounts on legal order, and rotating the signing
// key. Every action is recorded in admin_log.

// --- disabled accounts, published to community servers ---

// handleDisabledAccounts: GET /v1/disabled-accounts, public. Community servers
// poll it to end the sessions of disabled accounts right away instead of
// waiting for their identity tokens to expire. Only random identifiers are
// published.
func (s *Server) handleDisabledAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, disabled_at FROM users WHERE disabled_at IS NOT NULL ORDER BY disabled_at`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	type entry struct {
		Subject string    `json:"sub"`
		Since   time.Time `json:"since"`
	}
	list := []entry{}
	for rows.Next() {
		var e entry
		var at int64
		if err := rows.Scan(&e.Subject, &at); err != nil {
			writeErr(w, r, err)
			return
		}
		e.Since = time.Unix(at, 0).UTC()
		list = append(list, e)
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, map[string]any{"issuer": s.cfg.Issuer, "accounts": list})
}

// WatchDisabled closes the live connections of accounts disabled by the
// admin tool (another process), until ctx ends.
func (s *Server) WatchDisabled(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE disabled_at IS NOT NULL`)
		if err != nil {
			continue
		}
		disabled := map[string]bool{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil && s.hub.GroupOnline(id) {
				disabled[id] = true
			}
		}
		rows.Close()
		if len(disabled) > 0 {
			s.hub.Disconnect(func(_, group string) bool { return disabled[group] }, "account disabled")
		}
	}
}

// Admin works directly on the service's database.
type Admin struct {
	db  *sql.DB
	now func() time.Time
}

// NewAdmin opens the tools on an Identity database.
func NewAdmin(db *sql.DB) *Admin { return &Admin{db: db, now: time.Now} }

// FindUser resolves an id, email or pseudo.
func (a *Admin) FindUser(ctx context.Context, who string) (id, pseudo string, disabled bool, err error) {
	who = strings.TrimSpace(who)
	var at sql.NullInt64
	err = a.db.QueryRowContext(ctx, `SELECT id, pseudo, disabled_at FROM users WHERE id = ? OR email = ? OR pseudo_norm = ?`,
		who, strings.ToLower(who), strings.ToLower(who)).Scan(&id, &pseudo, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, fmt.Errorf("aucun compte ne correspond à %q", who)
	}
	return id, pseudo, at.Valid, err
}

func (a *Admin) log(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, action, userID, reason, operator string) error {
	var uid any
	if userID != "" {
		uid = userID
	}
	_, err := q.ExecContext(ctx, `INSERT INTO admin_log (action, user_id, reason, operator, created_at) VALUES (?, ?, ?, ?, ?)`,
		action, uid, reason, operator, a.now().Unix())
	return err
}

// SetDisabled disables or re-enables an account. A reason and an operator
// name are mandatory: this is only done on legal order.
func (a *Admin) SetDisabled(ctx context.Context, who string, disabled bool, reason, operator string) (string, error) {
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(operator) == "" {
		return "", errors.New("la raison (référence de la réquisition) et le nom de l'opérateur sont obligatoires")
	}
	id, pseudo, already, err := a.FindUser(ctx, who)
	if err != nil {
		return "", err
	}
	if already == disabled {
		return pseudo, fmt.Errorf("le compte %s est déjà dans cet état", pseudo)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var at any
	action := "account_enable"
	if disabled {
		at, action = a.now().Unix(), "account_disable"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, at, id); err != nil {
		return "", err
	}
	if err := a.log(ctx, tx, action, id, reason, operator); err != nil {
		return "", err
	}
	return pseudo, tx.Commit()
}

// LogEntry is one line of the administration log.
type LogEntry struct {
	At                               time.Time
	Action, UserID, Reason, Operator string
}

// Log returns the most recent entries first.
func (a *Admin) Log(ctx context.Context, limit int) ([]LogEntry, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT created_at, action, COALESCE(user_id, ''), reason, operator FROM admin_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		var at int64
		if err := rows.Scan(&at, &e.Action, &e.UserID, &e.Reason, &e.Operator); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- signing key rotation ---

// retiredKey is a former signing key, published until the tokens it signed expire.
type retiredKey struct {
	X         string `json:"x"` // base64url Ed25519 public key
	RetiredAt int64  `json:"retired_at"`
}

const retiredKeysFile = "retired-keys.json"

func readRetired(dataDir string) ([]retiredKey, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, retiredKeysFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []retiredKey
	return keys, json.Unmarshal(data, &keys)
}

// liveRetiredKeys returns the retired keys whose tokens may still be valid.
func liveRetiredKeys(dataDir string, tokenTTL time.Duration, now time.Time) ([]ed25519.PublicKey, error) {
	keys, err := readRetired(dataDir)
	if err != nil {
		return nil, err
	}
	var out []ed25519.PublicKey
	for _, k := range keys {
		if now.Before(time.Unix(k.RetiredAt, 0).Add(tokenTTL + time.Hour)) {
			pub, err := idtoken.DecodeKey(k.X)
			if err != nil {
				return nil, err
			}
			out = append(out, pub)
		}
	}
	return out, nil
}

// RotateSigningKey replaces the signing key. The old public key keeps being
// published (for the lifetime of the tokens it signed); its private key is
// destroyed. The service must be restarted to use the new key.
func (a *Admin) RotateSigningKey(ctx context.Context, dataDir, reason, operator string) (oldKID, newKID string, err error) {
	path := filepath.Join(dataDir, "signing.key")
	old, err := secret.LoadOrCreateKey(path)
	if err != nil {
		return "", "", err
	}
	keys, err := readRetired(dataDir)
	if err != nil {
		return "", "", err
	}
	oldPub := old.Public().(ed25519.PublicKey)
	keys = append(keys, retiredKey{X: idtoken.EncodeKey(oldPub), RetiredAt: a.now().Unix()})
	data, _ := json.MarshalIndent(keys, "", "  ")
	tmp := filepath.Join(dataDir, retiredKeysFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, filepath.Join(dataDir, retiredKeysFile)); err != nil {
		return "", "", err
	}
	if err := os.Remove(path); err != nil {
		return "", "", err
	}
	fresh, err := secret.LoadOrCreateKey(path)
	if err != nil {
		return "", "", err
	}
	oldKID, newKID = idtoken.KeyID(oldPub), idtoken.KeyID(fresh.Public().(ed25519.PublicKey))
	if err := a.log(ctx, a.db, "signing_key_rotate", "", reason+" ("+oldKID+" → "+newKID+")", operator); err != nil {
		slog.Error("admin log", "err", err)
	}
	return oldKID, newKID, nil
}
