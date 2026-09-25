package dbstore

// The notification-failure marker must survive an error text it cannot store
// verbatim.
//
// RecordNotifyFailure caps the recorded error so a pathological error cannot
// become an unbounded row. The cap used to be a BYTE slice — errText[:500] — and
// a Go error string carries arbitrary UTF-8 (an accented hostname, a curly
// quote, an em-dash, a localised message): the cut can land mid-character, and
// the resulting invalid UTF-8 makes Postgres refuse the whole UPDATE
// ("invalid byte sequence for encoding UTF8", SQLSTATE 22021). "Refuse the
// UPDATE" is not a cosmetic loss of error text: NO marker is written at all, so
// the thread goes on looking healthy to the operator — the exact silent-failure
// shape migration 000070 exists to prevent.
//
// The guard below feeds text whose 500th BYTE is inside a multi-byte rune, which
// is the smallest input that reproduces it, and asserts the marker IS written
// and the STORED text is valid UTF-8. Integration against a real Postgres (the
// encoding refusal is the server's, so a mock cannot produce it), skipped
// without the standard test env.
//
// Isolation: testdb.Open gives this package its own scratch schema, re-created
// on every run.

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wzul/jomform/api/internal/testdb"
)

// TestClearNotifyFailureIsScopedToItsKind is the store-level half of the
// kind-scoped clear.
//
// The two notification lanes are independent — the admin alert fires on filing,
// the filer alert on an admin reply — and either can be broken while the other
// works. The marker is a single row on the thread, so a clear that does not name
// a kind erases whichever failure was recorded, including one the success knows
// nothing about: a filing the platform team was never told about vanishes the
// moment an unrelated reply notification goes through, and nothing on any surface
// says so again.
//
// This pins the predicate itself (the HTTP-level reproduction is
// TestFeedbackNotifyFilerSuccessDoesNotEraseAnAdminFailure), including the
// direction that must NOT be narrowed away: a marker of the SAME kind still
// clears, or the fix would trade a lost marker for a permanent one.
func TestClearNotifyFailureIsScopedToItsKind(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	testdb.Reset(t, p, "feedback_threads", "feedback_messages")

	userID := testOwnerUser(t, ctx, p)
	store := NewFeedbackThreadStore(p)

	newThread := func(subject string) string {
		t.Helper()
		var id string
		if err := p.QueryRow(ctx,
			`INSERT INTO feedback_threads (user_id, user_email, subject)
			 VALUES ($1, 'filer@example.test', $2) RETURNING id`, userID, subject).Scan(&id); err != nil {
			t.Fatalf("create thread: %v", err)
		}
		return id
	}
	readMarker := func(id string) (string, *string) {
		t.Helper()
		var kind, errText *string
		if err := p.QueryRow(ctx,
			`SELECT notify_failed_kind, notify_failed_error FROM feedback_threads WHERE id = $1`,
			id).Scan(&kind, &errText); err != nil {
			t.Fatalf("read marker: %v", err)
		}
		if kind == nil {
			return "", nil
		}
		return *kind, errText
	}

	// ── the admin marker survives a FILER-lane clear ──
	adminThread := newThread("Gagal dimaklumkan kepada pasukan")
	if err := store.RecordNotifyFailure(ctx, FeedbackAdminScope(), adminThread,
		FeedbackNotifyKindAdmin, "enqueue feedback_notify: connection refused"); err != nil {
		t.Fatalf("RecordNotifyFailure(admin): %v", err)
	}
	if err := store.ClearNotifyFailure(ctx, FeedbackAdminScope(), adminThread,
		FeedbackNotifyKindFiler); err != nil {
		t.Fatalf("ClearNotifyFailure(filer): %v", err)
	}
	kind, errText := readMarker(adminThread)
	if kind != FeedbackNotifyKindAdmin {
		t.Fatalf("after clearing the FILER lane the marker is %q, want it still %q — the platform "+
			"team's missed filing was erased by a success on an unrelated lane",
			kind, FeedbackNotifyKindAdmin)
	}
	if errText == nil {
		t.Error("the admin marker kept its kind but lost its error text")
	}

	// ── the same kind still clears (the fix must not narrow clearing away) ──
	if err := store.ClearNotifyFailure(ctx, FeedbackAdminScope(), adminThread,
		FeedbackNotifyKindAdmin); err != nil {
		t.Fatalf("ClearNotifyFailure(admin): %v", err)
	}
	if kind, _ := readMarker(adminThread); kind != "" {
		t.Errorf("marker kind = %q after clearing its OWN kind, want it cleared", kind)
	}

	// ── an unknown/empty kind must not widen into a thread-wide clear ──
	filerThread := newThread("Balasan tidak sampai kepada pemfail")
	if err := store.RecordNotifyFailure(ctx, FeedbackAdminScope(), filerThread,
		FeedbackNotifyKindFiler, "enqueue feedback_notify: connection refused"); err != nil {
		t.Fatalf("RecordNotifyFailure(filer): %v", err)
	}
	if err := store.ClearNotifyFailure(ctx, FeedbackAdminScope(), filerThread, ""); err == nil {
		t.Error("ClearNotifyFailure accepted an EMPTY kind — an unknown kind must be refused, not " +
			"silently treated as \"clear whichever marker is there\"")
	}
	if kind, _ := readMarker(filerThread); kind != FeedbackNotifyKindFiler {
		t.Errorf("marker kind = %q after a clear with an empty kind, want it untouched (%q)",
			kind, FeedbackNotifyKindFiler)
	}
}

