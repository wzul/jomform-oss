#!/usr/bin/env bash
#
# Bite proof for the failure paths in jomform-backup.sh: the fail-closed test
# seam, and the twelve legacy failure paths the README names as proven to bite.
#
# A guard whose test still passes with the guard removed measures nothing. This
# sabotages ONE part of the seam at a time in a throwaway copy of the directory,
# runs the committed suite against it, and requires the suite to go RED each
# time — and that the red is a SEAM assertion, not some unrelated failure.
#
# It never touches the checkout: it copies this directory to a temp dir and
# mutates only the copy, so an uncommitted change here cannot be destroyed.
#
# Run:  bash deploy/backup/jomform-backup_bite.sh
# Exit: 0 only if EVERY sabotage was caught.
set -uo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DID_NOT_BITE=0
CAUGHT=0
WORK=""

reset_work() {
  [ -n "$WORK" ] && rm -rf "$WORK"
  WORK="$(mktemp -d /tmp/jfbackup_bite.XXXXXX)"
  # README.md too: the suite checks the documented cron entry against the seam,
  # so a copy without it would fail for a reason unrelated to the sabotage.
  cp "$SRC/jomform-backup.sh" "$SRC/jomform-backup_test.sh" "$SRC/README.md" "$WORK/"
}

# bite <label> <mutation-command...>
# The mutation runs with $MUTATE_TARGET = the throwaway script path.
bite() {
  local label="$1"; shift
  echo "=============================================================="
  echo "SABOTAGE: $label"
  reset_work
  local T="$WORK/jomform-backup.sh"
  MUTATE_TARGET="$T" "$@"
  if cmp -s "$SRC/jomform-backup.sh" "$T"; then
    echo "  ABORT: the mutation changed nothing — the sabotage did not land"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  # A mutation that leaves the script unparseable measures nothing either: every
  # case would fail for the same crash, which is not evidence about the seam.
  if ! bash -n "$T" 2>/dev/null; then
    echo "  ABORT: the mutation left the script unparseable (bash -n failed) — not a valid sabotage"
    bash -n "$T" 2>&1 | head -3 | sed 's/^/    /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  local out rc summary seam_fails
  out="$(bash "$WORK/jomform-backup_test.sh" 2>&1)"
  rc=$?
  summary="$(printf '%s\n' "$out" | tail -1)"
  # Only a FAIL on an assertion the seam cases own counts as a bite: a red suite
  # for an unrelated reason would otherwise be read as proof.
  seam_fails="$(printf '%s\n' "$out" | grep -cE '^\s+FAIL .*(refusing|JFBACKUP_ENV_FILE|JFBACKUP_REMOTE|no dump was taken|no media archive|listing changed|no alert email|no ALERT|the drill never started|no lock file|seam does not watch|seam tests that|destination)' || true)"
  if [ $rc -ne 0 ] && [ "${seam_fails:-0}" -gt 0 ]; then
    echo "  RESULT: RED ($seam_fails seam assertion(s) failed) — $summary"
    printf '%s\n' "$out" | grep -E '^\s+FAIL' | head -5 | sed 's/^/    /'
    CAUGHT=$((CAUGHT + 1))
  else
    echo "  RESULT: *** GREEN — THE GUARD DID NOT BITE *** (rc=$rc, $summary)"
    echo "    the suite passed with this sabotage in place, so its seam cases"
    echo "    prove nothing about it. First FAILs seen (none were seam assertions):"
    printf '%s\n' "$out" | grep -E '^\s+FAIL' | head -5 | sed 's/^/      /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
  fi
  reset_work
}

# --- the mutations -----------------------------------------------------------
# Each is a python3 one-liner over MUTATE_TARGET: exact-substring replacement, so
# it either lands or reports "did not land" instead of silently matching nothing
# (the failure mode that makes a bite harness lie).

echo "### baseline — the committed suite must be GREEN here"
reset_work
BASE_OUT="$(bash "$WORK/jomform-backup_test.sh" 2>&1)"
BASE_RC=$?
printf '%s\n' "$BASE_OUT" | tail -1 | sed 's/^/  /'
if [ $BASE_RC -ne 0 ]; then
  echo "ABORT: baseline is not green — a bite proof cannot be read on a red suite."
  rm -rf "$WORK"
  exit 1
