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
#              test       cd api && go test ./... -race -count=1 -timeout $API_TEST_TIMEOUT
#                         ($API_TEST_TIMEOUT defaults to 30m. WITHOUT it every
#                         package inherits Go's 10m per-package default and a
#                         green-but-slow package is reported as broken: measured
#                         on this host, `internal/httpapi` costs 601s on pristine
#                         main and 620s on the same host minutes later, so Go's
#                         own 10m budget reds a package that passes. The value is
#                         a CEILING, not a target — raising it does not hide a
#                         hang, it stops the hang being confused with host load.)
#              govuln     govulncheck over the module graph (CVE scan)
#   web        build      cd web && npm run build   (runs test:unit first)
#              lint       cd web && npm run lint    (eslint . — repo-wide)
#              webimage   docker build -f web/Dockerfile . — the web IMAGE
#                         build, i.e. the only command that produces the
#                         shipping web artifact. Until this line existed no gate
#                         built it: the preflight ran `npm run build` on the
#                         HOST, and the image build happened later in
#                         deploy/build-and-ship.sh, so a tree could be green
#                         through every gate with an image build that dies at
#                         `npm run build`. That is exactly the MCP-PARITY.md
#                         defect (absent from the build context -> ENOENT
#                         /repo/MCP-PARITY.md inside the image, host suite
#                         green) and the `context=web/` defect. Run with
#                         `--output type=cacheonly`: BuildKit executes every
#                         RUN, resolves every COPY and checks the `.dockerignore`
#                         filtering against the real context, but exports no
#                         image, so a full preflight does not leave a ~216 MB
#                         jomform-web tag behind on every card.
#              a11y       cd web && npm run a11y    (axe-core family only)
#              e2e        cd web && npm run e2e     (the feature specs under web/e2e)
#              npmaudit   cd web && npm audit --audit-level=high
#   lint       web        cd web && npm run lint    (eslint 9 + eslint-config-next
#                         flat config — the compiler-diagnostic rules that have no
#                         compiler here are off per-rule with the reason at each
#                         line; everything else that catches a real defect is on.
#                         `eslint .` EXITS NON-ZERO on an error, which is what
#                         makes this a gate rather than a report. ~46s on this
#                         host. Before it existed nothing checked the web tree
#                         for anything but types and the unit tests.)
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
#   scripts/preflight.sh                    # every gate (the DEFAULT — unchanged)
#   scripts/preflight.sh --list             # gate names, then exit
#   scripts/preflight.sh --only govuln      # just one gate (comma-separated)
#   scripts/preflight.sh --skip a11y,web    # everything except these
#   scripts/preflight.sh --for-diff <ref>   # only the gates <ref>..worktree can break
#   scripts/preflight.sh --quick            # INNER LOOP: api tests without -race
#   scripts/preflight.sh --explain-diff <ref>  # print that selection, run nothing
#   scripts/preflight.sh --gate-deps        # the per-gate path dependencies, then exit
#
#   --only/--skip exist for proving a single gate BITES (sabotage one thing,
#   run that gate, expect RED) and for re-running one gate after a fix. They are
#   NOT a routine bypass: a skip prints a loud WARNING block naming what did not
#   run. A gate that has never been seen red is not evidence.
#
#   --for-diff is the same shape of opt-in: it narrows the run to the gates the
#   diff can actually break, and it says in its own output what it skipped and
#   why. <base-ref> is EXPLICIT and required — a guessed default (origin/main) is
#   wrong on a stacked branch and would silently widen or narrow the run. The
#   DEFAULT (flag absent) is a full run. See the PATH-AWARE SELECTION section
#   below for the dependency rules and the fail-closed properties.
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
#   docker daemon               the webimage gate. If `docker info` cannot
#                               reach a daemon the gate is REFUSED (loudly),
#                               it does not skip: a skipped gate is not a
#                               passing gate, and this is the one gate that
#                               covers the shipping artifact. To run the rest
#                               of the preflight on a host with no daemon, say
#                               so explicitly with `--skip webimage` (which
#                               prints the NOT-RUN warning block).
#
# RUNTIME — LAST MEASURED 2026-10-03 on the agent host, one full 16-gate run on a
#   clean tree at 6 CPUs with no peer gate running, `16 passed, 0 failed,
#   0 skipped in 753s`: build 7s, vet 1s, test 279s, govuln 6s, web 45s,
#   webimage 73s, a11y 29s, e2e 123s, npmaudit 1s, gitleaks 9s, monitor 8s,
#   tmpkey 3s, backup 30s, prunewt 16s, refguard 4s, selftest 111s.
#
#   THAT IS ONE SAMPLE, NOT A BUDGET, and the error bar is wide by construction:
#   wall clock tracks host load and the gate set, not the tree. Across the runs
#   whose summaries are on this host the spread is minutes, not seconds — the
#   figures above were taken idle, and a contended host has been measured at
#   19-23 min for the same work.
#
#   TWO THINGS HERE ROT SILENTLY IF THEY ARE WRITTEN AS PROMISES, which is why this
#   block is dated and says what it was measured on:
#     * `test` dominates (279s of 753s here) and scales with host contention — it is
#       the gate that reds on a loaded host, not a slow one;
#     * `a11y` used to be quoted at 15s when it took 4m08s, because the gate ran the
#       WHOLE `web/e2e/` directory while its row described axe. It is now 29s
#       because the feature specs have their own gate (`e2e`, 123s) — so this
#       breakdown changed shape on 2026-10-03, and any figure copied from before
#       that date is wrong.
#
#   If you change the gate set or a gate's scope, the numbers here are stale: fix
#   them in the same commit, or delete them. Nothing checks them, deliberately —
#   unlike `--list`, wall clock is host- and load-dependent, so a guard would only
#   pin a value that is wrong the moment the machine is busier.
#   webimage: ~2m COLD (npm ci + next build in the image; measured 196s) and
#   ~3s warm, because the build cache carries it. So the gate is cheap on a
#   re-run and only pays for itself when something in the context changed —
#   which is precisely when it has something to say. `--no-cache-filter build`
#   is NOT used for that reason: it would pay the cold cost on every run to
#   catch a class the cache cannot hide (a missing or .dockerignore-excluded
#   COPY source still fails `failed to compute cache key` with a warm cache —
#   measured).
#
# CONCURRENCY: a bounded host-wide POOL of gate slots, not a single lock (card
#   t_79c0964a). `go test ./...` gives each test PROCESS its own scratch schema
#   (`test_<pkg>_p<pid>`) and its own Valkey key namespace (`jftest:p<pid>:`, on a
#   pid-derived DB) — both were built so that "several worktrees gate at once"
#   (the per-package-only schema name pulled tables out from under a peer:
#   `relation "workspaces" does not exist`, SQLSTATE 42P01). Nothing a gate
#   touches is shared, so a single host-wide lock no longer protected a resource:
#   it serialised LANES, and a second worker waited up to PREFLIGHT_LOCK_WAIT for
#   a gate it could have run in parallel (measured: a card held that lock 4h45m
#   for 4m42s of real CPU, 99 minutes of it `sleep`). The pool is
#   PREFLIGHT_SLOT_COUNT slots (default 2 — the board's own per-profile cap), so
#   two lanes gate in parallel while the host's CPU stays bounded. The one
#   contention symptom left is load, and the api `test` gate carries an explicit
#   30m `-timeout` CEILING so a loaded host has a bound it can report instead of a
#   hang to confuse it with. See the SLOT POOL section below the selection.
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

# `build` is the api build; `web` is `npm run build` in web/. `webimage` is the
# web IMAGE build (docker). All are named so --only/--skip can address them
# without ambiguity.
#
# ADDING A GATE: append the name here AND add its `run <name> ...` call below.
# scripts/preflight_test.sh derives both sides from this file (the inventory via
# `--list`, the runnable set from the `run <name>` call sites) and FAILS if they
# disagree, so a gate named here but never run — the silent-coverage-loss shape —
# is caught by `--only selftest`. No edit to the self-test is needed for gate #N.
GATE_NAMES=(build vet gofmt test govuln web lint webimage a11y e2e npmaudit gitleaks monitor tmpkey backup prunewt refguard selftest)

ONLY=""
SKIP=""
FOR_DIFF=""       # --for-diff <base-ref>: narrow to what <base-ref>..worktree can break
EXPLAIN_DIFF=""   # --explain-diff <base-ref>: print that selection, run nothing
GATE_DEPS=0       # --gate-deps: print the per-gate path dependencies, then exit
CLASSIFY=0        # --classify: read changed paths on stdin, print the gate set
CLASSIFY_PATHS="" # the paths --classify was given (comma-separated), if any
# WHETHER the flag was GIVEN, kept separately from its value. `--for-diff` with no
# argument leaves the value empty, which is indistinguishable from "flag absent" —
# so a bare `--for-diff` used to fall through to a full 7-minute gate run instead
# of being refused. Measured: that is exactly what happened.
FOR_DIFF_GIVEN=0
EXPLAIN_DIFF_GIVEN=0
# QUICK is the FAST INNER LOOP. It drops `-race` from the api `test` gate so a change
# can be iterated in ~10 min instead of ~13. It is NOT a substitute for the full run:
# `-race` is what makes the test gate a MERGE gate, and a quick run is never evidence
# that a change may merge or deploy. Measured on this host: the full `test` gate costs
# 299s and `internal/httpapi` alone accounts for 285s of it (785 tests, one schema per
# package, so it runs SERIALLY); without `-race` that package takes 173s.
QUICK=0
# SEL_DIFF is the narrowed gate set, SPACE-PADDED, and it stays EMPTY unless
# --for-diff/--explain-diff asked for one. selected() reads it, so it has to exist
# (empty) on every other path — an unset variable under `set -u` aborts the run
# before the first gate, which is how this was found.
#
# ${VAR:-} PRESERVES AN INHERITED VALUE, and that is load-bearing. The selection
# handoff is carried in the environment to a child (see the `--for-diff` branch
# below), so this prologue runs a SECOND time in that child while the selection
# block below skips recomputation (the exported `_PREFLIGHT_SEL` says it was
# already done). A plain `SEL_DIFF=""` therefore wiped the parent's narrowing and
# the child ran EVERY gate — a `--for-diff` run that silently stopped being
# narrowed. Measured, on a change under GATE-INVENTORY.md: the parent selected 7
# gates and the child ran all 14.
SEL_DIFF=""
# An EXPLICIT --only/--skip is this run's own instruction and beats a selection
# inherited from an enclosing --for-diff/--explain-diff run, which exists to answer
# a question about THAT run's diff. Measured (card t_fef2c0f7): a `selftest` gate
# fired from a --for-diff parent saw SEL_DIFF non-empty in every child it spawns,
# so `--only build` ran the 8 inherited gates and `--skipped` did not hit the
# all-skipped refusal. `--only build` runs ONLY build; `--skip <all>` is refused.
# The `--for-diff` flag itself is the one the gate is told about, and its argv never
# carries these flags, so no lane is affected. The parent run is untouched: it
# computes and exports the selection in the branch below, as before.
if [ -n "$ONLY" ] || [ -n "$SKIP" ] || [ -n "$CLASSIFY_PATHS" ]; then
  SEL_DIFF=""
  # Belt and braces: if this process is an explicit --only/--skip run, the handoff
  # marker must go too. It only ever says "the table was built once by the parent";
  # when this run is NOT narrowed by a diff, a stale marker has nothing to protect
  # and only invites a grandchild to re-inherit a selection this run already dropped.
  unset _PREFLIGHT_SEL
else
  SEL_DIFF="${SEL_DIFF:-}"
fi
# The rest of the selection handoff. Declared the same way, for the same reason —
# the banner and the summary read them on EVERY path (including the no-flag full
# run) without tripping `set -u`.
SEL_DIFF_LINE="${SEL_DIFF_LINE:-}"
SEL_DIFF_SKIPPED="${SEL_DIFF_SKIPPED:-}"
SEL_DIFF_FULL="${SEL_DIFF_FULL:-0}"
SEL_DIFF_REASON="${SEL_DIFF_REASON:-}"
SEL_COUNT="${SEL_COUNT:-0}"
SEL_BASE="${SEL_BASE:-}"

REUSE_RECORD=0
PREFLIGHT_LOG="${PREFLIGHT_LOG:-}"
# Gates this run REUSED from the per-tree record instead of re-running them.
# Declared here (not next to PASSED) because the reuse block below the lock fills
# it before the gate sections run; `summary` prints it on its own line so a
# remembered gate is never read as one this run observed.
REUSED=()
# The comma-separated gates this run REUSED, filled by the reuse block below the
# lock. Declared EMPTY here because the block reads it on paths where it stays
# empty (an `--only` selection disjoint from the record's vouched set), and an
# unset variable under `set -u` would abort the summary instead of printing it.
PREFLIGHT_REUSED=""

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
    # NOT acted on here: the selection machinery is defined further down, and a
    # `case` body cannot call a function that does not exist yet (measured — that
    # printed "command not found" for all three). The flag is recorded and served
    # after the machinery, before any gate body runs.
    --gate-deps) GATE_DEPS=1 ;;
    # Serving this needs the machinery, so it is deferred like --gate-deps. The
    # path list is given as a comma-separated argument so a caller can classify an
    # EXPLICIT set of paths without creating them on disk.
    --classify)   CLASSIFY=1 ;;
    --classify=*) CLASSIFY=1; CLASSIFY_PATHS="${1#*=}" ;;
    --only)      ONLY="${2:-}"; shift ;;
    --only=*)    ONLY="${1#*=}" ;;
    --skip)      SKIP="${2:-}"; shift ;;
    --skip=*)    SKIP="${1#*=}" ;;
    --for-diff)      FOR_DIFF="${2:-}"; FOR_DIFF_GIVEN=1; shift ;;
    --for-diff=*)    FOR_DIFF="${1#*=}"; FOR_DIFF_GIVEN=1 ;;
    --explain-diff)   EXPLAIN_DIFF="${2:-}"; EXPLAIN_DIFF_GIVEN=1; shift ;;
    --explain-diff=*) EXPLAIN_DIFF="${1#*=}"; EXPLAIN_DIFF_GIVEN=1 ;;
    # --reuse-record: the lane has already read the per-tree result record
    # (scripts/preflight_reuse.sh) and this run exists to re-run only what that
    # record could not vouch for. It does NOT widen what may be skipped: every
    # gate named here must still hold a recorded `pass` for THIS tree under THIS
    # environment fingerprint, or the run REFUSES rather than reporting a green
    # it did not earn. See the reuse block below the slot pool.
    --quick)          QUICK=1 ;;
    --reuse-record) REUSE_RECORD=1 ;;
    # --log <path>: where THIS run's output is being captured. Recorded in the
    # per-tree record so the next lane reads the evidence, not just the verdict —
    # and so a merged/hand-made summary can point at the run that observed it.
    --log)       PREFLIGHT_LOG="${2:-}"; shift ;;
    --log=*)     PREFLIGHT_LOG="${1#*=}" ;;
    *)           die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done

# The base ref is EXPLICIT and required. There is deliberately no default: the
# obvious candidate (`origin/main`) is WRONG on a stacked branch — this board
# carries several — and a wrong base silently widens or narrows the run, which is
# exactly the failure this whole feature has to avoid. An empty value is refused
# before the lock is taken, so a bad invocation fails fast instead of queueing.
if [ "$FOR_DIFF_GIVEN" = 1 ] && [ -z "${FOR_DIFF//[[:space:]]/}" ]; then
  die "--for-diff needs a base ref, e.g. --for-diff origin/main (there is no default: a guessed base is wrong on a stacked branch)"
fi
if [ "$EXPLAIN_DIFF_GIVEN" = 1 ] && [ -z "${EXPLAIN_DIFF//[[:space:]]/}" ]; then
  die "--explain-diff needs a base ref, e.g. --explain-diff origin/main"
fi
if [ "$FOR_DIFF_GIVEN" = 1 ] && [ "$EXPLAIN_DIFF_GIVEN" = 1 ]; then
  die "--for-diff and --explain-diff are mutually exclusive (--explain-diff runs nothing)"
fi
# Narrowing AND hand-picking at once is ambiguous: --only could ask for a gate the
# diff does not touch, and that gate would silently not run. Refuse the mix.
if [ "$FOR_DIFF_GIVEN" = 1 ] && { [ -n "$ONLY" ] || [ -n "$SKIP" ]; }; then
  die "--for-diff cannot be combined with --only/--skip (the diff decides the set; use one or the other)"
