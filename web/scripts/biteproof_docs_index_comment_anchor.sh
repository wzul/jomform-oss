#!/usr/bin/env bash
# Bite proof for web/src/lib/docsIndex.test.ts's declaresId() comment hole
# (card t_7b24b035).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: every case
# below is an ANCHOR THAT DOES NOT EXIST, made to look declared by text a reader
# never sees. A test inside the guard cannot show that, because it can only
# exercise the guard's OWN rules, not the absence of them — the "before" column
# has to come from the guard as it stands on the branch's base.
#
# Each case: mutate -> assert the mutation is really on disk -> run the BASE guard
# (expect GREEN, i.e. the hole) -> run the guard in this working tree (expect RED,
# naming the offender) -> restore byte-identically (re-checked with sha256).
#
# THE MUTATION IS THE CARD'S OWN MEASURED ONE, kept verbatim as case 1:
#   layout.tsx gains  { href: "/docs/security#qa-fake-anchor", label: "QA probe guide" }
#   security/page.tsx gains a `//` line containing  id: "qa-fake-anchor"
# and the page renders no such id. On the base guard that leaves the whole
# `npm run test:unit` suite GREEN (516/0) and `next build` exit 0, while the served
# HTML carries the href and no matching id — a reader clicking that sidebar row
# lands at the top of the page. Cases 2 and 3 are the same phantom in the two
# other comment forms the strip is supposed to catch, case 4 drives a DIFFERENT
# leg of the same helper (the index's own guide link, not the sidebar), case 5 is
# the control that a REAL declaration still satisfies it, case 6 requires the
# `/docs` index's `slug:` exception to survive the strip in both directions, and
# case 7 is the unmutated baseline.
#
# Run:  bash web/scripts/biteproof_docs_index_comment_anchor.sh
set -uo pipefail

cd "$(dirname "$0")/.." || exit 2          # -> web/

GUARD=src/lib/docsIndex.test.ts
LAYOUT=src/app/docs/layout.tsx
SECURITY=src/app/docs/security/page.tsx
INDEX=src/app/docs/page.tsx

# The three files the cases sabotage, and the base guard, all restored/removed on
# exit. `git status`/sha256 are re-checked per case, not assumed.
#
# NOTE the backup names are keyed by a LABEL, not by basename: SECURITY and INDEX
# are both `page.tsx`, and an earlier cut of this script backed both up to the same
# `page.tsx.bak`, so restoring wrote the index's source over the security page (and
# `restore_all` then reported success, because it compared each file against the
# hash captured for the OTHER one). Every file is now addressed by (label, path) and
# its hash is held in `sha_<label>`.
BAK=$(mktemp -d)
OLD_GUARD=$(mktemp)
trap 'rm -f "$OLD_GUARD"; rm -rf "$BAK"' EXIT

# The sabotaged files, as label:path pairs. `layout` and `security` are mutated by
# cases 1-3 and 5; `index` by cases 4 and 6a.
FILES=(layout src/app/docs/layout.tsx
       security src/app/docs/security/page.tsx
       index src/app/docs/page.tsx)

fail=0

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

