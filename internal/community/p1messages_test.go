package community

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (c *community) upload(token string, channelID int64, name string, data []byte) (attachmentJSON, result) {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/v1/channels/%d/attachments", c.http.URL, channelID), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var a attachmentJSON
	var er struct{ Error struct{ Code string } }
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &a)
	json.Unmarshal(raw, &er)
	return a, result{resp.StatusCode, er.Error.Code}
}

func (c *community) download(token, path string) (*http.Response, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.http.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (c *community) post(token string, ch int64, body map[string]any) message {
	c.t.Helper()
	var m message
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", ch, "/messages"), token, body, &m))
	return m
}

func TestRepliesReactionsPins(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	orig := c.post(c.owner, gen, map[string]any{"content": "Question : qui vient samedi ?"})
	reply := c.post(c.tok("bob"), gen, map[string]any{"content": "Moi !", "reply_to": orig.ID})
	if reply.ReplyTo == nil || *reply.ReplyTo != orig.ID || reply.Referenced == nil || reply.Referenced.Content != orig.Content {
		t.Fatalf("reply = %+v", reply)
	}
	if len(reply.Mentions) != 1 || reply.Mentions[0] != orig.AuthorID {
		t.Fatalf("replied author not notified: %v", reply.Mentions)
	}
	quiet := c.post(c.tok("bob"), gen, map[string]any{"content": "sans notifier", "reply_to": orig.ID, "mention_reply": false})
	if len(quiet.Mentions) != 0 {
		t.Fatal("mention_reply=false still notified")
	}
	other := c.createChannel(map[string]any{"name": "autre"})
	c.expect(400, "invalid_reply", c.call("POST", fmt.Sprint("/v1/channels/", other, "/messages"), c.owner, map[string]any{"content": "x", "reply_to": orig.ID}, nil))

	// Reactions.
	rpath := func(emoji string) string {
		return fmt.Sprintf("/v1/channels/%d/messages/%d/reactions/%s", gen, orig.ID, url.PathEscape(emoji))
	}
	c.expect(204, "", c.call("PUT", rpath("👍"), c.tok("bob"), nil, nil))
	c.expect(204, "", c.call("PUT", rpath("👍"), c.owner, nil, nil))
	c.expect(204, "", c.call("PUT", rpath("🎉"), c.owner, nil, nil))
	c.expect(400, "invalid_emoji", c.call("PUT", rpath("lol"), c.owner, nil, nil))
	var list []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), nil, &list))
	r := list[0].Reactions
	if len(r) != 2 || r[0].Emoji != "👍" || r[0].Count != 2 || !r[0].Me || r[1].Me {
		t.Fatalf("reactions seen by bob = %+v", r)
	}
	c.expect(403, "missing_permissions", c.call("DELETE", rpath("🎉")+"/"+c.srv.mustOwnerID(t), c.tok("bob"), nil, nil))
	c.expect(204, "", c.call("DELETE", rpath("👍"), c.tok("bob"), nil, nil))
	c.expect(204, "", c.call("DELETE", rpath("🎉")+"/"+c.srv.mustOwnerID(t), c.owner, nil, nil))

	// Pins.
	pin := fmt.Sprintf("/v1/channels/%d/pins/%d", gen, orig.ID)
	c.expect(403, "missing_permissions", c.call("PUT", pin, c.tok("bob"), nil, nil))
	c.expect(204, "", c.call("PUT", pin, c.owner, nil, nil))
	var pins []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/pins"), c.tok("bob"), nil, &pins))
	if len(pins) != 1 || pins[0].ID != orig.ID || pins[0].PinnedAt == nil {
		t.Fatalf("pins = %+v", pins)
	}
	c.expect(204, "", c.call("DELETE", pin, c.owner, nil, nil))
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/pins"), c.tok("bob"), nil, &pins))
	if len(pins) != 0 {
		t.Fatal("unpinned message still listed")
	}
}

