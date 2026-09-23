// Package identity implements the Quarel Identity service: accounts, 2FA,
// device sessions and issuance of portable identity tokens.
package identity

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

// Config holds the Identity service settings.
type Config struct {
	Addr     string
	DataDir  string
	Issuer   string // public name of this service, used in handles (pseudo@Issuer)
	TokenTTL time.Duration
	SMTP     SMTPMailer // SMTP.Host empty → emails are logged instead of sent
}

// ConfigFromEnv reads the configuration from QUAREL_* environment variables.
func ConfigFromEnv() (Config, error) {
	c := Config{
		Addr:    env("QUAREL_ADDR", ":8080"),
		DataDir: env("QUAREL_DATA_DIR", "./data"),
		Issuer:  env("QUAREL_ISSUER", "localhost:8080"),
		SMTP: SMTPMailer{
			Host:     os.Getenv("QUAREL_SMTP_HOST"),
			Port:     env("QUAREL_SMTP_PORT", "587"),
			User:     os.Getenv("QUAREL_SMTP_USER"),
			Password: os.Getenv("QUAREL_SMTP_PASSWORD"),
			From:     os.Getenv("QUAREL_SMTP_FROM"),
		},
	}
	ttl, err := time.ParseDuration(env("QUAREL_TOKEN_TTL", "12h"))
	if err != nil || ttl < time.Minute {
		return c, fmt.Errorf("QUAREL_TOKEN_TTL: invalid duration")
	}
	c.TokenTTL = ttl
	if c.SMTP.Host != "" && c.SMTP.From == "" {
		return c, fmt.Errorf("QUAREL_SMTP_FROM is required when QUAREL_SMTP_HOST is set")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Server is the Identity HTTP service.
type Server struct {
	cfg    Config
	db     *sql.DB
	signer *idtoken.Signer
	mailer Mailer
	now    func() time.Time
}

// Open prepares the data directory, database and signing key.
func Open(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	db, err := OpenDB(filepath.Join(cfg.DataDir, "identity.db"))
	if err != nil {
		return nil, err
	}
	key, err := loadOrCreateSigningKey(cfg.DataDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	var mailer Mailer = LogMailer{}
	if cfg.SMTP.Host != "" {
		mailer = cfg.SMTP
	}
	return New(cfg, db, key, mailer), nil
}

// New builds a Server from already-opened dependencies.
func New(cfg Config, db *sql.DB, key ed25519.PrivateKey, mailer Mailer) *Server {
	return &Server{
		cfg:    cfg,
		db:     db,
		signer: idtoken.NewSigner(cfg.Issuer, key),
		mailer: mailer,
		now:    time.Now,
	}
}

// Close releases the database.
func (s *Server) Close() error { return s.db.Close() }

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+idtoken.WellKnownPath, s.handleKeySet)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /v1/auth/verify-email", s.handleVerifyEmail)
	mux.HandleFunc("POST /v1/auth/resend-verification", s.handleResendVerification)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.authed(s.handleLogout))

	mux.HandleFunc("GET /v1/me", s.authed(s.handleMe))
	mux.HandleFunc("GET /v1/me/sessions", s.authed(s.handleListSessions))
	mux.HandleFunc("DELETE /v1/me/sessions/{id}", s.authed(s.handleRevokeSession))
	mux.HandleFunc("POST /v1/me/2fa/setup", s.authed(s.handle2FASetup))
	mux.HandleFunc("POST /v1/me/2fa/enable", s.authed(s.handle2FAEnable))
	mux.HandleFunc("POST /v1/me/2fa/disable", s.authed(s.handle2FADisable))

	mux.HandleFunc("POST /v1/identity/token", s.authed(s.handleIssueToken))
	return mux
}

// --- request/response helpers ---

type apiError struct {
	status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func errf(status int, code, format string, args ...any) *apiError {
	return &apiError{status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeErr sends err as a JSON error; unexpected errors are logged and hidden.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		slog.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
		ae = errf(http.StatusInternalServerError, "internal", "internal server error")
	}
	writeJSON(w, ae.status, map[string]*apiError{"error": ae})
}

const maxBody = 64 << 10

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errf(http.StatusBadRequest, "bad_request", "invalid JSON body: %v", err)
	}
	return nil
}

// --- authentication ---

type session struct {
	ID        string
	UserID    string
	DeviceKey string
}

type sessionKey struct{}

func (s *Server) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeErr(w, r, errf(http.StatusUnauthorized, "unauthorized", "missing bearer token"))
			return
		}
		var sess session
		var disabled sql.NullInt64
		err := s.db.QueryRowContext(r.Context(), `
			SELECT s.id, s.user_id, s.device_key, u.disabled_at
			FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.token_hash = ?`, sha256Hex(token)).Scan(&sess.ID, &sess.UserID, &sess.DeviceKey, &disabled)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, r, errf(http.StatusUnauthorized, "unauthorized", "invalid or expired session"))
			return
		}
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if disabled.Valid {
			writeErr(w, r, errf(http.StatusForbidden, "account_disabled", "this account has been disabled"))
			return
		}
		s.db.ExecContext(r.Context(), `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, s.now().Unix(), sess.ID)
		h(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, &sess)))
	}
}

func sessionFrom(r *http.Request) *session { return r.Context().Value(sessionKey{}).(*session) }

func (s *Server) handleKeySet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, s.signer.KeySet())
}
