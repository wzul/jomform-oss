#!/usr/bin/env python3
"""Bite proof for the mode-change notification guards.

Guards under proof (all in api/internal/dbstore/form_notification_mode_change_test.go):

  TestModeChangeToResponseRePointsTheUneditedDefault
  TestModeChangeBackToPaymentRePointsTheDefault
  TestModeChangeLeavesAMerchantEditedDefaultAlone
  TestModeChangeLeavesReAddressedDefaultAlone
  TestModeChangeLeavesAMerchantAuthoredRuleAlone
  TestModeChangeKeepsAnOptedOutFormSilent
  TestModeChangeIsIdempotentAndQuietWhenAlreadyCorrect

THE DEFECT (card t_a58848bd, found by QA on t_f37d710d): EnsureDefault picks a
form's default notification event from the form's mode AT INSERT TIME ONLY, so a
form switched to response mode after creation keeps the sale.paid default it was
born with. FireFormNotifications matches zero rows for response.submitted and the
merchant is never told anyone responded. The fix reconciles the UNEDITED default
rule onto the form's current mode inside FormStore.Update (the one write path that
can move a mode), touching only a row that is is_default AND still carries the
system's exact bytes for the other mode's default (same contract as 000062).

WHAT EACH CASE PROVES. One mutation at a time, each a COMPILING revert of a real
part of the fix, then the FULL dbstore package (never a single -run), and the red
must NAME the guard that caught it. The mutations are the plausible wrong fixes,
not arbitrary breakage:

  A  Remove the reconcile call from FormStore.Update  - byte for byte the carded
     defect: the mode-aware choice runs only at creation.
  B  ReconcileDefault returns without writing       - a silent no-op that still
     compiles and still "exists" at the call site.
  C  Drop the is_default predicate                  - a merchant-AUTHORED rule is
     seized (a rule he wrote himself is not the form's default).
  D  Drop the template-match predicate              - a merchant-EDITED default is
     overwritten, which is worse than the bug it fixes: his words go out.
  E  Drop the to_email predicate                    - a default re-addressed to
     the merchant's own inbox is taken away from it.

The CONTROL (unmutated tree) must be GREEN over the full package first, or every
red below is unmeasurable. Two refusals are built in, both measured rather than
assumed: an unset JOMFORM_TEST_DATABASE_URL exits 2 (the DB-backed guards would
otherwise SKIP and a skip reads as green), and a missing mutation anchor exits 2
naming it (a mutation that never landed is a silent green).

Restore is signal-safe: originals are held in memory, written back in a finally,
and a SIGTERM/SIGINT handler restores before exiting, so a timeout kill cannot
leave the worktree mutated. Every restore is verified by sha256.

Run:  python3 api/scripts/biteproof_form_notification_mode_change.py
"""
import hashlib
import os
import signal
import subprocess
import sys

sys.dont_write_bytecode = True

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FORM_GO = os.path.join(API, "internal/dbstore/form.go")
NOTIF_GO = os.path.join(API, "internal/dbstore/form_notification.go")
TARGETS = [FORM_GO, NOTIF_GO]
PKG = "./internal/dbstore/"

# The guards this harness exists to prove. A red that names none of these is not
# evidence about this fix, so it is reported as unattributable rather than counted.
GUARDS = [
    "TestModeChangeToResponseRePointsTheUneditedDefault",
    "TestModeChangeBackToPaymentRePointsTheDefault",
    "TestModeChangeLeavesAMerchantEditedDefaultAlone",
    "TestModeChangeLeavesReAddressedDefaultAlone",
    "TestModeChangeLeavesAMerchantAuthoredRuleAlone",
    "TestModeChangeKeepsAnOptedOutFormSilent",
    "TestModeChangeIsIdempotentAndQuietWhenAlreadyCorrect",
]


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


ORIG = {p: open(p, "rb").read() for p in TARGETS}
BEFORE = {p: sha(p) for p in TARGETS}
DONE = False


def restore_all():
    global DONE
    for p, data in ORIG.items():
        with open(p, "wb") as fh:
            fh.write(data)
        if sha(p) != BEFORE[p]:
            print("!! RESTORE FAILED for %s" % p, flush=True)
    DONE = True


def _sig(signum, _frame):
    restore_all()
    print("signal %d - originals restored, exiting" % signum, flush=True)
    sys.exit(130)


signal.signal(signal.SIGTERM, _sig)
signal.signal(signal.SIGINT, _sig)

# The early return every mutation below keeps the function compiling past: put
# the no-op right after the nil guard, so the rest of the body is unreachable.
NOOP_ANCHOR = """	wantName, wantEvent, wantSubject, wantBody := defaultNotificationFor(FormMode(f))
"""

# form.go: keep the post-write READ (and consume `f`, so the mutation compiles)
# but drop the reconcile CALL — byte for byte the carded defect: the mode-aware
# choice runs only at creation.
A_ANCHOR = (
    "	if f, gerr := s.GetByID(ctx, accountID, id); gerr == nil {\n"
    "		if _, rerr := s.notifications.ReconcileDefault(ctx, f); rerr != nil {\n"
)
A_REPLACED = (
    "	if f, gerr := s.GetByID(ctx, accountID, id); gerr == nil {\n"
    "		_ = f\n"
    "		if rerr := error(nil); rerr != nil {\n"
)

