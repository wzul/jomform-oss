#!/usr/bin/env bash
# Bite proof for web/src/lib/docsToolClaims.test.ts (card t_2e9f9c68).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: each case below
# is a phrasing the guard MISSED. The proof has to be a comparison — the guard as it
# stands on `origin/main` (the base this branch is rebased onto) must stay GREEN on the
# mutated tree, and the guard in this working tree must go RED and name the phantom. A
# test inside the guard cannot show that, because it can only exercise the guard's OWN
# rules, not the absence of them.
#
# Each case: mutate -> assert the mutation is really on disk -> run the OLD guard (expect
# GREEN, i.e. the hole) -> run the NEW guard (expect RED, naming the offender) -> restore
# the page byte-identically (re-checked with sha256).
#
# The OLD column is read from `origin/main`, not a pinned commit hash. A hash rots: this
# proof originally anchored on 2172312, whose guard reads `tools.go` alone and is RED on
# its own once a tool's literal moves out of that file (disable_2fa ->
# parity_issue396.go) — so the "old" column could no longer be GREEN and every case
# reported "does not reproduce". `origin/main` is the revision the comparison actually
# means ("before this branch" vs "this branch") and it moves with the base, so no hash
# has to be maintained here.
#
# THE VERDICT RULE REQUIRES POSITIVE EVIDENCE THAT THE GUARD RAN (card t_420ca40c).
# It used to be `if grep -q '^ℹ fail [1-9]'; then RED; else GREEN` — a decision from the
# ABSENCE of a failure line. A guard that never started therefore read GREEN: a missing
# file (`Could not find '…'`, node exit 1), a file that cannot be parsed, and a guard
# whose tests are all skipped all produced no `fail` line. GREEN there is not a weak
# verdict, it is a WRONG one, and it is indistinguishable from a real one in the output —
# on the OLD column it means "the base guard missed this", which is the claim this proof
# exists to make. The premise check is where it bit hardest: it refuses only when the base
# guard is not GREEN, so a base that ran no tests was accepted as a valid "before" column
# and every case then printed a verdict against a base that proved nothing. The rule now
# reads the guard's own summary and refuses (exit 2, with the reason) instead of choosing
# a column (rule_of() below). Measured on node 26.7.0: PASS prints `ℹ tests 9 / pass 9`
# with nine `✔ ` lines; a mutated page prints `pass 8 / fail 1`; a guard that cannot load
# prints `tests 1 / pass 0 / fail 1`; an all-skipped guard prints `tests 1 / pass 0 /
# skipped 1`; a missing file prints one line and NO summary at all. WHAT THE RULE CANNOT
# SEE, measured rather than assumed: node counts a .ts file that declares NO tests as
# `tests 1 / pass 1` plus a `✔ <filename>` line — the same shape as a real run — so a
# guard emptied of its tests still reads GREEN here. That case is covered INSIDE the
# guard instead, by its first test ("the guard is looking at the real docs tree and the
# real registry (it cannot pass vacuously)"), which asserts the docs walk actually found
# the tree. This rule is a floor under a running guard, not a proof that a guard asserts
# anything.
#
# THE VERDICT CHANNEL IS PER-RUN (card t_ff3640e7). `run_guard` used to write its output
# to the fixed host-global path /tmp/bite.last.out and both call sites re-`cat`ed it, so
# two runs overlapping in one worktree read each other's output and printed four false
# OLD/NEW verdicts (reproduced). The output now travels in a shell variable set by
# run_guard — which is also why run_guard is NOT called in a command substitution: that
# runs it in a subshell and the variable would not come back, which is the only reason the
# file round-trip existed. Nothing in here writes a fixed path any more.
#
# Run:  bash web/scripts/biteproof_docs_tool_claims.sh
set -uo pipefail

cd "$(dirname "$0")/.." || exit 2          # -> web/
OLD_GUARD=$(mktemp)
PAGE_BAK=$(mktemp)
# Per-run, and every mutated file has its backup already taken, because the trap below
# restores from these BEFORE any case can mutate anything.
EMBED_BAK=$(mktemp)
PAGE=src/app/docs/chat/page.tsx
EMBED=src/app/docs/embed/page.tsx
GUARD=src/lib/docsToolClaims.test.ts
cp "$PAGE" "$PAGE_BAK"
cp "$EMBED" "$EMBED_BAK"
PAGE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)
EMBED_SHA=$(sha256sum "$EMBED" | cut -d' ' -f1)

