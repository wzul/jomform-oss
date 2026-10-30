#!/usr/bin/env bash
# Bite proof for the aggregate word budget in web/src/lib/docsIndex.test.ts
# (card t_7f0dd2b7, filed by the verify lane on t_0d27a340).
#
# THE TRAP THIS PINS. The guard asserted `total < 800` — a FIXED page cap — and
# main arrived at 799 words over 33 entries: ONE word of headroom. The page's
# entry list is a CONTRACT (the parity test pins it in both directions) and every
# new feature is required to have an entry, so a fixed cap can only be paid for by
# trimming an existing summary — which is how #791 shipped /docs RED at 806 words
# and how the 9-word trim that bought PR 832's margin was spent again by PR 804.
# The fix derives the ceiling from the entries the page declares
# (`PAGE_BUDGET_PER_ENTRY * entries.length`, see the guard), so adding a feature
# moves the ceiling instead of eating the headroom down to a single word.
#
# WHY A SCRIPT AND NOT AN ASSERTION INSIDE THE GUARD. Every case needs a "before"
# column, and the before column is the guard as it stands on the branch's base: a
# test inside the guard can only exercise the guard's own rules, never a reader
# that lacks them. So each case: mutate -> assert the mutation is ON DISK -> run
# the BASE guard (the verdict the trap predicts) -> run this tree's guard (the
# verdict the fix predicts, and the offender it must NAME) -> restore
# byte-identically, re-checked by sha256.
#
# THE CASES, and what each one is worth:
#
#   * BASELINE — both guards GREEN on the unmutated tree, so no red below can be
#     this tree being broken.
#   * CASE 1, the card's own acceptance test — a NEW entry with a NORMAL summary
#     (22 words, plus its title in ORIGINAL_TITLES on both sides so the parity
#     check is not what fires): the BASE guard REDS ("the feature summaries total
#     821 words"), the tree is GREEN. That is the trap removed, driven rather
#     than argued.
#   * CASE 2, the hair trigger the card measured — the SMALLEST edit that trips the fixed
#     cap: the BASE reds at exactly the cap while the tree stays GREEN. Its SIZE is DERIVED
#     from the cap's real headroom on this page, and the cap itself is derived (page + 1, see
#     below), so the headroom is one word by construction — a hand-typed size is the same
#     staleness trap the base column fell into (card t_8715a2af). This is the shape that
#     would have blocked every lane's build.
#   * CASE 3, the direction that must NOT be lost — a page made FATTER by 30
#     words across two summaries with no new entry: RED in both columns, naming the
#     budget. A fix that only bought headroom would be GREEN here, so this is the
#     control that says the budget is still a budget.
#   * CASE 4, the DERIVED ceiling itself — the constant moved down one word per
#     entry, page untouched: the tree REDS naming the derived budget for the entry
#     count the page declares while the BASE is GREEN (the page's total sits under the
#     re-based fixed cap). A discriminating pair: the red can only come from the derived
#     budget, not from the old fixed cap. Both numbers in the needle are READ, not typed
#     (the constant out of this tree's guard, the entry count out of the guard's own
#     message), because a hand-typed "33 entries" was the same staleness trap.
#   * CASE 5, NEGATIVE CONTROL — a summary TRIMMED by ~10 words: GREEN in both
#     columns, so the check has not become "any edit reds".
#
# WHAT IT DOES NOT COVER, stated honestly: it does not re-derive the entry list
# (deleting an entry lowers the ceiling with it — the parity test above is what
# catches a lost feature, and this harness only measures the budget); it does not
# read the built page, so the sums are the declared summaries; and it does not
# prove the per-entry LIMIT (45 words) still bites — that is the sibling
# `biteproof_docs_index_render_sites.sh`'s subject.
#
# Run:  bash web/scripts/biteproof_docs_index_word_budget.sh
#       BITEPROOF_BASE=<rev> bash web/scripts/biteproof_docs_index_word_budget.sh
#
# Exit 0 = PASS, exit 2 = refused/no verdict, exit 1..n = that many failing checks.
#
# THE BASE COLUMN IS DERIVED FROM HISTORY AND RE-BASED ONTO TODAY'S PAGE (card
# t_9d8f1884). The "before" column has to be a guard that carries the FIXED cap, because
# that is the trap every case drives, and two things made that column impossible to PIN
# to one revision:
#   * once this fix merged, `origin/main` IS the fixed guard, so the base can only come
#     from HISTORY: the newest revision whose guard still ASSERTS `total < N,` and does
#     not declare the derived budget;
#   * and a pre-fix revision is a FOSSIL — its cap is a constant chosen when the page was
#     smaller, so once the page outgrows it the base column can never be GREEN on the
#     unmutated tree again and every case reports "the case does not reproduce the trap",
#     which is a statement about the PAGE, not about this branch. Measured 2026-10-29: the
#     page carried 873 words against the pinned 800 (a −73 deficit), so the harness
#     refused on BOTH its default base and the revision it suggested, and had no verdict
#     to print at all. `ORIGINAL_TITLES` goes stale the same way — it is part of the
#     guard, so a revision that predates today's entries reds the PARITY test on the
#     unmutated tree.
# So the base column is the pre-fix RULE, re-based onto today's page, and every re-base is
# DERIVED from the guards rather than typed:
#   * its CAP moves to `total < PAGE_TOTAL + 1` — the trap's own measured worst shape
#     ("main arrived at 799: ONE word of headroom") — in the `.oldcheck.ts` COPY only,
#     never in the guard and never in a git object;
#   * its `ORIGINAL_TITLES` receives the titles this tree's guard carries and the base's
#     does not, so the base reds for the BUDGET and never for page parity.
# Nothing else about the pre-fix guard is touched: its rule (ONE constant for the whole
# page, independent of how many entries exist) and the sentence it fails with are the
# historical ones, and that sentence is what the BASE legs assert on — derived, not typed.
# What the base column therefore proves is "the pre-fix rule reds on this mutation". It
# does NOT prove that the historical 800 would have red today's page, and it does not claim
# to: the cap it runs is stated in its own first line of output.
#
# SO THE DEFAULT RUN WORKS. `origin/main` is the FIXED guard now, so with no
# BITEPROOF_BASE the harness takes the newest revision from this guard's own history that
# still carries the pre-fix shape, announces it, and re-bases it. An EXPLICIT
# BITEPROOF_BASE that cannot hold the trap is still refused (exit 2) with the revision to
# point at, and the run refuses if either of two conditions fails:
#   CONDITION 1 OF 2 — no revision asserts a fixed cap without declaring the fix, so there
#                      is no "before" column to be had from this history at all;
#   CONDITION 2 OF 2 — the re-based base is not GREEN on the UNMUTATED tree, so a red case
#                      below could not be attributed to its mutation.
# Every number the cases assert on is read rather than typed: the page's total and the
# entry count out of the base guard's own failing message, the per-entry constant out of
# this tree's guard. That is the rule the whole harness exists for — a hand-typed 33
# entries, 800 words or 792-word budget goes stale exactly like the base column did.
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }

