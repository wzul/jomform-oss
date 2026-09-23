#!/usr/bin/env bash
#
# JomForm nightly backup: Postgres dump + media volume -> Google Drive via rclone.
#
# Installed on the production host OUTSIDE the checkout (default /usr/local/bin).
# The rclone config and its OAuth token are NEVER part of this repository:
# they live at /root/.config/rclone/rclone.conf (mode 600) and the repo may go
# public, with gitleaks scanning the full history.
#
# Design rules this script exists to enforce:
#   - A backup is only a backup once it has been RESTORED. `restore` is a
#     first-class mode here (see --restore-drill) and the README documents it.
#   - A SILENT failure is the bug this closes. Every failure path logs, writes a
#     status file, raises a syslog alert and emails the super-admins. Exit code
#     is non-zero on any failure so a cron MAILTO would also fire.
#   - "Exited 0" is not "uploaded". After the upload the remote object is
#     re-read and its size and hash compared against the local file.
#
# Usage:
#   jomform-backup.sh                     run a full backup (cron entry)
#   jomform-backup.sh --no-media          skip the media archive
#   jomform-backup.sh --local-only        dump + verify, do not upload
#   jomform-backup.sh --restore <file>    restore a dump into a throwaway
#                                         Postgres and print row counts
#
set -Eeuo pipefail

# cron starts a job with a minimal PATH; every command this script needs
# (docker, rclone, gzip, tar, python3, logger, flock) lives in /usr/bin or /bin.
# Pin the standard system path explicitly so the job cannot depend on an
# interactive shell's PATH. JFBACKUP_PATH_PREFIX exists so a test harness can
# put its stubs in front of the real binaries.
_SYS_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
if [ -n "${JFBACKUP_PATH_PREFIX:-}" ]; then
  PATH="${JFBACKUP_PATH_PREFIX}:${_SYS_PATH}"
else
  PATH="$_SYS_PATH"
fi
export PATH

# ---------------------------------------------------------------- configuration
# The installed values, named, so the fail-closed seam below can tell "this is
# the cron job" from "somebody is running this by hand".
REMOTE_DEFAULT="googldrive"
REMOTE_ROOT_DEFAULT="jomform-backups"
RCLONE_CONFIG_DEFAULT="/root/.config/rclone/rclone.conf"
ENV_FILE_DEFAULT="/opt/jomform/.env"
STAGE_DIR_DEFAULT="/var/backups/jomform"
STATE_DIR_DEFAULT="/var/lib/jomform-backup"
LOG_FILE_DEFAULT="/var/log/jomform-backup.log"
LOCK_FILE_DEFAULT="/var/lock/jomform-backup.lock"

REMOTE="${JFBACKUP_REMOTE:-$REMOTE_DEFAULT}"
REMOTE_ROOT="${JFBACKUP_REMOTE_ROOT:-$REMOTE_ROOT_DEFAULT}"
RCLONE_CONFIG="${RCLONE_CONFIG:-$RCLONE_CONFIG_DEFAULT}"
ENV_FILE="${JFBACKUP_ENV_FILE:-$ENV_FILE_DEFAULT}"
STAGE_DIR="${JFBACKUP_STAGE_DIR:-$STAGE_DIR_DEFAULT}"
STATE_DIR="${JFBACKUP_STATE_DIR:-$STATE_DIR_DEFAULT}"
LOG_FILE="${JFBACKUP_LOG_FILE:-$LOG_FILE_DEFAULT}"
LOCK_FILE="${JFBACKUP_LOCK_FILE:-$LOCK_FILE_DEFAULT}"
KEEP_DAILY="${JFBACKUP_KEEP_DAILY:-30}"
KEEP_MONTHLY="${JFBACKUP_KEEP_MONTHLY:-12}"
MEDIA_ENABLED="${JFBACKUP_MEDIA_ENABLED:-1}"
PG_SERVICE_LABEL="com.docker.compose.service=postgres"
MINIO_SERVICE_LABEL="com.docker.compose.service=minio"

