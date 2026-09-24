// Per-form notification email settings (issue #177, Gravity-Forms-style).
package dbstore

import (
	"context"
	"encoding/json"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// FormEmailSettings is the per-form receipt/notification email configuration.
//
// Issue #453: a form may override the workspace's email settings so a merchant
// can send different emails for different forms (mirroring #107 and the #57
// language-override pattern). Resolution order is:
//
//	per-form override -> workspace setting -> built-in default
//
// Subject/body/merchant email inherit when empty. The boolean toggles are
// tri-state: a nil *bool means "use the workspace default", while a non-nil
// pointer is an explicit per-form override (so a form can turn receipts OFF or
// merchant notifications ON without touching the subject/body).
type FormEmailSettings struct {
	FormID              string `json:"form_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`

	// Per-form overrides (issue #453). nil = inherit the workspace setting.
	ReceiptEnabledOverride *bool `json:"receipt_enabled_override,omitempty"`
	NotifyMerchantOverride *bool `json:"notify_merchant_override,omitempty"`
	HTMLOverride           *bool `json:"html_override,omitempty"`
}

// HasOverride reports whether any per-form email override is set. When false the
// form inherits the workspace settings entirely.
func (st *FormEmailSettings) HasOverride() bool {
	return st.ReceiptSubject != "" || st.ReceiptBody != "" ||
		st.MerchantNotifyEmail != "" ||
		st.ReceiptEnabledOverride != nil || st.NotifyMerchantOverride != nil ||
		st.HTMLOverride != nil
}

// FormEmailSettings reads (creating a default row when missing) the per-form
// email settings. Empty subject/body and nil toggles signal "use the workspace
// defaults" (issue #453).
func (s *FormStore) FormEmailSettings(ctx context.Context, formID string) (*FormEmailSettings, error) {
	st := &FormEmailSettings{FormID: formID, ReceiptEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		        COALESCE(merchant_notify_email,''), html,
		        receipt_enabled_override, notify_merchant_override, html_override
		   FROM form_email_settings WHERE form_id=$1`,
		formID).Scan(&st.ReceiptSubject, &st.ReceiptBody, &st.ReceiptEnabled,
		&st.NotifyMerchant, &st.MerchantNotifyEmail, &st.HTML,
		&st.ReceiptEnabledOverride, &st.NotifyMerchantOverride, &st.HTMLOverride)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO form_email_settings (form_id) VALUES ($1) ON CONFLICT DO NOTHING`, formID)
		if ierr != nil {
			return nil, ierr
		}
	}
	return st, nil
}

// FormLang reads the form's buyer-facing language override — `config.lang`, the
// same key the public renderer resolves at (issue #57 / language placement).
//
// It returns "" when the form carries no override OR carries one that is not a
// supported locale, so the caller falls through to the workspace default. That
// distinction is why it does NOT return NormalizeLang's answer: NormalizeLang
// maps "fr" onto "en", which would silently STOP the chain at the form instead
// of letting the workspace setting apply.
func (s *FormStore) FormLang(ctx context.Context, formID string) (string, error) {
	if formID == "" {
		return "", nil
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT config FROM forms WHERE id=$1`, formID).Scan(&raw); err != nil {
		return "", err
	}
	return formLangFromConfig(raw), nil
}

// FormLangOf returns a Form's own language override, for a caller that already
// holds the row (no extra query). Same "" contract as FormLang.
func FormLangOf(f *Form) string {
	if f == nil || f.Config == nil {
		return ""
	}
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return ""
	}
	return formLangFromConfig(raw)
}

// formLangFromConfig extracts a supported `lang` from a form's config JSON.
func formLangFromConfig(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg struct {
		Lang string `json:"lang"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	if !emailtext.Supported(cfg.Lang) {
		return ""
	}
	return emailtext.NormalizeLang(cfg.Lang)
}

// EmailLang resolves the language a form's automated email is written in, on the
// BUYER axis: form.config.lang ?? workspace_settings.default_lang ?? "en".
//
// This is the one place the buyer-facing chain is spelled out for email. It is
// deliberately NOT the merchant's personal users.lang — a merchant who set his
// dashboard to English while his public form is Malay gets an English auth email
// and his buyers get Malay receipts.
//
// formLang is the form's own override (FormLang / FormLangOf) and may be empty.
// wsDefaultLang is the workspace's stored `default_lang`, raw: it is passed
// unvalidated so this function is the ONE place the fallback lives, exactly as
// (*WorkspaceSettings).WorkspaceLang does for the rendered page. Both arguments
// accept an unsupported code and fall through, so "fr" at either step resolves
// to English rather than stopping the chain.
func EmailLang(formLang, wsDefaultLang string) string {
	if emailtext.Supported(formLang) {
		return emailtext.NormalizeLang(formLang)
	}
	if emailtext.Supported(wsDefaultLang) {
		return emailtext.NormalizeLang(wsDefaultLang)
	}
	return emailtext.En
}

// EmailLangFor is EmailLang with the workspace default resolved from the store.
// A missing workspace (or an unreadable settings row) contributes nothing, so
// the form override — or English — still decides. Never returns "".
func (s *WorkspaceStore) EmailLangFor(ctx context.Context, workspaceID, formLang string) string {
	if emailtext.Supported(formLang) {
		return emailtext.NormalizeLang(formLang)
	}
	if s == nil || workspaceID == "" {
		return emailtext.En
	}
	st, err := s.Settings(ctx, workspaceID)
	if err != nil || st == nil {
		return emailtext.En
	}
	return EmailLang("", st.DefaultLang)
}

// SaveFormEmailSettings upserts the per-form email settings.
func (s *FormStore) SaveFormEmailSettings(ctx context.Context, st *FormEmailSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO form_email_settings
		   (form_id, receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		    merchant_notify_email, html,
		    receipt_enabled_override, notify_merchant_override, html_override)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (form_id) DO UPDATE SET
		   receipt_subject=EXCLUDED.receipt_subject, receipt_body=EXCLUDED.receipt_body,
		   receipt_enabled=EXCLUDED.receipt_enabled, notify_merchant=EXCLUDED.notify_merchant,
		   merchant_notify_email=EXCLUDED.merchant_notify_email, html=EXCLUDED.html,
		   receipt_enabled_override=EXCLUDED.receipt_enabled_override,
		   notify_merchant_override=EXCLUDED.notify_merchant_override,
		   html_override=EXCLUDED.html_override,
		   updated_at=now()`,
		st.FormID, st.ReceiptSubject, st.ReceiptBody, st.ReceiptEnabled,
		st.NotifyMerchant, st.MerchantNotifyEmail, st.HTML,
		st.ReceiptEnabledOverride, st.NotifyMerchantOverride, st.HTMLOverride)
	return err
}

// ClearFormEmailOverrides resets every per-form email override so the form
// inherits the workspace settings again (issue #453). The subject/body are
// blanked and the toggles return to nil (inherit).
func (s *FormStore) ClearFormEmailOverrides(ctx context.Context, formID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE form_email_settings
		    SET receipt_subject='', receipt_body='', merchant_notify_email='',
		        receipt_enabled_override=NULL, notify_merchant_override=NULL,
		        html_override=NULL, updated_at=now()
		  WHERE form_id=$1`, formID)
	return err
}

