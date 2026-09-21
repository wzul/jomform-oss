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
  copyto)
    # A server-side copy reads the source from the REMOTE and writes the
    # destination on the REMOTE — neither path is a local file. The modelled
    # remote is $CASE_DIR/remote, addressed by basename.
    src="\${1:-}"; dest="\${2:-}"
    mkdir -p "$CASE_DIR/remote"
    if [ -f "$CASE_DIR/remote/\$(basename "\$src")" ]; then
      cp "$CASE_DIR/remote/\$(basename "\$src")" "$CASE_DIR/remote/\$(basename "\$dest")"
    else
      echo "object not found: \$src" >&2
      exit 1
    fi
    exit 0 ;;
  # rcat <remote-path> — read stdin and write it to the modelled remote path, so
  # the monthly sidecar in this stub holds the REAL BYTES the script produced. A
  # bare 'exit 0' here would make the monthly content assertions vacuous.
  rcat)
    path="\${1:-}"
    mkdir -p "$CASE_DIR/remote"
    if [ "\$MODE" = "rcat-fail" ] || [ "\${STUB_RCAT_FAIL:-0}" = "1" ]; then
      echo 'Failed to copy: googleapi: Error 403: rateLimitExceeded' >&2
      exit 1
    fi
    cat >"$CASE_DIR/remote/\$(basename "\$path")"
    exit 0 ;;
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
run_backup() { run_backup_script "$SCRIPT" "$@"; }

# run_backup_script <script> [env assignments...] -- [script args...]
# The script path is a parameter so a case can run a deliberately sabotaged COPY
# (see the bite case at the bottom) without editing the tracked file.
run_backup_script() {
  local script="$1"; shift
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
      bash "$script" "$@" >>"$RUN_LOG" 2>&1
  echo "exit=$?"
}
teardown_case() { rm -rf "$CASE_DIR"; }

touch_rclone_conf() { echo '[googldrive]' >"$CASE_DIR/rclone.conf"; }

# ---------------------------------------------------------- monthly helpers
# Names the script will use for THIS day, spelled exactly as the script does
# (`date -u +%Y-%m-%d`). Note that JFBACKUP_DAY_OF_MONTH only drives the
# promotion gate, not the file names, so a run on the 20th still produces
# jomform-daily-<20th>; the digest comparison below is against that object.
daily_name() { date -u +%Y-%m-%d; }
monthly_name() { date -u +%Y-%m; }

# check_isolated — the honest form of the card's acceptance: take the monthly
# pair the promotion produced, copy it into a directory that holds NOTHING else,
# and run a real `sha256sum -c`.
#
# The staged daily objects are deleted first, modelling local retention having
# pruned them (the dump script prunes local staging at the end of every run, so
# by the time a human reaches for the monthly copy the daily file is long gone).
# Without that step the check is not truthful: an absolute staging path in the
# sidecar still RESOLVES on this host, so `sha256sum -c` would pass on the very
# sidecar this card is about. That is exactly what a first attempt at this test
# did — it went green against the defect.
#   $1 label  $2 expect: pass|fail
check_isolated() {
  local label="$1" expect="$2" mn vdir
  mn="jomform-monthly-$(monthly_name).dump.gz"
  vdir="$(mktemp -d)"
  cp "$CASE_DIR/remote/$mn" "$CASE_DIR/remote/$mn.sha256" "$vdir/" 2>/dev/null
  if [ ! -f "$vdir/$mn.sha256" ]; then
    bad "$label: no monthly sidecar at $CASE_DIR/remote/$mn.sha256 (nothing was written)"
    rm -rf "$vdir"; return
  fi
  rm -f "$CASE_DIR/stage/jomform-daily-"*.dump.gz "$CASE_DIR/stage/jomform-daily-"*.dump.gz.sha256
  if (cd "$vdir" && sha256sum -c "$mn.sha256" >/dev/null 2>&1); then
    got=pass
  else
    got=fail
  fi
  if [ "$got" = "$expect" ]; then
    ok "$label: sha256sum -c $got on the isolated monthly pair, as required"
  else
    bad "$label: sha256sum -c $got but must be $expect (sidecar: $(cat "$vdir/$mn.sha256"))"
  fi
  rm -rf "$vdir"
}

