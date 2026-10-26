#!/usr/bin/env bash
# Bite proof for web/src/lib/docsA11y.test.ts (card t_18750b46).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions inside the
# guard: docsA11y.test.ts builds its route list from `docsRoutesOnDisk(DOCS_DIR)`
# (the shared recursive walk in web/src/lib/docsRoutes.ts). Its own
# "cannot pass vacuously" test is STRUCTURALLY unable to catch the defect that fix
# closed: a nested route is in `DOCS` BY CONSTRUCTION, and the coverage assertion
# ("every docs page is scanned") checks `DOCS ⊆ scannedPaths`, never the reverse —
# so a walk that omits a nested route can never be caught from inside that guard.
# Only an instrument that puts the page ON DISK and drives the PRE-FIX guard can
# show the widening bites. That is what this harness does.
#
# THE TWO DEFECTS the flat walk had (found by the deploy lane on #976, measured
# here against the flat arm):
#
#   A. A nested route under a ROUTE parent — `app/docs/chat/<seg>/page.tsx`. The
#      flat walk is one level deep, so `/docs/chat/<seg>` is invisible: its `<pre>`
#      and `<a>` rules are never applied, and the suite stays 7/7 GREEN.
#   B. A directory with NO `page.tsx` — a parent directory that is not itself a
#      route, e.g. `app/docs/<dir>/nested/helper.ts`. Every directory was mapped to
#      `<dir>/page.tsx` whether or not it was a route, so four of the seven tests
#      died on `ENOENT … <dir>/page.tsx`: the guard FAILS LOUDLY rather than
#      silently, which is the opposite failure shape and equally wrong.
#
# THE CASES (one `check()` call each; a case is one `BITES`/`***` line):
#
#   baseline        no mutation. Both arms GREEN — otherwise a red below could be
#                   this tree, not the sabotage.
#   CASE A          a nested route under a ROUTE parent (`chat`), carrying a
#                   scrollable `<pre className="overflow-x-auto">` with NO
#                   `tabIndex` — the exact shape the flat walk could not see.
#                   FLAT arm 7/7 GREEN (the defect), NEW arm RED naming the route.
#   CASE B          a nested route under a NON-route parent
#                   (`app/docs/<dir>/nested/page.tsx`, the parent has no page.tsx).
#                   FLAT arm RED on `ENOENT … <dir>/page.tsx` — a crash about a
#                   page that does not exist — and the nested page it does not
#                   reach is named by NOBODY. NEW arm RED naming the nested route,
#                   i.e. red for the RIGHT reason (the route is outside the scan).
#   CASE C          a directory with NO page.tsx anywhere (a parent directory left
#                   behind): FLAT arm RED on the ENOENT crash, NEW arm 7/7 GREEN.
#   CONTROL         a plain NON-page module beside the pages (`app/docs/*.tsx`, the
#                   shape `docsChrome.tsx` itself has). Neither arm may treat it as
#                   a route: GREEN in both columns. Without it, a "fix" that made
#                   every file under app/docs a route would read as a bite.
#
# THE OLD COLUMN IS THE FLAT ARM, derived from git rather than pinned: `git log -S
# docsRoutesOnDisk` finds the commit that introduced the recursive walk into THIS
# guard, and the flat arm is its PARENT. A pinned hash rots the moment the guard is
# touched; the derivation is re-checked by CAPABILITY on every run and REFUSES
# (exit 2) if the revision it resolves already walks recursively, does not carry the
# flat `readdirSync` walk at all, or is byte-identical to this tree's guard. For the
# record, when this harness was written the derivation resolved to 15b9a1a5 and the
# revision QA pinned in the card comment for the same measurement (92261cc7) carried
# a byte-identical guard (sha256 0f7265650100cb33df120891a54eccaa911f7e3832cd3c7581957795155740a1
# — measured with `git show <rev>:web/src/lib/docsA11y.test.ts`). `BITEPROOF_FLAT_ARM`
# overrides the derivation.
#
# THE VERDICT RULE REQUIRES POSITIVE EVIDENCE THE GUARD RAN (the rule
# biteproof_docs_tool_claims.sh settled on and the /docs family reuses). A decision
# from the ABSENCE of a failure line reads a guard that never started (a missing
# file, a file that cannot parse, an all-skipped guard) as GREEN — and on the OLD
# column that false GREEN is the claim this whole proof makes. The rule reads node's
# own summary and REFUSES (exit 2, with the reason) instead of choosing a column.
#
# THE VERDICT CHANNEL IS PER-RUN (a variable set by run_guard, which is therefore
# never called in a command substitution) and the SHARED input is serialised per
# worktree with a `flock` on a file keyed by `git rev-parse --absolute-git-dir`
# under a host-global base. THE LOCK DIRECTORY IS THE FAMILY'S (card t_76b3356d):
# every /docs bite harness takes `jomform-biteproof-docs-bite`, because they all
# mutate the same tree. `trap ... EXIT` removes every probe directory and file,
# the flat-arm copy beside the guard, and the temp files, on EVERY exit.
#
# THE TREE IS REQUIRED UNCHANGED afterwards: the last step reads
# `git status --porcelain` over `src/app/docs` and this guard, and fails if it is
# not empty — a restore by assertion, not by hope.
#
# Run:  bash web/scripts/biteproof_docs_a11y.sh
set -uo pipefail

BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# The shared runner gate and verdict rule (web/scripts/lib/node_test_verdict.sh): every
# verdict this harness prints comes from POSITIVE runner evidence, and a node that cannot
# run the guard is a REFUSAL that names itself — never an answer (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no verdict rule to read a guard's output with." >&2
  exit 2
}
cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (see the header) -----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so two runs in one worktree" >&2
    echo "cannot be serialised and would read each other's probe directories." >&2
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

# CAPABILITY, not presence (card t_b014807f): `node --version` succeeding proves nothing
# here — v18.19.1 answers it and then rejects `--disable-warning`, and every leg below
# would read that empty capture as a verdict. The gate runs the invocation this harness
# depends on and refuses, naming the node, when that produces no test-runner summary.
nt_require_node

GUARD=src/lib/docsA11y.test.ts
OLD_GUARD=$(mktemp)
# The flat arm is run from a copy INSIDE src/lib: its own relative reads
# (`../../e2e/a11y.spec.ts`) resolve against the file's location, so a copy under
# /tmp would read nothing. Removed on every exit.
FLAT_TMP="src/lib/docsA11y.flatarm.$$.ts"
PROBE_A=src/app/docs/chat/t-18750b46-a          # nested route under a ROUTE parent
PROBE_B=src/app/docs/t-18750b46-b               # nested route under a NON-route parent
PROBE_C=src/app/docs/t-18750b46-c               # directory with NO page.tsx anywhere
PROBE_CTL=src/app/docs/t-18750b46-ctl.tsx       # a non-page module beside the pages
PROBES="$PROBE_A $PROBE_B $PROBE_C"
fail=0

OLDTMP="$GUARD.selfcheck_driven.$$.ts"
SKTMP="$GUARD.selfcheck_skipped.$$.ts"
NOLOADTMP="$GUARD.selfcheck_loadfail.$$.ts"
MISSINGTMP="$GUARD.selfcheck_missing.$$.ts"
cleanup() {
  local rc=$? d
  for d in $PROBES; do rm -rf "$d"; done
  rm -f "$PROBE_CTL" "$FLAT_TMP"
  rm -f "$OLD_GUARD" "$OLDTMP" "$SKTMP" "$NOLOADTMP" "$MISSINGTMP"
  exit "$rc"
}
trap cleanup EXIT

# ---- the FLAT (pre-fix) arm, derived -------------------------------------------------
TOP=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "not in a git worktree" >&2; exit 2; }
GUARD_IN_REPO="${PWD#"$TOP"/}/$GUARD"

if [ -n "${BITEPROOF_FLAT_ARM:-}" ]; then
  FLAT_ARM_REV=$(git rev-parse "${BITEPROOF_FLAT_ARM}^{commit}" 2>/dev/null) || {
    echo "cannot resolve BITEPROOF_FLAT_ARM=${BITEPROOF_FLAT_ARM} — fetch it, or unset it" >&2
    exit 2
  }
  FLAT_ARM_HOW="BITEPROOF_FLAT_ARM"
