#!/usr/bin/env bash
#
# jomform-watch_test.sh — self-test for deploy/monitoring/jomform-watch.sh.
#
# WHY
#   A watchdog is only worth having if it (a) fires when prod is broken and
#   (b) stays quiet when prod is fine. Both halves are asserted here, offline:
#   a stub HTTP server, a fake notifier and a fake ssh/df, so the whole
#   alerting path runs end to end without touching the network or the prod host.
#   An alerting path that has never been observed firing is not proven — this is
#   that observation, in a form that re-runs on every change to the watchdog.
#
#   It also proves the failure modes that make a watcher dangerous: reporting
#   healthy when it cannot alert, and alerting on every single run (which trains
#   the operator to ignore it).
#
# Run:  deploy/monitoring/jomform-watch_test.sh
# Exit: 0 when every assertion holds, 1 otherwise.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WATCH="$HERE/jomform-watch.sh"
[ -f "$WATCH" ] || { echo "FAIL: $WATCH missing" >&2; exit 1; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # desc actual expected
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$2', want '$3')"; fi
}
contains() { # desc haystack needle
  if printf '%s' "$2" | grep -qF -- "$3"; then ok "$1"
  else bad "$1 (missing '$3' in: $(printf '%s' "$2" | tr '\n' ' ' | cut -c1-300))"; fi
}
notcontains() {
  if printf '%s' "$2" | grep -qF -- "$3"; then bad "$1 (found '$3')"; else ok "$1"; fi
}

