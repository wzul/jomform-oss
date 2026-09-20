package httpapi

// Issue #489: QR ordering was broken end-to-end because the order form never
// read ?table=N from the URL — the QR stand (qr.go) and the menu page (#438/
// #485) both carried the param, and the backend accepted table_number, but the
// three order-submitting renderers only echoed ?aff= (#444) and dropped the
// table number. Every QR order therefore landed with an empty
// sales.table_number.
//
// This is a one-sided-wiring guard in the spirit of route_parity_test.go
// (#472): it asserts that every renderer that builds an order POST body echoes
// the table number, so the wiring cannot silently regress again. The
// integration test below proves the echo actually lands on the sale and
// surfaces in the sale detail row and the CSV export.
//
// Renderer assertions are pure template tests (no DB). The submit test is an
// integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// tableEchoMarker is the exact expression every order body must carry: read
// ?table= from the URL and send it as table_number. Matching the full
// expression (not just the word "table") keeps the guard honest — a comment or
// an unrelated mention cannot satisfy it.
const tableEchoMarker = "table_number:(new URLSearchParams(location.search).get('table')||'')"

// tableEchoMarkerSpaced is the same echo in the spaced formatting used by the
// legacy formTmpl (public.go) body.
const tableEchoMarkerSpaced = "table_number: (new URLSearchParams(location.search).get('table') || '')"

// affEchoMarker is the pre-existing #444 affiliate echo the table echo mirrors.
const affEchoMarker = "affiliate_code:(new URLSearchParams(location.search).get('aff')||'')"

// TestIssue489EveryOrderBodyEchoesTableNumber renders each order-submitting
// renderer and asserts its emitted POST body carries the ?table=N echo. The
// four renderers are: the block product form, the response-only block form,
// the legacy form, and the custom-HTML response form.
func TestIssue489EveryOrderBodyEchoesTableNumber(t *testing.T) {
	cases := []struct {
		name string
		body func(t *testing.T) string
	}{
		{"block product form (render_blocks.go)", func(t *testing.T) string {
			var buf bytes.Buffer
			if err := blockTmpl.Execute(&buf, map[string]any{
				"Slug": "demo", "Title": "Demo", "Blocks": []struct{}{},
				"Lang": "en", "Ls": formLangs["en"],
				"HeightJS": template.HTML(iframeHeightJS),
			}); err != nil {
				t.Fatalf("blockTmpl.Execute: %v", err)
			}
			return buf.String()
		}},
		{"response-only block form (render_blocks.go)", func(t *testing.T) string {
			var buf bytes.Buffer
			if err := responseTmpl.Execute(&buf, map[string]any{
				"Slug": "demo", "Title": "Demo", "Fields": []struct{}{},
				"Lang": "en", "Ls": formLangs["en"],
				"HeightJS": template.HTML(iframeHeightJS),
			}); err != nil {
				t.Fatalf("responseTmpl.Execute: %v", err)
			}
			return buf.String()
		}},
		{"legacy product form (public.go)", func(t *testing.T) string {
			var buf bytes.Buffer
			if err := formTmpl.Execute(&buf, struct {
				Slug, Title, Description string
				Products                 []tmplProduct
				OGMeta                   template.HTML
				Footer                   footerInfo
				AntiSpam                 template.HTML
				NoIndex                  bool
				HeightJS                 template.HTML
				// Language placement: <html lang> + translated chrome.
				Lang string
				Ls   langStrings
			}{
				Slug: "demo", Title: "Demo", Description: "desc",
				Products: []tmplProduct{{ID: "p1", Name: "Item", PriceSen: 100}},
				OGMeta:   template.HTML(""),
				Footer:   footerInfo{},
				AntiSpam: template.HTML(antiSpamRenderFields()),
				NoIndex:  true,
				HeightJS: template.HTML(iframeHeightJS),
				Lang:     "en",
				Ls:       langStringsForCode("en"),
			}); err != nil {
				t.Fatalf("formTmpl.Execute: %v", err)
			}
			return buf.String()
		}},
		{"custom-HTML response form (render_custom.go)", func(t *testing.T) string {
			var buf bytes.Buffer
			if err := customFormTmpl.Execute(&buf, map[string]any{
				"Slug": "demo", "Title": "Demo",
				"BodyJS": template.JS(`"<form></form>"`),
				"Lang":   "en", "Ls": formLangs["en"],
				"AntiSpam": template.HTML(antiSpamRenderFields()),
				"HeightJS": template.HTML(iframeHeightJS),
			}); err != nil {
				t.Fatalf("customFormTmpl.Execute: %v", err)
			}
			return buf.String()
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := c.body(t)
			if !strings.Contains(body, tableEchoMarker) && !strings.Contains(body, tableEchoMarkerSpaced) {
				t.Errorf("%s does not echo ?table=N into the order body — a QR order would land with an empty table_number (issue #489).\nwant either:\n  %s\n  %s",
					c.name, tableEchoMarker, tableEchoMarkerSpaced)
			}
		})
	}
}

