-- Issue #158 down: remove the backfilled space-owned accounts. Only removes
-- accounts that were created purely to give a space an owner (i.e. accounts
-- linked to a space that carry no products, sales, forms or API keys). This
-- is a best-effort rollback; accounts with real data are left untouched.

DELETE FROM accounts a
WHERE a.space_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM products p WHERE p.account_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM sales s WHERE s.account_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM forms f WHERE f.account_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM api_keys k WHERE k.account_id = a.id);
