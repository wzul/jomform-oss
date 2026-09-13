-- Issue #518: drop the sale refunded amount column.
ALTER TABLE sales DROP COLUMN refunded_sen;
