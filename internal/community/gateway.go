package community

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

// Gateway protocol (GET /v1/gateway, WebSocket, JSON text frames):
//
//	client → {"op":"auth","token":"<session token>"}   first frame, within authTimeout
//	server → {"t":"READY","d":{member, server, channels, members}}
//	server → {"t":"<EVENT>","d":{...}}                 MESSAGE_CREATE, CHANNEL_UPDATE…
//
// Events are also delivered for changes the client made itself. After READY,
// frames sent by the client are ignored. Events queued while READY was being
// built may repeat state already in READY: clients must apply them idempotently.

const (
	authTimeout  = 10 * time.Second
	pingInterval = 30 * time.Second
	writeTimeout = 10 * time.Second
	sendBuffer   = 256

	closeInvalidSession websocket.StatusCode = 4001
)

type event struct {
	T string `json:"t"`
	D any    `json:"d"`
}

type wsConn struct {
	memberID string
	send     chan []byte
	reason   string // why the hub dropped this connection
}

// hub fans events out to connected members.
type hub struct {
	mu    sync.Mutex
	conns map[*wsConn]struct{}
}

func newHub() *hub { return &hub{conns: map[*wsConn]struct{}{}} }

func (h *hub) add(c *wsConn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *wsConn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

// dropLocked disconnects c; its writer closes the socket with reason.
func (h *hub) dropLocked(c *wsConn, reason string) {
	if _, ok := h.conns[c]; !ok {
		return
	}
	delete(h.conns, c)
	c.reason = reason
	close(c.send)
}

func (h *hub) broadcast(t string, d any) {
	data, err := json.Marshal(event{T: t, D: d})
	if err != nil {
		slog.Error("encoding event", "type", t, "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		select {
		case c.send <- data:
		default:
			h.dropLocked(c, "client too slow")
		}
	}
}

func (h *hub) disconnectMember(memberID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if c.memberID == memberID {
			h.dropLocked(c, "no longer a member")
		}
	}
}

func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		h.dropLocked(c, "server shutting down")
	}
}

func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	// Authentication uses a token in the first frame, never cookies, so
	// accepting any origin does not expose sessions to other websites.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4096)
	ctx := r.Context()

	actx, cancel := context.WithTimeout(ctx, authTimeout)
	var hello struct {
		Op    string `json:"op"`
		Token string `json:"token"`
	}
	err = wsjson.Read(actx, conn, &hello)
	cancel()
	if err != nil || hello.Op != "auth" {
		conn.Close(closeInvalidSession, "first frame must be {\"op\":\"auth\",\"token\":...}")
		return
	}
	m, expires, err := s.memberForToken(ctx, hello.Token)
	if err != nil || m == nil {
		conn.Close(closeInvalidSession, "invalid or expired session")
		return
	}

	// Register before building READY so no event is lost in between.
	c := &wsConn{memberID: m.ID, send: make(chan []byte, sendBuffer)}
	s.hub.add(c)
	defer s.hub.remove(c)

	ready, err := s.readyPayload(ctx, m)
	if err != nil {
		slog.Error("building READY", "err", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	err = wsjson.Write(wctx, conn, event{T: "READY", D: ready})
	cancel()
	if err != nil {
		return
	}

	ctx = conn.CloseRead(ctx) // discard client frames; ctx ends when the socket closes
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	expiry := time.NewTimer(time.Until(expires))
	defer expiry.Stop()
	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				conn.Close(websocket.StatusPolicyViolation, c.reason)
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-expiry.C:
			conn.Close(closeInvalidSession, "session expired; log in again")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) readyPayload(ctx context.Context, m *member) (map[string]any, error) {
	info, err := s.info(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := s.allChannels(ctx)
	if err != nil {
		return nil, err
	}
	members, err := s.activeMembers(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"member": m.json(), "server": info, "channels": channels, "members": members}, nil
}
