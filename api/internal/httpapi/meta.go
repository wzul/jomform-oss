// GET /v1/meta — the deployment facts a client needs to print a TRUE address.
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/installurl"
)

// WHY THIS ROUTE EXISTS (card t_48a93084). The dashboard prints a workspace's
// address in the picker, the workspace switcher, the rename form and the
// settings copy — and it used to hardcode the hosted shape
// ("Your forms will live at <slug>.jomform.com"). On a self-hosted install
// there is no such host: one origin serves every workspace, and forms live at
// `<APP_BASE_URL>/f/<slug>`. The dashboard cannot see the API's runtime config
// (JOMFORM_SELF_HOSTED is a server env var; NEXT_PUBLIC_* is baked at build
// time and cannot read it), so the deployment has to SAY which shape it is.
//
// Anonymous on purpose: the values are not secrets (the operator's own origin
// is already printed on every public page it serves), and the picker needs them
// before the workspace list can be trusted — but nothing here is per-account,
// so there is no data to leak.
//
// `origin` is the install's own origin as configured (trailing slash trimmed),
// and it is the SAME fact on both deployments — the hosted product's
// APP_BASE_URL is its own apex. A client MUST branch on `self_hosted`, never on
// whether the origin looks like a jomform.com host: the apex satisfies that
// test on the hosted product, which is exactly how issue #77 was broken. See
// lib/deployment.ts on the web side for the rule and its test.
func (h *RESTHandlers) deploymentMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"self_hosted": h.SelfHosted,
		"origin":      installurl.Origin(h.BaseURL),
	})
}
