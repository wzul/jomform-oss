-- Restore the value this migration's up half replaced, and only that value:
-- a badge an operator has since customised is left alone.
UPDATE system_settings
   SET value = '100% free · CHIP payments'
 WHERE key = 'landing_badge'
   AND value = '100% free · open source (MIT) · agent-native payments';