fail=0

# ONE trap, carrying two properties the shipped version did not have:
#
#   * the mutated pages are restored on EVERY exit, interrupts included. Restoring was on
#     the non-error path only (`cp "$EMBED_BAK" "$EMBED"` at the end of case 4), so an
#     interrupt — or any abort between the mutation and the restore — left a mutation on
#     disk and the tree dirty (t_ff3640e7 reproduced exactly that).
#   * every temp file a run created is removed here. They are per-run `mktemp` paths, so
#     one run cannot remove another's, and no run can leave debris behind.
TMP_E=''
# The temp copies of the guard also live beside the real one (the guard resolves its
# paths from import.meta.url, and node strips types by .ts extension), but under a
# PER-RUN name: a fixed `$GUARD.oldcheck.ts` is the same host-global channel as
# /tmp/bite.last.out — two runs overlapping in one worktree would overwrite each other's
# copy mid-case and read a verdict for the other run's guard.
OLDTMP="$GUARD.oldcheck.$$.ts"
SKTMP="$GUARD.selfcheck_skipped.$$.ts"
NOLOADTMP="$GUARD.selfcheck_loadfail.$$.ts"
MISSINGTMP="$GUARD.selfcheck_missing.$$.ts"
cleanup() {
  local rc=$?
  cp "$PAGE_BAK" "$PAGE" 2>/dev/null
  cp "$EMBED_BAK" "$EMBED" 2>/dev/null
  rm -f "$OLD_GUARD" "$PAGE_BAK" "$EMBED_BAK" "$TMP_E"
  rm -f "$OLDTMP" "$SKTMP" "$NOLOADTMP" "$MISSINGTMP"
  exit "$rc"
}
trap cleanup EXIT

# The "before" guard: the one on the branch's base. Fail closed, with the reason,
# when the premise does not hold — a verdict produced without a real "before" is
# worse than no verdict, because it reads as proof.
TOP=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "not in a git worktree" >&2; exit 2; }
GUARD_IN_REPO="${PWD#"$TOP"/}/$GUARD"
BASE_REF="${BITEPROOF_BASE:-origin/main}"
BASE_REV=$(git rev-parse "${BASE_REF}^{commit}" 2>/dev/null) || {
  echo "cannot resolve ${BASE_REF} — fetch it, or set BITEPROOF_BASE to the base ref" >&2
  exit 2
}
git cat-file -e "${BASE_REV}:${GUARD_IN_REPO}" 2>/dev/null || {
  echo "cannot read ${GUARD_IN_REPO} at ${BASE_REF} (${BASE_REV}) — the base does not" >&2
  echo "carry the guard yet, so there is no \"before\" to compare against." >&2
  exit 2
}
git show "${BASE_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines"

# rule_of <node exit status> — turns a guard run's output into a VERDICT.
# Reads $VERDICT_OUT (set by run_guard) and sets:
#   VERDICT      GREEN | RED | REFUSED
#   VERDICT_WHY  the reason, when REFUSED
#
#   GREEN   node exited 0, `ℹ pass [1-9]`, and at least one `✔ ` test line. The `✔ ` line
#           is the positive evidence that node's reporter emitted a per-test result at all
#           (the guard that cannot load, the all-skipped guard and the missing file each
#           fail one of the three). The `tests` count is deliberately NOT used as the
#           evidence: node counts a test-less file as `tests 1`, so it was never the thing
#           that distinguished a real run (see the header, "WHAT THE RULE CANNOT SEE").
#   RED     `ℹ fail [1-9]` AND `ℹ pass [1-9]`: a real test failed while others passed.
#           BOTH halves are needed. A guard that cannot LOAD reports `tests 1 / pass 0 /
#           fail 1` with no test title, and reading that as RED would let a broken
#           instrument print "BITES" for the case that wanted RED — the same trap the
#           unlock-class harness records for `go test -run` matching nothing
#           (BITE-HARNESSES.md: "a control's green could not be told from 'the guard
#           never ran'").
#   REFUSED anything else: no summary at all (missing file — node prints one line and
#           exits 1), every test skipped, a non-zero exit with nothing passing. The
#           caller exits 2 with the reason rather than printing a column.
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
      VERDICT_WHY="every test failed and none passed (${line}) — the guard could not load, so this is not the guard catching a mutation"
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

