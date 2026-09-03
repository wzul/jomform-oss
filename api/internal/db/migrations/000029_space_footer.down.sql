-- Issue #183 down: drop the optional footer columns.
ALTER TABLE space_settings
  DROP COLUMN IF EXISTS refund_url,
  DROP COLUMN IF EXISTS contact_phone,
  DROP COLUMN IF EXISTS contact_address,
  DROP COLUMN IF EXISTS business_name;
