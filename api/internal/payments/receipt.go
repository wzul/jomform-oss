// Receipt email on successful payment (SES SMTP).
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

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// Receipts bundles email config; nil disables receipts entirely.
type Receipts struct {
	Sender *email.Sender
	Base   string
	// Suppression (issue #56): consult before sending so a buyer who hard
	// bounces or complains never receives another receipt. Nil disables the
	// check. Receipts are NOT gated by the verification breaker — paying
	// customers must get their receipts.
	Suppression email.Suppressor
}

// defaultReceiptSubject / defaultReceiptBody are the language-specific receipt
// templates. They live in the shared transactional dictionary
// (api/internal/emailtext) so a receipt, a merchant notification and every auth
// email cannot drift apart — the same strings used to be duplicated here, in
// dbstore/form_email.go and in migration 000021.
func defaultReceipt(lang string) (subject, body string) {
	m := emailtext.Text(lang, emailtext.Receipt, nil)
	return m.Subject, m.Body
}

// SaleRef carries the minimal receipt fields.
type SaleRef struct {
	// SaleID is the sale's uuid, used to persist the SES MessageID back onto
	// the sale row for open/click correlation (issue #106). Empty when the
	// caller has no sale id (e.g. a bare receipt test).
	SaleID       string
	RefCode      string
	BuyerEmail   string
	BuyerName    string
	AmountSen    int64
	ItemsSummary string
	FormURL      string
	WorkspaceID  string // resolves per-workspace email settings (issue #107)
	FormID       string // resolves per-form email settings override (issue #177)
	TIN          string // LHDN e-invoicing (optional)
	MSIC         string

	// FormLang / WorkspaceLang are the BUYER-facing language inputs (language
	// consistency): the form's own `config.lang` override and the workspace's
	// `default_lang`. They resolve to the receipt's language in that order,
	// falling back to English.
	//
	// They are carried on the ref rather than looked up here because the caller
	// already holds the form and the workspace row, and because it makes the
	// two-axis rule visible at the call site: these fields are the form/workspace
	// axis, and the merchant's personal users.lang must NEVER be assigned to
	// them.
	FormLang      string
	WorkspaceLang string
}

// receiptData is the placeholder context rendered into a receipt template.
// Field names are the {{placeholders}} a merchant can use in the editor.
type receiptData struct {
	RefCode   string
	Amount    string // formatted "123.45" (RM, two decimals from integer sen)
	Items     string
	FormURL   string
	BuyerName string
	Date      string
}

// renderReceiptBody builds the plain-text receipt body from a SaleRef. Kept
// as a pure function so the money/URL formatting is unit-testable (issue #99).
// It renders the built-in template for lang.
func renderReceiptBody(ref SaleRef, lang string) string {
	subject, body := defaultReceipt(lang)
	_, body = renderReceipt(ref, subject, body, lang, false)
	return body
}

// renderReceipt renders a receipt subject+body from a template string using
// the sale data. html=true uses html/template (auto-escaping, no XSS);
// otherwise text/template. Empty subject/body fall back to the built-in
// template for lang.
func renderReceipt(ref SaleRef, subject, body, lang string, html bool) (subj, rendered string) {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(body) == "" {
		defSubject, defBody := defaultReceipt(lang)
		if strings.TrimSpace(subject) == "" {
			subject = defSubject
		}
		if strings.TrimSpace(body) == "" {
			body = defBody
		}
	}
	data := receiptData{
		RefCode:   ref.RefCode,
		Amount:    fmt.Sprintf("%.2f", float64(ref.AmountSen)/100),
		Items:     ref.ItemsSummary,
		FormURL:   ref.FormURL,
		BuyerName: ref.BuyerName,
		Date:      time.Now().Format("2006-01-02"),
	}
	subj = renderTemplate(subject, data, html)
	rendered = renderTemplate(body, data, html)
	return subj, rendered
}

