#!/usr/bin/env bash
#
# jomform-watch.sh — one-file uptime + disk watchdog for JomForm production.
#
# WHY THIS EXISTS
#   Production had no monitoring at all: no uptime check, no error alerting, no
#   disk alarm. An outage at 03:00 would have been discovered by a customer, not
#   by us. This is the smallest thing that removes that blindness: one bash
#   script, one cron entry, no monitoring stack, no daemon, no dependency beyond
#   curl + ssh.
#
# WHAT IT CHECKS (every run)
#   1. web     GET  $JF_WATCH_WEB_URL    (default https://jomform.com/)
#              Cloudflare -> Traefik -> web container, i.e. the path a real
#              visitor takes, so a dead web container is caught (a dead web
#              container leaves /healthz answering 200).
#   2. health  GET  $JF_WATCH_HEALTH_URL (default https://jomform.com/healthz)
#              Cloudflare -> Traefik -> api container.
#   3. ready   the api container's own /readyz, read over ssh on the prod host
#              (database + valkey). /healthz answers 200 while the database is
#              down, which would leave login and payments broken and this script
#              silent.
#   4. disk    `df` on the prod host over ssh, alert above JF_WATCH_DISK_MAX.
#   5. board   the agent host's Kanban board: `task_runs` rows that ended
#              crashed / gave_up / spawn_failed inside a trailing window. This is
#              the host-fault heartbeat a real incident lacked: a broken spawn
#              interpreter produced 110 crashed runs (9 in the 3 h before it was
#              found) and NOTHING paged, because no monitor read task_runs. The
#              only signal was a gateway journal WARNING ("dispatcher stuck … 0
#              workers spawned") that ALSO carried `Last tick held back:
#              active_pr=NN`, so it reads as "the respawn guard is doing its
#              job" while the real cause was the host, and no page ever arrived.
#              The detail states the split — total failures, how many distinct
#              cards and LANES (`tasks=`/`profiles=`), how many the dispatcher
#              itself attributed to the host (`infra=`), and how many died on the
#              spawn-import signature — so a reader is told which of the two they
#              are looking at instead of having to infer it.
#
#              Two shapes of "the board check is off", deliberately different:
#                * JF_WATCH_KANBAN_DB set but unreadable/unparseable → a FAILURE
#                  (alert), like an unreadable df: a watcher that cannot measure
#                  must not say ok.
#                * never configured and no board DB on this host → disabled, and
#                  the daily digest SAYS so (`board: NOT MEASURED here`), because
#                  a silently-disabled check is the original blindness.
#
# DELIVERY
#   Telegram Bot API, called straight from this script. The alert path must not
#   depend on the agent stack it is watching.
#
# NOISE CONTROL
#   Alerts are per-check and edge-triggered: one message when a check goes DOWN,
#   one when it RECOVERS, plus a repeat every JF_WATCH_REALERT_SECONDS while it
#   stays down. The daily digest (JF_WATCH_MODE=digest) is the watchdog's own
#   heartbeat: if the digest stops arriving, the watcher is dead too.
#
# USAGE
#   jomform-watch.sh                          # one check pass (cron: */5)
#   JF_WATCH_MODE=digest jomform-watch.sh     # heartbeat (cron: daily)
#
# EXIT CODES
#   0 all checks healthy (or digest sent)
#   1 at least one check failed (an alert was attempted)
#   2 misconfiguration — no notifier, or nothing to check (NEVER a silent pass:
#     a watcher that cannot alert must fail loudly rather than report "ok")
#   3 an alert could not be delivered
#
# CONFIG
#   Sourced from $JF_WATCH_ENV_FILE (default /etc/jomform-watch.env, chmod 600).
#   Every key has a default; see jomform-watch.env.example in this directory.

set -uo pipefail

CONFIG_FILE="${JF_WATCH_ENV_FILE:-/etc/jomform-watch.env}"
if [ -r "$CONFIG_FILE" ]; then
  set -a
  # shellcheck source=/dev/null
  . "$CONFIG_FILE"
  set +a
fi

MODE="${JF_WATCH_MODE:-check}"

WEB_URL="${JF_WATCH_WEB_URL:-https://jomform.com/}"
WEB_MARKER="${JF_WATCH_WEB_MARKER:-/_next/static/}"
HEALTH_URL="${JF_WATCH_HEALTH_URL:-https://jomform.com/healthz}"
HEALTH_MARKER="${JF_WATCH_HEALTH_MARKER:-service}"
ALLOW_NO_HTTP="${JF_WATCH_ALLOW_NO_HTTP:-0}"

