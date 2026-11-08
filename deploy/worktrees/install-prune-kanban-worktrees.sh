#!/usr/bin/env bash
#
# install-prune-kanban-worktrees.sh — put the worktree pruner on a host and arm it.
#
# WHY AN INSTALLER
#   The pruner runs OUTSIDE the checkout: the Hermes cron job
#   `prune-kanban-worktrees` (`no_agent: true`, `script: prune-kanban-worktrees.sh`)
#   resolves that name under $HERMES_HOME/scripts/, and the script's own log and
#   defaults live under /root/agents. Two copies of the same file therefore exist
#   on the host, and the one the cron actually runs is the $HERMES_HOME one — so
#   editing "the" script by path is how a fix lands in a file nobody runs. This
#   installer is the supported way to update it, and it keeps the FILENAME the
#   cron job already names, so the job needs no edit.
#
#   It is also the reason the script lives in the repo at all: the repo copy is
#   the source of truth and its self-test IS the `prunewt` preflight gate.
#
# WHAT IT DOES
#   1. validates the source with its own self-test (the same suite the `prunewt`
#      gate runs) BEFORE installing anything;
#   2. installs prune-kanban-worktrees.sh + the self-test to $HERMES_HOME/scripts
#      (root, 0755);
#   3. re-runs the self-test FROM THE INSTALLED LOCATION, so what was verified is
#      the artifact that will run, not the file it was copied from;
#   4. checks that the cron job the fix is for exists and names that filename,
#      and reports what to do if it does not (it never edits the job: the job's
#      own definition is not this script's business, and the filename it uses is
#      unchanged).
#
# WHEN A PROOF FAILS, IT SAYS WHY (card t_1483975b). A proof log is ~220 lines and
# the failing check can be anywhere in it; this installer used to print a fixed
# `tail -20` and delete the log, so the `prunesync` gate reddened three times on
# `212 passed, 1 failed` (and once on `210 passed, 3 failed`) with no failing check
# named and no evidence left. A failure now prints the failing check line(s) from
# the log, still prints the tail (a missing suite has no per-check FAIL line), and
# KEEPS the log, naming its path in the error.
#
# Usage:
#   deploy/worktrees/install-prune-kanban-worktrees.sh [--dry-run] [--no-cron-check]
#                                                      [--hermes-home PATH] [--skip-proof]
#                                                      [--from-ref REV] [--print-targets]
#
#   --from-ref REV    install the bytes at REV (`git show REV:deploy/worktrees/<name>`)
#                     instead of this tree's copies. THIS IS THE REPAIR PATH for the
#                     `prunesync` gate: that gate compares the copy the host RUNS with
#                     the merged revision and, on drift, tells the operator to run
#                     exactly this. Running the installer from a LANE's tree would put
#                     that lane's UNMERGED pruner on a destructive nightly job, so the
#                     repair has to name a revision, and this flag is what makes it a
#                     one-command, revision-exact repair from any tree.
#   --print-targets   print the file names this installer puts on the host, one per
#                     line, and exit. That list is what the checker compares, so a
#                     file added here cannot be silently left uncompared.
#
# It never deletes a worktree: installing is a file copy plus a proof run, and the
# proof run executes the self-test, which works in its own sandbox.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$HERE/prune-kanban-worktrees.sh"
SUITE="$HERE/prune-kanban-worktrees_test.sh"
SRC_NAME="prune-kanban-worktrees.sh"
SUITE_NAME="prune-kanban-worktrees_test.sh"
JOB_SCRIPT_NAME="$SRC_NAME"          # the name the cron job resolves
# THE ONE LIST of what lands on the host. It drives the install (below), the
# `--print-targets` output, and through that the `prunesync` gate's checker — so a
# third file added to the install cannot become a file nobody compares.
TARGET_NAMES=("$SRC_NAME" "$SUITE_NAME")
# WHERE TO INSTALL — deliberately NOT $HERMES_HOME.
#   The cron job that runs this pruner is defined in the DEFAULT profile
#   ($HERMES_HOME_DEFAULT/cron/jobs.json), so the copy it actually executes is
#   $HERMES_HOME_DEFAULT/scripts/<name>. A worker's own shell usually has
#   HERMES_HOME pointed at ITS profile (e.g. /root/.hermes/profiles/<lane>), and
#   installing there would put the fix in a directory the job never reads — which
#   is precisely the "the copy you edited is not the copy that runs" failure this
#   installer exists to prevent. So the target is explicit and overridable.
PRUNE_HERMES_HOME="${PRUNE_HERMES_HOME:-/root/.hermes}"

