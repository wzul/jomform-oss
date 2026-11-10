#!/usr/bin/env bash
# Bite proof for the refusal-survival guards (card t_ab4aa85b):
#   web/e2e/forms-trash-refusal.spec.ts   (the /forms Trash refusals)
#   web/e2e/workspace-trash.spec.ts       (the Danger zone's DELETE refusal)
#
# THE CLAIM is that a refusal the server sends back stays READABLE. Both guards
# were green-by-construction against the defect before this card's fix, because
# both sites wrote the sentence into the page's LOAD-error state and then fired a
# reload that clears that state on success:
#
#   forms/page.tsx      setErr(apiErr(...)); void loadTrash();   -> setErr("") on success
#   WorkspaceTab.tsx    setErr(apiErr(...)); await load();       -> setErr("") on success
#
# Measured on the unfixed shape: the sentence is never observable (`role=alert`
# sampled every 250ms from ~0.25s to ~3s: `[]` at every reading), so the merchant
# watched the click do nothing.
#
# WHY THIS HARNESS EXISTS AT ALL. The /forms guard used to HANG its second trash
# listing so the sentence could be observed at all — a stub that hides the defect
# cannot pin its absence, and the spec's own comment said the trick had to be
# revisited once the real fix landed. Since the fix the listing resolves
# ordinarily; this harness restores the WIPE and requires the guard to catch it.
#
# THE MUTATIONS ARE THE PRE-FIX BYTES, one call site each: the refusal goes back
# into the state the reload clears, with the reload kept (so the mutation is the
# mechanism, not a deletion of the feature). Each page-side case rebuilds the app
# first, so the spec measures THIS tree's build rather than a leftover one, and
# the red must land at a NAMED assertion — not at a crash or a failed build.
#
# A guard that only ever passes is a tautology, so the baseline is a real case
# too: the unmutated tree must be GREEN (11 passed) or a red below could be the
# harness itself. The count is 5 in the forms spec (the two refusals, the two
# transport legs, and t_d9dda884's failed-relist leg) and 6 in the workspace spec
# (the two trash legs, the refusal-survival leg, t_fe8a5d78's
# dialog-usable-again leg and the two failed-refetch legs card t_8030247d added).
#
# Run:  bash web/scripts/biteproof_forms_trash_refusal.sh
# Env:  A11Y_PORT (default 3413) — playwright.config.ts's webServer serves there.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE any `cd`, because the child
# this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# ---- PER-WORKTREE LOCK -----------------------------------------------------
# The family's key, NOT a per-script one: this harness rewrites
# src/app/forms/page.tsx, which biteproof_forms_bulk_failure.sh mutates too, and
# biteproof_dev_hosts.sh rewrites next.config.mjs. They all mutate the same tree,
# so they must all serialise on the same file (see web/src/lib/harnessLock.test.ts).
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore src/app/forms/page.tsx and" >&2
    echo "src/components/WorkspaceTab.tsx under each other's feet, which prints false verdicts." >&2
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
    echo "          (they mutate the same shared sources, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer mutating the same shared sources." >&2
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
# The child. Refuse if the lock it is supposed to have inherited cannot be shown
# to be held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same shared sources would give it false verdicts." >&2
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

# The two mutated sources and the two guards. Both overridable so a probe can
# reach this harness's own node-free refusal (harnessLock.test.ts leg D3).
PAGE=${BITEPROOF_PAGE:-src/app/forms/page.tsx}
WS_PAGE=${BITEPROOF_WS_PAGE:-src/components/WorkspaceTab.tsx}
SPEC=${BITEPROOF_SPEC:-e2e/forms-trash-refusal.spec.ts}
WS_SPEC=${BITEPROOF_WS_SPEC:-e2e/workspace-trash.spec.ts}
PLAYWRIGHT=./node_modules/.bin/playwright
# The lock vars must not reach anything this harness SPAWNS: a leaked
# LOCKED/LOCK_FILE/LOCK_DIR makes each spawned harness contend on the lock THIS
# script already holds (card t_df41e675).
nolock() { env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR "$@"; }
export A11Y_PORT="${A11Y_PORT:-3413}"
TMP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }

