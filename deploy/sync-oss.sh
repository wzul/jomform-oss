#!/usr/bin/env bash
# sync-oss — the public mirror (wzul/jomform-oss) carries RELEASED code, never a head.
#
# Before this existed the mirror was a manual code-drop: last synced 2026-11-02 at #1535,
# ~90 PRs behind main, with no workflow and no script anywhere that reproduced it. The
# answer to "does every new feature land in the public repo?" was therefore "only when
# somebody remembers", and the public tree stopped meaning anything in particular.
#
# Now the mirror is a RELEASE artifact: it is step 4 of deploy/cut-release.sh, so the
# public repo always equals a tagged release, and its commits say which one.
#
# Usage:
#   deploy/sync-oss.sh --tag v1.25.0 --dry-run     # show what would change, push nothing
#   deploy/sync-oss.sh --tag v1.25.0 --yes         # mirror the released tree
#
#   --tag <t>       the released tag to mirror (required; must exist here AND be pushed)
#   --repo-url <u>  mirror remote (default: $OSS_URL, else the OSS_URL default below)
#   --dir <p>       reuse this mirror clone instead of a temp one (must already exist)
#   --keep          keep the temporary mirror clone and print its path
#
# Exit: 0 mirrored / plan printed · 2 usage · 4 the tag is missing or not pushed
#       6 rsync/repo failure, or the released tree carries a secret-scanner-shaped
#         literal (GitHub push protection would refuse the push)
#       7 the mirror already matches (nothing to do)
set -o pipefail

PROG=${0##*/}
die()  { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say()  { printf '%s: %s\n' "$PROG" "$*"; }
warn() { printf '%s: %s\n' "$PROG" "$*" >&2; }

TAG=""; DRY=0; YES=0; KEEP=0; DIR=""
OSS_URL_DEFAULT="git@github.com:wzul/jomform-oss.git"
while [ $# -gt 0 ]; do
  case "$1" in
    --tag)       TAG=${2:?--tag needs a value}; shift 2 ;;
    --dry-run)   DRY=1; shift ;;
    --yes|-y)    YES=1; shift ;;
    --keep)      KEEP=1; shift ;;
    --dir)       DIR=${2:?--dir needs a path}; shift 2 ;;
    --repo-url)  OSS_URL=${2:?--repo-url needs a value}; shift 2 ;;
    -h|--help)   sed -n '2,22p' "$0"; exit 0 ;;
    *)           die "unknown argument: $1" ;;
  esac
done

[ -n "$TAG" ] || die "--tag is required (which release to mirror?)"
URL=${OSS_URL:-$OSS_URL_DEFAULT}

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || die "not in a git repo"
cd "$ROOT" || die "cannot cd $ROOT"

git rev-parse --verify --quiet "refs/tags/$TAG" >/dev/null || die "tag $TAG does not exist here"
if ! git ls-remote --exit-code origin "refs/tags/$TAG" >/dev/null 2>&1; then
  die "tag $TAG is not pushed to origin — the mirror must only carry published releases"
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/sync-oss.XXXXXX") || die "mktemp failed"
SNAP=$WORK/snapshot
MIRROR=${DIR:-$WORK/mirror}
mkdir -p "$SNAP"
cleanup() {
  [ "$KEEP" = 1 ] && { say "kept: snapshot=$SNAP mirror=$MIRROR"; return; }
  rm -rf "$WORK"
}
trap cleanup EXIT

git archive --format=tar "$TAG" | tar -x -C "$SNAP" || die "cannot export the tree of $TAG"
FILES=$(cd "$SNAP" && find . -type f | wc -l | tr -d ' ')
say "snapshot of $TAG: $FILES file(s)"

if [ -d "$MIRROR/.git" ]; then
  git -C "$MIRROR" fetch -q origin || die "cannot fetch $URL"
  git -C "$MIRROR" checkout -q -B main origin/main 2>/dev/null \
    || git -C "$MIRROR" checkout -q -B main
else
  git clone -q --depth 1 "$URL" "$MIRROR" || die "cannot clone $URL"
fi

