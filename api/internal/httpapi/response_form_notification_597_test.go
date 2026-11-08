package httpapi

// Issue #597: a RESPONSE-mode form's default notification must be one that can
// actually fire, and the email it sends must make sense for a response.
//
// THE BUG. EnsureDefault pre-created the default rule for EVERY form with the
// event hardcoded to `sale.paid` and a sale-flavoured body. A response form (an
// RSVP, an event registration, a contact form) records a `submissions` row and
// never records a sale, so `sale.paid` can never fire on it: the merchant was
// left with a rule that existed, was enabled, and silently did nothing. Prod
// confirmed it — every form_notifications row in the database was `sale.paid`,
// including all 11 response-mode forms, and the reporting form
// (`event-registration`) had exactly that dead rule.
//
// WHAT THIS TEST DRIVES. The real REST create route, then the real public submit
// route, then the real notification engine — no hand-built fixtures standing in
// for the code under test:
//
//  1. POST /v1/rest/forms with a response-mode config, and with a payment-mode
//     config, through the actual handler. The response form's default rule must
//     be `response.submitted`; the payment form's must STILL be `sale.paid`
//     (the regression case — the naive fix breaks every sales form).
//  2. POST the response form's submit route. The rendered notification must go
//     out to the merchant, and must contain the responder's real values with no
//     sale placeholder left in it.
//
// THE BITE. Revert EnsureDefault to the hardcoded 'sale.paid' and the first two
// assertions go RED and name the response form. The submit half goes red with
// them: the rule it looks for is the one the broken code never creates, so no
// email is enqueued at all. A test that passed before and after would prove
// nothing; this one fails on the pre-fix code.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

// respNotifQueue returns a queue whose key set belongs to this test process
// (testdb.Valkey). It used to FlushDB a fixed DB number (7), which a live
// process, a second suite or another package configured on that number shares —
// and the flush deleted their keys. Owning the key NAMES is the fix; see
// testdb/valkey.go.
func respNotifQueue(t *testing.T) (*emailqueue.Queue, *goredis.Client) {
	t.Helper()
	rdb, prefix := testdb.Valkey(t)
	return emailqueue.NewQueueWithPrefix(rdb, prefix), rdb
}

// queuedFormNotifications reads the ready jobs without claiming them. The key
// comes from the queue (Dequeue must not be used: it MOVEs a job into the
// in-flight list and blocks for its dequeue timeout), so it matches the prefix
// this process writes under.
func queuedFormNotifications(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client) []emailqueue.Job {
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
		out = append(out, j)
	}
	return out
}

