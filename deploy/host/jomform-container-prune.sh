#!/usr/bin/env bash
#
# JomForm production container GC (host-side).
# Installed on the production host at /usr/local/bin/jomform-container-prune.sh.
# Reclaims STOPPED containers that can no longer be needed on 203.0.113.10.
#
# WHY THIS FILE EXISTS
#   Prod accumulated 149 Exited + 3 Created anonymous one-shot `docker run` containers
#   between 2026-09-19 and 2026-10-12 (kanban card t_2646fc00). They are deploy/QA
#   inspection probes run over ssh: auto-named (`adoring_beaver`), command like
#   `sh -c 'find / -maxdepth ...'`, `cat /app/.next/BUILD_ID`, `strings /usr/local/bin/...`.
#   They are never invoked with --rm, so each one stays in `docker ps -a` forever.
#
# WHY THE HERMES kanban-container-gc PLUGIN CANNOT COVER THIS (card t_2646fc00 item 1)
#   That plugin only ever sees the host it runs on: it calls the LOCAL docker CLI
#   (`shutil.which("docker")`, no DOCKER_HOST, no ssh transport) and reads local /proc and
#   the local kanban board DBs. It also only matches card-stack name shapes
#   (`jf-*/mrg*/mg*/qa*` + `-pg|-vk|-minio`). Prod is a different host AND a different leak
#   class: anonymous probe containers that no card can be attributed to. Hence a host-local,
#   age-based prune on prod -- not a plugin extension.
#
# SAFETY INVARIANTS -- every one is enforced below, none is optional:
#   * NEVER removes a running / paused / restarting container (state gate, re-checked
#     against `docker ps` immediately before removal).
#   * NEVER removes an IMAGE (docker rm cannot), and NEVER removes a NAMED volume:
#     `docker rm -v` drops only the removed container's own anonymous volumes, and only
#     when no other container references them. Rollback lives in image tags, not containers.
#   * NEVER removes a container carrying a `com.docker.compose.project` label: the six live
#     jomform/traefik containers are all compose-labelled, and they are in KEEP_NAMES too.
#   * NEVER removes a container younger than JF_PRUNE_MIN_AGE_H hours (default 168 = 7 days).
#     A young stopped container may belong to an in-flight deploy or QA probe.
#   * FAILS CLOSED: if docker is unreachable, or the container list cannot be read, or the
#     list is empty, it LOGS and exits non-zero having removed nothing. It never guesses.
#
# Usage / overrides:
#   JF_PRUNE_MIN_AGE_H=168   age floor in hours (default 168)
#   JF_PRUNE_DRY_RUN=1       list what would go, remove nothing
#   /etc/jomform/keep-containers   optional, one container name per line, always kept
# Exit codes: 0 = ran (removed N >= 0), 1 = fail-closed / bad usage.

set -uo pipefail

MIN_AGE_H="${JF_PRUNE_MIN_AGE_H:-168}"
DRY_RUN="${JF_PRUNE_DRY_RUN:-0}"
KEEP_FILE="${JF_PRUNE_KEEP_FILE:-/etc/jomform/keep-containers}"
MAX_LOG_LIST=20

log() { printf '%s jomform-container-prune: %s\n' "$(date -Is)" "$*"; }

case "$MIN_AGE_H" in
  ''|*[!0-9]*) log "FATAL: JF_PRUNE_MIN_AGE_H must be a non-negative integer (got '$MIN_AGE_H')"; exit 1;;
esac

# The live stack. Belt and braces with the compose-label gate below.
KEEP_NAMES=" jomform-api-1 jomform-web-1 jomform-postgres-1 jomform-valkey-1 jomform-minio-1 traefik-traefik-1 "
if [ -r "$KEEP_FILE" ]; then
  while read -r extra; do
    case "$extra" in ''|'#'*) continue;; esac
    KEEP_NAMES="$KEEP_NAMES $extra "
  done < "$KEEP_FILE"
fi

command -v docker >/dev/null 2>&1 || { log "FATAL: docker not found on PATH"; exit 1; }
docker info >/dev/null 2>&1 || { log "FATAL: docker daemon unreachable - fail closed, nothing removed"; exit 1; }

