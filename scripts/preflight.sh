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
#   scripts/preflight.sh                    # every gate (the DEFAULT — unchanged)
#   scripts/preflight.sh --list             # gate names, then exit
#   scripts/preflight.sh --only govuln      # just one gate (comma-separated)
#   scripts/preflight.sh --skip a11y,web    # everything except these
#   scripts/preflight.sh --for-diff <ref>   # only the gates <ref>..worktree can break
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
# RUNTIME (measured on the agent host): api build 13s, vet 13s, test 2m35s,
#   govuln 25s, web build ~2m, a11y 15s, npm audit 1s, gitleaks 30s, backup ~45s.
#   Budget ~7 minutes for a full run.
#   webimage: ~2m COLD (npm ci + next build in the image; measured 196s) and
#   ~3s warm, because the build cache carries it. So the gate is cheap on a
#   re-run and only pays for itself when something in the context changed —
#   which is precisely when it has something to say. `--no-cache-filter build`
#   is NOT used for that reason: it would pay the cold cost on every run to
#   catch a class the cache cannot hide (a missing or .dockerignore-excluded
#   COPY source still fails `failed to compute cache key` with a warm cache —
#   measured).
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

# `build` is the api build; `web` is `npm run build` in web/. `webimage` is the
# web IMAGE build (docker). All are named so --only/--skip can address them
# without ambiguity.
#
# ADDING A GATE: append the name here AND add its `run <name> ...` call below.
# scripts/preflight_test.sh derives both sides from this file (the inventory via
# `--list`, the runnable set from the `run <name>` call sites) and FAILS if they
# disagree, so a gate named here but never run — the silent-coverage-loss shape —
# is caught by `--only selftest`. No edit to the self-test is needed for gate #N.
GATE_NAMES=(build vet test govuln web webimage a11y npmaudit gitleaks monitor tmpkey backup prunewt refguard selftest)

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
# SEL_DIFF is the narrowed gate set, SPACE-PADDED, and it stays EMPTY unless
# --for-diff/--explain-diff asked for one. selected() reads it, so it has to exist
# (empty) on every other path — an unset variable under `set -u` aborts the run
# before the first gate, which is how this was found.
#
# ${VAR:-} PRESERVES AN INHERITED VALUE, and that is load-bearing. The concurrency
# lock re-invokes this script via flock, so this prologue runs a SECOND time in the
# child while the selection block below skips recomputation (the exported
# `_PREFLIGHT_SEL` says it was already done). A plain `SEL_DIFF=""` therefore wiped
# the parent's narrowing and the child ran EVERY gate — a `--for-diff` run that
# silently stopped being narrowed. Measured, on a change under GATE-INVENTORY.md:
# the parent selected 7 gates and the child ran all 14.
SEL_DIFF="${SEL_DIFF:-}"
# The rest of the selection handoff. Declared the same way, for the same reason —
# the banner and the summary read them on EVERY path (including the no-flag full
# run) without tripping `set -u`.
SEL_DIFF_LINE="${SEL_DIFF_LINE:-}"
SEL_DIFF_SKIPPED="${SEL_DIFF_SKIPPED:-}"
SEL_DIFF_FULL="${SEL_DIFF_FULL:-0}"
SEL_DIFF_REASON="${SEL_DIFF_REASON:-}"
SEL_COUNT="${SEL_COUNT:-0}"
SEL_BASE="${SEL_BASE:-}"

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
    web|a11y)              printf '%s\n' 'web/*' ;;
    npmaudit)              printf '%s\n' 'web/package.json' 'web/package-lock.json' ;;
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
    a11y)                  printf '%s\n' 'web/e2e/*' 'web/playwright.config.ts' ;;
    npmaudit)              printf '%s\n' 'web/package.json' 'web/package-lock.json' ;;
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

  # Which files read the whole checkout (comment lines stripped, so a file is
  # only repo-wide when an ACTIVE line does it).
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
    rw_hits="$(grep -avE '^[[:space:]]*(#|//|\*|/\*)' "$file" 2>/dev/null | grep -cE "$PW_REPOWIDE_RE")"
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
# the lock, so it costs nothing and never queues.
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
  local need_api="$1" need_web="$2" need_a11y="$3" need_docker="$4"

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
g_build=0; g_vet=0; g_test=0; g_govuln=0; g_web=0; g_webimage=0; g_a11y=0; g_npmaudit=0; g_gitleaks=0
selected build    && g_build=1
selected vet      && g_vet=1
selected test     && g_test=1
selected govuln   && g_govuln=1
selected web      && g_web=1
selected webimage && g_webimage=1
selected a11y     && g_a11y=1
selected npmaudit && g_npmaudit=1
selected gitleaks && g_gitleaks=1

need_api=0; need_web=0; need_a11y=0; need_docker=0
if [ $g_build = 1 ] || [ $g_vet = 1 ] || [ $g_test = 1 ] || [ $g_govuln = 1 ]; then need_api=1; fi
if [ $g_web = 1 ]; then need_web=1; fi
if [ $g_a11y = 1 ]; then need_a11y=1; need_web=1; fi
if [ $g_webimage = 1 ]; then need_docker=1; fi
preflight_env "$need_api" "$need_web" "$need_a11y" "$need_docker"

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
if [ "$API_TEST_TIMEOUT" = 0 ]; then
  die "API_TEST_TIMEOUT=0 disables go test's timeout alarm — a genuinely hung package would block the gate with no bound. Use a positive duration (the default is 30m, which is a ceiling well above the ~10-12m a loaded host costs 'internal/httpapi')."
fi
case "$API_TEST_TIMEOUT" in
  *[!0-9hms.]*) die "API_TEST_TIMEOUT=$API_TEST_TIMEOUT is not a Go duration (e.g. 30m, 45m, 90s) — refusing to hand 'go test' a bad -timeout, which fails every package on that flag alone." ;;
esac
run test "cd api && go test ./... -race -count=1 -timeout $API_TEST_TIMEOUT" \
  bash -c "cd '$REPO_ROOT/api' && go test ./... -race -count=1 -timeout '$API_TEST_TIMEOUT'"

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
