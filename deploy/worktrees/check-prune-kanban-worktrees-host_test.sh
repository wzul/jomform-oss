#!/usr/bin/env bash
#
# check-prune-kanban-worktrees-host_test.sh — the `prunesync` gate's harness.
#
# WHY
#   `deploy/worktrees/check-prune-kanban-worktrees-host.sh` decides whether the pruner
#   the HOST RUNS is the pruner the repo MERGED, and the `prunesync` preflight gate
#   runs it on every lane's tree. A checker nobody has seen go red is not evidence, so
#   this harness drives it against fixtures built from the REAL drift pairs of card
#   t_d9cf1ddd — the host copies that were actually running while the merged revision
#   had moved on — and asserts the outcome of each run:
#
#     * the 2026-10-08 event, VERBATIM: the host pruner is
#       `git show c30aa8cf:deploy/worktrees/prune-kanban-worktrees.sh`
#       (sha256 e3137b91f59444ab5489eeefa11e85a6ce967ff116824f84eba907b9c75f7cd6 — the
#       revision the card measured on the host, which did NOT carry the live-process
#       guard `has_process_matching`) while the merged revision does. The run must exit
#       1, name the pruner, print BOTH sha256s, and show the missing guard by name in
#       the diff it prints. THIS IS THE ARM THE CARD ASKS FOR: it fails on the exact
#       drift that was live on the host.
#     * the 2026-10-10 event, the suite half: the host suite is
#       `git show d5bf0f04:deploy/worktrees/prune-kanban-worktrees_test.sh`
#       (sha256 620fbd5bd774700543ebbf4ab2fa6a405093ca791d8bf9e4424f8fff21a3ffb7) and
#       the pruner matches — a cosmetic drift is still a drift, and the run names the
#       SUITE (not the pruner) as the one that moved.
#     * the CONTROL that keeps the red attributable: the SAME host bytes, compared
#       against their OWN revision (`--ref old-pruner`), read byte-identical. Without
#       it, "the drifted pair goes red" could hold for a reason that has nothing to do
#       with the drift (an unreadable fixture, a broken git read).
#     * the verdicts that must NOT read green: an EMPTY host copy, a job that resolves
#       to a file that is GONE, a reference revision that does not resolve, a ref that
#       carries no such file, and a machine with no pruner at all (which is GREEN, but
#       only with a NOTE that says it compared 0 pairs — a check that can pass on the
#       absence of a signal would be worse than none).
#     * it is READ-ONLY: the sha256 of both host files is taken before and after the
#       drifted run. A checker that "fixed" the host inside a lane's gate run would
#       install unmerged bytes on a destructive nightly job.
#     * the compared set is DERIVED from the installer, not typed into the checker:
#       the sabotage arm rewrites the installer COPY's target list and requires the
#       checker to follow it — a file the installer starts putting on the host cannot
#       become a file nobody compares.
#     * the repair the drift verdict names (`--from-ref <rev>`) is exercised here: it
#       installs the REF's bytes and NOT the working tree's (the sandbox tree is made
#       dirty first, so a fallback to the tree's copy is red), it refuses a ref that
#       does not resolve / carries no such file / is empty, and the full proof path
#       (the revision's own suite, from the installed location) exits 0.
#     * AND THE ARMS THEMSELVES ARE DRIVEN. The last section neuters three of the
#       checker's OWN branches on a copy — the byte comparison, the EMPTY-file refusal,
#       the no-pruner-on-this-machine branch — asserting by COUNT that the edit landed
#       (an anchor that matched 0 or 2+ times would leave the shipped file in place and
#       measure nothing), requiring that arm's outcome or message to change, and then
#       restoring the copy and requiring it to come back. A green arm nobody has seen
#       fail is a tautology that looks exactly like a guard that works.
#
#   JF_PRUNESYNC_CHECK / JF_PRUNESYNC_INSTALLER point the sandbox at mutated copies of
#   either shipped script (the pruner suite's JF_PRUNE_SCRIPT, same idea), so a
#   whole-file sabotage sweep can be run against this harness without editing the tree.
#
#   The fixtures are built from the LIVE repo's history (the two revision names above),
#   so the bytes are the ones that really ran. Their sha256 is asserted as a PREMISE
#   before any verdict is read — a fixture that quietly stopped being the drift pair
#   would make every arm below prove nothing. A clone that does not carry those
#   commits FAILS loudly here rather than skipping the arm.
#
#   Entirely offline: a throwaway git repo with its own bare `origin` (so the default
#   `origin/main` reference is real), a fake Hermes home with a jobs.json, and copies
#   of the shipped scripts. No host, no cron, no board, no network. The real
#   /root/.hermes is never read or written: PRUNE_HERMES_HOME is exported to the
#   sandbox so even an arm that forgot its flag cannot reach it.
#
# Run:  bash deploy/worktrees/check-prune-kanban-worktrees-host_test.sh
# Exit: 0 when every arm holds, 1 otherwise.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIVE_ROOT="$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null)"
[ -n "$LIVE_ROOT" ] || { echo "FAIL: not inside a git repository (looked up from $HERE)" >&2; exit 1; }
LIVE_WT="$LIVE_ROOT/deploy/worktrees"

