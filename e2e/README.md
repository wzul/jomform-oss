# Money-path E2E (issue #99)

Playwright scaffolding for the money path (form → pay → receipt → confirm)
against a staging deploy + CHIP sandbox. The full operational runbook is in
[`docs/CHIP-SANDBOX-E2E.md`](../docs/CHIP-SANDBOX-E2E.md).

## What's here

- `playwright.config.ts` — config; reads `E2E_BASE_URL`, `E2E_CHIP_BRAND_ID`,
  `E2E_CHIP_SECRET_KEY`.
- `specs/money-path.spec.ts` — the five scenarios (happy path, merchant
  confirm, skip-capture, bank-direct, receipt content). Scenarios that need
  live CHIP checkout / a test inbox are marked `test.skip` until the exact
  selectors and a test-inbox reader are wired; the happy-path skeleton is
  runnable once a published form + test-mode account exist.

## Run

```bash
npm ci
export E2E_BASE_URL=https://staging.jomform.com
export E2E_CHIP_BRAND_ID=...
export E2E_CHIP_SECRET_KEY=...
npx playwright test
```

## CI

There is no GitHub Actions CI for this repo (the workflows were removed; see
`deploy/DEPLOY.md`). The money-invariant **unit** tests run as part of the local
gate (`scripts/preflight.sh`, gate `test` → `go test ./... -race`). The DB
integration tests (`api/internal/dbstore/money_invariants_test.go`) run when
`JOMFORM_TEST_DATABASE_URL` is set — the preflight refuses to run without it,
precisely so they cannot be skipped silently. The Playwright suite here runs
against a staging deploy with the `E2E_*` env vars, and is run manually.
