// The dashboard language pickers name each locale in that locale's OWN
// language (中文, தமிழ், العربية — `LANG_LABEL` in `components/i18n.tsx`), so
// every `<option>` that holds one must declare which language it is written in
// (WCAG 3.1.2 Language of Parts). Without it the option text is read with the
// voice of the surrounding document: on `/profile` and `/settings` an English
// page reads 中文 with an English voice, an Arabic page reads it with an Arabic
// one.
//
// THE DEFECT this guard was written against (measured on a `next build` of the
// tree BEFORE the fix, fresh browser state, session bundle stubbed at the
// browser level):
//
//   /profile  <select id="prof-lang">       → 5 options, 3 of them non-Latin,
//   /settings <select id="sf-default-lang">    NONE carrying a `lang`, every one
//                                              resolving through closest("[lang]")
//                                              to the document's "en" (or "ar").
//
// This is the `LANG_LABEL` half of the class PR #1422 fixed for the switcher's
// `LANG_SHORT` (the /mcp switcher, `LangToggle` and `LangSelect`). The source
// half of the guard is `src/lib/endonymLang.test.ts`, which pins these two call
// sites; THIS file is the half that proves the SERVED element carries the
// attribute — the only proof there is, because axe has NO rule for "text in
// another language inside a correctly declared document" (measured on the
// pre-fix build: a full-document scan of /profile reported 0 violations while
// three endonyms sat undeclared).
//
// WHY THE PAGES ARE DRIVEN WITH A STUBBED SESSION (the pattern
// `e2e/a11y-profile-tabs.spec.ts` and `e2e/a11y-settings-first-run-tab.spec.ts`
// use): `/profile` and `/settings` are gated on the server (the `jf_session`
// cookie, middleware.ts) AND on the workspace-keyed `dashboard-bundle`, which
// this gate does not provision (no Go server, no DB). A plain browser session
// renders no `<select>` at all on either page — measured — so a sweep that just
// visited them would find nothing and pass. The real client components are
// driven in a real browser against a stubbed API instead.
//
// THE ARMS
//   1. THE PICKERS (en)   — both pages, document lang="en": the picker renders
//                           its five labels, the three non-Latin ones declare
//                           their own language on the `<option>` itself, the two
//                           Latin ones add none, and the whole-page sweep finds
//                           0 mislabelled non-Latin text.
//   2. THE PICKERS (ar)   — the same on a document declared "ar": the CJK and
//                           Tamil endonyms must still declare THEMSELVES rather
//                           than inherit the document's Arabic (the other
//                           direction of the same fault).
//   3. THE DETECTOR IS LIVE — the en read is taken again with the `lang`
//                           attributes removed from the live DOM; the sweep has
//                           to report exactly the three endonyms. A guard for an
//                           ABSENT attribute is worth nothing unless it can be
//                           made to see its absence.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login before the page
// renders. This spec visits two of them, so it declares the session it means.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

/**
 * The scripts the endonyms are written in, and the language each one is.
 *
 * Derived from the TEXT, not from a list of the labels, so the sweep judges
 * whatever the picker renders — including a locale added later.
 */
const SCRIPTS = [
  { lang: "zh", script: "CJK", re: /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/ },
  { lang: "ta", script: "Tamil", re: /[\u0b80-\u0bff]/ },
  { lang: "ar", script: "Arabic", re: /[\u0600-\u06ff]/ },
] as const;
type ScriptName = (typeof SCRIPTS)[number]["script"];

/** The language a label is written in, by its script — undefined for Latin. */
const scriptOf = (text: string) => SCRIPTS.find((s) => s.re.test(text));

/** A FRESH browser state, with the language this case means. */
async function freshState(page: Page, lang: "en" | "ar") {
  await page.addInitScript((l) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      // Leftover `jf_lang` / tab keys from an earlier card would render a state
      // no new visitor has; that has already produced a false reading here.
      localStorage.setItem("jf_lang", l);
    } catch {
      /* storage unavailable */
    }
  }, lang);
}

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "",
  avatar_url: "",
  phone: "",
  timezone: "",
  totp_enabled: false,
  is_super_admin: false,
};

/** A `manage` member: the strip offers every tab these two pickers live under. */
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "manage" }],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  // lang:"" (never set) keeps the browser value in effect, which is how these
  // arms pin their locale to `jf_lang` instead of to the profile.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/auth\/(passkeys|social|mcp-agents|trust-devices)(\?.*)?$/, (r) =>
    r.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/auth\/trust-status(\?.*)?$/, (r) => r.fulfill(json({ trusted: false })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
}

