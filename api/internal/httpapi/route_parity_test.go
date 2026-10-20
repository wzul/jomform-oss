// Issue #472: route-parity guard.
//
// Two production incidents (#450, #459) shared one root cause: a backend route
// was renamed and the frontend kept calling the old path, and nothing failed
// until a user hit a broken page.
//
// This test closes that gap mechanically. It walks the Next.js source tree,
// extracts every literal /api/* path the frontend calls, maps each one to its
// backend form — the proxy in web/src/app/api/[...path]/route.ts rewrites
// /api/<x> -> {API}/v1/<x>, so the mapping is purely mechanical — and asserts
// that the backend mux actually has a matching route. A rename on one side
// only therefore fails this test with the list of orphan paths.
//
// Nothing here talks to a live server or a database: the mux is assembled
// in-process from the same RegisterRoutes calls Server.Handler() makes, and a
// route is considered to exist when the mux does not answer 404 for it.
//
// A literal that is not a request path at all — `/api/cf/status resolves before
// the first fetch`, a message that happens to start with a real path — is
// reported as a failure naming the literal and the file that carries it. It is
// never truncated to the path-shaped prefix, and it must never reach
// httptest.NewRequest: that panics, and a panic here kills the test binary, so
// one malformed string in the frontend would discard this package's entire
// verdict.
package httpapi

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// frontendSrcDir is the Next.js source tree, relative to this package's
// directory (Go runs tests with the package dir as the working directory).
const frontendSrcDir = "../../../web/src"

// frontendAPIPathMin is a floor on how many /api/* paths extraction must find.
// It exists so a broken extractor (a regex that silently matches nothing) can
// never turn this guard into a no-op that always passes. The tree carries well
// over 100 paths; 40 is a deliberately loose floor.
const frontendAPIPathMin = 40

var (
	// Double-quoted string literals that start with /api/.
	apiDoubleQuoted = regexp.MustCompile(`"(/api/[^"\n]*)"`)
	// Template literals that start with /api/.
	apiTemplateLiteral = regexp.MustCompile("`(/api/[^`\n]*)`")
)

// routeParityAllowlist exempts frontend paths that genuinely cannot be checked
// against a registered route, so the guard stays honest instead of noisy.
//
// Every entry MUST carry a reason. A path that is merely stale (renamed on the
// backend and not updated on the frontend) is exactly the bug this test exists
// to catch — it belongs in a fix, not in this map.
var routeParityAllowlist = map[string]string{}

