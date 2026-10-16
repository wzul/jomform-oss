#!/usr/bin/env bash
#
# scripts/biteproof_api_dockerignore.sh — prove that api/.dockerignore's `_test.go`
# exclusion is a REAL exclusion, measured INSIDE a real docker build of the api
# context, and that the ROOT .dockerignore's opposite choice is the negative control
# for the same class.
#
# WHY THIS EXISTS (the gap it closes). Card t_a83dcf33 shipped the fix (PR 1185): the
# file carried only a bare `*_test.go`, and a bare pattern in .dockerignore matches the
# context ROOT only — `*` never crosses `/`, exactly as in filepath.Match — so it
# excluded the api context's 0 top-level test files and left every NESTED one in the
# context (probe measured 413 on 2026-10-13 at main 54696d77, 441 on this tree). The fix
# added `**/*_test.go`. NOTHING ELSE MEASURES IT: no gate builds the api image (the
# `webimage` gate builds the WEB image only; the api image is built later by
# deploy/build-and-ship.sh, never gated) and no Go test, script or bite harness reads
# api/.dockerignore. So the file could be reverted to the bare rule and EVERY gate would
# stay green — the exact "looks right and does not work" shape the defect was.
#
# WHAT IT PROVES. Each case is a real `docker build --output type=cacheonly` whose
# readouts are counted INSIDE the build (`find /ctx`), and each case asserts the
# direction the other one cannot:
#   CASE 0  the committed api/.dockerignore, at its REAL path -> the build context holds
#           ZERO _test.go (the exclusion happens, and docker really reads the rules from
#           that path).
#   CASE 1  api/.dockerignore with BOTH `_test.go` rules removed -> the context holds the
#           whole tree, and the count equals the host tree's exactly. This is the control
#           that makes CASE 0's 0 mean something.
#   CASE 2  the BARE `*_test.go` rule alone — GH #768's exact file -> the count is
#           IDENTICAL to CASE 1's: the bare rule excludes exactly as much as no rule at
#           all, which is the defect stated as a measurement rather than a claim.
#   CASE 3  `**/*_test.go` alone -> ZERO, so the any-depth rule is the one doing the
#           work and the file's own comment about the two forms is true in this engine.
#   CASE 4  the ROOT .dockerignore, at its real path, over the root context ->
#           api/internal/mcpsrv/mcp_parity_test.go IS in the context. The web image's
#           build stage runs `npm run build` -> the unit tests, and
#           web/src/lib/langPlacement.test.ts reads that file at image-build time, so the
#           root file must NOT exclude test files.
#   CASE 5  the ROOT .dockerignore plus `**/*_test.go` -> that file is GONE. Red as
#           required: it proves CASE 4 is not vacuous (the probe really does see the root
#           file filter) and reproduces why the root file stays as it is.
#
# WHY THE COUNTS ARE READ INSIDE THE BUILD, NOT FROM THE BUILD OUTPUT: the
# `transferring context` line is not a faithful measure — in the docker-driver build it
# reported 8.75MB for a 4.0MB tree while the real context was 8.9MB (measured while
# fixing t_a83dcf33). `find /ctx` inside the RUN is the measurement, and CASE 1
# cross-checks it against the host tree.
#
# WHY IT MUTATES THE TRACKED FILES. A case has to feed docker a file at the path docker
# reads, and `--ignorefile` is not a `docker build` flag on this host (measured on
# 29.7.1: `unknown flag: --ignorefile`). Each mutation is applied IN PLACE from a
# snapshot of the committed bytes, and both files are restored and re-verified by sha256
# after every case (the convention web/scripts/biteproof_next_managed_files.sh and
# scripts/biteproof_web_image_gate.sh use). A per-worktree `flock` serialises two runs of
# THIS harness: a peer mid-case would read a mutated file and both runs would print false
# verdicts — the defect web/src/lib/harnessLock.test.ts exists for. LIMIT, stated because
# the lock cannot close it: the lock is only taken by this script, so it does NOT
# serialise against a peer lane running the `webimage` gate out of this same checkout
# while CASE 5 has the root file mutated (a few seconds). Do not run it as part of a gate.
#
# COST, measured on this host: ~7s per api-context case and ~5s per root-context case
# (alpine is the only image and it is pulled once). On demand only, never a gate — the
# decision BITE-HARNESSES.md records for the whole family. The probe Dockerfiles are
# written to a mktemp directory OUTSIDE the repo, so no case adds a file to the tree.
#
# Run:  ./scripts/biteproof_api_dockerignore.sh
# Exit: 0 every case bit; 1 a case did not; 2 REFUSED (nothing measured, nothing
#       mutated, no verdict — this script never reports a green it did not measure);
#       75 a peer holds this worktree's lock (the family's code for "did not run").

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
refuse() {
  printf 'BITEPROOF REFUSED: %s\n' "$1" >&2
  shift
  local line
  for line in "$@"; do printf '  %s\n' "$line" >&2; done
  printf '  Nothing was measured and nothing was mutated: this harness never reports a green it did not measure.\n' >&2
  exit 2
}

