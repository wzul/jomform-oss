// The document DECLARATION must describe the PROSE the page actually contains
// (card t_c9d23e2c, WCAG 3.1.1 Language of Page).
//
// THE DEFECT, measured live on jomform.com (v1.18.0, fresh browser state) by
// card t_6c4ccef4: `components/i18n.tsx` wrote the visitor's DASHBOARD locale to
// `document.documentElement` on every route —
//
//     document.documentElement.dir  = lang === "ar" ? "rtl" : "ltr";
//     document.documentElement.lang = lang;
//
// — and the knowledge base is English-only by design (`app/docs/docsChrome.tsx`:
// "The docs PROSE stays English: every /docs guide page is static English, and
// half-translating the knowledge base would be worse than not translating it";
// only the index's chrome LABELS are translated). So with `jf_lang=ar`,
// /docs/embed read `lang="ar"` / `dir="rtl"` with the h1 "Embed a form on your
// site", every paragraph and every nav label in ENGLISH, laid out
// right-to-left. /legal/privacy read the same. `jf_lang` is a real user setting
// (the /profile "Bahasa dashboard" select persists it), so it was reachable by
// an Arabic-preferring visitor reading a guide.
//
// WHY A BROWSER SPEC. `src/lib/docsA11y.test.ts` pins the rule and the
// provider's wiring from SOURCE — that the rule matches every /docs and /legal
// route on disk, and that the provider gates the write on it. Only a browser can
// show that the SERVED page really carries the declaration, after hydration, in
// both states this card is about.
//
// WHAT MAKES IT BITE — every state is REACHABLE before it is judged, because a
// page that rendered nothing passes every declaration assertion for free:
//   * the `dir` attribute is written by the provider's effect ONLY, after
//     hydration — `app/layout.tsx` sends `<html lang="en">` with NO dir. So
//     asserting dir is also the HYDRATION WITNESS: a page that never hydrated
//     has no dir attribute and FAILS here instead of passing on the
//     server-rendered lang.
//   * with `jf_lang=ar` on /docs the h1 must be the TRANSLATED label
//     (docs.title, Arabic), which proves the locale really was applied and the
//     page really hydrated — the assertions cannot pass because the app never
//     read `jf_lang` at all.
//   * and on the same page the English PROSE must be present (the guide's h1 and
//     an h2), so the English declaration is claimed over English text rather
//     than over a page that rendered nothing.
//   * the pre-fix reading is asserted backwards: on the build card t_6c4ccef4
//     measured, every `lang="en"` / `dir="ltr"` assertion below on a prose route
//     with `jf_lang=ar` is RED (the page carried ar / rtl).
//
// The API is stubbed at the browser level (there is no Go server in this gate —
// see playwright.config.ts), so the profile read answers "no language set" and
// the browser value is the one in effect, exactly as for a signed-out visitor.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { OPERATOR_KEY, operatorParts } from "../src/lib/operator";

// The three English-only prose trees, as the routes a reader really lands on.
// /docs/embed and /legal/privacy are the two the card measured; /docs (the
// index, whose chrome LABELS are translated but whose prose is English) and
// /legal/terms are the rest of that shape; /brand is the public brand-assets
// page card t_4d3691b9 measured declaring the visitor's locale over English
// prose. The second field is the English text that must be ON the page before
// its declaration is judged — on /docs it is the translated-in-arabic chrome
// label's ENGLISH value, which only appears once the page has hydrated.
const PROSE_ROUTES: { path: string; english: string }[] = [
  { path: "/docs", english: DICT["docs.title"].en },
  { path: "/docs/embed", english: "Embed a form on your site" },
  { path: "/legal/privacy", english: "Privacy Policy" },
  { path: "/legal/terms", english: "Terms of Service" },
  { path: "/brand", english: "Brand assets" },
];

// A translated surface that needs no session: the landing page the docs tree
// links back to. It is the control for "do not simply drop the dir write".
const TRANSLATED_ROUTES = ["/landing", "/mcp"];

/** A FRESH browser state, with the language this case means (or none at all). */
async function freshState(page: Page, lang: "ar" | "zh" | "en" | null) {
  await page.addInitScript((l) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      if (l) localStorage.setItem("jf_lang", l);
    } catch {
      /* storage unavailable */
    }
  }, lang);
  await page.context().clearCookies();
  // No Go server here: the profile read answers 401 and the browser value stays
  // in effect — the same path a signed-out visitor takes.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill({ status: 401, contentType: "application/json", body: "{}" }));
}

/** The declaration, read out of the live DOM (attribute AND computed style). */
async function declaration(page: Page): Promise<{ lang: string | null; dir: string | null; direction: string }> {
  return page.evaluate(() => ({
    lang: document.documentElement.getAttribute("lang"),
    dir: document.documentElement.getAttribute("dir"),
    direction: getComputedStyle(document.documentElement).direction,
  }));
}

/** Asserts the page IS the English-only prose surface, then its declaration. */
async function expectEnglishProse(page: Page, path: string, englishText: string | RegExp) {
  await expect(
    page.getByText(englishText).first(),
    `${path} must render its English prose — a declaration assertion over a blank page proves nothing`,
  ).toBeVisible({ timeout: 30_000 });

  // `dir` is written only by the provider's effect (after hydration); the
  // document sent by the server has no dir attribute. Asserting it is what
  // proves the effect ran AND what it wrote.
  await expect(
    page.locator("html"),
    `${path} must declare lang="en" — its prose is English-only (WCAG 3.1.1)`,
  ).toHaveAttribute("lang", "en");
  await expect(
    page.locator("html"),
    `${path} must render LTR — the text on it is English`,
  ).toHaveAttribute("dir", "ltr");
  const live = await declaration(page);
  expect(live, `${path} live declaration`).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
}

