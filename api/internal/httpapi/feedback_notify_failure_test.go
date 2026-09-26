// Feedback notification: a failed delivery is recorded, surfaced and retryable,
// and a long reply is no longer cut in silence.
//
// THE TWO DEFECTS THIS FILE EXISTS FOR.
//
//	d. sendFeedbackMail discarded BOTH errors (Enqueue and Send), notifyFeedback-
//	   Filer returned nothing, and handleAdminReplyFeedback answered
//	   201 {"status":"sent"} regardless. So an admin whose reply never left the
//	   building was told the filer had been notified, the filer waited for mail
//	   that was never queued, and nothing on any surface said otherwise — the
//	   thread, the board and the audit log all looked healthy.
//
//	e. feedbackExcerpt cut the quoted reply at 400 RUNES and appended "…", with
//	   no explanation. A reply longer than a few sentences arrived mid-thought
//	   and read as though the message simply ended.
//
// WHY THESE ARE HTTP TESTS. A unit call on sendFeedbackMail proves the function
// returns an error; it does not prove the ROUTE publishes it or that the marker
// lands on the thread an operator opens. These drive the real mux (RegisterRoutes
// + authed()), so what is proven is the composed behaviour — the same reason
// feedback_isolation_issue615_test.go is an HTTP test.
//
// THE BITE CONTROL is TestFeedbackNotifyFailureIsSurfacedAndRecorded's second
// half: the same route against a WORKING queue must answer "queued" and leave no
// marker. Without it, an implementation that answered "failed" unconditionally
// would pass the failure assertion while delivering nothing. The discarded-error
// mutation (`_ = h.EmailQueue.Enqueue(...)`) turns that half red — verified by
// hand on this branch.
//
// Integration against a real Postgres (+ Valkey for the enqueue paths), skipped
// without the standard test env.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/testdb"
)

// feedbackNotifyFixture is one wired env: a super-admin allowlist with a token
// for the admin, a filer token, and a filed thread.
type feedbackNotifyFixture struct {
	env        *feedbackEnv
	tokenUser  string
	tokenAdmin string
	adminEmail string
	thread     string
}

// newNotifyFixture builds the env, seeds a filer and a super-admin, and files one
// thread. It performs no sends itself, so each test owns the queue it hands over.
//
// The super-admin ALLOWLIST is installed only AFTER the first thread is filed.
// That is deliberate: the env has no email channel wired yet, so filing with
// recipients configured would record a (correct) delivery failure on the fixture
// thread and every test would start from a marker it did not set. Filing first
// means the baseline thread is genuinely unmarked — "no recipients configured" is
// a skip, not a failure.
func newNotifyFixture(t *testing.T) *feedbackNotifyFixture {
	t.Helper()
	e := newFeedbackEnv(t)
	_, _, tokenUser := e.newFeedbackUser(t)
	_, adminEmail, tokenAdmin := e.newFeedbackUser(t)

	allow := map[string]bool{strings.ToLower(strings.TrimSpace(adminEmail)): true}
	e.h.SuperAdmin = func(email string) bool { return allow[strings.ToLower(strings.TrimSpace(email))] }

	// File the baseline thread BEFORE the allowlist is set, then set it. The env
	// has no email channel wired at this point, so "no recipients configured" is
	// a logged SKIP (not a failure) and the baseline thread stays unmarked —
	// otherwise every test below would start from a marker it did not write.
	thread := e.fileFeedback(t, tokenUser, "Pembayaran tersekat",
		"pembayaran saya tersekat sejak pagi tadi")
	if at, _, _ := readNotifyMarker(t, e, thread); at != nil {
		t.Fatal("the fixture's baseline thread already carries a failure marker — the tests below " +
			"could not tell their own writes from this one")
	}
	e.h.SuperAdminEmails = []string{adminEmail}

	return &feedbackNotifyFixture{
		env: e, tokenUser: tokenUser, tokenAdmin: tokenAdmin,
		adminEmail: adminEmail, thread: thread,
	}
}

// fileThread files one extra thread through the real route, as the filer.
func (f *feedbackNotifyFixture) fileThread(t *testing.T, subject, body string) string {
	t.Helper()
	return f.env.fileFeedback(t, f.tokenUser, subject, body)
}