// TestFrontendAPIPathsResolveToBackendRoutes is the guard: every /api/* path
// the frontend calls must map onto a registered backend route.
func TestFrontendAPIPathsResolveToBackendRoutes(t *testing.T) {
	paths, malformed := extractFrontendAPIPaths(t)

	// A literal that is not a request path is REPORTED, not fatal, and never
	// silently truncated: `/api/foo bar` is a frontend mistake, and shortening
	// it to `/api/foo` here would hide it exactly the way a prefix-matching
	// rename guard hides a rename. Reporting it is also what stops one bad
	// string from taking this package's whole run down — httptest.NewRequest
	// panics on a target containing a space, and that panic discarded every
	// verdict in `go test ./...` for one malformed literal in a web test file.
	for _, bad := range malformed {
		t.Errorf("frontend /api/ literal %q in %s normalises to %q, which cannot be checked: %s",
			bad.Literal, strings.Join(bad.Files, ", "), bad.Path, bad.Reason)
	}
	if len(malformed) > 0 {
		t.Logf("%d frontend /api/* literal(s) were not checked: they are not request paths",
			len(malformed))
	}

	if len(paths) < frontendAPIPathMin {
		t.Fatalf("extracted only %d /api/* paths from %s (expected at least %d) — the extractor is broken, so this guard would pass vacuously",
			len(paths), frontendSrcDir, frontendAPIPathMin)
	}
	t.Logf("extracted %d unique frontend /api/* paths", len(paths))

	mux := buildParityMux()

	var orphans []string
	checked := 0
	for _, frontendPath := range sortedKeys(paths) {
		if reason, ok := routeParityAllowlist[frontendPath]; ok {
			t.Logf("allowlisted %s: %s", frontendPath, reason)
			continue
		}
		backendPath := "/v1" + strings.TrimPrefix(frontendPath, "/api")
		exists, _, err := routeExists(mux, backendPath)
		if err != nil {
			// Extraction already refuses a path that is not a request target,
			// so this is a hole in the rules above rather than a frontend
			// mistake. Say so; never die.
			t.Errorf("%s maps to backend path %q, which cannot be probed: %v",
				frontendPath, backendPath, err)
			continue
		}
		checked++
		if !exists {
			orphans = append(orphans, fmt.Sprintf(
				"%s  ->  %s  [404 from mux]  called from: %s",
				frontendPath, backendPath, strings.Join(paths[frontendPath], ", ")))
		}
	}

	if len(orphans) > 0 {
		t.Fatalf("%d frontend /api/* path(s) do not resolve to a registered backend route — "+
			"a rename landed on one side only (see #450, #459).\n"+
			"Fix the frontend path (or the backend route), or add an allowlist entry in "+
			"routeParityAllowlist with a reason:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
	t.Logf("all %d frontend /api/* paths resolve to a registered backend route", checked)
}

// TestRouteParityGuardDetectsRenamedPath proves the guard actually fails on a
// one-sided rename: the pre-#431 path that broke production must NOT resolve,
// while its replacement must. Without this, a mux assembled too loosely (e.g. a
// catch-all route) could make the guard above pass for the wrong reason.
func TestRouteParityGuardDetectsRenamedPath(t *testing.T) {
	mux := buildParityMux()

	// The old Space→Workspace name (#431/#459): /api/rest/spaces 404'd in
	// production because the backend route had moved to /v1/workspaces.
	if exists, _, err := routeExists(mux, "/v1/rest/spaces"); err != nil {
		t.Errorf("routeExists(/v1/rest/spaces): %v", err)
	} else if exists {
		t.Errorf("guard is not sensitive to renames: /v1/rest/spaces still resolves, " +
			"so an orphaned frontend path would go undetected")
	}
	if exists, status, err := routeExists(mux, "/v1/workspaces"); err != nil {
		t.Errorf("routeExists(/v1/workspaces): %v", err)
	} else if !exists {
		t.Errorf("sanity check failed: /v1/workspaces should resolve (status=%d)", status)
	}
}

// buildParityMux assembles the route table the same way Server.Handler()
// (server.go) does, but with bare handlers: no database, no Valkey, no
// network. Authed routes short-circuit to 401 because resolve() finds no
// session, so no store is ever dereferenced.
//
// The groups below mirror server.go's Handler(). If a new handler group is
// wired there, add it here too — a missing group makes this test fail loudly
// with false orphans rather than pass silently.
func buildParityMux() *http.ServeMux {
	mux := http.NewServeMux()
	(&AuthHandlers{}).RegisterRoutes(mux)
	(&RESTHandlers{}).RegisterRoutes(mux)
	(&PublicHandlers{}).RegisterRoutes(mux)
	(&PaymentsHandlers{}).RegisterRoutes(mux)
	return mux
}

// routeExists reports whether the mux has a route that accepts path. Go 1.22+
// ServeMux exposes no public pattern list, so this asks the mux itself: an
// exact pattern match means the route exists; otherwise the request is served
// to find out whether the mux answers 404 (no route) or something else (405
// Method Not Allowed / 301 clean-path redirect — both prove a route matched).
//
// A path that is not a request target is returned as an error, never as a
// panic: a panic here kills the whole test binary, so one malformed string
// discards every verdict in `go test ./...` (the incident this guards).
func routeExists(mux *http.ServeMux, path string) (exists bool, status int, err error) {
	req, err := newParityRequest(path)
	if err != nil {
		return false, 0, err
	}

	if _, pattern := mux.Handler(req); pattern != "" {
		return true, http.StatusOK, nil
	}

	// No GET pattern: execute whatever the mux handed back (its NotFound
	// handler, or its 405/redirect handler) to tell the two apart.
	rec := httptest.NewRecorder()
	status = 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				// A panic can only come from a handler the mux selected,
				// i.e. a pattern matched. The bare handlers have nil stores,
				// so treat "blew up" as "route exists".
				status = -1
			}
		}()
		mux.ServeHTTP(rec, req)
		status = rec.Code
	}()
	return status != http.StatusNotFound, status, nil
}

// newParityRequest builds the probe request, converting the panic that
// httptest.NewRequest raises on a malformed target into an error. Callers must
// validate the path first (validateFrontendAPIPath) so the frontend mistake is
// named — this is the second line of defence that keeps a bad string from
// crashing the binary even if it reaches a call site that skipped validation.
func newParityRequest(path string) (req *http.Request, err error) {
	defer func() {
		if r := recover(); r != nil {
			req, err = nil, fmt.Errorf("%q is not a valid HTTP request target: %v", path, r)
		}
	}()
	return httptest.NewRequest(http.MethodGet, path, nil), nil
}

