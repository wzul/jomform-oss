package mcpsrv

// Renaming a workspace ONTO a reserved label, over the REAL MCP transport.
//
// WHY THIS FILE EXISTS (card t_b7f81f5e, the rework of PR 691). The reserved
// list's MCP coverage was CLAIM-only (workspace_reserved_slug_586ff0c3_test.go
// drives create_workspace). update_workspace is a second write path onto the same
// rule, and it answered differently: the reserved leg returned an anonymous error
// that matched no arm of updateWorkspace's switch, so it fell to
// `fmt.Errorf("db: %w", err)` and an agent read
//
//	db: "billing" is reserved for JomForm: …
//
// — indistinguishable from a storage fault, for input the agent can simply
// change. That is the defect this file pins, and it asserts the THREE properties
// a refusal has to have to be usable by an agent: it is not a transport/db error,
// it carries the machine code, and nothing moved.
//
// Calls go through HTTPHandler → authenticate → the tool handler with a real API
// key, exactly as a configured MCP client does.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// TestMCPUpdateWorkspaceRefusesReservedSlugAsData is the acceptance item 2: the
// reserved refusal is a TYPED refusal an agent can act on, never `db: …`.
func TestMCPUpdateWorkspaceRefusesReservedSlugAsData(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updreserved", 1)
	httpSrv := wsMCPFixture(t, pool)
	sp := dbstore.NewWorkspaceStore(pool)
	before := updateWSSlug(t, pool, wsIDs[0])

	for _, slug := range []string{"billing", "s3", "docs", "mcp", "support", "admin", "templates"} {
		if !urlguard.ReservedSubdomain(slug) {
			t.Fatalf("fixture is wrong: %q is not reserved, so the assertions below would prove nothing", slug)
		}
		res := mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
			map[string]any{"workspace_id": wsIDs[0], "slug": slug, "confirm": true})

		// A policy refusal is DATA, not a transport error: an agent has to be able
		// to read the code and relay an instruction. `IsError` here would mean the
		// call died rather than being refused.
		if res.IsError {
			msg := toolResultText(t, res)
			t.Errorf("update_workspace onto the reserved label %q came back as a TRANSPORT ERROR: %q. "+
				"A reserved label is input the agent can change, so it is a refusal to report, not a "+
				"failure of the call", slug, msg)
			continue
		}
		out := succeeded(t, res)

		// THE DEFECT: the `db: ` prefix that the unclassified fall-through added.
		if msg := str(out["error"]); strings.Contains(msg, "db:") {
			t.Errorf("slug=%q refusal is wrapped as a database error: %q — an agent cannot tell \"the name "+
				"is JomForm's\" from a real storage fault, and the remedy (choose another address) is lost",
				slug, msg)
		}
		if out["status"] != "refused" {
			t.Errorf("slug=%q answered status=%v, want refused (answer %v)", slug, out["status"], out)
			continue
		}
		// The machine code, the same token REST sends, so the caller decides its
		// sentence from the code rather than parsing prose (#694).
		if got := str(out["code"]); got != urlguard.ReservedSubdomainCode {
			t.Errorf("slug=%q refusal code = %q, want %q (answer %v)", slug, got, urlguard.ReservedSubdomainCode, out)
		}
		// The WHY, naming the label: the shared urlguard wording, forwarded.
		msg := str(out["error"])
		if !strings.Contains(strings.ToLower(msg), "reserved") {
			t.Errorf("slug=%q refusal %q does not say the name is reserved for JomForm", slug, msg)
		}
		if !strings.Contains(msg, slug) {
			t.Errorf("slug=%q refusal %q does not name the label it refused", slug, msg)
		}
		// And nothing moved — read the ROW, not the answer.
		if got := updateWSSlug(t, pool, wsIDs[0]); got != before {
			t.Fatalf("slug=%q was refused but the workspace moved: %q -> %q", slug, before, got)
		}
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM workspace_slug_aliases WHERE slug=$1 AND workspace_id=$2`,
			slug, wsIDs[0]).Scan(&n); err != nil {
			t.Fatalf("count aliases for %q: %v", slug, err)
		}
		if n != 0 {
			t.Errorf("slug=%q was refused but %d alias row(s) were written", slug, n)
		}
	}

	// The alias table must be untouched for this workspace: every refusal above
	// is a refusal, so none of them may have reserved an address.
	aliases, err := sp.AliasesForWorkspace(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("AliasesForWorkspace: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("the refused renames left alias rows %v behind", aliases)
	}
}

// TestMCPReservedRefusalIsNotTheGenericSlugError is the boundary that keeps the
// two rules apart, measured on the agent's answer.
//
// The reserved LIST and the "*jomform" TAIL are different rules: one is a policy
// refusal about a well-formed name JomForm owns (answered with the reserved code),
// the other is the caller's own input being shaped badly (answered as a slug
// error). Collapsing them would send the dashboard to its "3-40 chars, a-z, 0-9,
// dash" sentence for a name like `billing`, which is both wrong and wrong in a way
// that makes the merchant retry the sensitive name.
func TestMCPReservedRefusalIsNotTheGenericSlugError(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updreservedkind", 1)
	httpSrv := wsMCPFixture(t, pool)

	// The reserved label: data, with the reserved code.
	reserved := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "billing", "confirm": true}))
	if str(reserved["code"]) != urlguard.ReservedSubdomainCode {
		t.Errorf("a reserved label answered code %q, want %q (answer %v)",
			str(reserved["code"]), urlguard.ReservedSubdomainCode, reserved)
	}

	// A malformed label: still a transport error, and carrying the rule's own
	// shape wording — NOT the reserved token. If the reserved arm swallowed the
	// shape legs, this would come back as data with a reserved code.
	shape := mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "ab", "confirm": true})
	if !shape.IsError {
		t.Errorf("a malformed slug answered as data (%s) — the shape legs must stay transport errors, "+
			"because their remedy is to retype the value", toolResultText(t, shape))
	} else if msg := toolResultText(t, shape); !strings.Contains(msg, dbstore.ErrWorkspaceSlugLength.Error()) {
		t.Errorf("a malformed slug refusal %q does not carry the length rule's own wording %q",
			msg, dbstore.ErrWorkspaceSlugLength.Error())
	}

	// The tail rule ("*jomform"): also a shape refusal, and it must NOT be
	// reported as the reserved-list token either — the two answer differently on
	// the wire and the dashboard renders a different sentence for each.
	tail := mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": "acme-jomform", "confirm": true})
	if !tail.IsError {
		t.Errorf("a *jomform slug answered as data (%s) — the tail rule is a SHAPE refusal and must stay "+
			"a transport error, and it must not carry the reserved token", toolResultText(t, tail))
	}
	// A control on the same tool, so this test cannot pass by the tool having
	// become a blanket refusal.
	if out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "name": "still works"})); out["status"] != "updated" {
		t.Errorf("a legitimate update was refused: %v — the reserved arm is over-reaching", out)
	}
}
