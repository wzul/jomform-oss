-- Issue #431 (down): reverse the Space → Workspace rename.
-- Restore the original table/column/index/constraint/trigger/function names.

-- 1. Rename tables back.
ALTER TABLE workspaces RENAME TO spaces;
ALTER TABLE workspace_settings RENAME TO space_settings;
ALTER TABLE workspace_members RENAME TO space_members;
ALTER TABLE workspace_invites RENAME TO space_invites;
ALTER TABLE workspace_legal_pages RENAME TO space_legal_pages;
ALTER TABLE workspace_email_settings RENAME TO space_email_settings;

-- 2. Rename columns back on the main tenant tables.
ALTER TABLE accounts RENAME COLUMN workspace_id TO space_id;
ALTER TABLE forms RENAME COLUMN workspace_id TO space_id;
ALTER TABLE products RENAME COLUMN workspace_id TO space_id;
ALTER TABLE sales RENAME COLUMN workspace_id TO space_id;
ALTER TABLE api_keys RENAME COLUMN workspace_id TO space_id;
ALTER TABLE users RENAME COLUMN default_workspace_id TO default_space_id;

-- 3. Rename columns back on the space_* tables.
ALTER TABLE space_settings RENAME COLUMN workspace_id TO space_id;
ALTER TABLE space_members RENAME COLUMN workspace_id TO space_id;
ALTER TABLE space_invites RENAME COLUMN workspace_id TO space_id;
ALTER TABLE space_legal_pages RENAME COLUMN workspace_id TO space_id;
ALTER TABLE space_email_settings RENAME COLUMN workspace_id TO space_id;

-- 4. Rename indexes back.
ALTER INDEX idx_workspaces_owner RENAME TO idx_spaces_owner;
ALTER INDEX forms_workspace_slug_key RENAME TO forms_space_slug_key;
ALTER INDEX workspace_legal_pages_workspace_idx RENAME TO space_legal_pages_space_idx;

-- 5. Recreate the forms slug-sync trigger + function with the original names.
DROP TRIGGER IF EXISTS trg_forms_sync_workspace_id ON forms;
DROP FUNCTION IF EXISTS forms_sync_workspace_id();
CREATE OR REPLACE FUNCTION forms_sync_space_id() RETURNS trigger AS $$
BEGIN
   NEW.space_id := (SELECT a.space_id FROM accounts a WHERE a.id = NEW.account_id);
   RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_forms_sync_space_id
    BEFORE INSERT OR UPDATE OF account_id ON forms
    FOR EACH ROW EXECUTE FUNCTION forms_sync_space_id();

-- 6. Rename constraints back.
ALTER TABLE space_members RENAME CONSTRAINT workspace_members_pkey TO space_members_pkey;
ALTER TABLE space_members RENAME CONSTRAINT workspace_members_workspace_id_user_id_key TO space_members_space_id_user_id_key;
ALTER TABLE space_invites RENAME CONSTRAINT workspace_invites_pkey TO space_invites_pkey;
ALTER TABLE space_invites RENAME CONSTRAINT workspace_invites_workspace_id_email_key TO space_invites_space_id_email_key;
ALTER TABLE space_legal_pages RENAME CONSTRAINT workspace_legal_pages_pkey TO space_legal_pages_pkey;
ALTER TABLE space_settings RENAME CONSTRAINT workspace_settings_pkey TO space_settings_pkey;
ALTER TABLE space_email_settings RENAME CONSTRAINT workspace_email_settings_pkey TO space_email_settings_pkey;
ALTER TABLE accounts RENAME CONSTRAINT accounts_workspace_id_fkey TO accounts_space_id_fkey;
ALTER TABLE forms RENAME CONSTRAINT forms_workspace_id_fkey TO forms_space_id_fkey;
ALTER TABLE products RENAME CONSTRAINT products_workspace_id_fkey TO products_space_id_fkey;
ALTER TABLE sales RENAME CONSTRAINT sales_workspace_id_fkey TO sales_space_id_fkey;
ALTER TABLE api_keys RENAME CONSTRAINT api_keys_workspace_id_fkey TO api_keys_space_id_fkey;
ALTER TABLE users RENAME CONSTRAINT users_default_workspace_id_fkey TO users_default_space_id_fkey;
