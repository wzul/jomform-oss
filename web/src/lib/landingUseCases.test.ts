// The landing page's use-case showcase: seven cards, and what each card may — and may not —
// carry (cards t_41666fb1 → t_b4e5881d).
//
// WHAT WAN ASKED FOR, IN TWO MOVES — and the second reverses the first
//   7 Nov (card t_41666fb1): "letak use case jugak untuk collect donation. setiap use case
//   tu bagi jugak instruction macam mana nak setup" — a donation showcase, and setup steps
//   on EVERY showcase. That card added, under each card's actions, a nested box holding a
//   shared heading, a four/five-item ordered list and a guide link. This file then pinned
//   those lists: the heading exactly seven times, seven `<ol className="mt-3 list-decimal">`
//   lists, each showcase's steps in order on the page.
//
//   Then, on a phone screenshot of the RSVP card (card t_b4e5881d): "tak sesuai ada text
//   kat sini. benda yang terlalu technical tak sesuai ada kat landing page." The step lists
//   are GONE from the landing page — the nested box, the ordered list and the shared heading
//   with them (33 dictionary keys deleted) — and each card keeps ONE plain line linking its
//   screenshot guide.
//
// THE RATIONALE DID NOT DISAPPEAR WITH THE LISTS. The old guard existed to prove that every
// showcase is a real use case of the shipped product with a route to the instructions. That
// job is unchanged; the route moved. So the assertions below were REWRITTEN, not dropped:
// where the guard used to count seven ordered lists, it now requires zero, and it requires
// each card's guide link to resolve to a guide that STILL HAS NUMBERED STEPS. Removing the
// how-to from the landing page can therefore never mean losing it from the product.
//
// THE RULE THIS FILE ENFORCES
//   1. Non-vacuity, per showcase. Each of the seven cards must render its own title, body,
//      four tick bullets, primary CTA — once each — and its guide line INSIDE its own slice
//      of app/landing/page.tsx. A card that silently loses its copy, or whose guide link
//      drifts into a neighbour, is what these arms catch.
//   2. ZERO setup lists on the landing. No `<ol className="mt-3 list-decimal">`, no heading
//      render, no nested `bg-[var(--jf-bg)]` box, and none of the 33 deleted dictionary keys
//      may come back. The instruction is encoded, so a later "improve the copy" commit
//      cannot quietly restore it.
//   3. The route to the steps still exists. Every showcase links /docs/guides/<slug>, the
//      page for it must exist and render that slug, and the guide's SOURCE
//      (docs/guides/<slug>/README.md) must still carry numbered steps 1..N. The landing
//      says what the product does; the guide says how.
//   4. The copy describes what SHIPS. `products.type` includes `custom_amount` (pay-what-
//      you-want, migration 000085_custom_amount_products), read from the migration rather
//      than from memory or from 000001's append-only history — 000001 still says
//      one_time|subscription, which is why an arm that read only 000001 passed while saying
//      nothing about the live constraint. The donation showcase must LEAD with the typed
//      amount (RM1.00 floor) and keep fixed denominations as the ALTERNATIVE. The negation
//      arm survives only for capability that is genuinely absent: a donor wall, a progress
//      bar.
//   5. Language discipline (Wan, repeatedly — card t_541a31d5 fixed the same defect in the
//      dashboard): `en` is English, `ms` is Malay, and every key here exists in all five
//      locales. `t()` falls back to the raw key, so a missing locale shows "land.qr1" to a
//      real visitor and nothing else fails.
//
// WHAT THIS CANNOT SEE (stated honestly): it reads SOURCE, the dictionary and the guide
// markdown. It cannot prove the copy matches the shipped UI, cannot judge translation
// quality, and cannot measure layout — the rendered /landing page was driven in a browser at
// 390px and 1440px for this card and measured there (the seven setup boxes measured
// 454/454/415/392/314/353/630px at 390px before the change, and 0 after).
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { DICT, LANGS, type Lang } from "../components/i18nDict.ts";
import { GUIDES } from "./guides.gen.ts";

