#!/usr/bin/env bash
# Bite proof for the render-site vacuity checks in web/src/lib/docsIndex.test.ts
# (card t_fc8a9ff2).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: the
# defect is SILENCE, not a wrong value. `docsSrc` used to be the RAW page source,
# so the vacuity test — the guard's own answer to the header's stated limit ("an
# id that is real text the page never renders still passes ... Telling those apart
# needs the built HTML") — was satisfied by text a reader never sees. A test
# inside the guard can only exercise the guard's OWN rules, not the absence of
# them, so the "before" column has to come from the guard as it stands on the
# branch's base.
#
# EACH CASE: mutate src/app/docs/page.tsx -> assert the mutation is really on disk
# -> run the BASE guard (origin/main, the hole) -> run the guard in this working
# tree -> restore the page byte-identically (sha256 re-checked, `git status`
# asserted clean). Only ONE file is ever mutated, and it is the page, never the
# guard — the guard's own sha256 is re-checked at the end.
#
# THE TWO DIRECTIONS, which are why a single case is not enough:
#
#   * the CONTROL drives the mutation with NO comment and requires RED in BOTH
#     columns. If the control were GREEN under the base guard there would be no
#     hole to fix; if it were GREEN under this tree the check would be blind.
#   * the HOLE case drives the SAME mutation plus one comment carrying the same
#     token and requires GREEN under the base guard (that is the defect: the
#     whole suite passes on a page that renders nothing) and RED, naming the
#     vacuity test, under this tree.
#
# WHAT A COMMENT FORM IS WORTH, measured for every form the strip claims to
# cover: a `//` line, a `{/* … */}` JSX container, and a module-scope `/* … */`.
# The card's own table is the `//` case; the other two are here because the same
# class of edit is what the strip's doc block promises, and a promise nobody
# drives is what this whole family of cards is about.
#
# A FOURTH FORM, which is not a render site at all (card `t_8b8d3365`): the same
# `docsSrc` is what `parseGroups` reads, so a `title:`/`body:` commented out
# inside GROUPS was a REAL feature entry to the base guard — it satisfied the
# docs-parity contract for a feature the page no longer lists. That case is
# driven here (CASE 7) rather than only claimed in the register's row, which is
# what the row used to do while the harness had no such case: measured on this
# tree, the row named a `GROUPS` case the script did not contain.
#
# THE OTHER FOUR LEGS the one stripped `docsSrc` made comment-proof (card
# `t_3b51370d`): this harness drove `{f.body}` and `id={f.slug}` (CASE 1-6) and
# the GROUPS parse (CASE 7) while the two legs a reader NOTICES FIRST — the
# category ANCHORS (`#sales-payments`, `#security`, …) and the TRANSLATED
# category labels — had no bite proof at all, though they are comment-satisfiable
# in exactly the same way. CASE 8-11 now drive them, one CONTROL (the render site
# moved off the page, no comment: RED in both columns) and one HOLE (the same
# mutation plus a `//` line carrying the token) each.
#
# THE MUTATION HELPERS FAIL CLOSED. `render_sites_off` / `comment_out_entry` run
# python to rewrite the page, and their non-zero status used to only short-circuit
# the `&&` chain: the case's `check` never ran, so that case measured nothing and
# the run could still print BITEPROOF PASS with the case's line silently missing
# from the output. Both now set `fail=1` and say so. Stated honestly: on TODAY's
# tree the premise check refuses first (a token that is no longer on the page also
# breaks the base guard, so the run exits 2 before any case is reached — measured),
# so this is defence in depth rather than a reproduced false PASS: the premise
# only covers the tokens that make the BASE column red, while a case whose mutation
# silently stops applying would otherwise be credited by the reader. (Found while
# adding CASE 8-11, whose whole point is that an undriven leg must not be credited.)
#
# THE VERDICT RULE REQUIRES POSITIVE EVIDENCE THAT THE GUARD RAN (card
# t_420ca40c, reused from web/scripts/biteproof_docs_tool_claims.sh verbatim): a
# verdict read from the ABSENCE of a `fail` line calls a guard that never started
# GREEN — which on the base column means "the base guard missed this", the exact
# claim this proof exists to make. GREEN needs node exit 0 with `ℹ pass [1-9]`
# AND a `✔ ` test line; RED needs `fail [1-9]` AND `pass [1-9]` (a guard that
# cannot load prints `tests 1 / pass 0 / fail 1` and calling that RED would print
# BITES for a case that wanted RED while the instrument was broken); anything else
# REFUSES with exit 2. The rule is self-checked on every run against three REAL
# degenerate node runs (all-skipped, cannot-load, missing).
#
# THE FAILURE COUNT IS PINNED TO 1 in the NEW column (the attribution rule from
# the sibling web/scripts/biteproof_docs_index_comment_anchor.sh): a red that is
# not exactly the vacuity test could be a sibling this mutation also broke, and
# then the bite would not be attributable to the sabotage. The count is read from
# node's own summary (`ℹ fail N`), not from counting `✖` lines — node prints each
# failure twice (once in the run, once under "✖ failing tests:"), so a grep over
# `✖` reports 2 or 3 for a single failure.
#
# Run:  bash web/scripts/biteproof_docs_index_render_sites.sh
set -uo pipefail

