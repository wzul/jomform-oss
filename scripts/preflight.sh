#!/usr/bin/env bash
#
# scripts/preflight.sh — the local security + build gate for JomForm.
#
# WHY THIS EXISTS
#   GitHub Actions is not a gate for this repo: every job fails with 0 steps
#   (account-level billing failure), so a red check says nothing about the code.
#   The gates that used to run ONLY in `.github/workflows/ci.yml` are ported here
#   so removing the workflows does not silently drop coverage: `go vet`,
#   `go test -race`, govulncheck, npm audit, gitleaks and the axe-core a11y scan.
#   `deploy/build-and-ship.sh` calls this BEFORE building an image, so a broken
#   or vulnerable tree cannot ship.
#
#   The k6 load gate (loadtest/gate.js) is deliberately NOT here — it was a
#   workflow_dispatch job, not a per-PR gate, and needs a prod-equivalent
#   endpoint. See loadtest/BASELINE.md.
#
# GATES (order mirrors the three CI jobs, so the port is auditable)
#   api        build      cd api && go build ./...
#              vet        cd api && go vet ./...
#              test       cd api && go test ./... -race -count=1
#              govuln     govulncheck over the module graph (CVE scan)
#   web        build      cd web && npm run build   (runs test:unit first)
#              a11y       cd web && npm run a11y    (axe-core, critical/serious)
#              npmaudit   cd web && npm audit --audit-level=high
#   secrets    gitleaks   full-history secret scan (not just the tip)
#
# USAGE
#   scripts/preflight.sh                    # every gate
#   scripts/preflight.sh --list             # gate names, then exit
#   scripts/preflight.sh --only govuln      # just one gate (comma-separated)
#   scripts/preflight.sh --skip a11y,web    # everything except these
#
#   --only/--skip exist for proving a single gate BITES (sabotage one thing,
#   run that gate, expect RED) and for re-running one gate after a fix. They are
#   NOT a routine bypass: a skip prints a loud WARNING block naming what did not
#   run. A gate that has never been seen red is not evidence.
#
# REQUIREMENTS (the gate FAILS, never silently skips, when these are missing)
#   JOMFORM_TEST_DATABASE_URL   real Postgres for the integration tests.
#                               Without it `go test ./...` SKIPS the DB tests
#                               and prints a green summary that proves nothing.
#   JOMFORM_TEST_VALKEY_URL     same for the Valkey-backed tests.
#                               `source /tmp/jf-env.sh` exports both.
#   web/node_modules            a fresh worktree has none, and `npm run build`
#                               then exits 0 with "sh: 1: next: not found" —
#                               a green-looking gate that built nothing.
#                               Seed with: cd web && npm ci
#   playwright chromium         `cd web && npx playwright install chromium`
#
# RUNTIME (measured on the agent host): api build 13s, vet 13s, test 2m35s,
#   govuln 25s, web build ~2m, a11y 15s, npm audit 1s, gitleaks 30s.
#   Budget ~6 minutes for a full run.
#
# CONCURRENCY CAVEAT: `go test ./...` gives each package its own scratch schema
#   named `test_<pkg>` with no per-run qualifier, so a CONCURRENT full run from
#   another checkout DROPs the schema mid-suite and the failures look like
#   `relation "users" does not exist` in code you never touched. If a package
#   fails here that passes alone, that race is the first thing to suspect —
#   re-run the package by itself. For a clean run, point the env vars at your
#   own database (see docs in the pipeline notes).
#
# ESCAPE HATCH: there is none here by design. `deploy/build-and-ship.sh
#   --skip-tests` bypasses this entire script and says so loudly; that flag is
#   for a genuine emergency only.
#
# SAFETY PROPERTIES (each one is load-bearing — do not "simplify" them away):
#   * every gate fails LOUDLY on a missing prerequisite instead of skipping;
#   * skipping EVERY gate is an ERROR, not a green run (see the selection check);
#   * a gate that never ran is never counted in `passed`.

set -uo pipefail

REPO_ROOT="$(git -C "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.." rev-parse --show-toplevel 2>/dev/null)"
if [ -z "${REPO_ROOT:-}" ] || [ ! -d "$REPO_ROOT" ]; then
  echo "PREFLIGHT FAIL: scripts/preflight.sh must be run from inside the git repository (run it from the repo root or via ./scripts/preflight.sh)" >&2
  exit 1
