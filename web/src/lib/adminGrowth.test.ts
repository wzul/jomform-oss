// Regression guard for the "New in 24h" / "New in 7d" stat cards on /admin.
//
// THE BUG: both cards rendered five bare numbers with nothing naming them:
//
//     NEW IN 24H
//     0 / 0 / 0 / 0 / 0
//
// Reported by Wan from the mobile admin page. A reader could not tell whether
// the five values were workspaces/forms/products/sales/users or anything else.
// The fix (Option A) keeps one card per window and adds a legend line under the
// value, naming each number in the same order.
//
// This repo has no JSX component-test runner (no vitest/jest — see
// src/lib/i18n.test.ts and src/lib/priceUnit.issue551.test.ts for the same
// approach), so the render rules are covered on the pure functions the page
// calls, and the page's call site is asserted from source.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. The legend is non-empty and names five distinct things in the same order
//      as the value line — this is the actual fix, not a re-statement of it:
//      the order is asserted against the Go struct that produces the JSON.
//   2. The value line contains every number, in field order (a mis-ordered
//      value line would silently pair each number with the wrong label).
//   3. The legend is fully translated in all five locales — reusing the
//      existing admin.* keys means those keys' completeness is what makes the
//      legend readable, so the reused keys are checked here explicitly.
//   4. The page actually passes the legend to the cards: a fix that is never
//      wired into the render is dead code behind a green suite.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  GROWTH_LEGEND,
  growthLegend,
  growthValue,
  type GrowthCounts,
} from "./adminGrowth.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const pageSource = readFileSync(
  fileURLToPath(new URL("../app/admin/page.tsx", import.meta.url)),
  "utf8",
);

// The Go struct that produces the growth_24h / growth_7d JSON. Read from source
// rather than hardcoded here so a reorder on the API side is caught instead of
// being mirrored into the test (which would make the test agree with the bug).
const goGrowthCounts = readFileSync(
  fileURLToPath(new URL("../../../api/internal/dbstore/admin_overview.go", import.meta.url)),
  "utf8",
).match(/type GrowthCounts struct \{([\s\S]*?)\n\}/);

// A distinct value per field, so a mis-ordered or dropped field is visible.
const SAMPLE: GrowthCounts = { workspaces: 1, forms: 2, products: 3, sales: 4, users: 5 };

const fakeT = (key: string) => `«${key}»`;

test("the API's GrowthCounts struct was located (the order check cannot pass vacuously)", () => {
  assert.ok(goGrowthCounts, "could not find `type GrowthCounts struct` in api/internal/dbstore/admin_overview.go");
  const fields = [...goGrowthCounts[1].matchAll(/^\s*(\w+)\s+int64\s+`json:"(\w+)"/gm)];
  assert.equal(fields.length, 5, "expected five int64 fields on GrowthCounts");
});

test("the legend order matches the API's GrowthCounts field order", () => {
  const goFields = [...goGrowthCounts![1].matchAll(/^\s*(\w+)\s+int64\s+`json:"(\w+)"/gm)].map(
    (m) => m[2],
  );
  assert.deepEqual(
    GROWTH_LEGEND.map((e) => e.field),
    goFields,
    "the legend order drifted from api/internal/dbstore/admin_overview.go GrowthCounts — " +
      "each number would then sit above the wrong label",
  );
});

test("the legend names five distinct entities — no bare `0 / 0 / 0 / 0 / 0`", () => {
  assert.equal(GROWTH_LEGEND.length, 5);
  const keys = GROWTH_LEGEND.map((e) => e.key);
  assert.equal(new Set(keys).size, 5, "the legend repeats a label");
  // The reported symptom: the card's only text was numbers and separators.
  assert.doesNotMatch(growthLegend(fakeT), /^\s*[\d\s/]*$/, "the legend is still numbers only");
});

