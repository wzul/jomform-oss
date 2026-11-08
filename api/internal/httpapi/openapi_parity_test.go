// Spec ↔ code parity guard for the public REST API contract.
//
// WHY THIS EXISTS
//
//	The API had 108 working /v1/rest/* routes and no published contract. The
//	obvious next step — write the endpoints down — is the exact move that
//	produced this repo's docs-drift card (t_d768342e): a prose document that
//	describes code it is no longer attached to, and nothing goes red when they
//	diverge. So the contract lives in openapi_routes.go next to the route table,
//	and this test pins it to the code in BOTH directions:
//
//	  1. Every /v1/rest/* registration in rest.go has a contract entry, and
//	     every contract entry is a real registration. Add a route without its
//	     contract → RED. Delete a route and leave the entry → RED.
//
//	  2. The published OpenAPI document contains exactly the registered path set,
//	     with exactly the registered methods. This is the assertion that keeps
//	     the artifact (which is what a developer actually reads) from drifting
//	     even if the registry stays honest.
//
//	  3. Every access level is asserted against the gate the handler really
//	     calls. "readonly is refused on writes" is a promise the docs make, so
//	     the doc's claim is checked against requireWriteScope/requireWriteRole/
//	     requireAdminRole rather than trusted. A route that becomes admin-only
//	     stops matching its `write` entry → RED. And every gate a contracted
//	     handler REACHES must itself be classified, because a gate missing from
//	     the classifier does not fail — it reports the route as `read`, which is
//	     how a route that refuses readonly keys was published as a read
//	     (t_e9562a9c). Those gates are found by PROPERTY (a helper that writes a
//	     403 and reaches the API-key scope axis — actx.KeyScopes), not by their
//	     spelling: the earlier name match saw only gates called `require*`, so
//	     the same refusal under any other name slid back onto the silent default
//	     (t_e37418be). A contracted handler that writes that refusal in its OWN
//	     body is classified by the same property — the handler that IS the gate
//	     is answered for rather than assumed to be `read` (t_b7e1b690) — and so
//	     is one that HANDS THE AXIS to a third function which writes the 403,
//	     because a readonly key is refused either way (t_52c77834).
//
//	  4. Every rate limit is asserted in BOTH directions, because the honest
//	     fact about this API is that almost nothing is throttled and that is the
//	     fact most likely to rot: a route documented as unthrottled that grows a
//	     middleware.RateLimit wrap fails, and a route documented as throttled
//	     whose limiter is removed fails.
//
//	  5. The document is validated with a real OpenAPI 3.1 validator, so an
//	     invalid spec cannot be published. It is validated from the JSON bytes
//	     that ship, not from the Go map, so the thing under test is the artifact.
//
//	  6. The two generated files on disk (api/openapi/openapi.json and
//	     web/src/lib/openapiSpec.gen.ts) must equal a fresh generation of the
//	     same source. A stale artifact fails here instead of being served.
//
// WHAT IT DOES NOT COVER (stated so the guard is not over-claimed)
//   - It is a structural check. It proves the spec NAMES every route, not that
//     each summary is well written, nor that a response schema matches every
//     branch a handler can take.
//   - The request/response schemas are hand-declared components; this test
//     proves they are syntactically valid and referenced, not that every field
//     the handler emits is listed.
//   - The 403 WRITE is followed through the AXIS, not through arbitrary data
//     flow. A refusal is found when the function that writes the 403 either reads
//     actx.KeyScopes itself or is HANDED it at the call site — actx.KeyScopes, a
//     local derived from it, or the caller's own parameter that was handed it
//     (t_52c77834). What is not followed is a scope decision that leaves that
//     chain before the 403 is written: a boolean stored on the handler, returned
//     by middleware, or carried in a struct field and tested by a third function
//     would look like a refusal off the axis, and a route shaped that way would
//     still reach the silent `read` default. Both closed shapes are pinned in the
//     classifier fixture (inline and delegated), so dropping either check goes
//     RED even though no live handler has that shape.
//   - The curl examples on /docs/api are checked to RUN against a local server by
//     a separate test (openapi_docs_examples_test.go); a shell example that
//     happens to be wrong for a reason its assertions do not model would pass.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/wzul/jomform/api/internal/httpapi/middleware"
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
// The classification is data (classifierGates, ordered by strength), applied to
// the gate the handler names — or, when it names none, to the gate it REACHES
// through a helper. Every gate this package uses to refuse a caller must appear
// there, because a gate that is missing does not fail — it silently falls through
// to `read`, which is how GET /v1/rest/settings/webhook-key was published as a
// read while its handler called requireWriteScope and answered a readonly key
// with 403 (card t_e9562a9c). requireWriteScope was already named in this file's
// header comment as one of the checked gates; it was not in the switch, so the
// guard accepted the false label and rejected the true one.
//
// The completeness of the table is the other test's job
// (TestHandlerAccessClassifierKnowsEveryNamedGate); this one only applies it.
func TestOpenAPIAccessMatchesTheHandlerGate(t *testing.T) {
	p := parseRESTPackage(t)
	for _, d := range sortedRouteDocs() {
		body := handlerBody(t, d.Handler)
		actual := classifyHandlerAccess(t, p, d.Handler, body)
		if actual != d.Access {
			t.Errorf("%s %s (%s): documented access %q but the handler's gate is %q.\n"+
				"Fix whichever is wrong — the docs must not promise a weaker or stronger gate "+
				"than the code enforces.", d.Method, d.Path, d.Handler, d.Access, actual)
		}
	}
}

// classifierGate is one entry of the classifier's data: a gate this package
// applies and the access level a route that calls it must be published with.
type classifierGate struct {
	Name   string // the call a handler makes, e.g. h.requireWriteRole(…)
	Access string
	Why    string
}

// classifierGates is the ONE place the gate→access mapping lives, declared as
// data rather than as the cases of a switch so the completeness guard can be
// exhaustive over it without re-listing the names — a second, hand-kept list is
// exactly the drift this file exists to catch.
//
// ORDER IS MEANINGFUL: strongest first, and classifyHandlerAccess returns the
// first match, because a handler that calls two gates must be published as the
// strongest one it applies (a handler matching two is reported separately).
var classifierGates = []classifierGate{
	{
		Name:   "requireAdminRole",
		Access: AccessAdmin,
		Why:    "the write gate plus the admin role in the ?workspace= scope",
	},
	{
		Name:   "requireWriteRole",
		Access: AccessWrite,
		Why:    "the write gate plus the manage-or-admin role in the ?workspace= scope",
	},
	{
		Name:   "requireWriteScope",
		Access: AccessWrite,
		Why: "the API-key scope gate on its own: a readonly key is refused with 403 even " +
			"though the handler only reads. It is not a full write (no ?workspace= role " +
			"axis), but it is the strongest gate the handler applies, and `read` would " +
			"promise the page's \"any authenticated key (readonly or readwrite)\" — which " +
			"is exactly the false claim this entry exists to stop.",
	},
	{
		// Card t_d07c8fbb. The write gate PLUS a live workspace for the account:
		// the three REST doors that mint a form row (createForm and both template
		// clones) call this one gate, because a form minted for an account with
		// no workspace is stored with workspace_id NULL and is unreachable once
		// published. It is a `write` route and no weaker: the write role is the
		// first thing it applies, so a readonly key is still refused with 403.
		// It sits last because it is only ever the sole gate of a handler that
		// also writes — a handler that names it alongside requireWriteRole would
		// be ambiguous to this table and is reported separately.
		Name:   "requireFormMint",
		Access: AccessWrite,
		Why: "the write gate plus a live workspace for the account — a form minted " +
			"without one is stored with workspace_id NULL and 404 at every address " +
			"once published",
	},
}

