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

func TestProofV2(t *testing.T) {
	_, device, _ := ed25519.GenerateKey(nil)
	c := &Claims{DeviceKey: EncodeKey(device.Public().(ed25519.PublicKey))}
	proof := SignProofV2(device, "srv", "n1", "Chez-Moi.example:8090", TLSAuthority)
	if err := VerifyProofV2(c, "srv", "n1", "chez-moi.example", TLSAuthority, proof); err != nil {
		t.Fatalf("valid proof refused: %v", err)
	}
	for _, bad := range []struct{ aud, nonce, host, mode string }{
		{"other", "n1", "chez-moi.example", TLSAuthority},
		{"srv", "n2", "chez-moi.example", TLSAuthority},
		{"srv", "n1", "evil.example", TLSAuthority},
		{"srv", "n1", "chez-moi.example", TLSBinding},
		{"srv", "n1", "chez-moi.example", "none"},
	} {
		if VerifyProofV2(c, bad.aud, bad.nonce, bad.host, bad.mode, proof) == nil {
			t.Errorf("proof accepted for %+v", bad)
		}
	}
	if VerifyProof(c, "srv", "n1", proof) == nil {
		t.Error("v2 proof accepted as v1")
	}
	for in, want := range map[string]string{"[2001:DB8::1]:443": "2001:db8::1", "Example.ORG.": "example.org", "10.0.0.1:8090": "10.0.0.1"} {
		if got := NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}
