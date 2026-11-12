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
#   1. ./deploy/build-and-ship.sh --ship                # on a clean tree; the gate runs first,
#                                                       # then the server half below runs HERE
#   2. scp the tarball to the server   [--ship does this]
#   3. on the server:  docker load -i <tarball>            [--ship does this]
#   4. on the server:  docker tag jomform-api:X.Y.Z jomform-api:latest
#                      # compose runs :latest — a stale :latest keeps the
#                      # OLD image even after `restart`          [--ship does this]
#   5. on the server:  docker compose up -d --force-recreate <service>
#                      (the service matching IMAGE - see the NOTE printed at the end)
#                      # plain `restart` will NOT pick up the new image   [--ship does this]
#   6. verify: docker compose logs <service> | head   → startup line shows
#      version=... commit=... matching the tag; then curl the health route
#                                                      [--ship does this]
#   7. purge the CDN's cached `/_next/static/*` entries on the deploy host
#                                                      [--ship does this]
#
# Step 7 exists because `/ _next/static/*` is served `public, max-age=31536000,
# immutable` and the CDN caches the RESPONSE INCLUDING ITS HEADERS. A release that
# changes headers but not asset bytes keeps the same hashed chunk names, so the
# edge keeps answering with the OLD header set for up to a year: measured on prod
# 2026-11-09, 39 of the shipped image's 60 assets came from the edge with ZERO
# security headers (ages up to 23.8 days) while the origin had carried them since
# v1.30.0. It runs ONLY after the ship is PROVEN (step 8b) — a failed recreate
# leaves the old image serving, and purging then would only fill the edge from the
# old origin. deploy/edge-purge-static.sh decides the rest: with
# JF_CF_PURGE_TOKEN + JF_CF_PURGE_ZONE_ID set on the deployment host it purges the
# prefix and logs the CDN's answer; with either unset or EMPTY it prints a LOUD
# warning and the release continues (the ship already happened, and no exit code
# brings the asset bytes back).
#
# WITHOUT --ship this is a BUILDER: it builds, saves the tarball and PRINTS steps 2-6 for a
# human. It never claims the image shipped, and it says nothing that could be read as a ship:
# the manual-steps block is headed "run manually" and the script's last word is "done", not
# "shipped". That distinction is load-bearing — see the next paragraph.
#
# --ship IS THE ONLY THING THAT REPORTS A SHIP, AND IT REPORTS IT ONLY AFTER ASKING THE
# SERVER. On the v1.27.3 release (2026-11-08) `deploy/cut-release.sh` reported RELEASED and
# mirrored the public repo while this script had printed "SSH_HOST not set — skipping scp":
# prod still ran v1.27.1, no jomform-api:v1.27.3 image existed on the host, and the mirror
# advertised a release prod had never received. The whole release path was reading ONE EXIT
# CODE, which a build always satisfies. So a ship run now:
#   * refuses up front (exit 7, before the gate and before any build) when REQUIRE_SHIP=1 and
#     neither SSH_HOST nor REMOTE_BUILD_HOST is set — an unreachable server is a refusal, not
#     an info line;
#   * performs scp + docker load + docker tag :latest + docker compose up -d --force-recreate
#     itself, over ssh;
#   * and then PROVES it by reading org.opencontainers.image.version off the RUNNING container
#     (deploy/ship_proof.sh — one definition, shared with cut-release.sh). A recreate that
#     left the old image live exits 7 here instead of passing quietly.
#
# THIS IS THE RELEASE GATE. AGENTS.md moved the FULL preflight here: a merge may
# proceed on the evidence the branch's own iteration run produced (full, scoped or
# reused), but a RELEASE runs the whole inventory on the exact tree being shipped,
# and refuses to build on red. A tree that already holds a whole-inventory green
# record is reused rather than re-gated, so a tree that WAS fully gated costs
# nothing here.
#
# REMOTE BUILD (--remote-build): the image is built ON the deployment server
# instead of on this host, because the compile is what makes this host's load
# spike. The source leaves as a `git archive` of HEAD, is verified by sha256 on the
# far side before it is unpacked, and is built there with the same stamps. No
# tarball is produced and nothing is scp'd — `docker load` is not needed either,
# because the image already exists where it runs. With --ship the tag :latest +
# recreate + proof still run, on the build host.
#
# Usage:
#   ./deploy/build-and-ship.sh [--ship] [--skip-tests] [--remote-build] [VERSION]
#
#   VERSION (or VERSION_OVERRIDE in the environment) may only RESTATE the tag on HEAD: the
#   version comes from deploy/require-release-tag.sh, and a contradicting override is refused
#   instead of silently ignored.
#
#   --ship          deploy for real: scp/load/tag/recreate over ssh and VERIFY the running
#                   container carries this tag. Requires SSH_HOST or REMOTE_BUILD_HOST
#                   (refused with exit 7 before anything is built). Same as REQUIRE_SHIP=1.
#                   Without it this script only builds and prints the server steps.
#
# Exit: 0 shipped (or built) · 1 refused or failed · 4 HEAD carries no release tag · 5 VERSION_OVERRIDE
#       contradicts the tag on HEAD · 7 the ship could not be performed or could not be PROVEN
#       (REQUIRE_SHIP=1 with no reachable server, a failed load/tag/recreate, or a running
#       container that does not carry this tag)
#
#   --skip-tests    DANGER: bypass the pre-build preflight (scripts/preflight.sh,
#                   every gate it lists) and print a loud warning naming them.
#                   Only for a genuine emergency (e.g. an urgent hotfix while the
#                   local test Postgres/Valkey is down) — never for a normal
#                   release.
#                   The gate is what keeps a broken build from crash-looping prod,
#                   so re-run WITHOUT this flag as soon as the emergency is over.
#
#   --remote-build  build on REMOTE_BUILD_HOST (the deployment server). Requires
#                   REMOTE_BUILD_HOST to be set — there is no default, so a typo
#                   can never silently build somewhere nobody looked. Note what a
#                   docker build does NOT inherit: the build itself runs inside the
#                   far side's dockerd, so GATE_CPU_QUOTA (below) bounds only the
#                   gate on THIS host, never the remote compile.
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
#   IMAGE       image name            (default: jomform-api). This is the ONLY
#               setting that decides which Dockerfile and context are used:
#               `*web*` -> web/Dockerfile with context `.`, `*api*` ->
#               api/Dockerfile with context `api`. An image name that is
#               neither requires BOTH of the next two to be set.
#   CONTEXT     build context dir     (default: derived from IMAGE, see above)
#   DOCKERFILE  Dockerfile path       (default: derived from IMAGE, see above)
#               Both may still be given explicitly — but a pair that CONTRADICTS
#               the image is refused with exit 1 before the gate and before any
#               build, never silently corrected. That is the combination that
#               built the Go binary, labelled it jomform-web and shipped it: the
#               web container crash-looped on a missing DATABASE_URL and the site
#               answered 502 until the tag was re-pointed by hand.
#   OUT_DIR     local tarball output  (default: /tmp — keep OUTSIDE the repo
#                                     so the clean-tree guard stays valid)
#   SSH_HOST    set to enable scp of the tarball (e.g. 203.0.113.10); REQUIRED for --ship
#               on a local build (the server half runs over ssh to this host)
#   SSH_USER    (default: root)   SSH_PORT (default: 22)   SSH_PATH (default: /tmp)
#   REQUIRE_SHIP  1 = same as --ship: deploy for real and verify (default: 0)
#   GATE_CPU_QUOTA  cgroup quota for the pre-build gate (default: 300% = 3 cores;
#                   set to "" to run it uncapped). systemd's CPUQuota counts
#                   CORES, not a share of the host. Measured on a 6-core host: an
#                   uncapped cold `go build ./...` peaked at 307% CPU and
#                   `npm run build` at 192%, while CPUQuota=100% (one core) cost
#                   4.5x and 2.9x wall time — so 400% bounds the gate's draw
#                   without costing any wall time. `nice` does NOT do this: it
#                   changes who runs, not how many threads are runnable, so it
#                   leaves the load average untouched.
#   REMOTE_BUILD_HOST      build on this host (e.g. 217.216.39.61) — see above
#   REMOTE_BUILD_DIR       where the source is unpacked there (default /root/jomform-build)
#   REMOTE_BUILD_SSH_USER  (default: root)   REMOTE_BUILD_SSH_PORT (default: 22)
#
# Example — build the web image instead. The IMAGE NAME ALONE IS ENOUGH: the
# Dockerfile and the context are derived from it (web/Dockerfile, context `.`,
# the repo ROOT — the web unit tests, which `npm run build` runs first, read
# api/ sources — see web/Dockerfile and deploy/DEPLOY.md):
#   IMAGE=jomform-web ./deploy/build-and-ship.sh
# The explicit form is equivalent, and is what deploy/cut-release.sh runs:
#   IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile ./deploy/build-and-ship.sh
# Stating the OTHER image's Dockerfile here is refused, not fixed.
#
# Example — the same, built on the deployment server:
#   REMOTE_BUILD_HOST=217.216.39.61 ./deploy/build-and-ship.sh --remote-build

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
OUT_DIR="${OUT_DIR:-/tmp}"
SSH_HOST="${SSH_HOST:-}"
SSH_USER="${SSH_USER:-root}"
SSH_PORT="${SSH_PORT:-22}"
SSH_PATH="${SSH_PATH:-/tmp}"
LOCK_FILE="${LOCK_FILE:-/tmp/jomform-build.lock}"
SKIP_TESTS="${SKIP_TESTS:-0}"
# --- release-path knobs (see the header for the measurements behind them) ----
# GATE_CPU_QUOTA bounds the pre-build GATE on this host. It is deliberately NOT
# applied to a docker build: a build executes inside dockerd, not in this shell's
# cgroup, so a client-side quota would be theatre. That is exactly why
# --remote-build exists — it moves the compile to a host whose job is not running
# lanes. Default 300% = 3 of 6 cores, so half the host stays free for other apps;
# empty = uncapped. Wan's standing rule is 300% at EVERY stage (dev/qa/merge/deploy),
# not just here — deploy/merge-train.sh takes the same quota.
GATE_CPU_QUOTA="${GATE_CPU_QUOTA-300%}"
# The BUILD is a second CPU draw, and it does NOT inherit this shell's cgroup: the
# compile runs inside dockerd's builder. `docker build --cpu-quota` is the supported
# way to bound it and it really does apply to the buildkit worker — measured on the
# deployment host (6 cores, alpine image, 6 parallel busy loops, host CPU from
# /proc/stat deltas): uncapped 4820 ms at ~4.7 cores; `--cpu-quota 300000` 8237 ms at
# 2.9 cores, reproduced 8186 ms / 2.9 cores. So the same 300% rule is applied to the
# build on BOTH paths (remote and local). Empty = uncapped.
BUILD_CPU_QUOTA="${BUILD_CPU_QUOTA-${GATE_CPU_QUOTA:-}}"
# "300%" -> 300000 (docker wants microseconds per 100 ms period); "300000" passes through.
quota_to_micros() {
  case "$1" in
    "") return 0 ;;
    *%) printf '%s' $(( ${1%\%} * 1000 )) ;;   # 1 core = 100000us per 100ms period
    *)  printf '%s' "$1" ;;
  esac
}
BUILD_CPU_FLAGS=()
if [ -n "$BUILD_CPU_QUOTA" ]; then
  _micros=$(quota_to_micros "$BUILD_CPU_QUOTA")
  [ -n "$_micros" ] && BUILD_CPU_FLAGS=(--cpu-quota "$_micros")
