package community

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// gatewayEvents connects token to the gateway and returns a function that
// waits for the next event of type t (others are skipped).
func (c *community) gatewayEvents(ctx context.Context, token string) func(t string) json.RawMessage {
	c.t.Helper()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.http.URL, "http")+"/v1/gateway", nil)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { conn.CloseNow() })
	wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": token})
	return func(t string) json.RawMessage {
		c.t.Helper()
		for {
			var ev struct {
				T string
				D json.RawMessage
			}
			if err := wsjson.Read(ctx, conn, &ev); err != nil {
				c.t.Fatalf("waiting for %s: %v", t, err)
			}
			if ev.T == t {
				return ev.D
			}
		}
	}
}

func TestSlashCommands(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	gen := c.channelID(c.owner, "général")
	var bot botJSON
	c.expect(201, "", c.call("POST", "/v1/bots", c.owner, map[string]any{"name": "Dés"}, &bot))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	decl := []map[string]any{{
		"name": "lancer", "description": "Lance des dés",
		"options": []map[string]any{
			{"name": "faces", "type": "integer", "required": true},
			{"name": "pour", "type": "member"},
			{"name": "secret", "type": "boolean"},
		},
	}}
	c.expect(403, "bots_only", c.call("PUT", "/v1/bots/@me/commands", c.tok("bob"), decl, nil))
	c.expect(400, "invalid_command", c.call("PUT", "/v1/bots/@me/commands", bot.Token, []map[string]any{{"name": "Majuscule"}}, nil))
	c.expect(400, "invalid_command", c.call("PUT", "/v1/bots/@me/commands", bot.Token, []map[string]any{{"name": "x", "options": []map[string]any{
		{"name": "a", "type": "string"}, {"name": "b", "type": "string", "required": true}}}}, nil))
	bobEvents := c.gatewayEvents(ctx, c.tok("bob"))
	c.expect(200, "", c.call("PUT", "/v1/bots/@me/commands", bot.Token, decl, nil))
	var update []botCommand
	json.Unmarshal(bobEvents("COMMANDS_UPDATE"), &update)
	if len(update) != 1 || update[0].BotID != bot.Member.ID || len(update[0].Options) != 3 {
		t.Fatalf("COMMANDS_UPDATE = %+v", update)
	}
	var list []botCommand
	c.expect(200, "", c.call("GET", "/v1/commands", c.tok("carol"), nil, &list))
	if len(list) != 1 || list[0].Name != "lancer" {
		t.Fatalf("commands = %+v", list)
	}

	run := func(who string, opts map[string]any) result {
		return c.call("POST", fmt.Sprint("/v1/channels/", gen, "/commands"), c.tok(who), map[string]any{"bot_id": bot.Member.ID, "name": "lancer", "options": opts}, nil)
	}
	c.expect(409, "bot_offline", run("bob", map[string]any{"faces": 6}))
	botEvents := c.gatewayEvents(ctx, bot.Token)
	botEvents("READY")
	c.expect(400, "missing_option", run("bob", map[string]any{}))
	c.expect(400, "invalid_option", run("bob", map[string]any{"faces": "six"}))
	c.expect(400, "invalid_option", run("bob", map[string]any{"faces": 6, "pour": "personne"}))
	c.expect(400, "unknown_option", run("bob", map[string]any{"faces": 6, "autre": 1}))
	c.expect(404, "unknown_command", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/commands"), c.tok("bob"), map[string]any{"bot_id": bot.Member.ID, "name": "autre"}, nil))

	// Only the bot hears the command; its public reply is marked with it.
	var started struct{ ID string }
	c.expect(202, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/commands"), c.tok("bob"),
		map[string]any{"bot_id": bot.Member.ID, "name": "lancer", "options": map[string]any{"faces": 20, "pour": c.id("carol")}}, &started))
	var it struct {
		ID        string
		Name      string
		Options   map[string]any
		ChannelID int64  `json:"channel_id"`
		MemberID  string `json:"member_id"`
	}
	json.Unmarshal(botEvents("INTERACTION_CREATE"), &it)
	if it.ID != started.ID || it.Options["faces"] != float64(20) || it.Options["pour"] != c.id("carol") || it.MemberID != c.id("bob") || it.ChannelID != gen {
		t.Fatalf("INTERACTION_CREATE = %+v", it)
	}
	c.expect(404, "not_found", c.call("POST", "/v1/interactions/"+it.ID+"/reply", c.tok("carol"), map[string]any{"content": "piraté"}, nil))
	var reply message
	c.expect(201, "", c.call("POST", "/v1/interactions/"+it.ID+"/reply", bot.Token, map[string]any{"content": "🎲 17"}, &reply))
	if reply.Interaction == nil || reply.Interaction.Name != "lancer" || reply.Interaction.MemberID != c.id("bob") || reply.AuthorID != bot.Member.ID {
		t.Fatalf("reply = %+v", reply)
	}
	var hist []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), nil, &hist))
	if last := hist[len(hist)-1]; last.Interaction == nil || last.Content != "🎲 17" {
		t.Fatalf("history = %+v", last)
	}

	// Ephemeral: only the member who ran it gets it, nothing stored.
	c.expect(202, "", run("bob", map[string]any{"faces": 6, "secret": true}))
	json.Unmarshal(botEvents("INTERACTION_CREATE"), &it)
	c.expect(204, "", c.call("POST", "/v1/interactions/"+it.ID+"/reply", bot.Token, map[string]any{"content": "🎲 4 (pour vous seul·e)", "ephemeral": true}, nil))
	var eph struct {
		Content   string
		ChannelID int64 `json:"channel_id"`
	}
	json.Unmarshal(bobEvents("INTERACTION_REPLY"), &eph)
	if eph.Content != "🎲 4 (pour vous seul·e)" || eph.ChannelID != gen {
		t.Fatalf("INTERACTION_REPLY = %+v", eph)
	}
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), nil, &hist))
	if last := hist[len(hist)-1]; last.Content != "🎲 17" {
		t.Fatalf("ephemeral reply stored: %+v", last)
	}
	// At most maxReplies answers to one interaction (ephemeral ones too).
	for i := 1; i < maxReplies; i++ {
		c.expect(204, "", c.call("POST", "/v1/interactions/"+it.ID+"/reply", bot.Token, map[string]any{"content": "encore", "ephemeral": true}, nil))
	}
	c.expect(429, "too_many_replies", c.call("POST", "/v1/interactions/"+it.ID+"/reply", bot.Token, map[string]any{"content": "encore", "ephemeral": true}, nil))
	// Too late.
	c.clock = c.clock.Add(16 * time.Minute)
	c.expect(404, "not_found", c.call("POST", "/v1/interactions/"+it.ID+"/reply", bot.Token, map[string]any{"content": "trop tard"}, nil))
}
