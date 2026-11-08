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
#   jf-prune-decoy-leak      RUNNING            -> the teardown check's BITES arm:
#                                                 created AFTER everything else and
#                                                 left running on purpose, so the
#                                                 run-scoped liveness predicate must
#                                                 report it (and report it gone when
#                                                 its container is)
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
# run's own probe containers and one empty-rootfs probe image, which are removed
# again before the suite exits, plus the SWEEP of probes left behind by runs that
# are GONE — a probe whose owning pid is not a live process, which can never be a
# legitimate finding of a later run (see "the HOST daemon" below).
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
# So the comparison has three verdicts, and only the first fails:
#   OURS          this run's own audited removal path recorded the id -> the suite
#                 really did touch the host (see host_own_remove). FAILS.
#   FOREIGN       the HOST daemon recorded a destroy for that id and this run
#                 recorded no removal of it -> ANOTHER actor did it. NAMED with its
#                 name and the second it happened, and NOT a failure of this tree.
#   FOREIGN-NOEVENT  the loss is not this run's, but the daemon's event record no
#                 longer reaches back to it. PASSES with that stated reason and the
#                 window's own counts — see WHY THE WINDOW HAS TWO SOURCES below. It
#                 may reason that way because "this run removed nothing it did not
#                 create" is a property of THIS FILE (one raw call site, a read-only
#                 door, a removal path that refuses unowned ids), which the self-check
#                 below re-measures on every run; it is not a property of the window.
#
# WHY THE WINDOW HAS TWO SOURCES (card `t_d243986f`). Until 2026-11-04 the window was
# read ONCE, at the end, as a `docker events --since <start-2s> --until <now+2s>`
# REPLAY over the daemon's in-memory event buffer — and that buffer is BOUNDED AT 256
# EVENTS (measured on this host: `--since` 300s, 600s, 1800s and 7200s all answer
# exactly 256). A quiet host emits ~2.5 events/s from health checks alone, so a ~40s
# window sits near 100 — and a CONCURRENT LANE (any docker build, any e2e/a11y/media
# gate) pushes the window past 256, evicting the peer probe's destroy before the
# replay reads it. Measured in the merge train for PR #1535, with lane `t_48a93084`
# running: this suite's OWN peer probe came back unattributed —
#     FAIL ... existed before this run and is GONE, and its removal CANNOT be attributed
# plus `the probe container the PEER removed mid-run is attributed FOREIGN and NAMED
# (want '1', got '0')`, 2 of 112 checks, on a tree with nothing wrong, and the run's
# remaining inventory was lost with it. So:
#   1. a LIVE `docker events` subscriber is started BEFORE the before-snapshot, so
#      every event is on disk the moment it happens and cannot be evicted (asserted
#      below: the peer probe's destroy must be in the LIVE file, and the stream must
#      still have been open when it was written — a live window that recorded nothing
#      is not a window);
#   2. the replay above is kept as the fallback for a subscriber that died or a daemon
#      that restarted mid-run, and the two are MERGED into the log the verdicts read.
# A loss found in neither is recorded with both counts and the daemon id, and it is
# not allowed to red the gate on a tree that removed nothing.
#
# AND THE SUBSCRIBER'S LIFETIME IS BOUNDED FROM THE INSIDE (card `t_1dd033e7`). A bare
# `docker events --since <t>` stream has no natural end, and the teardown trap is
# unreachable on the signal that actually kills a gate run: SIGKILL, a killed slot, a
# torn-down card container. Measured 2026-11-05: the subscriber of such a run was still
# reading the host daemon 40 minutes after the run ended, reparented to
# `systemd --user`, and it held the MERGE lock (`/var/lock/jomform-merge.lock`) and a
# preflight slot through descriptors it had inherited from the run above it — `flock`
# is open-file-description scoped, so one surviving child keeps the lock held for a
# pid that is long dead. So every `docker events` invocation is `--until`-bounded, a
# supervisor stops it as soon as THIS RUN's pid is gone, and the supervisor closes the
# descriptors it inherited before it starts any child. The run asserts both halves: a
# stale subscriber left by a run that is GONE is reaped and named, the live child holds
# no lock descriptor (a fixture this run takes at fd 200 makes that measurable on every
# run), and nothing from this run's window survives it. The SIGKILL shape itself is
# driven by `deploy/host/jomform-container-prune_leak_bite.sh`.
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
D_LEAK="$DECOY-leak"                      # the liveness arm's decoy: left running ON PURPOSE
IMG=""                                    # the per-run image the decoys are made from
# The running decoys' lifetime. It is NOT their identity: `$$ % 120` has 120
# values, so this number also matches plenty of other processes on this host —
# exactly the flake this suite paid for (card t_d088f152). The teardown check keys
# on the CONTAINER (see decoy_procs), never on this number; the value is here only
# so a decoy is still asleep when the arms that need it running look at it.
SLEEP_SECS=$(( 120 + ($$ % 120) ))

SB=""        # scratch dir (private daemon data-root, sockets, logs)
PRIV=""      # the private daemon's socket path
PRIV_URL=""
DPID=""
KEEPFILE=""
NOMKT_BIN="" # PATH prefix holding an always-failing `mktemp` (the fail-closed arm)
PLANTED=""   # the symlink this run plants at the guard's historical FIXED rm path
DECOY_IDS=() # container ids of THIS run's decoys, captured before the daemon goes
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

# How old a dead-owner probe must be before the sweep will take it. A probe planted by
# ANOTHER run that is still going — the gate pool runs two of these at once by design — is
# seconds old when a second run sweeps, and taking it would red that run's own arm.
# Measured: with a creation-time bound of "before this run started", two runs started a
# second apart swept each other's baits, because the earlier run's bait predates the later
# run's start. A run lasts ~60-90s and plants its probes in its first ~10s, so anything
# younger than 2 minutes may still have a live owner behind it; anything older is debris
# from a run that is gone (the leak this sweep exists for sat on the daemon for hours).
SWEEP_MIN_AGE=120

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
HOST_OWN_NAMES=()       # the NAMES those creates reserved, so a trap that lands between
                        # `docker create` returning and the id being stored still cleans up
HOST_OWN_REMOVED=()     # ids this run's own audited path removed
HOST_OWN_IMAGE=""       # the tag this run imported
HOST_OWN_IMAGE_ID=""
HOST_EVENTS_LOG=""       # the MERGED window the verdicts read (live + replay)
HOST_EVENTS_LIVE=""      # the LIVE subscriber's file: written as each event happens
HOST_EVENTS_REPLAY=""    # the end-of-run `--since/--until` replay (the fallback)
HOST_EVENTS_PID=""       # the live window's supervisor, so a run that dies cannot leak it
HOST_EVENTS_SUP_PID=""   # (same process; named separately for the fd/lifetime checks)
HOST_EVENTS_PID_SEEN=""  # its pid, kept for the teardown assertion
HOST_EVENTS_FROM=""
# Each `docker events` invocation inside the LIVE window is `--until`-bounded to this
# many seconds (see host_events_spawn_subscriber): a stream with no `--until` has no
# natural end, which is how a killed run leaked one onto the host for 40 minutes.
HOST_EVENTS_WINDOW="${JF_PRUNE_EVENT_WINDOW_SECS:-5}"
HOST_EVENTS_MAX_LIFETIME="${JF_PRUNE_EVENT_LIVE_MAX_SECS:-3600}"
LIVE_LOCK_FIXTURE=""     # a lock THIS run takes itself, to prove no child inherits it
LIVE_LOCK_FD=""          # the descriptor that lock is held on (allocated, never hardcoded)

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
  # The tag is ours before the import runs, for the same trap-window reason as the
  # container names above: a signal between `docker import` returning and the tag being
  # stored would leave an image the next run has no reason to look for.
  HOST_OWN_IMAGE="$tag"
  id="$(host_docker import - "$tag" <"$tar")" || return 1
  HOST_OWN_IMAGE_ID="$id"
  printf '%s|import|%s|%s\n' "$(date +%s)" "$id" "$tag" >>"$HOST_OWN_LOG"
}

