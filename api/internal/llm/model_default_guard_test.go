// The guard for the SHIPPED default model (card t_85e6f366).
//
// WHY THIS FILE EXISTS.
//
// `DefaultModel` is not a decoration — it is what every AI surface falls back to
// when LLM_MODEL is unset (llm.New replaces an empty model with it). It named a
// model the provider RETIRED on 2026-09-25, which answers HTTP 410 Gone, so a
// deployment built without the env override offered the assistant, the builder
// AI draft and the legal drafter and failed on every call. Prod only survived
// because the host carried an override; the trap was in the repo, one unset
// variable away from recurring on a fresh host, a rollback or a new environment.
//
// So the constant is pinned here in three directions:
//
//  1. it is the model that works (not a retired family id);
//  2. a client built with an EMPTY model name resolves to it — the fallback
//     path, which is the half a constant-only assertion cannot see;
//  3. every user-facing string that names a model names the SAME one: the MCP
//     tool descriptions, their entries on the docs page and the builder text.
//     AGENTS.md docs-parity says those move together, so they are asserted
//     together rather than eyeballed.
//
// (3) is a REPO-WIDE scan, not a list of files. The defect this guards is a file
// naming a retired model, and a fixed list reports "all good" the moment the id
// is pasted into a fourth place — the same reason the workspace-slug guard walks
// the tree. Exactly one file is exempt, and it is named with its reason below.
//
// WHY THE RETIRED ID IS ASSEMBLED FROM PARTS.
//
// `retiredModelFamily` is built at runtime on purpose: the retired id must not
// be greppable as a model id anywhere on the tree, this file included, because a
// copy-pasteable dead id is how it gets copied back. The only remaining mention
// in the repo is the applied migration in retiredMentionExempt (a historical
// record), and a repo-wide scan for the retired family prefix over the shipped
// tree is the check that says so. Reassembling this constant into one literal
// would put the dead id back on the tree under the guard's own name.
package llm

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// workingModel is the model prod actually runs (LLM_MODEL) and the value this
// repo must default to.
const workingModel = "deepseek-v4.1-flash"

// retiredModelFamily is the retired family PREFIX. Any id starting with it is a
// retired id, tagged or not — the provider's 410 was on the tagged one, and the
// untagged spelling is the same dead family. The prefix does not match
// `deepseek-v4.1-flash` (`.` where the family has `-`), so the working model is
// unaffected by the deny rule.
var retiredModelFamily = "deepseek-v4-" + "flash"

// modelToken matches a model id as it is written in prose, a description or a Go
// string. It must END on an alphanumeric so a trailing sentence period or a
// closing bracket is not swallowed into the token (`…-flash).`), while the inner
// `.`/`:` of a real id are kept — a tagged id is a DIFFERENT id and must not be
// normalised into the untagged one.
var modelToken = regexp.MustCompile(`deepseek-[A-Za-z0-9.:_-]*[A-Za-z0-9]`)

// retiredMentionExempt names the files where a retired id may still appear, with
// the reason, and nothing else is exempt. The migration is an APPLIED historical
// record: editing its comment churns a file the fresh-DB guard replays, and the
// prose describes what the migration did at the time, not what the app should do
// now. It is pinned here rather than ignored so the exemption is visible and the
// file cannot silently become a second place a dead id lives.
var retiredMentionExempt = map[string]string{
	"api/internal/db/migrations/000029_space_legal_pages.up.sql": "applied migration — historical record, never edited",
}

// userFacingModelSites are the files that name the model to a USER or an agent,
// and each must name it. They are listed because the parity claim is about these
// three surfaces specifically (the MCP tool description, its docs entry and the
// builder copy). Removing the mention from one of them is a deliberate decision,
// not an accident: it must edit this guard, which is the point.
var userFacingModelSites = []string{
	"api/internal/mcpsrv/tools.go",
	"web/src/app/docs/mcp/page.tsx",
	"web/src/app/builder/page.tsx",
}

// TestDefaultModelIsNotRetired pins the constant itself. Two assertions, because
// they fail differently: the exact id says WHICH model ships, and the family
// deny rule still bites if someone swaps in another id from the retired family
// (a tag change is not a fix — the whole family is gone).
func TestDefaultModelIsNotRetired(t *testing.T) {
	if DefaultModel == "" {
		t.Fatal("DefaultModel is empty: every AI surface would call the provider with no model at all, " +
			"which is the outage this constant is supposed to prevent")
	}
	if strings.HasPrefix(DefaultModel, retiredModelFamily) {
		t.Fatalf("DefaultModel = %q, which is the RETIRED family %q — the provider answers HTTP 410 Gone for it, "+
			"so a deployment without the LLM_MODEL override dies on every AI call while still offering the "+
			"feature in the UI. That is the outage this guard exists for.", DefaultModel, retiredModelFamily)
	}
	if DefaultModel != workingModel {
		t.Fatalf("DefaultModel = %q, want %q (the model prod runs via LLM_MODEL and the one the provider serves). "+
			"A different id must be a deliberate change to this guard and to the user-facing strings together.",
			DefaultModel, workingModel)
	}
}

