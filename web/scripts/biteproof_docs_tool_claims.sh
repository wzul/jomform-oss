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
# Run:  bash web/scripts/biteproof_docs_tool_claims.sh
set -uo pipefail

cd "$(dirname "$0")/.." || exit 2          # -> web/
OLD_GUARD=$(mktemp)
PAGE_BAK=$(mktemp)
PAGE=src/app/docs/chat/page.tsx
GUARD=src/lib/docsToolClaims.test.ts

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
cp "$PAGE" "$PAGE_BAK"
PAGE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)

fail=0
trap 'rm -f "$OLD_GUARD" "$PAGE_BAK"' EXIT

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines"

# run_guard <guardfile> -> prints "GREEN" or "RED"
# NOTE: the guard EXITS NON-ZERO when it fails, so the output is captured into a variable
# first. `node --test … | grep -q` under `set -o pipefail` reports the node exit status and
# reads a real match as "not found" — that exact trap produced a false verdict once.
run_guard() {
  local out
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || true
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
  printf '\n%s' "$out" > /tmp/bite.last.out
}

# The premise check runs the BASE guard, which is not in the tree — it is a copy
# beside the real guard (same reason as check(): the .ts extension is load-bearing
# and the guard resolves its paths from import.meta.url).
run_guard_premise() {
  local oldtmp="$GUARD.oldcheck.ts"
  cp "$OLD_GUARD" "$oldtmp"
  local got; got=$(run_guard "$oldtmp")
  rm -f "$oldtmp"
  printf '%s' "$got"
}

# PREMISE CHECK, before any case runs: the "before" guard must be GREEN on the tree
# as it stands, with no mutation. If it is not, the base itself is red for a reason
# that has nothing to do with this branch — and then no mutation can make it "miss"
# anything, because it is already failing. Every case would print "does not
# reproduce the gap" and the reader would have to work out that the instrument, not
# the guard, is broken. Fail closed instead, with the reason and the fix.
#
# NOTE the .ts extension on the temp copy: node strips types by extension, and a
# copy named without it dies with `SyntaxError: Unexpected token ':'` on the first
# type annotation — which reads as a RED guard and would make this check refuse
# every base. `check()` below copies to the same path for the same reason.
premise=$(run_guard_premise); premise_out=$(cat /tmp/bite.last.out)
if [ "$premise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) is ${premise} on the" >&2
  echo "UNMUTATED tree, so there is no \"before\" column and this proof would be about nothing." >&2
  echo "That is a red base, not a finding about this branch. Land the base's fix, or point" >&2
  echo "BITEPROOF_BASE at a base whose guard is green:" >&2
  grep -E '^ℹ (pass|fail)' <<<"$premise_out" | head -2 >&2
  grep -E '^✖' <<<"$premise_out" | head -2 >&2
  exit 2
fi

check() { # check <label> <old-want> <new-want> [must-name]
  local label="$1" old_want="$2" new_want="$3" needle="${4:-}" out got oldtmp
  # The temp copy MUST keep a .ts extension: node strips types by extension, and a .mjs
  # copy of the guard dies with `SyntaxError: Unexpected token ':'` on its first type
  # annotation — which reads as a failing guard rather than a harness bug. It lives beside
  # the real guard because the guard resolves its paths from import.meta.url.
  oldtmp="$GUARD.oldcheck.ts"
  cp "$OLD_GUARD" "$oldtmp"
  got=$(run_guard "$oldtmp"); out=$(cat /tmp/bite.last.out); rm -f "$oldtmp"
  if [ "$got" != "$old_want" ]; then
    echo "  *** ${label}: OLD guard wanted ${old_want}, got ${got} — the case does not reproduce the gap ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  elif [ "$label" = "baseline" ]; then
    echo "  ok   baseline: old guard ${got}, as expected"
  else
    echo "  ok   ${label}: old guard ${got} — the gap reproduces (it missed this)"
  fi

  got=$(run_guard "$GUARD"); out=$(cat /tmp/bite.last.out)
  if [ "$got" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${got} ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out"; then
    echo "  *** ${label}: NEW guard ${got} but the failure does not name ${needle} ***"
    grep -F 'presents' <<<"$out" | head -2; fail=1
  else
    echo "  BITES ${label}: new guard ${got}${needle:+, naming $needle}"
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
EMBED=src/app/docs/embed/page.tsx
EMBED_BAK=$(mktemp); cp "$EMBED" "$EMBED_BAK"
awk 'NR==2{print "// QA: the snippet is generated as the uploadpic tool."} {print}' "$EMBED" > /tmp/e && mv /tmp/e "$EMBED"
if grep -qF 'as the uploadpic tool' "$EMBED"; then
  echo "       mutation on disk: as the uploadpic tool"
  cp "$OLD_GUARD" "$GUARD.oldcheck.ts"; got=$(run_guard "$GUARD.oldcheck.ts"); rm -f "$GUARD.oldcheck.ts"
  [ "$got" = "GREEN" ] && echo "  ok   /docs/embed phantom: old guard GREEN (the hole is real)" \
                       || { echo "  *** old guard was ${got}, expected GREEN ***"; fail=1; }
  got=$(run_guard "$GUARD")
  if [ "$got" = "RED" ] && grep -qF 'presents "uploadpic" as a tool' /tmp/bite.last.out; then
    echo '  BITES /docs/embed phantom: new guard RED, naming presents "uploadpic" as a tool'
  else echo "  *** /docs/embed phantom: new guard ${got} (or did not name it) ***"; fail=1; fi
else echo "  *** mutation did not apply to $EMBED ***"; fail=1; fi
cp "$EMBED_BAK" "$EMBED"; rm -f "$EMBED_BAK"

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