// apiPathLiteral is one /api/… literal lifted out of the frontend source.
type apiPathLiteral struct {
	// Literal is the raw text between the quotes, exactly as written.
	Literal string
	// Path is normaliseFrontendAPIPath(Literal) — the form handed to the mux.
	Path string
	// Reason is non-empty when Path is not a usable HTTP request path.
	Reason string
	// Files are the files that carry this literal, relative to web/.
	Files []string
}

// scanFrontendAPISource extracts and classifies every /api/… literal in one
// file's source text.
//
// It is separate from the directory walk so the classification can be tested
// against a synthetic body. A probe literal planted in the real web/src tree
// would make every run of the guard fail — that is exactly the situation a test
// must be able to describe without reproducing.
func scanFrontendAPISource(body string) []apiPathLiteral {
	var out []apiPathLiteral
	for _, re := range []*regexp.Regexp{apiDoubleQuoted, apiTemplateLiteral} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			path := normaliseFrontendAPIPath(m[1])
			if path == "" || path == "/api" {
				continue
			}
			out = append(out, apiPathLiteral{
				Literal: m[1],
				Path:    path,
				Reason:  validateFrontendAPIPath(path),
			})
		}
	}
	return out
}

// extractFrontendAPIPaths walks the Next.js source tree and returns every
// literal /api/* path it finds, normalised, mapped to the files that call it.
//
// Literals that do not normalise to a request path are returned separately
// rather than dropped or truncated: the caller reports them as failures naming
// the literal and its file.
func extractFrontendAPIPaths(t *testing.T) (paths map[string][]string, malformed []apiPathLiteral) {
	t.Helper()

	root, err := filepath.Abs(frontendSrcDir)
	if err != nil {
		t.Fatalf("resolve %s: %v", frontendSrcDir, err)
	}
	return extractFrontendAPIPathsFrom(t, root)
}

