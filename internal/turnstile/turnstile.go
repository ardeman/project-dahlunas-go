// Package turnstile verifies checkout challenges with Cloudflare.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Secret, Hostname string
	HTTP             *http.Client
}

func New(secret, hostname string) *Client {
	return &Client{Secret: secret, Hostname: hostname, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// Verify requires a successful, single-use token for this host and checkout action.
// Provider errors never include the response body or credentials.
func (c *Client) Verify(ctx context.Context, token string) (bool, error) {
	if token == "" || len(token) > 2048 {
		return false, nil
	}
	body := url.Values{"secret": {c.Secret}, "response": {token}}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(body.Encode()))
	if err != nil {
		return false, errors.New("challenge request failed")
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return false, errors.New("challenge service unavailable")
	}
	defer resp.Body.Close()
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&result) != nil {
		return false, errors.New("invalid challenge service response")
	}
	return result.Success && result.Hostname == c.Hostname && result.Action == "checkout", nil
}
