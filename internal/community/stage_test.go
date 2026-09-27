package community

import (
	"fmt"
	"testing"
)

func TestStage(t *testing.T) {
	c, fv := newVoiceCommunity(t, "mod", "bob")
	c.expect(400, "invalid_type", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "text", "name": "x", "stage": true}, nil))
	var stage channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "voice", "name": "Conférence", "stage": true}, &stage))
	if !stage.Stage {
		t.Fatalf("stage = %+v", stage)
	}
	mod, bob := c.id("mod"), c.id("bob")
	c.expect(200, "", c.assign(c.owner, mod, c.createRole(c.owner, "Régie", "mute_members")))
	room := roomName(stage.ID)

	// The audience listens only; moderators talk.
	var join struct {
		CanSpeak  bool `json:"can_speak"`
		CanStream bool `json:"can_stream"`
	}
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", stage.ID, "/voice/join"), c.tok("bob"), nil, &join))
	if join.CanSpeak || join.CanStream {
		t.Fatalf("audience join = %+v", join)
	}
	c.expect(200, "", c.webhook("participant_joined", stage.ID, bob))
	c.expect(200, "", c.webhook("participant_joined", stage.ID, mod))
	if st := c.voiceStates(c.owner)[mod]; !st.CanSpeak {
		t.Fatalf("moderator state = %+v", st)
	}
	if st := c.voiceStates(c.owner)[bob]; st.CanSpeak || st.Speaker {
		t.Fatalf("audience state = %+v", st)
	}

	// bob raises his hand, cannot promote himself; the moderator invites him.
	self := func(body map[string]any) result { return c.call("PATCH", "/v1/voice/state", c.tok("bob"), body, nil) }
	c.expect(200, "", self(map[string]any{"hand_raised": true}))
	if st := c.voiceStates(c.tok("mod"))[bob]; !st.HandRaised {
		t.Fatalf("hand = %+v", st)
	}
	c.expect(403, "missing_permissions", self(map[string]any{"speaker": true}))
	c.expect(403, "missing_permissions", c.call("PATCH", "/v1/voice/states/"+mod, c.tok("bob"), map[string]any{"speaker": false}, nil))
	c.expect(200, "", c.call("PATCH", "/v1/voice/states/"+bob, c.tok("mod"), map[string]any{"speaker": true}, nil))
	if g, _ := fv.grant(room, bob); !g.Microphone || !g.Camera {
		t.Fatalf("speaker grant = %+v", g)
	}
	if st := c.voiceStates(c.owner)[bob]; !st.Speaker || st.HandRaised || !st.CanSpeak {
		t.Fatalf("speaker state = %+v", st)
	}
	// He steps down; invited again, then leaves: back in the audience next time.
	c.expect(200, "", self(map[string]any{"speaker": false}))
	if g, _ := fv.grant(room, bob); g.Microphone {
		t.Fatalf("after stepping down = %+v", g)
	}
	c.expect(200, "", c.call("PATCH", "/v1/voice/states/"+bob, c.tok("mod"), map[string]any{"speaker": true}, nil))
	c.expect(200, "", c.webhook("participant_left", stage.ID, bob))
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", stage.ID, "/voice/join"), c.tok("bob"), nil, &join))
	if join.CanSpeak {
		t.Fatal("still a speaker after leaving")
	}
	// Not a stage: speaker makes no sense; turning the stage off gives voices back.
	c.expect(200, "", c.webhook("participant_joined", stage.ID, bob))
	c.expect(200, "", c.call("PATCH", fmt.Sprint("/v1/channels/", stage.ID), c.owner, map[string]any{"stage": false}, nil))
	if g, _ := fv.grant(room, bob); !g.Microphone {
		t.Fatalf("stage off = %+v", g)
	}
	c.expect(409, "not_on_stage", c.call("PATCH", "/v1/voice/states/"+bob, c.tok("mod"), map[string]any{"speaker": true}, nil))
}