// renderTemplate executes a single template string against the receipt data.
// The {{placeholders}} (no leading dot) are registered as template functions
// so a merchant can write {{ref_code}}, {{amount}}, etc. directly. html=true
// uses html/template (context-aware auto-escaping); otherwise text/template.
// A template error falls back to the raw string so a bad merchant template
// can never block a paying buyer's receipt.
func renderTemplate(src string, data receiptData, html bool) string {
	funcs := template.FuncMap{
		"ref_code":   func() string { return data.RefCode },
		"amount":     func() string { return data.Amount },
		"items":      func() string { return data.Items },
		"form_url":   func() string { return data.FormURL },
		"buyer_name": func() string { return data.BuyerName },
		"date":       func() string { return data.Date },
	}
	if html {
		t, err := htmltemplate.New("receipt").Funcs(funcs).Parse(src)
		if err != nil {
			return src
		}
		var b bytes.Buffer
		if err := t.Execute(&b, data); err != nil {
			return src
		}
		return b.String()
	}
	t, err := texttemplate.New("receipt").Funcs(funcs).Parse(src)
	if err != nil {
		return src
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return src
	}
	return b.String()
}

// SendSaleReceipt emails the buyer a receipt rendered from the workspace's email
// settings (issue #107). Respects receipt_enabled (off = no buyer email) and
// suppression (#56). Failures are logged but NEVER fail the payment flow.
//
// Issue #199: when the Service has an EmailQueue wired, the receipt + merchant
// notification are enqueued and sent by a background worker — the request
// handler returns immediately and a slow SES call never blocks the payment
// callback. Rendering (cheap local work) still happens here so the worker only
// performs the SES network call. When EmailQueue is nil (bare-mode tests) the
// send happens synchronously as before.
func (s *Service) SendSaleReceipt(ctx context.Context, ref SaleRef) {
	if s.Receipts == nil || s.Receipts.Sender == nil || ref.BuyerEmail == "" {
		return
	}
	// Issue #56: never resend a receipt to a suppressed address.
	if s.Receipts.Suppression != nil {
		sup, err := email.ShouldSuppress(ctx, s.Receipts.Suppression, ref.BuyerEmail)
		if err != nil {
			// suppression check failure: log, don't block a paying buyer's receipt
			slog.Warn("receipt_suppression_check_error", "err", err, "ref", ref.RefCode)
		} else if sup {
			slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
			return
		}
	}

	// Per-workspace settings (issue #107). Defaults reproduce the old output.
	var ws *dbstore.EmailSettings
	if s.Workspaces != nil && ref.WorkspaceID != "" {
		if st, err := s.Workspaces.EmailSettings(ctx, ref.WorkspaceID); err == nil {
			ws = st
		}
	}
	// Issue #177/#453: resolve per-form override -> workspace -> default. A
	// per-form override wins field-by-field: non-empty subject/body/merchant
	// email, or an explicit on/off toggle. A form with no override at all
	// inherits the workspace settings unchanged.
	var formSt *dbstore.FormEmailSettings
	if s.Forms != nil && ref.FormID != "" {
		if st, err := s.Forms.FormEmailSettings(ctx, ref.FormID); err == nil {
			formSt = st
		}
	}
	// Language consistency: the BUYER-facing language is
	// form.config.lang ?? workspace_settings.default_lang ?? en — never the
	// merchant's personal users.lang. A guest buyer has no profile to consult,
	// which is why this is the only correct axis here. Resolved before the
	// settings so an inherited (never-customised) system template is rendered in
	// the buyer's language rather than the Malay literal the DB defaults carry.
	lang := dbstore.EmailLang(ref.FormLang, ref.WorkspaceLang)
	resolved := dbstore.ResolveFormEmailSettings(formSt, ws, lang)
	subject, body, html := resolved.ReceiptSubject, resolved.ReceiptBody, resolved.HTML
	receiptEnabled := resolved.ReceiptEnabled
	notifyMerchant := resolved.NotifyMerchant
	merchantEmail := resolved.MerchantNotifyEmail

	subj, rendered := renderReceipt(ref, subject, body, lang, html)

	// Issue #199: background path — enqueue the fully-rendered jobs and return.
	if s.EmailQueue != nil {
		if receiptEnabled {
			// Generate the PDF here (cheap local work) so the worker only does
			// the SES call. A PDF failure falls back to a text-only receipt.
			var pdf []byte
			if p, err := s.ReceiptPDF(ref); err == nil && len(p) > 0 {
				pdf = p
			}
			if err := s.EmailQueue.Enqueue(ctx, emailqueue.Job{
				Type: "receipt", To: ref.BuyerEmail,
				Subject: subj, Body: rendered, Lang: lang,
				AccountID: ref.WorkspaceID, SaleRef: ref.RefCode, SaleID: ref.SaleID,
				PDFFilename: "resit-" + ref.RefCode + ".pdf", PDFData: pdf,
			}); err != nil {
				slog.Warn("receipt_enqueue_failed", "ref", ref.RefCode, "err", err)
			}
		}
		if notifyMerchant {
			to := s.resolveMerchantEmail(ctx, ref, merchantEmail)
			if to == "" {
				slog.Warn("receipt_merchant_notify_no_recipient", "ref", ref.RefCode)
			} else if err := s.EmailQueue.Enqueue(ctx, emailqueue.Job{
				Type: "merchant_notify", To: to,
				Subject: emailtext.Phrase(lang, emailtext.PhraseMerchantPrefix) + subj,
				Body:    rendered, Lang: lang,
				AccountID: ref.WorkspaceID, SaleRef: ref.RefCode,
			}); err != nil {
				slog.Warn("receipt_merchant_notify_enqueue_failed", "ref", ref.RefCode, "err", err)
			}
		}
		return
	}

	// Synchronous path (bare-mode tests / no queue wired).
	persistReceiptMessageID := func(messageID string) {
		if messageID == "" || ref.SaleID == "" || s.Sales == nil {
			return
		}
		if err := s.Sales.SetReceiptMessageID(ctx, ref.SaleID, messageID); err != nil {
			slog.Warn("receipt_message_id_store_failed", "ref", ref.RefCode, "err", err)
		}
	}

	// Send the buyer receipt only when enabled.
	if receiptEnabled {
		s.sendReceipt(ctx, ref, lang, subj, rendered, html, persistReceiptMessageID)
	}

	// Also notify the merchant (copy or "new sale" notification).
	if notifyMerchant {
		s.notifyMerchant(ctx, ref, lang, subj, rendered, html, merchantEmail)
	}
}