SSH_TARGET="${JF_WATCH_SSH_TARGET:-root@203.0.113.10}"
SSH_OPTS="${JF_WATCH_SSH_OPTS:--o BatchMode=yes -o ConnectTimeout=10}"
COMPOSE_FILE="${JF_WATCH_COMPOSE_FILE:-/opt/jomform/compose.yml}"
READY_URL_LOCAL="${JF_WATCH_READY_URL_LOCAL:-http://127.0.0.1:8080/readyz}"
DISK_PATHS="${JF_WATCH_DISK_PATHS:-/}"
DISK_MAX="${JF_WATCH_DISK_MAX:-85}"

TIMEOUT="${JF_WATCH_TIMEOUT:-15}"
ATTEMPTS="${JF_WATCH_ATTEMPTS:-3}"
ATTEMPT_DELAY="${JF_WATCH_ATTEMPT_DELAY:-5}"
REALERT="${JF_WATCH_REALERT_SECONDS:-1800}"

STATE_DIR="${JF_WATCH_STATE_DIR:-/var/lib/jomform-watch}"
LOG_FILE="${JF_WATCH_LOG_FILE:-/var/log/jomform-watch.log}"
LOG_MAX_BYTES="${JF_WATCH_LOG_MAX_BYTES:-1048576}"

TG_TOKEN="${JF_WATCH_TELEGRAM_TOKEN:-}"
TG_CHATS="${JF_WATCH_TELEGRAM_CHAT:-}"
WATCH_HOST="${JF_WATCH_HOST:-$(hostname)}"

# ── the Kanban board check (check 5) ────────────────────────────────────────
# On an agent host the DEFAULT path is right and the check arms itself; on the
# prod host no board exists and it stays disabled (and the digest says so).
#   JF_WATCH_KANBAN_DB=auto          auto-detect the board DB (default)
#   JF_WATCH_KANBAN_DB=<path>        watch this exact board DB
#   JF_WATCH_KANBAN_DB=off           disable the board check explicitly
KANBAN_DB_CFG="${JF_WATCH_KANBAN_DB:-auto}"
# Tried in order when KANBAN_DB_CFG=auto; the first existing file wins.
KANBAN_DB_CANDIDATES="${JF_WATCH_KANBAN_DB_CANDIDATES:-/root/.hermes/kanban/boards/jomform/kanban.db /root/.hermes/kanban.db}"
KANBAN_WINDOW="${JF_WATCH_KANBAN_WINDOW_SECONDS:-3600}"   # trailing window
KANBAN_THRESHOLD="${JF_WATCH_KANBAN_THRESHOLD:-3}"         # failures in the window
# sqlite3 argv before the SQL, so the test can substitute it with a stub.
KANBAN_SQLITE_OVERRIDE="${JF_WATCH_KANBAN_SQLITE_CMD:-}"

# Test seams: full command lines that replace the real transport. Used by
# jomform-watch_test.sh; unset in production.
NOTIFY_CMD_OVERRIDE="${JF_WATCH_NOTIFY_CMD:-}"
READY_CMD_OVERRIDE="${JF_WATCH_READY_CMD:-}"
DF_CMD_OVERRIDE="${JF_WATCH_DF_CMD:-}"

# ─── logging ────────────────────────────────────────────────────────────────

log() {
  local lvl="$1"; shift
  local line
  line="$(printf '%s level=%-5s host=%s %s' "$(date -u +%FT%TZ)" "$lvl" "$WATCH_HOST" "$*")"
  printf '%s\n' "$line"
  # The log file is written by the script itself, not by a cron redirect: a
  # cron redirect is invisible to a manual run, and this log is the evidence
  # that the watchdog is alive between alerts.
  [ -n "$LOG_FILE" ] && printf '%s\n' "$line" >>"$LOG_FILE" 2>/dev/null
  return 0
}

