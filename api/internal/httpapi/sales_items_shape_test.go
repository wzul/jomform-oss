package httpapi

// The REST surface of the `sales.items` shape class.
//
// dbstore/sales_items_shape_test.go proves the STORE tolerates a non-array
// `sales.items`. This file proves the consequence the card is actually about:
// GET /v1/rest/products/trash is the endpoint that calls SalesCountsByProduct
// ONCE for the whole account and fails the ENTIRE request on its error, so ONE
// such row took the bin down for every product the account owns. The failure
// mode is not a panic and not a partial answer — it is a 500 with the bin
// unreadable, in REST and in the MCP tool that shares the query.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
// Requests go through the real route table with a real Bearer key, and every
// assertion on a status is paired with one on the ROW.

import (
	"context"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestTrashBinListingSurvivesANonArraySalesItems is the card's headline at the
// level a merchant experiences it: a sale whose `items` is an OBJECT must not
// make the workspace's product bin unreadable.
//
// The assertion is 200 + the OTHER products' rows, not "no panic": on the
// unfixed tree the handler's SalesCountsByProduct error reaches respondErr's
// default branch and the endpoint answers 500 "internal error" for every
// product in the account.
func TestTrashBinListingSurvivesANonArraySalesItems(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, key := productBinFixture(t)
	ps := dbstore.NewProductStore(pool)

	sold := seedTrashedProduct(t, ctx, pool, accountID, "IT shape sold", 1200)
	clean := seedTrashedProduct(t, ctx, pool, accountID, "IT shape clean", 1200)

	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-IT-' || md5(random()::text), '{}'::jsonb,
		         jsonb_build_array(jsonb_build_object('product_id', $2::text, 'qty', 1)), 1200)`,
		accountID, sold.ID); err != nil {
		t.Fatalf("seed the well-formed sale: %v", err)
	}
	// The only way this value can exist: a direct write. Every app path
	// serialises a slice.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		 VALUES ($1, 'JF-IT-' || md5(random()::text), '{}'::jsonb, '{"oops":true}'::jsonb, 0)`,
		accountID); err != nil {
		t.Fatalf("seed the non-array sale: %v", err)
	}

	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/products/trash = %d, want 200 — ONE sale with a non-array `items` made the whole workspace's bin unreadable (body %s)",
			code, raw)
	}

	rows := trashItems(t, raw)
	if len(rows) != 2 {
		t.Fatalf("bin listed %d product(s), want the account's 2 (body %s)", len(rows), raw)
	}
	for _, r := range rows {
		if ds, _ := r["deleted_at"].(string); ds == "" {
			t.Errorf("bin row %v has no deleted_at", r["name"])
		}
	}
	soldRow := productBinRow(t, raw, sold.ID)
	cleanRow := productBinRow(t, raw, clean.ID)
	if soldRow["sales_count"] != float64(1) {
		t.Errorf("bin sales_count for the well-formed sale = %v, want 1 — the non-array row must be SKIPPED, not swallow the other products' numbers", soldRow["sales_count"])
	}
	if cleanRow["sales_count"] != float64(0) {
		t.Errorf("bin sales_count for the clean product = %v, want 0", cleanRow["sales_count"])
	}
	if soldRow["can_purge"] != false {
		t.Error("can_purge=true for a product with a sale — the button would lie and the purge would refuse")
	}
	if cleanRow["can_purge"] != true {
		t.Error("can_purge=false for a product no sale references, with the non-array row present")
	}

	// And a purge the merchant does not need to think about must still go
	// through: the non-array row must not block an unrelated clean product.
	code, raw = tplReq(t, h, key, http.MethodDelete, "/v1/rest/products/"+clean.ID+"/purge",
		`{"confirm_name":"IT shape clean"}`)
	if code != http.StatusOK {
		t.Fatalf("purge of the clean product = %d, want 200 (body %s)", code, raw)
	}
	if _, err := ps.GetTrashed(ctx, accountID, clean.ID); err == nil {
		t.Error("the accepted purge answered 200 but the product is still in the bin")
	}
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/products/trash", "")
	if code != http.StatusOK {
		t.Fatalf("bin listing after the purge = %d, want 200 — the non-array row still makes the bin unreadable (body %s)", code, raw)
	}
	if findTrashItem(trashItems(t, raw), clean.ID) != nil {
		t.Errorf("the purged product is still in the bin: %s", raw)
	}
}
