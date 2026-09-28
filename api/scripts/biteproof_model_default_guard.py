#!/usr/bin/env python3
"""Bite proof for api/internal/llm/model_default_guard_test.go (card t_8853fc29).

WHAT IT PROVES. The guard's docs-parity half compares each pinned MCP tool
description (generate_form, generate_legal_page) against its entry on
web/src/app/docs/mcp/page.tsx. It used to READ THAT PAIR OUT OF ONE FILE
(`api/internal/mcpsrv/tools.go`), while the registry the rest of the repo agrees
on is the PACKAGE — #732 moved a tool literal (`disable_2fa`) into
parity_issue396.go, and both web guards were widened to `api/internal/mcpsrv/*.go`
for that reason. This harness sabotages a MOVE of one of the two PINNED literals
and requires the guard to answer about the registry rather than about one file.

THE TWO LEGS, per case. `BASE` is the guard at a base revision (default
`origin/main`, override with BITEPROOF_BASE) — the reader BEFORE the widening,
i.e. the single-file reader while that fix is still a branch. `BRANCH` is the
guard in the working tree. Each case carries its own expectation per leg, so it
can prove either "the old scope MISSED this" (BASE green, BRANCH red) or "the old
scope blamed the wrong file" (BASE red for a reason that is not true, BRANCH
green or red naming the real one). Needles are asserted against the failure
output, so a red in an unrelated assertion cannot be counted as a bite.

The base is therefore not a fixed commit: it must be a revision whose reader
still ANSWERS DIFFERENTLY from this tree's, and the harness now checks that
premise per run instead of trusting the bytes (see REFUSAL below).

THE PAIR IS DERIVED, NOT TYPED. Each mutation lifts the `Name:`/`Description:`
pair out of tools.go with the guard's own pattern and re-emits it into a sibling
file of the package as `func ...() *mcp.Tool { return &mcp.Tool{...} }` — the
shape #732 left `disable_2fa` in. Nothing about the sentence is hand-copied, so
this harness cannot drift from the source it sabotages. Every mutation must still
compile (`go build ./...` rc=0): a mutation that breaks the build would hide the
guard, and that is asserted per case rather than assumed.

REFUSAL, NOT A FALSE GREEN. Two refusals, both exit 2 without a verdict. The
first is a fast path: the base reader is BYTE-IDENTICAL to this tree's, so the
comparison would be one reader against itself. The second is the general rule,
and it is NOT a byte comparison: no case may pass unless the base reader ANSWERED
DIFFERENTLY from this tree's reader on it. A byte test cannot decide this premise
— once the guard's own widening merges into `origin/main`, the base file stays a
few bytes off this tree's (the guard can be rewritten around it) while READING THE
SAME REGISTRY, so every BASE leg answers like its BRANCH leg and the "missed this"
column is empty; a byte-equality refusal did not fire and the run printed
`0/7 BITES` for a premise that had quietly become "compare a reader with itself"
(card t_9df01abd, measured: base 22241 bytes vs tree 22242). The capability rule
requires positive evidence — at least one case whose BASE leg and BRANCH leg
disagree, in their green/red verdict or in the claim the guard made — and refuses
otherwise, naming BITEPROOF_BASE as the fix.

Restore is signal-safe: originals are held in memory, restored in a finally, and a
SIGTERM/SIGINT handler restores before exiting. Every restore is verified by
sha256 rather than asserted, and the run ends by requiring `git status` clean.
"""
import atexit
import hashlib
import os
import re
import signal
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
API = os.path.dirname(HERE)
REPO = os.path.dirname(API)
GUARD_REL = "api/internal/llm/model_default_guard_test.go"
TOOLS_REL = "api/internal/mcpsrv/tools.go"
SIBLING_REL = "api/internal/mcpsrv/parity_issue396.go"
SUBDIR_REL = "api/internal/mcpsrv/sub/biteproof_tool.go"

GUARD = os.path.join(REPO, GUARD_REL)
TOOLS = os.path.join(REPO, TOOLS_REL)
SIBLING = os.path.join(REPO, SIBLING_REL)

BASE_REF = os.environ.get("BITEPROOF_BASE", "origin/main")
# BITEPROOF_ONLY filters the case list by substring (diagnostics while the harness
# itself is being written; empty means every case, which is the real run).
ONLY = os.environ.get("BITEPROOF_ONLY", "")
TEST_PKG = "./internal/llm/"

