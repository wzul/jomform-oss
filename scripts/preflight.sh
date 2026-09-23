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
#   monitor    watchdog   deploy/monitoring/jomform-watch_test.sh — proves the
#                         uptime watchdog ALERTS on a failure and stays quiet on
#                         a success (offline: stub server + fake notifier)
#   tmpkey     helper     deploy/tmp-apikey_test.sh — proves a captured key id
#                         cannot carry psql's `INSERT 0 1` tag, that cleanup
#                         deletes by exact name (never a LIKE sweep of a sibling
#                         card's live key), and that a row which survives the
#                         delete is a FAILURE, not a quiet success
#                         (offline: fake ssh/scp/docker/curl, no host)
#   backup     suite      deploy/backup/jomform-backup_test.sh — the nightly
#                         Postgres + media backup's own failure-path suite: a
#                         copy that "exits 0" having landed nothing, a same-size
#                         object whose bytes differ, an empty dump archive, a
#                         remote with no hash, a restore drill that must not
#                         overwrite the backup job's status, and the fail-closed
#                         override seam. (offline: stubbed rclone/docker on the
#                         PATH it hands the script, no network, no DB, ~45s).
#                         The suite's bite proof (jomform-backup_bite.sh, ~8 min)
#                         is deliberately NOT wired in — on-demand only.
#   refguard   refs       scripts/refguard_test.sh — proves the hand-made
#                         `refs/remotes/<non-remote>/<N>` mirror guard BITES:
#                         the documented GitHub idiom
#                         (`git fetch origin pull/N/head:refs/remotes/pr/N`) is
#                         REFUSED in the primary checkout AND in a linked
#                         worktree; deletion stays allowed; a second remote's
#                         namespace stays allowed; normal git flow is untouched;
#                         and a hook that is missing, non-executable or stale is
#                         reported NOT ARMED instead of quietly guarding nothing.
#                         Offline: local bare remote + clones, no network, ~10s.
#   prunewt    helper      deploy/worktrees/prune-kanban-worktrees_test.sh —
#                         proves the worktree pruner's durability test cannot be
#                         fooled by a hand-made `refs/remotes/pr/<N>` mirror:
#                         work held only by one is reported UNSAFE instead of
#                         being removed (it once was, which is how a detached
#                         worktree holding the last copy of a commit was lost),
#                         while genuinely pushed/merged work is still removed and
#                         a live worker's worktree is still kept.
#                         Offline: a sandbox repo with its own bare `origin`, a
#                         fake `hermes kanban show`, no board, no network.
#   selftest   selfcheck  scripts/preflight_test.sh — the gate runner's OWN
#                         safety properties: an all-skipped run is REFUSED
#                         rather than reported green, a missing prerequisite
#                         FAILS instead of skipping, and the gate inventory
#                         cannot drift from the gates that can actually run.
#                         Offline and cheap: a sandbox repo plus a copy of this
#                         script, so it never triggers a full build/test.
#                         This gate is why the two checks that had rotted in
#                         preflight_test.sh could not stay broken (they were
#                         unreferenced, so nothing ever ran them).
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
#                               `source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}` exports both.
#   web/node_modules            a fresh worktree has none, and `npm run build`
#                               then exits 0 with "sh: 1: next: not found" —
#                               a green-looking gate that built nothing.
#                               Seed with: cd web && npm ci
#   web deps (all declared      web/scripts/web-deps-check.mjs asserts every
#     packages)                 package in web/package.json is installed. The
#                               `.bin/next` check above is not enough: a
#                               node_modules DIRECTORY that exists and is EMPTY
#                               (or an interrupted `npm ci`) still runs
#                               `npm run test:unit`, whose node --test then
#                               cannot load the test files that import react —
#                               measured 254 tests / 253 pass / 1 fail against
#                               260 / 260 / 0 for the same head with a real
#                               install. Under-reporting, not failing. The web
#                               gate refuses on it now (and `test:unit` itself
#                               refuses), so the count can never be read as
#                               evidence from an uninstalled tree.
#   playwright chromium         `cd web && npx playwright install chromium`
#
# RUNTIME (measured on the agent host): api build 13s, vet 13s, test 2m35s,
#   govuln 25s, web build ~2m, a11y 15s, npm audit 1s, gitleaks 30s, backup ~45s.
#   Budget ~7 minutes for a full run.
#
# CONCURRENCY: `go test ./...` gives each test PROCESS its own scratch schema,
#   named `test_<pkg>_p<pid>` — per-package so parallel packages do not collide,
#   and per-pid so a CONCURRENT full run from another checkout cannot drop this
#   one's schema mid-suite. Every checkout points at the same test database, so a
#   per-package-only name let two lanes gating at once destroy each other's data
#   and fail in code neither had touched (`relation "workspaces" does not exist`,
#   SQLSTATE 42P01). If a package still fails here but passes alone, the first
#   thing to suspect is a shared RESOURCE other than the schema: the test Valkey
#   (login-breaker counters). Re-run the package by itself to tell them apart.
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
#
# ADDING A GATE: append the name here AND add its `run <name> ...` call below.
# scripts/preflight_test.sh derives both sides from this file (the inventory via
# `--list`, the runnable set from the `run <name>` call sites) and FAILS if they
# disagree, so a gate named here but never run — the silent-coverage-loss shape —
# is caught by `--only selftest`. No edit to the self-test is needed for gate #N.
GATE_NAMES=(build vet test govuln web a11y npmaudit gitleaks monitor tmpkey backup prunewt refguard selftest)

