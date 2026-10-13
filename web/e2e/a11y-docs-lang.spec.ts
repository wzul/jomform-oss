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

// The two English-only prose trees, as the routes a reader really lands on.
// /docs/embed and /legal/privacy are the two the card measured; /docs (the
// index, whose chrome LABELS are translated but whose prose is English) and
// /legal/terms are the rest of the shape. The second field is the English text
// that must be ON the page before its declaration is judged — on /docs it is the
// translated-in-arabic chrome label's ENGLISH value, which only appears once the
// page has hydrated.
const PROSE_ROUTES: { path: string; english: string }[] = [
  { path: "/docs", english: DICT["docs.title"].en },
  { path: "/docs/embed", english: "Embed a form on your site" },
  { path: "/legal/privacy", english: "Privacy Policy" },
  { path: "/legal/terms", english: "Terms of Service" },
];

// A translated surface that needs no session: the landing page the docs tree
// links back to. It is the control for "do not simply drop the dir write".
const TRANSLATED_ROUTES = ["/landing", "/mcp"];

/** A FRESH browser state, with the language this case means (or none at all). */
async function freshState(page: Page, lang: "ar" | "en" | null) {
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
      page.getByText("Private forms and cookies").first(),
      "/docs/embed renders this English section heading in every locale",
    ).toBeVisible();

    await page.goto("/legal/privacy", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/legal/privacy", "Privacy Policy");

    await page.goto("/legal/terms", { waitUntil: "networkidle" });
    await expectEnglishProse(page, "/legal/terms", "Terms of Service");

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

    // A real click path a reader can run: the docs chrome's own back link. The
    // route changes and the language does NOT, so an implementation that read
    // the path once on mount leaves the next page declared English.
    await page.getByRole("link", { name: /Back to JomForm/ }).click();
    await expect.poll(() => new URL(page.url()).pathname, { timeout: 30_000 }).toBe("/");

    await expect(page.getByText(DICT["land.hero1"].ar).first()).toBeVisible({ timeout: 30_000 });
    await expect(
      page.locator("html"),
      "after navigating to a translated page the declaration must move back to the visitor's locale",
    ).toHaveAttribute("dir", "rtl");
    expect((await declaration(page)).direction).toBe("rtl");
  });
});
