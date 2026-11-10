// The landing hero's demo order form must print its payment methods ONCE, from the
// dictionary (card t_b4e5881d).
//
// WHAT WAS ON THE PAGE. Immediately under the Pay button the mock rendered its methods
// TWICE — a caption and, right under it, a chip strip:
//
//     <p>Card · FPX · DuitNow QR · GrabPay</p>
//     <ul>{["FPX", "DuitNow QR", "VISA", "GrabPay", "Boost"].map(…)}</ul>
//
// and the two copies DISAGREED: "Card" in the caption where the chips said "VISA" (a buyer
// reads those as two different methods), four methods in the caption and five in the chips,
// and a different order in each. Three things made it worse than duplication:
//
//   1. The page ALREADY claims the same thing twice, in all five locales, from
//      `land.path2b` (the "Buyer pays" step in StepsBand) and `land.f2` (the feature list
//      above the hero). The caption + chips were a third and fourth statement inside ~40px
//      of a phone screen.
//   2. It showed a strip the PRODUCT does not have. The real menu checkout
//      (api/internal/httpapi/menu.go) renders the Pay button and then only `<p class="err">`;
//      a buyer picks a method on CHIP's hosted page.
//   3. Both copies were HARDCODED ENGLISH on a page whose every other string is in
//      `i18nDict` across five locales, so a Malay or Arabic visitor read "Card · FPX" inline
//      beside translated copy.
//
// THE FIX THIS FILE PINS. The caption is deleted; ONE chip strip stays, and its names come
// from `LANDING_PAY_METHODS` in components/i18nDict.ts — the one place a brand token may be
// written down. So the contract is:
//
//   A. Each method name is listed exactly once, in `LANDING_PAY_METHODS` (no duplicate,
//      five entries), and the hero renders that list through exactly one `.map(` — one
//      strip, one pass. That is what "prints each method once" means once the caption is
//      gone.
//   B. NO landing component writes a method name in its own source, comments excluded. This
//      is the arm that stops a re-typed array or a second caption from coming back, and it
//      is why the names live in the dictionary instead of the component.
//   C. The claim still REACHES the visitor through the dictionary: `land.path2b` and
//      `land.f2` exist in all five locales and name FPX and DuitNow QR in every one (brand
//      tokens are not translated). Deleting the caption must not have deleted the claim.
//   D. The old caption shape — methods joined by "·" — is gone from the hero.
//
// IT BITES. The deleted pre-fix block is kept below as a FIXTURE and run through the same
// scanner: it must report FPX twice (caption + chip), GrabPay twice and the chips' VISA
// once, and the caption's "Card" as a separate method word. A scanner that reported nothing
// on the fixture would pass the hero arm vacuously.
//
// WHAT IT DOES NOT COVER (stated, not hidden):
//   * It reads SOURCE, not pixels. The built page was driven in a browser for this card and
//     the chip count measured in the live DOM (5 chips, each label once, no caption).
//   * It says nothing about payment-method names OUTSIDE the landing demo (settings' fee
//     rows, /docs/payment-fees, the payment forms): those legitimately name methods, and a
//     page-wide rule would fail on them.
//   * It does not decide whether CHIP supports a brand. It decides that the landing demo
//     may not assert brands of its own.
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS, LANDING_PAY_METHODS } from "../components/i18nDict.ts";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));
// The web suite's guards read each other's source; `buildContext.test.ts` needs this exact
// shape to resolve the base directory, so keep it a one-liner over `new URL(rel, …)`.
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/** The method names the hero printed twice before this card. Case-insensitive match. */
const METHODS = ["FPX", "DuitNow QR", "VISA", "GrabPay", "Boost"] as const;

/**
 * The landing demo tree: the hero plus every component the landing page renders next to it,
 * and the page itself. A method list moved into any of these is the same defect in a
 * different file.
 */
const LANDING_FILES = [
  "../components/landing/HeroOrderForm.tsx",
  "../components/landing/StepsBand.tsx",
  "../components/landing/ShowcaseCard.tsx",
  "../components/landing/AgentBand.tsx",
  "../app/landing/page.tsx",
];

