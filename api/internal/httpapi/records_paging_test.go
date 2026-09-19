package httpapi

// The parts of the sales⇄submission read that the FIRST round of mutation
// testing showed were not actually guarded. Each test here exists because a
// mutation of the corresponding behaviour left the suite GREEN:
//
//   M6  — the sales-only legacy array shape stopped applying ?status= and
//         nothing failed: the array shape and its filters had no test at all.
//   M8  — removing the `row_id` tiebreaker from the merged ORDER BY changed
//         which rows a page returns on identical timestamps, and nothing
//         failed: paging across two tables was never exercised.
//
// M10 (MCP list_sales dropping the responses) is covered in the mcpsrv package,
// where that reader lives.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// salesOnlyFixture seeds a payment-only account (no response form anywhere) with
// the given sale statuses, so the legacy array shape's sales-only path can be
// tested in isolation.
func salesOnlyFixture(t *testing.T, statuses ...string) (*RESTHandlers, *pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)
	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-sales-only-"+accountID[:8], "it-sales-only-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "it-sales-only-f-" + accountID[:8], Title: "Plain form",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	sales := dbstore.NewSaleStore(pool)
	for i, status := range statuses {
		if err := sales.Create(ctx, &dbstore.Sale{
			AccountID: accountID, FormID: f.ID,
			RefCode:  fmt.Sprintf("IT-SO-%s-%02d", accountID[:8], i),
			Client:   dbstore.SaleClient{FullName: fmt.Sprintf("Buyer %d", i), Email: "so@example.test"},
			Items:    []dbstore.SaleItem{{ProductID: "p", Name: "Tee", PriceSen: 100, Qty: 1}},
			TotalSen: 100, SaleType: dbstore.SaleTypeOneTime,
			PaymentMethod: "chip", Status: status,
		}); err != nil {
			t.Fatalf("create sale %d: %v", i, err)
		}
	}

	h := newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = dbstore.NewSubmissionStore(pool)
	return h, pool, accountID
}

// TestLegacyArrayStatusFilterOnASalesOnlyAccount closes mutation M6.
//
// The legacy array shape (no ?page=) ignored status/date/q entirely: two callers
// of the same URL got different answers for the same query string depending on
// whether they paged. A payment-only account is the case the unfiltered
// mixed-list tests do not reach, because there the array shape takes the
// record-set branch.
func TestLegacyArrayStatusFilterOnASalesOnlyAccount(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID := salesOnlyFixture(t, dbstore.SalePaid, dbstore.SalePaid, dbstore.SaleFailed)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	for _, tc := range []struct {
		status string
		want   int
	}{
		{"paid", 2},
		{"failed", 1},
		{"cancelled", 0},
	} {
		code, body := salesGet(t, h, raw, "/v1/rest/sales?status="+tc.status)
		if code != http.StatusOK {
			t.Fatalf("status=%s: code %d (body %s)", tc.status, code, body)
		}
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("status=%s: decode %v (%s)", tc.status, err, body)
		}
		if len(rows) != tc.want {
			t.Errorf("legacy array ?status=%s returned %d rows, want %d — the array shape "+
				"must apply the same filters as the paginated one:\n%s", tc.status, len(rows), tc.want, body)
		}
	}

	// The paginated shape must agree with the array shape for the same filter —
	// that agreement is the point of the fix.
	_, arrayBody := salesGet(t, h, raw, "/v1/rest/sales?status=paid")
	var arrayRows []map[string]any
	if err := json.Unmarshal(arrayBody, &arrayRows); err != nil {
		t.Fatalf("decode array: %v", err)
	}
	_, pageBody := salesGet(t, h, raw, "/v1/rest/sales?page=1&limit=50&status=paid")
	page := decodeSalesPage(t, pageBody)
	if int64(len(arrayRows)) != page.Total {
		t.Errorf("array shape returned %d rows for status=paid, the paginated shape reports total=%d "+
			"— the two shapes of one URL disagree", len(arrayRows), page.Total)
	}
}

// TestLegacyArrayQFilterOnASalesOnlyAccount is the search half of M6: the array
// shape must match the list's semantics (ref_code OR buyer name).
func TestLegacyArrayQFilterOnASalesOnlyAccount(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID := salesOnlyFixture(t, dbstore.SalePaid, dbstore.SalePaid, dbstore.SalePaid)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Every fixture row's buyer is "Buyer N"; the ref prefix matches all three.
	_, allBody := salesGet(t, h, raw, "/v1/rest/sales")
	var all []map[string]any
	if err := json.Unmarshal(allBody, &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered array rows = %d, want 3", len(all))
	}

	_, noneBody := salesGet(t, h, raw, "/v1/rest/sales?q=no-such-buyer-anywhere")
	var none []map[string]any
	if err := json.Unmarshal(noneBody, &none); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("array shape ?q with no match returned %d rows, want 0 — it is ignoring the filter:\n%s",
			len(none), noneBody)
	}

	_, oneBody := salesGet(t, h, raw, "/v1/rest/sales?q=Buyer+1")
	var one []map[string]any
	if err := json.Unmarshal(oneBody, &one); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(one) != 1 {
		t.Errorf("array shape ?q=Buyer 1 returned %d rows, want exactly 1:\n%s", len(one), oneBody)
	}
}

