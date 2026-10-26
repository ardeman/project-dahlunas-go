package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestPreferred(t *testing.T) {
	for header, want := range map[string]string{
		"":                        "id",
		"en":                      "en",
		"fr-FR,fr;q=0.9":          "id",
		"en-GB,en;q=0.9":          "en",
		"id-ID,id;q=0.9,en;q=0.8": "id",
		"en;q=0.5,id;q=0.4":       "en",
		"id;q=0.3, en;q=0.7":      "en",
		"in":                      "id",
		"EN-us":                   "en",
	} {
		if got := preferred(header); got != want {
			t.Errorf("preferred(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestChosenLanguageCarriesThroughCheckoutErrors(t *testing.T) {
	h := Handler(Options{Shop: &shop.Service{Store: &webStore{orders: map[string]shop.Order{}}, Gateway: &webGateway{}, Products: shop.CobaLagi(), BaseURL: "https://shop.example"}})
	r := httptest.NewRequest(http.MethodPost, "/checkout?lang=en", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || rec.Body.String() != "Reload the page.\n" || rec.Header().Get("Content-Language") != "en" {
		t.Fatal(rec.Code, rec.Body.String(), rec.Header())
	}
	r = httptest.NewRequest(http.MethodPost, "/checkout", nil)
	r.Header.Set("Accept-Language", "id")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Body.String() != "Muat ulang halaman.\n" || rec.Header().Get("Vary") != "Accept-Language" {
		t.Fatal(rec.Body.String(), rec.Header())
	}
}
