// Regression guard for "the primary CTA wraps onto two lines on a phone"
// (card t_ea90e668).
//
// THE BUG, measured in Chromium against a production build: `.jf-btn-primary`
// and `.jf-btn-ghost` set padding/size/radius but neither `white-space` nor
// `flex-shrink`, and they are used as the second child of the non-wrapping
// header row `flex items-center justify-between` (dashboard, forms, media,
// templates, brand, ...). A flex item defaults to `flex-shrink: 1`, so on a
// narrow screen the BUTTON — not the heading block — was the item that gave
// way, and because its label could wrap it shrank by WRAPPING rather than by
// refusing. `min-height: 40px` is a floor, not a ceiling, so the box grew.
//
//   before: dashboard CTA 53.59px tall / 2 lines at 320px, all five locales
//           (ms "+ Borang Baru" 53.59px, zh/ar/ta the same); a longer label
//           reached 3 lines / 72.39px
//   at 1280px the same button was 40px / 1 line — the box changed shape with
//           the viewport
//   after: 40px / 1 line at both 320px and 1280px, for every locale
//
// WHY THIS FILE IS A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (no vitest/jest; see
//   src/lib/adminResponsive.test.ts and src/lib/i18n.test.ts, which are written
//   the same way) and the a11y gate runs axe-core, which cannot measure layout
//   deformation. So `npm run test:unit` pins the two declarations BY NAME on
//   the two classes, and the pixel measurements above are made with Playwright
//   in a real browser and quoted here and in the PR.
//
// WHAT IS PINNED, AND WHY BOTH PROPERTIES
//   `flex-shrink: 0` is what stops the squeeze, and it alone is sufficient:
//   with `flex-shrink: 0` the button keeps its max-content width, so there is
//   no pressure to break the label. `white-space: nowrap` is the belt to that
//   brace, and it is the declaration that keeps the label on one line in any
//   OTHER container that constrains the button (a grid track, a `min-w-0`
//   parent, a nested flex row) — the class is shared by 137 call sites and most
//   of them are not the two header rows measured here. Both are pinned because
//   either one can be deleted without this file noticing if only the other is
//   asserted.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const css = readFileSync(fileURLToPath(new URL("../app/globals.css", import.meta.url)), "utf8");

/** The body of a `.name { ... }` rule (first match), or null. */
function ruleBody(name: string): string | null {
  const m = css.match(new RegExp(`\\.${name}\\s*\\{([^}]*)\\}`));
  return m ? m[1] : null;
}

/** Every `jf-btn*` class the app actually uses in JSX/TSX classNames. */
function usedBtnClasses(): string[] {
  const out = new Set<string>();
  for (const rel of [
    "../app/dashboard-client.tsx",
    "../app/forms/page.tsx",
    "../app/media/page.tsx",
    "../app/templates/page.tsx",
    "../app/settings/page.tsx",
    "../app/brand/brand-client.tsx",
    "../components/Shell.tsx",
  ]) {
    const src = readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
    for (const m of src.matchAll(/jf-btn[a-z-]*/g)) out.add(m[0]);
  }
  return [...out].sort();
}

const CLASSES = ["jf-btn-primary", "jf-btn-ghost"] as const;

test("the guard is looking at the real stylesheet (it cannot pass vacuously)", () => {
  // The two classes were present in globals.css when the defect was measured;
  // if either rule is renamed or the file is swapped, fail here rather than
  // assert against a null body.
  for (const cls of CLASSES) {
    const body = ruleBody(cls);
    assert.ok(body, `no .${cls} rule in globals.css — re-anchor this guard`);
    // The declarations the measured defect lives among must still be there, so
    // this is provably the button rule and not some other renamed block.
    assert.match(body, /min-height:\s*40px/, `.${cls} is not the measured button rule (no min-height:40px)`);
    assert.match(body, /padding:\s*10px 18px/, `.${cls} is not the measured button rule (no padding:10px 18px)`);
  }
});