test.describe("the document declaration matches the prose (WCAG 3.1.1)", () => {
  test("a fresh visitor gets lang=en / dir=ltr on every English-only prose page", async ({ page }) => {
    await freshState(page, null);

    for (const { path, english } of PROSE_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });
      expect(
        await page.evaluate(() => localStorage.getItem("jf_lang")),
        `${path}: this case is the DEFAULT state — no jf_lang may be in storage`,
      ).toBe(null);
      await expectEnglishProse(page, path, english);
    }

    // The same state on a translated surface: the default is English there too.
    for (const path of TRANSLATED_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });
      await expect(page.locator("html")).toHaveAttribute("lang", "en");
      await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
    }
  });

  test("with jf_lang=ar an English-only prose page still declares English and stays LTR", async ({ page }) => {
    await freshState(page, "ar");

    // The two pages the card measured, judged on their ENGLISH prose.
    await page.goto("/docs/embed", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/docs/embed", "Embed a form on your site");
    await expect(
      // Role-scoped: the /docs chrome now lists every section heading in its "On this page"
      // navigation (twice: sidebar and mobile), so a bare text query resolves those links
      // first — 13 matches, the first one scrolled out of the sticky rail. This assertion is
      // about the PAGE HEADING rendering in English, so it asks for the heading.
      page.getByRole("heading", { name: "Private forms and cookies" }),
      "/docs/embed renders this English section heading in every locale",
    ).toBeVisible();

    await page.goto("/legal/privacy", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/legal/privacy", "Privacy Policy");

    await page.goto("/legal/terms", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/legal/terms", "Terms of Service");

    // The brand-assets page (card t_4d3691b9): its prose is English and its
    // headings stay English in every locale, so the document must be declared
    // English even though the reader's dashboard locale is Arabic. `dir` is
    // written only by the provider's effect after hydration, so asserting it is
    // also what proves this page really ran the effect.
    await page.goto("/brand", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/brand", "Brand assets");
    await expect(
      page.getByRole("heading", { name: "Logo mark" }),
      "/brand renders its English section headings in every locale",
    ).toBeVisible();

    // The index is the interesting one: its chrome LABELS really are translated,
    // so the head proves the locale WAS applied (and the page hydrated) while
    // the document is still declared English over the English prose.
    await page.goto("/docs", { waitUntil: "networkidle" });
    await expect(
      page.getByRole("heading", { name: DICT["docs.title"].ar }),
      "/docs renders its translated chrome label in Arabic with jf_lang=ar — without this the " +
        "declaration assertions below could pass because the locale was never applied at all",
    ).toBeVisible({ timeout: 30_000 });
    await expect(
      page.getByRole("link", { name: "Overview & features" }),
      "/docs/… sidebar is static English prose, whatever the dashboard locale is",
    ).toBeVisible();
    await expect(page.locator("html"), "/docs prose is English — the declaration must say so").toHaveAttribute(
      "lang",
      "en",
    );
    await expect(page.locator("html"), "/docs must not be mirrored for an English page").toHaveAttribute("dir", "ltr");
    expect(await declaration(page)).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
  });

  test("with jf_lang=ar a genuinely translated surface still declares Arabic and renders RTL", async ({ page }) => {
    await freshState(page, "ar");

    for (const path of TRANSLATED_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });
      // Reachability again: the page must render, and on /landing it must render
      // its ARABIC hero — the control that says the locale is live.
      if (path === "/landing") {
        await expect(page.getByText(DICT["land.hero1"].ar).first()).toBeVisible({ timeout: 30_000 });
      }
      await expect(
        page.locator("html"),
        `${path} is a translated surface: an Arabic visitor gets lang="ar"`,
      ).toHaveAttribute("lang", "ar");
      await expect(
        page.locator("html"),
        `${path} must render RTL — this is the direction the fix must NOT drop`,
      ).toHaveAttribute("dir", "rtl");
      expect((await declaration(page)).direction, `${path} computed direction`).toBe("rtl");
    }
  });

  test("the declaration follows a client-side navigation out of the docs tree", async ({ page }) => {
    await freshState(page, "ar");

    await page.goto("/docs", { waitUntil: "networkidle" });
    await expect(page.locator("html")).toHaveAttribute("dir", "ltr");

    // A real click path a reader can run: the docs chrome's own brand link. The
    // route changes and the language does NOT, so an implementation that read
    // the path once on mount leaves the next page declared English.
    //
    // Card t_ff973d0b replaced the plain "← Back to JomForm" text link with the
    // branded lockup (JF tile + wordmark) that also reaches `/`. The link carries
    // no aria-label — its accessible name is its own text, "JomForm Docs", so a
    // speech-input label can never disagree with what is on screen at any width.
    // The path this case walks — click the docs chrome's own way out, land on `/`,
    // and watch the declaration follow — is unchanged; only the control moved.
    await page.getByRole("link", { name: /JomForm Docs/ }).click();
    await expect.poll(() => new URL(page.url()).pathname, { timeout: 30_000 }).toBe("/");

    await expect(page.getByText(DICT["land.hero1"].ar).first()).toBeVisible({ timeout: 30_000 });
    await expect(
      page.locator("html"),
      "after navigating to a translated page the declaration must move back to the visitor's locale",
    ).toHaveAttribute("dir", "rtl");
    expect((await declaration(page)).direction).toBe("rtl");
  });
});

// ══ a translated label declares its own language (card t_0e2d71f4) ══════════
//
// WCAG 3.1.2, LANGUAGE OF PARTS — the sibling of everything above. Those cases
// judge the PAGE's declaration; these judge the PARTS of an English page that are
// written in another language.
//
// THE DEFECT, measured live on prod v1.21.119 (revision 8062ca65) with
// `jf_lang=ar` (card t_86a8aabd): /docs correctly declared the document
// `lang="en"` — its prose is English — while 46 of the elements on it contained
// Arabic and 0 of them declared a language of their own. So the translated chrome
// ("وثائق JomForm", the section headings, the doc-link buttons) was a language
// CHANGE inside an English document with nothing announcing it, and a screen
// reader pronounces it with an English voice. Only /docs is affected: every other
// prose route renders 0 Arabic elements, and its chrome is static English.
//
// SCOPE: this sweep runs on /docs because the /docs index is where this card's
// fix lives (its chrome is the only translated one). /legal is judged by the
// sibling block BELOW — card t_b13ee0cc — which fixes the one thing on those two
// pages that was rendered in the visitor's locale inside a document PR 1201 pins
// to English: the operator sentence. Pointing THIS sweep at /legal without that
// fix reds on that single component (measured: 2 elements per page, both the
// footer sentence), which is what made the defect visible in the first place.
//
// WHY A DOM ASSERTION AND NOT AN axe PAGE. axe does not flag this class at all —
// it has no rule for "text in another language inside a correctly declared
// document" — so the only proof is reading the served DOM. The source half of
// this guard is `src/lib/docsA11y.test.ts`.
//
// WHAT MAKES IT BITE. The sweep takes every element whose OWN text (its direct
// text nodes — an ancestor's innerText contains the label too, and the language
// of a part is decided by the NEAREST declaration) contains Arabic, and requires
// that element to resolve, through its own or an ancestor's `lang`, to Arabic.
// Before the fix every one of them resolved to the document's "en", so the sweep
// is RED on the pre-fix build. The non-vacuity count below is what stops this
// passing for free on a page that rendered no Arabic at all.
/** Every element whose OWN text — or image ALT — is Arabic but whose nearest `lang`
 *  is not Arabic.
 *
 *  Non-rendered containers are skipped deliberately: Next's RSC payload lives in
 *  `<script>` tags inside <body>, and a static English page that merely MENTIONS
 *  Arabic (the language-name lists in the guides) would otherwise be reported as
 *  a passage with an undeclared language.
 *
 *  AN IMAGE'S ALT IS TEXT TOO (card t_e35a44f5). It is spoken by a screen reader
 *  but it is an ATTRIBUTE, so `body *` own-text reading above cannot see it: /brand
 *  carried `alt="شعار JomForm"` inside a document declared English with NOTHING
 *  declaring Arabic, and this sweep was green. An alt is judged exactly like own
 *  text — the nearest declaration (the image's own `lang` first) must be Arabic. */
async function mislabelledArabic(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const arabic = /[\u0600-\u06FF]/;
    const out: string[] = [];
    const nearestLang = (el: Element) =>
      (el.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase();
    for (const el of Array.from(document.querySelectorAll<HTMLElement>("body *"))) {
      if (["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"].includes(el.tagName)) continue;
      const own = Array.from(el.childNodes)
        .filter((n) => n.nodeType === 3)
        .map((n) => n.textContent ?? "")
        .join("")
        .trim();
      if (!own || !arabic.test(own)) continue;
      const declared = nearestLang(el);
      if (declared === "ar" || declared.startsWith("ar-")) continue;
      // The NEAREST declaration is what announces this text — printed with it, so a
      // failure names both the passage and the language it is being read in.
      out.push(
        `<${el.tagName.toLowerCase()}> ${JSON.stringify(own.slice(0, 40))} — nearest lang=${declared ? `"${declared}"` : "(none)"}`,
      );
    }
    for (const img of Array.from(document.querySelectorAll<HTMLImageElement>("img[alt]"))) {
      const alt = (img.getAttribute("alt") ?? "").trim();
      if (!alt || !arabic.test(alt)) continue;
      const declared = nearestLang(img);
      if (declared === "ar" || declared.startsWith("ar-")) continue;
      const src = (img.getAttribute("src") ?? "").slice(0, 40);
      out.push(
        `<img alt=${JSON.stringify(alt.slice(0, 40))} src=${JSON.stringify(src)}> — nearest lang=${declared ? `"${declared}"` : "(none)"}`,
      );
    }
    return out;
  });
}

