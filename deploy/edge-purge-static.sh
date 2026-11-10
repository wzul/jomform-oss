#!/usr/bin/env bash
#
# edge-purge-static.sh — ask the CDN to drop its copies of /_next/static/ after a ship.
#
# THE GAP THIS CLOSES (measured on prod, 2026-11-09). `/_next/static/*` is served
# with `cache-control: public, max-age=31536000, immutable`, and Cloudflare caches
# the RESPONSE — its header set included — under that lifetime. A release that
# changes response headers but not asset BYTES keeps the same hashed chunk names,
# because the hash covers the module graph, not the bytes: so the edge keeps
# answering with the OLD header set for up to a year. Measured that day: 39 of the
# 60 assets the running image ships were answered from the edge with ZERO security
# headers (ages up to 23.8 days) although the release had added all six at the
# origin. Only a manual `purge_everything` closed it. The origin was honest the
# whole time, which is exactly why looking at the origin cannot see this.
#
# WHY BY PREFIX, AND WHY THIS PREFIX. A CDN keys on the full URL, and a chunk is
# requested with whatever query string a page appends, so an exact-URL purge would
# leave the variants behind. `https://<host>/_next/static/` is the whole build
# output — 60 files in the shipped image — and nothing else lives under it. The
# HTML is deliberately NOT purged: those routes answer `DYNAMIC` (verified on
# prod: /, /docs, /docs/sales, /login), so their header set was never cached.
#
# WHY THIS IS A SHELL SCRIPT AND NOT api/internal/cdnpurge. That package is the
# right client and its RULES are what this file follows — a purge by prefix, an
# in-zone check, and "a 200 carrying success:false is a refusal, not a purge".
# What it cannot do is run here: the token exists only in the deployment host's
# compose `.env`, and the shell running deploy/build-and-ship.sh is a different
# machine (or at least a different environment) that never holds it. So this step
# has to execute where the credential is, which is why build-and-ship.sh pipes
# these bytes over ssh and runs them there, and why the request is made with curl
# instead of through the Go client. Nothing is re-decided: the endpoint, the body
# shape, the zone rule and the verdict are the ones cdnpurge documents.
#
# CONFIGURATION (all read from the environment of the host that runs this):
#
#   JF_CF_PURGE_TOKEN     API token with Zone → Cache Purge. Unset OR EMPTY means
#                         "not configured" — an emptiness trap worth knowing: the
#                         var NAME is present in prod's compose env while its VALUE
#                         is empty, so `printenv | grep JF_CF_PURGE` reports it as
#                         provisioned. Check the LENGTH, not the name.
#   JF_CF_PURGE_ZONE_ID   the zone those hosts live in. Same emptiness rule.
#   JF_CF_PURGE_HOSTS     optional comma-separated hosts in that zone; a wildcard
#                         ("*.jomform.com") covers subdomains and NOT the base host.
#                         Empty means "the host of APP_BASE_URL", which is this
#                         deployment's own public origin and therefore in zone.
#   JF_CF_PURGE_API_BASE  optional API root override (tests)
#   APP_BASE_URL          the public origin; its host is what is purged when no
#                         --host was given
#
# Usage:
#   ./deploy/edge-purge-static.sh [--host <host>]... [--hosts <a,b>]
#
# Exit:
#   0  purged and ACCEPTED by the CDN, or not configured (a supported state: the
#      warning is printed and the caller continues, because the ship already
#      happened and no exit code brings the asset bytes back)
#   2  a usage error
#   3  configured and attempted, but the purge did NOT land (refused, unreachable,
#      no host in zone, curl missing). The caller prints the warning and continues;
#      the exit code exists so the reason is attributable.
#
# NOTHING HERE PRINTS THE TOKEN. The Authorization header reaches curl through its
# config on stdin (`-K -`) rather than argv, so it does not appear in the process
# list of the deployment host either.
#
# Run:  ./deploy/edge-purge-static.sh
set -uo pipefail

# The prefix, one definition. A change here is what the purge covers.
STATIC_PREFIX="/_next/static/"

# The sentence every caller prints when the edge was NOT purged. It is stated once
# so build-and-ship.sh and this script cannot drift into two different claims.
CANON_NOT_PURGED='edge not purged: stale `_next/static` headers may be served for up to a year after a header-only release'

TIMEOUT_S="${JF_CF_PURGE_TIMEOUT_S:-20}"

