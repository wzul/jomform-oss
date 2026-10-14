# JomForm Load / Performance Baseline (issue #103)

This is the load-test runbook and recorded baseline for JomForm. It covers the
three load surfaces from issue #103 — public form (buyer path), dashboard
(authed REST), and payment create-purchase — plus the regression gate and the
first-bottleneck analysis.

## COST SAFETY — do this BEFORE any load run

**A load run spends real money unless the AI spam judge is turned OFF for the
workspace under test.** This is the step that was missing: one run of 17,123
submissions enqueued thousands of AI spam-judge jobs that POSTed to the real
model endpoint, which never showed up in the run's latency numbers — only on the
bill. Read this section before starting any sweep.

### Why a load run is an external-spend event

Every response-mode submission calls `PublicHandlers.enqueueSpamJudge`
(`api/internal/httpapi/public.go`, the end of `handleSubmit`, and the formless
menu-order path in `order.go`). That enqueues an **async** job, and the queue
worker (`spamjudge.Handler.Send`) then POSTs to the real model endpoint through
`llm.Client` (`api/internal/llm`): base URL `LLM_BASE_URL`, default
`https://ollama.com/v1`, credential `OLLAMA_API_KEY`. Async is the right design
for the request path and is exactly why the cost is invisible — it never appears
in a percentile, only as

    WARN spamjudge_judge_failed err="llm: Post \"https://ollama.com/v1/chat/completions\": …"
    INFO spamjudge_done …

back to back, thousands of times.

The lever is the per-workspace toggle `workspace_settings.spam_judge_enabled`
(migration `000062_workspace_spam_judge_enabled`, `NOT NULL DEFAULT TRUE`). It is
read on the **enqueue** — `WorkspaceStore.SpamJudgeEnabledByAccount`,
`SELECT COALESCE(ss.spam_judge_enabled, TRUE)` — so with it OFF **no judge job is
enqueued at all** and no AI work is spent.

### The guard (use this, do not hand-roll it)

`loadtest/cost-guard.sh` resolves the workspace (by the k6 `SPACE` subdomain, or
by uuid), reads the toggle through the same store accessors the API uses, and
refuses when it cannot prove the judge is OFF:

```bash
export DATABASE_URL=postgres://…@staging-db:5432/jomform   # the TEST/STAGING database

# 1. CERTIFY (read-only). Exit 0 = safe to start a sweep.
./loadtest/cost-guard.sh check --space "$SPACE"

# 2. If it refuses because the judge is ON, turn it OFF — and it READS IT BACK,
#    so "the save said OK" is never mistaken for "the judge is off":
./loadtest/cost-guard.sh disable --space "$SPACE"

# 3. Then sweep. baseline.sh runs step 1 itself and ABORTS before the first
#    virtual user when it is not certified (fail-closed, not a warning):
./loadtest/baseline.sh http://staging.example.com
```

Exit codes, so a runner (or you) can tell the cases apart:

| Code | Meaning | What to do |
|---|---|---|
| 0 | VERIFIED OFF — the sweep may start | nothing |
| 3 | REFUSED: the judge is ON (or a `disable` did not take effect) | `cost-guard.sh disable`, or dashboard → Settings → AI spam judge |
| 4 | REFUSED: the state could not be VERIFIED (unreachable DB, unknown slug, missing table) | fix the database URL / the workspace name — never read this as safe |
| 1 | usage / configuration error (nothing was read or written) | pass `--space`/`--workspace` and `--database-url` |

`baseline.sh` also takes `DATABASE_URL`, `SPACE`, `WORKSPACE_ID` and
`COST_GUARD` (the guard command, defaulting to `loadtest/cost-guard.sh`), and the
`GO`/`JF_COST_GUARD_BIN` variables control where the guard binary is built and
cached.

### The app-mediated alternatives (equally valid ways to turn it off)

- **Dashboard:** Settings → AI spam judge → off. Writes
  `PUT /v1/workspaces/{id}/settings` with `{"spam_judge_enabled": false}`.
- **MCP:** `set_workspace_settings` with `spam_judge_enabled=false` (a
  `readwrite` API key over `POST /mcp`) — `api/internal/mcpsrv/workspaces.go`.
