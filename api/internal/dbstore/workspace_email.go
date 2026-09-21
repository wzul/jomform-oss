// Per-workspace email template + behaviour settings (issue #107).
package dbstore

import (
	"context"
)

// EmailSettings is the per-workspace receipt/notification email configuration.
// Defaults reproduce the previous hardcoded receipt exactly, so existing
// behaviour is unchanged until a merchant customises.
type EmailSettings struct {
	WorkspaceID         string `json:"workspace_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// EmailSettings reads (creating a default row when missing) the per-workspace
// email settings. Defaults mirror the current hardcoded receipt template.
func (s *WorkspaceStore) EmailSettings(ctx context.Context, workspaceID string) (*EmailSettings, error) {
	st := &EmailSettings{WorkspaceID: workspaceID, ReceiptEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		        COALESCE(merchant_notify_email,''), html
		   FROM workspace_email_settings WHERE workspace_id=$1`,
		workspaceID).Scan(&st.ReceiptSubject, &st.ReceiptBody, &st.ReceiptEnabled,
		&st.NotifyMerchant, &st.MerchantNotifyEmail, &st.HTML)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO workspace_email_settings (workspace_id) VALUES ($1) ON CONFLICT DO NOTHING`, workspaceID)
		if ierr != nil {
			return nil, ierr
		}
	}
	return st, nil
}

// SaveEmailSettings upserts the per-workspace email settings.
func (s *WorkspaceStore) SaveEmailSettings(ctx context.Context, st *EmailSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspace_email_settings
		   (workspace_id, receipt_subject, receipt_body, receipt_enabled, notify_merchant, merchant_notify_email, html)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (workspace_id) DO UPDATE SET
		   receipt_subject=EXCLUDED.receipt_subject, receipt_body=EXCLUDED.receipt_body,
		   receipt_enabled=EXCLUDED.receipt_enabled, notify_merchant=EXCLUDED.notify_merchant,
		   merchant_notify_email=EXCLUDED.merchant_notify_email, html=EXCLUDED.html,
		   updated_at=now()`,
		st.WorkspaceID, st.ReceiptSubject, st.ReceiptBody, st.ReceiptEnabled,
		st.NotifyMerchant, st.MerchantNotifyEmail, st.HTML)
	return err
}
