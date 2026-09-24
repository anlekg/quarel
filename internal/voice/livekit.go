// Package voice talks to a LiveKit SFU: access tokens, room administration
// (Twirp JSON API) and webhook verification, plus supervision of an embedded
// livekit-server process.
//
// It deliberately avoids the LiveKit Go SDK, which pulls in a full WebRTC
// stack: the server only needs signed JWTs and a few JSON calls.
package voice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LiveKit is a client for one LiveKit deployment.
type LiveKit struct {
	apiURL string // e.g. http://127.0.0.1:7880
	key    string
	secret string
	client *http.Client
}

// NewLiveKit returns a client for the LiveKit server at apiURL.
func NewLiveKit(apiURL, key, secret string) *LiveKit {
	return &LiveKit{apiURL: apiURL, key: key, secret: secret, client: &http.Client{Timeout: 10 * time.Second}}
}

// grant is LiveKit's "video" claim.
type grant struct {
	RoomJoin          bool     `json:"roomJoin,omitempty"`
	RoomAdmin         bool     `json:"roomAdmin,omitempty"`
	RoomList          bool     `json:"roomList,omitempty"`
	Room              string   `json:"room,omitempty"`
	CanPublish        *bool    `json:"canPublish,omitempty"`
	CanSubscribe      *bool    `json:"canSubscribe,omitempty"`
	CanPublishData    *bool    `json:"canPublishData,omitempty"`
	CanPublishSources []string `json:"canPublishSources,omitempty"`
}

// Grant is what a participant may do in a room.
type Grant struct {
	Microphone bool // speak
	Camera     bool // share their camera
	Screen     bool // share their screen (with its sound)
	Listen     bool // receive the others' audio and video
}

// Track sources, as named in LiveKit tokens (lower case) and in its API (upper case).
const (
	SourceCamera      = "camera"
	SourceMicrophone  = "microphone"
	SourceScreen      = "screen_share"
	SourceScreenAudio = "screen_share_audio"
)

func (g Grant) sources() []string {
	var out []string
	if g.Microphone {
		out = append(out, SourceMicrophone)
	}
	if g.Camera {
		out = append(out, SourceCamera)
	}
	if g.Screen {
		out = append(out, SourceScreen, SourceScreenAudio)
	}
	return out
}

type claims struct {
	Name  string `json:"name,omitempty"`
	Video grant  `json:"video"`
	jwt.RegisteredClaims
}

func (l *LiveKit) sign(identity, name string, g grant, ttl time.Duration) (string, error) {
	now := time.Now()
	c := claims{Name: name, Video: g, RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    l.key,
		Subject:   identity,
		NotBefore: jwt.NewNumericDate(now.Add(-10 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(l.secret))
}

// JoinToken lets identity join room with the rights in g. An empty source
// list would mean "every source" to LiveKit, so publishing is off entirely
// when g allows none.
func (l *LiveKit) JoinToken(room, identity, name string, g Grant, ttl time.Duration) (string, error) {
	no := false
	sources := g.sources()
	canPublish := len(sources) > 0
	return l.sign(identity, name, grant{RoomJoin: true, Room: room, CanPublish: &canPublish, CanPublishSources: sources,
		CanSubscribe: &g.Listen, CanPublishData: &no}, ttl)
}

// call invokes a RoomService method through Twirp's JSON protocol.
func (l *LiveKit) call(ctx context.Context, method string, g grant, in, out any) error {
	tok, err := l.sign("quarel-server", "", g, time.Minute)
	if err != nil {
		return err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", l.apiURL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("livekit %s: %w", method, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var te struct{ Code, Msg string }
		json.Unmarshal(data, &te)
		if te.Code == "not_found" {
			return ErrNotFound
		}
		return fmt.Errorf("livekit %s: HTTP %d %s %s", method, resp.StatusCode, te.Code, te.Msg)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ErrNotFound is returned when the room or participant does not exist.
var ErrNotFound = errors.New("livekit: not found")

// RemoveParticipant disconnects identity from room.
func (l *LiveKit) RemoveParticipant(ctx context.Context, room, identity string) error {
	return l.call(ctx, "RemoveParticipant", grant{RoomAdmin: true, Room: room},
		map[string]string{"room": room, "identity": identity}, nil)
}

// SetPermissions changes identity's rights in room while connected; LiveKit
// unpublishes the tracks no longer allowed.
func (l *LiveKit) SetPermissions(ctx context.Context, room, identity string, g Grant) error {
	sources := []string{}
	for _, s := range g.sources() {
		sources = append(sources, strings.ToUpper(s))
	}
	return l.call(ctx, "UpdateParticipant", grant{RoomAdmin: true, Room: room}, map[string]any{
		"room":     room,
		"identity": identity,
		"permission": map[string]any{"can_subscribe": g.Listen, "can_publish": len(sources) > 0,
			"can_publish_sources": sources, "can_publish_data": false},
	}, nil)
}

// Participants lists the identities connected to each room.
func (l *LiveKit) Participants(ctx context.Context) (map[string][]string, error) {
	var rooms struct {
		Rooms []struct{ Name string } `json:"rooms"`
	}
	if err := l.call(ctx, "ListRooms", grant{RoomList: true}, map[string]any{}, &rooms); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rooms.Rooms {
		var ps struct {
			Participants []struct{ Identity string } `json:"participants"`
		}
		if err := l.call(ctx, "ListParticipants", grant{RoomAdmin: true, Room: r.Name}, map[string]string{"room": r.Name}, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps.Participants {
			out[r.Name] = append(out[r.Name], p.Identity)
		}
	}
	return out, nil
}

// Event is the part of a LiveKit webhook the server uses.
type Event struct {
	Type     string // participant_joined, participant_left, room_finished, track_published, track_unpublished…
	Room     string
	Identity string
	Source   string // track events: SourceCamera, SourceMicrophone, SourceScreen or SourceScreenAudio
}

// trackSources maps LiveKit's TrackSource enum (by name or number) to our names.
var trackSources = map[string]string{
	"CAMERA": SourceCamera, "1": SourceCamera,
	"MICROPHONE": SourceMicrophone, "2": SourceMicrophone,
	"SCREEN_SHARE": SourceScreen, "3": SourceScreen,
	"SCREEN_SHARE_AUDIO": SourceScreenAudio, "4": SourceScreenAudio,
}

// ReceiveWebhook verifies and decodes a LiveKit webhook request: the
// Authorization header is a JWT signed with the API secret, whose "sha256"
// claim is the base64 SHA-256 of the body.
func (l *LiveKit) ReceiveWebhook(r *http.Request) (Event, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return Event{}, err
	}
	var c struct {
		SHA256 string `json:"sha256"`
		jwt.RegisteredClaims
	}
	_, err = jwt.ParseWithClaims(r.Header.Get("Authorization"), &c, func(*jwt.Token) (any, error) { return []byte(l.secret), nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(l.key), jwt.WithLeeway(time.Minute))
	if err != nil {
		return Event{}, fmt.Errorf("webhook signature: %w", err)
	}
	sum := sha256.Sum256(body)
	if c.SHA256 != base64.StdEncoding.EncodeToString(sum[:]) {
		return Event{}, errors.New("webhook body does not match its signature")
	}
	var ev struct {
		Event       string `json:"event"`
		Room        struct{ Name string }
		Participant struct{ Identity string }
		Track       struct {
			Source json.RawMessage `json:"source"`
		}
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return Event{}, err
	}
	return Event{Type: ev.Event, Room: ev.Room.Name, Identity: ev.Participant.Identity,
		Source: trackSources[strings.Trim(string(ev.Track.Source), `"`)]}, nil
}
