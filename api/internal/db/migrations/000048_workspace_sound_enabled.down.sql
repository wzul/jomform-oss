-- Migration 000048 (down): drop the per-workspace sound toggle column.
ALTER TABLE workspace_settings DROP COLUMN IF EXISTS sound_enabled;
