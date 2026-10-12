#!/usr/bin/env bash
# deploy/vps/drift-check.sh — is the host running what the repo says it runs?
#
# WHY THIS EXISTS. The production stack is one VPS with docker compose, and its
# two load-bearing files — `/opt/jomform/compose.yml` and
# `/opt/traefik/dynamic/jomform.yml` — were HAND-MAINTAINED COPIES of the files in
# this directory. Measured 2026-10-12, that claim was already false:
#
#   file                     repo     live    what the drift would have cost
#   compose.prod.yml         n/a      130 ln  (the repo had NO prod compose at all)
#   traefik-jomform.yml       55 ln   179 ln  a different DIALECT: the repo file
#                                            was v2 (`Host(...)`, `certResolver:
#                                            le`, `entryPoints: [websecure]`)
#                                            while the live file is v3
#                                            (`HostRegexp`, `letsencrypt`, block
#                                            sequences). Installing the repo file
#                                            on a fresh host would have produced
#                                            routing that does not match prod.
#   docker-compose.yml       (dev shape, POSTGRES_PASSWORD default `jomform_dev`,
#                            pgdata mounted at /var/lib/postgresql/data while the
#                            pinned postgres:18 image puts its data dir under
#                            /var/lib/postgresql — i.e. a fresh host started from
#                            the repo compose would have come up with an EMPTY
#                            database, or a dev password)
#
# So the repo now holds the PROD files verbatim (byte-identical, sha in the
# commit) and this script is how that claim is CHECKED rather than believed. It
# is deliberately dumb: it diffs, it prints the diff, and it exits non-zero on
# any difference. A drift check that can pass on the absence of a signal — a
# missing file read as "nothing to compare" — would be worse than none, so a
# missing or unreadable live file is a FAILURE here, never an OK.
#
# USAGE
#   deploy/vps/drift-check.sh --host root@203.0.113.10        # over ssh (default paths)
#   deploy/vps/drift-check.sh --compose /tmp/c.yml --traefik /tmp/t.yml \
#                             --traefik-compose /tmp/tc.yml    # local files (the test)
#
# Exit: 0 = every pair identical; 1 = drift or an unreadable input.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VPS_DIR="$REPO_ROOT/deploy/vps"

HOST=""
COMPOSE_PATH="/opt/jomform/compose.yml"
TRAEFIK_PATH="/opt/traefik/dynamic/jomform.yml"
TRAEFIK_COMPOSE_PATH="/opt/traefik/docker-compose.yml"

COMPOSE_FILE="$VPS_DIR/compose.prod.yml"
TRAEFIK_FILE="$VPS_DIR/traefik-jomform.yml"
TRAEFIK_COMPOSE_FILE="$VPS_DIR/traefik-docker-compose.yml"

while [ $# -gt 0 ]; do
  case "$1" in
    --host)              HOST="$2"; shift 2 ;;
    --compose)           COMPOSE_PATH="$2"; shift 2 ;;
    --traefik)           TRAEFIK_PATH="$2"; shift 2 ;;
    --traefik-compose)   TRAEFIK_COMPOSE_PATH="$2"; shift 2 ;;
    -h|--help)           sed -n '2,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \?//'; exit 0 ;;
    *) echo "drift-check: unknown argument: $1" >&2; exit 2 ;;
  esac
done

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

# Fetch the live files. Over ssh we CAT into the work dir (so both modes compare
# plain files with the same code below — one comparison path, not two).
fetch() {
  local remote="$1" local_path="$2" label="$3"
  if [ -n "$HOST" ]; then
    if ! timeout 60 ssh -o BatchMode=yes -o ConnectTimeout=15 "$HOST" "cat '$remote'" > "$local_path" 2>"$WORK/err"; then
      echo "drift-check: FAIL  cannot read $label from $HOST:$remote" >&2
      sed 's/^/    /' "$WORK/err" >&2
      return 1
    fi
  else
    if [ ! -r "$remote" ]; then
      echo "drift-check: FAIL  cannot read $label at $remote" >&2
      return 1
    fi
    cp "$remote" "$local_path"
  fi
  # An empty read is not evidence of agreement — it is evidence of a broken read.
  if [ ! -s "$local_path" ]; then
    echo "drift-check: FAIL  $label read as EMPTY from ${HOST:+$HOST:}$remote — refusing to call that identical" >&2
    return 1
  fi
}

RC=0
check_pair() {
  local live_file="$1" repo_file="$2" label="$3"
  local live="$WORK/live.$label"

  if ! fetch "$live_file" "$live" "$label"; then
    RC=1
    return
  fi
  if [ ! -r "$repo_file" ]; then
    echo "drift-check: FAIL  the repo has no $repo_file to compare against" >&2
    RC=1
    return
  fi
  if diff -q "$repo_file" "$live" >/dev/null 2>&1; then
    echo "drift-check: OK    $label — $(basename "$repo_file") is byte-identical to ${HOST:+$HOST:}$live_file"
    return
  fi
  echo "drift-check: DRIFT $label — $(basename "$repo_file") does NOT match ${HOST:+$HOST:}$live_file" >&2
  echo "                   repo: $repo_file" >&2
  # `awk` rather than `diff | sed | head`: under `set -o pipefail` a reader that
  # exits early sends SIGPIPE to the writer and the pipeline fails, which used to
  # abort the script mid-report and HIDE the remaining pairs' verdicts (measured:
  # a drifted compose silenced the traefik pair's OK line).
  diff -u "$repo_file" "$live" > "$WORK/diff.txt" 2>&1 || true
  awk 'NR<=80 { print "    " $0 }' "$WORK/diff.txt" >&2 || true
  echo "                   (< = in the repo, > = live)" >&2
  RC=1
}

check_pair "$COMPOSE_PATH"         "$COMPOSE_FILE"         compose
check_pair "$TRAEFIK_PATH"         "$TRAEFIK_FILE"         traefik
check_pair "$TRAEFIK_COMPOSE_PATH" "$TRAEFIK_COMPOSE_FILE" traefik-compose

if [ "$RC" -eq 0 ]; then
  echo "drift-check: no drift — the repo's vps files are what this host runs."
else
  echo "drift-check: DRIFT FOUND — the repo and the host disagree (see above)." >&2
  echo "             Fix by installing the repo's file on the host (or, if the host" >&2
  echo "             change was deliberate, updating the repo file in a PR so both" >&2
  echo "             agree again). Do NOT 'fix' the repo file by copying the host's" >&2
  echo "             without a PR: that is how the drift happened." >&2
fi
exit "$RC"
