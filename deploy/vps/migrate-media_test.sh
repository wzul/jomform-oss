#!/usr/bin/env bash
# deploy/vps/migrate-media_test.sh — the offline harness for migrate-media.sh.
#
# WHAT IT IS DEFENDING. migrate-media.sh is the only thing standing between the
# production media bucket and a SeaweedFS store that was initialised EMPTY: the
# two stores do not share an on-disk format, so a "successful" run that in fact
# copied nothing looks exactly like a real one — the stack comes up healthy and
# every previously-uploaded image 404s. So the harness is mostly about the
# FAILURE directions:
#
#   1. it must not report success when the sides DISAGREE on object count or on
#      total bytes (the copy lost objects), or when rclone's per-object check
#      finds one missing;
#   2. it must not read a BROKEN source as an empty one (a failed list is a
#      refusal, not "0 objects, nothing to do");
#   3. it must not copy into a destination that already holds MORE than the
#      source (that is not this bucket, and the comparison afterwards would be
#      meaningless);
#   4. it must not put the credentials on a command line (`ps` is readable) and
#      must remove the 0600 config file however it exits;
#   5. PLAN (the default) must touch nothing — no service started, no copy;
#   6. the new service must be started INSIDE the live compose project (-p).
#      Without it compose derives the project from the compose file's directory,
#      creates a second network, and the copy silently aims at a store the api
#      will never use — measured on 2026-11-03: the same file run from a checkout
#      landed in project `vps` on `vps_internal`, not `jomform_internal`.
#
# `docker` and `rclone` are stubs on PATH: nothing talks to a real daemon or
# store. The live proof (a real MinIO store → a real SeaweedFS one) is in the
# card's evidence, not here: a harness that needs two live services is a gate
# that gets skipped.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/migrate-media.sh"
COMPOSE="$HERE/compose.prod.yml"
[ -x "$SCRIPT" ] || { echo "migrate-media_test: $SCRIPT is not executable" >&2; exit 1; }
[ -s "$COMPOSE" ] || { echo "migrate-media_test: missing $COMPOSE" >&2; exit 1; }

PASS=0
FAIL=0
ok()  { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; }
has()      { if grep -qF -- "$2" <<<"$1"; then ok "$3"; else bad "$3 (missing '$2')"; fi; }
hasnt()    { if grep -qF -- "$2" <<<"$1"; then bad "$3 (unexpectedly contains '$2')"; else ok "$3"; fi; }
check()    { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$3', want '$2')"; fi; }

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

# ── harness ──────────────────────────────────────────────────────────────────
# setup_case makes a per-case env file, a TMPDIR the script will use (so the
# config-cleanup assertion is about THIS run), and a stub PATH.
setup_case() {
  CASE_DIR="$(mktemp -d -p "$WORK")"
  STUB_BIN="$CASE_DIR/bin"
  mkdir -p "$STUB_BIN" "$CASE_DIR/tmp"
  CALL_LOG="$CASE_DIR/calls.log"
  : >"$CALL_LOG"

  cat >"$CASE_DIR/env" <<'EOF'
POSTGRES_PASSWORD=pgsecret
MINIO_ROOT_PASSWORD=oldminio-secret
S3_ACCESS_KEY=jomform
S3_SECRET_KEY=newstore-secret
EOF
  chmod 600 "$CASE_DIR/env"

  # rclone stub. Behaviour is driven by numeric knobs the cases pass through
  # `run`; every call is logged verbatim.
  cat >"$STUB_BIN/rclone" <<EOF
#!/usr/bin/env bash
set -u
# Split --config off the argv: it is logged separately (its MODE and whether it
# carried the credentials), and the logged argv is what rclone would have run —
# so an assertion about "--checksum" is about the real argument list.
args=(); cfg=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    --config) cfg="\${2:-}"; shift 2 ;;
    *) args+=("\$1"); shift ;;
  esac
done
if [ -n "\$cfg" ] && [ -f "\$cfg" ]; then
  printf 'config-mode %s\n' "\$(stat -c %a "\$cfg")" >>"$CALL_LOG"
  printf 'config-secrets %s\n' "\$(grep -c 'secret_access_key' "\$cfg")" >>"$CALL_LOG"
