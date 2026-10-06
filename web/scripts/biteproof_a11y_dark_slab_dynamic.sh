#!/usr/bin/env bash
# Bite proof for web/src/lib/a11yDarkSlabCoverage.test.ts's DYNAMIC-route arm
# (card t_70a79006).
#
# WHAT THIS PROVES. The defect is a hole in a guard, so the claim to prove is not
# "the guard is green today" but "the guard could not see this CLASS, and now it
# can". Both directions are driven on the SAME planted slab:
#
#   * the guard as it shipped (`origin/main` bytes) over a dark slab at
#     `app/probe-dyn/[id]/page.tsx`  -> GREEN  (the hole: judged by NEITHER layer)
#   * this tree's guard over the same slab -> RED, naming the route and the token
#   * CONTROL: the same slab at a STATIC route -> RED under BOTH guards, so the
#     pre-fix GREEN above is about the route CLASS and not about a guard that was
#     dead for every input
#   * NEGATIVE CONTROL: a dynamic route with NO slab -> GREEN under both
#   * BASELINE: the unmutated tree is GREEN, so a red below is the sabotage and
#     not this tree being broken
#
# It plants and removes files under `src/app`, so it is serialised per worktree on
# a flock and every removal is verified. It takes its OWN lock directory: the
# /docs family key `jomform-biteproof-docs-bite` exists for harnesses that mutate
# files under `src/app/docs`, and this one mutates probe routes outside that tree,
# so the two share no input. For the same reason it is NOT an entry in
# `web/src/lib/harnessLock.test.ts` — that file's premise is "harnesses that mutate
# the shared docs input"; the split is stated in BITE-HARNESSES.md rather than
# implied by the absence.
#
# Run:  bash web/scripts/biteproof_a11y_dark_slab_dynamic.sh
set -uo pipefail

BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve this script's directory" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                    # -> web/

LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"
if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so two runs in one worktree could" >&2
    echo "read each other's planted probe routes and print false verdicts." >&2
    exit 2
  }
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-a11y-dark-slab"
  mkdir -p "$LOCK_DIR" || { echo "BITEPROOF REFUSED: cannot create $LOCK_DIR" >&2; exit 2; }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || { echo "BITEPROOF REFUSED: cannot open $LOCK_FILE" >&2; exit 2; }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   a peer run holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  rc=$?
  if [ "$rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer that plants probe routes in the same tree." >&2
    echo "Nothing was mutated. Re-run once the peer finishes." >&2
    exit 75
  fi
  [ "$rc" = 0 ] || { echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${rc}" >&2; exit 2; }
  bash "$SELF" "$@"
  exit "$?"
fi

LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
[ -n "$LOCK_FILE" ] || {
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 but BITEPROOF_LOCK_FILE is empty." >&2; exit 2; }
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer planting probe routes would give it false verdicts." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

GUARD=src/lib/a11yDarkSlabCoverage.test.ts
GUARD_REPO="web/$GUARD"          # the same path as git sees it, from the repo root
SPEC=src/lib/jsxSource.ts
[ -f "$GUARD" ] || { echo "missing $GUARD — is this the right worktree?" >&2; exit 2; }

# The guard as it SHIPPED on origin/main, dropped beside the real one so its
# relative `../` paths still resolve to this tree's `web/src`. A copy that cannot
# be made (no origin/main, or the file is newer than the ref) is a REFUSAL: a
# missing "old" column is not a pass.
OLD_GUARD=src/lib/.biteproof-prefix-guard.ts
if ! git show "origin/main:$GUARD_REPO" > "$OLD_GUARD" 2>/dev/null; then
  echo "BITEPROOF REFUSED: cannot read $GUARD_REPO from origin/main, so the PRE-FIX column (the" >&2
  echo "hole this harness exists to show) cannot be driven." >&2
  rm -f "$OLD_GUARD"
  exit 2
fi
if grep -q 'PINNED_DYNAMIC_CARRIERS' "$OLD_GUARD"; then
  echo "BITEPROOF REFUSED: origin/main's guard ALREADY carries the dynamic-route rule, so the" >&2
  echo "pre-fix column would agree with this tree's and prove nothing (the base moved)." >&2
  rm -f "$OLD_GUARD"
  exit 2
fi
OLD_SHA=$(sha256sum "$OLD_GUARD" | cut -d' ' -f1)
GUARD_SHA_BEFORE=$(sha256sum "$GUARD" | cut -d' ' -f1)

DYN_DIR='src/app/probe-dyn/[id]'
DYN_PAGE="$DYN_DIR/page.tsx"
ST_DIR=src/app/probe-static
ST_PAGE="$ST_DIR/page.tsx"

fail=0
cleanup() {
  local rc=$?
  rm -f "$DYN_PAGE" "$ST_PAGE" "$OLD_GUARD"
  rmdir "$DYN_DIR" "$DYN_DIR/.." "$ST_DIR" 2>/dev/null
  exit "$rc"
}
trap cleanup EXIT

plant() { # plant <page-path> <token>
  mkdir -p "$(dirname "$1")" || { echo "  *** cannot create $(dirname "$1") ***" >&2; fail=1; return 1; }
  printf 'export default function P() {\n  return <div className="%s p-4 text-slate-100">hello</div>;\n}\n' "$2" > "$1"
}
unplant() { rm -f "$1"; rmdir "$(dirname "$1")" 2>/dev/null; }

run_guard() { # run_guard <guard-file>; sets OUT and RC
  local rc=0
  OUT=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || rc=$?
  RC=$rc
}
# verdict_of <rc>: GREEN | RED | REFUSED. A column with no run behind it is not a
# column — the rule web/scripts/biteproof_a11y_scan_prose.sh documents.
verdict_of() {
  local rc="$1"
  V=''
  if ! grep -qE '^ℹ ' <<<"$OUT"; then V=REFUSED; return; fi
  if grep -qE '^ℹ fail [1-9]' <<<"$OUT"; then
    if grep -qE '^ℹ pass [1-9]' <<<"$OUT"; then V=RED; else V=REFUSED; fi
    return
  fi
  if grep -qE '^ℹ pass [1-9]' <<<"$OUT" && grep -qE '^✔ ' <<<"$OUT" && [ "$rc" = 0 ]; then
    V=GREEN; return
  fi
  V=REFUSED
}

expect() { # expect <label> <guard-file> <want> [needle]
  local label="$1" guard="$2" want="$3" needle="${4:-}"
  run_guard "$guard"
  verdict_of "$RC"
  if [ "$V" = REFUSED ]; then
    echo "  *** ${label}: the guard DID NOT RUN — no verdict is available ***"
    grep -E '^ℹ ' <<<"$OUT" | head -4; fail=1; return
  fi
  if [ "$V" != "$want" ]; then
    echo "  *** ${label}: wanted ${want}, got ${V} ***"
    grep -E '^ℹ (pass|fail)' <<<"$OUT" | head -2; fail=1; return
  fi
  if [ -n "$needle" ] && ! grep -qF "$needle" <<<"$OUT"; then
    echo "  *** ${label}: ${V} but the failure does not name ${needle} ***"; fail=1; return
  fi
  echo "  ok   ${label}: ${V}${needle:+, naming $needle}"
}

echo
echo "== BASELINE: the unmutated tree is GREEN (a red below is the sabotage) =="
expect "baseline, this tree's guard" "$GUARD" GREEN
expect "baseline, PRE-FIX guard" "$OLD_GUARD" GREEN

echo
echo "== THE HOLE: a dark slab at a DYNAMIC route =="
plant "$DYN_PAGE" bg-slate-900 || true
echo "       planted /probe-dyn/[id] (paints bg-slate-900)"
expect "PRE-FIX guard, dynamic slab" "$OLD_GUARD" GREEN
expect "this tree's guard, dynamic slab" "$GUARD" RED "/probe-dyn/[id] (app/probe-dyn/[id]/page.tsx: paints bg-slate-900)"
unplant "$DYN_PAGE"

echo
echo "== CONTROL: the same slab at a STATIC route REDs under BOTH (case 1's green is about the class) =="
plant "$ST_PAGE" bg-slate-900 || true
echo "       planted /probe-static (paints bg-slate-900)"
expect "PRE-FIX guard, static slab" "$OLD_GUARD" RED "/probe-static (app/probe-static/page.tsx: paints bg-slate-900)"
expect "this tree's guard, static slab" "$GUARD" RED "/probe-static (app/probe-static/page.tsx: paints bg-slate-900)"
unplant "$ST_PAGE"

echo
echo "== NEGATIVE CONTROL: a dynamic route with NO slab stays GREEN under both =="
plant "$DYN_PAGE" bg-slate-100 || true
echo "       planted /probe-dyn/[id] (paints bg-slate-100 — below the slab floor)"
expect "PRE-FIX guard, no slab" "$OLD_GUARD" GREEN
expect "this tree's guard, no slab" "$GUARD" GREEN
unplant "$DYN_PAGE"

# The PRE-FIX copy has served its purpose; drop it before the stray check (the
# check is about PROBE ROUTES surviving, and a leftover copy is a different leak —
# asserted separately below).
rm -f "$OLD_GUARD"

# Nothing this run planted may survive: the tree must be byte-identical where it
# started, or every later `test:unit` in this worktree judges a mutated tree.
STRAY=$(git status --porcelain -- "$DYN_PAGE" "$ST_PAGE" "$OLD_GUARD" "$DYN_DIR" "$ST_DIR")
if [ -n "$STRAY" ]; then
  echo "  *** A PROBE SURVIVED THE RUN: ***"; echo "$STRAY"; fail=1
else
  echo
  echo "cleanup: no probe left behind (git status clean for every planted path)"
fi
# And the guard itself must be byte-identical: this harness drives it, never edits
# it, so any change means the run modified its own subject.
GUARD_SHA_NOW=$(sha256sum "$GUARD" | cut -d' ' -f1)
if [ "$GUARD_SHA_NOW" != "$GUARD_SHA_BEFORE" ]; then
  echo "  *** THE GUARD CHANGED DURING THE RUN ($GUARD_SHA_BEFORE -> $GUARD_SHA_NOW) ***"; fail=1
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — the pre-fix guard is blind to a dynamic-route slab, this tree's guard"
  echo "REDs naming it, and the static control REDs under both."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