fi
REMOTE_BUILD_HOST="${REMOTE_BUILD_HOST:-}"
REMOTE_BUILD_DIR="${REMOTE_BUILD_DIR:-/root/jomform-build}"
REMOTE_BUILD_SSH_USER="${REMOTE_BUILD_SSH_USER:-root}"
REMOTE_BUILD_SSH_PORT="${REMOTE_BUILD_SSH_PORT:-22}"
REMOTE_BUILD_FLAG=0
# REQUIRE_SHIP=1 (or --ship): this run is a DEPLOY, not a build. It must reach a server, it
# must perform the server half itself, and it must prove the running container carries this
# tag before it may exit 0. deploy/cut-release.sh sets this on both of its ships; a human can
# ask for the same thing with --ship. Default 0 keeps this script a builder for everyone else.
REQUIRE_SHIP="${REQUIRE_SHIP:-0}"
SHIP_RAN=0

die() { echo "ERROR: $*" >&2; exit 1; }
# die_rc — same message shape, but the exit code is the one the FAILING STEP chose, so a
# caller can tell "no release tag" (4) from "the override contradicts the tag" (5) instead of
# reading both as a generic 1.
die_rc() { local rc=$1; shift; echo "ERROR: $*" >&2; exit "$rc"; }
info() { echo "==> $*"; }

