// MCP tools for in-app feedback threads (parity with the dashboard surface).
//
// FOUR TOOLS, MATCHING THE FOUR DASHBOARD ACTIONS:
//
//	list_feedback        the caller's own threads (or all of them for a
//	                     super-admin, who gets the triage view)
//	get_feedback_thread  one thread + its messages
//	reply_feedback       append a message to a thread
//	set_feedback_status  move a thread's status (super-admin only)
//
// SCOPING IS THE WHOLE POINT OF THIS FILE. Unlike every other tool in this
// package, a feedback tool does NOT resolve a workspace/account scope: a
// feedback thread belongs to the PERSON who filed it and carries no account_id
// at all (migration 000063). So `list_feedback` and `get_feedback_thread` scope
// to the authenticated USER id — never to the account set — and an agent
// connected to a workspace cannot reach another person's thread by naming a
// workspace.
//
// A super-admin additionally gets the triage view (every thread) from the same
// tools, which is what keeps MCP parity real rather than nominal: the admin can
// list, read, reply and change status over MCP exactly as on the dashboard.
// That widening is gated on `isSuperAdmin`, resolved from the caller's own
// email against the same allowlist the REST admin routes use.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// feedbackScopeFor picks the store scope for the caller: the admin scope for a
// super-admin (triage), otherwise the caller's own user scope.
//
// AN API KEY IS REFUSED, AND THAT IS THE POINT. `userIDFor` resolves an
// API-key caller to its ACCOUNT OWNER's user id (an API key carries no user —
// see mcpsrv/http.go authenticate), which is the correct convention for
// account-scoped tools: a key on account X acts on account X's data.
//
// A feedback thread is NOT account-scoped data. It belongs to the PERSON who
// filed it. Letting the fallback apply here would silently convert an
// account-scoped credential into a person-scoped one: anyone who can obtain a
// key on the victim's account — a `manage` member of the victim's workspace can
// mint one via POST /v1/rest/keys, which is gated only on manage/admin — would
// read and write the victim's private conversations with JomForm. Same for the
// super-admin branch: `isSuperAdmin` resolves an API-key caller by its account
// owner's email, so a key on a super-admin's account would open the whole
// triage queue.
//
// So a caller with no USER identity is refused outright rather than mapped to
// somebody else's. Triage over MCP needs an OAuth token (user-bound) or the
// dashboard; a plain API key gets an actionable error. This is fail-closed: if
// the identity model changes, the tool errors instead of leaking.
func (s *Server) feedbackScopeFor(ctx context.Context) (dbstore.FeedbackScope, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return dbstore.FeedbackScope{}, errors.New("token missing or invalid")
	}
	if id.UserID == "" {
		return dbstore.FeedbackScope{}, errors.New(
			"feedback is users' personal data — an API key cannot access it. " +
				"Use an OAuth token (bound to a user) or the /feedback dashboard.")
	}
	if s.isSuperAdmin(ctx) {
		return dbstore.FeedbackAdminScope(), nil
	}
	return dbstore.FeedbackUserScope(id.UserID), nil
}

// feedbackStore returns the feedback store or an actionable error.
func (s *Server) feedbackStore() (*dbstore.FeedbackThreadStore, error) {
	if s.deps == nil || s.deps.Feedback == nil {
		return nil, errors.New("feedback system not configured")
	}
	return s.deps.Feedback, nil
}

// ── list_feedback ───────────────────────────────────────────────────────────

type listFeedbackIn struct {
	Status string `json:"status,omitempty" jsonschema:"Optional — open|in_progress|resolved|closed"`
	Page   int    `json:"page,omitempty" jsonschema:"Optional — page (starting at 1)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional — per-page limit (1-100, default 20)"`
}

