#!/usr/bin/env bash
# release-due — is a release owed right now, and (optionally) file exactly ONE card for it.
#
# Why this exists: the merger used to file a deploy card for every merge it landed
# (jf-merger/SOUL.md "kanban_create(title=\"deploy: <same subject>\"...)"), and the deploy
# lane tagged every deploy. Together that produced ~15 releases a day and 102 tags in a week,
# so "the version in prod" no longer meant anything and the public mirror had no released
# version to follow.
#
# Now a release is a DECISION on a cadence: a deploy ships a tagged tree
# (deploy/require-release-tag.sh), and a release is owed when the unreleased window is either
# big enough to be worth the full-inventory gate or old enough that a fix is sitting on main
# unused. The decision is code, not prose in a profile, so it can be tested.
#
# THE WINDOW MUST ALSO OWE AN IMAGE. Commit count and age are necessary, not sufficient:
# a commit to the release tooling itself is not a release. Measured on 2026-11-08, four of
# the last eight releases (v1.25.0, v1.25.1, v1.27.2, v1.27.4) changed only deploy/, CI or
# repo docs, shipped no image, and each still took GitHub's "Latest" badge off the version
# prod was running. So the diff is classified by PATH (deploy/release_scope.sh — the same
# rule cut-release.sh applies) and a window that owes nothing files no card at all: those
# commits accumulate and ride along with the next release that does owe an image.
#
# Policy knobs:
#   RELEASE_MIN_COMMITS    (default 5)  commits since the last tag
#   RELEASE_MAX_AGE_HOURS  (default 24) age of the OLDEST unreleased commit
# A window that meets EITHER threshold is due — PROVIDED it owes an image. Both set to 0
# disables the schedule and makes releases purely on-demand (then only --force files a card).
#
# Usage:
#   deploy/release-due.sh                        # print the decision and the numbers
#   deploy/release-due.sh --print-card           # print the card TEXT, touch no board
#   deploy/release-due.sh --file-card            # file the release card when due (deduped)
#   deploy/release-due.sh --force --file-card    # file it regardless (Wan said "release now",
#                                                # or a deliberate tooling milestone)
#   --from <ref>   what would be released (default: origin/main)
#
# --file-card goes through `hermes kanban`, which is FENCED in a dispatcher lane shell
# (HERMES_DELEGATED_CHILD_CONTEXT): the fence lives in the Kanban DB layer, so even `list`
# is refused there — and a dedupe whose input cannot be read must never file, because the
# duplicate it would create is the exact defect the dedupe exists to prevent. Such a shell
# gets exit 4 and, with --print-card, the exact text to file with its own kanban_create
# tool. That fence is a trust boundary, not a permission to work around.
#
# Exit: 0 due · 1 not due · 2 error · 3 a release card is already open (nothing filed)
#       4 --file-card cannot read/write the board from this shell (nothing filed)
set -o pipefail
PROG=${0##*/}
die() { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say() { printf '%s: %s\n' "$PROG" "$*"; }

# The scope rule lives in ONE file, shared with cut-release.sh (deploy/release_scope.sh).
# Resolved next to THIS script, not next to the repo root: `--from` can point at another
# checkout, and the rule that decides "does this window owe an image" must come from the
# tool that is running, not from whichever tree it is looking at.
SELF_DIR=$(cd "$(dirname "$0")" && pwd) || die "cannot resolve the script's directory"
SCOPE_LIB="$SELF_DIR/release_scope.sh"
[ -r "$SCOPE_LIB" ] || die "deploy/release_scope.sh is missing next to $PROG — this script cannot tell a diff that owes an image from one that does not, so it will NOT file a release card"
# shellcheck source=deploy/release_scope.sh
. "$SCOPE_LIB"

FROM=origin/main; FILE=0; FORCE=0; PRINT=0
while [ $# -gt 0 ]; do
  case "$1" in
    --from)      FROM=${2:?--from needs a ref}; shift 2 ;;
    --file-card) FILE=1; shift ;;
    --print-card) PRINT=1; shift ;;
    --force)     FORCE=1; shift ;;
    -h|--help)   sed -n '2,/^set -o pipefail/p' "$0" | sed '$d'; exit 0 ;;
    *)           die "unknown argument: $1" ;;
  esac
done

MIN_COMMITS=${RELEASE_MIN_COMMITS:-5}
MAX_AGE_HOURS=${RELEASE_MAX_AGE_HOURS:-24}
BOARD=${HERMES_KANBAN_BOARD:-jomform}

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || die "not in a git repo"
cd "$ROOT" || die "cannot cd $ROOT"
git fetch -q --tags --force origin 2>/dev/null || true
SHA=$(git rev-parse --verify "${FROM}^{commit}") || die "cannot resolve $FROM"

LAST=$(git describe --tags --abbrev=0 --match 'v[0-9]*' "$SHA" 2>/dev/null || echo none)
if [ "$LAST" = none ]; then
  COUNT=$(git rev-list --count "$SHA")
  OLDEST=$(git log --reverse --format=%ct "$SHA" | head -1)
