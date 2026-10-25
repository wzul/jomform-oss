#!/usr/bin/env bash
# Bite proof for the Settings → Workspace "Danger zone" refusal guard (card t_8030247d):
#   web/e2e/workspace-trash.spec.ts   (the delete / trash-restore refusals)
#
# THE CLAIM is that a refusal the server sends back stays READABLE even when the
# deletion-check REFETCH the refusal itself fires FAILS. It is the second surface of
# the defect card t_d9dda884 fixed on /forms' Trash view (same class, different page,
# filed separately per the board's one-surface-per-card convention).
#
# THE DEFECT, in the shipped code:
#
#   WorkspaceTab.tsx  doDelete()   setDeleteErr(refusal);  await load();
#   WorkspaceTab.tsx  load()       failure -> setCheck(null); setErr(r.error);
#   WorkspaceTab.tsx  render       if (!check) return <load-failure card/>;   <- whole page
#   WorkspaceTab.tsx  render       {(deleteErr || err) && …}                 <- never reached
#
# So a refused delete followed by a failing refetch (transient 5xx / offline) replaced
# the reason with "We couldn't check this workspace" and the merchant never read WHY
# the workspace was not deleted. The trash-RESTORE refusal (`trashErr`, on the same
# tab) is erased by the same branch — one site, two refusals.
#
# The two mutations are the PRE-FIX SHAPES of this card's own fix, each a single
# compiling edit with the rest of the fix KEPT (so a case measures the branch, not a
# deleted feature), and each rebuilds the app first: the spec measures the BUILD, so a
# stale .next would make the verdict about bytes nobody shipped.
#
# A guard that only ever passes is a tautology, so the baseline is a real case too:
# the unmutated tree must be GREEN at an EXACT 6 passed (this spec's 4 pre-existing
# arms plus this card's 2), or a red below could be the harness itself.
#
# Run:  bash web/scripts/biteproof_workspace_danger_refusal.sh
# Env:  A11Y_PORT (default 3415) — playwright.config.ts's webServer serves there.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE any `cd`, because the child this
# file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# ---- PER-WORKTREE LOCK -----------------------------------------------------
# The family's key, NOT a per-script one: this harness rewrites
# src/components/WorkspaceTab.tsx and runs playwright against the SAME worktree's
# build and the SAME webServer port as the other biteproof harnesses, which rewrite
# their own pages and rebuild the same .next — they all mutate the same tree, so they
# must all serialise on the same file (see web/src/lib/harnessLock.test.ts).
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs rebuild .next and drive the same port under" >&2
    echo "each other's feet, which prints false verdicts." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR. Set" >&2
    echo "BITEPROOF_LOCK_DIR to a writable dir." >&2
    exit 2
  }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || {
    echo "BITEPROOF REFUSED: cannot open the lock file $LOCK_FILE." >&2
    exit 2
  }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   another bite harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (they mutate the same shared tree, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer mutating the same tree." >&2
    echo "Nothing was mutated. Re-run once the other run finishes, or raise the wait with" >&2
    echo "BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi
# The child. Refuse if the lock it is supposed to have inherited cannot be shown to
# be held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same tree would give it false verdicts." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here — it would read REFUSED for any file." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

cd "$(dirname "$0")/.." || exit 2

# The mutated source and the guard. Both overridable so a probe can reach this
# harness's own node-free refusal (harnessLock.test.ts leg D3).
PAGE=${BITEPROOF_PAGE:-src/components/WorkspaceTab.tsx}
SPEC=${BITEPROOF_SPEC:-e2e/workspace-trash.spec.ts}
PLAYWRIGHT=./node_modules/.bin/playwright
# The lock vars must not reach anything this harness SPAWNS: a leaked
# LOCKED/LOCK_FILE/LOCK_DIR makes each spawned harness contend on the lock THIS
# script already holds (card t_df41e675).
nolock() { env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR "$@"; }
export A11Y_PORT="${A11Y_PORT:-3415}"
TMP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }

bak_of() { printf '%s/%s.bak' "$TMP" "$(printf '%s' "$1" | tr '/' '_')"; }
restore() {
  [ -f "$(bak_of "$PAGE")" ] && cp "$(bak_of "$PAGE")" "$PAGE"
}
trap 'restore; rm -rf "$TMP"' EXIT

for f in "$PAGE" "$SPEC"; do
  [ -f "$f" ] || { echo "BITEPROOF REFUSED: $f is missing — nothing to mutate." >&2; exit 2; }
done
cp "$PAGE" "$(bak_of "$PAGE")" || exit 2
PAGE_SHA_BEFORE="$(sha256sum "$PAGE")"

fail=0
bites=0
mutations=0

