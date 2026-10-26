package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledShopNeedsNoCredentials(t *testing.T) {
	t.Setenv("SHOP_ENABLED", "false")
	t.Setenv("MIDTRANS_SERVER_KEY", "")
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Checkout is not open yet") {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestEnabledShopRequiresConfiguration(t *testing.T) {
	t.Setenv("SHOP_ENABLED", "true")
	t.Setenv("MIDTRANS_PRODUCTION", "false")
	t.Setenv("MIDTRANS_SERVER_KEY", "")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "MIDTRANS_SERVER_KEY") {
		t.Fatal(err)
	}
	t.Setenv("MIDTRANS_PRODUCTION", "typo")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "MIDTRANS_PRODUCTION") {
		t.Fatal(err)
	}
}

func TestProductionRejectsInsecureURLAndEmulator(t *testing.T) {
	for name, value := range map[string]string{"SHOP_ENABLED": "true", "MIDTRANS_PRODUCTION": "true", "MIDTRANS_SERVER_KEY": "test-server", "MIDTRANS_CLIENT_KEY": "test-client", "SHOP_BASE_URL": "http://localhost:8080", "SHOP_REFUND_ID": "Ketentuan uji", "SHOP_REFUND_EN": "Test terms", "GOOGLE_CLOUD_PROJECT": "test-project", "RESEND_API_KEY": "test-mail", "RESEND_FROM": "shop@example.com", "SHOP_SUPPORT_EMAIL": "support@example.com", "DAHLUNAS_KEY_1_1": strings.Repeat("00", 32), "SHOP_KEY_ID": "1", "FIRESTORE_EMULATOR_HOST": "localhost:8081"} {
		t.Setenv(name, value)
	}
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "SHOP_BASE_URL") {
		t.Fatal(err)
	}
	t.Setenv("SHOP_BASE_URL", "https://shop.example")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "emulator") {
		t.Fatal(err)
	}
	t.Setenv("FIRESTORE_EMULATOR_HOST", "")
	if _, err := Handler(); err != nil {
		t.Fatal(err)
	}
}
