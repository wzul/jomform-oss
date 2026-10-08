// The /docs prose convention: a markdown marker must never reach the reader
// (card t_0447a70c).
//
// WHY THIS EXISTS (the defect, measured on prod)
//   Every /docs guide renders its prose as a plain text node — the section bodies
//   are `body: "…"` strings and the pages print them with `{p}`. There is no
//   markdown renderer on these pages (the ONE markdown renderer in the repo,
//   src/components/Markdown.ts, is the chat/feedback surface). So a backtick span
//   written into a body reaches the reader WITH its markers. Measured on
//   jomform.com, 2026-10-04, fresh browser state: /docs/chat showed
//
//     The fourth creates: `upsert_form` is proposable…
//     …deleting a form for good, `purge_form`: …
//
//   and `document.querySelectorAll("code").length === 0` on the page — the
//   markers are literal text, not a styled span. /docs/sales rendered 14 more
//   the same way, /docs/security 3, /docs/mcp 4, and /docs/chat 4 (two of them,
//   `upsert_form` / `purge_form`, added by PR #967 — the release whose docs page
//   this card's parent verified).
//
// WHY THE FIX IS THE PROSE AND NOT A RENDERER
//   The question was one decision, not one patch per page: either render inline
//   code on /docs or stop writing backticks there. The shipped convention already
//   answered it — EVERY comparable sentence in the tree writes the name as words.
//   Measured over the 18 docs pages before the fix: 11 of 775 prose strings used
//   a backtick span (25 spans), and all 11 rendered their markers. The other 764
//   name the same kinds of thing in plain prose — `sound_enabled` is written
//   sound_enabled (sales/page.tsx:79), `set_workspace_settings` and
//   `PATCH /v1/workspaces/{id}` are written bare (spam-detection/page.tsx:49,
//   workspaces/page.tsx:53), and so are `clear_billing_period`,
//   `spam_judge_enabled` and dozens more. Adding an inline-code renderer to /docs
//   would be a feature (and a second markdown path to keep in step with the
//   chat's), not a defect fix; making the 11 outliers match the other 764 is the
//   smaller change that removes the defect. Rendering code samples is still done,
//   and still correctly — but with a template literal in JSX (`` `…` `` inside
//   braces: api/page.tsx's curl blocks, embed/page.tsx's iframe snippet), which
//   prints the text verbatim instead of leaving a marker on it. That is why this
//   guard keys on STRING LITERALS: a JSX template literal is not one, and it is
//   the supported way to show code on these pages.
//
// HOW IT DECIDES (no heuristics, on purpose)
//   It parses each page with the TypeScript parser (`typescript` is already a
//   devDependency and src/lib/jsxSource.ts already depends on it for the same
//   reason) and collects a marker from EVERY node kind whose text reaches the
//   reader as plain text. A parser is used rather than a text scan because the
//   alternative has a measured failure in this very tree: a byte-level comment
//   scanner reads the RENDERED text `/v1/rest/*` in api/page.tsx:175 as a `/*`
//   comment opener and blanks 41 lines of the file (that defect is documented at
//   the top of src/lib/jsxSource.ts). With the AST, a comment is not a node at
//   all, so no comment can raise or hide a finding, and there is no strip to get
//   wrong.
//
//   FOUR shapes render an unescaped backtick to the reader, and all four are
//   collected (card t_8580357e widened the second and third from a
//   string-literal-only walk; card t_8b3a508b widened the fourth):
//     1. a `ts.StringLiteral` carrying a backtick — the `body: "…"` prose and
//        the JSX attribute strings like `label: "…"`.
//     2. a `ts.JsxText` carrying a backtick — RAW JSX prose, e.g.
//        `<p>see `marker` here</p>`. These bytes are neither a string nor a
//        template literal, so a walk that collects only string literals never
//        saw them, and they render exactly like the bodies do.
//     3. a `ts.NoSubstitutionTemplateLiteral` carrying an UNESCAPED backtick —
//        a template literal with no `${…}`, e.g. `` `prose with \`marker\` inside` ``
//        (the escaped form reaches the reader as a backtick; the author could
//        equally have written it plainly). The guard keys on `node.text`, which
//        decodes `\``, so an escaped marker and a plain one read alike.
//     4. a `ts.TemplateExpression` carrying a backtick in one of its STATIC
//        parts — a template literal WITH a `${…}` substitution, e.g.
//        `` (x: string) => `prose ${x} with \`marker\` inside` `` (card
//        t_8b3a508b). Its decoded static text is `node.head.text` followed by
//        each `templateSpans[i].literal.text`; the substitution itself renders
//        whatever the expression evaluates to, and the static parts around it
//        reach the reader with the same literal markers shapes 1 and 3 leave.
//        This is the shape card t_8580357e's widening could not see: it is
//        neither a `NoSubstitutionTemplateLiteral` (it HAS substitutions) nor a
//        string literal, so the guard reported GREEN on it while the reader saw
//        the marker — measured on the PR #975 head (2613ce89), guard unmodified,
//        a probe of exactly that line in docs/security/page.tsx left the suite
//        at 2 pass / 0 fail. Latent there (an AST scan of the 20 docs `.tsx`
//        found 14 `TemplateExpression` nodes, 0 carrying a backtick), but the
//        same "a documented exclusion is not a proof" that left shapes 2 and 3
//        open until they were driven.
//
//   THE ONE DELIBERATE EXCLUSION — a template literal inside `<code>`/`<pre>`
//   is the SUPPORTED way to show code on /docs (api/page.tsx's curl blocks,
//   embed/page.tsx's iframe snippet). Such a sample's `node.text` never contains
//   a backtick (the template's own delimiters are not part of `text`), so the
//   walk is already quiet on every one of them today — measured 9, all inside
//   `<code>`/`<pre>`. It is also SKIPPED EXPLICITLY when its text DOES contain a
//   backtick inside a `<code>`/`<pre>` element, so a future sample that prints a
//   backtick as part of the code it shows does not raise a false finding. The
//   exclusion covers BOTH template-literal shapes (3 and 4), because a sample
//   that shows code with a substitution is the same supported shape. The
//   distinction the cards named is kept: a marker in raw JSX prose is not a code
//   sample (shape 2), and a marker in a template literal OUTSIDE `<code>`/`<pre>`
//   is prose (shapes 3 and 4, e.g. `customize/page.tsx`'s `<span>{`{id}`}</span>`
//   shape if it ever escaped a marker). It does NOT widen to a template literal
//   merely because template literals are used to render code here.
//
// THE THIRD SURFACE — the /docs index's i18n CHROME LABELS (card t_cd81632d).
//   The index renders a second kind of prose that is not in any .tsx this walk
//   reads: its headings, category labels and quick links are dictionary VALUES
//   printed through the `T` island (`src/app/docs/docsChrome.tsx`:
//   `const { t } = useI18n(); return <>{t(k)}</>;` — a plain text node, with no
//   markdown renderer), and those values live in `src/components/i18nDict.ts`.
//   That file is outside DOCS_DIR, so the AST walk cannot reach it, and the
//   imported leg below does not read it either (that leg reads
//   apiExamples.gen.ts / openapiSpec.gen.ts through lib/openapi.ts). A backtick
//   written into a `docs.*` value therefore reached the reader literally while
//   this guard reported GREEN — measured on the card's head (af607fe3, clean
//   worktree): a marker planted in `docs.catSecurity`, a key the index renders
//   via `{g.labelKey}` at page.tsx:428, left the suite at 4 pass / 0 fail, and
//   the guard never named i18nDict.ts at all. Latent rather than live (no
//   `docs.*` value carries a backtick today), which is the same "a documented
//   exclusion is not a proof" shape this file's own header warns about, one
//   surface further out. The leg below is keyed on the RENDER SITE the way the
//   imported leg is: it judges the keys a /docs page really PRINTS, and not a
//   `docs.*` key that no page renders.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads the source, not the served HTML. It proves the marker is not in
//     the text a page renders — the `body` strings, the raw JSX prose, and the
//     template literals; that the page really prints `{p}` is pinned elsewhere
//     (docsIndex.test.ts's render-site checks and the fact reader in
//     web/scripts/docsFacts.mjs, which reads the BUILT page).
//   * It is scoped to `src/app/docs` PLUS the two surfaces those pages PRINT from
//     a module outside that directory: the imported generated prose (above) and
//     the /docs i18n chrome labels (below). Backticks in the chat/feedback
//     markdown surface are CORRECT there — that text goes through Markdown.ts,
//     which renders inline code as a `<code>` chip — so a tree-wide rule would be
//     wrong and would start a fight with the one place backticks belong. Nothing
//     else is widened by the two render-site-keyed legs: they judge exactly the
//     fields and the keys a /docs page declares it prints, not a module.
//   * It does not judge other markdown markers (`**bold**`, `_italic_`, `-` list
//     bullets) in docs prose. Measured before this guard was written: there are
//     none in any docs `body`/`desc`/`description` string, so a rule for them
//     would have no case to catch today and would be asserted blind — it is left
//     out rather than shipped untested. If one appears, the same argument
//     applies and this guard is where it should go.
//   * It cannot tell a backtick apart from a backtick the AUTHOR intended as a
//     literal apostrophe-ish mark. There is no such character in these pages;
//     that is a copy question, and this rule answers it the same way for every
//     one of them.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import ts from "typescript";

