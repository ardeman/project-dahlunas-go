package shop

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

type memoryStore struct {
	mu      sync.Mutex
	orders  map[string]Order
	version int
}

func (m *memoryStore) Create(_ context.Context, o Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.orders[o.ID]; ok {
		return ErrConflict
	}
	m.version++
	o.Revision = strconv.Itoa(m.version)
	m.orders[o.ID] = o
	return nil
}
func (m *memoryStore) Get(_ context.Context, id string) (Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	return o, nil
}
func (m *memoryStore) Save(_ context.Context, o Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.orders[o.ID]
	if !ok || old.Revision != o.Revision {
		return ErrConflict
	}
	m.version++
	o.Revision = strconv.Itoa(m.version)
	m.orders[o.ID] = o
	return nil
}
func (m *memoryStore) Undelivered(_ context.Context) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []Order
	for _, o := range m.orders {
		if o.DeliveryPending() {
			result = append(result, o)
		}
	}
	return result, nil
}
func (m *memoryStore) Unpaid(_ context.Context) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []Order
	for _, o := range m.orders {
		if o.AwaitingPayment() {
			result = append(result, o)
		}
	}
	return result, nil
}

type gateway struct {
	status   payment.Status
	checkout payment.Checkout
	err      error
}

func (g *gateway) Create(_ context.Context, c payment.Checkout) (payment.Session, error) {
	g.checkout = c
	return payment.Session{Token: "token"}, g.err
}
func (g *gateway) Notification(context.Context, []byte, string) (payment.Status, error) {
	return g.status, g.err
}

type mailer struct {
	mu    sync.Mutex
	calls int
	fail  bool
	code  string
}

func (m *mailer) Send(_ context.Context, o Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.code = o.Code
	if m.fail {
		return errors.New("mail failed")
	}
	return nil
}

func setup(t *testing.T) (*Service, *gateway, *mailer, Order) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	g, m := &gateway{}, &mailer{}
	s := &Service{Store: &memoryStore{orders: map[string]Order{}}, Gateway: g, Mailer: m, Products: CobaLagi(), BaseURL: "https://shop.example", SigningKey: func(uint16, uint8) (ed25519.PrivateKey, error) { return key, nil }, Now: func() time.Time { return time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC) }}
	o, err := s.Create(context.Background(), "coba-lagi-1", "buyer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	g.status = payment.Status{ID: o.ID, Amount: 5000, State: "settlement", Paid: true}
	return s, g, m, o
}

func TestCheckoutUsesCatalogAndPrivateLink(t *testing.T) {
	s, g, _, o := setup(t)
	if g.checkout.Amount != 5000 || g.checkout.FinishURL != s.BaseURL+o.Path() || len(o.ViewToken) != 64 {
		t.Fatal("invalid checkout")
	}
	if _, err := s.View(context.Background(), o.ID, "wrong"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.View(context.Background(), o.ID, o.ViewToken); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ slug, email string }{{"missing", "buyer@example.com"}, {"coba-lagi-1", "bad"}, {"coba-lagi-1", "Buyer <buyer@example.com>"}, {"coba-lagi-1", "a@example.com\r\nBcc:other@example.com"}} {
		if _, err := s.Create(context.Background(), input.slug, input.email); !errors.Is(err, ErrInput) {
			t.Fatalf("input=%+v error=%v", input, err)
		}
	}
}

func TestConcurrentNotificationsMakeOneCodeAndEmail(t *testing.T) {
	s, _, m, o := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Notify(context.Background(), nil)
			if err != nil && !errors.Is(err, ErrRetry) && !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := s.Notify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.Store.Get(context.Background(), o.ID)
	if !saved.Paid || !saved.EmailSent || m.calls != 1 || saved.Code != m.code {
		t.Fatalf("paid=%v sent=%v calls=%d", saved.Paid, saved.EmailSent, m.calls)
	}
	decoded, err := unlock.Parse(saved.Code)
	if err != nil || decoded.Project != 1 || decoded.Product != 1 {
		t.Fatalf("code=%+v err=%v", decoded, err)
	}
	key, _ := s.SigningKey(1, 1)
	canonical, err := unlock.Sign(key, decoded)
	if err != nil || canonical != saved.Code {
		t.Fatal("code signature changed")
	}
}

