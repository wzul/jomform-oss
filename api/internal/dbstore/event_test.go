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

// TestEventStoreWildcardFilter (issue #195): the EventName filter supports a
// trailing "*" wildcard so the dashboard and list_events can query MCP request
// history (event_name=mcp.*) without enumerating every mcp.* event name.
func TestEventStoreWildcardFilter(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEventStore(p)

	acc := uniqueAccount(t, ctx, p)

	// Record a mix of events: two mcp.tool_call rows and one unrelated row.
	for _, name := range []string{"mcp.tool_call", "mcp.tool_call", "form.created"} {
		if err := es.Record(ctx, acc, "", name, map[string]any{"tool": "list_products"}, "", ""); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}

	// Wildcard mcp.* matches only the two mcp.tool_call rows.
	got, total, err := es.List(ctx, EventFilter{AccountIDs: []string{acc}, EventName: "mcp.*", Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list mcp.*: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("mcp.* total=%d len=%d, want 2/2", total, len(got))
	}
	for _, e := range got {
		if e.EventName != "mcp.tool_call" {
			t.Fatalf("wildcard matched %q, want only mcp.tool_call", e.EventName)
		}
	}

	// Exact name still matches exactly (no wildcard expansion).
	exact, total, err := es.List(ctx, EventFilter{AccountIDs: []string{acc}, EventName: "mcp.tool_call", Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list exact: %v", err)
	}
	if total != 2 || len(exact) != 2 {
		t.Fatalf("exact total=%d len=%d, want 2/2", total, len(exact))
	}

	// Count honours the wildcard too.
	n, err := es.Count(ctx, EventFilter{AccountIDs: []string{acc}, EventName: "mcp.*"})
	if err != nil {
		t.Fatalf("count mcp.*: %v", err)
	}
	if n != 2 {
		t.Fatalf("count mcp.* = %d, want 2", n)
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

// TestEmailDeliveryEventStoreSaleRefCorrelation (issue #106): Record with a
// sale_ref, ListBySaleRef returns only that sale's events, and
// LatestBySaleRefs returns the most recent event per ref.
func TestEmailDeliveryEventStoreSaleRefCorrelation(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEmailDeliveryEventStore(p)

	refA := "JF-A-" + randSuffix()
	refB := "JF-B-" + randSuffix()
	email := "corr-" + randSuffix() + "@example.com"

	if err := es.Record(ctx, "", email, "delivered", "", refA, nil); err != nil {
		t.Fatalf("record delivered: %v", err)
	}
	if err := es.Record(ctx, "", email, "open", "", refA, map[string]any{"ipAddress": "203.0.113.1"}); err != nil {
		t.Fatalf("record open: %v", err)
	}
	if err := es.Record(ctx, "", email, "delivered", "", refB, nil); err != nil {
		t.Fatalf("record delivered B: %v", err)
	}

	// ListBySaleRef returns only refA's events, oldest first.
	got, err := es.ListBySaleRef(ctx, refA)
	if err != nil {
		t.Fatalf("list by sale ref: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("refA events = %d, want 2", len(got))
	}
	if got[0].EventType != "delivered" || got[1].EventType != "open" {
		t.Fatalf("refA order = %s,%s want delivered,open", got[0].EventType, got[1].EventType)
	}
	for _, e := range got {
		if e.SaleRef != refA {
			t.Fatalf("event sale_ref = %q, want %q", e.SaleRef, refA)
		}
	}

	// LatestBySaleRefs returns the most recent event per ref.
	latest, err := es.LatestBySaleRefs(ctx, []string{refA, refB})
	if err != nil {
		t.Fatalf("latest by refs: %v", err)
	}
	if latest[refA].EventType != "open" {
		t.Fatalf("latest[refA] = %s, want open", latest[refA].EventType)
	}
	if latest[refB].EventType != "delivered" {
		t.Fatalf("latest[refB] = %s, want delivered", latest[refB].EventType)
	}
}

// TestEmailDeliveryEventStoreListByFilter (issue #194): the account-scoped,
// paginated, filterable delivery-event query used by GET
// /v1/rest/email-deliveries and the list_email_deliveries MCP tool. Verifies
// account scoping, form filtering (via the sale's form_id join), sale_ref and
// event_type filters, and pagination.
func TestEmailDeliveryEventStoreListByFilter(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEmailDeliveryEventStore(p)
	fs := NewFormStore(p)
	ss := NewSaleStore(p)

	acc := uniqueAccount(t, ctx, p)
	other := uniqueAccount(t, ctx, p)

	// Two forms for the account; one sale per form.
	f1 := &Form{AccountID: acc, Slug: "del-f1-" + acc[:8], Title: "F1", Status: "published"}
	if err := fs.Create(ctx, f1); err != nil {
		t.Fatalf("create form 1: %v", err)
	}
	f2 := &Form{AccountID: acc, Slug: "del-f2-" + acc[:8], Title: "F2", Status: "published"}
	if err := fs.Create(ctx, f2); err != nil {
		t.Fatalf("create form 2: %v", err)
	}
	ref1 := "JF-D1-" + acc[:8]
	ref2 := "JF-D2-" + acc[:8]
	for _, s := range []*Sale{
		{AccountID: acc, FormID: f1.ID, RefCode: ref1, Client: SaleClient{FullName: "A", Email: "a@example.test"}, Items: []SaleItem{}, Status: SalePaid, PaymentMethod: "chip", SaleType: SaleTypeOneTime},
		{AccountID: acc, FormID: f2.ID, RefCode: ref2, Client: SaleClient{FullName: "B", Email: "b@example.test"}, Items: []SaleItem{}, Status: SalePaid, PaymentMethod: "chip", SaleType: SaleTypeOneTime},
	} {
		if err := ss.Create(ctx, s); err != nil {
			t.Fatalf("create sale: %v", err)
		}
	}

	email := "del-" + acc[:8] + "@example.com"
	// ref1: delivered + open. ref2: bounce.
	if err := es.Record(ctx, acc, email, "delivered", "", ref1, nil); err != nil {
		t.Fatalf("record delivered: %v", err)
	}
	if err := es.Record(ctx, acc, email, "open", "", ref1, map[string]any{"ipAddress": "203.0.113.1"}); err != nil {
		t.Fatalf("record open: %v", err)
	}
	if err := es.Record(ctx, acc, email, "bounce", "Permanent", ref2, map[string]any{"bounceType": "Permanent"}); err != nil {
		t.Fatalf("record bounce: %v", err)
	}
	// A system-mail event with no account (must never leak into account reads).
	if err := es.Record(ctx, "", "sys@example.com", "delivered", "", "", nil); err != nil {
		t.Fatalf("record system: %v", err)
	}

	// Account-scoped: only acc's events, newest first.
	all, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}
	if all[0].EventType != "bounce" {
		t.Fatalf("all[0] = %s, want bounce (newest first)", all[0].EventType)
	}
	// FormID resolved via the sale join.
	if all[0].FormID != f2.ID {
		t.Fatalf("bounce FormID = %q, want %q", all[0].FormID, f2.ID)
	}

	// Filter by form (f1) → only ref1's events.
	byForm, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, FormID: f1.ID, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list by form: %v", err)
	}
	if total != 2 || len(byForm) != 2 {
		t.Fatalf("by form total=%d len=%d, want 2,2", total, len(byForm))
	}
	for _, e := range byForm {
		if e.SaleRef != ref1 {
			t.Fatalf("by form event sale_ref = %q, want %q", e.SaleRef, ref1)
		}
	}

	// Filter by sale_ref.
	bySale, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, SaleRef: ref2, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list by sale: %v", err)
	}
	if total != 1 || len(bySale) != 1 || bySale[0].EventType != "bounce" {
		t.Fatalf("by sale total=%d len=%d type=%s, want 1,1,bounce", total, len(bySale), bySale[0].EventType)
	}

	// Filter by event_type.
	byType, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, EventType: "open", Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list by type: %v", err)
	}
	if total != 1 || len(byType) != 1 || byType[0].EventType != "open" {
		t.Fatalf("by type total=%d len=%d, want 1,1", total, len(byType))
	}

	// Pagination: limit 2 → page 1 has 2, page 2 has 1.
	p1, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, Page: 1, Limit: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(p1) != 2 || total != 3 {
		t.Fatalf("page1 len=%d total=%d, want 2,3", len(p1), total)
	}
	p2, _, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{acc}, Page: 2, Limit: 2})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(p2) != 1 {
		t.Fatalf("page2 len=%d, want 1", len(p2))
	}

	// A different account sees nothing.
	otherEvents, total, err := es.ListByFilter(ctx, DeliveryFilter{AccountIDs: []string{other}, Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("other account: %v", err)
	}
	if total != 0 || len(otherEvents) != 0 {
		t.Fatalf("other account total=%d len=%d, want 0,0", total, len(otherEvents))
	}
}

