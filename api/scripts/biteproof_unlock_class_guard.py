#!/usr/bin/env python3
"""Bite proof for the unlock CLASS guard (api/internal/httpapi/unlock_ratelimit_class_test.go).

The guard in that file (TestEveryUnlockRouteIsLimitedAtTheRouter) pins one thing:
EVERY `.../unlock` pattern the router holds answers a concurrent password burst
with a 429 — i.e. carries a limiter — whatever spelling registered it.

Card t_b2888b6c exists because the guard's FIRST version was a single-line text
regex over `mux.Handle` call sites, so a route registered through a helper or a
variable was invisible to it and the guard reported GREEN while an unlimited
password oracle was live. Every sabotage anyone had tried used the LITERAL shape
the regex was written against, which is why the hole stayed hidden. So the
headline case here is the INDIRECT shape: it is the defect, and a harness that
proved only the literal shape would be the very gap this card is about.

Per case the harness checks, and prints, all of:
  (a) the mutation really is on disk before the test runs (a sabotage that did
      not land proves nothing),
  (b) `go build ./...` is OK, so a compile error is not what made the guard
      fail (a "compiling revert" is the only honest sabotage),
  (c) the guard really went RED where RED is wanted, and GREEN where the case is
      a control (a wrapped route must NOT fail),
  (d) the failure NAMES the offending route — a red that does not say which
      route is unlimited is not evidence about this defect,
  (e) every touched file is restored byte-identically, verified by sha256, and a
      file the case created is gone again.

Restore is signal-safe: originals are held in memory and restored in a finally
and by a SIGTERM/SIGINT handler, so a timeout kill cannot leave the worktree
mutated.

Run:  python3 api/scripts/biteproof_unlock_class_guard.py
      (needs a Go toolchain; NO database, NO Valkey, NO network — the limiter
       consumes its token before the wrapped handler runs, so the bare handlers
       the guard builds are enough to observe the throttle.)
"""
import hashlib
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PUBLIC = os.path.join(API, "internal/httpapi/public.go")
# The fifth case registers its route from ANOTHER file in the package, so the
# walk is proved to see a registration the reworked guard's predecessor could
# not (it read one file's call sites one line at a time).
XFILE = os.path.join(API, "internal/httpapi/zz_qa_indirect_route.go")

GUARD_PKG = "./internal/httpapi/"
GUARD = "TestEveryUnlockRouteIsLimitedAtTheRouter"

# Every existing file the cases mutate, so they can be held and restored as a set.
MUTABLE = [PUBLIC]


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def exists(path):
    return os.path.exists(path)


ORIG = {}
BEFORE = {}
for _p in MUTABLE:
    with open(_p, "rb") as _fh:
        ORIG[_p] = _fh.read()
    BEFORE[_p] = sha(_p)


def restore_all():
    for p, data in ORIG.items():
        with open(p, "wb") as fh:
            fh.write(data)
    if exists(XFILE):
        os.remove(XFILE)


def _sig(signum, _frame):
    restore_all()
    print("signal %d - originals restored, exiting" % signum, flush=True)
    sys.exit(130)


signal.signal(signal.SIGTERM, _sig)
signal.signal(signal.SIGINT, _sig)


def run(args):
    p = subprocess.run(args, cwd=API, capture_output=True, text=True, timeout=1800)
    return p.returncode, (p.stdout + p.stderr)


# ─── the anchors ─────────────────────────────────────────────────────────────
# The last production registration in RegisterRoutes. Everything is injected
# after it so the injected route sits inside the same RegisterRoutes body the
# server serves, exactly as a real new unlock route would be added.
LAST_REG = (
    '\tmux.Handle("POST /{slug}/unlock", middleware.RateLimit(http.HandlerFunc(h.handleFormUnlock)))\n'
    '}\n'
)
UNWRAP_ONE_SHIPPED = (
    '\tmux.Handle("POST /f/{slug}/unlock", middleware.RateLimit(http.HandlerFunc(h.handleFormUnlock)))\n'
)
WRAP_ONE_SHIPPED = (
    '\tmux.HandleFunc("POST /f/{slug}/unlock", h.handleFormUnlock)\n'
)