fi
cd "$REPO_ROOT" || exit 1

# `build` is the api build; `web` is `npm run build` in web/. Both are named so
# --only/--skip can address them without ambiguity.
GATE_NAMES=(build vet test govuln web a11y npmaudit gitleaks)

ONLY=""
SKIP=""

die() { echo "PREFLIGHT FAIL: $*" >&2; exit 1; }
info() { echo "==> $*"; }

usage() { sed -n '2,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)   usage; exit 0 ;;
    --list)      printf '%s\n' "${GATE_NAMES[@]}"; exit 0 ;;
    --only)      ONLY="${2:-}"; shift ;;
    --only=*)    ONLY="${1#*=}" ;;
    --skip)      SKIP="${2:-}"; shift ;;
    --skip=*)    SKIP="${1#*=}" ;;
    *)           die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done

# selected() — is this gate in the requested set?
selected() {
  local gate="$1"
  if [ -n "$ONLY" ]; then
    case ",$ONLY," in *",$gate,"*) ;; *) return 1 ;; esac
  fi
  if [ -n "$SKIP" ]; then
    case ",$SKIP," in *",$gate,"*) return 1 ;; esac
  fi
  return 0
}

if [ -n "$ONLY" ]; then
  for g in ${ONLY//,/ }; do
    case " ${GATE_NAMES[*]} " in *" $g "*) ;; *) die "unknown gate for --only: $g (see --list)" ;; esac
  done
fi
if [ -n "$SKIP" ]; then
  for g in ${SKIP//,/ }; do
    case " ${GATE_NAMES[*]} " in *" $g "*) ;; *) die "unknown gate for --skip: $g (see --list)" ;; esac
  done
fi

SKIPPED=()
for gate in "${GATE_NAMES[@]}"; do
  selected "$gate" || SKIPPED+=("$gate")
done