# verify_monthly_pair — the acceptance the card states, for a run that is meant
# to be CORRECT. Content assertions first, then the real checksum run.
verify_monthly_pair() {
  local label="$1" vdir dn mn want got
  dn="jomform-daily-$(daily_name).dump.gz"
  mn="jomform-monthly-$(monthly_name).dump.gz"
  want="$(cut -d' ' -f1 "$CASE_DIR/remote/$dn.sha256" 2>/dev/null)"
  vdir="$(mktemp -d)"
  cp "$CASE_DIR/remote/$mn" "$CASE_DIR/remote/$mn.sha256" "$vdir/" 2>/dev/null
  if [ ! -f "$vdir/$mn.sha256" ]; then
    bad "$label: no monthly sidecar at $CASE_DIR/remote/$mn.sha256 (nothing was written)"
    rm -rf "$vdir"; return
  fi
  # An absolute staging path is the defect itself: it only ever resolved on the
  # host that produced the dump, and stops resolving the moment local retention
  # prunes the daily file.
  if grep -qE '(^|[[:space:]])/' "$vdir/$mn.sha256"; then
    bad "$label: monthly sidecar carries an absolute path: $(cat "$vdir/$mn.sha256")"
  fi
  if grep -qF "$dn" "$vdir/$mn.sha256"; then
    bad "$label: monthly sidecar still names the DAILY object ($dn)"
  fi
  if ! grep -qF "  $mn" "$vdir/$mn.sha256"; then
    bad "$label: monthly sidecar does not name $mn: $(cat "$vdir/$mn.sha256")"
  fi
  # The copy is server-side, so the monthly digest must equal the daily one.
  got="$(cut -d' ' -f1 "$vdir/$mn.sha256")"
  assert_eq "$label: monthly digest equals the daily digest" "$want" "$got"
  rm -rf "$vdir"
  check_isolated "$label" pass
}

# promote_and_verify — run a full backup with the 1st-of-month branch driven on,
# then verify the pair. This is the case that would have caught the defect.
promote_and_verify() {
  local label="$1"; shift
  rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 STUB_RCLONE_MODE=ok --)"
  assert_eq "$label: exit code" "exit=0" "$rc"
  assert_contains "$label: promotion ran" "first of month: promoting to" "$RUN_LOG"
  assert_contains "$label: promotion reported ok" "monthly copy ok" "$RUN_LOG"
  assert_file   "$label: monthly object on the remote" \
    "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz"
  verify_monthly_pair "$label"
}

# bite_toolbox was replaced by an exact Python edit: the sabotage is a literal
# string swap on a COPY of the script inside the case dir, so there is no
# shell-escaping layer to get wrong and the tracked file is never touched.

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

echo "case: monthly promotion writes a sidecar that NAMES the monthly object (sha256sum -c passes)"
# The defect: the promotion copied the DAILY sidecar verbatim, so the content
# stayed "<digest>  /var/backups/jomform/jomform-daily-<date>.dump.gz". The
# monthly artifact then failed `sha256sum -c` — the path does not resolve — and
# once local retention pruned the daily file the sidecar pointed at nothing.
# The promotion branch is gated on the 1st of the month, so this case drives it
# through the JFBACKUP_DAY_OF_MONTH seam; without a seam the branch would never
# execute and the case would be decoration.
setup_case; touch_rclone_conf
promote_and_verify "monthly promotion"
teardown_case

echo "case: monthly sidecar write fails -> the run FAILS (uploads_ok=0), not a silent 0"
# The other half of the rule: a promotion whose sidecar never landed must not be
# reported as a healthy run with an unverifiable monthly copy.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 STUB_RCLONE_MODE=ok STUB_RCAT_FAIL=1 --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the sidecar write" "monthly sidecar write failed" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
teardown_case

echo "case: no promotion on a day that is not the 1st (the gate still holds)"
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=20 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
if grep -q "first of month: promoting to" "$RUN_LOG"; then
  bad "promoted on the 20th — the 1st-of-month gate is gone"
else
  ok "no promotion on the 20th"
fi
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz" ]; then
  bad "a monthly object was written on the 20th"
else
  ok "no monthly object written on the 20th"
fi
teardown_case

echo "case: with NO override the script promotes iff today really is the 1st (the seam is not a bypass)"
# A seam that silently forced 01 would promote every night, duplicating the
# month's retention slot. Drive a run with the override UNSET and assert the
# real-date behaviour: the expectation is computed from `date -u +%d`, so this
# case is correct whatever day it runs on and would catch a hard-coded 01.
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
if [ "$(date -u +%d)" = "01" ]; then
  assert_contains "it IS the 1st: real promotion ran" "first of month: promoting to" "$RUN_LOG"
  verify_monthly_pair "unset override"