cd "$(dirname "$0")/.." || exit 2          # -> web/

PAGE=src/app/docs/page.tsx
GUARD=src/lib/docsIndex.test.ts
OLD_GUARD=$(mktemp)
PAGE_BAK=$(mktemp)
# Per-run temp copies of the guard, beside the real one: the guard resolves its
# paths from import.meta.url and node strips types by .ts extension. A FIXED name
# is the host-global channel two overlapping runs in one worktree would clobber
# (the trap t_ff3640e7 records for the tool-claims harness).
OLDTMP="$GUARD.oldcheck.$$.ts"
SKTMP="$GUARD.selfcheck_skipped.$$.ts"
NOLOADTMP="$GUARD.selfcheck_loadfail.$$.ts"
MISSINGTMP="$GUARD.selfcheck_missing.$$.ts"
cp "$PAGE" "$PAGE_BAK"
PAGE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)

fail=0

# The page is restored on EVERY exit, interrupts included, and every temp this run
# created is removed here. Restoring on the success path only is how a mutation
# survives an abort and the tree is left dirty.
cleanup() {
  local rc=$?
  cp "$PAGE_BAK" "$PAGE" 2>/dev/null
  rm -f "$OLD_GUARD" "$PAGE_BAK"
  rm -f "$OLDTMP" "$SKTMP" "$NOLOADTMP" "$MISSINGTMP"
  exit "$rc"
}
trap cleanup EXIT

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
guard_sha_before=$(sha256sum "$GUARD" | cut -d' ' -f1)

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines"

# rule_of <node exit status> — turns a guard run's output into a VERDICT.
# Reads $VERDICT_OUT (set by run_guard) and sets VERDICT (GREEN|RED|REFUSED) and
# VERDICT_WHY. Same rule as biteproof_docs_tool_claims.sh; see the header.
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

# run_guard <guardfile> — runs the guard and sets VERDICT / VERDICT_OUT.
# NOT called in a command substitution: that is a subshell and the variables would
# not come back to the caller. The guard EXITS NON-ZERO when it fails, so the
# status is captured rather than left for `set -o pipefail` to act on
# (`node --test … | grep -q` under pipefail reads a real match as "not found").
run_guard() {
  local rc=0
  VERDICT_OUT=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || rc=$?
  rule_of "$rc"
}

# The base guard is not in the tree; it runs as a sibling copy so the .ts extension
# is preserved (a copy named without it dies with `SyntaxError: Unexpected token
# ':'`, which reads as a RED guard).
run_base_guard() {
  cp "$OLD_GUARD" "$OLDTMP"
  run_guard "$OLDTMP"
  rm -f "$OLDTMP"
}

# ── PREMISE ──────────────────────────────────────────────────────────────────
# The base guard must be GREEN on the UNMUTATED tree. If it is not, the base is
# red for a reason that has nothing to do with this branch and no mutation can
# make it "miss" anything. Fail closed with the reason rather than printing a
# column against a broken instrument.
run_base_guard
if [ "$VERDICT" = REFUSED ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) did not RUN on the" >&2
  echo "unmutated tree (${VERDICT_WHY}), so there is no \"before\" column. Refusing (exit 2)." >&2
  grep -E '^ℹ ' <<<"$VERDICT_OUT" | head -6 >&2
  exit 2
