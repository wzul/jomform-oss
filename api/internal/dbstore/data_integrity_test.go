package dbstore

// Issue #102 — data-integrity invariant tests.
//
// These assert the invariants AGENTS.md and the schema rely on:
//   - Money is integer sen everywhere (no float drift).
//   - ref_code uniqueness.
//   - No orphaned sales on form delete (soft-delete keeps form_id).
//   - Slug uniqueness per workspace (#77) holds.
//   - Suppression list + breaker counters consistent.
//
// They are integration tests: skipped unless JOMFORM_TEST_DATABASE_URL is
// set; migrations are applied first, mirroring the production boot path.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// perWorkspaceSlugIndexRead is the schema-level half of TestSlugUniquenessPerWorkspace.
//
// It reads the catalogue directly instead of going through the pg_indexes view.
// Do NOT go back to `SELECT ... FROM pg_indexes WHERE tablename='forms' AND
// indexdef ILIKE ...`. `pg_indexes.indexdef` is pg_get_indexdef(i.oid) — a
// per-row relation open — and the view has no schema filter, so that form
// OID-opens EVERY `forms` index in the whole database, every lane's scratch
// schema included (25 of them on the shared test database). Two consequences,
// both measured on Postgres 18.3:
//
//  1. The database-wide sweep is the expensive one, and every index it touches
//     is opened by OID. Any peer connection that invalidates a relation OID
//     underneath it — a scratch schema's `DROP SCHEMA ... CASCADE` + migration
//     pass, or a lane's throwaway-database churn — can make that open fail with
//     `could not open relation with OID <n> (SQLSTATE XX000)`. That is the
//     failure this test hit in a package the failing diff never touched.
//  2. It reads other lanes' schemas: the same index exists once per scratch
//     schema, so the count comes back as the number of live lanes, not the one
//     this run owns. That figure is time-varying — it is the size of the board
//     (measured 5 quiet, 25 under load, 11 when last corrected), never a
//     constant this read should be expected to return.
//
// The read below is scoped to current_schema() and touches pg_class/pg_index/
// pg_attribute only; its plan carries no pg_get_indexdef call, so it opens no
// relation by OID and a peer's DDL has nothing to invalidate underneath it.
// TestSlugIndexReadIsSchemaScoped and TestSlugIndexReadDoesNotDeparseIndexDef
// pin both properties, and TestSlugIndexReadRequiresAUniqueIndex pins the
// `x.indisunique` requirement — the invariant is that the index ENFORCES
// uniqueness, so a read matching a non-unique index of the same key columns
// would report the #77 constraint present when nothing enforces it.
const perWorkspaceSlugIndexRead = `
	SELECT count(*) FROM pg_class i
	  JOIN pg_index x ON x.indexrelid = i.oid
	  JOIN pg_class t ON t.oid = x.indrelid
	  JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
	 WHERE nsp.nspname = current_schema()
	   AND t.relname = 'forms'
	   AND i.relkind IN ('i','I')
	   AND x.indisunique
	   AND (SELECT array_agg(a.attname ORDER BY a.attname)
	          FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
	          JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum
	         WHERE k.attnum <> 0 AND k.ord <= x.indnkeyatts) = ARRAY['slug','workspace_id']::name[]`

