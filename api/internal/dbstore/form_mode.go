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
func FormMode(f *Form) string {
	if f == nil || f.Config == nil {
		return FormModePayment
	}
	if m, ok := f.Config["mode"].(string); ok && m == FormModeResponse {
		return FormModeResponse
	}
	return FormModePayment
}

// IsResponseForm reports whether the form collects responses only (no payment).
func IsResponseForm(f *Form) bool { return FormMode(f) == FormModeResponse }