# ---------------------------------------------------------------------------
# IMAGE -> DOCKERFILE + CONTEXT: ONE DECISION, NOT TWO.
#
# These used to be resolved SEPARATELY (`CONTEXT="${CONTEXT:-api}"`,
# `DOCKERFILE="${DOCKERFILE:-api/Dockerfile}"`), so `IMAGE=jomform-web` on its own
# was accepted and built the API Dockerfile: the Go binary went out carrying
# `org.opencontainers.image.title=jomform-web` and tagged `jomform-web:<version>`.
# The web container then crash-looped on a missing DATABASE_URL and the public site
# answered 502 until the tag was re-pointed by hand. The only check on the pair was
# that the file exists — and `api/Dockerfile` exists.
#
# So the image name alone decides the pair, and an EXPLICIT pair that contradicts
# it is REFUSED (never silently corrected: the operator believed something
# different from what this script would have done, and that is worth saying out
# loud). The refusal happens here, before the release-selftest, the gate and any
# build.
#   *web*  -> web/Dockerfile, context `.`  (the repo root: the web unit tests that
#             `npm run build` runs first read api/ sources — see web/Dockerfile)
#   *api*  -> api/Dockerfile, context `api`
# An image name that is neither needs BOTH given explicitly; guessing is exactly
# how the wrong bytes got shipped.
# ---------------------------------------------------------------------------
# `./api/Dockerfile`, `api/Dockerfile` and `$REPO_ROOT/api/Dockerfile` are the same
# file; normalise before comparing so the guard cannot be spelled past.
normalize_rel_path() {
  local p="$1"
  while [ "${p#./}" != "$p" ]; do p="${p#./}"; done
  if [ -n "$REPO_ROOT" ] && [ "${p#"$REPO_ROOT"/}" != "$p" ]; then
    p="${p#"$REPO_ROOT"/}"
  fi
  printf '%s' "$p"
}
# Which unit a Dockerfile builds. Empty for anything that is not one of the two
# (a sandbox fixture path, for instance): there is then no cross-unit claim to check.
dockerfile_unit() {
  case "$(normalize_rel_path "$1")" in
    api/Dockerfile) printf 'api' ;;
    web/Dockerfile) printf 'web' ;;
  esac
}

REQUESTED_DOCKERFILE="${DOCKERFILE:-}"
REQUESTED_CONTEXT="${CONTEXT:-}"

case "$IMAGE" in
  *web*) IMAGE_UNIT=web ;;
  *api*) IMAGE_UNIT=api ;;
  *)     IMAGE_UNIT="" ;;
esac

PAIR_DOCKERFILE=""
PAIR_CONTEXT=""
case "$IMAGE_UNIT" in
  web) PAIR_DOCKERFILE=web/Dockerfile; PAIR_CONTEXT=. ;;
  api) PAIR_DOCKERFILE=api/Dockerfile; PAIR_CONTEXT=api ;;
  *)
    { [ -n "$REQUESTED_DOCKERFILE" ] && [ -n "$REQUESTED_CONTEXT" ]; } || die \
      "cannot derive a build pair from IMAGE=$IMAGE — it names neither the *web* nor the *api* image, so set DOCKERFILE=... and CONTEXT=... explicitly (guessing the Dockerfile is how one unit's bytes get tagged as another's)"
    ;;
esac

DOCKERFILE="${REQUESTED_DOCKERFILE:-$PAIR_DOCKERFILE}"
CONTEXT="${REQUESTED_CONTEXT:-$PAIR_CONTEXT}"

# The pair the image DOES build from, checked against what the caller stated.
if [ -n "$IMAGE_UNIT" ] && [ -n "$REQUESTED_DOCKERFILE" ]; then
  DF_UNIT="$(dockerfile_unit "$REQUESTED_DOCKERFILE")"
  if [ -n "$DF_UNIT" ] && [ "$DF_UNIT" != "$IMAGE_UNIT" ]; then
    die "IMAGE=$IMAGE is the $IMAGE_UNIT image and must be built from $PAIR_DOCKERFILE, but DOCKERFILE=$REQUESTED_DOCKERFILE builds the $DF_UNIT image — refusing to tag one unit's bytes as the other's"
  fi
  if [ -n "$DF_UNIT" ] && [ -n "$REQUESTED_CONTEXT" ]; then
    case "$DF_UNIT" in
      web) DF_CONTEXT=. ;;
      api) DF_CONTEXT=api ;;
    esac
    if [ "$(normalize_rel_path "$REQUESTED_CONTEXT")" != "$(normalize_rel_path "$DF_CONTEXT")" ]; then
      die "IMAGE=$IMAGE DOCKERFILE=$DOCKERFILE must be built with CONTEXT=$DF_CONTEXT, but CONTEXT=$REQUESTED_CONTEXT was given"
    fi
  fi
fi