// resolveMerchantEmail returns the merchant notification recipient: the
// configured merchant_notify_email, else the workspace owner's account email.
func (s *Service) resolveMerchantEmail(ctx context.Context, ref SaleRef, merchantEmail string) string {
	to := strings.TrimSpace(merchantEmail)
	if to == "" {
		if s.Workspaces != nil && ref.WorkspaceID != "" {
			if owner, err := s.Workspaces.ByID(ctx, ref.WorkspaceID); err == nil && owner != nil {
				if s.Users != nil {
					if u, uerr := s.Users.ByID(ctx, owner.Owner); uerr == nil && u != nil {
						to = u.Email
					}
				}
			}
		}
	}
	return to
}

// sendReceipt delivers the buyer receipt (with PDF attachment when available).
//
// The recipient was already checked at (*email.Sender).SendReceipt — the
// pre-send choke point — so a suppression hit here arrives as
// email.ErrSuppressed. It is logged and swallowed: the receipt is correctly not
// sent, no MessageID is persisted, and the payment flow is unaffected.
func (s *Service) sendReceipt(ctx context.Context, ref SaleRef, lang, subject, body string, html bool, persistReceiptMessageID func(string)) {
	if pdf, err := s.ReceiptPDF(ref); err == nil && len(pdf) > 0 {
		messageID, err := s.Receipts.Sender.SendReceiptWithAttachment(ref.BuyerEmail, subject, body, lang,
			"resit-"+ref.RefCode+".pdf", pdf)
		if err != nil {
			if errors.Is(err, email.ErrSuppressed) {
				slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
				return
			}
			slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
		} else {
			slog.Info("receipt_sent_pdf", "ref", ref.RefCode, "to", ref.BuyerEmail, "lang", lang)
			persistReceiptMessageID(messageID)
		}
		return
	}
	// fallback: text-only
	//
	// Reachability note (issue #601): ReceiptPDF has no failure path a caller can
	// provoke — it renders from a fixed template and returned a valid PDF for
	// every SaleRef tried, including the zero value — so in practice a receipt
	// with a buyer address always takes the attachment branch above. This branch
	// is kept as the guard-of-last-resort for a ref whose PDF cannot be built,
	// and it carries the same suppression handling so it cannot become a silent
	// hole if that ever changes. It is deliberately NOT covered by a test: no
	// input reaches it, so a test here would assert behaviour nobody can produce.
	messageID, err := s.Receipts.Sender.SendReceipt(ref.BuyerEmail, subject, body, lang)
	if err != nil {
		if errors.Is(err, email.ErrSuppressed) {
			slog.Warn("receipt_suppressed_skipped", "ref", ref.RefCode, "to", ref.BuyerEmail)
			return
		}
		slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
	} else {
		slog.Info("receipt_sent", "ref", ref.RefCode, "to", ref.BuyerEmail, "lang", lang)
		persistReceiptMessageID(messageID)
	}
}

