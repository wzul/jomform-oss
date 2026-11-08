#!/usr/bin/env bash
#
# jomform-container-prune_leak_bite.sh — the SIGKILL leak proof for the LIVE event
# window of `jomform-container-prune_test.sh` (card `t_1dd033e7`).
#
# WHAT IT PROVES, AND WHY IT CANNOT BE A CHECK INSIDE THE SUITE. The suite's own
# teardown closes its `docker events` subscriber, but a run that is SIGKILLed — the gate
# runner's last signal on a timeout, a killed slot, a torn-down card container — never
# reaches a trap. Measured 2026-11-05: the subscriber of such a run was still reading
# the host daemon 40 minutes later, reparented to `systemd --user`, and it held BOTH
# `/var/lock/jomform-merge.lock` and a preflight slot through the descriptors it had
# inherited from the run above it. `flock` is OPEN-FILE-DESCRIPTION scoped, so a dead
# pid's lock stays held while any descendant keeps the descriptor open — the merger's
# `flock -w 3600` waited on a lock nobody was using.
#
# So this harness drives the shape a normal run cannot: it takes the merge lock in ITS
# OWN shell at fd 200 (the number the merge lane's `flock` wrapper uses), starts the
# suite under it, waits for the LIVE window to open, SIGKILLs the suite, and then
# requires, within a bounded time:
#
#   1. NO `docker … events` process from that run survives; and
#   2. `flock -n /var/lock/jomform-merge.lock true` SUCCEEDS once this harness drops its
#      own descriptor — i.e. no child kept the lock alive.
#
# AND IT PROVES IT DISCRIMINATES. A green on its own is a tautology, so the default run
# also drives a CONTROL: a copy of the suite whose subscriber is put back to the
# PRE-FIX shape (a bare `docker events --since` stream, no `--until`, no reaper, no
# descriptor closure). On that copy the SAME SIGKILL shape must leave a survivor and a
# still-held lock, and the harness must report RED for it. A control that comes back
# clean means the harness is measuring nothing and the run fails.
#
# It is ON DEMAND, not a preflight gate: it deliberately kills a suite mid-run, so it
# is not something a per-merge gate should do.
#
# Run:  bash deploy/host/jomform-container-prune_leak_bite.sh            # control + fixed
#       bash deploy/host/jomform-container-prune_leak_bite.sh --control-only
# Exit: 0 only if the control read RED and the committed suite read GREEN;
#       1 if either half did not behave as expected; 2 if the harness could not run.
#
# THE MERGE LOCK IS A PRECONDITION, NOT A CASUALTY: the harness only runs while the lock
# is FREE (it must be able to take it), and it releases its own descriptor before the
# assertion. It never signals another lane's process and it never touches a container
# it did not plant.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_FILE=jomform-container-prune_test.sh
SUITE="$HERE/$SUITE_FILE"
HOST_SOCK="${JF_PRUNE_BITE_HOST_SOCKET:-unix:///var/run/docker.sock}"
REAL_MERGE_LOCK="${JF_PRUNE_LEAK_MERGE_LOCK:-/var/lock/jomform-merge.lock}"
SURVIVOR_WAIT="${JF_PRUNE_LEAK_SURVIVOR_WAIT:-15}"     # seconds
WINDOW_WAIT="${JF_PRUNE_LEAK_WINDOW_WAIT:-90}"         # seconds to reach the LIVE window
CONTROL_ONLY=0

WORK=""
PREP_DIR=""
SUITE_PID=""
SANDBOX=""
CHECKS=0
FAILS=0

say()   { printf '%s\n' "$*"; }
fatal() { printf 'FATAL: %s\n' "$*" >&2; exit 2; }
ok()    { CHECKS=$((CHECKS + 1)); printf '  ok   %s\n' "$1"; }
bad()   { CHECKS=$((CHECKS + 1)); FAILS=$((FAILS + 1)); printf '  FAIL %s\n' "$1"; }

