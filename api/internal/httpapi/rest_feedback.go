// In-app feedback REST surface: the user's own threads, and super-admin triage.
//
// TWO SURFACES, ONE STORE, DIFFERENT SCOPES.
//
//	/v1/rest/feedback*    every signed-in user. Scoped to the caller's own
//	                      threads via dbstore.FeedbackUserScope(uid) — the
//	                      user id is always the SESSION's, never a parameter,
//	                      so there is no request a caller can shape to read
//	                      another user's thread.
//	/v1/admin/feedback*   super-admin only (requireSuperAdmin + audit, the same
//	                      gate the suppressions and dead-letter sections use),
//	                      scoped with dbstore.FeedbackAdminScope().
//
// The feedback form itself is owned by the SYSTEM, not by any account: a thread
// carries no account_id (migration 000063), so nothing here can surface inside a
// customer's forms, /sales view, CSV export or spam queue. That is why these
// handlers never resolve a workspace/account scope, which every other
// /v1/rest/* handler does — a feedback thread is not tenant data.
//
// NOTIFYING: the email goes through the #199 background queue (the existing
// notification lane, including its pre-send suppression choke point) — no new
// channel. The AI spam judge is deliberately NOT called on this path: every
// filer is authenticated, so the threat is one account flooding, and that is
// answered with a per-user daily limit instead of a classifier that can
// misclassify a genuine complaint.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
)

// Feedback job type enqueued on the #199 queue. It rides the SAME sender as the
// per-form notifications (pay.Receipts.Sender), so it passes the pre-send
// suppression guard; see email_dispatcher.go's case for it.
const feedbackNotifyJobType = "feedback_notify"

// The outcomes a feedback notification attempt can have, plus "none" for a retry
// of a thread that is not marked as failed, and "skipped" for one that was
// deliberately not attempted at all.
//
// THESE ARE A CONTRACT, not log strings: the admin reply's HTTP response
// publishes one of them as "notification", so the console can say what actually
// happened instead of assuming success. `queued` is the normal answer (the #199
// worker sends it), `sent` is the bare-mode synchronous path, and `failed` means
// neither happened — which is exactly the case the old code answered "sent" for.
//
//	queued   handed to the #199 queue
//	sent     delivered synchronously by the mailer
//	failed   no channel delivered it; the error is logged and marked on the thread
//	none     nothing to retry (no failure recorded for that thread)
//	skipped  nothing was ATTEMPTED — no job left the process (see below)
//
// `skipped` exists because "no job was enqueued" and "a job was enqueued" must
// not be the same answer. The admin lane has a deliberate no-op exit: with an
// empty super-admin allowlist there is nobody to alert, which is a configuration
// choice and NOT a delivery failure (TestFeedbackNotifyNoRecipientsIsNotAFailure
// pins that — so it writes no marker). That is fine for the filing path, whose
// caller has nowhere to put an outcome, but it made the RETRY path lie: a marker
// was there, the operator pressed re-send, and the absent marker afterwards was
// read as "queued" while nothing was enqueued and no mail would ever arrive.
// `queued` must imply a job left the process, so a skip reports itself.
const (
	feedbackNotifyQueued  = "queued"
	feedbackNotifySent    = "sent"
	feedbackNotifyFailed  = "failed"
	feedbackNotifyNone    = "none"
	feedbackNotifySkipped = "skipped"
)

// The length and rate bounds live in feedbacksvc, which is the ONE filing path
// (REST, MCP tool, in-app chat). These aliases keep this file's existing
// validation readable without introducing a second copy of the numbers — a
// second copy is how a guard silently stops applying on one surface.
const (
	feedbackBodyMin    = feedbacksvc.BodyMin
	feedbackBodyMax    = feedbacksvc.BodyMax
	feedbackSubjectMax = feedbacksvc.SubjectMax
	feedbackDailyLimit = feedbacksvc.DefaultDailyLimit
)

// feedbackThreadOut is the user-facing thread shape. It carries no filer
// identity: the caller IS the filer, and echoing an id back adds nothing. It
// DOES carry the notification-failure marker, because the filer waiting for an
// answer is exactly who needs to know the answer never left.
type feedbackThreadOut struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastActivityAt string `json:"last_activity_at"`
	MessageCount   int64  `json:"message_count"`
	NotifyFailedAt string `json:"notify_failed_at,omitempty"`
	NotifyFailedBy string `json:"notify_failed_kind,omitempty"`
}

