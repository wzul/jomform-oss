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
  (e) the guard really RAN THE ROUTE UNDER TEST, in BOTH directions: the named
      test's own result marker (`--- PASS:` / `--- FAIL:`, which needs `-v`), and
      the route printed in the guard's own enumeration block ("N pattern(s)
      enumerated from the router and driven with a burst ..."). Without (e) a
      control case could not be told from "the guard never ran" — `go test`
      exits 0 when `-run` matches nothing, so a rename, a build tag or a changed
      `-run` would have printed `GREEN (control holds)` about a route nothing
      judged. Measured shape this closes: `go test -run ^NoSuchTest$` prints
      "testing: warning: no tests to run" and "ok ... [no tests to run]", rc=0,
      with no log block at all.
  (f) every touched file is restored byte-identically, verified by sha256, and a
      file the case created is gone again.

Seven cases. The two shapes that matter most, because each was missed by a
SUCCESSIVE version of the guard:
  - A/B (INDIRECT, registered through a helper): missed by the FIRST guard, the
    single-line text regex over `mux.Handle` call sites.
  - F/G (INLINE on the SERVED route table, server.go): missed by the SECOND
    guard, which enumerated a HAND-RE-LISTED mirror of the server's
    registrations, so a route added to Server.routes() outside the mirrored
    groups was served — live and unlimited — while the guard reported green.
    G is its control: the same inline registration WRAPPED must stay green, so
    F's red is attributable to the missing limiter and not to inline registration
    being rejected on sight.

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
# Cases F/G register INLINE on the SERVED route table (server.go, Server.routes
# ()) — the shape the previous guard's hand-re-listed mirror could not see.
SERVER = os.path.join(API, "internal/httpapi/server.go")

GUARD_PKG = "./internal/httpapi/"
GUARD = "TestEveryUnlockRouteIsLimitedAtTheRouter"

# Every existing file the cases mutate, so they can be held and restored as a set.
MUTABLE = [PUBLIC, SERVER]


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


# ─── "the guard really ran" ──────────────────────────────────────────────────
# A green `go test` is NOT evidence that anything ran: when -run matches no test,
# `go test` prints "testing: warning: no tests to run" and exits 0. MEASURED on
# this host (go1.27.0):
#
#     $ go test ./internal/httpapi/ -run '^TestNoSuchNameExistsAtAll$' -count=1 -v
#     testing: warning: no tests to run
#     PASS
#     ok  github.com/wzul/jomform/api/internal/httpapi  0.408s [no tests to run]
#     rc=0
#
# So a control case that asserted only `rc == 0` would print GREEN (control
# holds) after a rename, a build tag or a changed -run made the guard not run at
# all — a green that cannot be told from "the guard never ran", which is the
# failure mode this whole harness exists to prevent. Every case therefore
# requires the guard's OWN evidence, and it is checked for the controls (B and G)
# exactly as it is for the red cases:
#
#   * `-v` output carries a per-test result marker, `--- PASS: <name>` /
#     `--- FAIL: <name>`; neither appears when nothing ran.
#   * the guard's enumeration block (unlock_ratelimit_class_test.go's t.Logf)
#     names every pattern it enumerated AND prints the burst it fired at it, so
#     its presence is evidence that the walk ran and the route was DRIVEN.
#   * the route under test appears in that block, so the evidence is about THIS
#     case's route and not a sibling.
# The -v flag is what makes (1) and (2) reachable at all; without it the guard's
# t.Logf is swallowed on a passing run.
#
# The seam is self-checked on every run (selfcheck_bite_control): the same
# evidence function is fed a REAL vacuous output from a `-run` that matches
# nothing, and a REAL driven output from the guard, and must reject the first and
# accept the second. Without that, a future edit could neuter this check (or the
# guard could stop logging the block) and every case below would still print a
# verdict — the one failure this fix must not itself be able to acquire.
GUARD_RESULT_PASS = "--- PASS: %s" % GUARD
GUARD_RESULT_FAIL = "--- FAIL: %s" % GUARD
GUARD_ENUM_HEADER = "pattern(s) enumerated from the router and driven with a burst of"


