package community

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/anlekg/quarel/pkg/idtoken"
)

// Tokens from an Identity service that only accepts approved servers: sealed
// to this server and marked for it.
func TestSealedAndAudienceTokens(t *testing.T) {
	e := newEnv(t)
	_, owner := e.setupOwner()
	code := e.invite(owner, map[string]any{"max_uses": 0})
	bob := e.newUser("bob")
	pub := bob.device.Public().(ed25519.PublicKey)

	forUs, _, _ := e.signer.IssueFor(bob.sub, bob.handle, pub, time.Hour, e.clock, e.srv.id)
	sealed, err := idtoken.Seal(forUs, e.srv.enc.PublicKey().Bytes(), e.srv.id)
	if err != nil {
		t.Fatal(err)
	}
	e.mustLogin(bob, loginOpts{invite: code, token: sealed})

	// Marked for another server: refused, sealed or not.
	other, _, _ := e.signer.IssueFor(bob.sub, bob.handle, pub, time.Hour, e.clock, "autreserveur")
	e.expect(401, "wrong_audience", e.login(bob, loginOpts{token: other}, nil))
	elsewhere, _ := ecdh.X25519().GenerateKey(rand.Reader)
	sealedElsewhere, _ := idtoken.Seal(forUs, elsewhere.PublicKey().Bytes(), e.srv.id)
	e.expect(401, "invalid_token", e.login(bob, loginOpts{token: sealedElsewhere}, nil))

	// The encryption key is stable: derived from the server key.
	if !deriveEncKey(e.srv.key).Equal(e.srv.enc) {
		t.Fatal("encryption key must be derived from the server key")
	}
}