func TestAttachments(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	c.srv.cfg.MaxUploadBytes = 1 << 20
	gen := c.channelID(c.owner, "général")
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)

	// The declared name says .txt; the bytes say PNG: the bytes win.
	a, res := c.upload(c.tok("bob"), gen, "../../photo.txt", png)
	c.expect(201, "", res)
	if a.ContentType != "image/png" || a.Filename != "photo.txt" || a.Size != int64(len(png)) {
		t.Fatalf("attachment = %+v", a)
	}
	// Not sent yet: only its uploader can fetch it, and nobody else can attach it.
	if resp, _ := c.download(c.tok("carol"), a.URL); resp.StatusCode != 404 {
		t.Fatalf("unsent upload visible to others: %d", resp.StatusCode)
	}
	c.expect(400, "invalid_attachment", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), map[string]any{"attachments": []string{a.ID}}, nil))

	m := c.post(c.tok("bob"), gen, map[string]any{"attachments": []string{a.ID}}) // a file alone is a valid message
	if len(m.Attachments) != 1 || m.Attachments[0].ID != a.ID || m.Content != "" {
		t.Fatalf("message = %+v", m)
	}
	resp, body := c.download(c.tok("carol"), a.URL)
	if resp.StatusCode != 200 || !bytes.Equal(body, png) || resp.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: %d %v", resp.StatusCode, resp.Header)
	}

	// Anything that is not a safe media type is served as a download.
	html, _ := c.upload(c.tok("bob"), gen, "page.html", []byte("<html><script>alert(1)</script></html>"))
	c.post(c.tok("bob"), gen, map[string]any{"content": "voir", "attachments": []string{html.ID}})
	resp, _ = c.download(c.tok("carol"), html.URL)
	if resp.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("html served as %v", resp.Header)
	}

	// Size limit and permission.
	_, res = c.upload(c.tok("bob"), gen, "gros.bin", bytes.Repeat([]byte("x"), 2<<20))
	c.expect(413, "file_too_large", res)
	c.expect(200, "", c.override(c.owner, gen, "member", c.id("carol"), nil, []string{"attach_files"}))
	_, res = c.upload(c.tok("carol"), gen, "a.txt", []byte("coucou"))
	c.expect(403, "missing_permissions", res)

	// Members who cannot see the channel cannot download.
	c.expect(200, "", c.override(c.owner, gen, "member", c.id("carol"), nil, []string{"view_channel"}))
	if resp, _ := c.download(c.tok("carol"), a.URL); resp.StatusCode != 404 {
		t.Fatalf("file served to a member who cannot see the channel: %d", resp.StatusCode)
	}

	// Deleting the message deletes the file.
	c.expect(204, "", c.call("DELETE", fmt.Sprintf("/v1/channels/%d/messages/%d", gen, m.ID), c.tok("bob"), nil, nil))
	if _, err := os.Stat(filepath.Join(c.srv.attachmentsDir(), a.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file kept after deleting its message: %v", err)
	}
	// Unsent uploads are cleaned up after an hour.
	stale, _ := c.upload(c.tok("bob"), gen, "oubli.txt", []byte("jamais envoyé"))
	c.clock = c.clock.Add(pendingUploadTTL + time.Minute)
	if err := c.srv.CleanupAttachments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.srv.attachmentsDir(), stale.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsent upload not cleaned up")
	}
}

