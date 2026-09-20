package chat

import (
	"context"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
)

// fakeFeedbackStore is the smallest ThreadStore the chat tests need: it counts
// filings and records the last one, so "did the chat file through the shared
// service?" is directly observable without a database.
//
// WHY NOT A REAL POSTGRES HERE. What this file's tests own is the chat's WIRING
// (which path a submit_feedback call takes, and that the shared validation still
// applies). The store's own behaviour — the rolling-24h count, the row it writes,
// the email snapshot — is proven by feedbacksvc's tests and the REST integration
// tests against a real Postgres. A database here would add runtime, not coverage.
type fakeFeedbackStore struct {
	// recent is what CountRecentByUser reports (the cap check).
	recent int64
	// filed counts successful CreateThread calls.
	filed int
	// last is the last stored thread.
	last *dbstore.FeedbackThread
	// errCreate, when set, fails CreateThread (the store-unavailable path).
	errCreate error
}

// store returns the value to hand to feedbacksvc.Service.
func (f *fakeFeedbackStore) store() feedbacksvc.ThreadStore { return f }

func (f *fakeFeedbackStore) CountRecentByUser(_ context.Context, _ string, _ time.Time) (int64, error) {
	return f.recent, nil
}

func (f *fakeFeedbackStore) CreateThread(_ context.Context, userID, email, subject, body string) (*dbstore.FeedbackThread, error) {
	if f.errCreate != nil {
		return nil, f.errCreate
	}
	f.filed++
	f.last = &dbstore.FeedbackThread{
		ID:        "11111111-2222-3333-4444-555555555555",
		UserID:    userID,
		UserEmail: email,
		Subject:   subject,
		Status:    "open",
	}
	return f.last, nil
}

// newFakeFeedbackStore builds an empty fake.
func newFakeFeedbackStore() *fakeFeedbackStore { return &fakeFeedbackStore{} }
