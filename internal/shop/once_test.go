package shop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCreateOnceReusesSubmittedOrder(t *testing.T) {
	ctx := context.Background()
	s, g, _, _ := setup(t)
	once := func(key, slug, email string) Order {
		t.Helper()
		o, err := s.CreateOnce(ctx, key, slug, email, "")
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	if _, err := s.Submitted(ctx, "a", "coba-lagi-1", "buyer@example.com", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	first := once("a", "coba-lagi-1", "buyer@example.com")
	if again := once("a", "coba-lagi-1", "buyer@example.com"); again.ID != first.ID || again.ViewToken != first.ViewToken {
		t.Fatal("resubmitted form made another order")
	}
	if o, err := s.Submitted(ctx, "a", "coba-lagi-1", "buyer@example.com", ""); err != nil || o.ID != first.ID {
		t.Fatal(o.ID, err)
	}
	for _, other := range []Order{once("b", "coba-lagi-1", "buyer@example.com"), once("a", "coba-lagi-2", "buyer@example.com"), once("a", "coba-lagi-1", "other@example.com")} {
		if other.ID == first.ID {
			t.Fatal("different form reused the order")
		}
	}
	// A paid order is shown again, an ended one is replaced.
	g.status.ID, g.status.State = first.ID, "settlement"
	if err := s.Notify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if again := once("a", "coba-lagi-1", "buyer@example.com"); again.ID != first.ID || !again.Paid {
		t.Fatal("paid order not reused")
	}
	ended := once("c", "coba-lagi-1", "buyer@example.com")
	ended, _ = s.Store.Get(ctx, ended.ID)
	ended.State = "expire"
	if err := s.Store.Save(ctx, ended); err != nil {
		t.Fatal(err)
	}
	next := once("c", "coba-lagi-1", "buyer@example.com")
	if next.ID == ended.ID || once("c", "coba-lagi-1", "buyer@example.com").ID != next.ID {
		t.Fatal("ended order not replaced by one new order")
	}
	if _, err := s.CreateOnce(ctx, "", "coba-lagi-1", "buyer@example.com", ""); !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
}

func TestCreateOnceConcurrentSubmissions(t *testing.T) {
	s, _, _, _ := setup(t)
	var wg sync.WaitGroup
	ids := make([]string, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.CreateOnce(context.Background(), "race", "coba-lagi-1", "buyer@example.com", "")
			if err != nil {
				t.Error(err)
			}
			ids[i] = o.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatal(ids)
		}
	}
}

func TestCreateOnceWithoutPaymentSession(t *testing.T) {
	ctx := context.Background()
	s, _, _, _ := setup(t)
	id := submittedID("k", "coba-lagi-1", "buyer@example.com", "", 0)
	if err := s.Store.Create(ctx, Order{ID: id, State: "pending", Created: s.now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOnce(ctx, "k", "coba-lagi-1", "buyer@example.com", ""); !errors.Is(err, ErrRetry) {
		t.Fatal(err)
	}
	later := s.now().Add(sessionWait + time.Second)
	s.Now = func() time.Time { return later }
	o, err := s.CreateOnce(ctx, "k", "coba-lagi-1", "buyer@example.com", "")
	if err != nil || o.ID == id {
		t.Fatal(o.ID, err)
	}
}
