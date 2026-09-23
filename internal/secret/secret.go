// Package secret provides random identifiers, bearer tokens and key files.
package secret

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a random 128-bit identifier as lowercase base32 (26 chars).
func NewID() string { return Code(16) }

// Code returns n random bytes as lowercase base32.
func Code(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return strings.ToLower(b32.EncodeToString(b))
}

// NewToken returns a random 256-bit bearer secret, base64url encoded.
func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SHA256Hex hashes s; used to store bearer secrets without keeping them.
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// LoadOrCreateKey reads an Ed25519 private key (base64 seed) from path,
// generating it with mode 0600 if the file does not exist.
func LoadOrCreateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s: invalid key file", path)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
	if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
		return nil, err
	}
	return priv, nil
}
