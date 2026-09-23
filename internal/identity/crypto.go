package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/anlekg/quarel/internal/secret"
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

var (
	newID     = secret.NewID
	newSecret = secret.NewToken
	sha256Hex = secret.SHA256Hex
)