// TestMergedPaginationIsStableOnIdenticalTimestamps closes mutation M8.
//
// Postgres `now()` is the TRANSACTION timestamp, so rows written in one
// transaction share created_at EXACTLY. Without a deterministic tiebreaker in
// the ORDER BY, LIMIT/OFFSET over such a set can repeat or skip rows across
// pages. The fixture writes many rows in ONE transaction so the collision is
// real, then pages through the whole set and asserts every row is seen exactly
// once.
func TestMergedPaginationIsStableOnIdenticalTimestamps(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)
	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-page-"+accountID[:8], "it-page-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account: %v", err)
	}
	forms := dbstore.NewFormStore(pool)
	payment := &dbstore.Form{
		AccountID: accountID, Slug: "it-page-p-" + accountID[:8], Title: "Pay",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, payment); err != nil {
		t.Fatalf("create payment form: %v", err)
	}
	response := &dbstore.Form{
		AccountID: accountID, Slug: "it-page-r-" + accountID[:8], Title: "Resp",
		Status: "published", Config: map[string]any{"mode": "response"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, response); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	// ONE transaction for the whole set, so every row shares created_at and the
	// tiebreaker is the ONLY thing keeping the order deterministic.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	const n = 25
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, total_sen, sale_type, payment_method, status)
			 VALUES ($1, $2, $3, '{}'::jsonb, '[]'::jsonb, 100, 100, 'one_time', 'chip', 'paid')`,
			accountID, payment.ID, fmt.Sprintf("IT-PAGE-%s-%02d", accountID[:8], i)); err != nil {
			t.Fatalf("insert sale %d: %v", i, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO submissions (form_id, account_id, data) VALUES ($1, $2, $3)`,
			response.ID, accountID, map[string]any{"n": i}); err != nil {
			t.Fatalf("insert submission %d: %v", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = dbstore.NewSubmissionStore(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// Page through the whole merged set with a small limit and collect the row
	// identity of everything seen.
	const pageSize = 7
	seen := map[string]int{}
	var totalSeen int64 = -1
	for page := 1; page <= 20; page++ {
		code, body := salesGet(t, h, raw,
			fmt.Sprintf("/v1/rest/sales?page=%d&limit=%d", page, pageSize))
		if code != http.StatusOK {
			t.Fatalf("page %d: code %d (body %s)", page, code, body)
		}
		p := decodeSalesPage(t, body)
		if totalSeen < 0 {
			totalSeen = p.Total
		}
		if int64(p.Total) != totalSeen {
			t.Fatalf("page %d reports total=%d, page 1 reported %d", page, p.Total, totalSeen)
		}
		if len(p.Items) == 0 {
			break
		}
		for _, row := range p.Items {
			key := ""
			if id, _ := row["submission_id"].(string); id != "" {
				key = "sub:" + id
			} else if ref, _ := row["ref_code"].(string); ref != "" {
				key = "sale:" + ref
			} else {
				t.Fatalf("a merged row has neither submission_id nor ref_code: %v", row)
			}
			seen[key]++
		}
	}

	if int64(len(seen)) != totalSeen {
		t.Fatalf("paging saw %d distinct rows but the total is %d — rows were skipped or repeated "+
			"(an unstable order on identical created_at values)", len(seen), totalSeen)
	}
	if totalSeen != 2*n {
		t.Fatalf("merged total = %d, want %d (25 sales + 25 submissions)", totalSeen, 2*n)
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("row %s was returned %d times across the pages (want exactly 1)", key, count)
		}
	}
}

// TestSalesOnlyPagingStableOnIdenticalTimestamps is the same guard for the
// sales-only path (the store's own ORDER BY needs the tiebreaker too).
func TestSalesOnlyPagingStableOnIdenticalTimestamps(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)
	workspace, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID,
		"it-page-so-"+accountID[:8], "it-page-so-"+accountID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET workspace_id=$1 WHERE id=$2`, workspace.ID, accountID); err != nil {
		t.Fatalf("link account: %v", err)
	}
	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID, Slug: "it-page-so-f-" + accountID[:8], Title: "Pay",
		Status: "published", Config: map[string]any{"mode": "payment"}, Theme: map[string]any{},
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	const n = 20
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sales (account_id, form_id, ref_code, client, items, subtotal_sen, total_sen, sale_type, payment_method, status)
			 VALUES ($1, $2, $3, '{}'::jsonb, '[]'::jsonb, 100, 100, 'one_time', 'chip', 'paid')`,
			accountID, f.ID, fmt.Sprintf("IT-SOP-%s-%02d", accountID[:8], i)); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Forms = forms
	h.Submissions = dbstore.NewSubmissionStore(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	const pageSize = 6
	seen := map[string]int{}
	var total int64 = -1
	for page := 1; page <= 20; page++ {
		_, body := salesGet(t, h, raw, fmt.Sprintf("/v1/rest/sales?page=%d&limit=%d", page, pageSize))
		p := decodeSalesPage(t, body)
		if total < 0 {
			total = p.Total
		}
		if len(p.Items) == 0 {
			break
		}
		for _, row := range p.Items {
			ref, _ := row["ref_code"].(string)
			seen[ref]++
		}
	}
	if int64(len(seen)) != total {
		t.Fatalf("sales-only paging saw %d rows, total = %d — unstable order", len(seen), total)
	}
	for ref, count := range seen {
		if count != 1 {
			t.Errorf("sale %s returned %d times", ref, count)
		}
	}
}
