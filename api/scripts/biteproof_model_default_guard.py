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

REFUSAL, NOT A FALSE GREEN. Two refusals, both exit 2 without a verdict.

The first is a fast path and it fires on the BYTES: the base reader is identical,
character for character, to this tree's, so the comparison would be one reader
against itself. It reads the file with git_text() rather than git(), because the
general git() helper strips stdout and every Go file ends with a newline — with a
stripped base the test was unsatisfiable at any byte distance and the path was dead
code (card t_34ab7a00, measured at origin/main 272fda17, where BITEPROOF_BASE
defaults to the tree itself: 49788 bytes on disk, the stripped copy 49593 chars
against the tree's 49594, so it could not fire).

The second is the general rule, and it is NOT a byte comparison: no case may pass
unless the base reader ANSWERED DIFFERENTLY from this tree's reader on it. A byte
test cannot decide this premise — once the guard's own widening merges into
`origin/main`, the base file stays a few bytes off this tree's (the guard can be
rewritten around it) while READING THE SAME REGISTRY, so every BASE leg answers
like its BRANCH leg and the "missed this" column is empty; the run would print
`0/7 BITES` for a premise that had quietly become "compare a reader with itself"
(card t_9df01abd, measured: base 22241 bytes vs tree 22242). The capability rule
requires positive evidence — at least one case whose BASE leg and BRANCH leg
disagree, in their green/red verdict or in the claim the guard made — and refuses
otherwise, naming BITEPROOF_BASE as the fix.

The base it names is DERIVED from what INTRODUCED the divergence, not from the
newest commit that touched the file: see introducing_commit(). "The parent of the
newest commit that touched the guard" is off by one the moment any later commit
touches it, and then the refusal hands the operator a base that refuses too (card
t_34ab7a00). The revision is read from git by the refusal, and the refusal says in
so many words that it did not RUN it, so no bite count is ever printed for a
revision this run did not measure.

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
PAGE_REL = "web/src/app/docs/mcp/page.tsx"

GUARD = os.path.join(REPO, GUARD_REL)
TOOLS = os.path.join(REPO, TOOLS_REL)
SIBLING = os.path.join(REPO, SIBLING_REL)
PAGE = os.path.join(REPO, PAGE_REL)

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


def git_text(*args):
    """git, WITHOUT stripping stdout — for comparing a file's bytes.

    git() above strips, which is right for a sha or a path and WRONG for a file
    body: every Go file ends with a newline, so a stripped body can never equal
    the on-disk text and a byte-equality test built on it is unsatisfiable at any
    byte distance (card t_34ab7a00). Use this one wherever the value read is the
    value compared.
    """
    p = subprocess.run(["git"] + list(args), cwd=REPO, capture_output=True, text=True)
    return p.returncode, p.stdout, p.stderr.strip()


ORIG_TOOLS = read(TOOLS)
ORIG_PAGE = read(PAGE)
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


