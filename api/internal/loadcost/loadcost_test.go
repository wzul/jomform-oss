package loadcost

// Tests for the load-run COST SAFETY interlock (card t_bf70cb09).
//
// WHY EVERY ARM BELOW EXISTS
//
// The defect is an INVISIBLE cost: a load run's submissions enqueue async AI
// spam-judge jobs that POST to the real model endpoint, so a sweep spends paid
// model calls that never appear in its own latency numbers. The guard's value is
// therefore in the cases where it REFUSES, and in the fact that the sweep runner
// obeys that refusal. The arms are grouped exactly that way:
//
//	A. naming / verdict plumbing   — a wrong or ambiguous target is a usage
//	                                 error, and every sentinel maps onto the exit
//	                                 code the sweep runner reads;
//	B. the refusal                 — judge ON, judge ON because the settings row
//	                                 does not exist yet (the app's own default),
//	                                 and "could not read" (fail closed);
//	C. the write + the READ-BACK   — the write preserves every other column, it
//	                                 creates the row when it is missing, and a
//	                                 write that does NOT take effect is a refusal
//	                                 even though the statement returned no error;
//	D. the command and the wrapper — the exit codes an operator and the runner
//	                                 see, driven through the real built binary;
//	E. the sweep runner            — loadtest/baseline.sh really does abort BEFORE
//	                                 generating traffic.
//
// BITE PROOFS (what makes each arm fail if the behaviour is removed):
//
//	B1 delete the `st.SpamJudgeEnabled` check from Check      → B1 fails
//	B3 return nil when Settings() errors                      → B3 fails
//	C1 take the write's own status as the verdict             → C3 fails
//	C3 delete the fresh read from Disable                     → C3 fails
//	D1 map a judge-on verdict onto exit 0 in run()            → D2 fails
//	E1 delete the guard call from baseline.sh                 → E1 runs k6, fails
//	E2 ignore the guard's exit code in baseline.sh            → E1 exits 0, fails
//
// The integration arms need a real Postgres ($JOMFORM_TEST_DATABASE_URL, through
// api/internal/testdb, i.e. this package's own scratch schema). The arms that do
// not touch it (naming, exit-code mapping, the wrapper's build/caching contract)
// run without one — but the gate itself never runs without it, because the whole
// api suite fails loudly when the DSN is unset.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// ── shared scratch state ─────────────────────────────────────────────────────

var (
	sharedDir string

	buildOnce sync.Once
	sharedBin string
	buildErr  error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jf-cost-guard-test.")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		os.Exit(1)
	}
	sharedDir = dir
	code := m.Run()
	if rerr := os.RemoveAll(dir); rerr != nil {
		fmt.Fprintln(os.Stderr, "remove scratch dir:", rerr)
	}
	os.Exit(code)
}

// ── process helpers ──────────────────────────────────────────────────────────

// envWith returns the current environment with `extra` applied, dropping any
// inherited variable it redefines: a duplicate key in a child's env is resolved
// by whichever copy the runtime sees first, which is exactly the flake this
// avoids (an inherited DATABASE_URL would silently satisfy an arm that means to
// test its absence).
func envWith(extra ...string) []string {
	keys := make(map[string]bool, len(extra))
	for _, e := range extra {
		if i := strings.IndexByte(e, '='); i > 0 {
			keys[e[:i]] = true
		}
	}
	out := make([]string, 0, len(extra)+8)
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i > 0 && keys[e[:i]] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// run executes prog in dir and returns its streams and exit code.
func run(t *testing.T, dir string, extraEnv []string, prog string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(prog, args...)
	cmd.Dir = dir
	cmd.Env = envWith(extraEnv...)
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			// A process that never started (no executable bit, missing binary) has
			// no exit code and no stderr of its own — say so, or the arm reads as a
			// silent "-1" and the cause has to be guessed.
			errs.WriteString(fmt.Sprintf("exec %s: %v\n", prog, err))
		}
	}
	return out.String(), errs.String(), exitCodeOf(err)
}

// repoRoot is the checkout these tests run in (api/internal/loadcost → ../..).
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "loadtest", "baseline.sh")); err != nil {
		return "", fmt.Errorf("repo root %s does not hold loadtest/baseline.sh: %w", root, err)
	}
	return root, nil
}

