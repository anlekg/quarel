// Package adminui is the web administration interface shared by both Quarel
// servers: first password, dashboard, settings (written to the settings file
// of the data directory, environment variables win), backups, log. It runs on
// its own port, never opened on the router, and keeps working when the
// service itself cannot start, so a bad setting can always be fixed.
package adminui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/backup"
	"github.com/anlekg/quarel/internal/httpapi"
	"github.com/anlekg/quarel/internal/settings"
)

//go:embed static
var static embed.FS

// Field describes one setting shown in the settings page.
type Field struct {
	Key         string   `json:"key"` // QUAREL_* variable
	Label       string   `json:"label"`
	Help        string   `json:"help,omitempty"`
	Group       string   `json:"group"`
	Kind        string   `json:"kind"` // text, number, bool, select, secret, list
	Options     []Option `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	// ShowIf hides the field unless another field has one of these values ("KEY=a|b").
	ShowIf string `json:"show_if,omitempty"`
}

// Option is a choice of a select field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// State of the service run by the supervisor.
type State string

const (
	Starting State = "starting"
	Running  State = "running"
	Failed   State = "error"
	Stopped  State = "stopped"
)

// Options configure the interface for one service.
type Options struct {
	Product string // shown in the title
	Kind    string // "community" or "identity": enables the matching pages
	Fields  []Field
	// Validate checks the settings as currently loaded (builds the service config).
	Validate func() error
	// Restart asks the supervisor to restart the service with the current settings.
	Restart func()
	// Status returns service details for the dashboard (called while running);
	// the request tells under which address the admin reached this machine.
	Status func(r *http.Request) map[string]any
	// Backup describes the data to archive; Stop/Start bracket a restore.
	Backup         backup.Spec
	Identity       func() string // identity recorded in backups
	MaxSchema      int
	StopForRestore func() func() // stops the service; the returned func starts it again
	// API serves service-specific endpoints under /api/x/ (already authenticated).
	API  http.Handler
	Logs *Logs
}

// UI is the administration interface of one service.
type UI struct {
	opts  Options
	auth  *auth
	mu    sync.Mutex
	state State
	err   string
	since time.Time
}

// New loads the admin password of the data directory. When none is set yet,
// SetupCode returns the code to type to choose it from another machine.
func New(opts Options) (*UI, error) {
	a, err := loadAuth(settings.DataDir())
	if err != nil {
		return nil, err
	}
	return &UI{opts: opts, auth: a, state: Starting, since: time.Now()}, nil
}

// SetupCode is the one-time code for choosing the admin password remotely ("" once chosen).
func (u *UI) SetupCode() string {
	u.auth.mu.Lock()
	defer u.auth.mu.Unlock()
	return u.auth.setupCode
}

// SetState records the service state shown on the dashboard.
func (u *UI) SetState(s State, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state, u.err, u.since = s, "", time.Now()
	if err != nil {
		u.err = err.Error()
	}
}

// StateText describes the service state in French (tray menu).
func (u *UI) StateText() string {
	st, _, _ := u.current()
	switch st {
	case Running:
		return "en marche"
	case Starting:
		return "démarrage…"
	case Failed:
		return "arrêté, réglages à corriger"
	default:
		return "arrêté"
	}
}

// FirstRun reports whether the admin password is still to be chosen.
func (u *UI) FirstRun() bool { return u.auth.needsSetup() }

func (u *UI) current() (State, string, time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state, u.err, u.since
}

// Handler serves the interface and its API.
func (u *UI) Handler() http.Handler {
	mux := http.NewServeMux()
	files, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServerFS(files))
	mux.HandleFunc("GET /api/session", u.handleSession)
	mux.HandleFunc("POST /api/setup", u.handleSetup)
	mux.HandleFunc("POST /api/login", u.handleLogin)
	mux.HandleFunc("POST /api/logout", u.handleLogout)
	mux.HandleFunc("GET /api/status", u.authed(u.handleStatus))
	mux.HandleFunc("GET /api/settings", u.authed(u.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", u.authed(u.handlePutSettings))
	mux.HandleFunc("POST /api/restart", u.authed(u.handleRestart))
	mux.HandleFunc("GET /api/logs", u.authed(u.handleLogs))
	mux.HandleFunc("POST /api/password", u.authed(u.handlePassword))
	mux.HandleFunc("GET /api/backup", u.authed(u.handleBackup))
	mux.HandleFunc("POST /api/restore", u.authed(u.handleRestore))
	if u.opts.API != nil {
		x := u.authed(http.StripPrefix("/api/x", u.opts.API).ServeHTTP)
		mux.HandleFunc("GET /api/x/", x)
		mux.HandleFunc("POST /api/x/", x)
	}
	return secure(mux)
}

// secure adds protective headers and refuses cross-site writes: the session
// cookie is SameSite=Strict and writes need a custom header, which another
// site cannot send without a CORS preflight (never granted here).
func secure(h http.Handler) http.Handler {
	public := os.Getenv("QUAREL_ADMIN_PUBLIC") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !public && !fromLocalNetwork(r) {
			http.Error(w, "Administration accessible seulement depuis cette machine ou le réseau local (QUAREL_ADMIN_PUBLIC=1 pour lever cette restriction).", http.StatusForbidden)
			return
		}
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Quarel-Admin") != "1" {
				httpapi.WriteErr(w, r, httpapi.Errf(http.StatusForbidden, "forbidden", "missing X-Quarel-Admin header"))
				return
			}
			if o := r.Header.Get("Origin"); o != "" && !strings.HasSuffix(o, "://"+r.Host) {
				httpapi.WriteErr(w, r, httpapi.Errf(http.StatusForbidden, "forbidden", "cross-origin request"))
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func (u *UI) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !u.auth.valid(r) {
			httpapi.WriteErr(w, r, httpapi.Errf(http.StatusUnauthorized, "unauthorized", "sign in first"))
			return
		}
		h(w, r)
	}
}

func (u *UI) handleSession(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"product":     u.opts.Product,
		"kind":        u.opts.Kind,
		"setup":       u.auth.needsSetup(),
		"code_needed": u.auth.needsSetup() && !isLoopback(r),
		"signed_in":   u.auth.valid(r),
		"version":     backup.Version(),
	})
}

func checkNewPassword(p string) error {
	if len([]rune(p)) < minPassword || len(p) > 256 {
		return httpapi.Errf(http.StatusBadRequest, "weak_password", "at least %d characters", minPassword)
	}
	return nil
}

func (u *UI) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if err := httpapi.Decode(r, &req); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	if !u.auth.needsSetup() {
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusConflict, "already_set", "the admin password is already set"))
		return
	}
	ip := clientIP(r)
	if u.auth.throttled(ip) {
		httpapi.WriteErr(w, r, &httpapi.Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many attempts", RetryAfter: int64(failuresWindow.Seconds())})
		return
	}
	if !isLoopback(r) && !u.auth.codeOK(req.Code) {
		u.auth.failed(ip)
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusForbidden, "invalid_setup_code", "wrong setup code (see the server log)"))
		return
	}
	if err := checkNewPassword(req.Password); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	if err := u.auth.setPassword(req.Password); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	slog.Info("admin interface: password chosen", "from", ip)
	u.auth.newSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	if err := httpapi.Decode(r, &req); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	ip := clientIP(r)
	if u.auth.throttled(ip) {
		httpapi.WriteErr(w, r, &httpapi.Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many attempts", RetryAfter: int64(failuresWindow.Seconds())})
		return
	}
	if !u.auth.check(req.Password) {
		u.auth.failed(ip)
		slog.Warn("admin interface: wrong password", "from", ip)
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusUnauthorized, "invalid_credentials", "wrong password"))
		return
	}
	u.auth.newSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handleLogout(w http.ResponseWriter, r *http.Request) {
	u.auth.endSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Current, New string }
	if err := httpapi.Decode(r, &req); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	if !u.auth.check(req.Current) {
		u.auth.failed(clientIP(r))
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusUnauthorized, "invalid_credentials", "wrong password"))
		return
	}
	if err := checkNewPassword(req.New); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	if err := u.auth.setPassword(req.New); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	u.auth.newSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, errText, since := u.current()
	resp := map[string]any{
		"state":    st,
		"error":    errText,
		"since":    since.UTC(),
		"version":  backup.Version(),
		"data_dir": settings.DataDir(),
	}
	if st == Running && u.opts.Status != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		resp["service"] = u.opts.Status(r.WithContext(ctx))
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

type fieldValue struct {
	Field
	Value  string `json:"value"`  // secrets: never sent
	Set    bool   `json:"set"`    // a value exists (file or environment)
	Locked bool   `json:"locked"` // set by an environment variable
}

func (u *UI) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := make([]fieldValue, 0, len(u.opts.Fields))
	for _, f := range u.opts.Fields {
		v := settings.Get(f.Key)
		fv := fieldValue{Field: f, Set: v != "", Locked: settings.Locked(f.Key)}
		if f.Kind != "secret" {
			fv.Value = v
		}
		out = append(out, fv)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"fields": out})
}

// handlePutSettings saves the changed values, checks the resulting
// configuration (rolling back if invalid) and restarts the service.
func (u *UI) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Values map[string]*string `json:"values"` // null: back to the default
	}
	if err := httpapi.DecodeLimit(r, &req, 256<<10); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	known := map[string]Field{}
	for _, f := range u.opts.Fields {
		known[f.Key] = f
	}
	old := settings.Snapshot()
	next := settings.Snapshot()
	for k, v := range req.Values {
		f, ok := known[k]
		if !ok {
			httpapi.WriteErr(w, r, httpapi.Errf(http.StatusBadRequest, "unknown_setting", "unknown setting %s", k))
			return
		}
		if settings.Locked(k) {
			continue
		}
		if v == nil {
			delete(next, k)
			continue
		}
		val := strings.TrimSpace(*v)
		if f.Kind == "secret" && val == "" {
			continue // empty secret field: keep the current value
		}
		if strings.ContainsAny(val, "\n\r") {
			httpapi.WriteErr(w, r, httpapi.Errf(http.StatusBadRequest, "invalid_value", "%s: one line only", f.Label))
			return
		}
		next[k] = val
	}
	dir := settings.DataDir()
	if err := settings.Save(dir, next); err != nil {
		httpapi.WriteErr(w, r, err)
		return
	}
	if u.opts.Validate != nil {
		if err := u.opts.Validate(); err != nil {
			settings.Save(dir, old)
			httpapi.WriteErr(w, r, httpapi.Errf(http.StatusBadRequest, "invalid_settings", "%s", err.Error()))
			return
		}
	}
	slog.Info("admin interface: settings changed, restarting the service")
	u.opts.Restart()
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handleRestart(w http.ResponseWriter, r *http.Request) {
	slog.Info("admin interface: restart requested")
	u.opts.Restart()
	w.WriteHeader(http.StatusNoContent)
}

func (u *UI) handleLogs(w http.ResponseWriter, r *http.Request) {
	var lines []string
	if u.opts.Logs != nil {
		lines = u.opts.Logs.Lines()
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func (u *UI) handleBackup(w http.ResponseWriter, r *http.Request) {
	name := fmt.Sprintf("%s-%s.tar.gz", u.opts.Backup.Kind, time.Now().Format("2006-01-02-1504"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	identity := ""
	if u.opts.Identity != nil {
		identity = u.opts.Identity()
	}
	if _, err := backup.Create(u.opts.Backup, identity, w); err != nil {
		slog.Error("admin interface: backup failed", "err", err)
		// Headers are gone: the truncated download fails to open, which is the best signal left.
		return
	}
	slog.Info("admin interface: backup downloaded", "file", name)
}

// handleRestore stops the service, restores the uploaded archive (current
// data moved aside, never deleted) and starts the service again.
func (u *UI) handleRestore(w http.ResponseWriter, r *http.Request) {
	if u.opts.StopForRestore == nil {
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusNotImplemented, "unsupported", "restore not available"))
		return
	}
	body := http.MaxBytesReader(w, r.Body, 64<<30)
	start := u.opts.StopForRestore()
	defer start()
	m, moved, err := backup.Restore(u.opts.Backup, io.Reader(body), true, u.opts.MaxSchema)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			err = errors.New("archive too large")
		}
		httpapi.WriteErr(w, r, httpapi.Errf(http.StatusBadRequest, "restore_failed", "%s", err.Error()))
		return
	}
	slog.Info("admin interface: backup restored", "created_at", m.CreatedAt, "previous_data", moved)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"created_at": m.CreatedAt, "identity": m.Identity, "previous_data": moved})
}

// WriteJSON and WriteError are re-exported for service-specific API handlers.
var (
	WriteJSON  = httpapi.WriteJSON
	WriteError = httpapi.WriteErr
)

// DecodeJSON reads a JSON request body.
func DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v)
}

// fromLocalNetwork: loopback, private (RFC 1918, IPv6 ULA) or link-local
// client addresses. The page is never meant to be reachable from the Internet.
func fromLocalNetwork(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
