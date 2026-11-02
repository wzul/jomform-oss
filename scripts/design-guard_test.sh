#!/usr/bin/env bash
# scripts/design-guard_test.sh — the design gate's own verdict suite.
#
# WHAT IT PROVES. `scripts/design-guard.py` is a SOURCE SCANNER: it cannot be
# mutated at a call site the way a behavioural test can, so the evidence that it
# bites is a planted literal in a file it really scans, plus a control that the
# same scan of the same bytes WITHOUT the plant is green. Every case below runs
# the shipped scanner (never a copy of it) over a sandbox tree built for that
# case, and asserts BOTH the exit code AND the text that explains it — a red for
# the wrong reason is not a bite.
#
# The last two cases are the real ones: the live `web/src` must be GREEN (that
# is the gate), and a COPY of it with one literal planted in a real component
# must be RED naming that file and line. Measuring in a copy is deliberate —
# planting in the live tree would make a concurrent run read bytes this script
# wrote.
#
# Offline: no network, no database, no browser, ~1s.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
GUARD="$HERE/design-guard.py"
SB="$(mktemp -d "${TMPDIR:-/tmp}/design-guard-test.XXXXXX")"
trap 'rm -rf "$SB"' EXIT

PASS=0
FAIL=0
ok()   { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; }

# run_guard <root> — capture rc and output of the shipped scanner.
run_guard() {
  OUT="$(python3 "$GUARD" --root "$1" 2>&1)"
  RC=$?
}

# case_rc <name> <root> <expected-rc> — exit code only.
case_rc() {
  local name="$1" root="$2" want="$3"
  run_guard "$root"
  if [ "$RC" = "$want" ]; then ok "$name (rc=$RC)"; else
    bad "$name — expected rc=$want, got rc=$RC; output: $OUT"
  fi
}

# case_out <name> <root> <expected-rc> <substring> — exit code AND the sentence
# that explains it, so a failure cannot pass for an unrelated reason.
case_out() {
  local name="$1" root="$2" want="$3" needle="$4"
  run_guard "$root"
  if [ "$RC" != "$want" ]; then
    bad "$name — expected rc=$want, got rc=$RC; output: $OUT"
    return
  fi
  case "$OUT" in
    *"$needle"*) ok "$name (rc=$RC, names \"$needle\")" ;;
    *) bad "$name — rc=$RC but the output never says \"$needle\"; output: $OUT" ;;
  esac
}

# mk_tree <dir> — a minimal sandbox root: one .tsx and the token stylesheet.
# The stylesheet carries the reduced-motion baseline because the gate requires
# one in every tree it scans; a case that wants to prove the requirement bites
# removes it explicitly (case 17).
mk_tree() {
  mkdir -p "$1/web/src/app" "$1/web/src/components"
  cat > "$1/web/src/app/globals.css" <<'CSS'
:root { --jf-primary: #5e6ad2; }
@media (prefers-reduced-motion: reduce) {
  * { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; }
}
CSS
  cat > "$1/web/src/app/page.tsx" <<'TSX'
export default function Page() {
  return <div className="text-jf-2xs text-[var(--jf-primary)]">ok</div>;
}
TSX
}

# ─── 1. the control: a clean tree is GREEN ─────────────────────────────────
mk_tree "$SB/clean"
case_out "a clean tree passes and reports what it scanned" "$SB/clean" 0 "1 .tsx file(s) scanned"

# ─── 2. a raw six-digit hex in code is RED, naming file:line + literal ─────
mk_tree "$SB/hex"
printf 'export const X = () => <p className="bg-[#ff00ff]">x</p>;\n' >> "$SB/hex/web/src/app/page.tsx"
case_out "a planted #rrggbb in code is refused" "$SB/hex" 1 "app/page.tsx:4"
run_guard "$SB/hex"
case "$OUT" in
  *"#ff00ff"*) ok "the finding names the literal (#ff00ff)" ;;
  *) bad "the finding does not name the literal; output: $OUT" ;;
esac

