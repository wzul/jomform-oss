package httpapi

// REST transport for the PRODUCT trash: list / restore / purge.
//
// The store behaviour is pinned in dbstore's product_trash_test.go and the media
// half of the same feature in this package's media_trash_test.go. THIS file
// covers the third thing a store test cannot see: the route table and the
// handler bodies. Three routes were added by the product-bin change and, until
// this file, NOTHING in the package reached them — no test named the handlers,
// and none requested the route literals.
//
// What the store tests provably cannot cover, and why each assertion below is on
// the wire rather than on the store:
//
//	the route ORDER — GET /v1/rest/products/trash is registered next to
//	  GET /v1/rest/products/{id}. That the literal wins is a runtime property of
//	  net/http's pattern matcher, not of this code, so only an actual request can
//	  assert it. If it lost, the listing would be answered by getProduct with
//	  id="trash" and come back as a 404 about a malformed uuid.
//	the CONFIRMATION — purgeProduct compares a caller-supplied `confirm_name`
//	  against the trashed row's name. That comparison lives in the handler, is
//	  the only thing standing between a mistyped request and an irreversible
//	  delete, and a store test calling Purge directly never exercises it.
//	the two 409 BODIES — the refusal's `code` and its obstacle NUMBER are
//	  assembled in the handler from a re-queried count. The dashboard reads the
//	  code to render its own sentence and the number to state the obstacle, so a
//	  dropped field is a silent wire-contract change that only shows up as a
//	  generic message in the UI.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
// Requests go through the real RegisterRoutes mux with a real Bearer key, and
// every assertion on a status is paired with one on the ROW: a 200 that purged
// nothing, or a 409 that destroyed the product anyway, both fail here.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// productBinFixture seeds a user + account, wires the REST handlers over the
// test pool and returns a readwrite Bearer key for the account.
func productBinFixture(t *testing.T) (*RESTHandlers, *pgxpool.Pool, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	return h, pool, accountID, scopeTestKey(t, pool, accountID, "prod-bin")
}

// seedTrashedProduct creates an account product and moves it to the bin, so
// "it is in the bin" is a real store state rather than an assumed one.
func seedTrashedProduct(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, name string, priceSen int64) *dbstore.Product {
	t.Helper()
	ps := dbstore.NewProductStore(pool)
	p := &dbstore.Product{AccountID: accountID, Name: name, PriceSen: priceSen, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("create product %q: %v", name, err)
	}
	if err := ps.SoftDelete(ctx, accountID, p.ID); err != nil {
		t.Fatalf("soft delete %q: %v", name, err)
	}
	return p
}

// productBinRow decodes one bin row out of a listing, failing when the listing is
// not the trash family's BARE ARRAY. The bare array is the shared shape (forms,
// products and media all answer one), so a bin that answered an {"items":…}
// envelope would have broken every existing consumer — this is why the decode
// itself is the assertion.
func productBinRow(t *testing.T, raw []byte, id string) map[string]any {
	t.Helper()
	row := findTrashItem(trashItems(t, raw), id)
	if row == nil {
		t.Fatalf("product %s is not in the bin listing (body %s)", id, raw)
	}
	return row
}

// liveProductIDs decodes the live catalogue listing (also a bare array) into a
// set of ids.
func liveProductIDs(t *testing.T, raw []byte) map[string]bool {
	t.Helper()
	var live []map[string]any
	if err := json.Unmarshal(raw, &live); err != nil {
		t.Fatalf("the live product list is not a bare array: %v (body %s)", err, raw)
	}
	out := map[string]bool{}
	for _, p := range live {
		if id, ok := p["id"].(string); ok {
			out[id] = true
		}
	}
	return out
}

