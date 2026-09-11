package httpapi

// Issue #485: restaurant-menu page redesign. These are pure template/helper
// tests (no DB) locking in the reference layout:
//   * the table number from ?table=N shows in the header and is preserved on
//     the order links;
//   * best-sellers render as 2-column square-image cards;
//   * category items render as list rows (text left, thumbnail right) with a
//     separator between rows;
//   * items without an image render cleanly (no empty image slot);
//   * every user-facing string is localised across the 5 locales.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menu485Fixture builds a two-tier menu: one best-seller with an image, one
// category item with an image, and one category item without an image.
func menu485Fixture(t *testing.T) workspaceMenuData {
	t.Helper()
	lp := &dbstore.LandingPage{
		Enabled: true, Hero: "Hawa Cafe Nadayu 801", Subtitle: "Nadayu 801, Bandar Baru Bangi",
		PrimaryColor: "#4f46e5",
		Menu: &dbstore.MenuPage{
			Enabled:       true,
			BestSellerIDs: []string{"p1", "p2"},
			Categories:    []dbstore.MenuCategory{{Name: "Nasi Lemak", ProductIDs: []string{"p2", "p3"}}},
		},
	}
	h := &PublicHandlers{}
	ls := formLangs["en"]
	// Canonical JomForm media URLs so the #382/#471 variant rewrite applies.
	p1 := &dbstore.Product{ID: "p1", Name: "Nasi Lemak Ayam", PriceSen: 780,
		Images: []string{"https://jomform.com/media/u/acc/11111111-2222-3333-4444-555555555555.jpg"}}
	p2 := &dbstore.Product{ID: "p2", Name: "Nasi Lemak Sambal", Description: "Pedes", PriceSen: 680,
		Images: []string{"https://jomform.com/media/u/acc/11111111-2222-3333-4444-555555555555.jpg"}}
	p3 := &dbstore.Product{ID: "p3", Name: "Nasi Lemak Kosong", Description: "Tanpa lauk", PriceSen: 380}
	return workspaceMenuData{
		Slug: "hawa", Lp: lp, Ls: ls, Table: "8",
		ContactAddress: "Nadayu 801", ContactPhone: "+60123456789",
		BestSellers: []menuProductView{
			h.menuProductView(p1, true, "order", "8", ls.MenuBestBadge),
		},
		Categories: []menuCategoryView{{Name: "Nasi Lemak", Products: []menuProductView{
			h.menuProductView(p2, false, "order", "8", ls.MenuBestBadge),
			h.menuProductView(p3, false, "order", "8", ls.MenuBestBadge),
		}}},
		HasForms: true,
	}
}

func renderMenu485(t *testing.T, data workspaceMenuData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	return buf.String()
}

// The header shows the table number plus cafe name/location and an info
// affordance.
func TestIssue485HeaderShowsTableNumberAndCafe(t *testing.T) {
	body := renderMenu485(t, menu485Fixture(t))
	for _, want := range []string{
		`class="tbl">Table 8`,           // table number header
		"Hawa Cafe Nadayu 801",          // cafe name
		"Nadayu 801, Bandar Baru Bangi", // location line
		`class="info"`,                  // info affordance
		`class="back"`,                  // back arrow
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu header missing %q", want)
		}
	}
}

// A page opened without ?table=N renders no table header.
func TestIssue485NoTableOmitsHeader(t *testing.T) {
	d := menu485Fixture(t)
	d.Table = ""
	body := renderMenu485(t, d)
	if strings.Contains(body, `class="tbl"`) {
		t.Error("table header rendered without ?table=N")
	}
}

// The table number survives on the order links (filter/search round-trip).
func TestIssue485OrderLinksKeepTable(t *testing.T) {
	body := renderMenu485(t, menu485Fixture(t))
	if !strings.Contains(body, `href="/order?table=8"`) {
		t.Errorf("order link did not preserve the table number\nbody:\n%s", body)
	}
}

// Best-sellers are square-image cards in a 2-column grid; category items are
// list rows with a right thumbnail; a no-image item renders no <img> at all.
func TestIssue485TwoTierLayoutAndCleanNoImageRow(t *testing.T) {
	body := renderMenu485(t, menu485Fixture(t))

	// 2-column square cards.
	if !strings.Contains(body, `class="cards"`) {
		t.Error("best-seller section is not a cards grid")
	}
	if !strings.Contains(body, "grid-template-columns:repeat(2,minmax(0,1fr))") {
		t.Error("best-seller grid is not 2-column")
	}
	if !strings.Contains(body, "aspect-ratio:1 / 1") {
		t.Error("best-seller image is not square (1:1)")
	}
	// List rows + separator.
	if !strings.Contains(body, `class="rows"`) || !strings.Contains(body, `class="row"`) {
		t.Error("category section is not a list-row layout")
	}
	if !strings.Contains(body, ".row + .row { border-top:1px solid #e9ebef; }") {
		t.Error("list rows have no separator")
	}
	// Row thumbnail uses the small media variant (#382/#471); card uses medium.
	if !strings.Contains(body, "-thumbnail.jpg") {
		t.Error("row thumbnail did not resolve to the thumbnail media variant")
	}
	if !strings.Contains(body, "-medium.jpg") {
		t.Error("best-seller card did not resolve to the medium media variant")
	}

	// The no-image item ("Nasi Lemak Kosong") must render cleanly: its row
	// carries no <img> and no empty thumbnail slot.
	idx := strings.Index(body, "Nasi Lemak Kosong")
	if idx < 0 {
		t.Fatal("no-image item missing from the menu")
	}
	rowStart := strings.LastIndex(body[:idx], `<a class="row"`)
	rowEnd := strings.Index(body[idx:], "</a>")
	if rowStart < 0 || rowEnd < 0 {
		t.Fatal("could not isolate the no-image row")
	}
	row := body[rowStart : idx+rowEnd]
	if strings.Contains(row, "<img") {
		t.Errorf("no-image row rendered an image slot:\n%s", row)
	}
	if !strings.Contains(row, "Tanpa lauk") || !strings.Contains(row, "RM 3.80") {
		t.Errorf("no-image row missing text/price:\n%s", row)
	}
}

