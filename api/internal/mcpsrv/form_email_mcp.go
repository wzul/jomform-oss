// MCP parity tools: per-form notification email settings (issue #177).
//
// Issue #453: the per-form settings override the workspace email settings
// (#107) field-by-field, resolving per-form -> workspace -> default. The
// boolean toggles are tri-state so a form can turn receipts OFF or merchant
// notifications ON without touching the subject/body; a nil pointer means
// "inherit the workspace default".
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// getFormEmailSettingsIn is the input for get_form_email_settings.
type getFormEmailSettingsIn struct {
	FormID string `json:"form_id" jsonschema:"Form ID"`
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

	// Issue #453 tri-state overrides (nil = inherit the workspace default).
	ReceiptEnabledOverride *bool `json:"receipt_enabled_override,omitempty"`
	NotifyMerchantOverride *bool `json:"notify_merchant_override,omitempty"`
	HTMLOverride           *bool `json:"html_override,omitempty"`
}

// formEmailSettingsResolved is the effective (resolved) settings actually used
// when sending, plus the workspace settings the form inherits from.
type formEmailSettingsResolved struct {
	FormID              string `json:"form_id,omitempty"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// getFormEmailSettingsOut is the full read response.
type getFormEmailSettingsOut struct {
	formEmailSettingsOut
	// Workspace is the settings this form inherits from (nil when its account
	// has no workspace).
	Workspace *dbstore.EmailSettings `json:"workspace,omitempty"`
	// Effective is the resolved settings (per-form -> workspace -> default).
	Effective formEmailSettingsResolved `json:"effective"`
}

// getFormEmailSettings reads a form's per-form notification email settings plus
// the workspace settings it inherits from and the resolved effective values.
func (s *Server) getFormEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormEmailSettingsIn,
) (*mcp.CallToolResult, getFormEmailSettingsOut, error) {
	if s.deps.Forms == nil {
		return nil, getFormEmailSettingsOut{}, errors.New("forms not available")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, getFormEmailSettingsOut{}, err
	}
	form, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, getFormEmailSettingsOut{}, errors.New("form not found")
	}
	st, err := s.deps.Forms.FormEmailSettings(ctx, in.FormID)
	if err != nil {
		return nil, getFormEmailSettingsOut{}, err
	}
	ws := s.workspaceEmailSettings(ctx, form.AccountID)
	// Language consistency: the effective view is what a buyer receives, so it
	// renders an inherited system template in the buyer's language — the same
	// chain the sender uses (form.config.lang ?? workspace default ?? en).
	eff := dbstore.ResolveFormEmailSettings(st, ws,
		dbstore.EmailLang(dbstore.FormLangOf(form), s.workspaceLang(ctx, form.AccountID)))
	out := getFormEmailSettingsOut{
		formEmailSettingsOut: formEmailSettingsOut{
			FormID: st.FormID, ReceiptSubject: st.ReceiptSubject, ReceiptBody: st.ReceiptBody,
			ReceiptEnabled: st.ReceiptEnabled, NotifyMerchant: st.NotifyMerchant,
			MerchantNotifyEmail: st.MerchantNotifyEmail, HTML: st.HTML,
			ReceiptEnabledOverride: st.ReceiptEnabledOverride,
			NotifyMerchantOverride: st.NotifyMerchantOverride,
			HTMLOverride:           st.HTMLOverride,
		},
		Workspace: ws,
		Effective: formEmailSettingsResolved{
			FormID: eff.FormID, ReceiptSubject: eff.ReceiptSubject, ReceiptBody: eff.ReceiptBody,
			ReceiptEnabled: eff.ReceiptEnabled, NotifyMerchant: eff.NotifyMerchant,
			MerchantNotifyEmail: eff.MerchantNotifyEmail, HTML: eff.HTML,
		},
	}
	return nil, out, nil
}

// workspaceEmailSettings resolves the workspace email settings a form's account
// inherits from (nil when unresolvable).
func (s *Server) workspaceEmailSettings(ctx context.Context, accountID string) *dbstore.EmailSettings {
	if s.deps.Workspaces == nil || accountID == "" {
		return nil
	}
	wsID, err := s.deps.Workspaces.WorkspaceIDByAccount(ctx, accountID)
	if err != nil || wsID == "" {
		return nil
	}
	st, err := s.deps.Workspaces.EmailSettings(ctx, wsID)
	if err != nil {
		return nil
	}
	return st
}

// workspaceLang resolves the workspace's buyer-facing default language for an
// account, so an inherited system template is rendered in the language a buyer
// actually gets. Reads the same settings row workspaceEmailSettings reads and
// goes through WorkspaceLang, so a legacy row that predates the language column
// answers "en" rather than "".
func (s *Server) workspaceLang(ctx context.Context, accountID string) string {
	if s.deps.Workspaces == nil || accountID == "" {
		return emailtext.En
	}
	wsID, err := s.deps.Workspaces.WorkspaceIDByAccount(ctx, accountID)
	if err != nil || wsID == "" {
		return emailtext.En
	}
	st, serr := s.deps.Workspaces.Settings(ctx, wsID)
	if serr != nil || st == nil {
		return emailtext.En
	}
	return st.WorkspaceLang()
}

// setFormEmailSettingsIn updates a form's email settings. Omitted fields keep
// their current value (read-modify-write, matching set_email_settings).
//
// Issue #453: the *_override fields are tri-state — true/false sets an explicit
// per-form override, null (or omitted) inherits the workspace setting. Use
// clear_form_email_overrides to reset every override at once.
type setFormEmailSettingsIn struct {
	FormID              string  `json:"form_id" jsonschema:"Form ID"`
	ReceiptSubject      *string `json:"receipt_subject,omitempty" jsonschema:"Receipt subject (empty = use workspace default; omit = keep)"`
	ReceiptBody         *string `json:"receipt_body,omitempty" jsonschema:"Receipt body (empty = use workspace default; omit = keep)"`
	ReceiptEnabled      *bool   `json:"receipt_enabled,omitempty" jsonschema:"(legacy) Send receipt to buyer — treated as an explicit override"`
	NotifyMerchant      *bool   `json:"notify_merchant,omitempty" jsonschema:"(legacy) Send sale notification to merchant — treated as an explicit override"`
	MerchantNotifyEmail *string `json:"merchant_notify_email,omitempty" jsonschema:"Merchant email for notifications (empty = use account/workspace email; omit = keep)"`
	HTML                *bool   `json:"html,omitempty" jsonschema:"(legacy) HTML body — treated as an explicit override"`

	// Issue #453 tri-state overrides.
	ReceiptEnabledOverride *bool `json:"receipt_enabled_override,omitempty" jsonschema:"Per-form override: send receipt to buyer (true/false; null = follow workspace settings)"`
	NotifyMerchantOverride *bool `json:"notify_merchant_override,omitempty" jsonschema:"Per-form override: sale notification to merchant (true/false; null = follow workspace settings)"`
	HTMLOverride           *bool `json:"html_override,omitempty" jsonschema:"Per-form override: HTML body (true/false; null = follow workspace settings)"`
}

func (s *Server) setFormEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormEmailSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("form not found")
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
	if in.MerchantNotifyEmail != nil {
		st.MerchantNotifyEmail = *in.MerchantNotifyEmail
	}
	// Explicit override fields take precedence over the legacy aliases.
	if in.ReceiptEnabledOverride != nil {
		st.ReceiptEnabledOverride = in.ReceiptEnabledOverride
	} else if in.ReceiptEnabled != nil {
		st.ReceiptEnabledOverride = in.ReceiptEnabled
	}
	if in.NotifyMerchantOverride != nil {
		st.NotifyMerchantOverride = in.NotifyMerchantOverride
	} else if in.NotifyMerchant != nil {
		st.NotifyMerchantOverride = in.NotifyMerchant
	}
	if in.HTMLOverride != nil {
		st.HTMLOverride = in.HTMLOverride
	} else if in.HTML != nil {
		st.HTMLOverride = in.HTML
	}
	// Keep the legacy mirror columns consistent with the override.
	st.ReceiptEnabled = st.ReceiptEnabledOverride != nil && *st.ReceiptEnabledOverride
	st.NotifyMerchant = st.NotifyMerchantOverride != nil && *st.NotifyMerchantOverride
	st.HTML = st.HTMLOverride != nil && *st.HTMLOverride
	if err := s.deps.Forms.SaveFormEmailSettings(ctx, st); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}

// clearFormEmailOverridesIn is the input for clear_form_email_overrides.
type clearFormEmailOverridesIn struct {
	FormID string `json:"form_id" jsonschema:"Form ID"`
}

// clearFormEmailOverrides resets every per-form email override so the form
// inherits the workspace email settings again (issue #453).
func (s *Server) clearFormEmailOverrides(
	ctx context.Context, _ *mcp.CallToolRequest, in clearFormEmailOverridesIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms not available")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id is required")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("form not found")
	}
	if err := s.deps.Forms.ClearFormEmailOverrides(ctx, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "cleared", "form_id": in.FormID}, nil
}
