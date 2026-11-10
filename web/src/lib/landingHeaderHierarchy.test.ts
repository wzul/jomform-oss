// Regression guard for "the landing header's strongest control is Log in, not
// Build a form" (card t_53bcd451) — a public landing page whose new-visitor path
// ranks BELOW its returning-user path.
//
// THE DEFECT (the shipped landing screenshots at 390px and 1440px):
//
//   * the header's only filled button was `Log in` (purple fill, white label),
//     while `Build a form` was plain muted text AND `hidden … sm:block`, i.e.
//     absent entirely at 390px — so on a phone the header offered a first-time
//     visitor no way to start and put the account form in the strongest slot;
//   * the EN/BM switch measured ~20px tall: under the 24px AA pointer minimum
//     and far under the 44px touch target, and each button's only name was the
//     two-letter label ("BM"), which names neither the language nor the target;
//   * the trust bullets ("No monthly fee", …) were 12px in `--jf-muted` —
//     4.54:1 on this hero surface, i.e. scraping the AA floor while carrying
//     text the page exists to say;
//   * `/docs` was reachable from the footer only.
//
// WHAT THE FIX IS, and why each half is shaped the way it is:
//   * the fill is on the /builder anchor and on nothing else in the nav. /builder
//     IS the build path for a visitor with no session cookie: the SSR gate
//     (`src/lib/authedGuard.ts` + `src/middleware.ts`) turns a cookieless
//     /builder into /login?next=<absolute /builder URL>, and the login page
//     honours that after sign-in, so the "build a form" intent survives the
//     account step. Log in stays a plain text link.
//   * the fill must also be VISIBLE at 390px: the old anchor was
//     `hidden … sm:block`, which is how the phone header lost its only
//     build entry point.
//   * each language button is a 44px hit area (`min-h-11 min-w-11`) around an
//     unpadded-look visual pill, and its accessible name starts with the visible
//     short label and then names the language (WCAG 2.5.3 + a real name).
//   * the trust bullets are `text-sm` in `--jf-text-secondary`; the contrast
//     below is COMPUTED from the tokens in globals.css rather than asserted by
//     eye, so a token change that drops them under AA fails here.
//
// WHY A SOURCE-CONTRACT TEST AND NOT A DOM TEST: this repo has no DOM/JSX unit
// runner (see src/lib/docsApiProseWrap.test.ts). The rendered geometry was
// measured in a browser and is re-recorded in the card; what a unit test can pin
// is the SHAPE that carries the behaviour — which anchor holds the fill, which
// href it points at, the hit-area classes, the colour token — plus the token
// arithmetic. The a11y gate's axe scan measures the same colour pair on /landing
// on every run.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const LANDING = "../app/landing/page.tsx";
const GLOBALS = "../app/globals.css";

const landing = readFileSync(fileURLToPath(new URL(LANDING, import.meta.url)), "utf8");
const globals = readFileSync(fileURLToPath(new URL(GLOBALS, import.meta.url)), "utf8");

/** The primary FILL (`--jf-primary`, white label). Not a text colour. */
const FILL = /bg-\[var\(--jf-primary\)\]/;
/** The 44px hit-area utility (Tailwind `min-h-11` = 2.75rem = 44px at a 16px root). */
const HIT_44 = /\bmin-h-11\b/;

const NAV_BLOCK = landing.match(/<nav\b[^>]*>([\s\S]*?)<\/nav>/);
const nav = NAV_BLOCK?.[1] ?? "";

function classOf(tag: string) {
  return tag.match(/className="([^"]*)"/)?.[1] ?? "";
}

/** Every anchor in the nav with its href, classes and rendered label. */
function navAnchors(src: string) {
  return [...src.matchAll(/<a\b([^>]*)>([\s\S]*?)<\/a>/g)].map((m) => ({
    href: (m[1].match(/href="([^"]*)"/) ?? [])[1] ?? "",
    cls: classOf(m[0]),
    body: m[2],
    source: m[0],
  }));
}

/**
 * A `--jf-*` token's value from the LIGHT theme's `:root` block (the first
 * declaration of that name — the dark block redeclares the same names later).
 */
