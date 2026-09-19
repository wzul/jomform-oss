package httpapi

// Issue #606, end-to-end: the DEFAULT notification rule that the real REST form
// creation path installs (`POST /v1/rest/forms` -> EnsureDefault) must actually
// DELIVER on sale.paid.
//
// The unit-level regression lives in the payments package
// (notifications_admin_email_issue606_test.go). What this file adds is the part
// that cannot be proven there: the rule is created by the production handler, not
// by a hand call to EnsureDefault, and the recipient is resolved by the event the
// payment callback actually fires (ProcessCallbackTx -> fireSaleNotifications),
// with the NotificationRef the engine builds itself.
//
// Pre-fix, resolveAdminEmail passed the ACCOUNT id to Users.ByID — and
// users.id != accounts.id (the FK is accounts.owner_user_id), so the lookup
// always missed and this test failed with "0 jobs enqueued".
//
// Integration against the real test Postgres + Valkey; skipped when either is unset.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
)

// issue606HTTPKeyspace is a dedicated Valkey DB so FlushDB here cannot wipe a
// keyspace another package owns (emailqueue /3, the email breaker /2, the shared
// test env /1, the payments-package notifications test /11).
const issue606HTTPKeyspace = 12

// issue606HTTPQueue returns a queue on that keyspace, flushed per test.
func issue606HTTPQueue(t *testing.T) (*emailqueue.Queue, *goredis.Client) {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping notification integration test")
	}
	u := strings.TrimPrefix(valkeyURL, "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	opts, err := goredis.ParseURL(fmt.Sprintf("redis://%s/%d", u, issue606HTTPKeyspace))
	if err != nil {
		t.Fatalf("parse valkey url: %v", err)
	}
	rdb := goredis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if ferr := rdb.FlushDB(context.Background()).Err(); ferr != nil {
		t.Fatalf("flush test keyspace: %v", ferr)
	}
	return emailqueue.NewQueue(rdb), rdb
}

// issue606HTTPJobs reads the ready list without claiming it.
func issue606HTTPJobs(t *testing.T, rdb *goredis.Client) []emailqueue.Job {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), "jf:emailq:jobs", 0, -1).Result()
	if err != nil {
		t.Fatalf("read email queue: %v", err)
	}
	out := make([]emailqueue.Job, 0, len(raw))
	for _, r := range raw {
		var j emailqueue.Job
		if err := json.Unmarshal([]byte(r), &j); err != nil {
			t.Fatalf("unmarshal queued job %q: %v", r, err)
		}
		out = append(out, j)
	}
	return out
}

// issue606HTTPFixture seeds an owner + workspace + account linked to it, and
// returns the REST handlers, an API key, the ids and the owner email.
func issue606HTTPFixture(t *testing.T) (h *RESTHandlers, rawKey string, pool *pgxpool.Pool, ownerEmail, userID, workspaceID, accountID string) {
	t.Helper()
	ctx := context.Background()
	pool = bundleTestPool(t)

	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-owner-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id, email`).Scan(&userID, &ownerEmail); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// The RESTHandlers the production binary builds from main.go carry the
	// notification store; the shared bundle helper does not, so wire it here —
	// the create route's EnsureDefault call is skipped when it is nil.
	h = newBundleHandlers(pool)
	h.Notifications = dbstore.NewFormNotificationStore(pool)
	return h, bundleAPIKey(t, ctx, pool, accountID), pool,
		ownerEmail, userID, workspaceID, accountID
}

// TestIssue606DefaultRuleDeliversOnSalePaidForARESTCreatedForm is the card's
// acceptance criterion: a form created through the real REST path carries the
// default `sale.paid` -> `{admin_email}` rule, and a paid callback for that form
// enqueues the email to the workspace OWNER's address.
func TestIssue606DefaultRuleDeliversOnSalePaidForARESTCreatedForm(t *testing.T) {
	ctx := context.Background()
	h, rawKey, pool, ownerEmail, _, workspaceID, accountID := issue606HTTPFixture(t)

	// ── 1. create the form through the real REST route ──────────────────────
	rec := issue501Do(t, h, rawKey, http.MethodPost, "/v1/rest/forms",
		`{"title":"Issue 606 order form","config":{"blocks":[]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	formID, _ := issue501JSON(t, rec)["id"].(string)
	if formID == "" {
		t.Fatalf("form create returned no id: %s", rec.Body.String())
	}

	// ── 2. the default rule the handler installed (not one we inserted) ─────
	notif := dbstore.NewFormNotificationStore(pool)
	rules, err := notif.ListByForm(ctx, formID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	var def *dbstore.FormNotification
	for _, r := range rules {
		if r.Event == dbstore.NotifyEventSalePaid && r.ToEmail == "{admin_email}" {
			def = r
		}
	}
	if def == nil {
		t.Fatalf("the REST create path installed no sale.paid {admin_email} rule; rules = %+v", rules)
	}
	if !def.Enabled {
		t.Fatalf("the default rule is not enabled: %+v", def)
	}

	// ── 3. wire the payments service the callback path uses ────────────────
	q, rdb := issue606HTTPQueue(t)
	svc := &payments.Service{
		Accounts:      dbstore.NewAccountStore(pool),
		Sales:         dbstore.NewSaleStore(pool),
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Users:         dbstore.NewUserStore(pool),
		Forms:         dbstore.NewFormStore(pool),
		Notifications: notif,
		EmailQueue:    q,
	}
	h.Pay = svc

	// ── 4. a pending sale for that form, with a CHIP purchase ──────────────
	sale := &dbstore.Sale{
		AccountID: accountID, FormID: formID, RefCode: "IT606-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Ali", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "", Name: "Tee", PriceSen: 1500, Qty: 1}},
		TotalSen: 1500, SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
		Status: dbstore.SalePending,
	}
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID := "pur_606_" + accountID[:8]
	if err := sales.AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/checkout/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}

	// ── 5. drive the real paid-callback transition ─────────────────────────
	// Exactly the production shape (purchases.go ProcessCallback): the state
	// change + the notification under the per-sale advisory lock.
	if err := sales.WithSaleLock(ctx, sale.ID, func(tx pgx.Tx) error {
		return svc.ProcessCallbackTx(ctx, tx, &chip.Purchase{ID: purchaseID, Status: chip.StatusPaid})
	}); err != nil {
		t.Fatalf("paid callback: %v", err)
	}

	// ── 6. the merchant's new-sale email is queued, to the OWNER ───────────
	jobs := issue606HTTPJobs(t, rdb)
	var got *emailqueue.Job
	for i := range jobs {
		if jobs[i].Type == "form_notification" {
			got = &jobs[i]
		}
	}
	if got == nil {
		t.Fatalf("the default {admin_email} rule delivered nothing for a paid sale "+
			"(queue holds %d job(s)). The recipient resolved to \"\" and the email was "+
			"dropped with notify_no_recipient — the merchant is never told about the sale.", len(jobs))
	}
	if got.To != ownerEmail {
		t.Errorf("default new-sale email went to %q, want the workspace owner %q "+
			"(users.id and accounts.id are independent UUIDs; resolve through "+
			"accounts.owner_user_id)", got.To, ownerEmail)
	}
	if got.AccountID != accountID {
		t.Errorf("job account_id = %q, want %q", got.AccountID, accountID)
	}
	if got.Subject == "" || !strings.Contains(got.Subject, sale.RefCode) {
		t.Errorf("job subject = %q, want the default rule's subject with the ref code %q rendered",
			got.Subject, sale.RefCode)
	}
	_ = workspaceID
}
