#!/usr/bin/env bash
# Bite proof for the operator-identity guard (src/lib/operatorNotice.test.ts).
#
# Sabotages ONE thing at a time, runs the guard, and expects RED each time.
# Every case verifies (a) the sabotage is really in the file, (b) which tests
# failed, (c) that the restore really happened before the next case. The
# baseline commit must be the fix itself — this restores with `git checkout`,
# so an uncommitted change would be destroyed. It writes nothing to the repo.
#
# WHY THIS TAKES THE FAMILY'S LOCK (card t_76a02798)
#
# This harness rewrites src/components/i18nDict.ts IN PLACE and its guard reads
# that file in cases 5 and 7 — and i18nDict.ts is a SHARED source the docs-bite
# family mutates too: biteproof_docs_prose_convention.sh (its CASE 17/18) and
# biteproof_save_button.sh (its case 6). It shipped with no lock at all
# (`grep -c flock` was 0), so a concurrent run in the SAME worktree restores its
# own snapshot of i18nDict.ts under this run's feet, between this run's sabotage
# and the guard read that is supposed to see it. The guard then reads a dict that
# no longer holds the sabotage and this harness prints
# `*** GREEN — THE GUARD DID NOT BITE ***` for a sabotage that was really there.
#
# Measured pre-fix on these bytes: operator-bite.sh run concurrently with
# biteproof_save_button.sh in ONE worktree, 10 rounds — 13 FALSE
# `*** GREEN — THE GUARD DID NOT BITE ***` lines, all of them the two i18nDict.ts
# cases (7x "Arabic left English-only", 6x "Tamil loses the legal name"), against
# 0 in a solo run, while every round still printed `final tracked state: (clean)`
# and exited 0. The peer was damaged in the same rounds: 10/10 printed a false
# `❌ DID NOT BITE` and `BITES: n/11`, because this harness's own restore landed
# mid-peer-case. Both directions are the same defect and the same fix.
#
# The lock is the FAMILY's per-worktree `flock`, keyed on
# `git rev-parse --absolute-git-dir`, under a HOST-GLOBAL `/tmp` base — never
# `$TMPDIR`, because the run this must exclude usually belongs to another PROFILE
# and a per-profile TMPDIR would put the two runs in different directories and
# serialise nothing. Two lanes' cards never queue behind each other; only two runs
# in the SAME worktree serialise.
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits
# the open descriptor, so the lock is held for exactly as long as the copy that
# mutates anything lives. The child PROVES it holds the lock before it prints a
# verdict. A contended run WAITS and says so rather than failing — a refusal would
# turn "someone else is running it" into a red harness — and a wait-out is exit 75.
# `BITEPROOF_LOCK_WAIT=0` is how a caller asks for the refusal instead of the wait,
# and no `flock` on PATH is exit 2 rather than a silent degradation to "no lock".
#
# Run:  bash web/scripts/operator-bite.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the
# child this file re-execs under the lock needs a path that survives a changed CWD
# — a relative `$0` resolved after the `cd` names a file that is not there.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (card t_76a02798) ----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore src/components/i18nDict.ts" >&2
    echo "under each other's feet, which prints false verdicts (t_76a02798, reproduced:" >&2
    echo "13 false GREEN lines in 10 concurrent rounds). Install flock before trusting any" >&2
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
    echo "unserialised pair of runs prints false verdicts (t_76a02798)." >&2
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
    echo "would read its mutation as its own un-bitten sabotage — measured: 13 false verdict" >&2
    echo "lines in 10 concurrent rounds (t_76a02798). Nothing was mutated. Re-run once the" >&2
    echo "other run finishes, or raise the wait with BITEPROOF_LOCK_WAIT=<seconds>." >&2
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
GUARD="${BITEPROOF_GUARD:-src/lib/operatorNotice.test.ts}"
[ -f "$GUARD" ] || { echo "harness: $GUARD is missing — nothing to drive" >&2; exit 2; }

DID_NOT_BITE=0
RESTORE_FAILED=0

failing_tests() {
  # Only the names under "failing tests:" — the ✖ list earlier in the output is
  # the test-runner's own summary echo and over-counts.
  sed -n '/^✖ failing tests:/,$p' | grep -A1 '^test at ' | grep '^✖' | sed 's/^✖ /    - /' | sort -u
}

run_case() {
  local label="$1"
  echo "=============================================================="
  echo "SABOTAGE: $label"
  local out
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1)
  local failed
  failed=$(echo "$out" | failing_tests)
  if [ -z "$failed" ]; then
    echo "  RESULT: *** GREEN — THE GUARD DID NOT BITE ***"
    echo "$out" | tail -3 | sed 's/^/    /'
    DID_NOT_BITE=$((DID_NOT_BITE+1))
  else
    echo "  RESULT: RED"
    echo "$failed"
  fi

  git checkout -- src/ ../api 2>/dev/null
  local dirty
  dirty=$(git status --short src/ ../api | grep -v '^??' || true)
  if [ -n "$dirty" ]; then
    echo "  !!! RESTORE FAILED — still dirty:"; echo "$dirty"
    RESTORE_FAILED=$((RESTORE_FAILED+1))
  fi
}

