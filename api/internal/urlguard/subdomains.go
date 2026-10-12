// Reserved subdomain labels — the ONE list, and the ONE refusal.
//
// WHY THIS EXISTS (t_586ff0c3)
//
// A workspace slug becomes a public host: <slug>.jomform.com, served by the
// tenant wildcard router. Nothing checked WHAT a merchant claimed, so any name
// answered 200 — measured on prod before this change: support, status, billing,
// help, admin, blog, account, login, payments, checkout, cdn and assets all
// returned 200 from the tenant wildcard, and docs/mcp (real JomForm subdomains
// with their own static Traefik routers) would have been SHADOWED by a claim.
// That is an impersonation surface: billing.jomform.com asking a buyer for card
// details reads as JomForm, and a workspace owner is the only person needed to
// create it. Wan's rule: a merchant must not be able to claim a sensitive name
// even while JomForm is not using it.
//
// The slug is validated in two places (REST httpapi/rest_workspaces.go and MCP
// mcpsrv/workspaces.go) and they used to be near-identical copies. The list and
// the refusal live HERE so there is one rule with one implementation, exactly
// like the scheme rule above: adding the check in each validator would invite a
// second, drifting copy (a name added on one side only would still be
// claimable through the other).
//
// This is NOT the full serving-side list. accountSubdomain (httpapi/public.go)
// decides which hosts JomForm will not RESOLVE to an account, it is only
// consulted while serving a request, and it deliberately does NOT carry the
// sensitive/impersonation names — so it could never stop a claim: a workspace
// could still own `api` while every request to it fell through. Both rules are
// needed and they answer different questions:
//
//	accountSubdomain   — what may this Host header resolve to?  (serving)
//	reservedSubdomains — what may a workspace CLAIM?            (claim, here)
//
// BUT the two are not independent copies (card t_d49dae9f). The serving set used
// to be a bare literal chain inside accountSubdomain — `sub == "www" || sub ==
// "app" || ...` — a third declaration of the same names, in a package that could
// not see this file and that no test tied back to it. `www app api mail s3` was
// therefore written down in three places that could not see each other: this
// file, httpapi/public.go, and the `!Host(...)` exclusions in the Traefik
// wildcard routers. It is now declared ONCE, here, as
// ServingReservedSubdomains:
//
//	urlguard.ServingReservedSubdomains — the ONE declaration
//	  ├── accountSubdomain (httpapi/public.go) consumes it, so Go has one copy
//	  └── subdomains_test.go asserts the Traefik wildcard exclusions are drawn
//	      from it — a name excluded from the tenant wildcard that is NOT on this
//	      list would take a live merchant host away, so it is RED.
//
// There is no rename path for workspaces.slug (the only SQL that writes it is
// the INSERT in dbstore.WorkspaceStore.Create, and no REST/MCP route updates
// the column), so gating creation is sufficient.
package urlguard

import (
	"fmt"
	"strings"
)

// ReservedSubdomainCode is the stable failure token the REST refusal carries
// alongside its message, so the dashboard never has to parse prose to tell a
// reserved name apart from a malformed one (the same shape as
// dbstore.PublishWarningNoProducts). web/src/lib/workspaceSlug.ts mirrors this
// value and a cross-language test fails if the two drift.
const ReservedSubdomainCode = "reserved_subdomain"

// reservedSubdomains is the ONE list of subdomain labels a workspace may not
// claim. The value of each entry is WHY it is on the list — the set is meant to
// be read, and a reason is required so the list cannot be padded silently (a
// test fails on an entry with no reason). Labels are lower-case and carry no
// dot: they are the `<label>` of `<label>.jomform.com`.
//
// Deliberately NOT here:
//   - the apex itself (`jomform.com`), which is not a label a slug could be;
//   - account slugs (`accounts.slug`), a different namespace that happens to
//     include `jomform-templates`;
//   - a name whose only protection is the workspaces.slug UNIQUE constraint.
//     A row is not a policy: delete that workspace and the name is claimable
//     again, which is the hole this list closes. `templates` is on the list for
//     exactly that reason even though JomForm's own account will hold it.
//
// reservedSubdomainPrefix is reserved as a PREFIX rather than as labels, because
// JomForm's own names under it live in more than one namespace (see the note above
// the list). One entry here covers every `jomform-*` name, present and future.
const reservedSubdomainPrefix = "jomform-"

