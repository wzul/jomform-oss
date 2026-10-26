// The language switcher names each locale in that locale's OWN language, so the
// element that holds the name must declare which language it is in — otherwise
// a screen reader pronounces 中文 with the voice of the surrounding page
// (WCAG 3.1.2 Language of Parts).
//
// THE DEFECT, measured live on jomform.com/mcp (prod web v1.21.190, revision
// 645f31f7) from a fresh browser state:
//
//   jf_lang=en → documentElement.lang = "en"; three <button>s whose own text is
//                中文 / தமிழ் / عربي, each with NO lang of its own, each
//                resolving `el.closest("[lang]").getAttribute("lang")` to "en".
//   jf_lang=ar → the same three, the document lang="ar", nearest lang "ar".
//
// So 中文 was read with an English voice on the English page and an Arabic one
// on the Arabic page. The switcher on /mcp was a PRIVATE copy of the five labels
// (`app/mcp/mcp-client.tsx` had its own ternary chain); the shared components in
// `components/i18n.tsx` had the same shape. Both are fixed, and this spec is the
// executed half of the guard — the source half is `src/lib/endonymLang.test.ts`.
//
// WHY A DOM SWEEP AND NOT axe. axe has no rule for this class: "text in another
// language inside a correctly declared document" is not something it can judge.
// The only proof is reading the served DOM after hydration.
//
// WHAT MAKES IT BITE. Every element whose OWN text (its direct text nodes — the
// language of a passage is decided by the NEAREST declaration, so an ancestor's
// innerText would judge the wrong element) is written in CJK, Tamil or Arabic
// must resolve, through its own or an ancestor's `lang`, to that script's
// language. Before the fix every one of the endonyms resolved to the document's
// language, so both arms below are RED on the pre-fix build. The non-vacuity
// counts are what stop this passing for free on a switcher that rendered no
// endonym at all.
//
// The API is stubbed at the browser level (there is no Go server in this gate —
// see playwright.config.ts), so the profile read answers 401 and the browser
// value of `jf_lang` is the one in effect, exactly as for a signed-out visitor.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";

/**
 * The scripts the endonyms are written in, and the language each one is.
 *
 * Derived from the text, not from a list of the labels, so the sweep judges
 * whatever the switcher renders — including an endonym added later.
 */
const SCRIPTS = [
  { lang: "zh", script: "CJK", re: /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/ },
  { lang: "ta", script: "Tamil", re: /[\u0b80-\u0bff]/ },
  { lang: "ar", script: "Arabic", re: /[\u0600-\u06ff]/ },
] as const;

/** A FRESH browser state, with the language this case means. */
async function freshState(page: Page, lang: "en" | "ar") {
  await page.addInitScript((l) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      // The switch is the point of the arm: a session left behind by another
      // case would render the page for a state no visitor has.
      localStorage.setItem("jf_lang", l);
    } catch {
      /* storage unavailable */
    }
  }, lang);
  await page.context().clearCookies();
  // No Go server here: the profile read answers 401 and the browser value stays
  // in effect — the same path a signed-out visitor takes.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill({ status: 401, contentType: "application/json", body: "{}" }));
}