# ---------------------------------------------------------------------------
# purge_static_edge — drop the CDN's cached `/_next/static/*` entries (step 8c).
#
# THE CREDENTIAL IS NOT HERE. JF_CF_PURGE_TOKEN / JF_CF_PURGE_ZONE_ID live in the
# deployment host's compose `.env` (that is where cdnpurge reads them from, in the
# API container), and this shell is a different machine that never holds them. So
# the step runs WHERE THE TOKEN IS: this function builds one stdin stream — the
# four purge vars read out of `$SP_COMPOSE_DIR/.env` by NAME, then
# deploy/edge-purge-static.sh's own bytes — and pipes it into `bash -s` on
# $SP_DEST. The .env is READ, never sourced: it belongs to compose, not to bash,
# and a value containing `$(…)` must not become a command here.
#
# WHY READ THE VARS RATHER THAN LET THE SCRIPT DO IT: the script must also be
# runnable by hand with a plain environment (that is how its own suite drives it),
# so it reads the environment it is given and nothing else.
#
# NOTHING IN THIS PATH FAILS THE RELEASE. The ship has already been PROVEN by the
# time this runs; a CDN that refuses, a missing curl, an unreachable .env, or an
# unprovisioned token leave prod running the bytes it is running. What must NOT
# happen is a silent "purged" claim, so every non-purged outcome prints the same
# canonical sentence, and `edge not purged:` is what a log search finds.
purge_static_edge() {
  local script="$REPO_ROOT/deploy/edge-purge-static.sh" rc=0 stream
  local canon='edge not purged: stale `_next/static` headers may be served for up to a year after a header-only release'
  if [ ! -r "$script" ]; then
    info "WARNING: $canon"
    info "         $script is missing — restore it from the gated tree (git checkout -- deploy/edge-purge-static.sh)"
    return 0
  fi
  info "purging the static edge prefix through $SP_DEST (deploy/edge-purge-static.sh)"
  stream="$(mktemp "${TMPDIR:-/tmp}/static-edge-purge.XXXXXX" 2>/dev/null)" || stream=""
  if [ -z "$stream" ]; then
    info "WARNING: $canon"
    info "         could not create a temp file to build the purge stream"
    return 0
  fi
  {
    printf 'set -uo pipefail\n'
    printf 'cd %q || exit 3\n' "$SP_COMPOSE_DIR"
    printf '%s\n' '[ -r .env ] || exit 4'
    # Read one var by NAME out of .env, last definition wins, surrounding quotes
    # stripped. Deliberately not `set -a; . .env`: compose's .env is not a bash
    # script and sourcing it could execute what it holds.
    cat <<'PROLOGUE'
val() { awk -v k="$1" '{ line=$0; sub(/^[ \t]+/, "", line); if (index(line, k "=") == 1) { v=substr(line, length(k)+2); out=v } } END { printf "%s", out }' .env; }
unq() { local v="$1"; case "$v" in \"*\") v="${v#\"}"; v="${v%\"}" ;; \'*\') v="${v#\'}"; v="${v%\'}" ;; esac; printf '%s' "$v"; }
JF_CF_PURGE_TOKEN="$(unq "$(val JF_CF_PURGE_TOKEN)")"
JF_CF_PURGE_ZONE_ID="$(unq "$(val JF_CF_PURGE_ZONE_ID)")"
JF_CF_PURGE_HOSTS="$(unq "$(val JF_CF_PURGE_HOSTS)")"
JF_CF_PURGE_API_BASE="$(unq "$(val JF_CF_PURGE_API_BASE)")"
APP_BASE_URL="$(unq "$(val APP_BASE_URL)")"
export JF_CF_PURGE_TOKEN JF_CF_PURGE_ZONE_ID JF_CF_PURGE_HOSTS JF_CF_PURGE_API_BASE APP_BASE_URL
PROLOGUE
    cat "$script"
  } >"$stream" || {
    rm -f "$stream"
    info "WARNING: $canon"
    info "         could not write the purge stream"
    return 0
  }
  ssh "${SP_SSH_OPTS[@]}" "$SP_DEST" 'bash -s' <"$stream" || rc=$?
  rm -f "$stream"
  case "$rc" in
    0) : ;;
    3)
      info "WARNING: $canon"
      info "         the purge step on $SP_DEST exited 3 (see the reason above)"
      ;;
    4)
      info "WARNING: $canon"
      info "         $SP_DEST has no $SP_COMPOSE_DIR/.env — the purge credentials could not be read"
      ;;
    *)
      info "WARNING: $canon"
      info "         ssh/bash on $SP_DEST exited $rc — no purge was attempted"
      ;;
  esac
  return 0
}


# usage() prints this script's own header comment (from line 2 down to the
# `set -euo pipefail` line) with the leading "# " stripped.
usage() { sed -n '2,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }

VERSION_OVERRIDE="${VERSION_OVERRIDE:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)   usage; exit 0 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    --remote-build) REMOTE_BUILD_FLAG=1 ;;
    --ship)      REQUIRE_SHIP=1 ;;
    --)          shift; break ;;
    -*)          die "unknown flag: $1 (see --help)" ;;
    *)           [ -z "$VERSION_OVERRIDE" ] || die "unexpected extra argument: $1 (see --help)"
                 VERSION_OVERRIDE="$1" ;;
  esac
  shift
done

# The version stamp is resolved by deploy/require-release-tag.sh, which runs as a CHILD
# process — so the override must be EXPORTED and must not be reset here. Both shapes are
# accepted (env and positional) and both mean the same thing: a restatement of the tag that
# must equal it. Until this export existed, `VERSION_OVERRIDE=v9.9.9 ./deploy/build-and-ship.sh`
# on a v1.0.0 tree stamped v1.0.0 and shipped: the override was silently discarded, so a
# caller could not make the ship refuse a tree it thought it was not shipping.
export VERSION_OVERRIDE

# --remote-build names no host of its own on purpose: the host must be explicit in
# the environment, so a release can never be built somewhere nobody chose.
if [ "$REMOTE_BUILD_FLAG" = "1" ]; then
  [ -n "$REMOTE_BUILD_HOST" ] || die \
    "--remote-build needs REMOTE_BUILD_HOST=<host> (there is no default host — e.g. REMOTE_BUILD_HOST=217.216.39.61)"
fi

# ---------------------------------------------------------------------------
# 0. A SHIP MUST BE ABLE TO REACH THE SERVER — decided BEFORE the lock, the gate and the
#    build, so an unreachable deploy fails in a second instead of after half an hour.
#
#    This check is the fix for the v1.27.3 release, which spent the whole gate, built two
#    images, then printed "SSH_HOST not set — skipping scp" and exited 0: the caller saw
#    success and mirrored a tree prod never received. An info line is not a refusal, and an
#    exit code that means "the build finished" must never be read as "the server has it".
#    So on a ship path, no reachable server is exit 7 and nothing is gated or built.
# ---------------------------------------------------------------------------
SHIP_PROOF_LIB="$REPO_ROOT/deploy/ship_proof.sh"
if [ "$REQUIRE_SHIP" = "1" ]; then
  [ -r "$SHIP_PROOF_LIB" ] || die_rc 7 \
    "REQUIRE_SHIP=1 but $SHIP_PROOF_LIB is missing — refusing to ship what cannot be verified"
  # shellcheck source=deploy/ship_proof.sh
  . "$SHIP_PROOF_LIB"
  if ! ship_proof_target; then
    die_rc 7 "REQUIRE_SHIP=1 (--ship) but nothing can reach a server: set SSH_HOST=<deploy host> (or REMOTE_BUILD_HOST=<host> to build there). Nothing was gated, built or shipped. Run WITHOUT --ship for a build-only run."
  fi
  info "ship target: $SP_DEST ($SP_TARGET_KIND), compose dir $SP_COMPOSE_DIR"
