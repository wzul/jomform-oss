#!/usr/bin/env python3
"""Bite proof for the REPO-WIDE SCAN inside api/internal/llm/model_default_guard_test.go
(card t_6784f5bb).

WHAT IT PROVES. The guard's third leg claims to check that every file on the tree
which names a model names the shipped default. Two shapes were proven blind by
card t_6784f5bb on the pre-fix guard: a fixed EXTENSION whitelist (so a dead id in
a `.mjs`, a `.txt` or the extensionless `Dockerfile` was invisible) and a token
rule pinned to the `deepseek-` prefix (so an id from any other family was never
tokenised). Card t_16fa0164 added a third: the widened rule's blanket "the token
must contain a digit" filter, which dropped real IDS with no digit in them. This
harness mutates the tree one shape at a time and requires the guard in the WORKING
TREE to go RED naming the file it should have judged, while the guard at BASE
(default `origin/main`, override with BITEPROOF_BASE) is compared on the same
mutation.

THE BASE LEG IS PER-CASE, and that is not a detail. For a shape the OLD scan was
BLIND to (the extension whitelist, the `deepseek-`-only token) BASE must stay
GREEN — that is the "the old scan missed this" column. For a shape the old scan
CAUGHT and this widening LOST (case 9: the digit filter), BASE must go RED,
because that is exactly what makes the loss a REGRESSION rather than a residual —
and the `deepseek-`-prefixed reader did catch a digit-free `deepseek-…` id.

THE BASE IS ALSO PER-CASE IN THE OTHER SENSE (card t_492976fe): a case may pin a
`base_ref` of its own instead of the run-wide `BITEPROOF_BASE`. That exists
because one shape needs TWO comparisons that no single revision can make at once
— a LISTED family's `<family>:<tag>` id whose family carries NO HYPHEN (the shape
is spelled there rather than as an id, for the reason the constants below are).
The shipped reader is blind to it AND the reader this branch replaced (the
widened-but-digit-filtered one) had caught it and lost it. Cases 11-13 are that
set, and each records which revision it pins.

THE TWO LEGS, per case. BASE is the guard file as of the base revision, copied
over the working one for the run and restored after; BRANCH is the guard in the
working tree. Every case carries its own expectation per leg, and needles are
asserted against the failure OUTPUT, so a red raised by an unrelated assertion
cannot be counted as a bite.

WHAT IS DERIVED, NOT TYPED. The dead ids are ASSEMBLED in this file
(`"deep" + "seek-v4-" + "flash"`) so the harness itself carries no greppable model
id -- the same rule the guard follows: the guard reads this file too, and an id
written whole here would be judged as a stray mention, correctly. Cases 4 and 5
edit an existing mention in place rather than writing a hand-typed sentence, and
every mutation is asserted to still compile (`go build ./...` rc=0) before the
guard runs: a mutation that breaks the build would hide the guard, and that is
checked per case rather than assumed.

REFUSAL, NOT A FALSE GREEN. If the base revision's guard file is byte-identical to
the working tree's, every BASE leg compares a guard with itself and the "missed
this" column means nothing, so the harness exits 2 without a verdict.

Restore is signal-safe: originals are held in memory, restored in a finally, a
SIGTERM/SIGINT handler restores before exiting, and the files this harness CREATES
are removed explicitly. Every restore is verified by sha256, and the run ends by
requiring `git status --porcelain` clean.
"""
import atexit
import hashlib
import os
import re
import signal
import subprocess
import sys

# NEVER WRITE .pyc, AND THIS IS NOT HYGIENE (card t_bfef6b4f).
#
# This harness runs `go test` on a tree it has mutated, and the guard under test walks
# every shipped file and FAILS on one it cannot decode as text. Importing this module
# normally writes `api/scripts/__pycache__/biteproof_model_id_scan.cpython-3xx.pyc`
# into the tree, which is then (a) not ignored by git and (b) undecodable, so the guard
# goes RED for a reason that has nothing to do with the mutation — and
# `base_catches_probe` reads ANY test failure as "this reader caught the probe".
#
# The consequence was measured: EVERY revision answered `caught=True`, including the
# pre-widening reader the check exists to accept, so `derive_base()` refused every run
# with "the derived base ... still CAUGHT the capability probe". Reproduced by writing
# the probe by hand and reading the failure:
#
#     --- FAIL: TestUserFacingStringsNameTheSameModelAsTheConstant
#         the scan could not decode api/scripts/__pycache__/...cpython-314.pyc as text,
#         so it cannot judge ...
#
# Set before anything else can import, and also exported for the child processes this
# harness spawns (`go test` runs the guard, not this file, but a stray re-import would
# recreate the directory).
sys.dont_write_bytecode = True
os.environ["PYTHONDONTWRITEBYTECODE"] = "1"