// TestIssue489TableEchoMirrorsAffiliateEcho keeps the two URL echoes from
// drifting apart: every renderer that echoes ?aff= must also echo ?table=.
// This is the property that made the bug possible — the affiliate echo was
// added to all three bodies and the table echo to none.
func TestIssue489TableEchoMirrorsAffiliateEcho(t *testing.T) {
	sources := map[string]string{}
	for name, tmpl := range map[string]*template.Template{
		"blockTmpl":      blockTmpl,
		"responseTmpl":   responseTmpl,
		"formTmpl":       formTmpl,
		"customFormTmpl": customFormTmpl,
	} {
		sources[name] = tmpl.Tree.Root.String()
	}
	for name, src := range sources {
		hasAff := strings.Contains(src, "get('aff')")
		hasTable := strings.Contains(src, "get('table')")
		if hasAff && !hasTable {
			t.Errorf("%s echoes ?aff= but not ?table= — the one-sided wiring that caused #489", name)
		}
	}
}

// tableNumberFixture seeds a published payment form with one product and
// returns the handlers plus the workspace host, form slug and product id.
func tableNumberFixture(t *testing.T) (*PublicHandlers, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak", PriceSen: 800, Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-tbl-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "QR form", Status: "published",
		Config: map[string]any{
			"blocks":   []any{map[string]any{"type": "product", "product_id": prod.ID}},
			"products": []any{map[string]any{"id": prod.ID, "name": "Nasi Lemak", "price_sen": 800}},
		},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:      fs,
		Sales:      dbstore.NewSaleStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug, prod.ID
}

// TestIssue489SubmitPersistsAndSurfacesTableNumber is the end-to-end guard:
// an order submitted with table_number=8 (the value the client JS now reads
// from ?table=8) must persist on the sale and surface in the sale detail row
// and the CSV export.
func TestIssue489SubmitPersistsAndSurfacesTableNumber(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := tableNumberFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","table_number":"8"}`

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}

	// 1. The sale row carries the table number.
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TableNumber != "8" {
		t.Fatalf("sale.table_number = %q, want \"8\" (issue #489)", sale.TableNumber)
	}

	// 2. The sale detail row (the REST shape the Sales page reads) surfaces it.
	row := toSaleRow(sale, "")
	if row.TableNumber != "8" {
		t.Errorf("sale detail table_number = %q, want \"8\"", row.TableNumber)
	}

	// 3. The CSV export surfaces it.
	var csvBuf bytes.Buffer
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/v1/rest/sales/export.csv", nil)
	rest := &RESTHandlers{Sales: h.Sales}
	rest.exportSalesCSV(rec2, req2, &AccountCtx{Account: &dbstore.Account{ID: sale.AccountID, Slug: "it"}})
	csvBuf.Write(rec2.Body.Bytes())
	csvOut := csvBuf.String()
	// Issue #518 appended refund columns after table_number, so the old
	// positional "ends with ,8" substring is no longer a valid assertion.
	// Resolve the column by header name and read the same index from the data
	// row — this keeps the check honest (a shifted row still fails) without
	// pinning table_number to last place.
	recs, err := csv.NewReader(strings.NewReader(csvOut)).ReadAll()
	if err != nil || len(recs) < 2 {
		t.Fatalf("CSV export unreadable (%d rows, err %v):\n%s", len(recs), err, csvOut)
	}
	col := -1
	for i, name := range recs[0] {
		if name == "table_number" {
			col = i
		}
	}
	if col < 0 {
		t.Fatalf("CSV export header missing table_number column:\n%s", csvOut)
	}
	if len(recs[1]) != len(recs[0]) {
		t.Fatalf("CSV export row has %d fields, header has %d — columns are misaligned:\n%s", len(recs[1]), len(recs[0]), csvOut)
	}
	if recs[1][col] != "8" {
		t.Errorf("CSV export table_number = %q, want \"8\":\n%s", recs[1][col], csvOut)
	}
}

// TestIssue489SubmitWithoutTableLeavesItEmpty is the negative control: an order
// submitted with no table number (a plain online order) must not invent one.
func TestIssue489SubmitWithoutTableLeavesItEmpty(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := tableNumberFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"}`

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	if sale.TableNumber != "" {
		t.Errorf("sale.table_number = %q, want empty for an order without ?table=", sale.TableNumber)
	}
}