rotate_log() {
  [ -n "$LOG_FILE" ] || return 0
  [ -f "$LOG_FILE" ] || return 0
  local size
  size=$(wc -c <"$LOG_FILE" 2>/dev/null || echo 0)
  [ "$size" -gt "$LOG_MAX_BYTES" ] || return 0
  # Keep the tail half. In-place rewrite (not mv) so the cron redirect, which is
  # already open with O_APPEND, keeps writing to the same inode.
  local tmp
  tmp="$(mktemp "${LOG_FILE}.XXXXXX")" || return 0
  tail -c $((LOG_MAX_BYTES / 2)) "$LOG_FILE" >"$tmp" && cat "$tmp" >"$LOG_FILE"
  rm -f "$tmp"
}

# ─── state (counters + per-check edge state) ────────────────────────────────

COUNTERS_FILE=""
ensure_state_dir() {
  [ -d "$STATE_DIR" ] || mkdir -p "$STATE_DIR" 2>/dev/null
  COUNTERS_FILE="$STATE_DIR/counters"
  [ -f "$COUNTERS_FILE" ] || printf 'checks_total=0\nfailures_total=0\nalerts_total=0\n' >"$COUNTERS_FILE"
}

counter_get() { # name -> value on stdout
  local v
  v=$(sed -n "s/^$1=//p" "$COUNTERS_FILE" 2>/dev/null | tail -1)
  printf '%s' "${v:-0}"
}

counter_add() { # name delta
  local name="$1" delta="${2:-1}" cur new tmp
  cur=$(counter_get "$name")
  new=$((cur + delta))
  tmp="$(mktemp "$STATE_DIR/counters.XXXXXX")" || return 0
  sed "s/^$name=.*/$name=$new/" "$COUNTERS_FILE" >"$tmp" 2>/dev/null \
    || printf 'checks_total=0\nfailures_total=0\nalerts_total=0\n' >"$tmp"
  mv -f "$tmp" "$COUNTERS_FILE"
}

state_file() { printf '%s/%s.state' "$STATE_DIR" "$1"; }

# ─── probes ─────────────────────────────────────────────────────────────────

PROBE_CODE=""
PROBE_BODY=""
PROBE_ERR=""

probe_url() { # url
  local url="$1" out code err
  out="$(mktemp)" || { PROBE_ERR="mktemp failed"; return 1; }
  err="$(mktemp)" || { rm -f "$out"; PROBE_ERR="mktemp failed"; return 1; }
  code=$(curl -sS --max-time "$TIMEOUT" -o "$out" -w '%{http_code}' "$url" 2>"$err")
  local rc=$?
  PROBE_BODY="$(cat "$out" 2>/dev/null)"
  PROBE_ERR="$(tr '\n' ' ' <"$err" 2>/dev/null)"
  rm -f "$out" "$err"
  if [ $rc -ne 0 ]; then
    PROBE_CODE="000"
    return 1
  fi
  PROBE_CODE="$code"
  [ "$code" = "200" ] || return 1
  return 0
}

remote_ready() {
  if [ -n "$READY_CMD_OVERRIDE" ]; then
    bash -c "$READY_CMD_OVERRIDE"
    return $?
  fi
  local cmd="docker compose -f $COMPOSE_FILE exec -T api wget -qO- $READY_URL_LOCAL"
  if [ "$SSH_TARGET" = "local" ] || [ "$SSH_TARGET" = "-" ]; then
    # Installed ON the prod host: no ssh hop, and no dependency on this host's
    # key being authorised (the watchdog must not be able to break itself by
    # losing an ssh key).
    # shellcheck disable=SC2086
    bash -c "$cmd < /dev/null"
    return $?
  fi
  # The `< /dev/null` matters: `docker compose exec -T` reads stdin, and without
  # it the compose client consumes the ssh channel and the probe hangs.
  # shellcheck disable=SC2029,SC2086
  ssh $SSH_OPTS "$SSH_TARGET" "$cmd < /dev/null"
}

remote_df() { # path
  local path="$1"
  if [ -n "$DF_CMD_OVERRIDE" ]; then
    bash -c "$DF_CMD_OVERRIDE"
    return $?
  fi
  if [ "$SSH_TARGET" = "local" ] || [ "$SSH_TARGET" = "-" ]; then
    df -P -k "$path"
    return $?
  fi
  # shellcheck disable=SC2029,SC2086
  ssh $SSH_OPTS "$SSH_TARGET" "df -P -k $path"
}

# ─── notification ───────────────────────────────────────────────────────────

