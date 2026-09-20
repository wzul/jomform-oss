// Package feedbacksvc is the ONE way a feedback thread gets filed.
//
// WHY IT EXISTS. Feedback can be filed from three places — the /feedback page
// (REST), an agent over MCP (`submit_feedback`), and the in-app AI chat — and a
// second filing path is how a guard silently stops applying: the per-user daily
// limit, the length bounds and the notification would each have to be repeated,
// and the first one to be forgotten is invisible. So the validation, the rate
// limit, the store write and the notify hook live here once, and every caller
// maps the typed errors below onto its own surface (HTTP status, tool error).
//
// WHAT IT IS NOT. It is not a second feedback store: it wraps
// dbstore.FeedbackThreadStore, which is system-owned and NOT account-scoped
// (a thread carries no account_id at all — migration 000063). A thread belongs
// to the PERSON who filed it, so every method here takes a user id and there is
// no argument that widens the read to another person's thread.
//
// THE ANTI-ABUSE GUARD IS A RATE LIMIT, NOT A CLASSIFIER. Every filer is an
// authenticated user, so the threat is one account flooding the queue — and a
// spam classifier can misclassify a genuine complaint, which is a far worse
// failure for a feedback channel than a delayed ticket. Wan's decision (see the
// in-app feedback card): no AI spam judge on this path.
package feedbacksvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

const (
	// BodyMin is the shortest accepted message. It exists to filter a stray
	// Enter press, not to judge content: a filer who has nothing to say should
	// get a validation message, not silence from a queue nobody reads.
	BodyMin = 10
	// BodyMax / SubjectMax bound what one filing can store and mail, so a
	// single request cannot become an unbounded row.
	BodyMax    = 5000
	SubjectMax = 200

	// DefaultDailyLimit is the per-user filing cap over a rolling 24h.
	//
	// It is a product decision, not a measured rate: more than 10 separate
	// reports in a day is either someone stress-testing the queue or a problem
	// that needs a conversation rather than 11 tickets. A rate limit cannot
	// "false-positive" a genuine complaint — it only delays it.
	DefaultDailyLimit = 10
)

// Typed refusals. Callers map these onto their own surface; a caller must not
// re-derive the rule from a string, because the message is user-facing copy and
// the rule is what is enforced.
var (
	ErrNotConfigured    = errors.New("sistem maklum balas tidak dikonfigurasi")
	ErrUserRequired     = errors.New("pengguna diperlukan untuk memfailkan maklum balas")
	ErrTooShort         = errors.New("sila tulis sekurang-kurangnya 10 aksara supaya kami faham")
	ErrTooLong          = errors.New("maklum balas terlalu panjang (maksimum 5000 aksara)")
	ErrSubjectTooLong   = errors.New("tajuk terlalu panjang (maksimum 200 aksara)")
	ErrDailyLimit       = errors.New("had maklum balas harian dicapai (10 sehari)")
	ErrStoreUnavailable = errors.New("gagal menyimpan maklum balas")
)

// ThreadStore is the slice of dbstore.FeedbackThreadStore this service uses.
//
// It is an interface so the SERVICE can be tested without a database, and so a
// caller cannot reach past the service into the store (the service is the one
// path; the interface names exactly what that path needs). The concrete store
// satisfies it unchanged — production always passes the real one.
type ThreadStore interface {
	// CountRecentByUser counts the filer's threads since a time — the daily cap.
	CountRecentByUser(ctx context.Context, userID string, since time.Time) (int64, error)
	// CreateThread stores one thread.
	CreateThread(ctx context.Context, userID, userEmail, subject, body string) (*dbstore.FeedbackThread, error)
}

// Service files feedback threads through one path. Zero value is not usable:
// Store is required.
type Service struct {
	// Store is the system-owned feedback thread store. Required.
	Store ThreadStore
	// Users resolves the filer's current email, stored as the display snapshot
	// on the thread. Optional: an unknown user files with an empty snapshot.
	Users *dbstore.UserStore
	// Notify is called AFTER a thread is stored, best-effort. A notification
	// failure must never fail the filing: the thread is filed and readable
	// either way. Optional.
	Notify func(th *dbstore.FeedbackThread, body string)
	// DailyLimit overrides the rolling-24h per-user cap. <= 0 uses
	// DefaultDailyLimit. It is a field so a test can drive the cap without
	// filing ten threads, and so a future product decision is one edit.
	DailyLimit int
}

// limit returns the effective daily cap.
func (s *Service) limit() int {
	if s.DailyLimit > 0 {
		return s.DailyLimit
	}
	return DefaultDailyLimit
}

// CreateThread validates, rate-limits, stores and (best-effort) notifies one
// thread for userID. It returns the stored thread so the caller can show the
// user that it happened, with an id they can come back to.
//
// A refused filing stores NOTHING: the limit is counted before the insert.
//
// The name is CreateThread, not File, on purpose: the MCP authorization guard
// classifies a call as a write by its method NAME (scope_gate_guard_test.go's
// writeVerbs). A method that stores a row but does not say so would let
// submit_feedback pass the guard as if it reached no store — the guard would then
// have nothing to prove about the chat's one write.
func (s *Service) CreateThread(ctx context.Context, userID, subject, body string) (*dbstore.FeedbackThread, error) {
	if s == nil || s.Store == nil {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserRequired
	}
	body = strings.TrimSpace(body)
	subject = strings.TrimSpace(subject)
	switch {
	case len(body) < BodyMin:
		return nil, ErrTooShort
	case len(body) > BodyMax:
		return nil, ErrTooLong
	case len(subject) > SubjectMax:
		return nil, ErrSubjectTooLong
	}

	// Counted BEFORE the insert so a rejected filing leaves no row behind, and
	// scoped to this user only — one account flooding cannot exhaust another
	// account's allowance.
	since := time.Now().Add(-24 * time.Hour)
	if n, err := s.Store.CountRecentByUser(ctx, userID, since); err != nil {
		return nil, err
	} else if n >= int64(s.limit()) {
		return nil, ErrDailyLimit
	}

	email := ""
	if s.Users != nil {
		if u, err := s.Users.ByID(ctx, userID); err == nil && u != nil {
			email = u.Email
		}
	}

	th, err := s.Store.CreateThread(ctx, userID, email, subject, body)
	if err != nil {
		return nil, err
	}
	if s.Notify != nil {
		s.Notify(th, body)
	}
	return th, nil
}

// HTTPStatusHint maps a typed refusal onto the HTTP status a REST caller should
// answer with. It lives here so REST and any future surface cannot disagree
// about which refusal is a 400 and which is a 429.
func HTTPStatusHint(err error) int {
	switch {
	case errors.Is(err, ErrTooShort), errors.Is(err, ErrTooLong), errors.Is(err, ErrSubjectTooLong):
		return 400 // http.StatusBadRequest
	case errors.Is(err, ErrDailyLimit):
		return 429 // http.StatusTooManyRequests
	case errors.Is(err, ErrNotConfigured), errors.Is(err, ErrStoreUnavailable):
		return 503 // http.StatusServiceUnavailable
	case errors.Is(err, ErrUserRequired):
		return 401 // http.StatusUnauthorized
	default:
		return 500 // http.StatusInternalServerError
	}
}
