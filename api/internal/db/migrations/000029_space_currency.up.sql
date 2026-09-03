-- Issue #185: per-space currency (default MYR) + neutral minor-unit amounts.
--
-- Currency is a per-space setting (space_settings.currency, default MYR). The
-- space's account(s) inherit it for CHIP purchases. New amounts are stored in
-- the space's currency minor units. Historical sales keep their original
-- currency (legal/accounting requirement — refunds must be in the original
-- currency), so sales.currency records the currency at transaction time.
-- products.currency records the currency a product's price is denominated in
-- (a currency change applies to new products going forward).
ALTER TABLE space_settings
    ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT 'MYR'
    CHECK (currency IN ('MYR','USD','SGD','EUR','GBP','AUD','CNY','JPY','THB','IDR','PHP','HKD','KRW','INR','AED','SAR','NZD','CAD','CHF','VND','BND','TWD','MMK','KHR','NPR','PKR','EGP'));

-- Record the currency each sale was transacted in (default MYR for legacy rows,
-- which were all MYR/sen). Never auto-converted on a later currency change.
ALTER TABLE sales
    ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT 'MYR'
    CHECK (currency IN ('MYR','USD','SGD','EUR','GBP','AUD','CNY','JPY','THB','IDR','PHP','HKD','KRW','INR','AED','SAR','NZD','CAD','CHF','VND','BND','TWD','MMK','KHR','NPR','PKR','EGP'));

-- Record the currency a product's price is denominated in (default MYR for
-- legacy rows). A space currency change applies to new products going forward.
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT 'MYR'
    CHECK (currency IN ('MYR','USD','SGD','EUR','GBP','AUD','CNY','JPY','THB','IDR','PHP','HKD','KRW','INR','AED','SAR','NZD','CAD','CHF','VND','BND','TWD','MMK','KHR','NPR','PKR','EGP'));
