package httpapi

// The per-workspace AI spam-judge toggle.
//
// THE BUG THIS GUARDS AGAINST (the real incident): form `event-registration` on
// the production account had 4/4 real registrations flagged as spam by the
// background AI judge (gemma4). A flagged entry is not merely mislabelled: the
// hourly retention sweep runs `DELETE FROM submissions WHERE is_spam = true AND
// created_at < $1` (dbstore/submission.go), so an unreviewed flag is permanent
// data loss after 30 days. There was no way to turn the judge off.
//
// THE CONTRACT PINNED HERE (four separate properties, each of which can break
// silently on its own):
//
//   1. Judge OFF  → NO job reaches the queue. The gate is on the ENQUEUE
//      (`PublicHandlers.enqueueSpamJudge`), not inside the worker, so a disabled
//      workspace spends no AI work — asserting only "is_spam stayed false"
//      would NOT catch a regression that still enqueued the job, because nothing
//      consumes the queue in this test and the flag would stay false anyway.
//   2. Judge ON (the default) → exactly one job, carrying the submission id.
//      This is the regression proof for existing workspaces: adding the column
//      must not change their behaviour.
//   3. A workspace that has never written the setting reads as ENABLED. Pinned
//      against a really-migrated, really-empty row (`Settings()` on a workspace
//      with no workspace_settings row), so the `NOT NULL DEFAULT TRUE` column
//      default and the `Settings()` fallback literal are both exercised.
//   4. An unrelated settings save does not switch the judge back on or off.
//      `SaveSettings` writes EVERY column from the struct it is handed, so a
//      caller that builds a fresh literal instead of read-modify-writing `cur`
//      would persist `false` and silently kill spam detection. That is the
//      named hazard of the positive-named field, and it is pinned through the
//      real REST handler, not the store.
//
// WHY THE TOGGLE IS GATED ON THE ENQUEUE AND REACHES BOTH PATHS: the resolution
// goes account → `accounts.workspace_id` → `workspace_settings`, which is the
// same accessor shape as `IndexingByAccount`. `form` may be nil (formless
// menu-order path, issue #491) and the gate does not read the form at all, so
// that path is gated identically.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) and real
// Valkey (JOMFORM_TEST_VALKEY_URL); skipped when either is unset. The queue uses
// a DEDICATED Valkey database (…/4) and flushes it, so it can never disturb the
// auth suite (…/1), the email breaker (…/2) or the email queue (…/3) that run
// concurrently under `go test ./...`.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/spamjudge"
)

// issue622Queue returns a queue on this suite's own Valkey database, flushed
// once so a previous run's jobs cannot be counted.
func issue622Queue(t *testing.T) *emailqueue.Queue {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping spam-judge toggle integration test")
	}
	u := strings.TrimPrefix(valkeyURL, "valkey://")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[:i]
	}
	opts, err := goredis.ParseURL("redis://" + u + "/4")
	if err != nil {
		t.Fatalf("parse valkey url: %v", err)
	}
	rdb := goredis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	issue622FlushOnce.Do(func() {
		if err := rdb.FlushDB(context.Background()).Err(); err != nil {
			t.Fatalf("flush issue622 valkey db: %v", err)
		}
	})
	return emailqueue.NewQueue(rdb)
}

var issue622FlushOnce sync.Once

// issue622Fixture is one workspace with a published response-mode form, wired
// PublicHandlers carrying a real queue, and the ids the assertions need.
type issue622Fixture struct {
	h           *PublicHandlers
	q           *emailqueue.Queue
	pool        *pgxpool.Pool
	accountID   string
	workspaceID string
	workspaceSl string
	formSlug    string
}

func issue622New(t *testing.T) issue622Fixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i622-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i622-' || md5(random()::text), 'i622-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'i622-acc-' || md5(random()::text), 'i622-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "i622-form-" + accountID[:8]
	if err := fs.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "I622 response form",
		Status: "published",
		Config: map[string]any{
			"mode": "response",
			"blocks": []any{
				map[string]any{"id": "b1", "type": "name", "value": "Full name", "required": true},
				map[string]any{"id": "b2", "type": "email", "value": "Email", "required": true},
			},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}

	q := issue622Queue(t)
	h := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		SpamQueue:   q,
		BaseURL:     "http://localhost:8080",
	}
	return issue622Fixture{
		h: h, q: q, pool: pool,
		accountID: accountID, workspaceID: workspaceID,
		workspaceSl: workspaceSlug, formSlug: formSlug,
	}
}

