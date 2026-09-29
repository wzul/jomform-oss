package httpapi

// Card t_fc5cecbd — the CLASS guard for the unlock limiter.
// Card t_b2888b6c — enumeration is now the ROUTER'S, not the source text's.
// Card t_5c6413aa — the walk covers the SERVED table, not a hand-re-listed
// mirror of it.
//
// The behavioural guards in unlock_ratelimit_test.go prove the three CURRENT
// unlock routes are limited, but they enumerate those routes from a hand-written
// list, so a FOURTH `POST .../unlock` route added later would not be checked by
// them at all. That is the same shape of gap the board already had to fix once
// for the gate routes (the hand-written case list in
// TestEveryGateRouteHasItsOwnUnlockRoute): a guard that is only as wide as a
// human remembering to extend it is not the mechanical guard it claims to be.
//
// # WHY THE FIRST VERSION OF THIS GUARD WAS STILL THE SAME DEFECT (t_b2888b6c)
//
// This file used to read the package's own route registrations with ONE
// SINGLE-LINE TEXT REGEX
//
//	`mux\.(HandleFunc|Handle)\(\s*"[A-Z]+ +[^"]*/unlock"\s*,.*`
//
// and its header claimed a new unlock route "cannot be added unwrapped without
// failing here". That was false. A route whose pattern is passed through a
// VARIABLE OR A HELPER rather than written as a literal on a `mux.Handle` line
// was invisible to the regex, so the guard reported GREEN while an unlimited
// password oracle was live. MEASURED on the revision this file was reworked from
// (public.go, compiling — `go build ./...` rc=0):
//
//	registerUnlockRoute(mux, "POST /qa-indirect/{slug}/unlock", h.handleFormUnlock)
//	...
//	func registerUnlockRoute(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
//		mux.HandleFunc(pattern, h)
//	}
//
//	--- PASS: TestEveryUnlockRouteIsWrappedAtRegistration (0.06s)
//	      3 `.../unlock` registration(s) scanned, all RateLimit-wrapped
//
// Three literal routes scanned, a FOURTH live and unwrapped, guard green. For
// contrast the same guard DID bite on a literal injected route, which is exactly
// why the hole stayed hidden: every sabotage anyone tried used the shape the
// guard was written against. A text scan is only ever as wide as the SPELLINGS
// its author imagined, and a spelling is a private detail of the call site —
// the same failure as t_020146e6's hand-written list, one layer down. That
// header also said the point is to follow the code instead of a copy of it; a
// regex over call-site text IS a copy of it.
//
// # WHAT ENUMERATION IS NOW, AND WHY IT CANNOT HAVE A BLIND SPOT OF THAT SHAPE
//
// The route set comes from the table the SERVER ACTUALLY SERVES — not from a
// hand-written mirror of its registrations. Server.routes() (server.go) builds
// the one *http.ServeMux that Server.Handler() wraps, and Handler() is nothing
// but middleware around that object; the guard calls routes() and walks what it
// returns. muxPatterns() (gate_unlock_route_test.go) walks net/http's OWN
// routing tree, so it returns the patterns net/http actually holds — the same
// table a live request is dispatched from. A pattern that reaches that table is
// enumerated, whatever spelling put it there: a literal, a package constant, a
// `+` concatenation, a helper, another file in the package, a future
// registration API. There is no "indirect shape" for this walk, because the walk
// never sees a shape — only the result. That is the property the regex lacked
// and cannot get.
//
// # WHY THE TABLE IS REACHED THROUGH A SEAM RATHER THAN UNWRAPPED
//
// Server.Handler() returns `RequestID(Logger(recoverer(Secure(RealIP(
// AliasHostRedirect(mux)))))` — a tower of closure-based middlewares whose inner
// handler is not reachable by reflection without reading net/http's internals.
// So the mux is exposed to this package as Server.routes(), and two tests pin
// that the seam cannot lie:
//
//   - TestServerHandlerServesFromTheTableTheGuardWalks adds a route to
//     routes() AFTER Handler() was built and requires the served chain to
//     answer it. A Handler() that kept its own copy of the table (the shape
//     before this seam: every registration written inline in Handler()) fails
//     that by name.
//   - TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters requires every
//     route group the server can register to be WIRED in the guard's own server
//     or named in an allowlist with a reason. A group registered on the served
//     table but absent from the guard's table would put its routes outside the
//     walk, which is the defect class this file exists to close — one layer
//     down, so it is closed mechanically too.
//
// The seam is a method on *Server that the production server also uses, not a
// test-only copy: there is exactly one route table, and it is the one served.
//
// # WHY THE VERDICT IS BEHAVIOURAL
//
// Enumerating the router is not enough on its own. A registration can be reached
// through the mux and still be unlimited — that is the defect class this whole
// file exists to close — so each enumerated pattern is DRIVEN: a concurrent
// burst of wrong passwords from ONE address must produce a 429, exactly as the
// /order route beside it does. A route that reaches its handler twelve times
// straight is an unthrottled password oracle and fails by name, whether or not
// `middleware.RateLimit` appears anywhere near its registration.
//
// The burst is CONCURRENT for the reason unlock_ratelimit_test.go documents:
// middleware.RateLimit consumes the token before the handler runs, and an unlock
// then spends ~134ms in argon2id, which is larger than the 1-token/second
// refill, so a SEQUENTIAL loop never drains burst 10. Firing them together is
// also what a brute-force burst actually looks like, so the assertion matches
// the attack rather than a wall-clock ordinal.
//
// # WHAT IT DOES NOT COVER (stated so the guard is not over-claimed)
//
//   - A limiter applied by the HANDLER rather than by a wrapper around it passes,
//     because the burst then reaches the handler and the handler answers 429 —
//     a correct limiter, and this guard is deliberately indifferent to WHERE the
//     limit lives. It tests the OUTCOME of the route-level wrapper this repo
//     uses (the /order precedent), not its spelling.
//   - A limiter keyed on something other than middleware.ClientIP would still
//     refuse the tail of a burst, so it passes here while allowing a different
//     abuse. The per-buyer KEY is asserted separately and behaviourally in
//     unlock_ratelimit_test.go (TestUnlockRateLimitIsKeyedPerBuyer); it is not
//     re-asserted here.
//   - A limiter with a burst BELOW this guard's burst refuses a request early.
//     That is a legitimate limiter and this guard reports it green; the threshold
//     on the routes that matter (burst 10) is pinned by
//     TestUnlockRateLimitSparesTheFirstBuyers.
//   - It judges `.../unlock` PATTERNS only. An unlock route spelled without that
//     suffix is not recognised as one — but the gate's rendered script posts to
//     `location.pathname + '/unlock'` (asserted by
//     TestGatePostsToItsOwnScopeNotTheCanonicalRoute), so an unlock a buyer can
//     actually reach does carry the suffix.
//   - A HOST-QUALIFIED unlock pattern is reported UNDRIVABLE rather than judged:
//     the probe drives every pattern on one Host, and a pattern pinned to another
//     literal host cannot be reached that way (net/http has no host wildcard —
//     pinned by TestServeMuxRejectsAWildcardHost). That is a failure by name, not
//     a silent skip, and it is the right answer: this repo's unlock routes carry
//     no host section (the workspace host is resolved inside the handler), so such
//     a pattern is a new shape someone should look at.
//   - The guard's server wires the groups that need NO database, NO Valkey and
//     NO network (REST, Public, Payments). The two groups it cannot wire (auth,
//     OAuth) are named in unlockClassUnwiredRouteGroups with the reason, and
//     TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters fails if the
//     server gains a group that is neither wired here nor named there — so this
//     omission is checked rather than trusted.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/config"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