// ServingReservedSubdomains is the ONE declaration of the host labels JomForm
// will not RESOLVE to a workspace (card t_d49dae9f). It is the serving-side
// counterpart to reservedSubdomains below: that list answers "what may a
// workspace CLAIM", this one answers "what may a Host header resolve to".
//
// IT IS DELIBERATELY SMALLER THAN reservedSubdomains. The claim list carries
// sensitive/impersonation names (`billing`, `login`, …) that must be unclaimable
// before JomForm ever routes them; the serving list carries only the names that
// JomForm's own deployment actually answers on today (or once did, so a
// bookmarked host still resolves). Adding a name here means "a request on this
// host must not be treated as a tenant storefront"; it does NOT mean the name is
// for sale to a merchant — that is reservedSubdomains' job.
//
// WHY IT EXISTS AT ALL. This set used to be a bare literal chain inside
// accountSubdomain (httpapi/public.go: `sub == "www" || sub == "app" || …`), and
// it was ALSO written down as `!Host(...)` exclusions in the Traefik wildcard
// routers. Three declarations of one fact, in three files that could not see each
// other, and no test tied them together — which is exactly the drift class this
// card exists to close. It is now declared once here and consumed by both sides:
//
//	accountSubdomain (httpapi/public.go)   — reads this slice, so Go has one copy
//	subdomains_test.go                     — asserts the Traefik wildcard
//	                                         `!Host(...)` exclusions are drawn
//	                                         from it, so a name excluded from the
//	                                         tenant wildcard without being on this
//	                                         list is RED (it would take a real
//	                                         merchant host off the air).
//
// The names are bare lower-case labels with no dot: the `<label>` of
// `<label>.jomform.com`.
var ServingReservedSubdomains = []string{"www", "app", "api", "mail", "s3"}

var reservedSubdomains = map[string]string{
	// ── Infrastructure ───────────────────────────────────────────────────────
	// The names JomForm's own deployment and mail live under. Four of them are
	// already refused for SERVING by accountSubdomain (httpapi/public.go), which
	// is why they answer 404 today — that is not a claim guard, so they belong
	// here too.
	"www":  "a static Traefik Host for the apex site (deploy/vps/traefik-jomform.yml, jf-web)",
	"api":  "the API host: refused for serving by accountSubdomain, and the host integration docs point merchants at",
	"app":  "the application host: refused for serving by accountSubdomain, and a legacy dashboard host browsers still have bookmarked",
	"mail": "the mail host: SPF/DKIM records and bounces must stay under JomForm's control, never a merchant storefront",
	"s3":   "the object-storage host: refused for serving by accountSubdomain, and an infrastructure name buyers read as trusted",

	// ── Products and edge that are actually routed ───────────────────────────
	// These have their own static Traefik routers today, so a claim would shadow
	// a live JomForm surface rather than merely squat a name. The test in
	// subdomains_test.go parses traefik-jomform.yml and fails when a static
	// router's host is neither the apex nor on this list, so a router added
	// later cannot be forgotten here.
	"docs":      "a routed JomForm surface: docs.jomform.com 301s to jomform.com/docs (static Traefik router jf-docs-root)",
	"mcp":       "a routed JomForm surface: mcp.jomform.com serves the MCP endpoint AND the OAuth discovery/issuer (oauthmeta.DefaultBase)",
	"templates": "JomForm's own workspace slug: the jomform-templates account serves the template gallery at templates.jomform.com/tpl-*",
	"custom":    "the custom-domain CNAME target JomForm publishes to merchants (CNAME -> custom.jomform.com)",

	// ── Sensitive / impersonation names ──────────────────────────────────────
	// No JomForm router uses these, so nothing breaks while they are free — the
	// risk is a merchant (or a stranger who talks one into it) owning an address
	// that reads as JomForm's own business, support or billing channel. The
	// merchant then collects data, money or credentials under JomForm's name, or
	// uses it to make a phishing link look first-party.
	"support":  "impersonation: buyers are told to contact support, so support.jomform.com reads as JomForm's own help desk",
	"help":     "impersonation: the same help-desk surface under a second obvious name",
	"status":   "impersonation: a status page is trusted for outage and incident messages",
	"billing":  "impersonation: a billing address invites card and invoice details, the highest-value phishing surface here",
	"payments": "impersonation: reads as JomForm's payment operations and invites card data",
	"pay":      "impersonation: the short form of the same payment surface",
	"checkout": "impersonation: a checkout host is where buyers already expect to enter card details",
	"admin":    "impersonation: an admin host invites 'sign in to manage your account' credential phishing",
	"account":  "impersonation: an account host invites credential phishing, and is easily confused with the dashboard",
	"accounts": "impersonation: the plural of the account host, indistinguishable to a buyer",
	"login":    "impersonation: a login host collects credentials in JomForm's name",
	"signup":   "impersonation: a signup host collects credentials and personal data in JomForm's name",
	"security": "impersonation: a security host is trusted for urgent 'verify your account' messages",
	"blog":     "impersonation: content published at blog.jomform.com reads as JomForm's own announcements",
	"cdn":      "impersonation: assets served from cdn.jomform.com lend a phishing page JomForm's first-party origin",
	"assets":   "impersonation: the same first-party-origin abuse under a second common name",
}