# ─── 3. a raw arbitrary font size is RED ───────────────────────────────────
mk_tree "$SB/px"
printf 'export const Y = () => <p className="text-[17px]">x</p>;\n' >> "$SB/px/web/src/app/page.tsx"
case_out "a planted text-[17px] is refused" "$SB/px" 1 "text-[17px]"

# ─── 4/5. a hex in a comment is NOT code ───────────────────────────────────
mk_tree "$SB/comment"
cat >> "$SB/comment/web/src/app/page.tsx" <<'TSX'
// #5e6ad2 on white measures 4.70:1 — a measured value belongs in the rationale
/*
  the same figure quoted in a block comment:
  #5e6ad2 is 4.70:1
*/
export const Z = 1;
TSX
case_out "a hex in a // comment and in a /* */ block is not a finding" "$SB/comment" 0 "0 raw hex literals"

# ─── 6. a URL fragment is not a colour (the false-positive control) ────────
mk_tree "$SB/frag"
cat >> "$SB/frag/web/src/app/page.tsx" <<'TSX'
export const NAV = [{ href: "/docs/customize#affiliate-marketing", label: "Aff" }];
export const ISSUE = "#503"; // an issue reference, not a colour
TSX
case_out "a URL fragment and an all-digit 3-char reference are not findings" "$SB/frag" 0 "0 raw hex literals"

# ─── 7. a 3-digit hex WITH a letter IS a colour ────────────────────────────
mk_tree "$SB/short"
printf 'export const W = () => <path stroke="#fff" />;\n' >> "$SB/short/web/src/app/page.tsx"
case_out "a 3-digit hex with a letter (#fff) is refused" "$SB/short" 1 "#fff"

# ─── 8. a trailing exemption with a reason passes and is LISTED ────────────
mk_tree "$SB/trail"
printf 'export const PICK = "#7c3aed"; // design-tokens-exempt: a persisted colour-field default\n' >> "$SB/trail/web/src/app/page.tsx"
run_guard "$SB/trail"
if [ "$RC" = 0 ] && printf '%s' "$OUT" | grep -q 'exempt web/src/app/page.tsx:4'; then
  ok "a trailing exemption with a reason passes and is named in the pass output"
else
  bad "a trailing exemption with a reason did not pass/list; output: $OUT"
fi

# ─── 9. an exemption with NO reason does not exempt anything ───────────────
mk_tree "$SB/noreason"
printf 'export const PICK = "#7c3aed"; // design-tokens-exempt:\n' >> "$SB/noreason/web/src/app/page.tsx"
case_out "an exemption with no reason is a FAILURE, not a pass" "$SB/noreason" 2 "no reason"

# ─── 10. a block exemption covers the lines up to its end marker ───────────
mk_tree "$SB/block"
cat >> "$SB/block/web/src/app/page.tsx" <<'TSX'
// design-tokens-exempt: the palette below IS the rendered content
export const PALETTE = [
  { name: "A", hex: "#6366f1" },
  { name: "B", hex: "#8b5cf6" },
];
// design-tokens-exempt-end
export const AFTER = 1;
TSX
run_guard "$SB/block"
if [ "$RC" = 0 ] && printf '%s' "$OUT" | grep -q '2 documented exemption'; then
  ok "a block exemption covers its range and reports both literals"
else
  bad "a block exemption did not cover its range; output: $OUT"
fi

# ─── 10b. the SAME block, without its end marker, is a FAILURE ─────────────
mk_tree "$SB/unclosed"
cat >> "$SB/unclosed/web/src/app/page.tsx" <<'TSX'
// design-tokens-exempt: the palette below IS the rendered content
export const PALETTE = [
  { name: "A", hex: "#6366f1" },
];
TSX
case_out "an unterminated block exemption is a FAILURE" "$SB/unclosed" 2 "never closed"

# ─── 10c. the block BOUNDARY is real: a literal after the end marker still bites
mk_tree "$SB/after"
cat >> "$SB/after/web/src/app/page.tsx" <<'TSX'
// design-tokens-exempt: the palette below IS the rendered content
export const PALETTE = [{ hex: "#6366f1" }];
// design-tokens-exempt-end
export const LEAK = () => <p className="bg-[#123456]" />;
TSX
case_out "a literal AFTER the end marker is still refused" "$SB/after" 1 "#123456"

