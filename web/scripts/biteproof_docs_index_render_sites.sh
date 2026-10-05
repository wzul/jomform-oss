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
# A CAUGHT-UP BASE IS REFUSED, NOT REPORTED AS `fail=1` (card `t_858620fc`). The
# premise check below only refuses a base that does not CARRY the guard at all
# (`git cat-file -e`). It does not cover the base whose reader has CAUGHT UP to this
# branch: once the fix MERGES into `origin/main`, the base guard is still there and
# still GREEN on the unmutated tree, so the premise holds — and then every HOLE case
# reads `does not reproduce the premise`, seven `***` lines and a silent
# `BITEPROOF FAIL (fail=1)`, a reading indistinguishable from "the base is genuinely
# pre-fix and the mutations failed to apply". What identifies a caught-up base is the
# reader's CAPABILITY (it DECLARES `stripUnrendered(read(DOCS_PAGE))`), never a byte
# distance — the guard can be rewritten around the same reader. When it is caught up
# this script REFUSES (exit 2) and prints a `BITEPROOF_BASE=<rev>` resolved from git
# and offered only when that revision's guard really is pre-fix. The refusal is
# itself driven end to end on every run (see the self-check), so the instrument
# cannot silently go back to printing a false FAIL the next time main catches up.
#
# THE SHARED INPUT IS SERIALISED PER WORKTREE (card `t_03a646e2`). Every case
# mutates the SHARED page (`src/app/docs/page.tsx`) in place and `run_guard` reads
# it the whole time, so two overlapping runs in ONE worktree mutate the page under
# each other and print FALSE verdict lines — reproduced by QA on the tag tree
# f63fa325: two runs 1s apart printed 7 and 4 `***` lines (a false
# `baseline: NEW guard wanted GREEN, got RED` included) against 0 for a solo run,
# with rc=1 for both and no false PASS reachable. No per-run channel can fix that
# (the harness already uses per-run `.ts` copies): the shared thing is the INPUT.
# The lock is the sibling's (web/scripts/biteproof_docs_tool_claims.sh, cards
# t_ed66094c / t_ff3640e7), taken before the premise check, which READS the page.
# It is keyed on the worktree, so two lanes' cards never queue behind each other,
# and a contended run WAITS and says so; a wait-out is exit 75, not a red harness.
#
# Run:  bash web/scripts/biteproof_docs_index_render_sites.sh
set -uo pipefail

