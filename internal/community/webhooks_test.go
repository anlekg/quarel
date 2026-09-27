package community

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/anlekg/quarel/internal/sqlitedb"
)

func TestWebhooks(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	voice := c.channelID(c.owner, "Général")
	path := fmt.Sprint("/v1/channels/", gen, "/webhooks")

	c.expect(403, "missing_permissions", c.call("POST", path, c.tok("bob"), map[string]string{"name": "CI"}, nil))
	c.expect(400, "not_text_channel", c.call("POST", fmt.Sprint("/v1/channels/", voice, "/webhooks"), c.owner, map[string]string{"name": "CI"}, nil))
	var h webhookJSON
	c.expect(201, "", c.call("POST", path, c.owner, map[string]string{"name": "Intégration continue"}, &h))
	if h.Token == "" || h.ID == "" {
		t.Fatalf("webhook = %+v", h)
	}
	var list []webhookJSON
	c.expect(200, "", c.call("GET", path, c.owner, nil, &list))
	if len(list) != 1 || list[0].Token != "" || list[0].Name != "Intégration continue" {
		t.Fatalf("list = %+v (the token is never listed)", list)
	}

	// Posting needs the secret; @everyone does not notify, a member mention does.
	post := func(id, token, content string) result {
		return c.call("POST", "/v1/webhooks/"+id+"/"+token, "", map[string]string{"content": content}, nil)
	}
	c.expect(404, "not_found", post(h.ID, "qw_faux", "x"))
	var m message
	c.expect(201, "", c.call("POST", "/v1/webhooks/"+h.ID+"/"+h.Token, "", map[string]string{"content": fmt.Sprintf("Build OK @everyone <@%s>", c.id("bob"))}, &m))
	if m.Webhook == nil || m.Webhook.Name != "Intégration continue" || m.MentionEveryone || len(m.Mentions) != 1 {
		t.Fatalf("message = %+v", m)
	}
	var hist []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), nil, &hist))
	if last := hist[len(hist)-1]; last.Webhook == nil || last.Webhook.ID != h.ID {
		t.Fatalf("history = %+v", last)
	}
	// Not a listed member.
	var members []memberJSON
	c.expect(200, "", c.call("GET", "/v1/members", c.tok("bob"), nil, &members))
	for _, x := range members {
		if x.ID == m.AuthorID {
			t.Fatal("webhook listed as a member")
		}
	}

	// Deleted: the address stops working, its messages keep their name.
	c.expect(403, "missing_permissions", c.call("DELETE", "/v1/webhooks/"+h.ID, c.tok("bob"), nil, nil))
	c.expect(204, "", c.call("DELETE", "/v1/webhooks/"+h.ID, c.owner, nil, nil))
	c.expect(404, "not_found", post(h.ID, h.Token, "encore"))
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), nil, &hist))
	if last := hist[len(hist)-1]; last.Webhook == nil || last.Webhook.Name != "Intégration continue" {
		t.Fatalf("after deletion = %+v", last)
	}
}

// A webhook never lets its creator post where they could not: creating one
// needs manage_webhooks (manage_channels no longer suffices) and the right to
// post in the channel (manage_messages too in an announcement channel).
func TestWebhookNeedsPostingRights(t *testing.T) {
	c := newCommunity(t, "bob")
	var ch channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "announcement", "name": "annonces"}, &ch))
	override := fmt.Sprintf("/v1/channels/%d/overrides/member/%s", ch.ID, c.id("bob"))
	create := func() result {
		return c.call("POST", fmt.Sprint("/v1/channels/", ch.ID, "/webhooks"), c.tok("bob"), map[string]string{"name": "Admin"}, nil)
	}
	c.expect(200, "", c.call("PUT", override, c.owner, map[string]any{"allow": []string{"manage_channels"}}, nil))
	c.expect(403, "missing_permissions", create())
	c.expect(200, "", c.call("PUT", override, c.owner, map[string]any{"allow": []string{"manage_webhooks"}}, nil))
	c.expect(403, "missing_permissions", create()) // may not post in the announcements
	c.expect(403, "missing_permissions", c.call("POST", fmt.Sprint("/v1/channels/", ch.ID, "/messages"), c.tok("bob"), map[string]string{"content": "x"}, nil))
	c.expect(200, "", c.call("PUT", override, c.owner, map[string]any{"allow": []string{"manage_webhooks", "manage_messages"}}, nil))
	c.expect(201, "", create())
}

// Migration 12: roles and overrides that allowed manage_channels keep managing webhooks.
func TestManageWebhooksMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	db, err := sqlitedb.Open(path, migrations[:11])
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO roles (name, position, permissions, created_at) VALUES ('Modération', 1, ?, 0), ('Autre', 2, ?, 0)`, int64(permManageChannels|permSendMessages), int64(permSendMessages))
	db.Exec(`INSERT INTO channels (type, name, created_at) VALUES ('text', 'général', 0)`)
	db.Exec(`INSERT INTO channel_overrides (channel_id, target_type, target_id, allow, deny) VALUES (1, 'role', '1', ?, 0), (1, 'member', 'x', 0, ?)`, int64(permManageChannels), int64(permManageChannels))
	db.Close()
	if db, err = OpenDB(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mod, other, allow, deny int64
	db.QueryRow(`SELECT permissions FROM roles WHERE name = 'Modération'`).Scan(&mod)
	db.QueryRow(`SELECT permissions FROM roles WHERE name = 'Autre'`).Scan(&other)
	db.QueryRow(`SELECT allow FROM channel_overrides WHERE target_type = 'role'`).Scan(&allow)
	db.QueryRow(`SELECT deny FROM channel_overrides WHERE target_type = 'member'`).Scan(&deny)
	if perm(mod)&permManageWebhooks == 0 || perm(other)&permManageWebhooks != 0 || perm(allow)&permManageWebhooks == 0 || perm(deny)&permManageWebhooks == 0 {
		t.Fatalf("after migration: moderation %b, other %b, allow %b, deny %b", mod, other, allow, deny)
	}
}
