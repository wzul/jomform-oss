#!/usr/bin/env bash
#
# jomform-container-prune_bite.sh — the sabotage proof for the container-prune
# guard and its installer.
#
# A suite that still passes with the behaviour it pins REMOVED measures nothing.
# This sabotages ONE gate of the guard at a time (and, once, the installer's
# idempotency), runs the committed decoy suite against the mutated copy, and
# requires it to go RED on the assertion that names THAT gate — so a red for an
# unrelated reason is not credited, and a green is reported as "DID NOT BITE".
#
# It never touches the checkout: the guard, the installer and the suite are
# copied into a temporary directory first, and only the copy is mutated. The
# three committed files are sha256-checked before and after the run.
#
# It is ON DEMAND, not a preflight gate: each case costs a whole decoy suite
# (a private dockerd per case), measured ~24s, so the nine cases below are ~4
# minutes — the same reason deploy/backup/jomform-backup_bite.sh is not wired in.
# The SUITE is the per-merge gate (`prunehost`); this is the instrument that
# proves the suite's assertions can fail.
#
# Run:  bash deploy/host/jomform-container-prune_bite.sh
# Exit: 0 only if the baseline suite was GREEN and EVERY sabotage was caught;
#       1 if a case did not bite; 2 if the harness itself could not run.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD_FILE=jomform-container-prune.sh
SUITE_FILE=jomform-container-prune_test.sh
INSTALLER_FILE=install-host-container-prune.sh

WORK=""
CAUGHT=0
DID_NOT_BITE=0
TOTAL=0

say()   { printf '%s\n' "$*"; }
fatal() { printf 'FATAL: %s\n' "$*" >&2; exit 2; }

sha_of() { sha256sum "$HERE/$1" | awk '{print $1}'; }

for f in "$GUARD_FILE" "$SUITE_FILE" "$INSTALLER_FILE"; do
  [ -f "$HERE/$f" ] || fatal "$HERE/$f not found"
  bash -n "$HERE/$f" || fatal "$HERE/$f is not valid bash"
done

SHA_GUARD_BEFORE="$(sha_of "$GUARD_FILE")"
SHA_SUITE_BEFORE="$(sha_of "$SUITE_FILE")"
SHA_INSTALLER_BEFORE="$(sha_of "$INSTALLER_FILE")"

reset_work() {
  if [ -n "$WORK" ]; then
    case "$WORK" in
      "${TMPDIR:-/tmp}"/jf-prune-bite-bite.*) rm -rf "$WORK" ;;
      *) fatal "refusing to remove $WORK (not a jf-prune-bite-bite.* under TMPDIR)" ;;
    esac
  fi
  WORK="$(mktemp -d "${TMPDIR:-/tmp}/jf-prune-bite-bite.XXXXXX")"
  cp "$HERE/$GUARD_FILE" "$HERE/$SUITE_FILE" "$HERE/$INSTALLER_FILE" "$WORK/"
}
trap 'if [ -n "$WORK" ]; then case "$WORK" in "${TMPDIR:-/tmp}"/jf-prune-bite-bite.*) rm -rf "$WORK" ;; esac; fi' EXIT

# sub <old> <new> — replace an anchor in $MUTATE_TARGET, EXACTLY once. An
# anchor that occurs 0 or 2+ times is a harness bug, never a silent no-op: that
# is the failure mode that makes a bite harness report green while measuring the
# shipped file.
sub() {
  python3 - "$MUTATE_TARGET" "$1" "$2" <<'PY'
import pathlib, sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
text = pathlib.Path(path).read_text()
n = text.count(old)
if n != 1:
    sys.stderr.write("anchor occurs %d times, expected exactly 1:\n%s\n" % (n, old))
    sys.exit(3)
pathlib.Path(path).write_text(text.replace(old, new))
PY
}

