#!/usr/bin/env bash
# ship_proof.sh — ONE definition of "this tag is LIVE on the server".
#
# Sourced (never executed) by the two places that need the answer:
#   deploy/build-and-ship.sh  after it has shipped, to PROVE the recreate took effect
#   deploy/cut-release.sh     before it mirrors, so the public repo only ever carries a
#                             tree prod actually received
#
# WHY THIS FILE EXISTS (measured on the v1.27.3 release, 2026-11-08). `cut-release.sh
# --yes --tag v1.27.3` exited 0 and printed "shipped the API image", "shipped the web
# image", "mirrored v1.27.3", "RELEASED v1.27.3" while build-and-ship had only printed
# "SSH_HOST not set — skipping scp": prod still ran v1.27.1, no jomform-api:v1.27.3 image
# existed on the host, and the public mirror advertised a release prod had never seen.
# The release path only looked at an EXIT CODE, so "the build finished" was being read as
# "the server has it". Nothing in the repo could tell those two apart, because nothing had
# ever asked the server what it was running. This file is that question, so it is asked the
# same way by both scripts instead of being invented twice.
#
# WHAT COUNTS AS EVIDENCE: THE RUNNING CONTAINER'S IMAGE LABEL.
#   docker compose ps -q <service>                       -> the container compose is running
#   docker inspect --format '{{index .Config.Labels \
#                     "org.opencontainers.image.version"}}' <container>
# build-and-ship stamps org.opencontainers.image.version on BOTH images from the tag it
# built (see BUILD_ARGS), and a container inherits its image's labels. So one question
# answers it for both halves: is the service that is RUNNING the bytes just shipped?
#
# WHY NOT `curl /version`: that endpoint proves the API only. The API answering the new
# version while the web container still serves an older bundle is precisely the half-shipped
# state this repo has been in before (issue #576), and the web image has no version endpoint
# and no startup banner at all. The label check covers both, and it is answered by the
# running container rather than by the process that claims to have shipped it.
#
# WHY NOT `docker image inspect <image>:<tag>`: that proves an image EXISTS on the host, not
# that it is what runs. The 2026-11-08 failure would pass that check the moment the tarball
# was loaded, while prod kept serving the old container.
#
# USAGE (the caller needs no arguments; it sets the same variables build-and-ship uses):
#   . deploy/ship_proof.sh
#   ship_proof_target                      # 0 + SP_DEST/SP_SSH_OPTS/SP_COMPOSE_DIR, or 1
#   ship_proof_live <service> <image> <tag>  # 0 iff the running service carries <tag>
#
# Reads: SSH_HOST, SSH_USER, SSH_PORT, REMOTE_BUILD_HOST, REMOTE_BUILD_SSH_USER,
#        REMOTE_BUILD_SSH_PORT, REMOTE_COMPOSE_DIR (default /opt/jomform).
# A remote build wins: that path builds ON the server, so the server is REMOTE_BUILD_HOST.

# ship_proof_target — pick the server and its ssh options.
# Returns 0 when there is one to reach, 1 when neither SSH_HOST nor REMOTE_BUILD_HOST is
# set. A caller that must ship treats 1 as a refusal, never as a skip.
ship_proof_target() {
  local host user port
  if [ -n "${REMOTE_BUILD_HOST:-}" ]; then
    host=$REMOTE_BUILD_HOST
    user=${REMOTE_BUILD_SSH_USER:-root}
    port=${REMOTE_BUILD_SSH_PORT:-22}
    SP_TARGET_KIND="REMOTE_BUILD_HOST=$host"
  elif [ -n "${SSH_HOST:-}" ]; then
    host=$SSH_HOST
    user=${SSH_USER:-root}
    port=${SSH_PORT:-22}
    SP_TARGET_KIND="SSH_HOST=$host"
  else
    SP_TARGET_KIND=""
    SP_DEST=""
    SP_SSH_OPTS=()
    return 1
  fi
  SP_HOST=$host
  SP_USER=$user
  SP_PORT=$port
  SP_DEST="$user@$host"
  # The compose directory the printed server steps have always used (build-and-ship § 9,
  # deploy/monitoring/jomform-watch.sh). Same default, so the proof asks the same stack the
  # operator recreates.
  SP_COMPOSE_DIR="${REMOTE_COMPOSE_DIR:-/opt/jomform}"
  # BatchMode: a proof must never sit on a password prompt. ConnectTimeout: a dead host is a
  # failed release, not a hung one.
  SP_SSH_OPTS=(-p "$port" -o BatchMode=yes -o ConnectTimeout=10)
  return 0
}

