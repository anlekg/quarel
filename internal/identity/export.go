package identity

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"time"
)

// handleExport: GET /v1/me/export, everything this service keeps about the
// account (right of access and portability): profile and settings, sessions,
// device keys, friends, blocks, conversations (not their messages: they are
// end-to-end encrypted, only on the devices — the app adds its copy), invites,
// passkeys, push subscriptions, recovery backup (metadata). Secrets (password
// hash, TOTP secret, backup codes, session tokens) are never exported.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r).UserID
	if err := s.limit.export.Check(me); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	x := &exporter{ctx: ctx, db: s.db}
	q := x.rows
	image := func(table string) map[string]any {
		var ct string
		var data []byte
		var at int64
		if s.db.QueryRowContext(ctx, `SELECT content_type, data, updated_at FROM `+table+` WHERE user_id = ?`, me).Scan(&ct, &data, &at) != nil {
			return nil
		}
		return map[string]any{"content_type": ct, "updated_at": time.Unix(0, at).UTC(), "data_base64": base64.StdEncoding.EncodeToString(data)} // nanoseconds
	}
	doc := map[string]any{
		"format":      "quarel-identity-export-1",
		"service":     s.cfg.Issuer,
		"exported_at": s.now().UTC(),
		"account": first(q(`SELECT id, email, pseudo, created_at, email_verified_at, totp_enabled_at IS NOT NULL AS two_factor,
			bio, profile_theme, pseudo_changed_at, presence, share_typing, share_read_receipts, friend_requests, invited_by
			FROM users WHERE id = ?`, me)),
		"avatar":      image("avatars"),
		"banner":      image("banners"),
		"sessions":    q(`SELECT id, device_name, created_at, last_seen_at FROM sessions WHERE user_id = ? ORDER BY created_at`, me),
		"master_key":  first(q(`SELECT ed25519, created_at FROM master_keys WHERE user_id = ?`, me)),
		"device_keys": q(`SELECT session_id AS device_id, curve25519, ed25519, master_signature IS NOT NULL AS verified, created_at FROM device_keys WHERE user_id = ?`, me),
		"friends": q(`SELECT u.id, u.pseudo, f.status, f.requester = ?1 AS requested_by_me, f.created_at
			FROM friendships f JOIN users u ON u.id = CASE WHEN f.user_a = ?1 THEN f.user_b ELSE f.user_a END
			WHERE ?1 IN (f.user_a, f.user_b) ORDER BY f.created_at`, me),
		"blocks": q(`SELECT u.id, u.pseudo, b.created_at FROM blocks b JOIN users u ON u.id = b.blocked_id WHERE b.user_id = ? ORDER BY b.created_at`, me),
		"conversations": q(`SELECT c.id, c.kind, c.name, c.owner_id = ?1 AS mine, m.joined_at,
			(SELECT group_concat(u.pseudo, ', ') FROM conversation_members m2 JOIN users u ON u.id = m2.user_id WHERE m2.conversation_id = c.id) AS members
			FROM conversation_members m JOIN conversations c ON c.id = m.conversation_id WHERE m.user_id = ?1 ORDER BY m.joined_at`, me),
		"files_waiting":   q(`SELECT conversation_id, id, size, created_at FROM conv_attachments WHERE uploader_id = ? ORDER BY created_at`, me),
		"invites":         q(`SELECT code, max_uses, uses, created_at, expires_at FROM registration_invites WHERE created_by = ? ORDER BY created_at`, me),
		"passkeys":        q(`SELECT name, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at`, me),
		"push":            q(`SELECT p.session_id AS device_id, p.endpoint, p.created_at FROM push_subscriptions p JOIN sessions s ON s.id = p.session_id WHERE s.user_id = ?`, me),
		"recovery_backup": first(q(`SELECT version, length(data) AS encrypted_bytes, updated_at FROM backups WHERE user_id = ?`, me)),
		"messages_note": "Les messages privés sont chiffrés de bout en bout : le service ne les garde pas (seulement le temps de les remettre, chiffrés). " +
			"L'application Quarel ajoute à cet export l'historique gardé sur l'appareil.",
	}
	if x.err != nil {
		writeErr(w, r, x.err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="quarel-donnees.json"`)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, doc)
}

// exporter runs the export's queries, keeping the first error.
type exporter struct {
	ctx context.Context
	db  *sql.DB
	err error
}

func (x *exporter) rows(query string, args ...any) []map[string]any {
	if x.err != nil {
		return nil
	}
	out, err := exportRows(x.ctx, x.db, query, args...)
	x.err = err
	return out
}

func first(rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// exportRows reads rows as JSON objects (column name → value); *_at columns
// holding Unix seconds become dates.
func exportRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if n, ok := v.(int64); ok && len(c) > 3 && c[len(c)-3:] == "_at" {
				v = time.Unix(n, 0).UTC()
			}
			m[c] = v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
