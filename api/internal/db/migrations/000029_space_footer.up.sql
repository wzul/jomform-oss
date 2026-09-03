-- Issue #183: optional per-space public-form footer. All columns are optional —
-- a merchant who sets nothing gets no footer (preserving #80's clean default).
ALTER TABLE space_settings
  ADD COLUMN refund_url      TEXT NOT NULL DEFAULT '',
  ADD COLUMN contact_phone   TEXT NOT NULL DEFAULT '',
  ADD COLUMN contact_address TEXT NOT NULL DEFAULT '',
  ADD COLUMN business_name   TEXT NOT NULL DEFAULT '';