# CAPABILITY REFUSAL (card t_50e46013). Byte-equality between the base guard and this
# tree's is NOT the property this harness needs: it needs the base's reader to be
# BLIND to the mutation. Once the branch's own fix lands on the base, the two readers
# are the same one, every BASE leg reads RED, and the cases report
#
#   *** <case>: BASE guard wanted GREEN, got RED — the case does not reproduce the hole
#
# which blames the CASE for a premise that the BASE cannot satisfy. That is the wrong
# finding, and it is exactly the shape the sibling api harness refuses by name
# (`capability_refusal` in api/scripts/biteproof_model_default_guard.py: "REFUSING
# (exit 2, NO VERDICT)"). Measured on origin/main: the base tree's reader is
# byte-identical to this tree's (sha256 06d32d0d2f1dcf05 on both), so the harness
# printed 2 `***` lines and exit 1 against a sound branch.
#
# The reader is judged by its DECLARATION, not by its bytes — the same rule this
# harness's sibling (`biteproof_docs_index_render_sites.sh`) already applies, with the
# same two spellings. A base declaring the POST-fix reader cannot demonstrate the
# blind spot, so the run stops here with the reason and the fix rather than producing a
# verdict about nothing.
# strip_ts_comments — drop `//` tails and `/* ... */` blocks from TypeScript source
# before a DECLARATION is matched in it (card t_80a7fb64).
#
# THE HOLE THE ANCHORING WAS MEANT TO CLOSE. The pattern is line-anchored because "a
# match anywhere in the file would be satisfied by the same text quoted in a comment or
# a string while the real reader is something else". That argument holds for a `//` line
# comment, whose body carries a `// ` prefix between the line start and `const`. It does
# NOT hold for a BLOCK comment: a `/* ... */` body line sits at column 0 with no marker,
# so it is byte-identical in shape to a real declaration. `reader_class` ran on the RAW
# body, so a post-fix guard carrying the stale reader inside a block comment was
# classified PRE — the base is then treated as the "before" column although it already
# declares the fix, and the run reports a silent fail=1 (the D3 residual of card
# t_01df90af).
#
# Over-stripping FAILS CLOSED: a `//` inside a string literal (a URL) removes the rest of
# that line, which can only make a declaration UNREADABLE, and every caller already
# refuses on UNKNOWN rather than assuming pre-fix. Under-stripping is the unsafe
# direction, which is what this removes.
strip_ts_comments() {
  awk '
    BEGIN { inblock = 0 }
    {
      line = $0; out = ""; i = 1; n = length(line)
      while (i <= n) {
        two = substr(line, i, 2)
        if (inblock) {
          if (two == "*/") { inblock = 0; i += 2 } else { i++ }
        } else if (two == "/*") {
          inblock = 1; i += 2
        } else if (two == "//") {
          break
        } else {
          out = out substr(line, i, 1); i++
        }
      }
      print out
    }'
}
PRE_READER_RE='^[[:space:]]*(export[[:space:]]+)?(const|let|var)[[:space:]]+docsSrc[[:space:]]*=[[:space:]]*read\(DOCS_PAGE\)'
POST_READER_RE='^[[:space:]]*(export[[:space:]]+)?(const|let|var)[[:space:]]+docsSrc[[:space:]]*=[[:space:]]*stripUnrendered\(read\(DOCS_PAGE\)\)'
# RAW_DECLARES_ID_RE: the anchor helper still reading the UNSTRIPPED page, i.e. the base
# is blind to the comment-carried anchor cases too (t_7b24b035 fixed those separately).
RAW_DECLARES_ID_RE='const[[:space:]]+src[[:space:]]*=[[:space:]]*read\(file\)'
if grep -qE "$POST_READER_RE" "$OLD_GUARD"; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the base guard (${BASE_REF} ${BASE_REV:0:12}) declares the" >&2
  echo "POST-fix reader — \`docsSrc = stripUnrendered(read(DOCS_PAGE))\` — so it is the SAME reader this" >&2
  echo "tree uses and it cannot miss the comment-carried hole every case drives. Every BASE leg would" >&2
  echo "read RED and each case would report 'does not reproduce the hole', which is a statement about the" >&2
  echo "BASE and not about this branch." >&2
  echo >&2
  echo "Point BITEPROOF_BASE at a revision whose guard still reads the RAW page source:" >&2
  # Derive one rather than guess: the newest commit in the guard's history whose
  # `docsSrc` declaration is still the raw reader. Two traps, BOTH measured, are why
  # this loop looks the way it does:
  #
  #   * `git show ... | grep -q` silently reports NO MATCH under `set -o pipefail` —
  #     grep exits at the first hit, the writer takes SIGPIPE (141), and the pipeline
  #     reports failure. Use a herestring, as the sibling harnesses do.
  #   * `git log -- <pathspec>` resolves the path against the CWD, and this script has
  #     already `cd`-ed to `web/`, so the walk found ZERO commits. `git -C "$TOP"`.
  #
  # AND ITS LIMIT, MEASURED RATHER THAN ASSUMED. A raw `docsSrc` is necessary but NOT
  # sufficient: the anchor legs were fixed SEPARATELY (t_7b24b035), so a base between
  # the two fixes is blind to the `docsSrc` cases and still catches the anchor ones.
  # Verified on two candidates by reading the guard at each revision, not by trusting
  # the commit message:
  #
  #   eea308fe (raw docsSrc, `declaresId` already reads the COMMENT-STRIPPED source)
  #     -> blind to the 2 `docsSrc` cases, still catches the 3 anchor cases.
  #     2089b6fb (raw docsSrc, `declaresId` reads `read(file)`)
  #     -> blind to both, so it reproduces every case.
  #
  # So the search looks for BOTH properties, and the run is only as reproducible as the
  # base it names — which the message says out loud instead of implying otherwise.
  suggested=""
  while read -r rev; do
    [ -n "$rev" ] || continue
    _body="$(git show "${rev}:${GUARD_IN_REPO}" 2>/dev/null)"
    if grep -qE "$PRE_READER_RE" <<<"$(strip_ts_comments <<<"$_body")" && grep -qE "$RAW_DECLARES_ID_RE" <<<"$(strip_ts_comments <<<"$_body")"; then
      suggested="$rev"; break
    fi
  done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  # A raw docsSrc with the anchor helper already stripped is the better answer than
  # nothing when no fully-blind base exists, so fall back to it and say which it is.
  partial=0
  if [ -z "$suggested" ]; then
    while read -r rev; do
      [ -n "$rev" ] || continue
      _body="$(git show "${rev}:${GUARD_IN_REPO}" 2>/dev/null)"
      if grep -qE "$PRE_READER_RE" <<<"$(strip_ts_comments <<<"$_body")"; then suggested="$rev"; partial=1; break; fi
    done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  fi
  if [ -n "$suggested" ]; then
    echo "    BITEPROOF_BASE=${suggested} bash ${GUARD_IN_REPO}" >&2
    if [ "$partial" = 1 ]; then
      echo "    (${suggested:0:12}: raw \`docsSrc\`, so the \`docsSrc\`-carried cases reproduce — but this" >&2
      echo "     revision's anchor helper ALREADY reads the stripped source (t_7b24b035), so the ANCHOR" >&2
      echo "     cases still report 'does not reproduce the hole'.)" >&2
    else
      echo "    (${suggested:0:12}: raw \`docsSrc\` AND a raw anchor helper. Measured: every case then" >&2
      echo "     reproduces its hole, and the four ANCHOR cases fail instead on the attribution rule" >&2
      echo "     ('expected exactly 1 failing test, saw 2') because this branch's fix makes a SECOND test" >&2
      echo "     red alongside the one each case drives — a real, attributable red rather than a premise" >&2
      echo "     failure, but the harness still cannot print a clean 8/8 against this tree.)" >&2
    fi
  else
    echo "    (no revision in this branch's history of ${GUARD_IN_REPO} declares the raw reader either," >&2
    echo "     so no pre-fix revision can be named from here — set BITEPROOF_BASE by hand)" >&2
  fi
  exit 2