case "$(uname -s)" in Linux) ;; *) fatal "this harness is Linux-only (it reads /proc)" ;; esac
[ "$(id -u)" = 0 ] || fatal "this suite must run as root: it starts a private docker daemon"
[ -f "$SUITE" ] || fatal "$SUITE not found"
bash -n "$SUITE" || fatal "$SUITE is not valid bash"
command -v flock >/dev/null 2>&1 || fatal "flock is missing — the lock half cannot be measured"
command -v unshare >/dev/null 2>&1 || fatal "unshare is missing — the suite cannot start"
command -v nsenter >/dev/null 2>&1 || fatal "nsenter is missing — the suite cannot start"
command -v dockerd >/dev/null 2>&1 || fatal "dockerd is not on PATH"
docker --host "$HOST_SOCK" info >/dev/null 2>&1 \
  || fatal "the HOST docker daemon is unreachable ($HOST_SOCK)"

# sub <file> <old> <new> — replace an anchor EXACTLY once. A count of 0 or 2+ aborts the
# case: a silent no-op here would report on the shipped file while claiming to test the
# pre-fix one.
sub() {
  python3 - "$1" "$2" "$3" <<'PY'
import pathlib, sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
text = pathlib.Path(path).read_text()
n = text.count(old)
if n != 1:
    sys.stderr.write("anchor occurs %d times, expected exactly 1:\n%s\n" % (n, old))
    sys.exit(3)
pathlib.Path(path).write_text(text.replace(old, new))
PY
}

# events_pids — every `docker … events` process whose stdout/stderr still points into
# THIS case's private TMPDIR. That is exactly the set this case started: a concurrent
# gate run's subscriber writes into its own scratch tree, so it can never be counted
# (and never be killed) here.
#
# ONE `ps` PER SCAN, then a readlink only for the few matches. The obvious shape —
# walk /proc/[0-9]*/fd/* and readlink every descriptor — costs several forks per
# descriptor and took MINUTES per scan on this host while a merge train was running,
# which turned the harness's 15s bound into a hang. Measure the cost of a /proc scan
# before putting it in a poll loop.
events_pids() {
  local pid cmd target
  ps -eo pid=,args= 2>/dev/null | while read -r pid cmd; do
    [ -n "$pid" ] || continue
    [ "$pid" = "$$" ] && continue
    case "$cmd" in *docker*" events"*) ;; *) continue ;; esac
    for target in 1 2; do
      case "$(readlink "/proc/$pid/fd/$target" 2>/dev/null || true)" in
        *"$WORK/tmp"*) printf '%s\n' "$pid"; break ;;
      esac
    done
  done
}

# lock_holders — every process still holding an open descriptor on the lock file, with
# its command line. A failed lock assertion must NAME the holder: "the lock is held" is
# unactionable, and the whole incident was a lock held by a pid that looked dead.
# `fuser` answers it in one syscall; the /proc walk costs thousands of forks.
lock_holders() {
  local p
  for p in $(fuser "$REAL_MERGE_LOCK" 2>/dev/null || true); do
    printf '    held by pid %s: %s\n' "$p" \
      "$(ps -o args= -p "$p" 2>/dev/null | cut -c1-150)"
  done
}