- **SQL, in the staging schema only** (prefer the guard/dashboard/MCP, which go
  through the app's read-modify-write):

  ```sql
  INSERT INTO workspace_settings (workspace_id, spam_judge_enabled)
  VALUES ('<ws-uuid>', false)
  ON CONFLICT (workspace_id) DO UPDATE SET spam_judge_enabled = false;
  ```

  That INSERT shape **is** valid: every other column on that table is
  `NOT NULL DEFAULT …` (`api/internal/db/migrations/000012_spaces.up.sql`), which
  is why the app itself can write a bare
  `INSERT INTO workspace_settings (workspace_id) VALUES ($1)`. A `spam_judge_enabled`
  with no settings row **reads as ON** — the read must be
  `SELECT COALESCE((SELECT spam_judge_enabled FROM workspace_settings WHERE workspace_id = '<ws-uuid>'), TRUE);`
  (one row that merely does not exist yet is the default, i.e. ON).

- **Note on the REST route from a load harness:** `GET/PUT
  /v1/workspaces/{id}/settings` is **session-only** — `RESTHandlers.workspaceAccessByID`
  requires a session user, so a Bearer API key gets 401. A load runner therefore
  cannot read the toggle over REST with a key; that is why the guard reads the
  database (and why the dashboard/MCP paths above are the app-mediated ones).

### The other lever: point the judge at a local model

If you *want* the judge exercised without external spend, redirect the endpoint
instead (any OpenAI-compatible server):

```bash
LLM_BASE_URL=http://127.0.0.1:11434/v1   # plus LLM_MODEL, if you do not want the default
```

Unsetting `OLLAMA_API_KEY` is **not** the lever: the job is still enqueued, and
the worker then fails with a credential error and retries with backoff
(`llm.New` never returns a nil client, so `spamjudge.Handler.Send`'s
`spamjudge_no_llm` shortcut does not fire).

### Verify it took effect — do not assume

1. `./loadtest/cost-guard.sh check --space "$SPACE"` exits **0**.
2. After ONE submission, **no `spamjudge_*` line** appears in the API log, and
   `submissions.is_spam` for that row stays `NULL`. If either is not true, the
   toggle did not take effect and the sweep must not run.

### Do not do this in production

The judge is a product feature that protects merchants from spam and data-rot
(`is_spam` rows are deleted by the retention sweep). Turning it off is a
**test-cost** measure for a test/staging workspace — never a production change.

## Tooling

- **Driver:** [k6](https://k6.io) (self-hosted, no SaaS dependency; also
  runnable with no install via `go run go.k6.io/k6@latest`).
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
| `loadtest/cost-guard.sh` | COST SAFETY (run before any sweep) | The AI spam judge must be OFF, or the run spends paid model calls — see "COST SAFETY" above |
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

## On-demand load gate (was the `load-gate.yml` workflow)

The gate runs the public-form scenario at a fixed concurrency and fails the
run (k6 exits non-zero) if p95 latency or error rate exceeds the threshold.

It was wired as a **`workflow_dispatch`** job in `.github/workflows/load-gate.yml`,
which has been removed along with the rest of the CI (see `deploy/DEPLOY.md` —
"A job that never ran is not a gate"). It is deliberately **not** part of
`scripts/preflight.sh`: it was never a per-PR gate, and it needs a
prod-equivalent endpoint that a local run does not have.

Installing k6 — there is no k6 binary on the agent host, and you do not need to
add one. Run it straight from the module proxy:

```bash
# no install, ~90s the first time (subsequent runs hit the build cache)
go run go.k6.io/k6@latest version

# or install it properly if you will run it repeatedly
#   https://grafana.com/docs/k6/latest/set-up/install-k6/
```

Then run the gate (against a staging / prod-equivalent URL):

```bash
go run go.k6.io/k6@latest run \
  -e BASE_URL=https://staging.example.com \
  -e VUS=50 -e DURATION=30s -e THRESH_P95=500 -e THRESH_ERR=0.01 \
  loadtest/gate.js
```

For the full concurrency sweep instead of the single gate scenario:

```bash
go run go.k6.io/k6@latest run -e BASE_URL=https://staging.example.com loadtest/public-form.js
# or, for the whole VUS_LIST sweep writing machine-readable summaries:
./loadtest/baseline.sh https://staging.example.com
```

A non-zero exit means the threshold was breached — that is the gate firing.
Record the result in the table above; it is a release-health check, not a PR gate.

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
