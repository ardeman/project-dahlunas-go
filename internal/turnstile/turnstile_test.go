package turnstile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerification(t *testing.T) {
	for _, tc := range []struct {
		name, token, body string
		status            int
		ok, bad           bool
	}{
		{"valid", "token", `{"success":true,"hostname":"shop.example","action":"checkout"}`, 200, true, false},
		{"rejected or replayed", "token", `{"success":false,"error-codes":["timeout-or-duplicate"]}`, 200, false, false},
		{"foreign host", "token", `{"success":true,"hostname":"other.example","action":"checkout"}`, 200, false, false},
		{"wrong action", "token", `{"success":true,"hostname":"shop.example","action":"login"}`, 200, false, false},
		{"malformed", "token", `{`, 200, false, true},
		{"unavailable", "token", `{}`, 503, false, true},
		{"network", "token", "", 0, false, true},
		{"missing", "", "", 0, false, false},
		{"oversized", strings.Repeat("x", 2049), "", 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New("private-test-key", "shop.example")
			calls := 0
			c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || r.Method != "POST" {
					t.Fatal("wrong endpoint")
				}
				if err := r.ParseForm(); err != nil || r.PostForm.Get("secret") != c.Secret || r.PostForm.Get("response") != tc.token || r.PostForm.Get("remoteip") != "" {
					t.Fatal("wrong verification payload")
				}
				if tc.status == 0 {
					return nil, errors.New("network failure")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})
			ok, err := c.Verify(context.Background(), tc.token)
			if ok != tc.ok || (err != nil) != tc.bad {
				t.Fatal(ok, err)
			}
			if (tc.token == "" || len(tc.token) > 2048) && calls != 0 {
				t.Fatal("invalid token reached provider")
			}
			if err != nil && strings.Contains(err.Error(), c.Secret) {
				t.Fatal("secret leaked")
			}
		})
	}
}
