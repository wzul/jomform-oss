// The landing page's use-case showcase: every showcase carries a "How to set
// it up" list, and "Collect donations" is one of them (card t_41666fb1).
//
// WHAT WAN ASKED FOR
//   "letak use case jugak untuk collect donation. setiap use case tu bagi jugak
//   instruction macam mana nak setup" — a donation showcase, and setup steps on
//   EVERY showcase, not just sales copy.
//
// THE RULE THIS FILE ENFORCES, and why each half of it matters
//   1. Every showcase has a setup list. There are seven showcases (wedding, QR
//      ordering, tuition, Canva/MCP, affiliate, membership, donation), so the
//      shared "How to set it up" heading and the closing note each appear
//      exactly seven times. A showcase without them is the bug the card exists
//      to fix, and a count is the only thing that notices a block quietly
//      losing its steps in a merge.
//   2. Every step is a PATH, not a promise. `t()` falls back to the raw key
//      when a locale is missing, so a missing step renders "land.qrstep3" to
//      every user in every language and nothing else fails. The dictionary
//      check below runs per locale, not just for the key's existence.
//   3. The copy describes what SHIPS. `products.type` includes `custom_amount`
//      (pay-what-you-want, migration 000085_custom_amount_products), read from
//      the migration rather than from memory or from 000001's append-only
//      history — 000001 still says one_time|subscription, which is why the old
//      arm passed while saying nothing about the live constraint. So the
//      donation showcase must LEAD with the typed amount (a custom_amount
//      product, RM1.00 floor) and keep the fixed denominations as the
//      ALTERNATIVE. The negation arm survives only for capability that is
//      genuinely absent: a donor wall, a progress bar.
//   4. No dead guide link. /docs/guides/<slug> does not exist in this app
//      (src/app/docs has no `guides` directory, and there is no catch-all), so
//      the card's fallback applies: say the guide is coming, do not link a 404.
//
// WHAT THIS CANNOT SEE (stated honestly): it reads source and the dictionary.
// It cannot prove the steps match the shipped UI, cannot judge translation
// quality, and cannot measure layout — the rendered page was driven in a
// browser and the 320px pan measured separately in the card.
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { DICT, LANGS, type Lang } from "../components/i18nDict.ts";

/** The dictionary as an indexable map; DICT's literal type needs the hop. */
const dict = DICT as unknown as Record<string, Record<Lang, string>>;
const entry = (key: string) => dict[key];

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const landing = read("../app/landing/page.tsx");
// The LIVE constraint, not 000001: 000001 is append-only history, and the type
// set was widened by 000085 (pay-what-you-want). This is the file the copy has
// to answer to, so it is the file the guard reads.
const customAmountMigration = read(
  "../../../api/internal/db/migrations/000085_custom_amount_products.up.sql",
);
const templateMigration = read("../../../api/internal/db/migrations/000027_template_library.up.sql");

/**
 * The effective `products.type` set: the LAST `products_type_check` the
 * migration declares (a later ALTER re-declares it, and the last one wins).
 * Parsed rather than hard-coded so the arm below breaks when the schema moves
 * again instead of passing on a stale assumption.
 */
function productTypes(sql: string): string[] {
  const checks = [...sql.matchAll(/ADD CONSTRAINT products_type_check\s+CHECK \(type IN \(([^)]*)\)\)/g)];
  const last = checks.at(-1);
  if (!last) return [];
  return [...last[1].matchAll(/'([a-z_]+)'/g)].map((m) => m[1]).sort();
}

/** One entry per showcase: the i18n prefix its steps use, and how many steps. */
const SHOWCASES = [
  { name: "wedding / RSVP", prefix: "land.wedding", steps: 5 },
  { name: "QR ordering", prefix: "land.qr", steps: 5 },
  { name: "tuition / class fees", prefix: "land.tuition", steps: 5 },
  { name: "Canva → agent → MCP", prefix: "land.canva", steps: 4 },
  { name: "affiliate marketing", prefix: "land.aff", steps: 4 },
  { name: "club membership", prefix: "land.membership", steps: 4 },
  { name: "collect donations", prefix: "land.donation", steps: 5 },
];

/** The donation showcase's own keys (requirement 1 of the card). */
const DONATION_KEYS = [
  "land.donationtitle",
  "land.donationbody",
  "land.donation1",
  "land.donation2",
  "land.donation3",
  "land.donation4",
  "land.donationcta",
  "land.donationmcp",
  ...Array.from({ length: 5 }, (_, i) => `land.donationstep${i + 1}`),
];

const ALL_KEYS = [...DONATION_KEYS, "land.setup", "land.guidesoon"];

const count = (needle: string) => landing.split(needle).length - 1;