// TestEmptyModelResolvesToTheWorkingDefault pins the FALLBACK, which is the half
// a constant assertion cannot see: llm.New is what every caller passes its
// config through, and an empty LLM_MODEL is exactly the state that took prod
// down. An empty model must resolve to the working default — not to the retired
// id, not to "", and not to whatever a future edit to New happens to do.
//
// The override half is asserted in the same test on purpose: a "fix" that made
// New ignore the caller's model would pass the first assertion and break every
// deployment that sets LLM_MODEL deliberately.
func TestEmptyModelResolvesToTheWorkingDefault(t *testing.T) {
	t.Run("an empty model uses the shipped default", func(t *testing.T) {
		c := New("", "")
		if c == nil {
			t.Fatal("New(\"\", \"\") returned nil — there is no client to call the provider with")
		}
		if c.model == "" {
			t.Fatal("a client built with an empty model carries an empty model: the request has no model field, " +
				"so every AI surface fails even though the default exists")
		}
		if strings.HasPrefix(c.model, retiredModelFamily) {
			t.Fatalf("a client built with an empty model resolved to %q — a retired model. This is the "+
				"unset-LLM_MODEL outage reproduced exactly: the deployment looks healthy and every call is a 410.",
				c.model)
		}
		if c.model != workingModel {
			t.Fatalf("a client built with an empty model resolved to %q, want %q", c.model, workingModel)
		}
		if c.model != DefaultModel {
			t.Fatalf("the resolved model %q is not DefaultModel %q — the constant has stopped being the fallback, "+
				"so pinning the constant no longer says anything about what ships", c.model, DefaultModel)
		}
	})

	t.Run("an explicit model still wins", func(t *testing.T) {
		c := New("", "some-other-model")
		if c.model != "some-other-model" {
			t.Fatalf("New(baseURL, %q) resolved to %q — the caller's model must be honoured; a default that "+
				"overrides an explicit choice would silently ignore LLM_MODEL in every deployment",
				"some-other-model", c.model)
		}
	})
}