// TestRESTProductBinListsTheTrashedRow is the bin's read side: the row carries
// everything the trash view renders, including the pre-computed purge answer.
func TestRESTProductBinListsTheTrashedRow(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	clean := seedTrashedProduct(t, ctx, pool, accountID, "Kopi Ais", 650)

	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/products/trash = %d, want 200 (body %s)", code, raw)
	}
	row := productBinRow(t, raw, clean.ID)

	if row["name"] != "Kopi Ais" {
		t.Errorf("name = %v, want %q", row["name"], "Kopi Ais")
	}
	// Money is integer sen on the wire, never a fraction.
	if row["price_sen"] != float64(650) {
		t.Errorf("price_sen = %v (%T), want 650 as integer sen", row["price_sen"], row["price_sen"])
	}
	if ds, _ := row["deleted_at"].(string); ds == "" {
		t.Error("the bin row carries no deleted_at, so the UI cannot say when it was trashed")
	}
	if row["sales_count"] != float64(0) {
		t.Errorf("sales_count = %v, want 0 — the product was never sold", row["sales_count"])
	}
	if row["subscription_count"] != float64(0) {
		t.Errorf("subscription_count = %v, want 0 — nothing bills this product", row["subscription_count"])
	}
	// can_purge is what enables the permanent-delete control. On a clean row it
	// must be TRUE, or the button the whole feature exists for is dead.
	if row["can_purge"] != true {
		t.Errorf("can_purge = %v, want true on a product with no sales and no subscriptions", row["can_purge"])
	}

	// The live catalogue must NOT serve a trashed row (the issue #98 class): the
	// bin is a separate state, not a filter the ordinary read forgot.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/products = %d, want 200 (body %s)", code, raw)
	}
	if liveProductIDs(t, raw)[clean.ID] {
		t.Error("a trashed product is still served by the live catalogue listing — off the catalogue is not the same as in the bin")
	}
}

// TestRESTProductTrashRouteIsNotShadowedByProductID asserts the routing claim the
// route table's own comment makes, which no store test can reach.
//
// The risk is real and asymmetric: getProduct is keyed on `{id}`, so if the
// literal segment lost the match, GET /v1/rest/products/trash would be answered
// by getProduct with id="trash". That does not crash — it fails the uuid cast
// inside the store and comes back a plain 404 — so the symptom a user reports
// would be "the bin is always empty", not an error.
func TestRESTProductTrashRouteIsNotShadowedByProductID(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	trashed := seedTrashedProduct(t, ctx, pool, accountID, "Shadow Kopi", 900)

	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/products/trash = %d (%s) — the literal route did not win over "+
			"/v1/rest/products/{id}", code, raw)
	}
	// The proof it reached the bin handler and not getProduct: the answer is an
	// array of bin rows, which getProduct can never produce (getProduct answers a
	// single OBJECT with a name/price_sen pair, never a list).
	if len(raw) == 0 || raw[0] != '[' {
		t.Fatalf("GET /v1/rest/products/trash answered a non-array body, so it did not reach the bin "+
			"handler: %s", raw)
	}
	productBinRow(t, raw, trashed.ID)

	// The other half of the same claim — {id} still handles everything that is
	// NOT the literal. A non-uuid id is refused as not-found by the store, so
	// this is a 404 and NOT the bin listing; if the two routes were swapped we
	// would see the bin's 200 array here instead.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products/not-a-uuid-at-all", "")
	if code != http.StatusNotFound {
		t.Fatalf("GET /v1/rest/products/not-a-uuid-at-all = %d (%s), want 404 — either the {id} "+
			"route was swallowed by the bin, or an invalid id is no longer refused", code, raw)
	}
	if len(raw) > 0 && raw[0] == '[' {
		t.Errorf("a non-uuid id was answered with an array — that is the bin's shape, not getProduct's: %s", raw)
	}
}

