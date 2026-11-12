package payments

// Card t_69d2dd19, from Wan's two rules on the approved mockup:
//
//	1. The buyer-facing word is "Platform fee" (ms "Caj platform"). Never
//	   "Processing fee" / "Caj pemprosesan" — that wording described a charge the
//	   acquirer was taking, which is not what this line is.
//	2. A fee LINE never names the payment method. "Platform fee RM1.00", never
//	   "Platform fee — FPX". The method name belongs on the SETTINGS screen and in
//	   the merchant's own sale row; anything the BUYER receives must be name-free.
//
// WHY BOTH HALVES ARE ONE TEST. The method name used to ride INTO the fee line
// through a rate label (receipt.go printed "Processing fee — " + FeeRateLabel,
// which starts with the method), so a change that fixes the label but keeps the
// formatter can put the name back. The assertion is therefore over the rendered
// string, not over the format constant: the receipt text and HTML (all five
// languages), the PDF's fee label, and the `purchase.products` line name that
// CHIP shows on its own payment page — the three surfaces a buyer reads.
//
// WHAT IT DOES NOT COVER (stated honestly): the PUBLIC FORM's payment-method
// picker still prints each method's name, because that control's whole job is to
// let the buyer choose a method. The rule is about a FEE LINE, and the picker's
// rows are method choices; the fee it prints beside them is the same
// PlatformFeeLineName audited here.

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// buyerMethodWords are the tokens a buyer must never read on a fee line: the
// scheme names, the two FPX variants, and the wallets listed under e-Wallet.
var buyerMethodWords = []string{
	"FPX", "B2C", "B2B1", "Card", "DuitNow", "e-Wallet", "GrabPay", "Boost",
	// The raw CHIP identifiers, which are even worse than the labels.
	"fpx", "fpx_b2b1", "card", "ewallet", "duitnow_qr",
}

// buyerWrongLabelWords are the withdrawn label and its Malay arm.
var buyerWrongLabelWords = []string{"Processing fee", "processing fee", "Caj pemprosesan", "caj pemprosesan"}

// feeLineProblems reports every banned token a buyer-facing fee string carries.
// Pure over the string so the bite test below can hand it a planted leak.
func feeLineProblems(where, s string) []string {
	var problems []string
	lower := strings.ToLower(s)
	for _, w := range buyerMethodWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			problems = append(problems, where+": names the payment method via "+w+": "+s)
		}
	}
	for _, w := range buyerWrongLabelWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			problems = append(problems, where+": uses the withdrawn label "+w+": "+s)
		}
	}
	return problems
}

func TestNoBuyerFeeLineNamesThePaymentMethod(t *testing.T) {
	langs := []string{"en", "ms", "zh", "ta", "ar"}
	var problems []string

	for _, method := range FeeMethods {
		ref := SaleRef{
			RefCode:      "JF-69",
			ItemsSummary: "Derma surau x 1",
			AmountSen:    10100,
			FeeSen:       100,
			// Every snapshot field is filled: a formatter that reaches for any
			// of them must still not print a name.
			FeeMethod:    method,
			FeeFixedSen:  100,
			FeePercentBp: 0,
		}
		problems = append(problems, feeLineProblems("receipt line ("+method+")", ref.FeeLine())...)
		problems = append(problems, feeLineProblems("receipt items ("+method+")", receiptItems(ref))...)

		for _, lang := range langs {
			problems = append(problems, feeLineProblems("receipt text "+lang+" ("+method+")", renderReceiptBody(ref, lang))...)
			_, html := renderReceipt(ref, "", "", lang, true)
			problems = append(problems, feeLineProblems("receipt html "+lang+" ("+method+")", html)...)
		}

		// The purchase.products line CHIP shows on its own page.
		sale := &dbstore.Sale{
			RefCode: "JF-69", SubtotalSen: 10000, TotalSen: 10100,
			FeeSen: 100, PlatformFeeSen: 100, FeeMethod: method,
			Items: []dbstore.SaleItem{{Name: "Derma surau", PriceSen: 10000, Qty: 1}},
		}
		products := chipProductsForSale(sale)
		if len(products) != 2 {
			t.Fatalf("%s: products = %d, want the item plus the fee line", method, len(products))
		}
		fee := products[len(products)-1]
		if fee.Name != PlatformFeeLineName {
			problems = append(problems, "purchase line ("+method+"): name is "+fee.Name+", want "+PlatformFeeLineName)
		}
		problems = append(problems, feeLineProblems("purchase line ("+method+")", fee.Name)...)
	}

	// The PDF writes its own label table, not the receipt's.
	problems = append(problems, feeLineProblems("PDF label (ms)", receiptLabelFee.ms())...)
	problems = append(problems, feeLineProblems("PDF label (en)", receiptLabelFee.en())...)

	if len(problems) > 0 {
		t.Fatalf("a buyer-facing fee line leaks:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestTheFeeLineDetectorCanSeeALeak is the BITE. The assertions above are only
// worth something if the detector can see the exact shape the bug had — the
// method name carried into the line by the rate formatter — and the withdrawn
// label. A detector that matches nothing is green on any tree.
func TestTheFeeLineDetectorCanSeeALeak(t *testing.T) {
	planted := []string{
		"Processing fee — FPX 1.00%", // the rejected shape, verbatim
		"Platform fee — FPX RM 1.00",
		"Caj pemprosesan — DuitNow QR",
		"Card RM1.00",
		"Platform fee RM1.00 (GrabPay)",
	}
	for _, s := range planted {
		if len(feeLineProblems("planted", s)) == 0 {
			t.Errorf("the detector lets a leaked buyer fee line through: %q", s)
		}
	}
	// And it must not flag the correct line, or every future edit fails for the
	// wrong reason.
	if p := feeLineProblems("correct", "Platform fee: RM 1.00"); len(p) != 0 {
		t.Errorf("the detector flags the correct buyer line: %v", p)
	}
	if got, want := (SaleRef{FeeSen: 100}).FeeLine(), "Platform fee: RM 1.00"; got != want {
		t.Errorf("FeeLine() = %q, want %q", got, want)
	}
}
