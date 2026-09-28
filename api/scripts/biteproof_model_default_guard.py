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
`origin/main`, override with BITEPROOF_BASE) — the single-file reader. `BRANCH` is
the guard in the working tree. Each case carries its own expectation per leg, so
it can prove either "the old scope MISSED this" (BASE green, BRANCH red) or "the
old scope blamed the wrong file" (BASE red for a reason that is not true, BRANCH
green or red naming the real one). Needles are asserted against the failure
output, so a red in an unrelated assertion cannot be counted as a bite.

THE PAIR IS DERIVED, NOT TYPED. Each mutation lifts the `Name:`/`Description:`
pair out of tools.go with the guard's own pattern and re-emits it into a sibling
file of the package as `func ...() *mcp.Tool { return &mcp.Tool{...} }` — the
shape #732 left `disable_2fa` in. Nothing about the sentence is hand-copied, so
this harness cannot drift from the source it sabotages. Every mutation must still
compile (`go build ./...` rc=0): a mutation that breaks the build would hide the
guard, and that is asserted per case rather than assumed.

REFUSAL, NOT A FALSE GREEN. If the base revision's guard is byte-identical to the
working tree's, every BASE leg compares a reader with itself and the "missed this"
column means nothing — the harness exits 2 without a verdict (the rule
biteproof_mcp_tool_inventory.sh applies to its merge-base case). This check runs
FIRST: every case here depends on a BASE leg, so refusing early masks no case.

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

    print("base reader: %s (%s), %d bytes; tree reader: %d bytes"
          % (BASE_REF, base_rev, len(base_guard), len(ORIG_GUARD)), flush=True)
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
    for label, edits, added, base_expect, branch_expect in CASES:
        for rel, content in list(edits.items()) + list(added.items()):
            write(os.path.join(REPO, rel), content)

        b, bout_build = run(["go", "build", "./..."])
        trc, tout = run(["go", "test", TEST_PKG, "-count=1"])
        ok_branch, note_branch = judge(tout, trc, branch_expect)

        write(GUARD, base_guard)
        brc2, bout2 = run(["go", "test", TEST_PKG, "-count=1"])
        ok_base, note_base = judge(bout2, brc2, base_expect)
        write(GUARD, ORIG_GUARD)

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

    rcb, _ = run(["go", "build", "./..."])
    rct, _ = run(["go", "test", TEST_PKG, "-count=1"])
    print("final unmutated: go build ./... rc=%d, tree reader rc=%d" % (rcb, rct), flush=True)
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
