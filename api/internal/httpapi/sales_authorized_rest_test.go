package httpapi

// The sales list must keep emitting `authorized` after migration 000091 dropped
// the `sales.authorized` COLUMN.
//
// The column was a second copy of `captured_at IS NOT NULL`. The store now
// derives the boolean in saleSelect (`(captured_at IS NOT NULL) AS authorized`)
// and the REST row still declares `Authorized bool `json:"authorized"`` with no
// omitempty, fed from s.Authorized. The comment on that field promises "the
// wire field stays for compatibility", but nothing in the repo checks the wire:
//
//   - dbstore's TestSalesAuthorizedColumnIsGoneAndDerivedFromCapturedAt proves the
//     STORE derives the value; it never serialises a row;
//   - the httpapi tests that read /sales assert ref_code / status / record sets,
//     never this key.
//
// Two independent one-line edits could therefore drop the promise SILENTLY,
// and this test is the only thing that turns red for either:
//
//	(b) dropping the json tag (Go then emits `Authorized`) or adding omitempty
//	    (the uncaptured row then emits no key at all);
//	(c) deleting `Authorized: s.Authorized` from toSaleRow, so a captured sale
//	    reads false.
//
// Deleting the field from saleRow is the third way, but only because toSaleRow
// assigns it: the pair goes together and the compiler stops the build. (b) and
// (c) compile, keep every existing httpapi test green, and are what this guard
// is for.
//
// The assertions run on RAW JSON, not on the decoded struct: the key must be
// PRESENT (a map lookup on json.RawMessage) and decode to exactly the boolean
// its captured_at implies. Decoding into saleRow would pass under (b) and (c),
// which leave or write the zero value.
// Both REST shapes (the paging-less array asserted here and the paginated
// envelope) are built by toSaleRow, so this one mapping is the whole contract.
//
// Integration against the real test Postgres (skipped without
// JOMFORM_TEST_DATABASE_URL), through the real route table with a real Bearer
// key. The captured row is captured through dbstore.MarkCaptured — the only
// writer of captured_at — so the fixture cannot invent a state production
// cannot reach.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestRESTSalesStillEmitsAuthorized pins the derived `authorized` field on the
// GET /v1/rest/sales payload: one sale captured through MarkCaptured and one
// authorization left uncaptured, both located by ref_code, each asserted to
// carry the key with the value its captured_at implies.
func TestRESTSalesStillEmitsAuthorized(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)

	// Ref codes carry the account id so rows can never collide with another
	// test's in this package's schema.
	capturedRef := "JF-AUTHZ-CAP-" + accountID[:8]
	openRef := "JF-AUTHZ-OPEN-" + accountID[:8]
	sales := dbstore.NewSaleStore(pool)

	newHold := func(ref, buyer string) *dbstore.Sale {
		s := &dbstore.Sale{
			AccountID: accountID, RefCode: ref,
			Client:      dbstore.SaleClient{FullName: buyer, Email: buyer + "@example.test"},
			Items:       []dbstore.SaleItem{{ProductID: "p-authz", Name: "Tee", PriceSen: 5000, Qty: 1}},
			SubtotalSen: 5000, TotalSen: 5000,
			SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
			Status: dbstore.SalePending, SkipCapture: true,
		}
		if err := sales.Create(ctx, s); err != nil {
			t.Fatalf("create the sale %s: %v", ref, err)
		}
		return s
	}

	captured := newHold(capturedRef, "captured")
	if err := sales.MarkCaptured(ctx, captured.ID); err != nil {
		t.Fatalf("MarkCaptured on %s: %v", capturedRef, err)
	}
	newHold(openRef, "open")

	code, body := salesGet(t, h, key, "/v1/rest/sales")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/sales = %d, want 200 (body %s)", code, body)
	}

	// Raw JSON values: a missing key must be distinguishable from `false`, which
	// is exactly the difference a dropped tag / omitempty would hide.
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the legacy sales shape is not a bare array: %v (body %s)", err, body)
	}

	for _, tc := range []struct {
		ref            string
		wantAuthorized bool
		wantCaptured   bool
	}{
		{capturedRef, true, true},
		{openRef, false, false},
	} {
		var row map[string]json.RawMessage
		for _, r := range rows {
			var ref string
			if err := json.Unmarshal(r["ref_code"], &ref); err == nil && ref == tc.ref {
				row = r
				break
			}
		}
		if row == nil {
			t.Fatalf("sale %s is not in the list (body %s)", tc.ref, body)
		}

		raw, present := row["authorized"]
		if !present {
			t.Fatalf("sale %s has no `authorized` key; the wire field the dashboard and API-key consumers read was dropped (row %v)", tc.ref, row)
		}
		var got bool
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("sale %s authorized = %s, not a JSON boolean: %v", tc.ref, raw, err)
		}
		if got != tc.wantAuthorized {
			t.Errorf("sale %s authorized = %v, want %v (captured_at on the row: %v)",
				tc.ref, got, tc.wantAuthorized, tc.wantCaptured)
		}

		// The derivation's INPUT, on the same row: `authorized` is only meaningful
		// as `captured_at != nil`, so the fixture must be shown to be the pair it
		// claims to be and not two rows of the same kind.
		_, hasCaptured := row["captured_at"]
		if hasCaptured != tc.wantCaptured {
			t.Errorf("sale %s captured_at present = %v, want %v; authorized is derived from exactly this",
				tc.ref, hasCaptured, tc.wantCaptured)
		}
	}
}
