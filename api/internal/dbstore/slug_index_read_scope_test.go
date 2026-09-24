// Guards for the schema-level half of TestSlugUniquenessPerWorkspace.
//
// The read that asserts the per-workspace slug index exists used to be
// `SELECT COUNT(*) FROM pg_indexes WHERE tablename='forms' AND indexdef ILIKE
// '%workspace_id%' AND ...`. That shape has two defects, and both are pinned
// here so a later "simplification" back to it fails loudly:
//
//  1. `pg_indexes.indexdef` is pg_get_indexdef(i.oid) and the view has NO schema
//     filter, so the statement OID-opens every `forms` index in the whole
//     database — every concurrent lane's scratch schema included. Measured on
//     Postgres 18.3: the plan of the old form carries `pg_get_indexdef(i.oid)`
//     applied per index row, and the count comes back as the number of live
//     lanes instead of the one this run owns. That figure is the size of the
//     board, not a constant: 5 on a quiet shared database, 25 under load, 11
//     when this comment was last corrected. Do not read a number here as an
//     expected value — this run's read must return exactly 1 whatever it is.
//     Opening relations by OID across the whole database is what made this test
//     die with `could not open relation with OID <n> (SQLSTATE XX000)` when a
//     peer connection invalidated one of those OIDs underneath it.
//  2. It reads other lanes' rows, so the assertion is about the shared database
//     rather than about this package's own schema.
//
// TestSlugIndexReadDoesNotDeparseIndexDef pins (1) at the plan level,
// TestSlugIndexReadIsSchemaScoped pins (2) behaviourally, and
// TestSlugIndexReadRequiresAUniqueIndex pins the `AND x.indisunique` predicate
// the other two would let go. Each carries its own positive control, so a guard
// that silently stopped detecting anything would fail rather than pass vacuously.
package dbstore

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// wholeDatabaseSlugIndexProbe is the OLD shape, kept here only as a positive
// control: it is what the guards must be able to tell apart from the shipped
// read. Do not use it to assert anything about the schema.
const wholeDatabaseSlugIndexProbe = `
	SELECT COUNT(*) FROM pg_indexes
	WHERE tablename='forms' AND indexdef ILIKE '%workspace_id%' AND indexdef ILIKE '%slug%' AND indexdef ILIKE '%unique%'`

// explainPlan runs `EXPLAIN (VERBOSE, COSTS OFF)` and returns the plan text.
func explainPlan(t *testing.T, ctx context.Context, p interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, query string) string {
	t.Helper()
	rows, err := p.Query(ctx, "EXPLAIN (VERBOSE, COSTS OFF) "+query)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	return b.String()
}

// TestSlugIndexReadDoesNotDeparseIndexDef is the regression guard for the
// XX000 mechanism: the shipped read must not make Postgres open a relation by
// OID, which it does for every row of pg_indexes because that view's indexdef
// column is pg_get_indexdef(i.oid).
//
// A peer connection's DDL can only break this read through an OID-to-relation
// open, and removing that open is exactly what keeps a concurrent lane from
// taking this test down. The old shape's plan carries the call and the shipped
// one's must not — asserted in both directions so the guard cannot pass because
// it stopped parsing anything.
func TestSlugIndexReadDoesNotDeparseIndexDef(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	shipped := explainPlan(t, ctx, p, perWorkspaceSlugIndexRead)
	if strings.Contains(shipped, "pg_get_indexdef") {
		t.Errorf("the per-workspace slug index read calls pg_get_indexdef, i.e. it opens "+
			"relations by OID:\n%s\nAn OID opened from a catalogue read the catalogue can "+
			"invalidate underneath it is how a PEER lane's schema recreate takes this test "+
			"down with `could not open relation with OID` (SQLSTATE XX000). Read pg_class/"+
			"pg_index directly instead — see perWorkspaceSlugIndexRead.", shipped)
	}

	// Positive control: the old shape DOES carry the call, so the assertion
	// above is measuring something real rather than a string that never appears.
	old := explainPlan(t, ctx, p, wholeDatabaseSlugIndexProbe)
	if !strings.Contains(old, "pg_get_indexdef") {
		t.Fatalf("control: the whole-database pg_indexes probe no longer deparses indexdef, so "+
			"this guard can no longer tell the two shapes apart (plan:\n%s)", old)
	}
}