// classifyHandlerAccess returns the access level a handler's own source applies,
// by looking for the gate it calls. It is a function (not an inline switch) so
// TestHandlerAccessClassifierKnowsEveryNamedGate can assert that every gate a
// contracted handler reaches is classified — a new gate added to rest_scope.go
// must not be able to arrive as an unlabelled `read` default.
//
// The gates themselves are data (classifierGates); this loop only applies them in
// order of strength. The fallback is deliberately `read`, and it is safe only
// because TestHandlerAccessClassifierKnowsEveryNamedGate fails the build when a
// contracted handler reaches a refusal this table does not hold (t_e37418be).
func classifyHandlerAccess(t *testing.T, p *restPackage, handler, body string) string {
	t.Helper()
	for _, g := range classifierGates {
		if strings.Contains(body, g.Name+"(") {
			return g.Access
		}
	}
	// A gate the handler does not name but REACHES is still the gate the route
	// answers with: a handler that hands the request to a helper which refuses
	// has that helper's gate, and the published level must not promise a weaker
	// one. Checked second, so every route that names its gate directly — the set
	// the contract was written against — keeps exactly the answer it had.
	if p != nil {
		reached := p.reachable(handler)
		for _, g := range classifierGates {
			if reached[g.Name] {
				return g.Access
			}
		}
	}
	// A handler can BE the gate rather than call one, or hand the request to a
	// function that writes the 403 for it: a body that reaches a key-scope
	// refusal — the same one the discovery finds, so the two cannot disagree
	// about what a refusal is — carries that gate's access level.
	// Checked third — after the gates the handler names and the gates it reaches
	// by name — so a handler that also applies a stronger gate keeps that answer.
	//
	// (t_b7e1b690) The shape where the handler WRITES the refusal itself used to
	// fall through to the `read` default: the discovery excludes contracted
	// handler names from its set on purpose, because a handler is what the
	// contract LABELS; with that exclusion and no other net, a handler that WAS
	// the refusal was unsupported in both directions, and the published page
	// promised "any authenticated key" for a route that answered every readonly
	// key with 403.
	//
	// (t_52c77834) The DELEGATE shape used to fall through for a second reason:
	// the refusal is written by a third function the handler only CALLS, so
	// neither function holds both properties and a "both in the same body" test
	// cannot see it. keyScopeRefusalSet follows the axis along the call edge, and
	// this check reads the handler's REACHABLE set, so a handler that hands the
	// axis to a 403 writer is classified the same way as one that writes it.
	if p != nil {
		reached := p.reachable(handler)
		for name := range p.keyScopeRefusalSet(t) {
			if reached[name] {
				return requireWriteScopeAccess(t)
			}
		}
	}
	if refusesAPIKey(t, handler, body) {
		// A route that acts on a PERSON refuses an API key outright: its
		// handler demands a session user and answers 401 without one.
		return AccessSession
	}
	return AccessRead
}

// requireWriteScopeAccess is the access level classifierGates gives
// requireWriteScope — the gate an inline refusal is a copy of. It is read out of
// the table rather than written here so the inline check and the named-gate check
// cannot drift to different answers for the same refusal.
func requireWriteScopeAccess(t *testing.T) string {
	t.Helper()
	for _, g := range classifierGates {
		if g.Name == "requireWriteScope" {
			return g.Access
		}
	}
	t.Fatalf("classifierGates no longer holds requireWriteScope, so a key-scope refusal " +
		"written inline in a handler has no access level to be classified with — the table " +
		"lost the one gate that refuses a readonly key")
	return ""
}

// TestHandlerAccessClassifierKnowsEveryNamedGate is the direction that keeps the
// classifier honest, and it is scoped to the handlers this contract actually
// describes: for every contracted route, every gate its handler reaches must be
// one classifyHandlerAccess knows, and a handler must not match two of them at
// once.
//
// (The test's NAME is historical — t_e37418be kept it because the card and the
// guard it replaces are referred to by it. It is no longer about names; see HOW.)
//
// WHY it is needed: an unrecognised gate does not fail the parity test — the
// route is silently reported as `read`, which is exactly how a route that answers
// a readonly key with 403 came to be published as a read (card t_e9562a9c).
//
// HOW the gates are found — by PROPERTY, not by spelling (card t_e37418be). The
// version this replaced matched `require[A-Za-z]+` in the handler's source, so a
// refusal helper under any other name was invisible and the silent `read` default
// came straight back: a helper called enforceKeyWriteScope with requireWriteScope's
// own body, made a handler's only gate, kept every access guard green while the
// wire 403'd a readonly key AND minted it a keypair. The gate set now comes from
// apiKeyScopeRefusalGates (writes a 403 + reaches the API-key scope axis), and the
// `require*` name scan is kept only as a second, cheaper net for a gate that does
// not (yet) write a 403.
//
// Reachability, not just the direct call, is what the refusal check uses: a
// handler that hands the request to a local helper which then refuses is gated by
// that helper, and the route's label has to answer for it.
//
// The scope stops at this contract on purpose. The package also has gates for the
// older /v1/* surface (requireWorkspaceAdmin gates /v1/workspaces/*,
// requireSuperAdmin gates /v1/admin/*); those routes are not in the published
// contract, so they have no access level to be right or wrong about here. Naming
// them instead of measuring them would be the restatement-of-the-thing shape this
// file already avoids.
func TestHandlerAccessClassifierKnowsEveryNamedGate(t *testing.T) {
	p := parseRESTPackage(t)
	refusals := apiKeyScopeRefusalGates(t, p)
	known := map[string]bool{}
	for _, g := range classifierGates {
		known[g.Name] = true
	}
	namedRe := regexp.MustCompile(`^require[A-Za-z]+$`)

	var unclassified []string
	ambiguous := []string{}
	checked := 0
	for _, d := range sortedRouteDocs() {
		if len(p.funcs[d.Handler]) == 0 {
			t.Fatalf("contracted handler %s (%s %s) is not a function declared in the package — "+
				"this guard cannot check it", d.Handler, d.Method, d.Path)
		}
		direct := p.edges[d.Handler]
		// Ambiguity is measured on DIRECT calls: a handler that calls two
		// classified gates is classified by the table's order rather than by its
		// own strongest gate. (Gates delegate to each other, so the REACHABLE set
		// legitimately contains several — see the refusal check below.)
		var matched []string
		for name := range direct {
			if known[name] {
				matched = append(matched, name)
			}
		}
		sort.Strings(matched)
		if len(matched) > 1 {
			ambiguous = append(ambiguous, fmt.Sprintf("%s %s (%s) calls %v",
				d.Method, d.Path, d.Handler, matched))
			continue
		}
		if len(matched) == 1 {
			checked++
		}
		// (a) every refusal helper the route DEPENDS ON must be classified.
		for name := range p.reachable(d.Handler) {
			if !refusals[name] || known[name] {
				continue
			}
			unclassified = append(unclassified, fmt.Sprintf(
				"%s %s (%s) reaches refusal helper %s (%s)",
				d.Method, d.Path, d.Handler, name, p.file[name]))
		}
		// (b) the name scan, kept from the version this card replaced.
		for name := range direct {
			if namedRe.MatchString(name) && !known[name] {
				unclassified = append(unclassified,
					fmt.Sprintf("%s %s (%s) → %s", d.Method, d.Path, d.Handler, name))
			}
		}
	}
	sort.Strings(unclassified)
	sort.Strings(ambiguous)
	if len(unclassified) > 0 {
		t.Errorf("these contracted handlers depend on a refusal helper that is not among "+
			"the classified gates:\n  %s\nA key-scope refusal is answered by PROPERTY (as "+
			"`write`) rather than by this table, so the parity test is not blind to this "+
			"helper — but the table is the register of the gates the contract is labelled "+
			"against, and a refusal a contracted route reaches belongs in it. Add a case for "+
			"each gate (with its access level) to classifierGates in openapi_parity_test.go.",
			strings.Join(unclassified, "\n  "))
	}
	if len(ambiguous) > 0 {
		t.Errorf("these handlers match more than one classified gate, so they are classified by "+
			"the table's order rather than by their own strongest gate:\n  %s",
			strings.Join(ambiguous, "\n  "))
	}
	// A run that found no gate at all would make every loop above vacuous.
	if checked < 50 {
		t.Errorf("only %d of %d contracted handlers named a classified gate; the parse anchor "+
			"moved, so this guard would pass for the wrong reason",
			checked, len(sortedRouteDocs()))
	}
	t.Logf("checked %d contracted handlers across %d routes; %d refusal helpers discovered, "+
		"%d classified gates in use", checked, len(sortedRouteDocs()), len(refusals),
		len(classifierGates))
}

