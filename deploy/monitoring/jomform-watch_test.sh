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

# A missing prerequisite is a FAILURE, not a skip: the board check cannot be
# exercised without sqlite3, and a suite that quietly runs without it would
# report green on the one property this card exists to add.
command -v sqlite3 >/dev/null 2>&1 || {
  echo "FAIL: sqlite3 not found — the board-check fixtures cannot be built" >&2
  echo "      (a suite that cannot build its fixture must not report ok)" >&2
  exit 1
}
# Same rule for curl. The real-transport group further down hands every
# non-Telegram URL to the REAL curl by absolute path, and the probes themselves
# are real curl calls — without it that group could not run and would have to be
# skipped, which is the one thing a suite must not silently do here.
command -v curl >/dev/null 2>&1 || {
  echo "FAIL: curl not found — the watchdog's real transport cannot be exercised" >&2
  exit 1
}
REAL_CURL="$(command -v curl)"
# Same rule for the stale temp-key check: its fixture does not answer from a
# state file, it runs the SQL the watchdog COMPOSED against a real database (see
# stub_psql.sh below), so the client and the database are prerequisites. A suite
# that quietly skipped them would report green on the one predicate this check
# exists to prove.
command -v psql >/dev/null 2>&1 || {
  echo "FAIL: psql not found — the temp-key fixture cannot execute the watchdog's SQL" >&2
  exit 1
}
[ -n "${JOMFORM_TEST_DATABASE_URL:-}" ] || {
  echo "FAIL: JOMFORM_TEST_DATABASE_URL is unset — the temp-key fixture needs a real Postgres." >&2
  # Quoted VERBATIM from the advice scripts/preflight.sh emits: the selftest gate
  # sweeps every tracked file that names the test env and requires this exact
  # canonical string, so a hand-rolled variant here is drift (it was, once: the
  # escaped form was reported as a different path). Single quotes on purpose —
  # the operator must read the portable form, not this shell's expansion.
  echo '      source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh} first (the API integration tests need it too)' >&2
  exit 1
}

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # desc actual expected
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$2', want '$3')"; fi
}
# grep_has — the ONE matcher both helpers below use. A herestring, NOT
# `printf '%s' "$1" | grep -qF -- "$2"`: `grep -q` exits at the FIRST match, so
# the writer is still writing when the pipe closes, takes SIGPIPE (141), and
# under this file's `set -uo pipefail` the pipeline then reports 141 although
# grep MATCHED. `contains()` then takes its `bad` branch and prints
# "missing '<needle>' in: <haystack that plainly contains it>" — the false red
# this gate was caught printing on the `alert_suppressed` assertion (PR 669's
# merge tree, PR 801's head), always on the multi-line printed log and never on
# the single-line payloads. A herestring gives grep a file descriptor, so no
# writer process exists to be signalled.
#
# The window is LINE boundaries, not the 64 KiB pipe buffer (a smaller payload
# can flip): what matters is whether the writer still has unread bytes when grep
# exits, i.e. how many writes/lines are still queued. Measured on this host with
# the needle always present — 456 B single-line 0/3000 false reds; 526 B / 15
# lines with the needle on line 1 8/3000; 468 894 B / 20000 matching lines
# 300/300 with no load; the same 20000-line call through a herestring 0/300.
#
# grep's status is returned unchanged so a caller can tell "not found" (1) from
# "could not judge" (>1) — the old wording could not, which is why this took
# three cards to pin down.
grep_has() { # haystack needle
  grep -qF -- "$2" <<<"$1"
}

contains() { # desc haystack needle
  local rc=0
  grep_has "$2" "$3" || rc=$?
  if [ "$rc" = 0 ]; then ok "$1"
  elif [ "$rc" = 1 ]; then bad "$1 (missing '$3' — 0 matches over the whole ${#2}-byte haystack; the slice below is truncated for DISPLAY, it is not the evidence: $(printf '%s' "$2" | tr '\n' ' ' | cut -c1-300))"
  else bad "$1 (the matcher could not judge the haystack: status $rc, needle '$3' — this is NOT 'not found')"
  fi
}
notcontains() {
  local rc=0
  grep_has "$2" "$3" || rc=$?
  if [ "$rc" = 0 ]; then bad "$1 (found '$3')"
  elif [ "$rc" = 1 ]; then ok "$1"
  else bad "$1 (the matcher could not judge the haystack: status $rc, needle '$3')"
  fi
}

SB="$(mktemp -d /tmp/jomform_watch_test.XXXXXX)"
SERVER_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null
  # The temp-key fixture owns a scratch SCHEMA in the shared test database (never
  # a new database), so it is this suite's job to drop it — and only its own.
  if [ -n "${TMPKEY_SCHEMA:-}" ] && [ -n "${JOMFORM_TEST_DATABASE_URL:-}" ]; then
    psql "$JOMFORM_TEST_DATABASE_URL" -qAt \
      -c "drop schema if exists $TMPKEY_SCHEMA cascade" >/dev/null 2>&1
  fi
  rm -rf "$SB"
}
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

# ── the stale temp-API-key fixture (check 6) ────────────────────────────────
# The check's whole value is its WHERE clause (`revoked_at is null`, the 24 h
# window, the two lane prefixes). So this fixture does NOT answer from a state
# file: `stub_psql.sh` runs the SQL the watchdog COMPOSED against a real
# `api_keys` table in a SCRATCH SCHEMA of the shared test database — never a new
# database, never `public`. That is the only construction under which a wrong
# predicate turns this suite red; a bash stub filtering the rows itself would
# re-implement the filter and could stay green while the SQL measured nothing.
# The table mirrors the columns the query reads
# (api/internal/db/migrations/000001_init.up.sql:42-51).
TMPKEY_SCHEMA="jf_watch_tmpkey_p$$"
SQL_LOG="$SB/tmpkey_sql.log"
psql_q() { psql "$JOMFORM_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -qAt "$@"; }
tmpkey_fixture_create() {
  psql_q -c "drop schema if exists $TMPKEY_SCHEMA cascade" \
          -c "create schema $TMPKEY_SCHEMA" \
          -c "create table $TMPKEY_SCHEMA.api_keys (
                name       text        not null,
                created_at timestamptz not null default now(),
                revoked_at timestamptz)" || {
    echo "FAIL: the temp-key scratch schema could not be created in the test database" >&2
    exit 1
  }
}
tmpkey_clear() { psql_q -c "delete from $TMPKEY_SCHEMA.api_keys"; }
tmpkey_add() { # name age_hours live|revoked
  local revoked="NULL"
  [ "${3:-live}" = revoked ] && revoked="now()"
  psql_q -c "insert into $TMPKEY_SCHEMA.api_keys (name, created_at, revoked_at)
             values ('$1', now() - interval '$2 hours', $revoked)"
}
tmpkey_fixture_create

cat >"$SB/stub_psql.sh" <<'EOF'
#!/usr/bin/env bash
# `psql` for the temp-key legs: the REAL client, pointed at the scratch schema, so
# the SQL the watchdog built is the SQL that filters the fixture. `search_path`
# rides in PGOPTIONS on purpose — `-c "set search_path …; select …"` would print
# psql's "SET" command tag on the same stdout, which the watchdog would count as
# a leaked row.
set -uo pipefail
sql="${1:?stub_psql: no SQL argument}"
printf '%s\n' "$sql" >>"$WATCHTEST_SQL_LOG"
export PGOPTIONS="-c search_path=$WATCHTEST_SCHEMA,public"
exec psql "$JOMFORM_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -tA -c "$sql"
EOF
chmod +x "$SB/stub_psql.sh"
cat >"$SB/stub_psql_fail.sh" <<'EOF'
#!/usr/bin/env bash
printf 'psql: error: connection to server at "127.0.0.1" failed: Connection refused\n' >&2
exit 2
EOF
chmod +x "$SB/stub_psql_fail.sh"
PSQL_STUB="bash $SB/stub_psql.sh"
PSQL_FAIL="bash $SB/stub_psql_fail.sh"
# The environment every pass needs for that stub. The suite runs under `env -i`,
# so this array is the only thing that gets it there — and a case can still
# override it by passing a later assignment of the same name.
PSQL_ENV=(JF_WATCH_PSQL_CMD="$PSQL_STUB"
  JOMFORM_TEST_DATABASE_URL="$JOMFORM_TEST_DATABASE_URL"
  WATCHTEST_SCHEMA="$TMPKEY_SCHEMA" WATCHTEST_SQL_LOG="$SQL_LOG")

READY_OK='printf "%s" "{\"checks\":{\"database\":\"ok\",\"valkey\":\"ok\"},\"status\":\"ok\"}"'
READY_DB_DOWN='printf "%s" "{\"checks\":{\"database\":\"unavailable\",\"valkey\":\"ok\"},\"status\":\"degraded\"}"'
READY_UNREACHABLE='exit 1'
DF_OK='printf "%s\n" "Filesystem 1024-blocks Used Available Capacity Mounted on" "/dev/sda1 202876160 18874368 175000000 10% /"'
DF_FULL='printf "%s\n" "Filesystem 1024-blocks Used Available Capacity Mounted on" "/dev/sda1 202876160 190000000 3000000 92% /"'
DF_UNREADABLE='exit 255'

