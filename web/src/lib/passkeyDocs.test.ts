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
//   4. The destination is real: /docs' FAQ array carries an entry whose `slug`
//      PROPERTY is "passkeys", that entry is the shape the page renders an
//      anchor for, AND the FAQ map renders that slug as an id — so the link
//      scrolls rather than landing at the top. All three are read through the
//      TypeScript parser (see "WHERE THE SECTION IS", below), never by searching
//      the file for the text: a search is satisfied by a comment (t_ec01dcc2).
//   5. The FALSE CLAIM cannot come back: the docs page must not say the login
//      page deep-links the section, and the login page still carries no /docs
//      link. (5) is what stops the comment from claiming (1) before it exists.
//
// HOW IT READS THE PAGES: node --test cannot load JSX, so the pages are read as
// SOURCE. Two instruments, each used where it answers the question: the Passkeys
// tab is SLICED as text between the tab guard and the next tab, so an `href` on
// an unrelated row cannot satisfy the assertion; the /docs destination is read
// through the TypeScript parser (`typescript` is a devDependency and
// src/lib/jsxSource.ts already parses these pages with it), so the section is
// located as what it is — an object in the FAQ array — rather than by a text
// shape. The comment forms that can satisfy a text search are exactly why.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot render React, so it proves the link is DECLARED in the passkey
//     tab; that the browser paints it is not asserted here (no JSX runner).
//   * Translation QUALITY is not judged (i18n.test.ts says the same of itself).
//   * (5) pins the sentence that actually shipped; a fresh comment could still
//     invent a different false claim about the link. (1) is the assertion that
//     makes the FEATURE true; (5) only removes the specific lie.
//   * (4) reads the page's SOURCE through a parser, not the built HTML: a render
//     branch that never runs still counts as "rendered", the same limit
//     src/lib/docsIndex.test.ts states for itself. What is closed is the case
//     that shipped — prose carrying the literal — because prose is not an object
//     literal, not a property, and not a JSX attribute.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

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

// ── WHERE THE SECTION IS (t_ec01dcc2) ────────────────────────────────────────
//
// THE DEFECT THIS CLOSES. Leg (4)'s "the section exists" half used to ask
//
//     DOCS.includes('slug: "passkeys"')
//
// — a substring search over the RAW page source, so the FIRST occurrence of the
// literal ANYWHERE in the file decided it, a comment included. Measured on a
// fixture copy of the shipped page, with this file's own guard run against it
// (the harness is the test at the bottom of this file, which drives both
// directions on every gate): insert one comment line naming the literal above
// `const FAQ: FaqEntry[] = [` and DELETE the entry's own `slug: "passkeys",`
// line. The page then declares no passkey section at all — its only remaining
// occurrence of the literal is that comment — and this file still reported
// 4 pass / 0 fail, while /docs#passkeys landed at the top of the page.
//
// The Go twin (TestPasskeyDocsDescribeTheRecoveryPath in
// api/internal/httpapi/passkey_seam_test.go) asks the same question of the same
// page and was anchored the same way by t_1310d487 — the entry is located by its
// `slug` PROPERTY inside the array the page renders, never by the first
// occurrence of the literal. This is that question with this tree's own tool:
// `typescript` is a devDependency and src/lib/jsxSource.ts already reads these
// pages with it, so the entry is located as WHAT IT IS — an object literal in
// the FAQ array's initializer with a `slug` property — rather than by a text
// shape. A comment is then not merely "not matched by the pattern": it is not a
// property at all, so no spelling or placement of the literal in prose can
// answer this question.
//
// The same applies to the second half: the anchor is read as a JSX ATTRIBUTE on
// the element the FAQ map renders, not as the text `id={f.slug}`. That text also
// appears one branch up, in the GROUPS map — which renders the feature entries
// and not the FAQ's — so a page-wide search was satisfied while the passkey
// entry's own branch rendered no id at all.
//
// WHAT THIS REFUSES BY NAME rather than asserting against nothing: no FAQ array,
// no entry with that `slug` value, an entry that is the question/answer shape
// (the FAQ map renders those through a branch with no `id`), an entry whose
// `title`/`body` are not single string literals, and a FAQ map that renders no
// slug id. Each says which shape it now needs, so a legitimate re-shape is a
// named failure and not a silent pass.

const FAQ_MARKER = "const FAQ: FaqEntry[] = [";

