// Card t_25f5e9ea — the CLASS guard for the gate's unlock route.
// Card t_020146e6 — the case list is now DISCOVERED from the mux, not hand-written.
// Card t_0da7d67f — discovery is resolved BY POSITION, not by a wildcard's NAME.
//
// A private form's gate posts the password to its OWN address plus "/unlock"
// (location.pathname + "/unlock" — see renderPrivateGate). That rule is what
// made an EMBEDDED private form impossible to unlock: the gate used to hardcode
// the canonical host-scoped "/<slug>/unlock", so the embed posted at a route on
// the merchant's origin that could never name the workspace.
//
// Fixing only the embed route would have left the SAME defect on every other
// route that renders this gate — and it did: /f/{slug} renders the gate too, so
// the moment the gate started posting to its own path, /f/{slug} began posting
// at /f/{slug}/unlock, which was not registered. The page still rendered, still
// showed a password box, and could never be unlocked. That is the exact bug this
// card exists to fix, one route over.
//
// So this guard is deliberately ROUTE-AGNOSTIC and mechanical: it renders the
// gate through each route that can produce it, reads the target the rendered
// page would actually fetch, and asserts that target resolves to a handler (not
// the mux's own "404 page not found").
//
// It is a live-request guard, not a source scrape: it cannot be satisfied by a
// comment claiming the route exists.
//
// # WHY THE CASE LIST IS NOT WRITTEN DOWN (t_020146e6)
//
// This comment used to claim "the case list below is the full set of routes
// measured to reach renderPrivateGate" — beside a hand-written 3-entry slice,
// which made the guard exactly as wide as a human remembering to add a case.
// That is the failure mode the guard exists to remove, and it was measured on
// d9db99f: with `GET /qa-probe/{slug}` injected beside `GET /{slug}` (the SAME
// handler, so it renders the gate and posts to a /qa-probe/{slug}/unlock that
// nobody registered) the guard reported
//
//	--- PASS: TestEveryGateRouteHasItsOwnUnlockRoute
//
// with its three hardcoded subtests, and never touched the fourth route.
//
// The case list is now discovered, twice over, and neither half is a list a
// human maintains:
//
//  1. ENUMERATION comes from the mux's own route table (muxPatterns), so every
//     registered route is walked whether or not anyone remembered it. A gate
//     route that is NOT registered is unreachable by construction — the mux's
//     own reply to its GET is "404 page not found" — so it cannot reach the gate
//     and is out of scope by definition.
//  2. SELECTION is by REQUEST: every registration that can answer a GET — the
//     `GET` ones, plus a pattern registered with NO method, which net/http
//     matches for GET too — is driven with the fixture on its own host (a
//     host-qualified pattern is matched against the request Host) and asked
//     whether it rendered the gate (id="pw" plus the gate's own fetch). Nothing
//     here has to know which route that is, so a route added tomorrow is covered
//     without an edit here.
//
// # WHY FILLING IS BY POSITION AND NOT BY NAME (t_0da7d67f)
//
// The first cut of the discovered guard still resolved a concrete path by
// asking what each wildcard was CALLED: `{slug}` became the fixture's form slug,
// `{accslug}` the workspace slug, and anything else the placeholder "x". That
// made correct coverage depend on a name — and a wildcard name is a private
// detail of a registration (net/http hands it back only through
// PathValue(name)); nothing outside the handler can be required to know it. Two
// measured consequences, both fixed here:
//
//   - main's #750 renamed the embed wildcard `accslug` -> `wsslug` on the very
//     lines this guard rewrote. Under name-keyed substitution the embed route
//     was then driven at /embed/f/x/<slug>, resolved no form, and DROPPED OUT of
//     the judged set: the guard failed its own discovery floor on the merged
//     tree (2 of 3) instead of covering the route.
//   - a gate route registered as `GET /qa-renamed/{formSlug}` whose handler reads
//     PathValue("formSlug") is a legal net/http pattern of exactly the shape
//     #750 applied to the embed route. Driven at the placeholder /qa-renamed/x
//     it 404'd, was classified "not a gate route", and the guard reported PASS
//     while GET /qa-renamed/<slug> rendered the password gate and POST
//     /qa-renamed/<slug>/unlock answered "404 page not found" — the #722 defect
//     class, one shape over.
//
// The fix is to stop consulting names at all. Every wildcard of every discovered
// pattern is filled POSITIONALLY with the fixture's own identifiers (the form
// slug and the workspace slug), every assignment is tried, and a pattern is a
// gate route if ANY assignment renders the gate. No wildcard name can appear
// anywhere in that decision, so no rename can shrink the judged set.
//
// Completeness argument (what makes "not a gate route" a measurement rather than
// a shrug): the gate renders only when the handler resolves the fixture's form,
// and the fixture owns exactly one workspace and one form. The only channels a
// request can use to name them are the path (a wildcard — a position this guard
// cycles through both values) and the Host (host wildcards, filled with the
// workspace slug). So a route that CAN render the gate renders it for one of the
// assignments tried here. The one shape that would escape is a handler that
// composes its lookup from something this guard cannot vary — a query parameter,
// or two path values concatenated into one key; no route in this mux does that,
// and it is stated under WHAT IT DOES NOT COVER rather than left implied.
//
// # A PATTERN THAT CANNOT BE DRIVEN IS A FAILURE, NOT A SKIP
//
// The old walk wrote `continue` for "did not render the gate", which cannot tell
// "not a gate route" from "a gate route I failed to path-fill". Each filling is
// therefore checked against the mux itself (muxMatchFor): it must reach THAT
// pattern's own handler. A pattern where no filling does fails by name — the
// guard refuses to report on a route it could not drive. And because one handler
// renders (or does not render) the gate for every pattern it serves, two
// patterns sharing a handler must get the same verdict; a split means a probe
// missed the handler's form lookup and is reported as exactly that.
//
// Three floors keep a discovered case list honest, because it has a failure mode
// a written one does not — discovering nothing and reporting green: a floor on
// the walked pattern set, a floor on the discovered gate set, and a bounded
// per-route probe so a stalled handler fails by name instead of hanging the
// package.
//
// # WHAT IT DOES NOT COVER (stated so the guard is not over-claimed)
//
//   - The walk covers the mux PublicHandlers.RegisterRoutes populates. A
//     gate-rendering route registered on a DIFFERENT mux (e.g. inline in
//     Server.Handler) is not in that table and would not be discovered.
//   - muxPatterns reads net/http's unexported routing tree through reflection,
//     the only surface Go exposes (ServeMux has no pattern list; its own
//     `Handler()` answers one request at a time, not "what did you register").
//     A future Go release that renames those fields makes the walk return
//     nothing, which fails the floors above with an explanation rather than
//     silently narrowing the guard; TestGateRouteWalkerSeesEveryRegisteredPattern
//     pins the walk against exactly that. A release that shapes a `pattern`
//     field as a pointer to something other than a struct would have made the
//     walk PANIC (taking the whole package down, with no named verdict for any
//     guard in it) — that field is now skipped and reported by name instead, and
//     TestGateRouteWalkerReportsAFieldItCannotRead pins both directions: the
//     report fires for the unreadable shape, and it stays quiet for the shapes
//     the walk really does read.
//   - Discovery drives every GET route with a fixture path, so routes that are
//     unrelated to forms (a sitemap, a legal page, /t/{ref}) are requested too.
//     Their responses are only read for the gate marker; a 404/500 there is
//     neither asserted nor a failure of this guard.
//   - Each pattern is driven at ONE concrete path per assignment: a multi-segment
//     wildcard, or a path ending in "/", is filled with a single segment. A gate
//     reachable only at a DEEPER path under such a pattern is therefore covered
//     once, at the pattern's shortest path, and not at every subpath it matches.
//     No route in this mux today has that shape (the three gate routes are all
//     exact single-segment patterns).
//   - Wildcards are filled only with the fixture's own identifiers, and only by
//     position. A handler that resolves its form through a channel this guard
//     cannot vary (a query parameter, a header, or two path values concatenated
//     into one lookup key) would not be recognised as a gate route. No route in
//     this mux resolves a form that way; adding one is the change that would
//     need this guard revisited, and it says so here rather than looking
//     complete while not being so.
package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// gateFetchTargetRe extracts the target of the gate's unlock fetch from the
// rendered page — `fetch(location.pathname+'/unlock')`. Reading it out of the
// page (rather than restating it in the test) is the point: if the renderer is
// ever changed to post somewhere else, this guard follows it and checks THAT
// address instead of asserting a string the test itself supplied.
var gateFetchTargetRe = regexp.MustCompile(`fetch\(location\.pathname\s*\+\s*'(\/[a-z]+)'`)