type feedbackThreadOut struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	LastActivityAt string `json:"last_activity_at"`
	MessageCount   int64  `json:"message_count"`
	// UserID / UserEmail are populated for a super-admin triage read only.
	// For a normal caller the list is their own threads and echoing an identity
	// back adds nothing they do not already know.
	UserID    string `json:"user_id,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
}

// listFeedback returns one page of feedback threads, newest activity first.
//
// Scope: the caller's own threads, or every thread for a super-admin. The
// caller's scope is decided SERVER-SIDE from the authenticated identity — there
// is no argument that widens it.
func (s *Server) listFeedback(ctx context.Context, _ *mcp.CallToolRequest, in listFeedbackIn) (*mcp.CallToolResult, *listPageOutput[feedbackThreadOut], error) {
	// AUTHORIZE BEFORE CHECKING THE STORE. The scope guard is the authorization
	// decision, including the refusal of a caller with no user identity (an API
	// key — see feedbackScopeFor); a store-availability error returned first
	// would mask it with a configuration message.
	sc, err := s.feedbackScopeFor(ctx)
	if err != nil {
		return nil, nil, err
	}
	store, err := s.feedbackStore()
	if err != nil {
		return nil, nil, err
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status != "" && !dbstore.ValidFeedbackStatus(status) {
		return nil, nil, fmt.Errorf("invalid status: %q (open|in_progress|resolved|closed)", in.Status)
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	threads, total, err := store.ListFilteredPage(ctx, sc, status, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]feedbackThreadOut, 0, len(threads))
	for _, t := range threads {
		o := feedbackThreadOut{
			ID: t.ID, Subject: t.Subject, Status: t.Status,
			CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
			LastActivityAt: t.LastActivityAt.UTC().Format(time.RFC3339),
			MessageCount:   t.MessageCount,
		}
		// The filer identity is admin-only information (triage needs to know
		// who filed it; a user reading their own list does not).
		if sc.All {
			o.UserID = t.UserID
			o.UserEmail = t.UserEmail
		}
		out = append(out, o)
	}
	return nil, newListPageOut(out, page, limit, total), nil
}

// ── get_feedback_thread ─────────────────────────────────────────────────────

type getFeedbackThreadIn struct {
	ThreadID string `json:"thread_id" jsonschema:"Feedback thread ID (from list_feedback)"`
}

type feedbackMessageOut struct {
	ID          string `json:"id"`
	AuthorKind  string `json:"author_kind" jsonschema:"user or admin"`
	AuthorEmail string `json:"author_email,omitempty"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
}