test("the donation showcase is on the page with every one of its keys", () => {
  for (const key of DONATION_KEYS) {
    assert.equal(
      count(`t("${key}")`),
      1,
      `app/landing/page.tsx does not render ${key} exactly once — the donation showcase ` +
        `(card t_41666fb1) is the only thing that renders it`,
    );
  }
  // A showcase that renders its title but no steps is the "no instructions"
  // shape the card was filed against: assert the steps are inside an ordered
  // list, not floating paragraphs.
  assert.match(
    landing,
    /<ol className="mt-3 list-decimal[^"]*">\s*<li>\{t\("land\.donationstep1"\)\}<\/li>/,
    "the donation steps are not an ordered list starting at land.donationstep1",
  );
});

test("every showcase has a 'How to set it up' list, in order", () => {
  assert.equal(
    count('t("land.setup")'),
    SHOWCASES.length,
    `the "How to set it up" heading (land.setup) must appear once per showcase — ` +
      `${SHOWCASES.length} showcases, found ${count('t("land.setup")')}`,
  );
  assert.equal(
    count('t("land.guidesoon")'),
    SHOWCASES.length,
    `the closing note (land.guidesoon) must appear once per setup list — one showcase has no ` +
      `list, or a list has no closing line`,
  );
  assert.equal(count("<ol className=\"mt-3 list-decimal"), SHOWCASES.length, "one ordered list per showcase");

  for (const s of SHOWCASES) {
    // In order, 1..n, each rendered exactly once: a renumbered or dropped step
    // is the drift this pins.
    let last = -1;
    for (let i = 1; i <= s.steps; i++) {
      const key = `${s.prefix}step${i}`;
      const at = landing.indexOf(`t("${key}")`);
      assert.ok(at > 0, `${s.name}: ${key} is not rendered — the showcase lost step ${i}`);
      assert.ok(at > last, `${s.name}: ${key} is out of order on the page`);
      assert.equal(count(`t("${key}")`), 1, `${s.name}: ${key} is rendered more than once`);
      last = at;
    }
    // A step beyond the declared count would mean the list grew without this
    // guard knowing it (the count above is what keeps the two in step).
    assert.ok(
      !landing.includes(`t("${s.prefix}step${s.steps + 1}")`),
      `${s.name}: ${s.prefix}step${s.steps + 1} is rendered but SHOWCASES says ${s.steps} steps`,
    );
  }
});

