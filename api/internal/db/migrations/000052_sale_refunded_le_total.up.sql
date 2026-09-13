-- Migration 000052: enforce the money invariant refunded_sen <= total_sen on
-- sales (issue #527).
--
-- Issue #518 added sales.refunded_sen (the running refunded total, integer sen)
-- and an application-level over-refund guard in payments.RefundSale, which
-- rejects amount > 0 && refunded_sen + amount > total_sen BEFORE calling CHIP
-- and does the check + increment atomically under the per-sale advisory lock.
-- The application path is correct, but the invariant was never made structural:
-- nothing at the storage layer rejects refunded_sen > total_sen, so a future
-- code path, a new refund surface, a migration, or a manual operational fix can
-- persist a state every read surface (sales list, summary, analytics, CSV
-- export, MCP) derives money from — silently misstating the balance.
--
-- This adds the missing defence in depth. The invariant is named in the 000050
-- migration comments as "required by the money invariant refunded_sen <=
-- total_sen"; this is the constraint that makes it true rather than assumed.
--
-- Pre-existing rows satisfy it: refunded_sen defaults to 0, is only ever
-- incremented up to total_sen by the guarded refund path, and 000050 backfilled
-- refunded_sen = total_sen for status = 'refunded'. The guard below asserts that
-- before adding the constraint, so a database that somehow already violates the
-- invariant fails loudly at migration time instead of silently accepting the
-- constraint via NOT VALID semantics.
DO $$
DECLARE
    bad BIGINT;
BEGIN
    SELECT count(*) INTO bad FROM sales WHERE refunded_sen < 0 OR refunded_sen > total_sen;
    IF bad > 0 THEN
        RAISE EXCEPTION
            'cannot add sales_refunded_le_total: % existing row(s) violate refunded_sen <= total_sen',
            bad;
    END IF;
END $$;

ALTER TABLE sales
    ADD CONSTRAINT sales_refunded_nonneg_and_le_total
    CHECK (refunded_sen >= 0 AND refunded_sen <= total_sen);
