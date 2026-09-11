// Issue #93 SES delivered/open/click event handling tests.
//
// These exercise suppressionWriter.handleEvent — the switch that records
// Delivered/Open/Click into email_delivery_events (informational, no
// suppression/breaker) while keeping Bounce/Complaint behaviour unchanged.
// They run against real Postgres (skipped unless JOMFORM_TEST_DATABASE_URL is
// set, mirroring ses_issue56_test.go). The SNS signature guard itself is
// covered by email/sns_test.go and ses_issue56_test.go.
package httpapi

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// issue93Fixture wires a suppressionWriter over real Postgres with the
// email_delivery_events store wired in. Returns the pool too so tests can
// create accounts/sales for correlation assertions.
func issue93Fixture(t *testing.T) (*suppressionWriter, *dbstore.EmailDeliveryEventStore, *dbstore.SuppressionStore, *pgxpool.Pool) {
	t.Helper()
	pool := bundleTestPool(t)
	events := dbstore.NewEmailDeliveryEventStore(pool)
	supps := dbstore.NewSuppressionStore(pool)
	sw := &suppressionWriter{
		Suppressions:        supps,
		EmailDeliveryEvents: events,
		// Issue #106: correlate Open/Click/Delivered to a sale via MessageID.
		Sales: dbstore.NewSaleStore(pool),
	}
	return sw, events, supps, pool
}

// latestEventTypes returns the event_type values recorded for a given email,
// newest first.
func latestEventTypes(t *testing.T, events *dbstore.EmailDeliveryEventStore, email string) []string {
	t.Helper()
	all, err := events.List(context.Background(), 200)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var out []string
	for _, e := range all {
		if e.Email == email {
			out = append(out, e.EventType)
		}
	}
	return out
}

func containsStr(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// TestSESDeliveredOpenClickRecorded: Delivered/Open/Click notifications record
// rows into email_delivery_events with the right event_type + detail, and do
// NOT write a suppression (informational only).
func TestSESDeliveredOpenClickRecorded(t *testing.T) {
	sw, events, supps, _ := issue93Fixture(t)
	ctx := context.Background()
	email := "delivery-" + randSuffix() + "@example.com"

	// Delivered.
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Delivered",
		Delivery: &struct {
			Recipients []string `json:"recipients"`
		}{Recipients: []string{email}},
	}) {
		t.Fatal("Delivered should be handled")
	}
	// Open (standard SES shape: recipient in mail.destination, no open.recipients).
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Open",
		Open: &struct {
			Recipients []string `json:"recipients"`
			IPAddress  string   `json:"ipAddress"`
			UserAgent  string   `json:"userAgent"`
		}{IPAddress: "203.0.113.7", UserAgent: "Mozilla/5.0"},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{email}},
	}) {
		t.Fatal("Open should be handled")
	}
	// Click (standard SES shape: recipient in mail.destination, no click.recipients).
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Click",
		Click: &struct {
			Recipients []string `json:"recipients"`
			Link       string   `json:"link"`
		}{Link: "https://jomform.com/r/abc"},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{email}},
	}) {
		t.Fatal("Click should be handled")
	}

	got := latestEventTypes(t, events, email)
	want := []string{"delivered", "open", "click"}
	if len(got) != len(want) {
		t.Fatalf("recorded events = %v, want %v", got, want)
	}
	for _, w := range want {
		if !containsStr(got, w) {
			t.Fatalf("recorded events = %v, want %v", got, want)
		}
	}

	// No suppression written for informational events.
	if sup, err := supps.IsSuppressed(ctx, email); err != nil || sup {
		t.Fatalf("informational event must NOT suppress (sup=%v err=%v)", sup, err)
	}
}

// TestSESOpenClickDetail: open/click detail carries userAgent/ip/link.
func TestSESOpenClickDetail(t *testing.T) {
	sw, events, _, _ := issue93Fixture(t)
	ctx := context.Background()
	email := "detail-" + randSuffix() + "@example.com"

	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Open",
		Open: &struct {
			Recipients []string `json:"recipients"`
			IPAddress  string   `json:"ipAddress"`
			UserAgent  string   `json:"userAgent"`
		}{Recipients: []string{email}, IPAddress: "198.51.100.9", UserAgent: "curl/8"},
	}) {
		t.Fatal("Open should be handled")
	}
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Click",
		Click: &struct {
			Recipients []string `json:"recipients"`
			Link       string   `json:"link"`
		}{Recipients: []string{email}, Link: "https://jomform.com/r/xyz"},
	}) {
		t.Fatal("Click should be handled")
	}

	all, err := events.List(ctx, 200)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var openDetail, clickDetail map[string]any
	for _, e := range all {
		if e.Email != email {
			continue
		}
		if e.EventType == "open" {
			openDetail = e.Detail
		}
		if e.EventType == "click" {
			clickDetail = e.Detail
		}
	}
	if openDetail == nil || openDetail["userAgent"] != "curl/8" || openDetail["ipAddress"] != "198.51.100.9" {
		t.Fatalf("open detail = %v, want userAgent+ipAddress", openDetail)
	}
	if clickDetail == nil || clickDetail["link"] != "https://jomform.com/r/xyz" {
		t.Fatalf("click detail = %v, want link", clickDetail)
	}
}