# The two revisions the card measured on the host. Both are merged commits in this
# repo's own history; a shallow or trimmed clone fails the premise checks below.
OLD_PRUNER_REV="c30aa8cf"     # 2026-10-08: no `has_process_matching`
OLD_SUITE_REV="d5bf0f04"      # 2026-10-10: the suite before #1066

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # desc expected actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (want '$2', got '$3')"; fi
}
contains() { # desc haystack needle
  # A here-string, NOT `printf '%s' "$2" | grep -q`: under `set -uo pipefail` grep -q
  # exits at the first match, the writer dies of SIGPIPE (141) and pipefail reports
  # the pipeline as failed — a MATCH would read as a miss.
  if grep -qF -- "$3" <<<"$2"; then ok "$1"
  else bad "$1 (missing '$3' in: $(clip "$2"))"; fi
}
# clip: a haystack as one line for a failure message, HEAD **and TAIL**.
#
# WHY NOT `cut -c1-600` (card t_e89e95c7). The installer sends the failing proof's
# `tail -20` to stderr and `run_installer` captures stdout+stderr together, so the
# reason IS in the haystack — at the very END of it. A head-only cut printed the arm's
# verdict (`missing 'installed copy verified:'`) and the first 600 characters of
# preamble, i.e. everything except the one thing the red was asking for. Measured on
# that card's red: the message stopped at `re-running the self-test against the
# installed copy…`, exactly where the installer starts explaining itself.
clip() { # <text> [<head chars>] [<tail chars>]
  local flat head tail
  flat="$(printf '%s' "$1" | tr '\n' '|')"
  head="${2:-300}"; tail="${3:-1200}"
  if [ "${#flat}" -le $(( head + tail )) ]; then printf '%s' "$flat"; return 0; fi
  if [ "$tail" -le 0 ]; then printf '%s…[%d chars omitted]' "${flat:0:head}" \
    "$(( ${#flat} - head ))"; return 0; fi
  printf '%s…[%d chars omitted]…%s' "${flat:0:head}" \
    "$(( ${#flat} - head - tail ))" "${flat: -tail}"
}
count() { # desc haystack needle expected
  local n
  n="$(grep -cF -- "$3" <<<"$2")"
  check "$1" "$4" "$n"
}
sha() { sha256sum "$1" | awk '{print $1}'; }

SB="$(mktemp -d "${TMPDIR:-/tmp}/jf-prunesync-test.XXXXXX")"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

# EVERY installer/checker run in this harness is pointed at the sandbox Hermes home
# even when an arm forgets the flag: the scripts' own default is /root/.hermes, and a
# typo here would rewrite the REAL host's pruner.
export PRUNE_HERMES_HOME="$SB/host"

# The scripts under test. Both paths are overridable so this harness's own BITE can be
# proven the way the pruner suite proves its own with JF_PRUNE_SCRIPT: point either at a
# mutated COPY and the sandbox is built from that copy, so a neutered guard must show up
# as a red arm here.
CHECK_SRC="${JF_PRUNESYNC_CHECK:-$HERE/check-prune-kanban-worktrees-host.sh}"
INSTALLER_SRC="${JF_PRUNESYNC_INSTALLER:-$HERE/install-prune-kanban-worktrees.sh}"

REPO="$SB/repo"
ORIGIN="$SB/origin.git"
HOST="$SB/host"
FIX="$SB/fixtures"
mkdir -p "$REPO/deploy/worktrees" "$FIX" "$HOST/scripts" "$HOST/cron"