/** How many elements carry Arabic in their OWN text — the non-vacuity count. */
async function arabicOwnText(page: Page): Promise<number> {
  return page.evaluate(() => {
    const arabic = /[\u0600-\u06FF]/;
    return Array.from(document.querySelectorAll<HTMLElement>("body *")).filter(
      (el) =>
        !["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"].includes(el.tagName) &&
        Array.from(el.childNodes).some((n) => n.nodeType === 3 && arabic.test(n.textContent ?? "")),
    ).length;
  });
}

/** Every INNERMOST element whose text is exactly `text`, and its own `lang`. */
async function labelsOf(page: Page, text: string): Promise<{ tag: string; lang: string | null }[]> {
  return page.evaluate(
    (t) =>
      Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .filter((e) => e.childElementCount === 0 && (e.textContent ?? "").trim() === t)
        .map((e) => ({ tag: e.tagName.toLowerCase(), lang: e.getAttribute("lang") })),
    text,
  );
}

test.describe("a translated label declares its own language (WCAG 3.1.2)", () => {
  test("with jf_lang=ar every Arabic label on /docs declares Arabic, and the page stays English", async ({
    page,
  }) => {
    await freshState(page, "ar");
    await page.goto("/docs", { waitUntil: "networkidle" });

    // Reachability first: the locale really was applied, so there IS Arabic on the
    // page to judge. Without this the sweep could pass because nothing rendered.
    await expect(
      page.getByRole("heading", { name: DICT["docs.title"].ar }),
      "this case is only meaningful on a page that really rendered Arabic",
    ).toBeVisible({ timeout: 30_000 });
    expect(
      await arabicOwnText(page),
      "/docs rendered no element with Arabic in its own text, so the rule below would prove nothing",
    ).toBeGreaterThanOrEqual(8);

    const wrong = await mislabelledArabic(page);
    expect(
      wrong,
      `these elements hold Arabic text inside the English /docs document but nothing declares Arabic — ` +
        `WCAG 3.1.2 (Language of Parts), a screen reader reads them with an English voice:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);

    // The heading the card measured, read straight out of the live DOM: the
    // element carrying the Arabic label declares Arabic itself.
    const heading = await labelsOf(page, DICT["docs.title"].ar);
    expect(heading, "/docs did not render the Arabic heading card t_0e2d71f4 measured").not.toEqual([]);
    expect(heading.map((l) => l.lang), "the Arabic label must declare Arabic on the element that holds it").toEqual(
      heading.map(() => "ar"),
    );

    // …and the document declaration this fix must NOT have moved (the fix is a
    // language of PARTS; the page is still English and still LTR).
    expect(await declaration(page)).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
  });

  for (const lang of ["en", null] as const) {
    test(`with the locale ${lang ?? "unset (a fresh visitor)"} a label adds no language at all`, async ({ page }) => {
      await freshState(page, lang);
      await page.goto("/docs", { waitUntil: "networkidle" });

      await expect(page.getByRole("heading", { name: DICT["docs.title"].en })).toBeVisible({ timeout: 30_000 });
      expect(await arabicOwnText(page), `/docs rendered Arabic in its own text with jf_lang=${lang}`).toBe(0);
      expect(await mislabelledArabic(page)).toEqual([]);

      // The English label is the document's own language, so it declares nothing:
      // an element carrying lang= here would be a declaration the page does not need
      // (and, for a locale the reader did not pick, a wrong one).
      const heading = await labelsOf(page, DICT["docs.title"].en);
      expect(heading.length, "/docs did not render its English heading").toBeGreaterThan(0);
      expect(
        heading.map((l) => l.lang),
        "an English label must add no lang of its own — the document already declares English",
      ).toEqual(heading.map(() => null));
    });
  }
});

// ══ a localised IMAGE ALT declares its own language (card t_e35a44f5) ═══════
//
// THE DEFECT, measured on a production build of PR #1604's tree with
// `jf_lang=ar` and a fresh browser state:
//
//   document.documentElement.lang  → "en"       (this page's prose IS English)
//   <img alt="شعار JomForm">       → no lang     nearest [lang] → "en"
//   <img alt="لافتة JomForm">      → no lang     nearest [lang] → "en"
//
// PR #1602 turned /brand's two ALTs into dictionary lookups (`brand.logoAlt`,
// `brand.bannerAlt`, five locales) and PR #1604 declared the page English. Put
// together, an Arabic-preferring reader gets Arabic ALTs inside an English
// document with nothing declaring Arabic, so a screen reader speaks them with an
// English voice — WCAG 3.1.2 (Language of Parts), the class the /docs chrome
// labels above were fixed for.
//
// WHY THESE ARMS EXIST ON TOP OF THE SWEEP. `mislabelledArabic` reads an
// element's OWN TEXT and an alt is an ATTRIBUTE: the sweep was GREEN on this very
// page while every alt was Arabic and undeclared. It now reads `img[alt]` too (so
// every other page it judges gets the same rule for free); the arms below judge
// /brand in both directions and replay the pre-fix DOM live.
//
// The fix is `ownLangProps(lang)` spread onto each image: the RESOLVED locale
// from `useI18n()`, and `{}` for English — the document's own language adds
// nothing. Its source half is `src/lib/ownLang.test.ts`.

/** /brand's two images: the alt a screen reader speaks, and the language it is read in. */
async function brandImages(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLImageElement>("main img")).map((i) => ({
      alt: i.getAttribute("alt") ?? "",
      lang: i.getAttribute("lang"),
      nearest: (i.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase(),
    })),
  );
}

test.describe("a localised image alt declares its own language (WCAG 3.1.2)", () => {
  test("with jf_lang=ar /brand's Arabic alts declare Arabic inside the English document", async ({
    page,
  }) => {
    await freshState(page, "ar");
    await page.goto("/brand", { waitUntil: "networkidle" });

    // The document must be the ENGLISH one: without PR #1604's declaration the
    // alt's nearest language is Arabic already and this arm proves nothing.
    await expectEnglishProse(page, "/brand", "Brand assets");

    // Reachability: the locale really reached the page — both ALTs are the
    // Arabic dictionary values, so there IS another language to declare here.
    const imgs = await brandImages(page);
    expect(
      imgs.map((i) => i.alt),
      "/brand did not render the Arabic image alts PR #1602 added — this case would pass for free",
    ).toEqual([DICT["brand.logoAlt"].ar, DICT["brand.bannerAlt"].ar]);

    // The whole page first, so a revert fails naming the IMAGE (alt + src) and the
    // language it is being read in, not just an attribute count.
    const wrong = await mislabelledArabic(page);
    expect(
      wrong,
      `these passages on /brand are in Arabic inside the English document but nothing declares Arabic — ` +
        `WCAG 3.1.2 (Language of Parts), a screen reader reads them with an English voice:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);

    // …then the fix itself, per element: the declaration is on the element that
    // holds the text. Named by its ALT (the only human-meaningful handle these
    // images have — Next rewrites the logo's `src` to /_next/image).
    expect(
      imgs.map((i) => ({ alt: i.alt, lang: i.lang, nearest: i.nearest })),
      "each /brand image must declare the language of its own alt — WCAG 3.1.2",
    ).toEqual([
      { alt: DICT["brand.logoAlt"].ar, lang: "ar", nearest: "ar" },
      { alt: DICT["brand.bannerAlt"].ar, lang: "ar", nearest: "ar" },
    ]);

    // …and the document declaration this fix must NOT have moved: it is a
    // language of PARTS, the page is still English and still LTR.
    expect(await declaration(page)).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
  });

  test("in English /brand's alts add no language at all", async ({ page }) => {
    for (const lang of ["en", null] as const) {
      await freshState(page, lang);
      await page.goto("/brand", { waitUntil: "networkidle" });
      await expectEnglishProse(page, "/brand", "Brand assets");

      const imgs = await brandImages(page);
      expect(
        imgs.map((i) => i.alt),
        `/brand did not render its English alts with jf_lang=${lang}`,
      ).toEqual([DICT["brand.logoAlt"].en, DICT["brand.bannerAlt"].en]);
      expect(
        imgs.map((i) => i.lang),
        `an English alt must add no lang of its own with jf_lang=${lang} — the document already declares English`,
      ).toEqual([null, null]);
    }
  });

  test("the sweep names an image whose alt has no declaration (the pre-fix shape)", async ({
    page,
  }) => {
    await freshState(page, "ar");
    await page.goto("/brand", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/brand", "Brand assets");

    // Replay the pre-fix DOM on the SHIPPED page: strip the declaration the fix
    // adds. If the sweep cannot see an alt, this arm passes and the one above is
    // the only thing that would ever go red.
    const stripped = await page.evaluate(() => {
      const imgs = Array.from(document.querySelectorAll<HTMLImageElement>("main img"));
      for (const i of imgs) i.removeAttribute("lang");
      return imgs.length;
    });
    expect(stripped, "/brand did not render its two images").toBe(2);

    const wrong = await mislabelledArabic(page);
    expect(wrong.length, `the alt sweep did not report the stripped images:\n  ${wrong.join("\n  ")}`).toBe(2);
    expect(wrong.join("\n")).toContain("<img alt=");
    expect(wrong.join("\n")).toContain('nearest lang="en"');
  });
});

// ══ the operator sentence declares its own language (card t_b13ee0cc) ══════
//
// The same class as the /docs chrome labels above, on the two pages that sweep
// deliberately left out: /legal/privacy and /legal/terms.
//
// THE DEFECT, measured on a local production build of main with `jf_lang=ar` and
// filed from card t_0e2d71f4 (which found it and could not fix it there): both
// pages read `lang="en"` / `dir="ltr"` — PR 1201 pins the legal prose to English,
// correctly — while their footer carried the operator sentence in ARABIC,
// swapped in by the client after mount (`components/OperatorNotice.tsx` reads the
// visitor's locale from `useI18n()` on purpose, so all three surfaces follow it).
// Nothing declared that sentence's language: 2 elements per page held Arabic and
// resolved, through the nearest `lang`, to the document's "en". WCAG 3.1.2
// (Language of Parts), and a screen reader reads "يُدار JomForm …" with an
// English voice.
//
// WHY THE PAGE LOOKS CLEAN BEFORE HYDRATION: the legal pages are static server
// components, so the server render is the English sentence and the provider
// swaps it on mount. A reading taken before hydration sees 0 Arabic elements and
// certifies the defect as absent — which is exactly how the premise in
// t_0e2d71f4's body ("/legal/privacy, /legal/terms: 0 Arabic elements") was
// formed. Every case below therefore waits for the ARABIC SENTENCE ITSELF, which
// is also the hydration witness.
//
// WHAT MAKES IT BITE. The `<p>` holding the passage must carry `lang` equal to
// the RESOLVED locale. Three arms, because each one is the only thing that
// catches a different wrong implementation:
//   * jf_lang=ar — the reported defect: RED before the fix (the sweep lists the
//     <p> and the <bdi> inside it, both nearest lang="en").
//   * jf_lang=zh — a NON-Arabic locale: an implementation that hard-codes
//     `lang="ar"` (the tempting shortcut, since the defect was reported in
//     Arabic) passes the ar arm and fails here.
//   * jf_lang=en and unset — English is the document's own language, so the
//     sentence must add NO lang at all: an unconditional `lang={lang}` passes
//     both arms above and fails here.
//
// The document declaration is asserted in every arm too, so a "fix" that moved
// the PAGE instead of the PART (PR 1201's `lang="en"` on /legal) is refused.
const OPERATOR_SENTENCE_ROUTES = ["/legal/privacy", "/legal/terms"] as const;

/** The operator sentence a surface renders for `lang`, assembled exactly as the
 *  component does (`lead.trim()` + " " + the isolated registration clause).
 *  Derived from the production splitter and the production dictionary, so the
 *  expectation cannot drift from the thing under test. */
function operatorSentence(lang: "en" | "ms" | "zh" | "ta" | "ar"): string {
  const { lead, regClause } = operatorParts(DICT[OPERATOR_KEY][lang]);
  return `${lead.trim()} ${regClause}`;
}

/** The `<p>` elements whose whole text IS the operator sentence, with the
 *  language declaration that governs them — the attribute on the element itself
 *  and the nearest declaration a screen reader would use. */
async function operatorBlocks(page: Page, text: string) {
  return page.evaluate((sentence) => {
    return Array.from(document.querySelectorAll("p"))
      .filter((p) => (p.textContent ?? "").trim() === sentence)
      .map((p) => ({
        lang: p.getAttribute("lang"),
        nearest: (p.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase(),
      }));
  }, text);
}

test.describe("the operator sentence declares its own language (WCAG 3.1.2)", () => {
  test("with jf_lang=ar both legal pages declare Arabic on the sentence, and stay English pages", async ({
    page,
  }) => {
    await freshState(page, "ar");
    const sentence = operatorSentence("ar");

    for (const path of OPERATOR_SENTENCE_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });

      // Reachability: the ARABIC sentence is on the page. Before hydration the
      // server render is the English one, so this wait is also the proof that
      // the locale was applied at all — a page that never hydrated fails here
      // instead of passing the language assertions on an English sentence.
      await expect
        .poll(async () => (await operatorBlocks(page, sentence)).length, {
          timeout: 30_000,
          message:
            `${path} never rendered the Arabic operator sentence card t_b13ee0cc measured — ` +
            `the language assertions below would be judging a page that rendered nothing`,
        })
        .toBe(1);

      const [block] = await operatorBlocks(page, sentence);
      // The whole page, not just the footer: any Arabic element left undeclared
      // on this English page is the same defect. Non-vacuity: at least the <p>
      // and the <bdi> inside it carry Arabic of their own. Asserted BEFORE the
      // single-element check so a failure names every offending element.
      expect(
        await arabicOwnText(page),
        `${path} rendered no element with Arabic in its own text — the sweep below would prove nothing`,
      ).toBeGreaterThanOrEqual(2);
      const wrong = await mislabelledArabic(page);
      expect(
        wrong,
        `${path}: Arabic inside the English-declared legal page with nothing declaring it — ` +
          `WCAG 3.1.2 (Language of Parts):\n  ${wrong.join("\n  ")}`,
      ).toEqual([]);

      expect(
        block.lang,
        `${path}: the element that HOLDS the Arabic sentence must declare Arabic — inside the ` +
          `English-declared legal page an undeclared Arabic sentence is a language change a screen ` +
          `reader reads with an English voice (WCAG 3.1.2)`,
      ).toBe("ar");
      expect(block.nearest, `${path}: the nearest declaration a screen reader finds must be Arabic`).toBe("ar");

      // The fix is a language of PARTS: PR 1201's document declaration must not
      // have moved. The prose on this page is English and stays English.
      expect(await declaration(page), `${path} live declaration`).toEqual({
        lang: "en",
        dir: "ltr",
        direction: "ltr",
      });
    }
  });

  test("with jf_lang=zh the same sentence declares Chinese — the language is resolved, not hard-coded", async ({
    page,
  }) => {
    await freshState(page, "zh");
    const sentence = operatorSentence("zh");

    await page.goto("/legal/privacy", { waitUntil: "networkidle" });
    await expect
      .poll(async () => (await operatorBlocks(page, sentence)).length, {
        timeout: 30_000,
        message: "/legal/privacy never rendered the Chinese operator sentence",
      })
      .toBe(1);

    const [block] = await operatorBlocks(page, sentence);
    expect(
      block.lang,
      "the sentence's language is the RESOLVED locale: an implementation that hard-codes lang=\"ar\" " +
        "passes the Arabic arm above (the defect was reported in Arabic) and declares a Chinese " +
        "sentence as Arabic here — which is worse than declaring nothing",
    ).toBe("zh");
    // And this page is not an Arabic page: nothing Arabic is rendered at all, so
    // the Arabic sweep must not be the only arm this fix is judged by.
    expect(await arabicOwnText(page), "/legal/privacy rendered Arabic with jf_lang=zh").toBe(0);
    expect(await declaration(page)).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
  });

  for (const lang of ["en", null] as const) {
    test(`with the locale ${lang ?? "unset (a fresh visitor)"} the sentence declares no language at all`, async ({
      page,
    }) => {
      await freshState(page, lang);
      const sentence = operatorSentence("en");

      await page.goto("/legal/privacy", { waitUntil: "networkidle" });
      // The ENGLISH sentence is the server render, so this wait is satisfied by
      // the page as sent. The `lang` assertion below is what proves the provider
      // ran (it writes dir on <html>, asserted next).
      await expect
        .poll(async () => (await operatorBlocks(page, sentence)).length, {
          timeout: 30_000,
          message: "/legal/privacy did not render the English operator sentence",
        })
        .toBe(1);

      const [block] = await operatorBlocks(page, sentence);
      expect(
        block.lang,
        "an English sentence must add no lang of its own — English is the document's own language " +
          "here, and an unconditional lang={lang} would sprinkle a redundant declaration over the " +
          "default state every crawler and every new visitor sees",
      ).toBe(null);
      expect(await arabicOwnText(page), `/legal/privacy rendered Arabic with jf_lang=${lang}`).toBe(0);
      expect(await mislabelledArabic(page)).toEqual([]);
      // Hydration witness: `dir` is written by the provider's effect ONLY (the
      // server sends `<html lang="en">` with no dir), so a page that never
      // hydrated fails here rather than passing the null-lang assertion for free.
      expect(await declaration(page)).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
    });
  }
});

