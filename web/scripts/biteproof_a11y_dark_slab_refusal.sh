#!/usr/bin/env bash
# Bite proof for the ENVIRONMENT-BROKEN path of
# web/scripts/biteproof_a11y_dark_slab_dynamic.sh (card t_08be3356).
#
# THE DEFECT IT PINS. That harness records the PINNED pre-fix guard's baseline
# (`pinned_baseline`) and judges its pinned columns by the DIFFERENCE against it. It used
# to record `PINNED_BASE_V` / `PINNED_BASE_F` / `PINNED_BASE_N` only when the pinned guard
# produced a verdict — and the columns read them anyway. So a host where the pinned copy
# could not be loaded at baseline time took the harness down instead of reporting it:
#
#   *** baseline, PRE-FIX guard: the guard DID NOT RUN — no verdict is available ***
#   == THE HOLE: a dark slab at a DYNAMIC route ==
#          planted /probe-dyn/[id] (paints bg-slate-900)
#   …/biteproof_a11y_dark_slab_dynamic.sh: line 316: PINNED_BASE_F: unbound variable
#
# The run ended there: the static control, the negative control, the stray check and the
# harness's own verdict line were never printed, and the operator got a bash crash where
# the harness had already diagnosed the real problem one line above. The DIRECTION was
# always safe (exit non-zero either way); this is the diagnosis leg — the same tax card
# t_7cad4207 removed one layer up.
#
# WHAT IS DRIVEN, AND WHY THE STUB IS NOT "THE WHOLE GUARD IS BROKEN":
#
#   * case 0 — CONTROL, the real node. The harness is `BITEPROOF PASS`, exit 0, at the
#     eight checks the register advertises, so the fix did not weaken the happy path.
#   * case 1 — the pinned guard refuses its FIRST invocation and runs normally afterwards.
#     That transient shape is the ONLY one that reaches the unbound read (the baseline has
#     no verdict, a later pinned column does), and it is measured: on the pre-fix bytes
#     this case prints `line 316: PINNED_BASE_F: unbound variable` and exits 1 with the
#     probe still planted and no verdict line. Post-fix it must name the refusal, print
#     every remaining column, still reach `BITEPROOF FAIL`, and leave no probe behind.
#   * case 2 — the pinned guard refuses EVERY invocation (the shape a tree whose
#     `web/node_modules` is absent gives: `ℹ tests 1 / ℹ pass 0 / ℹ fail 1`, which the
#     shared verdict rule already reads as a REFUSAL). The neighbouring direction: every
#     pinned column refuses by name and the report still finishes.
#
# The stub `node` DELEGATES to the real node for every other file, so the harness's own
# capability probe and its columns over THIS tree's guard run for real — a case that
# refused every invocation would prove only that bash can print a message.
#
# It mutates nothing itself: the harness it drives plants and removes its own probe routes
# under a per-worktree flock and verifies the removal, so this script needs no lock of its
# own — which is why it is not an entry in web/src/lib/harnessLock.test.ts.
#
# Run:  bash web/scripts/biteproof_a11y_dark_slab_refusal.sh
set -uo pipefail

BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve this script's directory" >&2; exit 2; }
HARNESS="$BITE_HOME/biteproof_a11y_dark_slab_dynamic.sh"
[ -f "$HARNESS" ] || {
  echo "BITEPROOF REFUSED: $HARNESS is missing, so there is no error path to drive." >&2
  exit 2
}
command -v node >/dev/null 2>&1 || {
  echo "BITEPROOF REFUSED: node is not on PATH; the target harness would refuse before its" >&2
  echo "pinned columns, so this proof would measure nothing." >&2
  exit 2
}
REAL_NODE=$(command -v node)

TMP=$(mktemp -d "${TMPDIR:-/tmp}/dark-slab-refusal.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a scratch dir for the stub node." >&2
  exit 2
}
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

fail=0
cases=0

# A stub `node` that refuses the harness's pinned pre-fix guard copy and delegates every
# other invocation (the capability probe, this tree's guard) to the real node.
#
#   make_stub <dir> <mode: first|always>
make_stub() {
  local dir="$1" mode="$2" count="$1/count"
  mkdir -p "$dir/bin" || return 1
  {
    echo '#!/usr/bin/env bash'
    echo '# Stub node: the pinned pre-fix guard copy does not load. Every other invocation'
    echo '# is the real node, so the rest of the harness still measures something.'
    echo 'set -u'
    printf 'REAL_NODE=%q\n' "$REAL_NODE"
    printf 'COUNT_FILE=%q\n' "$count"
    printf 'MODE=%q\n' "$mode"
    cat <<'STUB'
for a in "$@"; do
  case "$a" in
    *.biteproof-prefix-guard.ts)
      if [ "$MODE" = always ] || [ ! -e "$COUNT_FILE" ]; then
        : > "$COUNT_FILE"
        echo "Error: the pinned pre-fix guard copy could not be loaded (stub: no test-runner summary)" >&2
        exit 1
      fi
      ;;
  esac
done
exec "$REAL_NODE" "$@"
STUB
  } > "$dir/bin/node" || return 1
  chmod +x "$dir/bin/node" || return 1
}