else
  if grep -q "first of month: promoting to" "$RUN_LOG"; then
    bad "promoted with NO override on $(date -u +%d) — the seam bypasses the real date"
  else
    ok "no promotion with no override on $(date -u +%d) (real date respected)"
  fi
fi
teardown_case

echo "case: the monthly case BITES — restore the pre-fix behaviour and it goes RED"
# A monthly assertion that can never fail is decoration, and the suite that
# shipped this defect had none at all. This re-runs the promotion against a
# sabotaged COPY of the script with the old behaviour put back — the monthly
# sidecar is a verbatim copyto of the DAILY sidecar, so its content names the
# daily path — and requires the monthly assertions to go red.
#
# The sabotage is a literal, exact string replacement performed by python3 on a
# file INSIDE the case dir. The tracked script is never written to, and there is
# no shell-escaping layer between the two strings (an earlier sed-shaped version
# of this silently failed to apply, which is how a "bite" case becomes a lie).
setup_case; touch_rclone_conf
cp "$SCRIPT" "$CASE_DIR/bite-target.sh"
python3 - "$CASE_DIR/bite-target.sh" "$CASE_DIR/bite-applied.txt" <<'PY'
import sys
path, marker = sys.argv[1], sys.argv[2]
src = open(path).read()
# The block this fix introduced, as shipped...
fixed = '''    elif ! printf '%s  %s\\n' "$digest" "$MONTHLY_NAME.dump.gz" \\
        | rclone --config "$RCLONE_CONFIG" rcat "$MONTHLY_DIR/$MONTHLY_NAME.dump.gz.sha256" 2>/dev/null; then'''
# ...replaced by the pre-fix behaviour: copy the daily sidecar verbatim.
broken = '''    elif ! rclone --config "$RCLONE_CONFIG" copyto \\
        "$DAILY_DIR/$DAILY_NAME.dump.gz.sha256" "$MONTHLY_DIR/$MONTHLY_NAME.dump.gz.sha256" 2>/dev/null; then'''
if src.count(fixed) != 1:
    open(marker, 'w').write('NOTFOUND count=%d' % src.count(fixed))
    sys.exit(3)
open(path, 'w').write(src.replace(fixed, broken))
open(marker, 'w').write('APPLIED')
PY
sab_status="$(cat "$CASE_DIR/bite-applied.txt" 2>/dev/null)"
assert_eq "sabotage applied to the copy (NOTFOUND means this case proves nothing)" "APPLIED" "$sab_status"
if bash -n "$CASE_DIR/bite-target.sh"; then ok "the sabotaged copy still parses"; else bad "the sabotaged copy does not parse"; fi
sab_rc="$(run_backup_script "$CASE_DIR/bite-target.sh" \
  JFBACKUP_DAY_OF_MONTH=01 STUB_RCLONE_MODE=ok --)"
# The pre-fix script reported a healthy run: that is what made the defect silent.
assert_eq "the pre-fix script still reports SUCCESS (it never noticed)" "exit=0" "$sab_rc"
dn="jomform-daily-$(daily_name).dump.gz"
mn="jomform-monthly-$(monthly_name).dump.gz"
if [ -f "$CASE_DIR/remote/$mn.sha256" ]; then
  if grep -qF "$dn" "$CASE_DIR/remote/$mn.sha256"; then
    ok "RED as required: the pre-fix monthly sidecar names the DAILY object"
  else
    bad "the sabotaged sidecar does not name the daily object — the sabotage is wrong: $(cat "$CASE_DIR/remote/$mn.sha256")"
  fi
  # The real proof, and the one that needs the retention model: with the staged
  # daily files pruned, the pre-fix sidecar's absolute path resolves nowhere, so
  # the checksum MUST fail. Assert that exact expectation rather than "it isn't
  # green", which would also be satisfied by a broken harness.
  check_isolated "pre-fix" fail
else
  bad "the pre-fix script wrote no monthly sidecar at all — cannot judge the bite"
fi
teardown_case

echo
printf '%d passed, %d failed\n' "$PASSES" "$FAILURES"
[ "$FAILURES" -eq 0 ]