# The shared runner gate and verdict rule (web/scripts/lib/node_test_verdict.sh).
# Every verdict this harness prints comes from POSITIVE runner evidence: a capture
# carrying no test-runner output is a REFUSAL, never a GREEN (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no verdict rule to read a guard's output with." >&2
  exit 2
}
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2               # -> web/

# ---- the /docs bite family's SHARED-INPUT lock -------------------------------
# Every /docs bite harness mutates the same tree the /docs guard reads, so two runs
# that overlap in ONE worktree read each other's mutations and print FALSE verdicts.
# The family therefore takes ONE lock per WORKTREE (`jomform-biteproof-docs-bite`,
# keyed on `git rev-parse --absolute-git-dir`), under a HOST-GLOBAL base (`/tmp`, never
# `$TMPDIR` — the run it must exclude usually belongs to another PROFILE). Held from
# before the first read until this process exits. A contended run WAITS and says so; a
# wait-out is exit 75. The PARENT takes it on fd 9 and re-execs this script; the child
# inherits the descriptor and PROVES it holds it before it prints anything.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and overlapping runs mutate the same docs files and print" >&2
    echo "FALSE verdicts. Install flock before trusting any verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir, so there is no" >&2
    echo "per-worktree key to serialise on, and an unserialised run prints false verdicts" >&2
    echo "when a peer runs here." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR. Set" >&2
    echo "BITEPROOF_LOCK_DIR to a writable dir." >&2
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
    echo "so this run is NOT serialised against a peer that mutates the same docs files." >&2
    echo "Nothing was mutated. Re-run once the other run finishes." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi

LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same docs files would give it false verdicts." >&2
  exit 2
fi
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

# ---- what this harness drives, and the premise it refuses without ------------
GUARD=src/lib/docsIndex.test.ts
GUARD_IN_REPO="web/$GUARD"
PAGE=src/app/docs/page.tsx
PAGE_IN_REPO="web/$PAGE"
OLD_COPY="$GUARD.oldcheck.ts"

node --version >/dev/null 2>&1 || { echo "BITEPROOF REFUSED: node is not on PATH." >&2; exit 2; }
# CAPABILITY, not presence (card t_b014807f): v18 answers `node --version` and then
# rejects `--disable-warning=…`, and every leg below then reads its empty capture as
# GREEN. The gate runs the invocation this harness depends on and refuses, naming the
# node, when that produces no test-runner summary.
nt_require_node
command -v perl >/dev/null 2>&1 || {
  echo "BITEPROOF REFUSED: perl is not on PATH, and the page mutations below are perl" >&2
  echo "substitutions with counted anchors (a mutation that does not land is a silent green)." >&2
  exit 2
}
for f in "$GUARD" "$PAGE"; do
  [ -f "$f" ] || { echo "BITEPROOF REFUSED: $f not found — run this from the web/ tree." >&2; exit 2; }
done

TOP=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "not in a git worktree" >&2; exit 2; }
# The porcelain line for the two files this harness mutates, captured BEFORE the first
# mutation, so the last check can tell "no mutation survived" from "the tree was already
# dirty when the lane ran it".
STATUS_BEFORE="$(git -C "$TOP" status --porcelain -- "$GUARD_IN_REPO" "$PAGE_IN_REPO")"
BASE_REF="${BITEPROOF_BASE:-origin/main}"
BASE_REV=$(git rev-parse "${BASE_REF}^{commit}" 2>/dev/null) || {
  echo "cannot resolve ${BASE_REF} — fetch it, or set BITEPROOF_BASE to the base ref" >&2
  exit 2
}
git cat-file -e "${BASE_REV}:${GUARD_IN_REPO}" 2>/dev/null || {
  echo "cannot read ${GUARD_IN_REPO} at ${BASE_REF} (${BASE_REV})" >&2
  exit 2
}
OLD_GUARD=$(mktemp)
PIN_PROBE=$(mktemp)
git show "${BASE_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"