// ══ the /docs PROSE declares the language of every endonym it names ═════════
// (card t_23c6895c — the prose half of the class the two blocks above judge on
// the chrome and the legal footer.)
//
// THE DEFECT, measured live on prod web v1.21.333 (fresh browser state; document
// `lang="en"`, which is correct — the guides are English prose):
//
//   /docs/customize  <strong>English, Bahasa Melayu, 中文, தமிழ்</strong>
//   /docs/customize  <strong>العربية</strong>
//   /docs/chat       "It speaks the language you write in: English, Bahasa Melayu, 中文, தமிழ் or العربية."
//
// 3 elements per page held CJK / Tamil / Arabic in their OWN text and every one
// resolved, through the nearest `lang`, to the document's "en". The names came
// from LITERALS in JSX (chat/page.tsx, customize/page.tsx), which is why the
// component fixes that declared the chrome labels (docsChrome.tsx) and the
// switcher/picker endonyms never reached them. They now render through
// `components/Endonym.tsx`, which puts `<span lang="zh">` etc. around the text it
// holds; the source half of this guard is `src/lib/docsProseEndonym.test.ts`.
//
// WHY A DOM SWEEP AND NOT axe. axe has no rule for "text in another language
// inside a correctly declared document" (measured on the pre-fix page: three
// undeclared endonyms, 0 axe violations), so the only proof is reading the served
// DOM — and only a browser shows the attribute surviving the render.
//
// WHAT MAKES IT BITE.
//   * The sweep is over every element's OWN text, and the language of a part is
//     decided by the NEAREST declaration — so an ancestor's `lang` cannot launder
//     an undeclared endonym, and on the pre-fix build every one of the three
//     resolves to "en".
//   * Non-vacuity per script, per page: the guides must really NAME all three, or
//     an empty sweep would pass for free.
//   * The detector arm strips the declarations off the REAL elements in the live
//     DOM and requires the sweep to report exactly those three — the same control
//     the release's verification used (3 exceptions) — so a sweep that cannot see
//     the defect cannot pass this file.
//   * The `jf_lang=ar` arm proves the declaration is a property of the TEXT and not
//     of the visitor's locale: these guides stay English pages either way.
const ENDONYM_PROSE_ROUTES = [
  { path: "/docs/chat", english: "It speaks the language you write in" },
  { path: "/docs/customize", english: "The supported languages are" },
] as const;

