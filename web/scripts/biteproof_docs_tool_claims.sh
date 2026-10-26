#!/usr/bin/env bash
# Bite proof for web/src/lib/docsToolClaims.test.ts.
#
# WHAT THIS PROVES, and why it is a script rather than more assertions: a comparison of the
# guard as it stands on `origin/main` (the base this branch is rebased onto) against the
# guard in this working tree, on a mutated docs page. A test inside the guard cannot show
# that, because it can only exercise the guard's OWN rules, not the absence of them.
#
# TWO DIRECTIONS are driven here, and they are not the same claim:
#
#   * FALSE POSITIVE (card t_3926a5dd) — OLD RED, NEW GREEN: the shipped guard reports a
#     tool the docs claim and the registry does not have, for a bracketed ASIDE after the
#     word "tool"/"tools" within 60 chars. Four measured forms: the three English ones —
#     "the tool (deprecated)", "the MCP tools (overview)", the literal "tool(s)" — and a
#     KANBAN CARD ID, "these tools (t_cde2e73a) are added to the registry here", which is
#     snake_case and so reached even the strict capture (that one is skipped as a citation
#     now, and the harness drives it because the strict spelling alone did not close it).
#     The old column must also NAME the aside's word, because a red for any other reason is
#     not this defect. The harm in this direction is a red that is WRONG, not a hole, which
#     is why the old column is what has to be green in a fixed tree.
#   * GAP — OLD GREEN, NEW RED: the base guard misses a phrasing this tree catches. This is
#     the direction every earlier case in this file drove.
#   * FALSE POSITIVE, second family (card t_ceef983f, whose defect report is QA card
#     t_034ae078) — OLD RED, NEW GREEN: the loose `as (the )?X tool` capture read a
#     DETERMINER or a POSSESSIVE — their, this, your, any, the — as a claimed tool name.
#     Five sentences, each its own case, each with the old column naming the rejected word
#     (the defect is exactly that the guard called that word a tool). The fix is a CLOSED
#     class excluded BY RULE inside both loose patterns (FUNCTION_WORDS), not a longer
#     stoplist; the section drives the already-stoplisted article "a" as a control that had
#     to stay quiet in BOTH columns.
#   * SELF-BITE — the GUARD ITSELF is mutated: BARE_NAME widened to accept a hyphen must
#     redden the guard's own new footprint test. The page-mutation cases prove the fix works
#     on the phrasings QA measured; this one proves the new test catches the CLASS, which is
#     what the card is about (a test that passes on its own three literals is the defect).
#     Driven by awk on a per-run temp copy beside the real guard, .ts extension kept.
#
# THE GAP CASES THIS PROOF WAS BUILT FOR CAN NO LONGER REPRODUCE THEIR PREMISE, and they are
# not deleted for it (card t_3926a5dd): their phrasings were gaps against a guard that
# predates #819, and `origin/main` now carries that fix, so BOTH columns catch them. They are
# still the guard's coverage claims — a shape that stopped catching them would be a hole
# nobody measured — so they are driven as REGRESSION cases instead: OLD RED, NEW RED, with
# the new column naming the offender. `check` prints which direction it read, so a case whose
# premise has moved cannot pass as the other kind; a stale premise reads as a `***` line, not
# as a verdict (measured before that change: 4 such lines, `fail=1`, on the unmutated tree).
# WHICH of the two it reads now depends on WHY the premise is unavailable (card t_bfaedba2):
# when the two columns gave the SAME ANSWER (the base reader is this tree's reader), the line
# is `SAME READER … not a finding, and not a bite`, and the run ends in the capability refusal
# below; only a base that answered DIFFERENTLY and still missed the case's expectation prints
# the `***` line and counts as a failure.
#
# THE FALSE POSITIVE CASES' PREMISE HAS SINCE MOVED THE SAME WAY (card t_ceef983f): the
# parenthetical narrowing and the four asides that motivated it landed ON `origin/main`, the
# OLD column here, so those cases can no longer show "OLD RED, NEW GREEN" — the OLD column is
# GREEN too. Measured in a clean worktree at base 4a4018aa with nothing else touched: the
# landed harness exits 1 with five `***` lines, "OLD guard wanted RED, got GREEN". They are
# driven as GREEN GREEN (quiet in both columns) instead, which still bites on the tree under
# test — re-widening the shape turns the NEW column RED. The five DETERMINER/POSSESSIVE cases
# added with t_ceef983f reproduce their premise (the loose shapes untested on the base, the old
# column naming the word) at a base that still carries the LOOSE capture — which `origin/main`
# no longer is, because t_ceef983f's own fix merged there too (#1147): measured on main today,
# the five read "OLD guard wanted RED, got GREEN" against a base guard byte-identical to this
# tree's. That is the condition the capability refusal below exists for.
#
# Each case: mutate -> assert the mutation is really on disk -> run the OLD guard -> run the
# NEW guard -> restore the page byte-identically (re-checked with sha256).
#
# The OLD column is read from `origin/main`, not a pinned commit hash. A hash rots: this
# proof originally anchored on 2172312, whose guard reads `tools.go` alone and is RED on
# its own once a tool's literal moves out of that file (disable_2fa ->
# parity_issue396.go) — so the "old" column could no longer be GREEN and every case
# reported "does not reproduce". `origin/main` is the revision the comparison actually
# means ("before this branch" vs "this branch") and it moves with the base, so no hash
# has to be maintained here. It moves with the base in the OTHER direction too, which is
# what the capability refusal below handles: once a branch's own guard change has merged,
# `origin/main` IS that reader, and this file REFUSES rather than printing a verdict.
#
# THE BASE READER MUST ANSWER DIFFERENTLY FROM THIS TREE'S (card t_bfaedba2 — the same defect
# class the api sibling `api/scripts/biteproof_model_default_guard.py` fixed in t_9df01abd).
# Every case here is a comparison of TWO readers, and the whole product of the file is the
# difference between them. Nothing used to check that a difference EXISTED: on a base whose
# guard has caught up to the branch under test (`origin/main` after the branch merged — which
# is exactly what a re-run on main after #1147 is) BOTH columns run the same reader, so the
# "the base guard missed this" column is empty by construction and each case printed either
# `*** … OLD guard wanted RED, got GREEN — the case does not reproduce its premise ***` or,
# worse, a `FIXED … old guard RED naming …` line that asserts the exact opposite of the `***`
# line three lines above it (measured on main `d3621211`, 2026-10-11: five of each, then
# `BITEPROOF FAIL (fail=1)`, EXIT=1 — a reader cannot tell that from a real hole).
#
# So, AFTER the cases have run, this file requires that at least one case made the base guard
# ANSWER DIFFERENTLY from this tree's — its verdict, or the claim text it reported with the
# line number dropped (`sig_of`) — and otherwise REFUSES: exit 2, NO VERDICT, no
# `BITEPROOF FAIL`/PASS summary, naming a revision to use instead. The revision is DERIVED
# from the guard's own history at the base (the parent of a commit there that touched the
# guard — a merge-base would be wrong on a branch cut from current main, because that IS the
# caught-up revision) and RE-CHECKED BY CAPABILITY inside that run, so this file never hands
# over a base that would refuse again. It is deliberately NOT a byte-equality test: the defect
# is a reader that has CAUGHT UP, and such a reader can differ from this tree's in BYTES while
# answering identically on every case (the api sibling's t_34ab7a00 measured exactly that), so
# a byte test would accept it. Bytes are not the property — the property is that the two
# readers ANSWER differently, which is the only thing a case here can measure.
#
# A BASE THAT HAS NOT MOVED IS REFUSED BEFORE THE CASE SET RUNS (card t_8835cf9c), because the
# rule above reads its evidence off the CASES: on the base it is asked about most — a tree that
# does not change this guard at all, which after a release's own merge is `origin/main` itself
# — it spends 19 page mutations and 45 node runs (30-58s on this loaded host) to discover that
# its two columns are one program. The `SAME READER` lines that route prints are FIVE CASE
# LINES, not one per comparison: the other 14 of the 19 comparisons also read the same, but
# they print as `ok … old guard GREEN — quiet in both columns` (9) / `ok … caught by BOTH
# columns` (4) / the baseline line, because their expectation still matched and the
# premise-unavailable branch that prints `SAME READER` is never taken; `grep -c 'SAME READER'`
# over the whole output reads 7, because the end-of-run refusal itself names the marker twice.
# A base whose guard is
# BYTE-IDENTICAL to this tree's IS this reader: the same file cannot answer differently on any
# mutation, so the answer rule is guaranteed to find 0 disagreements and the refusal below is
# reached on EVERY such run. That identity is therefore refused up front: exit 2, no case, no
# verdict, and the refusal READS THE BASE GUARD'S OWN DECLARATION to name what has caught up —
# the `parenthetical/strict` entry's capture spelling, `TOOLNAME` (the strict spelling this
# tree's case set was written against) versus `BARE_NAME` (the pre-fix one), with a spelling
# the instrument cannot place named as such rather than assumed pre-fix. Identity is used as a
# SUFFICIENT condition only, ALONGSIDE and never instead of the answer rule, which still
# decides every byte-different base (both the `answer_probe` inside the derivation and the
# end-of-run refusal, and the throwaway-commit leg measured in BITE-HARNESSES.md: a base that
# differs in BYTES and answers identically is refused by the ANSWER rule, not by the fast path
# here). Measured on this host (node v26.7.0, base `origin/main` `adc2534a`, stdout and stderr
# captured separately): the default base on a tree at `origin/main` returns in a few seconds
# (3-8s) with 28 output lines (3 stdout + 25 stderr), 6 node runs, 0 cases run and
# `git status` clean, where the same tree with this fast path disabled printed those five CASE
# lines (7 string occurrences) and exited 2 after 30-58s, 141 output lines, 19 page mutations
# and 45 node runs. Timings are wall-clock on a shared host and vary with load; the LINE
# counts above are the stable part.
#
# THE DECLARATION READ IS EVIDENCE FOR THE READER, NOT THE DECIDING CONDITION, and that is
# measured rather than preferred (card t_8835cf9c). The card that filed this asked for the
# sibling's rule — refuse when the base's `parenthetical/strict` entry already carries the
# strict spelling — and on THIS family that rule refuses the base this instrument itself
# recommends: the guard at `2ba8fe02`, the `BITEPROOF_BASE` the derivation below hands over and
# re-checks, ALREADY declares `(${TOOLNAME})` there (`git show
# 2ba8fe02:web/src/lib/docsToolClaims.test.ts`, the entry's own `re:` line), because the strict
# spelling landed (#1114) BEFORE the class that base is pre-fix for (#1147) — so does the base
# every determiner branch was cut from, which means that rule would also have refused
# `t_ceef983f`'s own case set, the very branch whose fix this harness exists to prove. A
# caught-up base is a property of the READER AS A WHOLE, not of one entry, so identity of the
# whole guard is what decides and the declaration is what NAMES the cause. The read's three
# outcomes are self-checked on every run against three real fixtures built from this tree's own
# guard (the sibling's pattern), because a read that answered one spelling for everything would
# name a cause this run did not measure.
#
# The `FIXED`/`QUIET` line is gated on the same evidence (card t_bfaedba2): it may print ONLY
# when the OLD column was OBSERVED doing what it describes (verdict matched AND, where the case
# names one, the old failure named the word). Printing it from the case's static arguments is
# what produced the contradiction above — on a caught-up base the old column was GREEN with
# `pass 10 / fail 0` and named nothing, while the line read `old guard RED naming …`.
#
# THE PASS PARAGRAPH IS BUILT FROM THIS RUN'S PER-CASE EVIDENCE (card t_3dee2bae) — the last
# un-gated claim line in this file, and the defect above one level UP: the summary. Its four
# trailing assertions were literals while only the `${DISAGREE}/${CASE_NO}` clause ahead of
# them was derived, so a run that read none of them printed all four. The sharpest is the
# determiner sentence: the five cases in FALSE POSITIVE 5-9 (`possessive (their)`,
# `demonstrative (this)`, `possessive (your)`, `quantifier (any)`, `article backoff (the)`) are
# declared `check … RED GREEN`, and on a base whose guard has CAUGHT UP on that class each one prints
# `SAME READER` — which does NOT set fail=1 (only the `***` branch does) — so a run whose only
# disagreement is some OTHER two-leg case reaches fail=0 and asserted five RED/GREEN readings
# it had not made. Every sentence that names a family is now gated on that family's own
# recorded outcome (`CASE_CLASS`, written by `check` and by the inline /docs/embed case): the
# determiner sentence only when all five read FIXED_NAMED (the same evidence its `FIXED` line
# needs), the footprint sentence only when the BARE_NAME widening read RED with the footprint
# test among its failures, the "four asides and the stated gap are quiet in both columns"
# sentence only when all five read GREEN in both columns, and "every claim phrasing is still
# caught by both columns" only when all four caught-by-both cases read RED in both. A sentence
# whose evidence is missing is REPLACED — not dropped — by one that names what this run read,
# so a PASS still says what it rests on. TWO OF THE FOUR GATES ARE DEFENCE IN DEPTH RATHER
# THAN REACHABLE TODAY, stated rather than implied: the self-bite case sets fail=1 whenever it
# does not read RED (so every PASS has it), and the four caught-by-both cases can only read
# `SAME READER` on a tree whose guard stopped catching them — where their premise is
# unavailable rather than the claim true.
#
# MEASURED (this host, node v26.7.0, the pages byte-identical after every run and `git status`
# clean; the harness is run from `web/`): (1) the named base `2ba8fe02` — the run in which all
# four families ARE observed — prints a PASS paragraph BYTE-IDENTICAL to the pre-fix bytes, so
# the gating costs an honest run nothing. (2) the default base (origin/main, whose guard has
# caught up on the determiners) with ONLY the refusal call no-op'd: the pre-fix bytes printed
# `BITEPROOF PASS` 0/19 AND the determiner sentence, with five `SAME READER` lines for exactly
# those five cases; the gated bytes print instead `the five determiner/possessive slot cases
# were NOT read as RED-under-the-base/GREEN-here in this run — … (read SAME_READER) — so no
# claim about them is made here`. (3) a base that has CAUGHT UP on the determiners but DISAGREES
# ELSEWHERE: no revision of the guard on `origin/main` is one — the guard has not changed since
# #1147, so every base from #1147 on answers identically on all 19 cases and an earlier base
# disagrees on the five determiners too — so the property is reproduced with a base BUILT for
# it: `origin/main`'s tree with ONE extra claim shape in the guard that fires only on the
# /docs/embed case's mutation. At that base the five determiners read `SAME READER` while
# `/docs/embed phantom` disagrees (1/19); the pre-fix bytes print `BITEPROOF PASS` 1/19 WITH
# the five-determiner sentence, and so do the gated bytes with that one gate forced
# unconditional (`if read_as FIXED_NAMED …` -> `if true`) — so the gate is what suppresses the
# claim — while the gated bytes as shipped print the honest line instead.
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
# THE SHARED *INPUT* IS SERIALISED PER WORKTREE (card t_ed66094c). The channel fix
# removed the shared OUTPUT; the cases still mutate the two SHARED pages in place
# (`src/app/docs/chat/page.tsx` for cases 1-3 and both negative controls,
# `src/app/docs/embed/page.tsx` for case 4), and `run_guard` walks the WHOLE docs
# tree — so a second run started while the first is mid-case reads a page the first
# has already mutated, attributes that mutation to its own case, or reads a page the
# first has already restored. Reproduced, 3/3 rounds: two runs 2s apart in one
# worktree printed 33 false `***` verdict lines across 6 runs (`OLD guard wanted
# GREEN, got RED`, `NEW guard wanted GREEN, got RED`, `NEW guard RED but the failure
# does not name …`) while the exit code stayed 1. No false PASS is reachable, but a
# harness whose entire product is a trustworthy verdict cannot print four wrong
# verdict lines, and no per-run channel can fix it: the shared thing is the INPUT.
#
# The lock is taken before the first mutation and before the premise check (that
# check READS both pages, so it is just as wrong on a peer's mutation) and is held
# until this process exits. It is keyed on the worktree, so two lanes' cards never
# queue behind each other, and a contended run WAITS and says so rather than failing
# — a refusal would turn "someone else is running it" into a red harness.
#
# THE KEY IS THE FAMILY'S, NOT THIS SCRIPT'S (card t_76b3356d). The lock directory
# was once per harness (its own `jomform-biteproof-docs-tool-claims`), which
# serialised two runs of THIS script and left two DIFFERENT harnesses of the family
# overlapping — and they all mutate the SAME docs tree. Measured 3/3 rounds with
# `biteproof_docs_prose_convention.sh` started together in ONE worktree: THIS harness
# printed 6 false `***` lines against 4 for a solo run of the same bytes, including a
# FALSE `registered name: NEW guard wanted GREEN, got RED` on its own negative control
# (a control that must stay GREEN read RED). Every harness of the family now takes ONE
# file, `jomform-biteproof-docs-bite`, so any two of them serialise; the two that had
# no lock at all (comment_anchor, mcp_tool_inventory) were given the same one.
# `web/src/lib/harnessLock.test.ts` leg E drives that cross-harness property, so a
# revert to a per-script directory turns it RED.
#
# Run:  bash web/scripts/biteproof_docs_tool_claims.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD — a
# relative `$0` resolved after the `cd` names a file that is not there. Measured: that
# mistake made the child die with `cd: web/scripts: No such file or directory` and
# `bash: /biteproof_docs_tool_claims.sh: No such file or directory` (exit 127).
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
cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (card t_ed66094c) ----------------------------------
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
# runs, which is how the defect was found), and a per-profile TMPDIR would put the two
# runs in different directories and serialise nothing.
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits the
# open descriptor, so the lock is held for exactly as long as the copy that mutates
# anything lives, and it is released when the parent exits — the `trap ... EXIT` below
# releases it as surely as it restores the pages, and there is no separate unlock line
# that can be skipped.
#
# The child PROVES the lock is held before it prints a verdict (a lock nobody can
# observe as held is the same "trust the absence" shape the verdict rule was rewritten
# to remove). Two legs, both needed: a second take on the same file from a separate
# process must be REFUSED, and a fresh file in the same directory must lock FINE — or
# "refused" would mean flock is inert here, not that this run holds the lock.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs read each other's mutated pages and print" >&2
    echo "FALSE verdicts (t_ed66094c, reproduced). Install flock, or change the harness to" >&2
    echo "stop mutating the shared pages, before trusting any verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised run prints false verdicts when a peer runs here (t_ed66094c)." >&2
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
    echo "so this run is NOT serialised against a peer that mutates the same two docs pages" >&2
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
  echo "NOT serialised — a peer mutating the same two docs pages would give it false verdicts." >&2
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

