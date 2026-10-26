#!/usr/bin/env bash
#
# jomform-container-prune_test.sh — the decoy suite for jomform-container-prune.sh.
#
# WHAT IT PROVES. The guard removes STOPPED containers and nothing else. Asserting
# that against a real host is impossible: the whole point of the guard is that it
# deletes things, so a destructive pass has to run somewhere it cannot do harm.
# This suite runs it against a PRIVATE Docker daemon of its own — a second
# `dockerd` on a scratch data-root and unix socket, started here and torn down
# here — and plants the decoys in it. The host's own containers are never even
# visible to the guard under test, which is asserted at the end rather than
# assumed.
#
# AND IT RUNS IN A NETWORK NAMESPACE OF ITS OWN, which is not decoration: a
# data-root and a socket isolate a second daemon's CONTAINERS, not its NETNS.
# `--bridge=none` does not merely refrain from creating a bridge — moby DELETES
# the default bridge (`daemon/daemon_unix.go`, `removeDefaultBridgeInterface()`)
# of whichever network namespace the daemon is started in, and in the host's
# namespace that is the HOST's `docker0`. While it is gone EVERY container on the
# default bridge is unreachable FROM the host, the shared test valkey included,
# so other lanes' gates red with valkey `i/o timeout` on trees that are correct.
# Measured on the agent host 2026-10-12 (card `t_a10c01dd`): 16 `docker0 is
# MISSING` ALERTs in under four hours, each 2-minute watchdog run repairing a
# bridge a harness run had just deleted again, with this suite's private
# `dockerd … --bridge=none …` in the process snapshot taken the second `docker0`
# vanished. So the suite re-execs itself under `unshare -n`, plants its OWN
# `docker0` decoy in that namespace, and REQUIRES the daemon to delete THAT one;
# the caller's bridge is then read, from the caller's namespace, before and after.
#
# The decoys, and the gate each one exists for:
#   jf-prune-decoy-young     exited,  no label  -> the AGE gate   (floor 168h: not a candidate)
#   jf-prune-decoy-plain     exited,  no label  -> the guard BITES (floor 0: removed)
#   jf-prune-decoy-compose   created, com.docker.compose.project=jomform -> the COMPOSE gate
#   jf-prune-decoy-keepfile  created, named in the keep file -> the KEEP gate
#   jf-prune-decoy-running   RUNNING            -> the STATE gate (never a candidate, survives a floor-0 run)
#   jf-prune-decoy-nomkt     exited,  no label  -> created LATER, for the refusal
#                                                 arm: the candidate a floor-0 pass
#                                                 that cannot create its private
#                                                 rm-output file must NOT remove
#
# Plus the halves of the guard that are not about a decoy: it FAILS CLOSED on an
# empty container list, it FAILS CLOSED when it cannot create its own private
# rm-output file (and removes nothing then), it never removes an image, and it
# never writes to a FIXED path — the path it used to write its rm output to
# (`/tmp/jomform-container-prune.rm.out`, card t_13f1c904) is planted here as a
# symlink to a file root can write, and the real floor-0 pass below must leave
# that file byte-identical (card t_3d34df24).
#
# It also drives the INSTALLER (install-host-container-prune.sh) twice against a
# sandbox prefix with a fake `crontab` on PATH, and asserts the cron entry stays
# at exactly one line, the installed copy's sha256 matches, and `--dry-run`
# prints the exact production cron line while invoking no crontab at all.
#
# REQUIREMENTS (it FAILS, never skips, without them): root — a private dockerd
# cannot run unprivileged — plus the `dockerd` binary, `busybox` (for a 1 MB
# throwaway image, so no image has to be pulled or borrowed), `unshare` and
# `nsenter` (util-linux: the namespace above, and the caller's bridge read
# through it) and a reachable HOST docker daemon. The host daemon is READ for the
# before/after snapshots and the event window below, and written ONLY for this
# run's own two probe containers and one empty-rootfs probe image, which are
# removed again before the suite exits (see "the HOST daemon" below).
#
# Run:  bash deploy/host/jomform-container-prune_test.sh
# Exit: 0 only if every assertion held; 1 = an assertion failed — including the
#       namespace arm, which FAILS CLOSED (it refuses to start a daemon at all,
#       so a lost namespace cannot damage the host); 2 = the suite itself could
#       not run (missing prerequisite, private daemon would not start).

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="${JF_PRUNE_SCRIPT:-$HERE/jomform-container-prune.sh}"
INSTALLER="${JF_PRUNE_INSTALLER:-$HERE/install-host-container-prune.sh}"

# ================================================================ the HOST daemon
# THE HOST DAEMON IS SHARED, so "this run touched nothing on it" has to be
# ATTRIBUTED rather than counted. A bare before/after set comparison cannot say it:
# measured 2026-10-16 on card `t_df1719c1`'s full gate run, a peer re-created a
# container on this host inside the suite's own ~38s window and the suite printed
#
#     FAIL no host container that existed before this run is gone (want '0', got '1')
#
# — a bare count naming nothing, on a tree with nothing wrong, while the runner stops
# at the first failure and loses the rest of that run's gate inventory. The removal
# was reproducible in 40s with a `docker rm -f` from a different process (card
# `t_da7509c4`).
#
# So the comparison has three verdicts, and only the third fails:
#   OURS          this run's own audited removal path recorded the id -> the suite
#                 really did touch the host (see host_own_remove).
#   FOREIGN       the HOST daemon recorded a destroy for that id and this run
#                 recorded no removal of it -> ANOTHER actor did it. NAMED with its
#                 name and the second it happened, and NOT a failure of this tree.
#   UNATTRIBUTED  neither. That is a fact about the WINDOW (daemon restarted, events
#                 unreadable), not a green: it fails, naming the container, because a
#                 run that cannot attribute a loss has not measured anything.
#
# WHAT MAKES "this run recorded no removal of it" SOUND: every call to the host
# daemon goes through ONE docker primitive (`host_docker`). It is wrapped by a
# read-only door (`host`, which refuses every destructive verb) and by three narrow
# write helpers (`host_own_import`, `host_own_create`, `host_own_remove`), and
# `host_own_remove` accepts ONLY an id this run created. A self-check counts the raw
# call sites below and fails the run if a second one appears.
#
# AND THE VERDICTS ARE EXERCISED ON EVERY RUN, not merely believed: this run plants
# two uniquely-named probe containers on the host daemon and a PEER PROCESS — a
# separate `bash -c` calling the docker CLI directly, the exact shape of the
# redeploy that caused the false red — removes one of them mid-run. The check below
# must report that one FOREIGN and NAME it, find the control probe still present, and
# find nothing unattributed, so a run whose attribution stops discriminating goes RED
# instead of quietly passing.
HOST_SOCK="${JF_PRUNE_BITE_HOST_SOCKET:-unix:///var/run/docker.sock}"

DECOY="jf-prune-decoy"
D_YOUNG="$DECOY-young"
D_PLAIN="$DECOY-plain"
D_COMPOSE="$DECOY-compose"
D_KEEPFILE="$DECOY-keepfile"
D_RUNNING="$DECOY-running"
D_NOMKT="$DECOY-nomkt"                    # the refusal arm's OWN candidate (created after the real run)
IMG=""                                    # the per-run image the decoys are made from
# The running decoy's lifetime, unique per run: if this process is ever found
# after the run, the leak can be traced to THIS run, and a second suite running
# concurrently (the gate pool allows it) cannot be mistaken for a leak of mine.
SLEEP_SECS=$(( 120 + ($$ % 120) ))

SB=""        # scratch dir (private daemon data-root, sockets, logs)
PRIV=""      # the private daemon's socket path
PRIV_URL=""
DPID=""
KEEPFILE=""
NOMKT_BIN="" # PATH prefix holding an always-failing `mktemp` (the fail-closed arm)
PLANTED=""   # the symlink this run plants at the guard's historical FIXED rm path
CHECKS=0
FAILS=0

say()   { printf '%s\n' "$*"; }
fatal() { printf 'FATAL: %s\n' "$*" >&2; exit 2; }
ok()    { CHECKS=$((CHECKS + 1)); printf '  ok   %s\n' "$1"; }
bad()   { CHECKS=$((CHECKS + 1)); FAILS=$((FAILS + 1)); printf '  FAIL %s\n' "$1"; }

# req <label> <command...> — a command that MUST succeed, and whose own output is
# printed when it does not. A swallowed docker error is how a suite reports a
# pristine-looking result while measuring nothing: this cost a whole debugging
# cycle on the concurrent-image collision below.
req() {
  local label="$1"; shift
  local err="$SB/req.out"
  if "$@" >"$err" 2>&1; then rm -f "$err"; return 0; fi
  printf 'FATAL: %s failed:\n' "$label" >&2
  sed 's/^/  /' "$err" >&2
  rm -f "$err"
  exit 2
}

# has <file> <literal> <label> — the literal must be present.
has() {
  if grep -qF -- "$2" "$1"; then ok "$3"; else bad "$3 (missing: $2)"; dump "$1"; fi
}
# lacks <file> <literal> <label> — the literal must be absent.
lacks() {
  if grep -qF -- "$2" "$1"; then bad "$3 (unexpectedly present: $2)"; dump "$1"; else ok "$3"; fi
}
# eq <got> <want> <label>
eq() {
  if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (want '$2', got '$1')"; fi
}
dump() {
  printf '       ---- %s ----\n' "$1"
  sed 's/^/       /' "$1"
  printf '       ------------------\n'
}