DRY_RUN=0
SKIP_PROOF=0
CHECK_CRON=1
FROM_REF=""
FROM_REF_GIVEN=0
PRINT_TARGETS=0

die() { echo "ERROR: $*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

# Every private path this run creates is registered here and removed on exit. It exists
# because this installer is now driven by more than one caller at a time — the
# `prunesync` gate's harness runs it, lanes run it, and a host may run it while both do
# — so a FIXED /tmp name would let one run read another's proof log and print a verdict
# about bytes it never proved. (Same class the container-prune guard's suite records for
# its own host-global temp names.)
TMP_PATHS=()
PROOF_LOGS_KEPT=()
cleanup_tmp() {
  local p keep k
  for p in ${TMP_PATHS+"${TMP_PATHS[@]}"}; do
    [ -n "$p" ] || continue
    keep=0
    for k in ${PROOF_LOGS_KEPT+"${PROOF_LOGS_KEPT[@]}"}; do
      [ "$k" = "$p" ] && keep=1
    done
    [ "$keep" = 1 ] && continue
    rm -rf "$p"
  done
}
trap cleanup_tmp EXIT

# ── how a RED explains itself ─────────────────────────────────────────────────
# A proof log is ~220 lines and the failing check can be ANYWHERE in it. The
# `prunesync` gate has reddened twice on `212 passed, 1 failed` with the failing
# check above the installer's fixed `tail -20`, which left the lane holding a count
# and no cause (cards t_2244109a, t_16d159f5) — and the log that would have named it
# was deleted with the run. So a failure now prints the failing check lines BY NAME,
# still prints the tail for the failures that are not a per-check FAIL (a missing
# suite, a fatal error), and KEEPS the log, naming its path in the error.
proof_failed() { # <log> <what was being proven>
  local log="$1" what="$2" fails
  fails="$(grep -E '^[[:space:]]*(FAIL|✘|not ok)' "$log" || true)"
  if [ -n "$fails" ]; then
    printf -- '--- failing check(s) in the %s self-test ---\n' "$what" >&2
    printf '%s\n' "$fails" >&2
  else
    printf -- '--- the %s self-test printed no per-check FAIL line: it did not reach the checks ---\n' "$what" >&2
  fi
  printf -- '--- last 20 lines of that log ---\n' >&2
  tail -20 "$log" >&2
  PROOF_LOGS_KEPT+=("$log")
  printf -- 'proof log kept: %s\n' "$log" >&2
}

# The source path to install a target FROM. Paired with TARGET_NAMES so the install
# loop, the printed list and the proof step cannot disagree about what goes where.
target_src() {  # <target name> -> repo path on stdout, non-zero when there is none
  case "$1" in
    "$SRC_NAME")   printf '%s' "$SRC" ;;
    "$SUITE_NAME") printf '%s' "$SUITE" ;;
    *) return 1 ;;
  esac
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)        DRY_RUN=1; shift;;
    --skip-proof)     SKIP_PROOF=1; shift;;
    --no-cron-check)  CHECK_CRON=0; shift;;
    --hermes-home)    PRUNE_HERMES_HOME="${2:-}"; shift 2;;
    --from-ref)       FROM_REF="${2:-}"; FROM_REF_GIVEN=1; shift 2;;
    --print-targets)  PRINT_TARGETS=1; shift;;
    -h|--help)        sed -n '2,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0;;
    *) die "unknown argument: $1";;
  esac
done

# A GIVEN --from-ref with an empty value is REFUSED rather than treated as absent:
# the flag's whole purpose is to stop the tree's copies being installed, and an empty
# value would silently do exactly that (the same trap `--for-diff` without a base
# records in scripts/preflight.sh).
if [ "$FROM_REF_GIVEN" = 1 ] && [ -z "$FROM_REF" ]; then
  die "--from-ref needs a revision — an empty value would install THIS tree's copies instead of the merged ones"
