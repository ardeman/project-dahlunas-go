package account

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func signToken(secret, uid, email string, exp int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"` + uid + `","email":"` + email + `","exp":` + itoa(exp) + `}`))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestVerifyToken(t *testing.T) {
	now := time.Date(2026, 11, 22, 6, 0, 0, 0, time.UTC)
	token := signToken("secret", "uid-1", "a@example.com", now.Add(5*time.Minute).Unix())
	claims, err := VerifyToken("secret", token, now)
	if err != nil || claims.UID != "uid-1" || claims.Email != "a@example.com" {
		t.Fatal(claims, err)
	}
	if _, err := VerifyToken("other", token, now); err == nil {
		t.Fatal("expected bad secret")
	}
	if _, err := VerifyToken("secret", token, now.Add(10*time.Minute)); err == nil {
		t.Fatal("expected expired")
	}
}

func TestDelivererSignsBody(t *testing.T) {
	var gotSig, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotSig = r.Header.Get("X-Dahlunas-Signature")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	d := &Deliverer{URL: server.URL, Secret: "delivery-secret", Client: server.Client()}
	paid := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	if err := d.Deliver(context.Background(), "uid-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 3, paid); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("delivery-secret"))
	mac.Write([]byte(gotBody))
	if gotSig != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal(gotSig, gotBody)
	}
	if !json.Valid([]byte(gotBody)) {
		t.Fatal(gotBody)
	}
}
