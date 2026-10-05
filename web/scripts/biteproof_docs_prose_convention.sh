#!/usr/bin/env bash
#
# web/scripts/biteproof_docs_prose_convention.sh — the bite proof for
# web/src/lib/docsProseConvention.test.ts (card t_0447a70c).
#
# WHY THIS EXISTS
#   The guard asserts that no /docs prose carries a backtick marker, because every
#   /docs guide renders its section bodies as plain text nodes and the markers
#   therefore reach the reader. A guard like that has one failure mode that a green
#   gate cannot see: a walk that collects nothing (a wrong anchor, a parser that
#   stops at the module scope, a walker pointed at the wrong directory) satisfies
#   "no string carries a backtick" without looking at a single body. So this proves
#   the guard GOES RED when a marker is present, in TWO pages, and stays GREEN on
#   the shapes that must not be reported — a backtick in a COMMENT (a text scanner
#   would flag it; the AST does not), a JSX TEMPLATE LITERAL showing code, and a
#   template literal INSIDE <code>/<pre> even when its text carries a backtick.
#
#   CARD t_8580357e WIDENED THE GUARD past `ts.StringLiteral`-only, and this
#   harness now drives the two shapes that widening added, each of which the
#   string-literal-only walk let through GREEN:
#     * CASE 7 — a marker in RAW JSX TEXT (a `ts.JsxText`), e.g. <p>… `m` …</p>.
#     * CASE 8 — a marker ESCAPED inside a TEMPLATE LITERAL, which reaches the
#       reader as a backtick because `node.text` decodes `\``.
#   Both must be RED, and they must be RED *for the widened reason*: each case
#   asserts node's own count, one failing test (the findings rule) and zero on the
#   vacuity test, so a guard that only ever widened the FINDINGS walk (and left
#   the vacuity walk string-only) is caught by CASE 7/8 too. CASE 9 is the
#   negative control that keeps the widening honest: the SAME escaped-backtick
#   template literal inside a <code> element stays GREEN, because that is the
#   supported way to show code on /docs.
#
#   CARD t_763c28c2 ADDED THE SECOND SURFACE — prose IMPORTED from a generated
#   module and printed with a JSX expression, which the DOCS_DIR walk cannot reach:
#     * CASE 10 — a marker in web/src/lib/apiExamples.gen.ts (the /docs/api worked
#       examples' notes, generated from api/internal/httpapi/openapi_examples.go),
#       which must be RED naming that file.
#     * CASE 11 — the negative control: a backtick inside `curl`, which the page
#       renders inside <code>, must stay GREEN, so the new surface does not flag
#       every code sample.
#
# WHAT IT DOES
#   It snapshots each file it mutates into a scratch dir, applies one mutation,
#   runs the REAL guard, requires the verdict this case predicts, and restores the
#   file from its snapshot (a cmp check, not a guess — and deliberately NOT
#   `git checkout`, which would revert the other files in this branch's own diff).
#
# NOT A GATE. It is on demand, run by the author of a change that touches the guard
#   or the docs prose it reads. See BITE-HARNESSES.md.
#
# Usage:  bash web/scripts/biteproof_docs_prose_convention.sh
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
web="$(cd "$here/.." && pwd)"
cd "$web" || exit 2

GUARD="src/lib/docsProseConvention.test.ts"
[ -f "$GUARD" ] || { echo "harness: $GUARD is missing — nothing to drive"; exit 2; }

snap="$(mktemp -d)"
trap 'rm -rf "$snap"' EXIT

run_guard() { node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1; }

# RED / GREEN / REFUSED, never inferred from the absence of a failure line: a
# guard that cannot load reports `tests 1 / pass 0 / fail 1`, and calling that
# GREEN would print a bite for a case that wanted RED while the instrument was
# broken (the rule web/scripts/biteproof_docs_tool_claims.sh established).
verdict() {
  local o="$1"
  if printf '%s' "$o" | grep -qE '^ℹ fail [1-9]' && printf '%s' "$o" | grep -qE '^ℹ pass [1-9]'; then echo RED; return; fi
  if printf '%s' "$o" | grep -qE '^ℹ pass [1-9]' && printf '%s' "$o" | grep -qE '^ℹ fail 0' && printf '%s' "$o" | grep -qE '^✔ '; then echo GREEN; return; fi
  echo REFUSED
}

fail=0
declare -a VERDICTS=()

snapshot() { cp -p "$1" "$snap/$(echo "$1" | tr / _)"; }
restore() {
  local f="$1" s="$snap/$(echo "$1" | tr / _)"
  cp -p "$s" "$f"
  cmp -s "$s" "$f" || { echo "  RESTORE MISMATCH: $f"; fail=1; }
}