// foreignRoutedHosts is the ONE declaration of the hosts JomForm's edge routes
// that are NOT under jomform.com (card t_3f44c437). The key is a WHOLE host —
// a foreign route has no `<label>.jomform.com` to name — and the value is WHY
// it is allowed, so the exception is reviewed rather than waved through.
//
// WHY IT EXISTS. subdomains_test.go parses the committed Traefik table
// (deploy/vps/traefik-jomform.yml, deploy/traefik-wildcard.yml) and classifies
// every Host(`…`) literal: the apex, then a claimable `<label>.jomform.com` — and
// until this list existed, anything else was a bare `t.Errorf("… Extend the
// guard.")`. PR #1167 replaced that file with a byte-for-byte copy of what the
// host runs, and the host runs a real merchant CUSTOM domain
// (`halo.example.com`, router jf-custom-cert-halo, its own Let's Encrypt
// certificate). A guard that reds on the file the host actually loads is worse
// than no guard at all: it is the `test` gate, so it stops every lane on the
// board — measured 2026-10-12 on pristine main 27444a64.
//
// The exception is DECLARED, the same shape as reservedSubdomains: a name is not
// added here to make a test pass, an entry says "this exact host is a route
// JomForm means to serve". Deliberately NOT here:
//   - anything under jomform.com (apex included): those are classified by LABEL
//     through reservedSubdomains, and declaring one here would open a hole in
//     the claim guard — a test refuses an entry that is under jomform.com;
//   - a host this edge does not route: the list describes this file's table.
var foreignRoutedHosts = map[string]string{
	"halo.example.com": "a merchant custom domain, not a JomForm subdomain: router jf-custom-cert-halo serves it from the API and obtains its own Let's Encrypt certificate for it (deploy/vps/traefik-jomform.yml, the byte-for-byte copy of the file the host loads)",
}

// ReservedSubdomain reports whether label may NOT be claimed as a workspace
// subdomain. The comparison is exact after trimming and lower-casing, so the
// rule does not depend on a caller having normalised the slug first (the length
// and charset rules still run in the callers, unchanged).
func ReservedSubdomain(label string) bool {
	norm := strings.ToLower(strings.TrimSpace(label))
	if _, reserved := reservedSubdomains[norm]; reserved {
		return true
	}
	// The `jomform-` PREFIX, not just the labels written down (card t_40338d5f).
	// JomForm's own surfaces are named under this prefix in more than one
	// namespace, and the gallery host `jomform-templates.jomform.com` was
	// claimable as a workspace slug — which serves the merchant's storefront on a
	// live JomForm surface. Reserved as a prefix so the next `jomform-*` name does
	// not need to be remembered, which is how this one was missed.
	return strings.HasPrefix(norm, reservedSubdomainPrefix)
}

// IsServingReserved reports whether label is one of the host labels JomForm's
// own deployment answers on, i.e. a Host header that must NOT be resolved to a
// workspace storefront (see ServingReservedSubdomains in subdomains.go). The
// comparison normalises the way ReservedSubdomain does — trimmed and
// lower-cased — so a caller does not have to remember to.
//
// This is the ONE predicate both consumers use: accountSubdomain (httpapi)
// calls it for the serving decision, and the reserved-name guard
// (subdomains_test.go) calls it when it asserts the Traefik wildcard
// `!Host(...)` exclusions are drawn from the same set. Keeping the predicate
// here rather than in each consumer is the point of card t_d49dae9f: a second
// hand-rolled membership test is the drift this card closes.
func IsServingReserved(label string) bool {
	norm := strings.ToLower(strings.TrimSpace(label))
	for _, l := range ServingReservedSubdomains {
		if l == norm {
			return true
		}
	}
	return false
}

// isForeignRoutedHost reports whether host is one JomForm's edge is DECLARED to
// route although it is not under jomform.com (see foreignRoutedHosts). The
// comparison is an exact match after trimming and lower-casing — deliberately not
// a suffix rule: a suffix rule would let `halo.example.com.evil.tld` read
// as the declared host, which is the shape of an impersonation.
//
// It is package-private because its only consumer is the Traefik guard in
// subdomains_test.go. The serving decision never meets a foreign host:
// accountSubdomain returns "" unless the host ends in .jomform.com, and a custom
// domain resolves through accounts.custom_domain instead.
func isForeignRoutedHost(host string) bool {
	_, ok := foreignRoutedHosts[strings.ToLower(strings.TrimSpace(host))]
	return ok
}

// ReservedSubdomainMessage is the refusal for a reserved label. It says WHY the
// name is refused — that it is reserved for JomForm — because a merchant who is
// told only "slug tidak sah" reads it as their own input being malformed and
// tries variations of the same sensitive name.
//
// Wording is English on purpose: this string is the wire message for API-key,
// curl and MCP clients, which cannot look up an i18n key, and #694's rule is
// that the DASHBOARD renders its own localised sentence (keyed off
// ReservedSubdomainCode) rather than the API's prose. Writing a fresh Malay
// literal here is the thing that rule forbids, so this refusal introduces none.
func ReservedSubdomainMessage(label string) string {
	return fmt.Sprintf(
		"%q is reserved for JomForm: it is an address JomForm owns or must protect (infrastructure, product or support/billing-looking names), so a workspace cannot claim it. Choose a different address.",
		strings.TrimSpace(label))
}
