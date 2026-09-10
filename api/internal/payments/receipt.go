// Receipt email on successful payment (SES SMTP).
package payments

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"
	"text/template"
	texttemplate "text/template"
	"time"

	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
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

// defaultReceiptSubject is the subject used when a workspace has not customised
// its email settings (issue #107). Matches the previous hardcoded subject.
const defaultReceiptSubject = "Resit pembayaran — {{ref_code}}"

// defaultReceiptBody is the body used when a workspace has not customised its
// email settings. Matches the previous hardcoded plain-text receipt exactly.
const defaultReceiptBody = `Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: {{form_url}}`

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
	WorkspaceID      string // resolves per-workspace email settings (issue #107)
	FormID       string // resolves per-form email settings override (issue #177)
	TIN          string // LHDN e-invoicing (optional)
	MSIC         string
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
// When subject/body are empty (defaults), the previous hardcoded output is
// reproduced exactly.
func renderReceiptBody(ref SaleRef) string {
	_, body := renderReceipt(ref, defaultReceiptSubject, defaultReceiptBody, false)
	return body
}

// renderReceipt renders a receipt subject+body from a template string using
// the sale data. html=true uses html/template (auto-escaping, no XSS);
// otherwise text/template. Empty subject/body fall back to the defaults.
func renderReceipt(ref SaleRef, subject, body string, html bool) (subj, rendered string) {
	if strings.TrimSpace(subject) == "" {
		subject = defaultReceiptSubject
	}
	if strings.TrimSpace(body) == "" {
		body = defaultReceiptBody
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
	subject, body, html := defaultReceiptSubject, defaultReceiptBody, false
	receiptEnabled := true
	notifyMerchant := false
	merchantEmail := ""
	if s.Workspaces != nil && ref.WorkspaceID != "" {
		if st, err := s.Workspaces.EmailSettings(ctx, ref.WorkspaceID); err == nil {
			subject, body, html = st.ReceiptSubject, st.ReceiptBody, st.HTML
			receiptEnabled = st.ReceiptEnabled
			notifyMerchant = st.NotifyMerchant
			merchantEmail = st.MerchantNotifyEmail
		}
	}
	// Issue #177: a per-form email override (non-empty subject or body) takes
	// precedence over the workspace defaults for this form's receipts.
	if s.Forms != nil && ref.FormID != "" {
		if st, err := s.Forms.FormEmailSettings(ctx, ref.FormID); err == nil &&
			(st.ReceiptSubject != "" || st.ReceiptBody != "") {
			subject, body, html = st.ReceiptSubject, st.ReceiptBody, st.HTML
			receiptEnabled = st.ReceiptEnabled
			notifyMerchant = st.NotifyMerchant
			merchantEmail = st.MerchantNotifyEmail
		}
	}

	subj, rendered := renderReceipt(ref, subject, body, html)

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
				Subject: subj, Body: rendered,
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
				Subject: "Jualan baharu — " + subj, Body: rendered,
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
		s.sendReceipt(ctx, ref, subj, rendered, html, persistReceiptMessageID)
	}

	// Also notify the merchant (copy or "new sale" notification).
	if notifyMerchant {
		s.notifyMerchant(ctx, ref, subj, rendered, html, merchantEmail)
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
func (s *Service) sendReceipt(ctx context.Context, ref SaleRef, subject, body string, html bool, persistReceiptMessageID func(string)) {
	if pdf, err := s.ReceiptPDF(ref); err == nil && len(pdf) > 0 {
		messageID, err := s.Receipts.Sender.SendReceiptWithAttachment(ref.BuyerEmail, subject, body,
			"resit-"+ref.RefCode+".pdf", pdf)
		if err != nil {
			slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
		} else {
			slog.Info("receipt_sent_pdf", "ref", ref.RefCode, "to", ref.BuyerEmail)
			persistReceiptMessageID(messageID)
		}
		return
	}
	// fallback: text-only
	messageID, err := s.Receipts.Sender.SendReceipt(ref.BuyerEmail, subject, body)
	if err != nil {
		slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
	} else {
		slog.Info("receipt_sent", "ref", ref.RefCode, "to", ref.BuyerEmail)
		persistReceiptMessageID(messageID)
	}
}

// notifyMerchant sends a copy of the receipt (or a "new sale" notification)
// to the merchant. The recipient is merchant_notify_email when set, else the
// workspace owner's account email. Suppression is NOT applied to the merchant —
// they opted in and must always get their sale notification.
func (s *Service) notifyMerchant(ctx context.Context, ref SaleRef, subject, body string, html bool, merchantEmail string) {
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
	merchantSubject := "Jualan baharu — " + subject
	if err := s.Receipts.Sender.Send(to, merchantSubject, body); err != nil {
		slog.Warn("receipt_merchant_notify_failed", "ref", ref.RefCode, "to", to, "err", err)
	} else {
		slog.Info("receipt_merchant_notify_sent", "ref", ref.RefCode, "to", to)
	}
}
