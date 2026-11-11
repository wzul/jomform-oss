// The dashboard proxy and the API must agree about the client address — pinned
// from the API side (card t_ae8791f9).
//
// THE DEFECT. web/src/app/api/[...path]/route.ts relayed NO client-address
// header, while the API resolves the client from exactly those headers whenever
// the socket peer is an address a request can arrive through
// (internal/httpapi/middleware/clientip.go — the compose network is one). Every
// dashboard request therefore reached the API from the WEB CONTAINER's address,
// so the per-IP rate limit and the login lockout shared ONE key across all
// browsers. Measured on prod, revision 72092730 (t_730c552d), reading prod
// valkey: a wrong-password login through https://jomform.com/api/auth/login
// created `jf:loginfail:ip:172.18.0.6` (== jomform-web-1 on the compose network)
// while the same login through https://api.jomform.com/v1/auth/login keyed on
// the visitor's own address. FailLockoutThreshold = 10 in a 15-minute window, so
// ten wrong logins from any mix of accounts through the dashboard refused EVERY
// dashboard login for 15 minutes, correct password included.
//
// WHY THIS TEST IS A SOURCE PIN. The API could not fix this alone: with no
// header in the request there is no evidence of the visitor to resolve, and
// keying on anything else would be inventing one. So the contract is a pair of
// files in two languages, and each half is pinned from the gate that can see it:
//
//   - the web half — that the relay copies the headers, and that route.ts uses
//     it — is a BEHAVIOURAL test of the rule with real Headers objects
//     (web/src/lib/clientAddress.test.ts, `npm run test:unit`);
//   - the API half — that the two sides name the SAME headers — is this file,
//     which is the only one of the two that can read the other side's rule.
//
// Neither half can run the other's code: importing route.ts under node --test
// fails on its `next/server` import (no exports map, no extension probing), and
// the route cannot be driven from Go. The pairing is therefore derived, not
// duplicated: the header names come from clientip.go's own reads, and the web
// hop's set is compared against them in BOTH directions, so
//
//   - a header the API resolves but the hop does not relay is RED here (that is
//     this card's defect), and
//   - a header the hop relays but the API does not resolve is RED too (a trust
//     input nobody decided on is how a second, weaker reader appears).
//
// WHAT THIS FILE DOES NOT CLAIM: that the values in those headers are
// trustworthy, or what Traefik/Cloudflare put in them. That is the API's
// decision, in one place, and its behaviour is pinned by the table and the burst
// tests in internal/httpapi/middleware/clientip_test.go — including the arm this
// card is about, "behind the CDN with an extra internal hop (the Next /api/*
// proxy)". The end-to-end evidence for the fix as SHIPPED (real Next server +
// real API + the valkey key the login creates) is recorded on the card.
package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// headerGetCall matches a read of a forwarding header through the request:
// r.Header.Get("CF-Connecting-IP").
var headerGetCall = regexp.MustCompile(`Header\.Get\(\s*"([^"]+)"\s*\)`)

// quotedName matches a double-quoted string literal inside the web hop's array.
var quotedName = regexp.MustCompile(`"([^"]+)"`)

// webSetMarker is the one declaration the web hop's relayed set is read from.
// Requiring the exact shape is deliberate: a renamed or restructured list must
// fail LOUDLY here rather than silently yield an empty set, which would make the
// comparison below pass by measuring nothing.
const (
	webSetMarker = "export const CLIENT_ADDRESS_HEADERS = ["
	webSetClose  = "] as const;"
)

