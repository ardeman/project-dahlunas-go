package unlock

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// KeyEnv names the environment variable holding a project's signing key:
// DAHLUNAS_KEY_<project>_<key ID>, its value the 32-byte Ed25519 seed in hex.
func KeyEnv(project uint16, keyID uint8) string {
	return fmt.Sprintf("DAHLUNAS_KEY_%d_%d", project, keyID)
}

// KeyFromEnv reads a project's signing key from its environment variable.
// Errors never include the value.
func KeyFromEnv(project uint16, keyID uint8) (ed25519.PrivateKey, error) {
	name := KeyEnv(project, keyID)
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return nil, fmt.Errorf("unlock: %s is not set", name)
	}
	return ParseSeed(name, value)
}

// ParseSeed decodes a 32-byte seed written in hex. name says where it came
// from in errors, which never include the value.
func ParseSeed(name, value string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("unlock: %s must be a %d-byte seed in hex", name, ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// NewSeed makes a random signing key, returning its seed in hex (secret) and
// its public key in hex (for apps to bundle).
func NewSeed() (seed, public string, err error) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(private.Seed()), hex.EncodeToString(pub), nil
}

// PublicHex returns key's public key in hex.
func PublicHex(key ed25519.PrivateKey) string {
	return hex.EncodeToString(key.Public().(ed25519.PublicKey))
}
