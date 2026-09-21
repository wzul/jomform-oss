-- Rollback for 000067: chat write proposals.
-- Dropping the table discards pending proposals only: a proposal is a token
-- bound to arguments, and the arguments were never applied. No user data and no
-- domain row depends on this table, so the down migration is a plain drop.
DROP INDEX IF EXISTS idx_chat_write_proposals_expires;
DROP INDEX IF EXISTS idx_chat_write_proposals_user_created;
DROP TABLE IF EXISTS chat_write_proposals;