else
  COUNT=$(git rev-list --count "$LAST..$SHA")
  OLDEST=$(git log --reverse --format=%ct "$LAST..$SHA" | head -1)
fi
COUNT=${COUNT:-0}

if [ "$COUNT" = 0 ]; then
  say "not due: nothing on $FROM since $LAST — there is no commit to release"
  exit 1
fi

NOW=$(date +%s)
AGE_H=$(( (NOW - ${OLDEST:-$NOW}) / 3600 ))

# Does the window OWE anything? This is the question the commit count cannot answer, and
# skipping it is what published v1.27.4 (deploy/ only) as a release. Classified by path,
# with the same rule cut-release.sh applies, so a window this script calls releasable is
# one cut-release will not then refuse.
SCOPE=$(rs_scope "$LAST" "$SHA")
SCOPE_SUMMARY=$(rs_scope_summary "$LAST" "$SHA")

REASON=""
[ "$FORCE" = 1 ] && REASON="forced (an explicit 'release now')"
if [ -z "$REASON" ] && [ "$MIN_COMMITS" != 0 ] && [ "$COUNT" -ge "$MIN_COMMITS" ]; then
  REASON="$COUNT commit(s) since $LAST (>= RELEASE_MIN_COMMITS=$MIN_COMMITS)"
fi
if [ -z "$REASON" ] && [ "$MAX_AGE_HOURS" != 0 ] && [ "$AGE_H" -ge "$MAX_AGE_HOURS" ]; then
  REASON="oldest unreleased commit is ${AGE_H}h old (>= RELEASE_MAX_AGE_HOURS=$MAX_AGE_HOURS)"
fi

say "last release $LAST · unreleased $COUNT commit(s) · oldest ${AGE_H}h · policy ${MIN_COMMITS} commits / ${MAX_AGE_HOURS}h"
if [ "$SCOPE" = image ]; then
  say "scope: $SCOPE_SUMMARY — this window owes an image"
else
  say "scope: ${SCOPE_SUMMARY:-no changed paths} — nothing here ships an image"
fi

# A gate, not a tie-break: when the diff owes no image there is no release to cut, whatever
# the count or the age says. Those commits are not lost — they ride along with the next
# release that does owe an image, and the notes roll them into one "Internal" line.
if [ "$SCOPE" != image ] && [ "$FORCE" != 1 ]; then
  say "not due: the $COUNT unreleased commit(s) owe no image — deploy/, CI and tooling changes accumulate and ride along with the next real release"
  say "  cutting a version for these bytes is how v1.25.0, v1.25.1, v1.27.2 and v1.27.4 took GitHub's \"Latest\" badge without an image behind it"
  say "  a deliberate tooling milestone is an explicit decision: $PROG --force --file-card"
  exit 1
fi

if [ -z "$REASON" ]; then
  say "not due yet — the window is smaller and younger than the policy"
  exit 1
fi
say "DUE: $REASON"


if [ "$FILE" = 0 ] && [ "$PRINT" = 0 ]; then
  exit 0
fi

