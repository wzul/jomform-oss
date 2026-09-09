-- Migration 000040: media scan status (issue #361).
--
-- Adds a per-media scan_status column so a later LLM-based content scan
-- (issue #364) can flag merchant-uploaded code assets (CSS/JS/HTML) and gate
-- whether they are served. Issue #361 only introduces the column and the
-- serving-side hardening; the background scan worker that flips this field is
-- a SEPARATE follow-up (issue #364). Default 'pending' means "not scanned yet"
-- and, until the scan engine lands, has no effect on serving (all uploaded
-- media is still served as hardened static content).
--
-- media is a NEW table (not a *Select-backed table), so the migration
-- scan-drift guard is unaffected; we update the Media struct + scan sites in
-- the same PR for column alignment.

ALTER TABLE media ADD COLUMN IF NOT EXISTS scan_status TEXT NOT NULL DEFAULT 'pending';
