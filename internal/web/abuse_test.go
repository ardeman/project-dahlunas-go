package web

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestCheckoutLimiterRefillAndConcurrency(t *testing.T) {
	l := &checkoutLimiter{}
	now := time.Now()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow(now) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatal(accepted.Load())
	}
	if l.allow(now.Add(time.Second)) {
		t.Fatal("refilled too soon")
	}
	if !l.allow(now.Add(2 * time.Second)) {
		t.Fatal("did not refill")
	}
	for i := 0; i < 10; i++ {
		if !l.allow(now.Add(time.Hour)) {
			t.Fatal("burst did not recover")
		}
	}
	if l.allow(now.Add(time.Hour)) {
		t.Fatal("unbounded refill")
	}
}

type botVerifier struct {
	ok    bool
	err   error
	calls int
}

func (b *botVerifier) Verify(_ context.Context, token string) (bool, error) {
	b.calls++
	return b.ok && token == "challenge", b.err
}

func TestCheckoutBotProtection(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		ok          bool
		err         error
		status      int
	}{
		{"passed", "challenge", true, nil, 303},
		{"missing", "", true, nil, 403},
		{"rejected", "challenge", false, nil, 403},
		{"outage", "challenge", false, errors.New("unavailable"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &webStore{orders: map[string]shop.Order{}}
			b := &botVerifier{ok: tc.ok, err: tc.err}
			s := &shop.Service{Store: store, Gateway: &webGateway{}, Products: shop.CobaLagi(), BaseURL: "https://shop.example", SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return ed25519.NewKeyFromSeed(make([]byte, 32)), nil }}
			h := Handler(Options{Shop: s, TurnstileSiteKey: "public-site-key", BotVerifier: b})
			home := httptest.NewRecorder()
			h.ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
			if !strings.Contains(home.Body.String(), `data-sitekey="public-site-key"`) || !strings.Contains(home.Body.String(), `data-action="checkout"`) {
				t.Fatal("widget missing")
			}
			cookie := home.Result().Cookies()[0]
			v := url.Values{"csrf": {cookie.Value}, "accept": {"yes"}, "email": {"buyer@example.com"}, "product": {"coba-lagi-1"}, "cf-turnstile-response": {tc.token}}
			post := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/checkout", strings.NewReader(v.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := post(); w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status != 303 && len(store.orders) != 0 {
				t.Fatal("failed verification created order")
			}
			// Exhaust the limiter with invalid forms; it must stop before verification.
			v.Set("csrf", "invalid")
			for i := 0; i < 9; i++ {
				if w := post(); w.Code != 403 {
					t.Fatal(w.Code)
				}
			}
			v.Set("csrf", cookie.Value)
			w := post()
			if w.Code != 429 || w.Header().Get("Retry-After") != "2" || b.calls != 1 {
				t.Fatal("limit bypassed", w.Code, b.calls)
			}
			for _, path := range []string{"/health", "/webhooks/midtrans"} {
				method := "GET"
				if strings.HasPrefix(path, "/webhooks") {
					method = "POST"
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("invalid")))
				if w.Code == 429 {
					t.Fatal("limited unrelated endpoint")
				}
			}
		})
	}
}