func repoRootOrFail(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// guardBin builds ./api/cmd/jf-cost-guard ONCE for the whole test binary, THROUGH
// the loadtest wrapper — so the wrapper's build-then-exec path is exercised by
// real use, and the arms that follow reuse the binary (the wrapper skips a
// rebuild because the binary is newer than every api source).
func guardBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(sharedDir, "jf-cost-guard")
		// Run the wrapper DIRECTLY (not through `bash <path>`): its executable bit
		// is part of the documented contract — the runbook shows
		// `./loadtest/cost-guard.sh …`, and baseline.sh execs it — so a missing +x
		// has to fail here instead of being masked by an interpreter.
		cmd := exec.Command(filepath.Join(root, "loadtest", "cost-guard.sh"), "help")
		cmd.Dir = root
		cmd.Env = envWith("JF_COST_GUARD_BIN=" + bin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build the guard through loadtest/cost-guard.sh: %w\n%s", err, out)
			return
		}
		sharedBin = bin
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return sharedBin
}

// runGuard runs the documented entry point (the wrapper) with the given extra
// environment and arguments.
func runGuard(t *testing.T, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	root := repoRootOrFail(t)
	env := append([]string{"JF_COST_GUARD_BIN=" + guardBin(t)}, extraEnv...)
	return run(t, root, env, filepath.Join(root, "loadtest", "cost-guard.sh"), args...)
}

// childEnv pins the child's search_path to THIS test process's scratch schema.
// The scratch schema is not on the database's default path, and pgx honours
// PGOPTIONS, so a child process can reach the same fixtures an in-process arm
// uses.
func childEnv(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	var sp string
	if err := pool.QueryRow(ctxT(t), `SHOW search_path`).Scan(&sp); err != nil {
		t.Fatalf("SHOW search_path: %v", err)
	}
	first := strings.TrimSpace(strings.Split(sp, ",")[0])
	if first == "" {
		t.Fatal("SHOW search_path returned nothing; the scratch schema cannot be named")
	}
	return []string{"PGOPTIONS=-c search_path=" + first + ",public"}
}

// writeStub writes an executable stub command and returns its path.
func writeStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return p
}

