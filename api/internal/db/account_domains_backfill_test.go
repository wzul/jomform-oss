package db

// Card t_669578ef.
//
// `account_domains` (000007) was created without carrying `accounts.custom_domain`
// across, so a legacy domain that is VERIFIED and serving is invisible in Settings:
// both readers of the domain list key off the new table only. This test pins the
// backfill (000074) and, just as importantly, the invariant that would have caught
// the gap on day one.

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAccountDomainsBackfillCarriesLegacyDomains is the card's required bite:
// a fixture with a legacy-ONLY domain must appear in the account_domains table
// after the migration. Run against the pre-migration schema the row is missing —
// which is the defect — so the assertion below is the thing that changes.
func TestAccountDomainsBackfillCarriesLegacyDomains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping account_domains backfill test")
	}

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	// 1. Current schema through the real runner, exactly as the API boots.
	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	// 2. Reproduce the PRE-000074 world: undo the backfill, then seed the legacy
	//    column the way prod holds it. Seeding BEFORE the migration and then
	//    re-applying it is what makes this a backfill test rather than a test of
	//    a table that happened to be empty.
	if _, err := pool.Exec(ctx, migrationFile(t, "000074_backfill_account_domains.down.sql")); err != nil {
		t.Fatalf("000074 down did not apply: %v", err)
	}

	userID := seedUser(t, ctx, pool)
	// A verified legacy domain, and one unverified — the two states the copy must
	// carry across distinctly.
	verifiedAccount := seedAccount(t, ctx, pool, userID)
	_, err = pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=true, custom_domain_verified_at=now()
		  WHERE id=$1`, verifiedAccount, "legacy-verified-"+name+".example.test")
	if err != nil {
		t.Fatalf("seed verified legacy domain: %v", err)
	}
	unverifiedAccount := seedAccount(t, ctx, pool, userID)
	_, err = pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=false WHERE id=$1`,
		unverifiedAccount, "legacy-pending-"+name+".example.test")
	if err != nil {
		t.Fatalf("seed unverified legacy domain: %v", err)
	}
	// An account with no legacy domain at all: the migration must leave it alone.
	emptyAccount := seedAccount(t, ctx, pool, userID)

	// The premise of this test: before the backfill BOTH domains are absent from
	// account_domains. Without this, a green result below could mean the migration
	// was never needed.
	if n := countAccountDomains(t, ctx, pool, verifiedAccount, unverifiedAccount, emptyAccount); n != 0 {
		t.Fatalf("premise broken: %d account_domains rows exist before the backfill, so the "+
			"legacy rows were already reachable and this test proves nothing", n)
	}

	// 3. Apply the backfill.
	if _, err := pool.Exec(ctx, migrationFile(t, "000074_backfill_account_domains.up.sql")); err != nil {
		t.Fatalf("000074 up did not apply: %v", err)
	}

	// 4. The verified domain is now LISTED, carrying its verification.
	var domain string
	var verified bool
	var verifiedAt *time.Time
	var provider string
	if err := pool.QueryRow(ctx,
		`SELECT domain, verified, verified_at, provider FROM account_domains WHERE account_id=$1`,
		verifiedAccount).Scan(&domain, &verified, &verifiedAt, &provider); err != nil {
		t.Fatalf("the verified legacy domain is still invisible after the backfill: %v", err)
	}
	if want := "legacy-verified-" + name + ".example.test"; domain != want {
		t.Errorf("backfilled domain = %q, want %q", domain, want)
	}
	if !verified {
		t.Error("a VERIFIED legacy domain came across as unverified — that turns a live domain " +
			"into a pending one, which is the defect in a different hat")
	}
	if verifiedAt == nil {
		t.Error("custom_domain_verified_at was not carried across")
	}
	if provider != "cname" {
		t.Errorf("provider = %q, want cname (the legacy single-domain behaviour)", provider)
	}

	// The unverified one came across too, and stayed unverified.
	if err := pool.QueryRow(ctx,
		`SELECT verified FROM account_domains WHERE account_id=$1`,
		unverifiedAccount).Scan(&verified); err != nil {
		t.Fatalf("the unverified legacy domain is still invisible after the backfill: %v", err)
	}
	if verified {
		t.Error("an UNVERIFIED legacy domain came across as verified — that would hand out a " +
			"domain the merchant never proved they own")
	}

	// The account with no legacy domain gained nothing.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM account_domains WHERE account_id=$1`, emptyAccount).Scan(&n); err != nil {
		t.Fatalf("count for the domain-less account: %v", err)
	}
	if n != 0 {
		t.Errorf("an account with no custom_domain gained %d account_domains rows", n)
	}

	// 5. Idempotent: a second pass (what a re-run on prod would do) adds nothing.
	if _, err := pool.Exec(ctx, migrationFile(t, "000074_backfill_account_domains.up.sql")); err != nil {
		t.Fatalf("re-applying 000074 must be safe: %v", err)
	}
	if got := countAccountDomains(t, ctx, pool, verifiedAccount, unverifiedAccount, emptyAccount); got != 2 {
		t.Errorf("second pass produced %d rows, want 2 — the migration is not idempotent", got)
	}

	// 6. The INVARIANT the card asks for, asserted directly: after the backfill no
	//    account holds a legacy domain that is missing from account_domains.
	if orphan := countOrphanLegacyDomains(t, ctx, pool); orphan != 0 {
		t.Errorf("%d account(s) still hold a custom_domain absent from account_domains; the "+
			"backfill skipped them", orphan)
	}
}