# Absolute path to this script, resolved BEFORE the `cd` below: the caught-up-base
# self-check invokes it recursively (BITEPROOF_REFUSE_ONLY), and the LOCK BLOCK below
# re-execs it, so the path has to survive a changed CWD. Fail closed when the
# directory cannot be resolved — an empty `$(cd … && pwd)` yields `/biteproof_…sh`,
# and the re-exec then dies with `No such file or directory` instead of taking the
# lock, which reads as "the lock is not held here" and refuses for the wrong reason.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (card t_03a646e2) ----------------------------------
#
# WHY (the bug this was written for): every case below mutates the SHARED page
# `src/app/docs/page.tsx` in place (`render_sites_off` / `comment_out_entry` /
# `insert_at_line` all write $PAGE) and `run_guard` reads the page the whole time,
# but this harness took NO lock, while its sibling
# web/scripts/biteproof_docs_tool_claims.sh carried one for exactly this shape
# (cards t_ed66094c / t_ff3640e7). Reproduced on the tag tree f63fa325 by QA
# (`t_3b5979be`, logs conc_a/conc_b beside its repro): two runs 1s apart in ONE
# worktree printed **7 and 4** false `***` verdict lines — including a false
# `baseline: NEW guard wanted GREEN, got RED` on the BASELINE, i.e. the suite
# reading as broken — against **0** for a solo run at the same base, and rc=1 for
# both (9 and 11 BITES instead of 13). No false PASS is reachable, but a harness
# whose whole product is a trustworthy verdict cannot print wrong verdict lines,
# and no per-run channel can fix it: the shared thing is the INPUT.
#
# The lock is taken before the premise check below, which READS the page, and is
# held until this process exits. It is keyed on the WORKTREE, so two lanes' cards
# never queue behind each other, and a contended run WAITS and says so rather than
# failing — a refusal would turn "someone else is running it" into a red harness.
#
# The key is `git rev-parse --absolute-git-dir`: one value per worktree, the same
# value from every subdirectory of it, and DIFFERENT for the primary tree and for
# each linked worktree (a linked worktree resolves to `<repo>/.git/worktrees/<name>`,
# the primary tree to `<repo>/.git`). `--show-toplevel` would name the same worktree
# too, but it is resolved against the CWD, so a run started from `web/` would derive
# a different key for the same tree and the lock would not be mutual.
#
# The lock file sits under a HOST-GLOBAL base, deliberately not $TMPDIR: the run it
# has to exclude usually belongs to another PROFILE (a QA re-run while the dev lane
# runs, which is how this defect was found), and a per-profile TMPDIR would put the
# two runs in different directories and serialise nothing.
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits the
# open descriptor, so the lock is held for exactly as long as the copy that mutates
# the page lives, and it is released when the parent exits — the `trap ... EXIT`
# below releases it as surely as it restores the page, and there is no separate
# unlock line that can be skipped.
#
# The child PROVES the lock is held before it prints a verdict (a lock nobody can
# observe as held is the same "trust the absence" shape the verdict rule was
# rewritten to remove). Two legs, both needed: a second take on the same file from a
# separate process must be REFUSED, and a fresh file in the same directory must lock
# FINE — or "refused" would mean flock is inert here, not that this run holds the lock.
#
# The directory is the FAMILY'S shared one (`jomform-biteproof-docs-bite`), the same
# file every /docs bite harness takes (card `t_76b3356d`). It used to be per HARNESS
# (its own `jomform-biteproof-docs-index-render-sites`), which serialised two runs of
# THIS script but left two DIFFERENT harnesses of this family overlapping — and they
# all mutate the SAME tree. Measured 3/3 rounds with `biteproof_docs_prose_convention.sh`
# and `biteproof_docs_tool_claims.sh` started together in ONE worktree: tool_claims
# printed 6 false `***` lines against 4 for a solo run, including a false
# `registered name: NEW guard wanted GREEN, got RED` on its own negative control.
# `web/src/lib/harnessLock.test.ts` leg E drives the cross-harness property, so a
# revert to a per-script directory turns it RED. Two lanes' cards still never
# serialise behind each other: the key is the WORKTREE.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate the same docs page and print FALSE" >&2
    echo "verdicts (t_03a646e2, reproduced). Install flock before trusting any verdict" >&2
    echo "from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised run prints false verdicts when a peer runs here (t_03a646e2)." >&2
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
    echo "so this run is NOT serialised against a peer that mutates the same docs page" >&2
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
  echo "NOT serialised — a peer mutating the same docs page would give it false verdicts." >&2
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

PAGE=src/app/docs/page.tsx
GUARD=src/lib/docsIndex.test.ts
# READING THE READER, and why it is not a byte comparison (cards `t_858620fc` and
# `t_01df90af`): once the fix MERGES into `origin/main` the base guard stays a few
# bytes off this tree's while READING THE SAME stripped text, so a byte test would
# compare a reader with itself (the sibling's byte fast path was itself unsatisfiable
# — card `t_dba4465c`). What is read instead is the base guard's TEXT, for a
# DECLARATION of the module-level reader. These are the two shapes either side of the
# fix (`7b90dd7c` declares the first, `4952045b` the second, measured over every
# revision of this guard).
#
# Anchored at the line START, not a bare substring: a match anywhere in the file would
# be satisfied by the same text quoted in a comment or a string while the real reader
# is something else (the defect t_7b24b035 fixed one layer down, in the guard itself).
# The pattern still tolerates what a reformat may change — leading whitespace, an
# `export`, `const`/`let`/`var` — because the point is the CAPABILITY, not the bytes.
# A reader spelled as a longer expression matches NEITHER declaration, which is
# deliberate: `reader_class` then answers UNKNOWN and the run refuses rather than
# assuming "pre-fix" for a shape it cannot read.
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
# The spellings the operator is shown, and the ones the diagnostics name.
PRE_READER='const docsSrc = read(DOCS_PAGE)'
POST_READER='const docsSrc = stripUnrendered(read(DOCS_PAGE))'
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
  rm -f "$GUARD.selfcheck_unknown.$$.tmp" "$GUARD.selfcheck_unknown.$$.idx"
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