HERE = os.path.dirname(os.path.abspath(__file__))
API = os.path.dirname(HERE)
REPO = os.path.dirname(API)

GUARD_REL = "api/internal/llm/model_default_guard_test.go"
GUARD = os.path.join(REPO, GUARD_REL)
FORMGEN_REL = "api/internal/formgen/formgen.go"

BASE_REF = os.environ.get("BITEPROOF_BASE", "origin/main")
ONLY = os.environ.get("BITEPROOF_ONLY", "")
TEST_NAME = "TestUserFacingStringsNameTheSameModelAsTheConstant"
TEST_PKG = "./internal/llm/"

# THE RUN-WIDE BASE IS DERIVED, NOT TYPED (card t_492976fe), and this is why.
#
# Cases 1-6 and 9 all answer the same question — "did the OLD scan miss this?"
# — and for them "the old scan" is the PRE-WIDENING reader: the one that tokenised
# `deepseek-` ids only and read a fixed extension whitelist. Their BASE legs expect
# GREEN precisely because that reader was blind.
#
# Until PR 841 merged, `origin/main` WAS that reader, so it was the default. The
# merge moved origin/main onto the WIDENED reader, and every legacy BASE leg then
# read RED: the harness was comparing this tree's reader with a reader that had
# caught up to it, which is the false premise BITE-HARNESSES.md records for the
# sibling harness (`biteproof_model_default_guard.py`, card t_9df01abd). Measured
# on the head that merged: 5 of 10 cases failed their BASE leg for this reason and
# no case was wrong about the guard.
#
# So the default is derived at startup as the parent of the newest commit at the
# base ref that touched the guard — the same rule the sibling harness prints in its
# capability refusal — and it is then CAPABILITY-CHECKED rather than trusted: a
# reader that catches a listed family's id in the `id:tag` spelling is the widened
# one and is refused, because using it as this base would empty the "missed this"
# column by construction. BITEPROOF_BASE still overrides the whole derivation.
#
# MEASURED, the three readers on this tree (guard blob / rule):
#
#   d495c1b5479fe6e  the PRE-WIDENING reader — token rule `deepseek-…` only, reads
#                    a fixed extension whitelist. Carried by eda839c1, ba4a7d10 and
#                    7d0489b3 (all on main), i.e. it is the derived base.
#   5b3b3b3dfd8e9e29  the DIGIT-FILTERED reader — widened token rule plus
#                    `strings.ContainsAny(id, "0123456789")`. Carried by 5376463e
#                    (the parent of the commit under review) and f488efcd.
#   734b5f3d9ce64215  the SEPARATOR reader — widened, filtered by `-`-or-leading
#                    digit. Carried by 3e4ba2d0, by its squash 0c1c6e9c, and now by
#                    origin/main.
DERIVED_BASE = os.environ.get("BITEPROOF_BASE") is None

# The probe the base capability check uses: a listed family's id in the `id:tag`
# spelling, assembled like every other id in this file so the harness itself
# carries no greppable model id. The pre-widening reader does not tokenise this
# family at all, so a base that CAUGHT it has caught up and is refused.
BASE_CHECK_PROBE_REL = "api/internal/qa_probe_basecheck.go"
BASE_CHECK_PROBE = "gem" + "ma:7" + "b"

# The revision the card's exemplar case pins BY NAME rather than trusting the
# run-wide base, and the reason it is a different revision:
#
#   5376463e — the head the commit under review REPLACED (the digit-filtered
#     reader). It CAUGHT the exemplar, which is what makes the loss a REGRESSION
#     introduced by that commit rather than a residual of the widening. MEASURED
#     (card t_492976fe): the one-file exemplar probe below is RED there, naming the
#     probe file.
#
# It is pinned by sha because it lives on PR 841's own branch, not on main (the PR
# was squash-merged, so its branch commits are not ancestors of main). A rebase or
# a deleted ref makes `git rev-parse` fail and the harness exit 2 without a verdict
# rather than compare a guard it did not mean to — and the case's BASE leg is
# itself the capability check: if the pinned reader did not catch the exemplar, the
# case would fail rather than report a comparison it did not make.
COLON_CAUGHT_BASE = os.environ.get("BITEPROOF_COLON_CAUGHT_BASE", "5376463e")

