// Package testdb — reclaiming leaked throwaway DATABASES, the counterpart of
// pruneDeadSchemas.
//
// WHY THIS FILE EXISTS (card t_46165e15)
//
// testdb.Open gives each test PROCESS a scratch SCHEMA (test_<pkg>_p<pid>) and
// pruneDeadSchemas reclaims the schemas a killed run leaves behind. The other
// half of the isolation story is the scratch DATABASE: three fixtures in this
// tree create a whole throwaway database, because the thing under test IS the
// database — applying the migration set from zero, or provisioning the shared
// extensions into a database whose `public` schema has none:
//
//   - internal/db/migrations_fresh_db_test.go       jomform_migtest_*
//   - internal/testdb/prune_open_reach_test.go      jomform_reachtest_*
//   - internal/testdb/scratch_extension_test.go     jomform_testdb_regress_*
//   - internal/testdb/extensions_integration_test.go jomform_tdbguard_*
//   - internal/testdb/prepare_extensions_ownership_test.go jomform_testdb_own_*
//
// (That list is not from memory: scratchdb_registry_test.go derives it from the
// files that actually issue a database-creating statement, which is how the last
// two — both outside this package's schema story and absent from the original
// measurement — were found.)
//
// A throwaway database is dropped by t.Cleanup, and t.Cleanup does NOT run when
// the process is killed — a gate hitting its timeout, an OOM, a lane being torn
// down. Every such run leaks a database, and nothing reclaimed them. Measured
// on the shared test host at 2026-09-25: 273 databases / 4506 MB, by prefix
// family:
//
//	jf_*               123
//	jomform_qa*         49
//	jomform_test*       20
//	jomform_migtest_*   28
//	repro_/tcf_*        16
//
// Two lessons are baked into the code below.
//
//  1. The family set must span MORE THAN ONE PREFIX. A prune scoped to
//     `jomform_migtest_%` leaves ~245 databases and ~4 GB behind, and the
//     jf_* family alone is 123 of them. scratchDBFamilies therefore enumerates
//     the families that were measured, and the in-tree creators are tied to
//     their entries by a guard (scratchdb_registry_test.go) so a fourth
//     throwaway-database fixture cannot start leaking unregistered.
//  2. The reclaim must be conservative in both directions. Dropping a database
//     a peer run is using costs that lane its whole gate; leaving a leaked one
//     behind costs disk. So the rule never reclaims a database with a live
//     backend, never one whose creating process is still alive, never the
//     shared test database or a cluster-reserved name, and — for a name it
//     cannot attribute to a process at all — never one younger than the grace
//     period, because such a database may be a peer's, created a moment ago and
//     not yet connected to.
//
// The enumeration is not a second source of truth for the in-tree fixtures:
// scratchdb_registry_test.go derives their prefixes from the create sites
// themselves and fails the build when the two disagree in either direction.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scratchDBFamily is one PREFIX FAMILY of throwaway databases the reclaim
// considers. Prefix matching is `HasPrefix`, which is why `jf_` covers
// `jf_qa_*` / `jf_merge_*` and `jomform_test` covers
// `jomform_testdb_regress_*`.
type scratchDBFamily struct {
	prefix string
	// createdBy is the file that creates databases under this prefix,
	// slash-separated and relative to the api tree. Empty means no current code
	// creates it: the family was measured on the host and is created by hand
	// (or by a checkout that predates this rule).
	createdBy string
}

