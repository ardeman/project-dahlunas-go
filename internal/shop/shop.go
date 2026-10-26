// Package shop owns orders, payment confirmation and unlock-code delivery.
package shop

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

var (
	ErrNotFound = errors.New("shop: order not found")
	ErrConflict = errors.New("shop: concurrent update")
	ErrInput    = errors.New("shop: invalid input")
	ErrRetry    = errors.New("shop: delivery pending")
)

type Product struct {
	Slug, NameID, NameEN, DescriptionID, DescriptionEN string
	Price                                              int64
	Project, Product                                   uint16
	KeyID                                              uint8
	Delivery                                           EmailCopy
	Gateway, Currency, VariantID                       string
}

type Order struct {
	ID, ViewToken, Email                  string
	Product                               Product
	Created                               time.Time
	SnapToken, Code, State                string
	Paid                                  bool
	EmailSent                             bool
	CheckoutURL, PaymentProof, ProviderID string
	EmailFirstAttempt, EmailLease         time.Time
	// Revision is the store's compare-and-swap token, never part of the record.
	Revision string `json:"-"`
}

// Store persists an order atomically. Save must compare Revision, returning
// ErrConflict on stale writes; Create must never overwrite an existing order.
// Undelivered lists orders whose stored DeliveryPending was true when written,
// and Unpaid those whose stored AwaitingPayment was true.
type Store interface {
	Create(context.Context, Order) error
	Get(context.Context, string) (Order, error)
	Save(context.Context, Order) error
	Undelivered(context.Context) ([]Order, error)
	Unpaid(context.Context) ([]Order, error)
}

// DeliveryPending reports a paid order whose email is not confirmed sent.
func (o Order) DeliveryPending() bool { return o.Paid && !o.EmailSent }

// AwaitingPayment reports an order with no final payment state yet.
func (o Order) AwaitingPayment() bool { return !o.Paid && o.State == "pending" }

// deliveryWindow ends automatic sends before Resend's 24-hour deduplication.
const deliveryWindow = 23 * time.Hour

// checkAfter gives the gateway's notification time to arrive first. After
// paymentWindow, longer than Snap's 24-hour token, an order is abandoned; a
// late notification can still fulfil it.
const (
	checkAfter    = 10 * time.Minute
	paymentWindow = 48 * time.Hour
)

type Mailer interface {
	Send(context.Context, Order) error
}

type Service struct {
	Store      Store
	Gateway    payment.Gateway
	Gateways   map[string]payment.Gateway
	Mailer     Mailer
	Products   []Product
	BaseURL    string
	SigningKey func(uint16, uint8) (ed25519.PrivateKey, error)
	Now        func() time.Time
}

// GatewayName and CurrencyCode preserve orders from the original IDR checkout.
func (p Product) GatewayName() string {
	if p.Gateway == "" {
		return payment.Midtrans
	}
	return p.Gateway
}
func (p Product) CurrencyCode() string {
	if p.Currency == "" {
		return "IDR"
	}
	return p.Currency
}
func (p Product) FormattedPrice() string {
	if p.CurrencyCode() == "USD" {
		return fmt.Sprintf("US$ %d.%02d", p.Price/100, p.Price%100)
	}
	return fmt.Sprintf("Rp %d", p.Price)
}
func (s *Service) gateway(name string) payment.Gateway {
	if g := s.Gateways[name]; g != nil {
		return g
	}
	if name == payment.Midtrans {
		return s.Gateway
	}
	return nil
}
func (s *Service) HasGateway(name string) bool { return s.gateway(name) != nil }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) Create(ctx context.Context, slug, email string) (Order, error) {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return Order{}, ErrInput
	}
	var product Product
	for _, p := range s.Products {
		if p.Slug == slug {
			product = p
			break
		}
	}
	gateway := s.gateway(product.GatewayName())
	if product.Price <= 0 || gateway == nil {
		return Order{}, ErrInput
	}
	// Validate the key before accepting a payment.
	if _, err := s.SigningKey(product.Project, product.KeyID); err != nil {
		return Order{}, errors.New("shop: signing unavailable")
	}
	id, err := randomHex(16)
	if err != nil {
		return Order{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return Order{}, err
	}
	proof := ""
	if product.GatewayName() == payment.LemonSqueezy {
		proof, err = randomHex(32)
		if err != nil {
			return Order{}, err
		}
	}
	o := Order{PaymentProof: proof, ID: id, ViewToken: token, Email: email, Product: product, Created: s.now(), State: "pending"}
	if err := s.Store.Create(ctx, o); err != nil {
		return Order{}, err
	}
	session, err := gateway.Create(ctx, payment.Checkout{Currency: product.CurrencyCode(), VariantID: product.VariantID, Proof: proof, ID: id, Email: email, Name: product.NameEN, Amount: product.Price, FinishURL: s.BaseURL + o.Path()})
	if err != nil {
		return Order{}, err
	}
	// A notification may race the Snap response. Preserve payment/delivery fields.
	for i := 0; i < 8; i++ {
		current, err := s.Store.Get(ctx, id)
		if err != nil {
			return Order{}, err
		}
		current.SnapToken = session.Token
		current.CheckoutURL = session.URL
		err = s.Store.Save(ctx, current)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return current, err
	}
	return Order{}, ErrConflict
}