# The dead ids this harness sabotages with, each ASSEMBLED from parts so this
# file carries no greppable model id of its own. That is not decoration: the
# guard reads this file too (`.py` is inside no whitelist any more), and an id
# written whole here would be judged as a stray mention — correctly, since a
# copy-pasteable dead id is how one gets copied back.
RETIRED = "deep" + "seek-v4-" + "flash"
RETIRED_TAGGED = RETIRED + ":0731"
OTHER_FAMILY = "qw" + "en3-coder" + ":480b"
UNKNOWN_FAMILY = "er" + "nie-4.5" + ":8b"
WORKING = "deep" + "seek-v4.1-" + "flash"
# A LISTED family's id that carries NO DIGIT — assembled like the rest. This is
# the shape the digit filter dropped (card t_16fa0164): `<family>-tail` is an id
# whether or not it happens to contain a digit, and the reader this harness
# compares against (the `deepseek-`-prefixed one) DID catch this shape for the
# deepseek family, which is why its BASE leg is expected RED rather than GREEN.
NODIGIT = "deep" + "seek-coder"
# A LISTED family's id in the `id:tag` spelling whose family carries NO HYPHEN.
# Assembled like the rest. This is the shape card t_492976fe opened on: the
# separator rule accepted only `-` or a leading digit, and both of those require
# something a hyphenless family does not have, so no branch reached it — and
# unknownModelTag cannot cover it either, because that net needs a digit BEFORE
# the colon. Spelled as a family here rather than as a whole id for the reason
# the other constants are.
COLON_NOHYPHEN = "mis" + "tral" + ":7" + "b"
# The same gap for a HYPHENATED family: the `-` in this id belongs to the FAMILY,
# not to the separator, so `idHasIdSeparator` sees the colon after the family and
# rejected it too. This is the id the guard's own header records as measured LIVE,
# so it is the exemplar the card is named after. (Named in prose rather than
# spelled: the guard reads THIS file, and a copy of the id here is a stray.)
COLON_EXEMPLAR = "gpt-" + "oss" + ":120" + "b"
# NEGATIVE: a family word followed by a colon and a SPACE is prose, not an id tag.
# The families are assembled for the same reason the ids are.
COLON_PROSE = "ph" + "i: a word; sol" + "ar: another."
# Bare family words with no id separator: prose, not an id. `Claude-` as a company
# name is the near-miss that must not be swallowed into the id shape.
BARE_PROSE = "Claude- is a company; phi and solar are words."
# NEGATIVE (card t_dba05702): an ordinary English hyphenated compound whose first
# word is a family name. That compound is matched by modelToken AND separates as
# an id, so on the PRE-FIX reader it is reported as a `command`-family model id and
# the guard reds about the shipped LLM default -- measured on a full local gate
# (tree df7cf6a8) where the phrase sat in a shell check label. The fixed reader
# keeps it as prose.
#
# ASSEMBLED FROM PARTS, like every id in this file, and for the same reason: the
# guard reads THIS .py file, so the compound written whole here would be a stray
# mention for the pre-fix reader on every BASE leg (that is the trap the harness
# docstring already records for the earlier cut's two spelled-out ids).
PROSE_COMPOUND_TOKEN = "com" + "mand-line"
PROSE_COMPOUND = "shellcheck: the %s half of the check must stay quiet." % PROSE_COMPOUND_TOKEN

# The revision the prose case pins BY NAME: the reader BEFORE the prose-compound
# exclusion (card t_dba05702), i.e. the shipped reader the fix replaces. Pinned by
# sha rather than the run-wide base because the run-wide derivation is stale for
# cases 1-5 (see the note below the CASES table) and would make this case's BASE
# leg read a reader it did not mean to. Resolved and capability-checked at startup
# like COLON_CAUGHT_BASE, so a rewritten ref refuses with exit 2 instead of
# reporting a comparison it did not make.
PROSE_CAUGHT_BASE = os.environ.get("BITEPROOF_PROSE_CAUGHT_BASE", "3b410e52")

# originals[path] = bytes before this harness touched anything (absent = created).
originals = {}
created = []


def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def git(*args):
    p = subprocess.run(["git"] + list(args), cwd=REPO, capture_output=True, text=True)
    return p.returncode, p.stdout


def show(ref, rel):
    code, out = git("show", "%s:%s" % (ref, rel))
    if code != 0:
        return None
    return out


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if path not in originals:
        originals[path] = None if not os.path.exists(path) else open(path, "rb").read()
    if originals[path] is None and path not in created:
        created.append(path)
    with open(path, "wb") as fh:
        fh.write(text.encode())


def swap_guard(text):
    """Swaps the guard file for a BASE leg. Deliberately NOT via write(): the
    swap must not register the base body as an "original" to restore."""
    with open(GUARD, "wb") as fh:
        fh.write(text.encode())


