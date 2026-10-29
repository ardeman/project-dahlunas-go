// Package web serves dahlunas's pages and endpoints.
package web

import (
	"context"
	"embed"
	"fmt"
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
	Shop *shop.Service
	// Catalog lists the products, with prices, while Shop is nil and checkout is closed.
	Catalog                     []shop.Product
	ClientKey                   string
	Production                  bool
	Support, RefundID, RefundEN string
	// TaskToken authorizes Cloud Scheduler's delivery sweep.
	TaskToken string
}

// groups lists the projects on sale, or on show while checkout is closed.
func (o Options) groups() []shop.Group {
	if o.Shop != nil {
		return o.Shop.Groups()
	}
	return shop.Groups(o.Catalog)
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

	// A shop with one project opens on that project's page.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		groups := config.groups()
		if len(groups) == 1 {
			checkoutForm(w, r, config, groups[0])
			return
		}
		p := newPage(r, config)
		render(w, p, "home.html", struct {
			page
			Groups []shop.Group
		}{p, groups})
	})
	mux.HandleFunc("GET /projects/{slug}", func(w http.ResponseWriter, r *http.Request) {
		for _, g := range config.groups() {
			if g.Slug == r.PathValue("slug") {
				checkoutForm(w, r, config, g)
				return
			}
		}
		fail(w, newPage(r, config), "Halaman tidak ditemukan.", "Page not found.", http.StatusNotFound)
	})
	registerShop(mux, config)
	registerTasks(mux, config)
	registerActivations(mux, config)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no-referrer would make browsers post forms with Origin: null, failing the checkout origin check.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}

// CheckImages reports a catalog icon or image missing from static/products,
// so a typo stops startup instead of showing a broken image.
func CheckImages(products []shop.Product) error {
	for _, p := range products {
		for _, name := range []string{p.Icon, p.ProjectImage.ID, p.ProjectImage.EN, p.Image.ID, p.Image.EN} {
			if name == "" {
				continue
			}
			if _, err := fs.Stat(static, "static/products/"+name); err != nil {
				return fmt.Errorf("catalog: image %s is not in internal/web/static/products", name)
			}
		}
	}
	return nil
}
