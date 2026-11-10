// The landing hero's demo order form asks HOW the buyer wants to pay, ONCE, from the
// dictionary — and it asks ABOVE the Pay button (card t_453489b9, the approved mockup).
//
// WHAT THE PAGE USED TO SHOW. Two cards ago (t_b4e5881d) this demo printed its payment
// methods twice — a caption and a chip strip — and the fix deleted the caption so one
// strip remained, UNDER the Pay button:
//
//     <button …>{t("land.fmpay")}</button>
//     <ul>{LANDING_PAY_METHODS.map((method) => <li>{method}</li>)}</ul>
//
// The problem with the survivor was the same problem the caption had, one layer down:
// a buyer reads the button, presses it, and only THEN learns that a set of methods
// exists — and they cannot pick one. The real product no longer works that way: both
// checkout surfaces render a radio group ABOVE the button and the order carries the
// choice to CHIP (api/internal/httpapi/paymethod.go, payments/paymethod.go). The mockup
// Wan approved replaces the strip with that control.
//
// THE CONTRACT THIS FILE PINS:
//
//   A. The demo's method list is `LANDING_PAY_OPTIONS` — four GROUPS, in the product's
//      order — rendered through exactly one `.map(`. No chip strip, no second list.
//   B. NO landing component writes a method name in its own source (comments excluded).
//      The labels and descriptions come from `i18nDict`, which is also what makes the
//      demo legible in the other four locales.
//   C. The picker sits ABOVE the Pay button, and the button's second line names the
//      chosen method: `land.payvia` exists in all five locales and carries {method}.
//   D. No method name is printed TWICE anywhere on the demo (the defect t_b4e5881d
//      measured, and the reason a caption + strip was one too many statements).
//
// IT BITES. The deleted pre-fix block is kept as a FIXTURE and run through the same
// scanner: it must still report FPX twice (caption + chip), GrabPay twice and the chips'
// VISA once. A scanner that reported nothing on the fixture would pass vacuously.
//
// WHAT IT DOES NOT COVER (stated, not hidden):
//   * It reads SOURCE, not pixels. The built page was driven in a browser at 390px and
//     1440px for this card and the rendered rows, the selected row and the button's two
//     lines were read out of the live DOM.
//   * It says nothing about payment-method names OUTSIDE the landing demo (settings' fee
//     rows, /docs, the payment forms): those legitimately name methods.
//   * It does not decide whether CHIP supports a brand. It decides that the landing demo
//     may not assert brands of its own, and that the demo's control matches the
//     product's.
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS, LANDING_PAY_OPTIONS } from "../components/i18nDict.ts";

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

/**
 * Every occurrence of a method name in `src`, as `name @ offset`, in source order.
 *
 * WHOLE WORDS, not substrings: the picker's dictionary keys are `land.payfpx` /
 * `land.paydnqr` — an identifier a translator never sees — and a substring test
 * would report "FPX" inside `payfpx`, i.e. fail the hero for NAMING its own key.
 * `\bfpx\b` does not match inside `payfpx` (no word boundary between `y` and
 * `f`), while a re-typed method name (`"FPX"`, `FPX ·`, `{chip}`) still does —
 * which is what the fixture arm below keeps proving.
 */
