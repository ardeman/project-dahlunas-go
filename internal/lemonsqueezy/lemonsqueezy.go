// Package lemonsqueezy implements one-time USD checkout and verified orders.
package lemonsqueezy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

var numeric = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
var localID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var proofID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Client struct {
	key, secret, store, base string
	production               bool
	http                     *http.Client
}

func New(key, secret, store string, production bool) (*Client, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(secret) == "" || !numeric.MatchString(store) {
		return nil, errors.New("lemonsqueezy: API key, webhook secret and numeric store ID required")
	}
	return &Client{key: key, secret: secret, store: store, production: production, base: "https://api.lemonsqueezy.com/v1", http: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return errors.New("lemonsqueezy: request failed")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/vnd.api+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("lemonsqueezy: unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("lemonsqueezy: API rejected request")
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(result) != nil {
		return errors.New("lemonsqueezy: invalid response")
	}
	return nil
}

func (c *Client) Create(ctx context.Context, o payment.Checkout) (payment.Session, error) {
	if o.Currency != "USD" || o.Amount <= 0 || !numeric.MatchString(o.VariantID) || !localID.MatchString(o.ID) || !proofID.MatchString(o.Proof) {
		return payment.Session{}, errors.New("lemonsqueezy: invalid checkout")
	}
	// Subscriptions, pay-what-you-want and quantities are outside this checkout's contract.
	var variant struct {
		Data struct {
			Type, ID   string
			Attributes struct {
				IsSubscription *bool  `json:"is_subscription"`
				PayWhatYouWant *bool  `json:"pay_what_you_want"`
				Status         string `json:"status"`
			}
		}
	}
	if err := c.request(ctx, http.MethodGet, "/variants/"+o.VariantID, nil, &variant); err != nil {
		return payment.Session{}, err
	}
	if variant.Data.Type != "variants" || variant.Data.ID != o.VariantID || variant.Data.Attributes.IsSubscription == nil || *variant.Data.Attributes.IsSubscription || variant.Data.Attributes.PayWhatYouWant == nil || *variant.Data.Attributes.PayWhatYouWant || (variant.Data.Attributes.Status != "published" && variant.Data.Attributes.Status != "pending") {
		return payment.Session{}, errors.New("lemonsqueezy: require an available one-time fixed-price variant")
	}
	variantID, _ := strconv.ParseInt(o.VariantID, 10, 64)
	relationship := func(kind, id string) any { return map[string]any{"data": map[string]string{"type": kind, "id": id}} }
	body := map[string]any{"data": map[string]any{"type": "checkouts", "attributes": map[string]any{
		"custom_price": o.Amount, "test_mode": !c.production, "preview": true,
		"expires_at":       time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		"product_options":  map[string]any{"name": o.Name, "redirect_url": o.FinishURL, "receipt_link_url": o.FinishURL, "enabled_variants": []int64{variantID}},
		"checkout_options": map[string]any{"embed": false, "discount": false},
		"checkout_data":    map[string]any{"email": o.Email, "custom": map[string]string{"order_id": o.ID, "payment_proof": o.Proof}, "variant_quantities": []any{map[string]any{"variant_id": variantID, "quantity": 1}}},
	}, "relationships": map[string]any{"store": relationship("stores", c.store), "variant": relationship("variants", o.VariantID)}}}
	var result struct {
		Data struct {
			Type, ID   string
			Attributes struct {
				URL       string `json:"url"`
				TestMode  *bool  `json:"test_mode"`
				StoreID   int64  `json:"store_id"`
				VariantID int64  `json:"variant_id"`
				Preview   struct {
					Currency string `json:"currency"`
					Subtotal int64  `json:"subtotal"`
					Discount int64  `json:"discount_total"`
				} `json:"preview"`
			}
		}
	}
	if err := c.request(ctx, http.MethodPost, "/checkouts", body, &result); err != nil {
		return payment.Session{}, err
	}
	a := result.Data.Attributes
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".lemonsqueezy.com") || !strings.HasPrefix(u.Path, "/checkout/") || u.Fragment != "" || result.Data.Type != "checkouts" || result.Data.ID == "" || (a.TestMode == nil || *a.TestMode == c.production) || strconv.FormatInt(a.StoreID, 10) != c.store || a.VariantID != variantID || a.Preview.Currency != "USD" || a.Preview.Subtotal != o.Amount || a.Preview.Discount != 0 {
		return payment.Session{}, errors.New("lemonsqueezy: checkout configuration mismatch")
	}
	return payment.Session{URL: a.URL}, nil
}

type orderResource struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		StoreID      int64  `json:"store_id"`
		Currency     string `json:"currency"`
		Subtotal     int64  `json:"subtotal"`
		Discount     int64  `json:"discount_total"`
		TaxInclusive bool   `json:"tax_inclusive"`
		Status       string `json:"status"`
		Refunded     bool   `json:"refunded"`
		TestMode     *bool  `json:"test_mode"`
	} `json:"attributes"`
}

