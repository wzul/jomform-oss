-- Fixed billing schedule option (Wan, round 13c).
--
-- Today a successful charge always schedules the next one from the moment of
-- success (nextChargeFrom(time.Now(), period)), so a late charge permanently
-- shifts every later charge. Merchants need to choose the policy per product:
--
--   'from_payment' (DEFAULT) — next = moment the charge succeeded.
--                               Current behaviour; for continuous services.
--   'catch_up'               — next = planned + 1 period, so a missed period is
--                               still collected. For fixed-term classes.
--   'skip_missed'            — next = the next planned date still in the future,
--                               so a missed period is never collected.
--
-- Default keeps every existing product bit-for-bit on today's behaviour.
ALTER TABLE products ADD COLUMN schedule_mode TEXT NOT NULL DEFAULT 'from_payment'
    CHECK (schedule_mode IN ('from_payment', 'catch_up', 'skip_missed'));