# run_guard <guardfile> — runs the guard and sets VERDICT / VERDICT_OUT / VERDICT_WHY.
# NOT called in a command substitution: that is a subshell, and the variables would not
# come back to the caller (see the note at the top about the per-run channel).
# NOTE: the guard EXITS NON-ZERO when it fails, so the status is captured rather than
# left for `set -o pipefail` to act on. `node --test … | grep -q` under `pipefail`
# reports the node exit status and reads a real match as "not found" — that exact trap
# produced a false verdict once.
run_guard() {
  local rc=0
  VERDICT_OUT=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || rc=$?
  rule_of "$rc"
}

# The premise check runs the BASE guard, which is not in the tree — it is a copy
# beside the real guard (same reason as check(): the .ts extension is load-bearing
# and the guard resolves its paths from import.meta.url).
run_guard_premise() {
  cp "$OLD_GUARD" "$OLDTMP"
  run_guard "$OLDTMP"
  rm -f "$OLDTMP"
}

# PREMISE CHECK, before any case runs: the "before" guard must be GREEN on the tree
# as it stands, with no mutation. If it is not, the base itself is red for a reason
# that has nothing to do with this branch — and then no mutation can make it "miss"
# anything, because it is already failing. Every case would print "does not
# reproduce the gap" and the reader would have to work out that the instrument, not
# the guard, is broken. Fail closed instead, with the reason and the fix.
#
# This is also where the verdict rule matters most: a base guard that RAN NO TESTS is
# not GREEN any more, so it cannot be accepted as the "before" column.
#
# NOTE the .ts extension on the temp copy: node strips types by extension, and a
# copy named without it dies with `SyntaxError: Unexpected token ':'` on the first
# type annotation — which reads as a RED guard and would make this check refuse
# every base. `check()` below copies to the same path for the same reason.
run_guard_premise
if [ "$VERDICT" = REFUSED ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) did not RUN on the" >&2
  echo "unmutated tree (${VERDICT_WHY}), so there is no \"before\" column and this proof" >&2
  echo "would be about nothing. A guard that never ran must not be read as GREEN — that is" >&2
  echo "the false verdict this rule exists to refuse. Check the base's guard can be read:" >&2
  grep -E '^ℹ ' <<<"$VERDICT_OUT" | head -6 >&2
  grep -E '^[✖]' <<<"$VERDICT_OUT" | head -2 >&2
  exit 2
fi
if [ "$VERDICT" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) is ${VERDICT} on the" >&2
  echo "UNMUTATED tree, so there is no \"before\" column and this proof would be about nothing." >&2
  echo "That is a red base, not a finding about this branch. Land the base's fix, or point" >&2
  echo "BITEPROOF_BASE at a base whose guard is green:" >&2
  grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2 >&2
  grep -E '^✖' <<<"$VERDICT_OUT" | head -2 >&2
  exit 2
fi