// TestAccountDomainsBackfillLeavesReAddedDomainsAlone is the conflict arm. An
// account that already re-added its legacy domain under the new table must not
// abort the migration, and must not end up with two rows.
func TestAccountDomainsBackfillLeavesReAddedDomainsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping account_domains conflict test")
	}

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationFile(t, "000074_backfill_account_domains.down.sql")); err != nil {
		t.Fatalf("000074 down did not apply: %v", err)
	}

	userID := seedUser(t, ctx, pool)
	accountID := seedAccount(t, ctx, pool, userID)
	shared := "readded-" + name + ".example.test"
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET custom_domain=$2, custom_domain_verified=true WHERE id=$1`,
		accountID, shared); err != nil {
		t.Fatalf("seed legacy domain: %v", err)
	}
	// The merchant re-added the SAME domain through the new UI, and re-verified it
	// there (so the states differ — this is exactly the case that must not abort).
	if _, err := pool.Exec(ctx,
		`INSERT INTO account_domains (account_id, domain, provider, verified,
		   tls_mode, tls_status, auto_cname)
		 VALUES ($1, $2, 'cname', false, 'acme', 'pending', true)`,
		accountID, shared); err != nil {
		t.Fatalf("seed re-added domain: %v", err)
	}

	// The migration must NOT abort on the unique index.
	if _, err := pool.Exec(ctx, migrationFile(t, "000074_backfill_account_domains.up.sql")); err != nil {
		t.Fatalf("000074 aborted on an account that had already re-added its legacy domain — "+
			"the ON CONFLICT (domain) arm is mandatory for this case: %v", err)
	}

	// Exactly one row, and it is the MERCHANT's (their TLS settings survive; the
	// backfill did not overwrite it with the legacy state).
	var n int
	var verified bool
	var tlsMode string
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM account_domains WHERE account_id=$1`, accountID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("account has %d account_domains rows, want 1 — the backfill duplicated a "+
			"domain that was already claimed", n)
	}
	if err := pool.QueryRow(ctx,
		`SELECT verified, tls_mode FROM account_domains WHERE account_id=$1`,
		accountID).Scan(&verified, &tlsMode); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if verified || tlsMode != "acme" {
		t.Error("the backfill OVERWROTE a claim the merchant made through the new UI — " +
			"their verification state and TLS settings must survive")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func seedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified)
		 VALUES ('domains-' || md5(random()::text) || '@example.test', 'x', true)
		 RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func seedAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerUserID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-domains-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id`, ownerUserID).Scan(&id); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return id
}

func countAccountDomains(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accounts ...string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM account_domains WHERE account_id = ANY($1)`, accounts).Scan(&n); err != nil {
		t.Fatalf("count account_domains: %v", err)
	}
	return n
}

// countOrphanLegacyDomains is the invariant the card asks to assert directly.
func countOrphanLegacyDomains(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM accounts a
		 WHERE a.custom_domain IS NOT NULL
		   AND a.custom_domain <> ''
		   AND NOT EXISTS (SELECT 1 FROM account_domains d WHERE d.domain = a.custom_domain)
	`).Scan(&n); err != nil {
		t.Fatalf("count orphan legacy domains: %v", err)
	}
	return n
}
