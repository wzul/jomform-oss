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
// second list in the other validator. TestBothSlugValidatorsConsumeThisRule
// reads the REST and MCP sources and fails if either stops calling into this
// package.
//
// THE LIST'S COVERAGE LIMIT, stated so this is not over-claimed: it proves the
// labels that are WRITTEN DOWN are routed-or-sensitive and reachable from both
// validators. It cannot know that some other sensitive name exists (a new
// product name, a new mail alias). That is a human review decision, and this is
// the file that makes it cheap to make.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
		src := mustReadFile(t, path)
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			// Only rule lines carry hosts; comments are skipped so a hostname
			// discussed in a comment is not mistaken for a routed one.
			if !strings.HasPrefix(trimmed, "rule:") {
				continue
			}
			if strings.Contains(trimmed, "HostRegexp(") {
				// A regex host is the tenant wildcard, not a static subdomain:
				// it matches the workspace subdomains on purpose.
				//
				// COUNT it, then KEEP GOING (card t_1ab825fc). This used to
				// `continue`, which skipped the ENTIRE rule line — so a line
				// mixing the wildcard with a static host hid that host from the
				// check below. Not hypothetical: the committed config already has
				//   HostRegexp(`[a-z0-9-]+.jomform.com`) && !Host(`www.jomform.com`)
				// so `www` was never checked on those lines. A router whose static
				// host is neither the apex nor reserved is exactly the drift this
				// guard exists to catch, and it was invisible whenever it shared a
				// line with the wildcard.
				//
				// traefikHostRe matches `Host(`…`)` and NOT `HostRegexp(` (the
				// characters after `Host` differ), so letting the line through
				// finds only its static literals. A negated `!Host(...)` counts on
				// purpose: the label still has to be one JomForm owns or protects,
				// which is why `www` is reserved at all.
				hostRegexps++
			}
			for _, m := range traefikHostRe.FindAllStringSubmatch(trimmed, -1) {
				host := m[2]
				checked++
				if tenantOrApexHost(host) {
					continue
				}
				label := strings.TrimSuffix(strings.ToLower(host), ".jomform.com")
				if label == strings.ToLower(host) {
					t.Errorf("%s routes Host(`%s`), which is not under jomform.com — "+
						"this guard only understands JomForm subdomains, so it cannot "+
						"tell whether the label is claimable. Extend the guard.", path, host)
					continue
				}
				if !ReservedSubdomain(label) {
					t.Errorf("%s routes Host(`%s`) statically, but %q is NOT in the "+
						"reserved list — a merchant could claim it and shadow this "+
						"JomForm surface. Add %q to reservedSubdomains (with its "+
						"reason) in subdomains.go.", path, host, label, label)
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
// and did not have (card t_d49dae9f). Its whole point is that the two-line
// `&& !Host(`www.jomform.com`)` clause on the tenant wildcard cannot be deleted
// again in silence.
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
// TWO INVARIANTS, both derived from the files themselves (no host, no network):
//
//  1. EXCLUSION IS DRAWN FROM THE ONE SOURCE. Every `!Host(`<label>.jomform.com`)`
//     exclusion must name a urlguard.ServingReservedSubdomain. An exclusion of a
//     name that is NOT on that list would take a real merchant's host off the
//     wildcard — a workspace would stop serving — so it is RED. The list is now
//     the ONE declaration shared with the Go serving check (accountSubdomain
//     consumes it too), which is what makes "the next reserved name cannot be
//     added to one side only" mechanical rather than aspirational.
//
//  2. EXCLUSION COVERS EVERY STATICALLY ROUTED HOST. For every label the file
//     ALSO gives its own static `Host(`<label>.jomform.com`)` router, the file's
//     wildcard routers must exclude that label. `www` has four such routers
//     (jf-web, jf-healthz, jf-web-api, jf-web-fallback), so www must be excluded
//     from both jf-wildcard (:443) and jf-wildcard-http (:80) — exactly the pair
//     #740 fixed. A NEW static router for a new label whose wildcard exclusion is
//     forgotten fails here, which is the "cannot quietly rot" half.
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
		src := mustReadFile(t, path)

		type ruleLine struct {
			no   int
			text string
		}
		var wildcardRules []ruleLine
		staticLabels := map[string]int{} // label -> the line that routes it statically

		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "rule:") {
				continue
			}
			lineNo := i + 1

			// (1) Any `!Host(...)` on ANY rule must name a name JomForm owns.
			//     Applied to every rule, not only wildcards: an exclusion
			//     anywhere in these files takes a jomform label out of a
			//     router, and that label must be one JomForm serves.
			for _, m := range traefikHostRe.FindAllStringSubmatch(trimmed, -1) {
				if m[1] != "!" {
					continue
				}
				exclusions++
				label, ok := jomformLabel(m[2])
				if !ok {
					t.Errorf("%s:%d excludes Host(`%s`), which is not a "+
						"<label>.jomform.com host — this guard only understands "+
						"JomForm subdomains. Extend the guard.", path, lineNo, m[2])
					continue
				}
				if !IsServingReserved(label) {
					t.Errorf("%s:%d excludes Host(`%s`) from its router, but %q is "+
						"NOT in urlguard.ServingReservedSubdomains — excluding a "+
						"label JomForm does not serve would take a real merchant's "+
						"host off this router. If %q really is a JomForm host, add "+
						"it to ServingReservedSubdomains in subdomains.go (the ONE "+
						"list); otherwise remove the exclusion.", path, lineNo, m[2], label, label)
				}
			}

			// (2) Static (non-negated) routes for a jomform label.
			for _, m := range traefikHostRe.FindAllStringSubmatch(trimmed, -1) {
				if m[1] == "!" {
					continue
				}
				if label, ok := jomformLabel(m[2]); ok {
					staticLabels[label] = lineNo
					staticRoutes++
				}
			}

			// Is this rule a tenant wildcard? A HostRegexp whose pattern is a
			// jomform host, in either file's escaping.
			for _, m := range traefikHostRegexpRe.FindAllStringSubmatch(trimmed, -1) {
				if strings.Contains(strings.ToLower(m[1]), "jomform") {
					wildcardRules = append(wildcardRules, ruleLine{no: lineNo, text: trimmed})
					wildcards++
				}
			}
		}

		// (2, continued) Every statically routed label must be excluded from
		// every wildcard rule in the SAME file.
		for _, wr := range wildcardRules {
			excluded := map[string]bool{}
			for _, m := range traefikHostRe.FindAllStringSubmatch(wr.text, -1) {
				if m[1] != "!" {
					continue
				}
				if label, ok := jomformLabel(m[2]); ok {
					excluded[label] = true
				}
			}
			for label, atLine := range staticLabels {
				if excluded[label] {
					continue
				}
				t.Errorf("%s:%d is a tenant wildcard (HostRegexp on jomform.com) "+
					"that does NOT exclude `%s`, but the same file routes "+
					"Host(`%s.jomform.com`) statically at line %d. The wildcard's "+
					"priority beats the static router, so %s.jomform.com would be "+
					"served by the wildcard's service instead — this is the exact "+
					"shadowing PR 740 fixed for www. Add `&& !Host(`%s.jomform.com`)` "+
					"to this rule.", path, wr.no, label, label, atLine, label, label)
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

func TestBothSlugValidatorsConsumeThisRule(t *testing.T) {
	// The card's first requirement, made mechanical: the reserved-name rule must
	// be consumed through ONE implementation, and a future edit that adds a
	// second, hand-rolled list anywhere must fail here.
	//
	// WHERE THE ONE IMPLEMENTATION IS. dbstore.ValidateWorkspaceSlug is the
	// shared slug rule (PR 691 made both writers delegate to it), and it is the
	// thing that consumes this package's list — so the reserved leg lives in ONE
	// function, and the two transports reuse it rather than each calling in.
	// That is a STRONGER form of the same requirement: with two callers, the
	// list could still be bypassed by one of them re-implementing the check, so
	// the transports are asserted to DELEGATE, not merely to mention urlguard.
	root := repoRoot(t)
	rule := filepath.Join("api", "internal", "dbstore", "workspace_slug.go")
	src := mustReadFile(t, filepath.Join(root, rule))
	if !strings.Contains(src, "urlguard.ReservedSubdomain(") {
		t.Errorf("%s does not call urlguard.ReservedSubdomain — the reserved-name "+
			"rule has drifted into a second (or missing) implementation there", rule)
	}
	for _, rel := range []string{
		filepath.Join("api", "internal", "httpapi", "rest_workspaces.go"),
		filepath.Join("api", "internal", "mcpsrv", "workspaces.go"),
	} {
		src := mustReadFile(t, filepath.Join(root, rel))
		if !strings.Contains(src, "dbstore.ValidateWorkspaceSlug(") {
			t.Errorf("%s does not call dbstore.ValidateWorkspaceSlug — it either "+
				"stopped validating the slug or grew its own copy of the rule, and "+
				"the reserved-name rule is then no longer one implementation", rel)
		}
	}
	// The private list must stay private: no caller may reach into it, or there
	// are two things to keep in step again.
	for _, rel := range []string{
		rule,
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
