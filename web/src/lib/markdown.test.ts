// The Markdown renderer for assistant output — the guard for the reported defect:
// an assistant answer showed its raw `**` markers instead of bold text.
//
// WHAT THIS FILE OWNS
//   1. THE ORIGINAL COMPLAINT, first: `**bold**` becomes <strong>, and no `**`
//      survives into the rendered text. The pre-fix page rendered `{m.content}`
//      into a `whitespace-pre-wrap` div, so the markers were the visible text.
//   2. Every element the card's table requires is an ELEMENT: italic, inline
//      code, a fenced block, ul/ol lists, links, and blank-line paragraphs.
//   3. It renders for real, not by source-scanning: react-dom/server
//      (renderToStaticMarkup) drives the component's actual element tree, and
//      the assertions read the HTML it produced. formPublicURL.test.ts already
//      uses this shape, so it runs under the existing gate
//      (`node --test src/lib/*.test.ts`) — no new runner, no vitest.
//   4. SECURITY. Assistant output is not trusted: the merchant's data (a form
//      answer, a product name, a customer's message) is echoed into replies, and
//      a question can contain anything. So a `<script>` or `<img onerror=…>`
//      inside the text must arrive in the DOM as characters, never as an element
//      — and a `javascript:` link must never become an anchor href.
//   5. The two surfaces share ONE renderer, and the raw text is what the server
//      stores (no "clean-up" on write), so a future renderer fix needs no
//      migration.
//
// WHY THE RENDERER IS Markdown.ts AND NOT .tsx: node --test strips types but
// cannot parse JSX, so a .tsx component would be unreachable from this file —
// and a guard that cannot import what it guards is decoration. The component is
// written with createElement for exactly this reason; converting it back to .tsx
// breaks these assertions.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { Markdown, parseInline, parseMarkdown } from "../components/Markdown.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const render = (text: string, variant?: "assistant" | "user") =>
  renderToStaticMarkup(createElement(Markdown, variant ? { text, variant } : { text }));

// The visible text of the rendered tree, with tags stripped — this is what a
// reader actually sees, which is how "no ** survives" is checked.
const visible = (html: string) =>
  html
    .replace(/<[^>]*>/g, "")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#x27;/g, "'")
    .replace(/&amp;/g, "&");

test("the guard is looking at real files and the renderer really runs (it cannot pass vacuously)", () => {
  const src = read("../components/Markdown.ts");
  assert.ok(src.includes("export function Markdown"), "Markdown.ts is not the renderer source");
  // The element path is the reason this component exists; a switch to innerHTML
  // would make every security assertion below pass vacuously. Match the PROPERTY
  // form only (the file's header comment discusses the name in prose).
  assert.doesNotMatch(
    src,
    /dangerouslySetInnerHTML\s*[:=]/,
    "the renderer now uses dangerouslySetInnerHTML — the XSS guard in this file would be meaningless",
  );
  assert.ok(
    render("plain teks").includes("plain teks"),
    "the renderer produced no output for plain text",
  );
});

// ── 1. the reported defect ───────────────────────────────────────────────────

test("**bold** renders as <strong> and no ** survives into the visible text", () => {
  const html = render("Jumlah jualan minggu ini **RM1,250.00** sahaja.");
  assert.match(
    html,
    /<strong[^>]*>RM1,250\.00<\/strong>/,
    "**bold** did not become <strong> — this is the exact defect the card reports",
  );
  assert.doesNotMatch(visible(html), /\*\*/, "a raw ** is still visible in the rendered answer");
  assert.doesNotMatch(html, /\*\*/, "a raw ** is still in the output at all");
});

test("italic renders as <em> for both markers, and arithmetic stays arithmetic", () => {
  const star = render("ini *condong* ya");
  assert.match(star, /<em>condong<\/em>/, "*text* did not become <em>");
  const under = render("ini _condong_ ya");
  assert.match(under, /<em>condong<\/em>/, "_text_ did not become <em>");
  // `2 * 3 * 4` has spaces around the markers; emphasis must not swallow it.
  const math = render("kira 2 * 3 * 4 = 24");
  assert.ok(visible(math).includes("2 * 3 * 4 = 24"), `arithmetic was mangled: ${visible(math)}`);
  assert.doesNotMatch(math, /<em>/, "a spaced * became emphasis");
});

