package app

import (
	"encoding/json"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
	"github.com/ardeman/project-dahlunas-go/internal/web"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Belum menerima pembayaran") {
		t.Fatal(r.Code, r.Body.String())
	}
	if !strings.Contains(r.Body.String(), "Sandbox") {
		t.Fatal("closed sandbox shop lost its band")
	}
	t.Setenv("SHOP_PRODUCTION", "true")
	h, err = Handler()
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(r.Body.String(), "Sandbox") {
		t.Fatal("closed production shop shows the sandbox band")
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
	t.Setenv("TURNSTILE_SITE_KEY", "")
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "TURNSTILE") {
		t.Fatal("production allowed missing bot protection", err)
	}
	t.Setenv("TURNSTILE_SITE_KEY", "test-public")
	if _, err := Handler(); err == nil {
		t.Fatal("accepted incomplete Turnstile keys")
	}
	t.Setenv("TURNSTILE_SECRET_KEY", "test-secret")
	if _, err := Handler(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAHLUNAS_TASK_TOKEN", "short")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "DAHLUNAS_TASK_TOKEN") {
		t.Fatal("accepted a guessable task token", err)
	}
}

func TestCustomCatalogValidatesEachProjectKey(t *testing.T) {
	for name, value := range map[string]string{"SHOP_ENABLED": "true", "MIDTRANS_PRODUCTION": "false", "MIDTRANS_SERVER_KEY": "test", "MIDTRANS_CLIENT_KEY": "test", "SHOP_BASE_URL": "http://localhost:8080", "SHOP_REFUND_ID": "Uji", "SHOP_REFUND_EN": "Test", "GOOGLE_CLOUD_PROJECT": "test-project", "RESEND_API_KEY": "test", "RESEND_FROM": "shop@example.com", "SHOP_SUPPORT_EMAIL": "support@example.com", "SHOP_KEY_ID": "", "FIRESTORE_EMULATOR_HOST": "localhost:8081", "DAHLUNAS_KEY_1_1": strings.Repeat("00", 32), "DAHLUNAS_KEY_2_3": ""} {
		t.Setenv(name, value)
	}
	b, err := os.ReadFile("../shop/catalog/coba-lagi.json")
	if err != nil {
		t.Fatal(err)
	}
	var projects []shop.ProjectConfig
	if err := json.Unmarshal(b, &projects); err != nil {
		t.Fatal(err)
	}
	second := projects[0]
	second.ID, second.KeyID = 2, 3
	for i := range second.Products {
		second.Products[i].Slug = "second-" + second.Products[i].Slug
	}
	// Re-read into a fresh slice so project products are independent.
	projects = nil
	json.Unmarshal(b, &projects)
	projects = append(projects, second)
	b, _ = json.Marshal(projects)
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHOP_CATALOG_FILE", path)
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "DAHLUNAS_KEY_2_3") {
		t.Fatal(err)
	}
	t.Setenv("DAHLUNAS_KEY_2_3", strings.Repeat("01", 32))
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "second-coba-lagi-1") {
		t.Fatal("custom catalog not rendered")
	}
	t.Setenv("SHOP_KEY_ID", "1")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "SHOP_KEY_ID") {
		t.Fatal(err)
	}
	t.Setenv("SHOP_KEY_ID", "")
	os.WriteFile(path, []byte(`[{"id":2}]`), 0600)
	if _, err := Handler(); err == nil {
		t.Fatal("invalid catalog enabled checkout")
	}
}

// Tracked catalogs ship in the image at /config; a broken one would stop startup.
func TestTrackedCatalogsAreValid(t *testing.T) {
	paths, _ := filepath.Glob("../../config/*.json")
	if len(paths) == 0 {
		t.Fatal("no tracked catalogs")
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		products, err := shop.ParseCatalog(file)
		file.Close()
		if err != nil {
			t.Fatal(path, err)
		}
		if err := web.CheckImages(products); err != nil {
			t.Fatal(path, err)
		}
	}
}

func TestProductionModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		shop, legacy string
		want, bad    bool
	}{{"", "", false, false}, {"", "true", true, false}, {"true", "", true, false}, {"false", "false", false, false}, {"true", "false", false, true}, {"false", "true", false, true}, {"typo", "", false, true}} {
		t.Run(tc.shop+"/"+tc.legacy, func(t *testing.T) {
			t.Setenv("SHOP_PRODUCTION", tc.shop)
			t.Setenv("MIDTRANS_PRODUCTION", tc.legacy)
			got, err := productionMode()
			if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
				t.Fatal(got, err)
			}
		})
	}
}

func TestLemonOnlyShopNeedsNoMidtransCredentials(t *testing.T) {
	for name, value := range map[string]string{"SHOP_ENABLED": "true", "SHOP_PRODUCTION": "false", "MIDTRANS_PRODUCTION": "false", "MIDTRANS_SERVER_KEY": "", "MIDTRANS_CLIENT_KEY": "", "SHOP_BASE_URL": "http://localhost:8080", "SHOP_REFUND_ID": "Uji", "SHOP_REFUND_EN": "Test", "GOOGLE_CLOUD_PROJECT": "test-project", "RESEND_API_KEY": "test", "RESEND_FROM": "shop@example.com", "SHOP_SUPPORT_EMAIL": "support@example.com", "SHOP_KEY_ID": "", "FIRESTORE_EMULATOR_HOST": "localhost:8081", "DAHLUNAS_KEY_1_1": strings.Repeat("00", 32), "LEMONSQUEEZY_API_KEY": "test", "LEMONSQUEEZY_WEBHOOK_SECRET": "test-secret", "LEMONSQUEEZY_STORE_ID": "12"} {
		t.Setenv(name, value)
	}
	b, err := os.ReadFile("../shop/catalog/coba-lagi.json")
	if err != nil {
		t.Fatal(err)
	}
	var projects []shop.ProjectConfig
	json.Unmarshal(b, &projects)
	for i := range projects[0].Products {
		p := &projects[0].Products[i]
		p.Gateway = "lemonsqueezy"
		p.Currency = "USD"
		p.VariantID = "34"
		p.Price = 599
	}
	b, _ = json.Marshal(projects)
	path := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHOP_CATALOG_FILE", path)
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "US$ 5.99") {
		t.Fatal("USD shop unavailable")
	}
	t.Setenv("LEMONSQUEEZY_WEBHOOK_SECRET", "")
	if _, err := Handler(); err == nil || !strings.Contains(err.Error(), "LEMONSQUEEZY_WEBHOOK_SECRET") {
		t.Fatal(err)
	}
}
