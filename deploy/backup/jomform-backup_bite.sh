#!/usr/bin/env bash
#
# Bite proof for the failure paths in jomform-backup.sh: the fail-closed test
# seam, and the thirteen legacy failure paths the README names as proven to bite.
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

# --------------------------------------------------------------- sandbox (do not remove)
# Two of the mutations below make the mutated script IGNORE an explicitly named
# knob, so it resolves that knob to the value INSTALLED on the host — the state
# dir, the staging dir. Run unsandboxed, that means this harness writes fabricated
# status files and staged dumps into the very production paths the backup job
# owns. That is not hypothetical: an earlier version of the new containment group
# did exactly that while being written, and it had to be hand-remediated.
#
# So the whole harness re-execs itself inside a private mount namespace with a
# tmpfs over each installed-default path before ANY mutation runs. A mutated
# script can then only ever reach the overlay. If `unshare` is unavailable the
# harness REFUSES to run rather than risk the host — a silent fallback to writing
# production state is the failure this wrapper exists to prevent.
if [ "${JFB_BITE_SANDBOXED:-0}" != "1" ]; then
  if ! command -v unshare >/dev/null 2>&1; then
    echo "REFUSING TO RUN: no \`unshare\`, and this harness must not run unsandboxed."
    echo "Two of its mutations redirect the script's state/staging to the installed paths."
    exit 2
  fi
  exec unshare -m --propagation private bash -c '
    set -e
    for d in /var/lib/jomform-backup /var/backups/jomform /var/lock /var/log; do
      mkdir -p "$d"
      mount -t tmpfs -o mode=0755 tmpfs "$d"
    done
    JFB_BITE_SANDBOXED=1 bash "$0"
  ' "$0"
fi

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
# The mutation runs with $MUTATE_TARGET = the throwaway SCRIPT path.
bite() { bite_one jomform-backup.sh "$@"; }

# bite_readme <label> <mutation-command...>
# Same contract, but $MUTATE_TARGET is the throwaway README.md: the suite reads
# its cron assertion out of that file, so a README mutation is the only way to
# show that assertion can fail at all.
bite_readme() { bite_one README.md "$@"; }

bite_one() { # <file to mutate, relative to the copied dir> <label> [mutation...]
  local target="$1"; shift
  local label="$1"; shift
  echo "=============================================================="
  echo "SABOTAGE: $label"
  reset_work
  local T="$WORK/$target"
  MUTATE_TARGET="$T" "$@"
  if cmp -s "$SRC/$target" "$T"; then
    echo "  ABORT: the mutation changed nothing — the sabotage did not land"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  # A mutation that leaves the SCRIPT unparseable measures nothing: every case
  # would fail for the same crash, which is not evidence about the seam. A
  # README mutation has no parser to run.
  if [ "$target" = "jomform-backup.sh" ] && ! bash -n "$T" 2>/dev/null; then
    echo "  ABORT: the mutation left the script unparseable (bash -n failed) — not a valid sabotage"
    bash -n "$T" 2>&1 | head -3 | sed 's/^/    /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    return
  fi
  local out rc summary seam_fails
  out="$(bash "$WORK/jomform-backup_test.sh" 2>&1)"
  rc=$?
  summary="$(printf '%s\n' "$out" | tail -1)"
  # Only a FAIL on an assertion about the guard or about the documented cron
  # shape counts as a bite: a red suite for an unrelated reason would otherwise
  # be read as proof.
  seam_fails="$(printf '%s\n' "$out" | grep -cE '^\s+FAIL .*(refusing|JFBACKUP_ENV_FILE|JFBACKUP_REMOTE|no dump was taken|no media archive|listing changed|no alert email|no ALERT|the drill never started|no lock file|seam does not watch|seam tests that|destination|cron entry)' || true)"
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

# 3. The REMOTE_ROOT leg removed: the production R2 half.
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

# 6. The escape hatch made permissive: JFBACKUP_ALLOW_UNSAFE opens the hatch
#    whatever value it holds, so `JFBACKUP_ALLOW_UNSAFE=0` — a caller writing the
#    value that means NO — is read as a deliberate real run. The exact
#    pre-fix `[ -n ... ]` test, restored.
bite "the escape hatch opens on a non-1 value (JFBACKUP_ALLOW_UNSAFE=0 behaves like =1)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="  if [ \"${JFBACKUP_ALLOW_UNSAFE:-}\" = \"1\" ]; then"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"  if [ -n \"${JFBACKUP_ALLOW_UNSAFE:-}\" ]; then",1))'

