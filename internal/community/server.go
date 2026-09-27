// Package community implements a self-hosted Quarel community server:
// members (portable identities), invites, channels and real-time messages.
package community

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/tls"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/netdiag"
	"github.com/anlekg/quarel/internal/ratelimit"
	"github.com/anlekg/quarel/internal/realtime"
	"github.com/anlekg/quarel/internal/secret"
	"github.com/anlekg/quarel/pkg/tlsbind"
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
	hub    *realtime.Hub
	now    func() time.Time

	voiceOpts *VoiceOptions  // nil: voice disabled
	voice     *voiceRegistry // who is in which voice channel

	key      ed25519.PrivateKey
	enc      *ecdh.PrivateKey                            // tokens sealed for this server (Identity services that approve servers)
	network  func(ctx context.Context) netdiag.Diagnosis // nil: no diagnosis available
	proxies  ratelimit.Proxies
	limit    struct{ global, auth, messages, uploads, typing, phone, threads *ratelimit.Limiter }
	previews *previewer    // nil: link previews disabled
	phone    PhoneVerifier // nil: phone verification unavailable
	disabled disabledSet   // accounts disabled by their identity service
	names    []string      // host names clients may use with an ordinary certificate (see checkProof)
	automod  autoModState  // recent messages and refusals (automatic moderation)
	cmds     interactions  // slash commands waiting for their bot's reply
}

// ServerID derives the public server identifier from its key.
func ServerID(pub ed25519.PublicKey) string { return tlsbind.ServerID(pub) }

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
	s := &Server{
		cfg:     cfg,
		db:      db,
		id:      ServerID(key.Public().(ed25519.PublicKey)),
		keys:    keys,
		nonces:  newNonceStore(),
		hub:     realtime.NewHub(),
		now:     time.Now,
		key:     key,
		enc:     deriveEncKey(key),
		proxies: cfg.TrustedProxies,
		names:   hostNames(cfg.TLS),
	}
	s.limit.global = ratelimit.New(cfg.Limits.Global, time.Minute)
	s.limit.auth = ratelimit.New(cfg.Limits.Auth, time.Minute)
	s.limit.messages = ratelimit.New(cfg.Limits.Messages, 10*time.Second)
	s.limit.uploads = ratelimit.New(cfg.Limits.Uploads, time.Minute)
	s.limit.typing = ratelimit.New(1, 3*time.Second)
	s.limit.phone = ratelimit.New(cfg.Limits.Phone, time.Hour)
	s.limit.threads = ratelimit.New(cfg.Limits.Threads, 10*time.Minute)
	s.phone = newPhoneVerifier(cfg)
	if s.cfg.MaxUploadBytes <= 0 {
		s.cfg.MaxUploadBytes = 25 << 20
	}
	if cfg.LinkPreviews {
		s.previews = newPreviewer(false)
	}
	return s
}

// SetNetworkDiagnosis provides the reachability diagnosis shown to server managers.
func (s *Server) SetNetworkDiagnosis(f func(ctx context.Context) netdiag.Diagnosis) { s.network = f }

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if s.network == nil {
		writeErr(w, r, errf(http.StatusServiceUnavailable, "unavailable", "network diagnosis is not available"))
		return
	}
	writeJSON(w, http.StatusOK, s.network(r.Context()))
}

// TLSConfig returns the HTTPS configuration (nil when TLS is off).
func (s *Server) TLSConfig() (*tls.Config, error) { return s.cfg.TLS.Build(s.cfg.DataDir, s.key) }

func (s *Server) byIP(r *http.Request) string { return s.proxies.ClientIP(r) }

// ID returns the server identifier.
func (s *Server) ID() string { return s.id }

// DisconnectAll closes every real-time connection.
func (s *Server) DisconnectAll() { s.hub.CloseAll() }

// Close disconnects clients and releases the database.
func (s *Server) Close() error {
	s.hub.CloseAll()
	return s.db.Close()
}

func (s *Server) logErr(msg string, err error) { slog.Error(msg, "err", err) }

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

// Overview is what the host's administration interface shows.
type Overview struct {
	Name     string
	Members  int
	HasOwner bool
}

