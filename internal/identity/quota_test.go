package identity

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/anlekg/quarel/internal/httpapi"
)

// What waits for a device is bounded, in all and per sender, so no contact
// can fill it (or the server's disk) alone; old items expire.
func TestInboxLimits(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.InboxMaxBytes = 4000
	ctx := context.Background()
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	device := alice.SessionID
	send := func(from loginResp, n int) error {
		_, err := e.srv.deliver(ctx, e.srv.db, device, inboxItem{Kind: "to_device", SenderUser: from.User.ID, SenderDevice: from.SessionID, Payload: strings.Repeat("x", n)})
		return err
	}
	if err := send(bob, 900); err != nil {
		t.Fatal(err)
	}
	if err := send(bob, 200); !errors.Is(err, errInboxFull) { // bob's quarter (1000) is used up
		t.Fatalf("second message from bob: %v", err)
	}
	if err := send(carol, 900); err != nil {
		t.Fatalf("another sender is not affected: %v", err)
	}
	if err := send(alice, 2000); err != nil { // the account's own devices (history transfer)
		t.Fatalf("own device: %v", err)
	}
	if err := send(alice, 500); !errors.Is(err, errInboxFull) { // 3800 + 500 > 4000
		t.Fatalf("over the device's total: %v", err)
	}

	// Items waiting longer than QUAREL_INBOX_TTL go.
	e.clock = e.clock.Add(e.srv.cfg.InboxTTL + 1)
	e.srv.cleanup(ctx)
	var n int
	e.srv.db.QueryRow(`SELECT COUNT(*) FROM inbox WHERE session_id = ?`, device).Scan(&n)
	if n != 0 {
		t.Fatalf("%d expired items left", n)
	}
}

// A full inbox does not block a to-device batch: the other devices get
// their messages (a group's keys, for instance).
func TestToDeviceSkipsFullInbox(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.InboxMaxBytes = 4000
	ctx := context.Background()
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	bob, _ := e.registerVerified("bob@example.com", "bob", "mot-de-passe-bob")
	carol, _ := e.registerVerified("carol@example.com", "carol", "mot-de-passe-carol")
	e.befriend(bob, alice, "alice")
	e.befriend(bob, carol, "carol")
	for _, lr := range []loginResp{alice, carol} { // devices exist once their keys are published
		_, master, _ := ed25519.GenerateKey(nil)
		e.expect(200, "", e.upload(newTestDevice(lr), master, true))
	}
	// bob's quarter of alice's inbox is used up.
	if _, err := e.srv.deliver(ctx, e.srv.db, alice.SessionID, inboxItem{Kind: "to_device", SenderUser: bob.User.ID, SenderDevice: bob.SessionID, Payload: strings.Repeat("x", 1000)}); err != nil {
		t.Fatal(err)
	}
	msg := map[string]any{"messages": []map[string]string{
		{"device_id": alice.SessionID, "payload": "room-key-for-alice"},
		{"device_id": carol.SessionID, "payload": "room-key-for-carol"},
	}}
	e.expect(204, "", e.call("POST", "/v1/to-device", bob.SessionToken, msg, nil))
	count := func(device, payload string) (n int) {
		e.srv.db.QueryRow(`SELECT COUNT(*) FROM inbox WHERE session_id = ? AND payload = ?`, device, payload).Scan(&n)
		return
	}
	if count(carol.SessionID, "room-key-for-carol") != 1 {
		t.Fatal("carol did not get her message because alice's inbox was full")
	}
	if count(alice.SessionID, "room-key-for-alice") != 0 {
		t.Fatal("a message was queued in a full inbox")
	}
}

func TestFileQuota(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.DMFileQuota = 1000
	ctx := context.Background()
	alice, _ := e.registerVerified("alice@example.com", "alice", "mot-de-passe-alice")
	if err := e.srv.fileRoom(ctx, alice.User.ID, 900); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.fileRoom(ctx, alice.User.ID, 1100); !httpapi.IsCode(err, "file_quota_exceeded") {
		t.Fatalf("over quota: %v", err)
	}
}