# ─── the sandbox repository, built from the SHIPPED scripts ──────────────────
# A real bare `origin` (not a stub), because the checker's DEFAULT reference is
# `origin/main` and an arm that could not resolve it would be testing the refusal
# instead of the verdict.
git init -q --bare "$ORIGIN"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email t@t
git -C "$REPO" config user.name t
for f in prune-kanban-worktrees.sh prune-kanban-worktrees_test.sh; do
  cp "$LIVE_WT/$f" "$REPO/deploy/worktrees/$f"
done
cp "$CHECK_SRC"     "$REPO/deploy/worktrees/check-prune-kanban-worktrees-host.sh"
cp "$INSTALLER_SRC" "$REPO/deploy/worktrees/install-prune-kanban-worktrees.sh"
git -C "$REPO" add -A
git -C "$REPO" commit -q -m base
git -C "$REPO" remote add origin "$ORIGIN"
git -C "$REPO" push -q -u origin main
git -C "$REPO" fetch -q origin

CHECK="$REPO/deploy/worktrees/check-prune-kanban-worktrees-host.sh"
INSTALLER_LIVE="$REPO/deploy/worktrees/install-prune-kanban-worktrees.sh"
cp "$INSTALLER_LIVE" "$SB/installer.keep"

run_check() { CHECK_OUT="$(bash "$CHECK" "$@" 2>&1)"; CHECK_RC=$?; }
run_installer() { INST_OUT="$(bash "$INSTALLER_LIVE" "$@" 2>&1)"; INST_RC=$?; }
# dump_installer_output: the installer's OWN last lines, for a red whose meaning is in
# the installer's explanation rather than in its exit code (card t_e89e95c7).
#
# The capture is already complete — `run_installer` merges stderr into INST_OUT and the
# installer sends the failing proof's `tail -20` there — so this is a DISPLAY function,
# not a capture fix: without it an arm whose installed-copy proof failed printed
# `want '0', got '1'` plus a head-only clip of the preamble, and the one place the
# failure explained itself was past the cut. Called on every non-zero installer exit.
# It prints and nothing else (no counters, no globals), so a caller may run it inside
# a `$(…)` — which is how the diagnostic's own control arm reads it.
dump_installer_output() { # [<lines>] -> installer output on stdout
  local n="${1:-25}"
  printf '    ---- installer output, last %s line(s) ----\n' "$n"
  printf '%s\n' "$INST_OUT" | tail -n "$n" | sed 's/^/    | /'
  printf '    ---- end installer output ----\n'
}

# ─── the drift fixtures, taken from the LIVE repo's history ──────────────────
fix_from_live() { # <rev> <repo path> <dest> -> 0 when the bytes were taken
  if git -C "$LIVE_ROOT" cat-file -e "$1:$2" 2>/dev/null; then
    git -C "$LIVE_ROOT" show "$1:$2" >"$3"
    return 0
  fi
  return 1
}

FIX_PRUNER_1008="$FIX/pruner.old-1008.sh"
FIX_SUITE_1010="$FIX/suite.old-1010.sh"
if fix_from_live "$OLD_PRUNER_REV" deploy/worktrees/prune-kanban-worktrees.sh "$FIX_PRUNER_1008"; then
  ok "the 2026-10-08 fixture was taken from $OLD_PRUNER_REV in this clone"
else
  bad "the 2026-10-08 fixture could not be taken: this clone does not carry $OLD_PRUNER_REV:deploy/worktrees/prune-kanban-worktrees.sh (a shallow or trimmed clone). The drift pair the card measured cannot be built, and a substitute would prove nothing."
fi
if fix_from_live "$OLD_SUITE_REV" deploy/worktrees/prune-kanban-worktrees_test.sh "$FIX_SUITE_1010"; then
  ok "the 2026-10-10 fixture was taken from $OLD_SUITE_REV in this clone"
else
  bad "the 2026-10-10 fixture could not be taken: this clone does not carry $OLD_SUITE_REV:deploy/worktrees/prune-kanban-worktrees_test.sh"
fi

# Branch `old-pruner`: the merged suite with the 2026-10-08 pruner, i.e. "this is what
# origin/main said when the host was last installed". It is what the CONTROL compares
# the same host bytes against.
if [ -f "$FIX_PRUNER_1008" ]; then
  git -C "$REPO" checkout -q -b old-pruner main
  cp "$FIX_PRUNER_1008" "$REPO/deploy/worktrees/prune-kanban-worktrees.sh"
  git -C "$REPO" commit -q -am "the 2026-10-08 host revision"
  git -C "$REPO" checkout -q main
