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
#   * runs the LOCAL PREFLIGHT gate (scripts/preflight.sh) BEFORE building the
#     image. There is no CI to fall back on — GitHub Actions is not a gate for
#     this repo — so the preflight IS the gate.
#     THE GATE LIST IS DERIVED, NOT WRITTEN DOWN HERE. The authoritative
#     inventory is `scripts/preflight.sh --list`, and this script derives both
#     the label it prints and the --skip-tests warning from it, so neither can
#     drift. Do NOT hand-write the list back in: an earlier version of this
#     header named the CI-era gates only, while the preflight had also grown
#     self-tests for the uptime watchdog and the temp-API-key helper — gates
#     that can stop a release and were invisible to anyone reading the header.
#     Run `./scripts/preflight.sh --list` for the current set.
#     A failing gate aborts with a non-zero exit before any image exists, so a
#     broken or vulnerable build cannot ship. (issue #476: the 000047 migration
#     crash-looped prod; a single `go test ./...` before the image build would
#     have caught it)
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
#   5. on the server:  docker compose up -d --force-recreate <service>
#                      (the service matching IMAGE - see the NOTE printed at the end)
#                      # plain `restart` will NOT pick up the new image
#   6. verify: docker compose logs <service> | head   → startup line shows
#      version=... commit=... matching the tag; then curl the health route
#
# Usage:
#   ./deploy/build-and-ship.sh [--skip-tests] [VERSION]
#
#   --skip-tests  DANGER: bypass the pre-build preflight (scripts/preflight.sh,
#                 every gate it lists) and print a loud warning naming them.
#                 Only for a genuine emergency (e.g. an urgent hotfix while the
#                 local test Postgres/Valkey is down) — never for a normal
#                 release.
#                 The gate is what keeps a broken build from crash-looping prod,
#                 so re-run WITHOUT this flag as soon as the emergency is over.
#
#   Before the gate takes a slot in the host gate pool it consults the per-tree
#   result record (scripts/preflight_reuse.sh). A tree whose verdict is ALREADY
#   recorded green is not re-gated on these bytes, and the reuse is printed
#   loudly (which gates were remembered, and that none of them was re-run).
#   There is deliberately NO flag for this: only a record that already covers
#   THESE bytes under THIS environment may narrow the run, and a partial or
#   absent record still gates in full. See docs/PREFLIGHT-REUSE.md.
#   Override the record store with PREFLIGHT_RESULT_DIR.
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
# Example — build the web image instead (context is the repo ROOT: the web
# unit tests, which `npm run build` runs first, read api/ sources — see
# web/Dockerfile and deploy/DEPLOY.md):
#   IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile ./deploy/build-and-ship.sh

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." rev-parse --show-toplevel)"
cd "$REPO_ROOT"

IMAGE="${IMAGE:-jomform-api}"
# Issue #576: the server-side steps are printed for a human to run, and they
# used to hardcode the api service. Building the web image therefore printed
# "docker compose up -d --force-recreate api" - following it reloads the API,
# leaves the new web image unloaded, and the failure is invisible: the API comes
# back healthy and prints the expected version line while the web container keeps
# serving the old build (v1.5.12 shipped this way and /docs/subscriptions 404'd).
# Derive the service from the image (jomform-api -> api, jomform-web -> web); set
# SERVICE explicitly if the compose service is ever renamed.
SERVICE="${SERVICE:-${IMAGE##*-}}"
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

# The gate inventory is READ FROM THE RUNNER, never copied into this file (the
# label and the --skip-tests warning below are the two places a hand-written
# list used to live, and both had gone stale: they named the CI-era gates while
# the preflight had also grown self-tests that can fail a release). Cosmetic
# only — the gate itself is the `preflight.sh` call — so a failure here degrades
# the wording instead of aborting the build, and the explicit `|| true` keeps
# `set -e` from turning that read into a silent exit.
GATE_INVENTORY=""
if [ -x "$REPO_ROOT/scripts/preflight.sh" ]; then
  GATE_INVENTORY="$(bash "$REPO_ROOT/scripts/preflight.sh" --list 2>/dev/null \
    | tr '\n' '/' | sed 's:/$::' || true)"
