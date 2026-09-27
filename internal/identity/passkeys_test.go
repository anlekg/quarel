package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// softKey is a software authenticator (P-256, "none" attestation) for the
// passkey ceremonies, playing the browser's part on this service's page.
type softKey struct {
	t      *testing.T
	priv   *ecdsa.PrivateKey
	id     []byte
	rpID   string
	origin string
	count  uint32
}

func newSoftKey(t *testing.T, rpID, origin string) *softKey {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{t: t, priv: priv, id: id, rpID: rpID, origin: origin}
}

var rawB64 = base64.RawURLEncoding

func (k *softKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": k.origin, "crossOrigin": false})
	return b
}

func (k *softKey) authData(attested bool) []byte {
	h := sha256.Sum256([]byte(k.rpID))
	k.count++
	out := append([]byte{}, h[:]...)
	flags := byte(0x01 | 0x04) // user present, verified
	if attested {
		flags |= 0x40
	}
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, k.count)
	if attested {
		out = append(out, make([]byte, 16)...) // AAGUID
		out = binary.BigEndian.AppendUint16(out, uint16(len(k.id)))
		out = append(out, k.id...)
		x, y := k.priv.X.FillBytes(make([]byte, 32)), k.priv.Y.FillBytes(make([]byte, 32))
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		out = append(out, cose...)
	}
	return out
}

// create answers registration options.
func (k *softKey) create(options map[string]any) map[string]any {
	challenge := options["publicKey"].(map[string]any)["challenge"].(string)
	cd := k.clientData("webauthn.create", challenge)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": k.authData(true)})
	return map[string]any{
		"id": rawB64.EncodeToString(k.id), "rawId": rawB64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{"clientDataJSON": rawB64.EncodeToString(cd), "attestationObject": rawB64.EncodeToString(att)},
	}
}

// get answers login options.
func (k *softKey) get(options map[string]any) map[string]any {
	challenge := options["publicKey"].(map[string]any)["challenge"].(string)
	cd := k.clientData("webauthn.get", challenge)
	ad := k.authData(false)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{
		"id": rawB64.EncodeToString(k.id), "rawId": rawB64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{"clientDataJSON": rawB64.EncodeToString(cd), "authenticatorData": rawB64.EncodeToString(ad), "signature": rawB64.EncodeToString(sig)},
	}
}

// ceremony plays the page: options, then the key's answer.
func (e *testEnv) ceremony(ticket string, answer func(map[string]any) map[string]any) result {
	e.t.Helper()
	return e.ceremonyOut(ticket, answer, nil)
}

// ceremonyOut is ceremony, decoding the page's answer (the code, signing in).
func (e *testEnv) ceremonyOut(ticket string, answer func(map[string]any) map[string]any, out any) result {
	e.t.Helper()
	var opt struct {
		Kind    string
		Options map[string]any
	}
	if res := e.call("POST", "/v1/passkeys/options", "", map[string]string{"ticket": ticket}, &opt); res.status != 200 {
		return res
	}
	return e.call("POST", "/v1/passkeys/finish", "", map[string]any{"ticket": ticket, "credential": answer(opt.Options)}, out)
}

