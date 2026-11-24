package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestProductionCatalogListsSoonProjectsWithoutSelling(t *testing.T) {
	file, err := os.Open("../../config/catalog-production.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	products, err := shop.ParseCatalog(file)
	if err != nil {
		t.Fatal(err)
	}
	// Coba Lagi must keep the default catalog's products and prices, with the production key.
	for i, want := range shop.CobaLagi() {
		if got := products[i]; got.Slug != want.Slug || got.Price != want.Price || got.Product != want.Product || got.KeyID != 2 || got.Soon {
			t.Fatal(got)
		}
	}
	s := &shop.Service{Store: &webStore{orders: map[string]shop.Order{}}, Gateway: &webGateway{}, Products: products, BaseURL: "https://shop.example"}
	h := Handler(Options{Shop: s, ClientKey: "public-client", Support: "support@example.com", RefundID: "Uji", RefundEN: "Test"})
	get := func(path string) string {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://shop.example"+path+"?lang=en", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "</html>") {
			t.Fatal(path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if home := get("/"); strings.Count(home, `class="card"`) != 3 || strings.Count(home, "Coming soon") != 2 || strings.Count(home, "From Rp 5.000") != 1 {
		t.Fatal(home)
	}
	if about := get("/about"); strings.Count(about, "Coming soon") != 2 || strings.Contains(about, "repaid access") {
		t.Fatal(about)
	}
	if terms := get("/terms"); strings.Contains(terms, "Bedug") || strings.Contains(terms, "Catat Saja") {
		t.Fatal(terms)
	}
	for _, slug := range []string{"bedug", "catat-saja"} {
		if page := get("/projects/" + slug); !strings.Contains(page, "Not on sale yet") || strings.Contains(page, "<form") || strings.Contains(page, "Rp ") {
			t.Fatal(page)
		}
	}
	for _, p := range products {
		if p.Soon {
			if _, err := s.Create(context.Background(), p.Slug, "buyer@example.com", ""); !errors.Is(err, shop.ErrInput) {
				t.Fatal(p.Slug, err)
			}
		}
	}
	if !strings.Contains(get("/projects/coba-lagi"), "Continue to payment") {
		t.Fatal("Coba Lagi must stay on sale")
	}
}
