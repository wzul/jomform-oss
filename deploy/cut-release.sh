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
#   3. ship THAT TREE                       (build-and-ship.sh --ship, full inventory)
#   4. mirror the released tree to jomform-oss
# Nothing public points at a tree that did not ship: the mirror is step 4, and it is only
# reached once the ship's own verification passed.
#
# STEP 3 AND STEP 4 ARE ENFORCED, NOT DOCUMENTED. Measured on v1.27.3 (2026-11-08): this
# script returned 0 and printed "shipped the API image", "shipped the web image",
# "mirrored v1.27.3", "RELEASED v1.27.3" while build-and-ship had only said
# "SSH_HOST not set — skipping scp". Prod still ran v1.27.1, no v1.27.3 image existed on the
# host, and the public mirror advertised a release prod had never received. The failure was
# reachable by doing nothing wrong: nothing in this environment sets SSH_HOST.
#   * With SHIP=1 the run REFUSES (exit 7, before the tag exists) unless it can reach a
#     server — SSH_HOST, or REMOTE_BUILD_HOST to build there. It never tags a release it
#     cannot ship.
#   * The ship is run with REQUIRE_SHIP=1, so build-and-ship performs scp+load+tag+recreate
#     itself and refuses (exit 7) if it cannot prove the running container carries the tag.
#   * After EACH image this script asks the server again (deploy/ship_proof.sh) and exits 5
#     if the running service does not carry $NEXT. A child's exit code is a claim; this is
#     the evidence.
#   * The mirror (step 4) is unreachable unless that verification passed. `--oss-only`
#     re-verifies the tag on prod before it mirrors, because the invariant is about the
#     TREE, not about which flag was used.
#
# Usage:
#   deploy/cut-release.sh --dry-run               # print the plan and the version, change nothing
#   deploy/cut-release.sh --print-next            # just the version the next release would carry
#   deploy/cut-release.sh --yes                   # cut the next MINOR release, ship, mirror
#   deploy/cut-release.sh --yes --patch           # hotfix: bump the patch instead
#   deploy/cut-release.sh --yes --no-ship         # tag + publish only (ship separately later;
#                                                 # needs --no-oss: nothing unshipped is mirrored)
#   --no-oss          release without mirroring the public repo
#   --api-only        skip the web image even when the release touched web/
#   deploy/cut-release.sh --yes --ship-only       # retry steps 3–4 for an EXISTING tag
#   deploy/cut-release.sh --yes --oss-only        # retry step 4 for an existing tag (proves
#                                                 # the tag is live on prod first)
#
#   --tag-only        record the tag WITHOUT publishing a GitHub release, shipping or
#                     mirroring. This is the honest way to mark a tooling milestone: it
#                     leaves GitHub's "Latest" badge on the version prod actually runs.
#   --prerelease      publish the GitHub release as a PRE-release (a prerelease never takes
#                     the "Latest" badge either)
#   --force           cut even when the diff owes no image (a deliberate product decision —
#                     it publishes a normal, Latest-taking release; prefer --tag-only)
#
#   --tag <vX.Y.Z>    cut THIS version instead of the next one — the repair path for a tag
#                     that exists but never shipped (delete the ref, then re-cut it here, so
#                     every tag is still created by one script and the move stays auditable)
#   --from <ref>      what to release (default: origin/main)
#   --notes-file <f>  release notes body (default: the generated changelog)
#   --minor           default: bump the minor (v1.23.12 -> v1.24.0)
#   --patch           bump the patch (v1.23.12 -> v1.23.13); the hotfix path
#
# A RELEASE MUST OWE AN IMAGE. Before anything is tagged the diff is classified by path
# (deploy/release_scope.sh, the same rule release-due.sh files cards with): api/**, web/**,
# MCP-PARITY.md and user-visible docs under docs/guides/** owe an image; deploy/, CI,
# scripts and repo docs owe nothing. When nothing is owed this script REFUSES (exit 3)
# rather than publishing a tag with no image behind it — which is what v1.27.4 did, taking
# the "Latest" badge off the version prod was running. The commits are not lost: they ride
# along with the next release that does owe an image.
#
# Exit: 0 released / plan printed · 2 usage · 3 nothing to release (no commits, or the diff
#       owes no image) · 4 the next version is already tagged · 5 the ship failed (the tag
#       REMAINS — it is the record of the decision; fix and re-run with --ship-only) · 6 the
#       mirror failed (the release stands; re-run with --oss-only) · 7 the run cannot reach a
#       server, so it refuses to tag or mirror an unshippable release (nothing was tagged)
#
# WHAT IT GUARANTEES ABOUT THE TREE IT SHIPS
#   The ship runs in a DETACHED worktree pinned to the tag, so it is exactly the tagged
#   bytes. That worktree has no web/node_modules (gitignored), so cut-release gives it
#   one with deploy/web_modules.sh — the SAME seeder deploy/merge-train.sh uses —
#   preferring the dependency set the per-tree gate record names. That is what lets a
#   tree the merge train already gated be REUSED here instead of gated again, and it is
#   why --ship-only is a real recovery: it re-seeds and retries the same tag.
set -o pipefail

