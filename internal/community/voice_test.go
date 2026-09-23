package community

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/voice"
)

// fakeVoice records calls instead of talking to LiveKit.
type fakeVoice struct {
	mu        sync.Mutex
	removed   []string // "room/identity"
	published map[string]bool
}

func (f *fakeVoice) JoinToken(room, identity, name string, canPublish bool, ttl time.Duration) (string, error) {
	return fmt.Sprintf("%s|%s|%s|%v", room, identity, name, canPublish), nil
}

func (f *fakeVoice) RemoveParticipant(_ context.Context, room, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, room+"/"+identity)
	return nil
}

func (f *fakeVoice) SetCanPublish(_ context.Context, room, identity string, can bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published[room+"/"+identity] = can
	return nil
}

func (f *fakeVoice) Participants(context.Context) (map[string][]string, error) { return nil, nil }

func (f *fakeVoice) ReceiveWebhook(r *http.Request) (voice.Event, error) {
	if r.Header.Get("Authorization") != "signed" {
		return voice.Event{}, errors.New("bad signature")
	}
	var ev voice.Event
	return ev, json.NewDecoder(r.Body).Decode(&ev)
}

func (f *fakeVoice) wasRemoved(room, identity string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.removed, room+"/"+identity)
}

func newVoiceCommunity(t *testing.T, pseudos ...string) (*community, *fakeVoice) {
	fv := &fakeVoice{published: map[string]bool{}}
	c := newCommunity(t, pseudos...)
	c.srv.EnableVoice(VoiceOptions{Backend: fv})
	c.http.Config.Handler = c.srv.Handler() // routes depend on voice being enabled
	return c, fv
}

// webhook simulates LiveKit notifying the server.
func (c *community) webhook(typ string, channelID int64, identity string) result {
	c.t.Helper()
	body, _ := json.Marshal(voice.Event{Type: typ, Room: roomName(channelID), Identity: identity})
	req, _ := http.NewRequest("POST", c.http.URL+"/internal/livekit/webhook", bytes.NewReader(body))
	req.Header.Set("Authorization", "signed")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return result{status: resp.StatusCode}
}

func (c *community) voiceStates(token string) map[string]voiceState {
	c.t.Helper()
	var list []voiceState
	c.expect(200, "", c.call("GET", "/v1/voice/states", token, nil, &list))
	out := map[string]voiceState{}
	for _, v := range list {
		out[v.MemberID] = v
	}
	return out
}

func TestVoiceDisabled(t *testing.T) {
	c := newCommunity(t, "bob")
	voiceCh := c.channelID(c.owner, "Général")
	c.expect(503, "voice_disabled", c.call("POST", fmt.Sprint("/v1/channels/", voiceCh, "/voice/join"), c.tok("bob"), nil, nil))
}

func TestVoiceJoin(t *testing.T) {
	c, _ := newVoiceCommunity(t, "bob")
	voiceCh := c.channelID(c.owner, "Général")
	text := c.channelID(c.owner, "général")
	join := func(token string, ch int64, out any) result {
		return c.call("POST", fmt.Sprint("/v1/channels/", ch, "/voice/join"), token, nil, out)
	}

	c.expect(400, "not_voice_channel", join(c.tok("bob"), text, nil))
	var j struct {
		URL      string `json:"url"`
		Token    string `json:"token"`
		CanSpeak bool   `json:"can_speak"`
	}
	c.expect(200, "", join(c.tok("bob"), voiceCh, &j))
	if j.Token != fmt.Sprintf("%s|%s|bob|true", roomName(voiceCh), c.id("bob")) || !j.CanSpeak || j.URL != c.http.URL+"/lk" {
		t.Fatalf("join = %+v", j)
	}

	// Without speak: listen-only token. Without connect: refused.
	c.expect(200, "", c.override(c.owner, voiceCh, "member", c.id("bob"), nil, []string{"speak"}))
	c.expect(200, "", join(c.tok("bob"), voiceCh, &j))
	if j.CanSpeak {
		t.Fatal("token allows speaking without the speak permission")
	}
	c.expect(200, "", c.override(c.owner, voiceCh, "member", c.id("bob"), nil, []string{"connect"}))
	c.expect(403, "missing_permissions", join(c.tok("bob"), voiceCh, nil))
	c.expect(200, "", c.override(c.owner, voiceCh, "member", c.id("bob"), nil, []string{"view_channel"}))
	c.expect(404, "not_found", join(c.tok("bob"), voiceCh, nil))
}