# host_own_create <name> <out-var> — a container this run OWNS on the host daemon
# (created, never started: no process, no network, no disk worth naming). Sets
# <out-var> to its id, again in THIS shell, for the same reason.
host_own_create() {
  local name="$1" outvar="$2" id
  # Reserve the NAME before the create, and clean up by name as well as by id (see
  # host_own_cleanup): a signal arriving between `docker create` returning and the id
  # being appended below is a window the sweep would only reach on the NEXT run, and a
  # SIGTERM is supposed to leave nothing behind at all. Measured 2026-11-04: a TERM in
  # exactly that window left a `jf-prune-controlprobe-<pid>` on the daemon.
  HOST_OWN_NAMES+=("$name")
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
  local id name
  for id in ${HOST_OWN_CONTAINERS[@]+"${HOST_OWN_CONTAINERS[@]}"}; do
    host_own_remove "$id" || true
  done
  HOST_OWN_CONTAINERS=()
  # The by-NAME half: a container whose create returned but whose id never made it into
  # HOST_OWN_CONTAINERS (a signal landed in that window) is still ours, and still gone.
  # `docker rm -f <name>` on a name we never got to create is a harmless error, and no
  # other run can hold one of these names — each embeds this run's pid.
  for name in ${HOST_OWN_NAMES[@]+"${HOST_OWN_NAMES[@]}"}; do
    host_docker rm -f "$name" >/dev/null 2>&1 || true
  done
  HOST_OWN_NAMES=()
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

# =============== probes left behind by runs that are GONE are SWEPT
# A probe planted on the HOST daemon carries the pid of the RUN that planted it in its
# name — this suite's `jf-prune-{peer,control}probe-<pid>`, and the sibling
# gate-concurrency harness's `jf-gate-concurrency-peer-<pid>-<n>-<n>`. That pid is what
# makes a leftover IDENTIFIABLE, and it is also what makes removing it safe: a probe
# whose owning pid is not a live process has no run left that will ever remove it, so it
# can never be a legitimate finding of a LATER run. It is garbage on a daemon every lane
# shares.
#
# WHY IT MATTERS. This suite takes its own probes down from EXIT/INT/TERM/HUP (see the
# traps below), but nothing survives a SIGKILL — the gate runner's final signal on a
# timeout — or a host that goes down under a run. The leftover then sits in the daemon
# as `Created`, is read as "existed before this run" by every run after it, and the
# moment ANOTHER actor removes it the later run reds with a loss it cannot attribute, on
# a tree that has nothing to do with it. Measured 2026-11-04 on the
# merge train: the host was carrying 12 such probes
# (`jf-gate-concurrency-peer-1738856-*-*` x8, `jf-gate-concurrency-peer-1464289-*-*` x4,
# `jf-prune-controlprobe-1913424`, `jf-prune-peerprobe-1913424`); a `deploy/*.sh`-only
# delta redded 5 of 115 checks, pristine `origin/main` redded the same 5 of 116, and
# `docker rm -f` of exactly those 12 made the SAME bytes 113/113 green. Nothing in the
# tree could have caused it, and the red names the innocent ref as `BISECT`.
#
# WHAT MAY BE REMOVED, AND NOTHING ELSE: a container whose name matches one of the two
# probe shapes above, whose embedded pid is NOT a live process, and whose state carries
# no process of its own (`created`/`exited`). A probe whose owner IS alive belongs to a
# CONCURRENT run — the gate pool runs two of these at once by design — and is never
# touched. Each removal goes through the one raw call site (`host_docker`) and is named
# as it happens, so the arm below can assert on every run exactly what was removed and
# that nothing else was.
PROBE_RE_OWN='^jf-prune-(peer|control)probe-([0-9]+)$'
PROBE_RE_CONC='^jf-gate-concurrency-peer-([0-9]+)-[0-9]+-[0-9]+$'
SWEPT=()
SWEPT_NAMES=()
STALE_A=""            # the sweep arm's bait, this suite's own probe shape, dead owner
STALE_B=""            # the sweep arm's bait, the gate-concurrency harness's shape
LIVEPROBE=""          # the sweep arm's control: a probe shape whose owner is ALIVE

# probe_name_pid <name> — the pid the PLANTING run embedded in a probe name; status 1
# when the name is not one of the two probe shapes.
probe_name_pid() {
  local name="${1:-}"
  if [[ "$name" =~ $PROBE_RE_OWN ]]; then printf '%s' "${BASH_REMATCH[2]}"; return 0; fi
  if [[ "$name" =~ $PROBE_RE_CONC ]]; then printf '%s' "${BASH_REMATCH[1]}"; return 0; fi
  return 1
}

# probe_owner_alive <pid> — 0 when a process with that pid exists.
probe_owner_alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

# dead_pid <slot> — a pid that CANNOT be allocated, and is measured as dead before it is
# used. It is `kernel.pid_max + 2*this run's pid + slot`: above pid_max, so the kernel
# can never hand it to a process (a reaped child's pid — the obvious way to get "a dead
# pid" — can be recycled by the very next process this script forks, which would make the
# sweep SPARE the bait on a busy host). The `2*` is what makes it unique per run: two runs
# whose pids differ by one would otherwise draw each other's slot numbers (measured: one
# run's slot 2 == the other's slot 1, and the two runs then fought over one container name).
dead_pid() {
  local slot="${1:-1}" pmax p
  pmax="$(cat /proc/sys/kernel/pid_max 2>/dev/null)" || return 1
  [ -n "$pmax" ] || return 1
  p=$(( pmax + 2 * $$ + slot ))
  probe_owner_alive "$p" && return 1
  printf '%s' "$p"
}

# host_plant_probe <name> — a container the SWEEP is expected to take. It is deliberately
# NOT in HOST_OWN_CONTAINERS: the audited removal path refuses an id this run did not
# create, and the point of the bait is that the sweep takes it BEFORE the before-snapshot,
# so it can never be read as a loss of this run. The teardown removes it BY NAME (see
# priv_stop) — a trap can land between `docker create` returning and the id being stored,
# and a bait left behind that way made a later run die on the name it could not re-create.
host_plant_probe() {
  local name="$1"
  host_docker create --name "$name" "$HOST_OWN_IMAGE_ID" true >/dev/null || return 1
}

# host_container_created <id> — the epoch the daemon created that container, so the sweep
# can tell a leftover (created before this run) from a concurrent run's fresh probe.
host_container_created() {
  local iso
  iso="$(host inspect --format '{{.Created}}' "$1" 2>/dev/null)" || return 1
  [ -n "$iso" ] || return 1
  date -u -d "$iso" +%s 2>/dev/null
}

# host_sweep_stale [<name this run planted>…] — remove the probes on the HOST daemon whose
# owning run is gone. Sets SWEPT/SWEPT_NAMES, writes them to $SB/swept-names.txt, and
# prints one line per container it swept AND one per candidate it deliberately SPARED, so a
# run that swept nothing says so out loud instead of looking like a run that found nothing.
# A name passed in is this run's own bait: it is younger than SWEEP_MIN_AGE by construction,
# so the age bound below would spare it, and it is named here only for the arm's sake.
host_sweep_stale() {
  local id name state pid born age force n
  SWEPT=(); SWEPT_NAMES=()
  host ps -a --no-trunc --format '{{.ID}}|{{.Names}}|{{.State}}' 2>/dev/null \
    >"$SB/host-probes-inventory.txt" || true
  while IFS='|' read -r id name state; do
    [ -n "$id" ] || continue
    pid="$(probe_name_pid "$name")" || continue
    if probe_owner_alive "$pid"; then
      printf '  spared: %s (%s) — its owning pid %s is ALIVE, so another run owns it\n' \
        "$name" "${id:0:12}" "$pid"
      continue
    fi
    force=0
    for n in "$@"; do [ "$n" = "$name" ] && force=1; done
    if [ "$force" = 0 ]; then
      born="$(host_container_created "$id")" || born=""
      if [ -z "$born" ]; then
        printf '  spared: %s (%s) — its owning pid %s is gone, but its creation time could not be read, so it is not taken\n' \
          "$name" "${id:0:12}" "$pid"
        continue
      fi
      age=$(( $(date +%s) - born ))
      if [ "$age" -lt "$SWEEP_MIN_AGE" ]; then
        printf '  spared: %s (%s) — its owning pid %s is gone, but it is only %ss old, so a run that is still going may have planted it\n' \
          "$name" "${id:0:12}" "$pid" "$age"
        continue
      fi
    fi
    case "$state" in
      created|exited) ;;
      *) printf '  spared: %s (%s) — its owning pid %s is gone, but it is %s, so it still has a process\n' \
               "$name" "${id:0:12}" "$pid" "$state" ; continue ;;
    esac
    if host_docker rm -f "$id" >/dev/null 2>&1; then
      SWEPT+=("$id"); SWEPT_NAMES+=("$name")
      printf '  swept: %s (%s) — its owning pid %s is not alive\n' "$name" "${id:0:12}" "$pid"
    else
      printf '  WARN: could not remove the stale probe %s (%s)\n' "$name" "${id:0:12}" >&2
    fi
  done <"$SB/host-probes-inventory.txt"
  : >"$SB/swept-names.txt"
  if [ "${#SWEPT_NAMES[@]}" -gt 0 ]; then
    printf '%s\n' "${SWEPT_NAMES[@]}" >"$SB/swept-names.txt"
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
# reader whether the LOSS or the MEASUREMENT is the problem. Both sources are named:
# a window whose live half is empty and whose replay is at the daemon's 256-event
# bound is a window that evicted what it was asked about, and that is a different
# thing from a daemon that recorded nothing.
window_state() {
  local live replay merged
  live="$(grep -cE '^[0-9]+\|' "$HOST_EVENTS_LIVE" 2>/dev/null || true)"
  replay="$(grep -cE '^[0-9]+\|' "$HOST_EVENTS_REPLAY" 2>/dev/null || true)"
  merged="$(grep -cE '^[0-9]+\|' "$HOST_EVENTS_LOG" 2>/dev/null || true)"
  printf 'event window: %s line(s) merged (live %s + replay %s), host daemon id %s -> %s' \
    "${merged:-0}" "${live:-0}" "${replay:-0}" "${HOST_ID_BEFORE:-?}" "${HOST_ID_AFTER:-?}"
}