PROG=${0##*/}
die()  { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say()  { printf '%s: %s\n' "$PROG" "$*"; }
warn() { printf '%s: %s\n' "$PROG" "$*" >&2; }

DRY=0; YES=0; KIND=minor; FROM=origin/main; WANT_TAG=
SHIP=1; OSS=1; SHIP_ONLY=0; OSS_ONLY=0; PRINT_NEXT=0; NOTES_FILE=""; API_ONLY=0
TAG_ONLY=0; PRERELEASE=0; FORCE=0
# Whether a tree actually reached prod in THIS run (and the web half of it). The mirror is
# gated on these, not on a flag: see section 4.
SHIPPED=0; WEB_SHIPPED=0
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
    --tag-only)  TAG_ONLY=1; shift ;;
    --prerelease) PRERELEASE=1; shift ;;
    --force)     FORCE=1; shift ;;
    --from)      FROM=${2:?--from needs a ref}; shift 2 ;;
    --tag)       WANT_TAG=${2:?--tag needs a version}; shift 2 ;;
    --notes-file) NOTES_FILE=${2:?--notes-file needs a path}; shift 2 ;;
    -h|--help)   sed -n '2,/^set -o pipefail/p' "$0" | sed '$d'; exit 0 ;;
    -*)          die "unknown flag: $1" ;;
    *)           die "unexpected argument: $1" ;;
  esac
done

# --tag-only means "tag and push ONLY": no GitHub release to take the Latest badge, and no
# ship or mirror, because a release that publishes nothing owes no artefact. Saying it once
# here keeps the three skips from drifting apart.
if [ "$TAG_ONLY" = 1 ]; then
  SHIP=0; OSS=0
fi
if [ "$TAG_ONLY" = 1 ] && [ "$PRERELEASE" = 1 ]; then
  die "--tag-only publishes no GitHub release, so --prerelease has nothing to mark; pick one"
fi

# The scope rule is shared with release-due.sh (deploy/release_scope.sh) so the card filer
# and the cutter cannot disagree about whether these bytes are releasable. Resolved next to
# THIS script: --from may name another checkout, but the rule is the running tool's.
SELF_DIR=$(cd "$(dirname "$0")" && pwd) || die "cannot resolve the script's directory"
SCOPE_LIB="$SELF_DIR/release_scope.sh"
[ -r "$SCOPE_LIB" ] || die "deploy/release_scope.sh is missing next to $PROG — this script cannot tell a diff that owes an image from one that does not, so it refuses to release"
# shellcheck source=deploy/release_scope.sh
. "$SCOPE_LIB"


ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || die "not in a git repo"
cd "$ROOT" || die "cannot cd $ROOT"
[ -n "$(git status --porcelain)" ] && die "working tree is dirty — a release is cut from a clean tree"

# THE SHIP PROOF IS SHARED WITH build-and-ship.sh (deploy/ship_proof.sh). One definition of
# "the tag is live on the server" is what stops the ship and the mirror from disagreeing
# about whether a tree shipped — the v1.27.3 failure was exactly that disagreement: the ship
# said nothing, this script said "shipped", and the mirror believed the script.
SHIP_PROOF_LIB="$ROOT/deploy/ship_proof.sh"
if [ -r "$SHIP_PROOF_LIB" ]; then
  # shellcheck source=deploy/ship_proof.sh
  . "$SHIP_PROOF_LIB"
fi

# THE RELEASE TREE IS DETACHED AT THE TAG, SO IT ARRIVES WITHOUT web/node_modules.
# (gitignored, so `git worktree add` cannot bring it.) Both halves of step 3
# depend on one existing:
#   * the web-side gates REFUSE a tree without it rather than reporting a build
#     that never happened — "web/node_modules/.bin/next is missing — 'npm run
#     build' would exit 0 with 'sh: 1: next: not found' and build NOTHING";
#   * the per-tree gate record's `webdeps` component is the sha256 of
#     web/node_modules/.package-lock.json, so an unseeded tree reads
#     `webdeps=none` and can NEVER reuse a record the merge train wrote for the
#     same bytes — it pays the whole inventory again.
# deploy/web_modules.sh is the SAME seeder deploy/merge-train.sh uses, not a
# second implementation of it: it prefers a copy whose webdeps is exactly what the
# record names (that is what keeps the reuse), then one installed from the same
# web/package-lock.json, then `npm ci`. `--ship-only` goes through it too, which
# is what makes the documented retry path an actual recovery.
#
# It is loaded from THE TREE BEING SHIPPED, falling back to this checkout: a
# `--ship-only` of an OLD tag (the v1.27.0 repair, for instance) is a tag whose
# tree predates this file, and the retry must still work.

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

  # ------------------------------------------------------- precondition: the diff OWES something
  # The question the commit count cannot answer, and the one that was never asked: does this
  # diff owe prod an image? v1.27.4 was six deploy/ files, shipped no image, and still
  # published a GitHub release — which took the "Latest" badge off v1.27.3, the version prod
  # was actually running. So a window that owes nothing is REFUSED here, before the tag
  # exists, with the commits kept for the next release that does owe an image.
  #
  # --tag-only / --prerelease are the sanctioned exceptions: neither takes the Latest badge,
  # so neither can mislead anyone about what prod runs. --force is the deliberate product
  # decision (it publishes a normal release) and must be typed on purpose.
  SCOPE=$(rs_scope "$LAST" "$SHA")
  SCOPE_SUMMARY=$(rs_scope_summary "$LAST" "$SHA")
  if [ "$SCOPE" != image ] && [ "$TAG_ONLY" != 1 ] && [ "$PRERELEASE" != 1 ] && [ "$FORCE" != 1 ]; then
    say "nothing to release: the diff ships no image"
    say "  $COUNT commit(s) since $LAST touch only ${SCOPE_SUMMARY:-nothing}: deploy/, CI and"
    say "  tooling do not move an image, so this window is not a release."
    say "  They are not lost — they ride along with the next release that does owe an image,"
    say "  and its notes roll them into one \"Internal\" line."
    say "  A tag here would still take GitHub's \"Latest\" badge with no image behind it"
    say "  (v1.25.0, v1.25.1, v1.27.2 and v1.27.4 did exactly that). Do one of:"
    say "    $PROG --yes --tag-only     # record the tag: no GitHub release, no ship, no mirror"
    say "    $PROG --yes --prerelease   # publish it as a prerelease (never takes Latest)"
    say "    $PROG --yes --force        # deliberate product decision: publish normally"
    exit 3
  fi
