// Package payment defines the boundary between checkout and payment gateways.
package payment

import "context"

type Checkout struct {
	ID, Email, Name, FinishURL string
	Amount                     int64
}

type Session struct {
	Token string `json:"token"`
}

type Status struct {
	ID     string
	Amount int64
	State  string
	Paid   bool
}

// Gateway verifies notification authenticity and fetches authoritative status.
// Browser callbacks never establish payment success.
type Gateway interface {
	Create(context.Context, Checkout) (Session, error)
	Notification(context.Context, []byte) (Status, error)
}
