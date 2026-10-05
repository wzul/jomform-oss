#!/usr/bin/env bash
# Bite proof for web/src/lib/llmsFullCoverage.test.ts (card t_96fb6ab1).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: the guard's
# new arm compares llms-full.txt against the pages ON DISK, and the property that
# has to hold is that the arm FAILS when a disk page has no entry — a test inside the
# guard can only exercise the rule, it cannot show that the rule would have caught
# the defect. Each case below drives that: the guard as it stands on the base
# (`origin/main`, the revision this branch is cut from) must stay GREEN on the
# mutated tree — that is the hole the card is about — and the guard in this working
# tree must go RED and NAME the page.
#
# The three cases 1-3 are the card's own measured pages (/docs/chat, /docs/builder,
# /docs/feedback): the base guard compares against the /docs INDEX's links only, and
# the index deliberately does not link any of them (they are reached from their own
# feature's inline "?"), so it cannot see them. Case 4 is the shape the card exists
# to keep guarded — a NEW page appearing under app/docs with no entry — which no
# index-scoped guard can catch. Case 5 (card t_3a5aa9b5) is the shape that survived
# even the disk arm: a NEW NESTED page, `app/docs/<seg>/<sub>/page.tsx`, whose parent
# directory is not itself a route, so the flat one-level walk the disk arm shipped
# with never reached it. The negative control proves the reverse direction is
# NOT a strict equality: an entry naming a page that is not a docs route stays GREEN.
#
# Each case: mutate -> assert the mutation is on disk -> run the BASE guard (expect
# GREEN, i.e. the hole) -> run THIS tree's guard (expect RED, naming the page) ->
# restore byte-identically (re-checked with sha256).
#
# The base is `origin/main` and not a pinned hash: a hash rots when the guard changes,
# and `origin/main` is the revision the comparison means and moves with the base.
#
# THE VERDICT RULE REQUIRES POSITIVE EVIDENCE THE GUARD RAN (the rule
# biteproof_docs_tool_claims.sh settled on and this script reuses). A decision from
# the ABSENCE of a failure line reads a guard that never started (a missing file, a
# file that cannot parse, an all-skipped guard) as GREEN — and on the BASE column that
# false GREEN is the claim this whole proof makes. The rule reads node's own summary
# and REFUSES (exit 2, with the reason) instead of choosing a column.
#
# THE VERDICT CHANNEL IS PER-RUN and the SHARED input (llms-full.txt, for cases 1-3)
# is serialised per worktree with a `flock` on `git rev-parse --absolute-git-dir`
# under a host-global base; `trap ... EXIT` restores `llms-full.txt` and removes every
# probe directory byte-identically on every exit. THE LOCK IS THE FAMILY'S (card
# t_76b3356d): every /docs bite harness takes the SAME file,
# `jomform-biteproof-docs-bite`, because they all mutate the same tree — a per-script
# directory serialised two runs of one script and left two DIFFERENT harnesses of the
# family overlapping (measured 3/3 rounds, 6 false `***` lines against 4 for a solo run,
# including a false `NEW guard wanted GREEN, got RED` on tool_claims' own control).
# Two lanes' cards still never serialise behind each other: the key is the WORKTREE.
#
# Run:  bash web/scripts/biteproof_llms_full_coverage.sh
set -uo pipefail

BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (see the header) -----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so two runs in one worktree" >&2
    echo "cannot be serialised and would read each other's mutated llms-full.txt." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir, so there is no" >&2
    echo "per-worktree key to serialise on." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR." >&2
    exit 2
  }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || {
    echo "BITEPROOF REFUSED: cannot open the lock file $LOCK_FILE." >&2
    exit 2
  }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   another run of this harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi

# The child. Refuse if the lock it is supposed to have inherited cannot be shown held.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 but BITEPROOF_LOCK_FILE is empty." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file, so \"the lock is held\" proves nothing." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

LLMS=public/llms-full.txt
GUARD=src/lib/llmsFullCoverage.test.ts
# Every probe route directory this harness creates. Two shapes now: a FLAT route
# (case 4) and a NESTED route under a parent that is NOT itself a route (case 5) —
# the shape a flat walk cannot reach (card t_3a5aa9b5).
PROBES="src/app/docs/t-96fb6ab1-probe src/app/docs/t-3a5aa9b5-nested"
PROBE_ROUTE=src/app/docs/t-96fb6ab1-probe
PROBE_NESTED=src/app/docs/t-3a5aa9b5-nested/guide/start