// gateRouteDiscoveryMin is the floor on the discovered gate set. The three
// routes below are the ones measured to reach renderPrivateGate, so a walk that
// finds fewer has lost coverage — a broken reflection walk, or a fixture that
// stopped rendering the gate — and must fail rather than pass on a narrower set
// than the guard claims to cover.
//
//	GET /{slug}                          host scope (workspace subdomain / custom domain)
//	GET /f/{slug}                        host scope
//	GET /embed/f/{wsslug}/{slug}         path scope (an embed's Host names the merchant)
const gateRouteDiscoveryMin = 3

// gatePatternFloor is the floor on the walk itself: this mux registers 20+
// patterns, so a walk returning a handful means the reflection is not seeing the
// route table (Go changed its internals), which would make the gate floors above
// meaningless. Deliberately loose — it exists to catch a blind walk, not to pin
// the route count.
const gatePatternFloor = 10

// gateRouteProbeBudget bounds how long one discovered route may spend in handler
// code before the guard fails. Measured on this fixture the slowest GET route in
// this mux is the OG image renderer (36-357ms across runs); every other route
// answers in 0-55ms. The budget exists to turn a route that BLOCKS into a named
// failure instead of a ten-minute package hang, so it is set with a wide margin
// above that measurement (a loaded host must not trip it) and the guard logs the
// measured slowest probe next to the budget on every run, so a future change has
// the data to argue from.
const gateRouteProbeBudget = 5 * time.Second

// patternStrings returns every pattern string held by the value rooted at root
// (for a *http.ServeMux that is its whole routing tree), plus a note for every
// `pattern` field it could NOT read.
//
// Go exposes no public pattern list on ServeMux — Handler(r) answers a single
// request, it does not enumerate — so the routing tree is walked by reflection:
// each leaf holds a *pattern whose String() is the pattern as it was registered
// ("GET /f/{slug}"). Handlers are not inspected; only the pattern strings are.
//
// TWO WAYS THIS WALK CAN FAIL TO SEE A PATTERN, and why neither is a panic or a
// silent nothing:
//
//   - The field names it reads are net/http's internals (routingNode.pattern,
//     pattern.str). A release that renames either returns a SHORT or EMPTY walk,
//     which the callers' floors refuse with an explanation (see
//     TestGateRouteWalkerSeesEveryRegisteredPattern and the guards' floors).
//   - A `pattern` field that is a pointer to something OTHER than a struct used
//     to make the walk PANIC — measured on go1.27.0 against the pre-fix code:
//     `pattern *int` gave "reflect: call of reflect.Value.FieldByName on int
//     Value", which would take the whole httpapi package down instead of failing
//     the guard by name. The pointee's KIND is now checked before it is read, and
//     such a field is reported through `unreadable` so the caller fails BY NAME.
//     (Measured on the same toolchain: this is the reachable panic. Reading an
//     unexported field is NOT one — FieldByName, Elem, Len, NumField, MapRange,
//     IsNil and String on a value obtained from an unexported field all work;
//     only Interface() is refused, and this walk never calls it.)
func patternStrings(root reflect.Value) (out, unreadable []string) {
	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if !v.IsValid() || depth > 32 {
			return
		}
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if v.IsNil() {
				return
			}
			walk(v.Elem(), depth+1)
		case reflect.Struct:
			// A leaf node's *pattern; str is the original pattern string.
			if p := v.FieldByName("pattern"); p.IsValid() && p.Kind() == reflect.Pointer && !p.IsNil() {
				if e := p.Elem(); e.Kind() != reflect.Struct {
					unreadable = append(unreadable,
						fmt.Sprintf("%s.pattern points at a %s, not a struct", v.Type(), e.Kind()))
				} else if s := e.FieldByName("str"); s.IsValid() && s.Kind() == reflect.String {
					out = append(out, s.String())
				}
			}
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}
	walk(root, 0)
	return out, unreadable
}

