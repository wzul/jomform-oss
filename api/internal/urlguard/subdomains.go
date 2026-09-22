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
// This is NOT the serving-side exclusion in httpapi/public.go (accountSubdomain):
// that list decides which hosts JomForm will not RESOLVE to an account, it is
// only consulted while serving a request, and it does not carry the sensitive
// names at all. It could therefore never stop a claim — a workspace could still
// own `api` while every request to it fell through. Both rules are needed and
// they answer different questions:
//
//	accountSubdomain   — what may this Host header resolve to?  (serving)
//	reservedSubdomains — what may a workspace CLAIM?            (claim, here)
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
var reservedSubdomains = map[string]string{
	// ── Infrastructure ───────────────────────────────────────────────────────
	// The names JomForm's own deployment and mail live under. Four of them are
	// already refused for SERVING by accountSubdomain (httpapi/public.go), which
	// is why they answer 404 today — that is not a claim guard, so they belong
	// here too.
	"www": "a static Traefik Host for the apex site (deploy/vps/traefik-jomform.yml, jf-web)",
	"api": "the API host: refused for serving by accountSubdomain, and the host integration docs point merchants at",
	"app": "the application host: refused for serving by accountSubdomain, and a legacy dashboard host browsers still have bookmarked",
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

// ReservedSubdomain reports whether label may NOT be claimed as a workspace
// subdomain. The comparison is exact after trimming and lower-casing, so the
// rule does not depend on a caller having normalised the slug first (the length
// and charset rules still run in the callers, unchanged).
func ReservedSubdomain(label string) bool {
	_, reserved := reservedSubdomains[strings.ToLower(strings.TrimSpace(label))]
	return reserved
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
