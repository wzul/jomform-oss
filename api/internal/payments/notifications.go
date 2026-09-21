// Per-form email notification engine (issue #193, Gravity-Forms-style).
//
// On each form event (sale.paid, sale.failed, response.submitted,
// subscription.charged) the engine evaluates the form's notification rules:
// for every enabled rule whose event matches AND whose optional conditional
// logic is satisfied by the submitted field values, it renders the subject/
// body (with {{placeholders}}) and enqueues an email via the #199 background
// queue. A form with zero notifications sends nothing.
//
// The recipient (`to_email`) is one of:
//   - a literal address (admin@example.com)
//   - `{admin_email}` — the merchant's account email, resolved at send time
//   - `{field_id}` — the submitted value of a form field (e.g. a buyer email)
//
// Failures are logged but NEVER fail the payment/submit flow.
package payments

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"
	"text/template"
	texttemplate "text/template"
	"time"
	"unicode"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// NotificationRef carries the placeholder context + recipient resolution
// inputs for one notification event.
type NotificationRef struct {
	// FormID is the form whose rules are evaluated.
	FormID string
	// WorkspaceID / AccountID resolve the merchant's admin_email.
	WorkspaceID string
	AccountID   string
	// RefCode / AmountSen / ItemsSummary / BuyerName / FormURL are the
	// standard {{placeholders}} (mirror the receipt context).
	RefCode      string
	AmountSen    int64
	ItemsSummary string
	BuyerName    string
	FormURL      string
	// Fields maps block_id → submitted value. Used for conditional logic and
	// for `{field_id}` recipients and `{{field_id}}` placeholders.
	Fields map[string]string
	// Issue #438: QR ordering flow — the table number the QR stand encoded
	// (e.g. "8"). Exposed as the {{table_number}} placeholder so the merchant
	// can show it in the sale.paid notification.
	TableNumber string
}

// FireFormNotifications evaluates a form's notification rules for one event
// and enqueues the matching emails. Best-effort: failures are logged, never
// propagated to the caller (a slow/broken notification must not block a
// payment or submission).
func (s *Service) FireFormNotifications(ctx context.Context, ref NotificationRef, event string) {
	if s.Notifications == nil || ref.FormID == "" {
		return
	}
	rules, err := s.Notifications.ListByFormEvent(ctx, ref.FormID, event)
	if err != nil {
		slog.Warn("notify_list_failed", "form", ref.FormID, "event", event, "err", err)
		return
	}
	for _, n := range rules {
		if !notificationConditionMet(n.Condition, ref.Fields) {
			continue
		}
		s.sendNotification(ctx, ref, n)
	}
}

// sendNotification renders + enqueues one matching notification rule.
func (s *Service) sendNotification(ctx context.Context, ref NotificationRef, n *dbstore.FormNotification) {
	to := s.resolveNotificationRecipient(ctx, ref, n.ToEmail)
	if to == "" {
		slog.Warn("notify_no_recipient", "form", ref.FormID, "event", n.Event, "id", n.ID)
		return
	}
	subj, body := renderNotification(n.Subject, n.Body, ref)
	if s.EmailQueue != nil {
		if err := s.EmailQueue.Enqueue(ctx, emailqueue.Job{
			Type: "form_notification", To: to, Subject: subj, Body: body,
			AccountID: ref.AccountID, SaleRef: ref.RefCode,
		}); err != nil {
			slog.Warn("notify_enqueue_failed", "form", ref.FormID, "id", n.ID, "err", err)
		}
		return
	}
	// Synchronous path (bare-mode tests / no queue wired).
	if s.Receipts != nil && s.Receipts.Sender != nil {
		if err := s.Receipts.Sender.Send(to, subj, body); err != nil {
			// A suppression hit is a correct skip, not a failure: the pre-send
			// guard refused the send before SES, and retrying is pointless.
			if errors.Is(err, email.ErrSuppressed) {
				slog.Warn("notify_suppressed_skipped", "form", ref.FormID, "id", n.ID, "to", to)
				return
			}
			slog.Warn("notify_send_failed", "form", ref.FormID, "id", n.ID, "err", err)
		}
	}
}