# run_harness <stub-dir|''> <label>; sets OUT and RC.
run_harness() {
  local stub="$1" rc=0
  if [ -n "$stub" ]; then
    OUT=$(PATH="$stub/bin:$PATH" bash "$HARNESS" 2>&1) || rc=$?
  else
    OUT=$(bash "$HARNESS" 2>&1) || rc=$?
  fi
  RC=$rc
}

report() { printf '%s\n' "$OUT" | sed 's/^/        /'; }

# --- case 0
cases=$((cases + 1))
echo
echo "== case 0: CONTROL — the real node: the happy path is unchanged"
run_harness ''
checks=$(grep -cE '^  (ok|base )' <<<"$OUT")
if [ "$RC" = 0 ] && grep -q '^BITEPROOF PASS' <<<"$OUT" && [ "$checks" = 8 ]; then
  echo "   OK (BITEPROOF PASS, exit 0, 8 checks)"
else
  echo "   CONTROL NOT GREEN — the harness's happy path is broken on this tree, so no"
  echo "   verdict below could be attributed to the environment stub (exit $RC, $checks checks):"
  report
  fail=1
fi

# --- case 1
cases=$((cases + 1))
echo
echo "== case 1: the pinned guard cannot load at BASELINE time but can afterwards"
if make_stub "$TMP/first" first; then
  run_harness "$TMP/first"
  problems=''
  [ "$RC" != 0 ] || problems="${problems}   exit 0 where the pinned baseline had no verdict\n"
  grep -q 'unbound variable' <<<"$OUT" && problems="${problems}   still dies on a bash 'unbound variable' instead of naming the refusal\n"
  grep -qE 'PRE-FIX guard, dynamic slab: NOT JUDGED' <<<"$OUT" || problems="${problems}   the first pinned column does not name its refusal\n"
  grep -qF '"the failure set did not move" has nothing to be measured against' <<<"$OUT" \
    || problems="${problems}   the refusal does not say WHAT cannot be measured\n"
  [ "$(grep -c 'NOT JUDGED' <<<"$OUT")" = 3 ] || problems="${problems}   every pinned column must refuse (wanted 3 'NOT JUDGED', got $(grep -c 'NOT JUDGED' <<<"$OUT"))\n"
  grep -q 'this tree.s guard, dynamic slab: RED, naming /probe-dyn/\[id\]' <<<"$OUT" \
    || problems="${problems}   this tree's guard did not print a RED naming /probe-dyn/[id] — the stub must not silence it, and the run may have stopped before it\n"
  grep -q '== NEGATIVE CONTROL' <<<"$OUT" || problems="${problems}   the report stopped before the negative control\n"
  grep -q '^cleanup: no probe left behind' <<<"$OUT" || problems="${problems}   the report stopped before the stray check\n"
  grep -q '^BITEPROOF FAIL' <<<"$OUT" || problems="${problems}   the run ends without a harness verdict line\n"
  if [ -z "$problems" ]; then
    echo "   OK (exit $RC, 3 named refusals, every remaining column printed, no unbound variable)"
    grep -E 'NOT JUDGED|BITEPROOF FAIL' <<<"$OUT" | sed 's/^/        /' | head -4
  else
    echo "   NOT FIXED — the environment-broken path does not report:"
    printf "$problems"
    report
    fail=1
  fi
else
  echo "   STUB NOT BUILT — cannot write $TMP/first/bin/node"
  fail=1
fi

# --- case 2
cases=$((cases + 1))
echo
echo "== case 2: the pinned guard cannot load AT ALL (the tree-without-node_modules shape)"
if make_stub "$TMP/always" always; then
  run_harness "$TMP/always"
  problems=''
  [ "$RC" != 0 ] || problems="${problems}   exit 0 where the pinned guard never ran\n"
  grep -q 'unbound variable' <<<"$OUT" && problems="${problems}   still dies on a bash 'unbound variable'\n"
  grep -qE 'baseline, PRE-FIX guard: the guard DID NOT RUN' <<<"$OUT" || problems="${problems}   the baseline does not name its refusal\n"
  grep -qE 'PRE-FIX guard, static slab: the guard DID NOT RUN' <<<"$OUT" || problems="${problems}   the static pinned column does not name its refusal\n"
  grep -q '== NEGATIVE CONTROL' <<<"$OUT" || problems="${problems}   the report stopped before the negative control\n"
  grep -q '^BITEPROOF FAIL' <<<"$OUT" || problems="${problems}   the run ends without a harness verdict line\n"
  if [ -z "$problems" ]; then
    echo "   OK (exit $RC, every pinned column named its refusal, report finished)"
  else
    echo "   NOT FIXED — a permanently un-runnable pinned guard is not reported:"
    printf "$problems"
    report
    fail=1
  fi
else
  echo "   STUB NOT BUILT — cannot write $TMP/always/bin/node"
  fail=1
fi

echo
if [ "$fail" = 0 ]; then
  echo "BITEPROOF PASS — the dark-slab harness reports an un-runnable PINNED guard by name, prints"
  echo "every remaining column, and still reaches its own verdict instead of dying on"
  echo "\`PINNED_BASE_F: unbound variable\` ($cases cases)."
else
  echo "BITEPROOF FAIL ($fail failing check(s), $cases cases)"
fi
exit "$fail"
