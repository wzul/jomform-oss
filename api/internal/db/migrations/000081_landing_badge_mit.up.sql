-- The landing badge seed was rewritten when the project claimed to be
-- closed-source; the project is open source under MIT again, so the seeded
-- default is restored WITH the licence name.
--
-- Only a row still holding the exact seed value is updated. An operator who has
-- customised the badge through the system-settings API keeps their wording —
-- a migration must not overwrite a live setting.
UPDATE system_settings
   SET value = '100% free · open source (MIT) · agent-native payments'
 WHERE key = 'landing_badge'
   AND value = '100% free · CHIP payments';