def lift_page(tool):
    """The page's own `{ name, desc }` row for one tool, read with the guard's
    page pattern rather than hand-typed — the page's indentation is spaces, and a
    hand-typed row silently matched zero times (which is what this harness said
    the first time it ran)."""
    pat = re.compile(r'[ \t]*\{\s*name:\s*"%s",\s*desc:\s*"(?:[^"\\]|\\.)*"\s*\},'
                     % re.escape(tool))
    hits = pat.findall(ORIG_PAGE)
    if len(hits) != 1:
        bail("the page holds the %s row %d times, expected 1" % (tool, len(hits)))
    return hits[0]


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

    # THE PAGE'S OWN DUPLICATE RULE (card t_ce63518c). The page reader keys by tool
    # name, so before the fix two `{ name, desc }` rows for one tool collapsed to
    # whichever the regex matched LAST — silently, while the registry reader
    # refused the same shape. Inserting the stale row FIRST is the ordering that
    # made the old reader pick the WRONG sentence. The old reader (the base leg)
    # has no page-dup rule, so it stays green on the same mutation; the branch
    # must refuse it. The needles are the tool name and both row positions.
    page_pair = lift_page(PINNED[0])
    out.append((
        "a DUPLICATE tool row on the docs page, stale copy FIRST",
        {PAGE_REL: ORIG_PAGE.replace(
            page_pair,
            '{ name: "%s", desc: "STALE DUPLICATE ROW (bite proof)." },\n%s'
            % (PINNED[0], page_pair), 1)},
        {},
        (True,),                                   # the old reader has no page-dup rule
        (False, "holds the \"%s\" tool description twice" % PINNED[0]),
    ))

    out.append((
        "a tool file in a SUBdirectory of the registry package",
        {},
        {SUBDIR_REL: "// Package sub is a throwaway subdirectory written by a bite proof.\n"
                     "// It names the shipped model id deepseek-v4.1-flash:1017 so the repo walk\n"
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
    # The page is edited by the duplicate-row case; without this the stale row
    # would leak into every later leg (card t_ce63518c).
    if os.path.exists(PAGE):
        write(PAGE, ORIG_PAGE)
    for rel in set(edits) | set(added):
        if rel in (TOOLS_REL, SIBLING_REL, PAGE_REL):
            continue
        path = os.path.join(REPO, rel)
        if os.path.exists(path):
            os.remove(path)


# The token that says the guard READS THE REGISTRY AS A PACKAGE rather than out of
# one file (api/internal/llm/model_default_guard_test.go declares
# `const mcpRegistryDir = "api/internal/mcpsrv"`). It is the mark of the reader
# whose landings are the divergence this harness needs a base revision from, so the
# refusal derives the revision by reading it — the reader's own text — instead of
# by a file's commit date.
PACKAGE_READER_MARKER = "mcpRegistryDir"


def _file_at(rev, rel):
    """One file's text at one revision, or None when git cannot read it there."""
    rc, text, _ = git_text("show", "%s:%s" % (rev, rel))
    return text if rc == 0 else None


def introducing_commit(base_rev):
    """The commit that INTRODUCED the divergence in the guard, or None.

    The divergence is a property of the READER, not of a date, so it is read out of
    the guard's own text: the package-wide registry reader is the revision whose
    guard declares PACKAGE_READER_MARKER. Walk the guard's history at or before
    base_rev oldest-first and take the first revision carrying it — earlier ones
    without it, later ones with it, so that transition IS the commit whose parent
    still holds the single-file reader.

    NOT `git log -1 <base> -- <guard>^`, which is what this used to do: that is the
    parent of the NEWEST commit that touched the file, so any commit after the
    widening (a test-only widening, a rewrite) moves the suggestion one commit too
    far forward and the refusal hands the operator a base that refuses exactly as
    this run just did — a loop. Measured at origin/main 0c1c6e9c (card t_34ab7a00):
    it named eda839c1, the parent of 0c1c6e9c (#841, a model-id-scan widening), and
    eda839c1's guard still declares the marker, so it refuses again. It only ever
    landed on the right revision while the newest guard-touching commit happened to
    BE the widening.

    None means the transition is not attributable from base_rev's history — the
    oldest revision that touched the guard already carries the marker (the
    divergence is older than this base's history) — so the refusal says that plainly
    rather than naming a revision it has not checked.
    """
    rc, out, _ = git("rev-list", "--reverse", base_rev, "--", GUARD_REL)
    if rc != 0:
        return None
    revs = [h for h in out.splitlines() if h]
    if not revs:
        return None
    oldest_body = _file_at(revs[0], GUARD_REL)
    if oldest_body is None or PACKAGE_READER_MARKER in oldest_body:
        return None
    for rev in revs:
        body = _file_at(rev, GUARD_REL)
        if body is not None and PACKAGE_READER_MARKER in body:
            return rev
    return None


def capability_refusal(base_rev, base_full, base_guard, seen, cases_run):
    """The refusal a byte-equality test could not make: print it, name a usable base.

    It says the base CANNOT demonstrate the blind spot — no verdict, no bite count —
    and it NAMES the base to use instead by deriving it (introducing_commit) rather
    than guessing one. The named revision is then re-read from git, and it is only
    offered when its guard really is the pre-divergence reader: a suggestion that
    still reads the package is reported as unattributable instead of being handed
    over — that is the loop this run just fell into (card t_34ab7a00).

    NO COUNT IS PRINTED FOR A REVISION THIS RUN DID NOT RUN. The previous text
    closed with "at <suggested> the same tree is 7/7", a number copied from another
    card's run and false for the revision named here; every figure below is either
    measured by this run or explicitly marked unmeasured.
    """
    intro = introducing_commit(base_full)
    intro_short = intro[:12] if intro else ""
    suggest = None
    if intro:
        _, parent, _ = git("rev-parse", "%s^" % intro)
        if parent:
            suggest = parent
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
        parent_body = _file_at(suggest, GUARD_REL)
        if parent_body is not None and PACKAGE_READER_MARKER not in parent_body:
            print("Point BITEPROOF_BASE at the revision BEFORE that reader landed — the parent of "
                  "%s, the commit that introduced the package-wide registry reader in %s (the guard "
                  "there does not declare %s):"
                  % (intro_short, GUARD_REL, PACKAGE_READER_MARKER), flush=True)
            print("    BITEPROOF_BASE=%s python3 api/scripts/biteproof_model_default_guard.py" % suggest,
                    flush=True)
        else:
            print("The commit that introduced the package-wide registry reader is %s, but its "
                  "parent %s reads the registry as a package too (it still declares %s), so this "
                  "harness cannot name a pre-divergence revision from %s's history. Point "
                  "BITEPROOF_BASE at a revision where the single-file reader still lived."
                  % (intro_short, suggest[:12], PACKAGE_READER_MARKER, base_rev), flush=True)
            suggest = None
    else:
        print("Point BITEPROOF_BASE at a revision where the single-file reader still lived — this "
              "run could not attribute the change to one commit at %s (no revision in its history "
              "for %s reaches the transition into the package-wide reader, %s: the oldest revision "
              "that touched it already declares the marker)."
              % (base_rev, GUARD_REL, PACKAGE_READER_MARKER), flush=True)
    print("Measured by THIS run: the base reader at %s is %d chars, this tree's is %d chars, and "
          "%d/%d case(s) disagreed. No count for any other revision is asserted here — a revision "
          "this run did not execute was not measured, and a count for it has to come from running "
          "it." % (base_rev, len(base_guard), len(ORIG_GUARD), len(seen), len(CASES)), flush=True)
    if not suggest:
        print("None of the %d case(s) above was a disagreement, so there is no bite count to print "
              "for this base." % cases_run, flush=True)
    return 2


def main():
    rc, base_rev, err = git("rev-parse", "--short", BASE_REF)
    if rc != 0:
        print("cannot resolve BITEPROOF_BASE=%s: %s" % (BASE_REF, err), flush=True)
        return 2
    _, base_full, _ = git("rev-parse", BASE_REF)
    # The UNSTRIPPED text is read for the comparison; `base_guard` (stripped, for the
    # leg) is kept only for the diagnostics below. Testing a stripped copy against the
    # on-disk text is what made this fast path unreachable (see git_text).
    rc, base_guard_raw, err = git_text("show", "%s:%s" % (BASE_REF, GUARD_REL))
    if rc != 0:
        print("BASE READER MISSING: %s:%s could not be read (%s) - without it this harness "
              "cannot show what the one-file scope missed" % (BASE_REF, GUARD_REL, err), flush=True)
        return 2
    base_guard = base_guard_raw.strip()
    if base_guard_raw == ORIG_GUARD:
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
        return capability_refusal(base_rev, base_full, base_guard, disagreements, case_no)

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
