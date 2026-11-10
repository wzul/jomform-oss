#!/usr/bin/env python3
"""Bite proof for api/internal/llm/model_default_guard_test.go (card t_8853fc29).

WHAT IT PROVES. The guard's docs-parity half compares each pinned MCP tool
description (generate_form, generate_legal_page) against its entry on the
/docs/mcp surface. It used to READ THAT PAIR OUT OF ONE FILE
(`api/internal/mcpsrv/tools.go`), while the registry the rest of the repo agrees
on is the PACKAGE — #732 moved a tool literal (`disable_2fa`) into
parity_issue396.go, and both web guards were widened to `api/internal/mcpsrv/*.go`
for that reason. This harness sabotages a MOVE of one of the two PINNED literals
and requires the guard to answer about the registry rather than about one file.

THE TWO LEGS, per case. `BASE` is the reader BEFORE the widening — the single-file
reader — and `BRANCH` is the guard in the working tree. Each case carries its own
expectation per leg, so it can prove either "the old scope MISSED this" (BASE green,
BRANCH red) or "the old scope blamed the wrong file" (BASE red for a reason that is
not true, BRANCH green or red naming the real one). Needles are asserted against the
failure output, so a red in an unrelated assertion cannot be counted as a bite.

WHICH FILE HOLDS THE ROWS (card t_ee4114af). The rows used to be inline on
web/src/app/docs/mcp/page.tsx. Card t_0356ed4c split that page into a hub plus
four task pages and moved the rows into web/src/lib/mcpToolGroups.ts, which the
pages render; the guard now reads the whole surface. This harness follows the
same move — its row-list handle is the module — because a duplicate planted on the
hub would be a duplicate the guard never sees.

THE BASE IS DERIVED FROM THIS TREE, NOT CHECKED OUT (card t_e04ca37d). It used to be
a revision (`BITEPROOF_BASE`, default `origin/main`), and no revision can play that
part any more, for two MEASURED reasons:

  * `origin/main`'s guard has CAUGHT UP — it declares the package-wide reader too, so
    every BASE leg answers like its BRANCH leg. Measured 2026-11-09 at b13fa5450:
    `cases where the two readers DISAGREED: 3/8` and `BITES: 1/8`, where the one bite
    is the duplicate-row case — a different rule — so nothing in the output is about
    the registry scope;
  * the revision that PREDATES the widening is a FOSSIL whose OTHER pins are stale.
    The base the old refusal named, 8d1c52fbe, pins workingModel to the bare family id
    — the spelling without the size tag this tree ships ("...:1017") — so its control
    is NOT green on the UNMUTATED tree (TestDefaultModelIsNotRetired,
    TestEmptyModelResolvesToTheWorkingDefault,
    TestUserFacingStringsNameTheSameModelAsTheConstant) and every verdict under it
    would be about the model id, not about the reader.

No revision avoids both: `git log -S` puts the package-wide reader at f3c17fad5
(2026-09-28) and the current model id at 2505fbff6 (2026-10-18), the model id landed
AFTER the widening, and `git merge-base --is-ancestor 2505fbff6 f3c17fad5` says NO.
So the BASE leg is built the way web/scripts/biteproof_docs_index_comment_anchor.sh
builds its BEFORE column (card t_b5690577): THIS TREE'S GUARD WITH ONLY THE PRE-FIX
REGISTRY SCOPE PUT BACK — the body of toolDescriptions() replaced by the single-file
read of api/internal/mcpsrv/tools.go, and everything else (the model constants, the
/docs/mcp surface reader, every other rule) left exactly as this tree has it. The
substitution is count-checked in both directions (derive_base_guard) and the two
texts must differ, so the columns cannot silently collapse into one reader.

BITEPROOF_BASE still selects a REVISION base, for archaeology only. It is refused,
with no verdict, when that revision's other pinned constants disagree with this
tree's (see REFUSAL 2) — which is exactly what the fossil above does.

The base must ANSWER DIFFERENTLY from this tree's reader on at least one case, and
the harness checks that premise per run instead of trusting the bytes (REFUSAL 3).

THE PAIR IS DERIVED, NOT TYPED. Each mutation lifts the `Name:`/`Description:`
pair out of tools.go with the guard's own pattern and re-emits it into a sibling
file of the package as `func ...() *mcp.Tool { return &mcp.Tool{...} }` — the
shape #732 left `disable_2fa` in. Nothing about the sentence is hand-copied, so
this harness cannot drift from the source it sabotages. Every mutation must still
compile (`go build ./...` rc=0): a mutation that breaks the build would hide the
guard, and that is asserted per case rather than assumed.

REFUSALS, NOT A FALSE GREEN. Three refusals, each exit 2 with no verdict and no bite
count.

1. A BASE WHOSE OTHER CONSTANTS DISAGREE WITH THIS TREE (card t_e04ca37d). One BASE leg
answers about ONE divergence — the registry scope. A revision that ALSO pins a
different model id reds its own control on the unmutated tree, and every case under it
is then about the constant rather than about the reader: measured at 8d1c52fbe, whose
control printed `build rc=0, tree reader rc=0, base reader rc=1` on this tree. So the
harness compares the two readers' `const X = "..."` pins BEFORE anything runs and
refuses when they differ, naming each one. The derived base cannot trip this (it IS
this tree's text outside the reverted scope), so it fires only on BITEPROOF_BASE.

2. A BASE THAT IS BYTE-IDENTICAL to this tree's (revision path only): the comparison
would be one reader against itself. It reads the file with git_text() rather than
git(), because the general git() helper strips stdout and every Go file ends with a
newline — with a stripped base the test was unsatisfiable at any byte distance and the
path was dead code (card t_34ab7a00, measured at origin/main 272fda17, where the base
defaulted to the tree itself: 49788 bytes on disk, the stripped copy 49593 chars
against the tree's 49594, so it could not fire).

3. NO CASE MADE THE TWO READERS ANSWER DIFFERENTLY. This is the general rule, and it is
NOT a byte comparison: no case may pass unless the base reader ANSWERED DIFFERENTLY
from this tree's reader on it. A byte test cannot decide this premise — a base whose
guard differs by bytes can still READ THE SAME REGISTRY, so every BASE leg answers like
its BRANCH leg and the "missed this" column is empty; such a run would print
`0/7 BITES` for a premise that had quietly become "compare a reader with itself" (card
t_9df01abd, measured: base 22241 bytes vs tree 22242). The capability rule requires
positive evidence — at least one case whose BASE leg and BRANCH leg disagree, in their
green/red verdict or in the claim the guard made — and refuses otherwise.

NO REVISION IS RECOMMENDED BY THE REFUSAL ANY MORE (card t_e04ca37d). It used to name
the parent of the commit that introduced the package-wide reader; that revision
(8d1c52fbe) is now REFUSED by rule 1 for pinning the older model id, so handing it over
would loop exactly as the t_34ab7a00 suggestion did. The remedy is to DROP
BITEPROOF_BASE: the default base is derived from this tree, so it cannot be caught up
and cannot carry a stale constant.

Restore is signal-safe: originals are held in memory, restored in a finally, and a
SIGTERM/SIGINT handler restores before exiting. Every restore is verified by
sha256 rather than asserted, and the run ends by comparing `git status` BEFORE and
AFTER its own mutations — the tree is expected to be dirty (it is the branch that
changes the guard), so what must not survive is anything this run ADDED.

Run:  python3 api/scripts/biteproof_model_default_guard.py
(with NO BITEPROOF_BASE — the default base is the derived one. BITEPROOF_BASE is
archaeology only, and a revision base is refused when its other pins disagree with
this tree's or when its guard already reads the registry as a package.)
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
ROWS_REL = "web/src/lib/mcpToolGroups.ts"

GUARD = os.path.join(REPO, GUARD_REL)
TOOLS = os.path.join(REPO, TOOLS_REL)
SIBLING = os.path.join(REPO, SIBLING_REL)
ROWS = os.path.join(REPO, ROWS_REL)

# Empty (the default) means DERIVE the base from this tree — card t_e04ca37d, see
# DERIVED BASE below. A non-empty value is a REVISION base, kept for archaeology: it
# is refused, with no verdict, when that revision's other pinned constants disagree
# with this tree's (REFUSAL 1).
BASE_REF = os.environ.get("BITEPROOF_BASE", "").strip()
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


# The BASE reader's own text and, for a REVISION base, the pre-split docs page that
# reader was written to read; both are resolved by main(). A leg is only an answer
# about a READER if that reader is run against the tree it was written for, so
# run_base_leg installs both and always puts both back — even when the leg raises. A
# DERIVED base (the default) needs no page swap: it IS this tree's guard, so it reads
# this tree's docs shape.
BASE_GUARD = None
BASE_PAGE_TEXT = None


def run_base_leg(cmd):
    """Run `cmd` with the BASE reader (and, for a revision base, its own docs page)."""
    write(GUARD, BASE_GUARD)
    if BASE_PAGE_TEXT is not None:
        write(BASE_DOCS_PAGE, BASE_PAGE_TEXT)
    try:
        return run(cmd)
    finally:
        if BASE_PAGE_TEXT is not None:
            write(BASE_DOCS_PAGE, ORIG_BASE_PAGE)
        write(GUARD, ORIG_GUARD)


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
ORIG_ROWS = read(ROWS)
ORIG_SIBLING = read(SIBLING)
ORIG_GUARD = read(GUARD)

# THE DOCS PAGE A REVISION BASE'S READER READS (card t_ee4114af).
#
# A pre-split reader cannot read a post-split tree. origin/main's guard looked for
# TOOL_GROUPS on web/src/app/docs/mcp/page.tsx; on a branch that has split
# /docs/mcp into a hub plus four task pages, that file is the hub and holds no
# rows, so the BASE leg went red on the UNMUTATED tree for a reason that says
# nothing about the reader (measured: CONTROL NOT GREEN, exit 1). A REVISION base
# therefore runs against the docs page AS OF that revision — the tree its reader was
# written for — and this tree's page is put back immediately after that leg.
#
# The DERIVED base (the default, card t_e04ca37d) needs none of this: it is this
# tree's guard with one function body reverted, so it reads this tree's docs shape by
# construction and BASE_PAGE_TEXT stays None.
BASE_DOCS_PAGE_REL = "web/src/app/docs/mcp/page.tsx"
BASE_DOCS_PAGE = os.path.join(REPO, BASE_DOCS_PAGE_REL)
ORIG_BASE_PAGE = read(BASE_DOCS_PAGE)

BEFORE = {TOOLS: sha(TOOLS), SIBLING: sha(SIBLING), GUARD: sha(GUARD),
          BASE_DOCS_PAGE: sha(BASE_DOCS_PAGE)}
# The tree is EXPECTED to be dirty (this harness runs on the branch that changes
# the guard), so the run is judged on what it ADDED to git status, not on the
# tree being clean.
_, STATUS_BEFORE, _ = git("status", "--porcelain")
STATUS_BEFORE = sorted(STATUS_BEFORE.splitlines())
DONE = False


def bail(msg):
    print("HARNESS BUG: %s" % msg, flush=True)
    sys.exit(2)


# ---- THE DERIVED BASE (the default since card t_e04ca37d) ---------------------
#
# The BASE column is the PRE-FIX READER RE-BASED ONTO THIS TREE: this tree's guard with
# ONE function body reverted to the single-file registry scope, and nothing else — which
# is the shape web/scripts/biteproof_docs_index_comment_anchor.sh uses for its BEFORE
# column (card t_b5690577), and for the same reason: every pre-fix revision is a fossil
# whose OTHER pins red today's tree on an unmutated control (measured here at 8d1c52fbe:
# its workingModel carries the bare family id, WITHOUT the "...:1017" size tag this tree
# ships, and the base reader is rc=1 on the unmutated tree).
#
# The substitution is COUNT-CHECKED: the doc comment and the function signature must
# each appear exactly once, the derived text must not call mcpToolFiles, must still
# define it (so no import goes unused and the file still compiles), and must differ from
# the tree's text. Any of those failing is a HARNESS BUG with no verdict, never a
# silently identical reader.
DERIVED_DOC_FIRST_LINE = (
    "// toolDescriptions returns the tool name\u2192description pairs held by the MCP tool\n")
DERIVED_SIGNATURE = (
    "func toolDescriptions(t *testing.T, hits map[string]map[string]string, root string) "
    "map[string]string {")
DERIVED_SINGLE_FILE_ANCHOR = 'const rel = "api/internal/mcpsrv/tools.go"'
PACKAGE_WALK_CALL = "files := mcpToolFiles(t, hits)"
# The declaration that marks a reader as the POST-fix (package-wide) one. Used only to
# refuse a REVISION base that has caught up; the derived base legitimately still holds it
# (the walker stays defined so no import goes unused) and never calls it.
PACKAGE_READER_DECL = "const mcpRegistryDir ="

# The pre-fix body, in this tree's own idioms: parseDescriptions() with the same pattern,
# the same "the pattern matched nothing" vacuity guard, and the pre-fix scope's own
# anchor on tools.go writing the model id (which is what made the old reader blame
# tools.go when a literal moved out of it).
DERIVED_FUNCTION = '''// toolDescriptions returns the tool name\u2192description pairs held by the MCP tool
// registry AS THE PRE-FIX READER SCOPE SAW IT: one file, api/internal/mcpsrv/tools.go.
//
// This body is the ONLY thing the bite proof
// (api/scripts/biteproof_model_default_guard.py) changes about this tree's guard when
// it builds the BASE reader, so the two columns differ by exactly the registry scope
// this guard was widened past.
func toolDescriptions(t *testing.T, hits map[string]map[string]string, root string) map[string]string {
\tt.Helper()
\tconst rel = "api/internal/mcpsrv/tools.go"
\tif _, ok := hits[rel]; !ok {
\t\tt.Fatalf("%s writes no model id \u2014 the tool descriptions the docs must match have moved or been rewritten", rel)
\t}
\tout, err := parseDescriptions(filepath.Join(root, rel), goDescriptionPattern)
\tif err != nil {
\t\tt.Fatalf("read %s: %v \u2014 the docs-parity half of this guard cannot run without the registry", rel, err)
\t}
\tif len(out) == 0 {
\t\tt.Fatalf("no tool name/description pair parsed from %s \u2014 the pattern matched nothing, so the docs-parity assertion would pass vacuously", rel)
\t}
\treturn out
}
'''


def derive_base_guard(tree_text):
    """(base_text, note): this tree's guard with ONLY the registry scope reverted.

    See the block above for why a revision cannot serve as the BASE column. Every
    precondition is counted, so a guard that is rewritten around this handle makes the
    harness say HARNESS BUG rather than compare a reader with itself.
    """
    for needle, what in ((DERIVED_DOC_FIRST_LINE, "the toolDescriptions doc comment"),
                         (DERIVED_SIGNATURE, "the toolDescriptions signature")):
        n = tree_text.count(needle)
        if n != 1:
            bail("%s appears %d times in %s, expected exactly 1 — the derivation below "
                 "anchors on it, and a guard rewritten around this handle would produce a "
                 "BASE reader that is not the pre-fix scope"
                 % (what, n, GUARD_REL))
    lines = tree_text.splitlines(keepends=True)
    start = next(i for i, line in enumerate(lines) if line == DERIVED_DOC_FIRST_LINE)
    func = next(i for i, line in enumerate(lines) if line.startswith("func toolDescriptions("))
    end = None
    for i in range(func, len(lines)):
        if lines[i].rstrip("\n") == "}":      # gofmt puts the file's top-level } at column 0
            end = i
            break
    if end is None:
        bail("cannot find the closing brace of toolDescriptions() in %s" % GUARD_REL)
    removed = lines[start:end + 1]
    derived = "".join(lines[:start]) + DERIVED_FUNCTION + "".join(lines[end + 1:])

    # Count-checked BOTH directions: the pre-fix scope IS in the derived text, the
    # package walk is NOT called by it, and the walker itself is still defined so the
    # file's imports stay used and the leg compiles.
    if derived.count(DERIVED_SINGLE_FILE_ANCHOR) != 1:
        bail("the derived BASE reader does not hold the single-file anchor %r exactly once"
             % DERIVED_SINGLE_FILE_ANCHOR)
    if PACKAGE_WALK_CALL in derived:
        bail("the derived BASE reader still calls %r, so it was not reverted" % PACKAGE_WALK_CALL)
    if tree_text.count(PACKAGE_WALK_CALL) != 1 or derived.count("func mcpToolFiles(") != 1:
        bail("the package walker is not exactly one call and one definition in the tree's "
             "guard, so reverting the CALL would either leave the scope widened or make an "
             "import unused (%s)" % GUARD_REL)
    if derived == tree_text:
        bail("the derived BASE reader is byte-identical to this tree's — the substitution "
             "did not take, and every case would compare one reader with itself")
    pinned = pinned_constants(tree_text)
    if pinned_constants(derived) != pinned:
        bail("the derivation changed a pinned constant (%s); it must revert the registry "
             "SCOPE and nothing else" % GUARD_REL)
    return derived, ("derived from this tree (%s): the toolDescriptions body reverted to "
                     "the single-file reader of api/internal/mcpsrv/tools.go, %d line(s) "
                     "replaced by %d" % (GUARD_REL, len(removed),
                                         DERIVED_FUNCTION.count("\n")))


# A pin is a top-level `const X = "..."` / `var X = "..."`: the values a revision's guard
# can silently hold at an older version of the tree (the model id is one, and it is what
# made 8d1c52fbe's control red). Only plain string literals are compared — a concatenated
# or computed pin is not something this check can read, and the docstring says so.
PINNED_CONST_RE = re.compile(
    r'^(?:const|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"((?:[^"\\]|\\.)*)"\s*$', re.M)


def pinned_constants(text):
    return dict(PINNED_CONST_RE.findall(text))


def constants_disagreeing(base_text):
    """[(name, tree_value, base_value)] for pins this tree HAS and the base spells differently."""
    tree = pinned_constants(ORIG_GUARD)
    base = pinned_constants(base_text)
    return [(name, tree[name], base[name]) for name in sorted(tree)
            if name in base and base[name] != tree[name]]


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
    """The row list's own `{ name, desc }` row for one tool, read with the guard's
    page pattern rather than hand-typed — the rows are space-indented, and a
    hand-typed row silently matched zero times (which is what this harness said
    the first time it ran)."""
    pat = re.compile(r'[ \t]*\{\s*name:\s*"%s",\s*desc:\s*"(?:[^"\\]|\\.)*"\s*\},'
                     % re.escape(tool))
    hits = pat.findall(ORIG_ROWS)
    if len(hits) != 1:
        bail("the row list holds the %s row %d times, expected 1" % (tool, len(hits)))
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
        # The needle the PRE-FIX (single-file) reader prints for a literal that LEFT
        # tools.go: the guard's own "the registry holds no such description" fatal,
        # whose subject is now the PACKAGE the old reader never read. That is the false
        # blame this case is about, so the needle is the false claim itself.
        missing = 'holds no "%s" tool description' % tool
        twice = 'holds the "%s" tool description twice' % tool
        differs = "differs between the registry (api/internal/mcpsrv)"

        out.append((
            "MOVE %s out of tools.go, sentence unchanged" % tool,
            {TOOLS_REL: tools_without(tool), SIBLING_REL: ORIG_SIBLING + moved},
            {},
            (False, missing),        # one file: the package "holds no such description"
            (True,),                 # the package reader answers correctly
        ))

        out.append((
            "MOVE %s, stale copy left in tools.go, moved sentence edited" % tool,
            {SIBLING_REL: ORIG_SIBLING + moved_edited},
            {},
            (True,),                 # one file compares the STALE copy: green, wrong
            (False, twice),          # the package reader refuses to pick one of two
        ))

        out.append((
            "MOVE %s, no copy behind, moved sentence edited" % tool,
            {TOOLS_REL: tools_without(tool), SIBLING_REL: ORIG_SIBLING + moved_edited},
            {},
            (False, missing),        # one file: the same false claim about the package
            (False, differs, "(bite proof)"),   # names the real disagreement
        ))

    # THE ROW LIST'S OWN DUPLICATE RULE (card t_ce63518c). The docs reader keys by tool
    # name, so before the fix two `{ name, desc }` rows for one tool collapsed to
    # whichever the regex matched LAST — silently, while the registry reader refused the
    # same shape. Inserting the stale row FIRST is the ordering that made the old reader
    # pick the WRONG sentence.
    #
    # BOTH LEGS ARE EXPECTED RED SINCE card t_e04ca37d, and that is not a weaker case:
    # the BASE reader is now this tree's guard with only the REGISTRY scope reverted, so
    # it carries the page-dup rule (a different fix, already landed) and refuses the
    # duplicate too. This case therefore proves the rule is still enforced on this tree's
    # docs surface; it is not a base/branch divergence, and the run needs the divergence
    # from the registry cases (see the DISAGREED line — 7/8, this one excluded).
    page_pair = lift_page(PINNED[0])
    out.append((
        "a DUPLICATE tool row in the row list, stale copy FIRST",
        {ROWS_REL: ORIG_ROWS.replace(
            page_pair,
            '{ name: "%s", desc: "STALE DUPLICATE ROW (bite proof)." },\n%s'
            % (PINNED[0], page_pair), 1)},
        {},
        (False, 'holds the "%s" tool description twice' % PINNED[0]),
        (False, 'holds the "%s" tool description twice' % PINNED[0]),
    ))

    out.append((
        "a tool file in a SUBdirectory of the registry package",
        {},
        {SUBDIR_REL: "// Package sub is a throwaway subdirectory written by a bite proof.\n"
                     "// It names the shipped model id deepseek-v4.1-flash:1017 so the repo walk\n"
                     "// sees the file and the registry reader has to decide about it.\n"
                     "package sub\n"},
        (True,),                     # one file never looked
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
    write(BASE_DOCS_PAGE, ORIG_BASE_PAGE)
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
    # The row list is edited by the duplicate-row case; without this the stale row
    # would leak into every later leg (card t_ce63518c).
    if os.path.exists(ROWS):
        write(ROWS, ORIG_ROWS)
    for rel in set(edits) | set(added):
        if rel in (TOOLS_REL, SIBLING_REL, ROWS_REL):
            continue
        path = os.path.join(REPO, rel)
        if os.path.exists(path):
            os.remove(path)


def capability_refusal(source, label, base_guard, seen, cases_run):
    """REFUSAL 3: no case made the two readers answer differently.

    It says the base CANNOT demonstrate the blind spot — no verdict, no bite count.

    It names NO revision. This refusal used to derive one from the guard's history
    (the parent of the commit that introduced the package-wide reader), and since card
    t_e04ca37d that revision is a fossil this harness REFUSES outright (REFUSAL 1: it
    pins the older model id, so its control reds on the unmutated tree). Repeating the
    suggestion would loop exactly as the t_34ab7a00 suggestion did, so the remedy it
    prints is the DERIVED base — the default when BITEPROOF_BASE is unset.

    NO COUNT IS PRINTED FOR A REVISION THIS RUN DID NOT RUN. Every figure below is
    measured by this run; the previous text closed with a figure copied from another
    card's run, which was false for the revision it named.
    """
    print(flush=True)
    print("REFUSING (exit 2, NO VERDICT): not one of the %d case(s) run so far made the base "
          "reader answer differently from this tree's. Every BASE leg is the same reader as its "
          "BRANCH leg, so the 'missed this' column is empty by construction and a bite count "
          "would be a statement about this instrument, not about the guard." % cases_run, flush=True)
    if source == "derived":
        print("The base was DERIVED here (%s), so a reader that answers exactly like the tree's "
              "means the derivation did not revert the registry scope. That is a HARNESS BUG: "
              "derive_base_guard() is count-checked and refuses to hand over a text that still "
              "calls mcpToolFiles, so check the case expectations before trusting any count."
              % label, flush=True)
    else:
        print("That is what a base whose reader has CAUGHT UP to this branch looks like: the "
              "package-wide registry reader lives there too, so the two files differ by bytes "
              "while the reader does not differ. A REVISION base can only answer this question "
              "while its reader is the pre-widening one (%s: %s)." % (label, BASE_REF), flush=True)
        print("Point it at a revision where the single-file reader still lived, or — better — "
              "drop BITEPROOF_BASE entirely: the default base is DERIVED from this tree "
              "(this tree's guard with ONLY the toolDescriptions body reverted), so it cannot "
              "be caught up and cannot carry a stale constant.", flush=True)
    print("Measured by THIS run: the base reader is %d chars, this tree's is %d chars, and "
          "%d/%d case(s) disagreed. No count for any other revision is asserted here — a "
          "revision this run did not execute was not measured."
          % (len(base_guard), len(ORIG_GUARD), len(seen), len(CASES)), flush=True)
    return 2


def constants_refusal(label, pairs):
    """REFUSAL 1: a revision base whose OTHER pins disagree with this tree.

    Prints it and returns exit 2. Measured: 8d1c52fbe (the parent of the commit that
    widened the reader) pins workingModel to the bare family id — no "...:1017" size
    tag — while this tree ships the tagged id, so its base reader answers rc=1 on the
    UNMUTATED tree and every case under it is about the model id rather than about the
    registry scope.
    """
    print(flush=True)
    print("REFUSING (exit 2, NO VERDICT): the guard at %s pins %d constant(s) to values this "
          "tree deliberately changed:" % (label, len(pairs)), flush=True)
    for name, tree_value, base_value in pairs:
        print("    %s: base %r / this tree %r" % (name, base_value, tree_value), flush=True)
    print("One BASE leg answers about ONE divergence — the registry scope. This revision's "
          "control leg reds on the value(s) above on an UNMUTATED tree, so every verdict under "
          "it would be a statement about the constant rather than about the reader. That is the "
          "measured shape of 8d1c52fbe: `build rc=0, tree reader rc=0, base reader rc=1`, red on "
          "TestDefaultModelIsNotRetired, TestEmptyModelResolvesToTheWorkingDefault and "
          "TestUserFacingStringsNameTheSameModelAsTheConstant. No bite count is printed for it.",
          flush=True)
    print("Drop BITEPROOF_BASE: the default base is DERIVED from this tree (this tree's guard "
          "with ONLY the toolDescriptions body reverted), so its other pins are this tree's by "
          "construction and cannot make the control red.", flush=True)
    return 2


def main():
    global BASE_GUARD, BASE_PAGE_TEXT
    base_note = ""
    if BASE_REF:
        source = "revision"
        rc, base_rev, err = git("rev-parse", "--short", BASE_REF)
        if rc != 0:
            print("cannot resolve BITEPROOF_BASE=%s: %s" % (BASE_REF, err), flush=True)
            return 2
        _, base_full, _ = git("rev-parse", BASE_REF)
        label = "%s (%s)" % (BASE_REF, base_full[:12])
        # The UNSTRIPPED text is read for the comparison; `base_guard` (stripped, for the
        # leg) is kept only for the diagnostics below. Testing a stripped copy against the
        # on-disk text is what made this fast path unreachable (see git_text).
        rc, base_guard_raw, err = git_text("show", "%s:%s" % (BASE_REF, GUARD_REL))
        if rc != 0:
            print("BASE READER MISSING: %s:%s could not be read (%s) - without it this harness "
                  "cannot show what the one-file scope missed" % (BASE_REF, GUARD_REL, err),
                  flush=True)
            return 2
        base_guard = base_guard_raw.strip()
        if base_guard_raw == ORIG_GUARD:
            print("REFUSING: the guard at %s is BYTE-IDENTICAL to this tree's (%s). Every case "
                  "compares the base reader against the working-tree reader, so here that would be "
                  "one reader against itself and the 'missed this' column would prove nothing. "
                  "Drop BITEPROOF_BASE: the default base is DERIVED from this tree."
                  % (label, BEFORE[GUARD][:12]), flush=True)
            return 2
        # REFUSAL 1: a base can only answer about ONE divergence (card t_e04ca37d).
        pairs = constants_disagreeing(base_guard)
        if pairs:
            return constants_refusal(label, pairs)
        # REFUSAL 1b: a revision whose guard already READS THE PACKAGE is this tree's
        # reader, not the pre-fix one, so its BASE legs would answer like their BRANCH
        # legs by construction. Measured at origin/main b13fa5450 with this case set:
        # `cases where the two readers DISAGREED: 3/8`, `BITES: 1/8` — and that one bite
        # is the duplicate-row case, a different rule. Refuse on the bytes that declare
        # the scope instead of paying eight cases to rediscover it.
        if PACKAGE_WALK_CALL in base_guard or PACKAGE_READER_DECL in base_guard:
            print(flush=True)
            print("REFUSING (exit 2, NO VERDICT): the guard at %s already reads the MCP registry "
                  "as a PACKAGE (it declares %r and calls mcpToolFiles), which is this tree's "
                  "reader rather than the pre-fix one. Every BASE leg would answer like its BRANCH "
                  "leg and the 'missed this' column would be a property of this instrument, not of "
                  "the guard. Measured at origin/main b13fa5450 with this case set: `cases where "
                  "the two readers DISAGREED: 3/8`, `BITES: 1/8`, and that one bite is the "
                  "duplicate-row case — a different rule."
                  % (label, PACKAGE_READER_DECL), flush=True)
            print("Drop BITEPROOF_BASE: the default base is DERIVED from this tree (this tree's "
                  "guard with ONLY the toolDescriptions body reverted), so it cannot have caught "
                  "up.", flush=True)
            return 2
        rc, base_page_raw, err = git_text("show", "%s:%s" % (BASE_REF, BASE_DOCS_PAGE_REL))
        if rc != 0:
            print("BASE DOCS PAGE MISSING: %s:%s could not be read (%s) - a revision base whose "
                  "reader predates the /docs/mcp split reads THAT page, so without it its leg "
                  "cannot answer anything and no verdict is printed"
                  % (BASE_REF, BASE_DOCS_PAGE_REL, err), flush=True)
            return 2
        BASE_PAGE_TEXT = base_page_raw
    else:
        # THE DEFAULT (card t_e04ca37d): this tree's guard with only the registry scope
        # reverted. Count-checked inside derive_base_guard; a failure there is a HARNESS BUG
        # with no verdict.
        source = "derived"
        base_rev = "derived"
        base_guard, base_note = derive_base_guard(ORIG_GUARD)
        label = "derived from this tree (pre-fix registry scope)"
    BASE_GUARD = base_guard
    print("base reader: %s, %d bytes (%d chars); tree reader: %d bytes (%d chars)"
          % (label, len(base_guard.encode()), len(base_guard),
             len(ORIG_GUARD.encode()), len(ORIG_GUARD)), flush=True)
    if source == "derived":
        print("base provenance: %s" % base_note, flush=True)
    else:
        print("base tree shape for the BASE leg: %s at %s is %d bytes; this tree's is %d%s"
              % (BASE_DOCS_PAGE_REL, label, len((BASE_PAGE_TEXT or "").encode()),
                 len(ORIG_BASE_PAGE.encode()),
                 "" if BASE_PAGE_TEXT == ORIG_BASE_PAGE else
                 " - DIFFERENT, so the BASE leg runs against the page its reader was written for"),
              flush=True)
    print("cases: %d%s" % (len(CASES), "" if not ADDED_PATHS else
                           " (+%d file(s) created and removed)" % len(ADDED_PATHS)), flush=True)

    run(["go", "build", "./..."])
    brc, _ = run(["go", "build", "./..."])
    trc, tout = run(["go", "test", TEST_PKG, "-count=1"])
    btrc, bout = run_base_leg(["go", "test", TEST_PKG, "-count=1"])
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

        brc2, bout2 = run_base_leg(["go", "test", TEST_PKG, "-count=1"])
        ok_base, note_base = judge(bout2, brc2, base_expect)

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
    for path in (TOOLS, SIBLING, GUARD, BASE_DOCS_PAGE):
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
        return capability_refusal(source, label, base_guard, disagreements, case_no)

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