// The recursive `app/docs` walk, shared with llmsFullCoverage.test.ts and
// sitemapCoverage.test.ts — see that file for why ONE walk (card t_3a5aa9b5).
import { filesUnder } from "./docsRoutes.ts";
import { apiExamples, endpointsByTag, specEndpoints } from "./openapi.ts";
// The /docs index's chrome labels. `docs.*` values reach the reader through the
// `T` island as plain text nodes, so this is the third surface (card t_cd81632d);
// only the keys a /docs page DECLARES it renders are judged. `Key` is re-exported
// from ../components/i18n (a `.tsx` the node test runner cannot load), so the
// dictionary itself is imported here, exactly as docsIndex.test.ts already does.
import { DICT } from "../components/i18nDict.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

// Every .tsx under src/app/docs, RECURSIVELY — including the shared layout and the
// docs chrome island at the root (a marker in any of them renders just as literally)
// and any nested route under a parent that is not itself a route (card t_3a5aa9b5;
// the walk this file used to carry was one level deep and could not reach it).
function docsFiles(): string[] {
  return filesUnder(DOCS_DIR).filter((f) => f.endsWith(".tsx") && !f.endsWith(".d.ts"));
}

type Finding = { file: string; line: number; kind: string; text: string };

// True when a node sits inside a `<code>`/`<pre>` element — the element that
// renders a code SAMPLE. A template literal there is the supported way to show
// code on /docs (api/page.tsx's curl blocks, embed/page.tsx's iframe snippet),
// so a marker printed by such a sample is code the page MEANS to show, not a
// markdown marker leaked into prose. Uses `parent` pointers, which the parse
// below asks for (`setParentNodes = true`).
function insideCodeSample(node: ts.Node): boolean {
  for (let p = node.parent; p; p = p.parent) {
    if (ts.isJsxElement(p)) {
      const name = p.openingElement.tagName.getText();
      if (name === "code" || name === "pre") return true;
    }
  }
  return false;
}

