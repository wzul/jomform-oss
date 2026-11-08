package payments

// Card t_1c4a3f54 — the buyer receipt was silently lost on the CHIP callback
// path: `receipt_enqueue_failed err="context canceled"` on a real paid sale
// (JF-374daeb0ce61, 2026-11-06) while the merchant's form notification — which
// enqueues synchronously inside the request — succeeded.
//
// Root cause: the callback hands the REQUEST context down to
// `sendReceiptAsync`, which fires the receipt in a goroutine. net/http cancels
// that context the moment the response is written. The receipt's own work is
// heavier than the notification's — it renders a PDF and reads the workspace
// settings first — so by the time it reaches `EmailQueue.Enqueue` the context
// was already dead and the enqueue failed with "context canceled". The sale
// stayed `paid`, the buyer got nothing, and the only trace was a WARN line.
//
// These tests are integration tests against a real Postgres + Valkey and skip
// when either is unset (testdb.Open / testdb.Valkey).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/testdb"
)

// t1c4a3f54Fixture seeds an owner user + workspace + account in that workspace
// and a workspace_email_settings row with a RECOGNISABLE receipt subject, so a
// queued job proves the receipt resolved the workspace settings — not just that
// something was enqueued.
func t1c4a3f54Fixture(t *testing.T, ctx context.Context) (accountID, workspaceID, subject string) {
	t.Helper()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('t1c4a3f54-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 't1c4a3f54-ws-' || md5(random()::text), 't1c4a3f54-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 't1c4a3f54-acc-' || md5(random()::text), 't1c4a3f54-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	subject = "RESIT-KHAS-" + accountID[:8]
	if err := dbstore.NewWorkspaceStore(pool).SaveEmailSettings(ctx, &dbstore.EmailSettings{
		WorkspaceID:    workspaceID,
		ReceiptSubject: subject,
		ReceiptBody:    "Terima kasih {{buyer_name}} — {{ref_code}}",
		ReceiptEnabled: true,
		NotifyMerchant: false,
	}); err != nil {
		t.Fatalf("save workspace email settings: %v", err)
	}
	return accountID, workspaceID, subject
}

// t1c4a3f54ReceiptJobs reads every job on the queue's ready list without
// claiming any. The key comes from the queue (per-test prefix), never a literal.
func t1c4a3f54ReceiptJobs(t *testing.T, q *emailqueue.Queue, rdb *goredis.Client) []emailqueue.Job {
	t.Helper()
	raw, err := rdb.LRange(context.Background(), q.ReadyKey(), 0, -1).Result()
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

// TestReceiptEnqueuedWhenRequestContextIsCanceled is the regression for the
// production defect: a receipt produced by a request whose context is already
// done must STILL reach the queue, rendered from the workspace's settings.
//
// Bite-proof (measured, card t_1c4a3f54): reverting just the context detach in
// sendReceiptAsync while keeping the enqueue-level detach leaves the job reaching
// the queue but rendered from the built-in default — the workspace's settings read
// failed on the cancelled context — and this test fails on the subject assertion.
// Reverting the detach entirely loses the job altogether. Both were run and
// observed red before the fix was restored.
func TestReceiptEnqueuedWhenRequestContextIsCanceled(t *testing.T) {
	ctx := context.Background()
	accountID, workspaceID, wantSubject := t1c4a3f54Fixture(t, ctx)
	pool := testdb.Open(t)

	rdb, prefix := testdb.Valkey(t)
	q := emailqueue.NewQueueWithPrefix(rdb, prefix)

	svc := &Service{
		Accounts:    dbstore.NewAccountStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		EmailQueue:  q,
		EmailEvents: dbstore.NewEmailDeliveryEventStore(pool),
		Receipts: &Receipts{
			Sender: email.New(email.SMTPConfig{Host: "127.0.0.1", Port: "1", From: "noreply@example.test"}),
			Base:   "https://example.test",
		},
	}

	buyer := "pembeli-" + accountID[:8] + "@example.test"
	const ref = "JF-CANCELED-CTX"
	sale := &dbstore.Sale{
		ID:        accountID, // unused by the receipt path beyond the job payload
		AccountID: accountID,
		RefCode:   ref,
		Client:    dbstore.SaleClient{FullName: "Pembeli Ujian", Email: buyer},
		Items:     []dbstore.SaleItem{{Name: "Tiket", PriceSen: 10000, Qty: 1}},
		TotalSen:  10204, FeeSen: 204, FeeMethod: "card", FeePercentBp: 200,
	}

	// The request this receipt came from is already over: exactly the state the
	// production callback left behind when it returned 200 and net/http cancelled
	// r.Context().
	reqCtx, cancel := context.WithCancel(ctx)
	cancel()

	svc.sendReceiptAsync(reqCtx, sale)

	var job *emailqueue.Job
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobs := t1c4a3f54ReceiptJobs(t, q, rdb)
		for i := range jobs {
			if jobs[i].Type == "receipt" {
				job = &jobs[i]
				break
			}
		}
		if job != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job == nil {
		t.Fatalf("no receipt job reached the queue for a paid sale whose request context was done.\n"+
			"This is the production defect: receipt_enqueue_failed err=\"context canceled\" — the\n"+
			"buyer pays, gets no receipt, and the seller is never told. Workspace %s, ref %s.",
			workspaceID, ref)
	}
	if job.To != buyer {
		t.Errorf("receipt recipient = %q, want the buyer %q", job.To, buyer)
	}
	if job.SaleRef != ref {
		t.Errorf("receipt sale_ref = %q, want %q", job.SaleRef, ref)
	}
	// The subject proves the WHOLE background pipeline ran on a live context, not
	// just the queue write: it is the workspace's custom subject, which is only
	// reachable through Workspaces.EmailSettings — a DB read that fails on a
	// cancelled context and silently degrades to the built-in default.
	if !strings.Contains(job.Subject, wantSubject) {
		t.Errorf("receipt subject = %q, want it to contain the workspace's custom subject %q.\n"+
			"A default subject here means the background path still used the cancelled\n"+
			"request context for its DB reads and only got as far as the queue by luck.",
			job.Subject, wantSubject)
	}
	if len(job.PDFData) == 0 {
		t.Errorf("receipt job carries no PDF; the paid-sale receipt is the buyer's proof of the grossed-up total")
	}
}

// TestReceiptEnqueueFailureIsRecordedNotSilent covers the second half of the
// card: when the queue itself cannot be reached, the loss must be RECORDED, not
// merely logged. An enqueue failure is invisible to the queue's own retry ladder
// (the job never reached Valkey), so it is written to email_delivery_events as an
// account-scoped `enqueue_failed` row correlated to the sale.
func TestReceiptEnqueueFailureIsRecordedNotSilent(t *testing.T) {
	ctx := context.Background()
	accountID, workspaceID, _ := t1c4a3f54Fixture(t, ctx)
	pool := testdb.Open(t)

	// A queue over an address nothing listens on: every offer fails immediately.
	dead := goredis.NewClient(&goredis.Options{
		Addr:         "127.0.0.1:1",
		MaxRetries:   -1,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() { _ = dead.Close() })
	q := emailqueue.NewQueueWithPrefix(dead, "jftest:t1c4a3f54-fail:")

	svc := &Service{
		Accounts:    dbstore.NewAccountStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		EmailQueue:  q,
		EmailEvents: dbstore.NewEmailDeliveryEventStore(pool),
		Receipts: &Receipts{
			Sender: email.New(email.SMTPConfig{Host: "127.0.0.1", Port: "1", From: "noreply@example.test"}),
			Base:   "https://example.test",
		},
	}

	buyer := "gagal-" + accountID[:8] + "@example.test"
	const ref = "JF-ENQUEUE-FAIL"
	svc.SendSaleReceipt(ctx, SaleRef{
		AccountID:    accountID,
		WorkspaceID:  workspaceID,
		RefCode:      ref,
		BuyerEmail:   buyer,
		BuyerName:    "Pembeli Gagal",
		AmountSen:    10204,
		ItemsSummary: "1× Tiket",
	})

	var eventType, email, saleRef, jobType, attempts string
	err := pool.QueryRow(ctx, `
		SELECT event_type, email, COALESCE(sale_ref,''),
		       COALESCE(detail->>'job_type',''), COALESCE(detail->>'attempts','')
		  FROM email_delivery_events
		 WHERE account_id = $1 AND sale_ref = $2
		 ORDER BY id DESC LIMIT 1`, accountID, ref).
		Scan(&eventType, &email, &saleRef, &jobType, &attempts)
	if err != nil {
		t.Fatalf("no delivery event recorded for a receipt that could not be enqueued: %v\n"+
			"A lost receipt must leave an account-scoped trace, or the merchant has no way to see it.", err)
	}
	if eventType != dbstore.EmailEventEnqueueFailed {
		t.Errorf("event_type = %q, want %q", eventType, dbstore.EmailEventEnqueueFailed)
	}
	if email != buyer {
		t.Errorf("event email = %q, want the intended recipient %q", email, buyer)
	}
	if saleRef != ref {
		t.Errorf("event sale_ref = %q, want %q", saleRef, ref)
	}
	if jobType != "receipt" {
		t.Errorf("event detail.job_type = %q, want \"receipt\"", jobType)
	}
	if attempts != "4" {
		t.Errorf("event detail.attempts = %q, want \"4\" — the retry ladder must run before the failure is declared", attempts)
	}
}