# ── a synthetic Kanban board ────────────────────────────────────────────────
# The board check reads `task_runs` and nothing else, so the fixture is the
# columns it uses. `profile` and `metadata` are there because the detail line
# reports LANE BREADTH and the dispatcher's own `infrastructure` attribution —
# the two things that separate "one card is broken" from "the host is killing
# every spawn".
BOARD="$SB/board.db"
make_board() {
  # Remove the sidecars too: the WAL leg of case 22 deliberately leaves a
  # `-wal`/`-shm` pair behind (that is the measured truth it pins), and a stale
  # `-wal` next to a freshly recreated `.db` is a corrupt fixture — the kind of
  # silent lie this file's fixture self-check exists to catch.
  rm -f "$BOARD" "$BOARD-wal" "$BOARD-shm"
  sqlite3 "$BOARD" "
    CREATE TABLE task_runs (
      id       INTEGER PRIMARY KEY AUTOINCREMENT,
      task_id  TEXT NOT NULL,
      profile  TEXT,
      outcome  TEXT,
      started_at INTEGER NOT NULL,
      ended_at INTEGER,
      metadata TEXT,
      error    TEXT);"
}
board_add() { # task_id profile outcome age_seconds error [metadata]
  local now meta err
  now=$(date +%s)
  if [ -n "${6:-}" ]; then meta="'$6'"; else meta="NULL"; fi
  # Double every single quote: the error strings carry real worker output, which
  # contains apostrophes ("Worker's last output"), and an unescaped one is a SQL
  # syntax error that silently inserts NOTHING — leaving the case asserting on an
  # empty board. Escaped here so a fixture cannot lie about what it loaded.
  err="${5//\'/\'\'}"
  sqlite3 "$BOARD" "INSERT INTO task_runs (task_id,profile,outcome,started_at,ended_at,error,metadata)
    VALUES ('$1','$2','$3',$((now - $4)),$((now - $4 + 1)),'$err',$meta);"
}
# A crash carrying the incident's own signature, so the alert can name the FAULT
# (`import_err=`) instead of only counting symptoms.
IMPORT_ERR="pid 123 exited with code 1 Worker's last output: \"python3: Error while finding module specification for 'hermes_cli.main' (ModuleNotFoundError: No module named 'hermes_cli')\""
CRASH_ERR="pid 124 exited with code 1"