SB="$(mktemp -d /tmp/jomform_watch_test.XXXXXX)"
SERVER_PID=""
cleanup() { [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null; rm -rf "$SB"; }
trap cleanup EXIT

# ── stub HTTP server ────────────────────────────────────────────────────────
# /web and /healthz fail INDEPENDENTLY, driven by $SB/mode, so an assertion can
# name exactly which check should have fired.
cat >"$SB/server.py" <<'PY'
import http.server, os, socketserver, sys
SB = sys.argv[1]
NEXT = b'<html><script src="/_next/static/chunks/a.js"></script></html>'
OK_HEALTH = b'{"service":"jomform","status":"ok"}'

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def _mode(self):
        return open(os.path.join(SB, "mode")).read().strip()
    def _hits(self):
        # Per-URL counters: an assertion about retry counts must not be polluted
        # by the OTHER probe's hits.
        name = "seq_web" if self.path.startswith("/web") else "seq_health"
        nf = os.path.join(SB, name)
        n = int(open(nf).read()) if os.path.exists(nf) else 0
        n += 1
        open(nf, "w").write(str(n))
        return n
    def do_GET(self):
        mode = self._mode()
        is_web = self.path.startswith("/web")
        n = self._hits()
        if is_web:
            if mode == "web_down":
                body, code = b"boom", 500
            elif mode == "web_wrong_body":
                body, code = b"<html>maintenance page</html>", 200
            elif mode == "web_flaky":
                body, code = (NEXT, 200) if n >= 3 else (b"boom", 500)
            elif mode == "health_down":
                body, code = NEXT, 200
            else:
                body, code = NEXT, 200
        else:
            if mode == "health_down":
                body, code = b"boom", 500
            else:
                body, code = OK_HEALTH, 200
        self.send_response(code)
        self.send_header("Content-Type", "text/html")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

socketserver.TCPServer.allow_reuse_address = True
srv = socketserver.TCPServer(("127.0.0.1", 0), H)
open(os.path.join(SB, "port"), "w").write(str(srv.server_address[1]))
srv.serve_forever()
PY
printf 'healthy' >"$SB/mode"
python3 "$SB/server.py" "$SB" &
SERVER_PID=$!
for _ in $(seq 1 50); do [ -s "$SB/port" ] && break; sleep 0.1; done
PORT="$(cat "$SB/port" 2>/dev/null)"
[ -n "$PORT" ] || { echo "FAIL: stub server did not start" >&2; exit 1; }
BASE="http://127.0.0.1:$PORT"

# ── fixtures ────────────────────────────────────────────────────────────────
NOTIFY="$SB/notify.log"
# The fake notifier records ONE LINE per message (the real body is multi-line,
# which would make "how many alerts were sent" unanswerable by counting lines).
cat >"$SB/stub_notify.sh" <<'EOF'
#!/usr/bin/env bash
{ tr '\n' '|'; echo; } >>"$1"
EOF
chmod +x "$SB/stub_notify.sh"
NOTIFY_CMD="cat | $SB/stub_notify.sh $NOTIFY"

READY_OK='printf "%s" "{\"checks\":{\"database\":\"ok\",\"valkey\":\"ok\"},\"status\":\"ok\"}"'
READY_DB_DOWN='printf "%s" "{\"checks\":{\"database\":\"unavailable\",\"valkey\":\"ok\"},\"status\":\"degraded\"}"'
READY_UNREACHABLE='exit 1'
DF_OK='printf "%s\n" "Filesystem 1024-blocks Used Available Capacity Mounted on" "/dev/sda1 202876160 18874368 175000000 10% /"'
DF_FULL='printf "%s\n" "Filesystem 1024-blocks Used Available Capacity Mounted on" "/dev/sda1 202876160 190000000 3000000 92% /"'
DF_UNREADABLE='exit 255'

STATE="$SB/state"; LOG="$SB/watch.log"
write_config() { # token ("" = a config file that names no bot)
  {
    printf '# test config\n'
    printf 'JF_WATCH_TELEGRAM_TOKEN=%s\n' "$1"
    printf 'JF_WATCH_TELEGRAM_CHAT=2103883\n'
  } >"$SB/env"
  chmod 600 "$SB/env"
}

# $1..: extra env assignments for this pass. Always sandboxed: stub server,
# fake notifier, fake ssh/df, private state dir and log, no network.
run() {
  env -i PATH="$PATH" HOME="$HOME" \
    JF_WATCH_ENV_FILE="$SB/env" \
    JF_WATCH_STATE_DIR="$STATE" \
    JF_WATCH_LOG_FILE="$LOG" \
    JF_WATCH_WEB_URL="$BASE/web" \
    JF_WATCH_HEALTH_URL="$BASE/healthz" \
    JF_WATCH_NOTIFY_CMD="$NOTIFY_CMD" \
    JF_WATCH_READY_CMD="$READY_OK" \
    JF_WATCH_DF_CMD="$DF_OK" \
    JF_WATCH_ATTEMPT_DELAY=0 JF_WATCH_TIMEOUT=5 \
    "$@" bash "$WATCH"
}

reset() { # fresh state + log + healthy stub + a configured notifier
  rm -rf "$STATE" "$NOTIFY"
  : >"$LOG"
  printf 'healthy' >"$SB/mode"
  rm -f "$SB/seq_web" "$SB/seq_health"
  write_config dummy
}
alert_count() { [ -f "$NOTIFY" ] && grep -c '^' "$NOTIFY" || echo 0; }
alerts() { [ -f "$NOTIFY" ] && cat "$NOTIFY" || printf ''; }

echo "jomform-watch self-test (sandbox $SB, stub server :$PORT)"
echo "--- healthy path ---"

# 1. Everything fine -> silent, exit 0, and the pass is logged (liveness).
reset
out=$(run); rc=$?
check "healthy run exits 0" "$rc" "0"
check "healthy run sends no alert" "$(alert_count)" "0"
contains "healthy run logs the pass" "$(cat "$LOG")" "level=info"
contains "healthy run records the http code" "$(cat "$LOG")" "web code=200"
contains "healthy run confirms dependencies" "$(cat "$LOG")" "ready database=ok valkey=ok"
contains "healthy run confirms disk headroom" "$(cat "$LOG")" "disk ok"

echo "--- an alert that actually fires ---"

# 2. Web container down -> one alert naming the check, non-zero exit.
reset; printf 'web_down' >"$SB/mode"
out=$(run); rc=$?
check "broken web exits 1" "$rc" "1"
check "broken web sends exactly one alert" "$(alert_count)" "1"
contains "alert names the failing check" "$(alerts)" "DOWN web"
contains "alert carries the http status" "$(alerts)" "code=500"
contains "alert says it is a prod alert" "$(alerts)" "JomForm prod alert"
contains "alert is timestamped locally" "$(alerts)" "+08"

# 3. Same failure on the next pass -> NO second message (edge-triggered), but
#    the run still reports failure.
out=$(run); rc=$?
check "repeat failure still exits 1" "$rc" "1"
check "repeat failure does not re-alert" "$(alert_count)" "1"
contains "repeat failure is logged as suppressed" "$(cat "$LOG")" "alert_suppressed"

# 4. Still down after the re-alert window -> exactly one more message.
out=$(run JF_WATCH_REALERT_SECONDS=0); rc=$?
check "re-alert after window" "$(alert_count)" "2"
contains "re-alert says STILL DOWN" "$(alerts)" "STILL DOWN web"

# 5. Recovery -> a RECOVERED message, then silence again.
printf 'healthy' >"$SB/mode"
out=$(run JF_WATCH_REALERT_SECONDS=0); rc=$?
check "recovery exits 0" "$rc" "0"
check "recovery sends one message" "$(alert_count)" "3"
contains "recovery alert says RECOVERED" "$(alerts)" "RECOVERED web"
out=$(run JF_WATCH_REALERT_SECONDS=0); rc=$?
check "no further alert when healthy" "$(alert_count)" "3"

# 6. 200 with the wrong body is a failure, not a pass: a proxy's error or
#    maintenance page answers 200, and only the page's own marker tells them apart.
reset; printf 'web_wrong_body' >"$SB/mode"
out=$(run); rc=$?
check "wrong page body exits 1" "$rc" "1"
check "wrong page body sends one alert" "$(alert_count)" "1"
contains "wrong page body names marker_missing" "$(alerts)" "marker_missing"

# 7. Two 500s then a 200 -> healthy, proving the retry actually retries.
reset; printf 'web_flaky' >"$SB/mode"
out=$(run JF_WATCH_ATTEMPTS=3); rc=$?
check "transient failure retried to success" "$rc" "0"
check "flaky run sent no alert" "$(alert_count)" "0"
check "three probe attempts were made" "$(cat "$SB/seq_web")" "3"

# 8. The api container down -> a second, independently-named check.
reset; printf 'health_down' >"$SB/mode"
out=$(run); rc=$?
check "api down exits 1" "$rc" "1"
contains "api down names the health check" "$(alerts)" "DOWN health"

echo "--- a URL that cannot answer at all ---"

# 9. The card's own instruction: point the probe at a bad URL.
reset
out=$(run JF_WATCH_WEB_URL="http://127.0.0.1:1/nothing"); rc=$?
check "unreachable url exits 1" "$rc" "1"
contains "unreachable url alerts" "$(alerts)" "DOWN web"
contains "unreachable url reports code=000" "$(alerts)" "code=000"

echo "--- readiness (database/valkey), which /healthz cannot see ---"

# 10. Api container up but its database is gone -> alert. /healthz answers 200
#     in this state, so without this check the watchdog would be silent while
#     login and payments are broken.
reset
out=$(run JF_WATCH_READY_CMD="$READY_DB_DOWN"); rc=$?
check "degraded /readyz exits 1" "$rc" "1"
contains "degraded /readyz alerts" "$(alerts)" "DOWN ready"
contains "degraded /readyz names the dependency" "$(alerts)" "database"

# 11. ssh to prod failing must be a failure, not silently skipped.
reset
out=$(run JF_WATCH_READY_CMD="$READY_UNREACHABLE"); rc=$?
check "unreadable /readyz exits 1" "$rc" "1"
contains "unreadable /readyz alerts" "$(alerts)" "DOWN ready"

echo "--- disk ---"

# 12. Over the threshold -> alert; under -> silence.
reset
out=$(run JF_WATCH_DF_CMD="$DF_FULL"); rc=$?
check "full disk exits 1" "$rc" "1"
contains "full disk alerts" "$(alerts)" "DOWN disk"
contains "full disk alert carries the percentage" "$(alerts)" "used=92%"

reset
out=$(run JF_WATCH_DF_CMD="$DF_OK"); rc=$?
check "space available exits 0" "$rc" "0"
check "space available sends no alert" "$(alert_count)" "0"

# 13. df unreadable -> fail loud (a watcher that cannot measure must not say ok).
reset
out=$(run JF_WATCH_DF_CMD="$DF_UNREADABLE"); rc=$?
check "unreadable df exits 1" "$rc" "1"
contains "unreadable df alerts" "$(alerts)" "DOWN disk"

echo "--- refusal to run blind ---"

# 14. No notifier at all -> exit 2, never a green report.
reset; write_config ""
out=$(env -i PATH="$PATH" HOME="$HOME" \
  JF_WATCH_ENV_FILE="$SB/env" JF_WATCH_STATE_DIR="$STATE" \
  JF_WATCH_LOG_FILE="$LOG" JF_WATCH_WEB_URL="$BASE/web" \
  JF_WATCH_HEALTH_URL="$BASE/healthz" \
  JF_WATCH_READY_CMD="$READY_OK" JF_WATCH_DF_CMD="$DF_OK" \
  JF_WATCH_ATTEMPT_DELAY=0 bash "$WATCH" 2>&1); rc=$?
check "no notifier exits 2" "$rc" "2"
contains "no notifier explains itself" "$out" "refusing to run without an alerting path"
notcontains "no notifier logs no pass" "$(cat "$LOG")" "failed=0"

# 15. A dead alerting backend is exit 3, not a green run: the failure was
#     detected and could not be delivered, which is an incident of its own.
reset; printf 'web_down' >"$SB/mode"
out=$(run JF_WATCH_NOTIFY_CMD="exit 1"); rc=$?
check "undeliverable alert exits 3" "$rc" "3"
contains "undeliverable alert is logged" "$(cat "$LOG")" "alert_undeliverable"

# 16. The token may come from the environment alone, with no config file.
reset
out=$(env -i PATH="$PATH" HOME="$HOME" \
  JF_WATCH_ENV_FILE="$SB/does-not-exist" \
  JF_WATCH_STATE_DIR="$STATE" JF_WATCH_LOG_FILE="$LOG" \
  JF_WATCH_WEB_URL="$BASE/web" JF_WATCH_HEALTH_URL="$BASE/healthz" \
  JF_WATCH_TELEGRAM_TOKEN=envtoken JF_WATCH_TELEGRAM_CHAT=12345 \
  JF_WATCH_NOTIFY_CMD="$NOTIFY_CMD" \
  JF_WATCH_READY_CMD="$READY_OK" JF_WATCH_DF_CMD="$DF_OK" \
  JF_WATCH_ATTEMPT_DELAY=0 bash "$WATCH"); rc=$?
check "env-only config runs" "$rc" "0"

echo "--- heartbeat digest ---"

# 17. Digest mode sends its own heartbeat, so silence means the watcher died.
reset
out=$(run); rc=$?
out=$(run JF_WATCH_MODE=digest); rc=$?
check "digest exits 0" "$rc" "0"
check "digest sends one message" "$(alert_count)" "1"
contains "digest is identifiable" "$(alerts)" "JomForm watchdog alive"
contains "digest counts the passes" "$(alerts)" "checks run: 1"
contains "digest reports the disk" "$(alerts)" "10% used on /"

echo "--- log rotation ---"

# 18. A log over the cap is trimmed in place (same inode: a cron-held fd, or
#     `tail -f`, keeps working).
reset
: >"$LOG"
head -c 4096 /dev/zero | tr '\0' 'x' >"$LOG"
inode_before=$(stat -c %i "$LOG")
out=$(run JF_WATCH_LOG_MAX_BYTES=1024); rc=$?
inode_after=$(stat -c %i "$LOG")
size=$(wc -c <"$LOG")
if [ "$size" -lt 4096 ] && [ "$inode_before" = "$inode_after" ]; then
  ok "log rotated in place under the cap ($size bytes, same inode)"
else
  bad "log rotation (size=$size inode $inode_before -> $inode_after)"
fi

echo
printf 'jomform-watch self-test: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
exit 0