function parsePage(src: string): ts.SourceFile {
  return ts.createSourceFile("page.tsx", src, ts.ScriptTarget.Latest, /* setParentNodes */ true, ts.ScriptKind.TSX);
}

/** The name of a property key, however it is spelled. */
function keyName(name: ts.PropertyName): string | undefined {
  if (ts.isIdentifier(name) || ts.isStringLiteral(name)) return name.text;
  return undefined;
}

/** A plain `name: value` property of an object literal. */
function propNamed(entry: ts.ObjectLiteralExpression, name: string): ts.PropertyAssignment | undefined {
  return entry.properties.find(
    (p): p is ts.PropertyAssignment => ts.isPropertyAssignment(p) && keyName(p.name) === name,
  );
}

/** The `string` a property holds, when it holds one literally. */
function stringValue(prop: ts.PropertyAssignment | undefined): string | undefined {
  if (!prop) return undefined;
  if (ts.isStringLiteral(prop.initializer) || ts.isNoSubstitutionTemplateLiteral(prop.initializer)) {
    return prop.initializer.text;
  }
  return undefined;
}

type Located = { ok: true; entry: ts.ObjectLiteralExpression } | { ok: false; reason: string };

/** faqEntryForSlug finds the entry whose `slug` PROPERTY is `slug`, in the FAQ
 * array the page renders — the same question the Go twin asks of this file
 * (faqEntryForSlug, api/internal/httpapi/passkey_seam_test.go). */
