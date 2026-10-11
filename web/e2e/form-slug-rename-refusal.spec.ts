// Browser evidence for the refused-slug-rename defect: drive the REAL Form
// Settings save in a REAL browser and read the message out of the live DOM.
//
// WHAT THIS PROVES (the unit suite cannot)
// The unit suite (src/lib/slugRenameErr.test.ts) reads the source and the
// dictionary; it never renders the page. A perfectly wired apiErr() mapping can
// still be dead behind a mis-wired call site — the refusal reaching setMsg, or
// the save button not wired to it at all. This file clicks the actual button and
// reads the actual <p> the merchant reads.
//
// THE DEFECT, as the merchant lived it: the save reported "Saved ✓" (or a bare
// "Failed") while the form kept its old slug, and when the server DID answer with
// a reason it was Malay prose printed verbatim to an English reader.
//
// The API is stubbed at the browser level, so this needs no Go server, session,
// or database — only the build the gate is already serving.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";

const FORM_ID = "form_slug_guard";

// stubAPI answers every /api/* call the page makes. The forms PATCH is pinned
// LAST so it wins (Playwright resolves matching routes in reverse registration
// order), and it is the one route whose response the test varies.
async function stubAPI(page: Page, patch: { status: number; body: unknown }) {
  const json = (body: unknown, status = 200) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) => {
    if (route.request().method() === "PATCH") {
      return route.fulfill(json(patch.body, patch.status));
    }
    return route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.com/f/demo-form",
        config: { blocks: [] },
        theme: {},
      }),
    );
  });
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (route) =>
    route.fulfill(json({ feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (route) =>
    route.fulfill(json({ confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (route) =>
    route.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
}

// The General tab's Save button is the one inside the same <section> as the slug
// field — scoped so a later tab's identical "Save" label cannot be clicked.
async function saveGeneralTab(page: Page, label: string) {
  const section = page.locator("section").filter({ has: page.locator("h2") }).first();
  await section.getByRole("button", { name: label }).first().click();
}

// messageText reads the merchant-visible <p> the page sets, by locating the slug
// input and walking up to the section, then reading that section's <p> sibling.
// Falls back to any short <p> directly under the tab body.
async function messageText(page: Page): Promise<string> {
  return page.evaluate(() => {
    const nodes = [...document.querySelectorAll("p")].map((p) => ({
      text: (p.textContent || "").trim(),
      cls: p.className || "",
    }));
    // The page renders the message as a bare <p> with a text-xs class, outside
    // the sections' own helper <p>s (those carry text-[11px]).
    const msg = nodes.filter((n) => n.cls.includes("text-xs") && !n.cls.includes("text-[11px]"));
    return msg.length ? msg[msg.length - 1].text : "";
  });
}

test.describe("a refused slug rename is visible, in the merchant's language", () => {
  test("409 form.slugInUse renders the English sentence, not the server prose", async ({ page }) => {
    // A FRESH context per test: no cookie, no localStorage, no jf_lang left by an
    // earlier card. `lang: ""` above keeps the profile from pinning a locale, so
    // this is exactly what a new English visitor gets.
    await page.addInitScript(() => {
      try {
        window.localStorage.clear();
      } catch {}
    });
    await signIn(page);
    await stubAPI(page, {
      status: 409,
      // The server's wire body: Malay prose + the machine code. The prose must
      // NEVER reach the page.
      body: { error: "slug borang telah diambil borang lain", code: "form.slugInUse" },
    });

    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForSelector('input[value="demo-form"]', { timeout: 30_000 });

    await saveGeneralTab(page, DICT["fs.save"].en);
    await page.waitForFunction(
      () => [...document.querySelectorAll("p")].some((p) => (p.textContent || "").includes("Another form")),
      null,
      { timeout: 30_000 },
    );

    const msg = await messageText(page);
    const title = await page.title();
    // Report the OBSERVED values so the evidence is in the run log, not inferred.
    console.log(`OBSERVED title=${JSON.stringify(title)}`);
    console.log(`OBSERVED message=${JSON.stringify(msg)}`);
    console.log(`OBSERVED lang=${await page.evaluate(() => document.documentElement.lang)}`);

    expect(msg).toBe(DICT["apiErr.formSlugInUse"].en);
    expect(msg).not.toContain("telah diambil");
    expect(msg).not.toBe(DICT["prof.failed"].en);
  });

  test("400 form.slugInvalid renders the rule sentence, not the server prose", async ({ page }) => {
    await page.addInitScript(() => {
      try {
        window.localStorage.clear();
      } catch {}
    });
    await signIn(page);
    await stubAPI(page, {
      status: 400,
      body: {
        error: "form slug must be 1-40 characters of a-z, 0-9 or -",
        code: "form.slugInvalid",
      },
    });

    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForSelector('input[value="demo-form"]', { timeout: 30_000 });

    await saveGeneralTab(page, DICT["fs.save"].en);
    await page.waitForFunction(
      () => [...document.querySelectorAll("p")].some((p) => (p.textContent || "").includes("1-40 characters")),
      null,
      { timeout: 30_000 },
    );

    const msg = await messageText(page);
    console.log(`OBSERVED invalid message=${JSON.stringify(msg)}`);
    expect(msg).toBe(DICT["apiErr.formSlugInvalid"].en);
  });

  test("a 200 still reports Saved ✓ — the refusal path did not swallow the success path", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      try {
        window.localStorage.clear();
      } catch {}
    });
    await signIn(page);
    stubAPI(page, { status: 200, body: { ok: true } });

    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForSelector('input[value="demo-form"]', { timeout: 30_000 });

    await saveGeneralTab(page, DICT["fs.save"].en);
    await page.waitForFunction(
      (saved) => [...document.querySelectorAll("p")].some((p) => (p.textContent || "").includes(saved)),
      DICT["fs.saved"].en,
      { timeout: 30_000 },
    );

    const msg = await messageText(page);
    console.log(`OBSERVED success message=${JSON.stringify(msg)}`);
    expect(msg).toContain(DICT["fs.saved"].en);
  });
});
