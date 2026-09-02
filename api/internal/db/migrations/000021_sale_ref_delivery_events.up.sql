-- Issue #106: correlate SES delivery events (delivered/open/click) to a
-- specific sale so merchants can see whether the buyer opened the receipt.
--
-- 1. email_delivery_events gains a nullable sale_ref column. The SNS endpoint
--    correlates an incoming SES event to a sale via the SES MessageID (the
--    receipt's message_id is captured at send time and stored on the sale),
--    then records the sale_ref on the delivery event row. NULL for system mail
--    or events that cannot be correlated.
ALTER TABLE email_delivery_events
    ADD COLUMN sale_ref TEXT;
CREATE INDEX idx_ede_sale_ref_time ON email_delivery_events (sale_ref, created_at DESC);

-- 2. sales gains receipt_message_id — the SES MessageID returned when the
--    receipt email is accepted for sending. This is the correlation key the
--    SNS endpoint uses to tie an Open/Click/Delivered event back to the sale.
ALTER TABLE sales
    ADD COLUMN receipt_message_id TEXT;
CREATE INDEX idx_sales_receipt_message_id ON sales (receipt_message_id);
