# SES Email Reputation Hardening (issue #56) — Runbook

This covers the SES bounce/complaint suppression, the verification circuit
breaker, the MX pre-check, and the SNS feedback endpoint added to protect the
SES identity from a targeted bounce attack. An attacker registers with fake
addresses, each firing a verification email that hard-bounces; without
protection this can take SES past the ≥5% review / ≥10% pause thresholds and
silence ALL transactional email (receipts too), because receipts and
verification share one SES identity. These three layers kill that attack.

## 1. Suppression list (`suppressed_emails`)

SES publishes `Bounce` and `Complaint` events to a configuration set → SNS
topic → `POST /v1/internal/ses/notifications`. On a **permanent** bounce or a
**complaint**, the address is upserted into `suppressed_emails` and its user
row (if any) is flagged `verified=false`, so the account cannot log in.

**Every outbound send consults the list first** — verification emails AND
receipts — so a suppressed address never gets another message. See §1.1 for
where that check lives and what it does when it cannot reach the database.

The list is managed by the platform owner on the `/admin` page (**Email
suppression list** section) and over MCP (`admin_list_suppressions`,
`admin_add_suppression`, `admin_remove_suppression`), backed by
`GET`/`POST /v1/admin/suppressions` and `DELETE /v1/admin/suppressions/{email}`
(issue #601). The same story is written for readers in
`web/src/app/docs/ses-reputation` — the dashboard links to it from that
section's inline "?".

The list is managed by the platform owner on the `/admin` page (**Email
suppression list** section) and over MCP (`admin_list_suppressions`,
`admin_add_suppression`, `admin_remove_suppression`), backed by
`GET`/`POST /v1/admin/suppressions` and `DELETE /v1/admin/suppressions/{email}`
(issue #601). The same story is written for readers in
`web/src/app/docs/ses-reputation` — the dashboard links to it from that
section's inline "?".

| column | meaning |
|---|---|
| `email` | lower-cased address (CITEXT PK — uniqueness and the equality lookup are case-insensitive) |
| `reason` | `hard_bounce` / `complaint` / `manual` (CHECK, migration 000061) |
| `source` | `bounce` / `complaint` / `manual` (CHECK, migration 000061) |
| `note` | the detail behind the reason: the SES bounce sub-type, `repeated soft bounce (n in 7d)`, or the operator's free text on a manual add |
| `created_by` | the super-admin's `users.id` for a manual add; NULL for bounce/complaint ingestion |
| `created_at` | when suppressed |

**Reason codes.** `hard_bounce` is a permanent bounce (including
`Suppressed` and `OnAccountSuppressionList`, which SES accepts without
attempting delivery and which therefore only burn quota), or an address that
produced ≥3 `Transient` bounces in a rolling 7 days. `complaint` is a spam
complaint. `manual` is an address the platform owner added by hand.

Transient (`Transient`/`Undeliverable`) bounces are NOT suppressed on the first
event (they are not reputation-damaging and a mailbox may recover), but they are
still counted towards the breaker so an anomalous transient-bounce storm is
visible. **Soft-bounce rule:** the third `Transient` bounce for the same address
inside a rolling 7 days suppresses it, because at that point the mailbox is
effectively dead and continuing to mail it only spends quota and damages the
sending identity. The tally is counted from `email_delivery_events` and includes
the event that triggered the check, so the `note`
(`repeated soft bounce (3 in 7d)`) is a number an operator can reproduce with a
`SELECT`. Event addresses are stored lower-cased so one mailbox cannot be tallied
under two spellings.

Migration 000061 replaced the free-text reasons this endpoint used to
write (`permanent bounce (Permanent)`) with the three codes above, moving the old
text into `note` so no provenance was lost.

### 1.1 The pre-send choke point

The check is **not** left to each caller to remember. `(*email.Sender).SendReceipt`
and `SendReceiptWithAttachment` — the only two `sesv2.SendEmail` calls in the
codebase — consult the list before handing anything to SES. Every outbound
message reaches SES through one of them, so a suppressed address never gets
another message whichever path produced it: buyer receipts, merchant
notifications, per-form notifications, verification, 2FA codes, magic links,
password resets and workspace invites, whether sent inline in a request handler
or by the background email worker.

- A suppressed recipient is **skipped**: the guard logs one
  `email_suppressed_skip` line (recipient obfuscated, subject) and returns
  `ErrSuppressed`. Every caller maps that to a logged no-op — it is never an
  error response, never a queue retry, never a dead-letter entry, because a
  retry to a suppressed address can never succeed. The worker logs
  `emailq_skip_suppressed`.
- A **lookup failure fails open**: it is logged as
  `email_suppression_check_error` and the send proceeds. A database blip must
  not strand a paying buyer's receipt or a registering user's verify link on a
  healthy address. (`ShouldSuppress` itself still surfaces the error; the
  fail-open decision belongs to the sender's guard.)
- **The merchant is not exempt.** The merchant notification is suppressed like
  any other mail. A merchant whose mailbox hard-bounces would otherwise keep
  generating bounces from the same sending identity that delivers buyer
  receipts, so one bad mailbox would drag down deliverability for every buyer. A
  merchant who stops receiving notifications changes the address in workspace
  email settings.

Individual senders keep their own pre-render checks as an early exit (they avoid
building a link or rendering a receipt for a message that will not be sent), but
those are no longer load-bearing: the choke point is what guarantees the rule.

**The choke point only sees mail the worker actually routes.** With Valkey up,
most mail is enqueued and delivered by the background email worker, so the
dispatcher (`(*EmailDispatcher).Send`) stands in front of the guard: a job type
with no case there is discarded before any sender is reached, and such a message
is neither delivered nor suppression-checked. That was live — `form_notification`
and `connect` were both enqueued and both unrouted — so the unknown-type branch
now returns an error instead of a logged no-op (the job retries, then
dead-letters where an operator can see and replay it), and
`api/internal/httpapi/email_dispatcher_jobtype_test.go` fails the build when any
enqueued `Type:` literal has no matching case. When adding a new queued job type,
add its case in the same change.

### Inspect / un-suppress a false positive (ops)

```sql
-- list suppressions, newest first
SELECT email, reason, source, note, created_by, created_at
FROM suppressed_emails ORDER BY created_at DESC LIMIT 100;

-- does a specific address have a suppression?
SELECT * FROM suppressed_emails WHERE email = 'person@example.com';

-- un-suppress a false positive (restore ability to email them)
DELETE FROM suppressed_emails WHERE email = 'person@example.com';

-- (the address's user row, if a person complains on the account they still own,
--  stays verified=false until they re-verify via a /verify link)
```

The dashboard/MCP path is preferred where an operator is involved: it writes an
`admin.super_admin_action` audit row, which a psql session does not.

## 2. Verification circuit breaker (`jf:bnc:*` in Valkey)

Verification sends and bounces are counted in hourly Valkey buckets
(`jf:bnc:send:{hour}`, `jf:bnc:bounce:{hour}`, 2h TTL). The breaker reads the
current + previous hour buckets: when the bounce ratio exceeds **5%** (default)
**and** there were at least **20** sends, verification mail is paused —
register returns `503 "banyak emel tidak dihantar, sila cuba lagi nanti"`.
Because every bucket expires after 2h, a quiet hour naturally drops the ratio
back to zero — the breaker **auto-resets after ~1 quiet hour**. No manual
intervention needed. Receipts are NOT gated by the breaker.

Thresholds are env-configurable (see Env Vars). On a Valkey read error the
breaker **fails open** (sends allowed) so a cache outage never permanently
silences a healthy identity's verification flow.

## 3. MX pre-check

Before a verification email is sent, the recipient domain's MX records are
resolved (`net.LookupMX`). A domain with no MX accepts no mail → registration is
rejected with `400 "domain emel tidak menerima emel"` **before the user row is
created**. Results are cached in Valkey (`jf:mx:{sha256(domain)}`): **positive
24h**, **negative 1h** (resilient to DNS flapping). A DNS error fails closed
(rejected) as the conservative reputation posture.

## 4. Environment variables (SES / SNS wiring)

| var | default | purpose |
|---|---|---|
| `SES_SMTP_HOST` | (empty = sends disabled) | gateway host; required for email |
| `SES_SMTP_USER` / `SES_SMTP_PASS` | | IAM access key id / secret key (SESv2 API) |
| `SES_FROM` | | verified sender, e.g. `noreply@jomform.com` |
| `SES_REPLY_TO` | (empty = no header) | **monitored** mailbox a recipient's Reply reaches, comma-separated. Set the Reply-To header on every message the SES sender emits (both the Simple and the raw-MIME path), so a reply is not lost at `SES_FROM`, which is `noreply@…` and has no reader. Leave unset only if a reply genuinely has nowhere to go |
| `SES_REGION` | `ap-southeast-1` | AWS region |
| `SES_BREAKER_MIN_SENDS` | `20` | min verification sends in the trailing hour before the ratio can trip the breaker |
| `SES_BREAKER_RATIO` | `0.05` | bounce ratio above which verification mail is paused |
| `SES_CONFIG_SET` | — | SES configuration-set name that emits Bounce/Complaint to SNS (documentation only) |
| `SES_SNS_NOTIF_ENABLED` | `false` | set `true` on the deployment that receives SNS so the `/v1/internal/ses/notifications` endpoint is registered |

AWS side (out of repo): create a configuration set, an SNS topic, subscribe the
API's HTTPS endpoint with subscription confirmation accepted, and connect the
configuration set's `Bounce` and `Complaint` event types to the topic. SES will
sign every SNS message; the API verifies the signature (RSASSA-PKCS1-v1_5
SHA-1, certificate fetched from the AWS-hosted `SigningCertURL`) before
recording anything — **no unauthenticated write**.