// ── fixtures ─────────────────────────────────────────────────────────────────

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// newWorkspace creates a user + workspace with random names, so arms sharing the
// one scratch schema cannot collide.
func newWorkspace(t *testing.T, pool *pgxpool.Pool) *dbstore.Workspace {
	t.Helper()
	ctx := ctxT(t)
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('lc-' || md5(random()::text) || '@example.test', 'x')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var id, slug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'loadcost test', 'lc-' || substr(md5(random()::text), 1, 8))
		 RETURNING id, slug`, userID).Scan(&id, &slug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return &dbstore.Workspace{ID: id, Slug: slug}
}

// setToggle writes the column straight to Postgres: test setup must not go
// through the code under test.
func setToggle(t *testing.T, pool *pgxpool.Pool, id string, on bool) {
	t.Helper()
	if _, err := store(pool).Settings(ctxT(t), id); err != nil { // materialise the row the app's way
		t.Fatalf("materialise settings row: %v", err)
	}
	if _, err := pool.Exec(ctxT(t),
		`UPDATE workspace_settings SET spam_judge_enabled = $2 WHERE workspace_id = $1`, id, on); err != nil {
		t.Fatalf("set spam_judge_enabled=%v: %v", on, err)
	}
}

// storedRow is the INDEPENDENT read the write arms assert on: the toggle plus the
// columns a careless write would clobber.
type storedRow struct {
	Exists      bool
	On          bool
	FooterText  string
	Currency    string
	DefaultLang string
	MenuJSON    string
	SoundOn     bool
	Indexing    bool
}

func readRow(t *testing.T, pool *pgxpool.Pool, id string) storedRow {
	t.Helper()
	var r storedRow
	err := pool.QueryRow(ctxT(t),
		`SELECT spam_judge_enabled, footer_text, currency, default_lang, menu_json::text, sound_enabled, indexing
		   FROM workspace_settings WHERE workspace_id = $1`, id).
		Scan(&r.On, &r.FooterText, &r.Currency, &r.DefaultLang, &r.MenuJSON, &r.SoundOn, &r.Indexing)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedRow{Exists: false}
	}
	if err != nil {
		t.Fatalf("read workspace_settings row: %v", err)
	}
	r.Exists = true
	return r
}

func store(pool *pgxpool.Pool) *dbstore.WorkspaceStore { return dbstore.NewWorkspaceStore(pool) }

func dsn() string { return os.Getenv(testdb.DSNEnv) }

// ── A. naming + verdict plumbing ─────────────────────────────────────────────

func TestResolveRejectsAnAmbiguousOrMalformedTarget(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)

	arms := []struct {
		name       string
		workspace  string
		space      string
		wantExit   int
		wantSubstr string
	}{
		{"neither naming", "", "", ExitUsage, "--space"},
		{"bad uuid", "not-a-uuid", "", ExitUsage, "not a uuid"},
		{"uppercase slug", "", "Demo", ExitUsage, "not a subdomain slug"},
		{"slug with an underscore", "", "demo_1", ExitUsage, "not a subdomain slug"},
	}
	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			_, err := Resolve(ctxT(t), store(pool), a.workspace, a.space)
			if err == nil {
				t.Fatalf("Resolve(%q, %q) succeeded; want a refusal", a.workspace, a.space)
			}
			if got := ExitCode(err); got != a.wantExit {
				t.Errorf("exit code = %d, want %d (err: %v)", got, a.wantExit, err)
			}
			if !strings.Contains(err.Error(), a.wantSubstr) {
				t.Errorf("message %q does not name %q", err.Error(), a.wantSubstr)
			}
		})
	}

	// An unknown slug is NOT a usage error: it is a state that could not be
	// verified, which the runner must treat as fail-closed (4), not as a typo.
	_, err := Resolve(ctxT(t), store(pool), "", "lc-does-not-exist")
	if err == nil {
		t.Fatal("Resolve on an unknown slug succeeded; want a refusal")
	}
	if got := ExitCode(err); got != ExitUnverified {
		t.Errorf("unknown slug exit code = %d, want %d (err: %v)", got, ExitUnverified, err)
	}
	if !strings.Contains(err.Error(), "lc-does-not-exist") {
		t.Errorf("message does not name the slug: %v", err)
	}

	// A slug resolves to a Target carrying BOTH id and slug, so the line an
	// operator reads names the workspace the way they typed it.
	tgt, err := Resolve(ctxT(t), store(pool), "", ws.Slug)
	if err != nil {
		t.Fatalf("Resolve by slug: %v", err)
	}
	if tgt.ID != ws.ID || tgt.Slug != ws.Slug {
		t.Fatalf("Resolve by slug = %+v, want id=%s slug=%s", tgt, ws.ID, ws.Slug)
	}

	// Naming the same workspace by both is allowed...
	tgt, err = Resolve(ctxT(t), store(pool), ws.ID, ws.Slug)
	if err != nil {
		t.Fatalf("Resolve by id AND slug: %v", err)
	}
	if tgt.ID != ws.ID {
		t.Fatalf("Resolve by id AND slug = %+v, want %s", tgt, ws.ID)
	}

	// ...but naming two DIFFERENT workspaces is refused: verifying one while the
	// k6 run hammers the other is a green check that proves nothing, which is the
	// class this guard exists to close.
	other := newWorkspace(t, pool)
	_, err = Resolve(ctxT(t), store(pool), other.ID, ws.Slug)
	if err == nil {
		t.Fatal("Resolve with a DIFFERENT id and slug succeeded; want a refusal")
	}
	if got := ExitCode(err); got != ExitUsage {
		t.Errorf("mismatched id/slug exit code = %d, want %d", got, ExitUsage)
	}
	if !strings.Contains(err.Error(), "DIFFERENT workspaces") {
		t.Errorf("message does not explain the mismatch: %v", err)
	}
}

func TestExitCodeMapping(t *testing.T) {
	arms := []struct {
		name string
		err  error
		want int
	}{
		{"nil is verified-off", nil, ExitOK},
		{"usage", fmt.Errorf("%w: nope", ErrUsage), ExitUsage},
		{"judge on", fmt.Errorf("%w: nope", ErrJudgeOn), ExitJudgeOn},
		{"not applied", fmt.Errorf("%w: nope", ErrNotApplied), ExitJudgeOn},
		{"unverified", fmt.Errorf("%w: nope", ErrUnverified), ExitUnverified},
		{"unknown errors fail closed", errors.New("boom"), ExitUnverified},
	}
	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			if got := ExitCode(a.err); got != a.want {
				t.Errorf("ExitCode(%v) = %d, want %d", a.err, got, a.want)
			}
		})
	}
	// A refusal must never share the exit code of a verified pass: the sweep
	// runner only distinguishes them by the number.
	if ExitJudgeOn == ExitOK || ExitUnverified == ExitOK || ExitUsage == ExitOK {
		t.Fatal("a refusal shares ExitOK")
	}
}

func TestRedactDSNDropsCredentials(t *testing.T) {
	got := RedactDSN("postgres://jomform:s3cr3t@staging.example.com:5432/jomform?sslmode=require")
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("RedactDSN leaked the password: %q", got)
	}
	if !strings.Contains(got, "staging.example.com:5432/jomform") {
		t.Errorf("RedactDSN dropped the useful part: %q", got)
	}
	if got := RedactDSN("not a dsn"); strings.Contains(got, "not a dsn") {
		t.Errorf("RedactDSN echoed an unparseable string: %q", got)
	}
}

// ── B. the refusal ───────────────────────────────────────────────────────────

func TestCheckRefusesWhenTheJudgeIsOn(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)
	setToggle(t, pool, ws.ID, true)

	st, err := Check(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug})
	if err == nil {
		t.Fatalf("Check succeeded on a judge-ON workspace (state %+v); want ErrJudgeOn", st)
	}
	if !errors.Is(err, ErrJudgeOn) {
		t.Fatalf("Check error = %v, want ErrJudgeOn", err)
	}
	if got := ExitCode(err); got != ExitJudgeOn {
		t.Errorf("exit code = %d, want %d", got, ExitJudgeOn)
	}
	for _, want := range []string{ws.ID, "spam_judge_enabled = true", "reads ON too"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal message does not mention %q: %v", want, err)
		}
	}
	// A CHECK MUST NOT MUTATE: a guard that turned the judge off as a side effect
	// would "pass" by changing the thing it verifies.
	if row := readRow(t, pool, ws.ID); !row.On {
		t.Error("Check changed the stored toggle")
	}
}

// TestCheckRefusesForAWorkspaceWithNoSettingsRow pins the DEFAULT: a workspace
// whose settings row was never materialised behaves as ON (the column's
// NOT NULL DEFAULT TRUE, and WorkspaceStore.Settings' own fallback literal), so a
// load run against a freshly seeded staging workspace spends model calls unless
// someone turns the judge off. This is the arm for the assumption the original
// incident rested on — that a workspace with no settings row had nothing to
// configure.
func TestCheckRefusesForAWorkspaceWithNoSettingsRow(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)

	var rows int
	if err := pool.QueryRow(ctxT(t),
		`SELECT count(*) FROM workspace_settings WHERE workspace_id = $1`, ws.ID).Scan(&rows); err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("this arm needs a workspace with NO settings row, found %d", rows)
	}

	st, err := Check(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug})
	if err == nil {
		t.Fatalf("Check passed for a workspace with no settings row (state %+v); the app reads it as ON", st)
	}
	if !errors.Is(err, ErrJudgeOn) {
		t.Fatalf("Check error = %v, want ErrJudgeOn", err)
	}
	if !strings.Contains(err.Error(), "does not exist yet") {
		t.Errorf("refusal does not explain that a missing row reads ON: %v", err)
	}
}

func TestCheckPassesWhenTheJudgeIsOff(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)
	setToggle(t, pool, ws.ID, false)

	st, err := Check(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug})
	if err != nil {
		t.Fatalf("Check on a judge-OFF workspace: %v", err)
	}
	if st.Enabled {
		t.Fatal("State.Enabled = true after a judge-OFF read")
	}
}

// TestCheckFailsClosedWhenTheDatabaseCannotBeRead: a guard that reported "safe"
// when it could not read the toggle would let exactly the unverified sweep it
// exists to prevent start.
func TestCheckFailsClosedWhenTheDatabaseCannotBeRead(t *testing.T) {
	if dsn() == "" {
		t.Skipf("%s not set; skipping integration test", testdb.DSNEnv)
	}
	// A pool of its own, closed before use: the read then fails the way an
	// unreachable database does, without disturbing the package pool the other
	// arms share.
	pool, err := pgxpool.New(ctxT(t), dsn())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	pool.Close()

	st, err := Check(ctxT(t), store(pool), &Target{ID: "00000000-0000-0000-0000-000000000000"})
	if err == nil {
		t.Fatalf("Check succeeded against a closed pool (state %+v); want ErrUnverified", st)
	}
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("Check error = %v, want ErrUnverified", err)
	}
	if got := ExitCode(err); got != ExitUnverified {
		t.Errorf("exit code = %d, want %d", got, ExitUnverified)
	}
}

// ── C. the write + the READ-BACK ─────────────────────────────────────────────

// TestDisableWritesOffAndKeepsEveryOtherSetting is the read-modify-write pin:
// SaveSettings writes EVERY column from the struct it is handed, so a caller
// that built a fresh literal would wipe the merchant's storefront config while
// turning the judge off.
func TestDisableWritesOffAndKeepsEveryOtherSetting(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)

	cur, err := store(pool).Settings(ctxT(t), ws.ID)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	cur.SpamJudgeEnabled = true
	cur.FooterText = "kept by the loadcost test"
	cur.Currency = "USD"
	cur.DefaultLang = "ms"
	cur.SoundEnabled = true
	if err := store(pool).SaveSettings(ctxT(t), cur); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	before := readRow(t, pool, ws.ID)
	if !before.On || before.FooterText != "kept by the loadcost test" || before.Currency != "USD" {
		t.Fatalf("the seed did not land: %+v", before)
	}

	st, err := Disable(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if st.Enabled {
		t.Fatal("Disable returned Enabled = true")
	}
	after := readRow(t, pool, ws.ID)
	if after.On {
		t.Error("the stored toggle is still ON after Disable returned success")
	}
	if after.FooterText != before.FooterText || after.Currency != before.Currency ||
		after.DefaultLang != before.DefaultLang || after.SoundOn != before.SoundOn ||
		after.MenuJSON != before.MenuJSON || after.Indexing != before.Indexing {
		t.Errorf("Disable clobbered other settings: before %+v after %+v", before, after)
	}
}

// TestDisableCreatesTheRowWhenItIsMissing also settles the SQL claim that once
// circulated: `INSERT INTO workspace_settings (workspace_id, spam_judge_enabled)
// VALUES (..., false)` is VALID, because every other column is `NOT NULL DEFAULT
// ...` (api/internal/db/migrations/000012_spaces.up.sql — the same defaults the
// app itself relies on when it inserts a bare workspace_id).
func TestDisableCreatesTheRowWhenItIsMissing(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)
	if row := readRow(t, pool, ws.ID); row.Exists {
		t.Fatalf("this arm needs a workspace with NO settings row, found %+v", row)
	}

	if _, err := Disable(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug}); err != nil {
		t.Fatalf("Disable on a workspace with no settings row: %v", err)
	}
	after := readRow(t, pool, ws.ID)
	if !after.Exists {
		t.Fatal("Disable did not create the settings row")
	}
	if after.On {
		t.Error("the created row has spam_judge_enabled = true")
	}
	if after.MenuJSON != "[]" || after.Currency != "MYR" || after.DefaultLang != "en" ||
		after.FooterText != "" || after.SoundOn || !after.Indexing {
		t.Errorf("the created row did not take the migration defaults: %+v", after)
	}
}