OLD_GUARD=$(mktemp)
LLMS_BAK=$(mktemp)
cp "$LLMS" "$LLMS_BAK"
LLMS_SHA=$(sha256sum "$LLMS" | cut -d' ' -f1)
fail=0

# ONE trap: restores llms-full.txt and removes every probe route on EVERY exit,
# interrupts included, and removes every temp this run created (all per-run mktemp).
OLDTMP="$GUARD.oldcheck.$$.ts"
SKTMP="$GUARD.selfcheck_skipped.$$.ts"
NOLOADTMP="$GUARD.selfcheck_loadfail.$$.ts"
MISSINGTMP="$GUARD.selfcheck_missing.$$.ts"
cleanup() {
  local rc=$? d
  cp "$LLMS_BAK" "$LLMS" 2>/dev/null
  for d in $PROBES; do rm -rf "$d"; done
  rm -f "$OLD_GUARD" "$LLMS_BAK" "$FLAT_ARM_GUARD"
  rm -f "$OLDTMP" "$SKTMP" "$NOLOADTMP" "$MISSINGTMP"
  exit "$rc"
}
trap cleanup EXIT

# The "before" guard: the one on the branch's base. Fail closed, with the reason.
TOP=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "not in a git worktree" >&2; exit 2; }
GUARD_IN_REPO="${PWD#"$TOP"/}/$GUARD"
BASE_REF="${BITEPROOF_BASE:-origin/main}"
BASE_REV=$(git rev-parse "${BASE_REF}^{commit}" 2>/dev/null) || {
  echo "cannot resolve ${BASE_REF} — fetch it, or set BITEPROOF_BASE" >&2
  exit 2
}
git cat-file -e "${BASE_REV}:${GUARD_IN_REPO}" 2>/dev/null || {
  echo "cannot read ${GUARD_IN_REPO} at ${BASE_REF} (${BASE_REV})" >&2
  exit 2
}
git show "${BASE_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"

# The FLAT-disk-arm guard: the merged revision of this SAME guard (the disk arm added
# by its own PR), captured before CAPABILITY refuses a base that already reads the
# disk. Case 5 needs it because case 5's shape survived BOTH the index-scoped base and
# the flat disk arm (card t_3a5aa9b5). Refuse by name if it cannot be read, never
# silently fall back to a base that would make the case prove nothing.
FLAT_ARM_GUARD=$(mktemp)
FLAT_ARM_REF="${BITEPROOF_FLAT_ARM:-d2b9142c478804a084aa8edae783f77ad68aedda}"  # PR 972 (the flat disk arm)
if ! git cat-file -e "${FLAT_ARM_REF}^{commit}" 2>/dev/null; then
  echo "BITEPROOF REFUSED: cannot resolve the flat-disk-arm revision ${FLAT_ARM_REF} (set BITEPROOF_FLAT_ARM)." >&2
  exit 2
fi
git cat-file -e "${FLAT_ARM_REF}:${GUARD_IN_REPO}" 2>/dev/null || {
  echo "BITEPROOF REFUSED: ${FLAT_ARM_REF} does not carry ${GUARD_IN_REPO}." >&2
  exit 2
}
git show "${FLAT_ARM_REF}:${GUARD_IN_REPO}" > "$FLAT_ARM_GUARD"
if ! grep -qE 'readdirSync|docsRoutesOnDisk' "$FLAT_ARM_GUARD"; then
  echo "BITEPROOF REFUSED: ${FLAT_ARM_REF} does not read the pages on disk, so it is not the" >&2
  echo "flat disk arm case 5 must measure the old column against." >&2
  exit 2
fi

# CAPABILITY of the base: if the base guard already reads the pages on disk, it has
# caught up to this tree and there is no gap to reproduce — every case would print
# "does not reproduce". Refuse by capability rather than measuring against a base
# that proves nothing (the rule biteproof_docs_index_render_sites.sh arrived at).
if grep -qE 'readdirSync|docsRoutesOnDisk' "$OLD_GUARD"; then
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) already reads the docs" >&2
  echo "pages on disk, so it has caught up to this tree and there is no gap to reproduce." >&2
  echo "Point BITEPROOF_BASE at a revision whose guard is still index-scoped." >&2
  exit 2
fi

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines"

