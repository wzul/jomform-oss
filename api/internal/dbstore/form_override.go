package dbstore

import "context"

// Form CHIP override accessors (columns from migration 000005).

// SetChipOverride stores per-form credentials (secret already encrypted by caller).
func (s *FormStore) SetChipOverride(ctx context.Context, accountID, formID, brandID, secretEnc, emailFallback string, whitelist []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET chip_brand_id=NULLIF($3,''), chip_secret_key_enc=NULLIF($4,''),
		   chip_email_fallback=NULLIF($5,'')::citext, chip_method_whitelist=$6
		 WHERE id=$2 AND account_id=$1`,
		accountID, formID, brandID, secretEnc, emailFallback, whitelist)
	return err
}

// ClearChipOverride removes the per-form override so account creds apply.
func (s *FormStore) ClearChipOverride(ctx context.Context, accountID, formID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE forms SET chip_brand_id=NULL, chip_secret_key_enc=NULL,
		   chip_email_fallback=NULL, chip_method_whitelist=NULL
		 WHERE id=$2 AND account_id=$1`, accountID, formID)
	return err
}

// ChipOverrideFor returns the form's override credentials (may be zero-value).
func (s *FormStore) ChipOverrideFor(ctx context.Context, accountID, formID string) (brandID, secretEnc, emailFallback string, whitelist []string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT COALESCE(chip_brand_id,''), COALESCE(chip_secret_key_enc,''),
		        COALESCE(chip_email_fallback::text,''), COALESCE(chip_method_whitelist,'{}')
		 FROM forms WHERE id=$1 AND account_id=$2`,
		formID, accountID).Scan(&brandID, &secretEnc, &emailFallback, &whitelist)
	return
}