# cleanup_case — leave the host exactly as it was found: the survivors this case may
# have left, the probe containers/image it planted on the HOST daemon (named after the
# suite pid, so the names belong to this case), and its scratch tree. A private dockerd
# is only ever started well after the point this harness kills the suite; it is handled
# anyway so a late kill cannot leak one.
cleanup_case() {
  local suite_pid="$1" p d ids
  for p in $(events_pids); do kill -9 "$p" 2>/dev/null || true; done
  sleep 0.2
  docker --host "$HOST_SOCK" rm -f \
    "jf-prune-peerprobe-$suite_pid" \
    "jf-prune-controlprobe-$suite_pid" \
    "jf-gate-concurrency-peer-$suite_pid-2-2" >/dev/null 2>&1 || true
  for d in "$WORK"/tmp/jf-prune-bite.*; do
    [ -d "$d" ] || continue
    docker --host "$HOST_SOCK" rmi "jf-prune-probe:${d##*/}" >/dev/null 2>&1 || true
    if [ -S "$d/docker.sock" ]; then
      ids="$(docker --host "unix://$d/docker.sock" ps -aq 2>/dev/null || true)"
      if [ -n "$ids" ]; then
        docker --host "unix://$d/docker.sock" rm -f $ids >/dev/null 2>&1 || true
      fi
    fi
    if [ -f "$d/dockerd.pid" ]; then
      p="$(cat "$d/dockerd.pid" 2>/dev/null || true)"
      [ -n "$p" ] && kill -9 "$p" 2>/dev/null || true
    fi
    rm -rf "$d"
  done
  wait "$suite_pid" 2>/dev/null || true
  rm -rf "$WORK"
  WORK=""
}