// TestMoneyIsIntegerSen asserts every money column is an integer type (BIGINT)
// and that no float/numeric-with-scale column is used for money. This guards
// against a migration silently switching money to a float type (float drift).
func TestMoneyIsIntegerSen(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	// Every money column in the schema must be BIGINT (integer sen).
	moneyCols := []struct {
		table, col string
	}{
		{"products", "price_sen"},
		{"sales", "subtotal_sen"},
		{"sales", "shipping_sen"},
		{"sales", "discount_sen"},
		{"sales", "total_sen"},
		// Issue #518: the running refunded total is money too.
		{"sales", "refunded_sen"},
		{"subscriptions", "amount_sen"},
		{"subscription_charges", "amount_sen"},
	}
	for _, mc := range moneyCols {
		var dataType string
		if err := p.QueryRow(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name=$1 AND column_name=$2`,
			mc.table, mc.col).Scan(&dataType); err != nil {
			t.Fatalf("lookup %s.%s: %v", mc.table, mc.col, err)
		}
		if dataType != "bigint" {
			t.Errorf("%s.%s is %s, want bigint (money must be integer sen, no float drift)", mc.table, mc.col, dataType)
		}
	}

	// No money column may be a float/numeric type anywhere in the app schema.
	// Restrict to user tables (exclude pg_catalog / information_schema / pg_*).
	rows, err := p.Query(ctx, `
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema NOT IN ('pg_catalog','information_schema')
		  AND t.table_type = 'BASE TABLE'
		  AND c.data_type IN ('double precision','real','numeric')
		  AND (c.column_name LIKE '%_sen' OR c.column_name LIKE '%amount%' OR c.column_name LIKE '%price%' OR c.column_name LIKE '%total%')`)
	if err != nil {
		t.Fatalf("scan float money columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tbl, col, dt string
		if err := rows.Scan(&tbl, &col, &dt); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		t.Errorf("money column %s.%s is %s — money must be integer sen, no float drift", tbl, col, dt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
}

// TestNoOrphanedSalesOnFormDelete asserts that soft-deleting a form keeps its
// sales' form_id intact (no orphaned sales, no NULL form_id from a hard
// delete). Soft-delete is the only delete path; form_id must survive it.
func TestNoOrphanedSalesOnFormDelete(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	ss := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	f := &Form{AccountID: acc, Slug: "it-softdel-" + acc[:8], Title: "IT", Status: "published"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	sale := &Sale{AccountID: acc, FormID: f.ID, RefCode: "JF-SD-" + acc[:8], Client: SaleClient{FullName: "A", Email: "a@example.test"}, Items: []SaleItem{}, Status: SalePaid, PaymentMethod: "chip", SaleType: SaleTypeOneTime}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	// Soft-delete the form.
	if err := fs.SoftDelete(ctx, acc, f.ID); err != nil {
		t.Fatalf("soft delete form: %v", err)
	}

	// The sale must still reference the form (form_id preserved, not NULL).
	got, err := ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("fetch sale after soft delete: %v", err)
	}
	if got.FormID != f.ID {
		t.Errorf("sale.form_id = %q after soft delete, want %q (no orphaned sales; soft-delete keeps form_id)", got.FormID, f.ID)
	}

	// The form row must still exist (soft-deleted, not hard-deleted).
	var deletedAt *string
	if err := p.QueryRow(ctx, `SELECT deleted_at::text FROM forms WHERE id=$1`, f.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("check form deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("form deleted_at is NULL after SoftDelete — soft-delete invariant violated")
	}
}

// TestSlugUniquenessPerWorkspace asserts the per-workspace unique index (#77) exists
// and that two live forms in the same workspace cannot share a slug, while the
// same slug in different workspaces is allowed.
func TestSlugUniquenessPerWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)

	// Schema-level: the per-workspace unique index must exist, read from the
	// catalogue directly — see perWorkspaceSlugIndexRead for why the whole-DB
	// `pg_indexes` sweep this replaced was the wrong shape.
	var n int
	if err := p.QueryRow(ctx, perWorkspaceSlugIndexRead).Scan(&n); err != nil {
		t.Fatalf("check per-workspace slug index: %v", err)
	}
	if n == 0 {
		t.Error("no per-workspace unique index on forms(workspace_id, slug) — slug uniqueness per workspace (#77) invariant violated")
	}

	// Behavioural: same slug in the same workspace must fail; across workspaces OK.
	accA, workspaceA := newWorkspaceAccount(t, ctx, p)
	accB, workspaceB := newWorkspaceAccount(t, ctx, p)
	if workspaceA == workspaceB {
		t.Fatalf("workspaces must differ, got %q", workspaceA)
	}
	slug := "it-per-workspace-" + accA[:8]

	fa := &Form{AccountID: accA, Slug: slug, Title: "A", Status: "published"}
	if err := fs.Create(ctx, fa); err != nil {
		t.Fatalf("create form in workspace A: %v", err)
	}
	// Same slug, same workspace (accA) → must fail.
	dup := &Form{AccountID: accA, Slug: slug, Title: "A2", Status: "published"}
	if err := fs.Create(ctx, dup); err == nil {
		t.Error("duplicate slug in same workspace succeeded — per-workspace slug uniqueness violated")
	}
	// Same slug, different workspace (accB) → must succeed.
	fb := &Form{AccountID: accB, Slug: slug, Title: "B", Status: "published"}
	if err := fs.Create(ctx, fb); err != nil {
		t.Fatalf("create form in workspace B with same slug: %v", err)
	}
}

// TestSuppressionListConsistency asserts the suppression list is consistent:
// upsert is idempotent (no duplicate rows), and a suppressed address is
// reported suppressed while a non-suppressed one is not.
func TestSuppressionListConsistency(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sup := NewSuppressionStore(p)

	email := "it-suppress-" + uniqueAccount(t, ctx, p)[:8] + "@example.test"

	// Not suppressed initially.
	if ok, err := sup.IsSuppressed(ctx, email); err != nil {
		t.Fatalf("IsSuppressed (initial): %v", err)
	} else if ok {
		t.Error("fresh address reported suppressed")
	}

	// Upsert twice — must be idempotent (single row).
	if _, err := sup.Upsert(ctx, email, "hard bounce", SuppressionSourceBounce); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if _, err := sup.Upsert(ctx, email, "complaint", SuppressionSourceComplaint); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	var count int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM suppressed_emails WHERE email=$1`, email).Scan(&count); err != nil {
		t.Fatalf("count suppressed: %v", err)
	}
	if count != 1 {
		t.Errorf("suppressed_emails has %d rows for %s, want 1 (upsert must be idempotent)", count, email)
	}

	// Now suppressed.
	if ok, err := sup.IsSuppressed(ctx, email); err != nil {
		t.Fatalf("IsSuppressed (after upsert): %v", err)
	} else if !ok {
		t.Error("suppressed address not reported suppressed")
	}

	// Remove → no longer suppressed.
	if err := sup.Remove(ctx, email); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ok, err := sup.IsSuppressed(ctx, email); err != nil {
		t.Fatalf("IsSuppressed (after remove): %v", err)
	} else if ok {
		t.Error("removed address still reported suppressed")
	}
}

// TestRefundedSenNeverExceedsTotalSen asserts the #518 money invariant:
// refunded_sen <= total_sen for every sale, and refunded_sen is never negative.
// The whole over-refund guard rests on this — if a refund could push the
// running total past the sale total, the balance would be unrecoverable and a
// merchant could be refunded more than they ever charged.
//
// It also asserts the schema default: a freshly inserted sale starts at 0, so a
// sale that has never been refunded cannot misreport a balance.
func TestRefundedSenNeverExceedsTotalSen(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	// A fresh sale defaults to refunded_sen = 0.
	sale := &Sale{
		AccountID: acc, RefCode: "JF-RF-" + randSuffix(),
		Client:      SaleClient{FullName: "A", Email: "a@example.test"},
		Items:       []SaleItem{{Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePaid,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	got, err := ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.RefundedSen != 0 {
		t.Errorf("new sale refunded_sen = %d, want 0", got.RefundedSen)
	}
	if got.RefundStatus() != RefundNone {
		t.Errorf("new sale RefundStatus() = %q, want %q", got.RefundStatus(), RefundNone)
	}

	// A partial refund is recorded and stays within the total.
	if err := ss.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		cur, err := ss.SaleByIDTx(ctx, tx, sale.ID)
		if err != nil {
			return err
		}
		_, err = ss.ApplyRefundTx(ctx, tx, cur, 2000)
		return err
	}); err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	got, err = ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload after partial: %v", err)
	}
	if got.RefundedSen != 2000 || got.RefundStatus() != RefundPartial {
		t.Errorf("after partial: refunded_sen = %d status = %q, want 2000 / %q", got.RefundedSen, got.RefundStatus(), RefundPartial)
	}
	if got.Status == SaleRefunded {
		t.Errorf("status = %q after a PARTIAL refund — must not read as fully refunded", got.Status)
	}

	// An over-refund inside the same atomic path is rejected by the SQL guard.
	if err := ss.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		cur, err := ss.SaleByIDTx(ctx, tx, sale.ID)
		if err != nil {
			return err
		}
		if _, err := ss.ApplyRefundTx(ctx, tx, cur, 9999); err == nil {
			t.Error("ApplyRefundTx accepted an over-refund")
		}
		return nil
	}); err != nil {
		t.Fatalf("over-refund attempt: %v", err)
	}

	// The remainder completes the refund and can never exceed the total.
	if err := ss.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		cur, err := ss.SaleByIDTx(ctx, tx, sale.ID)
		if err != nil {
			return err
		}
		_, err = ss.ApplyRefundTx(ctx, tx, cur, 3000)
		return err
	}); err != nil {
		t.Fatalf("remainder refund: %v", err)
	}
	got, err = ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload after full: %v", err)
	}
	if got.RefundedSen != got.TotalSen {
		t.Errorf("fully refunded: refunded_sen = %d, want total_sen = %d", got.RefundedSen, got.TotalSen)
	}
	if got.RefundStatus() != RefundFull {
		t.Errorf("RefundStatus() = %q, want %q", got.RefundStatus(), RefundFull)
	}
	if got.Status != SaleRefunded {
		t.Errorf("status = %q, want %q once fully refunded", got.Status, SaleRefunded)
	}
	if got.OutstandingSen() != 0 {
		t.Errorf("OutstandingSen() = %d, want 0 after a full refund", got.OutstandingSen())
	}

	// Schema-wide: no sale anywhere may have refunded_sen > total_sen or < 0.
	rows, err := p.Query(ctx, `SELECT id, refunded_sen, total_sen FROM sales WHERE refunded_sen < 0 OR refunded_sen > total_sen`)
	if err != nil {
		t.Fatalf("scan refund invariant: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var refunded, total int64
		if err := rows.Scan(&id, &refunded, &total); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		t.Errorf("sale %s violates the money invariant: refunded_sen = %d, total_sen = %d (want 0 <= refunded_sen <= total_sen)", id, refunded, total)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
}
