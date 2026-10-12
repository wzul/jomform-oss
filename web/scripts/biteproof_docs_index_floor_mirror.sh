#!/usr/bin/env bash
# Bite proof for web/src/lib/docsIndex.test.ts's PER_ENTRY_FLOOR mirror check
# (card t_dd08bc5c).
#
# THE HOLE THIS PINS. The mirror check between DESTINATION_FACTS and PER_ENTRY_FLOOR
# asked membership with `in`, which WALKS THE PROTOTYPE CHAIN. Both lists are plain
# object literals keyed on /docs entry titles, so a title that happens to name an
# `Object.prototype` member (`constructor`, `toString`, `valueOf`) read as PRESENT in a
# map that has no entry for it at all — which is the exact state each mirror assertion
# exists to report:
#
#   * `!Object.hasOwn(PER_ENTRY_FLOOR, t)` (the "entry has no floor" direction) was
#     `!(t in PER_ENTRY_FLOOR)`: rename a floored entry to `constructor` on EVERY side
#     (page title, DESTINATION_FACTS, ORIGINAL_TITLES) and DELETE its floor line, and
#     nothing went red — the entry has no floor and no check said so.
#   * the reverse direction was `!(t in DESTINATION_FACTS)`: an orphan floor line
#     `constructor: 1` was hidden by the same `in`, and its VALUE was 1 because
#     `({}).constructor.length` is `Function.prototype.length` — so the equality check
#     above it (`DESTINATION_FACTS[t].length !== PER_ENTRY_FLOOR[t]`) matched the
#     MEMBER instead of the table and stayed silent too.
#
# WHY A SCRIPT AND NOT AN ASSERTION INSIDE THE GUARD. Every case below needs a
# "before" column, and the before column is the guard as it stands on the branch's
# base. A test inside the guard can only exercise the guard's own rules — it cannot run
# a reader that lacks them. So each case: apply the mutation -> show it really is on
# disk -> run the BASE guard (expect the verdict the hole predicts) -> run this tree's
# guard (expect RED naming the offender, exactly one failing test) -> restore
# byte-identically, re-checked by sha256.
#
# It also pins the THIRD fix in the same change: an orphan floor line with an ORDINARY
# name used to fail the equality check with `TypeError: Cannot read properties of
# undefined (reading 'length')` instead of naming the drift. Case 4 asserts both the
# base's TypeError and the new, attributable message — and every case asserts the new
# guard never fails with that TypeError.
#
# REACHABILITY TODAY IS ZERO, and that is the point of closing it now: 0 of the 31
# /docs entry titles is a prototype member, so nothing is unguarded on the committed
# tree. It is a trap a future rename steps into, not a present defect — the same reason
# the fix is a predicate change rather than a data change.
#
# Run:  bash web/scripts/biteproof_docs_index_floor_mirror.sh
#       BITEPROOF_BASE=<rev> bash web/scripts/biteproof_docs_index_floor_mirror.sh
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
FACTS=src/lib/docsFacts.ts
PAGE=src/app/docs/page.tsx
OLD_COPY="$GUARD.oldcheck.ts"

node --version >/dev/null 2>&1 || { echo "BITEPROOF REFUSED: node is not on PATH." >&2; exit 2; }
for f in "$GUARD" "$FACTS" "$PAGE"; do
  [ -f "$f" ] || { echo "BITEPROOF REFUSED: $f not found — run this from the web/ tree." >&2; exit 2; }
done

TOP=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "not in a git worktree" >&2; exit 2; }
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