# 6b. The escape hatch defaulting to ON: the value test widened to `:-1`, so an
#     UNSET JFBACKUP_ALLOW_UNSAFE opens the hatch and no partial override is ever
#     refused. A default-permissive seam is the same defect with a friendlier
#     name, and only a case that leaves the variable unset can catch it.
bite "escape hatch defaults to ON (the seam is permissive by default)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="  if [ \"${JFBACKUP_ALLOW_UNSAFE:-}\" = \"1\" ]; then"
assert old in s, "anchor not found"
open(p,"w").write(s.replace(old,"  if [ \"${JFBACKUP_ALLOW_UNSAFE:-1}\" = \"1\" ]; then",1))'

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
# "Tested failure paths" section also makes the same claim for the thirteen legacy
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
echo "### legacy failure paths — the thirteen the README names as proven to bite"

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
  'log names the remote|alert body warns about same-host dump|the R2 credential remedy is named' <<'PY'
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

# 8. The documented cron line is made refusable, i.e. the nightly job would be
#    refused by the seam — the exact failure the cron assertion exists to
#    prevent. The script is untouched, so ONLY the assertion that reads the cron
#    entry can go red. (Before this sabotage existed, that assertion grepped
#    README line 8 — a prose sentence naming the same script path — so it read
#    prose instead of the cron entry and stayed green here.)
bite_readme "the documented cron entry sets a seam knob (the nightly job would be refused)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="17 2 * * * /usr/local/bin/jomform-backup.sh"
assert old in s, "anchor not found"
new="17 2 * * * JFBACKUP_STAGE_DIR=/tmp/x /usr/local/bin/jomform-backup.sh"
open(p,"w").write(s.replace(old,new,1))'

# 9. The documented cron line deleted. With nothing to read the nightly job's
#    shape is unverified, so this must FAIL — a "no cron found" branch that
#    reported ok would be the same vacuous check wearing a different coat.
bite_readme "the documented cron entry is removed (nothing left to check)" \
  python3 -c 'import os
p=os.environ["MUTATE_TARGET"]; s=open(p).read()
old="17 2 * * * /usr/local/bin/jomform-backup.sh >> /var/log/jomform-backup.log 2>&1"
assert old in s, "anchor not found"
new="# SABOTAGE: cron entry deleted"
open(p,"w").write(s.replace(old,new,1))'

# The monthly gate's failure no longer counted: a failed monthly promotion does
# not fail the run, which reports a healthy month with an unverifiable (or absent)
# monthly copy (exit 0, no `last-failure.json`).
#
# The anchor is the monthly gate's own FAIL_DETAIL + uploads_ok block, which is
# the only occurrence carrying "monthly promotion failed": the weekly gate is
# separate now, so a future edit that moves or rewrites the monthly block makes
# this mutation ABORT rather than quietly mutate the weekly path instead.
bite_py "monthly promotion failure returning a healthy run" \
  'log marks FAILED|last-failure.json written' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '''    FAIL_DETAIL="${PROMOTE_DETAIL:-monthly promotion failed}"
    log ERROR "$FAIL_DETAIL"
    uploads_ok=0
'''
assert old in s, "anchor not found"
new = '''    FAIL_DETAIL="${PROMOTE_DETAIL:-monthly promotion failed}"
    log ERROR "$FAIL_DETAIL"
'''
open(p, "w").write(s.replace(old, new, 1))
PY

# bite_target <label> <expected-FAIL-regex> <relative-file-in-WORK> <<'PY' ... PY
# Like bite_py, but mutates whichever file in the throwaway copy the property
# lives in — the README's hand-run recipe and the suite's own helper are files a
# case asserts about, so the sabotage has to land there, not in the script.
# The parse check is applied to the script only; a mutated suite/README is
# checked by the suite failing, which is the assertion under test.
bite_target() {
  local label="$1" expect="$2" rel="$3"
  reset_work
  local pyfile="$WORK/mutate.py"
  cat >"$pyfile"
  echo "=============================================================="
  echo "SABOTAGE: $label"
  local T="$WORK/$rel"
  if [ ! -f "$T" ]; then
    echo "  ABORT: $T does not exist in the throwaway copy"
    DID_NOT_BITE=$((DID_NOT_BITE + 1)); return
  fi
  local before; before="$(sha256sum "$T" | cut -d' ' -f1)"
  MUTATE_TARGET="$T" python3 "$pyfile"
  if [ $? -ne 0 ]; then
    echo "  ABORT: the mutation program failed to run — not a valid sabotage"
    DID_NOT_BITE=$((DID_NOT_BITE + 1)); return
  fi
  if [ "$(sha256sum "$T" | cut -d' ' -f1)" = "$before" ]; then
    echo "  ABORT: the mutation changed nothing — the sabotage did not land"
    DID_NOT_BITE=$((DID_NOT_BITE + 1)); return
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

# =============================================================================
# The remote check's matcher — a configured remote must not be refused
# =============================================================================
# `rclone listremotes` prints one line per remote, one write() per line with a real
# rclone. The matcher in require_remote reads that list from a here-string instead
# of through a pipe ON PURPOSE: `grep -qx` exits at its first match, so a pipe
# reader can be gone while the writer still has whole lines queued — the writer
# takes SIGPIPE, and under `set -o pipefail` the pipeline reports 141 although
# grep MATCHED. A remote that IS configured is then refused as "not configured":
# the nightly backup fails closed and mails a false "remote unavailable" alarm.
#
# This mutation changes ONLY the matcher body (the two other preconditions and
# every word of the refusal are untouched), so the red it must produce is about
# the read SHAPE and nothing else. What makes it bite is a config with more than
# one remote: the suite's two multi-remote cases drive a stub whose needle is the
# FIRST line and whose writer PAUSES before the rest, which is what turns the race
# into a deterministic red (196/200 refusals with no pause, 200/200 with one).
echo
echo "### the remote check's matcher (a configured remote must not be refused)"

bite_py "the remote check reads listremotes through a pipe again (a configured remote is refused)" \
  'multi-remote: exit code|the configured remote was NOT refused|no false alarm was raised' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '''  remotes="$(rclone --config "$RCLONE_CONFIG" listremotes 2>/dev/null || :)"
  grep -qx "${REMOTE}:" <<<"$remotes" \\
'''
assert old in s, "anchor not found"
new = '''  rclone --config "$RCLONE_CONFIG" listremotes 2>/dev/null \\
    | grep -qx "${REMOTE}:" \\
'''
open(p, "w").write(s.replace(old, new, 1))
PY

# =============================================================================
# Containment mutations — the guard is a refusal rule, not containment
# =============================================================================
# The README's hand-run section and the two cases at the end of the suite assert
# a property the seam does NOT provide: that a run is contained because the knobs
# naming its paths were provided. These mutations remove that property one way at
# a time and require the suite to go red.
#
# Two of them are the reason this whole file runs inside a mount namespace (see
# the sandbox block at the top): a script that ignores a named knob writes into
# the path installed on the host.
echo
echo "### containment — the guard refuses, it does not contain"

# The README's recipe drops JFBACKUP_STATE_DIR: a reader copying the block gets a
# run whose status file lands in the installed state dir.
bite_target "README recipe drops JFBACKUP_STATE_DIR" \
  'the hand-run recipe does not name JFBACKUP_STATE_DIR' "README.md" <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = "JFBACKUP_STATE_DIR=/tmp/jfbtest/state \\\n"
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "", 1))
PY

