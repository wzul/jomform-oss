package menufixture_test

// Card t_2244109a — the non-prod menu-checkout fixture must REALLY render an
// option group, and this test is what keeps that claim true.
//
// Two QA cards could not observe a radio/image-choice group on a deployment
// because no live form carries one, and each improvised a throwaway probe. This
// package is the probe kept, and these arms pin the two things a later lane
// depends on:
//
//   - the served page, from the REAL public handler over a real HTTP listener and
//     real Postgres rows, carries BOTH group kinds — each as a radio group named
//     by its block id, inside the checkout sheet's `.sheetfoot`. If the fixture
//     ever stopped producing them (a template arm removed, the attached order
//     form detached by a rename, a store signature changed), this test goes red
//     and the next QA card is not handed a page with nothing to measure.
//   - the order route accepts an answer from those groups and STORES it, and
//     refuses one outside them. So the fixture's checkout is wired, not just
//     painted.
//
// It does not do layout: a Go process has no layout engine. The geometry
// (13x13 control, 8px gap, full-width buyer fields, no sideways scroll at 1200px
// and 375px) is measured in Chromium by
// e2e/specs/menu-checkout-option-group.spec.ts, which runs against this same
// fixture through scripts/menu-fixture-e2e.sh.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set, like every integration test in
// this repo. The `test` gate refuses to run without it, so it cannot skip
// silently there.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/menufixture"
)

// openFixture starts a real HTTP server in front of the seeded handler and
// returns the fixture plus the origin to reach it on.
func openFixture(t *testing.T) (*menufixture.Fixture, string) {
	t.Helper()
	f, err := menufixture.Open(context.Background(), menufixture.Options{})
	if err != nil {
		if err == menufixture.ErrNoDSN {
			t.Skipf("%v", err)
		}
		t.Fatalf("open the menu fixture: %v", err)
	}
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(func() {
		srv.Close()
		if cerr := f.Close(context.Background()); cerr != nil {
			t.Errorf("close the fixture (the scratch schema must not be left behind): %v", cerr)
		}
	})
	f.SetBaseURL(srv.URL)
	return f, srv.URL
}