fi
if [ "$FOR_DIFF_GIVEN" = 1 ] && ! git rev-parse --verify --quiet "$FOR_DIFF^{commit}" >/dev/null 2>&1; then
  die "--for-diff base ref '$FOR_DIFF' does not resolve to a commit (a guess would silently widen or narrow the run)"
fi
if [ "$EXPLAIN_DIFF_GIVEN" = 1 ] && ! git rev-parse --verify --quiet "$EXPLAIN_DIFF^{commit}" >/dev/null 2>&1; then
  die "--explain-diff base ref '$EXPLAIN_DIFF' does not resolve to a commit"
fi

# ===========================================================================
# PATH-AWARE SELECTION — `--for-diff <base-ref>` and its two read-only
# companions `--explain-diff <base-ref>` and `--gate-deps`.
#
# OPT-IN BY CONSTRUCTION. The table below is built only inside the --for-diff
# branch; with no flag none of this executes and every gate runs exactly as
# before. A narrowed run never narrows silently: it prints what it skipped and
# why, in its own output (see the SKIPPED-by-diff block under the banner).
#
# WHY THIS IS A UNION OF "DECLARED AREA" AND "DERIVED READS", NOT A HAND TABLE.
# The area a gate is FOR is a judgement. What a gate actually READS is a property
# of the code, and this repo crosses areas in BOTH directions — measured, not
# assumed:
#
#   * `api/internal/httpapi/route_parity_test.go` walks `web/src/**`. A web-only
#     change that calls an unregistered `/api/*` path FAILS the api suite;
#     reproduced by adding one such file and running the guard, which went red
#     naming that exact path. A `test -> api/` rule alone would therefore turn a
#     red gate green.
#   * 19 web tests read `api/**` Go sources (cross-language contract guards) and
#     `web/src/lib/langPlacement.test.ts` reads the repo-root `MCP-PARITY.md`.
#   * the gate-INPUT files the card names (MCP-PARITY.md, GATE-INVENTORY.md,
#     AGENTS.md, deploy/backup/README.md) are prose that tests PARSE, so a
#     "docs are inert" rule silently disarms them.
#
# The declared table is kept (coarse, safe) and the derived rows are UNIONED in,
# so a cross-area read cannot be forgotten and the table cannot drift by hand.
#
# COST: pure bash plus four `git` calls (three greps and one ls-files), ~2s on a
# loaded host. No per-path forking: every norm/pathspec call writes through a
# nameref, because a subshell per candidate cost 3-20s on its own.
# ===========================================================================

# pw_norm_v <varname> <path> — lexical `..` normalisation, written through a
# nameref so the caller gets the result without a subshell.
#   No filesystem access: these strings are read OUT of source files and some are
#   deliberately not paths at all (an upload test carries "../../../../etc"),
#   so resolving them would be wrong as well as slow.
pw_norm_v() {
  local -n _pw_out="$1"
  local path="$2" seg
  local -a parts=() stack=()
  IFS='/' read -r -a parts <<<"$path"
  for seg in ${parts+"${parts[@]}"}; do
    case "$seg" in
      ""|".") continue ;;
      "..")
        if [ ${#stack[@]} -gt 0 ] && [ "${stack[${#stack[@]}-1]}" != ".." ]; then
          unset 'stack[${#stack[@]}-1]'
        else
          stack+=("..")
        fi ;;
      *) stack+=("$seg") ;;
    esac
  done
  _pw_out=""
  if [ ${#stack[@]} -gt 0 ]; then
    for seg in "${stack[@]}"; do _pw_out="${_pw_out}${_pw_out:+/}$seg"; done
  fi
}

# pw_pathspec_v <varname> <repo-rel-path> — the git pathspec for a candidate, or
# empty. Through a nameref for the same reason as pw_norm_v.
#   * outside the repo   -> empty: it cannot be an input to a run in this tree.
#   * a directory        -> "<dir>/*": reading a directory covers all of it.
#   * an existing file   -> the path.
#   * anything else      -> empty. git only reports paths that exist in one of
#     the two trees being compared, so a candidate that does not resolve here
#     cannot appear in the diff, and dropping it cannot narrow a live path away.
pw_pathspec_v() {
  local -n _pw_ps="$1"
  local p
  _pw_ps=""
  pw_norm_v p "$2"
  [ -n "$p" ] || return 0
  case "$p" in ..|../*) return 0 ;; esac
  if [ -d "$p" ]; then _pw_ps="${p%/}/*"
  elif [ -e "$p" ]; then _pw_ps="$p"
  fi
  return 0
}

# pw_declared <gate> — the AREA the gate is for. The card's dependency table.
#
# A gate with NO row here falls through to `*`, which means "cannot narrow —
# always run": a gate added to GATE_NAMES is then never silently unreachable.
# That fallback is a seatbelt, not the design — the `selftest` gate compares this
# table against `--list` (through `--gate-deps`) and fails when a gate is missing
# or has no pattern at all.
pw_declared() {
  case "$1" in
    build|vet|test|govuln) printf '%s\n' 'api/*' ;;
    web|a11y|e2e|lint)    printf '%s\n' 'web/*' ;;
    npmaudit)              printf '%s\n' 'web/package.json' 'web/package-lock.json' 'web/npm-audit-exceptions.json' 'web/scripts/npm-audit-gate.mjs' ;;
    monitor|tmpkey|backup|prunewt|refguard) printf '%s\n' 'deploy/*' 'scripts/*' ;;
    selftest)              printf '%s\n' 'scripts/*' 'GATE-INVENTORY.md' ;;
    gitleaks)              printf '%s\n' '*' ;;
    *)                     printf '%s\n' '*' ;;
  esac
}

# pw_sources <gate> — the files the gate EXECUTES. Only these are scanned for
# reads. Deliberately limited to what actually runs: a bite-proof or a helper no
# gate invokes is not a source of any gate, and listing one would widen the gate
# for a read nothing performs.
pw_sources() {
  case "$1" in
    build|vet|test|govuln) printf '%s\n' 'api/*' ;;
    web)                   printf '%s\n' 'web/package.json' 'web/src/*' 'web/next.config.mjs' 'web/tsconfig.json' 'web/scripts/web-deps-check.mjs' ;;
    lint)                  printf '%s\n' 'web/eslint.config.mjs' 'web/package.json' ;;
    a11y)                  printf '%s\n' 'web/e2e/a11y*.spec.ts' 'web/playwright.config.ts' ;;
    e2e)                   printf '%s\n' 'web/e2e/*.spec.ts' 'web/playwright.config.ts' ;;
    npmaudit)              printf '%s\n' 'web/package.json' 'web/package-lock.json' 'web/npm-audit-exceptions.json' 'web/scripts/npm-audit-gate.mjs' ;;
    monitor)               printf '%s\n' 'deploy/monitoring/*' ;;
    tmpkey)                printf '%s\n' 'deploy/tmp-apikey.sh' 'deploy/tmp-apikey_test.sh' ;;
    backup)                printf '%s\n' 'deploy/backup/*' ;;
    prunewt)               printf '%s\n' 'deploy/worktrees/*' ;;
    refguard)              printf '%s\n' 'scripts/refguard.sh' 'scripts/refguard_test.sh' ;;
    selftest)              printf '%s\n' 'scripts/preflight.sh' 'scripts/preflight_test.sh' ;;
    gitleaks)              printf '%s\n' '.gitleaks.toml' ;;
  esac
}

# The read SHAPES worth deriving. Measured on this repo, not guessed: these are
# the forms its Go, TypeScript and bash use to name a path outside the file's own
# directory. Shape 3 is the repo-root-relative literal trap the card names. A repo
# ROOT file named literally is the same trap one level up and is handled by
# pw_root_literals, which DERIVES the name set from the tree rather than listing
# the names here — see that function for why the list is not hand-maintained.
PW_READ_RE='(\.\./){2,}[A-Za-z0-9_@./-]*|filepath\.Join\([^)]*\)|"(api|web|deploy|scripts|docs|loadtest|e2e)/[A-Za-z0-9_@./*-]*"|\$(HERE|SCRIPT_DIR)/\.\./[A-Za-z0-9_./-]+'

# A repo-wide read (git grep / ls-files / rev-list over THIS checkout) makes a
# gate depend on every tracked file's CONTENT, so it can never narrow. It is
# deliberately conservative: a sandbox-only `git -C "$SB" rev-list` is flagged
# too, which costs a cheap suite an unnecessary run and CANNOT cost a red gate
# its run. The one live-repo instance is scripts/preflight_test.sh's doc sweep.
#
# IT IS APPLIED TO EXECUTABLE TEXT, NOT TO THE RAW LINE (pw_code_lines below).
# The check used to be `grep -cE "$PW_REPOWIDE_RE" <file>` over the RAW text, so
# a string literal that merely MENTIONS a git command counted as a read: the
# `t.Fatalf("git ls-files in %s: %v …")` message in
# api/internal/httpapi/embed_docs_snippet_test.go made the whole `api/*` area a
# whole-checkout reader, which marked build/vet/gofmt/test/govuln "cannot
# narrow" (9 '*' rows instead of 4) and turned the `selftest` classifier anchors
# RED on pristine origin/main — measured 2026-10-04, reproduced byte-identical on
# 3431e7cd, on PR 960's head and on the spine HEAD 1f553014. A read is a
# STATEMENT, not a substring, so the counting pipeline below strips each line to
# the code it runs before matching.
PW_REPOWIDE_RE='git[[:space:]]+(-C[[:space:]]+[^[:space:]]+[[:space:]]+)?(grep|ls-files|rev-list)[[:space:]]'

PW_SEL=""        # "<gate>\t<pathspec>" rows: the runtime dependency table
PW_BLOCKED=""    # gates that cannot narrow, one per line
PW_DEGRADED=0    # 1 -> derivation failed; every gate widens to `*`
declare -A PW_DERIVED=()  # file -> its candidate reads (newline separated)
declare -A PW_PS=()       # candidate -> its resolved pathspec ("" = unresolvable)
declare -A PW_RWSET=()    # file -> 1 when it reads the whole checkout
declare -A PW_CLAIMS=()   # gate -> paths it CLAIMS are its inputs (declared, no `*`)
declare -A PW_SRCS=()     # gate -> its source globs, one per line
PW_RW_FILES=()            # files that read the whole checkout

# pw_code_lines — reduce each line to the code it EXECUTES, so the repo-wide-read
# test reads a STATEMENT and not a substring.
#
# WHY. The repo-wide test used to grep the RAW line, so any mention of the shape
# counted as a read. The measured false positive (card t_bd79c727) was a
# `t.Fatalf("git ls-files in %s: %v …")` MESSAGE in
# api/internal/httpapi/embed_docs_snippet_test.go: it marked the whole `api/*`
# area a whole-checkout reader, so build/vet/gofmt/test/govuln all became
# "cannot narrow" (9 '*' rows instead of 4) and the `selftest` classifier anchors
# went RED on pristine origin/main (reproduced byte-identical on 3431e7cd, on
# PR 960's head and on the spine HEAD 1f553014). A message is not a read.
#
# WHAT IT DOES, and why the split is by LANGUAGE rather than uniform:
#   * every file: whole-line comments are dropped — `#`, `//`, `//*` and a
#     Go/TS block-comment continuation `* …` (exactly the set the old filter
#     removed, so a comment was already not a read and stays that way);
#   * a GO / TS / JS source ALSO has every quoted span dropped, because in those
#     languages a quoted string is DATA. That is the false-positive class: a
#     message that quotes the command. The set is the INTERPRETED pair `"…"`/`'…'`
#     AND the Go/TS BACKTICK raw string `` `…` `` — a raw string is exactly as
#     much DATA as an interpreted one, and leaving it out (card t_7e703b11, the
#     residual PR 961 left) made the reduction discriminate on QUOTE STYLE
#     rather than on "is this data": a message-only backtick literal degraded the
#     table to 9 '*' rows exactly as its `"…"` twin had before PR 961, turning
#     the `selftest` anchors RED. Removing those spans cannot hide a real read —
#     a Go read of the checkout in this repo is `exec.Command("git", "-C", root,
#     "ls-files", …)` (separate ARGS, no `git ls-files` substring at all), so the
#     pattern never saw it and this filter cannot lose it;
#   * a SHELL script keeps its quoted spans, because there `"$(git …)"` holds
#     CODE: the quote state is RE-OPENED inside a command substitution. A
#     quote-stripper that pairs the spans of
#     `MERGED_SHA="$(git -C "$REPO" rev-list …)"` (measured,
#     deploy/worktrees/prune-kanban-worktrees_test.sh:114) wrongly pairs the
#     inner and outer quotes, deletes the `rev-list`, and DROPS a genuine
#     repo-wide read — the unsafe direction, which would make `prunewt`
#     narrowable and let a narrowed run skip it. Shell therefore keeps the
#     pre-existing, more inclusive reading; only the non-shell shape narrows.
pw_code_lines() {
  case "$1" in
    *.go|*.ts|*.tsx|*.js|*.mjs|*.cjs)
      sed -e '/^[[:space:]]*\(#\|\/\/\|\/\*\|\*\)/d' -e 's/"[^"]*"//g' -e "s/'[^']*'//g" -e 's/`[^`]*`//g' "$1" 2>/dev/null ;;
    *)
      sed -e '/^[[:space:]]*\(#\|\/\/\|\/\*\|\*\)/d' "$1" 2>/dev/null ;;
  esac
}

# pw_cache_sources — resolve every gate's source globs ONCE. Both the ownership
# test and the repo-wide test call this per file per gate; without the cache that
# is ~950 subshells and was over 20s of the table build on its own.
pw_cache_sources() {
  local gate glob acc
  for gate in "${GATE_NAMES[@]}"; do
    [ -n "${PW_SRCS[$gate]+set}" ] && continue
    acc=""
    while IFS= read -r glob; do
      [ -n "$glob" ] || continue
      acc="${acc}${glob}
"
    done < <(pw_sources "$gate")
    PW_SRCS[$gate]="$acc"
  done
  return 0
}

pw_row() { PW_SEL="${PW_SEL}${1}	${2}
"; }
pw_blocked() { PW_BLOCKED="${PW_BLOCKED}${1}
"; }

# pw_derive_all — ONE awk pass over every line of every gate source that names a
# path, emitting "<file>\t<candidate>".
#   Every plausible base is tried for the ambiguous shapes because a wrong guess
#   NARROWS the run, which is the unsafe direction; pw_pathspec_v drops whatever
#   does not resolve, so the extra candidates cost nothing but a stat.
#   Comment lines are stripped: prose that quotes a path is documentation, not a
#   read — the same stripping the no-`|| true` check uses.
pw_derive_all() {
  git grep -nE "$PW_READ_RE" -- 'api/' 'web/' 'scripts/' 'deploy/' 2>/dev/null \
    | awk '
    function norm(p,    n,i,parts,st,stn,out) {
      n = split(p, parts, "/"); stn = 0; delete st
      for (i = 1; i <= n; i++) {
        if (parts[i] == "" || parts[i] == ".") continue
        if (parts[i] == "..") {
          if (stn > 0 && st[stn] != "..") { delete st[stn]; stn-- }
          else { stn++; st[stn] = ".." }
        } else { stn++; st[stn] = parts[i] }
      }
      out = ""
      for (i = 1; i <= stn; i++) out = out (i > 1 ? "/" : "") st[i]
      return out
    }
    function dirname(f,    i) {
      i = length(f); while (i > 0 && substr(f, i, 1) != "/") i--
      return (i > 1) ? substr(f, 1, i - 1) : ""
    }
    function emit(c) { if (c != "") print file "\t" c }
    {
      i = index($0, ":"); file = substr($0, 1, i - 1)
      rest = substr($0, i + 1); j = index(rest, ":")
      text = (j > 0) ? substr(rest, j + 1) : rest
      t = text; sub(/^[ \t]+/, "", t)
      if (t ~ /^(#|\/\/|\*|\/\*)/) next
      d = dirname(file)

      # 1. relative literals: ../../../web/src
      s = text
      while (match(s, /(\.\.\/){2,}[A-Za-z0-9_@.\/-]*/)) {
        lit = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
        emit(norm(d "/" lit))
      }
      # 2. filepath.Join(...) against every plausible base
      s = text
      while (match(s, /filepath\.Join\([^)]*\)/)) {
        call = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
        joined = ""; k = 0
        while (match(call, /"[^"]*"/)) {
          seg = substr(call, RSTART + 1, RLENGTH - 2); call = substr(call, RSTART + RLENGTH)
          joined = joined (k++ > 0 ? "/" : "") seg
        }
        if (joined != "") { emit(joined); emit("api/" joined); emit(norm(d "/" joined)) }
      }
      # 3. repo-root-relative literals: "api/internal/httpapi/rest.go"
      s = text
      while (match(s, /"(api|web|deploy|scripts|docs|loadtest|e2e)\/[A-Za-z0-9_@.\/-]*"/)) {
        lit = substr(s, RSTART + 1, RLENGTH - 2); s = substr(s, RSTART + RLENGTH)
        emit(lit); emit(norm(d "/" lit))
      }
      # 4. (reserved — the repo-ROOT-file literal shape is derived, not grepped
      #    here: see pw_root_literals. It cannot live in PW_READ_RE without
      #    widening the corpus to every quoted string in these trees.)
      # 5. bash: $HERE/../x and $SCRIPT_DIR/../x resolve from the script dir.
      #    $HERE IS the dirname of the file being scanned, so it is substituted
      #    with `d` and the `..` is left for norm() to collapse. Stripping the
      #    `../` and THEN prepending `d` counted the upward step twice and landed
      #    on the wrong base: $HERE/../AGENTS.md in scripts/preflight_test.sh came
      #    out as `scripts/AGENTS.md` (which does not exist) instead of the
      #    repo-root `AGENTS.md` — an UNDER-inclusion, the unsafe direction.
      s = text
      while (match(s, /\$(HERE|SCRIPT_DIR)\/\.\.\/[A-Za-z0-9_.\/-]+/)) {
        lit = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
        sub(/^\$[A-Z_]+\//, "", lit)
        emit(norm(d "/" lit))
      }
    }' 2>/dev/null
  pw_root_literals
  return 0
}