// adminFeedbackThreadOut is the triage shape: it names who filed the thread,
// which the admin list needs to answer a question. user_email is the snapshot
// taken at filing time (the admin-facing list is a record of what was said, so
// it shows the address as it was then).
//
// The notify-failure fields are what make the triage list the place a broken
// notification is SEEN: without them the console, the board and the thread all
// look healthy while the filer has been told nothing.
type adminFeedbackThreadOut struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	Status         string `json:"status"`
	UserID         string `json:"user_id"`
	UserEmail      string `json:"user_email"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastActivityAt string `json:"last_activity_at"`
	MessageCount   int64  `json:"message_count"`
	// NotifyFailedAt is the RFC3339 time of the last undelivered notification,
	// omitempty so a healthy thread's JSON is unchanged. kind says who was not
	// told (filer|admin) and detail is the error, for the operator card.
	NotifyFailedAt    string `json:"notify_failed_at,omitempty"`
	NotifyFailedKind  string `json:"notify_failed_kind,omitempty"`
	NotifyFailedError string `json:"notify_failed_error,omitempty"`
}

// notifyFailureFields renders the marker as RFC3339 (or "" when there is none).
// One helper so the two thread shapes cannot disagree about the format.
func notifyFailureAt(t *dbstore.FeedbackThread) string {
	if t.NotifyFailedAt == nil {
		return ""
	}
	return t.NotifyFailedAt.UTC().Format(time.RFC3339)
}

func toFeedbackThreadOut(t *dbstore.FeedbackThread) feedbackThreadOut {
	return feedbackThreadOut{
		ID: t.ID, Subject: t.Subject, Status: t.Status,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
		LastActivityAt: t.LastActivityAt.UTC().Format(time.RFC3339),
		MessageCount:   t.MessageCount,
		NotifyFailedAt: notifyFailureAt(t), NotifyFailedBy: t.NotifyFailedKind,
	}
}

func toAdminFeedbackThreadOut(t *dbstore.FeedbackThread) adminFeedbackThreadOut {
	return adminFeedbackThreadOut{
		ID: t.ID, Subject: t.Subject, Status: t.Status,
		UserID: t.UserID, UserEmail: t.UserEmail,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
		LastActivityAt: t.LastActivityAt.UTC().Format(time.RFC3339),
		MessageCount:   t.MessageCount,
		NotifyFailedAt: notifyFailureAt(t), NotifyFailedKind: t.NotifyFailedKind,
		NotifyFailedError: t.NotifyFailedError,
	}
}

// feedbackMessageOut is one message. author_kind is user|admin;
// author_email is resolved live for admin replies (empty for the filer's own,
// whose address the caller already knows).
type feedbackMessageOut struct {
	ID          string `json:"id"`
	AuthorKind  string `json:"author_kind"`
	AuthorEmail string `json:"author_email,omitempty"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
}

func toFeedbackMessageOut(m *dbstore.FeedbackMessage) feedbackMessageOut {
	authorEmail := ""
	if m.AuthorKind == dbstore.FeedbackMessageAuthorAdmin {
		authorEmail = m.AuthorEmail
	}
	return feedbackMessageOut{
		ID: m.ID, AuthorKind: m.AuthorKind, AuthorEmail: authorEmail,
		Body: m.Body, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ── user surface ────────────────────────────────────────────────────────────

type createFeedbackIn struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// handleCreateFeedback: POST /v1/rest/feedback — file a new thread.
//
// The filer is the SESSION user. There is no user_id in the request and no way
// to file on someone else's behalf.
//
// The filing itself goes through feedbacksvc — the SAME service the MCP
// submit_feedback tool and the in-app chat use. A second copy of the validation,
// the daily limit and the notification here is how one of them silently stops
// applying, so this handler owns only the HTTP mapping.
func (h *RESTHandlers) handleCreateFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var in createFeedbackIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	th, err := h.feedbackService().CreateThread(r.Context(), uid, in.Subject, in.Body)
	if err != nil {
		writeErr(w, feedbacksvc.HTTPStatusHint(err), err.Error())
		return
	}
	h.recordFeedbackEvent(r, "feedback.created", th.ID, map[string]any{"status": th.Status})
	writeJSON(w, http.StatusCreated, map[string]any{"status": "created", "id": th.ID})
}

// feedbackService builds the shared filing service from the wired stores. The
// notification is passed as the service's Notify hook, so a filing is notified
// exactly once wherever it came from.
func (h *RESTHandlers) feedbackService() *feedbacksvc.Service {
	return &feedbacksvc.Service{
		Store: h.Feedback,
		Users: h.Users,
		Notify: func(th *dbstore.FeedbackThread, body string) {
			_ = h.notifyFeedbackAdmins(th, body)
		},
	}
}

// handleListFeedback: GET /v1/rest/feedback?page=&limit= — the caller's own
// threads, newest activity first.
func (h *RESTHandlers) handleListFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	page, limit := parsePageLimit(r)
	// The status filter is accepted here too, so the user's own list can show
	// "resolved" separately. It is scoped by the same user predicate either way.
	statusOf := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if statusOf != "" && !dbstore.ValidFeedbackStatus(statusOf) {
		writeErr(w, http.StatusBadRequest, "invalid status (open|in_progress|resolved|closed)")
		return
	}
	threads, total, err := h.Feedback.ListFilteredPage(r.Context(),
		dbstore.FeedbackUserScope(uid), statusOf, page, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read the feedback")
		return
	}
	out := make([]feedbackThreadOut, 0, len(threads))
	for _, t := range threads {
		out = append(out, toFeedbackThreadOut(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// handleGetFeedback: GET /v1/rest/feedback/{id} — one thread + its messages,
// scoped to the caller.
func (h *RESTHandlers) handleGetFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	sc := dbstore.FeedbackUserScope(uid)
	id := r.PathValue("id")
	th, msgs, ok := h.readFeedbackThread(w, r, sc, id)
	if !ok {
		return
	}
	out := make([]feedbackMessageOut, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toFeedbackMessageOut(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"thread": toFeedbackThreadOut(th), "messages": out,
	})
}

type replyFeedbackIn struct {
	Body string `json:"body"`
}

// handleReplyFeedback: POST /v1/rest/feedback/{id}/messages — the filer adds a
// message to their own thread.
func (h *RESTHandlers) handleReplyFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	body, ok := h.readFeedbackBody(w, r)
	if !ok {
		return
	}
	// The scope is the SESSION user's, so a reply to another user's thread id
	// resolves to ErrFeedbackNotFound inside AddMessage and answers 404 — the
	// same response as a thread that does not exist.
	m, err := h.Feedback.AddMessage(r.Context(), dbstore.FeedbackUserScope(uid),
		r.PathValue("id"), dbstore.FeedbackMessageAuthorUser, uid, body)
	if err != nil {
		h.writeFeedbackError(w, err)
		return
	}
	h.recordFeedbackEvent(r, "feedback.replied", r.PathValue("id"), map[string]any{"actor": "user"})
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "sent", "message": toFeedbackMessageOut(m),
	})
}

