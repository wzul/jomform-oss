#!/usr/bin/env python3
"""Bite proof for the GET /v1/auth/verify failure-response guards.

The guards live in api/internal/httpapi/verify_fail_page_test.go (card
t_8c67149d). They pin one thing: a GET on a bad/expired verify link answers a
localized HTML page, while POST keeps the machine JSON envelope.

Each mutation below removes ONE property of that behaviour and requires the
named guard to go RED. Per mutation the harness checks (a) the sabotage really
landed on disk before the test ran, (b) the mutation still COMPILES (a compile
error would hide the guard, not prove it), (c) which guard failed, and (d) that
the restore happened, verified by sha256 rather than asserted.

Restore is signal-safe: originals are held in memory, restored in a finally and
by a SIGTERM/SIGINT handler, so a timeout kill cannot leave the worktree
mutated.

Run:  python3 api/scripts/biteproof_verify_fail_page.py
      (needs a Go toolchain; no database, no Valkey, no network.)
"""
import hashlib
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
VERIFY = os.path.join(API, "internal/httpapi/verify.go")
TARGETS = [VERIFY]
DONE = False

# The guard package and the guard test names this harness can attribute a
# failure to. Every name here must exist in verify_fail_page_test.go, so a
# renamed guard fails this harness loudly instead of silently attributing.
GUARD_PKG = "./internal/httpapi/"
GUARDS = [
    "TestVerifyGetFailureRendersLocalizedPage",
    "TestVerifyGetNoTokenRendersPage",
    "TestVerifyGetFailurePageExceedsBrowserThreshold",
    "TestVerifyGetFailureSaysWhichClassItIs",
    "TestVerifyPostKeepsJSONEnvelope",
    "TestVerifyUnconfiguredKeyStaysJSONOnGet",
]


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


ORIG = {p: open(p, "rb").read() for p in TARGETS}
BEFORE = {p: sha(p) for p in TARGETS}


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


