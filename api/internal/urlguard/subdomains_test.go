package urlguard

// t_586ff0c3 — the reserved-subdomain list, and the guard that keeps it honest.
//
// WHAT THESE TESTS ARE FOR
//
// A hand-written allow/deny list rots in two directions, and both are silent:
//
//  1. It drifts from what is actually ROUTED. Add a static Traefik router for a
//     new JomForm subdomain and, if nobody remembers this list, that subdomain
//     becomes claimable — the merchant's workspace then shadows a live JomForm
//     surface. TestReservedSubdomainsCoverStaticTraefikHosts parses
//     deploy/vps/traefik-jomform.yml and fails on any Host(...) that is neither
//     the apex nor in the list, so the reminder is mechanical.
//  2. It gets padded or hollowed out. A label with no reason cannot be reviewed;
//     dropping a category (say `account`) would reopen the impersonation hole
//     this card exists to close. The reason check and the required-category
//     check fail on both.
//
// And the rule itself has to stay ONE rule: the copy-paste failure mode is a
// second, hand-rolled list in another package. That fact has ONE home — the guard
// that owns the rule and DERIVES the set of files calling it by walking api/,
// api/internal/dbstore/workspace_slug_rule_guard_test.go
// (TestWorkspaceSlugRuleHasExactlyOneDefinition, merged as PR 1192). This file
// does not restate it: why, and what the version that stood here could not see,
// is in the section comment below.
//
// THE LIST'S COVERAGE LIMIT, stated so this is not over-claimed: it proves the
// labels that are WRITTEN DOWN are routed-or-sensitive, that the ONE rule
// consults this list, and that the list has not been copied into the rule or the
// two transports. It cannot know that some other sensitive name exists (a new
// product name, a new mail alias) — that is a human review decision, and this is
// the file that makes it cheap to make. Nor can it see a caller of the rule in a
// file it does not name: that set is derived, and asserted, in the dbstore guard
// above.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// repoRoot locates the repository root from this test file's directory
// (api/internal/urlguard -> ../../../). Callers must treat a miss as a failure,
// never a skip: a guard that silently skips proves nothing.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — this guard cannot run without it, and a skip would "+
			"report green while proving nothing", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return string(raw)
}

// --- the rule ---------------------------------------------------------------

func TestReservedSubdomainRejectsEveryCategory(t *testing.T) {
	// One name from each category the card names. If a category is dropped from
	// the list, this test fails by name — which is the point: the hole is not
	// "an entry went missing", it is "a sensitive name became claimable".
	infrastructure := []string{"www", "api", "app", "mail", "s3"}
	routed := []string{"docs", "mcp"}
	sensitive := []string{
		"support", "help", "status", "billing", "admin", "blog", "security",
		"account", "accounts", "login", "signup", "payments", "pay", "checkout",
		"cdn", "assets",
	}
	// Added by this card beyond the required floor, each because it is a real
	// JomForm address today (see the reasons in subdomains.go).
	extra := []string{"templates", "custom"}

	for _, group := range []struct {
		name   string
		labels []string
	}{
		{"infrastructure", infrastructure},
		{"routed product/edge", routed},
		{"sensitive/impersonation", sensitive},
		{"already-routed JomForm addresses", extra},
	} {
		for _, label := range group.labels {
			if !ReservedSubdomain(label) {
				t.Errorf("ReservedSubdomain(%q) = false, want true — %s: a workspace "+
					"could claim %s.jomform.com", label, group.name, label)
			}
		}
	}
}

func TestReservedSubdomainAcceptsALegitimateName(t *testing.T) {
	// The rule adds a denial, it must not become a blanket denial. These are the
	// shapes a real merchant picks, plus the live prod slugs (wan, abc, membe,
	// alpha, bravo) this list was introduced around without needing a rename.
	for _, ok := range []string{
		"acme", "kedai-kopi", "bengkel99", "toko-saya",
		"wan", "abc", "membe", "alpha", "bravo",
		// Near-misses of reserved labels: a substring or an affix is NOT a
		// reserved name, and refusing these would be the list over-reaching.
		"support-my", "mysupport", "apiary", "apparel", "blogger", "docsy",
		"checkoutku", "payments2", "admin-pro", "mailbox",
	} {
		if ReservedSubdomain(ok) {
			t.Errorf("ReservedSubdomain(%q) = true, want false — the list is "+
				"over-reaching and refusing a legitimate merchant address", ok)
		}
	}
}

func TestReservedSubdomainNormalisesBeforeComparing(t *testing.T) {
	// The callers lower-case and trim before they reach here, but the rule must
	// not depend on that: a mixed-case or space-padded value must not slip
	// through as "a different name".
	for _, v := range []string{" SUPPORT ", "Support", "BILLING", "\tDocs\n", "MCP"} {
		if !ReservedSubdomain(v) {
			t.Errorf("ReservedSubdomain(%q) = false, want true — normalisation "+
				"is the caller's job upstream, and the rule must not rely on it", v)
		}
	}
}

func TestReservedSubdomainMessageSaysWhy(t *testing.T) {
	// The merchant has to learn the name is RESERVED FOR JOMFORM, not that their
	// input was malformed — otherwise they try `support2`, `support-my`, etc.
	msg := ReservedSubdomainMessage("support")
	if msg == "" {
		t.Fatal("ReservedSubdomainMessage returned empty string")
	}
	if !strings.Contains(msg, "support") {
		t.Errorf("message %q does not name the refused label", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "reserved") {
		t.Errorf("message %q does not say the name is reserved", msg)
	}
	if !strings.Contains(msg, "JomForm") {
		t.Errorf("message %q does not say who reserved it", msg)
	}
}

func TestReservedSubdomainCodeIsStableAndSnakeCase(t *testing.T) {
	// The dashboard matches on this token instead of parsing prose (the shape
	// PublishWarningNoProducts established). It is mirrored in
	// web/src/lib/workspaceSlug.ts and cross-checked there.
	if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(ReservedSubdomainCode) {
		t.Errorf("ReservedSubdomainCode = %q, want a snake_case token", ReservedSubdomainCode)
	}
}

// --- the list cannot rot quietly -------------------------------------------

func TestEveryReservedSubdomainCarriesAReason(t *testing.T) {
	// A bare label is unreviewable: nobody can tell whether it is still routed,
	// still sensitive, or a leftover. The empty-reason case is also how a
	// padding entry would arrive.
	for label, reason := range reservedSubdomains {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("reservedSubdomains[%q] has no reason — an entry that cannot be "+
				"reviewed is an entry that cannot be removed either", label)
		}
		if len(strings.TrimSpace(reason)) < 20 {
			t.Errorf("reservedSubdomains[%q] reason %q is too thin to review", label, reason)
		}
	}
}