// extractFrontendAPIPathsFrom is the walk itself, over an arbitrary root, so the
// reporting half of this guard can be exercised against a temporary tree instead
// of against a probe planted in web/src — a probe there would make every real
// run of the guard fail. Reports stay relative to root's parent, i.e. to web/.
func extractFrontendAPIPathsFrom(t *testing.T, root string) (paths map[string][]string, malformed []apiPathLiteral) {
	t.Helper()

	paths = map[string][]string{}
	bad := map[string]*apiPathLiteral{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip build output / dependency trees if they ever appear here.
			switch d.Name() {
			case "node_modules", ".next", "dist":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".ts") && !strings.HasSuffix(d.Name(), ".tsx") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(filepath.Dir(root), path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		for _, lit := range scanFrontendAPISource(string(raw)) {
			if lit.Reason == "" {
				paths[lit.Path] = append(paths[lit.Path], rel)
				continue
			}
			if prev, ok := bad[lit.Literal]; ok {
				prev.Files = append(prev.Files, rel)
				continue
			}
			bad[lit.Literal] = &apiPathLiteral{
				Literal: lit.Literal,
				Path:    lit.Path,
				Reason:  lit.Reason,
				Files:   []string{rel},
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	for p, files := range paths {
		sort.Strings(files)
		paths[p] = dedupeStrings(files)
	}
	malformed = make([]apiPathLiteral, 0, len(bad))
	for _, m := range bad {
		sort.Strings(m.Files)
		malformed = append(malformed, *m)
	}
	sort.Slice(malformed, func(i, j int) bool { return malformed[i].Literal < malformed[j].Literal })
	return paths, malformed
}

// validateFrontendAPIPath reports why a normalised frontend path cannot be used
// as an HTTP request path, or "" when it can.
//
// The rule is RFC 3986's: a request target is built from pchar — unreserved,
// percent-encoded, sub-delims, ":" and "@" — plus "/", and this file's own
// "{param}" segment. Anything else, and in particular whitespace, means the
// literal is not a path but a sentence that starts with one: the guard could
// only "check" it by inventing a route, which is worse than reporting it.
//
// It deliberately does not truncate at the first space. Truncating
// `/api/foo bar` to `/api/foo` would turn a mistake the author must fix into a
// silent pass — the same failure mode as a rename guard that matches on a
// prefix.
func validateFrontendAPIPath(p string) string {
	if !strings.HasPrefix(p, "/api/") {
		return `does not start with "/api/"`
	}
	for i := 0; i < len(p); i++ {
		if isRoutePathByte(p[i]) {
			continue
		}
		return fmt.Sprintf("byte %d is %s", i, describeDisallowedPathByte(p[i]))
	}
	return ""
}

// isRoutePathByte reports whether c may appear in a request target: RFC 3986
// pchar plus "/" and the "{" "}" of a path-parameter segment.
func isRoutePathByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '.', '_', '~': // unreserved
		return true
	case '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=': // sub-delims
		return true
	case ':', '@', '/', '%': // pchar remainder, plus the segment separator
		return true
	case '{', '}': // a path parameter, as normaliseFrontendAPIPath emits it
		return true
	}
	return false
}

// describeDisallowedPathByte names a byte that cannot appear in a request
// target, in the terms of the mistake that produced it.
func describeDisallowedPathByte(c byte) string {
	switch c {
	case ' ':
		return "a space — a /api/… literal must end at the path; prose after the path belongs outside the string"
	case '	':
		return "a tab"
	case '\n':
		return "a newline"
	}
	if c < 0x20 || c == 0x7f {
		return fmt.Sprintf("a control character (%#x)", c)
	}
	if c >= 0x80 {
		return fmt.Sprintf("a non-ASCII byte (%#x) — percent-encode it", c)
	}
	return fmt.Sprintf("%q, which is not allowed unescaped in a request target — percent-encode it", string(c))
}

// normaliseFrontendAPIPath turns a frontend path literal into the stable form
// the backend route is written in:
//
//	"/api/rest/forms"                                 -> "/api/rest/forms"
//	`/api/rest/forms/${id}${workspaceQuery()}`        -> "/api/rest/forms/{param}"
//	`/api/rest/sales/${encodeURIComponent(ref)}/confirm${q}` -> "/api/rest/sales/{param}/confirm"
//	`/api/rest/media?limit=100${q}`                   -> "/api/rest/media"
//
// Query strings are dropped (routes are matched on path), a ${...}
// interpolation sitting in a path-segment position becomes {param}, and a
// trailing slash is trimmed.
func normaliseFrontendAPIPath(lit string) string {
	var b strings.Builder
	for i := 0; i < len(lit); {
		c := lit[i]
		if c == '?' {
			break // query string: the route is matched on the path only
		}
		if c == '$' && i+1 < len(lit) && lit[i+1] == '{' {
			if !strings.HasSuffix(b.String(), "/") {
				// An interpolation that does not occupy a whole path segment
				// (e.g. `.../forms${q}` or `.../sales/export${suffix}.csv`)
				// appends to the segment: everything after it is unknown.
				break
			}
			b.WriteString("{param}")
			depth := 1
			i += 2
			for i < len(lit) && depth > 0 {
				switch lit[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return strings.TrimSuffix(b.String(), "/")
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i > 0 && s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	return out
}

// TestRouteParityClassifiesLiteralsThatAreNotPaths pins the fix for the crash
// that took a whole api-package verdict down: a /api/… literal that continues
// past the path with prose normalised to a string containing spaces, and
// httptest.NewRequest panicked on it — so the guard reported nothing about the
// frontend and `go test ./...` died 511s into its run.
//
// The requirement is that such a literal is REPORTED and never silently
// truncated: wantPath for the prose case is the whole sentence, so an
// implementation that cuts at the first space and calls /api/cf/status a valid
// path fails this table.
func TestRouteParityClassifiesLiteralsThatAreNotPaths(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantPath   string
		wantReason string // substring; "" means the literal is a usable path
	}{
		{
			name:       "prose after a template literal path",
			source:     "const msg = `/api/cf/status resolves before the first fetch`;",
			wantPath:   "/api/cf/status resolves before the first fetch",
			wantReason: "a space",
		},
		{
			name:       "prose after a double-quoted path",
			source:     `const p = "/api/x y";`,
			wantPath:   "/api/x y",
			wantReason: "a space",
		},
		{
			name:       "fragment in the literal",
			source:     `const p = "/api/rest/forms#general";`,
			wantPath:   "/api/rest/forms#general",
			wantReason: `"#"`,
		},
		{
			name:     "plain path",
			source:   `fetch("/api/rest/forms");`,
			wantPath: "/api/rest/forms",
		},
		{
			name:     "path parameter",
			source:   "fetch(`/api/rest/forms/${id}`);",
			wantPath: "/api/rest/forms/{param}",
		},
		{
			name:     "query string is stripped, not a reason to reject",
			source:   `fetch("/api/rest/media?limit=100");`,
			wantPath: "/api/rest/media",
		},
		{
			name:     "percent-encoded value stays usable",
			source:   `fetch("/api/rest/media?url=https%3A%2F%2Fws.jomform.com%2Fx.svg");`,
			wantPath: "/api/rest/media",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lits := scanFrontendAPISource(tc.source)
			if len(lits) != 1 {
				t.Fatalf("scanFrontendAPISource(%q) found %d literals, want 1: %+v", tc.source, len(lits), lits)
			}
			got := lits[0]
			if got.Path != tc.wantPath {
				t.Errorf("path = %q, want %q (a cut at the first space would hide the mistake)", got.Path, tc.wantPath)
			}
			if tc.wantReason == "" {
				if got.Reason != "" {
					t.Errorf("literal %q was rejected: %s", got.Literal, got.Reason)
				}
				return
			}
			if got.Reason == "" {
				t.Fatalf("literal %q was accepted as a path, want it reported: httptest.NewRequest panics on it", got.Literal)
			}
			if !strings.Contains(got.Reason, tc.wantReason) {
				t.Errorf("reason %q does not mention %q", got.Reason, tc.wantReason)
			}
		})
	}
}

// TestRouteExistsReportsNonPathTargetsWithoutPanicking pins the fix at the call
// site: the strings that used to panic must come back as an error, because a
// panic in this binary discards every other verdict in the package's run.
func TestRouteExistsReportsNonPathTargetsWithoutPanicking(t *testing.T) {
	mux := buildParityMux()

	for _, target := range []string{
		"/v1/cf/status resolves before the first fetch",
		"/v1/x y",
		"/v1/x	y",
		"/v1/x\ny",
	} {
		exists, _, err := routeExists(mux, target)
		if err == nil {
			t.Errorf("routeExists(%q) = (exists=%v, err=nil); want an error", target, exists)
			continue
		}
		if !strings.Contains(err.Error(), "not a valid HTTP request target") {
			t.Errorf("routeExists(%q) error = %q, want it to name the invalid target", target, err)
		}
		if exists {
			t.Errorf("routeExists(%q) reported that the route exists", target)
		}
	}

	// Negative control: a real path must still be probed, so the guard has not
	// been turned into an always-error by this change.
	if exists, status, err := routeExists(mux, "/v1/workspaces"); err != nil || !exists {
		t.Fatalf("/v1/workspaces should resolve: exists=%v status=%d err=%v", exists, status, err)
	}
}

// TestRouteParityReportsMalformedLiteralsFromTheTreeWalk pins the reporting half
// of the fix on the walk itself, not only on scanFrontendAPISource: a literal
// that is not a request path must come back in the malformed list with the file
// that carries it, while usable literals beside it still come back as paths.
//
// Without this, deleting the reporting branch would leave the suite green while
// the guard silently skipped the frontend mistake — the "passes vacuously" shape
// this card exists to remove, because a malformed literal is exactly what used
// to crash the binary instead of being reported.
func TestRouteParityReportsMalformedLiteralsFromTheTreeWalk(t *testing.T) {
	web := t.TempDir()
	lib := filepath.Join(web, "src", "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", lib, err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(lib, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("good.ts", "fetch(\"/api/rest/forms\");\nconst one = `/api/rest/forms/${id}`;\n")
	write("prose.ts", "const msg = `/api/cf/status resolves before the first fetch`;\n")
	write("space.ts", `const p = "/api/x y";`)
	// Not TypeScript: the walk must not read it.
	write("notes.md", `"/api/not source y"`)

	paths, malformed := extractFrontendAPIPathsFrom(t, filepath.Join(web, "src"))

	if got := paths["/api/rest/forms"]; len(got) != 1 || got[0] != "src/lib/good.ts" {
		t.Errorf("paths[/api/rest/forms] = %v, want [src/lib/good.ts]", got)
	}
	if got := paths["/api/rest/forms/{param}"]; len(got) != 1 || got[0] != "src/lib/good.ts" {
		t.Errorf("paths[/api/rest/forms/{param}] = %v, want [src/lib/good.ts]", got)
	}
	if _, ok := paths["/api/not source y"]; ok {
		t.Errorf("a .md file was walked: %v", paths)
	}

	want := map[string]string{
		"/api/cf/status resolves before the first fetch": "src/lib/prose.ts",
		"/api/x y": "src/lib/space.ts",
	}
	if len(malformed) != len(want) {
		t.Fatalf("malformed = %+v, want %d entries (%v) — a dropped literal would make the guard pass over a frontend mistake", malformed, len(want), want)
	}
	for _, m := range malformed {
		file, ok := want[m.Literal]
		if !ok {
			t.Errorf("unexpected malformed literal %q (path %q)", m.Literal, m.Path)
			continue
		}
		if len(m.Files) != 1 || m.Files[0] != file {
			t.Errorf("malformed %q carries files %v, want [%s]", m.Literal, m.Files, file)
		}
		if m.Reason == "" {
			t.Errorf("malformed %q carries no reason", m.Literal)
		}
	}
}
