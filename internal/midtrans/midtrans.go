// Package midtrans implements Snap checkout without exposing server credentials.
package midtrans

import (
	"bytes"
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

var ErrSignature = errors.New("midtrans: invalid notification")

type Client struct {
	key, snapURL, statusURL string
	http                    *http.Client
}

func New(key string, production bool) *Client {
	snap, api := "https://app.sandbox.midtrans.com", "https://api.sandbox.midtrans.com"
	if production {
		snap, api = "https://app.midtrans.com", "https://api.midtrans.com"
	}
	return &Client{key, snap + "/snap/v1/transactions", api + "/v2/", &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return errors.New("midtrans: request failed")
	}
	r.SetBasicAuth(c.key, "")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(r)
	if err != nil {
		return errors.New("midtrans: unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("midtrans: API rejected request")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(result); err != nil {
		return errors.New("midtrans: invalid response")
	}
	return nil
}

func (c *Client) Create(ctx context.Context, o payment.Checkout) (payment.Session, error) {
	// Start with QRIS; other methods need price thresholds before being offered.
	body := map[string]any{
		"transaction_details": map[string]any{"order_id": o.ID, "gross_amount": o.Amount},
		"item_details":        []any{map[string]any{"id": "unlock", "price": o.Amount, "quantity": 1, "name": o.Name}},
		"customer_details":    map[string]string{"email": o.Email},
		"enabled_payments":    []string{"qris"},
		"callbacks":           map[string]string{"finish": o.FinishURL},
	}
	var s payment.Session
	err := c.request(ctx, http.MethodPost, c.snapURL, body, &s)
	if err == nil && s.Token == "" {
		err = errors.New("midtrans: missing token")
	}
	return s, err
}

type notification struct {
	ID        string `json:"order_id"`
	Code      string `json:"status_code"`
	Amount    string `json:"gross_amount"`
	Signature string `json:"signature_key"`
	State     string `json:"transaction_status"`
	Fraud     string `json:"fraud_status"`
}

func (c *Client) Notification(ctx context.Context, data []byte) (payment.Status, error) {
	var n notification
	if json.Unmarshal(data, &n) != nil || n.ID == "" || len(n.ID) > 50 || c.key == "" {
		return payment.Status{}, ErrSignature
	}
	want := sha512.Sum512([]byte(n.ID + n.Code + n.Amount + c.key))
	got, err := hex.DecodeString(n.Signature)
	if err != nil || subtle.ConstantTimeCompare(got, want[:]) != 1 {
		return payment.Status{}, ErrSignature
	}
	// The signature does not cover transaction_status/fraud_status. Fetch status
	// from Midtrans before trusting those fields, even for signed notifications.
	var current notification
	if err := c.request(ctx, http.MethodGet, c.statusURL+url.PathEscape(n.ID)+"/status", nil, &current); err != nil {
		return payment.Status{}, err
	}
	amount, err := rupiah(current.Amount)
	if err != nil || current.ID != n.ID {
		return payment.Status{}, errors.New("midtrans: invalid status")
	}
	paid := current.Code == "200" && ((current.State == "settlement" && (current.Fraud == "" || current.Fraud == "accept")) || (current.State == "capture" && current.Fraud == "accept"))
	return payment.Status{ID: current.ID, Amount: amount, State: current.State, Paid: paid}, nil
}

func rupiah(s string) (int64, error) {
	whole, fraction, _ := strings.Cut(s, ".")
	if fraction != "" && fraction != "00" {
		return 0, errors.New("midtrans: fractional rupiah")
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("midtrans: invalid amount")
	}
	return n, nil
}