// unlockRouteSuffix is the pattern tail that identifies a private-form unlock
// route. It is the suffix the gate's own script appends to the page it was
// served on (`location.pathname + '/unlock'`), which is what makes an unlock
// reachable by a buyer at all.
const unlockRouteSuffix = "/unlock"

// unlockClassProbeBurst is the burst this guard fires at each enumerated route.
// It matches the attack shape unlock_ratelimit_test.go uses (12) and is ABOVE the
// limiter's own burst (10), so a correctly wrapped route must answer 429 and an
// unwrapped one must not.
const unlockClassProbeBurst = 12

// unlockRouteDiscoveryMin is the floor on the discovered unlock set. Three routes
// are measured to exist on this tree; a walk that finds fewer has either lost the
// route table (a Go release reshaped the internals muxPatterns reads — see
// TestGateRouteWalkerSeesEveryRegisteredPattern) or lost the routes themselves,
// and must fail rather than report green on a set narrower than the guard claims.
//
//	POST /{slug}/unlock                    host scope
//	POST /f/{slug}/unlock                  host scope
//	POST /embed/f/{wsslug}/{slug}/unlock   path scope
const unlockRouteDiscoveryMin = 3

// unlockRoutePatternFloor is the floor on the WALK itself: the served table
// registers ~190 patterns across the three handler groups this guard wires (REST,
// Public, Payments; the auth and OAuth groups are not wired — see
// unlockClassRouteGroups), so a walk returning a handful means the reflection is
// not seeing the routing tree, which would make the unlock floor above
// meaningless in the other direction. Deliberately loose — it exists to catch a
// blind walk, not to pin the route count.
const unlockRoutePatternFloor = 50

// unlockProbeHost is the Host the probe sends on every request: the host a
// workspace at slug "jf-probe-ws" would be served on. Host-scoped patterns are
// matched against it, so it is part of aiming at a route (see unlockProbeFilling).
const unlockProbeHost = "jf-probe-ws.jomform.com"

// unlockProbeValues are the identifier values a pattern's wildcards are filled
// with, POSITIONALLY (never by name — see the t_0da7d67f note in
// gate_unlock_route_test.go: name-keyed substitution is how a rename dropped a
// route out of the judged set).
var unlockProbeValues = []string{"jf-probe-form", "jf-probe-ws"}

// unlockClassRouteGroups names every route GROUP the served table is built
// from, and says whether this guard WIRES it. The point is that the guard's own
// server omits two groups (they need a database and a Valkey client to exist at
// all) and that omission must FAIL when the server gains a group nobody
// classified — a group on the served table but outside this guard's table would
// hold routes outside the walk, which is the same defect class one layer down:
// a route set narrower than the guard claims, reported green.
//
// TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters reads server.go's
// routes() and requires every `X.RegisterRoutes(mux)` receiver there to appear
// here, and every group marked wired to have its handler NON-NIL on the very
// server this guard walks. Adding a group to the server therefore cannot land
// without either wiring it here or writing down why not.
var unlockClassRouteGroups = []struct {
	receiver string // the receiver as routes() spells it
	reason   string // non-empty: deliberately NOT wired, and this is why
	wired    func(*Server) bool
}{
	{
		receiver: "s.auth",
		reason: "AuthHandlers only exists when deps.Users AND deps.Sessions are wired, " +
			"and Sessions needs a Valkey client. No `/unlock` route lives in this group " +
			"(it is /v1/auth/*), so wiring it would add the walk's only external " +
			"dependency for no unlock coverage.",
	},
	{receiver: "s.deps.REST", wired: func(s *Server) bool { return s.deps.REST != nil }},
	{receiver: "s.deps.Public", wired: func(s *Server) bool { return s.deps.Public != nil }},
	{receiver: "s.deps.Payments", wired: func(s *Server) bool { return s.deps.Payments != nil }},
	{
		receiver: "s.oauth",
		reason: "OAuthHandlers is built alongside AuthHandlers and shares its dependency " +
			"surface (sessions + users). Its routes are /oauth/*, none of them an unlock.",
	},
}

