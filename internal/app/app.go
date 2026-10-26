// Package app assembles the HTTP service from environment configuration.
package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/ardeman/project-dahlunas-go/internal/delivery"
	"github.com/ardeman/project-dahlunas-go/internal/lemonsqueezy"
	"github.com/ardeman/project-dahlunas-go/internal/midtrans"
	"github.com/ardeman/project-dahlunas-go/internal/orderstore"
	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
	"github.com/ardeman/project-dahlunas-go/internal/turnstile"
	"github.com/ardeman/project-dahlunas-go/internal/unlock"
	"github.com/ardeman/project-dahlunas-go/internal/web"
)

func flag(name string) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return false, nil
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return result, nil
}

func Handler() (http.Handler, error) {
	config := web.Options{Support: "mail@ardeman.com", RefundID: os.Getenv("SHOP_REFUND_ID"), RefundEN: os.Getenv("SHOP_REFUND_EN")}
	if support := os.Getenv("SHOP_SUPPORT_EMAIL"); support != "" {
		config.Support = support
	}
	enabled, err := flag("SHOP_ENABLED")
	if err != nil {
		return nil, err
	}
	// Read first: a closed production shop must not show the sandbox band.
	production, err := productionMode()
	if err != nil {
		return nil, err
	}
	config.Production = production
	if !enabled {
		return web.Handler(config), nil
	}
	products := shop.CobaLagi()
	if path := os.Getenv("SHOP_CATALOG_FILE"); path != "" {
		if os.Getenv("SHOP_KEY_ID") != "" {
			return nil, errors.New("set key_id per project in SHOP_CATALOG_FILE; leave SHOP_KEY_ID unset")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("SHOP_CATALOG_FILE cannot be opened")
		}
		products, err = shop.ParseCatalog(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		if err := web.CheckImages(products); err != nil {
			return nil, err
		}
	} else if value := os.Getenv("SHOP_KEY_ID"); value != "" {
		keyID, err := strconv.ParseUint(value, 10, 8)
		if err != nil || keyID == 0 {
			return nil, errors.New("SHOP_KEY_ID must be between 1 and 255")
		}
		for i := range products {
			products[i].KeyID = uint8(keyID)
		}
	}
	required := []string{"SHOP_BASE_URL", "SHOP_REFUND_ID", "SHOP_REFUND_EN", "GOOGLE_CLOUD_PROJECT", "RESEND_API_KEY", "RESEND_FROM"}
	needed := map[string]bool{}
	for _, p := range products {
		needed[p.GatewayName()] = true
	}
	if needed[payment.Midtrans] {
		required = append([]string{"MIDTRANS_SERVER_KEY", "MIDTRANS_CLIENT_KEY"}, required...)
	}
	if needed[payment.LemonSqueezy] {
		required = append(required, "LEMONSQUEEZY_API_KEY", "LEMONSQUEEZY_WEBHOOK_SECRET", "LEMONSQUEEZY_STORE_ID")
	}
	for _, name := range required {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			return nil, fmt.Errorf("%s is required when SHOP_ENABLED=true", name)
		}
	}
	if a, err := mail.ParseAddress(config.Support); err != nil || a.Address != config.Support {
		return nil, errors.New("SHOP_SUPPORT_EMAIL must be an email address")
	}
	if _, err := mail.ParseAddress(os.Getenv("RESEND_FROM")); err != nil || strings.ContainsAny(os.Getenv("RESEND_FROM"), "\r\n") {
		return nil, errors.New("RESEND_FROM must be an email sender")
	}
	base := strings.TrimRight(os.Getenv("SHOP_BASE_URL"), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && !production && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return nil, errors.New("SHOP_BASE_URL must be an HTTPS origin (sandbox also permits HTTP localhost)")
	}
	for _, p := range products {
		if _, err := unlock.KeyFromEnv(p.Project, p.KeyID); err != nil {
			return nil, err
		}
	}
	store, err := orderstore.New(os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("FIRESTORE_EMULATOR_HOST"), production)
	if err != nil {
		return nil, err
	}
	gateways := map[string]payment.Gateway{}
	if os.Getenv("MIDTRANS_SERVER_KEY") != "" {
		gateways[payment.Midtrans] = midtrans.New(os.Getenv("MIDTRANS_SERVER_KEY"), production)
	}
	if needed[payment.LemonSqueezy] || os.Getenv("LEMONSQUEEZY_API_KEY") != "" {
		client, err := lemonsqueezy.New(os.Getenv("LEMONSQUEEZY_API_KEY"), os.Getenv("LEMONSQUEEZY_WEBHOOK_SECRET"), os.Getenv("LEMONSQUEEZY_STORE_ID"), production)
		if err != nil {
			return nil, err
		}
		gateways[payment.LemonSqueezy] = client
	}
	config.Shop = &shop.Service{Store: store, Activations: store, Gateways: gateways, Mailer: delivery.New(os.Getenv("RESEND_API_KEY"), os.Getenv("RESEND_FROM"), config.Support), Products: products, BaseURL: base, SigningKey: unlock.KeyFromEnv}
	siteKey, secret := strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY")), strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY"))
	if (siteKey == "") != (secret == "") || (production && siteKey == "") {
		return nil, errors.New("TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY must both be set; required for production checkout")
	}
	if siteKey != "" {
		config.TurnstileSiteKey = siteKey
		config.BotVerifier = turnstile.New(secret, u.Hostname())
	}
	config.TaskToken = strings.TrimSpace(os.Getenv("DAHLUNAS_TASK_TOKEN"))
	if config.TaskToken != "" && len(config.TaskToken) < 32 {
		return nil, errors.New("DAHLUNAS_TASK_TOKEN must be at least 32 characters")
	}
	config.ClientKey = os.Getenv("MIDTRANS_CLIENT_KEY")
	return web.Handler(config), nil
}

// SHOP_PRODUCTION names the shared environment; retain the original flag for old setups.
func productionMode() (bool, error) {
	legacy, err := flag("MIDTRANS_PRODUCTION")
	if err != nil {
		return false, err
	}
	if os.Getenv("SHOP_PRODUCTION") == "" {
		return legacy, nil
	}
	value, err := flag("SHOP_PRODUCTION")
	if err != nil {
		return false, err
	}
	if os.Getenv("MIDTRANS_PRODUCTION") != "" && legacy != value {
		return false, errors.New("SHOP_PRODUCTION and MIDTRANS_PRODUCTION must agree")
	}
	return value, nil
}