fi
# Branch `old-suite`: the merged pruner with the 2026-10-10 suite.
if [ -f "$FIX_SUITE_1010" ]; then
  git -C "$REPO" checkout -q -b old-suite main
  cp "$FIX_SUITE_1010" "$REPO/deploy/worktrees/prune-kanban-worktrees_test.sh"
  git -C "$REPO" commit -q -am "the 2026-10-10 host suite"
  git -C "$REPO" checkout -q main
fi
# Branch `no-tools`: a revision that carries no deploy/worktrees/<name> at all.
git -C "$REPO" checkout -q -b no-tools main
git -C "$REPO" rm -q -r deploy/worktrees
git -C "$REPO" commit -q -m "no deploy/worktrees"
git -C "$REPO" checkout -q main

git -C "$REPO" show main:deploy/worktrees/prune-kanban-worktrees.sh       >"$FIX/pruner.ref.sh"
git -C "$REPO" show main:deploy/worktrees/prune-kanban-worktrees_test.sh  >"$FIX/suite.ref.sh"

jobs_armed() {
  cat >"$HOST/cron/jobs.json" <<'JSON'
{
  "jobs": [
    {
      "id": "4851fec5bcf4",
      "name": "prune-kanban-worktrees",
      "script": "prune-kanban-worktrees.sh",
      "no_agent": true,
      "enabled": true,
      "state": "scheduled",
      "schedule": { "display": "0 4 * * *" }
    }
  ]
}
JSON
}
plant_ref() { # the host holds exactly the merged revision's bytes
  cp "$FIX/pruner.ref.sh" "$HOST/scripts/prune-kanban-worktrees.sh"
  cp "$FIX/suite.ref.sh"  "$HOST/scripts/prune-kanban-worktrees_test.sh"
}

echo "check-prune-kanban-worktrees-host self-test (sandbox $SB)"
echo "--- the premise: the fixtures ARE the drift pairs the card measured ---"
check "the 2026-10-08 fixture is byte-for-byte the file that was RUNNING on the host (sha256 e3137b91…)" \
  "e3137b91f59444ab5489eeefa11e85a6ce967ff116824f84eba907b9c75f7cd6" "$(sha "$FIX_PRUNER_1008")"
check "the 2026-10-10 fixture is byte-for-byte the suite that was RUNNING on the host (sha256 620fbd5b…)" \
  "620fbd5bd774700543ebbf4ab2fa6a405093ca791d8bf9e4424f8fff21a3ffb7" "$(sha "$FIX_SUITE_1010")"
check "the 2026-10-08 fixture really does NOT carry the live-process guard" \
  "0" "$(grep -c has_process_matching "$FIX_PRUNER_1008")"
check "...while the merged revision does (that difference IS the live-code drift)" \
  "yes" "$(grep -qF has_process_matching "$FIX/pruner.ref.sh" && echo yes || echo no)"
check "the 2026-10-10 fixture suite differs from the merged suite" \
  "different" "$(cmp -s "$FIX_SUITE_1010" "$FIX/suite.ref.sh" && echo same || echo different)"
check "the sandbox's default reference resolves to the sandbox's own main" \
  "$(git -C "$REPO" rev-parse main)" "$(git -C "$REPO" rev-parse origin/main)"

echo "--- a host that IS the merged revision reads green, on the DEFAULT reference ---"
jobs_armed
plant_ref
run_check
check "a host holding the merged bytes exits 0" "0" "$CHECK_RC"
contains "...naming the default reference and the commit it resolved to" \
  "$CHECK_OUT" "ref   origin/main = $(git -C "$REPO" rev-parse main | cut -c1-12)"
count "...reporting both installed files as compared" "$CHECK_OUT" "pair(s) compared against origin/main" 1
count "...one OK line per pair" "$CHECK_OUT" "byte-identical to origin/main:deploy/worktrees/" 2
count "...and nothing in the output that reads as a failure" "$CHECK_OUT" "DRIFT" 0
count "...nor as a skip of a pair it could not judge" "$CHECK_OUT" "could not be judged" 0

