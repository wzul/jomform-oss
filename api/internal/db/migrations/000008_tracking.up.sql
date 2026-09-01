-- Migration 000008: tracking scripts (global + per-form) — sanitized JS only
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS tracking_head TEXT NOT NULL DEFAULT '';

ALTER TABLE forms
    ADD COLUMN IF NOT EXISTS tracking_head TEXT NOT NULL DEFAULT '';