# =========================================== this run's own NETWORK namespace
# WHY THIS IS THE FIRST THING THAT TOUCHES ANYTHING. A private data-root, socket
# and pidfile isolate a second daemon's CONTAINERS, not its network namespace —
# and `--bridge=none` makes moby DELETE the default bridge of the namespace it is
# started in (`daemon/daemon_unix.go` → `removeDefaultBridgeInterface()`, the
# "--bridge=none use case"). Started in the host's namespace, that is the HOST's
# `docker0`: every container on the default bridge then becomes unreachable from
# the host (the shared test valkey among them, so other lanes' gates red with
# valkey `i/o timeout` on correct trees), and Docker does not recreate it.
# Measured on the agent host 2026-10-12 — card `t_a10c01dd`, the root cause of
# `t_eb901784`: 16 `docker0 is MISSING` ALERTs in the watchdog log
# (`/var/log/jomform-docker0-watchdog.log`) between 11:40 and 15:24, i.e. the
# bridge destroyed again within ~2 minutes of every repair by the watchdog cron,
# and this suite's own private `dockerd … --bridge=none …` (parent: this file) in
# the process snapshot taken the second `docker0` vanished.
#
# So the whole suite re-execs itself inside `unshare -n`. Every socket it uses
# (the private daemon's, the host daemon's, the host containerd's) is a
# filesystem object, which crosses a namespace boundary unchanged, and every
# decoy is `--network none`, so the run needs no network of its own. The
# descriptor allocated below holds the CALLER's namespace — the one that must
# still own its `docker0` when this run ends — open across the re-exec, which is
# what lets the namespace arm read the caller's bridge from inside its own and
# assert that it is the same interface afterwards.
#
# THE FD IS ALLOCATED BY bash, NEVER A LITERAL 9: `scripts/preflight.sh` holds
# its gate-pool slot on a descriptor it allocates and passes down through the
# environment, and a fixed number here could free a peer's slot.
if [ "${JF_PRUNE_CELL:-0}" != "1" ]; then
  [ "$(id -u)" = 0 ] || fatal "this suite must run as root: it starts a private docker daemon"
  command -v unshare >/dev/null 2>&1 \
    || fatal "unshare (util-linux) not on PATH — the private daemon cannot be kept out of the host's network namespace, and a --bridge=none dockerd DELETES the docker0 of the namespace it is started in"
  command -v nsenter >/dev/null 2>&1 \
    || fatal "nsenter (util-linux) not on PATH — the caller's docker0 could not be measured from inside this run's own namespace"
  CALLER_NS="$(readlink /proc/self/ns/net)" \
    || fatal "could not read /proc/self/ns/net — the caller's network namespace cannot even be named"
  SELF="${BASH_SOURCE[0]}"
  case "$SELF" in /*) ;; *) SELF="$PWD/$SELF" ;; esac
  exec {HOSTNS_FD}</proc/self/ns/net
  exec unshare -n env \
    JF_PRUNE_CELL=1 \
    JF_PRUNE_CALLER_NS="$CALLER_NS" \
    JF_PRUNE_HOSTNS_FD="$HOSTNS_FD" \
    bash "$SELF" "$@"
fi

# We are in our own namespace now, and fd $HOSTNS_FD (inherited, see above) holds
# the CALLER's. Both inodes are read here rather than assumed: the arm below
# compares them, so a run that is somehow still in the caller's namespace (the
# re-exec removed, `unshare` neutered) can never reach the point of starting a
# daemon there.
HOSTNS_FD="${JF_PRUNE_HOSTNS_FD:-}"
CALLER_NS="${JF_PRUNE_CALLER_NS:-}"
OUR_NS="$(readlink /proc/self/ns/net || true)"
HOST_D0_BEFORE=""                               # set before anything starts a daemon

# host_ip <args…> — run `ip` in the CALLER's namespace through the inherited fd.
host_ip() { nsenter --net="/proc/self/fd/$HOSTNS_FD" ip "$@"; }

# host_d0_id — "ifindex mac" of the CALLER's docker0, empty when it has none.
host_d0_id() {
  host_ip -o link show docker0 2>/dev/null \
    | sed -n 's/^\([0-9][0-9]*\): docker0:.*link\/ether \([0-9a-f:][0-9a-f:]*\).*/\1 \2/p'
}

# host_d0_intact <label> — ONE check either way, so a host that genuinely has no
# docker0 is measured (and said out loud) rather than skipped: "nothing to lose"
# is a fact about the host, not a pass for this suite.
host_d0_intact() {
  if [ -n "$HOST_D0_BEFORE" ]; then
    eq "$(host_d0_id)" "$HOST_D0_BEFORE" "$1"
  else
    ok "$1 — no docker0 existed in the caller's namespace before this run (measured), so there is none to lose"
  fi
}

# ---------------------------------------------------------------- the sandbox
priv() { docker --host "$PRIV_URL" "$@"; }

# ------------------------------------------------- the HOST daemon: one door in
HOST_CALLS_LOG=""       # every read-only host call + its epoch
HOST_OWN_LOG=""         # every write this run makes to the host daemon
HOST_OWN_CONTAINERS=()  # container ids this run created on the host
HOST_OWN_REMOVED=()     # ids this run's own audited path removed
HOST_OWN_IMAGE=""       # the tag this run imported
HOST_OWN_IMAGE_ID=""
HOST_EVENTS_LOG=""
HOST_EVENTS_FROM=""

# host_docker — THE one raw call site. Everything else in this file goes through it,
# and the self-check in "the HOST daemon was not touched" fails the run if a second
# one appears: the attribution below rests on "this run issued no other removal".
host_docker() { docker --host "$HOST_SOCK" "$@"; }

# host <args…> — the READ-ONLY door: logs the call, refuses every verb that could
# change the daemon, then delegates. A refusal here is a bug in this file (a write
# belongs in a target-checked host_own_* helper), so it is fatal, never ignored.
host() {
  if [ -n "$HOST_CALLS_LOG" ] && [ -d "${HOST_CALLS_LOG%/*}" ]; then
    printf '%s|%s\n' "$(date +%s)" "$*" >>"$HOST_CALLS_LOG"
  fi
  case "${1:-}" in
    rm|rmi|kill|stop|restart|pause|unpause|prune|create|run|start|import|load|\
    tag|commit|build|push|update|exec|rename|cp|network|volume|system|compose|\
    container|swarm|node|secret|config|plugin)
      printf 'FATAL: the READ-ONLY host door was asked to run `docker %s`\n' "$*" >&2
      printf '       This suite may only read the host daemon; a write belongs in a\n' >&2
      printf '       target-checked host_own_* helper (see the file header).\n' >&2
      exit 2 ;;
  esac
  host_docker "$@"
}

# host_own_import <tag> <tar-file> — an image for the probe containers. An EMPTY
# rootfs built from a tar here, so the arm pulls nothing, needs no network and does
# not depend on the host's own images. Sets HOST_OWN_IMAGE / HOST_OWN_IMAGE_ID.
# (Deliberately NOT a `$( … )` call: the caller's state — the registry of what this
# run owns, and therefore the cleanup and the OURS verdict — must live in THIS shell,
# and a command substitution would throw it away.)
host_own_import() {
  local tag="$1" tar="$2" id
  [ -n "$HOST_OWN_LOG" ] || { printf 'FATAL: host_own_import before the sandbox exists\n' >&2; exit 2; }
  id="$(host_docker import - "$tag" <"$tar")" || return 1
  HOST_OWN_IMAGE="$tag"
  HOST_OWN_IMAGE_ID="$id"
  printf '%s|import|%s|%s\n' "$(date +%s)" "$id" "$tag" >>"$HOST_OWN_LOG"
}

# host_own_create <name> <out-var> — a container this run OWNS on the host daemon
# (created, never started: no process, no network, no disk worth naming). Sets
# <out-var> to its id, again in THIS shell, for the same reason.
host_own_create() {
  local name="$1" outvar="$2" id
  id="$(host_docker create --name "$name" "$HOST_OWN_IMAGE_ID" true)" || return 1
  HOST_OWN_CONTAINERS+=("$id")
  printf -v "$outvar" '%s' "$id"
  printf '%s|create|%s|%s\n' "$(date +%s)" "$id" "$name" >>"$HOST_OWN_LOG"
}

# host_own_remove <id> — the ONLY way this run may remove a host container, and it
# refuses anything this run did not create. That refusal is what makes OURS mean
# "this suite did it": no other id can ever be reached through this door.
host_own_remove() {
  local id="$1" c owned=0
  for c in ${HOST_OWN_CONTAINERS[@]+"${HOST_OWN_CONTAINERS[@]}"}; do
    [ "$c" = "$id" ] && owned=1
  done
  if [ "$owned" != 1 ]; then
    printf 'FATAL: host_own_remove(%s) — this run did not create that container; refusing\n' "$id" >&2
    exit 2
  fi
  host_docker rm -f "$id" >/dev/null 2>&1 || return 1
  HOST_OWN_REMOVED+=("$id")
  printf '%s|rm|%s|\n' "$(date +%s)" "$id" >>"$HOST_OWN_LOG"
}

