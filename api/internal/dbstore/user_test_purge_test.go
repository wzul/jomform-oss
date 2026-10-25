package dbstore

// Integration tests for the QA test-account purge (card t_6ae7f586).
//
// THE DEFECT. Twenty verified test accounts accumulated on production because a
// lane registered a fresh account for every card that verified signup, and
// nothing could remove them: `purge_unverified_accounts` deletes only
// verified=false rows and no admin route or MCP tool could delete a user. A
// human removed them in SQL.
//
// WHAT IS PINNED HERE, and why each case can fail:
//
//   - the purge finds accounts by the PERSISTED MARKER, so a lookalike account
//     with the same email shape and no marker survives (delete the marker
//     predicate from the SQL and this case reds);
//   - asking for ONE account that is not marked is REFUSED, not "0 removed";
//   - a marked account holding a sale or a subscription is BLOCKED and named,
//     and does not stop the other marked accounts from being removed;
//   - the PROTECTED account (the one reused production login, unverified on
//     purpose) survives BOTH purges — the test-account purge and the unverified
//     sweep — and the bite proof neutralises the safeguard and requires the row
//     to be deleted, so the assertion is about the safeguard and not the fixture;
//   - a dry run commits nothing and reports exactly what a real run then removes;
//   - a second run is a no-op (idempotent).
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied first,
// mirroring the production boot path.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createMarkedUser inserts a user with the given markers plus an
// auto-provisioned account (mirroring handleRegister), returning both ids.
func createMarkedUser(t *testing.T, ctx context.Context, p *pgxpool.Pool, email string, isTest, verified bool) (string, string) {
	t.Helper()
	var uid string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified, is_test) VALUES ($1, 'it', $2, $3) RETURNING id`,
		email, verified, isTest).Scan(&uid); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	var accID string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-store', 'it-' || md5(random()::text)) RETURNING id`, uid).Scan(&accID); err != nil {
		t.Fatalf("create account for %s: %v", email, err)
	}
	return uid, accID
}

