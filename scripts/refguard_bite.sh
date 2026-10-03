#!/usr/bin/env bash
#
# Bite proof for scripts/refguard.sh: the guard whose whole product is a trustworthy
# refusal. A guard whose suite still passes with the guard neutered measures nothing, so
# this sabotages ONE behaviour of the guard at a time in a throwaway copy, runs the
# COMMITTED suite against it via REFGUARD_UNDER_TEST, and requires the suite to go RED
# each time — then runs the unmodified guard as the control and requires GREEN.
#
# WHY THIS FILE EXISTS (card t_bac5a2c5). t_dc1200ff closed claiming "offline suite, 100
# assertions, proven to bite (6/6 sabotages red, control green)". The SUITE is in the repo;
# the harness that ran the sabotages was NOT, so nothing in the repo could re-run that
# proof — refguard was the only gate claiming a bite proof with no committed way to produce
# it. The convention followed here is the `backup` gate's (deploy/backup/
# jomform-backup_bite.sh): a committed *_bite.sh beside the suite, on demand.
#
# TWO RULES THE PARENT CARD INSISTS ON, both honoured:
#
#   1. REFUSE A SABOTAGE THAT DOES NOT APPLY. A sed that no longer matches leaves a
#      byte-identical copy, and that row would silently test nothing. Every row `cmp -s`es
#      the mutated copy against the source and FAILS (not passes) when they are identical.
#      This is not theoretical: an earlier hand-written harness caught its own `no-siblings`
#      row this way, because the sed used an unescaped `#` delimiter and never applied.
#
#   2. VALUE-LEVEL SABOTAGES ONLY. Never delete a placeholder or an argument — that changes
#      argument counts and the mutation fails for the wrong reason rather than for the one
#      it is meant to demonstrate.
#
# NOT WIRED INTO THE `refguard` GATE, deliberately: it is minutes long and it mutates, so
# like the backup bite proof it is on-demand only. scripts/preflight.sh says so.
#
# The sabotages are the SIX the original run recorded (kept in
# boards/jomform/evidence/t_dc1200ff-refguard-bite.log), so a re-run reproduces that table
# rather than inventing a new one.
#
# Run:  bash scripts/refguard_bite.sh
# Exit: 0 only if the control is GREEN and EVERY sabotage was caught by the suite.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$HERE/refguard.sh"
SUITE="$HERE/refguard_test.sh"

[ -f "$GUARD" ] || { echo "REFUSED: $GUARD is missing" >&2; exit 2; }
[ -f "$SUITE" ] || { echo "REFUSED: $SUITE is missing" >&2; exit 2; }

SB="$(mktemp -d "${TMPDIR:-/tmp}/rg-bite.XXXXXX")"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

fail=0

# run_suite <guard-path> -> echoes the suite's summary line; returns the suite's rc
run_suite() {
  local g="$1" out rc
  out="$(REFGUARD_UNDER_TEST="$g" bash "$SUITE" 2>&1)"; rc=$?
  printf '%s\n' "$out" > "$SB/last.out"
  printf '%s\n' "$(grep -E 'refguard self-test:' "$SB/last.out" | tail -n 1)"
  return $rc
}

# sabotage <name> <python-expression-to-rewrite-the-guard>
#
# The rewrite is a python one-liner applied to the source; it must change the text. The
# mutated copy is compared with the source and the row FAILS if they are identical — the
# "a sabotage that did not apply" trap.
sabotage() {
  local name="$1" py="$2" rc summary
  local copy="$SB/$name.sh"
  cp "$GUARD" "$copy"
  python3 - "$copy" "$py" <<'PY' || { echo "  *** SABOTAGE '$name' DID NOT APPLY (the rewrite raised) ***"; fail=1; return; }
import io, sys
path, expr = sys.argv[1], sys.argv[2]
s = io.open(path, encoding="utf-8").read()
out = eval(expr, {"s": s})
if out == s:
    sys.exit("the rewrite changed nothing")
io.open(path, "w", encoding="utf-8").write(out)
PY
  if cmp -s "$copy" "$GUARD"; then
    echo "  *** SABOTAGE '$name' LEFT A BYTE-IDENTICAL COPY — this row would prove nothing ***"
    fail=1
    return
  fi
  echo "  mutation on disk: $name"
  summary="$(run_suite "$copy")"; rc=$?
  if [ $rc -ne 0 ]; then
    echo "      $name: suite RED (rc=$rc) — $summary"
    grep -E '^ *FAIL ' "$SB/last.out" | head -n 4 | sed 's/^/          /'
  else
    echo "  *** $name WAS NOT CAUGHT — the suite stayed GREEN (rc=$rc): $summary ***"
    fail=1
  fi
}

echo "=== control: the real guard must be GREEN ==="
summary="$(run_suite "$GUARD")"; rc=$?
if [ $rc -eq 0 ]; then
  echo "    control rc=0 | $summary"
else
  echo "  *** CONTROL FAILED: the unmodified guard does not pass its own suite (rc=$rc): $summary ***"
  grep -E '^ *FAIL ' "$SB/last.out" | head -n 6 | sed 's/^/      /'
  fail=1
fi

echo
echo "=== sabotage 1: the hook is installed but does nothing (a no-op) ==="
# hook_main's verdict line: make it a no-op by returning immediately.
sabotage noop 's.replace("hook_main() {\n", "hook_main() {\n  return 0\n", 1)'

echo
echo "=== sabotage 2: the hook returns at the FIRST violation instead of reading to EOF ==="
# Value-level: the read loop's exit becomes an early return, so later violations are missed.
sabotage earlyexit 's.replace("while IFS= read -r line; do\n", "while IFS= read -r line; do\n    return 0\n", 1)'

echo
echo "=== sabotage 3: the allow-list hardcodes the first remote, so a second one breaks ==="
sabotage hardorigin 's.replace("remote_names() {", "remote_names() {\n  printf %s\\\\n origin; return 0\n", 1)'

echo
echo "=== sabotage 4: install leaves the hook non-executable ==="
sabotage noexec 's.replace("chmod 755 \"$tmp\"", "chmod 644 \"$tmp\"", 1)'

echo
echo "=== sabotage 5: a DELETE is refused too, so the namespace becomes uncleanable ==="
sabotage nodel 's.replace("is_remote_tracking() {", "is_remote_tracking() {\n  return 0\n", 1)'

echo
echo "=== sabotage 6: scan reports a verdict instead of refusing when it has no live data ==="
# Value-level: the two in the "cannot measure" gate become zero, so it answers instead of
# exiting 2.
sabotage nolivefail 's.replace("refusing to report a staleness verdict from no live data", "reporting anyway", 1).replace("      return 2", "      return 0", 1)'

echo
if [ "$fail" -eq 0 ]; then
  echo "REFGUARD BITE PROOF PASS — the control was GREEN and every sabotage turned the suite RED."
else
  echo "REFGUARD BITE PROOF FAILED — see the *** lines above."
fi
exit $fail