STATE="$SB/state"; LOG="$SB/watch.log"
write_config() { # token ("" = a config file that names no bot) [chat list]
  {
    printf '# test config\n'
    printf 'JF_WATCH_TELEGRAM_TOKEN=%s\n' "$1"
    # The chat list is a second argument because the real-transport group's
    # fan-out leg needs TWO chats, and the config FILE is what the watchdog
    # reads: the script sources it after the environment, so it wins over an
    # env assignment and an env-only override would be silently ignored.
    printf 'JF_WATCH_TELEGRAM_CHAT=%s\n' "${2:-2103883}"
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
    JF_WATCH_KANBAN_DB="${KANBAN_DB_RUN:-auto}" \
    JF_WATCH_KANBAN_DB_CANDIDATES="${KANBAN_CANDIDATES_RUN:-/nonexistent/a.db}" \
    "${PSQL_ENV[@]}" \
    JF_WATCH_ATTEMPT_DELAY=0 JF_WATCH_TIMEOUT=5 \
    "$@" bash "$WATCH"
}

reset() { # fresh state + log + healthy stub + a configured notifier
  rm -rf "$STATE" "$NOTIFY"
  : >"$LOG"
  printf 'healthy' >"$SB/mode"
  rm -f "$SB/seq_web" "$SB/seq_health"
  # The temp-key check runs on EVERY pass, so a row left behind by an earlier
  # case would page a later, unrelated one. Clear the table and the recorded SQL
  # so each case measures only what it plants.
  tmpkey_clear
  : >"$SQL_LOG"
  write_config dummy
  KANBAN_DB_RUN="auto"                          # board check disabled unless a case says otherwise
  KANBAN_CANDIDATES_RUN="/nonexistent/a.db /nonexistent/b.db"
}
alert_count() { [ -f "$NOTIFY" ] && grep -c '^' "$NOTIFY" || echo 0; }
alerts() { [ -f "$NOTIFY" ] && cat "$NOTIFY" || printf ''; }

# pass_status_of <log> -> the `status=` token carried by the per-pass line, or
# empty when the line carries none.
#
# WHY THIS EXISTS: the status word on the per-pass line used to
# be the CONSTANT literal `pass`, emitted verbatim on a run that had already
# paged (`pass checks_total=… failed=1`). This log is the operator's primary
# surface, so `grep -c pass` counted failing runs as healthy and a human skimming
# the line read "pass" before reaching `failed=` — the silent-green shape this
# watchdog exists to remove. The token must therefore be read out of the line and
# compared, never assumed.
#
# The per-pass line is the one carrying `checks_total=`: the digest line uses
# `checks=`, and notify_sent / alert_suppressed carry neither, so a neighbouring
# line can never be mistaken for it. The LAST match wins, so a leg that runs more
# than one pass reads its newest line.
#
# The token is resolved FIELD-WISE, not as a substring: `status=` is the token the
# watchdog writes, and matching the field means some future check detail that
# happens to contain the text `status=` cannot be read as the line's status.
pass_status_of() {
  local line
  line="$(grep 'checks_total=' "$1" 2>/dev/null | tail -n 1)" || true
  [ -n "$line" ] || return 0
  awk '{ for (i = 1; i <= NF; i++) if ($i ~ /^status=/) { print $i; break } }' <<<"$line"
}

# ── the load-independent control for the matcher ─────────────────────────────
# Every `contains`/`notcontains` below reads a payload the WATCHDOG produced, and
# the ones that failed in the wild (the `alert_suppressed` assertion) read the
# multi-line printed log. So the control drives the REAL helpers on exactly that
# shape — many lines, needle on the FIRST line, which is the shape that leaves
# whole lines unread when `grep -q` exits at the first match — in BOTH
# directions: a present needle must be FOUND, an absent needle MISSED. On this
# shape the old pipe form reports the MATCH as missing with no load at all
# (measured: 300/300 false reds at 20000 lines; 8/3000 at 526 B / 15 lines), so
# this is evidence on an idle host rather than a rate that only shows up under
# contention. Without the second direction a `contains` that matched everything
# would look just as green; without the status assertion a helper that could not
# judge at all would look like "not found".
BIG_HAY="$(seq 1 20000 | sed 's/^/probe-needle line /')"
PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
contains "probe: present needle" "$BIG_HAY" "probe-needle line 1" >/dev/null 2>&1
contains "probe: absent needle"  "$BIG_HAY" "probe-needle line 99999" >/dev/null 2>&1
CONTROL_GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: contains() is pipe-free — FOUND the present needle and MISSED the absent one on a 20000-line haystack, needle on line 1" \
  "1/1" "$CONTROL_GOT"

# The matcher must be able to tell "not found" from "could not judge": status 1
# is a missing needle, anything above is a matcher that never ran (under load
# grep can fail to start and exits 127 with a message that would otherwise read
# as a plain miss). Assert both halves of that discriminator directly.
grep_has "some haystack" "absent-needle" >/dev/null 2>&1; rc=$?
check "the matcher reports a genuine miss as status 1" "1" "$rc"
grep -qF -- x "$SB/definitely-not-a-file" >/dev/null 2>&1; rc=$?
if [ "$rc" -gt 1 ]; then
  ok "an unjudgeable grep is a status above 1 (could-not-run), which contains() reports as NOT 'not found'"
else
  bad "an unjudgeable grep returned status $rc — contains() could not tell it apart from a missing needle"
fi

# The printed slice is DISPLAY ONLY — the card's third requirement, asserted in both
# directions. A needle that IS in the haystack but sits past the 300-byte slice the
# FAIL message prints must still be FOUND (if the slice were what got matched, this
# would read as `missing`), and a genuine miss must FAIL with a message that says the
# absence was measured over the WHOLE haystack — so a reader is never handed a
# truncated slice as the evidence for it.
PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
contains "probe: needle past the printed slice" "$BIG_HAY" "probe-needle line 200" >/dev/null 2>&1
DEEP_GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: a needle sitting past the 300-byte display slice is still FOUND (the slice is display, never the evidence)" \
  "$DEEP_GOT" "1/0"

PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
contains "probe: absent needle message" "$BIG_HAY" "probe-needle line 99999" >"$SB/miss.msg" 2>&1
MISS_GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
MISS_MSG="$(cat "$SB/miss.msg")"
check "control: the same haystack still reports a needle it does not contain as a FAIL (the probe is not a tautology)" \
  "$MISS_GOT" "0/1"
contains "the FAIL message grounds the absence in the UNTRUNCATED haystack" \
  "$MISS_MSG" "0 matches over the whole ${#BIG_HAY}-byte haystack"
contains "and it still names the needle it did not find" "$MISS_MSG" "missing 'probe-needle line 99999'"

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
# The status token is a VERDICT, so on a clean pass it must say so — and it is
# read out of the emitted line, not out of the source. This is the healthy half of
# the discriminating pair completed in 13d.
check "healthy run's per-pass line carries status=pass" "$(pass_status_of "$LOG")" "status=pass"

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

# 6b. A matcher that never RAN is not a missing marker (card t_1f09fdda). Before
#     this, a `grep` exiting above 1 took the same branch as a genuine miss and
#     printed `marker_missing=<marker> body=<first 120 bytes>`: absence asserted by
#     a matcher that never ran, beside a slice built not to contain the evidence.
#     Every one of the three false reds recorded on the live gate read that way,
#     which is why tracing them took three cards. The verdict must name the
#     matcher's status instead.
mkdir -p "$SB/fakebin_unjudged"
cat >"$SB/fakebin_unjudged/grep" <<'EOF'
#!/usr/bin/env bash
# A grep that cannot judge anything: status 2, the way grep exits when it cannot
# run (this suite pins that shape above as "could not judge", not "not found").
exit 2
EOF
chmod +x "$SB/fakebin_unjudged/grep"
reset; printf 'web_wrong_body' >"$SB/mode"
out=$(run PATH="$SB/fakebin_unjudged:$PATH" JF_WATCH_ATTEMPTS=1); rc=$?
check "an unjudgeable matcher still fails the check (a watcher that cannot measure must not say ok)" "$rc" "1"
contains "and the verdict says the matcher could not judge" "$(alerts)" "marker_unjudged"
contains "and carries the matcher's status, so the reader sees what happened" "$(alerts)" "matcher_status=2"
notcontains "a matcher that never ran is NOT reported as a missing marker" "$(alerts)" "marker_missing"
notcontains "and the verdict does not read as if the page were judged" "$(alerts)" "marker_present"

# 6c. The card's item 2, on exactly the shape it names: the needle IS in the body
#     and the matcher's status was non-zero. Re-measuring presence over the
#     UNTRUNCATED body has to make the verdict say the marker is THERE (and name
#     the matcher that failed) — never "missing", which is the first question a
#     false page raises.
mkdir -p "$SB/fakebin_flaky"
cat >"$SB/fakebin_flaky/grep" <<'EOF'
#!/usr/bin/env bash
# Judges everything normally EXCEPT the first question about the web marker, which
# exits 3 (a matcher that never ran) exactly once and then gets out of the way.
flag="$(dirname "$0")/.flaked"
for a in "$@"; do
  if [ "$a" = "/_next/static/" ] && [ ! -e "$flag" ]; then
    : >"$flag"; exit 3
  fi
done
exec /usr/bin/grep "$@"
EOF
chmod +x "$SB/fakebin_flaky/grep"
reset
out=$(run PATH="$SB/fakebin_flaky:$PATH" JF_WATCH_ATTEMPTS=1); rc=$?
check "a matcher whose status was not 0 still fails the check (the measurement did not happen)" "$rc" "1"
check "and it pages once, for the web check only" "$(alert_count)" "1"
contains "a marker that IS present is reported as present rather than missing" "$(alerts)" "marker_present=/_next/static/"
contains "and the line names the matcher's status, so the reader sees who failed" "$(alerts)" "matcher_status=3"
notcontains "…the opposite verdict from the one the old message gave on this shape" "$(alerts)" "marker_missing"

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

echo "--- a stale, never-revoked temp API key on prod (a standing write credential) ---"

# The fixtures below are REAL rows in a scratch schema, filtered by the SQL the
# watchdog composed (see stub_psql.sh): every direction asserted here is a
# verdict of that SQL, not of a bash imitation of it.

# 13a. A key younger than the window is an IN-FLIGHT run: legitimate, no page.
#      This is also where the composed SQL is pinned, because a predicate that
#      stopped discriminating would turn 13b-13d red but a predicate that
#      vanished entirely could still look green on an empty table.
reset
tmpkey_add qa-verify-tmp_t_inflight 1 live
out=$(run); rc=$?
check "a 1 h old live temp key (in-flight run) does not page" "$rc" "0"
check "…and sends no alert" "$(alert_count)" "0"
contains "…while the check still MEASURED prod" "$(cat "$LOG")" "tmpkeys stale=0"
contains "the composed SQL requires a LIVE row (revoked_at is null)" \
  "$(cat "$SQL_LOG")" "revoked_at is null"
contains "the composed SQL windows on created_at, not on last use" \
  "$(cat "$SQL_LOG")" "created_at < now() - interval '24 hours'"
contains "the composed SQL matches the qa lane's temp keys" \
  "$(cat "$SQL_LOG")" "name like 'qa-verify-tmp%'"
contains "the composed SQL matches the deploy lane's temp keys" \
  "$(cat "$SQL_LOG")" "name like 'deploy-verify-tmp%'"
# The CLEAN half of the status-token pair, captured here because this leg is a
# genuinely healthy pass on the temp-key check (an in-flight row, nothing leaked).
CLEAN_TOKEN="$(pass_status_of "$LOG")"

# 13b. A soft-revoked temp row of the SAME age is inert by construction (auth is
#      a per-request lookup filtered on `revoked_at IS NULL`): untidy, not a
#      leak, so it must stay silent. This is the exclusion a "just alert on old
#      temp rows" version of this check would get wrong.
reset
tmpkey_add qa-verify-tmp_t_old_revoked 25 revoked
out=$(run); rc=$?
check "a 25 h old REVOKED temp key stays silent" "$rc" "0"
check "…and sends no alert" "$(alert_count)" "0"
contains "…measured, not skipped" "$(cat "$LOG")" "tmpkeys stale=0"

# 13c. The long-lived legitimate keys are not temp keys: an old live `AI Agent
#      Key` row must not page, or the check cries wolf on every pass.
reset
tmpkey_add 'AI Agent Key' 1440 live
out=$(run); rc=$?
check "a 60 day old live 'AI Agent Key' row stays silent" "$rc" "0"
check "…and sends no alert" "$(alert_count)" "0"

# 13d. THE LEAK the card exists for: 25 h old, never revoked, qa lane. Pages
#      once, names the leaked row, and leaves the state file an operator greps.
reset
tmpkey_add qa-verify-tmp_t_deadrun 25 live
out=$(run); rc=$?
check "a 25 h old live qa temp key exits 1" "$rc" "1"
check "…and pages exactly once" "$(alert_count)" "1"
contains "the alert names the check" "$(alerts)" "DOWN tmpkeys"
contains "the alert states the count" "$(alerts)" "stale=1"
contains "the alert names the leaked row so the lane is visible" \
  "$(alerts)" "qa-verify-tmp_t_deadrun"
check "the check's state file is <STATE_DIR>/tmpkeys.state (the name to grep)" \
  "$([ -f "$STATE/tmpkeys.state" ] && echo yes || echo no)" "yes"

# 13d-bis. THE LABEL: this pass FAILED
#         (a page went out, the run exits 1), so the per-pass line's status token
#         must say so. It used to be the constant literal `pass` on every run, so
#         `grep -c pass` counted this line as healthy and a reader skimming it saw
#         "pass" before reaching `failed=`. Each assertion below reads the token
#         the watchdog actually wrote.
FAILING_TOKEN="$(pass_status_of "$LOG")"
check "a FAILING pass's status token is status=FAIL, not pass" "$FAILING_TOKEN" "status=FAIL"
notcontains "a failing pass line never reads status=pass (grep -c pass cannot count it healthy)" \
  "$(cat "$LOG")" "status=pass"
# The pre-fix shape, asserted literally: the constant word sat between `host=` and
# `checks_total=`, so this is the exact text a reader skimmed. It must be gone from
# a failing line — which is why this leg is RED on the pre-fix script and green
# after the fix, unlike the keyed assertion above.
notcontains "a failing pass line carries no bare 'pass' word either" \
  "$(cat "$LOG")" " pass "
check "the clean pass and the failing pass DIFFER in their status token (the whole point)" \
  "$CLEAN_TOKEN|$FAILING_TOKEN" "status=pass|status=FAIL"

# 13e. Edge-triggered like every other check: the same leak on the next 5-minute
#      tick is measured, logged, and NOT re-sent.
out=$(run); rc=$?
check "a repeat pass still exits 1" "$rc" "1"
check "…and does not page a second time" "$(alert_count)" "1"
contains "…the repeat is logged as suppressed" "$(cat "$LOG")" "alert_suppressed"

# 13f. Recovery: the row is hard-deleted -> one RECOVERED message, then silence,
#      and the state file is cleared (so the next leak pages immediately).
psql_q -c "delete from $TMPKEY_SCHEMA.api_keys where name = 'qa-verify-tmp_t_deadrun'"
out=$(run); rc=$?
check "removing the leaked row exits 0" "$rc" "0"
check "…and sends one recovery message" "$(alert_count)" "2"
contains "…naming the check as RECOVERED" "$(alerts)" "RECOVERED tmpkeys"
check "…and clears the state file" \
  "$([ -f "$STATE/tmpkeys.state" ] && echo yes || echo no)" "no"
out=$(run); rc=$?
check "no further message once the leak is gone" "$(alert_count)" "2"

# 13g. BOTH lanes are covered in one pass, and every row is named — the detail
#      is what tells an operator which lane abandoned a credential.
reset
tmpkey_add qa-verify-tmp_t_qa_leak 25 live
tmpkey_add deploy-verify-tmp_t_deploy_leak 30 live
out=$(run); rc=$?
check "two leaked rows in one pass exit 1" "$rc" "1"
contains "the page counts both" "$(alerts)" "stale=2"
contains "…and names the qa row" "$(alerts)" "qa-verify-tmp_t_qa_leak"
contains "…and names the deploy row" "$(alerts)" "deploy-verify-tmp_t_deploy_leak"

# 13h. A database it cannot read is a failed MEASUREMENT, never "no leak": the
#      same rule as an unreadable df. (This is also the shape a wrong password, a
#      stopped postgres container or a dropped ssh connection takes.)
reset
out=$(run JF_WATCH_PSQL_CMD="$PSQL_FAIL"); rc=$?
check "an unreadable api_keys query exits 1" "$rc" "1"
contains "…and says it could not measure" "$(alerts)" "unreadable"
contains "…carrying psql's own error" "$(alerts)" "Connection refused"
notcontains "…and never claims the leak count was zero" "$(alerts)" "stale=0"

# 13i. A misconfigured window must also fail loud rather than interpolate junk
#      into the interval and read the empty result as healthy.
reset
out=$(run JF_WATCH_TMPKEY_MAX_AGE_HOURS=abc); rc=$?
check "a non-numeric max age exits 1" "$rc" "1"
contains "…and names the bad configuration" "$(alerts)" "bad_config"

# 13j. The REAL transport for this check, not the stub. Every leg above replaces
#      the psql command line, which leaves the production shape DEAD as far as
#      this suite is concerned — the same blind spot the Telegram group below was
#      written to close. Here the watchdog builds its own `docker exec … psql …`
#      string and hands it to a PATH-shimmed `docker` that records its argv, so a
#      quoting bug (the SQL split into words on its way to psql) is visible as an
#      argument count instead of as a permanently "unreadable" check in prod.
mkdir -p "$SB/fakebin_docker"
DOCKER_ARGV="$SB/docker_argv"; DOCKER_ROWS="$SB/docker_rows"; DOCKER_STDOUT="$SB/docker_stdout"
cat >"$SB/fakebin_docker/docker" <<'EOF'
#!/usr/bin/env bash
# Fake `docker` for the temp-key real-transport legs: records its argv one word
# per line and answers the psql call from $DOCKER_ROWS (absent = no rows), after
# a banner on STDERR — the stream a leaked row must never be parsed from.
printf '%s\n' "$@" >"$DOCKER_ARGV"
case "$*" in
  *psql*)
    printf 'Warning: Permanently added the host key to the list of known hosts.\n' >&2
    [ -f "$DOCKER_ROWS" ] && cat "$DOCKER_ROWS"
    [ -f "$DOCKER_STDOUT" ] && cat "$DOCKER_STDOUT"
    exit 0 ;;
