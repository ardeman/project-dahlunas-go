package shop

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/account"
	"github.com/ardeman/project-dahlunas-go/internal/payment"
)

// cancelGateway is a gateway whose orders can be checked and cancelled.
type cancelGateway struct {
	mu       sync.Mutex
	statuses map[string]payment.Status
	// after is the status a cancel leaves behind.
	after   payment.Status
	checkNo error
	cancels int
	creates int
}

func (g *cancelGateway) Create(_ context.Context, c payment.Checkout) (payment.Session, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.creates++
	return payment.Session{Token: "token-" + c.ID}, nil
}
func (g *cancelGateway) Notification(ctx context.Context, b []byte, _ string) (payment.Status, error) {
	return g.Check(ctx, string(b))
}
func (g *cancelGateway) Check(_ context.Context, id string) (payment.Status, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.statuses[id], g.checkNo
}
func (g *cancelGateway) Cancel(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cancels++
	if g.after.State == "" {
		return errors.New("refused")
	}
	st := g.statuses[id]
	st.State, st.Paid = g.after.State, g.after.Paid
	g.statuses[id] = st
	return nil
}

func pendingOrder(t *testing.T, method string) (*Service, *cancelGateway, Order) {
	t.Helper()
	ctx := context.Background()
	s, _, _, _ := setup(t)
	g := &cancelGateway{statuses: map[string]payment.Status{}}
	s.Gateway = g
	o, err := s.Create(ctx, "coba-lagi-1", "buyer@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if o.CanChangePayment() {
		t.Fatal("no method chosen yet")
	}
	g.statuses[o.ID] = payment.Status{ID: o.ID, Amount: 5000, State: "pending", Method: method}
	if err := s.Notify(ctx, []byte(o.ID)); err != nil {
		t.Fatal(err)
	}
	if o, err = s.Store.Get(ctx, o.ID); err != nil || o.Method != method {
		t.Fatal(o.Method, err)
	}
	return s, g, o
}

func TestChangePaymentReplacesCancelledOrderOnce(t *testing.T) {
	ctx := context.Background()
	s, g, o := pendingOrder(t, "echannel")
	if !o.CanChangePayment() {
		t.Fatal("button missing")
	}
	if _, err := s.ChangePayment(ctx, o.ID, "wrong", "en"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	g.after = payment.Status{State: "cancel"}
	var wg sync.WaitGroup
	ids := make([]string, 6)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, err := s.ChangePayment(ctx, o.ID, o.ViewToken, "en")
			if err != nil {
				t.Error(err)
			}
			ids[i] = next.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == o.ID || id != ids[0] {
			t.Fatal(ids)
		}
	}
	if g.creates != 2 {
		t.Fatal(g.creates)
	}
	next, _ := s.Store.Get(ctx, ids[0])
	if next.Email != o.Email || next.Product.Slug != o.Product.Slug || next.Lang != "en" || next.Method != "" || next.SnapToken == "" {
		t.Fatalf("%+v", next)
	}
	// A delayed pending status must not reopen the cancelled payment.
	if err := s.apply(ctx, payment.Midtrans, payment.Status{ID: o.ID, Amount: 5000, State: "pending"}); err != nil {
		t.Fatal(err)
	}
	if old, _ := s.Store.Get(ctx, o.ID); old.State != "cancel" {
		t.Fatal(old.State)
	}
}

func TestChangePaymentNeverReplacesLivePayment(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		method string
		before func(*cancelGateway, string)
		paid   bool
	}{
		"cancel refused": {"qris", func(*cancelGateway, string) {}, false},
		"check fails":    {"qris", func(g *cancelGateway, _ string) { g.checkNo = errors.New("down") }, false},
		"card":           {"credit_card", func(g *cancelGateway, _ string) { g.after.State = "cancel" }, false},
		"already paid": {"qris", func(g *cancelGateway, id string) {
			st := g.statuses[id]
			st.State, st.Paid = "settlement", true
			g.statuses[id] = st
		}, true},
		"paid mid-cancel": {"qris", func(g *cancelGateway, _ string) { g.after = payment.Status{State: "settlement", Paid: true} }, true},
	} {
		t.Run(name, func(t *testing.T) {
			s, g, o := pendingOrder(t, tc.method)
			tc.before(g, o.ID)
			got, err := s.ChangePayment(ctx, o.ID, o.ViewToken, "en")
			if tc.paid != (err == nil) || got.Paid != tc.paid || g.creates != 1 {
				t.Fatal(got.Paid, err, g.creates)
			}
			if tc.paid && (got.ID != o.ID || got.Code == "") {
				t.Fatal("paid order not kept")
			}
			if (name == "already paid" || name == "card") && g.cancels != 0 {
				t.Fatal("cancelled a payment it must not touch")
			}
		})
	}
}

func TestChangePaymentKeepsAccount(t *testing.T) {
	ctx := context.Background()
	s, g, o := pendingOrder(t, "qris")
	o.Product.Account, o.AccountUID = true, "uid-1"
	if err := s.Store.Save(ctx, o); err != nil {
		t.Fatal(err)
	}
	s.Products = []Product{o.Product}
	s.Products[0].Months = 1
	s.Accounts = &account.Deliverer{}
	g.after = payment.Status{State: "cancel"}
	next, err := s.ChangePayment(ctx, o.ID, o.ViewToken, "en")
	if err != nil || next.AccountUID != "uid-1" || next.ID == o.ID {
		t.Fatal(next.AccountUID, err)
	}
}
