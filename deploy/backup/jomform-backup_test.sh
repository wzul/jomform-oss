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
# For the refusal path: the point is what did NOT happen (no email, no dump), and
# "the string is absent" is the only assertion that can catch a leak.
assert_not_contains() { # label needle file
  if grep -qF -- "$2" "$3" 2>/dev/null; then
    bad "$1: '$2' MUST NOT appear in $3: $(grep -nF -- "$2" "$3" | head -3 | tr '\n' ' ')"
  else
    ok "$1"
  fi
}
assert_file() { # label path
  if [ -f "$2" ]; then ok "$1"; else bad "$1: $2 does not exist"; fi
}
# The fail-closed assertions have to be about what did NOT happen; the stage
# directory holding exactly what it held before the run is the only shape that
# proves the refusal landed BEFORE the dump.
assert_no_file() { # label path
  if [ -e "$2" ]; then bad "$1: $2 exists but must not"; else ok "$1"; fi
}
assert_same_listing() { # label expected-listing dir
  local now
  now="$(ls -A "$2" 2>/dev/null | sort | tr '\n' ' ')"
  if [ "$1" = "$now" ]; then ok "$1: listing unchanged"
  else bad "$1: listing changed: before '$1' after '$now'"; fi
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
#
# Every knob the script's fail-closed test seam watches must be set here (and by
# any hand-run), which is the point: safety is a property of the CALLER in the
# committed suite too, so the suite must satisfy the guard the same way an
# operator has to. A case that needs a knob UNSET on purpose passes an explicit
# override (see the seam cases below), and the sabotaged-COPY case below reaches
# the same guard through this same helper.
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
      JFBACKUP_REMOTE="googldrive" \
      JFBACKUP_REMOTE_ROOT="jfbackup-test" \
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
assert_contains "upload verified" "verified googldrive:jfbackup-test/daily/" "$RUN_LOG"
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
assert_not_contains "the failure is logged at ERROR, not downgraded to WARN" "[WARN] jomform-backup FAILED" "$RUN_LOG"
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
    JFBACKUP_REMOTE="googldrive" \
    JFBACKUP_REMOTE_ROOT="jfbackup-test" \
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

echo "case: a restore whose pg_restore FAILS is reported as FAILED, not OK"
# Every other --restore case drives a stub that SUCCEEDS, so a drill that
# swallowed the pg_restore exit status would still look green. This one makes
# pg_restore fail and requires the drill to record a FAILED status.
setup_case; touch_rclone_conf
cat >"$STUB_BIN/docker" <<EOD
#!/usr/bin/env bash
case "\$1" in
  ps) echo jomform-postgres-1 ;;
  create) echo fakecid ;;
  start|stop|rm) exit 0 ;;
  exec)
    shift
    while [ "\$#" -gt 0 ]; do case "\$1" in -i|-T) shift ;; *) break ;; esac; done
    shift
    case "\$1" in
      pg_restore) cat >/dev/null; echo 'ERROR: relation "users" already exists' >&2; exit 1 ;;
      pg_isready) exit 0 ;;
      psql) exit 0 ;;
    esac ;;
esac
exit 0
EOD
chmod +x "$STUB_BIN/docker"
rc="$(run_backup -- --restore "$CASE_DIR/stage/payload.dump")"
assert_eq "drill exit code" "exit=1" "$rc"
assert_contains "the drill reports the failure" "pg_restore failed" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "the drill status records FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-restore-drill.json"
assert_not_contains "the drill does not claim success" "restore drill complete" "$RUN_LOG"
teardown_case

