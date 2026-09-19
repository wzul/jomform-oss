-- Rollback for 000062_workspace_spam_judge_enabled.
--
-- Drops exactly what the up migration added: the per-workspace AI spam-judge
-- toggle column. No data is migrated or transformed on the way in, so there is
-- nothing to restore on the way out — the judge returns to running for every
-- workspace, which is the behaviour that existed before 000062.
ALTER TABLE workspace_settings DROP COLUMN IF EXISTS spam_judge_enabled;