else
  # -C "$TOP": a pathspec is resolved against the CALLER's directory, and this script
  # has already cd'd into web/ — so the repo-relative path must be given with git's own
  # directory changed to the root (measured: from web/, `-- web/src/lib/...` matched
  # NOTHING and the derivation silently returned empty).
  INTRODUCING=$(git -C "$TOP" log --format=%H -1 -S'docsRoutesOnDisk' -- "$GUARD_IN_REPO" 2>/dev/null)
  [ -n "$INTRODUCING" ] || {
    echo "BITEPROOF REFUSED: cannot find the commit that introduced docsRoutesOnDisk into" >&2
    echo "$GUARD_IN_REPO, so the flat arm cannot be derived (set BITEPROOF_FLAT_ARM)." >&2
    exit 2
  }
  FLAT_ARM_REV=$(git rev-parse "${INTRODUCING}^" 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve the parent of $INTRODUCING." >&2
    exit 2
  }
  FLAT_ARM_HOW="the parent of $INTRODUCING (the commit that introduced the recursive walk)"
fi
git cat-file -e "${FLAT_ARM_REV}:${GUARD_IN_REPO}" 2>/dev/null || {
  echo "BITEPROOF REFUSED: ${FLAT_ARM_REV} does not carry ${GUARD_IN_REPO}, so it is not" >&2
  echo "a revision of this guard to measure the old column against." >&2
  exit 2
}
git show "${FLAT_ARM_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"

# CAPABILITY of the flat arm: refuse a revision that has CAUGHT UP to this tree (it
# would make every case print "the case does not reproduce the gap"), one that does
# not carry the flat walk at all, and one byte-identical to this tree's guard.
if grep -q 'docsRoutesOnDisk' "$OLD_GUARD"; then
  echo "BITEPROOF REFUSED: the revision ${FLAT_ARM_REV:0:12} (${FLAT_ARM_HOW}) already enumerates the" >&2
  echo "docs routes through the recursive walk, so it has caught up to this tree and there is" >&2
  echo "no flat-walk defect left to reproduce. Point BITEPROOF_FLAT_ARM at a pre-fix revision." >&2
  exit 2
fi
if ! grep -q 'readdirSync(DOCS_DIR)' "$OLD_GUARD"; then
  echo "BITEPROOF REFUSED: the flat arm ${FLAT_ARM_REV:0:12} does not carry the one-level" >&2
  echo "readdirSync(DOCS_DIR) walk, so it is not the shape this harness measures." >&2
  exit 2
fi
if cmp -s "$OLD_GUARD" "$GUARD"; then
  echo "BITEPROOF REFUSED: the flat arm ${FLAT_ARM_REV:0:12} is byte-identical to this tree's guard," >&2
  echo "so neither column can differ and every case would be vacuous." >&2
  exit 2
fi

cp "$OLD_GUARD" "$FLAT_TMP"
echo "flat arm (${FLAT_ARM_HOW}): ${FLAT_ARM_REV:0:12}, $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):          $(wc -l < "$GUARD") lines"

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

# PREMISE: the flat arm must be GREEN on the UNMUTATED tree, or no mutation can make
# it "miss" anything. (Case B/C expect it RED — but only because of a probe this
# harness created, which is why the premise is measured before any probe exists.)
run_guard "$FLAT_TMP"
if [ "$VERDICT" = REFUSED ]; then
  echo >&2; echo "BITEPROOF REFUSED: the flat arm did not RUN on the unmutated tree (${VERDICT_WHY})." >&2
  exit 2
fi
if [ "$VERDICT" != "GREEN" ]; then
  echo >&2; echo "BITEPROOF REFUSED: the flat arm is ${VERDICT} on the UNMUTATED tree — a red base." >&2
  echo >&2; grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" >&2
  exit 2
fi