# TWO-LEG BOOKKEEPING (card t_bfaedba2). Every `check` call and the /docs/embed case run the
# base reader and this tree's reader over the same mutation. Their answers are compared with
# `sig_of` (verdict + the claim text, line number dropped) and a case whose two answers DIFFER
# is a case where the "before" column means something. `DISAGREE` is the run's evidence that
# the base is a different reader at all; with 0 of them the whole comparison is a reader
# against itself and the run REFUSES at the end instead of printing a count (see
# `capability_refusal`). `CASE_NO` counts those comparisons, not the `==` section headings.
# `SAME_READER_CASES` counts the ones where the two readers agreed — reported in the summary,
# so a pass that rests on a handful of disagreeing cases says so instead of hiding the rest.
DISAGREE=0
CASE_NO=0
SAME_READER_CASES=0
FIRST_DISAGREE=''

# PER-CASE OUTCOME RECORD (card t_3dee2bae). `check` and the inline /docs/embed case write
# HERE the outcome each case actually READ this run, keyed by the case's label. The PASS
# paragraph at the end of this file is built from these records: a sentence that names a
# family of cases may print only when EVERY case of that family was observed reading what
# the sentence describes. Printing such a sentence from the case's static arguments is the
# defect t_bfaedba2 closed for the `FIXED`/`QUIET` lines, one level up — in the summary
# rather than in a case line — and it is what let this file assert the five determiner
# cases unobserved on a run whose only disagreement was some other case.
declare -A CASE_CLASS=()
# The self-bite case is not one of the two-leg cases (it compares a mutated copy of THIS
# tree's guard against this tree's), so it records its own flag rather than a class.
SELFBITE_SEEN=0