# host_events_close_inherited_fds — close every descriptor above 2 (except bash's own
# script descriptor) that this process inherited from the caller.
#
# WHY (card `t_1dd033e7`). `flock` is OPEN-FILE-DESCRIPTION scoped: the lock stays held
# while ANY process keeps a descriptor on that open file description. The merge lane
# takes `/var/lock/jomform-merge.lock` as `flock -w 3600 <lock> bash train.sh`, and the
# gate's slot pool opens `/var/lock/jomform-preflight-gate.lock.slot<N>` — both are
# inherited, as ordinary descriptors, by every process this suite starts. A long-lived
# child that keeps one holds a lock this suite NEVER took. Measured 2026-11-05: an
# orphaned `docker … events` subscriber whose run had ended 40 minutes earlier held the
# MERGE lock the whole time, and the merger's `flock -w 3600` would have timed out.
#
# 255 is skipped deliberately: bash keeps this script on it, and closing it under a
# running subshell can end the parse of the rest of this file.
host_events_close_inherited_fds() {
  local d fd
  for d in /proc/self/fd/*; do
    fd="${d##*/}"
    case "$fd" in ''|*[!0-9]*) continue ;; esac
    [ "$fd" -le 2 ] && continue
    [ "$fd" -eq 255 ] && continue
    eval "exec ${fd}>&-" 2>/dev/null || true
  done
}

# host_events_reap_stale — kill a `docker … events` subscriber left behind by a run
# whose scratch tree is already GONE, and NAME its pid.
#
# THIS VERSION cannot leak one (the window is bounded from the inside, see
# host_events_spawn_subscriber), but a subscriber left by an OLDER run — the measured
# 2026-11-05 leak of card `t_1dd033e7` — still can, and it still holds whatever lock
# descriptors it inherited. It is reaped before this run opens its own window.
#
# The predicate is that measured shape and nothing broader: the process is a
# `docker … events` stream AND its stdout/stderr still name a `jf-prune-bite.*` path
# that is `(deleted)` — i.e. the run that created it has finished and its scratch tree
# is gone. A CONCURRENT run's subscriber writes to a scratch dir that still exists, so
# the two-slot gate pool cannot sweep a peer's window out from under it.
host_events_reap_stale() {
  local d pid cmd fd target reaped=""
  for d in /proc/[0-9]*; do
    pid="${d#/proc/}"
    [ "$pid" = "$$" ] && continue
    cmd="$(tr '\0' ' ' <"$d/cmdline" 2>/dev/null || true)"
    case "$cmd" in
      *docker*' events'*) ;;
      *) continue ;;
    esac
    for fd in 1 2; do
      target="$(readlink "$d/fd/$fd" 2>/dev/null || true)"
      case "$target" in
        *'/jf-prune-bite.'*' (deleted)') ;;
        *) continue ;;
      esac
      if kill "$pid" 2>/dev/null; then reaped="$reaped $pid"; fi
      break
    done
  done
  if [ -n "$reaped" ]; then
    say "window: REAPED stale docker-events subscriber(s) from a run that is GONE:$reaped (they hold any lock descriptors they inherited)"
  fi
}

# host_events_reap_selftest — plant the shape the reaper exists for and require it
# killed, so the arm is exercised on every run instead of only on the day a leak
# happens to be present: a `docker … events` process whose stdout names a
# `jf-prune-bite.*` path that is already `(deleted)`.
host_events_reap_selftest() {
  local log="$SB/reap-fixture.log" pid i=0
  : >"$log"
  bash -c 'exec -a "docker --host fixture events" sleep 30' >"$log" 2>&1 &
  pid=$!
  rm -f "$log"
  while [ "$i" -lt 40 ]; do
    case "$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null || true)" in
      *'docker'*' events'*) break ;;
    esac
    sleep 0.05
    i=$((i + 1))
  done
  host_events_reap_stale
  if kill -0 "$pid" 2>/dev/null; then
    bad "the stale-subscriber reaper did NOT kill a docker-events process left by a run that is GONE (pid $pid)"
    kill -9 "$pid" 2>/dev/null || true
  else
    ok "the stale-subscriber reaper kills a docker-events process left behind by a run that is GONE, and names its pid"
  fi
  wait "$pid" 2>/dev/null || true
}

# host_events_lock_fixture — take a lock on a file in this run's scratch tree, on a
# descriptor of its own, and leave it open for the duration of the run.
#
# This is the FIXTURE that makes the "no inherited lock descriptor" assertion below
# bite on EVERY run: without it a suite started by hand (no gate above it) has no
# inherited lock fd and the assertion would pass vacuously. The file is this run's own,
# so holding it touches nothing the host cares about.
#
# The descriptor is allocated dynamically, NOT hardcoded to 200: the incident's fd WAS
# 200 (the merge lane's wrapper), and a fixture that re-used that number would close
# the very descriptor the harness is trying to pass down. The assertion compares the
# child's descriptor TARGETS, so the number does not matter.
host_events_lock_fixture() {
  LIVE_LOCK_FIXTURE="$SB/inherited-lock-fixture.lock"
  : >"$LIVE_LOCK_FIXTURE"
  exec {LIVE_LOCK_FD}>>"$LIVE_LOCK_FIXTURE" || return 1
  flock -n "$LIVE_LOCK_FD" || return 1
  return 0
}

host_events_lock_fixture_release() {
  [ -n "${LIVE_LOCK_FIXTURE:-}" ] || return 0
  [ -n "${LIVE_LOCK_FD:-}" ] && eval "exec ${LIVE_LOCK_FD}>&-" 2>/dev/null || true
  rm -f "$LIVE_LOCK_FIXTURE"
  LIVE_LOCK_FIXTURE=""
  LIVE_LOCK_FD=""
}

# host_events_fd_to <pid> <path> — the number of a descriptor of <pid> that points at
# <path>, empty when it holds none. Used to prove a child did NOT inherit this run's
# lock, which is the only way to check it while the run still holds the lock itself.
host_events_fd_to() {
  local pid="$1" want="$2" d
  [ -d "/proc/$pid/fd" ] || return 0
  for d in /proc/"$pid"/fd/*; do
    if [ "$(readlink "$d" 2>/dev/null || true)" = "$want" ]; then printf '%s\n' "${d##*/}"; return 0; fi
  done
  return 0
}

# host_events_alive_pids — every `docker … events` process whose output descriptors
# still point into THIS run's scratch tree. Those are the ones this run started (a
# peer's subscriber writes to its own scratch dir), and after the window closes there
# must be none.
host_events_alive_pids() {
  local d pid cmd fd target
  for d in /proc/[0-9]*; do
    pid="${d#/proc/}"
    [ "$pid" = "$$" ] && continue
    cmd="$(tr '\0' ' ' <"$d/cmdline" 2>/dev/null || true)"
    case "$cmd" in *docker*' events'*) ;; *) continue ;; esac
    for fd in 1 2; do
      target="$(readlink "$d/fd/$fd" 2>/dev/null || true)"
      case "$target" in
        *"$SB"*) printf '%s\n' "$pid"; break ;;
      esac
    done
  done
}