test("bold wins over italic in the ** ordering (the markers do not half-consume)", () => {
  const html = render("**tebal** dan *serong*");
  assert.match(html, /<strong[^>]*>tebal<\/strong>/);
  assert.match(html, /<em>serong<\/em>/);
});

// ── 2. the required element set ──────────────────────────────────────────────

test("inline code becomes <code>, fenced code becomes <pre><code>", () => {
  const inline = render("jalankan `npm run build` dulu");
  assert.match(inline, /<code[^>]*>npm run build<\/code>/, "inline code is not an element");
  assert.doesNotMatch(visible(inline), /`/, "the backticks are still visible");

  const fenced = render("Contoh:\n\n```bash\ncurl -X POST https://example.com\n```\n\nSelesai.");
  assert.match(fenced, /<pre[^>]*>/, "a fenced block did not become <pre>");
  assert.match(fenced, /<code[^>]*>curl -X POST https:\/\/example\.com<\/code>/, "the code block body is not code");
  assert.doesNotMatch(visible(fenced), /```/, "the fence markers are still visible");
  // Markdown inside a code block is NOT formatted — the span is verbatim.
  const literal = render("```\n**bukan bold**\n```");
  assert.ok(visible(literal).includes("**bukan bold**"), "the code block was markdown-formatted, not verbatim");
  assert.doesNotMatch(literal, /<strong/, "markdown inside a code fence was applied");
});

test("code blocks scroll inside the card instead of widening it", () => {
  // The layout half of this claim (no horizontal page overflow at 412px) is
  // measured in a real browser and reported in the PR; what is pinned here is
  // the class that carries it, so it cannot be dropped silently.
  const html = render("```\n" + "x".repeat(400) + "\n```");
  assert.match(
    html,
    /<pre class="[^"]*overflow-x-auto[^"]*"/,
    "the code block lost overflow-x-auto — a wide block now pushes the card past the viewport",
  );
  assert.match(html, /<code class="[^"]*whitespace-pre[^"]*/, "the code block cannot preserve its own lines");
  const long = render("tengok `" + "y".repeat(200) + "` ini");
  assert.match(long, /<code class="[^"]*break-words[^"]*"/, "a long inline code span cannot break on a phone");
});

test("- and 1. lists become <ul>/<ol> with list markers", () => {
  const ul = render("Borang anda:\n\n- Daftar Ahli\n- Bayaran\n- Maklum balas");
  assert.match(ul, /<ul[^>]*>/, "a bullet list did not become <ul>");
  assert.equal([...ul.matchAll(/<li[^>]*>/g)].length, 3, "the bullet list lost items");
  assert.match(ul, /<li[^>]*>Daftar Ahli<\/li>/, "a list item's text is not in the <li>");
  assert.doesNotMatch(visible(ul), /^- /m, "the - markers are still visible");

  const ol = render("Langkah:\n\n1. Buka Settings\n2. Pilih Payment\n3. Simpan");
  assert.match(ol, /<ol[^>]*>/, "an ordered list did not become <ol>");
  assert.equal([...ol.matchAll(/<li[^>]*>/g)].length, 3, "the ordered list lost items");
  assert.doesNotMatch(visible(ol), /^\d+\. /m, "the number markers are still visible");
});

test("links become <a> with rel=noopener noreferrer in a new tab", () => {
  const html = render("Baca [panduan](https://jomform.com/docs/chat) ini.");
  const anchor = html.match(/<a\b[^>]*>/)?.[0] ?? "";
  assert.match(anchor, /href="https:\/\/jomform\.com\/docs\/chat"/, "an https link did not become an anchor with its href");
  assert.match(anchor, /rel="noopener noreferrer"/, "the external link has no rel=noopener noreferrer");
  assert.match(anchor, /target="_blank"/, "the external link does not open in a new tab");
  assert.match(visible(html), /panduan/, "the link text was lost");
  assert.doesNotMatch(visible(html), /\]\(/, "the markdown link syntax is still visible");
});

test("a blank line separates paragraphs, and a single newline stays a line break", () => {
  const two = render("Perenggan satu.\n\nPerenggan dua.");
  assert.equal([...two.matchAll(/<p[^>]*>/g)].length, 2, "a blank line did not start a new paragraph");
  // The page used to render with whitespace-pre-wrap; a line break the user (or
  // the model) typed must not disappear.
  assert.match(two, /<p class="[^"]*whitespace-pre-wrap[^"]*"/, "line breaks inside a paragraph are no longer preserved");
  const one = render("Baris satu.\nBaris dua.");
  assert.equal([...one.matchAll(/<p[^>]*>/g)].length, 1, "a single newline became a paragraph break");
  assert.match(visible(one), /Baris satu\.\nBaris dua\./, "the single newline was dropped");
});

test("an empty answer renders nothing rather than an empty bubble", () => {
  assert.equal(render(""), "");
  assert.equal(render("   \n  "), "");
});

// ── 3. and 4. security ───────────────────────────────────────────────────────

test("HTML in the text is characters, never elements (no <script>, no <img>)", () => {
  const payload = '**<img src=x onerror=alert(1)>**';
  const html = render(payload);
  assert.ok(!html.includes("<img"), `an <img> element was created from model text: ${html}`);
  // Escaped, i.e. it is a character sequence inside the paragraph — not a tag and
  // not an attribute. (`onerror=` may appear as TEXT; what must not appear is a
  // real tag carrying it, which the <img check above covers.)
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/, "the payload text was not escaped");
  assert.match(
    html,
    /<strong[^>]*>&lt;img src=x onerror=alert\(1\)&gt;<\/strong>/,
    "the payload was not escaped inside <strong>",
  );
  // It is still shown, as text: hiding it would be its own defect.
  assert.match(visible(html), /<img src=x onerror=alert\(1\)>/, "the offending text was silently dropped");

  const script = render('cuba <script>alert("xss")</script> ini');
  assert.ok(!script.includes("<script"), `a <script> element was created from model text: ${script}`);
  assert.match(script, /&lt;script&gt;alert\(&quot;xss&quot;\)&lt;\/script&gt;/, "the script text was not escaped");
  assert.match(visible(script), /<script>alert\("xss"\)<\/script>/, "the script text was silently dropped");
});

test("an unsafe link scheme stays text — javascript: never becomes an href", () => {
  const html = render("[klik](javascript:alert(1)) sini");
  assert.doesNotMatch(html, /href="javascript:/i, "a javascript: URL reached an href");
  assert.doesNotMatch(html, /<a[\s>]/, "an unsafe-scheme link was still rendered as an anchor");
  assert.match(visible(html), /klik/, "the link text was lost");

  for (const bad of ["data:text/html,<script>alert(1)</script>", "/relative/path", "ftp://example.com/x"]) {
    const out = render(`[x](${bad})`);
    assert.doesNotMatch(out, /<a[\s>]/, `a non-http(s) scheme became an anchor: ${bad}`);
  }
  // http is allowed alongside https (the repo's own public form URLs can be http
  // in local/dev deployments; the strict https-only rule is for <img src>).
  assert.match(render("[x](http://localhost:8080/f/abc)"), /href="http:\/\/localhost:8080\/f\/abc"/);
});

test("an image is reduced to its alt text (images are deliberately unsupported)", () => {
  const html = render("![panduan](https://example.com/a.png) di sini");
  assert.ok(!html.includes("<img"), "an image was rendered — the renderer does not support images");
  assert.match(visible(html), /panduan/, "the alt text was lost");
  assert.doesNotMatch(visible(html), /!\[/, "the image syntax is still visible");
});

// ── 5. the surfaces share one renderer ──────────────────────────────────────

test("both chat bubbles get their own tone, so code stays legible on each", () => {
  const assistant = render("guna `npm ci`", "assistant");
  const user = render("guna `npm ci`", "user");
  assert.match(assistant, /<code class="[^"]*bg-slate-200\/70[^"]*"/, "the assistant bubble's code chip lost its light background");
  assert.match(user, /<code class="[^"]*bg-white\/20 text-white[^"]*"/, "the user bubble's code chip is not readable on the indigo bubble");
  assert.notEqual(assistant, user, "the two bubbles render identically — one of them is illegible");
});

test("the chat page, /feedback and the /admin triage all use this one renderer", () => {
  const chat = read("../app/chat/page.tsx");
  const feedback = read("../app/feedback/page.tsx");
  const admin = read("../app/admin/page.tsx");
  for (const [name, src] of [
    ["chat/page.tsx", chat],
    ["feedback/page.tsx", feedback],
    ["admin/page.tsx", admin],
  ] as const) {
    assert.match(src, /from "\.\.\/\.\.\/components\/Markdown"/, `${name} does not import the shared renderer`);
  }
  // The original defect, pinned at the call site: the chat transcript must not
  // go back to rendering the message object's raw text as the bubble's only
  // child (the `>{m.content}<` shape that produced raw `**` on screen).
  assert.doesNotMatch(
    chat,
    />\s*\{m\.content\}\s*</,
    "chat/page.tsx renders {m.content} raw again — that is the reported defect (raw ** on screen)",
  );
  assert.match(chat, /<Markdown[\s\S]{0,200}?text=\{m\.content\}/, "the chat bubble does not render the message through Markdown");
  assert.doesNotMatch(
    feedback,
    />\s*\{m\.body\}\s*</,
    "feedback/page.tsx renders {m.body} raw again",
  );
  assert.match(feedback, /<Markdown[\s\S]{0,200}?text=\{m\.body\}/, "the feedback thread body does not render through Markdown");
  assert.doesNotMatch(admin, />\s*\{m\.body\}\s*</, "the admin triage view renders {m.body} raw again");
  assert.match(admin, /<Markdown[\s\S]{0,200}?text=\{m\.body\}/, "the admin triage body does not render through Markdown");
  // Exactly one renderer in the tree: a second copy is how the two surfaces drift.
  assert.match(feedback, /components\/Markdown/, "the feedback page does not import the shared component");
});

test("the prompt tells the model the subset the renderer supports (the renderer pins the result)", () => {
  // The UI half and the prompt half are one feature: the renderer guarantees the
  // output, the prompt stops the model from emitting what the renderer drops
  // (tables, raw HTML) so answers do not silently lose structure.
  const prompt = read("../../../api/internal/chat/prompt.go");
  assert.match(prompt, /HOW TO ANSWER/, "the prompt's answer rules moved — re-anchor this guard");
  assert.match(prompt, /\*\*bold\*\*/, "the prompt no longer states the supported formatting subset");
  assert.match(prompt, /[Nn]ever (?:use|output|write).{0,60}HTML|[Dd]o not use HTML/, "the prompt no longer forbids raw HTML");
  assert.match(prompt, /table/i, "the prompt no longer forbids tables");
  assert.match(
    prompt,
    /language the user wrote|in Malay, answer/,
    "the language-matching rule was removed while adding the formatting rule",
  );
});

test("the stored text is the raw model text — nothing is cleaned on write", () => {
  // The card requires the raw text to survive, so a renderer fix needs no
  // migration and no data is lost. If a server-side "clean-up" appears, this
  // fails and the decision gets made deliberately instead of by accident.
  const rest = read("../../../api/internal/httpapi/rest_chat.go");
  assert.match(
    rest,
    /Reply:\s*res\.Reply/,
    "the chat reply is no longer passed straight through — re-anchor this guard",
  );
  for (const pattern of [/stripMarkdown/i, /sanitizeMarkdown/i, /normalizeMarkdown/i]) {
    assert.doesNotMatch(rest, pattern, "rest_chat.go now cleans the reply before storing it");
  }
});

test("the docs page documents what is rendered, so the behaviour is discoverable", () => {
  // DOCUMENTATION PARITY (AGENTS.md): a rendered-format feature that is not
  // documented is a surprise. The chat docs carry a section for it, and so does
  // the feedback docs (thread bodies render the same way).
  const chatDocs = read("../app/docs/chat/page.tsx");
  assert.match(chatDocs, /id: "formatting"/, "the chat docs lost the formatting section");
  assert.match(chatDocs, /strong|bold/, "the formatting section no longer names what is rendered");
  const fbDocs = read("../app/docs/feedback/page.tsx");
  assert.match(fbDocs, /id: "formatting"/, "the feedback docs do not mention that a thread body is rendered");
});

test("parseMarkdown/parseInline expose the structure the assertions above rely on", () => {
  // A direct read of the parser, so a change to the block/inline split shows up
  // here rather than only as a mysterious HTML diff.
  const blocks = parseMarkdown("- satu\n- dua\n\n```\ncode\n```\n\npara");
  assert.deepEqual(blocks.map((b) => b.kind), ["list", "code", "p"]);
  const inline = parseInline("a **b** `c` [d](https://e.com)");
  assert.deepEqual(inline.map((n) => n.t), ["text", "strong", "text", "code", "text", "link"]);
});