// TestSESBounceComplaintUnchanged: Bounce/Complaint still suppress + record
// (no regression to #56/#92).
func TestSESBounceComplaintUnchanged(t *testing.T) {
	sw, events, supps, _ := issue93Fixture(t)
	ctx := context.Background()
	email := "bounce-" + randSuffix() + "@example.com"

	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Bounce",
		Bounce: &sesBounce{
			BounceType:        "Permanent",
			BouncedRecipients: []sesRecipient{{EmailAddress: email}},
		},
	}) {
		t.Fatal("Bounce should be handled")
	}
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Complaint",
		Complaint: &struct {
			ComplainedRecipients []sesRecipient `json:"complainedRecipients"`
		}{ComplainedRecipients: []sesRecipient{{EmailAddress: email}}},
	}) {
		t.Fatal("Complaint should be handled")
	}

	// Both recorded.
	got := latestEventTypes(t, events, email)
	want := []string{"bounce", "complaint"}
	if len(got) != len(want) {
		t.Fatalf("recorded events = %v, want %v", got, want)
	}
	for _, w := range want {
		if !containsStr(got, w) {
			t.Fatalf("recorded events = %v, want %v", got, want)
		}
	}
	// Both suppressed.
	if sup, err := supps.IsSuppressed(ctx, email); err != nil || !sup {
		t.Fatalf("bounce+complaint must suppress (sup=%v err=%v)", sup, err)
	}
}

// TestSESUnknownEventIgnored: unknown/missing-payload events are ignored.
func TestSESUnknownEventIgnored(t *testing.T) {
	sw, _, _, _ := issue93Fixture(t)
	ctx := context.Background()
	if sw.handleEvent(ctx, &sesSNSMessage{EventType: "DeliveryDelay"}) {
		t.Fatal("unknown event should be ignored")
	}
	if sw.handleEvent(ctx, &sesSNSMessage{EventType: "Open"}) { // nil Open payload
		t.Fatal("Open with nil payload should be ignored")
	}
}

// TestSESOpenClickCorrelatedToSale (issue #106): an Open/Click/Delivered event
// whose mail.messageId matches a sale's receipt_message_id is recorded with
// that sale's ref_code, so the merchant can see per-sale email status.
func TestSESOpenClickCorrelatedToSale(t *testing.T) {
	sw, events, _, pool := issue93Fixture(t)
	ctx := context.Background()

	// Create a sale with a receipt_message_id (as SendSaleReceipt would set).
	acc, _ := bundleFixture(t, ctx, pool)
	ref := "JF-" + randSuffix()
	messageID := "msg-" + randSuffix()
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, total_sen, sale_type, payment_method, status, receipt_message_id)
		 VALUES ($1, $2, '{"full_name":"Buyer","email":"b@example.test"}', '[]', 1000, 'one_time', 'chip', 'paid', $3)`,
		acc, ref, messageID); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	// Delivered + Open + Click, all carrying the receipt's MessageID.
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Delivered",
		Delivery: &struct {
			Recipients []string `json:"recipients"`
		}{Recipients: []string{"b@example.test"}},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{"b@example.test"}, MessageID: messageID},
	}) {
		t.Fatal("Delivered should be handled")
	}
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Open",
		Open: &struct {
			Recipients []string `json:"recipients"`
			IPAddress  string   `json:"ipAddress"`
			UserAgent  string   `json:"userAgent"`
		}{IPAddress: "203.0.113.7", UserAgent: "Mozilla/5.0"},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{"b@example.test"}, MessageID: messageID},
	}) {
		t.Fatal("Open should be handled")
	}
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Click",
		Click: &struct {
			Recipients []string `json:"recipients"`
			Link       string   `json:"link"`
		}{Link: "https://jomform.com/r/abc"},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{"b@example.test"}, MessageID: messageID},
	}) {
		t.Fatal("Click should be handled")
	}

	// Every event for this sale's ref must carry the sale_ref.
	got, err := events.ListBySaleRef(ctx, ref)
	if err != nil {
		t.Fatalf("list by sale ref: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("events for %s = %d, want 3 (delivered/open/click)", ref, len(got))
	}
	for _, e := range got {
		if e.SaleRef != ref {
			t.Fatalf("event %s sale_ref = %q, want %q", e.EventType, e.SaleRef, ref)
		}
	}

	// An event with an unknown MessageID is recorded without a sale_ref.
	if !sw.handleEvent(ctx, &sesSNSMessage{
		EventType: "Open",
		Open: &struct {
			Recipients []string `json:"recipients"`
			IPAddress  string   `json:"ipAddress"`
			UserAgent  string   `json:"userAgent"`
		}{IPAddress: "198.51.100.9", UserAgent: "curl/8"},
		Mail: struct {
			Destination []string `json:"destination"`
			MessageID   string   `json:"messageId"`
		}{Destination: []string{"other@example.test"}, MessageID: "msg-unknown"},
	}) {
		t.Fatal("Open should be handled")
	}
	all, err := events.List(ctx, 200)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, e := range all {
		if e.Email == "other@example.test" && e.SaleRef != "" {
			t.Fatalf("uncorrelated open got sale_ref %q, want empty", e.SaleRef)
		}
	}
}
