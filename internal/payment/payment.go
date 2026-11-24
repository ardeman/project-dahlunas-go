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
var ErrNoTransaction = errors.New("payment: no transaction yet")

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
	ID     string
	Amount int64
	State  string
	Paid   bool
	// Method is the chosen payment type, empty when the gateway has none.
	Method                                 string
	Currency, VariantID, Proof, ProviderID string
}

// Gateway verifies notification authenticity and fetches authoritative status.
// Browser callbacks never establish payment success.
type Gateway interface {
	Create(context.Context, Checkout) (Session, error)
	Notification(context.Context, []byte, string) (Status, error)
}

// Canceler cancels a payment that is still pending. Callers must check the
// authoritative status afterwards rather than trust its result alone.
type Canceler interface {
	Cancel(context.Context, string) error
}

// Checker fetches an order's authoritative status without a notification, so
// a lost notification does not leave a paid order unfulfilled. It returns
// ErrNoTransaction while the buyer has not started paying.
type Checker interface {
	Check(context.Context, string) (Status, error)
}