test("each number has its own label, in the same order as the value line", () => {
  const value = growthValue(SAMPLE);
  assert.equal(value, "1 / 2 / 3 / 4 / 5");

  const labels = GROWTH_LEGEND.map((e) => e.key);
  assert.deepEqual(
    growthLegend(fakeT).split(" / "),
    labels.map((k) => `«${k}»`),
  );

  // Same separator on both lines, so number N renders directly above label N
  // and the pair reads across on a narrow card.
  const valueParts = value.split(" / ");
  const legendParts = growthLegend(fakeT).split(" / ");
  assert.equal(valueParts.length, legendParts.length);
  assert.equal(valueParts[0], String(SAMPLE[GROWTH_LEGEND[0].field]));
});

test("the legend labels the numbers, it does not restate them", () => {
  // A legend that echoed the counts would look self-describing while still
  // naming nothing (the failure mode this card was about).
  const legend = growthLegend((k) => k);
  for (const n of Object.values(SAMPLE)) {
    assert.doesNotMatch(legend, new RegExp(`\\b${n}\\b`), `legend contains the bare number ${n}`);
  }
});

test("every reused legend key is translated in all five locales", () => {
  for (const { key } of GROWTH_LEGEND) {
    const entry = DICT[key] as Record<string, unknown> | undefined;
    assert.ok(entry, `${key} is missing from the dictionary`);
    for (const lang of LANGS) {
      assert.equal(
        typeof entry![lang],
        "string",
        `${key}.${lang} is missing — a ${lang} user would see English`,
      );
      assert.ok((entry![lang] as string).trim() !== "", `${key}.${lang} is empty`);
    }
  }
});

test("the five legend keys are distinct across all five locales", () => {
  // Two fields sharing a translated label would make the legend ambiguous,
  // which is the defect being fixed — e.g. if `Sales` and `Products` collided.
  for (const lang of LANGS) {
    const shown = GROWTH_LEGEND.map(
      ({ key }) => (DICT[key] as Record<string, string>)[lang],
    );
    assert.equal(new Set(shown).size, shown.length, `${lang} legend labels are not distinct`);
  }
});

test("the admin page wires the legend into both growth cards", () => {
  // The helper can be perfect and never rendered. Assert the call site.
  assert.match(pageSource, /import\s*\{[^}]*\bgrowthLegend\b[^}]*\}\s*from\s*["']\.\.\/\.\.\/lib\/adminGrowth["']/);
  assert.match(pageSource, /import\s*\{[^}]*\bgrowthValue\b[^}]*\}\s*from\s*["']\.\.\/\.\.\/lib\/adminGrowth["']/);

  const cards = [...pageSource.matchAll(/<StatCard\b[^>]*admin\.growth(?:24h|7d)[^>]*\/>/g)].map(
    (m) => m[0],
  );
  assert.equal(cards.length, 2, "expected exactly the 24h and 7d growth cards");
  for (const card of cards) {
    assert.match(card, /value=\{growthValue\(/, `growth card lost its value line: ${card}`);
    assert.match(card, /sub=\{growthLegend\(t\)\}/, `growth card has no translated legend: ${card}`);
  }

  // The old bare-slash helper must be gone.
  assert.doesNotMatch(pageSource, /const g = \(g: Growth\)/);
  assert.doesNotMatch(pageSource, /\$\{g\.workspaces\} \/ \$\{g\.forms\}/);
});

test("the legend line can wrap instead of clipping on a phone-width card", () => {
  // The report came from a phone: the sub line must not be forced onto one
  // line (which would clip inside the card's 2-column mobile grid).
  assert.match(
    pageSource,
    /sub \? \(?\s*<p className="[^"]*\bbreak-words\b[^"]*"/,
    "the StatCard sub line does not allow wrapping",
  );
  assert.doesNotMatch(pageSource, /sub[^\n]*\btruncate\b/, "the sub line must not truncate");
  assert.doesNotMatch(pageSource, /sub[^\n]*\bwhitespace-nowrap\b/, "the sub line must not be nowrap");
});
