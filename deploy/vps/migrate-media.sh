#!/usr/bin/env bash
# deploy/vps/migrate-media.sh — copy the media bucket off the old MinIO service
# onto the SeaweedFS one, and PROVE the objects arrived.
#
# WHY THIS EXISTS. MinIO's community images were withdrawn (see the header of
# deploy/vps/compose.prod.yml), so the production stack was moved to SeaweedFS —
# same S3 API, different on-disk format. SeaweedFS cannot read the `miniodata`
# volume, so "swap the image and keep the volume" silently serves an EMPTY media
# store: every uploaded image a form still references 404s. The objects have to
# be copied THROUGH THE S3 API, which means the old server must still be running
# when this script runs.
#
# ORDER MATTERS, AND IT IS WHY THIS IS A SCRIPT AND NOT A PARAGRAPH:
#
#   1. take a backup first (`/usr/local/bin/jomform-backup.sh`) — the media
#      archive is the fallback if anything below goes wrong;
#   2. add S3_ACCESS_KEY / S3_SECRET_KEY to the env file (the new compose fails
#      closed without them);
#   3. THIS SCRIPT — it starts the new `s3` service as an EXTRA container in the
#      live compose project (-p), copies every object, and verifies count AND
#      bytes on both sides. The live compose file is NOT touched, and the api
#      keeps serving media from the old store the whole time: nothing uploads into
#      a store this run has not finished filling;
#   4. only after it prints VERIFIED: install the new compose and let it recreate
#      the api (`bootstrap-host.sh --force --apply` does both), then
#      `docker compose up -d --remove-orphans` — the last one is what stops the
#      old minio container;
#   5. keep the `jomform_miniodata` volume for a while. Nothing deletes it; do
#      not delete it by hand until the media has been exercised in the browser.
#
# WHY -p. `docker compose` derives the project name from the compose FILE's
# directory, so the same file run from a checkout lands in a second project
# (`vps`) with its own network — a store the live stack cannot reach, and the copy
# dies on "cannot create to:". Pinning the live project makes the new container
# join the live stack's network, which is also what the api will use after step 4.
#
# WHAT IT WILL NOT DO. It does not stop the old store, does not touch the
# database, and does not delete the source objects — a one-way copy that can be
# re-run is the whole safety margin. It also refuses to call the run a success
# unless the two sides agree on object count and total bytes AND rclone's
# per-object check finds nothing missing.
#
# USAGE (on the prod host, from a checkout of this repo)
#   deploy/vps/migrate-media.sh --env-file /opt/jomform/.env            # plan
#   deploy/vps/migrate-media.sh --env-file /opt/jomform/.env --apply    # copy
#
#   --apply            actually start the s3 service and copy (default: plan only)
#   --env-file FILE    credentials for BOTH stores (default /opt/jomform/.env)
#   --project NAME     the LIVE compose project the new container joins
#                      (default jomform). `docker compose` would otherwise derive
#                      it from the compose file's directory and land in a project
#                      the running stack cannot reach.
#   --network NAME     the network both stores share (default <project>_internal)
#   --from URL         old store endpoint (default http://minio:9000)
#   --to URL           new store endpoint (default http://s3:8333)
#   --bucket NAME      bucket on both sides (default jomform-media)
#   --from-key K / --from-secret S   old credentials
#                      (defaults: key `jomform`, secret MINIO_ROOT_PASSWORD —
#                      the literal pair the old compose used)
#   --to-key K / --to-secret S       new credentials
#                      (defaults: S3_ACCESS_KEY / S3_SECRET_KEY from the env file)
#   --compose FILE     the new compose to start the s3 service from
#                      (default deploy/vps/compose.prod.yml, i.e. this repo's)
#   --rclone-image IMG default rclone/rclone:latest
#   --allow-empty      allow a source bucket that holds 0 objects
#
# Exit: 0 = copied and verified; 1 = refused, failed, or the sides disagree.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

APPLY=0
ALLOW_EMPTY=0
ENV_FILE="/opt/jomform/.env"
PROJECT="jomform"
NETWORK=""
FROM_URL="http://minio:9000"
TO_URL="http://s3:8333"
BUCKET="jomform-media"
FROM_KEY=""
FROM_SECRET=""
TO_KEY=""
TO_SECRET=""
COMPOSE="$REPO_ROOT/deploy/vps/compose.prod.yml"
RCLONE_IMAGE="rclone/rclone:latest"

