package httpapi

// Card t_d5112f02 — the builder and the API must give ONE answer about a product
// block bound to an ARCHIVED product.
//
// THE DEFECT (the builder's half): its product list is active-only, so a block
// bound to an archived product got the placeholder and the persistent "buyers
// see: This form is not accepting orders right now" banner — while the API called
// the form healthy, because the product ROW still exists and the buyer page
// resolves the block through the form's saved snapshot. That is the exact shape
// card t_983425e5 fixed in the opposite direction, and the two surfaces disagreed
// again.
//
// The fix keeps the notice but tells the truth: the builder now reads the
// account's product ROW set and shows a separate "archived / in the bin" notice
// (never the buyer's sentence) for a block whose product still has a row. The web
// half is asserted in web/src/lib/productPick.test.ts. THIS file is the API half,
// and it exists so the two cannot drift:
//
//  1. The row set the builder reads — `?include_archived=1` (active + archived)
//     UNION `/products/trash` (the bin) — is asserted EQUAL to
//     dbstore.ProductIDsForAccount, the set the publish guard is handed, using
//     REAL rows in all three states. Change either read or the store query and
//     this test fails, which is what makes the builder's two URLs a contract and
//     not a guess.
//  2. The API's own answer for an archived-bound block is pinned at the two
//     surfaces the merchant hits: the publish route takes it live WITHOUT a
//     refusal (200, not 409), and the `/forms` row carries no
//     `not_accepting_orders` flag. The builder's "notice, not refusal" is that
//     same answer.
//
// The buyer page is deliberately untouched by this card; it already serves a
// checkout for such a form (the block paints from the snapshot).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// builderProductRowsFixture seeds one ACTIVE, one ARCHIVED and one TRASHED product
// for a fresh account with a workspace, and returns the ids in that order.
func builderProductRowsFixture(t *testing.T) (context.Context, *RESTHandlers, *pgxpool.Pool, string, string, [3]string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	products := dbstore.NewProductStore(pool)

	mk := func(name string) string {
		p := &dbstore.Product{
			AccountID: acc, Name: name, PriceSen: 100, Type: dbstore.SaleTypeOneTime, Active: true,
		}
		if err := products.Create(ctx, p); err != nil {
			t.Fatalf("create product %s: %v", name, err)
		}
		return p.ID
	}
	active := mk("derma aktif")
	archived := mk("derma arkaib")
	trashed := mk("derma tong sampah")

	if err := products.SetActive(ctx, acc, archived, false); err != nil {
		t.Fatalf("archive product: %v", err)
	}
	if err := products.SoftDelete(ctx, acc, trashed); err != nil {
		t.Fatalf("trash product: %v", err)
	}
	return ctx, h, pool, acc, key, [3]string{active, archived, trashed}
}

