package mcpsrv

// THE /docs/mcp SURFACE IS FOUR PAGES PLUS ONE MODULE — not one file.
//
// WHY THIS FILE EXISTS (card t_ee4114af). /docs/mcp used to be ONE page:
// web/src/app/docs/mcp/page.tsx carried the prose, the tool rows and the model
// mention, so every Go guard that judged "the docs" could name that one path and
// be right. Card t_0356ed4c split the page — it was 7,685 words and 35,608px
// tall on a phone — into a hub plus four task pages:
//
//	/docs/mcp                 the hub (an index, ~300 words)
//	/docs/mcp/connect         set the client up
//	/docs/mcp/tools           the registry, as tables
//	/docs/mcp/oauth           discovery, scopes, grants
//	/docs/mcp/troubleshooting what a refusal means
//
// and moved the tool rows into ONE module the pages import,
// web/src/lib/mcpToolGroups.ts. The web-side guards were widened for the split;
// the Go-side ones were not, so on the merge train they all still read the hub —
// which by then held neither a row nor the model mention — and seven guards went
// RED. They failed loudly rather than going vacuous, which is the right failure,
// but the fix is to widen them, not to re-add copy to the hub.
//
// So this file is the ONE place that knows where the /docs/mcp copy lives. Every
// Go guard that asks "is this documented on /docs/mcp?" goes through
// readDocsSurface / docsToolRowFor / docsToolGroupsSource instead of naming a
// page, so the next move of this copy is a one-line edit here rather than seven
// guards reading a file that no longer holds what they judge.
//
// WHAT IT DOES NOT DO: it never turns a missing file into a skip. A page in the
// set that cannot be read, or a tool list that cannot be found in any file the
// pages point at, is a FATAL that names every path it looked in. A guard that
// goes quiet when its subject disappears is the hole it exists to close.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// docsMCPPageRelPaths is every page of /docs/mcp after the split, repo-relative.
// The hub is kept in the set on purpose: a sentence can legitimately move back
// onto it, and a guard that only read the four task pages would then miss copy a
// reader does see.
var docsMCPPageRelPaths = []string{
	"web/src/app/docs/mcp/page.tsx",
	"web/src/app/docs/mcp/connect/page.tsx",
	"web/src/app/docs/mcp/tools/page.tsx",
	"web/src/app/docs/mcp/oauth/page.tsx",
	"web/src/app/docs/mcp/troubleshooting/page.tsx",
}

// docsMCPToolGroupsRelPath is where the tool rows live now. It is NOT assumed by
// the row extractor (which follows each page's `import`), but it is part of the
// surface: a tool name on this module reaches a reader through the tables
// /docs/mcp/tools renders from it, so "documented on /docs/mcp" includes it.
const docsMCPToolGroupsRelPath = "web/src/lib/mcpToolGroups.ts"

// docsRepoRoot resolves the repository root from this file's own location
// (api/internal/mcpsrv → ../../..). It is derived, never a relative path from
// the process working directory, so the guards behave the same however the test
// binary was invoked.
func docsRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the repository root, so the /docs/mcp " +
			"guards cannot read their subject")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// docsSurfaceFiles is every repo-relative file that can carry /docs/mcp copy.
func docsSurfaceFiles() []string {
	out := make([]string, 0, len(docsMCPPageRelPaths)+1)
	out = append(out, docsMCPPageRelPaths...)
	return append(out, docsMCPToolGroupsRelPath)
}

// readDocsSurface reads every file in the set and returns it keyed by
// repo-relative path. A file that cannot be read FAILS — naming every path it
// looked in — rather than shrinking the surface in silence: a smaller surface is
// a weaker guard that still reports green.
func readDocsSurface(t *testing.T) map[string]string {
	t.Helper()
	root := docsRepoRoot(t)
	files := docsSurfaceFiles()

	out := make(map[string]string, len(files))
	var missing []string
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			missing = append(missing, fmt.Sprintf("read %s: %v", rel, err))
			continue
		}
		out[rel] = string(raw)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("the /docs/mcp surface is incomplete under %s — %d of %d file(s) could not be "+
			"read:\n  %s\nThe tool copy is documented nowhere a user reads without these, so this "+
			"guard fails rather than judging a surface it could not read (it looked in: %s).",
			root, len(missing), len(files), strings.Join(missing, "\n  "), strings.Join(files, ", "))
	}
	return out
}

// docsToolRow is one source line that declares a tool row, and where it lives.
type docsToolRow struct {
	file string // repo-relative
	line int    // 1-based, for a failure a reader can open
	text string
}

// String renders the row's position as `path:line`, the form every failure
// message uses so a reader can jump straight to it.
func (r docsToolRow) String() string {
	return fmt.Sprintf("%s:%d", r.file, r.line)
}

