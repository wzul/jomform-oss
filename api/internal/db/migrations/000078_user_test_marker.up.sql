-- Test-account marker (card t_6ae7f586).
--
-- WHY. Between 26 Sep and 13 Oct a QA lane registered a fresh account on
-- PRODUCTION for every card that verified the signup/onboarding flow, and
-- nothing could remove them afterwards: 20 verified accounts (24 accounts and
-- 11 workspaces behind them) accumulated as 56% of all users. Nothing in the
-- schema distinguished them from a merchant's account — the only mark was the
-- shape of the email address, read by eye — and `purge_unverified_accounts`
-- removes only UNVERIFIED rows, so every one of them was out of its reach.
-- Removing them needed hand-written SQL with a pg_dump in front of it.
--
-- `is_test` is written AT INSERT TIME by the QA signup channel (a server-held
-- token, never a request field a normal visitor can set), not by the card that
-- created the account. That is the point: a card that fails mid-verification,
-- is killed, or is superseded must still leave an account the system can find
-- and remove, so "the lane deletes it at the end" is not the design.
--
-- The marker is deliberately NOT part of any scan or the public API: `users`
-- has no *Select constant, and every user query lists its columns explicitly,
-- so this column is invisible unless a dedicated query asks for it — which only
-- the test-account purge does.
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_test BOOLEAN NOT NULL DEFAULT false;

-- Partial index: the purge looks only for marked rows, and the table is
-- overwhelmingly unmarked.
CREATE INDEX IF NOT EXISTS idx_users_is_test ON users (is_test) WHERE is_test;
