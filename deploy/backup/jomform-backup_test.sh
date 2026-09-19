#!/usr/bin/env bash
#
# Tests for jomform-backup.sh.
#
# Run on a host with docker + bash (it stubs rclone and docker, needs neither a
# real remote nor a real database):
#
#   bash deploy/backup/jomform-backup_test.sh
#
# The point of these tests is the FAILURE paths. A backup script that exits 0
# having uploaded nothing is the defect the whole thing exists to prevent, so
# every "success" assertion below is paired with a case that must report FAILED.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SCRIPT_DIR/jomform-backup.sh"
FAILURES=0
PASSES=0

ok()   { PASSES=$((PASSES + 1)); printf '  ok   %s\n' "$1"; }
bad()  {
  FAILURES=$((FAILURES + 1)); printf '  FAIL %s\n' "$1"
  # Print the run log on failure: without it a red case is unactionable.
  if [ -n "${RUN_LOG:-}" ] && [ -f "$RUN_LOG" ]; then
    printf '       --- last 15 lines of %s ---\n' "$RUN_LOG"
    tail -15 "$RUN_LOG" | sed 's/^/       /'
  fi
}

assert_eq() { # label expected actual
  if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: expected '$2', got '$3'"; fi
}
assert_contains() { # label needle file
  if grep -qF -- "$2" "$3" 2>/dev/null; then ok "$1"; else bad "$1: '$2' not found in $3"; fi
}
assert_file() { # label path
  if [ -f "$2" ]; then ok "$1"; else bad "$1: $2 does not exist"; fi
}
# The status file is read by machines; a value needing escaping must not break it.
assert_json_valid() { # label path
  if python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$2" 2>/dev/null; then
    ok "$1"
  else
    bad "$1: $2 is not valid JSON: $(head -c 200 "$2" 2>/dev/null)"
  fi
}

# ----------------------------------------------------------------- harness
# A workdir per case with stub rclone / docker / logger on PATH.
setup_case() {
  CASE_DIR="$(mktemp -d)"
  STUB_BIN="$CASE_DIR/bin"
  mkdir -p "$STUB_BIN" "$CASE_DIR/stage" "$CASE_DIR/state" "$CASE_DIR/minio-data"
  printf 'fake-image-bytes' >"$CASE_DIR/minio-data/object.jpg"
  : >"$CASE_DIR/alerts.log"

  cat >"$CASE_DIR/env" <<'EOF'
SES_SMTP_USER=AKIAEXAMPLE
SES_SMTP_PASS=examplesecret
SES_FROM=noreply@example.com
SUPER_ADMIN_EMAILS=ops@example.com
EOF

  cat >"$STUB_BIN/logger" <<EOF
#!/usr/bin/env bash
printf 'logger %s\n' "\$*" >>"$CASE_DIR/alerts.log"
EOF

  # docker stub: serves the dump, the minio volume path and a restore container.
  cat >"$STUB_BIN/docker" <<EOF
#!/usr/bin/env bash
set -u
case "\$1" in
  ps)     echo "jomform-postgres-1" ;;
  inspect) echo "$CASE_DIR/minio-data" ;;
  exec)
    shift
    # drop -i / -T
    while [ "\$#" -gt 0 ]; do case "\$1" in -i|-T) shift ;; *) break ;; esac; done
    shift                       # container name
    case "\$1" in
      pg_dump)    cat "$CASE_DIR/stage/payload.dump" ;;
      # Must CONSUME the piped dump. A stub that ignores stdin leaves gunzip
      # writing into a closed pipe; with `set -o pipefail` the run then reports
      # a restore failure *intermittently* (a race on when SIGPIPE arrives),
      # which is a flaky test rather than a defect in the script.
      pg_restore) cat >/dev/null
                  printf 'TABLE DATA public users\ntable data public forms\n' ;;
      pg_isready) exit 0 ;;
      psql)       printf 'users|6\naccounts|7\nforms|37\nsales|35\nsubmissions|4\nworkspaces|2\n' ;;
    esac ;;
  start)  echo fakecid ;;
  create) echo fakecid ;;
  stop|rm) exit 0 ;;
