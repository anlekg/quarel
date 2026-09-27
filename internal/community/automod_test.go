package community

import (
	"fmt"
	"testing"
	"time"
)

func TestBannedWordMatching(t *testing.T) {
	words := []string{"Crétin", "arnaq*", "gros mot"}
	for text, want := range map[string]bool{
		"Quel CRETIN !":           true,  // case and accents ignored
		"crétinerie":              false, // whole words only
		"c'est une arnaque":       true,  // prefix
		"un gros   Mot ici":       true,  // phrase
		"gros et mot":             false,
		"rien à signaler":         false,
		"arnaq":                   true,
		"une arnaq* écrite telle": true,
	} {
		if got := bannedWord(words, text); got != want {
			t.Errorf("bannedWord(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestAutoMod(t *testing.T) {
	c := newCommunity(t, "mod", "bob")
	gen := c.channelID(c.owner, "général")
	mod := c.createRole(c.owner, "Modo", "manage_messages")
	c.expect(200, "", c.assign(c.owner, c.id("mod"), mod))
	send := func(who, text string) result {
		return c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok(who), map[string]any{"content": text}, nil)
	}

	var cfg autoModConfig
	c.expect(403, "missing_permissions", c.call("GET", "/v1/server/automod", c.tok("mod"), nil, nil))
	c.expect(200, "", c.call("GET", "/v1/server/automod", c.owner, nil, &cfg))
	if len(cfg.Words) != 0 || cfg.BlockLinks {
		t.Fatalf("default = %+v", cfg)
	}
	c.expect(400, "invalid_automod", c.call("PUT", "/v1/server/automod", c.owner, map[string]any{"max_mentions": 500}, nil))
	c.expect(200, "", c.call("PUT", "/v1/server/automod", c.owner, map[string]any{
		"words": []string{"crétin", " crétin ", "arnaq*"}, "block_links": true, "max_mentions": 2, "duplicates": true, "timeout": 300,
	}, &cfg))
	if len(cfg.Words) != 2 {
		t.Fatalf("words not cleaned: %v", cfg.Words)
	}

	c.expect(201, "", send("bob", "Bonjour tout le monde"))
	c.expect(403, "automod_word", send("bob", "Espèce de CRETIN"))
	c.expect(403, "automod_link", send("bob", "voir https://exemple.org"))
	// Editing into a banned word is refused too; the refusals so far: 2.
	var m message
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), map[string]any{"content": "propre"}, &m))
	c.expect(403, "automod_word", c.call("PATCH", fmt.Sprintf("/v1/channels/%d/messages/%d", gen, m.ID), c.tok("bob"), map[string]any{"content": "une arnaque"}, nil))
	// Third refusal within 10 minutes: bob is timed out automatically.
	c.expect(403, "timed_out", send("bob", "encore là"))
	var log []auditEntry
	c.expect(200, "", c.call("GET", "/v1/audit-log?target_id="+c.id("bob"), c.owner, nil, &log))
	blocks, timeouts := 0, 0
	for _, e := range log {
		switch e.Action {
		case auditAutomodBlock:
			blocks++
			if e.ActorID != nil || e.Details["rule"] == nil {
				t.Fatalf("automod entry = %+v", e)
			}
		case auditMemberTimeout:
			timeouts++
		}
	}
	if blocks != 3 || timeouts != 1 {
		t.Fatalf("audit: %d blocks, %d timeouts: %+v", blocks, timeouts, log)
	}
	c.clock = c.clock.Add(301 * time.Second)

	// Mentions and repeated messages.
	c.expect(403, "automod_mentions", send("bob", fmt.Sprintf("<@%s> <@%s> @everyone", c.id("mod"), c.owner0())))
	c.expect(201, "", send("bob", fmt.Sprintf("<@%s> <@%s>", c.id("mod"), c.owner0())))
	c.expect(201, "", send("bob", "achetez"))
	c.expect(201, "", send("bob", "Achetez !"))
	c.expect(403, "automod_duplicate", send("bob", "achetez"))
	c.clock = c.clock.Add(31 * time.Second)
	c.expect(201, "", send("bob", "achetez"))

	// Moderators (manage_messages) and the owner are exempt.
	c.expect(201, "", send("mod", "crétin, https://exemple.org"))
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.owner, map[string]any{"content": "arnaque"}, nil))
}