# host_own_cleanup — idempotent, and the only place the probes' end state is
# decided. Called from priv_stop, i.e. also when the run dies early, so a failed run
# cannot leave a container or an image behind on the host.
host_own_cleanup() {
  local id
  for id in ${HOST_OWN_CONTAINERS[@]+"${HOST_OWN_CONTAINERS[@]}"}; do
    host_own_remove "$id" || true
  done
  HOST_OWN_CONTAINERS=()
  if [ -n "$HOST_OWN_IMAGE" ]; then
    host_docker rmi -f "$HOST_OWN_IMAGE" >/dev/null 2>&1 || true
    if [ -n "$HOST_OWN_LOG" ] && [ -d "${HOST_OWN_LOG%/*}" ]; then
      printf '%s|rmi|%s|%s\n' "$(date +%s)" "$HOST_OWN_IMAGE_ID" "$HOST_OWN_IMAGE" >>"$HOST_OWN_LOG"
    fi
    HOST_OWN_IMAGE=""
    HOST_OWN_IMAGE_ID=""
  fi
  return 0
}

# attribution_of_container <id> — OURS | FOREIGN | UNATTRIBUTED (see the header).
attribution_of_container() {
  local id="$1" c
  for c in ${HOST_OWN_REMOVED[@]+"${HOST_OWN_REMOVED[@]}"}; do
    if [ "$c" = "$id" ]; then printf 'OURS'; return 0; fi
  done
  if awk -F'|' -v id="$id" '$2=="container" && $3=="destroy" && $4==id {n++} END{exit !n}' \
       "$HOST_EVENTS_LOG" 2>/dev/null; then
    printf 'FOREIGN'
  else
    printf 'UNATTRIBUTED'
  fi
}

# attribution_of_image <id> — same verdicts for an image (delete/untag).
attribution_of_image() {
  local id="$1"
  if awk -F'|' -v id="$id" '$2=="image" && ($3=="delete" || $3=="untag") && $4==id {n++} END{exit !n}' \
       "$HOST_EVENTS_LOG" 2>/dev/null; then
    printf 'FOREIGN'
  else
    printf 'UNATTRIBUTED'
  fi
}

# host_event_time <id> — the epoch of the destroy/delete event for that id, so a
# failure (or a FOREIGN note) names WHEN it happened.
host_event_time() {
  awk -F'|' -v id="$1" '$4==id && ($3=="destroy" || $3=="delete" || $3=="untag") {print $1; exit}' \
    "$HOST_EVENTS_LOG" 2>/dev/null
}

# window_state — what the attribution had to work with, for a message that tells the
# reader whether the LOSS or the MEASUREMENT is the problem.
window_state() {
  local n
  n="$(grep -c . "$HOST_EVENTS_LOG" 2>/dev/null || true)"
  printf 'event window: %s line(s), host daemon id %s -> %s' \
    "${n:-0}" "${HOST_ID_BEFORE:-?}" "${HOST_ID_AFTER:-?}"
}

# Drop the scratch tree, retrying briefly: the overlay mounts of a just-removed
# container are released asynchronously, and a single `rm -rf` can catch one
# still busy. Only ever removes a directory this script created, under $TMPDIR.
drop_scratch() {
  [ -n "$SB" ] || return 0
  case "$SB" in
    "${TMPDIR:-/tmp}"/jf-prune-bite.*) ;;
    *) printf 'WARN: refusing to remove scratch dir %s (not a jf-prune-bite.* under TMPDIR)\n' "$SB" >&2; return 0 ;;
  esac
  local i=0
  while [ "$i" -lt 8 ]; do
    rm -rf "$SB" 2>/dev/null && return 0
    sleep 1
    i=$((i + 1))
  done
  rm -rf "$SB"
}

# Release any mount left under this run's scratch tree. Measured, not
# theorised: the private dockerd talks to the host's containerd, and when a
# decoy's shim is torn down at the same moment the daemon is killed the overlay
# mount that backed it is occasionally left behind in the host's mount table
# (reproduced 2026-10-12: 2 of 5 runs, with no surviving process). A lazy
# unmount of a path INSIDE this suite's own scratch dir only detaches it from
# the mount table — the snapshot data stays in containerd's store, and nothing
# else on the host references the path — so the end state is a clean host either
# way. Scoped by the case pattern: it never touches a mount outside $SB.
release_mounts() {
  [ -n "$SB" ] || return 0
  local dev mnt fstype rest released=0 attempts
  for attempts in 1 2 3; do
    while read -r dev mnt fstype rest; do
      case "$mnt" in
        "$SB"/*) umount -l "$mnt" 2>/dev/null && released=$((released + 1)) ;;
      esac
    done < /proc/mounts
    [ "$released" -gt 0 ] || break
    sleep 1
  done
  if [ "$released" -gt 0 ]; then
    printf 'jf-prune-bite: detached %s leftover mount(s) under %s\n' "$released" "$SB"
  fi
  return 0
}

priv_stop() {
  # THE HOST PROBES FIRST, and unconditionally: they are the only objects this
  # suite ever wrote to the shared daemon, so they go even when the run died before
  # the attribution ran. Target-checked (host_own_remove refuses an id this run did
  # not create), and idempotent.
  host_own_cleanup
  # Remove the decoys BEFORE the daemon goes: a container left behind belongs to
  # containerd, not to the daemon object, so tearing the daemon down first can
  # leave its process running for the rest of its `sleep`.
  if [ -n "$PRIV_URL" ] && priv info >/dev/null 2>&1; then
    priv rm -f "$(priv ps -a -q)" >/dev/null 2>&1 || true
    # Let the removed containers' processes exit before the daemon is killed, so
    # the shim usually releases its mount itself.
    local j=0
    while [ "$j" -lt 10 ] && pgrep -f "sleep $SLEEP_SECS" >/dev/null 2>&1; do sleep 0.5; j=$((j + 1)); done
  fi
  if [ -n "$DPID" ] && kill -0 "$DPID" 2>/dev/null; then
    kill "$DPID" 2>/dev/null || true
    local i=0
    while kill -0 "$DPID" 2>/dev/null && [ "$i" -lt 20 ]; do sleep 0.5; i=$((i + 1)); done
    kill -9 "$DPID" 2>/dev/null || true
    wait "$DPID" 2>/dev/null || true
  fi
  DPID=""
  # Only ever removes the symlink THIS run planted at the guard's historical fixed
  # rm path (a fixed path by definition, so it cannot be owned exclusively).
  if [ -n "$PLANTED" ] && [ -L "$PLANTED" ]; then rm -f "$PLANTED"; fi
  release_mounts
  drop_scratch
}

# A leaked daemon would hold a socket, an overlay mount per decoy and a process
# for every container it still knows about — so the trap is not decoration, and
# the suite asserts its own teardown rather than trusting it.
trap 'priv_stop' EXIT
trap 'priv_stop; exit 130' INT TERM HUP

# sandbox_init — the scratch tree and the two host logs. Split out of priv_start
# because the HOST section runs before the private daemon is started and needs a place
# to keep its logs (and the probe containers are created before the before-snapshot).
sandbox_init() {
  SB="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-bite.XXXXXX")"
  PRIV="$SB/docker.sock"
  PRIV_URL="unix://$PRIV"
  mkdir -p "$SB/root" "$SB/rootfs" "$SB/tmp"
  HOST_CALLS_LOG="$SB/host-calls.log"
  HOST_OWN_LOG="$SB/host-own.log"
  HOST_EVENTS_LOG="$SB/host-events.log"
  : >"$HOST_CALLS_LOG"
  : >"$HOST_OWN_LOG"
}

priv_start() {
  [ -n "$SB" ] || sandbox_init
  dockerd \
    --data-root "$SB/root" \
    --exec-root "$SB/exec" \
    --pidfile "$SB/dockerd.pid" \
    --host "$PRIV_URL" \
    --iptables=false --ip6tables=false --bridge=none \
    --log-level error >"$SB/dockerd.log" 2>&1 &
  DPID=$!
  local i=0
  while [ "$i" -lt 60 ]; do
    if priv info >/dev/null 2>&1; then return 0; fi
    kill -0 "$DPID" 2>/dev/null || break
    sleep 1
    i=$((i + 1))
  done
  printf 'FATAL: the private docker daemon did not come up in 60s\n' >&2
  sed 's/^/  dockerd: /' "$SB/dockerd.log" | tail -20 >&2
  exit 2
}

# A throwaway image with NO network and NO dependency on the host's images: a
# static busybox in a one-file tar, imported into the private daemon. 1 MB, and
# it cannot fail because a machine has an empty image store.
#
# THE TAG IS UNIQUE PER RUN, AND THAT IS LOAD-BEARING: the private daemon shares
# the host's containerd, so the image NAME is a host-wide key — two suites
# importing the same tag at the same time make the second one fail with
# `AlreadyExists: image "…": already exists` (measured 2026-10-12, two concurrent
# runs, one rc=1). The gate pool runs gates in parallel, so a fixed tag would
# flake. A per-run tag cannot collide, and it also makes a leaked image
# identifiable.
priv_image() {
  local tag="jf-prune-bite:${SB##*/}"
  if [ -n "${JF_PRUNE_BITE_IMAGE:-}" ]; then
    host image inspect "$JF_PRUNE_BITE_IMAGE" >/dev/null 2>&1 \
      || fatal "JF_PRUNE_BITE_IMAGE=$JF_PRUNE_BITE_IMAGE is not in the host daemon"
    req "save $JF_PRUNE_BITE_IMAGE from the host daemon" host save "$JF_PRUNE_BITE_IMAGE" -o "$SB/image.tar"
    # NOTE: `docker load` keeps the image's own name, so the override path is NOT
    # collision-proof between two concurrent runs (the primary busybox path is).
    # It exists for a host without busybox, and it fails loudly rather than
    # quietly measuring nothing.
    req "load $JF_PRUNE_BITE_IMAGE into the private daemon" priv load -i "$SB/image.tar"
    TRUE_CMD="true"
    SLEEP_CMD="sh -c 'sleep $SLEEP_SECS'"
    IMG="$JF_PRUNE_BITE_IMAGE"
    say "image: $JF_PRUNE_BITE_IMAGE (loaded from the host daemon)"
    return 0
  fi
  local bx
  bx="$(command -v busybox || true)"
  [ -n "$bx" ] || fatal "no busybox on PATH and no JF_PRUNE_BITE_IMAGE set — this suite needs one of them for a throwaway image"
  cp "$bx" "$SB/rootfs/busybox" || fatal "could not copy busybox into the scratch rootfs"
  tar -cf "$SB/image.tar" -C "$SB/rootfs" busybox || fatal "could not build the throwaway image tar"
  req "import the throwaway image" priv import "$SB/image.tar" "$tag"
  TRUE_CMD="/busybox true"
  SLEEP_CMD="/busybox sleep $SLEEP_SECS"
  IMG="$tag"
  say "image: $tag (busybox imported from $bx, no pull, no host image)"
}