ONLY=""
SKIP=""

# Preserve the ORIGINAL argv before the parsing loop below consumes it with `shift`.
# The concurrency lock re-invokes this script via flock, and it must hand the child the
# same arguments — reading `$@` at the lock site would pass whatever survived the loop
# (nothing), so `--only <gate>` would silently become "run every gate".
PREFLIGHT_ARGV=("$@")

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

# ---- CONCURRENCY LOCK: serialise gate runs across worktrees ----------------
#
# LANE RULE 4 says "never parallelise gates": two concurrent `preflight.sh` runs
# share one scratch schema and one scratch valkey index, so they corrupt each
# other's fixtures and produce FALSE failures (a `go test` red that passes in
# isolation, or a 10m `-test.timeout` blown by CPU starvation). That rule was
# advisory — a worker could only obey it by remembering to, and the lanes
# genuinely cannot see each other. This lock makes it structural.
#
# WHY A FILE LOCK AND NOT A GATE CHANGE: the gates are correct as written; the
# defect is two of them running at once. A lock needs no edit to any gate, and
# it costs nothing when the host is idle.
#
# WHY BLOCK INSTEAD OF FAIL: refusing to run would turn an idle-host problem
# into a red gate for whoever lost the race — the false-red shape this fixes.
# Waiting is correct; the gate still runs, on the same tree, with the machine
# to itself.
#
# SCOPE: serialises the GATE BODY only. `--list`, argument validation and the
# banner stay outside, so a queued run can always be inspected and a typo still
# fails immediately rather than after a wait.
#
# LOCK PATH: `/var/lock` is tmpfs on Linux and is wiped on reboot. That is
# deliberate — a lock file left behind by a crash must never block future runs,
# and flock releases automatically when the holder dies, so there is nothing to
# clean up either way. `PREFLIGHT_LOCK_WAIT=0` turns the wait off explicitly if
# a caller ever needs to fail fast instead; it is not the default because
# failing fast is exactly what produces a false red.
PREFLIGHT_LOCK_FILE="${PREFLIGHT_LOCK_FILE:-/var/lock/jomform-preflight-gate.lock}"
PREFLIGHT_LOCK_WAIT="${PREFLIGHT_LOCK_WAIT:-1800}"