# ONE trap, carrying two properties the shipped version did not have:
#
#   * the mutated pages are restored on EVERY exit, interrupts included. Restoring was on
#     the non-error path only (`cp "$EMBED_BAK" "$EMBED"` at the end of case 4), so an
#     interrupt — or any abort between the mutation and the restore — left a mutation on
#     disk and the tree dirty (t_ff3640e7 reproduced exactly that).
#   * every temp file a run created is removed here. They are per-run `mktemp` paths, so
#     one run cannot remove another's, and no run can leave debris behind.
TMP_E=''
# The guard variant the SELF-BITE section writes (widened BARE_NAME). Declared empty here
# for the same reason as TMP_E: the trap below removes every per-run temp file even when a
# case aborts before the section that creates it.
WIDETMP=''
# The `parenthetical/strict` spelling fixtures the declaration read is self-checked against
# (card t_8835cf9c). Declared here for the same reason as WIDETMP; it is written beside the
# real guard and removed by the trap, so an abort cannot leave it in the tree.
SPTMP="$GUARD.spelling.$$.ts"
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
  rm -f "$OLDTMP" "$SKTMP" "$NOLOADTMP" "$MISSINGTMP" "$WIDETMP" "$SPTMP"
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

# sig_of <verdict> <guard-output> — the ANSWER a reader gave, for the two-leg comparison
# (card t_bfaedba2). The guard's own failure text names the offender as
# `presents "X" as a tool, but no such tool is registered`, so the signature is the VERDICT
# plus the claimed names in order, and NOTHING positional: the same claim on a different
# `file:line` is the same answer, and a renumbered assertion must not read as a reader that
# changed. That is the "claim text with the line number dropped" half of the capability rule
# the api sibling uses; the other half is the verdict.
sig_of() {
  local names
  names=$(grep -oE 'presents "[^"]+" as a tool' <<<"$2" | sort | tr '\n' ',')
  printf '%s|%s' "$1" "$names"
}

