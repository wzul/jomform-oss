#!/usr/bin/env python3
"""Bite proof for the menu-variation guards.

Sabotages ONE thing at a time, runs the named guard, and expects RED each time.
Every case verifies (a) the sabotage is really on disk when the test runs,
(b) which test failed, and (c) that the restore really happened before moving on.
It writes nothing else to the repo and leaves both source files byte-identical.

Run:  python3 api/scripts/biteproof_menu_variation.py
"""
import os
import signal
import subprocess
import sys

API = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MENU = os.path.join(API, "internal/httpapi/menu.go")
ORDER = os.path.join(API, "internal/httpapi/order.go")

# (label, file, old, new, tests, want_occurrences)
CASES = [
    (
        "payload drops variation_id",
        MENU,
        "product_id:it.id,variation_id:it.vid||'',quantity:it.qty",
        "product_id:it.id,quantity:it.qty",
        ["TestMenuVariationCheckoutPayloadCarriesVariationID"],
        1,
    ),
    (
        "live variation resolution neutralised (withVariations always bails)",
        MENU,
        'if h.ProductVariations == nil || v.ID == "" {',
        'if true {',
        ["TestMenuVariationMenuPageResolvesVariationsLive"],
        1,
    ),
    (
        "selector markup removed from the item templates",
        MENU,
        "{{if .Variations}}",
        "{{if false}}",
        ["TestMenuVariationMenuCardRendersVariationSelector",
         "TestMenuVariationMenuPageResolvesVariationsLive"],
        2,
    ),
    (
        "card tap adds the base price without a chosen variant "
        "(the selector is never consulted)",
        MENU,
        "    var sel=varOf(el);\n    if(sel){\n"
        "      // Variable item: the card tap cannot decide a variant. If the buyer has\n"
        "      // already picked one, top up that line; otherwise prompt for the choice.\n"
        "      if(!sel.value){ flagPick(sel); return; }\n"
        "      if(unbuyable(sel)){ flagPick(sel,attr('data-lbl-soldout')); return; }\n"
        "      var o=sel.options[sel.selectedIndex];\n"
        "      addItem(id,el.getAttribute('data-name'),o.getAttribute('data-price'),sel.value,o.getAttribute('data-vname'));\n"
        "      return;\n"
        "    }\n",
        "",
        ["TestMenuVariationVariableItemAsksForAVariant"],
        1,
    ),
    (
        "the selector lookup is stubbed out (returns no selector)",
        MENU,
        "    if(!el) return null;\n"
        "    var box=el.closest('.item')||el.parentNode;\n"
        "    return box?box.querySelector('select.mvar'):null;",
        "    return null;",
        # Asserted BEHAVIOURALLY: with the lookup stubbed to null, a no-choice
        # tap on a variable row never reaches the branch that flags its own
        # selector. The string test alone no longer catches this (the call site
        # and the function name both survive the stub), which is exactly the
        # class of hole this harness exists to find.
        ["TestMenuRowNoChoiceTapFlagsItsOwnSelector"],
        1,
    ),
    (
        "the row path attributes a variant to the FIRST dish of the category "
        "(the shipped #665 shape: no per-row wrapper, host looked up in the "
        "category container)",
        MENU,
        "{{end}}{{define \"menuRow\"}}\n<div class=\"item\">\n<button class=\"row\"",
        "{{end}}{{define \"menuRow\"}}\n<button class=\"row\"",
        ["TestMenuRowVariantIsAttributedToItsOwnDish",
         "TestMenuRowEachDishHasItsOwnHost",
         "TestMenuRowDuplicateRenderTapFlagsTheTappedRender",
         "TestMenuRowDuplicateRenderDoesNotCrossAttribute"],
        1,
    ),
    (
        "sold-out variant no longer disabled",
        MENU,
        "{{if .SoldOut}} disabled{{end}}",
        "",
        ["TestMenuVariationMenuPageResolvesVariationsLive"],
        2,
    ),
    (
        "server-side guard removed: a variable product ordered with no "
        "variation_id falls back to its base price",
        ORDER,
        "if it.VariationID == \"\" && h.ProductVariations != nil &&\n"
        "\t\t\th.isProductRow(r.Context(), tgt.AccountID, it.ProductID) {\n"
        "\t\t\tif vs, verr := h.ProductVariations.ListByProduct(r.Context(), it.ProductID); verr == nil && len(vs) > 0 {\n"
        "\t\t\t\twriteErr(w, http.StatusBadRequest, \"pilih variasi untuk \"+name)\n"
        "\t\t\t\treturn\n"
        "\t\t\t}\n"
        "\t\t}",
        "",
        ["TestMenuVariationOrderWithoutVariationIsRejected"],
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
    originals = {MENU: read(MENU), ORDER: read(ORDER)}
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
        print("  %-58s %s" % (label[:58], status))
    print("\nfinal files restored: %s" % all(read(p) == s for p, s in originals.items()))
    return 0 if ok else 1


if __name__ == "__main__":
    # A SIGTERM must not leave a mutated source behind: raise so the per-case
    # `finally` restores before the interpreter exits.
    def _term(signum, frame):
        raise SystemExit(3)

    signal.signal(signal.SIGTERM, _term)
    sys.exit(main())
