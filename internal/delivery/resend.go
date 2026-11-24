// Package delivery sends purchase emails in the buyer's language through Resend.
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

type Resend struct {
	key, from, support, endpoint string
	client                       *http.Client
}

func New(key, from, support string) *Resend {
	return &Resend{key, from, support, "https://api.resend.com/emails", &http.Client{Timeout: 15 * time.Second}}
}

func (r *Resend) Send(ctx context.Context, o shop.Order) error {
	copy := o.Product.DeliveryCopy()
	// A retry must send the same body, so the language is fixed on the order.
	pick := func(t shop.Text, separator string) string {
		switch o.Lang {
		case "id":
			return t.ID
		case "en":
			return t.EN
		}
		return t.ID + separator + t.EN
	}
	text := pick(copy.ThankYou, " / ") + "\n\n"
	if !o.Product.Account {
		text += o.Code + "\n\n"
	}
	text += pick(copy.Instructions, "\n") + "\n\n" +
		pick(shop.Text{ID: "Pesanan", EN: "Order"}, " / ") + ": " + o.ID + "\n" +
		pick(shop.Text{ID: "Bantuan", EN: "Support"}, " / ") + ": " + r.support
	body := map[string]any{
		"from": r.from, "to": []string{o.Email}, "reply_to": r.support,
		"subject": pick(copy.Subject, " / "),
		"text":    text,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+r.key)
	req.Header.Set("Content-Type", "application/json")
	key := "unlock/" + o.ID
	if o.Product.Account {
		key = "plus/" + o.ID
	}
	req.Header.Set("Idempotency-Key", key)
	resp, err := r.client.Do(req)
	if err != nil {
		return errors.New("delivery: unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("delivery: rejected")
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result) != nil || result.ID == "" {
		return errors.New("delivery: invalid response")
	}
	return nil
}
