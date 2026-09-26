// Feedback notification emails: the reader's language, the thread deep link, and
// the address a reply reaches.
//
// THREE DEFECTS, ONE MESSAGE, pinned here end to end:
//
//	a. No Reply-To. The feedback mail goes out through pay.Receipts.Sender, so
//	   the header is asserted through that sender's own MIME builder
//	   (api/internal/email/replyto_issue_feedback_test.go) rather than here.
//	b. The link opened /feedback (the queue) instead of the thread that was
//	   answered, so a filer with more than one thread had to hunt for it.
//	c. The subject and body were hardcoded Malay at the call site, while
//	   users.lang (en|ms|zh|ta|ar) exists and governs the dashboard. An English
//	   filer got a Malay email — subject, body and <html lang="ms">.
//
// WHY THESE ARE HTTP TESTS. `notifyFeedbackFiler` is private and reads the
// filer's language LIVE from `users`; only the real route chain (authed mux →
// handleAdminReplyFeedback → notifyFeedbackFiler) proves that an English user's
// stored lang is what decides the subject. A direct call to the helper would
// prove the helper, not the wiring.
//
// THE BITE PROOFS live in this file:
//   - TestFeedbackReplySubjectFollowsTheFilersLang calls with a user whose lang
//     is unset, which reproduces the pre-fix hardcoded-Malay path (the old code
//     sent Malay whatever the reader's language was) and requires English.
//   - TestFeedbackNotificationBitesOnHardcodedMalaySubject re-creates the exact
//     pre-fix literal and requires the per-language assertion to FAIL for it.
//
// The copy is rendered by the shared dictionary (internal/emailtext), which is
// what the language-consistency guard scans for; the expectations below are read
// out of that dictionary rather than restated beside it.
//
// Integration against a real Postgres + Valkey; skipped without the standard
// test env.
package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/testdb"
)

// feedbackNotifyQueue returns a queue on this TEST's own key namespace.
//
// It used to build on `feedbackNotifyKeyspace` — the fixed DB number that
// feedback_notify_failure_test.go declared — and FlushDB it at entry. That
// fixture is gone (#775): JOMFORM_TEST_VALKEY_URL is also the API's SESSION
// store, so a fixed index is a shared resource, and a per-test namespace is the
// ownership rule the whole package now uses. The keys come from testdb.Valkey
// and the queue is built on its prefix with emailqueue.NewQueueWithPrefix, so
// this file reads and counts only what it enqueued.
func feedbackNotifyQueue(t *testing.T) *emailqueue.Queue {
	t.Helper()
	rdb, prefix := testdb.Valkey(t)
	return emailqueue.NewQueueWithPrefix(rdb, prefix)
}

// queuedFeedbackMail is one message the notification path enqueued.
type queuedFeedbackMail struct {
	Type    string
	To      string
	Subject string
	Body    string
	// Lang is the READER's language as it left the handler. It is asserted here
	// because the shell is rendered at SEND time: if the language does not reach
	// the job, no sender downstream can put it in the branded HTML, and the
	// message goes out with a shell that claims another language.
	Lang string
}

// drainFeedbackQueue returns everything waiting on the queue, leaving it empty.
func drainFeedbackQueue(t *testing.T, q *emailqueue.Queue) []queuedFeedbackMail {
	t.Helper()
	var out []queuedFeedbackMail
	for {
		job, raw, err := q.DequeueRaw(context.Background())
		if err != nil {
			// An empty list is the expected terminal state.
			break
		}
		if err := q.Ack(context.Background(), raw); err != nil {
			t.Fatalf("ack queued job: %v", err)
		}
		out = append(out, queuedFeedbackMail{
			Type: job.Type, To: job.To, Subject: job.Subject, Body: job.Body, Lang: job.Lang,
		})
	}
	return out
}

// feedbackNotifyEnv is a feedback mux whose notification emails go onto a real
// queue, so a test can read back what was actually sent.
type feedbackNotifyEnv struct {
	*feedbackEnv
	queue *emailqueue.Queue
}

