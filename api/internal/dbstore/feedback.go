// In-app feedback threads (system-owned, two-way conversation).
//
// A feedback thread is a private conversation between ONE user and the JomForm
// team. It is not a form submission and it is not tenant data: the row carries
// no account_id at all (see migration 000063 for why), so it can never appear
// inside a customer's form list, /sales view, CSV export or spam queue.
//
// TWO SCOPES, AND THEY ARE NOT INTERCHANGEABLE.
// Every read in this file takes an explicit scope argument and enforces it in
// SQL:
//
//	FeedbackScopeUser{UserID}   — the filer's own threads. The `user_id = $1`
//	                              predicate is part of the query, not a check
//	                              the caller is trusted to have done. This is
//	                              the ONLY scope the user-facing routes and
//	                              tools use.
//	FeedbackScopeAdmin{}        — every thread, for super-admin triage. Only the
//	                              super-admin-gated routes and tools use it.
//
// The point of the type is that there is no "all threads" read that a caller
// reaches by passing an empty string: a user-scoped read can never widen into a
// cross-user read by omitting an argument, because the argument is a scope
// value, and the user scope always binds its own user id.
//
// WHY THIS IS THE SECURITY-CRITICAL FILE.
// Customer B must never read customer A's thread. That is enforced here, in
// every query, and proven by an integration test that drives the real routes
// (feedback_isolation_issue615_test.go) rather than by reading this comment.
package dbstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Feedback statuses (migration 000063, feedback_threads_status_chk). The CHECK
// constraint and this set must agree: the REST/MCP layers reject an unknown
// value with 400 rather than letting Postgres raise a constraint error, and
// FeedbackStatuses is what the docs and the UI dropdown read.
const (
	FeedbackStatusOpen       = "open"
	FeedbackStatusInProgress = "in_progress"
	FeedbackStatusResolved   = "resolved"
	FeedbackStatusClosed     = "closed"
)

// FeedbackMessageAuthorUser / FeedbackMessageAuthorAdmin are the two author
// kinds (feedback_messages_author_kind_chk).
const (
	FeedbackMessageAuthorUser  = "user"
	FeedbackMessageAuthorAdmin = "admin"
)

// FeedbackStatuses lists every valid status, in triage order. The REST/MCP
// validation and the dashboard dropdown both read this, so the accepted set
// cannot drift from the CHECK constraint in the migration.
var FeedbackStatuses = []string{
	FeedbackStatusOpen,
	FeedbackStatusInProgress,
	FeedbackStatusResolved,
	FeedbackStatusClosed,
}