func TestEveryReservedSubdomainIsAValidLabel(t *testing.T) {
	// A label with a dot or a slash would silently never match a slug (the
	// validators reject those characters long before this rule runs), so the
	// entry would look like protection and provide none.
	valid := regexp.MustCompile(`^[a-z0-9-]+$`)
	for label := range reservedSubdomains {
		if !valid.MatchString(label) {
			t.Errorf("reservedSubdomains key %q is not a bare lower-case label "+
				"(a-z, 0-9, dash) — it can never match a workspace slug", label)
		}
	}
}

func TestReservedSubdomainListHasNotCollapsed(t *testing.T) {
	// The set is a policy about names JomForm controls; a list that shrinks to a
	// handful would still pass every per-category assertion above if someone
	// deleted whole groups by accident. Pin a floor, the way this repo pins the
	// i18n dictionary's size.
	if len(reservedSubdomains) < 20 {
		t.Fatalf("reservedSubdomains has %d entries, want at least 20 — the list "+
			"has collapsed; it is a policy, not a placeholder", len(reservedSubdomains))
	}
}

// --- bound to reality: what Traefik actually routes -------------------------

// traefikHostRe matches a Host(`x`) literal inside a Traefik rule string, and
// captures a leading `!` when it is negated (`!Host(`x`)`). Both shapes matter:
// a plain `Host(`x`)` is a static route for x, a `!Host(`x`)` is an EXCLUSION
// that takes x away from the router it sits on. `HostRegexp(` does NOT match —
// the character after `Host` is `R`, not `(` — so a regex host is never mistaken
// for a static one.
var traefikHostRe = regexp.MustCompile("(!?)Host\\(`([^`]+)`\\)")

// traefikHostRegexpRe captures the pattern of a HostRegexp(`...`) matcher. The
// two committed files write the tenant wildcard with different escaping
// (deploy/vps/traefik-jomform.yml uses an unescaped dot, deploy/traefik-wildcard.yml
// escapes it), so callers test the CAPTURED TEXT for "jomform" rather than
// matching an escaped literal that would only fit one of the two spellings.
var traefikHostRegexpRe = regexp.MustCompile("HostRegexp\\(`([^`]+)`\\)")

// jomformLabel returns the subdomain label of a jomform.com host literal
// ("www" for "www.jomform.com"), and false for anything else — the apex, a
// foreign domain, or a malformed host. Only a <label>.jomform.com host is a
// workspace-addressable name this guard reasons about.
func jomformLabel(host string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	label, ok := strings.CutSuffix(h, ".jomform.com")
	if !ok || label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

// tenantOrApexHost reports whether a Traefik Host(...) literal is NOT a label a
// workspace could claim: the apex itself. Everything else at
// <label>.jomform.com is a claimable subdomain and must be reserved.
func tenantOrApexHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "jomform.com"
}

// --- reading the router table -----------------------------------------------
//
// A router is not a line, and the guards below need three things that a
// line-by-line `rule:` scan cannot give. Each of the three has already bitten
// this file:
//
//   - the rule text with its CONTINUATION lines JOINED. Traefik's own table
//     wraps long rules (jf-api-f, jf-custom-root), and a `Host(...)` on a wrapped
//     line was invisible — the same class as t_1ab825fc, where a rule line mixing
//     HostRegexp with a static Host skipped the whole line.
//   - the router's PRIORITY, because priority is what decides which router
//     serves a host. A guard that assumes "the tenant wildcard always wins" is
//     not describing Traefik, and that assumption is exactly what reds `docs`
//     and `mcp` on the live file this repo now holds (t_3f44c437).
//   - the ENTRY POINTS. Two routers on different entry points never compete, so
//     comparing their priorities would demand exclusions nothing needs.

// traefikRouter is one entry of a file provider's `http.routers` table.
type traefikRouter struct {
	name        string
	rule        string
	priority    int
	hasPriority bool
	entryPoints []string
	line        int
}