# note_two_legs <label> <old-verdict> <old-out> <new-verdict> <new-out> — records whether the
# two readers DISAGREED on this mutation, and returns 0 when they did (1 when the answers were
# identical, which is the `SAME READER` case). NOT a command substitution call site for
# run_guard (see the channel note); the counters live in the shell.
note_two_legs() {
  CASE_NO=$((CASE_NO + 1))
  if [ "$(sig_of "$2" "$3")" != "$(sig_of "$4" "$5")" ]; then
    DISAGREE=$((DISAGREE + 1))
    [ -n "$FIRST_DISAGREE" ] || FIRST_DISAGREE="$1"
    return 0
  fi
  SAME_READER_CASES=$((SAME_READER_CASES + 1))
  return 1
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

check() { # check <label> <old-want> <new-want> [must-name] [old-must-name]
  local label="$1" old_want="$2" new_want="$3" needle="${4:-}" old_needle="${5:-}"
  # The temp copy MUST keep a .ts extension: node strips types by extension, and a .mjs
  # copy of the guard dies with `SyntaxError: Unexpected token ':'` on its first type
  # annotation — which reads as a failing guard rather than a harness bug. It lives beside
  # the real guard because the guard resolves its paths from import.meta.url.
  local oldtmp="$OLDTMP"
  cp "$OLD_GUARD" "$oldtmp"
  run_guard "$oldtmp"; rm -f "$oldtmp"
  # The old column's output has to be kept: run_guard overwrites $VERDICT_OUT below, and the
  # FALSE POSITIVE cases prove the defect by grepping the OLD failure for the prose word it
  # reported. (A local, not a command substitution — run_guard inside `$( )` is a subshell
  # and its VERDICT would not come back; see the channel note at the top.)
  local old_v="$VERDICT" old_out="$VERDICT_OUT"
  [ "$old_v" = REFUSED ] && refuse "$label" "OLD"
  # BOTH legs run before either line is printed (card t_bfaedba2): whether the two readers
  # DISAGREED is what decides whether a case's unavailable premise is a `SAME READER` note or
  # a `***` failure, and that cannot be known from the old leg alone.
  run_guard "$GUARD"
  local new_v="$VERDICT" new_out="$VERDICT_OUT"
  [ "$new_v" = REFUSED ] && refuse "$label" "NEW"

  local same_reader=0
  if note_two_legs "$label" "$old_v" "$old_out" "$new_v" "$new_out"; then
    same_reader=0          # the two readers disagreed: a real "before" column
  else
    same_reader=1
  fi

  # `old_confirmed` is the ONLY thing that may license a FIXED/QUIET claim below: those two
  # lines assert a DIRECTION (the old guard was RED and named the word), and printing them
  # from the case's static arguments is how a caught-up base produced a
  # `FIXED … old guard RED naming …` line three lines after a `***` line that had just
  # measured the old column GREEN (measured on main, five times in one run — card t_bfaedba2).
  local old_confirmed=0
  local new_class=''        # what the new-column cascade below decided, applied at the end
  if [ "$old_v" != "$old_want" ]; then
    if [ "$same_reader" = 1 ]; then
      CASE_CLASS["$label"]=SAME_READER
      echo "  SAME READER ${label}: the base guard (${BASE_REF} ${BASE_REV:0:12}) answered EXACTLY as" \
           "this tree's on this mutation (${old_v}), so this case's premise (OLD ${old_want}) is not" \
           "available here — not a finding, and not a bite (the run refuses at the end unless some" \
           "case's two readers disagreed)"
    else
      CASE_CLASS["$label"]=PREMISE_MISSING
      echo "  *** ${label}: OLD guard wanted ${old_want}, got ${old_v} — the case does not reproduce its premise ***"
      grep -E '^ℹ (pass|fail)' <<<"$old_out" | head -2; fail=1
    fi
  elif [ -n "$old_needle" ] && ! grep -qF "$old_needle" <<<"$old_out"; then
    CASE_CLASS["$label"]=OLD_NAME_MISSING
    echo "  *** ${label}: OLD guard ${old_v} but its failure does not name ${old_needle} ***"
    grep -F 'presents' <<<"$old_out" | head -2; fail=1
  else
    old_confirmed=1
    # The class this case is HEADING FOR, from the case's own wants; the new-column cascade
    # below overrides it for the outcomes that only the new column can decide.
    if [ "$label" = "baseline" ]; then
      CASE_CLASS["$label"]=BASELINE
      echo "  ok   baseline: old guard ${old_v}, as expected"
    elif [ "$old_want" != "$new_want" ] && [ "$old_want" = "RED" ]; then
      CASE_CLASS["$label"]=FALSE_POSITIVE
      echo "  ok   ${label}: old guard RED — the false positive reproduces (it reported a claim nobody made)"
    elif [ "$old_want" = "$new_want" ] && [ "$old_want" = "RED" ]; then
      CASE_CLASS["$label"]=BOTH_RED
      echo "  ok   ${label}: old guard RED — caught by BOTH columns, so this claim was not narrowed away"
    elif [ "$old_want" = "$new_want" ]; then
      CASE_CLASS["$label"]=QUIET_BOTH
      echo "  ok   ${label}: old guard GREEN — quiet in both columns (a control, or a premise " \
           "that has moved — each section header says which)"
    else
      CASE_CLASS["$label"]=GAP
      echo "  ok   ${label}: old guard GREEN — the gap reproduces (it missed this)"
    fi
  fi

  if [ "$new_v" != "$new_want" ]; then
    new_class=NEW_FAIL
    echo "  *** ${label}: NEW guard wanted ${new_want}, got ${new_v} ***"
    grep -E '^ℹ (pass|fail)' <<<"$new_out" | head -2
    # WHAT the new column reported, not only that it was red: on a FALSE POSITIVE case a red
    # new column has to be the guard reading the prose as a phantom, and seeing the message
    # is what separates that from a red for an unrelated reason (a stale mutation, a broken
    # guard, an edit that took this case with it).
    grep -F 'presents' <<<"$new_out" | head -2; fail=1
  elif [ -n "$needle" ] && ! grep -qF "$needle" <<<"$new_out"; then
    new_class=NEW_NAME_MISSING
    echo "  *** ${label}: NEW guard ${new_v} but the failure does not name ${needle} ***"
    grep -F 'presents' <<<"$new_out" | head -2; fail=1
  elif [ "$new_want" = "GREEN" ] && [ "$old_confirmed" = 1 ] && [ "$old_want" = "RED" ] && [ -n "$old_needle" ]; then
    new_class=FIXED_NAMED
    echo "  FIXED ${label}: old guard RED naming ${old_needle}, new guard GREEN — the false positive is gone"
  elif [ "$new_want" = "GREEN" ] && [ "$old_confirmed" = 1 ] && [ "$old_want" = "RED" ]; then
    new_class=QUIET_FROM_RED
    echo "  QUIET ${label}: old guard RED, new guard GREEN — this form is deliberately not caught any more (stated gap)"
  elif [ "$new_want" = "GREEN" ] && [ "$old_confirmed" = 0 ]; then
    # NO DIRECTION IS CLAIMED. The new column did what the case predicts, but the old column
    # was never OBSERVED doing what the line would describe (see `old_confirmed` above), so
    # the honest line names that instead of inventing a FIXED/QUIET/BITES verdict.
    new_class=NO_DIRECTION
    echo "  no direction ${label}: new guard GREEN${needle:+, naming $needle} — the OLD column was not" \
         "confirmed RED${old_needle:+ and naming ${old_needle}}, so no FIXED/QUIET claim is made here"
  else
    echo "  BITES ${label}: new guard ${new_v}${needle:+, naming $needle}"
  fi

  # RECORD WHAT THIS CASE READ (card t_3dee2bae), for the PASS paragraph at the end of this
  # file. The FIRST cascade's terminal outcomes are decided there and are NOT overridable
  # here: a `SAME READER` note (the base reader answered EXACTLY as this tree's) is the one
  # record the summary must never lose, and the new-column cascade would otherwise relabel
  # it — the case still meets its new-column want, so it falls through to the `no direction`
  # branch and would be recorded as a case that read the new guard when in fact it never
  # read a "before" column at all.
  case "${CASE_CLASS[$label]:-}" in
    SAME_READER|PREMISE_MISSING|OLD_NAME_MISSING) : ;;
    *) CASE_CLASS["$label"]="${new_class:-${CASE_CLASS[$label]:-UNKNOWN}}" ;;
  esac
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

# ---- CAPABILITY REFUSAL (card t_bfaedba2) ----------------------------------------
#
# The rule the api sibling uses, applied to THIS file: after the cases have run, at least one
# of them must have made the BASE reader answer DIFFERENTLY from this tree's. `note_two_legs`
# counts those; with 0 of them the run compared a reader against itself and there is no
# "before" column to report on, so the run REFUSES — exit 2, no verdict, and NO
# `BITEPROOF FAIL`/PASS summary (a count there would be a statement about this instrument).