// walkNotes is the reporting seam muxPatterns uses: *testing.T satisfies it, and
// so does walkNoteRecorder, which lets the reporting itself be tested (testing.TB
// cannot be faked from outside the testing package — it carries an unexported
// method — so the seam is this narrow interface rather than TB).
type walkNotes interface {
	Helper()
	Errorf(format string, args ...any)
}

// reportWalkNotes fails BY NAME for a walk that could not read part of the route
// table. It exists because the alternative shapes are worse than a red test: a
// panic takes the whole package down (no named verdict for any guard in it), and
// returning the patterns it did read lets the guards report green on a set they
// only partly cover.
func reportWalkNotes(t walkNotes, unreadable []string) bool {
	t.Helper()
	if len(unreadable) == 0 {
		return true
	}
	t.Errorf("the route-table walk could not read %d `pattern` field(s): %v — a field this walk "+
		"cannot read is a route it cannot enumerate, so every guard that enumerates from the walk "+
		"would report on a set it does not cover. This is reported here by name rather than as a "+
		"panic (which would take the whole package down) or as a short walk (which the floors can "+
		"only catch when it is short ENOUGH). Re-point the walk at net/http's current shape.",
		len(unreadable), unreadable)
	return false
}

// muxPatterns returns every pattern registered on mux, sorted.
func muxPatterns(t *testing.T, mux *http.ServeMux) []string {
	t.Helper()
	out, unreadable := patternStrings(reflect.ValueOf(mux))
	reportWalkNotes(t, unreadable)
	sort.Strings(out)
	return out
}

// patternWildcard matches one {name} or {name...} wildcard segment.
var patternWildcard = regexp.MustCompile(`\{([^}]*)\}`)

// gateRouteProbe is one registered pattern taken apart into the pieces the guard
// needs to build requests for it. It holds no fixture value: the pattern is
// parsed once and the values are applied later, positionally, by pathFor. See
// the file header for why the wildcard NAMES are never consulted.
type gateRouteProbe struct {
	pattern  string   // as registered, e.g. "GET /f/{slug}"
	method   string   // as registered; "" means the pattern accepts every method
	hostPat  string   // "" when the pattern is not host-qualified
	pathPat  string   // the path as registered, wildcards intact
	wildcard []string // wildcard names in path order; {$} excluded (it consumes no value)
}

// gateRouteProbeFor parses a registered pattern. No fixture value is applied
// here — substitution is positional and happens in pathFor, so nothing in this
// file can go stale when a wildcard is renamed.
//
// The pattern grammar is net/http's own: `[METHOD ][HOST]/[PATH]`. BOTH optional
// parts are parsed, because skipping either would put a hole straight back into
// this guard:
//
//   - A pattern with NO method matches GET as well as POST, so `mux.HandleFunc(
//     "/qa/{slug}", h.handleWorkspaceRootForm)` renders the gate and posts to a
//     /qa/{slug}/unlock nobody registered. Treating "no method token" as "not a
//     GET route" would skip it — the very defect this guard exists to catch.
//   - A HOST-qualified pattern (`example.com/{slug}`) is matched against the
//     request's Host, so it is driven at its OWN literal host. net/http's
//     pattern grammar has no host wildcard — parsePattern rejects any '{' in the
//     host outright ("host contains '{' (missing initial '/'?)"), so a pattern
//     like `{ws}.jomform.com/{slug}` PANICS at registration and can never be a
//     live route. TestServeMuxRejectsAWildcardHost pins that, so this guard is
//     not silently assuming a filling net/http will not permit.
func gateRouteProbeFor(pat string) gateRouteProbe {
	p := gateRouteProbe{pattern: pat}
	rest := pat
	if sp := strings.IndexByte(rest, ' '); sp > 0 {
		p.method, rest = rest[:sp], rest[sp+1:]
	}
	// Host-qualified iff something precedes the first "/" (a bare path starts
	// with one).
	if i := strings.IndexByte(rest, '/'); i > 0 {
		p.hostPat, rest = rest[:i], rest[i:]
	}
	p.pathPat = rest
	for _, m := range patternWildcard.FindAllString(rest, -1) {
		name := m[1 : len(m)-1]
		if name == "$" {
			continue // /{$} is the literal path "/" — it consumes no value
		}
		p.wildcard = append(p.wildcard, name)
	}
	return p
}

// pathFor fills the pattern's wildcards POSITIONALLY with vals — vals[0] for the
// first wildcard, vals[1] for the second, and so on — without reading a single
// wildcard name. A `{$}` segment resolves to the empty string (which is what the
// pattern means: the path "/"). A multi-segment wildcard gets one segment: the
// shortest concrete path that reaches the route.
func (pr gateRouteProbe) pathFor(vals []string) string {
	i := 0
	return patternWildcard.ReplaceAllStringFunc(pr.pathPat, func(m string) string {
		if m[1:len(m)-1] == "$" {
			return ""
		}
		if i >= len(vals) {
			return ""
		}
		v := vals[i]
		i++
		return v
	})
}

// hostFor returns a pattern's host as registered: net/http has no host wildcard
// (see gateRouteProbeFor), so there is nothing to fill.
func (pr gateRouteProbe) hostFor(ws string) string {
	_ = ws
	return pr.hostPat
}

