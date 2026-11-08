#!/usr/bin/env bash
# require-release-tag — the ONE place that decides "what version is this tree?".
#
# A deploy ships a RELEASE. Before this existed, `build-and-ship.sh` resolved the version
# with `git describe --tags --always`, so an untagged HEAD silently shipped stamped with a
# raw SHA: the deploy happened, but nothing recorded that anyone had decided to release.
# Measured at the time of this change: 806 tags on 1,341 commits of main, 102 tags in the
# last seven days, ~15 deploys a day. The tag had become a by-product of deploying.
#
# Now the tag IS the decision. This script is deliberately tiny and dependency-free so the
# release rules can be tested without docker, without a network and without a build
# (deploy/release_selftest.sh drives it).
#
# Usage:
#   deploy/require-release-tag.sh          # prints the tag on HEAD
#   VERSION_OVERRIDE=v1.2.3 deploy/require-release-tag.sh
#
# Exit: 0 a tag is on HEAD (printed on stdout, nothing else)
#       4 HEAD carries no tag — nothing to deploy; cut a release first
#       5 VERSION_OVERRIDE was given and does not match the tag on HEAD
set -o pipefail
PROG=${0##*/}
err() { printf '%s: %s\n' "$PROG" "$*" >&2; }

TAG=$(git describe --tags --exact-match HEAD 2>/dev/null || true)

if [ -n "${VERSION_OVERRIDE:-}" ]; then
  if [ -z "$TAG" ] || [ "$VERSION_OVERRIDE" != "$TAG" ]; then
    err "VERSION_OVERRIDE=$VERSION_OVERRIDE does not match the tag on HEAD (${TAG:-none})"
    err "a release is a TAG, not an override — the override may only restate the tag"
    exit 5
  fi
  printf '%s\n' "$TAG"
  exit 0
fi

if [ -z "$TAG" ]; then
  err "HEAD $(git rev-parse --short HEAD 2>/dev/null || echo '?') carries no tag — refusing to deploy an unreleased tree"
  err "  a deploy ships a tagged RELEASE; cut one with: ./deploy/cut-release.sh --yes"
  exit 4
fi

printf '%s\n' "$TAG"
