package mcpsrv

// t_71c67471 — MCP parity for the sale currency.
//
// The REST sale row now carries `currency` (the currency the sale was RECORDED
// in). `list_sales` is the same read surface for an agent, and AGENTS.md's
// parity rule says the two must not tell different stories: an agent that sees
// total_sen 2490 with no currency has to guess, and the guess that shipped in
// the dashboard was "RM". These are pure mapping tests — no DB, no server.

import (
	"encoding/json"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestListSalesRowCarriesTheSalesOwnCurrency(t *testing.T) {
	out := saleOutFromSale(&dbstore.Sale{
		RefCode: "JF-usd-mcp", Status: dbstore.SalePaid, TotalSen: 2490, Currency: "USD",
	})
	if out.Currency != "USD" {
		t.Fatalf("list_sales currency = %q, want USD", out.Currency)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal sale out: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("decode sale out: %v", err)
	}
	if got := wire["currency"]; got != "USD" {
		t.Fatalf("list_sales JSON currency = %v, want \"USD\" (payload: %s)", got, body)
	}
}

func TestListSalesRowCurrencyFallsBackToMYR(t *testing.T) {
	// Historical rows (pre-#185) have no currency recorded; MYR is what they were
	// charged in, and an agent must get a label rather than an empty field.
	out := saleOutFromSale(&dbstore.Sale{RefCode: "JF-old-mcp", TotalSen: 2490})
	if out.Currency != "MYR" {
		t.Fatalf("list_sales currency = %q, want the MYR fallback", out.Currency)
	}
}
