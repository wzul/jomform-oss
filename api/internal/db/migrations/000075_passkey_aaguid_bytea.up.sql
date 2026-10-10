-- passkeys.aaguid was declared TEXT, but a WebAuthn AAGUID is 16 raw BYTES and
-- the handler wrote it with string(c.Authenticator.AAGUID) — the raw bytes
-- reinterpreted as a Go string, then sent as text. Postgres validates text as
-- UTF-8, and EVERY real authenticator's AAGUID contains a byte >= 0x80, so every
-- real registration died on the INSERT:
--
--     ERROR: invalid byte sequence for encoding "UTF8": 0xad (SQLSTATE 22021)
--
-- (Measured against a live Postgres: Chrome's adce0002-… fails on 0xad, Google
-- Password Manager's 9ddd1817-… on 0x9d, Windows Hello's 08987058-… on 0x98,
-- and even the all-zero AAGUID the spec defines for none-attestation fails on
-- 0x00. Only an all-ASCII AAGUID — i.e. a test virtual authenticator — could be
-- stored, which is why this stayed hidden.)
--
-- The handler turned that INSERT error into a 500, after the browser ceremony
-- had already succeeded, so the user saw a passkey prompt, approved it with
-- their fingerprint, and the passkey was never saved.
--
-- The value is a byte sequence, so BYTEA is the type it should have had. This
-- also matters beyond storage: AAGUID is what identifies the authenticator's
-- make and model, which the WebAuthn metadata service and the library's
-- AAGUID filtering are both keyed on.
--
-- Existing rows are converted with convert_to(...,'UTF8'), which round-trips
-- exactly: a row can only exist if its bytes were already valid UTF-8, because
-- anything else could never have been written.
ALTER TABLE passkeys ALTER COLUMN aaguid DROP DEFAULT;
ALTER TABLE passkeys ALTER COLUMN aaguid TYPE BYTEA USING convert_to(aaguid, 'UTF8');
ALTER TABLE passkeys ALTER COLUMN aaguid SET DEFAULT '\x'::bytea;
