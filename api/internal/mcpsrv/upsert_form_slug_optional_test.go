package mcpsrv

// upsert_form's `slug` is OPTIONAL on both paths, and the served schema must say
// so — the schema is enforced BEFORE the handler runs, so a `slug` in `required`
// makes the tool's own description a lie and the documented behaviour
// unreachable.
//
// The defect this file pins (card t_186b675c, found while verifying t_41b9c427 on
// prod v1.21.136): `upsertFormIn.Slug` was `json:"slug"` with no `omitempty`, so
// the schema generator (github.com/google/jsonschema-go: "Fields that are marked
// omitempty or omitzero are considered optional; all other fields become required
// properties") listed it as required. The tool Description promised the
// opposite — "slug (pilihan) = … kosongkan untuk kekalkan slug sedia ada" — and
// the handler implemented the opposite too: an ABSENT form_id auto-generates a
// slug (issue #176) and an empty slug on update keeps the stored one. An agent
// that followed the description got, before its arguments ever reached the
// handler:
//
//	tools/call upsert_form {"title":"…","config":{…}}
//	→ {"isError":true,"content":[{"text":"validating \"arguments\": validating
//	   root: required: missing properties: [\"slug\"]"}]}
//
// Two independent things are asserted, because passing one is not passing the
// other:
//
//	TestUpsertFormSchemaDoesNotRequireSlug        (no DB)  — the tools/list schema
//	  an agent is handed does not require `slug`, while `title` (genuinely
//	  required) still is; and the tool Description and the slug property's own
//	  description both still read as optional, so schema and prose cannot drift
//	  apart in either direction.
//	TestUpsertFormWithoutSlugReachesTheHandler    (real DB) — the acceptance: a
//	  real tools/call over the real transport with `slug` OMITTED reaches the
//	  handler on create (auto-slug) and on update (slug kept), with an explicit
//	  rename as the positive twin so a "slug is ignored everywhere" change cannot
//	  pass.
//
// The behavioural test skips without JOMFORM_TEST_DATABASE_URL, like every other
// DB-backed test in this package; the schema test always runs.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// requiredNames reads an inputSchema's top-level `required` as strings.
func requiredNames(schema map[string]any) []string {
	req, _ := schema["required"].([]any)
	out := make([]string, 0, len(req))
	for _, r := range req {
		s, _ := r.(string)
		out = append(out, s)
	}
	return out
}

// TestUpsertFormSchemaDoesNotRequireSlug is the guard for the reported defect at
// the level the agent actually meets it: the schema served by tools/list.
//
// The expectations are spelled out here rather than read from the Go struct,
// because a guard that derives its expectation from the code under test cannot
// catch that code changing.
func TestUpsertFormSchemaDoesNotRequireSlug(t *testing.T) {
	tool := listTool(t, "upsert_form")
	if tool.InputSchema == nil {
		t.Fatal("upsert_form has no inputSchema")
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal inputSchema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("inputSchema is not a JSON object: %v (%s)", err, raw)
	}
	// Log the schema as an MCP client receives it, so a failure report (and a
	// reviewer's own run) shows the real contract rather than a paraphrase.
	t.Logf("upsert_form inputSchema: %s", raw)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema has no properties object: %v", schema)
	}
	if _, ok := props["slug"]; !ok {
		t.Fatalf("inputSchema does not declare `slug` at all (%v) — the tool accepts and "+
			"honours it, so an agent could not set a form's URL path", props)
	}

	required := requiredNames(schema)
	for _, r := range required {
		if r == "slug" {
			t.Errorf("inputSchema requires `slug` (required=%v) — the schema is enforced before the "+
				"handler runs, so an agent that follows the description and omits it on an update never "+
				"reaches the handler: it gets `validating \"arguments\": required: missing properties: "+
				"[\\\"slug\\\"]` instead. slug is optional on BOTH paths: create auto-generates it "+
				"(issue #176) and update keeps the stored one", required)
		}
	}
	// Control: `title` IS genuinely required (the handler refuses an empty one),
	// so an empty/absent required list cannot make the loop above vacuous.
	var hasTitle bool
	for _, r := range required {
		if r == "title" {
			hasTitle = true
		}
	}
	if !hasTitle {
		t.Errorf("inputSchema required=%v does not list `title`, which the handler refuses when "+
			"empty — without it the \"slug is not required\" assertion above proves nothing", required)
	}

	// Prose side. The description an agent is handed is where the optionality is
	// promised, so it and the schema must not disagree; and the slug property's
	// own description must read as optional too (that is the text a client shows
	// beside the field).
	if !strings.Contains(tool.Description, "slug (optional)") {
		t.Errorf("upsert_form Description no longer says `slug (pilihan)`: %q — if slug has really "+
			"become mandatory the schema must say so too (and this test becomes the wrong place to "+
			"assert it), but a silent change of one side is the drift this test exists to catch",
			tool.Description)
	}
	slugProp, _ := props["slug"].(map[string]any)
	slugDesc, _ := slugProp["description"].(string)
	if !strings.Contains(slugDesc, "optional") {
		t.Errorf("the inputSchema's own `slug` description does not read as optional: %q", slugDesc)
	}
}