func TestPendingMismatchAndGatewayFailureNeverFulfill(t *testing.T) {
	for _, mode := range []string{"pending", "mismatch", "provider"} {
		t.Run(mode, func(t *testing.T) {
			s, g, m, o := setup(t)
			switch mode {
			case "pending":
				g.status.Paid = false
				g.status.State = "pending"
			case "mismatch":
				g.status.Amount = 1
			case "provider":
				g.err = errors.New("invalid signature")
			}
			err := s.Notify(context.Background(), nil)
			if mode != "pending" && err == nil {
				t.Fatal("expected error")
			}
			saved, _ := s.Store.Get(context.Background(), o.ID)
			if saved.Paid || saved.Code != "" || m.calls != 0 {
				t.Fatal("unpaid order fulfilled")
			}
		})
	}
}

func TestEmailFailureKeepsCodeAndRetriesWithinWindow(t *testing.T) {
	s, g, m, o := setup(t)
	m.fail = true
	if err := s.Notify(context.Background(), nil); !errors.Is(err, ErrRetry) {
		t.Fatal(err)
	}
	saved, _ := s.Store.Get(context.Background(), o.ID)
	code := saved.Code
	if !saved.Paid || code == "" || saved.EmailSent {
		t.Fatal("code was lost on email failure")
	}
	if err := s.Notify(context.Background(), nil); !errors.Is(err, ErrRetry) {
		t.Fatal(err)
	}
	if m.calls != 1 {
		t.Fatal("lease did not prevent duplicate sends")
	}
	now := s.now()
	s.Now = func() time.Time { return now.Add(2 * time.Minute) }
	m.fail = false
	if err := s.Notify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	saved, _ = s.Store.Get(context.Background(), o.ID)
	if saved.Code != code || !saved.EmailSent || m.calls != 2 {
		t.Fatal("retry changed code")
	}
	// Later stale notifications must not overwrite a fulfilled order.
	g.status.Paid = false
	g.status.State = "pending"
	if err := s.Notify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	saved, _ = s.Store.Get(context.Background(), o.ID)
	if !saved.Paid || saved.Code != code || m.calls != 2 {
		t.Fatal("stale status undid fulfillment")
	}
}

func TestEmailDoesNotRetryBeyondDedupeWindow(t *testing.T) {
	s, _, m, _ := setup(t)
	m.fail = true
	s.Notify(context.Background(), nil)
	now := s.now()
	s.Now = func() time.Time { return now.Add(23 * time.Hour) }
	if err := s.Notify(context.Background(), nil); !errors.Is(err, ErrRetry) {
		t.Fatal(err)
	}
	if m.calls != 1 {
		t.Fatal("late retry risked duplicate email")
	}
}

func TestMissingSigningKeyPreventsCheckout(t *testing.T) {
	s, g, _, _ := setup(t)
	g.checkout = payment.Checkout{}
	s.SigningKey = func(uint16, uint8) (ed25519.PrivateKey, error) { return nil, errors.New("key unavailable") }
	if _, err := s.Create(context.Background(), "coba-lagi-1", "buyer@example.com"); err == nil {
		t.Fatal("missing key accepted")
	}
	if g.checkout.ID != "" {
		t.Fatal("provider called without signing key")
	}
}

