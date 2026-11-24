// Package account verifies Catat Saja checkout tokens and delivers prepaid Plus.
package account

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrToken   = errors.New("account: invalid token")
	ErrDeliver = errors.New("account: delivery failed")
)

// Claims are what Catat Saja embeds in a short-lived checkout token.
type Claims struct {
	UID   string `json:"uid"`
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
}

// VerifyToken checks HMAC-SHA256(base64url(payload)) with the shared secret.
func VerifyToken(secret, token string, now time.Time) (Claims, error) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" || secret == "" {
		return Claims{}, ErrToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, given) {
		return Claims{}, ErrToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, ErrToken
	}
	var claims Claims
	if json.Unmarshal(raw, &claims) != nil {
		return Claims{}, ErrToken
	}
	if claims.UID == "" || len(claims.UID) > 128 || !strings.Contains(claims.Email, "@") || len(claims.Email) > 254 || claims.Exp <= now.Unix() {
		return Claims{}, ErrToken
	}
	return claims, nil
}

// Deliverer POSTs a signed body to Catat Saja's deliverPlus function.
type Deliverer struct {
	URL, Secret string
	Client      *http.Client
}

type body struct {
	UID     string `json:"uid"`
	OrderID string `json:"orderId"`
	Months  int    `json:"months"`
	PaidAt  string `json:"paidAt"`
}

func (d *Deliverer) Deliver(ctx context.Context, uid, orderID string, months int, paidAt time.Time) error {
	if d == nil || d.URL == "" || d.Secret == "" || months < 1 {
		return ErrDeliver
	}
	payload, err := json.Marshal(body{UID: uid, OrderID: orderID, Months: months, PaidAt: paidAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(d.Secret))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dahlunas-Signature", signature)
	resp, err := client.Do(req)
	if err != nil {
		return ErrDeliver
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrDeliver
	}
	return nil
}
