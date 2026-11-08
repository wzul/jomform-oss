package payments

// Card t_129664c9: the receipt PDF, its attachment name and the two CHIP
// checkout line-item titles are DISPLAY text rather than wire refusals, and each
// of them was still Malay-only. These tests pin the wording that was chosen, so a
// later edit cannot drop one half of the bilingual receipt, put the "resit-"
// attachment prefix back, or re-introduce a Malay line item on CHIP's hosted
// page (CHIP renders the title exactly as sent, so no buyer-side locale can ever
// correct it afterwards).
//
// The tests read the RENDERED artifacts where they can — the PDF's own bytes and
// the JSON body we actually POST to CHIP — not just the helpers that feed them.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// receiptFullRef renders EVERY grid row (fee, e-Invoice TIN, MSIC), so a label
// that goes missing from the layout is caught here instead of by a reader
// skimming a page.
func receiptFullRef() SaleRef {
	return SaleRef{
		RefCode:      "JF-ABC123",
		AmountSen:    12345,
		ItemsSummary: "1× Tee",
		FormURL:      "https://acme.jomform.com/tee",
		FeeSen:       210,
		FeeMethod:    "card",
		FeePercentBp: 200,
		TIN:          "C2584563200",
		MSIC:         "62010",
	}
}

// sameInBothLanguages are the labels that are legitimately printed once: the
// word is the same in Bahasa Malaysia and English ("Item", "Status") or is a
// statutory acronym pair. Everything else on the grid must carry both halves.
var sameInBothLanguages = map[string]bool{
	"Item":                 true,
	"Status":               true,
	"e-Invoice TIN (LHDN)": true,
	"MSIC":                 true,
}

func TestReceiptPDFLabelsAreBilingual(t *testing.T) {
	rows := receiptDetailRows(receiptFullRef())

	want := []string{
		"Rujukan / Reference",
		"Tarikh / Date",
		"Item",
		"Caj pemprosesan / Processing fee",
		"Jumlah / Amount",
		"e-Invoice TIN (LHDN)",
		"MSIC",
		"Status",
	}
	if len(rows) != len(want) {
		t.Fatalf("receipt grid has %d rows, want %d — the fixture must render every row: %v",
			len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i][0] != w {
			t.Errorf("grid row %d label = %q, want %q — the receipt prints "+
				"\"<Bahasa Malaysia> / <English>\" on every label (card t_129664c9)",
				i, rows[i][0], w)
		}
	}

	// The rule itself, so a label added later cannot be single-language: either
	// it is one of the pairs that reads the same in both, or it shows both.
	for _, r := range rows {
		if sameInBothLanguages[r[0]] {
			continue
		}
		if !strings.Contains(r[0], " / ") {
			t.Errorf("grid label %q is single-language; every label that is not the same "+
				"word in both languages must read \"<Bahasa Malaysia> / <English>\"", r[0])
		}
	}

	// The status stamp was the one Malay-only CELL: a buyer who does not read
	// Malay could not tell from it that the payment went through.
	if got := rows[len(rows)-1][1]; got != "Pembayaran diterima / PAID" {
		t.Errorf("status stamp = %q, want %q", got, "Pembayaran diterima / PAID")
	}
}

func TestReceiptPDFBytesCarryBothLanguages(t *testing.T) {
	pdf, err := (&Service{}).ReceiptPDF(receiptFullRef())
	if err != nil {
		t.Fatalf("ReceiptPDF: %v", err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-") {
		t.Fatalf("not a PDF: %q", string(pdf[:8]))
	}
	// Maroto writes its content streams uncompressed, so the rendered strings are
	// readable in the bytes: this asserts the document a buyer RECEIVES, not the
	// table that feeds it. The needles are complete lines, so a cell that goes
	// back to one language (the stamp and the footer were Malay-only) fails here
	// even though both of its words still appear somewhere in the file.
	body := string(pdf)
	for _, want := range []string{
		"Resit Pembayaran / Payment Receipt",
		"Rujukan / Reference",
		"Tarikh / Date",
		"Caj pemprosesan / Processing fee",
		"Jumlah / Amount",
		"Pembayaran diterima / PAID",
		"Terima kasih! Simpan dokumen ini sebagai resit rasmi.",
		"Thank you! Keep this document as your official receipt.",
		"https://acme.jomform.com/tee",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("receipt PDF does not contain %q — one language of the receipt went missing (card t_129664c9)", want)
		}
	}
}

func TestReceiptAttachmentIsNamedInEnglish(t *testing.T) {
	if got, want := receiptAttachmentName("JF-ABC123"), "receipt-JF-ABC123.pdf"; got != want {
		t.Fatalf("receipt attachment name = %q, want %q — the file name is the part of the "+
			"receipt a buyer sees before opening it (card t_129664c9)", got, want)
	}
}

// capturingChipServer is a CHIP stub that records the line-item title of every
// purchase it is asked to create, in the order the calls arrive.
type capturingChipServer struct {
	mu     sync.Mutex
	titles []string
}

func (c *capturingChipServer) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.titles...)
}

func newCapturingChipServer(t *testing.T, purchaseID string) (*chip.Client, *capturingChipServer) {
	t.Helper()
	c := &capturingChipServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /purchases/", func(w http.ResponseWriter, r *http.Request) {
		var req chip.PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode create-purchase body: %v", err)
		}
		c.mu.Lock()
		if len(req.Purchase.Products) > 0 {
			c.titles = append(c.titles, req.Purchase.Products[0].Name)
		}
		c.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": purchaseID, "status": "created"})
	})
	mux.HandleFunc("POST /purchases/"+purchaseID+"/charge/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": purchaseID, "status": "pending_charge"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return chip.New(srv.URL), c
}

// TestChipCheckoutLineItemTitlesAreEnglish asserts the title CHIP actually
// receives, because that is the only place it can be read: CHIP's hosted checkout
// renders it as sent and never tells us the buyer's language, so one wording
// serves every buyer and it cannot be fixed later.
func TestChipCheckoutLineItemTitlesAreEnglish(t *testing.T) {
	ctx := context.Background()
	pool := recurTestPool(t)
	accountID, productID, subID := seedRecurringFixture(t, ctx, pool, nil)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	client, capture := newCapturingChipServer(t, fmt.Sprintf("pur_titles_%d", time.Now().UnixNano()))
	svc := &Service{
		Chip:       client,
		Accounts:   dbstore.NewAccountStore(pool),
		Subs:       dbstore.NewSubscriptionStore(pool),
		Products:   dbstore.NewProductStore(pool),
		DecryptKey: key,
		BaseURL:    "https://app.example",
	}

	sub, err := svc.Subs.ByIDForAccount(ctx, accountID, subID)
	if err != nil {
		t.Fatalf("load sub: %v", err)
	}
	if err := svc.chargeSubscription(ctx, sub); err != nil {
		t.Fatalf("charge subscription: %v", err)
	}
	if _, err := svc.CreateUpdateCardPurchase(ctx, sub); err != nil {
		t.Fatalf("create update-card purchase: %v", err)
	}

	want := []string{
		"Subscription #" + productID,               // recurring charge
		"Card update — Subscription #" + productID, // card re-tokenize ($0 authorize)
	}
	got := capture.all()
	if len(got) != len(want) {
		t.Fatalf("CHIP received %d purchase(s), want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CHIP line item %d = %q, want %q — the title is rendered on CHIP's "+
				"hosted page as sent, so it must be one language for every buyer (card t_129664c9)",
				i, got[i], want[i])
		}
	}
}