// classifierFixtureSource is a miniature package holding one function per shape
// the classifier has to answer for, so the answers can be pinned without
// depending on this package happening to contain the shape (t_b7e1b690).
//
// It is source rather than a table of expectations because the classifier reads
// source: the properties under test are "does this body write a 403" and "does
// this body reach actx.KeyScopes", and a hand-written expectation would still
// pass after a refactor that made the classifier stop reading them.
const classifierFixtureSource = `package httpapi

import "net/http"

func fixtureReadHandler(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func fixtureInlineScopeRefusal(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if actx != nil && actx.ViaAPIKey && !dbstore.ScopeHasWrite(actx.KeyScopes) {
		writeErr(w, http.StatusForbidden, "scope tidak mencukupi (perlu readwrite)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func fixtureDelegatedScopeTestRefusal(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if !fixtureKeyHasWriteScope(actx) {
		writeErr(w, http.StatusForbidden, "scope tidak mencukupi (perlu readwrite)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func fixtureKeyHasWriteScope(actx *AccountCtx) bool {
	return actx != nil && actx.ViaAPIKey && dbstore.ScopeHasWrite(actx.KeyScopes)
}

func fixtureRoleRefusalOffTheScopeAxis(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if actx == nil || actx.Role != "admin" {
		writeErr(w, http.StatusForbidden, "admin sahaja")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func fixtureScopeReportNoRefusal(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	writeJSON(w, http.StatusOK, map[string]any{"scopes": actx.KeyScopes})
}

// The DELEGATE shape (t_52c77834): the handler reads the axis, a THIRD function
// writes the 403, and neither function holds both properties. This is the shape
// that used to leave both access guards green.
func fixtureDelegateWritesThe403(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if actx != nil && !dbstore.ScopeHasWrite(actx.KeyScopes) {
		fixtureRefuseReadonlyKeyThirdParty(w, actx.KeyScopes)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func fixtureRefuseReadonlyKeyThirdParty(w http.ResponseWriter, scopes []string) {
	_ = scopes
	writeErr(w, http.StatusForbidden, "scope tidak mencukupi (perlu readwrite)")
}

// The same delegate shape one call further out: the axis is handed to a helper
// that hands it on, so the 403 writer is two edges from the handler and reaches
// the axis only through a PARAMETER. It has its OWN writer on purpose: sharing
// fixtureRefuseReadonlyKeyThirdParty would let the one-hop case's axis edge
// satisfy this one, and the parameter hop would go untested (measured — that
// collision made the "drop the parameter taint" mutation survive).
func fixtureHandsTheAxisOn(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if fixtureKeyHasWriteScope(actx) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	fixtureRefuseIfReadonlyKey(w, actx.KeyScopes)
}

func fixtureRefuseIfReadonlyKey(w http.ResponseWriter, scopes []string) {
	fixtureRefuseIfStillReadonly(w, scopes)
}

func fixtureRefuseIfStillReadonly(w http.ResponseWriter, scopes []string) {
	_ = scopes
	writeErr(w, http.StatusForbidden, "scope tidak mencukupi (perlu readwrite)")
}

// The NEGATIVE control for the rule above: this body reads the axis (its
// response reports the scopes) and reaches a 403 writer, but that writer never
// sees the axis — a role refusal is not a key-scope refusal, so the route is
// still documented as read and the property must not claim otherwise.
func fixtureReadsAxisAndRefusesOffTheAxis(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scopes := actx.KeyScopes
	if actx.KeyID == "" {
		fixtureRefuseNotAdmin(w, "admin sahaja")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scopes": scopes})
}

func fixtureRefuseNotAdmin(w http.ResponseWriter, reason string) {
	writeErr(w, http.StatusForbidden, reason)
}
`

// classifierFixturePackage writes classifierFixtureSource into a temp package and
// parses it with the same parser the real package goes through. Shared by the two
// classifier fixture tests: both read the SAME source, so a shape added for one
// cannot drift away from the other.
func classifierFixturePackage(t *testing.T) *restPackage {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"),
		[]byte(classifierFixtureSource), 0o644); err != nil {
		t.Fatalf("cannot write the classifier fixture: %v", err)
	}
	p := parsePackageDir(t, dir)
	if len(p.funcs) < 12 {
		t.Fatalf("parsed only %d fixture functions — the fixture or the parse anchor moved, "+
			"so every case below would be answered from an empty call graph", len(p.funcs))
	}
	return p
}

// TestClassifierSeesAKeyScopeRefusalWrittenInlineInAHandler pins the classifier's
// answer for the refusal written where the contract looks: in the contracted
// handler's own body (t_b7e1b690).
//
// WHY a fixture: no live handler is shaped this way today — the only reader of
// actx.KeyScopes in this package is requireWriteScope — so an assertion about the
// real source would pass whether or not the shape is visible, which is exactly
// how it stayed invisible while the wire 403'd every readonly key. Dropping the
// classifier's inline check, or the transitivity of the scope-axis property it
// uses, turns this red immediately.
func TestClassifierSeesAKeyScopeRefusalWrittenInlineInAHandler(t *testing.T) {
	p := classifierFixturePackage(t)
	cases := []struct {
		handler string
		want    string
		why     string
	}{
		{"fixtureReadHandler", AccessRead, "writes no refusal at all"},
		{"fixtureInlineScopeRefusal", AccessWrite,
			"writes the key-scope refusal in the contracted handler's OWN body"},
		{"fixtureDelegatedScopeTestRefusal", AccessWrite,
			"writes the 403 itself and reaches the scope axis through a helper"},
		{"fixtureRoleRefusalOffTheScopeAxis", AccessRead,
			"writes a 403 but never reaches the API-key scope axis"},
		{"fixtureScopeReportNoRefusal", AccessRead,
			"reads the scope axis but refuses nobody"},
	}
	for _, c := range cases {
		body := fixtureFuncBody(t, c.handler)
		if got := classifyHandlerAccess(t, p, c.handler, body); got != c.want {
			t.Errorf("classifyHandlerAccess(%s) = %q, want %q: it %s. A route published as "+
				"`read` while its handler answers a readonly key with 403 is the defect this "+
				"classification exists to catch — see the file header, item 3.",
				c.handler, got, c.want, c.why)
		}
	}
}

// TestClassifierSeesAKeyScopeRefusalWrittenByAThirdFunctionTheHandlerCalls pins
// the DELEGATE shape of the same refusal (t_52c77834), which the file header used
// to list as a limit: the contracted handler reads actx.KeyScopes, and a THIRD
// function it only CALLS writes the 403.
//
// WHY a fixture: no live handler is shaped this way — the only reader of
// actx.KeyScopes in the shipped package is requireWriteScope — so nothing in the
// real source would notice the check being dropped. Measured on the deployed bytes
// (v1.21.166, ed07307f) with this shape planted in getCurrencyRates for
// GET /v1/rest/currency/rates (documented `read`): BOTH access guards stayed GREEN
// while the route answered every readonly key with 403 scope.readonly. Each case
// below removes one half of the mechanism that now catches it:
//
//   - dropping the axis-handed edge (fedArgs) means the third function is not a
//     refusal, so the handler falls back to the silent `read` default;
//   - dropping the reachability of the refusal from the handler (or going back to
//     "both properties in the SAME body") does the same;
//   - the off-axis case fails if the rule is widened to "reaches any 403 writer",
//     which would relabel every role-refusing route as `write`.
func TestClassifierSeesAKeyScopeRefusalWrittenByAThirdFunctionTheHandlerCalls(t *testing.T) {
	p := classifierFixturePackage(t)
	cases := []struct {
		handler string
		want    string
		why     string
	}{
		{"fixtureDelegateWritesThe403", AccessWrite,
			"reads the axis and hands it to a third function that writes the 403 — neither function holds both properties, so the pair has to be recognised along the call"},
		{"fixtureHandsTheAxisOn", AccessWrite,
			"the 403 writer is two edges away and receives the axis only through a parameter"},
		{"fixtureRefuseReadonlyKeyThirdParty", AccessWrite,
			"writes the 403 without reading the axis, but its caller hands the axis to it"},
		{"fixtureReadsAxisAndRefusesOffTheAxis", AccessRead,
			"reads the axis and reaches a 403 writer that never sees it — a role refusal is not a key-scope refusal"},
	}
	for _, c := range cases {
		body := fixtureFuncBody(t, c.handler)
		if got := classifyHandlerAccess(t, p, c.handler, body); got != c.want {
			t.Errorf("classifyHandlerAccess(%s) = %q, want %q: it %s. A route published as "+
				"`read` while its handler answers a readonly key with 403 is the defect this "+
				"classification exists to catch — see the file header, item 3.",
				c.handler, got, c.want, c.why)
		}
	}
}

// fixtureFuncBody returns one fixture function's source, so the body the
// classifier reads is the fixture's own bytes rather than a second copy of them.
func fixtureFuncBody(t *testing.T, name string) string {
	t.Helper()
	marker := "func " + name + "("
	i := strings.Index(classifierFixtureSource, marker)
	if i == -1 {
		t.Fatalf("the fixture does not declare %s", name)
	}
	rest := classifierFixtureSource[i:]
	if j := strings.Index(rest, "\nfunc "); j != -1 {
		rest = rest[:j]
	}
	return rest
}

// ── the gate discovery: a gate is a property of a helper's body ──────────────
//
// WHY IT IS NOT A NAME MATCH (card t_e37418be). This guard used to look for gates
// spelled `require*`, so it was blind to the same refusal under any other name: a
// helper called enforceKeyWriteScope with requireWriteScope's own body, made a
// handler's only gate, left every access guard green while the wire refused a
// readonly key with 403 and minted one anyway — the t_e9562a9c defect, one rename
// away. The discovery is therefore driven by what a helper DOES.
//
// A GATE is a function in this package that
//
//	(1) writes http.StatusForbidden, and
//	(2) reads actx.KeyScopes — the readonly/readwrite API-key scope axis —
//	    directly, or through a function that does, OR is HANDED the axis at a
//	    call site whose callee writes the 403.
//
// (2) is transitive on purpose: a gate that delegates its scope test to a helper
// still refuses a readonly key. The second half of (2) closes the shape the
// header used to list as a limit (t_52c77834): a refusal whose 403 is WRITTEN by
// a third function the gate only CALLS. There neither function holds both
// properties — the caller reads the axis, the callee writes the 403 — so the
// property has to follow the axis along the call edge, not just the call.
// requireResolvedWorkspaceScope is NOT one of these,
// and that is correct rather than an omission: it refuses a SESSION caller whose
// ?workspace= cannot be resolved and explicitly exempts API keys (actx.ViaAPIKey),
// so it never refuses a readonly key and a route whose strongest gate is it is
// honestly published as `read`.
//
// Excluded from the set: the contracted route handlers themselves. A handler is
// what the contract LABELS; it is not a gate the classifier has to hold, and
// counting it would report every handler that both delegates and writes its own
// 403 as an unclassifiable gate.

