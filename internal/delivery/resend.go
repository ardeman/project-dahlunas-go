// Package delivery sends bilingual unlock-code emails through Resend.
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
	body := map[string]any{
		"from": r.from, "to": []string{o.Email}, "reply_to": r.support,
		"subject": copy.Subject.ID + " / " + copy.Subject.EN,
		"text":    copy.ThankYou.ID + " / " + copy.ThankYou.EN + "\n\n" + o.Code + "\n\n" + copy.Instructions.ID + "\n" + copy.Instructions.EN + "\n\nPesanan / Order: " + o.ID + "\nBantuan / Support: " + r.support,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+r.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "unlock/"+o.ID)
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
