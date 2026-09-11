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
#   * runs the LOCAL build+test gate (AGENTS.md: CI is not the merge gate)
#     BEFORE building the image:
#         cd api && go build ./...
#         cd api && go test  ./...
#         cd web && npm run build
#     A failing gate aborts with a non-zero exit before any image exists, so a
#     broken build cannot ship. (issue #476: the 000047 migration crash-looped
#     prod; a single `go test ./...` before the image build would have caught it)
#   * stamps VERSION + COMMIT (git describe / rev-parse) as build args and
#     OCI labels, so prod startup logs and `docker image inspect` prove
#     exactly what is running
#
# Deploy order (do it in this sequence, every time):
#   0. Merge all PRs; create the git tag + GitHub release FIRST
#      (git tag vX.Y.Z && git push origin vX.Y.Z && gh release create vX.Y.Z)
#   1. ./deploy/build-and-ship.sh                       # on a clean tree;
#                                                       # build+test gate runs first
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
#   ./deploy/build-and-ship.sh [--skip-tests] [VERSION]
#
#   --skip-tests  DANGER: bypass the pre-build build+test gate and print a loud
#                 warning. Only for a genuine emergency (e.g. an urgent hotfix
#                 while the local test Postgres/Valkey is down) — never for a
#                 normal release. The gate is what keeps a broken build from
#                 crash-looping prod, so re-run WITHOUT this flag as soon as the
#                 emergency is over.
#
# The gate inherits your environment: export JOMFORM_TEST_DATABASE_URL /
# JOMFORM_TEST_VALKEY_URL so the DB-backed tests actually run (without them the
# suite skips those tests, as it does on a bare checkout).
#
# Overridable via environment:
#   SKIP_TESTS  1 = same as --skip-tests  (default: 0)
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
SKIP_TESTS="${SKIP_TESTS:-0}"

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# usage() prints this script's own header comment (from line 2 down to the
# `set -euo pipefail` line) with the leading "# " stripped.
usage() { sed -n '2,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }

VERSION_OVERRIDE=""
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)   usage; exit 0 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    --)          shift; break ;;
    -*)          die "unknown flag: $1 (see --help)" ;;
    *)           [ -z "$VERSION_OVERRIDE" ] || die "unexpected extra argument: $1 (see --help)"
                 VERSION_OVERRIDE="$1" ;;
  esac
  shift
done

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
# 5. Pre-build gate: local build + test trio (AGENTS.md rule — CI is NOT the
#    merge gate for this repo). Nothing is built or shipped unless all three
#    are green. Runs on the repo root (cd "$REPO_ROOT" above); api/ and web/
#    are the source dirs the images are built from.
#
#    Escape hatch: --skip-tests (or SKIP_TESTS=1) for genuine emergencies only.
# ---------------------------------------------------------------------------
run_gate_step() {
  local label="$1"; shift
  info "gate: $label"
  if ! "$@"; then
    die "pre-build gate FAILED: $label — fix it before building/shipping (or use --skip-tests for a genuine emergency)"
  fi
}

if [ "$SKIP_TESTS" = "1" ]; then
  cat >&2 <<'EOF'

  ============================================================
   !!!  WARNING: --skip-tests — PRE-BUILD GATE BYPASSED  !!!
  ============================================================
   go build / go test / npm run build were NOT run.
   A broken build can ship and crash-loop prod
   (see the 000047 migration incident).
   Re-run WITHOUT this flag as soon as the emergency is over.
  ============================================================

EOF
else
  run_gate_step "cd api && go build ./..." \
    bash -c 'cd api && go build ./...'
  run_gate_step "cd api && go test ./..." \
    bash -c 'cd api && go test ./...'
  run_gate_step "cd web && npm run build" \
    bash -c 'cd web && npm run build'
  info "pre-build gate green: go build + go test + npm run build"
fi

# ---------------------------------------------------------------------------
# 6. Build with provenance stamps (build args + OCI labels).
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
# 7. Save as a gzip tarball (server-to-server pattern: no registry needed).
# ---------------------------------------------------------------------------
TARBALL="$OUT_DIR/$IMAGE-$VERSION.tar.gz"
info "saving image → $TARBALL"
docker save "$IMAGE:$VERSION" | gzip > "$TARBALL"

# ---------------------------------------------------------------------------
# 8. Ship (optional): scp to the server when SSH_HOST is set.
# ---------------------------------------------------------------------------
if [ -n "$SSH_HOST" ]; then
  info "shipping $TARBALL → $SSH_USER@$SSH_HOST:$SSH_PATH/ (port $SSH_PORT)"
  scp -P "$SSH_PORT" "$TARBALL" "$SSH_USER@$SSH_HOST:$SSH_PATH/"
else
  info "SSH_HOST not set — skipping scp (copy $TARBALL manually)"
fi

# ---------------------------------------------------------------------------
# 9. Print the remaining server-side steps (never executed from here).
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