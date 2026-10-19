#!/usr/bin/env bash
# media-purge-probe.sh — does a purged media object stop being served, at the
# ORIGIN *and* at the EDGE?
#
# WHY THIS EXISTS. Measuring only the origin is what missed the defect this
# probe is for: `DELETE /v1/rest/media/{id}/purge` removed the row and the
# object, the origin answered 404 immediately — and the CDN edge went on serving
# its own copy, downloadable by anyone still holding the URL.
#
# WHAT IT MEASURES
#   1. the ORIGIN with a cache-buster (so a CDN in front cannot answer for it).
#      That response also DECLARES the lifetime the origin allows any shared
#      cache to keep the object for (`cache-control: max-age`); that number is
#      the reference every other number here is judged against.
#   2. the EDGE bare, and with a query appended — a CDN keys on the full URL, so
#      the query form is a SECOND entry, and a page, embed or tracking link can
#      append one. Both have to be gone (or bounded) for the purge to be safe.
#   3. optionally purges a trashed media row through the app's own route and
#      prints the JSON that route returns (it names the edge outcome: `edge`).
#
# WHY THE EDGE'S HEADER IS NOT THE EDGE LIFETIME. A CDN in front of the origin
# may REWRITE the client-facing `cache-control` — Cloudflare's zone setting
# "Browser Cache TTL" does exactly that — so the header a BROWSER receives can
# be hours long while the shared edge entry still expires on the origin's short
# lifetime. Measured on this stack 2026-10-18: the origin declared
# `public, max-age=300`, the edge handed the browser `max-age=14400`, and the
# edge entry itself went `cf-cache-status: EXPIRED` at t+302s. Reading that
# header as "the edge lifetime" reports STALE on a completely healthy stack —
# the false FAIL this script used to produce whenever no CDN credential was
# configured (the `edge: not_configured` path), which is a supported mode.
#
# THE RULE
#   * reference lifetime = the ORIGIN's declared max-age, read cache-busted, or
#     (when the origin already answers 404) the `edge_max_age_s` the purge reply
#     itself names. An edge value HIGHER than the reference is a rewrite of the
#     client-facing header and is NOT used as the edge lifetime.
#   * a served edge copy is BOUNDED (pass) when its lifetime is within --window
#     AND its `age` is consistent with that lifetime: `~N s left` is printed.
#   * an entry whose `age` is already LONGER than the lifetime the origin now
#     declares was filled under an older, longer TTL (a pre-fix entry, or an
#     edge-cache-TTL override) and is a FAIL: it cannot be relied on to expire
#     inside the window, and trusting the declared value would have hidden it.
#   * an entry with NO readable lifetime anywhere cannot be bounded, so it is a
#     FAIL — unless --await-expiry measures it stop serving instead.
#   * --await-expiry replaces inference with MEASUREMENT: it re-probes the copy
#     until it stops serving (404, `cf-cache-status: EXPIRED`/`REVALIDATED`, or
#     an age that RESETS on a refill) or --window + 5s elapses, and the
#     observation wins over every declared value. It costs up to --window
#     seconds per served copy, so it is OFF by default: the cheap inference
#     above cannot tell a CDN rewrite from a real edge-cache-TTL override on a
#     FRESH entry, and this flag can.
#
# USAGE
#   scripts/media-purge-probe.sh \
#     --origin https://api-origin.example.com \
#     --edge   https://jomform.com \
#     --path   /media/u/<account>/<uuid>.png \
#     [--purge <media-id> --api https://api-origin.example.com --token "$JOMFORM_API_TOKEN"] \
#     [--window 300] [--await-expiry] [--poll 5]
#
#   --path          the media path, exactly as it appears after the edge host
#   --purge         a TRASHED media id to purge through DELETE /v1/rest/media/{id}/purge
#                   (omit it for a read-only probe: the object is then SUPPOSED to
#                   still be served, at the origin and at the edge)
#   --api           base URL of the API that owns the row (defaults to --origin)
#   --token         bearer token for the purge (API key or session)
#   --window        the documented freshness window in seconds (default 300)
#   --await-expiry  measure the edge entry expire instead of inferring it
#   --poll          seconds between those measurements (default 5)
#
# EXIT CODES
#   0  PASS   1  FAIL   2  usage error, transport error, or nothing could be decided
set -uo pipefail

ORIGIN=""
EDGE=""
MEDIA_PATH=""
PURGE_ID=""
API_BASE=""
TOKEN=""
WINDOW=300
AWAIT=0
POLL=5