// setJudge writes the toggle straight to the column, so the test's setup cannot
// itself depend on the code under test.
func (f issue622Fixture) setJudge(t *testing.T, on bool) {
	t.Helper()
	sp := dbstore.NewWorkspaceStore(f.pool)
	if _, err := sp.Settings(context.Background(), f.workspaceID); err != nil {
		t.Fatalf("materialise settings row: %v", err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`UPDATE workspace_settings SET spam_judge_enabled = $2 WHERE workspace_id = $1 RETURNING spam_judge_enabled`,
		f.workspaceID, on).Scan(new(bool)); err != nil {
		t.Fatalf("set spam_judge_enabled=%v: %v", on, err)
	}
}

// submit posts one real response to the fixture's form through the real public
// handler and returns the submission id.
func (f issue622Fixture) submit(t *testing.T) string {
	t.Helper()
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},"fields":{"b1":"Ali","b2":"ali@example.test"}}`
	mux := http.NewServeMux()
	f.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+f.formSlug+"/order", bytes.NewBufferString(body))
	req.Host = f.workspaceSl + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("submit returned no submission id")
	}
	return resp.ID
}

// queueLens drains nothing; it only reports the ready-list length.
func (f issue622Fixture) queueLen(t *testing.T) int64 {
	t.Helper()
	n, err := f.q.Len(context.Background())
	if err != nil {
		t.Fatalf("queue len: %v", err)
	}
	return n
}

// TestIssue622JudgeOffEnqueuesNoJob — the fix. With the workspace toggle off, a
// real submission must put NOTHING on the queue and must not be flagged.
//
// BITE PROOF: delete the `SpamJudgeEnabledByAccount` call from
// enqueueSpamJudge and this test fails with
//
//	"submission to workspace i622-… enqueued 1 spam-judge job(s) with the judge OFF"
//
// — it names the workspace, so a partial gate (e.g. one that only covers the
// form path) is identified rather than passing silently.
func TestIssue622JudgeOffEnqueuesNoJob(t *testing.T) {
	f := issue622New(t)
	f.setJudge(t, false)

	// Confirm the store really reads it as OFF — if the resolution were broken
	// and returned true, the assertion below would fail for the wrong reason.
	sp := dbstore.NewWorkspaceStore(f.pool)
	if on, err := sp.SpamJudgeEnabledByAccount(context.Background(), f.accountID); err != nil {
		t.Fatalf("SpamJudgeEnabledByAccount: %v", err)
	} else if on {
		t.Fatalf("workspace %s reads as spam-judge ENABLED after writing false", f.workspaceID)
	}

	before := f.queueLen(t)
	subID := f.submit(t)
	if after := f.queueLen(t); after != before {
		t.Errorf("submission to workspace %s enqueued %d spam-judge job(s) with the judge OFF — "+
			"the per-workspace toggle is not gating the enqueue (workspace id %s)",
			f.workspaceSl, after-before, f.workspaceID)
	}

	// The entry is stored (the submit is not blocked — the toggle only removes
	// the judge, it never the submission) and carries no spam flag.
	sub, err := f.h.Submissions.ByID(context.Background(), subID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.IsSpam {
		t.Errorf("submission %s is_spam = true with the judge OFF", subID)
	}
	if sub.Data["b2"] != "ali@example.test" {
		t.Errorf("submission b2 = %v, want ali@example.test (the entry must still be recorded)", sub.Data["b2"])
	}
}

// TestIssue622JudgeOnByDefaultEnqueuesJob — the regression proof: a workspace
// that never touched the setting keeps the pre-toggle behaviour exactly.
func TestIssue622JudgeOnByDefaultEnqueuesJob(t *testing.T) {
	f := issue622New(t)
	// No write at all: the workspace_settings row may not even exist yet, which
	// is the "existing workspace, freshly migrated" shape.
	sp := dbstore.NewWorkspaceStore(f.pool)
	if on, err := sp.SpamJudgeEnabledByAccount(context.Background(), f.accountID); err != nil {
		t.Fatalf("SpamJudgeEnabledByAccount: %v", err)
	} else if !on {
		t.Fatalf("workspace %s defaults to spam-judge DISABLED — existing workspaces would lose spam protection", f.workspaceID)
	}

	before := f.queueLen(t)
	subID := f.submit(t)
	if after := f.queueLen(t); after != before+1 {
		t.Fatalf("submission with the judge ON enqueued %d job(s), want exactly 1 — spam protection is off by default",
			after-before)
	}

	job, raw, err := f.q.DequeueRaw(context.Background())
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job.Type != spamjudge.JobType {
		t.Errorf("job type = %q, want %q", job.Type, spamjudge.JobType)
	}
	var payload spamjudge.Job
	if err := json.Unmarshal([]byte(job.Body), &payload); err != nil {
		t.Fatalf("unmarshal job body: %v", err)
	}
	if payload.SubmissionID != subID {
		t.Errorf("job submission id = %q, want %q", payload.SubmissionID, subID)
	}
	if payload.AccountID != f.accountID {
		t.Errorf("job account id = %q, want %q", payload.AccountID, f.accountID)
	}
	t.Cleanup(func() { _ = f.q.Ack(context.Background(), raw) })
}

// TestIssue622SettingsDefaultIsEnabledForAnUnwrittenRow pins the migration's
// `NOT NULL DEFAULT TRUE` against a genuinely row-less workspace: Settings()
// falls back to its own literal when no row exists, so BOTH defaults have to
// agree or a brand-new workspace would silently lose spam detection.
func TestIssue622SettingsDefaultIsEnabledForAnUnwrittenRow(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i622d-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i622d-' || md5(random()::text), 'i622d-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	var rows int64
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_settings WHERE workspace_id = $1`, workspaceID).Scan(&rows); err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("this test needs a workspace with NO workspace_settings row, found %d", rows)
	}

	st, err := dbstore.NewWorkspaceStore(pool).Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if !st.SpamJudgeEnabled {
		t.Error("a workspace with no settings row reads spam_judge_enabled = false — " +
			"a brand-new workspace would ship with spam detection silently off")
	}
	// The literal and the column default must agree; read the row Settings()
	// just materialised.
	var stored bool
	if err := pool.QueryRow(ctx,
		`SELECT spam_judge_enabled FROM workspace_settings WHERE workspace_id = $1`, workspaceID).Scan(&stored); err != nil {
		t.Fatalf("read materialised row: %v", err)
	}
	if !stored {
		t.Error("the materialised workspace_settings row has spam_judge_enabled = false — " +
			"the column default disagrees with the Settings() fallback")
	}
}