HOSTNAME_S="$(hostname)"
STAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DAILY_NAME="jomform-daily-$(date -u +%Y-%m-%d)"
MONTHLY_NAME="jomform-monthly-$(date -u +%Y-%m)"
# Test seam. The monthly promotion only runs on the 1st, so without an override
# the promotion branch could never execute in the test suite — which is how a
# monthly sidecar that fails `sha256sum -c` shipped with every case green. The
# harness sets JFBACKUP_DAY_OF_MONTH to drive that branch; production never does.
DAY_OF_MONTH="${JFBACKUP_DAY_OF_MONTH:-$(date -u +%d)}"
LOCAL_ONLY=0
NO_MEDIA=0
RESTORE_FILE=""

STATUS="FAILED"
DETAIL=""
PG_BYTES=0
MEDIA_BYTES=0
UPLOADED=""
RESTORE_COUNTS=""
# Which run this is decides where the status is written. A backup run owns
# last-run.json; a drill records itself separately (see write_status).
IS_BACKUP_RUN=1
STATUS_FILE_NAME="last-run.json"

# ------------------------------------------------------------------------ utils
ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }

log() { printf '%s [%s] %s\n' "$(ts)" "${1:-INFO}" "${*:2}"; }

syslog_alert() {
  # journald copy so the alert is visible even if the log file is unreadable.
  logger -t jomform-backup -p "${1:-user.err}" "${*:2}" 2>/dev/null || true
}

# Read KEY=VALUE from the compose env file without exporting anything else.
env_value() {
  local key="$1"
  [ -r "$ENV_FILE" ] || return 0
  sed -n "s/^${key}=//p" "$ENV_FILE" | head -n1 | tr -d '"'"'"'\r'
}

# Email the super-admins directly over SESv2.
# Deliberately NOT via the API/queue: if the database is why the backup failed,
# the API's email path is down too, and the alert would be lost with it.
alert_email() {
  local subject="$1" body="$2"
  # Log the alert first. The email can fail (SES down, creds rotated, no
  # network); the record of what was raised must not depend on the send
  # succeeding, and this is what a human reads when they open the log.
  log ALERT "$subject"
  printf '%s\n' "$body" | while IFS= read -r line; do log ALERT "  $line"; done
  syslog_alert user.err "$subject"
  local key secret from recipients region
  key="$(env_value SES_SMTP_USER)"
  secret="$(env_value SES_SMTP_PASS)"
  from="$(env_value SES_FROM)"
  recipients="$(env_value SUPER_ADMIN_EMAILS)"
  region="${JFBACKUP_SES_REGION:-ap-southeast-1}"
  if [ -z "$key" ] || [ -z "$secret" ] || [ -z "$from" ] || [ -z "$recipients" ]; then
    log WARN "alert email skipped: SES env values not readable from $ENV_FILE"
    syslog_alert user.warning "alert email skipped: SES env values not readable"
    return 0
  fi
  JFB_KEY="$key" JFB_SECRET="$secret" JFB_FROM="$from" \
  JFB_TO="$recipients" JFB_REGION="$region" \
  JFB_SUBJECT="$subject" JFB_BODY="$body" \
  python3 - <<'PY' || log WARN "alert email send failed (see traceback above)"
import os, sys, json
try:
    import boto3
except Exception as exc:  # pragma: no cover
    print("boto3 unavailable:", exc); sys.exit(1)
to = [a.strip() for a in os.environ["JFB_TO"].split(",") if a.strip()]
try:
    boto3.client(
        "sesv2",
        region_name=os.environ["JFB_REGION"],
        aws_access_key_id=os.environ["JFB_KEY"],
        aws_secret_access_key=os.environ["JFB_SECRET"],
    ).send_email(
        FromEmailAddress=os.environ["JFB_FROM"],
        Destination={"ToAddresses": to},
        Content={
            "Simple": {
                "Subject": {"Data": os.environ["JFB_SUBJECT"][:200]},
                "Body": {"Text": {"Data": os.environ["JFB_BODY"]}},
            }
        },
    )
    print("alert email sent to", ", ".join(to))
except Exception as exc:
    print("alert email FAILED:", type(exc).__name__, exc)
    sys.exit(1)
PY
}

json_escape() {
  # argv, not stdin: a heredoc/`<<<` would append a newline to the JSON value.
  python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$1"
}