// yamlKeyLineRe matches a `key:` or `key: value` line: a router's own key, as
// opposed to a wrapped rule continuation (`|| PathPrefix(...)`), which has no
// colon of its own.
var yamlKeyLineRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):`)

// splitYAMLKey splits "key: value" into its key and raw value. ok is false for a
// line that is not a key at all, which is how a wrapped rule continuation is told
// apart from the next key.
func splitYAMLKey(line string) (key, value string, ok bool) {
	m := yamlKeyLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(line[len(m[1])+1:]), true
}

// unquoteYAMLScalar strips one matching pair of surrounding quotes. Both files
// write rules both bare (“Host(`x`)“) and quoted (`"Host(`x`)"`, `'!Host(...)'`
// after a YAML line fold), so the guard must read the VALUE Traefik reads — not
// the YAML spelling of it.
func unquoteYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseEntryPoints reads an `entryPoints:` value in either spelling the files
// use: the inline flow sequence (`entryPoints: [websecure]`) or the block
// sequence on the following lines (`entryPoints:` / `- websecure`).
func parseEntryPoints(lines []string, keyLine, indent int, value string) []string {
	var out []string
	if strings.TrimSpace(value) != "" {
		v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "["), "]")
		for _, part := range strings.Split(v, ",") {
			if p := unquoteYAMLScalar(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	for j := keyLine + 1; j < len(lines); j++ {
		lt := strings.TrimSpace(lines[j])
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		li := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
		if li < indent || !strings.HasPrefix(lt, "- ") {
			break
		}
		out = append(out, unquoteYAMLScalar(strings.TrimPrefix(lt, "- ")))
	}
	return out
}

// parseTraefikRouters reads the `http.routers` table out of a committed Traefik
// file. It parses the YAML SUBSET these files use (block mapping, block/flow
// sequences, folded scalar values) because the guard needs the per-router
// associations — rule WITH its continuations, priority, entryPoints — that a line
// filter cannot express. A shape it cannot read is a hard failure, never a skip:
// a guard that quietly reads nothing reports green and proves nothing (the
// counters in each test say the same).
func parseTraefikRouters(t *testing.T, path string) []traefikRouter {
	t.Helper()
	lines := strings.Split(mustReadFile(t, path), "\n")

	var routers []traefikRouter
	section := ""    // the indent-2 key we are inside, e.g. "routers"
	cur := -1        // index into routers, -1 when not inside a router
	ruleIndent := -1 // indent of the `rule:` key that a continuation must exceed

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, value, isKey := splitYAMLKey(trimmed)

		// A wrapped `rule:` value: deeper than the `rule:` key and not a key of
		// its own. Joined, so a Host literal on a continuation is checked like
		// any other.
		if cur >= 0 && ruleIndent >= 0 && indent > ruleIndent && !isKey {
			routers[cur].rule += " " + trimmed
			continue
		}
		ruleIndent = -1

		if indent <= 2 {
			if indent == 2 && isKey {
				section = key
			} else {
				section = ""
			}
			continue
		}
		if section != "routers" {
			continue
		}
		if indent == 4 {
			if !isKey {
				continue
			}
			routers = append(routers, traefikRouter{name: key, line: lineNo})
			cur = len(routers) - 1
			continue
		}
		if cur < 0 || !isKey {
			continue
		}
		switch key {
		case "rule":
			routers[cur].rule = value
			ruleIndent = indent
		case "priority":
			n, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("%s:%d: priority %q is not an integer (%v) — this guard "+
					"reads priorities to decide which router serves a host, so it "+
					"cannot reason about this table; fix the file or the parser",
					path, lineNo, value, err)
			}
			routers[cur].priority, routers[cur].hasPriority = n, true
		case "entryPoints":
			routers[cur].entryPoints = parseEntryPoints(lines, i, indent, value)
		}
	}

	for i := range routers {
		routers[i].rule = unquoteYAMLScalar(strings.TrimSpace(routers[i].rule))
	}
	return routers
}

// routerPriority is the number Traefik sorts a router by: the explicit
// `priority:` when the file sets one, and the LENGTH OF THE RULE otherwise — the
// documented default
// (https://doc.traefik.io/traefik/reference/routing-configuration/http/routing/rules-and-priority/:
// "the priority is directly equal to the length of the rule ... A value of 0 for
// the priority is ignored"). Both spellings appear in this repo, so assuming
// either one alone would be wrong.
func routerPriority(r traefikRouter) int {
	if r.hasPriority && r.priority != 0 {
		return r.priority
	}
	return len(r.rule)
}

// competingStaticRouters returns the routers from statics that share an entry
// point with w. Routers on different entry points never compete for a request,
// so they must not be compared. A missing entryPoints list is treated as "could
// compete" — the conservative direction for a guard that must not miss a
// shadowed host.
func competingStaticRouters(w traefikRouter, statics []traefikRouter) []traefikRouter {
	var out []traefikRouter
	for _, s := range statics {
		if len(w.entryPoints) == 0 || len(s.entryPoints) == 0 {
			out = append(out, s)
			continue
		}
		for _, we := range w.entryPoints {
			for _, se := range s.entryPoints {
				if we == se {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// wildcardOutranksStatic reports whether the tenant wildcard w WINS a request to
// a statically routed label's host — i.e. whether w can take that host away from
// the router the file gave it.
//
// Traefik serves the matching router with the HIGHEST priority (an explicit
// `priority:` overrides the rule-length default), so the wildcard wins only when
// no static router for that label outranks it on a shared entry point. The live
// file shows both sides of this: `www` (static 10, wildcard 20) IS outranked and
// must be excluded — PR 740's finding, still pinned here — while `docs`/`mcp`
// (static 60/55, wildcard 20) are NOT, which is why the file carries no exclusion
// for them and why a guard demanding one would be wrong about Traefik.
//
// A label with no static router on w's entry point also counts as outranked: the
// wildcard would then be the only router serving a host JomForm owns on that
// port, which is the same wrong-service symptom.
func wildcardOutranksStatic(w traefikRouter, statics []traefikRouter) bool {
	competing := competingStaticRouters(w, statics)
	if len(competing) == 0 {
		return true
	}
	wp := routerPriority(w)
	for _, s := range competing {
		if routerPriority(s) <= wp {
			return true
		}
	}
	return false
}

// staticRoutersFor returns a one-line description of the routers behind a label,
// with the priority each is sorted by, so a failure says WHICH router loses to
// the wildcard instead of only the file and line.
func staticRoutersFor(statics []traefikRouter) string {
	parts := make([]string, 0, len(statics))
	for _, s := range statics {
		parts = append(parts, fmt.Sprintf("%s:%d", s.name, routerPriority(s)))
	}
	return strings.Join(parts, ", ")
}

// --- bound to reality: what Traefik actually routes -------------------------

func TestReservedSubdomainsCoverStaticTraefikHosts(t *testing.T) {
	root := repoRoot(t)
	// The static (file-provider) router table for the VPS deployment. The
	// docker-provider wildcard file is included too so a Host(...) added there
	// cannot escape this guard.
	paths := []string{
		filepath.Join(root, "deploy", "vps", "traefik-jomform.yml"),
		filepath.Join(root, "deploy", "traefik-wildcard.yml"),
	}

	checked, hostRegexps := 0, 0
	for _, path := range paths {
		for _, r := range parseTraefikRouters(t, path) {
			// A regex host is the tenant wildcard, not a static subdomain: it
			// matches the workspace subdomains on purpose. COUNT it — a table
			// with no wildcard would mean a claimed slug is no longer a live
			// host, and this guard's premise would be wrong (card t_1ab825fc
			// is the reason the scan does not `continue` here: a rule line
			// mixing the wildcard with a static Host must still have that Host
			// checked, and joining continued lines keeps the same promise for a
			// literal that is wrapped onto the next line).
			if traefikHostRegexpRe.MatchString(r.rule) {
				hostRegexps++
			}
			for _, m := range traefikHostRe.FindAllStringSubmatch(r.rule, -1) {
				host := m[2]
				checked++
				if tenantOrApexHost(host) {
					continue
				}
				label, ok := jomformLabel(host)
				if !ok {
					// Not under jomform.com. Two legitimate kinds, and one
					// illegitimate: a DECLARED foreign route (a merchant's
					// custom domain — foreignRoutedHosts in subdomains.go), or
					// whatever nobody has looked at.
					if isForeignRoutedHost(host) {
						continue
					}
					t.Errorf("%s:%d router %s routes Host(`%s`), which is not "+
						"under jomform.com and is not declared: this guard cannot "+
						"tell whether the label is claimable. If it is a real "+
						"route, add it to foreignRoutedHosts in subdomains.go "+
						"with the reason it is allowed (the ONE list for non-"+
						"JomForm hosts); if it is not, remove the route — a guard "+
						"extended by deleting this check would stop seeing hosts "+
						"altogether.", path, r.line, r.name, host)
					continue
				}
				if !ReservedSubdomain(label) {
					t.Errorf("%s:%d router %s routes Host(`%s`) statically, but "+
						"%q is NOT in the reserved list — a merchant could claim "+
						"it and shadow this JomForm surface. Add %q to "+
						"reservedSubdomains (with its reason) in subdomains.go.",
						path, r.line, r.name, host, label, label)
				}
			}
		}
	}

	// A guard that found nothing to check is worthless: assert the parse
	// actually saw the router table, not an empty file or a moved key.
	if checked == 0 {
		t.Fatalf("no static Host(...) router found in %v — the traefik config moved "+
			"or its shape changed, so this guard checked nothing", paths)
	}
	// The tenant wildcard must still be there: if it is gone, subdomains are no
	// longer routed by regex at all and this guard's premise is wrong.
	if hostRegexps == 0 {
		t.Fatalf("no HostRegexp(...) router found in %v — the tenant wildcard is the "+
			"thing that makes a claimed slug a live host, and this guard assumes it", paths)
	}
	t.Logf("checked %d static Host(...) literal(s) and %d HostRegexp rule(s)", checked, hostRegexps)
}

// TestTraefikWildcardExcludesStaticallyRoutedHosts is the guard PR 740 needed
// and did not have (card t_d49dae9f), re-founded on what Traefik actually does
// (card t_3f44c437).
//
// THE DEFECT IT PINS (measured on prod, t_ba241aeb): a router that matches
// `HostRegexp(`[a-z0-9-]+.jomform.com`)` matches www.jomform.com — the regex is
// UNANCHORED, so `[a-z0-9-]+` stops at `ww` and the unescaped `.` matches the
// literal `w`; there is no need for the pattern to spell `www.`. The wildcard
// carries priority 20 and the www static router carries priority 10, so the
// wildcard WINS and www.jomform.com was served by the API (Go's `404 page not
// found`) instead of the web app. The `!Host(...)` exclusion is the only thing
// that stops it. Delete the clause — as main did before #740 — and www goes
// back to 404 while every gate stayed green.
//
// WHAT THIS GUARD GOT WRONG (t_3f44c437). It carried the same conclusion as a
// GENERAL rule — "the wildcard's priority beats the static router" — for every
// statically routed host, and red-on-any-file that did not exclude all of them.
// That is true for `www` (10 vs 20) and false for the rest of the live table:
// the wildcard is priority 20 while `docs` is 55/60 and `mcp` is 60, so Traefik
// serves THEIR routers for those hosts and the file correctly carries no
// exclusion for them. Traefik picks the matching router with the HIGHEST
// priority — explicit `priority:`, otherwise the rule's length (see
// routerPriority for the documented rule) — so "does the wildcard win?" is a
// comparison, not an assumption. PR #1167 put the host's real
// 179-line table into the repo as a byte-for-byte copy
// (deploy/vps/drift-check.sh fails if they differ), which is what surfaced it:
// the guard red on a correct file, and because it is the `test` gate it stopped
// every lane on the board.
//
// TWO INVARIANTS, both derived from the files themselves (no host, no network):
//
//  1. AN EXCLUSION NAMES A NAME JOMFORM PROTECTS. Every `!Host(`…`)` on any
//     rule must be the apex, a DECLARED foreign route (foreignRoutedHosts), or a
//     label ReservedSubdomain refuses. The harm an exclusion can do is take a
//     live host off a router, and that can only happen to a label a merchant
//     could actually hold — which is exactly what reservedSubdomains answers.
//     PR 960 drew this from ServingReservedSubdomains instead, which is a
//     strict sub-set (asserted separately): `mcp` is on the claim list and is
//     statically routed in this very file (jf-mcp-root), so the stricter source
//     reds a correct file. The serving list keeps its own job — what the API may
//     resolve to a workspace — and no exclusion legal before becomes illegal:
//     every serving-reserved label is claim-reserved too.
//
//  2. AN EXCLUSION EXISTS WHEREVER THE WILDCARD REALLY WINS. For every label the
//     file also gives its own static `Host(`<label>.jomform.com`)` router, the
//     wildcard must exclude that label IF it outranks those routers on an entry
//     point they share (wildcardOutranksStatic — routers on different entry
//     points never compete, so their priorities are not comparable at all). `www`
//     is the first case (10 <= 20, the pair #740 fixed); `docs` (55/60) and `mcp`
//     (60) are the second, and a wildcard that does NOT exclude them is correct.
//     A NEW static router for a new label whose wildcard exclusion is forgotten
//     at a lower-or-equal priority still fails here, which is the "cannot quietly
//     rot" half. The case where no exclusion is needed is LOGGED with the
//     priorities, so the guard's silence is evidence rather than an absence.
//
// SCOPE, stated so this guard is not over-claimed: it reads the committed files
// only. It cannot see prod, and it does not re-derive prod's 17 routers. It also
// does NOT require an exclusion on deploy/traefik-wildcard.yml (the
// docker-provider file), which carries no static Host(...) routers of its own —
// its two rules are PathPrefix routers. Their unanchored regex does match www, so
// IF that file were ever activated it would shadow www as well; that is a
// property of a file this card was told not to change (Wan, 2026-10-04: "JANGAN
// ubah/regenerate fail traefik mengikut routing"), so it is recorded in the
// completion note rather than enforced here. A test that demanded the exclusion
// on a file nobody may edit would be red on a green tree.
func TestTraefikWildcardExcludesStaticallyRoutedHosts(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "deploy", "vps", "traefik-jomform.yml"),
		filepath.Join(root, "deploy", "traefik-wildcard.yml"),
	}

	exclusions, wildcards, staticRoutes := 0, 0, 0
	for _, path := range paths {
		routers := parseTraefikRouters(t, path)

		// (1) Any `!Host(...)` on ANY rule must name a name JomForm owns or
		//     protects. Applied to every rule, not only wildcards: an exclusion
		//     anywhere in these files takes a jomform label out of a router, and
		//     that label must not be one a merchant could have claimed.
		for _, r := range routers {
			for _, m := range traefikHostRe.FindAllStringSubmatch(r.rule, -1) {
				if m[1] != "!" {
					continue
				}
				exclusions++
				host := m[2]
				if tenantOrApexHost(host) || isForeignRoutedHost(host) {
					continue
				}
				label, ok := jomformLabel(host)
				if !ok {
					t.Errorf("%s:%d router %s excludes Host(`%s`), which is not a "+
						"<label>.jomform.com host and is not a declared foreign "+
						"route: this guard cannot tell whether a merchant holds "+
						"it. Declare it in foreignRoutedHosts (subdomains.go) with "+
						"the reason it is allowed, or remove the exclusion.",
						path, r.line, r.name, host)
					continue
				}
				if !ReservedSubdomain(label) {
					t.Errorf("%s:%d router %s excludes Host(`%s`) from its router, "+
						"but %q is NOT in reservedSubdomains — a merchant CAN "+
						"claim that label, and excluding it would take a real "+
						"merchant's host off this router. If %q really is a "+
						"JomForm name, add it to reservedSubdomains (with its "+
						"reason) in subdomains.go (the ONE list); otherwise "+
						"remove the exclusion.", path, r.line, r.name, host, label, label)
				}
			}
		}

		// (2a) Static (non-negated) routes, per label, WITH the routers behind
		//      them: invariant 2 compares priorities, which needs the routers.
		staticByLabel := map[string][]traefikRouter{}
		for _, r := range routers {
			for _, m := range traefikHostRe.FindAllStringSubmatch(r.rule, -1) {
				if m[1] == "!" {
					continue
				}
				if label, ok := jomformLabel(m[2]); ok {
					staticByLabel[label] = append(staticByLabel[label], r)
					staticRoutes++
				}
			}
		}

		// (2b) The tenant wildcards: a HostRegexp whose pattern is a jomform
		//      host, in either file's escaping. A router counts once however many
		//      HostRegexp matchers its rule carries.
		var wildcardRouters []traefikRouter
		for _, r := range routers {
			for _, m := range traefikHostRegexpRe.FindAllStringSubmatch(r.rule, -1) {
				if strings.Contains(strings.ToLower(m[1]), "jomform") {
					wildcardRouters = append(wildcardRouters, r)
					wildcards++
					break
				}
			}
		}

		// (2c) Every label the wildcard WINS must be excluded from every
		//      wildcard router that wins it.
		for _, w := range wildcardRouters {
			excluded := map[string]bool{}
			for _, m := range traefikHostRe.FindAllStringSubmatch(w.rule, -1) {
				if m[1] != "!" {
					continue
				}
				if label, ok := jomformLabel(m[2]); ok {
					excluded[label] = true
				}
			}
			for label, statics := range staticByLabel {
				// The log line is the evidence for the cases this guard does NOT
				// demand an exclusion on — the reader should not have to guess
				// whether the check ran or the label was skipped.
				if !wildcardOutranksStatic(w, statics) {
					t.Logf("%s:%d wildcard %s (priority %d) does not exclude `%s`, "+
						"and does not need to: its static routers outrank it (%s) "+
						"on a shared entry point, so it cannot serve that host",
						path, w.line, w.name, routerPriority(w), label,
						staticRoutersFor(statics))
					continue
				}
				if excluded[label] {
					continue
				}
				t.Errorf("%s:%d wildcard %s (priority %d) does NOT exclude `%s`, "+
					"and the same file routes Host(`%s.jomform.com`) statically at "+
					"line %d without a higher priority (%s). Traefik serves the "+
					"router with the higher priority, so %s.jomform.com would be "+
					"served by the wildcard's service instead — this is the exact "+
					"shadowing PR 740 fixed for www. Add `&& !Host(`%s.jomform.com`)` "+
					"to this rule, or raise the static router's priority above %d.",
					path, w.line, w.name, routerPriority(w), label, label,
					statics[0].line, staticRoutersFor(statics), label, label,
					routerPriority(w))
			}
		}
	}

	// A guard that checked nothing proves nothing. Each of these three counts
	// being zero means the parse missed the file's shape, not that the file is
	// correct — so say so rather than passing vacuously.
	if exclusions == 0 {
		t.Fatalf("no !Host(...) exclusion found in %v — the www wildcard "+
			"exclusion PR 740 added is gone (or its shape changed); this guard "+
			"is now checking nothing", paths)
	}
	if wildcards == 0 {
		t.Fatalf("no tenant wildcard (HostRegexp on jomform.com) found in %v — "+
			"the parse missed it, so this guard checked nothing", paths)
	}
	if staticRoutes == 0 {
		t.Fatalf("no static Host(`<label>.jomform.com`) route found in %v — the "+
			"parse missed it, so the exclusion-coverage check is vacuous", paths)
	}
	t.Logf("checked %d exclusion(s), %d wildcard rule(s), %d static route(s)",
		exclusions, wildcards, staticRoutes)
}