// TestIssue622UnrelatedSettingsSaveKeepsTheToggle is the named hazard of the
// positive-named field, driven through the REAL REST handler: SaveSettings
// writes every column from the struct it is given, so if
// saveWorkspaceSettings stopped read-modify-writing the current value, a plain
// "save the footer" would persist spam_judge_enabled = false and silently kill
// spam detection for the workspace.
func TestIssue622UnrelatedSettingsSaveKeepsTheToggle(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i622s-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i622s-' || md5(random()::text), 'i622s-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'i622s-acc-' || md5(random()::text), 'i622s-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	sp := dbstore.NewWorkspaceStore(pool)
	if _, err := sp.Settings(ctx, workspaceID); err != nil {
		t.Fatalf("materialise settings row: %v", err)
	}
	// Turn the judge OFF, then save something completely unrelated through the
	// real handler and require it to stay off.
	if err := pool.QueryRow(ctx,
		`UPDATE workspace_settings SET spam_judge_enabled = false WHERE workspace_id = $1 RETURNING spam_judge_enabled`,
		workspaceID).Scan(new(bool)); err != nil {
		t.Fatalf("turn judge off: %v", err)
	}

	h := newBundleHandlers(pool)
	// Session auth (the dashboard/MCP path): the route is registered behind
	// `authed`, so an API key alone is not enough for the write.
	sess := authTestValkey(t)
	h.Sessions = sess
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+workspaceID+"/settings",
		bytes.NewBufferString(`{"footer_text":"saved by an unrelated settings change"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var stored bool
	if err := pool.QueryRow(ctx,
		`SELECT spam_judge_enabled FROM workspace_settings WHERE workspace_id = $1`, workspaceID).Scan(&stored); err != nil {
		t.Fatalf("read toggle: %v", err)
	}
	if stored {
		t.Errorf("workspace %s: an unrelated settings save turned the spam judge back ON "+
			"(it was explicitly off) — the read-modify-write no longer preserves the field", workspaceID)
	}

	// And the read surface the dashboard/MCP use reports the same value.
	got, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if got.SpamJudgeEnabled {
		t.Error("GET after the unrelated save reports spam_judge_enabled = true")
	}
	if got.FooterText != "saved by an unrelated settings change" {
		t.Errorf("footer_text = %q, want the value that was saved", got.FooterText)
	}
}