fi
printf 'rclone %s\n' "\${args[*]:-}" >>"$CALL_LOG"
cmd="\${args[0]:-}"
set -- \${args[@]+"\${args[@]:1}"}
case "\$cmd" in
  size)
    target="\${1:-}"
    if [ "\$target" = "from:\${BUCKET:-jomform-media}" ]; then
      [ "\${SRC_READ_RC:-0}" -eq 0 ] || { echo "ERROR : directory not found" >&2; exit 1; }
      printf '{"count":%s,"bytes":%s,"sizeless":0}\\n' "\${SRC_COUNT:-3}" "\${SRC_BYTES:-300}"
    else
      if grep -q '^rclone copy ' "$CALL_LOG"; then
        printf '{"count":%s,"bytes":%s,"sizeless":0}\\n' "\${DEST_AFTER_COUNT:-\${SRC_COUNT:-3}}" "\${DEST_AFTER_BYTES:-\${SRC_BYTES:-300}}"
      else
        printf '{"count":%s,"bytes":%s,"sizeless":0}\\n' "\${DEST_BEFORE_COUNT:-0}" "\${DEST_BEFORE_BYTES:-0}"
      fi
    fi ;;
  mkdir) exit "\${MKDIR_RC:-0}" ;;
  copy)  [ "\${COPY_RC:-0}" -eq 0 ] || { echo "copy failed" >&2; exit "\${COPY_RC}"; }
         exit 0 ;;
  check) if [ "\${CHECK_RC:-0}" -ne 0 ]; then echo "1 differences found" >&2; exit "\${CHECK_RC}"; fi
         exit 0 ;;
  *)     exit 0 ;;
esac
EOF

  # docker stub: answers `compose … ps` with a health, and hands an `rclone`
  # invocation to the rclone stub with the same arguments (the real script runs
  # rclone in a container).
  cat >"$STUB_BIN/docker" <<EOF
#!/usr/bin/env bash
set -u
printf 'docker %s\n' "\$*" >>"$CALL_LOG"
if [ "\${1:-}" = "compose" ]; then
  for a in "\$@"; do
    if [ "\$a" = "ps" ]; then echo "\${S3_HEALTH:-healthy}"; exit 0; fi
  done
  exit 0