test("every new key exists in all five locales, and the English is English", () => {
  const missing: string[] = [];
  for (const key of ALL_KEYS) {
    const e = entry(key);
    if (!e) {
      missing.push(`${key}: absent from DICT`);
      continue;
    }
    for (const lang of LANGS) {
      const value = e[lang];
      if (typeof value !== "string" || value.trim() === "") missing.push(`${key}: no ${lang}`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    "these landing keys are missing a locale. t() falls back to the raw key, so the page shows " +
      `"land.donationtitle" instead of a sentence:\n  ${missing.join("\n  ")}`,
  );

  // Language discipline (Wan, repeatedly — card t_541a31d5 fixed the same
  // defect in the dashboard): `en` is fully English, `ms` is Malay. A Malay
  // word left in an English sentence is what this catches; it is a stopword
  // list, so it cannot object to a product name such as "Donation".
  const MALAY = /\b(?:anda|untuk|dengan|pada|dari|dalam|tetapan|borang|produk|jualan|langganan|butang|pautan|halaman)\b/i;
  const NON_LATIN = /[\u0600-\u06FF\u0900-\u097F\u0E00-\u0E7F\u4E00-\u9FFF\u0B80-\u0BFF]/;
  const offenders: string[] = [];
  for (const key of ALL_KEYS) {
    const e = entry(key);
    if (!e) continue;
    if (MALAY.test(e.en)) offenders.push(`${key}.en contains a Malay word: ${e.en}`);
    if (NON_LATIN.test(e.en)) offenders.push(`${key}.en is not Latin script: ${e.en}`);
    if (NON_LATIN.test(e.ms)) offenders.push(`${key}.ms is not Latin script: ${e.ms}`);
  }
  assert.deepEqual(offenders, [], `language discipline:\n  ${offenders.join("\n  ")}`);
});

test("every step names a real path in the product, not a sales sentence", () => {
  // A step has to be followable: either it walks the UI with `→`
  // (Products → Add product …) or it names a concrete control / route / tool
  // the reader can go to. A sales sentence ("beautiful, effortless payments")
  // names none of them, which is what this separates.
  const CONCRETE =
    /→|\bPublish\b|Form settings|\bForms\b|\bSettings\b|\bSales\b|\bSubscriptions\b|\bbuilder\b|\bpalette\b|\bProducts\b|Add product|Canva|upload_media|set_form_theme|set_workspace_landing_page|\/mcp|https:\/\/|\?aff=CODE|commission|Donation template|Restaurant menu|QR code/i;
  const MARKETING = /\bamazing|beautiful|effortless|powerful|best-in-class|seamless|world-class\b/i;
  const vague: string[] = [];
  for (const s of SHOWCASES) {
    for (let i = 1; i <= s.steps; i++) {
      const key = `${s.prefix}step${i}`;
      const e = entry(key);
      if (!e) continue;
      const en = e.en ?? "";
      if (!CONCRETE.test(en) || MARKETING.test(en)) vague.push(`${key}: ${en}`);
    }
  }
  assert.deepEqual(
    vague,
    [],
    "these setup steps name no UI path, control or route (or read as sales copy), so a reader " +
      "cannot follow them:\n  " + vague.join("\n  "),
  );
});

test("the donation copy describes the shipped pay-what-you-want product, and the schema agrees", () => {
  // Read the rule from the schema instead of trusting the copy — and read the
  // migration that actually widened it, so this arm fails the day the type set
  // moves again (a green here is a claim about the live constraint, and 000001
  // alone cannot make it).
  const types = productTypes(customAmountMigration);
  assert.deepEqual(
    types,
    ["custom_amount", "one_time", "subscription"],
    "products.type changed. The donation copy below describes `custom_amount` (the buyer " +
      "types the amount, RM1.00 floor) — re-read land.donation* against the new set: " +
      `${types.length ? types.join(", ") : "(no products_type_check parsed)"}`,
  );

  // The showcase for "collect donations" must not omit the donation-specific
  // capability: the body, the lead bullet and the first step all have to say
  // the amount is typed by the donor, and the floor has to be stated (RM1.00 is
  // enforced by the server: payments.MinCustomAmountSen).
  const TYPED = /pay[- ]what[- ]you[- ]want|types? the amount|type the amount/i;
  for (const key of ["land.donationbody", "land.donation1", "land.donationstep1"]) {
    assert.match(
      entry(key).en,
      TYPED,
      `${key} does not describe the shipped custom_amount product. The landing page for ` +
        "collecting donations has to lead with the product the buyer types an amount into " +
        "(migration 000085), not only with fixed denominations",
    );
  }
  assert.match(
    entry("land.donation1").en,
    /RM1(?:\.00)?/,
    "land.donation1 does not state the RM1.00 minimum — an amount below it is refused, so " +
      "copy that leaves the floor out promises something the checkout rejects",
  );

  // Step 3 is where the typed amount meets the real form: the amount field the
  // donor fills in, with the fixed-denomination route kept as the alternative.
  assert.match(
    entry("land.donationstep3").en,
    /amount field/i,
    "land.donationstep3 does not mention the amount field the donor fills in — that field " +
      "is what a custom_amount product block renders",
  );
  assert.match(
    entry("land.donationstep3").en,
    /one-time product per amount/i,
    "land.donationstep3 no longer offers the fixed-denomination alternative (one one-time " +
      "product per amount)",
  );

  // Negation is kept only for what genuinely does not exist. A donor wall and a
  // progress bar have no code behind them, and the amount is not uncapped: the
  // floor is 100 sen. Promising any of those is a lie a reader could act on.
  const NON_EXISTENT = /\bdonor wall\b|\bprogress bar\b|\bany amount you like\b|\bno minimum\b/i;
  const liars = ALL_KEYS.filter((k) => NON_EXISTENT.test(entry(k)?.en ?? ""));
  assert.deepEqual(
    liars,
    [],
    "these keys promise something the product does not do — there is no donor wall, no " +
      `progress bar, and the amount is floored at RM1.00:\n  ${liars.join(", ")}`,
  );

  // And the template the second step starts from is really in the library.
  assert.ok(
    /'tpl-donation'/.test(templateMigration),
    "tpl-donation is not in the template library migration — donationstep2 tells the reader to pick it",
  );
});

test("no dead /docs/guides link: the page says the guide is coming instead", () => {
  // There is no app/docs/guides route (and no catch-all under app/docs), so a
  // /docs/guides/<slug> href is a 404. The card allows the honest fallback.
  // Only hrefs count: the render sites' own comments name the path to explain
  // why it is NOT linked, and prose must not be able to fail this.
  const guideHrefs = landing.match(/href=\{?["'`][^"'`]*\/docs\/guides/g) ?? [];
  assert.deepEqual(
    guideHrefs,
    [],
    "app/landing/page.tsx links /docs/guides/… — that route does not exist, so the link is a 404. " +
      "Keep the land.guidesoon note until a guide page ships.",
  );
  // The links that ARE on the page must point at real routes (the dashboard
  // routes the CTAs use, plus the docs pages the footer already had).
  for (const href of ["/builder", "/mcp", "/login", "/docs", "/docs/api"]) {
    assert.ok(landing.includes(`href="${href}"`), `the landing page lost its ${href} link`);
  }
});
