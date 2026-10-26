package orderstore

import (
	"context"
	"encoding/json"
	"errors"
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