while [ $# -gt 0 ]; do
  case "$1" in
    --apply)        APPLY=1; shift ;;
    --allow-empty)  ALLOW_EMPTY=1; shift ;;
    --env-file)     ENV_FILE="$2"; shift 2 ;;
    --project)      PROJECT="$2"; shift 2 ;;
    --network)      NETWORK="$2"; shift 2 ;;
    --from)         FROM_URL="$2"; shift 2 ;;
    --to)           TO_URL="$2"; shift 2 ;;
    --bucket)       BUCKET="$2"; shift 2 ;;
    --from-key)     FROM_KEY="$2"; shift 2 ;;
    --from-secret)  FROM_SECRET="$2"; shift 2 ;;
    --to-key)       TO_KEY="$2"; shift 2 ;;
    --to-secret)    TO_SECRET="$2"; shift 2 ;;
    --compose)      COMPOSE="$2"; shift 2 ;;
    --rclone-image) RCLONE_IMAGE="$2"; shift 2 ;;
    -h|--help)      sed -n '2,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "migrate-media: unknown argument: $1 (see --help)" >&2; exit 2 ;;
  esac
done

# The network both stores share defaults to the live project's, so --project
# alone cannot leave the new container on a network the api cannot reach.
[ -n "$NETWORK" ] || NETWORK="${PROJECT}_internal"

die() { echo "migrate-media: REFUSED — $*" >&2; exit 1; }
fail() { echo "migrate-media: FAILED — $*" >&2; exit 1; }
info() { echo "==> $*"; }

# env_get <KEY> — the value of KEY in the env file, or empty. Never printed by
# this function; the caller only ever passes it into a 0600 config file.
env_get() {
  local key="$1" line
  [ -r "$ENV_FILE" ] || return 0
  line="$(grep -E "^${key}=" "$ENV_FILE" | tail -n 1 || true)"
  [ -n "$line" ] || return 0
  printf '%s' "${line#*=}"
}

# ── 1. preconditions ─────────────────────────────────────────────────────────
command -v docker >/dev/null 2>&1 || die "docker is not on PATH"
[ -s "$COMPOSE" ] || die "no compose at $COMPOSE (the new stack is what defines the s3 service)"
[ -r "$ENV_FILE" ] || die "cannot read the env file '$ENV_FILE' (pass --env-file)"

if [ -z "$FROM_SECRET" ]; then
  FROM_SECRET="$(env_get MINIO_ROOT_PASSWORD)"
fi
[ -n "$FROM_KEY" ] || FROM_KEY="jomform"
[ -n "$TO_KEY" ] || TO_KEY="$(env_get S3_ACCESS_KEY)"
[ -n "$TO_SECRET" ] || TO_SECRET="$(env_get S3_SECRET_KEY)"

[ -n "$FROM_SECRET" ] || die "$ENV_FILE has no MINIO_ROOT_PASSWORD (nor S3_SECRET_KEY) — the OLD store's secret is what lets this read its objects; add it or pass --from-secret"
[ -n "$TO_KEY" ] || die "$ENV_FILE has no S3_ACCESS_KEY — the NEW store authenticates with S3_ACCESS_KEY/S3_SECRET_KEY (see deploy/vps/.env.prod.example); add them or pass --to-key"
[ -n "$TO_SECRET" ] || die "$ENV_FILE has no S3_SECRET_KEY — add it (openssl rand -hex 32) or pass --to-secret"

# ── 2. the rclone config: a 0600 temp file, never an argv ────────────────────
# Credentials on a command line are readable by every process on the host
# (`ps`), so they go in a file the container mounts read-only. The directory is
# 0700 under the caller's TMPDIR (a seam the test uses) and is removed by the
# trap below — including on the refusal paths.
CFG_DIR="$(mktemp -d "${TMPDIR:-/tmp}/jf-media.XXXXXX")"
chmod 700 "$CFG_DIR"
cleanup() { rm -rf "$CFG_DIR"; }
trap cleanup EXIT INT TERM
CFG="$CFG_DIR/rclone.conf"
umask 077
{
  printf '[from]\ntype = s3\nprovider = Other\nregion = us-east-1\nforce_path_style = true\n'
  printf 'access_key_id = %s\nsecret_access_key = %s\nendpoint = %s\n' "$FROM_KEY" "$FROM_SECRET" "$FROM_URL"
  printf '[to]\ntype = s3\nprovider = Other\nregion = us-east-1\nforce_path_style = true\n'
  printf 'access_key_id = %s\nsecret_access_key = %s\nendpoint = %s\n' "$TO_KEY" "$TO_SECRET" "$TO_URL"
} > "$CFG"
umask 022

