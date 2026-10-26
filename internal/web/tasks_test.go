package web

import (
	"context"
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestDeliverySweepRequiresToken(t *testing.T) {
	store := &webStore{orders: map[string]shop.Order{}}
	m := &webMail{}
	s := &shop.Service{Store: store, Mailer: m, SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return nil, nil }}
	id := strings.Repeat("a", 32)
	store.Create(context.Background(), shop.Order{ID: id, Paid: true, Code: "DL1-TEST"})
	token := strings.Repeat("t", 32)
	h := Handler(Options{Shop: s, TaskToken: token})
	for _, auth := range []string{"", "Bearer wrong", token} {
		r := httptest.NewRequest("POST", "/tasks/deliveries", nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || m.calls != 0 {
			t.Fatal("unauthorized sweep", auth, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/tasks/deliveries", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || m.calls != 1 || !strings.Contains(w.Body.String(), `"delivered":1`) || !store.orders[id].EmailSent {
		t.Fatal(w.Code, w.Body.String(), m.calls)
	}
	// Without a configured token the route does not exist.
	w = httptest.NewRecorder()
	Handler(Options{Shop: s}).ServeHTTP(w, r)
	if w.Code != 404 && w.Code != 405 {
		t.Fatal("sweep served without a token", w.Code)
	}
}
