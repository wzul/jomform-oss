// MCP tools: per-form CHIP override + authorize/capture flow.
package mcpsrv

import (
	"context"
	"errors"

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
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil || s.decKey == nil {
		return nil, nil, errors.New("override tidak tersedia")
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
	acc, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Forms.ClearChipOverride(ctx, acc, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "override_cleared", "form_id": in.FormID}, nil
}

func (s *Server) captureSaleMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in saleRefIn,
) (*mcp.CallToolResult, any, error) {
	pay := s.pay
	if pay == nil {
		return nil, nil, errors.New("payments tidak aktif")
	}
	acc, err := s.accountAndScope(ctx, true)
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
	acc, err := s.accountAndScope(ctx, true)
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

var _ = payments.ErrNotConfigured