# Run the guard under test against the PRIVATE daemon. Everything the guard needs
# is passed as an environment assignment ON THE COMMAND, so it can never fall
# back to the host socket: DOCKER_HOST is what the guard's own `docker` calls read.
# TMPDIR is this run's own empty directory, so the guard's PRIVATE rm-output file
# (see the fixed-path arm below) lands inside the sandbox and its removal can be
# asserted instead of assumed.
run_guard() { # <floor-hours> <dry-run 0|1> <out-file>
  local floor="$1" dry="$2" out="$3" rc=0
  DOCKER_HOST="$PRIV_URL" \
    TMPDIR="$SB/tmp" \
    PATH="$MKT_BIN:$PATH" \
    JF_PRUNE_MIN_AGE_H="$floor" \
    JF_PRUNE_DRY_RUN="$dry" \
    JF_PRUNE_KEEP_FILE="$KEEPFILE" \
    bash "$GUARD" >"$out" 2>&1 || rc=$?
  return "$rc"
}

# The same real run, with a `mktemp` that ALWAYS FAILS first on PATH: the arm that
# drives the guard's refusal to proceed without its private rm-output file.
run_guard_nomktemp() { # <out-file>
  local out="$1" rc=0
  DOCKER_HOST="$PRIV_URL" \
    TMPDIR="$SB/tmp" \
    PATH="$NOMKT_BIN:$PATH" \
    JF_PRUNE_MIN_AGE_H=0 \
    JF_PRUNE_DRY_RUN=0 \
    JF_PRUNE_KEEP_FILE="$KEEPFILE" \
    bash "$GUARD" >"$out" 2>&1 || rc=$?
  return "$rc"
}

# ------------------------------------------------------------------ run it
# THE NAMESPACE ARM, FIRST, and it FAILS CLOSED: nothing below this point has
# started a daemon, planted anything or removed anything, so a run that is in the
# CALLER's namespace stops here instead of deleting the caller's `docker0` and
# reporting on decoys it no longer controls. It is a comparison of two measured
# inodes rather than a marker check, because the marker is what a broken re-exec
# would still export: `exec unshare -n` replaced by `exec`, with the environment
# intact, leaves JF_PRUNE_CELL=1 set and lands in the caller's namespace all the
# same — which is exactly the mutation the bite proof drives.
if [ "${JF_PRUNE_CELL:-0}" != "1" ] || [ -z "$CALLER_NS" ] || [ -z "$OUR_NS" ] || [ "$OUR_NS" = "$CALLER_NS" ]; then
  bad "the private daemon would be started in the CALLER's network namespace (ours='${OUR_NS:-unreadable}', caller's='${CALLER_NS:-unset}'), where a --bridge=none dockerd DELETES the caller's docker0"
  say ""
  say "REFUSING to go further: the namespace arm fails closed, so a lost namespace cannot"
  say "damage the host. Nothing has been started, planted or removed at this point."
  say "RESULT: $FAILS of $CHECKS checks FAILED"
  exit 1
fi
ok "the suite and its private daemon run in their OWN network namespace ($OUR_NS), not the caller's ($CALLER_NS)"

# The instrument the bridge checks below depend on, proven before it is trusted: a
# descriptor that no longer points at a namespace would make every docker0 reading
# empty, and an empty reading compared against an empty one is a green that
# measures nothing.
ifaces="$(host_ip -o link show 2>/dev/null | wc -l | tr -d ' ')"
[ "${ifaces:-0}" -ge 1 ] \
  || fatal "the CALLER's network namespace could not be read through fd '$HOSTNS_FD' ($ifaces interfaces) — the caller's docker0 cannot be measured from here"
ok "the caller's namespace is readable through fd $HOSTNS_FD ($ifaces interfaces), so its docker0 is MEASURED rather than assumed"
HOST_D0_BEFORE="$(host_d0_id)"

[ "$(id -u)" = 0 ] || fatal "this suite must run as root: it starts a private docker daemon"
[ -f "$GUARD" ] || fatal "$GUARD not found"
[ -f "$INSTALLER" ] || fatal "$INSTALLER not found"
bash -n "$GUARD" || fatal "$GUARD is not valid bash"
bash -n "$INSTALLER" || fatal "$INSTALLER is not valid bash"
command -v dockerd >/dev/null 2>&1 || fatal "dockerd not on PATH — the private daemon cannot be started"
host info >/dev/null 2>&1 || fatal "the HOST docker daemon is unreachable ($HOST_SOCK) — 'nothing else was touched' could not be proven without it"

# The host's own container and image sets, so the claim "this suite removed nothing
# outside its sandbox" is a measurement. Peers may ADD during the run; losing one of
# these is what the attribution below has to explain. Read with --no-trunc: the CLI
# truncates ids in its own output, and the event stream reports them in full, so a
# truncated snapshot would never match an event.
say ""
say "== the HOST daemon: the before-snapshot, and the shared-daemon attribution arm =="
sandbox_init
HOST_ID_BEFORE="$(host info --format '{{.ID}}')"
# 1. The event window opens BEFORE the snapshot, so nothing that happens inside the
#    window can land before it. (The query is made at the END of the run, over this
#    window: one process fewer to leak, and no reliance on a stream staying alive.)
HOST_EVENTS_FROM="$(date -u -d '@'$(( $(date +%s) - 2 )) +%Y-%m-%dT%H:%M:%SZ)"
# 2. This run's OWN probe containers on the host daemon. The image is an EMPTY rootfs
#    imported from a tar built here — no pull, no network, no dependency on the host's
#    own images — and the containers are created, never started: no process, no
#    network, nothing but a name and a few bytes. They are removed in priv_stop.
tar -cf "$SB/hostprobe.tar" --files-from /dev/null \
  || fatal "could not build the empty-rootfs tar for the host probe image"
PEER_PROBE="jf-prune-peerprobe-$$"
CONTROL_PROBE="jf-prune-controlprobe-$$"
host_own_import "jf-prune-probe:${SB##*/}" "$SB/hostprobe.tar" \
  || fatal "could not import the probe image into the HOST daemon — the shared-daemon arm cannot run"
host_own_create "$PEER_PROBE" PEER_ID \
  || fatal "could not plant '$PEER_PROBE' on the HOST daemon"
host_own_create "$CONTROL_PROBE" CONTROL_ID \
  || fatal "could not plant '$CONTROL_PROBE' on the HOST daemon"
# 3. The before-snapshot, ids and names, taken AFTER the probes exist: the probes are
#    part of "existed before this run" exactly like every other container on the host.
host ps -a -q --no-trunc | sort >"$SB/host-before-ids.txt"
host ps -a --no-trunc --format '{{.ID}}|{{.Names}}' | sort >"$SB/host-before-names.txt"
host images -q --no-trunc | sort >"$SB/host-images-before.txt"
eq "$(grep -c -x -e "$PEER_ID" -e "$CONTROL_ID" "$SB/host-before-ids.txt" || true)" "2" \
  "both probe containers are in the before-snapshot (so the arm measures the same set the assertion does)"
# 4. THE PEER. A separate process — a plain `bash -c` calling the docker CLI directly,
#    which is exactly the shape of the redeploy that produced the false red — removes
#    the peer probe mid-run. It is NOT this run's audited removal path, so this run's
#    own log cannot contain it: the attribution has to name it FOREIGN from the host
#    daemon's own event stream.
PEER_DELAY=6
(
  bash -c 'sleep "$1"; exec docker --host "$2" rm -f "$3"' _ "$PEER_DELAY" "$HOST_SOCK" "$PEER_PROBE" \
    >/dev/null 2>&1
  printf '%s\n' "$?" >"$SB/peer.rc"
) &
PEER_PID=$!
say "peer: a separate process removes '$PEER_PROBE' in ${PEER_DELAY}s, mid-run — the shared-daemon"
say "      interference this suite must attribute rather than blame on the tree"

say ""
say "== this run's own namespace: the decoy docker0 the private daemon will delete =="
# WHAT THE DECOY IS FOR. The daemon deletes the default bridge of whatever
# namespace it runs in, so this namespace's `docker0` is planted here — before the
# daemon exists — and the arm below REQUIRES that it is gone afterwards. Without
# that requirement "the caller's docker0 survived" would credit the namespace for
# something a future docker might simply have stopped doing; with it, the survival
# is attributable to the namespace and nothing else. It is also why this suite
# cannot be run in a namespace that already has a bridge: the decoy would not be
# its own, and the requirement would be unfalsifiable.
if ip -o link show docker0 >/dev/null 2>&1; then
  bad "this run's own namespace already had a docker0 before the private daemon started — the decoy would not be this run's own, so a deletion here would prove nothing"
