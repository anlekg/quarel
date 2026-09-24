package adminui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anlekg/quarel/internal/secret"
	"golang.org/x/crypto/argon2"
)

const (
	adminFile      = "admin.json"
	cookieName     = "quarel_admin"
	sessionTTL     = 12 * time.Hour
	minPassword    = 10
	maxFailures    = 10
	failuresWindow = 15 * time.Minute
)

type adminData struct {
	Password string `json:"password"` // argon2id, PHC format
}

type auth struct {
	dir       string
	mu        sync.Mutex
	hash      string
	setupCode string // needed to choose the password from another machine
	sessions  map[string]time.Time
	failures  map[string][]time.Time
}

func loadAuth(dir string) (*auth, error) {
	a := &auth{dir: dir, sessions: map[string]time.Time{}, failures: map[string][]time.Time{}}
	data, err := os.ReadFile(filepath.Join(dir, adminFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		a.setupCode = strings.ToLower(secret.NewID()[:10])
	case err != nil:
		return nil, err
	default:
		var d adminData
		if err := json.Unmarshal(data, &d); err != nil || d.Password == "" {
			return nil, fmt.Errorf("%s: invalid file (delete it to choose a new admin password)", adminFile)
		}
		a.hash = d.Password
	}
	return a, nil
}

func (a *auth) needsSetup() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hash == ""
}

func (a *auth) setPassword(password string) error {
	h := hashPassword(password)
	data, _ := json.Marshal(adminData{Password: h})
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(a.dir, adminFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(a.dir, adminFile)); err != nil {
		return err
	}
	a.mu.Lock()
	a.hash, a.setupCode = h, ""
	a.sessions = map[string]time.Time{} // a new password closes every session
	a.mu.Unlock()
	return nil
}

func (a *auth) check(password string) bool {
	a.mu.Lock()
	h := a.hash
	a.mu.Unlock()
	return h != "" && verifyPassword(password, h)
}

// throttled reports whether ip made too many failed attempts recently.
func (a *auth) throttled(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := time.Now().Add(-failuresWindow)
	recent := a.failures[ip][:0]
	for _, t := range a.failures[ip] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	a.failures[ip] = recent
	return len(recent) >= maxFailures
}

func (a *auth) failed(ip string) {
	a.mu.Lock()
	a.failures[ip] = append(a.failures[ip], time.Now())
	a.mu.Unlock()
}

func (a *auth) newSession(w http.ResponseWriter) {
	token := secret.NewToken()
	a.mu.Lock()
	a.sessions[secret.SHA256Hex(token)] = time.Now().Add(sessionTTL)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds())})
}

func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	key := secret.SHA256Hex(c.Value)
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[key]
	if ok && time.Now().After(exp) {
		delete(a.sessions, key)
		return false
	}
	return ok
}

func (a *auth) endSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.mu.Lock()
		delete(a.sessions, secret.SHA256Hex(c.Value))
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (a *auth) codeOK(code string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.setupCode != "" && subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(code))), []byte(a.setupCode)) == 1
}

// isLoopback: requests from this machine may choose the first password without the setup code.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// argon2id, lighter than for user accounts: a single admin, rare logins.
func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, 2, 32*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, 32*1024, 2, 2,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &t, &threads); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// PendingSetup reports, before starting, whether the admin password of dir is
// still to be chosen (the Windows tray opens the page on first run).
func PendingSetup(dir string) (string, bool) {
	_, err := os.Stat(filepath.Join(dir, adminFile))
	return filepath.Join(dir, adminFile), errors.Is(err, fs.ErrNotExist)
}