// scratchDBFamilies is the ENUMERATED set of database-name prefixes this prune
// reclaims. It is deliberately spelled out rather than reduced to a single
// prefix, because the leak is not one family — see the package comment for the
// measurement each entry comes from.
//
// Every entry has to earn its place. A prefix that is too broad reclaims a
// database someone is keeping; one that is missing leaks a database forever.
// The in-tree entries are checked against the create sites by
// scratchdb_registry_test.go, so this list cannot drift from the fixtures.
var scratchDBFamilies = []scratchDBFamily{
	{prefix: "jomform_migtest_", createdBy: "internal/db/migrations_fresh_db_test.go"},
	{prefix: "jomform_reachtest_", createdBy: "internal/testdb/prune_open_reach_test.go"},
	{prefix: "jomform_testdb_regress_", createdBy: "internal/testdb/scratch_extension_test.go"},
	{prefix: "jomform_tdbguard_", createdBy: "internal/testdb/extensions_integration_test.go"},
	{prefix: "jomform_testdb_own_", createdBy: "internal/testdb/prepare_extensions_ownership_test.go"},

	// The families below have no in-tree creator: they were created by hand
	// (one per lane or card, now banned on this board) or by a checkout from
	// before this rule. They are enumerated from the measurement above, because
	// a prune that only knows the in-tree prefixes reclaims a fraction of the
	// leak.
	//
	// `jomform_test` (no trailing underscore) is the broad one: it also matches
	// `jomform_testdb_regress_*`. It does NOT match the shared test database —
	// `jomform_test` itself — only because reclaimableScratchDatabase excludes
	// that name explicitly, and TestTheSharedTestDatabaseIsNeverReclaimed pins
	// that. Do not "tidy" the entry into something narrower without moving that
	// protection with it.
	{prefix: "jf_"},
	{prefix: "jomform_qa"},
	{prefix: "jomform_test"},
	{prefix: "repro_"},
	{prefix: "tcf_"},
}

// scratchDatabaseFamily reports whether name belongs to a family this prune is
// allowed to consider. It is the FIRST gate, not the only one: everything that
// passes it still has to be unattributed-to-a-live-process, unconnected and old
// enough (see reclaimableScratchDatabase).
func scratchDatabaseFamily(name string) bool {
	for _, f := range scratchDBFamilies {
		if strings.HasPrefix(name, f.prefix) {
			return true
		}
	}
	return false
}

// reservedDatabases are cluster-level names that are never candidates. The two
// templates are also excluded by `not datistemplate` in the enumeration query,
// and `postgres` is the maintenance database every client and operator expects
// to find; it is listed here so the rule does not depend on no family prefix
// ever matching it. Being superuser, this role COULD drop any of them — the
// reservation, not the privilege, is what makes them safe.
var reservedDatabases = []string{"postgres", "template0", "template1"}

// legacyDatabaseGrace bounds the ambiguity of a database name that carries no
// pid. Such a name cannot be attributed to a process, so "no backend is
// connected" is not enough to conclude it is abandoned: a peer that ran
// `CREATE DATABASE` and has not opened its pool yet (pgxpool is lazy) looks
// exactly the same for a few milliseconds, and dropping it would red a lane
// that has nothing to do with this. Nothing legitimate holds a leaked scratch
// database for ten minutes, so a name older than this is abandoned for good.
//
// Databases created from here on carry the creating pid (`…_p<pid>`, the same
// convention pkgName uses for schemas), which makes their liveness attributable
// and takes them out of this heuristic entirely.
const legacyDatabaseGrace = 10 * time.Minute

