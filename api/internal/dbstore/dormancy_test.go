package dbstore

// Integration guards for the phase-1 dormancy report (card t_94e5cca3).
//
// Skipped when JOMFORM_TEST_DATABASE_URL is unset. Each test runs in this
// package's scratch schema (testdb.Open) and marks every row it seeds with
// @dormancy.test, so cleanup deletes its own rows and a -count=3 run starts clean —
// without TRUNCATEing tables that hold migration seed data (see below).
//
// WHAT IS PINNED HERE, AND WHY EACH ONE IS A SQL PROPERTY RATHER THAN A POLICY ONE:
//
//  1. The activity metric is ACCOUNT-scoped. Production measured 761 of 3,151
//     events (24%) with a NULL user_id; a rule keyed on events.user_id would
//     report a live account as dormant. The fixture writes exactly that shape.
//  2. The nudge's OWN audit rows must not count as merchant activity. If they did,
//     the first nudge would reset the account to "active today", it would drop out
//     of every bucket, and it could never be nudged again — the job would erase its
//     own subject.
//  3. A submission inside the window is reported (the policy layer turns it into an
//     exclusion; the exclusion itself is tested in internal/dormancy).
//  4. The report is ONE statement with a hard LIMIT — pinned by counting the Query
//     calls and by asking for more rows than the cap.
//  5. `sales` is not scanned once per account — pinned by counting the plan's
//     `sales` relation nodes, and by measuring the whole statement on a seeded
//     10,000-account fixture.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dormancyEmailSuffix marks every row this file seeds, so cleanup deletes ITS OWN
// rows and nothing else.
//
// WHY NOT testdb.Reset(users, …): TRUNCATE users CASCADE also removes the
// migration-seeded system identities (templates@system.jomform and its template
// gallery), and the tests that assert on them then fail with "system templates
// account missing". Those rows are DATABASE SEED DATA, not this test's fixtures —
// the report cannot disturb them, so the fixture is marked and deleted instead.
const dormancyEmailSuffix = "@dormancy.test"

// cleanupDormancyFixtures removes exactly the rows this file created. The delete
// order follows the foreign keys: sales RESTRICTs an account delete, the rest
// cascade.
func cleanupDormancyFixtures(t *testing.T, ctx context.Context, p *pgxpool.Pool) {
	t.Helper()
	marked := `(SELECT id FROM users WHERE email LIKE '%` + dormancyEmailSuffix + `')`
	stmts := []string{
		`DELETE FROM events WHERE account_id IN (SELECT a.id FROM accounts a WHERE a.owner_user_id IN ` + marked + `)`,
		`DELETE FROM subscriptions WHERE account_id IN (SELECT a.id FROM accounts a WHERE a.owner_user_id IN ` + marked + `)`,
		`DELETE FROM sales WHERE account_id IN (SELECT a.id FROM accounts a WHERE a.owner_user_id IN ` + marked + `)`,
		`DELETE FROM submissions WHERE account_id IN (SELECT a.id FROM accounts a WHERE a.owner_user_id IN ` + marked + `)`,
		`DELETE FROM forms WHERE account_id IN (SELECT a.id FROM accounts a WHERE a.owner_user_id IN ` + marked + `)`,
		`DELETE FROM accounts WHERE owner_user_id IN ` + marked,
		`DELETE FROM workspaces WHERE owner_user_id IN ` + marked,
		`DELETE FROM users WHERE email LIKE '%` + dormancyEmailSuffix + `'`,
	}
	for _, s := range stmts {
		if _, err := p.Exec(ctx, s); err != nil {
			t.Fatalf("cleanup dormancy fixtures: %v", err)
		}
	}
}

// testPoolWithDormancyCleanup opens the scratch schema and registers the EXACT
// cleanup, run both before (a previous aborted run) and after.
func testPoolWithDormancyCleanup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p := testPool(t)
	ctx := context.Background()
	cleanupDormancyFixtures(t, ctx, p)
	t.Cleanup(func() { cleanupDormancyFixtures(t, context.Background(), p) })
	return p
}

