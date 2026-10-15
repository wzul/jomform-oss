package dbstore

// The recorded gateway reason must survive a reason it cannot store verbatim.
//
// AddChargeFailedWithReason caps the CHIP failure text it writes into
// subscription_charges.failure_reason (migration 000057, issue #562) so a
// pathological upstream message cannot bloat the row. The cap used to be a BYTE
// slice — reason[:500] — and the reason is the gateway's raw error string,
// which carries arbitrary UTF-8: a localised provider message, a merchant or
// customer name with an accent, an em-dash. The cut then lands mid-character,
// and the resulting invalid UTF-8 makes Postgres refuse the whole INSERT
// ("invalid byte sequence for encoding UTF8", SQLSTATE 22021). This is not a
// cosmetic truncation: NO charge row is written at all, the caller only logs
// it (payments.recurring's sub_charge_reason_not_recorded) and moves on, so the
// subscription keeps failing with no visible cause on any surface — the exact
// silent-failure shape issue #562 added the column to prevent.
//
// The guard below feeds a reason whose 500th byte is the FIRST byte of a
// multi-byte rune (the smallest input that reproduces it) and asserts the row
// IS written, the stored text is valid UTF-8, and the cap still applies in
// CHARACTERS. The encoding refusal is the server's, so a mock cannot produce
// it: integration against a real Postgres, skipped without the standard test
// env. Isolation: testdb.Open gives this package its own scratch schema,
// re-created on every run.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAddChargeFailedWithReasonKeepsARuneBoundaryOnAMultiByteReason is the
// regression for the byte-slice cap on the recorded failure reason.
func TestAddChargeFailedWithReasonKeepsARuneBoundaryOnAMultiByteReason(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "reason-rune@example.test")

	// subChargeFailureReasonMax-1 ASCII bytes, then a 3-byte rune: byte 500
	// lands INSIDE that rune, which is what makes the byte slice invalid UTF-8.
	reason := strings.Repeat("x", subChargeFailureReasonMax-1) + "中" + strings.Repeat("y", 40)
	if !utf8.ValidString(reason) {
		t.Fatalf("fixture is not valid UTF-8 to begin with")
	}
	if utf8.ValidString(reason[:subChargeFailureReasonMax]) {
		t.Fatalf("fixture does not reproduce the defect: the byte slice at %d is still valid UTF-8, "+
			"so this test would pass against the buggy code too", subChargeFailureReasonMax)
	}

	if err := ss.AddChargeFailedWithReason(ctx, subID, 1500, 1, reason); err != nil {
		t.Fatalf("AddChargeFailedWithReason with a multi-byte reason straddling the %d-character cap: %v\n"+
			"the reason is the only record of why the recurring charge failed; an INSERT Postgres "+
			"refuses leaves the merchant with a failed subscription and no cause",
			subChargeFailureReasonMax, err)
	}

	var stored *string
	if err := p.QueryRow(ctx,
		`SELECT failure_reason FROM subscription_charges
		  WHERE subscription_id = $1 AND attempt = 1`, subID).Scan(&stored); err != nil {
		t.Fatalf("read the failure reason back: %v", err)
	}
	if stored == nil {
		t.Fatal("failure_reason is NULL — a failed charge recorded without its cause is the shape " +
			"issue #562 exists to prevent")
	}
	if !utf8.ValidString(*stored) {
		t.Fatalf("the stored failure reason is not valid UTF-8 (%q) — the truncation split a character",
			*stored)
	}
	// The cap still applies, in CHARACTERS: a character is 1..4 bytes, so the
	// byte length of the stored value is deliberately not capped.
	if got := utf8.RuneCountInString(*stored); got != subChargeFailureReasonMax {
		t.Errorf("stored failure reason is %d characters, want it truncated to %d",
			got, subChargeFailureReasonMax)
	}
	if !strings.HasPrefix(*stored, strings.Repeat("x", 100)) {
		t.Errorf("the stored reason lost its beginning: %q", *stored)
	}
}

// TestAddChargeFailedWithReasonStillCapsALongReason is the control that keeps
// the fix honest: a purely ASCII reason past the cap must still be truncated
// (to the cap), so the rune-aware slice cannot be "simplified" back into no
// bound at all.
func TestAddChargeFailedWithReasonStillCapsALongReason(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)
	acc := uniqueAccount(t, ctx, p)
	subID := newDueSubscriptionFixture(t, ctx, p, acc, "reason-cap@example.test")

	reason := strings.Repeat("chip: http 502: ", 100)
	if err := ss.AddChargeFailedWithReason(ctx, subID, 1500, 1, reason); err != nil {
		t.Fatalf("AddChargeFailedWithReason: %v", err)
	}

	var stored *string
	if err := p.QueryRow(ctx,
		`SELECT failure_reason FROM subscription_charges
		  WHERE subscription_id = $1 AND attempt = 1`, subID).Scan(&stored); err != nil {
		t.Fatalf("read the failure reason back: %v", err)
	}
	if stored == nil {
		t.Fatal("failure_reason is NULL")
	}
	if got := utf8.RuneCountInString(*stored); got != subChargeFailureReasonMax {
		t.Errorf("stored failure reason is %d characters, want %d — an unbounded reason row is "+
			"what the cap exists to stop", got, subChargeFailureReasonMax)
	}
}