def driving_evidence(out, result_marker, want_drove):
    """(ok, missing) — whether `out` shows the guard ran AND drove want_drove.

    `out` is the combined stdout+stderr of a `go test -v -run ^<guard>$` run.
    Nothing here reads the guard's behaviour; it reads the guard's own report
    that it enumerated the router and fired a burst at the named route.
    """
    missing = []
    if result_marker not in out:
        missing.append("the guard's own result marker %r" % result_marker)
    # The routes are checked inside the enumeration BLOCK, not merely somewhere in
    # the output: the block is the guard reporting which patterns it drove, and a
    # route named only in a failure message would not be that evidence.
    block = ""
    at = out.find(GUARD_ENUM_HEADER)
    if at < 0:
        missing.append("the guard's enumeration block (%r)" % GUARD_ENUM_HEADER)
    else:
        block = out[at:]
        # The block ends where the test's own result is reported.
        for stop in ("\n--- ", "\n=== NAME", "\nPASS\n", "\nFAIL\n"):
            i = block.find(stop)
            if i >= 0:
                block = block[:i]
    for w in want_drove:
        if block and w not in block:
            missing.append("the driven route %r inside the enumeration block" % w)
    if missing:
        return False, missing
    return True, []


def run_guard():
    """Run the guard verbosely and return (rc, output).

    `-v` is load-bearing: without it `go test` swallows the guard's t.Logf on a
    passing run, so the evidence driving_evidence needs is not in the output at
    all — which is exactly how the control cases used to be unfalsifiable.
    """
    return run(["go", "test", GUARD_PKG, "-run", "^%s$" % GUARD, "-count=1", "-v"])


def run_guard_name(name):
    """Run `go test -run ^<name>$ -v` and return (rc, output)."""
    return run(["go", "test", GUARD_PKG, "-run", "^%s$" % name, "-count=1", "-v"])


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

# ─── cases F/G: registered INLINE on the SERVED route table (server.go) ───────
#
# The registered-here anchors live in Server.routes() (server.go), not in
# RegisterRoutes. This is the shape the guard's predecessor missed: the mirror of
# registrations it enumerated was hand-written, so a route added to the served
# table outside the mirrored groups was served, live and unlimited, while the
# guard reported green.
SERVER_ANCHOR = '\t\tmux.Handle("GET /metrics", s.metrics.Handler())\n'
INLINE_UNWRAPPED = SERVER_ANCHOR + (
    '\t\tmux.HandleFunc("POST /qa-inline/{slug}/unlock", func(w http.ResponseWriter, r *http.Request) {\n'
    '\t\t\tw.WriteHeader(http.StatusForbidden)\n'
    '\t\t})\n'
)
INLINE_WRAPPED = SERVER_ANCHOR + (
    '\t\tmux.Handle("POST /qa-inline/{slug}/unlock", middleware.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n'
    '\t\t\tw.WriteHeader(http.StatusForbidden)\n'
    '\t\t})))\n'
)

# (label, mutations, want_red, want_named, want_drove)
#   mutations: list of ("replace", path, old, new) | ("create", path, content)
#   want_red:  True  -> the guard MUST fail (the sabotage is a real defect)
#              False -> the guard MUST pass (a control; catching it here would
#                       mean the guard reports a limit that is not there)
#   want_named: substrings the guard's output must contain when it is red, so a
#               red is attributable to THIS route and not to a sibling
#   want_drove: substrings that must appear in the guard's ENUMERATION BLOCK, so
#               the case is only counted when the guard really enumerated the
#               router and fired a burst at this route. Required for the control
#               cases (B and G) too — that is the whole point: a green that
#               cannot be told from "the guard never ran" is not a control.
#
# The shipped routes the unmutated tree must drive, for the CONTROL leg.
SHIPPED_UNLOCK_ROUTES = [
    "POST /embed/f/{wsslug}/{slug}/unlock",
    "POST /f/{slug}/unlock",
    "POST /{slug}/unlock",
]