// restPackage is this package's own source, parsed into the call graph the gate
// discovery and the classifier both need. It reads the syntax tree, not the
// text: a name in a comment or a string is not a call, and whether a helper
// refuses a caller is a property of its body rather than of its spelling.
type restPackage struct {
	// funcs maps a function name to its declarations. A name can carry more than
	// one declaration (two receivers in one package), and the union is what the
	// call graph needs — a call to the name can reach either body.
	funcs map[string][]*ast.FuncDecl
	file  map[string]string          // name → the file it is declared in, for failures
	edges map[string]map[string]bool // name → package functions it calls directly
	sel   map[string]map[string]bool // name → the `x.Y` selectors appearing in its body
	// fedArgs maps a function to the ARGUMENT POSITIONS callers hand the API-key
	// scope axis at (actx.KeyScopes, a local derived from it, or the caller's own
	// parameter that was handed it). This is the edge the 403 WRITE is followed
	// along: a writer that never reads the axis still refuses a readonly key when
	// its caller passes the axis to it (t_52c77834).
	fedArgs map[string]map[int]bool
	// scopeReaders caches readsScopeAxis. Its fixpoint is asked for once per
	// contracted route by the classifier and once by the gate discovery, and it
	// depends only on the parsed source, so it is computed once per restPackage.
	scopeReaders map[string]bool
	// refusals caches keyScopeRefusalSet, for the same reason.
	refusals map[string]bool
}

// parseRESTPackage parses every non-test .go file of this package. Failing to
// parse is fatal: a guard that silently saw fewer functions would find fewer
// gates, which is the only direction that matters here.
func parseRESTPackage(t *testing.T) *restPackage {
	t.Helper()
	p := parsePackageDir(t, filepath.Join(rootDir, "api", "internal", "httpapi"))
	if len(p.funcs) < 500 {
		t.Fatalf("parsed only %d functions from the httpapi package — the parse anchor moved, "+
			"so the gate discovery would find nothing and this guard would pass for the wrong "+
			"reason", len(p.funcs))
	}
	return p
}

// parsePackageDir builds the same call graph from every non-test .go file in
// dir. The 500-function anchor stays in parseRESTPackage because it is about
// THIS package; this function is separate so the classifier can also be
// exercised against a FIXTURE package a test writes (see
// TestClassifierSeesAKeyScopeRefusalWrittenInlineInAHandler). The shapes that
// test pins are properties of source, so the honest way to pin them is to plant
// them in source this parser reads, not to restate the predicate next to it.
func parsePackageDir(t *testing.T, dir string) *restPackage {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("openapi access guard: cannot list %s: %v", dir, err)
	}
	p := &restPackage{
		funcs:   map[string][]*ast.FuncDecl{},
		file:    map[string]string{},
		edges:   map[string]map[string]bool{},
		sel:     map[string]map[string]bool{},
		fedArgs: map[string]map[int]bool{},
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("openapi access guard: cannot parse %s: %v", e.Name(), perr)
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil {
				continue
			}
			p.funcs[fd.Name.Name] = append(p.funcs[fd.Name.Name], fd)
			if _, seen := p.file[fd.Name.Name]; !seen {
				p.file[fd.Name.Name] = e.Name()
			}
		}
	}
	var bodies []*ast.FuncDecl
	for _, f := range parsed {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Body == nil {
				continue
			}
			bodies = append(bodies, fd)
		}
	}
	// The axis-handed edges are a FIXPOINT, not a single pass: a helper that is
	// handed actx.KeyScopes is itself carrying it, so a call it makes with that
	// parameter carries it too. Each pass re-walks every body with the parameters
	// known to be fed so far tainted, and stops when a pass learns nothing new.
	// The bound is belt-and-braces: every pass must add an edge or an argument
	// position, so the loop cannot run away — but a guard that hangs the gate is
	// worse than one that fails it.
	for pass := 0; ; pass++ {
		if pass > len(bodies)+1 {
			t.Fatalf("the axis fixpoint did not settle in %d passes — the walk keeps "+
				"finding new fed arguments, so this guard would be measuring its own "+
				"iteration rather than the source", len(bodies)+1)
		}
		changed := false
		for _, fd := range bodies {
			if p.record(fd) {
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return p
}

// record walks one function body and updates the call graph: the direct calls it
// makes, the one-level selectors in it, and — for each call — which argument
// positions carry the API-key scope axis. It reports whether it learned anything
// it did not already know, which is what the fixpoint in parsePackageDir
// iterates on.
//
// "Carries the axis" is deliberately syntactic and local: actx.KeyScopes itself,
// a local assigned from it (`scopes := actx.KeyScopes`), an alias of actx, or one
// of THIS function's own parameters at a position a caller was seen handing the
// axis to. That is enough to follow the value along the delegation chain without
// pretending to be a points-to analysis; what it does not follow is stated in the
// file header.
func (p *restPackage) record(fd *ast.FuncDecl) bool {
	self := fd.Name.Name
	if p.edges[self] == nil {
		p.edges[self] = map[string]bool{}
	}
	if p.sel[self] == nil {
		p.sel[self] = map[string]bool{}
	}
	tainted := p.paramTaint(fd)
	// Locals assigned from the axis (or from another tainted local) carry it:
	// `scopes := actx.KeyScopes; refuse(w, scopes)`.
	for changed := true; changed; {
		changed = false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range as.Rhs {
				if !exprCarriesAxis(rhs, tainted) {
					continue
				}
				lhs := as.Lhs[0]
				if i < len(as.Lhs) {
					lhs = as.Lhs[i]
				}
				if id, ok := lhs.(*ast.Ident); ok && !tainted[id.Name] {
					tainted[id.Name] = true
					changed = true
				}
			}
			return true
		})
	}
	added := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			callee := ""
			switch fn := x.Fun.(type) {
			case *ast.Ident:
				callee = fn.Name
			case *ast.SelectorExpr:
				// h.requireWriteScope(…) and the package-level helpers
				// (scopeOr403(…)) both arrive here.
				callee = fn.Sel.Name
			}
			if _, ok := p.funcs[callee]; !ok || callee == "" {
				return true
			}
			if !p.edges[self][callee] {
				p.edges[self][callee] = true
				added = true
			}
			for i, arg := range x.Args {
				if !exprCarriesAxis(arg, tainted) {
					continue
				}
				if p.fedArgs[callee] == nil {
					p.fedArgs[callee] = map[int]bool{}
				}
				if !p.fedArgs[callee][i] {
					p.fedArgs[callee][i] = true
					added = true
				}
			}
		case *ast.SelectorExpr:
			// Only one-level selectors are recorded, which is all the
			// discovery needs: http.StatusForbidden / actx.KeyScopes.
			if id, ok := x.X.(*ast.Ident); ok {
				p.sel[self][id.Name+"."+x.Sel.Name] = true
			}
		}
		return true
	})
	return added
}

// paramTaint returns the parameter names of fd whose POSITION a caller was seen
// handing the API-key scope axis to. Positional rather than "any parameter of a
// function that touched the axis", so a helper that receives the axis and calls
// an unrelated 403 writer with its other parameters is not mistaken for a
// key-scope refusal.
func (p *restPackage) paramTaint(fd *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	fed := p.fedArgs[fd.Name.Name]
	if len(fed) == 0 || fd.Type == nil || fd.Type.Params == nil {
		return out
	}
	pos := 0
	for _, field := range fd.Type.Params.List {
		n := len(field.Names)
		if n == 0 {
			n = 1 // an unnamed parameter still occupies a position
		}
		for k := 0; k < n; k++ {
			if !fed[pos+k] {
				continue
			}
			for _, id := range field.Names {
				out[id.Name] = true
			}
			break
		}
		pos += n
	}
	return out
}

