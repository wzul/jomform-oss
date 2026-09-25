// Spec ↔ code parity guard for the public REST API contract.
//
// WHY THIS EXISTS
//   The API had 108 working /v1/rest/* routes and no published contract. The
//   obvious next step — write the endpoints down — is the exact move that
//   produced this repo's docs-drift card (t_d768342e): a prose document that
//   describes code it is no longer attached to, and nothing goes red when they
//   diverge. So the contract lives in openapi_routes.go next to the route table,
//   and this test pins it to the code in BOTH directions:
//
//     1. Every /v1/rest/* registration in rest.go has a contract entry, and
//        every contract entry is a real registration. Add a route without its
//        contract → RED. Delete a route and leave the entry → RED.
//
//     2. The published OpenAPI document contains exactly the registered path set,
//        with exactly the registered methods. This is the assertion that keeps
//        the artifact (which is what a developer actually reads) from drifting
//        even if the registry stays honest.
//
//     3. Every access level is asserted against the gate the handler really
//        calls. "readonly is refused on writes" is a promise the docs make, so
//        the doc's claim is checked against requireWriteScope/requireWriteRole/
//        requireAdminRole rather than trusted. A route that becomes admin-only
//        stops matching its `write` entry → RED. And every gate a contracted
//        handler calls must itself be classified, because a gate missing from
//        the classifier does not fail — it reports the route as `read`, which is
//        how a route that refuses readonly keys was published as a read
//        (t_e9562a9c).
//
//     4. Every rate limit is asserted in BOTH directions, because the honest
//        fact about this API is that almost nothing is throttled and that is the
//        fact most likely to rot: a route documented as unthrottled that grows a
//        middleware.RateLimit wrap fails, and a route documented as throttled
//        whose limiter is removed fails.
//
//     5. The document is validated with a real OpenAPI 3.1 validator, so an
//        invalid spec cannot be published. It is validated from the JSON bytes
//        that ship, not from the Go map, so the thing under test is the artifact.
//
//     6. The two generated files on disk (api/openapi/openapi.json and
//        web/src/lib/openapiSpec.gen.ts) must equal a fresh generation of the
//        same source. A stale artifact fails here instead of being served.
//
// WHAT IT DOES NOT COVER (stated so the guard is not over-claimed)
//   * It is a structural check. It proves the spec NAMES every route, not that
//     each summary is well written, nor that a response schema matches every
//     branch a handler can take.
//   * The request/response schemas are hand-declared components; this test
//     proves they are syntactically valid and referenced, not that every field
//     the handler emits is listed.
//   * The curl examples on /docs/api are checked to RUN against a local server by
//     a separate test (openapi_docs_examples_test.go); a shell example that
//     happens to be wrong for a reason its assertions do not model would pass.
package httpapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// registeredRESTRoute is one mux registration parsed out of RegisterRoutes.
type registeredRESTRoute struct {
	Method  string
	Path    string
	Handler string
	// Throttled reports whether the registration wraps the handler in a
	// middleware rate limiter.
	Throttled bool
}

// restGoRoutes parses the real route table in rest.go. Parsing the source rather
// than reflecting over a mux at runtime is deliberate: it is the only way to
// learn each route's HANDLER name (needed to read that handler's own gate) and
// whether the registration itself carries a rate-limit middleware.
//
// A registration is a STATEMENT, not a line: the three upload routes are written
// across two lines (`mux.Handle("POST /v1/rest/uploads/image",` then the
// middleware/handler on the next). Reading line-by-line would have missed their
// rate-limit wrap — which is exactly the mis-documentation this guard exists to
// prevent — so continuation lines of a mux.Handle statement are joined first.
func restGoRoutes(t *testing.T) []registeredRESTRoute {
	t.Helper()
	src := readRootFile(t, "api/internal/httpapi/rest.go")

	var statements []string
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "mux.Handle") {
			statements = append(statements, line)
			continue
		}
		// A continuation of the registration we are collecting: the statement is
		// still open (no closing paren yet) and this line is code, not a comment.
		if n := len(statements); n > 0 && !balanced(statements[n-1]) &&
			line != "" && !strings.HasPrefix(line, "//") {
			statements[n-1] += " " + line
		}
	}

	var out []registeredRESTRoute
	for _, stmt := range statements {
		m := regexp.MustCompile(`^mux\.Handle(?:Func)?\("(GET|POST|PUT|PATCH|DELETE) (/v1/rest/[^"]+)"`).FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		handler := ""
		if hm := regexp.MustCompile(`h\.(\w+)\)`).FindStringSubmatch(stmt); hm != nil {
			handler = hm[1]
		}
		out = append(out, registeredRESTRoute{
			Method:    m[1],
			Path:      m[2],
			Handler:   handler,
			Throttled: strings.Contains(stmt, "middleware.RateLimit"),
		})
	}
	return out
}