# answer_probe <rev> — 0 when the guard at <rev> ANSWERS DIFFERENTLY from this tree's guard on
# one measured probe mutation. Both readers are run on the SAME mutated page, so this is the
# same capability test the cases make, spent on ONE revision instead of the whole case set.
# The probe is a DETERMINER sentence — the shape the loose `as (the )?X tool` capture read as a
# claimed tool before FUNCTION_WORDS, and which this tree's guard stays quiet on — so a
# pre-fix reader is RED naming the word and a caught-up reader is GREEN.
#
# A REFUSED leg is never a disagreement (a candidate whose guard cannot load or that runs no
# test is not a "before" reader, it is a broken one) and the page is restored, byte-checked,
# before this function returns either way.
answer_probe() {
  local rev="$1" tmp="$OLDTMP" ok=1
  git show "${rev}:${GUARD_IN_REPO}" > "$tmp" 2>/dev/null || { rm -f "$tmp"; return 1; }
  mutate '// QA: the assistant can be used as their tool of choice.'
  grep -qF 'as their tool of choice' "$PAGE" || ok=0
  if [ "$ok" = 1 ]; then
    run_guard "$tmp"
    local probe_v="$VERDICT" probe_out="$VERDICT_OUT"
    run_guard "$GUARD"
    local tree_v="$VERDICT" tree_out="$VERDICT_OUT"
    if [ "$probe_v" = REFUSED ] || [ "$tree_v" = REFUSED ]; then
      ok=0
    elif [ "$(sig_of "$probe_v" "$probe_out")" = "$(sig_of "$tree_v" "$tree_out")" ]; then
      ok=0
    fi
  fi
  rm -f "$tmp"
  cp "$PAGE_BAK" "$PAGE"                     # silent: the trap also restores, this keeps the
  [ "$(sha256sum "$PAGE" | cut -d' ' -f1)" = "$PAGE_SHA" ] || return 1   # next probe clean
  [ "$ok" = 1 ]
}

# derive_base — walk this guard's own history AT THE BASE and RE-CHECK each candidate with
# `answer_probe`, so the revision this file hands a reader is one which the SAME RUN measured
# to be a different reader. The candidate is the PARENT of a commit at the base that touched
# the guard: a merge-base would be wrong on a branch cut from current main, because that IS the
# caught-up revision. Sets DERIVED_BASE (empty when nothing was found) and DERIVE_TRIED, and is
# shared by BOTH refusals — the identity fast path below and the end-of-run answer rule — so
# the two cannot drift into naming different revisions.
derive_base() {
  local rev cand tried=0
  DERIVED_BASE='' DERIVE_TRIED=0
  while read -r rev; do
    [ -n "$rev" ] || continue
    tried=$((tried + 1))
    [ "$tried" -gt 12 ] && break
    cand=$(git rev-parse "${rev}^" 2>/dev/null) || continue
    if [ -n "$cand" ] && git cat-file -e "${cand}:${GUARD_IN_REPO}" 2>/dev/null; then
      if answer_probe "$cand"; then DERIVED_BASE="$cand"; break; fi
    fi
  done <<<"$(git log --format=%H "$BASE_REV" -- ":(top)${GUARD_IN_REPO}" 2>/dev/null)"
  DERIVE_TRIED=$tried
}

# print_base_suggestion <cases-run> — the DERIVED (then re-checked) revision, printed for a
# reader whose run produced no verdict. `cases-run` 0 means no case was reached (the identity
# path below), and then it states NO case count rather than "0 of 0": a count this run did not
# measure must not be printed. It names no count for a revision this run did not execute.
print_base_suggestion() {
  local cases_run="$1"
  echo >&2
  if [ -n "$DERIVED_BASE" ]; then
    echo "Point BITEPROOF_BASE at the revision BEFORE this tree's change to the guard. It is" >&2
    echo "DERIVED from that guard's own history at ${BASE_REF} and RE-CHECKED BY CAPABILITY by this" >&2
    echo "run — the guard there answers DIFFERENTLY from this tree's on the determiner probe, so it" >&2
    echo "is a real \"before\" reader:" >&2
    echo "    BITEPROOF_BASE=${DERIVED_BASE} bash web/scripts/biteproof_docs_tool_claims.sh" >&2
    echo "Measured by THIS run: that probe at ${DERIVED_BASE:0:12} disagreed (the tree's guard is quiet on it," >&2
    echo "the guard there names the determinative)." >&2
  else
    echo "This run could NOT name a replacement base: none of the parents of the ${DERIVE_TRIED} commit(s)" >&2
    echo "at ${BASE_REF} that touched ${GUARD_IN_REPO} answers differently from this tree's guard on" >&2
    echo "the determiner probe. Point BITEPROOF_BASE at a revision where the guard still carried the" >&2
    echo "behaviour this tree changes; this file will not name a base it did not measure." >&2
  fi
  if [ "$cases_run" != 0 ]; then
    echo "Measured by THIS run: ${DISAGREE} of the ${cases_run} case(s) run here disagreed." >&2
  else
    echo "No case was reached by this run, so it asserts no case count." >&2
  fi
  echo "No count for any other revision is asserted here — a revision this run did not execute was" >&2
  echo "not measured." >&2
}

# capability_refusal — the ANSWER rule's refusal (see the header): 0 of the cases run here made
# the base reader answer differently, so the two columns were one reader and there is no
# verdict to print. Same derivation, same wording for the reader as before the identity fast
# path existed.
capability_refusal() {
  local cases_run="$1"
  echo >&2
  echo "REFUSING (exit 2, NO VERDICT): not one of the ${cases_run} two-leg case(s) run here made" >&2
  echo "the guard at ${BASE_REF} (${BASE_REV:0:12}) ANSWER DIFFERENTLY from this tree's — the same" >&2
  echo "verdict AND the same claim text (line number dropped) on every mutation. Both columns ran" >&2
  echo "the SAME READER, so the \"the base guard missed this\" column is empty by CONSTRUCTION and a" >&2
  echo "BITEPROOF count from this run would be a statement about this instrument, not the guard." >&2
  echo "That is what a base whose guard has CAUGHT UP to this tree looks like: ${BASE_REF} already" >&2
  echo "carries the change this tree is proving (measured: $(wc -l < "$OLD_GUARD") lines at the base," >&2
  echo "$(wc -l < "$GUARD") lines here). The lines above that read \`SAME READER\` are that artefact, not findings." >&2
  derive_base
  print_base_suggestion "$cases_run"
  exit 2
}

# ---- A CAUGHT-UP BASE IS REFUSED BEFORE ANY CASE (card t_8835cf9c) -----------------
#
# WHY this is a fast path made of a SUFFICIENT condition rather than a replacement for the
# answer rule above is argued in the header; WHAT it is made of is the three pieces below —
# the identity test, the declaration read that NAMES the cause, and the refusal — and each
# piece is self-checked on every run, because a fast path that quietly answered "identical"
# for everything would skip the whole case set and read as a capability finding.

# guard_sha <file> — the byte-identity idiom the rest of this file already checks the mutated
# pages with (sha256), so the fast path adds no tool the harness does not already need.
guard_sha() { sha256sum "$1" | cut -d' ' -f1; }

# THE IDENTITY TEST'S OWN PREMISE, asserted on every run against REAL files: the tree's guard
# against itself must read IDENTICAL, and against a copy of it carrying one more byte must read
# DIFFERENT. Without both legs, "the base is byte-identical to this tree's guard" would be true
# of every base and no case here would ever run again.
IDENTITY_PROBE=$(mktemp)
cp "$GUARD" "$IDENTITY_PROBE"
printf '%s\n' '// one more byte' >> "$IDENTITY_PROBE"
if [ "$(guard_sha "$GUARD")" != "$(guard_sha "$GUARD")" ] \
   || [ "$(guard_sha "$GUARD")" = "$(guard_sha "$IDENTITY_PROBE")" ]; then
  echo "BITEPROOF REFUSED: the byte-identity test this file decides a caught-up base with cannot" >&2
  echo "tell this tree's guard from a copy of it carrying one more byte, so \"the base is" >&2
  echo "byte-identical\" would be true of every base and the whole case set would be skipped." >&2
  rm -f "$IDENTITY_PROBE"
  exit 2
fi
rm -f "$IDENTITY_PROBE"

