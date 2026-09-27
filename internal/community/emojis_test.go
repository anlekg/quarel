package community

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func (c *community) postEmoji(token, name string, data []byte) (emojiJSON, result) {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.http.URL+"/v1/emojis?name="+url.QueryEscape(name), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var e emojiJSON
	var er struct{ Error struct{ Code string } }
	json.Unmarshal(raw, &e)
	json.Unmarshal(raw, &er)
	return e, result{resp.StatusCode, er.Error.Code}
}

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{95, 184, 165, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestCustomEmojis(t *testing.T) {
	c := newCommunity(t, "bob")
	gen := c.channelID(c.owner, "général")
	pic := tinyPNG()

	_, res := c.postEmoji(c.tok("bob"), "chat", pic)
	c.expect(403, "missing_permissions", res)
	_, res = c.postEmoji(c.owner, "Chat!", pic)
	c.expect(400, "invalid_name", res)
	_, res = c.postEmoji(c.owner, "texte", []byte("pas une image"))
	c.expect(400, "invalid_image", res)
	_, res = c.postEmoji(c.owner, "gros", append(tinyPNG(), make([]byte, maxEmojiBytes)...))
	c.expect(413, "too_large", res)
	e, res := c.postEmoji(c.owner, "chat_vert", pic)
	c.expect(201, "", res)
	_, res = c.postEmoji(c.owner, "chat_vert", pic)
	c.expect(409, "name_taken", res)

	var list []emojiJSON
	c.expect(200, "", c.call("GET", "/v1/emojis", c.tok("bob"), nil, &list))
	if len(list) != 1 || list[0] != e {
		t.Fatalf("emojis = %+v", list)
	}
	resp, body := c.download(c.tok("bob"), "/v1/emojis/"+e.ID)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, pic) {
		t.Fatalf("image: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp, _ := c.download("", "/v1/emojis/"+e.ID); resp.StatusCode != 401 {
		t.Fatalf("image without a session: %d", resp.StatusCode)
	}

	// Reactions: the server's own emojis, not made-up ones.
	msg := c.post(c.tok("bob"), gen, map[string]any{"content": "regardez <:chat_vert:" + e.ID + ">"})
	react := func(emoji string) result {
		return c.call("PUT", fmt.Sprintf("/v1/channels/%d/messages/%d/reactions/%s", gen, msg.ID, url.PathEscape(emoji)), c.tok("bob"), nil, nil)
	}
	c.expect(204, "", react("<:chat_vert:"+e.ID+">"))
	c.expect(400, "invalid_emoji", react("<:autre:"+e.ID+">"))
	c.expect(400, "invalid_emoji", react("<:chat_vert:inconnu12345>"))

	// Deleted: gone from the list and the disk.
	c.expect(204, "", c.call("DELETE", "/v1/emojis/"+e.ID, c.owner, nil, nil))
	if resp, _ := c.download(c.tok("bob"), "/v1/emojis/"+e.ID); resp.StatusCode != 404 {
		t.Fatalf("deleted image: %d", resp.StatusCode)
	}
	c.expect(200, "", c.call("GET", "/v1/emojis", c.tok("bob"), nil, &list))
	if len(list) != 0 {
		t.Fatalf("emojis after deletion = %+v", list)
	}
}