// balanced reports whether a statement's parentheses are closed.
func balanced(stmt string) bool {
	depth := 0
	for _, r := range stmt {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth <= 0
}

// readRootFile reads a file by its REPO-ROOT-relative path (unlike the
// package's readRepoFile, which resolves against api/). The artifacts and the
// route table are named from the repo root everywhere else in this card, so the
// same string appears in the guard, the generator and the docs.
func readRootFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(rootDir, rel))
	if err != nil {
		t.Fatalf("cannot read %s: %v (the guard cannot pass vacuously)", rel, err)
	}
	return string(b)
}

// rootDir is the repository root, resolved from this test file's location
// (api/internal/httpapi → three levels up).
var rootDir = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("openapi guard: cannot locate its own source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}()

func routeKey(method, path string) string { return method + " " + path }

// TestOpenAPIEveryRESTRouteIsContracted is direction 1: code → contract.
func TestOpenAPIEveryRESTRouteIsContracted(t *testing.T) {
	routes := restGoRoutes(t)
	if len(routes) < 50 {
		t.Fatalf("parsed only %d /v1/rest routes from rest.go — the parse anchor moved, "+
			"so this guard would be checking almost nothing", len(routes))
	}
	contracted := map[string]restRouteDoc{}
	for _, d := range restRouteDocs {
		contracted[routeKey(d.Method, d.Path)] = d
	}
	var missing []string
	for _, r := range routes {
		if _, ok := contracted[routeKey(r.Method, r.Path)]; !ok {
			missing = append(missing, routeKey(r.Method, r.Path)+" → handler "+r.Handler)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these routes are registered in rest.go with no entry in restRouteDocs "+
			"(the public contract is missing them):\n  %s\n\nAdd the entry in openapi_routes.go "+
			"and run: go run ./cmd/openapi-gen", strings.Join(missing, "\n  "))
	}
}

// TestOpenAPIContractHasNoPhantomRoutes is direction 2: contract → code.
func TestOpenAPIContractHasNoPhantomRoutes(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range restGoRoutes(t) {
		registered[routeKey(r.Method, r.Path)] = true
	}
	var phantom []string
	seen := map[string]bool{}
	for _, d := range restRouteDocs {
		k := routeKey(d.Method, d.Path)
		if seen[k] {
			t.Errorf("%s is declared twice in restRouteDocs", k)
		}
		seen[k] = true
		if !registered[k] {
			phantom = append(phantom, k+" → handler "+d.Handler)
		}
	}
	if len(phantom) > 0 {
		sort.Strings(phantom)
		t.Errorf("the contract documents routes that rest.go does not register — a developer "+
			"would build against an endpoint that 404s:\n  %s", strings.Join(phantom, "\n  "))
	}
}

// TestOpenAPISpecMatchesTheRegisteredRouteSet pins the published document to the
// route table, so the artifact cannot drift even if the registry stays honest.
func TestOpenAPISpecMatchesTheRegisteredRouteSet(t *testing.T) {
	doc := OpenAPISpec()
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("the generated document has no paths object")
	}
	inSpec := map[string]bool{}
	for path, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("path %s is not an operation map", path)
		}
		for method := range ops {
			inSpec[routeKey(strings.ToUpper(method), path)] = true
		}
	}
	registered := map[string]bool{}
	for _, r := range restGoRoutes(t) {
		registered[routeKey(r.Method, r.Path)] = true
	}
	var onlyCode, onlySpec []string
	for k := range registered {
		if !inSpec[k] {
			onlyCode = append(onlyCode, k)
		}
	}
	for k := range inSpec {
		if !registered[k] {
			onlySpec = append(onlySpec, k)
		}
	}
	sort.Strings(onlyCode)
	sort.Strings(onlySpec)
	if len(onlyCode) > 0 {
		t.Errorf("registered but absent from the published spec:\n  %s", strings.Join(onlyCode, "\n  "))
	}
	if len(onlySpec) > 0 {
		t.Errorf("in the published spec but not registered:\n  %s", strings.Join(onlySpec, "\n  "))
	}
}