function token(name: string): string {
  const hit = globals.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`));
  assert.ok(hit, `${GLOBALS} declares no --${name} — the contrast below cannot be computed`);
  return hit![1];
}

/** WCAG 2.x relative luminance of `#rrggbb`. */
function luminance(hex: string): number {
  const h = hex.replace("#", "");
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG 2.x contrast ratio between two `#rrggbb` values. */
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const AA_NORMAL = 4.5;

test("the guard is looking at the real header (it cannot pass vacuously)", () => {
  assert.ok(
    NAV_BLOCK,
    `no <nav> block could be read from ${LANDING} — every assertion below is anchored on it`,
  );
  const anchors = navAnchors(nav);
  assert.ok(
    anchors.length >= 3,
    `the landing <nav> holds ${anchors.length} anchors — expected at least the Docs, Log in and ` +
      "Build a form links, so the href assertions below would be pinning nothing",
  );
  for (const href of ["/builder", "/login", "/docs"]) {
    assert.ok(
      anchors.some((a) => a.href === href),
      `the landing <nav> has no anchor pointing at ${href} — measured hrefs: ` +
        `${JSON.stringify(anchors.map((a) => a.href))}`,
    );
  }
  // The bullets and the language buttons are the other two arms; if either site
  // moves, its assertion has to be re-anchored rather than silently skipped.
  assert.match(
    landing,
    /<ul\b[^>]*>\s*\{\[t\("land\.trust1"\)/,
    `${LANDING}: no <ul> immediately holding the land.trust1..3 labels — the bullet arm would pin nothing`,
  );
  assert.match(
    landing,
    /function LangButtons\(\)/,
    `${LANDING}: the language control is no longer a LangButtons component — re-anchor the hit-area arm`,
  );
});

test("the header's one filled button is the build path, and it survives 390px", () => {
  const anchors = navAnchors(nav);
  const filled = anchors.filter((a) => FILL.test(a.cls));

  assert.equal(
    filled.length,
    1,
    `the landing <nav> renders ${filled.length} filled (FILL) anchors — expected exactly one. ` +
      `Filled hrefs: ${JSON.stringify(filled.map((a) => a.href))}. A second filled control beside the ` +
      "build path is how Log in took the strongest slot in the first place (card t_53bcd451)",
  );
  assert.equal(
    filled[0].href,
    "/builder",
    `the header's filled primary points at ${filled[0].href}, not /builder. For a visitor with no ` +
      "session cookie /builder is redirected by the SSR gate to /login?next=…/builder, which is what " +
      "makes it the BUILD path; a filled /login sends a first-time visitor to the account form instead " +
      "(the defect this card reports)",
  );
  assert.match(
    filled[0].body,
    /land\.buildForm/,
    `the filled anchor renders ${JSON.stringify(filled[0].body.trim())}, not land.buildForm — its label is ` +
      "what tells a new visitor this control starts a form",
  );
  // The old anchor was `hidden … sm:block`, so the 390px header had no build entry
  // point at all. A `hidden`-scoped primary is the same defect in a new shape.
  assert.doesNotMatch(
    filled[0].cls,
    /\bhidden\b/,
    `the filled /builder anchor carries "hidden" (className "${filled[0].cls}") — the phone header ` +
      "then offers no way to start building, which is exactly the 390px measurement this card is about",
  );

  const login = anchors.find((a) => a.href === "/login");
  assert.ok(login, "the landing <nav> lost its /login anchor — the returning-user path must stay reachable");
  assert.doesNotMatch(
    login!.cls,
    FILL,
    `the nav's Log in anchor is filled (className "${login!.cls}") — the returning-user shortcut must ` +
      "stay a text link so the build path outranks it",
  );
});

test("Docs is reachable from the header, not only the footer", () => {
  const docs = navAnchors(nav).find((a) => a.href === "/docs");
  assert.ok(docs, "the landing <nav> has no /docs link");
  assert.doesNotMatch(
    docs!.cls,
    FILL,
    `the Docs link is filled (className "${docs!.cls}") — it is a reference link, not the page's action`,
  );
});

test("each language button is a 44px target whose name includes the language", () => {
  const from = landing.indexOf("function LangButtons()");
  const block = landing.slice(from, from + 2500); // the component is ~40 lines

  // One button TEMPLATE inside a `.map()` — so the count to pin is the locale
  // list it renders over, not the number of `<button` literals in the source.
  assert.match(
    block,
    /\[\s*"en"\s*,\s*"ms"\s*\]/,
    "LangButtons no longer maps over the en/ms pair — the hit-area arm below would pin an unknown control",
  );
  // The button template, read from `<button` through the `>` of its className
  // attribute: a `[^>]*` scan stops at the `>` inside `onClick={() => …}`, so
  // every attribute after it would be invisible to the assertions below.
  const start = block.indexOf("<button");
  const clsAt = block.indexOf('className="', start);
  const end = block.indexOf(">", clsAt);
  assert.ok(start !== -1 && clsAt !== -1 && end !== -1, "could not read the <button> tag out of LangButtons");
  const buttons = [block.slice(start, end + 1)];
  assert.equal(
    buttons.length,
    1,
    `LangButtons holds ${buttons.length} <button> literals — expected one template inside its .map()`,
  );
  for (const tag of buttons) {
    const cls = classOf(tag);
    assert.match(
      cls,
      HIT_44,
      `a language button has className "${cls}" with no min-h-11 — it measured ~20px tall, under the ` +
        "24px AA pointer minimum and far under the 44px touch target (card t_53bcd451)",
    );
    assert.match(
      tag,
      /type="button"/,
      `a language button has no type="button" (${tag}) — inside a form it would submit instead of switching`,
    );
    assert.match(
      tag,
      /aria-pressed=\{lang === l\}/,
      `a language button does not expose its selected state (${tag}) — the filled pill is the only ` +
        "signal a screen reader would otherwise get",
    );
    assert.match(
      tag,
      /aria-label=\{`[^`]*LANG_SHORT\[l\][^`]*LANG_LABEL\[l\][^`]*`\}/,
      `a language button's accessible name is not "<short label> — <language name>" (${tag}); it read ` +
        '"BM" alone, which names neither the language nor the target of the press. It must also OPEN ' +
        "with the visible short label (WCAG 2.5.3, label in name)",
    );
  }
});

test("the trust bullets are text-sm and measurably past AA on the hero surface", () => {
  const list = landing.match(/<ul\b([^>]*)>\s*\{\[t\("land\.trust1"\)/);
  assert.ok(list, `${LANDING}: the trust-bullet <ul> could not be read`);
  const cls = classOf(list![1]);

  assert.match(
    cls,
    /(^|\s)text-sm(\s|$)/,
    `the trust bullets are "${cls}" — they were text-xs (12px) muted text, i.e. the reassurance the ` +
      "page exists to give, rendered as the smallest thing on it",
  );
  assert.doesNotMatch(cls, /(^|\s)text-xs(\s|$)/, `the trust bullets are still text-xs ("${cls}")`);
  assert.match(
    cls,
    /text-\[var\(--jf-text-secondary\)\]/,
    `the trust bullets do not use --jf-text-secondary ("${cls}") — that is the token whose measured ` +
      `contrast (${contrast(token("jf-text-secondary"), token("jf-bg")).toFixed(2)}:1 on --jf-bg) clears AA`,
  );

  // The measurement, not the claim: computed from the tokens this page actually
  // paints with. globals.css is the only place a literal may live.
  const surface = token("jf-bg");
  const body = contrast(token("jf-text-secondary"), surface);
  const muted = contrast(token("jf-muted"), surface);
  assert.ok(
    body >= AA_NORMAL,
    `--jf-text-secondary (${token("jf-text-secondary")}) measures ${body.toFixed(2)}:1 on --jf-bg ` +
      `(${surface}), under the ${AA_NORMAL}:1 AA floor for normal text — the bullets cannot be read`,
  );
  // Why the token moved: the old pair was on the floor, not past it.
  assert.ok(
    muted < body,
    `--jf-muted (${muted.toFixed(2)}:1) is no longer weaker than --jf-text-secondary ` +
      `(${body.toFixed(2)}:1) — if the tokens were consolidated, re-derive this arm instead of deleting it`,
  );
});
