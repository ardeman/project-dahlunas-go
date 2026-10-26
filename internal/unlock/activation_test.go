package unlock

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The activation fixture from docs/activation.md; its seed is bytes 0 to 31.
const (
	activationPublicKey = "03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8"
	activationInstallID = "000102030405060708090a0b0c0d0e0f"
	activationPayload   = "010100020102030405060708090a0b0c0d0e0f10d65df89f702eec58ca3c7bf2001c9ffc7cd80553ae01d42a799ba26756142d9600005110"
	activationFixture   = "DA1.AEAQAAQBAIBQIBIGA4EASCQLBQGQ4DYQ2ZO7RH3QF3WFRSR4PPZAAHE77R6NQBKTVYA5IKTZTORGOVQUFWLAAACRCBLKDC75ZPO3ZHAXJRM4UJXCRSML45BMOTSEOX5KTE27G7AOG4UA5A2S4TG6WABQDI4YAZ5VOIDBGAQPHGBN2RBWVRMQIF6NUCR3O6IK"
)

func TestActivationFixture(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	key := ed25519.NewKeyFromSeed(seed)
	if hex.EncodeToString(key.Public().(ed25519.PublicKey)) != activationPublicKey {
		t.Fatal("fixture key changed")
	}
	keys := map[uint8]ed25519.PublicKey{1: key.Public().(ed25519.PublicKey)}
	a, err := VerifyActivation(activationFixture, 2, activationInstallID, keys)
	if err != nil || hex.EncodeToString(a.payload()) != activationPayload || a.Day != 20752 {
		t.Fatal(a, err)
	}
	signed, err := SignActivation(key, a)
	if err != nil || signed != activationFixture {
		t.Fatal("signing is not deterministic", signed, err)
	}

	other := strings.Repeat("f", 32)
	if _, err := VerifyActivation(activationFixture, 2, other, keys); !errors.Is(err, ErrNotThisDevice) {
		t.Fatal("verified on another install", err)
	}
	if _, err := VerifyActivation(activationFixture, 1, activationInstallID, keys); !errors.Is(err, ErrWrongProject) {
		t.Fatal(err)
	}
	if _, err := VerifyActivation(activationFixture, 2, activationInstallID, map[uint8]ed25519.PublicKey{2: keys[1]}); !errors.Is(err, ErrUnknownKey) {
		t.Fatal(err)
	}
	// A changed payload character breaks the signature, not the format.
	tampered := activationFixture[:4+88] + "B" + activationFixture[4+89:]
	if tampered == activationFixture {
		tampered = activationFixture[:4+88] + "C" + activationFixture[4+89:]
	}
	for _, bad := range []string{tampered, activationFixture[:len(activationFixture)-1], strings.Replace(activationFixture, "DA1.", "DL1.", 1), activationFixture + "A"} {
		if _, err := VerifyActivation(bad, 2, activationInstallID, keys); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