// handlerBody returns the source of one RESTHandlers method.
//
// The file is located by walking this package's own directory rather than from a
// hardcoded list: a new rest_*.go file (which is how every handler group here
// arrives) must not be able to make this guard fail with "the file list is
// stale", and a guard that enumerates files by hand is the rot it is meant to
// catch.
func handlerBody(t *testing.T, handler string) string {
	t.Helper()
	if handler == "" {
		t.Fatal("a route has no handler name — the registration shape changed")
	}
	entries, err := os.ReadDir(rootDir + "/api/internal/httpapi")
	if err != nil {
		t.Fatalf("cannot list the httpapi package: %v", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) < 40 {
		t.Fatalf("only %d package files found — the walk is broken, so this guard would "+
			"fail every handler", len(files))
	}
	marker := "func (h *RESTHandlers) " + handler + "("
	for _, f := range files {
		src := readRootFile(t, "api/internal/httpapi/"+f)
		i := strings.Index(src, marker)
		if i == -1 {
			continue
		}
		rest := src[i:]
		if j := strings.Index(rest, "\nfunc "); j != -1 {
			rest = rest[:j]
		}
		return rest
	}
	t.Fatalf("handler %s not found in any of the %d package files — the handler is wired in "+
		"rest.go but does not exist", handler, len(files))
	return ""
}

// TestOpenAPIAccessMatchesTheHandlerGate asserts the documented access level
// against the gate the handler actually applies. This is the claim the docs page
// repeats ("a readonly key is refused on writes with 403"), so it is checked,
// not trusted.
//
// The classification is by NAME, one case per named gate, in order of strength:
// every gate this package uses to refuse a caller must appear here, because a
// gate that is missing from the switch does not fail — it silently falls through
// to `read`, which is how GET /v1/rest/settings/webhook-key was published as a
// read while its handler called requireWriteScope and answered a readonly key
// with 403 (card t_e9562a9c). requireWriteScope was already named in this file's
// header comment as one of the checked gates; it was not in the switch, so the
// guard accepted the false label and rejected the true one.
func TestOpenAPIAccessMatchesTheHandlerGate(t *testing.T) {
	for _, d := range sortedRouteDocs() {
		body := handlerBody(t, d.Handler)
		actual := classifyHandlerAccess(t, d.Handler, body)
		if actual != d.Access {
			t.Errorf("%s %s (%s): documented access %q but the handler's gate is %q.\n"+
				"Fix whichever is wrong — the docs must not promise a weaker or stronger gate "+
				"than the code enforces.", d.Method, d.Path, d.Handler, d.Access, actual)
		}
	}
}

// classifyHandlerAccess returns the access level a handler's own source applies,
// by looking for the named gate it calls. It is a function (not an inline
// switch) so TestHandlerAccessClassifierKnowsEveryNamedGate can assert that
// every gate a contracted handler calls is classified — a new gate added to
// rest_scope.go must not be able to arrive as an unlabelled `read` default.
func classifyHandlerAccess(t *testing.T, handler, body string) string {
	t.Helper()
	switch {
	case strings.Contains(body, "requireAdminRole("):
		return AccessAdmin
	case strings.Contains(body, "requireWriteRole("):
		return AccessWrite
	case strings.Contains(body, "requireWriteScope("):
		// The API-key scope gate on its own: a readonly key is refused with
		// 403 even though the handler only reads. It is not a full write (no
		// ?workspace= role axis), but it is the strongest gate the handler
		// applies, and `read` would promise the page's "any authenticated key
		// (readonly or readwrite)" — which is exactly the false claim this
		// case exists to stop.
		return AccessWrite
	case refusesAPIKey(t, handler, body):
		// A route that acts on a PERSON refuses an API key outright: its
		// handler demands a session user and answers 401 without one.
		return AccessSession
	default:
		return AccessRead
	}
}

// TestHandlerAccessClassifierKnowsEveryNamedGate is the direction that keeps the
// classifier honest, and it is scoped to the handlers this contract actually
// describes: for every contracted route, every `require*` gate its handler calls
// must be one classifyHandlerAccess recognises, and a handler must not match two
// of them at once.
//
// WHY it is needed: an unrecognised gate does not fail the parity test — the
// route is silently reported as `read`, which is exactly how a route that answers
// a readonly key with 403 came to be published as a read (card t_e9562a9c).
// Without this, adding a fifth named write gate to rest_scope.go would put every
// route using it back on that default, and nothing would go red.
//
// The scope stops at this contract on purpose. The package also has gates for the
// older /v1/* surface (requireWorkspaceAdmin gates /v1/workspaces/*,
// requireSuperAdmin gates /v1/admin/*); those routes are not in the published
// contract, so they have no access level to be right or wrong about here. Naming
// them instead of measuring them would be the restatement-of-the-thing shape this
// file already avoids.
func TestHandlerAccessClassifierKnowsEveryNamedGate(t *testing.T) {
	classified := []string{"requireAdminRole", "requireWriteRole", "requireWriteScope"}
	gateRe := regexp.MustCompile(`\b(require[A-Za-z]+)\(`)

	var unclassified []string
	ambiguous := []string{}
	checked := 0
	for _, d := range sortedRouteDocs() {
		body := handlerBody(t, d.Handler)
		called := map[string]bool{}
		for _, m := range gateRe.FindAllStringSubmatch(body, -1) {
			called[m[1]] = true
		}
		matched := []string{}
		for _, name := range classified {
			if called[name] {
				matched = append(matched, name)
			}
		}
		if len(matched) > 1 {
			// The switch is ordered by strength, so a handler calling two gates
			// is classified by the first — which may not be its strongest.
			ambiguous = append(ambiguous, fmt.Sprintf("%s %s (%s) calls %v",
				d.Method, d.Path, d.Handler, matched))
			continue
		}
		if len(matched) == 1 {
			checked++
		}
		for name := range called {
			if !strings.Contains(strings.Join(classified, " "), name) {
				unclassified = append(unclassified,
					fmt.Sprintf("%s %s (%s) → %s", d.Method, d.Path, d.Handler, name))
			}
		}
	}
	sort.Strings(unclassified)
	sort.Strings(ambiguous)
	if len(unclassified) > 0 {
		t.Errorf("these contracted handlers call a gate classifyHandlerAccess does not know:\n  %s\n"+
			"An unclassified gate does not fail the parity test — the route is reported as `read`, "+
			"and a route that refuses a readonly key would be published as one that does not. "+
			"Add a case for each gate (with its access level) in openapi_parity_test.go.",
			strings.Join(unclassified, "\n  "))
	}
	if len(ambiguous) > 0 {
		t.Errorf("these handlers match more than one classified gate, so they are classified by "+
			"the switch's order rather than by their own strongest gate:\n  %s",
			strings.Join(ambiguous, "\n  "))
	}
	// A run that found no gate at all would make every loop above vacuous.
	if checked < 50 {
		t.Errorf("only %d of %d contracted handlers named a classified gate; the parse anchor "+
			"moved, so this guard would pass for the wrong reason",
			checked, len(sortedRouteDocs()))
	}
	t.Logf("checked %d contracted handlers across %d routes; %d classified gates in use",
		checked, len(sortedRouteDocs()), len(classified))
}


// refusesAPIKey reports whether a handler turns away a request with no session
// user. These routes are marked AccessSession in the registry, so the claim is
// the one thing that must be true for them.
//
// The check is deliberately narrow — the handler must mention a session user AND
// a 401 — because the alternative (guessing from the handler's name) is how a
// guard becomes a restatement of the thing it is checking.
func refusesAPIKey(t *testing.T, handler, body string) bool {
	t.Helper()
	mentionsSession := strings.Contains(body, "sessionOwnerID(r)")
	mentions401 := strings.Contains(body, "StatusUnauthorized")
	return mentionsSession && mentions401
}

// TestOpenAPIRateLimitsMatchTheCode asserts documented throttling in both
// directions: a route documented as unthrottled must have no limiter, and one
// documented as throttled must. Nothing else in the repo checks that the API's
// own statement about its rate limits is true.
func TestOpenAPIRateLimitsMatchTheCode(t *testing.T) {
	registered := map[string]registeredRESTRoute{}
	for _, r := range restGoRoutes(t) {
		registered[routeKey(r.Method, r.Path)] = r
	}
	for _, d := range sortedRouteDocs() {
		r, ok := registered[routeKey(d.Method, d.Path)]
		if !ok {
			continue // the phantom-route test owns that failure
		}
		body := handlerBody(t, d.Handler)
		switch d.RateSource {
		case "":
			if r.Throttled {
				t.Errorf("%s %s is wrapped in a rate-limit middleware but the contract documents "+
					"no limit — a caller would not know they can be throttled", d.Method, d.Path)
			}
			if strings.Contains(body, "Limiter != nil && !h.") {
				t.Errorf("%s %s enforces a limiter inside the handler but the contract documents no "+
					"limit", d.Method, d.Path)
			}
		case RateMiddleware:
			if !r.Throttled {
				t.Errorf("%s %s is documented as per-IP rate limited but its registration carries no "+
					"middleware.RateLimit wrap", d.Method, d.Path)
			}
		case RateHandler:
			if !strings.Contains(body, "Limiter != nil && !h.") {
				t.Errorf("%s %s is documented as rate limited by the handler, but the handler has no "+
					"limiter call", d.Method, d.Path)
			}
		default:
			t.Errorf("%s %s has an unknown RateSource %q", d.Method, d.Path, d.RateSource)
		}
	}
}

// TestOpenAPISpecIsValidFor31 validates the artifact that ships, with a real
// OpenAPI validator, so an invalid spec fails before it is published.
func TestOpenAPISpecIsValidFor31(t *testing.T) {
	b, err := OpenAPISpecJSON()
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(b)
	if err != nil {
		t.Fatalf("the published spec does not parse: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("spec declares openapi %q, want 3.1.0", doc.OpenAPI)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("the published spec FAILS OpenAPI 3.1 validation: %v", err)
	}
	// A validated-but-empty document would pass the call above, so assert the
	// content the pages promise is really there.
	if n := len(doc.Paths.Map()); n < 50 {
		t.Errorf("the validated spec has only %d paths — it is not the API", n)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Error("the spec declares no component schemas — the response bodies are undocumented")
	}
}

// generatedArtifactPaths is the generator's own list, so a new artifact cannot be
// generated-but-unchecked (or checked-but-never-written).
func generatedArtifactPaths() []string { return GeneratedArtifactPaths() }

// generatedArtifact renders the expected content of one published artifact via
// the package's own generator, so the check is against the single definition.
func generatedArtifact(t *testing.T, rel string) string {
	t.Helper()
	got := GeneratedArtifact(rel)
	if got == "" {
		t.Fatalf("no generator is registered for %s (the artifact list drifted)", rel)
	}
	return got
}

// TestGeneratedArtifactsAreInSync re-runs the generator in memory and compares
// both published files. A stale artifact (the common failure: someone edits the
// registry and forgets to regenerate) fails here rather than being served.
//
// web/public/openapi.json is in the list because it is the copy a visitor can
// actually fetch. The repository is private, so a link to api/openapi/
// openapi.json on github.com 404s for the third-party developer the /docs/api
// page is written for; the same bytes at https://jomform.com/openapi.json are
// served by the web app and need no account. Both copies come from one
// OpenAPISpecJSON() call, and each is compared against a fresh generation here,
// so neither can go stale on its own.
func TestGeneratedArtifactsAreInSync(t *testing.T) {
	// Every generated artifact. Missing one here would let that file rot while
	// this guard still reported green — so the list is asserted against the
	// generator's own list, not maintained by hand.
	want := []string{
		"api/openapi/openapi.json",
		"web/public/openapi.json",
		"web/src/lib/apiExamples.gen.ts",
		"web/src/lib/openapiSpec.gen.ts",
	}
	sort.Strings(want)
	if got := generatedArtifactPaths(); !equalStrings(got, want) {
		t.Errorf("the generator writes %v but this guard checks %v — an artifact is unguarded",
			generatedArtifactPaths(), want)
	}
	checked := 0
	for _, rel := range want {
		onDisk := readRootFile(t, rel)
		expected := generatedArtifact(t, rel)
		if onDisk != expected {
			t.Errorf("%s is STALE — it does not match a fresh generation of openapi_routes.go.\n"+
				"Run: cd api && go run ./cmd/openapi-gen", rel)
		}
		checked++
	}
	if checked != len(want) {
		t.Errorf("checked %d of %d artifacts", checked, len(want))
	}
	// The two JSON copies exist so that the same document is reachable with and
	// without repository access, so "they are identical" is the property that
	// makes the public one a substitute rather than a second source of truth.
	// Asserted directly as well as through the generator, because a future
	// refactor could give them separate renderers and each would still pass the
	// freshness check above while the two readers saw different contracts.
	if a, b := generatedArtifact(t, "api/openapi/openapi.json"),
		generatedArtifact(t, "web/public/openapi.json"); a != b {
		t.Errorf("the repository copy and the publicly served copy of the contract "+
			"differ (%d vs %d bytes) — a reader would get a different document depending "+
			"on whether they can open github.com", len(a), len(b))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