fi

# Does the release touch web/? The API image build is the default, so a release that changes the
# frontend owes a SECOND image — missing it ships an API whose server no longer matches the web
# bundle it serves.
# Diffs the commit being released ($SHA), NOT $NEXT: on a dry run the tag does not exist yet.
#
# THE RANGE IS MEASURED FROM THE TAG BEFORE THE RELEASE, and on the first cut that is
# exactly $LAST. On `--ship-only` (and on any re-run once the tag exists) `git describe
# --abbrev=0 "$SHA"` names the tag ON $SHA, so `$LAST..$SHA` is EMPTY and nothing looks
# like a web change: a retry of a web-touching release then ships the API image only and
# reports success, leaving prod on an API whose bundle the web container does not match.
# That is the recovery path this script documents, so it is measured against the PARENT's
# last tag instead — the same range the first cut used.
#
# "Touches web" is answered by the SAME rule that decides whether the diff owes anything
# (deploy/release_scope.sh): web/**, MCP-PARITY.md and docs/guides/** all reach the web
# image — the guides are materialised into web/src/lib/guides.gen.ts + web/public by
# web/scripts/guides.mjs, and MCP-PARITY.md is copied into the web build context.
scope_since() {   # the ref the released diff is measured FROM
  local since=$LAST
  if [ "$since" != none ] \
     && [ "$(git rev-parse --verify --quiet "refs/tags/$since^{commit}" 2>/dev/null)" = "$SHA" ]; then
    since=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$SHA^" 2>/dev/null || echo none)
  fi
  printf '%s\n' "$since"
}

touches_web() {
  rs_image_targets "$(scope_since)" "$SHA" | grep -qx 'web'
}

# ---------------------------------------------------------------- the notes
# A RELEASE NOTE IS A CHANGELOG, NOT A BUILD LOG. The old body dumped the diff and the
# commit subjects under it, so a reader could not tell a fix they care about from plumbing,
# and a release that changed nothing user-visible still looked busy. This is the product
# record instead: a dated title, aggregate stats, then one human line per user-visible
# change grouped by AREA, each carrying the PR that shipped it. Plumbing is rolled into a
# single "Internal" line (and dropped entirely when every commit in the window is plumbing),
# so it can never dominate the note.
#
# Everything is derived from the commits themselves (subject + touched paths), never from a
# hand-maintained list, so it cannot rot into a stale "what changed" page.

# The area a conventional-commit SCOPE belongs to. Unknown scopes are capitalised rather
# than dropped: an area nobody mapped is still a better heading than "misc".
notes_scope_area() {
  local s=${1:-}
  s=${s,,}
  case "$s" in
    '')                                     printf '\n' ;;
    form|forms|builder|field|fields|submission|submissions)
                                            printf 'Forms\n' ;;
    payment|payments|sale|sales|checkout|chip|billing|subscription|subscriptions|refund|pricing|invoice|invoices)
                                            printf 'Payments\n' ;;
    domain|domains|dns|ssl)                 printf 'Domains\n' ;;
    chat|ai|assistant|agent)                printf 'Chat\n' ;;
    mcp|tool|tools)                         printf 'MCP\n' ;;
    qr|menu|menus|order|orders|storefront)  printf 'QR & menus\n' ;;
    template|templates|theme|themes|design) printf 'Templates\n' ;;
    space|spaces|workspace|workspaces|team|teams|member|members)
                                            printf 'Workspaces\n' ;;
    auth|account|accounts|login|signin|signup|sso|oauth|2fa|session)
                                            printf 'Accounts & sign-in\n' ;;
    email|mail|ses|notification|notifications|reminder|reminders)
                                            printf 'Email\n' ;;
    media|upload|uploads|image|images|asset|assets)
                                            printf 'Media\n' ;;
    admin|analytics|report|reports|insight|insights|stats)
                                            printf 'Admin\n' ;;
    api|openapi|webhook|webhooks|integration|integrations)
                                            printf 'API & integrations\n' ;;
    docs|doc|guide|guides|landing|site|i18n|copy)
                                            printf 'Documentation\n' ;;
    web|app|ui|frontend)                    printf 'Web app\n' ;;
    *)                                      printf '%s\n' "$(notes_title "$s")" ;;
  esac
}