send_message() { # body
  local body="$1"
  if [ -n "$NOTIFY_CMD_OVERRIDE" ]; then
    printf '%s' "$body" | bash -c "$NOTIFY_CMD_OVERRIDE"
    return $?
  fi
  [ -n "$TG_TOKEN" ] && [ -n "$TG_CHATS" ] || return 2
  # Trim to something no Telegram client will reject, in case a probe body
  # leaked a long response into the text.
  [ "${#body}" -le 3500 ] || body="${body:0:3500}…"
  local chat rc=0 resp ok=1
  IFS=',' read -ra _chats <<<"$TG_CHATS"
  for chat in "${_chats[@]}"; do
    [ -n "$chat" ] || continue
    # The API's ok flag is read from a herestring, NOT `printf '%s' "$resp" |
    # grep -q`: `grep -q` exits at the FIRST match, so the writer is still
    # writing when the pipe closes, takes SIGPIPE (141), and under this file's
    # `set -uo pipefail` the pipeline then reports 141 although grep MATCHED —
    # which would make a DELIVERED alert read as `notify_failed` (worst case: a
    # false page, plus `exit 3` on a healthy notifier). A herestring gives grep a
    # file descriptor, so there is no writer process to signal. Same shape at the
    # probe-body marker below and at the /readyz reads further down.
    if resp=$(curl -sS --max-time "$TIMEOUT" -X POST \
      "https://api.telegram.org/bot${TG_TOKEN}/sendMessage" \
      --data-urlencode "chat_id=${chat}" \
      --data-urlencode "text=${body}" 2>&1) \
      && grep -q '"ok":true' <<<"$resp"; then
      log info "notify_sent chat=$chat $(printf '%s' "$resp" \
        | sed -n 's/.*"message_id":\([0-9]*\).*/message_id=\1/p')"
    else
      log error "notify_failed chat=$chat resp=$(printf '%s' "$resp" | tr -d '\n' | cut -c1-200)"
      ok=0
    fi
  done
  [ "$ok" = 1 ] || rc=1
  return $rc
}

notify_configured() {
  [ -n "$NOTIFY_CMD_OVERRIDE" ] || { [ -n "$TG_TOKEN" ] && [ -n "$TG_CHATS" ]; }
}

# ─── checks ─────────────────────────────────────────────────────────────────
# Each check_* prints a detail line and returns 0 (healthy) or 1 (failed).

# The marker verdict, and the two ways the OLD one could be wrong.
#
# `marker_missing` is a claim about the WHOLE body, so it may only be printed when
# a matcher actually RAN and found nothing (grep status 1). A `grep` that could not
# judge (status above 1 — it exits 2/127 when it cannot run at all, the shape this
# repo's own monitor suite pins) used to take the same branch and print
# `marker_missing=<marker> body=<first 120 bytes>`: absence asserted by a matcher
# that never ran, beside a slice constructed not to contain the evidence, with no
# way for a reader to tell the two apart.
#
# So presence is re-measured over the UNTRUNCATED body (`grep -cF`) and the verdict
# says which of the three it is:
#   marker_missing    a matcher ran and found nothing
#   marker_present    the marker IS there, so it is the MATCHER — not the page —
#                     that failed. The first thing a false page has to answer.
#   marker_unjudged   neither could be established
# The body is still trimmed for DISPLAY (a 200 KB page in a summary line is
# unhelpful), but the line now carries the haystack's size, so a printed slice can
# never be read as all of the evidence.
marker_verdict() { # name code marker matcher_status
  local name="$1" code="$2" marker="$3" mrc="$4" n crc=0 body
  n=$(grep -cF -- "$marker" <<<"$PROBE_BODY" 2>/dev/null) || crc=$?
  body="$(printf '%s' "$PROBE_BODY" | tr -d '\n' | cut -c1-120)"
  if [ "$crc" = 0 ] && [ "$n" -gt 0 ]; then
    printf '%s code=%s marker_present=%s matcher_status=%s body_bytes=%s body=%s' \
      "$name" "$code" "$marker" "$mrc" "${#PROBE_BODY}" "$body"
  elif [ "$mrc" = 1 ] || [ "$crc" = 1 ]; then
    printf '%s code=%s marker_missing=%s body_bytes=%s body=%s' \
      "$name" "$code" "$marker" "${#PROBE_BODY}" "$body"
  else
    printf '%s code=%s marker_unjudged=%s matcher_status=%s body_bytes=%s body=%s' \
      "$name" "$code" "$marker" "$mrc" "${#PROBE_BODY}" "$body"
  fi
}

