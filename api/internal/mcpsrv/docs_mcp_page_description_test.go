package mcpsrv

// /docs/mcp's per-tool descriptions must be the REGISTERED description, not a
// hand copy that nothing keeps in step.
//
// WHERE THE ROWS LIVE (card t_ee4114af). This guard used to name
// web/src/app/docs/mcp/page.tsx, because /docs/mcp was ONE page and the rows
// were inline on it. Card t_0356ed4c split that page into a hub plus four task
// pages and moved the rows into the module the pages import
// (web/src/lib/mcpToolGroups.ts). The guard now RESOLVES the source instead of
// naming a file — see docsToolGroupsSource in docs_surface_test.go — so the same
// assertion holds whichever file the array is declared in, and a failure names
// the file the rows are actually in. Everything below is the reason the rule
// exists; the paths it mentions are the pre-split ones.
//
// WHY THIS FILE EXISTS (card t_3a0d74d2). web/src/app/docs/mcp/page.tsx is the
// public tool reference: it is the copy a merchant, a search engine and an agent
// scraping the page read. Its rows were written by hand from the Go `Description`
// literals in this package, and nothing checked that the two agreed. Measured on
// this card's base (main af72e713): of 181 registration-to-row pairs, **40 rows
// differed from the description the server actually returns over tools/list** —
// in both directions:
//
//	archive_product    row stopped after the first sentence, so the page never
//	                   said the tool is NOT the trash or how to delete
//	get_workspace_settings
//	                   row listed indexing/sound_enabled/spam_judge_enabled that
//	                   the registered description never mentioned, so an MCP
//	                   client could not know those fields come back at all
//	get_realtime_status
//	                   row named the five event names; the registered description
//	                   named none of them
//
// The two existing guards could not see any of it. mcpToolInventory.test.ts
// checks that a row is a tool the registry registers (and vice versa) and states
// its own limit at :57 — "it does NOT check that a row's DESCRIPTION is accurate
// or matches the Go description"; mcp_parity_test.go checks that every MCP tool
// is documented on at least one docs page. So a row's PROSE was pinned by nothing
// at all, and the page was free to advertise a stale tool contract forever. That
// is exactly how `retry_feedback_notify` came to list four outcomes while its
// schema returned five (card t_ebb4d5f6, PR 1149): the same defect, one row wide.
//
// THE RULE, AND WHICH SURFACE IS THE AUTHORITY. The registered description wins:
// it is the machine-readable contract a client receives over the MCP protocol,
// and a page that carries something else is advertising a tool that does not
// exist. So: **a /docs/mcp row's `desc` must equal the registered Description
// byte for byte.** Where the page carried a fact the registry lacked, this card
// resolved it the other way round — the REGISTRY gained the fact (one edit, two
// surfaces) — rather than deleting documentation from a published page: the five
// admin cross-references, the menu `#493` detail, `get_workspace_settings`'s
// field list, `refund_sale`'s reply fields, `purge_unverified_accounts`'s split,
// the `publish_form`/`bulk_forms` acknowledge rule and others moved into the Go
// descriptions in the same change. All 40 rows are reconciled in that change, so
// this guard is green on arrival.
//
// WHAT IT CHECKS (no database, no network — it always runs):
//
//  1. The page really is being read and parsed: the TOOL_GROUPS block is found,
//     the row extractor finds >= 150 rows, the protocol enumeration finds >= 150
//     tools, and both extracts contain named anchors (list_products, get_account,
//     purge_workspace). A parse that silently found nothing cannot pass.
//  2. Every registered tool has EXACTLY ONE row: a missing row, a duplicated row
//     or a row for a name this package does not register each fails — a duplicate
//     row could carry the right text while the rendered one does not, which is why
//     "at least one row matches" would be the wrong rule.
//  3. Every row's `desc` equals the registered Description (the real string, read
//     over clientSession.ListTools like TestMCPToolsHaveObjectOutputSchema does —
//     never a regex over tools.go, which would make the guard pass on a literal
//     the server never returns).
//  4. A registered tool with an EMPTY description fails: without that, equality
//     could be satisfied by an empty row and an agent would be told nothing.
//
// THE ESCAPING IS THE ONE TRAP (measured while reconciling): the page writes its
// rows as TS string literals, and 48 of its em dashes are `\u2014` and 10 of its
// arrows `\u2192` while the registry carries the literal characters. The row text
// is therefore DECODED through the JS escape rules before it is compared — an
// `\uXXXX` escape is turned back into the character, and an escape this decoder
// does not know fails loudly rather than passing the raw bytes through (which
// would red every row that uses one, with a message about the prose instead of
// about the decoder).
//
// WHAT IT DOES NOT COVER, stated rather than implied:
//   * It reads the page SOURCE, not the served HTML. That a tool card renders at
//     all — and renders this text — is `npm run build` plus a browser read of
//     /docs/mcp; this guard cannot see the render.
//   * It judges the DESCRIPTION only. The order of the rows, the group a tool is
//     filed under, and whether a group's prose is accurate are not checked here.
//   * The decoder handles `\uXXXX`, `\"`, `\\`, `\n`, `\r`, `\t` and the plain
//     punctuation escapes. Only BMP escapes appear in the file today (measured:
//     `\u2014`, `\u2192`); a surrogate PAIR would decode per-unit, which would red
//     a correct row rather than pass a wrong one.
//
// Run with: go test ./internal/mcpsrv/ -count=1

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// docsMCPRow is one `{ name: "…", desc: "…" }` row of the tool list.
type docsMCPRow struct {
	name string
	desc string
	line int // 1-based line in the source that declares the row, for a failure a reader can open
}