echo "== CASE 0 (control): the unmutated tree must be GREEN =="
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case0=$v")
if [ "$v" = GREEN ]; then
  echo "  control: GREEN (the guard is not a tautology — it passes on the fixed tree)"
else
  echo "  control FAILED: expected GREEN, got $v"; printf '%s\n' "$out" | tail -25; exit 2
fi

# ── the HOLE cases: a marker present, the guard must go RED naming the page ────
echo "== CASE 1: the shipped defect re-inserted — a backtick span on /docs/chat =="
f="src/app/docs/chat/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/The fourth creates: upsert_form is proposable/The fourth creates: `upsert_form` is proposable/' "$f"
grep -q 'The fourth creates: `upsert_form`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case1=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/chat/page.tsx'; then
  echo "  BITES: RED naming docs/chat/page.tsx (the exact sentence /docs/chat shipped)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/chat)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 2: a marker in a DIFFERENT page — the defect was site-wide (4 pages) =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/over MCP with list_trusted_devices, revoke_trusted_device/over MCP with `list_trusted_devices`, revoke_trusted_device/' "$f"
grep -q 'with `list_trusted_devices`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case2=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/security/page.tsx'; then
  echo "  BITES: RED naming docs/security/page.tsx (the guard is not anchored on one file)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 3: a marker in a THIRD page (/docs/sales) =="
f="src/app/docs/sales/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/reports the mode as mode \(payment/reports the mode as `mode` (payment/' "$f"
grep -q 'as `mode` (payment' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case3=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/sales/page.tsx'; then
  echo "  BITES: RED naming docs/sales/page.tsx"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/sales)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the NEGATIVE controls: shapes the guard must NOT report ───────────────────
echo "== CASE 4 (negative control): a backtick in a COMMENT stays GREEN =="
f="src/app/docs/chat/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's|^import type \{ Metadata \} from "next";|import type { Metadata } from "next";\n// qa-probe: a COMMENT naming `do_not_flag_me` and `also_not` — a text scanner would report this, an AST cannot.|' "$f"
grep -q 'qa-probe: a COMMENT naming' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case4=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a comment is not a string literal, so the AST walk never sees it"
else
  echo "  CONTROL FAILED: verdict=$v — the guard is a text scan, not a parse"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 5 (negative control): a JSX template literal showing code stays GREEN =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The probe is a JSX template literal — the supported way to show code on a docs
# page — inserted at module scope right after the import so it parses as TSX
# without needing to find a render site (the AST walk covers the whole file).
printf 'const QA_PROBE = <p>{`a code sample: get_form_analytics`}</p>;\n' > "$snap/probe.tsx"
sed -i "/^import type { Metadata } from \"next\";/r $snap/probe.tsx" "$f"
grep -q 'a code sample: get_form_analytics' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case5=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a template literal is NoSubstitutionTemplateLiteral, not a StringLiteral"
else
  echo "  CONTROL FAILED: verdict=$v — the supported way to show code on /docs was reported"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the guard must also be looking at the tree, not a cached set ──────────────
echo "== CASE 6: a marker in the shared docs LAYOUT (a different .tsx, not a page) =="
f="src/app/docs/layout.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/label: "Overview & features"/label: "Overview \& features `probe`"/' "$f"
grep -q 'features `probe`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case6=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/layout.tsx'; then
  echo "  BITES: RED naming docs/layout.tsx (the walk covers the shared chrome too)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/layout.tsx)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# The widened cases (card t_8580357e) must be RED *for the widened reason*: node's
# own count is exactly ONE failing test, and the test that fails is the FINDINGS
# rule, not the vacuity rule. A guard that widened the findings walk but left the
# vacuity walk string-literal-only would fail this (the vacuity test would not see
# the new node and could not be the failing one), which is the point of asserting
# the name rather than the count alone.
fails_findings_only() {
  local o="$1"
  printf '%s' "$o" | grep -qE '^ℹ fail 1$' &&
    printf '%s' "$o" | grep -q '^✖ no /docs prose carries a backtick marker' &&
    ! printf '%s' "$o" | grep -q '^✖ the guard is reading the real docs tree'
}

# The IMPORTED-prose surface (card t_763c28c2) fails in its OWN test, not the
# DOCS_DIR one: a marker in a generated module the /docs/api page prints is
# reported by `no /docs imported prose carries a backtick marker`, so the
# attribution rule is the same shape with that test's name.
fails_imported_findings_only() {
  local o="$1"
  printf '%s' "$o" | grep -qE '^ℹ fail 1$' &&
    printf '%s' "$o" | grep -q '^✖ no /docs imported prose carries a backtick marker' &&
    ! printf '%s' "$o" | grep -q '^✖ the imported-prose check is reading the real rendered data'
}

