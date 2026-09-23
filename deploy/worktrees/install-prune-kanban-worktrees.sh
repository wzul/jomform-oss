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
# Usage:
#   deploy/worktrees/install-prune-kanban-worktrees.sh [--dry-run] [--no-cron-check]
#                                                      [--hermes-home PATH] [--skip-proof]
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

die() { echo "ERROR: $*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)        DRY_RUN=1; shift;;
    --skip-proof)     SKIP_PROOF=1; shift;;
    --no-cron-check)  CHECK_CRON=0; shift;;
    --hermes-home)    PRUNE_HERMES_HOME="${2:-}"; shift 2;;
    -h|--help)        sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0;;
    *) die "unknown argument: $1";;
  esac
done

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
  if ! JF_PRUNE_SCRIPT="$SRC" bash "$SUITE" >/tmp/jf-prune-install-proof.log 2>&1; then
    tail -20 /tmp/jf-prune-install-proof.log >&2
    die "the self-test FAILED on $SRC — refusing to install a pruner that has not proven itself"
  fi
  info "  source verified: $(tail -1 /tmp/jf-prune-install-proof.log)"
fi

# ── 2. install ────────────────────────────────────────────────────────────────
if [ "$DRY_RUN" = 0 ]; then
  mkdir -p "$DEST_DIR" || die "cannot create $DEST_DIR"
  install -m 0755 -o root -g root "$SRC" "$DEST_DIR/$SRC_NAME"
  install -m 0755 -o root -g root "$SUITE" "$DEST_DIR/$SUITE_NAME"
  info "installed $DEST_DIR/$SRC_NAME"
  info "installed $DEST_DIR/$SUITE_NAME"
else
  info "dry-run: would install $DEST_DIR/$SRC_NAME and $SUITE_NAME"
fi

# ── 3. prove the INSTALLED artifact, not the file it came from ───────────────
if [ "$SKIP_PROOF" = 0 ]; then
  if [ "$DRY_RUN" = 1 ]; then
    info "dry-run: would re-run the self-test from $DEST_DIR"
  else
    info "re-running the self-test against the installed copy…"
    if ! JF_PRUNE_SCRIPT="$DEST_DIR/$SRC_NAME" bash "$DEST_DIR/$SUITE_NAME" \
         >/tmp/jf-prune-install-installed-proof.log 2>&1; then
      tail -20 /tmp/jf-prune-install-installed-proof.log >&2
      die "the INSTALLED copy failed its self-test — the host is now running an unverified pruner at $DEST_DIR/$SRC_NAME"
    fi
    info "  installed copy verified: $(tail -1 /tmp/jf-prune-install-installed-proof.log)"
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
The self-test that gates it (also a preflight gate, 'prunewt'):
  JF_PRUNE_SCRIPT=$DEST_DIR/$SRC_NAME bash $DEST_DIR/$SUITE_NAME
EOF
