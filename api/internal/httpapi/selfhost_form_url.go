package httpapi

// Where a SELF-HOSTED install serves a published form (card t_6c0badbe, folded
// from t_f0b60bcc).
//
// Public form addressing is host-scoped on the hosted product: a tenant is
// served on `<workspace>.jomform.com/<slug>` (or a verified custom domain), and
// the apex serves nothing (issue #77). A self-hosted install has no tenant
// subdomains at all — ONE hostname, `<APP_BASE_URL>` — and the handler serves its
// own forms at `{APP_BASE_URL}/f/{slug}` (PublicHandlers.ownOriginForm, gated on
// JOMFORM_SELF_HOSTED; card t_28a06494).
//
// Every URL BUILDER nevertheless printed the hosted shape, so on a clean
// self-host install the builder's "Your form is live" dialog offered
// `https://<workspace>.jomform.com/<slug>`: a host that does not exist, a 404
// behind "Copy link", "Open form" and the QR stand. The URL and the serving path
// must agree, and only the deployment flag knows which shape is serving.
//
// ONE shape, ONE place. The two builders that emit a form URL
// (RESTHandlers.formURLForAccount for the dashboard/API payload,
// PublicHandlers.formURLForAccount for notification mail) both call this, so a
// self-hosted install cannot print one address from the dashboard and another
// from an email.

import "strings"

// installFormURL returns `{origin}/f/{slug}` for a self-hosted install, or ""
// when the origin is unusable — the caller then falls through to the hosted
// resolution rather than emitting a relative or malformed link.
//
// The path is `/f/` on purpose: it is the route the install actually serves
// (registered in PublicHandlers.RegisterRoutes), so the printed URL is reachable
// on the deployment that printed it. A trailing slash on the configured origin
// is trimmed so `APP_BASE_URL=https://forms.example.com/` cannot produce `//f/`.
func installFormURL(origin, slug string) string {
	base := strings.TrimRight(strings.TrimSpace(origin), "/")
	if base == "" || slug == "" {
		return ""
	}
	return base + "/f/" + slug
}