notes_title() {   # "payment-fees" -> "Payment fees"
  local s=${1:-}
  s=${s//[-_.]/ }
  printf '%s%s\n' "$(printf '%s' "${s:0:1}" | tr '[:lower:]' '[:upper:]')" "${s:1}"
}

notes_plural() {  # $1 = count  $2 = singular  $3 = plural
  if [ "${1:-0}" = 1 ]; then printf '%s' "$2"; else printf '%s' "$3"; fi
}

# The area of a changed path, used when the commit has no usable scope.
notes_area_for_path() {
  case "${1:-}" in
    api/internal/*) printf '%s\n' "$(notes_title "$(printf '%s' "${1#api/internal/}" | cut -d/ -f1)")" ;;
    api/*)          printf 'API\n' ;;
    web/*)          printf 'Web app\n' ;;
    docs/guides/*)  printf 'Documentation\n' ;;
    MCP-PARITY.md)  printf 'MCP\n' ;;
    *)              printf 'Internal\n' ;;
  esac
}

# Reading order for the note. Known product areas first (the order a customer scans them in),
# unknown areas alphabetically after them, plumbing last.
notes_area_rank() {
  case "${1:-}" in
    Forms)                printf '10\n'  ;;
    Payments)             printf '20\n'  ;;
    Domains)              printf '30\n'  ;;
    Chat)                 printf '40\n'  ;;
    MCP)                  printf '50\n'  ;;
    'QR & menus')         printf '60\n'  ;;
    Templates)            printf '70\n'  ;;
    Workspaces)           printf '80\n'  ;;
    'Accounts & sign-in') printf '90\n'  ;;
    Email)                printf '100\n' ;;
    Media)                printf '110\n' ;;
    Admin)                printf '120\n' ;;
    'API & integrations') printf '130\n' ;;
    'Web app')            printf '140\n' ;;
    API)                  printf '145\n' ;;
    Documentation)        printf '150\n' ;;
    Internal)             printf '999\n' ;;
    *)                    printf '500\n' ;;
  esac
}

# notes_record <subject> <newline-separated paths> -> "rank<TAB>area<TAB>bullet<TAB>pr"
notes_record() {
  local subj=${1:-} paths=${2:-} scope='' text='' pr='' area='' p owes=0
  text=$subj
  if printf '%s\n' "$subj" | grep -qE ' \(#[0-9]+\)$'; then
    pr=$(printf '%s\n' "$subj" | sed -nE 's/.* \(#([0-9]+)\)$/\1/p')
    text=$(printf '%s\n' "$subj" | sed -E 's/ \(#[0-9]+\)$//')
  fi
  if printf '%s\n' "$text" | grep -qE '^[A-Za-z]+(\([^)]+\))?!?: '; then
    scope=$(printf '%s\n' "$text" | sed -nE 's/^[A-Za-z]+\(([^)]+)\).*/\1/p')
    text=$(printf '%s\n' "$text"  | sed -E 's/^[A-Za-z]+(\([^)]+\))?!?: //')
  fi
  # A commit is PLUMBING when none of its files can move an image. That is the scope rule
  # itself (deploy/release_scope.sh), so this grouping can never disagree with the gate.
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    [ "$(rs_owe_scope_of "$p")" = image ] && { owes=1; break; }
  done <<<"$paths"
  if [ "$owes" = 0 ]; then
    area=Internal
  else
    area=$(notes_scope_area "$scope")
    if [ -z "$area" ]; then
      while IFS= read -r p; do
        [ -n "$p" ] || continue
        if [ "$(rs_owe_scope_of "$p")" = image ]; then
          area=$(notes_area_for_path "$p")
          break
        fi
      done <<<"$paths"
    fi
  fi
  [ -n "$area" ] || area=Internal
  printf '%s\t%s\t- %s%s\t%s\n' \
    "$(notes_area_rank "$area")" "$area" \
    "$text" "$([ -n "$pr" ] && printf ' (#%s)' "$pr")" "$pr"
}

# notes_records <range> -> one notes_record line per commit
notes_records() {
  local range=$1 subj='' files='' line
  while IFS= read -r line; do
    case "$line" in
      @@*)
        [ -n "$subj" ] && notes_record "$subj" "$files"
        subj=${line#@@}
        files=''
        ;;
      '') ;;
      *)  files="$files$line"$'\n' ;;
    esac
  done < <(git log --no-merges --reverse --format='@@%s' --name-only "$range" 2>/dev/null)
  if [ -n "$subj" ]; then notes_record "$subj" "$files"; fi
}