# ---- CONCURRENCY LOCK (taken first; the banner below prints once, in the child) ----
#
# The lock is taken by re-executing this same script under `flock`. Doing it before
# the banner and before any gate keeps the gates untouched, prints the banner exactly
# once (only the child gets that far), and reports the wait before it happens rather
# than leaving it to be discovered as a hang later.
if [ "${_PREFLIGHT_LOCKED:-}" != "1" ]; then
  # No `|| true` anywhere below: this repo's own `selftest` gate forbids it in an active
  # line, because a swallowed failure is how a gate stops being a gate. A missing lock
  # directory is a real problem (the lock would silently not exist), so it FAILS loudly.
  if [ ! -d "$(dirname "$PREFLIGHT_LOCK_FILE")" ]; then
    echo "preflight: lock directory $(dirname "$PREFLIGHT_LOCK_FILE") does not exist." >&2
    echo "preflight: set PREFLIGHT_LOCK_FILE to a path inside an existing directory (default: /var/lock)." >&2
    exit 1
  fi
  if command -v flock >/dev/null 2>&1; then
    # `_PREFLIGHT_LOCKED=1` is exported so the re-invoked copy runs the gates
    # instead of trying to take the lock again.
    #
    # NOT `exec`: exec would replace this process, so the timeout diagnostic
    # below could never run. Running flock as a child keeps the exit code
    # reachable and still passes the child's status straight through.
    #
    # `-E 75` gives a timeout its own code so a wait-out is distinguishable from
    # a gate failure — a caller must not read "could not serialise" as "the code
    # is broken".
    export _PREFLIGHT_LOCKED=1
    if ! flock -n "$PREFLIGHT_LOCK_FILE" true 2>/dev/null; then
      echo "  lock:   another gate is running — waiting up to ${PREFLIGHT_LOCK_WAIT}s"
      echo "          (two concurrent gates share one scratch schema and produce FALSE failures)"
    fi
    # Pass the ORIGINAL argv, not "$@" — the parsing loop above has already shifted it
    # away, and an empty argv silently turns `--only <gate>` into a full-gate run.
    flock -w "$PREFLIGHT_LOCK_WAIT" -E 75 "$PREFLIGHT_LOCK_FILE" "$0" "${PREFLIGHT_ARGV[@]}"
    _lock_rc=$?
    if [ "$_lock_rc" -eq 75 ]; then
      echo "preflight: could not acquire the gate lock at $PREFLIGHT_LOCK_FILE within ${PREFLIGHT_LOCK_WAIT}s" >&2
      echo "preflight: refusing to run unlocked — a parallel run shares one scratch schema and one" >&2
      echo "preflight: scratch valkey index, so it reports FALSE failures. NOTHING was built or tested." >&2
      echo "preflight: re-run once the other gate finishes, or raise the wait with PREFLIGHT_LOCK_WAIT=<seconds>." >&2
      echo "preflight: the lock is released automatically when its holder exits, including on a crash." >&2
      exit 75
    fi
    exit "$_lock_rc"
  fi
  echo "  lock:   flock not found — running WITHOUT serialisation (concurrent gates may FALSE-fail)"
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
      "JOMFORM_TEST_DATABASE_URL is unset — the integration tests would SKIP and report a green suite that proves nothing. Run: source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}"
    [ -n "${JOMFORM_TEST_VALKEY_URL:-}" ] || die \
      "JOMFORM_TEST_VALKEY_URL is unset — same silent-skip problem. Run: source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}"
  fi

  if [ "$need_web" = 1 ]; then
    [ -x "$REPO_ROOT/web/node_modules/.bin/next" ] || die \
      "web/node_modules/.bin/next is missing — 'npm run build' would exit 0 with 'sh: 1: next: not found' and build NOTHING (exit 0 with no route list is a FAILED gate, not a pass). Seed it with: cd web && npm ci"
    # `.bin/next` existing is NOT the same as the install being complete: an
    # empty node_modules directory (or an interrupted `npm ci`) can satisfy it
    # while `npm run test:unit` still cannot load the test files that import
    # react — the run then prints a smaller, authoritative-looking count
    # (measured 254/253/1 vs 260/260/0 at the same head) instead of failing.
    # The dependency check below is the one that refuses that shape, and it is
    # the SAME script `test:unit` runs first, so the gate and the suite cannot
    # disagree about whether this tree is installed.
    node "$REPO_ROOT/web/scripts/web-deps-check.mjs" "$REPO_ROOT/web" || die \
      "the web dependencies for this tree are not installed — 'npm run test:unit' would UNDER-REPORT (node --test runs without dependencies, so the files that import react fail to load and their tests never register) instead of failing. Seed it with: cd web && npm ci"
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

