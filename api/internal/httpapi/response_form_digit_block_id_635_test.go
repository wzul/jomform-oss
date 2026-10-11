package httpapi

// Issue #635 END-TO-END REGRESSION: a published RESPONSE form whose block id is
// not a valid text/template identifier must accept a real public submission and
// notify the merchant — not 500.
//
// THE BUG. `renderNotification` (api/internal/payments/notifications.go)
// registered every submitted block id as a text/template FuncMap key, and
// text/template PANICS on a key that is not a valid identifier
// (`function name "4nwz0vg" is not a valid identifier`). Block ids come from the
// builder's `Math.random().toString(36).slice(2, 9)`, which is base36, so an id
// routinely leads with a digit. The panic escaped the notification engine into
// the HTTP recoverer, so production answered
// `POST https://wan.jomform.com/event-registration/order` with
// **500 {"error":"internal error"}** and enqueued no email.
//
// WHY THIS TEST AND NOT THE #597 ONES. Every fixture in
// response_form_notification_597_test.go uses `b1`/`b2` — valid identifiers — so
// the FuncMap loop never sees a bad name and the suite stayed green over a
// production P0. This test seeds the production shape (`4nwz0vg`, a real base36
// id from the incident) and drives the same path those tests drive.
//
// WHAT IT PROVES, and why each assertion is here:
//   - the submit returns 201 through the FULL middleware chain (recoverer
//     included), which is what production serves. Pre-fix this is a 500 — the
//     production symptom, not a test panic that proves nothing about the status
//     a responder would see.
//   - the recoverer never fired: no `panic_recovered` line in the captured log.
//     The status alone would still be 201 if a future fix swallowed the failure
//     later in the request, and "no panic happened" is the card's stated
//     requirement.
//   - the merchant's notification is actually ENQUEUED, with the rendered body
//     (no unrendered `{{` left in it). A fix that merely returned early to dodge
//     the panic would silence the form instead of fixing it.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.
import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue635Queue returns a queue whose key set belongs to this test process
// (testdb.Valkey), plus the client backing it.
//
// The keys are namespaced per process, which is the isolation this test needs —
// the earlier shape, a dedicated DB number picked from a small pid-derived set,
// was only belt to that: a shared keyspace still let a peer built from the
// PRE-FIX fixture FlushDB it and delete the job this test was about to count,
// reporting "enqueued 0 form_notification jobs, want exactly 1" for a correct
// enqueue. Ownership of the key NAMES is what removes the failure, and it is
// what the account-scoped count below can no longer be undermined by.
//
// It deliberately does NOT FlushDB: deleting a key this run did not write is
// what made the old fixture destructive.
func issue635Queue(t *testing.T, accountID string) (*emailqueue.Queue, *goredis.Client) {
	t.Helper()
	rdb, prefix := testdb.Valkey(t)
	q := emailqueue.NewQueueWithPrefix(rdb, prefix)
	t.Cleanup(func() { issue635RemoveOwn(t, q, rdb, accountID) })
	return q, rdb
}

// issue635FormNotifications returns the form_notification jobs belonging to
// accountID — i.e. this run's own job, at most one.
//
// The ready list is this process's own (testdb.Valkey namespaces the keys), and
// the count is also scoped to the account this run minted, so even a job from
// another fixture in this same process is counted out rather than counted in.
func issue635FormNotifications(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client, accountID string) []emailqueue.Job {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), q.ReadyKey(), 0, -1).Result()
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	var out []emailqueue.Job
	for _, s := range raw {
		var j emailqueue.Job
		if err := json.Unmarshal([]byte(s), &j); err != nil {
			t.Fatalf("decode job %q: %v", s, err)
		}
		if j.Type == "form_notification" && j.AccountID == accountID {
			out = append(out, j)
		}
	}
	return out
}

// captureSlog routes the global slog sink into a buffer for one test and
// returns a reader for it. The HTTP recoverer logs `panic_recovered` there, so
// reading this buffer is how "no panic occurred" is observed rather than
// inferred from a status code.
func captureSlog(t *testing.T) func() string {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf.String
}

// issue635RemoveOwn deletes this run's own jobs from the ready list, matching
// the exact blob read so another fixture's entries are never touched.
func issue635RemoveOwn(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client, accountID string) {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), q.ReadyKey(), 0, -1).Result()
	if err != nil {
		t.Errorf("cleanup: read queue: %v", err)
		return
	}
	for _, s := range raw {
		var j emailqueue.Job
		if err := json.Unmarshal([]byte(s), &j); err != nil {
			continue
		}
		if j.Type == "form_notification" && j.AccountID == accountID {
			if err := rdb.LRem(context.Background(), q.ReadyKey(), 1, s).Err(); err != nil {
				t.Errorf("cleanup: remove own job: %v", err)
			}
		}
	}
}

