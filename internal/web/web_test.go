package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/health", "/", "/static/htmx.min.js", "/static/icon.svg", "/static/apple-touch-icon.png"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if path == "/health" && rec.Body.String() != "ok" {
			t.Errorf("health response = %q, want ok", rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /missing = %d, want 404", rec.Code)
	}
}

func TestHomeLanguages(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want HTML with UTF-8", got)
	}
	for _, text := range []string{`lang="id"`, "Pembayaran untuk proyek Ardeman.", `lang="en"`, "Payments for Ardeman's projects.", `href="/static/icon.svg"`} {
		if !strings.Contains(rec.Body.String(), text) {
			t.Errorf("homepage missing %q", text)
		}
	}
}