# CAPABILITY REFUSAL. Byte-equality is not the property this harness needs: it needs the
# base's mirror check to still ask membership with `in`, or every BASE leg reads RED and
# each case reports "does not reproduce the hole" — a statement about the BASE, not about
# this branch. Judged by the DECLARATION, and the search derives a candidate rather than
# guessing one.
PIN_RE='!(t in PER_ENTRY_FLOOR)'
FIXED_RE='Object\.hasOwn\(PER_ENTRY_FLOOR, t\)'
if ! grep -qF "$PIN_RE" "$OLD_GUARD"; then
  echo >&2
  echo "BITEPROOF REFUSED (exit 2, NO VERDICT): the base guard (${BASE_REF} ${BASE_REV:0:12}) does not" >&2
  echo "carry the pre-fix predicate \`${PIN_RE}\` any more — it is the same guard this tree has, so it" >&2
  echo "cannot miss the prototype-named hole every case drives." >&2
  grep -qF "$FIXED_RE" "$OLD_GUARD" && \
    echo "  (it does declare the fix: ${FIXED_RE})" >&2
  suggested=""
  while read -r rev; do
    [ -n "$rev" ] || continue
    if grep -qF "$PIN_RE" <<<"$(git show "${rev}:${GUARD_IN_REPO}" 2>/dev/null)"; then suggested="$rev"; break; fi
  done < <(git -C "$TOP" log --format='%H' -- "$GUARD_IN_REPO" 2>/dev/null)
  if [ -n "$suggested" ]; then
    echo "  Point BITEPROOF_BASE at a revision whose mirror check still uses \`in\`:" >&2
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
FILES=("$GUARD" "$FACTS" "$PAGE")
for f in "${FILES[@]}"; do cp "$f" "$BAK/$(basename "$f").bak"; done
SHA_GUARD=$(sha_of "$GUARD")
SHA_FACTS=$(sha_of "$FACTS")
SHA_PAGE=$(sha_of "$PAGE")

