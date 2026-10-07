#!/usr/bin/env bash
# Bite proof for web/e2e/settings-tabs.spec.ts.
#
# A guard that only ever passes is a tautology, and this file has TWO halves
# that are easy to mistake for one: the three source-side halves its header
# documents (strip class, reveal, focus ring) AND the tab-count wait this card
# rewrote. A wait that no longer bites is as bad as a guard that stops seeing
# the real page, so every case below has to produce a red at a NAMED assertion —
# a red for an unrelated reason (a crash, a failed build) is not a bite.
#
# The page-side cases rebuild the app first: the spec measures the BUILT page,
# and `npm run a11y` alone would judge the previous build.
#
# WHY THIS TAKES THE FAMILY'S LOCK AND A PER-RUN BACKUP (card t_df41e675)
#
# This harness rewrites src/app/settings/page.tsx IN PLACE and `run_case` reads it
# — and web/scripts/biteproof_save_button.sh (which landed on main with PR #1017)
# mutates that SAME file, byte for byte, plus src/components/SaveButton.tsx and
# src/components/i18nDict.ts. Neither took a lock (`grep -c flock` was 0 in both),
# and this file's backup path was the HOST-GLOBAL `/tmp/biteproof-dc06.<name>.bak`,
# so two runs in one worktree overwrote each other's backups AND each other's
# sources: a peer's restore lands under this run's feet mid-case, and this run then
# attributes the peer's bytes, or its own, to the wrong sabotage. Measured on
# biteproof_save_button.sh's pre-fix bytes — two concurrent runs in ONE worktree, 12
# rounds — 9 of 12 rounds printed a FALSE verdict line or left the tree holding a
# peer's bytes, every round still printing "all sources restored byte-identically".
#
# Both halves are required, the same pair card t_d101a943 gave
# biteproof_dev_hosts.sh:
#   1. The backup is a per-run `mktemp -d`, so one run can never restore another
#      run's bytes — that is the half that fixes the CROSS-worktree case.
#   2. The run takes the FAMILY's per-worktree `flock`, keyed on
#      `git rev-parse --absolute-git-dir`, under a HOST-GLOBAL `/tmp` base (never
#      `$TMPDIR`: the run this must exclude usually belongs to another PROFILE, and
#      a per-profile TMPDIR would put the two runs in different directories and
#      serialise nothing). Two lanes' cards therefore never queue behind each other;
#      only two runs that mutate the SAME worktree serialise.
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits the
# open descriptor, so the lock is held for exactly as long as the copy that mutates
# anything lives, and the `trap ... EXIT` releases it as surely as it restores the
# sources. The child PROVES it holds the lock before it prints a verdict. A contended
# run WAITS and says so, a wait-out is exit 75, and a missing `flock` is exit 2
# rather than silently degrading to "no lock".
#
# Run:  bash web/scripts/biteproof_settings_tabs.sh
# Env:  A11Y_PORT (default 3320) — the spec starts its own server there.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the
# child this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                # -> web/

SPEC="${BITEPROOF_SPEC:-e2e/settings-tabs.spec.ts}"
PAGE=src/app/settings/page.tsx
CSS=src/app/globals.css
PLAYWRIGHT=./node_modules/.bin/playwright
export A11Y_PORT="${A11Y_PORT:-3320}"

# ---- PER-WORKTREE LOCK (card t_df41e675) ----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore src/app/settings/page.tsx" >&2
    echo "(shared with web/scripts/biteproof_save_button.sh) under each other's feet, which" >&2
    echo "prints false verdicts (t_df41e675). Install flock before trusting any verdict from it." >&2
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
    echo "          (they mutate the same settings page, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer mutating the same settings page and" >&2
    echo "would either read its mutation or overwrite the page with its own backup." >&2
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
  echo "NOT serialised — a peer mutating the same settings page would give it false verdicts" >&2
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

# The spec to drive. Overridable so a leg of web/src/lib/harnessLock.test.ts can
# reach this harness's own deterministic, node-free refusal AFTER it has taken its
# lock (point this at a path that does not exist), which is how that file proves the
# key is per-worktree without running the suite. Default is the real spec.
[ -f "$SPEC" ] || { echo "harness: $SPEC is missing — nothing to drive" >&2; exit 2; }

# PER-RUN scratch: the backup path used to be the HOST-GLOBAL
# `/tmp/biteproof-dc06.<name>.bak`, so a concurrent run overwrote this run's backups
# and this run's restore then wrote the PEER's bytes back as if they were its own.
SNAP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }

restore() {
  # WHICH SOURCES MAKE THE BUILD STALE (card t_cd035f15). Only `$PAGE` and `$CSS` are
  # BUILD INPUTS — the spec (`$SPEC`) is not read by `next build` at all, which is why
  # case 4 sabotages only the spec and never rebuilds. So reverting a source is what
  # makes a rebuild NECESSARY, and it is necessary only for a build input whose bytes on
  # disk still differ from the clean snapshot. Rebuilding "just in case" on every
  # restore is what made the harness run `next build` TWICE at the end of a normal run:
  # the trailer's rebuild put the clean build back, then the EXIT trap's `restore`
  # rebuilt the same bytes AGAIN, and the second build started after the harness's last
  # printed line — so a caller that read the log and returned saw a worktree whose
  # `.next` was still being rewritten.
  #
  # `cmp` compares the file BEFORE the `cp` below, and it is asked only of the build
  # inputs. A stale one (`build_input_stale=1`) forces the rebuild exactly as before;
  # nothing else does, unless the last build FAILED (`BUILD_OK=0`), in which case this
  # still retries — a failed build is the one case where `.next` is not to be trusted.
  local build_input_stale=0
  for f in "$SPEC" "$PAGE" "$CSS"; do
    if [ -f "$SNAP/$(basename "$f").orig" ]; then
      case "$f" in
        "$PAGE"|"$CSS") cmp -s "$SNAP/$(basename "$f").orig" "$f" || build_input_stale=1 ;;
      esac
      cp "$SNAP/$(basename "$f").orig" "$f"
    fi
  done
  # THE BUILD TOO, NOT ONLY THE SOURCES (card t_22b560cb). Three of the four cases mutate
  # a SOURCE file and then rebuild, so the served bundle matches the mutation. An
  # interrupt (or a `kill`) between the rebuild and the next case left the SABOTAGED
  # build on disk: `git status` is clean, every source hash matches, and yet the running
  # app is the mutated one — the next run measures it, and a lane that only checks the
  # tree concludes the tree is fine. Restoring the sources without rebuilding is a
  # half-restore that reads as a full one.
  #
  # `rebuild` may not exist yet when the trap fires on an early exit, so this checks.
  if declare -F rebuild >/dev/null 2>&1; then
    if [ "$build_input_stale" = 1 ] || [ "${BUILD_OK:-0}" != 1 ]; then
      rebuild >/dev/null 2>&1 || echo "   WARNING: could not rebuild after restore — .next may still be a sabotage build" >&2
    fi
  fi
}
cleanup() { local rc=$?; restore; rm -rf "$SNAP"; exit "$rc"; }
trap cleanup EXIT

fail=0
for f in "$SPEC" "$PAGE" "$CSS"; do
  [ -f "$f" ] || { echo "missing $f — is this the right worktree?" >&2; exit 2; }
  cp "$f" "$SNAP/$(basename "$f").orig"
done
sha_before=""
for f in "$SPEC" "$PAGE" "$CSS"; do sha_before="$sha_before$(sha256sum "$f")
"; done

rebuild() {
  echo "   (next build — the spec measures the BUILT page)"
  # THE LOCK ENV MUST NOT LEAK INTO THE BUILD (card t_df41e675). `npm run build`
  # runs `test:unit`, which runs `web/src/lib/harnessLock.test.ts`, and that guard
  # SPAWNS this family's harnesses. This process holds the family lock and exported
  # `BITEPROOF_LOCKED=1` / `BITEPROOF_LOCK_FILE`, so without this strip every harness
  # the guard spawns inherits them and takes the CHILD path instead of its own lock
  # take — measured: leg A read exit 2 (the harness ran past its lock block to an
  # unrelated refusal) where it requires 75, so the whole `web` gate went red with
  # `biteproof_docs_tool_claims.sh: ... must exit 75 ... Got 2`. `env -u` removes
  # exactly those two variables and nothing else.
  #
  # `BUILD_OK` records whether the LAST build succeeded, so `restore` can tell "the
  # clean build is already on disk" from "no build of this tree has run yet or the
  # last one failed" (card t_cd035f15). It is left unset until a build actually
  # starts, and `restore` treats unset as not-ok — an exit before the first build
  # leaves `.next` unknown, which must still be rebuilt.
  BUILD_OK=0
  if ! env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE npm run build >"$SNAP/build.log" 2>&1; then
    echo "   BUILD FAILED — see $SNAP/build.log"
    return 1
  fi
  BUILD_OK=1
}

