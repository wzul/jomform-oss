#!/usr/bin/env bash
#
# scripts/biteproof_gate_concurrency.sh — ON DEMAND, NOT WIRED INTO THE GATE.
#
# WHAT IT PROVES (card `t_d243986f`). Two gates measure HOST state rather than the diff:
#
#   `prunehost`  deploy/host/jomform-container-prune_test.sh
#   `prunesync`  deploy/worktrees/check-prune-kanban-worktrees-host_test.sh
#
# and a PEER LANE running on the same host used to red both on a correct tree, which
# blocked PR #1535's merge for over an hour and cost ~48 min per retry (a red gate
# record cannot be reused, so the whole inventory was re-run):
#
#   * `prunehost` — 2 of 112: `FAIL host container 'jf-prune-peerprobe-<pid>' … existed
#     before this run and is GONE, and its removal CANNOT be attributed` plus
#     `the probe container the PEER removed mid-run is attributed FOREIGN and NAMED
#     (want '1', got '0')`. The window was a post-hoc `docker events --since` REPLAY,
#     and the daemon keeps only the last 256 events in memory (measured on this host:
#     `--since` 300s/600s/1800s/7200s all answer exactly 256), so a competing lane's
#     docker work evicted the probe's destroy before the replay asked for it.
#   * `prunesync` — 4 of 91, all on the repair arm, because its installer runs the
#     PRUNER SUITE as its source proof and a concurrent copy of that suite satisfies the
#     pruner's `has_process_matching "worktrees/$name "` guard with its own `git
#     worktree` argv (the sandbox's card ids were FIXED literals, because the ids are
#     the subject of the pruner's name→card-id derivation). One concurrent copy in four
#     is enough: `210 passed, 3 failed`. Fixed by deriving the fixture's ids per run
#     (`t_<6 hex><NN>`, card `t_d243986f`) so no two copies share a pattern — which also
#     covers the copies a lock cannot reach (a `--from-ref` extraction of an older
#     revision does not carry the lock, so serialising the cooperative copies would
#     leave exactly the colliding ones free).
#
# So this script runs BOTH gates while a peer does to this host exactly what the
# offending lane did — creates and removes containers on the host daemon, non-stop —
# and requires both to stay green. The `prunesync` leg ALSO runs a second copy of the
# pruner suite alongside the harness, because that is the shape that redded it (the
# harness's own installer proof is a suite run too). That second copy is what makes the
# fixture's id namespace the thing under test: the harness's proof runs the suite from
# the `--from-ref` revision, which on this branch is `main`'s copy WITHOUT the fix, so
# the two copies are exactly a fixed-id peer and a unique-id peer. The fix therefore has
# to work in BOTH directions — a lock alone could not, because a copy only takes the
# lock if it carries the lock.
#
# WHY IT IS NOT A GATE. It costs ~7-9 minutes (two gate-level runs, one of them the
# `prunesync` harness, which drives the installer's full proof path) and it writes to
# the shared host daemon for its whole duration. The per-merge gate keeps the cheap
# half of this: `prunehost`'s suite asserts on EVERY run that its LIVE event window
# captured the peer probe's destroy (the arm that was red on the failing merge), and
# that a loss with no event from either source passes with a reason while a loss THIS
# run caused still fails.
#
# RUN (as root, on the agent host):
#   bash scripts/biteproof_gate_concurrency.sh                # both gates
#   bash scripts/biteproof_gate_concurrency.sh --only prunehost
#   bash scripts/biteproof_gate_concurrency.sh --only prunesync
#
# EXIT: 0 only when every selected gate is GREEN under the peer; 1 otherwise; 2 when
# the proof itself could not run (no root, no daemon, no `busybox`/`dockerd`).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

ONLY="both"
PEER_PARALLEL=8
PEER_IMAGE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --only) ONLY="${2:-}"; shift 2 ;;
    --parallel) PEER_PARALLEL="${2:-8}"; shift 2 ;;
    --image) PEER_IMAGE="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,50p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done

say() { printf '%s\n' "$*"; }
step() { printf '\n=== %s\n' "$*"; }
die() { printf 'REFUSING: %s\n' "$*" >&2; exit 2; }

