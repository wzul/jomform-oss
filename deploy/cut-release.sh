#!/usr/bin/env bash
# cut-release — ONE tag per RELEASE, not one tag per deploy.
#
# Why this exists: every deploy used to tag its own version, because the deploy lane's
# rule was "EVERY deploy tags a version. No exceptions". Measured on 2026-11-07:
# 806 tags on 1,341 commits of main, 102 tags in the previous seven days, ~15 deploys a
# day. A tag therefore recorded "we shipped something", never "this is a release", and the
# public mirror (wzul/jomform-oss) had no released version to point at — its last sync was
# 2026-11-02, ~90 PRs behind.
#
# A release here is a DECISION with an order that cannot be reordered safely:
#   1. tag origin/main                      (the decision, and the record of it)
#   2. publish the GitHub release           (notes = what merged since the last tag)
#   3. ship THAT TREE                       (build-and-ship.sh, full inventory)
#   4. mirror the released tree to jomform-oss
# Nothing public points at a tree that did not ship: the mirror is step 4, and it is only
# reached once the ship's own verification passed.
#
# Usage:
#   deploy/cut-release.sh --dry-run               # print the plan and the version, change nothing
#   deploy/cut-release.sh --print-next            # just the version the next release would carry
#   deploy/cut-release.sh --yes                   # cut the next MINOR release, ship, mirror
#   deploy/cut-release.sh --yes --patch           # hotfix: bump the patch instead
#   deploy/cut-release.sh --yes --no-ship         # tag + publish only (ship separately later)
#   --no-oss          release without mirroring the public repo
#   --api-only        skip the web image even when the release touched web/
#   deploy/cut-release.sh --yes --ship-only       # retry steps 3–4 for an EXISTING tag
#   deploy/cut-release.sh --yes --oss-only        # retry step 4 for an existing tag
#
#   --tag <vX.Y.Z>    cut THIS version instead of the next one — the repair path for a tag
#                     that exists but never shipped (delete the ref, then re-cut it here, so
#                     every tag is still created by one script and the move stays auditable)
#   --from <ref>      what to release (default: origin/main)
#   --notes-file <f>  release notes body (default: generated from the commits)
#   --minor           default: bump the minor (v1.23.12 -> v1.24.0)
#   --patch           bump the patch (v1.23.12 -> v1.23.13); the hotfix path
#
# Exit: 0 released / plan printed · 2 usage · 3 nothing to release
#       4 the next version is already tagged · 5 the ship failed (the tag REMAINS — it is
#       the record of the decision; fix and re-run with --ship-only) · 6 the mirror failed
#       (the release stands; re-run with --oss-only)
set -o pipefail

PROG=${0##*/}
die()  { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say()  { printf '%s: %s\n' "$PROG" "$*"; }
warn() { printf '%s: %s\n' "$PROG" "$*" >&2; }

DRY=0; YES=0; KIND=minor; FROM=origin/main; WANT_TAG=
SHIP=1; OSS=1; SHIP_ONLY=0; OSS_ONLY=0; PRINT_NEXT=0; NOTES_FILE=""; API_ONLY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)   DRY=1; shift ;;
    --yes|-y)    YES=1; shift ;;
    --minor)     KIND=minor; shift ;;
    --patch)     KIND=patch; shift ;;
    --no-ship)   SHIP=0; shift ;;
    --no-oss)    OSS=0; shift ;;
    --api-only)  API_ONLY=1; shift ;;
    --ship-only) SHIP_ONLY=1; shift ;;
    --oss-only)  OSS_ONLY=1; OSS=1; SHIP=0; shift ;;
    --print-next) PRINT_NEXT=1; shift ;;
    --from)      FROM=${2:?--from needs a ref}; shift 2 ;;
    --tag)       WANT_TAG=${2:?--tag needs a version}; shift 2 ;;
    --notes-file) NOTES_FILE=${2:?--notes-file needs a path}; shift 2 ;;
    -h|--help)   sed -n '2,45p' "$0"; exit 0 ;;
    -*)          die "unknown flag: $1" ;;
    *)           die "unexpected argument: $1" ;;
  esac
done

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || die "not in a git repo"
cd "$ROOT" || die "cannot cd $ROOT"
[ -n "$(git status --porcelain)" ] && die "working tree is dirty — a release is cut from a clean tree"

