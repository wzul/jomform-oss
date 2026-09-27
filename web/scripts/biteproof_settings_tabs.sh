#!/usr/bin/env bash
# Bite proof for web/e2e/settings-tabs.spec.ts.
#
# A guard that only ever passes is a tautology, and this file has TWO halves
# that are easy to mistake for one: the three source-side halves its header
# documents (strip class, reveal, focus ring) AND the tab-count wait this card
# rewrote. A wait that no longer bites is as bad as a guard that stops seeing
# the real page, so every case below has to produce a red at a NAMED assertion —
# a red for an unrelated reason (a crash, a failed build) is not a bite.
#
# The page-side cases rebuild the app first: the spec measures the BUILT page,
# and `npm run a11y` alone would judge the previous build.
#
# Run:  bash web/scripts/biteproof_settings_tabs.sh
# Env:  A11Y_PORT (default 3320) — the spec starts its own server there.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

SPEC=e2e/settings-tabs.spec.ts
PAGE=src/app/settings/page.tsx
CSS=src/app/globals.css
PLAYWRIGHT=./node_modules/.bin/playwright
export A11Y_PORT="${A11Y_PORT:-3320}"

restore() {
  for f in "$SPEC" "$PAGE" "$CSS"; do
    if [ -f "/tmp/biteproof-dc06.$(basename "$f").bak" ]; then
      cp "/tmp/biteproof-dc06.$(basename "$f").bak" "$f"
    fi
  done
}
trap restore EXIT

fail=0
for f in "$SPEC" "$PAGE" "$CSS"; do
  cp "$f" "/tmp/biteproof-dc06.$(basename "$f").bak"
done
sha_before=""
for f in "$SPEC" "$PAGE" "$CSS"; do sha_before="$sha_before$(sha256sum "$f")
"; done

rebuild() {
  echo "   (next build — the spec measures the BUILT page)"
  if ! npm run build >/tmp/biteproof-dc06-build.log 2>&1; then
    echo "   BUILD FAILED — see /tmp/biteproof-dc06-build.log"
    return 1
  fi
}

# args: <case name> <expected failure text> <rebuild:yes|no> <mutation shell>
run_case() {
  local name="$1" expect="$2" do_build="$3" mutate="$4"
  echo "== $name"
  restore
  if ! eval "$mutate"; then
    echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
    fail=1
    restore
    return
  fi
  if [ "$do_build" = yes ] && ! rebuild; then
    fail=1
    restore
    return
  fi
  local out
  out="$($PLAYWRIGHT test "$SPEC" --reporter=list 2>&1)"
  # Herestring, not `printf | grep -q`: with `set -o pipefail` a reader that
  # exits at the first match can kill the writer with SIGPIPE (141) and report
  # a MATCH as a failure — load-dependent, and it would look like "not detected".
  if ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
    echo "   NOT DETECTED — the spec stayed green with this defect present"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qF "$expect" <<<"$out"; then
    echo "   DETECTED, but NOT at the expected assertion. Wanted: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    grep -E "^ +[0-9]+ failed" <<<"$out" | sort -u | head -1
    grep -F "$expect" <<<"$out" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
  restore
}

# ---------------------------------------------------------------------------
# The three source-side halves the spec's own header documents (lines 48-57).
# Each must still be 25 failed / 0 passed.
# ---------------------------------------------------------------------------
run_case "1. strip class back to flex flex-wrap" \
  "wrapped to" yes \
  "perl -0pi -e 's/className=\"jf-tabstrip mt-4 flex gap-1 overflow-x-auto border-b border-slate-200\"/className=\"jf-tabstrip mt-4 flex flex-wrap gap-1 border-b border-slate-200\"/' $PAGE && grep -q 'flex flex-wrap gap-1 border-b' $PAGE"

run_case "2. the reveal disabled (RULE 6's wait must not rescue it)" \
  "not even after waiting for the reveal to land" yes \
  "perl -0pi -e 's/el\.scrollIntoView\(\{ block: \"nearest\", inline: \"nearest\", behavior: \"instant\" \}\);/if (true) return;/' $PAGE && grep -q 'if (true) return;' $PAGE"

run_case "3. the inset focus-ring rule dropped" \
  "focus ring of" yes \
  "perl -0pi -e 's/\.jf-tabstrip \[role=\"tab\"\]:focus-visible \{/.jf-tabstrip-dropped [role=\"tab\"]:focus-visible {/' $CSS && grep -q 'jf-tabstrip-dropped' $CSS"

# ---------------------------------------------------------------------------
# The tab-count wait this card rewrote. Its whole point is that a session that
# never resolves the admin role fails AT THE WAIT, naming the session, instead
# of surfacing 40 lines later as a TAB_KEYS/label mismatch. So: stub a MEMBER
# bundle (the pre-bundle state the flake measured) against the ADMIN
# expectation and require that named failure.
# ---------------------------------------------------------------------------
run_case "4. a bundle that never resolves the admin role fails at the count wait" \
  "did not resolve the admin role" no \
  "perl -0pi -e 's/slug: \"demo\", role: \"admin\"/slug: \"demo\", role: \"member\"/' $SPEC && grep -q 'role: \"member\"' $SPEC && ! grep -q 'role: \"admin\"' $SPEC"

# ---------------------------------------------------------------------------
# The last case does not rebuild (its sabotage is in the spec, not the page), so
# a run would otherwise leave case 3's CSS-sabotaged build on disk — and the
# `a11y` gate does NOT rebuild, it serves whatever `.next` is already there, so
# the next gate run would judge a build this tree never made. Put the real build
# back before returning.
# ---------------------------------------------------------------------------
echo
echo "== restoring the build (the last case's sabotage must not be left served)"
if rebuild; then
  echo "   build restored"
else
  echo "   BUILD RESTORE FAILED — .next may still be a sabotage build; re-run 'npm run build'"
  fail=1
fi
echo
sha_after=""
for f in "$SPEC" "$PAGE" "$CSS"; do sha_after="$sha_after$(sha256sum "$f")
"; done
if [ "$sha_before" != "$sha_after" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  diff <(echo "$sha_before") <(echo "$sha_after")
  exit 2
fi
echo "restored byte-identically:"
echo "$sha_after" | sed 's/^/   /'
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