# SELF-CHECK of the verdict rule on every run, against REAL node runs: a driven
# guard is GREEN, an all-skipped guard and a guard that cannot load are REFUSED.
selfcheck_rule() {
  local bad='' sk="$SKTMP" noload="$NOLOADTMP"
  cp "$GUARD" "$OLDTMP"
  run_guard "$OLDTMP"
  rm -f "$OLDTMP"
  [ "$VERDICT" = "GREEN" ] || bad="leg 1: a real driven guard run read as ${VERDICT}, wanted GREEN"
  printf '%s\n' 'import { test } from "node:test";' 'test.skip("a guard whose tests never run", () => {});' > "$sk"
  printf '%s\n' 'const broken: = 1;' > "$noload"
  run_guard "$sk"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 2: an all-skipped guard read as ${VERDICT}, wanted REFUSED"
  run_guard "$noload"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 3: a guard that cannot load read as ${VERDICT}, wanted REFUSED"
  run_guard "$MISSINGTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 4: a missing guard read as ${VERDICT}, wanted REFUSED"
  rm -f "$sk" "$noload" "$MISSINGTMP"
  if [ -n "$bad" ]; then
    echo >&2; echo "BITEPROOF REFUSED: the VERDICT RULE ITSELF is broken — ${bad}." >&2
    exit 2
  fi
}
selfcheck_rule

refuse() {
  echo >&2; echo "  *** ${1}: the ${2} arm DID NOT RUN — ${VERDICT_WHY}. Refusing (exit 2) ***" >&2
  exit 2
}

# check <label> <old-want> <new-want> <old-needle> <new-needle> [kind]
#   <old-needle>/<new-needle>: a string the failing output must NAME, so a red (or a
#   green) cannot be credited to a sibling test or to an unrelated crash.
#   kind: "baseline" | "control" | "" (a defect case) — only the wording of the
#   success line, so a control that must agree in both columns is not printed as if
#   it reproduced a gap.
check() {
  local label="$1" old_want="$2" new_want="$3" old_needle="${4:-}" new_needle="${5:-}" kind="${6:-}"

  run_guard "$FLAT_TMP"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "FLAT"
  if [ "$VERDICT" != "$old_want" ]; then
    echo "  *** ${label}: FLAT arm wanted ${old_want}, got ${VERDICT} — the case does not reproduce the gap ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ -n "$old_needle" ] && [ "$VERDICT" = RED ] && ! grep -qF "$old_needle" <<<"$VERDICT_OUT"; then
    echo "  *** ${label}: FLAT arm ${VERDICT} but its failure does not name ${old_needle} ***"
    grep -E '✖' <<<"$VERDICT_OUT" | head -3; fail=1
  elif [ "$kind" = "baseline" ]; then
    echo "  ok   baseline: FLAT arm ${VERDICT}, as expected on the unmutated tree"
  elif [ "$kind" = "control" ]; then
    echo "  ok   ${label}: FLAT arm ${VERDICT} — the control must agree in both columns"
  else
    echo "  ok   ${label}: FLAT arm ${VERDICT}${old_needle:+ (its failure names ${old_needle})} — the pre-fix behaviour reproduces"
  fi

  run_guard "$GUARD"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "NEW"
  if [ "$VERDICT" != "$new_want" ]; then
    echo "  *** ${label}: NEW arm wanted ${new_want}, got ${VERDICT} ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ -n "$new_needle" ] && [ "$VERDICT" = RED ] && ! grep -qF "$new_needle" <<<"$VERDICT_OUT"; then
    echo "  *** ${label}: NEW arm ${VERDICT} but the failure does not name ${new_needle} ***"
    grep -E '✖' <<<"$VERDICT_OUT" | head -3; fail=1
  else
    echo "  BITES ${label}: NEW arm ${VERDICT}${new_needle:+ (naming ${new_needle})}"
  fi
}

echo
echo "== baseline (no mutation) =="
check "baseline" GREEN GREEN "" "" baseline