fi
if [ "\${1:-}" = "run" ]; then
  shift
  volhost=""
  while [ \$# -gt 0 ]; do
    case "\$1" in
      --rm) shift ;;
      --network|-v|--entrypoint|-e) [ "\$1" = "-v" ] && volhost="\${2%%:*}"; shift 2 ;;
      *) break ;;
    esac
  done
  shift                     # the rclone image
  # The real container reads /cfg/…; on this host that is the mounted host dir.
  out=()
  for a in "\$@"; do
    case "\$a" in
      /cfg/*) out+=("\$volhost/\${a#/cfg/}") ;;
      *)      out+=("\$a") ;;
    esac
  done
  exec "$STUB_BIN/rclone" \${out[@]+"\${out[@]}"}
fi
exit 0
EOF
  chmod +x "$STUB_BIN/docker" "$STUB_BIN/rclone"
}

# run [VAR=VAL ...] -- [script args...]
# The VAR=VAL pairs go into the CHILD environment only (the stubs read them), so
# one case cannot leak a knob into the next.
run() {
  local -a vars=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do vars+=("$1"); shift; done
  [ $# -gt 0 ] && shift
  set +e
  OUT="$(PATH="$STUB_BIN:$PATH" TMPDIR="$CASE_DIR/tmp" \
    env ${vars[@]+"${vars[@]}"} \
    bash "$SCRIPT" --env-file "$CASE_DIR/env" --compose "$COMPOSE" \
      --rclone-image rclone/rclone:latest "$@" 2>&1)"
  RC=$?
  set +e
}

# ── case 1: PLAN is the default and touches nothing ──────────────────────────
echo "case: PLAN (the default) starts nothing and copies nothing"
setup_case
run
check "exit code" 0 "$RC"
has "$OUT" "PLAN — nothing has been copied" "says nothing was copied"
has "$OUT" "rclone copy" "names the copy it WOULD do"
has "$OUT" "docker compose -p jomform -f" "names the pinned project in the plan"
check "nothing at all ran (no docker, no rclone)" "" "$(cat "$CALL_LOG")"
check "the temp config dir was cleaned up" "" "$(ls -A "$CASE_DIR/tmp")"

# ── case 2: the happy path, and the hygiene assertions ───────────────────────
echo "case: --apply copies, verifies, and cleans up after itself"
setup_case
run -- --apply
check "exit code (OUT: $OUT)" 0 "$RC"
has "$OUT" "VERIFIED — 3 object(s) / 300 byte(s) on both sides" "prints the verified totals"
has "$(cat "$CALL_LOG")" "rclone copy from:jomform-media to:jomform-media --checksum" "copied with --checksum"
has "$(cat "$CALL_LOG")" "rclone check from:jomform-media to:jomform-media --size-only --one-way" "ran the per-object check"
has "$(cat "$CALL_LOG")" "docker compose" "started the s3 service from the new compose"
S3START="$(grep -F ' up -d s3' "$CALL_LOG" | tail -n 1)"
has "$S3START" "-p jomform" "the 'up -d s3' call is pinned to the live project (default -p jomform)"
has "$(cat "$CALL_LOG")" "--network jomform_internal" "ran rclone on the live project's network"
has "$(cat "$CALL_LOG")" "config-mode 600" "the rclone config was 0600 while in use"
has "$(cat "$CALL_LOG")" "config-secrets 2" "the config carried both credentials (they are not on the argv)"
hasnt "$(cat "$CALL_LOG")" "oldminio-secret" "the old secret never appears on a command line"
hasnt "$OUT" "oldminio-secret" "the old secret never appears in the output"
hasnt "$OUT" "newstore-secret" "the new secret never appears in the output"
check "the temp config dir was cleaned up on success" "" "$(ls -A "$CASE_DIR/tmp")"
has "$OUT" "keep the jomform_miniodata volume" "tells the operator not to delete the old volume"

# ── case 2b: the project pinning follows --project, and the network with it ──
echo "case: --project moves the container AND the rclone network together"
setup_case
run -- --apply --project jftest
check "exit code (OUT: $OUT)" 0 "$RC"
has "$(cat "$CALL_LOG")" "docker compose -p jftest" "started the s3 service in project jftest"
S3START="$(grep -F ' up -d s3' "$CALL_LOG" | tail -n 1)"
has "$S3START" "-p jftest" "the 'up -d s3' call follows --project"
has "$(cat "$CALL_LOG")" "--network jftest_internal" "derived the network from the project"
hasnt "$(cat "$CALL_LOG")" "--network jomform_internal" "did not keep the default network"

# ── case 3: BITE — a copy that lost objects must not report success ──────────
echo "case: BITE — destination count differs, so the run FAILS"
setup_case
run DEST_AFTER_COUNT=2 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "reported success after losing an object"
has "$OUT" "object COUNT differs" "names the count mismatch"
hasnt "$OUT" "VERIFIED" "did not claim VERIFIED"

setup_case
run DEST_AFTER_BYTES=299 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "reported success with a byte total that differs"
has "$OUT" "BYTE total differs" "names the byte mismatch"

# ── case 4: BITE — rclone's per-object check is load-bearing ─────────────────
echo "case: BITE — a missing object caught by rclone check fails the run"
setup_case
run CHECK_RC=1 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "reported success after rclone check found a difference"
has "$OUT" "rclone check found objects missing" "names the per-object failure"

# ── case 5: BITE — a failed source read is not an empty source ──────────────
echo "case: BITE — an unreadable source store is refused, never read as empty"
setup_case
run SRC_READ_RC=1 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "treated a failed source read as an empty bucket"
has "$OUT" "cannot read from:jomform-media" "names the failed read"
hasnt "$OUT" "VERIFIED" "did not claim VERIFIED"

echo "case: an empty source is refused unless the operator says it is expected"
setup_case
run SRC_COUNT=0 SRC_BYTES=0 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "accepted a 0-object source without --allow-empty"
has "$OUT" "--allow-empty" "names the flag that would allow it"
setup_case
run SRC_COUNT=0 SRC_BYTES=0 -- --apply --allow-empty
check "with --allow-empty it proceeds" 0 "$RC"
has "$OUT" "VERIFIED" "verifies the (empty) copy"

# ── case 6: BITE — a destination that is not this bucket ────────────────────
echo "case: BITE — a destination holding MORE objects than the source is refused"
setup_case
run DEST_BEFORE_COUNT=9 DEST_BEFORE_BYTES=900 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "copied into a destination that cannot be this bucket"
has "$OUT" "more than the source" "explains why it refused"
hasnt "$(cat "$CALL_LOG")" "rclone copy" "did not copy anything"

# ── case 7: credentials ─────────────────────────────────────────────────────
echo "case: a missing credential is refused and NAMED"
setup_case
grep -v '^MINIO_ROOT_PASSWORD=' "$CASE_DIR/env" >"$CASE_DIR/env.nominio"; mv "$CASE_DIR/env.nominio" "$CASE_DIR/env"
chmod 600 "$CASE_DIR/env"
run -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "accepted an env file with no old-store secret"
has "$OUT" "MINIO_ROOT_PASSWORD" "names the missing old-store secret"

setup_case
grep -v '^S3_ACCESS_KEY=' "$CASE_DIR/env" >"$CASE_DIR/env.nokey"; mv "$CASE_DIR/env.nokey" "$CASE_DIR/env"
chmod 600 "$CASE_DIR/env"
run -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "accepted an env file with no S3 access key"
has "$OUT" "S3_ACCESS_KEY" "names the missing new-store key"

echo "case: a missing env file is refused, not guessed"
setup_case
set +e
OUT="$(PATH="$STUB_BIN:$PATH" TMPDIR="$CASE_DIR/tmp" bash "$SCRIPT" --env-file "$CASE_DIR/nope.env" --compose "$COMPOSE" 2>&1)"
RC=$?
set +e
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "ran without an env file"
has "$OUT" "cannot read the env file" "names the missing env file"

# ── case 8: a compose/service failure is fatal, not a warning ───────────────
echo "case: BITE — an s3 service that never gets healthy stops the run before any copy"
setup_case
run S3_HEALTH=starting MIGRATE_MEDIA_HEALTH_TRIES=1 -- --apply
[ "$RC" -ne 0 ] && ok "exit code non-zero (exit=$RC)" || bad "copied while the new store was not serving"
has "$OUT" "never reported healthy" "names the unhealthy service"
hasnt "$(cat "$CALL_LOG")" "rclone copy" "did not copy anything"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
