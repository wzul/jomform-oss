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
# A WRITE must carry --s3-no-check-bucket. The R2 token is scoped to ONE bucket,
# so ListBuckets is denied; rclone cannot confirm the bucket exists, concludes it
# is missing and calls CreateBucket, which the token is not allowed to do, so
# EVERY write fails closed. The stub models that REAL failure (10/10 against the
# live bucket, rclone 1.75.2 and 1.60.1) so the suite bites when the flag is
# dropped. Read commands do not need it and are not gated.
NO_CHECK=0
for a in "\${args[@]:-}"; do [ "\$a" = "--s3-no-check-bucket" ] && NO_CHECK=1; done
cmd="\${1:-}"; shift || true
case "\$cmd" in
  listremotes)
    if [ "\$MODE" = "no-remote" ]; then echo "other:"; else echo "r2jomform:"; fi ;;
  copy)
    # Record every write invocation so a case can assert the flag was carried.
    printf 'copy %s\\n' "\$*" >>"$CASE_DIR/rclone-writes.log"
    if [ "\$NO_CHECK" = "0" ]; then
      echo 'Failed to copy: failed to prepare upload: operation error S3: CreateBucket, https response error StatusCode: 403, api error AccessDenied: Access Denied' >&2
      exit 1
    fi
    if [ "\$MODE" = "copy-fail" ]; then
      # Real rclone errors carry double quotes; an unescaped copy of this text
      # into the status JSON would make the file unparseable.
      echo 'Failed to copy: googleapi: Error 403: The user has exceeded their Drive storage quota for "0AIx\"" files.' >&2
      exit 1
    fi
    # Model a REAL upload: the object lands in the remote dir, so the
    # post-upload verification below has something truthful to compare against.
    src="\${1:-}"; dest="\${2:-}"
    # STUB_SILENT_NAME names one object whose write silently drops the stream:
    # the command consumes it, exits 0 and stores NOTHING. That is the trap this
    # case exists for — an exit status that describes the command, not the
    # object. Only the named object is affected, so a case can still be driven
    # through the monthly promotion with healthy daily uploads.
    if [ -n "\${STUB_SILENT_NAME:-}" ] && [ "\$(basename "\$src")" = "\$STUB_SILENT_NAME" ]; then
      exit 0
    fi
    # STUB_FAIL_NAME is the opposite shape: the command itself reports a failure
    # for one object, so the script's own upload-error path is exercised while
    # the daily objects still upload normally.
    if [ -n "\${STUB_FAIL_NAME:-}" ] && [ "\$(basename "\$src")" = "\$STUB_FAIL_NAME" ]; then
      echo 'Failed to copy: googleapi: Error 403: rateLimitExceeded' >&2
      exit 1
    fi
    mkdir -p "$CASE_DIR/remote"
    cp "\$src" "$CASE_DIR/remote/\$(basename "\$src")"
    : >"\$dest" 2>/dev/null || true
    exit 0 ;;
  copyto)
    # A server-side copy reads the source from the REMOTE and writes the
    # destination on the REMOTE — neither path is a local file. The modelled
    # remote is $CASE_DIR/remote, addressed by basename.
    printf 'copyto %s\\n' "\$*" >>"$CASE_DIR/rclone-writes.log"
    if [ "\$NO_CHECK" = "0" ]; then
      echo 'Failed to copy: failed to prepare upload: operation error S3: CreateBucket, https response error StatusCode: 403, api error AccessDenied: Access Denied' >&2
      exit 1
    fi
    src="\${1:-}"; dest="\${2:-}"
    mkdir -p "$CASE_DIR/remote"
    # STUB_COPYTO_SILENT_NAME names one DESTINATION whose copy silently stores
    # nothing: the command exits 0 describing itself, not the object it was
    # asked to store. Scoped to one name so the daily uploads still land and the
    # run reaches the promotion.
    if [ -n "\${STUB_COPYTO_SILENT_NAME:-}" ] && [ "\$(basename "\$dest")" = "\$STUB_COPYTO_SILENT_NAME" ]; then
      exit 0
    fi
    # STUB_COPYTO_FAIL_NAME is the OTHER failure shape on this path: the copy
    # command itself reports an error for one destination and exits non-zero
    # (the provider's text on stderr, as real rclone prints it) while every other
    # object still lands — so a case can drive the promotion's own copy-failure
    # branch with healthy daily uploads ahead of it.
    if [ -n "\${STUB_COPYTO_FAIL_NAME:-}" ] && [ "\$(basename "\$dest")" = "\$STUB_COPYTO_FAIL_NAME" ]; then
      echo 'Failed to copy: googleapi: Error 403: The user has exceeded their Drive storage quota for "0AIx\\" files.' >&2
      exit 1
    fi
    if [ -f "$CASE_DIR/remote/\$(basename "\$src")" ]; then
      # STUB_COPYTO_CORRUPT_NAME stores DIFFERENT bytes under the right name:
      # the truncated server-side copy the sibling-gap probe reproduced, where
      # the name says jomform-monthly-<ym>.dump.gz and the bytes are a prefix of
      # it. Nothing in the script compares the promoted object with what the copy
      # was given except the digest re-read, so this is the shape that needs it.
      if [ -n "\${STUB_COPYTO_CORRUPT_NAME:-}" ] && [ "\$(basename "\$dest")" = "\$STUB_COPYTO_CORRUPT_NAME" ]; then
        head -c 16 "$CASE_DIR/remote/\$(basename "\$src")" >"$CASE_DIR/remote/\$(basename "\$dest")"
        exit 0
      fi
      cp "$CASE_DIR/remote/\$(basename "\$src")" "$CASE_DIR/remote/\$(basename "\$dest")"
    else
      echo "object not found: \$src" >&2
      exit 1
    fi
    exit 0 ;;
  # cat <remote-path> — re-read an object, the shape the script uses to verify a
  # write it produced itself. Mirrors real rclone: the stored bytes on stdout,
  # exit 3 "directory not found" when the object is not there.
  # STUB_CAT_CORRUPT_NAME names ONE object whose re-read returns bytes that
  # differ from what was sent (same shape, wrong digest). Scoped by name: the
  # script re-reads more than one generated object now, and a knob that
  # corrupted all of them would let one case failure stand in for another.
  cat)
    path="\${1:-}"
    base="\$(basename "\$path")"
    if [ -f "$CASE_DIR/remote/\$base" ]; then
      if [ -n "\${STUB_CAT_CORRUPT_NAME:-}" ] && [ "\$base" = "\$STUB_CAT_CORRUPT_NAME" ]; then
        printf 'deadbeefdeadbeefdeadbeefdeadbeef  %s\\n' "\$base"
      else
        cat "$CASE_DIR/remote/\$base"
      fi
      exit 0
    fi
    echo "directory not found" >&2
    exit 3 ;;
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
    # Report what is on the MODELLED REMOTE, like a real listing would. Reading
    # the local stage dir here instead is only correct for objects whose source
    # path happens to be the stage dir; it reports an object as missing when the
    # upload's source was a temp file (the monthly sidecar).
    base="\$(basename "\$path")"
    if [ -f "$CASE_DIR/remote/\$base" ]; then
      printf '[{"Name":"%s","Size":%s}]\\n' "\$base" "\$(stat -c %s "$CASE_DIR/remote/\$base")"
    else
      echo "[]"
    fi ;;
  # lsf <remote-path> — the modelled remote is ONE flat directory of basenames
  # ($CASE_DIR/remote), whatever tier path is passed; prune_remote applies its
  # own regex filter. Listing the real set is what lets a retention case assert
  # what was kept, instead of prune silently seeing an empty directory.
  lsf)
    if [ -d "$CASE_DIR/remote" ]; then
      ls -1 "$CASE_DIR/remote" 2>/dev/null || true
    fi
    exit 0 ;;
  # deletefile <remote-path> — remove the modelled object.
  deletefile)
    rm -f "$CASE_DIR/remote/\$(basename "\${1:-}")" 2>/dev/null || true
    exit 0 ;;
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
#
# Note what the seam does and does NOT give this helper. The seam only decides
# whether a run is classified as a hand-run; it moves no path. Containment here
# comes from these assignments alone — in particular JFBACKUP_STATE_DIR, which is
# where the status file goes and which the refusal path (fail -> write_status)
# also writes. Dropping any one of them would let the case reach the paths
# installed on this host, so the last case in this file asserts that this helper
# (and the README's hand-run recipe) still name every one of them.
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
      JFBACKUP_REMOTE="r2jomform" \
      JFBACKUP_REMOTE_ROOT="jfbackup-test" \
      RCLONE_CONFIG="$CASE_DIR/rclone.conf" \
      "${extra[@]}" \
      bash "$script" "$@" >>"$RUN_LOG" 2>&1
  echo "exit=$?"
}
teardown_case() { rm -rf "$CASE_DIR"; }

