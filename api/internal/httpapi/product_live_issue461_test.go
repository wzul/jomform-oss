package httpapi

// Issue #461 integration test (real Postgres, skipped unless
// JOMFORM_TEST_DATABASE_URL is set): product display data AND the charged price
// must resolve LIVE from the products table, keyed by product_id — the form
// config snapshot is only the fallback for an archived/missing product.
//
// Covers:
//   - an image uploaded AFTER the form was saved renders on the public form;
//   - a price/name change after the form save shows on the form;
//   - the submit path charges the CURRENT product price (server authority);
//   - an archived product falls back to the snapshot instead of blanking;
//   - pricing blocks (option/shipping) stay snapshot-based.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// issue461Fixture seeds a product whose DB row differs from the form's saved
// snapshot: the product has an image and a NEW name/price, while the snapshot
// has no images and the OLD name/price. Returns the handlers, the workspace
// slug, the form slug, the product id and the option block id.
func issue461Fixture(t *testing.T) (h *PublicHandlers, workspaceSlug, formSlug, prodID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
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

	// Live product: image present, new name + price.
	prod := &dbstore.Product{
		AccountID: accountID, Name: "Nasi Lemak Ayam Basmathi", PriceSen: 2100,
		Type: dbstore.SaleTypeOneTime, Active: true,
		Images: []string{"https://cdn.example/nasi-live.jpg"},
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// Form config holds the STALE snapshot: old name/price, no images. It also
	// carries an option (pricing) block that must stay snapshot-based.
	fs := dbstore.NewFormStore(pool)
	formSlug = "it-461-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Issue 461 form", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"type": "product", "product_id": prod.ID, "value": "Nasi Lemak"},
				map[string]any{"type": "option", "id": "opt-1", "value": "Extra sambal", "price_sen": 500,
					"choices": []any{map[string]any{"label": "Extra sambal", "value": "1", "price_sen": 500}}},
			},
			"products": []any{
				map[string]any{"id": prod.ID, "name": "Nasi Lemak", "price_sen": 1800},
			},
		},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h = &PublicHandlers{
		Forms:      fs,
		Sales:      dbstore.NewSaleStore(pool),
		Accounts:   dbstore.NewAccountStore(pool),
		Products:   dbstore.NewProductStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
		BaseURL:    "http://localhost:8080",
	}
	return h, workspaceSlug, formSlug, prod.ID
}

// issue461Serve routes one request through the public mux on the workspace host.
func issue461Serve(t *testing.T, h *PublicHandlers, host, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	var rdr *bytes.Buffer
	if body == "" {
		rdr = bytes.NewBuffer(nil)
	} else {
		rdr = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = host
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIssue461FormRendersLiveProductImageNameAndPrice(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := issue461Fixture(t)
	host := workspaceSlug + ".jomform.com"

	rec := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// The image uploaded after the form save must render (issue #461 root cause).
	if !strings.Contains(body, `class="pimg" data-size="small" src="https://cdn.example/nasi-live.jpg"`) {
		t.Errorf("product block did not render the LIVE image\nbody:\n%s", body)
	}
	// The live name + price must win over the stale snapshot.
	if !strings.Contains(body, "Nasi Lemak Ayam Basmathi") {
		t.Errorf("product block did not render the LIVE name")
	}
	if !strings.Contains(body, `data-price="2100"`) {
		t.Errorf("product block did not render the LIVE price 2100")
	}
	if strings.Contains(body, `data-price="1800"`) {
		t.Errorf("product block rendered the stale snapshot price 1800")
	}
	// The pricing block stays snapshot-based.
	if !strings.Contains(body, `data-price="500"`) {
		t.Errorf("option block price 500 missing")
	}
	_ = prodID
}

func TestIssue461SubmitChargesLiveProductPrice(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := issue461Fixture(t)
	host := workspaceSlug + ".jomform.com"

	// One live product (2100) + one snapshot pricing block (500) = 2600.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"items":[` +
		`{"product_id":"` + prodID + `","quantity":1},` +
		`{"product_id":"opt-1","quantity":1}],"payment_method":"bank_direct"}`
	rec := issue461Serve(t, h, host, http.MethodPost, "/"+formSlug+"/order", body)
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
	if len(sale.Items) != 2 {
		t.Fatalf("sale items = %d, want 2", len(sale.Items))
	}
	prodItem := sale.Items[0]
	if prodItem.Name != "Nasi Lemak Ayam Basmathi" {
		t.Errorf("charged item name = %q, want the LIVE name", prodItem.Name)
	}
	if prodItem.PriceSen != 2100 {
		t.Errorf("charged price = %d, want the LIVE 2100 (not the snapshot 1800)", prodItem.PriceSen)
	}
	// The pricing block keeps its own snapshot price.
	if sale.Items[1].PriceSen != 500 {
		t.Errorf("option block price = %d, want 500 (snapshot-based)", sale.Items[1].PriceSen)
	}
	if sale.TotalSen != 2600 {
		t.Errorf("sale total = %d, want 2600", sale.TotalSen)
	}
}

func TestIssue461ArchivedProductFallsBackToSnapshot(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := issue461Fixture(t)
	host := workspaceSlug + ".jomform.com"
	ctx := context.Background()

	// Archive the product: it drops out of the live active list, so the form
	// must fall back to the snapshot instead of blanking the block.
	if err := h.Products.Archive(ctx, issue461AccountID(t, h, workspaceSlug), prodID); err != nil {
		t.Fatalf("archive product: %v", err)
	}

	rec := issue461Serve(t, h, host, http.MethodGet, "/"+formSlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Nasi Lemak") {
		t.Errorf("archived product blanked the form; snapshot name missing")
	}
	if !strings.Contains(body, `data-price="1800"`) {
		t.Errorf("archived product did not fall back to the snapshot price 1800")
	}
	// The live-only image is not in the snapshot, so no thumbnail is expected.
	if strings.Contains(body, "nasi-live.jpg") {
		t.Errorf("archived product must not render the live image")
	}
}

// issue461AccountID resolves the account owning the workspace subdomain slug.
func issue461AccountID(t *testing.T, h *PublicHandlers, workspaceSlug string) string {
	t.Helper()
	ws, err := h.Workspaces.IDBySlug(context.Background(), workspaceSlug)
	if err != nil {
		t.Fatalf("workspace by slug: %v", err)
	}
	acc, err := h.Accounts.ByWorkspaceID(context.Background(), ws)
	if err != nil || acc == nil {
		t.Fatalf("account by workspace: %v", err)
	}
	return acc.ID
}