fi
if [ "$VERDICT" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) is ${VERDICT} on the" >&2
  echo "UNMUTATED tree — a red base, not a finding about this branch. Refusing (exit 2)." >&2
  grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2 >&2
  grep -E '^✖' <<<"$VERDICT_OUT" | head -2 >&2
  exit 2
fi

# ── SELF-CHECK OF THE VERDICT RULE ───────────────────────────────────────────
# Every column is decided by rule_of, so the rule is the one thing that must not
# be trusted unmeasured. Three REAL degenerate node runs, one per false verdict
# the old absence-of-failure rule produced.
selfcheck_rule() {
  local bad=''
  rule_of 0
  [ "$VERDICT" = "GREEN" ] || bad="leg 1: a real driven guard run read as ${VERDICT}, wanted GREEN"
  printf '%s\n' 'import { test } from "node:test";' 'test.skip("a guard whose tests never run", () => {});' > "$SKTMP"
  printf '%s\n' 'const broken: = 1;' > "$NOLOADTMP"
  run_guard "$SKTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 2: an all-skipped guard read as ${VERDICT}, wanted REFUSED (${VERDICT_WHY})"
  run_guard "$NOLOADTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 3: a guard that cannot load read as ${VERDICT}, wanted REFUSED (that is how a broken instrument prints BITES)"
  run_guard "$MISSINGTMP"
  [ -z "$bad" ] && [ "$VERDICT" != "REFUSED" ] && bad="leg 4: a missing guard read as ${VERDICT}, wanted REFUSED"
  rm -f "$SKTMP" "$NOLOADTMP"
  if [ -n "$bad" ]; then
    echo >&2
    echo "BITEPROOF REFUSED: the VERDICT RULE ITSELF is broken — ${bad}." >&2
    echo "No verdict from this run would be worth anything. Fix rule_of() first." >&2
    grep -E '^ℹ ' <<<"$VERDICT_OUT" | head -6 >&2
    exit 2
  fi
}
selfcheck_rule

refuse() { # refuse <label> <column>
  echo >&2
  echo "  *** ${1}: the ${2} guard DID NOT RUN — ${VERDICT_WHY} ***" >&2
  echo "  *** A column with no guard run behind it is not a column. Refusing (exit 2) ***" >&2
  exit 2
}

