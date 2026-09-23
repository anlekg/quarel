package idtoken

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestIssueVerifyProof(t *testing.T) {
	signer := NewSigner("id.example", newKey(t))
	device := newKey(t)
	now := time.Now()

	tok, exp, err := signer.Issue("u1", "alice@id.example", device.Public().(ed25519.PublicKey), time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(now) {
		t.Fatalf("expiry %v before now", exp)
	}

	c, err := Verify(tok, signer.KeySet(), now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "u1" || c.Handle != "alice@id.example" {
		t.Fatalf("unexpected claims %+v", c)
	}

	proof := SignProof(device, "server-a", "nonce-1")
	if err := VerifyProof(c, "server-a", "nonce-1", proof); err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
	if err := VerifyProof(c, "server-b", "nonce-1", proof); err == nil {
		t.Fatal("proof replayed on another audience was accepted")
	}
	if err := VerifyProof(c, "server-a", "nonce-2", proof); err == nil {
		t.Fatal("proof with another nonce was accepted")
	}
	if err := VerifyProof(c, "server-a", "nonce-1", SignProof(newKey(t), "server-a", "nonce-1")); err == nil {
		t.Fatal("proof from another device was accepted")
	}
}

func TestVerifyRejects(t *testing.T) {
	signer := NewSigner("id.example", newKey(t))
	device := newKey(t).Public().(ed25519.PublicKey)
	now := time.Now()
	tok, _, err := signer.Issue("u1", "alice@id.example", device, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Verify(tok, signer.KeySet(), now.Add(2*time.Hour)); err == nil {
		t.Error("expired token accepted")
	}

	other := NewSigner("id.example", newKey(t))
	if _, err := Verify(tok, other.KeySet(), now); err == nil {
		t.Error("token accepted with another service's keys")
	}

	wrongIssuer := signer.KeySet()
	wrongIssuer.Issuer = "evil.example"
	if _, err := Verify(tok, wrongIssuer, now); err == nil {
		t.Error("token accepted for another issuer")
	}

	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]
	if _, err := Verify(tampered, signer.KeySet(), now); err == nil {
		t.Error("tampered token accepted")
	}

	// "none" algorithm must never be accepted.
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + parts[1] + "."
	if _, err := Verify(none, signer.KeySet(), now); err == nil {
		t.Error("alg=none token accepted")
	}
}