esac
exit 0
EOF
chmod +x "$SB/fakebin_docker/docker"
reset; : >"$DOCKER_ARGV"; rm -f "$DOCKER_ROWS" "$DOCKER_STDOUT"
out=$(run JF_WATCH_PSQL_CMD= JF_WATCH_SSH_TARGET=local \
  PATH="$SB/fakebin_docker:$PATH" DOCKER_ARGV="$DOCKER_ARGV" DOCKER_ROWS="$DOCKER_ROWS" \
  DOCKER_STDOUT="$DOCKER_STDOUT"); rc=$?
check "the production transport (docker exec … psql) runs and reports healthy" "$rc" "0"
contains "…says it MEASURED the table even though the transport also wrote a banner on STDERR (a warning is not a leaked row)" \
  "$(cat "$LOG")" "tmpkeys stale=0 max_age=24h"
check "…through exactly 12 argv words (exec, container, psql, -U user, -d db, -v ON_ERROR_STOP=1, -tA, -c, SQL)" \
  "$(wc -l <"$DOCKER_ARGV")" "12"
check "…naming the prod container" "$(sed -n '2p' "$DOCKER_ARGV")" "jomform-postgres-1"
check "…the database, as its own argument" "$(sed -n '7p' "$DOCKER_ARGV")" "jomform"
check "…and -v ON_ERROR_STOP=1, without which a SQL error would read as 'no leak'" \
  "$(sed -n '9p' "$DOCKER_ARGV")" "ON_ERROR_STOP=1"
contains "…with the whole SQL arriving as ONE argument (no word-splitting on the way to psql)" \
  "$(sed -n '12p' "$DOCKER_ARGV")" "select name || '@'"
contains "…still carrying the revoked_at predicate" \
  "$(sed -n '12p' "$DOCKER_ARGV")" "revoked_at is null and created_at < now()"

# …and a row arriving through that same real transport is parsed and paged.
reset; : >"$DOCKER_ARGV"
printf '%s\n' 'qa-verify-tmp_t_viadocker@2026-10-21 15:16:36.79' >"$DOCKER_ROWS"
out=$(run JF_WATCH_PSQL_CMD= JF_WATCH_SSH_TARGET=local \
  PATH="$SB/fakebin_docker:$PATH" DOCKER_ARGV="$DOCKER_ARGV" DOCKER_ROWS="$DOCKER_ROWS"); rc=$?
check "a leaked row arriving over the production transport exits 1" "$rc" "1"
contains "…and the page counts it" "$(alerts)" "stale=1"
contains "…naming the row and its age" "$(alerts)" "qa-verify-tmp_t_viadocker@2026-10-21 15:16:36.79"

# 13k. Stdout that is not a row is neither counted (a false page) nor dropped (a
#      lost leak): the check refuses to judge and says so. A psql NOTICE, a
#      locale warning or a truncated line is this shape, and an operator reading
#      "stale=0" off it would be told there is no leak by a check that never
#      parsed an answer.
reset; : >"$DOCKER_ARGV"; rm -f "$DOCKER_ROWS"
printf '%s\n' 'NOTICE: relation "api_keys" was not found, skipping' >"$DOCKER_STDOUT"
out=$(run JF_WATCH_PSQL_CMD= JF_WATCH_SSH_TARGET=local \
  PATH="$SB/fakebin_docker:$PATH" DOCKER_ARGV="$DOCKER_ARGV" DOCKER_ROWS="$DOCKER_ROWS" \
  DOCKER_STDOUT="$DOCKER_STDOUT"); rc=$?
check "output that is not a row exits 1" "$rc" "1"
contains "…and refuses to judge it rather than counting it" "$(alerts)" "output_shape="
notcontains "…never reporting it as a leaked key" "$(alerts)" "stale=1"
notcontains "…nor as a clean measurement" "$(alerts)" "stale=0"
rm -f "$DOCKER_STDOUT"

echo "--- the Kanban board's host-fault rate (the incident this check exists for) ---"

# 13b. Fixture self-check: an escaped apostrophe in a worker error string once
#      made every INSERT fail silently, so the assertions below ran against an
#      EMPTY board and "no alert" read as "the check is fine". Prove the fixture
#      loaded before trusting anything measured off it.
make_board
board_add t_x dev crashed 600 "$IMPORT_ERR"
loaded="$(sqlite3 "$BOARD" "SELECT count(*) FROM task_runs WHERE task_id='t_x';")"
check "board fixture actually loads rows (real worker output with apostrophes)" "$loaded" "1"

# 14. THE REGRESSION: three worker crashes inside the window page. This is the
#     exact shape the card was filed on — 110 crashed runs, 9 in the last 3 h,
#     and nothing alerted because no monitor read task_runs.
reset; make_board
board_add t_a dev crashed 600 "$IMPORT_ERR"
board_add t_b qa  crashed 900 "$IMPORT_ERR"
board_add t_c merger crashed 1200 "$CRASH_ERR"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "board crash burst exits 1" "$rc" "1"
check "board crash burst sends exactly one alert" "$(alert_count)" "1"
contains "board alert names the check" "$(alerts)" "DOWN board"
contains "board alert counts the failures" "$(alerts)" "failures=3"
contains "board alert reports CARD breadth" "$(alerts)" "tasks=3"
contains "board alert reports LANE breadth" "$(alerts)" "profiles=3"
# Two of the three crashes carry the spawn-import signature; the third does not.
# Asserting 2 (not 3, not 0) proves the signature count discriminates rather than
# just repeating the total.
contains "board alert names the spawn-import signature" "$(alerts)" "import_err=2"

# 15. Below the threshold -> silent. One crash on one card is that card's
#     problem, not the host's, and paging on it is the noise that trains an
#     operator to ignore this alert.
reset; make_board
board_add t_a dev crashed 600 "$CRASH_ERR"
board_add t_b qa  crashed 900 "$CRASH_ERR"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "two crashes stay under the threshold" "$rc" "0"
check "two crashes send no alert" "$(alert_count)" "0"
contains "two crashes are still logged and measured" "$(cat "$LOG")" "failures=2"

# 16. A CARD-level failure is not a host fault. `timed_out` (iteration budget),
#     `reclaimed` (deliberately stopped), `blocked` and the review outcomes are
#     the board working as designed; counting them would page on ordinary work.
reset; make_board
board_add t_a dev  timed_out         600 "Iteration budget exhausted (500/500)"
board_add t_b qa   reclaimed         700 "manual_reclaim: PR already merged; work is moot"
board_add t_c dev  blocked           800 ""
board_add t_d dev  review_requested  900 ""
board_add t_e qa   changes_requested 1000 ""
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "card-level outcomes do not page" "$rc" "0"
check "card-level outcomes send no alert" "$(alert_count)" "0"
contains "card-level outcomes are excluded, not merely under the threshold" \
  "$(cat "$LOG")" "failures=0"

# 17. Runs the dispatcher ITSELF attributed to the host are counted, and the
#     count is stated. (`infrastructure: true` = the host refused the spawn —
#     RestartSafeScopeUnavailable, #114720.) Same page, better detail.
reset; make_board
board_add t_a dev spawn_failed 600 "no restart-safe scope" '{"failures": 1, "infrastructure": true}'
board_add t_b qa  spawn_failed 700 "no restart-safe scope" '{"failures": 1, "infrastructure": true}'
board_add t_c dev crashed      800 "$CRASH_ERR"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "infrastructure refusals page" "$rc" "1"
contains "infrastructure refusals are attributed" "$(alerts)" "infra=2"
contains "plain crashes stay in the remaining count" "$(alerts)" "failures=3"

# 18. Recovery: the burst stops -> one RECOVERED message, then silence. Without
#     this the alert is a one-way trip and the operator cannot tell whether the
#     host came back.
reset; make_board
board_add t_a dev crashed 600 "$CRASH_ERR"
board_add t_b qa  crashed 700 "$CRASH_ERR"
board_add t_c dev crashed 800 "$CRASH_ERR"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "burst pages (precondition for recovery)" "$(alert_count)" "1"
make_board  # the burst ages out / the host is fixed
out=$(run); rc=$?
check "board recovery exits 0" "$rc" "0"
check "board recovery sends one message" "$(alert_count)" "2"
contains "board recovery says RECOVERED" "$(alerts)" "RECOVERED board"
out=$(run); rc=$?
check "no repeated board alert when healthy" "$(alert_count)" "2"

# 19. A configured board DB that cannot be read is a FAILURE, not a silent pass —
#     the same shape as an unreadable df. This is also what a misnamed path or a
#     deleted board looks like, and a watcher that cannot measure must not say ok.
reset
KANBAN_DB_RUN="$SB/does-not-exist.db"
out=$(run); rc=$?
check "configured but missing board exits 1" "$rc" "1"
contains "configured but missing board alerts" "$(alerts)" "DOWN board"
contains "the alert says it could not measure" "$(alerts)" "unreadable"

# 20. A board DB that exists but is not a board (an empty file, a directory, an
#     unrelated sqlite DB) is also a failure: the query returned no parsable
#     answer, and "no answer" is not "no crashes".
reset
: >"$SB/notaboard.db"
KANBAN_DB_RUN="$SB/notaboard.db"
out=$(run); rc=$?
check "non-board DB exits 1" "$rc" "1"
contains "non-board DB alerts" "$(alerts)" "DOWN board"

