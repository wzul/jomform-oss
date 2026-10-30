// Regression guard for card t_02338e21 — a dashboard-language choice made
// through the real /profile picker was SILENTLY REVERTED when it landed before
// the i18n provider's mount read `GET /api/auth/profile` resolved.
//
// THE DEFECT
//   `I18nProvider`'s mount effect fetches the profile and then applied its
//   `lang` unconditionally (`setLangState(d.lang)` + the `jf_lang` mirror). That
//   answer is the row as it was WHEN THE GET WAS SERVED, so a POST from
//   `setLang()` that committed while the GET was in flight was overwritten by a
//   read that predated it. The write itself succeeded: the server kept the new
//   language, while the picker, `document.documentElement.lang` and
//   `localStorage.jf_lang` all flipped back to the previous one — and nothing
//   reconciled afterwards, so only a full reload showed the user their own
//   choice. Measured by QA on prod v1.22.27 (web 662d5b69), fresh browser
//   state, /profile opened by URL and the picker driven at once: 4/4 choices
//   (ms, zh, ta, ar) showed `/api/auth/me` on the NEW language and the page on
//   the OLD one. Control: the same page left 3s to settle applied all four
//   immediately and kept them, which is why the control itself was fine and the
//   bug was the race, not the wiring.
//
// WHY THIS IS A BROWSER MEASUREMENT
//   The claim is about ORDER, not about a line of source: "the answer to a read
//   issued at mount must not overwrite a choice made after it". A source guard
//   can see that a guard exists (src/lib/langPlacement.test.ts pins that shape);
//   only a browser can show that the choice survives the late answer on the real
//   picker. And a plain load is a race — measured >1s on prod but milliseconds
//   against a local stub, which is exactly the flake shape that teaches a lane
//   to mute a gate. So `/api/auth/profile` is HELD OPEN at the browser level
//   (`e2e/cf-status-prefetch.spec.ts` is the same pattern): the pre-POST window
//   is deterministic for as long as the test wants, the user path is driven for
//   real, and the stale answer is released only after the pick has been made.
//
// THE FOUR ARMS
//   1. THE DEFECT (ms): the picker is driven while the profile read is pending;
//      the read is then released with the PRE-POST value (`en`). The choice must
//      still be everywhere the user can see it — the select, the h1 and the
//      picker's own label in Malay, `documentElement.lang`, and the `jf_lang`
//      mirror. Asserting the intermediate state as well (the pick applies at
//      once) is what separates "the control is wired" from "the answer wins".
//   2. THE SAME WINDOW IN `ar`: the revert was also a layout event — the
//      dashboard's `dir` follows the language — so the arm pins `lang=ar` and
//      `dir=rtl` surviving the stale answer.
//   3. THE PRE-EXISTING RULE STILL HOLDS: with NOBODY picking, the profile must
//      still beat the browser's `jf_lang` (that is what "per-profile" means —
//      src/lib/langPlacement.test.ts pins its source shape). This arm is what
//      stops the fix from being "ignore the profile read", which would keep the
//      revert from happening by breaking the feature.
//   4. THE CONTROL, IN A BROWSER: a pick made after the read has resolved
//      applies and sticks. The card's own control case, so a fix that only
//      works before the read cannot pass.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

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

// Enough session for /profile to render its tabs (the identity fields hydrate
// from this bundle, so the page needs no per-mount profile round-trip).
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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

const PICKER = "#prof-lang";

/** The strings the page really renders, in the locale the picker is set to. */
const LABEL = (l: "en" | "ms" | "zh" | "ta" | "ar") => DICT["prof.language"][l];
const TITLE = (l: "en" | "ms" | "zh" | "ta" | "ar") => DICT["prof.title"][l];

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

async function stubAPI(page: Page) {
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win. The
  // catch-all is also what answers the language POST (a bare 200), so the write
  // always "succeeds" here — the defect is not a failed write.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/(passkeys|social|mcp-agents|trust-devices)(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/auth\/trust-status(\?.*)?$/, (r) => r.fulfill(json({ trusted: false })));
}

/** A fresh browser state per case: a `jf_lang` (or a session) left behind by an
 *  earlier card would render a state no new visitor has, and every assertion
 *  here compares against the locale the picker really is in. */
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
    } catch {
      /* storage unavailable */
    }
  });
});