fi

# 1. The seam call removed entirely: the guard is inert.
bite "enforce_test_seam never called (the guard is inert)" \
  python3 -c 'import os,sys
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="\nenforce_test_seam\n\nmkdir -p"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"\n: # SABOTAGE: guard disabled\n\nmkdir -p",1))'

# 2. The ENV_FILE leg removed: the credential half that actually mailed Wan.
#    Everything else (REMOTE, REMOTE_ROOT, the four location knobs) still works,
#    so a partial override with ENV_FILE unset must now be ALLOWED through.
bite "seam stops testing JFBACKUP_ENV_FILE (the credential half)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="  if [ -z \"${JFBACKUP_ENV_FILE:-}\" ]; then"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"  if false; then # SABOTAGE: ENV_FILE leg removed",1))'

# 3. The REMOTE_ROOT leg removed: the production Drive half.
bite "seam stops testing JFBACKUP_REMOTE_ROOT (the production Drive half)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="  if [ -z \"${JFBACKUP_REMOTE_ROOT:-}\" ]; then"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"  if false; then # SABOTAGE: REMOTE_ROOT leg removed",1))'

# 4. The refusal moved BELOW the mkdir+lock: the exit code is still 1, so only
#    the ORDERING assertions (no lock taken, no dump, unchanged stage listing)
#    can catch this. This is the "an exit 1 after the dump is not fail-closed"
#    shape. Inserted just above the dump so the script stays valid — a mutation
#    that breaks parsing would be rejected by the parse check in bite().
bite "seam moved down to after mkdir+lock (refusal too late)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="\nenforce_test_seam\n\nmkdir -p \"$STAGE_DIR\""
assert old in s, "anchor not found"
s=s.replace(old,"\nmkdir -p \"$STAGE_DIR\"",1)
anchor="log INFO \"=== jomform-backup start (host $HOSTNAME_S) ===\""
assert anchor in s, "second anchor not found"
s=s.replace(anchor, "enforce_test_seam\n"+anchor, 1)
open(p,"w").write(s)'

# 5. The refusal ALSO emails the super-admins: still refused, still exit 1, so
#    only the "no alert email" assertions can catch the leak.
bite "refusal also emails the super-admins (the leak the guard exists for)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old=chr(32)*2+"fail \"refusing a partial-override run:"
assert old in s, "anchor not found"
new=chr(32)*2+"alert_email \"[JomForm] Backup REFUSED\" \"sabotage\""+chr(10)+old
open(p,"w").write(s.replace(old,new,1))'

# 6. The escape hatch made permissive: JFBACKUP_ALLOW_UNSAFE defaults to set, so
#    no partial override is ever refused. A default-permissive seam is the same
#    defect with a friendlier name.
bite "escape hatch defaults to ON (the seam is permissive by default)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="  if [ -n \"${JFBACKUP_ALLOW_UNSAFE:-}\" ]; then"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"  if [ -n \"${JFBACKUP_ALLOW_UNSAFE:-1}\" ]; then",1))'

# 7. The seam stops watching the four LOCATION knobs, so a hand-run that sets
#    only STAGE_DIR is no longer recognised as a hand-run at all.
bite "seam stops watching the location knobs (hand-runs become invisible)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
for v in ("JFBACKUP_STAGE_DIR","JFBACKUP_STATE_DIR","JFBACKUP_LOG_FILE","JFBACKUP_LOCK_FILE"):
    old="seam_moved "+v
    assert old in s, "anchor not found: "+old
    s=s.replace(old,"true # SABOTAGE "+v,1)
open(p,"w").write(s)'

# 8. The seam stops watching the DESTINATION knobs. The guard is still called and
#    still tests all three legs, but a run that moves only JFBACKUP_REMOTE_ROOT is
#    no longer classified as a hand-run, so it returns at the top of the guard with
#    the credential default unexamined — the destination-only shape walks straight
#    through to the real SES credentials and the real inbox.
bite "seam stops watching the destination knobs (destination-only runs become invisible)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
for v in ("JFBACKUP_REMOTE","JFBACKUP_REMOTE_ROOT"):
    old="seam_moved "+v
    assert old in s, "anchor not found: "+old
    s=s.replace(old,"true # SABOTAGE "+v,1)
