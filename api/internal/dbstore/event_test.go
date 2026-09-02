package dbstore

// Integration tests for the event-tracking stores (issue #92): events
// (audit/behaviour log) and email_delivery_events (SES feedback). Skipped
// unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied first,
// mirroring the production boot path.

import (
	"context"
	"testing"
	"time"
)

// TestEventStoreRecordListCountBreakdown exercises the full write/read path:
// Record → List (paginated, filtered) → Count → Breakdown.
func TestEventStoreRecordListCountBreakdown(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEventStore(p)

	acc := uniqueAccount(t, ctx, p)
	user := testOwnerUser(t, ctx, p)

	// Record a few events across two names.
	for i := 0; i < 3; i++ {
		if err := es.Record(ctx, acc, user, "form.created", map[string]any{"form_id": "f1"}, "203.0.113.1", "test-agent"); err != nil {
			t.Fatalf("record form.created: %v", err)
		}
	}
	if err := es.Record(ctx, acc, "", "sale.confirmed", map[string]any{"ref_code": "JF-1"}, "203.0.113.2", ""); err != nil {
		t.Fatalf("record sale.confirmed: %v", err)
	}

	// List all (page 1, limit 10) — newest first.
	events, total, err := es.List(ctx, EventFilter{AccountIDs: []string{acc}, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}
	// Newest first: the last-recorded sale.confirmed should be first.
	if events[0].EventName != "sale.confirmed" {
		t.Fatalf("events[0].EventName = %q, want sale.confirmed", events[0].EventName)
	}
	if events[0].UserID != "" {
		t.Fatalf("sale.confirmed user_id = %q, want empty (system action)", events[0].UserID)
	}
	if events[0].IP != "203.0.113.2" {
		t.Fatalf("sale.confirmed ip = %q, want 203.0.113.2", events[0].IP)
	}

	// Filter by event_name.
	created, total, err := es.List(ctx, EventFilter{AccountIDs: []string{acc}, EventName: "form.created", Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if total != 3 || len(created) != 3 {
		t.Fatalf("filtered total=%d len=%d, want 3/3", total, len(created))
	}

	// Pagination: page 2 of limit 3 → 1 row.
	page2, total, err := es.List(ctx, EventFilter{AccountIDs: []string{acc}, Page: 2, Limit: 3})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if total != 4 || len(page2) != 1 {
		t.Fatalf("page2 total=%d len=%d, want 4/1", total, len(page2))
	}

	// Count.
	n, err := es.Count(ctx, EventFilter{AccountIDs: []string{acc}})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 4 {
		t.Fatalf("count = %d, want 4", n)
	}

	// Breakdown.
	bd, err := es.Breakdown(ctx, EventFilter{AccountIDs: []string{acc}})
	if err != nil {
		t.Fatalf("breakdown: %v", err)
	}
	if len(bd) != 2 {
		t.Fatalf("breakdown len = %d, want 2", len(bd))
	}
	// Ordered by count desc: form.created (3) first.
	if bd[0].EventName != "form.created" || bd[0].Count != 3 {
		t.Fatalf("breakdown[0] = %+v, want form.created/3", bd[0])
	}

	// Account scoping: a different account sees nothing.
	other := uniqueAccount(t, ctx, p)
	otherEvents, otherTotal, err := es.List(ctx, EventFilter{AccountIDs: []string{other}, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list other account: %v", err)
	}
	if otherTotal != 0 || len(otherEvents) != 0 {
		t.Fatalf("other account total=%d len=%d, want 0/0", otherTotal, len(otherEvents))
	}
}

// TestEventStoreSweep verifies the retention sweep deletes old rows and keeps
// audit-class rows that fall under their longer per-class TTL.
func TestEventStoreSweep(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEventStore(p)

	acc := uniqueAccount(t, ctx, p)

	// Insert events at three ages relative to the TTLs (default 90d, audit
	// class 180d):
	//   - form.created @ 200d old  → > default AND > audit → deleted
	//   - auth.login    @ 200d old  → > audit class TTL → deleted
	//   - auth.login    @ 120d old  → > default but < audit class TTL → kept
	old := time.Now().UTC().Add(-200 * 24 * time.Hour)
	mid := time.Now().UTC().Add(-120 * 24 * time.Hour)
	for _, row := range []struct {
		name string
		at   time.Time
	}{
		{"form.created", old},
		{"auth.login", old},
		{"auth.login", mid},
	} {
		if _, err := p.Exec(ctx,
			`INSERT INTO events (account_id, event_name, properties, created_at)
			 VALUES ($1, $2, '{}', $3)`, acc, row.name, row.at); err != nil {
			t.Fatalf("insert %s: %v", row.name, err)
		}
	}

	ttlByClass := map[string]time.Duration{
		"auth.":     180 * 24 * time.Hour,
		"settings.": 180 * 24 * time.Hour,
	}
	deleted, err := es.Sweep(ctx, ttlByClass, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// The two 200d-old rows are deleted; the 120d-old auth.login is kept by
	// the 180d audit-class TTL.
	if deleted != 2 {
		t.Fatalf("sweep deleted = %d, want 2", deleted)
	}

	// Confirm the 120d-old auth.login survives.
	var n int64
	if err := p.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE account_id=$1 AND event_name='auth.login'`, acc).Scan(&n); err != nil {
		t.Fatalf("count auth.login: %v", err)
	}
	if n != 1 {
		t.Fatalf("auth.login rows after sweep = %d, want 1 (kept by class TTL)", n)
	}
}

// TestEmailDeliveryEventStoreRecordListSweep exercises the SES delivery-event
// store: Record → List → Sweep.
func TestEmailDeliveryEventStoreRecordListSweep(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEmailDeliveryEventStore(p)

	// System mail (no account) + account mail.
	if err := es.Record(ctx, "", "bounce@example.com", "bounce", "Permanent", map[string]any{"bounceType": "Permanent"}); err != nil {
		t.Fatalf("record bounce: %v", err)
	}
	acc := uniqueAccount(t, ctx, p)
	if err := es.Record(ctx, acc, "complaint@example.com", "complaint", "", nil); err != nil {
		t.Fatalf("record complaint: %v", err)
	}

	events, err := es.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	// Newest first: complaint (recorded last) first.
	if events[0].EventType != "complaint" || events[0].AccountID != acc {
		t.Fatalf("events[0] = %+v, want complaint for account", events[0])
	}
	if events[1].EventType != "bounce" || events[1].BounceType != "Permanent" || events[1].AccountID != "" {
		t.Fatalf("events[1] = %+v, want bounce/Permanent/system", events[1])
	}

	// Sweep with a tiny TTL deletes everything (all rows are older than 1ns).
	deleted, err := es.Sweep(ctx, time.Nanosecond)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("sweep deleted = %d, want 2", deleted)
	}
}