function mentions(src: string): Array<{ name: string; at: number }> {
  const out: Array<{ name: string; at: number }> = [];
  const hay = src;
  for (const name of METHODS) {
    const re = new RegExp(`\\b${name}\\b`, "gi");
    for (const m of hay.matchAll(re)) {
      out.push({ name, at: m.index ?? 0 });
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
  // Non-vacuity: the Pay button is what the deleted strip sat under, so finding its
  // dictionary lookup proves the file is the component this card fixed.
  assert.ok(
    heroCode.includes('t("land.fmpay"'),
    "the hero must still render its Pay button from land.fmpay — the read found no such lookup",
  );
});

test("the demo's method list is the four groups, in the product's order", () => {
  assert.deepEqual(
    LANDING_PAY_OPTIONS.map((o) => o.id),
    ["fpx", "duitnow_qr", "card", "ewallet"],
    "LANDING_PAY_OPTIONS is the one place the demo's method rows may be written down; it " +
      "must list the four groups in the order the product's picker uses",
  );
  assert.equal(
    new Set(LANDING_PAY_OPTIONS.map((o) => o.labelKey)).size,
    LANDING_PAY_OPTIONS.length,
    "two rows share a label key — the demo would print one method twice",
  );
});

test("the hero renders the picker once, through one map, and no chip strip", () => {
  const maps = [...heroCode.matchAll(/LANDING_PAY_OPTIONS\.map\(/g)];
  assert.equal(
    maps.length,
    1,
    `the hero must render the method rows exactly once, found ${maps.length} `.trim() +
      ".map() call(s) — a second one is a second list",
  );
  // The row's text comes from the component's own static copy table, and the table is
  // keyed by the SAME ids the list holds: a row can never render blank, and a new option
  // cannot be added to the list without its copy.
  assert.match(
    heroCode,
    /LANDING_PAY_OPTIONS\.map\(\((\w+), (\w+)\)[\s\S]{0,2000}?const text = copy\[\2\]/,
    "each row must read its copy from the component's copy list, by the map index",
  );
  assert.match(heroCode, /\{text\.label\}/, "the row's label must come from the copy list");
  // The copy list is parallel to the options list: same length, same order. A row with
  // no copy renders blank, and a copy entry with no row is dead weight.
  const copyRows = [...heroCode.matchAll(/\{ label: t\("land\.pay\w+"\), desc: t\("land\.pay\w+"\) \}/g)];
  assert.equal(
    copyRows.length,
    LANDING_PAY_OPTIONS.length,
    `the hero's copy list has ${copyRows.length} row(s) for ${LANDING_PAY_OPTIONS.length} option(s) — ` +
      "they must be the same list in the same order",
  );
  // Every label/desc key is asked for with a LITERAL, so src/lib/i18nKeys.ts can see it.
  // `t(option.labelKey)` would be invisible to that scan: a key dropped in a merge then
  // renders the raw key to every user in every locale.
  for (const option of LANDING_PAY_OPTIONS) {
    for (const key of [option.labelKey, option.descKey]) {
      assert.ok(
        heroCode.includes(`t("${key}")`),
        `${key} is not resolved from a literal in the hero — a computed key is invisible to the i18n scan`,
      );
    }
  }
  assert.match(
    heroCode,
    /import\s*\{[^}]*LANDING_PAY_OPTIONS[^}]*\}\s*from\s*"\.\.\/i18nDict"/,
    "the hero must import LANDING_PAY_OPTIONS from ../i18nDict — a local array is the defect",
  );
  // The chip strip is gone: nothing renders LANDING_PAY_METHODS any more.
  assert.ok(
    !heroCode.includes("LANDING_PAY_METHODS"),
    "the hero still renders the chip strip — that is the list this card replaced with the picker",
  );
  // No caption above a strip (the t_b4e5881d defect) and no list after the Pay button:
  // the product rows are a <ul> ABOVE it, so the check is on what follows the button.
  assert.ok(!/Card\s*·/.test(heroCode), "a 'Card · …' caption is back in the hero");
  const afterPay = heroCode.slice(heroCode.indexOf('t("land.fmpay"'));
  assert.ok(
    !afterPay.includes("<li"),
    "a <li> follows the Pay button — the chip strip (or a second list) is back",
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
        "its own method list, and the one list lives in LANDING_PAY_OPTIONS",
    );
  }
});

test("the picker sits ABOVE the Pay button, which names the chosen method", () => {
  const picker = heroCode.indexOf('role="radiogroup"');
  const pay = heroCode.indexOf('t("land.fmpay"');
  assert.ok(picker > 0, "the hero no longer renders a radiogroup — the picker this card added is gone");
  assert.ok(pay > 0, "the hero no longer renders its Pay button");
  assert.ok(
    picker < pay,
    "the picker must come BEFORE the Pay button in the markup (the approved mockup): a " +
      "buyer has to be able to answer before they press Pay",
  );
  // The button's second line is the chosen method, through the dictionary, in all five
  // locales — a hardcoded "via FPX" would be English on four of them.
  assert.ok(
    heroCode.includes('t("land.payvia"'),
    "the Pay button must name the chosen method through land.payvia",
  );
  for (const lang of LANGS) {
    const text = dict["land.payvia"]?.[lang];
    assert.ok(text && text.includes("{method}"), `land.payvia.${lang} must carry {method}: ${text}`);
  }
});

test("the fixture — the pre-fix block — is reported by the same scanner", () => {
  // The bytes t_b4e5881d deleted, kept so the arms above are shown to MEASURE the defect
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
  // The duplication the earlier card measured: FPX, DuitNow QR and GrabPay were printed twice.
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

test("every row is legible in all five locales", () => {
  for (const option of LANDING_PAY_OPTIONS) {
    for (const key of [option.labelKey, option.descKey] as const) {
      const entry = dict[key];
      assert.ok(entry, `${key} must exist — the picker row would render a blank line`);
      for (const lang of LANGS) {
        const text = entry![lang];
        assert.ok(text && text.length > 0, `${key}.${lang} is empty — a locale would render a blank row`);
      }
    }
  }
  // The two scheme names are the same word in every locale (a brand is not translated),
  // so a "translation" that changed one is a bug, not a localisation.
  for (const key of ["land.payfpx", "land.paydnqr"] as const) {
    const en = dict[key]!.en;
    for (const lang of LANGS) {
      assert.equal(dict[key]![lang], en, `${key}.${lang} must read "${en}" — a scheme name is not translated`);
    }
  }
  assert.equal(dict["land.payfpx"]!.en, "FPX");
  assert.equal(dict["land.paydnqr"]!.en, "DuitNow QR");
});

test("the claim survives in the dictionary, in all five locales", () => {
  // The picker names the four groups; these two keys are what still tell the visitor that
  // the product accepts them (they are rendered by StepsBand and the feature list).
  for (const key of ["land.path2b", "land.f2"] as const) {
    const e = dict[key];
    assert.ok(
      e,
      `${key} must exist — it is what tells the visitor which methods are accepted`,
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
