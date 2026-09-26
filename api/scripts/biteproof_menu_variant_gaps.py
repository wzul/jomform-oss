#!/usr/bin/env python3
"""Bite proof for the menu-variant scope guards.

The guards live in api/internal/httpapi/menu_variant_scope_test.go. Each one
must go RED when its behaviour is removed — a guard that stays green is
measuring nothing. This harness sabotages ONE thing at a time, runs only the
named guard, and checks (a) the sabotage really is on disk when the test runs,
(b) which guard failed, and (c) that the restore really happened before the
next case. It leaves every file byte-identical.

Every guard in that file must be named by at least one case. A guard that no
case names is one nothing demonstrates, which is how this harness came to
declare more guards than it could prove — so main() refuses to run (a) when a
declared guard has no case and (b) when a case names a guard the file no longer
declares. The only exceptions are listed in CONTROL_GUARDS.

Run:  python3 api/scripts/biteproof_menu_variant_gaps.py
"""
import os
import re
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MENU = os.path.join(API, "internal/httpapi/menu.go")
BLOCKS = os.path.join(API, "internal/httpapi/render_blocks.go")
LANGS = os.path.join(API, "internal/httpapi/formLang.go")
GUARDS = os.path.join(API, "internal/httpapi/menu_variant_scope_test.go")

# Guards that exist to stop a mutation from "passing" by disabling the buyable
# path — they stay green on BOTH sides of every sabotage, so no case can name
# them and none should. Everything else declared in GUARDS needs a case.
CONTROL_GUARDS = {
    "TestMenuInStockVariantPickStillAdds",
}