// TestSaleStoreReceiptMessageID (issue #106): SetReceiptMessageID persists the
// SES MessageID and ByReceiptMessageID resolves it back to the sale.
func TestSaleStoreReceiptMessageID(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSaleStore(p)

	acc := uniqueAccount(t, ctx, p)
	ref := "JF-R-" + randSuffix()
	sale := &Sale{
		AccountID: acc, RefCode: ref,
		Client: SaleClient{FullName: "Buyer", Email: "r@example.test"},
		Items:  []SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: SaleTypeOneTime, PaymentMethod: "chip", Status: SalePaid,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	messageID := "msg-" + randSuffix()
	if err := ss.SetReceiptMessageID(ctx, sale.ID, messageID); err != nil {
		t.Fatalf("set receipt message id: %v", err)
	}
	byID, err := ss.ByID(ctx, sale.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if byID.ReceiptMessageID != messageID {
		t.Fatalf("receipt_message_id = %q, want %q", byID.ReceiptMessageID, messageID)
	}
	byMsg, err := ss.ByReceiptMessageID(ctx, messageID)
	if err != nil {
		t.Fatalf("by receipt message id: %v", err)
	}
	if byMsg.RefCode != ref {
		t.Fatalf("by message id ref = %q, want %q", byMsg.RefCode, ref)
	}
}

// TestEmailDeliveryEventStoreRecordListSweep exercises the SES delivery-event
// store: Record → List → Sweep.
//
// The email_delivery_events table is shared with the httpapi package's SES
// tests (ses_issue93_test.go) which run in parallel against the same test DB,
// so this test must scope every assertion to its OWN rows (unique emails)
// rather than asserting a global row count — otherwise it flakes under
// `go test ./...`.
func TestEmailDeliveryEventStoreRecordListSweep(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	es := NewEmailDeliveryEventStore(p)

	// Test isolation: the shared test DB accumulates rows across runs, and
	// this test asserts on a global List. Clear the table first so the
	// assertion is deterministic (issue #99 test hygiene).
	if _, err := p.Exec(ctx, `DELETE FROM email_delivery_events`); err != nil {
		t.Fatalf("clear email_delivery_events: %v", err)
	}
	// Unique emails so we never collide with rows written by other packages.
	bounceEmail := "bounce-" + randSuffix() + "@example.com"
	complaintEmail := "complaint-" + randSuffix() + "@example.com"

	// System mail (no account) + account mail.
	if err := es.Record(ctx, "", bounceEmail, "bounce", "Permanent", "", map[string]any{"bounceType": "Permanent"}); err != nil {
		t.Fatalf("record bounce: %v", err)
	}
	acc := uniqueAccount(t, ctx, p)
	if err := es.Record(ctx, acc, complaintEmail, "complaint", "", "", nil); err != nil {
		t.Fatalf("record complaint: %v", err)
	}

	// Filter the global list down to just this test's rows.
	events, err := es.List(ctx, 200)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var mine []EmailDeliveryEvent
	for _, e := range events {
		if e.Email == bounceEmail || e.Email == complaintEmail {
			mine = append(mine, e)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("own events = %d, want 2", len(mine))
	}
	// Newest first: complaint (recorded last) first.
	if mine[0].EventType != "complaint" || mine[0].AccountID != acc {
		t.Fatalf("mine[0] = %+v, want complaint for account", mine[0])
	}
	if mine[1].EventType != "bounce" || mine[1].BounceType != "Permanent" || mine[1].AccountID != "" {
		t.Fatalf("mine[1] = %+v, want bounce/Permanent/system", mine[1])
	}

	// Sweep with a tiny TTL deletes everything older than 1ns — including this
	// test's rows. Assert the sweep removed at least our two rows (the global
	// count is not asserted because other packages may have written rows too).
	deleted, err := es.Sweep(ctx, time.Nanosecond)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted < 2 {
		t.Fatalf("sweep deleted = %d, want >= 2 (our rows)", deleted)
	}
	// Our rows are gone after the sweep.
	after, err := es.List(ctx, 200)
	if err != nil {
		t.Fatalf("list after sweep: %v", err)
	}
	for _, e := range after {
		if e.Email == bounceEmail || e.Email == complaintEmail {
			t.Fatalf("row %s survived sweep", e.Email)
		}
	}
}