/** The three names this card measured, and the language each must declare. */
const PROSE_ENDONYMS = [
  { name: "中文", lang: "zh" },
  { name: "தமிழ்", lang: "ta" },
  { name: "العربية", lang: "ar" },
] as const;

type EndonymSweep = {
  counts: Record<"CJK" | "Tamil" | "Arabic", number>;
  wrong: { tag: string; own: string; expected: string; declared: string }[];
};

/**
 * Every element whose OWN text contains CJK, Tamil or Arabic, judged by the
 * NEAREST `lang` a screen reader would use.
 *
 * `counts` is the non-vacuity reading, `wrong` the failure list. Non-rendered
 * containers (`<script>` holds the RSC payload) are skipped: its text is not
 * prose a reader is meant to hear.
 */
async function endonymSweep(page: Page): Promise<EndonymSweep> {
  return page.evaluate(() => {
    const SCRIPT_RANGES = [
      { lang: "zh", script: "CJK", re: /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/ },
      { lang: "ta", script: "Tamil", re: /[\u0b80-\u0bff]/ },
      { lang: "ar", script: "Arabic", re: /[\u0600-\u06ff]/ },
    ] as const;

    const counts = { CJK: 0, Tamil: 0, Arabic: 0 };
    const wrong: EndonymSweep["wrong"] = [];

    for (const el of Array.from(document.querySelectorAll<HTMLElement>("body *"))) {
      if (["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"].includes(el.tagName)) continue;
      const own = Array.from(el.childNodes)
        .filter((n) => n.nodeType === 3)
        .map((n) => n.textContent ?? "")
        .join("")
        .trim();
      if (!own) continue;
      for (const range of SCRIPT_RANGES) {
        if (!range.re.test(own)) continue;
        counts[range.script] += 1;
        const declared = (el.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase();
        if (declared === range.lang || declared.startsWith(`${range.lang}-`)) continue;
        wrong.push({
          tag: el.tagName.toLowerCase(),
          own: own.slice(0, 40),
          expected: range.lang,
          declared: declared || "(none)",
        });
      }
    }
    return { counts, wrong };
  });
}

/** Asserts this page NAMES all three languages and declares each one. */
async function expectEndonymsDeclared(page: Page, path: string) {
  const swept = await endonymSweep(page);
  expect(
    swept.counts,
    `${path} did not render all three endonyms in its own text (${JSON.stringify(swept.counts)}) — the rule below would prove nothing about the names this card measured`,
  ).toEqual({ CJK: 1, Tamil: 1, Arabic: 1 });
  expect(
    swept.wrong,
    `${path}: these elements hold text in another language inside the English guide but nothing declares it — ` +
      `WCAG 3.1.2 (Language of Parts), a screen reader reads them with an English voice:\n  ` +
      swept.wrong.map((w) => `<${w.tag}> ${JSON.stringify(w.own)} — expected lang="${w.expected}", nearest lang="${w.declared}"`).join("\n  "),
  ).toEqual([]);

  // The element that HOLDS the name, read out of the live DOM: the endonym is
  // rendered with its own `lang`, not inherited from an ancestor that happens to
  // be right. `labelsOf` reads the innermost element whose text is exactly the name.
  for (const { name, lang } of PROSE_ENDONYMS) {
    const found = await labelsOf(page, name);
    expect(found.length, `${path} did not render the endonym ${name}`).toBeGreaterThan(0);
    expect(
      found.map((f) => f.lang),
      `${path}: the element holding ${name} must declare lang="${lang}" itself`,
    ).toEqual(found.map(() => lang));
    const nearest = await page.evaluate(
      (n) =>
        Array.from(document.querySelectorAll<HTMLElement>("body *"))
          .filter((e) => e.childElementCount === 0 && (e.textContent ?? "").trim() === n)
          .map((e) => (e.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase()),
      name,
    );
    expect(nearest, `${path}: the nearest declaration a screen reader finds for ${name} must be its own language`).toEqual(
      nearest.map(() => lang),
    );
  }

  // The guides are English prose; the fix is a language of PARTS.
  expect(await declaration(page), `${path} live declaration`).toEqual({ lang: "en", dir: "ltr", direction: "ltr" });
}

test.describe("the /docs prose declares each endonym's own language (WCAG 3.1.2)", () => {
  test("a fresh visitor gets the three names declared on both guides, documents still English", async ({ page }) => {
    await freshState(page, null);

    for (const { path, english } of ENDONYM_PROSE_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });
      // Reachability: the English sentence that NAMES the languages is on the page,
      // so the sweep below judges a rendered guide and not an empty shell.
      await expect(page.getByText(english).first()).toBeVisible({ timeout: 30_000 });
      await expect(page.getByText("中文", { exact: true }).first()).toBeVisible();
      await expectEndonymsDeclared(page, path);
    }
  });

  test("with jf_lang=ar the guides stay English and the names still declare their own scripts", async ({ page }) => {
    // The declaration belongs to the TEXT, not to the visitor's dashboard locale:
    // an implementation that declared the document language on the parts (or moved
    // the page) passes the fresh-state arm and fails here.
    await freshState(page, "ar");

    for (const { path, english } of ENDONYM_PROSE_ROUTES) {
      await page.goto(path, { waitUntil: "networkidle" });
      await expect(page.getByText(english).first()).toBeVisible({ timeout: 30_000 });
      // `/docs` chrome labels ARE translated, but these two guides' prose is not:
      // the names are their own endonyms whatever the locale.
      await expectEndonymsDeclared(page, path);
    }

    // And the control that says the locale really was applied on this tree: the
    // /docs index's heading follows it. Without this, the assertions above could
    // pass because the app ignored `jf_lang` entirely.
    await page.goto("/docs", { waitUntil: "networkidle" });
    await expect(page.getByRole("heading", { name: DICT["docs.title"].ar })).toBeVisible({ timeout: 30_000 });
  });

  test("the sweep bites: stripping the declarations live reports exactly the three names", async ({ page }) => {
    await freshState(page, null);
    await page.goto("/docs/chat", { waitUntil: "networkidle" });
    await expect(page.getByText("It speaks the language you write in").first()).toBeVisible({ timeout: 30_000 });

    // Baseline first: with the fix in place this page is clean.
    expect((await endonymSweep(page)).wrong).toEqual([]);

    // Remove `lang` from every element that holds one of the three names — the
    // pre-fix shape of THIS page, reproduced in the served DOM.
    const stripped = await page.evaluate((names) => {
      let n = 0;
      for (const el of Array.from(document.querySelectorAll<HTMLElement>("body *"))) {
        if (el.childElementCount !== 0) continue;
        if (!names.includes((el.textContent ?? "").trim())) continue;
        el.removeAttribute("lang");
        n += 1;
      }
      return n;
    }, PROSE_ENDONYMS.map((e) => e.name as string));
    expect(stripped, "the served /docs/chat page does not carry a lang on all three endonyms").toBe(3);

    const swept = await endonymSweep(page);
    expect(
      swept.wrong.length,
      `the sweep did not report the three undeclared endonyms it was given, so it cannot judge the defect: ${JSON.stringify(swept.wrong)}`,
    ).toBe(3);
    expect(swept.wrong.map((w) => w.expected).sort()).toEqual(["ar", "ta", "zh"]);
    expect(
      swept.wrong.map((w) => w.declared),
      "each undeclared endonym must resolve to the document's own language",
    ).toEqual(["en", "en", "en"]);
  });
});