func TestResponseFormDefaultNotificationFires(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping response notification test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, ownerUserID := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)
	q, rdb := respNotifQueue(t)

	store := dbstore.NewFormNotificationStore(pool)
	forms := dbstore.NewFormStore(pool)

	// A workspace is what gives the response form a public subdomain to submit
	// to, and gives the notification engine the merchant's account.
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

	// ── the RESPONSE form, created through the REAL route ───────────────────
	h := newBundleHandlers(pool)
	h.Notifications = store
	respSlug := "it-resp-" + acc[:8]
	respBody, _ := json.Marshal(map[string]any{
		"slug":  respSlug,
		"title": "Event Registration",
		"config": map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
	})
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms", string(respBody))
	if code != http.StatusCreated {
		t.Fatalf("create response form status = %d, body=%s", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created form: %v (%s)", err, raw)
	}
	// A new form is a draft, and the public submit route refuses a draft. Publish
	// it through the store so the submit half below drives the real route.
	if err := forms.SetStatus(ctx, acc, created.ID, dbstore.FormStatusPublished); err != nil {
		t.Fatalf("publish response form: %v", err)
	}

	// ── the PAYMENT form, same route — the regression control ───────────────
	paySlug := "it-pay-" + acc[:8]
	payBody, _ := json.Marshal(map[string]any{
		"slug":   paySlug,
		"title":  "Order Form",
		"config": map[string]any{"mode": "payment", "blocks": []any{}},
	})
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms", string(payBody))
	if code != http.StatusCreated {
		t.Fatalf("create payment form status = %d, body=%s", code, raw)
	}
	var createdPay struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &createdPay); err != nil {
		t.Fatalf("decode created payment form: %v (%s)", err, raw)
	}

	// ── 1. the default rule follows the form's mode ─────────────────────────
	respRules, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list response-form rules: %v", err)
	}
	if len(respRules) != 1 {
		t.Fatalf("response form %s got %d default rules, want exactly 1 (rules=%+v)",
			respSlug, len(respRules), respRules)
	}
	if respRules[0].Event != dbstore.NotifyEventResponseSubmitted {
		t.Errorf("response form %s default rule event = %q, want %q — a sale.paid rule on a "+
			"response form can never fire, so the merchant is never told anyone responded",
			respSlug, respRules[0].Event, dbstore.NotifyEventResponseSubmitted)
	}
	if !respRules[0].Enabled {
		t.Errorf("response form %s default rule is disabled", respSlug)
	}
	if respRules[0].ToEmail != "{admin_email}" {
		t.Errorf("response form %s default rule recipient = %q, want {admin_email}",
			respSlug, respRules[0].ToEmail)
	}

	payRules, err := store.ListByForm(ctx, createdPay.ID)
	if err != nil {
		t.Fatalf("list payment-form rules: %v", err)
	}
	if len(payRules) != 1 {
		t.Fatalf("payment form %s got %d default rules, want exactly 1", paySlug, len(payRules))
	}
	if payRules[0].Event != dbstore.NotifyEventSalePaid {
		t.Errorf("REGRESSION: payment form %s default rule event = %q, want %q — every sales "+
			"form must keep its sale.paid notification",
			paySlug, payRules[0].Event, dbstore.NotifyEventSalePaid)
	}

	// The response default's message must not talk about a sale. A response has
	// no reference code, amount or items, so those placeholders would be mailed
	// as their own literal text (text/template refuses an undefined function and
	// renderNotification falls back to the raw source).
	for _, bad := range []string{"{{ref_code}}", "{{amount}}", "{{items}}", "{{buyer_name}}"} {
		if strings.Contains(respRules[0].Subject, bad) || strings.Contains(respRules[0].Body, bad) {
			t.Errorf("response default notification uses the sale placeholder %s, which has no "+
				"value on a response:\nsubject=%q\nbody=%q", bad, respRules[0].Subject, respRules[0].Body)
		}
	}

	// ── 2. a real submission must actually produce the email ────────────────
	pay := &payments.Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Notifications: store,
		EmailQueue:    q,
	}
	ph := &PublicHandlers{
		Forms:       forms,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		Pay:         pay,
		BaseURL:     "http://localhost:8080",
	}
	mux := http.NewServeMux()
	ph.RegisterRoutes(mux)
	submitBody := `{"client":{"full_name":"Siti Aminah","email":"siti@example.test"},
	                "fields":{"b1":"Siti Aminah","b2":"siti@example.test"}}`
	req := httptest.NewRequest(http.MethodPost, "/"+respSlug+"/order", strings.NewReader(submitBody))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	jobs := queuedFormNotifications(t, q, rdb)
	var sent []emailqueue.Job
	for _, j := range jobs {
		if j.Type == "form_notification" {
			sent = append(sent, j)
		}
	}
	if len(sent) != 1 {
		t.Fatalf("a response submission enqueued %d form_notification jobs, want exactly 1 "+
			"(all jobs: %+v) — no email means the merchant is never told anyone responded",
			len(sent), jobs)
	}
	// The recipient is the merchant. With the {admin_email} owner-resolution fix
	// (issue #606) the address is the owner user's email; assert it is not empty
	// and not the raw token, which is the failure that fix addressed.
	if sent[0].To == "" || sent[0].To == "{admin_email}" {
		t.Errorf("notification recipient = %q, want the merchant's resolved email", sent[0].To)
	}
	if !strings.Contains(sent[0].Body, "Siti Aminah") {
		t.Errorf("notification body does not name the responder:\n%s", sent[0].Body)
	}
	if !strings.Contains(sent[0].Body, "siti@example.test") {
		t.Errorf("notification body does not carry the responder's email:\n%s", sent[0].Body)
	}
	for _, bad := range []string{"{{", "}}"} {
		if strings.Contains(sent[0].Subject, bad) || strings.Contains(sent[0].Body, bad) {
			t.Errorf("the sent notification still contains an unrendered placeholder %q:\nsubject=%q\nbody=%q",
				bad, sent[0].Subject, sent[0].Body)
		}
	}
	if strings.Contains(sent[0].Body, "RM 0.00") || strings.Contains(sent[0].Subject, "Jualan") {
		t.Errorf("the sent notification is a sale message for a response:\nsubject=%q\nbody=%q",
			sent[0].Subject, sent[0].Body)
	}
	// A response form must never produce a sale.
	var subID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM submissions WHERE form_id=$1 LIMIT 1`,
		created.ID).Scan(&subID); err != nil {
		t.Fatalf("no submission recorded for the response form: %v", err)
	}
	var saleCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM sales WHERE form_id=$1`, created.ID).Scan(&saleCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if saleCount != 0 {
		t.Errorf("response form recorded %d sales, want 0", saleCount)
	}
}