# A per-run backup per FILE, keyed by the full path so two `page.tsx`-shaped
# basenames cannot overwrite each other's backup.
bak_of() { printf '%s/%s.bak' "$TMP" "$(printf '%s' "$1" | tr '/' '_')"; }
restore() {
  local f
  for f in "$PAGE" "$WS_PAGE"; do
    [ -f "$(bak_of "$f")" ] && cp "$(bak_of "$f")" "$f"
  done
}
trap 'restore; rm -rf "$TMP"' EXIT

for f in "$PAGE" "$WS_PAGE" "$SPEC" "$WS_SPEC"; do
  [ -f "$f" ] || { echo "BITEPROOF REFUSED: $f is missing — nothing to mutate." >&2; exit 2; }
done
cp "$PAGE" "$(bak_of "$PAGE")" || exit 2
cp "$WS_PAGE" "$(bak_of "$WS_PAGE")" || exit 2
PAGE_SHA_BEFORE="$(sha256sum "$PAGE")"
WS_PAGE_SHA_BEFORE="$(sha256sum "$WS_PAGE")"

fail=0

rebuild() {
  echo "   (next build — the specs measure the BUILT page)"
  if ! nolock npm run build >"$TMP/build.log" 2>&1; then
    echo "   BUILD FAILED — see $TMP/build.log"
    return 1
  fi
}

# args: <case name> <expect: pass|red> <named assertion (ERE alternation)> <specs...>
#
# THE MATCH IS SCOPED TO THE REPORTER'S Error BLOCK, NOT THE WHOLE OUTPUT (card
# t_dd211ae1). Playwright ECHOES the spec's source on a failed assertion, so every
# string literal in the assertion message — the named thing itself — is printed
# whatever the test actually died of; matching the whole output would report "red
# at the named assertion" for a case that never reached one.
run_case() {
  local name="$1" want="$2" expect="$3"
  shift 3
  echo "== $name"
  local out
  out="$(nolock "$PLAYWRIGHT" test "$@" --project=feature-core --reporter=list 2>&1)"
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  if [ "$want" = pass ]; then
    # An EXACT count, not "some passed": the unmutated tree has 5 tests in the
    # forms spec (t_d9dda884 added the failed-relist leg) and 6 in the workspace
    # spec (t_fe8a5d78 added the dialog-usable-again leg, and t_8030247d the two
    # failed-refetch legs). A spec whose tests were deleted would print a smaller
    # number, and that must not read as a green control.
    if grep -qE "^ +11 passed" <<<"$out" && ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
      grep -E "^ +11 passed" <<<"$out" | sort -u | head -1
      echo "   OK (green control: the unmutated tree really passes these specs)"
    else
      echo "   BASELINE NOT GREEN — the specs are red on a tree with no defect, so no"
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
    grep -E "^ +[0-9]+ failed" <<<"$out" | sort -u | head -1
    grep -E "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
}

echo
echo "== baseline: the fix in place, this tree built"
restore
rebuild || exit 2
run_case "0. the fixed tree passes both guards (green control)" pass "" "$SPEC" "$WS_SPEC"

echo
echo "== 1. the /forms PRE-FIX shape: the purge refusal back in the state the relist clears"
# The exact pre-fix call site, with the relist KEPT (so the mutation is the
# mechanism of the wipe, not a deleted feature). Fails closed if the source no
# longer matches: the mutation must APPLY or the case proves nothing.
if grep -qE 'setTrashErr\(apiErr\(d, t\("forms\.purgeFail"\), t\)\);' "$PAGE" \
  && perl -0pi -e 's/setTrashErr\(apiErr\(d, t\("forms\.purgeFail"\), t\)\);/setErr(apiErr(d, t("forms.purgeFail"), t));/' "$PAGE" \
  && grep -qE 'setErr\(apiErr\(d, t\("forms\.purgeFail"\), t\)\);' "$PAGE" \
  && ! grep -qE 'setTrashErr\(apiErr\(d, t\("forms\.purgeFail"\), t\)\);' "$PAGE"; then
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
echo "== 2. the WorkspaceTab PRE-FIX shape: the delete refusal back in the state load() clears"
restore
rebuild || exit 2
if grep -qE 'setDeleteErr\(apiErr\(d, t\("ws\.deleteFail"\), t\)\);' "$WS_PAGE" \
  && perl -0pi -e 's/setDeleteErr\(apiErr\(d, t\("ws\.deleteFail"\), t\)\);/setErr(apiErr(d, t("ws.deleteFail"), t));/' "$WS_PAGE" \
  && grep -qE 'setErr\(apiErr\(d, t\("ws\.deleteFail"\), t\)\);' "$WS_PAGE" \
  && ! grep -qE 'setDeleteErr\(apiErr\(d, t\("ws\.deleteFail"\), t\)\);' "$WS_PAGE"; then
  if rebuild; then
    run_case "2. a refused workspace delete is wiped by its own reload" red \
      "the delete refusal must render a message at all|the delete refusal must survive the whole 3s window" \
      "$WS_SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== 3. the t_d9dda884 PRE-FIX shape: the failed relist reports into the page's load chrome"