// dsnDatabaseName returns the database the integration DSN names, or "" when
// the DSN is unset or unparseable.
//
// It exists because "which database is the suite's shared one" is NOT the same
// question as "which database is this connection in", and conflating the two
// destroyed the shared test database (measured while building this card):
// testdb's own guards call open() with a THROWAWAY database's DSN, so inside
// that call `current_database()` was the fixture and the shared database looked
// like an ordinary leaked candidate — old, unattributable, and momentarily
// unconnected — and the prune dropped it. Every later test in that run then died
// with `database "jomform_test" does not exist (SQLSTATE 3D000)`.
func dsnDatabaseName() string {
	raw := os.Getenv(DSNEnv)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// scratchDBCandidate is everything the reclaim decision is taken on for ONE
// database. Grouping it keeps the decision a pure function of its inputs, which
// is what lets the rule be driven with planted answers instead of a real host.
type scratchDBCandidate struct {
	name string
	// size is the database's on-disk size, read from the same directory walk
	// that the creation time comes from. A database whose directory is gone (a
	// drop in flight) is not a candidate at all — see candidates().
	size int64
	// created is the database's creation time and haveCreated says whether it
	// could be read at all (see scratchDBAgesQuery).
	created     time.Time
	haveCreated bool
	// backends is how many OTHER backends are connected to this database.
	backends int
}

// reclaimableScratchDatabase reports whether a leaked throwaway database may be
// dropped, and why not when it may not. Both directions matter:
//
//   - a database this process, a peer run or a human may still be using is
//     NEVER reclaimed — the reclaim is destructive and irreversible, while a
//     leaked database costs disk. Every arm below yields in that direction.
//   - a database that is genuinely abandoned IS reclaimed, or the leak comes
//     straight back: the whole point of the card is that t.Cleanup cannot run
//     on a killed process.
//
// `protect` is the set of names that are never candidates: the database the
// integration DSN names and the database this connection is in. Both are needed
// and neither implies the other — the suite's SHARED database is named by the
// DSN, while testdb's own guards open() throwaway databases, so the connection's
// own database is a different name there. Protecting only the latter dropped the
// shared test database for real; see dsnDatabaseName.
//
// The `jomform_test` family prefix matches the shared database's own name, so
// that exclusion is load-bearing, not belt-and-braces.
func reclaimableScratchDatabase(c scratchDBCandidate, protect []string) (bool, string) {
	for _, p := range protect {
		if p != "" && c.name == p {
			return false, "it is a database the suite runs in (the shared test database named by " + DSNEnv +
				", or the database this connection is in)"
		}
	}
	for _, r := range reservedDatabases {
		if c.name == r {
			return false, "it is a cluster-reserved database"
		}
	}
	if !scratchDatabaseFamily(c.name) {
		return false, "its name belongs to no throwaway-database family"
	}
	// `DROP DATABASE` refuses while any other session is connected, so this is
	// also the race-free ordering: the count is taken now, the drop is issued
	// later, and a peer that connects in between makes the drop fail rather
	// than take the peer's database away.
	if c.backends > 0 {
		return false, fmt.Sprintf("%d other backend(s) are connected to it", c.backends)
	}
	// A name that carries the creating process's pid is attributable: the
	// process either still exists (leave the database alone) or it is gone
	// (nothing can still be using it, reclaim it now).
	if pid, ok := schemaPID(c.name); ok {
		if ownerAliveFromErr(syscall.Kill(pid, 0)) {
			return false, fmt.Sprintf("its creating process (pid %d) still exists", pid)
		}
		return true, ""
	}
	// An unattributable name: only reclaimable once it is old enough that no
	// peer can be about to use it (see legacyDatabaseGrace).
	if !c.haveCreated {
		return false, "its creation time could not be read, so it cannot be told apart from a database a run is about to use"
	}
	if age := time.Since(c.created); age < legacyDatabaseGrace {
		return false, fmt.Sprintf("it was created %s ago, inside the %s grace period for a name with no pid",
			age.Round(time.Second), legacyDatabaseGrace)
	}
	return true, ""
}

// scratchDBCandidatesQuery lists every database the reclaim is allowed to
// consider. `information_schema` is the wrong catalogue for this: databases are
// not schemas, and pg_database is where they (and their sizes) live.
//
// `datname <> current_database()` keeps THIS connection's database out of the
// candidate list. It is not the only exclusion and must never be treated as one:
// the database the DSN names is a different name whenever open() runs on a
// fixture, and that is protected by the rule (see dsnDatabaseName) — leaving it
// to this clause alone is what dropped the shared test database.
//
// `pg_database_size(oid)` is a walk of the database's directory, so it reports
// NULL — not 0, and not an error (measured on this host: an unlinked directory
// and a bogus OID both answer NULL) — when there is no directory to walk. That
// is the same window scratchDBAgesQuery documents: a database that is being
// dropped right now still has its pg_database row and no longer has a
// directory. candidates() drops such a database from the candidate list rather
// than reading its unreadable size as zero; see the scan there.
const scratchDBCandidatesQuery = `SELECT datname, pg_database_size(oid)
  FROM pg_database
 WHERE NOT datistemplate AND datname <> current_database()
 ORDER BY datname`

// scratchDBAgesQuery reads each database's creation time. There is no catalogue
// column for it, so it comes from the database directory's PG_VERSION file,
// which the server writes when the database is created and never rewrites.
//
// pg_stat_file needs a role that is a superuser or holds pg_monitor /
// pg_read_server_files. When it is unavailable the unattributable arm of the
// rule is skipped entirely (an unreadable age is never reclaimed) and the prune
// records that in lastDBPruneNote. TestScratchDatabaseCreationTimesAreReadable
// fails loudly in that case, so the degradation cannot pass unnoticed.
//
// `true` (pg_stat_file's missing_ok) is LOAD-BEARING and is not about that
// degradation (card t_55ee71c8). `DROP DATABASE` unlinks base/<oid> BEFORE the
// pg_database row it deletes becomes invisible, so for the length of that window
// a database is listed here with no directory to stat — and without missing_ok,
// pg_stat_file raises `could not stat file "base/<oid>/PG_VERSION": No such file
// or directory (SQLSTATE 58P01)`. ONE such database errored the WHOLE read,
// because this is one statement: every age came back unreadable, the prune
// recorded the degradation note, and TestPruneDeadDatabasesNeverDropsProtected-
// Databases turned that note into a red on WHATEVER tree was under test (measured
// on this host: a full `go test ./...` runs packages in parallel and this prune
// itself drops databases, so the window is reachable from the suite alone).
//
// The distinction missing_ok buys is exactly the honest one: a vanished
// directory means THAT database's age is unknown — the answer the rule already
// treats conservatively (an age it cannot read is never reclaimed) — while a
// REAL unavailability of the age source is still an error and still sets the
// note, because missing_ok turns away a missing file only. `permission denied
// for function pg_stat_file` (measured on this host with a role holding neither
// pg_monitor nor pg_read_server_files), and every other errno, still error the
// statement.
//
// Known limit, stated rather than assumed: this tolerates the errno the flake
// produced (ENOENT). A concurrent drop that made stat fail some other way would
// still error the read — a rarer shape nobody has measured, and one the note
// would honestly report.
const scratchDBAgesQuery = `SELECT datname, (pg_stat_file('base/' || oid || '/PG_VERSION', true)).modification
  FROM pg_database
 WHERE NOT datistemplate`

// scratchDBBackendsQuery counts the backends connected to each database. The
// count excludes this session (`pid <> pg_backend_pid()`), or every database
// would report at least the one running the query — the trap the card records.
//
// It counts EVERY backend, deliberately, and must keep doing so. This count is
// not a guess about who a peer run is, it is the precondition of the DDL itself:
// the reclaim's drop is a PLAIN `DROP DATABASE` (see dropDatabase), and Postgres
// refuses that while anyone else is connected — "if anyone else is connected to
// the target database, this command will fail unless you use the FORCE option"
// (https://www.postgresql.org/docs/16/sql-dropdatabase.html), with FORCE being
// what terminates background worker connections. So filtering it to client
// backends — the way the SCHEMA prune's peer predicate does (card t_26671247,
// peerBackendsPredicate in testdb.go) — would not make the reclaim more eager;
// it would make the drop fail with `database "..." is being accessed by other
// users`. The two counts look alike and are not the same rule: the schema
// count protects a peer's catalogue reads, this one asks whether the DDL will
// succeed.
const scratchDBBackendsQuery = `SELECT datname, count(*)
  FROM pg_stat_activity
 WHERE datname IS NOT NULL AND pid <> pg_backend_pid()
 GROUP BY datname`

// scratchDBQuerier is the slice of the cluster the reclaim needs. It exists so
// the rule and the wiring can be driven against a fake that plants candidates
// with controlled answers — a live host can only ever report what it happens to
// hold, and the arm that matters most (a database a peer is using) cannot be
// planted on demand there.
type scratchDBQuerier interface {
	// currentDatabase names the database THIS connection is in, which is never
	// a candidate (dropping it would cut the prune's own branch off).
	currentDatabase(ctx context.Context) (string, error)
	candidates(ctx context.Context) ([]scratchDBCandidate, error)
	dropDatabase(ctx context.Context, name string) error
}

// scratchDBConn is the slice of a PostgreSQL connection the live querier uses:
// the admin pool in production, and — in the guard for a database dropped
// mid-scan (scratchdb_prune_test.go) — a TRANSACTION, because that guard has to
// hold a snapshot open across the drop and drive the real reads against the
// condition the flake names rather than racing for it. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so nothing about the production wiring changes.
type scratchDBConn interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// pgDatabases is the live implementation, over the admin connection.
type pgDatabases struct{ admin scratchDBConn }

func (q pgDatabases) currentDatabase(ctx context.Context) (string, error) {
	var name string
	err := q.admin.QueryRow(ctx, `SELECT current_database()`).Scan(&name)
	return name, err
}

func (q pgDatabases) candidates(ctx context.Context) ([]scratchDBCandidate, error) {
	rows, err := q.admin.Query(ctx, scratchDBCandidatesQuery)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	var cands []scratchDBCandidate
	for rows.Next() {
		var c scratchDBCandidate
		// A NULL size is a database whose directory is already gone — it is
		// being dropped right now (see scratchDBCandidatesQuery), so it is not
		// this prune's to reclaim and its size is not a number to report. It is
		// dropped from the list, and the reads below are not affected: this is
		// one database's unreadable input, not a failure of the query.
		var size *int64
		if err := rows.Scan(&c.name, &size); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan database row: %w", err)
		}
		if size == nil {
			continue
		}
		c.size = *size
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate databases: %w", err)
	}

	// Creation times, best effort — see scratchDBAgesQuery. A nil error with a
	// candidate missing from the map means "age unknown", and unknown is never
	// reclaimed.
	ages, ageErr := q.creationTimes(ctx)
	if ageErr != nil {
		note := "database creation times unavailable: " + ageErr.Error()
		lastDBPruneNote.Store(&note)
	} else {
		note := ""
		lastDBPruneNote.Store(&note)
	}

	// Connected backends. This one is NOT best effort: without it the rule
	// cannot tell a database a peer is using from an abandoned one, and
	// guessing in that direction is the only unrecoverable mistake here.
	backends, err := q.backends(ctx)
	if err != nil {
		return nil, fmt.Errorf("count backends per database: %w", err)
	}

	for i := range cands {
		if t, ok := ages[cands[i].name]; ok {
			cands[i].created, cands[i].haveCreated = t, true
		}
		cands[i].backends = backends[cands[i].name]
	}
	return cands, nil
}