CASES = [
    (
        "A: INDIRECT unwrapped unlock route (the card's defect)",
        [("replace", PUBLIC, LAST_REG, INDIRECT_UNWRAPPED)],
        True, ["POST /qa-indirect/{slug}/unlock"], ["POST /qa-indirect/{slug}/unlock"],
    ),
    (
        "B: the same INDIRECT route WRAPPED (control: must stay GREEN)",
        [("replace", PUBLIC, LAST_REG, INDIRECT_WRAPPED)],
        False, [], ["POST /qa-indirect/{slug}/unlock"],
    ),
    (
        "C: a LITERAL unwrapped unlock route (what the old guard DID catch)",
        [("replace", PUBLIC, LAST_REG, LITERAL_UNWRAPPED)],
        True, ["POST /qa-literal/{slug}/unlock"], ["POST /qa-literal/{slug}/unlock"],
    ),
    (
        "D: a SHIPPED route loses its wrapper (bites on production code)",
        [("replace", PUBLIC, UNWRAP_ONE_SHIPPED, WRAP_ONE_SHIPPED)],
        True, ["POST /f/{slug}/unlock"], ["POST /f/{slug}/unlock"],
    ),
    (
        "E: an INDIRECT route registered from ANOTHER FILE in the package",
        [("replace", PUBLIC, LAST_REG, XF_CALL), ("create", XFILE, XF_HANDLER)],
        True, ["POST /qa-xfile/{slug}/unlock"], ["POST /qa-xfile/{slug}/unlock"],
    ),
    (
        "F: an UNWRAPPED unlock route registered INLINE on the SERVED table (server.go)",
        [("replace", SERVER, SERVER_ANCHOR, INLINE_UNWRAPPED)],
        True, ["POST /qa-inline/{slug}/unlock"], ["POST /qa-inline/{slug}/unlock"],
    ),
    (
        "G: the same INLINE registration WRAPPED (control: must stay GREEN)",
        [("replace", SERVER, SERVER_ANCHOR, INLINE_WRAPPED)],
        False, [], ["POST /qa-inline/{slug}/unlock"],
    ),
]


def selfcheck_bite_control():
    """(ok, message) — prove driving_evidence() can reject a run that did nothing.

    Runs the guard's -run selector against a name that matches NO test and feeds
    the result to driving_evidence with a shipped route as the route it must have
    driven. `go test` exits 0 in that case, so the ONLY thing that can reject it
    is the evidence check itself. A second leg then feeds the REAL unmutated guard
    output (already in hand) and requires it to be accepted, so a check that
    rejected everything would not pass either.
    """
    vrc, vout = run_guard_name("TestZZSelfCheckNameThatMatchesNoTest")
    rejected, why = driving_evidence(vout, GUARD_RESULT_PASS, SHIPPED_UNLOCK_ROUTES)
    lines = [
        "  vacuous run:  go test -run ^TestZZSelfCheckNameThatMatchesNoTest$ rc=%d" % vrc,
        "                evidence check says ran=%s (must be NO)" % ("yes" if rejected else "NO"),
    ]
    if vrc != 0:
        lines.append("  SELF-CHECK UNUSABLE: the vacuous run did not exit 0 (rc=%d), so it is "
                     "not the benign-vacuous shape this check is for" % vrc)
        return False, "\n".join(lines)
    if rejected:
        lines.append("  SELF-CHECK FAILED: a run that matched no test was accepted as "
                     "evidence the guard ran. driving_evidence() is not checking what it "
                     "claims (missing: %s)" % (", ".join(why) or "nothing"))
        return False, "\n".join(lines)

    drc, dout = run_guard()
    accepted, why2 = driving_evidence(dout, GUARD_RESULT_PASS, SHIPPED_UNLOCK_ROUTES)
    lines.append("  driven run:   go test -run ^%s$ rc=%d" % (GUARD, drc))
    lines.append("                evidence check says ran=%s (must be yes)" % ("yes" if accepted else "NO"))
    if drc != 0 or not accepted:
        lines.append("  SELF-CHECK FAILED: the real guard output was NOT accepted as evidence "
                     "(rc=%d, missing: %s) — the check is too strict to be useful, or the "
                     "guard stopped logging its enumeration block" % (drc, ", ".join(why2) or "nothing"))
        return False, "\n".join(lines)
    return True, "\n".join(lines)