// seedAccount inserts a user + account (optionally linked to a workspace) and
// returns the account id and user id. created is the account's creation time.
func seedDormancyAccount(t *testing.T, ctx context.Context, p *pgxpool.Pool, email, slug, created string, workspaceID any) (accountID, userID string) {
	t.Helper()
	if err := p.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, created_at) VALUES ($1,'x',$2::timestamptz)
		RETURNING id::text`, email, created).Scan(&userID); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	if err := p.QueryRow(ctx, `
		INSERT INTO accounts (owner_user_id, name, slug, workspace_id, created_at)
		VALUES ($1::uuid,'acc',$2,$3::uuid,$4::timestamptz)
		RETURNING id::text`, userID, slug, workspaceID, created).Scan(&accountID); err != nil {
		t.Fatalf("seed account %s: %v", slug, err)
	}
	return accountID, userID
}

// findRow returns the report row for one account id.
func findRow(rows []DormancyRow, id string) (DormancyRow, bool) {
	for _, r := range rows {
		if r.AccountID == id {
			return r, true
		}
	}
	return DormancyRow{}, false
}

// TestDormancyReportCountsEventsWithNullUserID is the 761-row production shape: a
// system/API-key action has no user, and it must still keep the ACCOUNT awake.
func TestDormancyReportCountsEventsWithNullUserID(t *testing.T) {
	ctx := context.Background()
	p := testPoolWithDormancyCleanup(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// The account was created 200 days ago and every USER-bound action is 200 days
	// old — but an API-key/system event with user_id NULL arrived 2 days ago.
	acc, _ := seedDormancyAccount(t, ctx, p, "null-uid@dormancy.test", "it-null-uid", now.Add(-200*24*time.Hour).Format(time.RFC3339), nil)
	if _, err := p.Exec(ctx, `
		INSERT INTO events (account_id, user_id, event_name, created_at)
		VALUES ($1::uuid, NULL, 'api.request', $2::timestamptz)`,
		acc, now.Add(-2*24*time.Hour)); err != nil {
		t.Fatalf("insert NULL-user event: %v", err)
	}

	store := NewDormancyStore(p)
	rows, err := store.DormancyReport(ctx, DormancyReportOptions{Now: now})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	got, ok := findRow(rows, acc)
	if !ok {
		t.Fatalf("account %s missing from the report", acc)
	}
	// The metric must be the NULL-user event, not the 200-day-old creation.
	if d := now.Sub(got.LastActivity); d > 3*24*time.Hour {
		t.Fatalf("last_activity = %s, %v before now — the NULL-user_id event was NOT counted "+
			"(the production shape: 761 of 3,151 events have no user)", got.LastActivity, d)
	}
}

// TestDormancyNudgeEventsDoNotCountAsActivity is the self-erasing-nudge guard.
func TestDormancyNudgeEventsDoNotCountAsActivity(t *testing.T) {
	ctx := context.Background()
	p := testPoolWithDormancyCleanup(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	acc, user := seedDormancyAccount(t, ctx, p, "nudged@dormancy.test", "it-nudged", now.Add(-200*24*time.Hour).Format(time.RFC3339), nil)
	// The nudge was sent yesterday: its audit row is an EVENT on this account.
	if err := NewDormancyStore(p).RecordDormancyNudge(ctx, acc, user, "90", "nudged@dormancy.test"); err != nil {
		t.Fatalf("record nudge: %v", err)
	}

	rows, err := NewDormancyStore(p).DormancyReport(ctx, DormancyReportOptions{Now: now})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	got, ok := findRow(rows, acc)
	if !ok {
		t.Fatalf("account %s missing from the report", acc)
	}
	if d := now.Sub(got.LastActivity); d < 190*24*time.Hour {
		t.Fatalf("last_activity = %s (%v before now) — the dormancy.nudge_sent row counted as "+
			"merchant activity, so the nudge erases its own subject and can never repeat", got.LastActivity, d)
	}
	if got.LastNudgeAt == nil {
		t.Fatal("last_nudge_at is NULL after recording a nudge — the 90-day limit would not hold")
	}
}

// TestDormancyReportSeparatesRecentFromOldSubmissions pins the 90-day window on
// one account holding both an old and a recent submission.
func TestDormancyReportSeparatesRecentFromOldSubmissions(t *testing.T) {
	ctx := context.Background()
	p := testPoolWithDormancyCleanup(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	acc, _ := seedDormancyAccount(t, ctx, p, "buyers@dormancy.test", "it-buyers", now.Add(-300*24*time.Hour).Format(time.RFC3339), nil)
	var formID string
	if err := p.QueryRow(ctx, `
		INSERT INTO forms (account_id, slug, title, status)
		VALUES ($1::uuid,'it-buyers-form','f','published') RETURNING id::text`, acc).Scan(&formID); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	for _, ago := range []int{200, 45, 3} { // one outside the window, two inside
		if _, err := p.Exec(ctx, `
			INSERT INTO submissions (form_id, account_id, created_at) VALUES ($1::uuid,$2::uuid,$3::timestamptz)`,
			formID, acc, now.Add(-time.Duration(ago)*24*time.Hour)); err != nil {
			t.Fatalf("seed submission: %v", err)
		}
	}

	rows, err := NewDormancyStore(p).DormancyReport(ctx, DormancyReportOptions{Now: now})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	got, _ := findRow(rows, acc)
	if got.Submissions90d != 2 {
		t.Errorf("submissions_90d = %d, want 2 (the 200-day-old one is outside the window)", got.Submissions90d)
	}
	if got.FormsPublished != 1 {
		t.Errorf("forms_published = %d, want 1", got.FormsPublished)
	}
	// The submissions are also the account's freshest activity.
	if d := now.Sub(got.LastActivity); d > 4*24*time.Hour {
		t.Errorf("last_activity = %s, want the 3-day-old submission", got.LastActivity)
	}
}

// countingQuerier counts the statements a store issues.
type countingQuerier struct {
	inner *pgxpool.Pool
	n     int
}

func (c *countingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.n++
	return c.inner.Query(ctx, sql, args...)
}

// TestDormancyReportIsOneStatementWithAHardLimit is the "no N+1" guard at its most
// direct: however many rows come back, the store issues exactly one query, and the
// row cap holds.
func TestDormancyReportIsOneStatementWithAHardLimit(t *testing.T) {
	ctx := context.Background()
	p := testPoolWithDormancyCleanup(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 40; i++ {
		seedDormancyAccount(t, ctx, p, "multi-"+string(rune('a'+i%26))+string(rune('0'+i/26))+"@example.test",
			"it-multi-"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			now.Add(-time.Duration(30+i)*24*time.Hour).Format(time.RFC3339), nil)
	}

	cq := &countingQuerier{inner: p}
	store := &DormancyStore{pool: p, q: cq}
	rows, err := store.DormancyReport(ctx, DormancyReportOptions{Now: now, Limit: 10})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if cq.n != 1 {
		t.Fatalf("the report issued %d statements for %d rows — it must be ONE (no N+1)", cq.n, len(rows))
	}
	if len(rows) != 10 {
		t.Fatalf("limit=10 returned %d rows — the LIMIT is not applied", len(rows))
	}

	// The cap is hard: asking for far more than the maximum returns at most the max.
	cq.n = 0
	rows, err = store.DormancyReport(ctx, DormancyReportOptions{Now: now, Limit: 100000})
	if err != nil {
		t.Fatalf("report (huge limit): %v", err)
	}
	if len(rows) > DormancyReportLimitMax {
		t.Fatalf("limit=100000 returned %d rows, over the hard cap %d", len(rows), DormancyReportLimitMax)
	}
}

// TestDormancyReportScansSalesOnceAndIsFastAt10kAccounts states the plan and the
// runtime the card asks for.
func TestDormancyReportScansSalesOnceAndIsFastAt10kAccounts(t *testing.T) {
	ctx := context.Background()
	p := testPoolWithDormancyCleanup(t)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// 10,000 accounts, each with a user and 3 events; every 25th gets a form and a
	// sale, every 10th a submission. Set-based inserts, so the seed itself is fast.
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := p.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	mustExec(`
		INSERT INTO users (email, password_hash, created_at)
		SELECT 'seed-'||i||'@dormancy.test','x', now() - (i||' days')::interval
		FROM generate_series(1,10000) i`)
	mustExec(`
		INSERT INTO accounts (owner_user_id, name, slug, created_at)
		SELECT u.id,'seed','seed-acc-'||u.id, u.created_at
		FROM users u WHERE u.email LIKE 'seed-%@dormancy.test'`)
	mustExec(`
		INSERT INTO events (account_id, user_id, event_name, created_at)
		SELECT a.id, u.id, 'form.viewed', a.created_at + (k||' hours')::interval
		FROM accounts a JOIN users u ON u.id = a.owner_user_id,
		     generate_series(1,3) k
		WHERE a.slug LIKE 'seed-acc-%'`)
	mustExec(`
		INSERT INTO forms (account_id, slug, title, status)
		SELECT a.id,'seed-form-'||a.id,'f','published'
		FROM accounts a WHERE a.slug LIKE 'seed-acc-%'
		  AND ('x'||substr(md5(a.id::text),1,4))::bit(16)::int % 25 = 0`)
	mustExec(`
		INSERT INTO sales (account_id, ref_code, client, items, total_sen)
		SELECT f.account_id,'seed-sale-'||f.id,'{}','[]',100
		FROM forms f WHERE f.slug LIKE 'seed-form-%'`)
	mustExec(`
		INSERT INTO submissions (form_id, account_id, created_at)
		SELECT f.id, f.account_id, now() - interval '1 day'
		FROM forms f WHERE f.slug LIKE 'seed-form-%'
		  AND ('x'||substr(md5(f.id::text),1,4))::bit(16)::int % 2 = 0`)

	// (a) The plan: exactly ONE relation node named `sales`, whatever the planner
	// chooses (seq scan / index scan would each be one node; a per-account lateral
	// would be one node per account and the count would explode).
	planRows, err := p.Query(ctx, "EXPLAIN (FORMAT JSON) "+dormancyReportSQL, now.Add(-90*24*time.Hour), 200)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []byte
	for planRows.Next() {
		if err := planRows.Scan(&plan); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
	}
	planRows.Close()
	if plan == nil {
		t.Fatal("EXPLAIN returned no plan")
	}
	if got := strings.Count(string(plan), `"Relation Name": "sales"`); got != 1 {
		t.Errorf("the plan scans `sales` %d times, want 1 — the report must aggregate it once, not per account", got)
	}
	for _, rel := range []string{"events", "forms", "submissions", "users", "accounts"} {
		if got := strings.Count(string(plan), `"Relation Name": "`+rel+`"`); got != 1 {
			t.Errorf("the plan scans %q %d times, want 1", rel, got)
		}
	}

	// (b) The runtime on those 10,000 accounts. The bound is deliberately loose — it
	// is a tripwire for an accidental N+1 (which would be thousands of round trips),
	// not a performance target.
	start := time.Now()
	rows, err := NewDormancyStore(p).DormancyReport(ctx, DormancyReportOptions{Now: now, Limit: 200})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if len(rows) != 200 {
		t.Fatalf("got %d rows, want 200", len(rows))
	}
	if elapsed > 20*time.Second {
		t.Errorf("report took %v on 10,000 accounts — that is the shape of a per-row query", elapsed)
	}
	// Most dormant first: the seed's creation times run from 1 to 10,000 days ago,
	// and every account has events only on its creation day.
	if rows[0].LastActivity.After(rows[len(rows)-1].LastActivity) {
		t.Errorf("rows are not ordered most-dormant-first: first=%s last=%s", rows[0].LastActivity, rows[len(rows)-1].LastActivity)
	}
	t.Logf("dormancy report on %d seeded accounts: %v for %d rows (one statement)", 10000, elapsed, len(rows))
}
