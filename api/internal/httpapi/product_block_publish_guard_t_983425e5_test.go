package httpapi

// Card t_983425e5 — the merchant's half of the "This form is not accepting
// orders right now" defect.
//
// THE DEFECT, measured on prod (form e1fcc6cc-218d-4075-90b0-b2b8e03944c6,
// workspace `test-wan`): a PUBLISHED payment form whose product block was
// `{"id":"q5uhvoy","type":"product","value":"","quantity":true}` — no
// `product_id`. The buyer page was correct (it served the submit-less message
// page), but nothing told the merchant: the publish answered 200 and /forms
// showed a clean "Published" badge. Wan, mid-demo: "padahal aku dh publish form
// tu."
//
// This file measures both halves of the fix at the surfaces the merchant and the
// buyer actually hit:
//
//  1. the BUYER page is unchanged — the published form still serves the honest
//     "not accepting orders" page (the fix is on the merchant side, and the card
//     forbids touching this arm);
//  2. the PUBLISH path now refuses to go live silently and names the block
//     (`code:no_product_selected`, `block_id`) — and the acknowledgement still
//     exists, because a merchant may publish while the catalogue is being set
//     up;
//  3. the LIST that the dashboard renders carries `not_accepting_orders` for the
//     same form, so a published-but-dead row is visible without opening it;
//  4. the SIBLING case — the block names a product that was later purged — is the
//     same signal, while a block naming a product that still has a row (archived
//     or trashed) is not reported, because the renderer and the order path fall
//     back to the form's snapshot for it.
//
// The render arms are unit tests; the publish/list arms are integration tests
// and skip without JOMFORM_TEST_DATABASE_URL (scripts/preflight.sh fails loudly
// if it is unset).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// unpointedProductBlockConfig is the measured prod config: the merchant's fields,
// a product block that was never pointed at a product, and the workspace
// catalogue snapshotted in `products` (which is what made the old write-path rule
// answer "sellable").
func unpointedProductBlockConfig(productID string) map[string]any {
	return map[string]any{
		"mode": "payment",
		"blocks": []any{
			map[string]any{"id": "nm1", "type": "text", "value": "Nama Penuh"},
			map[string]any{"id": "em1", "type": "email", "value": "Emel"},
			map[string]any{"id": "q5uhvoy", "type": "product", "value": "", "quantity": true},
		},
		"products": []any{map[string]any{
			"id": productID, "name": "derma", "price_sen": float64(100),
		}},
	}
}

// 1. THE BUYER PAGE IS UNCHANGED. A published payment form whose product block
// carries no product id still serves the submit-less message page — it is the
// honest answer, and the card explicitly keeps it.
func TestPublishedUnpointedProductBlockStillServesTheHonestBuyerPage(t *testing.T) {
	f := &dbstore.Form{
		ID: "f_unpointed", AccountID: "acc_unpointed", Slug: "derma-surau",
		Title: "Derma Surau", Status: dbstore.FormStatusPublished,
		Config: unpointedProductBlockConfig("fdf8af0c-abc6-430a-885c-775342628bc2"),
	}
	body, rec := serveFormBody(t, f)
	if rec.Code != http.StatusOK {
		t.Fatalf("serveForm status = %d, want 200 (body: %s)", rec.Code, body)
	}
	if !strings.Contains(body, formLangs["en"].NoProductsMessage) {
		t.Errorf("the buyer page no longer carries %q — the merchant-side fix must not change what the "+
			"buyer is told; page: %s", formLangs["en"].NoProductsMessage, body)
	}
	for _, banned := range []struct{ what, marker string }{
		{"the block payment CTA", "Proceed to Payment"},
		{"a product checkbox", `class="p"`},
		{"the block submit handler's line-item gate", "if(!items.length)"},
	} {
		if strings.Contains(body, banned.marker) {
			t.Errorf("an unpointed product block was served a checkout: page carries %s (%q)", banned.what, banned.marker)
		}
	}
}