// ── super-admin surface ─────────────────────────────────────────────────────

// handleAdminListFeedback: GET /v1/admin/feedback?status=&page=&limit= — every
// thread, for triage. Super-admin only, audited.
func (h *RESTHandlers) handleAdminListFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.list")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	page, limit := parsePageLimit(r)
	statusOf := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if statusOf != "" && !dbstore.ValidFeedbackStatus(statusOf) {
		writeErr(w, http.StatusBadRequest, "invalid status (open|in_progress|resolved|closed)")
		return
	}
	threads, total, err := h.Feedback.ListFilteredPage(r.Context(),
		dbstore.FeedbackAdminScope(), statusOf, page, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read the feedback")
		return
	}
	counts, err := h.Feedback.CountByStatus(r.Context())
	if err != nil {
		// The counts are a header convenience, not the list itself: a failure
		// here must not hide the threads the operator asked for.
		counts = map[string]int64{}
	}
	out := make([]adminFeedbackThreadOut, 0, len(threads))
	for _, t := range threads {
		out = append(out, toAdminFeedbackThreadOut(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total, "counts": counts,
	})
}

// handleAdminGetFeedback: GET /v1/admin/feedback/{id} — one thread + messages.
// Super-admin only, audited.
func (h *RESTHandlers) handleAdminGetFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.get")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	th, msgs, ok := h.readFeedbackThread(w, r, dbstore.FeedbackAdminScope(), r.PathValue("id"))
	if !ok {
		return
	}
	out := make([]feedbackMessageOut, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toFeedbackMessageOut(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"thread": toAdminFeedbackThreadOut(th), "messages": out,
	})
}

// handleAdminReplyFeedback: POST /v1/admin/feedback/{id}/messages — the admin
// replies. Super-admin only, audited.
//
// The reply notifies the filer through the existing email lane. Reply to a
// resolved/closed thread is allowed: the operator decided to answer, and
// SetStatus is how they change the status, not the reply.
func (h *RESTHandlers) handleAdminReplyFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.reply")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	body, ok := h.readFeedbackBody(w, r)
	if !ok {
		return
	}
	adminID := h.sessionOwnerID(r)
	id := r.PathValue("id")
	// Resolve the thread through the admin scope first, so the notification can
	// name the filer and an unknown id answers 404 before anything is written.
	th, err := h.Feedback.Thread(r.Context(), dbstore.FeedbackAdminScope(), id)
	if err != nil {
		h.writeFeedbackError(w, err)
		return
	}
	m, err := h.Feedback.AddMessage(r.Context(), dbstore.FeedbackAdminScope(),
		id, dbstore.FeedbackMessageAuthorAdmin, adminID, body)
	if err != nil {
		h.writeFeedbackError(w, err)
		return
	}
	// The reply is stored; the notification is best-effort. The response says
	// WHICH of the two happened instead of claiming "sent" regardless — an
	// admin who is told a filer was notified when the queue rejected the job
	// closes the thread believing the complaint was answered (the defect this
	// closes). A failed attempt is already logged and marked on the thread.
	notification := h.notifyFeedbackFiler(dbstore.FeedbackAdminScope(), th, body)
	h.recordFeedbackEvent(r, "feedback.admin_replied", id, map[string]any{
		"actor": "admin", "notification": notification,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "sent", "notification": notification, "message": toFeedbackMessageOut(m),
	})
}

