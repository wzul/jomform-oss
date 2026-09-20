// The shared feedback filing service, without a database.
//
// WHY THIS PACKAGE'S TESTS MATTER HERE. The service exists so that the
// /feedback page, the MCP submit_feedback tool and the in-app chat cannot each
// grow their own copy of the rules. A test that only drove it through the chat
// would leave the service's own contract unpinned — and the contract is the
// thing the other two callers depend on. So the rules are pinned HERE, at the
// source, and the callers' tests assert only that they go through it.
//
// WHAT IS PINNED
//
//  1. A refused filing stores NOTHING. The daily count is checked BEFORE the
//     insert, so a user who hits the cap leaves no row behind. The opposite
//     shape (insert, then check) is invisible to the user and pollutes the
//     triage queue with the very filings the limit rejected.
//  2. The daily cap is PER USER. One account flooding cannot consume another
//     account's allowance, and the count is asked for the filer's own id.
//  3. The length rules are the ones the /feedback page and the docs quote.
//  4. The notify hook is called AFTER the row exists and its failure can never
//     fail the filing (the thread is filed and readable either way).
//  5. The typed refusals map to the HTTP statuses the REST edge answers with —
//     one mapping, so REST and any future surface cannot disagree.
package feedbacksvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// recordingStore is the fake ThreadStore. It records the args of each call so
// "did the service ask for THIS user's count?" is observable, not inferred.
type recordingStore struct {
	countedUserIDs []string
	created        []struct{ userID, email, subject, body string }
	recent         int64
	countErr       error
	createErr      error
	// createID is the id the fake returns (the DB generates a uuid).
	createID string
}

func (s *recordingStore) CountRecentByUser(_ context.Context, userID string, since time.Time) (int64, error) {
	s.countedUserIDs = append(s.countedUserIDs, userID)
	// The window must be ~24h: a service that asked for "since zero" would count
	// every filing ever and the cap would be a lifetime limit.
	if d := time.Since(since); d < 23*time.Hour || d > 25*time.Hour {
		return 0, errors.New("CountRecentByUser window is not ~24h: " + d.String())
	}
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.recent, nil
}

func (s *recordingStore) CreateThread(_ context.Context, userID, email, subject, body string) (*dbstore.FeedbackThread, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	id := s.createID
	if id == "" {
		id = "11111111-2222-3333-4444-555555555555"
	}
	s.created = append(s.created, struct{ userID, email, subject, body string }{userID, email, subject, body})
	return &dbstore.FeedbackThread{
		ID: id, UserID: userID, UserEmail: email, Subject: subject, Status: "open",
	}, nil
}

func newService(store ThreadStore) *Service { return &Service{Store: store} }

// TestCreateThreadStoresOneThread is the happy path: the row is written with the
// filer's id and the trimmed content.
func TestCreateThreadStoresOneThread(t *testing.T) {
	store := &recordingStore{}
	svc := newService(store)

	th, err := svc.CreateThread(context.Background(), "user-1", "  Resit tidak sampai  ", "  Pelanggan kata resit tidak diterima.  ")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if th == nil || th.ID == "" {
		t.Fatal("CreateThread returned no thread id — the user cannot come back to it")
	}
	if len(store.created) != 1 {
		t.Fatalf("the store saw %d threads, want 1", len(store.created))
	}
	got := store.created[0]
	if got.userID != "user-1" {
		t.Fatalf("stored userID = %q, want user-1 (a thread belongs to the person who filed it)", got.userID)
	}
	if got.subject != "Resit tidak sampai" || got.body != "Pelanggan kata resit tidak diterima." {
		t.Fatalf("stored subject/body = %q/%q, want them trimmed", got.subject, got.body)
	}
}