// readNotifyMarker reads the failure marker off the row, so the assertion does
// not depend on the JSON shape the API chose to publish.
func readNotifyMarker(t *testing.T, e *feedbackEnv, thread string) (*time.Time, string, string) {
	t.Helper()
	var at *time.Time
	var kind, errText *string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT notify_failed_at, notify_failed_kind, notify_failed_error
		   FROM feedback_threads WHERE id = $1`, thread).Scan(&at, &kind, &errText); err != nil {
		t.Fatalf("read notify marker: %v", err)
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return at, deref(kind), deref(errText)
}

// deadQueue returns a queue whose Valkey is unreachable (nothing listens on
// 127.0.0.1:1), so Enqueue fails the way a down or refusing queue does. Retries
// are disabled so the failure is immediate rather than three dial attempts deep.
func deadQueue(t *testing.T) *emailqueue.Queue {
	t.Helper()
	rdb := goredis.NewClient(&goredis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		MaxRetries:   -1, // -1 disables retries (0 means "use the default of 3")
	})
	t.Cleanup(func() { _ = rdb.Close() })
	// Prove the fixture is actually broken before any test relies on it: a
	// queue that silently worked would make every "failed" assertion pass for
	// the wrong reason (the #507 vacuous-fixture trap).
	if err := rdb.Ping(context.Background()).Err(); err == nil {
		t.Fatal("fixture is not broken: the dead queue's Valkey answered PING")
	}
	return emailqueue.NewQueue(rdb)
}

// liveQueue returns a queue on this TEST's own key namespace, plus the client
// backing it, so a test can inspect what was enqueued.
//
// The keys come from testdb.Valkey (issue #21aec38d), and that is what the
// isolation rests on. This fixture used to bypass the repo's scheme entirely:
// it hardcoded `feedbackNotifyKeyspace = 14`, discarded the index in
// JOMFORM_TEST_VALKEY_URL, and called FlushDB on 14 at every test entry.
//
// That fixed index WAS a session store. `JOMFORM_TEST_VALKEY_URL` is not only
// this file's queue URL — api/internal/auth/session.go:31 builds the SESSIONS
// client from the same variable, so with the env pointed at `…/14` the API's
// `jf:session:*` keys live in DB 14 and this fixture deleted them. Measured on
// this file's pre-fix shape (own Postgres + own Valkey, nothing else attached):
//
//	env index 3, planted in DB 14: jf:session:peer-probe, jf:emailq:jobs
//	one guard's run:               both keys gone, DBSIZE 14 = 0
//
// and with the env pointed at /14 the file wiped the sessions it had just
// minted, so 10 of its own 11 queue guards answered `retry = 401, want 200`.
// A hand-maintained list of "taken" indexes is what produced that: the list in
// the old comment here was already stale (testdb's own reserved set is
// {8, 10, 15} and includes /10, which that list called "the shared test env").
//
// So there is no index to pick any more, and nothing here deletes a key this
// run did not write: the namespace is per TEST, and testdb's cleanup SCANs it
// and deletes only its own keys. The queue is built on that same prefix with
// emailqueue.NewQueueWithPrefix, so the assertions below count THIS run's jobs
// rather than whatever shares the database.
//
// There is no "is it in use" assertion: a non-empty namespace is the NORMAL
// state here (the test just enqueued into it), so the guard would fire on
// correct code. Ownership is by the key NAMES; see api/internal/testdb/valkey.go.
func liveQueue(t *testing.T) (*emailqueue.Queue, *goredis.Client) {
	t.Helper()
	rdb, prefix := testdb.Valkey(t)
	q := emailqueue.NewQueueWithPrefix(rdb, prefix)
	// The assertions below read q.ReadyKey(), so if this queue were ever swapped
	// back to the production constructor they would silently read the SHARED
	// production key instead of this run's — the length check would then compare
	// 0 with 0 and pass while proving nothing. Refuse that shape here rather than
	// leaving it to a comment.
	if q.ReadyKey() == emailqueue.DefaultKeyPrefix+"jobs" {
		t.Fatalf("the fixture's queue is on the PRODUCTION key %q — the assertions below would read "+
			"and depend on a key this run does not own; build it on testdb.Valkey's prefix",
			q.ReadyKey())
	}
	if !strings.HasPrefix(q.ReadyKey(), prefix) {
		t.Fatalf("queue key %q is not under this test's namespace %q", q.ReadyKey(), prefix)
	}
	return q, rdb
}

// notifyQueueJobs reads the queue's ready list without claiming it. The key
// comes from the QUEUE, so it matches the prefix that queue writes under
// (issue #21aec38d) — a literal `jf:emailq:jobs` here would read the production
// key name and count a live worker's jobs, which is the shape that made the
// pre-fix version of this file count and delete other processes' state.
func notifyQueueJobs(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client) []emailqueue.Job {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), q.ReadyKey(), 0, -1).Result()
	if err != nil {
		t.Fatalf("read the enqueued jobs: %v", err)
	}
	out := make([]emailqueue.Job, 0, len(raw))
	for _, r := range raw {
		var j emailqueue.Job
		if err := json.Unmarshal([]byte(r), &j); err != nil {
			t.Fatalf("decode the enqueued job: %v\nraw: %s", err, r)
		}
		out = append(out, j)
	}
	return out
}

// notifyQueueJob is notifyQueueJobs for the single job a one-shot retry leaves.
func notifyQueueJob(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client) emailqueue.Job {
	t.Helper()
	jobs := notifyQueueJobs(t, q, rdb)
	if len(jobs) == 0 {
		t.Fatalf("no job on the queue under %s — the assertion is about a job that was enqueued", q.ReadyKey())
	}
	return jobs[0]
}

// feedbackNotifyLogs routes the default slog sink into a buffer for one test.
func feedbackNotifyLogs(t *testing.T) func() string {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf.String
}

// decodeNotifyResponse pulls the two fields the contract is about.
func decodeNotifyResponse(t *testing.T, body []byte) (status, notification string) {
	t.Helper()
	var out struct {
		Status       string `json:"status"`
		Notification string `json:"notification"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, body)
	}
	return out.Status, out.Notification
}