# CAPABILITY REFUSAL. What this harness needs from the base is the guard that carries
# the FIXED cap, because that is the trap every case drives. Once this fix is merged
# `origin/main` IS the fixed guard, so the base is judged by the DECLARATION, and the
# suggestion below is DERIVED from this guard's own history rather than hand-typed
# (the same rule `biteproof_docs_index_floor_mirror.sh` and
# `biteproof_model_default_guard.py` apply, for the same reason: a hand-typed revision
# goes stale, and a base that has caught up makes every BASE leg say "does not
# reproduce the trap" — a statement about the base, not about this branch).
#
# JUDGED BY THE DECLARATION, NOT BY A SUBSTRING (card t_8715a2af). This refusal used a
# plain substring (`grep -qF 'total < 800'`), and the FIXED guard quotes that very
# string while explaining what it replaced (`src/lib/docsIndex.test.ts`: "This test
# asserted `total < 800`"). The substring match therefore read the COMMENT, accepted the
# fixed guard as the "old" column, and every BASE leg then reported a statement about
# the base ("does not reproduce the trap") with exit 3 — the harness was born red on its
# default path from the moment the fix merged. Anything can quote the string a pin is
# named after, so the pin is now the ASSERTION LINE itself, and the fix is detected as a
# DECLARATION too (`^const PAGE_BUDGET_PER_ENTRY =`) rather than as a mention — the fixed
# guard names that constant in its comments as well (`see PAGE_BUDGET_PER_ENTRY above`),
# so the same trap would swallow the opposite rule.
#
# AND THE PIN IS THE SHAPE, NOT THE NUMBER (card t_9d8f1884). It used to be the one
# literal `total < 800,`, which made the base column a fossil: the only revisions that
# carry it are ones from before the page outgrew 800, so the base could never be GREEN on
# the unmutated tree again and the harness refused on both its default and its suggested
# base — no verdict at all. What makes a guard the PRE-FIX one is the SHAPE (one constant
# cap for the whole page, and no derived budget), so any cap matches here and the number
# is re-based from the page below.
CAP_DECL_RE='^([[:space:]]*)total < ([0-9]+),[[:space:]]*$'   # the pre-fix ASSERTION, any cap
PIN_RE='total < N'                                     # what a reader calls it (echo only)
FIXED_RE='PAGE_BUDGET_PER_ENTRY'                        # what a reader calls it (echo only)
FIXED_DECL_RE='^const PAGE_BUDGET_PER_ENTRY[[:space:]]*='
# carry_pin <file> — the base can hold the trap only if it ASSERTS a fixed cap and does
# NOT declare the derived one. Both halves are needed: the assertion alone no longer
# proves the guard is pre-fix (the same file could have gained the derived budget).
carry_pin() {
  grep -qE "$CAP_DECL_RE" "$1" || return 1
  grep -qE "$FIXED_DECL_RE" "$1" && return 1
  return 0
}
# newest_pre_fix_rev — the newest revision of this guard that still carries the pre-fix
# shape. Derived from the file's own history, so it cannot be a hand-typed revision that
# has since caught up with the fix (the shape biteproof_docs_index_floor_mirror.sh applies
# to the same problem).
newest_pre_fix_rev() {
  local rev
  while read -r rev; do
    [ -n "$rev" ] || continue
    git show "${rev}:${GUARD_IN_REPO}" > "$PIN_PROBE" 2>/dev/null || continue
    carry_pin "$PIN_PROBE" && { printf '%s' "$rev"; return 0; }
  done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  return 1
}
BASE_DESC="${BASE_REF} ${BASE_REV:0:12}"
if ! carry_pin "$OLD_GUARD"; then
  if [ -n "${BITEPROOF_BASE:-}" ]; then
    echo >&2
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): CONDITION 1 OF 2 FAILED — the base guard" >&2
    echo "(${BASE_DESC}) does not carry the pre-fix fixed cap (\`${PIN_RE},\`) as an ASSERTION" >&2
    echo "any more, so it cannot hold the trap every case drives." >&2
    grep -qE "$FIXED_DECL_RE" "$OLD_GUARD" && \
      echo "  (it does declare the fix: ${FIXED_RE})" >&2
    grep -qE "$CAP_DECL_RE" "$OLD_GUARD" && \
      echo "  (it also asserts a fixed cap — but a guard that declares the fix is not the pre-fix one)" >&2
    if suggested=$(newest_pre_fix_rev); then
      echo "  Point BITEPROOF_BASE at the newest revision whose guard still ASSERTS" >&2
      echo "  \`${PIN_RE},\` and does not declare the fix:" >&2
      echo "    BITEPROOF_BASE=${suggested} bash web/scripts/${0##*/}" >&2
      echo "  (with no BITEPROOF_BASE this harness picks it itself and says so; see the header.)" >&2
    else
      echo "  There is no revision to point at either: NO revision in ${GUARD_IN_REPO}'s history" >&2
      echo "  asserts \`${PIN_RE},\` without declaring the fix, so this harness cannot reach a" >&2
      echo "  verdict from this history at all." >&2
    fi
    rm -f "$OLD_GUARD" "$PIN_PROBE"
    exit 2
  fi
  # DEFAULT BASE: origin/main carries the fix, so the "before" column can only come from
  # history. This is announced on stdout, not silent — a reader must be able to see which
  # revision the base column ran.
  if suggested=$(newest_pre_fix_rev); then
    BASE_REV=$suggested
    git show "${BASE_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"
    BASE_DESC="history ${BASE_REV:0:12} (the default base ${BASE_REF} carries the fix)"
    echo "note:   the default base ${BASE_REF} is the FIXED guard, so the 'before' column is"
    echo "        derived from history: ${BASE_REV} — the newest revision that still asserts"
    echo "        a fixed cap and does not declare the fix. Its cap is re-based below."
  else
    echo >&2
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): CONDITION 1 OF 2 FAILED — the default base" >&2
    echo "(${BASE_DESC}) carries the fix, and NO revision in ${GUARD_IN_REPO}'s history asserts" >&2
    echo "\`${PIN_RE},\` without declaring the fix either, so this harness cannot reach a verdict" >&2
    echo "from this history at all." >&2
    rm -f "$OLD_GUARD" "$PIN_PROBE"
    exit 2
  fi
