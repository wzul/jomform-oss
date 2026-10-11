package mcpsrv

// retry_feedback_notify's prose must name every outcome its own schema returns.
//
// The tool can answer `skipped`: the retry path returns feedbackNotifySkipped
// when no super-admin recipient is configured (api/internal/httpapi/rest_feedback.go,
// pinned by TestFeedbackNotifyAdminRetryWithNoRecipientsIsNotQueued), and that
// value is reachable from MCP because RetryFeedbackNotify is the REST handler's
// own function. The output type says so four lines away from the registration,
// in retryFeedbackNotifyOut.Notification's `jsonschema` tag.
//
// The Description did not. It listed four outcomes while the tool returned five,
// which is the one omission an operator notices: "queued" for a skip claims mail
// went out when nothing was attempted. The same claim then reached a person,
// because /docs/mcp's row for the tool is that description transcribed. The
// inventory guard that keeps the page's ROWS honest (mcpToolInventory.test.ts)
// explicitly does not read descriptions, so nothing connected the two.
//
// This guard connects them, for this tool, on both surfaces a reader actually
// meets:
//
//  1. the registered tool description over the MCP protocol (tools/list) — the
//     machine-readable contract a connected client receives, and
//  2. the /docs/mcp row — what a person reads.
//
// It is built the way the rest of this package's guards are: the list it asserts
// is DERIVED from the output type's own `jsonschema` tag by reflection, never
// typed a second time, so it cannot rot into a copy of the thing it checks.
//
// Coverage, stated so it is not over-claimed: this pins the outcome LIST for
// retry_feedback_notify only. It does not check the prose of any other tool, and
// it does not prove the sentence explaining `skipped` is well written — only
// that the value is listed, carries a gloss, and is still in the schema.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const retryFeedbackNotifyTool = "retry_feedback_notify"

// outcomeTokenRe matches one enum member of a `jsonschema` tag. A tag is either a
// bare pipe list ("queued|sent|failed|none|skipped") or prose that happens to
// contain bars ("Optional — open|in_progress|resolved|closed"), so only
// lowercase-identifier tokens are read; the count check in
// derivedOutcomeRun keeps a tag that stops parsing from yielding an empty list
// the guard would then assert nothing about.
var outcomeTokenRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// derivedOutcomeRun returns the exact wording both surfaces must carry, built
// from the output type's own tag.
func derivedOutcomeRun(t *testing.T) string {
	t.Helper()

	f, ok := reflect.TypeOf(retryFeedbackNotifyOut{}).FieldByName("Notification")
	if !ok {
		t.Fatal("retryFeedbackNotifyOut has no Notification field — this guard is pinned to the field it reads")
	}
	tag := f.Tag.Get("jsonschema")
	if tag == "" {
		t.Fatal("retryFeedbackNotifyOut.Notification carries no jsonschema tag — the enum this guard derives from is gone")
	}

	var outcomes []string
	for _, tok := range strings.Split(tag, "|") {
		if tok = strings.TrimSpace(tok); outcomeTokenRe.MatchString(tok) {
			outcomes = append(outcomes, tok)
		}
	}
	if len(outcomes) < 2 {
		t.Fatalf("the jsonschema tag %q does not parse into an enum (%v) — the guard would pass without checking anything", tag, outcomes)
	}

	// "skipped" is the value this whole guard exists for, and the retry path
	// really returns it, so dropping it from the schema is not a way to make the
	// prose check below go green.
	skipped := false
	for _, o := range outcomes {
		if o == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("the output schema no longer lists \"skipped\" (%v) — the retry path answers it when no super-admin recipient is configured (api/internal/httpapi/rest_feedback.go, TestFeedbackNotifyAdminRetryWithNoRecipientsIsNotQueued)", outcomes)
	}

	return "Keputusan: " + strings.Join(outcomes, "|")
}

// TestRetryFeedbackNotifyProseNamesEveryOutcomeItsSchemaReturns fails when the
// registered description or the /docs/mcp row stops naming an outcome the schema
// returns.
func TestRetryFeedbackNotifyProseNamesEveryOutcomeItsSchemaReturns(t *testing.T) {
	want := derivedOutcomeRun(t)

	desc := registeredToolDescription(t, retryFeedbackNotifyTool)
	if !strings.Contains(desc, want) {
		t.Errorf("the registered %s description does not carry the schema's outcome list %q — a connected client is told that answer does not exist.\n  description: %s",
			retryFeedbackNotifyTool, want, desc)
	}
	if !strings.Contains(desc, "'skipped'") {
		t.Errorf("the description does not gloss \"skipped\" — it must say what the value means, the way it already does for \"none\".\n  description: %s", desc)
	}

	row := docsMcpRowFor(t, retryFeedbackNotifyTool)
	if !strings.Contains(row, want) {
		t.Errorf("/docs/mcp's row for %s does not carry the schema's outcome list %q — the rendered page is what a person reads.\n  row: %s",
			retryFeedbackNotifyTool, want, row)
	}
}

// registeredToolDescription reads the description of a tool over the real MCP
// protocol, so it sees what a client sees rather than a source regex.
func registeredToolDescription(t *testing.T, name string) string {
	t.Helper()

	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "outcome-prose-test", Version: "test"}, nil)
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

	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			if tl.Name != name {
				continue
			}
			if strings.TrimSpace(tl.Description) == "" {
				t.Fatalf("tool %q is registered with an empty description", name)
			}
			return tl.Description
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	t.Fatalf("tool %q is not registered — this guard is pinned to a tool the registry must carry", name)
	return ""
}

// docsMcpRowFor returns the one /docs/mcp source line that carries the tool's
// row, with the page's `\u2014` escapes resolved so the text reads as it renders.
//
// The page is located relative to this file (api/internal/mcpsrv →
// ../../../web/src/app/docs/mcp/page.tsx). A missing page or a missing/duplicated
// row is a FAILURE, not a skip: the row is the rendered surface this guard
// exists for, and a guard that skips when its subject disappears is the silent
// hole it is meant to close.
func docsMcpRowFor(t *testing.T, tool string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the /docs/mcp page")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "web", "src", "app", "docs", "mcp", "page.tsx"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — /docs/mcp is a surface this guard checks, so a missing page is a failure", path, err)
	}

	needle := `{ name: "` + tool + `"`
	var rows []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, needle) {
			rows = append(rows, strings.ReplaceAll(line, `\u2014`, "—"))
		}
	}
	if len(rows) != 1 {
		t.Fatalf("found %d rows for %q in %s — want exactly one", len(rows), tool, path)
	}
	return rows[0]
}
