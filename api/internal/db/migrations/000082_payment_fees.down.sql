ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_fee_nonneg_chk;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_percent_bp;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_fixed_sen;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_method;
ALTER TABLE sales DROP COLUMN IF EXISTS fee_sen;

ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_fee_override_chk;
ALTER TABLE forms DROP COLUMN IF EXISTS fee_override;

DROP TABLE IF EXISTS workspace_fee_acknowledgements;
DROP TABLE IF EXISTS payment_fee_rates;
