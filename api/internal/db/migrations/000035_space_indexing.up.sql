-- Issue #276: per-space SEO indexing toggle.
--
-- When true (default), the space's public storefront + forms are eligible for
-- search-engine / AI indexing (on the jomform.com apex and on custom domains).
-- When false, the space opts out of indexing entirely. The public form
-- renderer still emits noindex on jomform.com subdomains regardless of this
-- flag (privacy — payment links shouldn't appear on Google); this flag only
-- lifts indexing for spaces that use their own custom domain.
ALTER TABLE space_settings ADD COLUMN IF NOT EXISTS indexing BOOLEAN NOT NULL DEFAULT TRUE;
