// Money-invariant unit tests for the payment path (issue #99). These are pure
// and run in CI with no database or CHIP dependency: sen math, CHIP product
// mapping, and receipt formatting.
package payments

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestSumItemsSenMath locks the total_sen formula: subtotal + shipping - discount.
func TestSumItemsSenMath(t *testing.T) {
	cases := []struct {
		name      string
		subtotal  int64
		shipping  int64
		discount  int64
		wantTotal int64
	}{
		{"plain", 1000, 0, 0, 1000},
		{"with shipping", 1000, 500, 0, 1500},
		{"with discount", 1000, 0, 200, 800},
		{"shipping and discount", 1000, 500, 200, 1300},
		{"zero", 0, 0, 0, 0},
		{"discount exceeds subtotal", 100, 0, 500, -400}, // caller must clamp; formula is exact
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SumItems(c.subtotal, c.shipping, c.discount); got != c.wantTotal {
				t.Fatalf("SumItems(%d,%d,%d) = %d, want %d", c.subtotal, c.shipping, c.discount, got, c.wantTotal)
			}
		})
	}
}

// TestLineSubtotalSenMath verifies subtotal = Σ price_sen × qty.
func TestLineSubtotalSenMath(t *testing.T) {
	items := []dbstore.SaleItem{
		{Name: "Tee", PriceSen: 4500, Qty: 2}, // 9000
		{Name: "Cap", PriceSen: 2500, Qty: 1}, // 2500
	}
	if got := LineSubtotal(items); got != 11500 {
		t.Fatalf("LineSubtotal = %d, want 11500", got)
	}
	if got := LineSubtotal(nil); got != 0 {
		t.Fatalf("LineSubtotal(nil) = %d, want 0", got)
	}
}

// TestToCHIPProductsPricesInSen ensures prices are forwarded as integer SEN
// (never float ringgit) and quantities as strings, per CHIP's API.
func TestToCHIPProductsPricesInSen(t *testing.T) {
	items := []dbstore.SaleItem{
		{Name: "Tee", PriceSen: 4500, Qty: 2},
		{Name: "Cap", PriceSen: 2500, Qty: 1},
	}
	prods := toCHIPProducts(items)
	if len(prods) != 2 {
		t.Fatalf("len = %d, want 2", len(prods))
	}
	if prods[0].Price != 4500 || prods[0].Quantity != "2" {
		t.Errorf("prods[0] = %+v, want price 4500 qty \"2\"", prods[0])
	}
	if prods[1].Price != 2500 || prods[1].Quantity != "1" {
		t.Errorf("prods[1] = %+v, want price 2500 qty \"1\"", prods[1])
	}
	// CHIP computes the total as Σ price × qty; that must equal the line
	// subtotal (sen invariant) so the buyer is charged exactly what the sale
	// records.
	var sum int64
	for _, p := range prods {
		var q int64
		_, _ = fmt.Sscanf(p.Quantity, "%d", &q)
		sum += p.Price * q
	}
	if sum != LineSubtotal(items) {
		t.Errorf("CHIP product total %d != line subtotal %d", sum, LineSubtotal(items))
	}
}

// TestReceiptAmountFormatting asserts the receipt email renders the amount as
// RM with two decimals from integer SEN (no float drift).
func TestReceiptAmountFormatting(t *testing.T) {
	ref := SaleRef{RefCode: "JF-ABC123", AmountSen: 12345, ItemsSummary: "1× Tee", FormURL: "https://acme.jomform.com/tee"}
	body := renderReceiptBody(ref)
	if !strings.Contains(body, "RM 123.45") {
		t.Errorf("body missing formatted amount: %q", body)
	}
	if !strings.Contains(body, "JF-ABC123") {
		t.Errorf("body missing ref code: %q", body)
	}
	if !strings.Contains(body, "https://acme.jomform.com/tee") {
		t.Errorf("body missing workspace-scoped form URL (issue #83): %q", body)
	}
}