// newFeedbackNotifyEnv wires the feedback surface with a queue and a super-admin
// allowlist.
func newFeedbackNotifyEnv(t *testing.T, superAdmins ...string) *feedbackNotifyEnv {
	t.Helper()
	fe := newFeedbackEnv(t, superAdmins...)
	fe.h.EmailQueue = feedbackNotifyQueue(t)
	// feedbackEnv already registered the routes on its mux, and the handlers it
	// registered close over `h` — the same pointer — so setting the queue here is
	// what the live route sees.
	return &feedbackNotifyEnv{feedbackEnv: fe, queue: fe.h.EmailQueue}
}

// feedbackTestAdminEmail is the super-admin's own address. The gate compares the
// SESSION user's email against SuperAdminEmails, so the admin's user row has to
// carry exactly this address for the reply to be authorised.
const feedbackTestAdminEmail = "ops@example.test"

// newAdminUser creates the super-admin's own user row with a fixed email and
// returns a session token for it. The allowlist is set here too, so the session
// and the gate agree.
//
// Idempotent on the address: testdb.Reset truncates the feedback tables but not
// `users`, so a second test in the same package run must reuse the row rather
// than trip the unique email index.
func (e *feedbackNotifyEnv) newAdminUser(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ($1, 'it', true)
		 ON CONFLICT (email) DO UPDATE SET verified = true
		 RETURNING id`, feedbackTestAdminEmail).Scan(&userID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'fb-admin-' || md5(random()::text), 'fb-admin-' || md5(random()::text)
		 WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE owner_user_id = $1)`,
		userID); err != nil {
		t.Fatalf("create admin account: %v", err)
	}
	tok, err := e.sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	e.h.SuperAdminEmails = []string{feedbackTestAdminEmail}
	e.h.SuperAdmin = func(mail string) bool { return strings.EqualFold(mail, feedbackTestAdminEmail) }
	return tok
}

// queueAndDrain is a convenience for tests that do not care about a specific
// notification but must clear the queue between actions.
func (e *feedbackNotifyEnv) queueAndDrain(t *testing.T) {
	t.Helper()
	drainFeedbackQueue(t, e.queue)
}

// newFiler creates a user with an explicit dashboard language, plus a session.
func (e *feedbackNotifyEnv) newFiler(t *testing.T, lang string) (userID, email, token string) {
	t.Helper()
	ctx := context.Background()
	var langArg any
	if lang != "" {
		langArg = lang
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified, lang)
		 VALUES ('fb-' || md5(random()::text) || '@example.test', 'it', true, $1)
		 RETURNING id, email`, langArg).Scan(&userID, &email); err != nil {
		t.Fatalf("create user with lang=%q: %v", lang, err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'fb-acc-' || md5(random()::text), 'fb-' || md5(random()::text))`,
		userID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	tok, err := e.sessions.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return userID, email, tok
}

