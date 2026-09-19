package mcpsrv

// The per-workspace AI spam-judge toggle over MCP.
//
// AGENTS.md's MCP FEATURE PARITY rule requires the dashboard feature to be
// configurable through MCP, so the toggle is a FIELD on the two tools that
// already own per-workspace settings:
//
//   set_workspace_settings(workspace_id, spam_judge_enabled: false)
//   get_workspace_settings(workspace_id) -> { …, spam_judge_enabled }
//
// No new tool is registered, so the AGENTS.md parity checklist is unchanged —
// `get_workspace_settings`/`set_workspace_settings` were already registered and
// already recorded in `mcpParityBaselineTools` (mcp_parity_test.go). That is
// deliberate: adding a checklist line for a field would be wrong, since the
// checklist tracks TOOLS.
//
// WHAT IS PINNED, and why each one can break silently:
//
//   1. WRITE then READ through the real tool handlers: false round-trips. A
//      write path that accepted the field but dropped it (a missing
//      `if in.X != nil` arm) would leave the workspace judging spam with an
//      agent reporting success — the exact shape of the bug this card fixes.
//   2. Omit-to-keep: calling set_workspace_settings WITHOUT the field must not
//      change it. This is the documented contract of every other field on that
//      tool, and for THIS field breaking it means an agent editing an unrelated
//      setting silently re-enables spam detection the merchant turned off.
//   3. The default is ENABLED for a workspace that never wrote it, so an agent
//      reading a fresh workspace is told the truth about what will happen.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset, matching the mcpsrv convention.

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue622MCPFixture wires a server whose caller owns one workspace, plus a
// second workspace the caller has no access to (the tenancy negative).
type issue622MCPFixture struct {
	srv  *Server
	ctx  context.Context
	sp   *dbstore.WorkspaceStore
	ws   string
	wsMy string
}

func issue622MCPNew(t *testing.T) issue622MCPFixture {
	t.Helper()
	if strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL")) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping spam-judge toggle MCP test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	newUser := func() string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash)
			 VALUES ('i622m-' || md5(random()::text) || '@example.test', 'it')
			 RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("create user: %v", err)
		}
		return id
	}
	ownerID, otherID := newUser(), newUser()
	sp := dbstore.NewWorkspaceStore(pool)

	ws, err := sp.Create(ctx, ownerID, "I622 MCP mine", "i622m-mine-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	wsOther, err := sp.Create(ctx, otherID, "I622 MCP other", "i622m-other-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	srv := &Server{deps: &Deps{Accounts: dbstore.NewAccountStore(pool), Workspaces: sp}}
	return issue622MCPFixture{
		srv:  srv,
		ctx:  WithAuthIdentity(ctx, &AuthIdentity{UserID: ownerID, Scopes: []string{"readwrite"}}),
		sp:   sp,
		ws:   ws.ID,
		wsMy: wsOther.ID,
	}
}

// read622 calls the real get_workspace_settings handler and returns the
// workspace's settings as the protocol would.
func (f issue622MCPFixture) read622(t *testing.T) *dbstore.WorkspaceSettings {
	t.Helper()
	_, st, err := f.srv.getWorkspaceSettings(f.ctx, nil, workspaceRefIn{WorkspaceID: f.ws})
	if err != nil {
		t.Fatalf("get_workspace_settings: %v", err)
	}
	if st == nil {
		t.Fatal("get_workspace_settings returned no settings")
	}
	return st
}

// TestIssue622MCPToggleRoundTrips: an agent turns the judge off and reads back
// off, then on again.
//
// BITE PROOF for the MCP surface: delete the `if in.SpamJudgeEnabled != nil`
// arm from setWorkspaceSettings and the first assertion fails with
// "set_workspace_settings(spam_judge_enabled=false) left the judge ON" — the
// agent would report success while the workspace kept judging every entry.
func TestIssue622MCPToggleRoundTrips(t *testing.T) {
	f := issue622MCPNew(t)

	// Baseline: a workspace that never wrote the field reads as ON.
	if got := f.read622(t); !got.SpamJudgeEnabled {
		t.Fatalf("workspace %s defaults to spam_judge_enabled=false over MCP — "+
			"an agent would be told spam detection is off when it is on", f.ws)
	}

	off := false
	if _, _, err := f.srv.setWorkspaceSettings(f.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: f.ws, SpamJudgeEnabled: &off,
	}); err != nil {
		t.Fatalf("set_workspace_settings(false): %v", err)
	}
	if got := f.read622(t); got.SpamJudgeEnabled {
		t.Error("set_workspace_settings(spam_judge_enabled=false) left the judge ON — " +
			"the field is accepted but not persisted, so an agent reports success while " +
			"every submission is still sent to the AI judge")
	}

	// And back on.
	on := true
	if _, _, err := f.srv.setWorkspaceSettings(f.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: f.ws, SpamJudgeEnabled: &on,
	}); err != nil {
		t.Fatalf("set_workspace_settings(true): %v", err)
	}
	if got := f.read622(t); !got.SpamJudgeEnabled {
		t.Error("set_workspace_settings(spam_judge_enabled=true) left the judge OFF")
	}
}

