-- Issue #196: per-form webhook overrides.
-- A webhook may be scoped to a single form (form_id set) or space/account-wide
-- (form_id NULL). Form-scoped webhooks fire only for that form's events;
-- account-wide ones still fire for all forms. Existing rows keep NULL (space-wide).
ALTER TABLE outbound_webhooks
    ADD COLUMN form_id UUID REFERENCES forms(id) ON DELETE CASCADE;

CREATE INDEX idx_outbound_webhooks_form ON outbound_webhooks(form_id) WHERE form_id IS NOT NULL;