// ensureProtectedUser makes sure the protected fixture row exists and returns
// its id. The email is the one real designation, so several tests may need it in
// the same scratch schema: an upsert keeps them from colliding.
func ensureProtectedUser(t *testing.T, ctx context.Context, p *pgxpool.Pool, verified bool) string {
	t.Helper()
	var uid string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified) VALUES ($1, 'it', $2)
		 ON CONFLICT (email) DO UPDATE SET verified = EXCLUDED.verified
		 RETURNING id`, ProtectedAccountEmails[0], verified).Scan(&uid); err != nil {
		t.Fatalf("ensure protected user: %v", err)
	}
	return uid
}

// markedUserIDs returns the ids of every account the purge would consider.
func markedUserIDs(t *testing.T, ctx context.Context, p *pgxpool.Pool) map[string]bool {
	t.Helper()
	rows, err := p.Query(ctx, `SELECT id FROM users WHERE is_test`)
	if err != nil {
		t.Fatalf("list marked users: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan marked id: %v", err)
		}
		out[id] = true
	}
	return out
}

func emailInReport(refs []TestAccountRef, email string) bool {
	for _, r := range refs {
		if r.Email == email {
			return true
		}
	}
	return false
}

func blockedReport(blocked []TestAccountBlocked, email string) (string, bool) {
	for _, b := range blocked {
		if b.Email == email {
			return b.Reason, true
		}
	}
	return "", false
}

// TestPurgeTestAccountsRemovesOnlyMarkedAccounts is the primary guard: the
// marker decides, the email shape does not, and the report NAMES what went.
func TestPurgeTestAccountsRemovesOnlyMarkedAccounts(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Drain leftover marked accounts from earlier tests so the counts below
	// describe this test's fixtures exactly.
	if _, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{}); err != nil {
		t.Fatalf("pre-purge drain: %v", err)
	}

	// VERIFIED on purpose: every account in the production incident was
	// verified, which is exactly why the unverified sweep could not touch them.
	markedEmail := "jfqa-" + md5Rand() + "@example.com"
	markedID, markedAcc := createMarkedUser(t, ctx, p, markedEmail, true, true)

	// A LOOKALIKE: the same email shape, verified, unmarked. It must survive —
	// this is the case that reds if the is_test predicate is dropped.
	lookalikeEmail := "jfqa-" + md5Rand() + "@example.com"
	lookalikeID, lookalikeAcc := createMarkedUser(t, ctx, p, lookalikeEmail, false, true)
	defer cleanupUsers(t, ctx, p, lookalikeID)

	ordinaryID, _ := createMarkedUser(t, ctx, p, "customer-"+md5Rand()+"@example.test", false, true)
	defer cleanupUsers(t, ctx, p, ordinaryID)

	// A DRY RUN must name the account and change nothing.
	dry, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !emailInReport(dry.Removed, markedEmail) {
		t.Errorf("dry run did not name the marked account %q: %+v", markedEmail, dry.Removed)
	}
	if dry.Scanned < 1 {
		t.Errorf("dry run scanned %d marked accounts, want at least 1", dry.Scanned)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, markedID); n != 1 {
		t.Fatalf("the dry run DELETED the marked account (count=%d, want 1) — a dry run must commit nothing", n)
	}

	// The real run removes it, with its account. The lookalike and the ordinary
	// customer are untouched.
	res, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !emailInReport(res.Removed, markedEmail) {
		t.Errorf("the purge did not report removing %q: %+v", markedEmail, res.Removed)
	}
	for _, r := range res.Removed {
		if r.ID == "" || r.Email == "" {
			t.Errorf("the report row does not name the account (id=%q email=%q) — the report must NAME what it removed", r.ID, r.Email)
		}
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, markedID); n != 0 {
		t.Errorf("the marked account survived (count=%d, want 0)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE id = $1`, markedAcc); n != 0 {
		t.Errorf("the marked account's storefront survived (count=%d, want 0)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, lookalikeID); n != 1 {
		t.Errorf("the UNMARKED lookalike with the same email shape was deleted (count=%d, want 1) — "+
			"the purge must go by the marker, never by the address shape", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE id = $1`, lookalikeAcc); n != 1 {
		t.Errorf("the lookalike's storefront was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, ordinaryID); n != 1 {
		t.Errorf("an ordinary customer account was deleted (count=%d, want 1)", n)
	}

	// IDEMPOTENT: a second run finds nothing and says so.
	again, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{})
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if len(again.Removed) != 0 || again.Scanned != 0 {
		t.Errorf("second run was not a no-op: scanned=%d removed=%v", again.Scanned, again.Removed)
	}
}

// TestPurgeTestAccountsRefusesAnUnmarkedAccount pins the refusal the card
// requires: a purge asked to remove a specific real account must say NO, not
// "0 removed".
func TestPurgeTestAccountsRefusesAnUnmarkedAccount(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Same email shape as the incident's accounts, no marker.
	email := "jfqa-" + md5Rand() + "@maxxspace.com"
	uid, _ := createMarkedUser(t, ctx, p, email, false, true)
	defer cleanupUsers(t, ctx, p, uid)

	_, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{Email: email})
	if !errors.Is(err, ErrNotATestAccount) {
		t.Fatalf("purge by email on an UNMARKED account: err = %v, want ErrNotATestAccount", err)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, uid); n != 1 {
		t.Errorf("the unmarked account was deleted by a refused run (count=%d, want 1)", n)
	}
}

// TestPurgeTestAccountsBlocksMarkedAccountsWithSalesOrSubscriptions pins both
// RESTRICT guards, and the fact that one blocked candidate does not stop the
// others (the sweep's per-user transaction scope).
func TestPurgeTestAccountsBlocksMarkedAccountsWithSalesOrSubscriptions(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)
	if _, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{}); err != nil {
		t.Fatalf("pre-purge drain: %v", err)
	}

	// Marked account holding a sale.
	saleEmail := "jfqa-sale-" + md5Rand() + "@example.com"
	saleID, saleAcc := createMarkedUser(t, ctx, p, saleEmail, true, true)
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, subtotal_sen, total_sen)
		 VALUES ($1, 'it-' || md5(random()::text), '{}'::jsonb, '[]'::jsonb, 100, 100)`, saleAcc); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	defer cleanupUsersWithSales(t, ctx, p, saleID)

	// Marked account holding a subscription.
	subEmail := "jfqa-sub-" + md5Rand() + "@example.com"
	subID, subAcc := createMarkedUser(t, ctx, p, subEmail, true, true)
	var prodID string
	if err := p.QueryRow(ctx,
		`INSERT INTO products (account_id, name, price_sen, type, billing_period)
		 VALUES ($1, 'it-prod', 100, 'subscription', 'month') RETURNING id`, subAcc).Scan(&prodID); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO subscriptions (account_id, client_email, product_id, chip_token_purchase_id, period, amount_sen, next_charge_at)
		 VALUES ($1, 'buyer@example.test', $2, 'it-' || md5(random()::text), 'month', 100, now() + interval '30 days')`,
		subAcc, prodID); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	defer func() {
		// subscriptions.account_id is ON DELETE RESTRICT, so it goes first.
		_, _ = p.Exec(context.Background(), `DELETE FROM subscriptions WHERE account_id = $1`, subAcc)
		cleanupUsers(t, ctx, p, subID)
	}()

	// A third marked account with nothing protected: it must still go.
	freeEmail := "jfqa-free-" + md5Rand() + "@example.com"
	freeID, _ := createMarkedUser(t, ctx, p, freeEmail, true, true)
	defer cleanupUsers(t, ctx, p, freeID)

	res, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if reason, ok := blockedReport(res.Blocked, saleEmail); !ok {
		t.Errorf("the sale-holding marked account was not reported as BLOCKED: %+v", res.Blocked)
	} else if reason != BlockReasonSales {
		t.Errorf("blocked reason for %q = %q, want %q", saleEmail, reason, BlockReasonSales)
	}
	if reason, ok := blockedReport(res.Blocked, subEmail); !ok {
		t.Errorf("the subscription-holding marked account was not reported as BLOCKED: %+v", res.Blocked)
	} else if reason != BlockReasonSubscriptions {
		t.Errorf("blocked reason for %q = %q, want %q", subEmail, reason, BlockReasonSubscriptions)
	}
	if emailInReport(res.Removed, saleEmail) || emailInReport(res.Removed, subEmail) {
		t.Errorf("a blocked account was reported as removed: %+v", res.Removed)
	}
	if !emailInReport(res.Removed, freeEmail) {
		t.Errorf("the unprotected marked account was not removed — one blocked candidate must not stop "+
			"the others: %+v (blocked %+v)", res.Removed, res.Blocked)
	}

	// The blocked rows are exactly as they were, sale and subscription intact.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, saleID); n != 1 {
		t.Errorf("the sale-holding marked account was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM sales WHERE account_id = $1`, saleAcc); n != 1 {
		t.Errorf("the sale was deleted (count=%d, want 1) — merchant data must never be removed by the purge", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, subID); n != 1 {
		t.Errorf("the subscription-holding marked account was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM subscriptions WHERE account_id = $1`, subAcc); n != 1 {
		t.Errorf("the subscription was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, freeID); n != 0 {
		t.Errorf("the unprotected marked account survived (count=%d, want 0)", n)
	}
}

// TestPurgeTestAccountsNeverRemovesAProtectedAccount is Wan's mandatory
// safeguard for the test purge, WITH its bite proof.
//
// The designated account is marked is_test directly in SQL here (simulating a
// marking channel that was bypassed, or a mistake): the purge must still refuse
// it, because the protection is a property of the account and not a filter the
// caller can talk its way past.
func TestPurgeTestAccountsNeverRemovesAProtectedAccount(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	protectedID := ensureProtectedUser(t, ctx, p, false)
	if _, err := p.Exec(ctx, `UPDATE users SET is_test = true WHERE id = $1`, protectedID); err != nil {
		t.Fatalf("mark protected user as test: %v", err)
	}
	defer func() {
		_, _ = p.Exec(context.Background(), `UPDATE users SET is_test = false WHERE id = $1`, protectedID)
	}()

	if _, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{Email: ProtectedAccountEmails[0]}); !errors.Is(err, ErrProtectedAccount) {
		t.Fatalf("purge by email on the protected account: err = %v, want ErrProtectedAccount", err)
	}

	res, err := store.PurgeTestAccounts(ctx, TestPurgeOptions{})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if emailInReport(res.Removed, ProtectedAccountEmails[0]) {
		t.Errorf("the protected account was reported as removed: %+v", res.Removed)
	}
	if !markedUserIDs(t, ctx, p)[protectedID] {
		t.Errorf("the protected account was DELETED by the test purge — the designated login must survive " +
			"every purge (Wan, 2026-10-15)")
	}

	// BITE PROOF: neutralise the safeguard (an empty protected list) and the
	// SAME purge must delete the SAME row. Without this, a green above could be
	// the fixture's doing rather than the safeguard's.
	neutralised := &UserStore{pool: p, protectedOverride: &[]string{}}
	bite, err := neutralised.PurgeTestAccounts(ctx, TestPurgeOptions{Email: ProtectedAccountEmails[0]})
	if err != nil {
		t.Fatalf("bite-proof purge: %v", err)
	}
	if !emailInReport(bite.Removed, ProtectedAccountEmails[0]) {
		t.Fatalf("BITE PROOF FAILED: with the protected list neutralised the purge still did not remove the "+
			"protected account (%+v) — the assertion above proves nothing about the safeguard", bite.Removed)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, protectedID); n != 0 {
		t.Errorf("bite-proof run did not delete the row (count=%d, want 0)", n)
	}
}

// TestUnverifiedPurgeNeverRemovesAProtectedAccount is Wan's safeguard for the
// EXISTING sweep: the designated login is verified=false on production, so
// `purge_unverified_accounts` matches it today. It must survive, and the bite
// proof shows the exclusion is what keeps it alive.
func TestUnverifiedPurgeNeverRemovesAProtectedAccount(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	protectedID := ensureProtectedUser(t, ctx, p, false)
	if _, err := p.Exec(ctx,
		`UPDATE users SET verified = false, is_test = false, created_at = now() - interval '10 days' WHERE id = $1`,
		protectedID); err != nil {
		t.Fatalf("backdate protected user: %v", err)
	}

	// A deletable control in the same sweep: it going is what a "the sweep did
	// nothing at all" regression breaks.
	controlEmail := "sweep-control-" + md5Rand() + "@example.test"
	controlID, _ := createMarkedUser(t, ctx, p, controlEmail, false, false)
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, controlID); err != nil {
		t.Fatalf("backdate control: %v", err)
	}
	defer cleanupUsers(t, ctx, p, controlID)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	// The backlog count must not include a row the sweep can never touch.
	n, err := store.CountUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountUnverifiedOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("unverified backlog count = %d, want 1 (the control only; the protected account must not be counted)", n)
	}

	res, err := store.DeleteUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteUnverifiedOlderThan: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("sweep deleted %d accounts, want 1 (the control)", res.Deleted)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, protectedID); n != 1 {
		t.Errorf("the PROTECTED account was deleted by the unverified sweep (count=%d, want 1) — it is "+
			"verified=false on production and must survive every purge", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, controlID); n != 0 {
		t.Errorf("the control unverified account survived (count=%d, want 0)", n)
	}

	// BITE PROOF: neutralise the safeguard; the sweep must then delete it.
	neutralised := &UserStore{pool: p, protectedOverride: &[]string{}}
	if _, err := neutralised.DeleteUnverifiedOlderThan(ctx, cutoff); err != nil {
		t.Fatalf("bite-proof sweep: %v", err)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, protectedID); n != 0 {
		t.Fatalf("BITE PROOF FAILED: with the protected list neutralised the sweep still did not delete the "+
			"protected account (count=%d) — the assertion above proves nothing about the safeguard", n)
	}
}