// getFeedbackThread returns one thread and its messages, scoped. A thread
// outside the caller's scope answers "tidak dijumpai" — the same error as an
// unknown id, so the tool cannot be used to test whether another person's
// thread exists.
func (s *Server) getFeedbackThread(ctx context.Context, _ *mcp.CallToolRequest, in getFeedbackThreadIn) (*mcp.CallToolResult, map[string]any, error) {
	// Authorize first (see listFeedback).
	sc, err := s.feedbackScopeFor(ctx)
	if err != nil {
		return nil, nil, err
	}
	store, err := s.feedbackStore()
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.ThreadID) == "" {
		return nil, nil, errors.New("thread_id is required")
	}
	th, err := store.Thread(ctx, sc, in.ThreadID)
	if err != nil {
		if errors.Is(err, dbstore.ErrFeedbackNotFound) {
			return nil, nil, errors.New("feedback thread not found")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	msgs, err := store.Messages(ctx, sc, in.ThreadID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]feedbackMessageOut, 0, len(msgs))
	for _, m := range msgs {
		authorEmail := ""
		if m.AuthorKind == dbstore.FeedbackMessageAuthorAdmin {
			authorEmail = m.AuthorEmail
		}
		out = append(out, feedbackMessageOut{
			ID: m.ID, AuthorKind: m.AuthorKind, AuthorEmail: authorEmail,
			Body: m.Body, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	thread := map[string]any{
		"id": th.ID, "subject": th.Subject, "status": th.Status,
		"created_at":       th.CreatedAt.UTC().Format(time.RFC3339),
		"last_activity_at": th.LastActivityAt.UTC().Format(time.RFC3339),
		"message_count":    th.MessageCount,
	}
	// The notification-failure marker travels with the thread on BOTH scopes:
	// a super-admin triaging needs it, and a filer whose answer never arrived
	// is the other person the defect hurt. Omitted entirely when there is no
	// recorded failure, so a healthy thread's payload is unchanged.
	if th.NotifyFailedAt != nil {
		thread["notify_failed_at"] = th.NotifyFailedAt.UTC().Format(time.RFC3339)
		thread["notify_failed_kind"] = th.NotifyFailedKind
		if sc.All {
			thread["notify_failed_error"] = th.NotifyFailedError
		}
	}
	if sc.All {
		thread["user_id"] = th.UserID
		thread["user_email"] = th.UserEmail
	}
	return nil, map[string]any{"thread": thread, "items": out}, nil
}

// ── reply_feedback ──────────────────────────────────────────────────────────

type replyFeedbackIn struct {
	ThreadID string `json:"thread_id" jsonschema:"Feedback thread ID"`
	Body     string `json:"body" jsonschema:"Message body (minimum 10 characters)"`
}

// replyFeedback appends a message to a thread. The author kind is derived from
// the CALLER, not from the request: a super-admin writing through the triage
// scope is an admin reply (which also advances an open thread to in_progress),
// and everyone else is the filer. A client cannot claim to be an admin.
func (s *Server) replyFeedback(ctx context.Context, _ *mcp.CallToolRequest, in replyFeedbackIn) (*mcp.CallToolResult, map[string]any, error) {
	// Authorize first (see listFeedback).
	sc, err := s.feedbackScopeFor(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Issue #645: a reply is a WRITE. The feedback tools are user-scoped so no
	// workspace role applies; the credential's write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	store, err := s.feedbackStore()
	if err != nil {
		return nil, nil, err
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < 10 {
		return nil, nil, errors.New("please write at least 10 characters")
	}
	if len(body) > 5000 {
		return nil, nil, errors.New("message too long (maximum 5000 characters)")
	}

	authorKind := dbstore.FeedbackMessageAuthorUser
	authorID := sc.UserID
	if sc.All {
		// A super-admin replying is the admin side of the conversation, and the
		// reply is attributed to them so the thread records who answered.
		// The user id comes from the IDENTITY, not userIDFor: feedbackScopeFor
		// already refused a caller with no user identity, so an account-owner
		// fallback here would only ever mask a bug.
		authorKind = dbstore.FeedbackMessageAuthorAdmin
		if id := IdentityFromContext(ctx); id != nil {
			authorID = id.UserID
		}
		if authorID == "" {
			return nil, nil, errors.New("token missing or invalid")
		}
	}
	m, err := store.AddMessage(ctx, sc, in.ThreadID, authorKind, authorID, body)
	if err != nil {
		if errors.Is(err, dbstore.ErrFeedbackNotFound) {
			return nil, nil, errors.New("feedback thread not found")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if sc.All {
		s.auditSuperAdminAction(ctx, "admin.feedback.reply", map[string]any{"thread_id": in.ThreadID})
	}
	return nil, map[string]any{
		"status":     "sent",
		"message_id": m.ID,
		"author":     authorKind,
		"created_at": m.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// ── retry_feedback_notify ───────────────────────────────────────────────────

type retryFeedbackNotifyIn struct {
	ThreadID string `json:"thread_id" jsonschema:"Feedback thread ID (from list_feedback)"`
}

type retryFeedbackNotifyOut struct {
	ThreadID string `json:"thread_id"`
	// Notification is the outcome of the retry:
	// queued|sent|failed|none|skipped.
	// "none" means the thread had no recorded failure, so nothing was re-sent.
	// "skipped" means the attempt deliberately sent nothing — no super-admin
	// recipient is configured — so no job was enqueued. It is NOT a delivery and
	// not a failure: reporting "queued" here would tell the operator mail went
	// out when nothing left the process. The same values are produced by the
	// dashboard's retry button (api/internal/httpapi/rest_feedback.go).
	Notification string `json:"notification" jsonschema:"queued|sent|failed|none|skipped"`
}

// retryFeedbackNotify re-sends the notification that last failed for a thread —
// the release path for the failure marker, over MCP for parity with the
// dashboard's retry button.
//
// SUPER-ADMIN ONLY, and that is not an oversight: the marker exists because the
// platform team needs to notice it, and re-sending mail to a filer is an action
// on the platform's side of the conversation. A filer re-triggering their own
// notification would be a mail-amplification loop on top of mail that is already
// broken.
func (s *Server) retryFeedbackNotify(ctx context.Context, _ *mcp.CallToolRequest, in retryFeedbackNotifyIn) (*mcp.CallToolResult, retryFeedbackNotifyOut, error) {
	if err := s.requireSuperAdmin(ctx); err != nil {
		return nil, retryFeedbackNotifyOut{}, err
	}
	// The user-identity gate applies to the admin path too (see
	// setFeedbackStatus): isSuperAdmin judges an API-key caller by its ACCOUNT
	// OWNER's email, so without this a key on a super-admin's account would
	// reach the triage queue.
	if _, err := s.feedbackScopeFor(ctx); err != nil {
		return nil, retryFeedbackNotifyOut{}, err
	}
	// Issue #645: re-sending is a WRITE — the credential's write scope.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, retryFeedbackNotifyOut{}, err
	}
	if s.deps == nil || s.deps.RetryFeedbackNotify == nil {
		return nil, retryFeedbackNotifyOut{}, errors.New("feedback system not configured")
	}
	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		return nil, retryFeedbackNotifyOut{}, errors.New("thread_id is required")
	}
	outcome, err := s.deps.RetryFeedbackNotify(ctx, threadID)
	if err != nil {
		if errors.Is(err, dbstore.ErrFeedbackNotFound) {
			return nil, retryFeedbackNotifyOut{}, errors.New("feedback thread not found")
		}
		return nil, retryFeedbackNotifyOut{}, fmt.Errorf("db: %w", err)
	}
	s.auditSuperAdminAction(ctx, "admin.feedback.retry_notify",
		map[string]any{"thread_id": threadID, "notification": outcome})
	return nil, retryFeedbackNotifyOut{ThreadID: threadID, Notification: outcome}, nil
}

// ── set_feedback_status ─────────────────────────────────────────────────────

type setFeedbackStatusIn struct {
	ThreadID string `json:"thread_id" jsonschema:"Feedback thread ID"`
	Status   string `json:"status" jsonschema:"open|in_progress|resolved|closed"`
}

// setFeedbackStatus moves a thread's status. Super-admin only: status is the
// triage decision, and letting a filer mark their own thread "resolved" would
// let them hide an unanswered report from the queue.
func (s *Server) setFeedbackStatus(ctx context.Context, _ *mcp.CallToolRequest, in setFeedbackStatusIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.requireSuperAdmin(ctx); err != nil {
		return nil, nil, err
	}
	// The user-identity gate applies to the admin path too, and it must run
	// before the store check: `isSuperAdmin` judges an API-key caller by its
	// ACCOUNT OWNER's email, so without this a key on a super-admin's account
	// would open the whole triage queue. feedbackScopeFor is what encodes that
	// (it builds the admin scope only for a caller WITH a user identity).
	if _, err := s.feedbackScopeFor(ctx); err != nil {
		return nil, nil, err
	}
	// Issue #645: a status change is a WRITE — the credential's write scope.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	store, err := s.feedbackStore()
	if err != nil {
		return nil, nil, err
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if !dbstore.ValidFeedbackStatus(status) {
		return nil, nil, fmt.Errorf("invalid status: %q (open|in_progress|resolved|closed)", in.Status)
	}
	if strings.TrimSpace(in.ThreadID) == "" {
		return nil, nil, errors.New("thread_id is required")
	}
	if err := store.SetStatus(ctx, dbstore.FeedbackAdminScope(), in.ThreadID, status); err != nil {
		if errors.Is(err, dbstore.ErrFeedbackNotFound) {
			return nil, nil, errors.New("feedback thread not found")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	s.auditSuperAdminAction(ctx, "admin.feedback.set_status",
		map[string]any{"thread_id": in.ThreadID, "status": status})
	return nil, map[string]any{"status": "updated", "feedback_status": status}, nil
}
