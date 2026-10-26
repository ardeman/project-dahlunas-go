package unlock

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
)

// ActivationPrefix starts every version 1 activation, as specified in
// docs/activation.md: a 56-byte payload signed with Ed25519, written as
// "DA1." and 192 Base32 characters.
const ActivationPrefix = "DA1."

const (
	activationPayloadSize = 56
	activationBodySize    = 192
	activationDomain      = "Dahlunas/activation/v1\x00"
)

// ErrNotThisDevice means a genuine activation names another install.
var ErrNotThisDevice = errors.New("unlock: activation is for another device")

// Activation binds a code ID to one install of an app. Device is the SHA-256 of
// the app's install ID; Day is the issued day at activation.
type Activation struct {
	KeyID   uint8
	Project uint16
	CodeID  [16]byte
	Device  [32]byte
	Day     uint32
}

// DeviceHash is the hash Dahlunas stores and signs for an app's install ID.
func DeviceHash(installID string) [32]byte {
	return sha256.Sum256([]byte(installID))
}

// SignActivation signs a with key, whose key ID must be a.KeyID.
func SignActivation(key ed25519.PrivateKey, a Activation) (string, error) {
	if len(key) != ed25519.PrivateKeySize || a.KeyID == 0 || a.Project == 0 || a.CodeID == ([16]byte{}) {
		return "", ErrMalformed
	}
	payload := a.payload()
	signature := ed25519.Sign(key, append([]byte(activationDomain), payload...))
	return ActivationPrefix + encoding.EncodeToString(append(payload, signature...)), nil
}

// VerifyActivation checks input is an activation for project, signed by one of
// keys, for the install whose ID is installID.
func VerifyActivation(input string, project uint16, installID string, keys map[uint8]ed25519.PublicKey) (Activation, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(input), ActivationPrefix)
	if !ok || len(body) != activationBodySize {
		return Activation{}, ErrMalformed
	}
	raw, err := encoding.DecodeString(body)
	if err != nil || len(raw) != activationPayloadSize+ed25519.SignatureSize || raw[0] != Version {
		return Activation{}, ErrMalformed
	}
	var a Activation
	a.KeyID = raw[1]
	a.Project = binary.BigEndian.Uint16(raw[2:4])
	copy(a.CodeID[:], raw[4:20])
	copy(a.Device[:], raw[20:52])
	a.Day = binary.BigEndian.Uint32(raw[52:56])
	if a.Project != project {
		return Activation{}, ErrWrongProject
	}
	key, ok := keys[a.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return Activation{}, ErrUnknownKey
	}
	if !ed25519.Verify(key, append([]byte(activationDomain), raw[:activationPayloadSize]...), raw[activationPayloadSize:]) {
		return Activation{}, ErrBadSignature
	}
	if a.Device != DeviceHash(installID) {
		return Activation{}, ErrNotThisDevice
	}
	return a, nil
}

func (a Activation) payload() []byte {
	p := make([]byte, activationPayloadSize, activationPayloadSize+ed25519.SignatureSize)
	p[0] = Version
	p[1] = a.KeyID
	binary.BigEndian.PutUint16(p[2:4], a.Project)
	copy(p[4:20], a.CodeID[:])
	copy(p[20:52], a.Device[:])
	binary.BigEndian.PutUint32(p[52:56], a.Day)
	return p
}
