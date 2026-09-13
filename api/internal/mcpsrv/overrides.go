// MCP tools: per-form CHIP override + authorize/capture flow.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

type formOverrideIn struct {
	FormID          string   `json:"form_id" jsonschema:"ID borang"`
	BrandID         string   `json:"brand_id" jsonschema:"CHIP Brand ID untuk borang ini"`
	SecretKey       string   `json:"secret_key" jsonschema:"CHIP Secret Key (akan dienkripsi)"`
	EmailFallback   string   `json:"email_fallback,omitempty" jsonschema:"Fallback emel bila buyer tak bagi emel (one-time saja)"`
	MethodWhitelist []string `json:"payment_method_whitelist,omitempty" jsonschema:"Senarai kaedah bayaran, kosong = semua"`
}

type formRefIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

type saleRefIn struct {
	RefCode   string `json:"ref_code" jsonschema:"Kod rujukan jualan (JF-...)"`
	AmountSen *int64 `json:"amount_sen,omitempty" jsonschema:"Optional: jumlah penuh dalam sen untuk partial capture"`
}

func (s *Server) setFormChipOverride(
	ctx context.Context, _ *mcp.CallToolRequest, in formOverrideIn,
) (*mcp.CallToolResult, any, error) {
	if s.deps.Forms == nil || s.decKey == nil {
		return nil, nil, errors.New("override tidak tersedia")
	}
	// Issue #307: resolve + RBAC-gate the owning account (CHIP creds are admin-only).
	acc, err := s.formForAdmin(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	enc, err := cryptoutil.Encrypt(s.decKey, []byte(in.SecretKey))
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.SetChipOverride(ctx, acc, in.FormID, in.BrandID, enc, in.EmailFallback, in.MethodWhitelist); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "override_set", "form_id": in.FormID}, nil
}

func (s *Server) clearFormChipOverride(
	ctx context.Context, _ *mcp.CallToolRequest, in formRefIn,
) (*mcp.CallToolResult, any, error) {
	acc, err := s.formForAdmin(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.ClearChipOverride(ctx, acc, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "override_cleared", "form_id": in.FormID}, nil
}

// formForAdmin resolves the account owning a form and enforces the admin gate
// for that form's workspace (CHIP credentials - admin only, issue #162).
func (s *Server) formForAdmin(ctx context.Context, formID string) (string, error) {
	acc, err := s.resolveFormAccount(ctx, formID)
	if err != nil {
		return "", err
	}
	if err := s.requireAdminRoleIn(ctx, acc); err != nil {
		return "", err
	}
	return acc, nil
}

func (s *Server) captureSaleMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in saleRefIn,
) (*mcp.CallToolResult, any, error) {
	pay := s.pay
	if pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, errors.New("jualan tidak dijumpai")
	}
	if err := pay.CaptureSale(ctx, sale, in.AmountSen); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Sales.MarkCaptured(ctx, sale.ID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "captured", "ref_code": in.RefCode}, nil
}

func (s *Server) releaseSaleMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in saleRefIn,
) (*mcp.CallToolResult, any, error) {
	pay := s.pay
	if pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, errors.New("jualan tidak dijumpai")
	}
	if err := pay.ReleaseSale(ctx, sale); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Sales.SetStatus(ctx, sale.ID, dbstore.SaleCancelled); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "released", "ref_code": in.RefCode}, nil
}

// refundSaleMCP issues a full or partial refund against a paid sale (issue
// #407, extended by #518). amount_sen nil = a full refund of the OUTSTANDING
// balance; non-nil = a partial refund. The amount is validated against the
// sale's remaining balance before CHIP is called, and the refunded total is
// recorded on the sale: only a refund that clears the whole total marks the
// sale `refunded` — a partial leaves it reading as partially refunded.
func (s *Server) refundSaleMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in saleRefIn,
) (*mcp.CallToolResult, any, error) {
	pay := s.pay
	if pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	acc, err := s.requireSaleWrite(ctx, in.RefCode)
	if err != nil {
		return nil, nil, err
	}
	sale, err := s.deps.Sales.ByRefCode(ctx, in.RefCode)
	if err != nil || sale.AccountID != acc {
		return nil, nil, errors.New("jualan tidak dijumpai")
	}
	res, err := pay.RefundSale(ctx, sale, in.AmountSen)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{
		"status": res.Status, "ref_code": in.RefCode,
		"amount_sen": res.AmountSen, "refunded_sen": res.RefundedSen,
		"outstanding_sen": res.OutstandingSen, "fully_refunded": res.FullyRefunded,
		"refund_status": saleRefundStatus(sale.TotalSen, res.RefundedSen),
	}, nil
}

// saleRefundStatus derives the partial/full refund label from the sale total +
// the recorded refunded amount (issue #518), mirroring
// dbstore.Sale.RefundStatus() for callers holding the two numbers.
func saleRefundStatus(totalSen, refundedSen int64) string {
	switch {
	case refundedSen <= 0:
		return dbstore.RefundNone
	case refundedSen >= totalSen:
		return dbstore.RefundFull
	default:
		return dbstore.RefundPartial
	}
}

// captureAllPendingMCP captures every awaiting skip_capture authorization in
// one call (issue #38, the #12 addendum). One CHIP rejection never aborts the
// batch; every ref gets its own result. Issue #307: spans all the user's workspaces.
func (s *Server) captureAllPendingMCP(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, any, error) {
	pay := s.pay
	if pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	accs, err := s.accountScopes(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	var sales []*dbstore.Sale
	for _, acc := range accs {
		if err := s.requireWriteRoleIn(ctx, acc); err != nil {
			return nil, nil, err
		}
		one, err := s.deps.Sales.ListAwaitingCapture(ctx, acc)
		if err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		sales = append(sales, one...)
	}
	type refResult struct {
		RefCode string `json:"ref_code"`
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]refResult, 0, len(sales))
	ok, failed := 0, 0
	for _, sale := range sales {
		res := refResult{RefCode: sale.RefCode}
		if err := pay.CaptureSale(ctx, sale, nil); err != nil {
			res.Error = err.Error()
		} else if err := s.deps.Sales.MarkCaptured(ctx, sale.ID); err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
			ok++
		}
		if !res.OK {
			failed++
		}
		results = append(results, res)
	}
	return nil, map[string]any{
		"requested": len(sales), "captured": ok, "failed": failed, "results": results,
	}, nil
}

var _ = payments.ErrNotConfigured
