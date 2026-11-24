package web

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestResubmittedFormOpensSameOrder(t *testing.T) {
	store, bot := &webStore{orders: map[string]shop.Order{}}, &botVerifier{ok: true}
	s := &shop.Service{Store: store, Gateway: &webGateway{}, Products: shop.CobaLagi(), BaseURL: "https://shop.example", SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return ed25519.NewKeyFromSeed(make([]byte, 32)), nil }}
	h := Handler(Options{Shop: s, TurnstileSiteKey: "public-site-key", BotVerifier: bot})
	form := func() *http.Cookie {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		for _, want := range []string{`id="recent-order"`, `id="checkout"`, "/static/recent.js"} {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatal("missing", want)
			}
		}
		return w.Result().Cookies()[0]
	}
	post := func(c *http.Cookie) string {
		v := url.Values{"csrf": {c.Value}, "accept": {"yes"}, "email": {"buyer@example.com"}, "product": {"coba-lagi-1"}, "cf-turnstile-response": {"challenge"}}
		r := httptest.NewRequest("POST", "/checkout", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(c)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 303 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Header().Get("Location")
	}
	c := form()
	path := post(c)
	// Turnstile tokens work once, so the second submission must not verify again.
	bot.ok = false
	if post(c) != path || len(store.orders) != 1 || bot.calls != 1 {
		t.Fatal("duplicate order", len(store.orders), bot.calls)
	}
	bot.ok = true
	if post(form()) == path || len(store.orders) != 2 {
		t.Fatal("buying again must stay possible")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if b := w.Body.String(); !strings.Contains(b, `data-project="coba-lagi"`) || !strings.Contains(b, `data-state="pending"`) || !strings.Contains(b, "/static/recent.js") {
		t.Fatal(b)
	}
	for _, o := range store.orders {
		if o.Lang != "id" {
			t.Fatal("order must record the shop language", o.Lang)
		}
	}
	if strings.Contains(w.Body.String(), "/change") {
		t.Fatal("nothing to change before a method is chosen")
	}
	for id, o := range store.orders {
		if strings.Contains(path, id) {
			o.Method = "echannel"
			store.orders[id] = o
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if b := w.Body.String(); !strings.Contains(b, `action="`+path+`/change"`) {
		t.Fatal(b)
	}
	// This gateway can't cancel, so the live payment must stay the only one.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path+"/change", nil))
	if w.Code != 409 || len(store.orders) != 2 {
		t.Fatal(w.Code, len(store.orders))
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path[:len(path)-1]+"x/change", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	c = form()
	v := url.Values{"csrf": {c.Value}, "accept": {"yes"}, "email": {"buyer@example.com"}, "product": {"coba-lagi-1"}, "cf-turnstile-response": {"challenge"}}
	r := httptest.NewRequest("POST", "/checkout?lang=en", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(c)
	h.ServeHTTP(httptest.NewRecorder(), r)
	english := 0
	for _, o := range store.orders {
		if o.Lang == "en" {
			english++
		}
	}
	if english != 1 {
		t.Fatal("English checkout not recorded", english)
	}
}
