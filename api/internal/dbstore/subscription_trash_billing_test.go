package dbstore

// Cards t_3f895712 (parts 1 and 3 of the acceptance criteria) — the money defect.
//
// Binning a workspace marks `workspaces.deleted_at`. It does NOT touch the
// subscription, which stays `active` with its `next_charge_at` in the past, and
// nothing on the billing path could see the trash state at all: the only edge from
// a workspace to its subscription was `subscriptions.account_id → accounts`, and
// the account hangs off the workspace. So the scheduler kept charging a customer
// for a workspace the user had "deleted" — a ghost charge, and the one escape
// route (Purge) was itself blocked for any workspace with sales history.
//
// Wan's instruction is "hentikan sahaja": STOP BILLING, do not cancel. So the
// assertion below is two-sided on purpose — the subscription row must be left
// EXACTLY as it was (status still active, next_charge_at untouched), and billing
// must resume by itself on Restore. A test that only checked the first half would
// pass equally on a build that cancelled the subscription, which is precisely the
// behaviour Wan did not ask for.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDueSubscription puts a due, active subscription on the account of
// `workspaceID` and returns its id and the seeded next_charge_at.
func seedDueSubscription(t *testing.T, ctx context.Context, p *pgxpool.Pool, workspaceID string) (subID string, dueAt time.Time) {
	t.Helper()
	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspaceID).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	ps := NewProductStore(p)
	prod := &Product{
		AccountID: acc, Name: "IT recurring", PriceSen: 2500, Currency: "MYR",
		Type: "subscription", BillingPeriod: "month",
	}
	if err := ps.Create(ctx, prod); err != nil {
		t.Fatalf("create subscription product: %v", err)
	}
	dueAt = time.Now().Add(-time.Hour)
	if err := p.QueryRow(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id,
		                            period, amount_sen, next_charge_at)
		 VALUES ($1, 'it-trash-billing@example.test', $2, 'tok-' || md5(random()::text),
		         'month', 2500, $3)
		 RETURNING id`, acc, prod.ID, dueAt).Scan(&subID); err != nil {
		t.Fatalf("seed due subscription: %v", err)
	}
	return subID, dueAt
}

// dueContains reports whether DueActive returned the given subscription.
func dueContains(t *testing.T, ctx context.Context, ss *SubscriptionStore, subID string) bool {
	t.Helper()
	due, err := ss.DueActive(ctx, time.Now(), 200)
	if err != nil {
		t.Fatalf("DueActive: %v", err)
	}
	for _, s := range due {
		if s.ID == subID {
			return true
		}
	}
	return false
}

// TestBillingIsUnchangedForAnAccountWithNoWorkspace guards the shape of the fix.
//
// The natural-looking predicate — "only charge when the account has a LIVE
// workspace" — is wrong: it also stops billing accounts with NO workspace, which is
// not a trashed workspace, it is the pre-workspace era (the design note counts 6
// such accounts on prod). The `payments` package caught that when it was written
// (`TestRound13bFixedCountCollectsExactlyNThenStops`), but those tests are about
// charge mechanics, so nothing there says the billing predicate must keep treating
// an orphan account as billable. This leg says exactly that, and it is the control
// for the trash test above: same store, same call, only the trash state differs.
func TestBillingIsUnchangedForAnAccountWithNoWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ss := NewSubscriptionStore(p)

	var userID, accID, prodID, subID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('orphan-bill-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).
		Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// The pre-workspace shape: an account with workspace_id NULL.
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'orphan-' || md5(random()::text), 'orphan-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO products (account_id, name, price_sen, currency, type, billing_period)
		 VALUES ($1, 'orphan recurring', 1000, 'MYR', 'subscription', 'month') RETURNING id`,
		accID).Scan(&prodID); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id,
		                            period, amount_sen, next_charge_at)
		 VALUES ($1, 'orphan-billing@example.test', $2, 'tok-' || md5(random()::text),
		         'month', 1000, now() - interval '1 hour')
		 RETURNING id`, accID, prodID).Scan(&subID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	if !dueContains(t, ctx, ss, subID) {
		t.Error("an account with NO workspace is no longer billed — that is not a trashed workspace, " +
			"it is the pre-workspace era, and the ghost-charge fix must name only the trash state. " +
			"A predicate of the form EXISTS(… live workspace …) does this; NOT EXISTS(… trashed …) " +
			"does not")
	}
}

// TestTrashedWorkspaceStopsBillingAndRestoreResumesIt is the card's acceptance
// criterion 3, both directions.
func TestTrashedWorkspaceStopsBillingAndRestoreResumesIt(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	ss := NewSubscriptionStore(p)

	// Two workspaces: Delete refuses the owner's last one, so a single-workspace
	// fixture could never reach the trashed state this test is about.
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	subID, dueAt := seedDueSubscription(t, ctx, p, target)

	// CONTROL — before the trash the scheduler must see it. Without this a green
	// "not returned" below could mean the query never returned anything at all.
	if !dueContains(t, ctx, ss, subID) {
		t.Fatal("premise broken: the scheduler does not return a due, active subscription on a " +
			"LIVE workspace, so the assertion below would pass for the wrong reason")
	}

	// Bin the workspace.
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 1. Billing stops: the due, active subscription of a TRASHED workspace is not
	//    handed to the charger. THIS IS THE GHOST-CHARGE FIX.
	if dueContains(t, ctx, ss, subID) {
		t.Error("the scheduler still returned a due subscription whose workspace is in the bin — " +
			"the customer is charged for a workspace the user deleted (card t_3f895712)")
	}

	// 2. "Hentikan sahaja" — the row itself is UNTOUCHED. A build that cancelled or
	//    paused the subscription would satisfy (1) while doing something Wan did not
	//    ask for, and would need a compensating write on Restore.
	var status string
	var gotDue time.Time
	if err := p.QueryRow(ctx,
		`SELECT status, next_charge_at FROM subscriptions WHERE id=$1`, subID).
		Scan(&status, &gotDue); err != nil {
		t.Fatalf("read back subscription: %v", err)
	}
	if status != "active" {
		t.Errorf("subscription status = %q after the trash, want %q — the instruction is "+
			"'stop billing', not 'cancel', and a paused/cancelled row needs a compensating "+
			"write on Restore that can disagree with the trash state it mirrors", status, "active")
	}
	// Compare at SECOND resolution: Postgres stores micros, Go's time.Now() has
	// nanos, so the round trip cannot be exactly equal. The claim is "untouched",
	// which one second of slack tests without weakening it — a compensating write
	// on the trash path would move it by far more than a second.
	if gotDue.Unix() != dueAt.Unix() {
		t.Errorf("next_charge_at = %v after the trash, want it untouched at %v — the pause must "+
			"not write to the subscription at all", gotDue, dueAt)
	}

	// 3. Restore resumes billing by itself.
	if err := sp.Restore(ctx, target, user); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !dueContains(t, ctx, ss, subID) {
		t.Error("the scheduler still does not return the subscription after Restore — billing " +
			"must resume without any compensating write, which is the whole reason the pause is " +
			"a predicate over the live state rather than a status change")
	}
}

// TestWorkspacePurgeRemovesSubscriptionsBeforeTheProductThatHoldsThem is
// acceptance criterion 4. BOTH ends of a subscription RESTRICT — account_id and
// product_id — so a purge that reached the product step first failed with a raw FK
// error. The workspace here has a subscription-bearing product and no sales, so it
// is exactly the case the sales guard does NOT refuse.
func TestWorkspacePurgeRemovesSubscriptionsBeforeTheProductThatHoldsThem(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	subID, _ := seedDueSubscription(t, ctx, p, target)
	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	purged, err := sp.Purge(ctx, target, user)
	if err != nil {
		t.Fatalf("Purge refused a trashed workspace that holds a subscription: %v — the FK is "+
			"ON DELETE RESTRICT on BOTH subscriptions.account_id and subscriptions.product_id, so "+
			"the subscription rows must be deleted before the products and the accounts", err)
	}
	if purged["subscriptions"] != 1 {
		t.Errorf("purged[subscriptions] = %d, want 1 (the delete must report what it removed)",
			purged["subscriptions"])
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM subscriptions WHERE id=$1`, subID); n != 0 {
		t.Errorf("%d subscription row(s) survived the purge — a RESTRICT parent that outlives its "+
			"account is the raw-FK-error case this ordering exists to remove", n)
	}
	// The charges history cascades off the parent, so it must be gone too rather
	// than orphaned.
	if n := countRows(t, ctx, p,
		`SELECT COUNT(*) FROM subscriptions WHERE account_id=$1`, acc); n != 0 {
		t.Errorf("%d subscription row(s) still reference the purged account", n)
	}
}
