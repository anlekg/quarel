package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/internal/sqlitedb"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestMigrationToConversations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.db")
	old, err := sqlitedb.Open(path, migrations[:5]) // before group conversations
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, pseudo, pseudo_norm, password_hash, created_at) VALUES ('a', 'a@x', 'a', 'a', 'h', 1), ('b', 'b@x', 'b', 'b', 'h', 1)`,
		`INSERT INTO dms (id, user_a, user_b, created_at) VALUES ('dm1', 'a', 'b', 5)`,
		`INSERT INTO dm_events (id, dm_id, sender_user, sender_device, created_at) VALUES (42, 'dm1', 'a', 'dev', 6)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kind string
	var members, pairs int
	var event string
	db.QueryRow(`SELECT kind FROM conversations WHERE id = 'dm1'`).Scan(&kind)
	db.QueryRow(`SELECT COUNT(*) FROM conversation_members WHERE conversation_id = 'dm1'`).Scan(&members)
	db.QueryRow(`SELECT COUNT(*) FROM direct_pairs WHERE conversation_id = 'dm1'`).Scan(&pairs)
	db.QueryRow(`SELECT conversation_id FROM conv_events WHERE id = 42`).Scan(&event)
	if kind != "direct" || members != 2 || pairs != 1 || event != "dm1" {
		t.Fatalf("migrated: kind %q, %d members, %d pairs, event in %q", kind, members, pairs, event)
	}
	// New events keep counting after the migrated ones.
	res, err := db.Exec(`INSERT INTO conv_events (conversation_id, sender_user, sender_device, created_at) VALUES ('dm1', 'b', 'dev', 7)`)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := res.LastInsertId(); id <= 42 {
		t.Fatalf("new event id %d reuses a migrated one", id)
	}
}