# monitor — the uptime watchdog's self-test. This gate is unusual: the watchdog
# is production-only (it runs on the prod host, not in the repo), so the only
# thing testable here is its own harness — that the alerting path fires on a
# failure, stays quiet on a success, and refuses to report green when it has no
# way to alert. Those are exactly the properties that make a watcher worth
# having, and they are cheap (stub server + fake notifier, no network).
monitor_run() {
  bash "$REPO_ROOT/deploy/monitoring/jomform-watch_test.sh"
}
run monitor "watchdog self-test (alert fires, quiet when healthy, never blind)" monitor_run

# tmpkey — the temp-API-key helper's self-test. Same shape of gate as `monitor` and
# for the same reason: `deploy/tmp-apikey.sh` is only ever run against a live host,
# so what is testable in the repo is its own harness — that the psql command tag
# cannot ride along in a captured id, that cleanup deletes by EXACT name and cannot
# reach a sibling card's key, and that a row which does NOT go away is reported as a
# failure instead of a quiet success. Those three defects each left a live
# credential stranded on prod, so this is the gate that keeps them from returning.
# Offline by construction: fake ssh/scp, a fake `docker` that answers the queries
# from a state file, and a fake curl. No host, no network, no credentials.
tmpkey_run() {
  bash "$REPO_ROOT/deploy/tmp-apikey_test.sh"
}
run tmpkey "temp-key helper self-test (no tag in the id, exact-name delete, survivor is a failure)" tmpkey_run

# backup — the nightly Postgres + media backup's failure-path suite. Same shape of
# gate as `monitor`/`tmpkey` and for the same reason: `deploy/backup/jomform-backup.sh`
# only ever runs on the production host, so what is testable in the repo is its own
# harness. The defect it exists to close is DATA LOSS THAT LOOKS FINE — the job whose
# failure is silent is the one the whole directory is a deliverable for:
#   * `rclone copy` exits 0 but nothing lands on the remote -> the run must FAIL
#     (exit 0 is not proof of an upload);
#   * the object lands with the same size but different bytes -> FAIL on hash;
#   * the backend exposes no hash -> FAIL rather than degrade to a size-only check;
#   * an empty dump (no TABLE DATA) -> refused instead of uploaded;
#   * a restore drill must not overwrite the nightly job's own status, or a green
#     drill would hide that the backups had stopped succeeding;
#   * a partial-override hand-run must fail closed BEFORE any dump/lock/alert.
# Offline by construction: stub `rclone`/`docker` on the PATH it hands the script, no
# network, no database. ~45s.
#
# WHY IT IS A GATE AND NOT JUST A SCRIPT: until this line existed, NOTHING invoked
# this suite — `./scripts/preflight.sh --list` did not name it and the runner had no
# call site, so a future edit to jomform-backup.sh could break all 98 assertions
# while the gate still printed green. A regression net nobody runs is not a net.
# The suite's BITE PROOF (deploy/backup/jomform-backup_bite.sh, ~8 min, mutates a
# throwaway copy per sabotage) is deliberately NOT a per-run gate: it is on-demand
# (see deploy/backup/README.md), because 8 minutes on every merge is how a gate gets
# skipped. The ~45s suite is the one that has to run every time.
backup_run() {
  bash "$REPO_ROOT/deploy/backup/jomform-backup_test.sh"
}
run backup "nightly backup failure-path suite (exit 0 is not an upload, empty dump refused, drill cannot mask a failing job)" backup_run

