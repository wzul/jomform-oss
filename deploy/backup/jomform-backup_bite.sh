#!/usr/bin/env bash
#
# Bite proof for the fail-closed test seam in jomform-backup.sh.
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

rm -rf "$WORK"

echo "=============================================================="
echo "sabotages caught by the suite: $CAUGHT"
echo "sabotages the suite did NOT catch: $DID_NOT_BITE"
[ "$DID_NOT_BITE" -eq 0 ]