echo "case: the job works even when cron's PATH resolves nothing"
# `env -i` on its own does NOT prove the script pins its PATH: when the
# environment carries no PATH, bash supplies a default (/usr/bin:/bin), every
# command still resolves, and a script that merely inherited PATH would pass.
# Hand the job an unusable PATH and require it to work anyway — that is what
# "pinned" means, and it is the cron failure this case exists to prevent.
setup_case; touch_rclone_conf
run_log="$CASE_DIR/run.log"
env -i \
    PATH=/nonexistent-path \
    JFBACKUP_PATH_PREFIX="$STUB_BIN" \
    JFBACKUP_STAGE_DIR="$CASE_DIR/stage" \
    JFBACKUP_STATE_DIR="$CASE_DIR/state" \
    JFBACKUP_LOG_FILE="$run_log" \
    JFBACKUP_LOCK_FILE="$CASE_DIR/lock" \
    JFBACKUP_ENV_FILE="$CASE_DIR/env" \
    JFBACKUP_REMOTE=googldrive \
    JFBACKUP_REMOTE_ROOT=jfbackup-test \
    RCLONE_CONFIG="$CASE_DIR/rclone.conf" \
    STUB_RCLONE_MODE=ok \
    /bin/bash "$SCRIPT" --local-only >"$run_log" 2>&1
rc=$?
RUN_LOG="$run_log"
assert_eq "exit code with an unusable inherited PATH" "0" "$rc"
assert_contains "the dump still happened" "dump ok" "$run_log"
assert_contains "the run completed rather than dying on a missing binary" "local-only: skipping upload" "$run_log"
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

# ---------------------------------------------------------------------------
# The fail-closed test seam. On 2026-09-19 a hand-run set STAGE_DIR/STATE_DIR/
# LOG_FILE and forgot ENV_FILE/REMOTE_ROOT: it read the real SES credentials out
# of /opt/jomform/.env, mailed the real super-admin inbox, and wrote into the
# production Drive folder. These cases hold that shape to a refusal.
# ---------------------------------------------------------------------------

# run_backup_unset "<VARS to remove>" [assignments] -- [args]
# Every seam knob is set first and then the named ones are REMOVED, so each case
# isolates exactly one leg: an "ENV_FILE unset" case that also left REMOTE_ROOT
# unset would still refuse for the other reason and would pass with the ENV_FILE
# leg deleted — a case that measures nothing. `env -u X X=value` does NOT work
# for this: env applies arguments in order, so the assignment re-sets it after
# the unset. The knob is filtered out of the list instead.
run_backup_unset() {
  local unsets=" $1 "; shift
  local extra=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do extra+=("$1"); shift; done
  [ "$#" -gt 0 ] && shift
  local assigns=(
    "JFBACKUP_PATH_PREFIX=$STUB_BIN"
    "JFBACKUP_STAGE_DIR=$CASE_DIR/stage"
    "JFBACKUP_STATE_DIR=$CASE_DIR/state"
    "JFBACKUP_LOG_FILE=$RUN_LOG"
    "JFBACKUP_LOCK_FILE=$CASE_DIR/lock"
    "JFBACKUP_ENV_FILE=$CASE_DIR/env"
    "JFBACKUP_REMOTE=googldrive"
    "JFBACKUP_REMOTE_ROOT=jfbackup-test"
    "RCLONE_CONFIG=$CASE_DIR/rclone.conf"
  )
  local keep=() a name
  for a in "${assigns[@]}"; do
    name="${a%%=*}"
    case "$unsets" in *" $name "*) continue ;; esac
    keep+=("$a")
  done
  env "${keep[@]}" "${extra[@]}" bash "$SCRIPT" "$@" >>"$RUN_LOG" 2>&1
  echo "exit=$?"
}

