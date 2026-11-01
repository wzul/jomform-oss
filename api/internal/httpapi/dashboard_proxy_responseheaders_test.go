// The dashboard proxy must relay the response headers the API's contract puts in
// front of a BROWSER — the login lockout's Retry-After is the one that was missing
// (card t_2af1414e).
//
// THE DEFECT. web/src/app/api/[...path]/route.ts built the response half of its
// hop out of three hand-copied headers (Content-Type, Location, Set-Cookie) and
// dropped everything else the API set. The API states its backoff in a header:
//
//   - the login lockout answers 429 + `Retry-After: 900`
//     (internal/httpapi/auth.go, auth.LoginLockoutRetryAfter — internal/auth/loginlimiter.go)
//   - the coarse per-IP throttle answers 429 + `Retry-After: 30`
//     (internal/httpapi/middleware/ratelimit.go)
//
// Measured from outside on prod v1.21.129 (card t_5fd36e64), the SAME locked
// visitor and an identical body: `Retry-After: 900` direct to api.jomform.com,
// and no retry-after at all through https://jomform.com/api/auth/login. Direct
// API and MCP callers kept the header; only the browser path lost it, so a
// locked-out dashboard could not tell when its window clears.
//
// WHAT IS PINNED HERE, and why it has to be this half. That the hop copies the
// header VERBATIM, invents nothing when the API sent none, and that route.ts
// actually calls the rule, is a behavioural test with real Headers objects
// (web/src/lib/proxyResponseHeaders.test.ts, `npm run test:unit`). What that half
// cannot see is whether the API still STATES a backoff at all, and whether the
// hop's declared set has grown an entry the API never owes a browser — the web
// test can only compare the list against itself. So this file reads both sides:
//
//  1. DERIVES the backoff header from the API's own source (the two writers
//     above), and fails LOUDLY if they moved rather than quietly pinning
//     nothing. That the two writers really put it on the wire is pinned where
//     the behaviour is: internal/httpapi/auth_hardening_test.go and
//     internal/httpapi/middleware/ratelimit_test.go. This file is about the
//     AGREEMENT between the two languages, which neither of those can see.
//
//  2. Requires the hop to relay it — dropping `retry-after` from the declared
//     list is RED here, which is exactly the shipped defect. The same arm covers
//     `cache-control`: the API marks the merchant's authed stand `private,
//     no-store` (internal/httpapi/rest_qr_stand.go) and the dashboard opens that
//     route through this hop, so dropping it there made the browser path promise
//     less than the API does (card t_b870d44b). That writer is DERIVED from the
//     API's own source too, so the guarded set cannot quietly outlive it.
//
//  3. Requires the declared set to be EXACTLY what this proxy owes the browser:
//     the three transport headers it has always relayed (content-type, location,
//     set-cookie — the reasons are recorded at each entry of the web list and
//     restated in transportResponseHeaders below) plus the two derived headers.
//     An entry the API does not owe a browser fails too: a response
//     header relayed on nobody's decision is how a second, weaker promise about
//     this hop appears (the same "a trust input nobody decided on" arm the
//     client-address pin carries).
//
// WHAT THIS FILE DOES NOT CLAIM: the VALUES. It says which header crosses, not
// what number it carries — that is the API's, in one place, and the client is
// meant to obey it.
package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// retryAfterWrite matches a Retry-After the API sets on its own response:
// w.Header().Set("Retry-After", auth.LoginLockoutRetryAfter).
var retryAfterWrite = regexp.MustCompile(`Header\(\)\.Set\(\s*"Retry-After"`)

// cacheControlWrite matches a Cache-Control the API sets on its own response:
// w.Header().Set("Cache-Control", "private, no-store").
var cacheControlWrite = regexp.MustCompile(`Header\(\)\.Set\(\s*"Cache-Control"`)

// The one declaration the hop's relayed response-header set is read from
// (web/src/lib/proxyResponseHeaders.ts). Requiring the exact shape is
// deliberate: a renamed or restructured list must fail LOUDLY here rather than
// yield an empty set, which would make the comparison below pass by measuring
// nothing.
const (
	webResponseMarker = "export const RELAYED_RESPONSE_HEADERS = ["
	webResponseClose  = "] as const;"
)

