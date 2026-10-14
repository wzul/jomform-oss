#!/usr/bin/env bash
# Baseline load-test runner for JomForm (issue #103).
#
# Sweeps the public-form buyer path at a set of concurrency levels and
# records RPS + latency percentiles + error rate for each. This is the
# "record current capacity" step of the load/performance baseline.
#
# Usage:
#   ./loadtest/baseline.sh [BASE_URL] [API_KEY]
#
# Env overrides:
#   BASE_URL     target base URL (default http://localhost:8080)
#   API_KEY      Bearer API key for authed dashboard endpoints
#   VUS_LIST     space-separated concurrency levels (default "10 50 100 200")
#   DURATION     steady-state duration per level (default 30s)
#   RAMP         ramp-up duration per level (default 10s)
#   OUT_DIR      where JSON summaries are written (default loadtest/results)
#
# COST SAFETY (mandatory — see loadtest/BASELINE.md, "COST SAFETY")
#   Every response-mode submission enqueues an async AI spam-judge job that
#   POSTs to the REAL model endpoint, so a sweep spends paid model calls that
#   never appear in its own latency numbers. This script therefore runs the cost
#   guard BEFORE the first virtual user and REFUSES the sweep when the guard
#   cannot prove the judge is OFF for the workspace under test — it is a
#   fail-closed interlock, not a warning.
#     DATABASE_URL      TEST/STAGING Postgres URL the guard reads (required)
#     SPACE             workspace subdomain under test (default "demo")
#     WORKSPACE_ID      workspace uuid, if you prefer it over SPACE
#     COST_GUARD        the guard command (default loadtest/cost-guard.sh)
#
# Requires: k6 (https://k6.io). Install: https://grafana.com/docs/k6/latest/set-up/install/
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BASE_URL="${BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-}"
VUS_LIST="${VUS_LIST:-10 50 100 200}"
DURATION="${DURATION:-30s}"
RAMP="${RAMP:-10s}"
OUT_DIR="${OUT_DIR:-loadtest/results}"

mkdir -p "$OUT_DIR"

echo "== JomForm baseline sweep =="
echo "  base_url : $BASE_URL"
echo "  vus      : $VUS_LIST"
echo "  duration : $DURATION (ramp $RAMP)"
echo "  out_dir  : $OUT_DIR"
echo

# ── COST SAFETY INTERLOCK — BEFORE any traffic is generated ──────────────────
# The guard is asked about the workspace the sweep will hammer (SPACE, the same
# knob public-form.js uses), and its non-zero verdicts all mean the same thing
# here: the cost of this run is not verified, so the run does not start. See
# loadtest/BASELINE.md for what each code means and how to clear it.
SPACE="${SPACE:-demo}"
COST_GUARD="${COST_GUARD:-$HERE/cost-guard.sh}"
guard_args=(check)
if [ -n "${WORKSPACE_ID:-}" ]; then
  guard_args+=(--workspace "$WORKSPACE_ID")
else
  guard_args+=(--space "$SPACE")
fi
if [ -n "${DATABASE_URL:-}" ]; then
  guard_args+=(--database-url "$DATABASE_URL")
fi
if ! "$COST_GUARD" "${guard_args[@]}"; then
  echo
  echo "== ABORTED: the cost guard refused to certify this sweep ==" >&2
  echo "   No traffic was generated. Every submission this run would have made" >&2
  echo "   enqueues an AI spam-judge job that POSTs to the real model endpoint," >&2
  echo "   so an unverified sweep spends paid model calls silently." >&2
  echo "   Fix the guard's complaint above (or turn the judge off), then re-run." >&2
  echo "   See loadtest/BASELINE.md, section \"COST SAFETY\"." >&2
  exit 1
fi
echo

summary=""
for vus in $VUS_LIST; do
  echo "--- VUs=$vus ---"
  out="$OUT_DIR/baseline-vus-$vus.json"
  args=(-e "VUS=$vus" -e "DURATION=$DURATION" -e "RAMP=$RAMP" -e "BASE_URL=$BASE_URL")
  if [ -n "$API_KEY" ]; then
    args+=(-e "API_KEY=$API_KEY")
  fi
  # --summary-export writes a machine-readable JSON summary per run.
  k6 run --summary-export "$out" "${args[@]}" loadtest/public-form.js
  summary="$summary $vus"
  echo
done

echo "== Baseline sweep complete (VUs:$summary) =="
echo "Summaries written to $OUT_DIR/"
echo "Next: record the RPS / p50 / p95 / p99 / error-rate figures in"
echo "      loadtest/BASELINE.md (see the runbook)."
