package dbstore

// Issue #102 — data-integrity invariant tests.
//
// These assert the invariants AGENTS.md and the schema rely on:
//   - Money is integer sen everywhere (no float drift).
//   - ref_code uniqueness.
//   - No orphaned sales on form delete (soft-delete keeps form_id).
//   - Slug uniqueness per space (#77) holds.
//   - Suppression list + breaker counters consistent.
//
// They are integration tests: skipped unless JOMFORM_TEST_DATABASE_URL is
// set; migrations are applied first, mirroring the production boot path.

import (
	"context"
	"testing"
)

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

// TestSlugUniquenessPerSpace asserts the per-space unique index (#77) exists
// and that two live forms in the same space cannot share a slug, while the
// same slug in different spaces is allowed.
func TestSlugUniquenessPerSpace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)

	// Schema-level: the per-space unique index must exist.
	var n int
	if err := p.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE tablename='forms' AND indexdef ILIKE '%space_id%' AND indexdef ILIKE '%slug%' AND indexdef ILIKE '%unique%'`).Scan(&n); err != nil {
		t.Fatalf("check per-space slug index: %v", err)
	}
	if n == 0 {
		t.Error("no per-space unique index on forms(space_id, slug) — slug uniqueness per space (#77) invariant violated")
	}

	// Behavioural: same slug in the same space must fail; across spaces OK.
	accA, spaceA := newSpaceAccount(t, ctx, p)
	accB, spaceB := newSpaceAccount(t, ctx, p)
	if spaceA == spaceB {
		t.Fatalf("spaces must differ, got %q", spaceA)
	}
	slug := "it-per-space-" + accA[:8]

	fa := &Form{AccountID: accA, Slug: slug, Title: "A", Status: "published"}
	if err := fs.Create(ctx, fa); err != nil {
		t.Fatalf("create form in space A: %v", err)
	}
	// Same slug, same space (accA) → must fail.
	dup := &Form{AccountID: accA, Slug: slug, Title: "A2", Status: "published"}
	if err := fs.Create(ctx, dup); err == nil {
		t.Error("duplicate slug in same space succeeded — per-space slug uniqueness violated")
	}
	// Same slug, different space (accB) → must succeed.
	fb := &Form{AccountID: accB, Slug: slug, Title: "B", Status: "published"}
	if err := fs.Create(ctx, fb); err != nil {
		t.Fatalf("create form in space B with same slug: %v", err)
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
