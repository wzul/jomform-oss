#!/usr/bin/env bash
#
# deploy/biteproof_build_and_ship_gate.sh — prove the DEPLOY call site's reuse
# plumbing BITES.
#
# THE GUARD UNDER TEST is the per-tree record read in
# `deploy/build-and-ship.sh` § 5: before the pre-build gate joins the host gate
# pool it consults `scripts/preflight_reuse.sh` and, on `full`, passes
# `--reuse-record`; on `partial`, `--only <PREFLIGHT_RERUN> --reuse-record`; and
# otherwise gates in full. Before this plumbing existed the script hardcoded the
# bare `./scripts/preflight.sh`, so a full-green record for the shipped tree was
# IGNORED and every deploy re-ran the whole ~15 min gate. The record for
# v1.20.11's tree d609bd53 existed, was environment-matched, and was still not
# consumed: measured `test` 325s + `e2e` 183s + `selftest` 174s + `web` 75s … 910s.
#
# WHAT IT PROVES (each is a direction a reducible deploy gate would fail):
#
#   case A  A tree WITH a full-green record does NOT join the pool. Driven
#           against the REAL `scripts/preflight.sh`, the REAL consumer and a REAL
#           record: the runner takes its pre-pool exit, so no gate-pool slot file
#           is created, 0 gates are run, and the wall clock beyond the deploy's own
#           FIXED overhead is seconds — not the ~15 min the same bytes cost when
#           the record is ignored. (The window is the whole deploy, which includes
#           `deploy/release_selftest.sh`; that stage is load-sensitive and is
#           SUBTRACTED, from the duration the run itself reports. See the ceiling
#           comment below — an absolute ceiling on the whole window false-redded a
#           correct tree at host load 47.7, card t_87d48a3e.)
#   case B  A tree with NO record still runs the FULL gate. The argv handed to
#           `scripts/preflight.sh` carries NO reuse flag, and build-and-ship
#           labels the run as a full run (never as a reuse).
#   case C  A PARTIAL record re-runs ONLY the gates it could not vouch for:
#           `--only <PREFLIGHT_RERUN> --reuse-record`, with the remembered set
#           printed loudly and the run labelled REMEMBERED.
#   case D  A STALE-AND-REFUSED record (wrong environment / wrong tree — the
#           refusal `scripts/preflight.sh`'s own selftest checks 31-37 already
#           cover) still runs the full gate at the deploy call site. Those checks
#           are NOT duplicated here; this case drives the refusal through the
#           deploy call site, which is the only part they cannot see.
#
#   case F  THE RELEASE GATE'S CPU QUOTA, AS RECEIVED — not as written in the
#           source. `systemd-run` is stubbed on PATH to RECORD the argv it is
#           handed and then exec the wrapped command, so the assertion reads the
#           scope the gate actually gets: `CPUQuota=400%` by default. The docker
#           stub records the build argv, which must carry its OWN pinned
#           `--cpu-quota 300000`: a second run with GATE_CPU_QUOTA=500% proves the
#           build does not inherit it. This is the arm that fails on the pre-fix
#           bytes (`GATE_CPU_QUOTA` 300%, build inheriting it).
#   case G  THE INSTRUMENT'S OWN BITE for case F: the pre-fix quota pair is
#           substituted back into the sandbox copy, and the case F assertions must
#           then FAIL, naming CPUQuota=300% and the inherited build quota.
#
# `BITEPROOF_SHIP_FROM=<git-ref>` drives the sandbox cases against THAT revision's
# `deploy/build-and-ship.sh` instead of the working tree's, so the pre-fix bytes can
# be shown to red case F without editing or checking anything out. Case A needs the
# shipped tree and case G needs the fixed bytes, so both report a loud skip in that
# mode; every other case runs unchanged.
#
# WHAT IS REAL AND WHAT IS STUBBED, stated because it bounds the claim:
#   * `deploy/build-and-ship.sh` is the SHIPPED bytes, copied into that case's
#     sandbox and asserted sha-identical to the repo file. Cases B/C/D exercise
#     its ROUTING (which argv it builds and runs).
#   * Cases B/C/D run in a throwaway sandbox repo whose `scripts/preflight.sh`
#     and `scripts/preflight_reuse.sh` are stubs, so a "full gate" is a 0-second
#     call that records the argv it was handed. They prove the call site; they do
#     NOT re-prove the record format or the reusable-refusal rules — that is
#     `scripts/biteproof_preflight_reuse.sh`'s job, on the real scripts.
#   * Case A is END TO END on the real scripts: the real runner, the real
#     consumer, a real record written by the real scripts/gate_result.sh. It runs
#     in a THROWAWAY CLONE of this repo's HEAD, tagged there in its own ref
#     namespace, because deploy/build-and-ship.sh resolves the version through
#     deploy/require-release-tag.sh BEFORE it reads the record and refuses an
#     untagged HEAD — see the case A banner for why tagging the shared repo is
#     not an option. The tag goes on an EMPTY COMMIT in that clone, so the case
#     also runs on a HEAD that ALREADY carries a release tag — every lane sitting
#     on a released main (card t_b844d59b) — and the empty commit keeps HEAD's
#     tree, so the record still keys on the same content sha. It is bounded by
#     TIMEOUT seconds so a REGRESSED plumbing fails fast instead of silently
#     running the real ~15 min gate.
#   * Case A needs a CLEAN working tree (build-and-ship refuses a dirty one, by
#     design, and the clone carries HEAD's content rather than uncommitted
#     edits). A dirty tree is reported as a case-A setup refusal, not a bite.
#
# Run:  ./deploy/biteproof_build_and_ship_gate.sh
# Exit: 0 if every case BITES, 1 otherwise.
#
# Nothing under the repo is mutated: every mutation lives in $WORK (a private
# /tmp dir, removed on exit), the shipped build-and-ship.sh is sha-checked
# before and after, and NO tag or branch is created in the shared repository —
# case A tags its own clone, whose refs no other worktree can see. This harness
# therefore never takes a gate-pool slot and never writes to the host record
# store — PREFLIGHT_RESULT_DIR and PREFLIGHT_LOCK_FILE are pinned to $WORK for
# the whole run.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

BUILD_SHIP="deploy/build-and-ship.sh"
REUSE="scripts/preflight_reuse.sh"
RESULT="scripts/gate_result.sh"

# Ceiling for case A's REUSE EXIT, measured NET of the deploy's own fixed overhead.
#
# A pre-pool reuse exit is seconds; the full gate it replaces measured 910s.
# Anything between is a regression that must be reported, not waited out — but the
# timed window is the WHOLE deploy, and one stage in it is not the gate at all:
# `deploy/build-and-ship.sh` runs `deploy/release_selftest.sh` before it reads the
# record. That stage is hermetic, but it is ~21s of CPU nominal and it costs
# whatever the HOST gives it, so on a contended host it alone can fill any fixed
# ceiling — measured 112.78s at load 47.7 on 6 cores, while every semantic
# assertion of this harness still passed. The old absolute ceiling of 120s on the
# whole window therefore false-redded a CORRECT tree and named the reuse plumbing
# (card t_87d48a3e: 135s wall). The ceiling is now asserted on the wall clock
# MINUS that stage's duration as reported by the same run, so the subtraction is
# exact — same run, same host, same load — instead of a load estimate, and the
# assertion still bites: a gate that runs in full adds a measured 910s, i.e. two
# orders of magnitude over the ceiling.
# Overridable ONLY so the instrument can be shown to bite (see the assertion):
# raising it to get a green run is the false green that card removed.
CASE_A_MAX_SECONDS="${CASE_A_MAX_SECONDS:-120}"
# TIMEOUT is only a hang guard (the discriminating assertion is the ceiling above):
# it must sit ABOVE any plausible correct run on a busy host (measured 135s at load
# 47.7, so 600s is ~4x headroom) and BELOW a full gate (910s measured), so a
# plumbing that silently runs the real gate still fails instead of eating 15 min.
TIMEOUT=600
# The tag case A puts on its throwaway clone's HEAD. It IS the version the shipped
# script resolves (deploy/require-release-tag.sh reads the tag on HEAD), so the case
# passes NO version argument: an override may only RESTATE a tag, and that rule —
# including the contradiction and positional forms — is deploy/release_selftest.sh's
# subject, not this harness's.
CASE_A_TAG="v9.9.9"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