echo "--- the 2026-10-08 event VERBATIM: the running pruner predates the guard ---"
plant_ref
cp "$FIX_PRUNER_1008" "$HOST/scripts/prune-kanban-worktrees.sh"
before_pruner="$(sha "$HOST/scripts/prune-kanban-worktrees.sh")"
before_suite="$(sha "$HOST/scripts/prune-kanban-worktrees_test.sh")"
run_check
check "the drift exits 1" "1" "$CHECK_RC"
contains "...naming the pruner as the file that moved" "$CHECK_OUT" "DRIFT prune-kanban-worktrees.sh"
contains "...printing the sha256 of the copy that RUNS (the card's e3137b91…)" "$CHECK_OUT" "host sha256 $before_pruner"
contains "...and the sha256 of the revision the branch MERGED" "$CHECK_OUT" "ref  sha256 $(sha "$FIX/pruner.ref.sh")"
contains "...showing the missing live-process guard BY NAME in the diff" "$CHECK_OUT" "has_process_matching"
contains "...while the suite pair still reads OK (the red is the pruner's, not a blanket)" \
  "$CHECK_OUT" "OK    prune-kanban-worktrees_test.sh — byte-identical to origin/main:deploy/worktrees/"
count "...and BOTH pairs were compared rather than one" "$CHECK_OUT" "of 2 target(s) (2 compared) against origin/main" 1
contains "...and it names the revision-exact repair" "$CHECK_OUT" "install-prune-kanban-worktrees.sh --from-ref origin/main"
host_after="$(sha "$HOST/scripts/prune-kanban-worktrees.sh")"
suite_after="$(sha "$HOST/scripts/prune-kanban-worktrees_test.sh")"
check "the drifted run is READ-ONLY: the host pruner is byte-identical afterwards" "$before_pruner" "$host_after"
check "...and so is the host suite" "$before_suite" "$suite_after"

echo "--- the control: the same host bytes against their OWN revision read green ---"
run_check --ref old-pruner
check "the 2026-10-08 bytes compared against the 2026-10-08 revision exit 0" "0" "$CHECK_RC"
contains "...byte-identical, so the red above is attributable to the DRIFT and not to a broken fixture" \
  "$CHECK_OUT" "byte-identical to old-pruner:deploy/worktrees/prune-kanban-worktrees.sh"

echo "--- the 2026-10-10 event: the SUITE alone is stale ---"
plant_ref
cp "$FIX_SUITE_1010" "$HOST/scripts/prune-kanban-worktrees_test.sh"
run_check
check "a suite-only drift exits 1" "1" "$CHECK_RC"
contains "...naming the SUITE as the file that moved" "$CHECK_OUT" "DRIFT prune-kanban-worktrees_test.sh"
contains "...printing the sha256 of the stale suite (the card's 620fbd5b…)" \
  "$CHECK_OUT" "host sha256 620fbd5bd774700543ebbf4ab2fa6a405093ca791d8bf9e4424f8fff21a3ffb7"
contains "...while the pruner pair still reads OK" \
  "$CHECK_OUT" "OK    prune-kanban-worktrees.sh — byte-identical to origin/main:deploy/worktrees/"

echo "--- the verdicts that must NOT read green ---"
# No pruner on this machine at all: GREEN, but as a NOTE that compared nothing.
mkdir -p "$SB/nohost"
run_check --hermes-home "$SB/nohost"
check "a machine with no installed file and no job pointing at one exits 0" "0" "$CHECK_RC"
contains "...saying no pruner is on this machine" "$CHECK_OUT" "no pruner on this machine"
contains "...and that nothing was compared" "$CHECK_OUT" "0 pair(s) compared, 0 drifted"
count "...and NOT claiming a byte-identical comparison it never made" "$CHECK_OUT" "byte-identical" 0

# A job that resolves to a file that is gone: the cron job would run NOTHING.
mkdir -p "$SB/armed/scripts" "$SB/armed/cron"
cp "$HOST/cron/jobs.json" "$SB/armed/cron/jobs.json"
run_check --hermes-home "$SB/armed"
check "a job that resolves to a MISSING script exits 1 (not the no-host NOTE)" "1" "$CHECK_RC"
contains "...naming the absent path" "$CHECK_OUT" "$SB/armed/scripts/prune-kanban-worktrees.sh is MISSING"

# An empty host copy: an empty read is not agreement.
plant_ref
printf '' >"$HOST/scripts/prune-kanban-worktrees.sh"
run_check
check "an EMPTY host copy exits 1" "1" "$CHECK_RC"
contains "...refusing to read an empty file as agreement" "$CHECK_OUT" "reads as EMPTY"

