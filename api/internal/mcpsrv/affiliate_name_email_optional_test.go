package mcpsrv

// create_affiliate's `name` and `email` are OPTIONAL, and the served schema must
// say so — the schema is enforced BEFORE the handler runs, so a field listed in
// `required` makes the tool's own field descriptions a lie and the documented
// behaviour unreachable.
//
// The defect this file pins: createAffiliateIn.Name and .Email were
// `json:"name"` / `json:"email"` with no `omitempty`, so the schema generator
// (github.com/google/jsonschema-go: "Fields that are marked omitempty or
// omitzero are considered optional; all other fields become required
// properties") listed both as required. Their own
// jsonschema descriptions say "(pilihan)" — optional — and so does the storage
// layer: migrations/000047_affiliates.up.sql declares
//
//	name  TEXT NOT NULL DEFAULT '',
//	email TEXT NOT NULL DEFAULT '',
//
// and the handler passes both straight through without ever requiring them. An
// agent that followed the description got, before its arguments ever reached the
// handler:
//
//	tools/call create_affiliate {"workspace_id":"…","code":"ALI","commission_pct":10}
//	→ {"isError":true,"content":[{"text":"validating \"arguments\": validating
//	   root: required: missing properties: [\"name\" \"email\"]"}]}
//
// Two independent things are asserted, because passing one is not passing the
// other:
//
//	TestCreateAffiliateSchemaDoesNotRequireNameOrEmail  (no DB)  — the tools/list
//	  schema an agent is handed does not require `name`/`email`, while the three
//	  genuinely required arguments still are; and both property descriptions
//	  still read as optional, so schema and prose cannot drift apart in either
//	  direction.
//	TestCreateAffiliateWithoutNameOrEmailReachesTheHandler (real DB) — the
//	  acceptance: a real tools/call over the real transport with both fields
//	  OMITTED reaches the handler and stores the affiliate, with an explicit
//	  name/e-mail as the positive twin so a "name/email is ignored everywhere"
//	  change cannot pass.
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

// TestCreateAffiliateSchemaDoesNotRequireNameOrEmail is the guard for the
// reported defect at the level the agent actually meets it: the schema served by
// tools/list.
//
// The expectations are spelled out here rather than read from the Go struct,
// because a guard that derives its expectation from the code under test cannot
// catch that code changing.
func TestCreateAffiliateSchemaDoesNotRequireNameOrEmail(t *testing.T) {
	tool := listTool(t, "create_affiliate")
	if tool.InputSchema == nil {
		t.Fatal("create_affiliate has no inputSchema")
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
	t.Logf("create_affiliate inputSchema: %s", raw)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema has no properties object: %v", schema)
	}
	required := requiredNames(schema)
	for _, name := range []string{"name", "email"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("inputSchema does not declare `%s` at all (%v) — the tool accepts and stores "+
				"it, so an agent could not record the affiliate's %s", name, props, name)
		}
		for _, r := range required {
			if r == name {
				t.Errorf("inputSchema requires `%s` (required=%v) — the schema is enforced before the "+
					"handler runs, so an agent that follows the field description and omits it never "+
					"reaches the handler: it gets `validating \"arguments\": required: missing "+
					"properties: [%q]` instead. Affiliates are stored with an empty %s when it is not "+
					"supplied (migration 000047: %s TEXT NOT NULL DEFAULT '')", name, required, name, name, name)
			}
		}
	}
	// Control: `workspace_id` and `code` ARE genuinely required — the write gate
	// is scoped by the workspace, and `code` is the affiliate's referral key
	// (?aff=CODE), which the handler looks up and stores. Without this an
	// empty/absent required list would make the loop above vacuous.
	for _, want := range []string{"workspace_id", "code"} {
		var has bool
		for _, r := range required {
			if r == want {
				has = true
			}
		}
		if !has {
			t.Errorf("inputSchema required=%v does not list `%s`, which the handler needs — without "+
				"it the \"name/email is not required\" assertion above proves nothing", required, want)
		}
	}

	// Prose side. The field descriptions are where the optionality is promised,
	// so they and the schema must not disagree; a silent change of one side is
	// the drift this test exists to catch.
	for _, name := range []string{"name", "email"} {
		prop, _ := props[name].(map[string]any)
		desc, _ := prop["description"].(string)
		if !strings.Contains(desc, "optional") {
			t.Errorf("the inputSchema's own `%s` description does not read as optional: %q — if the "+
				"field has really become mandatory the schema must say so too (and this test becomes "+
				"the wrong place to assert it), but the two surfaces must not disagree", name, desc)
		}
	}
}