check_http() { # name url marker
  local name="$1" url="$2" marker="$3" i mrc
  for i in $(seq 1 "$ATTEMPTS"); do
    if probe_url "$url"; then
      # Capture grep's own status instead of letting the `if` collapse "found
      # nothing" into every other non-zero: only status 1 licenses the word
      # "missing" (see marker_verdict above).
      mrc=0
      [ -z "$marker" ] || grep -qF -- "$marker" <<<"$PROBE_BODY" || mrc=$?
      if [ "$mrc" = 0 ]; then
        printf '%s code=%s bytes=%s' "$name" "$PROBE_CODE" "${#PROBE_BODY}"
        return 0
      fi
      [ "$i" = "$ATTEMPTS" ] && {
        marker_verdict "$name" "$PROBE_CODE" "$marker" "$mrc"
        return 1
      }
    else
      [ "$i" = "$ATTEMPTS" ] && {
        printf '%s code=%s err=%s' "$name" "$PROBE_CODE" \
          "$(printf '%s' "$PROBE_ERR" | cut -c1-160)"
        return 1
      }
    fi
    sleep "$ATTEMPT_DELAY"
  done
  return 1
}

check_ready() { # prints detail, returns 0/1
  local i out
  for i in $(seq 1 "$ATTEMPTS"); do
    if out="$(remote_ready 2>&1)" \
      && grep -q '"database":"ok"' <<<"$out" \
      && grep -q '"valkey":"ok"' <<<"$out"; then
      printf 'ready database=ok valkey=ok'
      return 0
    fi
    [ "$i" = "$ATTEMPTS" ] && {
      printf 'ready body=%s' "$(printf '%s' "$out" | tr -d '\n' | cut -c1-160)"
      return 1
    }
    sleep "$ATTEMPT_DELAY"
  done
  return 1
}

check_disk() { # prints one line per over-threshold path, returns 0/1
  local i out line pct mount path header rc=0
  for i in $(seq 1 "$ATTEMPTS"); do
    if out="$(remote_df "$DISK_PATHS" 2>&1)" && [ -n "$out" ]; then
      break
    fi
    if [ "$i" = "$ATTEMPTS" ]; then
      printf 'disk unreadable err=%s' "$(printf '%s' "$out" | tr -d '\n' | cut -c1-160)"
      return 1
    fi
    sleep "$ATTEMPT_DELAY"
  done
  header=1
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    if [ "$header" = 1 ]; then header=0; continue; fi  # skip the "Filesystem" row
    pct=$(printf '%s' "$line" | awk '{gsub(/%/,"",$5); print $5}')
    mount=$(printf '%s' "$line" | awk '{print $6}')
    case "$pct" in ''|*[!0-9]*) continue ;; esac
    if [ "$pct" -gt "$DISK_MAX" ]; then
      printf 'disk path=%s used=%s%% max=%s%%' "${mount:-?}" "$pct" "$DISK_MAX"
      rc=1
    fi
  done <<<"$out"
  return $rc
}

# ─── check 5: the Kanban board's host-fault rate ────────────────────────────
# WHY: 110 runs ended `crashed` on the live board (9 in the 3 h before anyone
# noticed) and no monitor read `task_runs` at all. The fault was in the HOST —
# the dispatcher spawned workers on an interpreter that could not import
# `hermes_cli` — so the board filled with cards that each looked individually
# broken while nothing about the host was watched.
#
# WHAT IS COUNTED: `task_runs.outcome` in ('crashed','gave_up','spawn_failed') —
# the three host-fault outcomes. Deliberately NOT counted:
#   * 'timed_out','reclaimed','blocked','review_requested','changes_requested'
#     — a card that ran out of budget or was deliberately stopped is card-level
#     work, not a host fault; paging on those is the noise that trains an
#     operator to ignore the alert.
#   * `infrastructure: true` runs are not a separate EXCLUSION — the dispatcher
#     marks a host that REFUSED the spawn with that flag (no restart-safe scope,
#     `RestartSafeScopeUnavailable`, #114720), and those are real host faults, so
#     they count here too. They are reported separately in `infra=` because that
#     flag is the dispatcher's own attribution and is a smaller set than the host
#     faults this check is really about.
#
# The detail also carries BREADTH — `tasks=` (distinct task ids) and `profiles=`
# (distinct lanes). Breadth is the discriminator the dispatcher's own flag cannot
# give: `infrastructure: true` only marks a spawn the host REFUSED
# (`RestartSafeScopeUnavailable`), while the incident's 110 runs were ordinary
# `nonzero_exit` crashes that look exactly like a card failing. What made them a
# HOST fault is that they hit MANY cards across MANY lanes at once (dev, qa,
# merger, deployer) with one signature. Three crashes on one card is that card's
# problem; three crashes on three different lanes inside one window is the host.
# So the alert states the split instead of leaving the reader to infer it — the
# inference is exactly what went wrong during the incident.

