// PDF receipt generation (maroto v2.4) for paid sales.
package payments

import (
	"fmt"
	"time"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

// receiptText is one line of the receipt in each language it is PRINTED in.
//
// WHY THE RECEIPT IS BILINGUAL AND NOT ONE LANGUAGE. It is a document a merchant
// hands over (and files as the official receipt — it carries the LHDN e-Invoice
// TIN and MSIC rows), not a screen whose language can be resolved from a
// session, and its title plus four of its grid labels have been
// "<Bahasa Malaysia> / <English>" since long before this table existed. Printing
// both halves is the only wording that serves a Malay-first merchant and an
// English-reading buyer at the same time. Making the PDF follow ONE language
// instead is possible — the email body it is attached to already follows
// form.config.lang ?? the workspace's default_lang, which SaleRef carries as
// FormLang / WorkspaceLang — but choosing between "a bilingual document" and
// "follow the buyer's language" is a product decision about a merchant's
// document, not a translation fix, so card t_129664c9 left that choice open and
// instead made the existing bilingual intent consistent.
//
// WHY THE VALUES ARE KEYED BY LANGUAGE rather than written as a single
// "<ms> / <en>" literal: these ARE the two translations of one label. The guard
// that keeps this package's runtime string literals English
// (dbstore/refusal_text_english_guard_test.go) skips map values keyed by a
// language code for exactly that reason — a translation is supposed to be Malay,
// whereas prose that leaks onto a wire or a screen is not.
type receiptText map[string]string

// String renders both languages, "<Bahasa Malaysia> / <English>".
func (t receiptText) String() string {
	ms, en := t["ms"], t["en"]
	switch {
	case ms == "":
		return en
	case en == "" || en == ms:
		// The same word in both languages ("Item", "Status"): print it once.
		// "Item / Item" reads like a typo, not like a bilingual document.
		return ms
	default:
		return ms + " / " + en
	}
}

// ms and en are the halves the layout prints on their own row, for the lines
// where a sentence does not fit a "<ms> / <en>" pair (the footer at 8pt).
func (t receiptText) ms() string { return t["ms"] }
func (t receiptText) en() string { return t["en"] }

// The receipt's wording, in one place. A label that reads the same in both
// languages is deliberately listed with both halves rather than left as a lone
// English literal, so this table is the complete answer to "what does the
// receipt say, and in which language".
var (
	receiptTitle       = receiptText{"ms": "Resit Pembayaran", "en": "Payment Receipt"}
	receiptLabelRef    = receiptText{"ms": "Rujukan", "en": "Reference"}
	receiptLabelDate   = receiptText{"ms": "Tarikh", "en": "Date"}
	receiptLabelItem   = receiptText{"ms": "Item", "en": "Item"}
	receiptLabelFee    = receiptText{"ms": "Caj platform", "en": "Platform fee"}
	receiptLabelAmount = receiptText{"ms": "Jumlah", "en": "Amount"}
	receiptLabelStatus = receiptText{"ms": "Status", "en": "Status"}

	// The status stamp used to be Malay-only ("PAID - Pembayaran diterima"),
	// and it is the one cell a buyer looks at to know the payment went through.
	receiptStatusPaid = receiptText{"ms": "Pembayaran diterima", "en": "PAID"}

	// The footer used to be Malay-only too, under a bilingual title.
	receiptThanks = receiptText{
		"ms": "Terima kasih! Simpan dokumen ini sebagai resit rasmi.",
		"en": "Thank you! Keep this document as your official receipt.",
	}
)

// ReceiptPDF renders a simple branded receipt; returns PDF bytes.
func (s *Service) ReceiptPDF(ref SaleRef) ([]byte, error) {
	cfg := config.NewBuilder().
		WithTopMargin(12).
		WithLeftMargin(12).
		WithRightMargin(12).
		Build()
	m := maroto.New(cfg)

	// Header
	m.AddRows(
		text.NewRow(14, "JomForm", props.Text{Size: 20, Style: fontstyle.Bold, Align: align.Center}),
		text.NewRow(8, receiptTitle.String(), props.Text{Size: 12, Align: align.Center}),
	)

	// Details grid
	rows := receiptDetailRows(ref)
	for _, r := range rows {
		m.AddRow(9,
			text.NewCol(4, r[0], props.Text{Size: 10, Style: fontstyle.Bold}),
			text.NewCol(8, r[1], props.Text{Size: 10}),
		)
	}

	// Footer: the thank-you sentence in both languages, one row each (a pair
	// joined with " / " is unreadable as a centred line at 8pt), then the form
	// the buyer paid on. The rows are sized for 8pt text — a shorter row makes
	// the two sentences print on top of each other.
	m.AddRows(
		text.NewRow(4, ""),
		text.NewRow(5, receiptThanks.ms(), props.Text{Size: 8, Align: align.Center}),
		text.NewRow(5, receiptThanks.en(), props.Text{Size: 8, Align: align.Center}),
		text.NewRow(6, ref.FormURL, props.Text{Size: 8, Align: align.Center}),
	)

	doc, err := m.Generate()
	if err != nil {
		return nil, err
	}
	return doc.GetBytes(), nil
}

// receiptDetailRows is the PDF's label/value grid, split out so a test can
// assert the processing-fee row exists with the FROZEN rate (card t_135c3c72)
// without parsing generated PDF bytes.
func receiptDetailRows(ref SaleRef) [][2]string {
	rows := [][2]string{
		{receiptLabelRef.String(), ref.RefCode},
		{receiptLabelDate.String(), time.Now().Format("02-01-2006 15:04 MYT")},
		{receiptLabelItem.String(), ref.ItemsSummary},
	}
	// Card t_135c3c72: the charge is its OWN row, with the amount frozen at
	// purchase time — never folded into an item price and never recomputed from
	// the merchant's current settings. Card t_1a17d22d: the row is labelled
	// "Platform fee", JomForm's own word, so the value is just the amount.
	if ref.FeeSen > 0 {
		rows = append(rows, [2]string{receiptLabelFee.String(), "RM " + formatSen(ref.FeeSen)})
	}
	rows = append(rows, [2]string{receiptLabelAmount.String(), fmt.Sprintf("RM %.2f", float64(ref.AmountSen)/100)})
	if ref.TIN != "" {
		rows = append(rows, [2]string{"e-Invoice TIN (LHDN)", ref.TIN})
	}
	if ref.MSIC != "" {
		rows = append(rows, [2]string{"MSIC", ref.MSIC})
	}
	rows = append(rows, [2]string{receiptLabelStatus.String(), receiptStatusPaid.String()})
	return rows
}
