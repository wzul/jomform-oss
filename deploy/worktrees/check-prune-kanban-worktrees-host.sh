#!/usr/bin/env bash
#
# check-prune-kanban-worktrees-host.sh — is the pruner the HOST RUNS the pruner the
# repo MERGED?
#
# WHY THIS EXISTS (card t_d9cf1ddd)
#   The cron job `prune-kanban-worktrees` (`no_agent: true`, `0 4 * * *`) resolves
#   `script: prune-kanban-worktrees.sh` under $HERMES_HOME/scripts — a copy OUTSIDE
#   the checkout. The ONLY thing that ever puts a new revision there is the MANUAL
#   installer deploy/worktrees/install-prune-kanban-worktrees.sh: nothing runs it
#   automatically (deploy/build-and-ship.sh does not reference it) and no gate
#   compared the host copy with the repo, so every merge that touched
#   deploy/worktrees/* left the RUNNING copy stale until a lane remembered. That is
#   not hypothetical. Measured, hash-verified (each figure is the sha256 of the
#   file, and the sha256 of `git show <rev>:deploy/worktrees/<name>` for the
#   revision named):
#
#     when          merged change             host copy              result
#     2026-09-23    #719 e2ba3138             138ed09c / 7aef4d36    ok
#     by 2026-10-08 #797 c30aa8cf … #1050     e3137b91 / …           LIVE-CODE DRIFT:
#                   the running pruner predated the live-process guard and did not
#                   contain `has_process_matching` while the cron job kept running
#                   nightly; fixed by hand during t_a7312c6c
#     2026-10-10    #1066 bbd0630d (suite)     620fbd5b vs 7c2b39e5   drift; closed by
#                   t_82b6ae3c, which re-ran the installer by hand
#     2026-10-15    #1158 21649f92,            05474f06 / 97ff705a    LIVE-CODE DRIFT:
#                   #1161 f85a772f             vs the merged          the merged
#                   e60897d5 / 7871e173        pruner carries 11 non-comment lines
#                   the running copy does not have (the reaped-pid skip of #1158 and
#                   the exit-3 artefact count of #1161)
#
#   A manual step with no verifier IS the failure mode: t_82b6ae3c declared the
#   install satisfied on 2026-10-08 and the copy had drifted again two days later.
#   This script is the verifier. The `prunesync` preflight gate runs it, its own
#   offline harness (check-prune-kanban-worktrees-host_test.sh) proves it bites, and
#   it compares the copy that RUNS with the revision the branch MERGED.
#
# WHAT IT COMPARES, AND AGAINST WHAT
#   For every file the installer puts on the host — the list is READ from the
#   installer (`--print-targets`), so a file added there is compared here with no
#   second list to keep in step:
#
#     $PRUNE_HERMES_HOME/scripts/<name>           the copy the cron job resolves
#   vs
#     `git show <ref>:deploy/worktrees/<name>`    the MERGED revision (default origin/main)
#
#   THE REFERENCE IS THE MERGED REVISION, NOT THIS WORKING TREE, and that is a
#   decision rather than an oversight. A lane editing deploy/worktrees/* holds
#   different bytes from the merged ones, so a check against the working tree would
#   either red that lane for a difference only a merge can settle, or — the unsafe
#   direction — push it to install its UNMERGED pruner as the nightly job. origin/main
#   is the only revision the host is entitled to run.
#
#   It is a LOCAL ref, so it is only as fresh as the last fetch; the commit it
#   resolved to, and that commit's date, are printed on every run so a stale
#   reference is visible instead of silent. `--ref <rev>` checks a specific revision.
#
# IT NEVER WRITES. A check that "fixed" the host inside a lane's gate run would
#   install unmerged bytes on a destructive nightly job, so the repair is explicit,
#   revision-exact, and driven through the installer (which proves the revision's
#   own suite before and after installing) rather than done here:
#
#     deploy/worktrees/install-prune-kanban-worktrees.sh --from-ref origin/main
#
# EXIT
#   0  every compared pair is byte-identical, OR this machine holds no pruner at all
#      (no target present AND no job that resolves to one — a plain clone, reported
#      as a NOTE that names the 0 pairs it compared)
#   1  drift, or a pair that could not be judged: a MISSING or EMPTY host copy, a
#      reference revision that does not resolve, a ref that carries no such file
#   2  the invocation is wrong (unknown argument, empty --hermes-home/--ref)
#
# USAGE
#   deploy/worktrees/check-prune-kanban-worktrees-host.sh [--hermes-home PATH] [--ref REV]
#
#   --hermes-home PATH   the Hermes home whose scripts/ the cron job resolves
#                        (default $PRUNE_HERMES_HOME, else /root/.hermes — the same
#                        knob and default as the installer, so the two cannot
#                        disagree about which directory is the one that runs)
#   --ref REV            the merged revision to compare against (default
#                        $PRUNE_REF, else origin/main)
#
#   Read-only: nothing under $PRUNE_HERMES_HOME is written, and this script holds no
#   opinion about the OTHER copy the agent host carries (/root/agents): nothing
#   resolves a job to it (docs/MONITORING.md records it as the leftover it is), so a
#   verdict about it here would be a verdict about a file that never runs.
#
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER="$HERE/install-prune-kanban-worktrees.sh"
# Where the installed files live in the repo — the same directory this script and
# the installer are in, so the ref lookup and the installed set cannot drift apart.
REPO_SUBDIR="deploy/worktrees"
PRUNE_HERMES_HOME="${PRUNE_HERMES_HOME:-/root/.hermes}"
REF="${PRUNE_REF:-origin/main}"