// TestEnsureDefaultIsModeAwareAtTheStoreLevel pins the store-level rule directly,
// including the two shapes the REST payload cannot easily produce: a legacy form
// with NO mode key (a payment form — it must keep sale.paid) and a form whose
// merchant already has rules (EnsureDefault must be a no-op, so an opt-out is
// never reversed).
func TestEnsureDefaultIsModeAwareAtTheStoreLevel(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping EnsureDefault test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	forms := dbstore.NewFormStore(pool)
	store := dbstore.NewFormNotificationStore(pool)

	cases := []struct {
		name string
		slug string
		cfg  map[string]any
		want string
		why  string
	}{
		{"response form", "ed-resp-" + acc[:8], map[string]any{"mode": "response", "blocks": []any{}},
			dbstore.NotifyEventResponseSubmitted,
			"a response form records submissions, never a sale — sale.paid on it can never fire"},
		{"payment form", "ed-pay-" + acc[:8], map[string]any{"mode": "payment", "blocks": []any{}},
			dbstore.NotifyEventSalePaid, "an order form records sales and keeps sale.paid"},
		{"legacy form with no mode key", "ed-legacy-" + acc[:8], map[string]any{"blocks": []any{}},
			dbstore.NotifyEventSalePaid,
			"every pre-#171 form has no mode key, is a payment form, and has real sales — defaulting it to response would silence it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &dbstore.Form{
				AccountID: acc, Slug: tc.slug, Title: tc.name,
				Config: tc.cfg, Status: dbstore.FormStatusDraft,
			}
			if err := forms.Create(ctx, f); err != nil {
				t.Fatalf("create form: %v", err)
			}
			if err := store.EnsureDefault(ctx, f); err != nil {
				t.Fatalf("EnsureDefault: %v", err)
			}
			rules, err := store.ListByForm(ctx, f.ID)
			if err != nil {
				t.Fatalf("list rules: %v", err)
			}
			if len(rules) != 1 {
				t.Fatalf("got %d rules, want 1", len(rules))
			}
			if rules[0].Event != tc.want {
				t.Errorf("%s: event = %q, want %q — %s", tc.slug, rules[0].Event, tc.want, tc.why)
			}
			if !rules[0].IsDefault {
				t.Errorf("%s: the pre-created rule is not flagged is_default", tc.slug)
			}

			// A second call on a form that still has its rule must not add one.
			if err := store.EnsureDefault(ctx, f); err != nil {
				t.Fatalf("second EnsureDefault: %v", err)
			}
			again, _ := store.ListByForm(ctx, f.ID)
			if len(again) != 1 {
				t.Errorf("%s: EnsureDefault is not idempotent — %d rules after a second call", tc.slug, len(again))
			}
			// A merchant's own edit is never overwritten by a later call.
			if err := store.Update(ctx, f.ID, again[0].ID, &dbstore.FormNotification{
				Name: "Custom", Event: dbstore.NotifyEventSaleFailed, ToEmail: "ops@example.test",
				Subject: "Custom", Body: "Custom", Enabled: true,
			}); err != nil {
				t.Fatalf("update rule: %v", err)
			}
			if err := store.EnsureDefault(ctx, f); err != nil {
				t.Fatalf("EnsureDefault after an edit: %v", err)
			}
			edited, _ := store.ListByForm(ctx, f.ID)
			if len(edited) != 1 || edited[0].Subject != "Custom" {
				t.Errorf("%s: EnsureDefault overwrote or duplicated a merchant-edited rule: %+v", tc.slug, edited)
			}
			// NOTE (pre-existing, unchanged by this fix): deleting every rule and
			// calling EnsureDefault again DOES create one. That is the historical
			// behaviour of the guard — it is `WHERE NOT EXISTS` at call time, not a
			// recorded opt-out — and in production EnsureDefault is only reached on
			// form creation. It is recorded here rather than asserted as a contract,
			// because the code does not implement one and this change must not
			// quietly alter it.
		})
	}
}