[ "$(id -u)" = 0 ] || die "this proof must run as root: prunehost starts a private docker daemon"
command -v docker >/dev/null 2>&1 || die "no docker CLI on PATH"
command -v dockerd >/dev/null 2>&1 || die "no dockerd binary — prunehost cannot start its private daemon"
docker info >/dev/null 2>&1 || die "the HOST docker daemon is unreachable"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/jf-gate-concurrency.XXXXXX")"
PEER_IMAGE_IMPORTED=0

# THE PEER IMAGE IS AN EMPTY ROOTFS, IMPORTED HERE — the way the prunehost suite builds
# its own. Nothing is pulled, no host image is borrowed (so the proof cannot become a
# measurement of what happens to be on the machine), and `create` costs milliseconds,
# which is what lets the peer fill the daemon's 256-event ring inside a gate run.
# Measured 2026-11-04: with the host's first image (a ~140 MB web image) eight workers
# managed only ~5 create+remove cycles/s; this is an order of magnitude cheaper.
if [ -z "$PEER_IMAGE" ]; then
  mkdir -p "$WORK/rootfs"
  tar -cf "$WORK/peer-image.tar" -C "$WORK/rootfs" .
  PEER_IMAGE="jf-gate-concurrency-peer:$$"
  docker import "$WORK/peer-image.tar" "$PEER_IMAGE" >/dev/null 2>&1 \
    || die "could not import the throwaway peer image"
  PEER_IMAGE_IMPORTED=1
fi
PEER_IMAGE="$(docker image inspect --format '{{.Id}}' "$PEER_IMAGE" 2>/dev/null)" \
  || die "the peer image does not resolve to an image id"
PEER_STOP="$WORK/stop"
PEER_PREFIX="jf-gate-concurrency-peer-$$"
PEER_PIDS=()
# A CUMULATIVE count of what the peer created (`<worker> <id>` per line), not a
# point-in-time `docker ps` sample: `create` + `rm -f` on an empty rootfs completes in
# milliseconds, so a one-shot listing can legitimately catch NONE of them even while
# every worker is cycling (measured 2026-11-04: the first version of this readiness
# check sampled `docker ps -a` after 2s and refused at 0 on a host where all 8 workers
# were running). The file is append-only and short-line, so concurrent appends are safe.
PEER_CREATED_LOG="$WORK/peer-created.log"
PEER_CREATE_ERR="$WORK/peer-create.err"
: >"$PEER_CREATED_LOG"
: >"$PEER_CREATE_ERR"