# check <label> <old-want> <new-want> [must-name]
check() {
  local label="$1" old_want="$2" new_want="$3" needle="${4:-}"
  cp "$OLD_GUARD" "$OLDTMP"; run_guard "$OLDTMP"; rm -f "$OLDTMP"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "OLD"
  if [ "$VERDICT" != "$old_want" ]; then
    echo "  *** ${label}: OLD guard wanted ${old_want}, got ${VERDICT} — the case does not reproduce the premise ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ "$label" = "baseline" ]; then
    echo "  ok   baseline: old guard ${VERDICT}, as expected"
  elif [ "$old_want" = "GREEN" ] && [ "$new_want" = "GREEN" ]; then
    echo "  ok   ${label}: old guard GREEN, as expected (a negative control — the page really renders it)"
  elif [ "$old_want" = "GREEN" ]; then
    echo "  ok   ${label}: old guard GREEN — the hole reproduces (a comment satisfied the vacuity check)"
  else
    echo "  ok   ${label}: old guard RED — the control: with nothing carrying the token, the check does work"
  fi

  run_guard "$GUARD"
  [ "$VERDICT" = REFUSED ] && refuse "$label" "NEW"
  local nfail; nfail=$(sed -nE 's/^ℹ fail ([0-9]+)$/\1/p' <<<"$VERDICT_OUT" | head -1)
  : "${nfail:=0}"
  if [ "$VERDICT" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${VERDICT} ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$VERDICT_OUT"; then
    echo "  *** ${label}: NEW guard ${VERDICT} but the failure does not name ${needle} ***"
    grep -E '^✖' <<<"$VERDICT_OUT" | head -3; fail=1
  elif [ "$new_want" = "RED" ] && [ "$nfail" != "1" ]; then
    echo "  *** ${label}: expected exactly 1 failing test, saw ${nfail} — the red is not attributable to this mutation ***"
    grep -E '^✖' <<<"$VERDICT_OUT" | head -4; fail=1
  else
    echo "  BITES ${label}: new guard ${VERDICT}, ${nfail} failing test(s)${needle:+, naming: ${needle}}"
  fi
}

# insert_at_line <lineno> <text> — inserts <text> after line <lineno>, or before
# line 1 when <lineno> is 0. Written through a temp file so quotes and backslashes
# in the text reach the page untouched (awk -v would eat them).
insert_at_line() {
  local n="$1" text="$2" tmp; tmp=$(mktemp)
  { head -n "$n" "$PAGE"; printf '%s\n' "$text"; tail -n +"$((n + 1))" "$PAGE"; } > "$tmp"
  mv "$tmp" "$PAGE"
}
# anchor_line <anchor-text> — the page line holding it, or a refusal naming the
# anchor. An insert at the wrong place would make a case read GREEN for the wrong
# reason (the token landing somewhere the reader cannot see), so the anchor is
# resolved rather than assumed.
anchor_line() {
  local n; n=$(grep -nF "$1" "$PAGE" | head -1 | cut -d: -f1)
  if [ -z "$n" ]; then
    echo "  *** ANCHOR '$1' NOT FOUND in $PAGE — the page's shape moved; re-anchor this case ***" >&2
    fail=1; return 1
  fi
  printf '%s' "$n"
}
# render_sites_off <token> <replacement> — the page-wide swap the card measured.
# python3, not sed: the replacement carries `/` and `[^…]` and a sed delimiter
# would have to be escaped twice.
#
# FAIL CLOSED: a token that is not on the page any more means the case cannot be
# applied, and a case that silently does not run must not be reported as a pass
# (see the header). The caller's `&&` chain skips the `check`, so the failure is
# recorded here.
render_sites_off() {
  if python3 - "$PAGE" "$1" "$2" <<'PY'
import sys
p, token, repl = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(p).read()
n = s.count(token)
if n == 0:
    sys.exit(f"token {token!r} occurs 0 times in {p}")
open(p, "w").write(s.replace(token, repl))
print(f"       mutation on disk: {n} occurrence(s) of {token} -> {repl}")
PY
  then
    return 0
  fi
  echo "  *** MUTATION DID NOT APPLY to $PAGE: '$1' is not on the page — re-anchor this case ***" >&2
  fail=1
  return 1
}
assert_applied() { # assert_applied <token>
  if grep -qF "$1" "$PAGE"; then echo "       present on disk: $1"; else
    echo "  *** MUTATION DID NOT APPLY to $PAGE: $1 ***"; fail=1; return 1; fi
}
# comment_out_entry <title-substring> — comments out the whole object literal of
# the one entry whose `title:` holds the substring: the nearest preceding line
# that is exactly `{`, through the nearest following line that is exactly `},`,
# each prefixed with `// `. This is the mutation the register's row used to CLAIM
# without the harness driving it (card t_8b8d3365): the commented entry is gone
# from the RENDERED page while the base guard, which reads the raw source, still
# counts it as a feature entry.
comment_out_entry() {
  if python3 - "$PAGE" "$1" <<'PY'
import sys
p, needle = sys.argv[1], sys.argv[2]
lines = open(p).read().split("\n")
title = next((i for i, l in enumerate(lines) if "title: " in l and needle in l), None)
if title is None:
    sys.exit(f"no entry in {p} has a title holding {needle!r}")
open_ = next((i for i in range(title, -1, -1) if lines[i].strip() == "{"), None)
close = next((i for i in range(title, len(lines)) if lines[i].strip() == "},"), None)
if open_ is None or close is None:
    sys.exit(f"could not bracket the {needle!r} entry ({open_}..{close})")
for i in range(open_, close + 1):
    lines[i] = "// " + lines[i]
open(p, "w").write("\n".join(lines))
print(f"       mutation on disk: entry {needle!r} commented out, lines {open_ + 1}-{close + 1}")
PY
  then
    return 0
  fi
  echo "  *** MUTATION DID NOT APPLY to $PAGE: no entry whose title holds '$1' could be bracketed — re-anchor this case ***" >&2
  fail=1
  return 1
}
restore_page() {
  cp "$PAGE_BAK" "$PAGE"
  [ "$(sha256sum "$PAGE" | cut -d' ' -f1)" = "$PAGE_SHA" ] \
    && echo "       restored byte-identical ($(printf '%s' "$PAGE_SHA" | cut -c1-12))" \
    || { echo "  *** RESTORE MISMATCH $PAGE ***"; fail=1; }
}

# The reader-invisible token the OLD guard must swallow. Spelled once so every
# case carries the SAME text a real comment would.
CMT_BODY='each entry renders {f.body} in its paragraph, so the summary is shown'
CMT_SLUG='each entry renders id={f.slug} on its wrapper, so ? anchors scroll'
CMT_GID='each category section renders id={g.id}, so #sales-payments and friends scroll'
CMT_LABEL='the category heading renders <T k={g.labelKey} />, so the label is translated'

echo
echo "== BASELINE (no mutation) =="
check "baseline" GREEN GREEN

echo
echo "== CASE 1 (CONTROL): {f.body} moved off the page, NO comment =="
render_sites_off '{f.body}' '{f.title}' && {
  check "control: {f.body} not rendered, no comment" RED RED 'does not render {f.body}'; }
restore_page

echo
echo "== CASE 2 (HOLE): the same, plus a // line carrying {f.body} =="
render_sites_off '{f.body}' '{f.title}' && {
  insert_at_line 0 "// QA: $CMT_BODY"
  assert_applied '{f.body}' && {
    check "{f.body} carried by a // comment" GREEN RED 'does not render {f.body}'; }
}
restore_page

echo
echo "== CASE 3 (CONTROL): id={f.slug} moved off BOTH render sites, NO comment =="
# The card's own mutation, verbatim: the two real sites (:429 and :474) are the
# only place the slug reaches the DOM, and both are swapped.
render_sites_off 'id={f.slug}' 'id={f.title.replace(/[^a-zA-Z0-9]+/g, "-").toLowerCase()}' && {
  check "control: id={f.slug} not rendered, no comment" RED RED 'does not render the slug as an id'; }
restore_page

echo
echo "== CASE 4 (HOLE): the card's table — the same, plus a // line carrying id={f.slug} =="
render_sites_off 'id={f.slug}' 'id={f.title.replace(/[^a-zA-Z0-9]+/g, "-").toLowerCase()}' && {
  insert_at_line 0 "// QA: $CMT_SLUG"
  assert_applied 'id={f.slug}' && {
    check "id={f.slug} carried by a // comment" GREEN RED 'does not render the slug as an id'; }
}
restore_page

echo
echo "== CASE 5 (HOLE): id={f.slug} carried by a {/* … */} JSX container =="
# The container form the strip's doc block names, inserted INSIDE the JSX (where
# a JSX comment legally sits) rather than at module scope.
render_sites_off 'id={f.slug}' 'id={f.title.replace(/[^a-zA-Z0-9]+/g, "-").toLowerCase()}' && {
  if n=$(anchor_line '<div className="max-w-3xl">'); then
    insert_at_line "$n" "      {/* QA: $CMT_SLUG */}"
    assert_applied 'id={f.slug}' && {
      check "id={f.slug} carried by a {/* */} container" GREEN RED 'does not render the slug as an id'; }
  fi
}
restore_page

echo
echo "== CASE 6 (HOLE): {f.body} carried by a module-scope /* … */ block comment =="
render_sites_off '{f.body}' '{f.title}' && {
  insert_at_line 0 "/*
 * QA: $CMT_BODY
 */"
  assert_applied '{f.body}' && {
    check "{f.body} carried by a module-scope block comment" GREEN RED 'does not render {f.body}'; }
}
restore_page

echo
echo "== CASE 7 (HOLE): a feature entry commented out of GROUPS — not a render site at all =="
# The fourth form: the token is not a render site, the ENTRY is. `parseGroups`
# reads the same `docsSrc`, so commenting the whole object out left the base guard
# counting a feature the page no longer lists (its docs-parity contract is
# `ORIGINAL_TITLES`, and the title was still in the raw text). "Media library" is
# chosen because it is one of the four entries with NO `href` (see the guard's own
# note at DESTINATION_FACTS): an entry with a destination would also change the
# `DESTINATION_FACTS` / `PER_ENTRY_FLOOR` maps, which would make the NEW red
# unattributable to this mutation (the harness pins exactly 1 failing test).
comment_out_entry "Media library" && {
  assert_applied "//         title: \"Media library\"" && {
    check "feature entry commented out of GROUPS" GREEN RED 'no feature was lost in the reorganisation'; }
}
restore_page

echo
echo "== CASE 8 (CONTROL): id={g.id} moved off the group sections, NO comment =="
# Card t_3b51370d. The category ANCHORS ("#sales-payments", "#security", …) are
# the leg a reader follows from the sidebar, and the one stripped `docsSrc` makes
# it comment-satisfiable exactly like the slug. `id={g.labelKey}` is a real
# expression that renders a real id — just not the one the sidebar points at —
# so the mutation is a plausible edit and not a syntax error.
render_sites_off 'id={g.id}' 'id={g.labelKey}' && {
  check "control: id={g.id} not rendered, no comment" RED RED 'every category is a named, translated group'; }
restore_page

echo
echo "== CASE 9 (HOLE): the same, plus a // line carrying id={g.id} =="
render_sites_off 'id={g.id}' 'id={g.labelKey}' && {
  insert_at_line 0 "// QA: $CMT_GID"
  assert_applied 'id={g.id}' && {
    check "id={g.id} carried by a // comment" GREEN RED 'every category is a named, translated group'; }
}
restore_page

echo
echo "== CASE 10 (CONTROL): <T k={g.labelKey} /> moved off the headings, NO comment =="
# The translation leg: the category headings are the only place a group's labelKey
# reaches the page. `{g.id}` is again a real, compiling substitution.
render_sites_off '<T k={g.labelKey} />' '<T k={g.id} />' && {
  check "control: category labels not translated, no comment" RED RED 'every category is a named, translated group'; }
restore_page

echo
echo "== CASE 11 (HOLE): the same, plus a // line carrying <T k={g.labelKey} /> =="
render_sites_off '<T k={g.labelKey} />' '<T k={g.id} />' && {
  insert_at_line 0 "// QA: $CMT_LABEL"
  assert_applied '<T k={g.labelKey} />' && {
    check "category label carried by a // comment" GREEN RED 'every category is a named, translated group'; }
}
restore_page

echo
echo "== NEGATIVE CONTROL 12: a comment mentioning the token must NOT break a page that really renders it =="
# The other direction of this fix, and the one a careless strip breaks: the real
# sites are untouched, a comment names the same tokens, and the vacuity test must
# stay GREEN under BOTH guards. Without this case a strip that stripped rendered
# code as well would pass every case above.
insert_at_line 0 "// QA: this page renders $CMT_BODY and $CMT_SLUG"
insert_at_line 0 "// QA: this page renders $CMT_GID and $CMT_LABEL"
assert_applied '{f.body}' && {
  assert_applied 'id={g.id}' && {
    check "comment beside a real render site" GREEN GREEN; }
}
restore_page

echo
echo "== GUARD RESTORED =="
if [ "$(sha256sum "$GUARD" | cut -d' ' -f1)" = "$guard_sha_before" ]; then
  echo "       $GUARD untouched (sha256 unchanged) — this harness mutates the page, never the guard"
else
  echo "  *** $GUARD CHANGED during the run ***"; fail=1
fi
if [ -n "$(git status --porcelain -- "$PAGE")" ]; then
  echo "  *** $PAGE is DIRTY after the run — a case left a mutation on disk ***"; fail=1
else
  echo "       $PAGE clean in git after the run"
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — every comment form was GREEN under ${BASE_REF}'s guard and RED, naming the leg it broke, under this one (the four no-comment controls RED in both columns, the GROUPS case RED naming the docs-parity contract)."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