// notifyMerchant sends a copy of the receipt (or a "new sale" notification)
// to the merchant. The recipient is merchant_notify_email when set, else the
// workspace owner's account email.
//
// Suppression APPLIES to the merchant too. This deliberately reverses the
// earlier exemption ("the merchant opted in, always notify"): a merchant whose
// mailbox hard-bounces keeps generating bounces from the same sending identity
// that delivers buyer receipts, so exempting them is what lets one bad mailbox
// damage deliverability for every buyer. A merchant who stops receiving
// notifications changes the address in workspace email settings, which is the
// action the notification itself enables.
func (s *Service) notifyMerchant(ctx context.Context, ref SaleRef, lang, subject, body string, html bool, merchantEmail string) {
	to := strings.TrimSpace(merchantEmail)
	if to == "" {
		// Resolve the merchant's email from the workspace owner's account.
		if s.Workspaces != nil && ref.WorkspaceID != "" {
			if owner, err := s.Workspaces.ByID(ctx, ref.WorkspaceID); err == nil && owner != nil {
				if s.Users != nil {
					if u, uerr := s.Users.ByID(ctx, owner.Owner); uerr == nil && u != nil {
						to = u.Email
					}
				}
			}
		}
	}
	if to == "" {
		slog.Warn("receipt_merchant_notify_no_recipient", "ref", ref.RefCode)
		return
	}
	// Prefix the subject so the merchant can tell it apart from the buyer copy.
	// The prefix is in the BUYER-facing language: this is the merchant's copy OF
	// the buyer's receipt, and it deliberately does not switch to his personal
	// users.lang half-way through the message.
	merchantSubject := emailtext.Phrase(lang, emailtext.PhraseMerchantPrefix) + subject
	if err := s.Receipts.Sender.Send(to, merchantSubject, body, lang); err != nil {
		if errors.Is(err, email.ErrSuppressed) {
			slog.Warn("receipt_merchant_suppressed_skipped", "ref", ref.RefCode, "to", to)
			return
		}
		slog.Warn("receipt_merchant_notify_failed", "ref", ref.RefCode, "to", to, "err", err)
	} else {
		slog.Info("receipt_merchant_notify_sent", "ref", ref.RefCode, "to", to, "lang", lang)
	}
}