# host_events_spawn_subscriber — the LIVE half of the window with a lifetime that
# cannot outlive this run (card `t_1dd033e7`).
#
# A bare `docker events --since <t>` stream has NO natural end, and `host_events_live_stop`
# is unreachable on the signals that actually kill a gate run: SIGKILL (the runner's last
# signal on a timeout), a killed slot, a torn-down card container. Measured 2026-11-05:
# the subscriber of such a run was still reading the host daemon 40 minutes later,
# reparented to `systemd --user`, holding the MERGE lock and a preflight slot through
# the descriptors it had inherited from the run above it.
#
# So the lifetime is bounded from the INSIDE, three ways, and any one of them alone
# ends it:
#   1. every `docker events` invocation carries `--until` (HOST_EVENTS_WINDOW, 5s) and
#      the supervisor opens the next window — a leaked child therefore dies by itself
#      within one window even if nothing else works;
#   2. the supervisor stops as soon as THIS RUN's pid is gone (a SIGKILLed run is
#      reparented, not dead, so the check still sees it);
#   3. a hard lifetime cap (HOST_EVENTS_MAX_LIFETIME) so a wedged supervisor cannot
#      live forever either.
# And it closes the descriptors it inherited BEFORE it starts any child, so a child of
# this suite can never hold the merge lock the merge lane is waiting on.
#
# Each window is written to a scratch file and appended after the child exits, so the
# LIVE log a verdict reads never holds a half-written line; the append is deduplicated
# because consecutive windows share their boundary (`--since` is the previous `--until`).
host_events_spawn_subscriber() {
  local run_pid="$$"            # $$ is this RUN's pid, even inside the subshell
  local i=0
  rm -f "$SB/host-events-live.stop" "$SB/host-events-live.ready"
  (
    host_events_close_inherited_fds
    # READY only after the descriptors are closed, so the parent's check measures a
    # settled child instead of racing its startup.
    : >"$SB/host-events-live.ready"
    local from="$HOST_EVENTS_FROM" until_ts window="$SB/host-events-live.window"
    local waited=0
    while :; do
      [ -e "$SB/host-events-live.stop" ] && break
      kill -0 "$run_pid" 2>/dev/null || break
      [ "$waited" -lt "$HOST_EVENTS_MAX_LIFETIME" ] || break
      until_ts="$(date -u -d '@'$(( $(date +%s) + HOST_EVENTS_WINDOW )) +%Y-%m-%dT%H:%M:%S.%NZ)"
      : >"$window"
      host events --since "$from" --until "$until_ts" \
        --filter type=container --filter type=image \
        --filter event=destroy --filter event=delete --filter event=untag \
        --format '{{.Time}}|{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' \
        >"$window" 2>>"$SB/host-events-live.err"
      if [ -s "$window" ]; then
        comm -13 <(sort -u "$HOST_EVENTS_LIVE") <(sort -u "$window") >>"$HOST_EVENTS_LIVE"
      fi
      from="$until_ts"
      waited=$((waited + HOST_EVENTS_WINDOW))
    done
    rm -f "$window" "$SB/host-events-live.ready"
  ) &
  HOST_EVENTS_SUP_PID=$!
  HOST_EVENTS_PID=$HOST_EVENTS_SUP_PID
  HOST_EVENTS_PID_SEEN=$HOST_EVENTS_SUP_PID
  while [ ! -e "$SB/host-events-live.ready" ] && [ "$i" -lt 100 ]; do
    sleep 0.05
    i=$((i + 1))
  done
}

# host_events_check_no_inherited_lock — the in-run measurement of "a probe must not
# hold a lock it never took": the long-lived child (the supervisor) must hold NO
# descriptor of this run's lock fixture. The run itself keeps fd 200, so the check is
# on the CHILD's descriptor table, not on whether the lock can be taken.
host_events_check_no_inherited_lock() {
  local fd
  fd="$(host_events_fd_to "$HOST_EVENTS_SUP_PID" "$LIVE_LOCK_FIXTURE")"
  if [ -z "$fd" ]; then
    ok "the LIVE subscriber holds NO descriptor of this run's lock (the fd $LIVE_LOCK_FD this run took -> $(basename "$LIVE_LOCK_FIXTURE") was closed before the child started)"
    return 0
  fi
  bad "the LIVE subscriber INHERITED this run's lock (fd $fd -> $(basename "$LIVE_LOCK_FIXTURE")): a child this suite starts must not hold a lock it never took — this is the 2026-11-05 merge-lock leak"
  return 1
}

# host_events_live_start — open the LIVE half of the window (card `t_d243986f`).
#
# WHY A SUBSCRIBER AND NOT ONLY THE REPLAY. The daemon keeps its event record in a
# bounded in-memory ring — BOUNDED AT 256 EVENTS, measured on this host 2026-11-04:
# `docker events --since` for 300s, 600s, 1800s and 7200s every one answered exactly
# 256 lines. `--since <start>` therefore does NOT mean "everything since then"; it
# means "whatever of the last 256 is still in the ring". A concurrent lane's docker
# work fills that ring inside this suite's own window, and the peer probe's destroy
# was evicted before the end-of-run replay asked for it (PR #1535's merge run). A
# subscriber receives events as they are published and writes them to disk as it goes,
# so nothing can be evicted from under it.
#
# It is started BEFORE the before-snapshot, so an event that happens between the
# snapshot and the snapshot's own `docker ps` is still inside the window.
host_events_live_start() {
  [ -n "$HOST_EVENTS_LIVE" ] || return 1
  host_events_reap_selftest
  host_events_reap_stale
  host_events_lock_fixture || return 1
  : >"$HOST_EVENTS_LIVE"
  : >"$SB/host-events-live.err"
  host_events_spawn_subscriber
  # A failure here is already recorded as a FAIL; the window itself opened, so the run
  # continues and reports it in the summary rather than aborting the whole inventory.
  host_events_check_no_inherited_lock || true
  return 0
}

# host_events_live_stop — close it, and REQUIRE it to be gone: a stream this run
# stopped tracking would be a process leaked onto the host for the rest of the day.
#
# The supervisor notices a stop only at a window boundary, so this waits at most ONE
# window (plus slack) rather than signalling into a running `docker events` and leaving
# it to write into the LIVE log while the log is being read.
host_events_live_stop() {
  local sup="${HOST_EVENTS_SUP_PID:-}" i=0
  [ -n "$sup" ] || return 0
  : >"$SB/host-events-live.stop"
  while kill -0 "$sup" 2>/dev/null && [ "$i" -lt $(( (HOST_EVENTS_WINDOW + 10) * 4 )) ]; do
    sleep 0.25
    i=$((i + 1))
  done
  kill -9 "$sup" 2>/dev/null || true
  wait "$sup" 2>/dev/null || true
  rm -f "$SB/host-events-live.stop" "$SB/host-events-live.window"
  HOST_EVENTS_SUP_PID=""
  HOST_EVENTS_PID=""
}

# host_events_merge <live> <replay> — the log the verdicts read: the union of both
# sources, deduplicated (the two overlap by design) and stripped of anything that is
# not an event row, so a daemon error cannot be mistaken for one.
host_events_merge() {
  { grep -E '^[0-9]+\|' "$1" 2>/dev/null; grep -E '^[0-9]+\|' "$2" 2>/dev/null; } | sort -u
}

# record_container_loss <id> <name> — the ONE place a lost host container becomes a
# verdict. Extracted (card `t_d243986f`) so every shape can be exercised from a
# fixture on EVERY run, including the one that cannot be produced on demand: a loss
# whose destroy has been evicted from the daemon's bounded ring. HOST_ATTRIBUTION_OUT
# is where the account row lands, so a fixture run of this function cannot pollute the
# real account.
HOST_ATTRIBUTION_OUT=""
record_container_loss() {
  local cid="$1" cname="$2" cev="${3:-}" when="unknown"
  [ -n "$cev" ] || cev="$(host_event_time "$cid")"
  [ -n "$cev" ] && when="$(date -u -d "@$cev" +%H:%M:%SZ 2>/dev/null || printf '%s' "$cev")"
  case "$(attribution_of_container "$cid")" in
    OURS)
      printf 'OURS|%s|%s\n' "$cid" "$cname" >>"$HOST_ATTRIBUTION_OUT"
      bad "host container '$cname' (${cid:0:12}) existed before this run and was removed by THIS RUN's own audited removal path at $when — the suite touched the host" ;;
    FOREIGN)
      printf 'FOREIGN|%s|%s|%s\n' "$cid" "$cname" "$when" >>"$HOST_ATTRIBUTION_OUT"
      ok "host container '$cname' (${cid:0:12}) was removed at $when by ANOTHER actor on the shared daemon — FOREIGN, not this tree's failure (this run recorded no removal of it)" ;;
    *)
      printf 'FOREIGN-NOEVENT|%s|%s|\n' "$cid" "$cname" >>"$HOST_ATTRIBUTION_OUT"
      ok "host container '$cname' (${cid:0:12}) existed before this run and is GONE; its removal cannot be attributed to this tree — this run recorded no removal of it, and the ONLY way it can remove a host container refuses an id it did not create (self-checked above), while the daemon no longer holds a destroy for it ($(window_state)). NOT this tree's loss: it is recorded and PASSES with that reason rather than redding the gate" ;;
  esac
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

