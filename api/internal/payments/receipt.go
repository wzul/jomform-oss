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

// defaultReceiptSubject is the subject used when a space has not customised
// its email settings (issue #107). Matches the previous hardcoded subject.
const defaultReceiptSubject = "Resit pembayaran — {{ref_code}}"

// defaultReceiptBody is the body used when a space has not customised its
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
	RefCode      string
	BuyerEmail   string
	BuyerName    string
	AmountSen    int64
	ItemsSummary string
	FormURL      string
	SpaceID      string // resolves per-space email settings (issue #107)
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

// SendSaleReceipt emails the buyer a receipt rendered from the space's email
// settings (issue #107). Respects receipt_enabled (off = no buyer email) and
// suppression (#56). Failures are logged but NEVER fail the payment flow.
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

	// Per-space settings (issue #107). Defaults reproduce the old output.
	subject, body, html := defaultReceiptSubject, defaultReceiptBody, false
	receiptEnabled := true
	notifyMerchant := false
	merchantEmail := ""
	if s.Spaces != nil && ref.SpaceID != "" {
		if st, err := s.Spaces.EmailSettings(ctx, ref.SpaceID); err == nil {
			subject, body, html = st.ReceiptSubject, st.ReceiptBody, st.HTML
			receiptEnabled = st.ReceiptEnabled
			notifyMerchant = st.NotifyMerchant
			merchantEmail = st.MerchantNotifyEmail
		}
	}

	subj, rendered := renderReceipt(ref, subject, body, html)

	// Send the buyer receipt only when enabled.
	if receiptEnabled {
		s.sendReceipt(ctx, ref, subj, rendered, html)
	}

	// Also notify the merchant (copy or "new sale" notification).
	if notifyMerchant {
		s.notifyMerchant(ctx, ref, subj, rendered, html, merchantEmail)
	}
}

// sendReceipt delivers the buyer receipt (with PDF attachment when available).
func (s *Service) sendReceipt(ctx context.Context, ref SaleRef, subject, body string, html bool) {
	if pdf, err := s.ReceiptPDF(ref); err == nil && len(pdf) > 0 {
		if err := s.Receipts.Sender.SendWithAttachment(ref.BuyerEmail, subject, body,
			"resit-"+ref.RefCode+".pdf", pdf); err != nil {
			slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
		} else {
			slog.Info("receipt_sent_pdf", "ref", ref.RefCode, "to", ref.BuyerEmail)
		}
		return
	}
	// fallback: text-only
	if err := s.Receipts.Sender.Send(ref.BuyerEmail, subject, body); err != nil {
		slog.Warn("receipt_send_failed", "ref", ref.RefCode, "err", err)
	} else {
		slog.Info("receipt_sent", "ref", ref.RefCode, "to", ref.BuyerEmail)
	}
}

// notifyMerchant sends a copy of the receipt (or a "new sale" notification)
// to the merchant. The recipient is merchant_notify_email when set, else the
// space owner's account email. Suppression is NOT applied to the merchant —
// they opted in and must always get their sale notification.
func (s *Service) notifyMerchant(ctx context.Context, ref SaleRef, subject, body string, html bool, merchantEmail string) {
	to := strings.TrimSpace(merchantEmail)
	if to == "" {
		// Resolve the merchant's email from the space owner's account.
		if s.Spaces != nil && ref.SpaceID != "" {
			if owner, err := s.Spaces.ByID(ctx, ref.SpaceID); err == nil && owner != nil {
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