rebuild() {
  echo "   (next build — the specs measure the BUILT page)"
  if ! nolock npx next build >"$TMP/build.log" 2>&1; then
    echo "   BUILD FAILED — see $TMP/build.log"
    tail -20 "$TMP/build.log"
    return 1
  fi
}

# args: <case name> <expect: pass|red> <named assertion (ERE alternation)> <specs...>
#
# THE MATCH IS SCOPED TO THE REPORTER'S Error BLOCK, NOT THE WHOLE OUTPUT (card
# t_dd211ae1): playwright ECHOES the spec's source on a failed assertion, so every
# string literal in the assertion message — the named thing itself — is printed
# whatever the test actually died of.
run_case() {
  local name="$1" want="$2" expect="$3"
  shift 3
  echo "== $name"
  local out
  out="$(nolock "$PLAYWRIGHT" test "$@" --project=feature-e2e --reporter=list 2>&1)"
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  if [ "$want" = pass ]; then
    # An EXACT count, not "some passed": the unmutated tree has 6 tests in this
    # spec. A spec whose tests were deleted would print a smaller number, and that
    # must not read as a green control.
    if grep -qE "^ +6 passed" <<<"$out" && ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
      grep -E "^ +6 passed" <<<"$out" | sort -u | head -1
      echo "   OK (green control: the unmutated tree really passes this spec)"
    else
      echo "   BASELINE NOT GREEN — the spec is red on a tree with no defect, so no"
      echo "   red below could be attributed to a mutation:"
      grep -E "passed|failed|^ *Error: " <<<"$out" | sort -u | head -8
      fail=1
    fi
    return
  fi
  if ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
    echo "   NOT DETECTED — the guard stayed green with the pre-fix shape present"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qE "$expect" <<<"$error_block"; then
    echo "   DETECTED, but NOT at a named assertion. Wanted one of: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    bites=$((bites + 1))
    grep -E "^ +[0-9]+ failed" <<<"$out" | sort -u | head -1
    grep -E "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
}

# --- case 0
echo
echo "== case 0: baseline: the fix in place, this tree built (green control)"
restore
rebuild || exit 2
run_case "0. the fixed tree passes the guard" pass "" "$SPEC"

# --- case 1
echo
echo "== case 1: the PRE-FIX branch: the failure card renders no refusal at all"
# The exact pre-fix shape, with the rest of the fix KEPT: the refusal channels still
# exist and are still written, the branch simply does not read them — which is what
# the shipped `if (!check) { return <load-failure card/> }` did. Fails closed if the
# source no longer matches.
mutations=$((mutations + 1))
restore
if grep -qF 'const refusals = [deleteErr, trashErr].filter((s) => s !== "");' "$PAGE" \
  && perl -0pi -e 's/const refusals = \[deleteErr, trashErr\]\.filter\(\(s\) => s !== ""\);/const refusals: string[] = [];/' "$PAGE" \
  && grep -qF 'const refusals: string[] = [];' "$PAGE" \
  && ! grep -qF 'const refusals = [deleteErr, trashErr]' "$PAGE"; then
  if rebuild; then
    run_case "1. the refusal is erased by the load failure branch" red \
      "the refusal must stay readable when its own refetch FAILS|the refused restore must stay readable beside the load failure" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

# --- case 2
echo
echo "== case 2: only the TRASH-RESTORE refusal dropped: one branch, two refusals"
# The delete refusal is still read, so leg A stays GREEN and only the restore leg
# reds — which is what proves the two refusals are pinned separately rather than by
# one shared assertion.
mutations=$((mutations + 1))
restore
if grep -qF 'const refusals = [deleteErr, trashErr].filter((s) => s !== "");' "$PAGE" \
  && perl -0pi -e 's/const refusals = \[deleteErr, trashErr\]\.filter\(\(s\) => s !== ""\);/const refusals = [deleteErr].filter((s) => s !== "");/' "$PAGE" \
  && grep -qF 'const refusals = [deleteErr].filter((s) => s !== "");' "$PAGE" \
  && ! grep -qF '[deleteErr, trashErr].filter' "$PAGE"; then
  if rebuild; then
    run_case "2. a refused restore is dropped from the failure card" red \
      "the refused restore must stay readable beside the load failure" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== restoring the fix and the build"
restore
if ! rebuild; then
  echo "   BUILD RESTORE FAILED — .next may still be a sabotage build"
  fail=1
fi

PAGE_SHA_AFTER="$(sha256sum "$PAGE")"
if [ "$PAGE_SHA_BEFORE" != "$PAGE_SHA_AFTER" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  echo "    before: $PAGE_SHA_BEFORE"
  echo "    after : $PAGE_SHA_AFTER"
  exit 2
fi
echo "restored byte-identically:"
echo "   $PAGE_SHA_AFTER"
echo "BITES: $bites/$mutations mutations turned the guard RED"
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