fi

# ── 0. --print-targets: the installed-file set, for the caller ────────────────
# Served BEFORE any prerequisite check: it is a question about this file, not about
# a tree or a host, and the `prunesync` checker asks it on machines that have no
# host at all.
if [ "$PRINT_TARGETS" = 1 ]; then
  printf '%s\n' "${TARGET_NAMES[@]}"
  exit 0
fi

# ── 0b. --from-ref: work from a REVISION, not from this tree's copies ─────────
# The extraction is a plain `git show` per target into a private directory, and then
# everything below (proof → install → proof) runs UNCHANGED on those bytes — the
# proof included, so the revision's own suite is what certifies the revision.
if [ -n "$FROM_REF" ]; then
  REPO_ROOT="$(git -C "$HERE" rev-parse --show-toplevel 2>/dev/null)"
  [ -n "$REPO_ROOT" ] || die "--from-ref needs the installer to live inside a git repository (looked up from $HERE)"
  git -C "$REPO_ROOT" rev-parse --verify --quiet "$FROM_REF^{commit}" >/dev/null 2>&1 \
    || die "--from-ref '$FROM_REF' does not resolve to a commit in $REPO_ROOT"
  FROM_SHA="$(git -C "$REPO_ROOT" rev-parse --short "$FROM_REF^{commit}")"
  EXTRACT="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-install-from-ref.XXXXXX")"
  TMP_PATHS+=("$EXTRACT")
  for t in "${TARGET_NAMES[@]}"; do
    if ! git -C "$REPO_ROOT" show "$FROM_REF:deploy/worktrees/$t" >"$EXTRACT/$t" 2>"$EXTRACT/err"; then
      die "--from-ref '$FROM_REF' carries no deploy/worktrees/$t — refusing to install a partial set ($(tail -n 1 "$EXTRACT/err"))"
    fi
  done
  SRC="$EXTRACT/$SRC_NAME"
  SUITE="$EXTRACT/$SUITE_NAME"
  info "source: $FROM_REF ($FROM_SHA) — installing the MERGED bytes, NOT this tree's copies"
fi

[ -f "$SRC" ]   || die "$SRC not found (run this from the repo: deploy/worktrees/)"
[ -f "$SUITE" ] || die "$SUITE not found"
[ -n "$PRUNE_HERMES_HOME" ] || die "--hermes-home must not be empty"
[ -d "$PRUNE_HERMES_HOME" ] || die "no Hermes home at $PRUNE_HERMES_HOME (pass --hermes-home)"

DEST_DIR="$PRUNE_HERMES_HOME/scripts"

# ── 1. the source must pass its own self-test before anything is installed ────
if [ "$SKIP_PROOF" = 1 ]; then
  info "WARNING: --skip-proof — installing a copy nothing has just verified"
elif [ "$DRY_RUN" = 1 ]; then
  info "dry-run: would run the self-test on $SRC first"
else
  info "verifying the source with its own self-test (JF_PRUNE_SCRIPT=$SRC)…"
  PROOF_LOG="$(mktemp "${TMPDIR:-/tmp}/jf-prune-install-proof.XXXXXX")"
  TMP_PATHS+=("$PROOF_LOG")
  if ! JF_PRUNE_SCRIPT="$SRC" bash "$SUITE" >"$PROOF_LOG" 2>&1; then
    proof_failed "$PROOF_LOG" source
    die "the self-test FAILED on $SRC — refusing to install a pruner that has not proven itself (failing check(s) above, log kept at $PROOF_LOG)"
  fi
  info "  source verified: $(tail -1 "$PROOF_LOG")"
fi

# ── 2. install ────────────────────────────────────────────────────────────────
# Driven by TARGET_NAMES with target_src, so the set installed is the set that
# `--print-targets` publishes (and therefore the set the `prunesync` checker
# compares) rather than a second hand-written list of `install` calls.
if [ "$DRY_RUN" = 0 ]; then
  mkdir -p "$DEST_DIR" || die "cannot create $DEST_DIR"
  for t in "${TARGET_NAMES[@]}"; do
    src_path="$(target_src "$t")" || die "no source path is known for target $t"
    install -m 0755 -o root -g root "$src_path" "$DEST_DIR/$t" \
      || die "cannot install $DEST_DIR/$t from $src_path"
    info "installed $DEST_DIR/$t"
  done
