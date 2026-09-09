-- Migration 000041: media scan reason (issue #364).
--
-- Adds a scan_reason column so ops can see WHY a code asset was flagged /
-- quarantined (deterministic marker(s) or the AI judge's verdict). Only code
-- assets (CSS/JS/HTML/SVG) carry a meaningful scan path; images stay 'pending'
-- or 'ok' with no reason.
--
-- media is a NEW table (not a *Select-backed table) so the migration
-- scan-drift guard is unaffected; we update the Media struct + scan sites in
-- the same PR for column alignment.

ALTER TABLE media ADD COLUMN IF NOT EXISTS scan_reason TEXT NOT NULL DEFAULT '';