write_status() {
  mkdir -p "$STATE_DIR"
  local target="$STATE_DIR/$STATUS_FILE_NAME"
  cat >"$target" <<JSON
{
  "status": $(json_escape "$STATUS"),
  "detail": $(json_escape "$DETAIL"),
  "finished_at": $(json_escape "$(ts)"),
  "host": $(json_escape "$HOSTNAME_S"),
  "postgres_bytes": ${PG_BYTES},
  "media_bytes": ${MEDIA_BYTES},
  "uploaded": $(json_escape "$UPLOADED"),
  "remote": $(json_escape "$REMOTE:$REMOTE_ROOT")
}
JSON
  chmod 600 "$target"
  # A restore drill must NOT touch the backup job's own status: if it wrote
  # last-run.json, an operator checking "is the nightly backup healthy?" would
  # read the drill's OK and never see that the backups had stopped succeeding.
  if [ "$IS_BACKUP_RUN" != "1" ]; then
    return 0
  fi
  if [ "$STATUS" = "OK" ]; then
    date -u +%s >"$STATE_DIR/last-success.epoch"
    rm -f "$STATE_DIR/last-failure.json"
  else
    cp "$target" "$STATE_DIR/last-failure.json"
  fi
}

fail() {
  STATUS="FAILED"
  DETAIL="$*"
  # One grep-able line naming the job and the failure, so `grep -n 'jomform-backup
  # FAILED' /var/log/jomform-backup.log` finds every failed run.
  log ERROR "jomform-backup FAILED: $DETAIL"
  syslog_alert user.err "jomform-backup FAILED on $HOSTNAME_S: $DETAIL"
  write_status
}

cleanup() {
  local rc=$?
  if [ $rc -ne 0 ] && [ "$STATUS" != "FAILED" ]; then
    fail "unexpected error, exit code $rc"
  fi
  return 0
}
trap cleanup EXIT

# ------------------------------------------------------------- partial override
# A backup run that has moved ANY knob off its installed value is not the cron
# job, it is a hand-run. On 2026-09-19 such a run (three knobs set, two
# forgotten) read the real SES credentials out of /opt/jomform/.env and mailed the
# real super-admin inbox while writing into the production Drive folder. Safety
# there came from the caller remembering; this makes it come from the script.
#
# `JFBACKUP_STAGE_DIR=...` etc. is the signal, and it has to cover every knob
# that moves a *half* of the run, not only the local ones:
#
#   - RCLONE_CONFIG and JFBACKUP_PATH_PREFIX are included because neither is ever
#     set by the installed cron job, and moving RCLONE_CONFIG is still a hand-run
#     — it does NOT contain the run, since alert_email reads $ENV_FILE directly
#     rather than going through rclone at all.
#   - JFBACKUP_REMOTE and JFBACKUP_REMOTE_ROOT are included for the same reason
#     from the other side: neither is ever set by the installed job, and a run
#     that moves them is a hand-run whose EMAIL half is still production. The
#     credential file it would alert through is chosen by a knob this run never
#     names, so watching only the local knobs left that shape classified as the
#     nightly job: the refusal sat above the "was it provided" checks and returned
#     before it could read the destination at all. A destination-only override is
#     the shape an operator reaching for "let me try it into a scratch folder
#     first" produces, and it must be classified as a hand-run too.
#
# The rule both directions share: the installed job sets neither of these, so a
# value that differs from the installed one is a move — the same comparison the
# local knobs get.
UNSAFE_KNOBS=""
seam_moved() { # VARNAME effective-value installed-default
  [ "$2" = "$3" ] || UNSAFE_KNOBS="${UNSAFE_KNOBS}${UNSAFE_KNOBS:+, }$1"
}
# `JFBACKUP_ENV_FILE` is absent deliberately: it IS named here where an operator
# may reach a run, but it names the file to READ credentials from, not where a
# half of the run goes. A run that points it at a scratch file is already
# contained on the credential half — the guard's job is to refuse a run that
# reaches production by OMISSION, which is the shape that failed on 2026-09-19.
seam_moved JFBACKUP_STAGE_DIR  "$STAGE_DIR"       "$STAGE_DIR_DEFAULT"
seam_moved JFBACKUP_STATE_DIR  "$STATE_DIR"       "$STATE_DIR_DEFAULT"
seam_moved JFBACKUP_LOG_FILE   "$LOG_FILE"        "$LOG_FILE_DEFAULT"
seam_moved JFBACKUP_LOCK_FILE  "$LOCK_FILE"       "$LOCK_FILE_DEFAULT"
seam_moved RCLONE_CONFIG       "$RCLONE_CONFIG"   "$RCLONE_CONFIG_DEFAULT"
seam_moved JFBACKUP_REMOTE      "$REMOTE"          "$REMOTE_DEFAULT"
seam_moved JFBACKUP_REMOTE_ROOT "$REMOTE_ROOT"     "$REMOTE_ROOT_DEFAULT"
if [ -n "${JFBACKUP_PATH_PREFIX:-}" ]; then
  UNSAFE_KNOBS="${UNSAFE_KNOBS}${UNSAFE_KNOBS:+, }JFBACKUP_PATH_PREFIX"
