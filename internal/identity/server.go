// Package identity implements the Quarel Identity service: accounts, 2FA,
// device sessions and issuance of portable identity tokens.
package identity

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/ratelimit"
	"github.com/anlekg/quarel/internal/realtime"
	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/internal/tlsconf"
	"github.com/anlekg/quarel/pkg/idtoken"
)

// Config holds the Identity service settings.
type Config struct {
	Addr     string
	DataDir  string
	Issuer   string // public name of this service, used in handles (pseudo@Issuer)
	TokenTTL time.Duration
	SMTP     SMTPMailer // SMTP.Host empty → emails are logged instead of sent
	Limits   Limits
	// TrustedProxies are reverse proxies whose X-Forwarded-For is believed.
	TrustedProxies ratelimit.Proxies
	TLS            tlsconf.Config

	DMFileMaxBytes int64         // size limit of an encrypted conversation file
	DMFileTTL      time.Duration // how long the server keeps conversation files

	TURN TURNConfig // relay for peer-to-peer calls
}

// Limits caps request rates (0 disables a limit).
type Limits struct {
	Global            int // requests per client IP per minute, all endpoints
	Register          int // registrations per client IP per hour
	Login             int // login attempts per client IP per 10 minutes
	Email             int // email verification requests per client IP per hour
	FriendRequests    int // friend requests per user per hour
	Files             int // encrypted conversation files uploaded per user per hour
	AuthFailuresPerIP int // failed passwords/2FA codes per account and IP per hour → lockout
	AuthFailuresTotal int // failed passwords/2FA codes per account per hour, all IPs → lockout
}

// DefaultLimits are the production limits.
func DefaultLimits() Limits {
	return Limits{Global: 300, Register: 5, Login: 20, Email: 20, FriendRequests: 30, Files: 60,
		AuthFailuresPerIP: maxAuthFailures, AuthFailuresTotal: maxAuthFailuresAll}
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
	c.Limits = DefaultLimits()
	if os.Getenv("QUAREL_RATE_LIMITS") == "off" {
		c.Limits = Limits{AuthFailuresPerIP: maxAuthFailures, AuthFailuresTotal: maxAuthFailuresAll}
	}
	proxies, err := ratelimit.ParseProxies(os.Getenv("QUAREL_TRUSTED_PROXIES"))
	if err != nil {
		return c, fmt.Errorf("QUAREL_TRUSTED_PROXIES: %w", err)
	}
	c.TrustedProxies = proxies
	if c.TLS, err = tlsconf.FromEnv("off"); err != nil {
		return c, err
	}
	ttl, err := time.ParseDuration(env("QUAREL_TOKEN_TTL", "12h"))
	if err != nil || ttl < time.Minute {
		return c, fmt.Errorf("QUAREL_TOKEN_TTL: invalid duration")
	}
	c.TokenTTL = ttl
	if c.TURN, err = turnConfigFromEnv(); err != nil {
		return c, err
	}
	c.DMFileMaxBytes = int64(envInt("QUAREL_DM_FILE_MAX_MB", 25)) << 20
	if c.DMFileTTL, err = time.ParseDuration(env("QUAREL_DM_FILE_TTL", "720h")); err != nil || c.DMFileTTL < time.Hour {
		return c, fmt.Errorf("QUAREL_DM_FILE_TTL: invalid duration (at least 1h)")
	}
	if c.SMTP.Host != "" && c.SMTP.From == "" {
		return c, fmt.Errorf("QUAREL_SMTP_FROM is required when QUAREL_SMTP_HOST is set")
	}
	return c, nil
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
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
	hub    *realtime.Hub
	now    func() time.Time

	proxies ratelimit.Proxies
	limit   struct{ global, register, login, email, friends, files, typing, turn *ratelimit.Limiter }
	turnKey string // shared secret of the TURN relay ("" when it is off)
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
	key, err := secret.LoadOrCreateKey(filepath.Join(cfg.DataDir, "signing.key"))
	if err != nil {
		db.Close()
		return nil, err
	}
	retired, err := liveRetiredKeys(cfg.DataDir, cfg.TokenTTL, time.Now())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", retiredKeysFile, err)
	}
	var mailer Mailer = LogMailer{}
	if cfg.SMTP.Host != "" {
		mailer = cfg.SMTP
	}
	return New(cfg, db, key, mailer, retired...), nil
}