# (label, file, old, new, tests, want_occurrences)
CASES = [
    # ---- GAP 1: the hint lookup must reach the hint through its container ----
    (
        "hint lookup goes back to parentNode (the shape-dependent lookup)",
        MENU,
        "    var wrap=sel.closest('.mvarwrap')||sel.parentNode;\n"
        "    var hint=wrap?wrap.querySelector('.mvarstock'):null;\n"
        "    if(hint){\n"
        "      hint.textContent=msg||hint.getAttribute('data-hint')||'';\n"
        "      hint.hidden=false;\n"
        "    }",
        "    var wrap=sel.parentNode;\n"
        "    var hint=wrap?wrap.querySelector('.mvarstock'):null;\n"
        "    if(hint){ hint.hidden=false; }",
        ["TestMenuVariantHintSurvivesAnExtraWrapper"],
        1,
    ),
    (
        "clearPick also goes back to parentNode",
        MENU,
        "    sel.classList.remove('missing');\n"
        "    var wrap=sel.closest('.mvarwrap')||sel.parentNode;\n"
        "    var hint=wrap?wrap.querySelector('.mvarstock'):null;\n"
        "    if(hint){\n"
        "      hint.hidden=true;",
        "    sel.classList.remove('missing');\n"
        "    var wrap=sel.parentNode;\n"
        "    var hint=wrap?wrap.querySelector('.mvarstock'):null;\n"
        "    if(hint){\n"
        "      hint.hidden=true;",
        ["TestMenuVariantHintClearsThroughTheContainer"],
        1,
    ),
    (
        "clearPick stops restoring the rendered prompt it overrode",
        MENU,
        "      hint.hidden=true;\n"
        "      // Restore the rendered prompt, so the hint that shows on a LATER\n"
        "      // no-choice tap is the \"choose a variation\" text rather than whatever a\n"
        "      // previous refusal left in it. The text is the server's (data-hint), so\n"
        "      // it stays localised.\n"
        "      hint.textContent=hint.getAttribute('data-hint')||'';",
        "      hint.hidden=true;",
        ["TestMenuHintTextIsNotLeftStaleByARefusal"],
        1,
    ),
    # ---- GAP 2: a pick on a sold-out variant must be refused ----
    (
        "the change handler no longer re-checks disabled (sold out)",
        MENU,
        "    if(unbuyable(t)){ flagPick(t,attr('data-lbl-soldout')); return; }\n"
        "    clearPick(t);",
        "    clearPick(t);",
        ["TestMenuSoldOutVariantPickIsRefused"],
        1,
    ),
    (
        "the card tap no longer re-checks disabled (sold out)",
        MENU,
        "      if(unbuyable(sel)){ flagPick(sel,attr('data-lbl-soldout')); return; }\n",
        "",
        ["TestMenuSoldOutVariantCardTapIsRefused"],
        1,
    ),
    (
        "unbuyable() always answers false (the re-check is neutered)",
        MENU,
        "  function unbuyable(sel){\n"
        "    var o=sel.options[sel.selectedIndex];\n"
        "    return !!(o&&o.disabled);\n"
        "  }",
        "  function unbuyable(sel){ return false; }",
        ["TestMenuSoldOutVariantPickIsRefused",
         "TestMenuSoldOutVariantCardTapIsRefused"],
        1,
    ),
    # ---- GAP 3: the two renderers' selector contracts must stay disjoint ----
    (
        "the menu renders the form renderer's selector contract again "
        "(select.v + .varwrap + .varstock)",
        MENU,
        '<div class="mvarwrap"><select class="mvar" data-id="{{.ID}}"',
        '<div class="varwrap"><select class="v" data-id="{{.ID}}"',
        ["TestMenuAndFormSelectorContractsAreDisjoint"],
        2,
    ),
    (
        "the hint class goes back to the shared .varstock",
        MENU,
        '<div class="mvarstock" data-hint="{{.PickLabel}}" hidden>',
        '<div class="varstock" data-hint="{{.PickLabel}}" hidden>',
        ["TestMenuAndFormSelectorContractsAreDisjoint",
         "TestMenuVariantHintRevealedOnTheShippedShape"],
        2,
    ),
    (
        "the change handler filters on the form renderer's bare v class "
        "(so the two scripts share one contract)",
        MENU,
        "    if(!t||!t.classList||!t.classList.contains('mvar')) return;",
        "    if(!t||!t.classList||!t.classList.contains('v')) return;",
        ["TestMenuCartDoesNotDriveTheFormRenderersTotal"],
        1,
    ),
    (
        "the menu lookup goes back to a shared selector class "
        "(the form renderer's contract)",
        MENU,
        "    return box?box.querySelector('select.mvar'):null;",
        "    return box?box.querySelector('select.v'):null;",
        ["TestMenuVariantHintRevealedOnTheShippedShape"],
        1,
    ),
    # ---- GAP 3, the direction the two cases above do NOT reach: what the
    # ---- form renderer's own script does when the MENU's selector (or a menu
    # ---- pick) reaches it.
    (
        "the menu's selector also carries the form renderer's bare v class, "
        "so the form renderer's document-wide price lookup answers with the "
        "MENU's variant",
        MENU,
        '<div class="mvarwrap"><select class="mvar" data-id="{{.ID}}"',
        '<div class="mvarwrap"><select class="mvar v" data-id="{{.ID}}"',
        ["TestFormRendererIgnoresAMenuPick"],
        2,
    ),
    (
        "the menu's change handler answers the form renderer's selector "
        "contract again (filters on the bare v class) and resolves its host by "
        "a page-wide data-id lookup, so a pick on the FORM's selector lands in "
        "the MENU's basket",
        MENU,
        "    if(!t||!t.classList||!t.classList.contains('mvar')) return;\n"
        "    var box=t.closest('.item');\n"
        "    var host=box?box.querySelector('.card[data-id],.row[data-id]'):"
        "t.previousElementSibling;\n",
        "    if(!t||!t.classList||!t.classList.contains('v')) return;\n"
        "    var box=t.closest('.item');\n"
        "    var host=box?box.querySelector('.card[data-id],.row[data-id]'):"
        "document.querySelector('[data-id=\"'+t.getAttribute('data-id')+'\"]');\n",
        ["TestMenuBasketIgnoresTheFormRenderersSelectors"],
        1,
    ),
    (
        "the FORM's submit resolves its variation through a document-wide "
        "lookup again (class-agnostic), so it posts the MENU's chosen variant",
        BLOCKS,
        "      var sel=document.querySelector('select.v[data-id=\"'+b.dataset.id+'\"]');\n",
        "      var sel=document.querySelector('select[data-id=\"'+b.dataset.id+'\"]');\n",
        ["TestFormSubmitIsNotDrivenByTheMenusSelector"],
        1,
    ),
    (
        "a non-English locale loses the sold-out message the refusal path "
        "shows",
        LANGS,
        '\t\tMenuSoldOut:     "Habis stok",\n',
        '\t\tMenuSoldOut:     "",\n',
        ["TestMenuSoldOutLabelInFiveLocales"],
        1,
    ),
]


def read(path):
    with open(path) as f:
        return f.read()


def write(path, s):
    with open(path, "w") as f:
        f.write(s)