// TestDisableRefusesWhenTheWriteDoesNotTake is the arm the read-back exists for.
// A BEFORE UPDATE trigger that returns NULL silently CANCELS the update: the
// statement reports no error and changes nothing — the database-level version of
// a write that is "accepted" and does not take effect. A guard that trusted the
// write's own status would call this a success and let the sweep run.
func TestDisableRefusesWhenTheWriteDoesNotTake(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)
	setToggle(t, pool, ws.ID, true)

	if _, err := pool.Exec(ctxT(t),
		`CREATE OR REPLACE FUNCTION loadcost_noop_update() RETURNS trigger AS
		 $$ BEGIN RETURN NULL; END; $$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create the sabotage function: %v", err)
	}
	if _, err := pool.Exec(ctxT(t),
		`CREATE TRIGGER loadcost_noop_update_trg BEFORE UPDATE ON workspace_settings
		 FOR EACH ROW EXECUTE FUNCTION loadcost_noop_update()`); err != nil {
		t.Fatalf("create the sabotage trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TRIGGER IF EXISTS loadcost_noop_update_trg ON workspace_settings`); err != nil {
			t.Errorf("drop the sabotage trigger: %v", err)
		}
	})

	st, err := Disable(ctxT(t), store(pool), &Target{ID: ws.ID, Slug: ws.Slug})
	if err == nil {
		t.Fatalf("Disable reported success (state %+v) although the row was never updated", st)
	}
	if !errors.Is(err, ErrNotApplied) {
		t.Fatalf("Disable error = %v, want ErrNotApplied", err)
	}
	if got := ExitCode(err); got != ExitJudgeOn {
		t.Errorf("exit code = %d, want %d", got, ExitJudgeOn)
	}
	if !strings.Contains(err.Error(), "did not take effect") {
		t.Errorf("message does not say the write did not take effect: %v", err)
	}
	// The sabotage must have APPLIED, or this arm would pass for the wrong
	// reason (an update that silently worked would satisfy every assertion above).
	if row := readRow(t, pool, ws.ID); !row.On {
		t.Fatal("the row really was updated — the sabotage did not apply, so this arm proves nothing")
	}
}