# run_case <label> <suite-path> <expect: clean|leak>
#
#   clean — the POST-FIX expectation: no subscriber survives, the lock is free.
#   leak  — the PRE-FIX control: a subscriber survives and STILL HOLDS the lock. The
#           harness reports this as "the assertion was RED", which is what proves the
#           assertion discriminates.
run_case() {
  local label="$1" suite="$2" expect="$3"
  local i=0 marker=0 suite_pid survivors lock_state
  say ""
  say "=============================================================="
  say "CASE: $label (expect: $expect)"
  WORK="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-leak.XXXXXX")" || fatal "could not create a scratch dir"
  mkdir -p "$WORK/tmp"

  # THE PRECONDITION. The lock must be free for this harness to take it; if a merger is
  # running, this case cannot measure the merge-lock half at all, and saying so is
  # better than reporting a green that measured nothing else.
  if ! flock -n "$REAL_MERGE_LOCK" true 2>/dev/null; then
    say "  SKIP: $REAL_MERGE_LOCK is held by another lane right now — re-run when nothing is merging"
    rm -rf "$WORK"; WORK=""
    return 2
  fi

  exec 200>>"$REAL_MERGE_LOCK" || fatal "could not open $REAL_MERGE_LOCK on fd 200"
  flock -n 200 || fatal "could not take $REAL_MERGE_LOCK on fd 200"
  say "  holding $REAL_MERGE_LOCK on fd 200, exactly as the merge lane's flock wrapper hands it down"

  TMPDIR="$WORK/tmp" bash "$suite" >"$WORK/run.log" 2>&1 &
  suite_pid=$!
  SUITE_PID=$suite_pid

  # 1. Wait for the LIVE window to open. This is the moment a killed run has a
  #    subscriber on the host; killing earlier would measure nothing.
  while [ "$i" -lt $(( WINDOW_WAIT * 2 )) ]; do
    if grep -q 'window: LIVE subscriber open since' "$WORK/run.log" 2>/dev/null; then marker=1; break; fi
    if ! kill -0 "$suite_pid" 2>/dev/null; then break; fi
    sleep 0.5
    i=$((i + 1))
  done
  if [ "$marker" != 1 ]; then
    say "  the suite never reached its LIVE window; last lines:"
    tail -8 "$WORK/run.log" 2>/dev/null | sed 's/^/    /'
    exec 200>&- 2>/dev/null || true
    cleanup_case "$suite_pid"
    bad "$label: the suite reached its LIVE window (it did not — nothing was measured)"
    return 1
  fi
  say "  the LIVE window is open (suite pid $suite_pid)"

  # 2. Require the window to actually be streaming: at least one `docker … events`
  #    child must be visible, or the kill below would measure an empty window.
  survivors=""
  i=0
  while [ "$i" -lt 40 ]; do
    survivors="$(events_pids)"
    [ -n "$survivors" ] && break
    sleep 0.25
    i=$((i + 1))
  done
  if [ -z "$survivors" ]; then
    exec 200>&- 2>/dev/null || true
    cleanup_case "$suite_pid"
    bad "$label: the LIVE window has a docker-events child to leak in the first place (none was visible)"
    return 1
  fi
  say "  stream process(es) before the kill: $(printf '%s' "$survivors" | tr '\n' ' ')"

  # 3. THE SIGKILL. This is the signal a gate run actually dies of; no trap runs.
  kill -9 "$suite_pid" 2>/dev/null || true
  wait "$suite_pid" 2>/dev/null || true

  # 4. Bounded wait: does any subscriber from this run survive?
  survivors="$(events_pids)"
  i=0
  while [ -n "$survivors" ] && [ "$i" -lt $(( SURVIVOR_WAIT * 4 )) ]; do
    sleep 0.25
    i=$((i + 1))
    survivors="$(events_pids)"
  done

  # 5. Drop THIS harness's own descriptor, then ask whether the lock is free. In the
  #    pre-fix shape the surviving child still holds the same open file description,
  #    so the answer is "held" for a pid that is long dead — the 40-minute stall.
  #
  #    Polled, not sampled once: a child may still be exiting when the SIGKILL lands
  #    (the supervisor's current window runs to its `--until`), and "within a bounded
  #    time" is the property the card asks for. The control's holder never goes away, so
  #    the same bound still separates the two shapes.
  exec 200>&-
  i=0
  while :; do
    if flock -n "$REAL_MERGE_LOCK" true 2>/dev/null; then lock_state=free; break; fi
    lock_state=held
    [ "$i" -ge $(( SURVIVOR_WAIT * 8 )) ] && break
    sleep 0.125
    i=$((i + 1))
  done
  # Name the holder only once, and only if it outlived the bound: scanning every /proc
  # descriptor is expensive, so doing it inside the poll turned a 15s bound into minutes.
  if [ "$lock_state" = held ]; then
    holders="$(lock_holders)"
    if [ -n "$holders" ]; then
      say "  the lock is STILL held after ${SURVIVOR_WAIT}s, by:"
      printf '%s\n' "$holders"
    else
      say "  the lock reads HELD but no process has an fd on it — inspect /proc/locks before trusting this run"
    fi
  fi
  say "  survivors after ${SURVIVOR_WAIT}s: $(printf '%s' "${survivors:-none}" | tr '\n' ' ')"
  say "  merge lock after releasing fd 200: $lock_state"

  if [ "$expect" = clean ]; then
    if [ -z "$survivors" ]; then
      ok "$label: no docker-events subscriber from the killed run survives"
    else
      bad "$label: a docker-events subscriber from the killed run SURVIVED (pids: $(printf '%s' "$survivors" | tr '\n' ' '))"
    fi
    if [ "$lock_state" = free ]; then
      ok "$label: $REAL_MERGE_LOCK is FREE after the killed run — no child kept the descriptor"
    else
      bad "$label: $REAL_MERGE_LOCK is still HELD by a killed run's child — the merger would stall on a lock nobody is using"
      lock_holders
    fi
  else
    # The control is expected to LEAK. If it does not, this harness proves nothing.
    if [ -n "$survivors" ]; then
      ok "$label: the PRE-FIX shape leaked a subscriber as expected (pids: $(printf '%s' "$survivors" | tr '\n' ' ')) — the assertion discriminates"
    else
      bad "$label: the PRE-FIX shape did NOT leak a subscriber, so the harness measures nothing"
    fi
    if [ "$lock_state" = held ]; then
      ok "$label: the PRE-FIX shape kept the merge lock held after its run was killed — the exact 2026-11-05 stall, reproduced"
    else
      bad "$label: the PRE-FIX shape did NOT keep the merge lock held, so the lock assertion measures nothing"
    fi
  fi

  cleanup_case "$suite_pid"
  return 0
}