def run(names):
    """Return (exit_code, combined_output) for the named tests."""
    cmd = ["go", "test", "./internal/httpapi/", "-run",
           "^(%s)$" % "|".join(names), "-count=1"]
    env = dict(os.environ)
    env["JOMFORM_TEST_DATABASE_URL"] = env.get("JOMFORM_TEST_DATABASE_URL", "")
    p = subprocess.run(cmd, cwd=API, env=env, capture_output=True, text=True, timeout=1200)
    return p.returncode, p.stdout + p.stderr


def declared_guards():
    """Return the guard test names the guards file declares, in source order."""
    src = read(GUARDS)
    return re.findall(r"^func (Test\w+)\(", src, re.M)


def check_declared():
    """Every declared guard needs a case, and every case needs a declared guard.

    Two directions, because this harness exists to prove declared guards bite:
    a guard no case names is one nothing demonstrates (the gap this check was
    added for after the file declared three guards the harness could not
    prove), and a case naming a guard that no longer exists silently proves
    nothing while still printing BITES off some other failure.
    """
    declared = declared_guards()
    if not declared:
        print("HARNESS BUG: no guard functions found in %s" % os.path.relpath(GUARDS, API))
        return False
    named = set()
    for label, _, _, _, tests, _ in CASES:
        named.update(tests)
    missing = [g for g in declared if g not in named and g not in CONTROL_GUARDS]
    if missing:
        print("HARNESS BUG: these guards are declared but no case names them: %s" % ", ".join(missing))
        return False
    unknown = sorted(named - set(declared))
    if unknown:
        print("HARNESS BUG: these cases name a guard that is not in %s: %s"
              % (os.path.relpath(GUARDS, API), ", ".join(unknown)))
        return False
    stale_controls = sorted(CONTROL_GUARDS - set(declared))
    if stale_controls:
        print("HARNESS BUG: CONTROL_GUARDS lists a guard that is not declared: %s"
              % ", ".join(stale_controls))
        return False
    print("guard coverage: %d declared, %d named by a case, %d control(s) (%s)"
          % (len(declared), len(named), len(CONTROL_GUARDS), ", ".join(sorted(CONTROL_GUARDS))))
    return True


def main():
    if not check_declared():
        return 2
    originals = {p: read(p) for p in {MENU, BLOCKS, LANGS}}
    results = []
    for label, path, old, new, tests, want_count in CASES:
        orig = originals[path]
        occurrences = orig.count(old)
        if occurrences != want_count:
            print("HARNESS BUG: %r occurs %d times, expected %d" % (label, occurrences, want_count))
            for p, s in originals.items():
                write(p, s)
            return 2
        mutated = orig.replace(old, new)
        if mutated == orig:
            print("HARNESS BUG: mutation %r changed nothing" % label)
            for p, s in originals.items():
                write(p, s)
            return 2
        write(path, mutated)
        # The mutation MUST be on disk when the test runs.
        on_disk = read(path)
        if new not in on_disk or old in on_disk:
            print("HARNESS BUG: mutation %r not present on disk" % label)
            for p, s in originals.items():
                write(p, s)
            return 2
        try:
            code, out = run(tests)
        finally:
            # Restore even when the run is killed (SIGTERM/SIGINT) — a mutated
            # source left behind is worse than a failed run.
            write(path, orig)
        if read(path) != orig:
            print("RESTORE FAILED for %r — fix the file by hand" % label)
            return 3
        red = code != 0
        failed = [t for t in tests if ("--- FAIL: " + t) in out]
        results.append((label, tests, code, red, failed))
        print("\n=== %s ===" % label)
        print("  file:  %s" % os.path.relpath(path, API))
        print("  tests: %s" % ", ".join(tests))
        print("  exit=%d  verdict=%s  failed=%s" % (code, "RED (good)" if red else "GREEN (BAD)", failed or "none"))
        if not red:
            print("  ---- output tail ----")
            print("\n".join(out.strip().splitlines()[-15:]))

    print("\n================ SUMMARY ================")
    ok = True
    for label, tests, code, red, failed in results:
        if red and failed:
            status = "BITES"
        elif red:
            # A non-zero exit whose named guard is not among the failures
            # (a compile error, or a sibling test) proves nothing about it.
            status = "RED, NOT THE NAMED GUARD"
            ok = False
        else:
            status = "DID NOT BITE"
            ok = False
        print("  %-62s %s" % (label[:62], status))
    print("\nfinal files restored: %s" % all(read(p) == s for p, s in originals.items()))
    return 0 if ok else 1


if __name__ == "__main__":
    # A SIGTERM must not leave a mutated source behind: raise so the per-case
    # `finally` restores before the interpreter exits.
    def _term(signum, frame):
        raise SystemExit(3)

    signal.signal(signal.SIGTERM, _term)
    sys.exit(main())