# SELF-CHECK OF THE VERDICT RULE, on EVERY run — the pattern BITE-HARNESSES.md records
# for biteproof_unlock_class_guard.py ("the evidence check is itself self-checked on every
# run against a REAL vacuous output ... and a REAL driven one"). Without this, the rule
# that decides every column is the one thing in here nobody has ever seen fail — and the
# rule it replaces WAS wrong, in exactly the ways below, with nothing to say so. Each leg
# is a REAL node run, not a hand-written string:
#
#   1. a real DRIVEN run must read GREEN — the premise's own output, already captured
#   2. a real all-SKIPPED guard must be REFUSED — the false GREEN the card found: node
#      prints `tests 1 / pass 0 / skipped 1`, so the old absence rule read GREEN
#   3. a real guard that cannot LOAD must be REFUSED, NOT RED — `tests 1 / pass 0 / fail 1`
#      names no test, so a rule that called that RED would print "BITES" for a case that
#      wanted RED while the instrument was broken
#   4. a real MISSING path must be REFUSED — node exits 1 with one line and no summary
#
# The RED leg is not duplicated here: every case below asserts RED in its NEW column, so a
# rule that refused a real failure would exit 2 with that case's own reason.
selfcheck_rule() {
  local bad='' sk="$SKTMP" noload="$NOLOADTMP"
  # leg 1 — reuse the premise's real driven run (it passed GREEN above)
  rule_of 0
  [ "$VERDICT" = "GREEN" ] || bad="leg 1: a real driven guard run read as ${VERDICT}, wanted GREEN"
  # legs 2-4 — three genuinely broken guards, written where node will run them
  printf '%s\n' 'import { test } from "node:test";' 'test.skip("a guard whose tests never run", () => {});' > "$sk"
  printf '%s\n' 'const broken: = 1;' > "$noload"
  run_guard "$sk"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 2: an all-skipped guard read as ${VERDICT}, wanted REFUSED (${VERDICT_WHY})"
  run_guard "$noload"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 3: a guard that cannot load read as ${VERDICT}, wanted REFUSED (that is how a broken instrument prints BITES)"
  run_guard "$MISSINGTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 4: a missing guard read as ${VERDICT}, wanted REFUSED"
  rm -f "$sk" "$noload"
  if [ -n "$bad" ]; then
    echo >&2
    echo "BITEPROOF REFUSED: the VERDICT RULE ITSELF is broken — ${bad}." >&2
    echo "Every column above is decided by it, so no verdict from this run would be worth" >&2
    echo "anything. Fix rule_of() before trusting any case. Last node output:" >&2
    grep -E '^ℹ ' <<<"$VERDICT_OUT" | head -6 >&2
    exit 2
  fi
}
selfcheck_rule

refuse() { # refuse <label> <column> — the guard did not run: no verdict, no further cases
  echo >&2
  echo "  *** ${1}: the ${2} guard DID NOT RUN — ${VERDICT_WHY} ***" >&2
  echo "  *** A column with no guard run behind it is not a column. Refusing (exit 2) ***" >&2
  exit 2
}

