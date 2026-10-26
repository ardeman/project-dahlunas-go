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
	"strings"
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

type Firestore struct {
	// documents is the database's documents URL; base adds the collection.
	documents, collection, base, activations string
	client                                   *http.Client
	token                                    string
	emulator                                 bool
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
	collection, activations := "orders-sandbox", "activations-sandbox"
	if production {
		collection, activations = "orders-production", "activations-production"
	}
	documents := root + "/v1/projects/" + project + "/databases/(default)/documents"
	return &Firestore{documents: documents, collection: collection, base: documents + "/" + collection + "/", activations: documents + "/" + activations + "/", token: token, emulator: host != "", client: &http.Client{Timeout: 15 * time.Second}}, nil
}

// The order is one JSON payload; deliveryPending and awaitingPayment are
// separate fields so the sweep can query them.
type value struct {
	String  *string `json:"stringValue,omitempty"`
	Boolean *bool   `json:"booleanValue,omitempty"`
}

type document struct {
	Name       string           `json:"name,omitempty"`
	Fields     map[string]value `json:"fields"`
	UpdateTime string           `json:"updateTime,omitempty"`
}

func decode(d document, id string) (shop.Order, error) {
	var result shop.Order
	payload := d.Fields["payload"].String
	if payload == nil || json.Unmarshal([]byte(*payload), &result) != nil || result.ID != id || d.UpdateTime == "" {
		return shop.Order{}, errors.New("store: invalid record")
	}
	result.Revision = d.UpdateTime
	return result, nil
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
		text, pending, awaiting := string(payload), o.DeliveryPending(), o.AwaitingPayment()
		d := document{Fields: map[string]value{"payload": {String: &text}, "deliveryPending": {Boolean: &pending}, "awaitingPayment": {Boolean: &awaiting}}}
		b, err := json.Marshal(d)
		if err != nil {
			return shop.Order{}, err
		}
		body = bytes.NewReader(b)
	}
	d, err := f.exchange(ctx, method, f.base+id+query, body)
	if err != nil {
		return shop.Order{}, err
	}
	return decode(d, id)
}

// exchange sends one document request, mapping failed preconditions to
// shop.ErrConflict and a missing document to shop.ErrNotFound.
func (f *Firestore) exchange(ctx context.Context, method, target string, body io.Reader) (document, error) {
	r, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return document{}, errors.New("store: request failed")
	}
	resp, err := f.do(ctx, r)
	if err != nil {
		return document{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 404:
		return document{}, shop.ErrNotFound
	case 409, 412:
		return document{}, shop.ErrConflict
	}
	if resp.StatusCode == 400 {
		var failure struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&failure) == nil && failure.Error.Status == "FAILED_PRECONDITION" {
			return document{}, shop.ErrConflict
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return document{}, errors.New("store: operation failed")
	}
	var d document
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&d) != nil {
		return document{}, errors.New("store: invalid record")
	}
	return d, nil
}

func (f *Firestore) do(ctx context.Context, r *http.Request) (*http.Response, error) {
	r.Header.Set("Content-Type", "application/json")
	if !f.emulator {
		token, err := f.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.client.Do(r)
	if err != nil {
		return nil, errors.New("store: unavailable")
	}
	return resp, nil
}

// sweepLimit bounds one sweep; the next scheduled run picks up the rest.
const sweepLimit = 50

// Undelivered queries paid orders whose email is not confirmed sent. Records
// written before the deliveryPending field existed are not found.
func (f *Firestore) Undelivered(ctx context.Context) ([]shop.Order, error) {
	return f.flagged(ctx, "deliveryPending")
}

// Unpaid queries orders still waiting for a final payment state. Records
// written before the awaitingPayment field existed are not found.
func (f *Firestore) Unpaid(ctx context.Context) ([]shop.Order, error) {
	return f.flagged(ctx, "awaitingPayment")
}

func (f *Firestore) flagged(ctx context.Context, field string) ([]shop.Order, error) {
	query, _ := json.Marshal(map[string]any{"structuredQuery": map[string]any{
		"from":  []map[string]string{{"collectionId": f.collection}},
		"where": map[string]any{"fieldFilter": map[string]any{"field": map[string]string{"fieldPath": field}, "op": "EQUAL", "value": map[string]bool{"booleanValue": true}}},
		"limit": sweepLimit,
	}})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, f.documents+":runQuery", bytes.NewReader(query))
	if err != nil {
		return nil, errors.New("store: request failed")
	}
	resp, err := f.do(ctx, r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var results []struct {
		Document *document `json:"document"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, sweepLimit*128<<10)).Decode(&results) != nil {
		return nil, errors.New("store: query failed")
	}
	var orders []shop.Order
	for _, result := range results {
		if result.Document == nil {
			continue
		}
		id := result.Document.Name[strings.LastIndex(result.Document.Name, "/")+1:]
		if !orderID.MatchString(id) {
			return nil, errors.New("store: invalid record")
		}
		o, err := decode(*result.Document, id)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
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

// Activation records live in their own collection, one document per code ID,
// as one JSON payload like orders.
func (f *Firestore) activation(ctx context.Context, method, id, query string, a *shop.ActivationRecord) (shop.ActivationRecord, error) {
	if !orderID.MatchString(id) {
		return shop.ActivationRecord{}, shop.ErrNotFound
	}
	var body io.Reader
	if a != nil {
		payload, err := json.Marshal(a)
		if err != nil {
			return shop.ActivationRecord{}, err
		}
		text := string(payload)
		b, err := json.Marshal(document{Fields: map[string]value{"payload": {String: &text}}})
		if err != nil {
			return shop.ActivationRecord{}, err
		}
		body = bytes.NewReader(b)
	}
	d, err := f.exchange(ctx, method, f.activations+id+query, body)
	if err != nil {
		return shop.ActivationRecord{}, err
	}
	var result shop.ActivationRecord
	payload := d.Fields["payload"].String
	if payload == nil || json.Unmarshal([]byte(*payload), &result) != nil || result.CodeID != id || d.UpdateTime == "" {
		return shop.ActivationRecord{}, errors.New("store: invalid record")
	}
	result.Revision = d.UpdateTime
	return result, nil
}

func (f *Firestore) CreateActivation(ctx context.Context, a shop.ActivationRecord) error {
	_, err := f.activation(ctx, http.MethodPatch, a.CodeID, "?currentDocument.exists=false", &a)
	return err
}
func (f *Firestore) GetActivation(ctx context.Context, id string) (shop.ActivationRecord, error) {
	return f.activation(ctx, http.MethodGet, id, "", nil)
}
func (f *Firestore) SaveActivation(ctx context.Context, a shop.ActivationRecord) error {
	if a.Revision == "" {
		return shop.ErrConflict
	}
	_, err := f.activation(ctx, http.MethodPatch, a.CodeID, "?currentDocument.updateTime="+url.QueryEscape(a.Revision), &a)
	return err
}
