#!/usr/bin/env bash
#
# install-host-container-prune.sh — put the container-prune guard on a host and arm it.
#
# Installs `jomform-container-prune.sh` (the STOPPED-container guard, see
# deploy/host/README.md) to /usr/local/bin, creates the log file, and adds the
# nightly cron line — then VERIFIES the installed copy's sha256 against this
# repository's copy and the cron line's exact count.
#
# IDEMPOTENT BY CONSTRUCTION
#   The guard is re-installed byte-for-byte on every run, and the cron line is
#   appended ONLY when that exact line is absent. A second run therefore writes
#   no cron line at all and leaves the rest of the crontab untouched — so a
#   re-install after a host rebuild, or a second run by a second lane, cannot
#   duplicate the job.
#
# It NEVER runs the guard. Installing is not pruning; a hand-run is a deliberate
# step and is documented in deploy/host/README.md (`JF_PRUNE_DRY_RUN=1` first).
#
# Usage:
#   deploy/host/install-host-container-prune.sh [--host root@203.0.113.10] [--dry-run]
#                                              [--prefix DIR] [--log PATH]
#
#   (no flags)            install on THIS host (the intended use: run it on prod)
#   --host <ssh-target>   install on that host over ssh/scp
#   --prefix <dir>        where the guard is installed (default /usr/local/bin)
#   --log <path>          the file the cron entry redirects to
#                         (default /var/log/jomform-container-prune.log)
#   --dry-run             print the plan and the exact cron line, write NOTHING
#
# Exit codes: 0 = installed and verified (or dry-run), 1 = refused / verification
# failed, 2 = bad usage.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$HERE/jomform-container-prune.sh"

PREFIX=/usr/local/bin
LOG=/var/log/jomform-container-prune.log
HOST=""
DRY_RUN=0
SCHEDULE='40 3'

die() { echo "ERROR: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --host)    HOST="${2:-}";   shift 2 ;;
    --prefix)  PREFIX="${2:-}"; shift 2 ;;
    --log)     LOG="${2:-}";    shift 2 ;;
    --dry-run) DRY_RUN=1;       shift ;;
    -h|--help) sed -n '3,36p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
done

[ -n "$PREFIX" ] || die "--prefix needs a directory"
[ -n "$LOG" ] || die "--log needs a path"
[ -f "$SRC" ] || die "$SRC not found — this installer ships next to the guard it installs"
bash -n "$SRC" || die "$SRC is not valid bash — refusing to install it"

BIN="$PREFIX/jomform-container-prune.sh"
CRON_LINE="$SCHEDULE * * * $BIN >> $LOG 2>&1"
WANT_SHA="$(sha256sum "$SRC" | awk '{print $1}')"

# Writing /usr/local/bin and the ROOT crontab needs root; a sandboxed install
# (--prefix/--log pointing somewhere private) does not, which is what lets the
# suite exercise both paths without touching an installed tree.
if [ -z "$HOST" ] && [ "$PREFIX" = /usr/local/bin ] && [ "$(id -u)" != 0 ]; then
  die "must run as root to install into /usr/local/bin and the root crontab (use --prefix/--log for a sandbox install)"
fi

# --- the host surface --------------------------------------------------------
# Every write goes through these three, so a remote install and a local one are
# the same code path and the same verification. `q` quotes an argument for the
# shell that will re-read it on the other side.
q() { printf '%q' "$1"; }

host_run() {
  if [ -n "$HOST" ]; then ssh -o BatchMode=yes "$HOST" "$1"; else bash -c "$1"; fi
}
host_put() {
  if [ -n "$HOST" ]; then scp -q "$1" "$HOST:$2"; else cp "$1" "$2"; fi
}
cron_write() {
  if [ -n "$HOST" ]; then ssh -o BatchMode=yes "$HOST" crontab -; else crontab -; fi
}

say() { printf '%s\n' "$*"; }

say "installer : $(basename "${BASH_SOURCE[0]}")"
say "target    : ${HOST:-this host}"
say "install   : $BIN (0755)"
say "log       : $LOG"
say "cron      : $CRON_LINE"
say "source    : $SRC"
say "sha256    : $WANT_SHA"

if [ "$DRY_RUN" = 1 ]; then
  say "dry-run   : nothing written, nothing verified"
  exit 0
fi

# --- 1. the guard itself -----------------------------------------------------
host_run "install -d -m 0755 $(q "$PREFIX")" || die "could not create $PREFIX"
host_put "$SRC" "$BIN" || die "could not place $BIN"
if [ -z "$HOST" ] && [ "$(id -u)" = 0 ]; then
  host_run "chown root:root $(q "$BIN") && chmod 0755 $(q "$BIN")" || die "could not set $BIN to 0755 root:root"
else
  host_run "chmod 0755 $(q "$BIN")" || die "could not set $BIN to 0755"
fi
say "installed : $BIN"

# --- 2. the log file ---------------------------------------------------------
# `touch`, never `install /dev/null`: a re-install must not truncate a log.
host_run "install -d -m 0755 $(q "$(dirname "$LOG")")" || die "could not create $(dirname "$LOG")"
host_run "touch $(q "$LOG") && chmod 0644 $(q "$LOG")" || die "could not create $LOG"
say "log       : $LOG (created, existing content kept)"

# --- 3. the cron entry, only if absent ---------------------------------------
existing="$(host_run "crontab -l 2>/dev/null" || true)"
have="$(printf '%s\n' "$existing" | grep -cxF -- "$CRON_LINE" || true)"
if [ "${have:-0}" -gt 0 ]; then
  say "cron      : entry already present — unchanged"
else
  { [ -n "$existing" ] && printf '%s\n' "$existing"; printf '%s\n' "$CRON_LINE"; } | cron_write \
    || die "could not write the crontab"
  say "cron      : entry added"
fi

# --- 4. verify the installed copy and the cron entry -------------------------
got_sha="$(host_run "sha256sum $(q "$BIN")" | awk '{print $1}')"
[ "$got_sha" = "$WANT_SHA" ] || die "$BIN does not match the repository copy (want $WANT_SHA, got ${got_sha:-nothing})"
say "verified  : sha256 matches the repository copy"

[ "$(host_run "test -f $(q "$LOG")" && echo yes || echo no)" = yes ] || die "$LOG was not created"

count="$(host_run "crontab -l 2>/dev/null" | grep -cxF -- "$CRON_LINE" || true)"
[ "${count:-0}" -eq 1 ] || die "the crontab holds $count copies of the cron entry, want exactly 1"
say "verified  : cron entry present exactly once"

say "done. The guard has NOT been run — the next scheduled pass is 03:40."
say "      preview a pass by hand with: JF_PRUNE_DRY_RUN=1 $BIN"
exit 0
