// Package shop owns orders, payment confirmation and unlock-code delivery.
package shop

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/account"
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
	// Months is set for account (prepaid Plus) products.
	Months int
	// Account means delivery writes Plus on Catat Saja, not an unlock code.
	Account bool
	// Soon products are listed but can't be bought.
	Soon                         bool
	Delivery                     EmailCopy
	Gateway, Currency, VariantID string
	// ProjectSlug, ProjectName, Tagline, Icon and the images only present the
	// product in the shop.
	ProjectSlug         string
	ProjectName         Text
	Tagline             Text
	Icon                string
	ProjectImage, Image Text
	// About and Website describe the project on its page; orders never need them.
	About   []Text `json:"-"`
	Website string `json:"-"`
	// Activation lets a code be activated on one device at a time.
	Activation bool
}

type Order struct {
	ID, ViewToken, Email   string
	Product                Product
	Created                time.Time
	SnapToken, Code, State string
	// Lang is the shop language the buyer used, "id" or "en"; orders made before
	// it was recorded leave it empty and get a bilingual email.
	Lang string
	// Method is the payment type chosen at the gateway.
	Method    string
	Paid      bool
	EmailSent bool
	// AccountUID binds a Catat Saja checkout; AccountDone after deliverPlus.
	AccountUID                            string
	AccountDone                           bool
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

// DeliveryPending reports a paid order still waiting on account delivery or email.
func (o Order) DeliveryPending() bool {
	if !o.Paid {
		return false
	}
	if o.Product.Account && !o.AccountDone {
		return true
	}
	return !o.EmailSent
}

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

// AccountDeliverer turns on Catat Saja Plus after payment.
type AccountDeliverer interface {
	Deliver(ctx context.Context, uid, orderID string, months int, paidAt time.Time) error
}

type Service struct {
	Store       Store
	Activations ActivationStore
	Accounts    AccountDeliverer
	// CheckoutSecret verifies Catat Saja account tokens.
	CheckoutSecret string
	Gateway        payment.Gateway
	Gateways       map[string]payment.Gateway
	Mailer         Mailer
	Products       []Product
	BaseURL        string
	SigningKey     func(uint16, uint8) (ed25519.PrivateKey, error)
	Now            func() time.Time
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
	return "Rp " + thousands(p.Price)
}

// thousands groups digits with dots, as Indonesian prices are written.
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

// Group lists one project's products, in catalog order.
type Group struct {
	Slug    string
	Name    Text
	Tagline Text
	Icon    string
	Image   Text
	About   []Text
	Website string
	// Description is set when every product shares it, so it shows once.
	Description Text
	// Amounts is set when the products differ only in price, so the shop
	// offers them as a choice of amount.
	Amounts bool
	// Account is set when the project delivers Plus to a Catat Saja account.
	Account bool
	// Soon is set when the project is listed but not sold yet.
	Soon     bool
	Products []Product
}

// GroupSlug is the slug of the product's project page.
func (o Order) GroupSlug() string {
	return Groups([]Product{o.Product})[0].Slug
}

// From is the lowest price, for "from Rp 5.000" on the project's card.
func (g Group) From() Product {
	low := g.Products[0]
	for _, p := range g.Products[1:] {
		if p.CurrencyCode() == low.CurrencyCode() && p.Price < low.Price {
			low = p
		}
	}
	return low
}

// Groups splits products by project, keeping catalog order.
func Groups(products []Product) []Group {
	var groups []Group
	index := map[uint16]int{}
	for _, p := range products {
		i, ok := index[p.Project]
		if !ok {
			i = len(groups)
			index[p.Project] = i
			slug := p.ProjectSlug
			if slug == "" {
				slug = strconv.Itoa(int(p.Project))
			}
			groups = append(groups, Group{Slug: slug, Name: p.ProjectName, Tagline: p.Tagline, Icon: p.Icon, Image: p.ProjectImage, About: p.About, Website: p.Website, Description: Text{p.DescriptionID, p.DescriptionEN}, Amounts: true, Account: p.Account, Soon: p.Soon})
		}
		g := &groups[i]
		if g.Account != p.Account {
			g.Account = false
		}
		if g.Description != (Text{p.DescriptionID, p.DescriptionEN}) {
			g.Description = Text{}
		}
		if len(g.Products) > 0 {
			first := g.Products[0]
			if first.NameID != p.NameID || first.NameEN != p.NameEN || first.GatewayName() != p.GatewayName() || first.CurrencyCode() != p.CurrencyCode() || p.Image != (Text{}) {
				g.Amounts = false
			}
		} else if p.Image != (Text{}) {
			g.Amounts = false
		}
		g.Products = append(g.Products, p)
	}
	for i := range groups {
		if groups[i].Description == (Text{}) {
			groups[i].Amounts = false
		}
	}
	return groups
}

// Group finds a project's products by its slug.
func (s *Service) Group(slug string) (Group, bool) {
	for _, g := range s.Groups() {
		if g.Slug == slug {
			return g, true
		}
	}
	return Group{}, false
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

// sessionWait is how long an order may take to get its payment session before
// a resubmitted form gives up on it; it exceeds the gateway's request timeout.
const sessionWait = 20 * time.Second

// submittedID derives the order ID for one attempt of one submitted form, so
// sending the form again finds the same order. The key must be a secret only
// the buyer's browser holds.
func submittedID(key, slug, email, accountToken string, attempt int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"dahlunas-order", key, slug, email, accountToken, strconv.Itoa(attempt)}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// submitted reports the order a form already created and, when it has none that
// can still be used, the ID for its next order.
func (s *Service) submitted(ctx context.Context, key, slug, email, accountToken string) (Order, string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id := submittedID(key, slug, email, accountToken, attempt)
		for wait := 0; ; wait++ {
			o, err := s.Store.Get(ctx, id)
			if errors.Is(err, ErrNotFound) {
				return Order{}, id, nil
			}
			if err != nil {
				return Order{}, "", err
			}
			open := o.State == "pending" || o.State == "capture"
			if o.Paid || (open && (o.SnapToken != "" || o.CheckoutURL != "")) {
				return o, "", nil
			}
			if !open || s.now().Sub(o.Created) >= sessionWait {
				break
			}
			// The first submission is still opening its payment session.
			if wait >= 8 {
				return Order{}, "", ErrRetry
			}
			select {
			case <-ctx.Done():
				return Order{}, "", ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return Order{}, "", ErrRetry
}

// Submitted returns the usable order this form already created, or ErrNotFound.
func (s *Service) Submitted(ctx context.Context, key, slug, email, accountToken string) (Order, error) {
	o, next, err := s.submitted(ctx, key, slug, email, accountToken)
	if err == nil && next != "" {
		return Order{}, ErrNotFound
	}
	return o, err
}

// CreateOnce creates at most one usable order for a submitted form.
func (s *Service) CreateOnce(ctx context.Context, key, slug, email, accountToken, lang string) (Order, error) {
	if key == "" {
		return Order{}, ErrInput
	}
	return s.once(ctx, key, slug, email, accountToken, func(id string) (Order, error) {
		return s.create(ctx, id, slug, email, accountToken, lang)
	})
}

// once returns the usable order for these inputs, making it if there is none.
func (s *Service) once(ctx context.Context, key, slug, email, accountToken string, create func(id string) (Order, error)) (Order, error) {
	o, id, err := s.submitted(ctx, key, slug, email, accountToken)
	if err != nil || id == "" {
		return o, err
	}
	o, err = create(id)
	if errors.Is(err, errTaken) {
		// Another submission of the same form won the race.
		o, id, err = s.submitted(ctx, key, slug, email, accountToken)
		if err == nil && id != "" {
			err = ErrRetry
		}
	}
	return o, err
}

var errTaken = errors.New("shop: order already exists")

// Create always creates a new order.
func (s *Service) Create(ctx context.Context, slug, email, accountToken string) (Order, error) {
	id, err := randomHex(16)
	if err != nil {
		return Order{}, err
	}
	return s.create(ctx, id, slug, email, accountToken, "")
}

func (s *Service) create(ctx context.Context, id, slug, email, accountToken, lang string) (Order, error) {
	var product Product
	for _, p := range s.Products {
		if p.Slug == slug && !p.Soon {
			product = p
			break
		}
	}
	gateway := s.gateway(product.GatewayName())
	if product.Price <= 0 || gateway == nil {
		return Order{}, ErrInput
	}
	accountUID := ""
	if product.Account {
		if product.Months < 1 || s.Accounts == nil || s.CheckoutSecret == "" {
			return Order{}, errors.New("shop: account delivery unavailable")
		}
		claims, err := account.VerifyToken(s.CheckoutSecret, accountToken, s.now())
		if err != nil {
			return Order{}, ErrInput
		}
		email = claims.Email
		accountUID = claims.UID
	} else {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
			return Order{}, ErrInput
		}
		// Validate the key before accepting a payment.
		if _, err := s.SigningKey(product.Project, product.KeyID); err != nil {
			return Order{}, errors.New("shop: signing unavailable")
		}
	}
	return s.open(ctx, id, product, email, accountUID, lang)
}

// open stores a validated order and starts its payment session.
func (s *Service) open(ctx context.Context, id string, product Product, email, accountUID, lang string) (Order, error) {
	gateway := s.gateway(product.GatewayName())
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
	o := Order{PaymentProof: proof, ID: id, ViewToken: token, Email: email, Lang: lang, AccountUID: accountUID, Product: product, Created: s.now(), State: "pending"}
	if err := s.Store.Create(ctx, o); errors.Is(err, ErrConflict) {
		return Order{}, errTaken
	} else if err != nil {
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

// ended reports a payment that can no longer be completed.
func (o Order) ended() bool {
	return !o.Paid && o.State != "pending" && o.State != "capture"
}

// CanChangePayment reports whether the buyer can swap the chosen method. Cards
// are excluded: cancelling could void a charge that is being captured.
func (o Order) CanChangePayment() bool {
	return !o.Paid && o.State == "pending" && o.Product.GatewayName() == payment.Midtrans && o.Method != "" && o.Method != "credit_card"
}

// ChangePayment cancels a pending payment and opens a new order for the same
// purchase. It returns the order itself when that turns out to be paid. Only
// one usable replacement exists per order, however often this is called.
func (s *Service) ChangePayment(ctx context.Context, id, token, lang string) (Order, error) {
	o, err := s.View(ctx, id, token)
	if err != nil || o.Paid {
		return o, err
	}
	name := o.Product.GatewayName()
	gateway := s.gateway(name)
	canceler, _ := gateway.(payment.Canceler)
	checker, _ := gateway.(payment.Checker)
	if o.CanChangePayment() && canceler != nil && checker != nil {
		status, err := checker.Check(ctx, id)
		if err != nil {
			return Order{}, err
		}
		if status.State == "pending" && status.Method != "credit_card" {
			// The check below decides, since a payment can win this race.
			_ = canceler.Cancel(ctx, id)
			if status, err = checker.Check(ctx, id); err != nil {
				return Order{}, err
			}
		}
		applied := s.apply(ctx, name, status)
		if o, err = s.View(ctx, id, token); err != nil || o.Paid {
			return o, err
		}
		if applied != nil {
			return Order{}, applied
		}
	}
	// Only a payment known to be over may be replaced.
	if name != payment.Midtrans || !o.ended() {
		return Order{}, ErrInput
	}
	var product Product
	for _, p := range s.Products {
		if p.Slug == o.Product.Slug && !p.Soon {
			product = p
			break
		}
	}
	if product.Price <= 0 || product.Account != o.Product.Account || product.GatewayName() != name {
		return Order{}, ErrInput
	}
	if product.Account {
		// The bought account was proven when the first order was made.
		if product.Months < 1 || s.Accounts == nil || o.AccountUID == "" {
			return Order{}, errors.New("shop: account delivery unavailable")
		}
	} else if _, err := s.SigningKey(product.Project, product.KeyID); err != nil {
		return Order{}, errors.New("shop: signing unavailable")
	}
	return s.once(ctx, o.ViewToken, "change", o.ID, "", func(next string) (Order, error) {
		return s.open(ctx, next, product, o.Email, o.AccountUID, lang)
	})
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
		if status.State == "pending" && o.ended() && o.State != "abandoned" && o.State != "" {
			// A delayed pending status must not reopen an ended payment.
			return nil
		}
		o.State = status.State
		if status.Method != "" {
			o.Method = status.Method
		}
		if status.Paid {
			if !o.Product.Account {
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
			} else if o.AccountUID == "" || o.Product.Months < 1 {
				return errors.New("shop: account order incomplete")
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
// Account products call Catat Saja before the receipt email.
func (s *Service) Deliver(ctx context.Context, id string) error {
	for i := 0; i < 8; i++ {
		o, err := s.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if !o.Paid {
			return nil
		}
		if o.EmailSent && (!o.Product.Account || o.AccountDone) {
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
		if o.Product.Account && !o.AccountDone {
			if s.Accounts == nil {
				return ErrRetry
			}
			if err := s.Accounts.Deliver(ctx, o.AccountUID, o.ID, o.Product.Months, o.Created); err != nil {
				return ErrRetry
			}
			for j := 0; j < 8; j++ {
				current, err := s.Store.Get(ctx, id)
				if err != nil {
					return err
				}
				current.AccountDone = true
				err = s.Store.Save(ctx, current)
				if errors.Is(err, ErrConflict) {
					continue
				}
				if err != nil {
					return err
				}
				o = current
				break
			}
		}
		if o.EmailSent {
			return nil
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
			if o.Product.Account {
				current.AccountDone = true
			}
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

// Groups lists the shop's products by project.
func (s *Service) Groups() []Group { return Groups(s.Products) }
