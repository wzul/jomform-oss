#!/usr/bin/env bash
# release_scope — does an unreleased diff OWE prod anything?
#
# WHY THIS EXISTS. `release-due.sh` asked only "are there enough commits, or are they
# old enough?" and `cut-release.sh` asked nothing at all. So a commit to the release
# tooling itself cut a version. Measured on 2026-11-08: four of the last eight releases
# (v1.25.0, v1.25.1, v1.27.2, v1.27.4) changed only deploy/, CI or repo docs, shipped no
# image — and every one of them still took GitHub's **Latest** badge away from the
# version prod was actually running. v1.27.4's own notes admitted "NO IMAGE IS OWED …
# this tag is not a shipped prod version" while it was published AS a release.
#
# The question a release has to answer first is "does this diff owe prod an artefact?",
# and it is answered from the PATHS the diff touches — not from the commit count, and
# not from a sentence in a profile.
#
#   OWES AN IMAGE (release-worthy)
#     api/**          the API image (includes api/internal/db/migrations/*.sql)
#     web/**          the web image
#     MCP-PARITY.md   a build input on BOTH sides: api/internal/mcpsrv/mcp_parity_test.go
#                     and web/src/lib/langPlacement.test.ts parse it, and web/Dockerfile
#                     copies it into the build context (web/src/lib/buildContext.test.ts
#                     enforces that copy), so a change to it can move both images
#     docs/guides/**  user-visible documentation: web/scripts/guides.mjs materialises
#                     docs/guides into web/src/lib/guides.gen.ts + web/public/guides, so
#                     the web image is what renders it — a guide change is a product
#                     change, not tooling
#
#   OWES NOTHING (accumulates; rides along with the next release that does owe an image)
#     deploy/**, scripts/**, .github/**, e2e/**, loadtest/**, gate-inventory/**,
#     Makefile, AGENTS.md, GATE-INVENTORY.md, MCP-PARITY.md-adjacent repo docs,
#     docs/** other than docs/guides/**, and any other repo plumbing.
#
# ONE DEFINITION, TWO CALLERS. `release-due.sh` (may this window be released at all?) and
# `cut-release.sh` (may these bytes be tagged?) source this file, so the card filer and
# the cutter can never disagree about whether a diff is releasable. If they disagreed,
# the card would be filed and then the cut refused, and the next lane would spend a run
# discovering that.
#
# Public functions (the range is passed as PAIRS — since, sha — because
# `git diff --name-only <sha>` silently compares a commit against the WORKING TREE; a
# caller that meant "everything up to this commit" would classify whatever is checked
# out instead of the bytes being released):
#   rs_changed_paths <since|none> <sha>   every changed path, one per line
#   rs_scope <since|none> <sha>           "image" when the diff owes an image, else "none"
#   rs_scope_paths <since|none> <sha>     only the paths that made it owe one
#   rs_scope_summary <since|none> <sha>   one human line, e.g. "api/ (12), web/ (3)"
#   rs_image_targets <since|none> <sha>   "api" and/or "web", one per line

# rs_owe_scope_of <path> -> "image" or "none". THE RULE. Keep it a single case statement:
# a caller that wants a second opinion is how the two lanes drift apart again.
rs_owe_scope_of() {
  case "${1:-}" in
    api/*|web/*|MCP-PARITY.md|docs/guides/*) printf 'image\n' ;;
    *)                                       printf 'none\n'  ;;
  esac
}

rs_changed_paths() {   # $1 = since (tag/ref or 'none')  $2 = the commit being released
  local since=${1:-none} sha=${2:?rs_changed_paths needs the commit}
  if [ "$since" = none ]; then
    git ls-tree -r --name-only "$sha" 2>/dev/null
  else
    git diff --name-only "$since..$sha" 2>/dev/null
  fi
}

rs_scope() {           # $1 = since, $2 = sha -> "image" | "none"
  local p
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    if [ "$(rs_owe_scope_of "$p")" = image ]; then
      printf 'image\n'
      return 0
    fi
  done < <(rs_changed_paths "${1:-none}" "${2:?rs_scope needs the commit}")
  printf 'none\n'
}

rs_scope_paths() {     # the paths that decided it (empty when the scope is 'none')
  local p
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    [ "$(rs_owe_scope_of "$p")" = image ] && printf '%s\n' "$p"
  done < <(rs_changed_paths "${1:-none}" "${2:?rs_scope_paths needs the commit}")
}

# Group a path into the bucket a human would name it by: "api/", "web/", "MCP-PARITY.md",
# "docs/guides/", or the first segment of anything else ("deploy/", "scripts/", …).
rs_group_of() {
  case "${1:-}" in
    MCP-PARITY.md) printf 'MCP-PARITY.md\n' ;;
    docs/guides/*) printf 'docs/guides\n' ;;
    */*)           printf '%s/\n' "${1%%/*}" ;;
    *)             printf '%s\n' "$1" ;;
  esac
}

rs_scope_summary() {   # $1 = since, $2 = sha -> "api/ (12), web/ (3)" or "deploy/ (6)"
  local since=${1:-none} sha=${2:?rs_scope_summary needs the commit} p
  if [ "$(rs_scope "$since" "$sha")" = image ]; then
    rs_scope_paths "$since" "$sha"
  else
    rs_changed_paths "$since" "$sha"
  fi | while IFS= read -r p; do [ -n "$p" ] && rs_group_of "$p"; done \
    | sort | uniq -c | sort -rn \
    | awk '{ n=$1; $1=""; sub(/^ +/, ""); gsub(/[[:space:]]+$/, ""); out = (out == "" ? "" : out ", ") $0 " (" n ")"; } END { print out }'
}

rs_image_targets() {   # $1 = since, $2 = sha -> the images the diff owes, one per line
  local since=${1:-none} sha=${2:?rs_image_targets needs the commit}
  rs_scope_paths "$since" "$sha" | while IFS= read -r p; do
    case "$p" in
      api/*)        printf 'api\n' ;;
      web/*)        printf 'web\n' ;;
      MCP-PARITY.md) printf 'api\nweb\n' ;;
      docs/guides/*) printf 'web\n' ;;
    esac
  done | awk '!seen[$0]++'
}