// handleAdminRetryFeedbackNotify: POST /v1/admin/feedback/{id}/notify — re-send
// the notification that last failed for a thread. Super-admin only, audited.
//
// This is the release path for the failure marker: a visible "the filer was not
// told" is only half the fix, the other half is the operator being able to act
// on it (the #547/#579 dead-letter pattern). It re-attempts through the SAME
// delivery path as the original send, so a retry cannot invent a second channel.
func (h *RESTHandlers) handleAdminRetryFeedbackNotify(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.retry_notify")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	id := r.PathValue("id")
	outcome, err := h.RetryFeedbackNotify(r.Context(), id)
	if err != nil {
		h.writeFeedbackError(w, err)
		return
	}
	h.recordFeedbackEvent(r, "feedback.notify_retried", id, map[string]any{
		"actor": "admin", "notification": outcome,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "retried", "notification": outcome})
}

type setFeedbackStatusIn struct {
	Status string `json:"status"`
}

// handleAdminSetFeedbackStatus: POST /v1/admin/feedback/{id}/status — move a
// thread's status. Super-admin only, audited.
//
// An unknown status is 400 before the store: the CHECK constraint is the
// backstop, not the validation, so a bad value reads as caller error rather than
// a 500.
func (h *RESTHandlers) handleAdminSetFeedbackStatus(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if !h.requireSuperAdmin(r) {
		writeErr(w, http.StatusForbidden, "super-admin is required for this action")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.set_status")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "the feedback system is not configured")
		return
	}
	var in setFeedbackStatusIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if !dbstore.ValidFeedbackStatus(status) {
		writeErr(w, http.StatusBadRequest, "invalid status (open|in_progress|resolved|closed)")
		return
	}
	id := r.PathValue("id")
	if err := h.Feedback.SetStatus(r.Context(), dbstore.FeedbackAdminScope(), id, status); err != nil {
		h.writeFeedbackError(w, err)
		return
	}
	h.recordFeedbackEvent(r, "feedback.status_changed", id, map[string]any{"status": status})
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "feedback_status": status})
}

// ── shared helpers ──────────────────────────────────────────────────────────

// readFeedbackThread loads a thread + its messages under one scope and writes
// the error response itself. It returns ok=false when it has already answered,
// so every caller reads as "load, then render" with one error shape.
func (h *RESTHandlers) readFeedbackThread(w http.ResponseWriter, r *http.Request, sc dbstore.FeedbackScope, id string) (*dbstore.FeedbackThread, []*dbstore.FeedbackMessage, bool) {
	th, err := h.Feedback.Thread(r.Context(), sc, id)
	if err != nil {
		h.writeFeedbackError(w, err)
		return nil, nil, false
	}
	msgs, err := h.Feedback.Messages(r.Context(), sc, id)
	if err != nil {
		h.writeFeedbackError(w, err)
		return nil, nil, false
	}
	return th, msgs, true
}

// writeFeedbackError maps the feedback store's errors onto HTTP statuses. A
// thread outside the caller's scope maps to 404, never 403: distinguishing the
// two would tell a caller that another user's thread id exists.
func (h *RESTHandlers) writeFeedbackError(w http.ResponseWriter, err error) {
	if errors.Is(err, dbstore.ErrFeedbackNotFound) {
		writeErr(w, http.StatusNotFound, "feedback thread not found")
		return
	}
	writeErr(w, http.StatusInternalServerError, "failed to process the feedback")
}

// readFeedbackBody decodes + validates a message body for either surface.
func (h *RESTHandlers) readFeedbackBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in replyFeedbackIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < feedbackBodyMin {
		writeErr(w, http.StatusBadRequest, "please write at least 10 characters")
		return "", false
	}
	if len(body) > feedbackBodyMax {
		writeErr(w, http.StatusBadRequest, "message too long (maximum 5000 characters)")
		return "", false
	}
	return body, true
}