# rule_of <node exit status> — turns a guard run's output in $VERDICT_OUT into a verdict.
rule_of() {
  local rc="$1" out="$VERDICT_OUT"
  VERDICT='' VERDICT_WHY=''
  local line; line=$(grep -E '^ℹ (tests|pass|fail|skipped|todo) ' <<<"$out" | tr '\n' ' ')
  if ! grep -qE '^ℹ ' <<<"$out"; then
    VERDICT=REFUSED
    VERDICT_WHY="node exited ${rc} with NO test summary at all — the guard did not run"
    return
  fi
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then
    if grep -qE '^ℹ pass [1-9]' <<<"$out"; then
      VERDICT=RED
    else
      VERDICT=REFUSED
      VERDICT_WHY="every test failed and none passed (${line}) — the guard could not load"
    fi
    return
  fi
  if grep -qE '^ℹ pass [1-9]' <<<"$out" && grep -qE '^✔ ' <<<"$out" && [ "$rc" = 0 ]; then
    VERDICT=GREEN
    return
  fi
  VERDICT=REFUSED
  VERDICT_WHY="node exited ${rc} with ${line:-no summary line} — no passing test to show the guard ran"
}

# run_guard <guardfile> — sets VERDICT / VERDICT_OUT / VERDICT_WHY. NOT called in a
# command substitution (that is a subshell; the variables would not come back).
run_guard() {
  local rc=0
  VERDICT_OUT=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || rc=$?
  rule_of "$rc"
}

run_guard_premise() {
  cp "$OLD_GUARD" "$OLDTMP"
  run_guard "$OLDTMP"
  rm -f "$OLDTMP"
}

# PREMISE: the base guard must be GREEN on the UNMUTATED tree, or no mutation can
# make it "miss" anything.
run_guard_premise
if [ "$VERDICT" = REFUSED ]; then
  echo >&2; echo "BITEPROOF REFUSED: the base guard did not RUN on the unmutated tree (${VERDICT_WHY})." >&2
  exit 2
fi
if [ "$VERDICT" != "GREEN" ]; then
  echo >&2; echo "BITEPROOF REFUSED: the base guard is ${VERDICT} on the UNMUTATED tree — red base." >&2
  exit 2
fi

# SELF-CHECK of the verdict rule on every run, against REAL node runs: a driven
# guard is GREEN, an all-skipped guard and a guard that cannot load are REFUSED.
selfcheck_rule() {
  local bad='' sk="$SKTMP" noload="$NOLOADTMP"
  rule_of 0
  [ "$VERDICT" = "GREEN" ] || bad="leg 1: a real driven guard run read as ${VERDICT}, wanted GREEN"
  printf '%s\n' 'import { test } from "node:test";' 'test.skip("a guard whose tests never run", () => {});' > "$sk"
  printf '%s\n' 'const broken: = 1;' > "$noload"
  run_guard "$sk"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 2: an all-skipped guard read as ${VERDICT}, wanted REFUSED"
  run_guard "$noload"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 3: a guard that cannot load read as ${VERDICT}, wanted REFUSED"
  run_guard "$MISSINGTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 4: a missing guard read as ${VERDICT}, wanted REFUSED"
  rm -f "$sk" "$noload"
  if [ -n "$bad" ]; then
    echo >&2; echo "BITEPROOF REFUSED: the VERDICT RULE ITSELF is broken — ${bad}." >&2
    exit 2
  fi
}
selfcheck_rule

refuse() {
  echo >&2; echo "  *** ${1}: the ${2} guard DID NOT RUN — ${VERDICT_WHY}. Refusing (exit 2) ***" >&2
  exit 2
}