# pw_root_literals — the repo-ROOT files that a gate source names as a quoted
# literal, emitted in pw_derive_all's "<file>\t<candidate>" form.
#
# WHY THIS IS A DERIVATION AND NOT ONE MORE ALTERNATION ARM. It was an arm:
# `"(MCP-PARITY|GATE-INVENTORY|AGENTS)\.md"|"(Makefile|\.gitleaks\.toml|\.gitignore)"`.
# A hand-written list of names has to be extended by hand every time a gate starts
# reading a new repo-root file, and it was not — `BITE-HARNESSES.md` is read by
# api/internal/biteharness/inventory_test.go:64 through a `const` and was claimed
# by NO gate, so a diff touching it widened a one-gate run to all 15 (measured on
# 99c6aa79). The class is "a repo-root file a gate reads", so the set belongs to
# the tree, not to a list in this file.
#
# WHY IT IS A SEPARATE PASS. The name set is DERIVED first (`git ls-files` root
# entries) and the resulting names are then matched as WHOLE quoted literals over a
# narrowed corpus. Putting the shape into PW_READ_RE itself is not possible:
# an alternation of names is fine but a generic `"[A-Za-z0-9_.-]+"` arm there
# widens pw_derive_all's corpus from 392 to 38,602 lines (measured), because every
# string literal in api/, web/, scripts/ and deploy/ matches it — seconds of awk
# per invocation for lines whose candidates are then dropped as unresolvable. This
# pass reads only the files that MENTION a root-file name (the `git grep -lF` below)
# instead of all 38k.
#
# The literal is matched WHOLE (a string that merely CONTAINS a root name cannot
# contribute a candidate), which removes the earlier formulation's need to order
# the names longest-first: an unanchored alternation matched AGENTS.md inside the
# text "GATE-INVENTORY.md", and the resulting candidate is a file that exists here
# — so `selftest` was found claiming it. That variant also parsed `grep -aoHE`
# output as `file:line:text`, where the text is itself full of `:` (Go, YAML); awk
# is handed the FILES now, so there is no record to take apart.
#
# WHERE A BARE NAME RESOLVES IS THE WHOLE QUESTION, and the answer is already in
# this file — `pw_pathspec_v` resolves a candidate against the CURRENT directory
# first, because the tree is the repo root when the table is built. A gate source
# that names `"README.md"` is read the same way: relative to the SOURCE FILE's
# directory. So:
#   * `deploy/backup/jomform-backup_test.sh` naming "README.md" means
#     deploy/backup/README.md, NOT the root file — even though the root file
#     happens to exist. Treating it as the root file would CLAIM the root file for
#     the `backup` gate, and the root README.md is genuinely read by no gate, so a
#     claim on it would make the fail-closed branch weaker than the truth.
#   * `web/src/lib/nextManagedFiles.test.ts` naming "Makefile"/"AGENTS.md"/
#     ".gitignore" IS the root file: no sibling of those names exists under
#     web/src/lib, and the test says so in prose ("verified present at the worktree
#     root").
#   * `api/internal/biteharness/inventory_test.go` naming "BITE-HARNESSES.md" IS
#     the root file: no such sibling under api/internal/biteharness.
# The rule is therefore: emit the SAME-DIRECTORY candidate when a tracked file of
# that name sits beside the source (a genuine relative read, and the fan-out to it
# is required), AND the root candidate when no same-directory shadow exists. When a
# shadow does exist the root candidate is withheld: the bare name cannot be
# attributed to the root file, and widening is the safe direction.
#
# NO FAN-OUT TO NON-ROOT PATHS. The root candidate is the only one emitted, so a
# candidate never reaches `pw_pathspec_v` as some third path that exists merely
# because a name coincides (a name like `.next` would resolve against web/.next on
# a built tree and claim `test` for it; measured with the un-narrowed form). The
# same-directory candidate is NOT a guess of that kind: it is computed from the
# tree, and it is emitted only for a shadow that actually exists.
#
# `git grep -l` only NARROWS the corpus; membership is decided by the awk pass
# over that corpus, so a file it does not list cannot produce a candidate, and the
# two cannot disagree: `git grep -l` with no `--fixed-strings` matches a literal
# anywhere in the line, which is a superset of the whole-literal match.
pw_root_literals() {
  # `git ls-files | sort` is the whole input. `taken` is the set of every tracked
  # path; `shadow` is the set of NON-root tracked paths whose basename is a root
  # entry's name — a bare literal in that directory's source resolves to it first.
  local listing
  listing="$(git ls-files 2>/dev/null | sort)"

  # A root ENTRY, not a path: no `/` means the root (`.`/`..` excluded so a name
  # can never walk upward). `git ls-files` lists tracked entries only.
  local -a names=()
  local name
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    names+=("$name")
  done < <(printf '%s\n' "$listing" | awk 'index($0, "/") == 0 && $0 != "." && $0 != ".."')

  [ "${#names[@]}" -gt 0 ] || return 0

  # The corpus: every gate source that mentions ANY root-file name at all.
  #
  # WHY THE CORPUS IS MATCHED WITH A LITERAL SEARCH (`-lF`, one `-e` per name) AND
  # NOT AN ERE ALTERNATION. It was one: the names were escaped per character with
  # `sed 's/[][\.^$*/]/\\&/g'` and pasted into a single `git grep -lE "(A|B|…)"`.
  # That bracket class covers `] [ \ . ^ $ * /` and NOT the rest of the ERE set —
  # `+ ? ( ) { } |` are all ERE-active (repetition, grouping, alternation). So a
  # root file named `QA+PROBE.md` escaped to `QA\+PROBE\.md` if the class were
  # complete, but under the incomplete one to `QA+PROBE\.md`, where `+` still means
  # "one or more of the preceding character": the alternation matched
  # `QAPROBE.md` — a name that does not exist — and NOT the real file.
  # Measured on the escaped-alternation form: the corpus came back EMPTY for
  # `QA+PROBE.md` and `--classify='QA+PROBE.md'` widened a one-gate run to all 15.
  # A per-character escape is also a losing game by construction: the set it must
  # cover is every metacharacter grep's ERE engine honours, and a missing member is
  # silent. `-F` removes the question — there is no pattern language left to escape
  # INTO — and `-e` keeps each name its own literal pattern (ANY-match, exactly the
  # union the alternation expressed). It is also a superset-free simplification: a
  # name containing `|` or `/` cannot change the meaning of its neighbours.
  #
  # `-lF` can only NARROW the corpus (a file matching none of the patterns is not
  # listed); membership is still decided by the awk pass below, which tests the
  # exact literal, so the two verdicts cannot disagree — the corpus is a superset
  # of the whole-quoted-literal match.
  local -a grep_args=()
  for name in "${names[@]}"; do grep_args+=(-e "$name"); done

  local -a files=()
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    files+=("$name")
  done < <(git grep -lF "${grep_args[@]}" -- 'api/' 'web/' 'scripts/' 'deploy/' 2>/dev/null)

  [ "${#files[@]}" -gt 0 ] || return 0

  printf '%s\n' "$listing" | awk \
    -v list="$(printf '%s ' "${names[@]}")" \
    -v flist="$(printf '%s\n' "${files[@]}")" '
    BEGIN {
      n = split(list, a, " ")
      for (i = 1; i <= n; i++) if (a[i] != "") root[a[i]] = 1
    }
    # Pass 1: the tracked listing splits into the root names (known from `list`)
    # and the non-root files that SHADOW one of those names.
    FILENAME == "-" {
      p = $0
      if (p == "") next
      if (index(p, "/") == 0) next
      n = split(p, seg, "/")
      if (seg[n] in root) shadow[p] = 1
      next
    }
    # Pass 2: the gate sources.
    {
      line = $0
      src = FILENAME
      d = src; sub(/\/[^\/]*$/, "", d); if (d == src) d = ""
      # A comment line is prose, not a read — the same stripping pw_derive_all and
      # the no-`|| true` check use.
      t = line; sub(/^[ \t]+/, "", t)
      if (t ~ /^(#|\/\/|\*|\/\*)/) next
      while (match(line, /"[^"]*"/)) {
        lit = substr(line, RSTART + 1, RLENGTH - 2)
        line = substr(line, RSTART + RLENGTH)
        if (!(lit in root)) continue
        # The sibling first: a bare name in a source that has one of its own
        # resolves there, and that file is a real read of THIS gate.
        sib = (d == "") ? lit : (d "/" lit)
        if (sib in shadow) { print src "\t" sib; continue }
        # No shadow, so the bare name is the repo-root file.
        print src "\t" lit
      }
    }' - "${files[@]}" 2>/dev/null
  return 0
}

# pw_build_table — the whole dependency table, built once, before any gate runs.
pw_build_table() {
  PW_SEL=""; PW_BLOCKED=""; PW_DEGRADED=0
  PW_DERIVED=(); PW_PS=(); PW_RWSET=(); PW_CLAIMS=()
  local gate pat file cand ps g p rows="" derived
  local -a cands=()

  pw_cache_sources
  derived="$(pw_derive_all)"
  local nc nsrc
  nc="$(printf '%s\n' "$derived" | grep -c '[^[:space:]]')"

  # A deriver that finds NOTHING while there are sources to scan has not proved
  # "no dependencies" — it has FAILED, and a silent failure here would narrow
  # every gate. Widen all of them and announce it.
  nsrc="$(git ls-files -- 'api/' 'web/' 'scripts/' 'deploy/' 2>/dev/null | grep -c '[^[:space:]]')"
  if [ "$nsrc" -gt 0 ] && [ "$nc" -eq 0 ]; then
    PW_DEGRADED=1
    for gate in "${GATE_NAMES[@]}"; do
      pw_row "$gate" '*'
      pw_blocked "$gate"
    done
    return 0
  fi

  while IFS=$'	' read -r file cand; do
    [ -n "${file:-}" ] || continue
    [ -n "$cand" ] || continue
    PW_DERIVED[$file]="${PW_DERIVED[$file]:-}${cand}
"
  done <<<"$derived"

  # Which files read the whole checkout. The line is reduced to its code first
  # (pw_code_lines): a MESSAGE or a COMMENT that mentions the command is not a
  # read, and counting it made every `api/*` gate un-narrowable (card t_bd79c727).
  #
  # WHY THE PATTERN IS COUNTED, NOT PIPED INTO `grep -q`. This was a `grep ... |
  # grep -qE ...` and it is nondeterministic under `set -o pipefail`: when the
  # pattern matches, `grep -q` exits at the first match and the writer dies of
  # SIGPIPE (141), and pipefail then reports the pipeline as FAILED even though
  # it matched. Measured 2 misses in 40 runs of the same line on the same file.
  # The failure is a MISSED repo-wide read, i.e. the file is not added to
  # PW_RW_FILES, so the gate that reads the whole checkout is treated as
  # NARROWABLE and can be dropped from a narrowed run — the unsafe direction, and
  # `prunewt` is the gate it hits. Reading the output into a variable makes the
  # verdict depend on the match count, which cannot race.
  # (Same defect class as the 13 sites fixed in scripts/preflight_test.sh.)
  local rw_hits
  PW_RW_FILES=()
  for file in $(git grep -lE "$PW_REPOWIDE_RE" -- 'api/' 'web/' 'scripts/' 'deploy/' 2>/dev/null); do
    rw_hits="$(pw_code_lines "$file" | grep -cE "$PW_REPOWIDE_RE")"
    if [ "${rw_hits:-0}" -gt 0 ]; then
      PW_RWSET[$file]=1
      PW_RW_FILES+=("$file")
    fi
  done

  # Per gate: the declared area, then the derived reads of ITS OWN sources.
  # Membership is a `case` match against the gate's source globs — no file list is
  # enumerated here, because `git ls-files` per glob per gate cost ~0.7s and
  # building a per-gate file list cost a fork per file.
  for gate in "${GATE_NAMES[@]}"; do
    # A DECLARED `*` means "this gate cannot narrow — always run". It has to be
    # turned into the BLOCKED verdict here, not merely stored as a pattern:
    # the `*` row is not a claim on every path (that would make the
    # unclaimed-path check dead code), so a declared-`*` gate that is not ALSO
    # blocked claims nothing and is dropped from every narrowed run.
    # Measured: gitleaks — which scans the whole git history and is the last gate
    # that may be skipped — vanished from a web-only selection that way.
    local declared_blocked=0
    while IFS= read -r pat; do
      [ -n "$pat" ] || continue
      if [ "$pat" = '*' ]; then declared_blocked=1; continue; fi
      pw_row "$gate" "$pat"
    done < <(pw_declared "$gate")
    if [ "$declared_blocked" = 1 ]; then
      pw_blocked "$gate"
      pw_row "$gate" '*'
      continue
    fi

    # A gate whose sources read the whole checkout cannot narrow: its inputs are
    # every tracked file's CONTENT, which no path list can express.
    local rw_glob blocked=0
    for rw_glob in ${PW_RW_FILES+"${PW_RW_FILES[@]}"}; do
      [ -n "$rw_glob" ] || continue
      pw_matches_gate "$gate" "$rw_glob" && { blocked=1; break; }
    done
    if [ "$blocked" = 1 ]; then
      # The gate's DECLARED area is still recorded as a claim (above), so a path
      # in it is known; the gate itself just always runs.
      pw_blocked "$gate"
      pw_row "$gate" '*'
      continue
    fi

    while IFS= read -r file; do
      [ -n "$file" ] || continue
      case "$file" in
        */*) ;;
        *) continue ;;
      esac
      pw_matches_gate "$gate" "$file" || continue
      [ -n "${PW_DERIVED[$file]:-}" ] || continue
      while IFS= read -r cand; do
        [ -n "$cand" ] || continue
        cands+=("$cand")
        pw_row "$gate" "RAW${cand}"
      done <<<"${PW_DERIVED[$file]}"
    done < <(printf '%s\n' "${!PW_DERIVED[@]}")

  done

  # Resolve every UNIQUE candidate exactly once. Per-row resolution re-statted the
  # same path up to 14 times (measured 3.3s for one gate's 78 candidates).
  local u
  for u in ${cands+"${cands[@]}"}; do
    [ -n "$u" ] || continue
    [ -n "${PW_PS[$u]+set}" ] && continue
    pw_pathspec_v ps "$u"
    PW_PS[$u]="$ps"
  done

  while IFS=$'	' read -r g p; do
    [ -n "${g:-}" ] || continue
    case "$p" in
      RAW*)
        ps="${PW_PS[${p#RAW}]:-}"
        [ -n "$ps" ] && rows="${rows}${g}	${ps}
" ;;
      *) rows="${rows}${g}	${p}
" ;;
    esac
  done <<<"$PW_SEL"
  PW_SEL="$(printf '%s' "$rows" | grep -v '^$' | sort -u)"

  # PW_CLAIMS is built from the RESOLVED rows, not the raw candidates. A candidate
  # that names a DIRECTORY resolves to `<dir>/*`, and it is that pathspec which
  # matches a changed file — storing the bare candidate made `web/src` claim
  # nothing under it, so a change to web/src/app/page.tsx was reported as claimed
  # by no gate at all. Measured: that pushed a one-file web change from the web
  # gates to a 14-gate full run.
  #
  # `*` is EXCLUDED. It is the always-run row, not a claim on every path; keeping
  # it would make every path look claimed and turn the fail-closed branch into
  # dead code.
  while IFS=$'	' read -r g p; do
    [ -n "${g:-}" ] || continue
    [ -n "$p" ] || continue
    [ "$p" = '*' ] && continue
    PW_CLAIMS[$g]="${PW_CLAIMS[$g]:-}${p}
"
  done <<<"$PW_SEL"
  return 0
}

# pw_matches_gate <gate> <file> — does this gate's source set include this file?
#   Reads the CACHED glob list: this is called once per derived file per gate.
pw_matches_gate() {
  local gate="$1" f="$2" g
  while IFS= read -r g; do
    [ -n "$g" ] || continue
    case "$f" in $g) return 0 ;; esac
  done <<<"${PW_SRCS[$gate]:-}"
  return 1
}

# pw_gate_blocked <gate> — can this gate narrow at all?
#
# NOTE the explicit `return 1`: a `case` with NO matching arm exits 0 in bash, so
# a case-as-predicate without a final return reports "matched" for every input.
# That error made every gate report `*` the first time this ran.
#
# THE TRAILING NEWLINE IN THE PATTERN IS LOAD-BEARING, AND SO IS THE LEADING ONE.
# `$(...)` strips trailing newlines, so wrapping the list that way turned the LAST
# entry into a bare name with nothing after it — `*"$1\n"*` could never match it.
# Measured: `selftest` was recorded as unable to narrow (its own sources run
# `git grep` over the whole checkout) but answered "no" here, so it was treated as
# narrowable, its `*` row was skipped by pw_claims, and the gate dropped out of a
# diff that edits AGENTS.md — which that gate really does read. A red gate would
# have gone green. The leading newline matters for the same class of error in the
# other direction: "selftest\n" CONTAINS "test\n", so a mid-list `test` arm
# matched a list that never named it and `test` was wrongly marked unable to
# narrow. Both are substring traps; both sides are anchored now.
pw_gate_blocked() {
  [ "$PW_DEGRADED" = 1 ] && return 0
  [ -n "$PW_BLOCKED" ] || return 1
  # Prepend a newline so the FIRST entry is anchored like every other one.
  # pw_blocked always appends "<gate>\n", so the list is "\ng1\ng2\n…gn\n" here.
  local bl=$'\n'"${PW_BLOCKED}"
  case "$bl" in
    *$'\n'"$1"$'\n'*) return 0 ;;
  esac
  return 1
}

# pw_claims <gate> <changed-path> — is this changed path an input to this gate?
#
# Read from PW_CLAIMS, and the distinction is what keeps the fail-closed check
# alive. A gate that CANNOT narrow always runs, so its declared area still tells
# us the path is KNOWN — it is not an unknown path, and it must not trip the
# "no gate claims this" branch that widens the whole run. Measured: editing
# GATE-INVENTORY.md (claimed by `selftest`, which reads the whole checkout and so
# is always-run) was reported as claimed by NO gate and forced all 14 gates, which
# makes a common edit pay the full run for nothing. PW_CLAIMS carries the
# non-`*` declared area of EVERY gate plus the derived reads of the ones that can
# narrow, so a known path is recognised while a genuinely unknown one still
# widens the run.
pw_claims() {
  local gate="$1" c="$2" pat
  while IFS= read -r pat; do
    [ -n "$pat" ] || continue
    case "$c" in $pat) return 0 ;; esac
  done <<<"${PW_CLAIMS[$gate]:-}"
  return 1
}

# pw_changed_paths <base> — every path the diff touches, one per line.
#   --no-renames reports a rename as its delete PLUS its add: both names are
#   changes to files a gate may read.
#   Untracked (not ignored) files are included: a file this tree carries and the
#   base does not is exactly what a gate must see.
pw_changed_paths() {
  git diff --name-only --no-renames "$1" 2>/dev/null
  git ls-files --others --exclude-standard 2>/dev/null
  return 0
}

# pw_classify — stdin: the changed paths. stdout: "<gate>" lines, then a
# "#reason: <text>" line. This is the whole fail-closed policy and it is split out
# from pw_selection so the selftest can drive it with an EXPLICIT path list,
# without mutating a real tree (see --classify).
#
# FAIL-CLOSED in one place:
#   * an empty path list -> FULL (an empty list is not evidence);
#   * a changed path NO narrowable gate claims -> FULL, naming the path;
#   * an empty selection -> FULL.
# Gates that cannot narrow are added to every selection: they always run.
pw_classify() {
  local changed c gate claimed="" unclaimed="" hit=0 reason=""

  changed="$(cat)"
  if [ -z "$(printf '%s' "$changed" | tr -d '[:space:]')" ]; then
    reason="no changed path was given"
    printf '%s\n' "${GATE_NAMES[@]}"
    printf '#reason: %s\n' "$reason"
    return 0
  fi

  while IFS= read -r c; do
    [ -n "$c" ] || continue
    hit=0
    for gate in "${GATE_NAMES[@]}"; do
      pw_claims "$gate" "$c" && hit=1
    done
    [ "$hit" = 0 ] && unclaimed="${unclaimed}${c} "
  done <<<"$changed"

  if [ -n "$unclaimed" ]; then
    reason="no gate claims ${unclaimed% }"
    printf '%s\n' "${GATE_NAMES[@]}"
    printf '#reason: %s\n' "$reason"
    return 0
  fi

  for gate in "${GATE_NAMES[@]}"; do
    if pw_gate_blocked "$gate"; then claimed="${claimed}${gate}
"; continue; fi
    while IFS= read -r c; do
      [ -n "$c" ] || continue
      if pw_claims "$gate" "$c"; then claimed="${claimed}${gate}
"; break; fi
    done <<<"$changed"
  done
  if [ -z "$(printf '%s' "$claimed" | tr -d '[:space:]')" ]; then
    reason="the selection came out empty"
    printf '%s\n' "${GATE_NAMES[@]}"
    printf '#reason: %s\n' "$reason"
    return 0
  fi
  reason="narrowed to the gates the diff can break"
  printf '%s' "$claimed"
  printf '#reason: %s\n' "$reason"
  return 0
}

# pw_selection <base> — the changed paths from git, classified.
#
# THE REASON TRAVELS ON STDOUT ON PURPOSE. This is called in a command
# substitution (the caller needs the gate list to build a space-padded set), and a
# command substitution runs in a SUBSHELL — a global set inside the function does
# not reach the caller. Printing it keeps the whole result in one channel, so
# there is nothing to fall out of sync.
#
# An empty diff (unknown ref, no git, shallow clone, nothing changed) reaches the
# same FULL branch as an empty list here.
pw_selection() {
  pw_changed_paths "$1" | pw_classify
  return 0
}

# pw_gate_deps — the DOCUMENTED per-gate dependency table, one line per gate:
# "<gate>: <pattern> <pattern> …", where `*` means "cannot narrow, always runs".
#
# THIS IS THE TABLE THE SELFTEST CHECKS, and it is deliberately the DECLARED area
# plus the CANNOT-NARROW verdict — not the full derived set. The derived rows are
# a runtime safety net read out of the sources; they move whenever the sources
# move, so pinning them here would make every read added anywhere a red test. What
# must be checked is the part a human maintains: every gate has an area, and a gate
# that cannot narrow says so.
pw_gate_deps() {
  local gate pats
  for gate in "${GATE_NAMES[@]}"; do
    if pw_gate_blocked "$gate"; then
      printf '%s: *\n' "$gate"
      continue
    fi
    pats="$(pw_declared "$gate" | tr '\n' ' ')"
    printf '%s: %s\n' "$gate" "${pats% }"
  done
  return 0
}

# selected() — is this gate in the requested set?
#
# With --for-diff, SEL_DIFF holds the gates the diff can break (built once, above)
# and that set is the selection; ONLY/SKIP are refused in combination with it, so
# the two cannot disagree here.
selected() {
  local gate="$1"
  if [ -n "$SEL_DIFF" ]; then
    case "$SEL_DIFF" in *" $gate "*) return 0 ;; *) return 1 ;; esac
  fi
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

# ---- PATH-AWARE SELECTION (--for-diff / --explain-diff) --------------------
#
# Served here (the machinery is only defined at this point in the file) and before
# the slot pool, so it costs nothing and never queues.
if [ "$GATE_DEPS" = 1 ]; then
  # The cannot-narrow verdict is part of the table, so the table still has to be
  # built — but for a gate that can narrow, the DOCUMENTED area is what is printed.
  pw_build_table
  pw_gate_deps
  exit 0
fi

# --classify <paths> — the classification for an EXPLICIT path list, with nothing
# run and nothing created. <paths> is comma-separated; with no argument the paths
# are read from stdin instead. This exists so the safety properties (the trap
# files, fail-closed, the bite) can be asserted by the `selftest` gate in a sandbox
# without writing probe files into a real tree.
if [ "$CLASSIFY" = 1 ]; then
  pw_build_table
  if [ -n "$CLASSIFY_PATHS" ]; then
    printf '%s\n' "$CLASSIFY_PATHS" | tr ',' '\n' | pw_classify
  else
    pw_classify
  fi
  exit 0
fi

# Evaluated HERE, before the concurrency lock is taken. Two reasons:
#   * a bad invocation (a ref that is not a commit) must fail fast rather than
#     queue behind another gate for up to PREFLIGHT_LOCK_WAIT and then fail;
#   * --explain-diff runs no gate, so it must never take the lock at all.
#
# The computed selection is EXPORTED, and the locked child re-runs this script
# with the same argv — so the child must not rebuild the table. `_PREFLIGHT_SEL`
# is that handoff: it is set here and inherited, which makes the build happen
# exactly once per invocation instead of twice (the table costs ~4s).
#
# SEL_DIFF is a space-padded "<gate> <gate> " list. It is EMPTY on every
# invocation that does not ask for a diff, which is what keeps the DEFAULT a full
# run: selected() only consults it when it is non-empty.
if [ -z "${_PREFLIGHT_SEL:-}" ] && { [ "$FOR_DIFF_GIVEN" = 1 ] || [ "$EXPLAIN_DIFF_GIVEN" = 1 ]; }; then
  SEL_BASE="${FOR_DIFF:-$EXPLAIN_DIFF}"
  pw_build_table
  sel_out="$(pw_selection "$SEL_BASE")"
  SEL_DIFF_REASON="$(printf '%s\n' "$sel_out" | sed -n 's/^#reason: //p' | sed -n '1p')"
  # LEADING space as well as trailing: selected() matches `*" $gate "*`, so a
  # first gate with nothing before it would never match. Measured — the first
  # selected gate came out as skipped and appeared in both lists.
  SEL_DIFF=" "
  SEL_COUNT=0
  while IFS= read -r sg; do
    [ -n "$sg" ] || continue
    case "$sg" in '#reason:'*) continue ;; esac
    SEL_DIFF="${SEL_DIFF}${sg} "
    SEL_COUNT=$((SEL_COUNT + 1))
  done <<<"$sel_out"
  SEL_DIFF_LINE="${SEL_DIFF% }"
  SEL_DIFF_SKIPPED="$(for gate in "${GATE_NAMES[@]}"; do selected "$gate" || printf '%s ' "$gate"; done)"
  SEL_DIFF_SKIPPED="${SEL_DIFF_SKIPPED% }"
  # A selection that came out as the whole inventory was produced by the
  # fail-closed paths (unknown ref / unclaimed path), not by "the diff touches
  # everything" — those are different facts and only one of them is a narrowing.
  if [ "$SEL_COUNT" -eq "${#GATE_NAMES[@]}" ]; then
    SEL_DIFF_FULL=1
  else
    SEL_DIFF_FULL=0
  fi
  export SEL_DIFF SEL_DIFF_LINE SEL_DIFF_SKIPPED SEL_DIFF_FULL SEL_COUNT SEL_DIFF_REASON SEL_BASE
  export _PREFLIGHT_SEL=1
  if [ -n "$EXPLAIN_DIFF" ]; then
    echo "preflight --explain-diff $SEL_BASE"
    echo "  changed paths: $(pw_changed_paths "$SEL_BASE" | grep -c '[^[:space:]]')"
    echo "  gates selected ($SEL_COUNT of ${#GATE_NAMES[@]}): $SEL_DIFF_LINE"
    echo "  reason: $SEL_DIFF_REASON"
    if [ "$SEL_DIFF_FULL" = 1 ]; then
      echo "  FULL RUN — nothing was narrowed"
    else
      echo "  would SKIP (a gate that did not run is not evidence): $SEL_DIFF_SKIPPED"
    fi
    exit 0
  fi
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

# ---- SLOT POOL: bound how many gate runs this HOST does at once -------------
#
# A POOL, NOT A LOCK. The single host-wide lock this replaces said two concurrent
# `preflight.sh` runs "share one scratch schema and one scratch valkey index, so
# they corrupt each other's fixtures and produce FALSE failures". THAT PREMISE IS
# NO LONGER TRUE, and the lock was therefore no longer protecting anything — it
# was only serialising LANES:
#
#   * the scratch schema is `test_<pkg>_p<pid>` (api/internal/testdb/testdb.go,
#     pkgName) — per process, not per package, and its own package doc says the
#     pid is there so that "two concurrent full suites — the normal state of this
#     board, where several worktrees gate at once — both DROP and re-create
#     `test_<pkg>` … whichever re-creates it second pulls the tables out from
#     under the other" cannot happen. A schema left by a dead process is pruned
#     on the next Open, and only when no other connection exists;
#   * the Valkey namespace is `jftest:p<pid>:` on a pid-derived DB number
#     (api/internal/testdb/valkey.go, Prefix/Keyspace), with per-test keys under
#     it — no fixture in the repo FlushDBs anything any more.
#
# So what is left is a HOST CAPACITY question, and the answer to a capacity
# question is a CAPPED POOL, not a mutex: two lanes gate in parallel and the
# machine still has a bounded number of them. PREFLIGHT_SLOT_COUNT defaults to 2,
# which is the board's own per-profile dispatch cap — the host is sized for that,
# and raising it is a capacity decision rather than a correctness one.
#
# WHAT WAS MEASURED (card t_79c0964a, 2026-10-04): two jf-dev cards ran
# concurrently and did NOT overlap, because they contended for one lock.
# t_bd79c727 ran 4h45m with 4m42s of real CPU (2.8%), 12 commands at >=250s and
# 99 minutes of `sleep`; t_7b2ddf56's log carried `lock: another gate is running
# — waiting up to 1800s`. A waiter on that lock has been measured at 50-86
# minutes.
#
# WHY A FILE LOCK AND NOT A GATE CHANGE: the gates are correct as written; the
# old defect was two of them running at once WITHOUT A BOUND. A slot pool needs no
# edit to any gate, and it costs nothing when the host is idle or has a free slot.
#
# WHY WAIT INSTEAD OF FAIL: refusing would turn an idle-host problem into a red
# gate for whoever found all slots busy — the false-red shape this exists to
# avoid. Waiting is correct: the gate runs, on the same tree, once a slot frees.
#
# SCOPE: bounds the GATE BODY only. `--list`, argument validation and the banner
# stay outside, so a queued run can still be inspected and a typo still fails
# immediately rather than after a wait.
#
# SLOT FILES: one file per slot, `<lock path>.slot<N>`. `/var/lock` is tmpfs on
# Linux and is wiped on reboot. That is deliberate — a slot file left behind by a
# crash must never block future runs, and flock releases automatically when the
# holder dies, so there is nothing to clean up either way.
#
# ONE PROCESS MAY HOLD ONE SLOT. Two shapes could take a second, and both are
# handled explicitly rather than left to chance:
#   * `PREFLIGHT_LOCK_FILE` naming a path whose basename is itself a slot file (a
#     caller who read the slot directory and copied a name out of it) — refused
#     outright by the basename test below, which names PREFLIGHT_SLOT_COUNT as the
#     supported knob. Such a run would otherwise WAIT on the one slot it named;
#   * a nested `./scripts/preflight.sh` under a run that already holds one (the
#     `selftest` gate does exactly this, with its own private lock path) — the
#     INHERITED slot descriptor plus the recorded holder pid are the signature, and
#     the nested run says "already inside the pool" and runs its gates rather than
#     waiting for a slot its own ancestor is holding. It CANNOT simply re-take the
#     slot: flock refusal is open-file-description scoped, so an inherited fd is
#     not a lock and a fresh open by any process is refused (measured).
# Both are stated here because the previous design had a real self-deadlock
# (card t_0094bf00: a lane's queue grew 3 -> 9 runs with no gate executing) and
# the fix must not be re-introduced from the other side.
#
# THE ONE THING THAT MUST HAPPEN BEFORE THE POOL IS A WHOLE-INVENTORY REUSE.
# A full-green record IS the verdict for these bytes, so re-reading it needs no
# host time at all — and the card's acceptance is explicit that this path must not
# queue: a lane about to re-gate an already-green tree is the 39.4% being removed,
# and waiting on the gate is precisely the cost reuse exists to avoid. So the
# early exit is taken HERE, before any slot, and it holds only inside a
# whole-inventory reuse that `gr_reusable` has vouched for (same tree, same
# fingerprint, in TTL, every gate an observed pass). Everything else — a partial
# record, a red one, no record at all — falls through to the pool and runs as
# before. See the reuse block below the pool for the per-gate path.
if [ "${_PREFLIGHT_LOCKED:-}" != "1" ] && [ "$REUSE_RECORD" = 1 ]; then
  _pre_lib="${PREFLIGHT_RESULT_LIB:-$REPO_ROOT/scripts/gate_result.sh}"
  if [ -r "$_pre_lib" ] && [ -r "$REPO_ROOT/scripts/preflight_reuse.sh" ]; then
    # The consumer is the SINGLE source of truth for the decision, so the
    # pre-pool path cannot drift from the in-pool one. It is asked here only
    # when reuse was explicitly requested; otherwise the runner never sources it.
    #
    # NO test-env.sh SOURCING HERE, deliberately. The fingerprint contains
    # JOMFORM_TEST_DATABASE_URL, so sourcing a file that the CALLER did not source
    # would compute a different key from the one the consumer just computed for the
    # same tree — the pre-pool path would then refuse a record the consumer had
    # just vouched for, and the two paths would disagree about what is reusable.
    # Both read the caller's environment and nothing else.
    # shellcheck source=scripts/gate_result.sh
    . "$_pre_lib"
    if _pre_tree="$(gr_tree_sha "$REPO_ROOT")" && [ -n "$_pre_tree" ]; then
      _pre_file="$(gr_file "$_pre_tree")"
      if [ -r "$_pre_file" ]; then
        PREFLIGHT_REUSE_REPO="$REPO_ROOT"
        PREFLIGHT_TREE="$_pre_tree"
        # ASK ABOUT THIS RUN'S ACTUAL SELECTION, NOT THE WHOLE INVENTORY. The
        # consumer answers exactly the question it is given (PREFLIGHT_ONLY), and
        # this path may only skip what the caller SELECTED: `--only vet` must not
        # skip the other thirteen, or the run would report a whole-inventory reuse
        # for a one-gate request — the consumer and the runner answering different
        # questions is the drift check 21 exists to prevent. With no `--only` this
        # is the whole inventory, which is the case the early exit is for.
        if [ -n "$ONLY" ]; then PREFLIGHT_ONLY="$ONLY"; fi
        if . "$REPO_ROOT/scripts/preflight_reuse.sh" --source 2>/dev/null; then
          if preflight_reuse_decision && [ "$PREFLIGHT_DECISION" = "full" ]; then
            # THE NODE ADVISORY, printed HERE for the pre-pool path. The consumer
            # captures gr_reusable's output into a variable to build its EVIDENCE
            # line, so the note is not visible on that route — and this is the route
            # a whole-inventory reuse takes. A differing node is a note, never a
            # refusal (see gr_node_advisory), so its status is ignored.
            gr_node_advisory "$_pre_file" || :
            echo "=============================================================="
            echo "  JomForm preflight — per-tree result record"
            echo "  repo:   $REPO_ROOT"
            echo "  tree:   $(gr_short_sha "$_pre_tree")"
            echo "=============================================================="
            echo "  reuse:  FULL GREEN record for tree $(gr_short_sha "$_pre_tree") — every gate in this run's selection is already green on THESE bytes"
            echo "          ($_pre_file, finished $(gr_read_field "$_pre_file" finished_at))"
            echo "          verdict: REUSED (green), remembered ${PREFLIGHT_REMEMBERED_SECONDS}s from $(gr_read_field "$_pre_file" log)"
            echo "          took NO slot and ran NO gate — nothing was queued behind another run."
            echo "          the record is NOT re-stamped: it still describes the run that OBSERVED it."
            echo "--------------------------------------------------------------"
            echo "preflight summary: 0 passed, 0 failed, 0 skipped in 0s"
            echo "  REUSE: $(printf '%s' "$PREFLIGHT_SKIP" | tr ',' ' ') (REMEMBERED from a recorded pass on tree $(gr_short_sha "$_pre_tree"), NOT re-run this time)"
            echo "--------------------------------------------------------------"
            exit 0
          fi
        fi
      fi
    fi
  fi
fi

# ---- SLOT POOL: take a slot, then run the gate body ------------------------
#
# A POOL OF `<path>.slot1 .. .slotN`, not one lock file. flock is OPEN-FILE-
# DESCRIPTION scoped, so two processes that open the same slot file are mutually
# exclusive — and a process may NOT re-take a lock it holds through a NEW open
# (measured). The nested case is therefore handled by INHERITANCE (the slot
# descriptor and its holder's pid travel in the environment), not by re-taking,
# and this gate's own self-test measures both directions rather than assuming
# either.
#
# WAIT is how long to wait for a slot. Not a correctness knob: a run that finds
# every slot busy waits rather than failing, because failing fast is the false-red
# shape this exists to avoid.
PREFLIGHT_LOCK_WAIT="${PREFLIGHT_LOCK_WAIT:-1800}"
# COUNT is how many gate runs this host does at once. 2 is the board's own
# per-profile dispatch cap and is what the host is sized for; raising it is a
# capacity decision, not a correctness one, so it is a knob and not a constant.
PREFLIGHT_SLOT_COUNT="${PREFLIGHT_SLOT_COUNT:-2}"
# PREFLIGHT_SLOT_FD is the descriptor this process holds its slot on ("" = none).
# A NESTED invocation inherits it, and that is how it can tell "already inside the
# pool" from "must take a slot" — flock is OPEN-FILE-DESCRIPTION scoped, so a
# second open of the same slot file by ANY process (the holder's own descendant
# included) is REFUSED; there is no way to "re-take" one's own lock through a
# fresh open. PREFLIGHT_SLOT_HOLDER names the process that took it, which is what
# makes the inheritance check precise rather than a bare "some marker is set".
PREFLIGHT_SLOT_FD="${PREFLIGHT_SLOT_FD:-}"
PREFLIGHT_SLOT_HOLDER="${PREFLIGHT_SLOT_HOLDER:-}"
PREFLIGHT_SLOT_LOCK="${PREFLIGHT_SLOT_LOCK:-}"
PREFLIGHT_LOCK_FILE="${PREFLIGHT_LOCK_FILE:-/var/lock/jomform-preflight-gate.lock}"

# The slot file for slot $1. `N` is appended so the PATH itself changes per slot —
# the take below relies on a FRESH open of a DIFFERENT file per candidate.
preflight_slot_path() { printf '%s.slot%s' "$PREFLIGHT_LOCK_FILE" "$1"; }

# PREFLIGHT_LOCK_FILE must NOT name a slot file. `*.slot<N>` is a name this script
# derives, so a caller who copied one out of the slot directory has misunderstood
# the knob — and it is not harmless: `flock -w <path> -c true` on a slot file
# BLOCKS while that slot is held, so such a run could never start until the slot
# it named was free, and the refusal below is the difference between a clear error
# and an unexplainable hang. The supported knob is PREFLIGHT_SLOT_COUNT=<n>.
case "${PREFLIGHT_LOCK_FILE##*/}" in
  *.slot[0-9]*)
    echo "preflight: PREFLIGHT_LOCK_FILE names a SLOT file (${PREFLIGHT_LOCK_FILE##*/})." >&2
    echo "preflight: slots are DERIVED from that path, so this run would wait on one specific slot" >&2
    echo "preflight: (it can never start while that slot is held). Point PREFLIGHT_LOCK_FILE at the" >&2
    echo "preflight: base path and raise PREFLIGHT_SLOT_COUNT=<n> if you need more slots." >&2
    exit 1
    ;;
esac

# PREFLIGHT_SLOT_ALREADY_HELD — is this process already inside the pool because it
# INHERITED a slot from an ancestor? That is the self-deadlock case: the ancestor
# is waiting for this child, and this child would wait for a slot the ancestor is
# holding. The signature is the marker AND the named holder being one of OUR OWN
# ANCESTORS — a marker alone is not enough, because flock is open-file-description
# scoped: re-opening a slot file this process already holds is REFUSED (measured),
# so a marker set by any other route would make a run believe it was inside the
# pool when the gate body was guarding nothing.
#
# WHAT THIS IS NOT: the earlier design's `fuser` scan, which could not tell an
# outer `flock` wrapper from a peer and cost the whole queue when it guessed wrong
# (card t_0094bf00). A peer never appears in this process's own process chain, so
# the ancestor walk is exact rather than a heuristic.
preflight_slot_held_by_ancestor() {
  [ -n "${PREFLIGHT_SLOT_HOLDER:-}" ] || return 1
  [ -n "${PREFLIGHT_SLOT_FD:-}" ] || return 1
  local p=$$ anc=" "
  while [ -n "$p" ] && [ "$p" -gt 1 ] 2>/dev/null; do
    anc="$anc$p "
    p="$(sed -n 's/^[0-9]* ([^)]*) [A-Z] \([0-9]*\) .*/\1/p' "/proc/$p/stat" 2>/dev/null)"
    [ -n "$p" ] || break
  done
  case "$anc" in *" ${PREFLIGHT_SLOT_HOLDER} "*) return 0 ;; esac
  return 1
}

