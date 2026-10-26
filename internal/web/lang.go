package web

import (
	"net/http"
	"strconv"
	"strings"
)

// page carries what every template needs besides its own data: the
// configuration and the language the visitor reads.
type page struct {
	Options
	Lang string
	Path string
	// chosen is true when the language came from ?lang, so links keep it.
	// Firebase Hosting forwards no cookie but __session, so the URL holds the choice.
	chosen bool
}

func newPage(r *http.Request, config Options) page {
	p := page{Options: config, Lang: "id", Path: r.URL.Path}
	switch lang := r.URL.Query().Get("lang"); lang {
	case "id", "en":
		p.Lang, p.chosen = lang, true
	default:
		p.Lang = preferred(r.Header.Get("Accept-Language"))
	}
	return p
}

// preferred returns "en" when the header ranks English above Indonesian, else "id".
func preferred(header string) string {
	best, bestQ := "id", 0.0
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		if base == "in" {
			base = "id"
		}
		if (base == "id" || base == "en") && q > bestQ {
			best, bestQ = base, q
		}
	}
	return best
}

// EN reports whether the page is in English.
func (p page) EN() bool { return p.Lang == "en" }

// T picks the Indonesian or English text.
func (p page) T(id, en string) string {
	if p.EN() {
		return en
	}
	return id
}

// Href keeps a chosen language on links to this site.
func (p page) Href(path string) string {
	if !p.chosen {
		return path
	}
	if strings.Contains(path, "?") {
		return path + "&lang=" + p.Lang
	}
	return path + "?lang=" + p.Lang
}

// header marks responses whose language came from Accept-Language.
func (p page) header(w http.ResponseWriter) {
	w.Header().Set("Content-Language", p.Lang)
	if !p.chosen {
		w.Header().Add("Vary", "Accept-Language")
	}
}
