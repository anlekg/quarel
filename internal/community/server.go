// Package community implements a self-hosted Quarel community server:
// members (portable identities), invites, channels and real-time messages.
package community

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/secret"
)

var (
	errf      = httpapi.Errf
	writeJSON = httpapi.WriteJSON
	writeErr  = httpapi.WriteErr
	decode    = httpapi.Decode
)

// Server is a community server.
type Server struct {
	cfg    Config
	db     *sql.DB
	id     string // derived from the server key; clients sign login proofs for it
	keys   KeySource
	nonces *nonceStore
	hub    *hub
	now    func() time.Time
}

// ServerID derives the public server identifier from its key.
func ServerID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:16]))
}

// Open prepares the data directory, database, server key and first-start content.
func Open(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	db, err := OpenDB(filepath.Join(cfg.DataDir, "server.db"))
	if err != nil {
		return nil, err
	}
	key, err := secret.LoadOrCreateKey(filepath.Join(cfg.DataDir, "server.key"))
	if err != nil {
		db.Close()
		return nil, err
	}
	s := New(cfg, db, key, NewHTTPKeySource())
	if err := s.seed(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// New builds a Server from already-opened dependencies. Call seed before use.
func New(cfg Config, db *sql.DB, key ed25519.PrivateKey, keys KeySource) *Server {
	return &Server{
		cfg:    cfg,
		db:     db,
		id:     ServerID(key.Public().(ed25519.PublicKey)),
		keys:   keys,
		nonces: newNonceStore(),
		hub:    newHub(),
		now:    time.Now,
	}
}

// ID returns the server identifier.
func (s *Server) ID() string { return s.id }

// DisconnectAll closes every real-time connection.
func (s *Server) DisconnectAll() { s.hub.closeAll() }

// Close disconnects clients and releases the database.
func (s *Server) Close() error {
	s.hub.closeAll()
	return s.db.Close()
}

func (s *Server) nowMs() int64 { return s.now().UnixMilli() }

func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMs(v.Int64)
	return &t
}

// --- settings ---

const (
	accessPrivate = "private" // joining requires an invite
	accessPublic  = "public"  // anyone with a trusted identity can join
)

func (s *Server) setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func setSetting(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, key, value string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// seed creates the initial settings and channels on first start.
func (s *Server) seed(ctx context.Context) error {
	if name, err := s.setting(ctx, "name"); err != nil || name != "" {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setSetting(ctx, tx, "name", s.cfg.Name); err != nil {
		return err
	}
	if err := setSetting(ctx, tx, "access", accessPrivate); err != nil {
		return err
	}
	now := s.nowMs()
	for i, cat := range []struct{ name, childType, child string }{
		{"Salons textuels", "text", "général"},
		{"Salons vocaux", "voice", "Général"},
	} {
		res, err := tx.ExecContext(ctx, `INSERT INTO channels (type, name, position, created_at) VALUES ('category', ?, ?, ?)`, cat.name, i, now)
		if err != nil {
			return err
		}
		parent, _ := res.LastInsertId()
		if _, err := tx.ExecContext(ctx, `INSERT INTO channels (type, name, parent_id, position, created_at) VALUES (?, ?, ?, 0, ?)`,
			cat.childType, cat.child, parent, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PrepareClaim returns a fresh one-time code that makes its user the owner,
// or "" if the server already has an owner. A new code replaces the previous one.
func (s *Server) PrepareClaim(ctx context.Context) (string, error) {
	var owners int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE is_owner = 1`).Scan(&owners); err != nil {
		return "", err
	}
	if owners > 0 {
		return "", nil
	}
	code := secret.Code(10)
	return code, setSetting(ctx, s.db, "claim_code_hash", secret.SHA256Hex(code))
}

// --- HTTP ---

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/server", s.handleServerInfo)
	mux.HandleFunc("PATCH /v1/server", s.authed(s.ownerOnly(s.handleServerUpdate)))

	mux.HandleFunc("POST /v1/auth/challenge", s.handleChallenge)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.authed(s.handleLogout))

	mux.HandleFunc("GET /v1/members", s.authed(s.handleListMembers))
	mux.HandleFunc("GET /v1/members/@me", s.authed(s.handleMe))
	mux.HandleFunc("PATCH /v1/members/@me", s.authed(s.handleUpdateMe))
	mux.HandleFunc("DELETE /v1/members/@me", s.authed(s.handleLeave))

	mux.HandleFunc("GET /v1/invites", s.authed(s.handleListInvites))
	mux.HandleFunc("POST /v1/invites", s.authed(s.handleCreateInvite))
	mux.HandleFunc("DELETE /v1/invites/{code}", s.authed(s.handleRevokeInvite))

	mux.HandleFunc("GET /v1/channels", s.authed(s.handleListChannels))
	mux.HandleFunc("POST /v1/channels", s.authed(s.ownerOnly(s.handleCreateChannel)))
	mux.HandleFunc("PATCH /v1/channels/{id}", s.authed(s.ownerOnly(s.handleUpdateChannel)))
	mux.HandleFunc("DELETE /v1/channels/{id}", s.authed(s.ownerOnly(s.handleDeleteChannel)))

	mux.HandleFunc("GET /v1/channels/{id}/messages", s.authed(s.handleListMessages))
	mux.HandleFunc("POST /v1/channels/{id}/messages", s.authed(s.handleCreateMessage))
	mux.HandleFunc("PATCH /v1/channels/{id}/messages/{mid}", s.authed(s.handleEditMessage))
	mux.HandleFunc("DELETE /v1/channels/{id}/messages/{mid}", s.authed(s.handleDeleteMessage))

	mux.HandleFunc("GET /v1/gateway", s.handleGateway)
	return mux
}

type serverInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Access      string `json:"access"`
	MemberCount int    `json:"member_count"`
}

func (s *Server) info(ctx context.Context) (serverInfo, error) {
	info := serverInfo{ID: s.id}
	var err error
	if info.Name, err = s.setting(ctx, "name"); err != nil {
		return info, err
	}
	if info.Access, err = s.setting(ctx, "access"); err != nil {
		return info, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE left_at IS NULL`).Scan(&info.MemberCount)
	return info, err
}

// handleServerInfo is public: a client shows it before joining.
func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.info(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleServerUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   *string `json:"name"`
		Access *string `json:"access"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 100 {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_name", "name must be 1-100 characters"))
			return
		}
		if err := setSetting(ctx, s.db, "name", name); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	if req.Access != nil {
		if *req.Access != accessPrivate && *req.Access != accessPublic {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_access", "access must be %q or %q", accessPrivate, accessPublic))
			return
		}
		if err := setSetting(ctx, s.db, "access", *req.Access); err != nil {
			writeErr(w, r, err)
			return
		}
	}
	info, err := s.info(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.hub.broadcast("SERVER_UPDATE", info)
	writeJSON(w, http.StatusOK, info)
}
