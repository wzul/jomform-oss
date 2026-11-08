// Package installurl answers ONE question: what address does this deployment
// publish for a merchant's workspace?
//
// Public workspace addressing is host-scoped on the hosted product: a tenant is
// reached at `<slug>.jomform.com`, printed on QR stands, pasted into iframe
// snippets and handed to affiliates. A SELF-HOSTED install has no such domain
// and no tenant subdomains at all — ONE origin, `<APP_BASE_URL>`, whose apex
// serves the workspace and whose `/f/<slug>` route serves its forms (see
// PublicHandlers.ownOriginForm, gated on JOMFORM_SELF_HOSTED).
//
// The URL builders ignored that distinction and printed the hosted shape on
// every deployment, so on a clean self-host install `POST /v1/workspaces`
// answered `"url": "https://probe-ws-…​.jomform.com"` — a host that does not
// exist there (card t_48a93084). The merchant was handed a dead address at the
// first screen of the product.
//
// WHICH SHAPE IS DECIDED BY THE FLAG, NEVER BY THE HOST. The hosted product's
// APP_BASE_URL is its own apex, so "the configured origin is a jomform.com host"
// is TRUE on jomform.com — inferring from it is exactly what broke issue #77 /
// card t_4ddab217, where the product apex began serving form slugs. SelfHosted
// comes from JOMFORM_SELF_HOSTED, which the self-host bootstrap writes and the
// hosted deploy never sets.
//
// ONE implementation, called by both URL-emitting surfaces (REST workspace
// payloads and the MCP workspace tools), because a second copy is how the REST
// and MCP answers drift apart.
package installurl

import (
	"net/url"
	"strings"
)

// CNAMEHost is where a merchant points a custom domain: the hosted product's
// CNAME target (`custom.jomform.com`), or — on a self-hosted install — the
// install's own host (`forms.example.com:8443`), because that is the only host
// that routes to this deployment.
//
// The verifier is host-agnostic (it probes whether the domain SERVES JomForm —
// see RESTHandlers.verifyCustomDomainByID), so any host that reaches this
// install works; the copy must name that host rather than the product's.
//
// "" when a self-hosted install has no usable origin — callers must then say
// "this install" instead of printing an empty arrow.
func CNAMEHost(origin string, selfHosted bool) string {
	if !selfHosted {
		return "custom.jomform.com"
	}
	base := Origin(origin)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	// Host only: a CNAME value is a hostname (plus port), never a path. An
	// install served under a subpath still points the domain at the host.
	return u.Host
}

// Workspace is the address a workspace is reached at:
//
//   - hosted:   `https://<slug>.jomform.com`
//   - self-host: `<APP_BASE_URL>/` — the install's own apex, trailing slash
//     included, because that is the address the operator types and the one the
//     workspace root answers on. Forms live under it at `/f/<form-slug>`.
//
// "" is returned when the answer cannot be built honestly: an empty slug on the
// hosted product, or a self-hosted install with no usable APP_BASE_URL. A
// caller that gets "" must omit the field rather than print a relative or
// guessed address — the self-hosted branch is TERMINAL, because falling back to
// the hosted shape is the very bug this package exists to stop.
func Workspace(origin string, selfHosted bool, slug string) string {
	if !selfHosted {
		if slug == "" {
			return ""
		}
		return "https://" + slug + ".jomform.com"
	}
	base := Origin(origin)
	if base == "" {
		return ""
	}
	return base + "/"
}

// Origin returns the install's own origin with any trailing slash trimmed, or ""
// when APP_BASE_URL is empty, whitespace, or not an absolute http(s) URL with a
// host. `APP_BASE_URL=https://forms.example.com/` must not produce `//`.
//
// A scheme is required on purpose: `localhost:18099` parses as a URL whose scheme
// is "localhost", and printing `localhost:18099/` in a dashboard is a link that
// resolves relative to the current host — the dead-link class the footer rules
// already refuse (card t_457c4d87). Such a deployment gets "" instead.
func Origin(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return ""
	}
	rest := base[strings.Index(base, "//")+2:]
	if rest == "" || strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "#") {
		return ""
	}
	return base
}