// servedMux returns the ONE route table the server serves, built by
// Server.routes() — the same object Server.Handler() wraps. Nothing in this file
// re-lists registrations: the walk's subject is the served table itself, so a
// route added to routes() (inline or through a group) is enumerated whatever
// spelling put it there.
//
// The handler groups are BARE (zero-value) on purpose: this guard asks the mux
// which PATTERNS it holds and then measures the LIMITER, and
// middleware.RateLimit consumes its token before the wrapped handler ever runs —
// so the burst's throttle is observable with no database, no Valkey and no
// session. MEASURED on this tree: all three unlock routes answer 404 x10 + 429 x2
// to a 12-burst and 404 x10 (no 429) to a 10-burst against this bare table, in
// three successive rounds each. A store a bare handler does not have is
// nil-guarded throughout this package (it answers 404 for "no such form" rather
// than dereferencing), which is what keeps the probe's non-429 traffic a real
// answer rather than a recovered panic.
func servedMux() *Server {
	srv := NewServer(&config.Config{
		Port:        "0",
		DatabaseURL: "postgres://localhost/jomform_test",
		AppBaseURL:  "http://localhost:8080",
		Env:         "dev",
	}, testLogger(), &Deps{
		REST:     &RESTHandlers{},
		Public:   &PublicHandlers{},
		Payments: &PaymentsHandlers{},
	})
	// Build the HANDLER before handing the server back, so the table callers
	// walk is the one the PRODUCTION path produces. The route table is memoised
	// on the receiver (sync.Once → routesMux, server.go), and Handler() is what
	// production calls first (api/cmd/jomform-api/main.go) — Handler() takes
	// routes(), then registers inline on that same object. A walk that called
	// routes() alone fired the Once WITHOUT running those registrations, so it
	// cached the pre-handler table and an inline, unwrapped `.../unlock` route
	// was served, unlimited, and absent from the walk (MEASURED: 193 patterns,
	// route absent, guard GREEN; with Handler() first: 194 patterns, route
	// present, 403 x12, i.e. the guard names it RED — card t_48fd3de2).
	// Calling Handler() here is what keeps the guard's subject the table that is
	// actually served; TestUnlockClassGuardWalkTakesTheProductionPath pins it.
	_ = srv.Handler()
	return srv
}

// unlockPatternFillings returns the concrete paths to try for a pattern. The
// caller resolves each one against the mux and keeps the first that reaches the
// pattern under test, so a filling that lands on a SIBLING route is discarded
// rather than measured as if it were this route.
func unlockPatternFillings(pr gateRouteProbe) []string {
	fills := gateFillings(len(pr.wildcard), unlockProbeValues)
	out := make([]string, 0, len(fills))
	for _, fill := range fills {
		out = append(out, pr.pathFor(fill))
	}
	return out
}

// unlockRouteMatches reports whether a registered pattern is an unlock this guard
// judges: a pattern reachable by POST (no method token at all counts — net/http
// matches a method-less pattern for every method) whose path ends in the suffix
// the gate appends. No pattern spelling is consulted beyond that, which is the
// whole difference between this guard and the regex it replaces.
func unlockRouteMatches(pat string) bool {
	pr := gateRouteProbeFor(pat)
	if pr.method != "" && !strings.EqualFold(pr.method, http.MethodPost) {
		return false
	}
	return strings.HasSuffix(pr.pathPat, unlockRouteSuffix)
}

// resolveUnlockProbePath finds a concrete path that the mux itself dispatches to
// pat under host. It returns "" when no positional filling reaches that pattern —
// the caller reports that as undrivable rather than skipping it.
func resolveUnlockProbePath(mux *http.ServeMux, pat, host string) (string, string) {
	pr := gateRouteProbeFor(pat)
	for _, pth := range unlockPatternFillings(pr) {
		req := httptest.NewRequest(http.MethodPost, pth, nil)
		req.Host = host
		h, got := mux.Handler(req)
		if got == pat {
			return pth, handlerKey(h)
		}
	}
	return "", ""
}

