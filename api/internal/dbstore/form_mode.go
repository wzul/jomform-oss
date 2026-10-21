// The form's collection mode — the ONE rule that decides
// whether a form takes payment ("payment", the default) or only collects
// responses ("response", e.g. an RSVP).
//
// The mode lives in the form's JSON config (`forms.config.mode`) and is the
// same field the builder writes ("Form mode": Payment form / Response form),
// the public submit path branches on (a response form records a `submissions`
// row instead of an order), and the entry-limit counter keys off. It is
// therefore the ONLY thing that can tell a sales form from a non-sales form.
//
// This lives here rather than in httpapi because more than one layer needs the
// same answer: the REST form payload surfaces it (so the dashboard can label a
// row correctly) and the MCP form tools expose it too. Two copies of this rule
// would be two chances to disagree.
package dbstore

import "github.com/wzul/jomform/api/internal/customform"

// Form mode values stored in forms.config.mode.
const (
	FormModePayment  = "payment"  // takes payment (default, historical behaviour)
	FormModeResponse = "response" // collects responses only — no products, no payment
)

// FormMode returns the form's collection mode: FormModePayment (default) or
// FormModeResponse.
//
// Anything that is not explicitly "response" is "payment". That default is
// deliberate: forms created before #171 have no `mode` key at all, and they
// were payment forms — the overwhelming majority of them have sales. Treating
// a missing key as "response" would mislabel every legacy form.
//
// Card t_827b0a78: the ONE exception is a config carrying an ENABLED
// custom_html section. A custom-HTML form is response-only by construction
// (issue #255, customform's package doc: it collects answers, it never takes
// payment), so a raw-config write that stored the section without setting
// `mode` left the stored form describing itself as a payment form — and BOTH
// decisions keyed on that word: serveForm painted the payment/legacy layout
// instead of the merchant's body, and submitResolvedForm sent the POST to the
// order core, whose payload the custom page can never satisfy (it posts
// {client, fields} and no items, so the buyer got 400 "butiran tidak lengkap").
// The section itself is the authority here, asked through customform.Enabled
// (empty html means "no custom form", so clearing a body still falls back to
// the block renderer). Ask this function, never config["mode"] directly, and
// the page, the submit dispatch, the entry limit and the dashboard label cannot
// drift apart.
//
// The write paths ALSO store `mode:"response"` beside such a section
// (customform.NormalizeConfig), so a form written after that fix is
// self-describing; this arm is what keeps a row written before it — or copied
// from an older template — behaving as the custom form it is.
func FormMode(f *Form) string {
	if f == nil || f.Config == nil {
		return FormModePayment
	}
	if m, ok := f.Config["mode"].(string); ok && m == FormModeResponse {
		return FormModeResponse
	}
	if customform.Enabled(f.Config) {
		return FormModeResponse
	}
	return FormModePayment
}

// IsResponseForm reports whether the form collects responses only (no payment).
func IsResponseForm(f *Form) bool { return FormMode(f) == FormModeResponse }