// transportResponseHeaders are the headers this hop relays so the transport
// works at all, each with the reason it is not optional. They are named rather
// than derived because no file in the API states them: the API simply answers,
// and it is the BROWSER's needs that decide these three.
var transportResponseHeaders = map[string]string{
	"content-type": "the hop serves a body it fetched; without the type neither the browser nor the dashboard's JSON callers know what it is",
	"location":     "the OAuth redirects (issue #273) answer 302 and the BROWSER must follow them, not this container",
	"set-cookie":   "the session cookie and the Cloudflare OAuth state + PKCE pair (issue #22) — several per response, so getSetCookie() is what copies them",
}

// TestDashboardProxyRelaysTheAPIsRetryAfterHeader is the cross-language pin
// described in this file's header.
func TestDashboardProxyRelaysTheAPIsRetryAfterHeader(t *testing.T) {
	// 1. The derivation, which must be non-vacuous before it judges anything.
	writers := apiRetryAfterWriters(t)
	t.Logf("the API writes Retry-After in: %v", writers)
	for _, want := range []string{
		"internal/httpapi/auth.go",                 // the login lockout (Retry-After: 900)
		"internal/httpapi/middleware/ratelimit.go", // the coarse per-IP throttle (Retry-After: 30)
	} {
		if !contains(writers, want) {
			t.Fatalf("derived %v as the files that set Retry-After — expected %q among them. Either that "+
				"writer moved (re-point this guard, and the card's measurement with it) or this scan is "+
				"broken, and an empty or partial set would make the comparison below pass by measuring nothing",
				writers, want)
		}
	}

	// 2. What the hop owes a browser: those three, plus the two headers derived
	//    from the API's own source.
	cacheWriters := apiHeaderWriters(t, cacheControlWrite)
	t.Logf("the API sets Cache-Control in: %v", cacheWriters)
	if !contains(cacheWriters, "internal/httpapi/rest_qr_stand.go") {
		t.Fatalf("derived %v as the files that set Cache-Control — expected internal/httpapi/rest_qr_stand.go "+
			"among them: that route marks the merchant's authed printable stand `private, no-store` "+
			"because it names an unpublished form, and it is the response this hop carries to the "+
			"browser. Either the writer moved (re-point this guard) or the header is gone, and the "+
			"relayed set below would then be promising something no route states", cacheWriters)
	}
	owed := map[string]string{
		"retry-after":   "the API's own backoff for a 429 — the login lockout and the per-IP throttle both state it, and a dashboard client can only obey the value the server chose if it receives the header",
		"cache-control": "the API's own storage directive for the response — the merchant's authed stand (internal/httpapi/rest_qr_stand.go) is `private, no-store` because it names an unpublished form, and the dashboard's \"Print stand\" anchor opens that route THROUGH this hop; dropping it made the browser path promise less than the API does (measured on prod: `private, no-store` from the API host, no cache-control at all from https://jomform.com/api/rest/forms/{id}/qr-stand)",
	}
	for header, reason := range transportResponseHeaders {
		owed[header] = reason
	}

	webHeaders := webRelayedResponseHeaders(t)

	// Direction 1: every header the proxy owes the browser must be relayed.
	// Missing retry-after is the shipped defect: the browser sees the 429 and the
	// prose and cannot tell when the window clears.
	for header, reason := range owed {
		if !contains(webHeaders, header) {
			t.Errorf("this hop owes the browser %q (%s), but web/src/lib/proxyResponseHeaders.ts does "+
				"not relay it (relayed: %v) — measured on prod v1.21.129: `Retry-After: 900` direct to "+
				"https://api.jomform.com/v1/auth/login and no retry-after at all through "+
				"https://jomform.com/api/auth/login, for the same locked visitor and an identical body",
				header, reason, webHeaders)
		}
	}

	// Direction 2: the hop must not relay anything else. A response header the
	// API never owes a browser (X-Request-ID, the CSP it sets on its own HTML)
	// reaches the dashboard's callers as a promise nobody made.
	for _, header := range webHeaders {
		if _, ok := owed[header]; !ok {
			t.Errorf("web/src/lib/proxyResponseHeaders.ts relays %q, which this proxy does not owe the "+
				"browser — a response header relayed on nobody's decision is a second, weaker promise about "+
				"this hop; if the API really means the browser to see it, add it to the transport set in this "+
				"guard (with its reason) and to the module, deliberately", header)
		}
	}

	// 3. And the set must be USED, in a response header set the hop builds
	// itself: a list the proxy never calls proves nothing, and an upstream
	// pass-through would relay everything this guard just refused.
	route := webSource(t, "src/app/api/[...path]/route.ts")
	if !strings.Contains(route, "relayResponseHeaders(r.headers, respHeaders)") {
		t.Errorf("web/src/app/api/[...path]/route.ts does not relay the API's response headers through " +
			"relayResponseHeaders — without that call the relayed set above is dead code and every " +
			"dashboard response still reaches the browser with the three hand-copied headers only")
	}
	if regexp.MustCompile(`headers:\s*r\.headers`).MatchString(route) {
		t.Errorf("web/src/app/api/[...path]/route.ts hands the upstream response headers to NextResponse " +
			"wholesale — that relays the API's own-edge headers (X-Request-ID, the CSP on its own HTML) " +
			"which this proxy does not owe the browser, and it would make the direction-2 arm above " +
			"agree with itself instead of with the module")
	}
}

