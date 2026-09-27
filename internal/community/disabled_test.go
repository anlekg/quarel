package community

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

func TestDisabledAccounts(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	ctx := context.Background()
	bob := c.users["bob"]

	if err := c.srv.ApplyDisabled(ctx, testIssuer, []string{idtoken.AccountHash(bob.sub)}); err != nil {
		t.Fatal(err)
	}
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, nil))
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("carol"), nil, nil))
	var lr loginResp
	c.expect(403, "account_disabled", c.login(bob, loginOpts{}, &lr))

	// Re-enabled: bob comes back as the same member.
	if err := c.srv.ApplyDisabled(ctx, testIssuer, nil); err != nil {
		t.Fatal(err)
	}
	back := c.mustLogin(bob, loginOpts{})
	if back.Member.ID != c.id("bob") {
		t.Fatal("re-enabled account came back as another member")
	}
}

// A session whose device logged out (or was revoked) on its identity
// service ends here too; a later sign-in of the same device is not affected.
func TestEndedDevices(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	ctx := context.Background()
	bob := c.users["bob"]
	dkey := idtoken.EncodeKey(bob.device.Public().(ed25519.PublicKey))
	ended := []endedDevice{{Hash: idtoken.DeviceHash(dkey), At: c.clock.Add(time.Second)}}

	if err := c.srv.ApplyEnded(ctx, testIssuer, ended); err != nil {
		t.Fatal(err)
	}
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, nil))
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("carol"), nil, nil))

	// A token obtained before the session ended (still valid) opens nothing new.
	stale := bob.token(c.clock, time.Hour)
	c.clock = c.clock.Add(time.Minute)
	var lr loginResp
	c.expect(401, "session_ended", c.login(bob, loginOpts{token: stale}, &lr))

	again := c.mustLogin(bob, loginOpts{})
	if err := c.srv.ApplyEnded(ctx, testIssuer, ended); err != nil {
		t.Fatal(err)
	}
	c.expect(200, "", c.call("GET", "/v1/members/@me", again.SessionToken, nil, nil))
}

func TestFetchDisabled(t *testing.T) {
	issuer := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/disabled-accounts" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer,
			"accounts":      []map[string]string{{"h": "abc", "since": "2026-09-24T10:00:00Z"}},
			"ended_devices": []map[string]string{{"h": "def", "at": "2026-09-24T11:00:00Z"}}})
	}))
	defer srv.Close()
	issuer = strings.TrimPrefix(srv.URL, "http://") // 127.0.0.1:port → plain HTTP, like a local issuer
	list, err := fetchDisabled(context.Background(), srv.Client(), issuer)
	if err != nil || len(list.Accounts) != 1 || list.Accounts[0].Hash != "abc" || len(list.Devices) != 1 || list.Devices[0].Hash != "def" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := fetchDisabled(context.Background(), srv.Client(), "localhost"+issuer[strings.Index(issuer, ":"):]); err == nil {
		t.Fatal("a list claiming another issuer was accepted")
	}
}

// An account deleted on its identity service: its member is anonymised
// ("Ancien compte", no nickname, no roles, gone), its messages stay; a
// deleted owner leaves the server without one (a new claim code).
func TestDeletedAccounts(t *testing.T) {
	c := newCommunity(t, "bob", "carol")
	ctx := context.Background()
	gen := c.channelID(c.owner, "général")
	role := c.createRole(c.owner, "Tarot", "send_messages")
	c.expect(200, "", c.assign(c.owner, c.id("bob"), role))
	c.expect(200, "", c.call("PATCH", "/v1/members/@me", c.tok("bob"), map[string]any{"nickname": "Bobby"}, nil))
	c.expect(201, "", c.call("POST", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("bob"), map[string]any{"content": "au revoir"}, nil))
	var ownerSub string
	c.srv.db.QueryRow(`SELECT subject FROM members WHERE is_owner = 1`).Scan(&ownerSub)

	at := []endedDevice{{Hash: idtoken.AccountHash(c.users["bob"].sub), At: c.clock}}
	if err := c.srv.ApplyDeleted(ctx, testIssuer, at); err != nil {
		t.Fatal(err)
	}
	var handle string
	var nick sql.NullString
	var left sql.NullInt64
	c.srv.db.QueryRow(`SELECT handle, nickname, left_at FROM members WHERE id = ?`, c.id("bob")).Scan(&handle, &nick, &left)
	if handle != "" || nick.Valid || !left.Valid {
		t.Fatalf("bob after deletion: %q %v %v", handle, nick, left)
	}
	var roles int
	c.srv.db.QueryRow(`SELECT COUNT(*) FROM member_roles WHERE member_id = ?`, c.id("bob")).Scan(&roles)
	if roles != 0 {
		t.Fatal("roles kept")
	}
	c.expect(401, "unauthorized", c.call("GET", "/v1/members/@me", c.tok("bob"), nil, nil))
	var hist []message
	c.expect(200, "", c.call("GET", fmt.Sprint("/v1/channels/", gen, "/messages"), c.tok("carol"), nil, &hist))
	if last := hist[len(hist)-1]; last.Content != "au revoir" || last.AuthorID != c.id("bob") {
		t.Fatalf("message = %+v", last)
	}
	m, _ := memberBy(ctx, c.srv.db, `id = ?`, c.id("bob"))
	if m.json().DisplayName != deletedName {
		t.Fatalf("display name = %q", m.json().DisplayName)
	}
	// Applying the list again changes nothing; carol stays.
	if err := c.srv.ApplyDeleted(ctx, testIssuer, at); err != nil {
		t.Fatal(err)
	}
	c.expect(200, "", c.call("GET", "/v1/members/@me", c.tok("carol"), nil, nil))

	// The owner deletes their account: no owner any more, a claim code.
	if err := c.srv.ApplyDeleted(ctx, testIssuer, []endedDevice{{Hash: idtoken.AccountHash(ownerSub), At: c.clock}}); err != nil {
		t.Fatal(err)
	}
	var owners int
	c.srv.db.QueryRow(`SELECT COUNT(*) FROM members WHERE is_owner = 1`).Scan(&owners)
	if owners != 0 {
		t.Fatal("a deleted account still owns the server")
	}
	if code, _ := c.srv.PrepareClaim(ctx); code == "" {
		t.Fatal("no claim code for a server without owner")
	}
}
