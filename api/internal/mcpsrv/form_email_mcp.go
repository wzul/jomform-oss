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
		return nil, getFormEmailSettingsOut{}, errors.New("forms tidak tersedia")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, getFormEmailSettingsOut{}, err
	}
	form, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, getFormEmailSettingsOut{}, errors.New("borang tidak dijumpai")
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
	FormID              string  `json:"form_id" jsonschema:"ID borang"`
	ReceiptSubject      *string `json:"receipt_subject,omitempty" jsonschema:"Subjek resit (kosong = guna lalai workspace; omit = kekal)"`
	ReceiptBody         *string `json:"receipt_body,omitempty" jsonschema:"Badan resit (kosong = guna lalai workspace; omit = kekal)"`
	ReceiptEnabled      *bool   `json:"receipt_enabled,omitempty" jsonschema:"(legacy) Hantar resit kepada pembeli — dianggap sebagai override eksplisit"`
	NotifyMerchant      *bool   `json:"notify_merchant,omitempty" jsonschema:"(legacy) Hantar notifikasi jualan kepada merchant — dianggap sebagai override eksplisit"`
	MerchantNotifyEmail *string `json:"merchant_notify_email,omitempty" jsonschema:"Emel merchant untuk notifikasi (kosong = guna emel akaun/workspace; omit = kekal)"`
	HTML                *bool   `json:"html,omitempty" jsonschema:"(legacy) Badan HTML — dianggap sebagai override eksplisit"`

	// Issue #453 tri-state overrides.
	ReceiptEnabledOverride *bool `json:"receipt_enabled_override,omitempty" jsonschema:"Override per-borang: hantar resit kepada pembeli (true/false; null = ikut tetapan workspace)"`
	NotifyMerchantOverride *bool `json:"notify_merchant_override,omitempty" jsonschema:"Override per-borang: notifikasi jualan kepada merchant (true/false; null = ikut tetapan workspace)"`
	HTMLOverride           *bool `json:"html_override,omitempty" jsonschema:"Override per-borang: badan HTML (true/false; null = ikut tetapan workspace)"`
}

func (s *Server) setFormEmailSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormEmailSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
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
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// clearFormEmailOverrides resets every per-form email override so the form
// inherits the workspace email settings again (issue #453).
func (s *Server) clearFormEmailOverrides(
	ctx context.Context, _ *mcp.CallToolRequest, in clearFormEmailOverridesIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	if err := s.deps.Forms.ClearFormEmailOverrides(ctx, in.FormID); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "cleared", "form_id": in.FormID}, nil
}
