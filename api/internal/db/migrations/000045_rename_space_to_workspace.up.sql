-- Issue #431: rename product term 'Space' → 'Workspace' (full refactor).
-- Rename tables, columns, indexes, constraints, triggers and functions.
-- Internal identifiers are renamed too (no users yet, per Wan's update).

-- 1. Rename tables.
ALTER TABLE spaces RENAME TO workspaces;
ALTER TABLE space_settings RENAME TO workspace_settings;
ALTER TABLE space_members RENAME TO workspace_members;
ALTER TABLE space_invites RENAME TO workspace_invites;
ALTER TABLE space_legal_pages RENAME TO workspace_legal_pages;
ALTER TABLE space_email_settings RENAME TO workspace_email_settings;

-- 2. Rename columns on the main tenant tables.
ALTER TABLE accounts RENAME COLUMN space_id TO workspace_id;
ALTER TABLE forms RENAME COLUMN space_id TO workspace_id;
ALTER TABLE products RENAME COLUMN space_id TO workspace_id;
ALTER TABLE sales RENAME COLUMN space_id TO workspace_id;
ALTER TABLE api_keys RENAME COLUMN space_id TO workspace_id;
ALTER TABLE users RENAME COLUMN default_space_id TO default_workspace_id;

-- 3. Rename columns on the workspace_* tables.
ALTER TABLE workspace_settings RENAME COLUMN space_id TO workspace_id;
ALTER TABLE workspace_members RENAME COLUMN space_id TO workspace_id;
ALTER TABLE workspace_invites RENAME COLUMN space_id TO workspace_id;
ALTER TABLE workspace_legal_pages RENAME COLUMN space_id TO workspace_id;
ALTER TABLE workspace_email_settings RENAME COLUMN space_id TO workspace_id;

-- 4. Rename indexes.
ALTER INDEX idx_spaces_owner RENAME TO idx_workspaces_owner;
ALTER INDEX forms_space_slug_key RENAME TO forms_workspace_slug_key;
ALTER INDEX space_legal_pages_space_idx RENAME TO workspace_legal_pages_workspace_idx;

-- 5. Recreate the forms slug-sync trigger + function with the new column names
--    (the stored function body references space_id, which is now workspace_id).
DROP TRIGGER IF EXISTS trg_forms_sync_space_id ON forms;
DROP FUNCTION IF EXISTS forms_sync_space_id();
CREATE OR REPLACE FUNCTION forms_sync_workspace_id() RETURNS trigger AS $$
BEGIN
   NEW.workspace_id := (SELECT a.workspace_id FROM accounts a WHERE a.id = NEW.account_id);
   RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_forms_sync_workspace_id
    BEFORE INSERT OR UPDATE OF account_id ON forms
    FOR EACH ROW EXECUTE FUNCTION forms_sync_workspace_id();

-- 6. Rename auto-named constraints for consistency.
ALTER TABLE workspace_members RENAME CONSTRAINT space_members_pkey TO workspace_members_pkey;
ALTER TABLE workspace_members RENAME CONSTRAINT space_members_space_id_user_id_key TO workspace_members_workspace_id_user_id_key;
ALTER TABLE workspace_invites RENAME CONSTRAINT space_invites_pkey TO workspace_invites_pkey;
ALTER TABLE workspace_invites RENAME CONSTRAINT space_invites_space_id_email_key TO workspace_invites_workspace_id_email_key;
ALTER TABLE workspace_legal_pages RENAME CONSTRAINT space_legal_pages_pkey TO workspace_legal_pages_pkey;
ALTER TABLE workspace_settings RENAME CONSTRAINT space_settings_pkey TO workspace_settings_pkey;
ALTER TABLE workspace_email_settings RENAME CONSTRAINT space_email_settings_pkey TO workspace_email_settings_pkey;
ALTER TABLE accounts RENAME CONSTRAINT accounts_space_id_fkey TO accounts_workspace_id_fkey;
ALTER TABLE forms RENAME CONSTRAINT forms_space_id_fkey TO forms_workspace_id_fkey;
ALTER TABLE products RENAME CONSTRAINT products_space_id_fkey TO products_workspace_id_fkey;
ALTER TABLE sales RENAME CONSTRAINT sales_space_id_fkey TO sales_workspace_id_fkey;
ALTER TABLE api_keys RENAME CONSTRAINT api_keys_space_id_fkey TO api_keys_workspace_id_fkey;
ALTER TABLE users RENAME CONSTRAINT users_default_space_id_fkey TO users_default_workspace_id_fkey;