// exprCarriesAxis reports whether an argument expression is the API-key scope
// axis, or a value derived from it: actx.KeyScopes, an alias of actx, or a local
// (or parameter) that was tainted.
func exprCarriesAxis(e ast.Expr, tainted map[string]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok &&
				(id.Name == "actx" || tainted[id.Name]) && x.Sel.Name == "KeyScopes" {
				found = true
			}
		case *ast.Ident:
			if tainted[x.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

// reachable returns every function in this package `start` can reach through its
// own calls, `start` itself included.
func (p *restPackage) reachable(start string) map[string]bool {
	out := map[string]bool{start: true}
	for changed := true; changed; {
		changed = false
		for from := range out {
			for to := range p.edges[from] {
				if !out[to] {
					out[to] = true
					changed = true
				}
			}
		}
	}
	return out
}

// readsScopeAxis returns every function in this package that reads
// actx.KeyScopes — the readonly/readwrite API-key scope axis — directly or
// through a call to a function that does.
//
// Transitive on purpose: a refusal that delegates the scope TEST to a helper
// (and writes its own 403) still refuses a readonly key, so the property that
// identifies a refusal has to follow the call. This is the property BOTH the
// gate discovery and the classifier's inline-refusal check are built on, so
// there is one definition of "reaches the scope axis" rather than two.
func (p *restPackage) readsScopeAxis(t *testing.T) map[string]bool {
	t.Helper()
	if p.scopeReaders != nil {
		return p.scopeReaders
	}
	reads := map[string]bool{}
	for name := range p.funcs {
		if p.sel[name]["actx.KeyScopes"] {
			reads[name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name := range p.funcs {
			if reads[name] {
				continue
			}
			for callee := range p.edges[name] {
				if reads[callee] {
					reads[name] = true
					changed = true
					break
				}
			}
		}
	}
	p.scopeReaders = reads
	return reads
}

// keyScopeRefusalSet returns every function in this package that answers a
// readonly API key with 403 — the definition BOTH the gate discovery and the
// classifier are built on, so the two cannot disagree about what a refusal is.
//
// A function qualifies when it WRITES the 403 and either
//
//	(a) reads actx.KeyScopes, directly or through a call (readsScopeAxis), or
//	(b) is HANDED the axis at a call site (fedArgs) — the delegate shape
//	    (t_52c77834): the caller reads the axis, a third function writes the
//	    403, and neither function holds both properties on its own.
//
// (b) is what keeps the property honest along the call edge. A 403 writer that
// never sees the axis is NOT a key-scope refusal: that is a role/validation
// refusal, and labelling a route `write` for it would be a false claim in the
// other direction (pinned by the classifier fixture's off-axis negative case).
func (p *restPackage) keyScopeRefusalSet(t *testing.T) map[string]bool {
	t.Helper()
	if p.refusals != nil {
		return p.refusals
	}
	readsScope := p.readsScopeAxis(t)
	out := map[string]bool{}
	for name := range p.funcs {
		if !p.sel[name]["http.StatusForbidden"] {
			continue
		}
		if readsScope[name] || len(p.fedArgs[name]) > 0 {
			out[name] = true
		}
	}
	p.refusals = out
	return out
}

// apiKeyScopeRefusalGates returns the names of this package's refusal helpers, by
// the body property described in the section comment above. The names are the
// classifier's INPUT — they are never used to decide what a gate does.
func apiKeyScopeRefusalGates(t *testing.T, p *restPackage) map[string]bool {
	t.Helper()
	// (2): who reads the scope axis, directly or by delegating. This is the
	// ANCHOR below as well as a liveness check on the parse.
	readsScope := p.readsScopeAxis(t)
	if len(readsScope) == 0 {
		t.Fatalf("no function in the httpapi package reads actx.KeyScopes — the discovery " +
			"anchor moved, so this guard would find no gate at all and pass vacuously")
	}
	gates := map[string]bool{}
	for name := range p.keyScopeRefusalSet(t) {
		gates[name] = true
	}
	// A contracted handler is what the contract labels, not a gate. Deleting it
	// here stops a handler that delegates to a gate AND writes its own 403 from
	// being reported as an unclassified gate — and it is no longer a blind spot
	// in the other direction: a handler that IS the refusal (the same 403 and
	// scope axis in its own body) is classified by classifyHandlerAccess's own
	// property check, not by a lookup in this set (t_b7e1b690).
	for _, d := range restRouteDocs {
		delete(gates, d.Handler)
	}
	// ANCHOR, not classification: the predicate above is "the helper that answers
	// a readonly key with 403". If the parse stopped finding requireWriteScope —
	// the one helper that reads actx.KeyScopes today — every answer below would be
	// a meaningless pass, so its absence is reported rather than tolerated. (The
	// same anchor shape the mcpsrv scope-gate guard uses for this gate.)
	if !gates["requireWriteScope"] {
		t.Errorf("the discovery did not find requireWriteScope, which writes a 403 and reads "+
			"actx.KeyScopes (%s) — the predicate is broken, not the code", p.file["requireWriteScope"])
	}
	if len(gates) == 0 {
		t.Fatalf("found no API-key scope refusal helper in the package — the guard would pass " +
			"vacuously")
	}
	return gates
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

// chatPolicyCase is one chat route whose documented rate sentence must quote the
// constants the handler's bucket is built from.
//
// The numbers are READ from those constants, never written here: a test that
// restated 60/10 would be a fifth copy of the cap, and it would keep passing
// while the code and the contract drifted away from it together.
type chatPolicyCase struct {
	Path    string
	Noun    string // the word between the number and "per hour" in the sentence
	PerHour float64
	Burst   float64
	// Consts are the package identifiers the sentence must be BUILT from.
	Consts []string
}

func chatPolicyCases() []chatPolicyCase {
	return []chatPolicyCase{
		{Path: "/v1/rest/chat", Noun: "messages",
			PerHour: chatRatePerHour, Burst: chatBurst,
			Consts: []string{"chatRatePerHour", "chatBurst"}},
		{Path: "/v1/rest/chat/confirm", Noun: "confirmations",
			PerHour: chatConfirmPerHour, Burst: chatConfirmBurst,
			Consts: []string{"chatConfirmPerHour", "chatConfirmBurst"}},
		{Path: "/v1/rest/chat/cancel", Noun: "confirmations",
			PerHour: chatConfirmPerHour, Burst: chatConfirmBurst,
			Consts: []string{"chatConfirmPerHour", "chatConfirmBurst"}},
	}
}

// specRateLimit / specOperation are the parts of a published document this guard
// reads: the extension an agent parses, and the description a human reads.
type specRateLimit struct {
	Scope  string `json:"scope"`
	Policy string `json:"policy"`
}

type specOperation struct {
	RateLimit   *specRateLimit `json:"x-jomform-rate-limit"`
	Description string         `json:"description"`
}

// parseSpecPaths unmarshals a document's paths, keyed by path and then by
// lowercase HTTP method. It reads the BYTES that ship, not the Go map they were
// built from, because the bytes are what a developer downloads.
func parseSpecPaths(t *testing.T, name, body string) map[string]map[string]specOperation {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]specOperation `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("%s does not parse as JSON: %v", name, err)
	}
	return doc.Paths
}

// TestChatRatePoliciesQuoteTheEnforcedCaps pins the contract's chat rate
// sentences to the constants the server enforces. The sentence a caller reads —
// x-jomform-rate-limit.policy, and the same sentence the generator appends to the
// operation's description — must state chatRatePerHour/chatBurst for /chat and
// chatConfirmPerHour/chatConfirmBurst for confirm + cancel.
//
// WHY IT IS NOT REDUNDANT WITH TestGeneratedArtifactsAreInSync. That test proves
// the artifacts match a fresh generation of openapi_routes.go, so a WRONGLY WIRED
// derivation — the confirmation sentence quoting the message constants —
// regenerates consistently and stays green there. This test is checked against
// the document the CODE produces (a fresh, wrongly wired generation is red) and
// against the JSON artifacts ON DISK (a stale one is red), which together are the
// failure this guard exists for: the constant moved and the published contract
// did not. web/src/lib/openapiSpec.gen.ts needs no branch of its own — the sync
// test compares it byte-for-byte with the in-memory generation read here first.
func TestChatRatePoliciesQuoteTheEnforcedCaps(t *testing.T) {
	sources := []struct{ name, body string }{
		{"the document the generator produces (in memory)", generatedArtifact(t, "api/openapi/openapi.json")},
		{"api/openapi/openapi.json", readRootFile(t, "api/openapi/openapi.json")},
		{"web/public/openapi.json", readRootFile(t, "web/public/openapi.json")},
	}
	for _, src := range sources {
		paths := parseSpecPaths(t, src.name, src.body)
		// ANCHOR, not courtesy: a document that parsed to nothing would make
		// every lookup below a miss dressed up as a pass.
		if len(paths) < 50 {
			t.Errorf("%s has %d paths — that is not the API, so the lookups below would "+
				"check nothing", src.name, len(paths))
			continue
		}
		for _, c := range chatPolicyCases() {
			op, ok := paths[c.Path]["post"]
			if !ok {
				t.Errorf("%s: no POST %s in the document — the anchor moved", src.name, c.Path)
				continue
			}
			if op.RateLimit == nil {
				t.Errorf("%s: POST %s carries no x-jomform-rate-limit, so the contract "+
					"promises a chat lane that is not throttled", src.name, c.Path)
				continue
			}
			re := regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?) ` + c.Noun +
				` per hour per user, burst ([0-9]+(?:\.[0-9]+)?)$`)
			m := re.FindStringSubmatch(op.RateLimit.Policy)
			if m == nil {
				t.Errorf("%s: POST %s documents the rate policy %q, which does not quote the "+
					"cap as \"<N> %s per hour per user, burst <N>\" — the sentence and this "+
					"guard have to agree on the shape", src.name, c.Path, op.RateLimit.Policy, c.Noun)
				continue
			}
			gotPerHour, errPerHour := strconv.ParseFloat(m[1], 64)
			gotBurst, errBurst := strconv.ParseFloat(m[2], 64)
			if errPerHour != nil || errBurst != nil {
				t.Errorf("%s: POST %s documents the rate policy %q, which is not two numbers "+
					"(%v / %v)", src.name, c.Path, op.RateLimit.Policy, errPerHour, errBurst)
				continue
			}
			if gotPerHour != c.PerHour || gotBurst != c.Burst {
				t.Errorf("%s: POST %s documents %g per hour, burst %g, but the handler enforces "+
					"%g per hour, burst %g — the published contract tells the caller a limit the "+
					"server does not apply.\n(The sentence is built from the constants; if you "+
					"changed one, run: cd api && go run ./cmd/openapi-gen)",
					src.name, c.Path, gotPerHour, gotBurst, c.PerHour, c.Burst)
			}
			// The same sentence is appended to the description, which is the copy a
			// human reads in the rendered reference.
			if !strings.Contains(op.Description, "Rate limit: "+op.RateLimit.Policy+".") {
				t.Errorf("%s: POST %s's description does not carry \"Rate limit: %s.\" — the "+
					"extension and the prose have diverged", src.name, c.Path, op.RateLimit.Policy)
			}
		}
	}
}

// registryRateFields returns, per documented path, the SYNTAX of that entry's
// Rate field in restRouteDocs — and the number of contract entries it walked, so
// the caller can tell a moved parse anchor from a registry that simply has few
// throttled routes (only seven do). The syntax is the point: whether a sentence
// is built from a constant or typed out is not visible in the value it evaluates
// to.
func registryRateFields(t *testing.T, file *ast.File) (map[string]ast.Expr, int) {
	t.Helper()
	out := map[string]ast.Expr{}
	entries := 0
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != "restRouteDocs" || i >= len(vs.Values) {
					continue
				}
				slice, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("restRouteDocs is no longer a composite literal — this guard cannot " +
						"read the registry")
				}
				for _, elt := range slice.Elts {
					entry, ok := elt.(*ast.CompositeLit)
					if !ok {
						continue
					}
					entries++
					var path string
					var rate ast.Expr
					for _, f := range entry.Elts {
						kv, ok := f.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						switch key.Name {
						case "Path":
							if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								path, _ = strconv.Unquote(lit.Value)
							}
						case "Rate":
							rate = kv.Value
						}
					}
					if path != "" && rate != nil {
						out[path] = rate
					}
				}
			}
		}
	}
	return out, entries
}

