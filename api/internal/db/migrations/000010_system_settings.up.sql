CREATE TABLE IF NOT EXISTS system_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
INSERT INTO system_settings (key, value) VALUES
  ('site_footer_text', 'Dikuasakan oleh JomForm'),
  ('landing_badge', '100% free · CHIP payments')
ON CONFLICT (key) DO NOTHING;