func (o Order) Path() string { return "/orders/" + o.ID + "/" + o.ViewToken }

func (s *Service) View(ctx context.Context, id, token string) (Order, error) {
	if len(id) != 32 || len(token) != 64 {
		return Order{}, ErrNotFound
	}
	o, err := s.Store.Get(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(o.ViewToken)) != 1 {
		return Order{}, ErrNotFound
	}
	return o, nil
}

func (s *Service) Notify(ctx context.Context, data []byte) error {
	return s.NotifyGateway(ctx, payment.Midtrans, data, "")
}

func (s *Service) NotifyGateway(ctx context.Context, name string, data []byte, signature string) error {
	gateway := s.gateway(name)
	if gateway == nil {
		return ErrNotFound
	}
	status, err := gateway.Notification(ctx, data, signature)
	if err != nil {
		return err
	}
	return s.apply(ctx, name, status)
}

// apply records an authoritative gateway status and fulfils a paid order.
func (s *Service) apply(ctx context.Context, name string, status payment.Status) error {
	for i := 0; i < 8; i++ {
		o, err := s.Store.Get(ctx, status.ID)
		if err != nil {
			return err
		}
		currency := status.Currency
		if currency == "" && name == payment.Midtrans {
			currency = "IDR"
		}
		if name != o.Product.GatewayName() || currency != o.Product.CurrencyCode() || status.Amount != o.Product.Price {
			return errors.New("shop: payment amount mismatch")
		}
		if name == payment.LemonSqueezy {
			if len(o.PaymentProof) != 64 || subtle.ConstantTimeCompare([]byte(status.Proof), []byte(o.PaymentProof)) != 1 || status.VariantID != o.Product.VariantID || status.ProviderID == "" || (o.ProviderID != "" && o.ProviderID != status.ProviderID) {
				return errors.New("shop: payment order mismatch")
			}
			o.ProviderID = status.ProviderID
		}
		if o.Paid {
			return s.Deliver(ctx, o.ID)
		}
		o.State = status.State
		if status.Paid {
			key, err := s.SigningKey(o.Product.Project, o.Product.KeyID)
			if err != nil {
				return errors.New("shop: signing unavailable")
			}
			// The code ID and issue date are fixed at order creation. Even racing
			// notifications generate exactly the same signed code.
			id, err := hex.DecodeString(o.ID)
			if err != nil || len(id) != 16 {
				return errors.New("shop: invalid stored order")
			}
			day, err := unlock.IssuedDay(o.Created)
			if err != nil {
				return err
			}
			code := unlock.Code{Version: unlock.Version, KeyID: o.Product.KeyID, Project: o.Product.Project, Product: o.Product.Product, IssuedDay: day}
			copy(code.ID[:], id)
			o.Code, err = unlock.Sign(key, code)
			if err != nil {
				return err
			}
			o.Paid = true
		}
		err = s.Store.Save(ctx, o)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if o.Paid {
			return s.Deliver(ctx, o.ID)
		}
		return nil
	}
	return ErrConflict
}

