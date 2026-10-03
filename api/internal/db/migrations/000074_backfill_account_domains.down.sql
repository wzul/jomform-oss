-- Down for the account_domains backfill (000074).
--
-- The up migration COPIES a row that already existed in `accounts`; it creates no
-- new claim and no new state. Reversing it means deleting exactly the rows it
-- could have produced — and only those.
--
-- `provider = 'cname'` is not sufficient on its own: an account may have added a
-- CNAME domain through the new UI (addCustomDomain also uses provider 'cname'),
-- and deleting that would destroy a claim the merchant made deliberately. The
-- predicate below therefore requires the row to MATCH the legacy columns it was
-- copied from: same domain (CITEXT compares case-insensitively, so the `=`
-- already does what the up migration's copy did) and the same verification
-- state. A row the merchant has since re-verified, re-pointed at a tunnel, or
-- edited no longer matches and is left alone.
--
-- This is deliberately not a bare `DELETE FROM account_domains WHERE provider =
-- 'cname'`. A rollback that discards a merchant's manual claim is worse than no
-- rollback at all.
DELETE FROM account_domains ad
 USING accounts a
 WHERE ad.account_id = a.id
   AND ad.domain = a.custom_domain
   AND ad.provider = 'cname'
   AND ad.verified = a.custom_domain_verified
   AND a.custom_domain IS NOT NULL
   AND a.custom_domain <> '';