// gateFillings returns the value assignments to try for a pattern with n
// wildcards. This is the ONLY place a wildcard is resolved, and it resolves BY
// POSITION over the fixture's own identifiers — never by the wildcard's name.
//
// The full cross product is used while it stays small (2 values, n<=5 → 32
// assignments); beyond that it degrades to the uniform assignments plus the
// one-position-different ones, which still covers the shape that matters (one
// position holds the workspace slug, the rest the form slug). Bounded on
// purpose: this runs per route, inside one package test.
func gateFillings(n int, values []string) [][]string {
	if n == 0 {
		return [][]string{{}}
	}
	full := 1
	for i := 0; i < n && full <= 32; i++ {
		full *= len(values)
	}
	if full <= 32 {
		out := [][]string{{}}
		for i := 0; i < n; i++ {
			next := make([][]string, 0, len(out)*len(values))
			for _, cur := range out {
				for _, v := range values {
					next = append(next, append(append([]string(nil), cur...), v))
				}
			}
			out = next
		}
		return out
	}
	out := make([][]string, 0, len(values)+n*(len(values)-1))
	for _, v := range values {
		row := make([]string, n)
		for i := range row {
			row[i] = v
		}
		out = append(out, row)
	}
	for i := 0; i < n; i++ {
		for _, v := range values[1:] {
			row := make([]string, n)
			for j := range row {
				row[j] = values[0]
			}
			row[i] = v
			out = append(out, row)
		}
	}
	return out
}

// muxMatchFor asks the mux itself which pattern a GET reaches, and with which
// handler. The guard uses it to prove a filling actually drives the pattern
// under test: a filling that lands on a different pattern, or on nothing, is not
// evidence about this one and must not be read as "this pattern is not a gate
// route".
func muxMatchFor(mux *http.ServeMux, host, path string) (http.Handler, string) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	return mux.Handler(req)
}

// handlerKey names the handler a pattern resolves to, so two patterns served by
// the same code can be compared and so every classification can be printed with
// the handler that produced it.
func handlerKey(h http.Handler) string {
	if h == nil {
		return "<none>"
	}
	v := reflect.ValueOf(h)
	if v.Kind() == reflect.Func {
		if f := runtime.FuncForPC(v.Pointer()); f != nil {
			return f.Name()
		}
	}
	return fmt.Sprintf("%T", h)
}

// TestServeMuxRejectsAWildcardHost pins the net/http rule the probe's host
// handling stands on: a pattern's HOST section may not contain a wildcard.
// parsePattern rejects it outright ("host contains '{' (missing initial '/'?)"),
// and ServeMux.register PANICS on a rejected pattern — so `{ws}.jomform.com/
// {slug}` is not a route any process can serve, and a host-scoped registration
// is always a literal host.
//
// Why this is asserted rather than assumed: the guard reads a pattern's host
// verbatim and drives the request at it. If net/http ever grew a host wildcard,
// that verbatim host would name nothing and the route would look undriveable —
// so the rule this guard depends on is pinned here, at the cause.
func TestServeMuxRejectsAWildcardHost(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		mux := http.NewServeMux()
		mux.HandleFunc("{ws}.jomform.com/{slug}", func(http.ResponseWriter, *http.Request) {})
	}()
	if recovered == nil {
		t.Fatal("net/http REGISTERED a pattern whose host contains a wildcard — the class guard " +
			"drives host-qualified patterns at their literal host, which is only correct while a " +
			"host wildcard cannot exist. This guard's host handling needs revisiting.")
	}
	if msg := fmt.Sprint(recovered); !strings.Contains(msg, "host contains") {
		t.Fatalf("net/http refused the wildcard host for an unexpected reason (%v); the class "+
			"guard assumes the refusal is about the HOST section specifically", recovered)
	}
}

// TestGateRouteWalkerSeesEveryRegisteredPattern pins the walk the class guard
// depends on. If a Go release reshapes the routing tree, muxPatterns returns
// nothing and the class guard would go blind while still reporting green (with
// its floors explaining the empty walk) — this test fails at the cause instead:
// it registers known routes on a throwaway mux and requires every one back.
//
// The mux is exercised with more than a handful of patterns on purpose: the
// router holds few pairs in a slice and the rest in a map, so both tiers must be
// walked for the class guard's enumeration to be complete.
func TestGateRouteWalkerSeesEveryRegisteredPattern(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}

	for _, tc := range []struct {
		name     string
		patterns []string
	}{
		{"slice tier", []string{"GET /f/{slug}", "GET /{slug}", "POST /{slug}/unlock"}},
		{"map tier", func() []string {
			// Distinct first segments push the router past its small-slice tier.
			var pats []string
			for _, seg := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
				pats = append(pats, "GET /"+seg+"/{slug}")
			}
			return append(pats,
				"GET /{slug}",
				"POST /{slug}/unlock",
				"GET /zz-embed/f/{wsslug}/{slug}",
				"POST /zz-embed/f/{wsslug}/{slug}/unlock",
			)
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			for _, p := range tc.patterns {
				mux.HandleFunc(p, noop)
			}
			got := muxPatterns(t, mux)
			want := append([]string(nil), tc.patterns...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("muxPatterns walked %d of %d registered patterns — the class guard's "+
					"enumeration is incomplete, so a route could render the gate without being "+
					"covered.\nregistered: %v\ngot:        %v", len(got), len(want), want, got)
			}
		})
	}
}

// walkNoteRecorder captures what reportWalkNotes reports, so the walk's OWN
// failure reporting can be tested in both directions without a failing test.
// It satisfies walkNotes; *testing.T cannot be faked (testing.TB carries an
// unexported method), which is why muxPatterns takes that narrow seam.
type walkNoteRecorder struct {
	helperCalls int
	reports     []string
}

func (r *walkNoteRecorder) Helper() { r.helperCalls++ }

func (r *walkNoteRecorder) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

