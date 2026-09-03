// Form confirmation resolution (issue #179, Gravity-Forms-style).
//
// After a successful submit (payment or response), the first matching
// confirmation is picked by evaluating each confirmation's conditional logic
// against the submitted field values. The default confirmation has no
// condition and always matches, so it is the fallback. A message confirmation
// is rendered on the thank-you page (HTML-sanitized); a redirect confirmation
// sends the buyer to the configured URL.
package httpapi

import (
	"context"
	"strconv"
	"strings"

	"github.com/microcosm-cc/bluemonday"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// resolvedConfirmation is the outcome of confirmation resolution.
type resolvedConfirmation struct {
	Confirmation *dbstore.Confirmation
	// Message is the sanitized HTML to render when Type == message.
	Message string
	// RedirectURL is the URL to redirect to when Type == redirect.
	RedirectURL string
}

// sanitizeConfirmationHTML strips any script/event-handler/unsafe markup from
// a merchant-authored confirmation message so it can never execute on the
// buyer's page (no XSS). Safe formatting tags (p, br, strong, em, a, ul, li,
// h1-h4, blockquote, code, pre) are preserved.
var confirmationSanitizer = bluemonday.UGCPolicy()

// resolveConfirmation picks the first confirmation whose condition matches the
// submitted field values. The default confirmation always matches. Returns
// the resolved confirmation plus its rendered message (sanitized) or redirect
// URL. When no confirmation can be resolved (should not happen because the
// default always matches), a safe built-in message is returned.
func (h *PublicHandlers) resolveConfirmation(ctx context.Context, form *dbstore.Form, fields map[string]any) *resolvedConfirmation {
	confs, err := h.Forms.Confirmations(ctx, form.ID)
	if err != nil || len(confs) == 0 {
		def := dbstore.DefaultConfirmation(form.ID)
		return &resolvedConfirmation{
			Confirmation: def,
			Message:      confirmationSanitizer.Sanitize(def.Message),
		}
	}
	answers, nums := confirmationAnswers(fields)
	for _, c := range confs {
		if c.Condition == nil || conditionMet(&Condition{
			FieldID: c.Condition.FieldID,
			Op:      c.Condition.Op,
			Value:   c.Condition.Value,
		}, answers, nums) {
			return &resolvedConfirmation{
				Confirmation: c,
				Message:      confirmationSanitizer.Sanitize(c.Message),
				RedirectURL:  c.RedirectURL,
			}
		}
	}
	// Fallback: the default always matches, but guard anyway.
	def := dbstore.DefaultConfirmation(form.ID)
	return &resolvedConfirmation{
		Confirmation: def,
		Message:      confirmationSanitizer.Sanitize(def.Message),
	}
}

// confirmationAnswers flattens submitted field values into the string + numeric
// maps the condition evaluator expects. Values may be strings, numbers, or
// booleans (from the JSON submit payload).
func confirmationAnswers(fields map[string]any) (map[string]string, map[string]int64) {
	answers := map[string]string{}
	nums := map[string]int64{}
	for k, v := range fields {
		switch t := v.(type) {
		case string:
			answers[k] = t
			if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
				nums[k] = n
			}
		case float64:
			answers[k] = strconv.FormatFloat(t, 'f', -1, 64)
			nums[k] = int64(t)
		case bool:
			if t {
				answers[k] = "1"
			}
		case nil:
			answers[k] = ""
		}
	}
	return answers, nums
}
