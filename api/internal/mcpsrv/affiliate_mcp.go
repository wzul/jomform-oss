// Affiliate MCP tools (issue #444). A merchant creates affiliate links
// (?aff=CODE) for each affiliate; sales driven by an affiliate are attributed
// to that affiliate and commission is computed on paid sales.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// affiliatesStore returns the affiliate store (nil-safe).
func (s *Server) affiliatesStore() (*dbstore.AffiliateStore, error) {
	if s.deps == nil || s.deps.Affiliates == nil {
		return nil, errors.New("affiliate store tidak tersedia")
	}
	return s.deps.Affiliates, nil
}

// salesStore returns the sale store (nil-safe).
func (s *Server) salesStore() (*dbstore.SaleStore, error) {
	if s.deps == nil || s.deps.Sales == nil {
		return nil, errors.New("sale store tidak tersedia")
	}
	return s.deps.Sales, nil
}

// ── create_affiliate ────────────────────────────────────────────────────────────

type createAffiliateIn struct {
	WorkspaceID   string  `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Code          string  `json:"code" jsonschema:"Kod affiliate (cth: ALI, KAKAK)"`
	Name          string  `json:"name,omitempty" jsonschema:"Nama affiliate (pilihan)"`
	Email         string  `json:"email,omitempty" jsonschema:"Emel affiliate (pilihan)"`
	CommissionPct float64 `json:"commission_pct" jsonschema:"Kadar komisen %% (cth: 10 = 10%%)"`
}

type affiliateOut struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	CommissionPct float64 `json:"commission_pct"`
	ReferralURL   string  `json:"referral_url"`
}

func (s *Server) createAffiliate(
	ctx context.Context, _ *mcp.CallToolRequest, in createAffiliateIn,
) (*mcp.CallToolResult, *affiliateOut, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	// This also replaces the inline role comparison below, which checked the role
	// but never the scope.
	ws, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	affStore, err := s.affiliatesStore()
	if err != nil {
		return nil, nil, err
	}
	if existing, _ := affStore.ByCode(ctx, in.WorkspaceID, in.Code); existing != nil {
		return nil, nil, errors.New("code sudah wujud")
	}
	a := &dbstore.Affiliate{
		ID:            uuid.NewString(),
		WorkspaceID:   in.WorkspaceID,
		Code:          in.Code,
		Name:          in.Name,
		Email:         in.Email,
		CommissionPct: in.CommissionPct,
	}
	if err := affStore.Create(ctx, a); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, s.affiliateOut(ws.Slug, a), nil
}

// ── list_affiliates ──────────────────────────────────────────────────────────────

type listAffiliatesIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

func (s *Server) listAffiliates(
	ctx context.Context, _ *mcp.CallToolRequest, in listAffiliatesIn,
) (*mcp.CallToolResult, *listOutput[affiliateOut], error) {
	ws, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	affStore, err := s.affiliatesStore()
	if err != nil {
		return nil, nil, err
	}
	affs, err := affStore.ByWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]affiliateOut, 0, len(affs))
	for _, a := range affs {
		out = append(out, *s.affiliateOut(ws.Slug, a))
	}
	return nil, newListOut(out), nil
}

// ── get_affiliate_sales ─────────────────────────────────────────────────────────

type getAffiliateSalesIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	AffiliateID string `json:"affiliate_id" jsonschema:"ID affiliate (dari list_affiliates)"`
}

type affiliateSaleOut struct {
	RefCode   string `json:"ref_code"`
	Status    string `json:"status"`
	TotalSen  int64  `json:"total_sen"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) getAffiliateSales(
	ctx context.Context, _ *mcp.CallToolRequest, in getAffiliateSalesIn,
) (*mcp.CallToolResult, *listOutput[affiliateSaleOut], error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	affStore, err := s.affiliatesStore()
	if err != nil {
		return nil, nil, err
	}
	aff, err := affStore.ByID(ctx, in.AffiliateID)
	if err != nil || aff == nil || aff.WorkspaceID != in.WorkspaceID {
		return nil, nil, errors.New("affiliate tidak dijumpai")
	}
	salesStore, err := s.salesStore()
	if err != nil {
		return nil, nil, err
	}
	sales, err := salesStore.ByAffiliateCode(ctx, in.WorkspaceID, aff.Code)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]affiliateSaleOut, 0, len(sales))
	for _, sl := range sales {
		out = append(out, affiliateSaleOut{
			RefCode: sl.RefCode, Status: sl.Status, TotalSen: sl.TotalSen,
			Currency: sl.Currency, CreatedAt: sl.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return nil, newListOut(out), nil
}

// ── set_affiliate_commission ─────────────────────────────────────────────────────

type setAffiliateCommissionIn struct {
	WorkspaceID   string  `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	AffiliateID   string  `json:"affiliate_id" jsonschema:"ID affiliate (dari list_affiliates)"`
	CommissionPct float64 `json:"commission_pct" jsonschema:"Kadar komisen %% (cth: 10 = 10%%)"`
}

// setAffiliateCommission sets an affiliate's commission rate (issue #444).
//
// Issue #519: the affiliate id is checked against the workspace the caller was
// authorised against before anything is written — the same rule
// get_affiliate_sales applies. A mismatch reports the same not-found error as
// an unknown id, so a caller cannot tell whether an id exists elsewhere.
func (s *Server) setAffiliateCommission(
	ctx context.Context, _ *mcp.CallToolRequest, in setAffiliateCommissionIn,
) (*mcp.CallToolResult, map[string]bool, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	affStore, err := s.affiliatesStore()
	if err != nil {
		return nil, nil, err
	}
	// Scope check (issue #519): the affiliate must belong to in.WorkspaceID.
	aff, err := affStore.ByID(ctx, in.AffiliateID)
	if err != nil || aff == nil || aff.WorkspaceID != in.WorkspaceID {
		return nil, nil, errors.New("affiliate tidak dijumpai")
	}
	// Scoped store write (issue #519): the UPDATE itself is bound to the
	// authorised workspace, so a future caller cannot reintroduce the bug.
	if err := affStore.SetCommission(ctx, in.WorkspaceID, in.AffiliateID, in.CommissionPct); err != nil {
		if errors.Is(err, dbstore.ErrAffiliateNotFound) {
			return nil, nil, errors.New("affiliate tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]bool{"ok": true}, nil
}

// affiliateOut builds the output view + referral URL.
func (s *Server) affiliateOut(slug string, a *dbstore.Affiliate) *affiliateOut {
	return &affiliateOut{
		ID: a.ID, WorkspaceID: a.WorkspaceID, Code: a.Code, Name: a.Name,
		Email: a.Email, CommissionPct: a.CommissionPct,
		ReferralURL: "https://" + slug + ".jomform.com/?aff=" + a.Code,
	}
}