# 20b. THE INPUT THAT ACTUALLY BREAKS THE QUERY: a board that reads fine but
#      holds ONE `task_runs` row whose `metadata` is NOT valid JSON. The fourth
#      scalar is `json_extract(metadata, '$.infrastructure')`, and SQLite raises
#      `malformed JSON` on a non-JSON argument instead of returning NULL — so
#      before `json_valid(metadata) AND` was added, one such row aborted the WHOLE
#      single-round-trip SELECT into
#      `board unreadable db=… err=Error: stepping, malformed JSON`, which is
#      BOTH directions wrong at once:
#        * it pages the operator "unreadable" for a board that reads fine, AND
#        * it stops MEASURING, so a genuine crash burst in the same window pages
#          nobody, and `import_err=` (the spawn-import signature, the scalar
#          named after the incident that produced this check) is lost with the
#          same SELECT.
#      Cases 19/20 cover a missing board and a non-board; neither is this input —
#      the one shape a host fault writes, since a worker that died before it
#      could report carries whatever text its runner left in `metadata`.
#      Two legs, because "it did not crash" is not the property being claimed:
#        (a) it MEASURES and its scalars stay exact — with a non-JSON row and an
#            infrastructure row in the window, `failures=2` (both counted) and
#            `infra=1` (only the attributable one). A guard that swallowed the
#            count, or dropped every row that has metadata, fails here.
#            `metadata IS NULL` is unaffected (SQLite's `AND` short-circuits, so
#            `json_extract` never sees the bad value) and is pinned by case 14,
#            whose whole fixture is NULL metadata.
#        (b) a REAL burst that includes the bad row still PAGES. This is the half
#            that matters most: a guard that hid the error by blinding the check
#            would look green in leg (a).
#      Journal mode is deliberate here: `make_board` is rollback-mode, and the
#      abort is a property of the SELECT, not of the journal — case 22 is where
#      journal mode matters.
reset; make_board
board_add t_bad dev crashed      600 "pid 125 exited with code 1" 'worker died before it wrote a JSON metadata blob'
board_add t_inf dev spawn_failed 800 "no restart-safe scope"      '{"failures": 1, "infrastructure": true}'
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "a non-JSON metadata value does not abort the measurement" "$rc" "0"
contains "the board line still reports the MEASURED count" "$(cat "$LOG")" "board db=$BOARD failures=2"
contains "the infrastructure count stays exact beside a non-JSON row" "$(cat "$LOG")" "infra=1"
notcontains "a good board is not reported unreadable" "$(cat "$LOG")" "board unreadable"
notcontains "the malformed-JSON error never reaches the log" "$(cat "$LOG")" "malformed JSON"
check "and a sub-threshold count is still not paged" "$(alert_count)" "0"

# Same fixture shape, over the threshold: the burst must still page, with every
# scalar intact. (`import_err=` is the fifth scalar in that one SELECT — proving
# it survives here is proving the whole round-trip survived, not just the guard.)
reset; make_board
board_add t_bad dev crashed      600 "pid 125 exited with code 1" 'worker died before it wrote a JSON metadata blob'
board_add t_b   qa  crashed      700 "$IMPORT_ERR"
board_add t_c   dev spawn_failed 800 "no restart-safe scope"      '{"failures": 1, "infrastructure": true}'
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "a burst that includes a non-JSON row still pages" "$rc" "1"
check "and it is exactly one page" "$(alert_count)" "1"
contains "the burst page counts every failure" "$(alerts)" "failures=3"
contains "the burst page keeps the infrastructure attribution" "$(alerts)" "infra=1"
contains "the burst page keeps the spawn-import signature" "$(alerts)" "import_err=1"
notcontains "the burst page is not an unreadable report" "$(alerts)" "unreadable"

# 21. A host with NO board at all (prod) stays quiet AND says so. `auto` must not
#     turn "this host has no Kanban" into a failure, and it must not turn it into
#     an invisible no-op either — the digest names it (asserted in 23).
reset
KANBAN_DB_RUN="auto"; KANBAN_CANDIDATES_RUN="/nonexistent/a.db /nonexistent/b.db"
out=$(run); rc=$?
check "no board on the host exits 0" "$rc" "0"
check "no board on the host sends no alert" "$(alert_count)" "0"
contains "no board is reported as disabled, not healthy" "$(cat "$LOG")" "board disabled=1"

# 22. The board check must never WRITE to the board it watches: a monitor that
#     takes a write lock on the live board DB is a new outage. Assert the
#     fingerprint that IS the guarantee — the `.db`'s bytes (sha256) and its
#     mtime are unchanged by a pass — in BOTH journal modes, because the live
#     boards are WAL and a rollback-mode fixture cannot see the WAL case at all.
#
#     NOT asserted, deliberately: "no -wal/-shm was left behind". That claim was
#     here before and it was only ever true because the fixture is rollback-mode
#     (sqlite deletes its journal on close). A read-only open of a WAL database
#     materialises the `-wal`/`-shm` pair — SQLite's own URI docs name only
#     `immutable=1` as the way to skip locking and change detection
#     (https://www.sqlite.org/uri.html) — so on a WAL board the pair may APPEAR.
#     That is harmless (the open is read-only and takes no write lock) but it is
#     NOT what the old assertion claimed, so instead of pretending it does not
#     happen we pin the direction it actually goes: absent before, present after,
#     and the `.db` byte-identical across it. Asserting it in the direction the
#     old case could not see is what keeps this test from being a tautology.
fingerprint() { # prints "<sha256> <mtime>" — the pair that must survive a pass
  printf '%s %s' "$(sha256sum "$BOARD" | cut -d' ' -f1)" "$(stat -c '%Y' "$BOARD")"
}

reset; make_board
board_add t_a dev crashed 600 "$IMPORT_ERR"
board_add t_b qa  crashed 700 "$IMPORT_ERR"
board_add t_c dev crashed 800 "$IMPORT_ERR"
# The fixture is rollback-mode: prove it, so the two legs below are known to be
# measuring different modes rather than the same one twice.
check "fixture (rollback leg) is journal_mode=delete" \
  "$(sqlite3 "$BOARD" 'PRAGMA journal_mode;')" "delete"
check "fixture (rollback leg) starts with no sidecars" \
  "$([ -e "$BOARD-wal" ] || [ -e "$BOARD-shm" ] && echo yes || echo no)" "no"
BOARD_SHA_BEFORE="$(fingerprint)"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "watching a rollback-mode board exits 1 (it did the work)" "$rc" "1"
check "watching a rollback-mode board does not modify it" \
  "$(fingerprint)" "$BOARD_SHA_BEFORE"
if [ -e "$BOARD-wal" ] || [ -e "$BOARD-shm" ]; then
  bad "a rollback-mode board gained a -wal/-shm pair"
else
  ok "a rollback-mode board gains no -wal/-shm pair"
fi

# The WAL leg — the mode the live boards actually use. WAL-ifying the fixture and
# letting each sqlite3 close (a checkpoint on last-close) leaves the file in WAL
# mode with NO sidecars, so "before" is genuinely absent and a pass is what
# creates them. (Measured on this host: journal_mode=wal, wal=no shm=no after
# the fixture is built.)
reset; make_board
sqlite3 "$BOARD" "PRAGMA journal_mode=WAL;" >/dev/null
board_add t_a dev crashed 600 "$IMPORT_ERR"
board_add t_b qa  crashed 700 "$IMPORT_ERR"
board_add t_c dev crashed 800 "$IMPORT_ERR"
check "fixture (WAL leg) is journal_mode=wal" \
  "$(sqlite3 "$BOARD" 'PRAGMA journal_mode;')" "wal"
check "fixture (WAL leg) starts with no sidecars" \
  "$([ -e "$BOARD-wal" ] || [ -e "$BOARD-shm" ] && echo yes || echo no)" "no"
BOARD_SHA_BEFORE="$(fingerprint)"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
check "watching a WAL board exits 1 (it did the work)" "$rc" "1"
check "watching a WAL board does not modify the .db (bytes + mtime)" \
  "$(fingerprint)" "$BOARD_SHA_BEFORE"
# The measured truth the old assertion could not see, in the direction it goes.
if [ -e "$BOARD-wal" ] && [ -e "$BOARD-shm" ]; then
  ok "a WAL board MATERIALISES the -wal/-shm pair (sqlite read-only WAL behaviour, not a write)"
else
  bad "a WAL board did not materialise -wal/-shm — this leg is no longer measuring the WAL case (fixture or sqlite changed)"
fi

echo "--- refusal to run blind ---"

# 23. No notifier at all -> exit 2, never a green report.
reset; write_config ""
out=$(env -i PATH="$PATH" HOME="$HOME" \
  JF_WATCH_ENV_FILE="$SB/env" JF_WATCH_STATE_DIR="$STATE" \
  JF_WATCH_LOG_FILE="$LOG" JF_WATCH_WEB_URL="$BASE/web" \
  JF_WATCH_HEALTH_URL="$BASE/healthz" \
  JF_WATCH_READY_CMD="$READY_OK" JF_WATCH_DF_CMD="$DF_OK" \
  JF_WATCH_KANBAN_DB=auto \
  JF_WATCH_KANBAN_DB_CANDIDATES=/nonexistent/a.db \
  "${PSQL_ENV[@]}" \
  JF_WATCH_ATTEMPT_DELAY=0 bash "$WATCH" 2>&1); rc=$?
check "no notifier exits 2" "$rc" "2"
contains "no notifier explains itself" "$out" "refusing to run without an alerting path"
notcontains "no notifier logs no pass" "$(cat "$LOG")" "failed=0"

# 24. A dead alerting backend is exit 3, not a green run: the failure was
#     detected and could not be delivered, which is an incident of its own.
reset; printf 'web_down' >"$SB/mode"
out=$(run JF_WATCH_NOTIFY_CMD="exit 1"); rc=$?
check "undeliverable alert exits 3" "$rc" "3"
contains "undeliverable alert is logged" "$(cat "$LOG")" "alert_undeliverable"

