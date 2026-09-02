// Per-space email template + behaviour settings (issue #107).
package dbstore

import (
	"context"
)

// EmailSettings is the per-space receipt/notification email configuration.
// Defaults reproduce the previous hardcoded receipt exactly, so existing
// behaviour is unchanged until a merchant customises.
type EmailSettings struct {
	SpaceID             string `json:"space_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// EmailSettings reads (creating a default row when missing) the per-space
// email settings. Defaults mirror the current hardcoded receipt template.
func (s *SpaceStore) EmailSettings(ctx context.Context, spaceID string) (*EmailSettings, error) {
	st := &EmailSettings{SpaceID: spaceID, ReceiptEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		        COALESCE(merchant_notify_email,''), html
		   FROM space_email_settings WHERE space_id=$1`,
		spaceID).Scan(&st.ReceiptSubject, &st.ReceiptBody, &st.ReceiptEnabled,
		&st.NotifyMerchant, &st.MerchantNotifyEmail, &st.HTML)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO space_email_settings (space_id) VALUES ($1) ON CONFLICT DO NOTHING`, spaceID)
		if ierr != nil {
			return nil, ierr
		}
	}
	return st, nil
}

// SaveEmailSettings upserts the per-space email settings.
func (s *SpaceStore) SaveEmailSettings(ctx context.Context, st *EmailSettings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO space_email_settings
		   (space_id, receipt_subject, receipt_body, receipt_enabled, notify_merchant, merchant_notify_email, html)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (space_id) DO UPDATE SET
		   receipt_subject=EXCLUDED.receipt_subject, receipt_body=EXCLUDED.receipt_body,
		   receipt_enabled=EXCLUDED.receipt_enabled, notify_merchant=EXCLUDED.notify_merchant,
		   merchant_notify_email=EXCLUDED.merchant_notify_email, html=EXCLUDED.html,
		   updated_at=now()`,
		st.SpaceID, st.ReceiptSubject, st.ReceiptBody, st.ReceiptEnabled,
		st.NotifyMerchant, st.MerchantNotifyEmail, st.HTML)
	return err
}
