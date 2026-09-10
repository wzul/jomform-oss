-- Migration 000048: per-workspace realtime notification sound toggle (issue #447).
-- When true, the merchant's dashboard (e.g. a kitchen/order screen) plays an
-- audio beep when a new sale/response arrives over the SSE stream. Default
-- false (muted) so no merchant is surprised by sound.
ALTER TABLE workspace_settings ADD COLUMN IF NOT EXISTS sound_enabled BOOLEAN NOT NULL DEFAULT FALSE;
