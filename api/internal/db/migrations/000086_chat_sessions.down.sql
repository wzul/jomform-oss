-- Rollback for 000086: chat history.
--
-- A plain drop: chat_messages cascades with chat_sessions, and nothing else in
-- the schema references either table. The data lost is the conversation history
-- itself, which is the feature — a down migration cannot preserve it somewhere
-- else without inventing a second store, and the up migration is what a rollback
-- of the application image would have written anyway.
DROP INDEX IF EXISTS idx_chat_messages_session;
DROP TABLE IF EXISTS chat_messages;
DROP INDEX IF EXISTS idx_chat_sessions_scope_updated;
DROP TABLE IF EXISTS chat_sessions;
