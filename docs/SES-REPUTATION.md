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
receipts — so a suppressed address never gets another message.

| column | meaning |
|---|---|
| `email` | lower-cased address (PK) |
| `reason` | `permanent bounce (...)` / `complaint` |
| `source` | `bounce` / `complaint` / `manual` |
| `created_at` | when suppressed |

Transient (`Transient`/`Undeliverable`) bounces are NOT suppressed (they are
not reputation-damaging and a mailbox may recover), but they are still counted
towards the breaker so an anomalous transient-bounce storm is visible.

### Inspect / un-suppress a false positive (ops)

```sql
-- list suppressions, newest first
SELECT email, reason, source, created_at
FROM suppressed_emails ORDER BY created_at DESC LIMIT 100;

-- does a specific address have a suppression?
SELECT * FROM suppressed_emails WHERE email = 'person@example.com';

-- un-suppress a false positive (restore ability to email them)
DELETE FROM suppressed_emails WHERE email = 'person@example.com';

-- (the address's user row, if a person complains on the account they still own,
--  stays verified=false until they re-verify via a /verify link)
```

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
