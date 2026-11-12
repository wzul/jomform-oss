#!/usr/bin/env bash
# merge-train — gate a BATCH once, on the exact tree that will land on main.
#
# Why this exists: a gate run costs ~27 host-minutes (median 1638 s across 1,305
# recorded runs in 30 days = 634 host-hours/month). Today every card pays that for
# its own branch, and no run ever sees the COMBINATION of branches that main ships.
# The train pays it once per group, on the merged candidate — so the run sees the
# combination, not just the parts.
#
# SCOPE: the batch is gated DIFF-MAPPED by default (`--for-diff <base>`), because the
# full inventory measures ~72 min for a one-PR batch while a diff-mapped run of the
# same batch measures ~5–25 min — and the FULL inventory is owed once per RELEASE
# (AGENTS.md), where build-and-ship.sh runs it on the shipping bytes.
#
# A batch that touches a path where a diff-mapped run would be blind is escalated. It
# escalates to ONE OF TWO things, and the difference is the whole point of this script:
#
#   api-scoped   the only sensitive paths in the diff are Go sources under api/. The
#                batch is gated at the AFFECTED PACKAGE SET — the changed packages
#                plus every package whose build or TEST imports them, transitively —
#                with `go test -race` on exactly those, while build, vet, gofmt,
#                govuln and gitleaks still run over the whole tree. Measured: an
#                api/internal/payments change affects 8 of 49 packages, so the suite
#                is a fraction of `go test ./...` while every package that can SEE
#                the change is still tested. This mode exists because a full-suite
#                escalation on every api-touching PR made merge throughput 1 PR per
#                full suite — 2.6 cores for 10–20 min, serialised behind the gate
#                lock — and nearly every PR touches api/.
#   full         everything else sensitive: deploy/, the Dockerfile, non-Go api/
#                inputs (migrations, go.mod, testdata), and any api/ change this
#                script CANNOT map to packages (`go list` failure, a file in no
#                package directory). The narrowing is never guessed.
#
# A DATA-ONLY batch (docs, screenshots and web/ assets — no api/ path at all) stays
# diff-mapped, and its `test` gate is narrowed to the api PACKAGES WHOSE TESTS READ
# the changed data. `test` is a real dependency of a docs change: api *_test.go
# files walk web/src, docs/ and repo-root files, and the model-id guard in
# api/internal/llm walks the whole tree. Handing preflight the reader packages
# instead of `./...` is what stops a 20-screenshot guide from paying the whole
# module (measured 551 s, and 710 s on a later write of the same tree). The list is
# DERIVED by preflight (`--test-readers`) from the same read table the gate
# selection uses, never listed here; when it cannot be derived exactly, no list is
# passed and `go test ./...` runs as before.
#
# Usage:
#   deploy/merge-train.sh --base origin/main <ref>...            # gate the batch (diff-mapped)
#   deploy/merge-train.sh --base origin/main --full <ref>...     # force the FULL inventory
#   deploy/merge-train.sh --base origin/main --only gofmt web <ref>...   # fast, selftest only
#   deploy/merge-train.sh --base origin/main --land <pr>:<ref>...        # land, then prove the tree
#   deploy/merge-train.sh --select-test-packages <base>          # print the affected api packages
#                                                                # for <base>..HEAD and exit (no gate)
#
#   --base <ref>     base to merge onto (default: origin/main)
#   --full           run the whole inventory even when the diff would allow a mapped run
#   --only <gates>   forward a narrowed selection to preflight (SELFTEST ONLY — never a merge)
#   --land           after green, merge each PR with `gh pr merge --squash --author-email` in
#                    order, then assert origin/main's tree equals the tested candidate tree AND
#                    that every landed commit carries $MERGE_AUTHOR_EMAIL
#                    --land also holds the LAND TOKEN (flock) from BEFORE the base is read, so a
#                    second lane waits cheaply instead of gating a tree it can no longer land.
#                    Env: LAND_LOCK (default ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/
#                    train-land.lock), LAND_WAIT (seconds to wait, default 5400; 0 = fail now)
#   --keep           leave the candidate worktree behind for inspection
#   --bisect-mode    linear (default) | none
#   --check-authors <range>
#                    only assert the author identity of <range> (no gate; the selftest seam)
#   --select-test-packages <base>
#                    only print the api package set this script would test (no gate, no
#                    worktree: it reads <base>..HEAD of the CURRENT tree; the selftest seam)
#
# Exit: 0 green, 1 red (a culprit ref is named), 2 usage, 3 conflict,
#       4 main moved under us / landed tree != tested tree,
#       5 the BASE was already red — nothing was attributed to this batch,
#       6 a landed commit is authored by something other than $MERGE_AUTHOR_EMAIL,
#       7 LAND-BUSY: waited LAND_WAIT for the land token — nothing was gated or landed
set -o pipefail