// TestChatRatePoliciesAreDerivedNotRestated is the other half of the guard, and
// the half a value comparison cannot express: a literal that happens to be the
// right number compares equal to the constant, so only the source can tell the
// two apart. The chat lane's Rate fields must be CALLS whose bodies name the
// constants that enforce that route's cap — the shape restRouteDocs uses today —
// because that is what makes the published contract move with the constant
// instead of drifting from it (t_da26169c).
func TestChatRatePoliciesAreDerivedNotRestated(t *testing.T) {
	pkg := parseRESTPackage(t)
	file, err := parser.ParseFile(token.NewFileSet(),
		filepath.Join(rootDir, "api", "internal", "httpapi", "openapi_routes.go"),
		nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("cannot parse openapi_routes.go: %v", err)
	}
	rates, entries := registryRateFields(t, file)
	if entries < 50 {
		t.Fatalf("walked only %d contract entries in restRouteDocs — the registry's shape moved, "+
			"so this guard would check nothing", entries)
	}
	for _, c := range chatPolicyCases() {
		expr, ok := rates[c.Path]
		if !ok {
			t.Errorf("no contract entry for POST %s — the anchor moved", c.Path)
			continue
		}
		if lit, isLit := expr.(*ast.BasicLit); isLit {
			t.Errorf("POST %s's Rate is the string literal %s — a literal is a copy of the cap "+
				"that no test can tell from the truth. Build it from %s instead.",
				c.Path, lit.Value, strings.Join(c.Consts, " / "))
			continue
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			t.Errorf("POST %s's Rate is %T, not a call — the sentence has to be built from the "+
				"constants that enforce the cap (%s)", c.Path, expr, strings.Join(c.Consts, " / "))
			continue
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok {
			t.Errorf("POST %s's Rate calls %T — expected a package-level helper", c.Path, call.Fun)
			continue
		}
		decls := pkg.funcs[fn.Name]
		if len(decls) == 0 {
			t.Errorf("POST %s's Rate calls %s, which is not declared in this package",
				c.Path, fn.Name)
			continue
		}
		named := map[string]bool{}
		for _, d := range decls {
			ast.Inspect(d, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					named[id.Name] = true
				}
				return true
			})
		}
		for _, want := range c.Consts {
			if !named[want] {
				t.Errorf("POST %s's Rate is built by %s, which does not name %s — so the sentence "+
					"is not derived from the constant that enforces this route's cap (%s)",
					c.Path, fn.Name, want, strings.Join(c.Consts, " / "))
			}
		}
	}
}

// ── the per-IP rate sentences (card t_9cdfa18e) ──────────────────────────────
//
// The chat lane above is guarded because its cap is a named constant in
// rest_chat.go. These FOUR sentences are a different shape: the number that
// enforces them lives in a middleware.RateLimitWith call in rest.go, another
// file, and until this guard the contract's per-IP sentences were typed copies
// of those call sites. Move one — RateLimitWith(2, 6) -> RateLimitWith(9, 6) on
// /v1/rest/uploads/image — and api/openapi/openapi.json (and /docs/api, which
// renders the same sentence) went on promising 2/s while the route admitted 9/s,
// with every guard green: TestOpenAPIRateLimitsMatchTheCode asserts only that a
// RateMiddleware route carries SOME middleware.RateLimit wrap, never what that
// wrap was given, and it never reads the Rate string.
//
// The numbers are never restated in this file. The sentence is rendered from the
// constants the registration passes (aiGenerateRatePolicy / uploadRatePolicy in
// openapi_routes.go), and the guard reads the registration BACK OUT of rest.go
// to check that what a caller is promised is what the server applies.

// perIPPolicyCase is one /v1/rest route whose documented per-IP sentence must
// quote the numbers its OWN registration passes.
//
// The numbers are read out of rest.go — through the package's constants,
// in-process — rather than written here: a case that restated 2/6 would be a
// third copy of the cap, exactly the shape this guard exists to remove.
type perIPPolicyCase struct {
	Path   string
	Method string
	// Unit is the time unit the sentence quotes: "hour" or "second". It is the
	// one part of a sentence the call site cannot imply — a limiter is always
	// handed tokens per second — so it is stated, and the number is then checked
	// against the registration in that unit.
	Unit string
	// Consts are the package identifiers the sentence must be BUILT from AND that
	// the registration must PASS, in the order rate, burst.
	Consts []string
}

func perIPPolicyCases() []perIPPolicyCase {
	return []perIPPolicyCase{
		{Path: "/v1/rest/forms/ai-generate", Method: "POST", Unit: "hour",
			Consts: []string{"aiGenerateRatePerHour", "aiGenerateBurst"}},
		{Path: "/v1/rest/uploads/image", Method: "POST", Unit: "second",
			Consts: []string{"uploadRatePerSecond", "uploadBurst"}},
		{Path: "/v1/rest/uploads/avatar", Method: "POST", Unit: "second",
			Consts: []string{"uploadRatePerSecond", "uploadBurst"}},
		{Path: "/v1/rest/uploads/asset", Method: "POST", Unit: "second",
			Consts: []string{"uploadRatePerSecond", "uploadBurst"}},
	}
}

// restGORateWrap is the rate-limit wrap one /v1/rest registration in rest.go
// carries.
type restGORateWrap struct {
	// Kind is "with" for middleware.RateLimitWith(rate, burst), "bare" for
	// middleware.RateLimit (whose numbers are, by its own body, the exported
	// middleware.RateLimitRate / RateLimitBurst), and "" when the registration
	// carries no wrap at all.
	Kind  string
	Rate  ast.Expr
	Burst ast.Expr
	Line  int
}

