// Guard for card t_254d8a81: the passkey feature had no AGENTS.md docs-parity
// rule-4 inline "?" doc link, and /docs' own comment claimed the login page
// deep-linked the section.
//
// THE GAP (measured at origin/main 376a50c / this branch's base):
//
//   * `grep -rn '#passkeys' web/src` returned NOTHING — the anchor existed but no
//     surface linked it, so the passkeys section was unreachable from the UI.
//   * `web/src/app/docs/page.tsx:310` (and, after the #821 rebase, the module
//     comment at :53) said "The login page's passkey control deep-links this
//     section" — and `grep -c 'docs' web/src/app/login/page.tsx` was 0. The
//     comment asserted a link that did not exist and that nothing checked.
//   * `TestPasskeyDocsDescribeTheRecoveryPath` (api/internal/httpapi/passkey_seam_test.go)
//     asserts the SECTION TEXT only ("login page" / "non-discoverable" / "register
//     a new passkey"), so it stayed green the whole time the link was absent.
//
// WHAT THIS PINS (each assertion is a way the gap comes back):
//   1. Profile → Passkeys really renders the "?" link, and its `href` is
//      `/docs#passkeys` — the same shape the 2FA row on the Security tab carries.
//   2. The link carries its own `prof.passkeysDocs` title, so the anchor is
//      labelled rather than an unlabelled circle.
//   3. `prof.passkeysDocs` exists in the dictionary in all five locales (the
//      i18n.test.ts direction, re-checked here so the link cannot ship with a
//      raw key).
//   4. The destination is real: /docs declares `slug: "passkeys"` AND renders
//      `id={f.slug}`, so the link scrolls rather than landing at the top.
//   5. The FALSE CLAIM cannot come back: the docs page must not say the login
//      page deep-links the section, and the login page still carries no /docs
//      link. (5) is what stops the comment from claiming (1) before it exists.
//
// HOW IT READS THE PAGES: node --test cannot load JSX, so the pages are read as
// text, the same constraint src/lib/docsIndex.test.ts works under. The passkey
// block is SLICED between the tab guard and the next tab, so an `href` on an
// unrelated row cannot satisfy the assertion.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot render React, so it proves the link is DECLARED in the passkey
//     tab; that the browser paints it is not asserted here (no JSX runner).
//   * Translation QUALITY is not judged (i18n.test.ts says the same of itself).
//   * (5) pins the sentence that actually shipped; a fresh comment could still
//     invent a different false claim about the link. (1) is the assertion that
//     makes the FEATURE true; (5) only removes the specific lie.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const PROFILE = read("../app/profile/page.tsx");
const DOCS = read("../app/docs/page.tsx");
const LOGIN = read("../app/login/page.tsx");

// The Passkeys tab's own block, bounded at the next tab so an `href` elsewhere
// on the page cannot satisfy the assertion below.
function passkeyTabSource(): string {
  const start = PROFILE.indexOf('{tab === "passkeys" && (');
  assert.ok(start > 0, "app/profile/page.tsx no longer renders a passkeys tab (`tab === \"passkeys\" && (`)");
  const next = PROFILE.indexOf("{/* Connected accounts tab", start);
  assert.ok(next > start, "the passkeys tab block has no following tab to bound it — the slice would run to EOF");
  return PROFILE.slice(start, next);
}

test('Profile → Passkeys renders an inline "?" doc link to /docs#passkeys', () => {
  const block = passkeyTabSource();

  // The anchor element, not merely the string: the href must be an attribute of
  // an <a> in this block, so a comment or an unrelated mention cannot satisfy it.
  const anchors = [...block.matchAll(/<a\b[^>]*href="(\/docs[^"]*)"[^>]*>/g)];
  assert.ok(
    anchors.length > 0,
    'the passkeys tab renders no <a href="/docs…"> at all — AGENTS.md docs-parity rule 4 ' +
      "requires an inline \"?\" doc link next to the feature (the 2FA row one tab over has one)",
  );
  assert.equal(
    anchors.length,
    1,
    `the passkeys tab renders ${anchors.length} docs links; expected exactly the one "?" anchor so the assertion stays attributable`,
  );
  assert.equal(
    anchors[0][1],
    "/docs#passkeys",
    `the passkeys "?" link points at ${anchors[0][1]} — it must be /docs#passkeys, the anchor this ` +
      "feature's docs section carries",
  );

  // The anchor is LABELLED with the passkey-specific title (not an empty circle).
  const anchorEnd = block.indexOf("</a>", anchors[0].index);
  const anchor = block.slice(anchors[0].index, anchorEnd);
  assert.ok(
    anchor.includes('title={t("prof.passkeysDocs")}'),
    'the passkeys "?" anchor has no title={t("prof.passkeysDocs")} — the link must be labelled, ' +
      "the same way the 2FA link carries prof.twoFaDocs",
  );
});

test("prof.passkeysDocs exists in the dictionary in all five locales", () => {
  const entry = (DICT as Record<string, Record<string, unknown>>)["prof.passkeysDocs"];
  assert.ok(entry, "the dictionary has no prof.passkeysDocs — the ? link's title would render the raw key");
  assert.deepEqual([...LANGS], ["en", "ms", "zh", "ta", "ar"]);
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `prof.passkeysDocs.${lang} is missing — that locale would render the raw key instead of the title`,
    );
  }
});

test("the link's destination is real (the section exists and its anchor is rendered)", () => {
  assert.ok(
    DOCS.includes('slug: "passkeys"'),
    'app/docs/page.tsx declares no `slug: "passkeys"` section — /docs#passkeys would land at the top of the page',
  );
  assert.ok(
    DOCS.includes("id={f.slug}"),
    "app/docs/page.tsx does not render the slug as an id, so /docs#passkeys would not scroll",
  );
});

test("the docs page does not repeat the false deep-link claim about the login page", () => {
  // The exact clause that shipped at :310 (and moved to the module comment in #821).
  //
  // Matched against a NORMALISED view — comment markers dropped, whitespace
  // collapsed — because the sentence originally shipped WRAPPED across two `//`
  // lines, which a raw-source regex silently misses (the bite harness's wrapped
  // case is what caught that; both guards normalise for the same reason).
  const claim = DOCS.replace(/\/\//g, " ").replace(/\s+/g, " ");
  assert.doesNotMatch(
    claim,
    /login page'?s passkey control deep-links/i,
    "app/docs/page.tsx again claims the login page's passkey control deep-links #passkeys — that is " +
      "the false claim this card removed (nothing on the login page links /docs)",
  );
  // And it stays false: the login page must keep carrying no /docs link, so the
  // honest boundary the new comment states remains true.
  assert.ok(
    !LOGIN.includes("/docs"),
    "app/login/page.tsx now links /docs — the docs comment's honest boundary (the passkey " +
      "sign-in control carries no doc link) has changed, so both must be revisited",
  );
});
