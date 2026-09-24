package community

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/voice"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (c *community) trackWebhook(typ string, channelID int64, identity, source string) {
	c.t.Helper()
	body, _ := json.Marshal(voice.Event{Type: typ, Room: roomName(channelID), Identity: identity, Source: source})
	req, _ := http.NewRequest("POST", c.http.URL+"/internal/livekit/webhook", bytes.NewReader(body))
	req.Header.Set("Authorization", "signed")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
}

func TestVideoAndScreen(t *testing.T) {
	c, fv := newVoiceCommunity(t, "bob")
	a := c.channelID(c.owner, "Général")
	bob := c.id("bob")
	var join struct {
		CanSpeak  bool `json:"can_speak"`
		CanStream bool `json:"can_stream"`
	}
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", a, "/voice/join"), c.tok("bob"), nil, &join))
	if !join.CanSpeak || !join.CanStream {
		t.Fatalf("join = %+v (stream is granted to @everyone)", join)
	}
	c.expect(200, "", c.webhook("participant_joined", a, bob))
	c.trackWebhook("track_published", a, bob, voice.SourceCamera)
	c.trackWebhook("track_published", a, bob, voice.SourceScreen)
	if st := c.voiceStates(c.owner)[bob]; !st.Video || !st.Screen {
		t.Fatalf("state = %+v", st)
	}
	c.trackWebhook("track_unpublished", a, bob, voice.SourceScreen)
	if st := c.voiceStates(c.owner)[bob]; !st.Video || st.Screen {
		t.Fatalf("after stopping the screen share: %+v", st)
	}

	// Losing stream stops camera and screen, not the microphone.
	c.expect(200, "", c.override(c.owner, a, "member", bob, nil, []string{"stream"}))
	g, ok := fv.grant(roomName(a), bob)
	if !ok || g.Camera || g.Screen || !g.Microphone || !g.Listen {
		t.Fatalf("grant after losing stream = %+v", g)
	}
	if st := c.voiceStates(c.owner)[bob]; st.Video || st.CanStream || !st.CanSpeak {
		t.Fatalf("state after losing stream = %+v", st)
	}
	// A camera LiveKit would still report is not shown without the right.
	c.trackWebhook("track_published", a, bob, voice.SourceCamera)
	if c.voiceStates(c.owner)[bob].Video {
		t.Fatal("camera shown without the stream permission")
	}
}

func TestVoiceModeration(t *testing.T) {
	c, fv := newVoiceCommunity(t, "mod", "bob", "carol")
	a := c.channelID(c.owner, "Général")
	var b channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "voice", "name": "Réunion"}, &b))
	mod, bob := c.id("mod"), c.id("bob")
	modRole := c.createRole(c.owner, "Modo vocal", "mute_members", "deafen_members", "move_members")
	c.expect(200, "", c.assign(c.owner, mod, modRole))
	c.expect(200, "", c.webhook("participant_joined", a, bob))
	moderate := func(token, who string, body map[string]any) result {
		return c.call("PATCH", "/v1/voice/states/"+who, token, body, nil)
	}

	c.expect(403, "missing_permissions", moderate(c.tok("carol"), bob, map[string]any{"mute": true}))
	c.expect(403, "role_hierarchy", moderate(c.tok("mod"), c.owner0(), map[string]any{"mute": true}))

	// Server mute: the microphone is blocked in LiveKit and shown to everyone.
	c.expect(200, "", moderate(c.tok("mod"), bob, map[string]any{"mute": true, "reason": "larsen"}))
	if g, _ := fv.grant(roomName(a), bob); g.Microphone || !g.Listen || !g.Camera {
		t.Fatalf("grant after server mute = %+v", g)
	}
	if st := c.voiceStates(c.tok("carol"))[bob]; !st.ServerMute || st.CanSpeak {
		t.Fatalf("state = %+v", st)
	}
	// It persists across voice sessions.
	c.expect(200, "", c.webhook("participant_left", a, bob))
	var join struct {
		CanSpeak   bool `json:"can_speak"`
		ServerMute bool `json:"server_mute"`
	}
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", a, "/voice/join"), c.tok("bob"), nil, &join))
	if join.CanSpeak || !join.ServerMute {
		t.Fatalf("rejoin = %+v", join)
	}
	c.expect(200, "", c.webhook("participant_joined", a, bob))
	if st := c.voiceStates(c.owner)[bob]; !st.ServerMute || st.CanSpeak {
		t.Fatalf("state after rejoining = %+v", st)
	}
	// Deafen: hears nothing.
	c.expect(200, "", moderate(c.tok("mod"), bob, map[string]any{"deaf": true, "mute": false}))
	if g, _ := fv.grant(roomName(a), bob); g.Listen || !g.Microphone {
		t.Fatalf("grant after deafen = %+v", g)
	}
	c.expect(200, "", moderate(c.tok("mod"), bob, map[string]any{"deaf": false}))

	// Move: bob leaves this room and his client is told where to go.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.http.URL, "http")+"/v1/gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": c.tok("bob")})
	c.expect(403, "target_cannot_connect", func() result {
		c.expect(200, "", c.override(c.owner, b.ID, "member", bob, nil, []string{"connect"}))
		defer c.call("DELETE", fmt.Sprintf("/v1/channels/%d/overrides/member/%s", b.ID, bob), c.owner, nil, nil)
		return moderate(c.tok("mod"), bob, map[string]any{"channel_id": b.ID})
	}())
	c.expect(400, "not_voice_channel", moderate(c.tok("mod"), bob, map[string]any{"channel_id": c.channelID(c.owner, "général")}))
	c.expect(200, "", moderate(c.tok("mod"), bob, map[string]any{"channel_id": b.ID}))
	if !fv.wasRemoved(roomName(a), bob) {
		t.Fatal("moved member kept in the old room")
	}
	for {
		var ev struct {
			T string
			D struct {
				ChannelID     int64 `json:"channel_id"`
				FromChannelID int64 `json:"from_channel_id"`
			}
		}
		if err := wsjson.Read(ctx, conn, &ev); err != nil {
			t.Fatal("no VOICE_MOVE:", err)
		}
		if ev.T == "VOICE_MOVE" {
			if ev.D.ChannelID != b.ID || ev.D.FromChannelID != a {
				t.Fatalf("VOICE_MOVE = %+v", ev.D)
			}
			break
		}
	}

	// Disconnect.
	c.expect(200, "", c.webhook("participant_joined", b.ID, bob))
	c.expect(403, "missing_permissions", c.call("DELETE", "/v1/voice/states/"+bob, c.tok("carol"), nil, nil))
	c.expect(204, "", c.call("DELETE", "/v1/voice/states/"+bob, c.tok("mod"), nil, nil))
	if !fv.wasRemoved(roomName(b.ID), bob) {
		t.Fatal("not disconnected")
	}
	c.expect(409, "not_in_voice", c.call("DELETE", "/v1/voice/states/"+bob, c.tok("mod"), nil, nil))

	got := c.auditActions(c.owner, "?target_id="+bob)
	want := []string{auditVoiceDisconnect, auditVoiceMove, auditVoiceDeafen, auditVoiceDeafen, auditVoiceMute, auditVoiceMute}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("audit = %v\nwant %v", got, want)
	}
}