# kanban_db_resolve — the board DB to read, or "" to disable the check.
KANBAN_DB=""
KANBAN_DB_DISABLED_REASON=""
resolve_kanban_db() {
  local cand
  case "$KANBAN_DB_CFG" in
    ""|auto) for cand in $KANBAN_DB_CANDIDATES; do
               [ -f "$cand" ] && { KANBAN_DB="$cand"; return 0; }
             done
             KANBAN_DB_DISABLED_REASON="no board DB at: $KANBAN_DB_CANDIDATES"
             return 0 ;;
    off|none|disabled)
             KANBAN_DB_DISABLED_REASON="disabled by JF_WATCH_KANBAN_DB=$KANBAN_DB_CFG"
             return 0 ;;
    *)       KANBAN_DB="$KANBAN_DB_CFG"; return 0 ;;
  esac
}

kanban_sqlite() { # db_uri sql
  if [ -n "$KANBAN_SQLITE_OVERRIDE" ]; then
    # shellcheck disable=SC2086
    $KANBAN_SQLITE_OVERRIDE "$2"
    return $?
  fi
  sqlite3 -readonly "$1" "$2"
}

check_kanban() { # prints detail, returns 0 healthy / 1 failing / 2 disabled
  resolve_kanban_db
  if [ -z "$KANBAN_DB" ]; then
    printf 'board disabled=1 reason=%s' "$(printf '%s' "$KANBAN_DB_DISABLED_REASON" | tr ' ' '_')"
    return 2   # 2 = not applicable (disabled), NOT a failure
  fi
  # The DB is gone/misnamed: a watcher that cannot measure must not say ok.
  [ -f "$KANBAN_DB" ] || {
    printf 'board unreadable db=%s not_a_file' "$KANBAN_DB"
    return 1
  }
  local uri out rc
  # `mode=ro` in the URI AND `-readonly`: never take a write lock on the live
  # board DB, and never modify the `.db` itself — a pass leaves its bytes and
  # its mtime unchanged (the self-test asserts that fingerprint in BOTH journal
  # modes).
  #
  # Deliberately NOT claimed: that the pass leaves no file behind. A read-only
  # open of a **WAL** database materialises the `-wal`/`-shm` pair — they are
  # what read-only WAL readers share to coordinate — and the boards on an agent
  # host are WAL, so a pass on an idle board creates both files. SQLite's own
  # URI docs name only `immutable=1` as the way to skip locking and change
  # detection (https://www.sqlite.org/uri.html), and that must NOT be used here:
  # it disables change detection while the dispatcher is writing to the board.
  uri="file:$KANBAN_DB?mode=ro"
  # One round-trip, five scalars. `error LIKE '%ModuleNotFoundError%'` is the
  # import-failure signature of the incident that produced this card: the
  # dispatcher spawns on an interpreter that cannot import `hermes_cli`. It is
  # reported because it names the FAULT (and its fix), not just the symptom —
  # a bare crash count sends a reader to the cards instead of to the host.
  #
  # `json_valid(metadata) AND` guards the `infrastructure` read: SQLite RAISES
  # `malformed JSON` on a non-JSON argument to `json_extract` instead of
  # returning NULL, and `task_runs.metadata` is an untyped TEXT column — so ONE
  # row whose metadata is not JSON used to abort this entire SELECT into the
  # `unreadable` branch below. That was both directions wrong at once: a false
  # "board unreadable" page for a board that reads fine, and NO measurement, so
  # a real burst in the same window (and `import_err=`, fetched just below)
  # paged nobody. `json_valid` is 0 for non-JSON and for NULL, and the `AND`
  # short-circuits, so rows with NULL metadata are unaffected.
  out="$(kanban_sqlite "$uri" "
    SELECT
      (SELECT count(*) FROM task_runs
        WHERE outcome IN ('crashed','gave_up','spawn_failed')
          AND started_at > strftime('%s','now') - $KANBAN_WINDOW),
      (SELECT count(DISTINCT task_id) FROM task_runs
        WHERE outcome IN ('crashed','gave_up','spawn_failed')
          AND started_at > strftime('%s','now') - $KANBAN_WINDOW),
      (SELECT count(DISTINCT COALESCE(profile,'-')) FROM task_runs
        WHERE outcome IN ('crashed','gave_up','spawn_failed')
          AND started_at > strftime('%s','now') - $KANBAN_WINDOW),
      (SELECT count(*) FROM task_runs
        WHERE outcome IN ('crashed','gave_up','spawn_failed')
          AND started_at > strftime('%s','now') - $KANBAN_WINDOW
          AND json_valid(metadata)
          AND json_extract(metadata, '\$.infrastructure') IS NOT NULL),
      (SELECT count(*) FROM task_runs
        WHERE outcome = 'crashed'
          AND started_at > strftime('%s','now') - $KANBAN_WINDOW
          AND error LIKE '%ModuleNotFoundError%');" 2>&1)"
  rc=$?
  # A board that is not a board, a locked/missing DB, or an sqlite3 that is not
  # installed all land here: refuse to report ok on a measurement that did not
  # happen, the same shape as an unreadable `df`.
  if [ $rc -ne 0 ] || ! grep -Eq '^[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+$' <<<"$out"; then
    printf 'board unreadable db=%s err=%s' "$KANBAN_DB" \
      "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-160)"
    return 1
  fi
  local total tasks profiles infra importfail
  IFS='|' read -r total tasks profiles infra importfail <<<"$out"
  printf 'board db=%s failures=%s tasks=%s profiles=%s infra=%s window=%ss threshold=%s import_err=%s' \
    "$KANBAN_DB" "$total" "$tasks" "$profiles" "$infra" "$KANBAN_WINDOW" "$KANBAN_THRESHOLD" "$importfail"
  [ "$total" -lt "$KANBAN_THRESHOLD" ] || return 1
  return 0
}