# A reference that cannot be resolved, and one that carries no such file.
plant_ref
run_check --ref no-such-ref
check "an unresolvable reference exits 1 (the check cannot name what should be running)" "1" "$CHECK_RC"
contains "...naming the revision" "$CHECK_OUT" "'no-such-ref' does not resolve"
run_check --ref no-tools
check "a reference that carries no deploy/worktrees/<name> exits 1" "1" "$CHECK_RC"
contains "...naming the missing path in the ref" "$CHECK_OUT" "carries no deploy/worktrees/prune-kanban-worktrees.sh"

# Wrong invocations are 2, never a verdict.
run_check --bogus
check "an unknown argument is refused with 2" "2" "$CHECK_RC"
run_check --ref
check "--ref with no value is refused with 2" "2" "$CHECK_RC"
run_check --hermes-home ""
check "--hermes-home with an empty value is refused with 2" "2" "$CHECK_RC"

# No installer: the installed-file set cannot be read, so nothing can be judged.
mv "$INSTALLER_LIVE" "$SB/installer.hold"
run_check
check "a missing installer exits 1 rather than comparing a set it cannot read" "1" "$CHECK_RC"
contains "...saying the set comes from the installer" "$CHECK_OUT" "no installer at"
mv "$SB/installer.hold" "$INSTALLER_LIVE"

echo "--- the compared set is READ from the installer, not typed into the checker ---"
targets="$(bash "$INSTALLER_LIVE" --print-targets)"
check "the installer publishes exactly the two files it installs" "2" "$(grep -c . <<<"$targets")"
contains "...the pruner" "$targets" "prune-kanban-worktrees.sh"
contains "...and its suite" "$targets" "prune-kanban-worktrees_test.sh"
# The sabotage: the sandbox repo's installer gets a different target list. The checker
# must follow the list it READS, or a file the installer starts putting on the host
# becomes a file nobody compares. (The checker resolves the installer next to itself,
# so the sabotage has to be at that exact path; the copy kept above restores it.)
sed -i 's@^TARGET_NAMES=.*$@TARGET_NAMES=("install-prune-kanban-worktrees.sh")@' "$INSTALLER_LIVE"
check "the sabotage APPLIED (the copy's list really changed)" \
  "install-prune-kanban-worktrees.sh" "$(bash "$INSTALLER_LIVE" --print-targets | sed -n '1p')"
plant_ref
printf 'not the installer at all\n' >"$HOST/scripts/install-prune-kanban-worktrees.sh"
run_check
check "the checker follows the INSTALLER's list (the sabotaged name is what it compares)" "1" "$CHECK_RC"
contains "...reporting DRIFT on the name the installer published" "$CHECK_OUT" "DRIFT install-prune-kanban-worktrees.sh"
rm -f "$HOST/scripts/install-prune-kanban-worktrees.sh"
cp "$SB/installer.keep" "$INSTALLER_LIVE"
check "...and the installer copy is restored" \
  "2" "$(bash "$INSTALLER_LIVE" --print-targets | grep -c .)"

echo "--- the repair the drift verdict names: --from-ref installs the REF's bytes ---"
printf '\n# a lane edit that is not merged\n' >>"$REPO/deploy/worktrees/prune-kanban-worktrees.sh"
check "the sandbox tree really is dirty (its pruner differs from the ref)" \
  "different" "$(cmp -s "$REPO/deploy/worktrees/prune-kanban-worktrees.sh" "$FIX/pruner.ref.sh" && echo same || echo different)"
mkdir -p "$SB/repair-a"
run_installer --from-ref main --hermes-home "$SB/repair-a" --skip-proof
check "--from-ref main exits 0" "0" "$INST_RC"
check "...installing the REF's pruner bytes" \
  "$(sha "$FIX/pruner.ref.sh")" "$(sha "$SB/repair-a/scripts/prune-kanban-worktrees.sh")"
check "...and NOT the dirty working tree's copy (this is what makes the repair exact)" \
  "different" "$(cmp -s "$SB/repair-a/scripts/prune-kanban-worktrees.sh" \
                            "$REPO/deploy/worktrees/prune-kanban-worktrees.sh" && echo same || echo different)"
check "...and the REF's suite bytes" \
  "$(sha "$FIX/suite.ref.sh")" "$(sha "$SB/repair-a/scripts/prune-kanban-worktrees_test.sh")"
