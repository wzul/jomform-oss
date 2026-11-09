// The /docs/builder website-field sentence must state the rule the SERVER applies
// (card t_3d4e755e).
//
// THE DEFECT. PR #1359 made the website-field rule the browser's instead of an
// http(s)-prefix check, and the guide it shipped said:
//
//   "…accepts any absolute URL — https://…, ftp://…, mailto:… — and refuses
//    ONLY what is not a URL at all, such as yourname.com with no scheme."
//
// The words "refuses only" are false, and the post-deploy verify of v1.21.271
// measured the counter-example on prod (form qa-271-website-probe, commit 17accf8f):
//
//   value                          <input type=url>.validity.valid   POST /<slug>/order
//   http:/example.com (one slash)  true (Chrome 145)                 400 URL tidak sah
//   http://exa mple.com (space)    true (Chrome 145)                 400 URL tidak sah
//
// Both are MALFORMED rather than unusual: `url.Parse` refuses them, so
// ValidWebsiteURL refuses them, and the server-side comment
// (api/internal/formfields/formfields.go) says so deliberately — "a value that
// malformed is worth a corrective message rather than a stored broken link".
// The page said the opposite in the one place a merchant reads it.
//
// WHY THE TEST READS BOTH TREES. The invariant is not "the page says something
// about schemes" — it is that the page and the server agree. So this guard reads
// the rendered prose out of the docs page AND the server's own rule and its test:
//
//   (a) the sentence may not claim the server refuses *only* what is not a URL;
//   (b) it must name both divergence classes the server actually implements —
//       the script-bearing schemes, and the two malformed network shapes;
//   (c) those shapes must still be in the Go test's REFUSED (divergence) group
//       and not in its ACCEPTED group. If someone later normalises a one-slash
//       value so the server accepts it (the other accepted fix for this card),
//       this test goes red and says to update the page in the same change —
//       which is exactly the page/server agreement the sentence exists to state.
//
// Mutation evidence is in the PR: restoring the old sentence turns (a) and (b)
// red; moving `http:/example.com` into the Go test's accepted list turns (c) red.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The guide renders through the shared prose renderer: a section is an entry of
// `const SECTIONS: Section[]` keyed by `id: "…"`, not an `<h2 id="…">` in JSX.
// `sectionsOf` gives the anchor to this guard directly, so the section can be
// found by ITS id while the prose around it is read as text.
import { sectionsOf } from "./docsProseReader.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const PAGE = read("../app/docs/builder/page.tsx");
const GO_RULE = read("../../../api/internal/formfields/formfields.go");
const GO_TEST = read("../../../api/internal/formfields/website_url_t_27709300_test.go");

/** The prose of one docs section, as the reader gets it: the section's title and
 *  body strings, whitespace collapsed. Keyed on the section's `id` in SECTIONS so
 *  a reordering of the page cannot silently hand this test another paragraph, and
 *  so a renamed/removed anchor still fails loudly. */
function sectionText(src: string, id: string): string {
  const section = sectionsOf(src, "page.tsx").find((s) => s.id === id);
  assert.ok(
    section,
    `web/src/app/docs/builder/page.tsx has no SECTIONS entry with id="${id}" — the anchor this ` +
      `guard reads its prose from is gone`,
  );
  return [section.title, ...section.strings]
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}

const URL_SECTION = sectionText(PAGE, "url-fields");

/** One `[]string{…}` literal of the Go test, as source text, located by an
 *  unambiguous marker at or inside it (`accepted := []string{` opens the group of
 *  values the rule accepts; the divergence loop's opener is found from its first
 *  member, because the test carries two `for _, v := range []string{` loops). */
function goList(src: string, opener: string, member: string): string {
  const open = src.indexOf(opener);
  assert.ok(open >= 0, `api/internal/formfields/website_url_t_27709300_test.go no longer has ${opener}`);
  const close = src.indexOf("}", open);
  assert.ok(close > open, `the list opened by ${opener} is not closed`);
  const list = src.slice(open, close);
  assert.ok(list.includes(member), `the list opened by ${opener} does not hold ${member}`);
  return list;
}