# prunewt — the worktree pruner's self-test. Same shape of gate as
# `monitor`/`tmpkey` and for the same reason: `prune-kanban-worktrees.sh` only
# ever runs on the agent host (as a cron job with no agent loop), and its one
# irreversible mistake is removing the last copy of someone's work. What is
# testable here is its own harness — that the DURABILITY test cannot be fooled by
# a hand-made `refs/remotes/pr/<N>` mirror, which is in no refspec and therefore
# never pruned by `git fetch --prune`, so work held only by one reads as "pushed
# to a remote" and its worktree gets removed. That is not hypothetical: it is how
# this defect was reproduced. The suite also asserts the guards that must keep
# working after the fix (a genuinely pushed or merged commit is still removed, a
# live worker's worktree is still kept), because a pruner that refuses everything
# would pass a mirror-only test and be useless.
#
# Offline by construction: a sandbox repo with its own real bare `origin`, and a
# fake `hermes kanban show`. No board, no board DB writes, no network.
prunewt_run() {
  bash "$REPO_ROOT/deploy/worktrees/prune-kanban-worktrees_test.sh"
}
run prunewt "worktree pruner self-test (a stale refs/remotes/pr/N mirror is not durability; real durability still prunes)" prunewt_run

# refguard — the hand-made `refs/remotes/<non-remote>/<N>` mirror guard. Same shape
# of gate as `monitor`/`tmpkey`/`backup` and for the same reason: what is testable
# in the repo is the guard's own harness.
#
# WHY IT IS A GATE AND NOT JUST A CLEANUP. The defect it closes was "fixed" once
# by deleting the mirrors and was RE-ARMED WITHIN 20 MINUTES by a lane typing the
# GitHub-documented `git fetch origin pull/N/head:refs/remotes/pr/N` again — 26
# refs by the next day, 6 already stale. A cleanup is a point in time. The only
# mechanism that can stop a ref from being created is git's own
# `reference-transaction` hook (`git update-ref`, `fetch`, `push` all go through
# it), which is what `scripts/refguard.sh install` puts IN THE COMMON GIT DIR so
# it covers the primary checkout and every linked worktree the lanes work in.
#
# IT ALSO CHECKS THAT THE GUARD IS ARMED. This repo installs that hook into a
# checkout, not into the repo tree, so `git clean`/a fresh clone/an edited copy
# can leave it missing, non-executable or stale — a state in which the guard
# guards NOTHING while every other gate still reads green. `refguard check` makes
# that state a red gate (and reports the mirrors still present, without failing a
# branch that legitimately carries none). Offline and cheap (~10s): local bare
# remote plus clones, no network, no host state.
refguard_run() {
  bash "$REPO_ROOT/scripts/refguard_test.sh" || return 1
  # `check` is about the LIVE repo, so the gate asserts the hook is ARMED in the
  # shared git dir every worktree resolves. It deliberately does not fail on
  # mirrors existing: "the guard is armed" and "the namespace is clean" are two
  # questions, and failing the build on the second would fail every lane's
  # preflight for a cleanup that cannot be done from a PR.
  #
  # The repo to check is resolved via the COMMON git dir, not via the worktree: a
  # lane runs this from its own worktree, and `rev-parse --show-toplevel` there
  # would name the worktree, whose `scripts/` may be anything. The primary
  # checkout is where the hook has to be armed, because the hook itself is
  # read from the common git dir by every worktree.
  local common primary
  common="$(git -C "$REPO_ROOT" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
  [ -n "$common" ] || { echo "refguard: cannot resolve the common git dir" >&2; return 1; }
  primary="$(dirname "$common")"
  [ -d "$primary" ] || { echo "refguard: no primary checkout at $primary" >&2; return 1; }

  if [ ! -x "$primary/scripts/refguard.sh" ]; then
    # NOT a failure, and this asymmetry is deliberate. The guard ships as a PR:
    # before that PR is merged the file simply does not exist in the primary
    # checkout, so there is nothing to install or compare against. Failing here
    # would block EVERY lane's preflight on a change only a merge can land —
    # turning a safety net into a board-wide outage. It is announced loudly
    # rather than silently skipped, so the missing state is never invisible.
    echo "refguard: NOTE — $primary/scripts/refguard.sh is not present yet (the guard ships as a PR)."
    echo "refguard:        The suite above proves the guard works; it cannot be ARMED here until it is merged."
    return 0
  fi
  # `check` first, so a DIVERGENCE is reported (this branch may carry a different
  # rule from the one installed), then `ensure`, which arms it. Written as an
  # explicit if/else and NOT as `check ... || true`: the `selftest` gate forbids
  # `|| true` in any active line precisely so a gate cannot be quietly neutered,
  # and it caught an earlier draft of this line. Nothing here is swallowed —
  # every branch prints the check's output, and the final check below is the
  # assertion that decides the gate.
  local check_out
  if check_out="$(bash "$primary/scripts/refguard.sh" check "$primary" 2>&1)"; then
    printf '%s\n' "$check_out"
  else
    printf '%s\n' "$check_out"
    echo "refguard: the installed hook is missing, not executable, or differs from this" >&2
    echo "refguard: branch — (re)installing it from this branch's rule now." >&2
  fi
  if ! bash "$primary/scripts/refguard.sh" ensure "$primary"; then
    echo "refguard: could not arm the mirror guard for $primary" >&2
    return 1
  fi
  # Armed is not enough: the hook must actually be in place and current afterwards,
  # or the "installation" printed nothing useful and the gate would be certifying a
  # state it never reached.
  bash "$primary/scripts/refguard.sh" check "$primary" || {
    echo "refguard: the mirror guard is STILL not armed for $primary after ensure" >&2
    return 1
  }
}
run refguard "hand-made refs/remotes mirror guard (idiom refused incl. worktrees, normal flow untouched, hook armed)" refguard_run