fi

BAK=$(mktemp -d)
OUT_FILE=$(mktemp)
REBASED_GUARD=$(mktemp)
trap 'rm -f "$OLD_GUARD" "$PIN_PROBE" "$OUT_FILE" "$OUT_FILE.rc" "$OLD_COPY" "$REBASED_GUARD"; rm -rf "$BAK"' EXIT

sha_of() { sha256sum "$1" | cut -d' ' -f1; }
FILES=("$GUARD" "$PAGE")
for f in "${FILES[@]}"; do cp "$f" "$BAK/$(basename "$f").bak"; done
SHA_GUARD=$(sha_of "$GUARD")
SHA_PAGE=$(sha_of "$PAGE")

restore_all() {
  local ok=1 f
  for f in "${FILES[@]}"; do cp "$BAK/$(basename "$f").bak" "$f"; done
  [ "$(sha_of "$GUARD")" = "$SHA_GUARD" ] || { echo "  *** RESTORE MISMATCH $GUARD ***"; ok=0; }
  [ "$(sha_of "$PAGE")" = "$SHA_PAGE" ] || { echo "  *** RESTORE MISMATCH $PAGE ***"; ok=0; }
  [ "$ok" = 1 ] && echo "       restored byte-identical (2 files, each against its own hash)"
  BASE_SEDS=()
  fail=$((fail + (1 - ok)))
}

# run_guard <file> -> prints GREEN, RED or REFUSED; the full output lands in $OUT_FILE
# and the runner's exit status in $OUT_FILE.rc.
# Two traps this shape avoids, both seen in sibling harnesses: the test runner EXITS
# non-zero on failure, so the output is captured before it is inspected (a pipeline into
# `grep -q` under `set -o pipefail` reads a real match as "not found"); and run_guard is
# called as `got=$(run_guard ...)`, i.e. in a SUBSHELL, so a variable set inside it would
# be discarded — the output therefore goes to a FILE.
# THE THIRD TRAP IS THE VERDICT ITSELF (card t_b014807f): this used to print GREEN
# whenever the capture held no `ℹ fail [1-9]` line, so a node that could not run the file
# (a 64-byte "bad option" capture) answered GREEN on every leg. nt_capture truncates the
# capture first and reads GREEN only off `ℹ pass` + `ℹ fail 0` + a `✔ ` line, and REFUSED
# otherwise; the callers below turn REFUSED into exit 2.
run_guard() {
  nt_capture "$OUT_FILE" "$1"
}
# The base guard runs as a sibling `.ts` copy (node strips types by extension), beside the
# real guard because the guard resolves its paths from import.meta.url. CASE 1 adds an
# entry, so its title must also be added to ORIGINAL_TITLES in the BASE copy, or the base
# leg would go red on page parity instead of on the trap.
BASE_SEDS=()        # per-case edits, added and cleared by the cases
run_base_guard() {
  cp "$REBASED_GUARD" "$OLD_COPY"
  local e
  for e in "${BASE_SEDS[@]}"; do sed -i "$e" "$OLD_COPY"; done
  local got; got=$(run_guard "$OLD_COPY")
  rm -f "$OLD_COPY"
  printf '%s' "$got"
}

nfail() { sed -nE 's/^ℹ fail ([0-9]+)$/\1/p' "$OUT_FILE" | head -1; }

# ---- the base column's CAP, RE-BASED onto today's page -------------------------
# CASE 2 is the hair trigger: the SMALLEST edit that trips the FIXED cap, and that size is the
# cap's HEADROOM — not a constant. It used to be the pre-fix revision's own headroom, which
# works only while the page is still under that fossil's cap: measured 2026-10-29 the page
# carried 873 words against a pinned 800, so the "headroom" was −73 and the harness refused
# (card t_9d8f1884). The cap is therefore DERIVED from the page and written into the base
# COPY only — never the guard, never a git object — at `PAGE_TOTAL + 1`, the trap's own
# measured worst shape ("main arrived at 799: ONE word of headroom"). That makes the base
# column unable to go stale a second time.
#
# Every number below comes from a guard's own READER, never from a re-implementation: the
# page's total and the cap line both come out of the base guard when its cap is lowered so
# the assertion reds (a second counting rule would be one more thing to rot, t_8715a2af).
BASE_CAP_HIST=$(sed -nE "s/${CAP_DECL_RE}/\2/p" "$OLD_GUARD" | head -1)
CAP_LINES=$(grep -cE "$CAP_DECL_RE" "$OLD_GUARD")
case "${BASE_CAP_HIST:-}" in ''|*[!0-9]*)
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): cannot read a fixed cap out of the base guard's"
  echo "assertion \`${PIN_RE},\`, so the base column cannot be re-based onto this page."
  exit 2 ;;
esac
if [ "$CAP_LINES" != 1 ]; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the base guard carries ${CAP_LINES} lines matching"
  echo "\`${PIN_RE},\`, so re-basing 'the' cap onto today's page would have to guess which one is the cap."
  exit 2