function faqEntryForSlug(src: string, slug: string): Located {
  let faq: ts.ArrayLiteralExpression | undefined;
  for (const stmt of parsePage(src).statements) {
    if (!ts.isVariableStatement(stmt)) continue;
    for (const decl of stmt.declarationList.declarations) {
      if (
        ts.isIdentifier(decl.name) &&
        decl.name.text === "FAQ" &&
        decl.initializer !== undefined &&
        ts.isArrayLiteralExpression(decl.initializer)
      ) {
        faq = decl.initializer;
      }
    }
  }
  if (!faq) {
    return {
      ok: false,
      reason:
        `the page declares no \`${FAQ_MARKER}\` at module scope — a slug-anchored section renders only ` +
        "out of that array, so prose naming the literal cannot be one",
    };
  }

  const declared: string[] = [];
  for (const element of faq.elements) {
    if (!ts.isObjectLiteralExpression(element)) continue;
    const prop = propNamed(element, "slug");
    if (!prop) continue;
    const value = stringValue(prop);
    if (value === slug) return { ok: true, entry: element };
    // A non-literal value is reported as what it is, so a re-shaped array fails
    // by name instead of reading as "no such section".
    declared.push(value ?? prop.initializer.getText());
  }
  return {
    ok: false,
    reason:
      `no entry in the FAQ array declares \`slug: "${slug}"\` (it declares ` +
      `${declared.length ? declared.map((d) => `\`${d}\``).join(", ") : "none"}) — the literal in a ` +
      "comment, or in any other array, is not this section",
  };
}

/** Does the page SHOW the located entry? The FAQ map's `id={f.slug}` branch is
 * the one that is not the question/answer branch, and it renders the entry's
 * `title` and `body` — so an entry that also declares a `q`, or that carries no
 * text, is a section no reader (and no anchor) reaches. */
function sectionShape(entry: ts.ObjectLiteralExpression): { ok: true } | { ok: false; reason: string } {
  if (propNamed(entry, "q")) {
    return {
      ok: false,
      reason:
        "the entry also declares a `q`, so the FAQ map renders it through the question/answer branch — " +
        "that branch renders no `id`, so #passkeys would land at the top of the page",
    };
  }
  const title = stringValue(propNamed(entry, "title"));
  const body = stringValue(propNamed(entry, "body"));
  if (title === undefined || body === undefined) {
    return {
      ok: false,
      reason:
        "the entry's `title` and `body` are not single string literals — this guard asserts the section a " +
        "reader sees. Teach it the new shape rather than dropping the assertion",
    };
  }
  return { ok: true };
}

type RenderSite = { ok: true; param: string } | { ok: false; reason: string };

/** The element that gives an FAQ entry its anchor: an `id={<param>.slug}`
 * attribute inside `FAQ.map(<param> => …)`. Read as an attribute on the map's
 * own callback — the GROUPS map one branch up renders its own `id={f.slug}`. */
function faqSlugRenderSite(src: string): RenderSite {
  const maps: { param: string; callback: ts.ArrowFunction }[] = [];
  const findMaps = (node: ts.Node): void => {
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === "map" &&
      ts.isIdentifier(node.expression.expression) &&
      node.expression.expression.text === "FAQ"
    ) {
      const callback = node.arguments[0];
      const param = callback && ts.isArrowFunction(callback) ? callback.parameters[0] : undefined;
      if (callback && ts.isArrowFunction(callback) && param && ts.isIdentifier(param.name)) {
        maps.push({ param: param.name.text, callback });
      }
    }
    ts.forEachChild(node, findMaps);
  };
  findMaps(parsePage(src));
  if (maps.length === 0) {
    return {
      ok: false,
      reason: "the page no longer renders the FAQ array through `FAQ.map(` — the passkey section would not be on the page at all",
    };
  }

  for (const { param, callback } of maps) {
    let found = false;
    const findId = (node: ts.Node): void => {
      if (found) return;
      if (
        ts.isJsxAttribute(node) &&
        ts.isIdentifier(node.name) &&
        node.name.text === "id" &&
        node.initializer !== undefined &&
        ts.isJsxExpression(node.initializer)
      ) {
        const expr = node.initializer.expression;
        if (
          expr !== undefined &&
          ts.isPropertyAccessExpression(expr) &&
          expr.name.text === "slug" &&
          ts.isIdentifier(expr.expression) &&
          expr.expression.text === param
        ) {
          found = true;
        }
      }
      ts.forEachChild(node, findId);
    };
    findId(callback);
    if (found) return { ok: true, param };
  }
  return {
    ok: false,
    reason:
      `the FAQ map renders no \`id={…slug}\` attribute — the entry's own branch gives the section no anchor. ` +
      "A mention of that text in a comment, or the GROUPS map's own `id={f.slug}` one branch up (it renders " +
      "the feature entries, not the FAQ's), cannot give the passkey section its anchor",
  };
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
  // (4a) The section EXISTS — located by its own `slug` PROPERTY inside the array
  // the page renders. A comment naming the literal is not a property, so it can
  // no longer stand in for the section (t_ec01dcc2; the measurement is in the
  // note above the locator).
  const located = faqEntryForSlug(DOCS, "passkeys");
  if (!located.ok) {
    assert.fail(
      `app/docs/page.tsx has no passkey section — /docs#passkeys would land at the top of the page: ${located.reason}`,
    );
  }
  // (4b) …and it is a section the page SHOWS: the FAQ map renders an entry
  // through the question/answer branch when it declares a `q`, and that branch
  // renders no `id`.
  const shape = sectionShape(located.entry);
  if (!shape.ok) {
    assert.fail(`app/docs/page.tsx's \`slug: "passkeys"\` entry is not a section the page shows: ${shape.reason}`);
  }
  // (4c) The ANCHOR is rendered for that entry: an `id={…slug}` attribute inside
  // `FAQ.map(…)`. Read as an attribute, not as the text `id={f.slug}` — that text
  // also sits one branch up, in the GROUPS map, which renders the feature entries.
  const site = faqSlugRenderSite(DOCS);
  if (!site.ok) {
    assert.fail(`/docs#passkeys would not scroll: ${site.reason}`);
  }
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