/**
 * Hold the i18n provider's mount read open and answer it with `answer` — the
 * value the server held when the request was SERVED, i.e. the pre-POST one.
 *
 * `arrived` resolves once the request has actually reached the route, so no arm
 * reads the DOM before the fetch it is racing has started. `served` is a
 * `page.waitForResponse` for the same URL, to be awaited AFTER `release()`: it
 * is what tells the arm the stale answer has been delivered and its `.then`
 * chain has run.
 */
async function holdProfileRead(page: Page, answer: { lang: string }) {
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  let arrived!: () => void;
  const pending = new Promise<void>((resolve) => {
    arrived = resolve;
  });
  const urls: string[] = [];
  await page.route(/\/api\/auth\/profile$/, async (route) => {
    urls.push(route.request().url());
    arrived();
    await released;
    await route.fulfill(json(answer));
  });
  const served = () =>
    page.waitForResponse((r) => /\/api\/auth\/profile$/.test(r.url()) && r.request().method() === "GET", { timeout: 20_000 });
  return { release, arrived: pending, served, urls };
}

/** The DOM facts the card's evidence table is about, read from the live page. */
type PickerState = {
  value: string;
  label: string;
  h1: string;
  htmlLang: string;
  dir: string;
  stored: string | null;
  optionCount: number;
};

async function readPicker(page: Page): Promise<PickerState> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel) as HTMLSelectElement | null;
    const lab = document.querySelector(`label[for="${sel.slice(1)}"]`);
    const h1 = document.querySelector("h1");
    return {
      value: el ? el.value : "<no picker>",
      label: ((lab && lab.textContent) || "").trim(),
      h1: ((h1 && h1.textContent) || "").trim(),
      htmlLang: document.documentElement.lang,
      dir: document.documentElement.dir,
      stored: (() => {
        try {
          return localStorage.getItem("jf_lang");
        } catch {
          return null;
        }
      })(),
      optionCount: el ? el.options.length : 0,
    };
  }, PICKER);
}

/**
 * The page is STATICALLY PRERENDERED, so the served HTML already carries the
 * picker at its default value; only after HYDRATION does changing it reach the
 * i18n context. React leaves `__reactFiber$…`/`__reactProps$…` on a node it has
 * taken over, which is the deterministic "this node is hydrated" signal (no
 * sleep), and the same marker `e2e/a11y-profile-tabs.spec.ts` waits on.
 */
async function waitForHydratedPicker(page: Page) {
  await page.waitForFunction(
    (sel) => {
      const el = document.querySelector(sel);
      return !!el && Object.keys(el).some((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactProps$"));
    },
    PICKER,
    { timeout: 20_000 },
  );
}

/** Every language POST this page has issued, as raw bodies. */
function collectLangPosts(page: Page): string[] {
  const posts: string[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().endsWith("/api/auth/profile/lang")) posts.push(r.postData() ?? "");
  });
  return posts;
}

test("a pick made while the profile read is in flight is not reverted by it (ms)", async ({ page }) => {
  await stubAPI(page);
  const held = await holdProfileRead(page, { lang: "en" }); // the pre-POST read
  const posts = collectLangPosts(page);

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await held.arrived; // the read is issued and held open — the whole window
  await waitForHydratedPicker(page);

  const before = await readPicker(page);
  expect(before.value, "the picker should start on the value the page was served with").toBe("en");
  expect(before.htmlLang).toBe("en");

  // The user arrives and picks at once, inside the window.
  await page.selectOption(PICKER, "ms");

  // The write really goes out, and it is the choice (the premise of the race).
  await expect.poll(() => posts.length, { message: "no POST /api/auth/profile/lang was issued" }).toBe(1);
  expect(posts[0]).toContain('"lang":"ms"');

  // …and it applies at once: this half already worked before the fix, and it is
  // asserted so a regression in the WRITE cannot hide behind the race.
  const picked = await readPicker(page);
  expect(picked.value).toBe("ms");
  expect(picked.stored).toBe("ms");
  expect(picked.htmlLang).toBe("ms");
  expect(picked.label).toBe(LABEL("ms"));
  expect(picked.h1).toBe(TITLE("ms"));

  // Now deliver the stale answer — the row as it was before the POST.
  const served = held.served();
  held.release();
  await served;
  // The provider's `.then` is a microtask chain behind that response; give the
  // commit a turn so a revert would be painted before the assertion reads.
  await page.waitForTimeout(400);

  // THE DEFECT: on the broken code every one of these flipped back to `en`
  // while the server kept `ms`, so the user saw their choice disappear.
  const after = await readPicker(page);
  expect(after.value, "the late profile read reverted the picker").toBe("ms");
  expect(after.stored, "the late profile read reverted the jf_lang mirror").toBe("ms");
  expect(after.htmlLang, "the late profile read reverted the document language").toBe("ms");
  expect(after.label, "the picker's label is not in the chosen language").toBe(LABEL("ms"));
  expect(after.h1, "the page's heading is not in the chosen language").toBe(TITLE("ms"));
  expect(after.dir).toBe("ltr");
});