cleanup() {
  : >"$PEER_STOP"
  for p in ${PEER_PIDS[@]+"${PEER_PIDS[@]}"}; do kill "$p" 2>/dev/null; done
  for p in ${PEER_PIDS[@]+"${PEER_PIDS[@]}"}; do wait "$p" 2>/dev/null; done
  # Sweep anything the peers left: only names this run minted (pid-scoped).
  ids="$(docker ps -a -q --filter "name=$PEER_PREFIX" 2>/dev/null || true)"
  [ -n "$ids" ] && docker rm -f $ids >/dev/null 2>&1
  # …and the throwaway image, which this run imported and nothing else uses.
  [ "${PEER_IMAGE_IMPORTED:-0}" = 1 ] && docker rmi -f "$PEER_IMAGE" >/dev/null 2>&1
  # Keep the logs of a RED proof — the reason is in them — and remove a green one.
  if [ "${RESULT:-1}" -eq 0 ]; then
    rm -rf "$WORK"
  else
    printf 'logs kept in %s\n' "$WORK"
  fi
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- the peer lane
# The shape the offending lane had: containers appearing and disappearing on the shared
# host daemon while both gates measure it. `create` + `rm` on an empty rootfs is the
# cheapest way to produce real events (measured ~13 events/s at 8-way on the agent
# host), which is what fills the daemon's 256-event ring that the pre-fix `prunehost`
# replay read.
peer_worker() { # <n>
  local n="$1" i=0 id
  while [ ! -f "$PEER_STOP" ]; do
    i=$((i + 1))
    id="$(docker create --name "$PEER_PREFIX-$n-$i" "$PEER_IMAGE" true 2>>"$PEER_CREATE_ERR")" \
      || { sleep 0.2; continue; }
    printf '%s %s\n' "$n" "$id" >>"$PEER_CREATED_LOG"
    docker rm -f "$id" >/dev/null 2>&1
  done
}
start_peer() {
  step "the peer lane: $PEER_PARALLEL workers creating and removing containers on the HOST daemon"
  say "names: $PEER_PREFIX-<worker>-<n> (pid-scoped, swept on exit)"
  local n
  for n in $(seq 1 "$PEER_PARALLEL"); do
    peer_worker "$n" &
    PEER_PIDS+=($!)
  done
  # Up to 10s for the peers to prove they are doing anything at all, then a 1s sample
  # of their rate. A peer that creates nothing means the proof measures nothing, and
  # that is a REFUSAL (exit 2), never a green.
  local waited=0 created=0
  while [ "$waited" -lt 20 ]; do
    created="$(grep -c . "$PEER_CREATED_LOG" 2>/dev/null || true)"
    [ "${created:-0}" -gt 0 ] && break
    sleep 0.5; waited=$((waited + 1))
  done
  [ "${created:-0}" -gt 0 ] \
    || die "the peer created no container in 10s — the proof would measure nothing (stderr: $PEER_CREATE_ERR)"
  local mark="$created"
  sleep 1
  created="$(grep -c . "$PEER_CREATED_LOG" 2>/dev/null || true)"
  [ "$((created - mark))" -gt 0 ] \
    || die "the peer created ${mark} container(s) and then stalled for a second — a peer that stopped is not the shape this proof is about"
  say "the peer is running: ${mark} container(s) in the first $((waited / 2))s, then $((created - mark)) more in the next second (~$((created - mark)) create+remove cycles/s, 2 daemon events each)"
}

# ---------------------------------------------------------------- the gates
run_prunehost() {
  step "prunehost — the decoy suite, WITH the peer running"
  local log="$WORK/prunehost.log"
  bash "$ROOT/deploy/host/jomform-container-prune_test.sh" >"$log" 2>&1; local rc=$?
  grep -E '^RESULT|^  FAIL' "$log" | tail -20 | sed 's/^/    /'
  local failed
  failed="$(grep -c '^  FAIL' "$log" || true)"
  say "    rc=$rc, ${failed:-0} failing check(s)"
  [ "$rc" -eq 0 ] && [ "${failed:-0}" -eq 0 ]
}

run_prunesync() {
  step "prunesync — the checker/installer harness, WITH the peer running"
  # Two copies of the pruner suite at once is the shape that redded this gate (its
  # installer runs the suite as its source proof, and a concurrent copy's `git
  # worktree` argv satisfied the pruner's live-process guard). One of the copies is
  # driven here; the harness drives the other two.
  local second="$WORK/concurrent-suite.log"
  bash "$ROOT/deploy/worktrees/prune-kanban-worktrees_test.sh" >"$second" 2>&1 &
  local second_pid=$!
  local log="$WORK/prunesync.log"
  bash "$ROOT/deploy/worktrees/check-prune-kanban-worktrees-host_test.sh" >"$log" 2>&1; local rc=$?
  wait "$second_pid" 2>/dev/null; local second_rc=$?
  grep -E 'self-test: [0-9]+ passed|^  FAIL' "$log" | tail -20 | sed 's/^/    /'
  say "    concurrent pruner suite: $(grep -E 'self-test: [0-9]+ passed' "$second" | tail -1) (rc=$second_rc)"
  say "    rc=$rc"
  [ "$rc" -eq 0 ] && [ "$second_rc" -eq 0 ]
}

start_peer

RESULT=0
case "$ONLY" in
  both)      run_prunehost || RESULT=1; run_prunesync || RESULT=1 ;;
  prunehost) run_prunehost || RESULT=1 ;;
  prunesync) run_prunesync || RESULT=1 ;;
  *) die "--only takes 'prunehost', 'prunesync' or 'both' (got '$ONLY')" ;;
esac

step "verdict"
if [ "$RESULT" -eq 0 ]; then
  say "GREEN: every selected gate stayed green while a peer lane created and removed"
  say "        containers on the same host daemon for the whole run."
  exit 0
fi
say "RED: at least one gate did not survive the peer. The logs are in $WORK"
say "     (this script removes that directory on exit; re-run with the gate alone to"
say "      tell a tree failure from a concurrency failure)"
exit 1
