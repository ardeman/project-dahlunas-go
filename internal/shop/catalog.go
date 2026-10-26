package shop

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Text contains plain text in both supported languages.
type Text struct {
	ID string `json:"id"`
	EN string `json:"en"`
}

type EmailCopy struct {
	Subject      Text `json:"subject"`
	ThankYou     Text `json:"thank_you"`
	Instructions Text `json:"instructions"`
}

// ProjectConfig's name, icon and image are optional; they group and illustrate
// the project's products on the home page. Activation binds its codes to one
// device through Dahlunas; children's apps never set it.
type ProjectConfig struct {
	ID         uint16          `json:"id"`
	KeyID      uint8           `json:"key_id"`
	Name       Text            `json:"name"`
	Icon       string          `json:"icon,omitempty"`
	Image      Text            `json:"image"`
	Activation bool            `json:"activation,omitempty"`
	Email      EmailCopy       `json:"email"`
	Products   []ProductConfig `json:"products"`
}

type ProductConfig struct {
	Slug        string `json:"slug"`
	ID          uint16 `json:"id"`
	Price       int64  `json:"price"`
	Name        Text   `json:"name"`
	Description Text   `json:"description"`
	Image       Text   `json:"image"`
	Gateway     string `json:"gateway,omitempty"`
	Currency    string `json:"currency,omitempty"`
	VariantID   string `json:"variant_id,omitempty"`
}

//go:embed catalog/coba-lagi.json
var defaultCatalog []byte

// ParseCatalog validates the complete catalog before any checkout is enabled.
// IDs describe offline entitlements; multiple prices may share a product ID.
func ParseCatalog(r io.Reader) ([]Product, error) {
	b, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("catalog: unreadable or larger than 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var projects []ProjectConfig
	if err := d.Decode(&projects); err != nil {
		return nil, errors.New("catalog: invalid JSON or unknown field")
	}
	if d.Decode(new(any)) != io.EOF || len(projects) == 0 {
		return nil, errors.New("catalog: expected a nonempty project array")
	}
	validText := func(t Text) bool {
		return strings.TrimSpace(t.ID) != "" && strings.TrimSpace(t.EN) != "" && !strings.ContainsAny(t.ID+t.EN, "\r\x00")
	}
	projectIDs, slugs := map[uint16]bool{}, map[string]bool{}
	slugPattern := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// Images are files under web/static/products, never third-party URLs that
	// would expose buyers' IP addresses.
	assetPattern := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.(png|jpg|webp|svg)$`)
	optional := func(t Text, valid func(string) bool) bool {
		return t == Text{} || valid(t.ID) && valid(t.EN)
	}
	line := func(s string) bool { return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\r\n\x00") }
	asset := func(name string) bool { return assetPattern.MatchString(name) && len(name) <= 100 }
	var products []Product
	for _, project := range projects {
		if project.ID == 0 || project.KeyID == 0 || projectIDs[project.ID] || len(project.Products) == 0 {
			return nil, errors.New("catalog: project IDs must be unique and nonzero, with a key and products")
		}
		projectIDs[project.ID] = true
		if !optional(project.Name, line) || (project.Icon != "" && !asset(project.Icon)) || !optional(project.Image, asset) {
			return nil, fmt.Errorf("catalog: project %d needs a bilingual one-line name and image/icon file names such as coba-lagi-icon.png", project.ID)
		}
		copy := project.Email
		if !validText(copy.Subject) || strings.ContainsAny(copy.Subject.ID+copy.Subject.EN, "\n") || !validText(copy.ThankYou) || !validText(copy.Instructions) {
			return nil, fmt.Errorf("catalog: project %d needs bilingual email wording", project.ID)
		}
		for _, p := range project.Products {
			if !slugPattern.MatchString(p.Slug) || len(p.Slug) > 100 || slugs[p.Slug] || p.ID == 0 || p.Price <= 0 || p.Price > 10_000_000 || !validText(p.Name) || !validText(p.Description) || !optional(p.Image, asset) {
				return nil, fmt.Errorf("catalog: invalid product in project %d (unique slug, nonzero ID, bilingual text and price 1–10000000 required)", project.ID)
			}
			gateway, currency := p.Gateway, p.Currency
			if gateway == "" {
				gateway = payment.Midtrans
			}
			if currency == "" && gateway == payment.Midtrans {
				currency = "IDR"
			}
			if (gateway != payment.Midtrans && gateway != payment.LemonSqueezy) || (gateway == payment.Midtrans && (currency != "IDR" || p.VariantID != "")) || (gateway == payment.LemonSqueezy && (currency != "USD" || !numericID(p.VariantID) || p.Price > 100_000)) {
				return nil, errors.New("catalog: require Midtrans/IDR or Lemon Squeezy/USD with numeric variant_id (USD limit 100000 cents)")
			}
			slugs[p.Slug] = true
			products = append(products, Product{Slug: p.Slug, NameID: p.Name.ID, NameEN: p.Name.EN, DescriptionID: p.Description.ID, DescriptionEN: p.Description.EN, Price: p.Price, Project: project.ID, Product: p.ID, KeyID: project.KeyID, Delivery: copy, Gateway: gateway, Currency: currency, VariantID: p.VariantID, ProjectName: project.Name, Icon: project.Icon, ProjectImage: project.Image, Image: p.Image, Activation: project.Activation})
		}
	}
	return products, nil
}

// CobaLagi is the default catalog; SHOP_KEY_ID can still rotate its signing key.
func CobaLagi() []Product {
	products, err := ParseCatalog(bytes.NewReader(defaultCatalog))
	if err != nil {
		panic(err)
	}
	return products
}

// DeliveryCopy keeps orders created before catalog configuration readable.
// New orders persist the copy with their product, independent of later edits.
func (p Product) DeliveryCopy() EmailCopy {
	if p.Delivery.Subject.ID == "" && p.Project == 1 && p.Product == 1 {
		return CobaLagi()[0].Delivery
	}
	return p.Delivery
}

func numericID(value string) bool {
	n, err := strconv.ParseUint(value, 10, 63)
	return err == nil && len(value) <= 18 && n > 0 && strconv.FormatUint(n, 10) == value
}