// decodeTSStringLiteral decodes the BODY of a double-quoted TypeScript string
// literal (the bytes between the quotes), so the page's `\u2014` compares equal
// to the registry's `—`. It refuses an escape it does not understand instead of
// passing it through: an unknown escape would otherwise be reported as prose
// drift, which is a failure about the wrong thing.
func decodeTSStringLiteral(raw string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '\\' {
			b.WriteByte(raw[i])
			i++
			continue
		}
		if i+1 >= len(raw) {
			return "", fmt.Errorf("trailing backslash")
		}
		switch raw[i+1] {
		case 'u':
			if i+6 > len(raw) {
				return "", fmt.Errorf("truncated \\u escape at byte %d", i)
			}
			n, err := strconv.ParseUint(raw[i+2:i+6], 16, 32)
			if err != nil {
				return "", fmt.Errorf("bad \\u escape %q: %v", raw[i:i+6], err)
			}
			b.WriteRune(rune(n))
			i += 6
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case '"':
			b.WriteByte('"')
			i += 2
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '/':
			b.WriteByte('/')
			i += 2
		case '\'':
			b.WriteByte('\'')
			i += 2
		default:
			return "", fmt.Errorf("unknown escape %q at byte %d — teach the decoder this "+
				"escape rather than reading the row as drifted prose", raw[i:i+2], i)
		}
	}
	return b.String(), nil
}

// scanTSQuoted reads the quoted string literal that starts at s[from] and
// returns its decoded body plus the index just past the closing quote.
func scanTSQuoted(s string, from int) (string, int, error) {
	if from >= len(s) || s[from] != '"' {
		return "", 0, fmt.Errorf("no opening quote at byte %d", from)
	}
	end := from + 1
	for end < len(s) {
		if s[end] == '\\' {
			end += 2
			continue
		}
		if s[end] == '"' {
			body, err := decodeTSStringLiteral(s[from+1 : end])
			if err != nil {
				return "", 0, err
			}
			return body, end + 1, nil
		}
		end++
	}
	return "", 0, fmt.Errorf("unterminated string literal at byte %d", from)
}

