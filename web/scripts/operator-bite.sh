#!/usr/bin/env bash
# Bite proof for the operator-identity guard (src/lib/operatorNotice.test.ts).
#
# Sabotages ONE thing at a time, runs the guard, and expects RED each time.
# Every case verifies (a) the sabotage is really in the file, (b) which tests
# failed, (c) that the restore really happened before the next case. The
# baseline commit must be the fix itself — this restores with `git checkout`,
# so an uncommitted change would be destroyed. It writes nothing to the repo.
#
# Run:  bash web/scripts/operator-bite.sh
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
GUARD=src/lib/operatorNotice.test.ts
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
if echo "$BASE" | grep -qE '^ℹ fail [1-9]'; then
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
