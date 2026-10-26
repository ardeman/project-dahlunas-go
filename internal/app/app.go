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
	"github.com/ardeman/project-dahlunas-go/internal/midtrans"
	"github.com/ardeman/project-dahlunas-go/internal/orderstore"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
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
	if !enabled {
		return web.Handler(config), nil
	}
	production, err := flag("MIDTRANS_PRODUCTION")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"MIDTRANS_SERVER_KEY", "MIDTRANS_CLIENT_KEY", "SHOP_BASE_URL", "SHOP_REFUND_ID", "SHOP_REFUND_EN", "GOOGLE_CLOUD_PROJECT", "RESEND_API_KEY", "RESEND_FROM"} {
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
	keyID := uint64(1)
	if value := os.Getenv("SHOP_KEY_ID"); value != "" {
		keyID, err = strconv.ParseUint(value, 10, 8)
		if err != nil || keyID == 0 {
			return nil, errors.New("SHOP_KEY_ID must be between 1 and 255")
		}
	}
	if _, err := unlock.KeyFromEnv(1, uint8(keyID)); err != nil {
		return nil, err
	}
	store, err := orderstore.New(os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("FIRESTORE_EMULATOR_HOST"), production)
	if err != nil {
		return nil, err
	}
	products := shop.CobaLagi()
	for i := range products {
		products[i].KeyID = uint8(keyID)
	}
	config.Shop = &shop.Service{Store: store, Gateway: midtrans.New(os.Getenv("MIDTRANS_SERVER_KEY"), production), Mailer: delivery.New(os.Getenv("RESEND_API_KEY"), os.Getenv("RESEND_FROM"), config.Support), Products: products, BaseURL: base, SigningKey: unlock.KeyFromEnv}
	config.Production = production
	config.ClientKey = os.Getenv("MIDTRANS_CLIENT_KEY")
	return web.Handler(config), nil
}