// TestUpsertFormWithoutSlugReachesTheHandler is the card's acceptance criterion,
// driven over the real transport with a real API key: a tools/call that omits
// `slug` must be accepted by the SDK's argument validation and run the handler.
func TestUpsertFormWithoutSlugReachesTheHandler(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, _, accID, rawKey := wsMCPUserWorkspaces(t, pool, "noslug", 1)
	httpSrv := wsMCPFixture(t, pool)
	fs := dbstore.NewFormStore(pool)

	// ── CREATE with slug omitted: the auto-generated slug (issue #176). ──────
	res := mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"title":  "QA Slug Optional " + accID[:8],
		"config": map[string]any{"mode": "response", "blocks": []any{}},
	})
	if ok, msg := refused(res); ok {
		t.Fatalf("upsert_form (create) with `slug` omitted was refused: %s — the schema still makes "+
			"slug required, so the documented behaviour (auto-slug on create) is unreachable", msg)
	}
	created := succeeded(t, res)
	formID, _ := created["form_id"].(string)
	if formID == "" {
		t.Fatalf("upsert_form (create) returned %v, want a form_id", created)
	}
	autoSlug, _ := created["slug"].(string)
	if autoSlug == "" {
		t.Errorf("upsert_form (create) returned %v with no slug — issue #176 says an omitted slug is "+
			"generated, and the agent is told the URL path it got", created)
	}
	f, err := fs.GetByID(ctx, accID, formID)
	if err != nil {
		t.Fatalf("read created form: %v", err)
	}
	if f.Slug != autoSlug {
		t.Errorf("stored slug = %q but the call reported %q — the value the agent is told it owns "+
			"cannot disagree with what was written", f.Slug, autoSlug)
	}

	// ── UPDATE with slug omitted: the stored slug is KEPT. ──────────────────
	upd := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"form_id": formID,
		"title":   "QA Slug Optional Renamed " + accID[:8],
	}))
	if upd["status"] != "updated" {
		t.Fatalf("upsert_form (update) without `slug` returned %v, want status=updated", upd)
	}
	if got, _ := upd["slug"].(string); got != autoSlug {
		t.Errorf("update reported slug %q, want the stored one %q — an omitted slug must keep the "+
			"form's URL, not rename it or blank it", got, autoSlug)
	}
	f, err = fs.GetByID(ctx, accID, formID)
	if err != nil {
		t.Fatalf("read updated form: %v", err)
	}
	if f.Slug != autoSlug {
		t.Errorf("stored slug after an update that omitted slug = %q, want %q — a public URL moved "+
			"without being asked to", f.Slug, autoSlug)
	}
	if !strings.Contains(f.Title, "Renamed") {
		t.Errorf("stored title = %q, want the updated one — the call was accepted but did nothing", f.Title)
	}

	// ── Positive twin: a slug that IS supplied still renames. ───────────────
	newSlug := "qa-noslug-renamed-" + accID[:8]
	ren := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "upsert_form", map[string]any{
		"form_id": formID,
		"title":   f.Title,
		"slug":    newSlug,
	}))
	if got, _ := ren["slug"].(string); got != newSlug {
		t.Fatalf("upsert_form with an explicit slug reported %v, want slug=%q — making slug optional "+
			"must not make it ignored", ren, newSlug)
	}
	f, err = fs.GetByID(ctx, accID, formID)
	if err != nil {
		t.Fatalf("read renamed form: %v", err)
	}
	if f.Slug != newSlug {
		t.Errorf("stored slug = %q after an explicit rename to %q", f.Slug, newSlug)
	}
}
