#!/usr/bin/env bash
# Bite proof for the products-trash refusal guard (card t_602c48f5):
#   web/e2e/products-trash-refusal.spec.ts   (the /products Trash refusals)
#
# THE CLAIM is that a refusal the server sends back stays READABLE on the
# products bin. The guard was green-by-construction against the defect before this
# card's fix, because both sites wrote the sentence into the page's LOAD-error state
# and then fired the relist that clears that state on success:
#
#   products/page.tsx   restoreProduct   setError(...);       void loadTrash();   -> setError("")
#   products/page.tsx   purgeProduct     setError(apiErr(…)); void loadTrash();   -> setError("")
#
# Measured on the unfixed shape (real browser, built app, fresh storage, English):
# `p[role="alert"]` sampled every 100ms thirty times was absent at EVERY sample
# while the trash listing went 1 -> 2 calls, so the merchant watched the click do
# nothing.
#
# THE MUTATIONS ARE THE PRE-FIX BYTES, one call site each, with the relist KEPT
# (so a case measures the WIPE, not a deleted feature):
#   1. the purge refusal goes back into the state `loadTrash()` clears;
#   2. the restore refusal goes back there too;
#   3. the restore's CODE routing is reverted (a bare `t("prod.restoreFail")`
#      instead of `apiErr(...)`) — the card's second ask, pinned by its own leg;
#   4. the restore's `catch` is removed again, so a rejected fetch goes silent.
# Each case rebuilds the app first, so the spec measures THIS tree's build rather
# than a leftover one, and the red must land at a NAMED assertion — not at a crash
# or a failed build.
#
# A guard that only ever passes is a tautology, so the baseline is a real case too:
# the unmutated tree must be GREEN (5 passed) or a red below could be the harness
# itself.
#
# Run:  bash web/scripts/biteproof_products_trash_refusal.sh
# Env:  A11Y_PORT (default 3413) — playwright.config.ts's webServer serves there.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE any `cd`, because the child this
# file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# ---- PER-WORKTREE LOCK -----------------------------------------------------
# The family's key, NOT a per-script one: this harness rewrites
# src/app/products/page.tsx and runs playwright against the SAME worktree's build
# and the SAME webServer port as the other biteproof harnesses, which rewrite
# their own pages and rebuild the same .next — they all mutate the same tree, so
# they must all serialise on the same file (see web/src/lib/harnessLock.test.ts).
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
PAGE=${BITEPROOF_PAGE:-src/app/products/page.tsx}
SPEC=${BITEPROOF_SPEC:-e2e/products-trash-refusal.spec.ts}
PLAYWRIGHT=./node_modules/.bin/playwright
# The lock vars must not reach anything this harness SPAWNS: a leaked
# LOCKED/LOCK_FILE/LOCK_DIR makes each spawned harness contend on the lock THIS
# script already holds (card t_df41e675).
nolock() { env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR "$@"; }
export A11Y_PORT="${A11Y_PORT:-3413}"
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
    # An EXACT count, not "some passed": the unmutated tree has 5 tests in this
    # spec. A spec whose tests were deleted would print a smaller number, and that
    # must not read as a green control.
    if grep -qE "^ +5 passed" <<<"$out" && ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
      grep -E "^ +5 passed" <<<"$out" | sort -u | head -1
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

echo
echo "== baseline: the fix in place, this tree built"
restore
rebuild || exit 2
run_case "0. the fixed tree passes the guard (green control)" pass "" "$SPEC"

echo
echo "== 1. the PURGE PRE-FIX shape: the refusal back in the state the relist clears"
# The exact pre-fix call site, with the relist KEPT (so the mutation is the
# mechanism of the wipe, not a deleted feature). Fails closed if the source no
# longer matches: the mutation must APPLY or the case proves nothing.
mutations=$((mutations + 1))
if grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.purgeFail"\), t\)\);' "$PAGE" \
  && perl -0pi -e 's/setTrashErr\(apiErr\(d, t\("prod\.purgeFail"\), t\)\);/setError(apiErr(d, t("prod.purgeFail"), t));/' "$PAGE" \
  && grep -qE 'setError\(apiErr\(d, t\("prod\.purgeFail"\), t\)\);' "$PAGE" \
  && ! grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.purgeFail"\), t\)\);' "$PAGE"; then
  if rebuild; then
    run_case "1. a refused purge is wiped by its own relist" red \
      "the purge refusal must render a message at all|the purge refusal must survive the whole 3s window" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== 2. the RESTORE PRE-FIX shape: the refusal back in the state the relist clears"
# Only the RESTORE is needed before a mutation: the run below measures the build
# made AFTER the mutation, so a build of the restored tree here would be wasted.
# (Every case therefore costs ONE build, not two.)
restore
mutations=$((mutations + 1))
if grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);' "$PAGE" \
  && perl -0pi -e 's/setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);/setError(apiErr(d, t("prod.restoreFail"), t));/' "$PAGE" \
  && grep -qE 'setError\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);' "$PAGE" \
  && ! grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);' "$PAGE"; then
  if rebuild; then
    run_case "2. a refused restore is wiped by its own relist" red \
      "the restore refusal must render a message at all|the restore refusal must survive the whole 3s window" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== 3. the RESTORE CODE ROUTING reverted: the code is thrown away again"
restore
mutations=$((mutations + 1))
if grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);' "$PAGE" \
  && perl -0pi -e 's/setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);/setTrashErr(t("prod.restoreFail"));/' "$PAGE" \
  && grep -qE 'setTrashErr\(t\("prod\.restoreFail"\)\);' "$PAGE" \
  && ! grep -qE 'setTrashErr\(apiErr\(d, t\("prod\.restoreFail"\), t\)\);' "$PAGE"; then
  if rebuild; then
    run_case "3. a coded restore refusal is not translated" red \
      "a coded restore refusal must render the dictionary sentence for its code" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== 4. the RESTORE CATCH removed: a rejected fetch goes silent again"
restore
mutations=$((mutations + 1))
# The whole `let res: Response; try { … } catch { … }` prelude is replaced by the
# pre-fix bare fetch, so the rejection escapes as an unhandled rejection again.
if perl -0pi -e 's/    let res: Response;\n    try \{\n      res = await fetch\(`\/api\/rest\/products\/\$\{id\}\/restore\$\{workspaceQuery\(\)\}`\, \{ method: "POST" \}\);\n    \} catch \{\n(?:.*\n)*?      setTrashErr\(t\("prod\.restoreFail"\)\);\n      return;\n    \}\n/    const res = await fetch(`\/api\/rest\/products\/\$\{id\}\/restore\$\{workspaceQuery\(\)\}`, { method: "POST" });\n/' "$PAGE" \
  && grep -qE 'const res = await fetch\(`/api/rest/products/\$\{id\}/restore' "$PAGE" \
  && ! grep -qE 'let res: Response;' "$PAGE"; then
  if rebuild; then
    run_case "4. a rejected restore fails silently" red \
      "a rejected restore must not fail silently" \
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