open(p,"w").write(s)'

# =============================================================================
# Legacy failure-path mutations
# =============================================================================
# The seven mutations above all target the fail-closed seam. The README's
# "Tested failure paths" section also makes the same claim for the twelve legacy
# failure paths the script protects, so they are sabotaged below too.
#
# A claim like that is only true if this file actually applies each sabotage —
# so each is applied to the throwaway copy and the suite is required to catch
# it. Three of them did NOT bite against the suite as it stood (the `FAILED`
# line's level, an ignored restore failure, and the cron `PATH` pin); the suite
# grew the assertions those three need, and each mutation is what proves those
# assertions are load-bearing.
#
# Every mutation is an exact-substring replacement with an anchor assertion, so
# a mutation whose anchor disappears ABORTS rather than silently matching
# nothing — the failure mode that makes a bite harness lie about its coverage.

# bite_py <label> <expected-FAIL-regex> <<'PY' ... PY
# The mutation program arrives on stdin as a QUOTED heredoc, so it is literal:
# no shell expansion and no escaping, and the anchors can carry real quotes.
# (The seam mutations above predate this and keep their inline `python3 -c`
# form; two of the legacy ones were first written that way and came out wrong,
# which is exactly what the heredoc avoids.)
bite_py() {
  local label="$1" expect="$2"
  reset_work
  local pyfile="$WORK/mutate.py"
  cat >"$pyfile"
  echo "=============================================================="
  echo "SABOTAGE: $label"
  local T="$WORK/jomform-backup.sh"
  MUTATE_TARGET="$T" python3 "$pyfile"
  if [ $? -ne 0 ]; then
    echo "  ABORT: the mutation program failed to run — not a valid sabotage"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  if cmp -s "$SRC/jomform-backup.sh" "$T"; then
    echo "  ABORT: the mutation changed nothing — the sabotage did not land"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  if ! bash -n "$T" 2>/dev/null; then
    echo "  ABORT: the mutation left the script unparseable (bash -n failed) — not a valid sabotage"
    bash -n "$T" 2>&1 | head -3 | sed 's/^/    /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  local out rc summary owned
  out="$(bash "$WORK/jomform-backup_test.sh" 2>&1)"
  rc=$?
  summary="$(printf '%s\n' "$out" | tail -1)"
  owned="$(printf '%s\n' "$out" | grep -cE "^\s+FAIL .*($expect)" || true)"
  if [ $rc -ne 0 ] && [ "${owned:-0}" -gt 0 ]; then
    echo "  RESULT: RED ($owned assertion(s) on this path failed) — $summary"
    printf '%s\n' "$out" | grep -E '^\s+FAIL' | head -5 | sed 's/^/    /'
    CAUGHT=$((CAUGHT + 1))
  else
    echo "  RESULT: *** GREEN — THE SABOTAGE WAS NOT CAUGHT *** (rc=$rc, $summary)"
    echo "    the suite passed with this sabotage in place, so nothing in it"
    echo "    measures this path. First FAILs seen:"
    printf '%s\n' "$out" | grep -E '^\s+FAIL' | head -5 | sed 's/^/      /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
  fi
  reset_work
}

echo
echo "### legacy failure paths — the twelve the README names as proven to bite"

# Hash verification removed: a corrupt or truncated upload passes on size alone.
bite_py "removed hash verification" \
  'log names the hash mismatch|the failure is logged at ERROR' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  if [ "$got_hash" != "$want_hash" ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "  if false; then # SABOTAGE: no hash compare", 1))
PY

# Post-upload verification removed: "exit 0" becomes "uploaded" again.
bite_py "removed post-upload verification" \
  'log names the missing object|last-failure.json written' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  if ! VERIFY_ERR="$(verify_remote '
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, '  if ! VERIFY_ERR=""; then # SABOTAGE: no verify', 1))
PY

# The FAILED line downgraded to WARN: the message text survives, so only an
# assertion on the LEVEL can catch this.
bite_py "FAILED line downgraded to WARN" \
  'the failure is logged at ERROR, not downgraded to WARN' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  log ERROR "jomform-backup FAILED: $DETAIL"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, '  log WARN "jomform-backup FAILED: $DETAIL"', 1))