// ResolveFormEmailSettings returns the effective email settings for a form by
// applying the resolution order per-form override -> workspace setting ->
// built-in default (issue #453). ws may be nil when the workspace settings are
// unavailable; in that case the built-in defaults are used for inherited
// fields.
//
// lang is the resolved buyer-facing language (EmailLang) and decides the
// language of the BUILT-IN templates. Those templates live in the database as
// Malay literals: workspace_email_settings defaults to one (migration 000021)
// and EnsureDefault stores another per rule (#597), so a workspace nobody
// customised holds Malay and — before this — mailed Malay to a buyer whose form
// was English. A stored value that still equals a built-in template is therefore
// re-rendered in lang; anything a merchant actually wrote is passed through
// verbatim and never translated.
func ResolveFormEmailSettings(form *FormEmailSettings, ws *EmailSettings, lang string) *FormEmailSettings {
	l := emailtext.NormalizeLang(lang)
	def := emailtext.Text(l, emailtext.Receipt, nil)
	out := &FormEmailSettings{
		ReceiptSubject: def.Subject,
		ReceiptBody:    def.Body,
		ReceiptEnabled: true,
	}
	// 2. Workspace setting.
	if ws != nil {
		out.ReceiptSubject = ws.ReceiptSubject
		out.ReceiptBody = ws.ReceiptBody
		out.ReceiptEnabled = ws.ReceiptEnabled
		out.NotifyMerchant = ws.NotifyMerchant
		out.MerchantNotifyEmail = ws.MerchantNotifyEmail
		out.HTML = ws.HTML
	}
	// 1. Per-form override wins field-by-field.
	if form != nil {
		out.FormID = form.FormID
		if form.ReceiptSubject != "" {
			out.ReceiptSubject = form.ReceiptSubject
		}
		if form.ReceiptBody != "" {
			out.ReceiptBody = form.ReceiptBody
		}
		if form.MerchantNotifyEmail != "" {
			out.MerchantNotifyEmail = form.MerchantNotifyEmail
		}
		if form.ReceiptEnabledOverride != nil {
			out.ReceiptEnabled = *form.ReceiptEnabledOverride
		}
		if form.NotifyMerchantOverride != nil {
			out.NotifyMerchant = *form.NotifyMerchantOverride
		}
		if form.HTMLOverride != nil {
			out.HTML = *form.HTMLOverride
		}
	}
	// 3. Built-in defaults and the language-aware re-render of an inherited
	// system template.
	//
	// Each half is decided ON ITS OWN, because a subject and a body are two
	// separate merchant decisions even though the settings UI submits them
	// together: it loads both stored values and posts both back, so a merchant
	// who edits only the body writes the untouched system subject back verbatim.
	// A pair-wise test then classifies that row as entirely the merchant's, and
	// the Malay subject the pre-language release wrote is mailed to a buyer whose
	// form is English. Per field, an empty half is filled from the default and a
	// half that is still a BUILT-IN template is re-rendered in the buyer's
	// language; a half the merchant actually wrote is passed through untouched.
	// A mixed row therefore renders "merchant subject + localised body" with
	// neither half overwritten.
	if out.ReceiptSubject == "" || emailtext.SubjectIsSystemDefault(emailtext.Receipt, out.ReceiptSubject) {
		out.ReceiptSubject = def.Subject
	}
	if out.ReceiptBody == "" || emailtext.BodyIsSystemDefault(emailtext.Receipt, out.ReceiptBody) {
		out.ReceiptBody = def.Body
	}
	return out
}
