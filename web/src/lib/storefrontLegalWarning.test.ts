// Card t_e8b4f7da — the merchant must be told when the public footer will NOT
// publish a legal URL they just saved.
//
// THE DEFECT THIS PINS (measured on prod, v1.22.43)
//   /settings → Storefront → "Privacy policy URL" = https://abc.jomform.com/privacy/
//   → Save gave PUT 200, the page said "Footer saved ✓", the value was stored
//   verbatim — and the public form rendered no footer link at all, because the
//   footer refuses to emit a link the public route 404s. The same silence applied
//   to the canonical path with no saved page text (0 rows in workspace_legal_pages
//   is the state of every workspace on prod).
//
// WHAT THIS GUARD PINS
//   1. The two reason lines are rendered for the two reasons and for nothing else:
//      a shown link renders nothing, an EMPTY field (shown=false, no reason)
//      renders nothing, and wrong-URL + missing-page renders BOTH lines — the
//      case where correcting the URL alone still shows nothing.
//   2. The wording carries the URL the merchant must type (`served_url`), so the
//      warning is actionable rather than an apology.
//   3. The rule is NOT re-implemented in the web tree: the settings page renders
//      whatever the API answered, and neither the page nor lib/legalLinkWarning
//      tests the host or path itself.
//   4. The API actually answers: the settings save reads `legal_link_status` from
//      the PUT response and the load reads it from the GET, and the Go handler
//      that produces it reuses the footer's own functions (asserted in
//      api/internal/httpapi/storefront_legal_warning_t_e8b4f7da_test.go — the
//      parity matrix there fails the moment a second copy of the path rule
//      disagrees with the footer).
//   5. Every line has a dictionary entry in all five locales, so the merchant
//      never sees a raw key where the reason should be.
//
// WHAT IT DOES NOT COVER
//   It does not click Save. The saved-then-warned flow is driven in a real
//   browser against the built app and reported in the card handoff; this test
//   pins the mapping and the wiring, which is what a regression would break.
//
// Run with: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { legalLinkWarningLines, type LegalLinkStatus } from "./legalLinkWarning.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/** The page's own render of one field's warning, without the React page around it. */
function renderWarning(st?: LegalLinkStatus): string {
  const lines = legalLinkWarningLines(st);
  if (!lines.length) return "";
  return renderToStaticMarkup(
    createElement(
      "div",
      null,
      ...lines.map((l) =>
        createElement("p", { key: l.key, className: "text-xs font-medium text-amber-800" },
          (DICT[l.key] as Record<string, string>).en.replace("{served}", l.vars.served)),
      ),
    ),
  );
}

test("a dropped hosted URL is reported with the URL the route actually serves", () => {
  const html = renderWarning({
    shown: false,
    reason: "hosted_path_not_served",
    page_type: "privacy",
    served_url: "https://abc.jomform.com/privacy",
    has_content: false,
  });
  assert.match(html, /Not shown on your public forms/, `no warning rendered:\n${html}`);
  assert.match(html, /https:\/\/abc\.jomform\.com\/privacy\b/, "the warning does not name the served URL");
  assert.match(html, /no saved content yet/, "a URL with no page text must also say the page is empty");
});

test("the canonical hosted URL with no saved page says to write the page", () => {
  const html = renderWarning({
    shown: false,
    reason: "hosted_page_has_no_content",
    page_type: "terms",
    served_url: "https://abc.jomform.com/terms",
    has_content: false,
  });
  assert.match(html, /no saved content yet/, `no page-content warning:\n${html}`);
  assert.doesNotMatch(html, /no trailing slash|no extra path/, "the URL was already canonical");
});

test("a bad path whose page DOES have content only asks for the right URL", () => {
  const lines = legalLinkWarningLines({
    shown: false,
    reason: "hosted_path_not_served",
    served_url: "https://abc.jomform.com/privacy",
    has_content: true,
  });
  assert.deepEqual(lines.map((l) => l.key), ["set.sfLegalLinkNotServed"],
    "with the page written, the only thing left to fix is the URL — telling the merchant to write a page " +
    "that already exists sends them to the wrong place");
});

