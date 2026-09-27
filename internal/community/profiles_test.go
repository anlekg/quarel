package community

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (c *community) putRaw(token, path string, data []byte) result {
	c.t.Helper()
	req, _ := http.NewRequest("PUT", c.http.URL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var er struct{ Error struct{ Code string } }
	json.Unmarshal(raw, &er)
	return result{resp.StatusCode, er.Error.Code}
}

type memberProfile struct {
	Bio      string          `json:"bio"`
	Theme    json.RawMessage `json:"theme"`
	ProfileV int64           `json:"profile_v"`
	AvatarV  int64           `json:"avatar_v"`
	BannerV  int64           `json:"banner_v"`
}

func TestMemberProfiles(t *testing.T) {
	c := newCommunity(t, "bob", "carol", "mod")
	bob := c.id("bob")
	events := c.gatewayEvents(context.Background(), c.tok("carol"))

	// Bio and theme, partial updates.
	var me memberJSON
	c.expect(200, "", c.call("PATCH", "/v1/members/@me", c.tok("bob"), map[string]any{"nickname": "Bobby"}, &me))
	c.expect(200, "", c.call("PATCH", "/v1/members/@me", c.tok("bob"), map[string]any{
		"bio": "  Joueur de tarot  ", "theme": map[string]any{"colors": map[string]string{"accent": "#ff8800"}, "css": ".card { color: #fff }"},
	}, &me))
	if me.Nickname == nil || *me.Nickname != "Bobby" || me.ProfileV == 0 {
		t.Fatalf("nickname lost or no profile version: %+v", me)
	}
	var upd memberJSON
	json.Unmarshal(events("MEMBER_UPDATE"), &upd)
	if upd.ID != bob {
		t.Fatalf("MEMBER_UPDATE = %+v", upd)
	}
	var p memberProfile
	c.expect(200, "", c.call("GET", "/v1/members/"+bob+"/profile", c.tok("carol"), nil, &p))
	if p.Bio != "Joueur de tarot" || !strings.Contains(string(p.Theme), "#ff8800") || p.ProfileV != me.ProfileV {
		t.Fatalf("profile = %+v %s", p, p.Theme)
	}
	c.expect(400, "invalid_theme", c.call("PATCH", "/v1/members/@me", c.tok("bob"),
		map[string]any{"theme": map[string]any{"css": "a { background: url(//evil.example/x) }"}}, nil))
	c.expect(400, "invalid_bio", c.call("PATCH", "/v1/members/@me", c.tok("bob"), map[string]any{"bio": strings.Repeat("x", 501)}, nil))
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/"+bob+"/profile", "", nil, nil))

	// Automatic moderation applies to the bio.
	c.expect(200, "", c.call("PUT", "/v1/server/automod", c.owner, map[string]any{"words": []string{"arnaque"}}, nil))
	c.expect(403, "automod_word", c.call("PATCH", "/v1/members/@me", c.tok("bob"), map[string]any{"bio": "une arnaque"}, nil))

	// Avatar and banner on this server.
	pic := tinyPNG()
	c.expect(400, "invalid_image", c.putRaw(c.tok("bob"), "/v1/members/@me/avatar", []byte("<svg/>")))
	c.expect(413, "file_too_large", c.putRaw(c.tok("bob"), "/v1/members/@me/avatar", append(tinyPNG(), make([]byte, maxMemberAvatar)...)))
	c.expect(200, "", c.putRaw(c.tok("bob"), "/v1/members/@me/avatar", pic))
	c.expect(200, "", c.putRaw(c.tok("bob"), "/v1/members/@me/banner", pic))
	resp, body := c.download(c.tok("carol"), "/v1/members/"+bob+"/avatar")
	if resp.StatusCode != 200 || !bytes.Equal(body, pic) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("avatar: %d %v", resp.StatusCode, resp.Header)
	}
	var list []memberJSON
	c.expect(200, "", c.call("GET", "/v1/members", c.tok("carol"), nil, &list))
	for _, m := range list {
		if m.ID == bob && (m.AvatarV == 0 || m.BannerV == 0) {
			t.Fatalf("versions missing in the member list: %+v", m)
		}
	}
	c.expect(204, "", c.call("DELETE", "/v1/members/@me/banner", c.tok("bob"), nil, nil))
	if resp, _ := c.download(c.tok("carol"), "/v1/members/"+bob+"/banner"); resp.StatusCode != 404 {
		t.Fatalf("banner still served: %d", resp.StatusCode)
	}

	// The moderation resets it: moderate_members and the hierarchy.
	c.expect(403, "missing_permissions", c.call("DELETE", "/v1/members/"+bob+"/profile", c.tok("carol"), map[string]any{}, nil))
	c.assign(c.owner, c.id("mod"), c.createRole(c.owner, "Modo", "moderate_members"))
	c.expect(204, "", c.call("DELETE", "/v1/members/"+bob+"/profile", c.tok("mod"), map[string]any{"reason": "image choquante"}, nil))
	p = memberProfile{}
	c.expect(200, "", c.call("GET", "/v1/members/"+bob+"/profile", c.tok("carol"), nil, &p))
	if p.Bio != "" || p.Theme != nil || p.AvatarV != 0 {
		t.Fatalf("profile after reset = %+v", p)
	}
	if resp, _ := c.download(c.tok("carol"), "/v1/members/"+bob+"/avatar"); resp.StatusCode != 404 {
		t.Fatalf("avatar still served: %d", resp.StatusCode)
	}
	c.expect(403, "role_hierarchy", c.call("DELETE", "/v1/members/"+c.owner0()+"/profile", c.tok("mod"), map[string]any{}, nil))
	if got := c.auditActions(c.owner, "?action=member_profile_reset"); len(got) != 1 {
		t.Fatalf("audit = %v", got)
	}
}