# install_backup_copy <stage> <state> <log> <lock> <env> <rclone-conf>
# Builds a COPY of the script that behaves like an INSTALLED host, at
# $CASE_DIR/jomform-backup-installed.sh. Two things are rewritten, and both are
# needed to express the shape the guard missed:
#
#   1. the `_DEFAULT` values point at this case's paths, so an installed host can
#      be simulated without writing to /var/backups on this machine;
#   2. the pinned `_SYS_PATH` puts the case's stub directory first, so the harness
#      gets at its stub `docker`/`rclone`/`logger` WITHOUT setting
#      JFBACKUP_PATH_PREFIX. That second part is load-bearing: PATH_PREFIX is
#      itself a trigger, so a fixture that used it would already be a hand-run
#      before the destination was read, and the case below would pass with the
#      destination triggers deleted — measuring nothing.
#
# run_backup_unset cannot express this shape at all: every knob it sets is a
# trigger.
install_backup_copy() {
  local stage="$1" state="$2" log="$3" lock="$4" envf="$5" conf="$6"
  local dst="$CASE_DIR/jomform-backup-installed.sh"
  python3 - "$SCRIPT" "$dst" "$stage" "$state" "$log" "$lock" "$envf" "$conf" "$STUB_BIN" <<'PY'
import sys
src, dst, stage, state, log, lock, envf, conf, stub = sys.argv[1:10]
s = open(src).read()
subs = {
    'RCLONE_CONFIG_DEFAULT="/root/.config/rclone/rclone.conf"': 'RCLONE_CONFIG_DEFAULT="%s"' % conf,
    'ENV_FILE_DEFAULT="/opt/jomform/.env"': 'ENV_FILE_DEFAULT="%s"' % envf,
    'STAGE_DIR_DEFAULT="/var/backups/jomform"': 'STAGE_DIR_DEFAULT="%s"' % stage,
    'STATE_DIR_DEFAULT="/var/lib/jomform-backup"': 'STATE_DIR_DEFAULT="%s"' % state,
    'LOG_FILE_DEFAULT="/var/log/jomform-backup.log"': 'LOG_FILE_DEFAULT="%s"' % log,
    'LOCK_FILE_DEFAULT="/var/lock/jomform-backup.lock"': 'LOCK_FILE_DEFAULT="%s"' % lock,
    '_SYS_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"':
        '_SYS_PATH="%s:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"' % stub,
}
for old, new in subs.items():
    assert old in s, "anchor not found: " + old
    s = s.replace(old, new, 1)
open(dst, "w").write(s)
PY
  chmod +x "$dst"
}

# run_installed [env assignments] -- [script args]
# Runs the installed-shape copy with NO trigger knob set, so the only thing that
# can move off its installed value is what an operator edits into the cron line.
run_installed() {
  local extra=() args=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do extra+=("$1"); shift; done
  [ "$#" -gt 0 ] && shift
  args=("$@")
  env ${extra[@]+"${extra[@]}"} \
      bash "$CASE_DIR/jomform-backup-installed.sh" \
      ${args[@]+"${args[@]}"} >>"$RUN_LOG" 2>&1
  echo "exit=$?"
}

echo "case: partial override (STAGE_DIR set, JFBACKUP_ENV_FILE unset) REFUSES before any work"
# The remote is deliberately left WORKING and the rclone config readable, so the
# only reason this run cannot succeed is the seam itself. With the guard removed
# the run uploads through the stub and exits 0 — which is what makes the
# assertions below bite rather than pass by accident.
setup_case; touch_rclone_conf
before="$(ls -A "$CASE_DIR/stage" | sort | tr '\n' ' ')"
rc="$(run_backup_unset JFBACKUP_ENV_FILE STUB_RCLONE_MODE=no-remote --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "one line names the missing variable" "JFBACKUP_ENV_FILE" "$RUN_LOG"
assert_contains "log says it refused" "refusing a partial-override run" "$RUN_LOG"
assert_contains "the override that triggered it is named" "JFBACKUP_STAGE_DIR" "$RUN_LOG"
assert_contains "names the production value it would have fallen back to" "/opt/jomform/.env" "$RUN_LOG"
# ORDERING, not just the exit code: an exit 1 AFTER the dump is not fail-closed.
assert_no_file "no dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_no_file "no media archive was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d)-media.tar.gz"
assert_same_listing "$before" "$CASE_DIR/stage"
assert_no_file "no lock file was taken" "$CASE_DIR/lock"
# The email is the half the operator actually saw: the alert must not be raised.
assert_not_contains "no alert email was attempted" "[JomForm]" "$RUN_LOG"
assert_not_contains "no ALERT line was logged" " ALERT " "$RUN_LOG"
assert_contains "failure is recorded in the log" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "syslog record written" "jomform-backup FAILED" "$CASE_DIR/alerts.log"
assert_contains "status file records the refusal" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
teardown_case