// recordFeedbackEvent appends an audit row for a feedback action. Best-effort:
// a nil Events store or a write failure never fails the action the user asked
// for. Attributed to the acting user's own account when one exists, because the
// events table requires an account id — and a feedback thread deliberately has
// none of its own.
func (h *RESTHandlers) recordFeedbackEvent(r *http.Request, event, threadID string, props map[string]any) {
	if h.Events == nil {
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return
	}
	accountID := ""
	if h.Accounts != nil {
		if accs, err := h.Accounts.ByOwner(r.Context(), uid); err == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
	if accountID == "" {
		return
	}
	if props == nil {
		props = map[string]any{}
	}
	props["thread_id"] = threadID
	_ = h.Events.Record(r.Context(), accountID, uid, event, props, "", "")
}

// notifyFeedbackAdmins tells the platform team a new thread arrived, through the
// existing #199 email lane. Recipients are the configured super-admin
// allowlist — the same list that gates the triage surface, so there is exactly
// one place to change who is notified.
//
// The message is written in ENGLISH, deliberately: the recipients are identified
// by SUPER_ADMIN_EMAILS, an allowlist of operator mailboxes, not `users` rows —
// there is no lang to read, and inventing one (the filer's, say) would send an
// operator an email in a language they never chose. The filer's language governs
// the filer's email below.
//
// NotifyFeedbackAdmins is the exported form: the MCP submit_feedback tool and
// the in-app chat file through the same feedbacksvc, whose Notify hook is this
// function, so a thread filed from any surface notifies identically.
//
// Language consistency: a notification to the PLATFORM TEAM about a thread is
// written in the FILER's language — the filer's own words are being quoted back
// (the status, the subject, the excerpt), so re-labelling them in another
// language would produce a mixed-language email. The filer's language is
// resolved from the thread's user (users.lang, the PROFILE axis).
func (h *RESTHandlers) NotifyFeedbackAdmins(th *dbstore.FeedbackThread, body string) {
	// The filing path has nowhere to publish the outcome — the thread is already
	// stored and the response is its own — and the failure is recorded on the
	// thread by the delivery path itself. The RETRY path is the one that must not
	// discard it (see RetryFeedbackNotify).
	_ = h.notifyFeedbackAdmins(th, body)
}

func (h *RESTHandlers) notifyFeedbackAdmins(th *dbstore.FeedbackThread, body string) string {
	if th == nil || len(h.SuperAdminEmails) == 0 {
		// Nobody is configured to be told. That is a deliberate configuration
		// (no super-admin allowlist ⇒ no platform team to alert), not a
		// delivery failure, so it records no marker — the log line below is
		// the trace.
		//
		// It RETURNS that, though: "nothing was attempted" is not "queued", and
		// the RETRY path publishes this value to the operator. An absent marker
		// used to be read as success there, so a re-send with an empty allowlist
		// told the operator the platform team had been notified when no job was
		// ever enqueued.
		slog.Info("feedback_notify_skipped", "thread", threadIDOf(th), "kind", dbstore.FeedbackNotifyKindAdmin,
			"reason", "no super-admin recipients configured")
		return feedbackNotifySkipped
	}
	lang := h.feedbackLang(th.UserID)
	text := emailtext.Text(lang, emailtext.FeedbackNew, map[string]string{
		"from":   th.UserEmail,
		"status": th.Status,
		// The filing itself, as a bounded preview: this alert's job is to get the
		// platform team INTO the thread (the link is in the body), so the long
		// text does not have to travel — unlike the filer's reply mail below,
		// which is the answer itself and must arrive whole.
		"subject": feedbackSubjectOrExcerpt(th, lang),
		"excerpt": feedbackExcerpt(body),
		// The operator's link opens THE THREAD, not the queue (see
		// feedbackAdminLink).
		"link": feedbackAdminLink(h.BaseURL, th.ID),
	})
	// One outcome for the whole fan-out, and a non-delivery wins: the alert
	// reached the team only if every configured recipient got it.
	outcome := feedbackNotifySkipped
	for _, to := range h.SuperAdminEmails {
		got := h.deliverFeedbackMail(dbstore.FeedbackAdminScope(), threadIDOf(th),
			dbstore.FeedbackNotifyKindAdmin, to, text.Subject, text.Body, text.Lang)
		if outcome == feedbackNotifySkipped {
			outcome = got
			continue
		}
		if isDeliveredOutcome(outcome) {
			outcome = got
		}
	}
	return outcome
}

// notifyFeedbackFiler tells the filer an admin replied, and returns the outcome
// of the attempt. The address is read live from `users` by the user id on the
// thread, so a filer who changed their address is notified at the new one.
//
// Language consistency: the filer is a platform user, so this follows the
// PROFILE axis (his own users.lang) — same resolution as the new-thread
// notification above, read in the same lookup.
//
// The outcome is returned rather than discarded because the ADMIN REPLY's HTTP
// response publishes it: answering "sent" when the queue rejected the job is how
// the admin came to believe a filer had been told when nobody had (the defect
// this file's notification path exists to prevent). sc is the scope the failure
// marker is written under — the admin scope at every current call site, because
// the marker is platform state about a notification, not thread content.
func (h *RESTHandlers) notifyFeedbackFiler(sc dbstore.FeedbackScope, th *dbstore.FeedbackThread, body string) string {
	if th == nil {
		return feedbackNotifyFailed
	}
	if h.Users == nil {
		return h.markFeedbackNotifyFailed(sc, threadIDOf(th), dbstore.FeedbackNotifyKindFiler, "",
			errors.New("user store not configured"))
	}
	u, err := h.Users.ByID(context.Background(), th.UserID)
	if err != nil || u == nil || u.Email == "" {
		if err == nil {
			err = errors.New("filer has no address on record")
		}
		// The id is not the address, so it is safe to name in the log.
		return h.markFeedbackNotifyFailed(sc, threadIDOf(th), dbstore.FeedbackNotifyKindFiler, "", err)
	}
	lang := u.Lang
	text := emailtext.Text(lang, emailtext.FeedbackReply, map[string]string{
		"subject": feedbackSubjectOrExcerpt(th, lang),
		// The reply is carried WHOLE (feedbackQuotedText), not as an excerpt:
		// this mail IS the answer the filer was waiting for, and #753 removed
		// the 400-rune cut that made a long reply read as though the message
		// simply ended. Only the ADMIN alert above takes the bounded excerpt.
		"excerpt": feedbackQuotedText(body),
		// The filer's link opens the THREAD, not the feedback landing page:
		// a notification that says "we replied" and then drops the reader on
		// a list has not delivered the reply (see feedbackThreadLink).
		"link": feedbackThreadLink(h.BaseURL, th.ID),
	})
	return h.deliverFeedbackMail(sc, threadIDOf(th), dbstore.FeedbackNotifyKindFiler, u.Email,
		text.Subject, text.Body, text.Lang)
}

// feedbackLang resolves a feedback filer's platform language from his user row
// (the PROFILE axis). A missing/unreadable row, or an unset language, resolves
// to English. Never fails the notification: a language lookup is not worth
// dropping an email over.
func (h *RESTHandlers) feedbackLang(userID string) string {
	if h.Users == nil || userID == "" {
		return emailtext.En
	}
	u, err := h.Users.ByID(context.Background(), userID)
	if err != nil || u == nil {
		return emailtext.En
	}
	return emailtext.NormalizeLang(u.Lang)
}

// deliverFeedbackMail is the ONE place a feedback notification leaves this
// process. It returns the outcome and is the only writer of the thread's
// notification-failure marker, so a caller cannot send without the failure
// being either recorded on the thread or cleared from it.
//
// A successful attempt CLEARS a previous marker FOR THE SAME KIND: a thread that
// has recovered must stop advertising a failure that is no longer true. The kind
// goes to the clear so a success on one lane can never erase a failure on the
// other — the platform team's missed filing must survive an unrelated reply
// notification going through. The clear is a no-op when there is nothing to
// clear, and when the marker belongs to the other kind.
func (h *RESTHandlers) deliverFeedbackMail(sc dbstore.FeedbackScope, threadID, kind, to, subject, body, lang string) string {
	outcome, err := h.sendFeedbackMail(to, subject, body, lang)
	if outcome == feedbackNotifyFailed {
		return h.markFeedbackFailed(sc, threadID, kind, to, err)
	}
	slog.Info("feedback_notify_"+outcome, "thread", threadID, "kind", kind,
		"job_type", feedbackNotifyJobType, "to", obfuscateEmail(to))
	h.clearFeedbackNotifyFailure(sc, threadID, kind)
	return outcome
}

// isDeliveredOutcome reports whether an outcome means the mail actually left the
// process. Used only to fold a per-recipient fan-out into one answer.
func isDeliveredOutcome(outcome string) bool {
	return outcome == feedbackNotifyQueued || outcome == feedbackNotifySent
}

// markFeedbackNotifyFailed records a failure that never reached sendFeedbackMail
// (no filer address, no user store). It shares the marker+log path with
// deliverFeedbackMail so every failure has the same trace, whatever stage it
// died at.
func (h *RESTHandlers) markFeedbackNotifyFailed(sc dbstore.FeedbackScope, threadID, kind, to string, cause error) string {
	return h.markFeedbackFailed(sc, threadID, kind, to, cause)
}

// markFeedbackFailed is the single failure path: a structured log line naming
// the job type and the OBFUSCATED recipient (never the full address), plus the
// marker on the thread so a support person sees it where they already look
// (the same visibility principle as the #547 dead-letter list).
func (h *RESTHandlers) markFeedbackFailed(sc dbstore.FeedbackScope, threadID, kind, to string, cause error) string {
	errText := "unknown error"
	if cause != nil {
		errText = cause.Error()
	}
	slog.Error("feedback_notify_failed",
		"thread", threadID, "kind", kind, "job_type", feedbackNotifyJobType,
		"to", obfuscateEmail(to), "err", errText)
	if h.Feedback == nil || threadID == "" {
		return feedbackNotifyFailed
	}
	if err := h.Feedback.RecordNotifyFailure(context.Background(), sc, threadID, kind, errText); err != nil {
		// The marker failing is itself worth a line: without it the thread
		// looks healthy again, which is the original defect.
		slog.Error("feedback_notify_failure_record", "thread", threadID, "kind", kind, "err", err)
	}
	return feedbackNotifyFailed
}

// clearFeedbackNotifyFailure drops a stale marker after a later attempt of the
// SAME KIND worked, so a success on one notification lane never erases a failure
// on the other (see ClearNotifyFailure for why the kind is load-bearing).
func (h *RESTHandlers) clearFeedbackNotifyFailure(sc dbstore.FeedbackScope, threadID, kind string) {
	if h.Feedback == nil || threadID == "" {
		return
	}
	if err := h.Feedback.ClearNotifyFailure(context.Background(), sc, threadID, kind); err != nil {
		slog.Warn("feedback_notify_failure_clear", "thread", threadID, "kind", kind, "err", err)
	}
}

// threadIDOf is a nil-safe thread id for a log line.
func threadIDOf(th *dbstore.FeedbackThread) string {
	if th == nil {
		return ""
	}
	return th.ID
}

// feedbackMailJob is the queued message for one feedback notification: the job
// shape sendFeedbackMail enqueues, built in one place so the reader's language
// is set wherever the job is built.
//
// Lang is the READER's language and it is the SAME value that rendered the
// subject and the body — one lookup, passed down. The send side uses it for the
// branded HTML shell, so a Malay message cannot go out under a shell that
// declares English.
func feedbackMailJob(to, subject, body, lang string) emailqueue.Job {
	return emailqueue.Job{
		Type: feedbackNotifyJobType, To: to, Subject: subject, Body: body,
		Lang: lang,
	}
}

// sendFeedbackMail delivers one feedback email through the existing queue, with
// the synchronous mailer as the fallback when no queue is wired (bare-mode
// tests). Both paths are the ones every other notification uses — this adds no
// second channel.
//
// It returns the outcome AND the error. The error was discarded before, which
// is what let an admin reply answer "sent" for a job the queue had rejected.
//
//	queued  handed to the #199 queue (the worker sends it)
//	sent    delivered synchronously by the mailer
//	failed  neither happened; the error says why
//
// lang is the READER's language, and it is the SAME value the caller used to
// render the subject and the body: one lookup, passed down, so the branded HTML
// shell cannot end up in a different locale from the message it wraps. It
// travels on the job through feedbackMailJob (the one job-builder, the shell is
// rendered at send time by the sender) and to the synchronous fallback.
func (h *RESTHandlers) sendFeedbackMail(to, subject, body, lang string) (string, error) {
	if strings.TrimSpace(to) == "" {
		return feedbackNotifyFailed, errors.New("no recipient address")
	}
	if h.EmailQueue != nil {
		if err := h.EmailQueue.Enqueue(context.Background(), feedbackMailJob(to, subject, body, lang)); err != nil {
			return feedbackNotifyFailed, fmt.Errorf("enqueue %s: %w", feedbackNotifyJobType, err)
		}
		return feedbackNotifyQueued, nil
	}
	// mailerUnavailable (not `h.Mailer != nil`) is the guard: a typed-nil
	// *email.Sender is non-nil as an interface, and dereferencing it panics the
	// API process (#672). Both intents are kept here — the guard that makes
	// "no mailer" reachable for either shape, AND the returned Send error this
	// fix exists to stop discarding.
	if !mailerUnavailable(h.Mailer) {
		if err := h.Mailer.Send(to, subject, body, lang); err != nil {
			return feedbackNotifyFailed, fmt.Errorf("send %s: %w", feedbackNotifyJobType, err)
		}
		return feedbackNotifySent, nil
	}
	// No channel at all is a misconfiguration, not a "best effort" shrug: the
	// caller asked for a notification and nothing can deliver it.
	return feedbackNotifyFailed, errors.New("no email channel configured (queue and mailer both nil)")
}

// RetryFeedbackNotify re-attempts the notification that last failed for a
// thread and reports the outcome. It is the release path for the marker: the
// same "visible state + a way to act on it" pair the dead-letter surfaces use
// (#547/#579). A thread with no marker answers feedbackNotifyNone.
//
// The message quoted is the most recent one from the side that was NOT told:
// a filer who missed an admin reply gets that reply, and the platform team that
// missed a new thread gets the filing. It falls back to the thread subject when
// no message of that side exists, so a retry is always a real attempt.
//
// Exported because the MCP retry tool calls THIS function rather than
// re-implementing the decision: one logic, two interfaces.
func (h *RESTHandlers) RetryFeedbackNotify(ctx context.Context, threadID string) (string, error) {
	if h.Feedback == nil {
		return feedbackNotifyFailed, errors.New("the feedback system is not configured")
	}
	sc := dbstore.FeedbackAdminScope()
	th, err := h.Feedback.Thread(ctx, sc, threadID)
	if err != nil {
		return feedbackNotifyFailed, err
	}
	if th.NotifyFailedAt == nil {
		return feedbackNotifyNone, nil
	}
	msgs, err := h.Feedback.Messages(ctx, sc, threadID)
	if err != nil {
		return feedbackNotifyFailed, fmt.Errorf("db: %w", err)
	}
	// Which side authored the message the retry must quote. NOTE THE TWO
	// VOCABULARIES: a notify KIND names WHO WAS NOT TOLD ("filer"/"admin"),
	// an author kind names WHO WROTE A MESSAGE ("user"/"admin"). They are not
	// interchangeable — filtering messages on the notify kind matches nothing
	// (no message is ever authored by "filer"), so the quote stayed empty and
	// the subject fallback fired on EVERY filer retry: the filer was re-mailed
	// their own thread title and never the answer they had missed.
	wantAuthor := dbstore.FeedbackMessageAuthorAdmin
	if th.NotifyFailedKind == dbstore.FeedbackNotifyKindAdmin {
		// The platform team was not told about the filing, so the filing is
		// what the retry quotes.
		wantAuthor = dbstore.FeedbackMessageAuthorUser
	}
	quote := ""
	for _, m := range msgs {
		// Messages are ordered oldest-first, so the loop leaves the NEWEST
		// message of that side — the one the other side has not seen.
		if m.AuthorKind == wantAuthor {
			quote = m.Body
		}
	}
	if quote == "" {
		// Only reached when the thread carries no message from that side at
		// all; a retry is still a real attempt, so it falls back to the
		// subject rather than sending an empty mail.
		quote = feedbackSubjectOrExcerpt(th, h.feedbackLang(th.UserID))
	}
	if th.NotifyFailedKind == dbstore.FeedbackNotifyKindAdmin {
		// THE OUTCOME IS THE ATTEMPT'S OWN. This used to re-read the marker and
		// answer "queued" whenever no marker was left, which conflates "it
		// worked" with "nothing was attempted". notifyFeedbackAdmins has an exit
		// that writes no marker (no recipients configured), so an operator whose
		// allowlist was empty was told the notification had gone out while the
		// queue held zero jobs — the false-positive-claim class this surface
		// exists to remove, on its own retry path. Every value published here is
		// now the one the attempt produced, so "queued" implies a job left the
		// process and a skip answers "skipped".
		return h.notifyFeedbackAdmins(th, quote), nil
	}
	return h.notifyFeedbackFiler(sc, th, quote), nil
}

// feedbackSubjectOrExcerpt names a thread for an email line: its subject, or a
// placeholder when the filer left the subject empty (the UI makes it optional,
// so it is often empty). It is the FALLBACK for a notification that has no
// message from the side being told — see RetryFeedbackNotify. The placeholder is
// rendered in lang so it does not put an English "(no subject)" in a Malay email.
func feedbackSubjectOrExcerpt(th *dbstore.FeedbackThread, lang string) string {
	if th == nil || strings.TrimSpace(th.Subject) == "" {
		return emailtext.Phrase(lang, emailtext.PhraseNoSubject)
	}
	return th.Subject
}

// feedbackQuotedText is the message text quoted in a notification email.
//
// THERE IS NO LENGTH BOUND, AND THAT IS DELIBERATE. This used to cut the text at
// 400 runes and append "…", with no explanation to the reader: an admin reply
// longer than a few sentences arrived cut mid-thought and read as though the
// message simply ended (the "abrupt …" defect). The bound was defensive and had
// no evidence behind it — a feedback message is ONE message, bounded at the
// filing/validation layer by feedbacksvc.BodyMax = 5000 characters, which is
// ~5 KB of plain text and well inside every mail client's and SES's own limits
// (SES accepts a 10 MB message). So the full text is carried and nothing is
// silently dropped; if a bound is ever needed it must come with a stated reason
// and an honest "shortened — read the rest in the thread" line, never an orphan
// ellipsis.
func feedbackQuotedText(body string) string {
	return strings.TrimSpace(body)
}

// feedbackExcerpt bounds the message text quoted in the ADMIN alert, so a very
// long FILING does not produce an unreadable email. That alert's job is to get
// the platform team INTO the thread (its body carries the link), so the full text
// does not have to travel — unlike the filer's reply mail above, which IS the
// answer the filer was waiting for and is therefore carried whole.
func feedbackExcerpt(body string) string {
	const max = 400
	b := strings.TrimSpace(body)
	if len(b) <= max {
		return b
	}
	// Cut on a rune boundary so a multi-byte character is never split.
	r := []rune(b)
	if len(r) <= max {
		return b
	}
	return string(r[:max]) + "…"
}