# ─── 10d. a stray end marker is a FAILURE (a broken exemption is not silent) ─
mk_tree "$SB/stray"
printf '// design-tokens-exempt-end\n' >> "$SB/stray/web/src/app/page.tsx"
case_out "a stray end marker is a FAILURE" "$SB/stray" 2 "no open block"

# ─── 11. globals.css is where literals are allowed ─────────────────────────
mk_tree "$SB/css"
printf '.x { background: #abcdef; }\n' >> "$SB/css/web/src/app/globals.css"
case_out "a hex in globals.css is not scanned (it is the token file)" "$SB/css" 0 "0 raw hex literals"

# ─── 12. a .test.tsx fixture is not shipped code ───────────────────────────
mk_tree "$SB/testfile"
printf 'export const F = () => <p className="bg-[#ff0000]" />;\n' > "$SB/testfile/web/src/app/page.test.tsx"
case_out "a .test.tsx fixture is not scanned" "$SB/testfile" 0 "1 .tsx file(s) scanned"

# ─── 13. a missing / empty tree is an ERROR, never a pass ──────────────────
mkdir -p "$SB/nosrc"
case_out "a missing web/src fails loudly" "$SB/nosrc" 2 "does not exist"
mkdir -p "$SB/empty/web/src"
case_out "a web/src with no .tsx fails loudly (an empty scan proves nothing)" "$SB/empty" 2 "0 .tsx files found"

# ─── 15. transition-all is a motion defect (card t_d5336ec6) ───────────────
mk_tree "$SB/allprop"
printf 'export const T = () => <div className="transition-all duration-[var(--jf-dur-base)]" />;\n' >> "$SB/allprop/web/src/app/page.tsx"
case_out "a planted transition-all is refused" "$SB/allprop" 1 "transition-all"
run_guard "$SB/allprop"
case "$OUT" in
  *"transitions EVERY property"*) ok "the transition-all finding says why it is refused" ;;
  *) bad "the transition-all finding has no rationale; output: $OUT" ;;
esac

# ─── 16. a raw duration is the animation's raw hex ─────────────────────────
mk_tree "$SB/durms"
printf 'export const D = () => <div className="transition-colors duration-150ms" />;\n' >> "$SB/durms/web/src/app/page.tsx"
case_out "a planted duration-150ms is refused" "$SB/durms" 1 "duration-150ms"
mk_tree "$SB/durbracket"
printf 'export const E = () => <div className="transition-colors duration-[300ms]" />;\n' >> "$SB/durbracket/web/src/app/page.tsx"
case_out "a planted duration-[300ms] is refused" "$SB/durbracket" 1 "duration-[300ms]"
# CONTROL: the token form and a colour transition are NOT findings. Without
# this, "red on a planted duration" could just mean any duration-shaped string
# is red, which would red the correct tree.
mk_tree "$SB/durtoken"
printf 'export const F = () => <div className="transition-colors duration-[var(--jf-dur-base)]" />;\n' >> "$SB/durtoken/web/src/app/page.tsx"
case_out "the token form duration-[var(--jf-dur-base)] is GREEN" "$SB/durtoken" 0 "0 raw durations"

