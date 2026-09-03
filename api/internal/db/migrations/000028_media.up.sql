-- Issue #180: media library (WordPress-media-library-style).
--
-- media: one row per uploaded object in the media bucket. Merchant uploads
-- (image block #61, avatar #133) are recorded here so the dashboard can list
-- them with metadata (name, mime, size, date) and filter by form/source.
-- Customer file uploads (a dependency, not yet built) will land under a
-- distinct source ('customer') and a form_id so the Media page can filter by
-- form to show them.
--
-- This is a NEW table (not an ALTER on a *Select table), so the migration
-- scan-drift guard is unaffected.
--
-- Soft-delete: deleted_at set on delete; the object is also removed from
-- MinIO. Rows are excluded from listings once deleted_at is non-null.

CREATE TABLE media (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    form_id    UUID REFERENCES forms(id) ON DELETE SET NULL,
    key        TEXT NOT NULL,
    url        TEXT NOT NULL,
    mime       TEXT NOT NULL,
    size       BIGINT NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT 'merchant',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX media_account_created_idx ON media (account_id, created_at DESC);
CREATE INDEX media_account_form_idx ON media (account_id, form_id);