else
  ok "this run's own namespace has no docker0 yet (so the one planted below is the only bridge this run can lose)"
fi
ip link add docker0 type bridge \
  || fatal "could not plant the decoy docker0 in this run's own namespace (is the bridge kernel module available?)"
ip addr add 172.17.0.1/16 dev docker0 || fatal "could not address the decoy docker0"
ip link set docker0 up || fatal "could not bring the decoy docker0 up"
DECOY_D0="$(ip -o link show docker0 | sed -n 's/^\([0-9][0-9]*\): docker0:.*link\/ether \([0-9a-f:][0-9a-f:]*\).*/\1 \2/p')"
[ -n "$DECOY_D0" ] || fatal "could not read the decoy docker0's identity back after planting it"
say "decoy: this namespace's docker0 -> $DECOY_D0; the caller's is '$HOST_D0_BEFORE'"

say ""
say "== the private daemon =="
priv_start
# --host must win over DOCKER_HOST, or the isolation claim is wrong. Two daemons
# answer with different daemon ids; one daemon answering both means the suite
# would be reading the host through the flag and the private one by default.
export DOCKER_HOST="$PRIV_URL"
host_id="$(host info --format '{{.ID}}')"
priv_id="$(docker info --format '{{.ID}}')"
if [ -z "$host_id" ] || [ -z "$priv_id" ] || [ "$host_id" = "$priv_id" ]; then
  fatal "--host and DOCKER_HOST resolved to the same daemon (host='$host_id' default='$priv_id') — the sandbox is not isolated"
fi
ok "the private daemon is isolated from the host daemon (--host wins over DOCKER_HOST)"

# THE NETNS CLAIM, in three parts, immediately after the daemon has started —
# which is when a `--bridge=none` dockerd deletes a bridge. (1) the daemon process
# itself is inside this run's namespace; (2) it deleted a docker0, namely this
# run's own decoy, so the destructive behaviour is LIVE here and what spares the
# caller is the namespace and not a docker that changed its mind; (3) the caller's
# docker0 is the SAME interface it was — same ifindex, same MAC — so a recreated
# bridge somewhere else cannot read as a bridge that survived.
eq "$(readlink "/proc/$DPID/ns/net" 2>/dev/null)" "$OUR_NS" \
   "the private daemon itself runs inside this run's own network namespace ($OUR_NS)"
eq "$(ip -o link show docker0 >/dev/null 2>&1 && echo present || echo gone)" "gone" \
   "the private daemon DELETED the docker0 of its OWN namespace ($DECOY_D0 is gone) — --bridge=none really does this, so what spares the caller's is the namespace"
host_d0_intact "the caller's docker0 survived the private daemon's start, unchanged from '$HOST_D0_BEFORE' (same ifindex and MAC)"
priv_image
KEEPFILE="$SB/keep-containers"
printf '%s\n' "$D_KEEPFILE" >"$KEEPFILE"

# A `mktemp` on PATH that RECORDS what the guard asked for and what it got, then
# delegates to the real one. This is what makes the guard's private rm-output
# file a MEASUREMENT rather than an assumption: the arm asserts the template the
# guard asked for (a path under this run's own TMPDIR) and the created file's own
# identity (a regular file — not a followed symlink — mode 0600, owned by root).
# Nothing else in a guard pass
# calls the `mktemp` binary, so the log holds exactly one entry after a real run
# and zero after the dry runs.
MKT_BIN="$SB/mkt-bin"
RM_MKT_LOG="$SB/mktemp.invocations"
REAL_MKTEMP="$(command -v mktemp)"
[ -n "$REAL_MKTEMP" ] || fatal "no mktemp on PATH — the guard's private rm-output file cannot be measured"
mkdir -p "$MKT_BIN"
: >"$RM_MKT_LOG"
cat >"$MKT_BIN/mktemp" <<WRAPPER
#!/usr/bin/env bash
# Log the template, the created path and that path's own identity, then answer
# exactly as the real mktemp would (same args, same stdout, same status).
out="\$(exec "$REAL_MKTEMP" "\$@")" || exit \$?
printf 'template=%s\nfile=%s\nkind=%s\nmode=%s\nowner=%s\n' \
  "\$*" "\$out" "\$(stat -c '%F' "\$out")" "\$(stat -c '%a' "\$out")" "\$(stat -c '%U' "\$out")" >>"$RM_MKT_LOG"
printf '%s\n' "\$out"
WRAPPER
chmod 0755 "$MKT_BIN/mktemp"
chmod 0644 "$RM_MKT_LOG"
# The wrapper is load-bearing for an assertion below, so prove it delegates: a
# wrapper that failed to answer would abort the real run and red the suite for the
# wrong reason, and one that never logged would make the arm vacuous.
probe="$(PATH="$MKT_BIN:$PATH" mktemp "$SB/tmp/probe.XXXXXX")" || fatal "the delegating mktemp wrapper did not answer"
eq "$(grep -c '^template=' "$RM_MKT_LOG" || true)" "1" "the mktemp wrapper logs the call and delegates to the real mktemp"
[ -f "$probe" ] && ok "the wrapper's own probe file exists (it created a real file)" || bad "the wrapper's probe file was not created"
rm -f "$probe"
: >"$RM_MKT_LOG"

# ---------------------------------------------------------------- fail closed
say ""
say "== fail closed on an EMPTY container list (nothing removed) =="
OUT="$SB/empty.out"
rc=0; run_guard 0 1 "$OUT" || rc=$?
eq "$rc" "1" "an empty container list exits non-zero"
has "$OUT" "container list is EMPTY" "it says WHY, instead of quietly finding nothing to do"
eq "$(priv ps -a -q | wc -l | tr -d ' ')" "0" "nothing was removed"

# --------------------------------------------------------------------- decoys
say ""
say "== the decoys (all created in the private daemon) =="
req "create decoy $D_YOUNG" priv run --network none --name "$D_YOUNG" "$IMG" $TRUE_CMD
req "create decoy $D_PLAIN" priv run --network none --name "$D_PLAIN" "$IMG" $TRUE_CMD
req "create decoy $D_COMPOSE" priv create --label com.docker.compose.project=jomform --name "$D_COMPOSE" "$IMG" $TRUE_CMD
req "create decoy $D_KEEPFILE" priv create --name "$D_KEEPFILE" "$IMG" $TRUE_CMD
req "start decoy $D_RUNNING" priv run -d --network none --name "$D_RUNNING" "$IMG" $SLEEP_CMD
state_young="$(priv inspect --format '{{.State.Status}}' "$D_YOUNG")"
state_plain="$(priv inspect --format '{{.State.Status}}' "$D_PLAIN")"
state_compose="$(priv inspect --format '{{.State.Status}}' "$D_COMPOSE")"
state_keep="$(priv inspect --format '{{.State.Status}}' "$D_KEEPFILE")"
state_running="$(priv inspect --format '{{.State.Status}}' "$D_RUNNING")"
eq "$state_young" "exited" "$D_YOUNG is a stopped (exited) container — the leak class from prod"
eq "$state_plain" "exited" "$D_PLAIN is a stopped (exited) container"
eq "$state_compose" "created" "$D_COMPOSE is a stopped (created) container"
eq "$state_keep" "created" "$D_KEEPFILE is a stopped (created) container"
eq "$state_running" "running" "$D_RUNNING is RUNNING"
# Floor 0 compares the container's CreatedAt SECOND against now, so a decoy and
# the guard in the same second read as "young". Wait past that before the
# floor-0 arms, or the age gate would spare the decoy the guard is meant to take.
sleep 2

# ------------------------------------------------------- the AGE gate (168h)
say ""
say "== the AGE gate: floor 168h, every decoy is minutes old =="
OUT="$SB/age.out"
rc=0; run_guard 168 1 "$OUT" || rc=$?
eq "$rc" "0" "the default floor exits 0"
has "$OUT" "inspected=5" "all five decoys were inspected"
has "$OUT" "candidates=0" "none of them is a candidate"
# The named decoy is counted under `keep` and not under `young` because the
# guard's name gate is checked BEFORE its age gate (its own order); the three
# that reach the age check are the two unlabelled stopped decoys and the
# compose-labelled one.
has "$OUT" "skipped young=3 keep=1 compose=0" "the stopped decoys were spared, the keep-file one by name and the rest by age"
has "$OUT" "nothing to remove" "it says so"
lacks "$OUT" "candidates:" "no candidate was ever listed (so the running decoy was not either)"

# Same decoys, floor 0: the UNLABELLED stopped decoys become candidates. That is
# the age gate's discrimination — the only thing that changed between the two
# runs is the floor, so "young was spared" is attributable to the age check and
# not to a label or the keep file.
say ""
say "== the AGE gate, other side: the same decoys at floor 0 ARE candidates =="
OUT="$SB/floor0-young.out"
rc=0; run_guard 0 1 "$OUT" || rc=$?
eq "$rc" "0" "the floor-0 dry run exits 0"
cand="$(grep -m1 'candidates: ' "$OUT" | sed 's/.*candidates: //')"
eq "$(printf '%s\n' $cand | sort | tr '\n' ' ' | sed 's/ $//')" \
   "$(printf '%s\n%s\n' "$D_PLAIN" "$D_YOUNG" | sort | tr '\n' ' ' | sed 's/ $//')" \
   "the two unlabelled stopped decoys are candidates at floor 0 (and were NOT at 168h)"