type OptionRead = { text: string; lang: string | null; declared: string };

/** The `<option>`s of `selector`, with the language each one resolves to. */
async function readPicker(page: Page, selector: string) {
  return page.evaluate((sel) => {
    const select = document.querySelector(sel);
    if (!select) return null;
    return Array.from(select.querySelectorAll("option")).map((o) => ({
      text: (o.textContent ?? "").trim(),
      // The declaration has to be ON the option: an `<option>` holds text only,
      // so a wrapper is not possible and an ancestor's lang is the document's.
      lang: o.getAttribute("lang"),
      declared: (o.closest("[lang]")?.getAttribute("lang") ?? "").toLowerCase(),
    }));
  }, selector) as Promise<OptionRead[] | null>;
}

/**
 * Every element whose OWN text is in `script`, with the language it resolves to.
 *
 * Its own DIRECT text nodes only: the language of a passage is decided by the
 * NEAREST declaration, so judging an ancestor's innerText would judge the wrong
 * element.
 */
async function ownTextOf(page: Page, script: ScriptName) {
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
      if (!ok) {
        out.push(
          `<${el.tag}> ${JSON.stringify(el.own.slice(0, 30))} is ${script} — nearest lang=${
            el.declared ? `"${el.declared}"` : "(none)"
          }`,
        );
      }
    }
  }
  return out;
}

/**
 * Waits until the page really painted (the route is statically prerendered, so
 * the served markup is not evidence the client component ran): React's own
 * marker on the picker, the picker's five options, and the expected option text
 * in this locale. Returns the read either way so a page that never painted fails
 * on the caller's own assertion with the DOM in the message.
 */
async function settlePicker(page: Page, selector: string): Promise<OptionRead[] | null> {
  const deadline = Date.now() + 20_000;
  let read = await readPicker(page, selector);
  while (Date.now() < deadline) {
    const hydrated = await page.evaluate((sel) => {
      const el = document.querySelector(sel);
      return (
        !!el &&
        Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"))
      );
    }, selector);
    if (hydrated && read && read.length >= 5) return read;
    await page.waitForTimeout(50);
    read = await readPicker(page, selector);
  }
  return read;
}

/** `/profile`, whose language picker sits on the landing (identity) tab. */
async function openProfile(page: Page, lang: "en" | "ar"): Promise<OptionRead[] | null> {
  await freshState(page, lang);
  await stubAPI(page);
  const response = await page.goto("/profile", { waitUntil: "domcontentloaded" });
  expect(response?.status() ?? 0, `/profile answered HTTP ${response?.status() ?? "no response"} — nothing to sweep`).toBeLessThan(400);
  return settlePicker(page, "#prof-lang");
}

/** `/settings`, whose picker lives on the Storefront tab. */
async function openSettings(page: Page, lang: "en" | "ar"): Promise<OptionRead[] | null> {
  await freshState(page, lang);
  await stubAPI(page);
  const response = await page.goto("/settings", { waitUntil: "domcontentloaded" });
  expect(response?.status() ?? 0, `/settings answered HTTP ${response?.status() ?? "no response"} — nothing to sweep`).toBeLessThan(400);

  // The picker is in the storefront panel, which is one click inside the strip.
  const tab = page.getByRole("tab", { name: DICT["set.tab.storefront"][lang], exact: true });
  await expect(tab, `the settings strip never rendered its ${lang} Storefront tab`).toBeVisible({ timeout: 30_000 });
  await tab.click();
  return settlePicker(page, "#sf-default-lang");
}

/**
 * The contract for one picker on one page: five labels, the three non-Latin ones
 * declaring their own language ON the option, the two Latin ones declaring
 * nothing, and no mislabelled non-Latin text anywhere else on the page.
 */