MUTATIONS = [
    (
        "A: the reconcile call is removed from FormStore.Update (the carded defect shape: mode-aware choice only at creation)",
        FORM_GO,
        A_ANCHOR,
        A_REPLACED,
        ["TestModeChangeToResponseRePointsTheUneditedDefault"],
    ),
    (
        "B: ReconcileDefault becomes a silent no-op (it exists at the call site and does nothing)",
        NOTIF_GO,
        NOOP_ANCHOR,
        "	return false, nil\n" + NOOP_ANCHOR,
        ["TestModeChangeToResponseRePointsTheUneditedDefault"],
    ),
    (
        "C: the is_default predicate is dropped, so a merchant-AUTHORED rule is seized",
        NOTIF_GO,
        "		    AND is_default=true\n",
        "",
        ["TestModeChangeLeavesAMerchantAuthoredRuleAlone"],
    ),
    (
        "D: the subject/body half of the template-match predicate is dropped, so a merchant-EDITED default is overwritten",
        NOTIF_GO,
        "		    AND name=$7 AND event=$8 AND subject=$9 AND body=$10\n",
        "		    AND name=$7 AND event=$8 AND (subject=$9 OR $9 IS NOT NULL)"
        " AND (body=$10 OR $10 IS NOT NULL)\n",
        [
            "TestModeChangeLeavesAMerchantEditedDefaultAlone",
            "TestModeChangeLeavesReAddressedDefaultAlone",
        ],
    ),
    (
        "E: the to_email predicate is dropped, so a re-addressed default is taken from the inbox the merchant chose",
        NOTIF_GO,
        "		    AND COALESCE(to_email,'')=$6\n",
        "		    AND (COALESCE(to_email,'')=$6 OR $6 IS NOT NULL)\n",
        ["TestModeChangeLeavesReAddressedDefaultAlone"],
    ),
]


def run(cmd):
    p = subprocess.run(cmd, cwd=API, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


def guard_output_lines(out):
    """The `--- FAIL: <name>` markers, i.e. the guards that actually failed."""
    names = []
    for line in out.splitlines():
        s = line.strip()
        if s.startswith("--- FAIL:"):
            head = s.split(":", 1)[1].strip().split(" ")[0]
            if head:
                names.append(head)
    return names


def main():
    if not os.environ.get("JOMFORM_TEST_DATABASE_URL"):
        print("REFUSING: JOMFORM_TEST_DATABASE_URL is unset. Every guard under proof is DB-backed, "
              "so without it they SKIP - and a skipped guard reads as a green one. Source "
              "${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh} first.", flush=True)
        return 2

    run(["go", "build", "./..."])
    brc, bout = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", PKG, "-count=1"])
    print("=== CONTROL (unmutated): go build ./... rc=%d, full dbstore package rc=%d ===" % (brc, trc),
          flush=True)
    if brc != 0 or trc != 0:
        print("CONTROL NOT GREEN - fix the tree before proving the guard bites", flush=True)
        print(bout[-3000:], flush=True)
        print(tout[-6000:], flush=True)
        return 2

    not_biting = []
    unattributable = []
    for label, path, old, new, want_guards in MUTATIONS:
        data = ORIG[path].decode()
        hits = data.count(old)
        if hits != 1:
            print("REFUSING: %s - the anchor matched %d times in %s, expected 1. A mutation that "
                  "never landed is a silent green." % (label, hits, os.path.basename(path)), flush=True)
            restore_all()
            return 2
        mutation = data.replace(old, new)
        with open(path, "wb") as fh:
            fh.write(mutation.encode())
        # Prove the mutation is on disk, not merely written.
        if new not in open(path).read():
            print("REFUSING: %s - the mutation is not on disk after the write" % label, flush=True)
            restore_all()
            return 2

        b, _ = run(["go", "build", "./..."])
        tr, tout = run(["go", "test", PKG, "-count=1"])
        failed = guard_output_lines(tout)
        named = [g for g in want_guards if g in failed]
        bites = tr != 0
        if not bites:
            not_biting.append(label)
        elif not named:
            unattributable.append(label)
        print("  %-104s build rc=%d  full-pkg rc=%d  -> %s" % (
            label, b, tr,
            "RED (bites: %s)" % ",".join(named) if bites and named
            else ("RED but names NO expected guard" if bites else "GREEN (DOES NOT BITE)")), flush=True)
        if b != 0:
            print("      NOTE: the mutation ALSO broke the build (rc=%d) - a compile error would hide "
                  "the guard" % b, flush=True)
        for line in tout.strip().splitlines():
            if "--- FAIL" in line or "mode-change" in line or "can never fire" in line:
                print("      " + line.strip(), flush=True)

        with open(path, "wb") as fh:
            fh.write(ORIG[path])

    print(flush=True)
    for p in TARGETS:
        after = sha(p)
        print("restored %-24s sha256 %s %s" % (
            os.path.basename(p), after[:16], "OK" if after == BEFORE[p] else "MISMATCH"), flush=True)
    rc, _ = run(["go", "test", PKG, "-count=1"])
    rcb, _ = run(["go", "build", "./..."])
    print("final unmutated: dbstore package rc=%d, go build ./... rc=%d" % (rc, rcb), flush=True)
    print("MUTATIONS THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    print("MUTATIONS WHOSE RED NAMED NO EXPECTED GUARD: %d %s" % (len(unattributable), unattributable),
          flush=True)
    return 1 if (not_biting or unattributable or rc or rcb) else 0


if __name__ == "__main__":
    code = main()
    DONE = True
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
