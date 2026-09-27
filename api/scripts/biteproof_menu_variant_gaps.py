#!/usr/bin/env python3
"""Bite proof for the menu-variant scope guards.

The guards live in api/internal/httpapi/menu_variant_scope_test.go and
menu_live_stock_test.go (the live-stock guards the t_aa6e3596 fix added). Each
one must go RED when its behaviour is removed — a guard that stays green is
measuring nothing. This harness sabotages ONE thing at a time, runs only the
named guard, and checks (a) the sabotage really is on disk when the test runs,
(b) which guard failed, and (c) that the restore really happened before the
next case. It leaves every file byte-identical.

Every guard in EVERY file below must be named by at least one case. A guard that
no case names is one nothing demonstrates, which is how this harness came to
declare more guards than it could prove — so main() refuses to run (a) when a
declared guard has no case and (b) when a case names a guard no file declares.
The only exceptions are listed in CONTROL_GUARDS.

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
# The declaration check covers BOTH guard files. It started on the scope file
# alone (when that was the only place the guards lived); t_aa6e3596 put eleven
# more guards in menu_live_stock_test.go, and five of them were named by cases
# here — so covering only the first file would have made the check refuse the
# union ("these cases name a guard that is not in ...") instead of doing its
# job. Either direction of drift is still caught; it just has to look at both.
GUARDS = [
    os.path.join(API, "internal/httpapi/menu_variant_scope_test.go"),
    os.path.join(API, "internal/httpapi/menu_live_stock_test.go"),
]

# Guards that exist to stop a mutation from "passing" by disabling the buyable
# path — they stay green on BOTH sides of every sabotage, so no case can name
# them and none should. Everything else declared in GUARDS needs a case.
CONTROL_GUARDS = {
    "TestMenuInStockVariantPickStillAdds",
    "TestMenuLiveStockInStockPickStillAdds",
    "TestMenuReloadKeepsAnInStockBasketLine",
    "TestMenuCheckoutStillPostsWhenEverythingIsInStock",
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
        "the change handler stops routing through the shared add path and just "
        "adds (so a sold-out pick is never re-checked)",
        MENU,
        "    // The disabled attribute is the guard where this page is not scripted, but\n"
        "    // the page IS scripted and stays open: a variant can sell out after the\n"
        "    // page rendered, and the attribute is then stale. The live re-read in\n"
        "    // addIfBuyable is what refuses that pick; the previous line was only the\n"
        "    // rendered state. Until it answers, the attribute still refuses.\n"
        "    addIfBuyable(t,host);",
        "    var v=t.options[t.selectedIndex];\n"
        "    addItem(t.getAttribute('data-id'),host?host.getAttribute('data-name'):'','',t.value,v.getAttribute('data-vname'));",
        ["TestMenuSoldOutVariantPickIsRefused"],
        1,
    ),
    (
        "the card tap stops routing through the shared add path and just adds "
        "(so a sold-out variant is never re-checked)",
        MENU,
        "      if(!sel.value){ flagPick(sel); return; }\n"
        "      addIfBuyable(sel,el);\n"
        "      return;",
        "      addItem(id,el.getAttribute('data-name'),el.getAttribute('data-price'));\n"
        "      return;",
        ["TestMenuSoldOutVariantCardTapIsRefused"],
        1,
    ),
    (
        "unbuyable() always answers false (the re-check is neutered)",
        MENU,
        "  function unbuyable(sel){\n"
        "    var o=sel.options[sel.selectedIndex];\n"
        "    if(o&&o.disabled) return true;\n"
        "    if(!o) return false;\n"
        "    return soldOutLive(sel.getAttribute('data-id'), o.value);\n"
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
        '\t\tMenuSoldOut:      "Habis stok",\n',
        '\t\tMenuSoldOut:      "",\n',
        ["TestMenuSoldOutLabelInFiveLocales"],
        1,
    ),
    # ---- t_aa6e3596: the sold-out re-check must read LIVE stock, not only the
    # render-time attribute the page was served with. Each case here removes one
    # half of that and names the guard that must notice.
    (
        "unbuyable() goes back to reading only the render-time disabled "
        "attribute (the stale-page defect)",
        MENU,
        "    var o=sel.options[sel.selectedIndex];\n"
        "    if(o&&o.disabled) return true;\n"
        "    if(!o) return false;\n"
        "    return soldOutLive(sel.getAttribute('data-id'), o.value);",
        "    var o=sel.options[sel.selectedIndex];\n"
        "    return !!(o&&o.disabled);",
        ["TestMenuLiveStockRefusesAVariantThatSoldOutWhileThePageWasOpen",
         "TestMenuLiveStockRefusesACardTapOnAStaleSoldOutVariant",
         "TestMenuReloadDropsABasketLineThatWentSoldOut",
         "TestMenuCheckoutRefusesRatherThanPostingASoldOutLine"],
        1,
    ),
    (
        "the add path stops re-reading the live stock before it adds (it only "
        "consults what the page already knew)",
        MENU,
        "    loadLiveStock(function(){\n"
        "      if(unbuyable(sel)){ flagPick(sel,attr('data-lbl-soldout')); return; }\n"
        "      addItem(host.getAttribute('data-id'),host.getAttribute('data-name'),o.getAttribute('data-price'),sel.value,o.getAttribute('data-vname'));\n"
        "      clearPick(sel);\n"
        "    });",
        "    addItem(host.getAttribute('data-id'),host.getAttribute('data-name'),o.getAttribute('data-price'),sel.value,o.getAttribute('data-vname'));\n"
        "    clearPick(sel);",
        ["TestMenuLiveStockRefusesAVariantThatSoldOutWhileThePageWasOpen",
         "TestMenuLiveStockRefusesACardTapOnAStaleSoldOutVariant"],
        1,
    ),
    (
        "the page stops re-checking the basket it restored on open (the "
        "stranded-line defect)",
        MENU,
        "  loadLiveStock(dropSoldOutLines);\n",
        "",
        ["TestMenuReloadDropsABasketLineThatWentSoldOut"],
        1,
    ),
    (
        "checkout stops re-reading the live stock before it POSTs",
        MENU,
        "    loadLiveStock(function(){\n"
        "      var bad=null;\n"
        "      for(var i=0;i<cart.length;i++){\n"
        "        if(soldOutLive(cart[i].id,cart[i].vid)){ bad=cart[i]; break; }\n"
        "      }\n"
        "      if(bad){\n"
        "        openSheet();\n"
        "        fail(attr('data-lbl-stockchanged')+(bad.name?': '+bad.name:''));\n"
        "        render();\n"
        "        return;\n"
        "      }\n"
        "      setTimeout(send,wait);\n"
        "    });",
        "    setTimeout(send,wait);",
        ["TestMenuCheckoutRefusesRatherThanPostingASoldOutLine"],
        1,
    ),
    (
        "the live-stock endpoint stops being scoped to a menu form (it answers "
        "for any form the host names)",
        MENU,
        "	if menu := dbstore.MenuFromConfig(f.Config); menu == nil || !menu.Enabled {\n"
        "		writeErr(w, http.StatusNotFound, \"menu tidak dijumpai\")\n"
        "		return nil\n"
        "	}\n"
        "	return f",
        "	return f",
        ["TestMenuStockRouteIsGatedLikeTheMenuPage"],
        1,
    ),
    (
        "the live-stock endpoint reports the product's own row for a VARIABLE "
        "product instead of its variations (so a variant's sold-out state is "
        "never reported)",
        MENU,
        "		if h.ProductVariations != nil {\n"
        "			// A lookup error must not be reported as \"sold out\": the page then\n"
        "			// keeps its rendered attribute, which is the state before this\n"
        "			// endpoint existed.\n"
        "			if vs, err := h.ProductVariations.ListByProduct(r.Context(), p.ID); err == nil && len(vs) > 0 {\n"
        "				for _, v := range variationViews(vs) {\n"
        "					out = append(out, menuStockItem{ProductID: p.ID, VariationID: v.ID, SoldOut: v.SoldOut})\n"
        "				}\n"
        "				continue\n"
        "			}\n"
        "		}\n",
        "",
        ["TestMenuStockRouteIsGatedLikeTheMenuPage"],
        1,
    ),
    # ---- the remaining live-stock guards. The check above reads BOTH guard
    # ---- files, so these need cases too — a guard nothing sabotages is the
    # ---- exact drift this harness refuses to run with.
    (
        "unbuyable() drops the rendered attribute and trusts only the live "
        "payload, so a variant the server RENDERED sold out becomes addable "
        "whenever the read is empty (a failed fetch, or a payload that does not "
        "mention it)",
        MENU,
        "    var o=sel.options[sel.selectedIndex];\n"
        "    if(o&&o.disabled) return true;\n"
        "    if(!o) return false;\n",
        "    var o=sel.options[sel.selectedIndex];\n"
        "    if(!o) return false;\n",
        ["TestMenuStalePageFallsBackToTheRenderedAttributeWhenTheReadIsEmpty"],
        1,
    ),
    (
        "the served markup renders every variation disabled (so the stale-page "
        "fixture no longer reproduces the state the live-stock guards need, and "
        "they would pass without the live read)",
        MENU,
        "{{if .SoldOut}} disabled{{end}}",
        " disabled",
        ["TestMenuStaleVariantIsNotRenderedDisabled"],
        2,
    ),
    (
        "the live-stock endpoint serves a DRAFT form's stock (a form that is "
        "never publicly served)",
        MENU,
        "\tif f.Status == dbstore.FormStatusDraft {\n"
        "\t\twriteErr(w, http.StatusNotFound, \"borang tidak dijumpai\")\n"
        "\t\treturn nil\n"
        "\t}\n",
        "",
        ["TestMenuStockRouteRefusesADraftForm"],
        1,
    ),
    # ---- t_00b6a9cb: the SIMPLE (non-variant) dish tap. The tap had two
    # ---- branches and only the variable one was guarded, so a sold-out simple
    # ---- dish — which is served the SAME button as an in-stock one, no
    # ---- disabled attribute and no marker — went into the basket with no read
    # ---- at all. Both halves of that guard are sabotaged here: the read it
    # ---- defers the add on, and the refusal it announces when the read says
    # ---- sold out.
    (
        "the simple dish tap stops asking the live state before it adds (it "
        "adds whatever the render-time markup carried — the defect)",
        MENU,
        "    // Simple item: no variant to choose, so the tap adds it — but through the\n"
        "    // same live-stock guard the variable path uses (t_00b6a9cb). A sold-out\n"
        "    // simple dish is served the SAME button as an in-stock one, so the markup\n"
        "    // cannot be the guard here; the tap asks the server instead.\n"
        "    addIfSimpleBuyable(id,el);",
        "    addItem(id,el.getAttribute('data-name'),el.getAttribute('data-price'));",
        ["TestMenuLiveStockRefusesASoldOutSimpleDishTap",
         "TestMenuLiveStockInStockSimpleDishTapStillAdds"],
        1,
    ),
    (
        "the simple dish tap stops deferring the add on the live read (it only "
        "consults the state the page already had, so a dish that sold out after "
        "render is still added)",
        MENU,
        "    if(soldOutLive(id,'')){ announceSoldOut(host); return; }\n"
        "    loadLiveStock(function(){\n"
        "      if(soldOutLive(id,'')){ announceSoldOut(host); return; }\n"
        "      addItem(id,host.getAttribute('data-name'),host.getAttribute('data-price'));\n"
        "    });",
        "    if(soldOutLive(id,'')){ announceSoldOut(host); return; }\n"
        "    addItem(id,host.getAttribute('data-name'),host.getAttribute('data-price'));",
        ["TestMenuLiveStockRefusesASoldOutSimpleDishTap"],
        1,
    ),
    (
        "the simple dish refusal announces nothing (the buyer taps a dish and "
        "is told nothing about why it could not be added)",
        MENU,
        "    openSheet();\n"
        "    errEl.textContent=attr('data-lbl-soldout')+(name?': '+name:'');\n"
        "    errEl.hidden=false;",
        "    if(name){}",
        ["TestMenuLiveStockRefusesASoldOutSimpleDishTap"],
        1,
    ),
    (
        "the simple dish refusal drops the dish's name (the buyer is told "
        "something sold out but not which dish the tap was about)",
        MENU,
        "    errEl.textContent=attr('data-lbl-soldout')+(name?': '+name:'');",
        "    errEl.textContent=attr('data-lbl-soldout');",
        ["TestMenuLiveStockRefusesASoldOutSimpleDishTap"],
        1,
    ),
    (
        "the reload basket re-check stops dropping a SIMPLE line (only a "
        "variant line is re-checked, so a stranded simple line is carried to "
        "checkout)",
        MENU,
        "      if(soldOutLive(cart[i].id,cart[i].vid)){ dropped.push(cart[i]); continue; }",
        "      if(cart[i].vid&&soldOutLive(cart[i].id,cart[i].vid)){ dropped.push(cart[i]); continue; }",
        ["TestMenuReloadDropsASimpleBasketLineThatWentSoldOut"],
        1,
    ),
    (
        "the simple dish refusal fires before the read answers (every simple "
        "tap is refused on the state the page already had, so the menu stops "
        "selling and an empty read refuses everything)",
        MENU,
        "    if(soldOutLive(id,'')){ announceSoldOut(host); return; }\n"
        "    loadLiveStock(function(){\n"
        "      if(soldOutLive(id,'')){ announceSoldOut(host); return; }",
        "    if(true){ announceSoldOut(host); return; }\n"
        "    loadLiveStock(function(){\n"
        "      if(true){ announceSoldOut(host); return; }",
        ["TestMenuSimpleDishTapStillAddsWhenTheLiveReadIsEmpty",
         "TestMenuLiveStockInStockSimpleDishTapStillAdds"],
        1,
    ),
    (
        "the simple tap takes the VARIABLE add path (which needs a selector the "
        "simple dish does not have, so a simple dish becomes unaddable)",
        MENU,
        "    addIfSimpleBuyable(id,el);",
        "    addIfBuyable(varOf(el),el);",
        ["TestMenuLiveStockInStockSimpleDishTapStillAdds"],
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
    """Return the guard test names the guard files declare, in source order."""
    out = []
    for path in GUARDS:
        src = read(path)
        out.extend(re.findall(r"^func (Test\w+)\(", src, re.M))
    return out


def check_declared():
    """Every declared guard needs a case, and every case needs a declared guard.

    Two directions, because this harness exists to prove declared guards bite:
    a guard no case names is one nothing demonstrates (the gap this check was
    added for after the file declared three guards the harness could not
    prove), and a case naming a guard that no guard file declares silently
    proves nothing while still printing BITES off some other failure.

    It reads EVERY file in GUARDS: the coverage claim is only true for the files
    actually looked at, and the cases here now span two of them.
    """
    declared = declared_guards()
    if not declared:
        print("HARNESS BUG: no guard functions found in %s"
              % ", ".join(os.path.relpath(p, API) for p in GUARDS))
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
              % (" or ".join(os.path.relpath(p, API) for p in GUARDS), ", ".join(unknown)))
        return False
    stale_controls = sorted(CONTROL_GUARDS - set(declared))
    if stale_controls:
        print("HARNESS BUG: CONTROL_GUARDS lists a guard that is not declared: %s"
              % ", ".join(stale_controls))
        return False
    print("guard coverage: %d declared across %d file(s), %d named by a case, %d control(s) (%s)"
          % (len(declared), len(GUARDS), len(named), len(CONTROL_GUARDS),
             ", ".join(sorted(CONTROL_GUARDS))))
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