test("shown and empty fields render NOTHING (no false alarm)", () => {
  // Published link (external, or hosted with content): nothing to say.
  assert.equal(renderWarning({ shown: true, served_url: "https://abc.jomform.com/privacy" }), "");
  assert.equal(renderWarning({ shown: true }), "");
  // Empty field: the API answers shown=false with NO reason. An absent link is
  // not a warning, and a false one would train the merchant to ignore the real one.
  assert.equal(renderWarning({ shown: false }), "");
  assert.equal(renderWarning({ shown: false, has_content: false }), "");
  assert.equal(renderWarning(undefined), "");
});

test("the page renders the API's verdict — it does not re-derive the host/path rule", () => {
  const page = read("../app/settings/page.tsx");
  // Save path: the verdict comes back on the PUT response.
  assert.match(
    page,
    /legal_link_status/,
    "saveStorefrontSettings no longer reads `legal_link_status` from the PUT response — the save confirms " +
    "again without saying the link was dropped",
  );
  assert.match(
    page,
    /setSfLegalStatus\(/,
    "the page never stores the API's verdict, so no field can warn",
  );
  // Load path: an already-stored dropped URL must be reported without a re-save.
  const loader = page.slice(page.indexOf("const loadStorefrontSettings"), page.indexOf("const loadStorefrontSettings") + 4000);
  assert.match(loader, /legal_link_status/, "the settings LOAD ignores legal_link_status — a merchant who saved " +
    "a dropped URL before this shipped would still never be told");

  // The rendering helper must delegate the decision to the module...
  const helper = page.slice(page.indexOf("function legalUrlWarning"), page.indexOf("function legalUrlWarning") + 1200);
  assert.match(helper, /legalLinkWarningLines\(/, "the field warning no longer comes from lib/legalLinkWarning");

  // ...and NEITHER the rendering helper NOR the module may test the host or path
  // itself. This is the card's explicit rule: one copy of the rule, in the API, or
  // the dashboard and the footer can disagree about the same URL. (The whole page
  // is not scanned — it legitimately names jomform.com for other features, e.g.
  // the custom-domain CNAME target.)
  const warnSources = [
    ["the legalUrlWarning helper in app/settings/page.tsx", helper],
    ["lib/legalLinkWarning.ts", read("./legalLinkWarning.ts")],
  ] as const;
  // Comments are stripped first: these files EXPLAIN the rule in prose and must
  // be able to quote /privacy/ as the counter-example without tripping the guard.
  const stripComments = (s: string) =>
    s.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
  for (const [name, src] of warnSources) {
    assert.doesNotMatch(
      stripComments(src),
      /\.jomform\.com\b|startsWith\(\s*["']\/privacy|endsWith\(\s*["']\/?privacy/,
      `${name} hand-rolls the hosted legal URL rule — the host/path/content decision lives in the API ` +
      `(public.go: rewriteHostedLegalURL + classifyHostedLegalURL + servableHostedLegalLink) and must not ` +
      `be copied into the web tree`,
    );
  }
});

test("the Legal tab says how a page here reaches the footer", () => {
  const legalTab = read("../components/LegalTab.tsx");
  assert.match(legalTab, /set\.legalFooterPointer/, "the Legal tab no longer explains that the footer link " +
    "comes from the legal URL in Storefront — the two halves of the feature are disconnected again");
  const entry = DICT["set.legalFooterPointer"] as Record<string, string>;
  assert.ok(entry, "set.legalFooterPointer is missing from the dictionary");
  assert.match(entry.en, /\{slug\}/, "the pointer must name the workspace's own host");
});

test("every warning line has all five locales", () => {
  for (const key of ["set.sfLegalLinkNotServed", "set.sfLegalLinkNoContent"] as const) {
    const entry = DICT[key] as Record<string, string> | undefined;
    assert.ok(entry, `${key} is missing from the dictionary — the merchant would see the raw key`);
    for (const lang of LANGS) {
      assert.ok(entry![lang] && entry![lang].trim(), `${key} has no ${lang} translation`);
    }
    for (const lang of LANGS) {
      assert.match(entry![lang], /\{served\}/, `${key}/${lang} does not interpolate the served URL`);
    }
  }
});
