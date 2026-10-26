package midtrans

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

func TestSnapRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, password, ok := r.BasicAuth()
		if !ok || key != "test-key" || password != "" || r.Method != "POST" {
			t.Error("invalid authentication or method")
		}
		var body struct {
			Details struct {
				ID     string `json:"order_id"`
				Amount int64  `json:"gross_amount"`
			} `json:"transaction_details"`
			Methods   []string          `json:"enabled_payments"`
			Callbacks map[string]string `json:"callbacks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Details.Amount != 5000 || body.Details.ID != "order-1" || strings.Join(body.Methods, ",") != "qris" || body.Callbacks["finish"] != "https://shop.example/orders/private" {
			t.Error("checkout contract changed")
		}
		w.Write([]byte(`{"token":"snap-token"}`))
	}))
	defer server.Close()
	c := New("test-key", false)
	c.snapURL = server.URL
	s, err := c.Create(context.Background(), payment.Checkout{ID: "order-1", Amount: 5000, Email: "buyer@example.com", Name: "Code", FinishURL: "https://shop.example/orders/private"})
	if err != nil || s.Token != "snap-token" {
		t.Fatalf("Create = %v, %v", s, err)
	}
}

func signed(n notification) []byte {
	digest := sha512.Sum512([]byte(n.ID + n.Code + n.Amount + "test-key"))
	n.Signature = hex.EncodeToString(digest[:])
	b, _ := json.Marshal(n)
	return b
}

func TestNotificationUsesAuthoritativeStatus(t *testing.T) {
	for _, tc := range []struct {
		state, fraud, code, amount string
		paid, invalid              bool
	}{
		{"pending", "", "201", "5000.00", false, false},
		{"settlement", "", "200", "5000.00", true, false},
		{"settlement", "deny", "200", "5000.00", false, false},
		{"capture", "challenge", "200", "5000.00", false, false},
		{"capture", "accept", "200", "5000.00", true, false},
		{"refund", "accept", "200", "5000.00", false, false},
		{"settlement", "accept", "201", "5000.00", false, false},
		{"settlement", "accept", "200", "5000.50", false, true},
	} {
		t.Run(tc.state+tc.fraud+tc.code+tc.amount, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/order-1/status" {
					t.Error("incorrect status URL")
				}
				json.NewEncoder(w).Encode(notification{ID: "order-1", Code: tc.code, Amount: tc.amount, State: tc.state, Fraud: tc.fraud})
			}))
			defer server.Close()
			c := New("test-key", false)
			c.statusURL = server.URL + "/"
			// A valid signature on a forged successful body is insufficient.
			s, err := c.Notification(context.Background(), signed(notification{ID: "order-1", Code: "200", Amount: "5000.00", State: "settlement", Fraud: "accept"}), "")
			if (err != nil) != tc.invalid || s.Paid != tc.paid {
				t.Fatalf("status=%+v err=%v", s, err)
			}
		})
	}
}

func TestBadSignatureDoesNotContactProvider(t *testing.T) {
	c := New("test-key", false)
	c.statusURL = "http://invalid.invalid/"
	for _, data := range []string{`{`, `{}`, `{"order_id":"order-1","signature_key":"aa"}`} {
		if _, err := c.Notification(context.Background(), []byte(data), ""); !errors.Is(err, ErrSignature) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestProviderFailuresDoNotLeakSecrets(t *testing.T) {
	for _, body := range []string{`{"token":""}`, "not JSON"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		c := New("test-key", false)
		c.snapURL = server.URL
		_, err := c.Create(context.Background(), payment.Checkout{})
		if err == nil || strings.Contains(err.Error(), "test-key") {
			t.Errorf("error=%v", err)
		}
		server.Close()
	}
	c := New("secret", true)
	if c.snapURL != "https://app.midtrans.com/snap/v1/transactions" || c.statusURL != "https://api.midtrans.com/v2/" {
		t.Fatal("wrong production endpoints")
	}
}