// resolveNotificationRecipient resolves a rule's to_email: a literal address,
// `{admin_email}` (merchant's account email), or `{field_id}` (submitted value).
func (s *Service) resolveNotificationRecipient(ctx context.Context, ref NotificationRef, toEmail string) string {
	to := strings.TrimSpace(toEmail)
	if to == "" {
		return ""
	}
	if to == "{admin_email}" {
		return s.resolveAdminEmail(ctx, ref)
	}
	if strings.HasPrefix(to, "{") && strings.HasSuffix(to, "}") {
		fieldID := strings.TrimSuffix(strings.TrimPrefix(to, "{"), "}")
		if v, ok := ref.Fields[fieldID]; ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	return to
}

// resolveAdminEmail returns the merchant's account email for a form's owner.
//
// The recipient must be resolved through the OWNER USER id, never the account id
// (issue #606): `users.id` and `accounts.id` are independent UUIDs — the FK is
// `accounts.owner_user_id` (migrations/000001_init.up.sql:26-27) — so looking a
// user up by account id ALWAYS missed and the recipient resolved to "". Since
// `{admin_email}` is the recipient of the DEFAULT rule EnsureDefault creates for
// every new form, that silently killed the merchant's new-sale email.
//
// Mirrors the receipt path (receipt.go resolveMerchantEmail / notifyMerchant):
// account -> owner_user_id (or a workspace's owner when the ref carries no
// account) -> user email. A missing/unreadable store degrades to "" — no
// recipient, never a panic and never an invented address.
func (s *Service) resolveAdminEmail(ctx context.Context, ref NotificationRef) string {
	if s.Users == nil {
		return ""
	}
	// The owner USER id to look the email up by. Never an account id.
	ownerUserID := ""
	switch {
	case ref.AccountID != "":
		if s.Accounts == nil {
			// No way to map the account to its owner — degrade, don't guess.
			return ""
		}
		acc, err := s.Accounts.ByID(ctx, ref.AccountID)
		if err != nil || acc == nil {
			// An unreadable account (deleted, foreign, or a non-UUID id) fails
			// closed. `AccountStore.ByID` returns a non-nil zero Account with
			// its error, so the trailing `ownerUserID == ""` check would also
			// catch this — the early return is belt-and-braces and the nil half
			// guards a future ByID that nils its result.
			return ""
		}
		ownerUserID = acc.OwnerUserID
	case ref.WorkspaceID != "" && s.Workspaces != nil:
		// No account on the ref: fall back to the workspace owner (the
		// pre-#606 behaviour, kept for refs built without an account id).
		if sp, err := s.Workspaces.ByID(ctx, ref.WorkspaceID); err == nil && sp != nil {
			ownerUserID = sp.Owner
		}
	}
	if ownerUserID == "" {
		return ""
	}
	if u, err := s.Users.ByID(ctx, ownerUserID); err == nil && u != nil {
		return u.Email
	}
	return ""
}

// fireSaleNotifications evaluates a sale's form notification rules for one
// event (sale.paid | sale.failed | subscription.charged) and enqueues the
// matching emails. Best-effort — never blocks the payment flow.
func (s *Service) fireSaleNotifications(ctx context.Context, sale *dbstore.Sale, event string) {
	if s.Notifications == nil || sale.FormID == "" {
		return
	}
	workspaceID := ""
	if s.Workspaces != nil {
		if sid, serr := s.Workspaces.WorkspaceIDByAccount(ctx, sale.AccountID); serr == nil {
			workspaceID = sid
		}
	}
	formURL := s.formPublicURL(ctx, sale.AccountID, sale.FormID)
	if formURL == "" {
		formURL = s.BaseURL + "/t/" + sale.RefCode
	}
	s.FireFormNotifications(ctx, NotificationRef{
		FormID:       sale.FormID,
		WorkspaceID:  workspaceID,
		AccountID:    sale.AccountID,
		RefCode:      sale.RefCode,
		AmountSen:    sale.TotalSen,
		ItemsSummary: itemsSummary(sale.Items),
		BuyerName:    sale.Client.FullName,
		FormURL:      formURL,
		Fields:       saleFields(sale),
		// Issue #438: QR ordering flow — the table number the QR stand encoded.
		TableNumber: sale.TableNumber,
	}, event)
}

// saleFields flattens a sale's buyer client + items into a block_id → value
// map so notification conditional logic and {field_id} recipients can resolve
// against the submitted data.
func saleFields(sale *dbstore.Sale) map[string]string {
	f := map[string]string{}
	if sale.Client.FullName != "" {
		f["full_name"] = sale.Client.FullName
	}
	if sale.Client.Email != "" {
		f["email"] = sale.Client.Email
	}
	if sale.Client.Phone != "" {
		f["phone"] = sale.Client.Phone
	}
	return f
}

// notificationConditionMet evaluates a rule's conditional logic against the
// submitted field values. A nil condition always matches. Ops mirror the
// builder's condition.go: equals | not_equals | contains | gt | lt | not_empty.
func notificationConditionMet(cond *dbstore.NotificationCondition, fields map[string]string) bool {
	if cond == nil {
		return true
	}
	v := strings.TrimSpace(fields[cond.FieldID])
	switch cond.Op {
	case "equals":
		if cond.Value == "" {
			return v != ""
		}
		return strings.EqualFold(v, strings.TrimSpace(cond.Value))
	case "not_equals":
		return !notificationConditionMet(&dbstore.NotificationCondition{
			FieldID: cond.FieldID, Op: "equals", Value: cond.Value,
		}, fields)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(strings.TrimSpace(cond.Value)))
	case "gt":
		return parseNum(v) > parseNum(cond.Value)
	case "lt":
		return parseNum(v) < parseNum(cond.Value)
	case "not_empty":
		return v != ""
	}
	return true
}

func parseNum(s string) int64 {
	var n int64
	fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n
}

