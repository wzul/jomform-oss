// Per-workspace email template + behaviour settings (issue #107).
package dbstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// EmailSettings is the per-workspace receipt/notification email configuration.
//
// ReceiptSubject and ReceiptBody are EMPTY until the merchant writes them. An
// empty half is not a missing subject: it means "JomForm's own receipt, in the
// language the buyer's form is set to" (dbstore.ResolveFormEmailSettings fills
// it from internal/emailtext). Migration 000083 emptied the column DEFAULT and
// blanked the rows that still held the pre-language Malay literal; before that,
// every workspace stored one language's receipt as if it were merchant content.
type EmailSettings struct {
	WorkspaceID         string `json:"workspace_id"`
	ReceiptSubject      string `json:"receipt_subject"`
	ReceiptBody         string `json:"receipt_body"`
	ReceiptEnabled      bool   `json:"receipt_enabled"`
	NotifyMerchant      bool   `json:"notify_merchant"`
	MerchantNotifyEmail string `json:"merchant_notify_email"`
	HTML                bool   `json:"html"`
}

// EmailSettings reads the per-workspace email settings.
//
// A workspace with no stored row reads as the built-in defaults and is NOT
// materialised: a read must never write. This used to INSERT a default row from
// the error branch, so a plain page load — a single GET with no save — created
// one and owned `updated_at`, making that column useless as "did this merchant
// edit their email settings?". The row is created where a write belongs: the
// first SaveEmailSettings (the PUT handler) upserts it.
//
// A new row stores no receipt template at all: both text fields are empty,
// which the sender resolves to the built-in receipt in the buyer's language
// rather than to one stored language.
//
// Only pgx.ErrNoRows means "no row yet". Every other failure is returned, so a
// dropped connection or a renamed column surfaces as an error instead of being
// answered with the defaults and HTTP 200.
func (s *WorkspaceStore) EmailSettings(ctx context.Context, workspaceID string) (*EmailSettings, error) {
	st := &EmailSettings{WorkspaceID: workspaceID, ReceiptEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body, receipt_enabled, notify_merchant,
		        COALESCE(merchant_notify_email,''), html
		   FROM workspace_email_settings WHERE workspace_id=$1`,
		workspaceID).Scan(&st.ReceiptSubject, &st.ReceiptBody, &st.ReceiptEnabled,
		&st.NotifyMerchant, &st.MerchantNotifyEmail, &st.HTML)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return st, nil
		}
		return nil, err
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