fi

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
# 3. Resolve provenance stamps from git — A DEPLOY SHIPS A RELEASE, so HEAD must be
#    tagged. This used to be `git describe --tags --always`, which silently stamped an
#    untagged HEAD with a raw SHA: the deploy happened with no release decision behind
#    it, and tags became a by-product of deploying (measured 2026-11-07: 806 tags on
#    1,341 commits, 102 in seven days). The rule now lives in
#    deploy/require-release-tag.sh, which deploy/release_selftest.sh tests directly.
# ---------------------------------------------------------------------------
VERSION="$(bash "$REPO_ROOT/deploy/require-release-tag.sh")" || {
  rc=$?
  case "$rc" in
    4) die_rc 4 "refusing to build: HEAD carries no release tag — cut a release with ./deploy/cut-release.sh" ;;
    5) die_rc 5 "refusing to build: VERSION_OVERRIDE=${VERSION_OVERRIDE:-} does not match the tag on HEAD — a release is a TAG, not an override (see deploy/require-release-tag.sh)" ;;
    *) die_rc "$rc" "refusing to build: deploy/require-release-tag.sh exited $rc" ;;
  esac
}
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
info "stamps: version=$VERSION commit=$COMMIT date=$BUILD_DATE"

# The release rules are TESTED before they are used, here, because this is the only path that
# ships: require-release-tag (above), cut-release's version arithmetic, and the mirror sync.
# Hermetic — throwaway git repos, no network, no docker — so it cannot be the reason a release
# waits on a resource. SKIP_RELEASE_SELFTEST=1 is for a machine with no git binary at all.
#
# THE RELEASE ENVIRONMENT IS DROPPED from the child's environment (`env -u`), because this
# script EXPORTS the knobs of the release it is performing, and the suite tests FIXTURES — a
# caller's release does not describe them, it rewrites them. This defect has now shipped twice:
#
#   VERSION_OVERRIDE — exported here for require-release-tag.sh above (cut-release passes it),
#   while the selftest sets its OWN override per case. Measured on a v9.9.9 tree:
#   `VERSION_OVERRIDE=v9.9.9 ./deploy/build-and-ship.sh`, exactly what deploy/cut-release.sh
#   runs, scored 72 passed / 7 failed and refused to build; the same bytes with the variable
#   unset scored 79/0 — so the documented release path could not ship at all (#1633).
#
#   SSH_HOST / REMOTE_BUILD_HOST / REQUIRE_SHIP / IMAGE / CONTEXT / DOCKERFILE / SERVICE — the
#   ship plumbing added afterwards (#1650, #1653) leaked the CALLER's release into the sandbox
#   fixtures: REQUIRE_SHIP=1 made the sandbox's own build-and-ship refuse for a missing
#   ship_proof.sh (O/O2 exit 7 instead of 5/4), an inherited SSH_HOST made R1 "ship" with a
#   server that case does not give it, inherited REQUIRE_SHIP made R4's build-only control
#   print SHIPPED, and inherited IMAGE/CONTEXT/DOCKERFILE made R2-R4 look for web/Dockerfile
#   and report "no tarball". Measured on v1.28.0's bytes, `SSH_HOST=<host> REQUIRE_SHIP=1
#   IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile bash deploy/release_selftest.sh`
#   scored 189 passed / 21 failed; the same bytes with that set unset scored 210 passed /
#   0 failed, rc 0. A release could not ship until the operator scrubbed the environment by
#   hand — the gate that guards every release was being failed by the release itself.
#
# deploy/release_selftest.sh drops the same set itself as well. The two are INDEPENDENT, so
# neither call shape (this script's environment, or a human's shell) can leak it.
#
#   SSH_USER / SSH_PORT / SSH_PATH — the same group: this script scp's to
#   "$SSH_USER@$SSH_HOST:$SSH_PATH/" on port "$SSH_PORT", so a caller who ships to sshd on 2222
#   rewrote the sandbox's fixtures. It was invisible because R2 asserted `scp -P 22`, which
#   `scp -P 2222 …` satisfies. The suite now matches the port as a whole token (R2b) and proves
#   the sandbox used 22 / root / /tmp (R6); these three are scrubbed here for the same reason as
#   the eight above.
if [ -x "$REPO_ROOT/deploy/release_selftest.sh" ] && [ "${SKIP_RELEASE_SELFTEST:-0}" != 1 ]; then
  selftest_log="${TMPDIR:-/tmp}/release-selftest.$$.log"
  selftest_t0="$(date +%s)"
  if env -u VERSION_OVERRIDE -u SSH_HOST -u REMOTE_BUILD_HOST -u REQUIRE_SHIP \
         -u IMAGE -u CONTEXT -u DOCKERFILE -u SERVICE \
         -u SSH_USER -u SSH_PORT -u SSH_PATH \
         bash "$REPO_ROOT/deploy/release_selftest.sh" >"$selftest_log" 2>&1; then
    selftest_secs=$(( $(date +%s) - selftest_t0 ))
    # THE DURATION ON THIS LINE IS PART OF THE CONTRACT, not decoration. This stage is
    # the deploy's largest FIXED cost, it is CPU-bound, and what it costs is whatever
    # the HOST gives it — measured 112.78s at load 47.7 on 6 cores against a nominal
    # ~21s. deploy/biteproof_build_and_ship_gate.sh case A times the WHOLE deploy (a
    # reuse cannot remove this stage) and asserts its ceiling NET of it, which it can
    # only do from a number the run itself reports. Without that number the harness
    # either uses an absolute ceiling that false-reds a correct tree on a busy host
    # (card t_87d48a3e: 135s wall, every semantic assertion green) or guesses a load
    # factor. A release log that names the stage's cost is also simply more useful:
    # it makes "this release sat in its own selftest" visible instead of inferred.
    info "release rules selftest: passed ($(grep -c '^  ok' "$selftest_log" 2>/dev/null || echo '?') checks in ${selftest_secs}s)"
    rm -f "$selftest_log"
  else
    tail -25 "$selftest_log" >&2 || true
    die "the release rules selftest FAILED — refusing to build (full log: $selftest_log)"
  fi
fi

# ---------------------------------------------------------------------------
# 4. Preflight.
# ---------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || [ -n "$REMOTE_BUILD_HOST" ] || die "docker not found in PATH"
if [ -n "$REMOTE_BUILD_HOST" ]; then
  command -v ssh >/dev/null 2>&1 || die "ssh not found in PATH — required for REMOTE_BUILD_HOST"
  command -v tar >/dev/null 2>&1 || die "tar not found in PATH — required to unpack the source archive"
