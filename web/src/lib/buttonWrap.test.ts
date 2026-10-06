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
// AND, SINCE CARD t_fbfe40ca, THAT THE CLASS IS DEFINED AT ALL
//   The same walk had a second, larger hole: it read SEVEN hand-named files and
//   then EXCUSED the very tokens it was asked about.
//
//     * The seven files were dashboard-client, forms, media, templates,
//       settings, brand-client and Shell. The defect lived in three of them
//       (media, form-settings, settings) and in TWO IT COULD NOT SEE AT ALL —
//       `components/MediaPicker.tsx` and `components/ProductPicker.tsx`. A walk
//       with a manifest is a walk a NEW file can step outside, which is the one
//       thing this guard exists to prevent.
//     * `body === null` (the class has no rule in globals.css) was not a
//       failure: it was `continue`-d past for the two names
//       `jf-btn` / `jf-btn-outline`, on the reasoning that an unstyled hook
//       "cannot wrap, so a missing rule is not this defect". True about
//       wrapping, and exactly why the page shipped the defect — five `jf-btn`
//       sites and eleven `jf-btn-outline` sites rendered with no background, no
//       border and `cursor: default`, and this test was green.
//
//   Measured, not read: with the carded byte restored at
//   `app/media/page.tsx` ("jf-btn px-4 py-2 text-sm disabled:opacity-50", the
//   /media primary action), the pre-fix guard at `origin/main` reports
//   `tests 5 / pass 5 / fail 0` — it is green on the exact tree the card was
//   filed about. This version reports `fail 1` naming
//   `.jf-btn is rendered at app/media/page.tsx:410, components/MediaPicker.tsx:139`.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { codeOnly, jsxTags, readSource, tsxFiles } from "./jsxSource.ts";

const css = readFileSync(fileURLToPath(new URL("../app/globals.css", import.meta.url)), "utf8");

/** The body of a `.name { ... }` rule (first match), or null. */
function ruleBody(name: string): string | null {
  const m = css.match(new RegExp(`\\.${name}\\s*\\{([^}]*)\\}`));
  return m ? m[1] : null;
}

/** `/abs/web/src/`, the root every relative path below is resolved against. */
const SRC = fileURLToPath(new URL("../", import.meta.url));

/**
 * Every `jf-btn*` class the app actually renders, from every `.tsx` under
 * `web/src`, mapped to the `file:line` of each site.
 *
 * WHY A TREE WALK AND NOT A FILE LIST. This used to read seven hand-named
 * files, and the two the defect lived in were not among them: `MediaPicker.tsx`
 * and `ProductPicker.tsx` carry 3 of the 5 undefined `jf-btn` call sites, and
 * the file list could not see them — which is the half of this defect a
 * per-call-site fix would have left in place.
 *
 * WHY `className` AND NOT A TEXT MATCH. `jf-btn` is also the PREFIX of the
 * builder's `id`/`htmlFor` values (`jf-blk-<blockid>-<key>`,
 * `jf-cond-<blockid>`, `jf-block-value` in `src/app/builder/page.tsx`). Those
 * are element ids, not classes, and a raw text scan reports them as undefined
 * classes on every run. Reading the JSX tags gives the class lists and nothing
 * else.
 */
function usedBtnClasses(): Map<string, string[]> {
  const byCls = new Map<string, string[]>();
  for (const rel of tsxFiles(SRC)) {
    const src = readSource(`${SRC}${rel}`);
    const code = codeOnly(src);
    for (const e of jsxTags(code)) {
      for (const variant of e.tag.variants) {
        for (const raw of variant.split(/\s+/)) {
          // The shared tag scanner reads a class list that may arrive through an
          // expression, and a template literal's closing brace can end up glued
          // to the last token (`… jf-btn-primary}`). Braces and quotes are never
          // part of a class, so drop them; every real token is unchanged.
          const cls = raw.replace(/[^A-Za-z0-9_\-:[\]/.%#!(),]+/g, "");
          if (!/^jf-btn(-|$)/.test(cls)) continue;
          const line = src.slice(0, e.tag.openIdx).split("\n").length;
          const at = `${rel}:${line}`;
          const sites = byCls.get(cls) ?? [];
          if (!sites.includes(at)) sites.push(at);
          byCls.set(cls, sites);
        }
      }
    }
  }
  return byCls;
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
  const used = usedBtnClasses();
  assert.ok(
    used.has("jf-btn-primary") && used.has("jf-btn-ghost"),
    "the walk found neither shared button class — it is not reading the tree, " +
      "so a green here would prove nothing",
  );
  for (const [cls, sites] of used) {
    const body = ruleBody(cls);
    assert.ok(
      body,
      `.${cls} is rendered at ${sites.join(", ")} but has NO rule in globals.css. ` +
        `A jf-btn* token is a hook for a rule in this stylesheet, not a Tailwind ` +
        `utility — an undefined one renders as plain text (measured on /media in ` +
        `v1.20.37: "jf-btn px-4 py-2 text-sm" computed backgroundColor ` +
        `rgba(0,0,0,0), border 0px and cursor:default, so the page's primary ` +
        `action read as bare text while its sibling on /forms read as a filled ` +
        `button). Give it a rule here, or use a class that has one.`,
    );
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
