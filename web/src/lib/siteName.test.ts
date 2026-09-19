// Guard for the home page's site-name signals — the name Google Search prints
// above the title of a result.
//
// THE SYMPTOM: Search labelled jomform.com results with the bare domain
// (`jomform.com`, lower case) instead of the brand. Every other signal on the
// home page already said the right thing — `<title>` starts with "JomForm" and
// `og:site_name` is "JomForm" — but the home page carried NO structured data at
// all, and `WebSite` structured data is the one signal Google documents as
// "most important, if you want to specify a preference". So the strongest input
// was simply absent and Search fell back to the domain.
//
// WHAT GOOGLE ACCEPTS, AND WHAT IT DOES NOT: the choice is fully automated and
// there is no setting anywhere that forces it. This guard covers the machine
// facts we control — the markup exists, is on the home page only, parses, and
// agrees with the rest of the page. It says nothing about whether Search will
// pick the name, and a passing run here is NOT evidence that the result line
// has changed: that needs a recrawl and processing time on Google's side.
//
// This repo has no JSX/DOM test runner (no vitest/jest — see src/lib/i18n.test.ts,
// src/lib/adminResponsive.test.ts), so the page-level rules are asserted against
// source, and the data rules against the module the page renders from.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. The home page still emits the markup (a guard that passes because the
//      fix was reverted is the failure mode this file exists to prevent).
//   2. It emits it from the server component, not from the client bundle a
//      crawler never runs — `app/page.tsx` is a server component by virtue of
//      having no "use client" directive, so that is asserted too.
//   3. It is on the home page ONLY. Google supports one site name per domain
//      and rejects subdirectory-level site names; a second copy on a subpage is
//      a documented way to make the signal worse, not better.
//   4. `name` / `alternateName` / `url` hold the documented values, `name` is
//      the brand and not the domain, `alternateName` is lower case (Google does
//      not detect a domain preference written `Example.com`), and the domain is
//      present — it is the fallback for exactly the case being fixed.
//   5. `name` matches the other home-page sources: `<title>` and `og:site_name`
//      in app/page.tsx, and the visible brand in the landing markup. Google asks
//      for the site name to be used consistently, so a rename that touches one
//      of these and not the others is the next way this breaks.
//   6. The serialised script is JSON and carries no raw `<`, so it cannot close
//      its own tag.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  SITE_ALTERNATE_NAME,
  SITE_NAME,
  SITE_URL,
  WEBSITE_JSON_LD,
  websiteJsonLdScript,
} from "./siteName.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const APP = new URL("../app/", import.meta.url);
const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, APP)), "utf8");

const home = read("page.tsx");
const landing = read("landing/page.tsx");
const layout = read("layout.tsx");

/** The `<script type="application/ld+json">` tags in a source file. */
function ldJsonTags(src: string): string[] {
  return src.match(/<script\b[^>]*type="application\/ld\+json"[^>]*>/g) ?? [];
}

/** Every .tsx/.ts file under app/ (pages and route handlers, recursively). */
function appSources(dir: string): string[] {
  const base = dir.replace(/\/+$/, "");
  return readdirSync(base).flatMap((name) => {
    const full = `${base}/${name}`;
    if (statSync(full).isDirectory()) return appSources(full);
    return /\.tsx?$/.test(name) ? [full] : [];
  });
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  // If a path moves, fail here rather than silently checking nothing — the
  // failure mode #507 was filed about.
  assert.ok(home.includes("export const metadata"), "app/page.tsx is not the home page source");
  assert.ok(
    DICT["land.footer"] && typeof DICT["land.footer"].en === "string",
    "the landing dictionary key this guard reads is gone",
  );
  assert.ok(appSources(fileURLToPath(APP)).length > 30, "app/ scan found implausibly few sources");
});

test("the home page emits WebSite structured data from the server", () => {
  const tags = ldJsonTags(home);
  assert.equal(tags.length, 1, `expected exactly one ld+json tag in app/page.tsx, found ${tags.length}`);
  assert.match(
    home,
    /dangerouslySetInnerHTML=\{\{ __html: websiteJsonLdScript\(\) \}\}/,
    "app/page.tsx must render the JSON-LD built by websiteJsonLdScript()",
  );
  // A "use client" directive would move the tag into the browser bundle, which
  // a search crawler does not execute — the tag would exist in the repo and be
  // invisible to Google.
  assert.doesNotMatch(home, /^\s*"use client";/m, "app/page.tsx must stay a server component");
});