// Overview returns the server name, member count and whether it has an owner.
func (s *Server) Overview(ctx context.Context) (Overview, error) {
	info, err := s.info(ctx)
	if err != nil {
		return Overview{}, err
	}
	var owners int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE is_owner = 1`).Scan(&owners)
	return Overview{Name: info.Name, Members: info.MemberCount, HasOwner: owners > 0}, err
}

// SetName renames the server (from the host's administration interface).
func (s *Server) SetName(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return errf(http.StatusBadRequest, "invalid_name", "name must be 1-100 characters")
	}
	if err := setSetting(ctx, s.db, "name", name); err != nil {
		return err
	}
	info, err := s.info(ctx)
	if err != nil {
		return err
	}
	s.hub.Broadcast("SERVER_UPDATE", info)
	return nil
}

// --- HTTP ---

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/server", s.handleServerInfo)
	mux.HandleFunc("GET /v1/server/network", s.authed(s.needPerm(permManageServer, s.handleNetwork)))
	mux.HandleFunc("PATCH /v1/server", s.authed(s.needPerm(permManageServer, s.handleServerUpdate)))
	mux.HandleFunc("GET /v1/server/automod", s.authed(s.needPerm(permManageServer, s.handleAutoMod)))
	mux.HandleFunc("PUT /v1/server/automod", s.authed(s.needPerm(permManageServer, s.handleAutoMod)))

	mux.Handle("POST /v1/auth/challenge", s.limit.auth.Wrap(s.byIP, http.HandlerFunc(s.handleChallenge)))
	mux.Handle("POST /v1/auth/login", s.limit.auth.Wrap(s.byIP, http.HandlerFunc(s.handleLogin)))
	mux.HandleFunc("POST /v1/auth/logout", s.authed(s.handleLogout))

	mux.HandleFunc("GET /v1/members", s.authed(s.handleListMembers))
	mux.HandleFunc("GET /v1/members/@me", s.authed(s.handleMe))
	mux.HandleFunc("PATCH /v1/members/@me", s.authed(s.handleUpdateMe))
	mux.HandleFunc("DELETE /v1/members/@me", s.authed(s.handleLeave))
	mux.HandleFunc("GET /v1/members/@me/permissions", s.authed(s.handleMyPermissions))
	mux.HandleFunc("PUT /v1/members/{id}/roles/{role}", s.authed(s.handleMemberRole))
	mux.HandleFunc("DELETE /v1/members/{id}/roles/{role}", s.authed(s.handleMemberRole))
	mux.HandleFunc("POST /v1/members/{id}/kick", s.authed(s.handleKick))
	mux.HandleFunc("PUT /v1/members/{id}/timeout", s.authed(s.handleTimeout))
	mux.HandleFunc("DELETE /v1/members/{id}/timeout", s.authed(s.handleTimeout))
	mux.HandleFunc("POST /v1/members/{id}/purge", s.authed(s.handlePurge))
	mux.HandleFunc("POST /v1/members/{id}/transfer-ownership", s.authed(s.handleTransferOwnership))
	mux.HandleFunc("POST /v1/members/@me/accept-rules", s.authed(s.handleAcceptRules))
	mux.HandleFunc("POST /v1/members/@me/phone", s.authed(s.handlePhoneStart))
	mux.HandleFunc("POST /v1/members/@me/phone/verify", s.authed(s.handlePhoneVerify))
	mux.HandleFunc("GET /v1/audit-log", s.authed(s.handleAuditLog))
	mux.HandleFunc("GET /v1/bots", s.authed(s.needPerm(permManageServer, s.handleListBots)))
	mux.HandleFunc("POST /v1/bots", s.authed(s.needPerm(permManageServer, s.handleCreateBot)))
	mux.HandleFunc("POST /v1/bots/{id}/token", s.authed(s.handleResetBotToken))
	mux.HandleFunc("DELETE /v1/bots/{id}", s.authed(s.handleDeleteBot))
	mux.HandleFunc("GET /v1/channels/{id}", s.authed(s.handleGetChannel))
	mux.HandleFunc("GET /v1/channels/{id}/posts", s.authed(s.handleListPosts))
	mux.HandleFunc("POST /v1/channels/{id}/posts", s.authed(s.handleCreatePost))
	mux.HandleFunc("GET /v1/emojis", s.authed(s.handleListEmojis))
	mux.HandleFunc("POST /v1/emojis", s.authed(s.needPerm(permManageServer, s.handleCreateEmoji)))
	mux.HandleFunc("DELETE /v1/emojis/{id}", s.authed(s.needPerm(permManageServer, s.handleDeleteEmoji)))
	mux.HandleFunc("GET /v1/emojis/{id}", s.authed(s.handleEmojiImage))
	mux.HandleFunc("PUT /v1/bots/@me/commands", s.authed(s.handleSetCommands))
	mux.HandleFunc("GET /v1/commands", s.authed(s.handleListCommands))
	mux.HandleFunc("POST /v1/channels/{id}/commands", s.authed(s.handleRunCommand))
	mux.HandleFunc("POST /v1/interactions/{id}/reply", s.authed(s.handleInteractionReply))
	mux.HandleFunc("GET /v1/channels/{id}/webhooks", s.authed(s.handleListWebhooks))
	mux.HandleFunc("POST /v1/channels/{id}/webhooks", s.authed(s.handleCreateWebhook))
	mux.HandleFunc("DELETE /v1/webhooks/{id}", s.authed(s.handleDeleteWebhook))
	mux.HandleFunc("POST /v1/webhooks/{id}/{token}", s.handleWebhookPost)

	mux.HandleFunc("GET /v1/bans", s.authed(s.handleListBans))
	mux.HandleFunc("PUT /v1/bans/{id}", s.authed(s.handleBan))
	mux.HandleFunc("DELETE /v1/bans/{id}", s.authed(s.handleUnban))

	mux.HandleFunc("GET /v1/roles", s.authed(s.handleListRoles))
	mux.HandleFunc("POST /v1/roles", s.authed(s.handleCreateRole))
	mux.HandleFunc("PATCH /v1/roles/{id}", s.authed(s.handleUpdateRole))
	mux.HandleFunc("DELETE /v1/roles/{id}", s.authed(s.handleDeleteRole))

	mux.HandleFunc("GET /v1/invites", s.authed(s.handleListInvites))
	mux.HandleFunc("POST /v1/invites", s.authed(s.needPerm(permCreateInvite, s.handleCreateInvite)))
	mux.HandleFunc("DELETE /v1/invites/{code}", s.authed(s.handleRevokeInvite))

	mux.HandleFunc("GET /v1/channels", s.authed(s.handleListChannels))
	mux.HandleFunc("POST /v1/channels", s.authed(s.needPerm(permManageChannels, s.handleCreateChannel)))
	mux.HandleFunc("PATCH /v1/channels/{id}", s.authed(s.handleUpdateChannel))
	mux.HandleFunc("DELETE /v1/channels/{id}", s.authed(s.handleDeleteChannel))
	mux.HandleFunc("PUT /v1/channels/{id}/overrides/{type}/{target}", s.authed(s.handleSetOverride))
	mux.HandleFunc("DELETE /v1/channels/{id}/overrides/{type}/{target}", s.authed(s.handleSetOverride))

	mux.HandleFunc("POST /v1/channels/{id}/attachments", s.authed(s.handleUpload))
	mux.HandleFunc("GET /v1/attachments/{id}/{name}", s.authed(s.handleDownload))
	mux.HandleFunc("PUT /v1/channels/{id}/messages/{mid}/reactions/{emoji}", s.authed(s.handleReaction))
	mux.HandleFunc("DELETE /v1/channels/{id}/messages/{mid}/reactions/{emoji}", s.authed(s.handleReaction))
	mux.HandleFunc("DELETE /v1/channels/{id}/messages/{mid}/reactions/{emoji}/{member}", s.authed(s.handleReaction))
	mux.HandleFunc("GET /v1/channels/{id}/pins", s.authed(s.handleListPins))
	mux.HandleFunc("PUT /v1/channels/{id}/pins/{mid}", s.authed(s.handlePin))
	mux.HandleFunc("DELETE /v1/channels/{id}/pins/{mid}", s.authed(s.handlePin))
	mux.HandleFunc("POST /v1/channels/{id}/messages/{mid}/threads", s.authed(s.handleCreateThread))
	mux.HandleFunc("POST /v1/channels/{id}/typing", s.authed(s.handleTyping))
	mux.HandleFunc("POST /v1/channels/{id}/ack", s.authed(s.handleAck))
	mux.HandleFunc("GET /v1/read-states", s.authed(s.handleReadStates))
	mux.HandleFunc("GET /v1/notification-settings", s.authed(s.handleListNotificationSettings))
	mux.HandleFunc("PUT /v1/notification-settings/{id}", s.authed(s.handleSetNotification))
	mux.HandleFunc("GET /v1/search", s.authed(s.handleSearch))

	mux.HandleFunc("GET /v1/channels/{id}/messages", s.authed(s.handleListMessages))
	mux.HandleFunc("POST /v1/channels/{id}/messages", s.authed(s.handleCreateMessage))
	mux.HandleFunc("PATCH /v1/channels/{id}/messages/{mid}", s.authed(s.handleEditMessage))
	mux.HandleFunc("DELETE /v1/channels/{id}/messages/{mid}", s.authed(s.handleDeleteMessage))
	mux.HandleFunc("POST /v1/channels/{id}/messages/bulk-delete", s.authed(s.handleBulkDelete))

	mux.HandleFunc("POST /v1/channels/{id}/voice/join", s.authed(s.handleVoiceJoin))
	mux.HandleFunc("GET /v1/voice/states", s.authed(s.handleVoiceStates))
	mux.HandleFunc("PATCH /v1/voice/state", s.authed(s.handleVoiceSelfState))
	mux.HandleFunc("POST /v1/voice/leave", s.authed(s.handleVoiceLeave))
	mux.HandleFunc("PATCH /v1/voice/states/{member}", s.authed(s.handleVoiceModerate))
	mux.HandleFunc("DELETE /v1/voice/states/{member}", s.authed(s.handleVoiceKick))
	if s.voiceOpts != nil && s.voiceOpts.Proxy != nil {
		mux.Handle("/lk/", s.voiceOpts.Proxy)
	}
	mux.Handle("GET /voice-test/", voiceTestPage)

	mux.HandleFunc("GET /v1/gateway", s.handleGateway)
	api := httpapi.CORS(mux)
	// The LiveKit proxy passes LiveKit's own CORS headers through.
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/lk/") {
			mux.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
	return s.limit.global.Wrap(s.byIP, httpapi.BodyDeadline(routed, 30*time.Second, 15*time.Minute,
		func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/attachments") },
		func(r *http.Request) bool {
			return r.URL.Path == "/v1/gateway" || strings.HasPrefix(r.URL.Path, "/lk/")
		}))
}

// InternalHandler serves the loopback-only endpoints: LiveKit webhooks,
// sent over plain HTTP since LiveKit cannot verify a self-signed certificate.
func (s *Server) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/livekit/webhook", s.handleLiveKitWebhook)
	return mux
}

type serverInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Access      string `json:"access"`
	MemberCount int    `json:"member_count"`
	// Shown before joining: what a new member will have to do.
	Rules             string `json:"rules"` // "" = no rules screen
	RequirePhone      bool   `json:"require_phone"`
	PhoneVerification bool   `json:"phone_verification"` // a provider is configured
}

func (s *Server) info(ctx context.Context) (serverInfo, error) {
	info := serverInfo{ID: s.id, PhoneVerification: s.phone != nil}
	var err error
	if info.Name, err = s.setting(ctx, "name"); err != nil {
		return info, err
	}
	if info.Access, err = s.setting(ctx, "access"); err != nil {
		return info, err
	}
	if info.Rules, err = s.setting(ctx, "rules"); err != nil {
		return info, err
	}
	phone, err := s.setting(ctx, "require_phone")
	if err != nil {
		return info, err
	}
	info.RequirePhone = phone == "1"
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
		Name         *string `json:"name"`
		Access       *string `json:"access"`
		Rules        *string `json:"rules"` // "" removes the rules screen
		RequirePhone *bool   `json:"require_phone"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	ctx := r.Context()
	changes := map[string]any{}
	if req.Rules != nil {
		rules := strings.TrimSpace(*req.Rules)
		if len([]rune(rules)) > maxRulesLength {
			writeErr(w, r, errf(http.StatusBadRequest, "invalid_rules", "rules must be at most %d characters", maxRulesLength))
			return
		}
		previous, err := s.setting(ctx, "rules")
		if err != nil {
			writeErr(w, r, err)
			return
		}
		if err := setSetting(ctx, s.db, "rules", rules); err != nil {
			writeErr(w, r, err)
			return
		}
		if previous == "" && rules != "" {
			// A new rules screen is for newcomers: current members are not locked out.
			if _, err := s.db.ExecContext(ctx, `UPDATE members SET rules_accepted_at = ? WHERE left_at IS NULL AND rules_accepted_at IS NULL`, s.nowMs()); err != nil {
				writeErr(w, r, err)
				return
			}
		}
		changes["rules"] = rules
	}
	if req.RequirePhone != nil {
		if *req.RequirePhone && s.phone == nil {
			writeErr(w, r, errf(http.StatusBadRequest, "phone_verification_unavailable", "configure a phone verification provider first (QUAREL_PHONE_VERIFY)"))
			return
		}
		v := "0"
		if *req.RequirePhone {
			v = "1"
		}
		if err := setSetting(ctx, s.db, "require_phone", v); err != nil {
			writeErr(w, r, err)
			return
		}
		changes["require_phone"] = *req.RequirePhone
	}
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
		changes["name"] = name
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
		changes["access"] = *req.Access
	}
	info, err := s.info(ctx)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if len(changes) > 0 {
		s.audit(ctx, s.db, memberFrom(r).ID, auditServerUpdate, "", "", changes)
	}
	s.hub.Broadcast("SERVER_UPDATE", info)
	if req.Rules != nil || req.RequirePhone != nil {
		s.syncPermissions(ctx) // members may now be restricted, or no longer
	}
	writeJSON(w, http.StatusOK, info)
}
