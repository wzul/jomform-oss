ALTER TABLE accounts ADD COLUMN IF NOT EXISTS wh_sign_pub TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS wh_sign_priv TEXT NOT NULL DEFAULT '';
-- forms: custom success/failure redirect + embed flag + logo url
ALTER TABLE forms ADD COLUMN IF NOT EXISTS redirect_success TEXT NOT NULL DEFAULT '';
ALTER TABLE forms ADD COLUMN IF NOT EXISTS redirect_failure TEXT NOT NULL DEFAULT '';
ALTER TABLE forms ADD COLUMN IF NOT EXISTS allow_embed BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE forms ADD COLUMN IF NOT EXISTS logo_url TEXT NOT NULL DEFAULT '';
-- template gallery seed (system templates flag)
ALTER TABLE forms ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT false;