// schemaPropertyNames returns the exact top-level property names advertised in
// an MCP JSON schema, so the assertion is on the real property key rather than
// on a substring.
//
// This precision is load-bearing and was found by a BITE PROOF, not by review:
// the first version of this test used strings.Contains(schema,
// "spam_judge_enabled"), and renaming the wire tag to
// "spam_judge_enabled_probe_renamed" still PASSED — the correct prefix is a
// substring of the broken name, so the guard was green on a field an agent
// could no longer discover. Comparing whole property names closes it.
func schemaPropertyNames(t *testing.T, label string, schema any) []string {
	t.Helper()
	if schema == nil {
		t.Fatalf("%s advertises no schema", label)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal %s schema: %v", label, err)
	}
	var doc struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal %s schema: %v (%s)", label, err, raw)
	}
	names := make([]string, 0, len(doc.Properties))
	for n := range doc.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("%s advertises a schema with no properties — the guard cannot check "+
			"anything and must not pass vacuously (schema: %s)", label, raw)
	}
	return names
}

// TestIssue622MCPToolSchemaExposesTheToggle is the protocol-level half of the
// MCP requirement: the field must be visible to an agent in the tool's
// ADVERTISED schema, not merely accepted by the handler it resolves to.
// The go-sdk derives those schemas by reflecting on the `In`/`Out` struct tags,
// so a dropped or renamed `json` tag would make the toggle invisible in an
// agent's tool list while every handler-level test above still passed.
func TestIssue622MCPToolSchemaExposesTheToggle(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "issue622-schema", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	tools := map[string]*mcp.Tool{}
	cursor := ""
	for {
		res, lerr := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if lerr != nil {
			t.Fatalf("ListTools: %v", lerr)
		}
		for _, tl := range res.Tools {
			tools[tl.Name] = tl
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(tools) == 0 {
		t.Fatal("no tools registered — registerTools did not run")
	}

	// The WRITE tool must advertise the exact property name in its input schema.
	setTool, ok := tools["set_workspace_settings"]
	if !ok {
		t.Fatal("set_workspace_settings is not registered")
	}
	if !slices.Contains(schemaPropertyNames(t, "set_workspace_settings input", setTool.InputSchema), "spam_judge_enabled") {
		t.Errorf("set_workspace_settings does not advertise a `spam_judge_enabled` input property "+
			"(got %v) — an agent cannot discover the toggle",
			schemaPropertyNames(t, "set_workspace_settings input", setTool.InputSchema))
	}

	// The READ tool must advertise it in its output schema.
	getTool, ok := tools["get_workspace_settings"]
	if !ok {
		t.Fatal("get_workspace_settings is not registered")
	}
	if !slices.Contains(schemaPropertyNames(t, "get_workspace_settings output", getTool.OutputSchema), "spam_judge_enabled") {
		t.Errorf("get_workspace_settings does not advertise `spam_judge_enabled` in its output "+
			"schema (got %v) — an agent reading the tool result would not know the workspace's "+
			"spam setting",
			schemaPropertyNames(t, "get_workspace_settings output", getTool.OutputSchema))
	}
}

// TestIssue622MCPOmittedFieldKeepsCurrentValue: the omit-to-keep contract.
func TestIssue622MCPOmittedFieldKeepsCurrentValue(t *testing.T) {
	f := issue622MCPNew(t)

	off := false
	if _, _, err := f.srv.setWorkspaceSettings(f.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: f.ws, SpamJudgeEnabled: &off,
	}); err != nil {
		t.Fatalf("turn judge off: %v", err)
	}

	// An unrelated edit, with the field omitted entirely.
	footer := "an unrelated footer change"
	if _, _, err := f.srv.setWorkspaceSettings(f.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: f.ws, FooterText: &footer,
	}); err != nil {
		t.Fatalf("set_workspace_settings(footer only): %v", err)
	}

	got := f.read622(t)
	if got.SpamJudgeEnabled {
		t.Error("omitting spam_judge_enabled re-enabled the judge — an agent editing an " +
			"unrelated setting would silently turn spam detection back on for a merchant " +
			"who deliberately switched it off")
	}
	if got.FooterText != footer {
		t.Errorf("footer_text = %q, want %q (the unrelated field must still save)", got.FooterText, footer)
	}
}

// TestIssue622MCPIsTenantScoped: a caller cannot toggle another tenant's judge.
func TestIssue622MCPIsTenantScoped(t *testing.T) {
	f := issue622MCPNew(t)

	off := false
	_, _, err := f.srv.setWorkspaceSettings(f.ctx, nil, setWorkspaceSettingsIn{
		WorkspaceID: f.wsMy, SpamJudgeEnabled: &off,
	})
	if err == nil {
		t.Fatal("set_workspace_settings accepted a workspace the caller has no access to")
	}
	// And the other workspace is untouched (its judge is still on).
	st, serr := f.sp.Settings(context.Background(), f.wsMy)
	if serr != nil {
		t.Fatalf("read other workspace settings: %v", serr)
	}
	if !st.SpamJudgeEnabled {
		t.Error("the cross-tenant call mutated the other workspace's judge toggle")
	}
}
