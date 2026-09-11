package payments

// Issue #489: the QR table number (?table=N) must reach the merchant's
// sale.paid webhook payload and the {{table_number}} notification placeholder.
// The wiring that carries it from the sale into both was built in #438; these
// tests pin it down so it cannot regress now that the order form actually
// populates sales.table_number.

import (
	"context"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestIssue489NotificationRendersTableNumberPlaceholder asserts the
// {{table_number}} placeholder resolves to the sale's table number.
func TestIssue489NotificationRendersTableNumberPlaceholder(t *testing.T) {
	ref := NotificationRef{
		RefCode: "ABC123", AmountSen: 800, ItemsSummary: "Nasi Lemak x1",
		BuyerName: "Ali", FormURL: "https://shop.jomform.com/qr",
		TableNumber: "8",
	}
	subj, body := renderNotification(
		"Order {{ref_code}} — table {{table_number}}",
		"Hi {{buyer_name}}, serve to table {{table_number}} (RM {{amount}})",
		ref)
	if subj != "Order ABC123 — table 8" {
		t.Fatalf("subject = %q, want the table number rendered", subj)
	}
	if body != "Hi Ali, serve to table 8 (RM 8.00)" {
		t.Fatalf("body = %q, want the table number rendered", body)
	}
}

// TestIssue489NotificationPlaceholderEmptyWhenNoTable guards the plain online
// order: with no table number the placeholder renders empty, never a literal.
func TestIssue489NotificationPlaceholderEmptyWhenNoTable(t *testing.T) {
	ref := NotificationRef{RefCode: "ABC123", AmountSen: 800}
	_, body := renderNotification("", "table={{table_number}}", ref)
	if body != "table=" {
		t.Fatalf("body = %q, want the placeholder empty for a non-QR order", body)
	}
}

// TestIssue489PaidWebhookCarriesTableNumber asserts the sale.paid webhook
// payload includes table_number for a QR order.
func TestIssue489PaidWebhookCarriesTableNumber(t *testing.T) {
	ctx := context.Background()
	pool := lockTestPool(t)
	saleID, purchaseID := seedLockSale(t, ctx, pool)

	// Give the seeded sale a table number, as the order form now does.
	if _, err := pool.Exec(ctx,
		`UPDATE sales SET table_number = '8' WHERE id = $1`, saleID); err != nil {
		t.Fatalf("set table_number: %v", err)
	}

	var mu sync.Mutex
	var got map[string]any
	svc := &Service{
		Sales:    dbstore.NewSaleStore(pool),
		Accounts: dbstore.NewAccountStore(pool),
		OnPaid: func(_ context.Context, _, _ string, payload map[string]any) {
			mu.Lock()
			defer mu.Unlock()
			got = payload
		},
	}
	if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid}); err != nil {
		t.Fatalf("paid callback: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("OnPaid was not called for the paid event")
	}
	if tn, _ := got["table_number"].(string); tn != "8" {
		t.Fatalf("sale.paid payload table_number = %v, want \"8\"", got["table_number"])
	}
}