func TestLemonSqueezyOrderBinding(t *testing.T) {
	for _, name := range []string{"paid", "wrong gateway", "wrong currency", "wrong amount", "wrong proof", "wrong variant", "different provider order"} {
		t.Run(name, func(t *testing.T) {
			s, midtrans, m, _ := setup(t)
			lemon := &gateway{}
			s.Gateways = map[string]payment.Gateway{payment.LemonSqueezy: lemon}
			product := CobaLagi()[0]
			product.Slug = "international"
			product.Gateway = payment.LemonSqueezy
			product.Currency = "USD"
			product.Price = 599
			product.VariantID = "34"
			s.Products = append(s.Products, product)
			o, err := s.Create(context.Background(), product.Slug, "buyer@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if len(o.PaymentProof) != 64 || lemon.checkout.Currency != "USD" || lemon.checkout.Amount != 599 || lemon.checkout.Proof != o.PaymentProof || lemon.checkout.VariantID != "34" {
				t.Fatal("checkout not bound")
			}
			status := payment.Status{ID: o.ID, Amount: 599, Currency: "USD", Proof: o.PaymentProof, VariantID: "34", ProviderID: "56", State: "paid", Paid: true}
			switch name {
			case "wrong currency":
				status.Currency = "IDR"
			case "wrong amount":
				status.Amount = 598
			case "wrong proof":
				status.Proof = strings.Repeat("0", 64)
			case "wrong variant":
				status.VariantID = "35"
			case "different provider order":
				saved, _ := s.Store.Get(context.Background(), o.ID)
				saved.ProviderID = "57"
				if err := s.Store.Save(context.Background(), saved); err != nil {
					t.Fatal(err)
				}
			}
			lemon.status = status
			if name == "wrong gateway" {
				midtrans.status = status
				err = s.Notify(context.Background(), nil)
			} else {
				err = s.NotifyGateway(context.Background(), payment.LemonSqueezy, nil, "signature")
			}
			saved, _ := s.Store.Get(context.Background(), o.ID)
			if name != "paid" {
				if err == nil || saved.Paid || saved.Code != "" || m.calls != 0 {
					t.Fatal("invalid payment fulfilled", err)
				}
				return
			}
			if err != nil || !saved.Paid || saved.ProviderID != "56" || m.calls != 1 {
				t.Fatal("valid payment not fulfilled", err)
			}
			original := saved.Code
			// Currency and variant remain tied to the order snapshot after catalog edits.
			s.Products = CobaLagi()
			for i := 0; i < 2; i++ {
				if err := s.NotifyGateway(context.Background(), payment.LemonSqueezy, nil, "signature"); err != nil {
					t.Fatal(err)
				}
			}
			saved, _ = s.Store.Get(context.Background(), o.ID)
			if m.calls != 1 || saved.Code != original {
				t.Fatal("duplicate notification changed fulfillment")
			}
		})
	}
}

func TestSweepRetriesDeliveryAndFlagsReview(t *testing.T) {
	s, _, m, o := setup(t)
	m.fail = true
	s.Notify(context.Background(), nil)
	if r, err := s.Sweep(context.Background()); err != nil || r.Pending != 1 || m.calls != 1 {
		t.Fatal("sweep ignored the lease", r, err, m.calls)
	}
	now := s.now()
	s.Now = func() time.Time { return now.Add(2 * time.Minute) }
	if r, err := s.Sweep(context.Background()); err != nil || r.Pending != 1 || m.calls != 2 {
		t.Fatal("sweep did not retry", r, err, m.calls)
	}
	s.Now = func() time.Time { return now.Add(23 * time.Hour) }
	if r, err := s.Sweep(context.Background()); err != nil || len(r.Review) != 1 || r.Review[0] != o.ID || m.calls != 2 {
		t.Fatal("expired delivery was not flagged for review", r, err, m.calls)
	}
}

func TestSweepDeliversAndThenSkips(t *testing.T) {
	s, _, m, _ := setup(t)
	m.fail = true
	s.Notify(context.Background(), nil)
	now := s.now()
	s.Now = func() time.Time { return now.Add(2 * time.Minute) }
	m.fail = false
	if r, err := s.Sweep(context.Background()); err != nil || r.Delivered != 1 || m.calls != 2 {
		t.Fatal(r, err, m.calls)
	}
	if r, err := s.Sweep(context.Background()); err != nil || r.Delivered+r.Pending+len(r.Review) != 0 || m.calls != 2 {
		t.Fatal("delivered order swept again", r, err)
	}
}

