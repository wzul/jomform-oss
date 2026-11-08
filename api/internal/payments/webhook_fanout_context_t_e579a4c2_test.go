package payments_test

// Card t_e579a4c2: the webhook fan-out fired from a CHIP callback must not
// inherit the REQUEST context.
//
// The fan-out is asynchronous by design (issue #197): ProcessCallbackTx spawns
// it in a goroutine, that goroutine calls OnPaid, and OnPaid enqueues a durable
// webhook_retries row per matching webhook. But the goroutine used to be handed
// the callback's request context, and net/http cancels that context the moment
// the response is written — which is BEFORE the goroutine has done its work.
// ListActiveForDispatch and EnqueueRetry then both failed with `context
// canceled`, and the only trace was a log line: the merchant never received
// sale.paid and no retry row existed for the sweep to pick up.
//
// The test drives the REAL production fan-out (httpapi.DispatchWebhooks, the
// same call main.go's OnPaid makes) with the callback's own ordering:
//
//	ProcessCallback returns  →  request context is cancelled  →  fan-out runs
//
// The hook blocks until the test has cancelled, so the race is deterministic
// rather than a sleep. It then asserts the durable row exists — the thing that
// makes the delivery recoverable at all.
//
// RED before the fix (go s.OnPaid(ctx, …)): no webhook_retries row and OnPaid
// observed a cancelled context, for every event.
// GREEN after (s.goOnPaid → context.WithoutCancel): the row is there.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestWebhookFanoutSurvivesCallbackResponse(t *testing.T) {
	pool, err := testdb.Connect(t)
	if err != nil {
		t.Fatalf("test database unavailable: %v", err)
	}

	for _, tc := range []struct {
		name string
		// status is the callback event that must fan out.
		status string
		// wantEvent is the outbound event the fan-out must enqueue.
		wantEvent string
		// before is a callback to settle first with a LIVE context (a refund is
		// only a real transition from paid).
		before string
	}{
		{name: "paid", status: chip.StatusPaid, wantEvent: "sale.paid"},
		{name: "failed", status: chip.StatusError, wantEvent: "sale.failed"},
		{name: "refunded", status: chip.StatusRefunded, wantEvent: "sale.refunded", before: chip.StatusPaid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			accountID, purchaseID := seedFanoutSale(t, ctx, pool)

			whStore := dbstore.NewWebhookStore(pool)
			wh, werr := whStore.Create(ctx, accountID, "",
				"https://example.test/hook", "secret-fanout-1234", []string{tc.wantEvent})
			if werr != nil {
				t.Fatalf("create webhook: %v", werr)
			}

			svc := &payments.Service{
				Sales:    dbstore.NewSaleStore(pool),
				Accounts: dbstore.NewAccountStore(pool),
			}
			if tc.before != "" {
				if err := svc.ProcessCallback(ctx, &chip.Purchase{ID: purchaseID, Status: tc.before}); err != nil {
					t.Fatalf("settle %s first: %v", tc.before, err)
				}
			}

			// responseWritten is closed only AFTER the request context has been
			// cancelled — i.e. the hook is held until the "200 has been written",
			// which is exactly when net/http kills the request context.
			responseWritten := make(chan struct{})
			fannedOut := make(chan struct{})
			var hookCtxErr error

			svc.OnPaid = func(hctx context.Context, acctID, event string, payload map[string]any) {
				<-responseWritten
				hookCtxErr = hctx.Err()
				formID, _ := payload["form_id"].(string)
				// The real dispatcher, called the way main.go's OnPaid calls it.
				// The handler is unused by DispatchWebhooks, hence nil.
				httpapi.DispatchWebhooks(hctx, nil, whStore, acctID, formID, event, payload)
				close(fannedOut)
			}

			reqCtx, cancel := context.WithCancel(ctx)
			if err := svc.ProcessCallback(reqCtx, &chip.Purchase{ID: purchaseID, Status: tc.status}); err != nil {
				cancel()
				t.Fatalf("callback %s: %v", tc.status, err)
			}
			cancel()               // net/http cancels the request context with the response
			close(responseWritten) // the background fan-out is now free to run

			select {
			case <-fannedOut:
			case <-time.After(testdb.WebhookFireTimeout):
				t.Fatalf("webhook fan-out never ran for %s", tc.wantEvent)
			}

			var n int
			if err := pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM webhook_retries WHERE webhook_id=$1 AND event=$2`,
				wh.ID, tc.wantEvent).Scan(&n); err != nil {
				t.Fatalf("count webhook_retries: %v", err)
			}
			if n != 1 {
				t.Fatalf("webhook_retries rows for %s = %d, want 1 (the durable retry is what lets the merchant still receive the event); OnPaid context err = %v",
					tc.wantEvent, n, hookCtxErr)
			}
			if hookCtxErr != nil {
				t.Fatalf("OnPaid received a dead context (%v): the request context leaked into background work", hookCtxErr)
			}
		})
	}
}

// seedFanoutSale creates a pending one-time sale with a CHIP purchase id and
// returns the owning account id + purchase id.
func seedFanoutSale(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (accountID, purchaseID string) {
	t.Helper()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	sale := &dbstore.Sale{
		AccountID: accountID, RefCode: "IT-FANOUT-" + accountID[:8],
		Client:   dbstore.SaleClient{FullName: "Buyer", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "p1", Name: "Tee", PriceSen: 1000, Qty: 1}},
		TotalSen: 1000, SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
		Status: dbstore.SalePending,
	}
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	purchaseID = "pur_fanout_" + accountID[:8]
	if err := sales.AttachChipPurchase(ctx, sale.ID, purchaseID, "https://gate.example/checkout/"+purchaseID); err != nil {
		t.Fatalf("attach purchase: %v", err)
	}
	return accountID, purchaseID
}