WORK="$(mktemp -d /tmp/biteproof_build_and_ship.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

for f in "$BUILD_SHIP" "$REUSE" "$RESULT"; do
  [ -f "$f" ] || { echo "FAIL: $f not found — cannot prove anything about the deploy gate" >&2; exit 2; }
done

WANT_SHIP="$(sha256sum "$BUILD_SHIP" | awk '{print $1}')"
# Pin the fingerprint's free component for the WHOLE run, so the record case A
# writes and the run that must consume it compute the same key whatever the
# caller's shell carries.
export PREFLIGHT_KEY_EXTRA=""
mkdir -p "$WORK/bin" "$WORK/out"

# ---- the docker stub -----------------------------------------------------------
# build-and-ship calls `command -v docker`, then build / image inspect / save. The
# stub proves a build DID (or did not) follow the gate without building an image,
# which is what keeps this harness fast enough to re-run.
cat > "$WORK/bin/docker" <<'DOCKER_STUB'
#!/usr/bin/env bash
set -uo pipefail
log="${DOCKER_STUB_LOG:-/dev/null}"
case "${1:-}" in
  build) printf 'docker build %s\n' "$*" >>"$log"; exit 0 ;;
  image) printf 'sha256:0000000000000000000000000000000000000000000000000000000000000000\n'; exit 0 ;;
  save)  printf 'docker save %s\n' "$*" >>"$log"; cat /dev/null; exit 0 ;;
  *)     printf 'docker %s\n' "$*" >>"$log"; exit 0 ;;
esac
DOCKER_STUB
chmod +x "$WORK/bin/docker"

# ---- the systemd-run stub ------------------------------------------------------
# `deploy/build-and-ship.sh` wraps the pre-build gate in
# `systemd-run --scope -q -p CPUQuota=<quota>`. Stubbing it makes the harness
# HERMETIC (no transient scope on the host, no dependence on systemd being present)
# and, more importantly, RECORDS the scope the gate was actually handed — which is
# the only way to assert a quota the script computes rather than one written on a
# source line. The stub then execs the wrapped command, so a stubbed "gate" still
# runs and its own exit status still decides the case.
cat > "$WORK/bin/systemd-run" <<'SDR_STUB'
#!/usr/bin/env bash
set -uo pipefail
log="${SDR_STUB_LOG:-/dev/null}"
{ printf 'systemd-run'; printf ' [%s]' "$@"; printf '\n'; } >>"$log"
# Drop systemd-run's own switches, then run what it was asked to run.
while [ $# -gt 0 ]; do
  case "$1" in
    --scope|--quiet|-q) shift ;;
    -p|--property) shift 2 ;;
    -p*) shift ;;
    --) shift; break ;;
    *) break ;;
  esac
done
exec "$@"
SDR_STUB
chmod +x "$WORK/bin/systemd-run"

# The bytes under test. Normally the SHIPPED file in this working tree; with
# BITEPROOF_SHIP_FROM set, that git revision's copy — so the pre-fix default can be
# shown to red case F without editing or checking anything out.
SHIP_FROM="${BITEPROOF_SHIP_FROM:-}"
ship_bytes() {
  if [ -n "$SHIP_FROM" ]; then
    git -C "$REPO_ROOT" show "$SHIP_FROM:$BUILD_SHIP" || return 1
  else
    cat "$REPO_ROOT/$BUILD_SHIP"
  fi
}
# A ref that does not name the script would silently yield an EMPTY sandbox copy,
# every case would then fail with an unrelated error and the run would be read as a
# bite. Refuse it up front instead.
if [ -n "$SHIP_FROM" ] && ! git -C "$REPO_ROOT" cat-file -e "$SHIP_FROM:$BUILD_SHIP" 2>/dev/null; then
  echo "FAIL: BITEPROOF_SHIP_FROM=$SHIP_FROM does not name a $BUILD_SHIP — nothing here could be evidence about those bytes" >&2
  exit 2
fi

# ---- the sandbox (cases B/C/D) -------------------------------------------------
# A throwaway git repo holding the SHIPPED build-and-ship.sh plus stub scripts,
# so "run the gate" is a recorded argv and nothing is built. LOCK_FILE and OUT_DIR
# live OUTSIDE the sandbox repo: the script takes its own lock before the
# clean-tree guard, and a lock file inside the repo would make every run dirty.
make_sandbox() {
  local d="$1"
  mkdir -p "$d/scripts" "$d/deploy" "$d/api"
  ship_bytes > "$d/$BUILD_SHIP"
  : > "$d/api/Dockerfile"
  # build-and-ship resolves its version through deploy/require-release-tag.sh, a CHILD process.
  # That rule (a deploy ships a TAGGED tree) is deploy/release_selftest.sh's subject, not this
  # harness's: here the version only has to be stable so the preflight call site can be reached
  # and measured. Stubbing it pins v9.9.9 with no tag, no docker and no gate. (The stub is inert
  # on a tree whose build-and-ship predates the release-tag rule, so this case works on both.)
  cat > "$d/deploy/require-release-tag.sh" <<'RRT_STUB'
#!/usr/bin/env bash
# sandbox stub: this harness measures the preflight CALL SITE, not the tag rule.
printf 'v9.9.9\n'
RRT_STUB
  chmod +x "$d/deploy/require-release-tag.sh"
  cat > "$d/scripts/gate_result.sh" <<'GR_STUB'
gr_short_sha() { printf '%.7s\n' "$1"; }
GR_STUB
  cat > "$d/scripts/preflight_reuse.sh" <<'PR_STUB'
# sandbox consumer stub: returns the decision named by STUB_REUSE_DECISION,
# mirroring the real consumer's contract (0 = usable, 1 = gate normally).
preflight_reuse_decision() {
  PREFLIGHT_TREE="stubtree1234567890"
  PREFLIGHT_RECORD="${STUB_RECORD_PATH:-<none>}"
  PREFLIGHT_DECISION="${STUB_REUSE_DECISION:-none}"
  PREFLIGHT_SKIP="${STUB_SKIP:-}"
  PREFLIGHT_RERUN="${STUB_RERUN:-}"
  PREFLIGHT_REMEMBERED_SECONDS="${STUB_SECONDS:-0}"
  if [ "$PREFLIGHT_DECISION" = "none" ]; then
    PREFLIGHT_EVIDENCE="no reusable record for tree stubtre — gating: ${STUB_REASON:-no record}"
    return 1
  fi
  PREFLIGHT_EVIDENCE="STUB EVIDENCE: $PREFLIGHT_DECISION for tree stubtre — ${STUB_REASON:-}"
  return 0
}
PR_STUB
  cat > "$d/scripts/preflight.sh" <<'PF_STUB'
#!/usr/bin/env bash
set -uo pipefail
if [ "${1:-}" = "--list" ]; then printf 'build\nvet\n'; exit 0; fi
printf 'STUB preflight.sh argv:'; printf ' [%s]' "$@"; printf '\n'
# One argument per line: the argv is asserted exactly, and a value that was
# re-split or word-split shows up as extra lines. `$0` is deliberately NOT
# recorded — bash rewrites it to an ABSOLUTE path for a `./`-relative script, so
# it is not caller-controlled and would make an exact assertion impossible.
if [ -n "${PREFLIGHT_ARGV_LOG:-}" ]; then
  if [ "$#" -eq 0 ]; then
    printf '<no-args>\n' > "$PREFLIGHT_ARGV_LOG"
  else
    for _a in "$@"; do printf '%s\n' "$_a"; done > "$PREFLIGHT_ARGV_LOG"
  fi
fi
case " $* " in
  *" --reuse-record "*) printf '  ==> REUSED (not re-run, recorded pass on THESE bytes): stub-remembered\n'
                        printf '  REUSE: stub-remembered (REMEMBERED from a recorded pass, NOT re-run this time)\n' ;;