fi
[ -f "$DOCKERFILE" ] || die "$DOCKERFILE not found in $(pwd) — the image cannot be built from a Dockerfile that is not in the tree (the api Dockerfile lives on the Dockerfile branch, issue #25; merge/checkout it first)"
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
  # environment fingerprint and inside the TTL. `PREFLIGHT_RERUN` is therefore not
  # simply "the gates that failed": the consumer also puts `web` back on it when
  # this tree has no `web/.next` and a browser gate is about to run, because `web`
  # PRODUCES that build and a remembered pass is evidence about the BYTES, not about
  # a build directory — `--ship-only` re-seeds a fresh tree with the same tree sha
  # and carries only `web/node_modules` (card t_7ad889f7, `docs/PREFLIGHT-REUSE.md`
  # rule 10). So a `web` in this list on a tree whose record says `web: pass` is
  # correct, not a bug. The three decisions:
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
  # ---- SCOPE THE RELEASE GATE TO THE DIFF (card t_ec2aac42) ------------------
  # A release re-runs the whole preflight on the shipping tree, and the feature
  # e2e family is ~570s of it. The AREAS a release touched are covered
  # automatically: the two e2e gates narrow themselves through
  # `web/e2e/areas.tsv` when they are given a base ref, print the decision they
  # made (`e2e-core: subset — 4/76 feature specs (matched web/src/app/sales/**)`),
  # and run the WHOLE family for anything the map cannot attribute. The base is
  # the previous release tag, so the diff is exactly "what this release carries".
  # `PREFLIGHT_E2E=full` still forces every spec, and that is the only run whose
  # result may be quoted as the whole suite.
  #
  # It is added ONLY to the no-record argv: `--for-diff` cannot be combined with
  # `--only`/`--skip` (the diff decides the set), so the `partial` reuse branch
  # below gates without it rather than silently dropping one of the two.
  PREV_RELEASE_TAG="$(git describe --tags --abbrev=0 'HEAD^' 2>/dev/null || true)"
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
  # The scoped release run: only when the record vouched for nothing (a reuse
  # branch built its own argv above, and `--for-diff` cannot be combined with
  # `--only`). No previous tag means no narrowing — the whole family runs.
  if [ "${#GATE_ARGV[@]}" -eq 1 ] && [ -n "$PREV_RELEASE_TAG" ]; then
    GATE_ARGV+=(--for-diff "$PREV_RELEASE_TAG")
    info "scoped release gate: --for-diff $PREV_RELEASE_TAG (the e2e gates narrow to the areas this release touched and PRINT the decision; PREFLIGHT_E2E=full forces every spec)"
  fi
  # CPU-QUOTA THE GATE. The gate is the CPU-hungry half of a release (`go test
  # -race ./...`, `next build`, the shell suites) and on a host that also runs
  # lanes and other people's containers it takes whatever it can find. Bound it in
  # a transient systemd scope rather than deprioritising it: `nice` does not lower
  # the load average (the threads stay runnable), a cgroup quota does. Measured
  # here — CPUQuota=100% (1 core) cost 4.5x on `go build ./...` and 2.9x on
  # `npm run build`. A 300% quota sits above `npm run build`'s own peak (192%) and
  # shaves only the very top of a cold `go build` (307%), so it costs ~nothing in
  # wall time while guaranteeing three of six cores stay free for everything else
  # on this host. Falls back to an uncapped run, loudly.
  GATE_LAUNCH=()
  GATE_QUOTA_LABEL="no CPU quota"
  if [ -n "$GATE_CPU_QUOTA" ]; then
    if command -v systemd-run >/dev/null 2>&1; then
      GATE_LAUNCH=(systemd-run --scope -q -p "CPUQuota=$GATE_CPU_QUOTA")
      GATE_QUOTA_LABEL="CPUQuota=$GATE_CPU_QUOTA"
    else
      info "WARNING: systemd-run not found — the pre-build gate runs UNCAPPED (it will take every core it can)"
    fi
  fi
  # The argv is passed as SEPARATE arguments after the `bash -c` script, and is
  # expanded HERE (not into a prebuilt array) so a change to GATE_ARGV above is
  # always what runs: deploy/biteproof_build_and_ship_gate.sh case E sabotages the
  # routing by rewriting GATE_ARGV just before this call, and a prebuilt command
  # would make that sabotage a no-op — a vacuous bite proof.
  run_gate_step "${GATE_ARGV[*]} ($GATE_INVENTORY) [$GATE_QUOTA_LABEL]" \
    "${GATE_LAUNCH[@]}" bash -c "cd '$REPO_ROOT' && exec \"\$@\"" bash "${GATE_ARGV[@]}"
  if [ "${#GATE_ARGV[@]}" -gt 1 ]; then
    info "pre-build gate green: the per-tree record was read; the gates above are the ones it could not vouch for (the rest are REMEMBERED, and were NOT re-run)"
  else
    info "pre-build gate green: all scripts/preflight.sh gates passed"
  fi
fi

# ---------------------------------------------------------------------------
# 6. Build with provenance stamps (build args + OCI labels).
#    api/Dockerfile declares ARG VERSION / COMMIT / BUILD_DATE and wires all
#    three into the go build -ldflags -X version.* values, so the binary — and
#    therefore /version, `--version` and the startup banner — reports exactly
#    the stamps logged above and set as the OCI labels below. A build that
#    passes no --build-arg still reports dev/none/unknown, never "".
#    api/internal/version/dockerfile_wiring_test.go fails when that wiring rots.
# ---------------------------------------------------------------------------
info "building $IMAGE:$VERSION from $DOCKERFILE (context: $CONTEXT)"
TREE_SHA="$(git rev-parse "HEAD^{tree}")"
info "tree being built: $TREE_SHA (this is the tree the gate just covered)"

BUILD_ARGS=(
  --build-arg "VERSION=$VERSION"
  --build-arg "COMMIT=$COMMIT"
  --build-arg "BUILD_DATE=$BUILD_DATE"
  --label "org.opencontainers.image.title=$IMAGE"
  --label "org.opencontainers.image.version=$VERSION"
  --label "org.opencontainers.image.revision=$COMMIT"
  --label "org.opencontainers.image.created=$BUILD_DATE"
  --tag "$IMAGE:$VERSION"
)