// TestDashboardProxyRelaysEveryHeaderTheAPITrusts is the cross-language pin
// described in this file's header.
func TestDashboardProxyRelaysEveryHeaderTheAPITrusts(t *testing.T) {
	apiHeaders := apiResolvedHeaders(t)
	webHeaders := webRelayedHeaders(t)

	// The derivation must be non-vacuous before it is used to judge anything: a
	// regexp that stopped matching would otherwise make every comparison below
	// trivially true. Four headers: the three the CLIENT ADDRESS is resolved from,
	// plus CF-IPCity — the same file reads it for display only (the device-flow
	// consent email says where the request came from), and the dashboard hop has
	// to relay it for the same reason it relays the others.
	if len(apiHeaders) != 4 || !contains(apiHeaders, "cf-connecting-ip") || !contains(apiHeaders, "cf-ipcity") {
		t.Fatalf("derived %v from clientip.go as the headers it resolves the client from — expected the three the rule reads "+
			"(CF-Connecting-IP, X-Real-IP, X-Forwarded-For) plus CF-IPCity. Either the rule moved or this scan is broken, and an empty or "+
			"partial set would make the comparison below pass by measuring nothing", apiHeaders)
	}

	// Direction 1: every header the API resolves must be relayed by the hop.
	// Missing one is a silently weaker key: X-Forwarded-For absent means the API
	// cannot see the chain, so it keeps keying on the web container.
	for _, h := range apiHeaders {
		if !contains(webHeaders, h) {
			t.Errorf("clientip.go resolves the client from %q, but web/src/app/api/[...path]/route.ts does not relay it "+
				"(relayed: %v) — the API then has no evidence of the visitor and keys its per-IP rate limit and the "+
				"10-failure login lockout on the web container's address, one bucket shared by every browser "+
				"(measured on prod: jf:loginfail:ip:172.18.0.6)", h, webHeaders)
		}
	}

	// Direction 2: the hop must not relay anything the API does not resolve.
	for _, h := range webHeaders {
		if !contains(apiHeaders, h) {
			t.Errorf("web/src/app/api/[...path]/route.ts relays %q, which clientip.go does not resolve the client from — "+
				"a header with no reader is a trust input nobody decided on; add it to the resolution rule (and to its "+
				"guard) or stop relaying it", h)
		}
	}

	// And the set must be USED: a list the proxy never calls proves nothing about
	// the hop, which is the half that was missing on the shipped revision.
	route := webSource(t, "src/app/api/[...path]/route.ts")
	if !strings.Contains(route, "forwardClientAddress(req.headers, headers)") {
		t.Errorf("web/src/app/api/[...path]/route.ts does not relay the client address through forwardClientAddress — " +
			"without that call the relayed set above is dead code and every dashboard request still reaches the API " +
			"from this container's address")
	}
}

// apiResolvedHeaders returns the forwarding headers clientip.go reads, lower-cased
// and sorted. The file is the ONE reader of them (enforced by
// middleware/forwarded_headers_one_reader_test.go), so it is the API's whole
// statement of which headers may carry a client address.
func apiResolvedHeaders(t *testing.T) []string {
	t.Helper()
	src := apiSource(t, "internal/httpapi/middleware/clientip.go")

	var out []string
	for _, m := range headerGetCall.FindAllStringSubmatch(src, -1) {
		out = append(out, strings.ToLower(m[1]))
	}
	return sortedUnique(out)
}

// webRelayedHeaders returns the headers the web hop's relay copies, read from
// web/src/lib/clientAddress.ts.
func webRelayedHeaders(t *testing.T) []string {
	t.Helper()
	src := webSource(t, "src/lib/clientAddress.ts")

	start := strings.Index(src, webSetMarker)
	if start < 0 {
		t.Fatalf("web/src/lib/clientAddress.ts no longer declares %q — this guard reads the relayed set from that one "+
			"declaration, and an empty set would make it pass vacuously", webSetMarker)
	}
	rest := src[start+len(webSetMarker):]
	end := strings.Index(rest, webSetClose)
	if end < 0 {
		t.Fatalf("web/src/lib/clientAddress.ts no longer closes CLIENT_ADDRESS_HEADERS with %q — this guard cannot tell "+
			"where the list ends, so it refuses to read it rather than guess", webSetClose)
	}

	// Comments inside the array are dropped first: a marker string in prose is
	// not a header name, and one stray quote in a comment must not turn this into
	// a confusing parse failure.
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
	return sortedUnique(out)
}

// apiSource reads a file from the api module, relative to the module root.
func apiSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// sortedUnique normalises a name list so the two sides compare as sets.
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
