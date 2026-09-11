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
	paths := extractFrontendAPIPaths(t)
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
		checked++
		exists, _ := routeExists(mux, backendPath)
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
	if exists, _ := routeExists(mux, "/v1/rest/spaces"); exists {
		t.Errorf("guard is not sensitive to renames: /v1/rest/spaces still resolves, " +
			"so an orphaned frontend path would go undetected")
	}
	if exists, status := routeExists(mux, "/v1/workspaces"); !exists {
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
func routeExists(mux *http.ServeMux, path string) (exists bool, status int) {
	req := httptest.NewRequest(http.MethodGet, path, nil)

	if _, pattern := mux.Handler(req); pattern != "" {
		return true, http.StatusOK
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
	return status != http.StatusNotFound, status
}

// extractFrontendAPIPaths walks the Next.js source tree and returns every
// literal /api/* path it finds, normalised, mapped to the files that call it.
func extractFrontendAPIPaths(t *testing.T) map[string][]string {
	t.Helper()

	root, err := filepath.Abs(frontendSrcDir)
	if err != nil {
		t.Fatalf("resolve %s: %v", frontendSrcDir, err)
	}

	paths := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		body := string(raw)
		rel, relErr := filepath.Rel(filepath.Dir(root), path)
		if relErr != nil {
			rel = path
		}
		for _, re := range []*regexp.Regexp{apiDoubleQuoted, apiTemplateLiteral} {
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				norm := normaliseFrontendAPIPath(m[1])
				if norm == "" || norm == "/api" {
					continue
				}
				paths[norm] = append(paths[norm], filepath.ToSlash(rel))
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
	return paths
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
