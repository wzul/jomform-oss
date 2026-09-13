-- Migration 000053: TOTP replay protection — record the last consumed time-step
-- (issue #528).
--
-- auth.ValidateTOTPSecret is stateless: totp.ValidateCustom checks the code
-- against the current 30s step plus Skew: 1, and records nothing. The same valid
-- code therefore verifies repeatedly for the whole ~90s window, at all three
-- call sites (login 2FA, 2FA verify, and the issue #458 magic-session step-up
-- gate for sensitive actions).
--
-- The repo already implements strict single-use semantics for its emailed
-- 6-digit codes and email verification tokens, so TOTP was the one OTP path
-- without replay protection. This column lets validation reject a code whose
-- time-step was already consumed: a step is accepted only once.
--
-- 0 = no step consumed yet (and a safe default for existing rows: the current
-- TOTP step is a large positive number, so any real code advances past it and no
-- legitimate login is blocked by the upgrade).
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_last_step BIGINT NOT NULL DEFAULT 0;

-- Defence in depth for the "single-use" property at the storage layer: a
-- consumer may only ever move the recorded step FORWARD. A direct write that
-- rewinds it (which would reopen a consumed code for replay) is rejected.
ALTER TABLE users ADD CONSTRAINT users_totp_last_step_nonneg CHECK (totp_last_step >= 0);