test("a shared button label never wraps and the button never shrinks", () => {
  // Measured defect (production build, Chromium): without these the dashboard
  // CTA is 53.59px tall at a 320px viewport instead of 40px, and a longer
  // locale's label reaches 72.39px — a 3-line label inside a 8px-radius box.
  for (const cls of CLASSES) {
    const body = ruleBody(cls)!;
    assert.match(
      body,
      /(^|\s|;)white-space:\s*nowrap\s*;/,
      `.${cls} has no \`white-space: nowrap\` — its label can break onto a second line, so the button grows past its 40px single-line height on a narrow screen`,
    );
    assert.match(
      body,
      /(^|\s|;)flex-shrink:\s*0\s*;/,
      `.${cls} has no \`flex-shrink: 0\` — as a flex item in the \`flex items-center justify-between\` header rows it defaults to flex-shrink:1, so the button gives up width to the heading block and (its label being wrappable) shrinks by wrapping`,
    );
  }
});

test("the same two properties are on every jf-btn* class the app uses", () => {
  // This is a class defect, not one button: the three classes carry 137 call
  // sites. Any button class the app actually renders must carry the guard, so a
  // future `.jf-btn-*` addition cannot silently re-create the wrap.
  for (const cls of usedBtnClasses()) {
    const body = ruleBody(cls);
    if (body === null) {
      // `.jf-btn` and `.jf-btn-outline` are used in JSX with no rule of their
      // own (they are unstyled hooks; only the padding utilities on their
      // className give them a shape). They cannot wrap for the same reason a
      // plain <button> cannot be given one by this stylesheet, so a missing
      // rule is not this defect — but it must stay a deliberate omission, and
      // this assertion documents it.
      assert.ok(
        cls === "jf-btn" || cls === "jf-btn-outline",
        `.${cls} is used in the app but has no rule in globals.css and is not one of the two known unstyled hooks`,
      );
      continue;
    }
    assert.match(body, /white-space:\s*nowrap\s*;/, `.${cls} is a rendered button class without \`white-space: nowrap\``);
    assert.match(body, /flex-shrink:\s*0\s*;/, `.${cls} is a rendered button class without \`flex-shrink: 0\``);
  }
});

test("the two header classes still cannot be shrunk by a Tailwind utility", () => {
  // globals.css is a bare `@import "tailwindcss"` with the jf-* rules
  // UNLAYERED, so these rules outrank same-specificity Tailwind utilities.
  // If the rules ever move into an `@layer` the outcome starts depending on
  // layer order, which is exactly the fragility the fix was chosen to avoid.
  const before = css.slice(0, css.indexOf(".jf-btn-primary"));
  assert.doesNotMatch(
    before,
    /@layer\s+[a-z-]+\s*\{[\s\S]*\}/,
    "globals.css now opens a CSS @layer before the jf-btn rules — the jf-btn declarations are no longer unlayered, so a Tailwind utility of equal specificity could win",
  );
});

test("the /templates header group can wrap, so two nowrap buttons cannot push the page sideways", () => {
  // The nowrap fix took away the shrink the buttons used to absorb by wrapping,
  // and /templates puts TWO of them in a `shrink-0` group that is a child of
  // the header row's COLUMN half below `sm`. Measured at a 320px viewport after
  // the nowrap change and BEFORE this: the group was 367.6px wide inside a
  // 272px column, document scrollWidth 392 in `en`, 485 in `ms`, 408 in `ta`
  // (and 0 in the same build with the declarations removed). With flex-wrap +
  // max-w-full: 320 for every locale, and each label still fits the column on
  // one line (en 197.4/134.2, ms 271.6/153.9, ta 225.9/122.5).
  //
  // This is the only header in the repo with two nowrap buttons in a shrink-0
  // column child — `grep -rn 'shrink-0 items-center gap-'` finds five groups
  // and only this one is a column-half sibling of the title block.
  const page = readFileSync(fileURLToPath(new URL("../app/templates/page.tsx", import.meta.url)), "utf8");
  const m = page.match(/<div className="[^"]*shrink-0[^"]*items-center gap-2[^"]*self-start sm:self-auto[^"]*">/);
  assert.ok(m, "the /templates header action group moved — re-anchor this guard");
  assert.match(
    m[0],
    /className="[^"]*\bflex-wrap\b[^"]*"/,
    "the /templates header action group lost flex-wrap — with two nowrap buttons inside a shrink-0 child of the column header half, the group keeps its max-content width (measured 367.6px in a 272px column at 320px) and the page scrolls sideways",
  );
  assert.match(
    m[0],
    /className="[^"]*\bmax-w-full\b[^"]*"/,
    "the /templates header action group lost max-w-full, so flex-wrap cannot be honoured inside the 272px column",
  );
});
