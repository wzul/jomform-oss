// Card t_1463b71e — the General tab RENDERS the logo picker and the "Allow
// embedding" toggle, so its Save must persist them.
//
// WHAT THIS PROVES (the unit suite cannot)
// The defect was purely a WIRING one: `saveGeneral` sent `{slug,title,theme}` to
// PATCH /forms/{id}, while `logo_url` and `allow_embed` were written only by a
// PUT /forms/{id}/settings that lived behind the *Confirmation* tab's Save. A
// merchant picked a logo, pressed the Save next to it, read "Saved ✓" — and the
// reload removed the badge, because nothing had written it. No unit test can see
// that: the handler was correct and the route was correct; the button was not
// wired to it. This spec drives the real button in a real browser and reads the
// request the page actually made.
//
// The API is stubbed at the browser level (no Go server, session or database),
// with the fake server HOLDING the row so a save + reload reads back what was
// written — i.e. the same round trip a merchant gets, and the same round trip
// that used to discard the logo.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

const FORM_ID = "form_general_save_guard";
const NEW_LOGO = "https://demo.example.com/media/picked-logo.png";
const OLD_REDIRECT = "https://demo.example.com/thanks";

type SettingsBody = {
  redirect_success?: string;
  redirect_failure?: string;
  allow_embed?: boolean;
  logo_url?: string;
  failure_message?: string;
};

type Stub = {
  /** Every body the page PUT to /forms/{id}/settings, in order. */
  settingsPuts: SettingsBody[];
  /** Every method+url the page called, so "it saved nothing" is sayable. */
  calls: string[];
};

/**
 * stubAPI answers every /api/* call the page makes. Routes are registered
 * generic-first because Playwright resolves matching handlers in REVERSE
 * registration order, so the specific ones below win.
 */
async function stubAPI(
  page: Page,
  opts: { settingsStatus?: number } = {},
): Promise<Stub> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const stub: Stub = { settingsPuts: [], calls: [] };
  // The fake server's row: written by the settings PUT, read back by every GET.
  const stored: Required<SettingsBody> = {
    logo_url: "",
    allow_embed: false,
    redirect_success: OLD_REDIRECT,
    redirect_failure: "",
    failure_message: "",
  };

  await page.route(/(.*)\/api\/.*$/, (route) => {
    stub.calls.push(`${route.request().method()} ${route.request().url()}`);
    return route.fulfill(json(200, {}));
  });
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json(200, {
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  // The profile must not pin a locale: this reading is the ENGLISH default,
  // i.e. what a new visitor gets.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json(200, { lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (route) =>
    route.fulfill(json(200, { feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (route) =>
    route.fulfill(json(200, { confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (route) =>
    route.fulfill(json(200, [])),
  );
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (route) => route.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json(200, [])));
  // The status POST a successful General save makes after the PATCH.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/status(\?.*)?$/, (route) =>
    route.fulfill(json(200, { ok: true })),
  );
  // PUT /forms/{id}/settings — the write this card is about. Registered before
  // the form GET/PATCH so it is matched first for its own path.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/settings(\?.*)?$/, (route) => {
    expect(
      route.request().method(),
      "the display settings are written by PUT /forms/{id}/settings",
    ).toBe("PUT");
    const body = route.request().postDataJSON() as SettingsBody;
    stub.settingsPuts.push(body);
    const status = opts.settingsStatus ?? 200;
    if (status !== 200) {
      return route.fulfill(json(status, { error: "tetapan gagal disimpan", code: "" }));
    }
    stored.logo_url = body.logo_url ?? stored.logo_url;
    stored.allow_embed = body.allow_embed ?? stored.allow_embed;
    stored.redirect_success = body.redirect_success ?? stored.redirect_success;
    stored.redirect_failure = body.redirect_failure ?? stored.redirect_failure;
    stored.failure_message = body.failure_message ?? stored.failure_message;
    return route.fulfill(json(200, { status: "saved" }));
  });
  // The form itself: GET answers the fake server's row (so a reload reads back
  // what was saved), PATCH answers the title/slug/theme write.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) => {
    if (route.request().method() === "PATCH") {
      return route.fulfill(json(200, { status: "updated", slug: "demo-form" }));
    }
    return route.fulfill(
      json(200, {
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://demo.example.com/demo-form",
        mode: "payment",
        config: { blocks: [] },
        theme: {},
        ...stored,
      }),
    );
  });

  return stub;
}

