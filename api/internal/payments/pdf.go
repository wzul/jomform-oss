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
		text.NewRow(8, "Resit Pembayaran / Payment Receipt", props.Text{Size: 12, Align: align.Center}),
	)

	// Details grid
	rows := [][2]string{
		{"Rujukan / Reference", ref.RefCode},
		{"Tarikh / Date", time.Now().Format("02-01-2006 15:04 MYT")},
		{"Item", ref.ItemsSummary},
		{"Jumlah / Amount", fmt.Sprintf("RM %.2f", float64(ref.AmountSen)/100)},
	}
	if ref.TIN != "" {
		rows = append(rows, [2]string{"e-Invoice TIN (LHDN)", ref.TIN})
	}
	if ref.MSIC != "" {
		rows = append(rows, [2]string{"MSIC", ref.MSIC})
	}
	rows = append(rows, [2]string{"Status", "PAID - Pembayaran diterima"})
	for _, r := range rows {
		m.AddRow(9,
			text.NewCol(4, r[0], props.Text{Size: 10, Style: fontstyle.Bold}),
			text.NewCol(8, r[1], props.Text{Size: 10}),
		)
	}

	m.AddRow(12, text.NewCol(12,
		fmt.Sprintf("Terima kasih! Simpan dokumen ini sebagai resit rasmi. %s", ref.FormURL),
		props.Text{Size: 8, Align: align.Center, Top: 4}))

	doc, err := m.Generate()
	if err != nil {
		return nil, err
	}
	return doc.GetBytes(), nil
}
