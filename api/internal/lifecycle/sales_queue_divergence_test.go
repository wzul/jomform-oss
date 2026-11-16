package lifecycle

// THE REPRODUCTION of the sales machine's real divergence, against Postgres.
//
// The card that built this table required the duplicated "authorized" state to be
// RESOLVED on paper and REPRODUCED in a test — not fixed. Two things are true
// today and frozen here:
//
//  1. `authorized` BOOLEAN is a duplicate of captured_at, not of
//     status = 'authorized'. The only writer, dbstore's MarkCaptured, sets the
//     boolean, the timestamp and status = paid in one statement — so no write
//     path can produce authorized = true without captured_at, and a row can
//     carry status = 'authorized' with authorized = false. Status wins.
//
//  2. The capture QUEUE is narrower than the TABLE. ListAwaitingCapture filters
//     status IN ('pending','paid','authorized') — a filter that predates the
//     table — while the table declares `capture` from `confirmed` too (the
//     single-sale endpoint has always accepted it: the old handler checked only
//     skip_capture/captured_at/chip_purchase_id). A merchant who confirms an
//     uncaptured authorization can therefore capture it one by one, but "Capture
//     all" will never offer it. That is money left on a hold until it expires,
//     and it is filed as its own card rather than widened here: widening the
//     queue makes a bulk action charge a sale it does not charge today, which is
//     exactly the "no newly-legal transition" line this card must not cross.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// without it. It uses testdb.Open, whose per-process scratch schema means no
// database is created and nothing leaks between runs.

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// salesFixture creates a user + workspace + account and returns the account id,
// so a sale can be seeded with the same FK shape production has.
func salesFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	var userID, workspaceID, accountID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-lifecycle-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-lifecycle-' || md5(random()::text), 'it-lifecycle-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-lifecycle-acc-' || md5(random()::text), 'it-lifecycle-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID
}

