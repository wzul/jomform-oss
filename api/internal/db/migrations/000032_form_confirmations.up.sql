-- Issue #179: form confirmations (Gravity-Forms-style).
--
-- A form can have multiple confirmations, each either a message (HTML,
-- sanitized on render) or a redirect, optionally gated by conditional logic
-- against the submitted field values. The first matching confirmation (by
-- position) wins; the default confirmation (is_default=true) has no condition
-- and always matches, so it is the fallback. A default row is created lazily
-- on first read so every form has one without a migration backfill.

CREATE TABLE IF NOT EXISTS form_confirmations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    form_id      UUID NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL DEFAULT 'message' CHECK (type IN ('message','redirect')),
    message      TEXT NOT NULL DEFAULT '',
    redirect_url TEXT NOT NULL DEFAULT '',
    condition    JSONB,
    is_default   BOOLEAN NOT NULL DEFAULT false,
    position     INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_form_confirmations_form ON form_confirmations(form_id, position);

-- Issue #179: persist the submitted field values on a sale so the thank-you
-- page can evaluate confirmation conditional logic against them.
ALTER TABLE sales ADD COLUMN IF NOT EXISTS fields JSONB;