PINNED = ("generate_form", "generate_legal_page")


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def read(path):
    with open(path, "rb") as fh:
        return fh.read().decode()


def write(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as fh:
        fh.write(data.encode())


def run(cmd):
    p = subprocess.run(cmd, cwd=API, capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


def git(*args):
    p = subprocess.run(["git"] + list(args), cwd=REPO, capture_output=True, text=True)
    return p.returncode, p.stdout.strip(), p.stderr.strip()


ORIG_TOOLS = read(TOOLS)
ORIG_SIBLING = read(SIBLING)
ORIG_GUARD = read(GUARD)
BEFORE = {TOOLS: sha(TOOLS), SIBLING: sha(SIBLING), GUARD: sha(GUARD)}
# The tree is EXPECTED to be dirty (this harness runs on the branch that changes
# the guard), so the run is judged on what it ADDED to git status, not on the
# tree being clean.
_, STATUS_BEFORE, _ = git("status", "--porcelain")
STATUS_BEFORE = sorted(STATUS_BEFORE.splitlines())
DONE = False


def bail(msg):
    print("HARNESS BUG: %s" % msg, flush=True)
    sys.exit(2)


def pair_pattern(tool):
    """The guard's own pair pattern, for one tool."""
    return re.compile(r'(?s)\t\tName:\s*"%s",\n\t\tDescription:\s*"((?:[^"\\]|\\.)*)",\n'
                      % re.escape(tool))


def lift(tool):
    """The whole `Name:/Description:` pair text and its description, from tools.go."""
    m = pair_pattern(tool)
    hits = m.findall(ORIG_TOOLS)
    if len(hits) != 1:
        bail("%s pair found %d times in %s, expected 1" % (tool, len(hits), TOOLS_REL))
    return m.search(ORIG_TOOLS).group(0), hits[0]


def tools_without(tool):
    """tools.go with the pair removed (the registration site stays)."""
    whole, _ = lift(tool)
    if ORIG_TOOLS.count(whole) != 1:
        bail("tools.go holds the %s pair %d times" % (tool, ORIG_TOOLS.count(whole)))
    return ORIG_TOOLS.replace(whole, "")


def edited(desc):
    """A derived wording change that keeps every model id intact."""
    return desc[:-1] + " (bite proof)."


def move_func(tool, desc):
    """The literal in a sibling file of the package, in #732's own shape."""
    name = "biteproofMoved%sTool" % re.sub(r"[^A-Za-z0-9]", "", tool.title())
    return (
        "\n\n// biteproof (card t_8853fc29): this tool's literal has MOVED out of\n"
        "// tools.go and lives in this file, exactly as disable_2fa does.\n"
        "func %s() *mcp.Tool {\n"
        "\treturn &mcp.Tool{\n"
        '%s'
        "\t}\n"
        "}\n"
        % (name, "".join("\t\t" + line + "\n"
                         for line in ('Name:        "%s",' % tool,
                                      'Description: "%s",' % desc)))
    )


def cases():
    """(label, edits, added_files, base_expectation, branch_expectation).

    edits maps repo-relative path -> new content. added_files are created and
    removed. An expectation is (must_be_green, *needles): green means the leg must
    exit 0; otherwise the leg must be RED with every needle in its output.
    """
    out = []
    for tool in PINNED:
        _, desc = lift(tool)
        moved = move_func(tool, desc)
        moved_edited = move_func(tool, edited(desc))
        missing = 'tools.go has no "%s" tool description' % tool
        twice = 'holds the "%s" tool description twice' % tool
        differs = "differs between the registry (api/internal/mcpsrv)"

        out.append((
            "MOVE %s out of tools.go, sentence unchanged" % tool,
            {TOOLS_REL: tools_without(tool), SIBLING_REL: ORIG_SIBLING + moved},
            {},
            (False, missing),        # the old reader blames tools.go / false alarm
            (True,),                 # the package reader answers correctly
        ))

        out.append((
            "MOVE %s, stale copy left in tools.go, moved sentence edited" % tool,
            {SIBLING_REL: ORIG_SIBLING + moved_edited},
            {},
            (True,),                 # the old reader compares the STALE copy: green, wrong
            (False, twice),          # the package reader refuses to pick one of two
        ))

        out.append((
            "MOVE %s, no copy behind, moved sentence edited" % tool,
            {TOOLS_REL: tools_without(tool), SIBLING_REL: ORIG_SIBLING + moved_edited},
            {},
            (False, missing),        # blames a file the tool is no longer in
            (False, differs, "(bite proof)"),   # names the real disagreement
        ))

    out.append((
        "a tool file in a SUBdirectory of the registry package",
        {},
        {SUBDIR_REL: "// Package sub is a throwaway subdirectory written by a bite proof.\n"
                     "// It names the shipped model id deepseek-v4.1-flash so the repo walk\n"
                     "// sees the file and the registry reader has to decide about it.\n"
                     "package sub\n"},
        (True,),                     # the old reader never looked
        (False, "is not a direct child of the registry package"),
    ))
    return out


CASES = [c for c in cases() if ONLY in c[0]]
ADDED_PATHS = sorted({p for c in cases() for p in c[2]})


def restore_all():
    global DONE
    write(TOOLS, ORIG_TOOLS)
    write(SIBLING, ORIG_SIBLING)
    write(GUARD, ORIG_GUARD)
    for path in ADDED_PATHS:
        if os.path.exists(path):
            os.remove(path)
    DONE = True


atexit.register(lambda: None if DONE else restore_all())


def _sig(signum, _frame):
    restore_all()
    print("signal %d - originals restored, exiting" % signum, flush=True)
    sys.exit(130)


signal.signal(signal.SIGTERM, _sig)
signal.signal(signal.SIGINT, _sig)


def failure_lines(out):
    """Every failure line of a leg, verbatim, so a diagnosis is read and not guessed."""
    keep = []
    for line in out.splitlines():
        s = line.rstrip()
        if s.lstrip().startswith(("---", "FAIL", "ok ", "?   ", "model_default_guard_test.go:")):
            keep.append(s)
    return keep[:14]


CLAIM_RE = re.compile(r"^model_default_guard_test\.go:\d+:\s*(.*)$")

# A leg that went red WITHOUT naming a claim of its own (a compile error, a build
# failure) is not an answer to compare — see answers() below and the capability
# refusal in main().
NO_CLAIM = "RED: <no claim line>"


def leg_claim(out, rc):
    """The guard's own ANSWER for one leg, as text that can be compared between two legs.

    Green is "GREEN". Red is the guard's CLAIM — the text of its own t.Fatalf lines,
    with the `model_default_guard_test.go:<line>:` prefix stripped. The line NUMBER
    is deliberately dropped: two revisions of the file number the same assertion
    differently, and a comparison of the answers must not see that as a difference
    (it is not one — the reader said the same thing). Status lines (`--- FAIL`,
    `FAIL`, `ok  `) are dropped too: their timings differ run to run, so leaving them
    in would manufacture a disagreement out of a timing.
    """
    if rc == 0:
        return "GREEN"
    claims = []
    for line in failure_lines(out):
        m = CLAIM_RE.match(line.strip())
        if m:
            claims.append(m.group(1).strip())
    if not claims:
        return NO_CLAIM
    return "RED: " + " | ".join(sorted(claims))


def answers(claim):
    """True when a claim is an ANSWER the guard made, not an artifact of the build.

    A red with no t.Fatalf line of its own means the guard never got to say
    anything (a compile error, a missing symbol at that revision), so it must not
    be counted as a reader disagreeing with another reader — the capability refusal
    in main() would otherwise accept a base that does not even build as "different".
    """
    return claim == "GREEN" or (claim.startswith("RED: ") and claim != NO_CLAIM)


def judge(out, rc, expect):
    """Return (ok, note) for one leg against its expectation."""
    if expect[0]:
        return (rc == 0), ("GREEN as expected" if rc == 0 else "RED but should be GREEN")
    if rc == 0:
        return False, "GREEN (DOES NOT BITE)"
    for needle in expect[1:]:
        if needle not in out:
            return False, "RED without the needle %r" % needle
    return True, "RED (bites)"


def clear_case(edits, added):
    write(TOOLS, ORIG_TOOLS)
    write(SIBLING, ORIG_SIBLING)
    for rel in set(edits) | set(added):
        if rel in (TOOLS_REL, SIBLING_REL):
            continue
        path = os.path.join(REPO, rel)
        if os.path.exists(path):
            os.remove(path)


def capability_refusal(base_rev, base_guard, seen, cases_run):
    """The refusal a byte-equality test could not make: print it, name the usable base.

    It says the base CANNOT demonstrate the blind spot — no verdict, no bite count —
    and it derives the base to use instead rather than guessing one: the newest
    commit at or before BASE_REF that touched the guard is the one that moved it to
    this scope (that widening lands as a normal commit), so its PARENT is the
    revision whose reader still answers differently. A merge-base would be wrong
    here — on a branch cut from current main it IS the caught-up revision, which is
    exactly what just failed this check.
    """
    _, last_touch, _ = git("log", "-1", "--format=%H", base_rev, "--", GUARD_REL)
    _, suggest, _ = (git("rev-parse", "%s^" % last_touch) if last_touch else (1, "", ""))
    print(flush=True)
    print("REFUSING (exit 2, NO VERDICT): not one of the %d case(s) run so far made the reader at "
          "%s (%s) answer differently from this tree's. Every BASE leg is the same reader as its "
          "BRANCH leg, so the 'missed this' column is empty by construction and a bite count would "
          "be a statement about this instrument, not about the guard."
          % (cases_run, BASE_REF, base_rev), flush=True)
    print("That is what a base whose reader has CAUGHT UP to this branch looks like: the "
          "package-wide registry reader lives there too now (PR 835's shape, once it has merged), "
          "and the two files differ by bytes while the reader does not differ.", flush=True)
    if suggest:
        print("Point BITEPROOF_BASE at the revision BEFORE that reader landed — the parent of the "
              "newest commit at %s that touched the guard (%s):" % (base_rev, last_touch[:12]),
              flush=True)
        print("    BITEPROOF_BASE=%s python3 scripts/biteproof_model_default_guard.py" % suggest,
              flush=True)
    else:
        print("Point BITEPROOF_BASE at a revision where the single-file reader still lived.",
              flush=True)
    print("(Measured on this host for card t_9df01abd: at BITEPROOF_BASE=origin/main, base reader "
          "%d chars vs tree reader %d chars, %d/%d case(s) disagreed -> refused; at %s the same "
          "tree is 7/7.)"
          % (len(base_guard), len(ORIG_GUARD), len(seen), len(CASES),
             suggest[:12] or "the pre-fix revision"), flush=True)
    return 2


def main():
    rc, base_rev, err = git("rev-parse", "--short", BASE_REF)
    if rc != 0:
        print("cannot resolve BITEPROOF_BASE=%s: %s" % (BASE_REF, err), flush=True)
        return 2
    rc, base_guard, err = git("show", "%s:%s" % (BASE_REF, GUARD_REL))
    if rc != 0:
        print("BASE READER MISSING: %s:%s could not be read (%s) - without it this harness "
              "cannot show what the one-file scope missed" % (BASE_REF, GUARD_REL, err), flush=True)
        return 2
    if base_guard == ORIG_GUARD:
        print("REFUSING: the guard at %s (%s) is BYTE-IDENTICAL to this tree's (%s). Every case "
              "compares the base reader against the working-tree reader, so here that would be one "
              "reader against itself and the 'missed this' column would prove nothing. Run this on "
              "the branch that widens the guard, or set BITEPROOF_BASE to a revision where the "
              "single-file reader still lived (origin/main before that fix lands)."
              % (BASE_REF, base_rev, BEFORE[GUARD][:12]), flush=True)
        return 2
    print("base reader: %s (%s), %d bytes (%d chars); tree reader: %d bytes (%d chars)"
          % (BASE_REF, base_rev, len(base_guard.encode()), len(base_guard),
             len(ORIG_GUARD.encode()), len(ORIG_GUARD)), flush=True)
    print("cases: %d%s" % (len(CASES), "" if not ADDED_PATHS else
                           " (+%d file(s) created and removed)" % len(ADDED_PATHS)), flush=True)

    run(["go", "build", "./..."])
    brc, _ = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", TEST_PKG, "-count=1"])
    write(GUARD, base_guard)
    btrc, bout = run(["go", "test", TEST_PKG, "-count=1"])
    write(GUARD, ORIG_GUARD)
    print("=== CONTROL (unmutated): build rc=%d, tree reader rc=%d, base reader rc=%d ==="
          % (brc, trc, btrc), flush=True)
    if brc != 0 or trc != 0 or btrc != 0:
        print("CONTROL NOT GREEN - the tree is broken before any mutation, so nothing below "
              "would mean anything. A red base reader on the unmutated tree in particular makes "
              "every 'does not reproduce' verdict meaningless.", flush=True)
        print(tout[-2000:], flush=True)
        print(bout[-2000:], flush=True)
        clear_case({}, {})
        return 1

    not_biting = []
    # The premise this harness needs, per case, is that the BASE reader ANSWERS
    # DIFFERENTLY from the tree reader. A verdict where they never differ is one
    # reader measured against itself, however far apart the two FILES are — so it
    # is a refusal (exit 2), not a "0 bites" result. Collected here and judged
    # after every case has run, so the refusal names the whole picture.
    disagreements = []
    case_no = 0
    for case_no, (label, edits, added, base_expect, branch_expect) in enumerate(CASES, 1):
        for rel, content in list(edits.items()) + list(added.items()):
            write(os.path.join(REPO, rel), content)

        b, bout_build = run(["go", "build", "./..."])
        trc, tout = run(["go", "test", TEST_PKG, "-count=1"])
        ok_branch, note_branch = judge(tout, trc, branch_expect)

        write(GUARD, base_guard)
        brc2, bout2 = run(["go", "test", TEST_PKG, "-count=1"])
        ok_base, note_base = judge(bout2, brc2, base_expect)
        write(GUARD, ORIG_GUARD)

        base_claim = leg_claim(bout2, brc2)
        branch_claim = leg_claim(tout, trc)
        if answers(base_claim) and answers(branch_claim) and base_claim != branch_claim:
            disagreements.append(label)

        ok = ok_branch and ok_base and b == 0
        if not ok:
            not_biting.append(label)
        print("  %-62s build rc=%d | BASE %-27s | BRANCH %-27s -> %s"
              % (label, b, note_base, note_branch, "OK" if ok else "FAILED"), flush=True)
        if b != 0:
            print("      NOTE: the mutation ALSO broke the build (rc=%d) - a compile error "
                  "would hide the guard" % b, flush=True)
            for line in bout_build.strip().splitlines()[:4]:
                print("      " + line.strip(), flush=True)
        for leg, out in (("BASE", bout2), ("BRANCH", tout)):
            for line in failure_lines(out):
                print("      %-6s %s" % (leg, line), flush=True)

        clear_case(edits, added)

    print(flush=True)
    for path in (TOOLS, SIBLING, GUARD):
        after = sha(path)
        print("restored %-32s sha256 %s %s" % (
            os.path.relpath(path, REPO), after[:16],
            "OK" if after == BEFORE[path] else "MISMATCH"), flush=True)
    for rel in ADDED_PATHS:
        print("removed  %-32s %s" % (rel, "OK" if not os.path.exists(os.path.join(REPO, rel))
                                     else "STILL PRESENT"), flush=True)
    # `git status` is compared BEFORE vs AFTER rather than required empty: the
    # whole point of this harness is to run on a branch that CHANGES the guard
    # (and, in this card's case, BITE-HARNESSES.md), so a dirty tree is the
    # expected starting state. What must not survive the run is a leftover
    # mutation, and that is what this comparison catches.
    rc, status_after, _ = git("status", "--porcelain")
    leftover = sorted(set(status_after.splitlines()) - set(STATUS_BEFORE))
    print("git status: %d entry/entries before, %d after, %d introduced by this run"
          % (len(STATUS_BEFORE), len(status_after.splitlines()), len(leftover)), flush=True)
    if leftover:
        print("LEFT BEHIND BY THIS RUN: %r" % leftover, flush=True)

    # CAPABILITY REFUSAL, AFTER the cases have run. The premise every case needs is
    # that the base reader ANSWERS DIFFERENTLY from this tree's reader. If no case
    # produced a disagreement, this run compared a reader with itself — whatever the
    # two files' sizes are — and a "0 bites" verdict would read as a finding about
    # the tree when it is the instrument's premise that failed. Refuse instead, with
    # NO verdict and NO bite count.
    if not disagreements:
        return capability_refusal(base_rev, base_guard, disagreements, case_no)

    rcb, _ = run(["go", "build", "./..."])
    rct, _ = run(["go", "test", TEST_PKG, "-count=1"])
    print("final unmutated: go build ./... rc=%d, tree reader rc=%d" % (rcb, rct), flush=True)
    print("cases where the two readers DISAGREED (the premise held): %d/%d"
          % (len(disagreements), len(CASES)), flush=True)
    print("MUTATIONS THAT DID NOT BITE: %d %s" % (len(not_biting), not_biting), flush=True)
    print("BITES: %d/%d cases (each leg at its own named needle)"
          % (len(CASES) - len(not_biting), len(CASES)), flush=True)
    return 1 if (not_biting or rcb or rct or leftover) else 0


if __name__ == "__main__":
    code = main()
    DONE = True
    restore_all()
    print("EXIT=%d" % code, flush=True)
    sys.exit(code)
