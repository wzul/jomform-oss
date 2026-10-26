#!/usr/bin/env bash
# Bite proof for web/src/lib/devHosts.test.ts.
#
# Each case SABOTAGES the config in a different way, runs the guard, and asserts
# it goes RED. Case 0 asserts the UNSABOTAGED config is green first, so a guard
# that is red for an unrelated reason cannot be read as a bite. A guard that
# stays green through all four sabotages is not protecting anything. The config
# is restored after each case, and the file's sha256 is asserted unchanged at the
# end so a failed run cannot leave the tree edited.
#
# WHY THE BACKUP IS PER-RUN AND WHY THERE IS A LOCK (card t_d101a943)
#
# Both halves of this file were host-global, and it showed: on this board one
# card = one worktree, so two lanes running this harness at once is the ordinary
# case, and `web/next.config.mjs` was backed up through ONE fixed path
# (`/tmp/devHosts.cfg.orig`). Measured by QA on PR #1002's head (14/14 rounds
# left at least one tree dirty): tree A ended holding TREE B's bytes — including
# the staging hostname baked in as a literal default, i.e. A's committed tree left
# carrying the exact defect this guard exists to prevent — and another round left a
# tree with the whole hostname-validation loop deleted. Both runs printed
# `CONFIG NOT RESTORED` and left `M git status --porcelain web/next.config.mjs`:
# a later `npm run build` in that worktree builds the mutant and a later commit
# ships it.
#
#   1. The backup is a PER-RUN `mktemp`, so one run can never restore another
#      run's bytes — that is the half that fixes the cross-worktree case.
#   2. The run takes the FAMILY's per-worktree `flock`, `jomform-biteproof-docs-bite`,
#      keyed on `git rev-parse --absolute-git-dir`, exactly as its siblings do.
#      Every harness of this family mutates a file the family's guards read, and
#      two runs inside ONE worktree would otherwise be mutating and restoring
#      `next.config.mjs` under each other's feet mid-case. The key is the WORKTREE,
#      under a HOST-GLOBAL `/tmp` base (never `$TMPDIR`: the run this must exclude
#      usually belongs to another PROFILE — that is how this defect was found — and
#      a per-profile TMPDIR would put the two runs in different directories and
#      serialise nothing). Two lanes' cards therefore never queue behind each
#      other; only two runs of the family in the SAME worktree serialise.
#   3. A contended run WAITS and says so rather than failing — a refusal would turn
#      "someone else is running it" into a red harness — and a wait-out is exit 75.
#      `BITEPROOF_LOCK_WAIT=0` is how a caller asks for the refusal instead of the
#      wait, and no `flock` on PATH is exit 2 rather than a silent degradation to
#      "no lock" (a decorative lock is the same defect with a green light on it).
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits
# the open descriptor, so the lock is held for exactly as long as the copy that
# mutates anything lives, and the `trap ... EXIT` below releases it as surely as it
# restores the config — there is no separate unlock line that can be skipped. The
# child PROVES it holds the lock before it prints a verdict (a lock nobody can
# observe as held is the same "trust the absence" shape this family's verdict rule
# was rewritten to remove): a second take on the same file from a separate process
# must be REFUSED, and a fresh file in the same directory must lock FINE — or
# "refused" would mean flock is inert here, not that this run holds the lock.
#
# The restore is on EVERY exit (the `trap`), not on the non-error path only: an
# interrupt, or any abort between a mutation and its `restore`, used to leave the
# mutation on disk. And every `grep -q` that reads a captured guard output uses a
# HERE-STRING, never `printf '%s' "$out" | grep -q`: under this file's
# `set -uo pipefail` a MATCH could still report the pipeline as failed (`grep -q`
# exits at the first hit, `printf` takes SIGPIPE, pipefail propagates 141), so the
# `&&` chain short-circuited and the case printed a false `DID NOT BITE` while the
# counts were right. Measured on this host over the REAL case-2 capture (1899 B,
# `ℹ pass 7 / ℹ fail 1`): the pipe form false-negatived 6/2000 iterations, the
# here-string 0/2000; with `pipefail` off, 0/2000 vs 0/2000. Failure direction is
# false-RED only, never a false PASS.
#
# Run:  bash web/scripts/biteproof_dev_hosts.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the
# child this file re-execs under the lock needs a path that survives a changed
# CWD — a relative `$0` resolved after the `cd` names a file that is not there.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# The shared runner gate and verdict rule (web/scripts/lib/node_test_verdict.sh): every
# verdict this harness prints comes from POSITIVE runner evidence, and a node that cannot
# run the guard is a REFUSAL that names itself — never an answer (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no verdict rule to read a guard's output with." >&2
  exit 2
}
cd "$BITE_HOME/.." || exit 2                # -> web/
CFG=next.config.mjs
GUARD=src/lib/devHosts.test.ts
export PATH="/root/.hermes/tools/node-26.7.0-linux-x64/bin:$PATH"

