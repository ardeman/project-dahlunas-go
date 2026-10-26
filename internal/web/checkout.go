package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ardeman/project-dahlunas-go/internal/midtrans"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Halaman gagal dimuat / Page failed", 500)
	}
}

func registerShop(mux *http.ServeMux, config Options) {
	for _, path := range []string{"/terms", "/privacy", "/refunds"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			render(w, "policy.html", struct {
				Options
				Path string
			}{config, r.URL.Path})
		})
	}
	if config.Shop == nil {
		return
	}
	s := config.Shop
	mux.HandleFunc("POST /checkout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		base, _ := url.Parse(s.BaseURL)
		if origin := r.Header.Get("Origin"); origin != "" && origin != base.Scheme+"://"+base.Host {
			http.Error(w, "Forbidden", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Formulir tidak valid / Invalid form", 400)
			return
		}
		cookie, err := r.Cookie("checkout_csrf")
		if err != nil || len(cookie.Value) != 64 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.PostForm.Get("csrf"))) != 1 {
			http.Error(w, "Muat ulang halaman / Reload the page", 403)
			return
		}
		if r.PostForm.Get("accept") != "yes" {
			http.Error(w, "Setujui ketentuan / Accept the terms", 400)
			return
		}
		o, err := s.Create(r.Context(), r.PostForm.Get("product"), strings.TrimSpace(r.PostForm.Get("email")))
		if errors.Is(err, shop.ErrInput) {
			http.Error(w, "Produk atau email tidak valid / Invalid product or email", 400)
			return
		}
		if err != nil {
			http.Error(w, "Pembayaran belum tersedia. Coba lagi nanti. / Checkout unavailable. Try again later.", 503)
			return
		}
		http.Redirect(w, r, o.Path(), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /orders/{id}/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		o, err := s.View(r.Context(), r.PathValue("id"), r.PathValue("token"))
		if errors.Is(err, shop.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Coba lagi nanti / Try again later", 503)
			return
		}
		render(w, "order.html", struct {
			Options
			Order shop.Order
		}{config, o})
	})
	mux.HandleFunc("POST /webhooks/midtrans", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Invalid notification", 400)
			return
		}
		err = s.Notify(r.Context(), data)
		if errors.Is(err, midtrans.ErrSignature) {
			http.Error(w, "Invalid signature", 401)
			return
		}
		if err != nil {
			http.Error(w, "Retry notification", 503)
			return
		}
		w.Write([]byte("ok"))
	})
}

func withCheckoutForm(config Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "Unavailable", 503)
			return
		}
		token := hex.EncodeToString(b)
		http.SetCookie(w, &http.Cookie{Name: "checkout_csrf", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(config.Shop.BaseURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 3600})
		w.Header().Set("Cache-Control", "no-store")
		render(w, "home.html", struct {
			Options
			CSRF string
		}{config, token})
	}
}
