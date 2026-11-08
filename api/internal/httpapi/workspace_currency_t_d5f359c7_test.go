package httpapi

// Card t_d5f359c7 — seven surfaces printed a literal "RM" over an amount
// denominated in the WORKSPACE's currency.
//
// THE DEFECT: /products, /products (editor), /subscriptions, /spam, /builder,
// /form-settings and the menu ProductPicker each carried a local
//
//     const money = (sen) => `RM${(sen / 100).toLocaleString(...)}`
//
// while a workspace can be set to USD/SGD/EUR (Settings → Currency, issue #185).
// The pages could not have done better: nothing in their payloads said what the
// workspace charges in, so the fix is one wire change plus the shared formatter.
//
// WHAT THESE GUARDS COVER
//   1. GET /v1/rest/dashboard-bundle carries the ACTIVE workspace's `currency`,
//      and follows a currency switch — that field is what every price surface
//      labels with (web/src/components/session.tsx → lib/money.ts formatPrice).
//   2. The spam queue's sale rows carry the currency each sale was RECORDED in.
//      /spam is not a price surface: its only amount is a recorded sale total,
//      so the row's own currency wins and a workspace that switches currency
//      never relabels its own history.
//
// WHAT IT DOES NOT COVER: the rendered pixels. The web unit suite pins the
// wiring (web/src/lib/workspacePrice.test.ts) and the handoff's browser check
// pins the paint against a USD workspace.

import (
	"context"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestBundleCarriesTheWorkspaceCurrency is the card's first half: the price
// surfaces need ONE field that names the money they charge in.
func TestBundleCarriesTheWorkspaceCurrency(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, _ := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)

	// A fresh workspace defaults to MYR, and the bundle must say so rather than
	// leaving the client to assume anything.
	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status = %d, want 200", code)
	}
	if got := body["currency"]; got != "MYR" {
		t.Fatalf("fresh bundle currency = %v, want MYR — the client would render no price at all", got)
	}

	// Switch the workspace to USD exactly as Settings → Currency does.
	var workspaceID string
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, accountID).Scan(&workspaceID); err != nil {
		t.Fatalf("read account workspace: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read workspace settings: %v", err)
	}
	st.Currency = "USD"
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save workspace currency: %v", err)
	}

	code, body = bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("bundle status after switch = %d, want 200", code)
	}
	if got := body["currency"]; got != "USD" {
		t.Errorf("bundle currency = %v, want USD — a US$12.00 product would still be labelled RM", got)
	}
	// summary.currency is the client's fallback for a payload without the
	// bundle-level field; the two must name the same money.
	if sum, _ := body["summary"].(map[string]any); sum != nil {
		if got := sum["currency"]; got != "USD" {
			t.Errorf("summary.currency = %v, want USD — the fallback label drifted from the bundle's", got)
		}
	} else {
		t.Errorf("bundle carried no summary: %v", body)
	}
}

// TestSpamSalesCarryTheSaleCurrency is the card's /spam site, and the reason it
// is NOT labelled with the workspace currency: the spam queue lists recorded
// sales, so each row keeps the money it was charged in.
func TestSpamSalesCarryTheSaleCurrency(t *testing.T) {
	ctx := context.Background()
	h, pool, accountID, formID := salesRowFixture(t, "payment")
	raw := bundleAPIKey(t, ctx, pool, accountID)
	suffix := accountID[:8]

	seed := func(ref, currency string, amount int64) {
		sale := &dbstore.Sale{
			AccountID: accountID, FormID: formID, RefCode: ref,
			Client:      dbstore.SaleClient{FullName: "Spam buyer " + ref, Email: ref + "@example.test"},
			Items:       []dbstore.SaleItem{},
			SubtotalSen: amount, TotalSen: amount, Currency: currency,
			SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SalePaid,
		}
		if err := h.Sales.Create(ctx, sale); err != nil {
			t.Fatalf("create sale %s: %v", ref, err)
		}
		// Create() does not write the spam verdict (it is set by the background
		// judge), so the fixture flips it here.
		if _, err := pool.Exec(ctx,
			`UPDATE sales SET is_spam=true, spam_reason='t_d5f359c7 fixture' WHERE ref_code=$1`, ref); err != nil {
			t.Fatalf("flag sale %s as spam: %v", ref, err)
		}
	}
	seed("JF-SPAM-USD-"+suffix, "USD", 2490)
	seed("JF-SPAM-MYR-"+suffix, "MYR", 1000)

	code, body := bundleGet(t, h, raw, "/v1/rest/spam/sales")
	if code != http.StatusOK {
		t.Fatalf("spam sales status = %d, want 200 (body %v)", code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("spam sales items = %d, want the 2 flagged rows", len(items))
	}
	labels := map[string]bool{}
	for _, it := range items {
		row, _ := it.(map[string]any)
		ref, _ := row["ref_code"].(string)
		cur, _ := row["currency"].(string)
		if cur == "" {
			t.Errorf("spam row %s carries no currency — the page can only print a hardcoded symbol", ref)
			continue
		}
		labels[ref+"="+cur] = true
	}
	for _, want := range []string{"JF-SPAM-USD-" + suffix + "=USD", "JF-SPAM-MYR-" + suffix + "=MYR"} {
		if !labels[want] {
			t.Errorf("spam rows = %v, want %s — each sale must keep the currency it was recorded in", labels, want)
		}
	}
}