# The suite's own helper drops a knob: every case run through it would then reach
# the path installed on the host.
bite_target "suite helper drops JFBACKUP_STATE_DIR" \
  'the suite helper does not name JFBACKUP_STATE_DIR' "jomform-backup_test.sh" <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '      JFBACKUP_STATE_DIR="$CASE_DIR/state" \\\n'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, "", 1))
PY

# The false sentence is put back: "safety is a property of the script".
bite_target "false 'property of the script' sentence restored" \
  'no longer claims safety is a property of the script' "README.md" <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = "The same rule binds the committed test suite"
assert old in s, "anchor not found"
new = "safety is a property of the script rather than of the caller remembering. " + old
open(p, "w").write(s.replace(old, new, 1))
PY

# The script ignores an explicitly named STATE_DIR. The control pair in the suite
# must flip: the "named" leg writes to the installed state dir.
bite_py "script ignores a named STATE_DIR" \
  'named STATE_DIR' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'STATE_DIR="${JFBACKUP_STATE_DIR:-$STATE_DIR_DEFAULT}"'
assert old in s, "anchor not found"
new = 'STATE_DIR="$STATE_DIR_DEFAULT"  # SABOTAGE: knob ignored'
open(p, "w").write(s.replace(old, new, 1))
PY

# The script ignores an explicitly named STAGE_DIR: the "named" leg stages its
# dump into the installed staging dir.
bite_py "script ignores a named STAGE_DIR" \
  'named STATE_DIR: nothing was staged' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'STAGE_DIR="${JFBACKUP_STAGE_DIR:-$STAGE_DIR_DEFAULT}"'
assert old in s, "anchor not found"
new = 'STAGE_DIR="$STAGE_DIR_DEFAULT"  # SABOTAGE: knob ignored'
open(p, "w").write(s.replace(old, new, 1))
PY

# =============================================================================
# R2 port, weekly tier and retention — the assertions THIS change adds
# =============================================================================
echo
echo "### R2 writes and the R2 credential remedy"

