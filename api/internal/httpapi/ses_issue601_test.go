// Bounce/complaint ingestion tests (suppression list, issue #601).
//
// These drive suppressionWriter.handleEvent — the real SNS ingestion switch —
// against a real Postgres, and read the outcome back from the suppressed_emails
// table. They cover the hard/soft rule:
//
//   - Permanent bounce (any sub-type)  → suppress immediately
//   - Complaint                        → suppress immediately
//   - Transient bounce                 → do NOT suppress on the first event;
//     suppress on the >=3rd within 7 days
//   - a duplicate event                → still exactly one row, no error
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set, mirroring ses_issue56_test.go
// and ses_issue93_test.go.
package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// ---------------------------------------------------------------------------
// fixtures / helpers
// ---------------------------------------------------------------------------

// suppressionRowCount returns the number of suppressed_emails rows for an
// address, read straight from the table so a duplicate row would be visible
// (the CITEXT primary key would normally prevent one, and this proves it did).
func suppressionRowCount(t *testing.T, pool *pgxpool.Pool, addr string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM suppressed_emails WHERE email = $1`,
		strings.ToLower(strings.TrimSpace(addr))).Scan(&n); err != nil {
		t.Fatalf("count suppressed_emails: %v", err)
	}
	return n
}

// fetchSuppression reads the single suppression row for an address (or nil).
func fetchSuppression(t *testing.T, sw *suppressionWriter, addr string) *dbstore.SuppressedEmail {
	t.Helper()
	se, err := sw.Suppressions.ByEmail(context.Background(), addr)
	if err != nil {
		t.Fatalf("ByEmail(%s): %v", addr, err)
	}
	return se
}

// bounceMsg builds a Bounce event for one address.
func bounceMsg(addr, bounceType, subType string) *sesSNSMessage {
	return &sesSNSMessage{
		EventType: "Bounce",
		Bounce: &sesBounce{
			BounceType:        bounceType,
			BounceSubType:     subType,
			BouncedRecipients: []sesRecipient{{EmailAddress: addr}},
		},
	}
}

// complaintMsg builds a Complaint event for one address.
func complaintMsg(addr string) *sesSNSMessage {
	return &sesSNSMessage{
		EventType: "Complaint",
		Complaint: &struct {
			ComplainedRecipients []sesRecipient `json:"complainedRecipients"`
		}{ComplainedRecipients: []sesRecipient{{EmailAddress: addr}}},
	}
}

// ---------------------------------------------------------------------------
// hard bounces / complaints
// ---------------------------------------------------------------------------

// TestSESHardBounceCreatesExactlyOneSuppression: a Permanent bounce inserts one
// row with the canonical reason and the sub-type preserved in the note.
func TestSESHardBounceCreatesExactlyOneSuppression(t *testing.T) {
	sw, _, _, _ := issue93Fixture(t)
	ctx := context.Background()
	addr := "hardbounce-" + randSuffix() + "@example.com"

	if !sw.handleEvent(ctx, bounceMsg(addr, "Permanent", "General")) {
		t.Fatal("Permanent bounce should be handled")
	}
	se := fetchSuppression(t, sw, addr)
	if se == nil {
		t.Fatal("a Permanent bounce must suppress the address")
	}
	if se.Reason != dbstore.SuppressionReasonHardBounce {
		t.Errorf("reason = %q, want %q", se.Reason, dbstore.SuppressionReasonHardBounce)
	}
	if se.Source != string(dbstore.SuppressionSourceBounce) {
		t.Errorf("source = %q, want %q", se.Source, dbstore.SuppressionSourceBounce)
	}
	if se.Note != "General" {
		t.Errorf("note = %q, want the bounce sub-type %q", se.Note, "General")
	}
	if se.CreatedBy != "" {
		t.Errorf("created_by = %q, want empty for ingestion", se.CreatedBy)
	}
}

// TestSESDuplicateBounceIsIdempotent: replaying the same bounce must not error
// and must not create a second row.
func TestSESDuplicateBounceIsIdempotent(t *testing.T) {
	sw, events, _, pool := issue93Fixture(t)
	ctx := context.Background()
	addr := "dup-" + randSuffix() + "@example.com"

	for i := 0; i < 3; i++ {
		if !sw.handleEvent(ctx, bounceMsg(addr, "Permanent", "NoEmail")) {
			t.Fatalf("iteration %d: Permanent bounce should be handled", i)
		}
	}
	if n := suppressionRowCount(t, pool, addr); n != 1 {
		t.Fatalf("suppressed_emails rows = %d, want exactly 1 after 3 identical bounces", n)
	}
	// Every delivery event is still appended (the log is append-only).
	got := latestEventTypes(t, events, addr)
	if len(got) != 3 {
		t.Fatalf("recorded bounce events = %d, want 3", len(got))
	}
}

// TestSESComplaintSuppressesImmediately: a complaint suppresses on the first
// event with the complaint reason/source.
func TestSESComplaintSuppressesImmediately(t *testing.T) {
	sw, _, _, pool := issue93Fixture(t)
	ctx := context.Background()
	addr := "complaint-" + randSuffix() + "@example.com"

	if !sw.handleEvent(ctx, complaintMsg(addr)) {
		t.Fatal("Complaint should be handled")
	}
	se := fetchSuppression(t, sw, addr)
	if se == nil {
		t.Fatal("a complaint must suppress the address")
	}
	if se.Reason != dbstore.SuppressionReasonComplaint {
		t.Errorf("reason = %q, want %q (the canonical code the reason CHECK allows, "+
			"NOT free text)", se.Reason, dbstore.SuppressionReasonComplaint)
	}
	if se.Source != string(dbstore.SuppressionSourceComplaint) {
		t.Errorf("source = %q, want %q", se.Source, dbstore.SuppressionSourceComplaint)
	}
	if n := suppressionRowCount(t, pool, addr); n != 1 {
		t.Fatalf("suppressed_emails rows = %d, want 1", n)
	}
	// A duplicate complaint is also idempotent.
	if !sw.handleEvent(ctx, complaintMsg(addr)) {
		t.Fatal("duplicate Complaint should be handled")
	}
	if n := suppressionRowCount(t, pool, addr); n != 1 {
		t.Fatalf("suppressed_emails rows = %d, want 1 after a duplicate complaint", n)
	}
}

// TestIngestionWritesOnlyCanonicalReasonCodes pins the contract the migration
// 000061 CHECK enforces: an ingested row's reason is one of the three canonical
// codes, never the provider's free text. It runs a representative event of each
// kind and reads the column back, because the CHECK is what stops a future
// regression from writing prose here — and a write that violates it would be
// rejected at runtime, losing the suppression entirely.
func TestIngestionWritesOnlyCanonicalReasonCodes(t *testing.T) {
	sw, _, _, pool := issue93Fixture(t)
	ctx := context.Background()

	// Representative events whose reason must be a canonical code.
	cases := []struct {
		name    string
		msg     *sesSNSMessage
		addr    string
		want    string
		notWant string
	}{
		{"permanent/general", bounceMsg("c1-"+randSuffix()+"@example.com", "Permanent", "General"),
			"", dbstore.SuppressionReasonHardBounce, "permanent bounce (Permanent)"},
		{"permanent/noemail", bounceMsg("c2-"+randSuffix()+"@example.com", "Permanent", "NoEmail"),
			"", dbstore.SuppressionReasonHardBounce, "permanent bounce (Permanent)"},
		{"unsubscribed", bounceMsg("c3-"+randSuffix()+"@example.com", "Permanent", "UnsubscribedRecipient"),
			"", dbstore.SuppressionReasonHardBounce, "permanent bounce (Permanent)"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := c.msg.Bounce.BouncedRecipients[0].EmailAddress
			if !sw.handleEvent(ctx, c.msg) {
				t.Fatal("event should be handled")
			}
			se := fetchSuppression(t, sw, addr)
			if se == nil {
				t.Fatal("event did not suppress the address")
			}
			if se.Reason != c.want {
				t.Errorf("reason = %q, want %q", se.Reason, c.want)
			}
			if se.Reason == c.notWant {
				t.Errorf("reason = %q — the provider's free text was written to the reason "+
					"column instead of the canonical code", se.Reason)
			}
			if !dbstore.IsCanonicalReason(se.Reason) {
				t.Errorf("reason %q is not one of the canonical codes the CHECK allows", se.Reason)
			}
		})
	}

	// The complaint path's code, read from the same table. It goes through the
	// same suppress() helper, so asserting the raw column value here is what
	// pins the canonical code for this event kind too.
	addr := "c-complaint-" + randSuffix() + "@example.com"
	if !sw.handleEvent(ctx, complaintMsg(addr)) {
		t.Fatal("complaint should be handled")
	}
	se := fetchSuppression(t, sw, addr)
	if se == nil {
		t.Fatal("complaint did not suppress the address")
	}
	if se.Reason != dbstore.SuppressionReasonComplaint {
		t.Errorf("complaint reason = %q, want %q", se.Reason, dbstore.SuppressionReasonComplaint)
	}
	if !dbstore.IsCanonicalReason(se.Reason) {
		t.Errorf("complaint reason %q is not canonical", se.Reason)
	}
	if n := suppressionRowCount(t, pool, addr); n != 1 {
		t.Fatalf("suppressed_emails rows = %d, want 1", n)
	}
}

// TestSESPermanentSubTypesAllSuppress: every Permanent sub-type SES emits means
// the mailbox will not accept our mail, including the two that do not attempt
// delivery at all.
func TestSESPermanentSubTypesAllSuppress(t *testing.T) {
	cases := map[string]string{
		"General":                  "General",
		"NoEmail":                  "NoEmail",
		"Suppressed":               "Suppressed",
		"OnAccountSuppressionList": "OnAccountSuppressionList",
		"UnsubscribedRecipient":    "unsubscribed",
	}
	for subType, wantNote := range cases {
		t.Run(subType, func(t *testing.T) {
			sw, _, _, _ := issue93Fixture(t)
			ctx := context.Background()
			addr := "perm-" + subType + "-" + randSuffix() + "@example.com"

			if !sw.handleEvent(ctx, bounceMsg(addr, "Permanent", subType)) {
				t.Fatal("Permanent bounce should be handled")
			}
			se := fetchSuppression(t, sw, addr)
			if se == nil {
				t.Fatalf("Permanent/%s must suppress the address", subType)
			}
			if se.Reason != dbstore.SuppressionReasonHardBounce {
				t.Errorf("reason = %q, want hard_bounce", se.Reason)
			}
			if se.Note != wantNote {
				t.Errorf("note = %q, want %q", se.Note, wantNote)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// soft bounces
// ---------------------------------------------------------------------------

// TestSESTransientBounceRule: the first and second Transient bounce leave the
// address mailable; the third inside the window suppresses it.
func TestSESTransientBounceRule(t *testing.T) {
	sw, _, _, pool := issue93Fixture(t)
	ctx := context.Background()
	addr := "soft-" + randSuffix() + "@example.com"

	for i := 1; i <= 2; i++ {
		if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
			t.Fatalf("transient bounce %d should be handled", i)
		}
		if se := fetchSuppression(t, sw, addr); se != nil {
			t.Fatalf("after %d Transient bounce(s) the address is suppressed (reason %q) — "+
				"soft bounces must not suppress before the 3rd", i, se.Reason)
		}
	}

	// The third event inside the window fires the rule.
	if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
		t.Fatal("third transient bounce should be handled")
	}
	se := fetchSuppression(t, sw, addr)
	if se == nil {
		t.Fatal("the 3rd Transient bounce in 7 days must suppress the address")
	}
	if se.Reason != dbstore.SuppressionReasonHardBounce {
		t.Errorf("reason = %q, want hard_bounce", se.Reason)
	}
	if se.Note != "repeated soft bounce (3 in 7d)" {
		t.Errorf("note = %q, want the tally", se.Note)
	}
	if n := suppressionRowCount(t, pool, addr); n != 1 {
		t.Fatalf("suppressed_emails rows = %d, want 1", n)
	}
}

// TestSESTransientBounceOutsideWindowDoesNotSuppress: the rule is a rolling
// 7-day window — three Transient bounces spread wider than that are three
// isolated incidents, not a dead mailbox. The already-recorded rows are aged in
// the table with SQL so the window is exercised against real timestamps (the
// store stamps created_at itself, so a test cannot choose it at insert time).
func TestSESTransientBounceOutsideWindowDoesNotSuppress(t *testing.T) {
	sw, _, _, pool := issue93Fixture(t)
	ctx := context.Background()
	addr := "oldsoft-" + randSuffix() + "@example.com"

	// Two bounces, then back-date them past the window.
	for i := 0; i < 2; i++ {
		if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
			t.Fatal("transient bounce should be handled")
		}
	}
	if _, err := pool.Exec(ctx,
		`UPDATE email_delivery_events SET created_at = now() - interval '8 days' WHERE email = $1`,
		addr); err != nil {
		t.Fatalf("age delivery events: %v", err)
	}

	// A third one now: only ONE event falls inside the 7-day window.
	if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
		t.Fatal("third transient bounce should be handled")
	}
	if se := fetchSuppression(t, sw, addr); se != nil {
		t.Fatalf("an address with only 1 Transient bounce inside 7 days was suppressed "+
			"(reason %q note %q) — the 2 older events are outside the window",
			se.Reason, se.Note)
	}
}

// TestSESTransientBounceCountingIsCaseInsensitive: the count must find events
// stored lower-cased even when a later event arrives with different casing,
// otherwise a real repeat-offender escapes the rule.
func TestSESTransientBounceCountingIsCaseInsensitive(t *testing.T) {
	sw, _, _, _ := issue93Fixture(t)
	ctx := context.Background()
	lower := "mixedcase-" + randSuffix() + "@example.com"
	upper := "MIXEDCASE-" + lower[len("mixedcase-"):]

	// Two events recorded under the lower-cased address...
	for i := 0; i < 2; i++ {
		if !sw.handleEvent(ctx, bounceMsg(lower, "Transient", "MailboxFull")) {
			t.Fatal("transient bounce should be handled")
		}
	}
	// ...then the third arrives with different casing.
	if !sw.handleEvent(ctx, bounceMsg(upper, "Transient", "MailboxFull")) {
		t.Fatal("third transient bounce should be handled")
	}
	se := fetchSuppression(t, sw, lower)
	if se == nil {
		t.Fatal("the 3rd Transient bounce was not counted across casing — " +
			"the tally query must normalise the address")
	}
	if se.Note != "repeated soft bounce (3 in 7d)" {
		t.Errorf("note = %q, want the tally", se.Note)
	}
}

// TestSESSoftBounceTallyOnlyCountsTransientBounces: the tally must be scoped to
// Transient bounce events. It becomes observable exactly where a real operator
// hits it — an address whose suppression was removed as a false positive, while
// the older, stronger signals are still inside the 7-day window.
//
// Without the scoping, the stale Permanent bounce (or complaint) events left in
// email_delivery_events would push the tally over the threshold on the very next
// Transient bounce and re-suppress an address a human had just re-enabled.
func TestSESSoftBounceTallyOnlyCountsTransientBounces(t *testing.T) {
	// Three Permanent bounces and three complaints, all on one address.
	for _, tc := range []struct {
		name string
		msg  func(string) *sesSNSMessage
	}{
		{"permanent", func(a string) *sesSNSMessage { return bounceMsg(a, "Permanent", "General") }},
		{"complaint", complaintMsg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sw, _, supps, _ := issue93Fixture(t)
			ctx := context.Background()
			addr := "stale-" + tc.name + "-" + randSuffix() + "@example.com"

			// Three events of the non-Transient kind (each independently
			// suppresses, which is why the address must be un-suppressed next).
			for i := 0; i < 3; i++ {
				if !sw.handleEvent(ctx, tc.msg(addr)) {
					t.Fatalf("event %d should be handled", i)
				}
			}
			if se := fetchSuppression(t, sw, addr); se == nil {
				t.Fatal("fixture: the seeded events should have suppressed the address")
			}

			// An operator decides it was a false positive and removes it.
			if err := supps.Remove(ctx, addr); err != nil {
				t.Fatalf("remove suppression: %v", err)
			}
			if se := fetchSuppression(t, sw, addr); se != nil {
				t.Fatal("fixture: the suppression should be gone")
			}

			// ONE Transient bounce now. It is the first soft bounce, so the
			// rule must NOT fire — the 3 stale events are not soft bounces.
			if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
				t.Fatal("transient bounce should be handled")
			}
			if se := fetchSuppression(t, sw, addr); se != nil {
				t.Fatalf("an address re-enabled by an operator was re-suppressed by the FIRST "+
					"Transient bounce (reason %q, note %q) — the tally counted %s events, "+
					"which are not soft bounces", se.Reason, se.Note, tc.name)
			}
		})
	}
}

// TestSESTransientBounceWithoutEventStoreNeverSuppresses: with no delivery-event
// store wired there is nothing to count, so the rule cannot fire. Pinned because
// a silent "suppress anyway" default would be a behaviour nobody asked for.
func TestSESTransientBounceWithoutEventStoreNeverSuppresses(t *testing.T) {
	pool := bundleTestPool(t)
	supps := dbstore.NewSuppressionStore(pool)
	sw := &suppressionWriter{Suppressions: supps} // EmailDeliveryEvents deliberately nil
	ctx := context.Background()
	addr := "noevents-" + randSuffix() + "@example.com"

	for i := 0; i < 5; i++ {
		if !sw.handleEvent(ctx, bounceMsg(addr, "Transient", "MailboxFull")) {
			t.Fatal("transient bounce should be handled")
		}
	}
	if se := fetchSuppression(t, sw, addr); se != nil {
		t.Fatalf("suppressed with no event store wired (reason %q)", se.Reason)
	}
}
