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
#   * CASE 2, the hair trigger the card measured — ONE word added to one summary:
#     the BASE reds at exactly 800, the tree stays GREEN. This is the shape that
#     would have blocked every lane's build today.
#   * CASE 3, the direction that must NOT be lost — a page made FATTER by 28
#     words with no new entry: RED in both columns, naming the budget. A fix that
#     only bought headroom would be GREEN here, so this is the control that says
#     the budget is still a budget.
#   * CASE 4, the DERIVED ceiling itself — the constant moved 25 -> 24 words per
#     entry, page untouched: the tree REDS naming the 792-word budget for 33
#     entries while the BASE is GREEN (799 < 800). A discriminating pair: the red
#     can only come from the derived budget, not from the old fixed cap.
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
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
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
git show "${BASE_REV}:${GUARD_IN_REPO}" > "$OLD_GUARD"

# CAPABILITY REFUSAL. What this harness needs from the base is the guard that carries
# the FIXED cap, because that is the trap every case drives. Once this fix is merged
# `origin/main` IS the fixed guard, so the base is judged by the DECLARATION, and the
# suggestion below is DERIVED from this guard's own history rather than hand-typed
# (the same rule `biteproof_docs_index_floor_mirror.sh` and
# `biteproof_model_default_guard.py` apply, for the same reason: a hand-typed revision
# goes stale, and a base that has caught up makes every BASE leg say "does not
# reproduce the trap" — a statement about the base, not about this branch).
PIN_RE='total < 800'
FIXED_RE='PAGE_BUDGET_PER_ENTRY'
if ! grep -qF "$PIN_RE" "$OLD_GUARD"; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the base guard (${BASE_REF} ${BASE_REV:0:12}) does not" >&2
  echo "carry the pre-fix fixed cap \`${PIN_RE}\` any more, so it cannot hold the trap every case drives." >&2
  grep -qF "$FIXED_RE" "$OLD_GUARD" && \
    echo "  (it does declare the fix: ${FIXED_RE})" >&2
  suggested=""
  while read -r rev; do
    [ -n "$rev" ] || continue
    if grep -qF "$PIN_RE" <<<"$(git show "${rev}:${GUARD_IN_REPO}" 2>/dev/null)"; then suggested="$rev"; break; fi
  done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  if [ -n "$suggested" ]; then
    echo "  Point BITEPROOF_BASE at the newest revision whose guard still carries \`${PIN_RE}\`:" >&2
    echo "    BITEPROOF_BASE=${suggested} bash web/scripts/${0##*/}" >&2
  else
    echo "  (no revision in ${GUARD_IN_REPO}'s history carries \`${PIN_RE}\` either — set" >&2
    echo "   BITEPROOF_BASE by hand)" >&2
  fi
  rm -f "$OLD_GUARD"
  exit 2
fi

BAK=$(mktemp -d)
OUT_FILE=$(mktemp)
trap 'rm -f "$OLD_GUARD" "$OUT_FILE" "$OLD_COPY"; rm -rf "$BAK"' EXIT

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

# run_guard <file> -> prints GREEN or RED; the full output lands in $OUT_FILE.
# Two traps this shape avoids, both seen in sibling harnesses: the test runner EXITS
# non-zero on failure, so the output is captured before it is inspected (a pipeline into
# `grep -q` under `set -o pipefail` reads a real match as "not found"); and run_guard is
# called as `got=$(run_guard ...)`, i.e. in a SUBSHELL, so a variable set inside it would
# be discarded — the output therefore goes to a FILE.
run_guard() {
  local out
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || true
  printf '%s\n' "$out" > "$OUT_FILE"
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
}
# The base guard runs as a sibling `.ts` copy (node strips types by extension), beside the
# real guard because the guard resolves its paths from import.meta.url. CASE 1 adds an
# entry, so its title must also be added to ORIGINAL_TITLES in the BASE copy, or the base
# leg would go red on page parity instead of on the trap.
BASE_SEDS=()
run_base_guard() {
  cp "$OLD_GUARD" "$OLD_COPY"
  local e
  for e in "${BASE_SEDS[@]}"; do sed -i "$e" "$OLD_COPY"; done
  local got; got=$(run_guard "$OLD_COPY")
  rm -f "$OLD_COPY"
  printf '%s' "$got"
}

nfail() { sed -nE 's/^ℹ fail ([0-9]+)$/\1/p' "$OUT_FILE" | head -1; }

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
# m_lower_cap — the DERIVED ceiling itself, moved down one word per entry.
m_lower_cap() {
  sed -i 's|^const PAGE_BUDGET_PER_ENTRY = 25;$|const PAGE_BUDGET_PER_ENTRY = 24;|' "$GUARD"
  assert_on_disk "$GUARD" "const PAGE_BUDGET_PER_ENTRY = 24;" "the per-entry budget is 24" || return 1
}

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines, fixed cap '${PIN_RE}'"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines, derived budget '${FIXED_RE}'"

# PREMISE: the base guard must be GREEN on the unmutated tree. If it is not, it is red
# for a reason that has nothing to do with this branch and no mutation can make it
# "miss" anything.
premise=$(run_base_guard)
if [ "$premise" != "GREEN" ]; then
  echo >&2
  echo "BITEPROOF REFUSED: the base guard (${BASE_REF} ${BASE_REV:0:12}) is ${premise} on the UNMUTATED" >&2
  echo "tree, so there is no 'before' column and this proof would be about nothing." >&2
  grep -E '^ℹ (pass|fail)' "$OUT_FILE" | head -2 >&2
  grep -E '^✖' "$OUT_FILE" | head -2 >&2
  exit 2
fi
newpremise=$(run_guard "$GUARD")
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
echo "== CASE 2: ONE word added to one summary (the hair trigger the card measured) =="
echo "          (base: RED at exactly 800; tree: GREEN — 800 against a 825-word budget)"
m_grow_words "Bite." && check "one added word" RED "the feature summaries total 800 words" GREEN -
restore_all

echo
echo "== CASE 3: a page made FATTER by 30 words across two summaries, no new entry =="
echo "          (the direction that must NOT be lost; both columns RED, the tree naming the 825-word budget)"
echo "          (the filler is SPLIT so no body crosses the per-entry LIMIT — that assertion runs first)"
FAT_A="Filler alpha keeps this summary under its own per-entry limit while still pushing the page past the budget."
FAT_B="Filler beta raises the aggregate count too without becoming a paragraph here."
m_grow_words "$FAT_A" && m_grow_api_words "$FAT_B" && \
  check "a fatter page, no new entry" RED "words — the page is supposed to be read in ~2 minutes" RED "(25 words x 33 entries)"
restore_all

echo
echo "== CASE 4: the DERIVED ceiling moved 25 -> 24 words per entry, the page untouched =="
echo "          (base: GREEN — 799 is under the old 800; tree: RED naming the 792-word budget)"
m_lower_cap && check "the per-entry budget lowered" GREEN - RED "(24 words x 33 entries)"
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
  echo "BITEPROOF PASS — $passed checks: a new entry with a normal summary leaves the tree GREEN while the base guard reds, one added word no longer breaks the build, a fatter page still reds naming the derived budget, the moved constant reds while the base stays green, and a trim stays green."
else
  echo "BITEPROOF FAIL ($fail failing check(s), $passed passing)"
fi
exit "$fail"
