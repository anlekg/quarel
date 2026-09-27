package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/anlekg/quarel/pkg/e2ekeys"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var b64 = base64.RawStdEncoding

// testDevice simulates a client device's key material (the real client uses Olm).
type testDevice struct {
	login  loginResp
	ed     ed25519.PrivateKey
	edPub  string
	curve  string
	userID string
}

func newTestDevice(lr loginResp) *testDevice {
	pub, priv, _ := ed25519.GenerateKey(nil)
	c := make([]byte, 32)
	rand.Read(c)
	return &testDevice{login: lr, ed: priv, edPub: b64.EncodeToString(pub), curve: b64.EncodeToString(c), userID: lr.User.ID}
}

func (d *testDevice) id() string    { return d.login.SessionID }
func (d *testDevice) token() string { return d.login.SessionToken }

func sign(k ed25519.PrivateKey, msg []byte) string { return b64.EncodeToString(ed25519.Sign(k, msg)) }

// upload publishes the device keys; master is set for the account's first device.
func (e *testEnv) upload(d *testDevice, master ed25519.PrivateKey, withMaster bool) result {
	body := map[string]string{
		"curve25519": d.curve, "ed25519": d.edPub,
		"signature": sign(d.ed, e2ekeys.DeviceKeys(d.userID, d.id(), d.curve, d.edPub)),
	}
	if withMaster {
		body["master_key"] = b64.EncodeToString(master.Public().(ed25519.PublicKey))
		body["master_signature"] = sign(master, e2ekeys.DeviceCert(d.userID, d.id(), d.edPub))
	}
	return e.call("POST", "/v1/keys/device", d.token(), body, nil)
}

func (e *testEnv) login2(email, password, name string) loginResp {
	e.t.Helper()
	_, pub := deviceKey(e.t)
	var lr loginResp
	e.expect(200, "", e.call("POST", "/v1/auth/login", "", map[string]string{"login": email, "password": password, "device_name": name, "device_key": pub}, &lr))
	return lr
}

func (e *testEnv) befriend(a, b loginResp, pseudoB string) {
	e.t.Helper()
	e.expect(200, "", e.call("POST", "/v1/friends", a.SessionToken, map[string]string{"pseudo": pseudoB}, nil))
	e.expect(200, "", e.call("POST", "/v1/friends/"+a.User.ID+"/accept", b.SessionToken, nil, nil))
}