if [ "${_PREFLIGHT_LOCKED:-}" != "1" ] && preflight_slot_held_by_ancestor; then
  # ALREADY INSIDE THE POOL — a nested invocation that inherited an ancestor's
  # slot. Running the gates here is correct (they are inside the slot that ancestor
  # holds); taking a second slot would wait for a slot the ancestor's own run is
  # holding, forever. The `selftest` gate is itself a nested invocation, so this is
  # the path a real full run takes.
  echo "  slots:  this invocation is ALREADY INSIDE the gate pool — slot ${PREFLIGHT_SLOT_N:-?} is held"
  echo "          by ancestor pid ${PREFLIGHT_SLOT_HOLDER} (${PREFLIGHT_SLOT_LOCK:-?}), so the gates run now"
  echo "          under that slot. Do NOT wrap this script in a lock of its own; raise the cap with"
  echo "          PREFLIGHT_SLOT_COUNT=<n>."
elif [ "${_PREFLIGHT_LOCKED:-}" != "1" ]; then
  # No `|| true` anywhere below: this repo's own `selftest` gate forbids it in an active
  # line, because a swallowed failure is how a gate stops being a gate. A missing lock
  # directory is a real problem (no slot could be created), so it FAILS loudly.
  if [ ! -d "$(dirname "$PREFLIGHT_LOCK_FILE")" ]; then
    echo "preflight: lock directory $(dirname "$PREFLIGHT_LOCK_FILE") does not exist." >&2
    echo "preflight: set PREFLIGHT_LOCK_FILE to a path inside an existing directory (default: /var/lock)." >&2
    exit 1
  fi
  if ! command -v flock >/dev/null 2>&1; then
    echo "  slots:  flock not found — running WITHOUT a host cap (concurrent gates may FALSE-fail)"
  else
    # NO `2>/dev/null` ON A REDIRECTION-ONLY `exec` ANYWHERE BELOW. A redirection-
    # only `exec` applies its redirections to the SHELL ITSELF, so `exec {fd}>>…
    # 2>/dev/null` would point this script's stderr at /dev/null for the rest of
    # the run — every diagnostic from there on, `die` included, would vanish while
    # the exit code still changed. (Measured while writing this: it made 21 of the
    # self-test's checks read "rc=1, no output".) An open failure here is a genuine
    # host problem — the directory was just checked — so bash's own message is the
    # right thing to surface rather than to swallow.
    #
    # `-o` on the flock: this process has NO command to run under the lock, and
    # without it flock would run `true` and release the lock when that exited.
    # `-o` closes the descriptor before running the (empty) command, so the lock
    # lives on the fd THIS shell keeps open.
    #
    # A CANDIDATE IS TRIED ON AN INDEPENDENTLY-OPENED DESCRIPTOR, one per slot.
    # Two processes that open the same file are mutually exclusive; the price is
    # that a process may NOT re-take its own lock through a new open, which is why
    # the nested case above is handled by the inheritance check rather than here.
    preflight_take_slot() {  # sets PREFLIGHT_SLOT_FD / PREFLIGHT_SLOT_N, 1 if none free
      local n fd
      for n in $(seq 1 "$PREFLIGHT_SLOT_COUNT"); do
        fd=""
        if exec {fd}>>"$(preflight_slot_path "$n")" && flock -o -n "$fd" 2>/dev/null; then
          PREFLIGHT_SLOT_FD="$fd"
          PREFLIGHT_SLOT_N="$n"
          return 0
        fi
        if [ -n "$fd" ]; then exec {fd}>&-; fi
      done
      return 1
    }
    if preflight_take_slot; then
      # The slot is held for the whole gate body, and `_PREFLIGHT_LOCKED=1` is
      # exported so this script's own re-entry guard holds — nothing below tries to
      # take a second slot. HOLDER names this process, which is the marker a nested
      # invocation checks against its own ancestor chain.
      PREFLIGHT_SLOT_HOLDER="$$"
      PREFLIGHT_SLOT_LOCK="$(preflight_slot_path "$PREFLIGHT_SLOT_N")"
      export PREFLIGHT_SLOT_FD PREFLIGHT_SLOT_N PREFLIGHT_SLOT_HOLDER PREFLIGHT_SLOT_LOCK
      export _PREFLIGHT_LOCKED=1
    else
      # The pool is full. Say what is full, then wait. `-E 74` gives a wait-out its
      # own code so "could not get a slot" is distinguishable from "the code is
      # broken" — a caller must not read one as the other.
      echo "  slots:  all ${PREFLIGHT_SLOT_COUNT} gate slot(s) busy — waiting up to ${PREFLIGHT_LOCK_WAIT}s"
      echo "          (bound: this host runs ${PREFLIGHT_SLOT_COUNT} gates at once; the scratch schema and"
      echo "          the valkey namespace are per-process, so waiting here is a CPU choice, not a"
      echo "          correctness one)"
      _slot_deadline=$(( $(date +%s) + PREFLIGHT_LOCK_WAIT ))
      # THE WAIT MUST WATCH EVERY CANDIDATE, NOT ONE PATH (card t_1c59f9f5).
      # `flock -w … -c true <one path>` is a wait on THAT PATH; it says nothing
      # about the rest of the pool. The pool's wait used to be exactly that — one
      # blocking `flock` on slot 1 — so a run that found every slot busy proceeded
      # only when SLOT 1 freed, and exited 74 "could not get a gate slot …
      # NOTHING was built or tested" while another slot had been free the whole
      # time. Measured on the pre-fix head: hold slot1+slot2, free slot2 after 2s
      # and keep slot1 held, run with PREFLIGHT_LOCK_WAIT=8 — three runs, rc=74
      # each, with the host half empty. That is the false-red shape this pool
      # exists to avoid (see WHY WAIT INSTEAD OF FAIL above), and it widens with
      # PREFLIGHT_SLOT_COUNT, which the header advertises as a knob.
      #
      # The loop below waits the way the TAKE decides: `preflight_take_slot`
      # re-scans EVERY candidate from slot 1 on each call, so the wait is a
      # bounded poll of each candidate followed by a take, and it cannot be blind
      # to a free slot the take would accept. A bounded (not `-n`) wait is still a
      # real wait — it returns the moment that candidate frees — so this is a
      # watch on the pool, not a sleep loop; the `-c true` lock dies with its
      # command, so polling never KEEPS a slot, and the take is the only thing
      # that holds one.
      #
      # 74 IS DECIDED BY THE DEADLINE, NOT BY A CANDIDATE. A single candidate
      # timing out is the normal tick of the poll; only the deadline running out
      # means "the host is at cap", which is the one reading the header documents
      # 74 to carry. So 74 is unreachable while ANY candidate is free, which is
      # the property the pool claims.
      _slot_poll=1
      _slot_waiting=1
      while [ "$_slot_waiting" = 1 ]; do
        _slot_left=$(( _slot_deadline - $(date +%s) ))
        if [ "$_slot_left" -le 0 ]; then
          # Last chance: a NON-BLOCKING take before declaring the wait-out, so a
          # slot that freed between the previous tick and the deadline is still
          # accepted and 74 stays reserved for a host that really is at cap.
          if preflight_take_slot; then _slot_waiting=0; break; fi
          echo "preflight: could not get a gate slot within ${PREFLIGHT_LOCK_WAIT}s" >&2
          echo "preflight: NOTHING was built or tested. Re-run once a gate finishes, raise the wait with" >&2
          echo "preflight: PREFLIGHT_LOCK_WAIT=<seconds>, or raise the pool with PREFLIGHT_SLOT_COUNT=<n>." >&2
          echo "preflight: a slot is released automatically when its holder exits, including on a crash." >&2
          exit 74
        fi
        if preflight_take_slot; then _slot_waiting=0; break; fi
        # Nothing free yet. Block, boundedly, on EACH candidate in turn: whichever
        # frees first ends its own wait immediately, and the take at the top of the
        # next pass accepts it. The step is capped by the time left so the deadline
        # is never overshot by a whole poll interval.
        _slot_step="$_slot_poll"
        [ "$_slot_step" -le "$_slot_left" ] || _slot_step="$_slot_left"
        for _slot_n in $(seq 1 "$PREFLIGHT_SLOT_COUNT"); do
          # A held candidate ticking out its bound is EXPECTED here — that is the
          # poll, not a failure — so the status is deliberately not inspected: the
          # take on the next line is what decides whether a slot was really
          # obtained. A bare failing command cannot abort this run (there is no
          # `set -e` here), and writing it as `|| true` would trip this repo's own
          # `selftest` check 10, which is exactly the shape that hides a gate.
          flock -w "$_slot_step" -E 74 "$(preflight_slot_path "$_slot_n")" -c true
          if preflight_take_slot; then _slot_waiting=0; break; fi
        done
      done
      PREFLIGHT_SLOT_HOLDER="$$"
      PREFLIGHT_SLOT_LOCK="$(preflight_slot_path "$PREFLIGHT_SLOT_N")"
      export PREFLIGHT_SLOT_FD PREFLIGHT_SLOT_N PREFLIGHT_SLOT_HOLDER PREFLIGHT_SLOT_LOCK
      export _PREFLIGHT_LOCKED=1
    fi
  fi
