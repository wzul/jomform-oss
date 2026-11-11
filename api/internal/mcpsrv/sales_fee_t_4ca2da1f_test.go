package mcpsrv

// t_4ca2da1f — parity with the dashboard's Sales page: an agent reading a sale
// must see the buyer-borne fee and what the merchant actually receives, not only
// total_sen (what the buyer paid).
//
// The MCP rule in AGENTS.md ("every dashboard feature has a matching MCP tool")
// is why this lives here: the REST row and the dashboard now show fee_sen and the
// net, so list_sales — the same sales the page renders — must carry the same two
// numbers or an agent will quote RM1,010.10 as income on a RM1,000.00 sale.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

func TestSaleOutCarriesFeeAndNet(t *testing.T) {
	sl := &dbstore.Sale{
		RefCode: "JF-FEE1", Status: dbstore.SalePaid,
		SubtotalSen: 100000, FeeSen: 1010, FeeMethod: payments.FeeMethodFPX,
		FeePercentBp: 100, TotalSen: 101010,
	}
	out := saleOutFromSale(sl)
	if out.FeeSen != 1010 {
		t.Fatalf("FeeSen = %d, want 1010", out.FeeSen)
	}
	if out.NetSen != 100000 {
		t.Fatalf("NetSen = %d, want 100000 (total 101010 - fee 1010)", out.NetSen)
	}
	if out.FeeRateLabel != "FPX 1.00%" {
		t.Fatalf("FeeRateLabel = %q, want %q", out.FeeRateLabel, "FPX 1.00%")
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal saleOut: %v", err)
	}
	for _, key := range []string{`"fee_sen":1010`, `"fee_rate_label":"FPX 1.00%"`, `"net_sen":100000`} {
		if !strings.Contains(string(body), key) {
			t.Errorf("list_sales row is missing %s: %s", key, body)
		}
	}
}

// A sale with no processing fee must not look like it lost one: the fee fields
// are absent and the net IS the total, because that is what the merchant receives.
func TestSaleOutWithoutAFeeKeepsNetEqualToTotal(t *testing.T) {
	out := saleOutFromSale(&dbstore.Sale{RefCode: "JF-PLAIN", Status: dbstore.SalePaid, SubtotalSen: 5000, TotalSen: 5000})
	if out.FeeSen != 0 || out.FeeRateLabel != "" {
		t.Fatalf("a no-fee sale grew a fee: fee_sen=%d label=%q", out.FeeSen, out.FeeRateLabel)
	}
	if out.NetSen != 5000 {
		t.Fatalf("NetSen = %d, want the total 5000 on a no-fee sale", out.NetSen)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal saleOut: %v", err)
	}
	if strings.Contains(string(body), "fee_rate_label") {
		t.Fatalf("a no-fee sale's JSON grew fee_rate_label: %s", body)
	}
}