// --- one rule, one implementation ------------------------------------------
//
// WHERE THE TWO HALVES OF "ONE RULE" ARE ASSERTED, and why the caller half is no
// longer restated here.
//
//  1. THAT THE TRANSPORTS DELEGATE. dbstore.ValidateWorkspaceSlug is the ONE slug
//     rule and the REST and MCP writers call it — asserted in the package that
//     owns the rule, over a set DERIVED by walking api/:
//     api/internal/dbstore/workspace_slug_rule_guard_test.go,
//     TestWorkspaceSlugRuleHasExactlyOneDefinition.
//  2. THAT THE RULE CONSULTS THIS PACKAGE'S LIST — asserted below, and in that
//     same dbstore file too (TestTheReservedLegLivesInTheOneRule, which pins the
//     shared refusal wording beside it).
//
// Half 1 used to be restated HERE, as a fixed pair of paths checked with
// strings.Contains. That spelling is the defect this file's own comment used to
// describe, not a cheaper guard: it is satisfied by the files it names and blind
// to every other one. Measured before this change — a caller of the rule in a
// brand-new file (api/internal/httpapi/qa_sixth_caller.go) compiled clean
// (`go build ./...` RC=0) and left TestBothSlugValidatorsConsumeThisRule GREEN,
// while the dbstore guard's walk red the same file by path. Restating the fact
// here would also mean a second hand-written list of paths for ONE fact — two
// things to keep in step, the class of defect this section exists to prevent. So
// the assertion lives where the set is derived, and this comment stands where a
// check that could not see the drift it claimed to catch used to be.