# ── WHAT THE BASE'S READER IS: PRE-FIX, POST-FIX, OR UNREADABLE ──────────────
# Reads the base guard's TEXT for a DECLARATION of the reader, never its byte
# distance from this tree's file. A byte test cannot decide this: once the fix
# merges into `origin/main` the guard can be rewritten around the SAME reader, so
# the two files differ by bytes while the readers do not differ at all — and that
# is the state this refuses (the sibling's byte fast path was itself unsatisfiable,
# card `t_dba4465c`).
#
# reader_class <rev> -> PRE | POST | UNKNOWN on stdout, 1 when the guard cannot be
#   read at all. POST means the base declares the post-fix reader (caught up).
#   UNKNOWN means NEITHER declaration is present: a `docsSrc` spelled as a longer
#   expression, a helper, a rewrite this read does not recognise. It is NOT
#   "pre-fix" — nothing here showed the reader is the raw one — and assuming it were
#   is how a caught-up base gets measured as if it were the "before" column
#   (card `t_01df90af`: a post-fix reader carrying a stale `const docsSrc =
#   read(DOCS_PAGE)` in a comment used to come back "pre-fix").
reader_class() { # <rev> -> PRE | POST | UNKNOWN
  local body; body=$(git show "$1:$GUARD_IN_REPO" 2>/dev/null) || return 1
  grep -qE "$POST_READER_RE" <<<"$(strip_ts_comments <<<"$body")" && { printf 'POST'; return 0; }
  grep -qE "$PRE_READER_RE"  <<<"$(strip_ts_comments <<<"$body")" && { printf 'PRE';  return 0; }
  printf 'UNKNOWN'
}
# DEMONSTRABLY pre-fix: the base declares the pre-fix reader, and does not declare
# the post-fix one. Anything else — POST, UNKNOWN, unreadable — is refused by
# refuse_caught_up_base, which the caller reaches through this predicate.
# Fails closed on "cannot tell" rather than treating it as pre-fix.
base_reader_is_pre() { # <rev> -> 0 only when its guard DECLARES the pre-fix reader
  [ "$(reader_class "$1")" = PRE ]
}

# introducing_commit <rev> -> the commit that first DECLARES the post-fix reader in
# <rev>'s history for the guard, or empty. Walks oldest-first so a later commit that
# merely touches the file cannot move the answer; returns empty when the transition
# is older than <rev>'s history (the oldest revision already declares it), rather
# than naming a revision it has not checked. (Same rule the sibling
# api/scripts/biteproof_model_default_guard.py uses, card `t_34ab7a00`.)
#
# It reads with the SAME declaration pattern `reader_class` uses (card `t_01df90af`):
# if the two disagreed, the refusal could name a revision it then classifies as
# UNKNOWN and refuses — the loop the sibling card removed by deriving the named
# revision from the commit that introduced the reader rather than guessing.
introducing_commit() { # <rev> -> sha, or empty
  local revs oldest r body
  # `:/<path>` is the repo-root-relative pathspec form: this script has already `cd`ed
  # into web/, and a bare repo-root path here matches NOTHING (measured: 0 revisions,
  # so the attribution returned empty and the refusal could not name a base).
  revs=$(git rev-list --reverse "$1" -- ":/$GUARD_IN_REPO" 2>/dev/null) || return 1
  [ -z "$revs" ] && return 1
  oldest=${revs%%$'\n'*}
  body=$(git show "$oldest:$GUARD_IN_REPO" 2>/dev/null) || return 1
  grep -qE "$POST_READER_RE" <<<"$(strip_ts_comments <<<"$body")" && return 1   # older than this history
  while IFS= read -r r; do
    [ -z "$r" ] && continue
    body=$(git show "$r:$GUARD_IN_REPO" 2>/dev/null) || continue
    if grep -qE "$POST_READER_RE" <<<"$(strip_ts_comments <<<"$body")"; then printf '%s' "$r"; return 0; fi
  done <<<"$revs"
  return 1
}