func TestPasskeys(t *testing.T) {
	e := newEnv(t)
	const pw = "correct horse battery"
	lr, _ := e.registerVerified("a@example.com", "alice", pw)
	tok := lr.SessionToken
	key := newSoftKey(t, "id.test", "https://id.test")

	// Only next to TOTP (its backup codes are the way back in).
	e.expect(400, "2fa_not_enabled", e.call("POST", "/v1/me/passkeys", tok, map[string]string{"password": pw, "name": "Clé USB"}, nil))
	var setup struct{ Secret string }
	e.expect(200, "", e.call("POST", "/v1/me/2fa/setup", tok, map[string]string{"password": pw}, &setup))
	code, _ := totpCode(setup.Secret, totpStep(e.clock))
	e.expect(200, "", e.call("POST", "/v1/me/2fa/enable", tok, map[string]string{"code": code}, nil))

	// Adding one: a ticket for the page, which runs the ceremony.
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/me/passkeys", tok, map[string]string{"password": "faux faux faux", "name": "x"}, nil))
	var add struct{ Ticket, URL string }
	e.expect(200, "", e.call("POST", "/v1/me/passkeys", tok, map[string]string{"password": pw, "name": "Clé USB"}, &add))
	if add.URL != "https://id.test/passkey/#"+add.Ticket {
		t.Fatalf("url = %q", add.URL)
	}
	e.expect(204, "", e.ceremony(add.Ticket, key.create))
	e.expect(404, "ticket_expired", e.call("POST", "/v1/passkeys/options", "", map[string]string{"ticket": add.Ticket}, nil)) // single use
	if !strings.Contains(e.mail.last["a@example.com"], "« Clé USB ») vient d'être ajoutée") {
		t.Fatalf("no alert for the new key: %q", e.mail.last["a@example.com"])
	}
	var keys []passkeyJSON
	e.expect(200, "", e.call("GET", "/v1/me/passkeys", tok, nil, &keys))
	if len(keys) != 1 || keys[0].Name != "Clé USB" || keys[0].LastUsedAt != nil {
		t.Fatalf("passkeys = %+v", keys)
	}

	// Signing in: password, then the key on the page; the app polls.
	_, pub := deviceKey(t)
	login := map[string]any{"login": "alice", "password": pw, "device_key": pub, "device_name": "Portable", "passkey": true}
	var started struct {
		Ticket string `json:"passkey_ticket"`
		URL    string
	}
	e.expect(401, "invalid_credentials", e.call("POST", "/v1/auth/login", "", map[string]any{"login": "alice", "password": "faux faux faux", "device_key": pub, "passkey": true}, nil))
	e.expect(202, "", e.call("POST", "/v1/auth/login", "", login, &started))
	poll := func(out any) result {
		return e.call("POST", "/v1/auth/login/passkey", "", map[string]string{"ticket": started.Ticket}, out)
	}
	withCode := func(code string, out any) result {
		return e.call("POST", "/v1/auth/login/passkey", "", map[string]string{"ticket": started.Ticket, "code": code}, out)
	}
	e.expect(202, "", poll(nil))
	// A key of someone else is refused, and the ticket is spent.
	other := newSoftKey(t, "id.test", "https://id.test")
	e.expect(401, "invalid_passkey", e.ceremony(started.Ticket, other.get))
	e.expect(404, "ticket_expired", poll(nil))
	// The right key: the page shows the sign-in (device, address, time), then a
	// code that the app must send (someone who sent the link would need it too).
	e.expect(202, "", e.call("POST", "/v1/auth/login", "", login, &started))
	var shown struct {
		Device      string
		IP          string
		RequestedAt string `json:"requested_at"`
	}
	e.expect(200, "", e.call("POST", "/v1/passkeys/options", "", map[string]string{"ticket": started.Ticket}, &shown))
	if shown.Device != "Portable" || shown.IP == "" || shown.RequestedAt == "" {
		t.Fatalf("the page does not show the sign-in: %+v", shown)
	}
	var page struct{ Code string }
	e.expect(200, "", e.ceremonyOut(started.Ticket, key.get, &page))
	if len(page.Code) != 6 {
		t.Fatalf("code = %q", page.Code)
	}
	var status struct{ Status string }
	e.expect(202, "", poll(&status))
	if status.Status != "code_required" {
		t.Fatalf("status = %q", status.Status)
	}
	wrong := "000000"
	if page.Code == wrong {
		wrong = "111111"
	}
	e.expect(401, "invalid_passkey_code", withCode(wrong, nil))
	var sess loginResp
	e.expect(200, "", withCode(page.Code[:3]+" "+page.Code[3:], &sess))
	if sess.SessionToken == "" || sess.User.Pseudo != "alice" {
		t.Fatalf("session = %+v", sess)
	}
	e.expect(200, "", e.call("GET", "/v1/me", sess.SessionToken, nil, nil))
	e.expect(404, "ticket_expired", withCode(page.Code, nil)) // once
	// Five wrong codes: the ticket is gone.
	e.expect(202, "", e.call("POST", "/v1/auth/login", "", login, &started))
	e.expect(200, "", e.ceremonyOut(started.Ticket, key.get, &page))
	for i := 0; i < 4; i++ {
		e.expect(401, "invalid_passkey_code", withCode(wrong, nil))
	}
	e.expect(404, "ticket_expired", withCode(wrong, nil))
	e.expect(404, "ticket_expired", withCode(page.Code, nil))
	e.expect(200, "", e.call("GET", "/v1/me/passkeys", tok, nil, &keys))
	if keys[0].LastUsedAt == nil {
		t.Fatal("last use not recorded")
	}

	// A key for another site (wrong origin) is refused.
	elsewhere := newSoftKey(t, "id.test", "https://evil.example")
	e.expect(200, "", e.call("POST", "/v1/me/passkeys", tok, map[string]string{"password": pw}, &add))
	if res := e.ceremony(add.Ticket, elsewhere.create); res.code != "invalid_passkey" {
		t.Fatalf("foreign origin: %+v", res)
	}
	// The page is served with a strict CSP.
	resp, err := http.Get(e.http.URL + "/passkey/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") || !strings.Contains(string(body), "page.js") {
		t.Fatalf("page: %d %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	// Deleting one needs the password (and tells the owner); turning TOTP off removes them all.
	e.expect(401, "invalid_credentials", e.call("DELETE", "/v1/me/passkeys/"+keys[0].ID, tok, map[string]string{"password": "faux faux faux"}, nil))
	e.expect(204, "", e.call("DELETE", "/v1/me/passkeys/"+keys[0].ID, tok, map[string]string{"password": pw}, nil))
	if !strings.Contains(e.mail.last["a@example.com"], "« Clé USB » a été retirée") {
		t.Fatalf("no alert for the removed key: %q", e.mail.last["a@example.com"])
	}
	e.expect(400, "no_passkey", e.call("POST", "/v1/auth/login", "", login, nil))
}