# ---------------------------------------------------------------- the cases
if [ "${1:-}" = "--control-only" ]; then CONTROL_ONLY=1; fi
if [ "${1:-}" = "--fixed-only" ]; then CONTROL_ONLY=2; fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-leak-prep.XXXXXX")" || fatal "could not create a prep dir"
PREP_DIR="$WORK"
trap 'if [ -n "$PREP_DIR" ]; then case "$PREP_DIR" in "${TMPDIR:-/tmp}"/jf-prune-leak-*) rm -rf "$PREP_DIR" ;; esac; fi
      if [ -n "$WORK" ]; then case "$WORK" in "${TMPDIR:-/tmp}"/jf-prune-leak-*) rm -rf "$WORK" ;; esac; fi' EXIT
cp "$SUITE" "$WORK/$SUITE_FILE"
# The suite resolves its neighbours through BASH_SOURCE, so the control copy needs them
# BESIDE it: without the guard in the same directory the copy FATALs before it ever
# opens its LIVE window, and the case would report "the suite never reached its window"
# instead of measuring a leak.
cp "$HERE/jomform-container-prune.sh" "$HERE/install-host-container-prune.sh" "$WORK/" \
  || fatal "the control copy could not be assembled (guard/installer missing beside the suite)"
CONTROL="$WORK/$SUITE_FILE"
WORK=""

# THE CONTROL PATCH: put the PRE-FIX subscriber back. The anchor is the call this
# harness's fix introduced, so a rename in the suite must break this case loudly
# (sub aborts when the anchor is missing) rather than silently measure the fixed file.
old='  host_events_spawn_subscriber
  # A failure here is already recorded as a FAIL; the window itself opened, so the run'
new="$(cat <<'EOF'
  host events --since "$HOST_EVENTS_FROM" \
    --filter type=container --filter type=image \
    --filter event=destroy --filter event=delete --filter event=untag \
    --format '{{.Time}}|{{.Type}}|{{.Action}}|{{.Actor.ID}}|{{.Actor.Attributes.name}}' \
    >>"$HOST_EVENTS_LIVE" 2>"$SB/host-events-live.err" &
  HOST_EVENTS_PID=$!
  HOST_EVENTS_PID_SEEN=$HOST_EVENTS_PID
  HOST_EVENTS_SUP_PID=$HOST_EVENTS_PID
  # A failure here is already recorded as a FAIL; the window itself opened, so the run
EOF
)"
if ! sub "$CONTROL" "$old" "$new"; then
  say "ABORT: the control patch did not land — the harness cannot produce the pre-fix shape"
  exit 2
fi
if ! bash -n "$CONTROL"; then
  say "ABORT: the control copy is not valid bash"
  exit 2
fi

if [ "$CONTROL_ONLY" = 0 ]; then
  run_case "CONTROL — pre-fix subscriber (must leak, so the assertion bites)" "$CONTROL" leak
  CONTROL_RC=$?
  if [ "$CONTROL_RC" = 2 ]; then
    say ""
    say "ABORT: the control could not run (the merge lock was busy); nothing was measured"
    exit 2
  fi
fi

if [ "$CONTROL_ONLY" != 1 ]; then
  run_case "FIXED — the committed suite under the same SIGKILL" "$SUITE" clean
  FIXED_RC=$?
  if [ "$FIXED_RC" = 2 ]; then
    say ""
    say "ABORT: the fixed case could not run (the merge lock was busy); nothing was measured"
    exit 2
  fi
fi

# ---------------------------------------------------------------- the verdict
say ""
say "RESULT: $((CHECKS - FAILS)) of $CHECKS checks passed, $FAILS failed"
if [ "$FAILS" -eq 0 ]; then
  say "RESULT: the SIGKILL shape LEAKS on the pre-fix subscriber and is CLEAN on the committed suite — exit 0"
  exit 0
fi
say "RESULT: a case did not behave as expected — exit 1"
exit 1