fi

# enforce_test_seam — refuse a partial override BEFORE any work (no dump, no
# upload, no `mkdir`). Tests "was it provided", not "does its value differ from
# the default": pointing REMOTE_ROOT at a scratch name is legitimate, so a guard
# that only refused a *changed* value would let the dangerous shape through.
enforce_test_seam() {
  [ -n "$UNSAFE_KNOBS" ] || return 0   # the installed nightly job: nothing to do
  if [ -n "${JFBACKUP_ALLOW_UNSAFE:-}" ]; then
    log WARN "JFBACKUP_ALLOW_UNSAFE=1: ${UNSAFE_KNOBS} overridden and the escape hatch was used deliberately. This run WILL read credentials from $ENV_FILE and write to $REMOTE:$REMOTE_ROOT."
    syslog_alert user.warning "jomform-backup: unsafe override allowed (${UNSAFE_KNOBS}); reached $ENV_FILE and $REMOTE:$REMOTE_ROOT"
    return 0
  fi
  # Each of these three falls back to a PRODUCTION value when omitted: the real
  # credential file, the real remote, the real folder inside it. "Provided"
  # means the caller named it, whatever the name is. `detail` names the specific
  # installed value a MISSING knob would fall back to — not the effective value,
  # which may legitimately be a scratch name.
  local missing="" detail=""
  if [ -z "${JFBACKUP_ENV_FILE:-}" ]; then
    missing="JFBACKUP_ENV_FILE"
    detail="$ENV_FILE_DEFAULT (production SES credentials and super-admin inbox)"
  fi
  if [ -z "${JFBACKUP_REMOTE:-}" ]; then
    missing="${missing:+$missing and }JFBACKUP_REMOTE"
    detail="${detail:+$detail; }$REMOTE_DEFAULT (the production remote)"
  fi
  if [ -z "${JFBACKUP_REMOTE_ROOT:-}" ]; then
    missing="${missing:+$missing and }JFBACKUP_REMOTE_ROOT"
    detail="${detail:+$detail; }$REMOTE_ROOT_DEFAULT (the production backup folder)"
  fi
  [ -z "$missing" ] && return 0
  # One actionable line, via fail so the status file and syslog record it too.
  # Deliberately NO alert email here: the alert is half of what leaked to the
  # real super-admin inbox on the run this guard exists for.
  fail "refusing a partial-override run: $missing not set while $UNSAFE_KNOBS are overridden, so the missing knob(s) would default to $detail. Set JFBACKUP_ENV_FILE, JFBACKUP_REMOTE and JFBACKUP_REMOTE_ROOT to scratch values, or set JFBACKUP_ALLOW_UNSAFE=1 for a deliberate real run."
  exit 1
}

# ------------------------------------------------------------------ discover DB
# Resolve containers by compose label: container names carry a project prefix
# and an index that changes on recreate.
container_for() {
  docker ps --filter "label=$1" --format '{{.Names}}' | head -n1
}

require_remote() {
  command -v rclone >/dev/null 2>&1 || { echo "rclone is not installed"; return 1; }
  [ -r "$RCLONE_CONFIG" ] || { echo "rclone config not readable at $RCLONE_CONFIG"; return 1; }
  rclone --config "$RCLONE_CONFIG" listremotes | grep -qx "${REMOTE}:" \
    || { echo "rclone remote '${REMOTE}:' is not configured"; return 1; }
}