# ---------------- a decoy process, identified by RUN, never by a host-wide argv
# WHAT THIS REPLACES, AND WHY (card t_d088f152). The teardown assertion used to read
#
#     pgrep -f "sleep $SLEEP_SECS"
#
# — HOST-WIDE. Every lane's tool call, cron poll loop and certbot runs under this
# one pid namespace, so ANY process whose command line carries `sleep <n>` with n
# in the 120..239 window the suite draws its decoy lifetime from was read as a
# leaked decoy. Measured 2026-10-31 gating the merge tree for PR #1502:
#   FAIL no decoy process was left running (want 'gone', got 'alive')   (1 of 104)
# and a re-run of the IDENTICAL bytes (same tree 699dea68, same env key) passed
# 104/104. Nothing about the tree changed between them. A check that reads the
# whole host cannot tell "my decoy leaked" from "somebody else is sleeping", so on
# a shared host it is a coin flip dressed as a measurement — and a red one stops
# the runner, so it costs the gates after it (refguard, selftest) as well.
#
# SO THE PREDICATE IS KEYED ON THE CONTAINER THIS RUN CREATED. Docker puts every
# process of a container in that container's own cgroup; measured on this host,
#   /proc/<pid>/cgroup -> 0::/system.slice/docker-<full container id>.scope
# and the id belongs to this run alone (its daemon is private). Measured
# 2026-11-01 that the marker survives the leak this suite is about: `kill -9` of
# the private dockerd leaves the decoy's `sleep` RUNNING and still in that cgroup,
# so the predicate does see it. Reading the cgroup rather than the argv also makes
# it indifferent to the command's shape (`/busybox sleep N`, or `sh -c 'sleep N'`
# on the JF_PRUNE_BITE_IMAGE path) and to any sh→child nesting.
#
# decoy_procs <container id…> — one line per process still alive inside a
# container this run created: "<pid> <cmdline>". An unrelated `sleep N` anywhere
# on the host cannot appear here. If a host's cgroup layout ever stopped naming
# the container, this returns nothing and the BITES arm below reds — the safe
# direction (a liveness check that cannot see its own decoy must never be green).
#
# ONE PASS OVER /proc, AND NO FORK PER ENTRY. The first draft forked a `grep` for
# every (/proc entry × container id) — 2400 forks for a 4-id run — and the teardown
# crawl that produced (5s per call, 50s in the wait loop) is itself a way to hang
# this gate. Reading /proc/<pid>/cgroup with a shell `read` redirect costs no
# process, and only a MATCH forks (for its cmdline).
decoy_procs() {
  local d pid id line cg
  [ "$#" -gt 0 ] || return 0
  for d in /proc/[0-9]*; do
    pid="${d#/proc/}"
    [ "$pid" = "$$" ] && continue          # this scanner is never a decoy
    cg=""
    while IFS= read -r line; do cg="$cg$line;"; done 2>/dev/null <"$d/cgroup"
    [ -n "$cg" ] || continue
    for id in "$@"; do
      [ -n "$id" ] || continue
      case "$cg" in
        *"$id"*)
          printf '%s %s\n' "$pid" "$(tr '\0' ' ' <"$d/cmdline" 2>/dev/null)"
          break
          ;;
      esac
    done
  done
  return 0
}

# decoy_wait_gone <tries> <container id…> — wait in 0.5s steps (bounded) until no
# process of those containers is alive; 0 when none is. This is the RACE fix too:
# a decoy's `sleep` can outlive the private dockerd's SIGKILL by a few ms, so the
# teardown waits for the process it itself recorded instead of sampling once and
# calling a healthy teardown a leak.
decoy_wait_gone() {
  local tries="$1" i=0
  shift
  while [ "$i" -lt "$tries" ]; do
    [ -z "$(decoy_procs "$@")" ] && return 0
    sleep 0.5
    i=$((i + 1))
  done
  [ -z "$(decoy_procs "$@")" ]
}

