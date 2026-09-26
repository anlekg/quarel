// Command pingbot is a minimal Quarel bot: it answers "!ping" with "pong"
// in every channel it can read and write. It shows the whole bot API: token
// authentication, the real-time gateway, and REST calls.
//
//	QUAREL_URL=https://mon-serveur:8090 QUAREL_BOT_TOKEN=qb_… QUAREL_SERVER_ID=… go run ./examples/pingbot
//
// QUAREL_SERVER_ID (printed by "quarelctl bot-create") pins a server using a
// self-signed certificate; it is not needed with a certificate from an authority.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/anlekg/quarel/pkg/tlsbind"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type bot struct {
	base, token string
	http        *http.Client
	me          string // the bot's member ID, from READY
}

func main() {
	b := &bot{base: strings.TrimRight(os.Getenv("QUAREL_URL"), "/"), token: os.Getenv("QUAREL_BOT_TOKEN")}
	if b.base == "" || b.token == "" {
		log.Fatal("QUAREL_URL and QUAREL_BOT_TOKEN are required")
	}
	u, err := url.Parse(b.base)
	if err != nil {
		log.Fatal("QUAREL_URL: ", err)
	}
	b.http = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		TLSClientConfig: tlsbind.NewVerifier(u.Hostname(), os.Getenv("QUAREL_SERVER_ID")).Config(),
	}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for delay := time.Second; ctx.Err() == nil; delay = min(delay*2, time.Minute) {
		err := b.run(ctx)
		if ctx.Err() != nil {
			return
		}
		log.Printf("disconnected (%v), reconnecting in %s", err, delay)
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
	}
}

// run keeps one gateway connection open and handles its events.
func (b *bot) run(ctx context.Context) error {
	ws := "ws" + strings.TrimPrefix(b.base, "http") + "/v1/gateway"
	conn, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPClient: b.http})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)
	// 1. The first message authenticates (the bot token works like a session token).
	if err := wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": b.token}); err != nil {
		return err
	}
	for {
		var ev struct {
			T string          `json:"t"`
			D json.RawMessage `json:"d"`
		}
		if err := wsjson.Read(ctx, conn, &ev); err != nil {
			return err
		}
		switch ev.T {
		case "READY": // 2. The server sends the initial state.
			var d struct {
				Member struct {
					ID          string `json:"id"`
					DisplayName string `json:"display_name"`
				} `json:"member"`
				Server struct {
					Name string `json:"name"`
				} `json:"server"`
			}
			json.Unmarshal(ev.D, &d)
			b.me = d.Member.ID
			log.Printf("connected to %q as %s", d.Server.Name, d.Member.DisplayName)
		case "MESSAGE_CREATE": // 3. Then events, as they happen.
			var m struct {
				ID        int64  `json:"id"`
				ChannelID int64  `json:"channel_id"`
				AuthorID  string `json:"author_id"`
				Content   string `json:"content"`
			}
			json.Unmarshal(ev.D, &m)
			if m.AuthorID != b.me && strings.TrimSpace(m.Content) == "!ping" {
				// Writes go through the REST API.
				err := b.call(ctx, "POST", fmt.Sprintf("/v1/channels/%d/messages", m.ChannelID),
					map[string]any{"content": "pong", "reply_to": m.ID})
				if err != nil {
					log.Printf("reply failed: %v", err)
				}
			}
		}
	}
}

func (b *bot) call(ctx context.Context, method, path string, body any) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Code, Message string }
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusTooManyRequests {
			// Respect Retry-After before trying again (not done here, for brevity).
			log.Printf("rate limited, retry after %ss", resp.Header.Get("Retry-After"))
		}
		return fmt.Errorf("%d %s: %s", resp.StatusCode, e.Error.Code, e.Error.Message)
	}
	return nil
}