// notificationData is the placeholder context rendered into a notification
// template. Field names are the {{placeholders}} a merchant can use.
type notificationData struct {
	RefCode   string
	Amount    string
	Items     string
	FormURL   string
	BuyerName string
	Date      string
	// Fields carries the submitted field values so a merchant can reference
	// {{field_id}} directly in the subject/body.
	Fields map[string]string
	// Issue #438: QR ordering flow — the table number the QR stand encoded.
	TableNumber string
}

// renderNotification renders a notification subject+body from template strings
// using the standard placeholders plus any submitted field value. A template
// error falls back to the raw string so a bad merchant template can never
// block a notification.
func renderNotification(subject, body string, ref NotificationRef) (subj, rendered string) {
	data := notificationData{
		RefCode:   ref.RefCode,
		Amount:    fmt.Sprintf("%.2f", float64(ref.AmountSen)/100),
		Items:     ref.ItemsSummary,
		FormURL:   ref.FormURL,
		BuyerName: ref.BuyerName,
		Date:      time.Now().Format("2006-01-02"),
		Fields:    ref.Fields,
		// Issue #438: QR ordering flow — the table number the QR stand encoded.
		TableNumber: ref.TableNumber,
	}
	funcs := template.FuncMap{
		"ref_code":   func() string { return data.RefCode },
		"amount":     func() string { return data.Amount },
		"items":      func() string { return data.Items },
		"form_url":   func() string { return data.FormURL },
		"buyer_name": func() string { return data.BuyerName },
		"date":       func() string { return data.Date },
		// Issue #438: QR ordering flow — the table number the QR stand encoded.
		"table_number": func() string { return data.TableNumber },
	}
	// The responder's identity, registered as ALWAYS-defined. text/template
	// REFUSES to parse a template that names an undefined function, and
	// renderNotificationTemplate then falls back to the raw source — so a
	// placeholder the submission did not carry would be mailed as its own
	// literal text ({{email}}), not as an empty string. That is the shape a
	// response form's default notification hits: it shows who responded, and
	// a form need not have a name or email block at all. Defining the two keys
	// here makes an absent value render EMPTY. The submitted field values below
	// override them, so a present value still wins.
	funcs["full_name"] = func() string { return data.BuyerName }
	funcs["email"] = func() string { return "" }
	// Register every submitted field as a template function so {{field_id}}
	// resolves to the submitted value. A block id that is not a valid template
	// identifier is SKIPPED, because text/template PANICS on such a FuncMap key
	// (text/template/funcs.go `addValueFuncs` → `goodName`) and that panic
	// escaped renderNotification's contract and 500'd the public submit route
	// (issue #635).
	//
	// The builder mints ids with `Math.random().toString(36).slice(2, 9)`
	// (web/src/app/builder/page.tsx), which is base36, so an id routinely leads
	// with a digit (`4nwz0vg`) — the exact shape that reached this loop.
	//
	// Nothing addressable is lost by skipping: a name that is not a valid
	// template identifier is not parseable as a placeholder either, so
	// `{{4nwz0vg}}` could never have resolved (the lexer reads the leading digit
	// as a number). The submitted value still drives conditional logic
	// (notificationConditionMet) and `{field_id}` recipients
	// (resolveNotificationRecipient) — both read ref.Fields, not this map.
	for k, v := range data.Fields {
		if !isTemplateFuncName(k) {
			continue
		}
		kv := v
		funcs[k] = func() string { return kv }
	}
	subj = renderNotificationTemplate(subject, funcs, data)
	rendered = renderNotificationTemplate(body, funcs, data)
	return subj, rendered
}

// isTemplateFuncName reports whether name may be used as a text/template
// FuncMap key. text/template PANICS on any other name (text/template/funcs.go
// addValueFuncs → goodName), and that panic is not catchable by the parse-error
// fallback below it. The rule is restated here rather than exported by
// text/template, so it is pinned by a test against the stdlib's own behaviour
// (TestTemplateFuncNameMatchesStdlibGoodName) instead of by comment.
func isTemplateFuncName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case i == 0 && !unicode.IsLetter(r):
			return false
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			return false
		}
	}
	return true
}

// renderNotificationTemplate executes a source template against the notification
// data, falling back to the raw source on any failure — including a PANIC.
//
// The panic half is load-bearing (issue #635): text/template's Funcs PANICS
// (`function name %q is not a valid identifier`) when handed a FuncMap key that
// is not a valid identifier, and that happens BEFORE Parse, so neither the
// parse-error nor the execute-error fallback below can see it. A panic here
// escaped this function's documented "a bad merchant template can never block a
// notification" contract and reached the HTTP recoverer, turning a form
// submission into a 500. The caller skips invalid field ids so this should be
// unreachable; the recover makes the contract true rather than intended.
func renderNotificationTemplate(src string, funcs template.FuncMap, data notificationData) (out string) {
	if strings.TrimSpace(src) == "" {
		return src
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("notify_render_panic_recovered", "err", rec)
			out = src
		}
	}()
	t, err := texttemplate.New("notify").Funcs(funcs).Parse(src)
	if err != nil {
		return src
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return src
	}
	return b.String()
}

var _ = htmltemplate.HTMLEscape