// TestUserFacingStringsNameTheSameModelAsTheConstant asserts acceptance 3: the
// MCP tool descriptions, their docs entries and the builder copy name the model
// the code actually defaults to — checked as text, not by eye.
//
// It walks the whole repo (test files excluded, so no fixture can satisfy it)
// and requires every model id written anywhere to be the working one, with the
// single documented migration exemption. Direction matters both ways:
//
//   - revert the constant  -> every user-facing string now disagrees with it;
//   - revert a description -> that file names the retired id;
//   - add a NEW file naming a retired model -> it appears in the report by name.
func TestUserFacingStringsNameTheSameModelAsTheConstant(t *testing.T) {
	root := repoRoot(t)
	hits := scanRepoForModelIDs(t, root)

	// The anchor: the constant's own definition must be among the hits, or this
	// guard is reading the wrong tree and would pass vacuously.
	if !hits["api/internal/llm/llm.go"][workingModel] {
		t.Fatalf("api/internal/llm/llm.go does not spell the working model %q — the scan is anchored on the "+
			"wrong file and proves nothing", workingModel)
	}

	// Every hit, minus the documented exemption, must be the working model.
	var stray []string
	for rel, ids := range hits {
		if _, exempt := retiredMentionExempt[rel]; exempt {
			continue
		}
		for id := range ids {
			if id != workingModel {
				stray = append(stray, rel+": "+id)
			}
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Fatalf("these files name a model that is NOT the shipped default %q:\n  %s\n\n"+
			"A retired id here is what the user (or an agent reading the tool description) is told the "+
			"feature runs on. Point each at %q, or — if the mention is legitimately historical — add the "+
			"file to retiredMentionExempt with its reason.",
			workingModel, strings.Join(stray, "\n  "), workingModel)
	}

	// The user-facing surfaces must NAME it (a file that stops mentioning a
	// model cannot be checked for disagreement, and docs-parity says these
	// entries move with the tool).
	for _, rel := range userFacingModelSites {
		if !hits[rel][workingModel] {
			t.Fatalf("%s does not name the model %q. These surfaces tell a user or an agent what the feature "+
				"runs on, and AGENTS.md docs-parity keeps the MCP tool description and its docs entry in step — "+
				"if the mention is being removed deliberately, remove it from this guard in the same change.",
				rel, workingModel)
		}
	}

	// AGENTS.md docs-parity: the MCP tool description and its docs entry are the
	// same sentence. Escapes differ (`\\u2014` on the page, the character in Go),
	// so the comparison normalises them first — otherwise the check would be
	// satisfied by two identical strings and fail on two identical strings.
	toolsDesc := toolDescriptions(t, hits, root)
	docsDesc := docsPageDescriptions(t, root)
	for _, name := range []string{"generate_form", "generate_legal_page"} {
		goDesc, okGo := toolsDesc[name]
		pageDesc, okPage := docsDesc[name]
		if !okGo {
			t.Fatalf("api/internal/mcpsrv/tools.go has no %q tool description — the docs-parity half cannot "+
				"be checked and would pass vacuously", name)
		}
		if !okPage {
			t.Fatalf("web/src/app/docs/mcp/page.tsx has no %q entry — the MCP tool description and its docs "+
				"entry must move together (AGENTS.md docs-parity)", name)
		}
		if goDesc != pageDesc {
			t.Fatalf("the %q description differs between api/internal/mcpsrv/tools.go and "+
				"web/src/app/docs/mcp/page.tsx:\n  tools.go: %q\n  page.tsx: %q\n"+
				"They are the same sentence on two surfaces and must move together.",
				name, goDesc, pageDesc)
		}
	}
}

// repoRoot locates the repository root relative to this test file
// (api/internal/llm -> ../../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the repository root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// scanRepoForModelIDs returns every non-test source file under the repo that
// writes a model id, keyed by repo-relative slash path, with the set of ids it
// contains.
//
// The walk skips `_test.go` (a fixture must not be able to satisfy the guard),
// and the trees that are not shipped source: .git, node_modules, .next, vendor,
// test-results. A file whose ext we do not read cannot be judged, so the walk
// only reads extensions that can carry shipped prose or code.
func scanRepoForModelIDs(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()

	hits := map[string]map[string]bool{}
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, ".next": true, "vendor": true,
		"test-results": true, ".worktrees": true,
	}
	readExts := map[string]bool{
		".go": true, ".tsx": true, ".ts": true, ".jsx": true, ".js": true,
		".md": true, ".mdx": true, ".sql": true, ".yml": true, ".yaml": true,
		".json": true, ".sh": true, ".example": true, ".toml": true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !readExts[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		ids := map[string]bool{}
		for _, m := range modelToken.FindAllString(string(raw), -1) {
			ids[m] = true
		}
		if len(ids) == 0 {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		hits[filepath.ToSlash(rel)] = ids
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s for model ids: %v — the guard cannot run without reading the tree", root, err)
	}
	if len(hits) == 0 {
		t.Fatal("no file on the tree writes a model id — the scan read nothing, so it proves nothing")
	}
	return hits
}

// descriptionPattern pulls `Name: "x"` … `Description: "y"` pairs (tools.go) or
// `{ name: "x", desc: "y" }` pairs (the docs page) into a name→description map.
var (
	goDescriptionPattern   = regexp.MustCompile(`(?s)Name:\s*"([a-z0-9_]+)",\s*Description:\s*"((?:[^"\\]|\\.)*)"`)
	pageDescriptionPattern = regexp.MustCompile(`(?s)\{\s*name:\s*"([a-z0-9_]+)",\s*desc:\s*"((?:[^"\\]|\\.)*)"`)
)

func toolDescriptions(t *testing.T, hits map[string]map[string]bool, root string) map[string]string {
	t.Helper()
	const rel = "api/internal/mcpsrv/tools.go"
	if _, ok := hits[rel]; !ok {
		t.Fatalf("%s writes no model id — the tool descriptions the docs must match have moved or been rewritten", rel)
	}
	return readDescriptions(t, filepath.Join(root, rel), goDescriptionPattern, rel)
}

func docsPageDescriptions(t *testing.T, root string) map[string]string {
	t.Helper()
	const rel = "web/src/app/docs/mcp/page.tsx"
	return readDescriptions(t, filepath.Join(root, rel), pageDescriptionPattern, rel)
}

func readDescriptions(t *testing.T, path string, re *regexp.Regexp, rel string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the docs-parity half of this guard cannot run without it", rel, err)
	}
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = unescapeUnicode(m[2])
	}
	if len(out) == 0 {
		t.Fatalf("no tool name/description pairs parsed from %s — the pattern matched nothing, so the "+
			"docs-parity assertion would pass vacuously", rel)
	}
	return out
}

// unescapeUnicode resolves the `\uXXXX` escapes a TSX string literal may use for
// a character the Go source spells directly, so the two can be compared.
var unicodeEscape = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

func unescapeUnicode(s string) string {
	return unicodeEscape.ReplaceAllStringFunc(s, func(m string) string {
		var r rune
		for _, c := range []byte(m[2:]) {
			r <<= 4
			switch {
			case c >= '0' && c <= '9':
				r += rune(c - '0')
			case c >= 'a' && c <= 'f':
				r += rune(c-'a') + 10
			case c >= 'A' && c <= 'F':
				r += rune(c-'A') + 10
			}
		}
		return string(r)
	})
}
