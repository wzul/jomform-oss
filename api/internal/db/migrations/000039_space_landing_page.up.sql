-- Migration 000039: customizable space landing page (issue #360).
-- JSONB holding the merchant's landing-page design config (hero heading,
-- subtitle, body text, banner image, theme colors, logo, CTA buttons linking
-- to published forms). '{}' = no config -> handleSpaceRoot falls back to the
-- bare storefront listing (existing behavior).
ALTER TABLE space_settings ADD COLUMN IF NOT EXISTS landing_page_json JSONB NOT NULL DEFAULT '{}';