API_IGNORE="api/.dockerignore"
ROOT_IGNORE=".dockerignore"
API_DOCKERFILE="api/Dockerfile"
PROBE_IMAGE="alpine:3.20"

# --------------------------------------------------------------- prerequisites
# Each prerequisite REFUSES (exit 2, no verdict) rather than skipping: a skipped
# instrument is not a passing one, and every case below needs a real docker build.
command -v docker >/dev/null 2>&1 || refuse \
  "docker is not installed, so no case here can build the context it measures" \
  "Install docker, or run this harness on a host that has it."

docker info >/dev/null 2>&1 || refuse \
  "the docker daemon is unreachable (docker info failed), so no case here can build" \
  "Start the daemon before asking this instrument for a verdict."

[ -f "$API_DOCKERFILE" ] || refuse \
  "$API_DOCKERFILE is missing, so the context this harness measures is not buildable" \
  "This instrument measures the api/ BUILD CONTEXT; without the Dockerfile there is no" \
  "build to measure, and a missing Dockerfile is not a green."

if ! docker image inspect "$PROBE_IMAGE" >/dev/null 2>&1; then
  docker pull "$PROBE_IMAGE" >/dev/null 2>&1 || refuse \
    "the probe image $PROBE_IMAGE is neither local nor pullable (offline?), so no case can run" \
    "The probe is FROM $PROBE_IMAGE + COPY + find. Pre-pull it on a disconnected host."
fi

# The vacuacy floor: with no test file in the tree, a probe reading 0 would prove
# nothing at all — the one state in which this instrument must not report a verdict.
HOST_TESTFILES="$(find api -name '*_test.go' -not -path 'api/bin/*' | wc -l | tr -d ' ')"
HOST_ALLFILES="$(find api -type f -not -path 'api/bin/*' | wc -l | tr -d ' ')"
if [ "$HOST_TESTFILES" -eq 0 ]; then
  refuse "the host tree holds no _test.go under api/ ($HOST_TESTFILES test files)," \
         "so a probe that reads 0 would prove nothing and every case below would pass" \
         "vacuously. Nothing to measure until api/ has tests again."
fi

# ------------------------------------------------- the per-worktree lock (fd 9)
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-900}"
command -v flock >/dev/null 2>&1 || refuse \
  "flock is not installed, so two runs of this harness in one worktree cannot be serialised" \
  "A peer mid-case reads a mutated .dockerignore and both runs print FALSE verdicts —" \
  "the defect web/src/lib/harnessLock.test.ts exists for. Install flock."
case "$LOCK_WAIT" in
  ''|*[!0-9]*) refuse "BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds" ;;
esac
BITE_GIT_DIR="$(git rev-parse --absolute-git-dir 2>/dev/null)" || refuse \
  "cannot resolve this worktree's git dir (git rev-parse --absolute-git-dir), so there is" \
  "no per-worktree key to serialise on, and an unserialised run prints false verdicts"
BITE_KEY="$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)"
LOCK_DIR="$LOCK_BASE/jomform-biteproof-api-dockerignore"
mkdir -p "$LOCK_DIR" || refuse \
  "cannot create the lock directory $LOCK_DIR" \
  "Set BITEPROOF_LOCK_DIR to a writable directory."
