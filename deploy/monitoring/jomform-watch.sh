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

check_http() { # name url marker
  local name="$1" url="$2" marker="$3" i
  for i in $(seq 1 "$ATTEMPTS"); do
    if probe_url "$url"; then
      if [ -z "$marker" ] || grep -qF -- "$marker" <<<"$PROBE_BODY"; then
        printf '%s code=%s bytes=%s' "$name" "$PROBE_CODE" "${#PROBE_BODY}"
        return 0
      fi
      [ "$i" = "$ATTEMPTS" ] && {
        printf '%s code=%s marker_missing=%s body=%s' "$name" "$PROBE_CODE" \
          "$marker" "$(printf '%s' "$PROBE_BODY" | tr -d '\n' | cut -c1-120)"
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
  body="🩺 JomForm watchdog alive
$(alert_subject)
$(TZ=Asia/Kuala_Lumpur date '+%Y-%m-%d %H:%M %Z')
checks run: $(counter_get checks_total)
failing runs: $(counter_get failures_total)
alerts sent: $(counter_get alerts_total)
prod disk: ${disk_line:-unreadable}
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
