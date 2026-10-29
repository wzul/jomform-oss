#!/usr/bin/env bash
# Bite proof for web/src/lib/docsIndex.test.ts's declaresId() comment hole
# (card t_7b24b035).
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: every case
# below is an ANCHOR THAT DOES NOT EXIST, made to look declared by text a reader
# never sees. A test inside the guard cannot show that, because it can only
# exercise the guard's OWN rules, not the absence of them — the "before" column
# has to be a reader that still holds the hole.
#
# THE "BEFORE" COLUMN IS RE-BASED ONTO THIS TREE (card t_b5690577). It used to be a
# REVISION (`origin/main`, or one pointed at by BITEPROOF_BASE), and that stopped working
# once the fix merged — main IS the fixed guard — and once the guard around this one reader
# drifted from today's /docs pages, which made every pre-fix revision red on an UNMUTATED
# tree. It is now this tree's guard with ONLY the helper's source line put back to the
# spelling the base revision declares, so the two columns differ by exactly the rule this
# case set drives (see the derivation below the base resolution, which is count-checked
# and cannot silently collapse into one reader).
#
# Each case: mutate -> assert the mutation is really on disk -> run the BEFORE guard
# (expect GREEN, i.e. the hole) -> run the guard in this working tree (expect RED,
# naming the offender in every failing test) -> restore byte-identically (re-checked
# with sha256).
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

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD — a
# relative `$0` resolved after the `cd` names a file that is not there.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# The shared runner gate and verdict rule (web/scripts/lib/node_test_verdict.sh): every
# verdict this harness prints comes from POSITIVE runner evidence, and a node that cannot
# run the guard is a REFUSAL that names itself — never an answer (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no verdict rule to read a guard's output with." >&2
  exit 2
}
cd "$BITE_HOME/.." || exit 2               # -> web/

