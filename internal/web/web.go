// Package web serves dahlunas's pages and endpoints.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html
var templates embed.FS

//go:embed static
var static embed.FS

var pages = template.Must(template.ParseFS(templates, "templates/*.html"))

// Handler returns the routes of the service.
func Handler() http.Handler {
	mux := http.NewServeMux()

	assets, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pages.ExecuteTemplate(w, "home.html", nil); err != nil {
			http.Error(w, "page failed", http.StatusInternalServerError)
		}
	})

	return mux
}
