package recovery

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestPhraseRoundTrip(t *testing.T) {
	phrase, secret, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(phrase)); n != 12 {
		t.Fatalf("%d words", n)
	}
	got, err := Parse(phrase)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Parse = %x, %v", got, err)
	}
	// Case and accents do not matter.
	loose := strings.ToUpper(strings.NewReplacer("é", "e", "è", "e", "ê", "e", "à", "a", "ç", "c", "ô", "o", "î", "i", "û", "u").Replace(phrase))
	if got, err := Parse("  " + loose + " "); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("loose Parse = %x, %v", got, err)
	}
}

func TestBIP39Vector(t *testing.T) {
	// Encoding must follow BIP-39: all-zero entropy is word 0 eleven times,
	// then the word carrying the checksum (SHA-256(0^16) starts with 0x37 → 0011).
	zero := make([]byte, 16)
	fields := strings.Fields(encode(zero))
	for _, w := range fields[:11] {
		if w != words[0] {
			t.Fatalf("zero entropy gives %v", fields)
		}
	}
	if fields[11] != words[3] {
		t.Fatalf("checksum word = %s, want %s", fields[11], words[3])
	}
	// Known entropy from the BIP-39 test vectors (English list there; indices are list-independent).
	ent, _ := hex.DecodeString("7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f")
	want := []int{1019, 2015, 1790, 2039, 1983, 1533, 2031, 1919, 1019, 2015, 1790, 2040} // "legal winner thank year wave sausage worth useful legal winner thank yellow"
	for i, w := range strings.Fields(encode(ent)) {
		if index[fold(w)] != want[i] {
			t.Fatalf("word %d = index %d, want %d", i, index[fold(w)], want[i])
		}
	}
}

func TestParseErrors(t *testing.T) {
	phrase, _, _ := Generate()
	f := strings.Fields(phrase)
	if _, err := Parse(strings.Join(f[:11], " ")); !errors.Is(err, ErrInvalidPhrase) {
		t.Error("11 words accepted")
	}
	f2 := append([]string{}, f...)
	f2[3] = "bonjourx"
	if _, err := Parse(strings.Join(f2, " ")); !errors.Is(err, ErrInvalidPhrase) {
		t.Error("unknown word accepted")
	}
	// Swapping two different words almost always breaks the checksum.
	broken := 0
	for i := range 11 {
		f3 := append([]string{}, f...)
		if f3[i] == f3[i+1] {
			continue
		}
		f3[i], f3[i+1] = f3[i+1], f3[i]
		if _, err := Parse(strings.Join(f3, " ")); err != nil {
			broken++
		}
	}
	if broken < 5 {
		t.Errorf("checksum caught only %d of 11 swaps", broken)
	}
}

func TestSealOpen(t *testing.T) {
	_, secret, _ := Generate()
	key := Key(secret, "user1")
	ct, err := Seal(key, "user1", []byte("sauvegarde"))
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := Open(key, "user1", ct); err != nil || string(pt) != "sauvegarde" {
		t.Fatalf("Open = %q, %v", pt, err)
	}
	if _, err := Open(key, "user2", ct); !errors.Is(err, ErrWrongKey) {
		t.Error("backup opened for another user")
	}
	if _, err := Open(Key(secret, "user2"), "user2", ct); !errors.Is(err, ErrWrongKey) {
		t.Error("key of another user opened the backup")
	}
	_, other, _ := Generate()
	if _, err := Open(Key(other, "user1"), "user1", ct); !errors.Is(err, ErrWrongKey) {
		t.Error("another phrase opened the backup")
	}
	ct[len(ct)-1] ^= 1
	if _, err := Open(key, "user1", ct); !errors.Is(err, ErrWrongKey) {
		t.Error("tampered backup accepted")
	}
}