# args: <case name> <expected failure text> <rebuild:yes|no> <mutation shell>
#
# THE MATCH IS SCOPED TO THE REPORTER'S Error BLOCK, NOT THE WHOLE OUTPUT
# (card t_dd211ae1; the sibling of the same gap fixed in
# biteproof_forms_bulk_failure.sh). Matching `$expect` anywhere in Playwright's
# output is a false OK generator, for a structural reason: on a failure Playwright
# ECHOES THE SPEC'S SOURCE as a `>` context line, so every string literal in the
# assertion message is printed whatever the test actually died of.
#
# This harness is more exposed than its sibling, not less: its needles are TEMPLATE
# LITERALS in the spec (`the tab strip wrapped to ${m.rows} rows`, `the focus ring
# of ...`) whose constant prefix is ALSO written verbatim in the spec's own comments
# at lines 74, 79 and 106. A comment is echoed by nothing, but the literal below it
# is echoed by any failure in that block — including a timeout, which is the exact
# shape this harness exists to distinguish from a named assertion.
#
# Scoping to the reporter's cause lines keeps the claim that is true: a
# NON-assertion failure prints `Error: locator.innerText: Test timeout of ...`, an
# Error line that does not carry the needle. See the sibling file for why dropping
# the echoed block by line RANGE was rejected instead (its start marker is
# reporter-formatted, and a guard that silently matches nothing is worse than one
# that matches too much).
run_case() {
  local name="$1" expect="$2" do_build="$3" mutate="$4"
  echo "== $name"
  restore
  if ! eval "$mutate"; then
    echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
    fail=1
    restore
    return
  fi
  if [ "$do_build" = yes ] && ! rebuild; then
    fail=1
    restore
    return
  fi
  local out
  out="$($PLAYWRIGHT test "$SPEC" --reporter=list 2>&1)"
  # error_block is the reporter's own cause lines: `Error: ...` plus the
  # `expect(...)` chain that follows. Neither carries the spec's echoed source.
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  # Herestring, not `printf | grep -q`: with `set -o pipefail` a reader that
  # exits at the first match can kill the writer with SIGPIPE (141) and report
  # a MATCH as a failure — load-dependent, and it would look like "not detected".
  if ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
    echo "   NOT DETECTED — the spec stayed green with this defect present"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qF "$expect" <<<"$error_block"; then
    echo "   DETECTED, but NOT at the expected assertion. Wanted: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    grep -E "^ +[0-9]+ failed" <<<"$out" | sort -u | head -1
    grep -F "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
  restore
}

# ---------------------------------------------------------------------------
# The three source-side halves the spec's own header documents (lines 48-57).
# Each must still be 25 failed / 0 passed.
# ---------------------------------------------------------------------------
run_case "1. strip class back to flex flex-wrap" \
  "wrapped to" yes \
  "perl -0pi -e 's/className=\"jf-tabstrip mt-4 flex gap-1 overflow-x-auto border-b border-slate-200\"/className=\"jf-tabstrip mt-4 flex flex-wrap gap-1 border-b border-slate-200\"/' $PAGE && grep -q 'flex flex-wrap gap-1 border-b' $PAGE"

run_case "2. the reveal disabled (RULE 6's wait must not rescue it)" \
  "not even after waiting for the reveal to land" yes \
  "perl -0pi -e 's/el\\.scrollIntoView\\(\\{ block: \\\"nearest\\\", inline: \\\"nearest\\\", behavior: \\\"instant\\\" \\}\\);/if (true) return;/' $PAGE && grep -q 'if (true) return;' $PAGE"

run_case "3. the inset focus-ring rule dropped" \
  "focus ring of" yes \
  "perl -0pi -e 's/\\.jf-tabstrip \\[role=\\\"tab\\\"\\]:focus-visible \\{/.jf-tabstrip-dropped [role=\\\"tab\\\"]:focus-visible {/' $CSS && grep -q 'jf-tabstrip-dropped' $CSS"

# ---------------------------------------------------------------------------
# The tab-count wait this card rewrote. Its whole point is that a session that
# never resolves the admin role fails AT THE WAIT, naming the session, instead
# of surfacing 40 lines later as a TAB_KEYS/label mismatch. So: stub a MEMBER
# bundle (the pre-bundle state the flake measured) against the ADMIN
# expectation and require that named failure.
# ---------------------------------------------------------------------------
run_case "4. a bundle that never resolves the admin role fails at the count wait" \
  "did not resolve the admin role" no \
  "perl -0pi -e 's/slug: \\\"demo\\\", role: \\\"admin\\\"/slug: \\\"demo\\\", role: \\\"member\\\"/' $SPEC && grep -q 'role: \\\"member\\\"' $SPEC && ! grep -q 'role: \\\"admin\\\"' $SPEC"

# ---------------------------------------------------------------------------
# The last case does not rebuild (its sabotage is in the spec, not the page), so
# a run would otherwise leave case 3's CSS-sabotaged build on disk — and the
# `a11y` gate does NOT rebuild, it serves whatever `.next` is already there, so
# the next gate run would judge a build this tree never made. Put the real build
# back before returning.
# ---------------------------------------------------------------------------
echo
echo "== restoring the build (the last case's sabotage must not be left served)"
if rebuild; then
  echo "   build restored"
else
  echo "   BUILD RESTORE FAILED — .next may still be a sabotage build; re-run 'npm run build'"
  fail=1
fi
echo
sha_after=""
for f in "$SPEC" "$PAGE" "$CSS"; do sha_after="$sha_after$(sha256sum "$f")
"; done
if [ "$sha_before" != "$sha_after" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  diff <(echo "$sha_before") <(echo "$sha_after")
  exit 2
fi
echo "restored byte-identically:"
echo "$sha_after" | sed 's/^/   /'
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