fi

for ((i = 0; i < ${#FILES[@]}; i += 2)); do
  label=${FILES[i]}; path=${FILES[i + 1]}
  cp "$path" "$BAK/$label.bak"
  eval "SHA_$label=$(sha256sum "$path" | cut -d' ' -f1)"
done
sha_of() { sha256sum "$1" | cut -d' ' -f1; }

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines"

# run_guard <guardfile> -> prints "GREEN" or "RED"; the full output lands in the
# file named by OUT_FILE.
#
# Two traps this shape avoids, both measured in sibling harnesses:
#   * the guard EXITS NON-ZERO when it fails, so the output is captured before it
#     is inspected. `node --test … | grep -q` under `set -o pipefail` reports the
#     node exit status and reads a real match as "not found" — that exact trap
#     produced a false verdict once.
#   * run_guard is called as `got=$(run_guard …)`, so IT RUNS IN A SUBSHELL: an
#     assignment to a normal variable inside it is discarded, and the caller then
#     greps an empty string while the verdict still looks right. The output is
#     therefore written to a FILE, which survives the subshell. (An earlier cut of
#     this script set a variable here and every `must-name` check failed for this
#     reason while the reds were real.)
OUT_FILE=$(mktemp)
trap 'rm -f "$OLD_GUARD" "$OUT_FILE"; rm -rf "$BAK"' EXIT
run_guard() {
  local out
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || true
  printf '%s\n' "$out" > "$OUT_FILE"
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
}

# The base guard is not in the tree; it is run as a sibling copy so the .ts
# extension is preserved (node strips types by extension, and a copy named without
# it dies with `SyntaxError: Unexpected token ':'`, which reads as a RED guard).
# It lives beside the real guard because the guard resolves its paths from
# import.meta.url.
run_base_guard() {
  local oldtmp="$GUARD.oldcheck.ts"
  cp "$OLD_GUARD" "$oldtmp"
  local got; got=$(run_guard "$oldtmp")
  rm -f "$oldtmp"
  printf '%s' "$got"
}
guard_sha_before=$(sha_of "$GUARD")

# insert_after <file> <lineno> <text> — inserts <text> after line <lineno>, or
# before line 1 when <lineno> is 0. Written this way so backslashes and quotes in
# the text reach the file untouched (awk -v would eat them).
#
# THE INSERT POINT IS LOAD-BEARING. The guard's parsers read a REGION sliced from a
# named anchor (`const NAV_GROUPS`, `const NAV_SECTIONS`, `const GROUPS`), so a row
# inserted before that anchor is not a row the guard can see — an earlier cut of
# this script inserted at line 1 and case 1 read GREEN for that reason, not because
# the hole survived. `nav_row`/`nav_section`/`index_row` below resolve the anchor
# line first and refuse if it moved.
anchor_line() { # anchor_line <file> <anchor-text>
  local n; n=$(grep -nF "$2" "$1" | head -1 | cut -d: -f1)
  if [ -z "$n" ]; then
    echo "  *** ANCHOR '$2' NOT FOUND in $1 — the guard's region moved ***" >&2; fail=1; return 1
  fi
  printf '%s' "$n"
}
insert_after() {
  local f="$1" n="$2" text="$3" tmp; tmp=$(mktemp)
  { head -n "$n" "$f"; printf '%s\n' "$text"; tail -n +"$((n + 1))" "$f"; } > "$tmp"
  mv "$tmp" "$f"
}
# nav_row <text> — append a row inside NAV_GROUPS (after its `const NAV_GROUPS` line).
nav_row() {
  local n; n=$(anchor_line "$LAYOUT" "const NAV_GROUPS") || return 1
  insert_after "$LAYOUT" "$n" "$1"
}
# nav_section <text> — append a row inside NAV_SECTIONS.
nav_section() {
  local n; n=$(anchor_line "$LAYOUT" "const NAV_SECTIONS") || return 1
  insert_after "$LAYOUT" "$n" "$1"
}
assert_applied() { # assert_applied <file> <token>
  if grep -qF "$2" "$1"; then echo "       mutation on disk: $2"; else
    echo "  *** MUTATION DID NOT APPLY to $1: $2 ***"; fail=1; return 1; fi
}
restore_all() {
  local ok=1 label path
  for ((i = 0; i < ${#FILES[@]}; i += 2)); do
    label=${FILES[i]}; path=${FILES[i + 1]}
    cp "$BAK/$label.bak" "$path"
    if [ "$(sha_of "$path")" != "$(eval "printf '%s' \"\$SHA_$label\"")" ]; then
      echo "  *** RESTORE MISMATCH $path (label $label) ***"; fail=1; ok=0
    fi
  done
  [ "$ok" = 1 ] && echo "       restored byte-identical (3 files, each against its own hash)"
}

# PREMISE CHECK, before any case runs: the "before" guard must be GREEN on the
# tree as it stands, with no mutation. If it is not, the base itself is red for a
# reason that has nothing to do with this branch — and then no mutation can make
# it "miss" anything. Fail closed with the reason and the fix.
premise=$(run_base_guard)
if [ "$premise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) is ${premise} on the" >&2
  echo "UNMUTATED tree, so there is no \"before\" column and this proof would be about nothing." >&2
  echo "That is a red base, not a finding about this branch. Land the base's fix, or point" >&2
  echo "BITEPROOF_BASE at a base whose guard is green:" >&2
  grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2 >&2
  grep -E '^✖' "$OUT_FILE" | head -2 >&2
  exit 2
fi

# check <label> <base-want> <new-want> [must-name]
#
# `must-name` is the whole point of the new column: a RED that does not name the
# phantom would be attributable to a sibling, and the failure COUNT is pinned to 1
# so the red cannot be a second test that this mutation also broke.
check() {
  local label="$1" base_want="$2" new_want="$3" needle="${4:-}" got out
  got=$(run_base_guard); out=$(cat "$OUT_FILE")
  if [ "$got" != "$base_want" ]; then
    echo "  *** ${label}: BASE guard wanted ${base_want}, got ${got} — the case does not reproduce the hole ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  else
    echo "  ok   ${label}: base guard ${got} — the hole reproduces"
  fi

  got=$(run_guard "$GUARD"); out=$(cat "$OUT_FILE")
  # The count comes from node's own summary (`ℹ fail N`), not from counting `✖`
  # lines: node prints each failure TWICE — once in the run, once under
  # "✖ failing tests:" — so a grep over `✖` reports 2 or 3 for a single failure and
  # the attribution check below would always fail.
  local nfail; nfail=$(sed -nE 's/^ℹ fail ([0-9]+)$/\1/p' <<<"$out" | head -1)
  : "${nfail:=0}"
  if [ "$got" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${got} ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out"; then
    echo "  *** ${label}: NEW guard ${got} but the failure does not name ${needle} ***"
    grep -E '^✖' <<<"$out" | head -3; fail=1
  elif [ "$new_want" = "RED" ] && [ "$nfail" != "1" ]; then
    echo "  *** ${label}: expected exactly 1 failing test, saw ${nfail} — the red is not attributable to this mutation ***"
    grep -E '^✖' <<<"$out" | head -4; fail=1
  else
    echo "  BITES ${label}: new guard ${got}, ${nfail} failing test(s)${needle:+, naming: ${needle}}"
  fi
}

# run_base_guard is used twice per case (premise + case); it re-copies each time,
# so a stale temp copy can never be compared against itself.

echo
echo "== BASELINE (no mutation) =="
check "baseline" GREEN GREEN

echo
echo "== CASE 1: the card's measured mutation — a // line behind a phantom sidebar anchor =="
# The card's measurement did this in NAV_SECTIONS (the sidebar's "Guides" block);
# that is the leg PR 843 added at :969 and the one the card quotes.
nav_section '  { href: "/docs/security#qa-fake-anchor", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs/security#qa-fake-anchor"' && {
  insert_after "$SECURITY" 0 '// QA: this page declares id: "qa-fake-anchor" for the sidebar row.'
  assert_applied "$SECURITY" 'id: "qa-fake-anchor"' && {
    check "phantom anchor in a // comment" GREEN RED '/docs/security#qa-fake-anchor (Guides) — /docs/security declares no id "qa-fake-anchor"'; }
}
restore_all

echo
echo "== CASE 2: the same phantom in a {/* … */} JSX container =="
nav_section '  { href: "/docs/security#qa-fake-anchor", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs/security#qa-fake-anchor"' && {
  insert_after "$SECURITY" 58 '      {/* QA: this page declares id: "qa-fake-anchor" for the sidebar row. */}'
  assert_applied "$SECURITY" 'id: "qa-fake-anchor"' && {
    check "phantom anchor in a {/* */} container" GREEN RED '/docs/security#qa-fake-anchor (Guides) — /docs/security declares no id "qa-fake-anchor"'; }
}
restore_all

echo
echo "== CASE 3: the same phantom in a module-scope block comment =="
nav_section '  { href: "/docs/security#qa-fake-anchor", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs/security#qa-fake-anchor"' && {
  insert_after "$SECURITY" 12 '/*
 * QA probe: the sidebar row points at id: "qa-fake-anchor", which this page does
 * not render.
 */'
  assert_applied "$SECURITY" 'id: "qa-fake-anchor"' && {
    check "phantom anchor in a module-scope block comment" GREEN RED '/docs/security#qa-fake-anchor (Guides) — /docs/security declares no id "qa-fake-anchor"'; }
}
restore_all

echo
echo "== CASE 4: a DIFFERENT leg — the index entry's own guide link (:445) =="
# Repoint the "Trusted devices (2FA)" entry (index line 301, `href:
# "/docs/security#trusted-devices"`) at the phantom. The route still resolves, so
# this drives the ENTRY leg rather than the sidebar leg above. The line is located
# by the anchor it currently carries, so this refuses if that entry moved.
if grep -qF '"/docs/security#trusted-devices"' "$INDEX"; then
  sed -i 's|"/docs/security#trusted-devices"|"/docs/security#qa-fake-anchor"|' "$INDEX"
  assert_applied "$INDEX" '/docs/security#qa-fake-anchor' && {
    insert_after "$SECURITY" 0 '// QA: this page declares id: "qa-fake-anchor".'
    assert_applied "$SECURITY" 'id: "qa-fake-anchor"' && {
      check "phantom anchor behind an entry's guide link" GREEN RED 'Trusted devices (2FA) → /docs/security#qa-fake-anchor'; }
  }
else
  echo "  *** the entry whose guide link this case repoints moved in $INDEX ***"; fail=1
fi
restore_all

echo
echo "== NEGATIVE CONTROL 5: the phantom anchor backed by a REAL declaration stays GREEN =="
nav_section '  { href: "/docs/security#qa-fake-anchor", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs/security#qa-fake-anchor"' && {
  # A real `{ id: … }` row in the page's own data table — the anchor exists, so
  # the strip must NOT have blinded the helper to it. Inserted INSIDE SECTIONS
  # (after its `const SECTIONS: Section[] = [` line) so it is a row, not a stray.
  insert_after "$SECURITY" 14 '  { id: "qa-fake-anchor", title: "QA probe", body: ["x"] },'
  assert_applied "$SECURITY" 'id: "qa-fake-anchor"' && {
    check "real id declaration (the strip must not blind the helper)" GREEN GREEN; }
}
restore_all

echo
echo "== CASE 6: the /docs INDEX slug exception survives the strip — in both directions =="
# 6a: a `// slug:` comment must no longer satisfy the exception.
nav_section '  { href: "/docs#qa-fake-anchor", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs#qa-fake-anchor"' && {
  insert_after "$INDEX" 0 '// QA: this page declares slug: "qa-fake-anchor" for the sidebar row.'
  assert_applied "$INDEX" 'slug: "qa-fake-anchor"' && {
    check "phantom /docs anchor behind a // slug line" GREEN RED '/docs#qa-fake-anchor (Guides) — /docs declares no id "qa-fake-anchor"'; }
}
restore_all
# 6b: …and the real one (slug: "workspaces" → id={f.slug}, nine of them) still does.
# `form-analytics` is the better subject: it is a real slug in the index's GROUPS
# table and appears in the nav NOWHERE else, so this row is genuinely new and the
# anchor it names exists only through the exception — i.e. the strip must not have
# blinded the `slug:` path.
nav_section '  { href: "/docs#form-analytics", label: "QA probe guide" },'
assert_applied "$LAYOUT" '"/docs#form-analytics"' && {
  check "/docs#form-analytics, a real slug (the exception must survive)" GREEN GREEN; }
restore_all

echo
echo "== GUARD RESTORED =="
if [ "$(sha_of "$GUARD")" = "$guard_sha_before" ]; then
  echo "       $GUARD untouched (sha256 unchanged) — this harness mutates pages, never the guard"
else
  echo "  *** $GUARD CHANGED during the run ***"; fail=1
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — every phantom was GREEN under ${BASE_REF}'s guard and RED, naming the anchor, under this one."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