func TestGroups(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	dave, _ := e.registerVerified("dave@example.com", "dave", "mot-de-passe-dave")
	e.befriend(alice, bob, "bob")
	e.befriend(alice, carol, "carol")
	e.befriend(bob, dave, "dave")

	// Groups are created among friends; bob and carol need not be friends.
	e.expect(403, "not_friends", e.call("POST", "/v1/dms", alice.SessionToken, map[string]any{"user_ids": []string{bob.User.ID, dave.User.ID}, "name": "x"}, nil))
	e.expect(400, "invalid_name", e.call("POST", "/v1/dms", alice.SessionToken, map[string]any{"user_ids": []string{bob.User.ID}, "name": " "}, nil))
	var g convJSON
	e.expect(201, "", e.call("POST", "/v1/dms", alice.SessionToken, map[string]any{"user_ids": []string{bob.User.ID, carol.User.ID}, "name": "Tarot"}, &g))
	if g.Kind != "group" || len(g.Members) != 3 || *g.OwnerID != alice.User.ID {
		t.Fatalf("group = %+v", g)
	}
	// Co-members can exchange keys and messages without being friends.
	e.expect(200, "", e.call("GET", "/v1/users/"+carol.User.ID+"/keys", bob.SessionToken, nil, nil))
	e.expect(403, "not_friends", e.call("GET", "/v1/users/"+dave.User.ID+"/keys", carol.SessionToken, nil, nil))

	// Members add their own friends; the owner removes; anyone leaves.
	e.expect(403, "not_friends", e.call("PUT", "/v1/dms/"+g.ID+"/members/"+dave.User.ID, carol.SessionToken, nil, nil))
	e.expect(200, "", e.call("PUT", "/v1/dms/"+g.ID+"/members/"+dave.User.ID, bob.SessionToken, nil, &g))
	if len(g.Members) != 4 {
		t.Fatalf("members = %d", len(g.Members))
	}
	e.expect(403, "not_owner", e.call("DELETE", "/v1/dms/"+g.ID+"/members/"+dave.User.ID, bob.SessionToken, nil, nil))
	e.expect(204, "", e.call("DELETE", "/v1/dms/"+g.ID+"/members/"+dave.User.ID, alice.SessionToken, nil, nil))
	e.expect(404, "not_found", e.call("POST", "/v1/dms/"+g.ID+"/messages", dave.SessionToken, map[string]string{"payload": "x"}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/dms/"+g.ID, carol.SessionToken, map[string]string{"name": "Tarot du jeudi"}, &g))
	// The owner leaves: the group goes to the longest-standing member.
	e.expect(204, "", e.call("DELETE", "/v1/dms/"+g.ID+"/members/@me", alice.SessionToken, nil, nil))
	var list []convJSON
	e.expect(200, "", e.call("GET", "/v1/dms", bob.SessionToken, nil, &list))
	if len(list) != 1 || list[0].Name != "Tarot du jeudi" || *list[0].OwnerID != bob.User.ID || len(list[0].Members) != 2 {
		t.Fatalf("after the owner left: %+v", list)
	}
	e.expect(400, "not_a_group", e.call("PUT", "/v1/dms/"+func() string {
		var d convJSON
		e.call("POST", "/v1/dms", bob.SessionToken, map[string]string{"user_id": dave.User.ID}, &d)
		return d.ID
	}()+"/members/"+alice.User.ID, bob.SessionToken, nil, nil))
}

func TestTypingReadAndPrivacy(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	e.befriend(alice, bob, "bob")
	var dm convJSON
	e.expect(200, "", e.call("POST", "/v1/dms", alice.SessionToken, map[string]string{"user_id": bob.User.ID}, &dm))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.http.URL, "http")+"/v1/gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	wsjson.Write(ctx, conn, map[string]string{"op": "auth", "token": bob.SessionToken})
	next := func() (string, map[string]any) {
		for {
			var ev struct {
				T string
				D map[string]any
			}
			if err := wsjson.Read(ctx, conn, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.T == "DM_TYPING" || ev.T == "DM_READ" {
				return ev.T, ev.D
			}
		}
	}
	e.expect(204, "", e.call("POST", "/v1/dms/"+dm.ID+"/typing", alice.SessionToken, nil, nil))
	if typ, d := next(); typ != "DM_TYPING" || d["user_id"] != alice.User.ID {
		t.Fatalf("%s %v", typ, d)
	}
	e.expect(204, "", e.call("POST", "/v1/dms/"+dm.ID+"/read", alice.SessionToken, map[string]any{"event_id": 7}, nil))
	if typ, d := next(); typ != "DM_READ" || d["event_id"] != float64(7) {
		t.Fatalf("%s %v", typ, d)
	}
	// Turned off: nothing reaches bob (the next event he gets is the one after re-enabling).
	var p map[string]bool
	e.expect(200, "", e.call("PATCH", "/v1/me/privacy", alice.SessionToken, map[string]bool{"typing": false, "read_receipts": false}, &p))
	if p["typing"] || p["read_receipts"] {
		t.Fatalf("privacy = %v", p)
	}
	e.clock = e.clock.Add(5 * time.Second)
	e.call("POST", "/v1/dms/"+dm.ID+"/typing", alice.SessionToken, nil, nil)
	e.call("POST", "/v1/dms/"+dm.ID+"/read", alice.SessionToken, map[string]any{"event_id": 8}, nil)
	e.expect(200, "", e.call("PATCH", "/v1/me/privacy", alice.SessionToken, map[string]bool{"read_receipts": true}, nil))
	e.call("POST", "/v1/dms/"+dm.ID+"/read", alice.SessionToken, map[string]any{"event_id": 9}, nil)
	if typ, d := next(); typ != "DM_READ" || d["event_id"] != float64(9) {
		t.Fatalf("a disabled receipt or typing leaked: %s %v", typ, d)
	}
	// Non-members get nothing.
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	e.expect(404, "not_found", e.call("POST", "/v1/dms/"+dm.ID+"/typing", carol.SessionToken, nil, nil))
}

func TestConversationFiles(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.DMFileMaxBytes = 1 << 10
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	bob2 := e.login2("bob@example.com", "mot-de-passe-bob", "téléphone")
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	e.befriend(alice, bob, "bob")
	// Devices with published keys (the server keeps copies for devices only).
	_, aliceMaster, _ := ed25519.GenerateKey(nil)
	_, bobMaster, _ := ed25519.GenerateKey(nil)
	a1, b1, b2 := newTestDevice(alice), newTestDevice(bob), newTestDevice(bob2)
	e.expect(200, "", e.upload(a1, aliceMaster, true))
	e.expect(200, "", e.upload(b1, bobMaster, true))
	e.expect(200, "", e.upload(b2, bobMaster, false))
	var dm convJSON
	e.expect(200, "", e.call("POST", "/v1/dms", alice.SessionToken, map[string]string{"user_id": bob.User.ID}, &dm))

	do := func(method, path, token string, body []byte) (int, []byte) {
		req, _ := http.NewRequest(method, e.http.URL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	upload := func(query string) (string, int) {
		code, body := do("POST", "/v1/dms/"+dm.ID+"/files"+query, alice.SessionToken, bytes.Repeat([]byte{0xAB}, 100))
		var out struct {
			ID      string `json:"id"`
			Pending int    `json:"pending_devices"`
		}
		json.Unmarshal(body, &out)
		if code != 201 {
			t.Fatalf("upload%s: %d %s", query, code, body)
		}
		return out.ID, out.Pending
	}
	// By default, the copy waits for every other device of the conversation.
	id, pending := upload("")
	if pending != 2 {
		t.Fatalf("pending = %d, want bob's 2 devices", pending)
	}
	if code, got := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, bob.SessionToken, nil); code != 200 || len(got) != 100 {
		t.Fatalf("download: %d", code)
	}
	if code, _ := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, carol.SessionToken, nil); code != 404 {
		t.Fatalf("non-member download: %d", code)
	}
	// Deleted as soon as every waiting device acknowledged it.
	do("POST", "/v1/dms/"+dm.ID+"/files/"+id+"/ack", bob.SessionToken, nil)
	if code, _ := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, bob2.SessionToken, nil); code != 200 {
		t.Fatal("deleted while a device still waits for it")
	}
	do("POST", "/v1/dms/"+dm.ID+"/files/"+id+"/ack", bob2.SessionToken, nil)
	if code, _ := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, bob2.SessionToken, nil); code != 404 {
		t.Fatal("copy kept after every device had it")
	}
	if entries, _ := os.ReadDir(e.srv.filesDir()); len(entries) != 0 {
		t.Fatalf("%d file(s) left on disk", len(entries))
	}

	// Only for the devices that could not get it peer to peer.
	id, pending = upload("?for=" + b2.id())
	if pending != 1 {
		t.Fatalf("pending = %d", pending)
	}
	if code, _ := do("POST", "/v1/dms/"+dm.ID+"/files?for=someone-else", alice.SessionToken, []byte("x")); code != 400 {
		t.Fatalf("copy for a device outside the conversation: %d", code)
	}
	if code, _ := do("POST", "/v1/dms/"+dm.ID+"/files", alice.SessionToken, bytes.Repeat([]byte{1}, 2<<10)); code != 413 {
		t.Fatalf("oversized upload: %d", code)
	}
	if code, _ := do("DELETE", "/v1/dms/"+dm.ID+"/files/"+id, bob.SessionToken, nil); code != 403 {
		t.Fatalf("delete by another member: %d", code)
	}
	// A revoked device no longer holds the copy back.
	e.expect(204, "", e.call("DELETE", "/v1/me/sessions/"+b2.id(), bob.SessionToken, nil, nil))
	if err := e.srv.CleanupFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code, _ := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, bob.SessionToken, nil); code != 404 {
		t.Fatalf("copy kept for a revoked device: %d", code)
	}
	// And copies never outlive the cap (7 days by default).
	id, _ = upload("")
	e.clock = e.clock.Add(e.srv.cfg.DMFileTTL + time.Hour)
	e.srv.CleanupFiles(context.Background())
	if code, _ := do("GET", "/v1/dms/"+dm.ID+"/files/"+id, bob.SessionToken, nil); code != 404 {
		t.Fatalf("expired copy still served: %d", code)
	}
}
