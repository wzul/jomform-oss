#!/usr/bin/env bash
# Bite proof for web/e2e/forms-bulk-failure.spec.ts (card t_c9e37f49).
#
# THE CARD'S CLAIM is that a refused BULK publish on /forms leaves its sentence on
# screen long enough to read. The pre-fix shape did not: `load()` sat in the
# `finally`, so the `return` on both failure branches still triggered a reload,
# and `load()` clears `err` on success — the sentence was replaced ~200ms after it
# was set. This harness re-creates exactly that shape and requires the spec to go
# RED, at a NAMED assertion, with the whole suite rebuilt first so the spec
# measures THIS tree's build and not a leftover one.
#
# A guard that only ever passes is a tautology, so the baseline is a real case
# too, not a preamble: the unmutated tree must be GREEN (1 passed). Without it a
# red in case 2 could be the harness itself.
#
# Run:  bash web/scripts/biteproof_forms_bulk_failure.sh
# Env:  A11Y_PORT (default 3399) — playwright.config.ts's webServer serves there.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE any `cd`, because the child
# this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# ---- PER-WORKTREE LOCK (card t_df41e675) ----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore src/components/SaveButton.tsx," >&2
    echo "src/app/settings/page.tsx (shared with biteproof_settings_tabs.sh) and" >&2
    echo "src/components/i18nDict.ts under each other's feet, which prints false verdicts" >&2
    echo "(t_df41e675, reproduced: 9 of 12 rounds). Install flock before trusting any" >&2
    echo "verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised pair of runs prints false verdicts (t_df41e675)." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR, so two runs in" >&2
    echo "one worktree could not be serialised. Set BITEPROOF_LOCK_DIR to a writable dir." >&2
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
    echo "so this run is NOT serialised against a peer mutating the same shared sources and" >&2
    echo "would either read its mutation or overwrite a source with its own backup." >&2
    echo "Nothing was mutated. Re-run once the other run finishes, or raise the wait with" >&2
    echo "BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held" >&2
    echo "and this run would not be serialised against a peer in the same worktree." >&2
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
  echo "NOT serialised — a peer mutating the same shared sources would give it false verdicts" >&2
  echo "and could be left restoring this run's bytes. Unset BITEPROOF_LOCKED and let the" >&2
  echo "harness take the lock itself." >&2
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

SPEC=${BITEPROOF_SPEC:-e2e/forms-bulk-failure.spec.ts}
PAGE=${BITEPROOF_PAGE:-src/app/forms/page.tsx}
PLAYWRIGHT=./node_modules/.bin/playwright
# The lock vars must not reach anything this harness SPAWNS: harnessLock.test.ts
# runs the family harnesses, and a leaked LOCKED/LOCK_FILE/LOCK_DIR makes each
# spawned one contend on the lock THIS script already holds (card t_df41e675).
nolock() { env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR "$@"; }
export A11Y_PORT="${A11Y_PORT:-3399}"
# PER-RUN, and created HERE — in the child, after the lock — not on the parent path
# before it (card t_4a599d51). On the parent path the `mktemp -d` was never
# referenced again: the parent took the lock, re-exec'd the child (which created,
# used and removed its OWN scratch dir) and exited, so EVERY invocation leaked one
# directory into $TMPDIR — and a concurrency battery watching for a dirty tree read
# that leftover as corruption. Every other locked harness of the family already
# creates its per-run temp on the child path; this is the same shape.
TMP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }
BAK="$TMP/$(basename "$PAGE").bak"

restore() {
  [ -f "$BAK" ] && cp "$BAK" "$PAGE"
}
trap 'restore; rm -rf "$TMP"' EXIT

[ -f "$PAGE" ] || { echo "BITEPROOF REFUSED: $PAGE is missing — nothing to mutate." >&2; exit 2; }
cp "$PAGE" "$BAK" || exit 2
sha_before="$(sha256sum "$PAGE")"

fail=0

rebuild() {
  echo "   (next build — the spec measures the BUILT page)"
  if ! env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR npm run build >$TMP/build.log 2>&1; then
    echo "   BUILD FAILED — see $TMP/build.log"
    return 1
  fi
}