# 25. The token may come from the environment alone, with no config file.
reset
out=$(env -i PATH="$PATH" HOME="$HOME" \
  JF_WATCH_ENV_FILE="$SB/does-not-exist" \
  JF_WATCH_STATE_DIR="$STATE" JF_WATCH_LOG_FILE="$LOG" \
  JF_WATCH_WEB_URL="$BASE/web" JF_WATCH_HEALTH_URL="$BASE/healthz" \
  JF_WATCH_TELEGRAM_TOKEN=envtoken JF_WATCH_TELEGRAM_CHAT=12345 \
  JF_WATCH_NOTIFY_CMD="$NOTIFY_CMD" \
  JF_WATCH_READY_CMD="$READY_OK" JF_WATCH_DF_CMD="$DF_OK" \
  JF_WATCH_KANBAN_DB=auto JF_WATCH_KANBAN_DB_CANDIDATES=/nonexistent/a.db \
  "${PSQL_ENV[@]}" \
  JF_WATCH_ATTEMPT_DELAY=0 bash "$WATCH"); rc=$?
check "env-only config runs" "$rc" "0"

echo "--- the REAL Telegram transport (no JF_WATCH_NOTIFY_CMD) ---"

# WHY THIS GROUP EXISTS (card t_2418aba0). Every other assertion in this file
# runs the watchdog with JF_WATCH_NOTIFY_CMD set, and send_message() RETURNS at
# that override:
#
#     if [ -n "$NOTIFY_CMD_OVERRIDE" ]; then printf '%s' "$body" | bash -c "$NOTIFY_CMD_OVERRIDE"; return $?; fi
#
# So everything after it — the Telegram POST, the `"ok":true` read, the
# message_id extraction, the per-chat loop, and the `ok=0 -> rc=1 -> exit 3`
# branch send_alert takes when nothing was delivered — was DEAD as far as this
# suite was concerned. A regression in any of them (including the SIGPIPE shape
# PR 864 fixed at that very `grep`) stayed invisible to the gate.
#
# The seam here is PATH, not an env override: a shimmed `curl` answers ONLY the
# Telegram sendMessage POST from a canned reply and hands every other URL to the
# real curl, so the probes under test stay the real ones.
TG_CALLS="$SB/tg_calls"; TG_BODY="$SB/tg_body"; TG_RC="$SB/tg_rc"
mkdir -p "$SB/fakebin_tg"
cat >"$SB/fakebin_tg/curl" <<'EOF'
#!/usr/bin/env bash
# PATH-shadowed curl for the real-transport legs. It answers only the Telegram
# sendMessage POST: the reply comes from $TG_BODY (or $TG_BODY.<chat_id>, which
# is how a leg makes one chat of a fan-out refuse) and the exit status from
# $TG_RC. Every other URL — the probes — goes to the real curl, by absolute path.
tg=0; chat=""
for a in "$@"; do
  case "$a" in
    *api.telegram.org*) tg=1 ;;
    chat_id=*)          chat="${a#chat_id=}" ;;
  esac
done
if [ "$tg" = 1 ]; then
  # ONE line per POST (the argv carries the multi-line alert text, so newlines
  # are folded) — so `grep -c '^'` on this file is a POST count, and the chat is
  # the first field.
  printf '%s\n' "${chat:-?}|$(printf '%s' "$*" | tr '\n' ' ')" >>"$TG_CALLS"
  body="$TG_BODY"
  [ -n "$chat" ] && [ -f "$TG_BODY.$chat" ] && body="$TG_BODY.$chat"
  rc=0
  [ -f "$TG_RC" ] && rc="$(cat "$TG_RC")"
  [ "$rc" = 0 ] || exit "$rc"
  [ -f "$body" ] && cat "$body"
  exit 0
fi
exec "$REAL_CURL" "$@"
EOF
chmod +x "$SB/fakebin_tg/curl"

# Runs the watchdog on the REAL notify transport: no JF_WATCH_NOTIFY_CMD, the
# token + chat come from the config file, and `curl` is the shim above.
run_real_notify() { # script [extra env assignments...]
  local script="$1"; shift
  env -i PATH="$SB/fakebin_tg:$PATH" HOME="$HOME" \
    JF_WATCH_ENV_FILE="$SB/env" \
    JF_WATCH_STATE_DIR="$STATE" \
    JF_WATCH_LOG_FILE="$LOG" \
    JF_WATCH_WEB_URL="$BASE/web" \
    JF_WATCH_HEALTH_URL="$BASE/healthz" \
    JF_WATCH_NOTIFY_CMD= \
    JF_WATCH_READY_CMD="$READY_OK" \
    JF_WATCH_DF_CMD="$DF_OK" \
    JF_WATCH_KANBAN_DB=auto \
    JF_WATCH_KANBAN_DB_CANDIDATES=/nonexistent/a.db \
    "${PSQL_ENV[@]}" \
    JF_WATCH_ATTEMPT_DELAY=0 JF_WATCH_TIMEOUT=5 \
    TG_BODY="$TG_BODY" TG_RC="$TG_RC" TG_CALLS="$TG_CALLS" REAL_CURL="$REAL_CURL" \
    "$@" bash "$script"
}

tg_reset() { # reply_body reply_rc — the canned answer for the next POST(s)
  rm -f "$TG_BODY" "$TG_RC" "$TG_CALLS"
  rm -f "$TG_BODY".*
  if [ -n "$1" ]; then printf '%s' "$1" >"$TG_BODY"; fi
  printf '%s' "$2" >"$TG_RC"
  : >"$TG_CALLS"
}

# Each leg drives one shape of the transport through $1 (the shipped watchdog, or
# a mutant of it in the controls below) and asserts on the log it wrote. They are
# functions because the controls re-run them against a mutant.

# (a) DELIVERED: the API's `"ok":true` is read, notify_sent is logged WITH the
#     extracted message_id, exactly one POST goes to the configured bot, and the
#     run reports the detected failure as exit 1 (detected AND delivered) — not
#     the exit 3 that means "could not tell anyone".
leg_delivered() {
  reset; printf 'web_down' >"$SB/mode"
  tg_reset '{"ok":true,"result":{"message_id":4211,"chat":{"id":2103883},"date":1790000000}}' 0
  out=$(run_real_notify "$1"); rc=$?
  check "real transport: a delivered alert exits 1 (detected AND delivered, not 3)" "$rc" "1"
  contains "real transport: the delivery is logged as notify_sent for the configured chat" \
    "$(cat "$LOG")" "notify_sent chat=2103883"
  contains "real transport: and the message_id was extracted from the API's reply" \
    "$(cat "$LOG")" "message_id=4211"
  notcontains "real transport: a delivered alert is NOT also reported as notify_failed" \
    "$(cat "$LOG")" "notify_failed"
  check "real transport: exactly one POST was made" "$(grep -c '^' "$TG_CALLS")" "1"
  contains "real transport: the POST went to the configured bot's sendMessage endpoint" \
    "$(cat "$TG_CALLS")" "https://api.telegram.org/botdummy/sendMessage"
  contains "real transport: and carried the configured chat_id" \
    "$(cat "$TG_CALLS")" "chat_id=2103883"
}

# (b) REFUSED: the API answered, and said `"ok":false`. That is not a delivery —
#     it must read notify_failed, carry the API's own description, and take the
#     run to exit 3 so an undelivered alert is an incident of its own.
leg_refused() {
  reset; printf 'web_down' >"$SB/mode"
  tg_reset '{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}' 0
  out=$(run_real_notify "$1"); rc=$?
  check "real transport: an ok:false reply exits 3 (detected, NOT delivered)" "$rc" "3"
  contains "real transport: the refused POST is logged as notify_failed" \
    "$(cat "$LOG")" "notify_failed chat=2103883"
  contains "real transport: and the API's own description is carried into the log" \
    "$(cat "$LOG")" "chat not found"
  contains "real transport: the run names the alert as undeliverable" \
    "$(cat "$LOG")" "alert_undeliverable"
  notcontains "real transport: an ok:false reply is never logged as sent" \
    "$(cat "$LOG")" "notify_sent"
}

# (c) NO TRANSPORT AT ALL: curl never delivers (no reply, non-zero status). Same
#     verdict as (b) by a different route — this is the branch that must not be
#     reachable only through a canned override.
leg_dead_transport() {
  reset; printf 'web_down' >"$SB/mode"
  tg_reset '' 7
  out=$(run_real_notify "$1"); rc=$?
  check "real transport: a curl that cannot reach the API exits 3" "$rc" "3"
  contains "real transport: the dead transport is logged as notify_failed" \
    "$(cat "$LOG")" "notify_failed chat=2103883"
  contains "real transport: and the run names the alert as undeliverable" \
    "$(cat "$LOG")" "alert_undeliverable"
}

# (d) FAN-OUT: two chats, the first answering ok and the second refusing. The loop
#     must POST BOTH, report each one's own outcome, and fail the run — a loop
#     that stopped at the first chat (the control below) would look green here.
leg_multi_chat() {
  reset; write_config dummy "111,222"; printf 'web_down' >"$SB/mode"
  tg_reset '{"ok":true,"result":{"message_id":4211}}' 0
  printf '%s' '{"ok":false,"description":"Bad Request: chat not found"}' >"$TG_BODY.222"
  out=$(run_real_notify "$1"); rc=$?
  check "real transport: a two-chat fan-out with one refusal exits 3" "$rc" "3"
  check "real transport: BOTH chats were POSTed (the loop does not stop at the first)" \
    "$(grep -c '^' "$TG_CALLS")" "2"
  contains "real transport: the first chat is logged as sent" "$(cat "$LOG")" "notify_sent chat=111"
  contains "real transport: the second chat is logged as failed" "$(cat "$LOG")" "notify_failed chat=222"
  contains "real transport: and the refusing chat's own reply is what the log carries" \
    "$(cat "$LOG")" "chat not found"
}

