-- Rollback for 000063_feedback_threads.
--
-- Drops exactly what the up migration created, in dependency order: the
-- messages table first (it carries the FK to the threads table), then the
-- threads table with its two indexes. Nothing here is referenced by any other
-- table, so the rollback is complete and no orphan rows can be left behind.
--
-- No data is preserved, and that is correct: the up migration introduced the
-- table, so on a database that has not run it there is nothing to return to.
-- A rollback on a live database discards filed feedback — that is the nature of
-- dropping the only table holding it, and a "keep the rows" rollback would need
-- a second table the up migration never created.
DROP TABLE IF EXISTS feedback_messages;
DROP TABLE IF EXISTS feedback_threads;