// TestResponseFormWithDigitLeadingBlockIDSubmits201AndNotifies is the
// regression for GH #635.
func TestResponseFormWithDigitLeadingBlockIDSubmits201AndNotifies(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping the digit-leading block id regression")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, ownerUserID := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	queue, rdb := issue635Queue(t, acc)

	store := dbstore.NewFormNotificationStore(pool)
	forms := dbstore.NewFormStore(pool)

	// A workspace gives the form a public subdomain to submit to, and gives the
	// notification engine the account -> owner -> {admin_email} chain.
	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, ownerUserID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET workspace_id = (SELECT id FROM workspaces WHERE slug=$2) WHERE id=$1`,
		acc, workspaceSlug); err != nil {
		t.Fatalf("link account to workspace: %v", err)
	}

	// ── the form, created through the REAL REST route ───────────────────────
	// `4nwz0vg` is the id from the production config. `9e25ae7` is the second
	// repro id from the issue. `b1` is a valid identifier submitted in the same
	// request, so the test also pins that the fix skips only what it must.
	h := newBundleHandlers(pool)
	h.Notifications = store
	slug := "it-635-" + acc[:8]
	body, _ := json.Marshal(map[string]any{
		"slug":  slug,
		"title": "Event Registration",
		"config": map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "4nwz0vg", "type": "textarea", "value": "Catatan",
					"placeholder": "Any dietary notes?"},
				map[string]any{"id": "9e25ae7", "type": "text"},
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
	})
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms", string(body))
	if code != http.StatusCreated {
		t.Fatalf("create response form status = %d, body=%s", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created form: %v (%s)", err, raw)
	}
	// A new form is a draft and the public route refuses a draft; publish it so
	// the submit below drives the real handler.
	if err := forms.SetStatus(ctx, acc, created.ID, dbstore.FormStatusPublished); err != nil {
		t.Fatalf("publish response form: %v", err)
	}

	// The default rule the REST route just created must be the response one,
	// otherwise this test would be pinning a rule that can never fire.
	rules, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != 1 || rules[0].Event != dbstore.NotifyEventResponseSubmitted || !rules[0].Enabled {
		t.Fatalf("the form's default rule is not an enabled response.submitted rule: %+v", rules)
	}

	// ── the public submit, through the FULL production chain ────────────────
	pay := &payments.Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Notifications: store,
		EmailQueue:    queue,
	}
	pub := &PublicHandlers{
		Forms:       forms,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		Pay:         pay,
		BaseURL:     "http://localhost:8080",
	}
	// NewServer, not a bare mux: the recoverer is part of what production
	// serves, so the pre-fix panic must surface here as the responder-visible
	// 500 exactly as it did on prod — not as a test-binary crash.
	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{REST: h, Public: pub})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Capture the global slog sink: the recoverer's `panic_recovered` is logged
	// there, so an empty result is a direct observation that no panic fired.
	logs := captureSlog(t)

	submit := `{"client":{"full_name":"Siti Aminah","email":"siti@example.test"},
	            "fields":{"4nwz0vg":"Nasi lemak, tanpa sambal","9e25ae7":"Meja 8",
	                      "b1":"Siti Aminah","b2":"siti@example.test"}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ts.URL+"/"+slug+"/order", strings.NewReader(submit))
	if err != nil {
		t.Fatalf("build submit request: %v", err)
	}
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	// No client-address header: this request goes over TCP to an httptest server
	// (peer 127.0.0.1) and middleware.RealIP is not in that chain, so the key is
	// the peer. See setTestClientAddr.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /%s/order: %v", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /%s/order = %d, want 201 — a digit-leading block id (4nwz0vg) must "+
			"not break a form submission (GH #635 was a 500 here: renderNotification "+
			"handed text/template a FuncMap key it refuses, and the panic reached the "+
			"recoverer). engine log:\n%s", slug, resp.StatusCode, logs())
	}
	if got := logs(); strings.Contains(got, "panic_recovered") {
		t.Errorf("the request was served, but the recoverer still fired — a panic happened "+
			"somewhere in the submit path:\n%s", got)
	}

	// ── the notification must really have been produced ─────────────────────
	// Counted by THIS run's account id, and the captured engine log is included
	// in the failure so the reason is visible: the notification path logs its
	// skip/failure cases (notify_no_recipient, notify_enqueue_failed,
	// notify_list_failed) to the sink captureSlog is holding, and this assertion
	// used to discard them exactly when they mattered.
	sent := issue635FormNotifications(t, queue, rdb, acc)
	if len(sent) != 1 {
		t.Fatalf("a submission with a digit-leading block id enqueued %d form_notification "+
			"jobs, want exactly 1 — a fix that dodges the panic by skipping the send "+
			"silences the merchant instead of fixing the form. engine log:\n%s",
			len(sent), logs())
	}
	if sent[0].To == "" || sent[0].To == "{admin_email}" {
		t.Errorf("notification recipient = %q, want the merchant's resolved email", sent[0].To)
	}
	for _, bad := range []string{"{{", "}}"} {
		if strings.Contains(sent[0].Subject, bad) || strings.Contains(sent[0].Body, bad) {
			t.Errorf("the sent notification still carries an unrendered placeholder %q — the "+
				"renderer fell back to the raw template:\nsubject=%q\nbody=%q",
				bad, sent[0].Subject, sent[0].Body)
		}
	}
	if !strings.Contains(sent[0].Body, "Siti Aminah") {
		t.Errorf("the notification body does not name the responder:\n%s", sent[0].Body)
	}

	// The submission itself must be stored, with the digit-leading block ids in
	// its data — that is the payload that reached the engine.
	var stored []byte
	if err := pool.QueryRow(ctx,
		`SELECT data FROM submissions WHERE form_id=$1 LIMIT 1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("read submission: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(stored, &data); err != nil {
		t.Fatalf("decode submission data: %v", err)
	}
	if data["4nwz0vg"] != "Nasi lemak, tanpa sambal" {
		t.Errorf("submission data = %v, want the 4nwz0vg answer recorded", data)
	}
	if _, ok := data["9e25ae7"]; !ok {
		t.Errorf("submission data = %v, want the second digit-leading block id recorded", data)
	}
}