// TestRESTProductPurgeRefusesAWrongConfirmation covers the gate that no store
// test can reach: the caller must name the product back before it is destroyed.
//
// Each way of "not the right name" is exercised separately — omitted, empty,
// whitespace-only, and a different name — and each is paired with a ROW
// assertion, because a 400 that had already deleted the row would pass a
// status-only check while destroying the very thing the confirmation exists to
// protect.
func TestRESTProductPurgeRefusesAWrongConfirmation(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	p := seedTrashedProduct(t, ctx, pool, accountID, "Kopi Confirm", 700)
	ps := dbstore.NewProductStore(pool)

	cases := []struct {
		label string
		body  string
	}{
		{"an omitted confirm_name", `{}`},
		{"an empty confirm_name", `{"confirm_name":""}`},
		{"a whitespace-only confirm_name", `{"confirm_name":"   "}`},
		{"a different product's name", `{"confirm_name":"Nama Lain"}`},
		{"the name with the wrong case", `{"confirm_name":"kopi confirm"}`},
	}
	for _, tc := range cases {
		code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+p.ID+"/purge", tc.body)
		if code != http.StatusBadRequest {
			t.Fatalf("purge with %s = %d, want 400 (body %s)", tc.label, code, raw)
		}
		// The refusal must carry its OWN code, not the class fallback. The message
		// is assembled around the product's name, so it can never match an exact
		// key in errCodeByMessage — without a prefix entry the body answered
		// `request.badRequest`, a code the dashboard deliberately does not
		// translate (issue #6177), so a merchant who mistyped the name read the
		// server's Malay prose instead of their own language. Asserted on EVERY
		// case, the empty and whitespace-only ones included: "the mismatch names
		// its code" and "so does a confirmation that is merely blank" are two
		// claims, and the blank ones are what an early-return guard would skip.
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("purge with %s: the 400 body is not the error envelope: %v (%s)", tc.label, err, raw)
		}
		if got := body["code"]; got != CodeProductConfirmNameMismatch {
			t.Errorf("purge with %s: code = %v, want %q — %q is what the dashboard does NOT "+
				"translate, so the merchant would read the server's prose", tc.label, got,
				CodeProductConfirmNameMismatch, ClassBadRequest)
		}
		// ...and the prose SURVIVES beside it: API-key/curl/MCP callers consume
		// that string and cannot look up an i18n code. It still names the value
		// the caller has to type, which is the whole point of the confirmation.
		if s, _ := body["error"].(string); !strings.Contains(s, p.Name) {
			t.Errorf("purge with %s: error = %q, want the wire prose naming the product (%q) — "+
				"the code was added BESIDE the prose, not instead of it", tc.label, s, p.Name)
		}
		if _, err := ps.GetTrashed(ctx, accountID, p.ID); err != nil {
			t.Fatalf("purge with %s was REFUSED but the product left the bin anyway: %v", tc.label, err)
		}
	}

	// The positive control for the same comparison: the name is TrimSpace'd, so a
	// padded but otherwise correct confirmation IS accepted. Without this the
	// refusals above would also pass on a handler that refused everything.
	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+p.ID+"/purge",
		`{"confirm_name":"  Kopi Confirm  "}`)
	if code != http.StatusOK {
		t.Fatalf("purge with the right name padded by whitespace = %d, want 200 — the comparison "+
			"trims, so padding must not be a mismatch (body %s)", code, raw)
	}
	if _, err := ps.GetTrashed(ctx, accountID, p.ID); err == nil {
		t.Error("the accepted purge answered 200 but the product is still in the bin")
	}
}

// TestRESTProductPurgeRefusesAProductWithSales pins the sales refusal and its
// wire shape.
//
// A sale is history: `sales.subscription_product_id` is ON DELETE SET NULL, so
// without this named refusal the DELETE would SUCCEED and silently detach the
// sale from the product it records. The code and the count are asserted on the
// real response because the dashboard renders both.
func TestRESTProductPurgeRefusesAProductWithSales(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	p := seedTrashedProduct(t, ctx, pool, accountID, "Langganan Ahli", 2900)
	ps := dbstore.NewProductStore(pool)

	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-BIN-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 2900)`,
		accountID, p.ID); err != nil {
		t.Fatalf("seed sale: %v", err)
	}

	// The bin must warn BEFORE the click: the row says not-purgeable and states
	// how many sales it is holding onto.
	_, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	row := productBinRow(t, raw, p.ID)
	if row["can_purge"] != false {
		t.Errorf("can_purge = %v, want false on a product that has a sale", row["can_purge"])
	}
	if row["sales_count"] != float64(1) {
		t.Errorf("sales_count = %v, want 1", row["sales_count"])
	}

	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+p.ID+"/purge",
		`{"confirm_name":"`+p.Name+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("purge of a sold product = %d, want 409 (body %s)", code, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("refusal body is not JSON: %v (body %s)", err, raw)
	}
	if body["code"] != CodeProductHasSales {
		t.Errorf("refusal code = %v, want %q — without it the dashboard falls back to a generic "+
			"sentence and hides the obstacle", body["code"], CodeProductHasSales)
	}
	if body["sales_count"] != float64(1) {
		t.Errorf("refusal sales_count = %v, want 1", body["sales_count"])
	}
	// The refusal states a number; the bin printed a number. They are the same
	// product, so they must be the same number or the merchant is told two
	// different things about one row.
	if body["sales_count"] != row["sales_count"] {
		t.Errorf("the refusal says sales_count=%v while the bin said %v for the same product — "+
			"the bin and the 409 have to agree", body["sales_count"], row["sales_count"])
	}

	// And the refusal really did keep it: a 409 that deleted anyway is worse
	// than no guard.
	if _, err := ps.GetTrashed(ctx, accountID, p.ID); err != nil {
		t.Fatalf("the refused purge removed the product from the bin: %v", err)
	}
}