esac
exit 0
EOF

  # rclone stub: behaviour driven by STUB_RCLONE_MODE.
  cat >"$STUB_BIN/rclone" <<EOF
#!/usr/bin/env bash
set -u
MODE="\${STUB_RCLONE_MODE:-ok}"
# strip the --config <path> pair
args=()
while [ "\$#" -gt 0 ]; do
  if [ "\$1" = "--config" ]; then shift 2; continue; fi
  args+=("\$1"); shift
done
set -- "\${args[@]:-}"
cmd="\${1:-}"; shift || true
case "\$cmd" in
  listremotes)
    if [ "\$MODE" = "no-remote" ]; then echo "other:"; else echo "googldrive:"; fi ;;
  copy)
    if [ "\$MODE" = "copy-fail" ]; then
      # Real rclone errors carry double quotes; an unescaped copy of this text
      # into the status JSON would make the file unparseable.
      echo 'Failed to copy: googleapi: Error 403: The user has exceeded their Drive storage quota for "0AIx\"" files.' >&2
      exit 1
    fi
    # Model a REAL upload: the object lands in the remote dir, so the
    # post-upload verification below has something truthful to compare against.
    src="\${1:-}"; dest="\${2:-}"
    mkdir -p "$CASE_DIR/remote"
    cp "\$src" "$CASE_DIR/remote/\$(basename "\$src")"
    : >"\$dest" 2>/dev/null || true
    exit 0 ;;
  copyto) exit 0 ;;
  # hashsum MD5 <file-path> -> "<md5>  <basename>", from the local COPY of the
  # object. STUB_REMOTE_MISSING=1 models an object that never landed,
  # STUB_REMOTE_CORRUPT=1 a same-size object whose bytes differ.
  hashsum)
    shift                       # the hash type (MD5)
    path="\${1:-}"
    base="\$(basename "\$path")"
    if [ "\${STUB_REMOTE_MISSING:-0}" = "1" ]; then
      echo "directory not found" >&2
      exit 1
    fi
    if [ "\${STUB_NO_HASH:-0}" = "1" ]; then
      # A backend that exposes no hash for the object.
      exit 1
    fi
    if [ -f "$CASE_DIR/remote/\$base" ]; then
      if [ "\${STUB_REMOTE_CORRUPT:-0}" = "1" ]; then
        printf '00000000000000000000000000000000  %s\n' "\$base"
      else
        # Same shape as real rclone: "<md5>  <name>".
        printf '%s  %s\n' "\$(md5sum "$CASE_DIR/remote/\$base" | cut -d' ' -f1)" "\$base"
      fi
    else
      echo "directory not found" >&2
      exit 1
    fi ;;
  lsjson)
    path="\${1:-}"
    if [ "\$MODE" = "silent" ] || [ "\${STUB_REMOTE_MISSING:-0}" = "1" ]; then
      echo "[]"; exit 0
    fi
    # report the real local size, like a working remote would
    base="\$(basename "\$path")"
    if [ -f "$CASE_DIR/stage/\$base" ]; then
      printf '[{"Name":"%s","Size":%s}]\n' "\$base" "\$(stat -c %s "$CASE_DIR/stage/\$base")"
    else
      echo "[]"
    fi ;;
  lsf) exit 0 ;;
  deletefile) exit 0 ;;
  about) exit 0 ;;
esac
exit 0
EOF

  chmod +x "$STUB_BIN/logger" "$STUB_BIN/docker" "$STUB_BIN/rclone"
  # A real gzip stream: the restore drill actually gunzips the input, so a plain
  # text placeholder would fail the pipeline and test nothing.
  printf 'PGDPUMPBYTES' | gzip -9 >"$CASE_DIR/stage/payload.dump"
  RUN_LOG="$CASE_DIR/run.log"
}

