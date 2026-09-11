// Per-form notification email settings (issue #177, Gravity-Forms-style).
package dbstore

import "context"

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
func ResolveFormEmailSettings(form *FormEmailSettings, ws *EmailSettings) *FormEmailSettings {
	out := &FormEmailSettings{
		ReceiptSubject: defaultEmailReceiptSubject,
		ReceiptBody:    defaultEmailReceiptBody,
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
	// 3. Built-in defaults for any still-empty inherited field.
	if out.ReceiptSubject == "" {
		out.ReceiptSubject = defaultEmailReceiptSubject
	}
	if out.ReceiptBody == "" {
		out.ReceiptBody = defaultEmailReceiptBody
	}
	return out
}

// defaultEmailReceiptSubject / defaultEmailReceiptBody mirror the payments
// package defaults so resolution can be computed without importing payments
// (which would create an import cycle).
const (
	defaultEmailReceiptSubject = "Resit pembayaran — {{ref_code}}"
	defaultEmailReceiptBody    = `Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: {{form_url}}`
)