usage_die() { echo "prune-host-check: $*" >&2; echo "prune-host-check: see --help" >&2; exit 2; }
die()       { echo "prune-host-check: FAIL  $*" >&2; exit 1; }
note()      { printf 'prune-host-check: NOTE  %s\n' "$*"; }
okline()    { printf 'prune-host-check: OK    %s\n' "$*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --hermes-home) [ $# -ge 2 ] || usage_die "--hermes-home needs a path"; PRUNE_HERMES_HOME="$2"; shift 2 ;;
    --ref)         [ $# -ge 2 ] || usage_die "--ref needs a revision";      REF="$2";              shift 2 ;;
    -h|--help)     sed -n '2,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)             usage_die "unknown argument: $1" ;;
  esac
done
[ -n "$PRUNE_HERMES_HOME" ] || usage_die "--hermes-home must not be empty"
[ -n "$REF" ]              || usage_die "--ref must not be empty"

DEST_DIR="$PRUNE_HERMES_HOME/scripts"
JOBS_JSON="$PRUNE_HERMES_HOME/cron/jobs.json"

# ── the installed-file set, DERIVED from the installer ───────────────────────
# ONE list for the installer, the checker and the harness. A name typed here as
# well would be a second place to forget: a file the installer starts putting on
# the host would then never be compared, which is the drift this script exists for.
[ -f "$INSTALLER" ] || die "no installer at $INSTALLER — the installed-file set is read from its --print-targets, so without it nothing here can say what the host is supposed to hold"
targets_out="$(bash "$INSTALLER" --print-targets 2>&1)"
installer_rc=$?
[ "$installer_rc" -eq 0 ] || die "the installer at $INSTALLER could not print its target list (rc=$installer_rc): $(printf '%s' "$targets_out" | tr '\n' '|')"

