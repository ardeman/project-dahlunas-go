// Package orderstore persists orders in Firestore using conditional writes.
package orderstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

type Firestore struct {
	base     string
	client   *http.Client
	token    string
	emulator bool
}

// New uses Cloud Run's attached service account when token is empty. For local
// development, token can be a short-lived gcloud access token or host can point
// at a Firestore emulator. Collections separate sandbox and production orders.
func New(project, token, host string, production bool) (*Firestore, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{3,62}$`).MatchString(project) {
		return nil, errors.New("store: invalid project ID")
	}
	root := "https://firestore.googleapis.com"
	if host != "" {
		u, err := url.Parse("http://" + host)
		if err != nil || u.Host != host || u.Path != "" || u.User != nil {
			return nil, errors.New("store: invalid emulator host")
		}
		if production {
			return nil, errors.New("store: production cannot use emulator")
		}
		root = u.String()
	}
	collection := "orders-sandbox"
	if production {
		collection = "orders-production"
	}
	return &Firestore{base: root + "/v1/projects/" + project + "/databases/(default)/documents/" + collection + "/", token: token, emulator: host != "", client: &http.Client{Timeout: 15 * time.Second}}, nil
}

type document struct {
	Fields map[string]struct {
		Value string `json:"stringValue"`
	} `json:"fields"`
	UpdateTime string `json:"updateTime,omitempty"`
}

func (f *Firestore) accessToken(ctx context.Context) (string, error) {
	if f.token != "" {
		return f.token, nil
	}
	r, _ := http.NewRequestWithContext(ctx, "GET", "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	r.Header.Set("Metadata-Flavor", "Google")
	resp, err := f.client.Do(r)
	if err != nil {
		return "", errors.New("store: credentials unavailable")
	}
	defer resp.Body.Close()
	var t struct {
		Token string `json:"access_token"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&t) != nil || t.Token == "" {
		return "", errors.New("store: credentials unavailable")
	}
	return t.Token, nil
}

var orderID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (f *Firestore) request(ctx context.Context, method, id, query string, o *shop.Order) (shop.Order, error) {
	if !orderID.MatchString(id) {
		return shop.Order{}, shop.ErrNotFound
	}
	var body io.Reader
	if o != nil {
		payload, err := json.Marshal(o)
		if err != nil {
			return shop.Order{}, err
		}
		d := document{Fields: map[string]struct {
			Value string `json:"stringValue"`
		}{"payload": {Value: string(payload)}}}
		b, err := json.Marshal(d)
		if err != nil {
			return shop.Order{}, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, f.base+id+query, body)
	if err != nil {
		return shop.Order{}, errors.New("store: request failed")
	}
	r.Header.Set("Content-Type", "application/json")
	if !f.emulator {
		token, err := f.accessToken(ctx)
		if err != nil {
			return shop.Order{}, err
		}
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.client.Do(r)
	if err != nil {
		return shop.Order{}, errors.New("store: unavailable")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 404:
		return shop.Order{}, shop.ErrNotFound
	case 409, 412:
		return shop.Order{}, shop.ErrConflict
	}
	if resp.StatusCode == 400 {
		var failure struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&failure) == nil && failure.Error.Status == "FAILED_PRECONDITION" {
			return shop.Order{}, shop.ErrConflict
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return shop.Order{}, errors.New("store: operation failed")
	}
	var d document
	var result shop.Order
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&d) != nil || json.Unmarshal([]byte(d.Fields["payload"].Value), &result) != nil || result.ID != id || d.UpdateTime == "" {
		return shop.Order{}, errors.New("store: invalid record")
	}
	result.Revision = d.UpdateTime
	return result, nil
}

func (f *Firestore) Create(ctx context.Context, o shop.Order) error {
	_, err := f.request(ctx, http.MethodPatch, o.ID, "?currentDocument.exists=false", &o)
	return err
}
func (f *Firestore) Get(ctx context.Context, id string) (shop.Order, error) {
	return f.request(ctx, http.MethodGet, id, "", nil)
}
func (f *Firestore) Save(ctx context.Context, o shop.Order) error {
	if o.Revision == "" {
		return shop.ErrConflict
	}
	_, err := f.request(ctx, http.MethodPatch, o.ID, "?currentDocument.updateTime="+url.QueryEscape(o.Revision), &o)
	return err
}