// ── both directions of the locator, driven on every gate (t_ec01dcc2) ─────────
//
// Leg (4) above reads a TypeScript page that is full of comments and answers "is
// the passkey section real?" out of it, so "can a comment decide this answer?" is
// a question a test has to settle, not a promise in prose. It has been settled
// wrongly: the shipped leg was `DOCS.includes('slug: "passkeys"')`, so a single
// comment line naming the literal — with the entry's own `slug` property deleted
// — kept this whole file GREEN 4/4 while /docs rendered no passkey section at
// all. That shape is case 5 below.
//
// Every fixture is fed to BOTH the shipped one-line checks and the anchored
// locators, and a case is only valid when the SHIPPED check really answers what
// the case claims — asserted below, not assumed — because a fixture that does not
// reproduce its own premise would make the anchored answer prove nothing. The
// shipped checks are driven as the `includes` calls they are, not as a paraphrase
// of them, so this cannot drift from what was replaced.
test("the section locator is anchored to the property and the render site, not to the literal", () => {
  const PAGE = [
    'const GROUPS = [{ id: "account", entries: [] }];',
    "{{ABOVE}}",
    "const FAQ: FaqEntry[] = [",
    "  {",
    '    q: "How do I connect an AI agent?",',
    '    a: "Point any MCP client at https://mcp.jomform.com/mcp.",',
    "  },",
    "{{BETWEEN}}",
    "  {",
    "{{INSIDE}}",
    '    title: "Passkeys (sign in without a password)",',
    "{{SLUG}}",
    '    body: "An older passkey may be non-discoverable: delete that row and register a new passkey.",',
    "  },",
    "];",
    "",
    "export default function DocsIndexPage() {",
    "  return (",
    '    <div className="max-w-3xl">',
    "      {GROUPS.map((g) =>",
    "        g.entries.map((f) => (",
    "          <div key={f.title}{{GROUPID}}>{f.body}</div>",
    "        )),",
    "      )}",
    "      {FAQ.map(({{PARAM}}) =>",
    "        {{PARAM}}.q !== undefined ? (",
    "          <section key={{{PARAM}}.q}>{{{PARAM}}.a}</section>",
    "        ) : (",
    "          <section key={{{PARAM}}.title}{{ANCHOR}}>",
    "            <h3>{{{PARAM}}.title}</h3>",
    "            <p>{{{PARAM}}.body}</p>",
    "          </section>",
    "        ),",
    "      )}",
    "    </div>",
    "  );",
    "}",
  ].join("\n");

  // The shipped leg's own two questions, as they were written.
  const shippedSectionExists = (src: string) => src.includes('slug: "passkeys"');
  const shippedAnchorRendered = (src: string) => src.includes("id={f.slug}");

  // The prose shapes this card is about: a comment that merely NAMES the literal.
  const LINE_COMMENT = '// The passkey section (slug: "passkeys") is documented below.';

  const build = (parts: {
    above?: string;
    between?: string;
    inside?: string;
    slug?: string;
    param?: string;
    anchor?: string;
    groupId?: string;
  }): string => {
    const param = parts.param ?? "f";
    return PAGE.replace("{{ABOVE}}", parts.above ?? "")
      .replace("{{BETWEEN}}", parts.between ?? "")
      .replace("{{INSIDE}}", parts.inside ?? "")
      .replace("{{SLUG}}", parts.slug ?? '    slug: "passkeys",')
      .replace(/\{\{PARAM\}\}/g, param)
      .replace("{{ANCHOR}}", parts.anchor ?? ` id={${param}.slug}`)
      .replace("{{GROUPID}}", parts.groupId ?? " id={f.slug}");
  };

  const cases: {
    name: string;
    src: string;
    /** does the anchored leg (4a+4b) say the section is real? */
    sectionReal: boolean;
    /** does the anchored leg (4c) say the anchor is rendered? */
    anchorRendered: boolean;
    /** what the shipped `includes('slug: "passkeys"')` answers on this fixture */
    shippedExists: boolean;
    /** what the shipped `includes("id={f.slug}")` answers on this fixture */
    shippedAnchor: boolean;
  }[] = [
    {
      name: "control: the shipped shape, no prose naming the literal",
      src: build({}),
      sectionReal: true,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "premise: prose above the array also names the literal, and the entry really declares it (a mention must not RED the guard)",
      src: build({ above: LINE_COMMENT }),
      sectionReal: true,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "premise: the property is spelled with single quotes — the VALUE decides, and the shipped check misses a real section",
      src: build({ slug: "    slug: 'passkeys'," }),
      sectionReal: true,
      anchorRendered: true,
      shippedExists: false,
      shippedAnchor: true,
    },
    {
      name: "premise: the FAQ map's callback parameter is renamed — the anchor is read from that parameter, not from the name `f`",
      src: build({ param: "e" }),
      sectionReal: true,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: the entry's slug is renamed and a `//` line above the array names the literal — the shipped check still reports the section as real",
      src: build({ above: LINE_COMMENT, slug: '    slug: "workspaces",' }),
      sectionReal: false,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: the slug line is DELETED and a /* */ comment inside the entry carries `slug: \"passkeys\",` on a line of its own",
      src: build({ inside: '    /* the anchor this entry used to carry:\n      slug: "passkeys",\n    */', slug: "" }),
      sectionReal: false,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: a `//` line INSIDE the array names the literal, and no entry declares it",
      src: build({ between: '  // slug: "passkeys" is declared on the entry below', slug: '    slug: "two-factor",' }),
      sectionReal: false,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: the entry carries the slug but is the question/answer shape, so the id branch never renders it",
      src: build({ slug: '    slug: "passkeys",\n    q: "What are passkeys?",' }),
      sectionReal: false,
      anchorRendered: true,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: the passkey entry's own branch renders no id — only the GROUPS map one branch up does",
      src: build({ anchor: "" }),
      sectionReal: true,
      anchorRendered: false,
      shippedExists: true,
      shippedAnchor: true,
    },
    {
      name: "DEFECT: `id={f.slug}` survives only in a comment — no element renders it at all",
      src: build({ anchor: "", groupId: "", above: "// Every FAQ section renders its entry as id={f.slug}." }),
      sectionReal: true,
      anchorRendered: false,
      shippedExists: true,
      shippedAnchor: true,
    },
  ];

  assert.ok(cases.length >= 10, `only ${cases.length} fixtures — the two directions of both halves are not all driven`);

  const sectionVacuities: string[] = [];
  const anchorVacuities: string[] = [];

  for (const tc of cases) {
    const where = `fixture ${JSON.stringify(tc.name)}`;

    // The premise first: what the SHIPPED checks answer about this fixture. A
    // fixture that does not reproduce its claimed answer fails here, so a case
    // that has quietly stopped reproducing the defect cannot pass as evidence.
    assert.equal(
      shippedSectionExists(tc.src),
      tc.shippedExists,
      `${where}: the shipped \`includes('slug: "passkeys"')\` answers ${shippedSectionExists(tc.src)}, ` +
        `the case claims ${tc.shippedExists} — re-derive the fixture before trusting the anchored answer below`,
    );
    assert.equal(
      shippedAnchorRendered(tc.src),
      tc.shippedAnchor,
      `${where}: the shipped \`includes("id={f.slug}")\` answers ${shippedAnchorRendered(tc.src)}, ` +
        `the case claims ${tc.shippedAnchor} — re-derive the fixture before trusting the anchored answer below`,
    );

    // Then the anchored answer.
    const located = faqEntryForSlug(tc.src, "passkeys");
    let why = "";
    let sectionReal = false;
    if (!located.ok) {
      why = located.reason;
    } else {
      const shape = sectionShape(located.entry);
      sectionReal = shape.ok;
      if (!shape.ok) why = shape.reason;
    }
    assert.equal(
      sectionReal,
      tc.sectionReal,
      `${where}: the anchored "the section exists" answer is ${sectionReal}, want ${tc.sectionReal}${why ? ` — ${why}` : ""}`,
    );

    const site = faqSlugRenderSite(tc.src);
    assert.equal(
      site.ok,
      tc.anchorRendered,
      `${where}: the anchored "the anchor is rendered" answer is ${site.ok}, want ${tc.anchorRendered}` +
        `${site.ok ? "" : ` — ${site.reason}`}`,
    );

    if (tc.shippedExists && !sectionReal) sectionVacuities.push(tc.name);
    if (tc.shippedAnchor && !site.ok) anchorVacuities.push(tc.name);
  }

  // Vacuity floors: the fixtures must keep reproducing the defects this file
  // exists to catch — the shipped leg saying "the section exists" while the
  // anchored one refuses, and the shipped anchor check being satisfied by
  // something other than the entry's own render site.
  assert.ok(
    sectionVacuities.length >= 4,
    `only ${sectionVacuities.length} fixtures show the shipped "section exists" check satisfied while the ` +
      `anchored one refuses (${sectionVacuities.join("; ") || "none"}) — the fixtures no longer reproduce the defect`,
  );
  assert.ok(
    anchorVacuities.length >= 2,
    `only ${anchorVacuities.length} fixtures show the shipped anchor check satisfied while the anchored one ` +
      `refuses (${anchorVacuities.join("; ") || "none"}) — the fixtures no longer reproduce the defect`,
  );

  // And the control really located the passkey entry, not merely some entry: the
  // locator is keyed on the property's VALUE, and this is the assertion that says
  // the value it found is the section the link points at.
  const control = faqEntryForSlug(build({}), "passkeys");
  if (!control.ok) assert.fail(`the control fixture must locate the entry: ${control.reason}`);
  assert.match(
    control.entry.getText(),
    /Passkeys \(sign in without a password\)/,
    "the control fixture's located entry is not the passkey section — the locator is not keyed on the slug value",
  );
});