// docsMCPRows reads the tool list's `TOOL_GROUPS` array and returns one entry
// per `{ name: "…", desc: "…" }` row. It fails (never skips) when the array
// cannot be found — a guard that goes quiet when its subject disappears is the
// hole it exists to close.
//
// WHERE THE ARRAY LIVES IS RESOLVED, NOT ASSUMED (card t_ee4114af). It used to
// be inline on web/src/app/docs/mcp/page.tsx; the split moved it into a module
// the pages import (web/src/lib/mcpToolGroups.ts). docsToolGroupsSource follows
// the page's own `import` and returns whatever declares the array, so this
// extractor judges the rendered rows on either shape — and names the pages and
// modules it looked in when it can find neither.
func docsMCPRows(t *testing.T) []docsMCPRow {
	t.Helper()

	relPath, src := docsToolGroupsSource(t)

	start := strings.Index(src, "const TOOL_GROUPS")
	if start < 0 {
		t.Fatalf("%s no longer declares `const TOOL_GROUPS` — the row extractor cannot find the "+
			"tool list, so this guard would be judging nothing", relPath)
	}
	end := strings.Index(src[start:], "\n];")
	if end < 0 {
		t.Fatalf("%s's TOOL_GROUPS array has no closing `];` — the extractor has drifted from the "+
			"file's shape", relPath)
	}
	block := src[start : start+end]
	// Row line numbers are reported against the FILE, not the block, so a failure
	// names a line a reader can open.
	lineOffset := strings.Count(src[:start], "\n")

	var rows []docsMCPRow
	for i, line := range strings.Split(block, "\n") {
		// A comment is not a row: the array carries `//` explanations, and a
		// comment that quoted a row would otherwise be read as one. Only
		// whole-line comments are dropped, so a description holding `https://`
		// keeps its text.
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		obj := strings.Index(line, "{ name: \"")
		if obj < 0 {
			continue
		}
		name, next, err := scanTSQuoted(line, obj+len("{ name: "))
		if err != nil {
			t.Fatalf("%s:%d: read row name: %v", relPath, lineOffset+i+1, err)
		}
		descAt := strings.Index(line[next:], "desc: \"")
		if descAt < 0 {
			t.Fatalf("%s:%d: row %q has no `desc:` — the extractor has drifted from the row "+
				"shape, so this row's prose would go unjudged", relPath, lineOffset+i+1, name)
		}
		desc, _, err := scanTSQuoted(line, next+descAt+len("desc: "))
		if err != nil {
			t.Fatalf("%s:%d: read desc for %s: %v", relPath, lineOffset+i+1, name, err)
		}
		rows = append(rows, docsMCPRow{name: name, desc: desc, line: lineOffset + i + 1})
	}
	if strings.TrimSpace(relPath) == "" {
		t.Fatal("the extractor resolved no source for the tool list — every row would be unjudged")
	}
	return rows
}

// registeredDescriptions enumerates every tool over the real MCP protocol and
// returns name → Description, exactly as a client receives it.
func registeredDescriptions(t *testing.T) map[string]string {
	t.Helper()

	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "docs-description-test", Version: "test"}, nil)
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

	out := map[string]string{}
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			out[tl.Name] = tl.Description
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out
}

// firstDifference renders the byte offset and a little context around where two
// strings part, so a failure about a 700-character description is readable.
func firstDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	const window = 60
	from := i - window
	if from < 0 {
		from = 0
	}
	cut := func(s string) string {
		to := i + window
		if to > len(s) {
			to = len(s)
		}
		return s[from:to]
	}
	return fmt.Sprintf("first difference at byte %d\n    registry: …%s…\n    page    : …%s…",
		i, cut(want), cut(got))
}

