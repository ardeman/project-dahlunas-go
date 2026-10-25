package unlock

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestKeyEnv(t *testing.T) {
	if got := KeyEnv(1, 2); got != "DAHLUNAS_KEY_1_2" {
		t.Fatalf("KeyEnv = %s", got)
	}
}

func TestKeyFromEnvSignsForItsPublicKey(t *testing.T) {
	seed, public, err := NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(KeyEnv(1, 1), seed+"\n")
	key, err := KeyFromEnv(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if PublicHex(key) != public {
		t.Fatalf("public key = %s, want %s", PublicHex(key), public)
	}
	pub, _ := hex.DecodeString(public)
	code, err := New(key, 1, 1, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(code, 1, []uint16{1}, map[uint8]ed25519.PublicKey{1: pub}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyFromEnvErrorsHideTheValue(t *testing.T) {
	if _, err := KeyFromEnv(9, 9); err == nil || !strings.Contains(err.Error(), "DAHLUNAS_KEY_9_9 is not set") {
		t.Fatalf("err = %v", err)
	}
	for _, bad := range []string{"secret-not-hex", strings.Repeat("ab", 31), strings.Repeat("ab", 64)} {
		t.Setenv(KeyEnv(9, 9), bad)
		_, err := KeyFromEnv(9, 9)
		if err == nil || strings.Contains(err.Error(), bad) {
			t.Fatalf("err = %v", err)
		}
	}
}
