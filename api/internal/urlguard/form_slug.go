// Reserved FORM-SLUG names — the path-segment half of the same claim policy.
//
// WHY THIS EXISTS (t_f76f81eb). A form's slug is the path segment buyers are
// handed: https://<workspace>.jomform.com/<slug>, or the same path on a
// merchant's verified custom domain. Nothing checked it at storage. Measured on
// prod v1.25.0 before this rule:
//
//	POST /v1/rest/forms {"slug":"bad slug!!/x"} -> 201, url ".../bad slug!!/x"
//	PATCH /v1/rest/forms/{id} {"slug":"login"}  -> 200 (a reserved label)
//
// The first is a public address with raw spaces and an extra path segment that
// no router can serve; the second puts the merchant's form on a path JomForm
// itself answers. Both are refused now, by ONE rule with one implementation.
//
// TWO CLASSES OF NAME ARE REFUSED, and they are deliberately composed rather
// than restated:
//
//  1. the labels JomForm already refuses as a WORKSPACE claim — ReservedSubdomain
//     above. They are names JomForm owns or must protect, and a form on
//     `<merchant-domain>/billing` reads to a buyer exactly like the host
//     billing.jomform.com the workspace rule refuses. The list is REUSED, not
//     copied: a second hand-written list of the same labels is the drift this
//     package exists to prevent (see the header of subdomains.go).
//
//  2. the literal single-segment GET routes JomForm's own deployment answers on
//     the SAME host a form is served on. A custom domain and a workspace
//     subdomain both resolve to the API (deploy/vps/traefik-jomform.yml:
//     jf-custom-root / jf-wildcard -> jf-api), so the API's mux is the whole
//     surface a buyer's path can collide with — and a LITERAL pattern wins over
//     the `GET /{slug}` wildcard, so such a form would never be reachable at
//     the address the merchant was shown. `GET /privacy` is the measured
//     example: the hosted legal page answers there, and a form with that slug
//     was accepted and then never served.
//
// The path list is small and is held honest by a guard rather than by memory:
// api/internal/httpapi/form_slug_reserved_routes_test.go parses every mux
// registration in the package and fails when a slug-shaped, single-segment GET
// literal is not covered here. That guard is the reason a route added later
// cannot silently reopen this hole.
package urlguard

import (
	"fmt"
	"strings"
)

// FormSlugReservedCode is the stable failure token a form-slug write carries
// when the value is well formed but names something JomForm owns or serves, so
// the dashboard can render its own sentence instead of the API's prose (the
// shape ReservedSubdomainCode established).
//
// It is urlguard's token rather than a Code* constant in httpapi/errcode.go for
// the same reason ReservedSubdomainCode is: the wire message NAMES the refused
// label, so it can never be a key in that table (the table is keyed on exact
// message literals). web/src/lib/apiErr.ts maps it and apiErr.test.ts
// allow-lists it by name.
const FormSlugReservedCode = "form.slugReserved"

// reservedFormSlugPaths is the set of single-segment GET routes JomForm's own
// deployment answers on a workspace subdomain / custom domain, i.e. the literal
// patterns in the API mux that win over `GET /{slug}`.
//
// The value of each entry is WHY it is on the list, so an entry can be reviewed
// and removed rather than guessed at (the same convention reservedSubdomains
// follows). A label that is ALREADY refused as a workspace claim (mcp, api, …)
// is deliberately not repeated here — ReservedFormSlug consults that list too.
var reservedFormSlugPaths = map[string]string{
	"privacy": "the hosted privacy policy is served at GET /privacy on the same workspace subdomain / custom domain (handlePublicLegalPage), and the literal route wins over /{slug}",
	"terms":   "the hosted terms page is served at GET /terms on the same host (handlePublicLegalPage), and the literal route wins over /{slug}",
	"refund":  "the hosted refund policy is served at GET /refund on the same host (handlePublicLegalPage), and the literal route wins over /{slug}",
	"healthz": "the liveness probe answers GET /healthz on every host the API listens on (server.go), so a form there is never reached",
	"readyz":  "the readiness probe answers GET /readyz on every host the API listens on (server.go), so a form there is never reached",
	"version": "the build-identity endpoint answers GET /version on every host the API listens on (server.go), so a form there is never reached",
	"metrics": "the Prometheus scrape endpoint answers GET /metrics on every host the API listens on (server.go), so a form there is never reached",
}

// ReservedFormSlug reports whether label may NOT be used as a form's public
// slug. The comparison normalises the way ReservedSubdomain does — trimmed and
// lower-cased — so the rule does not depend on a caller having normalised first
// (the length and charset legs still run in dbstore.ValidateFormSlug, unchanged).
func ReservedFormSlug(label string) bool {
	norm := strings.ToLower(strings.TrimSpace(label))
	// Class 1: the workspace-claim list, reused verbatim (this also carries the
	// `jomform-` prefix rule, so a form cannot be named after a JomForm surface
	// that does not exist yet).
	if ReservedSubdomain(norm) {
		return true
	}
	// Class 2: the literal routes JomForm serves on the host the form lives on.
	_, reserved := reservedFormSlugPaths[norm]
	return reserved
}

// ReservedFormSlugMessage is the refusal a reserved form slug answers with. It
// says WHY — that JomForm uses the name for its own addresses — because a
// merchant told only "invalid slug" reads it as their own input being malformed
// and tries variations of the name that stay reserved.
//
// Wording is English on purpose: this string is the wire message for API-key,
// curl and MCP clients, which cannot look up an i18n key. The dashboard renders
// its own localised sentence keyed off FormSlugReservedCode.
func ReservedFormSlugMessage(label string) string {
	return fmt.Sprintf(
		"%q is reserved: JomForm uses that name for its own addresses, so a form cannot use it as its slug. Choose a different slug.",
		strings.TrimSpace(label))
}