// Every locale carries the full menu dictionary — no blank label can leak.
func TestIssue485MenuStringsInFiveLocales(t *testing.T) {
	fields := map[string]func(langStrings) string{
		"MenuTable":             func(l langStrings) string { return l.MenuTable },
		"MenuInfo":              func(l langStrings) string { return l.MenuInfo },
		"MenuBack":              func(l langStrings) string { return l.MenuBack },
		"MenuBestSeller":        func(l langStrings) string { return l.MenuBestSeller },
		"MenuBestBadge":         func(l langStrings) string { return l.MenuBestBadge },
		"MenuAll":               func(l langStrings) string { return l.MenuAll },
		"MenuSearchPlaceholder": func(l langStrings) string { return l.MenuSearchPlaceholder },
		"MenuFilterLabel":       func(l langStrings) string { return l.MenuFilterLabel },
		"MenuEmpty":             func(l langStrings) string { return l.MenuEmpty },
		"MenuPoweredBy":         func(l langStrings) string { return l.MenuPoweredBy },
	}
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	for code, ls := range formLangs {
		for name, get := range fields {
			if get(ls) == "" {
				t.Errorf("lang %q has empty %s", code, name)
			}
		}
	}
	// A non-English locale must actually localise the menu page.
	if got := langStringsForCode("ms").MenuTable; got != "Meja" {
		t.Errorf("ms MenuTable = %q, want Meja", got)
	}
	if got := langStringsForCode("unknown").MenuTable; got != "Table" {
		t.Errorf("unknown lang did not fall back to en: %q", got)
	}
}

// The page follows the resolved language: Arabic gets dir="rtl" and its own
// labels; the default (English) stays ltr.
func TestIssue485ArabicRendersRTL(t *testing.T) {
	d := menu485Fixture(t)
	d.Ls = formLangs["ar"]
	body := renderMenu485(t, d)
	if !strings.Contains(body, `dir="rtl"`) {
		t.Error("ar menu page did not set dir=rtl")
	}
	if !strings.Contains(body, "طاولة 8") {
		t.Errorf("ar menu page did not localise the table label\nbody:\n%s", body)
	}
	if !strings.Contains(body, "الأكثر مبيعاً") {
		t.Error("ar menu page did not localise the best-seller heading")
	}

	en := renderMenu485(t, menu485Fixture(t))
	if strings.Contains(en, `dir="rtl"`) {
		t.Error("en menu page must not be rtl")
	}
}

// A category filter + search are present; the server-side filter narrows the
// sections so the controls work without JS.
func TestIssue485FilterAndSearchPresentAndServerSide(t *testing.T) {
	d := menu485Fixture(t)
	body := renderMenu485(t, d)
	for _, want := range []string{
		`<select class="catsel"`,    // category filter
		`name="q"`,                  // search input
		`class="mag" type="submit"`, // magnifier submit (no-JS fallback)
	} {
		if !strings.Contains(body, want) {
			t.Errorf("filter row missing %q", want)
		}
	}

	// Server-side search narrows items by name/description.
	filtered := filterMenuItems(d.Categories[0].Products, "pedes")
	if len(filtered) != 1 || filtered[0].ID != "p2" {
		t.Errorf("search filter = %+v, want just p2", filtered)
	}
	if got := filterMenuItems(d.Categories[0].Products, "zzz"); len(got) != 0 {
		t.Errorf("search for a missing term returned %d items", len(got))
	}
}

// BestSellerProductIDs prefers the list, falls back to the legacy single id,
// and drops blanks/duplicates.
func TestIssue485BestSellerIDs(t *testing.T) {
	m := &dbstore.MenuPage{BestSeller: "legacy"}
	if got := m.BestSellerProductIDs(); len(got) != 1 || got[0] != "legacy" {
		t.Errorf("legacy fallback = %v", got)
	}
	m2 := &dbstore.MenuPage{BestSeller: "legacy", BestSellerIDs: []string{"a", "a", "", "b"}}
	got := m2.BestSellerProductIDs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("list dedupe = %v", got)
	}
	if !m2.IsBestSeller("b") || m2.IsBestSeller("legacy") {
		t.Error("IsBestSeller did not prefer the list over the legacy id")
	}
	var nilPage *dbstore.MenuPage
	if nilPage.IsBestSeller("x") || nilPage.BestSellerProductIDs() != nil {
		t.Error("nil MenuPage must be safe")
	}
}