emit_notes() {
  if [ -n "$NOTES_FILE" ]; then cat "$NOTES_FILE"; return; fi
  local range recs since commits prs areas date_line int_n int_prs images prev area rank bullet _pr
  since=$(scope_since)
  if [ "$since" = none ]; then range=$SHA; else range="$since..$SHA"; fi

  recs=$(mktemp "${TMPDIR:-/tmp}/release-recs.XXXXXX") || die "mktemp failed"
  notes_records "$range" > "$recs"

  date_line=$(LC_ALL=C date -u -d "@$(git show -s --format=%ct "$SHA")" +'%Y-%m-%d' 2>/dev/null \
              || date -u +'%Y-%m-%d')
  commits=$(git rev-list --count "$range" 2>/dev/null || echo "$COUNT")
  prs=$(git log --no-merges --format=%s "$range" 2>/dev/null \
        | grep -oE '\(#[0-9]+\)$' | sort -u | wc -l | tr -d ' ')
  areas=$(awk -F'\t' '$2 != "Internal" && $2 != "" { print $2 }' "$recs" | sort -u | wc -l | tr -d ' ')
  images=$(rs_image_targets "$since" "$SHA" | sed 's/^api$/API/; s/^web$/Web/' | paste -sd'+' - 2>/dev/null)

  printf '# %s — %s\n\n' "$NEXT" "$date_line"
  printf '**%s %s · %s %s · %s %s** since %s\n\n' \
    "$commits" "$(notes_plural "$commits" commit commits)" \
    "$prs"     "$(notes_plural "$prs" PR PRs)" \
    "$areas"   "$(notes_plural "$areas" 'product area' 'product areas')" \
    "$( [ "$since" = none ] && printf 'the first commit' || printf '%s' "$since" )"
  if [ -n "$images" ]; then
    printf 'Images owed: %s (%s).\n\n' "$(printf '%s' "$images" | sed 's/+/ + /g')" \
      "$( [ "$images" = 'API+Web' ] && printf 'the diff touches web/' || printf 'the diff moves this artefact' )"
  else
    printf 'Images owed: none — this is a tooling record, not a prod version.\n\n'
  fi

  # Product areas, in reading order, each commit one line. The 4th field (the PR number) is
  # read and dropped: `read` gives the LAST variable the rest of the line, tabs included.
  prev=''
  while IFS=$(printf '\t') read -r rank area bullet _pr; do
    [ -n "$area" ] || continue
    if [ "$area" != "$prev" ]; then
      printf '## %s\n' "$area"
      prev=$area
    fi
    printf '%s\n' "$bullet"
  done <<<"$(awk -F'\t' '$2 != "Internal" && $2 != ""' "$recs" \
             | sort -t "$(printf '\t')" -k1,1n -k2,2)"

  # Plumbing: ONE line, with the PRs, or nothing at all when the window is all plumbing —
  # a release note that leads with "deploy/ changed" is the defect this shape exists to fix.
  int_n=$(awk -F'\t' '$2 == "Internal"' "$recs" | wc -l | tr -d ' ')
  if [ "${int_n:-0}" -gt 0 ]; then
    int_prs=$(awk -F'\t' '$2 == "Internal" && $4 != "" { print $4 }' "$recs" \
              | sort -un | sed 's/^/#/' | paste -sd, - 2>/dev/null | sed 's/,/, /g')
    printf '\n## Internal\n- %s plumbing commit(s) (deploy/, CI, tests, repo docs) — no image owed' "$int_n"
    if [ -n "$int_prs" ]; then printf ' (%s)' "$int_prs"; fi
    printf '\n'
  fi

  rm -f "$recs"
}

if [ "$DRY" = 1 ]; then
  say "DRY RUN — nothing tagged, nothing published, nothing shipped, nothing mirrored"
  say "scope: ${SCOPE_SUMMARY:-nothing changed} — $([ "$SCOPE" = image ] && echo 'this diff owes an image' || echo 'no image owed')"
  say "would run:"
  say "  1. git tag -a $NEXT $SHA && git push origin $NEXT"
  if [ "$TAG_ONLY" = 1 ]; then
    say "  2. (skipped) --tag-only: no GitHub release, so \"Latest\" keeps pointing at the version prod runs"
  elif [ "$PRERELEASE" = 1 ]; then
    say "  2. gh release create $NEXT --title $NEXT --prerelease --notes <generated>"
  else
    say "  2. gh release create $NEXT --title $NEXT --notes <generated>"
  fi
  if [ "$SHIP" = 1 ]; then
    say "  3. worktree @ $NEXT -> REQUIRE_SHIP=1 ./deploy/build-and-ship.sh  (FULL inventory, tagged tree, then scp+load+tag+recreate+verify)"
    if [ "$API_ONLY" = 0 ] && touches_web; then
      say "  3b. IMAGE=jomform-web -> REQUIRE_SHIP=1 ./deploy/build-and-ship.sh  (the release touches web/)"
    fi
    # A plan that hides the refusal is a plan that lies: say it here, while it is free.
    if command -v ship_proof_target >/dev/null && ! ship_proof_target; then
      say "  !!!  THIS RUN WOULD REFUSE (exit 7): neither SSH_HOST nor REMOTE_BUILD_HOST is set,"
      say "       so the ship cannot reach prod. Nothing would be tagged, and nothing mirrored —"
      say "       a release that cannot ship is not cut."
    else
      say "      ship target: ${SP_DEST:-<none>}"
    fi
  else
    say "  3. (skipped) — $([ "$TAG_ONLY" = 1 ] && echo 'nothing is published, so nothing is shipped' || echo 'no ship for this run')"
  fi
  if [ "$OSS" = 1 ]; then
    if [ "$SHIP" = 0 ] && [ "$OSS_ONLY" = 0 ]; then
      say "  4. REFUSED: the mirror needs a shipped tree (--no-ship with the mirror is not a release)"
    else
      say "  4. ./deploy/sync-oss.sh --tag $NEXT --yes   (only reachable once the ship is verified on prod)"
    fi
  else
    say "  4. (skipped) — no mirror"
  fi
  say "release notes (the changelog that becomes the GitHub release body):"
  emit_notes | head -40 | sed 's/^/    | /'
  exit 0