fi
[ -n "$GATE_INVENTORY" ] || GATE_INVENTORY="(./scripts/preflight.sh --list failed — run it by hand)"

# ---------------------------------------------------------------------------
# 5. Pre-build gate: the LOCAL preflight (scripts/preflight.sh).
#
#    AGENTS.md rule — GitHub Actions is NOT the merge/deploy gate for this repo
#    (every job fails with 0 steps on an account-wide billing failure, so a red
#    check says nothing about the code). scripts/preflight.sh is the gate now,
#    and its gate list is the authoritative one (`--list`) — see the derivation
#    above rather than a copy of the names here.
#
#    Nothing is built or shipped unless the whole preflight is green, where
#    "green" is either a FULL RUN or a whole-inventory REUSE of a per-tree record
#    that already holds an observed `pass` for every gate on THESE bytes under
#    THIS environment fingerprint (see the reuse decision below). A partial or
#    absent record gates in full.
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
  cat >&2 <<EOF

  ============================================================
   !!!  WARNING: --skip-tests — PRE-BUILD GATE BYPASSED  !!!
  ============================================================
   scripts/preflight.sh was NOT run, so ALL of its gates were
   skipped: $GATE_INVENTORY
   A broken or vulnerable build can ship and crash-loop prod
   (see the 000047 migration incident).
   Re-run WITHOUT this flag as soon as the emergency is over.
  ============================================================