# ------------------------------------------------------------------- upload ops
# rclone exit 0 means "the command completed"; it does NOT mean the bytes landed.
# verify_remote re-reads the object and compares size and hash.
verify_remote() {
  local local_file="$1" remote_path="$2" want_size got_size want_hash got_hash
  want_size="$(stat -c %s "$local_file")"
  got_size="$(rclone --config "$RCLONE_CONFIG" lsjson "$remote_path" \
    --files-only 2>/dev/null | python3 -c 'import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    d = []
print(d[0]["Size"] if d else "")' 2>/dev/null)"
  if [ -z "$got_size" ]; then
    echo "remote object missing or unreadable after upload: $remote_path"
    return 1
  fi
  if [ "$got_size" != "$want_size" ]; then
    echo "remote size mismatch for $remote_path: local=$want_size remote=$got_size"
    return 1
  fi
  # Hash the remote FILE (hashsum/md5sum take a file path). `rclone check`
  # cannot be used here: it treats its second argument as a DIRECTORY and
  # fails with "is a file not a directory", so it can never verify one object.
  want_hash="$(md5sum "$local_file" | cut -d' ' -f1)"
  got_hash="$(rclone --config "$RCLONE_CONFIG" hashsum MD5 "$remote_path" 2>/dev/null | cut -d' ' -f1)"
  if [ -z "$got_hash" ]; then
    echo "remote hash unavailable for $remote_path (size matched: $want_size)"
    return 1
  fi
  if [ "$got_hash" != "$want_hash" ]; then
    echo "remote hash mismatch for $remote_path: local md5=$want_hash remote md5=$got_hash"
    return 1
  fi
  return 0
}

upload_file() {
  local local_file="$1" dest_dir="$2"
  rclone --config "$RCLONE_CONFIG" copy "$local_file" "$dest_dir" \
    --drive-use-trash=false --retries 3 --low-level-retries 5
}

# Prune a remote directory to the newest N matching files.
prune_remote() {
  local dir="$1" keep="$2" pattern="$3" listing total idx=0
  listing="$(rclone --config "$RCLONE_CONFIG" lsf "$dir" --files-only 2>/dev/null \
    | grep -E "$pattern" | sort -r || true)"
  total="$(printf '%s\n' "$listing" | grep -c . || true)"
  [ -z "$listing" ] && return 0
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    idx=$((idx + 1))
    [ "$idx" -le "$keep" ] && continue
    log INFO "retention: deleting $dir$f"
    rclone --config "$RCLONE_CONFIG" deletefile "$dir$f" 2>/dev/null \
      || log WARN "retention: could not delete $dir$f"
  done <<<"$listing"
  log INFO "retention: $dir kept $(( total < keep ? total : keep )) of $total"
}

# -------------------------------------------------------------------- pg_dump
do_dump() {
  local pg out
  pg="$(container_for "$PG_SERVICE_LABEL")"
  [ -n "$pg" ] || { echo "no running container with label $PG_SERVICE_LABEL"; return 1; }
  out="$STAGE_DIR/$DAILY_NAME.dump.gz"
  log INFO "pg_dump from container $pg -> $out"
  # < /dev/null: docker exec otherwise consumes the caller's stdin and silently
  # truncates anything piped after it.
  if ! docker exec "$pg" pg_dump -U jomform -d jomform -Fc < /dev/null | gzip -9 >"$out"; then
    echo "pg_dump pipeline failed"
    return 1
  fi
  # Guard against a dump that "succeeded" having produced an empty or malformed
  # archive (e.g. the container was mid-restart). Note: the archive is fed to
  # pg_restore through an explicit FILE redirect, never through the caller's
  # stdin — `docker exec` reads stdin, and inside an `ssh host 'bash -s'` run it
  # would swallow the rest of the script and stop with exit 0.
  local tables
  gunzip -c "$out" >"$out.raw"
  tables="$(docker exec -i "$pg" pg_restore --list <"$out.raw" | grep -c 'TABLE DATA' || true)"
  rm -f "$out.raw"
  if [ "${tables:-0}" -lt 1 ]; then
    echo "pg_dump produced an archive with no table data ($out)"
    return 1
  fi
  PG_BYTES="$(stat -c %s "$out")"
  sha256sum "$out" >"$out.sha256"
  log INFO "dump ok: $PG_BYTES bytes, $tables table-data entries, sha256 $(cut -c1-16 "$out.sha256")"
  return 0
}