fi
# page_total_words — the page's summary total, from the base guard's own READER (lower the cap
# until the assertion reds and read the number it prints). Nothing here re-counts words: a
# second counting rule would be one more thing to rot.
page_total_words() {
  cp "$OLD_GUARD" "$OLD_COPY"
  sed -i -E "s/${CAP_DECL_RE}/\1total < 1,/" "$OLD_COPY"
  run_guard "$OLD_COPY" >/dev/null
  nt_refuse_if "the word-total probe (the base guard with its cap lowered to 1)" "$OUT_FILE"
  rm -f "$OLD_COPY"
  sed -nE 's/.*the feature summaries total ([0-9]+) words.*/\1/p' "$OUT_FILE" | head -1
}
PAGE_TOTAL=$(page_total_words)
case "$PAGE_TOTAL" in ''|*[!0-9]*)
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the base guard reported no word total for this page," >&2
  echo "so the base column cannot be re-based — and a hand-typed cap is exactly what went" >&2
  echo "stale in the base column (card t_8715a2af)." >&2
  exit 2 ;;
esac
# The entry count comes from THIS tree's guard, not the base's: the pre-fix guard's sentence
# is only "total N words — the page is supposed to be read in ~2 minutes" (no entry count),
# while the derived budget's sentence names it ("… against a 35-word budget (1 words x 35
# entries)"). Reading it with the constant lowered to 1 gives BOTH numbers, and the two
# columns' totals must agree — otherwise they are not measuring one page and no case below
# would be attributable. Hand-typing this number is what rotted the CASE 3/4 needles (33
# entries while the page declared more), so it is read every run.
page_total_and_entries_this_tree() {
  cp "$GUARD" "$OLD_COPY"
  sed -i -E "s/^const PAGE_BUDGET_PER_ENTRY[[:space:]]*=[[:space:]]*[0-9]+;/const PAGE_BUDGET_PER_ENTRY = 1;/" "$OLD_COPY"
  run_guard "$OLD_COPY" >/dev/null
  nt_refuse_if "the entry-count probe (this tree's guard with its per-entry constant lowered to 1)" "$OUT_FILE"
  rm -f "$OLD_COPY"
}
page_total_and_entries_this_tree
NEW_TOTAL=$(sed -nE 's/.*the feature summaries total ([0-9]+) words against.*/\1/p' "$OUT_FILE" | head -1)
ENTRY_COUNT=$(sed -nE 's/.*\(1 words x ([0-9]+) entries\).*/\1/p' "$OUT_FILE" | head -1)
case "${ENTRY_COUNT:-}" in ''|*[!0-9]*)
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): this tree's guard did not name the number of entries" >&2
  echo "its derived budget covers, so the CASE 3/4 needles cannot be derived from it."
  exit 2 ;;
esac
if [ "${NEW_TOTAL:-}" != "$PAGE_TOTAL" ]; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the two columns disagree about this page — the base" >&2
  echo "guard reads ${PAGE_TOTAL} word(s), this tree's guard reads ${NEW_TOTAL:-nothing}. No case below" >&2
  echo "could be attributed to its mutation while the two columns are not measuring one page."
  exit 2
fi
# This tree's own per-entry constant, out of its DECLARATION (the line CASE 4 moves).
PER_ENTRY=$(grep -E "^const PAGE_BUDGET_PER_ENTRY[[:space:]]*=[[:space:]]*[0-9]+;" "$GUARD" \
              | head -1 | sed -nE 's/^const PAGE_BUDGET_PER_ENTRY[[:space:]]*=[[:space:]]*([0-9]+);$/\1/p')
case "${PER_ENTRY:-}" in ''|*[!0-9]*)
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): cannot read this tree's \`const PAGE_BUDGET_PER_ENTRY\`" >&2
  echo "declaration, so the derived budget's own message cannot be predicted."
  exit 2 ;;
esac
PER_ENTRY_LOW=$((PER_ENTRY - 1))

# ---- the re-based base guard: one cap line, and today's titles ---------------
# Both edits land in a COPY under mktemp, never in the guard and never in a git object.
BASE_CAP=$((PAGE_TOTAL + 1))       # the trap's own worst shape: ONE word of headroom
cp "$OLD_GUARD" "$REBASED_GUARD"
sed -i -E "s/${CAP_DECL_RE}/\1total < ${BASE_CAP},/" "$REBASED_GUARD"
REBASED_CAP_LINES=$(grep -cE "^[[:space:]]*total < ${BASE_CAP},[[:space:]]*\$" "$REBASED_GUARD")
if [ "$REBASED_CAP_LINES" != 1 ]; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): re-basing the base cap to ${BASE_CAP} left" >&2
  echo "${REBASED_CAP_LINES} matching line(s) instead of 1, so the base column was not re-based."
  exit 2
fi
# ORIGINAL_TITLES goes stale the same way the cap does: it is PART of the pre-fix guard, so a
# revision that predates today's entries reds the PARITY test on the unmutated tree — a
# statement about the page, not about this branch. The base copy therefore receives the
# titles this tree's guard carries and the base's does not (append-only: the base's own list
# is kept, so nothing it knew is dropped).
CUR_TITLES=$(sed -n '/^const ORIGINAL_TITLES = \[/,/^\];/p' "$GUARD")
BASE_TITLES=$(sed -n '/^const ORIGINAL_TITLES = \[/,/^\];/p' "$OLD_GUARD")
TITLES_ADD=""
while IFS= read -r tline; do
  [ -n "$tline" ] || continue
  printf '%s\n' "$BASE_TITLES" | grep -qxF "$tline" && continue
  TITLES_ADD="${TITLES_ADD}${tline}"$'\n'