# ─── edge-triggered alerting ────────────────────────────────────────────────

ALERT_LINES=""

record_failure() { # name detail
  local name="$1" detail="$2" sf now last
  sf="$(state_file "$name")"
  now=$(date +%s)
  if [ -f "$sf" ]; then
    last=$(cat "$sf" 2>/dev/null || echo 0)
    case "$last" in ''|*[!0-9]*) last=0 ;; esac
    if [ $((now - last)) -ge "$REALERT" ]; then
      printf '%s' "$now" >"$sf"
      ALERT_LINES="${ALERT_LINES}
STILL DOWN ${name}: ${detail}"
    else
      log info "alert_suppressed check=$name detail=${detail}"
    fi
  else
    printf '%s' "$now" >"$sf"
    ALERT_LINES="${ALERT_LINES}
DOWN ${name}: ${detail}"
  fi
}

record_recovery() { # name detail
  local name="$1" detail="$2" sf
  sf="$(state_file "$name")"
  [ -f "$sf" ] || return 0
  rm -f "$sf"
  ALERT_LINES="${ALERT_LINES}
RECOVERED ${name}: ${detail}"
}

alert_subject() {
  local s
  s=$(( $(counter_get checks_total) ))
  printf 'JomForm watchdog on %s (check #%s)' "$WATCH_HOST" "$s"
}