// creationTimes reads every non-template database's creation time. A database
// missing from the result is one whose age could not be read — most often a
// database dropped mid-scan, whose directory is already unlinked while its
// pg_database row is still visible (see scratchDBAgesQuery). Unknown is never
// reclaimed, so leaving it out is the conservative answer; it is NOT a
// degradation of the age source, which is what the note is reserved for.
func (q pgDatabases) creationTimes(ctx context.Context) (map[string]time.Time, error) {
	rows, err := q.admin.Query(ctx, scratchDBAgesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]time.Time)
	for rows.Next() {
		var name string
		// NULL = the directory was gone at this instant (missing_ok in the
		// query) — an age unknown for THAT database, not a failed read.
		var created *time.Time
		if err := rows.Scan(&name, &created); err != nil {
			return nil, err
		}
		if created == nil {
			continue
		}
		out[name] = *created
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// backends counts the connected backends per database, excluding this session.
func (q pgDatabases) backends(ctx context.Context) (map[string]int, error) {
	rows, err := q.admin.Query(ctx, scratchDBBackendsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// dropDatabase issues the reclaim. It is deliberately a PLAIN drop, never
// `WITH (FORCE)`: FORCE terminates whatever is connected, i.e. exactly the peer
// this rule exists to protect, and the fixture cleanups that do use it are
// dropping databases they created themselves. A plain drop refuses while a
// session is connected, so a peer that arrives between the count and the drop
// makes the reclaim yield rather than take its database away.
func (q pgDatabases) dropDatabase(ctx context.Context, name string) error {
	_, err := q.admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quote(name))
	return err
}

// dbPruneResult is what the most recent live database prune did, for the guards
// (and for a human reading a gate log) to judge the reclaim against instead of
// guessing from whatever the host looks like afterwards.
type dbPruneResult struct {
	// scanned is how many databases were considered, dropped is what was
	// reclaimed, bytes is the total size of those, and kept records every
	// candidate that stayed with the reason the rule gave. protected is the set
	// of names the prune refused to consider at all.
	scanned   int
	dropped   []string
	bytes     int64
	kept      []string
	protected []string
}

// lastDBPruneResult holds the outcome of the most recent LIVE prune, and
// lastDBPruneNote any degradation the operator should know about (an
// unreadable creation time, which silently switches the unattributable arm
// off). It reads a process-global, which is sound because each test binary is
// its own process and the guards read it immediately after the prune that set
// it.
var (
	lastDBPruneResult atomic.Pointer[dbPruneResult]
	lastDBPruneNote   atomic.Pointer[string]
)

// lastDBPrune returns the most recent live database prune's outcome, and
// whether one ran in this process.
func lastDBPrune() (dbPruneResult, bool) {
	r := lastDBPruneResult.Load()
	if r == nil {
		return dbPruneResult{}, false
	}
	return *r, true
}

// databasePruneNote returns the note the last live prune recorded about the
// inputs it could not read ("" when it read all of them).
func databasePruneNote() string {
	n := lastDBPruneNote.Load()
	if n == nil {
		return ""
	}
	return *n
}

// pruneDeadDatabases reclaims leaked throwaway databases. Like
// pruneDeadSchemas it is best-effort: a failure here must not fail the test
// that is about to run, so errors are swallowed, and nothing is dropped unless
// the rule says so.
//
// It runs from open(), BEFORE the caller's pool is created — not because this
// prune needs the database to be idle (it asks about each candidate's own
// backends), but because that is where the scratch-schema prune runs and
// because a prune that ran after the caller's pool existed would count the
// caller's own database as busy for the rest of the run.
func pruneDeadDatabases(ctx context.Context, admin *pgxpool.Pool) {
	shared := dsnDatabaseName()
	if shared == "" {
		// Never guess. The name that must survive is the one the DSN names;
		// without it the only exclusion left is the connection's own database,
		// and that alone is what dropped the shared test database for real.
		// Skipping is the conservative answer: the leak waits for a run whose
		// configuration says which database the suite runs in.
		note := DSNEnv + " names no database, so the database prune was skipped"
		lastDBPruneNote.Store(&note)
		lastDBPruneResult.Store(&dbPruneResult{})
		return
	}

	// The database the DSN names is protected BY NAME, because the connection
	// this prune runs on may be a throwaway one: open() is called with a
	// fixture's DSN by most of the guards in this package, and from there
	// `current_database()` is the fixture — so without the DSN's name the
	// suite's shared database looks like an ordinary leaked candidate and gets
	// dropped.
	res, err := pruneScratchDatabases(ctx, pgDatabases{admin}, shared)
	if err != nil {
		// Recorded rather than swallowed: the reclaim is best-effort for the
		// test about to run, but a prune that could not read its inputs must
		// not be indistinguishable from one that found nothing to do.
		note := "the database prune did not complete: " + err.Error()
		lastDBPruneNote.Store(&note)
	}
	lastDBPruneResult.Store(&res)
}

// pruneScratchDatabases applies the rule to every candidate the querier offers
// and drops the ones it reclaims, returning what it did. It is separated from
// the live querier so the WIRING — that the rule is consulted at all, and that
// a refusal is honoured — can be asserted with planted answers.
//
// `protect` are names that must never be considered, in addition to the
// database the querier's own connection is in (which the rule protects too).
func pruneScratchDatabases(ctx context.Context, q scratchDBQuerier, protect ...string) (dbPruneResult, error) {
	var res dbPruneResult

	own, err := q.currentDatabase(ctx)
	if err != nil {
		return res, fmt.Errorf("resolve this connection's database: %w", err)
	}
	res.protected = append([]string{own}, protect...)

	cands, err := q.candidates(ctx)
	if err != nil {
		return res, err
	}
	res.scanned = len(cands)

	for _, c := range cands {
		ok, why := reclaimableScratchDatabase(c, res.protected)
		if !ok {
			res.kept = append(res.kept, c.name+": "+why)
			continue
		}
		if err := q.dropDatabase(ctx, c.name); err != nil {
			// A drop that lost the race against a peer's new connection lands
			// here. That is the designed yield, so it is recorded, never
			// fatal.
			res.kept = append(res.kept, c.name+": drop refused: "+err.Error())
			continue
		}
		res.dropped = append(res.dropped, c.name)
		res.bytes += c.size
	}
	return res, nil
}