# ---------------------------------------------------------------- the sabotages
sab_state_gate()     { sub '    exited|created|dead) ;;' '    exited|created|dead|running) ;;'; }
sab_age_gate()       { sub '  if [ "$epoch" -ge "$cutoff" ]; then' '  if false; then'; }
sab_compose_gate()   { sub '  if [ -n "$proj" ]; then' '  if false; then'; }
sab_keepfile_gate()  { sub '    KEEP_NAMES="$KEEP_NAMES $extra "' '    : "sabotage: the keep file is ignored"'; }
sab_fail_closed()    { sub 'if [ -z "$raw" ]; then' 'if false; then'; }

# THE NETWORK-NAMESPACE CONTAINMENT of the private daemon (card `t_a10c01dd`).
# `--bridge=none` makes a `dockerd` DELETE the default bridge of whatever network
# namespace it is started in, so the decoy suite re-execs itself under `unshare -n`
# (see `deploy/host/jomform-container-prune_test.sh`) and the bridge it deletes is a
# decoy of its own. Dropping `unshare -n` from that re-exec puts the daemon back in
# the CALLER's namespace — which on this host is the one holding `docker0`, and the
# defect the card is about — and the suite must then go RED on the arm that names
# it. THE CASE IS SAFE TO RUN ON A LIVE HOST, and that is not luck: the namespace arm
# is checked BEFORE anything destructive exists and FAILS CLOSED, so the mutation
# cannot reach a daemon start (nothing is planted, nothing is removed, no decoy
# suite runs) — measured on the agent host 2026-10-12: after the case, the host's
# docker0 kept its ifindex and MAC and the watchdog log gained no ALERT.
sab_netns_isolation() { sub 'exec unshare -n env \' 'exec env \'; }
sab_installer_idem() { sub 'if [ "${have:-0}" -gt 0 ]; then' 'if false; then'; }

# THE SHARED-DAEMON ATTRIBUTION (card `t_da7509c4`). The suite compares the host's
# before/after container set and must ATTRIBUTE a loss — to this run, to another actor,
# or to nobody it can name — instead of counting it. Both halves are sabotaged below,
# because a classifier that never says FOREIGN reds the gate on a peer's redeploy (the
# original false red) and one that never says OURS stops noticing a suite that really
# did remove a host container.
#
# FOREIGN: the destroy-event lookup is replaced by "never found", so the probe a peer
# removes mid-run becomes UNATTRIBUTABLE and the suite must go RED naming it. That is
# the arm this suite runs on every invocation, so the case proves the live arm can
# fail — the false red cannot come back without a red here first.
sab_attribute_foreign() {
  sub '$3=="destroy" && $4==id' '0'
}

# OURS: this run's own removal log is no longer consulted, so a container this run
# removed would be excused as FOREIGN. Nothing in a quiet run exercises that branch,
# so the classifier's own self-test is the instrument — and it must go RED.
sab_attribute_ours() {
  sub 'for c in ${HOST_OWN_REMOVED[@]+"${HOST_OWN_REMOVED[@]}"}; do' 'for c in ""; do'
}

# The guard's PRIVATE rm-output file (card `t_3d34df24`). The four-line block that
# creates it is replaced wholesale in both cases below, because a hand-split anchor
# across it is exactly the shape that silently matches nothing. Both anchors are one
# exact block: `sub` ABORTS the case when it occurs 0 or 2+ times.
RM_OUT_BLOCK='rm_out="$(mktemp "${TMPDIR:-/tmp}/jomform-container-prune.rm.XXXXXXXX")" || {
  log "FATAL: could not create a private rm-output file (mktemp failed) - fail closed, nothing removed"
  exit 1
}'

# THE CARDED DEFECT, RESTORED: `docker rm`'s output goes back to the FIXED,
# predictable path in a world-writable directory that the guard shipped with. Root
# opens it for writing BEFORE `docker rm` runs, so the symlink the suite plants
# there first is followed and the file it points at is truncated.
sab_fixed_rm_path() { sub "$RM_OUT_BLOCK" 'rm_out=/tmp/jomform-container-prune.rm.out'; }