# The refusal itself. It names the CAUSE it found — a caught-up reader (POST) or a
# reader this read cannot place (UNKNOWN) — prints a BITEPROOF_BASE suggestion
# RESOLVED from git (never a literal), and only offers that revision when its guard is
# re-read and shown to DECLARE the pre-fix reader — a suggestion that refuses too is
# the loop this whole family of cards is about. No verdict and no bite count is
# printed for a base this run did not measure.
refuse_caught_up_base() {
  local klass intro='' parent=''
  klass=$(reader_class "$BASE_REV") || klass=''
  echo >&2
  echo "BITEPROOF REFUSED (VERDICT=REFUSED, exit 2): the base guard (${BASE_REF} ${BASE_REV:0:12})" >&2
  if [ "$klass" = POST ]; then
    echo "has CAUGHT UP to this tree's — ${GUARD_IN_REPO} there already declares" >&2
    echo "  ${POST_READER}" >&2
    echo "which is this tree's reader: both legs would be one reader measured against itself." >&2
  else
    echo "declares NEITHER reader this harness knows, so this run cannot show it is PRE-fix:" >&2
    echo "  pre-fix:  ${PRE_READER}" >&2
    echo "  post-fix: ${POST_READER}" >&2
    echo "A \`docsSrc\` spelled as a longer expression (a \`.replace()\`, a helper) may be either," >&2
    echo "and ASSUMING pre-fix is exactly how a caught-up base reads as the \"before\" column." >&2
  fi
  echo "There is no \"before\" column to prove anything against. This is the silent shape this" >&2
  echo "refusal exists for: the premise check would PASS (that guard is GREEN on the unmutated" >&2
  echo "tree) and every HOLE case would read \"does not reproduce the premise\" — a fail=1 reading" >&2
  echo "indistinguishable from a base that is genuinely pre-fix with mutations that stopped" >&2
  echo "applying." >&2
  intro=$(introducing_commit "$BASE_REV") || intro=''
  if [ -n "$intro" ]; then
    parent=$(git rev-parse "${intro}^" 2>/dev/null) || parent=''
    if [ -n "$parent" ] && base_reader_is_pre "$parent"; then
      echo >&2
      echo "The post-fix reader was introduced by ${intro:0:12}, whose parent ${parent:0:12} still" >&2
      echo "reads \`${PRE_READER}\` — re-read from git by this run, not assumed. Point" >&2
      echo "BITEPROOF_BASE at it:" >&2
      echo "    BITEPROOF_BASE=${parent:0:12} bash web/scripts/biteproof_docs_index_render_sites.sh" >&2
    else
      echo >&2
      echo "The post-fix reader was introduced by ${intro:0:12}, but its parent (${parent:0:12})" >&2
      echo "does not read as pre-fix either, so this harness cannot name a usable base from" >&2
      echo "${BASE_REV:0:12}'s history. Point BITEPROOF_BASE at a revision whose guard reads" >&2
      echo "\`${PRE_READER}\`." >&2
    fi
  else
    echo >&2
    echo "No revision in ${BASE_REV:0:12}'s history for ${GUARD_IN_REPO} reaches the transition" >&2
    echo "into the post-fix reader (the oldest one already declares it), so this run cannot" >&2
    echo "attribute it to one commit and will not guess. Point BITEPROOF_BASE at a revision" >&2
    echo "whose guard reads \`${PRE_READER}\`." >&2
  fi
  exit 2
}

