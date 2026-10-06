// The app's switches are ONE component and ONE set of states (card t_b73eee21).
//
// THE DEFECT, MEASURED ON THE BUILT PAGE
//   Six switches existed as hand-rolled markup copied into three files
//   (`settings/page.tsx` ×4, `form-settings/page.tsx` ×1, `sales/page.tsx` ×1).
//   Read out of the live DOM with a real Chromium against the production build,
//   every one of them computed `cursor: default` (the mouse never showed a
//   pointer), the OFF track was `bg-slate-300` (against which the white knob
//   measures 1.48:1 — two near-identical light greys on a WHITE card, so OFF
//   read as a disabled chip) and hovering one changed NOTHING: the
//   before/after computed styles were byte-identical.
//
// WHY A SOURCE GUARD AND NOT A BROWSER ONE
//   The rendered proof is the live-DOM reading on the card (`cursor`, the
//   hover diff, the knob/track colours). What a source scan can do that the
//   browser reading cannot is stop the SEVENTH switch being hand-rolled: this
//   file walks every `.tsx` under `web/src` and fails on any element that
//   declares `role="switch"` with anything other than the shared class. A rule
//   scoped to the three files we happen to know about would let the next page
//   repeat the defect, which is exactly how this one survived in three copies.
//
// WHAT IS PINNED
//   1. the guard is looking at the real tree (it cannot pass vacuously);
//   2. NO element anywhere in the app declares `role="switch"` outside the
//      shared component, and the old pill's fingerprints (`h-6 w-11`, the
//      `left-[22px]` knob) are gone from the tree;
//   3. all six switches are the component, in the three files that had them;
//   4. the component's CONTRACT is unchanged from the markup it replaced —
//      `role="switch"`, `aria-checked`, `type="button"`, an accessible name,
//      and the toggle semantics at every call site (the `setX((v) => !v)` /
//      `toggleSound(!soundEnabled)` calls are untouched — this card is
//      presentation only);
//   5. the shared class carries what was missing: a pointer cursor, a distinct
//      OFF colour token, a knob with an edge, and a hover state;
//   6. the old OFF colour is gone, so reverting the fix is caught.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { codeOnly, jsxTags, tsxFiles } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/**
 * Read a source under `web/src`, the way `src/lib/tintedSurface.test.ts` does.
 *
 * The base is `web/src` (a `../` from this file), which is why the paths below
 * are written `app/globals.css`, `components/Switch.tsx`.
 *
 * It is deliberately NOT named `read`: `src/lib/buildContext.test.ts` scans this
 * suite for `read("<lit>")` calls and resolves each through a helper it must be
 * able to see (`const read = (rel) => … new URL(rel, <base>)`). A `read` whose
 * base it cannot resolve is reported as an offender and turns the web build gate
 * red — measured, and fixed by not spelling the helper that way. The reads here
 * are all under `web/src`, which the image copies whole.
 */
const srcAt = (rel: string) => readFileSync(`${SRC}${rel}`, "utf8");

const css = srcAt("app/globals.css");
const component = srcAt("components/Switch.tsx");

const FILES = tsxFiles(SRC);

/** Every `role="switch"` element in a source, with its class list. */
function switchElements(src: string): { tag: string; cls: string; line: number }[] {
  const code = codeOnly(src);
  const out: { tag: string; cls: string; line: number }[] = [];
  const seen = new Set<number>();
  for (const e of jsxTags(code)) {
    // `jsxTags` emits an event on OPEN and another on CLOSE for the same tag
    // (and two for a self-closing one). One element, one finding.
    if (seen.has(e.tag.openIdx)) continue;
    seen.add(e.tag.openIdx);
    const attrs = code.slice(e.tag.attrStart, e.tag.attrEnd);
    if (!/\brole\s*=\s*"switch"/.test(attrs)) continue;
    const cm = /className\s*=\s*"([^"]*)"/.exec(attrs);
    out.push({
      tag: e.tag.name,
      cls: cm ? cm[1] : "",
      line: code.slice(0, e.tag.openIdx).split("\n").length,
    });
  }
  return out;
}

/** The hand-rolled pill's fingerprints, anywhere in the tree. */
function handRolledPills(): string[] {
  const out: string[] = [];
  for (const rel of FILES) {
    const code = codeOnly(srcAt(rel));
    code.split("\n").forEach((line, i) => {
      if (/h-6\s+w-11/.test(line) || /left-\[22px\]/.test(line)) {
        out.push(`${rel}:${i + 1}`);
      }
    });
  }
  return out;
}

