package orderstore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func TestConditionalWritesAcrossClients(t *testing.T) {
	var mu sync.Mutex
	var stored document
	revision := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer local-token" {
			t.Error("missing authorization")
		}
		if !strings.Contains(r.URL.Path, "/orders-sandbox/") {
			t.Error("wrong collection")
		}
		if r.Method == "PATCH" {
			q := r.URL.Query()
			if (q.Get("currentDocument.exists") == "false" && revision != 0) || (q.Get("currentDocument.updateTime") != "" && q.Get("currentDocument.updateTime") != stored.UpdateTime) {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":{"status":"FAILED_PRECONDITION"}}`))
				return
			}
			if q.Get("currentDocument.exists") == "" && q.Get("currentDocument.updateTime") == "" {
				t.Error("unconditional write")
			}
			json.NewDecoder(r.Body).Decode(&stored)
			revision++
			stored.UpdateTime = "2026-10-26T00:00:0" + strconv.Itoa(revision) + "Z"
		}
		if revision == 0 {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(stored)
	}))
	defer server.Close()
	f, _ := New("test-project", "local-token", "", false)
	f.base = server.URL + "/orders-sandbox/"
	ctx := context.Background()
	o := shop.Order{ID: strings.Repeat("a", 32), Email: "buyer@example.com"}
	if _, err := f.Get(ctx, o.ID); !errors.Is(err, shop.ErrNotFound) {
		t.Fatal(err)
	}
	if err := f.Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	if err := f.Create(ctx, o); !errors.Is(err, shop.ErrConflict) {
		t.Fatal("duplicate create", err)
	}
	first, err := f.Get(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := New("test-project", "local-token", "", false)
	other.base = f.base
	stale, err := other.Get(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	first.Paid = true
	first.Code = "persisted-code"
	if err := f.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := other.Save(ctx, stale); !errors.Is(err, shop.ErrConflict) {
		t.Fatal("stale update", err)
	}
	saved, err := other.Get(ctx, o.ID)
	if err != nil || !saved.Paid || saved.Code != "persisted-code" {
		t.Fatal("durability", saved, err)
	}
}

func TestStoreEnvironmentIsolation(t *testing.T) {
	sandbox, err := New("test-project", "", "localhost:8081", false)
	if err != nil {
		t.Fatal(err)
	}
	if !sandbox.emulator || !strings.Contains(sandbox.base, "orders-sandbox") {
		t.Fatal("sandbox")
	}
	production, err := New("test-project", "", "", true)
	if err != nil || !strings.Contains(production.base, "orders-production") {
		t.Fatal("production", err)
	}
	if _, err := New("test-project", "", "localhost:8081", true); err == nil {
		t.Fatal("production emulator allowed")
	}
	if _, err := New("../escape", "", "", false); err == nil {
		t.Fatal("invalid project allowed")
	}
	if _, err := sandbox.Get(context.Background(), "../private"); !errors.Is(err, shop.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestUndeliveredQueriesPendingFlag(t *testing.T) {
	id := strings.Repeat("b", 32)
	payload, _ := json.Marshal(shop.Order{ID: id, Paid: true})
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/documents:runQuery") {
			t.Error("unexpected request", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		query = string(b)
		text := string(payload)
		json.NewEncoder(w).Encode([]map[string]any{
			{"readTime": "2026-10-26T00:00:00Z"},
			{"document": document{Name: "projects/p/databases/(default)/documents/orders-sandbox/" + id, Fields: map[string]value{"payload": {String: &text}}, UpdateTime: "2026-10-26T00:00:01Z"}},
		})
	}))
	defer server.Close()
	f, _ := New("test-project", "local-token", "", false)
	f.documents = server.URL + "/documents"
	orders, err := f.Undelivered(context.Background())
	if err != nil || len(orders) != 1 || orders[0].ID != id || orders[0].Revision != "2026-10-26T00:00:01Z" {
		t.Fatal(orders, err)
	}
	if !strings.Contains(query, `"collectionId":"orders-sandbox"`) || !strings.Contains(query, `"fieldPath":"deliveryPending"`) {
		t.Fatal(query)
	}
	if _, err := f.Unpaid(context.Background()); err != nil || !strings.Contains(query, `"fieldPath":"awaitingPayment"`) {
		t.Fatal(query, err)
	}
}

func TestSaveWritesDeliveryPendingField(t *testing.T) {
	var written document
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&written)
		written.UpdateTime = "2026-10-26T00:00:02Z"
		json.NewEncoder(w).Encode(written)
	}))
	defer server.Close()
	f, _ := New("test-project", "local-token", "", false)
	f.base = server.URL + "/orders-sandbox/"
	o := shop.Order{ID: strings.Repeat("c", 32), Paid: true, Revision: "r1"}
	if err := f.Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if p := written.Fields["deliveryPending"].Boolean; p == nil || !*p {
		t.Fatal("paid, unsent order not flagged")
	}
	o.EmailSent = true
	f.Save(context.Background(), o)
	if p := written.Fields["deliveryPending"].Boolean; p == nil || *p {
		t.Fatal("sent order still flagged")
	}
	if a := written.Fields["awaitingPayment"].Boolean; a == nil || *a {
		t.Fatal("paid order awaiting payment")
	}
	f.Save(context.Background(), shop.Order{ID: o.ID, State: "pending", Revision: "r2"})
	if a := written.Fields["awaitingPayment"].Boolean; a == nil || !*a {
		t.Fatal("pending order not flagged")
	}
}

func TestActivationsUseOwnCollectionAndConditions(t *testing.T) {
	var stored document
	revision := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/activations-sandbox/") {
			t.Error("wrong collection", r.URL.Path)
		}
		if r.Method == "PATCH" {
			q := r.URL.Query()
			if (q.Get("currentDocument.exists") == "false" && revision != 0) || (q.Get("currentDocument.updateTime") != "" && q.Get("currentDocument.updateTime") != stored.UpdateTime) {
				w.WriteHeader(412)
				return
			}
			json.NewDecoder(r.Body).Decode(&stored)
			revision++
			stored.UpdateTime = "2026-10-26T00:00:0" + strconv.Itoa(revision) + "Z"
		}
		if revision == 0 {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(stored)
	}))
	defer server.Close()
	f, _ := New("test-project", "local-token", "", false)
	f.activations = server.URL + "/activations-sandbox/"
	ctx := context.Background()
	a := shop.ActivationRecord{CodeID: strings.Repeat("b", 32), Project: 2, Device: strings.Repeat("c", 64)}
	if _, err := f.GetActivation(ctx, a.CodeID); !errors.Is(err, shop.ErrNotFound) {
		t.Fatal(err)
	}
	if err := f.CreateActivation(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateActivation(ctx, a); !errors.Is(err, shop.ErrConflict) {
		t.Fatal("duplicate create", err)
	}
	got, err := f.GetActivation(ctx, a.CodeID)
	if err != nil || got.Device != a.Device || got.Revision == "" {
		t.Fatal(got, err)
	}
	stale := got
	got.Moves = 1
	if err := f.SaveActivation(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveActivation(ctx, stale); !errors.Is(err, shop.ErrConflict) {
		t.Fatal("stale save", err)
	}
}
