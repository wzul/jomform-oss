package mcpsrv

// MCP parity for the sales lifecycle table (AGENTS.md "MCP FEATURE PARITY"):
// `confirm_sale` goes through the same table as the REST door, so an agent
// cannot set a state the dashboard would refuse.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"errors"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
)

func TestConfirmSaleMCPFollowsTheLifecycleTable(t *testing.T) {
	st := issue518MCPFixture(t, 5000)

	// A settled CHIP payment confirms.
	if _, out, err := st.srv.confirmSale(st.ctx, nil, confirmSaleIn{RefCode: st.refCode}); err != nil {
		t.Fatalf("confirm_sale on a paid sale: %v", err)
	} else if out["status"] != "confirmed" {
		t.Errorf("confirm_sale status = %q, want confirmed", out["status"])
	}
	sale, err := st.srv.deps.Sales.ByID(st.ctx, st.saleID)
	if err != nil {
		t.Fatalf("read sale: %v", err)
	}
	if sale.Status != dbstore.SaleConfirmed {
		t.Errorf("sale status = %q, want confirmed", sale.Status)
	}

	// A cancelled sale is terminal: the tool refuses with the table's typed
	// error instead of writing `confirmed` over it.
	if err := st.srv.deps.Sales.SetStatus(st.ctx, st.saleID, dbstore.SaleCancelled); err != nil {
		t.Fatalf("reset status: %v", err)
	}
	if _, _, err := st.srv.confirmSale(st.ctx, nil, confirmSaleIn{RefCode: st.refCode}); !errors.Is(err, lifecycle.ErrIllegalTransition) {
		t.Errorf("confirm_sale on a cancelled sale = %v, want ErrIllegalTransition", err)
	}
	if sale, err := st.srv.deps.Sales.ByID(st.ctx, st.saleID); err != nil {
		t.Fatalf("re-read sale: %v", err)
	} else if sale.Status != dbstore.SaleCancelled {
		t.Errorf("a refused confirm_sale changed the sale to %q", sale.Status)
	}
}
