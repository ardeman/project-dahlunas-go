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
	for _, tc := range []struct {
		target, acceptLanguage string
		want, unwanted         []string
	}{
		{"/", "", []string{`<html lang="id">`, "Pembayaran untuk proyek Ardeman.", `href="/terms"`, `href="?lang=en"`}, []string{"Payments for Ardeman&#39;s projects."}},
		{"/", "en-US,en;q=0.9,id;q=0.8", []string{`<html lang="en">`, "Payments for Ardeman&#39;s projects.", `href="/terms"`}, []string{"Pembayaran untuk proyek Ardeman."}},
		{"/?lang=en", "", []string{`<html lang="en">`, "Payments for Ardeman&#39;s projects.", `href="/terms?lang=en"`}, nil},
		{"/?lang=id", "en", []string{`<html lang="id">`, "Pembayaran untuk proyek Ardeman.", `href="/terms?lang=id"`}, nil},
		{"/?lang=fr", "", []string{`<html lang="id">`, `href="/terms"`}, nil},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.target, nil)
		if tc.acceptLanguage != "" {
			r.Header.Set("Accept-Language", tc.acceptLanguage)
		}
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, r)
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q, want HTML with UTF-8", tc.target, got)
		}
		body := rec.Body.String()
		for _, text := range tc.want {
			if !strings.Contains(body, text) {
				t.Errorf("%s (%q) missing %q", tc.target, tc.acceptLanguage, text)
			}
		}
		for _, text := range tc.unwanted {
			if strings.Contains(body, text) {
				t.Errorf("%s (%q) shows %q", tc.target, tc.acceptLanguage, text)
			}
		}
	}
}
