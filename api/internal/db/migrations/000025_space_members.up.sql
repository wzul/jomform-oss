-- Issue #161 (part A of #160): space_members data model + invite flow.
--
-- space_members: who belongs to a space and with what role. The owner is
-- stored as a member row with role='admin' for uniformity (backfilled below),
-- so every space has at least one admin member.
--
-- space_invites: pending invitations for people who don't yet have a JomForm
-- account. When they sign up via the emailed link (token), the invite is
-- redeemed and they are added as a member with the invited role.

CREATE TABLE space_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'read_only' CHECK (role IN ('read_only','manage','admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (space_id, user_id)
);

CREATE TABLE space_invites (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'read_only' CHECK (role IN ('read_only','manage','admin')),
    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (space_id, email)
);

-- Backfill: every existing space owner becomes an admin member so the owner
-- is always an admin (uniformity with the invite flow).
INSERT INTO space_members (space_id, user_id, role)
SELECT id, owner_user_id, 'admin' FROM spaces
ON CONFLICT (space_id, user_id) DO NOTHING;