# bump_version: last tag + kind -> next tag. Pure arithmetic, so it is unit-tested.
bump_version() {   # $1=last tag (or 'none'), $2=minor|patch
  local last=$1 kind=$2 maj min pat
  if [ "$last" = none ]; then printf '%s\n' "v1.0.0"; return 0; fi
  last=${last#v}
  IFS=. read -r maj min pat <<<"$last"
  case "$maj$min${pat:-x}" in *[!0-9]*) printf '%s\n' "BAD:$1"; return 1 ;; esac
  case $kind in
    minor) maj=$maj; min=$((min+1)); pat=0 ;;
    patch) pat=${pat:-0}; pat=$((pat+1)) ;;
    *)     printf '%s\n' "BAD:$2"; return 1 ;;
  esac
  printf 'v%s.%s.%s\n' "$maj" "$min" "$pat"
}

# --force matters: a tag that was moved on the remote (the repair path does exactly that) makes a
# plain `fetch --tags` abort with "would clobber existing tag" and exit 1, and a release tool that
# silently works from stale tags would cut the wrong version.
if ! git fetch -q --tags --force origin; then
  warn "git fetch failed — using the refs this checkout already has (verify the tag you are cutting)"
fi
SHA=$(git rev-parse --verify "${FROM}^{commit}") || die "cannot resolve $FROM"

LAST=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$SHA" 2>/dev/null || echo none)
if [ "$LAST" = none ]; then
  COUNT=$(git rev-list --count "$SHA")
else
  COUNT=$(git rev-list --count "$LAST..$SHA")
fi
if [ -n "$WANT_TAG" ]; then
  case "$WANT_TAG" in
    v[0-9]*.[0-9]*.[0-9]*) NEXT=$WANT_TAG ;;
    *) die "--tag '$WANT_TAG' is not vX.Y.Z" ;;
  esac
else
  NEXT=$(bump_version "$LAST" "$KIND") || die "cannot compute the next version from tag '$LAST'"
fi

if [ "$PRINT_NEXT" = 1 ]; then
  printf '%s\n' "$NEXT"
  exit 0
fi

say "from $FROM ($(git rev-parse --short "$SHA"))"
say "last release: $LAST · commits since: $COUNT · kind: $KIND"
say "next release: $NEXT"

# ---------------------------------------------------------------- retry modes
if [ "$SHIP_ONLY" = 1 ] || [ "$OSS_ONLY" = 1 ]; then
  TAG=$(git describe --tags --exact-match "$SHA" 2>/dev/null || true)
  [ -n "$TAG" ] || die "--ship-only/--oss-only need an already-tagged $FROM (tag the tree first)"
  [ "$SHIP_ONLY" = 1 ] && SHIP=1
  [ "$OSS_ONLY" = 1 ] && { SHIP=0; }
  say "retrying for existing tag $TAG (ship=$SHIP oss=$OSS)"
  NEXT=$TAG
  COUNT=$(git rev-list --count "${LAST}..$SHA" 2>/dev/null || echo 0)
else
  if [ "$COUNT" = 0 ]; then
    say "nothing to release — $FROM has no commit after $LAST"
    exit 3
  fi
  if git rev-parse --verify --quiet "refs/tags/$NEXT" >/dev/null; then
    say "refusing: tag $NEXT already exists (nothing was tagged or shipped)"
    exit 4
  fi
fi

# Does the release touch web/? The API image build is the default, so a release that changes the
# frontend owes a SECOND image — missing it ships an API whose server no longer matches the web
# bundle it serves.
# Diffs the commit being released ($SHA), NOT $NEXT: on a dry run the tag does not exist yet.
touches_web() {
  if [ "$LAST" = none ]; then
    git diff --name-only "$SHA" 2>/dev/null | grep -q '^web/'
  else
    git diff --name-only "$LAST..$SHA" 2>/dev/null | grep -q '^web/'
  fi
}

# ---------------------------------------------------------------- the plan
emit_notes() {
  if [ -n "$NOTES_FILE" ]; then cat "$NOTES_FILE"; return; fi
  printf 'Release %s\n\n' "$NEXT"
  printf 'Commits since %s (%s):\n\n' "$LAST" "$COUNT"
  if [ "$LAST" = none ]; then RANGE=$SHA; else RANGE="$LAST..$SHA"; fi
  git log --no-merges --format='- %s (%h)' "$RANGE" | head -200
}