if [ -n "$REMOTE_BUILD_HOST" ]; then
  # -------------------------------------------------------------------------
  # 6r. REMOTE BUILD — compile on the deployment server, not on this host.
  #
  #     The compile is what spikes this host's load, and a docker build cannot be
  #     bounded from here (it runs inside dockerd). Moving it means the bytes have
  #     to leave as SOURCE, so the source is a `git archive` of exactly HEAD and is
  #     verified by sha256 on the far side BEFORE it is unpacked: a truncated or
  #     replaced archive fails the release instead of building the wrong code.
  #
  #     What is deliberately NOT done here UNLESS --ship is given: tagging `:latest` or
  #     recreating the service. On a build-only run those stay printed, human-run steps — the
  #     same policy as the local path. With --ship they run here and are verified, because a
  #     release that cannot reach prod must not report itself shipped.
  # -------------------------------------------------------------------------
  SSH_OPTS=(-p "$REMOTE_BUILD_SSH_PORT" -o BatchMode=yes)
  SSH_DEST="$REMOTE_BUILD_SSH_USER@$REMOTE_BUILD_HOST"
  ARCHIVE="$(mktemp "${OUT_DIR:-/tmp}/jomform-src-XXXXXX.tar")"
  trap 'rm -f "$ARCHIVE"' EXIT

  info "remote build on $SSH_DEST → $REMOTE_BUILD_DIR (source = git archive of HEAD $COMMIT)"
  git archive --format=tar -o "$ARCHIVE" HEAD \
    || die "git archive failed — nothing to build from"
  LOCAL_SUM="$(sha256sum "$ARCHIVE" | cut -d' ' -f1)"
  info "source archive: $(du -h "$ARCHIVE" | cut -f1), sha256 $LOCAL_SUM"

  REMOTE_SUM="$(cat "$ARCHIVE" | ssh "${SSH_OPTS[@]}" "$SSH_DEST" \
      "mkdir -p '$REMOTE_BUILD_DIR' && cat > '$REMOTE_BUILD_DIR/src.tar' && sha256sum '$REMOTE_BUILD_DIR/src.tar' | cut -d' ' -f1")" \
    || die "shipping the source archive to $SSH_DEST failed"
  [ "$REMOTE_SUM" = "$LOCAL_SUM" ] || die \
    "source archive differs on $SSH_DEST (local $LOCAL_SUM, remote $REMOTE_SUM) — refusing to build bytes that are not the gated tree"
  info "source archive verified on $SSH_DEST (sha256 $REMOTE_SUM)"

  ssh "${SSH_OPTS[@]}" "$SSH_DEST" \
    "set -euo pipefail; rm -rf '$REMOTE_BUILD_DIR/src'; mkdir -p '$REMOTE_BUILD_DIR/src'; tar -xf '$REMOTE_BUILD_DIR/src.tar' -C '$REMOTE_BUILD_DIR/src'" \
    || die "unpacking the source archive on $SSH_DEST failed"

  # Quoted as $REMOTE_BUILD_DIR/src/<path>: the archive root IS the repo root.
  info "docker build on $SSH_DEST (context: $REMOTE_BUILD_DIR/src/$CONTEXT)"
  ssh "${SSH_OPTS[@]}" "$SSH_DEST" \
    "set -euo pipefail; cd '$REMOTE_BUILD_DIR/src' && docker build $(printf '%q ' "${BUILD_CPU_FLAGS[@]}")-f '$DOCKERFILE' '$CONTEXT' $(printf '%q ' "${BUILD_ARGS[@]}")" \
    || die "remote docker build FAILED on $SSH_DEST — nothing was tagged or recreated"

  REMOTE_IMAGE_ID="$(ssh "${SSH_OPTS[@]}" "$SSH_DEST" "docker image inspect --format '{{.Id}}' '$IMAGE:$VERSION'")" \
    || die "the build reported success but $SSH_DEST has no $IMAGE:$VERSION image"
  info "built $IMAGE:$VERSION ON $SSH_DEST ($REMOTE_IMAGE_ID) — no tarball, no docker load"
  info "labels on that image (proves provenance):"
  ssh "${SSH_OPTS[@]}" "$SSH_DEST" \
    "docker image inspect --format '{{index .Config.Labels \"org.opencontainers.image.version\"}} {{index .Config.Labels \"org.opencontainers.image.revision\"}}' '$IMAGE:$VERSION'" \
    || true
  BUILD_HOST_LINE="$SSH_DEST (remote — nothing was compiled on this host)"
else
  docker build \
    "${BUILD_CPU_FLAGS[@]}" \
    -f "$DOCKERFILE" \
    "$CONTEXT" \
    "${BUILD_ARGS[@]}"

  IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$IMAGE:$VERSION")"
  info "built $IMAGE:$VERSION ($IMAGE_ID)"
  BUILD_HOST_LINE="this host"
fi

# ---------------------------------------------------------------------------
# 7. Save as a gzip tarball (server-to-server pattern: no registry needed).
#    Skipped entirely for a remote build: the image is already where it runs.
# ---------------------------------------------------------------------------
if [ -n "$REMOTE_BUILD_HOST" ]; then
  info "skipping tarball (remote build: the image already lives on $REMOTE_BUILD_HOST)"
  TARBALL=""
else
  TARBALL="$OUT_DIR/$IMAGE-$VERSION.tar.gz"
  info "saving image → $TARBALL"
  docker save "$IMAGE:$VERSION" | gzip > "$TARBALL"
fi

# ---------------------------------------------------------------------------
# 8. Ship. Two very different things share this step, and the difference is the whole point
#    of the v1.27.3 fix:
#      REQUIRE_SHIP=1 (--ship) — a DEPLOY: scp, load, tag :latest, recreate, then PROVE the
#        running container carries this tag. Any failure is exit 7 and it says what is on the
#        server instead.
#      default                — a BUILD: at most an scp of the tarball when SSH_HOST is set.
#        The server steps stay printed for a human, and nothing here claims they ran.
# ---------------------------------------------------------------------------
if [ -n "$REMOTE_BUILD_HOST" ]; then
  info "skipping scp (remote build: the image already lives on $REMOTE_BUILD_HOST)"
  if [ "$REQUIRE_SHIP" = "1" ]; then
    # The image was built on this same host, so there is nothing to load: tag :latest (compose
    # runs :latest — a stale one keeps the OLD image) and recreate the service from it.
    info "shipping on $SP_DEST: tag $IMAGE:latest + recreate '$SERVICE'"
    ssh "${SP_SSH_OPTS[@]}" "$SP_DEST" \
      "set -euo pipefail; docker tag '$IMAGE:$VERSION' '$IMAGE:latest' && cd '$SP_COMPOSE_DIR' && docker compose up -d --force-recreate '$SERVICE'" \
      || die_rc 7 "shipping on $SP_DEST FAILED (tag :latest / recreate '$SERVICE') — the service was NOT recreated from $VERSION"
    SHIP_RAN=1
  fi