# run_backup [env assignments...] -- [script args...]
run_backup() {
  local extra=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do extra+=("$1"); shift; done
  [ "$#" -gt 0 ] && shift
  env JFBACKUP_PATH_PREFIX="$STUB_BIN" \
      JFBACKUP_STAGE_DIR="$CASE_DIR/stage" \
      JFBACKUP_STATE_DIR="$CASE_DIR/state" \
      JFBACKUP_LOG_FILE="$RUN_LOG" \
      JFBACKUP_LOCK_FILE="$CASE_DIR/lock" \
      JFBACKUP_ENV_FILE="$CASE_DIR/env" \
      RCLONE_CONFIG="$CASE_DIR/rclone.conf" \
      "${extra[@]}" \
      bash "$SCRIPT" "$@" >>"$RUN_LOG" 2>&1
  echo "exit=$?"
}
teardown_case() { rm -rf "$CASE_DIR"; }

touch_rclone_conf() { echo '[googldrive]' >"$CASE_DIR/rclone.conf"; }

# ------------------------------------------------------------------- cases
echo "case: happy path uploads, verifies and reports OK"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "log says OK" "jomform-backup OK" "$RUN_LOG"
assert_contains "upload verified" "verified googldrive:jomform-backups/daily/" "$RUN_LOG"
assert_contains "status file OK" '"status": "OK"' "$CASE_DIR/state/last-run.json"
if [ -f "$CASE_DIR/state/last-failure.json" ]; then bad "no last-failure.json on success"; else ok "no last-failure.json on success"; fi
teardown_case

echo "case: rclone copy fails -> non-zero, ERROR logged, alert raised, last-failure written"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=copy-fail --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log has ERROR" "rclone copy failed" "$RUN_LOG"
assert_contains "log carries the provider message" "exceeded their Drive storage quota" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "status file FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
assert_json_valid "status file is parseable despite quotes in the error" "$CASE_DIR/state/last-run.json"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
assert_contains "syslog alert raised" "jomform-backup FAILED" "$CASE_DIR/alerts.log"
assert_contains "local dump still exists" "upload or verification failed" "$RUN_LOG"
teardown_case

echo "case: copy exits 0 but NOTHING lands on the remote -> must still FAIL"
# This is the exact trap: rclone exit 0 is not proof that bytes were uploaded.
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=silent STUB_REMOTE_MISSING=1 --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the missing object" "remote object missing or unreadable after upload" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
teardown_case

echo "case: object uploaded but its bytes differ (same size) -> FAIL on hash"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok STUB_REMOTE_CORRUPT=1 --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the hash mismatch" "remote hash mismatch" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
teardown_case

echo "case: remote hash unavailable -> FAIL rather than pass on size alone"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok STUB_NO_HASH=1 --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log reports no hash" "remote hash unavailable" "$RUN_LOG"
teardown_case

echo "case: remote configured but token/remote unusable -> alert says local dump is not offsite"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=no-remote --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the remote" "rclone remote unavailable" "$RUN_LOG"
assert_contains "alert body warns about same-host dump" "not an offsite backup" "$RUN_LOG"
assert_contains "token fix is named" "rclone config reconnect googldrive:" "$RUN_LOG"
teardown_case

