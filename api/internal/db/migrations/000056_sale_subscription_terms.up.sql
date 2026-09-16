-- Subscription terms on the sale (issue #560).
--
-- The public checkout resolved prices live from the products table but ignored
-- the product's TYPE, so every order was recorded sale_type='one_time'. A
-- subscription product therefore never activated: activateSubscription only runs
-- for a subscription sale, and the card is only tokenized for future charges
-- when the sale is a subscription.
--
-- Resolving the type fixes that, but activation also needs the SUBSCRIPTION's
-- own terms — and a cart may legitimately mix a subscription with one-time items
-- (the subscription product drives the recurring schedule; the other items are
-- charged once, in the first payment). Deriving the recurring amount from
-- sale.total_sen would then bill the one-time items every period, and
-- activateSubscription hardcoded `period := "month"` ("product lookup lands with
-- products store wiring" — a TODO never finished), which would bill a yearly
-- product monthly.
--
-- These columns record the terms chosen at checkout so activation reads them
-- rather than re-deriving them.
--
-- Nullable by design: every existing (and every one-time) sale has none. No
-- backfill is possible or meaningful — the information does not exist for
-- historical rows.
ALTER TABLE sales ADD COLUMN subscription_product_id UUID REFERENCES products(id) ON DELETE SET NULL;
ALTER TABLE sales ADD COLUMN subscription_amount_sen BIGINT CHECK (subscription_amount_sen IS NULL OR subscription_amount_sen >= 0);
ALTER TABLE sales ADD COLUMN subscription_period TEXT CHECK (subscription_period IS NULL OR subscription_period IN ('month', 'year'));