// TestRefusedFilingStoresNothing is rule 1: the cap is checked before the insert.
//
// The failure this prevents is specific: an insert-then-check would leave a row
// for every filing the limit rejected, so the queue would fill with exactly the
// filings the limit was there to stop, and the user would be told "refused" while
// the thread exists and a notification went out.
func TestRefusedFilingStoresNothing(t *testing.T) {
	store := &recordingStore{recent: DefaultDailyLimit}
	svc := newService(store)

	_, err := svc.CreateThread(context.Background(), "user-1", "tajuk", "kandungan yang cukup panjang")
	if !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("err = %v, want ErrDailyLimit", err)
	}
	if len(store.created) != 0 {
		t.Fatalf("a refused filing stored %d thread(s) — the count must be checked BEFORE the "+
			"insert", len(store.created))
	}
}

// TestDailyCapIsPerUser is rule 2: the count is asked for the filer's own id, so
// one account flooding cannot consume another's allowance.
func TestDailyCapIsPerUser(t *testing.T) {
	store := &recordingStore{}
	svc := newService(store)

	if _, err := svc.CreateThread(context.Background(), "user-A", "t", "kandungan yang cukup panjang"); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if len(store.countedUserIDs) != 1 {
		t.Fatalf("the store was asked for %d counts, want 1", len(store.countedUserIDs))
	}
	if store.countedUserIDs[0] != "user-A" {
		t.Fatalf("the count was asked for %q, want user-A — a count for the wrong id makes the "+
			"cap either a shared bucket or no cap at all", store.countedUserIDs[0])
	}
}

// TestLengthRulesAreTheQuotedOnes is rule 3: the same minimum/maximum the page
// and the docs state. A drift here is invisible — the user is refused by a rule
// the UI never told them.
func TestLengthRulesAreTheQuotedOnes(t *testing.T) {
	svc := newService(&recordingStore{})

	// Exactly at the minimum passes.
	if _, err := svc.CreateThread(context.Background(), "u", "t", strings.Repeat("a", BodyMin)); err != nil {
		t.Fatalf("a body of exactly %d chars was refused: %v — the UI tells users %d is enough",
			BodyMin, err, BodyMin)
	}
	// One under is refused.
	if _, err := svc.CreateThread(context.Background(), "u", "t", strings.Repeat("a", BodyMin-1)); !errors.Is(err, ErrTooShort) {
		t.Fatalf("a body of %d chars = %v, want ErrTooShort", BodyMin-1, err)
	}
	// At the maximum passes; one over is refused (the row and the email are
	// bounded by this).
	if _, err := svc.CreateThread(context.Background(), "u", "t", strings.Repeat("a", BodyMax)); err != nil {
		t.Fatalf("a body of exactly %d chars was refused: %v", BodyMax, err)
	}
	if _, err := svc.CreateThread(context.Background(), "u", "t", strings.Repeat("a", BodyMax+1)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("a body of %d chars = %v, want ErrTooLong", BodyMax+1, err)
	}
	// A whitespace-only body is too short, not stored as blank.
	if _, err := svc.CreateThread(context.Background(), "u", "t", strings.Repeat(" ", BodyMin+5)); !errors.Is(err, ErrTooShort) {
		t.Fatalf("a whitespace-only body = %v, want ErrTooShort", err)
	}
	// The subject is bounded too.
	if _, err := svc.CreateThread(context.Background(), "u", strings.Repeat("s", SubjectMax+1),
		strings.Repeat("a", BodyMin)); !errors.Is(err, ErrSubjectTooLong) {
		t.Fatalf("an over-long subject = %v, want ErrSubjectTooLong", err)
	}
}

// TestMissingFilerIsRefused: a filing with no user id must be refused, never
// stored against an empty filer. The MCP and chat paths both file on someone's
// behalf, and "nobody" is not someone.
func TestMissingFilerIsRefused(t *testing.T) {
	store := &recordingStore{}
	svc := newService(store)

	for _, uid := range []string{"", "   "} {
		if _, err := svc.CreateThread(context.Background(), uid, "t", "kandungan yang cukup panjang"); !errors.Is(err, ErrUserRequired) {
			t.Fatalf("CreateThread(%q) = %v, want ErrUserRequired", uid, err)
		}
	}
	if len(store.created) != 0 {
		t.Fatalf("a filing with no filer stored %d thread(s)", len(store.created))
	}
}