// TestCreateAffiliateWithoutNameOrEmailReachesTheHandler is the card's acceptance
// criterion, driven over the real transport with a real API key: a tools/call
// that omits `name` and `email` must be accepted by the SDK's argument
// validation and run the handler.
func TestCreateAffiliateWithoutNameOrEmailReachesTheHandler(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	userID, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "affopt", 1)
	httpSrv := wsMCPFixture(t, pool)
	affStore := dbstore.NewAffiliateStore(pool)

	// ── Both fields OMITTED: the call the description promises. ─────────────
	code := "ALI" + strings.ToUpper(userID[:6])
	res := mcpCall(t, httpSrv.URL, rawKey, "create_affiliate", map[string]any{
		"workspace_id":   wsIDs[0],
		"code":           code,
		"commission_pct": 10,
	})
	if ok, msg := refused(res); ok {
		t.Fatalf("create_affiliate with `name` and `email` omitted was refused: %s — the schema still "+
			"makes them required, so the documented behaviour (optional name/e-mail, stored empty) "+
			"is unreachable", msg)
	}
	created := succeeded(t, res)
	if got, _ := created["code"].(string); got != code {
		t.Fatalf("create_affiliate returned %v, want code=%q", created, code)
	}
	if got, _ := created["name"].(string); got != "" {
		t.Errorf("returned name = %q, want empty — an omitted name must not be invented", got)
	}
	if got, _ := created["email"].(string); got != "" {
		t.Errorf("returned email = %q, want empty — an omitted e-mail must not be invented", got)
	}
	wantURL := "?aff=" + code
	if got, _ := created["referral_url"].(string); !strings.Contains(got, wantURL) {
		t.Errorf("referral_url = %q, want it to contain %q — an affiliate the tool refused to name "+
			"still has to be attributable", got, wantURL)
	}
	stored, err := affStore.ByCode(ctx, wsIDs[0], code)
	if err != nil || stored == nil {
		t.Fatalf("read created affiliate by code %q: %v (nil=%v) — the call reported success but "+
			"nothing was stored", code, err, stored == nil)
	}
	if stored.Name != "" || stored.Email != "" {
		t.Errorf("stored affiliate = {name:%q, email:%q}, want both empty — an omitted field must "+
			"reach the store as empty, not be filled in", stored.Name, stored.Email)
	}

	// ── Positive twin: supplied fields are still stored. ────────────────────
	// Without this, making the fields optional by IGNORING them would pass the
	// arm above while silently dropping every affiliate's contact details.
	named := "KAKAK" + strings.ToUpper(userID[:6])
	withContact := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "create_affiliate", map[string]any{
		"workspace_id":   wsIDs[0],
		"code":           named,
		"name":           "Kakak Ali",
		"email":          "kakak@example.test",
		"commission_pct": 5,
	}))
	if got, _ := withContact["name"].(string); got != "Kakak Ali" {
		t.Errorf("create_affiliate with a name returned name=%q, want %q — making name optional must "+
			"not make it ignored", got, "Kakak Ali")
	}
	stored, err = affStore.ByCode(ctx, wsIDs[0], named)
	if err != nil || stored == nil {
		t.Fatalf("read affiliate by code %q: %v (nil=%v)", named, err, stored == nil)
	}
	if stored.Name != "Kakak Ali" || stored.Email != "kakak@example.test" {
		t.Errorf("stored affiliate = {name:%q, email:%q}, want {%q, %q}", stored.Name, stored.Email,
			"Kakak Ali", "kakak@example.test")
	}
}