// TestCreateWithTestMarkerRefusesAProtectedAddress pins the marking channel's
// lower bound: the store itself refuses to write is_test on a protected address,
// whatever caller asks.
func TestCreateWithTestMarkerRefusesAProtectedAddress(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	_, err := store.CreateWithTestMarker(ctx, ProtectedAccountEmails[0], "it", true)
	if !errors.Is(err, ErrProtectedAccount) {
		t.Fatalf("marking the protected address as a test account: err = %v, want ErrProtectedAccount", err)
	}

	// An ordinary address IS markable, and the plain Create path stays unmarked.
	email := "jfqa-" + md5Rand() + "@example.com"
	u, err := store.CreateWithTestMarker(ctx, email, "it", true)
	if err != nil {
		t.Fatalf("create marked account: %v", err)
	}
	defer cleanupUsers(t, ctx, p, u.ID)
	if !markedUserIDs(t, ctx, p)[u.ID] {
		t.Errorf("CreateWithTestMarker(isTest=true) did not persist the marker")
	}

	plain := "customer-" + md5Rand() + "@example.test"
	u2, err := store.Create(ctx, plain, "it")
	if err != nil {
		t.Fatalf("plain create: %v", err)
	}
	defer cleanupUsers(t, ctx, p, u2.ID)
	if markedUserIDs(t, ctx, p)[u2.ID] {
		t.Errorf("the ordinary Create path wrote is_test=true — the marker must only come from the QA channel")
	}
}