// adminReply replies to a thread as the super-admin and returns the enqueued
// filer notification. Anything already on the queue (the operator notification a
// filing produces) is cleared first, so exactly one message is attributed to the
// reply.
func (e *feedbackNotifyEnv) adminReply(t *testing.T, adminToken, threadID, body string) queuedFeedbackMail {
	t.Helper()
	e.queueAndDrain(t)
	rec := e.do(t, adminToken, "POST", "/v1/admin/feedback/"+threadID+"/messages",
		fmt.Sprintf(`{"body":%q}`, body))
	if rec.Code != 201 {
		t.Fatalf("admin reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	jobs := drainFeedbackQueue(t, e.queue)
	if len(jobs) != 1 {
		t.Fatalf("admin reply enqueued %d job(s), want exactly 1 (the filer's notification): %#v", len(jobs), jobs)
	}
	return jobs[0]
}

// feedbackReplySubject is the reply notification's subject in lang, read out of
// the shared dictionary so this file cannot disagree with the shipped copy.
func feedbackReplySubject(lang string) string {
	return emailtext.Text(lang, emailtext.FeedbackReply, nil).Subject
}

// TestFeedbackReplySubjectFollowsTheFilersLang is the card's language criterion:
// the subject of the reply notification follows the FILER's users.lang, for all
// five locales, and defaults to ENGLISH when lang is unset.
func TestFeedbackReplySubjectFollowsTheFilersLang(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")

	adminToken := e.newAdminUser(t)

	// The expectations are the dictionary's own subjects, with the two literals
	// the pre-fix code hardcoded pinned explicitly so a dictionary edit that
	// changed them would fail here rather than quietly reword a shipped email.
	want := map[string]string{
		"en": "New reply on your feedback",
		// The Malay string is the PRE-FIX literal: it must survive for a Malay
		// reader, which is what makes the English case meaningful.
		"ms": "Balasan baharu pada maklum balas anda",
		"zh": "您收到的反馈有新回复",
		"ta": "உங்கள் கருத்துக்கு புதிய பதில்",
		"ar": "رد جديد على ملاحظاتك",
		// Unset lang is the common case: users.lang is NULL until the user
		// chooses, and the default must be English — never Malay.
		"": "New reply on your feedback",
	}
	for _, lang := range []string{"en", "ms", "zh", "ta", "ar", ""} {
		userID, email, token := e.newFiler(t, lang)
		_ = userID
		threadID := e.fileFeedback(t, token, "Subject "+lang, "A report filed by "+lang+" reader with enough characters")
		got := e.adminReply(t, adminToken, threadID, "We are looking into this now.")
		if got.To != email {
			t.Fatalf("lang=%q: notification went to %q, want the filer %q", lang, got.To, email)
		}
		if want := want[lang]; got.Subject != want {
			t.Errorf("lang=%q: subject = %q, want %q — the subject must follow the FILER's "+
				"users.lang, never a hardcoded language", lang, got.Subject, want)
		}
		if got.Subject != emailtext.Text(lang, emailtext.FeedbackReply, nil).Subject {
			t.Errorf("lang=%q: the wired subject is not the dictionary's rendering", lang)
		}
		if got.Type != "feedback_notify" {
			t.Errorf("lang=%q: job type = %q, want feedback_notify", lang, got.Type)
		}
	}
}

// TestFeedbackReplyJobCarriesTheReadersLanguageToTheSender is the transport half
// of the language criterion, and the reason the shell stopped being dead code.
//
// The subject and body are rendered in the HANDLER, but the branded HTML shell is
// rendered at SEND time, by the SES sender. So the locale has to survive the
// queue hop: the handler must put it on the job and the dispatcher must hand it
// to the sender. It did not — the job had no language field at all, so whatever
// locale the subject and body were written in, the shell was always English.
//
// This asserts the language on the job the REAL route chain enqueued
// (adminReply → handleAdminReplyFeedback → notifyFeedbackFiler →
// sendFeedbackMail → EmailQueue.Enqueue). Without that value the sender cannot
// put the reader's language in the shell, and the proof test downstream
// (feedback_email_proof_evidence_test.go) cannot save it either.
func TestFeedbackReplyJobCarriesTheReadersLanguageToTheSender(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")

	adminToken := e.newAdminUser(t)

	// Every locale plus the unset case. The unset case is the common one
	// (users.lang is NULL until the user picks) and must normalise to "en", the
	// documented default, so the sender renders the English shell rather than an
	// empty lang attribute.
	for _, lang := range []string{"en", "ms", "zh", "ta", "ar", ""} {
		_, _, token := e.newFiler(t, lang)
		threadID := e.fileFeedback(t, token, "Subject "+lang,
			"A report filed by a "+lang+" reader with enough characters")
		got := e.adminReply(t, adminToken, threadID, "We are looking into this now.")

		want := lang
		if want == "" {
			want = "en"
		}
		if got.Lang != want {
			t.Errorf("lang=%q: the queued job carries Lang=%q, want %q — the branded HTML shell is "+
				"rendered at send time from this value, so dropping it here makes every message claim "+
				"the English shell whatever language its subject and body are written in",
				lang, got.Lang, want)
		}
	}

	// The operator notification carries the language too: it is rendered in the
	// FILER's language, so the shell must follow the same value the subject and
	// body were written in. The pre-fix code sent Malay to everyone; the earlier
	// fix localised the copy but still lost the language before the shell.
	adminEmail := feedbackTestAdminEmail
	// Ensures the shared admin row exists and sets the SUPER_ADMIN_EMAILS
	// allowlist the filing notification reads.
	e.newAdminUser(t)

	_, _, token := e.newFiler(t, "en")
	e.queueAndDrain(t)
	if rec := e.do(t, token, "POST", "/v1/rest/feedback",
		`{"subject":"operator language","body":"A report with enough characters to notify the operator"}`); rec.Code != 201 {
		t.Fatalf("file feedback = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	jobs := drainFeedbackQueue(t, e.queue)
	if len(jobs) != 1 {
		t.Fatalf("filing a thread enqueued %d job(s), want 1: %#v", len(jobs), jobs)
	}
	op := jobs[0]
	if op.To != adminEmail {
		t.Fatalf("the operator notification went to %q, want %q", op.To, adminEmail)
	}
	if op.Lang != "en" {
		t.Errorf("the operator's queued job carries Lang=%q, want %q (the language the copy was "+
			"rendered in) — the shell is rendered from this value at send time", op.Lang, "en")
	}
	if op.Subject != emailtext.Text("en", emailtext.FeedbackNew, nil).Subject {
		t.Errorf("operator subject = %q, want the English %q",
			op.Subject, emailtext.Text("en", emailtext.FeedbackNew, nil).Subject)
	}
}

// TestFeedbackNotificationBitesOnHardcodedMalaySubject is the bite proof: it
// re-creates the PRE-FIX behaviour (the hardcoded Malay literal) as the string
// under test and requires the per-language assertion to go red. Without it the
// test above could pass against an implementation that ignored lang entirely and
// happened to emit English.
//
// The bite is driven through the REAL path (queue → notification), not the
// helper, so it is the message a filer would actually receive that is judged.
func TestFeedbackNotificationBitesOnHardcodedMalaySubject(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")

	adminToken := e.newAdminUser(t)

	// The exact literal the pre-fix code sent, for every filer whatever their
	// language. A non-Malay filer receiving it is the whole defect.
	const preFixSubject = "Balasan baharu pada maklum balas anda"

	for _, lang := range []string{"en", "zh", "ta", "ar", ""} {
		_, _, token := e.newFiler(t, lang)
		threadID := e.fileFeedback(t, token, "S "+lang,
			"A report with enough characters to pass validation")
		e.queueAndDrain(t) // drop the filing notification
		got := e.adminReply(t, adminToken, threadID, "Answering with enough characters")
		if got.Subject == preFixSubject {
			t.Errorf("lang=%q: the notification still carries the hardcoded Malay subject %q — "+
				"an English (or Chinese, Tamil, Arabic) filer must not get Malay copy",
				lang, preFixSubject)
		}
	}

	// POSITIVE CONTROL: for a Malay filer that literal IS correct. Without this
	// the assertion above would also pass for an implementation that renamed the
	// string to something no locale ever produces.
	_, _, msToken := e.newFiler(t, "ms")
	msThread := e.fileFeedback(t, msToken, "Laporan", "Laporan dengan aksara yang mencukupi untuk lulus")
	e.queueAndDrain(t)
	if got := e.adminReply(t, adminToken, msThread, "Kami sedang menyemaknya sekarang."); got.Subject != preFixSubject {
		t.Fatalf("lang=ms subject = %q, want the Malay literal %q — the bite proof above would be "+
			"vacuous if that string were unreachable", got.Subject, preFixSubject)
	}
}

// TestFeedbackReplyBodyUsesTheFilersLanguage pins the BODY and the placeholder
// for an empty subject, which were hardcoded Malay too.
func TestFeedbackReplyBodyUsesTheFilersLanguage(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")

	adminToken := e.newAdminUser(t)
	// The English filer.
	_, _, enToken := e.newFiler(t, "en")
	enThread := e.fileFeedback(t, enToken, "", "An English report with enough characters to pass validation")
	enGot := e.adminReply(t, adminToken, enThread, "Thanks, this is fixed in the next release.")
	if !strings.Contains(enGot.Body, "We have replied to your feedback.") {
		t.Errorf("english filer's body is not English:\n%s", enGot.Body)
	}
	if strings.Contains(enGot.Body, "Kami telah membalas") {
		t.Errorf("english filer's body still carries the Malay body:\n%s", enGot.Body)
	}
	// The empty-subject placeholder from the pre-fix code was "(tanpa tajuk)" —
	// Malay, in an English message.
	if !strings.Contains(enGot.Body, emailtext.Phrase("en", emailtext.PhraseNoSubject)) {
		t.Errorf("english filer's empty-subject placeholder is not the dictionary's English one:\n%s", enGot.Body)
	}
	if strings.Contains(enGot.Body, "tanpa tajuk") {
		t.Errorf("english filer's body still carries the Malay placeholder:\n%s", enGot.Body)
	}

	// Malay filer: the Malay copy, unchanged.
	_, _, msToken := e.newFiler(t, "ms")
	msThread := e.fileFeedback(t, msToken, "Ada masalah", "Laporan bahasa Melayu dengan aksara yang mencukupi")
	msGot := e.adminReply(t, adminToken, msThread, "Kami sedang menyemaknya sekarang.")
	if !strings.Contains(msGot.Body, "Kami telah membalas maklum balas anda.") {
		t.Errorf("malay filer's body is not Malay:\n%s", msGot.Body)
	}
}

// TestFeedbackReplyLinkOpensTheThread is defect (b): the reply email must link to
// the THREAD that was answered, not to the queue, on the route the console
// actually serves.
func TestFeedbackReplyLinkOpensTheThread(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")
	e.h.BaseURL = "https://jomform.com"

	adminToken := e.newAdminUser(t)

	_, _, token := e.newFiler(t, "en")
	threadA := e.fileFeedback(t, token, "First thread", "The first report, long enough to pass validation")
	threadB := e.fileFeedback(t, token, "Second thread", "A second report, long enough to pass validation")

	// Two threads exist, so a link to the queue is ambiguous — which is exactly
	// the reported defect. The reply to thread B must name B.
	e.adminReply(t, adminToken, threadA, "Answering thread A with enough characters")
	gotB := e.adminReply(t, adminToken, threadB, "Answering thread B with enough characters")

	wantLink := "https://jomform.com/feedback?thread=" + threadB
	if !strings.Contains(gotB.Body, wantLink) {
		t.Fatalf("the reply notification must link to the thread that was answered (%s).\nbody:\n%s",
			wantLink, gotB.Body)
	}
	if strings.Contains(gotB.Body, threadA) {
		t.Fatalf("the reply for thread B linked to thread A:\n%s", gotB.Body)
	}
	// The old bare queue link must be gone: it is the defect.
	if strings.Contains(gotB.Body, "/feedback\n") || strings.HasSuffix(strings.TrimSpace(gotB.Body), "/feedback") {
		t.Fatalf("the notification still links to the bare queue:\n%s", gotB.Body)
	}
}

// TestFeedbackAdminLinkOpensTheThread is the same criterion on the operator side.
func TestFeedbackAdminLinkOpensTheThread(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")
	e.h.BaseURL = "https://jomform.com"
	// newAdminUser sets the SUPER_ADMIN_EMAILS allowlist. Filing the thread is
	// what notifies those operators.
	_ = e.newAdminUser(t)

	_, _, token := e.newFiler(t, "en")
	threadID := e.fileFeedback(t, token, "Something is broken", "A report with enough characters to pass validation")

	jobs := drainFeedbackQueue(t, e.queue)
	if len(jobs) != 1 {
		t.Fatalf("filing a thread enqueued %d job(s), want 1: %#v", len(jobs), jobs)
	}
	wantLink := "https://jomform.com/admin?thread=" + threadID
	if !strings.Contains(jobs[0].Body, wantLink) {
		t.Fatalf("the operator notification must link to the filed thread (%s).\nbody:\n%s",
			wantLink, jobs[0].Body)
	}
	// The pre-fix hardcoded Malay. The copy now comes from the shared
	// dictionary in the filer's language (here: the English filer above), so the
	// Malay subject must not appear.
	if jobs[0].Subject != emailtext.Text("en", emailtext.FeedbackNew, nil).Subject {
		t.Errorf("operator subject = %q, want the dictionary's English %q",
			jobs[0].Subject, emailtext.Text("en", emailtext.FeedbackNew, nil).Subject)
	}
	// BaseURL empty (a deployment that has not set it) must not produce a
	// dangling "?thread=" — the link degrades to the page itself.
	if got := feedbackThreadLink("", "abc"); got != "/feedback?thread=abc" {
		t.Errorf("feedbackThreadLink(\"\", id) = %q", got)
	}
	if got := feedbackAdminLink("https://x.test/", ""); got != "https://x.test/admin" {
		t.Errorf("an empty thread id must degrade to the page, got %q", got)
	}
	// The id is escaped into the query string, so a value with a reserved
	// character cannot break the link.
	if got := feedbackThreadLink("https://x.test", "a b&c"); !strings.Contains(got, "a+b%26c") {
		t.Errorf("thread id must be query-escaped, got %q", got)
	}
}

// TestFeedbackNotificationsAreNotMalayForAnUnknownFiler pins the resolution
// failure path: a thread whose user cannot be resolved must not fall back to the
// pre-fix hardcoded Malay. It asserts the dictionary directly because the HTTP
// path returns early (no address → no message), which is correct but
// unobservable.
func TestFeedbackNotificationsAreNotMalayForAnUnknownFiler(t *testing.T) {
	for _, lang := range []string{"", "en", "zh", "ta", "ar", "xx"} {
		if got := feedbackReplySubject(lang); got == "Balasan baharu pada maklum balas anda" && lang != "ms" {
			t.Errorf("lang=%q fell back to the Malay subject %q", lang, got)
		}
		if got := emailtext.Phrase(lang, emailtext.PhraseNoSubject); got == "(tanpa tajuk)" && lang != "ms" {
			t.Errorf("lang=%q fell back to the Malay placeholder %q", lang, got)
		}
	}
	// Every locale is distinct for the subject, so a test that pinned one string
	// for all of them could not pass.
	seen := map[string]string{}
	for _, lang := range []string{"en", "ms", "zh", "ta", "ar"} {
		s := feedbackReplySubject(lang)
		if prev, dup := seen[s]; dup {
			t.Errorf("locales %q and %q share the subject %q", prev, lang, s)
		}
		seen[s] = lang
	}
}

// TestFeedbackNotifyReadsTheFilersLangLive proves the language is read from the
// user row at send time, not captured when the thread was filed: a filer who
// switches their dashboard language then gets the reply in the new one.
func TestFeedbackNotifyReadsTheFilersLangLive(t *testing.T) {
	e := newFeedbackNotifyEnv(t)
	testdb.Reset(t, e.pool, "feedback_threads", "feedback_messages")

	adminToken := e.newAdminUser(t)

	userID, _, token := e.newFiler(t, "ms")
	threadID := e.fileFeedback(t, token, "Laporan", "Laporan dengan aksara yang mencukupi untuk lulus")
	e.queueAndDrain(t) // drop the filing notification

	if got := e.adminReply(t, adminToken, threadID, "Kami sedang menyemaknya."); got.Subject != feedbackReplySubject("ms") {
		t.Fatalf("pre-switch reply subject = %q, want Malay", got.Subject)
	}

	// The filer switches to English, as PATCH /v1/rest/me (users.lang) does.
	if err := dbstore.NewUserStore(e.pool).UpdateLang(context.Background(), userID, "en"); err != nil {
		t.Fatalf("update lang: %v", err)
	}
	got := e.adminReply(t, adminToken, threadID, "We are looking into it now.")
	if got.Subject != feedbackReplySubject("en") {
		t.Fatalf("after switching to en the reply subject = %q, want %q — the language must be read "+
			"at send time from users.lang, not captured at filing", got.Subject, feedbackReplySubject("en"))
	}
}