# ship_proof_live <service> <image> <tag> — the assertion.
# 0 = the running container of <service> was created from an image labelled <tag>.
# 1 = it was not, or the server could not be asked; the reason goes to stderr, because the
#     caller prints its own refusal and this text is the evidence for it.
ship_proof_live() {
  local service=${1:?ship_proof_live needs a service}
  local image=${2:?ship_proof_live needs an image}
  local tag=${3:?ship_proof_live needs a tag}
  local fmt remote got rc=0 errf why

  if [ -z "${SP_DEST:-}" ]; then
    printf 'ship_proof: no server target — call ship_proof_target first\n' >&2
    return 1
  fi
  if ! command -v ssh >/dev/null 2>&1; then
    printf 'ship_proof: ssh is not in PATH — cannot ask %s what it runs\n' "$SP_DEST" >&2
    return 1
  fi

  fmt='{{index .Config.Labels "org.opencontainers.image.version"}}'
  # ONE round trip: the container id and its image label are answered together, so the
  # proof cannot interleave with a recreate that lands between two calls.
  remote=$(cat <<REMOTE
set -uo pipefail
cd '$SP_COMPOSE_DIR' || exit 3
c=\$(docker compose ps -q '$service' | head -n 1) || exit 4
[ -n "\$c" ] || exit 5
docker inspect --format '$fmt' "\$c" || exit 6
REMOTE
)

  errf=$(mktemp "${TMPDIR:-/tmp}/ship-proof.XXXXXX" 2>/dev/null) || errf=""
  got=$(ssh "${SP_SSH_OPTS[@]}" "$SP_DEST" "$remote" 2>"${errf:-/dev/null}") || rc=$?

  if [ "$rc" -ne 0 ]; then
    case $rc in
      3) why="cannot enter $SP_COMPOSE_DIR on $SP_DEST (is the compose directory right? REMOTE_COMPOSE_DIR)" ;;
      4) why="'docker compose ps' failed on $SP_DEST" ;;
      5) why="$SP_DEST has no running '$service' container (compose ps -q returned nothing)" ;;
      6) why="'docker inspect' failed on the '$service' container on $SP_DEST" ;;
      *) why="ssh to $SP_DEST failed (exit $rc) — no proof either way" ;;
    esac
    printf 'ship_proof: %s\n' "$why" >&2
    if [ -n "$errf" ] && [ -s "$errf" ]; then
      tail -n 3 "$errf" | sed 's/^/  | /' >&2
    fi
    [ -n "$errf" ] && rm -f "$errf"
    return 1
  fi
  [ -n "$errf" ] && rm -f "$errf"

  got=$(printf '%s' "$got" | cut -d' ' -f1)

  if [ "$got" != "$tag" ]; then
    if [ -n "$got" ]; then
      printf "ship_proof: %s runs '%s' from %s labelled version=%s — NOT %s\n" \
        "$SP_DEST" "$service" "$image" "$got" "$tag" >&2
    else
      printf "ship_proof: %s runs '%s' from an image with NO org.opencontainers.image.version label — cannot prove it is %s\n" \
        "$SP_DEST" "$service" "$tag" >&2
    fi
    return 1
  fi
  return 0
}