lacks "$OUT" "$D_RUNNING" "the RUNNING decoy is still not a candidate at floor 0"

# ---------------------------- the STATE / COMPOSE / KEEP gates (floor 0, dry)
say ""
say "== the STATE, COMPOSE and KEEP gates: floor 0, dry run =="
priv rm -f "$D_YOUNG" >/dev/null 2>&1 || true   # its one job (the age arm) is done
OUT="$SB/floor0-dry.out"
rc=0; run_guard 0 1 "$OUT" || rc=$?
eq "$rc" "0" "the floor-0 dry run exits 0"
has "$OUT" "keep (name): $D_KEEPFILE" "the keep-file decoy is spared by NAME"
has "$OUT" "keep (compose project=jomform): $D_COMPOSE" "the compose-labelled decoy is spared by LABEL"
lacks "$OUT" "$D_RUNNING" "the RUNNING decoy is never listed as a candidate"
has "$OUT" "DRY RUN - nothing removed" "the dry run removed nothing"
cand="$(grep -m1 'candidates: ' "$OUT" | sed 's/.*candidates: //')"
eq "$cand" "$D_PLAIN" "the sole candidate is the plain stopped decoy"
eq "$(priv ps -a -q | wc -l | tr -d ' ')" "4" "all four remaining decoys survived the dry run"
if grep -qE 'jomform-(api|web|postgres|valkey|minio)-1|traefik-traefik-1' "$OUT"; then
  bad "a LIVE STACK name appeared in the guard's output"
else
  ok "no live stack container name appeared in the guard's output"
fi

# --------------------- the private rm-output file (the fixed /tmp path defect)
# WHAT THIS PINS, and why. The guard writes its `docker rm` output to a file it
# creates itself (`mktemp`, random name, O_EXCL, mode 0600). The version of this
# guard that shipped first (card `t_13f1c904`) redirected that output to a FIXED
# path in a world-writable directory — `/tmp/jomform-container-prune.rm.out` —
# and the redirect is opened by root BEFORE `docker rm` runs, so any local user
# who creates that name first (a symlink to a file root can write) made root
# truncate and replace the target. Reproduced by card `t_3d34df24` against the
# shipped bytes: victim sha256 89f2522a… -> f5120dc7….
#
# THE TWO HALVES ARE DRIVEN IN THIS ORDER ON PURPOSE. The refusal arm below is a
# real floor-0 pass that removes a candidate OF ITS OWN, and this suite's first
# draft ran it FIRST — which made the sabotage proof come out DID NOT BITE,
# measured on 2026-10-12: with the fixed path restored, the refusal arm removed
# the ONE candidate the planted-symlink arm depends on, so that arm then found
# `nothing to remove`, never reached the removal step, and left the victim
# untouched (the guard still exited 0, so nothing else caught it either). A guard
# that removes nothing cannot damage anything, so the symlink arm only measures
# something when a removal really happens first.
say ""
say "== the private rm-output file: never a fixed path, and fail closed without one =="
eq "$(grep -c '^template=' "$RM_MKT_LOG" || true)" "0" \
   "no rm-output file was created by the dry runs or by an earlier refusal (only a real removal creates one)"

# The planted symlink. It is planted at the path the guard USED TO write to, and
# the assertion is about the VICTIM's bytes rather than about the symlink itself:
# a fixed path cannot be owned exclusively, so a second concurrent run of this
# suite may replace the link, and a false red on a healthy host would be worse
# than the (only-in-concurrency) weaker claim. The real floor-0 run below is what
# would follow it.
PLANTED=/tmp/jomform-container-prune.rm.out
VICTIM="$SB/planted-symlink-victim.txt"
printf '%s\n' 'THIS FILE MUST NOT BE TOUCHED BY A CONTAINER PRUNE' >"$VICTIM"
victim_before="$(sha256sum "$VICTIM" | awk '{print $1}')"
ln -sfn "$VICTIM" "$PLANTED"
say "planted: $PLANTED -> $(readlink "$PLANTED" 2>/dev/null || echo '(the link was not planted)')"
eq "$(readlink "$PLANTED" 2>/dev/null)" "$VICTIM" "the decoy symlink is in place (the victim really is exposed)"

# ---------------------------------------------------- the guard BITES (floor 0)
say ""
say "== the guard BITES: floor 0, real run =="
OUT="$SB/floor0-real.out"
rc=0; run_guard 0 0 "$OUT" || rc=$?
eq "$rc" "0" "the real run exits 0"
has "$OUT" "removed=1 containers (4 -> 3)" "exactly one container was removed, counted before and after"
eq "$(priv ps -a --format '{{.Names}}' | grep -c "^$D_PLAIN$" || true)" "0" "$D_PLAIN is gone"
eq "$(priv inspect --format '{{.State.Status}}' "$D_RUNNING" 2>/dev/null)" "running" "the RUNNING decoy survived a REAL floor-0 run and is still running"
eq "$(priv inspect --format '{{.State.Status}}' "$D_COMPOSE" 2>/dev/null)" "created" "the compose-labelled decoy survived"
eq "$(priv inspect --format '{{.State.Status}}' "$D_KEEPFILE" 2>/dev/null)" "created" "the keep-file decoy survived"
eq "$(priv ps -a --format '{{.State}}' | sort | uniq -c | awk '{printf "%s %s;", $1, $2}')" \
   "2 created;1 running;" "the daemon holds exactly the three survivors (2 stopped, 1 running)"
leftover="$(priv ps -a --format '{{.Names}}' | grep -cv "^$DECOY-" || true)"
eq "$leftover" "0" "every container in the private daemon is one of this suite's decoys"

# The half the planted symlink exists for: the guard's one destructive pass ran,
# and the file the guard USED to open for writing is exactly as it was.
eq "$(sha256sum "$VICTIM" | awk '{print $1}')" "$victim_before" \
   "the planted symlink's target was NOT written through (the fixed /tmp path is never opened)"
