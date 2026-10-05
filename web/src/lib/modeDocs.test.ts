// Guard for card t_a58848bd: the form-mode control (builder → "Form mode") has
// no inline "?" doc link, so the one consequence a merchant cannot see from two
// buttons labelled Payment form / Response form — that the mode decides which
// default notification the form gets, and that switching it re-points an
// untouched default — was documented on /docs/email and reachable from nowhere.
//
// WHY IT MATTERS HERE MORE THAN USUALLY. The defect this card fixes is exactly
// that invisibility: QA measured three live response-mode forms whose only rule
// was a dead `sale.paid`, i.e. merchants who switched the mode and were never
// told their notification had stopped firing. AGENTS.md's docs-parity rule 4
// ("add an inline ? doc link in the UI next to the feature/field") is the
// mechanism for that class, and the mode control had none.
//
// WHAT THIS PINS (each is a way the gap comes back):
//   1. The builder really renders the "?" anchor, and its `href` is
//      /docs/email#notifications — the guide AND the anchor, so the link lands on
//      the paragraph that explains the mode change rather than the top of a page.
//   2. The anchor is labelled (title + aria-label on the SAME dictionary key),
//      the shape issue #924 settled for the four links PR 714 added: a bare "?"
//      circle with no accessible name is what #924 fixed, so a new link must not
//      reintroduce it.
//   3. It opens in a new tab (target="_blank" + rel) — a doc link that navigates
//      the builder away discards unsaved work, which is #924's other half.
//   4. The dictionary key exists in all five locales, so the link cannot ship
//      with a raw key rendering as its own name.
//   5. The destination is real: /docs/email declares `id: "notifications"`, so
//      the anchor scrolls rather than silently landing at the top.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * node --test cannot load JSX, so the page is read as TEXT. This proves the
//     link is DECLARED; that the browser paints it is not asserted here.
//   * It does not judge the translation QUALITY of the label (i18n.test.ts says
//     the same of itself).
//   * It cannot prove a merchant reads the guide — only that the surface offers
//     it, which is the part CI can decide.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const BUILDER = read("../app/builder/page.tsx");
const EMAIL_DOCS = read("../app/docs/email/page.tsx");

// The Form-mode block, bounded at its closing comment so an `href` elsewhere on
// the page cannot satisfy the assertions below (the builder is a large file with
// several doc links of its own).
function modeBlockSource(): string {
  const start = BUILDER.indexOf("{/* ── Form mode (issue #171) ── */}");
  assert.ok(start > 0, 'app/builder/page.tsx no longer marks the Form-mode block (`{/* ── Form mode (issue #171) ── */}`)');
  const next = BUILDER.indexOf("{/* Issue #177: everything else about the form", start);
  assert.ok(
    next > start,
    "the Form-mode block has no following marker to bound it — the slice would run past the control it documents",
  );
  return BUILDER.slice(start, next);
}

test('the builder\'s form-mode control carries an inline "?" doc link to the guide that explains the mode change', () => {
  const block = modeBlockSource();
  const anchors = [...block.matchAll(/<a\b[^>]*href="(\/docs[^"]*)"[^>]*>/g)];
  assert.equal(
    anchors.length,
    1,
    `the form-mode block renders ${anchors.length} docs links; expected exactly the one "?" anchor, so the ` +
      "assertions below stay attributable to the mode control rather than to a sibling",
  );
  assert.equal(
    anchors[0][1],
    "/docs/email#notifications",
    "the form-mode doc link must point at the section that explains what the mode does to the default " +
      "notification (/docs/email#notifications) — a link to the page top would satisfy 'it has a docs link' " +
      "while leaving the merchant to hunt for the answer",
  );
});

test("the form-mode doc link is labelled, opens in a new tab, and its label exists in every locale", () => {
  const block = modeBlockSource();
  const anchor = block.match(/<a\b[^>]*href="\/docs\/email#notifications"[^>]*>/);
  assert.ok(anchor, "the form-mode block lost its /docs/email#notifications anchor");
  const attrs = anchor[0];
  // Same dictionary key for title AND aria-label: one label, so the tooltip and
  // the accessible name cannot drift apart (the #924 shape).
  assert.ok(
    attrs.includes('title={t("bld.modeDocs")}') && attrs.includes('aria-label={t("bld.modeDocs")}'),
    'the form-mode doc link must carry title={t("bld.modeDocs")} AND aria-label={t("bld.modeDocs")} — an ' +
      "unlabelled \"?\" circle is an unnamed control (#924), and the sibling controls all carry both",
  );
  assert.ok(
    attrs.includes('target="_blank"') && /rel="/.test(attrs),
    "the form-mode doc link must open in a new tab (target=\"_blank\" + rel) — a doc link that navigates the " +
      "builder away discards the merchant's unsaved blocks (#924)",
  );

  const entry = DICT["bld.modeDocs"];
  assert.ok(entry, 'the dictionary has no "bld.modeDocs" entry, so the link renders its raw key');
  const missing = LANGS.filter((l) => !entry[l] || !String(entry[l]).trim());
  assert.deepEqual(
    missing,
    [],
    `"bld.modeDocs" is missing these locales: ${missing.join(", ")} — i18n.test.ts requires all five`,
  );
});

test("the destination really declares the anchor the form-mode link points at", () => {
  // The anchor has to exist as an id on the destination, or the link lands at the
  // top of the page and the merchant never sees the paragraph about the mode.
  assert.ok(
    /id:\s*"notifications"/.test(EMAIL_DOCS),
    'app/docs/email/page.tsx declares no section id "notifications", so /docs/email#notifications does not scroll',
  );
  // And the section must still carry the fact the link exists to deliver: the
  // mode change re-points the default notification. A link to a section whose
  // text no longer says why is the same silent gap one layer out.
  assert.ok(
    /Switching a form's mode re-points that default notification/.test(EMAIL_DOCS),
    "the /docs/email notifications section no longer explains that switching a form's mode re-points its " +
      "default notification — the surface would link a merchant to a section that does not answer the " +
      "question the link was added for",
  );
});
