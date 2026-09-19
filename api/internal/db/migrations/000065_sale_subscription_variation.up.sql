-- Issue #623: the variation a subscription sale was bought as.
--
-- The checkout records which variation of a subscription product the buyer
-- chose, so activation can carry it onto the subscription row and the recurring
-- engine can resolve that variation's own cycle (fixed billing count, and the
-- period fallback) exactly as the checkout did. Storing it on the sale (rather
-- than re-deriving it from the cart later) is the same rule the #560 terms
-- already follow: the terms chosen at checkout are recorded, not recomputed.
--
-- Nullable by design — every existing sale and every simple-product sale has
-- none, and NULL means "no variation", i.e. the product's cycle. ON DELETE SET
-- NULL keeps a refundable/paid historical sale intact if a variation is later
-- removed.

ALTER TABLE sales ADD COLUMN subscription_variation_id UUID REFERENCES product_variations(id) ON DELETE SET NULL;