func TestFriends(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	alice, _ := e.registerVerified("a@example.com", "alice", pw)
	bob, _ := e.registerVerified("b@example.com", "bob", pw)
	carol, _ := e.registerVerified("c@example.com", "carol", pw)

	e.expect(404, "not_found", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "nobody"}, nil))
	e.expect(400, "self_friend", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "ALICE"}, nil))

	var rel struct{ Status string }
	e.expect(200, "", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "Bob"}, &rel))
	if rel.Status != relOutgoing {
		t.Fatalf("status = %q", rel.Status)
	}
	e.expect(409, "already_requested", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "bob"}, nil))
	var lists map[string][]publicUser
	e.expect(200, "", e.call("GET", "/v1/friends", bob.SessionToken, nil, &lists))
	if len(lists["incoming"]) != 1 || lists["incoming"][0].ID != alice.User.ID {
		t.Fatalf("bob's lists = %+v", lists)
	}
	// Asking back accepts.
	e.expect(200, "", e.call("POST", "/v1/friends", bob.SessionToken, map[string]string{"pseudo": "alice"}, &rel))
	if rel.Status != relFriends {
		t.Fatalf("mutual request status = %q", rel.Status)
	}
	e.expect(409, "already_friends", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "bob"}, nil))

	// Carol's request can be declined.
	e.expect(200, "", e.call("POST", "/v1/friends", carol.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	e.expect(404, "not_found", e.call("POST", "/v1/friends/"+carol.User.ID+"/accept", carol.SessionToken, nil, nil))
	e.expect(204, "", e.call("DELETE", "/v1/friends/"+carol.User.ID, alice.SessionToken, nil, nil))
	e.expect(200, "", e.call("GET", "/v1/friends", alice.SessionToken, nil, &lists))
	if len(lists["friends"]) != 1 || len(lists["incoming"]) != 0 || len(lists["outgoing"]) != 0 {
		t.Fatalf("alice's lists = %+v", lists)
	}
	e.expect(204, "", e.call("DELETE", "/v1/friends/"+bob.User.ID, alice.SessionToken, nil, nil))
	e.expect(404, "not_found", e.call("DELETE", "/v1/friends/"+bob.User.ID, alice.SessionToken, nil, nil))
}

func TestDeviceKeys(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	_, master, _ := ed25519.GenerateKey(nil)
	d1 := newTestDevice(lr)

	e.expect(400, "master_key_required", e.upload(d1, nil, false))
	bad := *d1
	bad.curve = b64.EncodeToString(make([]byte, 32)) // signature no longer matches
	body := map[string]string{"curve25519": bad.curve, "ed25519": d1.edPub, "signature": sign(d1.ed, e2ekeys.DeviceKeys(d1.userID, d1.id(), d1.curve, d1.edPub))}
	e.expect(400, "invalid_signature", e.call("POST", "/v1/keys/device", d1.token(), body, nil))

	var dev deviceKeysJSON
	e.expect(200, "", e.upload(d1, master, true))
	e.expect(200, "", e.call("GET", "/v1/keys/device", d1.token(), nil, &dev))
	if !dev.Verified || dev.VerificationCode != e2ekeys.VerificationCode(d1.edPub) {
		t.Fatalf("first device = %+v", dev)
	}

	// A second device starts unverified; another master key is refused.
	d2 := newTestDevice(e.login2("a@example.com", pw, "phone"))
	_, other, _ := ed25519.GenerateKey(nil)
	e.expect(409, "master_key_exists", e.upload(d2, other, true))
	e.expect(200, "", e.upload(d2, nil, false))
	e.expect(200, "", e.call("GET", "/v1/keys/device", d2.token(), nil, &dev))
	if dev.Verified {
		t.Fatal("second device verified without approval")
	}
	// Approval needs a certificate from the master key.
	e.expect(400, "invalid_signature", e.call("POST", "/v1/keys/certify", d1.token(),
		map[string]string{"device_id": d2.id(), "master_signature": sign(other, e2ekeys.DeviceCert(d2.userID, d2.id(), d2.edPub))}, nil))
	e.expect(204, "", e.call("POST", "/v1/keys/certify", d1.token(),
		map[string]string{"device_id": d2.id(), "master_signature": sign(master, e2ekeys.DeviceCert(d2.userID, d2.id(), d2.edPub))}, nil))
	e.expect(200, "", e.call("GET", "/v1/keys/device", d2.token(), nil, &dev))
	if !dev.Verified {
		t.Fatal("certified device not verified")
	}

	// One-time keys: signed, consumed once, then the fallback key.
	otk := func(d *testDevice, id string) oneTimeKey {
		k := b64.EncodeToString([]byte(strings.Repeat(id, 32))[:32])
		return oneTimeKey{ID: id, Key: k, Signature: sign(d.ed, e2ekeys.OneTimeKey(d.id(), id, k))}
	}
	forged := otk(d1, "x")
	forged.Signature = sign(d2.ed, e2ekeys.OneTimeKey(d1.id(), "x", forged.Key))
	e.expect(400, "invalid_signature", e.call("POST", "/v1/keys/one-time", d1.token(), map[string]any{"keys": []oneTimeKey{forged}}, nil))
	fb := otk(d1, "f")
	e.expect(200, "", e.call("POST", "/v1/keys/one-time", d1.token(), map[string]any{"keys": []oneTimeKey{otk(d1, "a")}, "fallback": fb}, nil))

	bob, _ := e.registerVerified("b@example.com", "bob", pw)
	claim := map[string]any{"device_ids": []string{d1.id()}}
	e.expect(403, "not_friends", e.call("POST", "/v1/keys/claim", bob.SessionToken, claim, nil))
	e.expect(403, "not_friends", e.call("GET", "/v1/users/"+lr.User.ID+"/keys", bob.SessionToken, nil, nil))
	e.befriend(bob, lr, "alice")

	var claimed map[string]oneTimeKey
	e.expect(200, "", e.call("POST", "/v1/keys/claim", bob.SessionToken, claim, &claimed))
	if k := claimed[d1.id()]; k.ID != "a" || k.Fallback {
		t.Fatalf("first claim = %+v", k)
	}
	e.expect(200, "", e.call("POST", "/v1/keys/claim", bob.SessionToken, claim, &claimed))
	if k := claimed[d1.id()]; k.ID != "f" || !k.Fallback {
		t.Fatalf("second claim = %+v", k)
	}

	var keys struct {
		MasterKey *string          `json:"master_key"`
		Devices   []deviceKeysJSON `json:"devices"`
	}
	e.expect(200, "", e.call("GET", "/v1/users/"+lr.User.ID+"/keys", bob.SessionToken, nil, &keys))
	if keys.MasterKey == nil || len(keys.Devices) != 2 || keys.Devices[1].DeviceName != "phone" {
		t.Fatalf("keys = %+v", keys)
	}
	// Clients can check the certificates themselves.
	for _, d := range keys.Devices {
		if e2ekeys.Verify(*keys.MasterKey, e2ekeys.DeviceCert(lr.User.ID, d.DeviceID, d.Ed25519), *d.MasterSignature) != nil {
			t.Fatalf("certificate of %s does not verify", d.DeviceID)
		}
	}

	// Revoking a session removes its device keys.
	e.expect(204, "", e.call("DELETE", "/v1/me/sessions/"+d2.id(), d1.token(), nil, nil))
	e.expect(200, "", e.call("GET", "/v1/users/"+lr.User.ID+"/keys", bob.SessionToken, nil, &keys))
	if len(keys.Devices) != 1 {
		t.Fatalf("revoked device still listed: %+v", keys.Devices)
	}
}

type inboxList []inboxItem

func (e *testEnv) inbox(d *testDevice) inboxList {
	e.t.Helper()
	var list inboxList
	e.expect(200, "", e.call("GET", "/v1/inbox", d.token(), nil, &list))
	return list
}

func (e *testEnv) ack(d *testDevice, items inboxList) {
	e.t.Helper()
	ids := []int64{}
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	e.expect(204, "", e.call("POST", "/v1/inbox/ack", d.token(), map[string]any{"ids": ids}, nil))
}

func TestDirectMessages(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	aliceLR, _ := e.registerVerified("a@example.com", "alice", pw)
	bobLR, _ := e.registerVerified("b@example.com", "bob", pw)
	carolLR, _ := e.registerVerified("c@example.com", "carol", pw)
	_, am, _ := ed25519.GenerateKey(nil)
	_, bm, _ := ed25519.GenerateKey(nil)
	alice1, alice2 := newTestDevice(aliceLR), newTestDevice(e.login2("a@example.com", pw, "phone"))
	bob1, bob2 := newTestDevice(bobLR), newTestDevice(e.login2("b@example.com", pw, "phone"))
	e.expect(200, "", e.upload(alice1, am, true))
	e.expect(200, "", e.upload(alice2, nil, false))
	e.expect(200, "", e.upload(bob1, bm, true))
	e.expect(200, "", e.upload(bob2, nil, false))

	e.expect(403, "not_friends", e.call("POST", "/v1/dms", aliceLR.SessionToken, map[string]string{"user_id": bobLR.User.ID}, nil))
	e.befriend(aliceLR, bobLR, "bob")
	var dm convJSON
	e.expect(200, "", e.call("POST", "/v1/dms", alice1.token(), map[string]string{"user_id": bobLR.User.ID}, &dm))
	var again convJSON
	e.expect(200, "", e.call("POST", "/v1/dms", bob1.token(), map[string]string{"user_id": aliceLR.User.ID}, &again))
	if again.ID != dm.ID || dm.User.ID != bobLR.User.ID {
		t.Fatalf("dm = %+v, again = %+v", dm, again)
	}
	e.expect(404, "not_found", e.call("POST", "/v1/dms/"+dm.ID+"/messages", carolLR.SessionToken, map[string]string{"payload": "x"}, nil))

	// Live delivery on bob's first device.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.http.URL, "http")+"/v1/gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	wsjson.Write(ctx, ws, map[string]string{"op": "auth", "token": bob1.token()})
	var ready struct{ T string }
	if err := wsjson.Read(ctx, ws, &ready); err != nil || ready.T != "READY" {
		t.Fatalf("READY: %v %+v", err, ready)
	}

	var sent struct {
		EventID          int64 `json:"event_id"`
		RecipientDevices int   `json:"recipient_devices"`
	}
	e.expect(201, "", e.call("POST", "/v1/dms/"+dm.ID+"/messages", alice1.token(), map[string]string{"payload": "megolm-ciphertext"}, &sent))
	if sent.RecipientDevices != 2 {
		t.Fatalf("recipient devices = %d", sent.RecipientDevices)
	}
	var pushed struct {
		T string
		D []inboxItem
	}
	if err := wsjson.Read(ctx, ws, &pushed); err != nil || pushed.T != "INBOX" || pushed.D[0].Payload != "megolm-ciphertext" || *pushed.D[0].DMID != dm.ID {
		t.Fatalf("push = %v %+v", err, pushed)
	}

	// Every other device (alice's phone included) gets a copy; not the sending device.
	if n := len(e.inbox(alice1)); n != 0 {
		t.Fatalf("sender device received %d items", n)
	}
	a2 := e.inbox(alice2)
	if len(a2) != 1 || a2[0].Kind != "dm" {
		t.Fatalf("alice's phone inbox = %+v", a2)
	}

	// Delivery receipt once both of bob's devices acknowledged.
	e.ack(bob1, e.inbox(bob1))
	if n := len(e.inbox(alice1)); n != 0 {
		t.Fatal("receipt sent before every recipient device acknowledged")
	}
	b2 := e.inbox(bob2)
	e.ack(bob2, b2)
	receipts := e.inbox(alice1)
	if len(receipts) != 1 || receipts[0].Kind != "receipt" || *receipts[0].EventID != sent.EventID {
		t.Fatalf("receipts = %+v", receipts)
	}
	if n := len(e.inbox(bob2)); n != 0 {
		t.Fatalf("acknowledged items still in the mailbox: %d", n)
	}
	// Acknowledging someone else's items does nothing.
	e.expect(204, "", e.call("POST", "/v1/inbox/ack", bob1.token(), map[string]any{"ids": []int64{a2[0].ID}}, nil))
	stillThere := false
	for _, it := range e.inbox(alice2) {
		stillThere = stillThere || it.ID == a2[0].ID
	}
	if !stillThere {
		t.Fatal("another device's item was deleted")
	}

	// To-device messages: to friends' and own devices only.
	msg := map[string]any{"messages": []map[string]string{{"device_id": bob2.id(), "payload": "olm-ciphertext"}}}
	e.expect(204, "", e.call("POST", "/v1/to-device", alice1.token(), msg, nil))
	if items := e.inbox(bob2); len(items) != 1 || items[0].Kind != "to_device" || items[0].SenderDevice != alice1.id() {
		t.Fatalf("to-device = %+v", items)
	}
	e.expect(403, "not_friends", e.call("POST", "/v1/to-device", carolLR.SessionToken, msg, nil))

	// Unfriended: no more messages.
	e.expect(204, "", e.call("DELETE", "/v1/friends/"+bobLR.User.ID, alice1.token(), nil, nil))
	e.expect(403, "not_friends", e.call("POST", "/v1/dms/"+dm.ID+"/messages", alice1.token(), map[string]string{"payload": "x"}, nil))

	// A revoked device loses its mailbox.
	e.expect(204, "", e.call("POST", "/v1/auth/logout", alice2.token(), nil, nil))
	var n int
	e.srv.db.QueryRow(`SELECT COUNT(*) FROM inbox WHERE session_id = ?`, alice2.id()).Scan(&n)
	if n != 0 {
		t.Fatalf("revoked device kept %d mailbox items", n)
	}
}

func TestBackup(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	alice, _ := e.registerVerified("a@example.com", "alice", pw)
	phone := e.login2("a@example.com", pw, "phone")
	bob, _ := e.registerVerified("b@example.com", "bob", pw)

	e.expect(404, "no_backup", e.call("GET", "/v1/backup", alice.SessionToken, nil, nil))
	var v struct{ Version int64 }
	e.expect(200, "", e.call("PUT", "/v1/backup", alice.SessionToken, map[string]any{"version": 0, "data": "chiffré-1"}, &v))
	if v.Version != 1 {
		t.Fatalf("version = %d", v.Version)
	}
	// Two devices racing: the second write must be based on the latest version.
	e.expect(200, "", e.call("PUT", "/v1/backup", phone.SessionToken, map[string]any{"version": 1, "data": "chiffré-2"}, &v))
	e.expect(409, "version_conflict", e.call("PUT", "/v1/backup", alice.SessionToken, map[string]any{"version": 1, "data": "chiffré-périmé"}, nil))
	e.expect(409, "version_conflict", e.call("PUT", "/v1/backup", alice.SessionToken, map[string]any{"version": 0, "data": "écrase"}, nil))
	var got struct {
		Version int64
		Data    string
	}
	e.expect(200, "", e.call("GET", "/v1/backup", alice.SessionToken, nil, &got))
	if got.Version != 2 || got.Data != "chiffré-2" {
		t.Fatalf("backup = %+v", got)
	}
	// Backups are private to their account.
	e.expect(404, "no_backup", e.call("GET", "/v1/backup", bob.SessionToken, nil, nil))
	e.expect(204, "", e.call("DELETE", "/v1/backup", alice.SessionToken, nil, nil))
	e.expect(404, "no_backup", e.call("GET", "/v1/backup", phone.SessionToken, nil, nil))
}

// An account that lost every device holding its master key (and its
// recovery phrase) can start over: password (and 2FA) required, the old key,
// certifications and backup go, and a device creates a new master key.
func TestResetMasterKey(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	_, master, _ := ed25519.GenerateKey(nil)
	d1 := newTestDevice(lr)
	e.expect(200, "", e.upload(d1, master, true))
	e.expect(200, "", e.call("PUT", "/v1/backup", d1.token(), map[string]any{"version": 0, "data": "b3BhcXVl"}, nil))

	d2 := newTestDevice(e.login2("a@example.com", pw, "nouveau"))
	e.expect(200, "", e.upload(d2, nil, false))
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/keys/master/reset", d2.token(), map[string]string{"password": "mauvais"}, nil))
	e.expect(204, "", e.call("POST", "/v1/keys/master/reset", d2.token(), map[string]string{"password": pw}, nil))

	var dev deviceKeysJSON
	e.expect(200, "", e.call("GET", "/v1/keys/device", d1.token(), nil, &dev))
	if dev.Verified {
		t.Fatal("device still certified by the old master key")
	}
	e.expect(404, "no_backup", e.call("GET", "/v1/backup", d2.token(), nil, nil))
	// The device publishes a new master key and certifies itself.
	_, fresh, _ := ed25519.GenerateKey(nil)
	e.expect(200, "", e.upload(d2, fresh, true))
	e.expect(200, "", e.call("GET", "/v1/keys/device", d2.token(), nil, &dev))
	if !dev.Verified {
		t.Fatal("new master key not accepted")
	}
	if !strings.Contains(e.mail.last["a@example.com"], "réinitialisées") {
		t.Fatal("no alert email")
	}
}

// Who may send me a friend request: everyone, friends of friends (and
// members of my conversations), nobody. A request crossing mine still
// accepts it.
func TestFriendRequestPrivacy(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	alice, _ := e.registerVerified("a@example.com", "alice", pw)
	bob, _ := e.registerVerified("b@example.com", "bob", pw)
	carol, _ := e.registerVerified("c@example.com", "carol", pw)
	dave, _ := e.registerVerified("d@example.com", "dave", pw)
	var p map[string]any
	e.expect(200, "", e.call("GET", "/v1/me/privacy", alice.SessionToken, nil, &p))
	if p["friend_requests"] != "everyone" {
		t.Fatalf("default = %v", p)
	}
	e.expect(400, "bad_request", e.call("PATCH", "/v1/me/privacy", alice.SessionToken, map[string]string{"friend_requests": "some"}, nil))
	e.expect(200, "", e.call("PATCH", "/v1/me/privacy", alice.SessionToken, map[string]string{"friend_requests": "friends_of_friends"}, &p))
	if p["friend_requests"] != "friends_of_friends" || p["typing"] != true {
		t.Fatalf("privacy = %v", p)
	}
	e.befriend(alice, bob, "bob")
	e.expect(403, "friend_requests_closed", e.call("POST", "/v1/friends", carol.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	e.befriend(bob, carol, "carol") // now a friend of a friend
	e.expect(200, "", e.call("POST", "/v1/friends", carol.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	// A member of one of my conversations: bob's group with alice and dave,
	// still there after dave and bob are no longer friends.
	e.expect(403, "friend_requests_closed", e.call("POST", "/v1/friends", dave.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	e.befriend(bob, dave, "dave")
	e.expect(201, "", e.call("POST", "/v1/dms", bob.SessionToken, map[string]any{"user_ids": []string{alice.User.ID, dave.User.ID}, "name": "g"}, nil))
	e.expect(204, "", e.call("DELETE", "/v1/friends/"+dave.User.ID, bob.SessionToken, nil, nil))
	e.expect(200, "", e.call("POST", "/v1/friends", dave.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	// Nobody: new requests refused, but a request alice sent is still answered.
	e.expect(204, "", e.call("DELETE", "/v1/friends/"+dave.User.ID, alice.SessionToken, nil, nil))
	e.expect(200, "", e.call("PATCH", "/v1/me/privacy", alice.SessionToken, map[string]string{"friend_requests": "nobody"}, nil))
	e.expect(403, "friend_requests_closed", e.call("POST", "/v1/friends", dave.SessionToken, map[string]string{"pseudo": "alice"}, nil))
	e.expect(200, "", e.call("POST", "/v1/friends", alice.SessionToken, map[string]string{"pseudo": "dave"}, nil))
	var rel struct{ Status string }
	e.expect(200, "", e.call("POST", "/v1/friends", dave.SessionToken, map[string]string{"pseudo": "alice"}, &rel))
	if rel.Status != relFriends {
		t.Fatalf("crossed request = %q", rel.Status)
	}
}