// ── D. the command, through the real binary ──────────────────────────────────

// TestCommandCheckExitCodes drives the whole chain — wrapper, built binary, flag
// parsing, store, Postgres — because the exit codes are the contract the sweep
// runner reads, and only running it proves they survive the layers.
func TestCommandCheckExitCodes(t *testing.T) {
	pool := testdb.Open(t)
	theDSN := dsn()
	wsOff := newWorkspace(t, pool)
	setToggle(t, pool, wsOff.ID, false)
	wsOn := newWorkspace(t, pool)
	setToggle(t, pool, wsOn.ID, true)
	env := childEnv(t, pool)

	t.Run("judge off exits 0 and names where it checked", func(t *testing.T) {
		out, errs, code := runGuard(t, env, "check", "--workspace", wsOff.ID, "--database-url", theDSN)
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, ExitOK, out, errs)
		}
		if !strings.Contains(out, "spam_judge_enabled=false") {
			t.Errorf("stdout does not report the verified state: %s", out)
		}
		// The line names WHERE the check ran (so an operator can see which
		// database was certified) and NEVER the credentials.
		u, perr := url.Parse(theDSN)
		if perr == nil {
			if u.Host != "" && !strings.Contains(out, u.Host) {
				t.Errorf("stdout does not name the database host %q: %s", u.Host, out)
			}
			if pw, ok := u.User.Password(); ok && pw != "" && strings.Contains(out+errs, pw) {
				t.Error("the guard printed the database password")
			}
		}
		// A CHECK IS A READ, and the binary's own line must say so.
		if !strings.Contains(out, "nothing was changed") {
			t.Errorf("stdout does not say the check changed nothing: %s", out)
		}
	})

	t.Run("judge on exits 3", func(t *testing.T) {
		out, errs, code := runGuard(t, env, "check", "--workspace", wsOn.ID, "--database-url", theDSN)
		if code != ExitJudgeOn {
			t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, ExitJudgeOn, out, errs)
		}
		if !strings.Contains(errs, "spam_judge_enabled = true") || !strings.Contains(errs, wsOn.ID) {
			t.Errorf("the refusal does not name the workspace and the state: %s", errs)
		}
		if !strings.Contains(errs, "jf-cost-guard disable") {
			t.Errorf("the refusal does not say how to clear it: %s", errs)
		}
		if row := readRow(t, pool, wsOn.ID); !row.On {
			t.Error("the check changed the stored toggle")
		}
	})

	t.Run("judge on by slug exits 3", func(t *testing.T) {
		out, errs, code := runGuard(t, env, "check", "--space", wsOn.Slug, "--database-url", theDSN)
		if code != ExitJudgeOn {
			t.Fatalf("exit code = %d, want %d (stdout %s, stderr %s)", code, ExitJudgeOn, out, errs)
		}
	})

	t.Run("unknown slug exits 4", func(t *testing.T) {
		out, errs, code := runGuard(t, env, "check", "--space", "lc-no-such-space", "--database-url", theDSN)
		if code != ExitUnverified {
			t.Fatalf("exit code = %d, want %d (stdout %s, stderr %s)", code, ExitUnverified, out, errs)
		}
		if !strings.Contains(errs, "lc-no-such-space") {
			t.Errorf("the refusal does not name the slug: %s", errs)
		}
	})

	t.Run("no database-url exits 1 before connecting", func(t *testing.T) {
		out, errs, code := runGuard(t, append(env, "DATABASE_URL="), "check", "--workspace", wsOff.ID)
		if code != ExitUsage {
			t.Fatalf("exit code = %d, want %d (stdout %s, stderr %s)", code, ExitUsage, out, errs)
		}
		if !strings.Contains(errs, "--database-url") {
			t.Errorf("the usage error does not name the missing flag: %s", errs)
		}
	})

	t.Run("an unreachable database exits 4 and leaks no password", func(t *testing.T) {
		bad := "postgres://loadcost:hunter2@127.0.0.1:1/absent?sslmode=disable&connect_timeout=1"
		out, errs, code := runGuard(t, env, "check", "--workspace", wsOff.ID, "--database-url", bad)
		if code != ExitUnverified {
			t.Fatalf("exit code = %d, want %d (stdout %s, stderr %s)", code, ExitUnverified, out, errs)
		}
		if strings.Contains(out+errs, "hunter2") {
			t.Error("a connection failure printed the password")
		}
		if !strings.Contains(errs, "127.0.0.1:1") {
			t.Errorf("the refusal does not name the database that could not be reached: %s", errs)
		}
	})
}