elif [ -n "$SSH_HOST" ]; then
  if [ "$REQUIRE_SHIP" = "1" ]; then
    info "shipping $TARBALL → $SSH_USER@$SSH_HOST:$SSH_PATH/ (port $SSH_PORT)"
  else
    info "copying $TARBALL → $SSH_USER@$SSH_HOST:$SSH_PATH/ (port $SSH_PORT) — build only: the server steps are printed below, NOT run"
  fi
  scp -P "$SSH_PORT" "$TARBALL" "$SSH_USER@$SSH_HOST:$SSH_PATH/" \
    || die_rc 7 "scp of $TARBALL to $SSH_USER@$SSH_HOST:$SSH_PATH/ failed — nothing reached the server"
  if [ "$REQUIRE_SHIP" = "1" ]; then
    REMOTE_TARBALL="$SSH_PATH/$(basename "$TARBALL")"
    info "shipping on $SP_DEST: load $REMOTE_TARBALL, tag $IMAGE:latest, recreate '$SERVICE'"
    ssh "${SP_SSH_OPTS[@]}" "$SP_DEST" \
      "set -euo pipefail; docker load -i '$REMOTE_TARBALL' && docker tag '$IMAGE:$VERSION' '$IMAGE:latest' && cd '$SP_COMPOSE_DIR' && docker compose up -d --force-recreate '$SERVICE'" \
      || die_rc 7 "shipping on $SP_DEST FAILED (load / tag :latest / recreate '$SERVICE') — the service was NOT recreated from $VERSION"
    SHIP_RAN=1
  fi
else
  # Unreachable on a ship run (step 0 refuses it), so this is the build-only path.
  info "SSH_HOST not set — skipping scp (copy $TARBALL manually)"
fi

# ---------------------------------------------------------------------------
# 8b. THE PROOF. Recreating a container is not evidence that it runs the new image: compose
#     runs :latest, so a skipped/replaced tag, or an `up` that did not recreate, leaves the
#     OLD image serving traffic while every command above returned 0 (issue #576 shipped a
#     web container that way). Ask the SERVER what the running container is, and refuse the
#     release if the answer is not this tag. deploy/ship_proof.sh is the one definition of
#     that question — deploy/cut-release.sh asks it again before it mirrors.
# ---------------------------------------------------------------------------
if [ "$SHIP_RAN" = "1" ]; then
  info "verifying: the RUNNING '$SERVICE' container on $SP_DEST must carry org.opencontainers.image.version=$VERSION"
  if ! ship_proof_live "$SERVICE" "$IMAGE" "$VERSION"; then
    die_rc 7 "SHIP NOT PROVEN — '$SERVICE' on $SP_DEST is not running $VERSION (see above). The image exists on the host, but prod is serving something else: do NOT report this release as shipped. Fix the service (docker tag/skip-latest/compose recreate) and re-run, or roll back deliberately."
  fi
  info "SHIPPED $IMAGE:$VERSION — '$SERVICE' on $SP_DEST is running it (org.opencontainers.image.version=$VERSION)"
  # 8c. The roll is PROVEN, so the static edge prefix may be purged. This is the
  # only call site: on an unproven roll die_rc 7 above already exited, because
  # purging while the OLD image serves would just refill the edge from it.
  purge_static_edge
fi

# ---------------------------------------------------------------------------
# 9. What is left. After a verified ship there is nothing left, so the script says so and
#    gives the rollback instead; otherwise it prints the server-side steps (never executed
#    from here) exactly as before.
# ---------------------------------------------------------------------------
if [ "$SHIP_RAN" = "1" ]; then
cat <<EOF

Build host: $BUILD_HOST_LINE
Gated tree: $TREE_SHA
Image:      $IMAGE:$VERSION

SHIPPED to $SP_DEST and VERIFIED: service '$SERVICE' is running it
  (org.opencontainers.image.version=$VERSION read from the running container).

Step 8c asked the CDN to drop its cached /_next/static/ entries. Read the step's own line
above, because it is the only thing that knows the answer:
  'edge purge: … success=true'  -> the CDN ACCEPTED the purge (a following request should
                                   answer CF-Cache-Status: MISS)
  'edge not purged: …'          -> it did NOT happen: the edge still holds the previous
                                   header set for up to a year, so set JF_CF_PURGE_TOKEN and
                                   JF_CF_PURGE_ZONE_ID in $SP_COMPOSE_DIR/.env and re-run this
                                   script, or purge the zone once by hand.

Nothing is left to run by hand. Rollback: put the previous tag back on :latest
and recreate, e.g.
  docker tag $IMAGE:<previous-tag> $IMAGE:latest
  cd $SP_COMPOSE_DIR && docker compose up -d --force-recreate $SERVICE
EOF
elif [ -n "$REMOTE_BUILD_HOST" ]; then
cat <<EOF

Build host: $BUILD_HOST_LINE
Gated tree: $TREE_SHA

Remaining steps ON THE SERVER for service '$SERVICE' (run manually).
NOTE the first line is different from a local build: there is no docker load,
because the image was built there.

  docker tag $IMAGE:$VERSION $IMAGE:latest
  cd ${REMOTE_COMPOSE_DIR:-/opt/jomform} && docker compose up -d --force-recreate $SERVICE
  docker compose logs $SERVICE | head   # must show version=$VERSION commit=$COMMIT
EOF
else
REMOTE_TARBALL="$SSH_PATH/$(basename "$TARBALL")"
cat <<EOF

Build host: $BUILD_HOST_LINE
Gated tree: $TREE_SHA

Remaining steps ON THE SERVER for service '$SERVICE' (run manually):
  docker load -i $REMOTE_TARBALL
  docker tag $IMAGE:$VERSION $IMAGE:latest
  docker compose up -d --force-recreate $SERVICE
  docker compose logs $SERVICE | head   # must show version=$VERSION commit=$COMMIT
EOF
fi

# The API image carries the version banner, so its log line proves the deploy.
# The web image has no such banner - a successful recreate proves nothing about
# which build is live, so it needs a request-level check instead.
if [ "$SERVICE" != "api" ] && [ "$SHIP_RAN" != "1" ]; then
cat <<EOF

NOTE - '$SERVICE' has no startup version banner, so "container started" does NOT
mean the new build is live. Verify with a request-level check, e.g. for the web
image a page added in this release must return 200 (not 404):
  curl -s -o /dev/null -w '%{http_code}\n' https://<host>/<page-added-in-$VERSION>
EOF
fi
info "done"