TITLE="release: cut the next release (${COUNT} commit sejak ${LAST}, tertua ${AGE_H}j)"
BODY=$(cat <<EOF
## Kenapa kad ini wujud

Bukan satu deploy per merge lagi. Deploy mesti berjalan dari TAG
(\`deploy/require-release-tag.sh\`), dan release dipotong bila tetingkapnya cukup besar atau
cukup lama — bukan setiap kali PR mendarat.

Dasar pada masa kad ini difailkan:

    $REASON
    $LAST -> $FROM (${COUNT} commit, tertua ${AGE_H}j)
    skop: ${SCOPE_SUMMARY:-tiada} — $([ "$SCOPE" = image ] && echo 'berhutang image' || echo 'MILESTONE TOOLING (tiada image; --force dipakai)')
    polisi: RELEASE_MIN_COMMITS=${MIN_COMMITS} · RELEASE_MAX_AGE_HOURS=${MAX_AGE_HOURS}

## Apa yang perlu dibuat

1. \`./deploy/cut-release.sh --dry-run\` — baca pelan, versi seterusnya dan NOTA (changelog
   berkumpulan mengikut kawasan) dahulu.
2. \`SSH_HOST=<host prod> ./deploy/cut-release.sh --yes\` — tag, GitHub release, ship
   (inventori penuh), kemudian VERIFY pada prod dan barulah mirror ke
   \`wzul/jomform-oss\`. Tanpa SSH_HOST (atau REMOTE_BUILD_HOST) ia MENOLAK dengan
   exit 7 dan tidak mencipta tag: release yang tidak boleh sampai ke prod bukan release.
   Tag+publish sahaja mesti disertakan \`--no-ship --no-oss\` — mirror tidak
   pernah menerima pokok yang tidak di-ship.
3. Jika ini HOTFIX (prod rosak sekarang): \`SSH_HOST=<host> ./deploy/cut-release.sh --yes --patch\`.
4. Jika ship gagal selepas tag: tag itu KEKAL sebagai rekod; betulkan pohon, kemudian
   \`SSH_HOST=<host> ./deploy/cut-release.sh --yes --ship-only\`.
5. Jika mirror gagal: release tetap sah; \`SSH_HOST=<host> ./deploy/cut-release.sh --yes --oss-only\`
   (ia sahkan tag itu benar-benar hidup pada prod sebelum mirror).
6. Jika ini MILESTONE TOOLING yang sengaja ditag (bukan release prod):
   \`./deploy/cut-release.sh --yes --tag-only\` — tag + push sahaja, TIADA GitHub release,
   tiada ship dan tiada mirror, jadi lencana **Latest** kekal pada versi yang prod jalankan.
   Jangan guna \`--force\` untuk ini: \`--force\` menerbitkan release biasa.

Jangan cipta tag sendiri dan jangan jalankan \`build-and-ship.sh\` pada pokok tanpa tag — ia
akan menolak (exit 4).
EOF
)

if [ "$PRINT" = 1 ]; then
  printf 'TITLE: %s\n\n%s\n' "$TITLE" "$BODY"
  exit 0
fi

command -v hermes >/dev/null 2>&1 || die \
  "--file-card needs the hermes CLI on PATH — run '$PROG --print-card' and file it with the kanban_create tool instead"

# What to say when this shell cannot reach the board. Kept in one place so the list path
# (dedupe) and the create path can never explain the same fence in two different ways.
fence_explain() {
  printf '%s: nothing was filed.\n' "$PROG" >&2
  if [ -n "${HERMES_DELEGATED_CHILD_CONTEXT:-}" ]; then
    printf '%s: why: this is a dispatcher LANE shell (HERMES_DELEGATED_CHILD_CONTEXT=%s), and the\n' \
      "$PROG" "$HERMES_DELEGATED_CHILD_CONTEXT" >&2
    printf '%s:      board fences CLI access from delegate/lane contexts at the Kanban DB layer —\n' "$PROG" >&2
    printf '%s:      even `list` is refused, because it recomputes readiness in a write\n' "$PROG" >&2
    printf '%s:      transaction. That fence is a trust boundary, not a permission to work around.\n' "$PROG" >&2
  else
    printf '%s: why: the board could not be read or written from this shell (message above).\n' "$PROG" >&2
  fi
  printf '%s: next: file it with the kanban_create tool (assignee jf-deployer, project jomform,\n' "$PROG" >&2
  printf '%s:      priority 130) using the text from `%s --print-card`, or run\n' "$PROG" "$PROG" >&2
  printf '%s:      `%s --file-card` from a host shell.\n' "$PROG" "$PROG" >&2
}

# Dedupe: one OPEN release card at a time — "open" means anything not done/archived, so a
# blocked or in-review release card counts. Listing is the dedupe input, so a list that fails
# means we do NOT know whether a card exists; filing then would risk the duplicate this whole
# block exists to prevent, so the run stops instead.
OPEN=""
for st in triage todo scheduled ready running blocked review; do
  if ! OUT=$(HERMES_KANBAN_BOARD="$BOARD" hermes kanban --board "$BOARD" list --status "$st" 2>&1); then
    printf '%s\n' "$OUT" | sed 's/^/    /' >&2
    fence_explain
    exit 4
  fi
  HIT=$(printf '%s\n' "$OUT" | grep -iE 'release: ' || true)
  [ -n "$HIT" ] && OPEN="$OPEN$HIT"$'\n'
done
if [ -n "$OPEN" ]; then
  say "a release card is already open — nothing filed:"
  printf '%s\n' "$OPEN" | head -3 | sed 's/^/    /'
  exit 3
fi

if ! OUT=$(HERMES_KANBAN_BOARD="$BOARD" hermes kanban --board "$BOARD" create "$TITLE" \
     --assignee jf-deployer --project jomform --workspace worktree --priority 130 2>&1); then
  printf '%s\n' "$OUT" | sed 's/^/    /' >&2
  if [ -n "${HERMES_DELEGATED_CHILD_CONTEXT:-}" ]; then
    fence_explain
    exit 4
  fi
  die "kanban create failed (the board's own message is above) — nothing was filed"
fi
# `hermes kanban create` answers `Created <id>  (ready, assignee=...)`. Read the id from
# THAT answer: a second list would be a heavier, racier way to ask the same question.
NEW=$(printf '%s\n' "$OUT" | grep -oE 't_[0-9a-f]{8}' | head -1 || true)
if [ -n "$NEW" ]; then
  HERMES_KANBAN_BOARD="$BOARD" hermes kanban --board "$BOARD" edit "$NEW" --body "$BODY" >/dev/null 2>&1 || true
  say "filed $NEW: $TITLE"
else
  say "filed a release card (could not read its id back)"
fi
exit 0