fi

# ---- PER-TREE RESULT RECORD (scripts/gate_result.sh) ------------------------
# Everything from here on runs INSIDE the pool (and inside the child, on the
# branch below), so a queued run reaches its own record with a slot of its own.
#
# WHY IT IS HERE AND NOT AT THE BOTTOM: a gate run dies inside `run()` on the
# first failure, so an end-of-script write would only ever record GREEN runs and
# the record would have nothing to say about a red tree — which is precisely the
# case (148 of 206 repeated runs) the record is needed for. Each gate is recorded
# the moment it completes instead, and the file is rewritten after every gate.
#
# The record is HOST STATE, deliberately: `PREFLIGHT_RESULT_DIR` defaults to
# /var/lib/jomform/gate, outside the repo (a record is evidence about a machine,
# not a versioned artefact) and outside /tmp (which is wiped between sessions, so
# a record there would be useless by the next morning).
PREFLIGHT_RESULT_LIB="${PREFLIGHT_RESULT_LIB:-$REPO_ROOT/scripts/gate_result.sh}"
if [ -r "$PREFLIGHT_RESULT_LIB" ]; then
  # shellcheck source=scripts/gate_result.sh
  . "$PREFLIGHT_RESULT_LIB"
  GR_GATE_NAMES=("${GATE_NAMES[@]}")
  GR_REPO="$REPO_ROOT"
  GR_COMMIT="$(git rev-parse --short HEAD 2>/dev/null)"
  GR_BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
  GR_PID="$$"
  GR_LOG="${PREFLIGHT_LOG:-}"
  # What this run was ASKED for — recorded so a reader can tell a run that
  # observed everything from one that observed a slice. Empty means "the whole
  # inventory", which is `preflight.sh` with no --only.
  GR_SELECTED="${ONLY:-}"
  if GR_TREE="$(gr_tree_sha "$REPO_ROOT")"; then
    GR_TREE_SHORT="$(gr_short_sha "$GR_TREE")"
  else
    # Unidentifiable tree: do not guess, do not key a record on a commit sha.
    # The run continues — a record is advisory, and failing the gate over host
    # state would turn a green tree red — but nothing is written.
    echo "preflight: WARNING — could not compute a working-tree sha; NO result record will be written." >&2
    GR_TREE=""
  fi