// Deliver uses a durable lease and a stable provider idempotency key. Retries
// stop before the provider's 24-hour deduplication expires; ambiguous late
// deliveries require operator review instead of risking a duplicate email.
func (s *Service) Deliver(ctx context.Context, id string) error {
	for i := 0; i < 8; i++ {
		o, err := s.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if !o.Paid || o.EmailSent {
			return nil
		}
		now := s.now()
		if o.EmailLease.After(now) || (!o.EmailFirstAttempt.IsZero() && now.Sub(o.EmailFirstAttempt) >= deliveryWindow) {
			return ErrRetry
		}
		if o.EmailFirstAttempt.IsZero() {
			o.EmailFirstAttempt = now
		}
		o.EmailLease = now.Add(time.Minute)
		if err := s.Store.Save(ctx, o); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			return err
		}
		if err := s.Mailer.Send(ctx, o); err != nil {
			return ErrRetry
		}
		// Preserve concurrent writes after the external side effect.
		for j := 0; j < 8; j++ {
			current, err := s.Store.Get(ctx, id)
			if err != nil {
				return err
			}
			current.EmailSent = true
			current.EmailLease = time.Time{}
			err = s.Store.Save(ctx, current)
			if errors.Is(err, ErrConflict) {
				continue
			}
			return err
		}
		return ErrConflict
	}
	return ErrConflict
}

type SweepReport struct {
	Delivered, Pending int
	// Review lists order IDs whose automatic delivery window has closed.
	Review []string
	// Confirmed lists orders found paid without a notification; PaymentReview
	// lists orders whose gateway status failed verification.
	Confirmed, PaymentReview []string
}

// Sweep retries delivery for paid orders that are still waiting for email, then
// asks gateways about unpaid orders, so neither depends on gateways repeating
// their notifications.
func (s *Service) Sweep(ctx context.Context) (SweepReport, error) {
	report, err := s.sweepDeliveries(ctx)
	if err != nil {
		return report, err
	}
	return report, s.sweepPayments(ctx, &report)
}

func (s *Service) sweepDeliveries(ctx context.Context) (SweepReport, error) {
	var report SweepReport
	orders, err := s.Store.Undelivered(ctx)
	if err != nil {
		return report, err
	}
	for _, o := range orders {
		if !o.DeliveryPending() {
			// Saving refreshes a stale index, such as after a manual review.
			if err := s.Store.Save(ctx, o); err != nil && !errors.Is(err, ErrConflict) {
				return report, err
			}
			continue
		}
		err := s.Deliver(ctx, o.ID)
		switch {
		case err == nil:
			report.Delivered++
		case errors.Is(err, ErrRetry) && !o.EmailFirstAttempt.IsZero() && s.now().Sub(o.EmailFirstAttempt) >= deliveryWindow:
			report.Review = append(report.Review, o.ID)
		default:
			report.Pending++
		}
	}
	return report, nil
}

func (s *Service) sweepPayments(ctx context.Context, report *SweepReport) error {
	orders, err := s.Store.Unpaid(ctx)
	if err != nil {
		return err
	}
	for _, o := range orders {
		if !o.AwaitingPayment() {
			if err := s.Store.Save(ctx, o); err != nil && !errors.Is(err, ErrConflict) {
				return err
			}
			continue
		}
		age := s.now().Sub(o.Created)
		if age < checkAfter {
			continue
		}
		name := o.Product.GatewayName()
		checker, _ := s.gateway(name).(payment.Checker)
		if checker == nil {
			// Without a status lookup, only the gateway's notification can confirm.
			if age >= paymentWindow {
				if err := s.abandon(ctx, o.ID); err != nil {
					return err
				}
			}
			continue
		}
		status, err := checker.Check(ctx, o.ID)
		switch {
		case errors.Is(err, payment.ErrNoTransaction):
			if age >= paymentWindow {
				if err := s.abandon(ctx, o.ID); err != nil {
					return err
				}
			}
			continue
		case err != nil:
			// The gateway is unreachable; the next sweep asks again.
			continue
		}
		if err := s.apply(ctx, name, status); err != nil && !errors.Is(err, ErrRetry) {
			report.PaymentReview = append(report.PaymentReview, o.ID)
			continue
		}
		if status.Paid {
			report.Confirmed = append(report.Confirmed, o.ID)
		} else if status.State == "pending" && age >= paymentWindow {
			if err := s.abandon(ctx, o.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// abandon stops checking an order the buyer never paid.
func (s *Service) abandon(ctx context.Context, id string) error {
	for i := 0; i < 8; i++ {
		o, err := s.Store.Get(ctx, id)
		if err != nil || !o.AwaitingPayment() {
			return err
		}
		o.State = "abandoned"
		err = s.Store.Save(ctx, o)
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return ErrConflict
}