// restGoRateWraps reads the limiter ARGUMENTS out of rest.go's registrations.
//
// It parses the file with go/parser instead of joining lines the way
// restGoRoutes does, because what is read here is an expression nested inside
// `middleware.RateLimitWith(a, b)(handler)`: only a real parse tells
// `RateLimitWith(uploadRatePerSecond, uploadBurst)` apart from
// `RateLimitWith(9, 6)` — the difference this guard exists for.
func restGoRateWraps(t *testing.T) map[string]restGORateWrap {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset,
		filepath.Join(rootDir, "api", "internal", "httpapi", "rest.go"), nil,
		parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("cannot parse rest.go: %v", err)
	}
	patternRe := regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) (/v1/rest/[^ ]+)$`)
	out := map[string]restGORateWrap{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "mux" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		pattern, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		m := patternRe.FindStringSubmatch(pattern)
		if m == nil {
			return true
		}
		w := restGORateWrap{Line: fset.Position(call.Pos()).Line}
		ast.Inspect(call.Args[1], func(n ast.Node) bool {
			inner, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgID, ok := fn.X.(*ast.Ident)
			if !ok || pkgID.Name != "middleware" {
				return true
			}
			switch fn.Sel.Name {
			case "RateLimitWith":
				if len(inner.Args) == 2 {
					w.Kind, w.Rate, w.Burst = "with", inner.Args[0], inner.Args[1]
				}
			case "RateLimit":
				w.Kind = "bare"
			}
			return true
		})
		out[routeKey(m[1], m[2])] = w
		return true
	})
	return out
}

// capConstValues maps this package's rate-cap constants to their values, read
// from the SYMBOLS rather than restated, so the evaluator below resolves a
// registration's `aiGenerateRatePerHour/3600.0` to the number the limiter is
// really handed, and a constant that moves moves this map with it.
func capConstValues() map[string]float64 {
	return map[string]float64{
		"aiGenerateRatePerHour": aiGenerateRatePerHour,
		"aiGenerateBurst":       aiGenerateBurst,
		"uploadRatePerSecond":   uploadRatePerSecond,
		"uploadBurst":           uploadBurst,
		"chatRatePerHour":       chatRatePerHour,
		"chatBurst":             chatBurst,
		"chatConfirmPerHour":    chatConfirmPerHour,
		"chatConfirmBurst":      chatConfirmBurst,
	}
}

// rateExprValue evaluates one limiter argument from rest.go.
//
// Only the shapes a cap can honestly be written in are accepted — a number, one
// of the package's cap constants, or a sum/product/quotient of those (the hourly
// cap is divided by 3600 at the call site). Anything else is an ERROR rather
// than a silent zero: a guard that cannot tell what a registration passes must
// not certify that it passes the documented number.
func rateExprValue(t *testing.T, expr ast.Expr, consts map[string]float64) (float64, bool) {
	t.Helper()
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.INT || e.Kind == token.FLOAT {
			v, err := strconv.ParseFloat(e.Value, 64)
			if err != nil {
				t.Errorf("cannot read the limiter argument %s: %v", e.Value, err)
				return 0, false
			}
			return v, true
		}
		t.Errorf("a limiter argument for a documented cap is the %s literal %s — this guard only reads "+
			"numbers", e.Kind, e.Value)
		return 0, false
	case *ast.Ident:
		v, ok := consts[e.Name]
		if !ok {
			names := make([]string, 0, len(consts))
			for n := range consts {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Errorf("the limiter argument is %s, which this guard cannot resolve to a number. Name the cap in "+
				"one of the package constants (%s) so the published contract can quote what the registration "+
				"passes.", e.Name, strings.Join(names, ", "))
			return 0, false
		}
		return v, true
	case *ast.ParenExpr:
		return rateExprValue(t, e.X, consts)
	case *ast.UnaryExpr:
		if e.Op == token.SUB {
			v, ok := rateExprValue(t, e.X, consts)
			return -v, ok
		}
	case *ast.BinaryExpr:
		a, okA := rateExprValue(t, e.X, consts)
		b, okB := rateExprValue(t, e.Y, consts)
		if !okA || !okB {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return a + b, true
		case token.SUB:
			return a - b, true
		case token.MUL:
			return a * b, true
		case token.QUO:
			return a / b, true
		}
	}
	t.Errorf("the limiter argument is %T, which this guard cannot evaluate — a cap has to be a number or a "+
		"package constant", expr)
	return 0, false
}

// limiterNumbers returns the rate (tokens per SECOND, which is what a limiter is
// handed) and the burst this registration applies, read out of rest.go.
func (w restGORateWrap) limiterNumbers(t *testing.T) (rate, burst float64, ok bool) {
	t.Helper()
	switch w.Kind {
	case "with":
		consts := capConstValues()
		r, okR := rateExprValue(t, w.Rate, consts)
		b, okB := rateExprValue(t, w.Burst, consts)
		return r, b, okR && okB
	case "bare":
		// A bare middleware.RateLimit wrap IS RateLimitRate/RateLimitBurst — that
		// is its own body — so a sentence for such a route has to quote those.
		return middleware.RateLimitRate, middleware.RateLimitBurst, true
	}
	t.Errorf("rest.go line %d registers a /v1/rest route with no middleware rate wrap at all, so there is "+
		"nothing for the contract to quote", w.Line)
	return 0, 0, false
}

// perIPPolicyRe is the shape of a per-IP sentence. The guard and the sentence
// have to agree on it: if they do not, the parse below certifies nothing.
var perIPPolicyRe = regexp.MustCompile(
	`^([0-9]+(?:\.[0-9]+)?) per (hour|second) per IP, burst ([0-9]+(?:\.[0-9]+)?)$`)

// parsePerIPPolicy splits a published sentence into its numbers and its unit.
func parsePerIPPolicy(policy string) (rate float64, unit string, burst float64, ok bool) {
	m := perIPPolicyRe.FindStringSubmatch(policy)
	if m == nil {
		return 0, "", 0, false
	}
	r, errR := strconv.ParseFloat(m[1], 64)
	b, errB := strconv.ParseFloat(m[3], 64)
	if errR != nil || errB != nil {
		return 0, "", 0, false
	}
	return r, m[2], b, true
}

// rateWithinTolerance compares two cap numbers. The tolerance exists only
// because an hourly cap is the limiter's per-second number times 3600
// (5/3600*3600 is not exactly 5 in binary floating point); it is far smaller
// than any real change to a cap.
func rateWithinTolerance(a, b float64) bool {
	const eps = 1e-9
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return math.Abs(a-b) <= eps*scale
}

// inUnit converts a limiter rate (tokens per second) into the unit a sentence
// quotes.
func inUnit(rate float64, unit string) (float64, bool) {
	switch unit {
	case "second":
		return rate, true
	case "hour":
		return rate * 3600, true
	}
	return 0, false
}

// identsIn collects every identifier name in an expression — including the Sel
// of a selector, so a cap written as middleware.RateLimitRate counts as naming
// RateLimitRate.
func identsIn(expr ast.Expr) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// TestMiddlewareRatePoliciesQuoteTheRegisteredCaps pins each per-IP sentence to
// the numbers its own registration in rest.go passes. It reads the call site
// (restGoRateWraps → the limiter's arguments, through the package constants) and
// compares against the sentence in the document the generator produces NOW, and
// in the two JSON artifacts ON DISK.
//
// WHY THE ARTIFACTS ARE READ TOO. The in-memory document is the code's own
// answer, so it catches a registration that moved; the on-disk bytes are what a
// developer actually downloads, so they catch a registration that moved and was
// never regenerated — the half a code-only comparison misses, because a stale
// artifact agrees with the stale sentence.
func TestMiddlewareRatePoliciesQuoteTheRegisteredCaps(t *testing.T) {
	wraps := restGoRateWraps(t)
	// ANCHOR, not courtesy: a parse that saw a handful of registrations would
	// make every lookup below a miss dressed up as a pass.
	if len(wraps) < 50 {
		t.Fatalf("parsed %d /v1/rest registrations out of rest.go — that is not the route table, so the "+
			"lookups below would check nothing", len(wraps))
	}
	sources := []struct{ name, body string }{
		{"the document the generator produces (in memory)", generatedArtifact(t, "api/openapi/openapi.json")},
		{"api/openapi/openapi.json", readRootFile(t, "api/openapi/openapi.json")},
		{"web/public/openapi.json", readRootFile(t, "web/public/openapi.json")},
	}
	for _, src := range sources {
		paths := parseSpecPaths(t, src.name, src.body)
		if len(paths) < 50 {
			t.Errorf("%s has %d paths — that is not the API, so the lookups below would check nothing",
				src.name, len(paths))
			continue
		}
		for _, c := range perIPPolicyCases() {
			wrap, ok := wraps[routeKey(c.Method, c.Path)]
			if !ok {
				t.Errorf("rest.go registers no %s %s — the anchor moved", c.Method, c.Path)
				continue
			}
			limiterRate, limiterBurst, ok := wrap.limiterNumbers(t)
			if !ok {
				continue
			}
			wantRate, ok := inUnit(limiterRate, c.Unit)
			if !ok {
				t.Errorf("the case for %s %s states the unit %q, which is neither \"hour\" nor \"second\"",
					c.Method, c.Path, c.Unit)
				continue
			}
			op, ok := paths[c.Path][strings.ToLower(c.Method)]
			if !ok {
				t.Errorf("%s: no %s %s in the document — the anchor moved", src.name, c.Method, c.Path)
				continue
			}
			if op.RateLimit == nil {
				t.Errorf("%s: %s %s carries no x-jomform-rate-limit, so the contract promises a lane the "+
					"server throttles without saying so", src.name, c.Method, c.Path)
				continue
			}
			gotRate, gotUnit, gotBurst, shapeOK := parsePerIPPolicy(op.RateLimit.Policy)
			if !shapeOK {
				t.Errorf("%s: %s %s documents the rate policy %q, which does not quote the cap as "+
					"\"<N> per hour|second per IP, burst <N>\" — the sentence and this guard have to agree on "+
					"the shape", src.name, c.Method, c.Path, op.RateLimit.Policy)
				continue
			}
			if gotUnit != c.Unit {
				t.Errorf("%s: %s %s documents %q, but the case says this sentence quotes a cap per %s — "+
					"either the sentence or the case moved", src.name, c.Method, c.Path, op.RateLimit.Policy,
					c.Unit)
				continue
			}
			if !rateWithinTolerance(gotRate, wantRate) || !rateWithinTolerance(gotBurst, limiterBurst) {
				t.Errorf("%s: %s %s documents %g per %s per IP, burst %g, but rest.go line %d hands its limiter "+
					"%g per second, burst %g — i.e. %g per %s, burst %g. The published contract tells the caller "+
					"a limit the server does not apply.\n(The sentence is built from the constants; if you moved "+
					"one, run: cd api && go run ./cmd/openapi-gen)",
					src.name, c.Method, c.Path, gotRate, gotUnit, gotBurst, wrap.Line,
					limiterRate, limiterBurst, wantRate, c.Unit, limiterBurst)
			}
			// The same sentence is appended to the description, the copy a human
			// reads in the rendered reference.
			if !strings.Contains(op.Description, "Rate limit: "+op.RateLimit.Policy+".") {
				t.Errorf("%s: %s %s's description does not carry \"Rate limit: %s.\" — the extension and the "+
					"prose have diverged", src.name, c.Method, c.Path, op.RateLimit.Policy)
			}
		}
	}
}

// TestEveryMiddlewareRateSentenceIsGuarded is the anchor that closes the door
// this defect came in by. The four guards above only walk the routes their case
// table names, so a NEW throttled route could arrive with a typed sentence and
// every one of them would stay green. The contract's set of RateMiddleware
// entries and the case table must therefore be the SAME set, in both directions.
func TestEveryMiddlewareRateSentenceIsGuarded(t *testing.T) {
	var inRegistry []string
	for _, d := range restRouteDocs {
		if d.RateSource != RateMiddleware {
			continue
		}
		if d.Rate == "" {
			t.Errorf("%s %s declares RateSource=%s with an empty Rate — a throttled route that documents no "+
				"limit", d.Method, d.Path, d.RateSource)
			continue
		}
		inRegistry = append(inRegistry, routeKey(d.Method, d.Path))
	}
	var inCases []string
	for _, c := range perIPPolicyCases() {
		inCases = append(inCases, routeKey(c.Method, c.Path))
	}
	if len(inRegistry) < 4 {
		t.Fatalf("the registry holds %d per-IP sentences — fewer than the four known throttled routes, so "+
			"this guard would compare almost nothing (%v)", len(inRegistry), inRegistry)
	}
	sort.Strings(inRegistry)
	sort.Strings(inCases)
	if equalStrings(inRegistry, inCases) {
		return
	}
	for _, key := range inRegistry {
		if !containsString(inCases, key) {
			t.Errorf("%s is published with a middleware rate sentence but has no case in perIPPolicyCases() — "+
				"its numbers are therefore not checked against the registration in rest.go. Add it, and derive "+
				"its sentence from the cap instead of typing it.", key)
		}
	}
	for _, key := range inCases {
		if !containsString(inRegistry, key) {
			t.Errorf("perIPPolicyCases() holds %s, which the contract no longer publishes a middleware rate "+
				"sentence for — a stale case certifies nothing", key)
		}
	}
}

// TestMiddlewareRatePoliciesAreDerivedNotRestated is the half a value comparison
// cannot express (the same reasoning as the chat lane's guard): a literal that
// happens to be the right number compares equal to the constant. It checks BOTH
// ends of the copy — the sentence in openapi_routes.go, and the argument the
// registration in rest.go passes.
func TestMiddlewareRatePoliciesAreDerivedNotRestated(t *testing.T) {
	pkg := parseRESTPackage(t)
	file, err := parser.ParseFile(token.NewFileSet(),
		filepath.Join(rootDir, "api", "internal", "httpapi", "openapi_routes.go"),
		nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("cannot parse openapi_routes.go: %v", err)
	}
	rates, entries := registryRateFields(t, file)
	if entries < 50 {
		t.Fatalf("walked only %d contract entries in restRouteDocs — the registry's shape moved, so this "+
			"guard would check nothing", entries)
	}
	wraps := restGoRateWraps(t)
	for _, c := range perIPPolicyCases() {
		expr, ok := rates[c.Path]
		if !ok {
			t.Errorf("no contract entry for %s %s — the anchor moved", c.Method, c.Path)
			continue
		}
		requireRateBuiltFrom(t, fmt.Sprintf("%s %s's Rate", c.Method, c.Path), expr, pkg, c.Consts)

		wrap, ok := wraps[routeKey(c.Method, c.Path)]
		if !ok {
			t.Errorf("rest.go registers no %s %s — the anchor moved", c.Method, c.Path)
			continue
		}
		if wrap.Kind == "bare" {
			// middleware.RateLimit takes no arguments to inspect; its numbers are
			// the exported middleware constants, and the value guard already holds
			// the sentence to them.
			continue
		}
		if wrap.Kind != "with" {
			t.Errorf("rest.go line %d registers %s %s without a rate wrap, so the contract documents a limit "+
				"nothing applies", wrap.Line, c.Method, c.Path)
			continue
		}
		for i, arg := range []struct {
			expr ast.Expr
			role string
		}{{wrap.Rate, "rate"}, {wrap.Burst, "burst"}} {
			if i >= len(c.Consts) {
				break
			}
			if !identsIn(arg.expr)[c.Consts[i]] {
				t.Errorf("rest.go line %d passes %s to the limiter's %s argument for %s %s, which does not name "+
					"%s — the registration is restating the cap, so the contract can quote a number the route "+
					"does not apply", wrap.Line, sourceOf(arg.expr), arg.role, c.Method, c.Path, c.Consts[i])
			}
		}
	}
}

// requireRateBuiltFrom asserts a contract Rate expression is a CALL — not a
// literal — whose callee's body names every one of consts. That is what makes
// the published sentence move with the cap instead of drifting from it.
func requireRateBuiltFrom(t *testing.T, what string, expr ast.Expr, pkg *restPackage, consts []string) {
	t.Helper()
	if lit, isLit := expr.(*ast.BasicLit); isLit {
		t.Errorf("%s is the string literal %s — a literal is a copy of the cap that no test can tell from the "+
			"truth. Build it from %s instead.", what, lit.Value, strings.Join(consts, " / "))
		return
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		t.Errorf("%s is %T, not a call — the sentence has to be built from the constants that enforce the "+
			"cap (%s)", what, expr, strings.Join(consts, " / "))
		return
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok {
		t.Errorf("%s calls %T — expected a package-level helper", what, call.Fun)
		return
	}
	decls := pkg.funcs[fn.Name]
	if len(decls) == 0 {
		t.Errorf("%s calls %s, which is not declared in this package", what, fn.Name)
		return
	}
	named := map[string]bool{}
	for _, d := range decls {
		ast.Inspect(d, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				named[id.Name] = true
			}
			return true
		})
	}
	for _, want := range consts {
		if !named[want] {
			t.Errorf("%s is built by %s, which does not name %s — so the sentence is not derived from the "+
				"constant that enforces this route's cap (%s)", what, fn.Name, want, strings.Join(consts, " / "))
		}
	}
}

// sourceOf renders an expression for a failure message.
func sourceOf(expr ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), expr); err != nil {
		return fmt.Sprintf("%T", expr)
	}
	return b.String()
}

// containsString reports whether xs holds want.
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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