// TestGateRouteWalkerReportsAFieldItCannotRead pins the third leg of the walk's
// contract: a `pattern` field it cannot read is a ROUTE IT CANNOT ENUMERATE, and
// it must fail by name rather than by panicking or by returning a shorter list.
//
// MEASURED, on go1.27.0 against the pre-fix walk: a `pattern` field that is a
// non-nil pointer to a non-struct makes the read chain PANIC —
//
//	reflect: call of reflect.Value.FieldByName on int Value
//
// — out of muxPatterns, and therefore out of every guard in this package, since
// a panic in one test binary ends the whole run with no named verdict for any of
// them. That is the shape this asserts is now reported instead.
//
// The other half of the card's note ("FieldByName panics if the field is read
// through an interface value") was measured and does NOT reproduce: on the same
// toolchain FieldByName, Elem, Len, NumField, MapRange, IsNil and String are all
// legal on a value obtained from an unexported field (only Interface() is
// refused, and this walk never calls it). So the reachable panic is the pointee
// KIND, not the field's export status — asserted here, and the interface-typed
// leg is asserted NOT to panic below so the difference stays measured.
func TestGateRouteWalkerReportsAFieldItCannotRead(t *testing.T) {
	// ── the failure direction: an unreadable `pattern` is reported by name ──
	var (
		probeInt  int
		probeStr  = "legacy"
		unreadBad = reflect.ValueOf(&struct {
			pattern *int
			str     string
		}{pattern: &probeInt, str: probeStr})
	)

	out, unreadable := patternStrings(unreadBad)
	if len(unreadable) != 1 {
		t.Fatalf("patternStrings reported %d unreadable `pattern` field(s) for a *int one, want 1 "+
			"(read %d pattern(s): %v) — a field the walk cannot read must be reported, because "+
			"silently returning the patterns it could read lets every guard enumerate a set it "+
			"does not cover", len(unreadable), len(out), out)
	}
	if len(out) != 0 {
		t.Errorf("patternStrings invented %d pattern(s) from a *int `pattern` field: %v", len(out), out)
	}
	if !strings.Contains(unreadable[0], "*int") {
		t.Errorf("the unreadable note %q does not name what the field actually points at, so a "+
			"red would not say which shape needs re-pointing", unreadable[0])
	}

	rec := &walkNoteRecorder{}
	if reportWalkNotes(rec, unreadable) {
		t.Errorf("reportWalkNotes returned OK for a walk that could not read %v — the caller's "+
			"floors can only catch a SHORT walk, so an unreadable field that still leaves enough "+
			"patterns behind would pass silently", unreadable)
	}
	if len(rec.reports) != 1 {
		t.Fatalf("reportWalkNotes raised %d report(s) for %v, want exactly 1", len(rec.reports), unreadable)
	}

	// ── the pass direction (the control): the shapes the walk DOES read stay
	// quiet, and the walk really did read them, so the quiet is not vacuous ──
	rec = &walkNoteRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /f/{slug}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /{slug}/unlock", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /assets/{key...}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET example.com/{slug}", func(http.ResponseWriter, *http.Request) {})
	got, unread := patternStrings(reflect.ValueOf(mux))
	if len(got) != 5 {
		t.Fatalf("the walk read %d of the 5 registered patterns (%v) — the control below would "+
			"then be asserting quiet about a walk that is broken in the other direction", len(got), got)
	}
	if len(unread) != 0 {
		t.Errorf("the walk reported %v on a real mux — it must stay quiet about the shapes it "+
			"really does read, or every guard in this package reds on a healthy tree", unread)
	}
	if !reportWalkNotes(rec, unread) {
		t.Errorf("reportWalkNotes refused a healthy walk: %v", rec.reports)
	}
	if len(rec.reports) != 0 {
		t.Errorf("reportWalkNotes raised %v on a healthy walk, want none", rec.reports)
	}
	if rec.helperCalls == 0 {
		t.Errorf("reportWalkNotes called Helper() 0 times — a helper that does not mark itself " +
			"makes every report in a caller point at reportWalkNotes instead of the line that " +
			"supplied the walk")
	}

	// A `pattern` field that is a STRING is the net/http 1.21 muxEntry shape: it
	// must be skipped without a report and without a panic.
	strShape := reflect.ValueOf(&struct {
		pattern string
		h       any
	}{pattern: probeStr, h: 1})
	if _, unread := patternStrings(strShape); len(unread) != 0 {
		t.Errorf("a STRING `pattern` field (net/http's muxEntry shape) was reported as unreadable "+
			"(%v) — it holds no *pattern, so skipping it is correct and reporting it would red on "+
			"a shape the walk never claimed to read", unread)
	}

	// The interface-typed leg of the card's note: reading an unexported field
	// through an interface value must NOT panic. This is here so the difference
	// between the two failure modes stays measured rather than assumed.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("walking a value reached through an INTERFACE field panicked (%v) — the "+
					"card's note is that an unexported field read through an interface value "+
					"panics; if that is reachable in this walk it must be handled too", r)
			}
		}()
		// A *pattern-shaped field (pointer to a struct carrying `str`) reached
		// through an interface value — the shape ServeMux's tree uses for a
		// method-less child.
		ifaceShape := reflect.ValueOf(&struct {
			inner any
		}{inner: &struct {
			pattern *walkProbePattern
			str     string
		}{pattern: &walkProbePattern{str: probeStr}, str: probeStr}})
		got, unread := patternStrings(ifaceShape)
		if len(got) != 1 || got[0] != probeStr {
			t.Errorf("walking through an interface field read %v, want [%q] — the walk must see a "+
				"*pattern reached through an interface value, which is the shape ServeMux's own "+
				"tree uses for a method-less child", got, probeStr)
		}
		if len(unread) != 0 {
			t.Errorf("walking through an interface field reported %v", unread)
		}
	}()
}

// walkProbePattern is the shape the walk reads: a struct carrying `str`, the
// pattern as it was registered, reached through a `pattern` field that points at
// it. Named `walkProbe*` so it cannot be confused with production code.
type walkProbePattern struct{ str string }

// unlockTargetOf renders the page at path and returns the unlock path it would
// post to, resolved the way a browser resolves it (the page's own path plus the
// literal suffix the script appends).
func unlockTargetOf(t *testing.T, hnd http.Handler, host, path string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	m := gateFetchTargetRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("GET %s rendered no `fetch(location.pathname + '<suffix>')` gate — this guard "+
			"cannot tell what it would post to. body: %.300s", path, rec.Body.String())
	}
	return path + m[1], rec
}