else
  echo "preflight: WARNING — $PREFLIGHT_RESULT_LIB not found; NO per-tree result record will be written." >&2
fi

# gr_record_now — flush the record after a gate completes. Deliberately never
# fatal: the record is evidence, and a host-state problem must not red a gate run.
# Written without `|| true` — the `selftest` gate forbids it in any active line
# precisely so a gate cannot be quietly neutered, and a swallowed failure here is
# announced instead: the WARNING below is the whole point of catching it.
gr_record_now() {
  [ -n "${GR_TREE:-}" ] || return 0
  local rc=0
  gr_flush "$(( $(date +%s) - START_ALL ))" || rc=$?
  [ "$rc" -eq 0 ] || echo "preflight: WARNING — the per-tree result record could not be written (rc=$rc). The gate verdict above still stands; the next lane will simply re-gate." >&2
  return 0
}

# ---- REUSE: --reuse-record may only skip a gate this tree is KNOWN to pass ---
# scripts/preflight_reuse.sh (the consumer a lane runs BEFORE queueing) reads the
# record and hands back the gates it can vouch for. This is where that claim is
# CHECKED: the record must name THIS tree and THIS environment fingerprint, be
# inside the TTL, and hold an observed `pass` for every gate we are about to
# skip. Anything else is refused loudly, and the gate runs.
#
# The whole point of this block is that it can only ever NARROW a run. A record
# can never turn a red into a green: it can only replace a gate that would have
# been re-run with an observation of the SAME GATE, on the SAME BYTES, under the
# SAME FINGERPRINT — and a `fail` anywhere in the record refuses the reuse.
#
# THIS BLOCK IS THE PER-GATE PATH ONLY. The whole-inventory reuse was already
# taken before the slot pool (there is nothing left to do for a tree that is
# wholly green, and queueing for it is the cost the card removes). So reaching
# here with a full-green record means the pre-pool path declined for a reason of
# its own — which is still a refusal, not a licence to skip work: the pre-pool
# path is the only place a full-inventory reuse may be granted, and saying so in
# both places would be two chances to disagree.
if [ "$REUSE_RECORD" = 1 ]; then
  if [ -z "${GR_TREE:-}" ]; then
    die "--reuse-record was requested but this tree cannot be identified (no working-tree sha) — refusing to skip a gate on an unnameable tree"
  fi
  if [ ! -r "${PREFLIGHT_RESULT_LIB:-}" ]; then
    die "--reuse-record was requested but scripts/gate_result.sh is missing — nothing to read a record from"
  fi
  _reuse_file="$(gr_file "$GR_TREE")"
  if [ ! -r "$_reuse_file" ]; then
    die "--reuse-record was requested but there is no record for tree $GR_TREE_SHORT ($_reuse_file) — refusing to report a green this run did not earn"
  fi
  _reuse_reason=""
  if _reuse_reason="$(gr_reusable "$GR_TREE" "${GATE_NAMES[@]}" 2>&1)"; then
    # A full green that got this far was only NOT taken pre-pool because the
    # caller's SELECTION is a subset of the inventory — the pre-pool path asks
    # about the whole inventory and declines for `--only vet`. That is the normal
    # `--only <subset> --reuse-record` shape, and it must still reuse the selected
    # gates (they are recorded passes) rather than refuse the run. Fall through to
    # the per-gate path below, which honours the selection.
    :
  fi
  # Not a full green: reuse the recorded passes, re-run the rest. Build the reuse
  # list from the record, then verify each entry individually so a gate is only
  # ever skipped on an explicit `pass`.
  #
  # THE ENVIRONMENT CHECK IS RE-DONE HERE, AND THAT IS NOT REDUNDANT. gr_reusable
  # above returned non-zero, and the reason it did is exactly what decides whether
  # a partial record may be used at all: a record whose FINGERPRINT or TREE no
  # longer matches must contribute NOTHING, while one that is merely not-yet-green
  # may still vouch for the gates it did observe. Reading the record's passes
  # without this guard is how a bumped key, a moved toolchain or an edited gate
  # script would hand a stale pass to a tree it never described.
  _reuse_ok=(); _reuse_bad=""; _reuse_off=""
  if ! _reuse_usable="$(gr_record_usable "$GR_TREE" 2>&1)"; then
    _reuse_ok=()
  else
    _reuse_passes="$(gr_gates_with_pass "$_reuse_file" "${GATE_NAMES[@]}")"
    for _g in $_reuse_passes; do
      case ",${SKIP}," in
        *",$_g,"*) _reuse_bad="$_reuse_bad $_g" ;;   # already skipped by the caller: harmless, not a reuse
        *) _reuse_ok+=("$_g") ;;
      esac
    done
  fi
  # A gate the CALLER skipped is not a reuse, and calling it one would claim a
  # saving this run did not make. Say which, so the ledger stays honest.
  [ -z "$_reuse_bad" ] || echo "preflight: note — already skipped by --only/--skip, so not counted as reused:$_reuse_bad" >&2
  # ---- "VOUCHES FOR NO GATE" IS A QUESTION ABOUT THE RECORD, NOT THE SELECTION ----
  # This refusal is decided against the gates the RECORD vouches for, BEFORE the
  # selection filter below. It used to be decided AFTER it, and that made the
  # documented red-tree lane unreachable: `docs/PREFLIGHT-REUSE.md` tells a lane
  # whose run was red to re-run exactly what the record could not vouch for
  #
  #     ./scripts/preflight.sh --only <PREFLIGHT_RERUN> --reuse-record
  #
  # and on a record whose only `fail` is that gate every recorded `pass` is off
  # that selection. Filtering first discarded all of them, the vouched set came
  # out empty, and the run DIED with "vouches for NO gate" — rc=1, no ledger
  # line, on the very shape (148 of the 206 repeated runs measured on this host)
  # the record exists for. Measured on the record for tree 53854bd: without
  # `--only` it ran `vet` once and named the other thirteen; with the documented
  # `--only vet` it refused.
  #
  # The two questions are different and both still have to be asked:
  #   * does the record vouch for ANYTHING?  -> about the RECORD (this check);
  #   * may THIS RUN claim a saving for it?  -> about the SELECTION (below).
  # A record that vouches for nothing is still a hard refusal, because then the
  # reuse mechanism is contributing nothing and the lane must be told.
  _reuse_vouched=${#_reuse_ok[@]}
  PREFLIGHT_REUSED_LABEL="$_reuse_file"
  if [ "$_reuse_vouched" -eq 0 ]; then
    die "--reuse-record was requested but the record for tree $GR_TREE_SHORT vouches for NO gate ($_reuse_reason) — refusing to run as if it did"
  fi
  # The node advisory, printed here for the PER-GATE path. gr_reusable already
  # announced it when it returned green, but this path is reached exactly when the
  # record is NOT a full green (or the selection is narrower), and a lane reading a
  # verdict must still be able to see that the web-side gates were observed under a
  # different node. It is a NOTE: gr_node_advisory's non-zero status is ignored.
  gr_node_advisory "$_reuse_file" || :
  # ---- the SECOND question: what may this run count as saved? ------------------
  # A gate this run was never going to execute (`--only vet` while the record
  # vouches for `monitor`) is not a reuse: counting it would inflate the ledger
  # with host-seconds nobody was spending. So the ledger and the REUSE line take
  # only the intersection — but the vouched set above already proved the record
  # is worth reading, and a NON-EMPTY intersection is not required for the run to
  # proceed. When the intersection is empty the run executes its own selection and
  # says plainly that the carried gates are outside it.
  _reuse_sel=()
  for _g in "${_reuse_ok[@]}"; do
    if selected "$_g"; then _reuse_sel+=("$_g"); else _reuse_off="$_reuse_off $_g"; fi
  done
  [ -z "$_reuse_off" ] || echo "preflight: note — not in this run's selection, so not counted as reused:$_reuse_off" >&2
  _reuse_ok=("${_reuse_sel[@]}")
  if [ "${#_reuse_ok[@]}" -eq 0 ]; then
    # THE DOCUMENTED RED-TREE SHAPE. The record vouches for $_reuse_vouched gate(s)
    # and this run selected none of them, so there is nothing to fold into SKIP
    # and nothing to ledger — it runs exactly what it was asked for. Saying so is
    # the difference between a record read that is honest about its scope and one
    # that reads as if the selection had been vouched for.
    echo "==> REUSE: $_reuse_vouched recorded pass(es) carried over (not in this run's selection, so NOT ledgered):$_reuse_off"
    echo "    from $_reuse_file (finished $(gr_read_field "$_reuse_file" finished_at)); tree $GR_TREE_SHORT"
    echo "    this run executes its own selection below; the carried gates were neither run nor reused here."
  fi
  # Fold the vouched-for gates into SKIP. They are gates this tree is KNOWN to
  # pass, so not re-running them is not a coverage loss — and each one is named.
  # With an empty intersection PREFLIGHT_REUSED stays empty: no gate is skipped
  # on the record's authority, so the run below is the caller's selection as asked.
  if [ "${#_reuse_ok[@]}" -gt 0 ]; then
    PREFLIGHT_REUSED="$(printf '%s,' "${_reuse_ok[@]}")"; PREFLIGHT_REUSED="${PREFLIGHT_REUSED%,}"
    # FOLD INTO SKIP — THIS IS THE MECHANISM, not a bookkeeping line. `run()` returns
    # early for a gate that is not `selected()`, and `SKIP` is what `selected()`
    # consults, so without this line the REUSE line would be printed and every
    # vouched-for gate would then be re-executed anyway.
    SKIP="${SKIP:+$SKIP,}$PREFLIGHT_REUSED"
    REUSED=("${_reuse_ok[@]}")
    echo "==> REUSED (not re-run, recorded pass on THESE bytes): $PREFLIGHT_REUSED"
    echo "    from $_reuse_file (finished $(gr_read_field "$_reuse_file" finished_at)); tree $GR_TREE_SHORT"
    echo "    this run's verdict is a RECORD READ, not a re-run (scripts/preflight_reuse.sh labelled it)."
    # The saving is LEDGERED here, and ONLY when this run really skipped work on
    # the record's authority. A lane that drives the runner directly (the
    # documented way to re-run just the failed gates) makes the same saving, and
    # acceptance for this card is that the saving is MEASURED from real files
    # rather than estimated from the design. Only gates that were really skipped
    # on a recorded pass are counted — and the consumer that ran FIRST wrote this
    # same event already, so the write below is deduped (see gr_ledger).
    _reuse_secs="$(gr_gates_reused_seconds "$_reuse_file" "${_reuse_ok[@]}")"
    _led_rc=0; gr_ledger "$GR_TREE" partial "$_reuse_secs" "$PREFLIGHT_REUSED" || _led_rc=$?
    [ "$_led_rc" -eq 0 ] || echo "preflight: WARNING — could not append to the reuse ledger (rc=$_led_rc); the measured saving under-counts." >&2
  fi
  SKIPPED=()
  for gate in "${GATE_NAMES[@]}"; do
    selected "$gate" || SKIPPED+=("$gate")
  done
  # A gate that was REUSED is not "still skipped and not reused" — it is a
  # recorded pass. The reused gates are folded into SKIP (that is the mechanism),
  # so printing SKIP here without subtracting REUSED contradicts the line above
  # and reads as "these have no recorded pass" about gates that were just
  # vouched for. Only the gates that are skipped for a DIFFERENT reason belong on
  # this line.
  _reuse_notreused=()
  for gate in "${SKIPPED[@]}"; do
    case ",${PREFLIGHT_REUSED}," in
      *",$gate,"*) ;;
      *) _reuse_notreused+=("$gate") ;;
    esac
  done
  if [ ${#_reuse_notreused[@]} -gt 0 ]; then
    echo "    STILL SKIPPED AND NOT REUSED (no recorded pass): ${_reuse_notreused[*]}" >&2
  fi
  # REUSE MUST NEVER *CAUSE* A VACUOUS RUN TO READ GREEN. The pre-existing rule
  # above already refuses a run whose selection was empty to begin with. What this
  # catches is the NEW shape reuse introduces: a selection that had real gates in
  # it, every one of which the record vouched for, so nothing executes. That is
  # legitimate (the verdict exists for these bytes) but it must be ANNOUNCED as a
  # record read rather than printed as a gate run — a `passed` count that includes
  # gates nobody executed in this process is exactly the misreading this card is
  # about.
  _will_run=0
  for gate in "${GATE_NAMES[@]}"; do
    selected "$gate" || continue
    case ",${PREFLIGHT_REUSED}," in
      *",$gate,"*) ;;
      *) _will_run=$((_will_run+1)) ;;
    esac
  done
  if [ "$_will_run" -eq 0 ]; then
    echo "==> nothing to execute: every gate in this run's selection is a recorded pass on tree $GR_TREE_SHORT"
    echo "    ${#REUSED[@]} gate(s) verified against $PREFLIGHT_REUSED_LABEL — this run is a RECORD READ, not a gate run."
    echo "    The summary below counts them as REUSE, never as PASS."
  fi