git -C "$REPO" checkout -q -- deploy/worktrees/prune-kanban-worktrees.sh

run_installer --from-ref no-such-ref --hermes-home "$SB/repair-b" --skip-proof
check "a ref that does not resolve is REFUSED (rc 1)" "1" "$INST_RC"
contains "...naming the ref" "$INST_OUT" "does not resolve to a commit"
check "...and it installed NOTHING" "absent" "$( [ -e "$SB/repair-b/scripts" ] && echo present || echo absent )"
run_installer --from-ref no-tools --hermes-home "$SB/repair-c" --skip-proof
check "a ref carrying no deploy/worktrees/<name> is REFUSED" "1" "$INST_RC"
contains "...naming the path it could not read" "$INST_OUT" "carries no deploy/worktrees/prune-kanban-worktrees.sh"
check "...and it installed NOTHING" "absent" "$( [ -e "$SB/repair-c/scripts" ] && echo present || echo absent )"
run_installer --from-ref "" --hermes-home "$SB/repair-d" --skip-proof
check "an EMPTY --from-ref is REFUSED rather than silently installing this tree's copies" "1" "$INST_RC"
contains "...saying exactly that" "$INST_OUT" "an empty value would install THIS tree's copies"

# The FULL proof path, which is what the repair actually runs: the revision's own
# suite, run once on the source and again from the INSTALLED location.
mkdir -p "$SB/repair-e"
run_installer --from-ref main --hermes-home "$SB/repair-e"
check "the full repair (with proofs) exits 0" "0" "$INST_RC"
# A red here is the one verdict in this harness that costs a full 45-minute gate run to
# re-read, so the installer's own account of it is printed BEFORE the assertions that
# read it: a failing INSTALLED-copy proof explains itself in the last lines of the
# installer's output, and nothing else in this file may cut them off.
[ "$INST_RC" -eq 0 ] || dump_installer_output
contains "...having verified the extracted revision" "$INST_OUT" "source verified:"
contains "...and re-verified the INSTALLED copy" "$INST_OUT" "installed copy verified:"
check "...with the installed pruner still the REF's bytes" \
  "$(sha "$FIX/pruner.ref.sh")" "$(sha "$SB/repair-e/scripts/prune-kanban-worktrees.sh")"

echo "--- the diagnostic: a failing installed copy NAMES ITS CAUSE, not just its exit code ---"
# CARD t_e89e95c7, DRIVEN. The installed-copy self-test failed once on gate inputs
# byte-identical to origin/main, and the harness could not say why: the failing proof
# explains itself in the installer's LAST lines, and the arm's message showed the FIRST
# 600 characters — so the red named its verdict and nothing else. The capture was never
# the defect (`run_installer` merges stderr, where the installer sends that `tail -20`);
# the DISPLAY was, and that is what this arm pins. A sabotaged installer copy deletes the
# installed suite immediately before step 3, so the installed proof fails for a NAMED
# reason, and the dump the arm now prints must carry that reason.
cp "$INSTALLER_LIVE" "$SB/installer.keep.diag"
mkdir -p "$SB/repair-f"
awk -v inject='  rm -f "$DEST_DIR/$SUITE_NAME"' '
  { print }
  /SKIP_PROOF.*= 0/ && !done { print inject; done = 1 }
' "$INSTALLER_LIVE" >"$SB/installer.diag"
cp "$SB/installer.diag" "$INSTALLER_LIVE"
check "S4 premise: the sabotage landed (the installed suite is removed just before step 3)" \
  "1" "$(grep -cF 'rm -f "$DEST_DIR/$SUITE_NAME"' "$INSTALLER_LIVE")"
run_installer --from-ref main --hermes-home "$SB/repair-f"
check "S4 the sabotaged repair FAILS, so there is a red to explain" "1" "$INST_RC"
DIAG="$(dump_installer_output)"
contains "...and the dump names the failing step" "$DIAG" "the INSTALLED copy failed its self-test"
contains "...and the reason THAT proof gave" "$DIAG" "prune-kanban-worktrees_test.sh: No such file or directory"
check "...which the old head-only cut would have dropped (so this arm measures the display, not the capture)" \
  "0" "$(clip "$INST_OUT" 300 0 | grep -cF 'No such file or directory')"
