-- Issue #438: QR ordering flow — table number on a sale.
-- The QR stand encodes ?table=N; the order carries the table so the
-- merchant knows where to serve. NULL/'' = no table (regular online order).
ALTER TABLE sales ADD COLUMN table_number TEXT;
