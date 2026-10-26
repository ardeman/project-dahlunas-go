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
	"time"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

func render(w http.ResponseWriter, p page, name string, data any) {
	p.header(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, p.T("Halaman gagal dimuat", "Page failed"), 500)
	}
}

// fail answers with an error page in the visitor's language, keeping the
// site's navigation.
func fail(w http.ResponseWriter, p page, id, en string, code int) {
	p.header(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	pages.ExecuteTemplate(w, "error.html", struct {
		page
		Message string
	}{p, p.T(id, en)})
}

func registerShop(mux *http.ServeMux, config Options) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, newPage(r, config), "Halaman tidak ditemukan.", "Page not found.", http.StatusNotFound)
	})
	for _, path := range []string{"/terms", "/privacy", "/refunds"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			p := newPage(r, config)
			render(w, p, "policy.html", p)
		})
	}
	if config.Shop == nil {
		return
	}
	s := config.Shop
	limiter := &checkoutLimiter{}
	mux.HandleFunc("POST /checkout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p := newPage(r, config)
		if !limiter.allow(time.Now()) {
			w.Header().Set("Retry-After", "2")
			fail(w, p, "Terlalu banyak permintaan. Coba lagi nanti.", "Too many requests. Try again shortly.", http.StatusTooManyRequests)
			return
		}
		base, _ := url.Parse(s.BaseURL)
		if origin := r.Header.Get("Origin"); origin != "" && origin != base.Scheme+"://"+base.Host {
			http.Error(w, "Forbidden", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			fail(w, p, "Formulir tidak valid.", "Invalid form.", 400)
			return
		}
		cookie, err := r.Cookie(csrfCookie)
		if err != nil || len(cookie.Value) != 64 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.PostForm.Get("csrf"))) != 1 {
			fail(w, p, "Muat ulang halaman.", "Reload the page.", 403)
			return
		}
		if r.PostForm.Get("accept") != "yes" {
			fail(w, p, "Setujui ketentuan.", "Accept the terms.", 400)
			return
		}
		if config.BotVerifier != nil {
			ok, err := config.BotVerifier.Verify(r.Context(), r.PostForm.Get("cf-turnstile-response"))
			if err != nil {
				fail(w, p, "Verifikasi belum tersedia. Coba lagi nanti.", "Verification unavailable. Try again later.", http.StatusServiceUnavailable)
				return
			}
			if !ok {
				fail(w, p, "Muat ulang halaman dan ulangi verifikasi.", "Reload the page and complete verification again.", http.StatusForbidden)
				return
			}
		}
		o, err := s.Create(r.Context(), r.PostForm.Get("product"), strings.TrimSpace(r.PostForm.Get("email")))
		if errors.Is(err, shop.ErrInput) {
			fail(w, p, "Produk atau email tidak valid.", "Invalid product or email.", 400)
			return
		}
		if err != nil {
			fail(w, p, "Pembayaran belum tersedia. Coba lagi nanti.", "Checkout unavailable. Try again later.", 503)
			return
		}
		http.Redirect(w, r, p.Href(o.Path()), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /orders/{id}/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		p := newPage(r, config)
		o, err := s.View(r.Context(), r.PathValue("id"), r.PathValue("token"))
		if errors.Is(err, shop.ErrNotFound) {
			fail(w, p, "Halaman tidak ditemukan.", "Page not found.", http.StatusNotFound)
			return
		}
		if err != nil {
			fail(w, p, "Coba lagi nanti.", "Try again later.", 503)
			return
		}
		render(w, p, "order.html", struct {
			page
			Order shop.Order
		}{p, o})
	})
	for _, gateway := range []string{payment.Midtrans, payment.LemonSqueezy} {
		if !s.HasGateway(gateway) {
			continue
		}
		mux.HandleFunc("POST /webhooks/"+gateway, func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Invalid notification", 400)
				return
			}
			err = s.NotifyGateway(r.Context(), gateway, data, r.Header.Get("X-Signature"))
			if errors.Is(err, payment.ErrIgnored) {
				w.WriteHeader(200)
				return
			}
			if errors.Is(err, payment.ErrSignature) {
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
}

// csrfCookie is __session because Firebase Hosting strips every other cookie
// before a request reaches Cloud Run.
const csrfCookie = "__session"

func checkoutForm(w http.ResponseWriter, r *http.Request, config Options, g shop.Group) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "Unavailable", 503)
		return
	}
	token := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(config.Shop.BaseURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	w.Header().Set("Cache-Control", "no-store")
	p := newPage(r, config)
	render(w, p, "project.html", struct {
		page
		CSRF    string
		Group   shop.Group
		Listing bool
	}{p, token, g, len(config.Shop.Groups()) > 1})
}