done < <(printf '%s\n' "$CUR_TITLES" | grep -E '^[[:space:]]*"[^"]+",[[:space:]]*$')
if [ -n "$TITLES_ADD" ]; then
  if ! ADD="$TITLES_ADD" perl -0777 -pi -e '
        my $add = $ENV{ADD};
        my $n = s{(^const ORIGINAL_TITLES = \[.*?)(^\];)}{$1 . $add . $2}mse;
        die "ORIGINAL_TITLES block matched $n times, want 1\n" unless $n == 1;
      ' "$REBASED_GUARD"; then
    echo >&2
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): could not add this tree's newer titles to the" >&2
    echo "base guard's ORIGINAL_TITLES, so the base would red on page parity instead of on the cap."
    exit 2
  fi
  while IFS= read -r tline; do
    [ -z "$tline" ] && continue
    grep -qxF "$tline" "$REBASED_GUARD" || {
      echo >&2
      echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the title ${tline} is not in the re-based base" >&2
      echo "guard after the merge, so the re-base did not land."
      exit 2
    }
  done <<< "$TITLES_ADD"
fi
TITLES_ADDED=$(printf '%s' "$TITLES_ADD" | grep -c '[^[:space:]]' || true)

CASE2_FILLER_ROOM=20   # the per-entry LIMIT is 45 and the Sales body carries 23, so 22 fit; 20 with margin
CASE2_DEFICIT=$((BASE_CAP - PAGE_TOTAL))
if [ "$CASE2_DEFICIT" -lt 1 ] || [ "$CASE2_DEFICIT" -gt "$CASE2_FILLER_ROOM" ]; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): CASE 2's hair trigger would have to add ${CASE2_DEFICIT}" >&2
  echo "word(s) to reach the fixed cap at ${BASE_CAP} from this page's ${PAGE_TOTAL} — outside the" >&2
  echo "1..${CASE2_FILLER_ROOM} one summary can take without crossing the per-entry LIMIT (a wider gap" >&2
  echo "needs a SPLIT filler like CASE 3's, which this harness does not have)." >&2
  exit 2
fi
CASE2_FILLER="Bite proof filler words that keep this summary inside its own per-entry limit while still pushing the page exactly onto the fixed cap."

fail=0
passed=0

# check <label> <base-want> <base-needle|-> <new-want> <new-needle|-> [want-nfail]
#   base-needle  : text the BASE leg must carry (the trap's own message)
#   new-needle   : text the NEW leg must carry (the budget it must name)
#   want-nfail   : how many tests the NEW leg must fail (default 1 when NEW is RED)
check() {
  local label="$1" base_want="$2" base_needle="$3" new_want="$4" new_needle="$5" want_n="${6:-}"
  local got
  # `-` means "no needle is required" for that column.
  [ "$base_needle" = "-" ] && base_needle=""
  [ "$new_needle" = "-" ] && new_needle=""
  [ -n "$want_n" ] || want_n=1

  got=$(run_base_guard)
  # A refusal must not leave the case's mutation on disk: restore first, then refuse.
  if [ "$got" = REFUSED ]; then restore_all; nt_refuse_if "${label}: BASE guard" "$OUT_FILE"; fi
  if [ "$got" != "$base_want" ]; then
    echo "  *** ${label}: BASE guard wanted ${base_want}, got ${got} — the case does not reproduce the trap ***"
    grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2; fail=$((fail + 1))
  elif [ -n "$base_needle" ] && ! grep -qF "$base_needle" "$OUT_FILE"; then
    echo "  *** ${label}: BASE guard ${got} but its output does not carry '${base_needle}' ***"
    grep -E '^✖' "$OUT_FILE" | head -3; fail=$((fail + 1))
  else
    echo "  ok   ${label}: base guard ${got}${base_needle:+, carrying '${base_needle}'}"
  fi

  got=$(run_guard "$GUARD")
  # A refusal must not leave the case's mutation on disk: restore first, then refuse.
  if [ "$got" = REFUSED ]; then restore_all; nt_refuse_if "${label}: NEW guard" "$OUT_FILE"; fi
  local n; n=$(nfail); : "${n:=0}"
  if [ "$got" != "$new_want" ]; then
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${got} ***"
    grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2; fail=$((fail + 1))
  elif [ -n "$new_needle" ] && ! grep -qF "$new_needle" "$OUT_FILE"; then
    echo "  *** ${label}: NEW guard ${got} but the failure does not name '${new_needle}' ***"
    grep -E '^✖' "$OUT_FILE" | head -3; fail=$((fail + 1))
  elif [ "$new_want" = "RED" ] && [ "$n" != "$want_n" ]; then
    echo "  *** ${label}: expected exactly ${want_n} failing test(s), saw ${n} — the red is not attributable to this mutation ***"
    grep -E '^✖' "$OUT_FILE" | head -4; fail=$((fail + 1))
  else
    echo "  BITES ${label}: new guard ${got}, ${n} failing test(s)${new_needle:+, naming '${new_needle}'}"
    passed=$((passed + 1))
  fi
}

# ---- the mutations, each asserted to be on disk before it is measured --------
assert_on_disk() { # assert_on_disk <file> <fixed string> <label>
  if grep -qF "$2" "$1"; then echo "       mutation on disk: $3"; return 0; fi
  echo "  *** MUTATION DID NOT APPLY to $1: $3 ***"; fail=$((fail + 1)); return 1
}

