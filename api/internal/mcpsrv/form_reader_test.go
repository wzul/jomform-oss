package mcpsrv

// MCP form READER — the missing half of the form builder surface.
//
// The form builder's sub-resources were all readable (get_form_analytics,
// get_form_chip_feeds, get_form_confirmations, get_form_notifications,
// get_form_restrictions, get_form_og_image, get_form_email_settings,
// get_form_failure_message) while the form ITSELF was not: `upsert_form` is a
// writer (its title is required and it never returns the form), so an agent
// could not read a form at all — it could not answer "what blocks does this
// form have right now?".
//
// That is not a convenience gap. It makes "change the form's title" unsafe to
// perform over MCP: without a read, the agent cannot verify that the existing
// blocks survive the write, which is exactly how a customer lost a landing
// page to an injected empty config.
//
// Two tests, covering the two failure shapes:
//
//  1. TestFormReaderToolsRegistered — the tools are actually registered and
//     discoverable over the MCP protocol (a handler that is never registered is
//     invisible to the agent and to the parity guard's reverse direction), and
//     get_form advertises the form_id argument it needs.
//
//  2. TestFormReaderRoundTripPreservesBlocks — the read → write-one-field →
//     re-read cycle keeps the form's config byte-identical. This is the
//     acceptance criterion of the card that added the reader.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// bearerRoundTripper adds a Bearer API key to every MCP request, which is what a
// client does after it is configured with a key.
type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r2)
}

// TestFormReaderToolsRegistered asserts list_forms and get_form are registered,
// enumerated over the real MCP protocol (tools/list), not by reading source.
//
// BITE: remove either addTool call from registerTools() and this test fails,
// as does TestMCPParityChecklistToolsAreRegistered (AGENTS.md names both tools
// on the form-builder checklist line, and neither is in
// mcpParityStaleChecklistEntries any more).
func TestFormReaderToolsRegistered(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
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

	res, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, name := range []string{"list_forms", "get_form"} {
		if !got[name] {
			t.Errorf("form reader: tool %q is not registered by registerTools — an agent cannot %s",
				name, map[string]string{"list_forms": "discover forms", "get_form": "read a form"}[name])
		}
	}

	// get_form must advertise its form_id argument, or the tool is unusable.
	var found bool
	for _, tl := range res.Tools {
		if tl.Name != "get_form" {
			continue
		}
		found = true
		raw, merr := json.Marshal(tl.InputSchema)
		if merr != nil {
			t.Fatalf("marshal get_form input schema: %v", merr)
		}
		if !strings.Contains(strings.ToLower(string(raw)), "form_id") {
			t.Errorf("get_form input schema does not expose form_id: %s", raw)
		}
	}
	if !found {
		t.Error("get_form tool not found in tools/list")
	}

	// list_forms and get_form must not be pinned as stale checklist entries any
	// more: TestMCPParityGuardBaselinesStayHonest enforces that, and reaching
	// here means the tools really are registered.
	for _, name := range []string{"list_forms", "get_form"} {
		if _, stale := mcpParityStaleChecklistEntries[name]; stale {
			t.Errorf("mcpParityStaleChecklistEntries still lists %q although it IS registered — remove the entry", name)
		}
	}
}

// formBlocksLen returns the number of blocks in a form's config, and whether
// `blocks` was present as an array at all — so a reader that dropped `config`
// reports a clear failure instead of panicking in a type assertion.
func formBlocksLen(t *testing.T, cfg map[string]any) (int, bool) {
	t.Helper()
	raw, ok := cfg["blocks"]
	if !ok {
		return 0, false
	}
	arr, ok := raw.([]any)
	if !ok {
		return 0, false
	}
	return len(arr), true
}