check() { # check <label> <old-want> <new-want> [must-name] [old-guard-file]
  local label="$1" old_want="$2" new_want="$3" needle="${4:-}" old_file="${5:-$OLD_GUARD}"
  local oldtmp="$OLDTMP"
  cp "$old_file" "$oldtmp"
  run_guard "$oldtmp"; rm -f "$oldtmp"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "OLD"
  if [ "$VERDICT" != "$old_want" ]; then
    echo "  *** ${label}: OLD guard wanted ${old_want}, got ${VERDICT} — the case does not reproduce the gap ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ "$label" = "baseline" ]; then
    echo "  ok   baseline: old guard ${VERDICT}, as expected"
  else
    echo "  ok   ${label}: old guard ${VERDICT} — the gap reproduces (it missed this)"
  fi

  run_guard "$GUARD"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "NEW"
  if [ "$VERDICT" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${VERDICT} ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$VERDICT_OUT"; then
    echo "  *** ${label}: NEW guard ${VERDICT} but the failure does not name ${needle} ***"
    grep -F 'are not named' <<<"$VERDICT_OUT" | head -2; fail=1
  else
    echo "  BITES ${label}: new guard ${VERDICT}${needle:+, naming $needle}"
  fi
}

# Remove the single line naming <path> from llms-full.txt (the entries this card
# added are one line each), asserting the removal really landed.
remove_entry() {
  local path="$1" tmp
  tmp=$(mktemp)
  grep -vF "jomform.com${path})" "$LLMS" > "$tmp"
  if ! grep -qF "jomform.com${path})" "$tmp"; then mv "$tmp" "$LLMS"; else rm -f "$tmp"; return 1; fi
  return 0
}
restore_llms() {
  cp "$LLMS_BAK" "$LLMS"
  [ "$(sha256sum "$LLMS" | cut -d' ' -f1)" = "$LLMS_SHA" ] \
    && echo "       restored byte-identical" \
    || { echo "  *** RESTORE MISMATCH $LLMS ***"; fail=1; }
}

echo
echo "== baseline (no mutation) =="
check "baseline" GREEN GREEN

for page in /docs/chat /docs/builder /docs/feedback; do
  echo
  echo "== CASE: the $page entry removed from llms-full.txt =="
  if remove_entry "$page"; then
    echo "       mutation on disk: no line names jomform.com${page})"
    check "$page unnamed" GREEN RED "$page"
  else
    echo "  *** MUTATION DID NOT APPLY: $page ***"; fail=1
  fi
  restore_llms
done

echo
echo "== CASE 4: a NEW page on disk with no entry (the 'cannot go missing again' shape) =="
mkdir -p "$PROBE_ROUTE"
printf 'export default function Probe() { return null; }\n' > "$PROBE_ROUTE/page.tsx"
if [ -f "$PROBE_ROUTE/page.tsx" ] && ! grep -qF "jomform.com/docs/t-96fb6ab1-probe)" "$LLMS"; then
  echo "       mutation on disk: $PROBE_ROUTE/page.tsx exists with no llms-full.txt entry"
  check "new page with no entry" GREEN RED "/docs/t-96fb6ab1-probe"
else
  echo "  *** MUTATION DID NOT APPLY: probe route ***"; fail=1
fi
rm -rf "$PROBE_ROUTE"
echo "       probe route removed"

echo
echo "== CASE 5: a NEW NESTED page whose parent is NOT a route (card t_3a5aa9b5) =="
# The nesting is the point: `t-3a5aa9b5-nested/` has NO page.tsx, so a walk that
# filters on the DIRECTORY having a page.tsx never reaches guide/start/page.tsx.
mkdir -p "$PROBE_NESTED"
printf 'export default function Probe() { return null; }\n' > "$PROBE_NESTED/page.tsx"
if [ -f "$PROBE_NESTED/page.tsx" ] \
   && [ ! -f "src/app/docs/t-3a5aa9b5-nested/page.tsx" ] \
   && ! grep -qF "jomform.com/docs/t-3a5aa9b5-nested/guide/start)" "$LLMS"; then
  echo "       mutation on disk: $PROBE_NESTED/page.tsx exists under a non-route parent, with no entry"
  # OLD column is the FLAT disk arm (PR 972), not origin/main's index-scoped guard:
  # that is the revision this card found the hole in, and it must stay GREEN on a
  # nested page — the flat walk cannot reach it.
  check "nested page under a non-route parent" GREEN RED "/docs/t-3a5aa9b5-nested/guide/start" "$FLAT_ARM_GUARD"
else
  echo "  *** MUTATION DID NOT APPLY: nested probe route ***"; fail=1
fi
rm -rf "src/app/docs/t-3a5aa9b5-nested"
echo "       nested probe route removed"

echo
echo "== NEGATIVE CONTROL: an entry naming a page that is NOT a docs route stays GREEN in both =="
printf -- '- [Brand assets](https://jomform.com/brand) — extra line, not a docs route.\n' >> "$LLMS"
if grep -qF "jomform.com/brand) — extra line" "$LLMS"; then
  echo "       mutation on disk: an extra non-docs entry"
  check "extra non-docs entry" GREEN GREEN
else
  echo "  *** MUTATION DID NOT APPLY: extra non-docs entry ***"; fail=1
fi
restore_llms

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — each disk page with no entry was GREEN under the base guard and RED under this one."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