# perl_replace <file> <anchor> <replacement> <label> — ONE occurrence, asserted.
# The anchor is passed through \Q...\E, so a `.` or a `(` in the anchor is text, and
# an anchor that matches 0 or 2+ times is REFUSED rather than silently mutating the
# wrong thing (a mutation that never landed is a silent green — the shape this whole
# harness exists to remove).
perl_replace() {
  local file="$1" anchor="$2" repl="$3" label="$4" count
  count=$(ANCHOR="$anchor" perl -0777 -ne 'my $a = $ENV{ANCHOR}; my $n = () = /\Q$a\E/g; print $n' "$file")
  if [ "$count" != "1" ]; then
    echo "  *** MUTATION ANCHOR MISMATCH in $file: ${label} matched ${count} times, want 1 ***"
    fail=$((fail + 1)); return 1
  fi
  ANCHOR="$anchor" REPL="$repl" perl -0777 -pi -e 'my $a=$ENV{ANCHOR}; my $r=$ENV{REPL}; s/\Q$a\E/$r/' "$file"
  return 0
}

# m_add_entry — append a whole feature ENTRY to the LAST group's entries array.
# No `href`: an entry with a link must also carry DESTINATION_FACTS, and the facts
# check is not what this case is about (the guard's own comment names Products /
# Landing pages / Media library / Settings as the no-href shape).
PROBE_TITLE="QA budget probe"
PROBE_BODY="Reserved for the word-budget bite proof and never on the shipped page, this summary is a normal-length feature line of twenty-two words."
ADD_ANCHOR=$'    ],\n  },\n];'
m_add_entry() {
  local repl
  repl=$(printf '      {\n        title: "%s",\n        body: "%s",\n      },\n%s' "$PROBE_TITLE" "$PROBE_BODY" "$ADD_ANCHOR")
  perl_replace "$PAGE" "$ADD_ANCHOR" "$repl" "a new entry appended to the last group" || return 1
  sed -i "s|^  \"Activity log (audit trail)\",\$|  \"Activity log (audit trail)\",\\n  \"${PROBE_TITLE}\",|" "$GUARD"
  assert_on_disk "$PAGE" "title: \"${PROBE_TITLE}\"," "the probe entry is in GROUPS" || return 1
  assert_on_disk "$GUARD" "  \"${PROBE_TITLE}\"," "the probe title is in ORIGINAL_TITLES" || return 1
  BASE_SEDS+=("s|^  \"Activity log (audit trail)\",\$|  \"Activity log (audit trail)\",\\n  \"${PROBE_TITLE}\",|")
}

SALES_BODY='        body: "Track every sale: reference code, status, buyer, amount, payment method and date. Confirm bank transfers, capture or release authorisations, refund, and export CSV.",'
API_BODY='        body: "Drive JomForm from your own server: create forms, read sales and responses, manage products, subscriptions and webhooks.",'
# m_grow_words <sentence> — append a sentence to the SALES summary (no new entry).
# Every mutated body must stay under the per-entry LIMIT (45 words), or the SAME test's
# length assertion fires FIRST and the budget assertion below it is never reached — the
# case would then measure the wrong rule (measured: Sales at 23 words + a 23-word filler
# is 46, over the limit).
m_grow_words() {
  local extra="$1" repl
  repl=$(printf '%s %s' "${SALES_BODY%\",}" "$extra\",")
  perl_replace "$PAGE" "$SALES_BODY" "$repl" "the Sales summary grows by '${extra}'" || return 1
}
# m_grow_api_words <sentence> — the second body CASE 3 spreads its filler over.
m_grow_api_words() {
  local extra="$1" repl
  repl=$(printf '%s %s' "${API_BODY%\",}" "$extra\",")
  perl_replace "$PAGE" "$API_BODY" "$repl" "the REST API summary grows by '${extra}'" || return 1
}
# m_trim_words — the same summary, cut back (the negative-control direction).
m_trim_words() {
  local repl='        body: "Track every sale: reference code, buyer, amount and date. Confirm bank transfers, refund, and export CSV.",'
  perl_replace "$PAGE" "$SALES_BODY" "$repl" "the Sales summary drops seven words" || return 1
}
# m_lower_cap — the DERIVED ceiling itself, moved down one word per entry. Both numbers are
# read from the guard's own declaration, never typed: "25 -> 24" went stale the moment the
# constant moved, and the needle below would then name a budget the guard no longer prints.
m_lower_cap() {
  sed -i "s|^const PAGE_BUDGET_PER_ENTRY = ${PER_ENTRY};$|const PAGE_BUDGET_PER_ENTRY = ${PER_ENTRY_LOW};|" "$GUARD"
  assert_on_disk "$GUARD" "const PAGE_BUDGET_PER_ENTRY = ${PER_ENTRY_LOW};" "the per-entry budget is ${PER_ENTRY_LOW}" || return 1
}

echo "old guard (${BASE_DESC}): $(wc -l < "$OLD_GUARD") lines, fixed cap '${PIN_RE}' (history: ${BASE_CAP_HIST})"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines, derived budget '${FIXED_RE}'"
echo "re-based base column:              cap ${BASE_CAP} = page ${PAGE_TOTAL} + 1, plus ${TITLES_ADDED} newer title(s) in ORIGINAL_TITLES"