echo
echo "== CASE A: nested route under a ROUTE parent (defect A) =="
mkdir -p "$PROBE_A"
printf 'export default function ProbeA() {\n  return (\n    <pre className="overflow-x-auto">{`a nested route whose parent is a route`.repeat(30)}</pre>\n  );\n}\n' > "$PROBE_A/page.tsx"
if [ -f "$PROBE_A/page.tsx" ] && [ -f "src/app/docs/chat/page.tsx" ]; then
  echo "       mutation on disk: $PROBE_A/page.tsx (parent src/app/docs/chat IS a route)"
  # FLAT arm: the page is invisible, so its scrollable <pre> is never checked → GREEN.
  # NEW arm: it is a route, it is not in PAGES, and its <pre> has no tabIndex → RED,
  # naming the route in the coverage failure and in the <pre> failure.
  check "nested route under a route parent" GREEN RED "" "/docs/chat/t-18750b46-a"
else
  echo "  *** MUTATION DID NOT APPLY: $PROBE_A ***"; fail=1
fi
rm -rf "$PROBE_A"
echo "       probe route removed"

echo
echo "== CASE B: nested route under a NON-route parent (defect B) =="
mkdir -p "$PROBE_B/nested"
printf 'export default function ProbeB() {\n  return <p>a nested route whose parent is not a route</p>;\n}\n' > "$PROBE_B/nested/page.tsx"
if [ -f "$PROBE_B/nested/page.tsx" ] && [ ! -f "$PROBE_B/page.tsx" ]; then
  echo "       mutation on disk: $PROBE_B/nested/page.tsx (parent has NO page.tsx)"
  # FLAT arm: `t-18750b46-b` is a directory, so it is mapped to a page.tsx that does
  # not exist → the ENOENT crash, naming the page it invented. That is the defect:
  # red for a page nobody wrote, while the nested page it cannot reach is unmentioned.
  # NEW arm: the nested route is a route and is not in PAGES → RED, naming it.
  check "nested route under a non-route parent" RED RED \
    "$PROBE_B/page.tsx" "/docs/t-18750b46-b/nested"
else
  echo "  *** MUTATION DID NOT APPLY: $PROBE_B ***"; fail=1
fi
rm -rf "$PROBE_B"
echo "       probe route removed"

echo
echo "== CASE C: a directory with NO page.tsx anywhere (defect B, no route inside) =="
mkdir -p "$PROBE_C/nested"
printf 'export const helper = 1;\n' > "$PROBE_C/nested/helper.ts"
if [ -f "$PROBE_C/nested/helper.ts" ] && [ ! -f "$PROBE_C/page.tsx" ]; then
  echo "       mutation on disk: $PROBE_C/nested/helper.ts (no page.tsx under the probe)"
  # FLAT arm: the ENOENT crash on the page.tsx it invented for a non-route directory.
  # NEW arm: a directory without a page.tsx is only ever a parent → 7/7 GREEN.
  check "directory with no page.tsx" RED GREEN "$PROBE_C/page.tsx" ""
else
  echo "  *** MUTATION DID NOT APPLY: $PROBE_C ***"; fail=1
fi
rm -rf "$PROBE_C"
echo "       probe directory removed"

echo
echo "== NEGATIVE CONTROL: a non-page module beside the pages is not a route in either arm =="
printf 'export const ctl = 1;\n' > "$PROBE_CTL"
if [ -f "$PROBE_CTL" ]; then
  echo "       mutation on disk: $PROBE_CTL (a file, not a page.tsx — the shape docsChrome.tsx has)"
  check "non-page module beside the pages" GREEN GREEN "" "" control
else
  echo "  *** MUTATION DID NOT APPLY: $PROBE_CTL ***"; fail=1
fi
rm -f "$PROBE_CTL"
echo "       control file removed"

echo
echo "== the tree must be unchanged =="
rm -f "$FLAT_TMP"                     # the flat-arm copy is this harness's own temp
DIRTY=$(git status --porcelain -- src/app/docs "$GUARD" 2>/dev/null)
if [ -z "$DIRTY" ]; then
  echo "  ok   src/app/docs and $GUARD are clean (no probe and no backup left behind)"
else
  echo "  *** THE TREE IS DIRTY after this run:"; printf '%s\n' "$DIRTY"; fail=1
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — baseline + 4/4 cases: the flat arm missed the nested route under a"
  echo "route parent, crashed on the two non-route-directory shapes, and left a non-page"
  echo "module alone; this tree's guard reds on the first two route shapes and stays green"
  echo "on the rest, and the tree is byte-identical afterwards."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
