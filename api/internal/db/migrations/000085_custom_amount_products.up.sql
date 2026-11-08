-- Pay-what-you-want products ("custom_amount"), for donations.
--
-- A custom-amount product has no fixed asking price: the buyer types the
-- amount, and the SERVER validates it against this product's limits before it
-- prices the sale (httpapi/custom_amount.go). price_sen keeps a DISPLAY value
-- (the suggested amount, else the floor) so every pre-existing consumer that
-- reads a product price still has one — it is never what a custom-amount sale
-- is charged.
--
-- min_price_sen is floored at 100 sen (RM1.00) for this type in the CHECK
-- below, and in the API (payments.MinCustomAmountSen). Below that the
-- acquirer's fixed per-transaction cost exceeds the donation: a 1-sen gift nets
-- the merchant nothing and the gross-up that keeps the buyer's fee on top
-- becomes nonsense. The floor is a PLATFORM constant, matched here so a direct
-- write cannot create a product the checkout refuses to price.
ALTER TABLE products ADD COLUMN min_price_sen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN max_price_sen BIGINT;
ALTER TABLE products ADD COLUMN suggested_price_sen BIGINT;

ALTER TABLE products DROP CONSTRAINT products_type_check;
ALTER TABLE products ADD CONSTRAINT products_type_check
    CHECK (type IN ('one_time','subscription','custom_amount'));

-- billing_period stays NULL for custom_amount: it is a single purchase, never a
-- recurring one. The old constraint only allowed a non-period for one_time.
ALTER TABLE products DROP CONSTRAINT products_check;
ALTER TABLE products ADD CONSTRAINT products_check
    CHECK (billing_period IN ('month','year') OR type IN ('one_time','custom_amount'));

ALTER TABLE products ADD CONSTRAINT products_amount_limits_chk CHECK (
    min_price_sen >= 0
    AND (max_price_sen IS NULL OR max_price_sen >= min_price_sen)
    AND (suggested_price_sen IS NULL OR suggested_price_sen >= 0)
    AND (type <> 'custom_amount' OR (
        min_price_sen >= 100
        AND (max_price_sen IS NULL OR max_price_sen >= min_price_sen)
        AND (suggested_price_sen IS NULL OR suggested_price_sen >= min_price_sen)
    ))
);

-- sales.sale_type must accept the type the order core records, or the INSERT
-- fails and the sale is lost. Reports and exports switch on this column, so a
-- custom-amount sale that had to be recorded as one_time would be misreported.
ALTER TABLE sales DROP CONSTRAINT sales_sale_type_check;
ALTER TABLE sales ADD CONSTRAINT sales_sale_type_check
    CHECK (sale_type IN ('one_time','subscription','custom_amount'));
