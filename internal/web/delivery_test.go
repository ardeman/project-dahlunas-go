package web

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestPaidPageFollowsDelivery(t *testing.T) {
	file, err := os.Open("../../config/catalog-sandbox.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	products, err := shop.ParseCatalog(file)
	if err != nil {
		t.Fatal(err)
	}
	var plus, code shop.Product
	for _, p := range products {
		if p.Account && p.Months == 1 {
			plus = p
		} else if !p.Account && code.Slug == "" {
			code = p
		}
	}
	// Stored orders lose the website, as they do in Firestore.
	plus.Website = ""
	store := &webStore{orders: map[string]shop.Order{}}
	s := &shop.Service{Store: store, Gateway: &webGateway{}, Products: products, CheckoutSecret: "secret", BaseURL: "https://shop.example"}
	h := Handler(Options{Shop: s, ClientKey: "public-client", Support: "support@example.com", RefundID: "Uji", RefundEN: "Test"})
	id, view := strings.Repeat("a", 32), strings.Repeat("b", 64)
	const fast, slow, polls = `hx-trigger="every 5s"`, `hx-trigger="every 30s"`, "hx-get"
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }
	for name, c := range map[string]struct {
		order       shop.Order
		lang        string
		want, avoid []string
	}{
		"turning on":     {shop.Order{Product: plus, EmailFirstAttempt: ago(time.Second)}, "en", []string{fast, `hx-select="#delivery"`, "Turning on Plus, usually"}, []string{"Plus is on.", "Open Catat Saja"}},
		"not started":    {shop.Order{Product: plus}, "en", []string{fast, "Turning on Plus, usually"}, nil},
		"on, no email":   {shop.Order{Product: plus, AccountDone: true, EmailFirstAttempt: ago(time.Second)}, "en", []string{fast, "Plus is on.", "1 month added to your account.", `href="https://catatsaja.ardeman.com" rel="noreferrer"`, "email is on its way"}, []string{"Turning on"}},
		"done":           {shop.Order{Product: plus, AccountDone: true, EmailSent: true, EmailFirstAttempt: ago(time.Hour)}, "en", []string{"Plus is on.", "Open Catat Saja", "sent to your email", `<span class="nowrap">Rp 19.000</span>`}, []string{polls}},
		"selesai":        {shop.Order{Product: plus, AccountDone: true, EmailSent: true}, "id", []string{"Plus aktif.", "1 bulan ditambahkan", "Buka Catat Saja", "Konfirmasi telah dikirim"}, []string{polls}},
		"slow":           {shop.Order{Product: plus, EmailFirstAttempt: ago(5 * time.Minute)}, "en", []string{slow, "longer than usual", "mailto:support@example.com"}, []string{"Open Catat Saja"}},
		"lambat":         {shop.Order{Product: plus, EmailFirstAttempt: ago(5 * time.Minute)}, "id", []string{slow, "lebih lama dari biasanya"}, nil},
		"on, slow email": {shop.Order{Product: plus, AccountDone: true, EmailFirstAttempt: ago(5 * time.Minute)}, "en", []string{slow, "but your Plus is on"}, nil},
		"stopped":        {shop.Order{Product: plus, EmailFirstAttempt: ago(2 * time.Hour)}, "en", []string{"longer than usual"}, []string{polls}},
		"code pending":   {shop.Order{Product: code, Code: "CODE", EmailFirstAttempt: ago(time.Second)}, "en", []string{fast, ">CODE</textarea>", "email is on its way"}, []string{"Plus"}},
		"code slow":      {shop.Order{Product: code, Code: "CODE", EmailFirstAttempt: ago(5 * time.Minute)}, "en", []string{slow, "mailto:support@example.com"}, nil},
		"code done":      {shop.Order{Product: code, Code: "CODE", EmailSent: true}, "en", []string{">CODE</textarea>", "sent by email"}, []string{polls}},
	} {
		t.Run(name, func(t *testing.T) {
			o := c.order
			o.ID, o.ViewToken, o.Paid, o.State = id, view, true, "settlement"
			store.orders[id] = o
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://shop.example/orders/"+id+"/"+view+"?lang="+c.lang, nil))
			body := w.Body.String()
			for _, want := range c.want {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q in %s", want, body)
				}
			}
			for _, avoid := range c.avoid {
				if strings.Contains(body, avoid) {
					t.Fatalf("unwanted %q in %s", avoid, body)
				}
			}
			// The code must stay outside what refreshes, so copying isn't disturbed.
			if i, j := strings.Index(body, "<textarea"), strings.Index(body, `id="delivery"`); j < 0 || i > j {
				t.Fatal("code inside the refreshed part")
			}
		})
	}
}
