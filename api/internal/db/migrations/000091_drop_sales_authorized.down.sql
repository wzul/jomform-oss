-- Restore the legacy `authorized` flag on sales (rollback of 000091).
--
-- The restore is lossless: the only writer (dbstore.SaleStore.MarkCaptured)
-- always set the flag together with `captured_at`, so `captured_at IS NOT NULL`
-- reconstructs every value the column could hold. The default keeps it NOT NULL
-- for a row inserted by an older binary between the down-migration and a
-- restart.
ALTER TABLE sales ADD COLUMN IF NOT EXISTS authorized BOOLEAN NOT NULL DEFAULT false;

UPDATE sales SET authorized = (captured_at IS NOT NULL);