// New builds a Server from already-opened dependencies. retired keys are
// former signing keys still published (see Admin.RotateSigningKey).
func New(cfg Config, db *sql.DB, key ed25519.PrivateKey, mailer Mailer, retired ...ed25519.PublicKey) *Server {
	s := &Server{
		cfg:    cfg,
		db:     db,
		signer: idtoken.NewSigner(cfg.Issuer, key, retired...),
		mailer: mailer,
		hub:    realtime.NewHub(),
		now:    time.Now,
	}
	s.hub.OnGroup = s.presenceHook
	s.proxies = cfg.TrustedProxies
	s.limit.global = ratelimit.New(cfg.Limits.Global, time.Minute)
	s.limit.register = ratelimit.New(cfg.Limits.Register, time.Hour)
	s.limit.login = ratelimit.New(cfg.Limits.Login, 10*time.Minute)
	s.limit.email = ratelimit.New(cfg.Limits.Email, time.Hour)
	s.limit.friends = ratelimit.New(cfg.Limits.FriendRequests, time.Hour)
	s.limit.files = ratelimit.New(cfg.Limits.Files, time.Hour)
	s.limit.typing = ratelimit.New(1, 3*time.Second)
	s.limit.turn = ratelimit.New(60, time.Hour)
	if s.cfg.DMFileMaxBytes <= 0 {
		s.cfg.DMFileMaxBytes = 25 << 20
	}
	if s.cfg.DMFileTTL <= 0 {
		s.cfg.DMFileTTL = 30 * 24 * time.Hour
	}
	return s
}

func (s *Server) byIP(r *http.Request) string { return s.proxies.ClientIP(r) }

// limited applies a per-IP limiter to a handler.
func (s *Server) limited(l *ratelimit.Limiter, h http.HandlerFunc) http.HandlerFunc {
	return l.Wrap(s.byIP, h).ServeHTTP
}

// DisconnectAll closes every real-time connection.
func (s *Server) DisconnectAll() { s.hub.CloseAll() }