// checker adds a status lookup to the test gateway.
type checker struct {
	*gateway
	calls    int
	checkErr error
}

func (c *checker) Check(_ context.Context, id string) (payment.Status, error) {
	c.calls++
	if c.checkErr != nil {
		return payment.Status{}, c.checkErr
	}
	return c.status, nil
}

func TestSweepConfirmsPaymentWithoutNotification(t *testing.T) {
	s, g, m, o := setup(t)
	c := &checker{gateway: g}
	s.Gateway = c
	if r, err := s.Sweep(context.Background()); err != nil || c.calls != 0 {
		t.Fatal("checked before the notification had time to arrive", r, err)
	}
	now := s.now()
	s.Now = func() time.Time { return now.Add(10 * time.Minute) }
	r, err := s.Sweep(context.Background())
	if err != nil || len(r.Confirmed) != 1 || r.Confirmed[0] != o.ID || m.calls != 1 || m.code == "" {
		t.Fatal("paid order not fulfilled", r, err, m.calls)
	}
	if r, err := s.Sweep(context.Background()); err != nil || c.calls != 1 || len(r.Confirmed) != 0 {
		t.Fatal("paid order checked again", r, err, c.calls)
	}
}

func TestSweepAbandonsUnpaidOrders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status payment.Status
		err    error
		state  string
	}{
		{"expired", payment.Status{Amount: 5000, State: "expire"}, nil, "expire"},
		{"still pending", payment.Status{Amount: 5000, State: "pending"}, nil, "abandoned"},
		{"never started", payment.Status{}, payment.ErrNoTransaction, "abandoned"},
		{"gateway down", payment.Status{}, errors.New("unavailable"), "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, g, m, o := setup(t)
			tc.status.ID = o.ID
			g.status = tc.status
			c := &checker{gateway: g, checkErr: tc.err}
			s.Gateway = c
			now := s.now()
			s.Now = func() time.Time { return now.Add(48 * time.Hour) }
			r, err := s.Sweep(context.Background())
			stored, _ := s.Store.Get(context.Background(), o.ID)
			if err != nil || stored.State != tc.state || stored.Paid || m.calls != 0 || len(r.Confirmed)+len(r.PaymentReview) != 0 {
				t.Fatal(r, err, stored.State)
			}
		})
	}
}

func TestSweepFlagsMismatchedPayment(t *testing.T) {
	s, g, m, o := setup(t)
	g.status.Amount = 1
	s.Gateway = &checker{gateway: g}
	now := s.now()
	s.Now = func() time.Time { return now.Add(time.Hour) }
	r, err := s.Sweep(context.Background())
	if err != nil || len(r.PaymentReview) != 1 || r.PaymentReview[0] != o.ID || len(r.Confirmed) != 0 || m.calls != 0 {
		t.Fatal(r, err)
	}
}

func TestSweepAbandonsWithoutChecker(t *testing.T) {
	s, _, _, o := setup(t)
	now := s.now()
	s.Now = func() time.Time { return now.Add(47 * time.Hour) }
	s.Sweep(context.Background())
	if stored, _ := s.Store.Get(context.Background(), o.ID); stored.State != "pending" {
		t.Fatal("abandoned too early", stored.State)
	}
	s.Now = func() time.Time { return now.Add(48 * time.Hour) }
	s.Sweep(context.Background())
	if stored, _ := s.Store.Get(context.Background(), o.ID); stored.State != "abandoned" {
		t.Fatal("not abandoned", stored.State)
	}
	// A late notification still fulfils the order.
	if err := s.Notify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if stored, _ := s.Store.Get(context.Background(), o.ID); !stored.Paid {
		t.Fatal("late payment ignored")
	}
}
