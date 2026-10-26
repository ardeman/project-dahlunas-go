package web

import (
	"context"
	"crypto/ed25519"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/midtrans"
	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

type webStore struct {
	orders   map[string]shop.Order
	revision int
}

func (s *webStore) Create(_ context.Context, o shop.Order) error {
	if _, ok := s.orders[o.ID]; ok {
		return shop.ErrConflict
	}
	s.revision++
	o.Revision = strconv.Itoa(s.revision)
	s.orders[o.ID] = o
	return nil
}
func (s *webStore) Get(_ context.Context, id string) (shop.Order, error) {
	o, ok := s.orders[id]
	if !ok {
		return o, shop.ErrNotFound
	}
	return o, nil
}
func (s *webStore) Save(_ context.Context, o shop.Order) error {
	if old, ok := s.orders[o.ID]; !ok || old.Revision != o.Revision {
		return shop.ErrConflict
	}
	s.revision++
	o.Revision = strconv.Itoa(s.revision)
	s.orders[o.ID] = o
	return nil
}

type webGateway struct {
	checkout payment.Checkout
	paid     bool
}

func (g *webGateway) Create(_ context.Context, c payment.Checkout) (payment.Session, error) {
	g.checkout = c
	return payment.Session{Token: "browser-token"}, nil
}
func (g *webGateway) Notification(_ context.Context, b []byte) (payment.Status, error) {
	if string(b) != "valid" {
		return payment.Status{}, midtrans.ErrSignature
	}
	return payment.Status{ID: g.checkout.ID, Amount: g.checkout.Amount, State: "settlement", Paid: g.paid}, nil
}

type webMail struct{ calls int }

func (m *webMail) Send(context.Context, shop.Order) error { m.calls++; return nil }

func TestBrowserCheckoutThroughPaidPage(t *testing.T) {
	g, m := &webGateway{}, &webMail{}
	s := &shop.Service{Store: &webStore{orders: map[string]shop.Order{}}, Gateway: g, Mailer: m, Products: shop.CobaLagi(), BaseURL: "https://shop.example", SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return ed25519.NewKeyFromSeed(make([]byte, 32)), nil }}
	h := Handler(Options{Shop: s, ClientKey: "public-client", Support: "support@example.com", RefundID: "Uji", RefundEN: "Test"})
	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "https://shop.example/", nil))
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Rp 5000") || !strings.Contains(home.Body.String(), "Rp 19000") || !strings.Contains(home.Body.String(), "Rp 49000") {
		t.Fatal(home.Code, home.Body.String())
	}
	cookie := home.Result().Cookies()[0]
	values := url.Values{"csrf": {cookie.Value}, "accept": {"yes"}, "email": {"buyer@example.com"}, "product": {"coba-lagi-1"}, "amount": {"1"}}
	post := func(origin, csrf, accept string) *httptest.ResponseRecorder {
		values.Set("csrf", csrf)
		values.Set("accept", accept)
		r := httptest.NewRequest("POST", "https://shop.example/checkout", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post("https://evil.example", cookie.Value, "yes"); w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
	if w := post("https://shop.example", "bad", "yes"); w.Code != 403 {
		t.Fatal("bad csrf", w.Code)
	}
	if w := post("https://shop.example", cookie.Value, ""); w.Code != 400 {
		t.Fatal("missing terms", w.Code)
	}
	w := post("https://shop.example", cookie.Value, "yes")
	if w.Code != 303 || g.checkout.Amount != 5000 {
		t.Fatal("checkout", w.Code, g.checkout.Amount, w.Body.String())
	}
	path := w.Header().Get("Location")
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	before := get(path + "?transaction_status=settlement")
	if before.Code != 200 || strings.Contains(before.Body.String(), "DL1.") || !strings.Contains(before.Body.String(), "browser-token") {
		t.Fatal("browser marked order paid", before.Body.String())
	}
	if before.Header().Get("Cache-Control") != "no-store" || before.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("private page headers")
	}
	if wrong := get("/orders/" + g.checkout.ID + "/" + strings.Repeat("0", 64)); wrong.Code != 404 {
		t.Fatal("wrong token", wrong.Code)
	}
	notify := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/webhooks/midtrans", strings.NewReader(body)))
		return w
	}
	if w := notify("invalid"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	g.paid = true
	for i := 0; i < 2; i++ {
		if w := notify("valid"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	after := get(path)
	if after.Code != 200 || !strings.Contains(after.Body.String(), "DL1.") || !strings.Contains(after.Body.String(), "The code has been sent by email") || m.calls != 1 {
		t.Fatal("paid page", after.Code, m.calls, after.Body.String())
	}
	if strings.Contains(after.Body.String(), "buyer@example.com") || strings.Contains(after.Body.String(), "every 5s") {
		t.Fatal("paid page leaks email or keeps polling")
	}
}

func TestPoliciesAndDisabledCheckout(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/terms", "/privacy", "/refunds"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `lang="en"`) {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/checkout", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
