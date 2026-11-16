package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func ptr(s string) *string { return &s }

// TestSubmitRestrictionAsksTheOnePredicate is the (state × submit) pin for the
// SUBMIT side (card t_305be5b1): the same row that makes the page show a
// pending/expired message must make a direct POST refuse, with the state whose
// message the page shows. It needs no stores — the window question is answered
// from the row and the clock alone.
func TestSubmitRestrictionAsksTheOnePredicate(t *testing.T) {
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	limit := 5
	blocksWithProduct := []any{
		map[string]any{"id": "b1", "type": "name"},
		map[string]any{"id": "b2", "type": "product", "product_id": "p-1"},
	}

	cases := []struct {
		name   string
		form   *dbstore.Form
		want   restrictionState
		refuse bool
	}{
		{
			name:   "within the window",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, StartAt: &start, EndAt: &end},
			want:   restrictionOpen,
			refuse: false,
		},
		{
			name:   "no schedule at all",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished},
			want:   restrictionOpen,
			refuse: false,
		},
		{
			// The two exact boundaries (at start_at, at end_at) are NOT pinned
			// here: this helper reads the wall clock, so "exactly at end" would be
			// "a microsecond after end" by the time the predicate looks. They are
			// pinned where the clock is injectable —
			// lifecycle.TestFormOrdersJudgesTheWindowAtThePostedInstant and the
			// matrix's two boundary rows.
			name:   "before start refuses as pending",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, StartAt: &end},
			want:   restrictionPending,
			refuse: true,
		},
		{
			name:   "after end refuses as expired",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, EndAt: &start},
			want:   restrictionExpired,
			refuse: true,
		},
		{
			// The entry limit needs a count, and with no store wired the count is
			// unknown (-1), which the predicate reads as "skip the arm" — never as
			// "reached". The limit itself is proven by the integration test that
			// posts to a real form.
			name:   "an unmeasurable entry limit is not a refusal",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, EntryLimit: &limit},
			want:   restrictionOpen,
			refuse: false,
		},
		{
			// The product rule is the order core's, not this gate's: a payment form
			// with nothing to sell must not be turned into a 409 here.
			name:   "a payment form with nothing to sell is not refused by this gate",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": "name"}}}},
			want:   restrictionOpen,
			refuse: false,
		},
		{
			name:   "a sellable payment form is not refused",
			form:   &dbstore.Form{Status: dbstore.FormStatusPublished, Config: map[string]any{"blocks": blocksWithProduct}},
			want:   restrictionOpen,
			refuse: false,
		},
	}
	h := &PublicHandlers{}
	for _, c := range cases {
		got, refused := h.submitRestriction(context.Background(), c.form)
		if refused != c.refuse {
			t.Errorf("%s: refused = %v, want %v (state %v)", c.name, refused, c.refuse, got)
			continue
		}
		if got != c.want {
			t.Errorf("%s: state = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRestrictionMessageOverride(t *testing.T) {
	h := &PublicHandlers{}
	f := &dbstore.Form{
		Config:         map[string]any{"lang": "en"},
		PendingMessage: ptr("custom pending"),
		ExpiredMessage: ptr("custom expired"),
		LimitMessage:   ptr("custom limit"),
	}
	r := httptest.NewRequest("GET", "/", nil)
	if got := h.restrictionMessage(r, f, restrictionPending); got != "custom pending" {
		t.Fatalf("pending override: got %q", got)
	}
	if got := h.restrictionMessage(r, f, restrictionExpired); got != "custom expired" {
		t.Fatalf("expired override: got %q", got)
	}
	if got := h.restrictionMessage(r, f, restrictionLimitReached); got != "custom limit" {
		t.Fatalf("limit override: got %q", got)
	}
	// Empty override → language default.
	f2 := &dbstore.Form{Config: map[string]any{"lang": "en"}}
	if got := h.restrictionMessage(r, f2, restrictionPending); got == "" {
		t.Fatalf("expected a default pending message, got empty")
	}
}