LOCK_FILE="${BITEPROOF_LOCK_FILE:-$LOCK_DIR/$BITE_KEY.lock}"
exec 9>"$LOCK_FILE" || refuse "cannot open the lock file $LOCK_FILE"
if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
  printf '  lock:   another run of this harness holds this worktree lock — waiting up to %ss\n' "$LOCK_WAIT"
fi
flock -w "$LOCK_WAIT" -E 75 9
_lock_rc=$?
if [ "$_lock_rc" -eq 75 ]; then
  echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s, so" >&2
  echo "  this run is NOT serialised against a peer that mutates the same .dockerignore files." >&2
  echo "  Nothing was mutated and no verdict is reported. Re-run once the other run finishes, or" >&2
  echo "  raise the wait with BITEPROOF_LOCK_WAIT=<seconds>." >&2
  exit 75
fi

# ------------------------------------------------- snapshot the mutated files
WORK="$(mktemp -d "${TMPDIR:-/tmp}/biteproof_api_dockerignore.XXXXXX")" || refuse \
  "cannot create a scratch directory under ${TMPDIR:-/tmp}" \
  "The probe Dockerfiles live there, deliberately outside the repo."
PROBE_LOG="$WORK/probe.log"

API_SNAP="$WORK/api.dockerignore.orig"
ROOT_SNAP="$WORK/root.dockerignore.orig"
API_ABSENT=0; ROOT_ABSENT=0
if [ -f "$API_IGNORE" ]; then cp "$API_IGNORE" "$API_SNAP"; else : > "$API_SNAP"; API_ABSENT=1; fi
if [ -f "$ROOT_IGNORE" ]; then cp "$ROOT_IGNORE" "$ROOT_SNAP"; else : > "$ROOT_SNAP"; ROOT_ABSENT=1; fi
API_WANT="$(sha256sum "$API_SNAP" | awk '{print $1}')"
ROOT_WANT="$(sha256sum "$ROOT_SNAP" | awk '{print $1}')"
GIT_START="$(git status --porcelain -- "$API_IGNORE" "$ROOT_IGNORE")"

