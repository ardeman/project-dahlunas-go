package shop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ardeman/project-dahlunas-go/internal/payment"
	"github.com/ardeman/project-dahlunas-go/internal/unlock"
)

func TestCatalogValidation(t *testing.T) {
	for _, name := range []string{"valid", "duplicate project", "duplicate slug", "zero project", "zero key", "zero product", "zero price", "too expensive", "missing language", "missing instructions", "subject newline", "invalid slug", "no products", "remote image", "half image", "multiline name"} {
		t.Run(name, func(t *testing.T) {
			var projects []ProjectConfig
			json.Unmarshal(defaultCatalog, &projects)
			p := &projects[0]
			switch name {
			case "duplicate project":
				projects = append(projects, *p)
			case "duplicate slug":
				p.Products[1].Slug = p.Products[0].Slug
			case "zero project":
				p.ID = 0
			case "zero key":
				p.KeyID = 0
			case "zero product":
				p.Products[0].ID = 0
			case "zero price":
				p.Products[0].Price = 0
			case "too expensive":
				p.Products[0].Price = 10_000_001
			case "missing language":
				p.Products[0].Description.EN = " "
			case "missing instructions":
				p.Email.Instructions.ID = ""
			case "subject newline":
				p.Email.Subject.EN += "\nBcc: other@example.com"
			case "invalid slug":
				p.Products[0].Slug = "../other"
			case "no products":
				p.Products = nil
			case "remote image":
				p.Icon = "https://example.com/icon.png"
			case "half image":
				p.Products[0].Image = Text{ID: "shot.png"}
			case "multiline name":
				p.Name.EN = "Coba\nLagi"
			}
			b, _ := json.Marshal(projects)
			products, err := ParseCatalog(bytes.NewReader(b))
			if name == "valid" {
				if err != nil || len(products) != 3 {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
	for _, input := range []string{"null", "[]", string(defaultCatalog) + " {}", strings.Replace(string(defaultCatalog), `"key_id"`, `"key_typo"`, 1), strings.Repeat(" ", (1<<20)+1)} {
		if _, err := ParseCatalog(strings.NewReader(input)); err == nil {
			t.Fatal("accepted malformed catalog")
		}
	}
}

func TestProjectOrderKeepsCatalogSnapshot(t *testing.T) {
	var projects []ProjectConfig
	json.Unmarshal(defaultCatalog, &projects)
	second := projects[0]
	second.ID, second.KeyID, second.Slug = 2, 3, "second"
	second.Email.Subject = Text{ID: "Kode proyek kedua", EN: "Second project code"}
	second.Email.Instructions = Text{ID: "Buka pengaturan", EN: "Open settings"}
	second.Products = []ProductConfig{{Slug: "second-project", ID: 7, Price: 9000, Name: Text{ID: "Kedua", EN: "Second"}, Description: Text{ID: "Fitur kedua", EN: "Second features"}}}
	projects = append(projects, second)
	b, _ := json.Marshal(projects)
	products, err := ParseCatalog(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	s, g, _, _ := setup(t)
	s.Products = products
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32))
	s.SigningKey = func(project uint16, id uint8) (ed25519.PrivateKey, error) {
		if project != 2 || id != 3 {
			t.Errorf("wrong signing key %d/%d", project, id)
		}
		return key, nil
	}
	o, err := s.Create(context.Background(), "second-project", "buyer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if g.checkout.Amount != 9000 || g.checkout.Name != "Second" {
		t.Fatal("wrong gateway product")
	}
	// Removing a project must not change an already-created order or its key.
	s.Products = CobaLagi()
	g.status = payment.Status{ID: o.ID, Amount: 9000, State: "settlement", Paid: true}
	if err := s.Notify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.Store.Get(context.Background(), o.ID)
	code, err := unlock.Parse(saved.Code)
	if err != nil || code.Project != 2 || code.Product != 7 || code.KeyID != 3 || saved.Product.DeliveryCopy() != second.Email {
		t.Fatal("order snapshot changed", err)
	}
}

func TestLegacyOrderDeliveryCopy(t *testing.T) {
	p := Product{Project: 1, Product: 1}
	if p.DeliveryCopy() != CobaLagi()[0].Delivery {
		t.Fatal("legacy Coba Lagi copy missing")
	}
	if (Product{Project: 2, Product: 1}).DeliveryCopy() != (EmailCopy{}) {
		t.Fatal("unrelated project got Coba Lagi wording")
	}
}

func TestCatalogGatewayCurrencyPairs(t *testing.T) {
	for _, name := range []string{"USD", "unknown gateway", "USD on Midtrans", "IDR on Lemon", "missing variant", "invalid variant", "too expensive"} {
		t.Run(name, func(t *testing.T) {
			var projects []ProjectConfig
			json.Unmarshal(defaultCatalog, &projects)
			p := &projects[0].Products[0]
			p.Gateway = payment.LemonSqueezy
			p.Currency = "USD"
			p.Price = 599
			p.VariantID = "34"
			switch name {
			case "unknown gateway":
				p.Gateway = "other"
			case "USD on Midtrans":
				p.Gateway = payment.Midtrans
			case "IDR on Lemon":
				p.Currency = "IDR"
			case "missing variant":
				p.VariantID = ""
			case "invalid variant":
				p.VariantID = "../34"
			case "too expensive":
				p.Price = 100001
			}
			b, _ := json.Marshal(projects)
			products, err := ParseCatalog(bytes.NewReader(b))
			if name == "USD" {
				if err != nil || products[0].FormattedPrice() != "US$ 5.99" {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid currency/gateway accepted")
			}
		})
	}
}

func TestGroupsShowSharedDescriptionOnce(t *testing.T) {
	products := CobaLagi()
	other := Product{Slug: "other", Project: 2, DescriptionID: "Lain", DescriptionEN: "Other"}
	groups := Groups(append(products, other))
	if len(groups) != 2 || len(groups[0].Products) != 3 || groups[0].Name.EN != "Coba Lagi" || groups[0].Icon != "coba-lagi-icon.png" || groups[0].Description.ID == "" {
		t.Fatal(groups)
	}
	products[1].DescriptionEN = "Different"
	if g := Groups(products); g[0].Description != (Text{}) {
		t.Fatal("kept a description the products do not share", g[0].Description)
	}
}

func TestGroupsOfferAmountsOnlyForPriceTiers(t *testing.T) {
	products := CobaLagi()
	g := Groups(products)[0]
	if !g.Amounts || g.Slug != "coba-lagi" || g.From().Price != 5000 {
		t.Fatal(g.Amounts, g.Slug, g.From().Price)
	}
	products[2].NameEN = "Coba Lagi patron"
	if Groups(products)[0].Amounts {
		t.Fatal("offered differently named products as amounts")
	}
	if g := Groups([]Product{{Slug: "a", Project: 7, DescriptionID: "x", DescriptionEN: "x"}}); g[0].Slug != "7" {
		t.Fatal("slug should default to the project ID", g[0].Slug)
	}
}

func TestCatalogProjectSlugs(t *testing.T) {
	project := func(id int, slug string) string {
		return fmt.Sprintf(`{"id":%d,"key_id":1,"slug":%q,"tagline":{"id":"Satu","en":"One"},"email":{"subject":{"id":"s","en":"s"},"thank_you":{"id":"t","en":"t"},"instructions":{"id":"i","en":"i"}},"products":[{"slug":"p%d","id":1,"price":5000,"name":{"id":"n","en":"n"},"description":{"id":"d","en":"d"}}]}`, id, slug, id)
	}
	products, err := ParseCatalog(strings.NewReader("[" + project(1, "") + "," + project(2, "bedug") + "]"))
	if err != nil || products[0].ProjectSlug != "1" || products[1].ProjectSlug != "bedug" || products[1].Tagline.EN != "One" {
		t.Fatal(products, err)
	}
	for _, bad := range []string{project(1, "Bad Slug"), project(1, "x") + "," + project(2, "x"), project(1, "2") + "," + project(2, "")} {
		if _, err := ParseCatalog(strings.NewReader("[" + bad + "]")); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestRupiahGroupsThousands(t *testing.T) {
	for price, want := range map[int64]string{5: "Rp 5", 5000: "Rp 5.000", 10_000_000: "Rp 10.000.000"} {
		if got := (Product{Price: price}).FormattedPrice(); got != want {
			t.Fatal(got, want)
		}
	}
}