restore_all() {
  local ok=1 f
  for f in "${FILES[@]}"; do cp "$BAK/$(basename "$f").bak" "$f"; done
  [ "$(sha_of "$GUARD")" = "$SHA_GUARD" ] || { echo "  *** RESTORE MISMATCH $GUARD ***"; ok=0; }
  [ "$(sha_of "$FACTS")" = "$SHA_FACTS" ] || { echo "  *** RESTORE MISMATCH $FACTS ***"; ok=0; }
  [ "$(sha_of "$PAGE")" = "$SHA_PAGE" ] || { echo "  *** RESTORE MISMATCH $PAGE ***"; ok=0; }
  [ "$ok" = 1 ] && echo "       restored byte-identical (3 files, each against its own hash)"
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
# real guard because the guard resolves its paths from import.meta.url. Cases that rename
# an entry title must rename it in ORIGINAL_TITLES too, or the base leg goes red for a
# parity reason that has nothing to do with the hole — so the copy is re-made and the
# case's pending seds are applied to it on every run.
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

# check <label> <base-want> <base-needle|-> <new-want> <new-needle|->
#   base-needle  : text the BASE leg must carry (the hole or the pre-fix defect)
#   new-needle   : text the NEW leg must carry (the offender it names)
check() {
  local label="$1" base_want="$2" base_needle="$3" new_want="$4" new_needle="$5"
  local got
  # `-` means "no needle is required" for that column.
  [ "$base_needle" = "-" ] && base_needle=""
  [ "$new_needle" = "-" ] && new_needle=""

  got=$(run_base_guard)
  if [ "$got" != "$base_want" ]; then
    echo "  *** ${label}: BASE guard wanted ${base_want}, got ${got} — the case does not reproduce the hole ***"
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
  elif [ "$new_want" = "RED" ] && [ "$n" != "1" ]; then
    echo "  *** ${label}: expected exactly 1 failing test, saw ${n} — the red is not attributable to this mutation ***"
    grep -E '^✖' "$OUT_FILE" | head -4; fail=$((fail + 1))
  elif [ "$new_want" = "RED" ] && grep -qF "Cannot read properties of undefined (reading 'length')" "$OUT_FILE"; then
    echo "  *** ${label}: NEW guard ${got} by THROWING the pre-fix TypeError instead of naming the drift ***"
    fail=$((fail + 1))
  else
    echo "  BITES ${label}: new guard ${got}, ${n} failing test(s)${new_needle:+, naming '${new_needle}'}"
    passed=$((passed + 1))
  fi
}

# ---- the mutations, each asserted to be on disk before it is measured --------
m_rename() { # m_rename <new title>
  local name="$1"
  sed -i "s|^  \"Order forms\": \[|  \"${name}\": [|" "$FACTS"
  sed -i "s|^  \"Order forms\",\$|  \"${name}\",|" "$GUARD"
  sed -i "s|^        title: \"Order forms\",\$|        title: \"${name}\",|" "$PAGE"
  assert_on_disk "$FACTS" "\"${name}\": [" || return 1
  assert_on_disk "$GUARD" "\"${name}\"," || return 1
  assert_on_disk "$PAGE" "title: \"${name}\"," || return 1
  BASE_SEDS+=("s|^  \"Order forms\",\$|  \"${name}\",|")
}
m_delete_floor() {
  sed -i '/^  "Order forms": 3,$/d' "$FACTS"
  if grep -qF '  "Order forms": 3,' "$FACTS"; then
    echo "  *** MUTATION DID NOT APPLY: the floor line survived in $FACTS ***"; fail=$((fail + 1)); return 1
  fi
  echo "       mutation on disk: the 'Order forms' floor line is gone from $FACTS"
}
m_orphan_floor() { # m_orphan_floor <line>
  local line="$1"
  sed -i "s|^  Sales: 5,\$|  Sales: 5,\\n${line}|" "$FACTS"
  assert_on_disk "$FACTS" "$line" || return 1
}
assert_on_disk() {
  if grep -qF "$2" "$1"; then echo "       mutation on disk: $2 ($1)"; return 0; fi
  echo "  *** MUTATION DID NOT APPLY to $1: $2 ***"; fail=$((fail + 1)); return 1
}

echo "old guard (${BASE_REF} ${BASE_REV:0:12}): $(wc -l < "$OLD_GUARD") lines, predicate '${PIN_RE}'"
echo "new guard (this tree):             $(wc -l < "$GUARD") lines, predicate '${FIXED_RE}'"

# PREMISE: the base guard must be GREEN on the unmutated tree. If it is not, it is red
# for a reason that has nothing to do with this branch and no mutation can make it "miss"
# anything.
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
  exit 2
fi

echo
echo "== BASELINE (no mutation) =="
check "baseline" GREEN - GREEN -

echo
echo "== CASE 1: a prototype-named entry, renamed on every side, with its floor line DELETED =="
echo "          (the card's mutation; GREEN on the base, and the entry is left unguarded)"
m_rename "constructor" && m_delete_floor && \
  check "prototype-named entry with no floor" GREEN - RED "constructor"
restore_all

echo
echo "== CASE 2: CONTROL — the same edit with an ORDINARY name =="
echo "          (both guards RED: the escape is the prototype NAME, not the 'rename everything together' shape)"
m_rename "Order forms renamed" && m_delete_floor && \
  check "ordinary-named entry with no floor" RED "Order forms renamed" RED "Order forms renamed"
restore_all

echo
echo "== CASE 3: an ORPHAN floor line named after a prototype member =="
echo "          (constructor: 1 — its value is Function.prototype.length, so the equality check matches the member)"
m_orphan_floor "  constructor: 1," && \
  check "orphan prototype-named floor line" GREEN - RED "constructor"
restore_all

echo
echo "== CASE 4: an ORPHAN floor line with an ORDINARY name =="
echo "          (base: TypeError reading 'length'; new: the drift, named — never the TypeError)"
m_orphan_floor "  \"QA orphan\": 3," && \
  check "orphan ordinary-named floor line" RED "Cannot read properties of undefined (reading 'length')" RED "QA orphan"
restore_all

echo
echo "== CASE 5: NON-REGRESSION — the floor line deleted, nothing else changed =="
echo "          (t_a6a91844's own bite; must stay RED and keep naming the entry)"
m_delete_floor && check "a deleted floor line, no rename" RED "Order forms" RED "Order forms"
restore_all

echo
echo "== GUARD RESTORED =="
if [ "$(sha_of "$GUARD")" = "$SHA_GUARD" ] && [ "$(sha_of "$FACTS")" = "$SHA_FACTS" ] \
   && [ "$(sha_of "$PAGE")" = "$SHA_PAGE" ] && [ ! -e "$OLD_COPY" ]; then
  echo "       $GUARD, $FACTS, $PAGE all byte-identical to the pre-run bytes; no .oldcheck copy left"
else
  echo "  *** a file was NOT restored — restore it from $BAK BY HAND before doing anything else ***"
  fail=$((fail + 1))
fi

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — $passed checks: every prototype-named mutation is RED under this tree's guard and names the offender, the ordinary-named controls are RED in both columns, and no case fails by throwing the pre-fix TypeError."
else
  echo "BITEPROOF FAIL ($fail failing check(s), $passed passing)"
fi
exit "$fail"