TARGETS=()
while IFS= read -r t; do
  [ -n "$t" ] || continue
  case "$t" in
    */*|.|..) die "the installer's target list carries '$t', which is not a bare file name — refusing to compare a path it did not name plainly" ;;
  esac
  TARGETS+=("$t")
done <<<"$targets_out"
[ "${#TARGETS[@]}" -gt 0 ] || die "the installer printed NO target name(s) — a check that compares nothing proves nothing"

# ── is there a pruner on this machine at all? ────────────────────────────────
# "This machine does not run the pruner" and "the pruner has drifted" are different
# facts and only one of them is a defect, so they are told apart with the same
# source of truth the installer uses for the same question: the target home's own
# jobs.json (read as a literal name search, exactly as the installer reads it, so
# the two cannot disagree about whether a job is armed). A MISSING target while a
# job IS armed is the opposite of a skip: the cron job would run nothing.
armed=""
if [ -f "$JOBS_JSON" ]; then
  for t in "${TARGETS[@]}"; do
    if grep -qF "\"$t\"" "$JOBS_JSON"; then armed="$armed $t"; fi
  done
fi
present=0
for t in "${TARGETS[@]}"; do
  if [ -e "$DEST_DIR/$t" ]; then present=$((present + 1)); fi
done
if [ "$present" -eq 0 ] && [ -z "$armed" ]; then
  note "no pruner on this machine: $DEST_DIR holds none of [${TARGETS[*]}] and $JOBS_JSON names no job that resolves to one."
  note "nothing was compared — this is not the host the cron job runs on (a plain clone, or a host the installer was never run on)."
  echo "prune-host-check: 0 pair(s) compared, 0 drifted."
  exit 0
fi

# ── the reference revision ───────────────────────────────────────────────────
REPO_ROOT="$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null)"
[ -n "$REPO_ROOT" ] || die "this script is not inside a git repository ($HERE), so the merged revision of $REPO_SUBDIR/* cannot be resolved"
ref_sha="$(git -C "$REPO_ROOT" rev-parse --verify --quiet "$REF^{commit}" 2>/dev/null)"
[ -n "$ref_sha" ] || die "the reference revision '$REF' does not resolve to a commit in $REPO_ROOT — a check that cannot name the revision the host should be running proves nothing (pass --ref <rev>)"
ref_date="$(git -C "$REPO_ROOT" log -1 --format=%cI "$ref_sha" 2>/dev/null)"
echo "prune-host-check: ref   $REF = ${ref_sha:0:12} ($ref_date) resolved in $REPO_ROOT"
echo "prune-host-check: host  $DEST_DIR (${present} of ${#TARGETS[@]} installed file(s) present; armed job name(s):${armed:-none})"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-host-check.XXXXXX")"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

# ── one pair per installed file ──────────────────────────────────────────────
RC=0; compared=0; drifted=0; unjudged=0
for t in "${TARGETS[@]}"; do
  live="$DEST_DIR/$t"
  rel="$REPO_SUBDIR/$t"
  ref_file="$WORK/ref.$t"
  if ! git -C "$REPO_ROOT" show "$REF:$rel" >"$ref_file" 2>"$WORK/err"; then
    echo "prune-host-check: FAIL  $REF carries no $rel — the repo no longer ships a file this installer puts on the host, so the running copy cannot be judged against the merged revision" >&2
    awk 'NR<=3 { print "    " $0 }' "$WORK/err" >&2
    RC=1; unjudged=$((unjudged + 1)); continue
  fi
  if [ ! -e "$live" ]; then
    echo "prune-host-check: FAIL  $live is MISSING while the installer puts $t on the host (armed job name(s):${armed:-none}) — an absent script cannot be what the cron job resolves" >&2
    RC=1; unjudged=$((unjudged + 1)); continue
  fi
  if [ ! -s "$live" ]; then
    echo "prune-host-check: FAIL  $live reads as EMPTY — an empty read is not evidence of agreement (and an empty file is not a pruner)" >&2
    RC=1; unjudged=$((unjudged + 1)); continue
  fi
  live_sha="$(sha256sum "$live" | awk '{print $1}')"
  ref_sha256="$(sha256sum "$ref_file" | awk '{print $1}')"
  compared=$((compared + 1))
  if [ "$live_sha" = "$ref_sha256" ]; then
    okline "$t — byte-identical to $REF:$rel (sha256 ${live_sha:0:12}…)"
    continue
  fi
  echo "prune-host-check: DRIFT $t — the copy under $DEST_DIR is NOT $REF:$rel" >&2
  echo "                   host: $live" >&2
  echo "                   ref : $REF:$rel" >&2
  echo "                   host sha256 $live_sha" >&2
  echo "                   ref  sha256 $ref_sha256" >&2
  # `awk` rather than `diff … | head`: under `set -o pipefail` a reader that exits
  # early sends SIGPIPE to the writer, which would make this report abort mid-way
  # (the same defect deploy/vps/drift-check.sh records for its own diff). The labels
  # name the two SIDES rather than the private temp path the ref was read into.
  diff -u --label "$live (what RUNS)" --label "$REF:$rel (what was MERGED)" \
    "$live" "$ref_file" >"$WORK/diff.txt" 2>&1
  awk 'NR<=60 { print "    " $0 }' "$WORK/diff.txt" >&2
  echo "                   (< = what RUNS on the host, > = what the branch MERGED)" >&2
  RC=1; drifted=$((drifted + 1))
done

if [ "$RC" -eq 0 ]; then
  okline "$compared pair(s) compared against $REF (${ref_sha:0:12}) — the copy the cron job runs IS the revision the branch merged."
  exit 0
fi

echo "prune-host-check: ${drifted} drifted, ${unjudged} could not be judged, of ${#TARGETS[@]} target(s) ($compared compared) against $REF (${ref_sha:0:12})." >&2
echo "  The cron job resolves its script under $DEST_DIR, so the copy that RUNS is not the copy the branch MERGED." >&2
REPAIR="$INSTALLER --from-ref $REF"
case "$PRUNE_HERMES_HOME" in
  /root/.hermes) ;;
  *) REPAIR="$REPAIR --hermes-home $PRUNE_HERMES_HOME" ;;
esac
echo "  Repair (installs the MERGED bytes from $REF — NOT this working tree's — and re-runs the revision's" >&2
echo "  own suite from the installed location, so what was verified is the artifact that will run):" >&2
echo "    $REPAIR" >&2
echo "  Then re-check: $0 --ref $REF" >&2
exit 1