def apply_mutations(mutations):
    """Apply every mutation, verifying each really landed. Returns an error string."""
    for m in mutations:
        if m[0] == "replace":
            _, path, old, new = m
            src = open(path).read()
            if src.count(old) != 1:
                return "anchor %r occurs %d times in %s" % (old[:60], src.count(old), path)
            # Compare the RESULT, not substring membership: an insertion anchor
            # (old) is legitimately still present in new when the injected text
            # is written around it, so "old in on_disk" would call a landed
            # mutation a failure. The exact expected content is the honest check.
            expected = src.replace(old, new)
            open(path, "w").write(expected)
            if open(path).read() != expected:
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

    for label, mutations, _, _, _ in CASES:
        for m in mutations:
            if m[0] == "replace":
                _, path, old, _ = m
                src = open(path).read()
                if src.count(old) != 1:
                    print("HARNESS BUG: %s: anchor occurs %d times in %s, expected 1: %r"
                          % (label, src.count(old), os.path.basename(path), old[:80]), flush=True)
                    return 2
    if GUARD not in open(os.path.join(API, "internal/httpapi/unlock_ratelimit_class_test.go")).read():
        print("HARNESS BUG: %s is not declared in unlock_ratelimit_class_test.go — this "
              "harness cannot attribute a failure it cannot name" % GUARD, flush=True)
        return 2

    brc, bout = run(["go", "build", "./..."])
    trc, tout = run_guard()
    print("=== CONTROL (unmutated): go build rc=%d, %s rc=%d ===" % (brc, GUARD, trc), flush=True)

    # Prove the evidence check itself can fail, BEFORE anything is called green:
    # the same driving_evidence() is fed a REAL vacuous run (a -run that matches
    # nothing) and a REAL driven run (the guard above), and must reject the first
    # and accept the second. A green CONTROL with a broken evidence check would be
    # the very "green that cannot be told from did not run" this harness exists to
    # catch, one level up.
    sc_ok, sc_msg = selfcheck_bite_control()
    print("=== evidence self-check: %s ===" % ("OK" if sc_ok else "FAILED"), flush=True)
    print(sc_msg, flush=True)
    if not sc_ok:
        print("CONTROL EVIDENCE CHECK DOES NOT WORK - every verdict below would be "
              "unfalsifiable, refusing to report", flush=True)
        return 1

    ok, missing = driving_evidence(tout, GUARD_RESULT_PASS, SHIPPED_UNLOCK_ROUTES)
    if brc != 0 or trc != 0 or not ok:
        print("CONTROL NOT GREEN - fix the guard before proving it bites", flush=True)
        for m in missing:
            print("  missing from evidence: %s" % m, flush=True)
        print(tout[-3000:], flush=True)
        return 1
    print("CONTROL drove the shipped routes: %s" % ", ".join(SHIPPED_UNLOCK_ROUTES), flush=True)

    not_biting = []
    try:
        for label, mutations, want_red, want_named, want_drove in CASES:
            err = apply_mutations(mutations)
            if err:
                print("HARNESS BUG: %s: %s" % (label, err), flush=True)
                not_biting.append(label)
                restore_all()
                continue

            b, _ = run(["go", "build", "./..."])
            tr, out = run_guard()

            named = [w for w in want_named if w in out]
            # The guard's own evidence that it RAN and drove this route. Checked
            # for controls too: `go test` exits 0 when -run matches nothing, so
            # without this a control's "GREEN (control holds)" is unfalsifiable.
            marker = GUARD_RESULT_FAIL if want_red else GUARD_RESULT_PASS
            drove_ok, drove_missing = driving_evidence(out, marker, want_drove)
            if want_red:
                bites = b == 0 and tr != 0 and len(named) == len(want_named) and drove_ok
            else:
                bites = b == 0 and tr == 0 and drove_ok
            if not bites:
                not_biting.append(label)

            if want_red:
                verdict = "RED (bites)" if bites else "GREEN (DOES NOT BITE)"
            else:
                verdict = "GREEN (control holds)" if bites else "RED (CONTROL FAILED)"
            print("  %-58s build rc=%d  guard rc=%d  ran=%s  -> %s"
                  % (label, b, tr, "yes" if drove_ok else "NO", verdict), flush=True)
            if want_red:
                print("      must name: %s" % ", ".join(want_named), flush=True)
                print("      named:     %s" % (", ".join(named) or "(none)"), flush=True)
            print("      must drive: %s" % ", ".join(want_drove), flush=True)
            for m in drove_missing:
                print("      NOT EVIDENCED: %s" % m, flush=True)
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

    rc, rout = run_guard()
    rcb, _ = run(["go", "build", "./..."])
    fok, fmissing = driving_evidence(rout, GUARD_RESULT_PASS, SHIPPED_UNLOCK_ROUTES)
    print("final unmutated: %s rc=%d ran=%s, go build rc=%d"
          % (GUARD, rc, "yes" if fok else "NO", rcb), flush=True)
    for m in fmissing:
        print("  NOT EVIDENCED (final): %s" % m, flush=True)
    print("CASES THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    ok = (not not_biting) and rc == 0 and rcb == 0 and fok and not exists(XFILE)
    return 0 if ok else 1


if __name__ == "__main__":
    code = main()
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