echo "case: partial override with JFBACKUP_REMOTE_ROOT unset also REFUSES"
setup_case; touch_rclone_conf
before="$(ls -A "$CASE_DIR/stage" | sort | tr '\n' ' ')"
rc="$(run_backup_unset JFBACKUP_REMOTE_ROOT STUB_RCLONE_MODE=no-remote --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "one line names the missing variable" "JFBACKUP_REMOTE_ROOT" "$RUN_LOG"
assert_contains "log says it refused" "refusing a partial-override run" "$RUN_LOG"
assert_contains "names the production folder it would have fallen back to" "jomform-backups" "$RUN_LOG"
assert_no_file "no dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_same_listing "$before" "$CASE_DIR/stage"
assert_not_contains "no alert email was attempted" "[JomForm]" "$RUN_LOG"
assert_contains "syslog record written" "jomform-backup FAILED" "$CASE_DIR/alerts.log"
teardown_case

echo "case: partial override naming both missing variables lists both in one line"
setup_case; touch_rclone_conf
rc="$(run_backup_unset "JFBACKUP_ENV_FILE JFBACKUP_REMOTE JFBACKUP_REMOTE_ROOT" STUB_RCLONE_MODE=no-remote --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "names ENV_FILE" "JFBACKUP_ENV_FILE" "$RUN_LOG"
assert_contains "names REMOTE" "JFBACKUP_REMOTE" "$RUN_LOG"
assert_contains "names REMOTE_ROOT" "JFBACKUP_REMOTE_ROOT" "$RUN_LOG"
assert_not_contains "no alert email was attempted" "[JomForm]" "$RUN_LOG"
teardown_case

echo "case: a --restore drill with a partial override is refused too, before its container"
setup_case; touch_rclone_conf
rc="$(run_backup_unset JFBACKUP_ENV_FILE -- --restore "$CASE_DIR/stage/payload.dump")"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "refused" "refusing a partial-override run" "$RUN_LOG"
assert_not_contains "the drill never started" "restore drill: container" "$RUN_LOG"
teardown_case

echo "case: destination-only override (no LOCATION knob moved) REFUSES before any work"
# The shape the guard missed: the run moves ONLY the destination. The installed
# job sets NEITHER JFBACKUP_REMOTE nor JFBACKUP_REMOTE_ROOT, so watching only the
# local knobs classified this as the nightly job and the guard returned before its
# "was it provided" checks — leaving the credential half at the installed
# JFBACKUP_ENV_FILE (/opt/jomform/.env, real SES keys and SUPER_ADMIN_EMAILS) and
# mailing the real inbox. Nothing has to be forgotten for this shape either: an
# operator editing the installed cron line to try a scratch folder produces it.
#
# run_backup_unset cannot express it — every knob it sets is itself a trigger, so
# the run would be a hand-run before the destination was read. The fixture below
# is the INSTALLED shape (no knob set at all) with one destination knob moved.
setup_case; touch_rclone_conf
install_backup_copy "$CASE_DIR/stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"
before="$(ls -A "$CASE_DIR/stage" | sort | tr '\n' ' ')"
rc="$(run_installed JFBACKUP_REMOTE_ROOT=scratch-root)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log says it refused" "refusing a partial-override run" "$RUN_LOG"
assert_contains "the destination knob that moved is named" "while JFBACKUP_REMOTE_ROOT" "$RUN_LOG"
assert_contains "names the credential file it would have read" "$CASE_DIR/env" "$RUN_LOG"
# ORDERING: this is the half that leaked on a destination-only run, so the refusal
# has to land before the dump and before the lock, not after them.
assert_no_file "no dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_no_file "no media archive was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d)-media.tar.gz"
assert_same_listing "$before" "$CASE_DIR/stage"
assert_no_file "no lock file was taken" "$CASE_DIR/lock"
assert_not_contains "no alert email was attempted" "[JomForm]" "$RUN_LOG"
assert_contains "status file records the refusal" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
teardown_case