// webRelayedResponseHeaders returns the response headers the hop declares it
// relays, read from the one declaration in
// web/src/lib/proxyResponseHeaders.ts. Comments inside the array are dropped
// first: a header name quoted in prose is not a relayed header, and one stray
// quote in a comment must not turn this into a confusing parse failure.
func webRelayedResponseHeaders(t *testing.T) []string {
	t.Helper()
	src := webSource(t, "src/lib/proxyResponseHeaders.ts")

	start := strings.Index(src, webResponseMarker)
	if start < 0 {
		t.Fatalf("web/src/lib/proxyResponseHeaders.ts no longer declares %q — this guard reads the "+
			"relayed response headers from that one declaration, and an empty set would let it pass vacuously",
			webResponseMarker)
	}
	rest := src[start+len(webResponseMarker):]
	end := strings.Index(rest, webResponseClose)
	if end < 0 {
		t.Fatalf("web/src/lib/proxyResponseHeaders.ts no longer closes RELAYED_RESPONSE_HEADERS with "+
			"%q — this guard cannot tell where the list ends, so it refuses to read it rather than guess",
			webResponseClose)
	}

	var body strings.Builder
	for _, line := range strings.Split(rest[:end], "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		body.WriteString(line)
		body.WriteString("\n")
	}

	var out []string
	for _, m := range quotedName.FindAllStringSubmatch(body.String(), -1) {
		out = append(out, strings.ToLower(m[1]))
	}
	if len(out) == 0 {
		t.Fatalf("read ZERO header names from RELAYED_RESPONSE_HEADERS in " +
			"web/src/lib/proxyResponseHeaders.ts — the parse found the declaration but not its entries, " +
			"so every comparison in this file would pass by measuring nothing")
	}
	return sortedUnique(out)
}

// apiRetryAfterWriters lists the non-test .go files under internal/httpapi that
// set a Retry-After on a response, as paths relative to the api module root.
func apiRetryAfterWriters(t *testing.T) []string {
	t.Helper()
	return apiHeaderWriters(t, retryAfterWrite)
}

// apiHeaderWriters lists the non-test .go files under internal/httpapi that set a
// response header matching `write`, as paths relative to the api module root. The
// file list comes from git (apiSourceFiles), so a generated artefact is never
// judged and a new writer cannot be missed.
func apiHeaderWriters(t *testing.T, write *regexp.Regexp) []string {
	t.Helper()
	root := apiSourceRoot(t)

	var out []string
	for _, path := range apiSourceFiles(t) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel %s: %v", path, err)
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "internal/httpapi/") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if write.Match(src) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}