esac
printf 'preflight summary: STUB 1 passed, 0 failed, 0 skipped in 0s\n'
exit "${STUB_PREFLIGHT_RC:-0}"
PF_STUB
  chmod +x "$d/scripts/preflight.sh"
  git -C "$d" init -q
  git -C "$d" add -A
  git -C "$d" -c user.email=bite@example.invalid -c user.name=biteproof commit -qm "sandbox"
}

# err_of <file> — the reason a setup function recorded, on ONE line, or a refusal
# to stay silent when it recorded none. Setup failures used to be reported as a
# guess about which step broke; a reason file replaces the guess.
err_of() {
  local f="$1" out
  out="$(tr '\n' ' ' 2>/dev/null < "$f" | sed 's/[[:space:]]*$//')"
  printf '%s' "${out:-no reason was recorded — the setup refused without saying why}"
}

# make_tagged_tree <dir> [<errfile>] [<pretag>] — a THROWAWAY CLONE of this repo
# at HEAD, with CASE_A_TAG as the ONLY tag on its HEAD. Prints <dir> on success.
#
# Why a clone and not a tag here: `git tag` in this worktree writes to the SHARED
# repository's refs/tags, which every other worktree of this repo can see — a
# lane midway through cut-release, or any `git describe`, would read a v9.9.9
# that no human released. A clone has its own .git, so its tags are invisible
# outside $WORK, while --shared keeps it cheap (measured ~1s, 33 MB of
# checkout, objects borrowed read-only).
#
# It also has to be the tree the RECORD is written and read in:
# scripts/gate_result.sh keys a record on the CONTENT tree sha (path-independent)
# plus an environment fingerprint whose webdeps component is
# web/node_modules/.package-lock.json. The clone carries only committed content —
# no node_modules (gitignored) — so writer and reader must both live here, or they
# would compute two different fingerprints for "the same" tree.
#
# WHY IT MAKES AN EMPTY COMMIT BEFORE TAGGING (card t_b844d59b). A lane sitting
# on a released main has a HEAD that ALREADY carries a release tag — origin/main
# was v1.41.0 when this was written — and `git clone` copies that tag in. Tagging
# HEAD as well leaves TWO tags on ONE commit, and `git describe --tags
# --exact-match` answers the OTHER one: measured in a throwaway repo, a HEAD
# carrying v1.41.0 plus this harness's v9.9.9 describes as v1.41.0, so BOTH this
# function's precondition AND deploy/require-release-tag.sh (which the shipped
# build-and-ship runs BEFORE it reads a record) would read a version this harness
# never chose. `git commit --allow-empty` moves the clone's HEAD onto an untagged
# commit that KEEPS HEAD's TREE: measured, both trees were df55a7dc, so the tag is
# unambiguous AND the content tree sha — the key scripts/gate_result.sh writes and
# reads — is unchanged.
#
# WHY THE CALLER GETS A REASON, NOT A BARE 1 (card t_b844d59b). This used to
# `return 1` from four different failures (clone, tag, describe, dirty) and the
# caller reported every one of them as "git clone --shared / git tag failed" — a
# message naming two things that had both worked, on the very shape (a released
# main) that is common. The reason goes to <errfile> because the caller reads the
# directory through a command substitution, i.e. in a SUBSHELL, where a variable
# set in here would not survive.
#
# <pretag> is for the harness's OWN tag-independence check: it puts a decoy tag on
# the clone's HEAD BEFORE the empty commit, reproducing a source repo whose HEAD is
# already released, without touching this repo's shared refs. The name is one this
# harness OWNS, and a decoy that already exists is a REFUSAL, not a skip: the first
# version of that check used a real release name (v1.41.0), which this repo already
# carries at another commit, so `git tag` was skipped, HEAD stayed untagged and the
# check passed vacuously — measured, the mutation below was invisible to it.
make_tagged_tree() {
  local d="$1" err="${2:-$1.err}" pretag="${3:-}" got
  : >"$err" 2>/dev/null || err=/dev/null
  if ! git clone --quiet --shared "$REPO_ROOT" "$d" >/dev/null 2>&1; then
    printf 'git clone --shared %s %s failed (nothing was cloned)\n' "$REPO_ROOT" "$d" >>"$err"
    return 1
  fi
  if [ -n "$pretag" ]; then
    if git -C "$d" rev-parse --verify -q "refs/tags/$pretag" >/dev/null 2>&1; then
      printf 'the decoy tag %s already exists in %s — it must be a name this harness owns\n' "$pretag" "$d" >>"$err"
      return 1
    fi
    git -C "$d" tag "$pretag" >/dev/null 2>&1 || {
      printf 'could not place the decoy tag %s in %s\n' "$pretag" "$d" >>"$err"; return 1; }
  fi
  if ! git -C "$d" -c user.email=bite@example.invalid -c user.name=biteproof \
        commit -q --allow-empty -m "biteproof case A: untagged HEAD so $CASE_A_TAG is unambiguous" >/dev/null 2>&1; then
    printf 'git commit --allow-empty in %s failed (HEAD cannot be moved off an existing tag)\n' "$d" >>"$err"
    return 1
  fi
  if ! git -C "$d" tag "$CASE_A_TAG" >/dev/null 2>&1; then
    printf 'git tag %s in %s failed\n' "$CASE_A_TAG" "$d" >>"$err"
    return 1
  fi
  # The tag must be ON HEAD, not merely in the clone, and it must be the ONLY one
  # there: require-release-tag uses `git describe --tags --exact-match HEAD`, and a
  # second tag on that commit wins over it (measured, see above).
  got="$(git -C "$d" describe --tags --exact-match HEAD 2>&1)"
  if [ "$got" != "$CASE_A_TAG" ]; then
    printf 'the clone does not describe as %s: describe --tags --exact-match HEAD answered [%s], tags at HEAD [%s]\n' \
      "$CASE_A_TAG" "${got:-none}" "$(git -C "$d" tag --points-at HEAD 2>/dev/null | paste -sd, -)" >>"$err"
    return 1
  fi
  if [ -n "$(git -C "$d" status --porcelain)" ]; then
    printf 'the clone %s has a DIRTY working tree: %s\n' "$d" \
      "$(git -C "$d" status --porcelain | head -n 3 | paste -sd';' -)" >>"$err"
    return 1
  fi
  printf '%s\n' "$d"
}

# run_sandbox <dir> <tag> <logfile> <argvlog> [NAME=VALUE ...] — run the shipped
# script in the sandbox; the trailing assignments are extra environment for that run
# (case F uses one to prove the build quota does not inherit the gate's).
run_sandbox() {
  local d="$1" tag="$2" log="$3" argvlog="$4"; shift 4
  local rc=0
  ( cd "$d" && env PATH="$WORK/bin:$PATH" \
      LOCK_FILE="$WORK/$tag.lock" OUT_DIR="$WORK/out" \
      DOCKER_STUB_LOG="$WORK/$tag.docker.log" \
      SDR_STUB_LOG="$WORK/$tag.systemd.log" \
      PREFLIGHT_ARGV_LOG="$argvlog" \
      "$@" bash "$d/$BUILD_SHIP" v9.9.9 >"$log" 2>&1 ) || rc=$?
  return "$rc"
}

