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

// traefikHostRe matches the Host(`x`) forms inside a Traefik rule string.
var traefikHostRe = regexp.MustCompile("Host\\(`([^`]+)`\\)")

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
				hostRegexps++
				continue
			}
			for _, m := range traefikHostRe.FindAllStringSubmatch(trimmed, -1) {
				host := m[1]
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
