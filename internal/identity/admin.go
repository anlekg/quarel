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
	"regexp"
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

// Admin returns the operator tools on the running service's database.
func (s *Server) Admin() *Admin { return NewAdmin(s.db) }

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

// Account is a row of the operator's account search.
type Account struct {
	ID         string     `json:"id"`
	Pseudo     string     `json:"pseudo"`
	Email      string     `json:"email"`
	CreatedAt  time.Time  `json:"created_at"`
	DisabledAt *time.Time `json:"disabled_at"`
}

// Search finds accounts by id, email or pseudo (prefix, case-insensitive);
// an empty query lists the most recent ones.
func (a *Admin) Search(ctx context.Context, q string, limit int) ([]Account, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, pseudo, email, created_at, disabled_at FROM users
		WHERE ? = '' OR id = ? OR email LIKE ? ESCAPE '\' OR pseudo_norm LIKE ? ESCAPE '\'
		ORDER BY created_at DESC LIMIT ?`, q, q, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var acc Account
		var created int64
		var disabled sql.NullInt64
		if err := rows.Scan(&acc.ID, &acc.Pseudo, &acc.Email, &created, &disabled); err != nil {
			return nil, err
		}
		acc.CreatedAt = time.Unix(created, 0).UTC()
		if disabled.Valid {
			t := time.Unix(disabled.Int64, 0).UTC()
			acc.DisabledAt = &t
		}
		out = append(out, acc)
	}
	return out, rows.Err()
}

// Counts returns the number of accounts and of disabled ones.
func (a *Admin) Counts(ctx context.Context) (total, disabled int, err error) {
	err = a.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(disabled_at) FROM users`).Scan(&total, &disabled)
	return
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

// --- registration invitations and community servers (operator) ---

// Invites lists every invitation, newest first.
func (a *Admin) Invites(ctx context.Context) ([]Invite, error) { return listInvites(ctx, a.db, nil) }

// CreateInvite creates an operator invitation (maxUses 0: unlimited; validFor 0: never expires).
func (a *Admin) CreateInvite(ctx context.Context, note string, maxUses int, validFor time.Duration) (Invite, error) {
	if maxUses < 0 {
		return Invite{}, errors.New("nombre d'utilisations invalide")
	}
	var exp *time.Time
	if validFor > 0 {
		t := a.now().Add(validFor).UTC().Truncate(time.Second)
		exp = &t
	}
	return createInvite(ctx, a.db, "", strings.TrimSpace(note), maxUses, exp, a.now())
}

// RevokeInvite makes an invitation unusable (it stays listed).
func (a *Admin) RevokeInvite(ctx context.Context, code string) error {
	_, err := a.db.ExecContext(ctx, `UPDATE registration_invites SET expires_at = ? WHERE code = ?`, a.now().Unix(), code)
	return err
}

// CommunityServer is a server known to the operator: blocked, or asking for approval.
type CommunityServer struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"`
	URL         string     `json:"url,omitempty"`
	Contact     string     `json:"contact,omitempty"`
	Status      string     `json:"status"` // pending, approved, rejected, or "" (never asked)
	Blocked     bool       `json:"blocked"`
	Reason      string     `json:"reason,omitempty"` // why it is blocked
	RequestedAt *time.Time `json:"requested_at,omitempty"`
}

// Servers lists blocked servers and approval requests.
func (a *Admin) Servers(ctx context.Context) ([]CommunityServer, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, COALESCE(name, ''), COALESCE(url, ''), COALESCE(contact, ''), COALESCE(status, ''), blocked, COALESCE(reason, ''), requested_at FROM (
			SELECT a.server_id AS id, a.name, a.url, a.contact, a.status, b.server_id IS NOT NULL AS blocked, b.reason, a.requested_at
			FROM server_approvals a LEFT JOIN blocked_servers b ON b.server_id = a.server_id
			UNION ALL
			SELECT b.server_id, NULL, NULL, NULL, NULL, 1, b.reason, NULL
			FROM blocked_servers b WHERE b.server_id NOT IN (SELECT server_id FROM server_approvals))
		ORDER BY requested_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CommunityServer{}
	for rows.Next() {
		var c CommunityServer
		var req sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &c.URL, &c.Contact, &c.Status, &c.Blocked, &c.Reason, &req); err != nil {
			return nil, err
		}
		if req.Valid {
			t := time.Unix(req.Int64, 0).UTC()
			c.RequestedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

var serverIDRe = regexp.MustCompile(`^[a-z2-7]{26}$`)

// BlockServer adds a community server to the public block list.
func (a *Admin) BlockServer(ctx context.Context, id, reason, operator string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if !serverIDRe.MatchString(id) {
		return errors.New("identifiant de serveur invalide (26 caractères, visible dans son lien d'invitation après « sid= »)")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO blocked_servers (server_id, reason, created_at) VALUES (?, ?, ?)
		ON CONFLICT (server_id) DO UPDATE SET reason = excluded.reason`, id, strings.TrimSpace(reason), a.now().Unix()); err != nil {
		return err
	}
	if err := a.log(ctx, tx, "server_block", "", "serveur "+id+" : "+reason, operator); err != nil {
		return err
	}
	return tx.Commit()
}

// UnblockServer removes a server from the block list.
func (a *Admin) UnblockServer(ctx context.Context, id, operator string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM blocked_servers WHERE server_id = ?`, id); err != nil {
		return err
	}
	if err := a.log(ctx, tx, "server_unblock", "", "serveur "+id, operator); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideServer approves or rejects an approval request.
func (a *Admin) DecideServer(ctx context.Context, id string, approve bool, operator string) error {
	status, action := "rejected", "server_reject"
	if approve {
		status, action = "approved", "server_approve"
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE server_approvals SET status = ?, decided_at = ? WHERE server_id = ?`, status, a.now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("aucune demande de ce serveur")
	}
	if err := a.log(ctx, tx, action, "", "serveur "+id, operator); err != nil {
		return err
	}
	return tx.Commit()
}