// TestResponseFormDefaultRuleMatchesWhatTheStoreCreates pins the rule the test
// above asserts against the one definition in dbstore, so the fixture, the
// migration and EnsureDefault cannot drift into three copies of the same rule.
func TestResponseFormDefaultRuleMatchesWhatTheStoreCreates(t *testing.T) {
	name, event, storedSubject, storedBody := dbstore.DefaultNotification(dbstore.FormModeResponse)
	if event != dbstore.NotifyEventResponseSubmitted {
		t.Fatalf("response-mode default event = %q, want %q", event, dbstore.NotifyEventResponseSubmitted)
	}
	if name != "Admin Notification" {
		t.Errorf("response-mode default name = %q", name)
	}
	// The rule stores no text (card t_1768cb11): the event picks the dictionary
	// kind, and that kind is rendered in the form's language at send time.
	if storedSubject != "" || storedBody != "" {
		t.Errorf("the response default rule stores a template: subject=%q body=%q",
			storedSubject, storedBody)
	}
	response := emailtext.Text(emailtext.En, emailtext.NotificationResponse, nil)
	for _, bad := range []string{"{{ref_code}}", "{{amount}}", "{{items}}", "{{buyer_name}}"} {
		if strings.Contains(response.Subject, bad) || strings.Contains(response.Body, bad) {
			t.Errorf("the response default template uses %s, which has no value on a response:\n"+
				"subject=%q\nbody=%q", bad, response.Subject, response.Body)
		}
	}
	// The payment default must be untouched by this change: it still renders the
	// dictionary's sale template, placeholders included.
	_, payEvent, payStoredSubject, payStoredBody := dbstore.DefaultNotification(dbstore.FormModePayment)
	if payEvent != dbstore.NotifyEventSalePaid {
		t.Errorf("payment-mode default event = %q, want %q", payEvent, dbstore.NotifyEventSalePaid)
	}
	if payStoredSubject != "" || payStoredBody != "" {
		t.Errorf("the payment default rule stores a template: subject=%q body=%q",
			payStoredSubject, payStoredBody)
	}
	sale := emailtext.Text(emailtext.En, emailtext.NotificationSale, nil)
	if !strings.Contains(sale.Subject, "{{ref_code}}") || !strings.Contains(sale.Body, "RM {{amount}}") {
		t.Errorf("the payment default lost its sale placeholders:\nsubject=%q\nbody=%q",
			sale.Subject, sale.Body)
	}
}
