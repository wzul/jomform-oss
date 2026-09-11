package mcpsrv

// Issue #477 — MCP parity guard.
//
// AGENTS.md mandates MCP feature parity ("Every dashboard feature MUST have a
// matching MCP tool") and carries a hand-maintained parity checklist under the
// "MCP FEATURE PARITY" heading. With ~150 registered tools, that prose list
// drifts silently: nothing failed when a dashboard feature shipped without its
// MCP tool, and a blocked edit to AGENTS.md (protected file) could leave the
// checklist behind reality with no signal at all.
//
// This file is the automated guard. It runs in the normal `go test ./...` pass
// with no database required:
//
//  1. TestMCPParityChecklistToolsAreRegistered — enumerates the tools the
//     server actually registers, over the MCP protocol (tools/list, so it is
//     the real registered set, not a source regex), and asserts every tool
//     named in the AGENTS.md parity checklist is registered.
//
//  2. TestMCPParityRegisteredToolsAreDocumented — the reverse direction:
//     every registered tool must be named in the checklist OR recorded in the
//     frozen baseline below. A tool in neither set fails the build, so a NEW
//     tool cannot ship unnoticed.
//
//  3. TestMCPParityGuardBaselinesStayHonest — hygiene on the two bookkeeping
//     lists, so they cannot rot: baseline entries must be real registered
//     tools that are genuinely absent from the checklist, and stale-checklist
//     entries must be genuinely stale. When AGENTS.md is corrected, the lists
//     must shrink or this test fails.
//
// Parsing rules (kept deliberately narrow so the guard is honest, not noisy):
// only lines inside the parity checklist section are read, and only backticked
// snake_case identifiers to the RIGHT of the "→" arrow are treated as tool
// names. File paths, `?aff=CODE`, `forms[]`, `GET /v1/rest/events/stream` and
// parameters such as `template_id` therefore never trip the guard.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// parityChecklistHeading starts the dashboard→MCP parity checklist in AGENTS.md.
const parityChecklistHeading = "Parity checklist"

// snakeIdentRe matches a backticked snake_case identifier, the shape of an MCP
// tool name. Anything with dots, slashes, spaces, brackets or uppercase (file
// paths, URLs, query params, Go type snippets) is not a tool name.
var snakeIdentRe = regexp.MustCompile("^[a-z][a-z0-9_]*$")

// backtickRe captures backticked spans inside the checklist section.
var backtickRe = regexp.MustCompile("`([^`]+)`")

// mcpParityStaleChecklistEntries are names the AGENTS.md parity checklist
// lists on the tool side of the arrow but which are NOT registered tools.
// Each one is either a renamed/never-shipped tool or a parameter that reads
// like a tool name. They are recorded here so the guard stays green while the
// checklist is corrected — AGENTS.md is a protected file, so the correction
// cannot always ride along with the code change (issue #477).
//
// Do NOT add entries for newly intended tools: fix the checklist instead. The
// hygiene test asserts each entry is genuinely still stale, so this map must
// be emptied as AGENTS.md catches up.
var mcpParityStaleChecklistEntries = map[string]string{
	// Never registered under this name: the checklist's "Form builder" line
	// names list_forms/get_form, but the registered form tools are
	// upsert_form/publish_form/unpublish_form/bulk_forms and list_forms/get_form
	// have no registered counterpart.
	"list_forms": "checklist names it; not registered (form listing is covered by upsert_form/bulk_forms)",
	"get_form":   "checklist names it; not registered (form retrieval is covered by upsert_form's form_id path)",
	// Subscriptions: cancel_subscription is registered, but the on-demand
	// recurring-charge sweep is not exposed as a tool.
	"run_due_charges": "checklist names it; not registered (the due-charge sweep runs on the scheduler, not as a tool)",
	// Social accounts: the registered tool is unlink_social_account.
	"disconnect_social_account": "renamed — registered tool is unlink_social_account",
	// Profile avatar: the registered tool is update_profile_avatar.
	"set_avatar": "renamed — registered tool is update_profile_avatar",
	// e-Invoice: the registered tools are set_einvoice/get_einvoice_settings.
	"set_einvoice_settings": "renamed — registered tool is set_einvoice",
	// System settings: the registered writer is set_system_setting (singular).
	"set_system_settings": "renamed — registered tool is set_system_setting",
	// Cloudflare: the registered tools are connect_cloudflare_api_token /
	// connect_cloudflare_global_key.
	"set_cloudflare_api_token":  "renamed — registered tool is connect_cloudflare_api_token",
	"set_cloudflare_global_key": "renamed — registered tool is connect_cloudflare_global_key",
	// Domain TLS: the registered custom-cert tool is set_domain_custom_cert,
	// and there is no get_domain_tls reader (get_domain_tls_email reads the
	// ACME email only).
	"set_domain_tls_cert": "renamed — registered tool is set_domain_custom_cert",
	"get_domain_tls":      "checklist names it; not registered (get_domain_tls_email covers the ACME email)",
	// template_id is a parameter of use_template, not a tool.
	"template_id": "parameter of use_template, not a tool name",
}