// TestGateRouteProbeFillsWildcardsByPositionNotName pins the pattern parser the
// class guard's enumeration depends on. Two things are asserted, and both are
// the point of t_0da7d67f:
//
//   - the pattern grammar is read whole: an optional method (a method-less
//     pattern matches GET too, so skipping it would reopen the hole), an optional
//     host, `{$}` as the literal path "/", and a multi-segment wildcard filled
//     with one segment.
//   - substitution is POSITIONAL. A pattern filled with the same values yields
//     the same path whatever its wildcards are called, so a rename (`accslug` ->
//     `wsslug`, or anything -> `{formSlug}`) cannot change which path the guard
//     drives — which is how a renamed wildcard used to drop a gate route out of
//     the judged set while the guard stayed green.
func TestGateRouteProbeFillsWildcardsByPositionNotName(t *testing.T) {
	const ws, slug = "it-ws", "it-form"
	for _, tc := range []struct {
		name    string
		pat     string
		vals    []string
		want    gateRouteProbe
		wantHst string
		wantPth string
	}{
		{
			name:    "method and path",
			pat:     "GET /f/{slug}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET /f/{slug}", method: "GET", pathPat: "/f/{slug}", wildcard: []string{"slug"}},
			wantPth: "/f/" + slug,
		},
		{
			name:    "NO method still matches GET",
			pat:     "/qa/{slug}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "/qa/{slug}", pathPat: "/qa/{slug}", wildcard: []string{"slug"}},
			wantPth: "/qa/" + slug,
		},
		{
			// net/http has NO host wildcard: parsePattern refuses a '{' in the
			// host ("host contains '{' (missing initial '/'?)"), so this shape
			// cannot be a registered route. The probe must still read its host
			// section back VERBATIM — a guard that "filled" it with the workspace
			// slug would be driving a host net/http will not register.
			name:    "host section is taken as registered (no host wildcard exists)",
			pat:     "{ws}.jomform.com/{slug}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "{ws}.jomform.com/{slug}", hostPat: "{ws}.jomform.com", pathPat: "/{slug}", wildcard: []string{"slug"}},
			wantHst: "{ws}.jomform.com",
			wantPth: "/" + slug,
		},
		{
			name:    "host-qualified with a method",
			pat:     "GET example.com/{slug}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET example.com/{slug}", method: "GET", hostPat: "example.com", pathPat: "/{slug}", wildcard: []string{"slug"}},
			wantHst: "example.com",
			wantPth: "/" + slug,
		},
		{
			name:    "root wildcard consumes no value",
			pat:     "GET /{$}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET /{$}", method: "GET", pathPat: "/{$}"},
			wantPth: "/",
		},
		{
			name:    "multi-segment wildcard takes one segment",
			pat:     "GET /assets/{key...}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET /assets/{key...}", method: "GET", pathPat: "/assets/{key...}", wildcard: []string{"key..."}},
			wantPth: "/assets/" + slug,
		},
		{
			// The rename this card was reworked for: the wildcard is called
			// `formSlug`, and it is still driven at the fixture's form slug.
			name:    "a RENAMED wildcard is filled the same way",
			pat:     "GET /qa-renamed/{formSlug}",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET /qa-renamed/{formSlug}", method: "GET", pathPat: "/qa-renamed/{formSlug}", wildcard: []string{"formSlug"}},
			wantPth: "/qa-renamed/" + slug,
		},
		{
			name:    "two wildcards are filled in path order",
			pat:     "GET /embed/f/{wsslug}/{slug}",
			vals:    []string{ws, slug},
			want:    gateRouteProbe{pattern: "GET /embed/f/{wsslug}/{slug}", method: "GET", pathPat: "/embed/f/{wsslug}/{slug}", wildcard: []string{"wsslug", "slug"}},
			wantPth: "/embed/f/" + ws + "/" + slug,
		},
		{
			name:    "literal path",
			pat:     "GET /privacy",
			vals:    []string{slug},
			want:    gateRouteProbe{pattern: "GET /privacy", method: "GET", pathPat: "/privacy"},
			wantPth: "/privacy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := gateRouteProbeFor(tc.pat)
			if got.pattern != tc.want.pattern || got.method != tc.want.method ||
				got.hostPat != tc.want.hostPat || got.pathPat != tc.want.pathPat ||
				!reflect.DeepEqual(got.wildcard, tc.want.wildcard) {
				t.Fatalf("gateRouteProbeFor(%q) = %+v, want %+v", tc.pat, got, tc.want)
			}
			if pth := got.pathFor(tc.vals); pth != tc.wantPth {
				t.Errorf("gateRouteProbeFor(%q).pathFor(%v) = %q, want %q", tc.pat, tc.vals, pth, tc.wantPth)
			}
			if hst := got.hostFor(ws); hst != tc.wantHst {
				t.Errorf("gateRouteProbeFor(%q).hostFor(%q) = %q, want %q", tc.pat, ws, hst, tc.wantHst)
			}
		})
	}
}