// TestTheOneRuleConsumesThisList pins the direction that belongs to this package:
// the reserved-name refusal is enforced INSIDE the shared rule, so a caller that
// reaches the rule inherits it instead of each transport restating the list.
//
// HOW IT READS (card t_b0f76a19). It used to read the rule file as TEXT and look
// for "urlguard.ReservedSubdomain(", so a COMMENT satisfied it — measured with
// the leg disabled and its call text left behind in a comment: the rule built
// clean (`go build ./...` RC=0) and this test stayed GREEN. It now PARSES the
// rule with comments dropped (`parser.ParseFile(..., 0)`) and requires the CALL
// inside ValidateWorkspaceSlug's own body, resolving the package from the rule
// file's import of THIS package — so renaming the import is not read as drift
// while some other package's ReservedSubdomain is not accepted as this one.
//
// Its remaining limits, stated rather than implied: it proves the call EXISTS
// inside the rule, not that the refusal behaves correctly (a reserved label
// refused with the shared wording, before the shape leg, is asserted in
// dbstore/workspace_slug_reserved_sentinel_test.go); and a call reached through a
// variable that holds this function (`f := urlguard.ReservedSubdomain`) is not a
// qualified CallExpr and would not be seen. Which files CALL the rule is not
// this test's subject; see the section comment above.
func TestTheOneRuleConsumesThisList(t *testing.T) {
	root := repoRoot(t)
	rule := filepath.Join("api", "internal", "dbstore", "workspace_slug.go")
	src := mustReadFile(t, filepath.Join(root, rule))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, rule), src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v — this guard cannot run without it", rule, err)
	}

	var names []string
	for _, imp := range file.Imports {
		p, perr := strconv.Unquote(imp.Path.Value)
		if perr != nil || p != thisPackagePath {
			continue
		}
		if imp.Name != nil {
			names = append(names, imp.Name.Name)
			continue
		}
		names = append(names, "urlguard")
	}
	if len(names) == 0 {
		t.Fatalf("%s no longer imports %s — the reserved-name rule has left the one rule, "+
			"and this guard would otherwise be checking nothing", rule, thisPackagePath)
	}

	body := funcBodyNamed(file, "ValidateWorkspaceSlug")
	if body == nil {
		t.Fatalf("no `func ValidateWorkspaceSlug` in %s — the guard's anchor moved", rule)
	}
	if !callsQualified(body, names, "ReservedSubdomain") {
		t.Errorf("%s does not CALL urlguard.ReservedSubdomain inside ValidateWorkspaceSlug "+
			"(parsed with comments dropped, so a comment naming the call does not count) — the "+
			"reserved-name rule has drifted into a second (or missing) implementation there", rule)
	}
}

