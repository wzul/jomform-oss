package mcpsrv

// The workspace NAME cap over the REAL MCP transport.
//
// Two things only the protocol can show, and both were wrong:
//
//  1. AN OVER-LONG NAME REACHED THE AGENT AS `db: <message>`. The cap's refusal
//     was a fresh errors.New, which matched no arm of updateWorkspace's switch,
//     so it fell through to `fmt.Errorf("db: %w", err)` — the agent saw a
//     database-shaped error for a name that is merely too long, and could not
//     tell it apart from a real storage fault. It now maps to the sentinel's own
//     stated message.
//  2. THE TOOL IS A SECOND WRITE PATH. create_workspace enforced no cap at all
//     while update_workspace enforced the byte cap, so the two ends of the same
//     value disagreed; the assertions below drive BOTH tools with the same name
//     and require the same verdict, so the rule cannot drift back into two.
//
// Calls go through HTTPHandler → authenticate → the tool handler with a real API
// key, exactly as a configured MCP client does.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// TestMCPUpdateWorkspaceNameCapIsAClassifiedRefusal is requirement 1/2 over MCP: a
// name at the character cap renames and the VALUE is asserted; one over is
// refused with the rule's own wording, never a wrapped `db:` error.
func TestMCPUpdateWorkspaceNameCapIsAClassifiedRefusal(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updnamecap", 1)
	httpSrv := wsMCPFixture(t, pool)
	sp := dbstore.NewWorkspaceStore(pool)

	// At the cap, and the two multi-byte names the byte check used to refuse.
	for _, tc := range []struct{ why, name string }{
		{"45 CJK characters = 135 BYTES; the byte check refused it, the character rule accepts it",
			strings.Repeat("店", 45)},
		{"120 accented characters = 240 bytes",
			strings.Repeat("é", 120)},
	} {
		out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
			map[string]any{"workspace_id": wsIDs[0], "name": tc.name}))
		if out["status"] != "updated" {
			t.Fatalf("%s\nupdate_workspace refused it: %v", tc.why, out)
		}
		if out["name"] != tc.name {
			t.Errorf("%s\nanswer name = %v, want the submitted name verbatim", tc.why, out["name"])
		}
		got, err := sp.ByID(ctx, wsIDs[0])
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != tc.name {
			t.Errorf("%s\nrow name = %q, want the submitted name", tc.why, got.Name)
		}
	}

	// One character over: an error result, WITHOUT the `db:` prefix that the
	// unclassified fall-through added.
	over := strings.Repeat("a", dbstore.WorkspaceNameMaxChars+1)
	res := mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "name": over})
	if !res.IsError {
		t.Fatalf("an over-long name was accepted over MCP: %s", toolResultText(t, res))
	}
	msg := toolResultText(t, res)
	if strings.Contains(msg, "db:") {
		t.Errorf("the refusal is wrapped as a database error: %q — an agent cannot tell \"the name is too "+
			"long\" from a real storage fault", msg)
	}
	if !strings.Contains(msg, dbstore.ErrWorkspaceNameLength.Error()) {
		t.Errorf("the refusal does not carry the rule's own wording (%q): %q",
			dbstore.ErrWorkspaceNameLength.Error(), msg)
	}
	// Nothing was written by the refused call.
	got, err := sp.ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name == over {
		t.Error("the refused over-long name was written anyway")
	}
}