# Every rclone WRITE must carry --s3-no-check-bucket. The R2 token is scoped to
# ONE bucket, so ListBuckets is denied; without the flag rclone concludes the
# bucket is missing, calls CreateBucket (403) and EVERY write fails — the backup
# fails every night. This is the single most valuable assertion in the change:
# without it a future edit silently re-introduces a backup that never uploads.
bite_py "rclone write missing --s3-no-check-bucket (the bucket token cannot CreateBucket)" \
  'upload verified|log says OK|every write carried|the flag was dropped' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = " --s3-no-check-bucket"
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, ""))
PY

# The Drive remedy (`rclone config reconnect <remote>:`) does not exist for an S3
# remote. Put it back and the alert sends an operator to a command that cannot
# work, while the real R2 remedy goes unnamed.
bite_py "the Drive OAuth reconnect hint restored in place of the R2 remedy" \
  'the R2 credential remedy is named|no OAuth reconnect hint' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = '''The S3 credentials for the '${REMOTE}:' remote live in /root/.config/rclone/rclone.conf
(mode 600). An S3 remote has no OAuth token to reconnect: if uploads fail with an
auth error, issue a NEW R2 API token in the Cloudflare dashboard (Object Read & Write
on the '$REMOTE_ROOT' bucket) and write its Access Key ID / Secret Access Key into
that config, then re-run the backup.'''
assert old in s, "anchor not found"
new = 'If the token has expired, re-run: rclone config reconnect ${REMOTE}:'
open(p, "w").write(s.replace(old, new, 1))
PY

echo
echo "### weekly tier and media promotion"

# The weekly gate disabled: the weekly tier never builds.
bite_py "weekly gate disabled (the weekly tier never builds)" \
  'weekly promotion ran|weekly object on the remote|weekly promotion reported ok' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'if [ "$DAY_OF_WEEK" = "7" ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'if false; then # SABOTAGE: weekly gate disabled', 1))
PY

# The weekly gate always on: it promotes every night, duplicating the tier's slot.
bite_py "weekly gate always on (promotes every night)" \
  'no weekly promotion on a non-Sunday|no weekly dump object written on a non-Sunday' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'if [ "$DAY_OF_WEEK" = "7" ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'if true; then # SABOTAGE: weekly gate always on', 1))
PY

# The media archive no longer promoted into a tier: a tier restore rebuilds the
# rows but renders no customer media. Covers BOTH tiers (promote_tier is shared).
bite_py "media archive no longer promoted into a tier" \
  'media archive|media\.tar\.gz|byte-identical' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'if [ "$tier_ok" = "1" ] && [ -f "$media_local" ]; then'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'if false; then # SABOTAGE: media not promoted', 1))
PY

echo
echo "### retention budgets and overrides"

bite_py "daily retention budget reverted (KEEP_DAILY 21 -> 30)" \
  'daily files kept|daily SETS kept' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'KEEP_DAILY="${JFBACKUP_KEEP_DAILY:-21}"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'KEEP_DAILY="${JFBACKUP_KEEP_DAILY:-30}" # SABOTAGE', 1))
PY

bite_py "weekly retention budget reverted (KEEP_WEEKLY 12 -> 4)" \
  'weekly files kept' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'KEEP_WEEKLY="${JFBACKUP_KEEP_WEEKLY:-12}"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'KEEP_WEEKLY="${JFBACKUP_KEEP_WEEKLY:-4}" # SABOTAGE', 1))
PY

bite_py "monthly retention budget reverted (KEEP_MONTHLY 18 -> 12)" \
  'monthly files kept' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'KEEP_MONTHLY="${JFBACKUP_KEEP_MONTHLY:-18}"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'KEEP_MONTHLY="${JFBACKUP_KEEP_MONTHLY:-12}" # SABOTAGE', 1))
PY

bite_py "weekly prune call removed (weekly sets are never pruned)" \
  'weekly files kept|weekly override keeps' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = "prune_remote \"$WEEKLY_DIR/\" \"$KEEP_WEEKLY\" '^jomform-weekly-'"
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, ': # SABOTAGE: weekly prune removed', 1))
PY

bite_py "KEEP_* overrides ignored (the budget is hard-coded)" \
  'daily override keeps|weekly override keeps|monthly override keeps' <<'PY'
import os
p = os.environ["MUTATE_TARGET"]
s = open(p).read()
old = 'KEEP_DAILY="${JFBACKUP_KEEP_DAILY:-21}"'
assert old in s, "anchor not found"
open(p, "w").write(s.replace(old, 'KEEP_DAILY="21" # SABOTAGE: override ignored', 1))
PY

rm -rf "$WORK"

echo "=============================================================="
echo "sabotages caught by the suite: $CAUGHT"
echo "sabotages the suite did NOT catch: $DID_NOT_BITE"
[ "$DID_NOT_BITE" -eq 0 ]
