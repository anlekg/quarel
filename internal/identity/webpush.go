package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anlekg/quarel/internal/netguard"
	"github.com/anlekg/quarel/internal/settings"
)

// Web Push (P2, decided by the PM: "empty wake-up"). The web app (PWA) of a
// device that is not connected gets an EMPTY push through its browser's push
// service when something arrives in its inbox from someone else (not what its
// own devices send it): no content, no sender, no conversation — the push
// service only learns that something happened. The service worker then shows
// a notification without details. Pushes are signed with this service's VAPID
// key (RFC 8292, data/vapid.key) and sent only to the browsers' push services
// (knownPushHosts, port 443; QUAREL_PUSH_HOSTS adds others) at public IPs: an
// endpoint cannot make this service call anything else. At most one per
// device every pushInterval; a subscription the push service no longer knows
// (404, 410) is deleted.

const pushInterval = 30 * time.Second

// knownPushHosts are the push services of the browsers (and their subdomains):
// Chrome, Edge (Chromium), Opera, Samsung Internet: FCM; Firefox: Mozilla
// autopush; Safari: Apple; Edge (legacy): WNS.
var knownPushHosts = []string{"fcm.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com"}

// pushEndpointOK says whether this service may send pushes to endpoint.
func (s *Server) pushEndpointOK(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(endpoint) > 1024 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	match := func(list []string) bool {
		for _, h := range list {
			if host == h || strings.HasSuffix(host, "."+h) {
				return true
			}
		}
		return false
	}
	if match(s.cfg.PushHosts) {
		return true
	}
	return match(knownPushHosts) && (u.Port() == "" || u.Port() == "443")
}

type webPush struct {
	once     sync.Once
	key      *ecdsa.PrivateKey
	err      error
	mu       sync.Mutex
	lastSent map[string]time.Time // by device
	client   *http.Client
}

func (s *Server) vapidKey() (*ecdsa.PrivateKey, error) {
	s.webpush.once.Do(func() {
		path := filepath.Join(s.cfg.DataDir, "vapid.key")
		if data, err := os.ReadFile(path); err == nil {
			block, _ := pem.Decode(data)
			if block == nil {
				s.webpush.err = errors.New("vapid.key: not PEM")
				return
			}
			k, err := x509.ParseECPrivateKey(block.Bytes)
			s.webpush.key, s.webpush.err = k, err
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			s.webpush.err = err
			return
		}
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			s.webpush.err = err
			return
		}
		der, _ := x509.MarshalECPrivateKey(k)
		if err := os.MkdirAll(s.cfg.DataDir, 0o700); err != nil {
			s.webpush.err = err
			return
		}
		s.webpush.err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
		s.webpush.key = k
	})
	return s.webpush.key, s.webpush.err
}

func vapidPublic(k *ecdsa.PrivateKey) string {
	pub, _ := k.PublicKey.ECDH()
	return b64url.EncodeToString(pub.Bytes()) // uncompressed point, as PushManager.subscribe wants
}

// handlePushKey: GET /v1/push/key → {key}: the applicationServerKey of subscriptions.
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	k, err := s.vapidKey()
	if err != nil {
		writeErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]string{"key": vapidPublic(k)})
}

// handleSetPush: PUT /v1/me/push {endpoint} (this device), DELETE to stop.
func (s *Server) handleSetPush(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	ctx := r.Context()
	if r.Method == http.MethodDelete {
		s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id = ?`, sess.ID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	if !s.pushEndpointOK(req.Endpoint) {
		writeErr(w, r, errf(http.StatusBadRequest, "invalid_endpoint", "a push endpoint is an https address of a browser's push service"))
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO push_subscriptions (session_id, endpoint, created_at) VALUES (?, ?, ?)
		ON CONFLICT (session_id) DO UPDATE SET endpoint = excluded.endpoint`, sess.ID, req.Endpoint, s.now().Unix()); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wake sends an empty push to a device that is not connected, if it asked
// for it, unless everything that arrived comes from its own account (another
// of its devices syncing: nothing to see).
func (s *Server) wake(deviceID string, senders []string) {
	if s.hub.KeyOnline(deviceID) {
		return
	}
	var owner string
	if err := s.db.QueryRow(`SELECT s.user_id FROM push_subscriptions p JOIN sessions s ON s.id = p.session_id WHERE p.session_id = ?`, deviceID).Scan(&owner); err != nil {
		return // no subscription
	}
	others := false
	for _, u := range senders {
		others = others || u != owner
	}
	if !others {
		return
	}
	now := s.now()
	s.webpush.mu.Lock()
	if s.webpush.lastSent == nil {
		s.webpush.lastSent = map[string]time.Time{}
	}
	if now.Sub(s.webpush.lastSent[deviceID]) < pushInterval {
		s.webpush.mu.Unlock()
		return
	}
	s.webpush.lastSent[deviceID] = now
	if len(s.webpush.lastSent) > 100000 {
		s.webpush.lastSent = map[string]time.Time{deviceID: now}
	}
	s.webpush.mu.Unlock()
	go s.sendPush(context.Background(), deviceID)
}

func (s *Server) sendPush(ctx context.Context, deviceID string) {
	var endpoint string
	if err := s.db.QueryRowContext(ctx, `SELECT endpoint FROM push_subscriptions WHERE session_id = ?`, deviceID).Scan(&endpoint); err != nil {
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil || !s.pushEndpointOK(endpoint) { // subscribed before the list of push services
		s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id = ?`, deviceID)
		return
	}
	k, err := s.vapidKey()
	if err != nil {
		slog.Error("web push key", "err", err)
		return
	}
	jwt, err := vapidJWT(k, u.Scheme+"://"+u.Host, "https://"+s.cfg.Issuer, s.now())
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	req.Header.Set("TTL", "3600")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+vapidPublic(k))
	resp, err := s.pushClient().Do(req)
	if err != nil {
		slog.Warn("web push", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		s.db.ExecContext(context.Background(), `DELETE FROM push_subscriptions WHERE session_id = ? AND endpoint = ?`, deviceID, endpoint)
	}
}

// pushClient reaches public addresses only (never this host's network), no proxy.
func (s *Server) pushClient() *http.Client {
	s.webpush.mu.Lock()
	defer s.webpush.mu.Unlock()
	if s.webpush.client == nil {
		allowPrivate := settings.Get("QUAREL_PUSH_ALLOW_PRIVATE") == "1" // tests only
		d := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || (!netguard.Public(ip) && !allowPrivate) {
				return fmt.Errorf("push endpoint %s is not a public address", host)
			}
			return nil
		}}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.DialContext = d.DialContext
		s.webpush.client = &http.Client{Transport: tr, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return s.webpush.client
}

// vapidJWT signs the ES256 token of RFC 8292 for one push service.
func vapidJWT(k *ecdsa.PrivateKey, audience, subject string, now time.Time) (string, error) {
	header := b64url.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": audience, "exp": now.Add(12 * time.Hour).Unix(), "sub": subject})
	signing := header + "." + b64url.EncodeToString(claims)
	h := sha256.Sum256([]byte(signing))
	r, sv, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return signing + "." + b64url.EncodeToString(sig), nil
}