# selftest — the gate runner's OWN self-test (scripts/preflight_test.sh). Same
# shape of gate as `monitor`/`tmpkey`, one level up: the only thing testable about
# the gate runner is its own safety properties, and they are exactly the ones that
# matter here — an all-skipped run must be REFUSED rather than reported green, a
# missing prerequisite must FAIL rather than silently skip, and the gate
# inventory must not drift from the gates that can actually run.
#
# WHY IT IS A GATE AND NOT JUST A SCRIPT: until this line existed,
# `grep -rn preflight_test scripts/ deploy/ Makefile` found no caller, so the
# self-test was never executed by anything — two of its checks had rotted
# (check 1 stopped testing anything, and the --list expectation was stale) and
# nothing noticed. A regression net nobody runs is not a net. It is offline and
# cheap (a sandbox repo plus a COPY of this script, so it never triggers a real
# build), so it can run inside every full gate.
selftest_run() {
  bash "$REPO_ROOT/scripts/preflight_test.sh"
}
run selftest "gate-runner self-test (empty run refused, missing prereq fails, no gate inventory drift)" selftest_run

summary

if [ ${#FAILED[@]} -gt 0 ]; then exit 1; fi
if [ ${#SKIPPED[@]} -gt 0 ]; then
  info "preflight green for the gates that ran — ${#SKIPPED[@]} gate(s) were SKIPPED, so this is NOT full coverage"
else
  info "preflight green: all ${#PASSED[@]} gates passed"
fi