// TestFeedbackNotifyFailureIsSurfacedAndRecorded is the regression for defect d.
//
// Two halves, and the second is what makes the first mean anything:
//
//  1. the queue rejects the job ⇒ the reply's response must NOT claim the
//     notification succeeded, the failure must be logged with the job type and
//     the OBFUSCATED recipient, and the thread must carry the marker;
//  2. the same route with a working queue ⇒ "queued" and NO marker.
func TestFeedbackNotifyFailureIsSurfacedAndRecorded(t *testing.T) {
	logs := feedbackNotifyLogs(t)
	f := newNotifyFixture(t)
	e := f.env

	const reply = "Kami sedang menyemak isu ini dan akan maklumkan segera."

	// ── half 1: a queue that rejects the job ──
	e.h.EmailQueue = deadQueue(t)

	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		fmt.Sprintf(`{"body":%q}`, reply))
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin reply with a dead queue = %d, want 201 (the reply IS stored; only the "+
			"notification is best-effort) — body %s", rec.Code, rec.Body.String())
	}
	_, notification := decodeNotifyResponse(t, rec.Body.Bytes())
	if notification == "sent" || notification == "" {
		t.Errorf("the reply to a REJECTED enqueue answered notification=%q — the admin is told the "+
			"filer was notified when nothing was queued, which is the defect (body %s)",
			notification, rec.Body.String())
	}
	if notification != "failed" {
		t.Errorf("notification = %q, want \"failed\" for a rejected enqueue (body %s)",
			notification, rec.Body.String())
	}

	at, kind, errText := readNotifyMarker(t, e, f.thread)
	if at == nil {
		t.Fatal("the thread carries no notify_failed_at marker — the failure is invisible on the one " +
			"surface an operator opens, which is the whole point of the fix")
	}
	if kind != dbstore.FeedbackNotifyKindFiler {
		t.Errorf("notify_failed_kind = %q, want %q (the FILER was not told)",
			kind, dbstore.FeedbackNotifyKindFiler)
	}
	if !strings.Contains(errText, feedbackNotifyJobType) {
		t.Errorf("notify_failed_error = %q, want it to name the job type %q so an operator can tell "+
			"which lane failed", errText, feedbackNotifyJobType)
	}

	// The failure must also be logged, with the job type and the OBFUSCATED
	// recipient — never the full address.
	logged := logs()
	if !strings.Contains(logged, "feedback_notify_failed") {
		t.Errorf("no feedback_notify_failed log line; log:\n%s", logged)
	}
	if !strings.Contains(logged, feedbackNotifyJobType) {
		t.Errorf("the failure log does not name the job type %q; log:\n%s", feedbackNotifyJobType, logged)
	}
	if f.adminEmail != "" && strings.Contains(logged, f.adminEmail) {
		t.Errorf("the failure log leaked the full recipient address %q; log:\n%s", f.adminEmail, logged)
	}

	// The filer must be able to SEE it too: a filer whose answer never arrived
	// is the other person this defect hurt.
	filerRec := e.do(t, f.tokenUser, http.MethodGet, "/v1/rest/feedback/"+f.thread, "")
	if !strings.Contains(filerRec.Body.String(), "notify_failed_at") {
		t.Errorf("the filer's own thread read does not report the failed notification:\n%s",
			filerRec.Body.String())
	}

	// ── half 2 (the control): a queue that accepts the job ──
	// A NEW thread, so the first one's marker cannot mask a missing write.
	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	controlThread := f.fileThread(t, "Kawalan", "thread kawalan untuk mengesahkan tiada penanda palsu")

	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+controlThread+"/messages",
		fmt.Sprintf(`{"body":%q}`, reply))
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin reply with a live queue = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if _, notification := decodeNotifyResponse(t, rec.Body.Bytes()); notification != "queued" {
		t.Errorf("a reply whose job REACHED the queue answered notification=%q, want \"queued\" — an "+
			"implementation that always answers \"failed\" would pass the assertion above while "+
			"delivering nothing (body %s)", notification, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, controlThread); at != nil {
		t.Error("a thread whose notification was queued carries a failure marker — the marker must " +
			"mean what it says")
	}
}

