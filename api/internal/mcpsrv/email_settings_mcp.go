// MCP parity tools: per-workspace email template + behaviour settings (issue #107).
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getEmailSettingsIn resolves the workspace whose email settings to read.
type getEmailSettingsIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

// getEmailSettings reads a workspace's receipt/notification email settings.
func (s *Server) getEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in getEmailSettingsIn,
) (*mcp.CallToolResult, *dbstoreEmailSettingsOut, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.EmailSettings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	return nil, &dbstoreEmailSettingsOut{
		WorkspaceID:             st.WorkspaceID,
		ReceiptSubject:      st.ReceiptSubject,
		ReceiptBody:         st.ReceiptBody,
		ReceiptEnabled:      st.ReceiptEnabled,
		NotifyMerchant:      st.NotifyMerchant,
		MerchantNotifyEmail: st.MerchantNotifyEmail,
		HTML:                st.HTML,
	}, nil
}

// dbstoreEmailSettingsOut mirrors dbstore.EmailSettings for the MCP response.
type dbstoreEmailSettingsOut struct {
	WorkspaceID             string `json:"workspace_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// setEmailSettingsIn updates a workspace's email settings. Omitted fields keep
// their current value (read-modify-write, matching set_workspace_settings).
type setEmailSettingsIn struct {
	WorkspaceID             string  `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	ReceiptSubject      *string `json:"receipt_subject,omitempty" jsonschema:"Subjek resit (placeholder {{ref_code}} dll; omit = kekal)"`
	ReceiptBody         *string `json:"receipt_body,omitempty" jsonschema:"Badan resit (placeholder {{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}; omit = kekal)"`
	ReceiptEnabled      *bool   `json:"receipt_enabled,omitempty" jsonschema:"Hantar resit kepada pembeli (true/false; omit = kekal)"`
	NotifyMerchant      *bool   `json:"notify_merchant,omitempty" jsonschema:"Hantar salinan/notifikasi jualan kepada merchant (true/false; omit = kekal)"`
	MerchantNotifyEmail *string `json:"merchant_notify_email,omitempty" jsonschema:"Emel merchant untuk notifikasi (kosong = guna emel akaun; omit = kekal)"`
	HTML                *bool   `json:"html,omitempty" jsonschema:"Badan HTML (auto-escape, tiada XSS; true/false; omit = kekal)"`
}

func (s *Server) setEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setEmailSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	// Read-modify-write: omit a field = keep its current value.
	st, err := sp.EmailSettings(ctx, in.WorkspaceID)
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
	if err := sp.SaveEmailSettings(ctx, st); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}