usage() {
  # This script is ALSO executed as a stdin stream (`bash -s`) by
  # deploy/build-and-ship.sh, where BASH_SOURCE names no readable file — so the
  # self-read is guarded and the usage line still works in that mode.
  local src="${BASH_SOURCE[0]:-}"
  if [ -n "$src" ] && [ -r "$src" ]; then
    sed -n '2,/^set -uo pipefail$/p' "$src" | sed '$d' | sed 's/^# \{0,1\}//'
  else
    printf 'deploy/edge-purge-static.sh [--host <host>]... [--hosts <a,b>]\n'
  fi
}

warn() { printf '%s\n' "$*" >&2; }

# warn_not_purged <why> — the LOUD block. Always the same canonical sentence first,
# then the one line that says what an operator has to do about it.
warn_not_purged() {
  warn ""
  warn "  ============================================================================"
  warn "   WARNING: $CANON_NOT_PURGED"
  warn "  ============================================================================"
  [ -n "${1:-}" ] && warn "   $1"
  warn ""
  return 0
}

# trim <string> — surrounding whitespace, so a var set to "   " reads as unset.
trim() {
  local v="$1"
  v="${v#"${v%%[![:space:]]*}"}"
  v="${v%"${v##*[![:space:]]}"}"
  printf '%s' "$v"
}

# norm_host <host-or-url> — lower-case host with any scheme, path and port removed.
# Mirrors cdnpurge.normalizeHost so "https://JomForm.com:443/x" and "jomform.com"
# name the same zone entry.
norm_host() {
  local h
  h="$(trim "${1:-}")"
  h="${h#*://}"
  h="${h%%/*}"
  if [[ "$h" =~ ^(.*):([0-9]+)$ ]]; then h="${BASH_REMATCH[1]}"; fi
  printf '%s' "$h" | tr '[:upper:]' '[:lower:]'
}

# in_zone <host> <comma-list> — mirrors cdnpurge.Client.inZone. A listed host matches
# exactly; "*.base" matches any depth UNDER base and NOT base itself. That asymmetry
# is deliberate and is the trap this script reports rather than hides: prod's
# JF_CF_PURGE_HOSTS is "*.jomform.com" (it exists for per-workspace form hosts),
# while the web app itself is the APEX — so the apex has to be listed too.
in_zone() {
  local host="$1" raw part base
  local -a parts
  IFS=',' read -r -a parts <<<"${2:-}"
  for raw in "${parts[@]}"; do
    part="$(norm_host "$raw")"
    [ -n "$part" ] || continue
    [ "$part" = "$host" ] && return 0
    case "$part" in
      '*.'*)
        base="${part#\*.}"
        if [ -n "$base" ]; then
          case "$host" in *".$base") return 0 ;; esac
        fi
        ;;
    esac
  done
  return 1
}

# valid_host — a DNS name we are willing to put in a JSON body and a URL. Anything
# else is refused by name rather than escaped into the request.
valid_host() {
  [[ "${1:-}" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]]
}

HOSTS=()
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --host)
      [ -n "${2:-}" ] || { warn "edge-purge-static: --host needs a value"; exit 2; }
      HOSTS+=("$2"); shift 2 ;;
    --hosts)
      [ -n "${2:-}" ] || { warn "edge-purge-static: --hosts needs a value"; exit 2; }
      IFS=',' read -r -a _more <<<"$2"
      for _h in "${_more[@]}"; do HOSTS+=("$_h"); done
      shift 2 ;;
    *) warn "edge-purge-static: unknown argument: $1 (see --help)"; exit 2 ;;
  esac
done

TOKEN="$(trim "${JF_CF_PURGE_TOKEN:-}")"
ZONE="$(trim "${JF_CF_PURGE_ZONE_ID:-}")"
IN_ZONE_HOSTS="$(trim "${JF_CF_PURGE_HOSTS:-}")"
API_BASE="$(trim "${JF_CF_PURGE_API_BASE:-}")"
APP_BASE="$(trim "${APP_BASE_URL:-}")"
[ -n "$API_BASE" ] || API_BASE="https://api.cloudflare.com/client/v4"
API_BASE="${API_BASE%/}"

# ── 1. Not configured is a supported state, and it is reported LOUDLY ───────────
if [ -z "$TOKEN" ] || [ -z "$ZONE" ]; then
  warn_not_purged "JF_CF_PURGE_TOKEN and/or JF_CF_PURGE_ZONE_ID are unset or EMPTY here (check the value LENGTH, not whether the name appears in printenv). Set both on the deployment host (deploy/vps/.env.prod.example) to have the CDN drop /_next/static/ at ship time."
  exit 0
fi