// servedPage fetches the menu page over real HTTP, with NO Host header of its
// own — which is the whole point of the fixture's Host pinning: a QA browser
// loads http://127.0.0.1:PORT/<slug> and the handler still resolves the tenant.
func servedPage(t *testing.T, origin string, f *menufixture.Fixture) string {
	t.Helper()
	resp, err := http.Get(origin + f.MenuPath())
	if err != nil {
		t.Fatalf("GET %s%s: %v", origin, f.MenuPath(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the served page: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 — the fixture does not serve a menu page\nbody:\n%s",
			f.MenuPath(), resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	return string(body)
}

func parse(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse the served HTML: %v", err)
	}
	return doc
}

func count(doc *html.Node, selector string) int {
	return len(cascadia.QueryAll(doc, cascadia.MustCompile(selector)))
}

// TestServedMenuCarriesBothOptionGroups is the card: one radio group AND one
// image-choice group, on the page the fixture serves, in the checkout sheet.
func TestServedMenuCarriesBothOptionGroups(t *testing.T) {
	f, origin := openFixture(t)
	body := servedPage(t, origin, f)
	doc := parse(t, body)

	// The menu itself renders: one add-to-cart row per seeded product.
	items := count(doc, `button.row[type=button][data-id]`)
	if items != len(f.ProductIDs) {
		t.Errorf("the served page has %d add-to-cart rows, want %d (one per seeded product)", items, len(f.ProductIDs))
	}

	// Both groups are inside the checkout sheet (.sheetfoot), not loose in the page.
	if got := count(doc, `.sheetfoot .radioswrap`); got != 2 {
		t.Errorf("the checkout sheet carries %d option groups, want 2 (a radio group and an image-choice group)", got)
	}

	// Each group is one radio per declared choice, named by its block id — the
	// key the checkout submits and the order route validates. The two groups must
	// NOT share a name: sharing one is card t_5d625806, where two questions became
	// a single HTML radio group and answering the second cleared the first.
	radio := count(doc, `.sheetfoot input[type=radio][name="`+f.RadioBlock+`"]`)
	if want := len(menufixture.RadioChoices); radio != want {
		t.Errorf("the radio group %q has %d radios, want %d — the buyer cannot pick a size", f.RadioBlock, radio, want)
	}
	imageChoice := count(doc, `.sheetfoot input[type=radio][name="`+f.ImageChoiceBlock+`"]`)
	if want := len(menufixture.ImageChoices); imageChoice != want {
		t.Errorf("the image-choice group %q has %d radios, want %d — an image-choice block that is not a radio "+
			"group is the free-text fallback card t_efd99d75 fixed", f.ImageChoiceBlock, imageChoice, want)
	}
	if f.RadioBlock == f.ImageChoiceBlock {
		t.Errorf("both groups are named %q — two questions in one HTML radio group", f.RadioBlock)
	}
	if stray := count(doc, `.sheetfoot input[type=text][name="`+f.ImageChoiceBlock+`"]`); stray != 0 {
		t.Errorf("the image-choice block also renders %d free-text input(s) — the option group is not a text box", stray)
	}

	// The labels the merchant set are what the buyer reads (verbatim, so a
	// localisation sweep cannot silently drop the question).
	for _, want := range []string{"Pilih saiz", "Pilihan suhu", "Kecil", "Besar", "Panas", "Sejuk"} {
		if !strings.Contains(body, want) {
			t.Errorf("the served page never shows %q — the option labels are the merchant's, not the fixture's", want)
		}
	}

	// The buyer fields the sheet collects alongside them.
	for _, sel := range []string{`input#menuName[name=full_name]`, `input#menuEmail[name=email]`, `#menuPay`} {
		if count(doc, sel) != 1 {
			t.Errorf("the checkout sheet is missing %q — the order cannot be submitted", sel)
		}
	}

	// The rule that keeps the option control intrinsic (card t_664549fc). Its
	// CASCADE is pinned in httpapi/menu_radio_size_t_664549fc_test.go; here the
	// point is only that the fixture's page still serves the rule the geometry
	// spec measures against.
	want := `.sheetfoot .radios input[type=radio] { width:auto; height:auto;`
	if !strings.Contains(compactCSS(body), want) {
		t.Errorf("the served stylesheet no longer carries `%s` — the option control would fill the row and push "+
			"the merchant's label to its edge", want)
	}
}

// TestFixtureOrderStoresOptionAnswers proves the checkout is WIRED, not painted:
// a real POST to the menu form's own order route, with both answers, is accepted
// (201, bank_direct so nothing leaves the box), the money is integer sen, and the
// answers land on the sale. An answer outside the group is refused, so the page
// and the server contract are the same contract.
func TestFixtureOrderStoresOptionAnswers(t *testing.T) {
	f, origin := openFixture(t)
	// The page must be reachable first: a submit is only meaningful against a
	// checkout that really rendered.
	_ = servedPage(t, origin, f)

	post := func(fields string) *http.Response {
		t.Helper()
		body := `{"client":{"full_name":"Ali Buyer","email":"ali.buyer@example.test"},` +
			`"items":[{"product_id":"` + f.ProductIDs[0] + `","variation_id":"","quantity":1}],` +
			`"payment_method":"bank_direct","table_number":"","affiliate_code":"",` +
			`"fields":` + fields + `}`
		resp, err := http.Post(origin+"/"+f.MenuSlug+"/order", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("POST the order: %v", err)
		}
		return resp
	}

	resp := post(`{"website":"","` + f.RadioBlock + `":"b","` + f.ImageChoiceBlock + `":"panas"}`)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("order with both option answers = %d, want 201 (bank_direct)\nbody:\n%s", resp.StatusCode, raw)
	}
	var ok struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(raw, &ok); err != nil || ok.RefCode == "" {
		t.Fatalf("decode the order answer (%v): %s", err, raw)
	}
	fields, err := f.SaleFieldsByRef(context.Background(), ok.RefCode)
	if err != nil {
		t.Fatalf("read the sale's stored answers: %v", err)
	}
	if got := fields[f.RadioBlock]; got != "b" {
		t.Errorf("stored fields[%s] = %v, want the chosen size \"b\" (all fields: %v)", f.RadioBlock, got, fields)
	}
	if got := fields[f.ImageChoiceBlock]; got != "panas" {
		t.Errorf("stored fields[%s] = %v, want the chosen temperature \"panas\" (all fields: %v)", f.ImageChoiceBlock, got, fields)
	}
	// Money is integer sen, and it is the PRODUCT's price — the menu charges the
	// live row, not a snapshot on the form.
	sale, err := f.SaleByRef(context.Background(), ok.RefCode)
	if err != nil {
		t.Fatalf("read the sale: %v", err)
	}
	if sale.TotalSen != 850 {
		t.Errorf("sale total_sen = %d, want 850 (the seeded Nasi Lemak price, integer sen)", sale.TotalSen)
	}

	// An answer outside the declared choices is refused, not stored: the fixture
	// is the same contract the deployment enforces.
	resp2 := post(`{"website":"","` + f.RadioBlock + `":"xxl"}`)
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode == http.StatusCreated {
		t.Errorf("an answer outside the radio group was ACCEPTED (201): %s — the checkout no longer validates "+
			"against the merchant's choices", body2)
	}
}

// compactCSS collapses the served markup's whitespace runs so a rule can be
// found the way it was authored, not the way the template indented it.
func compactCSS(body string) string {
	return strings.Join(strings.Fields(body), " ")
}