# The indirection that defeated the text regex: the PATTERN is a variable at the
# call site and the actual mux call lives in a helper, so no line of production
# source contains `mux.Handle(".../unlock"` for this route.
HELPER = (
    '\n'
    '// registerUnlockRoute registers an unlock route through a helper — the shape\n'
    '// the pre-t_b2888b6c text-regex class guard could not see.\n'
    'func registerUnlockRoute(mux *http.ServeMux, pattern string, h http.Handler) {\n'
    '\tmux.Handle(pattern, h)\n'
    '}\n'
)
INDIRECT_UNWRAPPED = LAST_REG[:-2] + (
    '\t// QA probe: an unlock route registered INDIRECTLY (no literal mux.Handle\n'
    '\t// with a "/unlock" pattern), UNWRAPPED — the old guard\'s blind spot.\n'
    '\tregisterUnlockRoute(mux, "POST /qa-indirect/{slug}/unlock", http.HandlerFunc(h.handleFormUnlock))\n'
    '}\n'
) + HELPER
INDIRECT_WRAPPED = LAST_REG[:-2] + (
    '\t// QA probe: the same INDIRECT registration, WRAPPED. The control: this one\n'
    '\t// must leave the guard GREEN.\n'
    '\tregisterUnlockRoute(mux, "POST /qa-indirect/{slug}/unlock", middleware.RateLimit(http.HandlerFunc(h.handleFormUnlock)))\n'
    '}\n'
) + HELPER
LITERAL_UNWRAPPED = LAST_REG[:-2] + (
    '\t// QA probe: a LITERAL unwrapped unlock route — the shape the old guard did\n'
    '\t// catch. The reworked guard must not have regressed on it.\n'
    '\tmux.HandleFunc("POST /qa-literal/{slug}/unlock", h.handleFormUnlock)\n'
    '}\n'
)

# Cross-file: the registration lives in a file this guard's predecessor never
# read, reached from RegisterRoutes through one call.
XF_HANDLER = (
    'package httpapi\n'
    '\n'
    '// QA probe (created by api/scripts/biteproof_unlock_class_guard.py): an unlock\n'
    '// route registered INDIRECTLY, from ANOTHER FILE in the package.\n'
    'import "net/http"\n'
    '\n'
    'func qaRegisterIndirectUnlockFromAnotherFile(mux *http.ServeMux, h *PublicHandlers) {\n'
    '\tregisterUnlockRoute(mux, "POST /qa-xfile/{slug}/unlock", http.HandlerFunc(h.handleFormUnlock))\n'
    '}\n'
)
XF_CALL = LAST_REG[:-2] + (
    '\t// QA probe: an unlock route registered from ANOTHER FILE, indirectly.\n'
    '\tqaRegisterIndirectUnlockFromAnotherFile(mux, h)\n'
    '}\n'
) + HELPER

# (label, mutations, want_red, want_named)
#   mutations: list of ("replace", path, old, new) | ("create", path, content)
#   want_red:  True  -> the guard MUST fail (the sabotage is a real defect)
#              False -> the guard MUST pass (a control; catching it here would
#                       mean the guard reports a limit that is not there)
#   want_named: substrings the guard's output must contain when it is red, so a
#               red is attributable to THIS route and not to a sibling
CASES = [
    (
        "A: INDIRECT unwrapped unlock route (the card's defect)",
        [("replace", PUBLIC, LAST_REG, INDIRECT_UNWRAPPED)],
        True, ["POST /qa-indirect/{slug}/unlock"],
    ),
    (
        "B: the same INDIRECT route WRAPPED (control: must stay GREEN)",
        [("replace", PUBLIC, LAST_REG, INDIRECT_WRAPPED)],
        False, [],
    ),
    (
        "C: a LITERAL unwrapped unlock route (what the old guard DID catch)",
        [("replace", PUBLIC, LAST_REG, LITERAL_UNWRAPPED)],
        True, ["POST /qa-literal/{slug}/unlock"],
    ),
    (
        "D: a SHIPPED route loses its wrapper (bites on production code)",
        [("replace", PUBLIC, UNWRAP_ONE_SHIPPED, WRAP_ONE_SHIPPED)],
        True, ["POST /f/{slug}/unlock"],
    ),
    (
        "E: an INDIRECT route registered from ANOTHER FILE in the package",
        [("replace", PUBLIC, LAST_REG, XF_CALL), ("create", XFILE, XF_HANDLER)],
        True, ["POST /qa-xfile/{slug}/unlock"],
    ),
]


