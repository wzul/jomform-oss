#!/usr/bin/env python3
"""Bite proof for api/internal/config/ses_replyto_wiring_guard_test.go.

For each mutation: apply, confirm `go build ./...` still succeeds (the mutation
is SILENT to the compiler - which is the whole reason the guard exists), run the
FULL config package, then restore.

Restore is signal-safe: originals are held in memory, restored in a finally, and
a SIGTERM/SIGINT handler restores before exiting, so a timeout kill cannot leave
the worktree mutated. Every restore is verified by sha256 rather than asserted.
"""
import atexit
import hashlib
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MAIN = os.path.join(API, "cmd/jomform-api/main.go")
CONFIG = os.path.join(API, "internal/config/config.go")
TARGETS = [MAIN, CONFIG]
DONE = False


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


atexit.register(lambda: None if DONE else restore_all())


def _sig(signum, _frame):
    restore_all()
    print("signal %d - originals restored, exiting" % signum, flush=True)
    sys.exit(130)


signal.signal(signal.SIGTERM, _sig)
signal.signal(signal.SIGINT, _sig)


def run(cmd):
    p = subprocess.run(cmd, cwd=API, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


AUTH_REPLYTO = (
    "\t\t\t// The monitored mailbox a Reply reaches. Carried by the sender, not\n"
    "\t\t\t// by a call site, so every message gets it (a Reply to a message\n"
    "\t\t\t// with no Reply-To goes to noreply@jomform.com, where nothing\n"
    "\t\t\t// reads it).\n"
    "\t\t\tReplyTo: cfg.SESReplyTo,\n"
)

PAY_REPLYTO = (
    "\t\t\t\t// Same reply-handling rule as the auth mailer above: every\n"
    "\t\t\t\t// message this sender emits carries the monitored Reply-To.\n"
    "\t\t\t\tReplyTo: cfg.SESReplyTo,\n"
)

MUTATIONS = [
    ("A1: delete the AUTH mailer's ReplyTo wiring line", MAIN, AUTH_REPLYTO, ""),
    ("A2: delete the PAYMENTS receipt sender's ReplyTo wiring line", MAIN, PAY_REPLYTO, ""),
    ("B: SES_REPLY_TO falls back to SES_FROM (a plausible address nobody reads)",
     CONFIG, 'SESReplyTo: parseEmailList(os.Getenv("SES_REPLY_TO")),',
     'SESReplyTo: parseEmailList(os.Getenv("SES_FROM")),'),
    ("C: the env var name is misspelled, so the value never arrives",
     CONFIG, 'os.Getenv("SES_REPLY_TO")', 'os.Getenv("SES_REPLY_TO_TYPO")'),
    ("D: parseEmailList stops lowercasing (mixed case reaches the header)",
     CONFIG, 'if e := strings.ToLower(strings.TrimSpace(part)); e != "" {',
     'if e := strings.TrimSpace(part); e != "" {'),
]


def main():
    # Prewarm so per-mutation builds are incremental, not cold.
    run(["go", "build", "./..."])

    brc, _ = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", "./internal/config/", "-count=1"])
    print("=== CONTROL (unmutated): go build ./... rc=%d, full config package rc=%d ===" % (brc, trc), flush=True)
    if brc != 0 or trc != 0:
        print("CONTROL NOT GREEN - fix the guard before proving it bites", flush=True)
        print(tout[-3000:], flush=True)
        return 1

    not_biting = []
    for label, path, old, new in MUTATIONS:
        data = ORIG[path].decode()
        if data.count(old) != 1:
            print("  %-68s SKIP: anchor matched %d times" % (label, data.count(old)), flush=True)
            not_biting.append(label)
            continue
        with open(path, "wb") as fh:
            fh.write(data.replace(old, new).encode())

        b, _ = run(["go", "build", "./..."])
        tr, tout = run(["go", "test", "./internal/config/", "-count=1"])
        bites = tr != 0
        if not bites:
            not_biting.append(label)
        print("  %-68s build rc=%d  full-pkg test rc=%d  -> %s"
              % (label, b, tr, "RED (bites)" if bites else "GREEN (DOES NOT BITE)"), flush=True)
        if b != 0:
            print("      NOTE: mutation ALSO broke the build (rc=%d) - a compile error "
                  "would hide the guard" % b, flush=True)
        for line in tout.strip().splitlines():
            if line.startswith("---") or "ses_replyto_wiring" in line:
                print("      " + line.strip(), flush=True)

        with open(path, "wb") as fh:
            fh.write(ORIG[path])

    print(flush=True)
    for p in TARGETS:
        after = sha(p)
        print("restored %-12s sha256 %s %s" % (
            os.path.basename(p), after[:16], "OK" if after == BEFORE[p] else "MISMATCH"), flush=True)
    rc, _ = run(["go", "test", "./internal/config/", "-count=1"])
    rcb, _ = run(["go", "build", "./..."])
    print("final unmutated: config package rc=%d, go build ./... rc=%d" % (rc, rcb), flush=True)
    print("MUTATIONS THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    return 1 if (not_biting or rc or rcb) else 0


if __name__ == "__main__":
    code = main()
    DONE = True
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