if [ "$DRY" = 1 ]; then
  say "DRY RUN — nothing tagged, nothing published, nothing shipped, nothing mirrored"
  say "would run:"
  say "  1. git tag -a $NEXT $SHA && git push origin $NEXT"
  say "  2. gh release create $NEXT --title $NEXT --notes <generated>"
  if [ "$SHIP" = 1 ]; then
    say "  3. worktree @ $NEXT -> ./deploy/build-and-ship.sh  (FULL inventory, tagged tree)"
    if [ "$API_ONLY" = 0 ] && touches_web; then
      say "  3b. IMAGE=jomform-web -> ./deploy/build-and-ship.sh  (the release touches web/)"
    fi
  fi
  [ "$OSS" = 1 ] && say "  4. ./deploy/sync-oss.sh --tag $NEXT --yes"
  say "notes preview (first 12 lines):"
  emit_notes | head -12 | sed 's/^/    | /'
  exit 0
fi

[ "$YES" = 1 ] || die "refusing to release without --yes (re-run with --dry-run to see the plan)"

# ---------------------------------------------------------------- 1. the tag
if [ "$SHIP_ONLY" = 0 ] && [ "$OSS_ONLY" = 0 ]; then
  NOTES_TMP=$(mktemp "${TMPDIR:-/tmp}/release-notes.XXXXXX") || die "mktemp failed"
  trap 'rm -f "$NOTES_TMP"' EXIT
  emit_notes >"$NOTES_TMP"
  git tag -a "$NEXT" "$SHA" -m "Release $NEXT" || die "git tag failed"
  git push origin "$NEXT" || die "git push of tag $NEXT failed — the tag exists locally only; push it, then --ship-only"
  say "tagged and pushed $NEXT"
  # ------------------------------------------------------------- 2. the release
  if command -v gh >/dev/null 2>&1; then
    gh release create "$NEXT" --title "$NEXT" --notes-file "$NOTES_TMP" \
      || warn "gh release create failed — the tag stands; publish the release by hand"
    say "published GitHub release $NEXT"
  else
    warn "gh not found — the tag stands, but no GitHub release was published"
  fi
else
  NOTES_TMP=""
fi

# ---------------------------------------------------------------- 3. the ship
WT=""
cleanup_wt() { [ -n "$WT" ] && git worktree remove --force "$WT" >/dev/null 2>&1; }
trap 'cleanup_wt; [ -n "${NOTES_TMP:-}" ] && rm -f "$NOTES_TMP"' EXIT

if [ "$SHIP" = 1 ]; then
  WT=$(mktemp -d "${TMPDIR:-/tmp}/release-tree.XXXXXX")/tree
  git worktree add -q --detach "$WT" "$NEXT" || die "cannot create the release worktree at $NEXT"
  say "shipping the tagged tree in $WT (full inventory — this is the release gate)"
  if ! ( cd "$WT" && VERSION_OVERRIDE="$NEXT" ./deploy/build-and-ship.sh ); then
    warn "SHIP FAILED for $NEXT — the tag and release REMAIN (they are the record of the decision)"
    warn "  fix the tree, re-cut with --tag $NEXT after deleting the ref, or re-run: $PROG --yes --ship-only"
    exit 5
  fi
  say "shipped the API image for $NEXT"
  if [ "$API_ONLY" = 0 ] && touches_web; then
    say "the release touches web/ — shipping the web image too"
    if ! ( cd "$WT" && IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile \
             VERSION_OVERRIDE="$NEXT" ./deploy/build-and-ship.sh ); then
      warn "WEB SHIP FAILED for $NEXT — the API image is already on prod under this tag"
      warn "  fix the tree, re-cut with --tag $NEXT after deleting the ref, or re-run: $PROG --yes --ship-only"
      exit 5
    fi
    say "shipped the web image for $NEXT"
  fi
else
  say "skipping the ship (--no-ship/--oss-only)"
fi

# ---------------------------------------------------------------- 4. the mirror
if [ "$OSS" = 1 ]; then
  if ! ./deploy/sync-oss.sh --tag "$NEXT" --yes; then
    warn "MIRROR FAILED for $NEXT — the release stands; re-run: $PROG --yes --oss-only"
    exit 6
  fi
  say "mirrored $NEXT to the public repo"
else
  say "skipping the public mirror (--no-oss)"
fi

say "RELEASED $NEXT"
