package shop

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

// MaxMoves bounds how often a code moves to another device by itself; support
// can reset Moves after that.
const MaxMoves = 3

var (
	ErrActivationOff = errors.New("shop: project does not activate codes")
	ErrMoved         = errors.New("shop: code is active on another device")
	ErrMoveLimit     = errors.New("shop: code has no moves left")
)

// ActivationRecord holds the one device a code is active on, as a hash of the
// app's random install ID; it holds no buyer data.
type ActivationRecord struct {
	CodeID             string
	Project            uint16
	Device             string
	Moves              int
	Activated, Updated time.Time
	// Revision is the store's compare-and-swap token, never part of the record.
	Revision string `json:"-"`
}

// ActivationStore persists records by code ID with the same guarantees as Store.
type ActivationStore interface {
	CreateActivation(context.Context, ActivationRecord) error
	GetActivation(context.Context, string) (ActivationRecord, error)
	SaveActivation(context.Context, ActivationRecord) error
}

// installID is the app's random 16-byte install ID in lowercase hex.
var installID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Activate makes code active on the install and returns a signed activation.
// With move set (the buyer entered the code) a code active elsewhere moves
// here while moves remain; without it (the app's periodic check) it never
// moves and ErrMoved tells the app to give up its activation. The returned
// count is the moves left.
func (s *Service) Activate(ctx context.Context, code, install string, move bool) (string, int, error) {
	if !installID.MatchString(install) {
		return "", 0, ErrInput
	}
	parsed, err := unlock.Parse(code)
	if err != nil {
		return "", 0, ErrInput
	}
	var products []uint16
	var keyID uint8
	for _, p := range s.Products {
		if p.Project == parsed.Project && p.Activation {
			products, keyID = append(products, p.Product), p.KeyID
		}
	}
	if products == nil {
		return "", 0, ErrActivationOff
	}
	codeKey, err := s.SigningKey(parsed.Project, parsed.KeyID)
	if err != nil {
		return "", 0, ErrInput
	}
	c, err := unlock.Verify(code, parsed.Project, products, map[uint8]ed25519.PublicKey{parsed.KeyID: codeKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return "", 0, ErrInput
	}
	device := unlock.DeviceHash(install)
	now := s.now()
	record, err := s.Activations.GetActivation(ctx, hex.EncodeToString(c.ID[:]))
	switch {
	case errors.Is(err, ErrNotFound):
		record = ActivationRecord{CodeID: hex.EncodeToString(c.ID[:]), Project: c.Project, Device: hex.EncodeToString(device[:]), Activated: now, Updated: now}
		err = s.Activations.CreateActivation(ctx, record)
	case err != nil:
	case record.Device == hex.EncodeToString(device[:]):
		// The same install checking again needs no write.
	case !move:
		return "", MaxMoves - record.Moves, ErrMoved
	case record.Moves >= MaxMoves:
		return "", 0, ErrMoveLimit
	default:
		record.Device, record.Moves, record.Activated, record.Updated = hex.EncodeToString(device[:]), record.Moves+1, now, now
		err = s.Activations.SaveActivation(ctx, record)
	}
	if err != nil {
		return "", 0, err
	}
	key, err := s.SigningKey(c.Project, keyID)
	if err != nil {
		return "", 0, err
	}
	day, err := unlock.IssuedDay(now)
	if err != nil {
		return "", 0, err
	}
	activation, err := unlock.SignActivation(key, unlock.Activation{KeyID: keyID, Project: c.Project, CodeID: c.ID, Device: device, Day: day})
	return activation, MaxMoves - record.Moves, err
}
