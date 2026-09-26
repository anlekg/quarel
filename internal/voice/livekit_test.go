package voice

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJoinToken(t *testing.T) {
	l := NewLiveKit("http://unused", "APIkey", "secret-secret-secret")
	parse := func(g Grant) claims {
		tok, err := l.JoinToken("channel-4", "member1", "Alice", g, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		var c claims
		if _, err := jwt.ParseWithClaims(tok, &c, func(*jwt.Token) (any, error) { return []byte("secret-secret-secret"), nil }); err != nil {
			t.Fatal(err)
		}
		return c
	}
	// Listening only: no publishing at all (an empty source list would mean "any source").
	c := parse(Grant{Listen: true})
	if c.Issuer != "APIkey" || c.Subject != "member1" || c.Name != "Alice" || !c.Video.RoomJoin || c.Video.Room != "channel-4" ||
		c.Video.CanPublish == nil || *c.Video.CanPublish || c.Video.CanSubscribe == nil || !*c.Video.CanSubscribe {
		t.Fatalf("claims = %+v", c)
	}
	c = parse(Grant{Microphone: true, Screen: true})
	if !*c.Video.CanPublish || *c.Video.CanSubscribe || fmt.Sprint(c.Video.CanPublishSources) != "[microphone screen_share screen_share_audio]" {
		t.Fatalf("claims = %+v", c.Video)
	}
}

func webhookRequest(t *testing.T, key, secret string, body []byte) *http.Request {
	sum := sha256.Sum256(body)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": key, "exp": time.Now().Add(time.Minute).Unix(), "sha256": base64.StdEncoding.EncodeToString(sum[:]),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r.Header.Set("Authorization", tok)
	return r
}

func TestReceiveWebhook(t *testing.T) {
	l := NewLiveKit("http://unused", "APIkey", "s3cret")
	track, _ := json.Marshal(map[string]any{"event": "track_published", "room": map[string]string{"name": "channel-4"},
		"participant": map[string]string{"identity": "m1"}, "track": map[string]any{"sid": "TR_x", "source": "SCREEN_SHARE"}})
	if ev, err := l.ReceiveWebhook(webhookRequest(t, "APIkey", "s3cret", track)); err != nil || ev.Source != SourceScreen {
		t.Fatalf("track event = %+v, %v", ev, err)
	}
	body, _ := json.Marshal(map[string]any{"event": "participant_joined", "room": map[string]string{"name": "channel-4"},
		"participant": map[string]string{"identity": "m1"}})

	ev, err := l.ReceiveWebhook(webhookRequest(t, "APIkey", "s3cret", body))
	if err != nil || ev != (Event{Type: "participant_joined", Room: "channel-4", Identity: "m1"}) {
		t.Fatalf("event = %+v, err = %v", ev, err)
	}
	if _, err := l.ReceiveWebhook(webhookRequest(t, "APIkey", "wrong", body)); err == nil {
		t.Error("webhook signed with another secret accepted")
	}
	r := webhookRequest(t, "APIkey", "s3cret", body)
	r.Body = http.NoBody
	r2 := httptest.NewRequest("POST", "/", bytes.NewReader(append(body, ' ')))
	r2.Header = r.Header
	if _, err := l.ReceiveWebhook(r2); err == nil {
		t.Error("tampered webhook body accepted")
	}
}

func TestRoomServiceCalls(t *testing.T) {
	var got []string
	var perm map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c claims
		_, err := jwt.ParseWithClaims(r.Header.Get("Authorization")[len("Bearer "):], &c, func(*jwt.Token) (any, error) { return []byte("s"), nil })
		if err != nil || !(c.Video.RoomAdmin || c.Video.RoomList) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		got = append(got, r.URL.Path)
		switch r.URL.Path {
		case "/twirp/livekit.RoomService/UpdateParticipant":
			perm = in["permission"].(map[string]any)
			w.Write([]byte(`{}`))
		case "/twirp/livekit.RoomService/ListRooms":
			w.Write([]byte(`{"rooms":[{"name":"channel-4"}]}`))
		case "/twirp/livekit.RoomService/ListParticipants":
			w.Write([]byte(`{"participants":[{"identity":"m1"},{"identity":"m2"}]}`))
		case "/twirp/livekit.RoomService/RemoveParticipant":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"code":"not_found","msg":"participant not found"}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	l := NewLiveKit(srv.URL, "k", "s")
	ctx := t.Context()
	ps, err := l.Participants(ctx)
	if err != nil || len(ps["channel-4"]) != 2 {
		t.Fatalf("participants = %v, %v", ps, err)
	}
	if err := l.SetPermissions(ctx, "channel-4", "m1", Grant{Microphone: true, Camera: true}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(perm["can_publish_sources"]) != "[MICROPHONE CAMERA]" || perm["can_publish"] != true || perm["can_subscribe"] != false {
		t.Fatalf("permission sent = %v", perm)
	}
	if err := l.RemoveParticipant(ctx, "channel-4", "gone"); err != ErrNotFound {
		t.Fatalf("remove missing participant: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("calls = %v", got)
	}
}

// Only LiveKit's signalling goes through the community server's port.
func TestProxyPaths(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path)) }))
	defer backend.Close()
	port, _ := strconv.Atoi(backend.URL[strings.LastIndex(backend.URL, ":")+1:])
	front := httptest.NewServer(Proxy("/lk", port))
	defer front.Close()
	for path, want := range map[string]int{
		"/lk/rtc": 200, "/lk/rtc/validate": 200, "/lk/rtc/v1": 200, "/lk/rtc/v1/validate": 200,
		"/lk/twirp/livekit.RoomService/ListRooms": 404, "/lk/": 404, "/lk/rtcx": 404, "/lk/rtc/../twirp/x": 404,
	} {
		res, err := http.Get(front.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, res.StatusCode, want)
		}
	}
}
