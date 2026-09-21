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
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/feedbacksvc"
)

// Feedback job type enqueued on the #199 queue. It rides the SAME sender as the
// per-form notifications (pay.Receipts.Sender), so it passes the pre-send
// suppression guard; see email_dispatcher.go's case for it.
const feedbackNotifyJobType = "feedback_notify"

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
// identity: the caller IS the filer, and echoing an id back adds nothing.
type feedbackThreadOut struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastActivityAt string `json:"last_activity_at"`
	MessageCount   int64  `json:"message_count"`
}

// adminFeedbackThreadOut is the triage shape: it names who filed the thread,
// which the admin list needs to answer a question. user_email is the snapshot
// taken at filing time (the admin-facing list is a record of what was said, so
// it shows the address as it was then).
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

func toFeedbackThreadOut(t *dbstore.FeedbackThread) feedbackThreadOut {
	return feedbackThreadOut{
		ID: t.ID, Subject: t.Subject, Status: t.Status,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
		LastActivityAt: t.LastActivityAt.UTC().Format(time.RFC3339),
		MessageCount:   t.MessageCount,
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
	}
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
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	var in createFeedbackIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
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
			h.notifyFeedbackAdmins(th, body)
		},
	}
}

// handleListFeedback: GET /v1/rest/feedback?page=&limit= — the caller's own
// threads, newest activity first.
func (h *RESTHandlers) handleListFeedback(w http.ResponseWriter, r *http.Request, _ *AccountCtx) {
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	page, limit := parsePageLimit(r)
	// The status filter is accepted here too, so the user's own list can show
	// "resolved" separately. It is scoped by the same user predicate either way.
	statusOf := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if statusOf != "" && !dbstore.ValidFeedbackStatus(statusOf) {
		writeErr(w, http.StatusBadRequest, "status tidak sah (open|in_progress|resolved|closed)")
		return
	}
	threads, total, err := h.Feedback.ListFilteredPage(r.Context(),
		dbstore.FeedbackUserScope(uid), statusOf, page, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca maklum balas")
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
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
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
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
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
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.list")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	page, limit := parsePageLimit(r)
	statusOf := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if statusOf != "" && !dbstore.ValidFeedbackStatus(statusOf) {
		writeErr(w, http.StatusBadRequest, "status tidak sah (open|in_progress|resolved|closed)")
		return
	}
	threads, total, err := h.Feedback.ListFilteredPage(r.Context(),
		dbstore.FeedbackAdminScope(), statusOf, page, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca maklum balas")
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
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.get")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
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
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.reply")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
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
	h.notifyFeedbackFiler(th, body)
	h.recordFeedbackEvent(r, "feedback.admin_replied", id, map[string]any{"actor": "admin"})
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "sent", "message": toFeedbackMessageOut(m),
	})
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
		writeErr(w, http.StatusForbidden, "perlu super-admin untuk tindakan ini")
		return
	}
	h.auditSuperAdminAccess(r, "admin.feedback.set_status")
	if h.Feedback == nil {
		writeErr(w, http.StatusServiceUnavailable, "sistem maklum balas tidak dikonfigurasi")
		return
	}
	var in setFeedbackStatusIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if !dbstore.ValidFeedbackStatus(status) {
		writeErr(w, http.StatusBadRequest, "status tidak sah (open|in_progress|resolved|closed)")
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
		writeErr(w, http.StatusNotFound, "thread maklum balas tidak dijumpai")
		return
	}
	writeErr(w, http.StatusInternalServerError, "gagal memproses maklum balas")
}

// readFeedbackBody decodes + validates a message body for either surface.
func (h *RESTHandlers) readFeedbackBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in replyFeedbackIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "badan permintaan tidak sah")
		return "", false
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < feedbackBodyMin {
		writeErr(w, http.StatusBadRequest, "sila tulis sekurang-kurangnya 10 aksara")
		return "", false
	}
	if len(body) > feedbackBodyMax {
		writeErr(w, http.StatusBadRequest, "mesej terlalu panjang (maksimum 5000 aksara)")
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
// NotifyFeedbackAdmins is the exported form: the MCP submit_feedback tool and
// the in-app chat file through the same feedbacksvc, whose Notify hook is this
// function, so a thread filed from any surface notifies identically.
func (h *RESTHandlers) NotifyFeedbackAdmins(th *dbstore.FeedbackThread, body string) {
	h.notifyFeedbackAdmins(th, body)
}

func (h *RESTHandlers) notifyFeedbackAdmins(th *dbstore.FeedbackThread, body string) {
	if th == nil || len(h.SuperAdminEmails) == 0 {
		return
	}
	subj := "Maklum balas baharu diterima"
	text := "Maklum balas baharu diterima.\n\n" +
		"Dari   : " + th.UserEmail + "\n" +
		"Status : " + th.Status + "\n" +
		"Tajuk  : " + feedbackSubjectOrExcerpt(th) + "\n\n" +
		feedbackExcerpt(body) + "\n\n" +
		"Balas di " + strings.TrimRight(h.BaseURL, "/") + "/admin/feedback\n"
	for _, to := range h.SuperAdminEmails {
		h.sendFeedbackMail(to, subj, text)
	}
}

// notifyFeedbackFiler tells the filer an admin replied. The address is read
// live from `users` by the user id on the thread, so a filer who changed their
// address is notified at the new one.
func (h *RESTHandlers) notifyFeedbackFiler(th *dbstore.FeedbackThread, body string) {
	if th == nil || h.Users == nil {
		return
	}
	u, err := h.Users.ByID(context.Background(), th.UserID)
	if err != nil || u == nil || u.Email == "" {
		return
	}
	subj := "Balasan baharu pada maklum balas anda"
	text := "Kami telah membalas maklum balas anda.\n\n" +
		"Tajuk  : " + feedbackSubjectOrExcerpt(th) + "\n\n" +
		feedbackExcerpt(body) + "\n\n" +
		"Lihat thread di " + strings.TrimRight(h.BaseURL, "/") + "/feedback\n"
	h.sendFeedbackMail(u.Email, subj, text)
}

// sendFeedbackMail delivers one feedback email through the existing queue, with
// the synchronous mailer as the fallback when no queue is wired (bare-mode
// tests). Both paths are the ones every other notification uses — this adds no
// second channel.
func (h *RESTHandlers) sendFeedbackMail(to, subject, body string) {
	if to == "" {
		return
	}
	if h.EmailQueue != nil {
		// A queue error is logged by the caller's path already (best-effort
		// notification); the thread itself is safely stored.
		_ = h.EmailQueue.Enqueue(context.Background(), emailqueue.Job{
			Type: feedbackNotifyJobType, To: to, Subject: subject, Body: body,
		})
		return
	}
	if !mailerUnavailable(h.Mailer) {
		_ = h.Mailer.Send(to, subject, body)
	}
}

// feedbackSubjectOrExcerpt names a thread for an email line: its subject, or the
// first message's opening characters when the filer left the subject empty (the
// UI makes it optional, so it is often empty).
func feedbackSubjectOrExcerpt(th *dbstore.FeedbackThread) string {
	if strings.TrimSpace(th.Subject) != "" {
		return th.Subject
	}
	return "(tanpa tajuk)"
}

// feedbackExcerpt bounds the message text quoted in a notification, so a long
// filing does not produce an unreadable email.
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