touch_rclone_conf() { echo '[r2jomform]' >"$CASE_DIR/rclone.conf"; }

# install_system_path_without_rclone <mirror-dir>
# <mirror-dir> becomes a PATH DIRECTORY carrying the six directories the script
# pins in _SYS_PATH, with `rclone` left out — so a run whose pinned path IS the
# mirror is a run on a host where rclone was never installed.
#
# Why not a PATH prefix: JFBACKUP_PATH_PREFIX only PREPENDS to _SYS_PATH, so
# /usr/bin stays on the run's path. A prefix entry that shadows rclone is not
# possible either — bash skips any PATH candidate that is not an executable
# regular file, so a directory, a non-executable file or a broken symlink named
# `rclone` all resolve through to the real binary (measured on a host that has
# /usr/bin/rclone). The six pinned directories are therefore mirrored here, and
# rclone is dropped from the mirror.
#
# `cp -as` makes SYMLINKS, so nothing is copied and the unlink below can only
# ever remove the mirror's own entry, never the real binary.
install_system_path_without_rclone() { # <mirror-dir>
  local dest="$1" d
  mkdir -p "$dest"
  for d in /usr/local/sbin /usr/local/bin /usr/sbin /usr/bin /sbin /bin; do
    [ -d "$d" ] || continue
    # -f: a name the six directories share (bin -> usr/bin, sbin -> usr/sbin) is
    # overwritten rather than reporting an error.
    cp -asf "$d/." "$dest/" 2>/dev/null || true
  done
  unlink "$dest/rclone" 2>/dev/null || true
}

# assert_path_without_rclone <label> <mirror-dir> [<extra-dir>...]
# Asserts that a run whose PATH is `<extra-dir>:<mirror-dir>` cannot reach
# rclone — that IS the premise of the case — and that the same PATH is still a
# WORKING system path. The canaries are part of the assertion rather than a
# nicety: a mirror that failed to build would make rclone unresolvable for the
# wrong reason, and the case would pass having measured nothing. Returns
# non-zero on failure so the caller can report it loudly instead of running the
# case against a path that cannot express the premise.
assert_path_without_rclone() { # label mirror [extra-dir...]
  local label="$1"; shift
  local run_path="" d
  for d in "$@"; do run_path="${run_path:+$run_path:}$d"; done
  local probe
  probe="$(env PATH="$run_path" bash -c '
    command -v rclone >/dev/null 2>&1 && { echo "rclone still resolves, to $(command -v rclone)"; exit 1; }
    for c in python3 docker gzip; do
      command -v "$c" >/dev/null 2>&1 || { echo "$c does not resolve - the fixture PATH is not a working system path"; exit 1; }
    done
    echo ok' 2>&1)"
  if [ "$probe" = "ok" ]; then
    ok "$label"
    return 0
  fi
  bad "$label: $probe"
  return 1
}

# ---------------------------------------------------------- tier helpers
# Names the script will use for THIS day, spelled exactly as the script does.
# JFBACKUP_DAY_OF_MONTH / JFBACKUP_DAY_OF_WEEK only drive the promotion GATES,
# not the file names, so a run on the 20th still produces jomform-daily-<20th>;
# the digest comparisons below are against that object.
daily_name() { date -u +%Y-%m-%d; }
weekly_name() { date -u +%G-W%V; }
monthly_name() { date -u +%Y-%m; }
tier_name() { # monthly|weekly -> the tier's date-stamped name suffix
  case "$1" in
    monthly) monthly_name ;;
    weekly)  weekly_name ;;
    *) echo "unknown tier: $1" >&2; return 1 ;;
  esac
}

# check_isolated <label> <expect> [tier]
# The honest form of the card's acceptance: take the tier pair the promotion
# produced, copy it into a directory that holds NOTHING else, and run a real
# `sha256sum -c`.
#
# The staged daily objects are deleted first, modelling local retention having
# pruned them (the dump script prunes local staging at the end of every run, so
# by the time a human reaches for the tier copy the daily file is long gone).
# Without that step the check is not truthful: an absolute staging path in the
# sidecar still RESOLVES on this host, so `sha256sum -c` would pass on the very
# sidecar this card is about. That is exactly what a first attempt at this test
# did — it went green against the defect.
check_isolated() {
  local label="$1" expect="$2" tier="${3:-monthly}"
  local tn vdir got
  tn="jomform-$tier-$(tier_name "$tier").dump.gz"
  vdir="$(mktemp -d)"
  cp "$CASE_DIR/remote/$tn" "$CASE_DIR/remote/$tn.sha256" "$vdir/" 2>/dev/null
  if [ ! -f "$vdir/$tn.sha256" ]; then
    bad "$label: no $tier sidecar at $CASE_DIR/remote/$tn.sha256 (nothing was written)"
    rm -rf "$vdir"; return
  fi
  rm -f "$CASE_DIR/stage/jomform-daily-"*.dump.gz "$CASE_DIR/stage/jomform-daily-"*.dump.gz.sha256
  if (cd "$vdir" && sha256sum -c "$tn.sha256" >/dev/null 2>&1); then
    got=pass
  else
    got=fail
  fi
  if [ "$got" = "$expect" ]; then
    ok "$label: sha256sum -c $got on the isolated $tier pair, as required"
  else
    bad "$label: sha256sum -c $got but must be $expect (sidecar: $(cat "$vdir/$tn.sha256"))"
  fi
  rm -rf "$vdir"
}

# verify_tier_pair <label> <tier>
# The acceptance the card states, for a run that is meant to be CORRECT. Content
# assertions first, then the media archive asserted present (a tier restore that
# cannot render customer media is the defect this half closes), then the real
# checksum run.
verify_tier_pair() {
  local label="$1" tier="$2"
  local vdir dn tn want got
  dn="jomform-daily-$(daily_name).dump.gz"
  tn="jomform-$tier-$(tier_name "$tier").dump.gz"
  want="$(cut -d' ' -f1 "$CASE_DIR/remote/$dn.sha256" 2>/dev/null)"
  if [ ! -f "$CASE_DIR/remote/jomform-$tier-$(tier_name "$tier")-media.tar.gz" ]; then
    bad "$label: no $tier media archive on the remote — a tier restore could not render customer media"
  fi
  vdir="$(mktemp -d)"
  cp "$CASE_DIR/remote/$tn" "$CASE_DIR/remote/$tn.sha256" "$vdir/" 2>/dev/null
  if [ ! -f "$vdir/$tn.sha256" ]; then
    bad "$label: no $tier sidecar at $CASE_DIR/remote/$tn.sha256 (nothing was written)"
    rm -rf "$vdir"; return
  fi
  # An absolute staging path is the defect itself: it only ever resolved on the
  # host that produced the dump, and stops resolving the moment local retention
  # prunes the daily file.
  if grep -qE '(^|[[:space:]])/' "$vdir/$tn.sha256"; then
    bad "$label: $tier sidecar carries an absolute path: $(cat "$vdir/$tn.sha256")"
  fi
  if grep -qF "$dn" "$vdir/$tn.sha256"; then
    bad "$label: $tier sidecar still names the DAILY object ($dn)"
  fi
  if ! grep -qF "  $tn" "$vdir/$tn.sha256"; then
    bad "$label: $tier sidecar does not name $tn: $(cat "$vdir/$tn.sha256")"
  fi
  # The copy is server-side, so the tier digest must equal the daily one.
  got="$(cut -d' ' -f1 "$vdir/$tn.sha256")"
  assert_eq "$label: $tier digest equals the daily digest" "$want" "$got"
  rm -rf "$vdir"
  check_isolated "$label" pass "$tier"
}