// TestFeedbackNotifyMarkerIsClearedWhenALaterAttemptSucceeds proves the marker is
// state about the LAST attempt, not a permanent stain: once the queue is healthy
// again and a notification goes through, the thread stops advertising a failure
// that is no longer true. A marker that can never clear would train an operator
// to ignore it.
func TestFeedbackNotifyMarkerIsClearedWhenALaterAttemptSucceeds(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env

	e.h.EmailQueue = deadQueue(t)
	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		`{"body":"Balasan pertama yang gagal dihantar kepada pemfail"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, f.thread); at == nil {
		t.Fatal("precondition failed: the first (failing) reply recorded no marker")
	}

	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		`{"body":"Balasan kedua yang sempat masuk baris gilir"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, f.thread); at != nil {
		t.Error("a successful notification did not clear the earlier failure marker — the thread " +
			"keeps claiming the filer was not told after they were")
	}
}

// TestFeedbackNotifyRetryResendsAndClearsTheMarker is the release path: the
// marker is only half the fix, the operator must be able to act on it (#547/#579
// visible-state-plus-a-way-to-act). It also asserts the re-sent job is a real
// feedback_notify for the right recipient.
func TestFeedbackNotifyRetryResendsAndClearsTheMarker(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env
	long := longFeedbackReply()

	e.h.EmailQueue = deadQueue(t)
	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		fmt.Sprintf(`{"body":%q}`, long))
	if rec.Code != http.StatusCreated {
		t.Fatalf("failing reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, f.thread); at == nil {
		t.Fatal("precondition failed: the failing reply recorded no marker")
	}

	// Non-admin callers cannot re-send: the marker is platform state and the
	// action is on the platform's side of the conversation.
	rec = e.do(t, f.tokenUser, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/notify", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin retry = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// The queue recovers, and the operator re-sends.
	q, rdb := liveQueue(t)
	e.h.EmailQueue = q
	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin retry = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if _, notification := decodeNotifyResponse(t, rec.Body.Bytes()); notification != "queued" {
		t.Errorf("retry notification = %q, want \"queued\" (body %s)", notification, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, f.thread); at != nil {
		t.Error("the retry succeeded but the failure marker is still on the thread")
	}

	// The re-sent job is a real feedback_notify job, addressed to the FILER.
	job := notifyQueueJob(t, q, rdb)
	if job.Type != feedbackNotifyJobType {
		t.Errorf("enqueued job type = %q, want %q", job.Type, feedbackNotifyJobType)
	}
	var filerEmail string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT u.email FROM feedback_threads t JOIN users u ON u.id = t.user_id WHERE t.id = $1`,
		f.thread).Scan(&filerEmail); err != nil {
		t.Fatalf("read the filer address: %v", err)
	}
	if !strings.EqualFold(job.To, filerEmail) {
		t.Errorf("the retry was addressed to %q, want the filer %q", job.To, filerEmail)
	}
	// …AND IT CARRIES THE REPLY. Without this the test passes against a retry
	// that mails the filer their own thread title instead of the answer they
	// missed — which is exactly what the notify-kind/author-kind mix-up did.
	// This is the mirror of TestFeedbackNotifyRetryOfAnAdminAlertGoesToTheTeam,
	// which asserts the filing text on the admin path.
	if !strings.Contains(job.Body, long) {
		t.Errorf("the retried filer mail does not carry the admin reply — the filer is told there is "+
			"an answer without being given one\nbody:\n%s", job.Body)
	}

	// A thread with nothing to retry answers "none" rather than re-sending
	// mail that was never lost.
	healthy := f.fileThread(t, "Sihat", "thread tanpa sebarang kegagalan penghantaran")
	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+healthy+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retry on a healthy thread = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if _, notification := decodeNotifyResponse(t, rec.Body.Bytes()); notification != "none" {
		t.Errorf("retry on a thread with no recorded failure = %q, want \"none\" — re-sending mail "+
			"that was never lost is how an operator double-mails a filer", notification)
	}
}

// longFeedbackReply is a reply comfortably past the old 400-rune bound, with
// multi-byte characters past the cut point so a rune-boundary bug is covered too.
func longFeedbackReply() string {
	var b strings.Builder
	for i := 0; b.Len() < 900; i++ {
		fmt.Fprintf(&b, "Ayat %d menyatakan keadaan sebenar isu ini dengan cukup panjang. ", i)
	}
	b.WriteString("Ini ayat terakhir dengan aksara é dan 中 supaya pemotongan pada sempadan rune terbukti. " +
		"Sekian, terima kasih.")
	return b.String()
}

// TestFeedbackNotifyEmailCarriesTheFullReply is the regression for defect e.
//
// It asserts on the ACTUAL enqueued job body — rendered by the real handler, not
// by a helper the test itself wrote — so the reader sees the email that would be
// sent: the whole reply is present, the last sentence is present, and there is no
// orphan "…". The rendered email is logged so the result is SEEN, not inferred.
func TestFeedbackNotifyEmailCarriesTheFullReply(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env
	q, rdb := liveQueue(t)
	e.h.EmailQueue = q

	reply := longFeedbackReply()
	if len([]rune(reply)) <= 400 {
		t.Fatalf("fixture is not long enough to exercise the old bound: %d runes", len([]rune(reply)))
	}

	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		fmt.Sprintf(`{"body":%q}`, reply))
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	job := notifyQueueJob(t, q, rdb)

	t.Logf("rendered filer notification email:\n--- subject ---\n%s\n--- body ---\n%s\n--- end ---",
		job.Subject, job.Body)

	if !strings.Contains(job.Body, reply) {
		t.Errorf("the notification email does not carry the full reply — it is truncated. "+
			"reply runes=%d, body runes=%d\nbody:\n%s",
			len([]rune(reply)), len([]rune(job.Body)), job.Body)
	}
	if strings.Contains(job.Body, "…") {
		t.Errorf("the notification email carries an ellipsis with no explanation — the reader is "+
			"told nothing about why the message ended\nbody:\n%s", job.Body)
	}
	// The tail is the sharpest single assertion: a cut anywhere before the last
	// sentence loses it, and the tail is where the old bound always cut.
	tail := "Sekian, terima kasih."
	if !strings.Contains(job.Body, tail) {
		t.Errorf("the last sentence of the reply is missing from the email — the old 400-rune cut "+
			"landed before it\nbody:\n%s", job.Body)
	}
}

// TestFeedbackQuotedTextKeepsEverything is the unit-level complement to the
// rendered test above: nothing is dropped without a note, and a multi-byte reply
// is not split mid-character.
func TestFeedbackQuotedTextKeepsEverything(t *testing.T) {
	short := "  Balasan ringkas.  "
	if got := feedbackQuotedText(short); got != "Balasan ringkas." {
		t.Errorf("feedbackQuotedText(%q) = %q, want the trimmed text", short, got)
	}

	// 1000 three-byte characters: a byte-length comparison would cut this at
	// ~133 characters (the old bug's cousin), a rune-aware one keeps it all.
	long := strings.Repeat("中", 1000)
	got := feedbackQuotedText(long)
	if got != long {
		t.Errorf("feedbackQuotedText dropped text from a %d-rune multi-byte message (got %d runes)",
			len([]rune(long)), len([]rune(got)))
	}
	if strings.Contains(got, "…") {
		t.Error("feedbackQuotedText appended an ellipsis for a message it also returned in full")
	}
}

// TestFeedbackNotifyFailureOnMissingAddressIsRecorded covers the other way a
// notification dies before it ever reaches the queue: the filer's address is gone
// (a deleted/blanked address). Before the fix that path returned in silence on
// the same line as a successful send.
func TestFeedbackNotifyFailureOnMissingAddressIsRecorded(t *testing.T) {
	logs := feedbackNotifyLogs(t)
	f := newNotifyFixture(t)
	e := f.env
	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	logs()

	// Blank the filer's address: the notification now has nowhere to go.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE users SET email = '' WHERE id = (SELECT user_id FROM feedback_threads WHERE id = $1)`,
		f.thread); err != nil {
		t.Fatalf("blank the filer address: %v", err)
	}

	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		`{"body":"Balasan kepada pemfail yang alamatnya sudah tiada"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("reply with a missing filer address = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if _, notification := decodeNotifyResponse(t, rec.Body.Bytes()); notification != "failed" {
		t.Errorf("notification = %q, want \"failed\" — the send never happened, so the admin must not "+
			"be told the filer was notified (body %s)", notification, rec.Body.String())
	}
	at, kind, _ := readNotifyMarker(t, e, f.thread)
	if at == nil || kind != dbstore.FeedbackNotifyKindFiler {
		t.Errorf("missing-address failure marker = (%v, %q), want a time and kind %q",
			at, kind, dbstore.FeedbackNotifyKindFiler)
	}
	if !strings.Contains(logs(), "feedback_notify_failed") {
		t.Errorf("the missing-address failure was not logged; log:\n%s", logs())
	}
}

// TestFeedbackNotifyAdminFilingAlertIsMarkedWhenTheQueueRefuses covers the OTHER
// kind: the platform team not being told a new thread was filed. It is the same
// class of silence, so it must leave the same trace.
func TestFeedbackNotifyAdminFilingAlertIsMarkedWhenTheQueueRefuses(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env
	// A filing whose alert REACHED the queue, to prove the marker is not simply
	// firing for every thread.
	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	third := f.fileThread(t, "Isu kedua", "isu kedua yang perlu dimaklumkan kepada pasukan JomForm")
	if at, _, _ := readNotifyMarker(t, e, third); at != nil {
		t.Fatal("precondition failed: a filing with a live queue recorded a failure marker")
	}

	e.h.EmailQueue = deadQueue(t)
	thread := f.fileThread(t, "Isu ketiga", "isu ketiga yang gagal dimaklumkan kepada pasukan JomForm")

	at, kind, _ := readNotifyMarker(t, e, thread)
	if at == nil {
		t.Fatal("the admin alert failing to queue left no marker — the platform team was never told " +
			"about this thread and no surface says so")
	}
	if kind != dbstore.FeedbackNotifyKindAdmin {
		t.Errorf("notify_failed_kind = %q, want %q (the ADMIN alert failed, not the filer's)",
			kind, dbstore.FeedbackNotifyKindAdmin)
	}
}

// TestFeedbackNotifyNoRecipientsIsNotAFailure pins the deliberate distinction the
// card asks for: a no-op because nobody is configured to be told must NOT be
// recorded as a delivery failure. A marker that fires on a configuration choice
// is a marker an operator learns to ignore.
func TestFeedbackNotifyNoRecipientsIsNotAFailure(t *testing.T) {
	logs := feedbackNotifyLogs(t)
	e := newFeedbackEnv(t)
	_, _, tokenUser := e.newFeedbackUser(t)
	e.h.SuperAdminEmails = nil // nobody to notify
	e.h.EmailQueue = deadQueue(t)
	logs()

	thread := e.fileFeedback(t, tokenUser, "Tanpa penerima",
		"thread yang tiada penerima untuk dimaklumkan")

	if at, _, _ := readNotifyMarker(t, e, thread); at != nil {
		t.Error("a filing with NO configured recipients recorded a delivery failure — nothing was " +
			"attempted, so nothing failed")
	}
	if !strings.Contains(logs(), "feedback_notify_skipped") {
		t.Errorf("the deliberate skip was not logged, so it is indistinguishable from silence; log:\n%s",
			logs())
	}
}

// TestFeedbackNotifyRetryOfAnAdminAlertGoesToTheTeam covers the retry of the
// OTHER kind: when the platform team missed a filing, the retry re-sends the
// FILING to the admins — not an admin reply to the filer. Getting this backwards
// would mail the wrong person, so it is asserted on the enqueued job.
func TestFeedbackNotifyRetryOfAnAdminAlertGoesToTheTeam(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env

	e.h.EmailQueue = deadQueue(t)
	thread := f.fileThread(t, "Gagal dimaklumkan", "isu yang tidak sempat dimaklumkan kepada pasukan")
	if at, _, _ := readNotifyMarker(t, e, thread); at == nil {
		t.Fatal("precondition failed: the failing admin alert recorded no marker")
	}

	q, rdb := liveQueue(t)
	e.h.EmailQueue = q
	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+thread+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	job := notifyQueueJob(t, q, rdb)
	if !strings.EqualFold(job.To, f.adminEmail) {
		t.Errorf("the retried admin alert went to %q, want the platform team %q — a retry must not "+
			"mail the filer an admin reply they never received", job.To, f.adminEmail)
	}
	if at, _, _ := readNotifyMarker(t, e, thread); at != nil {
		t.Error("the retry succeeded but the failure marker is still set")
	}
}

// TestFeedbackNotifyFilerSuccessDoesNotEraseAnAdminFailure is defect 1: the two
// notification lanes are INDEPENDENT, and a success on one must not erase the
// other's failure.
//
// The admin lane fails when a thread is FILED (the platform team was never told
// about it); the filer lane fails when an admin REPLIES (the filer was never told
// about the answer). ClearNotifyFailure used to key its UPDATE on the thread
// alone, so the moment ANY later notification went through the marker vanished —
// including the admin one. The operator's console showed no badge and no banner,
// and the platform team's missed filing was gone with no trace anywhere: the
// marker is the only record that it ever happened.
//
// The four steps below are the reproduction from the card, in order. Step 3 is
// the part that makes it a data-loss bug rather than a cosmetic one: with the
// marker erased, the retry answers "none" (nothing to re-send), so the missed
// filing is not merely invisible — it becomes unrecoverable through the UI.
func TestFeedbackNotifyFilerSuccessDoesNotEraseAnAdminFailure(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env

	// ── 1. the queue is dead, a thread is filed: the ADMIN alert fails ──
	e.h.EmailQueue = deadQueue(t)
	thread := f.fileThread(t, "Gagal dimaklumkan", "isu yang tidak sempat dimaklumkan kepada pasukan")
	at, kind, _ := readNotifyMarker(t, e, thread)
	if at == nil {
		t.Fatal("precondition failed: a filing with a dead queue recorded no marker for the admin alert")
	}
	if kind != dbstore.FeedbackNotifyKindAdmin {
		t.Fatalf("precondition failed: notify_failed_kind = %q, want %q", kind, dbstore.FeedbackNotifyKindAdmin)
	}

	// ── 2. the queue recovers; an admin replies; the FILER lane succeeds ──
	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+thread+"/messages",
		`{"body":"Balasan yang sampai kepada pemfail selepas baris gilir pulih"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if _, notification := decodeNotifyResponse(t, rec.Body.Bytes()); notification != "queued" {
		t.Fatalf("the filer notification on a live queue = %q, want \"queued\" — the precondition is that "+
			"THIS lane now succeeds (body %s)", notification, rec.Body.String())
	}

	at, kind, _ = readNotifyMarker(t, e, thread)
	if at == nil {
		t.Fatal("the success of the FILER notification erased the ADMIN notification's failure marker — " +
			"the platform team was never told this thread exists and no surface says so any more")
	}
	if kind != dbstore.FeedbackNotifyKindAdmin {
		t.Errorf("notify_failed_kind = %q after the filer lane succeeded, want it still %q",
			kind, dbstore.FeedbackNotifyKindAdmin)
	}

	// ── 3. the retry must still see something to re-send ──
	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+thread+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	_, notification := decodeNotifyResponse(t, rec.Body.Bytes())
	if notification == "none" {
		t.Fatalf("the retry answered \"none\" — with the marker erased there is nothing to re-send, so the " +
			"missed filing is not merely invisible, it is UNRECOVERABLE through the console")
	}
	if notification != "queued" {
		t.Errorf("retry notification = %q, want \"queued\" (the queue is live and the admin alert is the "+
			"one being re-sent)", notification)
	}

	// ── 4. the marker is now cleared, by the retry that delivered it ──
	if at, _, _ := readNotifyMarker(t, e, thread); at != nil {
		t.Error("the admin retry delivered but the admin marker is still set")
	}
}

// TestFeedbackNotifySameKindSuccessStillClearsItsOwnMarker is the control that
// keeps the kind-scoped clear honest: clearing must still work for the marker of
// the kind that succeeded, or the fix would have traded a lost marker for a
// permanent one. The filer lane fails, then succeeds, and the FILER marker goes.
func TestFeedbackNotifySameKindSuccessStillClearsItsOwnMarker(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env

	e.h.EmailQueue = deadQueue(t)
	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/messages",
		`{"body":"Balasan pertama yang gagal dihantar kepada pemfail"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failing reply = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if _, kind, _ := readNotifyMarker(t, e, f.thread); kind != dbstore.FeedbackNotifyKindFiler {
		t.Fatalf("precondition failed: notify_failed_kind = %q, want %q", kind, dbstore.FeedbackNotifyKindFiler)
	}

	q, _ := liveQueue(t)
	e.h.EmailQueue = q
	rec = e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+f.thread+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if at, _, _ := readNotifyMarker(t, e, f.thread); at != nil {
		t.Error("the retry of the FILER notification succeeded but its own marker is still set — the " +
			"thread keeps claiming the filer was not told after they were")
	}
}

// TestFeedbackNotifyAdminRetryWithNoRecipientsIsNotQueued covers the admin lane's
// no-op exit on the retry path.
//
// The admin lane has a deliberate no-op exit: with an empty super-admin allowlist
// there is nobody to alert, which is a CONFIGURATION choice and not a delivery
// failure (TestFeedbackNotifyNoRecipientsIsNotAFailure pins that, so the filing
// path writes no marker). The RETRY path has no such luxury — the operator is
// looking at a marker and pressed "send again", so the response must say what
// actually happened. The retry used to infer its answer by re-reading the marker
// instead of asking the attempt what it did, and a skip writes no marker.
//
// MEASURED, NOT ASSUMED. On 2eb6e4d this sequence answers `failed` with 0 jobs
// enqueued and the marker left in place — not `queued` as the card reported (the
// marker is never cleared by the skip, so the re-read finds it set and reports a
// failure). `failed` is the conservative wrong answer rather than a false
// positive, but it is wrong in a way the operator feels: the console renders "We
// couldn't re-send the notification", and because the marker survives, the banner
// and the retry button stay and the same error repeats on every press. The honest
// answer is `skipped`: nothing was attempted, and a delivery value would be a
// claim no job backs.
//
// The delivery values are checked against the QUEUE, so "queued" can only ever
// mean a job left the process.
func TestFeedbackNotifyAdminRetryWithNoRecipientsIsNotQueued(t *testing.T) {
	f := newNotifyFixture(t)
	e := f.env

	// The filing alert fails, so there is a marker to retry.
	e.h.EmailQueue = deadQueue(t)
	thread := f.fileThread(t, "Penerima hilang", "isu yang ditandakan gagal sebelum senarai dikosongkan")
	_, kind, _ := readNotifyMarker(t, e, thread)
	if kind != dbstore.FeedbackNotifyKindAdmin {
		t.Fatalf("precondition failed: notify_failed_kind = %q, want %q", kind, dbstore.FeedbackNotifyKindAdmin)
	}

	// The queue is healthy again, but the allowlist is now empty: there is nobody
	// to notify, so nothing is attempted.
	q, rdb := liveQueue(t)
	e.h.EmailQueue = q
	e.h.SuperAdminEmails = nil

	rec := e.do(t, f.tokenAdmin, http.MethodPost, "/v1/admin/feedback/"+thread+"/notify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	_, notification := decodeNotifyResponse(t, rec.Body.Bytes())

	enqueued, err := rdb.LLen(context.Background(), q.ReadyKey()).Result()
	if err != nil {
		t.Fatalf("read the queue length: %v", err)
	}
	if enqueued != 0 {
		t.Fatalf("fixture is wrong: %d job(s) on the queue, but nothing should have been enqueued", enqueued)
	}
	if notification == "queued" || notification == "sent" {
		t.Errorf("retry notification = %q with %d jobs enqueued — the operator is told mail went out when "+
			"nothing left the process and will never arrive", notification, enqueued)
	}
	if notification != "skipped" {
		t.Errorf("retry notification = %q with %d jobs enqueued, want \"skipped\" — a deliberately "+
			"unattempted notification must say so; reporting a failure for a configuration state sends "+
			"the operator chasing a delivery problem that does not exist", notification, enqueued)
	}

	// And the marker is untouched by the skip: it is still the record of the ORIGINAL
	// failure, which is what the operator must be able to read and retry.
	at, kind2, _ := readNotifyMarker(t, e, thread)
	if at == nil {
		t.Error("the skipped retry cleared the failure marker — the thread now looks healthy while " +
			"the platform team still has not been told")
	}
	if kind2 != dbstore.FeedbackNotifyKindAdmin {
		t.Errorf("notify_failed_kind = %q after a skipped retry, want %q", kind2, dbstore.FeedbackNotifyKindAdmin)
	}
}