fi

echo "=============================================================="
echo "  JomForm preflight — local gate (GitHub Actions is not a gate)"
echo "  repo:   $REPO_ROOT"
echo "  commit: $(git rev-parse --short HEAD) ($(git describe --tags --always 2>/dev/null || echo untagged))"
echo "  gates:  ${GATE_NAMES[*]}"
if [ -n "$ONLY" ]; then echo "  --only: $ONLY"; fi
if [ -n "${SEL_DIFF_LINE:-}" ]; then
  echo "  --for-diff ${SEL_BASE}: ${SEL_COUNT}/${#GATE_NAMES[@]} gates"
fi
echo "=============================================================="

# A narrowed run must NEVER be silent. The board's rule is that a gate which did
# not run is not evidence, so a run that narrowed has to say so in its own output
# — including WHY, so the reader can judge the selection rather than trust it.
if [ -n "${SEL_DIFF_LINE:-}" ]; then
  cat >&2 <<EOF

  ############################################################
  #  !!  NARROWED RUN: --for-diff $SEL_BASE
  ############################################################
  #  RUN:  $SEL_DIFF_LINE
  #  NOT RUN (this is NOT evidence): ${SEL_DIFF_SKIPPED:-none}
  #  WHY:  $SEL_DIFF_REASON
EOF
  if [ "${SEL_DIFF_FULL:-0}" = 1 ]; then
    cat >&2 <<EOF
  #  That is the FULL ${#GATE_NAMES[@]}-gate set: the diff was unclassifiable (or
  #  touched a path no gate claims), so nothing was narrowed.
EOF
  fi
  cat >&2 <<EOF
  #  Read --explain-diff <base-ref> to see the classification without running.
  ############################################################

EOF
fi

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
  local need_api="$1" need_web="$2" need_a11y="$3" need_docker="$4" need_lint="$5"

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
    # disagree about whether this tree is installed. It refuses at TWO depths:
    # whole package directories absent (the shallow interruption) and every
    # declared directory present but holding no payload (the deep one — an
    # `npm ci` killed after the directories are created, where `.bin/next` and
    # `next`'s own payload are already on disk and this `-x` check is blind to
    # it). Measured on the deep shape: the check used to exit 0 saying "14
    # declared package(s) installed", and `npm run test:unit` then printed
    # 326/324/2 against 350/350/0.
    node "$REPO_ROOT/web/scripts/web-deps-check.mjs" "$REPO_ROOT/web" || die \
      "the web dependencies for this tree are not installed — 'npm run test:unit' would UNDER-REPORT (node --test runs without dependencies, so the files that import react fail to load and their tests never register) instead of failing. Seed it with: cd web && npm ci"
  fi

  if [ "$need_a11y" = 1 ]; then
    [ -x "$REPO_ROOT/web/node_modules/.bin/playwright" ] || die \
      "web/node_modules/.bin/playwright is missing — seed web/node_modules first: cd web && npm ci"
  fi

  # `lint` — `eslint` is a DEV dependency, so a tree with the four runtime deps
  # but no dev deps would run `npm run lint` and get "eslint: not found" from
  # npm rather than a verdict. Refused loudly, like every other prerequisite:
  # a linter that did not run must never read as a lint that passed.
  if [ "$need_lint" = 1 ]; then
    [ -x "$REPO_ROOT/web/node_modules/.bin/eslint" ] || die \
      "web/node_modules/.bin/eslint is missing — the lint gate would report nothing (npm exits non-zero with 'eslint: not found' rather than linting). Seed it with: cd web && npm ci"
  fi

  # `webimage` — the one gate that covers the SHIPPING artifact, so a host that
  # cannot build the image is a REFUSED gate, not a skipped one. Silently
  # skipping here would reproduce the exact shape this gate was added for: a
  # preflight that reads green while the only command that builds the artifact
  # was never run. A host with no daemon has to say `--skip webimage` out loud,
  # which prints the NOT-RUN block.
  if [ "$need_docker" = 1 ]; then
    [ -f "$REPO_ROOT/web/Dockerfile" ] || die \
      "web/Dockerfile is missing — the webimage gate builds the shipping web artifact and cannot run without it"
    if ! docker info >/dev/null 2>&1; then
      die "the docker daemon is unreachable (docker info failed) — the webimage gate builds the web IMAGE, the only artifact that ships, so it is refused rather than skipped. Start the daemon, or state the gap explicitly with: ./scripts/preflight.sh --skip webimage"
    fi
  fi
}

# ---------------------------------------------------------------------------
# Gate runner.
# ---------------------------------------------------------------------------
FAILED=()
PASSED=()
START_ALL=$(date +%s)

# run() — execute one gate, record the observation, and stop the run on a failure.
#
# THE RECORD IS WRITTEN HERE, PER GATE, AND THAT IS THE WHOLE POINT. A gate run
# exits on its first failure, so a single write at the end of the script would
# only ever describe GREEN runs and the record would be useless for the 148 of
# 206 repeated runs that were re-gating a red tree (the case where reuse is worth
# the most: 21.3 of the 38.3 host-hours). Recording each gate as it completes
# also means the record survives a killed run, which is how a lane recovers the
# passes from an attempt that was interrupted.
# ---------------------------------------------------------------------------
# A FAILED GATE MUST SAY WHAT KIND OF FAILURE IT WAS (card t_abfc22b7).
#
# The gate used to print one line for every non-zero exit —
# `PREFLIGHT FAIL: gate 'test' (…) exited 1 after 697s` — so a TIMEOUT looked
# exactly like an assertion failure. That is what made the defect expensive: run 1
# reported FAIL after 697s with **no test failing and no assertion firing**, the
# only line in the log being `panic: test timed out after 10m0s`, and the reader
# chased the test the runner happened to name instead of the budget. A gate that
# reds when nothing is wrong trains the next worker to re-run until green.
#
# The budget was fixed separately (API_TEST_TIMEOUT, default 30m); this is the
# other half — the gate now NAMES the kind of failure, and when it is a timeout it
# says the run ran out of BUDGET rather than that the code is broken.
# ---------------------------------------------------------------------------
_budget_secs() {  # "<num><unit>" -> seconds; 0 when it cannot be read
  local v="$1" num unit
  num="${v%%[!0-9]*}"
  unit="${v#"$num"}"
  [ -n "$num" ] || { echo 0; return; }
  case "$unit" in
    ''|s) echo "$num" ;;
    m)    echo $((num * 60)) ;;
    h)    echo $((num * 3600)) ;;
    *)    echo 0 ;;
  esac
}

# gate_failure_kind <log> <elapsed_s> <budget_s> -> timeout | crash | failure
gate_failure_kind() {
  local log="$1" secs="$2" budget="$3"
  # Go's own alarm fired. It names the test it happened to be in, which is NOT
  # the culprit — the budget is.
  if grep -qE 'panic: test timed out after' "$log" 2>/dev/null; then echo timeout; return; fi
  # The run outlived the budget we handed `go test`, so whatever it printed after
  # that is the tail of a run that was killed, not a verdict.
  if [ "$budget" -gt 0 ] && [ "$secs" -ge "$budget" ]; then echo timeout; return; fi
  # A crash is not a failed assertion either: the reader must not go looking for
  # a test that changed behaviour.
  if grep -qE '^(panic:|fatal error:|signal: )' "$log" 2>/dev/null; then echo crash; return; fi
  echo failure
}