func TestLinkPreviews(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<html><head><title>Titre de repli</title>
			<meta property="og:title" content="Quarel sort en V1">
			<meta property="og:description" content="Une messagerie auto-hébergée.">
			<meta property="og:site_name" content="Le Journal"></head><body>…</body></html>`)
	}))
	defer site.Close()

	// The real previewer refuses local addresses (SSRF protection).
	if _, err := newPreviewer(false).fetch(context.Background(), site.URL); !errors.Is(err, errForbiddenAddress) {
		t.Fatalf("local address fetched: %v", err)
	}

	c := newCommunity(t)
	c.srv.previews = newPreviewer(true) // tests only: the site runs on 127.0.0.1
	gen := c.channelID(c.owner, "général")
	m := c.post(c.owner, gen, map[string]any{"content": "Lisez " + site.URL + "/article !"})
	var got []message
	for range 50 {
		c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.owner, nil, &got))
		if len(got[0].Embeds) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) != 1 || got[0].ID != m.ID || len(got[0].Embeds) != 1 {
		t.Fatalf("no preview: %+v", got)
	}
	e := got[0].Embeds[0]
	if e.Title != "Quarel sort en V1" || e.SiteName != "Le Journal" || e.URL != site.URL+"/article" {
		t.Fatalf("embed = %+v", e)
	}
}

func TestSearchAndUnread(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	hidden := c.createChannel(map[string]any{"name": "secret"})
	c.expect(200, "", c.override(c.owner, hidden, "member", c.id("bob"), nil, []string{"view_channel"}))

	c.post(c.owner, gen, map[string]any{"content": "Rendez-vous devant l'école à midi"})
	c.post(c.owner, gen, map[string]any{"content": fmt.Sprintf("<@%s> tu viens à l'ECOLE ?", c.id("bob"))})
	c.post(c.owner, hidden, map[string]any{"content": "l'école secrète"})
	c.post(c.tok("bob"), gen, map[string]any{"content": "je réponds"})

	var found []message
	c.expect(200, "", c.call("GET", "/v1/search?q="+url.QueryEscape("ecole"), c.tok("bob"), nil, &found))
	if len(found) != 2 {
		t.Fatalf("bob found %d messages (hidden channel must not leak)", len(found))
	}
	c.expect(200, "", c.call("GET", "/v1/search?q="+url.QueryEscape("éco midi"), c.owner, nil, &found))
	if len(found) != 1 || !strings.Contains(found[0].Content, "midi") {
		t.Fatalf("prefix + AND search = %+v", found)
	}
	c.expect(200, "", c.call("GET", "/v1/search?q=ecole&author_id="+c.id("bob"), c.owner, nil, &found))
	if len(found) != 0 {
		t.Fatal("author filter ignored")
	}
	c.expect(400, "invalid_query", c.call("GET", "/v1/search?q="+url.QueryEscape(`"*()`), c.owner, nil, nil))

	// Unread: bob has 2 unread messages from the owner in général (his own do not count), one mentioning him.
	var states []readState
	c.expect(200, "", c.call("GET", "/v1/read-states", c.tok("bob"), nil, &states))
	var g readState
	for _, s := range states {
		if s.ChannelID == hidden {
			t.Fatal("read state of a hidden channel")
		}
		if s.ChannelID == gen {
			g = s
		}
	}
	// bob posted last, which marks the channel read up to his message.
	if g.Unread != 0 || g.Mentions != 0 {
		t.Fatalf("after posting, général = %+v", g)
	}
	c.post(c.owner, gen, map[string]any{"content": fmt.Sprintf("<@%s> et encore", c.id("bob"))})
	c.post(c.owner, gen, map[string]any{"content": "rien pour toi"})
	c.expect(200, "", c.call("GET", "/v1/read-states", c.tok("bob"), nil, &states))
	for _, s := range states {
		if s.ChannelID == gen {
			g = s
		}
	}
	if g.Unread != 2 || g.Mentions != 1 {
		t.Fatalf("général = %+v", g)
	}
	var acked readState
	c.expect(200, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/ack"), c.tok("bob"), map[string]any{"message_id": g.LastMessageID}, &acked))
	if acked.Unread != 0 || acked.Mentions != 0 {
		t.Fatalf("after ack = %+v", acked)
	}
}

func TestThreadsAnnouncementsSettings(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	gen := c.channelID(c.owner, "général")
	start := c.post(c.tok("bob"), gen, map[string]any{"content": "Organisation du tournoi de samedi\ndétails à venir"})
	var th channel
	c.expect(201, "", c.call("POST", fmt.Sprintf("/v1/channels/%d/messages/%d/threads", gen, start.ID), c.tok("carol"), map[string]any{}, &th))
	if th.Type != chanThread || th.Name != "Organisation du tournoi de samedi" || *th.ParentID != gen || *th.ThreadStarter != start.ID {
		t.Fatalf("thread = %+v", th)
	}
	c.expect(409, "thread_exists", c.call("POST", fmt.Sprintf("/v1/channels/%d/messages/%d/threads", gen, start.ID), c.owner, map[string]any{}, nil))
	inThread := c.post(c.tok("bob"), th.ID, map[string]any{"content": "je m'inscris"})
	c.expect(400, "nested_thread", c.call("POST", fmt.Sprintf("/v1/channels/%d/messages/%d/threads", th.ID, inThread.ID), c.owner, map[string]any{}, nil))
	var msgs []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.owner, nil, &msgs))
	if msgs[0].ThreadID == nil || *msgs[0].ThreadID != th.ID {
		t.Fatalf("starter message does not point to its thread: %+v", msgs[0])
	}
	// A thread follows its channel's permissions.
	c.expect(200, "", c.override(c.owner, gen, "member", c.id("carol"), nil, []string{"view_channel"}))
	c.expect(404, "not_found", c.call("GET", fmt.Sprint("/v1/channels/", th.ID, "/messages"), c.tok("carol"), nil, nil))
	// Deleting the channel deletes its threads.
	c.expect(204, "", c.call("DELETE", fmt.Sprint("/v1/channels/", gen), c.owner, nil, nil))
	c.expect(404, "not_found", c.call("GET", fmt.Sprint("/v1/channels/", th.ID, "/messages"), c.owner, nil, nil))

	// Announcement channels: reading for all, posting needs manage_messages.
	var ann channel
	c.expect(201, "", c.call("POST", "/v1/channels", c.owner, map[string]any{"type": "announcement", "name": "annonces"}, &ann))
	if ann.Type != chanAnnouncement {
		t.Fatalf("announcement = %+v", ann)
	}
	c.expect(403, "missing_permissions", c.call("POST", fmt.Sprint("/v1/channels/", ann.ID, "/messages"), c.tok("bob"), map[string]any{"content": "spam"}, nil))
	c.post(c.owner, ann.ID, map[string]any{"content": "Mise à jour ce soir"})
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", ann.ID, "/messages"), c.tok("bob"), nil, &msgs))

	// Notification settings (stored for the member's clients).
	var settings []notificationSetting
	c.expect(200, "", c.call("PUT", fmt.Sprint("/v1/notification-settings/", ann.ID), c.tok("bob"), map[string]any{"level": "none", "mute_for": 3600}, &settings))
	c.expect(200, "", c.call("PUT", "/v1/notification-settings/0", c.tok("bob"), map[string]any{"level": "mentions"}, &settings))
	if len(settings) != 2 || settings[0].ChannelID != 0 || settings[1].Level != "none" || settings[1].MutedUntil == nil {
		t.Fatalf("settings = %+v", settings)
	}
	c.expect(200, "", c.call("PUT", fmt.Sprint("/v1/notification-settings/", ann.ID), c.tok("bob"), map[string]any{"level": "default"}, &settings))
	if len(settings) != 1 {
		t.Fatalf("default level should remove the channel setting: %+v", settings)
	}
	c.expect(200, "", c.call("PUT", fmt.Sprint("/v1/notification-settings/", ann.ID), c.tok("bob"), map[string]any{"level": "all", "mute_for": -1}, &settings))
	if settings[1].MutedUntil == nil || settings[1].MutedUntil.Year() != 9999 {
		t.Fatalf("indefinite mute = %+v", settings[1])
	}
	c.expect(400, "invalid_level", c.call("PUT", "/v1/notification-settings/0", c.tok("bob"), map[string]any{"level": "fort"}, nil))

	// Roles shown separately.
	var rl role
	c.expect(201, "", c.call("POST", "/v1/roles", c.owner, map[string]any{"name": "Staff", "hoist": true}, &rl))
	if !rl.Hoist {
		t.Fatal("hoist not stored")
	}
}
