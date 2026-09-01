-- Space model: tenant layer above accounts.
CREATE TABLE IF NOT EXISTS spaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    slug          TEXT UNIQUE NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_spaces_owner ON spaces(owner_user_id);

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;

-- Link accounts to spaces (1:1 for migration — each existing account becomes a space).
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS space_id UUID REFERENCES spaces(id);

-- Create a default space per existing account and link it.
INSERT INTO spaces (id, owner_user_id, name, slug)
SELECT gen_random_uuid(), a.owner_user_id, a.name, a.slug
FROM accounts a
ON CONFLICT (slug) DO NOTHING;

UPDATE accounts a
SET space_id = s.id
FROM spaces s
WHERE s.slug = a.slug AND a.space_id IS NULL;

-- Space-scoped data (existing rows migrated to their account's space)
ALTER TABLE forms ADD COLUMN IF NOT EXISTS space_id UUID REFERENCES spaces(id);
ALTER TABLE products ADD COLUMN IF NOT EXISTS space_id UUID REFERENCES spaces(id);
ALTER TABLE sales ADD COLUMN IF NOT EXISTS space_id UUID REFERENCES spaces(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS space_id UUID REFERENCES spaces(id);

UPDATE forms f SET space_id = a.space_id FROM accounts a WHERE a.id = f.account_id AND f.space_id IS NULL;
UPDATE products p SET space_id = a.space_id FROM accounts a WHERE a.id = p.account_id AND p.space_id IS NULL;
UPDATE sales s SET space_id = a.space_id FROM accounts a WHERE a.id = s.account_id AND s.space_id IS NULL;
UPDATE api_keys k SET space_id = a.space_id FROM accounts a WHERE a.id = k.account_id AND k.space_id IS NULL;

-- Space settings: storefront config (footer, PP/ToS links, menu, theme)
CREATE TABLE IF NOT EXISTS space_settings (
    space_id UUID PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
    footer_text   TEXT NOT NULL DEFAULT '',
    company_reg   TEXT NOT NULL DEFAULT '',
    privacy_url   TEXT NOT NULL DEFAULT '',
    terms_url     TEXT NOT NULL DEFAULT '',
    menu_json     JSONB NOT NULL DEFAULT '[]'
);