// TestRecordNotifyFailureKeepsARuneBoundaryOnAMultiByteError is the regression
// for the byte-slice cap.
func TestRecordNotifyFailureKeepsARuneBoundaryOnAMultiByteError(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	testdb.Reset(t, p, "feedback_threads", "feedback_messages")

	userID := testOwnerUser(t, ctx, p)
	var threadID string
	if err := p.QueryRow(ctx,
		`INSERT INTO feedback_threads (user_id, user_email, subject)
		 VALUES ($1, 'filer@example.test', 'Pembayaran tersekat') RETURNING id`,
		userID).Scan(&threadID); err != nil {
		t.Fatalf("create thread: %v", err)
	}

	// feedbackNotifyErrorMax-1 ASCII bytes, then a 3-byte rune: byte 500 lands
	// INSIDE that rune, which is what makes the byte slice invalid UTF-8.
	errText := strings.Repeat("a", feedbackNotifyErrorMax-1) + "中中中" + strings.Repeat("b", 50)
	if utf8.ValidString(errText) != true {
		t.Fatalf("fixture is not valid UTF-8 to begin with")
	}
	if utf8.ValidString(errText[:feedbackNotifyErrorMax]) {
		t.Fatalf("fixture does not reproduce the defect: the byte slice at %d is still valid UTF-8, "+
			"so this test would pass against the buggy code too", feedbackNotifyErrorMax)
	}

	store := NewFeedbackThreadStore(p)
	if err := store.RecordNotifyFailure(ctx, FeedbackAdminScope(), threadID,
		FeedbackNotifyKindFiler, errText); err != nil {
		t.Fatalf("RecordNotifyFailure with a multi-byte error straddling the %d-character cap: %v\n"+
			"the marker is meant to be the durable record of a failed notification; a write that "+
			"Postgres refuses leaves the thread looking healthy", feedbackNotifyErrorMax, err)
	}

	var at *time.Time
	var kind, stored *string
	if err := p.QueryRow(ctx,
		`SELECT notify_failed_at, notify_failed_kind, notify_failed_error
		   FROM feedback_threads WHERE id = $1`, threadID).Scan(&at, &kind, &stored); err != nil {
		t.Fatalf("read the marker back: %v", err)
	}
	if at == nil {
		t.Fatal("no failure marker was written — an operator opening this thread sees a healthy " +
			"notification that never left the building")
	}
	if kind == nil || *kind != FeedbackNotifyKindFiler {
		t.Fatalf("notify_failed_kind = %v, want %q", kind, FeedbackNotifyKindFiler)
	}
	if stored == nil {
		t.Fatal("notify_failed_error is NULL")
	}
	if !utf8.ValidString(*stored) {
		t.Fatalf("the stored error text is not valid UTF-8 (%q) — the truncation split a character",
			*stored)
	}
	// The cap still applies, in CHARACTERS: this is a display value on the triage
	// card, and the point of the fix is that the cut is legal, not that the cap
	// was dropped.
	if got := utf8.RuneCountInString(*stored); got != feedbackNotifyErrorMax {
		t.Errorf("stored error text is %d characters, want it truncated to %d",
			got, feedbackNotifyErrorMax)
	}
	if !strings.HasPrefix(*stored, strings.Repeat("a", 100)) {
		t.Errorf("the stored text lost its beginning: %q", *stored)
	}
}

// TestRecordNotifyFailureStillCapsALongError is the control that keeps the fix
// honest: a purely ASCII error past the cap must still be truncated (to the cap),
// so the rune-aware slice cannot be "simplified" back into no bound at all.
func TestRecordNotifyFailureStillCapsALongError(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	testdb.Reset(t, p, "feedback_threads", "feedback_messages")

	userID := testOwnerUser(t, ctx, p)
	var threadID string
	if err := p.QueryRow(ctx,
		`INSERT INTO feedback_threads (user_id, user_email, subject)
		 VALUES ($1, 'filer@example.test', 'Panjang') RETURNING id`,
		userID).Scan(&threadID); err != nil {
		t.Fatalf("create thread: %v", err)
	}

	store := NewFeedbackThreadStore(p)
	errText := strings.Repeat("x", feedbackNotifyErrorMax*3)
	if err := store.RecordNotifyFailure(ctx, FeedbackAdminScope(), threadID,
		FeedbackNotifyKindAdmin, errText); err != nil {
		t.Fatalf("RecordNotifyFailure: %v", err)
	}

	var stored string
	if err := p.QueryRow(ctx,
		`SELECT notify_failed_error FROM feedback_threads WHERE id = $1`, threadID).Scan(&stored); err != nil {
		t.Fatalf("read the marker back: %v", err)
	}
	if utf8.RuneCountInString(stored) != feedbackNotifyErrorMax {
		t.Errorf("stored error text is %d characters, want %d — an unbounded error row is what the "+
			"cap exists to stop", utf8.RuneCountInString(stored), feedbackNotifyErrorMax)
	}
}
