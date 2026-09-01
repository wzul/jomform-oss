#!/usr/bin/env bash
#
# build-and-ship.sh — guarded, single-writer build + ship for JomForm images.
#
# Solves the issue where two agents built images from the same working tree
# at the same time and prod ended up with two different "v1.0.0" image IDs,
# one of them pre-dating the release code.
#
# Guarantees:
#   * refuses to build from a dirty working tree (git status --porcelain)
#   * flock on /tmp/jomform-build.lock so concurrent builds cannot interleave
#   * stamps VERSION + COMMIT (git describe / rev-parse) as build args and
#     OCI labels, so prod startup logs and `docker image inspect` prove
#     exactly what is running
#
# Deploy order (do it in this sequence, every time):
#   0. Merge all PRs; create the git tag + GitHub release FIRST
#      (git tag vX.Y.Z && git push origin vX.Y.Z && gh release create vX.Y.Z)
#   1. ./deploy/build-and-ship.sh                       # on a clean tree
#   2. scp the tarball to the server (script does this when SSH_HOST is set)
#   3. on the server:  docker load -i <tarball>
#   4. on the server:  docker tag jomform-api:X.Y.Z jomform-api:latest
#                      # compose runs :latest — a stale :latest keeps the
#                      # OLD image even after `restart`
#   5. on the server:  docker compose up -d --force-recreate api
#                      # plain `restart` will NOT pick up the new image
#   6. verify: docker compose logs api | head   → startup line shows
#      version=... commit=... matching the tag; then curl the health route
#
# Usage:
#   ./deploy/build-and-ship.sh [VERSION]
#
# Overridable via environment:
#   IMAGE       image name            (default: jomform-api)
#   CONTEXT     build context dir     (default: api)
#   DOCKERFILE  Dockerfile path       (default: api/Dockerfile)
#   OUT_DIR     local tarball output  (default: /tmp — keep OUTSIDE the repo
#                                     so the clean-tree guard stays valid)
#   SSH_HOST    set to enable scp of the tarball (e.g. 203.0.113.10)
#   SSH_USER    (default: root)   SSH_PORT (default: 22)   SSH_PATH (default: /tmp)
#
# Example — build the web image instead:
#   IMAGE=jomform-web CONTEXT=web DOCKERFILE=web/Dockerfile ./deploy/build-and-ship.sh

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." rev-parse --show-toplevel)"
cd "$REPO_ROOT"

IMAGE="${IMAGE:-jomform-api}"
CONTEXT="${CONTEXT:-api}"
DOCKERFILE="${DOCKERFILE:-api/Dockerfile}"
OUT_DIR="${OUT_DIR:-/tmp}"
SSH_HOST="${SSH_HOST:-}"
SSH_USER="${SSH_USER:-root}"
SSH_PORT="${SSH_PORT:-22}"
SSH_PATH="${SSH_PATH:-/tmp}"
LOCK_FILE="${LOCK_FILE:-/tmp/jomform-build.lock}"

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

usage() { sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  "") ;;
  *) VERSION_OVERRIDE="$1" ;;
esac

# ---------------------------------------------------------------------------
# 1. Single-writer lock: fail fast if another build is already running.
# ---------------------------------------------------------------------------
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  die "another build holds $LOCK_FILE — wait for it, don't interleave"
fi
info "lock acquired: $LOCK_FILE (pid $$)"

# ---------------------------------------------------------------------------
# 2. Clean-tree guard: no builds from dirty or mid-edit working trees.
# ---------------------------------------------------------------------------
if [ -n "$(git status --porcelain)" ]; then
  git status --porcelain >&2 || true
  die "working tree is dirty — commit or stash before building"
fi

# ---------------------------------------------------------------------------
# 3. Resolve provenance stamps from git (tag first, then build).
# ---------------------------------------------------------------------------
VERSION="${VERSION_OVERRIDE:-$(git describe --tags --always)}"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
info "stamps: version=$VERSION commit=$COMMIT date=$BUILD_DATE"

# ---------------------------------------------------------------------------
# 4. Preflight.
# ---------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || die "docker not found in PATH"
[ -f "$DOCKERFILE" ] || die "$DOCKERFILE not found — the api image Dockerfile lives on the Dockerfile branch (issue #25); merge/checkout it first"
mkdir -p "$OUT_DIR"

# ---------------------------------------------------------------------------
# 5. Build with provenance stamps (build args + OCI labels).
#    api/Dockerfile should declare ARG VERSION / COMMIT / BUILD_DATE and wire
#    them into the go build -ldflags -X version.* values; until it does, the
#    labels below still tie the image to the exact commit.
# ---------------------------------------------------------------------------
info "building $IMAGE:$VERSION from $DOCKERFILE (context: $CONTEXT)"
docker build \
  -f "$DOCKERFILE" \
  "$CONTEXT" \
  --build-arg "VERSION=$VERSION" \
  --build-arg "COMMIT=$COMMIT" \
  --build-arg "BUILD_DATE=$BUILD_DATE" \
  --label "org.opencontainers.image.title=$IMAGE" \
  --label "org.opencontainers.image.version=$VERSION" \
  --label "org.opencontainers.image.revision=$COMMIT" \
  --label "org.opencontainers.image.created=$BUILD_DATE" \
  --tag "$IMAGE:$VERSION"

IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$IMAGE:$VERSION")"
info "built $IMAGE:$VERSION ($IMAGE_ID)"

# ---------------------------------------------------------------------------
# 6. Save as a gzip tarball (server-to-server pattern: no registry needed).
# ---------------------------------------------------------------------------
TARBALL="$OUT_DIR/$IMAGE-$VERSION.tar.gz"
info "saving image → $TARBALL"
docker save "$IMAGE:$VERSION" | gzip > "$TARBALL"

# ---------------------------------------------------------------------------
# 7. Ship (optional): scp to the server when SSH_HOST is set.
# ---------------------------------------------------------------------------
if [ -n "$SSH_HOST" ]; then
  info "shipping $TARBALL → $SSH_USER@$SSH_HOST:$SSH_PATH/ (port $SSH_PORT)"
  scp -P "$SSH_PORT" "$TARBALL" "$SSH_USER@$SSH_HOST:$SSH_PATH/"
else
  info "SSH_HOST not set — skipping scp (copy $TARBALL manually)"
fi

# ---------------------------------------------------------------------------
# 8. Print the remaining server-side steps (never executed from here).
# ---------------------------------------------------------------------------
REMOTE_TARBALL="$SSH_PATH/$(basename "$TARBALL")"
cat <<EOF

Remaining steps ON THE SERVER (run manually):
  docker load -i $REMOTE_TARBALL
  docker tag $IMAGE:$VERSION $IMAGE:latest
  docker compose up -d --force-recreate api
  docker compose logs api | head        # must show version=$VERSION commit=$COMMIT
EOF
info "done"