# SELF-CHECK OF THE REFUSAL, ON EVERY RUN — the check-the-check pattern
# BITE-HARNESSES.md records for biteproof_unlock_class_guard.py. The refusal above is
# only worth having if it fires on a caught-up base, fires on a base whose reader this
# read cannot place, AND stays quiet on a pre-fix one. ALL THREE legs run the REAL
# script (BITEPROOF_REFUSE_ONLY=1, which resolves the base, applies the capability
# test and exits 3 without a refusal) so a refusal that refuses everything is caught
# here instead of looking like a fix.
selfcheck_caught_up_refusal() {
  local ref rev intro parent out rc1 rc2 rc3 unknowntree
  for ref in origin/main HEAD; do
    rev=$(git rev-parse "${ref}^{commit}" 2>/dev/null) || continue
    intro=$(introducing_commit "$rev") || intro=''
    [ -z "$intro" ] && continue
    parent=$(git rev-parse "${intro}^" 2>/dev/null) || parent=''
    base_reader_is_pre "$parent" && break
    intro=''
  done
  if [ -z "$intro" ]; then
    echo >&2
    echo "BITEPROOF REFUSED: the caught-up-base refusal cannot be self-checked — no post-fix" >&2
    echo "reader could be attributed from origin/main or HEAD, so this run has no way to produce" >&2
    echo "a caught-up base and no way to show the refusal still bites. A refusal nobody has seen" >&2
    echo "fire is the defect this instrument exists to remove. Refusing (exit 2)." >&2
    exit 2
  fi
  # leg 3's fixture: a guard whose reader is spelled so NEITHER declaration matches —
  # the "cannot tell" shape (card t_01df90af). Built as a real COMMIT in the object DB
  # (head's tree with the guard blob swapped): `BITEPROOF_BASE` must resolve to a
  # commit, so a bare blob would make the child die on "cannot resolve" and the leg
  # would prove nothing. GIT_INDEX_FILE is a temp file, so this worktree's real index
  # is never touched; the objects are unreferenced and gc prunes them. The text is
  # transformed in a temp file the fixture removes.
  #
  # The index path must be ABSOLUTE: git resolves a relative GIT_INDEX_FILE against
  # the repo TOPLEVEL, not the cwd (measured: `src/lib/x.idx` from web/ tried
  # `<worktree>/src/lib/…`, which does not exist — read-tree died 128 and this
  # self-check exited 2 saying nothing).
  local rewriter="$PWD/$GUARD.selfcheck_unknown.$$.tmp" idx="$PWD/$GUARD.selfcheck_unknown.$$.idx" tree=''
  awk -v anchor="$POST_READER" '
    index($0, anchor) {
      indent = substr($0, 1, index($0, anchor) - 1)
      printf "%s// pre-fix spelling: %s\n", indent, anchor
      printf "%sconst docsSrc = stripUnrendered(read(DOCS_PAGE).replace(/\\s+$/, \"\"));\n", indent
      seen = 1
      next
    }
    { print }
    END { if (!seen) exit 1 }
  ' "$GUARD" > "$rewriter" || { rm -f "$rewriter"; exit 2; }
  tree="$(git hash-object -w --stdin < "$rewriter")" || { rm -f "$rewriter"; exit 2; }
  rm -f "$rewriter"
  rm -f "$idx"
  GIT_INDEX_FILE="$idx" git read-tree HEAD 2>/dev/null || { rm -f "$idx"; exit 2; }
  # The path must be the REPO-ROOT-relative one (`$GUARD_IN_REPO`), because the index
  # is the repo's: `$GUARD` is relative to web/ and git rejects it (measured: "fatal:
  # path 'web/src/lib/docsIndex.test.ts' exists, but not 'src/lib/docsIndex.test.ts'").
  GIT_INDEX_FILE="$idx" git update-index --cacheinfo "100644,$tree,$GUARD_IN_REPO" 2>/dev/null \
    || { rm -f "$idx"; exit 2; }
  tree=$(GIT_INDEX_FILE="$idx" git write-tree) || { rm -f "$idx"; exit 2; }
  rm -f "$idx"
  unknowntree=$(git commit-tree "$tree" -p HEAD -m \
    "biteproof refusal self-check fixture (a reader this read cannot place)") \
    || { echo "BITEPROOF REFUSED: could not build the unplaceable-reader fixture" >&2; exit 2; }
  echo "refusal self-check: post-fix fixture ${intro:0:12}, pre-fix fixture ${parent:0:12}"
  out=$(BITEPROOF_REFUSE_ONLY=1 BITEPROOF_BASE="$intro" bash "$SELF" 2>&1); rc1=$?
  if [ "$rc1" != 2 ] || ! grep -qF 'BITEPROOF REFUSED (VERDICT=REFUSED' <<<"$out"; then
    echo >&2
    echo "BITEPROOF REFUSED: the caught-up-base refusal DID NOT BITE — a base whose guard is the" >&2
    echo "post-fix reader (${intro:0:12}) came back exit ${rc1}, not 2 with the refusal. Every verdict" >&2
    echo "this run could print is worth nothing until that fires. Fix base_reader_is_pre first." >&2
    echo "$out" | tail -4 >&2
    exit 2
  fi
  grep -qF 'BITEPROOF_BASE=' <<<"$out" || {
    echo >&2
    echo "BITEPROOF REFUSED: the refusal fired but printed no BITEPROOF_BASE suggestion, so an" >&2
    echo "operator has no way past it. Fix refuse_caught_up_base first." >&2
    exit 2
  }
  out=$(BITEPROOF_REFUSE_ONLY=1 BITEPROOF_BASE="$parent" bash "$SELF" 2>&1); rc2=$?
  if [ "$rc2" != 3 ] || grep -qF 'BITEPROOF REFUSED' <<<"$out"; then
    echo >&2
    echo "BITEPROOF REFUSED: the caught-up-base refusal refuses a PRE-fix base too — the pre-fix" >&2
    echo "fixture ${parent:0:12} came back exit ${rc2}, not 3, so the test is not discriminating and" >&2
    echo "would block every honest run. Fix base_reader_is_pre first." >&2
    echo "$out" | tail -4 >&2
    exit 2
  fi
  # leg 3: the "cannot tell" shape must REFUSE (fail closed), not be assumed pre-fix.
  # Before card t_01df90af this fixture classified as PRE and the run fell through to
  # measure it — silently, in the exact failure mode the refusal exists to remove.
  out=$(BITEPROOF_REFUSE_ONLY=1 BITEPROOF_BASE="$unknowntree" bash "$SELF" 2>&1); rc3=$?
  # The refusal must fire AND name ITS cause — a child that refused a fixture the
  # capability test never saw (an unresolvable base, a missing guard) exits 2 with a
  # different message, and reading that as proof would be the same "green for the
  # wrong reason" this leg exists to catch.
  if [ "$rc3" != 2 ] || ! grep -qF 'declares NEITHER reader' <<<"$out"; then
    echo >&2
    echo "BITEPROOF REFUSED: the refusal does not cover a base whose reader it CANNOT PLACE — a" >&2
    echo "guard declaring NEITHER reader (fixture ${unknowntree:0:12}) came back exit ${rc3} without" >&2
    echo "the NEITHER-reader refusal. Assuming \"pre-fix\" for a shape this read does not recognise is" >&2
    echo "how a caught-up base reads as the \"before\" column — the failure this refusal exists to" >&2
    echo "remove. Fix reader_class first." >&2
    echo "$out" | tail -6 >&2
    exit 2
  fi
}
# Skipped in the probe child (BITEPROOF_REFUSE_ONLY=1): the child's job is only to
# apply the capability test and report its exit code, and letting it run this
# self-check would recurse without end (measured: the run hung on its first probe).
# selfcheck_block_comment_anchor — the D3 residual (card t_80a7fb64), asserted rather
# than described.
#
# The declaration read is line-anchored to keep a COMMENT from satisfying it. That works
# for a `//` line comment (its body carries the `// ` prefix, so the anchored pattern
# never sees a declaration there) but NOT for a block comment: a `/* ... */` body line
# sits at column 0 with no marker, so it is byte-identical in shape to a real
# declaration. Reading the RAW body classified a post-fix guard that carries the stale
# reader inside a block comment as PRE — the base is then measured as the "before" column
# although it already declares the fix, and the run reports a silent fail=1.
#
# This leg builds BOTH shapes in memory and requires the read to place them correctly:
# the block-comment body must NOT count as a declaration, and a real declaration beside a
# comment still must. It runs on every invocation, so the strip cannot rot back.
selfcheck_block_comment_anchor() {
  local raw_block raw_comment_only raw_real
  # A post-fix guard whose ONLY pre-fix-looking text is a column-0 block-comment body.
  raw_block='/*
const docsSrc = read(DOCS_PAGE)
*/
const docsSrc = stripUnrendered(read(DOCS_PAGE))'
  # A file whose ONLY declaration-looking text is inside the block: after the strip it
  # must declare NOTHING.
  raw_comment_only='/*
const docsSrc = read(DOCS_PAGE)
*/'
  # A real pre-fix declaration, beside no comment.
  raw_real='const docsSrc = read(DOCS_PAGE)'

  if grep -qE "$PRE_READER_RE" <<<"$(strip_ts_comments <<<"$raw_block")"; then
    echo >&2
    echo "BITEPROOF REFUSED: the declaration read still sees a PRE-fix reader inside a" >&2
    echo "column-0 BLOCK COMMENT, so a caught-up base carrying the stale spelling in a" >&2
    echo "\`/* ... */\` block is classified PRE and measured as the before column (card" >&2
    echo "t_80a7fb64, the D3 residual). Fix the read in reader_class / strip_ts_comments." >&2
    exit 2
  fi
  if ! grep -qE "$POST_READER_RE" <<<"$(strip_ts_comments <<<"$raw_block")"; then
    echo >&2
    echo "BITEPROOF REFUSED: stripping the block comment also removed the REAL post-fix" >&2
    echo "declaration beside it, so the read would answer UNKNOWN on a caught-up base —" >&2
    echo "over-stripping is the other end of the same defect. Fix strip_ts_comments." >&2
    exit 2
  fi
  if ! grep -qE "$PRE_READER_RE" <<<"$(strip_ts_comments <<<"$raw_real")"; then
    echo >&2
    echo "BITEPROOF REFUSED: a REAL pre-fix declaration is no longer read as one, so every" >&2
    echo "base fixture this harness builds would be unplaceable. Fix strip_ts_comments." >&2
    exit 2
  fi
  if grep -qE "$PRE_READER_RE|$POST_READER_RE" <<<"$(strip_ts_comments <<<"$raw_comment_only")"; then
    echo >&2
    echo "BITEPROOF REFUSED: a file whose only declaration is inside a block comment still" >&2
    echo "reads as declaring one, so the strip is leaking the block's body through." >&2
    exit 2
  fi
}