test("the structured data is on the home page only", () => {
  const HOME = fileURLToPath(new URL("page.tsx", APP));
  const offenders = appSources(fileURLToPath(APP))
    .filter((p) => p !== HOME)
    .filter((p) => ldJsonTags(readFileSync(p, "utf8")).length > 0);
  assert.deepEqual(
    offenders,
    [],
    "site names are one per domain and are not supported per subdirectory: keep the " +
      "WebSite markup on the home page and remove it from these files",
  );
});

test("the home-page-only check can actually see a second copy", () => {
  // Positive control: the check above is a filter over a scan, so prove both
  // halves work — the detector recognises a planted tag, and the scan really
  // reaches the other pages (a scan that returned only the home page would make
  // the check above pass no matter what the rest of the app contained).
  const HOME = fileURLToPath(new URL("page.tsx", APP));
  const others = appSources(fileURLToPath(APP)).filter((p) => p !== HOME);
  assert.ok(others.length > 30, "the app/ scan returned implausibly few non-home sources");

  const planted = `const tag = '<script type="application/ld+json">{}</script>';`;
  assert.equal(ldJsonTags(planted).length, 1, "the detector misses a planted ld+json tag");
  assert.deepEqual(
    others.filter(() => ldJsonTags(planted).length > 0),
    others,
    "the off-home rule would not flag a tag planted on another page",
  );
});

test("name, alternateName and url hold the documented values", () => {
  assert.equal(WEBSITE_JSON_LD["@context"], "https://schema.org");
  assert.equal(WEBSITE_JSON_LD["@type"], "WebSite");
  assert.equal(WEBSITE_JSON_LD.name, SITE_NAME);
  assert.equal(WEBSITE_JSON_LD.url, SITE_URL);
  assert.equal(WEBSITE_JSON_LD.alternateName, SITE_ALTERNATE_NAME);

  assert.equal(SITE_NAME, "JomForm", "the preferred name is the brand, not the domain");
  assert.equal(SITE_URL, "https://jomform.com/", "url must be the canonical home page");
  // Google detects a domain-as-site-name preference only in all lower case.
  assert.equal(SITE_ALTERNATE_NAME, "jomform.com");
  assert.equal(SITE_ALTERNATE_NAME, SITE_ALTERNATE_NAME.toLowerCase());
});

test("the rendered script is JSON and cannot close its own tag", () => {
  const raw = websiteJsonLdScript();
  assert.doesNotMatch(raw, /</, "a raw '<' inside the JSON could terminate the script element");
  const parsed = JSON.parse(raw) as typeof WEBSITE_JSON_LD;
  assert.deepEqual(parsed, WEBSITE_JSON_LD);
});

test("the home page uses the same name in every source Google reads", () => {
  const title = home.match(/title: "([^"]+)"/);
  const siteName = home.match(/^\s*siteName: SITE_NAME,\s*$/m);
  assert.ok(title, "app/page.tsx no longer declares a title");
  assert.match(
    title[1],
    new RegExp(`^${SITE_NAME}\\b`),
    `the <title> must open with the site name "${SITE_NAME}"`,
  );
  // `siteName: SITE_NAME` is the fix's own wiring; the literal that used to sit
  // here is what a careless rename leaves behind.
  assert.ok(siteName, "og:site_name must be the shared SITE_NAME constant, not a literal");
  assert.doesNotMatch(home, /siteName: "/, "og:site_name has a hard-coded copy again");

  // The document title a crawler sees is the home page's, but layout.tsx also
  // declares one; if that drifts it can win on some routes.
  const layoutTitle = layout.match(/title: "([^"]+)"/);
  assert.ok(layoutTitle, "app/layout.tsx no longer declares a title");
  assert.ok(
    layoutTitle[1].startsWith(SITE_NAME),
    `app/layout.tsx <title> must open with "${SITE_NAME}": ${layoutTitle[1]}`,
  );
});

test("the visible home page brand is the same name in every locale", () => {
  // Google asks for the site name to be used consistently on the home page, and
  // the landing page is what a visitor (and a rendering crawler) actually sees.
  // The brand is split across a styled span, so it is matched per site: the
  // nav lockup and the footer string — not a file-wide "JomForm" search, which
  // a stray match anywhere in the file would satisfy.
  assert.match(
    landing,
    /Jom<span[^>]*>Form<\/span>/,
    "the landing nav lockup no longer renders the brand as Jom + Form",
  );
  for (const lang of LANGS) {
    assert.ok(
      DICT["land.footer"][lang].includes(SITE_NAME),
      `land.footer (${lang}) must name ${SITE_NAME}`,
    );
  }
});
