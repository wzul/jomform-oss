-- Migration 000039 (down): drop the space landing-page config column.
ALTER TABLE space_settings DROP COLUMN IF EXISTS landing_page_json;