# (f) The temp-key exclusion leg, driven through the real notify transport so a
#     mutant that PAGES is observed as a POST (and a delivered page, hence exit 1)
#     rather than as a notification the stub notifier swallowed. One 25 h old
#     REVOKED row: silent on the shipped file, paging on the no-`revoked_at`
#     mutant below.
leg_tmpkey_revoked() { # script
  reset
  tmpkey_add qa-verify-tmp_t_old_revoked 25 revoked
  tg_reset '{"ok":true,"result":{"message_id":4211}}' 0
  out=$(run_real_notify "$1"); rc=$?
  check "a 25 h old REVOKED temp key stays silent" "$rc" "0"
  check "…and pages nobody" "$(grep -c '^' "$TG_CALLS")" "0"
}

# (e) The multi-line reply, 20 001 lines with `"ok":true` on line 1. Its size is
#     asserted because the control below is only valid on THIS shape: the pre-fix
#     pipe form's false-fail is NOT monotonic in size (measured on this host with
#     the ok flag always present: 132 B 0/10, 5 181 B 1/10, 25 581 B 0/10,
#     102 081 B 9/10, and this shape 50/50) — it depends on line boundaries.
tg_big_reply() {
  printf '%s\n' '{"ok":true,"result":{"message_id":4211,"chat":{"id":2103883},"date":1790000000}}' >"$SB/tg_big"
  awk 'BEGIN{for(i=0;i<20000;i++) printf "{\"pad\":\"%040d\"}\n", i}' >>"$SB/tg_big"
}
leg_multiline_delivery() {
  reset; printf 'web_down' >"$SB/mode"
  tg_reset '' 0
  cp "$SB/tg_big" "$TG_BODY"
  out=$(run_real_notify "$1"); rc=$?
  check "the real path delivers on the 20 001-line reply (ok:true on line 1)" "$rc" "1"
  contains "…and logs notify_sent rather than a false failure" \
    "$(cat "$LOG")" "notify_sent chat=2103883"
  notcontains "…with no notify_failed anywhere in the run" "$(cat "$LOG")" "notify_failed"
}

tg_big_reply
check "fixture: the multi-line reply is the measured shape (20 001 lines / 1 020 081 bytes)" \
  "$(wc -l <"$SB/tg_big")/$(wc -c <"$SB/tg_big")" "20001/1020081"

leg_delivered "$WATCH"
leg_refused "$WATCH"
leg_dead_transport "$WATCH"
leg_multi_chat "$WATCH"

# ── the bite proofs ─────────────────────────────────────────────────────────
# The group above is evidence only if it goes RED when the transport regresses.
# So each control drives a MUTATED copy of the watchdog through the SAME leg and
# asserts the leg's verdict flips. The mutations are the real pre-fix shapes, not
# arbitrary breakage:
#   M1  the `"ok":true` read restored to `printf '%s' "$resp" | grep -q …` — the
#       form PR 864 replaced. On the reply above, `grep -q` exits at line 1 while
#       the writer still has ~10^6 bytes queued, the writer takes SIGPIPE (141),
#       and under the watchdog's own `set -uo pipefail` that status wins the
#       pipeline although grep MATCHED — so a DELIVERED alert reads
#       `notify_failed` and the run exits 3. (Measured here 50/50; the pipeline's
#       own status on this host: pipe form 141, herestring 0.)
#   M2  the multi-chat loop degraded to the first chat only.
cat >"$SB/mutate.py" <<'PY'
import sys
src, dst, old, new = sys.argv[1:5]
s = open(src).read()
n = s.count(old)
if n != 1:
    sys.stderr.write("mutation matched %d sites, want exactly 1\n" % n)
    sys.exit(1)
open(dst, "w").write(s.replace(old, new))
PY
mutant() { # name old new -> prints the mutant's path (nothing if it did not apply)
  mkdir -p "$SB/mutants"
  if python3 "$SB/mutate.py" "$WATCH" "$SB/mutants/$1.sh" "$2" "$3"; then
    printf '%s' "$SB/mutants/$1.sh"
  fi
}
# The shipped read, the form it replaced, and the loop — verbatim from the watchdog.
HERE_READ="      && grep -q '\"ok\":true' <<<\"\$resp\"; then"
PIPE_READ="      && printf '%s' \"\$resp\" | grep -q '\"ok\":true'; then"
LOOP_ALL="  for chat in \"\${_chats[@]}\"; do"
LOOP_ONE="  for chat in \"\${_chats[@]:0:1}\"; do"

MUT_PIPE="$(mutant pipe "$HERE_READ" "$PIPE_READ")"
MUT_LOOP="$(mutant loop "$LOOP_ALL" "$LOOP_ONE")"
check "control: both mutants were built (each pre-fix line found exactly once)" \
  "$([ -n "$MUT_PIPE" ] && [ -n "$MUT_LOOP" ] && echo built || echo MISSING)" "built"
contains "control: the pipe-form mutant carries the pre-fix pipeline read" \
  "$(cat "$MUT_PIPE" 2>/dev/null)" "$PIPE_READ"
contains "control: the loop mutant stops after the first chat" \
  "$(cat "$MUT_LOOP" 2>/dev/null)" "$LOOP_ONE"

# M1, both directions on the same leg: green on the shipped file, red on the
# mutant. One direction alone would pass on a leg that asserts nothing at all.
PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_multiline_delivery "$WATCH" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: the shipped herestring form is green on the multi-line reply (3/3)" "$GOT" "3/0"

PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_multiline_delivery "$MUT_PIPE" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: …and the pre-fix pipe form turns the SAME leg red (no false green)" "$GOT" "0/3"

# M2, both directions on the fan-out leg.
PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_multi_chat "$WATCH" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: the fan-out leg is green on the shipped file (5/5)" "$GOT" "5/0"

PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_multi_chat "$MUT_LOOP" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: …and a loop that stops at the first chat turns it red (the second chat is never POSTed)" \
  "$GOT" "1/4"

# ── the bite proof for the temp-key check's exclusion predicate ─────────────
# The group above only proves something if its verdicts are decided by the
# WHERE clause. The cheapest REAL regression is dropping `revoked_at is null`:
# the check would then page on the soft-revoked rows this card deliberately
# excludes (inert by construction — the app's own DELETE is a soft revoke, a
# decision this card must not fight) and cry wolf on every finished run. So the
# same leg — one 25 h old REVOKED fixture row, the real SQL against the real
# scratch table — is driven against a mutant with that predicate removed: it must
# flip from silent to paging. Without this, a `revoked_at` clause that silently
# stopped being applied (a "fix" that deleted it, say) would leave the suite green.
LIVE_PRED="revoked_at is null and "
MUT_REVOKED="$(mutant no_revoked_at "$LIVE_PRED" "")"
check "control: the no-revoked_at mutant was built (the predicate was found exactly once)" \
  "$([ -n "$MUT_REVOKED" ] && echo built || echo MISSING)" "built"
contains "control: the mutant really lost the predicate" \
  "$(cat "$MUT_REVOKED" 2>/dev/null)" "where created_at < now()"
notcontains "…and no longer carries it" "$(cat "$MUT_REVOKED" 2>/dev/null)" "$LIVE_PRED"

PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_tmpkey_revoked "$WATCH" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: the shipped file is silent on a 25 h old REVOKED temp key (2/2)" "$GOT" "2/0"

PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
leg_tmpkey_revoked "$MUT_REVOKED" >/dev/null 2>&1
GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: …and dropping 'revoked_at is null' turns that SAME leg red (it pages on an inert row)" \
  "$GOT" "0/2"

echo "--- heartbeat digest ---"

# 26. Digest mode sends its own heartbeat, so silence means the watcher died.
#     It also declares the BOARD check's state, so a disabled check is visible
#     daily instead of being an invisible no-op: `board: NOT MEASURED here`.
reset
out=$(run); rc=$?
out=$(run JF_WATCH_MODE=digest); rc=$?
check "digest exits 0" "$rc" "0"
check "digest sends one message" "$(alert_count)" "1"
contains "digest is identifiable" "$(alerts)" "JomForm watchdog alive"
contains "digest counts the passes" "$(alerts)" "checks run: 1"
contains "digest reports the disk" "$(alerts)" "10% used on /"
contains "digest reports the board check as disabled, not as fine" \
  "$(alerts)" "board: NOT MEASURED here"

# 27. The same digest, on a host that HAS a board, reports the measured rate —
#     the heartbeat carries the number, so the absence of a page is informative.
reset; make_board
board_add t_a dev crashed 600 "$CRASH_ERR"
KANBAN_DB_RUN="$BOARD"
out=$(run); rc=$?
out=$(run JF_WATCH_MODE=digest); rc=$?
check "digest with a board exits 0" "$rc" "0"
contains "digest reports the measured board rate" "$(alerts)" "board: board db="
contains "digest reports the failure count it measured" "$(alerts)" "failures=1"

echo "--- log rotation ---"

# 28. A log over the cap is trimmed in place (same inode: a cron-held fd, or
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
echo "--- the installer: save-then-install, drift, and the revert path ---"

# WHY THIS GROUP EXISTS (card t_e7a0ae5e). Step 1 of
# deploy/monitoring/install-jomform-watch.sh used to be a bare
#   install -m 0755 -o root -g root "$HERE/jomform-watch.sh" "$BIN"
# which overwrote the live watchdog IN PLACE with nothing kept. Measured on the
# agent host: the copy installed 2026-10-13 was gone the moment the installer
# ran, and it matched no committed revision either, so "reinstall it from git"
# was not a faithful rollback. A change that cannot be undone in one command is
# not one this board should ship, so the sequence and the printed revert are
# asserted here.
#
# It is driven through the installer's own --bin / --binary-only seams (both
# documented in its header): the REAL target is /usr/local/bin, and the rest of
# the run writes /etc, installs cron and sends a live Telegram alert — none of
# which a gate may do, so the fake target is the only honest way to exercise it.
INST="$HERE/install-jomform-watch.sh"
[ -f "$INST" ] || { echo "FAIL: $INST missing" >&2; exit 1; }
# The installer writes with `install -o root -g root`; as a non-root run every
# leg below would fail for a reason that is not the property under test, so say
# so plainly instead of reporting nine confusing reds.
if [ "$(id -u)" != 0 ]; then
  echo "FAIL: the installer group needs root (it asserts an install -o root -g root sequence)" >&2
  echo "      (a suite that cannot run a leg must not report ok — run the gate as root)" >&2
  exit 1
