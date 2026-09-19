-- Migration 000061: suppression list admin surface.
--
-- Migration 000018 created suppressed_emails with four columns and free-text
-- reason/source values written directly by the SNS ingestion path
-- (api/internal/httpapi/sesnotif.go, reason = 'permanent bounce (Permanent)').
-- Nothing could READ the list: SuppressionStore.List/Remove had zero callers,
-- there was no REST route, no MCP tool and no screen, so a super-admin had no
-- way to see which addresses the platform had stopped mailing, or to add or
-- remove one.
--
-- This migration gives the table what an admin surface needs, without changing
-- its identity: `email` stays the CITEXT primary key, which is what makes
-- uniqueness and the pre-send equality lookup case-insensitive and indexed.
--
--   note        the human-readable detail behind a canonical reason
--               (the bounce sub-type, "repeated soft bounce (3 in 7d)", or a
--               free-text note a super-admin types when adding an address)
--   created_by  the super-admin's users.id for a manual add; NULL for
--               bounce/complaint ingestion, which has no actor
--
-- and constrains reason/source to the three canonical codes each so a filter or
-- a UI label can never be written against a value that does not exist. The
-- backfill preserves the old free-text reason in `note` before the CHECK lands,
-- so no provenance is lost.
--
-- Guard notes this file must respect (they are what make it safe on both an
-- empty database and production):
--
--   * suppressed_emails has NO `*Select` constant (selectTables in
--     api/internal/dbstore/migration_scan_drift_test.go lists only
--     accounts/forms/products/sales/workspaces/subscriptions), so there is no
--     select/scan to keep column-aligned and no `intentionallyUnselected`
--     entry to add.
--   * TestMigrationsApplyFromZero runs this on an EMPTY database, where the
--     backfill UPDATE matches nothing — it must not depend on rows existing.
--   * The CHECK is added WITH a validated existing set, so it can only fail on
--     data this migration itself normalised. Postgres validates every existing
--     row when the constraint is added, which is why the backfill runs first.
--   * On a fresh database 000018 has just created the table empty, so both
--     ADD COLUMN statements and the backfill are no-ops and the CHECK is
--     trivially satisfied. Re-running is impossible: Migrate records the file
--     in schema_migrations.

-- 1. The two new columns.
ALTER TABLE suppressed_emails ADD COLUMN IF NOT EXISTS note       TEXT NOT NULL DEFAULT '';
ALTER TABLE suppressed_emails ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;

-- 2. Normalise the free-text reasons already written by sesnotif.go, keeping
--    the detail in `note`. A row already carrying a canonical reason is left
--    alone (its note is whatever it already had). Source drives the mapping
--    because it is the part of the old write path that was already an enum:
--    'bounce' → hard_bounce, 'complaint' → complaint, anything else → manual.
UPDATE suppressed_emails
   SET note   = reason,
       reason = CASE source WHEN 'bounce' THEN 'hard_bounce'
                            WHEN 'complaint' THEN 'complaint'
                            ELSE 'manual' END
 WHERE reason NOT IN ('hard_bounce','complaint','manual');

-- 3. Freeze the two enums. reason/source are now the canonical codes the REST
--    layer, the MCP tools and the dashboard all branch on.
ALTER TABLE suppressed_emails
  ADD CONSTRAINT suppressed_emails_reason_chk
  CHECK (reason IN ('hard_bounce','complaint','manual')),
  ADD CONSTRAINT suppressed_emails_source_chk
  CHECK (source IN ('bounce','complaint','manual'));

-- 4. The admin list is newest-first and the search filters with ILIKE, so the
--    ordering column needs an index of its own (the CITEXT PK covers the
--    equality lookup only).
CREATE INDEX IF NOT EXISTS idx_suppressed_emails_created_at
  ON suppressed_emails (created_at DESC);