func TestServerTheme(t *testing.T) {
	c := newCommunity(t, "bob")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events := c.gatewayEvents(ctx, c.tok("bob"))
	events("READY")

	th := map[string]any{"colors": map[string]string{"bg": "#101820", "accent": "#e0555d"}, "gradient": map[string]any{"angle": 160, "stops": []string{"#101820", "#302030"}},
		"font": "space-grotesk", "css": ".message { border-radius: 12px }"}
	c.expect(403, "missing_permissions", c.call("PUT", "/v1/server/theme", c.tok("bob"), map[string]any{"theme": th}, nil))
	c.expect(400, "invalid_theme", c.call("PUT", "/v1/server/theme", c.owner, map[string]any{"theme": map[string]any{"css": "@import 'x.css';"}}, nil))
	var got serverTheme
	c.expect(200, "", c.call("PUT", "/v1/server/theme", c.owner, map[string]any{"theme": th}, &got))
	var ev serverTheme
	json.Unmarshal(events("THEME_UPDATE"), &ev)
	if !strings.Contains(string(ev.Theme), "space-grotesk") || ev.BackgroundV != 0 {
		t.Fatalf("THEME_UPDATE = %+v", ev)
	}

	// The background image.
	c.expect(403, "missing_permissions", c.putRaw(c.tok("bob"), "/v1/server/theme/background", tinyPNG()))
	c.expect(200, "", c.putRaw(c.owner, "/v1/server/theme/background", tinyPNG()))
	json.Unmarshal(events("THEME_UPDATE"), &ev)
	if ev.BackgroundV == 0 {
		t.Fatalf("no background version: %+v", ev)
	}
	if resp, body := c.download(c.tok("bob"), "/v1/server/theme/background"); resp.StatusCode != 200 || !bytes.Equal(body, tinyPNG()) {
		t.Fatalf("background: %d", resp.StatusCode)
	}

	// READY of a new connection carries it.
	ready := c.gatewayEvents(ctx, c.tok("bob"))
	var r struct {
		Theme serverTheme `json:"theme"`
	}
	json.Unmarshal(ready("READY"), &r)
	if !strings.Contains(string(r.Theme.Theme), "#e0555d") || r.Theme.BackgroundV != ev.BackgroundV {
		t.Fatalf("READY theme = %+v", r.Theme)
	}

	// Removed: {} and DELETE.
	c.expect(200, "", c.call("PUT", "/v1/server/theme", c.owner, map[string]any{"theme": map[string]any{}}, nil))
	c.expect(200, "", c.call("DELETE", "/v1/server/theme/background", c.owner, nil, &got))
	if (got.Theme != nil && string(got.Theme) != "null") || got.BackgroundV != 0 {
		t.Fatalf("theme after removal = %+v", got)
	}
}
