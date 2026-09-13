package dbstore

// Issue #527: the money invariant refunded_sen <= total_sen is enforced in the
// application (payments.RefundSale, race-safe under the per-sale advisory lock)
// but was NOT enforced by the database. Nothing at the storage layer rejected
// refunded_sen > total_sen, so a future code path, a new refund surface, a
// migration, or a manual operational fix could persist impossible money — and
// every read surface (sales list, summary, analytics, CSV export, MCP) derives
// the outstanding balance and the partial-vs-full distinction from this column.
//
// These tests assert the DATABASE rejects the bad write. An app-level test alone
// is not sufficient: it only proves the guarded path is careful, not that the
// storage layer cannot be lied to.
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"strings"
	"testing"
)

// TestIssue527DBRejectsRefundedAboveTotal is the core guard: a DIRECT SQL write
// (bypassing every application guard) that makes refunded_sen exceed total_sen
// must be rejected by the constraint.
func TestIssue527DBRejectsRefundedAboveTotal(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

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

	// Direct SQL, past the guarded refund path entirely.
	_, err := p.Exec(ctx,
		`UPDATE sales SET refunded_sen = total_sen + 1 WHERE id = $1`, sale.ID)
	if err == nil {
		t.Fatalf("issue #527: the database accepted refunded_sen = total_sen + 1; " +
			"the refunded_sen <= total_sen invariant is not enforced at the storage layer")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sales_refunded_nonneg_and_le_total") {
		t.Fatalf("write was rejected, but not by the expected constraint: %v", err)
	}

	// The row must be untouched by the failed write.
	got, err := ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("reload sale: %v", err)
	}
	if got.RefundedSen != 0 {
		t.Fatalf("refunded_sen = %d after a rejected write, want 0", got.RefundedSen)
	}
}

// TestIssue527DBRejectsNegativeRefunded is the lower bound of the same
// invariant: refunded_sen must never go negative (which would inflate the
// apparent outstanding balance and break the summary arithmetic).
func TestIssue527DBRejectsNegativeRefunded(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	sale := &Sale{
		AccountID: acc, RefCode: "JF-RF-" + randSuffix(),
		Client:      SaleClient{FullName: "B", Email: "b@example.test"},
		Items:       []SaleItem{{Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePaid,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	if _, err := p.Exec(ctx,
		`UPDATE sales SET refunded_sen = -1 WHERE id = $1`, sale.ID); err == nil {
		t.Fatalf("issue #527: the database accepted a negative refunded_sen")
	}
}

// TestIssue527DBStillAllowsValidRefunds is the negative control: the constraint
// must not block legitimate states — 0, a partial, and the full total.
func TestIssue527DBStillAllowsValidRefunds(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)
	acc := uniqueAccount(t, ctx, p)

	sale := &Sale{
		AccountID: acc, RefCode: "JF-RF-" + randSuffix(),
		Client:      SaleClient{FullName: "C", Email: "c@example.test"},
		Items:       []SaleItem{{Name: "Tee", PriceSen: 5000, Qty: 1}},
		SubtotalSen: 5000, TotalSen: 5000,
		SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePaid,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	for _, sen := range []int64{0, 2000, 5000} {
		if _, err := p.Exec(ctx,
			`UPDATE sales SET refunded_sen = $2 WHERE id = $1`, sale.ID, sen); err != nil {
			t.Fatalf("valid refunded_sen = %d was rejected: %v", sen, err)
		}
	}
}