# ─── 17. the reduced-motion baseline is required, and must collapse ────────
mk_tree "$SB/noreduce"
cat > "$SB/noreduce/web/src/app/globals.css" <<'CSS'
:root { --jf-primary: #5e6ad2; }
CSS
case_out "a globals.css with no reduced-motion block is refused" "$SB/noreduce" 1 "prefers-reduced-motion"
mk_tree "$SB/emptydurations"
cat > "$SB/emptydurations/web/src/app/globals.css" <<'CSS'
:root { --jf-primary: #5e6ad2; }
@media (prefers-reduced-motion: reduce) {
  * { animation-duration: 0.01ms !important; }
}
CSS
case_out "a reduced-motion block that never collapses transition-duration is refused" "$SB/emptydurations" 1 "transition-duration"
# CONTROL: the baseline the real stylesheet carries passes (mk_tree above).

# ─── 18. the REAL baseline is checked, not just the rule text ──────────────
# A copy of the real tree with the baseline removed must be RED for that reason
# — proving the gate reads the stylesheet the app ships.
mkdir -p "$SB/realcss/web"
cp -a "$ROOT/web/src" "$SB/realcss/web/src"
python3 - "$SB/realcss/web/src/app/globals.css" <<'PY'
import sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
s = s.replace("@media (prefers-reduced-motion: reduce)", "@media (prefers-reduced-motion: no-preference)", 1)
open(p, "w", encoding="utf-8").write(s)
PY
case_out "the REAL tree with its motion baseline removed is RED" "$SB/realcss" 1 "prefers-reduced-motion"
cp -a "$ROOT/web/src/app/globals.css" "$SB/realcss/web/src/app/globals.css"
case_out "the same copy with the real baseline restored is GREEN" "$SB/realcss" 0 "reduced-motion baseline present"

# ─── 14. the REAL tree is green, and a COPY of it with one plant is red ────
run_guard "$ROOT"
if [ "$RC" = 0 ]; then
  ok "the live web/src is GREEN ($(printf '%s' "$OUT" | sed -n 's/.*PASS — \([0-9]*\) .tsx.*/\1/p') .tsx files)"
else
  bad "the live web/src is RED — the gate's own tree does not pass; output: $OUT"
fi
case "$OUT" in
  *"0 raw hex literals, 0 arbitrary font sizes"*) ok "the live pass reports zero findings in both rules" ;;
  *) bad "the live pass does not report zero findings; output: $OUT" ;;
esac

mkdir -p "$SB/real/web"
cp -a "$ROOT/web/src" "$SB/real/web/src"
# Plant inside a REAL component, on a line that was clean before the plant.
PLANT="web/src/app/builder/page.tsx"
printf '\nexport const DESIGN_GUARD_PLANT = () => <p className="bg-[#0f0f0f]" />;\n' >> "$SB/real/$PLANT"
case_out "a copy of the REAL tree with one planted hex is RED" "$SB/real" 1 "app/builder/page.tsx:"
run_guard "$SB/real"
case "$OUT" in
  *"#0f0f0f"*) ok "the real-tree plant is named by value (#0f0f0f)" ;;
  *) bad "the real-tree plant is not named by value; output: $OUT" ;;
esac
# And the CONTROL for that plant: the same copy, unplanted, is green. Without
# this, "red after planting" could just mean the copy was red already.
rm "$SB/real/$PLANT"
cp -a "$ROOT/$PLANT" "$SB/real/$PLANT"
case_out "the same copy WITHOUT the plant is green (the plant is what bit)" "$SB/real" 0 "0 raw hex literals"

# The two motion rules, planted in the SAME real component, each red by value
# and each followed by the unplanted control.
printf '\nexport const MOTION_GUARD_PLANT_A = () => <div className="transition-all" />;\n' >> "$SB/real/$PLANT"
case_out "a copy of the REAL tree with a planted transition-all is RED" "$SB/real" 1 "app/builder/page.tsx:"
cp -a "$ROOT/$PLANT" "$SB/real/$PLANT"
case_out "…and green again once the transition-all plant is removed" "$SB/real" 0 "0 transition-all"
printf '\nexport const MOTION_GUARD_PLANT_B = () => <div className="duration-250ms" />;\n' >> "$SB/real/$PLANT"
case_out "a copy of the REAL tree with a planted duration-250ms is RED" "$SB/real" 1 "duration-250ms"
cp -a "$ROOT/$PLANT" "$SB/real/$PLANT"
case_out "…and green again once the duration plant is removed" "$SB/real" 0 "0 raw durations"

# ─── verdict ───────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: design guard suite FAILED — $PASS passed, $FAIL failed"
  exit 1
fi
echo "RESULT: design guard suite passed — $PASS checks, 0 failed"
