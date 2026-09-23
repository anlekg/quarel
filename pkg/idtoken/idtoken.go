// Package idtoken defines Quarel identity tokens.
//
// An Identity service issues short-lived tokens (JWT, EdDSA/Ed25519) that
// carry a user's stable ID, handle and the public key of the device holding
// the token. Community servers verify them offline using the Identity
// service's published key set, so the Identity service never learns which
// servers a user connects to.
//
// Tokens carry no audience. To stop a server from replaying a token against
// another server, the holder must also present a proof: a signature, made
// with the device key, over the target server's audience and a nonce chosen
// by that server.
package idtoken

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// WellKnownPath is where an Identity service publishes its KeySet.
const WellKnownPath = "/.well-known/quarel-identity"

const (
	proofContext = "quarel-auth-v1"
	clockLeeway  = 30 * time.Second
)

var b64 = base64.RawURLEncoding

// Claims are the contents of an identity token.
type Claims struct {
	// Handle is the human-readable identifier, e.g. "alice@identity.quarel.app".
	// It may change; bans and membership must key on (Issuer, Subject).
	Handle string `json:"handle"`
	// DeviceKey is the base64url Ed25519 public key of the device the token
	// was issued to. Proofs must be signed with the matching private key.
	DeviceKey string `json:"dkey"`
	jwt.RegisteredClaims
}

// PublicKey is one published signing key of an Identity service.
type PublicKey struct {
	KID string `json:"kid"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
}

// KeySet is the document served at WellKnownPath.
type KeySet struct {
	Issuer string      `json:"issuer"`
	Keys   []PublicKey `json:"keys"`
}

// KeyID derives a stable key identifier from a public key.
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// EncodeKey encodes an Ed25519 public key as base64url.
func EncodeKey(pub ed25519.PublicKey) string { return b64.EncodeToString(pub) }

// DecodeKey decodes a base64url Ed25519 public key.
func DecodeKey(s string) (ed25519.PublicKey, error) {
	raw, err := b64.DecodeString(s)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("idtoken: invalid Ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

func (ks KeySet) lookup(kid string) (ed25519.PublicKey, error) {
	for _, k := range ks.Keys {
		if k.KID != kid {
			continue
		}
		if k.Alg != "EdDSA" || k.Crv != "Ed25519" {
			return nil, fmt.Errorf("idtoken: unsupported key %s/%s", k.Alg, k.Crv)
		}
		return DecodeKey(k.X)
	}
	return nil, fmt.Errorf("idtoken: unknown key id %q", kid)
}

// Signer issues tokens for one Identity service.
type Signer struct {
	issuer string
	key    ed25519.PrivateKey
	kid    string
}

// NewSigner returns a Signer for issuer using key.
func NewSigner(issuer string, key ed25519.PrivateKey) *Signer {
	return &Signer{issuer: issuer, key: key, kid: KeyID(key.Public().(ed25519.PublicKey))}
}

// Issuer returns the issuer name.
func (s *Signer) Issuer() string { return s.issuer }

// KeySet returns the public key set to publish at WellKnownPath.
func (s *Signer) KeySet() KeySet {
	pub := s.key.Public().(ed25519.PublicKey)
	return KeySet{
		Issuer: s.issuer,
		Keys:   []PublicKey{{KID: s.kid, Alg: "EdDSA", Crv: "Ed25519", X: EncodeKey(pub)}},
	}
}

// Issue creates a token for userID bound to deviceKey.
func (s *Signer) Issue(userID, handle string, deviceKey ed25519.PublicKey, ttl time.Duration, now time.Time) (string, time.Time, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, err
	}
	exp := now.Add(ttl).Truncate(time.Second)
	claims := Claims{
		Handle:    handle,
		DeviceKey: EncodeKey(deviceKey),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        b64.EncodeToString(jti),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	t.Header["kid"] = s.kid
	signed, err := t.SignedString(s.key)
	return signed, exp, err
}

// Verify checks a token's signature, issuer and expiry against ks.
func Verify(token string, ks KeySet, now time.Time) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			return ks.lookup(kid)
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(ks.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockLeeway),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return nil, fmt.Errorf("idtoken: %w", err)
	}
	if claims.Subject == "" {
		return nil, errors.New("idtoken: missing subject")
	}
	if _, err := DecodeKey(claims.DeviceKey); err != nil {
		return nil, err
	}
	return &claims, nil
}

// PeekIssuer returns the token's claimed issuer WITHOUT verifying it, so a
// server can pick which KeySet to verify against. Never trust it otherwise.
func PeekIssuer(token string) (string, error) {
	var claims Claims
	if _, _, err := jwt.NewParser().ParseUnverified(token, &claims); err != nil {
		return "", fmt.Errorf("idtoken: %w", err)
	}
	if claims.Issuer == "" {
		return "", errors.New("idtoken: missing issuer")
	}
	return claims.Issuer, nil
}

func proofMessage(audience, nonce string) []byte {
	return []byte(proofContext + "\x00" + audience + "\x00" + nonce)
}

// SignProof signs a server challenge with the device private key.
// audience identifies the server being joined; nonce is its fresh challenge.
func SignProof(device ed25519.PrivateKey, audience, nonce string) string {
	return b64.EncodeToString(ed25519.Sign(device, proofMessage(audience, nonce)))
}

// VerifyProof checks that proof was signed by the device bound to c.
// The caller must ensure nonce was issued by itself and is used only once.
func VerifyProof(c *Claims, audience, nonce, proof string) error {
	pub, err := DecodeKey(c.DeviceKey)
	if err != nil {
		return err
	}
	sig, err := b64.DecodeString(proof)
	if err != nil || !ed25519.Verify(pub, proofMessage(audience, nonce), sig) {
		return errors.New("idtoken: invalid proof")
	}
	return nil
}