// TestFormReaderOverTheMCPProtocol calls the tools the way an agent does: a
// real MCP client session (in-memory transport) issuing tools/call, with the
// real dbstore wired and the authenticated identity on the server session's
// context. It decodes the JSON the agent receives, so it proves the tool is
// callable and returns the form's real blocks — not merely that a handler
// function exists.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
func TestFormReaderOverTheMCPProtocol(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP-protocol form-reader test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-proto-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-proto-' || md5(random()::text), 'mcp-proto-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "mcp-proto-" + accountID[:8],
		Title:     "Western Food Sample",
		Config: map[string]any{
			"mode": "payment",
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name"},
				map[string]any{"id": "b_chop", "type": "product", "product_id": "76109c58"},
			},
		},
		Theme:  map[string]any{"layout": "centered"},
		Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	srv := &Server{
		mcp: mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		// Accounts is needed by resolveFormAccount → userAccessibleAccounts for
		// an API-key identity whose UserID is empty.
		deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "reader-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	// The server session's context is what a real transport derives the request
	// context from, so the authenticated principal rides on it.
	authCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	serverSession, err := srv.mcp.Connect(authCtx, sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	// ── tools/call get_form ────────────────────────────────────────────────
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_form",
		Arguments: map[string]any{"form_id": f.ID},
	})
	if err != nil {
		t.Fatalf("tools/call get_form: %v", err)
	}
	if res.IsError {
		t.Fatalf("tools/call get_form returned an error result: %+v", res.Content)
	}
	payload := toolResultText(t, res)
	if payload == "" {
		t.Fatal("get_form returned no text content")
	}
	var detail struct {
		ID     string         `json:"id"`
		Title  string         `json:"title"`
		Mode   string         `json:"mode"`
		Config map[string]any `json:"config"`
		Theme  map[string]any `json:"theme"`
	}
	if err := json.Unmarshal([]byte(payload), &detail); err != nil {
		t.Fatalf("decode get_form payload %q: %v", payload, err)
	}
	if detail.ID != f.ID || detail.Title != "Western Food Sample" {
		t.Errorf("get_form payload id/title = %q/%q, want %q/Western Food Sample", detail.ID, detail.Title, f.ID)
	}
	if got, ok := formBlocksLen(t, detail.Config); !ok || got != 2 {
		t.Errorf("get_form payload: blocks present=%v count=%d, want 2 — the agent cannot see the form's blocks", ok, got)
	}
	if detail.Theme["layout"] != "centered" {
		t.Errorf("get_form payload theme = %v, want layout=centered", detail.Theme)
	}

	// ── tools/call list_forms ──────────────────────────────────────────────
	lres, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_forms"})
	if err != nil {
		t.Fatalf("tools/call list_forms: %v", err)
	}
	if lres.IsError {
		t.Fatalf("tools/call list_forms returned an error result: %+v", lres.Content)
	}
	var listed struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Mode  string `json:"mode"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(toolResultText(t, lres)), &listed); err != nil {
		t.Fatalf("decode list_forms payload: %v", err)
	}
	var found bool
	for _, it := range listed.Items {
		if it.ID == f.ID {
			found = true
			if it.Mode != dbstore.FormModePayment {
				t.Errorf("list_forms mode = %q, want %q", it.Mode, dbstore.FormModePayment)
			}
		}
	}
	if !found {
		t.Errorf("list_forms over the protocol did not return the seeded form %s", f.ID)
	}
}

// TestFormReaderOverHTTPTransport drives the tools through the REAL MCP
// streamable-HTTP handler with a REAL API key — the path an agent actually uses
// in production, including the Bearer authentication and the per-request
// identity. The in-memory protocol test above covers the tool contract; this one
// covers the wire and the auth that gate it.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
func TestFormReaderOverHTTPTransport(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP HTTP-transport form-reader test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-http-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-http-' || md5(random()::text), 'mcp-http-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "mcp-http-" + accountID[:8],
		Title:     "Western Food Sample",
		Config: map[string]any{
			"mode": "payment",
			"blocks": []any{
				map[string]any{"id": "b_name", "type": "name"},
				map[string]any{"id": "b_email", "type": "email"},
				map[string]any{"id": "b_chop", "type": "product", "product_id": "76109c58"},
			},
		},
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// A real API key row: the HTTP handler hashes the Bearer value and looks it
	// up here, exactly as prod does.
	const rawKey = "jf_test_mcp_reader_0000000000000001"
	keys := dbstore.NewAPIKeyStore(pool)
	if _, err := keys.Create(ctx, accountID, auth.HashAPIKey(rawKey), "mcp-reader-http", []string{"readwrite"}); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	srv := New(&Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool), Keys: keys},
		nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	defer httpSrv.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "http-reader-test", Version: "test"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: httpSrv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{
			base: http.DefaultTransport, token: rawKey,
		}},
		DisableStandaloneSSE: true,
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := client.Connect(callCtx, transport, nil)
	if err != nil {
		t.Fatalf("MCP connect over HTTP: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      "get_form",
		Arguments: map[string]any{"form_id": f.ID},
	})
	if err != nil {
		t.Fatalf("tools/call get_form over HTTP: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_form returned an error result over HTTP: %+v", res.Content)
	}
	var detail struct {
		ID     string         `json:"id"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal([]byte(toolResultText(t, res)), &detail); err != nil {
		t.Fatalf("decode get_form payload: %v", err)
	}
	if detail.ID != f.ID {
		t.Errorf("get_form over HTTP returned form %q, want %q", detail.ID, f.ID)
	}
	if got, ok := formBlocksLen(t, detail.Config); !ok || got != 3 {
		t.Errorf("get_form over HTTP: blocks present=%v count=%d, want 3", ok, got)
	}

	// An unauthenticated call must still be refused (the reader must not have
	// opened a hole in the transport's auth).
	noAuth := &http.Client{}
	req, _ := http.NewRequestWithContext(callCtx, http.MethodPost, httpSrv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	resp, err := noAuth.Do(req)
	if err != nil {
		t.Fatalf("unauthenticated request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated /mcp returned %d, want 401", resp.StatusCode)
	}
}

