package idtoken

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestSealedTokenForOneServer(t *testing.T) {
	_, signKey, _ := ed25519.GenerateKey(rand.Reader)
	dev, _, _ := ed25519.GenerateKey(rand.Reader)
	s := NewSigner("id.example", signKey)
	tok, _, err := s.IssueFor("u1", "alice@id.example", dev, time.Hour, time.Now(), "serverA")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	sealed, err := Seal(tok, a.PublicKey().Bytes(), "serverA")
	if err != nil || !IsSealed(sealed) {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(sealed, b, "serverA"); err == nil {
		t.Fatal("another server's key must not open the token")
	}
	if _, err := Open(sealed, a, "serverB"); err == nil {
		t.Fatal("the token is bound to the server ID")
	}
	plain, err := Open(sealed, a, "serverA")
	if err != nil || plain != tok {
		t.Fatalf("open: %v", err)
	}
	c, err := Verify(plain, s.KeySet(), time.Now())
	if err != nil || !c.ForAudience("serverA") || c.ForAudience("serverB") {
		t.Fatalf("audience: %v %v", c, err)
	}
	open, _, _ := s.Issue("u1", "alice@id.example", dev, time.Hour, time.Now())
	if c, _ := Verify(open, s.KeySet(), time.Now()); !c.ForAudience("anything") {
		t.Fatal("tokens without audience are accepted anywhere")
	}
}

func TestApprovalSignature(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	enc, _ := ecdh.X25519().GenerateKey(rand.Reader)
	r := SignApproval(key, "id.example", ApprovalRequest{EncKey: b64.EncodeToString(enc.PublicKey().Bytes()), Name: "Club", URL: "https://club.example:8090", Contact: "a@b.c"})
	if _, err := VerifyApproval("id.example", r); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyApproval("other.example", r); err == nil {
		t.Fatal("bound to the issuer")
	}
	r.Name = "Autre"
	if _, err := VerifyApproval("id.example", r); err == nil {
		t.Fatal("fields are signed")
	}
}