# ---- THE FAMILY'S SHARED-INPUT LOCK (t_ed66094c, t_03a646e2, t_f4f4681c, t_76b3356d) --
#
# This harness MUTATES shared files under `src/app/docs` in place (`layout.tsx`,
# `security/page.tsx`, `page.tsx`) and `run_guard` reads the WHOLE docs tree, so two
# runs that overlap in ONE worktree read each other's mutations and print FALSE verdict
# lines. Every /docs bite harness shares that input, so they ALL take ONE lock file per
# worktree, `jomform-biteproof-docs-bite`. A lock keyed on the SCRIPT (as each harness
# used to carry) serialises two runs of the same harness but leaves two DIFFERENT
# harnesses of the family overlapping: measured 3/3 rounds with prose + tool_claims in
# one worktree, tool_claims printed 6 false `***` lines against 4 for a solo run,
# including a FALSE `registered name: NEW guard wanted GREEN, got RED` on its own
# negative control (t_76b3356d). Keying on the shared INPUT is what a per-script key
# cannot do.
#
# The key is the WORKTREE (`git rev-parse --absolute-git-dir`), under a HOST-GLOBAL
# base (`/tmp`, never `$TMPDIR` — the run it must exclude usually belongs to another
# PROFILE), so two lanes' cards never queue behind each other. Held from before the
# first read until this process exits. A contended run WAITS and says so rather than
# failing; a wait-out is exit 75. The PARENT takes it on fd 9 and re-execs this script;
# the child inherits the open descriptor and PROVES it holds it (a second take must be
# refused, a fresh file in the same directory must lock FINE) before it prints anything.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and overlapping runs mutate the same docs files and print" >&2
    echo "FALSE verdicts (t_76b3356d, reproduced). Install flock before trusting any" >&2
    echo "verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised run prints false verdicts when a peer runs here." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR, so two runs in" >&2
    echo "one worktree could not be serialised. Set BITEPROOF_LOCK_DIR to a writable dir." >&2
    exit 2
  }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || {
    echo "BITEPROOF REFUSED: cannot open the lock file $LOCK_FILE." >&2
    exit 2
  }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   another /docs bite harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (the family shares its input, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer that mutates the same docs files" >&2
    echo "and would print false verdicts. Nothing was mutated. Re-run once the other run" >&2
    echo "finishes, or raise the wait with BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held" >&2
    echo "and this run would not be serialised against a peer in the same worktree." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi

# The child. Refuse if the lock it is supposed to have inherited cannot be shown to be
# held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same docs files would give it false verdicts." >&2
  echo "Run the harness WITHOUT BITEPROOF_LOCKED set and let it take the lock itself." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here — it would read REFUSED for any file." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

# CAPABILITY, not presence (card t_b014807f): `node --version` succeeding proves nothing
# here — v18.19.1 answers it and then rejects `--disable-warning`, and every leg below
# would read that empty capture as a verdict. The gate runs the invocation this harness
# depends on and refuses, naming the node, when that produces no test-runner summary.
nt_require_node

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
BASE_GUARD=$(mktemp)
trap 'rm -f "$OLD_GUARD" "$BASE_GUARD" "$BASE_GUARD.probe"; rm -rf "$BAK"' EXIT

# The sabotaged files, as label:path pairs. `layout` and `security` are mutated by
# cases 1-3 and 5; `index` by cases 4 and 6a.
FILES=(layout src/app/docs/layout.tsx
       security src/app/docs/security/page.tsx
       index src/app/docs/page.tsx)

fail=0

# The "before" guard: the reader this rule replaced, re-based onto this tree. Fail closed,
# with the reason, when the premise does not hold — a verdict produced without a real
# "before" is worse than no verdict, because it reads as proof.
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
git show "${BASE_REV}:${GUARD_IN_REPO}" > "$BASE_GUARD"

# THE "BEFORE" COLUMN IS THE PRE-FIX READER, RE-BASED ONTO THIS TREE (card t_b5690577).
#
# Every case below drives ONE rule: an anchor declared only inside text a reader never
# sees (a `//` line, a `{/* … */}` container, a module-scope block comment) must not
# satisfy declaresId(). So the "before" column needs exactly one property — the helper's
# source line as it stood BEFORE the strip:
#
#     const src = read(file);
#
# and nothing else about it has to be historical. Two MEASURED facts forced that shape:
#
#   * once the fix merged, `origin/main` IS the fixed guard, so a base carrying the hole
#     can only come from this guard's HISTORY;
#   * a pre-fix revision is a FOSSIL. The rest of the guard moved on (its ORIGINAL_TITLES,
#     its parity and line-length rules), so the old file reds TODAY's pages on an
#     UNMUTATED tree. Measured here 2026-10-29 at origin/main 662d5b69: the revision this
#     harness used to suggest (7d0489b3) ran `ℹ pass 6 / ℹ fail 3` on the unmutated tree
#     ("every feature entry is one short line", "no feature was lost in the
#     reorganisation"), the premise check refused, and the harness had NO verdict at all.
#
# So the pre-fix SPELLING is read out of the base revision's guard and substituted into
# THIS tree's guard, in the `.oldcheck.ts` copy only. The substitution is count-checked in
# both directions and the copy must differ from this tree's guard in exactly one line, so
# the two columns cannot silently collapse into the same reader. What the base column then
# proves is "the pre-fix reader accepts this phantom"; it does NOT claim that the
# historical guard as a whole still runs today's pages, and it does not print as if it did.
PRE_ANCHOR_RE='^[[:space:]]*const[[:space:]]+src[[:space:]]*=[[:space:]]*read\(file\);[[:space:]]*$'
POST_ANCHOR_RE='^[[:space:]]*const[[:space:]]+src[[:space:]]*=[[:space:]]*stripUnrendered\(read\(file\)\);[[:space:]]*$'

# anchor_reader_line <file> <re> -> the ONE line matching <re>; fails on zero or 2+.
anchor_reader_line() {
  local n
  [ -f "$1" ] || return 1
  n=$(grep -cE "$2" "$1" 2>/dev/null) || n=0
  [ "$n" = 1 ] || return 1
  grep -E "$2" "$1" | head -1
}

# newest_pre_fix_rev — the newest revision of this guard whose declaresId() still reads
# the raw page. Derived from the file's own history, so it cannot be a hand-typed
# revision that has since caught up (the rule biteproof_docs_index_word_budget.sh applies
# to the same problem).
newest_pre_fix_rev() {
  local rev
  while read -r rev; do
    [ -n "$rev" ] || continue
    git show "${rev}:${GUARD_IN_REPO}" 2>/dev/null > "$BASE_GUARD.probe" || continue
    if anchor_reader_line "$BASE_GUARD.probe" "$PRE_ANCHOR_RE" >/dev/null; then
      rm -f "$BASE_GUARD.probe"
      printf '%s' "$rev"
      return 0
    fi
  done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  rm -f "$BASE_GUARD.probe"
  return 1
}

if PRE_LINE=$(anchor_reader_line "$BASE_GUARD" "$PRE_ANCHOR_RE"); then
  PRE_FROM="${BASE_REF} ${BASE_REV:0:12}"
else
  if suggested=$(newest_pre_fix_rev); then
    PRE_REV=$suggested
    git show "${PRE_REV}:${GUARD_IN_REPO}" > "$BASE_GUARD"
    PRE_LINE=$(anchor_reader_line "$BASE_GUARD" "$PRE_ANCHOR_RE") || {
      echo "BITEPROOF REFUSED: ${PRE_REV} was selected from history as carrying the pre-fix" >&2
      echo "anchor reader, but no single line of it matches \`const src = read(file);\`." >&2
      exit 2
    }
    PRE_FROM="history ${PRE_REV:0:12} (${BASE_REF} carries the fix)"
  else
    echo >&2
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): no revision in the history of" >&2
    echo "${GUARD_IN_REPO} declares the pre-fix anchor reader" >&2
    echo "    const src = read(file);" >&2
    echo "so there is no \"before\" spelling to re-base onto this tree, and every case below" >&2
    echo "would be the current reader measured against itself. Point BITEPROOF_BASE at a" >&2
    echo "revision that still carries the raw helper if you know one." >&2
    rm -f "$BASE_GUARD"
    exit 2
  fi
