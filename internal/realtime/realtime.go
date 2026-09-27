// Package realtime is the WebSocket event gateway shared by Quarel services.
//
// Protocol (JSON text frames):
//
//	client → {"op":"auth","token":"<session token>"}   first frame, within authTimeout
//	server → {"t":"READY","d":{...}}
//	server → {"t":"<EVENT>","d":{...}}
//
// Frames sent by the client after authentication are ignored. Connections
// are registered before READY is built, so events queued meanwhile may
// repeat state already in READY: clients must apply events idempotently.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	authTimeout  = 10 * time.Second
	pingInterval = 30 * time.Second
	writeTimeout = 10 * time.Second
	sendBuffer   = 256

	// CloseInvalidSession closes connections with a missing, invalid or expired session.
	CloseInvalidSession websocket.StatusCode = 4001
)

type event struct {
	T string `json:"t"`
	D any    `json:"d"`
}

// Auth identifies an authenticated connection. Key identifies the
// subscriber (a member, a device…), Group what it belongs to (a user…).
type Auth struct {
	Key, Group string
	Expires    time.Time // zero: never
}

type conn struct {
	Auth
	send   chan []byte
	reason string // why the hub dropped this connection
	gone   bool   // no longer counted in its group
}

// Hub fans events out to connected clients.
type Hub struct {
	mu     sync.Mutex
	conns  map[*conn]struct{}
	groups map[string]int // open connections per group

	// OnGroup, if set, is called when a group gets its first connection
	// (online) or loses its last one (offline). Set it before serving.
	OnGroup func(group string, online bool)
}

func NewHub() *Hub { return &Hub{conns: map[*conn]struct{}{}, groups: map[string]int{}} }

// GroupOnline reports whether a group has at least one open connection.
func (h *Hub) GroupOnline(group string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.groups[group] > 0
}

// KeyOnline reports whether a key (a member, a bot) has an open connection.
func (h *Hub) KeyOnline(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if c.Key == key {
			return true
		}
	}
	return false
}

// Broadcast sends an event to every connection.
func (h *Hub) Broadcast(t string, d any) { h.BroadcastTo(t, d, nil) }

// BroadcastTo sends an event to the connections for which allow returns true
// (all of them if allow is nil). allow runs under the hub lock: keep it in memory.
func (h *Hub) BroadcastTo(t string, d any, allow func(key, group string) bool) {
	data, err := json.Marshal(event{T: t, D: d})
	if err != nil {
		slog.Error("encoding event", "type", t, "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if allow == nil || allow(c.Key, c.Group) {
			h.sendLocked(c, data)
		}
	}
}

// SendEachIf sends each connection its own payload, skipping those for which build returns false.
func (h *Hub) SendEachIf(t string, build func(key, group string) (any, bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		d, ok := build(c.Key, c.Group)
		if !ok {
			continue
		}
		data, err := json.Marshal(event{T: t, D: d})
		if err != nil {
			slog.Error("encoding event", "type", t, "err", err)
			return
		}
		h.sendLocked(c, data)
	}
}

// Disconnect closes the connections matching match with reason.
func (h *Hub) Disconnect(match func(key, group string) bool, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if match(c.Key, c.Group) {
			h.dropLocked(c, reason)
		}
	}
}

// CloseAll closes every connection.
func (h *Hub) CloseAll() {
	h.Disconnect(func(string, string) bool { return true }, "server shutting down")
}

func (h *Hub) sendLocked(c *conn, data []byte) {
	select {
	case c.send <- data:
	default:
		h.dropLocked(c, "client too slow")
	}
}

// dropLocked disconnects c; its writer closes the socket with reason.
func (h *Hub) dropLocked(c *conn, reason string) {
	if _, ok := h.conns[c]; !ok {
		return
	}
	delete(h.conns, c)
	c.reason = reason
	close(c.send)
}

func (h *Hub) add(c *conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.groups[c.Group]++
	first := h.groups[c.Group] == 1
	h.mu.Unlock()
	if first && h.OnGroup != nil {
		h.OnGroup(c.Group, true)
	}
}

func (h *Hub) remove(c *conn) {
	h.mu.Lock()
	delete(h.conns, c) // already gone if the hub dropped it
	if c.gone {
		h.mu.Unlock()
		return
	}
	c.gone = true
	h.groups[c.Group]--
	last := h.groups[c.Group] == 0
	if last {
		delete(h.groups, c.Group)
	}
	h.mu.Unlock()
	if last && h.OnGroup != nil {
		h.OnGroup(c.Group, false)
	}
}

// Serve upgrades the request to a WebSocket, authenticates the first frame
// with authenticate (nil Auth: rejected), sends ready's payload as READY,
// then streams events until the client leaves or the session expires.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request,
	authenticate func(ctx context.Context, token string) (*Auth, error),
	ready func(ctx context.Context, a *Auth) (any, error)) {
	// Authentication uses a token in the first frame, never cookies, so
	// accepting any origin does not expose sessions to other websites.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(4096)
	ctx := r.Context()

	actx, cancel := context.WithTimeout(ctx, authTimeout)
	var hello struct {
		Op    string `json:"op"`
		Token string `json:"token"`
	}
	err = wsjson.Read(actx, ws, &hello)
	cancel()
	if err != nil || hello.Op != "auth" {
		ws.Close(CloseInvalidSession, "first frame must be {\"op\":\"auth\",\"token\":...}")
		return
	}
	auth, err := authenticate(ctx, hello.Token)
	if err != nil || auth == nil {
		ws.Close(CloseInvalidSession, "invalid or expired session")
		return
	}

	// Register before building READY so no event is lost in between.
	c := &conn{Auth: *auth, send: make(chan []byte, sendBuffer)}
	h.add(c)
	defer h.remove(c)

	payload, err := ready(ctx, auth)
	if err != nil {
		slog.Error("building READY", "err", err)
		ws.Close(websocket.StatusInternalError, "internal error")
		return
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	err = wsjson.Write(wctx, ws, event{T: "READY", D: payload})
	cancel()
	if err != nil {
		return
	}

	ctx = ws.CloseRead(ctx) // discard client frames; ctx ends when the socket closes
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	var expiry <-chan time.Time
	if !auth.Expires.IsZero() {
		t := time.NewTimer(time.Until(auth.Expires))
		defer t.Stop()
		expiry = t.C
	}
	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				ws.Close(websocket.StatusPolicyViolation, c.reason)
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := ws.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-expiry:
			ws.Close(CloseInvalidSession, "session expired; log in again")
			return
		case <-ctx.Done():
			return
		}
	}
}