# --------------------------------------------------------------- media volume
# Covered deliberately: the MinIO volume holds customer-uploaded media and is
# small (hundreds of KB today). A DB-only restore would leave media rows pointing
# at objects that no longer exist.
do_media() {
  [ "$MEDIA_ENABLED" = "1" ] || { log INFO "media archive disabled"; return 0; }
  [ "$NO_MEDIA" = "1" ] && { log INFO "media archive skipped (--no-media)"; return 0; }
  local minio src out
  minio="$(container_for "$MINIO_SERVICE_LABEL")"
  [ -n "$minio" ] || { echo "no running container with label $MINIO_SERVICE_LABEL"; return 1; }
  src="$(docker inspect "$minio" --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Source}}{{end}}{{end}}')"
  [ -n "$src" ] && [ -d "$src" ] || { echo "could not resolve the minio /data volume source (got '$src')"; return 1; }
  out="$STAGE_DIR/$DAILY_NAME-media.tar.gz"
  log INFO "media archive $src -> $out"
  if ! tar -C "$src" -czf "$out" .; then
    echo "media archive failed"
    return 1
  fi
  MEDIA_BYTES="$(stat -c %s "$out")"
  log INFO "media ok: $MEDIA_BYTES bytes"
  return 0
}

# ------------------------------------------------------------------- retention
# Local staging keeps 3 days; Drive is the durable copy.
prune_local() {
  local f
  for f in $(ls -1t "$STAGE_DIR"/jomform-daily-* 2>/dev/null | tail -n +7); do
    rm -f "$f"
  done
}

# --------------------------------------------------------------- restore drill
# Restores a dump into a THROWAWAY postgres and prints real row counts. This is
# the only check that proves the dump is a backup.
do_restore() {
  local file="$1" name cid
  [ -r "$file" ] || { echo "dump not found or unreadable: $file"; return 1; }
  name="jf-restore-$$-$RANDOM"
  cid="$(docker create --name "$name" \
    -e POSTGRES_PASSWORD=restoretest -e POSTGRES_DB=postgres postgres:18-alpine)"
  docker start "$cid" >/dev/null
  log INFO "restore drill: container $name"
  local i
  for i in $(seq 1 60); do
    docker exec "$cid" pg_isready -U postgres -q < /dev/null && break
    sleep 1
  done
  docker exec "$cid" psql -U postgres -Atc 'CREATE DATABASE jomform_restore' < /dev/null >/dev/null
  # pg_restore reads the dump through a pipe; psql below gets </dev/null so it
  # cannot consume the caller's stdin (docker exec reads stdin, and inside an
  # `ssh host 'bash -s'` run it would swallow the rest of the script).
  if ! gunzip -c "$file" | docker exec -i "$cid" pg_restore \
      -U postgres -d jomform_restore --no-owner --exit-on-error >/dev/null 2>&1; then
    # Print the pipeline's own diagnostics. `set -o pipefail` reports a bare
    # "restore failed" for a SIGPIPE from either side, which is unactionable.
    local diag
    diag="$(gunzip -c "$file" 2>&1 | docker exec -i "$cid" pg_restore \
      -U postgres -d jomform_restore --no-owner --exit-on-error 2>&1 | tail -5 || true)"
    docker stop "$cid" >/dev/null 2>&1 || true
    docker rm -f "$cid" >/dev/null 2>&1 || true
    echo "pg_restore failed for $file: $diag"
    return 1
  fi
  RESTORE_COUNTS="$(docker exec "$cid" psql -U postgres -d jomform_restore -Atc "
    select 'users', count(*) from users union all
    select 'accounts', count(*) from accounts union all
    select 'forms', count(*) from forms union all
    select 'sales', count(*) from sales union all
    select 'submissions', count(*) from submissions union all
    select 'workspaces', count(*) from workspaces" < /dev/null)"
  printf '%s\n' "$RESTORE_COUNTS" | while IFS='|' read -r k v; do
    log INFO "restored $k = $v"
  done
  docker stop "$cid" >/dev/null 2>&1 || true
  docker rm -f "$cid" >/dev/null 2>&1 || true
  log INFO "restore drill complete for $file"
  return 0
}