# verify_monthly_pair <label> — the monthly tier's spelling of the above.
verify_monthly_pair() { verify_tier_pair "$1" monthly; }

# daily_files — how many objects matching a tier's prefix are on the modelled
# remote. prune_remote keeps min(total, keep) FILES, and one backup SET is 3
# files, so a "kept N sets" assertion is this count over 3.
tier_file_count() {
  ls -1 "$CASE_DIR/remote" 2>/dev/null | grep -c "^jomform-$1-" || true
}

# seed_sets <tier> <count>
# Put <count> whole backup SETS (dump + sidecar + media, 3 files each) on the
# modelled remote, dated 1999 so every one of them sorts OLDER than the run's own
# daily set. Retention is then asserted on the kept SETS, not merely on "some
# files remain".
seed_sets() {
  local t="$1" n="$2" i d
  mkdir -p "$CASE_DIR/remote"
  for i in $(seq 1 "$n"); do
    # zero-padded so a string `sort -r` orders the SETS by day (without it,
    # `-9` sorts above `-30` and the "newest set" assertion would measure the
    # harness's own naming, not retention).
    d="$(printf '%02d' "$i")"
    printf 'old' >"$CASE_DIR/remote/jomform-$t-1999-01-$d.dump.gz"
    printf 'old' >"$CASE_DIR/remote/jomform-$t-1999-01-$d.dump.gz.sha256"
    printf 'old' >"$CASE_DIR/remote/jomform-$t-1999-01-$d-media.tar.gz"
  done
}

# promote_and_verify — run a full backup with the 1st-of-month branch driven on,
# then verify the pair. This is the case that would have caught the defect.
promote_and_verify() {
  local label="$1"; shift
  # DAY_OF_WEEK=3 keeps this a purely monthly run even on a Sunday, so the
  # monthly assertions cannot be satisfied by the weekly tier's objects.
  rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok --)"
  assert_eq "$label: exit code" "exit=0" "$rc"
  assert_contains "$label: promotion ran" "first of month: promoting to" "$RUN_LOG"
  assert_contains "$label: promotion reported ok" "monthly copy ok" "$RUN_LOG"
  assert_file   "$label: monthly object on the remote" \
    "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz"
  verify_monthly_pair "$label"
}