# parenthetical_spelling <guardfile> — what the guard's OWN `parenthetical/strict` entry
# declares its capture to be, read out of the entry's `re:` line. THREE OUTCOMES, named rather
# than collapsed (the sibling harness's rule): TOOLNAME (the strict spelling this tree's case
# set was written against), BARE_NAME (the pre-fix one), and `unplaceable` for an entry spelled
# as something this instrument cannot read — which is reported as such, never assumed pre-fix.
parenthetical_spelling() {
  local entry
  entry=$(grep -A 2 'shape: "parenthetical/strict"' "$1" | grep -oE '\$\{(TOOLNAME|BARE_NAME)\}' | head -1)
  case "$entry" in
    '${TOOLNAME}') printf 'TOOLNAME' ;;
    '${BARE_NAME}') printf 'BARE_NAME' ;;
    *) printf 'unplaceable' ;;
  esac
}

# THE READ IS SELF-CHECKED ON EVERY RUN against three REAL fixtures built from this tree's own
# guard, each with ONLY that entry's capture rewritten — the pattern the sibling
# biteproof_docs_index_render_sites.sh uses for its declaration read, and the reason the read
# is not decoration: one that answered a single spelling for every file would name a cause this
# run never measured. A fixture whose rewrite does not apply reads as the tree's own spelling
# and fails its expectation here, so a no-op rewrite cannot pass by luck.
spelling_selfcheck() {
  local spec want expect got bad=''
  for spec in TOOLNAME:TOOLNAME BARE_NAME:BARE_NAME MYSTERY_CAPTURE:unplaceable; do
    want="${spec%%:*}"; expect="${spec##*:}"
    awk -v want="$want" '
      prev ~ /shape: "parenthetical\/strict"/ { gsub(/\$\{(TOOLNAME|BARE_NAME)\}/, "${" want "}") }
      { print; prev = $0 }
    ' "$GUARD" > "$SPTMP"
    got=$(parenthetical_spelling "$SPTMP")
    [ "$got" = "$expect" ] || bad="${want}: the fixture read as ${got}, wanted ${expect}"
  done
  rm -f "$SPTMP"
  if [ -n "$bad" ]; then
    echo "BITEPROOF REFUSED: the declaration read behind this refusal is broken — ${bad}." >&2
    echo "It names the cause in the refusal below, so a read that answers one spelling for every" >&2
    echo "guard would name a cause this run did not measure. Fix parenthetical_spelling()." >&2
    exit 2
  fi
}
spelling_selfcheck

# caught_up_refusal — the base IS this tree's reader. No case is run for it (see the header),
# the cause is NAMED from the base guard's own declaration, and the replacement base is DERIVED
# and re-checked exactly as the answer rule's refusal does it.
caught_up_refusal() {
  local base_spelling tree_spelling
  base_spelling=$(parenthetical_spelling "$OLD_GUARD")
  tree_spelling=$(parenthetical_spelling "$GUARD")
  echo >&2
  echo "REFUSING (exit 2, NO VERDICT): the guard at ${BASE_REF} (${BASE_REV:0:12}) is BYTE-IDENTICAL to" >&2
  echo "this tree's guard — sha256 $(guard_sha "$OLD_GUARD") at the base and" >&2
  echo "$(guard_sha "$GUARD") here, $(wc -l < "$GUARD") lines on both sides — so both columns would run" >&2
  echo "the SAME READER: the base has CAUGHT UP with this tree and there is no \"before\" column to" >&2
  echo "compare against. That is what a tree which does not change this guard looks like — after a release" >&2
  echo "merges its own fix, that is \`origin/main\` itself. No case is run and no BITEPROOF count is" >&2
  echo "printed for it: the case set below would measure this reader against itself." >&2
  echo >&2
  echo "Read out of the guards' OWN declarations, so the cause is named rather than guessed: the" >&2
  echo "\`parenthetical/strict\` entry spells its capture ${base_spelling} at ${BASE_REF} and ${tree_spelling}" >&2
  echo "here. The strict spelling (TOOLNAME) is the one this case set was written against, and a base" >&2
  echo "carrying it ALREADY is the reader this tree proves — unless this tree changes the guard, in which" >&2
  echo "case the bytes differ, this refusal does not fire, and the cases decide." >&2
  derive_base
  print_base_suggestion 0
  exit 2
}

# THE CALL SITE: one comparison of the two guards' bytes, before the first `mutate` in this
# file. Everything below is the case set, and none of it can say anything about a base that IS
# this reader — the answer rule enforces the same refusal at the END of that set; this gets
# there without running it.
if [ "$(guard_sha "$GUARD")" = "$(guard_sha "$OLD_GUARD")" ]; then
  caught_up_refusal
fi

echo
echo "== baseline (no mutation) =="
check "baseline" GREEN GREEN

# ---- THE DEFECT THIS PROOF IS ABOUT (card t_3926a5dd) --------------------------
#
# Ordinary English that puts a lower-case word in brackets within the shape's 60-char
# window after "tool"/"tools". Each of the three was a real draft on the filed card that had
# to be reworded purely to appease the pattern, and each is the same phrase the guard's own
# test ("the parenthetical shape is strict, and the spelling is what keeps it off prose")
# asserts must stay quiet — the two instruments read the same three forms, one by running the
# shipped guard on them and one by running this tree's.
#
# The direction WAS the INVERSE of the cases below: OLD RED (it reported a claim nobody
# made), NEW GREEN, with the old column naming the prose word.
#
# THE PREMISE HAS MOVED, measured rather than assumed (card t_ceef983f): the parenthetical
# narrowing has since landed ON `origin/main`, which is the OLD column here, so that column is
# now GREEN as well. Reproduced in a clean worktree at the base with nothing else touched —
# the harness there exits 1 with this section's four lines and the stated-gap line below
# reading "OLD guard wanted RED, got GREEN — the case does not reproduce its premise". The
# cases are NOT deleted for it (the house rule for the GAP family below, whose premise moved
# the same way): a form that stopped being quiet would be a hole nobody measured, and
# `check ... GREEN GREEN` asserts the thing that matters on this tree — both columns quiet —
# so reverting the narrowing in the tree under test still turns the NEW column RED. What is
# lost is the old column NAMING a word; it no longer reports one, so there is no word to name.
echo
echo "== FALSE POSITIVE 1: a gloss in brackets after the word 'tool' ==  (premise moved: quiet in both columns)"
mutate '// QA: the tool (deprecated) is no longer listed'
assert_applied '(deprecated) is no longer listed' && {
  check "prose gloss (deprecated)" GREEN GREEN; }
restore_page

echo
echo "== FALSE POSITIVE 2: a gloss in brackets after the word 'tools' ==  (premise moved: quiet in both columns)"
mutate '// QA: This page describes the MCP tools (overview) of the assistant.'
assert_applied 'tools (overview) of the assistant' && {
  check "prose gloss (overview)" GREEN GREEN; }
restore_page

echo
echo "== FALSE POSITIVE 3: the literal English plural marker 'tool(s)' ==  (premise moved: quiet in both columns)"
mutate '// QA: words after the word "tool(s)" reads as a phantom claim'
assert_applied '"tool(s)" reads as a phantom claim' && {
  check "plural marker (s)" GREEN GREEN; }
restore_page

echo
echo "== FALSE POSITIVE 4: a KANBAN CARD ID in brackets after the word 'tools' ==  (premise moved: quiet in both columns)"
# The face the STRICT spelling alone does not close: a card id is `t_` + 8 hex, which is
# snake_case, so it reached the strict capture too (measured on the fixed tree before the
# shape skipped the spelling). A bracketed aside is exactly where a docs comment cites a
# card, and the card that filed this defect could not write its own id there.
mutate '// QA: these tools (t_cde2e73a) are added to the registry here'
assert_applied 'tools (t_cde2e73a) are added' && {
  check "card-id citation (t_cde2e73a)" GREEN GREEN; }
restore_page

