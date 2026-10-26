// Package web serves dahlunas's pages and endpoints.
package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/ardeman/project-dahlunas-go/internal/shop"
)

//go:embed templates/*.html
var templates embed.FS

//go:embed static
var static embed.FS

var pages = template.Must(template.ParseFS(templates, "templates/*.html"))

// Handler returns the routes of the service.
type Options struct {
	TurnstileSiteKey string
	BotVerifier      interface {
		Verify(context.Context, string) (bool, error)
	}
	Shop                        *shop.Service
	ClientKey                   string
	Production                  bool
	Support, RefundID, RefundEN string
}

func Handler(options ...Options) http.Handler {
	var config Options
	if len(options) > 0 {
		config = options[0]
	}
	mux := http.NewServeMux()

	assets, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))

	// Cloud Run reserves some paths ending in z, including /healthz.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	if config.Shop != nil {
		mux.HandleFunc("GET /{$}", withCheckoutForm(config))
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := pages.ExecuteTemplate(w, "home.html", config); err != nil {
				http.Error(w, "page failed", http.StatusInternalServerError)
			}
		})
	}
	registerShop(mux, config)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no-referrer would make browsers post forms with Origin: null, failing the checkout origin check.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}
