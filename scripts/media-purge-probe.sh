#!/usr/bin/env bash
# media-purge-probe.sh — does a purged media object stop being served, at the
# ORIGIN *and* at the EDGE?
#
# WHY THIS EXISTS. Measuring only the origin is what missed the defect this
# probe is for: `DELETE /v1/rest/media/{id}/purge` removed the row and the
# object, the origin answered 404 immediately — and Cloudflare's edge went on
# serving its own copy for up to a day (`cache-control: public, max-age=86400`,
# `cf-cache-status: HIT`, `age: 35`, measured on prod 2026-10-18). A check that
# stops at the origin reports a clean purge while the object is still
# downloadable by anyone holding the URL.
#
# WHAT IT DOES
#   1. probes the ORIGIN with a cache-buster (so a CDN in front cannot answer for
#      it) and the EDGE without one (so the CDN's own copy is what answers);
#   2. optionally purges a trashed media row through the app's own route, and
#      prints the JSON that route returns (it names the edge outcome: `edge`);
#   3. re-probes both and decides:
#        PASS  the origin is 404 AND the edge is 404
#              (or the edge's declared lifetime is within the documented window,
#               in which case it prints the exact residual seconds — the copy is
#               self-healing, not stranded);
#        FAIL  the origin still serves the object, or the edge declares a
#              lifetime longer than the documented window.
#
# USAGE
#   scripts/media-purge-probe.sh \
#     --origin https://api-origin.example.com \
#     --edge   https://jomform.com \
#     --path   /media/u/<account>/<uuid>.png \
#     [--purge <media-id> --api https://api-origin.example.com --token "$JOMFORM_API_TOKEN"] \
#     [--window 300]
#
#   --path      the media path, exactly as it appears after the edge host
#   --purge     optional: a TRASHED media id to purge through DELETE /v1/rest/media/{id}/purge
#   --api       base URL of the API that owns the row (defaults to --origin)
#   --token     bearer token for the purge (API key or session)
#   --window    the documented freshness window in seconds (default 300)
#
# EXIT CODES
#   0  PASS   1  FAIL   2  usage or transport error (nothing was decided)
set -uo pipefail

ORIGIN=""
EDGE=""
MEDIA_PATH=""
PURGE_ID=""
API_BASE=""
TOKEN=""
WINDOW=300

usage() { sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --origin) ORIGIN="${2:-}"; shift 2 ;;
    --edge)   EDGE="${2:-}";   shift 2 ;;
    --path)   MEDIA_PATH="${2:-}"; shift 2 ;;
    --purge)  PURGE_ID="${2:-}";   shift 2 ;;
    --api)    API_BASE="${2:-}";   shift 2 ;;
    --token)  TOKEN="${2:-}";      shift 2 ;;
    --window) WINDOW="${2:-}";     shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "media-purge-probe: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$ORIGIN" ] || [ -z "$EDGE" ] || [ -z "$MEDIA_PATH" ]; then
  echo "media-purge-probe: --origin, --edge and --path are required" >&2
  exit 2
fi
case "$WINDOW" in (*[!0-9]*|"") echo "media-purge-probe: --window must be a number of seconds" >&2; exit 2 ;; esac
[ -n "$API_BASE" ] || API_BASE="$ORIGIN"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

# probe <url> <header-file> — prints the HTTP status, or 000 when the request
# never completed (a transport failure is not a verdict).
probe() {
  local url="$1" hdr="$2" code
  code="$(curl -sS -o "$TMP/body" -D "$hdr" -w '%{http_code}' --max-time 20 "$url" 2>"$TMP/err")" || code="000"
  case "$code" in (''|*[!0-9]*) code=000 ;; esac
  printf '%s' "$code"
}

header_value() { # header_value <header-file> <name>
  tr -d '\r' < "$1" | awk -v n="$(printf '%s' "$2" | tr 'A-Z' 'a-z')" '
    BEGIN { IGNORECASE = 1 }
    { name = tolower(substr($0, 1, index($0, ":") - 1)); if (name == n) { sub(/^[^:]*:[ ]*/, ""); v = $0 } }
    END { print v }'
}

# max_age <cache-control> — the declared freshness lifetime, 0 when none.
max_age() {
  printf '%s\n' "$1" | tr ',' '\n' | awk '
    { gsub(/^[ \t]+|[ \t]+$/, ""); if ($1 ~ /^max-age=/) { split($1, p, "="); v = p[2] } }
    END { print v + 0 }'
}

stamp="$(date +%s)"
ORIGIN_URL="${ORIGIN}${MEDIA_PATH}?cb=${stamp}"
EDGE_URL="${EDGE}${MEDIA_PATH}"
# The same object with a query appended is a SEPARATE edge entry (a CDN keys on
# the full URL), and it gets its own probe: a page, an embed or a tracking link
# can append one, and the purge covers it by prefix.
EDGE_V_URL="${EDGE}${MEDIA_PATH}?v=${stamp}"