priv_stop() {
  # The LIVE event window first (card `t_d243986f`): it is a `docker events` stream
  # this run started, so a run that dies anywhere below must not leave it reading the
  # host daemon for the rest of the day.
  host_events_live_stop
  # The lock fixture this run took (card `t_1dd033e7`): closing its descriptor is the
  # release, and the file goes with the scratch tree.
  host_events_lock_fixture_release
  # THE HOST PROBES FIRST, and unconditionally: they are the only objects this
  # suite ever wrote to the shared daemon, so they go even when the run died before
  # the attribution ran. Target-checked (host_own_remove refuses an id this run did
  # not create), and idempotent.
  host_own_cleanup
  # Remove the decoys BEFORE the daemon goes: a container left behind belongs to
  # containerd, not to the daemon object, so tearing the daemon down first can
  # leave its process running for the rest of its `sleep`.
  if [ -n "$PRIV_URL" ]; then
    # Remove this run's decoys BEFORE the daemon goes — a container left behind belongs to
    # the SHARED containerd, not to the daemon object, so tearing the daemon down first can
    # leave its process running for the rest of its `sleep`. REMOVAL IS RETRIED AND
    # VERIFIED: with two runs in flight the daemon can be slow enough that a single
    # `priv info`/`rm` round trip does not empty the list, and a container missed here is
    # one whose process survives the daemon (measured: `sleep 142` still in
    # `docker-<id>.scope` 25s after the teardown).
    local t=0 ids left
    while [ "$t" -lt 6 ]; do
      if priv info >/dev/null 2>&1; then
        ids="$(priv ps -a -q 2>/dev/null || true)"
        if [ -n "$ids" ]; then priv rm -f $ids >/dev/null 2>&1 || true; fi
        left="$(priv ps -a -q 2>/dev/null | grep -c . || true)"
        [ "${left:-0}" = 0 ] && break
      fi
      sleep 1
      t=$((t + 1))
    done
    # Let the removed containers' processes exit before the daemon is killed, so
    # the shim usually releases its mount itself. Keyed on THIS RUN's containers
    # (decoy_procs): the host-wide `pgrep -f "sleep N"` that used to be here
    # waited on any unrelated sleep in the 120..239 window on this shared host,
    # and the teardown assertion below then FAILED on it (card t_d088f152).
    decoy_wait_gone 10 "${DECOY_IDS[@]}"
  fi
  if [ -n "$DPID" ] && kill -0 "$DPID" 2>/dev/null; then
    kill "$DPID" 2>/dev/null || true
    local i=0
    while kill -0 "$DPID" 2>/dev/null && [ "$i" -lt 20 ]; do sleep 0.5; i=$((i + 1)); done
    kill -9 "$DPID" 2>/dev/null || true
    wait "$DPID" 2>/dev/null || true
  fi
  DPID=""
  # LAST RESORT, and only for THIS RUN's decoys: if a container could not be removed before
  # the daemon went away, its process is kept alive by the SHARED containerd's shim and
  # would sit in `docker-<id>.scope` for the rest of its `sleep`. Signal it directly —
  # `decoy_procs` matches the container ids this run created, so no other process on the
  # host can be reached by this loop. Normally it is a no-op: the retried `priv rm -f`
  # above has already emptied the list.
  local p
  for p in $(decoy_procs "${DECOY_IDS[@]}" 2>/dev/null | awk '{print $1}'); do
    kill -9 "$p" 2>/dev/null || true
  done
  decoy_wait_gone 10 "${DECOY_IDS[@]}"
  # Only ever removes the symlink THIS run planted at the guard's historical fixed
  # rm path (a fixed path by definition, so it cannot be owned exclusively).
  if [ -n "$PLANTED" ] && [ -L "$PLANTED" ]; then rm -f "$PLANTED"; fi
  # The sweep arm's baits are named from THIS RUN's own pid, so the name belongs to this
  # run and to nothing else. Removed BY NAME rather than by id: a trap can land between
  # `docker create` returning and the id being stored — measured, a SIGTERM in that window
  # left one bait behind, and the next run then died on the name it could not re-create.
  if [ -n "$STALE_A" ]; then host_docker rm -f "$STALE_A" >/dev/null 2>&1 || true; fi
  if [ -n "$STALE_B" ]; then host_docker rm -f "$STALE_B" >/dev/null 2>&1 || true; fi
  # The "live owner" the arm's control probe is named after is THIS RUN's own pid, so
  # there is nothing extra to tear down: host_own_cleanup above already removes it, and
  # if this run is SIGKILLed its pid goes with it, leaving the control probe sweepable by
  # the next run rather than a probe that looks owned by a live run for an hour.
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
  HOST_EVENTS_LIVE="$SB/host-events-live.log"
  HOST_EVENTS_REPLAY="$SB/host-events-replay.log"
  : >"$HOST_CALLS_LOG"
  : >"$HOST_OWN_LOG"
  : >"$HOST_EVENTS_LOG"
  : >"$HOST_EVENTS_LIVE"
  : >"$HOST_EVENTS_REPLAY"
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
# 1. TWO sources, opened here and read at the end (card `t_d243986f`). The LIVE
#    subscriber is started BEFORE the snapshot, so nothing that happens inside the
#    window can land before it, and every event is on disk as it is published — the
#    daemon's `--since` replay is a bounded ring (256 events) and a concurrent lane
#    evicts from it. The window's start still goes to the replay as its `--since`, so
#    the fallback covers the same span. (Both are merged into one log at the end; see
#    host_events_merge.)
HOST_EVENTS_FROM="$(date -u -d '@'$(( $(date +%s) - 2 )) +%Y-%m-%dT%H:%M:%SZ)"
host_events_live_start \
  || fatal "could not open the LIVE event window on the host daemon — the attribution would rest on the bounded replay alone"
say "window: LIVE subscriber open since $HOST_EVENTS_FROM (pid $HOST_EVENTS_PID); the replay at the end is the fallback"
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
# 2b. THE SWEEP, AND ITS OWN ARM. Two containers shaped like probes
#     from runs that are GONE (a VERIFIED-dead pid in the name) must be swept; one shaped
#     like a probe of a LIVE run must be SPARED — otherwise the sweep would be "remove
#     anything called jf-*", which on a daemon shared by a two-slot gate pool would
#     delete a CONCURRENT run's probes out from under it. It runs BEFORE the
#     before-snapshot, so what it plants can never be read as a loss later, and it is
#     measured rather than believed: the arm checks the premise (the two dead pids are
#     dead, the live one is alive), that both baits went, that the live one stayed, and
#     that nothing outside the two probe shapes was removed.
DEAD_A="$(dead_pid 1)" || fatal "could not produce a pid that cannot be allocated for the sweep arm"
DEAD_B="$(dead_pid 2)" || fatal "could not produce a second unallocatable pid for the sweep arm"
[ "$DEAD_A" != "$DEAD_B" ] || fatal "the sweep arm drew the same dead pid twice"
STALE_A="jf-prune-peerprobe-$DEAD_A"
STALE_B="jf-gate-concurrency-peer-$DEAD_B-1-1"
# The control: a probe of the SAME two shapes whose owning pid is ALIVE. Its owner is
# this run's own pid, so it costs no extra process — and when this run is SIGKILLed its
# pid dies with it, which is what makes the control probe sweepable by the next run
# instead of looking owned for another hour.
LIVEPROBE="jf-gate-concurrency-peer-$$-2-2"
host_plant_probe "$STALE_A" \
  || fatal "could not plant '$STALE_A' on the HOST daemon"
host_plant_probe "$STALE_B" \
  || fatal "could not plant '$STALE_B' on the HOST daemon"
host_own_create "$LIVEPROBE" LIVE_ID \
  || fatal "could not plant '$LIVEPROBE' on the HOST daemon"
eq "$(probe_owner_alive "$DEAD_A" && echo alive || echo dead)" "dead" \
  "sweep arm: the first bait's owning pid $DEAD_A really is not a live process (the premise of the sweep, measured)"
eq "$(probe_owner_alive "$DEAD_B" && echo alive || echo dead)" "dead" \
  "sweep arm: the second bait's owning pid $DEAD_B really is not a live process"
eq "$(probe_owner_alive "$$" && echo alive || echo dead)" "alive" \
  "sweep arm: the control probe's owning pid $$ really IS a live process (this run)"
host_sweep_stale "$STALE_A" "$STALE_B" >"$SB/sweep.out" 2>&1
say ""
say "== the sweep: probes left behind by runs that are GONE =="
sed 's/^/  /' "$SB/sweep.out"
eq "$(grep -c -x -- "$STALE_A" "$SB/swept-names.txt" || true)" "1" \
  "the sweep removed '$STALE_A' — this suite's own probe shape, owner's pid $DEAD_A gone"
eq "$(grep -c -x -- "$STALE_B" "$SB/swept-names.txt" || true)" "1" \
  "the sweep removed '$STALE_B' — the sibling gate-concurrency harness's shape, owner's pid $DEAD_B gone"
eq "$(grep -c -x -- "$LIVEPROBE" "$SB/swept-names.txt" || true)" "0" \
  "the sweep SPARED '$LIVEPROBE' — a probe shape whose owning pid $$ is ALIVE, i.e. a concurrent run's"
eq "$(host ps -a --format '{{.Names}}' | grep -c -x -- "$LIVEPROBE" || true)" "1" \
  "…and that spared probe is still on the host daemon (the sweep did not merely fail to log it)"
eq "$(host ps -a --format '{{.Names}}' | grep -c -x -e "$STALE_A" -e "$STALE_B" || true)" "0" \
  "both baits are really gone from the daemon, not just recorded as swept"
eq "$(grep -vcE "$PROBE_RE_OWN|$PROBE_RE_CONC" "$SB/swept-names.txt" || true)" "0" \
  "the sweep removed NOTHING whose name is not one of the two probe shapes (on a shared daemon that is the whole safety argument)"
# The teardown's OWN installation, checked rather than trusted: the sweep above is what
# makes a SIGKILLed run harmless, and these are what make a run that is ASKED to stop
# tidy up its own probes instead of leaving them for the sweep.
trap -p EXIT INT TERM HUP >"$SB/teardown-traps.txt"
# A process started as an ASYNCHRONOUS command inherits SIGINT IGNORED, and bash then
# refuses to trap it: the file shows `trap -- '' SIGINT` instead of a handler. That is not
# a leak — a process that ignores SIGINT is never interrupted by one — so the expectation
# is 4, minus one for exactly that shape. Measured: the same traps written by a foreground
# run print 4 handler lines, by `cmd &` 3.
INT_UNTRAPPABLE=0
grep -qx "trap -- '' SIGINT" "$SB/teardown-traps.txt" && INT_UNTRAPPABLE=1
eq "$(grep -c 'priv_stop' "$SB/teardown-traps.txt" || true)" "$((4 - INT_UNTRAPPABLE))" \
  "the teardown is installed on EXIT, INT, TERM and HUP (a run asked to stop removes its own probes rather than leaking them; INT is counted out only where SIGNAL IGNORE inherited by an asynchronous command made it untrappable)"
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
# The event window, over the whole run: close the LIVE subscriber (its file is then
# complete), then take the `--since/--until` replay as the fallback, then merge the
# two into the log every verdict below reads. `--until` two seconds ahead so a
# destroy that lands while the replay is being composed is still inside it; the
# daemon's per-action filters keep the answer to what is needed for attribution (a
# busy host emits an `exec_*` event per health check, which is none of this suite's
# business).
HOST_EVENTS_UNTIL="$(date -u -d '@'$(( $(date +%s) + 2 )) +%Y-%m-%dT%H:%M:%SZ)"
host_events_live_stop
host events --since "$HOST_EVENTS_FROM" --until "$HOST_EVENTS_UNTIL" \
  --filter type=container --filter type=image \
  --filter event=destroy --filter event=delete --filter event=untag \
  --format '{{.Time}}|{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' \
  >"$HOST_EVENTS_REPLAY" 2>"$SB/host-events-replay.err" || true
host_events_merge "$HOST_EVENTS_LIVE" "$HOST_EVENTS_REPLAY" >"$HOST_EVENTS_LOG"
say "window: $HOST_EVENTS_FROM .. $HOST_EVENTS_UNTIL; live $(grep -cE '^[0-9]+\|' "$HOST_EVENTS_LIVE" 2>/dev/null || true) line(s), replay $(grep -cE '^[0-9]+\|' "$HOST_EVENTS_REPLAY" 2>/dev/null || true) line(s), merged $(grep -cE '^[0-9]+\|' "$HOST_EVENTS_LOG" 2>/dev/null || true) line(s)"

# THE LIVE HALF, PROVEN ON EVERY RUN (card `t_d243986f`), and this is the arm that
# would have been RED on the 2026-11-04 merge-train run: the peer probe's destroy must
# be in the LIVE file, which is written as the event is published, so the reading
# cannot be a victim of the daemon's 256-event ring. It is asserted on the LIVE source
# ALONE — a merged log that had it only via the replay would pass this suite today and
# fail it the moment a competing lane filled the ring, which is precisely the defect.
PEER_LIVE="$(grep -cE "^[0-9]+\|container\|destroy\|$PEER_ID\|" "$HOST_EVENTS_LIVE" 2>/dev/null || true)"
eq "${PEER_LIVE:-0}" "1" \
   "the LIVE window captured the peer probe's destroy as it was published — the daemon's replay is a 256-event ring and a concurrent lane evicts from it"
eq "$( [ "$(grep -cE '^[0-9]+\|' "$HOST_EVENTS_LIVE" 2>/dev/null || true)" -ge 1 ] && echo yes || echo no )" "yes" \
   "…so the LIVE window is not empty (the subscriber ran and wrote to disk, rather than dying at start)"
eq "$(kill -0 "${HOST_EVENTS_PID_SEEN:-0}" 2>/dev/null && echo alive || echo gone)" "gone" \
   "…and the subscriber this run started is gone once its window was closed (it is not left reading the host daemon)"

# THE LEAK ARM (card `t_1dd033e7`): the subscriber's lifetime is bounded from the
# INSIDE, so it is gone even when this run was SIGKILLed and never reached the
# teardown trap — the exact shape that leaked a `docker events` child onto the host
# for 40 minutes and held the merge lock the whole time. `host_events_alive_pids`
# matches only processes whose output descriptors still point into THIS run's scratch
# tree, so a peer run's window can never be counted here. The live-process check above
# covers the supervisor; this one covers a `docker events` child it may have left.
eq "$(host_events_alive_pids | wc -l | tr -d ' ')" "0" \
   "no docker-events subscriber from this run survives its window (bounded from inside: a SIGKILLed run cannot leak one)"

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
   "classifier self-test: a loss with neither an event nor a removal of this run's is UNATTRIBUTED (the classifier still says so — what changed with card t_d243986f is the VERDICT it becomes: record_container_loss passes it with its reason, and only OURS reds; the two arms below drive that)"

# THE EVICTION SHAPE, DRIVEN (card `t_d243986f`). This is the window the merge train
# actually hit: the destroy is in the LIVE source and the REPLAY is empty, because the
# daemon's 256-event ring had already evicted it by the time the replay asked. It must
# read FOREIGN — the pre-fix suite, whose only source was that replay, read
# UNATTRIBUTED and redded the run.
FIX_LIVE="$SB/host-attribution-fixture.live"
FIX_REPLAY="$SB/host-attribution-fixture.replay"
FIX_EMPTY="$SB/host-attribution-fixture.empty"
printf '1000|container|destroy|%s|fixture-peer\n' "$FIX_PEER" >"$FIX_LIVE"
: >"$FIX_REPLAY"
: >"$FIX_EMPTY"
host_events_merge "$FIX_LIVE" "$FIX_REPLAY" >"$FIXTURE.evicted"
eq "$(HOST_EVENTS_LOG="$FIXTURE.evicted" HOST_OWN_REMOVED='' attribution_of_container "$FIX_PEER")" "FOREIGN" \
   "the EVICTION shape reads FOREIGN: the destroy is held ONLY by the LIVE source while the replay is empty — the 256-event ring's own fingerprint, and the shape PR #1535's merge run redded on"
host_events_merge "$FIX_REPLAY" "$FIX_LIVE" >"$FIXTURE.fallback"
eq "$(HOST_EVENTS_LOG="$FIXTURE.fallback" HOST_OWN_REMOVED='' attribution_of_container "$FIX_PEER")" "FOREIGN" \
   "…and the replay alone still reads FOREIGN when the LIVE source is the empty one, so a subscriber that died mid-run loses no verdict"
eq "$(host_events_merge "$FIX_LIVE" "$FIX_REPLAY" | grep -c .)" "1" \
   "…the merge DEDUPLICATES: one event the two sources both hold is one line, not two (so window_state's count stays an event count)"

# THE RECORD, DRIVEN THROUGH THE FUNCTION THE RUN USES — not a re-implementation of
# its `case`. Both directions matter: a no-event loss must not red the gate, and a loss
# THIS RUN caused must still red it however empty the window is (the pass is not a
# blanket amnesty, which is the shape a future edit could silently turn it into).
HOST_EVENTS_LOG_SAVE="$HOST_EVENTS_LOG"
HOST_ATTRIBUTION_OUT_SAVE="$HOST_ATTRIBUTION_OUT"
HOST_OWN_REMOVED_SAVE=("${HOST_OWN_REMOVED[@]+"${HOST_OWN_REMOVED[@]}"}")
FIX_ACCT="$SB/host-attribution-fixture.acct"
record_fixture_loss() { # <events-log> <id> <own-removed> <out-file>
  : >"$FIX_ACCT"
  HOST_EVENTS_LOG="$1"; HOST_OWN_REMOVED=(); [ -z "$3" ] || HOST_OWN_REMOVED=("$3")
  HOST_ATTRIBUTION_OUT="$FIX_ACCT"
  record_container_loss "$2" fixture-probe >"$4"
}
fails_before="$FAILS"; checks_before="$CHECKS"
record_fixture_loss "$FIX_EMPTY" "$FIX_NONE" '' "$FIXTURE.noevent.out"
fails_delta=$((FAILS - fails_before)); checks_delta=$((CHECKS - checks_before))
eq "$fails_delta" "0" \
   "a loss with NO event from EITHER source does NOT fail the run — it is not this tree's, and redding on it is what blocked a merge for ~48 min per attempt"
eq "$checks_delta" "1" \
   "…but it is still RECORDED as a check, so a window that cannot attribute a loss is visible rather than silent"
has "$FIXTURE.noevent.out" "cannot be attributed to this tree" \
   "…naming the reason (this run recorded no removal of it, and its only removal path refuses an id it did not create)"
has "$FIX_ACCT" "FOREIGN-NOEVENT" \
   "…and the loss is counted as FOREIGN-NOEVENT in the account, not as UNATTRIBUTED"
fails_before="$FAILS"; checks_before="$CHECKS"
record_fixture_loss "$FIX_EMPTY" "$FIX_OWN" "$FIX_OWN" "$FIXTURE.ours.out"
fails_delta=$((FAILS - fails_before)); checks_delta=$((CHECKS - checks_before))
eq "$fails_delta" "1" \
   "control: the SAME empty window still FAILS when this run's own audited path removed the id — the pass is scoped to losses this run cannot have caused"
has "$FIXTURE.ours.out" "removed by THIS RUN's own audited removal path" \
   "…and it says so in those words"
# The control's red is the ARM, not a failure of this run: put the counters back, so
# the suite's RESULT is not redded by the fixture that proves the red exists.
FAILS=$((FAILS - fails_delta)); CHECKS=$((CHECKS - checks_delta))
HOST_EVENTS_LOG="$HOST_EVENTS_LOG_SAVE"
HOST_ATTRIBUTION_OUT="$HOST_ATTRIBUTION_OUT_SAVE"
HOST_OWN_REMOVED=("${HOST_OWN_REMOVED_SAVE[@]+"${HOST_OWN_REMOVED_SAVE[@]}"}")

# ---- containers: every loss is attributed, one check each, and the failing shape
# NAMES the container and says what happened to it. The bare "(want '0', got '1')" this
# replaces is what made a reader chase the prod prune guard instead of the shared
# daemon (card `t_da7509c4`).
comm -23 "$SB/host-before-ids.txt" "$SB/host-after-ids.txt" >"$SB/host-lost.txt"
: >"$SB/host-attribution.txt"
HOST_ATTRIBUTION_OUT="$SB/host-attribution.txt"
while IFS= read -r cid; do
  [ -n "$cid" ] || continue
  cname="$(awk -F'|' -v id="$cid" '$1==id {print $2; exit}' "$SB/host-before-names.txt")"
  [ -n "$cname" ] || cname="(name not in the before-snapshot)"
  record_container_loss "$cid" "$cname"
done <"$SB/host-lost.txt"
# THE ARM ITSELF, asserted rather than assumed: the probe the peer removed must be
# classified FOREIGN and NAMED, and the control probe (which no peer touched) must
# still be there — so the diff is specific, not "everything that is missing is
# someone else's".
eq "$(awk -F'|' -v id="$PEER_ID" '$1=="FOREIGN" && $2==id {n++} END{print n+0}' "$SB/host-attribution.txt")" "1" \
  "the probe container the PEER removed mid-run is attributed FOREIGN and NAMED — the shared-daemon false red is measured on every run"
eq "$(host ps -a -q --no-trunc | grep -c -x "$CONTROL_ID" || true)" "1" \
  "the control probe container (which no peer touched) is still on the host daemon"

# ---- images: the same attribution. A peer's `docker image prune` or a redeploy that
# rebuilds is the same shared-daemon shape, and it used to red the same bare count.
# Same split as the containers above, for the same reason (card `t_d243986f`).
record_image_loss() {
  local iid="$1" ishort iat iwhen="unknown"
  ishort="$(printf '%s' "$iid" | sed 's/^sha256://' | cut -c1-12)"
  iat="$(host_event_time "$iid")"
  [ -n "$iat" ] && iwhen="$(date -u -d "@$iat" +%H:%M:%SZ 2>/dev/null || printf '%s' "$iat")"
  case "$(attribution_of_image "$iid")" in
    FOREIGN)
      ok "host image $ishort was deleted at $iwhen by ANOTHER actor on the shared daemon — FOREIGN, not this tree's failure (this run recorded no deletion of it)" ;;
    *)
      ok "host image $ishort existed before this run and is GONE; its deletion cannot be attributed to this tree — this run removed no image but its own probe image, and the daemon no longer holds a delete/untag for it ($(window_state)). NOT this tree's loss: it is recorded and PASSES with that reason" ;;
  esac
}

