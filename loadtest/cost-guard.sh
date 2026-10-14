#!/usr/bin/env bash
#
# loadtest/cost-guard.sh — the load/perf run's cost-safety check, as ONE command.
#
# WHY THIS EXISTS
#
#   Every response-mode submission enqueues an ASYNC AI spam-judge job
#   (api/internal/httpapi/public.go, `enqueueSpamJudge`), and the queue worker
#   then POSTs to the REAL model endpoint (api/internal/llm). The async design is
#   right for the request path and is exactly why the cost is invisible in a load
#   run: it never appears in a latency percentile, only on the bill. One measured
#   run of 17,123 submissions spent thousands of paid external model calls.
#
#   This wrapper is the stable PATH to the check. The check itself is
#   `api/cmd/jf-cost-guard`, which resolves and reads the toggle through the SAME
#   store accessors the API uses (`WorkspaceStore.Settings` / `SaveSettings`), so
#   it cannot drift from the enqueue gate — see the package doc in
#   api/internal/loadcost/loadcost.go for why a doc-level SQL snippet is not the
#   guard.
#
# USAGE
#
#   loadtest/cost-guard.sh check   [--space <subdomain> | --workspace <uuid>] [--database-url <dsn>]
#   loadtest/cost-guard.sh disable [--space <subdomain> | --workspace <uuid>] [--database-url <dsn>]
#
#   check    READ ONLY; exit 0 only when the toggle reads OFF.
#   disable  Write it OFF, then READ IT BACK; exit 0 only when a fresh read says
#            false. Never point this at production — the judge is a product
#            feature, and the scope of this guard is the COST OF A TEST RUN.
#
# EXIT CODES (from the Go command; baseline.sh refuses the sweep on any non-zero)
#
#   0  VERIFIED OFF    3  REFUSED: judge ON (or the write did not take effect)
#   1  usage error     4  REFUSED: could not verify (fail closed)
#
# ENVIRONMENT
#   DATABASE_URL          TEST/STAGING Postgres URL (or pass --database-url)
#   SPACE / WORKSPACE_ID  defaults for --space / --workspace
#   GO                    go binary (default: go)
#   JF_COST_GUARD_BIN     where to build/cache the binary
#                         (default: $TMPDIR/jf-cost-guard)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
GO="${GO:-go}"
BIN="${JF_COST_GUARD_BIN:-${TMPDIR:-/tmp}/jf-cost-guard}"

# Build when the binary is missing or OLDER than any api source, so a load run
# after a pull picks up the current check instead of a cached one. The Go module
# root is `api/` (there is no workspace file at the repo root), so the build runs
# from there. `go build` fails loudly when the toolchain or the module cache is
# missing — which is the right shape here: an unbuildable guard must refuse the
# sweep, not pass it.
stale=1
if [ -x "$BIN" ]; then
  if [ -z "$(find "$REPO_ROOT/api" -name '*.go' -newer "$BIN" -print -quit)" ]; then
    stale=0
  fi
fi
if [ "$stale" = 1 ]; then
  echo "cost-guard: building $BIN from $REPO_ROOT/api ..." >&2
  ( cd "$REPO_ROOT/api" && "$GO" build -o "$BIN" ./cmd/jf-cost-guard )
fi

exec "$BIN" "$@"
