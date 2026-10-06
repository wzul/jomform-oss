#!/usr/bin/env bash
# Bite proof for web/src/lib/saveButton.test.ts.
#
# Each case SABOTAGES one property the guard pins, runs the guard, and asserts it
# goes RED. Every mutated file is restored byte-identically (sha256 re-checked at
# the end). Sources are copied into a per-run `mktemp -d` rather than a
# host-global name, so two worktrees running this concurrently cannot corrupt
# each other — the failure mode a sibling harness in this repo was caught with.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
export PATH="/root/.hermes/tools/node-26.7.0-linux-x64/bin:${PATH}"
GUARD=src/lib/saveButton.test.ts
TMP="$(mktemp -d)"

declare -A FILES=(
  [savebtn]="src/components/SaveButton.tsx"
  [settings]="src/app/settings/page.tsx"
  [dict]="src/components/i18nDict.ts"
)
declare -A SUM
for k in "${!FILES[@]}"; do
  cp "${FILES[$k]}" "$TMP/$k.orig"
  SUM[$k]=$(sha256sum "${FILES[$k]}" | cut -d' ' -f1)
done
restore_all() { for k in "${!FILES[@]}"; do cp "$TMP/$k.orig" "${FILES[$k]}"; done; }
trap 'restore_all; rm -rf "$TMP"' EXIT

run_guard() { node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1; }
fail=0
expect_red() {
  if printf '%s' "$2" | grep -qE '^ℹ fail [1-9]'; then echo "  ✅ BITES — $1";
  else echo "  ❌ DID NOT BITE — $1 (the guard is not protecting this)"; fail=1; fi
}

echo "case 0: baseline (unsabotaged must be GREEN)"
out=$(run_guard); printf '%s' "$out" | grep -E '^ℹ (pass|fail)'
if printf '%s' "$out" | grep -qE '^ℹ fail 0'; then echo "  ✅ baseline green"; else echo "  ❌ baseline not green"; fail=1; fi

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
echo "RESULT: $([ $fail -eq 0 ] && echo 'ALL CASES BIT — exit 0' || echo 'A CASE DID NOT BITE — exit 1')"
exit $fail
