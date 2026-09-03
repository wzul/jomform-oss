// MCP parity tools: per-space hosted legal pages (issue #184).
// generate_legal_page, get_legal_page, set_legal_page mirror the dashboard
// Legal settings tab (AGENTS.md MCP parity rule).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/llm"
)

// legalPageOut is the MCP response shape for one legal page.
type legalPageOut struct {
	SpaceID  string `json:"space_id"`
	PageType string `json:"page_type"`
	Mode     string `json:"mode"`
	Content  string `json:"content"`
}

// validLegalPageType reports whether a page type is one of the three hosted
// legal pages.
func validLegalPageType(t string) bool {
	return t == dbstore.LegalPagePrivacy || t == dbstore.LegalPageTerms || t == dbstore.LegalPageRefund
}

// getLegalPageIn reads one legal page for a space.
type getLegalPageIn struct {
	SpaceID  string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	PageType string `json:"page_type" jsonschema:"Jenis halaman: privacy|terms|refund"`
}

// getLegalPage returns one legal page's content + mode for a space.
func (s *Server) getLegalPage(
	ctx context.Context, _ *mcp.CallToolRequest, in getLegalPageIn,
) (*mcp.CallToolResult, *legalPageOut, error) {
	if _, _, err := s.accessibleSpace(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	if !validLegalPageType(in.PageType) {
		return nil, nil, errors.New("page_type mesti privacy|terms|refund")
	}
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	page, err := sp.LegalPage(ctx, in.SpaceID, in.PageType)
	if err != nil {
		if errors.Is(err, dbstore.ErrLegalPageNotFound) {
			return nil, nil, errors.New("halaman belum ditetapkan")
		}
		return nil, nil, err
	}
	return nil, &legalPageOut{
		SpaceID: page.SpaceID, PageType: page.PageType, Mode: page.Mode, Content: page.Content,
	}, nil
}

// generateLegalPageIn carries the merchant's business details for the prompt
// template.
type generateLegalPageIn struct {
	SpaceID        string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	PageType       string `json:"page_type" jsonschema:"Jenis halaman: privacy|terms|refund"`
	BusinessName   string `json:"business_name,omitempty" jsonschema:"Nama perniagaan"`
	RegistrationNo string `json:"registration_no,omitempty" jsonschema:"Nombor pendaftaran"`
	Contact        string `json:"contact,omitempty" jsonschema:"Hubungan (emel/telefon)"`
	Address        string `json:"address,omitempty" jsonschema:"Alamat"`
	DataCollected  string `json:"data_collected,omitempty" jsonschema:"Data yang dikutip (privacy)"`
	PaymentTerms   string `json:"payment_terms,omitempty" jsonschema:"Terma pembayaran (terms)"`
	RefundTerms    string `json:"refund_terms,omitempty" jsonschema:"Terma refund (refund)"`
	Website        string `json:"website,omitempty" jsonschema:"Laman web"`
	EffectiveDate  string `json:"effective_date,omitempty" jsonschema:"Tarikh berkuat kuasa"`
}

// generateLegalPage drafts a legal page via the LLM (deepseek-v4-flash) with a
// prompt template filled with the merchant's business details. Returns the
// draft for review — it is NOT saved until set_legal_page is called.
func (s *Server) generateLegalPage(
	ctx context.Context, _ *mcp.CallToolRequest, in generateLegalPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	_, role, err := s.accessibleSpace(ctx, in.SpaceID)
	if err != nil {
		return nil, nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, nil, errors.New("perlu peranan manage atau admin")
	}
	if !validLegalPageType(in.PageType) {
		return nil, nil, errors.New("page_type mesti privacy|terms|refund")
	}
	if s.deps.LLM == nil {
		return nil, nil, errors.New("penjanaan LLM tidak tersedia")
	}
	content, err := s.deps.LLM.GenerateLegalPage(ctx, in.PageType, llm.BusinessDetails{
		BusinessName:   in.BusinessName,
		RegistrationNo: in.RegistrationNo,
		Contact:        in.Contact,
		Address:        in.Address,
		DataCollected:  in.DataCollected,
		PaymentTerms:   in.PaymentTerms,
		RefundTerms:    in.RefundTerms,
		Website:        in.Website,
		EffectiveDate:  in.EffectiveDate,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{
		"space_id": in.SpaceID, "page_type": in.PageType,
		"mode": dbstore.LegalModeAuto, "content": content,
	}, nil
}

// setLegalPageIn stores a legal page's content + mode.
type setLegalPageIn struct {
	SpaceID  string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	PageType string `json:"page_type" jsonschema:"Jenis halaman: privacy|terms|refund"`
	Content  string `json:"content" jsonschema:"Kandungan halaman (teks/HTML)"`
	Mode     string `json:"mode" jsonschema:"auto (dijana LLM) atau manual (ditulis sendiri)"`
}

// setLegalPage stores a legal page's content + mode for a space.
func (s *Server) setLegalPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setLegalPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	_, role, err := s.accessibleSpace(ctx, in.SpaceID)
	if err != nil {
		return nil, nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, nil, errors.New("perlu peranan manage atau admin")
	}
	if !validLegalPageType(in.PageType) {
		return nil, nil, errors.New("page_type mesti privacy|terms|refund")
	}
	if in.Mode != dbstore.LegalModeAuto && in.Mode != dbstore.LegalModeManual {
		return nil, nil, errors.New("mode mesti auto|manual")
	}
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sp.SaveLegalPage(ctx, &dbstore.LegalPage{
		SpaceID: in.SpaceID, PageType: in.PageType, Mode: in.Mode, Content: in.Content,
	}); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "space_id": in.SpaceID, "page_type": in.PageType}, nil
}