test("the guard is looking at the real tree (it cannot pass vacuously)", () => {
  assert.ok(FILES.length >= 40, `the walk found only ${FILES.length} .tsx files — it is not reading the app`);
  // After the fix the CALL SITES carry no `role="switch"` literal — it lives in
  // the component — so the vacuity anchor is the component itself plus the
  // three imports that used to hold the hand-rolled markup.
  const withSwitches = FILES.filter((rel) => switchElements(srcAt(rel)).length > 0);
  assert.ok(
    withSwitches.includes("components/Switch.tsx"),
    `the scan found no role="switch" element in components/Switch.tsx (it found ${withSwitches.length} ` +
      `elsewhere: ${withSwitches.join(", ")}) — re-anchor this guard`,
  );
  for (const rel of ["app/settings/page.tsx", "app/form-settings/page.tsx", "app/sales/page.tsx"]) {
    assert.match(
      srcAt(rel),
      /import\s*\{\s*Switch\s*\}\s*from\s*"[^"]*components\/Switch"/,
      `${rel} no longer imports the shared Switch — the walk is reading a stale tree`,
    );
  }
  assert.match(
    css,
    /\.jf-switch\s*\{/,
    "globals.css has no .jf-switch rule — re-anchor this guard instead of deleting it",
  );
});

