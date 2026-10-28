#!/usr/bin/env bash
# spec-counts.sh — RE-READ the a11y/e2e spec counts GATE-INVENTORY.md quotes.
#
# WHY THIS EXISTS (card t_74e52a43). The `a11y` and `e2e` rows of
# GATE-INVENTORY.md each carry a bare typed count of the specs their gate runs,
# and both rows used to instruct the reader to "re-measure the count when the
# specs grow". It rotted anyway, because NOTHING re-read the number: the only
# enforcement was a future card noticing. Nine cards each paid a lane run for
# one number (t_9500edf3, t_df0857c6, t_07eb69c8, t_44c9a306, t_769a5cb9,
# t_c33effe4, t_212be8da, t_3839995c, t_c5c7f37a), and when this script was
# written the rot was BACK in both rows on origin/main 0aa385c0 — they claimed
# 147 tests in 16 files (`a11y`) and 274 in 54 (`e2e`) while the enumeration read
# 159 in 17 and 276 in 55 — with every gate in the set passing. The reason is
# structural: `selftest` parses that table for gate NAMES only, so a row can say
# any number at all and stay green.
#
# WHAT IT ASSERTS. For each family below it runs the SAME enumeration the gate
# itself runs — `playwright test --project=<p> --list`, which starts no server
# and no browser — parses playwright's own `Total: N tests in M files` line, and
# compares BOTH numbers against the CURRENT figure in that family's row.
#
# THE ROW CONTRACT. The CURRENT figure is the FIRST `**N tests in M files**`
# span in the row; everything after it is history ("the figure before it was
# **145 tests in 16 files**"), which is why the comparison reads the FIRST match
# and not the last. A row that carries no such span is a FAILURE, never a skip —
# a guard that reads nothing proves nothing. When you add specs, put the new
# figure first and keep the old one after it with what grew and by which card, so
# the history stays checkable.
#
# WHAT IT DOES NOT CHECK. The rows also quote each gate's own summary line
# ("2 skipped / 222 passed"). Those cannot be read without RUNNING the suites,
# so they stay prose and the `a11y`/`e2e` gates print them on every run. A figure
# this script cannot re-read must not look like one it did.
#
# COST: ~5s (measured 4.1s and 4.6s on the agent host, 2026-10-28). It needs
# `web/node_modules` for the playwright binary only (the `preflight.sh` wiring
# checks that prerequisite and REFUSES rather than skipping), and it needs no
# build, no server and no browser.
#
# USAGE: scripts/spec-counts.sh [repo-root]   (default: this script's parent dir)
# EXIT:  0 every quoted figure is current; 1 anything else, with the fix printed.
set -uo pipefail

REPO_ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
INVENTORY="$REPO_ROOT/GATE-INVENTORY.md"
PLAYWRIGHT="$REPO_ROOT/web/node_modules/.bin/playwright"

# The families, as "<gate row in GATE-INVENTORY.md>:<playwright project>". Kept as
# a literal list here rather than derived from the inventory: a row that VANISHES
# must be a failure, and a list derived from the file could not notice that.
FAMILIES="a11y:a11y e2e:feature-e2e"

if [ ! -f "$INVENTORY" ]; then
  echo "SPEC COUNTS FAIL: GATE-INVENTORY.md not found at $INVENTORY — the document this gate re-reads is missing, so there is nothing to compare against" >&2
  exit 1
fi
if [ ! -x "$PLAYWRIGHT" ]; then
  echo "SPEC COUNTS FAIL: $PLAYWRIGHT is missing — the count cannot be re-read without playwright, and reporting 'current' without reading would be a fabrication. Seed it with: cd web && npm ci" >&2
  exit 1
fi

# row_of <gate> — the table row for that gate, or empty.
row_of() {
  grep -m1 -E "^\| \`$1\` \|" "$INVENTORY" 2>/dev/null
}

# current_figure <row> — the FIRST `**N tests in M files**` span's TEXT without the
# bold markers ("159 tests in 17 files"), or empty.
# awk's match() reports the LEFTMOST match, which is the contract; a greedy sed
# (`s/.*\*\*//`) would silently read the LAST one, i.e. a historical figure.
current_figure() {
  printf '%s\n' "$1" | awk '{
    if (match($0, /\*\*[0-9]+ tests in [0-9]+ files\*\*/)) {
      print substr($0, RSTART + 2, RLENGTH - 4); exit
    }
  }'
}