def apply_mutations(mutations):
    """Apply every mutation, verifying each really landed. Returns an error string."""
    for m in mutations:
        if m[0] == "replace":
            _, path, old, new = m
            src = open(path).read()
            if src.count(old) != 1:
                return "anchor %r occurs %d times in %s" % (old[:60], src.count(old), path)
            open(path, "w").write(src.replace(old, new))
            on_disk = open(path).read()
            if new not in on_disk or old in on_disk:
                return "replacement did not land in %s" % path
        elif m[0] == "create":
            _, path, content = m
            if exists(path):
                return "%s already exists — refusing to overwrite" % path
            open(path, "w").write(content)
            if open(path).read() != content:
                return "created file %s does not match" % path
        else:
            return "unknown mutation kind %r" % (m[0],)
    return ""


def main():
    # Prewarm so the per-case builds are incremental, not cold.
    run(["go", "build", "./..."])

    src = open(PUBLIC).read()
    for label, mutations, _, _ in CASES:
        for m in mutations:
            if m[0] == "replace":
                if src.count(m[2]) != 1:
                    print("HARNESS BUG: %s: anchor occurs %d times, expected 1: %r"
                          % (label, src.count(m[2]), m[2][:80]), flush=True)
                    return 2
    if GUARD not in open(os.path.join(API, "internal/httpapi/unlock_ratelimit_class_test.go")).read():
        print("HARNESS BUG: %s is not declared in unlock_ratelimit_class_test.go — this "
              "harness cannot attribute a failure it cannot name" % GUARD, flush=True)
        return 2

    brc, bout = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", GUARD_PKG, "-run", "^%s$" % GUARD, "-count=1"])
    print("=== CONTROL (unmutated): go build rc=%d, %s rc=%d ===" % (brc, GUARD, trc), flush=True)
    if brc != 0 or trc != 0:
        print("CONTROL NOT GREEN - fix the guard before proving it bites", flush=True)
        print(tout[-3000:], flush=True)
        return 1

    not_biting = []
    try:
        for label, mutations, want_red, want_named in CASES:
            err = apply_mutations(mutations)
            if err:
                print("HARNESS BUG: %s: %s" % (label, err), flush=True)
                not_biting.append(label)
                restore_all()
                continue

            b, _ = run(["go", "build", "./..."])
            tr, out = run(["go", "test", GUARD_PKG, "-run", "^%s$" % GUARD, "-count=1"])

            named = [w for w in want_named if w in out]
            if want_red:
                bites = b == 0 and tr != 0 and len(named) == len(want_named)
            else:
                bites = b == 0 and tr == 0
            if not bites:
                not_biting.append(label)

            if want_red:
                verdict = "RED (bites)" if bites else "GREEN (DOES NOT BITE)"
            else:
                verdict = "GREEN (control holds)" if bites else "RED (CONTROL FAILED)"
            print("  %-58s build rc=%d  guard rc=%d  -> %s" % (label, b, tr, verdict), flush=True)
            if want_red:
                print("      must name: %s" % ", ".join(want_named), flush=True)
                print("      named:     %s" % (", ".join(named) or "(none)"), flush=True)
            if b != 0:
                print("      NOTE: mutation ALSO broke the build (rc=%d) - a compile error would "
                      "hide the guard, so this case proves nothing" % b, flush=True)
                print("      " + out.strip().splitlines()[0] if out.strip() else "", flush=True)
            if not bites:
                print("      ---- output tail ----", flush=True)
                print("\n".join(out.strip().splitlines()[-12:]), flush=True)

            restore_all()
    finally:
        restore_all()

    print(flush=True)
    for p in MUTABLE:
        after = sha(p)
        print("restored %-22s sha256 %s %s"
              % (os.path.basename(p), after[:16], "OK" if after == BEFORE[p] else "MISMATCH"), flush=True)
    print("created file removed: %s" % ("OK" if not exists(XFILE) else "STILL PRESENT"), flush=True)

    rc, _ = run(["go", "test", GUARD_PKG, "-run", "^%s$" % GUARD, "-count=1"])
    rcb, _ = run(["go", "build", "./..."])
    print("final unmutated: %s rc=%d, go build rc=%d" % (GUARD, rc, rcb), flush=True)
    print("CASES THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    ok = (not not_biting) and rc == 0 and rcb == 0 and not exists(XFILE)
    return 0 if ok else 1


if __name__ == "__main__":
    code = main()
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