test("no switch in the app is hand-rolled: every role=switch element carries the shared class", () => {
  // THE GUARD. A tenth switch added to a tenth page must use the component (or
  // at least the shared class); anything else is the defect coming back, and it
  // is reported with its file:line so the fix is one edit.
  const offenders: string[] = [];
  for (const rel of FILES) {
    for (const el of switchElements(srcAt(rel))) {
      const classes = el.cls.split(/\s+/).filter(Boolean);
      if (!classes.includes("jf-switch")) {
        offenders.push(
          `${rel}:${el.line} — <${el.tag} role="switch" className="${el.cls || "(none)"}">`,
        );
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `every role="switch" must render the shared class (web/src/components/Switch.tsx, which ` +
      `renders className="jf-switch"). These are hand-rolled:\n  ${offenders.join("\n  ")}\n` +
      `The hand-rolled copies all shipped cursor:default, a 1.48:1 OFF track and no hover state ` +
      `(measured on the built page, card t_b73eee21).`,
  );
});

test("no copy of the old pill markup is left anywhere in the tree", () => {
  // Belt to the guard above: a leftover COPY that no longer carries
  // role="switch" (a disabled lookalike, a commented-out block that codeOnly
  // does not blank because it is JSX) would not be caught by the role scan.
  // These two class fragments are the old pill's shape, not a generic utility.
  const found = handRolledPills();
  assert.deepEqual(
    found,
    [],
    `the old hand-rolled switch shape (h-6 w-11 / left-[22px]) is still in the tree:\n  ` +
      `${found.join("\n  ")}\nThe switch is web/src/components/Switch.tsx with the .jf-switch classes now.`,
  );
});

test("all six switches are the shared component", () => {
  // The card names the six: four in settings (Storefront + the landing-page
  // toggle), one in form-settings, one in /sales. Asserted per file so a
  // refactor that drops one back to raw markup is named, not counted.
  const counts: Record<string, number> = {
    "app/settings/page.tsx": 4,
    "app/form-settings/page.tsx": 1,
    "app/sales/page.tsx": 1,
  };
  for (const [rel, want] of Object.entries(counts)) {
    const src = srcAt(rel);
    const uses = [...src.matchAll(/<Switch\b/g)].length;
    assert.equal(
      uses,
      want,
      `${rel} renders ${uses} <Switch> elements, expected ${want}`,
    );
    assert.match(
      src,
      /import\s*\{\s*Switch\s*\}\s*from\s*"[^"]*components\/Switch"/,
      `${rel} does not import the shared Switch`,
    );
  }
});

test("the component keeps the accessibility contract the raw markup had", () => {
  // role + aria-checked were on every copy before and must stay on the control
  // itself (not on a wrapper): axe and the keyboard both read them there.
  assert.match(component, /role="switch"/, "Switch.tsx no longer declares role=\"switch\"");
  assert.match(component, /aria-checked=\{checked\}/, "Switch.tsx does not bind aria-checked to the caller's boolean");
  assert.match(component, /type="button"/, "Switch.tsx has no type=\"button\" — inside a form it would submit");
  assert.match(component, /aria-label=\{label\}/, "Switch.tsx does not carry an accessible name");
  // `onClick={onChange}` — the component must not own the toggle itself: the
  // callers' `setX((v) => !v)` semantics are unchanged by this card.
  assert.match(component, /onClick=\{onChange\}/, "Switch.tsx no longer forwards onChange to the click");
  // The knob is decoration: the checked state is the button's aria-checked, and
  // announcing it twice is a screen-reader regression.
  assert.match(
    component,
    /<span aria-hidden="true" className="jf-switch-knob"/,
    "the knob is no longer aria-hidden — the state would be announced twice",
  );
});

test("the /sales sound switch keeps its load-dependent hover tooltip (card t_5ca04dc0)", () => {
  // THE DEFECT. The hand-rolled /sales pill carried
  //   title={soundLoaded ? t("sales.soundLabel") : t("sales.soundLoading")}
  // and the conversion to <Switch> dropped it: the component had no `title`
  // prop, and `label` becomes `aria-label`, which is the accessible NAME a
  // screen reader announces — not a tooltip a mouse user sees. Measured on the
  // built page (fresh localStorage, /sales): `hasTitleAttr` true before the
  // conversion, false after. The card is the restore of those two legs, so both
  // are pinned: the component must FORWARD a title, and the page must PASS the
  // load-dependent copy.
  assert.match(
    component,
    /title\?:\s*string/,
    "Switch.tsx declares no optional `title` prop — a call site cannot pass a tooltip at all",
  );
  assert.match(
    component,
    /title=\{title\}/,
    "Switch.tsx declares a `title` prop but does not forward it to the <button> — the tooltip never reaches the DOM",
  );
  const sales = codeOnly(srcAt("app/sales/page.tsx"));
  assert.match(
    sales,
    /<Switch[\s\S]*?title=\{soundLoaded \? t\("sales\.soundLabel"\) : t\("sales\.soundLoading"\)\}/,
    "app/sales/page.tsx no longer passes the load-dependent tooltip to the sound switch — " +
      "before the stored value is read a mouse user hovering the disabled switch loses the " +
      'only hint that it is still loading (t("sales.soundLoading"))',
  );
  // IN-FILE BITE PROOF. Both assertions above are checked against a plant: strip
  // the forward and the pass and they must stop matching. Without this, an
  // assertion that silently matched nothing (a rename, a whitespace change) would
  // read as a clean tree — the shape this file's header calls out.
  const forward = /title=\{title\}/;
  assert.equal(forward.test(component), true, "the component forward is not what is asserted");
  assert.equal(
    forward.test(component.replace("      title={title}\n", "")),
    false,
    "the component assertion does not bite: dropping `title={title}` still matches",
  );
  const pass = /<Switch[\s\S]*?title=\{soundLoaded \? t\("sales\.soundLabel"\) : t\("sales\.soundLoading"\)\}/;
  assert.equal(pass.test(sales), true, "the /sales pass is not what is asserted");
  assert.equal(
    pass.test(sales.replace(/title=\{soundLoaded[^\n]*\n/, "")),
    false,
    "the /sales assertion does not bite: dropping the title prop still matches",
  );
});

test("the shared class carries the four things the copies were missing", () => {
  const body = (name: string): string => {
    const m = css.match(new RegExp(`\\.${name}\\s*\\{([^}]*)\\}`));
    assert.ok(m, `no .${name} rule in globals.css — re-anchor this guard`);
    return m![1];
  };
  const sw = body("jf-switch");
  // 1. the pointer the live DOM measured as `default` on all six copies.
  assert.match(sw, /cursor:\s*pointer/, ".jf-switch has no pointer cursor — that was the measured defect");
  // 2. a hover cue.
  assert.match(css, /\.jf-switch:hover:not\(:disabled\)\s*\{[^}]*box-shadow/, ".jf-switch has no hover state");
  // 3. the OFF track is a TOKEN, and it is not the old grey. Asserted on the
  //    VALUE, not on the class NAME: this file, globals.css and the component's
  //    own comments all quote `bg-slate-300` in prose explaining the fix, and a
  //    name match would flag the explanation instead of the defect.
  assert.match(
    css,
    /\.jf-switch\[aria-checked="false"\]\s*\{[^}]*background:\s*var\(--jf-switch-off\)/s,
    ".jf-switch's OFF state does not use --jf-switch-off",
  );
  assert.match(
    css,
    /--jf-switch-off:\s*#64748b/,
    "globals.css lost --jf-switch-off (#64748b, whose white knob is 4.76:1)",
  );
  assert.doesNotMatch(
    css,
    /--jf-switch-off:\s*#94a3b8/,
    "--jf-switch-off is back to #94a3b8, whose white knob is 2.56:1 — under the 3:1 floor of WCAG 1.4.11",
  );
  // 4. the OFF rule must NOT paint its own ring. #988 shipped OFF with a
  //    #64748b border while ON is a plain solid fill, so the two states were not
  //    the same object. Asserted here because it is invisible to a colour check.
  const offRule = css.match(/\.jf-switch\[aria-checked="false"\]\s*\{([^}]*)\}/s);
  assert.ok(offRule, "no .jf-switch[aria-checked=false] rule — re-anchor this guard");
  assert.doesNotMatch(
    offRule![1],
    /border-color:\s*#/,
    ".jf-switch's OFF state paints a coloured ring again — ON is a solid fill, so the two states stop matching",
  );
  assert.match(
    offRule![1],
    /border-color:\s*transparent/,
    ".jf-switch's OFF state must keep the 1px reserved as a transparent border, or the pill's box moves",
  );
  assert.doesNotMatch(
    css,
    /--jf-switch-off:\s*#cbd5e1/,
    "--jf-switch-off is back to slate-300 (#cbd5e1), whose white knob is 1.48:1",
  );
  assert.doesNotMatch(
    css,
    /background(-color)?:\s*#cbd5e1\b/i,
    "globals.css paints the old slate-300 track (#cbd5e1) as a background",
  );
  const callSites = [
    ["web/src/components/Switch.tsx", component],
    ["web/src/app/settings/page.tsx", srcAt("app/settings/page.tsx")],
    ["web/src/app/form-settings/page.tsx", srcAt("app/form-settings/page.tsx")],
    ["web/src/app/sales/page.tsx", srcAt("app/sales/page.tsx")],
  ] as const;
  const slateBack: string[] = [];
  for (const [rel, src] of callSites) {
    // `codeOnly` blanks the comments that quote the old class in prose, so this
    // reads the CODE: a real `bg-slate-300` in a className is caught, an
    // explanation of the fix (which quotes it) is not.
    codeOnly(src).split("\n").forEach((line, i) => {
      if (/bg-slate-300/.test(line)) slateBack.push(`${rel}:${i + 1}`);
    });
  }
  assert.deepEqual(
    slateBack,
    [],
    `the old slate-300 OFF track is back in code (a white knob on it measures 1.48:1):\n  ${slateBack.join("\n  ")}`,
  );
  // 4. the knob has a real edge (the shadow is what separates it from the card).
  assert.match(body("jf-switch-knob"), /box-shadow:\s*0\s+1px\s+2px/, ".jf-switch-knob has no edge (box-shadow)");
  // And the state is driven by the ATTRIBUTE the component sets, so a switch
  // whose aria-checked and paint disagree is not expressible.
  assert.match(css, /\.jf-switch\[aria-checked="true"\]\s*\{[^}]*background:\s*var\(--jf-primary\)/, ".jf-switch's ON state is not --jf-primary on aria-checked");
});

test("the guard would fail on the markup it replaced (in-file bite proof)", () => {
  // The exact shape that shipped in all three files, planted hermetically: the
  // scan must report it. Without this, a scan that silently matched nothing
  // would read as a clean tree.
  const old = [
    '<button type="button" role="switch" aria-checked={sfIndexing}',
    '  onClick={() => setSfIndexing((v) => !v)}',
    '  className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${',
    '    sfIndexing ? "bg-[var(--jf-primary)]" : "bg-slate-300"',
    '  }`}>',
    '  <span aria-hidden className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${',
    '    sfIndexing ? "left-[22px]" : "left-0.5"}`} />',
    "</button>",
  ].join("\n");
  const els = switchElements(old);
  assert.equal(els.length, 1, "the scan did not find the planted switch element");
  assert.equal(
    els[0].cls.split(/\s+/).includes("jf-switch"),
    false,
    "the planted old markup was reported as compliant — the scan cannot bite",
  );
  // And the shared component's own markup passes the same scan.
  const els2 = switchElements(component);
  assert.equal(els2.length, 1, "the scan did not find the switch in Switch.tsx");
  assert.ok(
    els2[0].cls.split(/\s+/).includes("jf-switch"),
    "Switch.tsx's own markup would be reported by its own guard",
  );
});