# rclone_run <args...> — run rclone in the container, on the compose network so
# the service names resolve exactly as they do for the api.
rclone_run() {
  docker run --rm --network "$NETWORK" -v "$CFG_DIR:/cfg:ro" \
    "$RCLONE_IMAGE" --config /cfg/rclone.conf "$@"
}

echo "migrate-media — source=$FROM_URL  dest=$TO_URL  bucket=$BUCKET"
echo "               project=$PROJECT  network=$NETWORK"
echo "               compose=$COMPOSE  env=$ENV_FILE  mode=$([ "$APPLY" -eq 1 ] && echo APPLY || echo PLAN)"
echo

if [ "$APPLY" -ne 1 ]; then
  echo "PLAN — nothing has been copied. This would:"
  echo "  1. docker compose -p $PROJECT -f $COMPOSE up -d s3"
  echo "     (start the NEW store INSIDE the live project; the old one keeps serving)"
  echo "  2. rclone mkdir  to:$BUCKET                    (no-op when it exists)"
  echo "  3. rclone copy   from:$BUCKET → to:$BUCKET     (--checksum)"
  echo "  4. rclone size   on both sides; compare count AND bytes"
  echo "  5. rclone check  from:$BUCKET to:$BUCKET --size-only --one-way"
  echo
  echo "Re-run with --apply to do it. Take a backup first, and keep the api on the"
  echo "OLD store until this prints VERIFIED (install the new compose after)."
  exit 0
fi

# ── 3. start the new store ───────────────────────────────────────────────────
# -p is load-bearing: without it compose derives the project from $COMPOSE's
# directory, creates a SECOND network, and the copy cannot reach the new store.
info "starting the s3 service in project '$PROJECT' (docker compose -p $PROJECT -f $COMPOSE up -d s3)"
docker compose -p "$PROJECT" -f "$COMPOSE" --env-file "$ENV_FILE" up -d s3 \
  || fail "could not start the s3 service (is the env file missing S3_ACCESS_KEY/S3_SECRET_KEY?)"

# Wait for it to SERVE, not merely to exist: the healthcheck is an unsigned GET
# answered with the S3 error document. The try count is a seam so the offline
# harness does not spend two minutes proving that an unhealthy service stops the
# run; 60 × 2s is the real value.
HEALTH_TRIES="${MIGRATE_MEDIA_HEALTH_TRIES:-60}"
info "waiting for the s3 service to become healthy"
for _ in $(seq 1 "$HEALTH_TRIES"); do
  state="$(docker compose -p "$PROJECT" -f "$COMPOSE" --env-file "$ENV_FILE" ps --format '{{.Health}}' s3 2>/dev/null | head -n 1 || true)"
  [ "$state" = "healthy" ] && break
  sleep 2
done
if [ "${state:-}" != "healthy" ]; then
  docker compose -p "$PROJECT" -f "$COMPOSE" --env-file "$ENV_FILE" logs --tail 40 s3 >&2 || true
  fail "the s3 service never reported healthy (last state: '${state:-unknown}')"
fi

# ── 4. read both sides before copying ────────────────────────────────────────
# A failed read is a refusal, never a zero: "could not list" and "holds nothing"
# must not look the same, or an empty result would be counted as a migration.
info "reading the source bucket"
SRC_BEFORE="$(rclone_run size "from:$BUCKET" --json)" || fail "cannot read from:$BUCKET — wrong endpoint/credentials, or the old store is not running (is its container still up?)"
SRC_COUNT="$(printf '%s' "$SRC_BEFORE" | sed -n 's/.*"count":\([0-9]*\).*/\1/p')"
SRC_BYTES="$(printf '%s' "$SRC_BEFORE" | sed -n 's/.*"bytes":\([0-9]*\).*/\1/p')"
[ -n "$SRC_COUNT" ] && [ -n "$SRC_BYTES" ] || fail "could not parse the source size: $SRC_BEFORE"
if [ "$SRC_COUNT" -eq 0 ] && [ "$ALLOW_EMPTY" -ne 1 ]; then
  die "from:$BUCKET holds 0 objects. If that is genuinely the state of the old store, re-run with --allow-empty; otherwise this is a read of the wrong bucket."
fi
echo "    source: $SRC_COUNT object(s), $SRC_BYTES byte(s)"