// thisPackagePath is the import path a rule must use to reach this package's
// list. Matching the PATH rather than the identifier "urlguard" means an aliased
// import is a legitimate rename, not drift.
const thisPackagePath = "github.com/wzul/jomform/api/internal/urlguard"

// funcBodyNamed returns the body of the top-level func called name, or nil when
// there is none — a nil is a failure at the call site, never a pass.
func funcBodyNamed(file *ast.File, name string) *ast.BlockStmt {
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name != nil && fn.Name.Name == name {
			return fn.Body
		}
	}
	return nil
}

// callsQualified reports whether node contains a CALL to <one of pkg>.<fn>.
// Comments are absent from a parsed tree, so text describing the call is never a
// call.
func callsQualified(node ast.Node, pkg []string, fn string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != fn {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		for _, name := range pkg {
			if id.Name == name {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// TestTheRuleAndTransportsHoldNoCopyOfTheReservedList is the tripwire for the
// copy-paste failure the section above is about. reservedSubdomains is unexported,
// so the compiler already stops another package from NAMING the list through this
// package; what it cannot stop is the list being pasted into the rule or a
// transport under the same name, which is how a second copy would start. Any
// mention in these three files fails, code or comment.
//
// Scope, stated so it is not over-claimed: it reads the rule and the two
// transports by path. A copy under that name in a fourth file is not seen here,
// and a copied list under a DIFFERENT name is not seen by any source read (the
// behavioural tests are what catch a rule that is wrong rather than duplicated).
func TestTheRuleAndTransportsHoldNoCopyOfTheReservedList(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("api", "internal", "dbstore", "workspace_slug.go"),
		filepath.Join("api", "internal", "httpapi", "rest_workspaces.go"),
		filepath.Join("api", "internal", "mcpsrv", "workspaces.go"),
	} {
		src := mustReadFile(t, filepath.Join(root, rel))
		if strings.Contains(src, "reservedSubdomains") {
			t.Errorf("%s refers to the package-private list directly — the list must "+
				"stay inside urlguard so there is exactly one copy", rel)
		}
	}
}

func TestReservedListIsSortedForReview(t *testing.T) {
	// Not a correctness rule — a review aid, and the reason the file's categories
	// read as groups: if the list is rewritten wholesale, this says so.
	labels := make([]string, 0, len(reservedSubdomains))
	for label := range reservedSubdomains {
		labels = append(labels, label)
	}
	t.Logf("%d reserved subdomain labels: %s", len(labels), strings.Join(sorted(labels), " "))
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// --- the jomform- prefix (card t_40338d5f) ---------------------------------

// TestJomFormPrefixIsReserved is the guard for the hole the label list left open.
//
// The list above is a set of LABELS, and JomForm's own names live under more than
// one of them ("templates" is listed, but the host JomForm actually serves the
// gallery on is `jomform-templates.jomform.com`, and the `jomform-` prefix spans
// namespaces). Measured on the shipped tree before the fix: claiming a workspace
// with the slug `jomform-templates` answered 201 over the real POST /v1/workspaces
// route and the claimed workspace then served the merchant's storefront at that
// host — a live JomForm surface shadowed by a tenant. Enumerating names one by one
// is what missed it, so the reservation is a PREFIX.
//
// The negative legs matter as much as the positive ones: a prefix rule that also
// swallowed `my-jomform` or `jomformx` would take legitimate merchant addresses,
// and `workspaces.slug LIKE 'jomform-%'` returns ZERO rows, so the positive legs
// cost no existing merchant anything.
func TestJomFormPrefixIsReserved(t *testing.T) {
	reserved := []string{
		"jomform-templates",   // the measured defect
		"jomform-templates-2", // the same hole one keystroke away
		"jomform-anything",    // any future JomForm name
		"JOMFORM-TEMPLATES",   // the rule normalises before comparing
		"  jomform-templates", // ... and trims
	}
	for _, label := range reserved {
		if !ReservedSubdomain(label) {
			t.Errorf("ReservedSubdomain(%q) = false, want true — JomForm's own surfaces "+
				"are named under the `jomform-` prefix, and a workspace that claims one "+
				"serves its own storefront on a live JomForm host (t_40338d5f)", label)
		}
	}
	// A prefix rule must not be greedy: these are legitimate merchant addresses.
	allowed := []string{
		"my-jomform", // contains the word, does not start with the prefix
		"jomformx",   // starts with the letters, not the prefix (no hyphen)
		"templates2", // not the listed label and not under the prefix
		"jomform",    // the prefix itself without the hyphen is a different name
	}
	for _, label := range allowed {
		if ReservedSubdomain(label) {
			t.Errorf("ReservedSubdomain(%q) = true, want false — the prefix rule must "+
				"reserve `jomform-` and NOT every name containing it, or it takes "+
				"legitimate merchant addresses", label)
		}
	}
}

// --- the ONE serving list (card t_d49dae9f) --------------------------------

// TestServingReservedSubdomainsIsTheOneDeclaration pins the source of the
// serving-side decision. Before this card `www app api mail s3` was written down
// three times — here, in httpapi/public.go as a literal `sub == "www" || …`
// chain, and as `!Host(...)` exclusions in the Traefik wildcards — and nothing
// tied the three together. Now Go has exactly one copy, and this test fails if a
// consumer grows its own.
//
// Two things are asserted, and both are the card's requirement made mechanical:
//
//  1. accountSubdomain (httpapi/public.go) CONSUMES this package's predicate. A
//     re-inlined literal chain would pass every behavioural test and still be the
//     drifted copy, so the assertion is on the SOURCE, not on behaviour.
//  2. The serving list is a non-empty set of bare labels and is a SUBset of the
//     claim list. A serving-reserved name is by definition one JomForm owns, so
//     it must also be unclaimable; a name served-but-claimable would let a
//     merchant take a host JomForm answers on. (The reverse does NOT hold — the
//     claim list is deliberately larger, carrying the impersonation names.)
func TestServingReservedSubdomainsIsTheOneDeclaration(t *testing.T) {
	if len(ServingReservedSubdomains) == 0 {
		t.Fatal("ServingReservedSubdomains is empty — a serving guard that names " +
			"nothing proves nothing")
	}
	valid := regexp.MustCompile(`^[a-z0-9-]+$`)
	for _, label := range ServingReservedSubdomains {
		if !valid.MatchString(label) {
			t.Errorf("ServingReservedSubdomains entry %q is not a bare lower-case "+
				"label (a-z, 0-9, dash) — it can never match a Host label", label)
		}
		if !ReservedSubdomain(label) {
			t.Errorf("ServingReservedSubdomains carries %q but ReservedSubdomain(%q) "+
				"is false — a host JomForm serves must also be unclaimable, or a "+
				"merchant could take it and shadow JomForm's own deployment", label, label)
		}
	}

	// (1) The consumer reads the ONE predicate. Assert on the source so a
	//     re-inlined copy cannot hide behind green behaviour.
	root := repoRoot(t)
	public := filepath.Join("api", "internal", "httpapi", "public.go")
	src := mustReadFile(t, filepath.Join(root, public))
	if !strings.Contains(src, "urlguard.IsServingReserved(") {
		t.Errorf("%s does not call urlguard.IsServingReserved — accountSubdomain has "+
			"grown its own copy of the serving list again, which is the drift this "+
			"card exists to close", public)
	}
	// The old literal chain must be GONE from public.go. If it survives next to
	// the call, the two can disagree.
	if strings.Contains(src, `sub == "www"`) {
		t.Errorf(`%s still carries the literal chain (sub == "www" ...) — the serving `+
			`list must live ONLY in urlguard.ServingReservedSubdomains`, public)
	}
}

// TestIsServingReservedIsTheOnePredicate checks the predicate itself, including
// the normalisation a caller must not have to remember.
func TestIsServingReservedIsTheOnePredicate(t *testing.T) {
	for _, label := range []string{"www", "app", "api", "mail", "s3", " WWW ", "Api"} {
		if !IsServingReserved(label) {
			t.Errorf("IsServingReserved(%q) = false, want true", label)
		}
	}
	// A merchant host — and, importantly, the impersonation labels that are
	// CLAIM-reserved but NOT serving-reserved: JomForm does not answer on them,
	// so they must resolve as tenants once legitimately claimed.
	for _, label := range []string{"wan", "acme", "billing", "login", "docs", "mcp", ""} {
		if IsServingReserved(label) {
			t.Errorf("IsServingReserved(%q) = true, want false — the serving list must "+
				"stay the deployment's own hosts, not the wider claim list", label)
		}
	}
}

// --- the foreign-host declaration (card t_3f44c437) -------------------------

// TestForeignRoutedHostsAreDeclaredAndNarrow is the other half of the fix the
// Traefik guard needed: `halo.wanzul-hosting.com` is a real merchant custom
// domain in the live table, so it is declared rather than waved through.
//
// The declaration is an EXCEPTION list, which is exactly the kind of thing that
// rots — so it is held to the same standard as reservedSubdomains: every entry
// says WHY it is allowed, the match is exact (not a suffix, or
// `halo.wanzul-hosting.com.evil.tld` would read as the declared host), and an
// entry that belongs to the LABEL path (anything under jomform.com) is refused
// here, because declaring one would be a hole in the claim guard rather than a
// documented exception.
func TestForeignRoutedHostsAreDeclaredAndNarrow(t *testing.T) {
	validHost := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	for host, reason := range foreignRoutedHosts {
		if !validHost.MatchString(host) {
			t.Errorf("foreignRoutedHosts key %q is not a bare lower-case host "+
				"(labels of a-z, 0-9, dash) — it can never match a Host(`…`) "+
				"literal in the Traefik table", host)
		}
		if host == "jomform.com" || strings.HasSuffix(host, ".jomform.com") {
			t.Errorf("foreignRoutedHosts carries %q, which IS a jomform.com host: "+
				"those are classified by LABEL through reservedSubdomains, and an "+
				"entry here would let a claimable label bypass the claim guard "+
				"(it is the list for hosts that are NOT under jomform.com)", host)
		}
		if len(strings.TrimSpace(reason)) < 20 {
			t.Errorf("foreignRoutedHosts[%q] reason %q is too thin to review — an "+
				"exception nobody can check is an exception nobody can remove",
				host, reason)
		}
	}

	// The predicate normalises the way the rest of this package does, and it is
	// an EXACT match: a host that merely ends with a declared one is not it.
	for _, host := range []string{
		"halo.wanzul-hosting.com", " HALO.Wanzul-Hosting.com ", "halo.wanzul-hosting.com	",
	} {
		if !isForeignRoutedHost(host) {
			t.Errorf("isForeignRoutedHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{
		"", "jomform.com", "www.jomform.com", "wanzul-hosting.com",
		"halo.wanzul-hosting.com.evil.example", "evil.example.com",
	} {
		if isForeignRoutedHost(host) {
			t.Errorf("isForeignRoutedHost(%q) = true, want false — the declaration "+
				"is an exact, normalised match on a host the edge really routes, "+
				"not a suffix rule and not a blanket allowance for foreign hosts",
				host)
		}
	}
}

// --- the priority comparison, exercised on its own --------------------------

// TestWildcardOutranksStaticIsAPriorityComparison is the control for the rule the
// shadowing invariant rests on, so the contrast cannot rot: the SAME wildcard
// outranks a static router at priority 10 (www — the pair PR 740 fixed) and does
// NOT outrank one at 30, 55 or 60 (the `docs`/`mcp` routers in the live table).
// A guard that hard-coded either answer — which is what "the wildcard's priority
// beats the static router" did — fails here on the other case.
func TestWildcardOutranksStaticIsAPriorityComparison(t *testing.T) {
	wildcard := traefikRouter{
		name:        "jf-wildcard",
		rule:        "HostRegexp(`[a-z0-9-]+.jomform.com`) && !Host(`www.jomform.com`)",
		priority:    20,
		hasPriority: true,
		entryPoints: []string{"websecure"},
	}
	static := func(name string, priority int, eps ...string) traefikRouter {
		return traefikRouter{name: name, priority: priority, hasPriority: true,
			rule:        fmt.Sprintf("Host(`%s.jomform.com`)", name),
			entryPoints: eps}
	}

	for _, tc := range []struct {
		what    string
		statics []traefikRouter
		want    bool
	}{
		{"a static router at 10 is beaten by the 20 wildcard (www)", []traefikRouter{static("jf-web", 10, "websecure")}, true},
		{"a tie at 20 counts as beaten — the outcome must not rest on the tiebreak", []traefikRouter{static("jf-tie", 20, "websecure")}, true},
		{"a static router at 30 wins (www's /api router)", []traefikRouter{static("jf-web-api", 30, "websecure")}, false},
		{"docs: both of its routers win (60, 55)", []traefikRouter{static("jf-docs-root", 60, "websecure"), static("jf-docs-prefix", 55, "websecure")}, false},
		{"one beaten router among winners decides it (www has jf-web at 10 as well)", []traefikRouter{static("jf-healthz", 60, "websecure"), static("jf-web", 10, "websecure")}, true},
		{"a router on another entry point does not compete", []traefikRouter{static("jf-web-fallback", 1, "web")}, true},
		{"one competing router and one elsewhere: only the competing one counts", []traefikRouter{static("jf-web-fallback", 1, "web"), static("jf-web", 30, "websecure")}, false},
	} {
		if got := wildcardOutranksStatic(wildcard, tc.statics); got != tc.want {
			t.Errorf("wildcardOutranksStatic(%s) = %v, want %v", tc.what, got, tc.want)
		}
	}

	// A router with no explicit priority is sorted by the LENGTH OF ITS RULE,
	// the documented Traefik default — so the comparison must still work when a
	// future router omits `priority:`. Both files set it explicitly today, which
	// is exactly why this leg is easy to lose.
	short, long := traefikRouter{rule: strings.Repeat("x", 12)}, traefikRouter{rule: strings.Repeat("y", 40)}
	if got := routerPriority(short); got != 12 {
		t.Errorf("routerPriority(rule of 12 chars, no explicit priority) = %d, want 12", got)
	}
	if !wildcardOutranksStatic(wildcard, []traefikRouter{short}) {
		t.Errorf("the 20 wildcard must beat a router whose default priority is 12")
	}
	if wildcardOutranksStatic(wildcard, []traefikRouter{long}) {
		t.Errorf("a router whose default priority is 40 must beat the 20 wildcard")
	}
}

// TestParseTraefikRoutersReadsTheCommittedTable pins the parse against the file
// the host loads, so a shape the parser cannot read is a failure here rather than
// a guard that quietly checks nothing (each test's counters cover the "zero
// routers" case; this covers "read it wrong").
func TestParseTraefikRoutersReadsTheCommittedTable(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "deploy", "vps", "traefik-jomform.yml")
	routers := parseTraefikRouters(t, path)
	if len(routers) < 10 {
		t.Fatalf("parsed %d router(s) from %s — the table moved, or the parser "+
			"stopped reading it (the guards above would then be checking nothing)",
			len(routers), path)
	}
	byName := map[string]traefikRouter{}
	for _, r := range routers {
		if _, dup := byName[r.name]; dup {
			t.Errorf("%s parsed twice under the name %s", path, r.name)
		}
		byName[r.name] = r
	}

	w, ok := byName["jf-wildcard"]
	if !ok {
		t.Fatalf("%s has no jf-wildcard router — the tenant wildcard is what the "+
			"shadowing guard exists for", path)
	}
	if got := routerPriority(w); got != 20 {
		t.Errorf("jf-wildcard priority = %d, want 20 (the value the file sets)", got)
	}
	// The file writes entryPoints as a BLOCK sequence; the v2-style sibling wrote
	// it as a flow sequence. Both must read the same, or every entry-point
	// comparison silently degrades to "compare everything".
	if len(w.entryPoints) != 1 || w.entryPoints[0] != "websecure" {
		t.Errorf("jf-wildcard entryPoints = %v, want [websecure]", w.entryPoints)
	}
	if got := parseEntryPoints([]string{"      entryPoints: [websecure, web]"}, 0, 6, "[websecure, web]"); len(got) != 2 || got[0] != "websecure" || got[1] != "web" {
		t.Errorf("parseEntryPoints(flow sequence) = %v, want [websecure web]", got)
	}

	// jf-custom-root's value is FOLDED onto a second line in the file (and
	// single-quoted): it must arrive whole and unquoted, or the exclusions on it
	// are half-read.
	cr := byName["jf-custom-root"]
	for _, want := range []string{"!Host(`jomform.com`)", "!Host(`www.jomform.com`)", "!Host(`mcp.jomform.com`)", "PathPrefix(`/`)"} {
		if !strings.Contains(cr.rule, want) {
			t.Errorf("jf-custom-root rule = %q, want it to contain %q (its value is "+
				"folded onto the next line in the file)", cr.rule, want)
		}
	}
	if strings.ContainsAny(cr.rule, `'"`) {
		t.Errorf("jf-custom-root rule still carries YAML quoting: %q", cr.rule)
	}
	if !strings.Contains(byName["jf-api-f"].rule, "PathPrefix(`/healthz`)") {
		t.Errorf("jf-api-f rule = %q — its continued lines were not joined",
			byName["jf-api-f"].rule)
	}
	if got := routerPriority(byName["jf-docs-prefix"]); got != 55 {
		t.Errorf("jf-docs-prefix priority = %d, want 55", got)
	}

	// The docker-provider file: two PathPrefix routers, one entry point each.
	wcs := parseTraefikRouters(t, filepath.Join(root, "deploy", "traefik-wildcard.yml"))
	if len(wcs) != 2 {
		t.Fatalf("parsed %d router(s) from deploy/traefik-wildcard.yml, want 2", len(wcs))
	}
	for _, r := range wcs {
		if len(r.entryPoints) != 1 || r.entryPoints[0] != "websecure" {
			t.Errorf("%s entryPoints = %v, want [websecure]", r.name, r.entryPoints)
		}
		if strings.ContainsAny(r.rule, `'"`) {
			t.Errorf("%s rule still carries YAML quoting: %q", r.name, r.rule)
		}
	}
}