test("the same window in ar keeps the choice AND its direction (rtl)", async ({ page }) => {
  await stubAPI(page);
  const held = await holdProfileRead(page, { lang: "en" });
  const posts = collectLangPosts(page);

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await held.arrived;
  await waitForHydratedPicker(page);
  expect((await readPicker(page)).dir).toBe("ltr");

  await page.selectOption(PICKER, "ar");
  await expect.poll(() => posts.length).toBe(1);
  expect(posts[0]).toContain('"lang":"ar"');

  const served = held.served();
  held.release();
  await served;
  await page.waitForTimeout(400);

  const after = await readPicker(page);
  expect(after.value).toBe("ar");
  expect(after.htmlLang).toBe("ar");
  // A revert was a LAYOUT event too: the dashboard's direction follows the
  // language, so the stale answer flipped the page back to LTR as well.
  expect(after.dir, "the late profile read reverted the document direction").toBe("rtl");
  expect(after.label).toBe(LABEL("ar"));
  expect(after.h1).toBe(TITLE("ar"));
});

test("with nobody picking, the profile still wins over the browser value", async ({ page }) => {
  // The pre-existing rule this fix must NOT break (src/lib/langPlacement.test.ts
  // pins its source shape): the setting is per-PROFILE. Ignoring the mount read
  // outright would "fix" the revert by deleting the feature.
  await page.addInitScript(() => {
    try {
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  await stubAPI(page);
  const held = await holdProfileRead(page, { lang: "zh" });

  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await held.arrived;
  await waitForHydratedPicker(page);

  const served = held.served();
  held.release();
  await served;
  await page.waitForTimeout(400);

  const after = await readPicker(page);
  expect(after.value, "the profile's language did not win over the browser's").toBe("zh");
  expect(after.stored).toBe("zh");
  expect(after.htmlLang).toBe("zh");
  expect(after.label).toBe(LABEL("zh"));
  expect(after.h1).toBe(TITLE("zh"));
});

test("a pick made after the read resolved still applies and sticks", async ({ page }) => {
  // The card's own control case ("with the page left to settle, choices apply
  // and stick"), in a browser: a fix that only worked BEFORE the read would
  // pass the arm above and fail the user who picks a second later.
  await stubAPI(page);
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  const posts = collectLangPosts(page);

  const read = page.waitForResponse((r) => /\/api\/auth\/profile$/.test(r.url()) && r.request().method() === "GET");
  await page.goto("/profile", { waitUntil: "domcontentloaded" });
  await read;
  await waitForHydratedPicker(page);

  await page.selectOption(PICKER, "ta");
  await expect.poll(() => posts.length).toBe(1);
  expect(posts[0]).toContain('"lang":"ta"');

  const settled = await readPicker(page);
  expect(settled.value).toBe("ta");
  expect(settled.htmlLang).toBe("ta");

  // Still there a moment later — no late writer to fight with in this case, so
  // this arm guards the ordinary path, not the race.
  await page.waitForTimeout(400);
  const after = await readPicker(page);
  expect(after.value).toBe("ta");
  expect(after.stored).toBe("ta");
  expect(after.label).toBe(LABEL("ta"));
  expect(after.h1).toBe(TITLE("ta"));
});