echo "media-purge-probe  $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo "  origin : $ORIGIN   (probed with a cache-buster so the CDN cannot answer)"
echo "  edge   : $EDGE     (probed bare, and with a query appended — a separate CDN entry)"
echo "  path   : $MEDIA_PATH"
echo "  window : ${WINDOW}s"

probe_all() { # probe_all <label> — fills ST_ORIGIN/ST_EDGE/ST_EDGE_V and header files
  local label="$1"
  ST_ORIGIN="$(probe "$ORIGIN_URL" "$TMP/origin.h")"
  ST_EDGE="$(probe "$EDGE_URL" "$TMP/edge.h")"
  ST_EDGE_V="$(probe "$EDGE_V_URL" "$TMP/edgev.h")"
  CC_EDGE="$(header_value "$TMP/edge.h" cache-control)"
  CCS_EDGE="$(header_value "$TMP/edge.h" cf-cache-status)"
  AGE_EDGE="$(header_value "$TMP/edge.h" age)"
  printf '  %-8s origin=%s (cache-busted)   edge=%s [cf-cache-status=%s age=%s cache-control=%s]   edge? v=:%s\n' \
    "$label" "$ST_ORIGIN" "$ST_EDGE" "${CCS_EDGE:-—}" "${AGE_EDGE:-—}" "${CC_EDGE:-—}" "$ST_EDGE_V"
}

probe_all "before"

if [ -n "$PURGE_ID" ]; then
  if [ -z "$TOKEN" ]; then
    echo "media-purge-probe: --purge needs --token" >&2
    exit 2
  fi
  echo "  purge  : DELETE ${API_BASE}/v1/rest/media/${PURGE_ID}/purge"
  code="$(curl -sS -o "$TMP/purge.json" -w '%{http_code}' --max-time 30 \
    -X DELETE -H "Authorization: Bearer ${TOKEN}" \
    "${API_BASE}/v1/rest/media/${PURGE_ID}/purge" 2>"$TMP/err")" || code="000"
  echo "  purge  : HTTP ${code} $(tr -d '\n' < "$TMP/purge.json" 2>/dev/null | cut -c1-400)"
  probe_all "after"
else
  echo "  (no --purge: probe only — nothing was deleted)"
fi

# ── verdict ─────────────────────────────────────────────────────────────────
FAILED=0
if [ "$ST_ORIGIN" = "000" ] || [ "$ST_EDGE" = "000" ] || [ "$ST_EDGE_V" = "000" ]; then
  echo "verdict: ERROR — a probe did not complete (status 000); nothing was decided"
  exit 2
fi

if [ "$ST_ORIGIN" != "404" ]; then
  echo "verdict: FAIL — the origin still answers ${ST_ORIGIN} for the cache-busted URL, so the object"
  echo "         was not actually removed (the purge did not run, or it names another id)"
  FAILED=1
fi

edge_window="$(max_age "$CC_EDGE")"
edge_state="404"
if [ "$ST_EDGE" != "404" ] || [ "$ST_EDGE_V" != "404" ]; then
  if [ "$edge_window" -le "$WINDOW" ] && [ "$edge_window" -gt 0 ]; then
    remaining=$((edge_window - ${AGE_EDGE:-0}))
    [ "$remaining" -lt 0 ] && remaining=0
    edge_state="bounded (${remaining}s left of a declared ${edge_window}s)"
  else
    edge_state="STALE beyond the documented window (declared max-age=${edge_window}s, window=${WINDOW}s)"
    FAILED=1
  fi
fi

if [ "$FAILED" -eq 0 ]; then
  if [ "$ST_EDGE" = "404" ] && [ "$ST_EDGE_V" = "404" ]; then
    echo "verdict: PASS — origin 404 and edge 404 (bare and with a query), the edge copy is gone"
  else
    echo "verdict: PASS — origin 404 and the edge copy is ${edge_state}"
    echo "         (a purge that names the edge in its JSON removes the entry outright; without a"
    echo "          CDN credential the declared lifetime is the bound, and it expires on its own)"
  fi
  exit 0
fi

if [ "$ST_EDGE" != "404" ] || [ "$ST_EDGE_V" != "404" ]; then
  echo "         edge: ${edge_state}"
  echo "         (bare=${ST_EDGE} cf-cache-status=${CCS_EDGE:-—} age=${AGE_EDGE:-—}"
  echo "          cache-control=${CC_EDGE:-—}  query variant=${ST_EDGE_V} — a separate CDN entry)"
fi
echo "verdict: FAIL"
exit 1
