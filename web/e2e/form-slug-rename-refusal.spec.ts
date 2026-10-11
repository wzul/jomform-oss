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
// THE API is stubbed at the browser level, so this needs no Go server, session,
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

// How long the stubbed refusal is HELD BACK before it is fulfilled. This is a
// pin, not a slowdown: see `failureBanner` below.
const SLOW_REFUSAL_MS = 1_500;

// stubAPI answers every /api/* call the page makes. The forms PATCH is pinned
// LAST so it wins (Playwright resolves matching routes in reverse registration
// order), and it is the one route whose response the test varies. `patchDelayMs`
// holds that response back so the failure arms exercise a banner that arrives
// AFTER the click, which is the state they raced in.
async function stubAPI(page: Page, patch: { status: number; body: unknown }, patchDelayMs = 0) {
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
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, async (route) => {
    if (route.request().method() === "PATCH") {
      if (patchDelayMs) await new Promise((r) => setTimeout(r, patchDelayMs));
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

// ── reading the merchant's message ─────────────────────────────────────────
//
// The page writes its save feedback to ONE node: `data-testid="action-msg"`
// (web/src/app/form-settings/page.tsx:1302), whose `data-tone` is
// success|failure and which carries `role="alert"` on a failure. Every arm below
// WAITS on that node and READS that node.
//
// That scoping is the fix for a real flake (card t_8ab7ba9e), not a tidy-up.
// This file used to read "the LAST <p> whose class contains text-xs and not
// text-[11px]" across the WHOLE document — a heuristic that only holds while no
// other 12px paragraph happens to be last. The sidebar's group labels are
// exactly such paragraphs (web/src/components/Shell.tsx:111-116 renders
// `p#nav-desktop-grp-help` with `text-xs` in its class), so when the refusal
// banner lagged the 400 arm read the nav label: it asserted `"Help"` against the
// rule sentence, measured 1 of 32 runs. Its WAIT made that reachable — the
// needle, "1-40 characters", is printed STATICALLY by the slug hint
// (`fs.slugHint`, page.tsx:1356), so the wait was already satisfied before the
// click and the read raced the response.
const banner = (page: Page) => page.getByTestId("action-msg");

type Banner = { present: boolean; text: string; tone: string; role: string };

/** The banner's OWN node, read raw so the OBSERVED line is the real DOM. */
async function readBanner(page: Page): Promise<Banner> {
  return page.evaluate(`(() => {
    const el = document.querySelector('[data-testid="action-msg"]');
    if (!el) return { present: false, text: "", tone: "", role: "" };
    return {
      present: true,
      text: (el.textContent || "").trim(),
      tone: el.getAttribute("data-tone") || "",
      role: el.getAttribute("role") || "",
    };
  })()`) as Promise<Banner>;
}

/**
 * Wait for the banner to BE the failure alert, with text in it, then read it.
 *
 * The wait is scoped to the banner node — never to page-wide text — so neither
 * the static slug hint nor a nav label can satisfy it. `toContainText(/\S/)` is
 * the retry that keeps the read off the first paint of an empty banner; the
 * `role` check afterwards is what makes "the alert's own node" an assertion
 * rather than a claim: a nav label can carry neither `data-tone` nor `role`.
 */
async function failureBanner(page: Page): Promise<Banner> {
  await expect(
    banner(page),
    'no refusal banner: nothing on this page carries data-testid="action-msg" with data-tone="failure"',
  ).toHaveAttribute("data-tone", "failure", { timeout: 30_000 });
  await expect(banner(page), "the refusal banner rendered empty").toContainText(/\S/, {
    timeout: 30_000,
  });
  const r = await readBanner(page);
  expect(
    r.role,
    `the banner read back role=${JSON.stringify(r.role)} — a refusal must be the live alert region, so reading anything else means this arm is looking at the wrong node`,
  ).toBe("alert");
  return r;
}

/** Same, for the success arm: `data-tone="success"` and no alert role. */
async function successBanner(page: Page): Promise<Banner> {
  await expect(
    banner(page),
    'no save banner: nothing on this page carries data-testid="action-msg" with data-tone="success"',
  ).toHaveAttribute("data-tone", "success", { timeout: 30_000 });
  await expect(banner(page), "the save banner rendered empty").toContainText(/\S/, {
    timeout: 30_000,
  });
  return readBanner(page);
}

/**
 * The decoy, pinned: the sidebar's `Help` group label is on this screen NOW.
 *
 * It is the exact `text-xs` <p> the pre-fix read landed on (see the block above),
 * so asserting it is present — while the arm reads the banner — is what keeps
 * this a regression test for the flake instead of a story about one.
 */
async function expectNavDecoyPresent(page: Page) {
  await expect(
    page.locator("p#nav-desktop-grp-help"),
    "the sidebar's `Help` group label is not on this page any more, so this arm no longer pins the read it exists to pin",
  ).toHaveText(DICT["navgroup.help"].en);
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
    await stubAPI(
      page,
      {
        status: 409,
        // The server's wire body: Malay prose + the machine code. The prose must
        // NEVER reach the page.
        body: { error: "slug borang telah diambil borang lain", code: "form.slugInUse" },
      },
      SLOW_REFUSAL_MS,
    );

    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForSelector('input[value="demo-form"]', { timeout: 30_000 });

    await saveGeneralTab(page, DICT["fs.save"].en);
    const r = await failureBanner(page);
    await expectNavDecoyPresent(page);
    const title = await page.title();
    // Report the OBSERVED values so the evidence is in the run log, not inferred.
    console.log(`OBSERVED title=${JSON.stringify(title)}`);
    console.log(`OBSERVED message=${JSON.stringify(r.text)}`);
    console.log(`OBSERVED tone=${JSON.stringify(r.tone)} role=${JSON.stringify(r.role)}`);
    console.log(`OBSERVED lang=${await page.evaluate(() => document.documentElement.lang)}`);

    expect(r.text).toBe(DICT["apiErr.formSlugInUse"].en);
    expect(r.text).not.toContain("telah diambil");
    expect(r.text).not.toBe(DICT["prof.failed"].en);
  });

  test("400 form.slugInvalid renders the rule sentence, not the server prose", async ({ page }) => {
    await page.addInitScript(() => {
      try {
        window.localStorage.clear();
      } catch {}
    });
    await signIn(page);
    await stubAPI(
      page,
      {
        status: 400,
        body: {
          error: "form slug must be 1-40 characters of a-z, 0-9 or -",
          code: "form.slugInvalid",
        },
      },
      SLOW_REFUSAL_MS,
    );

    await page.goto(`/form-settings?form=${FORM_ID}`);
    await page.waitForSelector('input[value="demo-form"]', { timeout: 30_000 });

    await saveGeneralTab(page, DICT["fs.save"].en);
    // This arm waited on "1-40 characters", which `fs.slugHint` prints statically
    // on every render — so the wait proved nothing and the read ran against
    // whatever 12px paragraph was last. It now waits on the banner itself, with
    // the refusal deliberately held back (SLOW_REFUSAL_MS) so that lag is a
    // certainty in every run rather than a 1-in-32 coincidence of host load.
    const r = await failureBanner(page);
    await expectNavDecoyPresent(page);
    console.log(`OBSERVED invalid message=${JSON.stringify(r.text)}`);
    console.log(`OBSERVED invalid tone=${JSON.stringify(r.tone)} role=${JSON.stringify(r.role)}`);
    expect(r.text).toBe(DICT["apiErr.formSlugInvalid"].en);
    expect(r.text).not.toContain("1-40 characters of a-z, 0-9 or -");
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
    const r = await successBanner(page);
    console.log(`OBSERVED success message=${JSON.stringify(r.text)}`);
    console.log(`OBSERVED success tone=${JSON.stringify(r.tone)} role=${JSON.stringify(r.role)}`);
    expect(r.text).toContain(DICT["fs.saved"].en);
  });
});
