package lemonsqueezy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func sign(b []byte) string {
	m := hmac.New(sha256.New, []byte("webhook-secret"))
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}
func client(t *testing.T, fn transport) *Client {
	t.Helper()
	c, err := New("api-secret", "webhook-secret", "12", false)
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: fn}
	return c
}

const checkoutResponse = `{"data":{"type":"checkouts","id":"checkout-1","attributes":{"url":"https://shop.lemonsqueezy.com/checkout/custom/example?signature=test","store_id":12,"variant_id":34,"test_mode":true,"preview":{"currency":"USD","subtotal":599,"discount_total":0}}}}`
const variantResponse = `{"data":{"type":"variants","id":"34","attributes":{"status":"published","is_subscription":false,"pay_what_you_want":false}}}`
const orderResponse = `{"data":{"type":"orders","id":"56","attributes":{"store_id":12,"currency":"USD","subtotal":599,"discount_total":0,"tax":120,"total":719,"tax_inclusive":false,"status":"paid","test_mode":true,"refunded":false}}}`
const itemResponse = `{"meta":{"page":{"total":1}},"data":[{"type":"order-items","id":"78","attributes":{"order_id":56,"variant_id":34,"price":599,"quantity":1}}]}`

func event() []byte {
	return []byte(`{"meta":{"event_name":"order_created","custom_data":{"order_id":"` + strings.Repeat("a", 32) + `","payment_proof":"` + strings.Repeat("b", 64) + `"}},"data":{"type":"orders","id":"56","attributes":{"status":"paid"}}}`)
}

func TestCheckoutPayloadAndValidation(t *testing.T) {
	for _, name := range []string{"valid", "subscription", "pwyw", "draft", "missing variant flags", "wrong URL", "wrong currency", "wrong price", "wrong store", "wrong variant", "wrong mode", "missing mode"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := client(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer api-secret" || r.Header.Get("Content-Type") != "application/vnd.api+json" {
					t.Error("missing auth or content type")
				}
				if r.URL.Path == "/v1/variants/34" {
					b := variantResponse
					switch name {
					case "subscription":
						b = strings.Replace(b, `"is_subscription":false`, `"is_subscription":true`, 1)
					case "pwyw":
						b = strings.Replace(b, `"pay_what_you_want":false`, `"pay_what_you_want":true`, 1)
					case "draft":
						b = strings.Replace(b, "published", "draft", 1)
					case "missing variant flags":
						b = `{"data":{"type":"variants","id":"34","attributes":{"status":"published"}}}`
					}
					return response(b), nil
				}
				if r.Method != "POST" || r.URL.Path != "/v1/checkouts" {
					t.Fatal("wrong checkout endpoint")
				}
				var body struct {
					Data struct {
						Attributes struct {
							CustomPrice int64 `json:"custom_price"`
							TestMode    bool  `json:"test_mode"`
							Preview     bool  `json:"preview"`
							Checkout    struct {
								Email  string            `json:"email"`
								Custom map[string]string `json:"custom"`
							} `json:"checkout_data"`
							Options struct {
								Discount bool `json:"discount"`
							} `json:"checkout_options"`
							Product struct {
								Variants []int  `json:"enabled_variants"`
								Redirect string `json:"redirect_url"`
							} `json:"product_options"`
						} `json:"attributes"`
						Relationships map[string]struct{ Data struct{ ID string } } `json:"relationships"`
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				a := body.Data.Attributes
				if a.CustomPrice != 599 || !a.TestMode || !a.Preview || a.Options.Discount || a.Checkout.Email != "buyer@example.com" || a.Checkout.Custom["order_id"] != strings.Repeat("a", 32) || a.Checkout.Custom["payment_proof"] != strings.Repeat("b", 64) || len(a.Product.Variants) != 1 || a.Product.Variants[0] != 34 || a.Product.Redirect != "https://shop.example/private" || body.Data.Relationships["store"].Data.ID != "12" || body.Data.Relationships["variant"].Data.ID != "34" {
					t.Error("checkout payload mismatch")
				}
				b := checkoutResponse
				switch name {
				case "wrong URL":
					b = strings.Replace(b, "shop.lemonsqueezy.com", "evil.example", 1)
				case "wrong currency":
					b = strings.Replace(b, "USD", "IDR", 1)
				case "wrong price":
					b = strings.Replace(b, "599", "598", 1)
				case "wrong store":
					b = strings.Replace(b, `"store_id":12`, `"store_id":13`, 1)
				case "wrong variant":
					b = strings.Replace(b, `"variant_id":34`, `"variant_id":35`, 1)
				case "wrong mode":
					b = strings.Replace(b, `"test_mode":true`, `"test_mode":false`, 1)
				case "missing mode":
					b = strings.Replace(b, `"test_mode":true,`, "", 1)
				}
				return response(b), nil
			})
			s, err := c.Create(context.Background(), payment.Checkout{ID: strings.Repeat("a", 32), Proof: strings.Repeat("b", 64), VariantID: "34", Currency: "USD", Amount: 599, Email: "buyer@example.com", Name: "Code", FinishURL: "https://shop.example/private"})
			if name == "valid" {
				if err != nil || s.URL == "" || calls != 2 {
					t.Fatal(s, err, calls)
				}
			} else if err == nil {
				t.Fatal("accepted invalid checkout")
			}
		})
	}
}