restore_all() {
  if [ "$API_ABSENT" = 1 ]; then rm -f "$API_IGNORE"; else cp "$API_SNAP" "$API_IGNORE"; fi
  if [ "$ROOT_ABSENT" = 1 ]; then rm -f "$ROOT_IGNORE"; else cp "$ROOT_SNAP" "$ROOT_IGNORE"; fi
}
cleanup() {
  restore_all
  # The failure messages tell the operator to restore BY HAND from the snapshots, so a
  # run that reported a failure KEEPS them: deleting the thing the remedy names is how a
  # recovery instruction becomes a dead path.
  if [ "$FAIL" -gt 0 ]; then
    printf '  snapshots kept for manual recovery (this run reported failures): %s\n' "$WORK"
    return
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

# restore_check <label> — restore both files and re-verify byte-identically (by sha256
# against the snapshot). A case that leaves a mutation on disk makes every later run in
# this worktree judge a mutated tree, so this is a failure, never a warning.
restore_check() {
  local label="$1" got
  restore_all
  if [ "$API_ABSENT" = 1 ]; then
    [ -e "$API_IGNORE" ] && bad "$label — $API_IGNORE did not exist at the start and this run LEFT IT BEHIND"
  else
    got="$(sha256sum "$API_IGNORE" 2>/dev/null | awk '{print $1}')"
    [ "$got" = "$API_WANT" ] || bad "$label — $API_IGNORE is NOT restored byte-identically (sha256 $got != $API_WANT) — restore it BY HAND from $API_SNAP before doing anything else"
  fi
  if [ "$ROOT_ABSENT" = 1 ]; then
    [ -e "$ROOT_IGNORE" ] && bad "$label — $ROOT_IGNORE did not exist at the start and this run LEFT IT BEHIND"
  else
    got="$(sha256sum "$ROOT_IGNORE" 2>/dev/null | awk '{print $1}')"
    [ "$got" = "$ROOT_WANT" ] || bad "$label — $ROOT_IGNORE is NOT restored byte-identically (sha256 $got != $ROOT_WANT) — restore it BY HAND from $ROOT_SNAP before doing anything else"
  fi
}

# A mutation that did not land is a silent GREEN, so every case asserts its edit is on
# disk (whole-line, fixed-string) before it measures anything.
line_present() { grep -qxF "$2" "$1"; }
line_absent()  { ! grep -qxF "$2" "$1"; }

# mutate_api_drop <rule>... — rewrite api/.dockerignore from the COMMITTED bytes with
# the named rule LINES removed (nothing else changes: a mutation that is a rewrite is
# not a mutation of this class). Each pattern goes to grep through its OWN `-e`, because
# `grep -vxF P1 P2` treats P2 as a FILE NAME and then rewrites the ignore file from an
# EMPTY read (measured on this harness's first cut: `grep: **/*_test.go: No such file or
# directory` while the case still measured a number — a mutation that did not do what it
# says is exactly the silent-green shape this file exists for).
# Returns 1 when there is no file to mutate, 2 when grep itself refused the snapshot.
mutate_api_drop() {
  [ "$API_ABSENT" = 1 ] && return 1
  local args=() p rc
  for p in "$@"; do args+=(-e "$p"); done
  grep -vxF "${args[@]}" "$API_SNAP" > "$API_IGNORE"
  rc=$?
  [ "$rc" -le 1 ] || return 2
  return 0
}

# mutation_failed <case> — one message for the two ways a mutation can fail, so a red is
# never attributed to the wrong cause.
mutation_failed() {
  if [ "$API_ABSENT" = 1 ]; then
    bad "$1 — $API_IGNORE does not exist, so the rules that carry this class are GONE and no mutation could be applied. CASE 0 above measured the consequence"
  else
    bad "$1 — the mutation could not be applied to $API_IGNORE (grep refused the snapshot); re-run and check the harness itself"
  fi
}

# ------------------------------------------------------------- the probe builds
cat > "$WORK/api_ctx.Dockerfile" <<'DOCKERFILE'
FROM alpine:3.20
COPY . /ctx
RUN echo "PROBE_TESTFILES=$(find /ctx -name '*_test.go' | wc -l)" \
 && echo "PROBE_ALLFILES=$(find /ctx -type f | wc -l)" \
 && echo "PROBE_TESTLIST=$(find /ctx -name '*_test.go' | sort | head -3 | tr '\n' ' ')"
DOCKERFILE

cat > "$WORK/root_ctx.Dockerfile" <<'DOCKERFILE'
FROM alpine:3.20
COPY api /ctx
RUN echo "PROBE_PARITYTEST=$(find /ctx -name 'mcp_parity_test.go' | wc -l)" \
 && echo "PROBE_TESTFILES=$(find /ctx -name '*_test.go' | wc -l)" \
 && echo "PROBE_ALLFILES=$(find /ctx -type f | wc -l)"
DOCKERFILE

PROBE_RC=0; PROBE_TESTFILES=""; PROBE_ALLFILES=""; PROBE_PARITYTEST=""

probe_field() { sed -n "s/.*$1=\([0-9][0-9]*\).*/\1/p" "$PROBE_LOG" | tail -n 1; }
probe_paths() { sed -n 's/.*PROBE_TESTLIST=\(.*\)$/\1/p' "$PROBE_LOG" | tail -n 1; }

# probe_build <dockerfile> <context> — run one real build and read the probe's own
# counts out of the build log. `--no-cache` so the RUN always executes (a cached layer
# would report the previous context's numbers) and `--output type=cacheonly` so no
# image is exported or tagged. The Dockerfile is passed with -f from outside the
# context, which is why the repo tree never carries a probe file.
probe_build() {
  local df="$1" ctx="$2"
  docker build -f "$df" --progress=plain --no-cache --output type=cacheonly "$ctx" >"$PROBE_LOG" 2>&1
  PROBE_RC=$?
  PROBE_TESTFILES="$(probe_field PROBE_TESTFILES)"
  PROBE_ALLFILES="$(probe_field PROBE_ALLFILES)"
  PROBE_PARITYTEST="$(probe_field PROBE_PARITYTEST)"
}

# probe_gate — the verdict gate for a probe that has just run: 0 only when the build
# SUCCEEDED and PRINTED a count. A missing count is a FAILURE and never a 0: an empty
# read that silently became "nothing was found" is exactly how a broken instrument
# reports green, which is the class this file exists for. The self-check at the end
# drives THIS function against a build that cannot print, so the gate's own bite is
# measured on every run rather than asserted.
probe_gate() {
  if [ "$PROBE_RC" -ne 0 ]; then
    printf 'the probe build FAILED (rc=%s), so no count was produced — log: %s\n' "$PROBE_RC" "$PROBE_LOG"
    return 1
  fi
  if [ -z "$PROBE_TESTFILES" ]; then
    printf 'the probe built but printed no PROBE_TESTFILES count (a missing count must never read as 0) — log: %s\n' "$PROBE_LOG"
    return 1
  fi
  return 0
}

echo "biteproof: the api build context's _test.go exclusion (api/.dockerignore)"
echo "  repo:   $(pwd)"
echo "  head:   $(git rev-parse --short HEAD)"
echo "  api/.dockerignore sha256 ${API_WANT}$( [ "$API_ABSENT" = 1 ] && printf ' (ABSENT at the start of this run)')"
echo "  .dockerignore     sha256 ${ROOT_WANT}$( [ "$ROOT_ABSENT" = 1 ] && printf ' (ABSENT at the start of this run)')"
echo "  host:   api/ holds $HOST_TESTFILES _test.go of $HOST_ALLFILES files (the counts CASE 1 cross-checks against)"
echo "---"

N_NO_RULE=""

# --- CASE 0: the committed file, at its real path (control GREEN) -------------
echo "== CASE 0: the committed api/.dockerignore, at its real path =="
probe_build "$WORK/api_ctx.Dockerfile" api/
if why="$(probe_gate)"; then
  if [ "$PROBE_TESTFILES" = 0 ]; then
    ok "CASE 0 — the api build context holds 0 _test.go in ${PROBE_ALLFILES} files: the committed exclusion happens, and docker reads it from $API_IGNORE"
  else
    bad "CASE 0 — the committed $API_IGNORE left $PROBE_TESTFILES _test.go in the api build context (want 0). This is the defect itself. First offenders: $(probe_paths)"
  fi
else
  bad "CASE 0 — $why"
fi
restore_check "CASE 0"

# --- CASE 1: no _test.go rule at all (the control for CASE 0's 0) -------------
echo "== CASE 1: api/.dockerignore with BOTH _test.go rules removed =="
if mutate_api_drop '*_test.go' '**/*_test.go'; then
  if line_absent "$API_IGNORE" '*_test.go' && line_absent "$API_IGNORE" '**/*_test.go'; then
    probe_build "$WORK/api_ctx.Dockerfile" api/
    if why="$(probe_gate)"; then
      N_NO_RULE="$PROBE_TESTFILES"
      if [ "$PROBE_TESTFILES" = "$HOST_TESTFILES" ]; then
        ok "CASE 1 — with no _test.go rule the context holds $PROBE_TESTFILES (== the host tree's $HOST_TESTFILES, so no other rule filtered one either): CASE 0's 0 comes from the rules and not from a tree with no tests in it"
      else
        bad "CASE 1 — the probe counted $PROBE_TESTFILES in the context against $HOST_TESTFILES on the host. Either a non-_test.go rule is filtering ${HOST_TESTFILES}-minus-${PROBE_TESTFILES} test file(s) out of the context (that is this same class and worth a look), or files are being left out of the context for another reason. Reconcile before trusting CASE 2/3"
      fi
    else
      bad "CASE 1 — $why"
    fi
  else
    bad "CASE 1 — the mutation did not land (a _test.go rule is still in $API_IGNORE), so the case would have measured the committed file and reported a false green"
  fi
else
  mutation_failed "CASE 1"
fi
restore_check "CASE 1"

# --- CASE 2: GH #768's exact file — the BARE rule alone (must bite) ----------
echo "== CASE 2: the BARE *_test.go rule alone — GH #768's exact file =="
if mutate_api_drop '**/*_test.go'; then
  if line_present "$API_IGNORE" '*_test.go' && line_absent "$API_IGNORE" '**/*_test.go'; then
    probe_build "$WORK/api_ctx.Dockerfile" api/
    if why="$(probe_gate)"; then
      if [ -z "$N_NO_RULE" ]; then
        bad "CASE 2 — CASE 1 measured no count, so the bare rule's \"excludes nothing at depth\" comparison cannot be made on this run"
      elif [ "$PROBE_TESTFILES" = "$N_NO_RULE" ]; then
        ok "CASE 2 — the bare rule excludes EXACTLY as much as no rule at all: $PROBE_TESTFILES (== CASE 1's $N_NO_RULE). It looks like an exclusion and is not one at depth; the \`**\` rule is the one doing the work"
      else
        bad "CASE 2 — with the bare rule alone the context holds $PROBE_TESTFILES but CASE 1 measured $N_NO_RULE with no rule at all. The two must be equal: if the bare rule now excludes anything at depth, this instrument's premise for GH #768 has moved and must be re-derived"
      fi
    else
      bad "CASE 2 — $why"
    fi
  else
    bad "CASE 2 — the mutation did not land (want $API_IGNORE to carry \`*_test.go\` and not \`**/*_test.go\`), so the case would have measured the committed file"
  fi
else
  mutation_failed "CASE 2"
fi
restore_check "CASE 2"

# --- CASE 3: the `**` rule alone (which form does the work) ------------------
echo "== CASE 3: \`**/*_test.go\` alone =="
if mutate_api_drop '*_test.go'; then
  if line_absent "$API_IGNORE" '*_test.go' && line_present "$API_IGNORE" '**/*_test.go'; then
    probe_build "$WORK/api_ctx.Dockerfile" api/
    if why="$(probe_gate)"; then
      if [ "$PROBE_TESTFILES" = 0 ]; then
        ok "CASE 3 — the any-depth rule alone leaves 0 _test.go in the context, so it is the rule the exclusion rests on and the file's comment about the two forms is true here"
      else
        bad "CASE 3 — with \`**/*_test.go\` alone the context still holds $PROBE_TESTFILES _test.go. The any-depth wildcard does NOT cover this engine, so the committed file's protection rests on the bare rule (which CASE 2 measured as inert) and is not what it claims. First offenders: $(probe_paths)"
      fi
    else
      bad "CASE 3 — $why"
    fi
  elif line_absent "$API_IGNORE" '*_test.go'; then
    bad "CASE 3 — with the bare rule removed the file carries NO \`**/*_test.go\` rule either: the committed file has no any-depth rule at all (GH #768's file), and the context carries every nested test file. That is the defect this instrument exists for, and CASE 0 measured it above"
  else
    bad "CASE 3 — the mutation did not land (the bare rule is still in $API_IGNORE), so the case would have measured the committed file"
  fi
else
  mutation_failed "CASE 3"
fi
restore_check "CASE 3"

# --- CASE 4: the ROOT file, at its real path (the negative control) ----------
echo "== CASE 4: the ROOT .dockerignore, at its real path, over the root context =="
probe_build "$WORK/root_ctx.Dockerfile" .
if why="$(probe_gate)"; then
  if [ "${PROBE_PARITYTEST:-}" = 1 ] && [ "${PROBE_TESTFILES:-0}" -gt 0 ]; then
    ok "CASE 4 — the root context carries mcp_parity_test.go ($PROBE_PARITYTEST) and $PROBE_TESTFILES _test.go: the ROOT file excludes no test file, which is what the web image's build needs (its build stage runs the unit tests, and langPlacement.test.ts reads that file at image-build time)"
  else
    bad "CASE 4 — the root context carries mcp_parity_test.go $PROBE_PARITYTEST time(s) and $PROBE_TESTFILES _test.go in total; both must be at least 1. The root .dockerignore is excluding test files, which breaks the web image build's own unit tests — do NOT \"fix\" the api side of this by copying that rule to the root file"
  fi
else
  bad "CASE 4 — $why"
fi
restore_check "CASE 4"

# --- CASE 5: the root file PLUS an exclusion (must bite) ---------------------
echo "== CASE 5: the ROOT .dockerignore plus \`**/*_test.go\` (the shape a careless 'fix' would add) =="
if [ "$ROOT_ABSENT" = 1 ]; then
  bad "CASE 5 — $ROOT_IGNORE does not exist, so the rule that must NOT be added cannot be driven, and CASE 4's control cannot be shown to bite"
else
  cp "$ROOT_SNAP" "$ROOT_IGNORE"
  printf '\n**/*_test.go\n' >> "$ROOT_IGNORE"
  if line_present "$ROOT_IGNORE" '**/*_test.go'; then
    probe_build "$WORK/root_ctx.Dockerfile" .
    if why="$(probe_gate)"; then
      if [ "${PROBE_PARITYTEST:-x}" = 0 ] && [ "${PROBE_TESTFILES:-x}" = 0 ]; then
        ok "CASE 5 — RED as required: with that rule in the root file the context loses mcp_parity_test.go and every other _test.go (0 of both), so CASE 4 is not vacuous — the probe really does see the root file filter, and this is the shape that breaks the web image build's unit tests"
      else
        bad "CASE 5 — the root context still carried mcp_parity_test.go $PROBE_PARITYTEST time(s) and $PROBE_TESTFILES _test.go with \`**/*_test.go\` in the ROOT file, so CASE 4's green cannot be attributed to the absence of that rule"
      fi
    else
      bad "CASE 5 — $why"
    fi
  else
    bad "CASE 5 — the mutation did not land (\`**/*_test.go\` is not in $ROOT_IGNORE)"
  fi
fi
restore_check "CASE 5"

# --- SELF-CHECK: a count-less probe must be a FAILURE, not a 0 ---------------
echo "== SELF-CHECK: a probe that prints no count must be reported as a FAILURE, never as 0 =="
cat > "$WORK/broken_ctx.Dockerfile" <<'DOCKERFILE'
FROM alpine:3.20
COPY ./no-such-path-in-this-context /ctx
DOCKERFILE
probe_build "$WORK/broken_ctx.Dockerfile" api/
if probe_gate >"$WORK/selfcheck.out" 2>&1; then
  bad "SELF-CHECK — the gate ACCEPTED a probe that printed no count (rc=$PROBE_RC), so every 0 above could be an unmeasured zero and this instrument is decoration"
else
  ok "SELF-CHECK — the gate REJECTED a count-less probe on this run ($(tr -d '\n' < "$WORK/selfcheck.out" | cut -c1-140))"
fi

# --- the tree state, asserted rather than hoped ------------------------------
# The property is "this run left the tree as it found it", NOT "the tree matches HEAD":
# the harness is meant to be runnable from a card that is itself editing one of these
# files, so a legitimate dirty file must not read as a restore failure.
echo "---"
GIT_END="$(git status --porcelain -- "$API_IGNORE" "$ROOT_IGNORE")"
if [ "$GIT_END" = "$GIT_START" ]; then
  ok "this run left the tree exactly as it found it (git reports [$GIT_START] before and [$GIT_END] after; both files restored byte-identically, sha256 $API_WANT / $ROOT_WANT)"
else
  bad "this run CHANGED the ignore files' state: before [$GIT_START], after [$GIT_END] — restore them BY HAND from $API_SNAP / $ROOT_SNAP"
fi

if [ "$FAIL" -eq 0 ]; then
  echo "biteproof: 6 cases, $PASS checks passed, 0 failed in ${SECONDS}s — every case was a real \`docker build --output type=cacheonly\` of the context it names, both files came back byte-identical, and no image was exported"
else
  echo "biteproof: 6 cases, $PASS passed, $FAIL failed in ${SECONDS}s — this instrument does NOT cover what its cases claim"
fi
[ "$FAIL" -eq 0 ] || exit 1
exit 0