send_alerts() { # rc from checks
  local failed="$1" header body rc
  [ -n "$ALERT_LINES" ] || return 0
  if [ "$failed" = 1 ]; then
    header="⚠️ JomForm prod alert"
  else
    header="✅ JomForm prod recovered"
  fi
  body="${header}
$(alert_subject)
$(TZ=Asia/Kuala_Lumpur date '+%Y-%m-%d %H:%M %Z')
${ALERT_LINES# }"
  send_message "$body"
  rc=$?
  counter_add alerts_total 1
  if [ $rc -ne 0 ]; then
    log error "alert_undeliverable subject=${header}"
    return 3
  fi
  return 0
}

# ─── main ───────────────────────────────────────────────────────────────────

rotate_log
ensure_state_dir

if ! notify_configured; then
  log error "misconfigured: no notifier (set JF_WATCH_TELEGRAM_TOKEN + JF_WATCH_TELEGRAM_CHAT in $CONFIG_FILE, or JF_WATCH_NOTIFY_CMD)"
  echo "jomform-watch: refusing to run without an alerting path — a watcher that cannot alert must not report ok" >&2
  exit 2
fi

if [ "$MODE" = "digest" ]; then
  status="all ok"
  [ "$(counter_get failures_total)" = 0 ] || status="$(counter_get failures_total) failing run(s) so far"
  disk_line="$(remote_df "$DISK_PATHS" 2>/dev/null | awk 'NR==2{printf "%s used on %s", $5, $6}')"
  # The board check's own state, reported here on purpose: the digest is the
  # watchdog's heartbeat, so "the board check is disabled on this host" is
  # visible daily instead of being an invisible no-op (#110 crashed runs and no
  # monitor, which is the defect this check closes).
  kb_rc=0
  kb_line="$(check_kanban)" || kb_rc=$?
  case "$kb_rc" in
    0) kb_line="board: $kb_line" ;;
    2) kb_line="board: NOT MEASURED here ($kb_line)" ;;
    *) kb_line="board: UNREADABLE — $kb_line" ;;
  esac
  body="🩺 JomForm watchdog alive
$(alert_subject)
$(TZ=Asia/Kuala_Lumpur date '+%Y-%m-%d %H:%M %Z')
checks run: $(counter_get checks_total)
failing runs: $(counter_get failures_total)
alerts sent: $(counter_get alerts_total)
prod disk: ${disk_line:-unreadable}
${kb_line}
last status: ${status}"
  send_message "$body" || exit 3
  log info "digest_sent checks=$(counter_get checks_total) failures=$(counter_get failures_total)"
  exit 0
fi

if [ "$ALLOW_NO_HTTP" != 1 ] && [ -z "$WEB_URL" ] && [ -z "$HEALTH_URL" ]; then
  log error "misconfigured: no http check configured"
  exit 2
fi

failed=0
results=""

if [ -n "$WEB_URL" ]; then
  if d=$(check_http web "$WEB_URL" "$WEB_MARKER"); then
    results="${results}${d} | "
    record_recovery web "$d"
  else
    results="${results}${d} | "
    failed=1
    record_failure web "$d [$WEB_URL]"
  fi
fi

if [ -n "$HEALTH_URL" ]; then
  if d=$(check_http health "$HEALTH_URL" "$HEALTH_MARKER"); then
    results="${results}${d} | "
    record_recovery health "$d"
  else
    results="${results}${d} | "
    failed=1
    record_failure health "$d [$HEALTH_URL]"
  fi
fi

if r=$(check_ready); then
  results="${results}${r} | "
  record_recovery ready "$r"
else
  results="${results}${r} | "
  failed=1
  record_failure ready "$r"
fi

if dl=$(check_disk); then
  # check_disk printed nothing, i.e. every watched path is under the threshold.
  results="${results}disk ok | "
  record_recovery disk "disk under ${DISK_MAX}% on all watched paths"
else
  results="${results}${dl} | "
  failed=1
  record_failure disk "$dl"
fi

# Board check. Three outcomes, deliberately distinct:
#   0 healthy   — counted, recoverable (an alert must be able to say RECOVERED)
#   1 failing   — the host-fault rate is over the threshold, or the board DB
#                 could not be read (a watcher that cannot measure must not say ok)
#   2 disabled  — no board on this host (prod). Reported, never alerted, and the
#                 digest states it: a silently-disabled check is the blindness
#                 this check exists to remove.
kb_rc=0
kb=$(check_kanban) || kb_rc=$?
results="${results}${kb} | "
case "$kb_rc" in
  0|2) record_recovery board "$kb" ;;   # 2 = disabled here; still a recovery
                                        # from any prior DOWN (a check that
                                        # stopped measuring is not "down")
  *) failed=1; record_failure board "$kb" ;;
esac

counter_add checks_total 1
[ "$failed" = 1 ] && counter_add failures_total 1

log info "pass checks_total=$(counter_get checks_total) failed=$failed ${results% | }"

if [ -n "$ALERT_LINES" ]; then
  send_alerts "$failed"
  arc=$?
  [ "$arc" -ne 0 ] && exit 3
fi

[ "$failed" = 1 ] && exit 1
exit 0