// mcpParityBaselineTools are registered tools that are NOT named in the
// AGENTS.md parity checklist. They predate the checklist or are not dashboard
// features (account/workspace plumbing, sub-resource settings, admin/ops
// helpers). This is a frozen baseline for issue #477: it exists only so the
// reverse-direction check can fail on genuinely NEW drift.
//
// Do NOT grow this list silently. A newly registered tool must either be added
// to the AGENTS.md parity checklist (the rule) or added here with a comment
// saying why it is not a checklist feature. The hygiene test keeps every entry
// honest.
var mcpParityBaselineTools = []string{
	// Account / workspace plumbing (login picker, workspace CRUD, membership).
	"cancel_workspace_invite",
	"create_workspace",
	"get_account",
	"get_workspace_settings",
	"invite_workspace_member",
	"list_workspace_invites",
	"list_workspace_members",
	"list_workspaces",
	"remove_workspace_member",
	"set_default_workspace",
	"set_workspace_currency",
	"set_workspace_settings",
	"update_workspace_member_role",
	// Form sub-resources and per-form settings (each dashboard tab has its own
	// tool; the checklist tracks the form builder itself).
	"bulk_forms",
	"bulk_products",
	"clear_form_email_overrides",
	"delete_form",
	"generate_form",
	"get_form_chip_feeds",
	"get_form_chip_mapping",
	"get_form_confirmations",
	"get_form_email_settings",
	"get_form_failure_message",
	"get_form_notifications",
	"get_form_og_image",
	"get_form_restrictions",
	"register_form_fields",
	"set_form_chip_feeds",
	"set_form_chip_mapping",
	"set_form_confirmations",
	"set_form_email_settings",
	"set_form_failure_message",
	"set_form_notifications",
	"set_form_og_image",
	"set_form_password",
	"set_form_restrictions",
	"set_form_theme",
	// Sales / submissions / spam operations.
	"capture_all_pending_sales",
	"get_sale_email_status",
	"list_form_submissions",
	"list_spam_sales",
	"list_spam_submissions",
	"unspam_sale",
	"unspam_submission",
	// Subscriptions + product variations.
	"list_product_variations",
	"list_subscriptions",
	"update_subscription_card",
	// Settings surfaces (email, CHIP, currency, system).
	"clear_chip_settings",
	"get_currency_rates",
	"get_email_settings",
	"set_email_settings",
	"set_einvoice",
	"set_system_setting",
	// Registered counterparts of renamed checklist entries (the checklist
	// still names the old names — see mcpParityStaleChecklistEntries).
	"connect_cloudflare_api_token",
	"connect_cloudflare_global_key",
	"set_domain_custom_cert",
	"unlink_social_account",
	// Cloudflare status + profile fields.
	"get_cloudflare_status",
	"update_profile_avatar",
	"update_profile_phone",
	"update_profile_timezone",
	// 2FA setup step (get/enable/disable_2fa are in the checklist).
	"setup_2fa",
	// Content, media, QR menu, legal pages.
	"generate_legal_page",
	"generate_qr",
	"get_legal_page",
	"get_menu_page",
	"set_legal_page",
	"set_menu_page",
	// Webhook delivery ops + trusted-device bulk revoke.
	"retry_webhook_delivery",
	"revoke_all_trusted_devices",
	// Admin/ops (super-admin or maintenance).
	"list_email_deliveries",
	"purge_unverified_accounts",
}

// registeredToolNames returns the set of tool names the server actually
// registers, enumerated over the MCP protocol so it is the real tools/list
// set (paginated to completion).
func registeredToolNames(t *testing.T) map[string]bool {
	t.Helper()

	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "parity-test", Version: "test"}, nil)
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

	got := map[string]bool{}
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			got[tl.Name] = true
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(got) == 0 {
		t.Fatal("no tools registered — registerTools did not run")
	}
	return got
}

// readAgentsMDPaths locates AGENTS.md at the repository root, relative to this
// test file (api/internal/mcpsrv → ../../../AGENTS.md).
func readAgentsMDPaths(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate AGENTS.md")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "AGENTS.md"))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("AGENTS.md not found at %s — parity guard skipped", path)
		}
		t.Fatalf("stat AGENTS.md: %v", err)
	}
	return path
}