// docsToolRowsFor returns every source line across the whole /docs/mcp surface
// that DECLARES a row for `tool`. It reads the surface even when it finds a match
// early, so the duplicate rule below a caller sees one consistent view.
//
// The needle is the row shape the pages use — `{ name: "list_products"` — not a
// bare mention: a tool name in prose or in a cross-reference is not a row, and
// treating it as one is how a duplicate check starts refusing a correct tree.
func docsToolRowsFor(t *testing.T, tool string) []docsToolRow {
	t.Helper()
	surface := readDocsSurface(t)

	files := make([]string, 0, len(surface))
	for rel := range surface {
		files = append(files, rel)
	}
	sort.Strings(files)

	needle := `{ name: "` + tool + `"`
	var rows []docsToolRow
	for _, rel := range files {
		for i, line := range strings.Split(surface[rel], "\n") {
			if strings.Contains(line, needle) {
				rows = append(rows, docsToolRow{file: rel, line: i + 1, text: line})
			}
		}
	}
	return rows
}

// docsToolRowFor returns THE one row for `tool` on the /docs/mcp surface and
// refuses to guess otherwise. Zero rows and two rows are both failures: a
// duplicate could carry the checked text while the row a reader sees does not,
// and a missing row means the tool is documented nowhere a user reads.
func docsToolRowFor(t *testing.T, tool string) docsToolRow {
	t.Helper()
	rows := docsToolRowsFor(t, tool)
	switch len(rows) {
	case 0:
		t.Fatalf("no docs entry for %s on any page of the /docs/mcp surface — looked in: %s.\n"+
			"DOCUMENTATION PARITY requires exactly one row, and without it the tool is "+
			"documented nowhere a user reads.", tool, strings.Join(docsSurfaceFiles(), ", "))
	case 1:
		return rows[0]
	default:
		where := make([]string, 0, len(rows))
		for _, r := range rows {
			where = append(where, r.String())
		}
		t.Fatalf("found %d docs entries for %s on the /docs/mcp surface (%s) — the guard cannot "+
			"tell which one a reader sees, so a duplicate row could carry the checked text while "+
			"the rendered one does not.", len(rows), tool, strings.Join(where, ", "))
	}
	return docsToolRow{}
}

// toolGroupsImportRe captures `import { … } from "…"` so the row extractor can
// read the module a page actually imports instead of assuming the array is
// inline on the page.
var toolGroupsImportRe = regexp.MustCompile(`import\s*\{([^}]*)\}\s*from\s*"([^"]+)"`)

// docsToolGroupsSource finds the source that declares `const TOOL_GROUPS` and
// returns it plus its repo-relative path.
//
// It FOLLOWS THE DATA rather than naming a file, because that is exactly how the
// guards broke: the array moved into a module and every guard that had the page
// path hard-coded went red against a page that was, by then, correct. So:
//
//  1. a page that declares `const TOOL_GROUPS` inline is the answer (the shape
//     the pages had before the split, and the shape they would have again if the
//     rows moved back);
//  2. otherwise the import each page writes is resolved relative to that page and
//     read — the module the page renders from IS the tool list;
//  3. otherwise this FAILS, naming the pages it read and the import targets it
//     tried, including any that could not be opened. A missing module is not a
//     skip: it is the row list a reader sees, gone.
func docsToolGroupsSource(t *testing.T) (string, string) {
	t.Helper()
	root := docsRepoRoot(t)

	var problems []string
	// (1) inline on a page.
	for _, rel := range docsMCPPageRelPaths {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			problems = append(problems, fmt.Sprintf("read %s: %v", rel, err))
			continue
		}
		if strings.Contains(string(raw), "const TOOL_GROUPS") {
			return rel, string(raw)
		}
	}

	// (2) the module a page imports.
	tried := map[string]bool{}
	for _, rel := range docsMCPPageRelPaths {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // already recorded above
		}
		for _, m := range toolGroupsImportRe.FindAllStringSubmatch(string(raw), -1) {
			if !strings.Contains(m[1], "TOOL_GROUPS") {
				continue
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(rel), m[2]))
			if filepath.Ext(target) == "" {
				target += ".ts"
			}
			if tried[target] {
				continue
			}
			tried[target] = true
			src, rerr := os.ReadFile(filepath.Join(root, target))
			if rerr != nil {
				problems = append(problems, fmt.Sprintf("read %s (imported by %s): %v", target, rel, rerr))
				continue
			}
			if strings.Contains(string(src), "const TOOL_GROUPS") {
				return target, string(src)
			}
			problems = append(problems, fmt.Sprintf("%s (imported by %s) declares no `const TOOL_GROUPS`",
				target, rel))
		}
	}

	targets := make([]string, 0, len(tried))
	for target := range tried {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	sort.Strings(problems)
	t.Fatalf("no source declares `const TOOL_GROUPS`, so the row extractor cannot find the tool "+
		"list and would be judging nothing.\n  pages read: %s\n  modules imported: %s\n  %s\n"+
		"The tool list is the copy a reader sees on /docs/mcp/tools; if it moved, teach this "+
		"reader where, do not delete the guard.",
		strings.Join(docsMCPPageRelPaths, ", "),
		strings.Join(targets, ", "),
		strings.Join(problems, "\n  "))
	return "", ""
}