argv_of() { [ -r "$1" ] && paste -sd' ' "$1" || printf '<no argv recorded>'; }
show() { printf '        log: %s\n' "$1"; sed 's/^/          | /' "$1" | head -n 24; }

# The quota the GATE was actually launched under: the `-p CPUQuota=<value>` the
# systemd-run stub recorded, NOT the line in the source.
sdr_quota() {
  [ -r "$1" ] || return 0
  sed -n 's/.*\[CPUQuota=\([^]]*\)\].*/\1/p' "$1" | head -n1
}
# The quota the BUILD was actually given: the value after `--cpu-quota` in the docker
# stub's recorded `docker build` argv (docker wants microseconds per 100 ms period).
docker_build_quota() {
  [ -r "$1" ] || return 0
  sed -n 's/^docker build //p' "$1" | tr ' ' '\n' \
    | awk '/^--cpu-quota$/ { getline; print; exit }'
}

echo "biteproof: the deploy call site's per-tree record reuse"
echo "  repo: $(pwd)"
echo "  head: $(git rev-parse --short HEAD)"
echo "  ship: $BUILD_SHIP (sha256 ${WANT_SHIP:0:12})"
[ -z "$SHIP_FROM" ] || echo "  bytes under test: $SHIP_FROM:$BUILD_SHIP (BITEPROOF_SHIP_FROM) — case A needs the shipped tree and case G needs the fixed bytes, so both skip"
echo "---"

# ============================================================================
# case A — a full-green record must NOT join the pool (REAL runner, end to end)
#
# THE TREE IS A THROWAWAY, TAGGED CLONE OF HEAD — not this worktree. Two facts
# force that, and both were measured, not assumed:
#
#   1. build-and-ship.sh resolves the version through
#      deploy/require-release-tag.sh BEFORE it reads the record, so on an
#      UNTAGGED HEAD it exits 4/5 without ever reaching the preflight call site
#      this case exists to measure. That is the release rule working; the harness
#      hands it a TAGGED tree and passes no version, so the tag itself is what
#      the release stamps.
#   2. `git tag` HERE would write to the SHARED repository's refs/tags, which
#      every worktree of this repo can see: a lane midway through cut-release,
#      or any `git describe`, would read a v9.9.9 nobody released. A clone owns
#      its refs, so the tag lives and dies inside $WORK.
#
# The tag goes on an EMPTY COMMIT in that clone, so a source HEAD that already
# carries a release tag — every lane on a released main — does not make the tag
# ambiguous (make_tagged_tree says why, with the measurement), and the
# tag-independence check below re-proves it on every run instead of trusting it.
#
# The record is written AND read in that clone. scripts/gate_result.sh keys a
# record on the CONTENT tree sha (path-independent) plus an environment
# fingerprint that includes webdeps (web/node_modules/.package-lock.json), and a
# clone carries only committed content — no node_modules — so a writer in one
# tree and a reader in another would compute two fingerprints for "the same"
# tree. The two trees' content shas are compared below, so "the clone is the
# bytes this harness reports" is measured rather than assumed.
# ============================================================================
echo "== case A: a full-green record does not join the gate pool (real scripts, timing) =="
if [ -n "$SHIP_FROM" ]; then
  printf '  skip case A — BITEPROOF_SHIP_FROM=%s drives this run against another revision'"'"'s bytes; case A measures the SHIPPED tree and writes a real record\n' "$SHIP_FROM"
else
CA="$WORK/caseA"; mkdir -p "$CA/store"
CLEAN=1
[ -z "$(git status --porcelain)" ] || CLEAN=0
if [ "$CLEAN" != "1" ]; then
  bad "case A — the working tree is DIRTY: this case measures a throwaway clone of HEAD, so uncommitted edits would not be the bytes it gates. Commit or stash, then re-run. (This is a setup refusal, NOT a bite.)"
elif git -C "$REPO_ROOT" rev-parse --verify -q "refs/tags/$CASE_A_TAG" >/dev/null 2>&1; then
  bad "case A — the SHARED repository already carries the tag $CASE_A_TAG, so this case could not tell its own throwaway tag from a pre-existing release. Delete it (git -C $REPO_ROOT tag -d $CASE_A_TAG) and re-run. (setup refusal, NOT a bite.)"
elif ! TREE="$(make_tagged_tree "$CA/tree" "$CA/setup.err")"; then
  bad "case A — could not prepare the throwaway clone at $CA/tree: $(err_of "$CA/setup.err"). A release-tagged build-and-ship would then refuse before the call site, so nothing below would be evidence. (setup refusal, NOT a bite.)"