fi

POST_LINE=$(anchor_reader_line "$GUARD" "$POST_ANCHOR_RE") || {
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): this tree's guard (${GUARD}) does not declare" >&2
  echo "the comment-stripping anchor reader" >&2
  echo "    const src = stripUnrendered(read(file));" >&2
  echo "exactly once, so there is no fix here to demonstrate — the case set would be the" >&2
  echo "pre-fix reader measured against itself." >&2
  rm -f "$BASE_GUARD"
  exit 2
}

awk -v old="$POST_LINE" -v new="$PRE_LINE" \
  '$0 == old && !seen { print new; seen = 1; next } { print }' "$GUARD" > "$OLD_GUARD"
CHANGED_LINES=$(diff "$GUARD" "$OLD_GUARD" | grep -cE '^[<>]') || true
if [ "${CHANGED_LINES:-0}" != 2 ] ||
   ! anchor_reader_line "$OLD_GUARD" "$PRE_ANCHOR_RE" >/dev/null ||
   anchor_reader_line "$OLD_GUARD" "$POST_ANCHOR_RE" >/dev/null; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the re-based \"before\" guard is not this" >&2
  echo "tree's guard with EXACTLY the one reader line changed (measured: ${CHANGED_LINES:-?}" >&2
  echo "changed lines), so the two columns would not differ by the rule this case set drives." >&2
  rm -f "$BASE_GUARD"
  exit 2
fi
echo "before: ${PRE_FROM} — this tree's guard with the anchor reader reverted to"
echo "        \`const src = read(file);\` (one line, \`const src = stripUnrendered(read(file));\` replaced)"
rm -f "$BASE_GUARD"