else
  for t in "${TARGET_NAMES[@]}"; do
    info "dry-run: would install $DEST_DIR/$t"
  done
fi

# ── 3. prove the INSTALLED artifact, not the file it came from ───────────────
if [ "$SKIP_PROOF" = 0 ]; then
  if [ "$DRY_RUN" = 1 ]; then
    info "dry-run: would re-run the self-test from $DEST_DIR"
  else
    info "re-running the self-test against the installed copy…"
    INSTALLED_LOG="$(mktemp "${TMPDIR:-/tmp}/jf-prune-install-installed-proof.XXXXXX")"
    TMP_PATHS+=("$INSTALLED_LOG")
    if ! JF_PRUNE_SCRIPT="$DEST_DIR/$SRC_NAME" bash "$DEST_DIR/$SUITE_NAME" \
         >"$INSTALLED_LOG" 2>&1; then
      proof_failed "$INSTALLED_LOG" installed
      die "the INSTALLED copy failed its self-test — the host is now running an unverified pruner at $DEST_DIR/$SRC_NAME (failing check(s) above, log kept at $INSTALLED_LOG)"
    fi
    info "  installed copy verified: $(tail -1 "$INSTALLED_LOG")"
  fi
fi

# ── 4. the cron job the fix is for ───────────────────────────────────────────
# Read the TARGET home's own job definition rather than asking `hermes cron list`:
# that CLI lists the DEFAULT profile's jobs resolved against the CALLER's
# HERMES_HOME, so from a lane's shell it answers about the wrong profile and
# reports "no such job" for a job that exists (observed on the agent host:
# hermes cron list found nothing while the job was live in the other home).
# jobs.json is the definition the scheduler of that home actually reads.
if [ "$CHECK_CRON" = 1 ]; then
  JOBS_JSON="$PRUNE_HERMES_HOME/cron/jobs.json"
  if [ "$DRY_RUN" = 1 ]; then
    info "dry-run: would check $JOBS_JSON for a job whose script is $JOB_SCRIPT_NAME"
  elif [ ! -f "$JOBS_JSON" ]; then
    info "NOTE: no job definitions at $JOBS_JSON — cannot confirm the cron job."
    info "      The job must name $JOB_SCRIPT_NAME, which resolves to $DEST_DIR/$JOB_SCRIPT_NAME."
  elif grep -q "\"$JOB_SCRIPT_NAME\"" "$JOBS_JSON"; then
    info "cron job present in $JOBS_JSON (script=$JOB_SCRIPT_NAME) — no job edit needed"
    # Show the schedule so a changed interval cannot pass unnoticed.
    sched="$(grep -o '"display"[[:space:]]*:[[:space:]]*"[^"]*"' "$JOBS_JSON" | sed -n '1p' | sed -E 's/.*"([^"]*)"$/\1/')"
    [ -n "$sched" ] && info "  schedule: $sched"
  else
    info "NOTE: $JOBS_JSON defines no job whose script is $JOB_SCRIPT_NAME."
    info "      Create it with: hermes cron create --no-agent --script $JOB_SCRIPT_NAME --schedule '0 4 * * *'"
  fi
fi

cat <<EOF

Pruner installed. Verify with:
  $DEST_DIR/$SRC_NAME --dry-run          # what it would remove, changing nothing
  tail -5 /root/agents/prune-kanban-worktrees.log
Is the copy that RUNS the revision the branch MERGED? (the 'prunesync' gate asks this too):
  $HERE/check-prune-kanban-worktrees-host.sh
The self-test that gates it (also a preflight gate, 'prunewt'):
  JF_PRUNE_SCRIPT=$DEST_DIR/$SRC_NAME bash $DEST_DIR/$SUITE_NAME
From here on, keep it level with the merged revision by re-running THIS installer
from a revision rather than from whatever tree you happen to be in:
  $HERE/install-prune-kanban-worktrees.sh --from-ref origin/main
EOF
