// Package e2ekeys defines what Quarel devices sign when publishing
// end-to-end encryption keys, so servers and clients verify the same bytes.
//
// Keys and signatures are Ed25519 / Curve25519 in unpadded standard base64,
// as produced by Olm. Each user has a master signing key (created by their
// first device) that certifies their verified devices; contacts pin it on
// first use.
package e2ekeys

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
)

func msg(parts ...string) []byte { return []byte(strings.Join(parts, "\x00")) }

// DeviceKeys is what a device signs with its own Ed25519 key to publish its identity keys.
func DeviceKeys(userID, deviceID, curve25519, ed25519 string) []byte {
	return msg("quarel-device-keys-v1", userID, deviceID, curve25519, ed25519)
}

// DeviceCert is what the master key signs to certify (verify) a device.
func DeviceCert(userID, deviceID, ed25519 string) []byte {
	return msg("quarel-device-cert-v1", userID, deviceID, ed25519)
}

// OneTimeKey is what a device signs for each one-time (or fallback) key it publishes.
func OneTimeKey(deviceID, keyID, key string) []byte {
	return msg("quarel-one-time-key-v1", deviceID, keyID, key)
}

// Decode decodes base64 with or without padding.
func Decode(s string) ([]byte, error) {
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// Verify checks an Ed25519 signature made by publicKey over message.
func Verify(publicKey string, message []byte, signature string) error {
	pub, err := Decode(publicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("e2ekeys: invalid public key")
	}
	sig, err := Decode(signature)
	if err != nil || !ed25519.Verify(pub, message, sig) {
		return errors.New("e2ekeys: invalid signature")
	}
	return nil
}

// VerificationCode is the code shown on a new device and compared by the user
// on an already verified device before approving it: 80 bits of the device's
// Ed25519 key hash, as four groups of four base32 characters.
func VerificationCode(deviceEd25519 string) string {
	h := sha256.Sum256([]byte(deviceEd25519))
	s := base32.StdEncoding.EncodeToString(h[:10])
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// NormalizeCode makes a typed verification code comparable (case, dashes, spaces).
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}