// guardFixture creates an account ready to publish, its API-keyed handlers and a
// real product, and returns them. One product row is enough: the point of the
// fixture is that the form SNAPSHOTS a sellable product while its block points
// at none.
func guardFixture(t *testing.T) (context.Context, *RESTHandlers, *dbstore.FormStore, string, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	bundleLinkWorkspace(t, ctx, pool, acc)
	key := bundleAPIKey(t, ctx, pool, acc)
	h := newBundleHandlers(pool)
	forms := dbstore.NewFormStore(pool)
	prod := &dbstore.Product{
		AccountID: acc, Name: "derma", PriceSen: 100, Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	return ctx, h, forms, acc, key, prod.ID
}

// 2. THE PUBLISH PATH REFUSES TO GO LIVE SILENTLY, and says which block to fix.
func TestPublishRefusesAnUnpointedProductBlockAndNamesTheBlock(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-guard integration test")
	}
	ctx, h, forms, acc, key, prodID := guardFixture(t)

	f := &dbstore.Form{
		AccountID: acc, Slug: "unpointed-" + acc[:8], Title: "Derma Surau",
		Config: unpointedProductBlockConfig(prodID), Theme: map[string]any{},
		Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/publish", "")
	if code != http.StatusConflict {
		t.Fatalf("publishing a payment form whose product block is unpointed = %d, want 409 "+
			"(body=%s) — 200 here is the defect: it goes live and the buyer is told the form is not "+
			"accepting orders", code, raw)
	}
	var refused map[string]any
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, raw)
	}
	if refused["code"] != dbstore.PublishWarningNoProductSelected {
		t.Errorf("refusal code = %v, want %q", refused["code"], dbstore.PublishWarningNoProductSelected)
	}
	if refused["requires_ack"] != true {
		t.Errorf("refusal requires_ack = %v, want true (the merchant may publish while the catalogue is being set up)", refused["requires_ack"])
	}
	if refused["block_id"] != "q5uhvoy" {
		t.Errorf("refusal block_id = %v, want q5uhvoy — the builder links straight to that block", refused["block_id"])
	}
	if msg, _ := refused["error"].(string); !strings.Contains(msg, "not accepting orders") {
		t.Errorf("refusal prose does not quote the buyer's sentence: %q", msg)
	}
	// A refusal changes nothing.
	after, err := forms.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("re-read form: %v", err)
	}
	if after.Status != dbstore.FormStatusDraft {
		t.Errorf("status after refusal = %q, want draft", after.Status)
	}

	// 3. THE DASHBOARD SIGNAL: even once the merchant acknowledges and the row
	// says "published", the list marks it as not accepting orders.
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+f.ID+"/publish", `{"acknowledge":true}`); code != http.StatusOK {
		t.Fatalf("acknowledged publish = %d, want 200 (body=%s)", code, raw)
	}
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/forms", "")
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
	for _, row := range rows {
		if row.ID != f.ID {
			continue
		}
		found = true
		if row.Status != dbstore.FormStatusPublished {
			t.Errorf("list status = %q, want published (the row IS live; the badge is what must warn)", row.Status)
		}
		if !row.NotAcceptingOrders {
			t.Error("a published payment form whose buyer page says \"not accepting orders\" is listed as fine — " +
				"this is the clean \"Published\" badge the card is about")
		}
		if row.NoProductBlockID != "q5uhvoy" {
			t.Errorf("list no_product_block_id = %q, want q5uhvoy", row.NoProductBlockID)
		}
	}
	if !found {
		t.Fatalf("the form is missing from GET /v1/rest/forms (%s)", raw)
	}

	// The single-form GET carries the same two fields, so the builder and the
	// list cannot disagree.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/forms/"+f.ID, "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/forms/{id} = %d (body=%s)", code, raw)
	}
	var one struct {
		NotAcceptingOrders bool   `json:"not_accepting_orders"`
		NoProductBlockID   string `json:"no_product_block_id"`
	}
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatalf("decode single form: %v (%s)", err, raw)
	}
	if !one.NotAcceptingOrders || one.NoProductBlockID != "q5uhvoy" {
		t.Errorf("single form payload = %+v, want not_accepting_orders + block q5uhvoy", one)
	}
}

// 4. THE SIBLING CASE: a block naming a PURGED product is the same signal; a
// block naming an existing row (archived/trashed, or live) is not.
func TestProductBlockPointingAtAPurgedProductIsTheSameSignal(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping publish-guard integration test")
	}
	ctx, h, forms, acc, key, prodID := guardFixture(t)

	// Control: a block naming a product row that EXISTS publishes bare — the
	// guard must not accuse a healthy form (which is what makes the refusal
	// below about absence rather than about product blocks in general).
	healthy := &dbstore.Form{
		AccountID: acc, Slug: "healthy-" + acc[:8], Title: "Derma Surau",
		Config: unpointedProductBlockConfig(prodID), Theme: map[string]any{},
		Status: dbstore.FormStatusDraft,
	}
	blocks := healthy.Config["blocks"].([]any)
	blocks[2].(map[string]any)["product_id"] = prodID
	if err := forms.Create(ctx, healthy); err != nil {
		t.Fatalf("create healthy form: %v", err)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+healthy.ID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publishing a form whose product block names a live product = %d, want 200 (body=%s)", code, raw)
	}

	// The sibling: the row was purged, so the id resolves to nothing. The config
	// still carries it — only the account's product-row set can see the hole.
	purged := &dbstore.Form{
		AccountID: acc, Slug: "purged-" + acc[:8], Title: "Derma Surau (purged)",
		Config: unpointedProductBlockConfig(uuid.NewString()), Theme: map[string]any{},
		Status: dbstore.FormStatusDraft,
	}
	purgedBlocks := purged.Config["blocks"].([]any)
	purgedBlocks[2].(map[string]any)["product_id"] = uuid.NewString()
	if err := forms.Create(ctx, purged); err != nil {
		t.Fatalf("create purged form: %v", err)
	}
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+purged.ID+"/publish", "")
	if code != http.StatusConflict {
		t.Fatalf("publishing a form whose product block names a PURGED product = %d, want 409 (body=%s)", code, raw)
	}
	var refused map[string]any
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, raw)
	}
	if refused["code"] != dbstore.PublishWarningNoProductSelected {
		t.Errorf("purged-product refusal code = %v, want %q (the same signal as an empty block)",
			refused["code"], dbstore.PublishWarningNoProductSelected)
	}
}