// toolResultText concatenates the text content blocks of a tool result.
func toolResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestFormReaderRoundTripPreservesBlocks is the card's acceptance criterion: a
// read → change ONLY the title → re-read cycle leaves the form's config
// byte-identical. It drives the REAL handlers over a real Postgres
// (upsertForm as the writer, listForms/getForm as the reader), so a reader that
// silently dropped `config` — or a writer that wiped the blocks — fails here.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
func TestFormReaderRoundTripPreservesBlocks(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP form-reader round-trip test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-reader-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-reader-' || md5(random()::text), 'mcp-reader-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)

	// A payment form with REAL blocks and a product snapshot — the shape an
	// agent must not be able to destroy by writing a title. The blocks and the
	// theme are deliberately non-trivial: an empty config would make the
	// "preserved" assertion vacuous.
	blocks := []any{
		map[string]any{"id": "block_name", "type": "name", "label": "Nama"},
		map[string]any{"id": "block_email", "type": "email", "label": "Emel"},
		map[string]any{"id": "block_chop", "type": "product", "product_id": "prod-76109c58"},
	}
	f := &dbstore.Form{
		AccountID: accountID,
		Slug:      "mcp-reader-" + accountID[:8],
		Title:     "Western Food Sample",
		Config: map[string]any{
			"mode":   "payment",
			"blocks": blocks,
			"products": []any{
				map[string]any{"id": "prod-76109c58", "name": "Chicken Chop", "price_sen": float64(1990)},
			},
		},
		Theme:  map[string]any{"primary": "#0f172a", "layout": "centered"},
		Status: dbstore.FormStatusPublished,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	// ── discovery: the form appears in the list, with its id ───────────────
	_, page, err := srv.listForms(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("listForms: %v", err)
	}
	if page == nil || len(page.Items) == 0 {
		t.Fatal("listForms returned no items — the agent cannot discover the form id")
	}
	var listed bool
	for _, it := range page.Items {
		if it.ID == f.ID {
			listed = true
			if it.Title != "Western Food Sample" || it.Mode != dbstore.FormModePayment {
				t.Errorf("list entry = title %q mode %q, want the seeded title and payment mode", it.Title, it.Mode)
			}
		}
	}
	if !listed {
		t.Fatalf("seeded form %s missing from listForms output", f.ID)
	}

	// ── read BEFORE: the reader must return the real blocks + theme ─────────
	_, before, err := srv.getForm(callCtx, nil, formRefIn{FormID: f.ID})
	if err != nil {
		t.Fatalf("getForm (before): %v", err)
	}
	if before == nil {
		t.Fatal("getForm returned no detail")
	}
	if got, ok := formBlocksLen(t, before.Config); !ok || got != len(blocks) {
		t.Fatalf("getForm before: blocks present=%v, %d blocks, want %d — the reader does not return the form's config",
			ok, got, len(blocks))
	}
	if before.Theme["layout"] != "centered" {
		t.Errorf("getForm before: theme not returned (%v)", before.Theme)
	}
	beforeJSON, err := json.Marshal(before.Config)
	if err != nil {
		t.Fatalf("marshal config before: %v", err)
	}

	// ── change ONLY the title, through the real writer ──────────────────────
	// Config is omitted (nil) — the store's COALESCE keeps the stored config.
	if _, _, err := srv.upsertForm(callCtx, nil, upsertFormIn{
		FormID: f.ID,
		Title:  "Western Food Sample — Edisi Baharu",
	}); err != nil {
		t.Fatalf("upsertForm (title only): %v", err)
	}

	// ── read AFTER: same blocks, new title ─────────────────────────────────
	_, after, err := srv.getForm(callCtx, nil, formRefIn{FormID: f.ID})
	if err != nil {
		t.Fatalf("getForm (after): %v", err)
	}
	if after.Title != "Western Food Sample — Edisi Baharu" {
		t.Errorf("after title = %q, want the updated title", after.Title)
	}
	afterJSON, err := json.Marshal(after.Config)
	if err != nil {
		t.Fatalf("marshal config after: %v", err)
	}
	// The card's proof: a git-diff-equivalent comparison of the config before
	// and after. Byte equality of the marshalled maps is that comparison.
	if string(beforeJSON) != string(afterJSON) {
		t.Errorf("config changed across a title-only update\n before: %s\n after:  %s", beforeJSON, afterJSON)
	}
	if len(after.Theme) != len(before.Theme) {
		t.Errorf("theme changed across a title-only update: before %v after %v", before.Theme, after.Theme)
	}

	// ── the config is really in the DB, not just in the handler's memory ────
	// Re-read through a fresh server: the blocks must survive the round trip
	// in storage, which is what an agent's next call sees.
	fresh := &Server{deps: &Deps{Forms: dbstore.NewFormStore(pool), Accounts: dbstore.NewAccountStore(pool)}}
	_, reread, err := fresh.getForm(callCtx, nil, formRefIn{FormID: f.ID})
	if err != nil {
		t.Fatalf("getForm (fresh server): %v", err)
	}
	if got, ok := formBlocksLen(t, reread.Config); !ok || got != len(blocks) {
		t.Errorf("stored form has blocks present=%v, %d blocks after a title-only update, want %d — the blocks were lost",
			ok, got, len(blocks))
	}
	if reread.CreatedAt == "" || reread.UpdatedAt == "" {
		t.Errorf("getForm did not report created_at/updated_at (got %q/%q)", reread.CreatedAt, reread.UpdatedAt)
	}
}
