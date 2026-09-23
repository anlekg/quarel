// Package recovery implements Quarel recovery phrases and encrypted backups.
//
// A recovery phrase is 12 words from the official French BIP-39 list,
// encoding 128 random bits plus a 4-bit checksum (BIP-39 encoding), so typos
// are detected. The phrase never leaves the user's hands: it derives (HKDF-
// SHA256) the key that encrypts the account backup (XChaCha20-Poly1305,
// bound to the user ID). The server only stores the ciphertext; 128 bits of
// entropy make guessing the phrase infeasible.
package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/text/unicode/norm"
)

// french.txt is the BIP-39 French word list (MIT License, github.com/bitcoin/bips).
//
//go:embed french.txt
var frenchList string

var (
	words []string       // display form (NFC)
	index map[string]int // normalized form → position
)

func init() {
	for i, w := range strings.Fields(frenchList) {
		words = append(words, norm.NFC.String(w))
		if i == 0 {
			index = map[string]int{}
		}
		index[fold(w)] = i
	}
	if len(words) != 2048 || len(index) != 2048 {
		panic("recovery: corrupted word list")
	}
}

// fold makes a typed word comparable: lower case, accents removed.
func fold(w string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(strings.TrimSpace(w))) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

const entropyBytes = 16 // 128 bits → 12 words

// Generate returns a new recovery phrase and the secret it encodes.
func Generate() (phrase string, secret []byte, err error) {
	secret = make([]byte, entropyBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, err
	}
	return encode(secret), secret, nil
}

// encode turns 16 bytes into 12 words: 128 bits + 4 checksum bits = 12 × 11 bits.
func encode(secret []byte) string {
	sum := sha256.Sum256(secret)
	bits := append(append([]byte{}, secret...), sum[0]) // checksum = top 4 bits of sum[0]
	out := make([]string, 12)
	for i := range out {
		v := 0
		for j := range 11 {
			pos := i*11 + j
			if bits[pos/8]&(0x80>>(pos%8)) != 0 {
				v |= 1 << (10 - j)
			}
		}
		out[i] = words[v]
	}
	return strings.Join(out, " ")
}

// ErrInvalidPhrase reports a phrase with unknown words or a wrong checksum.
var ErrInvalidPhrase = errors.New("recovery: invalid phrase")

// Parse checks a typed phrase (case and accents ignored) and returns its secret.
func Parse(phrase string) ([]byte, error) {
	fields := strings.Fields(phrase)
	if len(fields) != 12 {
		return nil, fmt.Errorf("%w: 12 words expected, got %d", ErrInvalidPhrase, len(fields))
	}
	bits := make([]byte, entropyBytes+1)
	for i, w := range fields {
		v, ok := index[fold(w)]
		if !ok {
			return nil, fmt.Errorf("%w: unknown word %q (word %d)", ErrInvalidPhrase, w, i+1)
		}
		for j := range 11 {
			if v&(1<<(10-j)) != 0 {
				pos := i*11 + j
				bits[pos/8] |= 0x80 >> (pos % 8)
			}
		}
	}
	secret := bits[:entropyBytes]
	sum := sha256.Sum256(secret)
	if bits[entropyBytes]&0xF0 != sum[0]&0xF0 {
		return nil, fmt.Errorf("%w: checksum mismatch (typo?)", ErrInvalidPhrase)
	}
	return secret, nil
}

// Key derives the backup encryption key of a user from the phrase's secret.
func Key(secret []byte, userID string) []byte {
	key := make([]byte, chacha20poly1305.KeySize)
	io.ReadFull(hkdf.New(sha256.New, secret, []byte("quarel-backup-salt-v1"), []byte("quarel-backup-key-v1\x00"+userID)), key)
	return key
}

// Seal encrypts a backup; the ciphertext is bound to userID.
func Seal(key []byte, userID string, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, []byte("quarel-backup-v1\x00"+userID)), nil
}

// ErrWrongKey means the backup was not encrypted with this phrase (or was altered).
var ErrWrongKey = errors.New("recovery: this phrase does not open the backup")

// Open decrypts a backup made by Seal.
func Open(key []byte, userID string, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, ErrWrongKey
	}
	pt, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte("quarel-backup-v1\x00"+userID))
	if err != nil {
		return nil, ErrWrongKey
	}
	return pt, nil
}
