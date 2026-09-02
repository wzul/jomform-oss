# CHIP Sandbox / Test-Mode E2E Runbook

QA runbook for the money path (form → pay → receipt → confirm) against the
**CHIP sandbox** (test mode). This is the operational companion to the
Playwright E2E scaffolding in `e2e/` and the money-invariant unit tests in
`api/internal/payments/money_test.go` and
`api/internal/dbstore/money_invariants_test.go` (issue #99).

## 1. How test mode works in JomForm

JomForm stores CHIP credentials **per account** (`accounts.chip_brand_id`,
`accounts.chip_secret_key_enc`, `accounts.chip_mode`). There is a single CHIP
API endpoint; **test vs live is decided by which secret key the merchant
pastes** (WooCommerce parity):

- `sk_test_…` → `chip_mode = 'test'`
- `sk_live_…` → `chip_mode = 'live'`

The mode label is auto-derived from the secret prefix in
`api/internal/httpapi/rest_chip.go` (`setChipCreds`). A test-mode brand/secret
pair is therefore just a normal account whose stored secret starts with
`sk_test_`. No separate "sandbox flag" is needed — the sandbox is the account's
test-mode credentials.

### Confirming a test-mode brand/secret exists for automated tests

1. Create a CHIP **test** brand in the CHIP portal (Sandbox / Test mode).
2. Copy its **Brand ID** and **Secret Key** (`sk_test_…`).
3. In JomForm, set the account's CHIP credentials to that pair (dashboard
   Settings → CHIP, or the `set_chip_credentials` MCP tool). The save is
   **validated against CHIP** (`ValidateCredentials`) before it is stored, so
   a wrong pair is rejected.
4. Confirm the account now reports `chip_mode = 'test'` (dashboard Settings →
   CHIP shows the mode badge; the MCP `get_account` returns `chip_mode`).

> **CI/staging:** export the test credentials as env vars
> `JOMFORM_E2E_CHIP_BRAND_ID` and `JOMFORM_E2E_CHIP_SECRET_KEY` and have the
> E2E setup seed an account with them (see `e2e/README.md`). Never commit a
> real secret; use a GitHub Actions secret.

## 2. Sandbox card

CHIP sandbox accepts the standard test card. Use a card number that CHIP's
sandbox recognizes (e.g. `4242 4242 4242 4242`), any future expiry, any CVC.
The exact test card is documented in the CHIP portal under Sandbox → Test
cards; if the portal differs, use the card listed there.

## 3. Running the E2E suite

Prereqs: a staging deploy of the API + web, a test-mode CHIP account seeded
with the sandbox credentials, and a way to read the receipt email (SES test
inbox / SNS subscription, or a test inbox the account's `chip_email_fallback`
points to).

```bash
cd e2e
npm ci
# Point at your staging deploy + seeded test account:
export E2E_BASE_URL=https://staging.jomform.com
export E2E_CHIP_BRAND_ID=...
export E2E_CHIP_SECRET_KEY=...
npx playwright test
```

The five scenarios (see `e2e/specs/money-path.spec.ts`):

1. **Happy path** — buyer opens a published form → fills → pays (sandbox card)
   → thank-you → receipt email sent.
2. **Merchant confirm** — merchant confirms the sale → status transitions
   `pending → paid → confirmed`.
3. **Skip-capture** — authorize → capture (full + partial) → release →
   cancelled.
4. **Bank-direct** — instructions + proof upload → confirm.
5. **Receipt content** — email + PDF carry the correct space-scoped form URL
   (#83), correct amounts, and no footer (#80).

## 4. Money invariants asserted

- `total_sen = subtotal + shipping - discount` (integer sen, no floats).
- Currency is the account currency (MYR MVP).
- `ref_code` is unique (DB UNIQUE constraint) — a payment always maps to one
  sale.
- Webhook redelivery is idempotent: the `webhook_events.dedup_key` UNIQUE
  index means a re-delivered CHIP callback is inserted once, so it never
  double-charges or double-confirms.

These are covered by unit tests that run in CI without a database
(`api/internal/payments/money_test.go`) and by DB integration tests that run
when `JOMFORM_TEST_DATABASE_URL` is set (`api/internal/dbstore/money_invariants_test.go`).

## 5. Running locally

```bash
# 1. Start local deps (Postgres, Valkey, MinIO)
make up
# 2. Run the API
make dev
# 3. Run the web app against it
cd web && npm run dev
# 4. Seed a test-mode CHIP account (dashboard or MCP), then run E2E:
cd e2e && npm ci && npx playwright test
```

## 6. Running in CI

The money-invariant unit tests run in the existing `api` CI job
(`go test ./... -race`). The DB integration tests are skipped unless
`JOMFORM_TEST_DATABASE_URL` is set; to enable them, add a Postgres service
container and set that env var in the workflow. The Playwright E2E suite runs
against a staging deploy in a separate job that sets the `E2E_*` env vars from
GitHub Actions secrets.
