// Per-form notification email settings (issue #177, Gravity-Forms-style).
package dbstore

import "context"

// FormEmailSettings is the per-form receipt/notification email configuration.
// Empty subject/body means "fall back to the workspace's email settings"; a
// customised row (non-empty subject or body) overrides the workspace defaults for
// that form's receipts. Defaults mirror the workspace defaults so existing
// behaviour is unchanged until a merchant customises.
type FormEmailSettings struct {
	FormID              string `json:"form_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// FormEmailSettings reads (creating a default row when missing) the per-form
// email settings. Empty subject/body signal "use the workspace defaults".
func (s *FormStore) FormEmailSettings(ctx context.Context, formID string) (*FormEmailSettings, error) {
	st := &FormEmailSettings{FormID: formID, ReceiptEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		        COALESCE(merchant_notify_email,''), html
		   FROM form_email_settings WHERE form_id=$1`,
		formID).Scan(&st.ReceiptSubject, &st.ReceiptBody, &st.ReceiptEnabled,
		&st.NotifyMerchant, &st.MerchantNotifyEmail, &st.HTML)
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
		   (form_id, receipt_subject, receipt_body, receipt_enabled, notify_merchant, merchant_notify_email, html)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (form_id) DO UPDATE SET
		   receipt_subject=EXCLUDED.receipt_subject, receipt_body=EXCLUDED.receipt_body,
		   receipt_enabled=EXCLUDED.receipt_enabled, notify_merchant=EXCLUDED.notify_merchant,
		   merchant_notify_email=EXCLUDED.merchant_notify_email, html=EXCLUDED.html,
		   updated_at=now()`,
		st.FormID, st.ReceiptSubject, st.ReceiptBody, st.ReceiptEnabled,
		st.NotifyMerchant, st.MerchantNotifyEmail, st.HTML)
	return err
}