// seedSale inserts one sale in the shape a real row has, and returns its id.
// capturedAt nil = uncaptured; the `authorized` boolean is passed explicitly so
// the disagreeing shapes can be produced on purpose.
func seedSale(t *testing.T, ctx context.Context, p *pgxpool.Pool, accountID, status string, authorized bool, captured bool) string {
	t.Helper()
	var id string
	capturedAtSQL := "NULL"
	if captured {
		capturedAtSQL = "now()"
	}
	err := p.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO sales (account_id, ref_code, client, items, subtotal_sen, total_sen,
		     status, skip_capture, authorized, captured_at, chip_purchase_id)
		   VALUES ($1, 'REF-' || md5(random()::text), '{}'::jsonb, '[]'::jsonb, 1000, 1000,
		     $2, true, $3, %s, 'pur_' || md5(random()::text))
		   RETURNING id`, capturedAtSQL),
		accountID, status, authorized).Scan(&id)
	if err != nil {
		t.Fatalf("seed a %s sale (authorized=%v, captured=%v): %v", status, authorized, captured, err)
	}
	return id
}

// TestMarkCapturedStampsTheBooleanAndTheTimestampTogether pins the only write
// path that ever sets `authorized`: it writes status = paid, authorized = true
// and captured_at in ONE statement, so the boolean cannot exist without the
// timestamp. That is why the boolean is an annotation of the past and not a
// state — and why status='authorized' with authorized=false is a shape the code
// can still meet (legacy rows) but never produce.
func TestMarkCapturedStampsTheBooleanAndTheTimestampTogether(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID := salesFixture(t, ctx, p)
	store := dbstore.NewSaleStore(p)

	saleID := seedSale(t, ctx, p, accountID, dbstore.SalePending, false, false)

	before, err := store.ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("read seeded sale: %v", err)
	}
	if before.Authorized || before.CapturedAt != nil {
		t.Fatalf("seeded sale is already captured: authorized=%v captured_at=%v", before.Authorized, before.CapturedAt)
	}
	if !before.SkipCapture {
		t.Fatal("seeded sale lost skip_capture")
	}

	if err := store.MarkCaptured(ctx, saleID); err != nil {
		t.Fatalf("MarkCaptured: %v", err)
	}
	after, err := store.ByID(ctx, saleID)
	if err != nil {
		t.Fatalf("re-read captured sale: %v", err)
	}
	if !after.Authorized {
		t.Error("MarkCaptured did not set authorized=true")
	}
	if after.CapturedAt == nil {
		t.Error("MarkCaptured did not stamp captured_at — the boolean would then exist without it")
	}
	if after.Status != dbstore.SalePaid {
		t.Errorf("MarkCaptured status = %q, want %q (the table's capture target)", after.Status, dbstore.SalePaid)
	}

	// The row the write produced: the machine now refuses a second capture by its
	// GUARD, not by reading the boolean.
	if _, err := SalesMachine.Apply(after, SaleEventCapture); err == nil {
		t.Error("the machine allowed a second capture on a captured authorization")
	}
}

// TestAwaitingCaptureQueueIsNarrowerThanTheTable is the frozen reproduction.
// Read it as a statement of today's behaviour, not as an endorsement:
//
//   - a `confirmed` sale that is still an uncaptured authorization is
//     CAPTURABLE (the table has the edge, the guard passes) but is NOT in the
//     bulk queue;
//   - a legacy `status='authorized'` row whose boolean says false IS in the
//     queue, which is the second proof that status — not the boolean — is what
//     every reader agrees on.
func TestAwaitingCaptureQueueIsNarrowerThanTheTable(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID := salesFixture(t, ctx, p)
	store := dbstore.NewSaleStore(p)

	confirmedUncaptured := seedSale(t, ctx, p, accountID, dbstore.SaleConfirmed, false, false)
	legacyAuthorized := seedSale(t, ctx, p, accountID, dbstore.SaleAuthorized, false, false)
	pendingUncaptured := seedSale(t, ctx, p, accountID, dbstore.SalePending, false, false)
	capturedPaid := seedSale(t, ctx, p, accountID, dbstore.SalePaid, true, true)

	queued, err := store.ListAwaitingCapture(ctx, accountID)
	if err != nil {
		t.Fatalf("ListAwaitingCapture: %v", err)
	}
	inQueue := map[string]bool{}
	for _, s := range queued {
		inQueue[s.ID] = true
	}

	// Controls: the two shapes the queue is FOR.
	if !inQueue[pendingUncaptured] {
		t.Error("a pending uncaptured authorization is not in the capture queue")
	}
	if inQueue[capturedPaid] {
		t.Error("a captured authorization is in the capture queue — it would be charged twice")
	}

	// The legacy row: status says authorized, the boolean says false. Every
	// reader keys on status, so it is queued.
	legacy, err := store.ByID(ctx, legacyAuthorized)
	if err != nil {
		t.Fatalf("read legacy authorized sale: %v", err)
	}
	if legacy.Status != dbstore.SaleAuthorized {
		t.Fatalf("legacy sale status = %q, want authorized", legacy.Status)
	}
	if legacy.Authorized {
		t.Fatal("this case needs authorized=false to reproduce the split, and the seed produced true")
	}
	if !inQueue[legacyAuthorized] {
		t.Error("a status='authorized' row with authorized=false is NOT queued: the queue read the boolean, not the status")
	}
	if res, err := SalesMachine.Apply(legacy, SaleEventCapture); err != nil {
		t.Errorf("the table refuses capture from a legacy authorized row: %v", err)
	} else if res.To != dbstore.SalePaid {
		t.Errorf("legacy authorized capture target = %q, want paid", res.To)
	}

	// THE DIVERGENCE. If this starts failing, someone aligned the queue with the
	// table (or the table with the queue) — that is the separate fix, and this
	// frozen expectation is what has to move with it.
	confirmed, err := store.ByID(ctx, confirmedUncaptured)
	if err != nil {
		t.Fatalf("read confirmed uncaptured sale: %v", err)
	}
	res, err := SalesMachine.Apply(confirmed, SaleEventCapture)
	if err != nil {
		t.Fatalf("the table no longer allows capture from confirmed (%v) — the divergence changed shape; "+
			"re-read sales.go's capture edges before updating this test", err)
	}
	if res.To != dbstore.SalePaid {
		t.Errorf("confirmed capture target = %q, want paid", res.To)
	}
	if inQueue[confirmedUncaptured] {
		t.Error("the bulk queue now INCLUDES a confirmed uncaptured authorization. " +
			"That is the alignment this card deliberately did not make (it newly charges a sale on a bulk action); " +
			"if it was made on purpose, this frozen expectation moves with it and the card's behaviour-freeze list must name it.")
	}
}