// ══ the operator sentence is LAID OUT in its own direction (card t_d2f2e21f) ══
//
// The block above pins how the sentence is PRONOUNCED (its `lang`). This one pins
// how its runs are LAID OUT, which is a separate fact and the one a reader
// actually sees.
//
// THE DEFECT, measured on prod v1.21.332 (revision a3661d1e) with `jf_lang=ar` at
// 1440x900, per-word client rects on /legal/privacy: the <p> declared `lang="ar"`
// and computed `direction: ltr`, because the legal pages keep `dir="ltr"` on
// <html> (their prose is English, PR 1201 — correctly). Under an LTR paragraph
// base the bidi algorithm places the RTL runs in LOGICAL order, left to right, so
// the leftmost run was "يُدار" — the sentence's FIRST word — and the rightmost was
// the registration clause, its LAST: an Arabic reader scanning right-to-left met
// the sentence back to front. Same on /legal/terms. /landing read correctly only
// because its document is already `dir="rtl"`.
//
// WHAT MAKES IT BITE. The direction assertions are one half; the other half is
// geometry read from the live DOM, because `direction: rtl` is exactly what a
// `dir` attribute is FOR and the two cannot be judged apart:
//   * the sentence's FIRST character must sit at the paragraph's RIGHT edge —
//     `firstCharRightGap` near 0, `firstCharLeftGap` past the middle. Pre-fix the
//     same two numbers are the mirror image (left gap 0), which is the defect.
//     Measured with a Range over that one character, so it is wrap-independent: an
//     RTL paragraph starts at the right edge on EVERY line.
//   * inside the isolated clause, the glyphs '2' and '0' of the registration
//     number must still read left-to-right (the `<bdi dir="ltr">` the language
//     block above pins), so the fix cannot be "flip everything".
//   * the four LTR arms refuse the mirror: an implementation that emits
//     `dir="rtl"` unconditionally passes the Arabic arm and fails there.
// A page that rendered nothing passes none of this: every case first waits for the
// sentence ITSELF to be in the DOM.
type OperatorLayout = {
  /** The declaration on the passage itself. */
  lang: string | null;
  dir: string | null;
  /** …and what the browser resolved for it. */
  direction: string;
  /** The registration clause's own direction — must stay ltr in every locale. */
  bdiDir: string | null;
  bdiDirection: string | null;
  /** Client x of the clause's first two glyphs ('2' then '0'): ltr order = ascending. */
  clauseFirstLeft: number | null;
  clauseSecondLeft: number | null;
  /** The sentence's first character, measured against the paragraph's own box. */
  firstCharLeftGap: number | null;
  firstCharRightGap: number | null;
  paragraphWidth: number;
};