function expectPicker(options: OptionRead[] | null, page_: string, lang: "en" | "ar") {
  expect(options, `${page_} (document lang="${lang}") rendered no language picker at all`).not.toBeNull();
  const opts = options!;

  // Non-vacuity: the five labels `LANGS_SHOWN` names. A picker that rendered
  // nothing, or rendered only the Latin labels, would otherwise pass for free.
  expect(
    opts.length,
    `${page_}: the picker renders ${opts.length} options — ${JSON.stringify(opts)}`,
  ).toBe(5);

  const scripted = opts.filter((o) => scriptOf(o.text));
  expect(
    scripted.length,
    `${page_}: the picker rendered ${scripted.length} non-Latin endonym(s), expected 3 — ${JSON.stringify(opts)}`,
  ).toBe(3);

  for (const o of scripted) {
    const expected = scriptOf(o.text)!.lang;
    expect(
      o.lang,
      `${page_} (document lang="${lang}"): the ${JSON.stringify(o.text)} option must carry lang="${expected}" ` +
        `ITSELF — an <option> holds text only, so an ancestor's lang is the document's, and the endonym is ` +
        `read with the wrong voice (WCAG 3.1.2)`,
    ).toBe(expected);
    // The nearest declaration is the one a screen reader uses; it has to be the
    // option's own, not something above it that happens to agree.
    expect(o.declared, `${page_}: the ${JSON.stringify(o.text)} option resolves to ${o.declared || "(none)"}`).toBe(
      expected,
    );
  }

  // The Latin endonyms (English / Bahasa Melayu) add nothing: they are the
  // script the documents that render this picker are written in.
  const latin = opts.filter((o) => !scriptOf(o.text));
  expect(latin.length, `${page_}: the Latin labels are missing — ${JSON.stringify(opts)}`).toBe(2);
  for (const o of latin) {
    expect(
      o.lang,
      `${page_}: the ${JSON.stringify(o.text)} option must add no lang — a declaration here restates the document's own language`,
    ).toBeNull();
  }
}

for (const lang of ["en", "ar"] as const) {
  test(`/profile: the language picker declares each endonym's own language (${lang})`, async ({ page }) => {
    const options = await openProfile(page, lang);
    expectPicker(options, "/profile", lang);

    // The document really is the language this arm pinned — otherwise the sweep
    // below would be judging a page that rendered for another locale.
    await expect(page.locator("html")).toHaveAttribute("lang", lang);

    // Non-vacuity for the sweep: the three endonyms really are in the document.
    for (const script of ["CJK", "Tamil", "Arabic"] as const) {
      const found = await ownTextOf(page, script);
      expect(found.length, `/profile rendered no ${script} text — the sweep below would prove nothing`).toBeGreaterThan(
        0,
      );
    }
    const wrong = await mislabelled(page);
    expect(
      wrong,
      `these elements on /profile (document lang="${lang}") hold text in a language the document is not written ` +
        `in, and nothing declares it — WCAG 3.1.2:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);
  });

  test(`/settings: the language picker declares each endonym's own language (${lang})`, async ({ page }) => {
    const options = await openSettings(page, lang);
    expectPicker(options, "/settings", lang);

    await expect(page.locator("html")).toHaveAttribute("lang", lang);

    for (const script of ["CJK", "Tamil", "Arabic"] as const) {
      const found = await ownTextOf(page, script);
      expect(
        found.length,
        `/settings rendered no ${script} text — the sweep below would prove nothing`,
      ).toBeGreaterThan(0);
    }
    const wrong = await mislabelled(page);
    expect(
      wrong,
      `these elements on /settings (document lang="${lang}") hold text in a language the document is not written ` +
        `in, and nothing declares it — WCAG 3.1.2:\n  ${wrong.join("\n  ")}`,
    ).toEqual([]);
  });
}

test("the arms measure the live DOM: removing the options' lang makes the sweep report the three endonyms", async ({
  page,
}) => {
  // A guard for an ABSENT attribute is worth nothing unless it can be made to
  // see that absence. The same read is taken again with `lang` removed from the
  // options in the live DOM — neutralised BY VALUE, never by deleting an arm —
  // and the sweep has to name exactly the three endonyms that were undeclared
  // before the fix.
  const options = await openProfile(page, "en");
  expectPicker(options, "/profile", "en");
  expect(await mislabelled(page), "the fixed /profile must start with 0 mislabelled elements").toEqual([]);

  await page.evaluate(() => {
    for (const o of Array.from(document.querySelectorAll("#prof-lang option"))) o.removeAttribute("lang");
  });

  const after = await readPicker(page, "#prof-lang");
  expect(
    after!.filter((o) => o.lang).length,
    "the options still carry a lang after it was removed — this arm is not measuring the attribute it was written for",
  ).toBe(0);

  const wrong = await mislabelled(page);
  expect(
    wrong.length,
    `with every option's lang removed, the sweep still reports nothing — it is not measuring the defect:\n  ` +
      JSON.stringify(after),
  ).toBe(3);
  for (const o of after!.filter((x) => scriptOf(x.text))) {
    expect(
      wrong.some((w) => w.includes(JSON.stringify(o.text.slice(0, 30)))),
      `the undeclared ${JSON.stringify(o.text)} option is not named by the sweep: ${JSON.stringify(wrong)}`,
    ).toBe(true);
  }
});
