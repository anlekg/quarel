package community

import (
	"fmt"
	"testing"
)

// A member downloads what the server keeps about them: their messages
// (in every channel), reactions, roles, settings; nothing about others.
func TestExport(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	gen := c.channelID(c.owner, "général")
	role := c.createRole(c.owner, "Tarot", "send_messages")
	c.expect(200, "", c.assign(c.owner, c.id("bob"), role))
	var m message
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), map[string]any{"content": "premier message"}, &m))
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), map[string]any{"content": "celui de carol"}, nil))
	c.expect(204, "", c.call("PUT", fmt.Sprintf("/v1/channels/%d/messages/%d/reactions/%s", gen, m.ID, "👍"), c.tok("bob"), nil, nil))

	var doc struct {
		Format    string
		Member    memberJSON
		Roles     []map[string]any
		Reactions []map[string]any
		Messages  []struct {
			ID      int64
			Channel string
			Content string
		}
	}
	c.expect(200, "", c.call("GET", "/v1/members/@me/export", c.tok("bob"), nil, &doc))
	if doc.Format != "quarel-server-export-1" || doc.Member.ID != c.id("bob") || len(doc.Roles) != 1 || doc.Roles[0]["name"] != "Tarot" {
		t.Fatalf("export = %+v", doc)
	}
	if len(doc.Messages) != 1 || doc.Messages[0].Content != "premier message" || doc.Messages[0].Channel != "général" {
		t.Fatalf("messages = %+v", doc.Messages)
	}
	if len(doc.Reactions) != 1 || doc.Reactions[0]["emoji"] != "👍" {
		t.Fatalf("reactions = %+v", doc.Reactions)
	}
}