// TestRESTProductPurgeRefusesAProductWithASubscription pins the second refusal.
//
// This one is the harsher of the two: `subscriptions.product_id` is ON DELETE
// RESTRICT, so with no named refusal the purge would surface as a raw SQLSTATE
// 23503 and a 500 — the caller would be told the server broke instead of what to
// clear.
func TestRESTProductPurgeRefusesAProductWithASubscription(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	ps := dbstore.NewProductStore(pool)
	p := &dbstore.Product{
		AccountID: accountID, Name: "Sub Bulanan", PriceSen: 1500, Currency: "MYR",
		Type: "subscription", BillingPeriod: "month",
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("create subscription product: %v", err)
	}
	if err := ps.SoftDelete(ctx, accountID, p.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id,
		                            period, amount_sen, next_charge_at)
		 VALUES ($1, 'it-bin@example.test', $2, 'tok-' || md5(random()::text), 'month', 1500, now())`,
		accountID, p.ID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	_, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	row := productBinRow(t, raw, p.ID)
	if row["can_purge"] != false {
		t.Errorf("can_purge = %v, want false while a subscription still bills this product", row["can_purge"])
	}
	if row["subscription_count"] != float64(1) {
		t.Errorf("subscription_count = %v, want 1", row["subscription_count"])
	}

	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+p.ID+"/purge",
		`{"confirm_name":"`+p.Name+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("purge of a subscribed product = %d, want 409, not a 500 from the FK (body %s)", code, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("refusal body is not JSON: %v (body %s)", err, raw)
	}
	if body["code"] != CodeProductHasSubscriptions {
		t.Errorf("refusal code = %v, want %q", body["code"], CodeProductHasSubscriptions)
	}
	if body["subscription_count"] != float64(1) {
		t.Errorf("refusal subscription_count = %v, want 1", body["subscription_count"])
	}
	if _, err := ps.GetTrashed(ctx, accountID, p.ID); err != nil {
		t.Fatalf("the refused purge removed the product from the bin: %v", err)
	}
}