fi

[ "$YES" = 1 ] || die "refusing to release without --yes (re-run with --dry-run to see the plan)"

# ------------------------------------------------- precondition: nothing unshipped is mirrored
# The header invariant — "nothing public points at a tree that did not ship" — is enforced
# here rather than merely stated. Tagging a release while skipping the ship is a legitimate
# step (--no-ship), but the MIRROR is what makes the claim public, so it cannot ride along
# with a tree nobody deployed. `--oss-only` is exempt: it is a retry of step 4 for a tag that
# is verified on prod below before anything is pushed.
if [ "$SHIP" = 0 ] && [ "$OSS" = 1 ] && [ "$OSS_ONLY" = 0 ]; then
  die "--no-ship with the mirror is refused: nothing public points at a tree that did not ship (use --no-oss to tag+publish only, or ship it and mirror after)"
fi

# ------------------------------------------------- precondition: the ship must be REACHABLE
# Checked BEFORE the tag exists, because the order in this file is tag -> publish -> ship ->
# mirror and a tag is the record of a decision. An unshippable release must not leave that
# record behind: on v1.27.3 the release was tagged, published and mirrored from a run that
# could not reach prod at all, and only a human finishing the server half by hand made the
# mirror's claim true. Exit 7, nothing tagged.
if [ "$SHIP" = 1 ] || [ "$OSS_ONLY" = 1 ]; then
  command -v ship_proof_target >/dev/null 2>&1 || die \
    "deploy/ship_proof.sh is missing from $ROOT — this script cannot tell a shipped tree from a built one, so it refuses to release"
fi
if [ "$SHIP" = 1 ]; then
  if ! ship_proof_target; then
    warn "REFUSING to cut $NEXT: this run must SHIP, but nothing can reach the server —"
    warn "  neither SSH_HOST (the deploy host) nor REMOTE_BUILD_HOST (build there) is set."
    warn "  A release that cannot reach prod must not be tagged, and its tree must never be mirrored:"
    warn "  on v1.27.3 this exact state reported RELEASED and mirrored the public repo while prod"
    warn "  stayed on the previous version and no image with that tag existed on the host."
    warn "  Do one of:"
    warn "    SSH_HOST=<deploy host> $PROG --yes [--tag $NEXT]          # ship for real, then verify"
    warn "    REMOTE_BUILD_HOST=<deploy host> $PROG --yes [--tag $NEXT] # build there, then verify"
    warn "    $PROG --yes --no-ship --no-oss                            # tag + publish ONLY (nothing shipped, nothing mirrored)"
    warn "  NOTHING was tagged."
    exit 7
  fi
  say "ship target: $SP_DEST ($SP_TARGET_KIND), compose dir $SP_COMPOSE_DIR"
fi

# ---------------------------------------------------------------- 1. the tag
if [ "$SHIP_ONLY" = 0 ] && [ "$OSS_ONLY" = 0 ]; then
  NOTES_TMP=$(mktemp "${TMPDIR:-/tmp}/release-notes.XXXXXX") || die "mktemp failed"
  trap 'rm -f "$NOTES_TMP"' EXIT
  emit_notes >"$NOTES_TMP"
  # A --tag-only record is the ONLY artefact of that decision, so the annotated tag carries
  # the changelog itself; a normal release keeps the terse "Release <tag>" annotation and
  # publishes the notes to GitHub.
  if [ "$TAG_ONLY" = 1 ]; then
    git tag -a "$NEXT" "$SHA" -F "$NOTES_TMP" || die "git tag failed"
  else
    git tag -a "$NEXT" "$SHA" -m "Release $NEXT" || die "git tag failed"
  fi
  git push origin "$NEXT" || die "git push of tag $NEXT failed — the tag exists locally only; push it, then --ship-only"
  say "tagged and pushed $NEXT"
  # ------------------------------------------------------------- 2. the release
  # --tag-only stops here: the tag is the record, and publishing anything would move
  # GitHub's "Latest" badge onto a version with no image behind it. That badge is what tells
  # a reader which bytes prod runs, so a tooling record must never take it.
  if [ "$TAG_ONLY" = 1 ]; then
    say "no GitHub release (--tag-only): \"Latest\" stays on the version prod is running"
  elif command -v gh >/dev/null 2>&1; then
    if [ "$PRERELEASE" = 1 ]; then
      gh release create "$NEXT" --title "$NEXT" --prerelease --notes-file "$NOTES_TMP" \
        || warn "gh release create failed — the tag stands; publish the release by hand"
      say "published GitHub PRE-release $NEXT (a prerelease never takes the Latest badge)"
    else
      gh release create "$NEXT" --title "$NEXT" --notes-file "$NOTES_TMP" \
        || warn "gh release create failed — the tag stands; publish the release by hand"
      say "published GitHub release $NEXT"
    fi
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