// parseParityChecklist returns the tool names named on the tool side of the
// arrow in the AGENTS.md parity checklist. It reads only the checklist
// section, and only backticked snake_case identifiers that appear to the right
// of a "→" arrow (the dashboard → MCP direction).
func parseParityChecklist(t *testing.T, path string) map[string]bool {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, parityChecklistHeading) {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s no longer contains a %q section — the parity guard cannot run; "+
			"update parityChecklistHeading in this test", path, parityChecklistHeading)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}

	// Group the section into checklist items: a top-level "- " line starts an
	// item; indented continuation lines belong to the item above it.
	var items []string
	for _, l := range lines[start:end] {
		switch {
		case strings.HasPrefix(l, "- "):
			items = append(items, l)
		case strings.TrimSpace(l) != "" && len(items) > 0:
			items[len(items)-1] += "\n" + l
		}
	}

	names := map[string]bool{}
	for _, item := range items {
		arrow := strings.Index(item, "→")
		if arrow == -1 {
			continue
		}
		for _, m := range backtickRe.FindAllStringSubmatch(item[arrow:], -1) {
			if snakeIdentRe.MatchString(m[1]) {
				names[m[1]] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("parsed no tool names from the %q section of %s — the checklist format changed; "+
			"update parseParityChecklist in this test", parityChecklistHeading, path)
	}
	return names
}

// TestMCPParityChecklistToolsAreRegistered asserts every tool named in the
// AGENTS.md parity checklist is actually registered (issue #477 acceptance 1).
func TestMCPParityChecklistToolsAreRegistered(t *testing.T) {
	registered := registeredToolNames(t)
	checklist := parseParityChecklist(t, readAgentsMDPaths(t))

	var missing []string
	for name := range checklist {
		if registered[name] {
			continue
		}
		if _, stale := mcpParityStaleChecklistEntries[name]; stale {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("AGENTS.md parity checklist names %d tool(s) that are not registered: %v\n"+
			"Either register the tool or, if the checklist entry is stale, record it in "+
			"mcpParityStaleChecklistEntries with a reason.", len(missing), missing)
	}
}

// TestMCPParityRegisteredToolsAreDocumented is the reverse direction: every
// registered tool must be named in the AGENTS.md parity checklist or recorded
// in the frozen baseline (issue #477 acceptance 2). A tool in neither set is
// new drift and fails the build.
func TestMCPParityRegisteredToolsAreDocumented(t *testing.T) {
	registered := registeredToolNames(t)
	checklist := parseParityChecklist(t, readAgentsMDPaths(t))

	baseline := map[string]bool{}
	for _, name := range mcpParityBaselineTools {
		baseline[name] = true
	}

	var undocumented []string
	for name := range registered {
		if checklist[name] || baseline[name] {
			continue
		}
		undocumented = append(undocumented, name)
	}
	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Errorf("%d registered MCP tool(s) are absent from the AGENTS.md parity checklist: %v\n"+
			"Add them to the checklist (AGENTS.md \"MCP FEATURE PARITY\") or, if they are not "+
			"dashboard features, record them in mcpParityBaselineTools with a reason.", len(undocumented), undocumented)
	}
}

// TestMCPParityGuardBaselinesStayHonest keeps the two bookkeeping lists from
// rotting: baseline entries must be registered tools that are genuinely absent
// from the checklist, and stale-checklist entries must be genuinely stale.
func TestMCPParityGuardBaselinesStayHonest(t *testing.T) {
	registered := registeredToolNames(t)
	checklist := parseParityChecklist(t, readAgentsMDPaths(t))

	for _, name := range mcpParityBaselineTools {
		switch {
		case !registered[name]:
			t.Errorf("mcpParityBaselineTools lists %q, which is not a registered tool — remove it", name)
		case checklist[name]:
			t.Errorf("mcpParityBaselineTools lists %q, which the parity checklist already names — remove it", name)
		}
	}
	seen := map[string]bool{}
	for _, name := range mcpParityBaselineTools {
		if seen[name] {
			t.Errorf("mcpParityBaselineTools lists %q twice", name)
		}
		seen[name] = true
	}

	for name, reason := range mcpParityStaleChecklistEntries {
		switch {
		case !checklist[name]:
			t.Errorf("mcpParityStaleChecklistEntries lists %q, which the parity checklist no longer names — remove it", name)
		case registered[name]:
			t.Errorf("mcpParityStaleChecklistEntries lists %q, which IS registered — remove it", name)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("mcpParityStaleChecklistEntries entry %q has no reason", name)
		}
	}
}