echo "case: the SAME destination-only run with all three legs provided proceeds"
# The refusal must be about the missing legs, not about moving the destination: a
# hand-run that names its own credential file, remote and folder is legitimate and
# has to still work, or the guard is a ban on hand-runs rather than a seam.
setup_case; touch_rclone_conf
install_backup_copy "$CASE_DIR/stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"
rc="$(run_installed JFBACKUP_ENV_FILE="$CASE_DIR/env" JFBACKUP_REMOTE=googldrive JFBACKUP_REMOTE_ROOT=scratch-root -- --local-only)"
assert_eq "exit code" "exit=0" "$rc"
assert_not_contains "the seam did not fire" "refusing a partial-override run" "$RUN_LOG"
assert_file "dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
teardown_case

echo "case: the SAME run with the seam knobs set proceeds normally"
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "log says OK" "jomform-backup OK" "$RUN_LOG"
assert_file "dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_not_contains "seam did not fire" "refusing a partial-override run" "$RUN_LOG"
teardown_case

echo "case: JFBACKUP_ALLOW_UNSAFE=1 is the deliberate escape hatch, and it WARNs"
setup_case; touch_rclone_conf
# All three production fallbacks unset, so the WARN has to name the real values
# it is about to reach — that is the operator's only signal of what they hit.
rc="$(run_backup_unset "JFBACKUP_ENV_FILE JFBACKUP_REMOTE JFBACKUP_REMOTE_ROOT" JFBACKUP_ALLOW_UNSAFE=1 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "warns the hatch was used" "JFBACKUP_ALLOW_UNSAFE=1" "$RUN_LOG"
assert_contains "names the credential file reached" "/opt/jomform/.env" "$RUN_LOG"
assert_contains "names the production remote reached" "googldrive:jomform-backups" "$RUN_LOG"
assert_contains "syslog carries the unsafe override too" "unsafe override allowed" "$CASE_DIR/alerts.log"
assert_file "dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
teardown_case

echo "case: the installed cron shape is NOT refused (no false positive on the nightly job)"
# The four location knobs at their installed values with ENV_FILE/REMOTE_ROOT
# unset is exactly what cron runs, and it must stay allowed — a seam that
# refuses the nightly job is worse than none. Checked against the documented
# cron entry rather than by running it: executing the installed shape offline
# would write to /var/backups on this host, and the property under test is the
# classification, which is determined by what the cron line sets.
cron_line="$(grep -m1 -F '/usr/local/bin/jomform-backup.sh' "$SCRIPT_DIR/README.md" || true)"
if [ -z "$cron_line" ]; then
  bad "no cron entry found in README to check the installed shape against"
elif printf '%s' "$cron_line" | grep -qE 'JFBACKUP_|RCLONE_CONFIG='; then
  bad "the documented cron entry overrides a seam knob — the guard would refuse the nightly job: $cron_line"
else
  ok "the cron entry sets none of the seam knobs (so the nightly job is never refused)"
fi
# And the guard must fire on a value that merely DIFFERS for the knobs the
# installed job sets, which is how the installed job is told apart from a
# hand-run. The two DESTINATION knobs are in this list too: a run that moves only
# the destination has contained only its drive half, and the credential half it
# still reaches is the one the guard exists to protect.
for knob in JFBACKUP_STAGE_DIR JFBACKUP_STATE_DIR JFBACKUP_LOG_FILE JFBACKUP_LOCK_FILE RCLONE_CONFIG JFBACKUP_REMOTE JFBACKUP_REMOTE_ROOT; do
  if grep -qE "^seam_moved ${knob}[[:space:]]" "$SCRIPT"; then
    ok "seam watches $knob"
  else
    bad "seam does not watch $knob — a hand-run that moves only that knob is not detected"
  fi
done
# The guard must test PROVISION, not value: requiring REMOTE_ROOT to *differ*
# would let an operator point it at the scratch name and still be refused, and
# requiring it to merely be non-empty would not distinguish default from set.
for knob in JFBACKUP_ENV_FILE JFBACKUP_REMOTE JFBACKUP_REMOTE_ROOT; do
  if grep -qE "^  if \[ -z \"\\\$\{${knob}:-\}\" \]; then" "$SCRIPT"; then
    ok "seam tests that $knob was PROVIDED (not that its value differs)"
  else
    bad "seam does not test provision of $knob"
  fi
done

echo
printf '%d passed, %d failed\n' "$PASSES" "$FAILURES"
[ "$FAILURES" -eq 0 ]