# ── the two shapes card t_8580357e widened the guard to see ───────────────────
echo "== CASE 7: a marker in RAW JSX TEXT (ts.JsxText) must be RED ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# Raw JSX prose: neither a string literal nor a template literal, so the
# string-literal-only walk let it through GREEN. Inserted at module scope so it
# parses as TSX without a render site (the AST walk covers the whole file).
printf 'const QA_RAWTEXT = <p>raw jsx prose with `raw_marker` inside</p>;\n' > "$snap/p7.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p7.txt" "$f"
grep -q 'raw jsx prose with `raw_marker` inside' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case7=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/security/page.tsx' && printf '%s' "$out" | grep -q '\[raw JSX text\]' && fails_findings_only "$out"; then
  echo "  BITES: RED naming docs/security/page.tsx [raw JSX text] (the JsxText hole is closed)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security, [raw JSX text], one findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 8: a marker ESCAPED inside a TEMPLATE LITERAL must be RED ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# A NoSubstitutionTemplateLiteral whose decoded `node.text` carries a backtick:
# the escaped form renders the marker exactly like the plain one. Before the
# widening this node kind was the guard's documented exclusion.
printf 'const QA_ESCAPED = `prose with \\`escaped_marker\\` inside`;\n' > "$snap/p8.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p8.txt" "$f"
grep -q 'prose with \\`escaped_marker\\` inside' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case8=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'docs/security/page.tsx' && printf '%s' "$out" | grep -q '\[template literal\]' && fails_findings_only "$out"; then
  echo "  BITES: RED naming docs/security/page.tsx [template literal] (the escaped-marker hole is closed)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security, [template literal], one findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 9 (negative control): the SAME escaped marker inside <code> stays GREEN ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The supported way to show code on /docs: a template literal inside <code>. A
# sample that prints a backtick is code the page MEANS to show, so the widening
# must not report it — this is the control that keeps CASE 8 from being "flag
# every template literal".
printf 'const QA_CODEBT = <code>{`a sample that prints a \\`marker\\``}</code>;\n' > "$snap/p9.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p9.txt" "$f"
grep -q 'a sample that prints a \\`marker\\`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case9=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a template literal inside <code> is the supported code-sample shape"
else
  echo "  CONTROL FAILED: verdict=$v — the widening flagged a code sample as prose"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the IMPORTED-prose surface (card t_763c28c2) ──────────────────────────────
# /docs/api's prose is not in the page .tsx: it is imported from the generated
# modules (web/src/lib/apiExamples.gen.ts, openapiSpec.gen.ts) and printed with a
# JSX expression, so the DOCS_DIR walk above — the whole guard before this card —
# reported GREEN while the live page rendered 20 literal backticks (measured on
# jomform.com 2026-10-05, fresh browser state). CASE 10 drives that hole: a marker
# in the generated DATA module must turn the guard RED naming that file. CASE 11 is
# the negative control that keeps it from flagging code: a backtick inside `curl`,
# which the page renders inside <code>, must stay GREEN.
echo "== CASE 10: a marker in the IMPORTED data module (apiExamples.gen.ts) must be RED ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/Leave slug out and it is generated/Leave `slug` out and it is generated/' "$f"
grep -q 'Leave `slug` out' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case10=$v")
if [ "$v" = RED ] && printf '%s' "$out" | grep -q 'src/lib/apiExamples.gen.ts' && fails_imported_findings_only "$out"; then
  echo "  BITES: RED naming src/lib/apiExamples.gen.ts (the imported prose the DOCS_DIR walk cannot see)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/lib/apiExamples.gen.ts, one imported-findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 11 (negative control): a backtick inside the curl CODE SAMPLE stays GREEN ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# `curl` is rendered inside <code> — the supported way to show code — so a backtick
# in it is code the page MEANS to show, not a markdown marker leaked into prose.
# Without this the imported check would flag every code sample.
perl -0pi -e 's/curl -sS/curl -sS `/' "$f"
grep -q 'curl -sS `' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case11=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a backtick in a curl code sample is not judged as prose"
else
  echo "  CONTROL FAILED: verdict=$v — the imported check flagged a code sample"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== FINAL: every mutated file restored, tree GREEN again ==="
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("final=$v")
[ "$v" = GREEN ] || { fail=1; printf '%s\n' "$out" | tail -25; }

echo
echo "verdicts: ${VERDICTS[*]}"
echo "BITES: $(printf '%s\n' "${VERDICTS[@]}" | grep -cE '^case(1|2|3|6|7|8|10)=RED')/7 HOLE cases RED, $(printf '%s\n' "${VERDICTS[@]}" | grep -cE '^case(4|5|9|11)=GREEN')/4 CONTROLS GREEN"
echo "RESULT: $([ "$fail" = 0 ] && echo 'ALL CASES OK' || echo 'FAILURES PRESENT')"
exit "$fail"
