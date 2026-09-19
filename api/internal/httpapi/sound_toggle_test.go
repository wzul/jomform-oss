package httpapi

// The per-workspace realtime notification sound setting (issue #447).
//
// WHY THIS TEST EXISTS: the /sales notification-sound switch is the only UI that
// writes sound_enabled, and it writes EXACTLY ONE field —
// `{"sound_enabled": true}` on `PUT /v1/workspaces/{id}/settings`. That is only
// correct because saveWorkspaceSettings is a read-modify-write: it loads the
// stored settings and merges the supplied fields over them. If that merge stops
// applying to this field, the single-field request would silently do nothing (or
// drop another setting), and the dashboard switch would report success for a
// value the server never stored — the same class of silent failure issue #622
// pinned for the spam judge.
//
// The properties pinned here are the SERVER half of the contract the dashboard
// switch relies on (the browser half — load, write, revert — is pinned in
// web/src/lib/soundToggle.test.ts and web/src/lib/workspaceSound.test.ts):
//
//   1. A one-field write of `sound_enabled` is applied and reads back through the
//      settings GET that seeds the switch on every page load.
//   2. That write does not disturb the other settings — asserted on the AI
//      spam-judge toggle, the field whose silent clobbering AGENTS.md names as a
//      hazard — and an unrelated settings save does not flip the sound toggle
//      back off.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) and Valkey
// for the session store; skipped when unset, like the sibling suites.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

type soundToggleFixture struct {
	pool        *pgxpool.Pool
	userID      string
	workspaceID string
}

// newSoundToggleFixture creates a real user + workspace and materialises the
// settings row, so a read of sound_enabled is always a read of a row that
// exists (the column default is what the assertions compare against).
func newSoundToggleFixture(t *testing.T) soundToggleFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var f soundToggleFixture
	f.pool = pool
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i447-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&f.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i447-' || md5(random()::text), 'i447-' || md5(random()::text))
		 RETURNING id`, f.userID).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// The account is required: the session-auth path resolves the caller's
	// account (each workspace owns its own account), and with none the request is
	// rejected as unauthenticated before the handler is reached.
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'i447-acc-' || md5(random()::text), 'i447-' || md5(random()::text), id
		 FROM workspaces WHERE id = $2
		 RETURNING id`, f.userID, f.workspaceID).Scan(new(string)); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// Materialise the settings row through the store, the same way any read path
	// does, so the test never depends on the code under test to create it.
	if _, err := dbstore.NewWorkspaceStore(pool).Settings(ctx, f.workspaceID); err != nil {
		t.Fatalf("materialise settings row: %v", err)
	}
	return f
}

// put drives the REAL authenticated handler, so a status code (not just a store
// call) is part of every assertion.
func (f soundToggleFixture) put(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()
	h := newBundleHandlers(f.pool)
	sess := authTestValkey(t)
	h.Sessions = sess
	token, err := sess.Create(ctx, f.userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+f.workspaceID+"/settings",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func (f soundToggleFixture) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()
	h := newBundleHandlers(f.pool)
	sess := authTestValkey(t)
	h.Sessions = sess
	token, err := sess.Create(ctx, f.userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+f.workspaceID+"/settings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// storedSound reads the column directly, so the assertion never goes through the
// same handler that wrote it.
func (f soundToggleFixture) storedSound(t *testing.T) bool {
	t.Helper()
	var on bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT sound_enabled FROM workspace_settings WHERE workspace_id = $1`,
		f.workspaceID).Scan(&on); err != nil {
		t.Fatalf("read sound_enabled: %v", err)
	}
	return on
}

func (f soundToggleFixture) storedSpamJudge(t *testing.T) bool {
	t.Helper()
	var on bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT spam_judge_enabled FROM workspace_settings WHERE workspace_id = $1`,
		f.workspaceID).Scan(&on); err != nil {
		t.Fatalf("read spam_judge_enabled: %v", err)
	}
	return on
}

// TestIssue447SoundToggleRoundTrips is the server half of the switch: enable it
// with the one-field body the page sends, then read the value back through the
// GET the page loads from.
func TestIssue447SoundToggleRoundTrips(t *testing.T) {
	f := newSoundToggleFixture(t)

	// A fresh workspace is muted (the column default), so a write that does
	// nothing is distinguishable from a value that was already true.
	if f.storedSound(t) {
		t.Fatalf("a fresh workspace reads sound_enabled = true; the default must be muted")
	}

	rec := f.put(t, `{"sound_enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT sound_enabled=true status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !f.storedSound(t) {
		t.Fatal("the one-field write the /sales switch sends did not store sound_enabled = true — " +
			"the switch would flip and save nothing (issue #447)")
	}

	// The READ the page performs on mount.
	getRec := f.get(t)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET workspace settings status = %d", getRec.Code)
	}
	var payload struct {
		SoundEnabled bool `json:"sound_enabled"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode settings GET: %v (body %s)", err, getRec.Body.String())
	}
	if !payload.SoundEnabled {
		t.Fatal("GET workspace settings does not report sound_enabled = true, so a reloaded " +
			"/sales page cannot seed its switch from the stored value")
	}

	// And off again: a merchant must be able to silence the beep.
	rec = f.put(t, `{"sound_enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT sound_enabled=false status = %d", rec.Code)
	}
	if f.storedSound(t) {
		t.Fatal("sound_enabled could not be turned off — a merchant could never silence the beep")
	}
}

// TestIssue447SoundToggleSurvivesUnrelatedSettingsSaves pins the merge contract
// from both sides: the one-field write must not disturb another setting, and an
// unrelated save must not switch the sound toggle off.
func TestIssue447SoundToggleSurvivesUnrelatedSettingsSaves(t *testing.T) {
	f := newSoundToggleFixture(t)

	// Turn the AI spam judge OFF first, so a clobber is visible: it is the field
	// AGENTS.md warns is silently overwritten by a whole-struct write.
	if rec := f.put(t, `{"spam_judge_enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT spam_judge_enabled=false status = %d", rec.Code)
	}

	// Now the /sales switch: one field, the sound toggle only.
	if rec := f.put(t, `{"sound_enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT sound_enabled=true status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if !f.storedSound(t) {
		t.Fatal("sound_enabled was not stored by the one-field write")
	}
	if f.storedSpamJudge(t) {
		t.Fatal("the one-field sound write turned the AI spam judge back ON — " +
			"saveWorkspaceSettings is no longer merging the omitted fields")
	}

	// An unrelated save (a footer) must leave the sound toggle ON.
	if rec := f.put(t, `{"footer_text":"saved by an unrelated settings change"}`); rec.Code != http.StatusOK {
		t.Fatalf("unrelated settings save status = %d", rec.Code)
	}
	if !f.storedSound(t) {
		t.Fatal("an unrelated workspace settings save switched the notification sound back OFF — " +
			"the beep would silently disappear the next time the merchant edits any setting")
	}
}