/** The dictionary as an indexable map; DICT's literal type needs the hop. */
const dict = DICT as unknown as Record<string, Record<Lang, string>>;
const entry = (key: string) => dict[key];

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const landing = read("../app/landing/page.tsx");
/**
 * The page's CODE, comments removed. The page explains this card's decision in prose, and
 * prose that names the deleted heading must not be able to satisfy (or trip) a guard about
 * what the page renders. The lookbehind keeps `https://` inside a string from being cut.
 */
const landingCode = landing
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/(?<![:"'`])\/\/[^\n]*/g, "");

// The LIVE constraint, not 000001: 000001 is append-only history, and the type set was
// widened by 000085 (pay-what-you-want). This is the file the copy has to answer to.
const customAmountMigration = read(
  "../../../api/internal/db/migrations/000085_custom_amount_products.up.sql",
);
const templateMigration = read("../../../api/internal/db/migrations/000027_template_library.up.sql");

/**
 * The effective `products.type` set: the LAST `products_type_check` the migration declares
 * (a later ALTER re-declares it, and the last one wins). Parsed rather than hard-coded so
 * the arm below breaks when the schema moves again instead of passing on a stale assumption.
 */
function productTypes(sql: string): string[] {
  const checks = [...sql.matchAll(/ADD CONSTRAINT products_type_check\s+CHECK \(type IN \(([^)]*)\)\)/g)];
  const last = checks.at(-1);
  if (!last) return [];
  return [...last[1].matchAll(/'([a-z_]+)'/g)].map((m) => m[1]).sort();
}

/** One entry per showcase: the copy it must render, and the guide it must link. */
type Showcase = {
  name: string;
  title: string;
  body: string;
  bullets: readonly string[];
  cta: string;
  /** The i18n prefix the deleted step keys used — kept so their absence is asserted. */
  prefix: string;
  guide: { slug: string; labelKey: string };
};

const SHOWCASES: readonly Showcase[] = [
  {
    name: "wedding / RSVP",
    title: "land.weddingtitle",
    body: "land.weddingbody",
    bullets: ["land.wedding1", "land.wedding2", "land.wedding3", "land.wedding4"],
    cta: "land.weddingcta",
    prefix: "land.wedding",
    guide: { slug: "wedding-rsvp", labelKey: "land.guidewedding" },
  },
  {
    name: "QR ordering",
    title: "land.qrtitle",
    body: "land.qrbody",
    bullets: ["land.qr1", "land.qr2", "land.qr3", "land.qr4"],
    cta: "land.qrcta",
    prefix: "land.qr",
    guide: { slug: "qr-table-ordering", labelKey: "land.guideqr" },
  },
  {
    name: "tuition / class fees",
    title: "land.tuitiontitle",
    body: "land.tuitionbody",
    bullets: ["land.tuition1", "land.tuition2", "land.tuition3", "land.tuition4"],
    cta: "land.tuitioncta",
    prefix: "land.tuition",
    guide: { slug: "tuition-class", labelKey: "land.guidetuition" },
  },
  {
    name: "Canva → agent → MCP",
    title: "land.canvatitle",
    body: "land.canvabody",
    bullets: ["land.canva1", "land.canva2", "land.canva3", "land.canva4"],
    cta: "land.canvacta",
    prefix: "land.canva",
    guide: { slug: "design-with-mcp", labelKey: "land.guidecanva" },
  },
  {
    name: "affiliate marketing",
    title: "land.afftitle",
    body: "land.affbody",
    bullets: ["land.aff1", "land.aff2", "land.aff3", "land.aff4"],
    cta: "land.affcta",
    prefix: "land.aff",
    guide: { slug: "affiliate-programme", labelKey: "land.guideaff" },
  },
  {
    name: "club membership",
    title: "land.membershiptitle",
    body: "land.membershipbody",
    bullets: ["land.membership1", "land.membership2", "land.membership3", "land.membership4"],
    cta: "land.membershipcta",
    prefix: "land.membership",
    guide: { slug: "club-membership", labelKey: "land.guidemembership" },
  },
  {
    name: "collect donations",
    title: "land.donationtitle",
    body: "land.donationbody",
    bullets: ["land.donation1", "land.donation2", "land.donation3", "land.donation4"],
    cta: "land.donationcta",
    prefix: "land.donation",
    guide: { slug: "donations", labelKey: "land.guidedonation" },
  },
];

/** How many times `needle` is written in the landing page's code. */
const count = (needle: string) => landingCode.split(needle).length - 1;

/** The landing page's code from a showcase's title to the close of its own card. */
function cardSlice(s: Showcase): string {
  const from = landingCode.indexOf(`t("${s.title}")`);
  const to = landingCode.indexOf("/>", from);
  assert.ok(
    from > 0 && to > from,
    `${s.name}: could not find the card on the landing page — t("${s.title}") then its ` +
      "closing /> is how every card is sliced",
  );
  return landingCode.slice(from, to);
}

/**
 * The numbered step headings of a guide, in order: `## 1. …`, `## 2. …` (the shape every
 * guide uses; `## Notes` has no number and is correctly ignored).
 *
 * Read from `guides.gen.ts` — the markdown the guide PAGE renders, materialised from
 * docs/guides/<slug>/README.md by web/scripts/guides.mjs — rather than from docs/ directly:
 * the docs tree is outside the web image's build context (root .dockerignore), so a test
 * that reads it passes on the host and cannot run in the image (src/lib/buildContext.test.ts).
 */
function numberedSteps(body: string): number[] {
  return [...body.matchAll(/^##\s+(\d+)\.\s+\S/gm)].map((m) => Number(m[1]));
}

test("the harness reads the real landing page and all seven showcases", () => {
  // Non-vacuity first: every arm below walks SHOWCASES, so a rename of the component (or a
  // page that lost the grid) must fail here instead of passing over nothing.
  assert.ok(
    landingCode.length > 5000,
    `expected to read app/landing/page.tsx, got ${landingCode.length} chars of code`,
  );
  assert.equal(
    count("<ShowcaseCard"),
    SHOWCASES.length,
    `the landing page must render ${SHOWCASES.length} showcase cards, found ${count("<ShowcaseCard")}`,
  );
  assert.equal(
    count("</ShowcaseCard>"),
    0,
    "every showcase card is self-closing now — a card with children is a setup list growing back",
  );
});

test("every showcase still renders its title, body, four bullets and CTA exactly once", () => {
  const missing: string[] = [];
  for (const s of SHOWCASES) {
    for (const key of [s.title, s.body, s.cta, ...s.bullets]) {
      const n = count(`t("${key}")`);
      if (n !== 1) missing.push(`${s.name}: ${key} rendered ${n}x`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    "a showcase lost or duplicated its copy — every card must keep one title, one body, " +
      `four tick bullets and one primary CTA:\n  ${missing.join("\n  ")}`,
  );
});

test("ZERO 'How to set it up' lists on the landing page", () => {
  // The instruction (card t_b4e5881d), encoded. Each arm is a different way the old block
  // could grow back: the ordered list, the heading, the nested box, the dictionary keys.
  assert.equal(
    count('<ol className="mt-3 list-decimal'),
    0,
    "an ordered setup list is back on the landing page — the steps belong in " +
      "docs/guides/<slug>, not under a showcase",
  );
  assert.equal(count('t("land.setup")'), 0, 'the "How to set it up" heading is rendered again');
  assert.equal(
    count("mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4"),
    0,
    "the nested setup box (a card inside a card) is back on a showcase",
  );
  assert.ok(
    !/how to set it up/i.test(landingCode),
    "the landing page says 'How to set it up' again — the list was deleted but its heading returned",
  );
  assert.ok(
    !/How to set it up|Cara menyediakannya/.test(JSON.stringify(dict)),
    "a dictionary value still carries the 'How to set it up' heading",
  );

  // Dead keys: the card that deleted the lists deleted their copy too. A key left behind is
  // copy nobody renders, and the next reader would take it for a live string.
  const orphans: string[] = [];
  const dead = [
    "land.setup",
    ...SHOWCASES.flatMap((s) => [1, 2, 3, 4, 5].map((i) => `${s.prefix}step${i}`)),
  ];
  for (const key of dead) {
    if (entry(key)) orphans.push(key);
  }
  assert.deepEqual(
    orphans,
    [],
    `these setup keys are still in i18nDict.ts but nothing renders them:\n  ${orphans.join("\n  ")}`,
  );
});

test("every showcase still links its own guide, inside its own card", () => {
  for (const s of SHOWCASES) {
    const card = cardSlice(s);
    assert.ok(
      card.includes(`href: "/docs/guides/${s.guide.slug}"`),
      `${s.name} no longer links /docs/guides/${s.guide.slug} — deleting the step list must ` +
        "not cut the reader's route to the steps",
    );
    assert.ok(
      card.includes(`t("${s.guide.labelKey}")`),
      `${s.name} links its guide with no ${s.guide.labelKey} label inside the card`,
    );
    assert.ok(
      !card.includes('t("land.guidesoon")'),
      `${s.name} links its guide AND says the guide is coming soon — one of the two is wrong`,
    );
  }

  // One link per showcase, and nothing else on the page links a guide.
  const hrefs = [...landingCode.matchAll(/href:\s*"(\/docs\/guides\/[a-z0-9-]+)"/g)].map((m) => m[1]);
  assert.deepEqual(
    hrefs,
    SHOWCASES.map((s) => `/docs/guides/${s.guide.slug}`),
    "the landing page's guide lines must be one per showcase, in showcase order",
  );
});

test("each linked guide resolves to a page that still has numbered steps", () => {
  // This is the arm that replaced the old "seven ordered lists" assertion. The reason the
  // lists were on the landing page was to prove each use case is reachable in the shipped
  // product; that reason is served by the guide now, so the guide is what gets checked.
  const problems: string[] = [];
  for (const s of SHOWCASES) {
    const { slug } = s.guide;
    const page = `../app/docs/guides/${slug}/page.tsx`;
    if (!existsSync(pathOf(page))) {
      problems.push(`${s.name}: ${page} does not exist — the guide line would 404`);
      continue;
    }
    if (!read(page).includes(`guideBySlug("${slug}")`)) {
      problems.push(`${s.name}: ${page} does not render guideBySlug("${slug}")`);
    }
    const guide = GUIDES.find((g) => g.slug === slug);
    if (!guide) {
      problems.push(
        `${s.name}: src/lib/guides.gen.ts has no guide "${slug}" — the guide page would be ` +
          "empty (run `npm run guides:sync`)",
      );
      continue;
    }
    const steps = numberedSteps(guide.body);
    if (steps.length < 2) {
      problems.push(
        `${s.name}: the rendered guide "${slug}" has ${steps.length} numbered step(s) — the ` +
          "steps the landing page used to carry are gone from the product too",
      );
      continue;
    }
    const expected = Array.from({ length: steps.length }, (_, i) => i + 1);
    if (steps.join(",") !== expected.join(",")) {
      problems.push(
        `${s.name}: the rendered guide "${slug}" numbers its steps ${steps.join(",")} — ` +
          `expected 1..${steps.length}`,
      );
    }
  }
  assert.deepEqual(
    problems,
    [],
    "a showcase's guide link no longer leads to numbered steps. The landing page stopped " +
      `carrying the how-to, so the guide has to:\n  ${problems.join("\n  ")}`,
  );
});

test("every remaining showcase key exists in all five locales, and the English is English", () => {
  const keys = SHOWCASES.flatMap((s) => [s.title, s.body, s.cta, ...s.bullets, s.guide.labelKey]);
  keys.push("land.showcases", "land.showcasessub", "land.usecasetitle");

  const missing: string[] = [];
  for (const key of keys) {
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
    "these landing keys are missing a locale. t() falls back to the raw key, so the page " +
      `shows "land.qr1" instead of a sentence:\n  ${missing.join("\n  ")}`,
  );

  const MALAY = /\b(?:anda|untuk|dengan|pada|dari|dalam|tetapan|borang|produk|jualan|langganan|butang|pautan|halaman)\b/i;
  const NON_LATIN = /[\u0600-\u06FF\u0900-\u097F\u0E00-\u0E7F\u4E00-\u9FFF\u0B80-\u0BFF]/;
  const offenders: string[] = [];
  for (const key of keys) {
    const e = entry(key);
    if (!e) continue;
    if (MALAY.test(e.en)) offenders.push(`${key}.en contains a Malay word: ${e.en}`);
    if (NON_LATIN.test(e.en)) offenders.push(`${key}.en is not Latin script: ${e.en}`);
    if (NON_LATIN.test(e.ms)) offenders.push(`${key}.ms is not Latin script: ${e.ms}`);
  }
  assert.deepEqual(offenders, [], `language discipline:\n  ${offenders.join("\n  ")}`);
});

test("the donation copy still describes the shipped pay-what-you-want product, and the schema agrees", () => {
  const types = productTypes(customAmountMigration);
  assert.deepEqual(
    types,
    ["custom_amount", "one_time", "subscription"],
    "products.type changed. The donation copy describes `custom_amount` (the buyer types " +
      "the amount, RM1.00 floor) — re-read land.donation* against the new set: " +
      `${types.length ? types.join(", ") : "(no products_type_check parsed)"}`,
  );

  // The showcase for "collect donations" must not omit the donation-specific capability:
  // the body and the lead bullet have to say the amount is typed by the donor, and the
  // floor has to be stated (RM1.00 is enforced by the server: payments.MinCustomAmountSen).
  const TYPED = /pay[- ]what[- ]you[- ]want|types? the amount|type the amount/i;
  for (const key of ["land.donationbody", "land.donation1"]) {
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

  // The guide keeps the two claims the deleted landing steps used to make: the amount field
  // the donor fills in, and the fixed-denomination alternative (one product per amount). If
  // the landing page lost them, the guide has to carry them.
  const donationsGuide = GUIDES.find((g) => g.slug === "donations");
  assert.ok(
    donationsGuide,
    'src/lib/guides.gen.ts has no "donations" guide — the showcase links /docs/guides/donations',
  );
  assert.match(
    donationsGuide!.body,
    /amount field|types? the amount/i,
    "the donations guide no longer mentions the amount field the donor fills in — the " +
      "landing page stopped saying it in a step, so the guide is where it lives now",
  );
  assert.match(
    donationsGuide!.body,
    /one product per amount|one \*\*Product\*\* block per amount/i,
    "the donations guide no longer offers the fixed-denomination alternative (one product / " +
      "one Product block per amount) now that the landing step that said so is gone",
  );

  // Negation is kept only for what genuinely does not exist. A donor wall and a progress
  // bar have no code behind them, and the amount is not uncapped: the floor is 100 sen.
  const NON_EXISTENT = /\bdonor wall\b|\bprogress bar\b|\bany amount you like\b|\bno minimum\b/i;
  const liars = SHOWCASES.flatMap((s) => [s.body, ...s.bullets]).filter((k) =>
    NON_EXISTENT.test(entry(k)?.en ?? ""),
  );
  assert.deepEqual(
    liars,
    [],
    "these keys promise something the product does not do — there is no donor wall, no " +
      `progress bar, and the amount is floored at RM1.00:\n  ${liars.join(", ")}`,
  );

  // And the template the donation guide starts from is really in the library.
  assert.ok(
    /'tpl-donation'/.test(templateMigration),
    "tpl-donation is not in the template library migration — the donation guide tells the " +
      "reader to pick it from the gallery",
  );
});

test("the landing page keeps its real routes", () => {
  // The cards' CTAs are real routes the dashboard serves; the footer's docs links were there
  // before this card and are not part of the showcase rewrite.
  for (const href of ["/builder", "/mcp", "/login", "/docs", "/docs/api"]) {
    assert.ok(landingCode.includes(`href="${href}"`), `the landing page lost its ${href} link`);
  }
});
