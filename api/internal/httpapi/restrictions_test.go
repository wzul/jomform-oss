package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func ptr(s string) *string { return &s }

func TestScheduleState(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)

	// No schedule → always open.
	if st := scheduleState(&dbstore.Form{}, now); st != restrictionOpen {
		t.Fatalf("no schedule: got %v, want open", st)
	}
	// Only start set, now before start → pending.
	f := &dbstore.Form{StartAt: &start}
	if st := scheduleState(f, now.Add(-2*time.Hour)); st != restrictionPending {
		t.Fatalf("before start: got %v, want pending", st)
	}
	// Only end set, now after end → expired.
	f = &dbstore.Form{EndAt: &end}
	if st := scheduleState(f, now.Add(2*time.Hour)); st != restrictionExpired {
		t.Fatalf("after end: got %v, want expired", st)
	}
	// Within the window → open.
	f = &dbstore.Form{StartAt: &start, EndAt: &end}
	if st := scheduleState(f, now); st != restrictionOpen {
		t.Fatalf("within window: got %v, want open", st)
	}
	// Exactly at start → open (not before).
	if st := scheduleState(f, start); st != restrictionOpen {
		t.Fatalf("at start: got %v, want open", st)
	}
	// Exactly at end → open (not after).
	if st := scheduleState(f, end); st != restrictionOpen {
		t.Fatalf("at end: got %v, want open", st)
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
