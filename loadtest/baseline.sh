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
#   BASE_URL   target base URL (default http://localhost:8080)
#   API_KEY    Bearer API key for authed dashboard endpoints
#   VUS_LIST   space-separated concurrency levels (default "10 50 100 200")
#   DURATION   steady-state duration per level (default 30s)
#   RAMP       ramp-up duration per level (default 10s)
#   OUT_DIR    where JSON summaries are written (default loadtest/results)
#
# Requires: k6 (https://k6.io). Install: https://grafana.com/docs/k6/latest/set-up/install/
set -euo pipefail

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
