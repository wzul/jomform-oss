#!/usr/bin/env bash
# Bite proof for web/src/lib/saveButton.test.ts.
#
# Each case SABOTAGES one property the guard pins, runs the guard, and asserts it
# goes RED. Every mutated file is restored byte-identically (sha256 re-checked at
# the end).
#
# WHY THIS TAKES THE FAMILY'S LOCK (card t_df41e675)
#
# This file mutates THREE shared sources IN PLACE — src/components/SaveButton.tsx,
# src/app/settings/page.tsx and src/components/i18nDict.ts — and the run reads them
# throughout every case. A sibling harness on main, web/scripts/biteproof_settings_tabs.sh,
# mutates the SAME settings/page.tsx byte for byte. It shipped with no lock at all
# (`grep -c flock` was 0), and the per-run `mktemp -d` below fixed only the BACKUP
# path: the RESTORE still writes the shared file in place, so a peer's restore lands
# under this run's feet mid-case and this run then attributes the peer's bytes to its
# own sabotage.
#
# Measured here on the pre-fix bytes — two concurrent runs of THIS script in ONE
# worktree, 12 rounds — 9 of 12 rounds printed a FALSE verdict line (`DID NOT BITE —
# missing dict key`, `— forked base class`, `— false return ignored`, `— rejection
# treated as success`, `— no unmount cleanup`, `— no success boolean from the
# handler`) or left the tree dirty (`src/components/i18nDict.ts NOT restored`,
# `src/components/SaveButton.tsx NOT restored`), while EVERY round still printed
# `all sources restored byte-identically`. That is the harm exactly: the harness
# reports its OWN sabotage as un-bitten because the peer restored the file first,
# and it reports a restore it did not make.
#
# The lock is the FAMILY's per-worktree `flock`, keyed on
# `git rev-parse --absolute-git-dir`, under a HOST-GLOBAL `/tmp` base — never
# `$TMPDIR`, because the run this must exclude usually belongs to another PROFILE
# and a per-profile TMPDIR would put the two runs in different directories and
# serialise nothing. Two lanes' cards never queue behind each other; only two runs
# in the SAME worktree serialise.
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits the
# open descriptor, so the lock is held for exactly as long as the copy that mutates
# anything lives, and the `trap ... EXIT` below releases it as surely as it restores
# the sources — there is no separate unlock line that can be skipped. The child
# PROVES it holds the lock before it prints a verdict (a lock nobody can observe as
# held is the same "trust the absence" shape this family's verdict rule exists to
# remove): a second take on the same file from a separate process must be REFUSED,
# and a fresh file in the same directory must lock FINE — or "refused" would mean
# flock is inert here, not that this run holds the lock.
#
# A contended run WAITS and says so rather than failing — a refusal would turn
# "someone else is running it" into a red harness — and a wait-out is exit 75.
# `BITEPROOF_LOCK_WAIT=0` is how a caller asks for the refusal instead of the wait,
# and no `flock` on PATH is exit 2 rather than a silent degradation to "no lock" (a
# decorative lock is the same defect with a green light on it).
#
# Every `grep -q` that reads a captured guard output uses a HERE-STRING, never
# `printf '%s' "$out" | grep -q`: under this file's `set -uo pipefail` a MATCH can
# still report the pipeline as failed (`grep -q` exits at the first hit, `printf`
# takes SIGPIPE, `pipefail` propagates 141), so the `&&` chain short-circuits and the
# case prints a false `DID NOT BITE` while the counts are right. Failure direction is
# false-RED only, never a false PASS.
#
# Run:  bash web/scripts/biteproof_save_button.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the
# child this file re-execs under the lock needs a path that survives a changed CWD —
# a relative `$0` resolved after the `cd` names a file that is not there.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                # -> web/
export PATH="/root/.hermes/tools/node-26.7.0-linux-x64/bin:${PATH}"

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

# The guard to drive. Overridable so a leg of web/src/lib/harnessLock.test.ts can
# reach this harness's own deterministic, node-free refusal AFTER it has taken its
# lock (point this at a path that does not exist), which is how that file proves the
# key is per-worktree without running the suite. Default is the real guard.
GUARD="${BITEPROOF_GUARD:-src/lib/saveButton.test.ts}"
[ -f "$GUARD" ] || { echo "harness: $GUARD is missing — nothing to drive" >&2; exit 2; }

TMP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }

declare -A FILES=(
  [savebtn]="src/components/SaveButton.tsx"
  [settings]="src/app/settings/page.tsx"
  [dict]="src/components/i18nDict.ts"
)
declare -A SUM
for k in "${!FILES[@]}"; do
  [ -f "${FILES[$k]}" ] || { echo "missing ${FILES[$k]} — is this the right worktree?" >&2; exit 2; }
  cp "${FILES[$k]}" "$TMP/$k.orig"
  SUM[$k]=$(sha256sum "${FILES[$k]}" | cut -d' ' -f1)
done
restore_all() { for k in "${!FILES[@]}"; do cp "$TMP/$k.orig" "${FILES[$k]}"; done; }
trap 'restore_all; rm -rf "$TMP"' EXIT

run_guard() { node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1; }
fail=0
bites=0
expect_red() {
  # HERE-STRING, never `printf '%s' "$2" | grep -q`: under `set -o pipefail` a MATCH
  # can still report the pipeline as failed (SIGPIPE 141) and print a false
  # `DID NOT BITE`. See the header for the measurement.
  if grep -qE '^ℹ fail [1-9]' <<<"$2"; then echo "  ✅ BITES — $1"; bites=$((bites+1));
  else echo "  ❌ DID NOT BITE — $1 (the guard is not protecting this)"; fail=1; fi
}

echo "case 0: baseline (unsabotaged must be GREEN)"
out=$(run_guard); grep -E '^ℹ (pass|fail)' <<<"$out"
if grep -qE '^ℹ fail 0' <<<"$out"; then echo "  ✅ baseline green"; bites=$((bites+1));
else echo "  ❌ baseline not green"; fail=1; fi

echo "case 1: the landing button is unwired (THE REPORTED SYMPTOM)"
python3 - "${FILES[settings]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('onSave={saveLandingPage}','onClick={saveLandingPage}')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "landing save bypasses SaveButton" "$out"; restore_all

echo "case 2: a FAILED save confirms anyway (the false return ignored)"
python3 - "${FILES[savebtn]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('if ((await onSave()) === false) confirmed = false;','await onSave();')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "false return ignored" "$out"; restore_all

echo "case 3: a thrown error confirms anyway"
python3 - "${FILES[savebtn]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('      confirmed = false;\n    } finally {','    } finally {')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "rejection treated as success" "$out"; restore_all

echo "case 4: the handler stops reporting the outcome"
python3 - "${FILES[settings]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('async function saveLandingPage(): Promise<boolean> {','async function saveLandingPage() {')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "no success boolean from the handler" "$out"; restore_all

echo "case 5: the button uses the LONG label (widens mid-press)"
python3 - "${FILES[settings]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('savedLabel={t("set.lpSavedShort")}','savedLabel={t("set.lpSaved")}')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "long saved label on the button" "$out"; restore_all

echo "case 6: the dictionary key vanishes (t() would render the raw key)"
python3 - "${FILES[dict]}" <<'PY'
import sys, re
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=re.sub(r'\n  "set\.lpSavedShort": \{[^}]*\},', '', s)
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "missing dict key" "$out"; restore_all

echo "case 7: a stale timer can revert a newer label (clear-before-arm removed)"
python3 - "${FILES[savebtn]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('''    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
''','')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "no clear-before-re-arm" "$out"; restore_all

echo "case 8: the unmount cleanup is removed"
python3 - "${FILES[savebtn]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('''      if (timer.current !== null) window.clearTimeout(timer.current);''','')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "no unmount cleanup" "$out"; restore_all

echo "case 9: the saved state forks the base class"
python3 - "${FILES[savebtn]}" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding="utf-8").read()
s=s.replace('savedClassName = "jf-btn-primary jf-btn-saved"','savedClassName = "jf-btn-saved"')
open(p,"w",encoding="utf-8").write(s)
PY
out=$(run_guard); expect_red "forked base class" "$out"; restore_all

echo
bad=0
for k in "${!FILES[@]}"; do
  now=$(sha256sum "${FILES[$k]}" | cut -d' ' -f1)
  [ "$now" = "${SUM[$k]}" ] || { echo "❌ ${FILES[$k]} NOT restored"; bad=1; }
done
[ $bad -eq 0 ] && echo "all sources restored byte-identically" || fail=1
echo "BITES: ${bites}/10 sabotages turned the guard RED"
echo "RESULT: $([ $fail -eq 0 ] && echo 'ALL CASES BIT — exit 0' || echo 'A CASE DID NOT BITE — exit 1')"
exit $fail
