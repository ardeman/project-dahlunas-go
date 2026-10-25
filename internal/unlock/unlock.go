// Package unlock makes and checks version 1 unlock codes, as specified in
// docs/code-format.md: a 26-byte payload signed with Ed25519, written as
// "DL1." and 144 Base32 characters.
package unlock

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Version is the only binary version this package makes and accepts.
	Version = 1
	// Prefix starts every canonical version 1 code.
	Prefix = "DL1."

	payloadSize  = 26
	envelopeSize = payloadSize + ed25519.SignatureSize
	bodySize     = 144
	maxInputSize = 512
	domain       = "Dahlunas/unlock/v1\x00"
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Errors returned by Parse and Verify. Callers may tell a code from a newer
// format or key apart (to suggest an app update) from a broken one.
var (
	ErrMalformed          = errors.New("unlock: malformed code")
	ErrUnsupportedVersion = errors.New("unlock: unsupported version")
	ErrWrongProject       = errors.New("unlock: code is for another project")
	ErrUnsupportedProduct = errors.New("unlock: unsupported product")
	ErrUnknownKey         = errors.New("unlock: unknown key")
	ErrBadSignature       = errors.New("unlock: bad signature")
)

// Code is a decoded unlock code. Its fields are untrusted until Verify
// succeeds.
type Code struct {
	Version   uint8
	KeyID     uint8
	Project   uint16
	Product   uint16
	ID        [16]byte
	IssuedDay uint32

	signature []byte
}

// IssuedDay returns whole UTC days since 1970-01-01 at t.
func IssuedDay(t time.Time) (uint32, error) {
	day := t.Unix() / 86400
	if t.Unix() < 0 || day > 0xFFFFFFFF {
		return 0, fmt.Errorf("unlock: date %s is outside the issued day range", t.UTC().Format(time.DateOnly))
	}
	return uint32(day), nil
}

// New makes a code for project and product, signed with key under keyID,
// with a random code ID and today's issued day.
func New(key ed25519.PrivateKey, keyID uint8, project, product uint16, now time.Time) (string, error) {
	day, err := IssuedDay(now)
	if err != nil {
		return "", err
	}
	c := Code{Version: Version, KeyID: keyID, Project: project, Product: product, IssuedDay: day}
	for c.ID == ([16]byte{}) {
		if _, err := rand.Read(c.ID[:]); err != nil {
			return "", err
		}
	}
	return Sign(key, c)
}

// Sign returns the canonical text of c signed with key.
func Sign(key ed25519.PrivateKey, c Code) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("unlock: private key must be 64 bytes")
	}
	if err := c.checkFields(); err != nil {
		return "", err
	}
	payload := c.payload()
	envelope := append(payload, ed25519.Sign(key, message(payload))...)
	return Prefix + encoding.EncodeToString(envelope), nil
}

// Parse normalizes and decodes input without checking its signature.
func Parse(input string) (Code, error) {
	if len(input) > maxInputSize {
		return Code{}, ErrMalformed
	}
	var b strings.Builder
	for i := 0; i < len(input); i++ {
		switch ch := input[i]; {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
		case ch >= 'a' && ch <= 'z':
			b.WriteByte(ch - 'a' + 'A')
		default:
			b.WriteByte(ch)
		}
	}
	text, ok := strings.CutPrefix(b.String(), Prefix)
	if !ok {
		if len(text) > 3 && strings.HasPrefix(text, "DL") && text[3] == '.' {
			return Code{}, ErrUnsupportedVersion
		}
		return Code{}, ErrMalformed
	}
	body := strings.ReplaceAll(text, "-", "")
	if len(body) != bodySize {
		return Code{}, ErrMalformed
	}
	for i := 0; i < len(body); i++ {
		if ch := body[i]; !(ch >= 'A' && ch <= 'Z' || ch >= '2' && ch <= '7') {
			return Code{}, ErrMalformed
		}
	}
	envelope, err := encoding.DecodeString(body)
	if err != nil || len(envelope) != envelopeSize || encoding.EncodeToString(envelope) != body {
		return Code{}, ErrMalformed
	}
	p := envelope[:payloadSize]
	c := Code{
		Version:   p[0],
		KeyID:     p[1],
		Project:   binary.BigEndian.Uint16(p[2:4]),
		Product:   binary.BigEndian.Uint16(p[4:6]),
		IssuedDay: binary.BigEndian.Uint32(p[22:26]),
		signature: envelope[payloadSize:],
	}
	copy(c.ID[:], p[6:22])
	if c.Version != Version {
		return Code{}, ErrUnsupportedVersion
	}
	if err := c.checkFields(); err != nil {
		return Code{}, err
	}
	return c, nil
}

// Verify parses input and checks it is a code for project, for one of
// products, signed by one of keys (32-byte public keys by key ID).
func Verify(input string, project uint16, products []uint16, keys map[uint8]ed25519.PublicKey) (Code, error) {
	c, err := Parse(input)
	if err != nil {
		return Code{}, err
	}
	if c.Project != project {
		return Code{}, ErrWrongProject
	}
	supported := false
	for _, p := range products {
		supported = supported || p == c.Product
	}
	if !supported {
		return Code{}, ErrUnsupportedProduct
	}
	key, ok := keys[c.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return Code{}, ErrUnknownKey
	}
	if !ed25519.Verify(key, message(c.payload()), c.signature) {
		return Code{}, ErrBadSignature
	}
	return c, nil
}

// Canonical returns the canonical text of a parsed code.
func (c Code) Canonical() string {
	return Prefix + encoding.EncodeToString(append(c.payload(), c.signature...))
}

// Grouped returns the canonical text with the body in groups of eight for
// typing by hand.
func Grouped(canonical string) string {
	body := strings.TrimPrefix(canonical, Prefix)
	groups := make([]string, 0, len(body)/8)
	for i := 0; i < len(body); i += 8 {
		groups = append(groups, body[i:min(i+8, len(body))])
	}
	return Prefix + strings.Join(groups, "-")
}

func (c Code) checkFields() error {
	if c.Version != Version || c.KeyID == 0 || c.Project == 0 || c.Product == 0 || c.ID == ([16]byte{}) {
		return ErrMalformed
	}
	return nil
}

func (c Code) payload() []byte {
	p := make([]byte, payloadSize, envelopeSize)
	p[0] = c.Version
	p[1] = c.KeyID
	binary.BigEndian.PutUint16(p[2:4], c.Project)
	binary.BigEndian.PutUint16(p[4:6], c.Product)
	copy(p[6:22], c.ID[:])
	binary.BigEndian.PutUint32(p[22:26], c.IssuedDay)
	return p
}

func message(payload []byte) []byte {
	return bytes.Join([][]byte{[]byte(domain), payload[:payloadSize]}, nil)
}