/**
 * Remove comments so the hero may NAME the defect in prose (this card's fix documents itself
 * in a `{/* … *\/}` block) while its rendered code stays clean. The lookbehind keeps a
 * `https://` URL inside a string from being cut.
 */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(?<![:"'`])\/\/[^\n]*/g, "");
}

/** Every occurrence of a method name in `src`, as `name @ offset`, in source order. */
function mentions(src: string): Array<{ name: string; at: number }> {
  const out: Array<{ name: string; at: number }> = [];
  const hay = src.toLowerCase();
  for (const name of METHODS) {
    const needle = name.toLowerCase();
    for (let at = hay.indexOf(needle); at !== -1; at = hay.indexOf(needle, at + needle.length)) {
      out.push({ name, at });
    }
  }
  return out.sort((a, b) => a.at - b.at);
}

/** The line of `src` a byte offset falls on, trimmed — what a failure shows. */
function lineAt(src: string, offset: number): string {
  const start = src.lastIndexOf("\n", offset) + 1;
  const end = src.indexOf("\n", offset);
  return src.slice(start, end === -1 ? src.length : end).trim();
}

const HERO = LANDING_FILES[0];
const heroSource = read(HERO);
const heroCode = stripComments(heroSource);

/** The dictionary as plain records, so a locale lookup is typed rather than cast per key. */
const dict = DICT as unknown as Record<string, Record<string, string>>;

test("the harness reads the real hero component", () => {
  assert.ok(existsSync(pathOf(HERO)), `${HERO} must exist — a moved or deleted hero would make this guard pass over nothing`);
  assert.ok(
    heroSource.length > 2000,
    `expected to read the hero's own source, got ${heroSource.length} chars`,
  );
  // Non-vacuity: the Pay button is what the deleted caption sat under, so finding its
  // dictionary lookup proves the file is the component this card fixed.
  assert.ok(
    heroCode.includes('t("land.fmpay"'),
    "the hero must still render its Pay button from land.fmpay — the read found no such lookup",
  );
});

test("each method name is listed exactly once, in the dictionary", () => {
  assert.deepEqual(
    [...LANDING_PAY_METHODS],
    [...METHODS],
    "LANDING_PAY_METHODS is the one place the demo's method names may be written down; it " +
      "must list exactly the five brands the hero shows, in order",
  );
  assert.equal(
    new Set(LANDING_PAY_METHODS).size,
    LANDING_PAY_METHODS.length,
    "a method is listed twice in LANDING_PAY_METHODS — the strip would print it twice",
  );
});

test("the hero renders the list once, through one map, and nothing else", () => {
  const maps = [...heroCode.matchAll(/LANDING_PAY_METHODS\.map\(/g)];
  assert.equal(
    maps.length,
    1,
    `the hero must render the method list exactly once, found ${maps.length} `.trim() +
      ".map() call(s) — a second one is a second strip",
  );
  // The chip itself is the list value, not a re-typed string: `{method}`, after `{method}`
  // was chosen as the map's parameter name.
  assert.match(
    heroCode,
    /LANDING_PAY_METHODS\.map\(\((\w+)\)[\s\S]{0,400}>\s*\{\1\}\s*<\/li>/,
    "the chip must render the mapped value, not a literal — the mapper's parameter has to " +
      "reach the <li>'s text",
  );
  // And the hero takes the names from the dictionary rather than holding its own copy.
  assert.match(
    heroCode,
    /import\s*\{[^}]*LANDING_PAY_METHODS[^}]*\}\s*from\s*"\.\.\/i18nDict"/,
    "the hero must import LANDING_PAY_METHODS from ../i18nDict — a local array is the defect",
  );
  // No caption above the strip. The old one was a `·`-joined method sentence; a method name
  // cannot appear in this file at all (asserted below), which subsumes "the caption is gone",
  // so this arm only has to show the strip is the LAST thing under the Pay button.
  assert.ok(
    !/Card\s*·/.test(heroCode),
    "a 'Card · …' caption is back in the hero — the duplication this card deleted",
  );
});

test("no landing component writes a payment-method name in its own source", () => {
  for (const file of LANDING_FILES) {
    const code = stripComments(read(file));
    const found = mentions(code);
    assert.deepEqual(
      found.map((f) => `${file}: ${f.name} @ ${lineAt(code, f.at)}`),
      [],
      `${file} names a payment method outside i18nDict — the landing demo must not carry ` +
        "its own method list, and the one list lives in LANDING_PAY_METHODS",
    );
  }
});

test("the fixture — the pre-fix block — is reported by the same scanner", () => {
  // The bytes this card deleted, kept so the arms above are shown to MEASURE the defect
  // rather than to pass on an empty page.
  const PRE_FIX = [
    '        <p className="mt-2 text-center text-jf-3xs text-[var(--jf-muted)]">',
    "          Card · FPX · DuitNow QR · GrabPay",
    "        </p>",
    "",
    '        <ul className="mt-2 flex flex-wrap justify-center gap-1.5">',
    '          {["FPX", "DuitNow QR", "VISA", "GrabPay", "Boost"].map((chip) => (',
    "            <li key={chip}>{chip}</li>",
    "          ))}",
    "        </ul>",
  ].join("\n");

  const found = mentions(stripComments(PRE_FIX));
  // The duplication the card measured: FPX, DuitNow QR and GrabPay were printed twice.
  for (const twice of ["FPX", "DuitNow QR", "GrabPay"]) {
    assert.equal(
      found.filter((f) => f.name === twice).length,
      2,
      `the fixture printed ${twice} twice (caption + chip) — the scanner must say so`,
    );
  }
  // VISA and Boost appeared only in the chips: the caption named four methods, the chips five.
  assert.equal(found.filter((f) => f.name === "VISA").length, 1);
  assert.equal(found.filter((f) => f.name === "Boost").length, 1);

  // The contradiction inside the duplication: the caption's card-payment word is not the
  // chips' word for the same thing, and the caption is joined by "·".
  const caption = PRE_FIX.split("\n")[1];
  assert.ok(caption.includes("Card"), "control: the pre-fix caption's card-payment word");
  assert.ok(!caption.includes("VISA"), "control: the pre-fix caption never said VISA");
  assert.ok(caption.includes("·"), "control: the pre-fix caption is the '·'-joined sentence");
});

test("the claim survives in the dictionary, in all five locales", () => {
  // The strip no longer states the methods in the page's own words, so these two keys are
  // what carries the claim to the visitor. Both must exist, be filled in, and still name
  // the brands (which are not translated in any locale).
  for (const key of ["land.path2b", "land.f2"] as const) {
    const e = dict[key];
    assert.ok(
      e,
      `${key} must exist — the hero's caption was deleted, so this key is what still tells ` +
        "the visitor which methods are accepted",
    );
    for (const lang of LANGS) {
      const text = e![lang];
      assert.ok(text && text.length > 0, `${key}.${lang} is empty — a locale would render a blank claim`);
      assert.ok(text.includes("FPX"), `${key}.${lang} no longer names FPX: ${text}`);
      assert.ok(text.includes("DuitNow QR"), `${key}.${lang} no longer names DuitNow QR: ${text}`);
    }
  }
});

test("each claim is rendered from exactly one place in the landing demo", () => {
  for (const key of ["land.path2b", "land.f2"] as const) {
    const sites = LANDING_FILES.filter((f) => stripComments(read(f)).includes(`"${key}"`));
    assert.equal(
      sites.length,
      1,
      `${key} must be written down in exactly one landing file, found ` +
        `${sites.length === 0 ? "none" : sites.join(", ")}`,
    );
  }
  // The two call sites this fix relies on, named so a rename has to be a deliberate edit
  // rather than a silent loss of the claim.
  assert.ok(
    stripComments(read("../components/landing/StepsBand.tsx")).includes('t("land.path2b")'),
    "StepsBand must render land.path2b — it is the surviving statement of the methods",
  );
  assert.ok(
    stripComments(read("../app/landing/page.tsx")).includes("t(f.key)"),
    "the landing page renders its feature lines through t(f.key); land.f2 lives in that table",
  );
});
