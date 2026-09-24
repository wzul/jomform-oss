#!/usr/bin/env python3
"""Bite proof for the three menu-variant scope guards.

The guards live in api/internal/httpapi/menu_variant_scope_test.go. Each one
must go RED when its behaviour is removed — a guard that stays green is
measuring nothing. This harness sabotages ONE thing at a time, runs only the
named guard, and checks (a) the sabotage really is on disk when the test runs,
(b) which guard failed, and (c) that the restore really happened before the
next case. It leaves every file byte-identical.

Run:  python3 api/scripts/biteproof_menu_variant_gaps.py
"""
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MENU = os.path.join(API, "internal/httpapi/menu.go")

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


def main():
    originals = {MENU: read(MENU)}
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
        status = "BITES" if red else "DID NOT BITE"
        if not red:
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