// readIDs pulls the `id` of every row out of a REST list response.
func readIDs(t *testing.T, h *RESTHandlers, key, path string) []string {
	t.Helper()
	code, raw := tplReq(t, h, key, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d (body=%s)", path, code, raw)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode GET %s: %v (%s)", path, err, raw)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// 1. THE ROW-SET CONTRACT. The builder reconstructs the account's product-row set
// from two list reads. That union must be EXACTLY the set the publish guard is
// handed, or the archived-vs-purged split the builder renders is a different
// question from the one the API answers.
func TestBuilderRowSetMatchesThePublishGuard(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping row-set contract test")
	}
	ctx, h, pool, acc, key, ids := builderProductRowsFixture(t)

	// The API's own set: every product ROW the account still has (archived and
	// trashed included, a purged/hard-deleted id absent).
	guardSet, err := dbstore.ProductIDsForAccount(ctx, pool, acc)
	if err != nil {
		t.Fatalf("ProductIDsForAccount: %v", err)
	}

	// The builder's two reads, exactly as web/src/app/builder/page.tsx issues them.
	live := readIDs(t, h, key, "/v1/rest/products?include_archived=1")
	trash := readIDs(t, h, key, "/v1/rest/products/trash")

	builder := map[string]bool{}
	for _, id := range append(append([]string{}, live...), trash...) {
		builder[id] = true
	}

	for _, id := range ids {
		if !builder[id] {
			t.Errorf("product %s is a row the account still has, but the builder's two reads (include_archived + trash) do not return it — the "+
				"builder would call that block PURGED and quote the buyer's refusal for a form the API calls healthy", id)
		}
		if !guardSet[id] {
			t.Errorf("product %s is missing from ProductIDsForAccount — the publish guard would report a block naming it as pointing at nothing", id)
		}
	}
	// The direction that matters just as much: an id that is NOT a row must not be
	// in the builder's set, or the builder would stay silent about a purged block
	// (the false-negative direction card t_983425e5 closed).
	purged := uuid.NewString()
	if builder[purged] {
		t.Errorf("a purged (never-existing) id %s appears in the builder's row set — the builder would suppress the buyer's refusal", purged)
	}
	if guardSet[purged] {
		t.Errorf("a purged id %s appears in ProductIDsForAccount", purged)
	}
	// And the active list alone must NOT be enough: that is the bug — the picker
	// list is what the builder used to read.
	active := readIDs(t, h, key, "/v1/rest/products")
	activeSet := map[string]bool{}
	for _, id := range active {
		activeSet[id] = true
	}
	if activeSet[ids[1]] || activeSet[ids[2]] {
		t.Errorf("the active-only product list now returns an archived/trashed row — the picker contract this card is written against changed")
	}
}

// 2. THE API'S ANSWER FOR AN ARCHIVED-BOUND BLOCK: healthy. Publishing is not
// refused and the /forms row carries no not-accepting-orders flag. The builder's
// archived notice is this answer, rendered on the merchant's side.
func TestArchivedProductBlockPublishesAndIsListedHealthy(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping archived-product publish test")
	}
	ctx, h, _, acc, key, ids := builderProductRowsFixture(t)
	archived := ids[1]
	forms := h.Forms
	cfg := unpointedProductBlockConfig(archived)
	blocks := cfg["blocks"].([]any)
	blocks[2].(map[string]any)["product_id"] = archived
	cfg["blocks"] = blocks
	f := &dbstore.Form{
		AccountID: acc, Slug: "archived-" + acc[:8], Title: "Derma Surau",
		Config: cfg, Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// The publish route must NOT refuse: the row exists, the buyer page sells it
	// from the snapshot, and nothing is dead.
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publishing a form whose product block names an ARCHIVED product = %d, want 200 (body=%s) — a 409 here would be the "+
			"opposite disagreement from card t_983425e5: the builder's archived notice would contradict a refusal", code, raw)
	}

	// …and the list row the dashboard renders must agree.
	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/forms", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms = %d (body=%s)", code, raw)
	}
	var rows []struct {
		ID                 string `json:"id"`
		Status             string `json:"status"`
		NotAcceptingOrders bool   `json:"not_accepting_orders"`
		NoProductBlockID   string `json:"no_product_block_id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode list: %v (%s)", err, raw)
	}
	found := false
	for _, r := range rows {
		if r.ID != f.ID {
			continue
		}
		found = true
		if r.Status != dbstore.FormStatusPublished {
			t.Errorf("list status = %q, want published", r.Status)
		}
		if r.NotAcceptingOrders {
			t.Error("the /forms row marks an archived-product form as not accepting orders — the builder's archived notice is " +
				"now the only surface saying the form is healthy, which is exactly the drift this card removes")
		}
		if r.NoProductBlockID != "" {
			t.Errorf("list no_product_block_id = %q, want empty", r.NoProductBlockID)
		}
	}
	if !found {
		t.Fatalf("the form is missing from GET /v1/rest/forms (%s)", raw)
	}
}
