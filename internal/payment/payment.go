// Package payment defines the boundary between checkout and payment gateways.
package payment

import (
	"context"
	"errors"
)

const (
	Midtrans     = "midtrans"
	LemonSqueezy = "lemonsqueezy"
)

var ErrSignature = errors.New("payment: invalid notification signature")
var ErrIgnored = errors.New("payment: notification ignored")

type Checkout struct {
	ID, Email, Name, FinishURL string
	Amount                     int64
	Currency, VariantID, Proof string
}

type Session struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

type Status struct {
	ID                                     string
	Amount                                 int64
	State                                  string
	Paid                                   bool
	Currency, VariantID, Proof, ProviderID string
}

// Gateway verifies notification authenticity and fetches authoritative status.
// Browser callbacks never establish payment success.
type Gateway interface {
	Create(context.Context, Checkout) (Session, error)
	Notification(context.Context, []byte, string) (Status, error)
}