// TestMCPCreateAndUpdateAgreeOnTheNameCap is requirement 4 on the MCP surface: the
// SAME name must get the same verdict from create_workspace and update_workspace.
// Before the fix create accepted anything while update refused over 120 bytes.
func TestMCPCreateAndUpdateAgreeOnTheNameCap(t *testing.T) {
	requireTestDB(t)
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updnameparity", 1)
	httpSrv := wsMCPFixture(t, pool)

	for _, tc := range []struct {
		label string
		name  string
		ok    bool
	}{
		{"at the cap", strings.Repeat("a", dbstore.WorkspaceNameMaxChars), true},
		{"45 CJK characters", strings.Repeat("店", 45), true},
		{"over the cap", strings.Repeat("a", dbstore.WorkspaceNameMaxChars+1), false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			// A unique slug per subtest, and a short one: the create leg must not
			// fail for a reason that has nothing to do with the name.
			createSlug := "mcpnamecap-" + strings.ToLower(strings.ReplaceAll(tc.label, " ", ""))
			if r := []rune(createSlug); len(r) > 40 {
				createSlug = string(r[:40])
			}
			cres := mcpCall(t, httpSrv.URL, rawKey, "create_workspace",
				map[string]any{"name": tc.name, "slug": createSlug})
			createOK := !cres.IsError

			ures := mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
				map[string]any{"workspace_id": wsIDs[0], "name": tc.name})
			updateOK := !ures.IsError

			if createOK != tc.ok {
				t.Errorf("create_workspace with a %d-character name ok=%v, want %v: %s",
					utf8.RuneCountInString(tc.name), createOK, tc.ok, toolResultText(t, cres))
			}
			if updateOK != tc.ok {
				t.Errorf("update_workspace with a %d-character name ok=%v, want %v: %s",
					utf8.RuneCountInString(tc.name), updateOK, tc.ok, toolResultText(t, ures))
			}
			if createOK != updateOK {
				t.Errorf("create_workspace and update_workspace DISAGREE about a %d-character name: "+
					"create ok=%v (%s) update ok=%v (%s) — the name rule is not one rule",
					utf8.RuneCountInString(tc.name), createOK, toolResultText(t, cres),
					updateOK, toolResultText(t, ures))
			}
			if !tc.ok {
				// The REASON, not just the verdict: the name rule returns ""
				// together with its error, so a create path that ran the
				// emptiness test first would refuse an over-long name as a
				// MISSING one — same status, wrong reason, and no status check
				// can tell them apart.
				want := dbstore.ErrWorkspaceNameLength.Error()
				if got := toolResultText(t, cres); !strings.Contains(got, want) {
					t.Errorf("create_workspace refused an over-long name with %q, want the name rule's "+
						"own reason %q", got, want)
				}
				if got := toolResultText(t, ures); !strings.Contains(got, want) {
					t.Errorf("update_workspace refused an over-long name with %q, want %q", got, want)
				}
			}
		})
	}
}

// TestMCPUpdateWorkspaceSlugOnlySurvivesALegacyOverLongStoredName is the MCP half
// of requirement 3: a slug-only update on a workspace whose STORED name is over
// the old byte cap must go through. An agent asked to move the public address of
// such a workspace could not — the request carried no name, but the omitted field
// fell back to the stored one and was length-checked.
func TestMCPUpdateWorkspaceSlugOnlySurvivesALegacyOverLongStoredName(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	_, wsIDs, _, rawKey := wsMCPUserWorkspaces(t, pool, "updlegacylong", 1)
	httpSrv := wsMCPFixture(t, pool)

	before := updateWSSlug(t, pool, wsIDs[0])
	stored := strings.Repeat("店", 200)
	if _, err := pool.Exec(ctx, `UPDATE workspaces SET name=$2 WHERE id=$1`, wsIDs[0], stored); err != nil {
		t.Fatalf("seed the legacy over-long name: %v", err)
	}
	newSlug := "mcp-legacylong-" + wsIDs[0][:6]

	out := succeeded(t, mcpCall(t, httpSrv.URL, rawKey, "update_workspace",
		map[string]any{"workspace_id": wsIDs[0], "slug": newSlug, "confirm": true}))
	if out["status"] != "updated" {
		t.Fatalf("a slug-only update of a workspace whose stored name is over the cap: %v, want "+
			"status=updated", out)
	}
	if out["slug"] != newSlug || updateWSSlug(t, pool, wsIDs[0]) != newSlug {
		t.Fatalf("slug = %v (row %q), want %q", out["slug"], updateWSSlug(t, pool, wsIDs[0]), newSlug)
	}
	// The promise the agent has to be able to relay, and the stored name is
	// untouched by a request that never mentioned it.
	if out["previous_slug"] != before || out["previous_url_redirects"] != true {
		t.Errorf("answer = %v, want previous_slug=%q with previous_url_redirects=true", out, before)
	}
	got, err := dbstore.NewWorkspaceStore(pool).ByID(ctx, wsIDs[0])
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Name != stored {
		t.Errorf("the stored name was rewritten by a slug-only update: %q", got.Name)
	}
}
