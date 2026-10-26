package shop

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

type memoryActivations struct {
	records map[string]ActivationRecord
	writes  int
}

func (m *memoryActivations) CreateActivation(_ context.Context, a ActivationRecord) error {
	if _, ok := m.records[a.CodeID]; ok {
		return ErrConflict
	}
	m.writes++
	a.Revision = strconv.Itoa(m.writes)
	m.records[a.CodeID] = a
	return nil
}

func (m *memoryActivations) GetActivation(_ context.Context, id string) (ActivationRecord, error) {
	a, ok := m.records[id]
	if !ok {
		return ActivationRecord{}, ErrNotFound
	}
	return a, nil
}

func (m *memoryActivations) SaveActivation(_ context.Context, a ActivationRecord) error {
	if m.records[a.CodeID].Revision != a.Revision {
		return ErrConflict
	}
	m.writes++
	a.Revision = strconv.Itoa(m.writes)
	m.records[a.CodeID] = a
	return nil
}

func TestActivationMovesBetweenDevices(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	products := CobaLagi()
	bedug := Product{Slug: "bedug-1", Project: 2, Product: 1, KeyID: 1, Activation: true}
	store := &memoryActivations{records: map[string]ActivationRecord{}}
	s := &Service{Activations: store, Products: append(products, bedug), SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return key, nil }, Now: func() time.Time { return time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC) }}
	ctx := context.Background()
	code, _ := unlock.New(key, 1, 2, 1, s.now())
	device := func(n int) string { return strings.Repeat(strconv.Itoa(n), 32) }
	public := map[uint8]ed25519.PublicKey{1: key.Public().(ed25519.PublicKey)}

	activation, left, err := s.Activate(ctx, code, device(1), false)
	if err != nil || left != MaxMoves {
		t.Fatal(left, err)
	}
	if _, err := unlock.VerifyActivation(activation, 2, device(1), public); err != nil {
		t.Fatal("first device's activation does not verify", err)
	}
	if _, _, err := s.Activate(ctx, code, device(1), true); err != nil || store.writes != 1 {
		t.Fatal("same device should renew without a move or write", err, store.writes)
	}
	if _, left, err := s.Activate(ctx, code, device(2), false); !errors.Is(err, ErrMoved) || left != MaxMoves {
		t.Fatal("a check from another device must not move the code", left, err)
	}
	for n := 2; n <= MaxMoves+1; n++ {
		activation, left, err = s.Activate(ctx, code, device(n), true)
		if err != nil || left != MaxMoves-(n-1) {
			t.Fatal(n, left, err)
		}
		if _, err := unlock.VerifyActivation(activation, 2, device(n-1), public); !errors.Is(err, unlock.ErrNotThisDevice) {
			t.Fatal("activation verified on the device the code left", err)
		}
	}
	if _, _, err := s.Activate(ctx, code, device(1), false); !errors.Is(err, ErrMoved) {
		t.Fatal("old device still holds the code", err)
	}
	if _, _, err := s.Activate(ctx, code, device(9), true); !errors.Is(err, ErrMoveLimit) {
		t.Fatal("moved past the limit", err)
	}

	coba, _ := unlock.New(key, 1, 1, 1, s.now())
	if _, _, err := s.Activate(ctx, coba, device(1), true); !errors.Is(err, ErrActivationOff) {
		t.Fatal("Coba Lagi codes must never activate online", err)
	}
	for _, bad := range []struct{ code, install string }{{code, "short"}, {code, strings.ToUpper(device(10))}, {"DL1.broken", device(1)}, {code[:len(code)-1] + "A", device(1)}} {
		if _, _, err := s.Activate(ctx, bad.code, bad.install, true); !errors.Is(err, ErrInput) {
			t.Fatal("accepted", bad, err)
		}
	}
}