// TestUnconfiguredServiceRefuses: a nil service or a nil store is a typed
// refusal, not a panic and not a silent success.
func TestUnconfiguredServiceRefuses(t *testing.T) {
	var nilSvc *Service
	if _, err := nilSvc.CreateThread(context.Background(), "u", "t", "kandungan yang cukup panjang"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil service = %v, want ErrNotConfigured", err)
	}
	if _, err := newService(nil).CreateThread(context.Background(), "u", "t", "kandungan yang cukup panjang"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil store = %v, want ErrNotConfigured", err)
	}
}

// TestNotifyRunsAfterTheRowExistsAndCannotFailTheFiling is rule 4.
//
// The order matters: a notification for a thread that was never stored sends the
// team to look at a thread that does not exist. And a mail failure must not turn
// a successful filing into an error the user sees — they would file again.
func TestNotifyRunsAfterTheRowExistsAndCannotFailTheFiling(t *testing.T) {
	store := &recordingStore{}
	svc := newService(store)
	var notifiedSubject string
	var notifiedWhenStored int
	svc.Notify = func(th *dbstore.FeedbackThread, body string) {
		notifiedSubject = th.Subject
		// The store must already have the row when the hook runs.
		notifiedWhenStored = len(store.created)
	}

	th, err := svc.CreateThread(context.Background(), "u", "Resit", "Pelanggan kata resit tidak diterima.")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if notifiedWhenStored != 1 {
		t.Fatalf("Notify ran when the store held %d thread(s) — the team would be told about a "+
			"thread that does not exist yet", notifiedWhenStored)
	}
	if notifiedSubject != "Resit" || notifiedSubject != th.Subject {
		t.Fatalf("Notify saw subject %q, want %q", notifiedSubject, th.Subject)
	}
}

// TestStoreErrorsAreReturnedNotSwallowed: a count or insert failure must reach
// the caller. A swallowed error would answer "filed" for a thread that was never
// stored — the worst possible outcome for a feedback channel.
func TestStoreErrorsAreReturnedNotSwallowed(t *testing.T) {
	boom := errors.New("db down")
	if _, err := newService(&recordingStore{countErr: boom}).CreateThread(
		context.Background(), "u", "t", "kandungan yang cukup panjang"); !errors.Is(err, boom) {
		t.Fatalf("a count failure = %v, want it returned", err)
	}
	if _, err := newService(&recordingStore{createErr: boom}).CreateThread(
		context.Background(), "u", "t", "kandungan yang cukup panjang"); !errors.Is(err, boom) {
		t.Fatalf("an insert failure = %v, want it returned", err)
	}
}

// TestHTTPStatusHintIsTheWholeMapping is rule 5: REST and any future surface
// must not disagree about which refusal is a 400 and which is a 429.
func TestHTTPStatusHintIsTheWholeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrTooShort, 400},
		{ErrTooLong, 400},
		{ErrSubjectTooLong, 400},
		{ErrDailyLimit, 429},
		{ErrNotConfigured, 503},
		{ErrStoreUnavailable, 503},
		{ErrUserRequired, 401},
		{errors.New("something else"), 500},
	}
	for _, tc := range cases {
		if got := HTTPStatusHint(tc.err); got != tc.want {
			t.Errorf("HTTPStatusHint(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
	// Wrapped errors must still classify: a caller that wraps with context (the
	// usual Go idiom) would otherwise fall through to 500 and the user would see
	// "server error" for their own too-short message.
	if got := HTTPStatusHint(errJoin(ErrDailyLimit, errors.New("context"))); got != 429 {
		t.Errorf("a wrapped ErrDailyLimit = %d, want 429", got)
	}
}

// errJoin wraps two errors the way errors.Join does, kept explicit so the test
// states what it is checking.
func errJoin(a, b error) error { return errors.Join(a, b) }