/** Every element whose OWN text is in `script`, with the language it resolves to. */
async function ownTextOf(page: Page, script: (typeof SCRIPTS)[number]["script"]) {
  const source = SCRIPTS.find((s) => s.script === script)!.re.source;
  return page.evaluate((pattern) => {
    const re = new RegExp(pattern);
    const out: { tag: string; own: string; declared: string }[] = [];
    for (const el of Array.from(document.querySelectorAll<HTMLElement>("body *"))) {
      if (["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"].includes(el.tagName)) continue;
      const own = Array.from(el.childNodes)
        .filter((n) => n.nodeType === 3)
        .map((n) => n.textContent ?? "")
        .join("")
        .trim();
      if (!own || !re.test(own)) continue;
      const declared = (el.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase();
      out.push({ tag: el.tagName.toLowerCase(), own, declared });
    }
    return out;
  }, source);
}

/**
 * The whole-page sweep: every element whose own text is non-Latin must resolve
 * to the language of THAT text — 0 exceptions.
 */
async function mislabelled(page: Page): Promise<string[]> {
  const out: string[] = [];
  for (const { lang, script } of SCRIPTS) {
    for (const el of await ownTextOf(page, script)) {
      const ok = el.declared === lang || el.declared.startsWith(`${lang}-`);
      if (!ok) out.push(`<${el.tag}> ${JSON.stringify(el.own.slice(0, 30))} is ${script} — nearest lang=${el.declared ? `"${el.declared}"` : "(none)"}`);
    }
  }
  return out;
}

/** The switcher's five buttons, read as text + the lang attribute they declare. */
async function switcherButtons(page: Page) {
  return page.evaluate(() => {
    const nav = document.querySelector("nav");
    if (!nav) return [] as { text: string; lang: string | null }[];
    return Array.from(nav.querySelectorAll<HTMLElement>("button")).map((b) => ({
      text: (b.textContent ?? "").trim(),
      lang: b.getAttribute("lang"),
    }));
  });
}

/** The language a label is written in, by its script — undefined for Latin. */
const scriptOf = (text: string) => SCRIPTS.find((s) => s.re.test(text));

test.describe("a language switcher declares the language of each endonym (WCAG 3.1.2)", () => {
  test("on /mcp with jf_lang=en every endonym declares its own language", async ({ page }) => {
    await freshState(page, "en");
    await page.goto("/mcp", { waitUntil: "networkidle" });

    // Reachability first: the locale really was applied and the page really
    // hydrated. `dir` is written by the provider's effect only, so asserting it
    // is also the hydration witness — a page that never hydrated has no dir.
    await expect(
      page.getByRole("heading", { name: DICT["mcp.title"].en }),
      "this case is only meaningful on a page that really rendered in English",
    ).toBeVisible({ timeout: 30_000 });
    await expect(page.locator("html"), "/mcp with jf_lang=en must stay English and LTR").toHaveAttribute("lang", "en");
    await expect(page.locator("html"), "/mcp with jf_lang=en must stay English and LTR").toHaveAttribute("dir", "ltr");

    // Non-vacuity: the three non-Latin endonyms the card measured are on screen.
    for (const script of ["CJK", "Tamil", "Arabic"] as const) {
      const found = await ownTextOf(page, script);
      expect(found.length, `/mcp rendered no ${script} endonym — the sweep below would prove nothing`).toBeGreaterThan(0);
    }

    const wrong = await mislabelled(page);
    expect(
      wrong,
      `these elements hold text in a language the /mcp document (lang="en") is not written in, and nothing declares it — ` +
        `WCAG 3.1.2, a screen reader reads them with an English voice:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);

    // The endonym carries the declaration on the very element that holds it,
    // and the Latin labels add none: EN/BM are the document's own script.
    const buttons = await switcherButtons(page);
    const scripted = buttons.filter((b) => scriptOf(b.text));
    expect(buttons.length, `/mcp's switcher must render its five labels; measured ${JSON.stringify(buttons)}`).toBe(5);
    expect(scripted.length, `/mcp must render the three non-Latin endonyms; measured ${JSON.stringify(buttons)}`).toBe(3);
    for (const b of scripted) {
      const expected = scriptOf(b.text)!.lang;
      expect(b.lang, `the ${JSON.stringify(b.text)} button must declare lang="${expected}" itself`).toBe(expected);
    }
    const latin = buttons.filter((b) => !scriptOf(b.text));
    expect(latin.length, `the Latin labels (EN / BM) are missing: ${JSON.stringify(buttons)}`).toBe(2);
    for (const b of latin) {
      expect(b.lang, `the ${JSON.stringify(b.text)} label must add no lang — it is written in the document's own script`).toBeNull();
    }
  });

  test("on /mcp with jf_lang=ar every endonym declares its own language", async ({ page }) => {
    await freshState(page, "ar");
    await page.goto("/mcp", { waitUntil: "networkidle" });

    // Reachability: the Arabic page really rendered, and the document is the
    // declaration this fix must NOT have moved.
    await expect(
      page.getByRole("heading", { name: DICT["mcp.title"].ar }),
      "this case is only meaningful on a page that really rendered in Arabic",
    ).toBeVisible({ timeout: 30_000 });
    await expect(page.locator("html"), "/mcp with jf_lang=ar must declare Arabic").toHaveAttribute("lang", "ar");
    await expect(page.locator("html"), "/mcp with jf_lang=ar must render RTL").toHaveAttribute("dir", "rtl");
    expect(
      await page.evaluate(() => getComputedStyle(document.documentElement).direction),
      "/mcp with jf_lang=ar must compute as RTL",
    ).toBe("rtl");

    // Non-vacuity: the Arabic page is full of Arabic text, the CJK and Tamil
    // endonyms are still on it, and each must declare ITS language rather than
    // inherit the document's.
    for (const script of ["CJK", "Tamil"] as const) {
      const found = await ownTextOf(page, script);
      expect(found.length, `/mcp rendered no ${script} endonym — the sweep below would prove nothing`).toBeGreaterThan(0);
    }

    const wrong = await mislabelled(page);
    expect(
      wrong,
      `these elements hold text in a language the /mcp document (lang="ar") is not written in, and nothing declares it — ` +
        `WCAG 3.1.2, a screen reader reads them with an Arabic voice:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);

    const buttons = await switcherButtons(page);
    for (const b of buttons.filter((x) => scriptOf(x.text))) {
      const expected = scriptOf(b.text)!.lang;
      expect(b.lang, `the ${JSON.stringify(b.text)} button must declare lang="${expected}" itself, not inherit "ar"`).toBe(
        expected,
      );
    }
    // Still exactly three non-Latin labels and the two Latin ones, in this
    // locale too: the control is the same on every page.
    expect(buttons.filter((b) => scriptOf(b.text)).length).toBe(3);
    expect(buttons.filter((b) => !scriptOf(b.text)).length).toBe(2);
  });
});