echo
echo "== FALSE POSITIVE 5-9: a DETERMINER or POSSESSIVE in the loose shapes' slot (t_ceef983f) =="
# The class this harness could not drive before the fix: `as (the )?X tool` captured ANY
# lower-case identifier, and ordinary English puts a determinative or a possessive exactly
# there. Each sentence below was RED against the shipped guard (QA card t_034ae078 measured
# all five on the real page) and the old column has to NAME the word, because the defect is
# precisely that the guard called that word a tool the docs claim.
#
# The fix is a closed class excluded BY RULE inside both loose patterns (FUNCTION_WORDS),
# not a longer stoplist: the old column here reports the word and the new column is quiet.
# The determinatives are what the class is FOR; the negative control below (the article "a",
# already stoplisted) is driven so the section shows a form that was quiet in BOTH columns.
mutate '// QA: the assistant can be used as their tool of choice.'
assert_applied 'as their tool of choice' && {
  check "possessive (their)" RED GREEN '' 'presents "their" as a tool'; }
restore_page

echo
echo "== FALSE POSITIVE 6: a DEMONSTRATIVE in the loose slot =="
mutate '// QA: it behaves as this tool does.'
assert_applied 'as this tool does' && {
  check "demonstrative (this)" RED GREEN '' 'presents "this" as a tool'; }
restore_page

echo
echo "== FALSE POSITIVE 7: a POSSESSIVE PRONOUN in the loose slot =="
mutate '// QA: the page acts as your tool to reach the API.'
assert_applied 'as your tool to reach the API' && {
  check "possessive (your)" RED GREEN '' 'presents "your" as a tool'; }
restore_page

echo
echo "== FALSE POSITIVE 8: a QUANTIFIER in the loose slot =="
mutate '// QA: the same as any tool on the platform.'
assert_applied 'as any tool on the platform' && {
  check "quantifier (any)" RED GREEN '' 'presents "any" as a tool'; }
restore_page

echo
echo "== FALSE POSITIVE 9: the ARTICLE the shape has to back off from =="
# The `(?:the )?` in `as (?:the )?X tool` is optional, so the engine can spend that "the"
# AS the captured name: "works as the tool it is told to be" read as `presents "the" as a
# tool`. The class closes it (and the other spellings of `the` in the slot).
mutate '// QA: the assistant works as the tool it is told to be.'
assert_applied 'as the tool it is told to be' && {
  check "article backoff (the)" RED GREEN '' 'presents "the" as a tool'; }
restore_page

echo
echo "== NEGATIVE CONTROL: the stoplisted article 'a' was quiet in BOTH columns =="
# Already absorbed by PROSE_WORDS before the fix, so the new column must not have narrowed
# anything here. Driven so "the class did it" cannot be read as "the shapes went quiet".
mutate '// QA: described as a tool for the workspace.'
assert_applied 'as a tool for the workspace' && check "stoplisted article (a)" GREEN GREEN
restore_page

echo
echo "== SELF-BITE: widening BARE_NAME reddens the guard's OWN footprint test (t_ceef983f) =="
# The five cases above are driven against the SHIPPED guard as the OLD column, which proves
# the fix works on the phrasings QA measured. It does not prove the guard's new test CATCHES
# the class: a test that only passes on the sentences it was written for is the defect the
# card is about. So the guard itself is mutated, in a temp copy beside the real one (the
# .ts extension is load-bearing for the same reason as elsewhere in this file), with
# BARE_NAME widened to accept a hyphen — and the run must be RED in the footprint test.
#
# The hyphen is the measured widening, not a stand-in: the guard's own corpus carries
# "documented as a read-only tool", which the shipped BARE_NAME cannot spell past (it stops
# at "read"), so the widened capture reaches "read-only" and that word is in no class.
WIDETMP="$GUARD.selfbite.$$.ts"
awk '{ if ($0 == "const BARE_NAME = String.raw`[a-z][a-z0-9_]*`;") print "const BARE_NAME = String.raw`[a-z][a-z0-9_-]*`;"; else print }' "$GUARD" > "$WIDETMP"
if grep -qF 'const BARE_NAME = String.raw`[a-z][a-z0-9_-]*`;' "$WIDETMP"; then
  echo "       mutation on disk: BARE_NAME widened to [a-z][a-z0-9_-]*"
  run_guard "$WIDETMP"
  [ "$VERDICT" = REFUSED ] && refuse "BARE_NAME widened" "NEW"
  if [ "$VERDICT" = "RED" ] && grep -qF "footprint on ordinary English is a CLOSED class" <<<"$VERDICT_OUT"; then
    SELFBITE_SEEN=1
    echo '  BITES BARE_NAME widened: the footprint test is RED, so the class is pinned and a widening cannot move the cliff silently'
    grep -F 'the loose shapes reach' <<<"$VERDICT_OUT" | head -1
  else
    echo "  *** BARE_NAME widened: the guard is ${VERDICT} and the footprint test is not among the failures — the new test does not catch a widening ***"
    grep -E '^ℹ (pass|fail)' <<<"$VERDICT_OUT" | head -2; fail=1
  fi
else
  echo "  *** BARE_NAME widening DID NOT APPLY — the constant in the guard moved; re-derive this case ***"; fail=1
fi
rm -f "$WIDETMP"

echo
echo "== STATED GAP: a SINGLE-WORD parenthetical claim is deliberately not caught (t_3926a5dd) =="
# bracket position a bare English word and a name are structurally identical — see the
# guard's own note and its WHAT IT DOES NOT COVER paragraph. Driven here so the trade is a
# measured line of output rather than a sentence in a comment.
#
# Its premise moved with the four asides above, and for the same reason: the shipped guard
# used to report this phantom and `origin/main` — the OLD column — now carries the narrowing,
# so BOTH columns are quiet and the line is "quiet in both columns". Driving it as RED GREEN
# is what reddened the landed harness on the base (measured in a clean worktree at
# 4a4018aa, exit 1); it still bites on this tree, where re-widening the parenthetical shape
# would turn the NEW column RED.
mutate '// QA: the MCP tool the proposal lane pairs with (deleteall).'
assert_applied 'pairs with (deleteall)' && {
  check "single-word parenthetical not caught (stated gap)" GREEN GREEN; }
restore_page

echo
echo "== REGRESSION: the underscore-free name in the shipped phrasing is caught by BOTH =="
mutate '// QA: the assistant can delete every product as the deleteall tool.'
assert_applied 'as the deleteall tool' && {
  check "single-word phantom" RED RED 'presents "deleteall" as a tool'; }
restore_page

echo
echo "== REGRESSION: the header-comment parenthetical is caught by BOTH (strict spelling) =="
mutate '// QA: this page documents the MCP tool the proposal lane pairs with (propose_write).'
assert_applied 'pairs with (propose_write)' && {
  check "header-comment parenthetical" RED RED 'presents "propose_write" as a tool'; }
restore_page

echo
echo "== REGRESSION: the same parenthetical WRAPPED onto the next line =="
mutate '// QA: the MCP tool the proposal lane pairs with
// (propose_write), the reversible set it may prepare,'
assert_applied '(propose_write), the reversible set' && {
  check "parenthetical wrapped across lines" RED RED 'presents "propose_write" as a tool'; }
restore_page