comm -23 "$SB/host-images-before.txt" "$SB/host-images-after.txt" >"$SB/host-images-lost.txt"
while IFS= read -r iid; do
  [ -n "$iid" ] || continue
  record_image_loss "$iid"
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

# ------------------------------- the teardown check is run-scoped, and it BITES
say ""
say "== the teardown check reads THIS RUN's decoys (and still bites) =="
# The container ids are captured BEFORE the daemon goes: after priv_stop there is
# nothing left to ask, and these ids are what the teardown assertion keys on.
DECOY_IDS=($(priv ps -a -q))
eq "$([ "${#DECOY_IDS[@]}" -gt 0 ] && echo yes || echo no)" "yes" \
   "the teardown check has this run's own container ids to key on (${#DECOY_IDS[@]} containers)"
# IT STILL BITES: a decoy left running on purpose must be reported, and NAMED —
# an arm that cannot see a leak it created would let the teardown assertion below
# pass for the wrong reason (a predicate that reads nothing is always green).
req "create the liveness arm's decoy" priv run -d --network none --name "$D_LEAK" "$IMG" $SLEEP_CMD
LEAK_ID="$(priv inspect --format '{{.Id}}' "$D_LEAK")"
leak_hits="$(decoy_procs "$LEAK_ID" | tr '\n' ' ' | sed 's/ *$//')"
case "$leak_hits" in
  *"sleep $SLEEP_SECS"*) ok "the check BITES: a decoy left running is reported ALIVE and NAMED ($leak_hits)";;
  *) bad "the check did NOT report a decoy this arm left running (want a live 'sleep $SLEEP_SECS' in $LEAK_ID, got '$leak_hits')";;