// Close disconnects clients and releases the database.
func (s *Server) Close() error {
	s.hub.CloseAll()
	return s.db.Close()
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+idtoken.WellKnownPath, s.handleKeySet)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /v1/auth/register", s.limited(s.limit.register, s.handleRegister))
	mux.HandleFunc("POST /v1/auth/verify-email", s.limited(s.limit.email, s.handleVerifyEmail))
	mux.HandleFunc("POST /v1/auth/resend-verification", s.limited(s.limit.email, s.handleResendVerification))
	mux.HandleFunc("POST /v1/auth/login", s.limited(s.limit.login, s.handleLogin))
	mux.HandleFunc("POST /v1/auth/logout", s.authed(s.handleLogout))
	mux.HandleFunc("POST /v1/auth/forgot-password", s.limited(s.limit.email, s.handleForgotPassword))
	mux.HandleFunc("POST /v1/auth/reset-password", s.limited(s.limit.login, s.handleResetPassword))
	mux.HandleFunc("GET /v1/disabled-accounts", s.handleDisabledAccounts)

	mux.HandleFunc("GET /v1/me", s.authed(s.handleMe))
	mux.HandleFunc("PATCH /v1/me", s.authed(s.handleChangePseudo))
	mux.HandleFunc("DELETE /v1/me", s.authed(s.handleDeleteAccount))
	mux.HandleFunc("POST /v1/me/password", s.authed(s.handleChangePassword))
	mux.HandleFunc("POST /v1/me/email", s.authed(s.handleChangeEmail))
	mux.HandleFunc("POST /v1/me/email/confirm", s.authed(s.handleConfirmEmail))
	mux.HandleFunc("PATCH /v1/me/profile", s.authed(s.handleUpdateProfile))
	mux.HandleFunc("PUT /v1/me/avatar", s.authed(s.handleSetAvatar))
	mux.HandleFunc("DELETE /v1/me/avatar", s.authed(s.handleDeleteAvatar))
	mux.HandleFunc("PUT /v1/me/presence", s.authed(s.handleSetPresence))
	mux.HandleFunc("GET /v1/users/{id}/profile", s.handleProfile)
	mux.HandleFunc("GET /v1/users/{id}/avatar", s.handleAvatar)
	mux.HandleFunc("GET /v1/blocks", s.authed(s.handleListBlocks))
	mux.HandleFunc("POST /v1/blocks", s.authed(s.handleBlock))
	mux.HandleFunc("PUT /v1/blocks/{id}", s.authed(s.handleBlock))
	mux.HandleFunc("DELETE /v1/blocks/{id}", s.authed(s.handleUnblock))
	mux.HandleFunc("GET /v1/me/sessions", s.authed(s.handleListSessions))
	mux.HandleFunc("DELETE /v1/me/sessions/{id}", s.authed(s.handleRevokeSession))
	mux.HandleFunc("POST /v1/me/2fa/setup", s.authed(s.handle2FASetup))
	mux.HandleFunc("POST /v1/me/2fa/enable", s.authed(s.handle2FAEnable))
	mux.HandleFunc("POST /v1/me/2fa/disable", s.authed(s.handle2FADisable))

	mux.HandleFunc("POST /v1/identity/token", s.authed(s.handleIssueToken))

	mux.HandleFunc("GET /v1/friends", s.authed(s.handleListFriends))
	mux.HandleFunc("POST /v1/friends", s.authed(s.handleAddFriend))
	mux.HandleFunc("POST /v1/friends/{id}/accept", s.authed(s.handleAcceptFriend))
	mux.HandleFunc("DELETE /v1/friends/{id}", s.authed(s.handleRemoveFriend))

	mux.HandleFunc("POST /v1/keys/device", s.authed(s.handleUploadDeviceKeys))
	mux.HandleFunc("GET /v1/keys/device", s.authed(s.writeOwnDevice))
	mux.HandleFunc("POST /v1/keys/certify", s.authed(s.handleCertifyDevice))
	mux.HandleFunc("POST /v1/keys/one-time", s.authed(s.handleUploadOneTimeKeys))
	mux.HandleFunc("POST /v1/keys/claim", s.authed(s.handleClaimKeys))
	mux.HandleFunc("GET /v1/users/{id}/keys", s.authed(s.handleUserKeys))

	mux.HandleFunc("GET /v1/dms", s.authed(s.handleListDMs))
	mux.HandleFunc("POST /v1/dms", s.authed(s.handleOpenDM))
	mux.HandleFunc("POST /v1/dms/{id}/messages", s.authed(s.handleSendDM))
	mux.HandleFunc("PATCH /v1/dms/{id}", s.authed(s.handleRenameGroup))
	mux.HandleFunc("PUT /v1/dms/{id}/members/{user}", s.authed(s.handleAddGroupMember))
	mux.HandleFunc("DELETE /v1/dms/{id}/members/{user}", s.authed(s.handleRemoveGroupMember))
	mux.HandleFunc("POST /v1/dms/{id}/typing", s.authed(s.handleTyping))
	mux.HandleFunc("POST /v1/dms/{id}/read", s.authed(s.handleRead))
	mux.HandleFunc("POST /v1/dms/{id}/files", s.authed(s.handleUploadFile))
	mux.HandleFunc("GET /v1/dms/{id}/files/{file}", s.authed(s.handleDownloadFile))
	mux.HandleFunc("DELETE /v1/dms/{id}/files/{file}", s.authed(s.handleDeleteFile))
	mux.HandleFunc("GET /v1/me/privacy", s.authed(s.handlePrivacy))
	mux.HandleFunc("GET /v1/calls/ice-servers", s.authed(s.handleCallServers))
	mux.HandleFunc("PATCH /v1/me/privacy", s.authed(s.handlePrivacy))
	mux.HandleFunc("POST /v1/to-device", s.authed(s.handleSendToDevice))
	mux.HandleFunc("GET /v1/inbox", s.authed(s.handleInbox))
	mux.HandleFunc("POST /v1/inbox/ack", s.authed(s.handleAckInbox))

	mux.HandleFunc("GET /v1/backup", s.authed(s.handleGetBackup))
	mux.HandleFunc("PUT /v1/backup", s.authed(s.handlePutBackup))
	mux.HandleFunc("DELETE /v1/backup", s.authed(s.handleDeleteBackup))

	mux.HandleFunc("GET /v1/gateway", s.handleGateway)
	return s.limit.global.Wrap(s.byIP, mux)
}

// --- request/response helpers (shared conventions, see internal/httpapi) ---

type apiError = httpapi.Error

var (
	errf      = httpapi.Errf
	writeJSON = httpapi.WriteJSON
	writeErr  = httpapi.WriteErr
	decode    = httpapi.Decode
)

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
		sess, disabled, err := s.sessionForToken(r.Context(), token)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if sess == nil {
			writeErr(w, r, errf(http.StatusUnauthorized, "unauthorized", "invalid or expired session"))
			return
		}
		if disabled {
			writeErr(w, r, errf(http.StatusForbidden, "account_disabled", "this account has been disabled"))
			return
		}
		s.db.ExecContext(r.Context(), `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, s.now().Unix(), sess.ID)
		h(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	}
}

func sessionFrom(r *http.Request) *session { return r.Context().Value(sessionKey{}).(*session) }

func (s *Server) handleKeySet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, s.signer.KeySet())
}
