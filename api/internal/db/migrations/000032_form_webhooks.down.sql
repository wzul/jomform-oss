DROP INDEX IF EXISTS idx_outbound_webhooks_form;
ALTER TABLE outbound_webhooks DROP COLUMN IF EXISTS form_id;
