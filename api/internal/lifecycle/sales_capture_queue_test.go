package lifecycle

// THE BULK-CAPTURE QUEUE AND THE SALES TABLE ARE THE SAME RULE.
//
// The "Capture all pending" queue (dbstore.ListAwaitingCapture, used by
// POST /v1/rest/sales/bulk-capture with refs="all" and by the MCP
// capture_all_pending_sales tool) filters by `skip_capture`, `captured_at`,
// `chip_purchase_id` and a STATUS LIST. That status list was written inline in
// the query and predated this table, and it read
// ('pending','paid','authorized') — three of the four states the table captures
// from. The consequence was money: a sale CONFIRMED before its authorization was
// captured could be charged one sale at a time (the per-sale endpoint's guard
// passes), and "Capture all" never offered it, so the hold expired and the
// merchant was never charged for an authorization they believed was covered.
//
// The fix states the rule once: the queue's status set is
// dbstore.CaptureQueueStatuses, and it must equal
// SalesMachine.StatesAllowing(SaleEventCapture) in BOTH directions. The first
// test below is that pin (pure, no database); the second proves it against real
// rows, including the two controls that stop it from passing by the queue being
// broken the other way (a captured row, an ordinary non-skip_capture sale).
//
// This file also carries the DERIVATION guard. The card that built this table
// froze a second fact here: `authorized` BOOLEAN was an annotation of
// captured_at (its only writer, dbstore.MarkCaptured, stamped the boolean, the
// timestamp and status=paid in one statement), and status — not the boolean —
// is what every reader keys on. Card t_7f50c3fc then removed the duplicate:
// migration 000091 drops the column, the shared SELECT reports
// `(captured_at IS NOT NULL) AS authorized`, and
// TestSalesAuthorizedColumnIsGoneAndDerivedFromCapturedAt fails if either half
// regresses. Status is still the authority.
//
// Integration tests: need real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// without it. They use testdb.Open, whose per-process scratch schema means no
// database is created and nothing leaks between runs.

