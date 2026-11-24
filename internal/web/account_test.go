package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestAccountCheckoutPage(t *testing.T) {
	file, err := os.Open("../../config/catalog-sandbox.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	products, err := shop.ParseCatalog(file)
	if err != nil {
		t.Fatal(err)
	}
	s := &shop.Service{Store: &webStore{orders: map[string]shop.Order{}}, Gateway: &webGateway{}, Products: products, CheckoutSecret: "secret", BaseURL: "https://shop.example"}
	h := Handler(Options{Shop: s, ClientKey: "public-client", Support: "support@example.com", RefundID: "Uji", RefundEN: "Test"})
	get := func(query url.Values) string {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://shop.example/projects/catat-saja?"+query.Encode(), nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"uid":"u","email":"a@example.com","exp":%d}`, time.Now().Add(time.Minute).Unix())))
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(payload))
	token := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	for lang, wants := range map[string][]string{
		"id": {"1 bulan", "3 bulan", "12 bulan", "Pilih lama aktif"},
		"en": {"1 month<", "3 months", "12 months", "Choose a duration"},
	} {
		body := get(url.Values{"lang": {lang}, "token": {token}})
		for _, want := range append(wants, `name="account_token" value="`+token+`"`, `value="a@example.com"`) {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: missing %q in %s", lang, want, body)
			}
		}
		// Follow the language link as a browser would: the token must survive.
		for _, other := range []string{"id", "en"} {
			if !strings.Contains(body, `href="?lang=`+other+`&amp;token=`+token+`"`) {
				t.Fatalf("%s: link to %s lost the token: %s", lang, other, body)
			}
		}
	}
	for _, bad := range []string{"", token + "x"} {
		body := get(url.Values{"token": {bad}})
		if !strings.Contains(body, "Buka dari Catat Saja") || strings.Contains(body, "account_token") || !strings.Contains(body, `href="?lang=en"`) {
			t.Fatal(bad, body)
		}
	}
	// The order page repeats the chosen duration.
	id, view := strings.Repeat("a", 32), strings.Repeat("b", 64)
	s.Store.(*webStore).orders[id] = shop.Order{ID: id, ViewToken: view, Product: products[len(products)-2], State: "pending", SnapToken: "snap"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://shop.example/orders/"+id+"/"+view+"?lang=en", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Catat Saja Plus · 3 months · Rp 49.000") {
		t.Fatal(w.Code, w.Body.String())
	}
}
