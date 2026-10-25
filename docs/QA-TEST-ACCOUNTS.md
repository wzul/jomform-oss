# QA test accounts — how they are marked, and how they are removed

A lane that verifies the signup / onboarding flow needs an account. On JomForm
those accounts are **marked at creation** and removed by **one admin tool**, so a
lane never has to delete anything by hand and a card that dies mid-verification
never leaves an account nobody can remove.

This document is the operator's page for that channel. The code lives in
`api/internal/httpapi/auth.go` (marking) and
`api/internal/dbstore/user_test_purge.go` (purging); the MCP tool is
`purge_test_accounts` (`api/internal/mcpsrv/purge_test_accounts_mcp.go`).

## Why the marker exists

Between 26 Sep and 13 Oct a QA lane registered a **fresh account on production**
for every card that verified signup. Twenty of them accumulated — 20 of 36 users,
with 24 accounts and 11 workspaces behind them — and nothing could remove them:

* `purge_unverified_accounts` deletes only accounts that never confirmed their
  email. Every one of these was **verified**.
* No admin route and no MCP tool could delete an account at all.
* Nothing in the schema said "this is a test". The only mark was the shape of the
  email address, which only a human reading it could recognise.

Removing them needed hand-written SQL with a `pg_dump` in front and a dry-run
transaction first. **That hand path must never be needed again**, so:

1. the account carries a persisted marker (`users.is_test`), written by the
   system at INSERT time, and
2. an admin tool removes every marked account, whatever its age, whoever
   created it, and whether or not the card that created it ever finished.

The second point is the reason "the lane deletes it at the end" was rejected: a
card that fails, is killed or is superseded is the **common** path, and it would
leave behind exactly the account this exists to remove.

## Marking a test account (QA lanes)

Registration is unchanged except for one optional header, and the header is only
honoured when its value equals a **server-held token**:

```bash
curl -sS -X POST "$BASE_URL/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -H "X-JomForm-QA-Signup: $JOMFORM_QA_SIGNUP_TOKEN" \
  -d '{"email":"jfqa-'"$RANDOM"'@example.com","password":"correct-horse-battery"}'
```

* The token comes from the environment variable `JOMFORM_QA_SIGNUP_TOKEN` on the
  server. It is **not** a request field, so an ordinary visitor cannot mark their
  own account — a body field like `"is_test": true` does nothing.
* **Unset token = the channel is closed.** The header is ignored and every
  registration is an ordinary one. Staging sets it; **production deliberately
  does not** (`deploy/vps/compose.prod.yml` has no such variable), because lanes
  verify signup on staging and never register on prod.
* A wrong or missing token that still carries the header is treated as an
  ordinary registration (not marked), so the header cannot even be used to probe
  the secret.
* The comparison is constant-time.

### The protected address

`wan@jomform.com` is the ONE designated test login that lanes **reuse**. It is
`verified=false` on production, so it is a candidate of the unverified sweep — and
it must survive every purge:

* both purges (unverified sweep and test-account purge) exclude it, at the
  candidate list **and** again inside the per-user transaction;
* the marking channel refuses it: registering that address with the QA token
  answers `400`, and the store refuses to write `is_test` on it even if a caller
  skips the handler.

The list is `dbstore.ProtectedAccountEmails`. It is a code constant on purpose:
a configuration value that a caller or a deploy could empty would turn a
safeguard into a switch.

## Removing test accounts (one command)

`purge_test_accounts` is a super-admin MCP tool. Every call is audited.

**Dry run — the default.** Reports exactly what would be removed and commits
nothing:

```json
{ "name": "purge_test_accounts", "arguments": {} }
```

**The destructive form** — requires the explicit switch:

```json
{ "name": "purge_test_accounts", "arguments": { "confirm": true } }
```

**One account** (also a dry run unless `confirm` is set). Asking about an account
that is not marked is **refused**, not reported as "0 removed":

```json
{ "name": "purge_test_accounts", "arguments": { "email": "jfqa@example.com" } }
```

### The report names what happened

```json
{
  "dry_run": true,
  "removed_emails": ["jfqa@example.com"],
  "removed": [
    { "id": "9f0c…", "email": "jfqa@example.com", "verified": true, "created_at": "2026-10-13T04:11:09Z" }
  ],
  "removed_count": 1,
  "blocked": [],
  "blocked_count": 0,
  "scanned": 1,
  "note": "1 akaun ujian AKAN dipadam, 0 TERSEKAT …, 1 diimbas. …"
}
```

`blocked` names the accounts the tool **refused**, with a reason:
`sales`, `subscriptions` or `fk_restricted` (some other row still references the
account). A blocked account is left exactly as it was — merchant data is never
destroyed by this tool.

### Guarantees

| Guard | Behaviour |
|---|---|
| Marker only | Only `users.is_test = true` rows are candidates. An unmarked account with the same email shape is never touched. |
| Not marked → refused | `email` pointing at an unmarked account returns an error; nothing is deleted. |
| Sales / subscriptions | The account is skipped, named in `blocked`, and its data is untouched (`sales.account_id` and `subscriptions.account_id` are `ON DELETE RESTRICT`). |
| Protected address | Never removed by any purge, even if someone marks it. |
| Dry run | Runs the real delete in a transaction that is always rolled back, so "would remove" and "removed" cannot disagree. |
| Idempotent | A second run finds nothing and returns an empty report — safe to run on a schedule or after every card. |
| No age cutoff | An account is removable because it is marked, not because it is old. |
| No creator filter | Works for an account created by any lane, any tool, any card — including cards that never finished. |

## What a lane should do

1. Verify signup/onboarding on **staging** (never on production).
2. Register with the QA header so the account is marked.
3. Do not delete anything yourself — the end-of-card delete is exactly the design
   that failed. Run the dry run at the start of the next QA card to confirm the
   backlog, then `confirm: true` to clear it, or let the operator do it.