info "ensuring the destination bucket exists"
rclone_run mkdir "to:$BUCKET" \
  || fail "cannot create to:$BUCKET — wrong endpoint/credentials on the new store, or its container is not on '$NETWORK' (check --project/--network; the service must be the one the api will use)"

DEST_BEFORE="$(rclone_run size "to:$BUCKET" --json)" || fail "cannot read to:$BUCKET"
DEST_COUNT="$(printf '%s' "$DEST_BEFORE" | sed -n 's/.*"count":\([0-9]*\).*/\1/p')"
DEST_BYTES="$(printf '%s' "$DEST_BEFORE" | sed -n 's/.*"bytes":\([0-9]*\).*/\1/p')"
echo "    dest:   ${DEST_COUNT:-?} object(s), ${DEST_BYTES:-?} byte(s)"
# A resume is fine (dest ⊆ source). dest holding MORE objects than the source
# means this is not the same data set, and "copy then compare" would either
# fail confusingly or (worse) be read as success.
if [ -n "$DEST_COUNT" ] && [ "$DEST_COUNT" -gt "$SRC_COUNT" ]; then
  die "to:$BUCKET already holds $DEST_COUNT objects, more than the source's $SRC_COUNT — that is not a resumable copy of this bucket. Point --to/--bucket at the right store."
fi

# ── 5. copy ──────────────────────────────────────────────────────────────────
info "copying from:$BUCKET → to:$BUCKET"
rclone_run copy "from:$BUCKET" "to:$BUCKET" --checksum --transfers 4 --stats 15s --stats-one-line \
  || fail "rclone copy returned non-zero — nothing below this line was verified"

# ── 6. verify: count, bytes, and every object name ───────────────────────────
info "verifying"
SRC_AFTER="$(rclone_run size "from:$BUCKET" --json)" || fail "cannot re-read the source"
DST_AFTER="$(rclone_run size "to:$BUCKET" --json)" || fail "cannot read the destination"
S_N="$(printf '%s' "$SRC_AFTER" | sed -n 's/.*"count":\([0-9]*\).*/\1/p')"
S_B="$(printf '%s' "$SRC_AFTER" | sed -n 's/.*"bytes":\([0-9]*\).*/\1/p')"
D_N="$(printf '%s' "$DST_AFTER" | sed -n 's/.*"count":\([0-9]*\).*/\1/p')"
D_B="$(printf '%s' "$DST_AFTER" | sed -n 's/.*"bytes":\([0-9]*\).*/\1/p')"
echo "    source:      ${S_N:-?} object(s), ${S_B:-?} byte(s)"
echo "    destination: ${D_N:-?} object(s), ${D_B:-?} byte(s)"
[ "${S_N:-x}" = "${D_N:-y}" ] || fail "object COUNT differs (source ${S_N:-?}, destination ${D_N:-?}) — the copy is incomplete"
[ "${S_B:-x}" = "${D_B:-y}" ] || fail "BYTE total differs (source ${S_B:-?}, destination ${D_B:-?}) — the copy is incomplete"

# Per-object check. --size-only is deliberate: it is the comparison every S3
# implementation can answer from its own listing, with no dependency on an ETag
# digest scheme (a multipart upload's ETag is a composite, not the object's MD5,
# so a hash comparison is not portable across implementations without reading the
# objects back). The count AND byte-total equality above already pins the totals,
# so this check is about every source NAME being present on the destination.
rclone_run check "from:$BUCKET" "to:$BUCKET" --size-only --one-way \
  || fail "rclone check found objects missing from the destination — the copy is incomplete"

cat <<EOF

migrate-media: VERIFIED — $S_N object(s) / $S_B byte(s) on both sides, and every
source object is present on the destination.

The api is STILL on the old store. NEXT, IN THIS ORDER (this script does not do
them):

  1. install the new compose and let it recreate the api against the store this
     run just filled:
       deploy/host/bootstrap-host.sh --env-file $ENV_FILE --force --apply
     (it also runs "compose up -d", i.e. the s3 service becomes a real project
      service instead of the extra container started here)
  2. curl -fsS https://<your host>/healthz      (api healthy, S3 reachable)
  3. docker compose -p $PROJECT -f /opt/jomform/compose.yml --env-file $ENV_FILE up -d --remove-orphans
     ^ this is what STOPS the old store; until it runs, both are up
  4. load a form that already had an uploaded image: the OLD objects are the ones
     that prove the copy, so check a pre-existing image, not just a fresh upload
  5. keep the jomform_miniodata volume. Nothing deletes it; do not delete it by
     hand until the media has been exercised, and keep a backup until then too.
EOF
