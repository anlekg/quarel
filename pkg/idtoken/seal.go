package idtoken

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hpke"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
)

// SealedPrefix starts a token encrypted for one server.
const SealedPrefix = "qe1."

func sealInfo(serverID string) []byte { return []byte("quarel-identity-token-v1\x00" + serverID) }

// Seal encrypts token for the server serverID, whose X25519 public key is
// encKey (HPKE, RFC 9180: DHKEM(X25519), HKDF-SHA256, ChaCha20-Poly1305).
func Seal(token string, encKey []byte, serverID string) (string, error) {
	pub, err := ecdh.X25519().NewPublicKey(encKey)
	if err != nil {
		return "", err
	}
	pk, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return "", err
	}
	ct, err := hpke.Seal(pk, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), sealInfo(serverID), []byte(token))
	if err != nil {
		return "", err
	}
	return SealedPrefix + b64.EncodeToString(ct), nil
}

// IsSealed reports whether token was encrypted with Seal.
func IsSealed(token string) bool { return strings.HasPrefix(token, SealedPrefix) }

// Open decrypts a sealed token with the server's X25519 private key.
func Open(sealed string, key *ecdh.PrivateKey, serverID string) (string, error) {
	ct, err := b64.DecodeString(strings.TrimPrefix(sealed, SealedPrefix))
	if err != nil {
		return "", errors.New("idtoken: malformed sealed token")
	}
	k, err := hpke.NewDHKEMPrivateKey(key)
	if err != nil {
		return "", err
	}
	pt, err := hpke.Open(k, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), sealInfo(serverID), ct)
	if err != nil {
		return "", errors.New("idtoken: sealed token not meant for this server")
	}
	return string(pt), nil
}

// ForAudience reports whether c may be accepted by server audience: tokens
// without an audience are accepted anywhere.
func (c *Claims) ForAudience(audience string) bool {
	return len(c.Audience) == 0 || slices.Contains([]string(c.Audience), audience)
}

// ApprovalRequest is what a community server sends to an Identity service
// that only accepts approved servers.
type ApprovalRequest struct {
	ServerKey string `json:"server_key"` // base64url Ed25519 identity key (its hash is the server ID)
	EncKey    string `json:"enc_key"`    // base64url X25519 key tokens are sealed to
	Name      string `json:"name"`
	URL       string `json:"url"`
	Contact   string `json:"contact"`
	Signature string `json:"signature"` // by the server identity key, over approvalMessage
}

func approvalMessage(issuer string, r ApprovalRequest) []byte {
	return []byte(strings.Join([]string{"quarel-server-approval-v1", issuer, r.ServerKey, r.EncKey, r.Name, r.URL, r.Contact}, "\x00"))
}

// SignApproval signs r for issuer with the server identity key.
func SignApproval(key ed25519.PrivateKey, issuer string, r ApprovalRequest) ApprovalRequest {
	r.ServerKey = EncodeKey(key.Public().(ed25519.PublicKey))
	r.Signature = b64.EncodeToString(ed25519.Sign(key, approvalMessage(issuer, r)))
	return r
}

// VerifyApproval checks the signature of r for issuer and returns the server's identity key.
func VerifyApproval(issuer string, r ApprovalRequest) (ed25519.PublicKey, error) {
	pub, err := DecodeKey(r.ServerKey)
	if err != nil {
		return nil, err
	}
	enc, err := b64.DecodeString(r.EncKey)
	if err != nil || len(enc) != 32 {
		return nil, errors.New("idtoken: invalid encryption key")
	}
	sig, err := b64.DecodeString(r.Signature)
	if err != nil || !ed25519.Verify(pub, approvalMessage(issuer, r), sig) {
		return nil, errors.New("idtoken: invalid approval signature")
	}
	return pub, nil
}

// AccountHash and DeviceHash name, in an Identity service's public list of
// disabled accounts and ended sessions, an account (its subject) or a device
// (its key, as in tokens) without revealing it: a community server hashes
// its own members' subjects and session device keys to find them.
func AccountHash(subject string) string { return listHash("quarel-disabled-account-v1", subject) }

// DeviceHash: see AccountHash.
func DeviceHash(deviceKey string) string { return listHash("quarel-ended-device-v1", deviceKey) }

func listHash(context, v string) string {
	h := sha256.Sum256([]byte(context + "\x00" + v))
	return b64.EncodeToString(h[:])
}