PY

# Empty-archive guard removed: a mid-restart pg_dump uploads a file with no
# table data in it.
bite_py "removed empty-archive guard" \
  'log rejects the archive' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  if [ "${tables:-0}" -lt 1 ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "  if false; then # SABOTAGE: guard removed", 1))
PY

# Status JSON unescaped: a real rclone error carries double quotes, which makes
# last-run.json unparseable for every machine that reads it.
bite_py "unescaped status JSON" \
  'status file is parseable|status file OK|status file FAILED|drill file says OK' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = """  python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$1\""""
assert old in s, "anchor not found"
new = """  printf '%s' "$1"  # SABOTAGE: unescaped"""
open(p, "w").write(s.replace(old, new, 1))
PY

# Ignored restore failure: the drill reports OK on a restore that failed.
bite_py "ignored restore failure" \
  'drill exit code|the drill status records FAILED|the drill does not claim success' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '''    echo "pg_restore failed for $file: $diag"
    return 1'''
assert old in s, "anchor not found"
new = '''    echo "pg_restore failed for $file: $diag"
    return 0  # SABOTAGE: restore failure ignored'''
open(p, "w").write(s.replace(old, new, 1))
PY

# An upload failure returning exit 0: the run "succeeds" having uploaded
# nothing, and last-failure.json is never written.
bite_py "upload failure returning exit 0" \
  'the failure is logged at ERROR|log marks FAILED|syslog alert raised|local dump still exists' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '    uploads_ok=0'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "    : # SABOTAGE: upload failure swallowed", 1))
PY

# The no-hash branch removed: a backend exposing no hash silently degrades to a
# size-only comparison, which is the check the script exists to refuse.
bite_py "backend exposing no hash degrades to a size-only check" \
  'log reports no hash' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  if [ -z "$got_hash" ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "  if false; then # SABOTAGE: size-only check", 1))
PY

# The "rclone is not installed" check removed: a missing binary is reported as
# merely an unconfigured remote.
bite_py "rclone-not-installed check removed" \
  'log names rclone' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  command -v rclone >/dev/null 2>&1 || { echo "rclone is not installed"; return 1; }'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "  : # SABOTAGE: no rclone presence check", 1))
PY

# The remote check disabled: a missing or expired remote no longer stops the run
# before it uploads into nowhere.
bite_py "disabled remote check" \
  'log names the remote|alert body warns about same-host dump|token fix is named' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
# Replace only the membership test, not the whole line: the line ends with a
# backslash continuation into the `|| { ... }` branch, so a trailing comment
# here would swallow the continuation and make the script unparseable.
old = 'grep -qx "${REMOTE}:"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'grep -q .', 1))
PY

# The cron PATH pin removed: the job depends on the interactive PATH and dies the
# first time cron gives it none. The suite hands the job a PATH that resolves
# nothing, which is what makes this bite.
bite_py "removed cron PATH pin" \
  'unusable inherited PATH|the dump still happened|the run completed rather than dying' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '_SYS_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"'
assert old in s, "anchor not found"
s = s.replace(old, '_SYS_PATH="${PATH:-}" # SABOTAGE: PATH not pinned', 1)
old2 = 'else\n  PATH="$_SYS_PATH"\nfi'
assert old2 in s, "second anchor not found"
open(p, "w").write(s.replace(old2, 'else\n  : # SABOTAGE: no PATH pin\nfi', 1))
PY

# A restore drill writing the backup job's status: a green drill hides a failing
# nightly backup from an operator reading last-run.json.
bite_py "restore drill writing the backup job status" \
  'backup status is untouched|drill recorded under its own name' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '  STATUS_FILE_NAME="last-restore-drill.json"'
assert old in s, "anchor not found"
new = '  STATUS_FILE_NAME="last-run.json" # SABOTAGE: drill writes the job status'
open(p, "w").write(s.replace(old, new, 1))
PY

rm -rf "$WORK"

echo "=============================================================="
echo "sabotages caught by the suite: $CAUGHT"
echo "sabotages the suite did NOT catch: $DID_NOT_BITE"
[ "$DID_NOT_BITE" -eq 0 ]