# sabotage_promoted_dump_verification — put the pre-fix behaviour back into a COPY
# of the script inside the case dir: the promotion is reported ok without the
# promoted dump ever having been read. The swap is exact and literal, so a future
# edit that moves or rewrites that branch makes this report NOTFOUND — and the
# case calling it fails loudly — instead of quietly mutating nothing.
# Prints APPLIED, or NOTFOUND count=N, for the caller to assert on.
sabotage_promoted_dump_verification() {
  cp "$SCRIPT" "$CASE_DIR/bite-target.sh"
  python3 - "$CASE_DIR/bite-target.sh" "$CASE_DIR/bite-applied.txt" <<'PY'
import sys
path, marker = sys.argv[1], sys.argv[2]
src = open(path).read()
# The digest re-read this card added, as shipped...
fixed = '''    elif ! PROMOTE_VERIFY_ERR="$(verify_remote_digest "$dump_dest" "$digest" 2>&1)"; then
      PROMOTE_DETAIL="$tier dump verification failed: $PROMOTE_VERIFY_ERR"
      log ERROR "$PROMOTE_DETAIL"
      tier_ok=0
    else
      log INFO "verified $dump_dest"
'''
# ...replaced by trusting the copyto's exit status, which is the pre-fix shape:
# the copy command's exit status says nothing about the destination object, and
# the promotion is reported without anything having read the promoted dump.
# (The `else log INFO` line is kept so the branch still parses and still LIES
# that the object was verified.)
broken = '''    else
      log INFO "verified $dump_dest"
'''
if src.count(fixed) != 1:
    open(marker, 'w').write('NOTFOUND count=%d' % src.count(fixed))
    sys.exit(3)
open(path, 'w').write(src.replace(fixed, broken))
open(marker, 'w').write('APPLIED')
PY
  cat "$CASE_DIR/bite-applied.txt" 2>/dev/null
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
assert_contains "upload verified" "verified r2jomform:jfbackup-test/daily/" "$RUN_LOG"
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
# An S3 remote has NO OAuth token to reconnect: the old Drive remedy told an
# operator to run `rclone config reconnect <remote>:`, which does not exist for
# R2. The hint must name the real remedy instead — a NEW R2 API token written
# into rclone.conf — and must not send them to a command that cannot work.
assert_contains "the R2 credential remedy is named" "issue a NEW R2 API token" "$RUN_LOG"
assert_contains "the credentials file is named" "/root/.config/rclone/rclone.conf" "$RUN_LOG"
assert_not_contains "no OAuth reconnect hint (an S3 remote has none)" "rclone config reconnect" "$RUN_LOG"
teardown_case

echo "case: rclone missing entirely -> the dump still happens and the run FAILS loudly"
setup_case; touch_rclone_conf
# The premise is "rclone is not installed at all", and a PATH PREFIX cannot
# express it: JFBACKUP_PATH_PREFIX only PREPENDS to the script's pinned
# _SYS_PATH, so /usr/bin stays on the path and on a host where /usr/bin/rclone
# exists the run finds the REAL rclone and this case goes red for a reason that
# is not about the script. Measured on such a host: a directory, a
# non-executable file, and a broken symlink named `rclone` in the prefix ALL
# still resolve through to the real binary — bash skips any PATH entry that is
# not an executable regular file. So the fixture removes rclone from the pinned
# path itself: a symlink mirror of the six directories _SYS_PATH names, minus
# rclone, pinned into a COPY of the script (the tracked file is never touched).
install_system_path_without_rclone "$CASE_DIR/norclone"
# Only the stubs the run needs for everything that is NOT rclone. The mirror
# already carries the real docker/logger; these keep the case off /usr/bin for
# the two programs it stubs.
mkdir -p "$CASE_DIR/norclone-stubs"
cp "$STUB_BIN/docker" "$STUB_BIN/logger" "$CASE_DIR/norclone-stubs/"
chmod +x "$CASE_DIR/norclone-stubs"/*
# The mirror is only evidence if the shell agrees rclone is unreachable on it.
# If the fixture cannot express the premise on this host the case is reported as
# a FAILURE with the reason — a silent skip would be a green suite proving
# nothing, which is the shape this repo's gate rules forbid.
if assert_path_without_rclone "fixture: rclone is NOT reachable on the run's PATH" \
    "$CASE_DIR/norclone" "$CASE_DIR/norclone-stubs"; then
  cp "$SCRIPT" "$CASE_DIR/backup-no-rclone.sh"
  python3 - "$CASE_DIR/backup-no-rclone.sh" "$CASE_DIR/norclone" <<'PY'
import sys
path, mirror = sys.argv[1], sys.argv[2]
src = open(path).read()
old = '_SYS_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"'
if src.count(old) != 1:
    sys.exit("anchor found %d times, expected 1" % src.count(old))
open(path, "w").write(src.replace(old, '_SYS_PATH="%s"' % mirror, 1))
PY
  if [ "$(grep -c '^_SYS_PATH="'"$CASE_DIR"'/norclone"$' "$CASE_DIR/backup-no-rclone.sh")" -eq 1 ]; then
    ok "the fixture pinned the copy at the rclone-less path"
  else
    bad "the fixture did not pin the copy at the rclone-less path — the case would run the host's path"
  fi
  if bash -n "$CASE_DIR/backup-no-rclone.sh" 2>/dev/null; then
    ok "the pinned copy is a valid script"
  else
    bad "the pinned copy does not parse"
  fi
  rc="$(run_backup_script "$CASE_DIR/backup-no-rclone.sh" \
    JFBACKUP_PATH_PREFIX="$CASE_DIR/norclone-stubs" --)"
  assert_eq "exit code" "exit=1" "$rc"
  assert_contains "log names rclone" "rclone is not installed" "$RUN_LOG"
  assert_file "dump was still written" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
fi
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
    JFBACKUP_REMOTE="r2jomform" \
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
    JFBACKUP_REMOTE=r2jomform \
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

echo "case: the monthly sidecar write exits 0 and stores NOTHING -> the run FAILS"
# The trap this case exists for. `rclone rcat`'s exit status describes the
# COMMAND, not the object: an rclone that consumes the stream, exits 0 and writes
# nothing used to leave a healthy-looking run whose monthly sidecar was never in
# Drive — a monthly dump with no verifiable checksum, which is the artifact the
# sidecar exists to prevent. The stub drops exactly one named object's stream.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_SILENT_NAME="jomform-monthly-$(monthly_name).dump.gz.sha256" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the sidecar verification" "monthly sidecar verification failed" "$RUN_LOG"
assert_contains "log says the object is missing remotely" "remote object missing or unreadable after upload" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "status file FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
# And the whole point: nothing on the remote claims to be a verified sidecar.
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz.sha256" ]; then
  bad "a sidecar object exists although the write dropped the stream"
else
  ok "no sidecar object on the remote, and the run said so"
fi
teardown_case

echo "case: the sidecar lands but its stored CONTENT differs -> the run FAILS"
# Size and md5 alone are not the acceptance: STUB_CAT_CORRUPT_NAME leaves the
# stored object's size and hash correct for the modelled backend (so
# verify_remote passes) and makes the re-read return different bytes, which
# only the content comparison can catch. A same-length object with the wrong
# digest would be an unusable sidecar. It names the SIDECAR, so the dump the
# promotion re-reads is not the object this case corrupts.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_CAT_CORRUPT_NAME="jomform-monthly-$(monthly_name).dump.gz.sha256" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the content mismatch" "remote content mismatch" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
teardown_case

echo "case: the sidecar upload command fails -> the run FAILS (uploads_ok=0), not a silent 0"
# The other half of the rule: a promotion whose sidecar never left the host must
# not be reported as a healthy run with an unverifiable monthly copy.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_FAIL_NAME="jomform-monthly-$(monthly_name).dump.gz.sha256" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the failed sidecar upload" "monthly sidecar upload failed" "$RUN_LOG"
assert_contains "log carries the provider message" "rateLimitExceeded" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
teardown_case

echo "case: the promoted monthly dump write exits 0 and stores NOTHING -> the run FAILS"
# The same write-through shape the sidecar had, on the promotion's OTHER write:
# `rclone copyto`'s exit status describes the COMMAND, not the destination
# object, so a server-side copy that exits 0 having stored nothing used to leave
# a run reporting "monthly copy ok" with no monthly dump in Drive. The stub drops
# exactly one named destination's copy, so the daily uploads still land and the
# run reaches the promotion.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_SILENT_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the dump verification" "monthly dump verification failed" "$RUN_LOG"
assert_contains "log says the object is missing remotely" "remote object missing or unreadable after promotion" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "status file FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
# The whole point: nothing on the remote claims a monthly copy exists. No dump —
# and no sidecar either, because the sidecar is written only once the promoted
# dump has been read back, so a sidecar can never vouch for an absent dump.
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz" ]; then
  bad "a monthly dump object exists although the copy stored nothing"
else
  ok "no monthly dump object on the remote, and the run said so"
fi
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz.sha256" ]; then
  bad "a monthly sidecar exists for a dump that is not in Drive"
else
  ok "no sidecar written for the dump that never landed"
fi
teardown_case

echo "case: the promoted dump lands as DIFFERENT bytes under the right name -> the run FAILS"
# The shape that made the monthly sidecar worse than absent: the pair LOOKS
# complete (both names are on the remote) and the run used to report OK, while
# the reader's own `sha256sum -c` on the pair fails. The stub stores a truncated
# copy under the monthly name — a different size, so a size comparison would
# catch it too, but what the script actually compares here is the DIGEST of the
# promoted object against the digest the copy was given.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_CORRUPT_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the dump verification" "monthly dump verification failed" "$RUN_LOG"
assert_contains "log names the digest mismatch" "remote digest mismatch" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
assert_contains "status file FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
# And the dump that failed verification must not be dressed with a sidecar: a
# sidecar beside it is exactly what makes the pair look complete and verified.
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz.sha256" ]; then
  bad "a sidecar was written beside a dump that failed verification"
else
  ok "no sidecar written beside the dump that failed verification"
fi
teardown_case

echo "case: the promotion's OWN copyto FAILS -> the run FAILS, and no sidecar is written"
# The other failure mode of the promotion's write. The two cases above drive a
# `copyto` that exits 0 and lies about the object; this one drives a `copyto` that
# reports a failure of its own (the provider's error text, non-zero exit) — the
# branch at jomform-backup.sh:715-720 that nothing exercised. The daily trio is
# uploaded normally by the stub, so the run reaches the promotion with a healthy
# upload phase: the failure must be the PROMOTION's, reported as such, and it must
# not leave a sidecar vouching for a dump that was never copied.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_FAIL_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log names the failed promotion" "monthly dump copy failed" "$RUN_LOG"
assert_contains "log carries the provider's own error text" \
  "Failed to copy: googleapi: Error 403: The user has exceeded their Drive storage quota for" "$RUN_LOG"
assert_contains "log marks FAILED" "jomform-backup FAILED" "$RUN_LOG"
# The failure has to survive into the run's VERDICT, not only into its log. The
# promoted-dump re-read below this branch overwrites FAIL_DETAIL when this branch
# leaves the month marked ok, so without it the record a human reads names the
# re-read instead of the copy that actually broke — and that is the shape the
# bite case below requires the suite to catch.
assert_contains "the FAILED record carries the copy failure" \
  "jomform-backup FAILED: upload or verification failed: Failed to copy: googleapi: Error 403" "$RUN_LOG"
assert_contains "status detail carries the provider's own text" \
  "upload or verification failed: Failed to copy: googleapi: Error 403" "$CASE_DIR/state/last-run.json"
assert_contains "status file FAILED" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
# The provider's message carries double quotes; an unescaped copy of it into the
# status JSON would leave the file unparseable, which is the one thing a status
# reader cannot recover from.
assert_json_valid "status file stays parseable with the provider's quotes" "$CASE_DIR/state/last-run.json"
assert_file "last-failure.json written" "$CASE_DIR/state/last-failure.json"
# The failure is the promotion's, not the run's: the daily objects the earlier
# phase verified must still be on the remote, or the case would be measuring a
# broken stub instead of the failure it claims.
dn="jomform-daily-$(daily_name).dump.gz"
for f in "$dn" "$dn.sha256" "jomform-daily-$(daily_name)-media.tar.gz"; do
  assert_file "daily object still uploaded: $f" "$CASE_DIR/remote/$f"
done
# And the whole point: nothing on the remote claims a monthly copy exists. No
# dump — and no sidecar either, because the sidecar is written only once the
# promoted dump has been read back, so a failed copy can never be dressed as a
# verified pair.
assert_no_file "no monthly dump on the remote after a failed copy" \
  "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz"
assert_no_file "no monthly sidecar written for a copy that failed" \
  "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz.sha256"
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

echo "case: weekly promotion runs on Sunday (UTC) and promotes dump + sidecar + media"
# The weekly tier is new. It is built the SAME way as the monthly one (a shared
# promote_tier) and is held to the same rule: nothing is reported until the
# promoted object has been re-read — never a promotion that was not re-read.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_WEEK=7 JFBACKUP_DAY_OF_MONTH=20 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "weekly promotion ran" "sunday (UTC): promoting to" "$RUN_LOG"
assert_contains "weekly promotion reported ok" "weekly copy ok" "$RUN_LOG"
wn="jomform-weekly-$(weekly_name).dump.gz"
for f in "$wn" "$wn.sha256" "jomform-weekly-$(weekly_name)-media.tar.gz"; do
  assert_file "weekly object on the remote: $f" "$CASE_DIR/remote/$f"
done
verify_tier_pair "weekly promotion" weekly
teardown_case

echo "case: no weekly promotion on a day that is not Sunday (the gate holds)"
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_WEEK=3 JFBACKUP_DAY_OF_MONTH=20 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
if grep -q "sunday (UTC): promoting to" "$RUN_LOG"; then
  bad "promoted weekly on a Wednesday — the Sunday gate is gone"
else
  ok "no weekly promotion on a non-Sunday"
fi
assert_no_file "no weekly dump object written on a non-Sunday" \
  "$CASE_DIR/remote/jomform-weekly-$(weekly_name).dump.gz"
teardown_case

echo "case: with NO DAY_OF_WEEK override the script promotes iff today really is Sunday"
# Same discipline as the monthly seam: the expectation is computed from the real
# `date -u +%u`, so a hard-coded weekly gate is caught whatever day this runs on.
setup_case; touch_rclone_conf
rc="$(run_backup STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
if [ "$(date -u +%u)" = "7" ]; then
  assert_contains "it IS Sunday: real weekly promotion ran" "sunday (UTC): promoting to" "$RUN_LOG"
  verify_tier_pair "unset weekly override" weekly
else
  if grep -q "sunday (UTC): promoting to" "$RUN_LOG"; then
    bad "promoted weekly with NO override on $(date -u +%u) — the seam bypasses the real date"
  else
    ok "no weekly promotion with no override on $(date -u +%u) (real date respected)"
  fi
fi
teardown_case

echo "case: the monthly promotion also promotes the media archive (a monthly restore must render media)"
# Behaviour change: a monthly SET now carries the -media.tar.gz too. Without it
# a monthly restore rebuilds the rows but renders no customer media.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
mm="jomform-monthly-$(monthly_name)-media.tar.gz"
assert_file "monthly media archive on the remote" "$CASE_DIR/remote/$mm"
if cmp -s "$CASE_DIR/remote/$mm" "$CASE_DIR/remote/jomform-daily-$(daily_name)-media.tar.gz"; then
  ok "the promoted monthly media archive is byte-identical to the daily one"
else
  bad "the promoted monthly media archive differs from the daily archive"
fi
teardown_case

echo "case: every rclone WRITE carries --s3-no-check-bucket (the bucket-scoped token cannot CreateBucket)"
# The R2 token is scoped to ONE bucket, so ListBuckets is denied; without this
# flag rclone cannot confirm the bucket exists, calls CreateBucket (403) and
# EVERY write fails — the backup fails every night. This run exercises both write
# shapes (the daily `copy` and the promotion's server-side `copyto`), and the
# stub logs each invocation, so the assertion covers all of them.
setup_case; touch_rclone_conf
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=7 STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=0" "$rc"
assert_file "the write log recorded invocations" "$CASE_DIR/rclone-writes.log"
writes="$(wc -l <"$CASE_DIR/rclone-writes.log" 2>/dev/null | tr -d ' ')"
flagged="$(grep -c -- '--s3-no-check-bucket' "$CASE_DIR/rclone-writes.log" 2>/dev/null || true)"
if [ "${writes:-0}" -ge 6 ] && [ "${writes:-0}" = "${flagged:-0}" ]; then
  ok "all ${writes} write invocations carried --s3-no-check-bucket"
else
  bad "a write invocation did not carry --s3-no-check-bucket (writes=$writes flagged=$flagged): $(grep -v -- '--s3-no-check-bucket' "$CASE_DIR/rclone-writes.log" 2>/dev/null | tr '\n' ' ')"
fi
teardown_case

echo "case: an rclone write WITHOUT --s3-no-check-bucket FAILS exactly as the real bucket does"
# The shape a future edit silently re-introduces: drop the flag and the nightly
# backup fails EVERY night. The stub models the real backend (CreateBucket ->
# AccessDenied), so a copy of the script with the flag removed must go red.
setup_case; touch_rclone_conf
cp "$SCRIPT" "$CASE_DIR/noflag.sh"
python3 - "$CASE_DIR/noflag.sh" "$CASE_DIR/bite-applied.txt" <<'PY'
import sys
path, marker = sys.argv[1], sys.argv[2]
src = open(path).read()
old = ' --s3-no-check-bucket'
n = src.count(old)
assert n >= 1, "anchor not found: --s3-no-check-bucket"
open(path, "w").write(src.replace(old, ""))
open(marker, "w").write("APPLIED removed=%d" % n)
PY
if grep -q '^APPLIED removed=' "$CASE_DIR/bite-applied.txt" 2>/dev/null; then
  ok "the flag was dropped from every write ($(cat "$CASE_DIR/bite-applied.txt"))"
else
  bad "the flag-removal sabotage did not apply: $(cat "$CASE_DIR/bite-applied.txt" 2>/dev/null)"
fi
if bash -n "$CASE_DIR/noflag.sh"; then ok "the flagless copy still parses"; else bad "the flagless copy does not parse"; fi
rc="$(run_backup_script "$CASE_DIR/noflag.sh" STUB_RCLONE_MODE=ok --)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "the write fails with the backend's own error" "AccessDenied" "$RUN_LOG"
assert_contains "the CreateBucket attempt is named" "CreateBucket" "$RUN_LOG"
assert_contains "the run fails loudly" "jomform-backup FAILED" "$RUN_LOG"
teardown_case

echo "case: retention keeps 7 daily / 4 weekly / 6 monthly SETS and prunes the rest"
# prune_remote keeps the newest N FILES; one backup SET is 3 files, so the tier
# budgets are 21 / 12 / 18. Whole sets are seeded OLDER than the run's own daily
# set (year 1999), so the newest sets are the ones kept.
setup_case; touch_rclone_conf
seed_sets daily 30
seed_sets weekly 18
seed_sets monthly 24
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=20 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok LC_ALL=C --)"
assert_eq "exit code" "exit=0" "$rc"
assert_eq "daily files kept (7 sets x 3)" "21" "$(tier_file_count daily)"
assert_eq "weekly files kept (4 sets x 3)" "12" "$(tier_file_count weekly)"
assert_eq "monthly files kept (6 sets x 3)" "18" "$(tier_file_count monthly)"
assert_eq "daily SETS kept (files / 3)" "7" "$(( $(tier_file_count daily) / 3 ))"
assert_file "the newest seeded daily set survives" "$CASE_DIR/remote/jomform-daily-1999-01-30.dump.gz"
assert_no_file "the oldest seeded daily set was pruned" "$CASE_DIR/remote/jomform-daily-1999-01-01.dump.gz"
teardown_case

echo "case: JFBACKUP_KEEP_DAILY / _WEEKLY / _MONTHLY overrides are honoured"
setup_case; touch_rclone_conf
seed_sets daily 30
seed_sets weekly 18
seed_sets monthly 24
rc="$(run_backup JFBACKUP_DAY_OF_MONTH=20 JFBACKUP_DAY_OF_WEEK=3 \
  JFBACKUP_KEEP_DAILY=3 JFBACKUP_KEEP_WEEKLY=6 JFBACKUP_KEEP_MONTHLY=9 \
  STUB_RCLONE_MODE=ok LC_ALL=C --)"
assert_eq "exit code" "exit=0" "$rc"
assert_eq "daily override keeps 3 files (1 set)" "3" "$(tier_file_count daily)"
assert_eq "weekly override keeps 6 files (2 sets)" "6" "$(tier_file_count weekly)"
assert_eq "monthly override keeps 9 files (3 sets)" "9" "$(tier_file_count monthly)"
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
fixed = '''    printf '%s  %s\\n' "$digest" "$tier_name.dump.gz" >"$sidecar_tmp"'''
# ...replaced by the pre-fix behaviour: the DAILY sidecar verbatim, so the
# monthly object's sidecar names the daily staging path (and its absolute path
# stops resolving as soon as local retention prunes the daily file).
broken = '''    cp "$STAGE_DIR/$DAILY_NAME.dump.gz.sha256" "$sidecar_tmp"'''
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
  JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok --)"
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

echo "case: the promoted-dump case BITES on a copy that stored NOTHING — remove the re-read and it goes GREEN"
# The promotion's other write now has a case that can fail; this proves the
# assertion it lives on bites instead of decorating. `sabotage_promoted_dump_verification`
# puts the pre-fix behaviour back into a COPY of the script (the tracked file is
# never written to), so the run has nothing to read back and reports whatever the
# copy command's exit status said.
setup_case; touch_rclone_conf
sab_status="$(sabotage_promoted_dump_verification)"
assert_eq "sabotage applied to the copy (NOTFOUND means this case proves nothing)" "APPLIED" "$sab_status"
if bash -n "$CASE_DIR/bite-target.sh"; then ok "the sabotaged copy still parses"; else bad "the sabotaged copy does not parse"; fi
sab_rc="$(run_backup_script "$CASE_DIR/bite-target.sh" JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_SILENT_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
# The pre-fix shape reports success and never notices: that is what made it silent.
assert_eq "RED as required: without the re-read the silent-drop copy reports SUCCESS" "exit=0" "$sab_rc"
assert_contains "RED as required: and it called the month healthy" "monthly copy ok" "$RUN_LOG"
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz" ]; then
  bad "the sabotaged run left a monthly dump on the remote — the stub is wrong, not the proof"
else
  ok "RED as required: no monthly dump in Drive, yet the run called the month ok"
fi
teardown_case

echo "case: the promoted-dump case BITES on a truncated copy too — GREEN run, and the reader's checksum fails"
# The second shape, same sabotage: the pair the pre-fix script leaves behind
# LOOKS complete — the truncated dump plus a sidecar naming it — and the run
# reported ok, which is precisely the pair the README tells a reader to reject
# with `sha256sum -c`. That check is run for real here. The suite's own case for
# this shape asserts the fixed script FAILS it (exit 1, no sidecar).
setup_case; touch_rclone_conf
sab_status="$(sabotage_promoted_dump_verification)"
assert_eq "sabotage applied to the copy (NOTFOUND means this case proves nothing)" "APPLIED" "$sab_status"
if bash -n "$CASE_DIR/bite-target.sh"; then ok "the sabotaged copy still parses"; else bad "the sabotaged copy does not parse"; fi
sab_rc="$(run_backup_script "$CASE_DIR/bite-target.sh" JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_CORRUPT_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
assert_eq "RED as required: without the re-read the truncated copy reports SUCCESS" "exit=0" "$sab_rc"
assert_contains "RED as required: and it called the month healthy" "monthly copy ok" "$RUN_LOG"
check_isolated "pre-fix truncated promotion" fail
teardown_case

echo "case: the copyto-FAILURE case BITES — drop the branch's own failure mark and it goes RED"
# The case above is only worth its assertions if they can fail. This puts the
# pre-fix shape back into a COPY of the script inside the case dir (the tracked
# file is never written to): the copyto-failure branch keeps its log line but
# stops marking the month as failed.
#
# Measured honestly, because the mutation does NOT leave the run green: the
# promoted-dump re-read (added for the sibling defect) catches the absent object
# as well, so the sabotaged run is still exit=1 and still writes last-failure.json.
# What the branch's own failure mark decides is therefore not the exit code but
# the VERDICT: with it gone, FAIL_DETAIL is overwritten by the re-read's message
# and the record a human reads names the re-read instead of the copy that broke.
# That is the assertion this bite is driven at — and why the new case asserts the
# provider's text in the FAILED record and in the status detail, not only in the
# branch's own log line (which survives the mutation untouched).
setup_case; touch_rclone_conf
cp "$SCRIPT" "$CASE_DIR/bite-target.sh"
python3 - "$CASE_DIR/bite-target.sh" "$CASE_DIR/bite-applied.txt" <<'PY'
import sys
path, marker = sys.argv[1], sys.argv[2]
src = open(path).read()
# The branch as shipped: it logs the failure AND marks the tier failed...
fixed = '''    log ERROR "$tier dump copy failed: $PROMOTE_DETAIL"
    tier_ok=0
'''
# ...replaced by the log line alone, so the failure of the copy command no
# longer reaches the run's own verdict.
broken = '''    log ERROR "$tier dump copy failed: $PROMOTE_DETAIL"
'''
if src.count(fixed) != 1:
    open(marker, 'w').write('NOTFOUND count=%d' % src.count(fixed))
    sys.exit(3)
open(path, 'w').write(src.replace(fixed, broken))
open(marker, 'w').write('APPLIED')
PY
sab_status="$(cat "$CASE_DIR/bite-applied.txt" 2>/dev/null)"
assert_eq "sabotage applied to the copy (NOTFOUND means this case proves nothing)" "APPLIED" "$sab_status"
if bash -n "$CASE_DIR/bite-target.sh"; then ok "the sabotaged copy still parses"; else bad "the sabotaged copy does not parse"; fi
sab_rc="$(run_backup_script "$CASE_DIR/bite-target.sh" JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_COPYTO_FAIL_NAME="jomform-monthly-$(monthly_name).dump.gz" --)"
# Still a failed run: the promoted-dump re-read catches the copy that stored
# nothing. Asserted, not assumed — and it is why the bite below is aimed at the
# verdict's DETAIL rather than at the exit code, which this mutation does not move.
assert_eq "with the failure mark gone the run still fails (the re-read catches it)" "exit=1" "$sab_rc"
# The branch's own log line survives — it is the MARK that was removed, not the
# message — so an assertion on that line alone would not bite; this is the case's
# control, stated rather than left implicit.
assert_contains "the branch's own log line survives the mutation (control)" \
  "monthly dump copy failed" "$RUN_LOG"
# RED as required: the copy failure no longer reaches the run's verdict. Both the
# FAILED record and the machine-readable detail now name the re-read instead, so
# the new case's `FAILED record carries the copy failure` /
# `status detail carries the provider's own text` assertions are the load-bearing
# ones, and this mutation turns them red.
assert_not_contains "RED as required: the FAILED record no longer carries the copy failure" \
  "jomform-backup FAILED: upload or verification failed: Failed to copy" "$RUN_LOG"
assert_not_contains "RED as required: the status detail no longer carries the provider's text" \
  "upload or verification failed: Failed to copy" "$CASE_DIR/state/last-run.json"
assert_contains "the sabotaged run reports the re-read's failure in the verdict instead" \
  "jomform-backup FAILED: upload or verification failed: monthly dump verification failed" "$RUN_LOG"
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
    "JFBACKUP_REMOTE=r2jomform"
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
#
# `-u RCLONE_CONFIG` is load-bearing: an installed host has no RCLONE_CONFIG in
# its environment, and this host EXPORTS one. Left inherited, that value differs
# from the copy's rewritten RCLONE_CONFIG_DEFAULT and the run is misclassified as
# a hand-run for a knob nobody moved — a red that measures the host, not the guard.
run_installed() {
  local extra=() args=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do extra+=("$1"); shift; done
  [ "$#" -gt 0 ] && shift
  args=("$@")
  env -u RCLONE_CONFIG ${extra[@]+"${extra[@]}"} \
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
assert_contains "names the production folder it would have fallen back to" "jomform-backup" "$RUN_LOG"
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
rc="$(run_installed JFBACKUP_ENV_FILE="$CASE_DIR/env" JFBACKUP_REMOTE=r2jomform JFBACKUP_REMOTE_ROOT=scratch-root -- --local-only)"
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
assert_contains "names the production remote reached" "r2jomform:jomform-backup" "$RUN_LOG"
assert_contains "syslog carries the unsafe override too" "unsafe override allowed" "$CASE_DIR/alerts.log"
assert_file "dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
teardown_case

echo "case: a destination-only run with JFBACKUP_ALLOW_UNSAFE=0 is REFUSED (the hatch needs the value 1)"
# The hatch is opened by the VALUE, not by the variable being SET. A `[ -n ... ]`
# test read `JFBACKUP_ALLOW_UNSAFE=0` — a caller writing the value that means
# "no" — exactly like `=1`: the run reached the installed credential file and
# mailed the installed super-admin inbox, while the audit WARN quoted a `=1`
# nobody had set. Same fixture and same knob as the destination-only case above,
# only the value differs, so the pair isolates the value test.
setup_case; touch_rclone_conf
install_backup_copy "$CASE_DIR/stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"
before="$(ls -A "$CASE_DIR/stage" | sort | tr '\n' ' ')"
rc="$(run_installed JFBACKUP_REMOTE_ROOT=scratch-root JFBACKUP_ALLOW_UNSAFE=0)"
assert_eq "exit code" "exit=1" "$rc"
assert_contains "log says it refused" "refusing a partial-override run" "$RUN_LOG"
assert_contains "the refusal names the missing JFBACKUP_ENV_FILE" "JFBACKUP_ENV_FILE" "$RUN_LOG"
assert_not_contains "the hatch was not logged as used" "escape hatch was used" "$RUN_LOG"
assert_no_file "no dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_no_file "no media archive was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d)-media.tar.gz"
assert_same_listing "$before" "$CASE_DIR/stage"
assert_no_file "no lock file was taken" "$CASE_DIR/lock"
assert_not_contains "no alert email was attempted" "[JomForm]" "$RUN_LOG"
assert_contains "status file records the refusal" '"status": "FAILED"' "$CASE_DIR/state/last-run.json"
teardown_case

echo "case: no other value opens the hatch either (false / empty / an arbitrary string)"
# `1` is the documented value; anything else is not a request for a deliberate
# real run and must leave the seam refusing. The empty value is the one an
# `export JFBACKUP_ALLOW_UNSAFE=` produces. The labels keep the vocabulary the
# bite harness classifies on (no dump was taken / no alert email), so restoring
# the value-agnostic `[ -n ... ]` test reads as a seam failure there too.
for v in false "" 2; do
  setup_case; touch_rclone_conf
  install_backup_copy "$CASE_DIR/stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"
  rc="$(run_installed JFBACKUP_REMOTE_ROOT=scratch-root "JFBACKUP_ALLOW_UNSAFE=$v")"
  assert_eq "exit code with JFBACKUP_ALLOW_UNSAFE='$v'" "exit=1" "$rc"
  assert_no_file "no dump was taken with JFBACKUP_ALLOW_UNSAFE='$v'" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
  assert_not_contains "no alert email was attempted with JFBACKUP_ALLOW_UNSAFE='$v'" "[JomForm]" "$RUN_LOG"
  teardown_case
done

echo "case: the SAME destination-only run with JFBACKUP_ALLOW_UNSAFE=1 still proceeds"
# The positive direction of the value test, in the same fixture: a caller who
# sets the documented value is asking for a deliberate real run and must not be
# refused — otherwise the hatch is a ban rather than an escape hatch.
setup_case; touch_rclone_conf
install_backup_copy "$CASE_DIR/stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"
rc="$(run_installed JFBACKUP_REMOTE_ROOT=scratch-root JFBACKUP_ALLOW_UNSAFE=1 -- --local-only)"
assert_eq "exit code" "exit=0" "$rc"
assert_contains "the hatch is open for the documented value, and names it" "JFBACKUP_ALLOW_UNSAFE=1" "$RUN_LOG"
assert_file "dump was taken" "$CASE_DIR/stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"
assert_not_contains "the seam did not refuse" "refusing a partial-override run" "$RUN_LOG"
teardown_case

echo "case: the installed cron shape is NOT refused (no false positive on the nightly job)"
# The four location knobs at their installed values with ENV_FILE/REMOTE_ROOT
# unset is exactly what cron runs, and it must stay allowed — a seam that
# refuses the nightly job is worse than none. Checked against the documented
# cron entry rather than by running it: executing the installed shape offline
# would write to /var/backups on this host, and the property under test is the
# classification, which is determined by what the cron line sets.
#
# Read the CRON ENTRY, not the first mention of the script: README line 8 is a
# prose sentence that names the same path, so `grep -F` alone matches it and
# the check below then reads prose instead of the cron entry and passes
# vacuously. Take the fenced ```cron block (that IS the documented entry, so a
# knob on a line-continuation is caught too); fall back to a cron-SHAPED line
# (five scheduling fields, then the command) if the fence is renamed or absent.
# Comment lines are ignored so a doc comment inside the fence cannot raise a
# false positive. Nothing found is a FAIL: with nothing to read, the nightly
# job's shape is unverified, which is not the same as "not refused".
cron_field='[0-9*][^[:space:]]*'
cron_re="^${cron_field}[[:space:]]+${cron_field}[[:space:]]+${cron_field}[[:space:]]+${cron_field}[[:space:]]+${cron_field}[[:space:]]+.*jomform-backup\\.sh([[:space:]]|\$)"
cron_block="$(awk '/^```cron[[:space:]]*$/{f=1;next} /^```/{f=0} f' "$SCRIPT_DIR/README.md" || true)"
if ! grep -qF 'jomform-backup.sh' <<<"$cron_block"; then
  cron_block="$(grep -E "$cron_re" "$SCRIPT_DIR/README.md" | sed -n '1p' || true)"
fi
cron_knob_line="$(grep -vE '^[[:space:]]*#' <<<"$cron_block" | grep -E 'JFBACKUP_|RCLONE_CONFIG=' | sed -n '1p' || true)"
if [ -z "$cron_block" ]; then
  bad "no cron entry found in README to check the installed shape against (the nightly job's shape is unverified)"
elif [ -n "$cron_knob_line" ]; then
  bad "the documented cron entry overrides a seam knob — the guard would refuse the nightly job: $cron_knob_line"
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
  if grep -qE "^  if \[ -z \"\\\$\\{${knob}:-\\}\" \]; then" "$SCRIPT"; then
    ok "seam tests that $knob was PROVIDED (not that its value differs)"
  else
    bad "seam does not test provision of $knob"
  fi
done
# The escape hatch must open on the VALUE `1`, not on the variable merely being
# set: `JFBACKUP_ALLOW_UNSAFE=0` is a caller writing the value that means NO, and
# a `[ -n ... ]` test read it as YES — the run then reached the installed
# credential file (JFBACKUP_ENV_FILE) and the installed inbox, while the WARN
# quoted a `=1` nobody had set. Asserted on the source as well as by the cases
# above, because the source is what a reader audits.
if grep -qF 'if [ "${JFBACKUP_ALLOW_UNSAFE:-}" = "1" ]; then' "$SCRIPT"; then
  ok "seam tests the hatch VALUE (only JFBACKUP_ALLOW_UNSAFE=1 opens it)"
else
  bad "seam does not test the hatch VALUE: JFBACKUP_ALLOW_UNSAFE=0 is treated as a deliberate real run that reaches JFBACKUP_ENV_FILE"
fi

echo "case: the SEAM IS NOT CONTAINMENT — an unnamed STATE_DIR lands in the INSTALLED default"
# This is the case behind the README's corrected wording. The seam answers only
# "cron job or hand-run"; it moves no path. Where a run's status file lands is
# decided by the knobs THAT RUN names, and any knob it omits resolves to the
# value installed on the host. Measured here on a copy of the script whose
# installed defaults point inside the case dir, so the "installed default" of a
# control pair is the case's own path and no host path is ever reachable.
#
# The pair is the whole point: the SAME run, one knob different. Named -> the
# record goes to the scratch dir and the installed default stays untouched;
# omitted -> the record goes to the installed default. A suite that could not
# tell those two apart would be exactly as misleading as the README sentence
# this case exists for.
setup_case; touch_rclone_conf
install_backup_copy "$CASE_DIR/inst-stage" "$CASE_DIR/state" "$RUN_LOG" "$CASE_DIR/lock" "$CASE_DIR/env" "$CASE_DIR/rclone.conf"

# leg 1 (named): every containment knob named, so the record must NOT appear at
# the installed default. Note this is also the README-recipe shape.
rc="$(run_installed \
  JFBACKUP_STAGE_DIR="$CASE_DIR/scratch-stage" \
  JFBACKUP_STATE_DIR="$CASE_DIR/scratch-state" \
  JFBACKUP_LOG_FILE="$RUN_LOG" \
  JFBACKUP_LOCK_FILE="$CASE_DIR/scratch-lock" \
  JFBACKUP_ENV_FILE="$CASE_DIR/env" \
  JFBACKUP_REMOTE=r2jomform \
  JFBACKUP_REMOTE_ROOT=jfbackup-test \
  -- --local-only)"
assert_eq "named STATE_DIR: exit code" "exit=0" "$rc"
assert_file "named STATE_DIR: the record is in the named scratch dir" "$CASE_DIR/scratch-state/last-run.json"
assert_no_file "named STATE_DIR: the INSTALLED default was left alone" "$CASE_DIR/state/last-run.json"
assert_no_file "named STATE_DIR: nothing was staged at the installed stage default" \
  "$CASE_DIR/inst-stage/jomform-daily-$(date -u +%Y-%m-%d).dump.gz"

# leg 2 (omitted): the same run with JFBACKUP_STATE_DIR left out. It still names
# every other knob and still provides the three legs, so the seam is satisfied
# and the run proceeds — and the record lands at the INSTALLED default. That is
# the shape a reader of the bullet list (rather than of the recipe block) runs.
# Leg 1's scratch state dir is removed first, so the "did not go there" assertion
# below is about THIS run and not about leg 1's leftovers.
rm -rf "$CASE_DIR/scratch-state"
rc="$(run_installed \
  JFBACKUP_STAGE_DIR="$CASE_DIR/scratch-stage" \
  JFBACKUP_LOG_FILE="$RUN_LOG" \
  JFBACKUP_LOCK_FILE="$CASE_DIR/scratch-lock" \
  JFBACKUP_ENV_FILE="$CASE_DIR/env" \
  JFBACKUP_REMOTE=r2jomform \
  JFBACKUP_REMOTE_ROOT=jfbtest-scratch \
  -- --local-only)"
assert_eq "omitted STATE_DIR: exit code (the seam is satisfied, so it runs)" "exit=0" "$rc"
assert_not_contains "omitted STATE_DIR: it was not refused" "refusing a partial-override run" "$RUN_LOG"
assert_file "omitted STATE_DIR: the record landed at the INSTALLED default" "$CASE_DIR/state/last-run.json"
assert_contains "omitted STATE_DIR: and it is the run's own honest record" '"status": "OK"' "$CASE_DIR/state/last-run.json"
assert_no_file "omitted STATE_DIR: the scratch dir it did not name stayed empty" "$CASE_DIR/scratch-state/last-run.json"
teardown_case

echo "case: the documented recipe and the suite helper both name every containment knob"
# The README tells a reader that safety is the caller's by naming every knob, and
# this suite's own cases depend on the same discipline through run_backup_script.
# A helper (or recipe) that quietly stopped naming one of them would leave the
# suite writing into the very paths it is supposed to stay out of — the defect
# the README sentence used to assert was impossible. Asserted structurally AND
# behaviourally above; this is the structural half, so a future edit that drops a
# knob is caught in the same place the claim is made.
#
# The knob list is the knobs a run WRITES to (STAGE/STATE/LOG/LOCK — these are
# the paths a run leaves behind) plus the three the seam requires to be provided.
# RCLONE_CONFIG is deliberately absent: it is read-only, a --local-only run never
# touches it, and the recipe documents omitting it.
containment_knobs="JFBACKUP_STAGE_DIR JFBACKUP_STATE_DIR JFBACKUP_LOG_FILE JFBACKUP_LOCK_FILE JFBACKUP_ENV_FILE JFBACKUP_REMOTE JFBACKUP_REMOTE_ROOT"

helper_body="$(sed -n '/^run_backup_script() {$/,/^}$/p' "$SCRIPT_DIR/jomform-backup_test.sh")"
if [ -z "$helper_body" ]; then
  bad "could not extract run_backup_script from the suite — this check measures nothing"
else
  for knob in $containment_knobs; do
    if grep -qF "${knob}=" <<<"$helper_body"; then
      ok "the suite helper names $knob"
    else
      bad "the suite helper does not name $knob — cases would write to the path installed on this host"
    fi
  done
fi

# The recipe block is extracted from the README rather than grepped loosely, so a
# mention of a knob in prose does not satisfy it: only an assignment inside the
# block counts.
recipe="$(awk '
  /^A hand-run therefore looks like this:$/ { grab = 1; next }
  grab && /^```/ { fences++; if (fences == 2) exit; next }
  grab { print }
' "$SCRIPT_DIR/README.md")"
if [ -z "$recipe" ]; then
  bad "could not extract the hand-run recipe from README.md — this check measures nothing"
else
  for knob in $containment_knobs; do
    if grep -qF "${knob}=" <<<"$recipe"; then
      ok "the hand-run recipe names $knob"
    else
      bad "the hand-run recipe does not name $knob — a reader copying it reaches the installed path"
    fi
  done
fi
# And the false sentence itself must stay gone. It claimed the suite's safety was
# a property of the SCRIPT; it is a property of the caller naming every knob, and
# the wording that says otherwise is what sent two operators into production state.
assert_not_contains "the README no longer claims safety is a property of the script" \
  "safety is a property of the" "$SCRIPT_DIR/README.md"

echo "case: the sidecar-verification case BITES — remove the re-read and it goes RED"
# The assertion introduced for this defect bites: with the post-upload re-read of
# the sidecar removed, an upload that exits 0 and stores nothing must take the
# suite red instead of being reported as a healthy monthly copy.
#
# Same discipline as the case above: an exact literal swap on a COPY inside the
# case dir, and a sabotage that fails to apply is reported as NOTFOUND rather
# than passing silently — a bite case that never applied the mutation is a lie.
setup_case; touch_rclone_conf
cp "$SCRIPT" "$CASE_DIR/bite-target.sh"
python3 - "$CASE_DIR/bite-target.sh" "$CASE_DIR/bite-applied.txt" <<'PY'
import sys
path, marker = sys.argv[1], sys.argv[2]
src = open(path).read()
# The verification this card added, as shipped...
fixed = '''    elif ! SIDECAR_ERR="$(verify_remote "$sidecar_tmp" "$sidecar_dest" 2>&1)"; then
      PROMOTE_DETAIL="$tier sidecar verification failed: $SIDECAR_ERR"
      log ERROR "$PROMOTE_DETAIL"
      tier_ok=0
    elif ! SIDECAR_ERR="$(verify_remote_content "$sidecar_tmp" "$sidecar_dest" 2>&1)"; then
      PROMOTE_DETAIL="$tier sidecar verification failed: $SIDECAR_ERR"
      log ERROR "$PROMOTE_DETAIL"
      tier_ok=0
    else
      log INFO "verified $sidecar_dest"
    fi'''
# ...replaced by trusting the upload's exit status alone, which is exactly the
# shape the card reported: "monthly copy ok" for a sidecar that is not on the remote.
broken = '''    else
      log INFO "uploaded $sidecar_dest"
    fi'''
if src.count(fixed) != 1:
    open(marker, 'w').write('NOTFOUND count=%d' % src.count(fixed))
    sys.exit(3)
open(path, 'w').write(src.replace(fixed, broken))
open(marker, 'w').write('APPLIED')
PY
sab_status="$(cat "$CASE_DIR/bite-applied.txt" 2>/dev/null)"
assert_eq "sabotage applied to the copy (NOTFOUND means this case proves nothing)" "APPLIED" "$sab_status"
if bash -n "$CASE_DIR/bite-target.sh"; then ok "the sabotaged copy still parses"; else bad "the sabotaged copy does not parse"; fi
# Same silent-drop remote as the case above: the sidecar's stream is discarded
# while every other object lands, which is the failure the re-read exists for.
sab_rc="$(run_backup_script "$CASE_DIR/bite-target.sh" JFBACKUP_DAY_OF_MONTH=01 JFBACKUP_DAY_OF_WEEK=3 STUB_RCLONE_MODE=ok \
  STUB_SILENT_NAME="jomform-monthly-$(monthly_name).dump.gz.sha256" --)"
# The pre-fix shape reports success and never notices: that is what made it silent.
assert_eq "RED as required: without the re-read the run reports SUCCESS" "exit=0" "$sab_rc"
assert_contains "RED as required: and it claimed the sidecar was uploaded" \
  "uploaded r2jomform:jfbackup-test/monthly/" "$RUN_LOG"
if [ -e "$CASE_DIR/remote/jomform-monthly-$(monthly_name).dump.gz.sha256" ]; then
  bad "the sabotaged run left a sidecar on the remote — the stub is wrong, not the proof"
else
  ok "RED as required: no sidecar on the remote, yet the suite's own case would have called it ok"
fi
teardown_case

echo
printf '%d passed, %d failed\n' "$PASSES" "$FAILURES"
[ "$FAILURES" -eq 0 ]
