-- Issue #171: response-only forms (Google-Form-style, no payment).
--
-- submissions: one row per response-only form submission. A response-mode
-- form has no products and no payment step — the buyer's field answers are
-- stored here as a JSONB map {block_id: value}. Payment-mode forms never
-- write to this table (they keep creating sales as before).
--
-- This is a NEW table (not an ALTER on a *Select table), so the migration
-- scan-drift guard is unaffected.

CREATE TABLE submissions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    form_id    UUID NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    data       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX submissions_form_created_idx ON submissions (form_id, created_at DESC);
CREATE INDEX submissions_account_created_idx ON submissions (account_id, created_at DESC);
