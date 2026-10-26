package web

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

type webActivations map[string]shop.ActivationRecord

func (m webActivations) CreateActivation(_ context.Context, a shop.ActivationRecord) error {
	if _, ok := m[a.CodeID]; ok {
		return shop.ErrConflict
	}
	m[a.CodeID] = a
	return nil
}
func (m webActivations) GetActivation(_ context.Context, id string) (shop.ActivationRecord, error) {
	a, ok := m[id]
	if !ok {
		return a, shop.ErrNotFound
	}
	return a, nil
}
func (m webActivations) SaveActivation(_ context.Context, a shop.ActivationRecord) error {
	m[a.CodeID] = a
	return nil
}

func TestActivationEndpoint(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	s := &shop.Service{Activations: webActivations{}, Products: []shop.Product{{Slug: "bedug-1", Project: 2, Product: 1, KeyID: 1, Activation: true}}, SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return key, nil }}
	h := Handler(Options{Shop: s})
	code, _ := unlock.New(key, 1, 2, 1, time.Now())
	post := func(body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/activations", strings.NewReader(body)))
		var answer map[string]any
		json.NewDecoder(w.Body).Decode(&answer)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("activation answers must not be cached")
		}
		return w.Code, answer
	}
	first, second := strings.Repeat("1", 32), strings.Repeat("2", 32)
	if status, answer := post(`{"code":"` + code + `","install_id":"` + first + `"}`); status != 200 || !strings.HasPrefix(answer["activation"].(string), "DA1.") || answer["moves_left"] != float64(shop.MaxMoves) {
		t.Fatal(status, answer)
	}
	if status, answer := post(`{"code":"` + code + `","install_id":"` + second + `"}`); status != 409 || answer["error"] != "moved" {
		t.Fatal(status, answer)
	}
	if status, answer := post(`{"code":"` + code + `","install_id":"` + second + `","move":true}`); status != 200 || answer["moves_left"] != float64(shop.MaxMoves-1) {
		t.Fatal(status, answer)
	}
	for _, body := range []string{`{"code":"DL1.x","install_id":"` + first + `"}`, `{"code":"` + code + `","install_id":"` + first + `","email":"a@b.c"}`, `not json`} {
		if status, answer := post(body); status != 400 || answer["error"] != "invalid" {
			t.Fatal(body, status, answer)
		}
	}
	if w := httptest.NewRecorder(); func() int {
		Handler(Options{}).ServeHTTP(w, httptest.NewRequest("POST", "/activations", nil))
		return w.Code
	}() == 200 {
		t.Fatal("activation served without a shop")
	}
}