EOF
else
  [ -x "$REPO_ROOT/scripts/preflight.sh" ] || die \
    "$REPO_ROOT/scripts/preflight.sh not found or not executable — it is the pre-build gate; restore it (git checkout -- scripts/preflight.sh) rather than building without a gate"

  # ---- PER-TREE RECORD: READ BEFORE JOINING THE GATE POOL --------------------
  # The gate body runs inside a bounded host gate pool (preflight.sh's SLOT POOL,
  # default 2 slots), and a tree that has ALREADY been gated green does not need
  # to take a slot for a verdict that exists. scripts/preflight_reuse.sh is the
  # consumer that answers that question, and it never gates and never takes a
  # slot — so it is free to run here, before the pool.
  #
  # WHAT MAY BE SKIPPED, AND WHAT MAY NOT. Only a gate the record holds an
  # observed `pass` for, for THIS tree (working-tree sha, not commit) under THIS
  # environment fingerprint and inside the TTL. The three decisions:
  #   * full    -> `--reuse-record` alone. Every gate in the inventory is a
  #                recorded pass on THESE bytes, so the runner re-reads the record
  #                BEFORE the pool and exits without taking a slot at all.
  #   * partial -> `--only <PREFLIGHT_RERUN> --reuse-record`. Re-run EXACTLY the
  #                gates the record could not vouch for; the recorded passes are
  #                remembered, not re-run. The rerun list is never empty on this
  #                branch (the consumer reports partial only when a gate is
  #                missing), and if it ever were, `--only ""` degrades to a full
  #                run — the safe direction.
  #   * none / exit 1 / unreadable record -> gate IN FULL, exactly as before.
  # There is deliberately NO flag for any of this and no `--skip-tests` fallback:
  # a deploy must never be the place where a gate the tree is not known to pass is
  # skipped, and a skipped gate must never look like a run one. `run_gate_step`
  # prints the argv it executes (reuse flags included), the runner prints its own
  # `==> REUSE:` line naming every remembered gate, and it never counts a
  # remembered gate as `passed` — see docs/PREFLIGHT-REUSE.md §1-§2.
  #
  # ONE ARRAY FOR THE ARGV, so the label, the executed command and the closing
  # message cannot disagree about what was run.
  GATE_ARGV=(./scripts/preflight.sh)
  REUSE_LINE=""
  if [ -r "$REPO_ROOT/scripts/preflight_reuse.sh" ] && [ -r "$REPO_ROOT/scripts/gate_result.sh" ]; then
    PREFLIGHT_REUSE_REPO="$REPO_ROOT"
    # shellcheck source=scripts/gate_result.sh
    . "$REPO_ROOT/scripts/gate_result.sh"
    # shellcheck source=scripts/preflight_reuse.sh
    . "$REPO_ROOT/scripts/preflight_reuse.sh"
    if preflight_reuse_decision; then
      GATE_LABEL_RECORD="$PREFLIGHT_EVIDENCE"
      echo "==> per-tree gate record: $GATE_LABEL_RECORD"
      echo "    record:    ${PREFLIGHT_RECORD:-none}"
      echo "    reusable:  $PREFLIGHT_DECISION"
      echo "    remembered (not re-run): ${PREFLIGHT_SKIP:-none}"
      echo "    must run:  ${PREFLIGHT_RERUN:-none}"
      echo "    host-seconds the record saves: $PREFLIGHT_REMEMBERED_SECONDS"
      case "$PREFLIGHT_DECISION" in
        full)
          GATE_ARGV+=(--reuse-record)
          REUSE_LINE="WHOLE-INVENTORY REUSE — tree $(gr_short_sha "$PREFLIGHT_TREE"), ${PREFLIGHT_REMEMBERED_SECONDS}s remembered; the runner re-reads the record before the pool and takes NO slot"
          ;;
        partial)
          GATE_ARGV+=(--only "$PREFLIGHT_RERUN" --reuse-record)
          REUSE_LINE="PARTIAL RECORD — remembered: ${PREFLIGHT_SKIP:-none}; RE-RUNNING ONLY: $PREFLIGHT_RERUN (${PREFLIGHT_REMEMBERED_SECONDS}s remembered)"
          ;;
      esac
    else
      echo "==> per-tree gate record: $PREFLIGHT_EVIDENCE"
    fi
  else
    echo "==> per-tree gate record: scripts/preflight_reuse.sh and/or scripts/gate_result.sh are missing" >&2
    echo "    — GATING IN FULL (a deploy must not skip a gate it cannot look up)." >&2
  fi
  [ -z "$REUSE_LINE" ] || info "reuse: $REUSE_LINE"
  # The argv is passed as SEPARATE arguments after the `bash -c` script, so a
  # reuse flag is never re-split or word-split: `exec "$@"` runs exactly the array
  # this script decided on, from inside $REPO_ROOT.
  run_gate_step "${GATE_ARGV[*]} ($GATE_INVENTORY)" \
    bash -c "cd '$REPO_ROOT' && exec \"\$@\"" bash "${GATE_ARGV[@]}"
  if [ "${#GATE_ARGV[@]}" -gt 1 ]; then
    info "pre-build gate green: the per-tree record was read; the gates above are the ones it could not vouch for (the rest are REMEMBERED, and were NOT re-run)"
  else
    info "pre-build gate green: all scripts/preflight.sh gates passed"
  fi
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

Remaining steps ON THE SERVER for service '$SERVICE' (run manually):
  docker load -i $REMOTE_TARBALL
  docker tag $IMAGE:$VERSION $IMAGE:latest
  docker compose up -d --force-recreate $SERVICE
  docker compose logs $SERVICE | head   # must show version=$VERSION commit=$COMMIT
EOF

# The API image carries the version banner, so its log line proves the deploy.
# The web image has no such banner - a successful recreate proves nothing about
# which build is live, so it needs a request-level check instead.
if [ "$SERVICE" != "api" ]; then
cat <<EOF

NOTE - '$SERVICE' has no startup version banner, so "container started" does NOT
mean the new build is live. Verify with a request-level check, e.g. for the web
image a page added in this release must return 200 (not 404):
  curl -s -o /dev/null -w '%{http_code}\n' https://<host>/<page-added-in-$VERSION>
EOF
fi
info "done"