/** The General tab's Save — scoped to the page's first card so a later tab's
 *  identically-labelled control can never be the one clicked. */
async function saveGeneralTab(page: Page) {
  const section = page.locator("section").filter({ has: page.locator("h2") }).first();
  await expect(section.getByRole("heading", { name: DICT["fs.general"].en })).toBeVisible();
  await section.getByRole("button", { name: DICT["fs.save"].en, exact: true }).first().click();
}

/** The page's ONE save-feedback node (data-testid="action-msg"). */
const banner = (page: Page) => page.getByTestId("action-msg");

async function readBanner(page: Page) {
  return (await page.evaluate(`(() => {
    const el = document.querySelector('[data-testid="action-msg"]');
    if (!el) return { present: false, text: "", tone: "", role: "" };
    return {
      present: true,
      text: (el.textContent || "").trim(),
      tone: el.getAttribute("data-tone") || "",
      role: el.getAttribute("role") || "",
    };
  })()`) ) as { present: boolean; text: string; tone: string; role: string };
}

/** Open the media picker and set the logo through its PASTED-URL control — the
 *  path a merchant without an upload uses, and the one that needs no upload
 *  endpoint (so the stubbed server stays honest). */
async function pickLogoByPaste(page: Page, url: string) {
  await page.getByRole("button", { name: DICT["mediaPicker.choose"].en, exact: true }).click();
  await page.getByRole("button", { name: DICT["mediaPicker.pasteUrl"].en, exact: true }).click();
  const field = page.locator('input[placeholder="https://cdn.example/logo.png"]');
  await field.fill(url);
  await page.getByRole("button", { name: DICT["mediaPicker.useUrl"].en, exact: true }).click();
}

/** The logo the field is currently showing, read out of the live DOM. */
async function logoReading(page: Page) {
  return (await page.evaluate(`(() => {
    const noLogo = ${JSON.stringify(DICT["mediaPicker.noLogo"].en)};
    const img = Array.from(document.querySelectorAll("img")).find((i) =>
      (i.getAttribute("src") || "").includes("/media/"));
    return {
      noLogo: document.body.innerText.includes(noLogo),
      imgSrc: img ? img.getAttribute("src") : "",
      removeButton: Array.from(document.querySelectorAll("button"))
        .some((b) => (b.textContent || "").trim() === ${JSON.stringify(DICT["mediaPicker.remove"].en)}),
    };
  })()`) ) as { noLogo: boolean; imgSrc: string; removeButton: boolean };
}

test.beforeEach(async ({ page }) => {
  // A FRESH state per test: no cookie, no localStorage, no jf_lang left by an
  // earlier card. The profile answers lang:"" so this is the English default.
  await page.addInitScript(() => {
    try {
      window.localStorage.clear();
    } catch {}
  });
  await signIn(page);
});

