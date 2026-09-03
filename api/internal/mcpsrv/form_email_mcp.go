// MCP parity tools: per-form notification email settings (issue #177).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getFormEmailSettingsIn is the input for get_form_email_settings.
type getFormEmailSettingsIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// formEmailSettingsOut mirrors dbstore.FormEmailSettings for the MCP response.
type formEmailSettingsOut struct {
	FormID              string `json:"form_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// getFormEmailSettings reads a form's per-form notification email settings.
func (s *Server) getFormEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormEmailSettingsIn,
) (*mcp.CallToolResult, formEmailSettingsOut, error) {
	acc, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, formEmailSettingsOut{}, err
	}
	if s.deps.Forms == nil {
		return nil, formEmailSettingsOut{}, errors.New("forms tidak tersedia")
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, formEmailSettingsOut{}, errors.New("borang tidak dijumpai")
	}
	st, err := s.deps.Forms.FormEmailSettings(ctx, in.FormID)
	if err != nil {
		return nil, formEmailSettingsOut{}, err
	}
	return nil, formEmailSettingsOut{
		FormID: st.FormID, ReceiptSubject: st.ReceiptSubject, ReceiptBody: st.ReceiptBody,
		ReceiptEnabled: st.ReceiptEnabled, NotifyMerchant: st.NotifyMerchant,
		MerchantNotifyEmail: st.MerchantNotifyEmail, HTML: st.HTML,
	}, nil
}

// setFormEmailSettingsIn updates a form's email settings. Omitted fields keep
// their current value (read-modify-write, matching set_email_settings).
type setFormEmailSettingsIn struct {
	FormID              string  `json:"form_id" jsonschema:"ID borang"`
	ReceiptSubject      *string `json:"receipt_subject,omitempty" jsonschema:"Subjek resit (kosong = guna lalai space; omit = kekal)"`
	ReceiptBody         *string `json:"receipt_body,omitempty" jsonschema:"Badan resit (kosong = guna lalai space; omit = kekal)"`
	ReceiptEnabled      *bool   `json:"receipt_enabled,omitempty" jsonschema:"Hantar resit kepada pembeli (true/false; omit = kekal)"`
	NotifyMerchant      *bool   `json:"notify_merchant,omitempty" jsonschema:"Hantar salinan/notifikasi jualan kepada merchant (true/false; omit = kekal)"`
	MerchantNotifyEmail *string `json:"merchant_notify_email,omitempty" jsonschema:"Emel merchant untuk notifikasi (kosong = guna emel akaun; omit = kekal)"`
	HTML                *bool   `json:"html,omitempty" jsonschema:"Badan HTML (auto-escape, tiada XSS; true/false; omit = kekal)"`
}

func (s *Server) setFormEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormEmailSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	// Read-modify-write: omit a field = keep its current value.
	st, err := s.deps.Forms.FormEmailSettings(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if in.ReceiptSubject != nil {
		st.ReceiptSubject = *in.ReceiptSubject
	}
	if in.ReceiptBody != nil {
		st.ReceiptBody = *in.ReceiptBody
	}
	if in.ReceiptEnabled != nil {
		st.ReceiptEnabled = *in.ReceiptEnabled
	}
	if in.NotifyMerchant != nil {
		st.NotifyMerchant = *in.NotifyMerchant
	}
	if in.MerchantNotifyEmail != nil {
		st.MerchantNotifyEmail = *in.MerchantNotifyEmail
	}
	if in.HTML != nil {
		st.HTML = *in.HTML
	}
	if err := s.deps.Forms.SaveFormEmailSettings(ctx, st); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}
