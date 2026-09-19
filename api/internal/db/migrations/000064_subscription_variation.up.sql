-- Issue #623: which product variation a subscription was bought as.
--
-- A variation may carry its own subscription cycle now. The recurring engine
-- resolves a subscription's fixed billing count at every charge, so it has to
-- know WHICH variation the customer bought, exactly as it already knows which
-- product. Without this the engine would keep reading the product's charge
-- count and a variation-fixed plan would bill on the product's terms — the same
-- bug class this issue fixes on the checkout path, merely moved one step later.
--
-- Nullable by design: every existing subscription (and every simple-product
-- subscription) has none, and NULL means "no variation — the product's cycle
-- fully applies", which is precisely today's behaviour. ON DELETE SET NULL
-- follows the same rule: if a variation is removed the subscription falls back
-- to the product's cycle rather than losing the subscription.
--
-- Read via subSelect/scanSub (the shared subscription select), so the column is
-- also carried by every subscription read surface.

ALTER TABLE subscriptions ADD COLUMN variation_id UUID REFERENCES product_variations(id) ON DELETE SET NULL;
