package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestRetryUsesSameIdempotencyKeyAndBody(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Idempotency-Key") != "unlock/order-1" {
			t.Error("headers")
		}
		var body struct {
			To      []string `json:"to"`
			Text    string   `json:"text"`
			Subject string   `json:"subject"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.To) != 1 || body.To[0] != "buyer@example.com" || !strings.Contains(body.Text, "DL1.test-code") || !strings.Contains(body.Text, "Thank you") || !strings.Contains(body.Text, "Terima kasih") {
			t.Error("email contents")
		}
		if body.Subject != "Kode Kedua / Your Second code" || !strings.Contains(body.Text, "Open settings") || strings.Contains(body.Text, "Coba Lagi") {
			t.Error("wrong project wording")
		}
		bodies = append(bodies, body.Text)
		w.Write([]byte(`{"id":"email-1"}`))
	}))
	defer server.Close()
	m := New("test-key", "shop@example.com", "support@example.com")
	m.endpoint = server.URL
	o := shop.Order{ID: "order-1", Email: "buyer@example.com", Code: "DL1.test-code", Product: shop.CobaLagi()[0]}
	o.Product.Delivery = shop.EmailCopy{Subject: shop.Text{ID: "Kode Kedua", EN: "Your Second code"}, ThankYou: shop.Text{ID: "Terima kasih", EN: "Thank you"}, Instructions: shop.Text{ID: "Buka pengaturan", EN: "Open settings"}}
	for i := 0; i < 2; i++ {
		if err := m.Send(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	if bodies[0] != bodies[1] {
		t.Fatal("retry body changed")
	}
}

func TestEmailProviderFailureIsNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("private provider details"))
	}))
	defer server.Close()
	m := New("secret", "shop@example.com", "support@example.com")
	m.endpoint = server.URL
	err := m.Send(context.Background(), shop.Order{})
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

func TestEmailUsesOrderLanguage(t *testing.T) {
	var subject, text string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Subject, Text string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		subject, text = body.Subject, body.Text
		w.Write([]byte(`{"id":"email-1"}`))
	}))
	defer server.Close()
	m := New("test-key", "shop@example.com", "support@example.com")
	m.endpoint = server.URL
	copy := shop.EmailCopy{Subject: shop.Text{ID: "Kode", EN: "Code"}, ThankYou: shop.Text{ID: "Terima kasih", EN: "Thank you"}, Instructions: shop.Text{ID: "Buka", EN: "Open"}}
	for _, tc := range []struct {
		lang          string
		account       bool
		subject, text string
	}{
		{"id", false, "Kode", "Terima kasih\n\nDL1.x\n\nBuka\n\nPesanan: o1\nBantuan: support@example.com"},
		{"en", false, "Code", "Thank you\n\nDL1.x\n\nOpen\n\nOrder: o1\nSupport: support@example.com"},
		{"en", true, "Code", "Thank you\n\nOpen\n\nOrder: o1\nSupport: support@example.com"},
		{"", false, "Kode / Code", "Terima kasih / Thank you\n\nDL1.x\n\nBuka\nOpen\n\nPesanan / Order: o1\nBantuan / Support: support@example.com"},
	} {
		o := shop.Order{ID: "o1", Email: "buyer@example.com", Code: "DL1.x", Lang: tc.lang}
		o.Product.Delivery, o.Product.Account = copy, tc.account
		if err := m.Send(context.Background(), o); err != nil || subject != tc.subject || text != tc.text {
			t.Fatalf("%s: %v %q %q", tc.lang, err, subject, text)
		}
	}
}
