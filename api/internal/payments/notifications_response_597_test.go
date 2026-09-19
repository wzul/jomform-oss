package payments

// Issue #597: the response-form default notification must render, and every
// placeholder it names must have a real value on a submission.
//
// Why this is a payments-package test: `renderNotification` is unexported here,
// and the rendering rule it implements is the one that matters. text/template
// REFUSES to parse a template naming an undefined function, and
// renderNotificationTemplate falls back to the RAW SOURCE on a parse error — so
// a placeholder the submission did not carry is mailed as its own literal text
// (`{{email}}`), not as an empty string. Measured on this engine: a template
// `Pendaftaran {{full_name}}` with a ref carrying no such field renders back as
// `Pendaftaran {{full_name}}`. The response default names the responder's
// identity, and a form need not collect a name or an email at all, so the two
// keys are registered as always-defined and an absent value renders empty.
//
// Run with: cd api && go test ./internal/payments/
import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestResponseDefaultNotificationRendersNoLiteralPlaceholders renders the
// default body for a real submission and for a sparse one.
func TestResponseDefaultNotificationRendersNoLiteralPlaceholders(t *testing.T) {
	_, _, subject, body := dbstore.DefaultNotification(dbstore.FormModeResponse)

	cases := []struct {
		name string
		ref  NotificationRef
		want []string
	}{
		{
			name: "a full submission",
			ref: NotificationRef{
				FormURL:   "https://shop.jomform.com/event-registration",
				BuyerName: "Siti Aminah",
				Fields: map[string]string{
					"b1": "Siti Aminah", "b2": "siti@example.test",
					"full_name": "Siti Aminah", "email": "siti@example.test",
				},
			},
			want: []string{"Siti Aminah", "siti@example.test", "https://shop.jomform.com/event-registration"},
		},
		{
			name: "a submission with no name or email block at all",
			ref: NotificationRef{
				FormURL: "https://shop.jomform.com/rsvp",
				Fields:  map[string]string{"b9": "yes"},
			},
			want: []string{"https://shop.jomform.com/rsvp"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSubj, gotBody := renderNotification(subject, body, tc.ref)
			for _, want := range tc.want {
				if !strings.Contains(gotBody, want) {
					t.Errorf("rendered body is missing %q:\n%s", want, gotBody)
				}
			}
			for _, s := range []string{gotSubj, gotBody} {
				if strings.Contains(s, "{{") || strings.Contains(s, "}}") {
					t.Errorf("an unrendered placeholder survived into the message — the engine "+
						"falls back to the raw template, so this would be mailed literally:\n"+
						"subject=%q\nbody=%q", gotSubj, gotBody)
				}
			}
			// A response has no reference code, amount or items.
			for _, bad := range []string{"RM ", "Jualan", "Rujukan", "Pembeli"} {
				if strings.Contains(gotSubj, bad) || strings.Contains(gotBody, bad) {
					t.Errorf("the response notification carries the sale wording %q:\n"+
						"subject=%q\nbody=%q", bad, gotSubj, gotBody)
				}
			}
		})
	}
}

// TestAbsentIdentityPlaceholderRendersEmptyNotLiteral pins the mechanism itself,
// so a future change to the func map cannot quietly reintroduce the literal-text
// failure: undefined placeholders render as empty, defined ones as their value.
func TestAbsentIdentityPlaceholderRendersEmptyNotLiteral(t *testing.T) {
	ref := NotificationRef{Fields: map[string]string{"full_name": "Ali"}}
	// full_name is present as a field; email is absent entirely.
	_, body := renderNotification("x", "Nama: {{full_name}}\nEmel: {{email}}", ref)
	if body != "Nama: Ali\nEmel: " {
		t.Errorf("body = %q, want the absent {{email}} rendered EMPTY, not as literal text", body)
	}

	// And the sale placeholders stay empty (not literal) on a response-shaped
	// ref for the same reason.
	_, body = renderNotification("x", "{{ref_code}}|{{amount}}|{{items}}", NotificationRef{})
	if body != "|0.00|" {
		t.Errorf("body = %q, want the sale placeholders rendered from their zero values", body)
	}
}
