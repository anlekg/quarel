package community

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/ratelimit"
)

// Threads and forum posts: created at a limited pace, archived when inactive
// (out of the lists, still readable and searchable), reopened by a message.
func TestThreadBounds(t *testing.T) {
	c := newCommunity(t, "bob")
	c.srv.limit.threads = ratelimit.New(2, 10*time.Minute)
	gen := c.channelID(c.owner, "général")
	var forum channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "forum", "name": "entraide"}, &forum))

	// bob: two threads or posts per 10 minutes; the owner (manage_channels) is not limited.
	thread := func(tok, text string) (channel, result) {
		var m message
		c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), tok, map[string]string{"content": text}, &m))
		var th channel
		return th, c.call("POST", fmt.Sprintf("/v1/channels/%d/messages/%d/threads", gen, m.ID), tok, map[string]any{}, &th)
	}
	th, res := thread(c.tok("bob"), "le train de demain")
	c.expect(201, "", res)
	var post forumPost
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", forum.ID, "/posts"), c.tok("bob"), map[string]string{"title": "Question", "content": "Comment ça marche ?"}, &post))
	_, res = thread(c.tok("bob"), "encore un")
	c.expect(429, "rate_limited", res)
	c.expect(429, "rate_limited", c.call("POST", fmt.Sprint("/v1/channels/", forum.ID, "/posts"), c.tok("bob"), map[string]string{"title": "Encore", "content": "x"}, nil))
	for i := 0; i < 3; i++ {
		_, res = thread(c.owner, fmt.Sprint("sujet ", i))
		c.expect(201, "", res)
	}

	// A week without a message: archived, out of the lists.
	listed := func(id int64) bool {
		var list []channel
		c.expect(200, "", c.call("GET", "/v1/channels", c.tok("bob"), nil, &list))
		for _, x := range list {
			if x.ID == id {
				return true
			}
		}
		return false
	}
	// (sessions would expire with the clock: the thread is made older instead)
	week := (threadArchiveAfter + time.Hour).Milliseconds()
	c.srv.db.Exec(`UPDATE channels SET created_at = created_at - ? WHERE id IN (?, ?)`, week, th.ID, post.Channel.ID)
	c.srv.db.Exec(`UPDATE messages SET created_at = created_at - ? WHERE channel_id IN (?, ?)`, week, th.ID, post.Channel.ID)
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", post.Channel.ID, "/messages"), c.owner, map[string]string{"content": "Comme ça."}, nil))
	if err := c.srv.ArchiveThreads(t.Context()); err != nil {
		t.Fatal(err)
	}
	if listed(th.ID) || !listed(post.Channel.ID) || !listed(gen) {
		t.Fatalf("after archiving: thread listed %v, post listed %v", listed(th.ID), listed(post.Channel.ID))
	}
	var got channel
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", th.ID), c.tok("bob"), nil, &got))
	if got.ArchivedAt == nil || got.Name != th.Name {
		t.Fatalf("archived thread = %+v", got)
	}
	var hist []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", th.ID, "/messages"), c.tok("bob"), nil, &hist))
	var found []message
	c.expect(200, "", c.call("GET", "/v1/search?q="+url.QueryEscape("train demain"), c.tok("bob"), nil, &found))
	if len(found) == 0 {
		t.Fatal("an archived thread's messages are no longer searchable")
	}

	// A message reopens it.
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", th.ID, "/messages"), c.tok("bob"), map[string]string{"content": "on y retourne ?"}, nil))
	if !listed(th.ID) {
		t.Fatal("a message did not reopen the thread")
	}
	var reopened channel
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", th.ID), c.tok("bob"), nil, &reopened))
	if reopened.ArchivedAt != nil {
		t.Fatalf("reopened thread = %+v", reopened)
	}
}