echo
echo "== REGRESSION: the same on a DIFFERENT docs page (the walk covers the whole tree) =="
# The mutated copy goes through a per-run mktemp, not a fixed /tmp/e (the same
# host-global channel as the verdict file: two overlapping runs would clobber it
# mid-mutation and the case would then read the other run's page).
TMP_E=$(mktemp)
awk 'NR==2{print "// QA: the snippet is generated as the uploadpic tool."} {print}' "$EMBED" > "$TMP_E" && mv "$TMP_E" "$EMBED"
if grep -qF 'as the uploadpic tool' "$EMBED"; then
  echo "       mutation on disk: as the uploadpic tool"
  cp "$OLD_GUARD" "$OLDTMP"; run_guard "$OLDTMP"; rm -f "$OLDTMP"
  old_v="$VERDICT"; old_out="$VERDICT_OUT"
  [ "$old_v" = REFUSED ] && refuse "/docs/embed phantom" "OLD"
  run_guard "$GUARD"
  new_v="$VERDICT"; new_out="$VERDICT_OUT"
  [ "$new_v" = REFUSED ] && refuse "/docs/embed phantom" "NEW"
  # Same two-leg bookkeeping as `check` (card t_bfaedba2): this case is driven inline because
  # it mutates the OTHER page, and it must feed the same capability rule.
  if note_two_legs "/docs/embed phantom" "$old_v" "$old_out" "$new_v" "$new_out"; then
    embed_disagreed=1
  else
    embed_disagreed=0
  fi
  if [ "$old_v" = "RED" ] && grep -qF 'presents "uploadpic" as a tool' <<<"$old_out"; then
    CASE_CLASS["/docs/embed phantom"]=BOTH_RED
    echo '  ok   /docs/embed phantom: old guard RED — caught by BOTH columns, so the walk did not narrow'
    if [ "$new_v" = "RED" ] && grep -qF 'presents "uploadpic" as a tool' <<<"$new_out"; then
      echo '  BITES /docs/embed phantom: new guard RED, naming presents "uploadpic" as a tool'
    else CASE_CLASS["/docs/embed phantom"]=NEW_FAIL
      echo "  *** /docs/embed phantom: new guard ${new_v} (or did not name it) ***"; fail=1; fi
  elif [ "$embed_disagreed" = 0 ]; then
    CASE_CLASS["/docs/embed phantom"]=SAME_READER
    echo "  SAME READER /docs/embed phantom: the base guard (${BASE_REF} ${BASE_REV:0:12}) answered" \
         "EXACTLY as this tree's on this mutation (${old_v}), so this case's premise (OLD RED naming" \
         "the phantom) is not available here — not a finding, and not a bite"
  else
    CASE_CLASS["/docs/embed phantom"]=PREMISE_MISSING
    echo "  *** /docs/embed phantom: old guard ${old_v} (or did not name the phantom) ***"; fail=1
  fi
else echo "  *** mutation did not apply to $EMBED ***"; fail=1; fi
restore_embed

echo
echo "== NEGATIVE CONTROL: a REGISTERED name in the same phrasing must stay GREEN in both =="
mutate '// QA: the same capability exists over MCP as the list_products tool, which is registered.'
assert_applied 'as the list_products tool' && check "registered name" GREEN GREEN
restore_page

echo
echo "== NEGATIVE CONTROL: a REGISTERED name in the PARENTHETICAL form must stay GREEN in both =="
# The strict shape still fires on a bracketed snake_case name (the guard's own test pins the
# real /docs/activity site); it is quiet here only because the name is registered.
mutate '// QA: the MCP tool the proposal lane pairs with (list_products).'
assert_applied 'pairs with (list_products)' && check "registered parenthetical" GREEN GREEN
restore_page

echo
echo "== NEGATIVE CONTROL: the guard still catches real prose it was already catching =="
mutate '// QA: has no tool to call, and the one tool of its own.'
assert_applied 'has no tool to call' && check "ordinary prose stays quiet" GREEN GREEN
restore_page

echo
# ---- THE CAPABILITY RULE, BEFORE ANY VERDICT (card t_bfaedba2) --------------------
#
# `note_two_legs` counted the cases whose two readers gave DIFFERENT answers. Zero of them
# means the base is not a "before" reader at all, and every line above that reported a stale
# premise was the same reader on both sides — so this run REFUSES, exactly as the api sibling
# does: exit 2, no verdict, no `BITEPROOF FAIL`/PASS summary, and a DERIVED (then re-checked)
# replacement base named in the refusal. It runs AFTER the cases because the property it
# tests can only be read off their answers; a byte-equality fast path would be a weaker
# version of the same test and would miss a caught-up reader whose bytes differ from this
# tree's (measured on the api sibling, t_34ab7a00).
if [ "$DISAGREE" = 0 ]; then
  capability_refusal "$CASE_NO"
fi

# ---- THE PASS PARAGRAPH READS ITS OWN EVIDENCE (card t_3dee2bae) ------------------
#
# Each of the four sentences below names a FAMILY of cases, and each is printed only when
# every case of that family was OBSERVED reading what the sentence describes (`CASE_CLASS`,
# written by `check` and by the inline /docs/embed case above). A sentence whose evidence is
# absent is REPLACED — not dropped — by one that names what this run actually read, so a
# PASS still says what it rests on. The sentences were one literal string before this, so a
# run that read none of them printed all four: most sharply the determiner sentence on a
# base whose guard has CAUGHT UP on those five `check … RED GREEN` cases (each prints `SAME
# READER`, which does not set fail=1 — only the `***` branch does), where a run whose only
# disagreement is some other two-leg case reaches `fail=0` and used to assert five
# RED/GREEN readings it had not made.
DET_SLOT_CASES=( "possessive (their)" "demonstrative (this)" "possessive (your)" \
                 "quantifier (any)" "article backoff (the)" )
ASIDE_QUIET_CASES=( "prose gloss (deprecated)" "prose gloss (overview)" "plural marker (s)" \
                    "card-id citation (t_cde2e73a)" "single-word parenthetical not caught (stated gap)" )
CAUGHT_BOTH_CASES=( "single-word phantom" "header-comment parenthetical" \
                    "parenthetical wrapped across lines" "/docs/embed phantom" )

# read_as <class> <label>... — 0 when EVERY label was recorded reading <class> in THIS run.
read_as() {
  local want="$1"; shift
  local l
  for l in "$@"; do
    [ "${CASE_CLASS[$l]:-}" = "$want" ] || return 1
  done
}
# not_read_as <class> <label>... — the labels that did NOT read <class>, as
# `label (read CLASS)`, for the sentences that have to say what this run read instead.
not_read_as() {
  local want="$1"; shift
  local l out=''
  for l in "$@"; do
    [ "${CASE_CLASS[$l]:-}" = "$want" ] && continue
    out="${out}${out:+, }${l} (read ${CASE_CLASS[$l]:-nothing})"
  done
  printf '%s' "$out"
}

if read_as FIXED_NAMED "${DET_SLOT_CASES[@]}"; then
  det_sentence='The five determiner/possessive slot words were RED under the shipped guard and GREEN under this one, each with the old column naming the word it rejected'
else
  det_sentence="the five determiner/possessive slot cases were NOT read as RED-under-the-base/GREEN-here in this run — $(not_read_as FIXED_NAMED "${DET_SLOT_CASES[@]}") — so no claim about them is made here"
fi
if [ "$SELFBITE_SEEN" = 1 ]; then
  selfbite_sentence="the guard's own footprint test is RED when BARE_NAME is widened (so the new test catches the class, not just its own literals)"
else
  selfbite_sentence='the BARE_NAME widening case did not read RED in the footprint test in this run, so no claim about it is made here'
fi
if read_as QUIET_BOTH "${ASIDE_QUIET_CASES[@]}"; then
  aside_sentence='the four asides and the stated gap are quiet in both columns, their premise having moved with the narrowing that landed on the base'
else
  aside_sentence="the four asides and the stated gap were NOT all quiet in both columns in this run — $(not_read_as QUIET_BOTH "${ASIDE_QUIET_CASES[@]}")"
fi
if read_as BOTH_RED "${CAUGHT_BOTH_CASES[@]}"; then
  caught_sentence='every claim phrasing is still caught by both columns'
else
  caught_sentence="the claim phrasings were NOT all caught by both columns in this run — $(not_read_as BOTH_RED "${CAUGHT_BOTH_CASES[@]}")"
fi

if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — ${DISAGREE}/${CASE_NO} two-leg case(s) had a base reader that ANSWERED DIFFERENTLY from this tree's (first: ${FIRST_DISAGREE}), so the \"before\" column is real; ${SAME_READER_CASES} case(s) read the same in both columns. ${det_sentence}; ${selfbite_sentence}; ${aside_sentence}; and ${caught_sentence}."
else
  echo "BITEPROOF FAIL (fail=$fail) — base reader disagreed on ${DISAGREE}/${CASE_NO} two-leg case(s)"
fi
exit "$fail"