restore
rebuild || exit 2
# The exact pre-fix failure branch of `loadTrash()`, with the relist KEPT: the
# listing failure goes back into `err`, whose render branch returns early and
# replaces the whole page — the refusal the merchant was just shown included.
if grep -qE 'setTrashLoadErr\(r\.error\);' "$PAGE" \
  && perl -0pi -e 's/setTrashLoadErr\(r\.error\);/setTrashed([]);\n      setErr(r.error);/' "$PAGE" \
  && ! grep -qE 'setTrashLoadErr\(r\.error\);' "$PAGE" \
  && grep -qE 'setTrashed\(\[\]\);' "$PAGE"; then
  if rebuild; then
    run_case "3. a refusal whose relist FAILS is replaced by the load-failure page" red \
      "the refusal must stay readable when its own relist fails|the refusal sentence must be readable|the failed relist must not replace the page|the rows must not be blanked by a failed relist|the restore refusal across a FAILED relist" \
      "$SPEC"
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== 4. the t_fe8a5d78 PRE-FIX shape: busy is never cleared on a REFUSAL"
restore
rebuild || exit 2
# The two `setBusy(false)` sites in this file are the refusal branch (this card's
# fix, immediately before `await load()`) and the `catch` (a TRANSPORT failure).
# Remove ONLY the refusal one, so a refused DELETE leaves `busy` true forever:
# the dialog closes with the reason readable, and reopening it from the Danger
# zone's own button (not gated on busy) yields a dialog whose slug input and
# Cancel are disabled, whose confirm button reads `common.saving`, and whose
# overlay ignores clicks. The catch keeps its own reset — the mutation is the
# missing reset on the REFUSAL path, not "busy never clears at all".
BUSY_BEFORE=$(grep -c 'setBusy(false);' "$WS_PAGE")
if [ "$BUSY_BEFORE" = 2 ] \
  && perl -0pi -e 's/\n[ \t]*setBusy\(false\);\n([ \t]*await load\(\);)/\n$1/' "$WS_PAGE" \
  && [ "$(grep -c 'setBusy(false);' "$WS_PAGE")" = 1 ]; then
  if rebuild; then
    run_case "4. a refused delete leaves the dialog stuck on Working…" red \
      "the slug input must be enabled again|Cancel must be enabled again|the confirm button must offer the action again|toBeEnabled" \
      "$WS_SPEC"
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
WS_PAGE_SHA_AFTER="$(sha256sum "$WS_PAGE")"
if [ "$PAGE_SHA_BEFORE" != "$PAGE_SHA_AFTER" ] || [ "$WS_PAGE_SHA_BEFORE" != "$WS_PAGE_SHA_AFTER" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  echo "  $PAGE"
  echo "    before: $PAGE_SHA_BEFORE"
  echo "    after : $PAGE_SHA_AFTER"
  echo "  $WS_PAGE"
  echo "    before: $WS_PAGE_SHA_BEFORE"
  echo "    after : $WS_PAGE_SHA_AFTER"
  exit 2
fi
echo "restored byte-identically:"
echo "   $PAGE_SHA_AFTER"
echo "   $WS_PAGE_SHA_AFTER"
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