test("the General tab's Save writes the logo AND the embed toggle it renders", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await stubAPI(page);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  await expect(page.locator('input[value="demo-form"]')).toBeVisible({ timeout: 45_000 });

  // The form starts with no logo and embedding OFF (the measured prod row).
  const before = await logoReading(page);
  console.log(`OBSERVED before save: ${JSON.stringify(before)}`);
  expect(before.removeButton, "premise: no logo is set, so the field shows the empty state").toBe(false);

  await pickLogoByPaste(page, NEW_LOGO);
  // The toggle the card names: OFF → ON, a visible change with no home in the
  // PATCH this button used to send.
  const embed = page.getByRole("checkbox", { name: DICT["bld.allowEmbed"].en });
  await expect(embed, "premise: the embed toggle renders on the General tab").toBeVisible();
  expect(await embed.isChecked(), "premise: the stored row has allow_embed=false").toBe(false);
  await embed.check();

  await saveGeneralTab(page);

  await expect(banner(page), "no save banner rendered").toHaveAttribute("data-tone", "success", {
    timeout: 30_000,
  });
  const saved = await readBanner(page);
  console.log(`OBSERVED banner: ${JSON.stringify(saved)}`);
  console.log(`OBSERVED settings PUTs: ${JSON.stringify(stub.settingsPuts)}`);
  console.log(`OBSERVED calls: ${JSON.stringify(stub.calls.filter((c) => c.includes("/api/rest/forms")))}`);

  expect(
    stub.settingsPuts.length,
    `the General Save made ${stub.settingsPuts.length} settings write(s) — the logo and the ` +
      `embed toggle it renders were never sent, which is the defect: the button said "Saved ✓" ` +
      `and the next reload dropped the logo. Calls made: ${stub.calls.join(", ")}`,
  ).toBe(1);
  const body = stub.settingsPuts[0];
  expect(body.logo_url, "the picked logo must be in the settings write").toBe(NEW_LOGO);
  expect(body.allow_embed, "the toggle must be sent EXPLICITLY — allow_embed defaults to true when omitted").toBe(
    true,
  );
  // Sending these two must not blank the fields the same PUT also carries.
  expect(body.redirect_success, "the redirect already stored must survive this save").toBe(OLD_REDIRECT);
  expect(body.redirect_failure).toBe("");
  expect(body.failure_message).toBe("");

  // …and the value survives the page's own reload — the step that used to
  // remove the badge with nothing said.
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.locator('input[value="demo-form"]')).toBeVisible({ timeout: 45_000 });
  const after = await logoReading(page);
  console.log(`OBSERVED after reload: ${JSON.stringify(after)}`);
  expect(after.noLogo, "after the reload the logo field must still show the picked logo").toBe(false);
  expect(after.imgSrc, "the reloaded field must point at the stored url").toContain("/media/picked-logo.png");
  await expect(
    page.getByRole("checkbox", { name: DICT["bld.allowEmbed"].en }),
    "the embed toggle must come back ON after the reload",
  ).toBeChecked();
});

test("a settings write that fails is VISIBLE, and no success is claimed", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await stubAPI(page, { settingsStatus: 500 });
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator('input[value="demo-form"]')).toBeVisible({ timeout: 45_000 });

  await pickLogoByPaste(page, NEW_LOGO);
  await saveGeneralTab(page);

  await expect(banner(page), "a refused settings write must be a failure banner").toHaveAttribute(
    "data-tone",
    "failure",
    { timeout: 30_000 },
  );
  const r = await readBanner(page);
  console.log(`OBSERVED failure banner: ${JSON.stringify(r)}`);
  expect(r.text.trim().length, "the failure banner rendered empty").toBeGreaterThan(0);
  expect(r.text).not.toContain(DICT["fs.saved"].en.replace(/\s*✓\s*$/, ""));
  expect(stub.settingsPuts.length, "the settings write was attempted").toBe(1);
  // The merchant's input is NOT discarded by a failed save (the reported
  // symptom was the logo silently vanishing from the preview).
  const after = await logoReading(page);
  console.log(`OBSERVED after the failed save: ${JSON.stringify(after)}`);
  expect(after.removeButton, "the picked logo must still be in the field after a failed save").toBe(true);
});

test("the Confirmation tab no longer offers two identically-labelled Save buttons", async ({ page }) => {
  test.setTimeout(120_000);
  await stubAPI(page);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator('input[value="demo-form"]')).toBeVisible({ timeout: 45_000 });

  // The tab strip is a <nav> of plain buttons (no role="tab"), so it is
  // addressed by its container rather than by an ARIA role.
  await page.locator("nav").getByRole("button", { name: DICT["fs.confirmation"].en, exact: true }).click();
  await expect(
    page.locator("section").getByRole("heading", { name: DICT["fs.confirmation"].en, exact: true }),
  ).toBeVisible();

  const labels: string[] = (await page.evaluate(`(() => {
    return Array.from(document.querySelectorAll("section button"))
      .map((b) => (b.textContent || "").trim());
  })()`) ) as string[];
  console.log(`OBSERVED Confirmation tab buttons: ${JSON.stringify(labels)}`);

  expect(labels, "the display-settings save names what it writes").toContain(
    DICT["fs.saveConfirmationSettings"].en,
  );
  expect(labels, "the confirmations save names what it writes").toContain(DICT["fs.saveConfirmations"].en);
  expect(
    labels.filter((l) => l === DICT["fs.save"].en).length,
    "no two buttons on this tab may share the bare 'Save' label — that is what made a dropped field invisible",
  ).toBe(0);
});
