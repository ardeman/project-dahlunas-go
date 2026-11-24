package shop

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

type fakeAccounts struct {
	calls int
	err   error
}

func (f *fakeAccounts) Deliver(context.Context, string, string, int, time.Time) error {
	f.calls++
	return f.err
}

func accountToken(secret, uid, email string, exp int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"` + uid + `","email":"` + email + `","exp":` + mustJSON(exp) + `}`))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAccountCheckoutAndDelivery(t *testing.T) {
	g, m := &gateway{}, &mailer{}
	accounts := &fakeAccounts{}
	secret := "checkout-secret"
	product := Product{
		Slug: "catat-saja-1m", NameID: "Plus", NameEN: "Plus",
		DescriptionID: "d", DescriptionEN: "d", Price: 19000,
		Project: 3, Product: 1, Months: 1, Account: true,
		ProjectSlug: "catat-saja", Gateway: payment.Midtrans, Currency: "IDR",
		Delivery: EmailCopy{Subject: Text{"s", "s"}, ThankYou: Text{"t", "t"}, Instructions: Text{"i", "i"}},
	}
	s := &Service{
		Store: &memoryStore{orders: map[string]Order{}}, Gateway: g, Mailer: m,
		Accounts: accounts, CheckoutSecret: secret, Products: []Product{product},
		BaseURL: "https://shop.example",
		SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) {
			return nil, errors.New("no code key")
		},
		Now: func() time.Time { return time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC) },
	}
	token := accountToken(secret, "uid-1", "buyer@example.com", s.Now().Add(5*time.Minute).Unix())
	o, err := s.Create(context.Background(), "catat-saja-1m", "", token)
	if err != nil || o.AccountUID != "uid-1" || o.Email != "buyer@example.com" || o.Code != "" {
		t.Fatal(o, err)
	}
	if _, err := s.Create(context.Background(), "catat-saja-1m", "other@example.com", ""); !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
	if err := s.apply(context.Background(), payment.Midtrans, payment.Status{ID: o.ID, Amount: 19000, Currency: "IDR", Paid: true, State: "settlement"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Get(context.Background(), o.ID)
	if !got.Paid || !got.AccountDone || !got.EmailSent || accounts.calls != 1 || m.calls != 1 || got.Code != "" {
		t.Fatal(got, accounts.calls, m.calls)
	}
}