/** The passage's own layout, read out of the live DOM by geometry, not by CSS text. */
async function operatorLayout(page: Page, sentence: string): Promise<OperatorLayout | null> {
  return page.evaluate((s): OperatorLayout | null => {
    const p = Array.from(document.querySelectorAll("p")).find((e) => (e.textContent ?? "").trim() === s);
    if (!p) return null;
    const bdi = p.querySelector("bdi");
    const box = p.getBoundingClientRect();
    const glyph = (node: Text, at: number) => {
      const r = document.createRange();
      r.setStart(node, at);
      r.setEnd(node, at + 1);
      return r.getBoundingClientRect();
    };
    const textOf = (n: Node | null): Text | null =>
      n && n.nodeType === Node.TEXT_NODE ? (n as Text) : null;
    const lead = textOf(p.firstChild);
    // `.trim()` is what the component renders, so the first character of the text
    // node is the first character of the sentence.
    const first = lead ? glyph(lead, lead.data.length - lead.data.trimStart().length) : null;
    const clause = bdi ? textOf(bdi.firstChild) : null;
    const c0 = clause && clause.data.length > 1 ? glyph(clause, 0) : null;
    const c1 = clause && clause.data.length > 1 ? glyph(clause, 1) : null;
    return {
      lang: p.getAttribute("lang"),
      dir: p.getAttribute("dir"),
      direction: getComputedStyle(p).direction,
      bdiDir: bdi?.getAttribute("dir") ?? null,
      bdiDirection: bdi ? getComputedStyle(bdi).direction : null,
      clauseFirstLeft: c0 ? Math.round(c0.left) : null,
      clauseSecondLeft: c1 ? Math.round(c1.left) : null,
      firstCharLeftGap: first ? Math.round(first.left - box.left) : null,
      firstCharRightGap: first ? Math.round(box.right - first.right) : null,
      paragraphWidth: Math.round(box.width),
    };
  }, sentence);
}

/** Waits until `path` holds `sentence`, then returns the passage's layout. */
async function laidOutSentence(page: Page, path: string, sentence: string): Promise<OperatorLayout> {
  await page.goto(path, { waitUntil: "networkidle" });
  await expect
    .poll(async () => (await operatorLayout(page, sentence)) !== null, {
      timeout: 30_000,
      message:
        `${path} never rendered the operator sentence for this locale — the layout assertions would be ` +
        `judging a page that rendered nothing`,
    })
    .toBe(true);
  const layout = await operatorLayout(page, sentence);
  expect(layout, `${path}: the sentence vanished between the wait and the read`).not.toBeNull();
  return layout as OperatorLayout;
}

// ══ the registration clause never SPLITS across lines (card t_bdcee56f) ══════
//
// The block above pins how the sentence is laid out. This one pins that its
// registration clause stays ONE unit, because the sentence is ~706px wide in the
// 720px legal-page paragraph: only 14px of slack separates "fits on one line"
// from "a line break has to happen", and the only break opportunity that fits is
// the hyphen INSIDE the code. Measured in Chromium on this tree at a 692px
// paragraph (a 740px viewport): line 1 ended `201403298654 (سابقًا PC0012291-`
// and line 2 held `H).` — three glyphs stranded at the paragraph's right edge,
// exactly the fragment the card reports. It is a knife-edge, not a fixed width:
// prod at 720px renders one line with the same build, so the orphan arrives
// whenever the font is a little wider (another platform's Arabic fallback, a
// larger browser default font size) or a locale reads longer.
//
// The fix is `whitespace-nowrap` on the <bdi>, so the clause wraps as a unit.
// What this arm MEASURES, per viewport, from the live DOM:
//   * the <bdi>'s client rects all share ONE line box — pre-fix the 692px case
//     reads two tops, and the paragraph's per-character lines put a clause
//     fragment on the second one;
//   * no line carries a fragment of the clause shorter than a whole word (the
//     `H).` shape), and the concatenated fragments are the clause byte for byte,
//     so nothing was dropped instead of moved;
//   * the no-wrap rule did not change the base direction: the passage is still
//     computed `rtl`, the clause still `ltr`;
//   * the clause stays INSIDE the paragraph box and the document does not scroll
//     sideways — nowrap moves the clause to the next line, it must not make it
//     overflow, which is the failure mode the 320px case guards.
type ClauseWrap = {
  /** Distinct line tops of the <bdi>'s own client rects (one = never split). */
  bdiLineTops: number[];
  /** The clause characters that landed on each paragraph line, in order. */
  clauseParts: string[];
  /** The <bdi>'s horizontal extent, and the paragraph's own box. */
  bdiLeft: number;
  bdiRight: number;
  paragraphLeft: number;
  paragraphRight: number;
  paragraphWidth: number;
  /** Direction is a property of the WORDS, not of the wrap: both must survive. */
  paragraphDirection: string;
  bdiDirection: string;
  scrollWidth: number;
  innerWidth: number;
};

async function clauseWrap(page: Page, sentence: string): Promise<ClauseWrap | null> {
  return page.evaluate((s): ClauseWrap | null => {
    const p = Array.from(document.querySelectorAll("p")).find((e) => (e.textContent ?? "").trim() === s);
    const bdi = p?.querySelector("bdi") ?? null;
    if (!p || !bdi) return null;
    const box = p.getBoundingClientRect();
    const rects = Array.from(bdi.getClientRects());
    const bdiLineTops = Array.from(new Set(rects.map((r) => Math.round(r.top)))).sort((a, b) => a - b);
    // Which clause characters sit on which line, read per character so a split
    // INSIDE the clause cannot hide behind a line that merely looks short.
    const lines = new Map<number, string[]>();
    const walker = document.createTreeWalker(p, NodeFilter.SHOW_TEXT);
    let node: Node | null;
    while ((node = walker.nextNode())) {
      if (!bdi.contains(node)) continue;
      const text = node as Text;
      for (let i = 0; i < text.data.length; i++) {
        const r = document.createRange();
        r.setStart(text, i);
        r.setEnd(text, i + 1);
        const b = r.getBoundingClientRect();
        if (b.width === 0 && b.height === 0) continue;
        const key = Math.round(b.top);
        if (!lines.has(key)) lines.set(key, []);
        (lines.get(key) as string[]).push(text.data[i]);
      }
    }
    return {
      bdiLineTops,
      clauseParts: Array.from(lines.entries())
        .sort((a, b) => a[0] - b[0])
        .map(([, cs]) => cs.join("")),
      bdiLeft: Math.round(Math.min(...rects.map((r) => r.left))),
      bdiRight: Math.round(Math.max(...rects.map((r) => r.right))),
      paragraphLeft: Math.round(box.left),
      paragraphRight: Math.round(box.right),
      paragraphWidth: Math.round(box.width),
      paragraphDirection: getComputedStyle(p).direction,
      bdiDirection: getComputedStyle(bdi).direction,
      scrollWidth: document.documentElement.scrollWidth,
      innerWidth: window.innerWidth,
    };
  }, sentence);
}

/** Navigates to `path` and waits for the passage, then reads the clause's wrap. */
async function laidOutClause(page: Page, path: string, sentence: string): Promise<ClauseWrap> {
  await page.goto(path, { waitUntil: "networkidle" });
  await expect
    .poll(async () => (await clauseWrap(page, sentence)) !== null, {
      timeout: 30_000,
      message: `${path} never rendered the operator sentence with its <bdi> clause`,
    })
    .toBe(true);
  const wrap = await clauseWrap(page, sentence);
  expect(wrap, `${path}: the sentence vanished between the wait and the read`).not.toBeNull();
  return wrap as ClauseWrap;
}


