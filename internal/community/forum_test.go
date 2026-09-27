package community

import (
	"fmt"
	"testing"
	"time"
)

func TestForum(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	var forum channel
	c.expect(403, "missing_permissions", c.call("POST", "/v1/channels", c.tok("bob"), map[string]any{"type": "forum", "name": "entraide"}, nil))
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "forum", "name": "entraide", "topic": "Posez vos questions"}, &forum))
	if forum.Type != chanForum {
		t.Fatalf("forum = %+v", forum)
	}
	base := fmt.Sprint("/v1/channels/", forum.ID)
	// No messages of its own.
	c.expect(400, "not_text_channel", c.call("POST", base+"/messages", c.tok("bob"), map[string]any{"content": "x"}, nil))
	c.expect(400, "not_forum", c.call("POST", fmt.Sprint("/v1/channels/", c.channelID(c.owner, "général"), "/posts"), c.tok("bob"), map[string]any{"title": "t", "content": "x"}, nil))

	var p1, p2 forumPost
	c.expect(201, "", c.call("POST", base+"/posts", c.tok("bob"), map[string]any{"title": "Le son grésille", "content": "Depuis hier, en vocal."}, &p1))
	if p1.Channel.Type != chanThread || *p1.Channel.ParentID != forum.ID || p1.Channel.Name != "Le son grésille" || p1.MessageCount != 1 {
		t.Fatalf("post = %+v", p1)
	}
	c.clock = c.clock.Add(time.Minute)
	c.expect(201, "", c.call("POST", base+"/posts", c.tok("carol"), map[string]any{"title": "Idée de salon", "content": "Un salon musique ?"}, &p2))
	c.expect(400, "invalid_name", c.call("POST", base+"/posts", c.tok("carol"), map[string]any{"title": "", "content": "x"}, nil))

	var posts []forumPost
	c.expect(200, "", c.call("GET", base+"/posts", c.tok("carol"), nil, &posts))
	if len(posts) != 2 || posts[0].Channel.ID != p2.Channel.ID {
		t.Fatalf("posts = %+v", posts)
	}
	// Answering a post brings it back to the top.
	c.clock = c.clock.Add(time.Minute)
	c.post(c.tok("carol"), p1.Channel.ID, map[string]any{"content": "Essayez un autre micro."})
	c.expect(200, "", c.call("GET", base+"/posts", c.tok("bob"), nil, &posts))
	if posts[0].Channel.ID != p1.Channel.ID || posts[0].MessageCount != 2 || posts[0].AuthorID != c.id("bob") || posts[0].Excerpt != "Depuis hier, en vocal." {
		t.Fatalf("posts after an answer = %+v", posts)
	}
	// The forum's permissions apply to its posts.
	c.expect(200, "", c.override(c.owner, forum.ID, "role", "1", nil, []string{"send_messages"}))
	c.expect(403, "missing_permissions", c.call("POST", base+"/posts", c.tok("bob"), map[string]any{"title": "t", "content": "x"}, nil))
	c.expect(403, "missing_permissions", c.call("POST", fmt.Sprint("/v1/channels/", p1.Channel.ID, "/messages"), c.tok("bob"), map[string]any{"content": "x"}, nil))
	// Deleting the forum deletes its posts.
	c.expect(204, "", c.call("DELETE", base, c.owner, nil, nil))
	c.expect(404, "not_found", c.call("GET", fmt.Sprint("/v1/channels/", p1.Channel.ID, "/messages"), c.tok("bob"), nil, nil))
}
