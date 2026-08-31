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
}

// SendSaleReceipt emails the buyer a plain-text receipt. Failures are
// logged but NEVER fail the payment flow.
func (s *Service) SendSaleReceipt(ctx context.Context, ref SaleRef) {
	if s.Receipts == nil || s.Receipts.Sender == nil || ref.BuyerEmail == "" {
		return
	}
	body := fmt.Sprintf(receiptTemplate,
		ref.RefCode, float64(ref.AmountSen)/100, ref.ItemsSummary, ref.FormURL)
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