# PREMISE: the re-based base guard must be GREEN on the unmutated tree. If it is not, it is
# red for a reason that has nothing to do with this branch and no mutation can make it
# "miss" anything.
premise=$(run_base_guard)
nt_refuse_if "BASELINE, the base guard on the UNMUTATED tree" "$OUT_FILE"
if [ "$premise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: CONDITION 2 OF 2 FAILED — the re-based base guard (${BASE_DESC}, cap" >&2
  echo "${BASE_CAP}) is ${premise} on the UNMUTATED tree, so there is no 'before' column and this" >&2
  echo "proof would be about nothing (the page, not this branch)." >&2
  grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2 >&2
  grep -E '^✖' "$OUT_FILE" | head -2 >&2
  exit 2
fi
newpremise=$(run_guard "$GUARD")
nt_refuse_if "BASELINE, this tree's guard on the UNMUTATED tree" "$OUT_FILE"
if [ "$newpremise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: this tree's guard is ${newpremise} on the UNMUTATED tree, so a red below" >&2
  echo "would not be attributable to any case." >&2
  grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2 >&2
  grep -E '^✖' "$OUT_FILE" | head -2 >&2
  exit 2
fi

echo
echo "== BASELINE (no mutation) =="
check "baseline" GREEN - GREEN -

echo
echo "== CASE 1: a NEW feature entry with a NORMAL 22-word summary (the card's acceptance test) =="
echo "          (base: RED — the fixed cap has one word of headroom; tree: GREEN — the ceiling moved)"
m_add_entry && check "a new entry with a normal summary" RED "words — the page is supposed to be read in ~2 minutes" GREEN -
restore_all

echo
echo "== CASE 2: the SMALLEST edit that trips the fixed cap — ${CASE2_DEFICIT} word(s) here (${PAGE_TOTAL} -> ${BASE_CAP}) =="
echo "          (the size is DERIVED from the cap's real headroom on this page; it was ONE word at the tag's 799)"
echo "          (base: RED at exactly ${BASE_CAP}; tree: GREEN — ${BASE_CAP} against a derived budget)"
CASE2_WORDS=$(printf '%s' "$CASE2_FILLER" | cut -d' ' -f1-"$CASE2_DEFICIT")
m_grow_words "$CASE2_WORDS" && assert_on_disk "$PAGE" "$CASE2_WORDS" "the derived CASE 2 filler is on disk" && \
  check "the smallest edit that trips the fixed cap" RED "the feature summaries total ${BASE_CAP} words" GREEN -
restore_all

echo
echo "== CASE 3: a page made FATTER by 30 words across two summaries, no new entry =="
echo "          (the direction that must NOT be lost; both columns RED, the tree naming the derived budget)"
echo "          (the filler is SPLIT so no body crosses the per-entry LIMIT — that assertion runs first)"
FAT_A="Filler alpha keeps this summary under its own per-entry limit while still pushing the page past the budget."
FAT_B="Filler beta raises the aggregate count too without becoming a paragraph here."
m_grow_words "$FAT_A" && m_grow_api_words "$FAT_B" && \
  check "a fatter page, no new entry" RED "words — the page is supposed to be read in ~2 minutes" RED "(${PER_ENTRY} words x ${ENTRY_COUNT} entries)"
restore_all

echo
echo "== CASE 4: the DERIVED ceiling moved ${PER_ENTRY} -> ${PER_ENTRY_LOW} words per entry, the page untouched =="
echo "          (base: GREEN — the page's ${PAGE_TOTAL} sits under the re-based fixed cap ${BASE_CAP}; tree: RED"
echo "           naming the derived budget for the ${ENTRY_COUNT} entries the page declares)"
m_lower_cap && check "the per-entry budget lowered" GREEN - RED "(${PER_ENTRY_LOW} words x ${ENTRY_COUNT} entries)"
restore_all

echo
echo "== CASE 5: NEGATIVE CONTROL — a summary TRIMMED by seven words =="
echo "          (GREEN in both columns: the check has not become 'any edit reds')"
m_trim_words && check "a trimmed summary" GREEN - GREEN -
restore_all

echo
echo "== GUARD RESTORED =="
if [ "$(sha_of "$GUARD")" = "$SHA_GUARD" ] && [ "$(sha_of "$PAGE")" = "$SHA_PAGE" ] && [ ! -e "$OLD_COPY" ]; then
  echo "       $GUARD and $PAGE are byte-identical to the pre-run bytes; no .oldcheck copy left"
else
  echo "  *** a file was NOT restored — restore it from $BAK BY HAND before doing anything else ***"
  fail=$((fail + 1))
fi
# The worktree leg compares against the STATUS THIS RUN STARTED WITH, not against
# "clean": a lane legitimately runs this harness on a tree with uncommitted work in
# those two files, and the property that matters is that no MUTATION survived — a
# leftover would change this line.
STATUS_NOW="$(git -C "$TOP" status --porcelain -- "$GUARD_IN_REPO" "$PAGE_IN_REPO")"
if [ "$STATUS_NOW" = "$STATUS_BEFORE" ]; then
  echo "       git status --porcelain for both files is unchanged by this run (${STATUS_BEFORE:+dirty at start, dirty now}${STATUS_BEFORE:-clean before and after})"
else
  echo "  *** git status CHANGED for the files this harness mutates:"
  echo "      before: ${STATUS_BEFORE:-<clean>}"
  echo "      after:  ${STATUS_NOW:-<clean>}"
  fail=$((fail + 1))
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — $passed checks: a new entry with a normal summary leaves the tree GREEN while the base guard reds, the smallest edit that trips the fixed cap (${CASE2_DEFICIT} word(s) derived from its headroom) no longer breaks the build, a fatter page still reds naming the derived budget, the moved constant reds while the base stays green, and a trim stays green."
else
  echo "BITEPROOF FAIL ($fail failing check(s), $passed passing)"
fi
exit "$fail"