# figure_tests / figure_files <"N tests in M files"> — the two integers. Kept as
# separate readers so a malformed figure can be caught by the digit guard in the
# loop instead of silently comparing garbage, and so the MESSAGES can quote the
# text playwright actually printed (a 1-test project says "1 test in 1 file").
figure_tests() { printf '%s' "${1%% test*}"; }
figure_files() { local f="${1##* in }"; printf '%s' "${f%% file*}"; }

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/spec-counts.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT

status=0
stale=""
read_ok=0
checked=""

for fam in $FAMILIES; do
  gate="${fam%%:*}"
  project="${fam##*:}"
  row="$(row_of "$gate")"
  if [ -z "$row" ]; then
    echo "SPEC COUNTS FAIL: GATE-INVENTORY.md has no row for the \`$gate\` gate — the row this gate exists to re-read is gone, so nothing here can be current" >&2
    status=1
    continue
  fi
  claim="$(current_figure "$row")"
  if [ -z "$claim" ]; then
    echo "SPEC COUNTS FAIL: the \`$gate\` row carries no \`**N tests in M files**\` figure — a guard that reads nothing proves nothing. Put the current count in the row in that exact form, first, with the history after it." >&2
    status=1
    continue
  fi
  claim_tests="$(figure_tests "$claim")"
  claim_files="$(figure_files "$claim")"
  case "${claim_tests}${claim_files}" in
    ''|*[!0-9]*)
      echo "SPEC COUNTS FAIL: the \`$gate\` row's figure parsed as tests='$claim_tests' files='$claim_files' — that is not a pair of integers, so no comparison is possible" >&2
      status=1
      continue
      ;;
  esac

  log="$tmpdir/$gate.log"
  ( cd "$REPO_ROOT/web" && "$PLAYWRIGHT" test --project="$project" --list ) >"$log" 2>&1
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "SPEC COUNTS FAIL: 'playwright test --project=$project --list' exited $rc on this tree — the enumeration did not complete, so the \`$gate\` figure cannot be re-read. Nothing below is a verdict on the count:" >&2
    sed -n '1,40p' "$log" | sed 's/^/  | /' >&2
    status=1
    continue
  fi
  listed="$(sed -nE 's/^Total: ([0-9]+ tests? in [0-9]+ files?)$/\1/p' "$log")"
  if [ -z "$listed" ]; then
    echo "SPEC COUNTS FAIL: the --list output for project '$project' carries no 'Total: N tests in M files' line, so the count could not be parsed. This is the fail-closed arm, not a pass — the output was:" >&2
    sed -n '1,40p' "$log" | sed 's/^/  | /' >&2
    status=1
    continue
  fi
  listed_tests="$(figure_tests "$listed")"
  listed_files="$(figure_files "$listed")"
  read_ok=$((read_ok + 1))
  checked="${checked}${gate}=${listed_tests}/${listed_files}; "

  if [ "$claim_tests" = "$listed_tests" ] && [ "$claim_files" = "$listed_files" ]; then
    echo "speccounts: \`$gate\` row says **$claim** and 'playwright test --project=$project --list' on THIS tree reads $listed — current"
  else
    stale="${stale}  \`$gate\`: the row says **$claim**; --list reads **$listed**\\n"
    status=1
  fi
done

# A run that compared nothing must never look green: the loop reads at least two
# families, so zero successful reads means the whole mechanism is broken.
if [ "$read_ok" -eq 0 ]; then
  echo "SPEC COUNTS FAIL: not one family's count could be read on this tree ($checked) — a run that compares nothing proves nothing" >&2
  exit 1
fi

if [ "$status" -ne 0 ]; then
  echo "" >&2
  echo "SPEC COUNTS STALE — GATE-INVENTORY.md quotes a spec count that is not what this tree runs:" >&2
  printf '%b' "$stale" >&2
  echo "" >&2
  echo "Fix the CURRENT figure in each row (the FIRST \`**N tests in M files**\` span; the later ones" >&2
  echo "are history and must stay), say what grew and by which card, and re-run THIS gate:" >&2
  echo "  ./scripts/preflight.sh --only speccounts" >&2
  exit 1
fi

echo "speccounts: every spec count GATE-INVENTORY.md quotes is what this tree runs (${checked%; })"
exit 0
