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
# The decoys, and the gate each one exists for:
#   jf-prune-decoy-young     exited,  no label  -> the AGE gate   (floor 168h: not a candidate)
#   jf-prune-decoy-plain     exited,  no label  -> the guard BITES (floor 0: removed)
#   jf-prune-decoy-compose   created, com.docker.compose.project=jomform -> the COMPOSE gate
#   jf-prune-decoy-keepfile  created, named in the keep file -> the KEEP gate
#   jf-prune-decoy-running   RUNNING            -> the STATE gate (never a candidate, survives a floor-0 run)
#
# Plus the two halves of the guard that are not about a decoy: it FAILS CLOSED on
# an empty container list, and it never removes an image.
#
# It also drives the INSTALLER (install-host-container-prune.sh) twice against a
# sandbox prefix with a fake `crontab` on PATH, and asserts the cron entry stays
# at exactly one line, the installed copy's sha256 matches, and `--dry-run`
# prints the exact production cron line while invoking no crontab at all.
#
# REQUIREMENTS (it FAILS, never skips, without them): root — a private dockerd
# cannot run unprivileged — plus the `dockerd` binary, `busybox` (for a 1 MB
# throwaway image, so no image has to be pulled or borrowed) and a reachable HOST
# docker daemon, which is read ONLY to prove this run did not touch it.
#
# Run:  bash deploy/host/jomform-container-prune_test.sh
# Exit: 0 only if every assertion held; 1 = an assertion failed; 2 = the suite
#       itself could not run (missing prerequisite, private daemon would not start).

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="${JF_PRUNE_SCRIPT:-$HERE/jomform-container-prune.sh}"
INSTALLER="${JF_PRUNE_INSTALLER:-$HERE/install-host-container-prune.sh}"

# The HOST daemon is read-only for this suite: two snapshots, compared at the end.
HOST_SOCK="${JF_PRUNE_BITE_HOST_SOCKET:-unix:///var/run/docker.sock}"

DECOY="jf-prune-decoy"
D_YOUNG="$DECOY-young"
D_PLAIN="$DECOY-plain"
D_COMPOSE="$DECOY-compose"
D_KEEPFILE="$DECOY-keepfile"
D_RUNNING="$DECOY-running"
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

# ---------------------------------------------------------------- the sandbox
priv() { docker --host "$PRIV_URL" "$@"; }
host() { docker --host "$HOST_SOCK" "$@"; }

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
  release_mounts
  drop_scratch
}

# A leaked daemon would hold a socket, an overlay mount per decoy and a process
# for every container it still knows about — so the trap is not decoration, and
# the suite asserts its own teardown rather than trusting it.
trap 'priv_stop' EXIT
trap 'priv_stop; exit 130' INT TERM HUP

priv_start() {
  SB="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-bite.XXXXXX")"
  PRIV="$SB/docker.sock"
  PRIV_URL="unix://$PRIV"
  mkdir -p "$SB/root" "$SB/rootfs"
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
run_guard() { # <floor-hours> <dry-run 0|1> <out-file>
  local floor="$1" dry="$2" out="$3" rc=0
  DOCKER_HOST="$PRIV_URL" \
    JF_PRUNE_MIN_AGE_H="$floor" \
    JF_PRUNE_DRY_RUN="$dry" \
    JF_PRUNE_KEEP_FILE="$KEEPFILE" \
    bash "$GUARD" >"$out" 2>&1 || rc=$?
  return "$rc"
}

# ------------------------------------------------------------------ run it
[ "$(id -u)" = 0 ] || fatal "this suite must run as root: it starts a private docker daemon"
[ -f "$GUARD" ] || fatal "$GUARD not found"
[ -f "$INSTALLER" ] || fatal "$INSTALLER not found"
bash -n "$GUARD" || fatal "$GUARD is not valid bash"
bash -n "$INSTALLER" || fatal "$INSTALLER is not valid bash"
command -v dockerd >/dev/null 2>&1 || fatal "dockerd not on PATH — the private daemon cannot be started"
host info >/dev/null 2>&1 || fatal "the HOST docker daemon is unreachable ($HOST_SOCK) — 'nothing else was touched' could not be proven without it"

# The host's own container and image sets, so the claim "this suite removed
# nothing outside its sandbox" is a measurement. Peers may ADD during the run;
# losing one of these would be the failure.
HOST_CONTAINERS_BEFORE="$(host ps -a -q | sort)"
HOST_IMAGES_BEFORE="$(host images -q | sort)"

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
priv_image
KEEPFILE="$SB/keep-containers"
printf '%s\n' "$D_KEEPFILE" >"$KEEPFILE"

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

# ------------------------------------------------------- the host was not touched
say ""
say "== the HOST daemon was not touched =="
lost_containers="$(comm -23 <(printf '%s\n' "$HOST_CONTAINERS_BEFORE") <(host ps -a -q | sort) | grep -c . || true)"
lost_images="$(comm -23 <(printf '%s\n' "$HOST_IMAGES_BEFORE") <(host images -q | sort) | grep -c . || true)"
eq "$lost_containers" "0" "no host container that existed before this run is gone"
eq "$lost_images" "0" "no host image that existed before this run is gone"
eq "$(host ps -a --format '{{.Names}}' | grep -c "^$DECOY-" || true)" "0" "no decoy ever existed on the host daemon"

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