def run(args):
    p = subprocess.run(args, cwd=API, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


# The one method-split block in verifyFail. Removing it restores the exact
# pre-fix behaviour: every failure, GET included, answers the JSON envelope.
METHOD_SPLIT = (
    "\tif r.Method != http.MethodGet {\n"
    "\t\twriteErr(w, status, wireMsg)\n"
    "\t\treturn\n"
    "\t}\n"
    "\twriteVerifyFailPage(w, lang, a.loginURL(), class)"
)
NO_SPLIT = (
    "\twriteErr(w, status, wireMsg)\n"
    "\t_ = lang\n"
    "\t_ = class"
)

MUTATIONS = [
    # 1. The card's own defect, re-introduced: GET goes back to the envelope.
    ("A: verifyFail answers the envelope for every method (the original bug)",
     METHOD_SPLIT, NO_SPLIT,
     ["TestVerifyGetFailureRendersLocalizedPage", "TestVerifyGetNoTokenRendersPage"]),
    # 2. The page stops being localized: every locale renders the English
    #    sentence, which is the #6177 failure mode on the surface that reaches
    #    a human directly.
    ("B: every locale renders the English sentence",
     '\ttext, ok := verifyFailText[lang]\n'
     '\tif !ok {\n'
     '\t\ttext = verifyFailText["en"]\n'
     '\t\tlang = "en"\n'
     '\t}',
     '\ttext := verifyFailText["en"]\n'
     '\t_ = lang',
     ["TestVerifyGetFailureRendersLocalizedPage"]),
    # 3. The three failure classes collapse onto one sentence — the caller can
    #    no longer tell a stale link from a mangled URL.
    ("C: every failure class renders the SAME sentence",
     "\tcase verifyFailUnknown:\n\t\treturn c.unknown\n\tcase verifyFailMissing:\n\t\treturn c.missing\n",
     "",
     ["TestVerifyGetFailureSaysWhichClassItIs"]),
    # 4. Arabic loses its direction, so RTL prose renders in an LTR document.
    ("D: Arabic renders without dir=rtl",
     'func verifyFailDir(lang string) string {\n\tif lang == "ar" {\n\t\treturn "rtl"\n\t}\n\treturn "ltr"\n}',
     'func verifyFailDir(lang string) string {\n\t_ = lang\n\treturn "ltr"\n}',
     ["TestVerifyGetFailureRendersLocalizedPage"]),
    # 5. The page is stripped down until the body falls under 512 bytes — the
    #    realistic shape of this regression is an author trimming the inline
    #    styles ("the page does not need CSS"), and Chrome would then discard
    #    the page and show its own error instead. Measured headroom on the real
    #    page: zh (the shortest locale) is 749 bytes, i.e. 237 over the floor,
    #    so this mutation must remove more than that. Removing the head tags
    #    alone (~100 bytes) does NOT cross it — tried, and correctly reported
    #    GREEN (does not bite) rather than counted as a proof.
    ("E: the page is stripped under the 512-byte floor",
     '<!doctype html><html lang="%s" dir="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>JomForm</title></head>\n'
     '<body style="font-family:system-ui,sans-serif;background:#0b1020;color:#e6e9ef;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0">\n'
     '<div style="text-align:center;max-width:480px;padding:32px">\n'
     '<h1 style="color:#5e6ad2">JomForm</h1>',
     '<!doctype html><html lang="%s" dir="%s"><head></head><body><div><h1>JomForm</h1>',
     ["TestVerifyGetFailurePageExceedsBrowserThreshold"]),
    # 6. POST is dragged onto the page too — the app/API half of the contract
    #    is broken in the other direction.
    ("F: POST renders the HTML page as well",
     "\tif r.Method != http.MethodGet {\n\t\twriteErr(w, status, wireMsg)\n\t\treturn\n\t}",
     "",
     ["TestVerifyPostKeepsJSONEnvelope"]),
]


def main():
    # Prewarm so the per-mutation test builds are incremental, not cold.
    run(["go", "build", "./..."])

    declared = [g for g in GUARDS
                if g in open(os.path.join(API, "internal/httpapi/verify_fail_page_test.go")).read()]
    if len(declared) != len(GUARDS):
        missing = sorted(set(GUARDS) - set(declared))
        print("HARNESS BUG: guard(s) not declared in verify_fail_page_test.go: %s" % missing,
              flush=True)
        print("This harness cannot attribute a failure it cannot name.", flush=True)
        return 2

    brc, _ = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", GUARD_PKG, "-run", "TestVerify", "-count=1"])
    print("=== CONTROL (unmutated): go build rc=%d, TestVerify* rc=%d ===" % (brc, trc), flush=True)
    if brc != 0 or trc != 0:
        print("CONTROL NOT GREEN - fix the guard before proving it bites", flush=True)
        print(tout[-3000:], flush=True)
        return 1

    not_biting = []
    for label, old, new, wants in MUTATIONS:
        data = ORIG[VERIFY].decode()
        if data.count(old) != 1:
            print("  %-64s SKIP: anchor matched %d times" % (label, data.count(old)), flush=True)
            not_biting.append(label)
            continue
        mutated = data.replace(old, new)
        if mutated == data:
            print("  %-64s SKIP: no-op mutation" % label, flush=True)
            not_biting.append(label)
            continue
        with open(VERIFY, "wb") as fh:
            fh.write(mutated.encode())

        # (a) the sabotage is really on disk for the run about to happen.
        on_disk = open(VERIFY).read()
        if old in on_disk or new not in on_disk:
            print("  %-64s HARNESS BUG: mutation not on disk" % label, flush=True)
            not_biting.append(label)
            with open(VERIFY, "wb") as fh:
                fh.write(ORIG[VERIFY])
            continue

        b, _ = run(["go", "build", "./..."])
        tr, tout = run(["go", "test", GUARD_PKG, "-run", "TestVerify", "-count=1"])

        # (c) WHICH guard noticed.
        failed = sorted({line.split()[2].split("/")[0]
                         for line in tout.splitlines()
                         if line.startswith("--- FAIL:")})
        caught = [g for g in wants if g in failed]
        bites = tr != 0 and len(caught) == len(wants)
        if not bites:
            not_biting.append(label)
        print("  %-64s build rc=%d  TestVerify* rc=%d  -> %s"
              % (label, b, tr, "RED (bites)" if bites else "GREEN (DOES NOT BITE)"), flush=True)
        print("      wanted: %s" % ", ".join(wants), flush=True)
        print("      failed: %s" % (", ".join(failed) or "(none)"), flush=True)
        if b != 0:
            print("      NOTE: mutation ALSO broke the build (rc=%d) - a compile error "
                  "would hide the guard" % b, flush=True)

        with open(VERIFY, "wb") as fh:
            fh.write(ORIG[VERIFY])

    print(flush=True)
    for p in TARGETS:
        after = sha(p)
        print("restored %-10s sha256 %s %s" % (
            os.path.basename(p), after[:16], "OK" if after == BEFORE[p] else "MISMATCH"), flush=True)
    rc, _ = run(["go", "test", GUARD_PKG, "-run", "TestVerify", "-count=1"])
    rcb, _ = run(["go", "build", "./..."])
    print("final unmutated: TestVerify* rc=%d, go build rc=%d" % (rc, rcb), flush=True)
    print("MUTATIONS THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    return 1 if (not_biting or rc or rcb) else 0


if __name__ == "__main__":
    code = main()
    DONE = True
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