import (
	"context"
	"fmt"
	"sort"
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

// saleSeed is one row to insert, in the shape a real sale has. The zero value is
// the shape the capture queue is FOR: skip_capture on, uncaptured, a CHIP
// purchase attached. The two negative fields turn on the ways a sale can look
// capturable and not be. There is no `authorized` field to seed: the column is
// gone (migration 000091) and the value is derived from captured_at, so a seed
// that wants a captured row says Captured.
type saleSeed struct {
	Status   string
	Captured bool
	// NotSkipCapture is an ordinary CHIP sale: charged at once, never a hold.
	NotSkipCapture bool
	// NoPurchase is a skip_capture sale with no CHIP purchase yet — there is no
	// hold to capture.
	NoPurchase bool
}

// seedSale inserts one sale and returns its id. captured=false leaves captured_at
// NULL, and since the `authorized` column is gone (migration 000091) that
// timestamp is the only thing the derived `authorized` value comes from.
func seedSale(t *testing.T, ctx context.Context, p *pgxpool.Pool, accountID string, seed saleSeed) string {
	t.Helper()
	var id string
	capturedAtSQL := "NULL"
	if seed.Captured {
		capturedAtSQL = "now()"
	}
	purchaseSQL := "'pur_' || md5(random()::text)"
	if seed.NoPurchase {
		// Empty, not NULL: an unattached purchase reads as "" through every
		// COALESCE in the select, and the guard tests it that way.
		purchaseSQL = "''"
	}
	err := p.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO sales (account_id, ref_code, client, items, subtotal_sen, total_sen,
		     status, skip_capture, captured_at, chip_purchase_id)
		   VALUES ($1, 'REF-' || md5(random()::text), '{}'::jsonb, '[]'::jsonb, 1000, 1000,
		     $2, $3, %s, %s)
		   RETURNING id`, capturedAtSQL, purchaseSQL),
		accountID, seed.Status, !seed.NotSkipCapture).Scan(&id)
	if err != nil {
		t.Fatalf("seed a %s sale (%+v): %v", seed.Status, seed, err)
	}
	return id
}

// TestMarkCapturedStampsTheTimestampThatIsTheBoolean pins the only capture
// write: it sets status = paid and stamps captured_at in ONE statement, and the
// sale reads back `authorized = true` because that field is derived from the
// timestamp. The column that used to hold the flag is gone (migration 000091),
// so there is no second place for it to drift.
func TestMarkCapturedStampsTheTimestampThatIsTheBoolean(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID := salesFixture(t, ctx, p)
	store := dbstore.NewSaleStore(p)

	saleID := seedSale(t, ctx, p, accountID, saleSeed{Status: dbstore.SalePending})

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
		t.Error("a captured sale does not read authorized=true — the derivation from captured_at is broken")
	}
	if after.CapturedAt == nil {
		t.Error("MarkCaptured did not stamp captured_at")
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

// TestCaptureQueueOffersExactlyWhatTheTableAllows is the pin that replaces the
// frozen reproduction of the divergence. The queue's status set and the table's
// capture-source states must be the SAME SET — named in both directions, because
// each direction is a different bug:
//
//   - a table state missing from the queue = an authorization the per-sale
//     endpoint charges and "Capture all" silently drops (this was `confirmed`);
//   - a queue state the table refuses = a bulk action offering a row its own
//     guard rejects (money refused mid-batch, per-row error noise).
func TestCaptureQueueOffersExactlyWhatTheTableAllows(t *testing.T) {
	queued := dbstore.CaptureQueueStatuses()
	table := SalesMachine.StatesAllowing(SaleEventCapture)
	if len(table) == 0 {
		t.Fatal("the table declares no capture edge at all — the fixture assumption is wrong")
	}
	sortedQueued := append([]string(nil), queued...)
	sort.Strings(sortedQueued)

	for _, state := range table {
		found := false
		for _, q := range sortedQueued {
			if q == state {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the table allows capture from %q and the bulk queue does not offer it: "+
				"a sale in that state is charged by POST /v1/rest/sales/{ref}/capture and never by \"Capture all\"", state)
		}
	}
	for _, state := range sortedQueued {
		if !SalesMachine.Allows(state, SaleEventCapture) {
			t.Errorf("the bulk queue offers %q but the table declares no capture edge from it: "+
				"the batch would offer a row its own guard refuses", state)
		}
	}
	if got, want := fmt.Sprint(sortedQueued), fmt.Sprint(table); got != want {
		t.Errorf("capture queue statuses = %s, table capture states = %s (both sorted) — the queue and the table must be one rule", got, want)
	}

	// A capture always lands on `paid`: dbstore.MarkCaptured writes that status in
	// its own statement, so a state whose capture edge pointed anywhere else would
	// make the queue's promise and the write disagree.
	for _, state := range table {
		sale := &dbstore.Sale{Status: state, SkipCapture: true, ChipPurchaseID: "pur_pin"}
		res, err := SalesMachine.Apply(sale, SaleEventCapture)
		if err != nil {
			t.Errorf("the table's own capture edge from %q refuses a plain uncaptured authorization: %v", state, err)
			continue
		}
		if res.To != dbstore.SalePaid {
			t.Errorf("capture from %q lands on %q; MarkCaptured writes %q", state, res.To, dbstore.SalePaid)
		}
	}
}

// TestBulkCaptureQueueOffersEveryCapturableState is the same rule against real
// rows: every state the table captures from, in the shape the queue is for, IS
// offered — including `confirmed`, which used to be dropped — and every shape
// the queue must NOT offer stays out.
func TestBulkCaptureQueueOffersEveryCapturableState(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID := salesFixture(t, ctx, p)
	store := dbstore.NewSaleStore(p)

	// The capturable shapes: each state the table captures from, uncaptured, with
	// a hold attached. The legacy status='authorized' row is seeded with no
	// captured_at too, so (now that `authorized` is derived) it reads false and
	// the queue is proved to key on status, not on that field.
	wanted := map[string]string{}
	for _, state := range SalesMachine.StatesAllowing(SaleEventCapture) {
		wanted[state] = seedSale(t, ctx, p, accountID, saleSeed{Status: state})
	}
	confirmedID, ok := wanted[dbstore.SaleConfirmed]
	if !ok {
		t.Fatal("the table no longer captures from confirmed — this test is about that state")
	}

	// The controls: capturable-looking and not capturable.
	capturedID := seedSale(t, ctx, p, accountID, saleSeed{Status: dbstore.SaleConfirmed, Captured: true})
	ordinaryID := seedSale(t, ctx, p, accountID, saleSeed{Status: dbstore.SaleConfirmed, NotSkipCapture: true})
	noPurchaseID := seedSale(t, ctx, p, accountID, saleSeed{Status: dbstore.SaleConfirmed, NoPurchase: true})
	terminal := map[string]string{}
	for _, state := range []string{dbstore.SaleCancelled, dbstore.SaleFailed, dbstore.SaleRefunded} {
		terminal[state] = seedSale(t, ctx, p, accountID, saleSeed{Status: state})
	}

	queued, err := store.ListAwaitingCapture(ctx, accountID)
	if err != nil {
		t.Fatalf("ListAwaitingCapture: %v", err)
	}
	inQueue := map[string]string{}
	for _, s := range queued {
		inQueue[s.ID] = s.Status
	}

	// Direction 1: everything the table captures from is offered.
	for state, id := range wanted {
		if _, ok := inQueue[id]; !ok {
			t.Errorf("a %s sale that is an uncaptured authorization is NOT in the capture queue: "+
				"the per-sale endpoint would charge it and \"Capture all\" would leave it to expire", state)
		}
	}
	// The reported regression, named so a failure explains itself.
	if _, ok := inQueue[confirmedID]; !ok {
		t.Error("the CONFIRMED uncaptured authorization is not queued — this is the defect the card names")
	}

	// Direction 2: nothing else is offered.
	for label, id := range map[string]string{
		"an already-captured authorization":                   capturedID,
		"an ordinary non-skip_capture sale":                   ordinaryID,
		"a skip_capture sale with no CHIP purchase (no hold)": noPurchaseID,
		"a cancelled sale":                                    terminal[dbstore.SaleCancelled],
		"a failed sale":                                       terminal[dbstore.SaleFailed],
		"a refunded sale":                                     terminal[dbstore.SaleRefunded],
	} {
		if _, ok := inQueue[id]; ok {
			t.Errorf("%s is in the capture queue: the queue must offer only what the capture move accepts", label)
		}
	}

	// Equivalence, not just containment: every row the queue hands the batch must
	// pass the SAME guard the single-sale endpoint applies, or the batch reports a
	// per-row refusal for something it offered.
	for _, s := range queued {
		if _, err := SalesMachine.Apply(s, SaleEventCapture); err != nil {
			t.Errorf("the queue offers %s (status %s) and the capture move refuses it: %v", s.RefCode, s.Status, err)
		}
	}

	// And the count is exactly the capturable shapes: a stray row would mean the
	// account filter or one of the other predicates is wide open.
	if len(queued) != len(wanted) {
		t.Errorf("queue holds %d rows, want %d (one per capture-source state): %v", len(queued), len(wanted), inQueue)
	}

	// The old behaviour, recorded so nobody re-introduces it: a captured row is
	// out, and MarkCaptured is what put it out.
	if err := store.MarkCaptured(ctx, confirmedID); err != nil {
		t.Fatalf("MarkCaptured on the confirmed authorization: %v", err)
	}
	after, err := store.ListAwaitingCapture(ctx, accountID)
	if err != nil {
		t.Fatalf("ListAwaitingCapture after capture: %v", err)
	}
	for _, s := range after {
		if s.ID == confirmedID {
			t.Error("a captured confirmed authorization is still queued — it would be charged twice")
		}
	}
	if len(after) != len(queued)-1 {
		t.Errorf("queue holds %d rows after capturing one, want %d", len(after), len(queued)-1)
	}
}

// TestSalesAuthorizedColumnIsGoneAndDerivedFromCapturedAt is the guard for the
// FIX (card t_7f50c3fc): the duplicated column is dropped (migration 000091) and
// the API reports `authorized` as `captured_at IS NOT NULL`, so a reader can
// never see the two disagree and no writer can reintroduce the drift.
//
// It bites in three independent ways:
//   - if the column is added back to the schema, the information_schema count
//     is non-zero;
//   - if the select stops deriving the field, the seeded shapes disagree with
//     their own captured_at;
//   - if the column comes back, a raw UPDATE ... SET authorized succeeds.
func TestSalesAuthorizedColumnIsGoneAndDerivedFromCapturedAt(t *testing.T) {
	ctx := context.Background()
	p := testdb.Open(t)
	accountID := salesFixture(t, ctx, p)
	store := dbstore.NewSaleStore(p)

	// 1. The schema has ONE place for the fact: captured_at.
	var cols int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'sales' AND column_name = 'authorized'`,
	).Scan(&cols); err != nil {
		t.Fatalf("read information_schema for sales.authorized: %v", err)
	}
	if cols != 0 {
		t.Fatalf("sales.authorized still exists (%d column): the duplicated flag is back, so it can drift from captured_at again", cols)
	}

	// 2. Every shape reads back a boolean that EQUALS its own timestamp. There is
	// no seed that can make them disagree any more — that is the point.
	cases := []struct {
		name     string
		status   string
		captured bool
	}{
		{"pending uncaptured authorization", dbstore.SalePending, false},
		{"legacy status=authorized, nothing captured", dbstore.SaleAuthorized, false},
		{"captured paid authorization", dbstore.SalePaid, true},
	}
	for _, tc := range cases {
		id := seedSale(t, ctx, p, accountID, saleSeed{Status: tc.status, Captured: tc.captured})
		sale, err := store.ByID(ctx, id)
		if err != nil {
			t.Fatalf("%s: read sale: %v", tc.name, err)
		}
		if got, want := sale.Authorized, sale.CapturedAt != nil; got != want {
			t.Errorf("%s: authorized=%v but captured_at is set=%v — the field is not derived from the timestamp", tc.name, got, want)
		}
		if sale.Authorized != tc.captured {
			t.Errorf("%s: authorized=%v, want %v (the seed's captured_at state)", tc.name, sale.Authorized, tc.captured)
		}
	}

	// 3. No write path can set the old flag: there is no column to update. This
	// is what makes the drift unrepeatable rather than merely unused.
	id := seedSale(t, ctx, p, accountID, saleSeed{Status: dbstore.SalePending})
	if _, err := p.Exec(ctx, `UPDATE sales SET authorized=true WHERE id=$1`, id); err == nil {
		t.Fatal("UPDATE sales SET authorized=true succeeded — the duplicated column is back and a writer can drift it again")
	}
}