# ── 2. Which hosts? ────────────────────────────────────────────────────────────
if [ "${#HOSTS[@]}" -eq 0 ] && [ -n "$APP_BASE" ]; then
  HOSTS+=("$APP_BASE")
fi
if [ "${#HOSTS[@]}" -eq 0 ]; then
  warn_not_purged "no host to purge: pass --host <host> or set APP_BASE_URL, so the prefix has an origin."
  exit 2
fi

PREFIXES=()
DROPPED=()
for raw in "${HOSTS[@]}"; do
  h="$(norm_host "$raw")"
  if [ -z "$h" ]; then
    continue
  fi
  if ! valid_host "$h"; then
    DROPPED+=("$h (not a plain DNS name)")
    continue
  fi
  if [ -n "$IN_ZONE_HOSTS" ] && ! in_zone "$h" "$IN_ZONE_HOSTS"; then
    DROPPED+=("$h (not covered by JF_CF_PURGE_HOSTS=$IN_ZONE_HOSTS)")
    continue
  fi
  # de-duplicate, so a host named twice is one prefix
  for p in "${PREFIXES[@]:-}"; do
    [ "$p" = "https://$h$STATIC_PREFIX" ] && continue 2
  done
  PREFIXES+=("https://$h$STATIC_PREFIX")
done

if [ "${#DROPPED[@]}" -gt 0 ]; then
  # Purge what IS in zone (leaving the good hosts stale to be consistent about a
  # bad one would be the worse failure), then report the drop as a red step.
  for d in "${DROPPED[@]}"; do
    warn "edge purge: NOT purging $d"
  done
  warn "edge purge: add the missing host(s) to JF_CF_PURGE_HOSTS — a wildcard matches SUBDOMAINS ONLY, so the apex has to be listed by name."
fi

if [ "${#PREFIXES[@]}" -eq 0 ]; then
  warn_not_purged "no host was in the configured zone, so nothing was asked of the CDN."
  exit 3
fi

command -v curl >/dev/null 2>&1 || {
  warn_not_purged "curl is not installed on this host, so the purge request could not be made."
  exit 3
}

# ── 3. The request ────────────────────────────────────────────────────────────
# hosts are validated DNS names above, so the JSON needs no escaping.
payload='{"prefixes":['
sep=''
for p in "${PREFIXES[@]}"; do
  payload="$payload$sep\"$p\""
  sep=','
done
payload="$payload]}"

endpoint="$API_BASE/zones/$ZONE/purge_cache"
body="$(mktemp "${TMPDIR:-/tmp}/edge-purge-static.body.XXXXXX")" || body=""
if [ -z "$body" ]; then
  warn_not_purged "could not create a temp file for the CDN's reply, so the purge request was not made."
  exit 3
fi
errf="$(mktemp "${TMPDIR:-/tmp}/edge-purge-static.err.XXXXXX")" || errf=""
cleanup() { [ -n "$body" ] && rm -f "$body"; [ -n "$errf" ] && rm -f "$errf"; }
trap cleanup EXIT

code="$(
  printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" |
    curl -sS -K - --max-time "$TIMEOUT_S" \
      -X POST \
      -H 'Content-Type: application/json' \
      --data "$payload" \
      -o "$body" -w '%{http_code}' \
      "$endpoint" 2>"${errf:-/dev/null}"
)" || code="000"

# A 200 carrying success:false is a REFUSAL (Cloudflare answers 200 with errors[] for
# several rejections, rate limiting among them), so the status code alone is read by
# nothing here.
success="$(tr -d '[:space:]' <"$body" 2>/dev/null | grep -o '"success":[a-z]*' | head -n 1)"

if [ "${code#2}" != "$code" ] && [ "$success" = '"success":true' ]; then
  joined=''
  for p in "${PREFIXES[@]}"; do joined="${joined:+$joined }$p"; done
  printf '==> edge purge: %s -> http %s success=true (%s)\n' "$endpoint" "$code" "$joined"
  [ "${#DROPPED[@]}" -eq 0 ] || exit 3
  exit 0
fi

detail="http ${code}${success:+ $success}"
[ -s "$body" ] && detail="$detail body=$(head -c 400 "$body" | tr '\n' ' ')"
[ -s "${errf:-/dev/null}" ] && detail="$detail curl=$(head -c 200 "$errf" | tr '\n' ' ')"
warn "edge purge REFUSED: $endpoint -> $detail"
warn_not_purged "the CDN did not accept the prefix purge, so the old header set can stay at the edge for up to a year."
exit 3