# The OTHER half: the file is still private, but the guard no longer REFUSES when
# it cannot create one. Measured, not assumed: `rm_out` is then empty, `>"$rm_out"`
# fails with "No such file or directory", `docker rm` never runs, and the pass ends
# `removed=0 containers` with exit 0 — a silent no-op reported as success.
sab_rm_out_fail_closed() {
  sub "$RM_OUT_BLOCK" 'rm_out="$(mktemp "${TMPDIR:-/tmp}/jomform-container-prune.rm.XXXXXXXX")"'
}

# BOTH halves of the running-container defence. The guard has two and they are
# independent: the stopped-state filter keeps a running container out of the
# candidate list, and the `docker ps` re-read immediately before the removal
# drops any candidate that is running by then. Removing the filter ALONE leaves
# the decoy alive — measured 2026-10-12: the run goes red on five LISTING
# assertions and the real floor-0 pass still reports `removed=1 containers
# (4 -> 3)`, because the re-check aborts the running candidate. So the only way
# to show the re-check is load-bearing is to remove it too.
#
# The re-check's own condition line is read OUT OF THE FILE rather than typed
# here: it contains nested single quotes (awk's), and a hand-typed anchor with
# them is exactly the kind of thing that silently matches nothing.
sab_state_gate_and_recheck() {
  sub '    exited|created|dead) ;;' '    exited|created|dead|running) ;;' || return $?
  local anchor
  anchor="$(grep -m1 'END {exit !found}' "$MUTATE_TARGET")"
  [ -n "$anchor" ] || { echo "the re-check anchor was not found in $MUTATE_TARGET" >&2; return 3; }
  sub "$anchor" '  if false; then'
}

# bite <label> <file> <needle> <mutator>
bite() {
  local label="$1" file="$2" needle="$3" mutator="$4"
  TOTAL=$((TOTAL + 1))
  say "=============================================================="
  say "SABOTAGE: $label"
  reset_work
  local T="$WORK/$file"
  if ! MUTATE_TARGET="$T" "$mutator"; then
    say "  ABORT: the mutation did not land (see the anchor above) — this case measured nothing"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    reset_work
    return
  fi
  if cmp -s "$HERE/$file" "$T"; then
    say "  ABORT: the mutation changed nothing — the sabotage did not land"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    reset_work
    return
  fi
  if ! bash -n "$T" >/dev/null 2>&1; then
    say "  ABORT: the mutation left the file unparseable — every case would crash for that reason"
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
    reset_work
    return
  fi

  local out rc fails
  out="$(bash "$WORK/$SUITE_FILE" 2>&1)"; rc=$?
  fails="$(printf '%s\n' "$out" | grep -c '^  FAIL' || true)"
  if [ "$rc" -ne 0 ] && [ "${fails:-0}" -gt 0 ] && printf '%s\n' "$out" | grep '^  FAIL' | grep -qF -- "$needle"; then
    say "  RESULT: RED on the expected assertion — CAUGHT (rc=$rc, $fails failing check(s))"
    printf '%s\n' "$out" | grep '^  FAIL' | head -3 | sed 's/^/    /'
    CAUGHT=$((CAUGHT + 1))
  else
    say "  RESULT: *** DID NOT BITE *** (rc=$rc, $fails failing check(s))"
    say "    the suite passed (or red on something else) with this sabotage in place,"
    say "    so the assertion naming '$needle' proves nothing about it."
    printf '%s\n' "$out" | grep -E '^  FAIL|^RESULT' | head -6 | sed 's/^/      /'
    DID_NOT_BITE=$((DID_NOT_BITE + 1))
  fi
  reset_work
}

# ------------------------------------------------------------------ baseline
say "### baseline — the committed suite against the committed files must be GREEN"
reset_work
out="$(bash "$WORK/$SUITE_FILE" 2>&1)"; rc=$?
printf '%s\n' "$out" | tail -3 | sed 's/^/  /'
if [ "$rc" -ne 0 ]; then
  say "ABORT: the baseline suite is RED on the unmutated copy, so a red below could be the tree, not the sabotage"
  exit 2