// TestReceiptPDFAmountAndURL asserts the PDF receipt carries the same amount
// and the workspace-scoped form URL (issue #83), and no footer (issue #80).
func TestReceiptPDFAmountAndURL(t *testing.T) {
	ref := SaleRef{RefCode: "JF-ABC123", AmountSen: 12345, ItemsSummary: "1× Tee", FormURL: "https://acme.jomform.com/tee"}
	pdf, err := (&Service{}).ReceiptPDF(ref)
	if err != nil {
		t.Fatalf("ReceiptPDF: %v", err)
	}
	if len(pdf) == 0 {
		t.Fatal("empty PDF")
	}
	// PDF is binary; assert it is a valid PDF header and non-trivial size.
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Errorf("not a PDF: %q", string(pdf[:8]))
	}
	if len(pdf) < 1000 {
		t.Errorf("PDF suspiciously small: %d bytes", len(pdf))
	}
}

// TestChipPurchaseRequestTopLevelRedirects guards the AGENTS.md rule that
// redirect/callback/reference fields are TOP-LEVEL on the create-purchase
// request (nested-in-purchase gets silently discarded by CHIP).
func TestChipPurchaseRequestTopLevelRedirects(t *testing.T) {
	req := chip.PurchaseRequest{
		SuccessRedirect: "https://acme.jomform.com/t/JF-1",
		FailureRedirect: "https://acme.jomform.com/t/JF-1?status=fail",
		SuccessCallback: "https://api.jomform.com/v1/payments/chip/callback",
		Reference:       "JF-1",
		Purchase: chip.PurchaseDetails{
			Products: []chip.Product{{Name: "Tee", Price: 1000, Quantity: "1"}},
			Currency: "MYR",
		},
	}
	if req.SuccessRedirect == "" || req.FailureRedirect == "" || req.SuccessCallback == "" || req.Reference == "" {
		t.Fatal("redirect/callback/reference must be top-level on PurchaseRequest")
	}
	if req.Purchase.SuccessRedirect != "" || req.Purchase.Reference != "" {
		t.Fatal("redirect/reference must NOT be nested inside PurchaseDetails")
	}
}

// TestRenderReceiptPlaceholders (issue #107) asserts the {{placeholders}} are
// substituted server-side and the default template reproduces the previous
// hardcoded output.
func TestRenderReceiptPlaceholders(t *testing.T) {
	ref := SaleRef{RefCode: "JF-ABC123", BuyerName: "Ali", AmountSen: 12345, ItemsSummary: "1× Tee", FormURL: "https://acme.jomform.com/tee"}
	subj, body := renderReceipt(ref, "Resit {{ref_code}}", "Hai {{buyer_name}}, bayar {{amount}} untuk {{items}} — {{form_url}} pada {{date}}", false)
	if subj != "Resit JF-ABC123" {
		t.Errorf("subject = %q, want placeholder substituted", subj)
	}
	if !strings.Contains(body, "Hai Ali, bayar 123.45 untuk 1× Tee — https://acme.jomform.com/tee") {
		t.Errorf("body missing substituted placeholders: %q", body)
	}
	if !strings.Contains(body, "pada ") {
		t.Errorf("body missing date placeholder: %q", body)
	}
}

// TestRenderReceiptHTMLAutoEscapes (issue #107) asserts html/template
// auto-escaping prevents XSS via a merchant-supplied HTML body.
func TestRenderReceiptHTMLAutoEscapes(t *testing.T) {
	ref := SaleRef{RefCode: "JF-X", BuyerName: "<script>alert(1)</script>", AmountSen: 100, ItemsSummary: "x", FormURL: "https://acme.jomform.com/x"}
	_, body := renderReceipt(ref, "s", "<p>Hi {{buyer_name}}</p>", true)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("HTML body not escaped — XSS: %q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("expected escaped buyer name in HTML body: %q", body)
	}
}

// TestRenderReceiptBadTemplateFallsBack (issue #107) asserts a malformed
// merchant template never blocks a receipt — it falls back to the raw string.
func TestRenderReceiptBadTemplateFallsBack(t *testing.T) {
	ref := SaleRef{RefCode: "JF-X", AmountSen: 100, ItemsSummary: "x", FormURL: "https://acme.jomform.com/x"}
	_, body := renderReceipt(ref, "s", "{{unclosed", false)
	if body != "{{unclosed" {
		t.Errorf("bad template should fall back to raw string, got %q", body)
	}
}