// ValidFeedbackStatus reports whether s is a status the CHECK constraint
// accepts. Callers validate with this to answer 400 instead of surfacing a
// Postgres constraint violation as a 500.
func ValidFeedbackStatus(s string) bool {
	for _, v := range FeedbackStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// FeedbackThread is one conversation. There is no AccountID field on purpose:
// see the file header. UserEmail is a display snapshot taken at filing time.
type FeedbackThread struct {
	ID             string
	UserID         string
	UserEmail      string
	Subject        string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	// MessageCount is populated by the list reads (it is what the triage list
	// and the user's list both show); zero on a bare ByID read.
	MessageCount int64
	// NotifyFailedAt / NotifyFailedKind / NotifyFailedError record the LAST
	// notification attempt for this thread that failed (migration 000070).
	// A nil timestamp is the normal state and means "no known failure" — which
	// includes every row written before this marker existed. It is cleared
	// again as soon as a later attempt succeeds.
	NotifyFailedAt    *time.Time
	NotifyFailedKind  string
	NotifyFailedError string
}

// FeedbackNotifyKindFiler / FeedbackNotifyKindAdmin name WHO was not told when a
// notification attempt failed (feedback_threads_notify_failed_kind_chk).
//
//	filer  the person who filed the thread was not told about an admin reply
//	admin  the platform team was not told a new thread was filed
const (
	FeedbackNotifyKindFiler = "filer"
	FeedbackNotifyKindAdmin = "admin"
)

// ValidFeedbackNotifyKind reports whether kind is a value the CHECK constraint
// accepts. Callers validate with this rather than letting Postgres raise a
// constraint violation the operator cannot read.
func ValidFeedbackNotifyKind(kind string) bool {
	return kind == FeedbackNotifyKindFiler || kind == FeedbackNotifyKindAdmin
}

// FeedbackMessage is one entry in a thread. AuthorKind is "user" or "admin";
// AuthorUserID is set for an admin reply and empty for the filer's own message.
type FeedbackMessage struct {
	ID           string
	ThreadID     string
	AuthorKind   string
	AuthorUserID string
	// AuthorEmail is resolved from `users` at read time for display. It is NOT
	// stored on the message: an admin's address changing should be reflected,
	// and a snapshot here would silently go stale in the one place an operator
	// looks to answer "who replied to this?".
	AuthorEmail string
	Body        string
	CreatedAt   time.Time
}

// FeedbackScope is the read/write scope for every feedback query. Exactly one
// of the two shapes is constructed; see the file header.
type FeedbackScope struct {
	// UserID non-empty ⇒ the filer's own threads only.
	UserID string
	// All true ⇒ every thread (super-admin triage). Mutually exclusive with
	// UserID, and constructed through FeedbackAdminScope so a caller does not
	// hand-roll a scope that reads as if it were user-scoped.
	All bool
}

// FeedbackUserScope scopes a query to one user's own threads.
func FeedbackUserScope(userID string) FeedbackScope {
	return FeedbackScope{UserID: userID}
}

// FeedbackAdminScope scopes a query to every thread (super-admin triage only).
func FeedbackAdminScope() FeedbackScope {
	return FeedbackScope{All: true}
}

// ErrFeedbackNotFound indicates a missing thread, or one the caller may not
// see. The two are deliberately the same error: a caller must not be able to
// tell "no such thread" from "not yours", or the 404 becomes an existence
// oracle for other users' thread ids.
var ErrFeedbackNotFound = errors.New("feedback thread not found")

// ErrFeedbackInvalidScope indicates a scope that cannot be used to scope a
// feedback query (neither a user id nor the admin scope). It exists so a
// mis-constructed scope fails loudly instead of silently matching nothing — or
// worse, matching everything.
var ErrFeedbackInvalidScope = errors.New("feedback: scope must name a user or be the admin scope")

// feedbackNotifyErrorMax bounds the recorded failure text (migration 000070).
// It is a display value on the triage card, not a log: 500 characters is enough
// for a queue/SES error and stops a pathological error becoming an unbounded row.
const feedbackNotifyErrorMax = 500

// FeedbackThreadStore wraps pgx for feedback_threads + feedback_messages.
type FeedbackThreadStore struct{ pool *pgxpool.Pool }

// NewFeedbackThreadStore builds a FeedbackThreadStore.
func NewFeedbackThreadStore(pool *pgxpool.Pool) *FeedbackThreadStore {
	return &FeedbackThreadStore{pool: pool}
}

// feedbackThreadSelect is the column list every feedback_threads read scans.
// Kept as one constant so the reads cannot drift apart column-wise.
//
// The notify-failure trio (migration 000070) is part of the standard read on
// purpose: the triage list shows the marker per row and the filer's own thread
// read reports it too, so a third reader would otherwise have to remember to
// fetch it — which is exactly how a visible marker stops being visible.
const feedbackThreadSelect = `SELECT t.id, t.user_id, t.user_email, t.subject, t.status,
	t.created_at, t.updated_at, t.last_activity_at,
	t.notify_failed_at, COALESCE(t.notify_failed_kind, ''), COALESCE(t.notify_failed_error, ''),
	(SELECT COUNT(*) FROM feedback_messages m WHERE m.thread_id = t.id)
	FROM feedback_threads t`

// scanFeedbackThread scans one row of feedbackThreadSelect, in that order. One
// function so the two read paths (Thread, ListFilteredPage) cannot disagree
// about the destination order.
func scanFeedbackThread(row pgx.Row) (*FeedbackThread, error) {
	th := &FeedbackThread{}
	if err := row.Scan(&th.ID, &th.UserID, &th.UserEmail, &th.Subject, &th.Status,
		&th.CreatedAt, &th.UpdatedAt, &th.LastActivityAt,
		&th.NotifyFailedAt, &th.NotifyFailedKind, &th.NotifyFailedError,
		&th.MessageCount); err != nil {
		return nil, err
	}
	return th, nil
}

// feedbackMessageSelect is the column list every feedback_messages read scans.
// The author's live email is joined in for display.
const feedbackMessageSelect = `SELECT m.id, m.thread_id, m.author_kind, COALESCE(m.author_user_id::text, ''),
	COALESCE(u.email::text, ''), m.body, m.created_at
	FROM feedback_messages m
	LEFT JOIN users u ON u.id = m.author_user_id`

// scopeClause builds the WHERE fragment (and its argument) that enforces a
// scope, plus the next free parameter index. An invalid scope is an error, never
// an empty predicate: an unfiltered read here would be a cross-user leak.
func (sc FeedbackScope) scopeClause(start int) (string, []any, int, error) {
	switch {
	case sc.All:
		return "true", nil, start, nil
	case sc.UserID != "":
		return "user_id = $" + itoa(start), []any{sc.UserID}, start + 1, nil
	default:
		return "", nil, start, ErrFeedbackInvalidScope
	}
}

// CreateThread files a new feedback thread from a user and records the first
// message in the same transaction, so a thread can never exist with no message
// for its list row to render.
//
// userEmail is the display snapshot; the caller passes the authenticated user's
// current address. An empty body is rejected: a thread with nothing in it is not
// a conversation and the triage list would show a blank row.
func (s *FeedbackThreadStore) CreateThread(ctx context.Context, userID, userEmail, subject, body string) (*FeedbackThread, error) {
	if userID == "" {
		return nil, ErrFeedbackInvalidScope
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	th := &FeedbackThread{
		UserID:    userID,
		UserEmail: userEmail,
		Subject:   subject,
		Status:    FeedbackStatusOpen,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO feedback_threads (user_id, user_email, subject)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at, last_activity_at`,
		userID, userEmail, subject,
	).Scan(&th.ID, &th.CreatedAt, &th.UpdatedAt, &th.LastActivityAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO feedback_messages (thread_id, author_kind, author_user_id, body)
		 VALUES ($1, $2, $3, $4)`,
		th.ID, FeedbackMessageAuthorUser, userID, body); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	th.MessageCount = 1
	return th, nil
}

// Thread returns one thread, scoped. A thread outside the scope returns
// ErrFeedbackNotFound — the same error as a missing id, so a caller cannot use
// the status code to test whether another user's thread exists.
func (s *FeedbackThreadStore) Thread(ctx context.Context, sc FeedbackScope, id string) (*FeedbackThread, error) {
	if id == "" {
		return nil, ErrFeedbackNotFound
	}
	clause, args, _, err := sc.scopeClause(2)
	if err != nil {
		return nil, err
	}
	args = append([]any{id}, args...)
	row := s.pool.QueryRow(ctx,
		feedbackThreadSelect+` WHERE t.id = $1 AND `+qualifyThreadClause(clause), args...)
	th, err := scanFeedbackThread(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFeedbackNotFound
		}
		return nil, err
	}
	return th, nil
}

// Messages returns a thread's messages oldest-first, scoped. An out-of-scope
// thread returns ErrFeedbackNotFound for the same reason Thread does: the read
// is guarded rather than trusting that the caller already resolved the thread.
//
// The scope predicate is evaluated in the same query as the messages, so there
// is no window in which a caller could read a thread it may not see.
func (s *FeedbackThreadStore) Messages(ctx context.Context, sc FeedbackScope, threadID string) ([]*FeedbackMessage, error) {
	if threadID == "" {
		return nil, ErrFeedbackNotFound
	}
	clause, args, _, err := sc.scopeClause(2)
	if err != nil {
		return nil, err
	}
	args = append([]any{threadID}, args...)
	rows, err := s.pool.Query(ctx,
		feedbackMessageSelect+
			` JOIN feedback_threads t ON t.id = m.thread_id
			  WHERE m.thread_id = $1 AND `+qualifyThreadClause(clause)+
			` ORDER BY m.created_at ASC, m.id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*FeedbackMessage{}
	for rows.Next() {
		m := &FeedbackMessage{}
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.AuthorKind, &m.AuthorUserID,
			&m.AuthorEmail, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListFilteredPage returns one page of threads, newest activity first, plus the
// total for the same filter. statusOf filters by status when non-empty; an
// unknown status is the caller's to validate (REST/MCP do it with
// ValidFeedbackStatus) — here a non-empty value is passed through, and the CHECK
// constraint is the backstop.
//
// The scope predicate is part of the WHERE clause, so the user scope cannot
// return another user's row even if the caller passes no other filter.
func (s *FeedbackThreadStore) ListFilteredPage(ctx context.Context, sc FeedbackScope, statusOf string, page, limit int) ([]*FeedbackThread, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	clause, args, next, err := sc.scopeClause(1)
	if err != nil {
		return nil, 0, err
	}
	where := qualifyThreadClause(clause)
	if statusOf != "" {
		args = append(args, statusOf)
		where += " AND t.status = $" + itoa(next)
		next++
	}

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM feedback_threads t WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	paged := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := s.pool.Query(ctx,
		feedbackThreadSelect+` WHERE `+where+
			` ORDER BY t.last_activity_at DESC, t.id DESC LIMIT $`+itoa(next)+
			` OFFSET $`+itoa(next+1), paged...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*FeedbackThread{}
	for rows.Next() {
		th, err := scanFeedbackThread(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, th)
	}
	return out, total, rows.Err()
}

// AddMessage appends a message to a thread and bumps the thread's activity
// timestamp, in one transaction so a reply can never land on a thread without
// moving it in the triage ordering.
//
// The scope is enforced BEFORE the insert (the thread is resolved through the
// same scoped read), so a caller cannot append a message to a thread it may not
// see even if it guessed the id. The insert itself therefore only ever touches a
// thread the scope already permitted.
//
// A reply by an admin additionally moves an OPEN thread to in_progress — an
// answer is a response to the report, and leaving it "open" would make the
// triage list lie about a thread already being worked. A thread the admin has
// already moved to resolved/closed is left at that status: the operator set it
// deliberately and a follow-up reply must not silently reopen their decision.
func (s *FeedbackThreadStore) AddMessage(ctx context.Context, sc FeedbackScope, threadID, authorKind, authorUserID, body string) (*FeedbackMessage, error) {
	if threadID == "" {
		return nil, ErrFeedbackNotFound
	}
	if authorKind != FeedbackMessageAuthorUser && authorKind != FeedbackMessageAuthorAdmin {
		return nil, errors.New("feedback: author_kind must be user or admin")
	}
	// Resolve first: an out-of-scope thread must fail here, before any write.
	if _, err := s.Thread(ctx, sc, threadID); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	m := &FeedbackMessage{ThreadID: threadID, AuthorKind: authorKind, AuthorUserID: authorUserID, Body: body}
	var authorArg any
	if authorUserID != "" {
		authorArg = authorUserID
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO feedback_messages (thread_id, author_kind, author_user_id, body)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		threadID, authorKind, authorArg, body,
	).Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, err
	}
	// Bump activity always; advance an open thread to in_progress on an admin
	// reply only (see the doc comment).
	status := ""
	if authorKind == FeedbackMessageAuthorAdmin {
		status = FeedbackStatusInProgress
	}
	if _, err := tx.Exec(ctx,
		`UPDATE feedback_threads
		    SET last_activity_at = now(),
		        updated_at       = now(),
		        status           = CASE WHEN $2 <> '' AND status = 'open'
		                                THEN $2 ELSE status END
		  WHERE id = $1`, threadID, status); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// SetStatus changes a thread's status, scoped. Returns ErrFeedbackNotFound when
// the scope does not cover the thread.
//
// Status is per thread, not per message (see migration 000063): the triage list
// asks one question per conversation, and the user's list renders one badge.
func (s *FeedbackThreadStore) SetStatus(ctx context.Context, sc FeedbackScope, threadID, status string) error {
	if threadID == "" {
		return ErrFeedbackNotFound
	}
	clause, args, _, err := sc.scopeClause(3)
	if err != nil {
		return err
	}
	// $1 = thread id, $2 = new status, $3.. = scope.
	full := append([]any{threadID, status}, args...)
	tag, err := s.pool.Exec(ctx,
		`UPDATE feedback_threads t SET status = $2, updated_at = now()
		  WHERE t.id = $1 AND `+qualifyThreadClause(clause), full...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// CountRecentByUser returns how many threads a user has filed since `since`
// (with at least one message, which CreateThread guarantees). It backs the
// per-user daily filing limit.
//
// This is the anti-flood mechanism that replaces the AI spam judge on this path
// (see the card: every filer is authenticated, so the threat is one account
// flooding, not bots — and a rate limit cannot misclassify a genuine complaint
// the way a classifier can).
func (s *FeedbackThreadStore) CountRecentByUser(ctx context.Context, userID string, since time.Time) (int64, error) {
	if userID == "" {
		return 0, ErrFeedbackInvalidScope
	}
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM feedback_threads WHERE user_id = $1 AND created_at >= $2`,
		userID, since).Scan(&n)
	return n, err
}

// CountByStatus returns thread counts per status across every thread (admin
// triage header). Deliberately admin-only: it aggregates the whole table, so it
// takes no scope.
func (s *FeedbackThreadStore) CountByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, COUNT(*) FROM feedback_threads GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for _, st := range FeedbackStatuses {
		out[st] = 0
	}
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// RecordNotifyFailure marks a thread as having an undelivered notification: the
// last attempt to tell `kind` about this thread failed with errText. It is the
// durable half of the fix for the silent-notification defect — the log line says
// it once, this row says it on the thread an operator actually opens.
//
// It is deliberately best-effort in the same sense the notification is: the
// caller logs and returns it, and a store failure never turns a stored admin
// reply into an error response. The marker is STATE ABOUT a notification, not
// the notification.
//
// The error text is truncated so a pathological error cannot become an
// unbounded row, and an empty kind/error is refused rather than written: a
// marker that does not say who was not told, or why, is not worth having.
func (s *FeedbackThreadStore) RecordNotifyFailure(ctx context.Context, sc FeedbackScope, threadID, kind, errText string) error {
	if threadID == "" {
		return ErrFeedbackNotFound
	}
	if !ValidFeedbackNotifyKind(kind) {
		return errors.New("feedback: notify failure kind must be filer or admin")
	}
	errText = strings.TrimSpace(errText)
	if errText == "" {
		errText = "unknown error"
	}
	// The cap is applied on a RUNE boundary, never on a byte index: errText is
	// a Go error string that can carry any UTF-8 (an accented hostname, a curly
	// quote, an em-dash), and slicing bytes at 500 can land mid-character. The
	// resulting invalid UTF-8 is refused by Postgres ("invalid byte sequence for
	// encoding UTF8", SQLSTATE 22021), so the UPDATE fails and NO marker is
	// written at all — the thread goes on looking healthy, which is the exact
	// silent-failure shape this marker exists to prevent.
	if r := []rune(errText); len(r) > feedbackNotifyErrorMax {
		errText = string(r[:feedbackNotifyErrorMax])
	}
	clause, args, _, err := sc.scopeClause(4)
	if err != nil {
		return err
	}
	// $1 = thread id, $2 = kind, $3 = error, $4.. = scope.
	full := append([]any{threadID, kind, errText}, args...)
	tag, err := s.pool.Exec(ctx,
		`UPDATE feedback_threads t
		    SET notify_failed_at = now(), notify_failed_kind = $2, notify_failed_error = $3
		  WHERE t.id = $1 AND `+qualifyThreadClause(clause), full...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// ClearNotifyFailure drops the marker after a later notification attempt FOR THE
// SAME KIND succeeded, so a thread that recovers stops advertising a failure that
// is no longer true. Scoped like every other write here.
//
// WHY THE KIND IS A PARAMETER. The two lanes are INDEPENDENT: the admin alert
// fires when a thread is filed, the filer alert when an admin replies, and either
// can be broken while the other works. A clear keyed on the thread alone therefore
// erases a failure the success knows nothing about — a filing the platform team
// was never told about disappears the moment an unrelated reply notification goes
// through, and no surface says so again (the marker is the only record). The
// predicate `notify_failed_kind = $2` is what makes "the marker clears on a later
// success" mean "of the same kind".
//
// RowsAffected is deliberately ignored: clearing an already-clear marker is the
// desired end state, and reporting "not found" for it would make a
// correctly-recovered thread look like a missing one. A marker belonging to the
// OTHER kind also matches no rows, which is the correct outcome, not an error.
func (s *FeedbackThreadStore) ClearNotifyFailure(ctx context.Context, sc FeedbackScope, threadID, kind string) error {
	if threadID == "" {
		return ErrFeedbackNotFound
	}
	if !ValidFeedbackNotifyKind(kind) {
		// Refuse rather than default to "clear everything": an empty or unknown
		// kind must never widen this into the thread-wide clear it replaces.
		return errors.New("feedback: notify failure kind must be filer or admin")
	}
	clause, args, _, err := sc.scopeClause(3)
	if err != nil {
		return err
	}
	// $1 = thread id, $2 = kind, $3.. = scope.
	full := append([]any{threadID, kind}, args...)
	if _, err := s.pool.Exec(ctx,
		`UPDATE feedback_threads t
		    SET notify_failed_at = NULL, notify_failed_kind = NULL, notify_failed_error = NULL
		  WHERE t.id = $1 AND notify_failed_at IS NOT NULL AND notify_failed_kind = $2 AND `+qualifyThreadClause(clause), full...); err != nil {
		return err
	}
	return nil
}

// qualifyThreadClause prefixes an unqualified scope fragment with the threads
// alias, so the same clause shape works in a query that also joins
// feedback_messages (where a bare `user_id` would be ambiguous against the
// messages' author_user_id).
func qualifyThreadClause(clause string) string {
	if clause == "true" {
		return clause
	}
	return strings.ReplaceAll(clause, "user_id", "t.user_id")
}