[ "${BITEPROOF_REFUSE_ONLY:-0}" = 1 ] || selfcheck_caught_up_refusal
[ "${BITEPROOF_REFUSE_ONLY:-0}" = 1 ] || selfcheck_block_comment_anchor

# CLASSIFY THIS RUN'S OWN BASE AND REFUSE IF IT IS NOT USABLE. Runs after the
# self-check, so the mechanism is proved before it is used. Anything that is not
# DEMONSTRABLY pre-fix refuses here (a caught-up reader, or a reader this read cannot
# place); only a declared pre-fix reader falls through to the premise check below.
base_reader_is_pre "$BASE_REV" || refuse_caught_up_base

# The probe child's exit: reached only with BITEPROOF_REFUSE_ONLY=1, and only when the
# classification above did NOT refuse — so the self-check can tell "refused" (exit 2)
# from "did not refuse" (exit 3) by exit code alone. It is only reached for a base
# whose reader is DEMONSTRABLY pre-fix.
if [ "${BITEPROOF_REFUSE_ONLY:-0}" = 1 ]; then
  echo "BITEPROOF CAPABILITY: ${BASE_REF} ${BASE_REV:0:12} is PRE-fix (not refused)"
  exit 3
fi

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
echo "== CASE 13 (HOLE): {f.title} replaced by a CONSTANT at BOTH heading sites (card t_1f175168) =="
# The vacuity test asserted only {f.body}/id={f.slug}/id={g.id}/<T k={g.labelKey}/ — never the
# entry TITLE or the guide LINK, so the whole suite stayed GREEN 10/0 while every feature
# heading on /docs rendered the literal word "Feature". `key={f.title}` is left alone on
# purpose: it is an ATTRIBUTE on the same element, and a test looking for a bare `f.title`
# would still pass while the heading rendered a constant. GREEN under the base guard, RED
# under this one is exactly the HOLE shape.
render_sites_off '>{f.title}</h4>' '>{"Feature"}</h4>' && {
  render_sites_off '>{f.title}</h3>' '>{"Feature"}</h3>' && {
    check "constant heading at both sites" GREEN RED '[render-site-entry-title]'; }
}
restore_page

echo
echo "== CASE 14 (HOLE): href={f.href} pointed at /docs (card t_1f175168) =="
# The link destination is an ATTRIBUTE slot, not a JSX text slot, so it needs its own
# pattern: `href={f.href}`. Removed, every "Full guide" link went to /docs itself and the
# suite was still green 10/0.
render_sites_off 'href={f.href}' 'href="/docs"' && {
  check "guide link pointed at /docs" GREEN RED '[render-site-guide-link]'; }
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