# ---- PER-WORKTREE LOCK (card t_d101a943) ----------------------------------
#
# The key is `git rev-parse --absolute-git-dir`: one value per worktree, the same
# value from every subdirectory of it, and DIFFERENT for the primary tree and for
# each linked worktree (a linked worktree resolves to `<repo>/.git/worktrees/<name>`,
# the primary tree to `<repo>/.git`). `--show-toplevel` would name the same worktree
# too, but it is resolved against the CWD, so a run started from `web/` would derive
# a different key for the same tree and the lock would not be mutual.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore web/next.config.mjs under" >&2
    echo "each other's feet (t_d101a943, reproduced). Install flock before trusting any" >&2
    echo "verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised pair of runs corrupts next.config.mjs (t_d101a943)." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR, so two runs in" >&2
    echo "one worktree could not be serialised. Set BITEPROOF_LOCK_DIR to a writable dir." >&2
    exit 2
  }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || {
    echo "BITEPROOF REFUSED: cannot open the lock file $LOCK_FILE." >&2
    exit 2
  }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   another bite harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (they mutate the same web/ config, so concurrently they corrupt it)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer mutating the same web/next.config.mjs" >&2
    echo "and would either read its mutation or overwrite the config with its own backup." >&2
    echo "Nothing was mutated. Re-run once the other run finishes, or raise the wait with" >&2
    echo "BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held" >&2
    echo "and this run would not be serialised against a peer in the same worktree." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi

# The child. Refuse if the lock it is supposed to have inherited cannot be shown to
# be held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same web/next.config.mjs would give it false" >&2
  echo "verdicts and could be left restoring this run's bytes. Unset BITEPROOF_LOCKED and let" >&2
  echo "the harness take the lock itself." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here — it would read REFUSED for any file." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

# CAPABILITY, not presence (card t_b014807f): `node --version` succeeding proves nothing
# here — v18.19.1 answers it and then rejects `--disable-warning`, and every leg below
# would read that empty capture as a verdict. The gate runs the invocation this harness
# depends on and refuses, naming the node, when that produces no test-runner summary.
nt_require_node

for f in "$CFG" "$GUARD"; do
  [ -f "$f" ] || { echo "missing $f — is this the right worktree?" >&2; exit 2; }
done

BEFORE=$(sha256sum "$CFG" | cut -d' ' -f1)
# PER-RUN, so one run can never restore another run's bytes (card t_d101a943).
BAK=$(mktemp) || { echo "cannot create a per-run backup file" >&2; exit 2; }
cp -f "$CFG" "$BAK"
# The guard's output channel, also per-run: nt_capture truncates it before every run, so
# a stale capture cannot back a verdict (card t_b014807f).
CAP=$(mktemp) || { echo "cannot create a per-run capture file" >&2; exit 2; }

# run_guard -> prints GREEN, RED or REFUSED; the output lands in $CAP.
run_guard() { nt_capture "$CAP" "$GUARD"; }
restore()  { cp -f "$BAK" "$CFG"; }

fail=0
# ONE trap, carrying two properties the shipped version did not have: the config is
# restored on EVERY exit (an interrupt, or an abort between a mutation and its
# `restore`, used to leave the mutation on disk), and the per-run temp files are
# always removed — no run can leave either a mutation or debris behind.
cleanup() {
  local rc=$?
  restore 2>/dev/null
  rm -f "$BAK" "$CAP" "$CAP.rc"
  exit "$rc"
}
trap cleanup EXIT

expect_red() { # $1 = case name, $2 = verdict (GREEN | RED | REFUSED)
  # A capture with no runner output is NOT "the guard did not bite" — it is a refusal
  # (card t_b014807f). It exits 2 and names the node, rather than printing a verdict
  # about a guard that never ran.
  if [ "$2" = REFUSED ]; then nt_refuse_if "$1" "$CAP"; fi
  # HERE-STRING, never `printf '%s' "$2" | grep -q`: under `set -o pipefail` a MATCH
  # can still report the pipeline as failed (SIGPIPE 141) and print a false
  # `DID NOT BITE`. See the header for the measurement.
  if [ "$2" = RED ]; then
    echo "  ✅ BITES — $1 turned the guard RED"
  else
    echo "  ❌ DID NOT BITE — $1 left the guard GREEN (the guard is not protecting this)"
    fail=1
  fi
}

echo "case 0: baseline (the unsabotaged config must be GREEN)"
out=$(run_guard); nt_refuse_if "the baseline guard run" "$CAP"
grep -E '^ℹ (pass|fail)' "$CAP"
if [ "$out" = GREEN ]; then echo "  ✅ baseline green"; else echo "  ❌ baseline not green"; fail=1; fi

echo "case 1: drop allowedDevOrigins entirely (the fix removed)"
python3 - "$CFG" <<'PY'
import re,sys
p=sys.argv[1]; s=open(p).read()
s=s.replace("  allowedDevOrigins: DEV_HOSTS,\n","")
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "no allowedDevOrigins" "$out"; restore

echo "case 2: dev header forced back to immutable (the caching defect restored)"
python3 - "$CFG" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
s=s.replace('process.env.NODE_ENV === "development"','false')
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "immutable in dev" "$out"; restore

echo "case 3: drop the hostname validation (the silent no-op restored)"
python3 - "$CFG" <<'PY'
import re,sys
p=sys.argv[1]; s=open(p).read()
s=re.sub(r'for \(const host of DEV_HOSTS\) \{.*?\n\}\n', '', s, flags=re.S)
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "no validation" "$out"; restore

echo "case 4: bake the staging hostname in as a literal default (the per-env drift restored)"
python3 - "$CFG" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
s=s.replace('(process.env.JOMFORM_DEV_HOSTS || "")','(process.env.JOMFORM_DEV_HOSTS || "jomform-staging.example.com")')
open(p,'w').write(s)
PY
out=$(run_guard); expect_red "baked-in literal" "$out"; restore

AFTER=$(sha256sum "$CFG" | cut -d' ' -f1)
echo
if [ "$BEFORE" = "$AFTER" ]; then echo "config restored intact ($AFTER)"; else echo "❌ CONFIG NOT RESTORED"; fail=1; fi
exit $fail