for ((i = 0; i < ${#FILES[@]}; i += 2)); do
  label=${FILES[i]}; path=${FILES[i + 1]}
  cp "$path" "$BAK/$label.bak"
  eval "SHA_$label=$(sha256sum "$path" | cut -d' ' -f1)"
done
sha_of() { sha256sum "$1" | cut -d' ' -f1; }

echo "before column: this tree's guard with the anchor reader as it stood at ${PRE_FROM}"
echo "               ($(wc -l < "$OLD_GUARD") lines, one line changed)"
echo "this tree:     ${GUARD} ($(wc -l < "$GUARD") lines)"

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
#   * THE VERDICT IS NOT "NO FAILURE LINE MEANS GREEN" (card t_b014807f). This reader
#     used to print GREEN whenever the capture held no `ℹ fail [1-9]` line, so a node
#     that could not run the file at all (v18.19.1 rejects
#     `--disable-warning=…` with a 64-byte "bad option" capture) answered GREEN on
#     every leg: the harness printed a verdict it never measured. nt_capture truncates
#     the capture first and reads GREEN only off `ℹ pass` + `ℹ fail 0` + a `✔ ` line,
#     REFUSED otherwise, and the callers below turn REFUSED into exit 2.
OUT_FILE=$(mktemp)
trap 'rm -f "$OLD_GUARD" "$BASE_GUARD" "$BASE_GUARD.probe" "$OUT_FILE" "$OUT_FILE.rc"; rm -rf "$BAK"' EXIT
run_guard() {
  nt_capture "$OUT_FILE" "$1"
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
nt_refuse_if "BASELINE, the base guard on the UNMUTATED tree" "$OUT_FILE"
if [ "$premise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the re-based \"before\" guard is ${premise} on the UNMUTATED tree, so" >&2
  echo "there is no \"before\" column and this proof would be about nothing. That is a broken" >&2
  echo "premise, not a finding about this branch:" >&2
  grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2 >&2
  grep -E '^✖' "$OUT_FILE" | head -2 >&2
  exit 2
fi

# check <label> <base-want> <new-want> [must-name]
#
# `must-name` is the whole point of the new column: a RED that does not name the phantom
# would be attributable to a sibling. The attribution rule is PER FAILING TEST, not a
# pinned count (card t_b5690577): node's own "✖ failing tests:" section is split into test
# blocks and EVERY block must carry the phantom's text. That is stricter than a count in
# the direction that matters — an unrelated test that starts failing (a parity or
# line-length rule) names something else and reds the case — while allowing the SECOND
# test the same phantom legitimately trips. It had to allow that: `docsIndex.test.ts:1134`
# ("the categories in the docs sidebar match the categories on the page") now reports a
# sidebar row that sends the reader nowhere, so a phantom sidebar anchor reds BOTH it and
# the older "a broken sidebar row is reported under the group it is really declared in"
# pin (`:1254`, whose per-row sweep counts every broken row and so sees the phantom too).
# Measured on this host 2026-10-29: the sidebar cases read `ℹ pass 9 / ℹ fail 2` with both
# messages naming the phantom, while the entry-link case reads `pass 10 / fail 1`. A pinned
# `1` therefore reported "not attributable" for four cases whose reds were fully
# attributable — the harness could not print a clean verdict on this tree at all.
# FAILING_BLOCKS_OF <capture> -> one line per failing test, its block flattened.
failing_blocks() {
  awk '
    /^✖ failing tests:/ { section = 1; next }
    !section { next }
    /^test at / {
      if (blk ~ /[^ ]/) print blk
      blk = ""
      next
    }
    { blk = blk " " $0 }
    END { if (blk ~ /[^ ]/) print blk }
  ' "$1"
}
check() {
  local label="$1" base_want="$2" new_want="$3" needle="${4:-}" got out
  got=$(run_base_guard); out=$(cat "$OUT_FILE")
  # A refusal must not leave the case's mutation on disk: restore first, then refuse.
  if [ "$got" = REFUSED ]; then restore_all; nt_refuse_if "${label}: BASE guard" "$OUT_FILE"; fi
  if [ "$got" != "$base_want" ]; then
    echo "  *** ${label}: BASE guard wanted ${base_want}, got ${got} — the case does not reproduce the hole ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  else
    echo "  ok   ${label}: base guard ${got} — the hole reproduces"
  fi

  got=$(run_guard "$GUARD"); out=$(cat "$OUT_FILE")
  # A refusal must not leave the case's mutation on disk: restore first, then refuse.
  if [ "$got" = REFUSED ]; then restore_all; nt_refuse_if "${label}: NEW guard" "$OUT_FILE"; fi
  # The count comes from node's own summary (`ℹ fail N`), not from counting `✖`
  # lines: node prints each failure TWICE — once in the run, once under
  # "✖ failing tests:" — so a grep over `✖` reports 2 or 3 for a single failure and
  # the attribution check below would always fail.
  local nfail; nfail=$(sed -nE 's/^ℹ fail ([0-9]+)$/\1/p' <<<"$out" | head -1)
  : "${nfail:=0}"
  local unattributed=""
  if [ "$new_want" = "RED" ] && [ -n "$needle" ]; then
    unattributed=$(failing_blocks "$OUT_FILE" | grep -vF "$needle")
  fi
  if [ "$got" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${got} ***"
    grep -E '^ℹ (pass|fail)' <<<"$out" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out"; then
    echo "  *** ${label}: NEW guard ${got} but the failure does not name ${needle} ***"
    grep -E '^✖' <<<"$out" | head -3; fail=1
  elif [ "$new_want" = "RED" ] && [ -z "$needle" ]; then
    echo "  *** ${label}: a RED leg must name the phantom it drives; this one declares no needle ***"
    fail=1
  elif [ "$new_want" = "RED" ] && [ -n "$unattributed" ]; then
    echo "  *** ${label}: NEW guard RED with ${nfail} failing test(s), and not every one names the phantom — the red is not attributable to this mutation ***"
    printf '%s\n' "$unattributed" | head -3 | sed 's/^ */       /'; fail=1
  elif [ "$new_want" = "RED" ] && [ "$nfail" = 0 ]; then
    echo "  *** ${label}: NEW guard RED with no failing test in the summary — the guard could not be read ***"
    fail=1
  else
    local suffix=""
    [ "$new_want" = "RED" ] && suffix=", every one naming the phantom"
    echo "  BITES ${label}: new guard ${got}, ${nfail} failing test(s)${suffix}"
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
  echo "BITEPROOF PASS — every phantom was GREEN under the before column (the pre-fix anchor"
  echo "reader, re-based onto this tree) and RED, naming the anchor, under this one."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