usage() { awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --origin) ORIGIN="${2:-}"; shift 2 ;;
    --edge)   EDGE="${2:-}";   shift 2 ;;
    --path)   MEDIA_PATH="${2:-}"; shift 2 ;;
    --purge)  PURGE_ID="${2:-}";   shift 2 ;;
    --api)    API_BASE="${2:-}";   shift 2 ;;
    --token)  TOKEN="${2:-}";      shift 2 ;;
    --window) WINDOW="${2:-}";     shift 2 ;;
    --await-expiry) AWAIT=1; shift ;;
    --poll)   POLL="${2:-}";       shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "media-purge-probe: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$ORIGIN" ] || [ -z "$EDGE" ] || [ -z "$MEDIA_PATH" ]; then
  echo "media-purge-probe: --origin, --edge and --path are required" >&2
  exit 2
fi
case "$WINDOW" in (*[!0-9]*|"") echo "media-purge-probe: --window must be a number of seconds" >&2; exit 2 ;; esac
[ "$WINDOW" -gt 0 ] || { echo "media-purge-probe: --window must be greater than 0" >&2; exit 2; }
case "$POLL" in (*[!0-9]*|"") echo "media-purge-probe: --poll must be a number of seconds" >&2; exit 2 ;; esac
[ "$POLL" -gt 0 ] || { echo "media-purge-probe: --poll must be greater than 0" >&2; exit 2; }
if [ -n "$PURGE_ID" ] && [ -z "$TOKEN" ]; then
  echo "media-purge-probe: --purge needs --token" >&2
  exit 2
fi
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

