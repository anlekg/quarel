package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (RFC 9106, second recommended option). Tests lower them.
var argonParams = struct {
	time, memory uint32
	threads      uint8
}{time: 3, memory: 64 * 1024, threads: 2}

// argonSlots caps concurrent password hashes so a burst of logins cannot exhaust memory.
var argonSlots = make(chan struct{}, 4)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := argonParams
	argonSlots <- struct{}{}
	key := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, 32)
	<-argonSlots
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash")
	}
	var version int
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	argonSlots <- struct{}{}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	<-argonSlots
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when a login names an unknown account, so
// response time does not reveal whether the account exists.
var dummyHash = sync.OnceValue(func() string {
	h, err := hashPassword("quarel-dummy-password")
	if err != nil {
		panic(err)
	}
	return h
})

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newID returns a random 128-bit identifier as lowercase base32 (26 chars).
func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ToLower(idEncoding.EncodeToString(b))
}

// newSecret returns a random 256-bit bearer secret, base64url encoded.
func newSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// loadOrCreateSigningKey reads the service's Ed25519 key from dir, generating it on first run.
func loadOrCreateSigningKey(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, "signing.key")
	data, err := os.ReadFile(path)
	if err == nil {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s: invalid signing key", path)
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