// The marker a node carries, and the node kind, with `file:line` for the
// message. Four shapes reach the reader as plain text (header, "HOW IT
// DECIDES"); everything else returns null.
function markerIn(node: ts.Node): { kind: string; text: string } | null {
  // 1. A quoted string literal (`"…"` / `'…'`): the `body: "…"` prose and the
  //    attribute strings (`label: "…"`). `node.text` is already decoded, so a
  //    backtick in it is an unescaped marker.
  if (ts.isStringLiteral(node)) {
    return node.text.includes("`") ? { kind: "string literal", text: node.text } : null;
  }
  // 2. Raw JSX text — an element's literal children, e.g. `<p>see `marker`</p>`.
  //    Neither a string nor a template literal, so a walk that collected only
  //    `ts.StringLiteral` never saw it (card t_8580357e).
  if (ts.isJsxText(node)) {
    return node.text.includes("`") ? { kind: "raw JSX text", text: node.text } : null;
  }
  // 3. A template literal with no `${…}`. `node.text` DECODES `\``, so an
  //    ESCAPED marker reads as a backtick here — the escaped form renders the
  //    marker exactly like the plain one (card t_8580357e). The one exception
  //    is a code SAMPLE: a template literal inside <code>/<pre> is how this
  //    tree shows code, and a sample that prints a backtick is code the page
  //    means to show.
  if (ts.isNoSubstitutionTemplateLiteral(node)) {
    if (!node.text.includes("`")) return null;
    if (insideCodeSample(node)) return null;
    return { kind: "template literal", text: node.text };
  }
  // 4. A template literal WITH a `${…}` substitution. Its static parts — the
  //    text before the first `${…}` (`node.head.text`) and after each one
  //    (`templateSpans[i].literal.text`) — reach the reader literally, markers
  //    and all; the substitution itself renders whatever its expression yields.
  //    `text` DECODES `\`` in each part, exactly as shape 3's `node.text` does.
  //    Neither a `NoSubstitutionTemplateLiteral` (it has substitutions) nor a
  //    string literal, so the guard reported GREEN on it while the reader saw
  //    the marker (card t_8b3a508b). Same `<code>`/`<pre>` exclusion as shape 3.
  if (ts.isTemplateExpression(node)) {
    const text = [node.head.text, ...node.templateSpans.map((s) => s.literal.text)].join("");
    if (!text.includes("`")) return null;
    if (insideCodeSample(node)) return null;
    return { kind: "template expression", text };
  }
  return null;
}