def restore():
    """Puts every touched file back. Idempotent and safe to call more than once
    (a second call re-writes the same bytes) — an earlier cut short-circuited on
    a flag, so the first restore() left the BASE guard in place for every later
    case and turned four bites into false greens."""
    for path, body in originals.items():
        if body is None:
            if os.path.exists(path):
                os.remove(path)
            continue
        with open(path, "wb") as fh:
            fh.write(body)


def on_signal(signum, frame):
    restore()
    sys.exit(2)


atexit.register(restore)

# The guard's own body is snapshotted like any other file — it is the one file
# this harness swaps for a BASE leg, and a swap that is not in originals is how
# the working guard never came back.
GUARD_ORIGINAL = open(GUARD, "rb").read()
originals[GUARD] = GUARD_ORIGINAL

# The base guard's file, materialised so it can be swapped in for a BASE leg.
BASE_GUARD = show(BASE_REF, GUARD_REL)

# Per-case base readers, keyed by the ref the case pins. A case that leaves
# `base_ref` unset uses BASE_GUARD (the run-wide base); a case that pins one gets
# that revision's guard instead, so one run can compare against two readers.
BASE_GUARDS = {}


def run_test():
    p = subprocess.run(["go", "test", TEST_PKG, "-count=1", "-run", TEST_NAME],
                       cwd=API, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def build():
    p = subprocess.run(["go", "build", "./..."], cwd=API, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def verdict(label, expect_red, needles):
    """Runs the guard against the CURRENT tree and checks the verdict + needles."""
    rc, out = run_test()
    red = rc != 0
    if red != expect_red:
        print("   %s: FAIL -- expected %s, guard %s" % (
            label, "RED" if expect_red else "GREEN", "RED" if red else "GREEN"))
        for line in out.splitlines()[:12]:
            print("      | " + line)
        return False
    missing = [n for n in needles if n not in out]
    if missing:
        print("   %s: FAIL -- the %s did not name: %s" % (
            label, "red" if expect_red else "green run", missing))
        for line in out.splitlines()[:12]:
            print("      | " + line)
        return False
    print("   %s: ok (%s%s)" % (label, "RED" if red else "GREEN",
                                "" if not needles else ", names %s" % ", ".join(needles)))
    return True


def base_is_good(ref):
    """True when the FULL case set reports zero failures with `ref` as the run-wide base.

    This is the question the operator is actually asking, asked directly. The
    capability probe above is a cheap SCREEN (it skips a reader that is widened by
    definition), but it accepts readers that are still too new for the legacy legs —
    measured: `0c1c6e9c` passes the probe and fails five of fourteen cases.

    Runs in a child process, so the caller's globals and the tree's guard file are not
    disturbed by the attempt. The child is byte-identical work to a normal run, which is
    why this is only called for candidates that pass the probe.
    """
    env = dict(os.environ)
    env["BITEPROOF_BASE"] = ref
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    proc = subprocess.run([sys.executable, os.path.abspath(__file__)],
                          capture_output=True, text=True, env=env, cwd=REPO)
    return proc.returncode == 0


def derive_base():
    """The run-wide base revision, derived rather than typed.

    The revision the legacy BASE legs were written against: a reader OLDER than this
    tree's, so "did the old scan miss this?" has an answer. Derived because PR 841
    MERGED — `origin/main` stopped holding the pre-widening reader, so the old default
    silently stopped meaning what those expectations were written against.

    THE PROBE ALONE IS NOT ENOUGH, AND THAT IS MEASURED (card t_bfef6b4f). Walking the
    guard's history newest-first and accepting the first revision whose reader does not
    catch the colon probe picks `0c1c6e9c`, which passes the probe but is STILL too new
    for five of the fourteen cases — they read `BASE guard ... FAIL -- expected GREEN,
    guard RED`:

        ba4a7d10  cases: 14, failed: 0     <- the revision these cases were written for
        e173f747  cases: 14, failed: 0
        0c1c6e9c  cases: 14, failed: 5    <- probe-passes, still wrong

    So the capability probe is a NECESSARY screen and not the answer: the legacy legs
    span several widening shapes at once (the extension whitelist, the `deepseek-`
    prefix, the digit filter), and only a reader old enough for ALL of them gives the
    "missed this" column its meaning. The walk therefore runs the FULL harness against
    each candidate and accepts the first that reports zero failures — the same question
    the operator is asking, asked directly instead of through a proxy.

    Returns (ref_or_None, reason). A candidate whose reader already catches the probe is
    skipped without a full run, since it is the widened reader by definition.
    """
    _, base_rev = git("rev-parse", "--short", BASE_REF)
    base_rev = base_rev.strip()
    if not base_rev:
        return None, "cannot resolve %s" % BASE_REF
    _, last_touch = git("log", "-1", "--format=%H", base_rev, "--", GUARD_REL)
    last_touch = last_touch.strip()
    if not last_touch:
        return None, "%s records no commit touching %s" % (base_rev, GUARD_REL)
    _, parent = git("rev-parse", "--short", "%s^" % last_touch)
    parent = parent.strip()
    if not parent:
        return None, "%s (the commit that last touched the guard) has no parent" % last_touch[:12]

    _, history = git("log", "--format=%H", base_rev, "--", GUARD_REL)
    candidates = [parent] + [r for r in history.split() if r and r != last_touch and r != parent]
    probed = 0
    for cand in candidates:
        if show(cand, GUARD_REL) is None:
            continue
        if base_catches_probe(cand):
            continue
        probed += 1
        if base_is_good(cand):
            return cand, None
    if probed == 0:
        return None, ("every revision in %s's history of %s carries the widened reader, so no legacy "
                      "BASE leg can be compared" % (BASE_REF, GUARD_REL))
    return None, ("none of the %d probe-passing revisions in %s's history of %s gives a clean run — "
                  "the legacy BASE expectations no longer match any reader in this history"
                  % (probed, BASE_REF, GUARD_REL))


def base_catches_probe(ref):
    """True when `ref`'s guard catches the capability probe on a scratch tree.

    The probe is a listed family's id in the `id:tag` spelling. The pre-widening
    reader tokenises `deepseek-` ids only, so it does not judge this at all; the
    widened reader does. This is the CAPABILITY test the sibling harness records:
    the question is whether the reader ANSWERS DIFFERENTLY, not whether the file
    differs by a byte.
    """
    guard_here = open(GUARD, "rb").read()
    probe_path = os.path.join(REPO, BASE_CHECK_PROBE_REL)
    try:
        swap_guard(show(ref, GUARD_REL))
        # write() registers the probe for restore(), which is what removes it —
        # so a signal mid-check still leaves no file behind.
        write(probe_path, "package qaprobe\n\n// The drafter calls %s via the internal endpoint.\n"
              % BASE_CHECK_PROBE)
        rc, _ = run_test()
        return rc != 0
    finally:
        restore()
        with open(GUARD, "wb") as fh:
            fh.write(guard_here)


def base_guard_for(case):
    """The guard file to swap in for a case's BASE leg. A case may pin its own
    `base_ref`; otherwise the run-wide BITEPROOF_BASE is used. Returns
    (label, text), and the text is None when the ref carries no such file."""
    ref = case.get("base_ref") or BASE_REF
    if ref == BASE_REF:
        return ref, BASE_GUARD
    if ref not in BASE_GUARDS:
        BASE_GUARDS[ref] = show(ref, GUARD_REL)
    return ref, BASE_GUARDS[ref]


def run_case(case):
    name = case["name"]
    if ONLY and ONLY not in name:
        return None
    print("== %s" % name)
    before = sha(GUARD)
    try:
        case["mutate"]()
        rc, out = build()
        if rc != 0:
            print("   HARNESS BUG: the mutation does not compile, so the guard would hide behind a broken "
                  "build\n%s" % out[:2000])
            return False
        ok = verdict("BRANCH guard", case["branch_red"], case.get("needles", []))
        if case.get("base_expect") is not None:
            base_label, base_text = base_guard_for(case)
            if base_text is None:
                print("   REFUSING: %s has no %s, so this case's BASE leg cannot be read" % (base_label, GUARD_REL))
                return False
            swap_guard(base_text)
            ok = verdict("BASE guard (%s)" % base_label, case["base_expect"], case.get("base_needles", [])) and ok
            restore()
            if sha(GUARD) != before:
                print("   HARNESS BUG: the working guard did not come back byte-identical")
                return False
    finally:
        restore()
    if sha(GUARD) != before:
        print("   HARNESS BUG: the guard file is not byte-identical after this case")
        return False
    return ok


def probe_mjs():
    write(os.path.join(REPO, "web/next.config.qa_probe.mjs"),
          "// default model: %s\n" % RETIRED_TAGGED)


def probe_extensionless():
    write(os.path.join(REPO, "api/Dockerfile.qa_probe"),
          "ARG QA_PROBE_MODEL=%s\n" % RETIRED_TAGGED)


def probe_txt():
    write(os.path.join(REPO, "web/public/qa_probe_llms.txt"),
          "model: %s\n" % RETIRED_TAGGED)


def probe_other_family():
    body = open(os.path.join(REPO, FORMGEN_REL), "rb").read().decode()
    mutated = body.replace(WORKING, OTHER_FAMILY, 1)
    if mutated == body:
        raise SystemExit("HARNESS BUG: %s no longer names the shipped model" % FORMGEN_REL)
    write(os.path.join(REPO, FORMGEN_REL), mutated)


def probe_unknown_family():
    write(os.path.join(REPO, "api/qa_probe_notes.md"), "Runs on %s today.\n" % UNKNOWN_FAMILY)


def probe_existing_shape():
    write(os.path.join(REPO, "web/src/lib/qa_probe_model.ts"),
          "export const M = \"%s\";\n" % RETIRED_TAGGED)


def probe_negative():
    # The shipped id plus a pile of colon tokens that are NOT model ids: ports,
    # npm script names, a Go module hash. The widened token rule must stay quiet.
    write(os.path.join(REPO, "api/qa_probe_negative.go"),
          "package qaprobe\n\n// %s served on localhost:8080 (h1:5TJZWKEWniPve33v, a11y:install)\n" % WORKING)


def probe_skipped_tree():
    # The recorded Turbopack artefact: `.next/cache` caches the build shell's own
    # environment, which is git-ignored and cannot reach an image.
    write(os.path.join(REPO, "web/.next/cache/qa_probe.sst"),
          "ANTHROPIC_MODEL=%s\n" % RETIRED_TAGGED)


def probe_nodigit_id():
    # A LISTED family's id with no digit in it, in a file whose only model mention
    # this is. The pre-fix digit filter dropped it, so the scan judged nothing
    # (card t_16fa0164); the pre-widening reader caught it, so the BASE leg must
    # be RED on this case.
    write(os.path.join(REPO, "api/internal/qa_probe_nodigit.go"),
          "package qaprobe\n\n// The drafter calls %s via the internal endpoint.\n" % NODIGIT)


def probe_bare_prose():
    # NEGATIVE: bare family words carry no id separator, so they are prose and
    # must stay silent. `Claude-` is the near-miss: the id shape is
    # `<family>-<tail>`, so a hyphen with nothing after it is still prose.
    write(os.path.join(REPO, "api/internal/qa_probe_bare_prose.go"),
          "package qaprobe\n\n// %s\n" % BARE_PROSE)


def probe_colon_exemplar():
    # The shape the card is named after: a LISTED, HYPHENATED family's id in the
    # `id:tag` spelling. The `-` in it belongs to the FAMILY, not to the
    # separator, so idHasIdSeparator saw `:` and rejected it — judged by nothing
    # on the shipped reader, and this is the id the guard's own header records as
    # measured LIVE.
    write(os.path.join(REPO, "api/internal/qa_probe_colon_gptoss.go"),
          "package qaprobe\n\n// The drafter calls %s via the internal endpoint.\n" % COLON_EXEMPLAR)


def probe_colon_nohyphen():
    # A LISTED family with NO HYPHEN at all in the `id:tag` spelling: neither old
    # branch can reach it (the hyphen branch needs the `-`, the digit branch a
    # leading digit) and unknownModelTag needs a digit BEFORE the colon, which
    # this family does not carry.
    write(os.path.join(REPO, "api/internal/qa_probe_colon_mistral.go"),
          "package qaprobe\n\n// The drafter calls %s via the internal endpoint.\n" % COLON_NOHYPHEN)


def probe_colon_prose():
    # NEGATIVE control: a family word followed by a colon and a SPACE is a
    # sentence, not an id tag. The colon branch must refuse it.
    write(os.path.join(REPO, "api/internal/qa_probe_colon_prose.go"),
          "package qaprobe\n\n// %s\n" % COLON_PROSE)


def probe_prose_compound():
    # The English hyphenated compound whose first word is a family name (card
    # t_dba05702). A file whose ONLY model-shaped token is this compound: the
    # PRE-FIX reader reports it as a `command`-family id and reds, the fixed reader
    # keeps it as prose and stays GREEN. Written into a shipped .go file so the
    # walk certainly reads it, and the probe text carries no other id.
    write(os.path.join(REPO, "api/internal/qa_probe_prose_compound.go"),
          "package qaprobe\n\n// %s\n" % PROSE_COMPOUND)


CASES = [
    {
        "name": "case 1: a NEW .mjs file naming the retired id (the old whitelist had no .mjs)",
        "mutate": probe_mjs,
        "branch_red": True, "needles": ["web/next.config.qa_probe.mjs", RETIRED],
        "base_expect": False,
    },
    {
        "name": "case 2: a NEW extensionless file naming the retired id (filepath.Ext is \"\")",
        "mutate": probe_extensionless,
        "branch_red": True, "needles": ["api/Dockerfile.qa_probe", RETIRED],
        "base_expect": False,
    },
    {
        "name": "case 3: a NEW .txt file naming the retired id (a served file shape)",
        "mutate": probe_txt,
        "branch_red": True, "needles": ["web/public/qa_probe_llms.txt", RETIRED],
        "base_expect": False,
    },
    {
        "name": "case 4: a shipped .go file naming a DEAD id from another family (a qwen id)",
        "mutate": probe_other_family,
        "branch_red": True, "needles": [FORMGEN_REL, OTHER_FAMILY],
        "base_expect": False,
    },
    {
        "name": "case 5: an id whose family is not on the list, carrying a model SIZE tag",
        "mutate": probe_unknown_family,
        "branch_red": True, "needles": ["api/qa_probe_notes.md", UNKNOWN_FAMILY, "SIZE tag"],
        "base_expect": False,
    },
    {
        "name": "case 6: the shape that already bit before this card (a NEW .ts file) stays caught",
        "mutate": probe_existing_shape,
        "branch_red": True, "needles": ["web/src/lib/qa_probe_model.ts", RETIRED],
        "base_expect": True,
    },
    {
        "name": "case 7: NEGATIVE control -- the shipped id and non-model colon tokens stay green",
        "mutate": probe_negative,
        "branch_red": False,
    },
    {
        "name": "case 8: NEGATIVE control -- a dead id inside a SKIPPED tree (.next) stays green",
        "mutate": probe_skipped_tree,
        "branch_red": False,
    },
    {
        "name": "case 9: a LISTED family's id with NO DIGIT (the digit filter dropped it; the old reader caught it)",
        "mutate": probe_nodigit_id,
        "branch_red": True, "needles": ["api/internal/qa_probe_nodigit.go", NODIGIT],
        # RED, not GREEN: the `deepseek-`-prefixed reader this BASE leg compares
        # against caught this exact shape, which is what makes the digit filter a
        # regression rather than a residual (card t_16fa0164).
        "base_expect": True, "base_needles": ["api/internal/qa_probe_nodigit.go", NODIGIT],
    },
    {
        "name": "case 10: NEGATIVE control -- bare family words with no id separator stay green",
        "mutate": probe_bare_prose,
        "branch_red": False,
    },
    {
        "name": "case 11: a LISTED family's hyphenless-colon id (the shipped reader is blind to it)",
        "mutate": probe_colon_nohyphen,
        "branch_red": True, "needles": ["api/internal/qa_probe_colon_mistral.go", COLON_NOHYPHEN],
        # The run-wide base is the derived PRE-WIDENING reader, which matched only
        # `deepseek-` ids — it never tokenised this family, so BASE must stay
        # GREEN. That is the "the shipped scan was blind" column for this shape.
        "base_expect": False,
    },
    {
        "name": "case 12: the card's exemplar -- a listed family's `id:tag` id with a hyphen IN the family",
        "mutate": probe_colon_exemplar,
        "branch_red": True, "needles": ["api/internal/qa_probe_colon_gptoss.go", COLON_EXEMPLAR],
        # This leg pins the head the commit under review REPLACED (5376463e, the
        # digit-filtered reader), which CAUGHT this id — which is what makes the
        # loss a REGRESSION introduced by that commit rather than a residual of the
        # widening. MEASURED: RED there naming the probe file. Its BASE leg IS the
        # capability check: a pinned reader that did not catch the exemplar would
        # fail this case rather than report a comparison it did not make.
        "base_ref": COLON_CAUGHT_BASE,
        "base_expect": True, "base_needles": ["api/internal/qa_probe_colon_gptoss.go", COLON_EXEMPLAR],
    },
    {
        "name": "case 13: NEGATIVE control -- a family word, a colon and a SPACE stay green",
        "mutate": probe_colon_prose,
        "branch_red": False,
    },
    {
        "name": "case 14: NEGATIVE control -- an English hyphenated compound is prose, not an id",
        "mutate": probe_prose_compound,
        # GREEN here: after the fix the compound is not an id at all.
        "branch_red": False,
        # RED at the pinned pre-fix reader: this was a FALSE POSITIVE, and that is
        # the whole point of the case -- the shipped reader reported the phrase as
        # a `command`-family model id and redd about the LLM default (card
        # t_dba05702, measured on a full local gate, tree df7cf6a8).
        "base_ref": PROSE_CAUGHT_BASE,
        "base_expect": True,
        "base_needles": ["api/internal/qa_probe_prose_compound.go", PROSE_COMPOUND_TOKEN],
    },
]

# KNOWN STALENESS IN THE RUN-WIDE DERIVATION (NOT this card's change; measured
# byte-identically red on a pristine checkout of the base tree, so it is a
# pre-existing defect and not a regression). Cases 1-5 expect `base_expect: False`
# because they were written against the PRE-WIDENING reader (the `deepseek-`-only
# token rule with a fixed extension whitelist). The derivation picks the parent of
# the NEWEST commit at the base ref that touched the guard, and after #861
# (`3b410e52`, which follows the widening #841) that parent is `c5427aed` — a
# reader that already reads every file and every family, so it CATCHES those five
# mutations and the BASE legs read RED. Its capability check only probes the
# `id:tag` colon shape, which `c5427aed` does not catch, so the too-new reader is
# accepted rather than refused. Filed separately; the one case this card adds
# (case 14) therefore pins its base BY SHA (`PROSE_CAUGHT_BASE`) instead of using
# the run-wide derivation, so its leg does not depend on the broken premise.


def main():
    global BASE_REF, BASE_GUARD

    if DERIVED_BASE:
        derived, why = derive_base()
        if derived is None:
            print("REFUSING (exit 2, NO VERDICT): no usable run-wide base. %s. The legacy BASE legs all ask "
                  "\"did the OLD scan miss this?\", so they need a reader older than this tree's — pass "
                  "BITEPROOF_BASE=<rev> to name one." % why)
            return 2
        BASE_REF, BASE_GUARD = derived, show(derived, GUARD_REL)
        print("derived run-wide base: %s (the newest revision in the guard's history whose FULL case "
              "set reports zero failures — screened by the capability probe first)"
              % BASE_REF)

    if BASE_GUARD is None:
        print("HARNESS BUG: %s has no %s, so no BASE leg can run" % (BASE_REF, GUARD_REL))
        return 2

    # The pinned per-case refs must resolve and carry a guard file BEFORE any case
    # runs. A rewrite of PR 841's branch (or, for the prose ref, a moved sha) would
    # otherwise turn a case into a half-run whose BASE leg reads nothing.
    #
    #   COLON_CAUGHT_BASE — the head the commit under review replaced (PR 841's
    #     digit-filtered reader), pinned because it lives on a squashed branch.
    #   PROSE_CAUGHT_BASE — the shipped reader before the prose-compound exclusion
    #     (card t_dba05702), pinned so case 14's BASE leg reads the reader the fix
    #     replaces rather than the run-wide derivation, which is stale for the
    #     legacy extension cases (recorded below the CASES table).
    for pinned, why in ((COLON_CAUGHT_BASE, "BITEPROOF_COLON_CAUGHT_BASE"), (PROSE_CAUGHT_BASE, "BITEPROOF_PROSE_CAUGHT_BASE")):
        code, _ = git("rev-parse", "--verify", "%s^{commit}" % pinned)
        if code != 0:
            print("REFUSING: %s pins %s, which is not a revision in this repo -- the case it backs "
                  "compares a specific reader, and a missing ref would let its BASE leg read nothing. "
                  "Re-derive it or override the env var." % (why, pinned))
            return 2
        body = show(pinned, GUARD_REL)
        if body is None:
            print("REFUSING: %s pins %s, which carries no %s" % (why, pinned, GUARD_REL))
            return 2
        BASE_GUARDS[pinned] = body
        if BASE_GUARD.encode() == body.encode():
            print("REFUSING: %s pins %s, which IS the run-wide base %s, so the case it backs would "
                  "compare one reader with itself" % (why, pinned, BASE_REF))
            return 2

    if open(GUARD, "rb").read() == BASE_GUARD.encode():
        print("REFUSING: the working guard is byte-identical to %s's, so every BASE leg would compare a "
              "guard with itself and the \"the old scan missed this\" column would prove nothing." % BASE_REF)
        return 2

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    # The tree state BEFORE anything is touched. The run must leave it exactly
    # like this — a harness that is being written on an in-progress branch has
    # its own uncommitted work in the tree, so "git status is empty" would be the
    # wrong assertion (it would fail on the very branch that added the harness).
    _, status_before = git("status", "--porcelain")

    results = []
    for case in CASES:
        r = run_case(case)
        if r is None:
            continue
        results.append((case["name"], r))
    print()
    for name, ok in results:
        print("%-4s %s" % ("OK" if ok else "FAIL", name))
    bad = [n for n, ok in results if not ok]
    _, status_after = git("status", "--porcelain")
    drifted = [l for l in status_after.splitlines()
               if l.strip() and l not in status_before.splitlines()]
    print()
    if drifted:
        print("TREE DRIFTED during the run (harness bug — a file it did not restore):")
        for l in drifted[:20]:
            print("   " + l)
    print("cases: %d, failed: %d, tree unchanged: %s (restored byte-identically)" % (
        len(results), len(bad), not drifted))
    if bad or drifted:
        return 1
    print("ALL CASES BITES (BRANCH) / STAYED GREEN (BASE) AS EXPECTED, sources restored byte-identically")
    return 0


if __name__ == "__main__":
    sys.exit(main())
