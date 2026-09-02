# JomForm Load / Performance Baseline (issue #103)

This is the load-test runbook and recorded baseline for JomForm. It covers the
three load surfaces from issue #103 — public form (buyer path), dashboard
(authed REST), and payment create-purchase — plus the regression gate and the
first-bottleneck analysis.

## Tooling

- **Driver:** [k6](https://k6.io) (self-hosted, no SaaS dependency).
- **Scripts:** `loadtest/*.js` (see below).
- **Mock CHIP:** `loadtest/mock-chip` — a minimal CHIP Collect API stub so the
  payment path can be load-tested without hitting the live CHIP sandbox.

### Scripts

| Script | Surface | Endpoints exercised |
|---|---|---|
| `loadtest/public-form.js` | Public form (buyer path) | `GET /{slug}` (SSR render), `POST /{slug}/order` (submit) |
| `loadtest/dashboard.js` | Dashboard (authed) | `GET /v1/rest/dashboard-bundle`, `/v1/rest/sales`, `/v1/rest/analytics/forms`, `/v1/rest/products` |
| `loadtest/payment.js` | Payment create-purchase | `POST /{slug}/order` with `payment_method=chip` (→ CHIP create-purchase) |
| `loadtest/gate.js` | Regression gate | Public-form render + submit at fixed concurrency, fails on threshold breach |
| `loadtest/common.js` | Shared helpers | Env parsing, URL builders, auth headers |

### Env knobs (all scripts)

| Env | Default | Purpose |
|---|---|---|
| `BASE_URL` | `http://localhost:8080` | Target base URL |
| `VUS` | `50` | Virtual users |
| `DURATION` | `30s` | Steady-state duration |
| `RAMP` | `10s` | Ramp-up duration |
| `FORM_SLUG` / `SPACE` | `demo` / `demo` | Public form slug + space subdomain |
| `API_KEY` | (empty) | Bearer API key for authed endpoints |
| `THRESH_P95` | `500` | p95 latency threshold (ms) |
| `THRESH_ERR` | `0.01` | Error-rate threshold (fraction) |

## Running a baseline sweep

The buyer path is the highest-traffic surface, so the baseline sweep targets
it. Run against a **prod-equivalent stack** (staging with the same DB pool,
Valkey, and Next proxy hop as production).

```bash
# 1. Start the mock CHIP server (payment path only)
go run ./loadtest/mock-chip

# 2. Point the API at the mock CHIP and run the sweep
CHIP_API_BASE_URL=http://localhost:8090 \
  ./loadtest/baseline.sh http://staging.example.com
```

`baseline.sh` sweeps `VUS_LIST` (default `10 50 100 200`) and writes a
machine-readable k6 summary per level to `loadtest/results/`. Record the
RPS / p50 / p95 / p99 / error-rate figures into the table below.

### Recorded baseline (fill in from a prod-equivalent run)

> **Status:** methodology + gate committed; **numbers pending a run against
> the prod-equivalent stack.** The table below is the template to fill in.
> Do not treat the placeholders as measured capacity.

| VUs | RPS | p50 (ms) | p95 (ms) | p99 (ms) | Error rate |
|---|---|---|---|---|---|
| 10  | —   | —        | —        | —        | —          |
| 50  | —   | —        | —        | —        | —          |
| 100 | —   | —        | —        | —        | —          |
| 200 | —   | —        | —        | —        | —          |

### Gate thresholds (from issue #103)

The regression gate fails when, at 50 VUs on the buyer path:

- **p95 latency > 500 ms**, or
- **error rate > 1%**.

These are the defaults in `loadtest/gate.js` (`THRESH_P95=500`,
`THRESH_ERR=0.01`). Adjust only after a measured baseline shows the current
stack comfortably under them.

## CI / on-demand load gate

The gate runs the public-form scenario at a fixed concurrency and fails the
run (k6 exits non-zero) if p95 latency or error rate exceeds the threshold.
It is wired as a **workflow_dispatch** job in
`.github/workflows/load-gate.yml` so it can be triggered on demand against a
staging URL, and can be added to the PR CI once a staging endpoint is
available.

```bash
# On-demand gate (CI or local)
k6 run -e VUS=50 -e DURATION=30s -e THRESH_P95=500 -e THRESH_ERR=0.01 \
  loadtest/gate.js
```

## First-bottleneck analysis

Based on a code-level review of the request paths (the measured numbers above
will confirm), the **first bottleneck is the Postgres connection pool on the
buyer submit path**, followed by the CHIP create-purchase round-trip.

### Why the DB pool is the first bottleneck

`POST /{slug}/order` (`api/internal/httpapi/public.go:handleSubmit`) performs
**four sequential DB round-trips** before it can respond:

1. `PublishedBySpaceSlug` — resolve the published form (1 query).
2. `Accounts.ByID` — validate the account (1 query).
3. `Products.DecrementStock` — atomic stock decrement per item (1 query).
4. `Sales.Create` — insert the pending sale (1 query).

Each is a separate connection from the pool. Under concurrency the pool
(`pgx` default) becomes the serialization point: as VUs rise, requests queue
on pool acquisition before any of the four queries run. This is the classic
"DB pool exhaustion" signature — latency climbs steeply past the pool's
`MaxConns` while CPU stays low.

### Secondary: CHIP create-purchase latency

For `payment_method=chip`, `handleSubmit` then calls
`CreateChipPurchaseForSale` (`api/internal/payments/purchases.go`), which makes
a **synchronous HTTP call to the CHIP API** (`POST /purchases/`) before
responding. This adds the CHIP round-trip latency to every chip submit and
holds a DB connection (via `AttachChipPurchase`) for the duration. In a load
test against the real sandbox this dominates p95; the mock (`mock-chip`)
isolates JomForm's own capacity from CHIP's.

### Other surfaces

- **Dashboard bundle** (`GET /v1/rest/dashboard-bundle`) fans out to products,
  sales rollup, analytics and settings sections in one round-trip (issue #13/#29).
  It is authed and account-scoped, so it is bounded by the same DB pool; the
  bundle was already built to collapse the old 6–10 request waterfall.
- **Next proxy hop** (`web/src/app/api/[...path]/route.ts`) adds one hop for
  dashboard `/api/*` → Go `/v1/*`. It is a thin pass-through; not expected to
  be the first bottleneck, but it does add a fixed per-request overhead.

### Recommended next step

Confirm with the measured baseline: if p95 at 50 VUs is already near/over the
gate, the first fix is to raise the DB pool `MaxConns` (or add a read replica
for the render path) and/or batch the submit-path queries into a single
transaction. Re-run the sweep and update the table above.
