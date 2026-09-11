package httpapi

// Issue #461 / #438: the restaurant-menu page must render the product's LIVE
// image/name/price straight from the products table (it never used the form
// snapshot — this test locks that in so the menu can't regress to a snapshot).
// Pure template test — no DB.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestIssue461MenuPageRendersLiveProductData(t *testing.T) {
	lp := &dbstore.LandingPage{
		Enabled: true, Hero: "Warung Ali", PrimaryColor: "#4f46e5",
		Menu: &dbstore.MenuPage{
			Enabled:    true,
			BestSeller: "p1",
			Categories: []dbstore.MenuCategory{{Name: "Makanan", ProductIDs: []string{"p1"}}},
		},
	}
	h := &PublicHandlers{}
	p := &dbstore.Product{
		ID: "p1", Name: "Nasi Lemak Ayam Basmathi", Description: "Pedes",
		PriceSen: 2100, Active: true,
		Images: []string{"https://cdn.example/nasi-live.jpg"},
	}
	data := workspaceMenuData{
		Slug: "warung", Lp: lp, Ls: formLangs["en"], Table: "8",
		Categories: []menuCategoryView{{Name: "Makanan", Products: []menuProductView{
			h.menuProductView(p, true, formLangs["en"].MenuBestBadge, formLangs["en"].MenuAdd),
		}}},
		HasForms: true,
	}
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"https://cdn.example/nasi-live.jpg", // live image
		"Nasi Lemak Ayam Basmathi",          // live name
		"RM 21.00",                          // live price
		`data-id="p1"`,                      // add-to-cart target carries the product id (#492)
		`data-price="2100"`,                 // the client price is integer sen (#492)
		"Table 8",                           // table-number header (#485)
		"Best-seller",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu page missing %q\nbody:\n%s", want, body)
		}
	}
}
