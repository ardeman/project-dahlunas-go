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
func (s *webStore) Undelivered(_ context.Context) ([]shop.Order, error) {
	var result []shop.Order
	for _, o := range s.orders {
		if o.DeliveryPending() {
			result = append(result, o)
		}
	}
	return result, nil
}
func (s *webStore) Unpaid(_ context.Context) ([]shop.Order, error) {
	var result []shop.Order
	for _, o := range s.orders {
		if o.AwaitingPayment() {
			result = append(result, o)
		}
	}
	return result, nil
}

type webGateway struct {
	checkout  payment.Checkout
	paid      bool
	signature string
}

func (g *webGateway) Create(_ context.Context, c payment.Checkout) (payment.Session, error) {
	g.checkout = c
	if c.Currency == "USD" {
		return payment.Session{URL: "https://shop.lemonsqueezy.com/checkout/custom/test"}, nil
	}
	return payment.Session{Token: "browser-token"}, nil
}
func (g *webGateway) Notification(_ context.Context, b []byte, signature string) (payment.Status, error) {
	g.signature = signature
	if string(b) != "valid" {
		return payment.Status{}, midtrans.ErrSignature
	}
	return payment.Status{ID: g.checkout.ID, Amount: g.checkout.Amount, State: "settlement", Paid: g.paid, Currency: g.checkout.Currency, VariantID: g.checkout.VariantID, Proof: g.checkout.Proof, ProviderID: "56"}, nil
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
	if home.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("checkout form must keep its Origin header", home.Header().Get("Referrer-Policy"))
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

func TestProjectCopyOnPages(t *testing.T) {
	product := shop.Product{Slug: "second", NameID: "Produk Kedua", NameEN: "Second product", DescriptionID: "Fitur kedua", DescriptionEN: "Second features", Price: 9000, Project: 2, Product: 7, KeyID: 3, Delivery: shop.EmailCopy{Instructions: shop.Text{ID: "Buka pengaturan", EN: "Open settings"}}}
	o := shop.Order{ID: strings.Repeat("a", 32), ViewToken: strings.Repeat("b", 64), Product: product, Paid: true, Code: "DL1.example"}
	store := &webStore{orders: map[string]shop.Order{o.ID: o}}
	s := &shop.Service{Store: store, Products: []shop.Product{product}, BaseURL: "https://shop.example"}
	h := Handler(Options{Shop: s})
	for _, path := range []string{"/", "/terms", o.Path()} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "Coba Lagi") {
			t.Fatal(path, "wrong project copy")
		}
		want := "Second product"
		if path == o.Path() {
			want = "Open settings"
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal(path, "missing project wording")
		}
	}
}

func TestInternationalCheckoutAndSignedWebhookRoute(t *testing.T) {
	g, m := &webGateway{}, &webMail{}
	p := shop.CobaLagi()[0]
	p.Gateway = payment.LemonSqueezy
	p.Currency = "USD"
	p.Price = 599
	p.VariantID = "34"
	p.Slug = "international"
	s := &shop.Service{Mailer: m, Store: &webStore{orders: map[string]shop.Order{}}, Gateways: map[string]payment.Gateway{payment.LemonSqueezy: g}, Products: []shop.Product{p}, BaseURL: "https://shop.example", SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return ed25519.NewKeyFromSeed(make([]byte, 32)), nil }}
	h := Handler(Options{Shop: s})
	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(home.Body.String(), "US$ 5.99") || !strings.Contains(home.Body.String(), "Lemon Squeezy") {
		t.Fatal("missing USD product")
	}
	cookie := home.Result().Cookies()[0]
	values := url.Values{"csrf": {cookie.Value}, "accept": {"yes"}, "email": {"buyer@example.com"}, "product": {"international"}}
	r := httptest.NewRequest("POST", "https://shop.example/checkout", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := w.Header().Get("Location")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path+"?status=paid", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://shop.lemonsqueezy.com/checkout/custom/test") || strings.Contains(w.Body.String(), "snap.js") || strings.Contains(w.Body.String(), "data-snap-token") || strings.Contains(w.Body.String(), g.checkout.Proof) || strings.Contains(w.Body.String(), "DL1.") {
		t.Fatal("incorrect hosted checkout page")
	}
	// An invalid signature must not be retried as a delivery error.
	r = httptest.NewRequest("POST", "/webhooks/lemonsqueezy", strings.NewReader("invalid"))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	g.paid = true
	for i := 0; i < 2; i++ {
		r = httptest.NewRequest("POST", "/webhooks/lemonsqueezy", strings.NewReader("valid"))
		r.Header.Set("X-Signature", "signed-payload")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || g.signature != "signed-payload" {
			t.Fatal("signature header not forwarded", w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "DL1.") || !strings.Contains(w.Body.String(), "US$ 5.99") || m.calls != 1 {
		t.Fatal("international order not fulfilled once")
	}
}