// TestCommandDisableTurnsTheJudgeOffAndProvesIt is the operator's path: one
// command, and the row is really false afterwards.
func TestCommandDisableTurnsTheJudgeOffAndProvesIt(t *testing.T) {
	pool := testdb.Open(t)
	ws := newWorkspace(t, pool)
	setToggle(t, pool, ws.ID, true)

	out, errs, code := runGuard(t, childEnv(t, pool),
		"disable", "--workspace", ws.ID, "--database-url", dsn())
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, ExitOK, out, errs)
	}
	if !strings.Contains(out, "FRESH read") {
		t.Errorf("the success line does not say the verdict came from a fresh read: %s", out)
	}
	if row := readRow(t, pool, ws.ID); !row.Exists || row.On {
		t.Errorf("after disable the stored row is %+v, want exists=true on=false", row)
	}
	// And the check then certifies it: the two commands agree.
	if _, errs, code := runGuard(t, childEnv(t, pool),
		"check", "--workspace", ws.ID, "--database-url", dsn()); code != ExitOK {
		t.Fatalf("check after disable exit code = %d, want %d (stderr %s)", code, ExitOK, errs)
	}
}

// ── D'. the wrapper's build-and-reuse contract ───────────────────────────────

// TestWrapperBuildsThenReusesTheBinary pins the wrapper's caching rule with a
// stubbed `go`: the binary is built from ./api/cmd/jf-cost-guard into the cache
// path, its arguments are passed through unchanged, and a second call does NOT
// rebuild (a rebuild on every load run would be a cost this guard must not add).
func TestWrapperBuildsThenReusesTheBinary(t *testing.T) {
	root := repoRootOrFail(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "cache", "jf-cost-guard")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	goLog := filepath.Join(dir, "go.argv")
	writeStub(t, dir, "go", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$JF_TEST_GO_LOG"
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$out" ]; then echo "stub go: no -o <path>" >&2; exit 2; fi
cat > "$out" <<'STUBGUARD'
#!/usr/bin/env bash
printf 'STUB-GUARD %s\n' "$*"
exit 0
STUBGUARD
chmod +x "$out"
`)
	env := []string{
		"GO=" + filepath.Join(dir, "go"),
		"JF_COST_GUARD_BIN=" + bin,
		"JF_TEST_GO_LOG=" + goLog,
		"PATH=" + dir + ":" + os.Getenv("PATH"),
	}
	wrapper := filepath.Join(root, "loadtest", "cost-guard.sh")

	out, errs, code := run(t, root, env, wrapper, "check", "--space", "demo")
	if code != 0 {
		t.Fatalf("first wrapper call exit code = %d\nstdout: %s\nstderr: %s", code, out, errs)
	}
	if !strings.Contains(out, "STUB-GUARD check --space demo") {
		t.Errorf("the wrapper did not exec the built binary with the caller's arguments: %s", out)
	}
	first := readFileOrFail(t, goLog)
	if !strings.Contains(first, "build -o "+bin) || !strings.Contains(first, "./cmd/jf-cost-guard") {
		t.Errorf("the wrapper did not build the guard command into the cache path: %q", first)
	}
	if n := len(strings.Split(strings.TrimSpace(first), "\n")); n != 1 {
		t.Errorf("expected exactly one build on the first call, got %d: %q", n, first)
	}

	out2, errs2, code2 := run(t, root, env, wrapper, "check", "--space", "demo")
	if code2 != 0 {
		t.Fatalf("second wrapper call exit code = %d (stderr %s)", code2, errs2)
	}
	second := readFileOrFail(t, goLog)
	if len(strings.Split(strings.TrimSpace(second), "\n")) != 1 {
		t.Errorf("the wrapper REBUILT on the second call: %q", second)
	}
	if out2 != out {
		t.Errorf("the second call behaved differently:\nfirst:  %q\nsecond: %q", out, out2)
	}
}

// TestWrapperFailsClosedWhenTheBuildFails: an unbuildable guard must refuse the
// sweep, not pass it.
func TestWrapperFailsClosedWhenTheBuildFails(t *testing.T) {
	root := repoRootOrFail(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "cache", "jf-cost-guard")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	writeStub(t, dir, "go", "#!/usr/bin/env bash\necho 'stub go: build failed' >&2\nexit 1\n")
	env := []string{
		"GO=" + filepath.Join(dir, "go"),
		"JF_COST_GUARD_BIN=" + bin,
		"PATH=" + dir + ":" + os.Getenv("PATH"),
	}
	out, errs, code := run(t, root, env, filepath.Join(root, "loadtest", "cost-guard.sh"),
		"check", "--space", "demo")
	if code == 0 {
		t.Fatalf("the wrapper exited 0 although the build failed\nstdout: %s", out)
	}
	if !strings.Contains(errs, "building") {
		t.Errorf("the wrapper did not say it was building: %s", errs)
	}
	if strings.Contains(out, "STUB-GUARD") {
		t.Errorf("the wrapper exec'd a binary that was never built: %s", out)
	}
}

func readFileOrFail(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// ── E. the sweep runner obeys the refusal ────────────────────────────────────

// TestBaselineRefusesTheSweepWhenTheGuardRefuses is the wiring arm. The guard can
// be perfectly correct and still protect nothing if the runner ignores it — the
// dead-call-site failure the repo's other suites pin. The guard and k6 are both
// stubbed, so this arm is about the WIRING and nothing else.
func TestBaselineRefusesTheSweepWhenTheGuardRefuses(t *testing.T) {
	root := repoRootOrFail(t)
	dir := t.TempDir()
	guardLog := filepath.Join(dir, "guard.argv")
	k6Log := filepath.Join(dir, "k6.argv")
	writeStub(t, dir, "stub-guard", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$JF_TEST_GUARD_LOG"
exit "${JF_TEST_GUARD_RC:-0}"
`)
	writeStub(t, dir, "k6", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$JF_TEST_K6_LOG"
exit 0
`)
	const wsID = "11111111-2222-3333-4444-555555555555"
	env := []string{
		"COST_GUARD=" + filepath.Join(dir, "stub-guard"),
		"JF_TEST_GUARD_RC=3",
		"JF_TEST_GUARD_LOG=" + guardLog,
		"JF_TEST_K6_LOG=" + k6Log,
		"WORKSPACE_ID=" + wsID,
		"DATABASE_URL=postgres://loadcost:pw@127.0.0.1:5432/none",
		"SPACE=lcspace",
		"VUS_LIST=10",
		"DURATION=1s",
		"RAMP=1s",
		"OUT_DIR=" + filepath.Join(dir, "results"),
		"PATH=" + dir + ":" + os.Getenv("PATH"),
	}

	out, errs, code := run(t, root, env, filepath.Join(root, "loadtest", "baseline.sh"))
	if code == 0 {
		t.Fatalf("baseline.sh exited 0 although the cost guard refused (a load run would have spent model calls)\nstdout: %s", out)
	}
	if _, err := os.Stat(k6Log); !os.IsNotExist(err) {
		t.Fatalf("k6 RAN although the guard refused: %s", readFileOrFail(t, k6Log))
	}
	if !strings.Contains(errs, "ABORTED") {
		t.Errorf("the abort is not announced: %s", errs)
	}
	argv := readFileOrFail(t, guardLog)
	if !strings.Contains(argv, "check") || !strings.Contains(argv, "--workspace "+wsID) {
		t.Errorf("the guard was not asked to CHECK the named workspace: %q", argv)
	}
	if !strings.Contains(argv, "--database-url") {
		t.Errorf("the guard was not given the database URL: %q", argv)
	}
}

// TestBaselineRunsTheSweepWhenTheGuardCertifies is the control arm: without it,
// "baseline.sh refuses" could be satisfied by a runner that never runs anything.
func TestBaselineRunsTheSweepWhenTheGuardCertifies(t *testing.T) {
	root := repoRootOrFail(t)
	dir := t.TempDir()
	guardLog := filepath.Join(dir, "guard.argv")
	k6Log := filepath.Join(dir, "k6.argv")
	writeStub(t, dir, "stub-guard", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$JF_TEST_GUARD_LOG"
exit "${JF_TEST_GUARD_RC:-0}"
`)
	writeStub(t, dir, "k6", `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$JF_TEST_K6_LOG"
exit 0
`)
	env := []string{
		"COST_GUARD=" + filepath.Join(dir, "stub-guard"),
		"JF_TEST_GUARD_RC=0",
		"JF_TEST_GUARD_LOG=" + guardLog,
		"JF_TEST_K6_LOG=" + k6Log,
		"SPACE=lcspace",
		"VUS_LIST=10",
		"DURATION=1s",
		"RAMP=1s",
		"OUT_DIR=" + filepath.Join(dir, "results"),
		"PATH=" + dir + ":" + os.Getenv("PATH"),
		"DATABASE_URL=",
		"WORKSPACE_ID=",
	}

	out, errs, code := run(t, root, env, filepath.Join(root, "loadtest", "baseline.sh"))
	if code != 0 {
		t.Fatalf("baseline.sh exit code = %d, want 0\nstdout: %s\nstderr: %s", code, out, errs)
	}
	k6 := readFileOrFail(t, k6Log)
	if !strings.Contains(k6, "loadtest/public-form.js") {
		t.Errorf("k6 was not run on the public-form scenario: %q", k6)
	}
	if n := len(strings.Split(strings.TrimSpace(k6), "\n")); n != 1 {
		t.Errorf("expected one k6 run for VUS_LIST=10, got %d: %q", n, k6)
	}
	// With no WORKSPACE_ID the guard is asked by SPACE — the same knob the k6
	// script uses, so the workspace verified is the workspace hammered.
	argv := readFileOrFail(t, guardLog)
	if !strings.Contains(argv, "--space lcspace") {
		t.Errorf("the guard was not asked about the k6 SPACE: %q", argv)
	}
	if strings.Contains(argv, "--database-url") {
		t.Errorf("the guard was given a database URL that was not set: %q", argv)
	}
}