test.describe("the operator sentence is laid out in its own direction", () => {
  test("with jf_lang=ar the sentence reads right-to-left inside the LTR legal page", async ({ page }) => {
    await freshState(page, "ar");
    const sentence = operatorSentence("ar");

    for (const path of OPERATOR_SENTENCE_ROUTES) {
      const l = await laidOutSentence(page, path, sentence);

      // 1. The passage declares the direction itself, and the browser resolved it.
      expect(
        l.dir,
        `${path}: the Arabic passage carries no dir — it inherits the page's dir="ltr", the runs come out in ` +
          `logical order and the sentence reads back to front for an Arabic reader (the reported defect)`,
      ).toBe("rtl");
      expect(l.direction, `${path}: computed direction of the Arabic passage`).toBe("rtl");
      // …and its language declaration, which the block above owns, is untouched.
      expect(l.lang, `${path}: the passage must still declare Arabic`).toBe("ar");

      // 2. The registration clause keeps its own LTR isolation: '2' then '0' read
      //    left-to-right inside it. A fix that flipped the whole paragraph would
      //    pass 1 and fail here.
      expect(l.bdiDir, `${path}: the <bdi> clause must keep dir="ltr"`).toBe("ltr");
      expect(l.bdiDirection, `${path}: computed direction of the clause`).toBe("ltr");
      expect(
        l.clauseSecondLeft,
        `${path}: the registration number must read left-to-right inside the isolated clause`,
      ).toBeGreaterThan(l.clauseFirstLeft as number);

      // 3. The reading order, measured: the sentence's FIRST character sits at the
      //    paragraph's RIGHT edge, i.e. where an RTL line starts. Pre-fix it sits at
      //    the left edge (leftGap 0) and this pair is the mirror image.
      expect(
        l.firstCharLeftGap,
        `${path}: the sentence's first character is ${l.firstCharLeftGap}px from the paragraph's LEFT edge ` +
          `and ${l.firstCharRightGap}px from its right edge (${l.paragraphWidth}px wide) — an RTL paragraph ` +
          `starts at the right, so an Arabic reader must meet "يُدار" there, not the registration clause`,
      ).toBeGreaterThan(l.paragraphWidth / 2);
      expect(l.firstCharRightGap, `${path}: the sentence's first character must hug the right edge`).toBeLessThan(
        12,
      );

      // 4. The fix is a direction of PARTS: PR 1201's document declaration, and the
      //    English prose it describes, must not have moved.
      expect(await declaration(page), `${path} live declaration`).toEqual({
        lang: "en",
        dir: "ltr",
        direction: "ltr",
      });
    }
  });

  test("with jf_lang=ar the registration clause is never split across two lines", async ({ page }) => {
    await freshState(page, "ar");
    const sentence = operatorSentence("ar");
    const { regClause } = operatorParts(DICT[OPERATOR_KEY].ar);

    // Viewports around the knife edge the defect lives on. The paragraph is
    // min(viewport, 768) - 48px wide:
    //   320x844  → 272px, the narrowest the card names. The clause is ~210px, so
    //              nowrap must still FIT it — this is the no-overflow arm.
    //   390x844  → 342px.
    //   740x577  → 692px. MEASURED pre-fix on this tree: line 1 ended
    //              `201403298654 (سابقًا PC0012291-`, line 2 held `H).`.
    //   1280x577 → 720px, the width the card measured on prod.
    const viewports = [
      { width: 320, height: 844 },
      { width: 390, height: 844 },
      { width: 740, height: 577 },
      { width: 1280, height: 577 },
    ] as const;

    for (const vp of viewports) {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      const w = await laidOutClause(page, "/legal/privacy", sentence);
      const where = `${vp.width}x${vp.height} (paragraph ${w.paragraphWidth}px)`;

      // 1. ONE line box. Pre-fix the 692px case reads two tops.
      expect(
        w.bdiLineTops.length,
        `${where}: the registration clause was laid out on ${w.bdiLineTops.length} lines ` +
          `(${JSON.stringify(w.clauseParts)}) — it must move to the next line WHOLE, never break at ` +
          `its own hyphen and strand \`H).\` at the paragraph's edge`,
      ).toBe(1);

      // 2. …and the paragraph's per-character lines agree: the clause's
      //    characters all land on ONE line and none went missing. A second line
      //    carrying three glyphs (`H).`) is the reported shape.
      const parts = w.clauseParts.filter((part) => part.trim().length > 0);
      expect(
        parts.length,
        `${where}: the clause's characters landed on ${parts.length} lines: ${JSON.stringify(parts)}`,
      ).toBe(1);
      expect(parts[0], `${where}: the clause lost characters instead of moving whole`).toBe(regClause);

      // 3. nowrap is a WRAP rule: it must not have touched the base direction.
      //    The direction geometry arms above stay the authority on reading order;
      //    this is the "wrapping only" half of that claim, at each width.
      expect(w.paragraphDirection, `${where}: the passage is no longer RTL`).toBe("rtl");
      expect(w.bdiDirection, `${where}: the clause is no longer LTR`).toBe("ltr");

      // 4. …and it must not overflow. The clause stays inside the paragraph's own
      //    box and the document does not scroll sideways — nowrap moves the clause
      //    to the next line, it must not push it out of the column.
      expect(
        w.bdiLeft,
        `${where}: the clause starts ${w.paragraphLeft - w.bdiLeft}px LEFT of the paragraph`,
      ).toBeGreaterThanOrEqual(w.paragraphLeft - 1);
      expect(
        w.bdiRight,
        `${where}: the clause ends ${w.bdiRight - w.paragraphRight}px RIGHT of the paragraph`,
      ).toBeLessThanOrEqual(w.paragraphRight + 1);
      expect(w.scrollWidth, `${where}: the page scrolls sideways`).toBe(w.innerWidth);
    }
  });

  test("with jf_lang=en or unset the sentence adds no dir and still reads left-to-right", async ({ page }) => {
    for (const lang of ["en", null] as const) {
      await freshState(page, lang);
      // The sentence is the ENGLISH one, so this arm also proves the fix did not
      // key on being on the legal pages rather than on the locale.
      const sentence = operatorSentence("en");
      const l = await laidOutSentence(page, "/legal/privacy", sentence);

      expect(
        l.dir,
        `jf_lang=${lang}: the English sentence must carry NO dir — the document already declares LTR and a ` +
          `declaration that repeats it is noise in the state every crawler and every new visitor sees`,
      ).toBe(null);
      expect(l.direction, `jf_lang=${lang}: computed direction of the English passage`).toBe("ltr");
      expect(l.lang, `jf_lang=${lang}: an English sentence declares no lang`).toBe(null);
      expect(
        l.firstCharLeftGap,
        `jf_lang=${lang}: an LTR paragraph starts at the LEFT edge — this is the control that says the ` +
          `measurement above is not symmetric noise`,
      ).toBeLessThan(12);
    }
  });

  test("with jf_lang=zh a non-Arabic locale adds no dir either", async ({ page }) => {
    await freshState(page, "zh");
    // The mirror of the `lang` block's zh arm: an implementation that hard-codes
    // dir="rtl" (the tempting shortcut, since the defect was reported in Arabic)
    // passes the Arabic arm and mirrors the Chinese sentence here.
    const l = await laidOutSentence(page, "/legal/privacy", operatorSentence("zh"));
    expect(l.dir, "a Chinese sentence must not be declared RTL").toBe(null);
    expect(l.direction).toBe("ltr");
    expect(l.lang, "…and it still declares Chinese, not Arabic").toBe("zh");
    expect(l.firstCharLeftGap, "a Chinese sentence starts at the LEFT edge").toBeLessThan(12);
  });
});