// TestDocsMCPPageRowsCarryTheRegisteredDescription pins the rule above: every
// /docs/mcp row carries the description the registry actually returns.
func TestDocsMCPPageRowsCarryTheRegisteredDescription(t *testing.T) {
	// The rows' home is RESOLVED, not assumed (card t_ee4114af): the tool list
	// moved out of the /docs/mcp hub into the module its pages import, and a
	// failure must name the file the rows actually live in.
	relPath, _ := docsToolGroupsSource(t)
	rows := docsMCPRows(t)
	reg := registeredDescriptions(t)

	// ── 1. the guard is looking at real data (it cannot pass vacuously) ──────
	if len(rows) < 150 {
		t.Fatalf("parsed only %d row(s) from %s — the extractor has drifted from that page's "+
			"shape, so a row's prose would go unjudged", len(rows), relPath)
	}
	if len(reg) < 150 {
		t.Fatalf("enumerated only %d registered tool(s) over tools/list — registerTools did not "+
			"run, so this guard would require almost nothing", len(reg))
	}
	byName := map[string][]docsMCPRow{}
	for _, r := range rows {
		byName[r.name] = append(byName[r.name], r)
	}
	for _, anchor := range []string{"list_products", "get_account", "purge_workspace"} {
		if len(byName[anchor]) == 0 {
			t.Fatalf("%s has no row for %s, so the row extractor is not reading the tool list",
				relPath, anchor)
		}
		if _, ok := reg[anchor]; !ok {
			t.Fatalf("%s is not registered, so the protocol enumeration is not reading the registry",
				anchor)
		}
	}

	// ── 2. both directions, and no duplicate row ────────────────────────────
	var missing, duplicated, phantom []string
	names := make([]string, 0, len(reg))
	for n := range reg {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch len(byName[n]) {
		case 0:
			missing = append(missing, n)
		case 1:
		default:
			duplicated = append(duplicated,
				fmt.Sprintf("%s (%d rows at %s)", n, len(byName[n]), docsRowLines(byName[n])))
		}
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if _, ok := reg[r.name]; !ok && !seen[r.name] {
			seen[r.name] = true
			phantom = append(phantom, fmt.Sprintf("%s (%s:%d)", r.name, relPath, r.line))
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s omits %d registered tool(s):\n  %s\nThe page says it lists the whole "+
			"registry, so add the row carrying the registered Description.",
			relPath, len(missing), strings.Join(missing, "\n  "))
	}
	if len(duplicated) > 0 {
		t.Errorf("%s lists %d tool(s) more than once:\n  %s\nThe guard cannot tell which row a "+
			"reader sees, so a duplicate could carry the registered text while the rendered row "+
			"does not.", relPath, len(duplicated), strings.Join(duplicated, "\n  "))
	}
	if len(phantom) > 0 {
		t.Errorf("%s advertises %d tool(s) this package does not register:\n  %s",
			relPath, len(phantom), strings.Join(phantom, "\n  "))
	}

	// ── 3/4. the description itself ─────────────────────────────────────────
	var mismatched []string
	for _, n := range names {
		want := reg[n]
		if strings.TrimSpace(want) == "" {
			t.Errorf("%s registers %s with an EMPTY description. An agent is told nothing about "+
				"the call, and equality with a row would then be satisfied by an empty row too.",
				"mcpsrv", n)
			continue
		}
		if len(byName[n]) != 1 {
			continue // already reported above
		}
		got := byName[n][0].desc
		if got == want {
			continue
		}
		mismatched = append(mismatched, fmt.Sprintf(
			"%s  (%s:%d)\n  %s\nThe registered description is the contract a client receives over "+
				"tools/list; the page must carry it verbatim. If the ROW is the one that is right "+
				"(it documents a fact the registry lacks), the registry description must gain that "+
				"fact in the same change — one edit, two surfaces — rather than the two drifting "+
				"apart again.",
			n, relPath, byName[n][0].line, firstDifference(want, got)))
	}
	if len(mismatched) > 0 {
		sort.Strings(mismatched)
		t.Errorf("%d of %d row(s) on %s do NOT carry the registered description:\n\n  %s\n",
			len(mismatched), len(names), relPath, strings.Join(mismatched, "\n\n  "))
	}
}

// docsRowLines lists the source lines of a duplicated row set, for the message.
func docsRowLines(rs []docsMCPRow) string {
	lines := make([]string, 0, len(rs))
	for _, r := range rs {
		lines = append(lines, strconv.Itoa(r.line))
	}
	return "line " + strings.Join(lines, ", line ")
}