fi
say "  baseline GREEN (rc=0) — every red below is the sabotage"
reset_work

# ------------------------------------------------------------------ the cases
bite "the STATE gate (the stopped-state filter widened to admit a RUNNING container)" \
     "$GUARD_FILE" \
     "the RUNNING decoy is never listed as a candidate" \
     sab_state_gate

bite "the PRE-REMOVAL RE-CHECK (both halves of the running guard removed, so the decoy is taken)" \
     "$GUARD_FILE" \
     "the RUNNING decoy survived a REAL floor-0 run" \
     sab_state_gate_and_recheck

bite "the AGE gate (the floor no longer spares a young container)" \
     "$GUARD_FILE" \
     "none of them is a candidate" \
     sab_age_gate

bite "the COMPOSE gate (a compose-labelled container is taken)" \
     "$GUARD_FILE" \
     "the compose-labelled decoy is spared by LABEL" \
     sab_compose_gate

bite "the KEEP-FILE gate (a name in /etc/jomform/keep-containers is taken)" \
     "$GUARD_FILE" \
     "the keep-file decoy is spared by NAME" \
     sab_keepfile_gate

bite "the FAIL-CLOSED gate (an empty container list is no longer an error)" \
     "$GUARD_FILE" \
     "an empty container list exits non-zero" \
     sab_fail_closed

bite "the NETWORK-NAMESPACE containment (the private daemon put back in the CALLER's namespace, where --bridge=none deletes the host's docker0)" \
     "$SUITE_FILE" \
     "would be started in the CALLER's network namespace" \
     sab_netns_isolation

bite "the INSTALLER's idempotency (the cron entry is added even when present)" \
     "$INSTALLER_FILE" \
     "two runs leave exactly ONE cron entry" \
     sab_installer_idem

bite "the SHARED-DAEMON attribution, FOREIGN half (a peer's removal no longer attributed, so it becomes the old bare count)" \
     "$SUITE_FILE" \
     "removal CANNOT be attributed" \
     sab_attribute_foreign

bite "the SHARED-DAEMON attribution, OURS half (this run's own removals no longer recognised, so a real one would be excused)" \
     "$SUITE_FILE" \
     "self-test: an id this run's own audited path removed is OURS" \
     sab_attribute_ours

bite "the PRIVATE rm-output file (the fixed /tmp path restored, so a symlink planted there is followed)" \
     "$GUARD_FILE" \
     "NOT written through" \
     sab_fixed_rm_path

bite "the FAIL-CLOSED refusal when no private rm-output file can be created" \
     "$GUARD_FILE" \
     "no private rm-output file means a non-zero exit" \
     sab_rm_out_fail_closed

# ------------------------------------------------------------------ the verdict
say ""
say "### the committed files must be untouched by this harness"
fail_restore=0
[ "$(sha_of "$GUARD_FILE")" = "$SHA_GUARD_BEFORE" ] || { say "  CHANGED: $GUARD_FILE"; fail_restore=1; }
[ "$(sha_of "$SUITE_FILE")" = "$SHA_SUITE_BEFORE" ] || { say "  CHANGED: $SUITE_FILE"; fail_restore=1; }
[ "$(sha_of "$INSTALLER_FILE")" = "$SHA_INSTALLER_BEFORE" ] || { say "  CHANGED: $INSTALLER_FILE"; fail_restore=1; }
if [ "$fail_restore" -eq 0 ]; then
  say "  all three sources byte-identical (sha256 re-checked)"
else
  say "  A SOURCE CHANGED — this harness must never edit the checkout"
  exit 2
fi

say ""
say "BITES: $CAUGHT/$TOTAL, did-not-bite: $DID_NOT_BITE"
if [ "$DID_NOT_BITE" -eq 0 ] && [ "$CAUGHT" -eq "$TOTAL" ]; then
  say "RESULT: ALL CASES BIT — exit 0"
  exit 0
fi
say "RESULT: A CASE DID NOT BITE — exit 1"
exit 1
