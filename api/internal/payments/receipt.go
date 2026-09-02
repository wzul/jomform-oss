// Receipt email on successful payment (SES SMTP).
package payments

import (
	"context"
	"fmt"
	"log/slog"

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

const receiptTemplate = `Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : %s
Jumlah  : RM %.2f
Item    : %s
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: %s
`

// SaleRef carries the minimal receipt fields.
type SaleRef struct {
	RefCode      string
	BuyerEmail   string
	AmountSen    int64
	ItemsSummary string
	FormURL      string
	TIN          string // LHDN e-invoicing (optional)
	MSIC         string
}

// renderReceiptBody builds the plain-text receipt body from a SaleRef. Kept
// as a pure function so the money/URL formatting is unit-testable (issue #99).
func renderReceiptBody(ref SaleRef) string {
	return fmt.Sprintf(receiptTemplate,
		ref.RefCode, float64(ref.AmountSen)/100, ref.ItemsSummary, ref.FormURL)
}

// SendSaleReceipt emails the buyer a plain-text receipt. Failures are
// logged but NEVER fail the payment flow.
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
	body := renderReceiptBody(ref)
	subject := "Resit pembayaran — " + ref.RefCode

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