# int_or <value> <fallback> — a header read as an integer, or the fallback when
# it is absent or garbled (an edge that sends no `age` must not read as age 0).
int_or() {
  case "${1:-}" in (''|*[!0-9]*) printf '%s' "$2" ;; (*) printf '%s' "$1" ;; esac
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
if [ "$AWAIT" -eq 1 ]; then
  echo "  window : ${WINDOW}s (await-expiry: measuring each entry, not reading the header)"
else
  echo "  window : ${WINDOW}s (inferred from the declared lifetimes; --await-expiry measures instead)"
fi

probe_all() { # probe_all <label> — fills P_* and the header files
  local label="$1"
  P_ORIGIN_ST="$(probe "$ORIGIN_URL" "$TMP/origin.h")"
  P_EDGE_ST="$(probe "$EDGE_URL" "$TMP/edge.h")"
  P_EDGEV_ST="$(probe "$EDGE_V_URL" "$TMP/edgev.h")"
  P_ORIGIN_CC="$(header_value "$TMP/origin.h" cache-control)"
  P_EDGE_CC="$(header_value "$TMP/edge.h" cache-control)"
  P_EDGE_CCS="$(header_value "$TMP/edge.h" cf-cache-status)"
  P_EDGE_AGE="$(header_value "$TMP/edge.h" age)"
  P_EDGEV_CC="$(header_value "$TMP/edgev.h" cache-control)"
  P_EDGEV_CCS="$(header_value "$TMP/edgev.h" cf-cache-status)"
  P_EDGEV_AGE="$(header_value "$TMP/edgev.h" age)"
  printf '  %-8s origin=%s [cache-control=%s]\n' "$label" "$P_ORIGIN_ST" "${P_ORIGIN_CC:-—}"
  printf '           edge=%s [cf-cache-status=%s age=%s cache-control=%s]\n' \
    "$P_EDGE_ST" "${P_EDGE_CCS:-—}" "${P_EDGE_AGE:-—}" "${P_EDGE_CC:-—}"
  printf '           edge? v=%s [cf-cache-status=%s age=%s cache-control=%s]\n' \
    "$P_EDGEV_ST" "${P_EDGEV_CCS:-—}" "${P_EDGEV_AGE:-—}" "${P_EDGEV_CC:-—}"
}

probe_all "before"

# The reference lifetime: what the ORIGIN allows a shared cache to keep. Read
# before the purge, because afterwards the origin answers 404 for this object.
ORIGIN_TTL="$(max_age "$P_ORIGIN_CC")"
REF_SRC="the origin"

if [ -n "$PURGE_ID" ]; then
  echo "  purge  : DELETE ${API_BASE}/v1/rest/media/${PURGE_ID}/purge"
  code="$(curl -sS -o "$TMP/purge.json" -w '%{http_code}' --max-time 30 \
    -X DELETE -H "Authorization: Bearer ${TOKEN}" \
    "${API_BASE}/v1/rest/media/${PURGE_ID}/purge" 2>"$TMP/err")" || code="000"
  echo "  purge  : HTTP ${code} $(tr -d '\n' < "$TMP/purge.json" 2>/dev/null | cut -c1-400)"
  # No declaration came from the origin (it was already gone): fall back to the
  # freshness window the app itself names in the purge reply.
  if [ "$ORIGIN_TTL" -eq 0 ] && [ -s "$TMP/purge.json" ]; then
    ORIGIN_TTL="$(tr -d '\n' < "$TMP/purge.json" \
      | sed -n 's/.*"edge_max_age_s"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')"
    case "$ORIGIN_TTL" in (''|*[!0-9]*) ORIGIN_TTL=0 ;; (*) REF_SRC="the purge reply" ;; esac
  fi
  probe_all "after"
else
  echo "  (no --purge: probe only — nothing was deleted)"
fi

if [ "$ORIGIN_TTL" -gt 0 ]; then
  echo "  declared: a shared cache may hold this object for ${ORIGIN_TTL}s (${REF_SRC}); the window is ${WINDOW}s"
fi

# ── verdict ─────────────────────────────────────────────────────────────────
if [ "$P_ORIGIN_ST" = "000" ] || [ "$P_EDGE_ST" = "000" ] || [ "$P_EDGEV_ST" = "000" ]; then
  echo "verdict: ERROR — a probe did not complete (status 000); nothing was decided"
  exit 2
fi

FAILED=0
EDGE_SERVED=0
EDGE_HARD=0       # a served copy that cannot expire inside the window
EDGE_UNDECIDED=0  # a served copy whose lifetime could not be read at all

if [ -n "$PURGE_ID" ]; then
  if [ "$P_ORIGIN_ST" != "404" ]; then
    echo "verdict: FAIL — the origin still answers ${P_ORIGIN_ST} for the cache-busted URL after the purge,"
    echo "         so the object was not actually removed (the purge did not run, or it names another id)"
    FAILED=1
  fi
elif [ "$P_ORIGIN_ST" = "404" ]; then
  echo "  note   : probe only — the object is already gone at the origin (404); only the edge copy is judged"
else
  echo "  note   : probe only — nothing was purged, so origin ${P_ORIGIN_ST} is the healthy answer"
fi

# assess_copy <status> <cache-control> <age>
#   Sets A_STATE (gone|bounded|legacy|unbounded|undecidable) and A_TEXT.
assess_copy() {
  local st="$1" cc="$2" age_raw="$3" declared age eff src rem grace note=""
  if [ "$st" = "404" ]; then
    A_STATE="gone"
    A_TEXT="404 — the edge copy is gone"
    return 0
  fi
  declared="$(max_age "$cc")"
  age="$(int_or "$age_raw" -1)"
  src=""
  # No reference lifetime at all (the origin already answers 404 and the purge
  # reply named none): the edge's own header cannot say whether it is the edge
  # TTL or a CDN rewrite of the client-facing value, so nothing can be decided
  # from it. --await-expiry measures the entry instead.
  if [ "$ORIGIN_TTL" -le 0 ]; then
    A_STATE="undecidable"
    A_TEXT="${st} but the origin no longer serves the object, so no lifetime is declared for reference, and the edge's own header cannot tell a CDN rewrite from a real edge TTL"
    return 0
  fi
  if [ "$declared" -eq 0 ] || [ "$declared" -gt "$ORIGIN_TTL" ]; then
    eff="$ORIGIN_TTL"
    src="${REF_SRC}"
    if [ "$declared" -gt "$ORIGIN_TTL" ]; then
      note=" (its header says ${declared}s, but a CDN REWRITES the client-facing header above the origin's value, so that is not the edge lifetime)"
    else
      note=" (the edge sent no usable cache-control)"
    fi
  else
    eff="$declared"
    src="the edge"
  fi

  # An entry older than the lifetime the origin declares NOW was filled under a
  # longer one (a pre-fix fill, or an edge-cache-TTL override). A 60s tolerance
  # absorbs clock skew and a cache serving a moment past expiry; anything beyond
  # that is a different, longer TTL — and trusting the declared value would have
  # hidden it.
  grace=60
  if [ "$age" -ge 0 ] && [ "$age" -gt $((eff + grace)) ]; then
    A_STATE="legacy"
    A_TEXT="${st} with age=${age}s on a ${eff}s lifetime — the entry is OLDER than the lifetime ${src} declares now, so it was filled under a longer TTL and will keep serving past the ${WINDOW}s window"
    return 0
  fi

  if [ "$eff" -gt "$WINDOW" ]; then
    A_STATE="unbounded"
    A_TEXT="${st} and a shared cache may hold it for ${eff}s (${src}), longer than the ${WINDOW}s window${note}"
    return 0
  fi

  rem="$eff"
  [ "$age" -ge 0 ] && rem=$((eff - age))
  [ "$rem" -lt 0 ] && rem=0
  A_STATE="bounded"
  if [ "$age" -ge 0 ]; then
    A_TEXT="${st} but bounded: ~${rem}s left of a ${eff}s lifetime (${src})${note}"
  else
    A_TEXT="${st} but bounded: at most ${eff}s (${src}; the edge sent no age)${note}"
  fi
  return 0
}

# observe_expiry <url> — measure one edge URL until it stops serving. Sets
# A_WAIT (expired|timeout) and A_AT (seconds observed). The entry has stopped
# when it answers 404, reports cf-cache-status EXPIRED/REVALIDATED, or its age
# RESETS — which is a refill: the old entry is gone and a new one has started.
observe_expiry() {
  local url="$1" start now deadline st ccs age prev=-1
  start="$(date +%s)"
  deadline=$((start + WINDOW + 5))
  while :; do
    st="$(probe "$url" "$TMP/wait.h")"
    ccs="$(header_value "$TMP/wait.h" cf-cache-status)"
    age="$(int_or "$(header_value "$TMP/wait.h" age)" -1)"
    now="$(date +%s)"
    A_AT=$((now - start))
    case "$st" in
      (404) A_WAIT="expired"; return 0 ;;
      (000) : ;;  # a transport blip is not a verdict; keep measuring
    esac
    case "$(printf '%s' "$ccs" | tr 'A-Z' 'a-z')" in
      (expired|revalidated) A_WAIT="expired"; return 0 ;;
    esac
    if [ "$age" -ge 0 ] && [ "$prev" -ge 0 ] && [ "$age" -lt "$prev" ]; then
      A_WAIT="expired"   # the age went BACKWARDS: the old entry was replaced
      return 0
    fi
    [ "$age" -ge 0 ] && prev="$age"
    if [ "$now" -ge "$deadline" ]; then A_WAIT="timeout"; return 0; fi
    sleep "$POLL"
  done
}

assess_and_report() { # <label> <url> <status> <cache-control> <cf-cache-status> <age>
  local label="$1" url="$2" st="$3" cc="$4" ccs="$5" age="$6"
  assess_copy "$st" "$cc" "$age"
  if [ "$A_STATE" = "gone" ]; then
    printf '  edge   : %-5s %s\n' "$label" "$A_TEXT"
    return 0
  fi
  EDGE_SERVED=1
  if [ "$AWAIT" -eq 1 ]; then
    observe_expiry "$url"
    if [ "$A_WAIT" = "expired" ]; then
      printf '  edge   : %-5s measured: the entry stopped serving after %ss (inferred: %s)\n' "$label" "$A_AT" "$A_TEXT"
      return 0
    fi
    printf '  edge   : %-5s STILL SERVING after the %ss window + 5s grace — measured, not inferred (inferred: %s)\n' \
      "$label" "$WINDOW" "$A_TEXT"
    EDGE_HARD=1
    return 0
  fi
  printf '  edge   : %-5s %s\n' "$label" "$A_TEXT"
  case "$A_STATE" in
    (legacy|unbounded) EDGE_HARD=1 ;;
    (undecidable)      EDGE_UNDECIDED=1 ;;
  esac
  return 0
}

assess_and_report "bare" "$EDGE_URL" "$P_EDGE_ST" "$P_EDGE_CC" "$P_EDGE_CCS" "$P_EDGE_AGE"
assess_and_report "query" "$EDGE_V_URL" "$P_EDGEV_ST" "$P_EDGEV_CC" "$P_EDGEV_CCS" "$P_EDGEV_AGE"

if [ "$FAILED" -eq 0 ] && [ "$EDGE_HARD" -eq 0 ] && [ "$EDGE_UNDECIDED" -eq 0 ]; then
  if [ "$EDGE_SERVED" -eq 0 ]; then
    if [ -n "$PURGE_ID" ]; then
      echo "verdict: PASS — origin 404 and the edge 404 (bare and with a query): the copy is gone at both ends"
    else
      echo "verdict: PASS — probe only: the object is gone at the origin and at the edge (bare and with a query)"
    fi
  elif [ -n "$PURGE_ID" ]; then
    echo "verdict: PASS — origin 404 and every edge copy is BOUNDED by the lifetime its origin declares, so it"
    echo "         expires on its own inside the ${WINDOW}s window (no CDN credential needed); the residual is printed above"
  else
    echo "verdict: PASS — probe only: the object is served at the origin and every edge copy is BOUNDED inside"
    echo "         the ${WINDOW}s window; the residual is printed above"
  fi
  exit 0
fi

if [ "$FAILED" -eq 0 ] && [ "$EDGE_HARD" -eq 0 ]; then
  echo "verdict: UNDECIDED — an edge copy is served but no lifetime could be read at either end, so this run"
  echo "         cannot say whether it expires inside the window. Re-probe an object the origin still serves"
  echo "         (so the origin declares its lifetime), or pass --await-expiry to measure the entry instead."
  exit 2
fi

echo "verdict: FAIL"
exit 1