// TestGateFillingsCoverEveryPosition pins the assignment generator: every
// wildcard position must be offered every fixture value, because that is the
// whole completeness argument for "this pattern is not a gate route" (see the
// file header). A generator that dropped a position would silently narrow the
// guard — the failure this card reworked.
func TestGateFillingsCoverEveryPosition(t *testing.T) {
	if got, want := gateFillings(2, []string{"slug", "ws"}),
		[][]string{{"slug", "slug"}, {"slug", "ws"}, {"ws", "slug"}, {"ws", "ws"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gateFillings(2, {slug,ws}) = %v, want %v", got, want)
	}
	if got, want := gateFillings(1, []string{"slug", "ws"}), [][]string{{"slug"}, {"ws"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gateFillings(1, {slug,ws}) = %v, want %v", got, want)
	}
	if got, want := gateFillings(0, []string{"slug", "ws"}), [][]string{{}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gateFillings(0, {slug,ws}) = %v, want one empty assignment", got)
	}
	// Past the cross-product budget the assignment COUNT drops, but every
	// position still gets every value, so the cover is never narrower in position
	// than before.
	degenerate := gateFillings(8, []string{"slug", "ws"})
	if len(degenerate) == 0 {
		t.Fatal("gateFillings(8, ...) returned no assignment at all — a pattern with that many wildcards would be undriveable")
	}
	for _, fill := range degenerate {
		if len(fill) != 8 {
			t.Fatalf("gateFillings(8, ...) produced an assignment of %d values, want 8", len(fill))
		}
		for i, v := range fill {
			if v != "slug" && v != "ws" {
				t.Fatalf("gateFillings(8, ...)[%d] = %q, want one of the fixture values", i, v)
			}
		}
	}
}

// TestEveryGateRouteHasItsOwnUnlockRoute is the class guard: for EVERY route
// that renders the private-form password gate, the address that gate would post
// to must be a registered route. The set of such routes is discovered from the
// mux itself and confirmed by request, never read off a list written here.
func TestEveryGateRouteHasItsOwnUnlockRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gate/unlock route-parity guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)
	host := ws + ".jomform.com"

	// ── enumerate: the mux's own route table, not a case list ────────────────
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	patterns := muxPatterns(t, mux)
	if len(patterns) < gatePatternFloor {
		t.Fatalf("walked only %d registered pattern(s) from PublicHandlers.RegisterRoutes "+
			"(expected at least %d) — the route-table walk is blind, so this guard would "+
			"cover whatever it happens to see. Patterns: %v", len(patterns), gatePatternFloor, patterns)
	}

	// The fixture owns exactly one workspace and one form, and these are the only
	// values a request can use to name them. They are applied BY POSITION, so the
	// name a pattern gave its wildcards never enters the decision.
	values := []string{slug, ws}

	type routeVerdict struct {
		pattern string
		handler string
		host    string
		path    string
	}
	var judged, unjudged []routeVerdict
	var slowest time.Duration
	var slowestProbe string

	// ── confirm each route by REQUEST: does it render the gate? ──────────────
	for _, pat := range patterns {
		pr := gateRouteProbeFor(pat)
		// A pattern with no method accepts GET too (net/http's own rule), so it
		// is a candidate; anything else registered for another method is not.
		if pr.method != "" && !strings.EqualFold(pr.method, http.MethodGet) {
			continue
		}
		fills := gateFillings(len(pr.wildcard), values)

		var (
			driven   bool
			hKey     string
			firstHst string
			firstPth string
			hitRec   *httptest.ResponseRecorder
			hitHst   string
			hitPth   string
		)
		triedPaths := make([]string, 0, len(fills))
		for _, fill := range fills {
			pth := pr.pathFor(fill)
			hst := pr.hostFor(ws)
			if hst == "" {
				hst = host
			}
			triedPaths = append(triedPaths, pth)
			_, matched := muxMatchFor(mux, hst, pth)
			if matched != pat {
				// This filling does not reach the pattern under test — it lands
				// on a different (more specific) route, or on nothing. It is not
				// evidence about THIS pattern, so it is not used as any.
				continue
			}
			driven = true
			if firstPth == "" {
				firstHst, firstPth = hst, pth
			}
			start := time.Now()
			rec := publicGet(t, hnd, hst, pth)
			elapsed := time.Since(start)
			if elapsed > slowest {
				slowest, slowestProbe = elapsed, pat+" -> "+pth
			}
			if hKey == "" {
				// Record the handler the MUX matched for this route, from the
				// bare mux (the full chain wraps handlers in middleware, so a
				// name read from it would not be comparable across patterns).
				h, _ := muxMatchFor(mux, hst, pth)
				hKey = handlerKey(h)
			}
			if !strings.Contains(rec.Body.String(), `id="pw"`) {
				continue // not a gate route at this filling
			}
			hitRec, hitHst, hitPth = rec, hst, pth
			break
		}

		if !driven {
			// The old walk `continue`d here, which cannot tell "not a gate route"
			// from "a gate route whose path this guard failed to fill". It is a
			// failure: the guard refuses to report on a route it could not drive,
			// rather than quietly declaring it not a gate route. A renamed
			// wildcard used to land exactly here.
			t.Errorf("could not drive the registered pattern %q: none of the %d path filling(s) %v "+
				"reached that pattern's OWN handler (the mux matched a different pattern, or "+
				"nothing). A route this guard cannot drive is a route it cannot judge, so it "+
				"fails here by name instead of being reported as \"not a gate route\".", pat, len(fills), triedPaths)
			continue
		}
		if hitRec == nil {
			unjudged = append(unjudged, routeVerdict{pattern: pat, handler: hKey, host: firstHst, path: firstPth})
			continue
		}
		// It renders the gate, so it MUST be a gate route this guard can judge.
		if !gateFetchTargetRe.MatchString(hitRec.Body.String()) {
			t.Errorf("GET %s (from the registered pattern %q) rendered the password gate but no "+
				"`fetch(location.pathname + '/unlock')` — this guard cannot tell what it would "+
				"post to, so the route is covered by nothing. body: %.300s", hitPth, pat, hitRec.Body.String())
			continue
		}
		judged = append(judged, routeVerdict{pattern: pat, handler: hKey, host: hitHst, path: hitPth})
	}

	t.Logf("slowest route probe: %s took %s (budget %s)", slowestProbe, slowest, gateRouteProbeBudget)
	if slowest > gateRouteProbeBudget {
		t.Fatalf("driving %s took %s, over the %s budget — this guard cannot judge a route it "+
			"cannot get an answer from, and a stalled route would hang the package instead of "+
			"failing here", slowestProbe, slowest, gateRouteProbeBudget)
	}

	// ── one handler, one verdict, on the same host ───────────────────────────
	// The gate is rendered (or not) by the handler CODE, so two patterns served
	// by the same code, reached on the SAME host, cannot have opposite verdicts.
	// A split means a filling missed the handler's form lookup — the way a
	// renamed wildcard used to make a gate route disappear from the judged set
	// while this test still reported green.
	//
	// The comparison is on the registered FUNCTION, not the value the mux hands
	// back: net/http wraps a registration with host scope, so the same function
	// reached through two different hosts is not comparable as a value. The host
	// is part of the key for the same reason — the same handler is SUPPOSED to
	// behave differently on a host that serves no such workspace (measured on QA
	// sabotage G, where a host-qualified pattern sharing handleWorkspaceRootForm
	// with /{slug} legitimately did not render the gate).
	type verdictCount struct{ gate, notGate int }
	byHandler := map[string]*verdictCount{}
	tally := func(v routeVerdict, isGate bool) {
		k := v.handler + " on " + v.host
		c := byHandler[k]
		if c == nil {
			c = &verdictCount{}
			byHandler[k] = c
		}
		if isGate {
			c.gate++
		} else {
			c.notGate++
		}
	}
	for _, v := range judged {
		tally(v, true)
	}
	for _, v := range unjudged {
		tally(v, false)
	}
	for k, c := range byHandler {
		if c.gate > 0 && c.notGate > 0 {
			t.Errorf("handler %s was classified BOTH as a gate-rendering route (%d pattern(s)) and "+
				"as not one (%d pattern(s)). One handler renders the gate for every pattern it "+
				"serves on a host, so this split means a probe did not reach that handler's form "+
				"lookup and a gate route is going unjudged.", k, c.gate, c.notGate)
		}
	}

	// ── a pattern this guard cannot reach is reported, never passed quietly ──
	// A host-qualified pattern is reachable only on ITS host, and the fixture's
	// form resolves only on the fixture's workspace host. So such a pattern
	// cannot be judged here: whether it renders the gate, and whether its unlock
	// exists, are both unknown. That is stated as a failure rather than left as a
	// silent "not a gate route", because silence is exactly the hole this card
	// exists to close (the #722 class: a page that renders a password box nobody
	// can satisfy). Today PublicHandlers.RegisterRoutes registers NO
	// host-qualified pattern, so this is a floor against the shape, not a
	// complaint about current code.
	for _, v := range unjudged {
		if v.host == host {
			continue
		}
		t.Errorf("the registered pattern %q is host-qualified (%s), and this fixture's workspace "+
			"is served on %s — so this guard cannot tell whether that route renders the password "+
			"gate, nor whether the gate's unlock exists. It is reported as UNCOVERED instead of "+
			"being passed off as \"not a gate route\". Either serve the fixture on that host or "+
			"extend this guard to reach it.", v.pattern, v.host, host)
	}

	if len(judged) < gateRouteDiscoveryMin {
		t.Fatalf("the walk found %d gate-rendering route(s), fewer than the %d measured to reach "+
			"renderPrivateGate — either the walk is blind or the fixture no longer renders the "+
			"gate, and this guard would be green on a set it does not cover. Found: %v",
			len(judged), gateRouteDiscoveryMin, judged)
	}
	names := make([]string, 0, len(judged))
	for _, gr := range judged {
		names = append(names, gr.pattern)
	}
	t.Logf("discovered %d gate-rendering route(s) among %d registered pattern(s): %s",
		len(judged), len(patterns), strings.Join(names, ", "))
	// Every other driven route is reported with the handler that decided it, so
	// "not a gate route" is a printed measurement rather than a silent skip.
	for _, v := range unjudged {
		t.Logf("  not a gate route: %-38s driven at %s%s (handler %s)", v.pattern, v.host, v.path, v.handler)
	}

	// ── each discovered route must have its own working unlock ──────────────
	for _, gr := range judged {
		gr := gr
		t.Run(gr.pattern, func(t *testing.T) {
			pth, probeHost := gr.path, gr.host
			target, rec := unlockTargetOf(t, hnd, probeHost, pth)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s -> %d, want 200 (this route is supposed to render the gate here). "+
					"body: %.300s", pth, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `id="pw"`) {
				t.Fatalf("GET %s did not render the password gate, so this case does not exercise "+
					"the path under test. body: %.300s", pth, rec.Body.String())
			}

			// The route exists iff the mux does not answer its own no-route
			// reply. A JSON error body means SOME handler matched (that is how
			// the canonical route answers a wrong Host, for instance); the
			// literal ServeMux text means nothing is registered there.
			res := publicPostJSON(t, hnd, probeHost, target, map[string]any{"password": gateFixturePassword})
			if res.Code == http.StatusNotFound && strings.TrimSpace(res.Body.String()) == "404 page not found" {
				t.Fatalf("the gate served at %s posts to %s, and that route DOES NOT EXIST — the "+
					"page renders a password box that can never be satisfied. Register the "+
					"matching POST route in RegisterRoutes (see embedUnlock / handleFormUnlock).",
					pth, target)
			}
			// A route that exists must also actually unlock with the right
			// password: present-but-broken is not better than absent.
			if res.Code != http.StatusOK {
				t.Errorf("POST %s (the gate's own target for %s) with the CORRECT password -> %d, "+
					"want 200. body=%s", target, pth, res.Code, res.Body.String())
			}
		})
	}
}

// TestGatePostsToItsOwnScopeNotTheCanonicalRoute pins the property the class
// guard relies on: the gate must derive its target from the page it is rendered
// on, never from a hardcoded canonical path. If this is ever changed back, the
// class guard above would be testing a different rule than production uses.
func TestGatePostsToItsOwnScopeNotTheCanonicalRoute(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gate target guard")
	}
	h, ws, slug := embedPrivateFixture(t)
	hnd := publicServer(h)

	// Render the SAME form through two different routes and read the target from
	// each page. Each target must sit under the route it was rendered on — that
	// is the whole point: one page, its own scope.
	embedPath := "/embed/f/" + ws + "/" + slug
	canonPath := "/" + slug
	for _, tc := range []struct{ path, want string }{
		{embedPath, embedPath + "/unlock"},
		{canonPath, canonPath + "/unlock"},
	} {
		target, _ := unlockTargetOf(t, hnd, ws+".jomform.com", tc.path)
		if target != tc.want {
			t.Errorf("the gate rendered at %s posts to %s, want %s — the gate must post to its OWN "+
				"scope, because an embed's Host is the merchant's domain and the canonical route "+
				"resolves the form from the Host", tc.path, target, tc.want)
		}
	}
}