# args: <case name> <expect: pass|red> <named assertion (may be an ERE alternation)>
#
# THE MATCH IS SCOPED TO THE REPORTER'S Error BLOCK, NOT THE WHOLE OUTPUT
# (card t_dd211ae1). Matching `$expect` anywhere in Playwright's output is a false
# OK generator, and the reason is structural rather than a slip: when an assertion
# fails, Playwright ECHOES THE SPEC'S SOURCE as a `>` context line, so every string
# literal in the assertion message — the named thing itself — is printed whatever
# the test actually died of. Measured on this tree: `timeout` on the visible wait
# prints
#
#       Error: the bulk refusal must render a message at all
#       expect(locator).toBeVisible() failed
#       Locator: ...
#       Expected: visible / Timeout: 20000ms / Error: element(s) not found
#       ...
#         165 |   await expect(alert, "the bulk refusal must render a message at all")...
#       > 165 |   await expect(...)...
#
# — the label appears BOTH as the failure's own cause (the `Error:` line) and as the
# echoed source. So a timeout satisfies an unscoped search, and the harness reports
# "red at the named assertion" for a case that never reached one. That is exactly
# the property the BITE-HARNESSES.md row claims ("the red must land at a NAMED
# assertion … not at a crash or a failed build").
#
# Scoping it to `^ +Error:` keeps the claim that is true and drops the one that is
# not: a NON-assertion failure prints `Error: locator.innerText: Test timeout of
# 120000ms exceeded` — an Error line that does not carry the label — so the check
# still reds. The alternative considered and REJECTED: dropping the echoed block by
# line range. Its start marker is reporter-formatted (`- 1) [project] › file ›
# title`) and would silently match nothing on a reporter change, turning the guard
# off rather than making it wrong — the worse failure. A `grep` that matches text
# from the wrong region degrades loudly; one that matches nothing at all degrades
# silently.
run_case() {
  local name="$1" want="$2" expect="$3"
  echo "== $name"
  local out
  out="$(nolock "$PLAYWRIGHT" test "$SPEC" --reporter=list 2>&1)"
  # error_block is the reporter's own cause lines for this run: `Error: ...` under
  # the `1)` failure header, plus the `expect(...)` chain that follows it. Both
  # carry the real reason and neither carries the spec's echoed source.
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  if [ "$want" = pass ]; then
    # Herestring, not `printf | grep -q`: with `set -o pipefail` a reader that
    # exits at the first match can kill the writer with SIGPIPE (141) and report
    # a MATCH as a failure.
    if grep -qE "^ +1 passed" <<<"$out"; then
      grep -E "^ +1 passed" <<<"$out" | sort -u | head -1
      echo "   OK (green control: the unmutated tree really passes this spec)"
    else
      echo "   BASELINE NOT GREEN — the spec is red on a tree with no defect, so no"
      echo "   red below could be attributed to a mutation:"
      grep -E "passed|failed|^ *Error: " <<<"$out" | sort -u | head -8
      fail=1
    fi
    return
  fi
  if ! grep -qE "^ +1 failed" <<<"$out"; then
    echo "   NOT DETECTED — the spec stayed green with the pre-fix shape present"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qE "$expect" <<<"$error_block"; then
    echo "   DETECTED, but NOT at a named assertion. Wanted one of: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    grep -E "^ +1 failed" <<<"$out" | sort -u | head -1
    grep -E "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
}

echo
echo "== baseline: the fix in place, this tree built"
restore
rebuild || exit 2
run_case "0. the fixed tree passes (green control)" pass ""

echo
echo "== 1. the PRE-FIX shape: load() back in the finally"
# The exact pre-fix bytes: the success path keeps only `setErr("")`, the reload
# moves into the `finally` beside setBusy(false) — so a failure `return` reloads
# and the reload clears the sentence. Fails closed if the source no longer
# matches (the mutation must APPLY, or the case proves nothing).
if perl -0pi -e 's/\n      setErr\(""\);\n      load\(\);\n    \} finally \{\n(.*?)\n      setBusy\(false\);\n    \}/\n      setErr("");\n    } finally {\n$1\n      setBusy(false);\n      load();\n    }/s' "$PAGE" \
  && grep -q 'setBusy(false);' "$PAGE" && grep -qE '^      load\(\);$' "$PAGE" \
  && ! perl -0ne 'exit 1 if /setErr\(""\);\n      load\(\);/' "$PAGE"; then
  if rebuild; then
    run_case "1. a refused batch reloads and wipes the sentence" red \
      'the bulk refusal must render a message at all|the refusal must survive the whole 3s window|a refused batch must not reload the list'
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
  echo "   BUILD RESTORE FAILED — .next may still be a sabotage build; re-run 'env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR npm run build'"
  fail=1
fi

echo
sha_after="$(sha256sum "$PAGE")"
if [ "$sha_before" != "$sha_after" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  echo "  before: $sha_before"
  echo "  after : $sha_after"
  exit 2
fi
echo "restored byte-identically:"
echo "   $sha_after"
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