fi

IROOT="$SB/installer"; PKG1="$IROOT/pkg1"; PKG2="$IROOT/pkg2"
mkdir -p "$IROOT/bin" "$PKG1" "$PKG2"
cp "$INST" "$PKG1/"; cp "$INST" "$PKG2/"
cp "$WATCH" "$PKG1/jomform-watch.sh"; cp "$WATCH" "$PKG2/jomform-watch.sh"
# A second, DIFFERENT incoming revision, so a second install has different bytes
# to land (one file cannot be both the outgoing and the incoming of one run).
printf '\n# v2 revision — a different incoming byte stream\n' >>"$PKG2/jomform-watch.sh"
IBIN="$IROOT/bin/jomform-watch.sh"
inst_sha() { sha256sum "$1" | cut -d' ' -f1; }
V1_SHA="$(inst_sha "$PKG1/jomform-watch.sh")"
V2_SHA="$(inst_sha "$PKG2/jomform-watch.sh")"
printf '#!/usr/bin/env bash\necho OLD-REVISION\n' >"$IROOT/old.sh"
chmod 0755 "$IROOT/old.sh"
OLD_SHA="$(inst_sha "$IROOT/old.sh")"
check "installer fixture: the two incoming revisions really differ (or a drift leg would be a no-op)" \
  "$([ "$V1_SHA" != "$V2_SHA" ] && echo differ || echo same)" "differ"
check "installer fixture: the outgoing bytes differ from both incoming revisions" \
  "$([ "$OLD_SHA" != "$V1_SHA" ] && [ "$OLD_SHA" != "$V2_SHA" ] && echo differ || echo same)" "differ"
# The gate must never touch the HOST's watchdog: prove the fake target was used.
HOST_BIN_SHA_BEFORE="$([ -f /usr/local/bin/jomform-watch.sh ] && inst_sha /usr/local/bin/jomform-watch.sh || echo absent)"
inst_archives() { # count + path of the timestamped .prev.<ts> files this run made
  ARCHIVED_COUNT=0; ARCHIVED=""
  for f in "$IROOT"/bin/jomform-watch.sh.prev.*; do
    [ -e "$f" ] || continue
    ARCHIVED_COUNT=$((ARCHIVED_COUNT+1)); ARCHIVED="$f"
  done
}

# (a) FRESH: nothing installed -> no .prev, because there is nothing to keep.
out=$(bash "$PKG1/install-jomform-watch.sh" --bin "$IBIN" --binary-only 2>&1); rc=$?
check "installer: a fresh install exits 0" "$rc" "0"
check "…and lands the incoming bytes" "$(inst_sha "$IBIN")" "$V1_SHA"
check "…at 0755" "$(stat -c %a "$IBIN")" "755"
check "…with no .prev (a backup of nothing is not a backup)" \
  "$([ -e "$IBIN.prev" ] && echo yes || echo no)" "no"
contains "…says it was fresh rather than inventing a backup" "$out" "fresh install"
contains "…and prints the incoming sha256, one line, no second command needed" "$out" "sha256=$V1_SHA"
notcontains "…with no revert line, because nothing was replaced" "$out" "revert (one command)"

# (b) IDENTICAL re-run -> still no .prev: identical bytes cannot be undone by a
#     revert, so a "backup" here would only be noise (and would soon be a .prev
#     that holds nothing but the version it accompanies).
out=$(bash "$PKG1/install-jomform-watch.sh" --bin "$IBIN" --binary-only 2>&1); rc=$?
check "installer: an identical re-install exits 0" "$rc" "0"
contains "…reports no drift in the card's own words" "$out" "installed sha == incoming sha"
check "…and still writes no .prev" "$([ -e "$IBIN.prev" ] && echo yes || echo no)" "no"
contains "…saying why, rather than leaving the reader to infer it" "$out" "no backup written"

# (c) DRY-RUN over drift: the report must name the drift and say whether a
#     .prev would be created — and must change NOTHING.
install -m 0755 "$IROOT/old.sh" "$IBIN"
out=$(bash "$PKG1/install-jomform-watch.sh" --bin "$IBIN" --binary-only --dry-run 2>&1); rc=$?
check "installer dry-run: exits 0 over a drifting target" "$rc" "0"
contains "…reports the drift as the card specifies it" "$out" "installed sha != incoming sha"
contains "…prints the outgoing sha256" "$out" "sha256=$OLD_SHA"
contains "…prints the incoming sha256" "$out" "sha256=$V1_SHA"
contains "…and says a .prev WOULD be created, naming the path" \
  "$out" "would save $IBIN → $IBIN.prev (0700)"
check "…while writing nothing: the target still holds the old bytes" "$(inst_sha "$IBIN")" "$OLD_SHA"
check "…and no .prev appeared" "$([ -e "$IBIN.prev" ] && echo yes || echo no)" "no"

# (d) THE CARD'S FIRST REQUIREMENT: .prev holds the OLD bytes, the target holds
#     the NEW bytes, .prev is 0700, and the revert line is printed.
out=$(bash "$PKG1/install-jomform-watch.sh" --bin "$IBIN" --binary-only 2>&1); rc=$?
check "installer: the overwrite run exits 0" "$rc" "0"
check "…the target now holds the NEW bytes" "$(inst_sha "$IBIN")" "$V1_SHA"
check "…and .prev holds the OLD bytes" "$(inst_sha "$IBIN.prev")" "$OLD_SHA"
check "…at 0700 (a rollback copy is not world-readable)" "$(stat -c %a "$IBIN.prev")" "700"
contains "…the run prints the sha256 of the file it replaced" "$out" "sha256=$OLD_SHA"
contains "…and the sha256 it installed" "$out" "sha256=$V1_SHA"
REVERT="$(sed -n 's/^revert (one command): //p' <<<"$out" | sed -n '1p')"
check "…and the exact one-command revert, complete enough to paste" \
  "$REVERT" "install -m 0755 -o root -g root $IBIN.prev $IBIN"
# The printed line must WORK — a revert path nobody has run is not a revert path.
if [ "$REVERT" = "install -m 0755 -o root -g root $IBIN.prev $IBIN" ]; then
  if eval "$REVERT" >/dev/null 2>&1 && [ "$(inst_sha "$IBIN")" = "$OLD_SHA" ]; then
    ok "…and running that printed line puts the OLD bytes back (one command, as advertised)"
  else
    bad "the printed revert line did not restore the old bytes (target now $(inst_sha "$IBIN"))"
  fi
else
  bad "not running a revert line that is not the expected one"
fi

# (e) A run from exactly that reverted state: .prev already holds the outgoing
#     bytes, so it is KEPT as-is. Archiving a copy of itself would be noise;
#     dropping it would lose the only copy of OLD.
inst_archives; ARCHIVES_BEFORE="$ARCHIVED_COUNT"
out=$(bash "$PKG1/install-jomform-watch.sh" --bin "$IBIN" --binary-only 2>&1); rc=$?
check "installer: re-running over a reverted target exits 0" "$rc" "0"
contains "…keeps .prev because it already holds the outgoing bytes" \
  "$out" "kept the existing $IBIN.prev (sha256=$OLD_SHA)"
inst_archives
check "…instead of archiving a second copy of the same bytes" "$ARCHIVED_COUNT" "$ARCHIVES_BEFORE"
check "…and .prev still holds OLD, untouched" "$(inst_sha "$IBIN.prev")" "$OLD_SHA"

# (f) THE CARD'S SECOND REQUIREMENT: a second install must not destroy the first
#     .prev's content. State is target=v1, .prev=OLD, so this run replaces v1
#     with v2 and OLD must survive under a timestamped name.
out=$(bash "$PKG2/install-jomform-watch.sh" --bin "$IBIN" --binary-only 2>&1); rc=$?
check "installer: the second (different) install exits 0" "$rc" "0"
check "…lands the new revision" "$(inst_sha "$IBIN")" "$V2_SHA"
check "…and .prev now holds the version this run replaced" "$(inst_sha "$IBIN.prev")" "$V1_SHA"
inst_archives
check "…while the FIRST .prev's bytes survive as exactly one timestamped archive" "$ARCHIVED_COUNT" "1"
check "…holding the OLD bytes, not the ones it had just seen" \
  "$(inst_sha "$ARCHIVED")" "$OLD_SHA"
check "…also at 0700" "$(stat -c %a "$ARCHIVED")" "700"
contains "…and the run says where they went" \
  "$out" "archived the previous backup $IBIN.prev → $ARCHIVED"
# The revert printed by THIS run restores the version it replaced (v1), not OLD —
# i.e. the command always points at the immediately previous watchdog.
REVERT2="$(sed -n 's/^revert (one command): //p' <<<"$out" | sed -n '1p')"
check "…and its printed revert is the same one-command form" \
  "$REVERT2" "install -m 0755 -o root -g root $IBIN.prev $IBIN"
if [ "$REVERT2" = "install -m 0755 -o root -g root $IBIN.prev $IBIN" ]; then
  if eval "$REVERT2" >/dev/null 2>&1 && [ "$(inst_sha "$IBIN")" = "$V1_SHA" ]; then
    ok "…and running it restores v1 (the immediate predecessor), not the archived OLD"
  else
    bad "the second run's revert did not restore v1 (target now $(inst_sha "$IBIN"))"
  fi
else
  bad "not running a revert line that is not the expected one"
fi

# The gate itself must not have reached the REAL install target.
check "the installer group never touched /usr/local/bin/jomform-watch.sh" \
  "$([ -f /usr/local/bin/jomform-watch.sh ] && inst_sha /usr/local/bin/jomform-watch.sh || echo absent)" \
  "$HOST_BIN_SHA_BEFORE"

echo
printf 'jomform-watch self-test: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
exit 0