// unlockBurstCodes fires n concurrent POSTs at probePath from ONE address and
// returns the status codes observed, in any order. No *testing.T: it runs from
// goroutines.
//
// middleware.ClientIP reads CF-Connecting-IP first (else X-Real-IP, else the
// socket peer), so setting it is how the probe plays "these requests came from
// one address".
func unlockBurstCodes(hnd http.Handler, host, probePath, ip string, n int) []int {
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, probePath, strings.NewReader(`{"password":"not-it"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("CF-Connecting-IP", ip)
			req.Host = host
			rec := httptest.NewRecorder()
			<-start // released together: the burst must land inside one refill window
			hnd.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()
	return codes
}

// countStatus counts one status code in a burst.
func countStatus(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

// unlockBurstSummary renders a burst's status codes, so a red is readable
// without re-running the probe.
func unlockBurstSummary(codes []int) string {
	seen := map[int]int{}
	var order []int
	for _, c := range codes {
		if _, ok := seen[c]; !ok {
			order = append(order, c)
		}
		seen[c]++
	}
	var b strings.Builder
	for _, c := range order {
		fmt.Fprintf(&b, "%d x%d  ", c, seen[c])
	}
	return strings.TrimSpace(b.String())
}

// TestEveryUnlockRouteIsLimitedAtTheRouter is the mechanical class guard: every
// `.../unlock` pattern the SERVED table holds must answer a burst with a 429 —
// i.e. be limited — no matter how its registration was spelled or which file
// wrote it.
//
// It replaces a single-line text regex over `mux.Handle` call sites, which
// reported GREEN on a live, unwrapped, indirectly-registered unlock route
// (measured; see the file header). This guard has no shape to miss: it walks the
// table the server actually serves and judges that table's behaviour.
func TestEveryUnlockRouteIsLimitedAtTheRouter(t *testing.T) {
	srv := servedMux()
	mux := srv.routes()
	patterns := muxPatterns(t, mux)
	if len(patterns) < unlockRoutePatternFloor {
		t.Fatalf("walked only %d registered pattern(s) from the server's served table (expected at "+
			"least %d) — the route-table walk is blind, so this guard would cover only whatever it "+
			"happens to see instead of every registered route. Patterns: %v",
			len(patterns), unlockRoutePatternFloor, patterns)
	}

	type unlockRoute struct {
		pattern string
		handler string
		path    string
	}

	var found []unlockRoute
	var undrivable []string

	for _, pat := range patterns {
		if !unlockRouteMatches(pat) {
			continue
		}
		probePath, hKey := resolveUnlockProbePath(mux, pat, unlockProbeHost)
		if probePath == "" {
			// A pattern this guard cannot drive is a pattern it cannot judge.
			// Failing by name here is the point: silently skipping it is how a
			// route stops being covered without anything going red.
			undrivable = append(undrivable, pat)
			continue
		}
		found = append(found, unlockRoute{pattern: pat, handler: hKey, path: probePath})
	}

	if len(undrivable) > 0 {
		t.Errorf("%d `...%s` pattern(s) are registered but could not be driven on host %s: %v — a "+
			"pattern this guard cannot reach is a pattern it cannot judge, so it is reported here by "+
			"name instead of being dropped from the covered set.",
			len(undrivable), unlockRouteSuffix, unlockProbeHost, undrivable)
	}

	// Non-vacuity: the routes this guard is about must be IN the enumeration, or
	// the walk stopped seeing them and a green here would prove nothing.
	if len(found) < unlockRouteDiscoveryMin {
		t.Fatalf("found only %d `...%s` pattern(s) in the router — fewer than the %d this guard is "+
			"required to cover, so either the walk is blind or the routes are gone, and a green here "+
			"would be about a set the guard does not cover. Found: %v",
			len(found), unlockRouteSuffix, unlockRouteDiscoveryMin, found)
	}

	// ── drive each one: an unlock is a PASSWORD ORACLE and must be limited ──
	var unlimited []string
	var lines []string
	for _, u := range found {
		ip := uniqueTestIP()
		codes := unlockBurstCodes(mux, unlockProbeHost, u.path, ip, unlockClassProbeBurst)
		summary := unlockBurstSummary(codes)
		lines = append(lines, fmt.Sprintf("%-42s %-44s -> %s", u.pattern, u.path, summary))
		if countStatus(codes, http.StatusTooManyRequests) == 0 {
			unlimited = append(unlimited, fmt.Sprintf("%s  (driven at %s, handler %s) -> %s",
				u.pattern, u.path, u.handler, summary))
		}
	}

	t.Logf("%d `...%s` pattern(s) enumerated from the router and driven with a burst of %d from "+
		"one address:\n  %s", len(found), unlockRouteSuffix, unlockClassProbeBurst, strings.Join(lines, "\n  "))

	if len(unlimited) > 0 {
		t.Fatalf("%d `...%s` route(s) are registered WITHOUT a limiter: a burst of %d wrong "+
			"passwords from one address produced NO 429, so each is an unlimited online guessing "+
			"surface. An unlock runs an argon2id check against the form's real access hash and "+
			"answers 403/200, so it must carry the same limiter as the /order route beside it:\n"+
			"  mux.Handle(\"POST <pattern>\", middleware.RateLimit(http.HandlerFunc(h.<handler>)))\n"+
			"Offending route(s):\n  %s",
			len(unlimited), unlockRouteSuffix, unlockClassProbeBurst, strings.Join(unlimited, "\n  "))
	}
}

// TestUnlockClassGuardSeesARouteRegisteredIndirectly pins the property this card
// exists for, at the cause: a route registered through a HELPER — no literal
// `mux.Handle` with a "/unlock" pattern anywhere — must be enumerated by the walk
// this guard uses, and must be judged UNLIMITED when it is not wrapped.
//
// It builds its OWN mux, so it proves the INSTRUMENT reaches the indirect shape
// without depending on the shipped routes staying a particular size — including
// that a future re-narrowing of the guard back to a source scan (which this tree
// would satisfy: three literal routes, all wrapped) cannot go unnoticed.
func TestUnlockClassGuardSeesARouteRegisteredIndirectly(t *testing.T) {
	// The indirection that defeated the text regex: the pattern is a VARIABLE at
	// the call site, and the actual registration lives in a helper.
	registerIndirect := func(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, h)
	}

	for _, tc := range []struct {
		name      string
		wrapped   bool
		wantLimit bool
	}{
		{"unwrapped indirect route is judged UNLIMITED", false, false},
		{"wrapped indirect route is judged limited", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noop := func(http.ResponseWriter, *http.Request) {}
			mux := http.NewServeMux()
			if tc.wrapped {
				registerIndirect(mux, "POST /qa-indirect/{slug}/unlock", unlockedHandler(noop))
			} else {
				registerIndirect(mux, "POST /qa-indirect/{slug}/unlock", noop)
			}
			// A sibling that is NOT an unlock, so the recogniser is exercised too.
			registerIndirect(mux, "POST /qa-indirect/{slug}/order", noop)

			var seen []string
			for _, p := range muxPatterns(t, mux) {
				if unlockRouteMatches(p) {
					seen = append(seen, p)
				}
			}
			if !contains(seen, "POST /qa-indirect/{slug}/unlock") {
				t.Fatalf("the walk did not enumerate the INDIRECTLY registered route %q (it saw %v). "+
					"That is the card's defect: a registration the guard cannot see is a route it "+
					"cannot judge, and the guard reports green while the route ships unwrapped.",
					"POST /qa-indirect/{slug}/unlock", seen)
			}
			if contains(seen, "POST /qa-indirect/{slug}/order") {
				t.Fatalf("the recogniser also claimed the sibling %q as an unlock (%v) — the "+
					"recognition rule is not the `%s` suffix",
					"POST /qa-indirect/{slug}/order", seen, unlockRouteSuffix)
			}

			codes := unlockBurstCodes(mux, unlockProbeHost, "/qa-indirect/jf-probe-form/unlock",
				uniqueTestIP(), unlockClassProbeBurst)
			throttled := countStatus(codes, http.StatusTooManyRequests)
			if tc.wantLimit && throttled == 0 {
				t.Fatalf("a WRAPPED indirect route was not limited by the probe (%s) — the probe is "+
					"not measuring the limiter, so a green in the class guard would mean nothing",
					unlockBurstSummary(codes))
			}
			if !tc.wantLimit && throttled > 0 {
				t.Fatalf("an UNWRAPPED indirect route answered 429 (%s) — something other than the "+
					"limiter produces 429 here, which would make the class guard green on an unlimited "+
					"route", unlockBurstSummary(codes))
			}
		})
	}
}

// unlockedHandler wraps a handler in the same wrapper the production unlock
// routes use, so the sabotage test above can build its own mux without depending
// on a production registration staying in place.
func unlockedHandler(h http.HandlerFunc) http.HandlerFunc {
	limited := middleware.RateLimit(http.HandlerFunc(h))
	return func(w http.ResponseWriter, r *http.Request) { limited.ServeHTTP(w, r) }
}

// TestServerHandlerServesFromTheTableTheGuardWalks pins the SEAM the whole class
// guard stands on: Server.Handler() must dispatch from the very table
// Server.routes() returns, not from a private copy of it.
//
// It is written at the cause, not by reading server.go: it builds the server,
// takes the served table, then ADDS A ROUTE to that same table and requires the
// handler Handler() ALREADY returned to answer it. A Handler() that built its own
// mux (the shape before this seam — every registration inline in Handler()) has a
// table this route is absent from, and this fails naming the route.
//
// This is what lets the class guard walk a mux instead of a hand-written mirror
// of the registrations: if the seam ever stops being the served table, this goes
// RED while the class guard would otherwise be judging a table nobody serves.
func TestServerHandlerServesFromTheTableTheGuardWalks(t *testing.T) {
	srv := servedMux()
	hnd := srv.Handler() // built BEFORE the route below exists
	table := srv.routes()

	const (
		pattern = "POST /qa-seam/{slug}/unlock"
		path    = "/qa-seam/jf-probe-form/unlock"
	)
	table.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	// Sanity: the route is really on the table the guard would walk.
	if !contains(muxPatterns(t, table), pattern) {
		t.Fatalf("the route %q added to Server.routes() is not on the table it returned — the walk "+
			"and the table disagree, so this test cannot say anything about the seam", pattern)
	}

	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Host = unlockProbeHost
	rec := httptest.NewRecorder()
	hnd.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST %s (on the served table as %q) answered %d through Server.Handler(), want 403 "+
			"from the handler registered on Server.routes(). Server.Handler() is not dispatching from "+
			"the table routes() returns — it is serving a PRIVATE copy, so every guard that walks "+
			"routes() (the unlock class guard among them) is judging a table nobody serves. That is "+
			"exactly how an inline registration stayed outside the guard while it reported green.",
			path, pattern, rec.Code)
	}
	t.Logf("seam holds: a route added to Server.routes() after Handler() was built is answered by "+
		"Handler() (POST %s -> %d)", path, rec.Code)
}

// serverHandlerBody returns the source lines of Server.Handler() and the line
// number its signature sits on, read from server.go. It is the ONE body the guard
// below inspects — whether a route registration may live in it — and a parse that
// found nothing FAILS rather than reading an empty body (a scan of "" finds no
// registration, which is how such a guard reports green on anything).
func serverHandlerBody(t *testing.T) (body []string, firstLine int) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(rootDir, "api", "internal", "httpapi", "server.go"))
	if err != nil {
		t.Fatalf("read server.go: %v — the guard below reads Handler()'s own body, so it can say "+
			"nothing without this file", err)
	}
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func (s *Server) Handler() http.Handler {") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("server.go has no `func (s *Server) Handler() http.Handler {` line — the function " +
			"was renamed or moved, so a scan of its body would read nothing and this guard would " +
			"pass vacuously. Re-point it before trusting either check here")
	}
	// net/http's own body ends at the first line that is exactly `}` at column 0
	// — gofmt guarantees that for a top-level func. Taking the first such line is
	// what stops the slice wandering into the next function.
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			return lines[start : i+1], start + 1
		}
	}
	t.Fatalf("Handler() at server.go:%d never closes at column 0 — the body could not be delimited, "+
		"so this guard cannot say which registrations are inside it", start+1)
	return nil, 0
}

// routeRegistrationRe matches the call shapes that REGISTER a route on a
// *http.ServeMux, so a registration in Handler()'s body is caught whatever
// spelling put it there — the literal call, a method value, or a group's
// RegisterRoutes. The pattern is deliberately loose (any `Handle`, `HandleFunc`
// or `RegisterRoutes` receiver): a false positive on Handler()'s body is a
// prompt to look, and the body has no reason to call any of them.
var routeRegistrationRe = regexp.MustCompile(`(?m)\.(HandleFunc|Handle|RegisterRoutes)\s*\(`)

// TestServerHandlerRegistersNoRouteOutsideRoutes enforces the rule
// Server.routes()'s doc comment states: registrations belong in routes(), never
// inline in Handler().
//
// The rule matters because Handler() registers onto the SAME mux routes()
// returns, but only when Handler() RUNS. A route registered inline there is
// therefore invisible to a walker that took routes() without building the
// handler (the t_48fd3de2 defect: 193 patterns, the route absent, guard GREEN)
// while it is live and unlimited on the served chain. Two things close that: the
// walk was moved to production's order
// (TestUnlockClassGuardWalkTakesTheProductionPath), and this fails BY NAME if
// the shape ever returns — including through a helper the registration scan
// cannot see, because it rejects the whole CALL SHAPE rather than a pattern
// literal.
func TestServerHandlerRegistersNoRouteOutsideRoutes(t *testing.T) {
	body, first := serverHandlerBody(t)

	var offenders []string
	for i, l := range body {
		code := l
		if j := strings.Index(code, "//"); j >= 0 {
			code = code[:j] // a comment naming mux.Handle is not a registration
		}
		if routeRegistrationRe.MatchString(code) {
			offenders = append(offenders, fmt.Sprintf("server.go:%d: %s", first+i, strings.TrimSpace(l)))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("Server.Handler() registers %d route(s) on the mux:\n  %s\n"+
			"Registrations belong in Server.routes(), never inline in Handler(): Handler() runs "+
			"AFTER routes() returns, so anything registered here is on the served table but absent "+
			"from a walk that takes routes() without building the handler — live and unlimited "+
			"while the unlock class guard reports green (card t_48fd3de2). Move the registration "+
			"into routes().", len(offenders), strings.Join(offenders, "\n  "))
	}
	t.Logf("Handler() body: %d line(s) from server.go:%d, no route registration", len(body), first)
}

// TestUnlockClassGuardWalkTakesTheProductionPath pins that the guard walks the
// table via PRODUCTION'S ORDER: servedMux() must build Handler() before it hands
// the server to a walker.
//
// Why it is source-read rather than behavioural: the defect it guards is a
// MISSING call in a test helper, and the only way to exhibit it behaviourally is
// to put a registration inline in Handler() — which
// TestServerHandlerRegistersNoRouteOutsideRoutes forbids precisely so that shape
// cannot ship. With no such registration on the tree, a behavioural probe cannot
// tell the two call orders apart (both yield the same table), so the honest pin
// is: the instrument takes the production order, and the behavioural proof of
// the order's necessity is the harness case F2
// (api/scripts/biteproof_unlock_class_guard.py), which injects the inline
// unwrapped route and requires the guard RED naming it.
//
// This is the same shape as TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters,
// one layer in: that guard reads the SERVER's source to check the guard's table
// is wide enough, this one reads the GUARD's source to check the table it walks
// is built the way production builds it.
func TestUnlockClassGuardWalkTakesTheProductionPath(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(rootDir, "api", "internal", "httpapi",
		"unlock_ratelimit_class_test.go"))
	if err != nil {
		t.Fatalf("read the class guard's own source: %v — this check is about how that walk builds "+
			"its table, and it cannot say anything without the file", err)
	}
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func servedMux() *Server {") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the class guard has no `func servedMux() *Server {` — the walk's server was renamed " +
			"or inlined, so this guard would read nothing and pass vacuously. Re-point it at whatever " +
			"builds the walked table")
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			end = i
			break
		}
	}
	if end < 0 {
		t.Fatalf("servedMux() at line %d never closes at column 0 — its body could not be delimited, "+
			"so this guard cannot say whether the handler is built before the table is walked", start+1)
	}
	body := lines[start : end+1]

	handlerCall, returnAt := -1, -1
	for i, l := range body {
		code := l
		if j := strings.Index(code, "//"); j >= 0 {
			code = code[:j]
		}
		if strings.Contains(code, ".Handler()") && handlerCall < 0 {
			handlerCall = i
		}
		if strings.HasPrefix(strings.TrimSpace(code), "return ") && returnAt < 0 {
			returnAt = i
		}
	}
	if handlerCall < 0 {
		t.Fatalf("servedMux() (line %d) never calls Handler(): its table is built by routes() alone, "+
			"which fires the memoising sync.Once BEFORE Handler()'s own registrations run — so an "+
			"unwrapped route registered inline in Handler() is served, unlimited, and ABSENT from this "+
			"walk while the class guard reports green (MEASURED, card t_48fd3de2: 193 patterns and 3 "+
			"unlock routes here vs 194 and 4 with Handler() built first). Build the handler first: "+
			"production does (api/cmd/jomform-api/main.go calls Handler()), so the walk must too.",
			start+1)
	}
	if returnAt >= 0 && handlerCall > returnAt {
		t.Fatalf("servedMux() calls Handler() AFTER its `return` (Handler at body line %d, return at "+
			"%d) — the table is handed to the walker before the handler is built, which is the same "+
			"hole as not calling it at all", handlerCall, returnAt)
	}
	t.Logf("servedMux() builds Handler() at body line %d, before returning (body lines %d..%d of "+
		"the class guard)", handlerCall, start+1, end+1)
}

// TestUnlockRoutesAreLimitedThroughTheServedChain is the second direction of the
// same property: the shipped unlock routes must be limited through the FULL
// server chain, not only on the bare table — and they must be the routes the
// guard's own enumeration found. It drives Server.Handler() (the real middleware
// stack: RealIP → Secure → recoverer → Logger → RequestID → the served table).
func TestUnlockRoutesAreLimitedThroughTheServedChain(t *testing.T) {
	srv := servedMux()
	hnd := srv.Handler()

	known := []struct{ pattern, path string }{
		{"POST /{slug}/unlock", "/jf-probe-form/unlock"},
		{"POST /f/{slug}/unlock", "/f/jf-probe-form/unlock"},
		{"POST /embed/f/{wsslug}/{slug}/unlock", "/embed/f/jf-probe-ws/jf-probe-form/unlock"},
	}

	// The guard's own enumeration must carry these names, so a rename cannot
	// shrink the judged set while this test still passes on the old paths.
	var enumerated []string
	for _, p := range muxPatterns(t, srv.routes()) {
		if unlockRouteMatches(p) {
			enumerated = append(enumerated, p)
		}
	}

	for _, k := range known {
		if !contains(enumerated, k.pattern) {
			t.Errorf("the class guard's enumeration does not carry %q (it has %v) — an unlock route "+
				"registered outside the served table is outside the guard.",
				k.pattern, enumerated)
			continue
		}
		// A bare PublicHandlers resolves no form, so the route answers 404 — what
		// this asserts is the LIMITER, which runs before the handler either way.
		burst := unlockBurstCodes(hnd, unlockProbeHost, k.path, uniqueTestIP(), unlockClassProbeBurst)
		if countStatus(burst, http.StatusTooManyRequests) == 0 {
			t.Errorf("POST %s through the served chain (%s): a burst of %d from one address produced "+
				"NO 429 — the route is unthrottled on the server, whatever the bare table shows. codes: %s",
				k.path, k.pattern, unlockClassProbeBurst, unlockBurstSummary(burst))
		}
	}

	// And the threshold: exactly the burst must be served, so the limiter is not
	// tighter on the served chain than the /order precedent (this is the same
	// property TestUnlockRateLimitSparesTheFirstBuyers pins behaviourally with a
	// real form; here it pins that the bare-table numbers above are the same shape).
	codes := unlockBurstCodes(hnd, unlockProbeHost, "/jf-probe-form/unlock", uniqueTestIP(), 10)
	if n := countStatus(codes, http.StatusTooManyRequests); n > 0 {
		t.Errorf("POST /jf-probe-form/unlock through the served chain: %d of the first 10 requests "+
			"from one address were throttled (%s) — tighter than the burst-10 precedent",
			n, unlockBurstSummary(codes))
	}
	t.Logf("served chain: %d `...%s` route(s) enumerated, all three limited through the full stack",
		len(enumerated), unlockRouteSuffix)
}

// TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters pins the guard's own
// INSTRUMENT against the defect this card is about, one layer down.
//
// The guard walks Server.routes() of ITS OWN server — which cannot wire the auth
// or OAuth groups (both need a Valkey client to exist at all). So the guard's
// table is narrower than production's by two groups, and that is a mirror by
// another name: if the server gained a group that registers an unlock route and
// the guard's server did not wire it, the route would be outside the walk while
// the guard stayed green.
//
// So this test reads server.go's routes() and requires every route group the
// server registers to be CLASSIFIED in unlockClassRouteGroups — either wired in
// the guard's own server (and then required to be non-nil there) or named with a
// reason. A new group cannot land unclassified.
func TestUnlockClassGuardWiresEveryRouteGroupTheServerRegisters(t *testing.T) {
	// rootDir is the repo root, resolved by openapi_parity_test.go's own
	// runtime.Caller helper — the same root every other file-reading guard in
	// this package uses, so the path below cannot drift from the package's.
	src, err := os.ReadFile(filepath.Join(rootDir, "api", "internal", "httpapi", "server.go"))
	if err != nil {
		t.Fatalf("read server.go: %v — this guard cannot check its own instrument without the "+
			"route table's source", err)
	}

	// Every `X.RegisterRoutes(mux)` call in the server's route table, by receiver.
	regRe := regexp.MustCompile(`(?m)^\s*(\S+)\.RegisterRoutes\(mux\b`)
	var registered []string
	for _, m := range regRe.FindAllStringSubmatch(string(src), -1) {
		registered = append(registered, m[1])
	}
	if len(registered) < 4 {
		t.Fatalf("found only %d route group(s) in server.go's table (%v) — the scan is blind, so a "+
			"group it failed to see could register an unlock route outside the guard's walk",
			len(registered), registered)
	}

	known := map[string]*struct {
		receiver string
		reason   string
		wired    func(*Server) bool
	}{}
	for i := range unlockClassRouteGroups {
		g := &unlockClassRouteGroups[i]
		known[g.receiver] = g
	}

	srv := servedMux()
	var unclassified []string
	for _, recv := range registered {
		g, ok := known[recv]
		if !ok {
			unclassified = append(unclassified, recv)
			continue
		}
		// A group marked WIRED must really be wired on the server this guard
		// walks — otherwise its routes are served but outside the walk.
		if g.wired == nil {
			continue
		}
		if !g.wired(srv) {
			t.Errorf("route group %q is marked wired in unlockClassRouteGroups but is NIL on the "+
				"server this guard walks — its routes are on the served table and NOT in the guard's "+
				"enumeration, so an unlock route added to it would be judged by nothing here", recv)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("server.go registers %d route group(s) that unlockClassRouteGroups does not "+
			"classify: %v — the guard's own table is then narrower than the served one BY HAND, "+
			"which is the defect this file exists to close, one layer down. Either wire the group in "+
			"servedMux() (and mark it wired) or name it with a reason for why its routes need no "+
			"unlock coverage.", len(unclassified), unclassified)
	}

	var unwired []string
	for i := range unlockClassRouteGroups {
		if g := &unlockClassRouteGroups[i]; g.wired == nil {
			unwired = append(unwired, g.receiver)
		}
	}
	t.Logf("route groups on the served table: %d, all classified (%d deliberately not wired: %v)",
		len(registered), len(unwired), unwired)
}

// TestUnlockPatternIsIdentifiedByItsTailNotItsWholePath pins the recogniser the
// enumeration rests on: a route is an unlock because its PATTERN ends in
// "/unlock" and is POST-reachable — not because it matches one remembered
// spelling. A future unlock at a new prefix (`/qa-new/{slug}/unlock`, or a
// helper-registered `/anything/{x}/unlock`) is therefore inside the guard's scope
// the moment it is registered, which is the whole difference between this guard
// and the regex it replaces.
func TestUnlockPatternIsIdentifiedByItsTailNotItsWholePath(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    bool
	}{
		{"POST /{slug}/unlock", true},
		{"POST /f/{slug}/unlock", true},
		{"POST /embed/f/{wsslug}/{slug}/unlock", true},
		{"POST /qa-indirect/{slug}/unlock", true},
		{"POST /anything/{x}/{y}/unlock", true},
		// No method token: net/http matches such a pattern for POST too, so it is
		// an unlock a gate can post to and must be judged.
		{"/no-method/{slug}/unlock", true},
		// The bare suffix is a POST route ending in "/unlock": judged too, because
		// whether it reaches an unlock HANDLER is not this recogniser's question.
		{"POST /unlock", true},
		// Not unlocks: the suffix is not at the END of the path.
		{"POST /{slug}/unlock-harder", false},
		{"POST /{slug}/unlocked", false},
		// Not POST-reachable, so no gate fetch can reach it.
		{"GET /{slug}/unlock", false},
		{"DELETE /{slug}/unlock", false},
		// A sibling route, for contrast with the three above.
		{"POST /{slug}/order", false},
	} {
		if got := unlockRouteMatches(tc.pattern); got != tc.want {
			t.Errorf("pattern %q classified as unlock=%v, want %v — the recognition rule is \"a "+
				"POST-reachable pattern whose path ends in %q\", and any other rule is a spelling list "+
				"a new route can fall outside of", tc.pattern, got, tc.want, unlockRouteSuffix)
		}
	}
}

// TestUnlockProbePathsResolveBackToTheirOwnPattern pins the probe's own
// precondition: the concrete path it fires at must be one the mux dispatches to
// the pattern under test. A filling that resolved to a SIBLING route would make
// the burst a measurement of that sibling, so a limited route could look
// unlimited (or the reverse) purely because the probe aimed at the wrong
// registration.
func TestUnlockProbePathsResolveBackToTheirOwnPattern(t *testing.T) {
	mux := servedMux().routes()
	for _, pat := range []string{
		"POST /{slug}/unlock",
		"POST /f/{slug}/unlock",
		"POST /embed/f/{wsslug}/{slug}/unlock",
	} {
		if pth, h := resolveUnlockProbePath(mux, pat, unlockProbeHost); pth == "" {
			t.Errorf("no positional filling of %q resolves back to that pattern on %s (tried %v) — "+
				"the probe cannot aim at this route, so it would be judged by whatever sibling it "+
				"actually hits", pat, unlockProbeHost, unlockPatternFillings(gateRouteProbeFor(pat)))
		} else {
			t.Logf("%-42s -> %-44s handler=%s", pat, pth, h)
		}
	}
}

// TestUnlockClassGuardFailsOnAnUndrivablePattern pins the failure shape: a
// registered `.../unlock` pattern the guard cannot turn into a request must be
// REPORTED, never dropped from the covered set. A silent skip is how a route
// stops being covered while the run stays green.
//
// The trap is a HOST-QUALIFIED pattern — the shape this guard genuinely cannot
// drive, because it sends every request on ONE host and net/http has no host
// wildcard (pinned by TestServeMuxRejectsAWildcardHost). It is registered the
// same way a real route would be, so nothing about the trap is synthetic.
//
// The branch is the LAST resort of resolution: the guard reports undrivable when
// resolveUnlockProbePath gives it no path, whatever the reason. Pinning only the
// OUTCOME ("undrivable == 1") therefore cannot tell the claim — "the walk cannot
// reach a host-pinned pattern" — from ANY other reason resolution returns empty
// (a broken filling, a parser that stops reading the host section), and a red
// would name the symptom without the cause. So the cause is asserted too, as a
// MATCHED PAIR: the host-qualified trap reads its host section into hostPat and
// cannot be resolved on the one host the probe drives, and the SAME pattern with
// the host section stripped off IS drivable. Only the host section differs
// between the two, so the difference in outcome is attributable to it and to
// nothing else (pinned against the parser by
// TestGateRouteProbeFillsWildcardsByPositionNotName, which asserts hostPat).
func TestUnlockClassGuardFailsOnAnUndrivablePattern(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{slug}/unlock", noop)                  // a real, drivable sibling
	mux.HandleFunc("POST other.example.com/{slug}/unlock", noop) // pinned to another literal host

	const (
		trap     = "POST other.example.com/{slug}/unlock" // host-pinned to another literal host
		stripped = "POST /{slug}/unlock"                  // the SAME pattern, host section removed
	)

	drivable, undrivable := 0, 0
	for _, pat := range muxPatterns(t, mux) {
		if !unlockRouteMatches(pat) {
			continue
		}
		if pth, _ := resolveUnlockProbePath(mux, pat, unlockProbeHost); pth == "" {
			undrivable++
		} else {
			drivable++
		}
	}
	if drivable != 1 {
		t.Errorf("expected the drivable sibling to be driven exactly once, got %d — the probe's "+
			"resolution is not measuring what this test thinks it is", drivable)
	}
	if undrivable != 1 {
		t.Fatalf("the host-qualified trap was not reported undrivable (drivable=%d undrivable=%d) — "+
			"this guard's undrivable branch does not fire, so a pattern it cannot drive would be "+
			"dropped from the covered set silently", drivable, undrivable)
	}

	// ── the CAUSE, not just the outcome ──
	pr := gateRouteProbeFor(trap)
	if pr.hostPat == "" {
		t.Fatalf("the parser read NO host section out of %q (hostPat is empty, pathPat=%q) — the "+
			"trap is only a trap if the host section is READ; with it dropped the pattern would "+
			"resolve on the probe host and the undrivable count above would be measuring something "+
			"else", trap, pr.pathPat)
	}
	// the trap must NOT resolve on the one host the probe drives: it is pinned to
	// `other.example.com`, and net/http has no host wildcard, so no request the
	// probe can send reaches it.
	if pth, hk := resolveUnlockProbePath(mux, trap, unlockProbeHost); pth != "" {
		t.Errorf("%q resolved to %q (handler %s) on host %q — it must NOT, because it is pinned "+
			"to %q. If this ever resolves, the undrivable branch is no longer being exercised by "+
			"this test and the outcome assertion above is vacuous", trap, pth, hk, unlockProbeHost, pr.hostPat)
	}
	// the matched pair: identical in every respect except the host section.
	spr := gateRouteProbeFor(stripped)
	if spr.hostPat != "" {
		t.Fatalf("the stripped pattern %q still carries a host section (%q) — the pair is not "+
			"matched, so the drivability difference below would not be attributable to the host",
			stripped, spr.hostPat)
	}
	if pth, hk := resolveUnlockProbePath(mux, stripped, unlockProbeHost); pth == "" {
		t.Errorf("the SAME pattern with its host section stripped (%q) is ALSO undrivable on host "+
			"%q — so the trap is not the host section, and the undrivable count above is not "+
			"evidence about host-pinned patterns (fillings tried: %v, handler %s)",
			stripped, unlockProbeHost, unlockPatternFillings(spr), hk)
	}
	t.Logf("undrivable report works: drivable=%d, undrivable=%d; trap %q hostPat=%q is undrivable, "+
		"the same pattern stripped %q is drivable", drivable, undrivable, trap, pr.hostPat, stripped)
}

// contains reports whether needle is in hay.
func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