else
  # A REAL record for the CLONE's tree, written by the REAL gate_result.sh, under
  # THIS environment (gr_flush keys it on gr_key, i.e. the caller's own
  # fingerprint). PREFLIGHT_RESULT_DIR is EXPORTED so the writer, the reader below
  # and the preflight child all key on the same private store, never the host's.
  REC_DIR="$CA/store"
  export PREFLIGHT_RESULT_DIR="$REC_DIR"
  export PREFLIGHT_REUSE_REPO="$TREE"
  # shellcheck source=scripts/gate_result.sh
  . "$TREE/$RESULT"
  REAL_TREE="$(gr_tree_sha "$REPO_ROOT")"
  GR_TREE="$(gr_tree_sha "$TREE")"
  # --- the setup must not depend on THIS worktree's HEAD being UNTAGGED ----------
  # Card t_b844d59b: `git clone` copies the source's tags, so on a lane sitting on
  # a released main the clone's HEAD ALREADY carries a release tag — and a second
  # tag on that commit is never what `git describe --tags --exact-match` answers
  # (measured: v1.41.0, not v9.9.9). That used to make case A refuse on exactly the
  # HEADs every lane has right after a release, reported as "git clone failed".
  # Reproduced HERE on every run rather than trusted: a DECOY release tag — a name
  # this harness owns, so it cannot accidentally pre-exist — is put on a throwaway
  # clone's HEAD before the empty commit, and the prepared HEAD must still describe
  # as CASE_A_TAG alone AND keep the content tree sha the record is keyed on (an
  # empty commit keeps HEAD's tree). The commit the case moved OFF must still carry
  # the decoy, which is what proves the reproduction happened instead of a check
  # that silently tagged an untagged HEAD.
  PRE_RELEASE_TAG="v0.0.0-bite-decoy"
  if [ -z "$REAL_TREE" ]; then
    bad "case A — could not compute this worktree's content tree sha, so the tag-independence check has nothing to compare against"
  elif DECOY_TREE="$(make_tagged_tree "$CA/decoy" "$CA/decoy.err" "$PRE_RELEASE_TAG")"; then
    DECOY_DESCRIBE="$(git -C "$DECOY_TREE" describe --tags --exact-match HEAD 2>/dev/null)"
    DECOY_TAGS="$(git -C "$DECOY_TREE" tag --points-at HEAD 2>/dev/null | paste -sd, -)"
    DECOY_PARENT_TAGS="$(git -C "$DECOY_TREE" tag --points-at HEAD~1 2>/dev/null | paste -sd, -)"
    DECOY_SHA="$(gr_tree_sha "$DECOY_TREE")"
    case ",$DECOY_PARENT_TAGS," in
      *",$PRE_RELEASE_TAG,"*) DECOY_ON_PARENT=1 ;;
      *) DECOY_ON_PARENT=0 ;;
    esac
    if [ "$DECOY_DESCRIBE" = "$CASE_A_TAG" ] && [ "$DECOY_TAGS" = "$CASE_A_TAG" ] \
       && [ "$DECOY_ON_PARENT" = "1" ] \
       && [ -n "$DECOY_SHA" ] && [ "$DECOY_SHA" = "$REAL_TREE" ]; then
      ok "case A — TAG-INDEPENDENT SETUP: a throwaway clone whose HEAD carried a release tag ($PRE_RELEASE_TAG, decoy; tags at the commit the case moved off: $DECOY_PARENT_TAGS) describes as $CASE_A_TAG ALONE after the empty commit, and its content tree $(gr_short_sha "$DECOY_SHA") is still this worktree's — so case A runs on a lane sitting on a released main instead of refusing there"
    else
      # Name the LEG that failed: the tag legs and the bytes leg fail for different
      # reasons, and a single message for both sends the next lane to the wrong one.
      _legs=""
      [ "$DECOY_DESCRIBE" = "$CASE_A_TAG" ] || _legs="$_legs; describe --tags --exact-match answered [${DECOY_DESCRIBE:-none}] instead of $CASE_A_TAG (a second tag on one commit never wins it — the card t_b844d59b defect, back again)"
      [ "$DECOY_TAGS" = "$CASE_A_TAG" ] || _legs="$_legs; tags at the prepared HEAD were [${DECOY_TAGS:-none}], not $CASE_A_TAG alone"
      [ "$DECOY_ON_PARENT" = "1" ] || _legs="$_legs; the commit the case moved off carries [${DECOY_PARENT_TAGS:-none}], not the decoy $PRE_RELEASE_TAG — so the pre-tagged shape was never reproduced and this check would be vacuous"
      if [ -z "$DECOY_SHA" ] || [ "$DECOY_SHA" != "$REAL_TREE" ]; then
        _legs="$_legs; its content tree is [$(gr_short_sha "${DECOY_SHA:-}")] vs this worktree's [$(gr_short_sha "$REAL_TREE")], so the record would key on OTHER bytes (an empty commit must keep HEAD's tree)"
      fi
      bad "case A — the TAG-INDEPENDENCE CHECK failed$_legs. A case-A setup that cannot tag an already-released HEAD, or that changes its bytes, refuses lanes on a released main or measures the wrong tree. (It is a harness defect, NOT a bite.)"
    fi
  else
    bad "case A — could not prepare the decoy-tagged clone that proves the setup survives a source HEAD already carrying a release tag: $(err_of "$CA/decoy.err")"
  fi
  # MEASURED, not asserted: (a) the tree under test is a CLONE, not this worktree —
  # a tag on this worktree would be shared with every other lane — and (b) that
  # clone's HEAD really carries the tag, because require-release-tag uses
  # `git describe --tags --exact-match HEAD`.
  TREE_GITDIR="$(git -C "$TREE" rev-parse --absolute-git-dir 2>/dev/null)"
  REPO_GITDIR="$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir 2>/dev/null)"
  TREE_TAG="$(git -C "$TREE" describe --tags --exact-match HEAD 2>/dev/null)"
  if [ -z "$REAL_TREE" ] || [ -z "$GR_TREE" ]; then
    bad "case A — could not compute a content tree sha (harness [$REAL_TREE], throwaway clone [$GR_TREE]), so nothing below would be evidence about either"
  elif [ "$REAL_TREE" != "$GR_TREE" ]; then
    bad "case A — the throwaway clone does NOT carry this harness's bytes (content tree $(gr_short_sha "$GR_TREE") vs $(gr_short_sha "$REAL_TREE")): the case would be evidence about a tree the repo does not hold. (setup refusal, NOT a bite.)"
  elif [ -z "$TREE_GITDIR" ] || [ "$TREE_GITDIR" = "$REPO_GITDIR" ]; then
    bad "case A — the tree under test is not a separate clone (git dir [${TREE_GITDIR:-none}] vs this worktree's [$REPO_GITDIR]): tagging it would either hit the shared refs or no refs at all. (setup refusal, NOT a bite.)"
  elif [ "$TREE_TAG" != "$CASE_A_TAG" ]; then
    bad "case A — the throwaway clone's HEAD carries no $CASE_A_TAG (describe --tags --exact-match = [${TREE_TAG:-none}]), so a release-tagged build-and-ship refuses before the call site. (setup refusal, NOT a bite.)"
  else
    GR_TREE_SHORT="$(gr_short_sha "$GR_TREE")"
    GR_REPO="$TREE"
    GR_COMMIT="$(git -C "$TREE" rev-parse --short HEAD)"
    GR_BRANCH="$(git -C "$TREE" rev-parse --abbrev-ref HEAD)"
    GR_PID=$$
    GR_GATE_NAMES=()
    while IFS= read -r g; do [ -n "$g" ] && GR_GATE_NAMES+=("$g"); done < <(bash "$TREE/scripts/preflight.sh" --list)
    GR_PASSED="${#GR_GATE_NAMES[@]}"; GR_FAILED=0; GR_SKIPPED=0
    # A FINISHED run: since card t_d49a99f1 the reader refuses a record whose run did
    # not reach the end of its inventory (`complete: false`), and this fixture models
    # exactly that — a full-inventory run that finished.
    GR_COMPLETE=1
    _now="$(gr_now)"
    for g in "${GR_GATE_NAMES[@]}"; do
      GR_STATUS[$g]="pass"; GR_SECONDS[$g]=45; GR_AT[$g]="$_now"; GR_REUSED[$g]=false
    done
    if gr_flush 910; then
      REC_FILE="$(gr_file "$GR_TREE")"
      ok "case A — throwaway clone $TREE (git dir $TREE_GITDIR, distinct from this worktree's $REPO_GITDIR): HEAD $(git -C "$TREE" rev-parse --short HEAD) is an EMPTY commit on top of this worktree's HEAD $(git -C "$REPO_ROOT" rev-parse --short HEAD) (which itself carries [$(git -C "$REPO_ROOT" tag --points-at HEAD | paste -sd, - | sed 's/^$/no tag/')]), so it is on tag $TREE_TAG alone; content tree $GR_TREE_SHORT == the harness tree — wrote a REAL full-green record (${#GR_GATE_NAMES[@]} gates) at $REC_FILE"
      # The REAL consumer must call it usable BEFORE the run, or the case would be
      # measuring a refusal and calling it a bite.
      # shellcheck source=scripts/preflight_reuse.sh
      . "$TREE/$REUSE"
      preflight_reuse_decision >/dev/null 2>&1
      if [ "${PREFLIGHT_DECISION:-}" = "full" ]; then
        ok "case A — the REAL consumer reads it as usable: full (${PREFLIGHT_REMEMBERED_SECONDS}s remembered) — so anything below is the call site, not the record"
      else
        bad "case A — the record is not usable by the real consumer (decision=[${PREFLIGHT_DECISION:-}]): ${PREFLIGHT_EVIDENCE:-} — the case would prove nothing"
      fi
      # The gate must not take any slot in the pool: a whole-inventory reuse exits
      # BEFORE the pool, so no slot file may be created (the slot files are
      # `<base>.slot<N>`, derived by preflight.sh — see its SLOT POOL block).
      A_LOCK="$CA/host-gate.lock"
      t0="$(date +%s)"
      CA_RC=0
      ( cd "$TREE" && \
        PREFLIGHT_RESULT_DIR="$REC_DIR" PREFLIGHT_REUSE_REPO="$TREE" \
        PATH="$WORK/bin:$PATH" LOCK_FILE="$CA/build.lock" OUT_DIR="$CA/out" \
        DOCKER_STUB_LOG="$CA/docker.log" \
        PREFLIGHT_LOCK_FILE="$A_LOCK" \
        timeout "$TIMEOUT" bash "$TREE/$BUILD_SHIP" >"$CA/out.log" 2>&1 ) || CA_RC=$?
      t1="$(date +%s)"; a_secs=$((t1 - t0))
      if [ "$CA_RC" -eq 124 ]; then
        bad "case A — build-and-ship did NOT exit within ${TIMEOUT}s: with a full-green record it must exit in seconds, so the reuse plumbing is not being consulted"
      elif [ "$CA_RC" -ne 0 ]; then
        bad "case A — build-and-ship exited $CA_RC with a full-green record present (a reused gate must not red the deploy); tree $TREE is tagged $(git -C "$TREE" describe --tags --exact-match HEAD 2>/dev/null || echo NONE), so this is NOT the untagged-tree refusal"; show "$CA/out.log"
      elif grep -q 'took NO slot and ran NO gate' "$CA/out.log" \
           && grep -q 'summary: 0 passed' "$CA/out.log" \
           && grep -q 'WHOLE-INVENTORY REUSE' "$CA/out.log"; then
        ok "case A — the runner exited BEFORE the slot pool (0 gates run): '$(grep -m1 'took NO slot' "$CA/out.log" | sed 's/^ *//')'"
      else
        bad "case A — with a full-green record present the gate did NOT take the pre-pool reuse exit"; show "$CA/out.log"
      fi
      if [ ! -e "$A_LOCK" ] && [ -z "$(ls -A "$A_LOCK".slot* 2>/dev/null)" ]; then
        ok "case A — NO gate-pool slot was taken (neither $A_LOCK nor ${A_LOCK}.slot<N> exists): this deploy never joined the pool"
      else
        bad "case A — a gate-pool slot EXISTS ($A_LOCK / $(ls "$A_LOCK".slot* 2>/dev/null | tr '\n' ' ')): the run joined the pool despite a full-green record"
      fi
      if [ -r "$CA/docker.log" ] && grep -q '^docker build' "$CA/docker.log"; then
        ok "case A — the image build DID proceed after the reused gate (the gate did not block the release)"
      else
        bad "case A — no image build followed the reused gate, so the reuse did not behave as a green gate"; show "$CA/out.log"
      fi
      # The tag, not an override, must be what the release stamped: the case passes
      # no VERSION_OVERRIDE, so this line is evidence the release-tag step ran on a
      # TAGGED tree instead of being bypassed.
      if grep -q "==> stamps: version=$CASE_A_TAG " "$CA/out.log"; then
        ok "case A — the deploy stamped version $CASE_A_TAG from the clone's TAG (no VERSION_OVERRIDE passed, so the release-tag step was exercised, not bypassed)"
      else
        bad "case A — the deploy did not stamp version $CASE_A_TAG from the tag, so the release-tag resolution never reached the build"; show "$CA/out.log"
      fi
      # ---- WHAT THE CLOCK MAY ASSERT, AND WHAT IT MAY NOT -------------------
      # The window above is the WHOLE deploy, and one stage in it is NOT the gate:
      # build-and-ship runs `deploy/release_selftest.sh` BEFORE it reads the record,
      # and a reuse cannot remove that stage. It is CPU-bound, so its cost is
      # whatever the HOST gives it — measured 112.78s at load 47.7 on 6 cores
      # against a nominal ~21s, i.e. it alone can fill any fixed ceiling, while
      # every semantic assertion above stayed green. Asserting the absolute wall
      # clock therefore false-redded a CORRECT tree and blamed the reuse plumbing
      # (card t_87d48a3e: 135s wall, this exact FAIL line). So the ceiling is
      # asserted on the time BEYOND that stage, taking the duration from the RUN
      # ITSELF (`release rules selftest: passed (N checks in Ms)`, build-and-ship
      # §3): same run, same host, same load, so the subtraction is exact instead of
      # a load estimate, and a full gate regression still adds a measured 910s.
      #
      # FAIL-SAFE DIRECTIONS, stated: (a) the stage line present but WITHOUT a
      # duration is a SETUP REFUSAL, not a quiet zero — a zero would silently turn
      # this back into the absolute ceiling and re-introduce the false red; (b) the
      # stage line absent entirely means the stage did not run in this tree
      # (SKIP_RELEASE_SELFTEST=1 or no exec bit), so the whole wall clock is net.
      SELFTEST_SECS=""
      A_NET=""
      if grep -q 'release rules selftest: passed' "$CA/out.log" 2>/dev/null; then
        SELFTEST_SECS="$(sed -n 's/.*release rules selftest: passed (.* in \([0-9]\{1,\}\)s)$/\1/p' "$CA/out.log" | tail -n1)"
        if [ -n "$SELFTEST_SECS" ]; then
          # `10#` because bash reads a leading-zero constant as OCTAL, and a
          # leading-zero duration (from a future format change, or a hand-edited
          # log) would make this expansion FAIL — leaving A_NET unset and the
          # assertion below silently skipped, which is a false green. With an
          # explicit base the arithmetic cannot fail on a digit string.
          A_NET=$(( 10#$a_secs - 10#$SELFTEST_SECS ))
          [ "$A_NET" -ge 0 ] || A_NET=0
        else
          bad "case A — the deploy reported its release-selftest stage WITHOUT a duration, so the reuse exit cannot be timed net of it (that stage is what host contention inflates, and it is the whole reason the absolute ceiling false-redded). Restore the duration to build-and-ship.sh §3. (setup refusal, NOT a bite.)"
        fi
      else
        SELFTEST_SECS=0
        A_NET="$a_secs"
      fi
      if [ -n "$A_NET" ]; then
        if [ "$A_NET" -lt "$CASE_A_MAX_SECONDS" ]; then
          ok "case A — REUSE EXIT ${A_NET}s net of the deploy's own fixed overhead (wall clock ${a_secs}s − release-selftest ${SELFTEST_SECS}s, measured BY THAT RUN, so host contention cancels out; the same bytes re-gated in full cost a measured 910s on this host): the record removed the gate, and the clock that is left is the deploy's own work"
        else
          bad "case A — REUSE EXIT ${A_NET}s net of fixed overhead is NOT what a pre-pool reuse exit takes (ceiling ${CASE_A_MAX_SECONDS}s; wall clock ${a_secs}s − release-selftest ${SELFTEST_SECS}s) — the record did not remove the gate"
        fi
      fi
    else
      bad "case A — could not write the REAL record for tree $GR_TREE_SHORT into $REC_DIR, so this case cannot run"
    fi
  fi
  # The private store and repo pin are for case A only; do not leak them into the
  # sandbox cases (their stubs ignore them, but a leak would be invisible).
  unset PREFLIGHT_RESULT_DIR PREFLIGHT_REUSE_REPO
fi
fi

# ============================================================================
# case B — NO record: the FULL gate runs, and is labelled a full run
# ============================================================================
echo "== case B: no record -> the full gate runs (no reuse flag), labelled a full run =="
SB="$WORK/sandboxB"; make_sandbox "$SB"
B_RC=0; run_sandbox "$SB" caseB "$WORK/caseB.log" "$WORK/caseB.argv" || B_RC=$?
if [ "$B_RC" -ne 0 ]; then
  bad "case B — the sandbox run exited $B_RC (the gate stub is green; a non-zero exit is the call site failing)"; show "$WORK/caseB.log"
elif [ "$(argv_of "$WORK/caseB.argv")" != "<no-args>" ]; then
  bad "case B — with NO record the gate was invoked with argv [$(argv_of "$WORK/caseB.argv")], so a reuse flag was passed for a tree with nothing recorded"
elif grep -q 'all scripts/preflight.sh gates passed' "$WORK/caseB.log" \
     && ! grep -q 'WHOLE-INVENTORY REUSE\|PARTIAL RECORD' "$WORK/caseB.log"; then
  ok "case B — NO record: the gate ran with NO argv (no reuse flag) and the run is labelled a FULL run, never a reuse"
else
  bad "case B — with NO record the run was labelled as a reuse (or not labelled a full run)"; show "$WORK/caseB.log"
fi

# ============================================================================
# case C — a PARTIAL record re-runs ONLY what it could not vouch for
# ============================================================================
echo "== case C: a partial record re-runs only the unvouched gates (--only <rerun> --reuse-record) =="
make_sandbox "$WORK/sandboxC"
export STUB_REUSE_DECISION=partial STUB_SKIP="test,web,e2e" STUB_RERUN="build,vet" STUB_SECONDS=420
C_RC=0; run_sandbox "$WORK/sandboxC" caseC "$WORK/caseC.log" "$WORK/caseC.argv" || C_RC=$?
unset STUB_REUSE_DECISION STUB_SKIP STUB_RERUN STUB_SECONDS
if [ "$C_RC" -ne 0 ]; then
  bad "case C — the sandbox run exited $C_RC (the gate stub is green)"; show "$WORK/caseC.log"
elif [ "$(argv_of "$WORK/caseC.argv")" != "--only build,vet --reuse-record" ]; then
  bad "case C — a PARTIAL record was NOT routed to the documented shape: argv [$(argv_of "$WORK/caseC.argv")], want [--only build,vet --reuse-record]"
elif ! grep -q 'RE-RUNNING ONLY: build,vet' "$WORK/caseC.log"; then
  bad "case C — the run does not say which gates it is re-running"; show "$WORK/caseC.log"
elif ! grep -q 'remembered (not re-run): test,web,e2e' "$WORK/caseC.log"; then
  bad "case C — the run does not name the REMEMBERED set, so a skipped gate would look identical to a run one"; show "$WORK/caseC.log"
elif ! grep -q 'REMEMBERED, and were NOT re-run' "$WORK/caseC.log"; then
  bad "case C — the closing line does not say the remembered gates were not re-run"; show "$WORK/caseC.log"
else
  ok "case C — PARTIAL: argv [./scripts/preflight.sh --only build,vet --reuse-record]; remembered [test,web,e2e] named loudly; closing says REMEMBERED, not 'all gates passed'"
fi

# ============================================================================
# case D — a stale-and-refused record still gates in full, at the call site
# ============================================================================
echo "== case D: a STALE/REFUSED record still gates in full through the deploy call site =="
make_sandbox "$WORK/sandboxD"
export STUB_REUSE_DECISION=none \
  STUB_REASON="the environment changed since that record (recorded key [a1] != current [b2])"
D_RC=0; run_sandbox "$WORK/sandboxD" caseD "$WORK/caseD.log" "$WORK/caseD.argv" || D_RC=$?
unset STUB_REUSE_DECISION STUB_REASON
if [ "$D_RC" -ne 0 ]; then
  bad "case D — the sandbox run exited $D_RC (the gate stub is green)"; show "$WORK/caseD.log"
elif [ "$(argv_of "$WORK/caseD.argv")" != "<no-args>" ]; then
  bad "case D — a REFUSED record still narrowed the run: argv [$(argv_of "$WORK/caseD.argv")] carries a reuse flag"
elif ! grep -q 'the environment changed since that record' "$WORK/caseD.log"; then
  bad "case D — the refusal reason was not surfaced (the lane cannot tell WHY it gated)"; show "$WORK/caseD.log"
elif ! grep -q 'all scripts/preflight.sh gates passed' "$WORK/caseD.log"; then
  bad "case D — the refused-record run is not labelled a FULL run"; show "$WORK/caseD.log"
else
  ok "case D — a record refused for a changed environment is surfaced and the deploy gates IN FULL: no argv (no reuse flag)"
fi

# ============================================================================
# case E — the instrument's OWN bite: the pre-fix routing (the record read but
# IGNORED at the call site) must violate the shape case C asserts. Without this
# the four cases above could pass on a script that never consumed a record at all.
# ============================================================================
echo "== case E: instrument bite — the pre-fix routing (record ignored) violates case C's asserted shape =="
SE="$WORK/sandboxE"; make_sandbox "$SE"
# THE SABOTAGE, on the SHIPPED bytes (a copy): force the bare call at the very
# call site, reproducing the pre-fix outcome — the record is read and then
# IGNORED. One inserted line, anchored on the unique `run_gate_step` line, so the
# mutation is the call-site defect and nothing else.
awk '/^  run_gate_step "\$\{GATE_ARGV\[\*\]\}/ && !done { print "  GATE_ARGV=(./scripts/preflight.sh)"; done=1 } { print }' \
  "$SE/$BUILD_SHIP" > "$SE/ship.mut"
if ! grep -qF '  GATE_ARGV=(./scripts/preflight.sh)' "$SE/ship.mut"; then
  bad "case E — the sabotage did not land in the sandbox copy, so this case proves nothing"
else
  mv "$SE/ship.mut" "$SE/$BUILD_SHIP"
  # The sandbox is its own repo and build-and-ship refuses a dirty tree, so the
  # mutation must be committed THERE before the run — otherwise the case would
  # measure the clean-tree guard instead of the routing.
  git -C "$SE" -c user.email=bite@example.invalid -c user.name=biteproof commit -q -am "case E mutant: pre-fix routing"
  export STUB_REUSE_DECISION=partial STUB_SKIP="test,web,e2e" STUB_RERUN="build,vet" STUB_SECONDS=420
  E_RC=0; run_sandbox "$SE" caseE "$WORK/caseE.log" "$WORK/caseE.argv" || E_RC=$?
  unset STUB_REUSE_DECISION STUB_SKIP STUB_RERUN STUB_SECONDS
  E_ARGV="$(argv_of "$WORK/caseE.argv")"
  if [ "$E_RC" -ne 0 ]; then
    bad "case E — the mutated sandbox exited $E_RC, so the verdict cannot be attributed to the routing"; show "$WORK/caseE.log"
  elif [ "$E_ARGV" = "--only build,vet --reuse-record" ]; then
    bad "case E — with the record IGNORED at the call site the argv still matched case C's assertion: case C does NOT discriminate, so the cases above could pass vacuously"
  elif [ "$E_ARGV" != "<no-args>" ]; then
    bad "case E — the sabotage yielded argv [$E_ARGV], not the pre-fix <no-args> shape, so it is not the mutation this case claims"
  elif ! grep -q 'all scripts/preflight.sh gates passed' "$WORK/caseE.log"; then
    bad "case E — the sabotaged run is not labelled as a full run (the pre-fix closing line is missing)"; show "$WORK/caseE.log"
  else
    ok "case E — INSTRUMENT BITES: with the record ignored at the call site a PARTIAL record yields argv [<no-args>] and the pre-fix 'all gates passed' label — exactly the shape case C rejects, so case C is not vacuous"
  fi
fi

# ============================================================================
# case F — the quota the gate ACTUALLY receives, and the build's own pinned quota
#
# Nothing here reads a source line: `systemd-run` is a stub on PATH that records the
# argv it is handed (then execs it), and `docker` is the stub above that records
# `docker build ...`. So the asserted values are the ones the shipped script
# COMPUTED at run time. The release is the one run that always selects the full
# inventory, which includes the three browser gates preflight.sh starts CONCURRENTLY
# — a 300% gate there is a measured false red (card t_d1bc5164: a11y FAIL 1014s/817s
# at 300%, GREEN at 400% on the same tree 7670267d7), so the arm pins 400% and, in
# the second run, proves the BUILD did not move with it.
# ============================================================================
echo "== case F: the gate's scope is CPUQuota=400% and the docker build keeps --cpu-quota 300000, AS RECEIVED =="
SF="$WORK/sandboxF"; make_sandbox "$SF"
F_RC=0; run_sandbox "$SF" caseF "$WORK/caseF.log" "$WORK/caseF.argv" || F_RC=$?
F_QUOTA="$(sdr_quota "$WORK/caseF.systemd.log")"
F_BUILD="$(docker_build_quota "$WORK/caseF.docker.log")"
if [ "$F_RC" -ne 0 ]; then
  bad "case F — the sandbox run exited $F_RC (the gate stub is green, so a non-zero exit is the call site failing)"; show "$WORK/caseF.log"
elif [ "$F_QUOTA" != "400%" ]; then
  bad "case F — the gate was launched under CPUQuota=[${F_QUOTA:-<none recorded>}], want 400% ($([ -r "$WORK/caseF.systemd.log" ] && head -n1 "$WORK/caseF.systemd.log" || echo 'the systemd-run stub never ran'))"
elif [ "$F_BUILD" != "300000" ]; then
  bad "case F — the docker build was given --cpu-quota=[${F_BUILD:-<none recorded>}], want 300000 (300%, its own measured value — the build must not move with the gate)"
else
  ok "case F — AS RECEIVED: gate scope [$(head -n1 "$WORK/caseF.systemd.log" | sed 's/^systemd-run //')] and [$(sed -n 's/^docker build //p' "$WORK/caseF.docker.log" | head -n1 | cut -c1-60)…]"
fi
# The build quota must be INDEPENDENT of the gate's: the pre-fix code set
# BUILD_CPU_QUOTA from GATE_CPU_QUOTA, so bumping the gate silently moved the build.
F2_RC=0; run_sandbox "$SF" caseF2 "$WORK/caseF2.log" "$WORK/caseF2.argv" "GATE_CPU_QUOTA=500%" || F2_RC=$?
F2_QUOTA="$(sdr_quota "$WORK/caseF2.systemd.log")"
F2_BUILD="$(docker_build_quota "$WORK/caseF2.docker.log")"
if [ "$F2_RC" -ne 0 ]; then
  bad "case F — the GATE_CPU_QUOTA=500% run exited $F2_RC"; show "$WORK/caseF2.log"
elif [ "$F2_QUOTA" != "500%" ]; then
  bad "case F — with GATE_CPU_QUOTA=500% the gate was launched under [${F2_QUOTA:-<none recorded>}]: the override does not reach the scope"
elif [ "$F2_BUILD" != "300000" ]; then
  bad "case F — with GATE_CPU_QUOTA=500% the BUILD moved to --cpu-quota=[${F2_BUILD:-<none recorded>}]: the build inherits the gate's quota instead of keeping its pinned 300%"
else
  ok "case F — build quota is INDEPENDENT: GATE_CPU_QUOTA=500% moved the gate scope to CPUQuota=500% and left the build at --cpu-quota 300000"
fi

# ============================================================================
# case G — the instrument's OWN bite for case F: the pre-fix quota pair must
# violate it. Without this, case F could be passing because it reads something
# other than the scope the gate was handed.
# ============================================================================
echo "== case G: instrument bite — the pre-fix quota pair (300% gate, build inheriting it) violates case F =="
if [ -n "$SHIP_FROM" ]; then
  printf '  skip case G — this run is already driven by BITEPROOF_SHIP_FROM=%s (pre-fix bytes); case F above is the red proof\n' "$SHIP_FROM"
else
SG="$WORK/sandboxG"; make_sandbox "$SG"
# THE SABOTAGE, on the SHIPPED bytes (a copy): put back the pre-fix pair — a 300%
# gate default and a build that inherits whatever the gate is. Both substitutions are
# verified before the run, so a sabotage that cannot land is reported as a broken
# instrument instead of silently passing.
awk '
  index($0, "GATE_CPU_QUOTA=\"${GATE_CPU_QUOTA-400%}\"") == 1 { print "GATE_CPU_QUOTA=\"${GATE_CPU_QUOTA-300%}\""; g=1; next }
  index($0, "BUILD_CPU_QUOTA=\"${BUILD_CPU_QUOTA-300%}\"") == 1 { print "BUILD_CPU_QUOTA=\"${BUILD_CPU_QUOTA-${GATE_CPU_QUOTA:-}}\""; b=1; next }
  { print }
  END { exit !(g && b) }
' "$SG/$BUILD_SHIP" > "$SG/ship.mut"
if ! grep -qF 'GATE_CPU_QUOTA="${GATE_CPU_QUOTA-300%}"' "$SG/ship.mut" \
   || ! grep -qF 'BUILD_CPU_QUOTA="${BUILD_CPU_QUOTA-${GATE_CPU_QUOTA:-}}"' "$SG/ship.mut"; then
  bad "case G — the pre-fix quota pair did not land in the sandbox copy (the quota lines changed shape), so this case proves nothing"
else
  mv "$SG/ship.mut" "$SG/$BUILD_SHIP"
  # The sandbox is its own repo and build-and-ship refuses a dirty tree, so the
  # mutation must be committed THERE before the run.
  git -C "$SG" -c user.email=bite@example.invalid -c user.name=biteproof commit -q -am "case G mutant: pre-fix quotas"
  G_RC=0; run_sandbox "$SG" caseG "$WORK/caseG.log" "$WORK/caseG.argv" || G_RC=$?
  G_QUOTA="$(sdr_quota "$WORK/caseG.systemd.log")"
  if [ "$G_RC" -ne 0 ]; then
    bad "case G — the mutated sandbox exited $G_RC, so the verdict cannot be attributed to the quota"; show "$WORK/caseG.log"
  elif [ "$G_QUOTA" = "400%" ]; then
    bad "case G — with the pre-fix default the gate scope still recorded CPUQuota=400%: case F does not read the quota the gate receives, so it could pass vacuously"
  elif [ "$G_QUOTA" != "300%" ]; then
    bad "case G — the sabotage yielded CPUQuota=[${G_QUOTA:-<none recorded>}], not the pre-fix 300%, so it is not the mutation this case claims"
  else
    ok "case G — INSTRUMENT BITES on the gate quota: the pre-fix default launches the gate under CPUQuota=300%, the exact value case F rejects"
  fi
  G2_RC=0; run_sandbox "$SG" caseG2 "$WORK/caseG2.log" "$WORK/caseG2.argv" "GATE_CPU_QUOTA=500%" || G2_RC=$?
  G2_BUILD="$(docker_build_quota "$WORK/caseG2.docker.log")"
  if [ "$G2_RC" -ne 0 ]; then
    bad "case G — the mutated GATE_CPU_QUOTA=500% run exited $G2_RC"; show "$WORK/caseG2.log"
  elif [ "$G2_BUILD" = "300000" ]; then
    bad "case G — with the pre-fix inheritance a 500% gate still gave the build 300000: case F's independence assertion does not discriminate"
  elif [ "$G2_BUILD" != "500000" ]; then
    bad "case G — the mutated run gave the build --cpu-quota=[${G2_BUILD:-<none recorded>}], not the inherited 500000"
  else
    ok "case G — INSTRUMENT BITES on the build pin: with the pre-fix inheritance GATE_CPU_QUOTA=500% also moves the build to --cpu-quota 500000, the value case F rejects"
  fi
fi
fi

# ============================================================================
# integrity — the shipped bytes were not touched by this run
# ============================================================================
echo "== integrity: the shipped build-and-ship.sh is byte-identical after the run =="
GOT_SHIP="$(sha256sum "$BUILD_SHIP" | awk '{print $1}')"
if [ "$GOT_SHIP" = "$WANT_SHIP" ]; then
  ok "integrity — $BUILD_SHIP unchanged (sha256 ${GOT_SHIP:0:12}); every mutation lived in $WORK"
else
  bad "integrity — $BUILD_SHIP CHANGED during the run ($GOT_SHIP != $WANT_SHIP) — restore it before doing anything else"
fi

echo "---"
if [ "$FAIL" -eq 0 ]; then
  echo "biteproof: $PASS checks passed, 0 failed — the deploy call site's reuse plumbing BITES: a full-green record skips the gate pool (no slot taken, 0 gates, seconds), a missing / partial / refused record still gates in full, a partial record re-runs only the unvouched gates, each case reports which gates were remembered rather than run, and the gate is launched under CPUQuota=400% with the docker build kept at its own pinned 300% (both read from stub-recorded argv, and the pre-fix quotas are shown to violate that)"
else
  echo "biteproof: $PASS passed, $FAIL failed — the deploy call site does NOT hold something it claims to"
fi
[ "$FAIL" -eq 0 ] || exit 1