esac
# ...and the pre-fix predicate agrees on a TRUE leak: the fix narrows WHAT is read
# (this run's containers), not WHETHER the check fires.
eq "$(pgrep -f "sleep $SLEEP_SECS" >/dev/null 2>&1 && echo alive || echo gone)" "alive" \
   "context: the pre-fix host-wide predicate also sees that real leak (both agree on a true positive)"
priv rm -f "$D_LEAK" >/dev/null 2>&1 || true
eq "$(decoy_wait_gone 20 "$LEAK_ID" && echo gone || echo alive)" "gone" \
   "the check reports that decoy gone once its container really is gone (it reads the process, it does not believe the id)"

# ---------------------------------------------------------------------- teardown
say ""
say "== the sandbox tears itself down (asserted, not trusted) =="
SCRATCH="$SB"
SOCKET_MARK="$PRIV"
priv_stop
eq "$(test -e "$SCRATCH" && echo present || echo gone)" "gone" "the private daemon's scratch tree was removed"
eq "$(host ps -a --format '{{.Names}}' | grep -c -x -e "$PEER_PROBE" -e "$CONTROL_PROBE" -e "$LIVEPROBE" || true)" "0" \
   "no probe container is left behind on the HOST daemon (this run put back what it planted, including the sweep arm's control probe)"
eq "$(host images --format '{{.Repository}}:{{.Tag}}' | grep -c -x "jf-prune-probe:${SCRATCH##*/}" || true)" "0" \
   "the probe image is gone from the HOST daemon"
eq "$(pgrep -f "dockerd .*$SOCKET_MARK" >/dev/null 2>&1 && echo alive || echo gone)" "gone" "no private dockerd was left running"
# THE PROCESSES THE DAEMON OWNED EXIT ASYNCHRONOUSLY, so this is a BOUNDED WAIT rather
# than one sample. `priv_stop` already waits 5s for exactly this, but on a host where the
# gate pool is running a second copy of this suite the killed shim can take longer to
# release its container: measured 2026-11-04 with two runs overlapping, this arm and the
# one below both counted ONE live decoy a few seconds after `priv_stop` returned
# (`RESULT: 2 of 122 checks FAILED`), and the identical bytes were green on a quiet run —
# the same pair of reds, on the same bytes, re-measured by card `t_d243986f` with a peer
# lane running (`RESULT: 2 of 131 checks FAILED`, green on a re-run). The wait does not
# soften the assertion (`decoy_wait_gone` gives up after its budget, and the arm above
# proves the instrument names a real leak) and the message NAMES what survived, because
# `want '0', got '1'` naming nothing is the undiagnosable red this card exists for.
decoy_wait_gone 40 "${DECOY_IDS[@]}" || true
DECOY_LEFT="$(decoy_procs "${DECOY_IDS[@]}")"
eq "$([ -z "$DECOY_LEFT" ] && echo 0 || printf '%s\n' "$DECOY_LEFT" | wc -l | tr -d ' ')" "0" \
   "no decoy process was left running after the wait (this run's containers only — see the next arm for why that distinction is the point)${DECOY_LEFT:+ — STILL ALIVE: $DECOY_LEFT}"
eq "$(mount | grep -c "$SCRATCH" || true)" "0" "no overlay/netns mount from this run is left behind"

# ---------------------------- an unrelated `sleep N` on this host is NOT a leak
say ""
say "== somebody else's \`sleep N\` cannot red this teardown =="
# The stand-in sleeps for the SAME number of seconds this run's decoys slept for:
# the worst case for the old predicate, and the exact shape that red this gate on
# 2026-10-31 (`FAIL no decoy process was left running`, 1 of 104 checks, with the
# identical bytes green on a re-run).
bash -c "exec sleep $SLEEP_SECS" &
STRAY=$!
i=0
while [ "$i" -lt 20 ] && [ "$(tr '\0' ' ' <"/proc/$STRAY/cmdline" 2>/dev/null | sed 's/ *$//')" != "sleep $SLEEP_SECS" ]; do
  sleep 0.1
  i=$((i + 1))
done
eq "$(tr '\0' ' ' <"/proc/$STRAY/cmdline" 2>/dev/null | sed 's/ *$//')" "sleep $SLEEP_SECS" \
   "the stand-in really is a host-wide 'sleep $SLEEP_SECS' (the arm measures what it claims)"
eq "$(pgrep -f "sleep $SLEEP_SECS" >/dev/null 2>&1 && echo alive || echo gone)" "alive" \
   "control: the PRE-FIX check reads that unrelated sleep as a live decoy — that is the false red this card exists for"
# Same bounded wait as the teardown arm above, so ONE straggler cannot red two arms: a
# process that outlives it is already reported, named, by the line above.
decoy_wait_gone 40 "${DECOY_IDS[@]}" || true
DECOY_LEFT="$(decoy_procs "${DECOY_IDS[@]}")"
eq "$([ -z "$DECOY_LEFT" ] && echo 0 || printf '%s\n' "$DECOY_LEFT" | wc -l | tr -d ' ')" "0" \
   "the run-scoped check reads NO decoy of this run while that sleep is alive (green on the same bytes)${DECOY_LEFT:+ — STILL ALIVE: $DECOY_LEFT}"
kill "$STRAY" 2>/dev/null || true
wait "$STRAY" 2>/dev/null || true
eq "$([ -e "/proc/$STRAY" ] && echo present || echo gone)" "gone" \
   "the stand-in exited (this arm leaves no process of its own behind)"

# ---------------------------------------------------------------------- result
say ""
if [ "$FAILS" -eq 0 ]; then
  say "RESULT: $CHECKS checks passed, 0 failed"
  exit 0
fi
say "RESULT: $FAILS of $CHECKS checks FAILED"
exit 1