func TestVoiceStatesAndWebhooks(t *testing.T) {
	c, fv := newVoiceCommunity(t, "bob", "carol")
	a := c.channelID(c.owner, "Général")
	var b channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "voice", "name": "Jeux"}, &b))
	bob := c.id("bob")

	c.expect(401, "invalid_webhook", c.call("POST", "/internal/livekit/webhook", "", map[string]string{"Type": "participant_joined"}, nil))
	c.expect(409, "not_in_voice", c.call("PATCH", "/v1/voice/state", c.tok("bob"), map[string]bool{"self_mute": true}, nil))

	c.expect(200, "", c.webhook("participant_joined", a, bob))
	st := c.voiceStates(c.tok("carol"))[bob]
	if st.ChannelID != a || !st.CanSpeak || st.SelfMute {
		t.Fatalf("state = %+v", st)
	}

	// Deafening implies muting.
	var me voiceState
	c.expect(200, "", c.call("PATCH", "/v1/voice/state", c.tok("bob"), map[string]bool{"self_deaf": true}, &me))
	if !me.SelfDeaf || !me.SelfMute {
		t.Fatalf("self state = %+v", me)
	}

	// Joining another channel moves the member out of the first one.
	c.expect(200, "", c.webhook("participant_joined", b.ID, bob))
	if !fv.wasRemoved(roomName(a), bob) {
		t.Fatal("member not removed from the previous channel")
	}
	c.expect(200, "", c.webhook("participant_left", a, bob)) // late event for the old room: ignored
	if st := c.voiceStates(c.tok("carol"))[bob]; st.ChannelID != b.ID {
		t.Fatalf("after move: %+v", st)
	}
	c.expect(200, "", c.webhook("participant_left", b.ID, bob))
	if _, ok := c.voiceStates(c.tok("carol"))[bob]; ok {
		t.Fatal("state kept after leaving")
	}

	// A participant without connect (stale token) is kicked out on arrival.
	c.expect(200, "", c.override(c.owner, a, "member", c.id("carol"), nil, []string{"connect"}))
	c.expect(200, "", c.webhook("participant_joined", a, c.id("carol")))
	if !fv.wasRemoved(roomName(a), c.id("carol")) {
		t.Fatal("participant without connect not removed")
	}
	if _, ok := c.voiceStates(c.owner)[c.id("carol")]; ok {
		t.Fatal("participant without connect recorded")
	}

	// Voice states in hidden channels are not shown.
	c.expect(200, "", c.webhook("participant_joined", b.ID, bob))
	c.expect(200, "", c.override(c.owner, b.ID, "member", c.id("carol"), nil, []string{"view_channel"}))
	if _, ok := c.voiceStates(c.tok("carol"))[bob]; ok {
		t.Fatal("carol sees a voice state in a hidden channel")
	}
}

func TestVoiceFollowsPermissions(t *testing.T) {
	c, fv := newVoiceCommunity(t, "bob", "carol")
	a := c.channelID(c.owner, "Général")
	bob, carol := c.id("bob"), c.id("carol")
	c.expect(200, "", c.webhook("participant_joined", a, bob))
	c.expect(200, "", c.webhook("participant_joined", a, carol))

	// Losing speak mutes server-side; getting it back unmutes.
	c.expect(200, "", c.override(c.owner, a, "member", bob, nil, []string{"speak"}))
	if can, ok := fv.published[roomName(a)+"/"+bob]; !ok || can {
		t.Fatalf("speak not revoked in LiveKit: %v %v", can, ok)
	}
	if c.voiceStates(c.owner)[bob].CanSpeak {
		t.Fatal("state still says bob can speak")
	}
	c.expect(204, "", c.call("DELETE", fmt.Sprintf("/v1/channels/%d/overrides/member/%s", a, bob), c.owner, nil, nil))
	if !fv.published[roomName(a)+"/"+bob] {
		t.Fatal("speak not restored")
	}

	// Losing connect disconnects.
	c.expect(200, "", c.override(c.owner, a, "member", bob, nil, []string{"connect"}))
	if !fv.wasRemoved(roomName(a), bob) {
		t.Fatal("bob not removed after losing connect")
	}

	// Kicked members leave voice.
	c.expect(204, "", c.call("POST", "/v1/members/"+carol+"/kick", c.owner, map[string]any{}, nil))
	if !fv.wasRemoved(roomName(a), carol) {
		t.Fatal("kicked member still in voice")
	}

	// Deleting the channel disconnects its participants.
	dave := c.newUser("dave")
	daveID := c.mustLogin(dave, loginOpts{invite: c.invite(c.owner, nil)}).Member.ID
	c.expect(200, "", c.webhook("participant_joined", a, daveID))
	c.expect(204, "", c.call("DELETE", fmt.Sprint("/v1/channels/", a), c.owner, nil, nil))
	if !fv.wasRemoved(roomName(a), daveID) {
		t.Fatal("participant kept after channel deletion")
	}
	if len(c.voiceStates(c.owner)) != 0 {
		t.Fatalf("states left: %v", c.voiceStates(c.owner))
	}
}