func TestNotificationAuthenticityAndAuthoritativeOrder(t *testing.T) {
	for _, name := range []string{"valid", "forged", "missing signature", "tampered", "pending", "refunded", "partial refund", "wrong store", "wrong mode", "missing mode", "wrong currency", "discount", "tax inclusive", "wrong quantity", "wrong item order", "multiple items", "wrong price", "API failure", "ignored event"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := client(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer api-secret" {
					t.Error("missing auth")
				}
				if name == "API failure" {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("private provider details"))}, nil
				}
				b := orderResponse
				switch r.URL.Path {
				case "/v1/orders/56":
					switch name {
					case "pending":
						b = strings.Replace(b, `"status":"paid"`, `"status":"pending"`, 1)
					case "refunded":
						b = strings.Replace(b, `"refunded":false`, `"refunded":true`, 1)
					case "partial refund":
						b = strings.Replace(b, `"status":"paid"`, `"status":"partial_refund"`, 1)
					case "wrong store":
						b = strings.Replace(b, `"store_id":12`, `"store_id":13`, 1)
					case "wrong mode":
						b = strings.Replace(b, `"test_mode":true`, `"test_mode":false`, 1)
					case "missing mode":
						b = strings.Replace(b, `"test_mode":true,`, "", 1)
					case "wrong currency":
						b = strings.Replace(b, "USD", "EUR", 1)
					case "discount":
						b = strings.Replace(b, `"discount_total":0`, `"discount_total":1`, 1)
					case "tax inclusive":
						b = strings.Replace(b, `"tax_inclusive":false`, `"tax_inclusive":true`, 1)
					}
				case "/v1/order-items":
					if r.URL.Query().Get("filter[order_id]") != "56" {
						t.Error("wrong item filter")
					}
					b = itemResponse
					switch name {
					case "wrong quantity":
						b = strings.Replace(b, `"quantity":1`, `"quantity":2`, 1)
					case "wrong item order":
						b = strings.Replace(b, `"order_id":56`, `"order_id":57`, 1)
					case "multiple items":
						b = strings.Replace(b, `"total":1`, `"total":2`, 1)
					case "wrong price":
						b = strings.Replace(b, `"price":599`, `"price":598`, 1)
					}
				default:
					t.Fatal("unexpected endpoint")
				}
				return response(b), nil
			})
			body := event()
			if name == "ignored event" {
				body = []byte(strings.Replace(string(body), "order_created", "order_refunded", 1))
			}
			signature := sign(body)
			switch name {
			case "forged":
				signature = strings.Repeat("0", 64)
			case "missing signature":
				signature = ""
			case "tampered":
				body = append(body, ' ')
			}
			s, err := c.Notification(context.Background(), body, signature)
			switch name {
			case "valid":
				if err != nil || !s.Paid || s.Amount != 599 || s.Currency != "USD" || s.VariantID != "34" || s.ProviderID != "56" || s.Proof != strings.Repeat("b", 64) || calls != 2 {
					t.Fatal(s, err, calls)
				}
			case "pending", "refunded", "partial refund":
				if err != nil || s.Paid {
					t.Fatal("unpaid event fulfilled", s, err)
				}
			case "forged", "missing signature", "tampered":
				if !errors.Is(err, payment.ErrSignature) || calls != 0 {
					t.Fatal("unverified API access", err, calls)
				}
			case "ignored event":
				if !errors.Is(err, payment.ErrIgnored) || calls != 0 {
					t.Fatal(err, calls)
				}
			default:
				if err == nil || strings.Contains(err.Error(), "private provider") || strings.Contains(err.Error(), "api-secret") {
					t.Fatal("invalid order accepted", s, err)
				}
			}
		})
	}
}
