package identity

import (
	"context"
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