func (c *Client) Notification(ctx context.Context, body []byte, signature string) (payment.Status, error) {
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write(body)
	got, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return payment.Status{}, payment.ErrSignature
	}
	var event struct {
		Meta struct {
			Event  string `json:"event_name"`
			Custom struct {
				OrderID string `json:"order_id"`
				Proof   string `json:"payment_proof"`
			} `json:"custom_data"`
		} `json:"meta"`
		Data orderResource `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil {
		return payment.Status{}, errors.New("lemonsqueezy: invalid event")
	}
	if event.Meta.Event != "order_created" {
		return payment.Status{}, payment.ErrIgnored
	}
	if event.Data.Type != "orders" || !numeric.MatchString(event.Data.ID) || !localID.MatchString(event.Meta.Custom.OrderID) || !proofID.MatchString(event.Meta.Custom.Proof) {
		return payment.Status{}, errors.New("lemonsqueezy: invalid order binding")
	}
	var current struct {
		Data orderResource `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/orders/"+event.Data.ID, nil, &current); err != nil {
		return payment.Status{}, err
	}
	a := current.Data.Attributes
	if current.Data.Type != "orders" || current.Data.ID != event.Data.ID || strconv.FormatInt(a.StoreID, 10) != c.store || (a.TestMode == nil || *a.TestMode == c.production) || a.Currency != "USD" || a.Subtotal <= 0 || a.Discount != 0 || a.TaxInclusive {
		return payment.Status{}, errors.New("lemonsqueezy: order configuration mismatch")
	}
	var items struct {
		Meta struct {
			Page struct {
				Total int `json:"total"`
			} `json:"page"`
		} `json:"meta"`
		Data []struct {
			Type       string `json:"type"`
			Attributes struct {
				OrderID   int64 `json:"order_id"`
				VariantID int64 `json:"variant_id"`
				Price     int64 `json:"price"`
				Quantity  int64 `json:"quantity"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/order-items?filter%5Border_id%5D="+event.Data.ID+"&page%5Bsize%5D=2", nil, &items); err != nil {
		return payment.Status{}, err
	}
	if len(items.Data) != 1 || items.Meta.Page.Total != 1 {
		return payment.Status{}, errors.New("lemonsqueezy: require one order item")
	}
	item := items.Data[0].Attributes
	if items.Data[0].Type != "order-items" || item.Quantity != 1 || item.Price != a.Subtotal || strconv.FormatInt(item.OrderID, 10) != current.Data.ID || item.VariantID <= 0 {
		return payment.Status{}, errors.New("lemonsqueezy: order item mismatch")
	}
	return payment.Status{ID: event.Meta.Custom.OrderID, Amount: a.Subtotal, Currency: a.Currency, VariantID: strconv.FormatInt(item.VariantID, 10), Proof: event.Meta.Custom.Proof, ProviderID: current.Data.ID, State: a.Status, Paid: a.Status == "paid" && !a.Refunded}, nil
}