echo "case: rclone missing entirely -> the dump still happens and the run FAILS loudly"
setup_case; touch_rclone_conf
# A PATH prefix with the other stubs but NO rclone, so only rclone is missing.
mkdir -p "$CASE_DIR/norclone"
cp "$STUB_BIN/docker" "$STUB_BIN/logger" "$CASE_DIR/norclone/"
chmod +x "$CASE_DIR/norclone"/*
rc="$(run_backup JFBACKUP_PATH_PREFIX="$CASE_DIR/norclone" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names rclone" "rclone is not installed" "$RUN_LOG"
assert_file "dump was still written" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
teardown_case

echo "case: runs under cron's minimal environment (PATH pinned, no shell profile)"
# cron gives a job a bare environment. If the script depended on the
# interactive PATH, the job would fail in a way nobody sees until the day it
# matters. Run it with env -i and only the variables the job needs.
setup_case; touch_rclone_conf
run_log="$CASE_DIR/run.log"
env -i \
    JFBACKUP_PATH_PREFIX="$STUB_BIN" \
    JFBACKUP_STAGE_DIR="$CASE_DIR/stage" \
    JFBACKUP_STATE_DIR="$CASE_DIR/state" \
    JFBACKUP_LOG_FILE="$run_log" \
    JFBACKUP_LOCK_FILE="$CASE_DIR/lock" \
    JFBACKUP_ENV_FILE="$CASE_DIR/env" \
    RCLONE_CONFIG="$CASE_DIR/rclone.conf" \
    STUB_RCLONE_MODE=ok \
    /bin/bash "$SCRIPT" --local-only >"$run_log" 2>&1
rc=$?
RUN_LOG="$run_log"
assert_eq "exit code under env -i" "0" "$rc"
assert_contains "dump happened with no inherited PATH" "dump ok" "$run_log"
teardown_case

echo "case: --local-only never uploads and exits 0"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=copy-fail -- --local-only)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "log says no upload" "skipping upload" "$RUN_LOG"
teardown_case

echo "case: a restore drill must NOT overwrite the backup job's own status"
# Otherwise an operator checking "is the nightly backup healthy?" reads the
# drill's OK and never learns that the backups stopped succeeding.
setup_case; touch_rclone_conf
# a previously FAILED backup run leaves its status behind
mkdir -p "$CASE_DIR/state"
printf '{"status": "FAILED", "detail": "previous backup failed"}\n' >"$CASE_DIR/state/last-run.json"
cp "$CASE_DIR/state/last-run.json" "$CASE_DIR/state/last-failure.json"
rc="$(run_backup -- --restore "$CASE_DIR/stage/payload.dump")"
assert_eq "drill exit code" "exit=0" "$rc"
assert_contains "drill did run" "restore drill complete" "$RUN_LOG"
assert_contains "backup status is untouched" "previous backup failed" "$CASE_DIR/state/last-run.json"
assert_file "drill recorded under its own name" "$CASE_DIR/state/last-restore-drill.json"
assert_contains "drill file says OK" '"status": "OK"' "$CASE_DIR/state/last-restore-drill.json"
teardown_case

echo "case: --restore on a missing file FAILS"
setup_case; touch_rclone_conf
rc="$(run_backup -- --restore /nonexistent/file.dump.gz)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the file" "dump not found or unreadable" "$RUN_LOG"
teardown_case

echo "case: --restore drill reports real row counts and cleans up its container"
setup_case; touch_rclone_conf
rc="$(run_backup -- --restore "$CASE_DIR/stage/payload.dump")"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "users count" "restored users = 6" "$RUN_LOG"
assert_contains "forms count" "restored forms = 37" "$RUN_LOG"
assert_contains "sales count" "restored sales = 35" "$RUN_LOG"
assert_contains "submissions count" "restored submissions = 4" "$RUN_LOG"
assert_contains "drill completed" "restore drill complete" "$RUN_LOG"
teardown_case

echo "case: an empty dump (no TABLE DATA) is rejected instead of uploaded"
setup_case; touch_rclone_conf
cat >"$STUB_BIN/docker" <<EOF
#!/usr/bin/env bash
case "\$1" in
  ps) echo jomform-postgres-1 ;;
  exec) shift; while [ "\$#" -gt 0 ]; do case "\$1" in -i|-T) shift ;; *) break ;; esac; done; shift
        case "\$1" in pg_dump) printf 'x' ;; pg_restore) exit 0 ;; esac ;;
esac
exit 0
EOF
chmod +x "$STUB_BIN/docker"
rc="$(run_backup STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log rejects the archive" "archive with no table data" "$RUN_LOG"
teardown_case

echo
printf '%d passed, %d failed\n' "$PASSES" "$FAILURES"
[ "$FAILURES" -eq 0 ]