PROG=${0##*/}
die() { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say() { printf '%s\n' "$PROG: $*"; }

# The land token re-execs this script under `flock`, so the script's own path must still
# resolve AFTER the run has cd'd to the repo root. Resolve it once, here, while $0 is
# relative to the caller's cwd. The original argv is kept for the same reason: the option
# loop below SHIFTS it away, and a re-exec with an empty argv would gate nothing.
SELF=$0
case "$SELF" in
  /*) ;;
  *) SELF=$(cd "$(dirname "$SELF")" && pwd)/$(basename "$SELF") ;;
esac
ORIG_ARGV=("$@")

# ---------------------------------------------------------------- author identity
# The repo rule: the private repo seeds a public OSS repo, so the PERSONAL address must never
# enter a commit. A checkout's git config CANNOT deliver that on this path — `gh pr merge` has
# GitHub create the squash commit, and GitHub authors it from the MERGING ACCOUNT. That is why
# every squash on main carried the personal gmail while its committer read
# `GitHub <noreply@github.com>`. `--author-email` is the supported seam (GraphQL
# MergePullRequestInput.authorEmail), and --land verifies it took effect instead of trusting it.
#
# The address must be the ID-PREFIXED form, and that is not a style choice. Measured 2026-11-04
# against the merge API on a throwaway repo: `wzul@users.noreply.github.com` — the legacy form
# this repo's git config uses, and one GitHub does accept on a PUSHED commit — is REFUSED here
# with `GraphQL: Invalid email address (mergePullRequest)`, while
# `19372567+wzul@users.noreply.github.com` (the account id `gh api user` reports) lands the
# squash as `A:Wan Zulkarnain <19372567+wzul@users.noreply.github.com> C:GitHub <noreply@github.com>`.
# So `git config user.email` (legacy, for commits a checkout authors) and this variable (the ID
# form, for commits GitHub authors) are deliberately two spellings of the same identity.
#
# `${VAR-default}` and not `${VAR:-default}`: an EXPLICITLY empty value is a mistake worth
# refusing below, not a silent fall back to the default.
MERGE_AUTHOR_EMAIL=${MERGE_AUTHOR_EMAIL-19372567+wzul@users.noreply.github.com}

# Authors in a range that are NOT $MERGE_AUTHOR_EMAIL, one per line.
foreign_authors() {   # $1 = rev range
  git log --format='%ae' "$1" 2>/dev/null | sort -u | grep -v -x -F -- "$MERGE_AUTHOR_EMAIL"
}

# ---------------------------------------------------------------------------
# The AFFECTED api PACKAGE SET — what the api test gate runs for a batch that
# touches api/.
#
# WHY THIS IS A REVERSE REACHABILITY WALK AND NOT `go list -deps`. `.Deps` answers
# "what does this package import", which is the wrong direction: a change to
# `internal/payments` is observable by `internal/httpapi`, and httpapi does not
# appear in payments' dependency list. And `.Deps` EXCLUDES test-only imports
# unless `-test` is asked for, but a package whose *_test.go imports the changed
# package can fail on that change just as loudly as one that links it. So the
# graph here is the union of the DIRECT edges `Imports`, `TestImports` and
# `XTestImports`, walked backwards from the changed packages: every package that
# can see the change at build time or in a test ends up in the set.
#
# FAIL SAFE, NEVER GUESS. Anything that cannot be mapped to packages makes this
# return 1, and the caller runs the FULL inventory instead: a changed api/ file
# that is not .go (a migration, go.mod, a testdata fixture), a .go file in a
# directory `go list` does not list as a package, or a tree `go list` cannot even
# enumerate (a compile-broken package). A narrowed suite that silently omitted a
# package would be a blind gate, which is worse than a slow one.
#
# $1 = worktree root, $2 = base sha. Prints the import paths on stdout, one per
# line, and a "select: N of M package(s)" summary on stderr.
select_test_packages() {
  local tree=$1 base=$2 changed
  [ -d "$tree/api" ] || return 1
  changed=$(git -C "$tree" diff --name-only "$base" HEAD -- api/ 2>/dev/null) || return 1
  [ -n "$changed" ] || return 1
  # An environment block has a size ceiling; a diff that big is not a batch where
  # narrowing buys anything, so hand it to the full inventory.
  [ ${#changed} -lt 100000 ] || return 1
  TRAIN_CHANGED="$changed" python3 - "$tree/api" <<'PY'
import collections, os, subprocess, sys

api = os.path.abspath(sys.argv[1])
root = os.path.dirname(api)
files = [p.strip() for p in os.environ.get("TRAIN_CHANGED", "").splitlines() if p.strip()]
if not files:
    sys.exit(1)
# A non-Go change under api/ (a migration, go.mod/go.sum, a testdata fixture) has
# no package of its own and can still break any package that reads it. Not mappable.
notgo = [p for p in files if not p.endswith(".go")]
if notgo:
    sys.stderr.write("select: %s is not a Go source file — cannot map a package\n" % notgo[0])
    sys.exit(1)

fmt = r'{{.Dir}}{{"\t"}}{{.ImportPath}}{{"\t"}}{{join .Imports " "}}{{"\t"}}{{join .TestImports " "}}{{"\t"}}{{join .XTestImports " "}}'
try:
    res = subprocess.run(["go", "list", "-f", fmt, "./..."], cwd=api,
                         capture_output=True, text=True)
except OSError as e:
    sys.stderr.write("select: cannot run go list: %s\n" % e)
    sys.exit(1)
if res.returncode != 0:
    first = (res.stderr.strip().splitlines() or ["(no stderr)"])[0]
    sys.stderr.write("select: go list failed in api/ (%s) — cannot map packages in a tree that does not list\n" % first)
    sys.exit(1)

dir2pkg, edges, total = {}, {}, 0
for line in res.stdout.splitlines():
    parts = line.split("\t")
    if len(parts) < 2:
        continue
    d, imp = parts[0], parts[1]
    total += 1
    dir2pkg[d] = imp
    deps = []
    for field in parts[2:]:
        deps.extend(field.split())
    edges[imp] = deps

seeds = set()
for p in files:
    d = os.path.normpath(os.path.join(root, os.path.dirname(p)))
    if d not in dir2pkg:
        sys.stderr.write("select: %s is not a directory go list reports as a package\n"
                         % os.path.dirname(p))
        sys.exit(1)
    seeds.add(dir2pkg[d])
if not seeds:
    sys.exit(1)

# Reverse edges over build AND test imports, then reachability back from the seeds.
rev = collections.defaultdict(set)
for imp, deps in edges.items():
    for dep in deps:
        rev[dep].add(imp)
seen, queue = set(seeds), list(seeds)
while queue:
    cur = queue.pop()
    for parent in rev.get(cur, ()):
        if parent not in seen:
            seen.add(parent)
            queue.append(parent)

selected = sorted(seen)
if not selected:
    sys.exit(1)
sys.stderr.write("select: %d of %d package(s) — changed plus every package whose build or test imports them\n"
                 % (len(selected), total))
print("\n".join(selected))
PY
}

# Certify that every commit in the range carries the repo identity.
# 0 clean; 2 when the range cannot be read (never certify what was not read);
# 6 when a foreign author is present.
assert_merge_authors() {   # $1 = rev range
  local n bad
  if ! n=$(git rev-list --count "$1" 2>/dev/null); then
    say "cannot read '$1' — refusing to certify an author identity it never read"
    return 2
  fi
  bad=$(foreign_authors "$1") || bad=""
  if [ -n "$bad" ]; then
    say "AUTHOR MISMATCH in '$1': $(printf '%s ' "$bad" | tr '\n' ' ')— want $MERGE_AUTHOR_EMAIL"
    say "the merge landed, but not under the repo identity; fix the merge path before the next one"
    return 6
  fi
  say "author identity $MERGE_AUTHOR_EMAIL on all $n commit(s) of '$1'"
  return 0
}

BASE=origin/main
ONLY=""
FULL=0
LAND=0
KEEP=0
CHECK_AUTHORS=""
SELECT_PKGS=""
BISECT_MODE=linear
REFS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --base)        BASE=${2:?--base needs a ref}; shift 2 ;;
    --only)        ONLY=$2; shift 2 ;;
    --full)        FULL=1; shift ;;
    --land)        LAND=1; shift ;;
    --keep)        KEEP=1; shift ;;
    --bisect-mode) BISECT_MODE=${2:?}; shift 2 ;;
    --check-authors) CHECK_AUTHORS=${2:?--check-authors needs a rev range}; shift 2 ;;
    --select-test-packages) SELECT_PKGS=${2:?--select-test-packages needs a base ref}; shift 2 ;;
    -h|--help)     sed -n '2,64p' "$0"; exit 0 ;;
    --)            shift; REFS+=("$@"); break ;;
    -*)            die "unknown flag: $1" ;;
    *)             REFS+=("$1"); shift ;;
  esac
done

# Identity-only mode: assert a range's authors and exit — no refs, no candidate, no gate.
# This is the seam the selftest drives, so the guard is proven to bite rather than trusted.
if [ -n "$CHECK_AUTHORS" ]; then
  ROOT=$(git rev-parse --show-toplevel) || die "not in a git repo"
  cd "$ROOT" || die "cannot cd $ROOT"
  assert_merge_authors "$CHECK_AUTHORS"
  exit $?
fi

# Package-selection-only mode: print the api package set this train would test for
# `<base>..HEAD` and exit — no candidate worktree, no gate. This is the seam the
# selftest drives, so the SELECTION is proven (contains an importer, omits an
# unrelated package) on the same code path the gate uses, not on a copy of it.
if [ -n "$SELECT_PKGS" ]; then
  ROOT=$(git rev-parse --show-toplevel) || die "not in a git repo"
  cd "$ROOT" || die "cannot cd $ROOT"
  git rev-parse --verify --quiet "$SELECT_PKGS^{commit}" >/dev/null \
    || die "base ref not found: $SELECT_PKGS"
  select_test_packages "$ROOT" "$(git rev-parse "$SELECT_PKGS^{commit}")"
  exit $?
fi

# `--land` takes `<pr>:<ref>` per entry, and the SAME argv must also build the candidate.
# Split them once here: the build uses the ref, the landing uses the PR number. (Without
# this, `--land 1545:pr1545` could gate nothing — `git rev-parse 1545:pr1545` does not
# resolve a refname, so the only working spelling was a ref-only batch that never landed.)
PRS=()
_split=()
for _e in "${REFS[@]}"; do
  case "$_e" in
    [0-9]*:*) PRS+=("${_e%%:*}"); _split+=("${_e#*:}") ;;
    *)        PRS+=("");           _split+=("$_e") ;;
  esac
done
REFS=("${_split[@]}")

[ ${#REFS[@]} -gt 0 ] || die "no refs given (nothing to test)"
# The train TAKES OWNERSHIP of this variable (it computes it, gate_candidate exports
# it). An inherited value would narrow the api suite of a run the train chose not to
# narrow — a silent scope change on the one path that decides what merges.
[ -n "${PREFLIGHT_TEST_PACKAGES:-}" ] && \
  die "PREFLIGHT_TEST_PACKAGES is already set in this environment — the TRAIN computes it from the batch's diff; an inherited list would silently narrow a run the train did not narrow. Unset it and re-run."
[ "$LAND" = 1 ] && [ -n "$ONLY" ] && \
  die "--land refuses a narrowed selection: --only exists for self-tests, never for a landing"
[ "$LAND" = 1 ] && [ -z "$MERGE_AUTHOR_EMAIL" ] && \
  die "--land refuses an empty MERGE_AUTHOR_EMAIL: landing under an unknown identity is the bug"
# The repo rule, in code: a landing may only carry a GitHub no-reply identity. A personal or
# corporate address here would put it on main AND in the public mirror — the defect this seam
# exists to prevent. (The API rejects a spelling it does not accept; this rejects a CLASS of
# address we must never land at all, and does it before the gate is paid for.)
case "$MERGE_AUTHOR_EMAIL" in
  *@users.noreply.github.com) ;;
  *) die "--land refuses '$MERGE_AUTHOR_EMAIL': the merge author must be a *@users.noreply.github.com identity" ;;
esac

# The repo root is the one that owns this script's tree.
ROOT=$(git rev-parse --show-toplevel) || die "not in a git repo"
cd "$ROOT" || die "cannot cd $ROOT"

# ---------------------------------------------------------------- the land token
# Two lanes may verify concurrently, but only ONE may enter the window between reading the
# base and pushing the merge. Without this, the second `--land` run reads the same base,
# waits out the (serialised) gate, and then dies on `main moved under us` — a full gate's
# wall time spent to land nothing, and the card returns to the queue to repeat it.
#
# The token is taken BEFORE the base is resolved, so a waiting lane starts on a base that
# is still current when it gets to land. Verify-only runs (no --land) do not take it: they
# never push, and blocking them would serialise work that is safe to parallelise.
#
# WHY `flock --close <file> <cmd>` AND NOT `exec 9>>file` + `flock 9`: bash does NOT set
# FD_CLOEXEC on a descriptor opened by a redirection, and FLOCK lives on the open file
# DESCRIPTION — so every child born after the token (git, the systemd-run preflight, npm,
# gh) inherits fd 9 and keeps the lock alive after the train itself is gone. Measured: with
# a gate stub still running, a SIGTERM to the train left the stub and its `sleep` holding
# the token, and a second lane waited the full LAND_WAIT (5400 s) for a train that no longer
# existed. `LAND_WAIT` bounds the WAITER, not the HOLDER, so a hung gate would hold it
# forever. `--close` closes the descriptor in the child (0 fds on the token), the wrapper
# keeps the lock for exactly as long as the train lives, and killing the train releases it.
LAND_LOCK="${LAND_LOCK:-${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/train-land.lock}"
LAND_WAIT="${LAND_WAIT:-5400}"
if [ "$LAND" = 1 ]; then
  if [ -z "${_LAND_TOKEN:-}" ]; then
    # Outer run: take the token once, then RE-EXEC this script under it. `_LAND_TOKEN` is
    # exported so the re-executed copy runs the body instead of forking another flock.
    _lockdir=${LAND_LOCK%/*}
    [ -d "$_lockdir" ] || mkdir -p "$_lockdir" 2>/dev/null || true
    command -v flock >/dev/null 2>&1 \
      || die "--land needs flock(1) for the land token: $LAND_LOCK"
    export _LAND_TOKEN=1 LAND_LOCK LAND_WAIT
    flock --close -w "$LAND_WAIT" -E 7 "$LAND_LOCK" "$SELF" "${ORIG_ARGV[@]}"; _rc=$?
    if [ "$_rc" = 7 ]; then
      say "LAND-BUSY waited ${LAND_WAIT}s for the land token ($LAND_LOCK) — nothing was gated or landed"
    fi
    exit "$_rc"
  fi
  say "land token held ($LAND_LOCK); the base read below is the base we land on"
fi

git rev-parse --verify --quiet "$BASE" >/dev/null || die "base ref not found: $BASE"
BASE_SHA=$(git rev-parse "$BASE")
BASE_TREE=$(git rev-parse "$BASE^{tree}")

# THE GATE'S CPU BUDGET, AND WHY THIS SCRIPT'S IS 400% (card t_d1bc5164).
#
# 300% is enough for one suite. It is NOT enough for the three browser gates this
# script runs CONCURRENTLY when the diff maps to them (scripts/preflight.sh's
# `run_browser_group` starts a11y, e2e-builder and e2e-core at once, each with its own
# `next start` plus playwright workers) — and the shortfall does not look like
# contention, it looks like a FAILED SUITE. Measured on this 6-core host, on ONE
# unchanged candidate tree (7670267d7, PR #1722):
#
#   CPUQuota=300%  train: a11y FAIL 1014 s, re-run FAIL 817 s -> RED on the batch,
#                          BISECT=combination, nothing landed
#   a11y alone     PASS 287 s        (scripts/preflight.sh --only a11y)
#   all three suites concurrent, UNCAPPED: a11y 220/220 PASS, 9.6 min
#   CPUQuota=400%  GREEN: a11y 604 s, e2e-builder 606 s, e2e-core 655 s -> landed
#
# A 287 s suite that takes 817-1014 s and reds is starvation, not assertions; every
# such red costs a whole train run and names an innocent ref. 400% is the value
# AGENTS.md documents for a gated run on this host, so the train uses it too.
#
# The release's GATE_CPU_QUOTA is a SEPARATE knob (deploy/build-and-ship.sh, 300% since
# #1551, where it also bounds BUILD_CPU_QUOTA); this variable is the train's alone.
#
# The cap still leaves cores for other lanes (400% of 6 cores = 4); an empty value runs
# UNCAPPED and is the wrong default on a shared host. systemd's CPUQuota counts CORES;
# `nice` would not help (it changes who runs, not how many threads stay runnable, so
# the load average is untouched).
TRAIN_CPU_QUOTA="${TRAIN_CPU_QUOTA:-400%}"
LAUNCH=()
if [ -n "$TRAIN_CPU_QUOTA" ]; then
  if command -v systemd-run >/dev/null 2>&1; then
    LAUNCH=(systemd-run --scope -q -p "CPUQuota=$TRAIN_CPU_QUOTA")
  else
    say "WARNING: systemd-run not found — the train gates UNCAPPED"
  fi
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/merge-train.XXXXXX") || die "mktemp failed"
CAND="$WORK/candidate"
cleanup() {
  if [ "$KEEP" = 1 ]; then
    say "candidate kept at $CAND (last gated tree ${CAND_TREE:-none}; bisect may have rebuilt it)"
  else
    git worktree remove --force "$CAND" >/dev/null 2>&1
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

# ---------------------------------------------------------------- gate runner
# The `web` gate refuses to pretend it built anything without node_modules (it reds
# with "next is missing" rather than exiting 0 having built nothing). A fresh candidate
# worktree has none, so seed it before gating.
#
# THE SEEDER IS SHARED, NOT DUPLICATED: deploy/web_modules.sh is the same code the
# RELEASE path (deploy/cut-release.sh) runs on its tag-pinned worktree, so the two
# lanes cannot drift about what "seeded" means. It copies rather than hardlinking on
# purpose: hardlinking would share `node_modules/.cache` between two concurrent
# builds, which is exactly the kind of shared mutable state that produces FALSE gate
# results. `npm ci` per candidate is the fallback (~81 s), and the train's own cache
# ($WORK/web_modules, one copy for the whole run) keeps a bisect rebuild at a local
# copy (~20 s) instead of another install.
WM_LIB="$ROOT/deploy/web_modules.sh"
[ -r "$WM_LIB" ] || die "$WM_LIB is missing — the train cannot seed web/node_modules, and a web-side gate would then refuse the candidate"
# shellcheck source=deploy/web_modules.sh
. "$WM_LIB"

seed_web_modules() {
  [ -x "$CAND/web/node_modules/.bin/next" ] && return 0
  ( WM_PROG=$PROG; WM_REPO=$ROOT; WM_CACHE_DIR="$WORK/web_modules" \
    WM_TEMPLATE="${TRAIN_NODE_MODULES:-}" WM_NPM_LOG="$WORK/npm.log" \
    WM_LAUNCH=("${LAUNCH[@]}"); wm_seed "$CAND" ) || return 1
  [ -x "$CAND/web/node_modules/.bin/next" ] || return 1
  return 0
}

# Only the web-side gates read `web/node_modules` (the shell suites, next build, the specs).
# A narrowed run that selects none of them must not pay ~81 s of `npm ci` for nothing — that
# is the difference between a 40 s self-test and a 5 min one.
needs_seed=1
if [ -n "$ONLY" ]; then
  needs_seed=0
  for _g in $ONLY; do
    case "$_g" in
      web|webimage|selftest|e2e|a11y|lint|speccounts|design) needs_seed=1 ;;
    esac
  done
fi
# A stub gate (self-test hook) reads no node_modules, so seeding would be pure cost.
[ -n "${TRAIN_GATE_CMD:-}" ] && needs_seed=0

# One gate invocation, on the candidate worktree, exactly as a release would run it.
# SCOPED_N / SCOPED_TOTAL / SCOPED_KIND are set here (not by the caller) because a
# bisect re-runs this per prefix, and each prefix has its own affected set.
SCOPED_N=0
SCOPED_TOTAL=0
SCOPED_KIND=""
gate_candidate() {   # -> 0 green, 1 red
  if [ "$needs_seed" = 1 ]; then
    seed_web_modules || { say "cannot seed web/node_modules — refusing to gate"; return 1; }
  fi
  local args=() pkgs="" readers=""
  SCOPED_N=0; SCOPED_TOTAL=0; SCOPED_KIND=""
  # A bisect re-runs this function, and the first call may have exported a list. Only the
  # api-scoped branch below may leave one set: a stale export would make a LATER full run
  # refuse (preflight treats a list on a default run as a lie) or narrow a run the train
  # did not narrow.
  unset PREFLIGHT_TEST_PACKAGES PREFLIGHT_TEST_PACKAGES_WHY
  if [ -n "$ONLY" ]; then
    args=(--only "$ONLY")
  elif [ "$MODE" = api-scoped ]; then
    # Hand preflight the packages this batch can affect. It refuses a package list
    # without --for-diff, and stamps the record so this pass cannot be reused as a
    # full one — so this mode can never be read as `go test ./...`.
    if pkgs=$(select_test_packages "$CAND" "$BASE_SHA" 2>"$WORK/select.log"); then
      export PREFLIGHT_TEST_PACKAGES="$pkgs"
      SCOPED_N=$(printf '%s\n' "$pkgs" | grep -c .)
      SCOPED_TOTAL=$(sed -n 's/^select: [0-9][0-9]* of \([0-9][0-9]*\).*/\1/p' "$WORK/select.log" | head -1)
      args=(--for-diff "$BASE_SHA")
    else
      # The selection failed on THIS tree (a bisect prefix can differ from the full
      # candidate). Never fall through to a narrowed run with no list: escalate.
      sed 's/^/    | /' "$WORK/select.log" 2>/dev/null | head -2
      say "cannot map this diff to api packages — running the FULL inventory instead"
      unset PREFLIGHT_TEST_PACKAGES PREFLIGHT_TEST_PACKAGES_WHY
      MODE=full
    fi
  elif [ "$MODE" = scoped ]; then
    # The batch is gated against the base it will land on: preflight maps the changed
    # paths to the gates that can see them, and names the ones it did not run.
    #
    # `test` can be one of those gates on a DATA-ONLY diff, because api *_test.go
    # files read web/src, docs/ and repo-root files (and one guard walks the whole
    # tree). Ask preflight WHICH PACKAGES own those readers and hand it that set;
    # without this the batch pays `go test ./...` for the whole module — measured
    # 551-710 s — while the rest of a docs verify is minutes.
    #
    # NO LIST UNLESS PREFLIGHT CAN PROVE ONE. `--test-readers` exits 1 (reason on
    # stderr) for an api/ path, a non-test reader, a reader at the module root or a
    # tree `go list` cannot enumerate; the run then keeps ./... exactly as before.
    if readers=$( cd "$CAND" && ./scripts/preflight.sh --test-readers "$BASE_SHA" 2>"$WORK/select.log" ); then
      export PREFLIGHT_TEST_PACKAGES="$readers"
      export PREFLIGHT_TEST_PACKAGES_WHY=readers
      SCOPED_N=$(printf '%s\n' "$readers" | grep -c .)
      SCOPED_TOTAL=$(sed -n 's/^select: [0-9][0-9]* of \([0-9][0-9]*\).*/\1/p' "$WORK/select.log" | head -1)
      SCOPED_KIND=readers
      say "test scope: ${SCOPED_N}/${SCOPED_TOTAL} api package(s) — the *_test.go readers of the changed data"
    else
      unset PREFLIGHT_TEST_PACKAGES PREFLIGHT_TEST_PACKAGES_WHY
      sed 's/^/    | /' "$WORK/select.log" 2>/dev/null | head -2
    fi
    args=(--for-diff "$BASE_SHA")
  fi
  # The same overridable durable default every other site names (preflight.sh emits this
  # exact advice, and its self-test reds on a durable site that quotes a different path).
  local env_file="${TEST_ENV_FILE:-${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}}"
  if [ -f "$env_file" ]; then
    # shellcheck disable=SC1090
    set -a; . "$env_file"; set +a
  fi
  local cmd=(./scripts/preflight.sh)
  # Self-test hook: prove WHICH argv the train would run without paying for a real gate.
  [ -n "${TRAIN_GATE_CMD:-}" ] && read -r -a cmd <<<"$TRAIN_GATE_CMD"
  ( cd "$CAND" && "${LAUNCH[@]}" "${cmd[@]}" "${args[@]}" ) >"$WORK/gate.log" 2>&1
  local rc=$?
  tail -3 "$WORK/gate.log" | sed 's/^/    | /'
  return $(( rc == 0 ? 0 : 1 ))
}

# Is the BASE itself already red? The record store is keyed on the tree, so a batch that
# reds on a gate main already fails would otherwise be attributed to the first ref in it —
# a false BISECT and a rework card for work that is innocent. Absence of a record proves
# nothing (a scoped run skips gates), so only a RECORDED RED blocks bisection.
base_is_red() {   # -> 0 and prints the failing gates when the store already says BASE is red
  local dir=${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}
  local f="$dir/$BASE_TREE.json"
  [ -f "$f" ] || return 1
  python3 - "$f" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(1)
if int(d.get("failed") or 0) > 0:
    bad = sorted(k for k, v in (d.get("gates") or {}).items()
                 if str((v or {}).get("status")) != "pass")
    print(" ".join(bad) or "unknown")
    sys.exit(0)
sys.exit(1)
PY
}

# ------------------------------------------------------------------- build it
# Rebuild a candidate holding the first $1 refs. Squash-per-ref mirrors what
# `gh pr merge --squash` produces, so the tested tree is the tree that lands.
build_candidate() {   # $1 = count, rest ignored -> 0 ok, 3 conflict
  local n=$1 i
  git worktree remove --force "$CAND" >/dev/null 2>&1
  git worktree add -q --detach "$CAND" "$BASE_SHA" || die "cannot create candidate worktree"
  for (( i = 0; i < n; i++ )); do
    local ref=${REFS[$i]}
    local sha
    sha=$(git rev-parse --verify --quiet "$ref") || die "ref not found: $ref"
    if ! git -C "$CAND" merge --squash "$sha" >"$WORK/merge.log" 2>&1; then
      say "CONFLICT $ref"
      grep -E '^CONFLICT|^error:|both modified' "$WORK/merge.log" | head -5 | sed 's/^/    | /'
      return 3
    fi
    if git -C "$CAND" diff --cached --quiet; then
      say "empty $ref (already in $BASE) — skipped"
      continue
    fi
    git -C "$CAND" commit -q -m "train: $ref" -m "batched by deploy/merge-train.sh on $BASE_SHA"
  done
  return 0
}

say "base $BASE ($BASE_SHA), ${#REFS[@]} ref(s): ${REFS[*]}"
build_candidate ${#REFS[@]} || exit 3

CAND_TREE=$(git -C "$CAND" rev-parse HEAD^{tree})
say "candidate tree $CAND_TREE"
if [ "$CAND_TREE" = "$BASE_TREE" ]; then
  say "candidate is identical to $BASE — nothing to test"
  exit 0
fi

# ------------------------------------------------------------------- decide scope
# The FULL inventory is owed once per RELEASE (AGENTS.md), not once per merge: a
# diff-mapped run of a batch measures ~5–25 min against ~72 min for the full one, and
# the release gate still runs the full inventory on the bytes that ship. A batch that
# moves a path whose blast radius reaches past its own files is escalated — to one of
# two things:
#
#   api-scoped  every sensitive path is api/**.go  -> gate the affected package set
#   full        anything else (deploy/, Dockerfile, a migration .sql, go.mod, a
#               testdata fixture, a tree `go list` cannot enumerate)
#
# Fail safe, not fail fast: an unmappable api/ change is FULL, never a narrowed run
# with a hole in it.
#
# TWO PATTERNS, NOT ONE, AND THE SPLIT IS THE FIX (PR #1701 measured 13 min).
# The structural half is about WHERE a path lives: api/, deploy/, a Dockerfile —
# code that owns the gate. The keyword half is about WHAT a path IS: a payment,
# auth or session surface whose blast radius the package graph cannot express.
# Applying the keyword half to the whole path made a documentation screenshot
# escalate: `docs/guides/surau-donation/04-chip-kunci.png` contains "chip", so a
# batch that touched NO Go, deploy or auth file paid the 27-gate FULL inventory
# (632 s of Go gates + both e2e halves) instead of the 15 gates its diff maps to.
# A documentation or binary-asset path cannot change what runs — it is DATA, and
# data inputs are the dependency table's job: `--for-diff` claims a path only when
# a gate really reads it, and an UNCLAIMED path already widens to the full run by
# the fail-closed rule in scripts/preflight.sh. So no coverage is lost by keeping
# the keyword test out of documentation and out of non-code file types.
ESCALATE_STRUCT_RE='^(api/|deploy/|Dockerfile)'
ESCALATE_KEY_RE='migrat|payment|chip|billing|settle|refund|auth|session|oauth|passkey|totp|2fa|credential'
# An INERT path: documentation prose, or a binary/static asset. Never code — and the
# test is a FILE EXTENSION, never a directory name. A `(^|/)docs/` arm made "inert"
# mean "anywhere under a directory called docs", and `web/src/app/docs/**` is 50
# `.ts/.tsx` SOURCE files, four of them carrying a keyword
# (`web/src/app/docs/mcp/oauth/page.tsx`, `.../docs/api/authentication/page.tsx`,
# `.../docs/guides/payment-fees/page.tsx`, `.../docs/payment-fees/page.tsx`): a page
# whose blast radius the Go package graph cannot express was gated diff-mapped on the
# strength of its directory. Measured on a one-commit candidate touching
# `web/src/app/docs/mcp/oauth/page.tsx`: `escalated` with the directory arm,
# `diff-mapped` without it. Directory inertness also bought nothing — 0 tracked files
# under the repo-root `docs/` lack an inert extension — so
# `docs/guides/surau-donation/04-chip-kunci.png` stays diff-mapped on `.png` alone.
ESCALATE_INERT_RE='\.(md|mdx|markdown|png|jpe?g|gif|webp|svg|ico|pdf|txt)$'
MODE=full
WHY="--full (forced)"
if [ -n "$ONLY" ]; then
  MODE=narrowed; WHY="[$ONLY] (selftest)"
elif [ "$FULL" = 0 ]; then
  DIFF_ALL=$(git -C "$CAND" diff --name-only "$BASE_SHA" HEAD)
  ESC_STRUCT=$(printf '%s\n' "$DIFF_ALL" | grep -E "$ESCALATE_STRUCT_RE" || true)
  ESC_KEY=$(printf '%s\n' "$DIFF_ALL" | grep -Ev "$ESCALATE_INERT_RE" | grep -Ei "$ESCALATE_KEY_RE" || true)
  ESC_ALL="$(printf '%s\n%s\n' "$ESC_STRUCT" "$ESC_KEY" | grep -v '^$' | sort -u || true)"
  ESC_OTHER=$(printf '%s\n' "$ESC_ALL" | grep -v -E '^api/' || true)
  if [ -z "$ESC_ALL" ]; then
    MODE=scoped; WHY="diff-mapped vs $BASE_SHA"
  elif [ -n "$ESC_OTHER" ]; then
    # A sensitive path OUTSIDE api/ (deploy/, Dockerfile, a web auth/session file):
    # the package graph says nothing about it. Full inventory, as before.
    MODE=full; WHY="escalated ($(printf '%s\n' "$ESC_OTHER" | head -5 | tr '\n' ' '))"
  elif pkgs=$(select_test_packages "$CAND" "$BASE_SHA" 2>"$WORK/select.log"); then
    # A RED BASE IS NOT A TIME TO NARROW. If the store already says the base tree
    # fails a gate, a scoped batch that silently skips that package would report
    # GREEN on a base main already fails — the exact false green the `base_is_red`
    # guard below refuses to ATTRIBUTE to a batch. Pay the inventory instead.
    if _basered=$(base_is_red); then
      MODE=full
      WHY="escalated (base tree $BASE_TREE is already red: ${_basered} — a narrowed suite would hide it)"
    else
      MODE=api-scoped
      WHY="api-scoped: changed+importers ($(sed -n 's/^select: //p' "$WORK/select.log" | head -1))"
    fi
  else
    MODE=full
    WHY="escalated ($(printf '%s\n' "$ESC_ALL" | head -3 | tr '\n' ' ') — an api/ change this script cannot map to packages: $(head -1 "$WORK/select.log" | sed 's/^select: //'))"
  fi
fi

say "gate: $WHY $([ ${#LAUNCH[@]} -gt 0 ] && echo "under CPUQuota=$TRAIN_CPU_QUOTA" || echo 'UNCAPPED')"
if gate_candidate; then
  if [ "$MODE" = full ]; then
    say "GREEN candidate tree $CAND_TREE (full inventory)"
  elif [ "$MODE" = api-scoped ]; then
    _untouched=$(( ${SCOPED_TOTAL:-0} - ${SCOPED_N:-0} ))
    [ "$_untouched" -ge 0 ] || _untouched=0
    say "GREEN candidate tree $CAND_TREE (api-scoped: changed+importers, ${SCOPED_N}/${SCOPED_TOTAL} packages — NOT full coverage; NEVER ran: go test ./... for the other $_untouched package(s); the RELEASE owes the full inventory)"
  elif [ "$SCOPED_KIND" = readers ]; then
    _untouched=$(( ${SCOPED_TOTAL:-0} - ${SCOPED_N:-0} ))
    [ "$_untouched" -ge 0 ] || _untouched=0
    say "GREEN candidate tree $CAND_TREE (readers-scoped: the ${SCOPED_N}/${SCOPED_TOTAL} api package(s) whose tests READ the changed data — NOT full coverage; NEVER ran: go test ./... for the other $_untouched package(s); the RELEASE owes the full inventory)"
  else
    say "GREEN candidate tree $CAND_TREE ($MODE — NOT full coverage; the RELEASE owes the full inventory)"
  fi
else
  say "RED on the batch"
  # Before blaming a ref, make sure the base it was merged onto was not already red. A
  # skipped gate is not a passing gate, but neither is an innocent ref a culprit.
  if _base_fails=$(base_is_red); then
    say "BASE-RED base tree $BASE_TREE was already red (${_base_fails}) before this batch"
    say "  refusing to bisect: prefix attribution would name an innocent ref"
    say "  fix the base (file a jf-dev card for ${_base_fails}) and re-run the train"
    exit 5
  fi
  if [ "$BISECT_MODE" = none ]; then exit 1; fi
  # A cap below 400% is the one measured way this host turns a 287 s suite into an
  # 817-1014 s red (see the CPU-budget note at the top of this file), so say it on every
  # red: the next lane reads the BISECT line and little else.
  _quota_pct=${TRAIN_CPU_QUOTA%\%}
  case "${_quota_pct:-x}" in ''|*[!0-9]*) _quota_pct="" ;; esac
  if [ -n "$_quota_pct" ] && [ "$_quota_pct" -lt 400 ]; then
    say "note: this gate ran under CPUQuota=$TRAIN_CPU_QUOTA; the three concurrent browser gates need >=400% on this host, so a red here can be CONTENTION — re-run the SAME tree at TRAIN_CPU_QUOTA=400% before blaming a ref"
  fi
  # A batch of ONE cannot be a COMBINATION: with a single ref there is no second ref to
  # combine with, so `BISECT=combination` named NO culprit at all and sent a rework card
  # after bytes that pass on their own (card t_d1bc5164: that is exactly what the 300%
  # run printed for an innocent PR #1722). The red is either that ref's own tree or the
  # gate's environment; naming the ref is the most this run can honestly say.
  if [ "${#REFS[@]}" -eq 1 ]; then
    say "BISECT=${REFS[0]} (the batch holds one ref — there is nothing to bisect)"
    say "  if this ref is green on its own, the red was the gate's environment, not its tree"
    exit 1
  fi
  # Prefix bisect: smallest prefix that reds names the culprit (refs are in queue order).
  local_bad=""
  for (( k = 1; k < ${#REFS[@]}; k++ )); do
    say "bisect: testing first $k ref(s)"
    build_candidate $k || { say "bisect: prefix $k conflicted — cannot attribute"; exit 1; }
    if ! gate_candidate; then
      local_bad=${REFS[$((k-1))]}
      break
    fi
  done
  if [ -z "$local_bad" ]; then
    local_bad=${REFS[$(( ${#REFS[@]} - 1 ))]}
    say "every prefix is green on its own — the breakage needs the COMBINATION"
    say "BISECT=combination (eject any one of: ${REFS[*]})"
  else
    say "BISECT=$local_bad"
  fi
  exit 1
fi

# --------------------------------------------------------------------- landing
if [ "$LAND" = 1 ]; then
  [ -n "$ONLY" ] && die "--land refuses a narrowed selection: --only exists for self-tests, never for a landing"
  cur=$(git ls-remote origin main | awk '{print $1}')
  [ "$cur" = "$BASE_SHA" ] || die "main moved under us ($cur != $BASE_SHA) — re-run the train"
  for i in "${!REFS[@]}"; do
    pr=${PRS[$i]}
    head=${REFS[$i]}
    [ -n "$pr" ] || die "--land needs <pr>:<ref> entries, got a bare ref: $head"
    say "landing #$pr ($head)"
    gh pr merge "$pr" -R wzul/jomform --squash --author-email "$MERGE_AUTHOR_EMAIL" \
      --match-head-commit "$(git rev-parse "$head")" \
      || die "gh pr merge $pr failed — candidate tree is NOT on main; re-run the train"
  done
  git fetch -q origin main
  LANDED_TREE=$(git rev-parse "origin/main^{tree}")
  if [ "$LANDED_TREE" != "$CAND_TREE" ]; then
    say "MISMATCH: landed tree $LANDED_TREE != tested tree $CAND_TREE"
    say "the gate did not vouch for what is on main — re-run the train on the landed tree"
    exit 4
  fi
  say "LANDED tree $LANDED_TREE == tested candidate tree"
  # The identity is the other half of "what actually landed": the tree can match while the
  # author does not. Assert it here rather than trusting --author-email to have been honoured.
  assert_merge_authors "$BASE_SHA..origin/main" || exit 6
fi
exit 0