has "$VICTIM" "MUST NOT BE TOUCHED" "the victim's original text is intact, byte for byte"
# ...and the positive half: the file the guard DID open was its own, created
# through mktemp under this run's TMPDIR, regular, 0600, owned by root.
eq "$(grep -c '^template=' "$RM_MKT_LOG" || true)" "1" "the real pass created its rm-output file exactly once"
mkt_template="$(grep -m1 '^template=' "$RM_MKT_LOG" | cut -d= -f2-)"
mkt_file="$(grep -m1 '^file=' "$RM_MKT_LOG" | cut -d= -f2-)"
case "$mkt_template" in
  "$SB"/tmp/*) ok "the guard asked mktemp for a PRIVATE path under its own TMPDIR ($mkt_template)";;
  *)           bad "the guard asked mktemp for '$mkt_template' — not a private path under $SB/tmp";;
esac
case "$mkt_file" in
  "$SB"/tmp/*) ok "the rm output really was written to that private file ($mkt_file)";;
  *)           bad "the rm output's file was '$mkt_file' — not under $SB/tmp";;
esac
mkt_kind="$(grep -m1 '^kind=' "$RM_MKT_LOG" | cut -d= -f2-)"
case "$mkt_kind" in
  regular*) ok "the rm-output path is a regular file ($mkt_kind), not a followed symlink";;
  *)        bad "the rm-output path is a '$mkt_kind' — not a regular file";;
esac
eq "$(grep -m1 '^mode=' "$RM_MKT_LOG" | cut -d= -f2-)" "600" \
   "it is mode 0600 — nobody but root can read root's rm output"
eq "$(grep -m1 '^owner=' "$RM_MKT_LOG" | cut -d= -f2-)" "root" "it is owned by root"
eq "$(find "$SB/tmp" -maxdepth 1 -name 'jomform-container-prune.rm.*' | wc -l | tr -d ' ')" "0" \
   "the guard deleted its own rm-output file before exiting (nothing left in TMPDIR)"

# ---- the refusal arm, AFTER the real run (see the order comment above): it needs
# a candidate of its own, because the real run above has just taken $D_PLAIN.
say ""
say "== the private rm-output file is REQUIRED: a pass that cannot create one removes NOTHING =="
req "create decoy $D_NOMKT" priv run --network none --name "$D_NOMKT" "$IMG" $TRUE_CMD
eq "$(priv inspect --format '{{.State.Status}}' "$D_NOMKT")" "exited" \
   "$D_NOMKT is a stopped container — the leak class this arm's floor-0 pass would take"
# Floor 0 compares the CreatedAt SECOND against now, so wait past that or the age
# gate would spare the decoy and the arm would measure the age gate instead.
sleep 2
NOMKT_BIN="$SB/nomkt-bin"
mkdir -p "$NOMKT_BIN"
cat >"$NOMKT_BIN/mktemp" <<'FAKEMKT'
#!/usr/bin/env bash
# An always-failing `mktemp`: this arm measures the guard's refusal, and it must
# not depend on the real TMPDIR being full or read-only.
echo "fake mktemp: refusing to create $*" >&2
exit 1
FAKEMKT
chmod 0755 "$NOMKT_BIN/mktemp"
# The arm's premise, verified rather than assumed: if the fake did NOT fail, the
# arm would be measuring the real mktemp and would pass for the wrong reason.
rc=0; PATH="$NOMKT_BIN:$PATH" mktemp >/dev/null 2>&1 || rc=$?
eq "$rc" "1" "the arm's fake mktemp really fails (the arm measures the guard)"
OUT="$SB/nomktemp.out"
rc=0; run_guard_nomktemp "$OUT" || rc=$?
eq "$rc" "1" "no private rm-output file means a non-zero exit"
has "$OUT" "could not create a private rm-output file" "it says WHY, and calls it fail-closed"
has "$OUT" "nothing removed" "it states that nothing was removed"
eq "$(priv ps -a --format '{{.Names}}' | grep -c "^$D_NOMKT$" || true)" "1" \
   "NOTHING was removed: the candidate SURVIVED the refusal (a real floor-0 pass that removes nothing)"
eq "$(priv ps -a --format '{{.Names}}' | grep -cv "^$DECOY-" || true)" "0" \
   "the private daemon still holds only this suite's decoys"

# ------------------------------------------------------- the host was not touched
say ""
say "== the HOST daemon was not touched (ATTRIBUTED, not counted) =="
# The peer long since fired; waiting here is what makes the arm deterministic rather
# than a race with the rest of the suite.
if [ -n "${PEER_PID:-}" ]; then wait "$PEER_PID" 2>/dev/null || true; fi
peer_rc="$(cat "$SB/peer.rc" 2>/dev/null || echo missing)"
eq "$peer_rc" "0" "the PEER process really removed its probe container on the HOST daemon (rc=$peer_rc)"

host ps -a -q --no-trunc | sort >"$SB/host-after-ids.txt"
host images -q --no-trunc | sort >"$SB/host-images-after.txt"
HOST_ID_AFTER="$(host info --format '{{.ID}}')"
# The event window, over the whole run. `--until` two seconds ahead so a destroy that
# lands while this line is being composed is still inside it; the daemon's per-action
# filters keep the answer to what is needed for attribution (a busy host emits an
# `exec_*` event per health check, which is none of this suite's business).
HOST_EVENTS_UNTIL="$(date -u -d '@'$(( $(date +%s) + 2 )) +%Y-%m-%dT%H:%M:%SZ)"
host events --since "$HOST_EVENTS_FROM" --until "$HOST_EVENTS_UNTIL" \
  --filter type=container --filter type=image \
  --filter event=destroy --filter event=delete --filter event=untag \
  --format '{{.Time}}|{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' \
  >"$HOST_EVENTS_LOG" 2>&1 || true
say "window: $HOST_EVENTS_FROM .. $HOST_EVENTS_UNTIL; $(grep -c . "$HOST_EVENTS_LOG" || true) event line(s)"

# THE SELF-CHECK the attribution rests on: one raw call site, and the read-only door
# never issued a destructive verb (it refuses them, so this is a tripwire on the file
# rather than a hope about the run).
raw_host_sites="$(grep -cE '(^|[^a-zA-Z_])docker[[:space:]]+--host[[:space:]]+"\$HOST_SOCK"' "${BASH_SOURCE[0]}")"
eq "$raw_host_sites" "1" \
  "this suite reaches the HOST daemon through exactly ONE raw call site (host_docker), so 'this run issued no other removal' is a property of the file"
eq "$(grep -cE '\|(rm|rmi|kill|stop|restart|prune|create|run|start|import|load|tag|commit|build|push)( |$)' "$HOST_CALLS_LOG" || true)" "0" \
  "the READ-ONLY door issued no destructive host command during this run (every host call is logged, and the door refuses those verbs)"
eq "$(grep -c '^[0-9]*|rm|' "$HOST_OWN_LOG" || true)" "0" \
  "this run's own audited removal path removed NO host container before the attribution was computed (its probes are taken down after it)"

# THE INSTRUMENT, before it is believed: the three verdicts against a fixture, so a
# classifier that stops distinguishing them is caught even on a host where nothing at
# all was lost (the live arm below only ever exercises FOREIGN).
FIXTURE="$SB/host-attribution-fixture"
FIX_PEER="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
FIX_OWN="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
FIX_NONE="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
printf '1000|container|destroy|%s|fixture-peer\n' "$FIX_PEER" >"$FIXTURE.events"
eq "$(HOST_EVENTS_LOG="$FIXTURE.events" HOST_OWN_REMOVED='' attribution_of_container "$FIX_PEER")" "FOREIGN" \
   "classifier self-test: a destroy event with no removal by this run is FOREIGN (another actor did it)"
eq "$(HOST_EVENTS_LOG="$FIXTURE.events" HOST_OWN_REMOVED="$FIX_OWN" attribution_of_container "$FIX_OWN")" "OURS" \
   "classifier self-test: an id this run's own audited path removed is OURS (not FOREIGN, whatever else the window says)"
eq "$(HOST_EVENTS_LOG="$FIXTURE.events" HOST_OWN_REMOVED='' attribution_of_container "$FIX_NONE")" "UNATTRIBUTED" \
   "classifier self-test: a loss with neither an event nor a removal of this run's is UNATTRIBUTED — the shape that fails"

# ---- containers: every loss is attributed, one check each, and the failing shape
# NAMES the container and says what happened to it. The bare "(want '0', got '1')" this
# replaces is what made a reader chase the prod prune guard instead of the shared
# daemon (card `t_da7509c4`).
comm -23 "$SB/host-before-ids.txt" "$SB/host-after-ids.txt" >"$SB/host-lost.txt"
: >"$SB/host-attribution.txt"
while IFS= read -r cid; do
  [ -n "$cid" ] || continue
  cname="$(awk -F'|' -v id="$cid" '$1==id {print $2; exit}' "$SB/host-before-names.txt")"
  [ -n "$cname" ] || cname="(name not in the before-snapshot)"
  cshort="${cid:0:12}"
  cev="$(host_event_time "$cid")"
  when="unknown"
  if [ -n "$cev" ]; then when="$(date -u -d "@$cev" +%H:%M:%SZ 2>/dev/null || printf '%s' "$cev")"; fi
  case "$(attribution_of_container "$cid")" in
    OURS)
      printf 'OURS|%s|%s\n' "$cid" "$cname" >>"$SB/host-attribution.txt"
      bad "host container '$cname' ($cshort) existed before this run and was removed by THIS RUN's own audited removal path at $when — the suite touched the host" ;;
    FOREIGN)
      printf 'FOREIGN|%s|%s|%s\n' "$cid" "$cname" "$when" >>"$SB/host-attribution.txt"
      ok "host container '$cname' ($cshort) was removed at $when by ANOTHER actor on the shared daemon — FOREIGN, not this tree's failure (this run recorded no removal of it)" ;;
    *)
      printf 'UNATTRIBUTED|%s|%s\n' "$cid" "$cname" >>"$SB/host-attribution.txt"
      bad "host container '$cname' ($cshort) existed before this run and is GONE, and its removal CANNOT be attributed: this run recorded no removal of it and the HOST daemon recorded no destroy event for it — the loss is not this tree's, but the window cannot say who did it ($(window_state))" ;;
  esac
done <"$SB/host-lost.txt"
# THE ARM ITSELF, asserted rather than assumed: the probe the peer removed must be
# classified FOREIGN and NAMED, and the control probe (which no peer touched) must
# still be there — so the diff is specific, not "everything that is missing is
# someone else's".
eq "$(awk -F'|' -v id="$PEER_ID" '$1=="FOREIGN" && $2==id {n++} END{print n+0}' "$SB/host-attribution.txt")" "1" \
  "the probe container the PEER removed mid-run is attributed FOREIGN and NAMED — the shared-daemon false red is measured on every run"
eq "$(host ps -a -q --no-trunc | grep -c -x "$CONTROL_ID" || true)" "1" \
  "the control probe container (which no peer touched) is still on the host daemon"

# ---- images: same three verdicts. A peer's `docker image prune` or a redeploy that
# rebuilds is the same shared-daemon shape, and it used to red the same bare count.
comm -23 "$SB/host-images-before.txt" "$SB/host-images-after.txt" >"$SB/host-images-lost.txt"
while IFS= read -r iid; do
  [ -n "$iid" ] || continue
  ishort="$(printf '%s' "$iid" | sed 's/^sha256://' | cut -c1-12)"
  iat="$(host_event_time "$iid")"
  iwhen="unknown"
  if [ -n "$iat" ]; then iwhen="$(date -u -d "@$iat" +%H:%M:%SZ 2>/dev/null || printf '%s' "$iat")"; fi
  case "$(attribution_of_image "$iid")" in
    FOREIGN)
      ok "host image $ishort was deleted at $iwhen by ANOTHER actor on the shared daemon — FOREIGN, not this tree's failure (this run recorded no deletion of it)" ;;
    *)
      bad "host image $ishort existed before this run and is GONE, and its deletion CANNOT be attributed: this run recorded no deletion of it and the HOST daemon recorded no delete event for it ($(window_state))" ;;
  esac
done <"$SB/host-images-lost.txt"
eq "$(host images -q --no-trunc | grep -c -x "$HOST_OWN_IMAGE_ID" || true)" "1" \
  "the probe image this run imported is still in the host daemon (this run deleted nothing there before this check)"

eq "$(host ps -a --format '{{.Names}}' | grep -c "^$DECOY-" || true)" "0" "no decoy ever existed on the host daemon"
# The bridge, at the END of the run as well: this is the caller's view of the whole
# suite, which is what the next lane's `docker run --network bridge` needs. (The
# same measurement right after the daemon started is what ATTRIBUTES a failure to
# this suite rather than to some other actor on the host.)
host_d0_intact "the caller's docker0 is still there after the whole suite, and is the same interface (ifindex and MAC) it started as"

# --------------------------------------------------------------- the installer
say ""
say "== the installer: idempotent, sha-verified, and dry-run writes nothing =="
IX="$SB/install"
FAKEBIN="$IX/fakebin"
mkdir -p "$FAKEBIN" "$IX/prefix" "$IX/log"
export FAKE_CRONTAB_DB="$IX/crontab.db"
export FAKE_CRONTAB_LOG="$IX/crontab.invocations"
: >"$FAKE_CRONTAB_LOG"

# The pre-existing crontab content: a re-install must preserve it and append
# exactly one line of its own.
printf '%s\n' '# JomForm nightly backup -> Google Drive. Logs to /var/log/jomform-backup.log.' \
  '17 2 * * * /usr/local/bin/jomform-backup.sh >> /var/log/jomform-backup.log 2>&1' >"$FAKE_CRONTAB_DB"

cat >"$FAKEBIN/crontab" <<'FAKE'
#!/usr/bin/env bash
# fake `crontab` for the sandbox: a file IS the crontab, and every call is logged.
printf 'INVOKED %s\n' "$*" >>"$FAKE_CRONTAB_LOG"
case "${1:-}" in
  -l) [ -f "$FAKE_CRONTAB_DB" ] || exit 1; cat "$FAKE_CRONTAB_DB" ;;
  -)  cat >"$FAKE_CRONTAB_DB" ;;
  *)  echo "fake crontab: unsupported argument: $*" >&2; exit 2 ;;
esac
FAKE
chmod 0755 "$FAKEBIN/crontab"
# An `ssh` that runs the command LOCALLY and an `scp` that copies LOCALLY: the
# sandbox is the "remote host", so the installer's remote code path is driven
# for real instead of being shipped unexercised.
cat >"$FAKEBIN/ssh" <<'FAKE'
#!/usr/bin/env bash
# fake `ssh`: drop the options and the target, run the command here.
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
shift || exit 2
exec bash -c "$*"
FAKE
chmod 0755 "$FAKEBIN/ssh"
cat >"$FAKEBIN/scp" <<'FAKE'
#!/usr/bin/env bash
# fake `scp`: drop flags, then copy src to the path after "host:".
args=()
while [ $# -gt 0 ]; do
  case "$1" in -*) shift ;; *) args+=("$1"); shift ;; esac
done
src="${args[0]:-}"; dst="${args[1]:-}"
[ -n "$src" ] && [ -n "$dst" ] || exit 2
dst="${dst#*:}"
cp "$src" "$dst"
FAKE
chmod 0755 "$FAKEBIN/scp"

path_with_fake() { printf '%s' "$FAKEBIN:$PATH"; }
# Refuse to go further if the fake is not the crontab that will be FOUND: with
# the real one first, a sandboxed install would edit the root crontab.
found="$(PATH="$(path_with_fake)" command -v crontab)"
eq "$found" "$FAKEBIN/crontab" "the sandbox's fake crontab is the one on PATH (the root crontab cannot be reached)"

SANDBOX_LINE="40 3 * * * $IX/prefix/jomform-container-prune.sh >> $IX/log/prune.log 2>&1"

PATH="$(path_with_fake)" bash "$INSTALLER" --prefix "$IX/prefix" --log "$IX/log/prune.log" >"$IX/run1.out" 2>&1
eq "$?" "0" "install (run 1) exits 0"
eq "$(sha256sum "$IX/prefix/jomform-container-prune.sh" | awk '{print $1}')" \
   "$(sha256sum "$GUARD" | awk '{print $1}')" "the installed copy is byte-identical to the repository copy"
eq "$(stat -c '%a' "$IX/prefix/jomform-container-prune.sh")" "755" "the installed copy is 0755"
eq "$(test -f "$IX/log/prune.log" && echo yes || echo no)" "yes" "the log file was created"
eq "$(grep -cxF -- "$SANDBOX_LINE" "$FAKE_CRONTAB_DB" || true)" "1" "the cron entry is present exactly once"
eq "$(grep -c . "$FAKE_CRONTAB_DB")" "3" "the pre-existing crontab content was preserved (2 lines + ours)"
eq "$(grep -cxF '# JomForm nightly backup -> Google Drive. Logs to /var/log/jomform-backup.log.' "$FAKE_CRONTAB_DB" || true)" "1" "the nightly backup's comment line survived"
eq "$(grep -c '^INVOKED -$' "$FAKE_CRONTAB_LOG" || true)" "1" "the crontab was WRITTEN once"

PATH="$(path_with_fake)" bash "$INSTALLER" --prefix "$IX/prefix" --log "$IX/log/prune.log" >"$IX/run2.out" 2>&1
eq "$?" "0" "install (run 2, same flags) exits 0"
has "$IX/run2.out" "already present" "run 2 reports the cron entry as already present"
eq "$(grep -cxF -- "$SANDBOX_LINE" "$FAKE_CRONTAB_DB" || true)" "1" "two runs leave exactly ONE cron entry"
eq "$(grep -c '^INVOKED -$' "$FAKE_CRONTAB_LOG" || true)" "1" "run 2 did not write the crontab at all"
has "$IX/run2.out" "sha256 matches" "run 2 re-verifies the installed sha256"

# The remote path: the same two runs through the fake ssh/scp.
RX="$SB/install-remote"
mkdir -p "$RX/prefix" "$RX/log"
REMOTE_LINE="40 3 * * * $RX/prefix/jomform-container-prune.sh >> $RX/log/prune.log 2>&1"
PATH="$(path_with_fake)" bash "$INSTALLER" --host root@jf-sandbox --prefix "$RX/prefix" --log "$RX/log/prune.log" >"$RX/run1.out" 2>&1
eq "$?" "0" "install over the fake ssh/scp exits 0"
has "$RX/run1.out" "sha256 matches" "the REMOTE path verifies the installed copy's sha256 (it is not skipped)"
eq "$(grep -cxF -- "$REMOTE_LINE" "$FAKE_CRONTAB_DB" || true)" "1" "the remote install added its own cron entry exactly once"
PATH="$(path_with_fake)" bash "$INSTALLER" --host root@jf-sandbox --prefix "$RX/prefix" --log "$RX/log/prune.log" >"$RX/run2.out" 2>&1
eq "$?" "0" "re-install over the fake ssh/scp exits 0"
eq "$(grep -cxF -- "$REMOTE_LINE" "$FAKE_CRONTAB_DB" || true)" "1" "re-installing remotely did NOT duplicate the entry"

# --dry-run, with the INSTALLED paths: it must print the exact production cron
# line and write nothing at all.
LOGGED_LINE='40 3 * * * /usr/local/bin/jomform-container-prune.sh >> /var/log/jomform-container-prune.log 2>&1'
inv_before="$(grep -c . "$FAKE_CRONTAB_LOG" || true)"
db_before="$(sha256sum "$FAKE_CRONTAB_DB" | awk '{print $1}')"
if [ -f /var/log/jomform-container-prune.log ]; then
  log_existed=yes
  log_mtime_before="$(stat -c '%Y' /var/log/jomform-container-prune.log)"
else
  log_existed=no
  log_mtime_before=""
fi
PATH="$(path_with_fake)" bash "$INSTALLER" --dry-run >"$IX/dry.out" 2>&1
eq "$?" "0" "--dry-run exits 0"
has "$IX/dry.out" "$LOGGED_LINE" "the dry run prints the EXACT production cron line"
eq "$(grep -c . "$FAKE_CRONTAB_LOG" || true)" "$inv_before" "the dry run invoked no crontab at all"
eq "$(sha256sum "$FAKE_CRONTAB_DB" | awk '{print $1}')" "$db_before" "the dry run changed nothing in the crontab"
if [ -f /var/log/jomform-container-prune.log ]; then
  eq "$(stat -c '%Y' /var/log/jomform-container-prune.log)" "$log_mtime_before" "the dry run did not touch the installed log file"
else
  eq "$(test -f /var/log/jomform-container-prune.log && echo yes || echo no)" "no" "the dry run did not create the installed log file"
fi

# ---------------------------------------------------------------------- teardown
say ""
say "== the sandbox tears itself down (asserted, not trusted) =="
SCRATCH="$SB"
SOCKET_MARK="$PRIV"
priv_stop
eq "$(test -e "$SCRATCH" && echo present || echo gone)" "gone" "the private daemon's scratch tree was removed"
eq "$(host ps -a --format '{{.Names}}' | grep -c -x -e "$PEER_PROBE" -e "$CONTROL_PROBE" || true)" "0" \
   "neither probe container is left behind on the HOST daemon (this run put back what it planted)"
eq "$(host images --format '{{.Repository}}:{{.Tag}}' | grep -c -x "jf-prune-probe:${SCRATCH##*/}" || true)" "0" \
   "the probe image is gone from the HOST daemon"
eq "$(pgrep -f "dockerd .*$SOCKET_MARK" >/dev/null 2>&1 && echo alive || echo gone)" "gone" "no private dockerd was left running"
eq "$(pgrep -f "sleep $SLEEP_SECS" >/dev/null 2>&1 && echo alive || echo gone)" "gone" "no decoy process was left running"
eq "$(mount | grep -c "$SCRATCH" || true)" "0" "no overlay/netns mount from this run is left behind"

# ---------------------------------------------------------------------- result
say ""
if [ "$FAILS" -eq 0 ]; then
  say "RESULT: $CHECKS checks passed, 0 failed"
  exit 0
fi
say "RESULT: $FAILS of $CHECKS checks FAILED"
exit 1
