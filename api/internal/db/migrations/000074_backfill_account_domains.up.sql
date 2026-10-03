-- Migration 000074: backfill the legacy single custom domain into account_domains.
--
-- WHY. `accounts.custom_domain` (000001) and `custom_domain_verified` (000004)
-- predate `account_domains` (000007), and 000007 created the table without
-- carrying the existing rows across:
--
--   CREATE TABLE IF NOT EXISTS account_domains ( ... );
--   -- no INSERT ... SELECT FROM accounts
--
-- Both readers of the domain LIST key off the new table only
-- (rest_domains2.go's listCustomDomains -> DomainStore.ListDomains, and
-- rest_dashboard.go feeding the dashboard bundle), so a domain that exists, is
-- VERIFIED and is serving traffic is invisible in Settings. Measured on prod:
-- account `wan Store` owns `halo.example.com` in `account_domains` and
-- `jftest.example.com` in `accounts.custom_domain` (verified), and
-- Settings shows only the first. The merchant could not verify, remove, or
-- configure TLS for a domain that was live — and the same database disagreed
-- with itself about which domains the account owns.
--
-- `ON CONFLICT (domain) DO NOTHING` is MANDATORY, not defensive. `domain` is
-- CITEXT UNIQUE so that a domain claimed by one account cannot be claimed by
-- another (Wan confirmed that is the intended behaviour, and this migration does
-- not change the constraint). An account that already re-added its legacy domain
-- under the new table therefore has a DIFFERENT account_domains row and no id to
-- match on; keying the insert on account_id would either duplicate the domain and
-- abort on the unique index, or need a lookup. Keying on `domain` makes the
-- already-migrated account a no-op, and `custom_domain` is itself UNIQUE, so at
-- most one account can produce any given domain: the row that loses the conflict
-- (if any) is not a silent loss.
--
-- verified / verified_at are carried across on purpose: copying the domain but
-- defaulting `verified` to false would turn a live, verified domain into a
-- pending one, which is the same defect wearing a different hat.
--
-- IDEMPOTENT. Re-running inserts nothing new: the first pass makes `domain` a
-- conflict for every row it copied, and the `<> ''`/NULL predicates are stable.

INSERT INTO account_domains (account_id, domain, provider, verified, verified_at)
SELECT id, custom_domain, 'cname', custom_domain_verified, custom_domain_verified_at
  FROM accounts
 WHERE custom_domain IS NOT NULL
   AND custom_domain <> ''
ON CONFLICT (domain) DO NOTHING;