# --------------------------------------------------------------------- main
# Argument parsing comes first: --help must work, and an unknown argument must
# still be a usage error rather than a seam refusal.
while [ $# -gt 0 ]; do
  case "$1" in
    --local-only) LOCAL_ONLY=1; shift ;;
    --no-media) NO_MEDIA=1; shift ;;
    --restore) RESTORE_FILE="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

# Fail closed before ANY work: no mkdir, no lock, no dump, no upload, no email.
# A partial-override run must not reach production credentials or the production
# Drive folder, so this runs above everything that could touch them.
enforce_test_seam

mkdir -p "$STAGE_DIR" "$STATE_DIR"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log WARN "another jomform-backup run holds $LOCK_FILE; exiting"
  exit 0
fi

if [ -n "$RESTORE_FILE" ]; then
  # A drill is not a backup: record it under its own name so it can never
  # overwrite the nightly job's status.
  IS_BACKUP_RUN=0
  STATUS_FILE_NAME="last-restore-drill.json"
  if do_restore "$RESTORE_FILE"; then
    STATUS="OK"; DETAIL="restore drill ok"
  else
    fail "restore drill failed"
    exit 1
  fi
  write_status
  exit 0
fi

log INFO "=== jomform-backup start (host $HOSTNAME_S) ==="

if ! do_dump; then
  fail "pg_dump failed"
  alert_email "[JomForm] Backup FAILED on $HOSTNAME_S — dump" \
    "The nightly Postgres dump did not complete at $STAMP on $HOSTNAME_S.
No new backup exists in Google Drive. Investigate: tail -n 100 $LOG_FILE"
  exit 1
fi

if ! do_media; then
  fail "media archive failed"
  alert_email "[JomForm] Backup FAILED on $HOSTNAME_S — media" \
    "The Postgres dump succeeded but the MinIO media archive failed at $STAMP.
Restore the media volume by hand before trusting a restore: tail -n 100 $LOG_FILE"
  exit 1
fi

if [ "$LOCAL_ONLY" = "1" ]; then
  STATUS="OK"; DETAIL="local-only run (no upload)"
  log INFO "local-only: skipping upload"
  write_status
  exit 0
fi

# ---- upload
if ! REMOTE_ERR="$(require_remote)"; then
  fail "rclone remote unavailable: $REMOTE_ERR"
  alert_email "[JomForm] Backup FAILED on $HOSTNAME_S — Google Drive remote unavailable" \
    "The dump was written locally ($STAGE_DIR) but could NOT be uploaded at $STAMP.

$REMOTE_ERR

A local dump on the same host as the database is not an offsite backup.
If the token has expired, re-run: rclone config reconnect ${REMOTE}:"
  exit 1
fi

DAILY_DIR="$REMOTE:$REMOTE_ROOT/daily"
MONTHLY_DIR="$REMOTE:$REMOTE_ROOT/monthly"

uploads_ok=1
FAIL_DETAIL=""
for f in "$STAGE_DIR/$DAILY_NAME.dump.gz" "$STAGE_DIR/$DAILY_NAME.dump.gz.sha256" \
         "$STAGE_DIR/$DAILY_NAME-media.tar.gz"; do
  [ -f "$f" ] || continue
  log INFO "upload $(basename "$f") -> $DAILY_DIR/"
  if ! UPLOAD_ERR="$(upload_file "$f" "$DAILY_DIR" 2>&1)"; then
    # Carry the provider's own message into the detail line: it names the real
    # cause (quota, 403, expired token) instead of leaving "upload failed".
    FAIL_DETAIL="$(printf '%s' "$UPLOAD_ERR" | tr '\n' ' ' | cut -c1-300)"
    log ERROR "rclone copy failed for $f: $FAIL_DETAIL"
    uploads_ok=0
    continue
  fi
  if ! VERIFY_ERR="$(verify_remote "$f" "$DAILY_DIR/$(basename "$f")" 2>&1)"; then
    FAIL_DETAIL="$VERIFY_ERR"
    log ERROR "post-upload verification failed for $f: $VERIFY_ERR"
    uploads_ok=0
    continue
  fi
  UPLOADED="${UPLOADED}$(basename "$f") "
  log INFO "verified $DAILY_DIR/$(basename "$f")"