const JS_AT = GO_TEST.indexOf('"javascript:alert(1)"');
const DIVERGENCE_OPENER = GO_TEST.lastIndexOf("for _, v := range []string{", JS_AT);
const REFUSED_DIVERGENCES = GO_TEST.slice(
  DIVERGENCE_OPENER,
  GO_TEST.indexOf("}", JS_AT),
);
const ACCEPTED = goList(GO_TEST, "accepted := []string{", '"ftp://example.com/x"');

test("the builder guide does not claim the server refuses only what is not a URL", () => {
  assert.ok(URL_SECTION.length > 0, "the #url-fields section renders no prose");
  for (const claim of [
    "only what is not a URL at all",
    "one deliberate exception",
    "There is one deliberate exception",
  ]) {
    assert.ok(
      !URL_SECTION.includes(claim),
      `the builder guide still says "${claim}". The server is stricter than the page in TWO ` +
        `classes, not one, and not only on non-URLs: it also refuses a malformed network URL the ` +
        `browser's lenient parser accepts (http:/example.com, http://exa mple.com) — both measured ` +
        `on prod v1.21.271 as 400 URL tidak sah while input[type=url] reported them valid ` +
        `(t_3d4e755e). Section: ${URL_SECTION}`,
    );
  }
});

test("the builder guide names both divergences the server implements", () => {
  // (1) the security rule: script-bearing schemes.
  for (const scheme of ["javascript:", "data:", "vbscript:"]) {
    assert.ok(
      URL_SECTION.includes(scheme),
      `the #url-fields prose does not name ${scheme} — ValidWebsiteURL refuses the script-bearing ` +
        `opaque schemes on purpose (the answer is stored and read back outside the form), and a ` +
        `merchant who finds out only at submit time was told the wrong thing`,
    );
  }
  // (2) the parser rule: the two measured malformed shapes, by the shape itself,
  // because "some values" is the kind of sentence that over-promises again.
  for (const [shape, why] of [
    ["http:/example.com", "a missing second slash"],
    ["http://exa mple.com", "a space in the host"],
  ]) {
    assert.ok(
      URL_SECTION.includes(shape),
      `the #url-fields prose does not name ${shape} (${why}). That value is VALID to the ` +
        `browser's <input type=url> and REFUSED by the server with 400 URL tidak sah: Website — ` +
        `measured on prod v1.21.271 (t_3d4e755e). If the shape is no longer refused, fix the ` +
        `server and this test's Go half first, then rewrite the sentence.`,
    );
  }
  assert.ok(
    /browser/i.test(URL_SECTION),
    "the #url-fields prose no longer contrasts the browser's verdict with the server's — that " +
      "contrast is the whole point of the section",
  );
});

test("the two malformed shapes are still refused by the server rule the page describes", () => {
  for (const v of ['"http:/example.com"', '"http://exa mple.com"']) {
    assert.ok(
      REFUSED_DIVERGENCES.includes(v),
      `${v} is no longer in the Go test's divergence group (the loop holding ` +
        `javascript:alert(1)). The builder guide now states that the server refuses these two ` +
        `malformed shapes although the browser accepts them — if the server started accepting ` +
        `one, the page must be corrected in the same change (t_3d4e755e).`,
    );
    assert.ok(
      !ACCEPTED.includes(v),
      `${v} has moved into the Go test's ACCEPTED list, so the server now normalises and stores ` +
        `the value the browser accepted. That is a legitimate fix — but the /docs/builder ` +
        `#url-fields sentence still tells the merchant the server refuses it. Update the page in ` +
        `this commit (t_3d4e755e).`,
    );
  }
  // The security rule is the other half of the sentence: it must stay refused.
  assert.ok(
    GO_RULE.includes('case "javascript", "data", "vbscript":'),
    "api/internal/formfields/formfields.go no longer refuses the script-bearing schemes this " +
      "guide tells the merchant are refused (t_3d4e755e)",
  );
});