check() { # check <label> <old-want> <new-want> [must-name]
  local label="$1" old_want="$2" new_want="$3" needle="${4:-}"
  # The temp copy MUST keep a .ts extension: node strips types by extension, and a .mjs
  # copy of the guard dies with `SyntaxError: Unexpected token ':'` on its first type
  # annotation — which reads as a failing guard rather than a harness bug. It lives beside
  # the real guard because the guard resolves its paths from import.meta.url.
  local oldtmp="$OLDTMP"
  cp "$OLD_GUARD" "$oldtmp"
  run_guard "$oldtmp"; rm -f "$oldtmp"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "OLD"
  if [ "$VERDICT" != "$old_want" ]; then
    echo "  *** ${label}: OLD guard wanted ${old_want}, got ${VERDICT} — the case does not reproduce the gap ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ "$label" = "baseline" ]; then
    echo "  ok   baseline: old guard ${VERDICT}, as expected"
  else
    echo "  ok   ${label}: old guard ${VERDICT} — the gap reproduces (it missed this)"
  fi

  run_guard "$GUARD"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "NEW"
  if [ "$VERDICT" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${VERDICT} ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$VERDICT_OUT"; then
    echo "  *** ${label}: NEW guard ${VERDICT} but the failure does not name ${needle} ***"
    grep -F 'presents' <<<"$VERDICT_OUT" | head -2; fail=1
  else
    echo "  BITES ${label}: new guard ${VERDICT}${needle:+, naming $needle}"
  fi
}

mutate() { # mutate <text-to-insert> — inserted after the page's 2-line title/comment head
  local tmp; tmp=$(mktemp)
  { head -2 "$PAGE"; printf '%s\n' "$1"; tail -n +3 "$PAGE"; } > "$tmp"; mv "$tmp" "$PAGE"
}
assert_applied() {
  if grep -qF "$1" "$PAGE"; then echo "       mutation on disk: $1"; else
    echo "  *** MUTATION DID NOT APPLY: $1 ***"; fail=1; return 1; fi
}
restore_page() {
  cp "$PAGE_BAK" "$PAGE"
  [ "$(sha256sum "$PAGE" | cut -d' ' -f1)" = "$PAGE_SHA" ] \
    && echo "       restored byte-identical" \
    || { echo "  *** RESTORE MISMATCH $PAGE ***"; fail=1; }
}
restore_embed() {
  cp "$EMBED_BAK" "$EMBED"
  [ "$(sha256sum "$EMBED" | cut -d' ' -f1)" = "$EMBED_SHA" ] \
    && echo "       restored byte-identical" \
    || { echo "  *** RESTORE MISMATCH $EMBED ***"; fail=1; }
}

echo
echo "== baseline (no mutation) =="
check "baseline" GREEN GREEN

echo
echo "== CASE 1: Gap 1 — underscore-free name in the shipped phrasing =="
mutate '// QA: the assistant can delete every product as the deleteall tool.'
assert_applied 'as the deleteall tool' && {
  check "single-word phantom" GREEN RED 'presents "deleteall" as a tool'; }
restore_page

echo
echo "== CASE 2: Gap 2 — the header-comment parenthetical (one line) =="
mutate '// QA: this page documents the MCP tool the proposal lane pairs with (propose_write).'
assert_applied 'pairs with (propose_write)' && {
  check "header-comment parenthetical" GREEN RED 'presents "propose_write" as a tool'; }
restore_page

echo
echo "== CASE 3: Gap 2 authentic shape — the parenthetical WRAPPED onto the next line =="
mutate '// QA: the MCP tool the proposal lane pairs with
// (propose_write), the reversible set it may prepare,'
assert_applied '(propose_write), the reversible set' && {
  check "parenthetical wrapped across lines" GREEN RED 'presents "propose_write" as a tool'; }
restore_page

echo
echo "== CASE 4: Gap 1 on a DIFFERENT docs page (the walk covers the whole tree) =="
# The mutated copy goes through a per-run mktemp, not a fixed /tmp/e (the same
# host-global channel as the verdict file: two overlapping runs would clobber it
# mid-mutation and the case would then read the other run's page).
TMP_E=$(mktemp)
awk 'NR==2{print "// QA: the snippet is generated as the uploadpic tool."} {print}' "$EMBED" > "$TMP_E" && mv "$TMP_E" "$EMBED"
if grep -qF 'as the uploadpic tool' "$EMBED"; then
  echo "       mutation on disk: as the uploadpic tool"
  cp "$OLD_GUARD" "$OLDTMP"; run_guard "$OLDTMP"; rm -f "$OLDTMP"
  [ "$VERDICT" = REFUSED ] && refuse "/docs/embed phantom" "OLD"
  [ "$VERDICT" = "GREEN" ] && echo "  ok   /docs/embed phantom: old guard GREEN (the hole is real)" \
                           || { echo "  *** old guard was ${VERDICT}, expected GREEN ***"; fail=1; }
  run_guard "$GUARD"
  [ "$VERDICT" = REFUSED ] && refuse "/docs/embed phantom" "NEW"
  if [ "$VERDICT" = "RED" ] && grep -qF 'presents "uploadpic" as a tool' <<<"$VERDICT_OUT"; then
    echo '  BITES /docs/embed phantom: new guard RED, naming presents "uploadpic" as a tool'
  else echo "  *** /docs/embed phantom: new guard ${VERDICT} (or did not name it) ***"; fail=1; fi
else echo "  *** mutation did not apply to $EMBED ***"; fail=1; fi
restore_embed

echo
echo "== NEGATIVE CONTROL: a REGISTERED name in the same phrasing must stay GREEN in both =="
mutate '// QA: the same capability exists over MCP as the list_products tool, which is registered.'
assert_applied 'as the list_products tool' && check "registered name" GREEN GREEN
restore_page

echo
echo "== NEGATIVE CONTROL: the guard still catches real prose it was already catching =="
mutate '// QA: has no tool to call, and the one tool of its own.'
assert_applied 'has no tool to call' && check "ordinary prose stays quiet" GREEN GREEN
restore_page

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — both gap phrasings were GREEN under the shipped guard and RED under this one."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