// Every marker in one page, in source order.
function backtickMarkers(file: string): Finding[] {
  const src = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const found: Finding[] = [];
  const visit = (node: ts.Node) => {
    const m = markerIn(node);
    if (m) {
      const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
      found.push({ file, line: line + 1, kind: m.kind, text: m.text });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

const FILES = docsFiles();
const FINDINGS: Finding[] = FILES.flatMap(backtickMarkers);

test("the guard is reading the real docs tree (it cannot pass vacuously)", () => {
  // The walk must find the pages AND their prose: a parse that collected nothing
  // would satisfy the rule below while looking at no string at all.
  assert.ok(
    FILES.length >= 15,
    `found only ${FILES.length} .tsx files under ${DOCS_DIR} — the guard is not looking at the docs tree`,
  );
  const chat = FILES.find((f) => f.endsWith("/docs/chat/page.tsx"));
  assert.ok(chat, "docs/chat/page.tsx is not in the walked set — the page this card is about is not being read");
  const chatSrc = readFileSync(chat!, "utf8");
  assert.ok(
    chatSrc.includes("What it may propose"),
    "docs/chat/page.tsx is not the page this guard was written against (its section title moved)",
  );
  // Prove the AST walk really visits prose strings rather than stopping at the
  // module scope: assert a known prose body is among the visited literals.
  const bodies: string[] = [];
  // The three node kinds the walk was widened to (cards t_8580357e and
  // t_8b3a508b) are counted too, so an emptied JsxText / template-literal /
  // TemplateExpression branch cannot pass this test vacuously while the
  // string-literal count stays green.
  const jsxTexts: string[] = [];
  const codeSamples: string[] = [];
  const templateExpressions: string[] = [];
  const collect = (file: string) => {
    const sf = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const visit = (n: ts.Node) => {
      if (ts.isStringLiteral(n)) bodies.push(n.text);
      if (ts.isJsxText(n)) jsxTexts.push(n.text);
      if (ts.isNoSubstitutionTemplateLiteral(n) && insideCodeSample(n)) codeSamples.push(n.text);
      if (ts.isTemplateExpression(n)) {
        const text = [n.head.text, ...n.templateSpans.map((s) => s.literal.text)].join("");
        templateExpressions.push(text);
      }
      ts.forEachChild(n, visit);
    };
    visit(sf);
  };
  for (const f of FILES) collect(f);
  assert.ok(
    bodies.length >= 300,
    `collected only ${bodies.length} string literals from the docs tree — the AST walk is not descending into the ` +
      `prose, so a backtick written into a body would be invisible to this guard`,
  );
  assert.ok(
    bodies.some((b) => b.startsWith("The assistant is a chat panel inside the dashboard")),
    "the docs/chat prose body is not among the visited literals — the walk is not reaching the section bodies",
  );
  // Raw JSX text is the FIRST hole this guard missed (card t_8580357e): if the
  // walk stops collecting `ts.JsxText`, shape 2 goes invisible again.
  assert.ok(
    jsxTexts.length >= 500,
    `collected only ${jsxTexts.length} raw JSX text nodes from the docs tree — a marker written into raw JSX prose ` +
      `would be invisible to this guard (the hole card t_8580357e filed)`,
  );
  assert.ok(
    jsxTexts.some((t) => t.replace(/\s+/g, " ").trim().startsWith("A machine-readable OpenAPI 3.1 contract")),
    "a known raw JSX text node (docs/api) is not among the visited nodes — the walk is not reaching JSX prose",
  );
  // A code sample shown through a template literal is the SUPPORTED shape, and
  // the guard must be able to tell it apart from prose: if this collection
  // empties, `insideCodeSample` is broken and every sample would be reported.
  assert.ok(
    codeSamples.length >= 3,
    `collected only ${codeSamples.length} code-sample template literals from the docs tree — the ` +
      `code/pre element exclusion is what keeps the supported way of showing code from being reported`,
  );
  // A template literal WITH a substitution (`ts.TemplateExpression`) is the
  // FOURTH shape whose static parts reach the reader (card t_8b3a508b): if the
  // walk stops collecting it, a marker around a `${…}` goes invisible again and
  // the findings rule below cannot catch it. The pages use this shape today —
  // measured 14, all trivial (`#`, `example-`, `endpoints-`, a lone space), none
  // carrying a backtick — so the count is real, not assumed, and the named
  // `href={`#${…}`}` anchor proves the walk reaches a real one rather than an
  // empty branch.
  assert.ok(
    templateExpressions.length >= 5,
    `collected only ${templateExpressions.length} template expressions from the docs tree — a marker around a ` +
      `substitution would be invisible to this guard (the hole card t_8b3a508b filed)`,
  );
  assert.ok(
    templateExpressions.includes("#"),
    "no `#{…}` interpolation template expression (the docs sidebar hash hrefs) is among the visited nodes — " +
      "the walk is not reaching template expressions",
  );
});

test("no /docs prose carries a backtick marker (these pages render plain text)", () => {
  const lines = FINDINGS.map(
    (f) =>
      `${f.file.replace(DOCS_DIR, "src/app/docs")}:${f.line}  [${f.kind}]  ${JSON.stringify(f.text.slice(0, 120))}`,
  );
  assert.deepEqual(
    FINDINGS,
    [],
    // The message must not itself carry a literal backtick: this file is parsed
    // by the widened walk, so a marker written into THIS message would be
    // reported — and would be exactly the defect the guard forbids. The word is
    // "backtick"; the rendered example is described, not reproduced.
    `these /docs nodes carry a backtick marker. Every /docs guide renders its prose as a plain text node ` +
      `(the bodies are quoted strings printed with {p}, raw JSX text is printed as-is, and a template literal with ` +
      `no ${"${…}"} is printed verbatim), so the markers reach the reader literally — measured on ` +
      `jomform.com 2026-10-04: /docs/chat showed "The fourth creates: upsert_form is proposable…" with the tool ` +
      `name wrapped in backticks and document.querySelectorAll("code").length === 0. Write the name as plain ` +
      `words, which is what the rest of the tree already does (sound_enabled, set_workspace_settings, ` +
      `PATCH /v1/workspaces/{id} are all bare). To SHOW code on a docs page, use a template literal INSIDE ` +
      `<code>/<pre> — the supported shape (api/page.tsx's curl blocks) — which prints the text verbatim. ` +
      `Findings:\n  ${lines.join("\n  ")}`,
  );
});

// ── the IMPORTED-prose surface (cards t_763c28c2, t_8f684cfe) ────────────────
//
// The walk above is scoped to src/app/docs, so it can only see prose a page
// DECLARES itself — a `body: "…"` string, raw JSX text, or a template literal.
// /docs/api is not that shape: its worked examples and its endpoint reference are
// imported (src/lib/openapi.ts → the generated apiExamples.gen.ts and
// openapiSpec.gen.ts) and printed with a JSX expression, `{e.note}` /
// `{e.description}`. A marker in one of those strings is not in any .tsx the walk
// reads, so the walk reported GREEN while the live page rendered 20 literal
// backticks (measured on jomform.com 2026-10-05, fresh browser state).
//
// The first cut of this leg read only four fields — `{e.why}`/`{e.note}` and
// `{e.summary}`/`{e.description}` (card t_763c28c2). It missed two more the page
// prints as plain JSX text, which card t_8f684cfe drove RED and added:
//   * `{e.title}`, the worked-example HEADING (api/page.tsx:139);
//   * `{e.rateLimit}`, the throttle policy (:388 and the throttled list at :291,
//     shaped from x-jomform-rate-limit.policy).
// Both are imported strings in neither the page source nor the four fields above,
// so a marker in either was invisible to this leg — measured on the t_763c28c2
// head: a marker in an example title left the suite at 4 pass / 0 fail.
//
// A THIRD imported string the page prints as plain JSX text was still missed
// (card t_62da3451): `{group.tag}`, each endpoint GROUP's HEADING
// (api/page.tsx:368 — an `<h3>`, NOT inside `<code>`, measured live on prod
// 2026-10-05: all 19 tag headings have `closest("code") === null`). `group` comes
// from `endpointsByTag()` (lib/openapi.ts:100), whose `tag` is
// `op.tags?.[0] ?? "Other"` (:88) read from OPENAPI_SPEC — the CONTRACT's `tags[]`
// in the generated openapiSpec.gen.ts, exactly the imported shape the six fields
// above are. A marker there reached the reader while this leg reported GREEN —
// measured on the #984 head (5e3b8d58, tree daa6906): editing the FIRST `"Profile"`
// tag (openapiSpec.gen.ts:5528) so it reads with a backtick injected left the
// suite at 4 pass / 0 fail. It is the same residual class #984 narrowed but did
// not close, which is why this leg now reads the tag too.
//
// A FOURTH imported string (card t_d1221e35): the ACCESS label
// (api/page.tsx:379 — `{e.access}` in a `<span>`, NOT inside `<code>`, measured
// live on prod 2026-10-05: it renders READ with `closest("code") === null`),
// printed once per endpoint. `e.access` is `op["x-jomform-access"] ?? "read"`
// (lib/openapi.ts:89) from the SAME generated contract (120 occurrences), the same
// imported shape `{group.tag}` and `{e.rateLimit}` are — yet a marker in it was
// invisible to this leg. Measured on this card's parent head (tree 09796a85):
// editing the FIRST `"x-jomform-access": "read"` (openapiSpec.gen.ts:731) to
// `"re\`ad"` left the suite at **6 pass / 0 fail**. The METHOD badge in the same
// `<span>` block ({e.method}, api/page.tsx:375) is judged alongside it — its value
// is an object KEY of the contract's `paths` map rather than a generated string
// field, so a marker there would have to come from the generator, but the render
// site is the same plain-text shape and the whole span block is now covered.
//
// This second surface is keyed on the RENDER SITE, not on the bare string: the
// fields below are the ones the page PRINTS as plain text. `e.curl` is
// deliberately NOT among them — it is rendered inside `<code>`, the supported way
// to show code, so a backtick in a curl string (or in a `<code>` template
// literal) is code the page means to show, exactly as the DOCS_DIR walk treats
// it. `e.path` and `e.expectContains` are excluded for the same reason (both are
// inside `<code>` at :377 and :146 respectively). `{group.tag}` above is NOT such
// an exclusion: the page prints it in an `<h3>` (:368), plain text. Adding a field
// here that the page does not render would assert blindly; the vacuity test below
// pins each render site against the page source.
type ImportedProse = { module: string; surface: string; field: string; text: string };

function importedProseValues(): ImportedProse[] {
  const out: ImportedProse[] = [];
  const EXAMPLES_MOD = "src/lib/apiExamples.gen.ts";
  const SPEC_MOD = "src/lib/openapiSpec.gen.ts";
  for (const e of apiExamples) {
    out.push({ module: EXAMPLES_MOD, surface: `docs/api example "${e.id}"`, field: "{e.why}", text: e.why });
    out.push({ module: EXAMPLES_MOD, surface: `docs/api example "${e.id}"`, field: "{e.note}", text: e.note });
    // The example's HEADING (api/page.tsx:139, `{e.title}`) is plain JSX text
    // too, so a marker in a title reaches the reader exactly as a marker in a
    // note does (card t_8f684cfe). It is a separate generated field from `note`
    // and was the one imported string the first cut of this leg never read.
    out.push({ module: EXAMPLES_MOD, surface: `docs/api example "${e.id}"`, field: "{e.title}", text: e.title });
  }
  // The endpoint GROUP HEADINGS (api/page.tsx:368, `{group.tag}` in an <h3>) come
  // from the contract's own `tags[]`, so a marker in a tag reaches the reader as
  // plain text exactly as a marker in a summary does (card t_62da3451). ONE value
  // per tag, matching the ONE heading the page prints per tag — the page does not
  // print the tag anywhere else as prose (the group's `id` is an attribute, not
  // text), so pushing it per endpoint would judge the same string several times.
  for (const g of endpointsByTag()) {
    out.push({ module: SPEC_MOD, surface: `docs/api endpoint group "${g.tag}"`, field: "{group.tag}", text: g.tag });
  }
  for (const e of specEndpoints()) {
    const where = `docs/api endpoint ${e.method} ${e.path}`;
    out.push({ module: SPEC_MOD, surface: where, field: "{e.summary}", text: e.summary });
    out.push({ module: SPEC_MOD, surface: where, field: "{e.description}", text: e.description });
    // The THROTTLE policy (api/page.tsx:388 `rate limited: {e.rateLimit}` and the
    // throttled list at :291) is likewise plain text; it is shaped from the
    // contract's x-jomform-rate-limit.policy (card t_8f684cfe).
    out.push({ module: SPEC_MOD, surface: where, field: "{e.rateLimit}", text: e.rateLimit });
    // The ACCESS label (api/page.tsx:379, `{e.access}` in a <span>, NOT inside
    // <code> — measured live on prod 2026-10-05: the label renders READ with
    // closest("code") === null) is printed once per endpoint, plain text. It comes
    // from the contract's own `x-jomform-access` (lib/openapi.ts:89,
    // `op["x-jomform-access"] ?? "read"`) in openapiSpec.gen.ts — the same
    // imported shape {e.summary} and {e.rateLimit} are — so a marker in it reaches
    // the reader exactly as a marker in a summary does. Card t_d1221e35 drove this
    // RED: on the parent head, editing the FIRST `"x-jomform-access": "read"` left
    // the suite at 6 pass / 0 fail while this leg never named the field.
    out.push({ module: SPEC_MOD, surface: where, field: "{e.access}", text: e.access });
    // The METHOD badge (api/page.tsx:375, `{e.method}` in a <span>, NOT inside
    // <code>) is the other plain-text imported string in the same span block
    // (card t_d1221e35). Its value is an object KEY of the contract's `paths` map
    // (lib/openapi.ts:79, `Object.keys(item)`, upper-cased), so a marker there
    // would have to come from the generator rather than the contract writer — a
    // lower-risk surface than {e.access}, but the same class: an imported string
    // the page prints as plain text, so it is judged here too.
    out.push({ module: SPEC_MOD, surface: where, field: "{e.method}", text: e.method });
  }
  return out;
}

const IMPORTED_PROSE = importedProseValues();
const IMPORTED_FINDINGS = IMPORTED_PROSE.filter((p) => p.text.includes("`"));

test("the imported-prose check is reading the real rendered data (it cannot pass vacuously)", () => {
  // The set must really hold the imported strings the page renders — an emptied
  // list (a renamed export, a shaping layer that dropped a field) would make the
  // rule below pass while looking at nothing.
  assert.ok(
    IMPORTED_PROSE.length >= 100,
    `collected only ${IMPORTED_PROSE.length} imported prose values — the endpoint reference ` +
      `(>50 operations) and the worked examples are not being read`,
  );
  for (const e of apiExamples) {
    assert.ok(
      IMPORTED_PROSE.some((p) => p.text === e.note && p.surface.includes(e.id)),
      `example "${e.id}"'s note is not among the checked values — a marker there would be invisible`,
    );
  }
  // The two fields card t_8f684cfe added must really be in the set, keyed to the
  // surface that renders them: the render-site loop below iterates whatever
  // fields are present, so dropping one of these pushes would leave it passing
  // while the field became invisible again. Asserted per-item, not by count.
  for (const e of apiExamples) {
    assert.ok(
      IMPORTED_PROSE.some((p) => p.field === "{e.title}" && p.text === e.title && p.surface.includes(e.id)),
      `example "${e.id}"'s title is not among the checked values — a marker there would be invisible`,
    );
  }
  for (const e of specEndpoints()) {
    assert.ok(
      IMPORTED_PROSE.some(
        (p) => p.field === "{e.rateLimit}" && p.surface.includes(`${e.method} ${e.path}`),
      ),
      `endpoint ${e.method} ${e.path}'s rateLimit is not among the checked values — a marker there would be invisible`,
    );
  }
  // The ACCESS label and the METHOD badge (card t_d1221e35) must really be in the
  // set too, exactly as the tag/title/rateLimit fields above: the render-site loop
  // below iterates whatever fields are present, so dropping either push would leave
  // the suite passing while the field became invisible again — the hole this card
  // closed (measured on the parent head: a marker in `x-jomform-access` left the
  // suite at 6 pass / 0 fail). Asserted per-item against the surface that renders
  // it, not by count.
  for (const e of specEndpoints()) {
    assert.ok(
      IMPORTED_PROSE.some(
        (p) => p.field === "{e.access}" && p.text === e.access && p.surface.includes(`${e.method} ${e.path}`),
      ),
      `endpoint ${e.method} ${e.path}'s access label is not among the checked values — a marker there would be invisible`,
    );
    assert.ok(
      IMPORTED_PROSE.some(
        (p) => p.field === "{e.method}" && p.text === e.method && p.surface.includes(`${e.method} ${e.path}`),
      ),
      `endpoint ${e.method} ${e.path}'s method badge is not among the checked values — a marker there would be invisible`,
    );
  }
  // The endpoint GROUP HEADING card t_62da3451 added must be in the set too, keyed
  // to the tag the page prints: the render-site loop below iterates whatever fields
  // are present, so dropping this push would leave the test passing while the tag
  // heading became invisible again. Asserted per-item, not by count.
  for (const g of endpointsByTag()) {
    assert.ok(
      IMPORTED_PROSE.some((p) => p.field === "{group.tag}" && p.text === g.tag),
      `the endpoint group heading "${g.tag}" is not among the checked values — a marker in a ` +
        `contract tag would be invisible`,
    );
  }
  // Every field this file claims to cover must have a RENDER SITE in the page, or
  // the check is asserting a string nobody prints. Comments are stripped so a
  // field merely NAMED in a comment cannot satisfy this.
  const apiPage = readFileSync(join(DOCS_DIR, "api", "page.tsx"), "utf8")
    .split("\n")
    .filter((l) => !l.trim().startsWith("//") && !l.trim().startsWith("*") && !l.trim().startsWith("/*"))
    .join("\n");
  for (const field of [...new Set(IMPORTED_PROSE.map((p) => p.field))]) {
    const token = field.slice(1, -1); // strip the braces: e.note -> e.note
    assert.ok(
      apiPage.includes(`{${token}}`),
      `the page has no render site for ${field} — this check would assert a string the reader never sees`,
    );
  }
  // And the code-sample fields must be excluded, or every curl command (and every
  // path / expected-fragment) would be reported: the page renders all of them
  // inside <code>, the supported shape.
  assert.ok(
    IMPORTED_PROSE.every((p) => !p.field.includes("curl")),
    "e.curl is in the imported-prose set; it is a code sample (rendered in <code>) and must not be judged as prose",
  );
  assert.ok(
    IMPORTED_PROSE.every((p) => p.field !== "{e.path}" && p.field !== "{e.expectContains}"),
    "a <code>-rendered field (e.path / e.expectContains) is in the imported-prose set; those are code samples and must not be judged as prose",
  );
});

test("no /docs imported prose carries a backtick marker (the api page prints it plain text)", () => {
  const lines = IMPORTED_FINDINGS.map(
    (f) => `${f.module}  ${f.surface}  [${f.field}]  ${JSON.stringify(f.text.slice(0, 120))}`,
  );
  assert.deepEqual(
    IMPORTED_FINDINGS,
    [],
    `these /docs/api values carry a backtick marker but are printed as plain JSX text (measured on ` +
      `jomform.com 2026-10-05, fresh browser state: /docs/api rendered 20 literal backticks). The ` +
      `values come from web/src/lib/apiExamples.gen.ts and openapiSpec.gen.ts, which are GENERATED — ` +
      `edit the source (api/internal/httpapi/openapi_examples.go for a worked-example note, ` +
      `openapi_routes.go for an endpoint description) and regenerate with ` +
      `"cd api && go run ./cmd/openapi-gen". Write the name as plain words, as the rest of the tree ` +
      `does; to show code, use a template literal INSIDE <code> (the curl blocks). ` +
      `Findings:\n  ${lines.join("\n  ")}`,
  );
});

// ── the THIRD surface: the /docs index's i18n CHROME LABELS (card t_cd81632d) ──
//
// WHY THIS EXISTS. /docs renders a second kind of prose that is in no .tsx at all:
// its page heading, its intro, its five "start here" steps, its section headings,
// the eight category headings, the five quick-link labels and the FAQ heading are
// DICTIONARY VALUES printed through the `T` island —
//
//   src/app/docs/docsChrome.tsx:  export function T({ k }) { const { t } = useI18n(); return <>{t(k)}</>; }
//
// — which emits the value as a plain text node, with no markdown renderer, exactly
// as the `body: "…"` strings of the guide pages are emitted. The values live in
// src/components/i18nDict.ts, which is OUTSIDE `DOCS_DIR`, so the AST walk above
// cannot reach it, and the imported leg above does not read it either (that leg
// reads apiExamples.gen.ts / openapiSpec.gen.ts through lib/openapi.ts). So a
// backtick written into a `docs.*` label reached the reader with its markers while
// this guard reported GREEN.
//
// MEASURED, not reasoned about (card t_cd81632d): on this file's own head
// (af607fe3, clean worktree) a marker planted in `docs.catSecurity` — a key the
// index renders through `{g.labelKey}` at page.tsx:428 — left the suite at
// 4 pass / 0 fail, and the guard never named i18nDict.ts. The file was restored
// byte-identically afterwards. It is LATENT rather than live today: no `docs.*`
// value carries a backtick (measured: a grep of the `"docs.` lines for a backtick
// returns nothing). That is the same "a documented exclusion is not a proof"
// shape this file's own header warns about, one surface further out.
//
// KEYED ON THE RENDER SITE, like the imported leg and for the same reason: what
// is judged is the value whose KEY a /docs page DECLARES it prints. A `docs.*`
// key the pages render nowhere is NOT asserted here — asserting it would make a
// backtick in a string no reader ever sees a failure, and it would also mean this
// leg's coverage grew the moment an unrelated key appeared in the dictionary. The
// vacuity test below pins each site FORM against the page source and refuses a
// key it cannot place in one of them.
//
// The three forms a /docs page declares a rendered label in, all read out of the
// page source with comments stripped (a key NAMED in a comment is not a key the
// page prints — the same rule, and the same strip, that docsIndex.test.ts uses for
// its own render sites, card t_7b24b035):
//   * `<T k="docs.title" />` — a literal key on the island.
//   * `<T k={l.labelKey} />` — a DATA key, where the page's own array declares
//     `labelKey: "docs.linkDesign"`. The pair is the render site: the island
//     prints whatever key the data names, so the data's key is what is judged.
//   * `<T k={g.labelKey} />` over a `labelKey: "docs.catSecurity"` — the same
//     shape, a different array. Both are read by matching the data form directly.
type RenderedLabel = { key: string; file: string; site: string };

// The comment forms that render nothing — the identical rule docsIndex.test.ts
// already applies before it reads a render site: a `{/* … */}` JSX container, a
// module-scope `/* … */` block and a `//` line. Deliberately NOT stripped: a
// template literal, whose text really does reach the page (customize/page.tsx
// renders through one) — instead, a render-site match that falls INSIDE a
// template literal is refused by the vacuity test, so a key that is only sample
// text can never be read as a label.
function stripUnrenderedComments(src: string): string {
  return src
    .replace(/\{\s*\/\*[\s\S]*?\*\/\s*\}/g, " ")
    .replace(/^[ \t]*\/\*[\s\S]*?\*\//gm, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ");
}

// The [start, end) ranges of the template literals in a comment-stripped source.
// Used only to REFUSE a render-site match that sits inside one (a code sample
// that spells a `docs.*` key is not a label the page prints). Backslash escapes
// are honoured so an escaped backtick cannot close a span early.
function templateSpans(src: string): [number, number][] {
  const spans: [number, number][] = [];
  let i = 0;
  while (i < src.length) {
    if (src[i] === "`") {
      const start = i;
      i += 1;
      while (i < src.length && src[i] !== "`") i += src[i] === "\\" ? 2 : 1;
      spans.push([start, i + 1]);
      i += 1;
      continue;
    }
    i += 1;
  }
  return spans;
}

const inTemplate = (spans: [number, number][], at: number) =>
  spans.some(([s, e]) => at >= s && at < e);

// Every label a /docs page declares it renders, read from the source. One entry
// per (key, site); the same key from the same file twice is deduped by the caller.
function renderedLabels(): RenderedLabel[] {
  const out: RenderedLabel[] = [];
  for (const file of docsFiles()) {
    const src = stripUnrenderedComments(readFileSync(file, "utf8"));
    const spans = templateSpans(src);
    const rel = file.replace(DOCS_DIR, "src/app/docs");
    for (const [re, site] of [
      // A literal key on the island: `<T k="docs.title" />`. Both attribute forms
      // are read — the plain one and the braced string one — because the JSX the
      // island is used with may spell either.
      [/<T\s+k="([^"]+)"/g, "<T k=\"…\" />"],
      [/<T\s+k=\{\s*"([^"]+)"\s*\}/g, "<T k={\"…\"} />"],
      // A data-declared key: `labelKey: "docs.catSecurity"` in the array the page
      // maps over and renders with `<T k={g.labelKey} />`.
      [/labelKey:\s*"([^"]+)"/g, "labelKey: \"…\" (rendered by <T k={…labelKey} />)"],
    ] as const) {
      for (const m of src.matchAll(re)) {
        const at = m.index ?? 0;
        // A key spelled inside a template literal is code the page SHOWS, not a
        // label it prints. Refusing it here is what keeps this leg's coverage
        // exactly the declared render sites.
        if (inTemplate(spans, at)) continue;
        out.push({ key: m[1], file: rel, site });
      }
    }
  }
  return out;
}