done

# Monthly copy on the 1st. rclone server-side copy, so no re-upload.
if [ "$DAY_OF_MONTH" = "01" ]; then
  log INFO "first of month: promoting to $MONTHLY_DIR/$MONTHLY_NAME.dump.gz"
  monthly_ok=1
  if ! PROMOTE_ERR="$(rclone --config "$RCLONE_CONFIG" copyto \
      "$DAILY_DIR/$DAILY_NAME.dump.gz" "$MONTHLY_DIR/$MONTHLY_NAME.dump.gz" 2>&1)"; then
    FAIL_DETAIL="$(printf '%s' "$PROMOTE_ERR" | tr '\n' ' ' | cut -c1-300)"
    log ERROR "monthly dump copy failed: $FAIL_DETAIL"
    monthly_ok=0
  fi
  # The sidecar is written HERE, not copied from the daily object. The daily
  # sidecar's content is "<digest>  /var/backups/jomform/jomform-daily-<date>.dump.gz"
  # because it was produced by `sha256sum "$out"`; copying that file verbatim
  # leaves the monthly artifact with a sidecar naming the DAILY path, so
  # `sha256sum -c jomform-monthly-<ym>.dump.gz.sha256` fails on a perfectly good
  # dump and the path stops existing once local retention prunes the daily file.
  # Same digest (the copy is server-side, so the bytes are identical), monthly
  # name, bare basename so it resolves beside the object in any directory the
  # reader downloaded the pair into.
  if [ "$monthly_ok" = "1" ]; then
    local_sidecar="$STAGE_DIR/$DAILY_NAME.dump.gz.sha256"
    digest="$(cut -d' ' -f1 "$local_sidecar" 2>/dev/null || true)"
    if [ -z "$digest" ]; then
      FAIL_DETAIL="monthly sidecar: no digest in $local_sidecar"
      log ERROR "$FAIL_DETAIL"
      monthly_ok=0
    elif ! printf '%s  %s\n' "$digest" "$MONTHLY_NAME.dump.gz" \
        | rclone --config "$RCLONE_CONFIG" rcat "$MONTHLY_DIR/$MONTHLY_NAME.dump.gz.sha256" 2>/dev/null; then
      FAIL_DETAIL="monthly sidecar write failed: $MONTHLY_DIR/$MONTHLY_NAME.dump.gz.sha256"
      log ERROR "$FAIL_DETAIL"
      monthly_ok=0
    fi
  fi
  if [ "$monthly_ok" = "1" ]; then
    log INFO "monthly copy ok (sha256 $digest, sidecar names $MONTHLY_NAME.dump.gz)"
  else
    uploads_ok=0
  fi
fi

if [ "$uploads_ok" != "1" ]; then
  fail "upload or verification failed${FAIL_DETAIL:+: $FAIL_DETAIL}"
  alert_email "[JomForm] Backup FAILED on $HOSTNAME_S — upload" \
    "pg_dump succeeded locally but the Google Drive upload/verification failed at $STAMP.
Local dump: $STAGE_DIR/$DAILY_NAME.dump.gz ($PG_BYTES bytes)
Last run: $STATE_DIR/last-run.json
Log: tail -n 200 $LOG_FILE"
  exit 1
fi

# ---- retention
prune_remote "$DAILY_DIR/" "$KEEP_DAILY" '^jomform-daily-'
prune_remote "$MONTHLY_DIR/" "$KEEP_MONTHLY" '^jomform-monthly-'
prune_local

STATUS="OK"
DETAIL="dump ${PG_BYTES}B, media ${MEDIA_BYTES}B uploaded to $REMOTE:$REMOTE_ROOT/daily"
write_status
log INFO "=== jomform-backup OK: $DETAIL ==="
exit 0
