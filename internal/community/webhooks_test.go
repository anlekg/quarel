package community

import (
	"fmt"
	"testing"
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