// TestSlugIndexReadIsSchemaScoped pins the other half of the defect: the read
// must resolve `forms` inside THIS run's schema, not anywhere in the database.
//
// It plants a decoy `forms` table carrying the same unique index shape in a
// schema that is not current_schema() — a temporary table, which is exactly the
// kind of object only a schema-scoped read can tell apart, because every
// session's pg_temp is invisible to every other session yet its rows sit in
// pg_class/pg_index like any other relation. The whole-database probe sees the
// decoy; the shipped read must not.
func TestSlugIndexReadIsSchemaScoped(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	// Inside a transaction so the TEMP decoy is rolled back: a temp table left
	// on a pooled connection would shadow `forms` for later tests in the
	// package that address it unqualified.
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE forms (id bigint PRIMARY KEY, workspace_id uuid, slug text)`); err != nil {
		t.Fatalf("create decoy: %v", err)
	}
	if _, err := tx.Exec(ctx, `CREATE UNIQUE INDEX decoy_workspace_slug_idx ON forms (workspace_id, slug)`); err != nil {
		t.Fatalf("index the decoy: %v", err)
	}

	var wholeDB int
	if err := tx.QueryRow(ctx, wholeDatabaseSlugIndexProbe).Scan(&wholeDB); err != nil {
		t.Fatalf("control probe: %v", err)
	}
	if wholeDB < 2 {
		t.Fatalf("control: the whole-database probe saw %d matching index(es) with a decoy "+
			"planted, so it is not sweeping the database and this guard would pass vacuously", wholeDB)
	}

	var got int
	if err := tx.QueryRow(ctx, perWorkspaceSlugIndexRead).Scan(&got); err != nil {
		t.Fatalf("read per-workspace slug index: %v", err)
	}
	if got != 1 {
		t.Errorf("the per-workspace slug index read matched %d index(es) with a decoy `forms` "+
			"table outside current_schema() planted, want exactly 1 (this run's own). The read "+
			"must resolve `forms` through current_schema() — a database-wide sweep counts other "+
			"lanes' schemas, and on the shared test database that is every concurrent run.", got)
	}
}

// perWorkspaceSlugIndexReadIsUniqueRequired is perWorkspaceSlugIndexRead with its
// `AND x.indisunique` predicate relaxed to `AND true`, i.e. the shape of the
// shipped read if the uniqueness requirement were dropped. It is the MUTATION the
// guard below exists to catch, written out so the two reads can be run side by
// side on the same schema in one test — a test cannot mutate a Go constant, so
// the difference has to exist as a statement for the assertion to be about it.
//
// The two strings are pinned as identical except for that one line by
// TestSlugIndexReadAnyIndexVariantIsAPerfectMutation, so this cannot drift into
// an independent query that is easier to tell apart than the real mutation is.
func perWorkspaceSlugIndexReadIsUniqueRequired() string {
	return strings.Replace(perWorkspaceSlugIndexRead, "\t   AND x.indisunique", "\t   AND true", 1)
}

// TestSlugIndexReadMatchesThePartialUniqueIndex pins the shape of the index the
// assertion is about: `forms_workspace_slug_key` is
// `UNIQUE (workspace_id, slug) WHERE deleted_at IS NULL`, i.e. a PARTIAL unique
// index. A read that required a non-partial index (or a given key order) would
// miss the very index #77 is about and report a false invariant violation.
func TestSlugIndexReadMatchesThePartialUniqueIndex(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	var partial bool
	if err := p.QueryRow(ctx, `
		SELECT bool_or(x.indpred IS NOT NULL)
		  FROM pg_class i
		  JOIN pg_index x ON x.indexrelid = i.oid
		  JOIN pg_class t ON t.oid = x.indrelid
		  JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
		 WHERE nsp.nspname = current_schema()
		   AND t.relname = 'forms'
		   AND i.relkind IN ('i','I')
		   AND x.indisunique
		   AND (SELECT array_agg(a.attname ORDER BY a.attname)
		          FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
		          JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum
		         WHERE k.attnum <> 0 AND k.ord <= x.indnkeyatts) = ARRAY['slug','workspace_id']::name[]`).Scan(&partial); err != nil {
		t.Fatalf("probe index partialness: %v", err)
	}
	if !partial {
		t.Fatalf("the per-workspace unique slug index is no longer partial — #77 relies on " +
			"`WHERE deleted_at IS NULL`, and the read (and this guard) are built around that shape")
	}

	// The shipped read must NOT filter partial indexes out; doing so returns 0
	// and reports a false invariant violation. Asserted by running the shipped
	// read with an added `indpred IS NULL` and requiring the two to differ.
	var withPredicate int
	if err := p.QueryRow(ctx, `
		SELECT count(*) FROM pg_class i
		  JOIN pg_index x ON x.indexrelid = i.oid
		  JOIN pg_class t ON t.oid = x.indrelid
		  JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
		 WHERE nsp.nspname = current_schema()
		   AND t.relname = 'forms'
		   AND i.relkind IN ('i','I')
		   AND x.indisunique
		   AND x.indpred IS NULL
		   AND (SELECT array_agg(a.attname ORDER BY a.attname)
		          FROM unnest(x.indkey) WITH ORDINALITY AS k(attnum, ord)
		          JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = k.attnum
		         WHERE k.attnum <> 0 AND k.ord <= x.indnkeyatts) = ARRAY['slug','workspace_id']::name[]`).Scan(&withPredicate); err != nil {
		t.Fatalf("probe non-partial variant: %v", err)
	}
	if withPredicate != 0 {
		t.Errorf("a non-partial-only variant of the read matched %d index(es); the guard below "+
			"assumes the real index is partial, so it can no longer tell the shapes apart", withPredicate)
	}

	var shipped int
	if err := p.QueryRow(ctx, perWorkspaceSlugIndexRead).Scan(&shipped); err != nil {
		t.Fatalf("read per-workspace slug index: %v", err)
	}
	if shipped != 1 {
		t.Errorf("the per-workspace slug index read matched %d index(es), want 1 — adding "+
			"`AND x.indpred IS NULL` to it returns %d and would report a false invariant violation", shipped, withPredicate)
	}
}

// TestSlugIndexReadAnyIndexVariantIsAPerfectMutation is a self-check on the
// mutation the guard below asserts about: the mutated read must differ from the
// shipped one in the uniqueness predicate and in NOTHING else.
//
// Without this, the guard could be satisfied by a hand-written second query that
// is easier to tell apart than the real mutation is, and would then pass while
// the actual one-line mutation goes unnoticed. If perWorkspaceSlugIndexRead is
// edited and the mutated shape is not, one of the two assertions below fails.
func TestSlugIndexReadAnyIndexVariantIsAPerfectMutation(t *testing.T) {
	mutated := perWorkspaceSlugIndexReadIsUniqueRequired()
	if !strings.Contains(mutated, "	   AND true") {
		t.Fatalf("the any-index variant no longer carries `AND true`, so it is not the mutation "+
			"the guard below claims to catch:\n%s", mutated)
	}
	if strings.Contains(mutated, "indisunique") {
		t.Fatalf("the any-index variant still carries an uniqueness predicate, so it is not the "+
			"`AND x.indisunique` -> `AND true` mutation:\n%s", mutated)
	}
	if strings.Count(mutated, "\n") != strings.Count(perWorkspaceSlugIndexRead, "\n") {
		t.Fatalf("the any-index variant has a different number of lines than the shipped read, so " +
			"the two differ in more than the uniqueness predicate")
	}
	if !strings.EqualFold(strings.Replace(mutated, "AND true", "AND x.indisunique", 1), perWorkspaceSlugIndexRead) {
		t.Fatalf("the any-index variant is not the shipped read with only the uniqueness predicate "+
			"changed:\n--- shipped ---\n%s\n--- mutated ---\n%s", perWorkspaceSlugIndexRead, mutated)
	}
}

// TestSlugIndexReadRequiresAUniqueIndex pins the last predicate the read's
// correctness depends on: `AND x.indisunique`.
//
// It is load-bearing, and it is the one predicate whose removal the other three
// slug guards do NOT catch. They all pass on a read that has stopped requiring a
// unique index, and TestSlugUniquenessPerWorkspace only fails on `n == 0`, so
// without this predicate the #77 assertion can go green on a non-unique key match
// — i.e. on an index that enforces nothing.
//
// A guard for it cannot simply count what the shipped read matches on the clean
// schema, because the clean schema holds exactly one `(workspace_id, slug)` index
// (forms_workspace_slug_key, UNIQUE) and no non-unique one: `AND x.indisunique`
// and `AND true` both return 1 there and are indistinguishable. That is the whole
// hole — the predicate is unpinned because nothing on the tree exercises the
// difference. So this test BUILDS the difference: it plants a NON-unique
// `(workspace_id, slug)` index inside a transaction and rolls it back, and
// requires the shipped read to ignore it while the mutated read picks it up.
//
// The plant is the positive control for the plant, too: asserting first that the
// mutated read DOES see it is what stops this test from passing vacuously on a
// schema where the plant failed to land.
//
// The plant has to be a real index in THIS run's schema. A TEMP decoy — the
// mechanism the sibling TestSlugIndexReadIsSchemaScoped uses — cannot work here:
// a TEMP table lands in pg_temp_N, so current_schema() stays the run's own schema
// and BOTH shapes ignore it (measured; pg_my_temp_schema() = pg_temp_83 while
// current_schema() = test_dbstore). Only a non-temp index is visible to the
// mutated read.
func TestSlugIndexReadRequiresAUniqueIndex(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	// Baseline: with no plant, the two shapes agree, so any difference observed
	// below is attributable to the plant rather than to the schema.
	var shippedBefore int
	if err := p.QueryRow(ctx, perWorkspaceSlugIndexRead).Scan(&shippedBefore); err != nil {
		t.Fatalf("read per-workspace slug index: %v", err)
	}
	if shippedBefore != 1 {
		t.Fatalf("the per-workspace slug index read matched %d index(es) on the clean schema, "+
			"want 1; this guard plants a decoy beside it and needs that baseline to be exactly one",
			shippedBefore)
	}

	// Inside a transaction so the decoy is rolled back. It is a real (not TEMP)
	// index in this run's own schema, which is what makes it visible to the
	// mutated read; the rollback is what keeps it out of the schema every other
	// test in this package reads, `forms_workspace_slug_key` included.
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The same key columns as the real index, deliberately NON-unique: this is
	// exactly the index the read must not accept as the #77 constraint.
	if _, err := tx.Exec(ctx, `CREATE INDEX guard_decoy_nonunique_workspace_slug_idx ON forms (workspace_id, slug)`); err != nil {
		t.Fatalf("plant non-unique decoy: %v", err)
	}
	var decoyUnique bool
	if err := tx.QueryRow(ctx, `
		SELECT x.indisunique FROM pg_class i
		  JOIN pg_index x ON x.indexrelid = i.oid
		 WHERE i.relname = 'guard_decoy_nonunique_workspace_slug_idx'`).Scan(&decoyUnique); err != nil {
		t.Fatalf("read decoy uniqueness: %v", err)
	}
	if decoyUnique {
		t.Fatalf("the planted decoy is UNIQUE, so dropping `AND x.indisunique` would be " +
			"undetectable — the plant cannot control the mutation it exists to expose")
	}

	// Positive control for the plant: the mutated read MUST see it. If it does
	// not, the decoy is not in a place either shape can match and the assertion
	// below would pass vacuously.
	var anyIndex int
	if err := tx.QueryRow(ctx, perWorkspaceSlugIndexReadIsUniqueRequired()).Scan(&anyIndex); err != nil {
		t.Fatalf("read with any-index variant: %v", err)
	}
	if anyIndex != 2 {
		t.Fatalf("control: the any-index variant matched %d index(es) with a non-unique "+
			"`forms (workspace_id, slug)` index planted, want 2 (the real one plus the decoy); "+
			"the plant is not reaching the read, so this guard is not testing anything", anyIndex)
	}

	// The assertion: the shipped read must still match exactly the one UNIQUE
	// index. If `AND x.indisunique` is dropped from perWorkspaceSlugIndexRead,
	// this returns 2 and fails — which is the mutation this guard exists for.
	var shipped int
	if err := tx.QueryRow(ctx, perWorkspaceSlugIndexRead).Scan(&shipped); err != nil {
		t.Fatalf("read per-workspace slug index: %v", err)
	}
	if shipped != 1 {
		t.Errorf("the per-workspace slug index read matched %d index(es) with a NON-unique "+
			"`forms (workspace_id, slug)` index planted, want exactly 1. The `AND x.indisunique` "+
			"predicate is what keeps this read on the index that actually enforces #77; without "+
			"it the read also matches an index that enforces nothing, and "+
			"TestSlugUniquenessPerWorkspace only fails on `n == 0`, so the invariant assertion "+
			"goes green on a constraint that does not exist. The any-index variant of the same "+
			"statement returns %d here.", shipped, anyIndex)
	}
}