# The gate's environment fingerprint includes the TEST DATABASE (see
# scripts/gate_result.sh, gr_key_material). A record written by the merge train was
# written with the test env loaded; a release cut from a shell that never sourced it
# has `testdb=unset`, so the record is refused with "the environment changed since
# that record" — and the api gates die on the same missing variable. deploy/
# merge-train.sh sources this file for exactly this reason; the deploy lane's shell
# is not guaranteed to have done it, and the release must not depend on that.
load_test_env() {
  local f="${TEST_ENV_FILE:-${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}}"
  if [ ! -f "$f" ]; then
    warn "no test-environment file at $f — gating with this shell's environment only (JOMFORM_TEST_DATABASE_URL is ${JOMFORM_TEST_DATABASE_URL:+set}${JOMFORM_TEST_DATABASE_URL:-unset})"
    return 0
  fi
  # shellcheck disable=SC1090
  set -a; . "$f"; set +a
  say "test environment: $f"
}

if [ "$SHIP" = 1 ]; then
  WT=$(mktemp -d "${TMPDIR:-/tmp}/release-tree.XXXXXX")/tree
  git worktree add -q --detach "$WT" "$NEXT" || die "cannot create the release worktree at $NEXT"
  say "shipping the tagged tree in $WT (full inventory — this is the release gate)"
  load_test_env

  # The seeder comes from the tree being shipped when it is there (so a release runs
  # the tag's own code), and from this checkout otherwise — an old tag re-shipped
  # with --ship-only predates deploy/web_modules.sh.
  WM_LIB="$WT/deploy/web_modules.sh"
  [ -r "$WM_LIB" ] || WM_LIB="$ROOT/deploy/web_modules.sh"
  if [ ! -r "$WM_LIB" ]; then
    warn "SHIP NOT STARTED for $NEXT — no deploy/web_modules.sh in the tagged tree ($WT) or in"
    warn "  $ROOT, so the release tree cannot be given a web/node_modules and the web-side"
    warn "  gates would refuse it. NOTHING was built. The tag and release REMAIN."
    exit 5
  fi
  # shellcheck source=deploy/web_modules.sh
  . "$WM_LIB"

  # Give the release tree its web/node_modules BEFORE the ship reads the gate
  # record. `want` is the webdeps the per-tree record for THIS tree names — the
  # bytes that make the record reusable — and it is empty when there is no record
  # (then the seed just makes the web gates runnable).
  TREE=$(git -C "$WT" rev-parse 'HEAD^{tree}')
  WANT=$(wm_record_webdeps "$TREE")
  WM_PROG="$PROG"
  WM_REPO="$ROOT"
  WM_LOCK_CACHE="${JOMFORM_WEB_MODULES_CACHE:-/var/lib/jomform/web-modules}"
  say "release tree $TREE${WANT:+ — a gate record for it names webdeps ${WANT:0:12}}"
  if ! wm_seed "$WT" "$WANT"; then
    warn "SHIP NOT STARTED for $NEXT — the release tree could not be given a web/node_modules, and"
    warn "  the web-side gates REFUSE an unseeded tree rather than reporting a build they did not run."
    warn "  NOTHING was built. The tag and release REMAIN (they are the record of the decision)."
    warn "  Point the seed at an existing install and retry:"
    warn "    RELEASE_NODE_MODULES=<a worktree>/web/node_modules $PROG --yes --ship-only"
    exit 5
  fi
  if [ -n "$WANT" ] && [ "$WM_MATCHED" = 1 ]; then
    say "web deps match the gate record for tree ${TREE:0:7} — the ship REUSES that record instead of taking a gate slot"
  elif [ -n "$WANT" ]; then
    say "the record for tree ${TREE:0:7} names webdeps ${WANT:0:12} but this tree now has $(wm_webdeps "$WT" | cut -c1-12) — the ship gates in FULL (a record cannot vouch for a different dependency set)"
  else
    say "no gate record for tree ${TREE:0:7} — the ship gates in full"
  fi

  if ! ( cd "$WT" && REQUIRE_SHIP=1 VERSION_OVERRIDE="$NEXT" ./deploy/build-and-ship.sh ); then
    warn "SHIP FAILED for $NEXT — the tag and release REMAIN (they are the record of the decision)"
    warn "  fix the tree, re-cut with --tag $NEXT after deleting the ref, or re-run: $PROG --yes --ship-only"
    exit 5
  fi
  # A CHILD'S EXIT CODE IS A CLAIM; THIS IS THE EVIDENCE. build-and-ship already asked the
  # server (it runs the same deploy/ship_proof.sh), and this asks again so a release cannot
  # report itself shipped on the strength of a child that no longer verifies anything. The
  # cost is one ssh round trip; the alternative is the v1.27.3 mirror.
  if ! ship_proof_live api jomform-api "$NEXT"; then
    warn "SHIP NOT PROVEN for $NEXT — the API ship returned success, but prod is not running $NEXT."
    warn "  The tag and release REMAIN. Nothing will be mirrored: the public repo only carries a"
    warn "  tree prod actually received. Fix the server half and re-run: $PROG --yes --ship-only"
    exit 5
  fi
  say "shipped the API image for $NEXT (verified: prod runs $NEXT)"
  WEB_SHIPPED=0
  if [ "$API_ONLY" = 0 ] && touches_web; then
    say "the release touches web/ — shipping the web image too"
    if ! ( cd "$WT" && IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile \
             REQUIRE_SHIP=1 VERSION_OVERRIDE="$NEXT" ./deploy/build-and-ship.sh ); then
      warn "WEB SHIP FAILED for $NEXT — the API image is already on prod under this tag"
      warn "  fix the tree, re-cut with --tag $NEXT after deleting the ref, or re-run: $PROG --yes --ship-only"
      exit 5
    fi
    if ! ship_proof_live web jomform-web "$NEXT"; then
      warn "WEB SHIP NOT PROVEN for $NEXT — the web ship returned success, but prod's web container"
      warn "  is not running $NEXT (the half-shipped state of issue #576)."
      warn "  The tag and release REMAIN; nothing will be mirrored. Re-run: $PROG --yes --ship-only"
      exit 5
    fi
    say "shipped the web image for $NEXT (verified: prod's web container runs $NEXT)"
    WEB_SHIPPED=1
  fi
  SHIPPED=1