# --- read state (one call each) -------------------------------------------
if ! raw="$(docker ps -a --no-trunc --format '{{.ID}}|{{.Names}}|{{.State}}|{{.CreatedAt}}')"; then
  log "FATAL: docker ps -a failed - fail closed, nothing removed"; exit 1
fi
if [ -z "$raw" ]; then
  log "FATAL: container list is EMPTY (the stack is always up) - fail closed, nothing removed"; exit 1
fi

# id -> compose project, so the compose gate costs one call, not one per container.
compose_map="$(docker ps -a --no-trunc --format '{{.ID}}|{{.Label "com.docker.compose.project"}}')" || {
  log "FATAL: could not read compose labels - fail closed, nothing removed"; exit 1
}

cutoff=$(( $(date +%s) - MIN_AGE_H * 3600 ))
inspected=0
cand_ids=()
cand_names=()
skipped_young=0
skipped_keep=0
skipped_compose=0

while IFS='|' read -r id name state created; do
  [ -n "${id:-}" ] || continue
  [ -n "${name:-}" ] || continue
  inspected=$(( inspected + 1 ))

  case "$state" in
    exited|created|dead) ;;
    *) continue ;;
  esac

  case "$KEEP_NAMES" in
    *" $name "*) log "keep (name): $name"; skipped_keep=$(( skipped_keep + 1 )); continue ;;
  esac

  epoch="$(date -d "${created:0:25}" +%s 2>/dev/null)"
  if [ -z "$epoch" ]; then
    log "skip (unparsable CreatedAt '$created'): $name"; continue
  fi
  if [ "$epoch" -ge "$cutoff" ]; then
    skipped_young=$(( skipped_young + 1 )); continue
  fi

  proj="$(printf '%s\n' "$compose_map" | awk -F'|' -v want="$id" '$1==want {print $2; exit}')"
  if [ -n "$proj" ]; then
    log "keep (compose project=$proj): $name"; skipped_compose=$(( skipped_compose + 1 )); continue
  fi

  cand_ids+=( "$id" )
  cand_names+=( "$name" )
done <<< "$raw"

n=${#cand_ids[@]}
log "inspected=$inspected candidates=$n (age floor ${MIN_AGE_H}h; skipped young=$skipped_young keep=$skipped_keep compose=$skipped_compose)"

if [ "$n" -eq 0 ]; then
  log "nothing to remove"
  exit 0
fi

if [ "${#cand_names[@]}" -le "$MAX_LOG_LIST" ]; then
  log "candidates: ${cand_names[*]}"
else
  log "candidates (first ${MAX_LOG_LIST} of ${n}): ${cand_names[*]:0:$MAX_LOG_LIST}"
fi

if [ "$DRY_RUN" = "1" ]; then
  log "DRY RUN - nothing removed"
  exit 0
fi

# --- re-verify no candidate is running right now ---------------------------
running="$(docker ps --no-trunc --format '{{.ID}}')" || {
  log "FATAL: could not re-read running containers - fail closed, nothing removed"; exit 1
}
rm_ids=()
for i in "${cand_ids[@]}"; do
  if printf '%s\n' "$running" | awk -v want="$i" '$1==want {found=1} END {exit !found}'; then
    log "ABORT candidate $i: it is running now - skipping it"
    continue
  fi
  rm_ids+=( "$i" )
done
if [ "${#rm_ids[@]}" -eq 0 ]; then
  log "nothing to remove after the running re-check"
  exit 0
fi

# --- remove ----------------------------------------------------------------
before_count="$(docker ps -a -q | wc -l)"
if docker rm -v -f "${rm_ids[@]}" >/tmp/jomform-container-prune.rm.out 2>&1; then
  :
else
  log "WARN docker rm reported an error (see /tmp/jomform-container-prune.rm.out):"
  while read -r line; do log "  rm: $line"; done < /tmp/jomform-container-prune.rm.out
fi
after_count="$(docker ps -a -q | wc -l)"
removed=$(( before_count - after_count ))

# Verify by counting, never by the rm exit status alone.
state_after="$(docker ps -a --format '{{.State}}' | sort | uniq -c | tr '\n' ' ')"
log "removed=$removed containers (${before_count} -> ${after_count}); states now: ${state_after}"
exit 0