run() {
  local gate="$1" label="$2"; shift 2
  if ! selected "$gate"; then return 0; fi
  info "gate [$gate]: $label"
  local t0 t1 rc log
  t0=$(date +%s)
  log="$(mktemp "${TMPDIR:-/tmp}/preflight-gate-$gate.XXXXXX")"
  # NOTE: no `|| true`, no `continue-on-error`. A gate either passes or stops
  # the run; that is the whole point of moving it off GitHub. The `tee` is only
  # so a FAILED gate's output can be read back and CLASSIFIED (see
  # gate_failure_kind) while still streaming live.
  if "$@" 2>&1 | tee "$log"; then
    rm -f "$log"
    t1=$(date +%s)
    PASSED+=("$gate")
    # Observation for the per-tree record. Deliberately AFTER the gate, and the
    # `pass` is recorded with the seconds it took, because that per-gate number
    # is what a later lane's saving is measured from.
    if declare -F gr_flush >/dev/null 2>&1 && [ -n "${GR_TREE:-}" ]; then
      GR_STATUS["$gate"]="pass"; GR_SECONDS["$gate"]="$((t1-t0))"; GR_AT["$gate"]="$(gr_now)"
      GR_PASSED="${#PASSED[@]}"; GR_FAILED="${#FAILED[@]}"; GR_SKIPPED="${#SKIPPED[@]}"
      gr_record_now
    fi
    info "gate [$gate]: PASS ($((t1-t0))s)"
  else
    rc=$?
    t1=$(date +%s)
    FAILED+=("$gate")
    if declare -F gr_flush >/dev/null 2>&1 && [ -n "${GR_TREE:-}" ]; then
      GR_STATUS["$gate"]="fail"; GR_SECONDS["$gate"]="$((t1-t0))"; GR_AT["$gate"]="$(gr_now)"
      GR_PASSED="${#PASSED[@]}"; GR_FAILED="${#FAILED[@]}"; GR_SKIPPED="${#SKIPPED[@]}"
      gr_record_now
    fi
    local _secs=$((t1 - t0)) _kind="failure" _budget=0
    _budget="$(_budget_secs "${API_TEST_TIMEOUT:-}")"
    _kind="$(gate_failure_kind "$log" "$_secs" "$_budget")"
    rm -f "$log"
    case "$_kind" in
      timeout)
        echo "PREFLIGHT FAIL: gate '$gate' ($label) ran out of its TIME BUDGET after ${_secs}s" >&2
        echo "  This is a TIMEOUT, not a failed assertion: the gate was killed, so its output above is the" >&2
        echo "  tail of an unfinished run and NO test verdict can be read from it. Raise the budget (for the" >&2
        echo "  api test gate: API_TEST_TIMEOUT, e.g. API_TEST_TIMEOUT=45m ./scripts/preflight.sh) or re-run on" >&2
        echo "  a quieter host. Do NOT read this as the package being broken." >&2
        ;;
      crash)
        echo "PREFLIGHT FAIL: gate '$gate' ($label) CRASHED after ${_secs}s (panic / fatal error / signal)" >&2
        echo "  No assertion decided this; the process died. Read the panic above, not the test names." >&2
        ;;
      *)
        echo "PREFLIGHT FAIL: gate '$gate' ($label) exited $rc after ${_secs}s" >&2
        ;;
    esac
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
  # A reused gate is NOT one this run observed. It is printed on its own line,
  # labelled, with the record it came from — so nobody reads the `passed` count
  # as "N gates ran". This is the "a reused result must be labelled as reused"
  # requirement, and it is the reason REUSED is a separate array from PASSED.
  if [ ${#REUSED[@]} -gt 0 ]; then
    echo "  REUSE: ${REUSED[*]} (REMEMBERED from a recorded pass on tree ${GR_TREE_SHORT:-?}, NOT re-run this time)"
  fi
  # The SKIP line means "no evidence at all" — so a REUSED gate must not appear
  # there. A reused gate IS evidence (an observed pass on these bytes, on another
  # run); listing it under `(not evidence)` would contradict the REUSE line
  # directly above it and misrepresent a remembered pass as a gap.
  _sum_notreused=()
  for gate in "${SKIPPED[@]}"; do
    case ",${PREFLIGHT_REUSED:-}," in
      *",$gate,"*) ;;
      *) _sum_notreused+=("$gate") ;;
    esac
  done
  [ ${#_sum_notreused[@]} -gt 0 ] && echo "  SKIP: ${_sum_notreused[*]} (not evidence)"
  [ ${#FAILED[@]} -gt 0 ] && echo "  FAIL: ${FAILED[*]}"
  echo "--------------------------------------------------------------"
}

# --- which gates are selected, for the env preflight ------------------------
g_build=0; g_vet=0; g_test=0; g_govuln=0; g_web=0; g_lint=0; g_webimage=0; g_a11y=0; g_npmaudit=0; g_gitleaks=0
selected build    && g_build=1
selected vet      && g_vet=1
selected test     && g_test=1
selected govuln   && g_govuln=1
selected web      && g_web=1
selected lint     && g_lint=1
selected webimage && g_webimage=1
selected a11y     && g_a11y=1
selected npmaudit && g_npmaudit=1
selected gitleaks && g_gitleaks=1

need_api=0; need_web=0; need_a11y=0; need_docker=0; need_lint=0
if [ $g_build = 1 ] || [ $g_vet = 1 ] || [ $g_test = 1 ] || [ $g_govuln = 1 ]; then need_api=1; fi
if [ $g_web = 1 ]; then need_web=1; fi
if [ $g_a11y = 1 ]; then need_a11y=1; need_web=1; fi
if [ $g_webimage = 1 ]; then need_docker=1; fi
# `lint` reads web/eslint.config.mjs and web/package.json but runs eslint from
# web/node_modules — it does NOT need the full `web` build. Keeping them separate
# means a lint failure is reported as a lint failure, not as a failed build.
if [ $g_lint = 1 ]; then need_lint=1; fi
preflight_env "$need_api" "$need_web" "$need_a11y" "$need_docker" "$need_lint"

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
# gofmt — a fail that is not gofmt-clean is a DIFF NOBODY REVIEWED.
#
# WHY THIS GATE EXISTS (cards t_40bc6f95, t_d60f987b). `go vet` does not check
# formatting, so nothing did: measured on origin/main, 26 files under `api/` were not
# gofmt-clean, and two of them were REAL defects a reviewer would have caught —
# `consume.go` had a doc paragraph turned into a `#` HEADING (so its explanation read
# as a title), and `account_slug_host_issue77_test.go` had a whole test body's comment
# and code left at column 0 inside the function.
#
# Every one of the 26 is a comment/whitespace change, which is what makes them
# invisible: `gofmt -l` names files, and a diff that only moves comments is the diff a
# reader skims. That is exactly how they accumulated. Verified before formatting: of the
# 26, ZERO changed a single code token (`strip_comments` + whitespace-normalise, compared
# to `gofmt` output) — so this gate is safe to add to a tree that is not clean, and its
# fix is a no-op for behaviour.
#
# `gofmt -l` and not `gofmt -d`: the list is what the gate needs, and -d on 26 files is
# 26 full-file diffs in a passing run's output. It names EVERY offending file rather than
# stopping at the first, so one run tells a lane the whole job.
run gofmt "cd api && test -z \"\$(gofmt -l .)\" (every api/ file is gofmt-clean)" \
  bash -c "cd '$REPO_ROOT/api' && out=\$(gofmt -l .); if [ -n \"\$out\" ]; then echo 'not gofmt-clean:'; echo \"\$out\" | sed 's/^/  /'; echo 'Fix with: cd api && gofmt -w \$(gofmt -l .)'; exit 1; fi"

# ---------------------------------------------------------------------------
# api/test — WITH -race, and with an EXPLICIT per-package budget.
#
# WHY -timeout IS HERE AND NOT LEFT TO THE DEFAULT. `go test` panics a package
# after 10 minutes (Go's own default — `go help testflag`: "-timeout d ... The
# default is 10 minutes (10m)") and that budget is PER PACKAGE, applied to every
# package. `internal/httpapi` is the largest package and its cost tracks host
# load, so on a busy host it crosses 600s and the gate reports
# `panic: test timed out after 10m0s` for a package that is GREEN. Measured on
# this host: pristine `origin/main` (ca110813) rc=1 at 601.195s, the same
# package rc=0 at 620.593s — the same tree and the same host, minutes apart.
# Independently re-measured for this card on this very tree at load 15-24:
# `go test ./internal/httpapi/ -race -count=1 -timeout 45m` -> rc=0,
# `ok ... 608.540s` — i.e. 8.5s over the budget Go would have applied by
# default, so the default is what made it red, not the package.
# A 10-minute timeout looks exactly like a hung test, so the reader chases the
# test the runner happened to name (`TestTrustedDevicesListAndRevokeOne`, which
# is merely where the clock ran out) instead of the budget.
#
# WHY 30m AND NOT "no timeout": `-timeout 0` disables the alarm entirely, which
# turns a genuinely hung package into an unbounded gate — the merge lane waits on
# it and nothing says why. The value is a CEILING, not a target: it is chosen to
# sit well clear of a loaded-host run (~10-12 min) while still being a bound, so
# a hang is still caught, just not confused with a slow host.
#
# IT IS OVERRIDABLE FOR THE SAME REASON THE VALIDATION BELOW EXISTS: a host
# slower than this one must be able to raise the ceiling without editing the
# gate, and a value that is not a Go duration (or `0`) must be REFUSED rather
# than passed to `go test` — a typo would otherwise turn the gate off or kill the
# run on a bad duration.
#
# Bite proof: scripts/biteproof_api_test_gate_timeout.sh (on demand, measured
# 150s on this host) mutates this gate one way at a time — the flag removed from
# the line, the alarm disabled with `-timeout 0`, the flag left in the printed
# label while the EXECUTED command loses it, and the override hardcoded — reads
# the argv `go` was handed under a stubbed `go`, then runs the REAL selftest gate
# and requires it RED naming the budget. The cheap half is per-run: `selftest`
# check 17 asserts the budget is present, that API_TEST_TIMEOUT reaches the argv,
# and that the check can go RED.
# ---------------------------------------------------------------------------
: "${API_TEST_TIMEOUT:=30m}"
# ANY ZERO DURATION DISABLES THE ALARM, not just the bare `0` (card t_fb782d12).
# `go test -timeout 0s`, `-timeout 0m` and `-timeout 0h` are all zero — the guard
# compared against the literal string "0", so every one of them passed it and the
# gate ran with no bound: a genuinely hung package would block the run forever
# instead of failing it. Measured: `go test ./... -count=1 -timeout 0s` was handed
# to `go` by the gate. The units are optional and any suffix is a Go duration, so
# strip them before comparing.
_att_digits="${API_TEST_TIMEOUT%%[!0-9]*}"
if [ -z "$_att_digits" ]; then
  die "API_TEST_TIMEOUT=$API_TEST_TIMEOUT is not a Go duration (e.g. 30m, 45m, 90s) — there is no leading number to read, so this cannot be a positive budget."
fi
if [ "$(printf '%s' "$_att_digits" | tr -d '0')" = "" ]; then
  die "API_TEST_TIMEOUT=$API_TEST_TIMEOUT disables go test's timeout alarm — every spelling of zero (0, 0s, 0m, 0h) turns the bound OFF, so a genuinely hung package would block the gate with no bound instead of failing. Use a positive duration (the default is 30m, which is a ceiling well above the ~10-12m a loaded host costs 'internal/httpapi')."
fi
case "$API_TEST_TIMEOUT" in
  *[!0-9hms.]*) die "API_TEST_TIMEOUT=$API_TEST_TIMEOUT is not a Go duration (e.g. 30m, 45m, 90s) — refusing to hand 'go test' a bad -timeout, which fails every package on that flag alone." ;;
esac
# THE RACE FLAG IS WHAT MAKES THIS A MERGE GATE. `--quick` drops it for the inner
# loop and says so loudly; a quick run is evidence for the EDITOR, never for a merge
# or a deploy. Without `-race` the run is ~40% cheaper on `internal/httpapi`
# (measured 285s -> 173s), which is the difference between iterating and not running
# the gate at all.
RACE="-race"
if [ "$QUICK" -eq 1 ]; then
  RACE=""
  # A QUICK RUN MUST NOT BE REUSABLE AS A FULL ONE. The per-tree gate record is
  # keyed partly on PREFLIGHT_KEY_EXTRA (gate_result.sh), and a run that recorded
  # `test: pass` under the same key as a -race run would let a lane reuse it as
  # evidence for a MERGE — which it is not. Stamping the key makes the two
  # mutually non-reusable, so a quick pass cannot vouch for a full one.
  # APPENDED, never overwritten: a caller already using the variable keeps theirs.
  export PREFLIGHT_KEY_EXTRA="${PREFLIGHT_KEY_EXTRA:+$PREFLIGHT_KEY_EXTRA;}race=none"
  printf '\n%s\n' "================================================================"
  printf '%s\n' "PREFLIGHT --quick: the api test gate runs WITHOUT -race."
  printf '%s\n' "  This is an INNER-LOOP run, not a merge gate. Before merging or"
  printf '%s\n' "  deploying, run ./scripts/preflight.sh with no flags."
  printf '%s\n' "================================================================"
fi
run test "cd api && go test ./... $RACE -count=1 -timeout $API_TEST_TIMEOUT" \
  bash -c "cd '$REPO_ROOT/api' && go test ./... $RACE -count=1 -timeout '$API_TEST_TIMEOUT'"

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
# web/lint — `eslint .` over the whole web tree.
#
# WHY IT IS A GATE AND NOT A SCRIPT NOBODY RUNS. web/package.json once declared
# `"lint": "next lint"` and Next 16 removed the command, so the script failed on
# every run and nothing noticed because NOTHING CALLED IT — no gate, no Makefile
# target, no doc. web/src/lib/webScripts.test.ts now pins the wiring; this is the
# other half, the part that makes the wiring mean something. The tooling (eslint
# 9 + eslint-config-next, flat config in web/eslint.config.mjs) did not exist in
# the repo at all before this gate.
#
# `eslint .` EXITS 1 on an error, which is the whole point: a lint gate that
# reports and exits 0 is a report, not a gate. `--max-warnings 0` is deliberately
# NOT set — the 65 remaining warnings are the advisory tail (unused vars,
# `window.location.href` navigation, a jsx-a11y advisory) and turning them into a
# hard stop would make every unrelated PR red for noise. The `lint` gate fails on
# ERRORS; the warnings stay visible in the gate output for whoever wants them.
# ---------------------------------------------------------------------------
run lint "cd web && npm run lint (eslint ., repo-wide; fails on errors)" \
  bash -c "cd '$REPO_ROOT/web' && npm run lint"

# ---------------------------------------------------------------------------
# web/webimage — the web IMAGE build: the only command that produces the web
# artifact that actually ships.
#
# WHY IT IS A GATE AND NOT JUST THE SHIP STEP. `deploy/build-and-ship.sh`
# builds the image at its step 6, AFTER it has already run this preflight
# (step 5), so until this gate existed NOTHING in the gate set ever built the
# image — a tree could be green across every gate with `npm run build` passing
# on the HOST while `docker build -f web/Dockerfile .` died. Both recorded
# occurrences of that class are in web/Dockerfile's own header: MCP-PARITY.md
# missing from the build context (`ENOENT: /repo/MCP-PARITY.md` at import, one
# failing test, `next build` never reached) and a context of `web/` instead of
# the repo root (`/api/internal/dbstore/...` instead of `/repo/api/...`).
# web/src/lib/buildContext.test.ts covers the COPY SET for literal reads — it is
# a good guard and it is NOT enough: it cannot see a `.dockerignore` rule that
# excludes a COPY source, a malformed COPY, a runtime-stage COPY --from pointing
# at a path the build stage never produced, or any host-versus-image difference
# that is not a literal read. Only executing the build sees those.
#
# `--output type=cacheonly` is what makes it affordable: BuildKit still executes
# every RUN, resolves every COPY against the real context (including the
# `.dockerignore` filtering) and fails on the same errors, but it exports no
# image. Measured on the agent host: 196s the first time (npm ci + next build in
# the image), ~3s when the cache carries it. Without it a full preflight would
# leave another ~216 MB jomform-web tag behind every run. Note that a warm cache
# does NOT hide the class above: a missing or .dockerignore-excluded COPY source
# fails `failed to compute cache key` even when every other layer is CACHED.
#
# Bite proof: scripts/biteproof_web_image_gate.sh (on demand, ~2.5 min) mutates
# the context and asserts this gate goes RED while the HOST suite stays green —
# the whole point — then restores each file byte-identically. The cheap half is
# per-run: `selftest` checks 13/14 assert the gate exists, is a REAL build (not
# `--check`, which reports "no warnings found" for a COPY source that does not
# exist) and refuses rather than skips when docker or web/Dockerfile is missing.
webimage_run() {
  cd "$REPO_ROOT" || return 1
  echo "webimage: docker build --file web/Dockerfile --output type=cacheonly . (context = repo root)"
  docker build --file web/Dockerfile --output type=cacheonly .
}
run webimage "docker build -f web/Dockerfile . (the shipping web image, cache-only)" webimage_run

# ---------------------------------------------------------------------------
# web/a11y — axe-core. Fails on any critical/serious WCAG violation.
#   FIDELITY: playwright.config.ts starts the server itself
#   (reuseExistingServer is OFF) and we hand it a FREE port. A leftover
#   `next-server` from an earlier run would otherwise be reused and axe would
#   report a clean pass for a build this tree never produced. The port is
#   discovered here rather than fixed, so a stale server cannot collide either.
#   Needs web/build to have produced .next, hence the order.
# ---------------------------------------------------------------------------
# ---- shared: RESERVE a port for a playwright web server --------------------
#
# BOTH the a11y gate and the e2e gate start the app's own production server, so
# both need a port that is free AND cannot be taken between the pick and the bind.
# The reservation is `nc -l -p <port>`: it binds and listens, so `ss` sees it and
# an unprivileged competitor is refused with EADDRINUSE (verified on this host
# before it was used). Callers MUST release it immediately before the server
# starts, so nothing is reused — the gate still starts its OWN server, which is
# what `reuseExistingServer: false` exists to protect.
#
# A retry was the alternative and is deliberately NOT used: it hides the race
# behind a green instead of removing it, and a genuine EADDRINUSE (a peer holding
# the whole range) must still be reported.
A11Y_RESERVED_PORT=""
A11Y_RESERVE_PID=""
a11y_port_reserve() {  # sets A11Y_RESERVED_PORT / A11Y_RESERVE_PID
  A11Y_RESERVED_PORT=""
  A11Y_RESERVE_PID=""
  local candidate
  for candidate in $(seq 3100 3140); do
    if ! ss -lntn "sport = :$candidate" 2>/dev/null | grep -q LISTEN; then
      if command -v nc >/dev/null 2>&1; then
        nc -l -p "$candidate" >/dev/null 2>&1 &
        A11Y_RESERVE_PID=$!
        sleep 0.3
        if ss -lntn "sport = :$candidate" 2>/dev/null | grep -q LISTEN; then
          A11Y_RESERVED_PORT="$candidate"
          return 0
        fi
        kill "$A11Y_RESERVE_PID" >/dev/null 2>&1 || :
        wait "$A11Y_RESERVE_PID" 2>/dev/null || :
        A11Y_RESERVE_PID=""
        continue
      fi
      A11Y_RESERVED_PORT="$candidate"
      return 0
    fi
  done
  echo "no free port in 3100-3140 for the web server" >&2
  return 1
}

# Release the reservation so the server can take the port. Safe when the holder
# already died (a peer stole the port from it too): there is simply nothing to free.
a11y_port_release() {
  if [ -n "$A11Y_RESERVE_PID" ]; then
    kill "$A11Y_RESERVE_PID" >/dev/null 2>&1 || :
    wait "$A11Y_RESERVE_PID" 2>/dev/null || :
  fi
  A11Y_RESERVE_PID=""
}

# ---- web/a11y: the axe family only -----------------------------------------
#
# WHAT THIS GATE IS, AND WHAT IT USED TO BE. `npm run a11y` is `playwright test`
# and playwright's `testDir` is `./e2e`, so this gate used to run the WHOLE
# directory: 29 axe tests plus 134 feature end-to-end tests — a gate whose name
# said accessibility and whose body was mostly not. The feature specs were not
# lost by the split; they are owned by the `e2e` gate below, and the selftest
# asserts every spec belongs to exactly one family so a new spec cannot fall
# between the two.
a11y_run() {
  cd "$REPO_ROOT/web" || return 1
  a11y_port_reserve || return 1
  echo "a11y: starting the app's own server on port $A11Y_RESERVED_PORT (never reusing a leftover)"
  a11y_port_release
  A11Y_PORT="$A11Y_RESERVED_PORT" npm run a11y
}

# ---- web/e2e: the feature end-to-end specs ---------------------------------
#
# These are real regressions (a binned workspace restores, a refused bulk publish
# keeps its sentence, a stale workspace slug does not bounce the dashboard) and
# they earned their coverage by accident, inside the a11y gate. They are NOT
# accessibility checks. A split that left them in NO gate would be a silent
# coverage loss — which is why this gate exists.
#
# SERVER ISOLATION IS THE SAME, DELIBERATELY: `reuseExistingServer` stays false,
# because a leftover `next-server` would serve a build this tree never produced
# and the specs would then pass against the wrong code.
e2e_run() {
  cd "$REPO_ROOT/web" || return 1
  a11y_port_reserve || return 1
  echo "e2e:  starting the app's own server on port $A11Y_RESERVED_PORT (never reusing a leftover)"
  a11y_port_release
  A11Y_PORT="$A11Y_RESERVED_PORT" npm run e2e
}

run a11y "cd web && npm run a11y (axe-core family; the feature specs are gate \`e2e\`)" a11y_run
run e2e "cd web && npm run e2e (the feature specs under web/e2e)" e2e_run

# ---------------------------------------------------------------------------
# web/npmaudit — fail on high/critical advisories. Never `|| true`.
#
# The command is NOT a bare `npm audit` any more: it is the audit PLUS a narrow,
# self-expiring exception list (web/npm-audit-exceptions.json, read by
# web/scripts/npm-audit-gate.mjs). The reason is concrete: the `lint` gate's
# toolchain pulls `braces@3.0.3`, whose advisory has no published fixed release
# (`npm view braces@3.0.4` is a 404), so the alternative to naming it is not
# adding the linter at all. The wrapper keeps the gate's default intact — an
# advisory NOT named in that file still fails here with npm's own report — and
# adds the two properties an allowlist needs to not rot: an exception without an
# `expires` date is REFUSED, an expired one FAILS, and one that is no longer
# reported is printed as STALE.
# ---------------------------------------------------------------------------
run npmaudit "cd web && npm run audit:gate (npm audit --audit-level=high + time-boxed exceptions)" \
  bash -c "cd '$REPO_ROOT/web' && npm run audit:gate"

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
# ITS BITE PROOF IS scripts/refguard_bite.sh, ON DEMAND AND NOT WIRED IN — the same
# convention as the `backup` gate's. t_dc1200ff closed claiming "100 assertions, proven to
# bite (6/6 sabotages red, control green)" while the harness that ran the sabotages was
# never committed, so nothing in the repo could re-run that proof (card t_bac5a2c5). The
# harness sabotages ONE behaviour of the guard at a time in a throwaway copy, runs THIS
# suite against it via REFGUARD_UNDER_TEST, requires RED each time, and refuses a sabotage
# that did not apply rather than letting a byte-identical copy read as a caught mutation.
# It is minutes long and it mutates, so it is not a gate.
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