// TestRESTProductPurgeHappyPathAndRestore is the two successful transitions: a
// clean trashed product really is destroyed by a confirmed purge, and a trashed
// product really does come back to the live catalogue.
func TestRESTProductPurgeHappyPathAndRestore(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	ps := dbstore.NewProductStore(pool)
	doomed := seedTrashedProduct(t, ctx, pool, accountID, "Buang Ini", 1200)
	restored := seedTrashedProduct(t, ctx, pool, accountID, "Ambil Balik", 1300)

	// ── purge: confirmed by name, the row is gone and so is it from the bin.
	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+doomed.ID+"/purge",
		`{"confirm_name":"`+doomed.Name+`"}`)
	if code != http.StatusOK {
		t.Fatalf("confirmed purge = %d, want 200 (body %s)", code, raw)
	}
	var purged map[string]any
	if err := json.Unmarshal(raw, &purged); err != nil {
		t.Fatalf("purge body is not JSON: %v (body %s)", err, raw)
	}
	if purged["status"] != "purged" {
		t.Errorf("purge status = %v, want \"purged\"", purged["status"])
	}
	if _, err := ps.GetTrashed(ctx, accountID, doomed.ID); err == nil {
		t.Error("the purge answered 200 but the product is still in the bin")
	}
	_, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if findTrashItem(trashItems(t, raw), doomed.ID) != nil {
		t.Error("a purged product is still listed in the bin")
	}
	// THE ONE ASSERTION THE TWO ABOVE CANNOT MAKE. Both of them are satisfied by
	// a "purge" that RESTORES: GetTrashed is scoped `deleted_at IS NOT NULL` and
	// the bin listing filters on the same column, so a row put back on the live
	// catalogue is silently missing from both and a 200 that un-trashes reads
	// exactly like a 200 that destroyed. A purge is promised irreversible while a
	// restore is a one-request undo, so the difference has to be read off the
	// TABLE — deliberately not through any store method, every one of which has
	// its own idea of which rows count.
	var purgedRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM products WHERE id=$1`, doomed.ID).Scan(&purgedRows); err != nil {
		t.Fatalf("count products: %v", err)
	}
	if purgedRows != 0 {
		t.Errorf("the row is still in products after a confirmed purge (count=%d) — a purge that "+
			"un-trashes instead of deleting is a restore wearing the purge's wire contract", purgedRows)
	}

	// ── restore: the SAME row comes back into the catalogue.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/products/"+restored.ID+"/restore", "")
	if code != http.StatusOK {
		t.Fatalf("restore = %d, want 200 (body %s)", code, raw)
	}
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/products after restore = %d (body %s)", code, raw)
	}
	if !liveProductIDs(t, raw)[restored.ID] {
		t.Errorf("a restored product is not back in the live catalogue (body %s)", raw)
	}
	_, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if findTrashItem(trashItems(t, raw), restored.ID) != nil {
		t.Error("a restored product is still listed in the bin")
	}
}

// TestRESTProductPurgeRefusesALiveRow: the bin is the ONLY way into a permanent
// delete, so a call aimed at a live product must not destroy anything.
//
// The refusal is a 404, and deliberately so — GetTrashed is scoped to trashed
// rows, which is also what stops the endpoint being used to probe whether a live
// product exists. The assertion is on the ROW, because the point is that nothing
// happened.
func TestRESTProductPurgeRefusesALiveRow(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)

	ps := dbstore.NewProductStore(pool)
	live := &dbstore.Product{AccountID: accountID, Name: "Masih Jual", PriceSen: 800, Currency: "MYR", Type: "one_time"}
	if err := ps.Create(ctx, live); err != nil {
		t.Fatalf("create live product: %v", err)
	}

	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+live.ID+"/purge",
		`{"confirm_name":"`+live.Name+`"}`)
	if code != http.StatusNotFound {
		t.Fatalf("purge of a LIVE product = %d, want 404 (body %s) — the bin is the only way in", code, raw)
	}
	// Nothing destroyed, and nothing quietly moved to the bin either.
	if _, err := ps.Get(ctx, accountID, live.ID); err != nil {
		t.Fatalf("the refused purge of a live product destroyed it: %v", err)
	}
	if _, err := ps.GetTrashed(ctx, accountID, live.ID); err == nil {
		t.Error("the refused purge pushed a live product into the bin — the call must change nothing at all")
	}

	// THE PROBE SURFACE. The 404 above is also what a WRONG confirmation on a
	// trashed product's sibling-id would give, so on its own it does not pin
	// WHICH read the handler made: the store's Purge carries its own
	// trashed-only guard and would refuse a live row no matter what the handler
	// read first.
	//
	// What distinguishes them is a WRONG name against a LIVE product's id. The
	// handler's confirmation read is GetTrashed (account-scoped AND
	// trashed-scoped), so the read fails before the name is ever compared and the
	// answer is the same 404 an unknown id gives. Swap that read for the ordinary
	// Get and the read SUCCEEDS, the name comparison runs, and the caller gets a
	// 400 — which tells a writer holding only the uuid that this id names a real,
	// live product, and whose name it is, because the 400's body echoes the name
	// it wanted. That is exactly the "cannot be used to probe a live product's
	// existence" the handler's comment claims, and it is the only half of this
	// guarantee the wire can show.
	code, raw = tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+live.ID+"/purge",
		`{"confirm_name":"definitely-not-the-name"}`)
	if code != http.StatusNotFound {
		t.Errorf("purge of a LIVE product with a WRONG name = %d, want the same 404 an unknown id "+
			"gives — a different answer here is an existence probe (body %s)", code, raw)
	}
	if strings.Contains(string(raw), live.Name) {
		t.Errorf("the refusal body echoes the LIVE product's name (%q), so a guessed uuid reveals "+
			"it: %s", live.Name, raw)
	}
	// The control for that assertion: an unknown id and the live id must be
	// indistinguishable, or the difference itself is the probe.
	unknownCode, unknownRaw := tplReq(t, h, key, http.MethodDelete,
		"/v1/rest/products/"+accountID+"/purge", `{"confirm_name":"definitely-not-the-name"}`)
	if unknownCode != code {
		t.Errorf("an unknown id answers %d while a LIVE product answers %d — the pair tells a caller "+
			"which uuids are real products", unknownCode, code)
	}
	_ = unknownRaw
}