# What would change. `--delete` is deliberate: the mirror is a MIRROR of the released tree,
# so a file removed upstream must disappear here too. The dry run lists those deletions
# before anything is pushed, because they are the one destructive part of this script.
DIFF=$(rsync -a --delete --dry-run --itemize-changes --exclude '.git' "$SNAP"/ "$MIRROR"/ 2>&1) \
  || die "rsync dry-run failed"
CHANGED=$(printf '%s\n' "$DIFF" | grep -c '^[<>*]' || true)
DELETED=$(printf '%s\n' "$DIFF" | grep -c '^\*deleting' || true)
say "changes vs the mirror: $CHANGED path(s), $DELETED deletion(s)"
if [ "$CHANGED" != 0 ]; then
  printf '%s\n' "$DIFF" | grep -E '^\*deleting|^[<>]' | head -20 | sed 's/^/    /'
  [ "$CHANGED" -gt 20 ] && say "    … $(($CHANGED-20)) more"
fi

if [ "$CHANGED" = 0 ]; then
  say "the mirror already matches $TAG — nothing to do"
  exit 7
fi

# ---------------------------------------------------- push protection, locally
# GitHub refuses a push whose content matches a provider secret pattern, even when the
# value is a synthetic fixture, and push protection has no path allowlist: the repo's own
# gitleaks gate allowlists `api/internal/.*_test\.go`, so a test fixture that is invisible
# locally can still stop the mirror. Worse, that refusal arrives at step 4 of the release,
# AFTER the deploy has shipped, and leaves the mirror stale (t_1a11c700: a `sk_live_`
# fixture in api/internal/cryptoutil/mask_test.go blocked the v1.25.0 mirror).
#
# Scanning the released tree turns that into a local, pre-push failure that names the file.
# It is deliberately stricter than push protection — a whole-tree scan, not only what this
# push adds — because the mirror must never carry such a literal at all. The 20-character
# floor is a floor, not GitHub's (undocumented) minimum: it sits below the 48 that was
# actually refused, and `sk_live_` followed by 20 alphanumerics is a shape this repo
# otherwise carries only in a test fixture.
SECRET_SHAPED='(sk|rk)_(live|test)_[A-Za-z0-9]{20,}'
HITS=$(grep -rIlE "$SECRET_SHAPED" "$SNAP" 2>/dev/null || true)
if [ -n "$HITS" ]; then
  warn "$TAG carries a key-shaped literal, which GitHub push protection refuses:"
  printf '%s\n' "$HITS" | sed "s|^$SNAP/*|    |" >&2
  warn "build such a fixture at run time (a prefix plus strings.Repeat(...)) instead of as one literal"
  exit 6
fi

if [ "$DRY" = 1 ]; then
  say "DRY RUN — nothing pushed. Would commit 'Release $TAG' and push to $URL"
  exit 0
fi
[ "$YES" = 1 ] || die "refusing to push without --yes (re-run with --dry-run to see the change list)"

rsync -a --delete --exclude '.git' "$SNAP"/ "$MIRROR"/ || die "rsync failed"
git -C "$MIRROR" add -A || die "git add failed in the mirror"
if git -C "$MIRROR" diff --cached --quiet; then
  say "the mirror already matches $TAG — nothing to commit"
  exit 7
fi
git -C "$MIRROR" -c user.name="Wan Zulkarnain" -c user.email="wanzulkarnain69@gmail.com" \
  commit -q -m "Release $TAG" -m "Mirrored from the private repo at tag $TAG (released tree)." \
  || die "commit failed in the mirror"
git -C "$MIRROR" rev-parse --verify --quiet "refs/tags/$TAG" >/dev/null \
  || git -C "$MIRROR" tag -a "$TAG" -m "Release $TAG" || warn "cannot tag the mirror $TAG"
git -C "$MIRROR" push -q origin HEAD:main || die "push to $URL failed"
git -C "$MIRROR" push -q origin "refs/tags/$TAG" || warn "could not push the mirror tag $TAG"

MIRROR_SHA=$(git -C "$MIRROR" rev-parse --short HEAD)
say "mirrored $TAG -> $URL @ $MIRROR_SHA ('Release $TAG')"