const RENDERED_LABELS = renderedLabels();
const RENDERED_KEYS = [...new Set(RENDERED_LABELS.map((l) => l.key))].sort();
const DICT_MODULE = "src/components/i18nDict.ts";
const DICT_DOCS_KEYS = Object.keys(DICT).filter((k) => k.startsWith("docs."));

// The locale values of a rendered key that carry a backtick marker.
function labelMarkers(key: string): { lang: string; text: string }[] {
  const entry = (DICT as Record<string, Record<string, unknown>>)[key];
  if (!entry) return [];
  return Object.entries(entry)
    .filter(([, v]) => typeof v === "string" && v.includes("`"))
    .map(([lang, v]) => ({ lang, text: v as string }));
}

const LABEL_FINDINGS = RENDERED_KEYS.map((key) => ({ key, markers: labelMarkers(key) })).filter(
  (f) => f.markers.length > 0,
);

test("the i18n-label check is reading the real rendered keys (it cannot pass vacuously)", () => {
  // The dictionary must really be the one that holds the /docs chrome labels — a
  // renamed export, or a dictionary that emptied, would make the rule below pass
  // while looking at no label at all.
  assert.ok(
    DICT_DOCS_KEYS.length >= 15,
    `found only ${DICT_DOCS_KEYS.length} "docs.*" keys in ${DICT_MODULE} — the dictionary the /docs ` +
      `index prints through <T/> is not being read, so a marker in a label would be invisible`,
  );
  // And the render-site parse must find the labels the page really declares. Three
  // forms, each pinned to a REAL key, so a parse that stops reading one of them
  // cannot pass on the strength of the others.
  const keys = new Set(RENDERED_KEYS);
  for (const k of ["docs.title", "docs.catSecurity", "docs.linkAPI"]) {
    assert.ok(
      keys.has(k),
      `the render-site parse did not find ${k} — a /docs page declares it (a literal <T k="…"/>, a ` +
        `GROUPS labelKey and a QUICK_LINKS labelKey respectively), so a marker in it would be invisible`,
    );
  }
  // Every key the parse judged must be a `docs.*` key: the render sites are all in
  // the docs chrome, so a parse that swept in an unrelated string (a class name, an
  // href, a page title) has stopped being a render-site read and must fail loudly
  // rather than assert strings nobody prints as labels.
  const notDocs = RENDERED_KEYS.filter((k) => !k.startsWith("docs."));
  assert.deepEqual(
    notDocs,
    [],
    `the render-site parse read these as i18n labels but they are not "docs.*" keys — the parse is ` +
      `sweeping in strings that are not labels at all:\n  ${notDocs.join("\n  ")}`,
  );
  // A key must be a key the dictionary HAS, or the rule below would judge nothing
  // and the leg would be a shape that cannot fail.
  const missing = RENDERED_KEYS.filter((k) => !DICT_DOCS_KEYS.includes(k));
  assert.deepEqual(
    missing,
    [],
    `these rendered labels are not in ${DICT_MODULE}, so t() would print the raw key and the marker ` +
      `check above would be judging nothing:\n  ${missing.join("\n  ")}`,
  );
  // The render SITES must be the three forms the page uses, counted from the
  // source: at least one literal island use and at least the two data arrays'
  // worth of `labelKey:` declarations. A page that stopped rendering its labels
  // through <T/> at all would satisfy the rule below vacuously.
  const pageSrc = stripUnrenderedComments(readFileSync(join(DOCS_DIR, "page.tsx"), "utf8"));
  assert.ok(
    /<T\s+k=/.test(pageSrc),
    `src/app/docs/page.tsx no longer renders any label through the <T/> island — the check below ` +
      `would be asserting the values of labels the page does not print`,
  );
  const labelKeys = [...pageSrc.matchAll(/labelKey:\s*"[^"]+"/g)].length;
  assert.ok(
    labelKeys >= 12,
    `found only ${labelKeys} labelKey declarations in src/app/docs/page.tsx — the GROUPS and ` +
      `QUICK_LINKS arrays are where the category and quick-link labels are declared`,
  );
});

test("no /docs i18n label carries a backtick marker (the T island prints it plain text)", () => {
  const lines = LABEL_FINDINGS.flatMap((f) =>
    f.markers.map(
      (m) => `${DICT_MODULE}  ${f.key}  [${m.lang}]  ${JSON.stringify(m.text.slice(0, 120))}`,
    ),
  );
  assert.deepEqual(
    LABEL_FINDINGS,
    [],
    `these /docs i18n label values carry a backtick marker but are printed as plain text nodes — ` +
      `src/app/docs/docsChrome.tsx renders them through the T island ` +
      `(const { t } = useI18n(); return <>{t(k)}</>), with no markdown renderer, exactly as the ` +
      `guide pages print their body strings, so the marker reaches the reader literally. ` +
      `Measured on this guard's own head (af607fe3): a marker planted in docs.catSecurity, a key ` +
      `the /docs index renders through {g.labelKey}, left the suite at 4 pass / 0 fail and this ` +
      `file did not name ${DICT_MODULE} at all (card t_cd81632d). Write the name as plain words, ` +
      `which is what the rest of the tree already does. ` +
      `Findings:\n  ${lines.join("\n  ")}`,
  );
});