echo "### baseline — the guard must be GREEN here (this is the fix as committed)"
BASE=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1)
echo "$BASE" | grep -E '^ℹ (tests|pass|fail)'
# Here-string, not `echo "$BASE" | grep -qE …` (card t_39b04c34). Under this file's
# `set -uo pipefail`, `grep -q` exits at its first match while `echo` is still writing
# BASE (a real node --test output for a failing guard is >2 KB), so echo takes SIGPIPE,
# the pipeline reports 141 although grep MATCHED, and this baseline check aborts a
# harness whose guard is genuinely red. Measured on this host, the real shape:
# 1/1500 non-zero with the match on the FIRST matching line, 0/1500 with it last.
# A here-string has no writer process, so there is nothing left to signal.
if grep -qE '^ℹ fail [1-9]' <<<"$BASE"; then
  echo "ABORT: baseline is not green — commit the fix first (this script restores with git checkout)."
  exit 1
fi
if [ -n "$(git status --short src/ ../api | grep -v '^??' || true)" ]; then
  echo "ABORT: uncommitted changes in src/ or ../api — this script would destroy them."
  exit 1
fi

# 1. the block removed from the landing / logged-out home footer
perl -0pi -e 's/\n\s*<OperatorNotice className="mt-2" \/>//' src/app/landing/page.tsx
echo "  [landing OperatorNotice refs left: $(grep -c 'OperatorNotice' src/app/landing/page.tsx)]"
run_case "block removed from landing / home footer"

# 2. the block removed from the privacy policy
perl -0pi -e 's/\s*<OperatorNotice className="text-sm leading-relaxed text-slate-600" \/>//' src/app/legal/privacy/page.tsx
echo "  [privacy OperatorNotice refs left: $(grep -c 'OperatorNotice' src/app/legal/privacy/page.tsx)]"
run_case "block removed from /legal/privacy"

# 3. the block removed from the terms page
perl -0pi -e 's/\s*<OperatorNotice className="text-sm leading-relaxed text-slate-600" \/>//' src/app/legal/terms/page.tsx
echo "  [terms OperatorNotice refs left: $(grep -c 'OperatorNotice' src/app/legal/terms/page.tsx)]"
run_case "block removed from /legal/terms"

# 4. the registration number copied into a page as a second definition
perl -0pi -e 's/(export default function PrivacyPage\(\) \{)/$1\n  const REG = "201403298654";/' src/app/legal/privacy/page.tsx
grep -n 'const REG' src/app/legal/privacy/page.tsx | sed 's/^/  [planted: /;s/$/]/'
run_case "registration number copied into a page (a second definition)"

# 5. a non-English locale left English-only
perl -0pi -e 's/^    ar: "يُدار JomForm.*$/    ar: "JomForm is operated by WANZUL HOSTING ENTERPRISE — Registration No. 201403298654 (formerly PC0012291-H).",/m' src/components/i18nDict.ts
echo "  [ar line now: $(grep -E '^    ar: "JomForm is operated' src/components/i18nDict.ts | head -1 | cut -c1-60)]"
run_case "Arabic left English-only"

# 6. the RTL isolation dropped from the shared component
perl -0pi -e 's/<bdi dir="ltr">\{regClause\}<\/bdi>/\{regClause\}/' src/components/OperatorNotice.tsx
echo "  [bdi left in component: $(grep -c 'bdi dir=' src/components/OperatorNotice.tsx)]"
run_case "bdi dir=ltr isolation dropped (the RTL regression)"

# 7. one locale loses the legal name
perl -0pi -e 's/WANZUL HOSTING ENTERPRISE ஆல்/ஆல்/' src/components/i18nDict.ts
run_case "Tamil loses the legal name"

# 8. the legal name dropped from the one definition (the shared module)
perl -0pi -e 's/export const OPERATOR_LEGAL_NAME = "WANZUL HOSTING ENTERPRISE";/export const OPERATOR_LEGAL_NAME = "JomForm";/' src/lib/operator.ts
grep -n 'OPERATOR_LEGAL_NAME =' src/lib/operator.ts | sed 's/^/  [now: /;s/$/]/'
run_case "the shared module's legal name changed (all locales must follow)"

# 9. the operator sentence hard-coded into a surface instead of the shared block
perl -0pi -e 's/(<h1 className="text-3xl font-bold">Terms of Service<\/h1>)/$1\n      <p>JomForm is operated by WANZUL HOSTING ENTERPRISE — Registration No. 201403298654 (formerly PC0012291-H).<\/p>/' src/app/legal/terms/page.tsx
run_case "operator sentence hard-coded into a surface"

# 10. the platform identity wired into the merchant public-form footer (Go)
printf '\n// SABOTAGE: platform identity leaked into the merchant footer\nvar sabotageOperator = "WANZUL HOSTING ENTERPRISE 201403298654"\n' >> ../api/internal/httpapi/public.go
echo "  [planted in public.go: $(grep -c sabotageOperator ../api/internal/httpapi/public.go)]"
run_case "platform identity leaked into the merchant public-form footer (Go)"

# 11. the component re-pinned to a lang prop (four locales go English-only)
perl -0pi -e 's/export default function OperatorNotice\(\{ className \}: \{ className\?: string \}\) \{/export default function OperatorNotice({ lang = "en", className }: { lang?: string; className?: string }) {/' src/components/OperatorNotice.tsx
perl -0pi -e 's/const \{ t \} = useI18n\(\);/const t = (k: any) => DICT[k][lang];/' src/components/OperatorNotice.tsx
echo "  [component lang prop default present: $(grep -c 'lang = \"en\"' src/components/OperatorNotice.tsx)]"
run_case "component re-pinned to a lang prop (four locales go English-only)"

echo "=============================================================="
echo "cases where the guard did NOT bite: $DID_NOT_BITE"
echo "cases where the restore failed:     $RESTORE_FAILED"
echo "final tracked state (must be empty):"
git status --short src/ ../api | grep -v '^??' || echo "  (clean)"