else
  say "skipping the ship (--no-ship/--oss-only)"
fi

# ---------------------------------------------------------------- 4. the mirror
# THE INVARIANT, ENFORCED: the public repo only ever carries a tree prod received. With
# SHIP=1 that means the verification above passed (SHIPPED=1); with --oss-only it means the
# tag is live on prod NOW, checked here rather than assumed from a flag.
if [ "$OSS" = 1 ]; then
  if [ "$SHIPPED" != 1 ]; then
    [ "$OSS_ONLY" = 1 ] || die "refusing to mirror $NEXT: this run did not ship (internal: the mirror is only reached by a verified ship)"
    say "mirror retry: verifying first that prod really runs $NEXT (the mirror only carries shipped trees)"
    if ! ship_proof_target; then
      warn "REFUSING to mirror $NEXT: --oss-only needs SSH_HOST (or REMOTE_BUILD_HOST) to prove the tag"
      warn "  is live on prod. Nothing public points at a tree that did not ship."
      exit 7
    fi
    if ! ship_proof_live api jomform-api "$NEXT"; then
      warn "REFUSING to mirror $NEXT — prod is not running it. Ship it first ($PROG --yes --ship-only)."
      exit 7
    fi
    if [ "$API_ONLY" = 0 ] && touches_web; then
      if ! ship_proof_live web jomform-web "$NEXT"; then
        warn "REFUSING to mirror $NEXT — the API is live but prod's web container is not on $NEXT."
        exit 7
      fi
    fi
    say "verified on prod: $NEXT is live — the mirror may carry it"
  fi
  if ! ./deploy/sync-oss.sh --tag "$NEXT" --yes; then
    warn "MIRROR FAILED for $NEXT — the release stands; re-run: $PROG --yes --oss-only"
    exit 6
  fi
  say "mirrored $NEXT to the public repo"
else
  say "skipping the public mirror (--no-oss)"
fi

# THE FINAL LINE IS NOT ALLOWED TO OVERSTATE THE RUN. "RELEASED" means a tag that shipped;
# a tag+publish-only run says so, because that is what it did, and a --tag-only record says
# it published nothing at all.
if [ "$SHIPPED" = 1 ] || [ "$OSS_ONLY" = 1 ]; then
  FINAL="RELEASED $NEXT"
  if [ "$SHIPPED" = 1 ] && [ "$WEB_SHIPPED" = 1 ]; then
    FINAL="$FINAL (api + web verified running $NEXT on prod)"
  elif [ "$SHIPPED" = 1 ]; then
    FINAL="$FINAL (api verified running $NEXT on prod)"
  fi
  [ "$OSS" = 1 ] && FINAL="$FINAL, mirrored to the public repo"
  say "$FINAL"
elif [ "$TAG_ONLY" = 1 ]; then
  say "CUT $NEXT — tagged and pushed ONLY: no GitHub release (--tag-only), nothing shipped, nothing mirrored"
else
  say "CUT $NEXT — tagged and published; NOTHING was shipped (--no-ship) and nothing was mirrored"
fi
