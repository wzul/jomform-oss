#!/usr/bin/env bash
# Bite proof for web/e2e/forms-bulk-failure.spec.ts (card t_c9e37f49).
#
# THE CARD'S CLAIM is that a refused BULK publish on /forms leaves its sentence on
# screen long enough to read. The pre-fix shape did not: `load()` sat in the
# `finally`, so the `return` on both failure branches still triggered a reload,
# and `load()` clears `err` on success — the sentence was replaced ~200ms after it
# was set. This harness re-creates exactly that shape and requires the spec to go
# RED, at a NAMED assertion, with the whole suite rebuilt first so the spec
# measures THIS tree's build and not a leftover one.
#
# A guard that only ever passes is a tautology, so the baseline is a real case
# too, not a preamble: the unmutated tree must be GREEN (1 passed). Without it a
# red in case 2 could be the harness itself.
#
# Run:  bash web/scripts/biteproof_forms_bulk_failure.sh
# Env:  A11Y_PORT (default 3399) — playwright.config.ts's webServer serves there.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

SPEC=e2e/forms-bulk-failure.spec.ts
PAGE=src/app/forms/page.tsx
PLAYWRIGHT=./node_modules/.bin/playwright
export A11Y_PORT="${A11Y_PORT:-3399}"
BAK="/tmp/biteproof-c9e37f49.$(basename "$PAGE").bak"

restore() {
  [ -f "$BAK" ] && cp "$BAK" "$PAGE"
}
trap restore EXIT

cp "$PAGE" "$BAK" || exit 2
sha_before="$(sha256sum "$PAGE")"

fail=0

rebuild() {
  echo "   (next build — the spec measures the BUILT page)"
  if ! npm run build >/tmp/biteproof-c9e37f49-build.log 2>&1; then
    echo "   BUILD FAILED — see /tmp/biteproof-c9e37f49-build.log"
    return 1
  fi
}

# args: <case name> <expect: pass|red> <named assertion (may be an ERE alternation)>
#
# THE MATCH IS SCOPED TO THE REPORTER'S Error BLOCK, NOT THE WHOLE OUTPUT
# (card t_dd211ae1). Matching `$expect` anywhere in Playwright's output is a false
# OK generator, and the reason is structural rather than a slip: when an assertion
# fails, Playwright ECHOES THE SPEC'S SOURCE as a `>` context line, so every string
# literal in the assertion message — the named thing itself — is printed whatever
# the test actually died of. Measured on this tree: `timeout` on the visible wait
# prints
#
#       Error: the bulk refusal must render a message at all
#       expect(locator).toBeVisible() failed
#       Locator: ...
#       Expected: visible / Timeout: 20000ms / Error: element(s) not found
#       ...
#         165 |   await expect(alert, "the bulk refusal must render a message at all")...
#       > 165 |   await expect(...)...
#
# — the label appears BOTH as the failure's own cause (the `Error:` line) and as the
# echoed source. So a timeout satisfies an unscoped search, and the harness reports
# "red at the named assertion" for a case that never reached one. That is exactly
# the property the BITE-HARNESSES.md row claims ("the red must land at a NAMED
# assertion … not at a crash or a failed build").
#
# Scoping it to `^ +Error:` keeps the claim that is true and drops the one that is
# not: a NON-assertion failure prints `Error: locator.innerText: Test timeout of
# 120000ms exceeded` — an Error line that does not carry the label — so the check
# still reds. The alternative considered and REJECTED: dropping the echoed block by
# line range. Its start marker is reporter-formatted (`- 1) [project] › file ›
# title`) and would silently match nothing on a reporter change, turning the guard
# off rather than making it wrong — the worse failure. A `grep` that matches text
# from the wrong region degrades loudly; one that matches nothing at all degrades
# silently.
run_case() {
  local name="$1" want="$2" expect="$3"
  echo "== $name"
  local out
  out="$($PLAYWRIGHT test "$SPEC" --reporter=list 2>&1)"
  # error_block is the reporter's own cause lines for this run: `Error: ...` under
  # the `1)` failure header, plus the `expect(...)` chain that follows it. Both
  # carry the real reason and neither carries the spec's echoed source.
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  if [ "$want" = pass ]; then
    # Herestring, not `printf | grep -q`: with `set -o pipefail` a reader that
    # exits at the first match can kill the writer with SIGPIPE (141) and report
    # a MATCH as a failure.
    if grep -qE "^ +1 passed" <<<"$out"; then
      grep -E "^ +1 passed" <<<"$out" | sort -u | head -1
      echo "   OK (green control: the unmutated tree really passes this spec)"
    else
      echo "   BASELINE NOT GREEN — the spec is red on a tree with no defect, so no"
      echo "   red below could be attributed to a mutation:"
      grep -E "passed|failed|^ *Error: " <<<"$out" | sort -u | head -8
      fail=1
    fi
    return
  fi
  if ! grep -qE "^ +1 failed" <<<"$out"; then
    echo "   NOT DETECTED — the spec stayed green with the pre-fix shape present"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qE "$expect" <<<"$error_block"; then
    echo "   DETECTED, but NOT at a named assertion. Wanted one of: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    grep -E "^ +1 failed" <<<"$out" | sort -u | head -1
    grep -E "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
}

echo
echo "== baseline: the fix in place, this tree built"
restore
rebuild || exit 2
run_case "0. the fixed tree passes (green control)" pass ""

echo
echo "== 1. the PRE-FIX shape: load() back in the finally"
# The exact pre-fix bytes: the success path keeps only `setErr("")`, the reload
# moves into the `finally` beside setBusy(false) — so a failure `return` reloads
# and the reload clears the sentence. Fails closed if the source no longer
# matches (the mutation must APPLY, or the case proves nothing).
if perl -0pi -e 's/\n      setErr\(""\);\n      load\(\);\n    \} finally \{\n(.*?)\n      setBusy\(false\);\n    \}/\n      setErr("");\n    } finally {\n$1\n      setBusy(false);\n      load();\n    }/s' "$PAGE" \
  && grep -q 'setBusy(false);' "$PAGE" && grep -qE '^      load\(\);$' "$PAGE" \
  && ! perl -0ne 'exit 1 if /setErr\(""\);\n      load\(\);/' "$PAGE"; then
  if rebuild; then
    run_case "1. a refused batch reloads and wipes the sentence" red \
      'the bulk refusal must render a message at all|the refusal must survive the whole 3s window|a refused batch must not reload the list'
  else
    fail=1
  fi
else
  echo "   MUTATION DID NOT APPLY — the source no longer matches this script"
  fail=1
fi

echo
echo "== restoring the fix and the build"
restore
if ! rebuild; then
  echo "   BUILD RESTORE FAILED — .next may still be a sabotage build; re-run 'npm run build'"
  fail=1
fi

echo
sha_after="$(sha256sum "$PAGE")"
if [ "$sha_before" != "$sha_after" ]; then
  echo "RESTORE FAILED — the tree is not byte-identical:"
  echo "  before: $sha_before"
  echo "  after : $sha_after"
  exit 2
fi
echo "restored byte-identically:"
echo "   $sha_after"
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