# A run that skips EVERY gate is a no-op, not a pass. Without this check,
# `--skip <all>` exits 0 and prints "green", which is exactly the empty-gate
# shape this script exists to prevent. Refuse it.
if [ ${#SKIPPED[@]} -eq ${#GATE_NAMES[@]} ]; then
  die "every gate was skipped (--only/--skip selected nothing to run) — that is an empty run, not a passing one. Omit --only/--skip to run the full gate."
fi

echo "=============================================================="
echo "  JomForm preflight — local gate (GitHub Actions is not a gate)"
echo "  repo:   $REPO_ROOT"
echo "  commit: $(git rev-parse --short HEAD) ($(git describe --tags --always 2>/dev/null || echo untagged))"
echo "  gates:  ${GATE_NAMES[*]}"
if [ -n "$ONLY" ]; then echo "  --only: $ONLY"; fi
echo "=============================================================="

if [ ${#SKIPPED[@]} -gt 0 ]; then
  cat >&2 <<EOF

  ############################################################
  #  !!!  WARNING: ${#SKIPPED[@]} GATE(S) SKIPPED              #
  ############################################################
  #  NOT RUN: ${SKIPPED[*]}
  #  A skipped gate is NOT a passing gate. Do not read this
  #  run as full coverage, and do not deploy on it unless the
  #  skipped gate genuinely does not apply to your change.
  ############################################################

EOF
fi

# ---------------------------------------------------------------------------
# Environment preflight. Everything here FAILS rather than skipping: a gate
# that turns itself off when a prerequisite is missing is how a green run
# proves nothing.
# ---------------------------------------------------------------------------
preflight_env() {
  local need_api="$1" need_web="$2" need_a11y="$3"

  if [ "$need_api" = 1 ]; then
    [ -n "${JOMFORM_TEST_DATABASE_URL:-}" ] || die \
      "JOMFORM_TEST_DATABASE_URL is unset — the integration tests would SKIP and report a green suite that proves nothing. Run: source /tmp/jf-env.sh"
    [ -n "${JOMFORM_TEST_VALKEY_URL:-}" ] || die \
      "JOMFORM_TEST_VALKEY_URL is unset — same silent-skip problem. Run: source /tmp/jf-env.sh"
  fi

  if [ "$need_web" = 1 ]; then
    [ -x "$REPO_ROOT/web/node_modules/.bin/next" ] || die \
      "web/node_modules/.bin/next is missing — 'npm run build' would exit 0 with 'sh: 1: next: not found' and build NOTHING (exit 0 with no route list is a FAILED gate, not a pass). Seed it with: cd web && npm ci"
  fi

  if [ "$need_a11y" = 1 ]; then
    [ -x "$REPO_ROOT/web/node_modules/.bin/playwright" ] || die \
      "web/node_modules/.bin/playwright is missing — seed web/node_modules first: cd web && npm ci"
  fi
}

# ---------------------------------------------------------------------------
# Gate runner.
# ---------------------------------------------------------------------------
FAILED=()
PASSED=()
START_ALL=$(date +%s)

run() {
  local gate="$1" label="$2"; shift 2
  if ! selected "$gate"; then return 0; fi
  info "gate [$gate]: $label"
  local t0 t1 rc
  t0=$(date +%s)
  # NOTE: no `|| true`, no `continue-on-error`. A gate either passes or stops
  # the run; that is the whole point of moving it off GitHub.
  if "$@"; then
    t1=$(date +%s)
    PASSED+=("$gate")
    info "gate [$gate]: PASS ($((t1-t0))s)"
  else
    rc=$?
    t1=$(date +%s)
    FAILED+=("$gate")
    echo "PREFLIGHT FAIL: gate '$gate' ($label) exited $rc after $((t1-t0))s" >&2
    summary
    exit 1
  fi
}

summary() {
  local end elapsed
  end=$(date +%s); elapsed=$((end-START_ALL))
  echo "--------------------------------------------------------------"
  echo "preflight summary: ${#PASSED[@]} passed, ${#FAILED[@]} failed, ${#SKIPPED[@]} skipped in ${elapsed}s"
  [ ${#PASSED[@]} -gt 0 ] && echo "  PASS: ${PASSED[*]}"
  [ ${#SKIPPED[@]} -gt 0 ] && echo "  SKIP: ${SKIPPED[*]} (not evidence)"
  [ ${#FAILED[@]} -gt 0 ] && echo "  FAIL: ${FAILED[*]}"
  echo "--------------------------------------------------------------"
}

# --- which gates are selected, for the env preflight ------------------------
g_build=0; g_vet=0; g_test=0; g_govuln=0; g_web=0; g_a11y=0; g_npmaudit=0; g_gitleaks=0
selected build    && g_build=1
selected vet      && g_vet=1
selected test     && g_test=1
selected govuln   && g_govuln=1
selected web      && g_web=1
selected a11y     && g_a11y=1
selected npmaudit && g_npmaudit=1
selected gitleaks && g_gitleaks=1

need_api=0; need_web=0; need_a11y=0
if [ $g_build = 1 ] || [ $g_vet = 1 ] || [ $g_test = 1 ] || [ $g_govuln = 1 ]; then need_api=1; fi
if [ $g_web = 1 ]; then need_web=1; fi
if [ $g_a11y = 1 ]; then need_a11y=1; need_web=1; fi
preflight_env "$need_api" "$need_web" "$need_a11y"

# ---------------------------------------------------------------------------
# api/build — the module still compiles.
# ---------------------------------------------------------------------------
run build "cd api && go build ./..." \
  bash -c "cd '$REPO_ROOT/api' && go build ./..."

# ---------------------------------------------------------------------------
# api/vet
# ---------------------------------------------------------------------------
run vet "cd api && go vet ./..." \
  bash -c "cd '$REPO_ROOT/api' && go vet ./..."

# ---------------------------------------------------------------------------
# api/test — WITH -race. The old local gate ran plain `go test`, so a data race
# could only ever be caught in CI.
# ---------------------------------------------------------------------------
run test "cd api && go test ./... -race -count=1" \
  bash -c "cd '$REPO_ROOT/api' && go test ./... -race -count=1"

# ---------------------------------------------------------------------------
# api/govuln — CVE scan over the module graph.
#   GOTOOLCHAIN pin (issue #368): govulncheck@latest declares a go directive
#   BELOW this module's (1.27), so the go command would build the scanner with
#   the older toolchain and then fail to parse this module ("package requires
#   newer Go version"). Pin it to the toolchain the module resolves to.
#   Exit code: govulncheck exits non-zero when the code is affected by a
#   vulnerability, which is what makes this a gate rather than a report.
# ---------------------------------------------------------------------------
run govuln "cd api && govulncheck ./... (module graph CVE scan)" \
  bash -c "cd '$REPO_ROOT/api' && GOTOOLCHAIN=\$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@latest ./..."

# ---------------------------------------------------------------------------
# web/build — `npm run build` runs `npm run test:unit` first, so the web unit
# suite and the cross-language Go-contract guards run here too. Treat exit 0
# with no emitted route list as a FAILURE (that is the "next: not found" shape).
# ---------------------------------------------------------------------------
run web "cd web && npm run build (runs test:unit, then next build)" \
  bash -c "cd '$REPO_ROOT/web' && npm run build"

# ---------------------------------------------------------------------------
# web/a11y — axe-core. Fails on any critical/serious WCAG violation.
#   FIDELITY: playwright.config.ts starts the server itself
#   (reuseExistingServer is OFF) and we hand it a FREE port. A leftover
#   `next-server` from an earlier run would otherwise be reused and axe would
#   report a clean pass for a build this tree never produced. The port is
#   discovered here rather than fixed, so a stale server cannot collide either.
#   Needs web/build to have produced .next, hence the order.
# ---------------------------------------------------------------------------
a11y_run() {
  cd "$REPO_ROOT/web" || return 1
  # Find a free port (base 3100, stepping up). `ss` is the source of truth for
  # "in use"; a connect probe would miss a socket that is listening but not
  # accepting yet.
  local port=""
  local candidate
  for candidate in $(seq 3100 3140); do
    if ! ss -lntn "sport = :$candidate" 2>/dev/null | grep -q LISTEN; then
      port="$candidate"
      break
    fi
  done
  [ -n "$port" ] || { echo "no free port in 3100-3140 for the a11y web server" >&2; return 1; }
  echo "a11y: starting the app's own server on port $port (never reusing a leftover)"
  A11Y_PORT="$port" npm run a11y
}

run a11y "cd web && npm run a11y (axe-core critical/serious)" a11y_run

# ---------------------------------------------------------------------------
# web/npmaudit — fail on high/critical advisories. Never `|| true`.
# ---------------------------------------------------------------------------
run npmaudit "cd web && npm audit --audit-level=high" \
  bash -c "cd '$REPO_ROOT/web' && npm audit --audit-level=high"

# ---------------------------------------------------------------------------
# secrets/gitleaks — FULL history, not just the tip. In CI that was
# `fetch-depth: 0`; locally the equivalent is that the clone is not shallow and
# the scan really walks every commit. A shallow checkout silently scans almost
# nothing, so refuse to run there instead of reporting "no leaks found".
#   gitleaks runs via `go run` (what CI did) so there is no new binary
#   dependency to install on the host.
# ---------------------------------------------------------------------------
gitleaks_run() {
  if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
    echo "this is a SHALLOW clone — the full-history secret scan is impossible; it would scan a handful of commits and report 'no leaks found'. In CI this was fetch-depth: 0. Unshallow first:" >&2
    echo "  git fetch --unshallow" >&2
    return 1
  fi
  local commits
  commits=$(git rev-list --count HEAD)
  echo "scanning $commits commits (full history)"
  cd "$REPO_ROOT" || return 1
  # TOOLCHAIN: `go env GOVERSION` at the REPO ROOT reports the DEFAULT toolchain
  # (go1.22.2 on the agent host) because the root has no go.mod — the repo's
  # 1.27.0 declaration lives in api/go.mod. gitleaks v8 requires go >= 1.24.11,
  # so resolving the toolchain from the root makes the `go run` below fail to
  # BUILD the scanner, and the gate then fails for a reason that has nothing to
  # do with secrets. Resolve it from api/, where the module's go directive is.
  # If the scanner fails to build we still exit non-zero (the `go run` status
  # propagates through `run`), so this can never fail open — but a gate that
  # fails for the wrong reason trains people to ignore it.
  local toolchain
  toolchain="$(cd "$REPO_ROOT/api" && go env GOVERSION)"
  GOTOOLCHAIN="$toolchain" go run github.com/zricethezav/gitleaks/v8@latest \
    detect --source . --no-banner --redact --config .gitleaks.toml
}

run gitleaks "gitleaks detect (full git history, no leaks)" gitleaks_run

summary

if [ ${#FAILED[@]} -gt 0 ]; then exit 1; fi
if [ ${#SKIPPED[@]} -gt 0 ]; then
  info "preflight green for the gates that ran — ${#SKIPPED[@]} gate(s) were SKIPPED, so this is NOT full coverage"
else
  info "preflight green: all ${#PASSED[@]} gates passed"
fi
