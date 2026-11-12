-- Platform fee as its own line in the CHIP purchase (card t_1a17d22d).
--
-- WHY. Until this migration the buyer-borne fee was modelled as the ACQUIRER's
-- charge passed on to the buyer and GROSSED UP into the total: RM1,000 at 1%
-- was charged as RM1,010.10 with a "Processing fee — Card 1.00%" line. The
-- arithmetic was right but the presentation was not: it read as a mysterious
-- markup on the buyer's own amount. CHIP's `purchase.products[]` can carry the
-- charge as its own item line, so the buyer's price stays exactly the amount
-- they typed and JomForm's charge appears as
--
--     products: [ {name: "<item>", price: A}, {name: "Platform fee", price: P} ]
--
-- WHAT A payment_fee_rates ROW MEANS NOW.
--   platform_fee_sen > 0                          -> the platform model: P is
--       added as its own line and the buyer's price is untouched.
--   platform_fee_sen = 0 and fee_percent_bp = 0
--     and fee_fixed_sen > 0                      -> arithmetically identical to
--       a flat platform fee of fee_fixed_sen (grossing up a CONSTANT fee leaves
--       exactly A + fixed), so it is read as a platform fee of that amount and
--       the buyer's price is unchanged.
--   platform_fee_sen = 0 and fee_percent_bp > 0  -> LEGACY GROSS-UP. See the
--       migration decision below.
--
-- MIGRATION DECISION (card requirement 6): a workspace that stores a
-- PERCENTAGE rate keeps being charged EXACTLY what it charges today for one
-- release. payments.GrossUp still prices it, the sale records
-- fee_model = 'legacy_grossup', and nothing silently becomes a platform amount
-- the merchant never set. Moving to the platform model is an explicit save of a
-- platform amount (Settings → Payment fees, once the approved UI ships); until
-- then the old rate fields stay readable and keep working.
ALTER TABLE payment_fee_rates
    ADD COLUMN platform_fee_sen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE payment_fee_rates
    ADD CONSTRAINT payment_fee_rates_platform_chk CHECK (platform_fee_sen >= 0);

-- FPX is TWO CHIP identifiers with different acquirer costs: `fpx` (personal,
-- B2C) and `fpx_b2b1` (corporate, B2B1). A platform fee is keyed by the CHIP
-- identifier, so both need their own row. They are also two buyer choices
-- (fpx and fpx_b2b1 are two picker rows), and each row's amount comes from its
-- own identifier.
ALTER TABLE payment_fee_rates DROP CONSTRAINT payment_fee_rates_method_chk;
ALTER TABLE payment_fee_rates
    ADD CONSTRAINT payment_fee_rates_method_chk
    CHECK (method IN ('fpx', 'fpx_b2b1', 'card', 'ewallet', 'duitnow_qr'));

-- Sale snapshot additions. The existing fee_* columns stay: they are what the
-- receipt and reporting read, and they keep every past sale readable exactly as
-- it was charged.
--
--   platform_fee_sen  the platform fee ACTUALLY charged for this purchase
--                     (0 on a fee-free sale, and 0 on a legacy gross-up sale,
--                     where the charge is the gross-up component instead);
--   fee_model         which model priced the sale — 'platform' |
--                     'legacy_grossup' | '' (no fee) — so history stays
--                     readable after the legacy path retires;
--   fee_identifier    the CHIP identifier whose fee was charged (`fpx` vs
--                     `fpx_b2b1`), so the purchase can be pinned to it and the
--                     buyer cannot reach the other variant at CHIP's page.
ALTER TABLE sales ADD COLUMN platform_fee_sen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN fee_model TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN fee_identifier TEXT NOT NULL DEFAULT '';
ALTER TABLE sales
    ADD CONSTRAINT sales_platform_fee_nonneg_chk CHECK (platform_fee_sen >= 0);
ALTER TABLE sales
    ADD CONSTRAINT sales_fee_model_chk
    CHECK (fee_model IN ('', 'platform', 'legacy_grossup'));