cp "$SB/installer.keep.diag" "$INSTALLER_LIVE"
check "...and the installer copy is restored" "0" \
  "$(grep -cF 'rm -f "$DEST_DIR/$SUITE_NAME"' "$INSTALLER_LIVE")"

echo "--- the bite: each guard, neutered on a copy, flips its own arm ---"
# A green arm is not evidence until the guard behind it has been seen to matter. Each
# control below neuters ONE branch of the checker on a copy, asserts the EDITED text is
# on disk (a sed whose anchor matches 0 or 2+ times would leave the shipped file in place
# and the control would prove nothing), requires the arm's own outcome to change, and
# then restores the copy and requires the arm to come back. The mutations are edits of
# the checker's own text, so there is no seam in the shipped script for them to hide in.
cp "$CHECK" "$SB/check.keep"
mutate() { # <sed script> <anchor text> <replacement marker>
  cp "$SB/check.keep" "$CHECK"
  local before after
  before="$(grep -cF -- "$2" "$CHECK")"
  sed -i "$1" "$CHECK"
  after="$(grep -cF -- "$2" "$CHECK")"
  if [ "$before" = 1 ] && [ "$after" = 0 ] && [ "$(grep -cF -- "$3" "$CHECK")" = 1 ]; then
    ok "the mutation landed ($2 -> $3, anchor seen exactly once)"
  else
    bad "the mutation did NOT land (anchor before=$before after=$after, new=$(grep -cF -- "$3" "$CHECK")) — this control would be measuring the shipped file and prove nothing"
  fi
}

# C1: the byte comparison itself. The 2026-10-08 fixture must flip from RED to GREEN.
plant_ref
cp "$FIX_PRUNER_1008" "$HOST/scripts/prune-kanban-worktrees.sh"
run_check
check "C1 premise: the shipped checker calls the 2026-10-08 fixture drift" "1" "$CHECK_RC"
mutate 's@if \[ "\$live_sha" = "\$ref_sha256" \]; then@if true; then@' \
  'if [ "$live_sha" = "$ref_sha256" ]; then' 'if true; then'
run_check
check "C1 ...and the SAME fixture reads GREEN once the comparison is neutered — so the red above IS the comparison" "0" "$CHECK_RC"
count "...with no DRIFT line left in the output" "$CHECK_OUT" "DRIFT" 0
cp "$SB/check.keep" "$CHECK"
run_check
check "C1 restored: the fixture is drift again" "1" "$CHECK_RC"

# C2: the EMPTY-file refusal. The verdict stays red either way (an empty file differs
# from the ref too), so what must vanish is the REFUSAL ITSELF — the sentence that says
# an empty read is not evidence of agreement.
plant_ref
printf '' >"$HOST/scripts/prune-kanban-worktrees.sh"
run_check
contains "C2 premise: the shipped checker refuses an EMPTY host copy by name" "$CHECK_OUT" "reads as EMPTY"
mutate 's@if \[ ! -s "\$live" \]; then@if false; then@' \
  'if [ ! -s "$live" ]; then' 'if false; then'
run_check
count "C2 ...and that refusal is GONE once its branch is neutered (the empty file is then reported as drift)" \
  "$CHECK_OUT" "reads as EMPTY" 0
contains "...while the run still fails, so the control is about the refusal and not about the verdict" "$CHECK_OUT" "DRIFT"
cp "$SB/check.keep" "$CHECK"

# C3: the no-pruner-on-this-machine branch. Neutered, the "no host" arm must stop being
# green — it falls through to comparing files that are not there.
run_check --hermes-home "$SB/nohost"
check "C3 premise: a machine with no pruner is green on the shipped checker" "0" "$CHECK_RC"
mutate 's@if \[ "\$present" -eq 0 \] && \[ -z "\$armed" \]; then@if false; then@' \
  'if [ "$present" -eq 0 ] && [ -z "$armed" ]; then' 'if false; then'
run_check --hermes-home "$SB/nohost"
check "C3 ...and that machine is RED once the branch is neutered (it compares files that do not exist)" "1" "$CHECK_RC"
count "...with the no-host NOTE gone" "$CHECK_OUT" "no pruner on this machine" 0
cp "$SB/check.keep" "$CHECK"
run_check --hermes-home "$SB/nohost"
check "C3 restored: the machine is green again" "0" "$CHECK_RC"
rm -f "$SB/check.keep"

echo
printf 'check-prune-kanban-worktrees-host self-test: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
exit 0
