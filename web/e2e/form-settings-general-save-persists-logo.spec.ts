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
//
// CARD t_19c55242 adds the ORDERING promise from the fix's own comment:
// `saveGeneral` checks the https rule BEFORE it PATCHes, so a refused logo
// cannot leave the title/slug/status half-written. The cases at the bottom
// seed a stored non-https `logo_url` (a legacy row — the shipped picker
// refuses such a paste, so this is the only reachable path to the guard) and
// assert that the refusal sends NO write at all.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

const FORM_ID = "form_general_save_guard";
const NEW_LOGO = "https://demo.example.com/media/picked-logo.png";
const OLD_REDIRECT = "https://demo.example.com/thanks";
// Card t_19c55242: the scheme a LEGACY row can still hold. The shipped UI
// cannot produce it (MediaPicker refuses a non-https paste before `fsLogo` is
// ever set), so it is seeded through the form GET — the only way the
// page-level guard is reachable at all.
const BAD_LOGO = "http://x/y.png";
const CHANGED_TITLE = "Demo form CHANGED";

type SettingsBody = {
  redirect_success?: string;
  redirect_failure?: string;
  allow_embed?: boolean;
  logo_url?: string;
  failure_message?: string;
};

/** The title/slug/theme write (PATCH /forms/{id}). Card t_106955f6 reads its
 *  `theme` map: it must carry the keys the General tab does not own. */
type PatchBody = { title?: string; slug?: string; theme?: Record<string, unknown> };

type Stub = {
  /** Every body the page PUT to /forms/{id}/settings, in order. */
  settingsPuts: SettingsBody[];
  /** Every method+url the page asked for, so "it saved nothing" is sayable. */
  calls: string[];
  /** Every body the page PATCHed to /forms/{id} — the title/slug/theme write. */
  formPatches: PatchBody[];
};

/**
 * stubAPI answers every /api/* call the page makes. Routes are registered
 * generic-first because Playwright resolves matching handlers in REVERSE
 * registration order, so the specific ones below win.
 */
async function stubAPI(
  page: Page,
  // `storedLogo` seeds a `logo_url` the server ALREADY holds — a legacy row.
  // The picker refuses a non-https paste, so this is the only route by which a
  // non-https `fsLogo` can exist in the page (card t_19c55242).
  //
  // `storedTheme` seeds the `theme` jsonb the server holds (card t_106955f6):
  // the General Save's payload is judged against it, because the real PATCH
  // REPLACES the whole document when the key is present.
  opts: { settingsStatus?: number; storedLogo?: string; storedTheme?: Record<string, unknown> } = {},
): Promise<Stub> {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const stub: Stub = { settingsPuts: [], calls: [], formPatches: [] };
  // The fake server's row: written by the settings PUT, read back by every GET.
  const stored: Required<SettingsBody> = {
    logo_url: opts.storedLogo ?? "",
    allow_embed: false,
    redirect_success: OLD_REDIRECT,
    redirect_failure: "",
    failure_message: "",
  };

  await page.route(/(.*)\/api\/.*$/, (route) => {
    return route.fulfill(json(200, {}));
  });
  // Record EVERY request the page makes at the BROWSER level, not from inside
  // the catch-all route above: Playwright resolves the LAST matching handler,
  // so a specific route SHADOWS the catch-all and a request it answers is never
  // seen there. The form route below swallows `PATCH /forms/{id}?workspace=…`
  // too — its `[^/]+$` matches the query as well — which is exactly how a
  // "no write was sent" assertion once stayed green while the PATCH happened.
  page.on("request", (r) => stub.calls.push(`${r.method()} ${r.url()}`));
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
    // The REAL API refuses a non-https logo_url on this path (urlguard → 400,
    // the Malay "mesti bermula dengan https://" message). The fake server does
    // the same on purpose: it keeps this spec from resting on the page's own
    // guard being the only refusal, so a refactor that drops that guard still
    // fails here — and it fails on the ORDER (the PATCH already went out), not
    // on a green banner.
    if (body.logo_url && !body.logo_url.trim().startsWith("https://")) {
      return route.fulfill(
        json(400, { error: "logo_url must start with https://", code: "" }),
      );
    }
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
  //
  // The fake server holds `theme` the way the real one does (card t_106955f6):
  // a PATCH carrying a theme REPLACES the stored document, so a payload that
  // dropped a key really does lose it on the next GET — the round trip the
  // merchant sees, not just a recorded body.
  let storedTheme: Record<string, unknown> = { ...(opts.storedTheme ?? {}) };
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) => {
    if (route.request().method() === "PATCH") {
      const body = route.request().postDataJSON() as PatchBody;
      stub.formPatches.push(body);
      if (body.theme && typeof body.theme === "object") storedTheme = { ...body.theme };
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
        theme: storedTheme,
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

/** Every request the page made that CHANGES something — the measured shape of
 *  a refused save on prod was "zero non-GET requests" (card t_9ca1f0f8). */
const writes = (stub: Stub) => stub.calls.filter((c) => !c.startsWith("GET "));

/** The logo the page currently holds, read out of the LIVE DOM: the preview
 *  <img> for an unknown-mime url (a stored legacy http:// value), plus the
 *  Remove control that only renders once `fsLogo` is non-empty. */
async function seededLogoReading(page: Page, url: string) {
  return (await page.evaluate(`(() => {
    const srcs = Array.from(document.querySelectorAll("img")).map((i) => i.getAttribute("src") || "");
    return {
      hasSrc: srcs.includes(${JSON.stringify(url)}),
      removeButton: Array.from(document.querySelectorAll("button"))
        .some((b) => (b.textContent || "").trim() === ${JSON.stringify(DICT["mediaPicker.remove"].en)}),
    };
  })()`) ) as { hasSrc: boolean; removeButton: boolean };
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

// Card t_19c55242 — the ORDERING the fix's own comment promises.
//
// `saveGeneral` says «The https rule is checked BEFORE the PATCH so a refused
// logo does not leave the title/slug/status half-written». That is a promise
// about the ORDER of two writes, and nothing above can see it: every case
// starts from a row whose logo is empty or https, so the guard never fires.
// A refactor that moved the PATCH above the guard — or dropped the guard and
// let the PATCH store the title before the settings PUT was refused — keeps
// all three green while a merchant's title lands under a rejected logo.
//
// The state is SEEDED through the form GET on purpose: a stored non-https
// `logo_url` is the only way this guard is reachable, because the media
// picker refuses a non-https paste before `fsLogo` can hold it.
test("a stored non-https logo is refused BEFORE the title PATCH, so nothing is half-written", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stub = await stubAPI(page, { storedLogo: BAD_LOGO });
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("#fs-title")).toHaveValue("Demo form", { timeout: 45_000 });

  // PREMISE: the page really holds the stored non-https value. Without this the
  // test would pass by saving a form with no logo at all.
  const seeded = await seededLogoReading(page, BAD_LOGO);
  console.log(`OBSERVED seeded logo: ${JSON.stringify(seeded)}`);
  expect(seeded.removeButton, "premise: the field holds a logo, so it offers Remove").toBe(true);
  expect(seeded.hasSrc, `premise: the field previews the stored ${BAD_LOGO}`).toBe(true);

  // A REAL change on each of the two write paths the save owns: the title (the
  // PATCH body) and the embed toggle (the settings PUT body). If the guard ran
  // after the PATCH, this title would land in the database.
  await page.locator("#fs-title").fill(CHANGED_TITLE);
  const embed = page.getByRole("checkbox", { name: DICT["bld.allowEmbed"].en });
  expect(await embed.isChecked(), "premise: the stored row has allow_embed=false").toBe(false);
  await embed.check();

  await saveGeneralTab(page);

  await expect(banner(page), "a refused logo must paint a FAILURE banner").toHaveAttribute(
    "data-tone",
    "failure",
    { timeout: 30_000 },
  );
  const r = await readBanner(page);
  const ws = writes(stub);
  console.log(`OBSERVED refusal banner: ${JSON.stringify(r)}`);
  console.log(`OBSERVED non-GET calls after the click: ${JSON.stringify(ws)}`);
  console.log(`OBSERVED form PATCH bodies: ${JSON.stringify(stub.formPatches)}`);

  // THE ORDERING ASSERTION — first, because it is what this case exists for.
  // Named separately from the blanket one below so a failure says which half of
  // the promise broke.
  expect(
    ws.filter((c) => c.startsWith("PATCH ") && c.includes("/api/rest/forms/")),
    `the title/slug PATCH ran BEFORE the logo was refused — that is the half-written save this ` +
      `guard exists to prevent. PATCH bodies: ${JSON.stringify(stub.formPatches)}. ` +
      `Calls: ${stub.calls.join(", ")}`,
  ).toEqual([]);
  expect(
    stub.formPatches,
    `the pending title was PATCHed to the form before the logo was refused. ` +
      `Bodies: ${JSON.stringify(stub.formPatches)}`,
  ).toEqual([]);
  expect(
    ws.filter((c) => c.includes("/settings")),
    `a /settings write was attempted for a logo the page itself refused. Calls: ${stub.calls.join(", ")}`,
  ).toEqual([]);
  expect(stub.settingsPuts.length, "no settings body may be PUT for a refused logo").toBe(0);
  expect(
    ws,
    `the refusal must send NO write at all — prod measured zero non-GET requests for this click. ` +
      `Calls: ${stub.calls.join(", ")}`,
  ).toEqual([]);

  expect(r.text, "the banner must be the URL-scheme sentence, not a generic failure").toBe(
    DICT["url.notHttps"].en,
  );
  expect(r.role, "the refusal must be announced, not only painted").toBe("alert");

  // …and the staged edit is still there, so a green run cannot come from the
  // Save never having been pressed.
  await expect(page.locator("#fs-title"), "the merchant's input is not discarded").toHaveValue(
    CHANGED_TITLE,
  );
  await expect(embed).toBeChecked();
});

// The Confirmation tab's Save goes through the SAME helper, so one assertion
// on it is enough — plus a POSITIVE CONTROL: with an https logo the identical
// button writes. Without that control a dead button would satisfy "no write".
test("the Confirmation tab's Save refuses the same stored non-https logo, and writes once it is https", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stub = await stubAPI(page, { storedLogo: BAD_LOGO });
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("#fs-title")).toHaveValue("Demo form", { timeout: 45_000 });

  const confirmationTab = page
    .locator("nav")
    .getByRole("button", { name: DICT["fs.confirmation"].en, exact: true });
  const confirmationSave = page.getByRole("button", {
    name: DICT["fs.saveConfirmationSettings"].en,
    exact: true,
  });
  await confirmationTab.click();
  await expect(
    page.locator("section").getByRole("heading", { name: DICT["fs.confirmation"].en, exact: true }),
  ).toBeVisible();
  await confirmationSave.click();

  await expect(banner(page), "the shared helper must report the refusal here too").toHaveAttribute(
    "data-tone",
    "failure",
    { timeout: 30_000 },
  );
  const refused = await readBanner(page);
  console.log(`OBSERVED Confirmation refusal: ${JSON.stringify(refused)}`);
  expect(refused.text).toBe(DICT["url.notHttps"].en);
  expect(refused.role).toBe("alert");
  expect(
    stub.settingsPuts.length,
    `the Confirmation Save PUT a settings body for a refused logo. Calls: ${stub.calls.join(", ")}`,
  ).toBe(0);
  expect(
    writes(stub),
    `the Confirmation Save sent a write for a refused logo. Calls: ${stub.calls.join(", ")}`,
  ).toEqual([]);

  // POSITIVE CONTROL — the same button with an https logo does write.
  await page
    .locator("nav")
    .getByRole("button", { name: DICT["fs.general"].en, exact: true })
    .click();
  await pickLogoByPaste(page, NEW_LOGO);
  await confirmationTab.click();
  await confirmationSave.click();
  await expect(banner(page), "the same Save must succeed once the logo is https").toHaveAttribute(
    "data-tone",
    "success",
    { timeout: 30_000 },
  );
  console.log(`OBSERVED Confirmation settings PUTs: ${JSON.stringify(stub.settingsPuts)}`);
  expect(stub.settingsPuts.length, "the control Save writes exactly once").toBe(1);
  expect(stub.settingsPuts[0].logo_url, "the control Save carries the https logo").toBe(NEW_LOGO);
});

// Card t_106955f6 — the General Save must not delete theme keys it does not own.
//
// THE DEFECT, measured on prod (form 08a43fd7…, v1.21.259), one click on this
// very button:
//
//   before  {"layout":"centered","primary":"#b45309","spacing":"comfortable",
//            "product_image_size":"large","product_image_layout":"image-top"}
//   after   {"primary":"#b45309","product_image_size":"large",
//            "product_image_layout":"image-top"}
//
// The API's PATCH REPLACES the whole `theme` jsonb when the key is present, and
// the page built that map from the four keys THIS TAB owns. `layout` and
// `spacing` are read by the block renderer's CSS conditionals
// (`{{if .ThemeLayout}}body{justify-content:…;padding:…} .card{max-width:…}{{end}}`,
// `{{if .ThemeSpacing}}.card{padding:…}{{end}}`), so a routine save silently
// re-styled the buyer's page — a form that served centring and 48px card padding
// started serving neither, with no error and nothing in the UI.
//
// WHY THIS CASE AND NOT A UNIT TEST ALONE
// The unit suite pins the payload builder (src/lib/formSettingsTheme.test.ts);
// this case pins that the PAGE USES IT — it drives the real button in a real
// browser and reads the body the browser actually sent. A handler that rebuilt
// the map inline, or a call site that passed the wrong base, is invisible to the
// builder's own tests.
//
// `storedTheme` seeds the row's jsonb. The fake server REPLACES it on a PATCH
// exactly like the API does, so a payload that dropped a key really loses it on
// the reload — the merchant's round trip, not just a recorded body.
const STORED_THEME: Record<string, unknown> = {
  layout: "centered",
  spacing: "comfortable",
  background: "#eeeeee",
  text: "#111111",
  card_background: "#ffffff",
  card_border_radius: "12px",
  font_family: "Georgia, serif",
  custom_css: ".card { box-shadow: none; }",
  custom_js: "window.__jfOld = true;",
  primary: "#5e6ad2",
  product_image_size: "large",
  product_image_layout: "image-top",
};

test("the General Save keeps every theme key this tab does not own", async ({ page }) => {
  test.setTimeout(120_000);
  const stub = await stubAPI(page, { storedTheme: STORED_THEME });
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("html"), "the reading is the ENGLISH default").toHaveAttribute("lang", "en");
  await expect(page.locator("#fs-title")).toHaveValue("Demo form", { timeout: 45_000 });

  // PREMISE: the page really READ the stored theme — the round trip starts from
  // it. Without this, an empty theme would make "nothing was deleted" vacuous.
  await expect(
    page.locator("#fs-theme-color"),
    "premise: the colour input is hydrated from the stored theme",
  ).toHaveValue("#5e6ad2");
  const jsField = page.locator(`textarea[aria-label="${DICT["bld.customJs"].en}"]`);
  expect(
    await jsField.inputValue(),
    "premise: the custom-JS field is hydrated from the stored theme",
  ).toBe("window.__jfOld = true;");

  // A real edit on this tab: the colour. Everything else it writes is unchanged,
  // which is exactly the case that used to delete the rest of the document.
  await page.locator("#fs-theme-color").fill("#b45309");
  await saveGeneralTab(page);
  await expect(banner(page), "the save must report success").toHaveAttribute("data-tone", "success", {
    timeout: 30_000,
  });
  await expect.poll(() => stub.formPatches.length, { timeout: 30_000 }).toBe(1);

  const first = stub.formPatches[0];
  console.log(`OBSERVED General Save PATCH body: ${JSON.stringify(first)}`);

  // THE DEFECT, asserted on the exact body the browser sent. `toBeDefined` first
  // so a body with NO theme is reported as such rather than as 12 mismatches.
  expect(first.theme, "the General Save must send a theme").toBeDefined();
  expect(
    first.theme,
    `the PATCH body replaced the theme document instead of updating it: ${JSON.stringify(first.theme)}. ` +
      `The API writes a present theme as-is, so every key missing here is DELETED from the form — and ` +
      `layout/spacing/custom_css are what the public page renders from.`,
  ).toEqual({ ...STORED_THEME, primary: "#b45309" });

  // The page's own reload must show the same document (the fake server replaced
  // its row with the PATCH body above) — the reading a merchant gets.
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.locator("#fs-title")).toHaveValue("Demo form", { timeout: 45_000 });
  expect(
    await page.locator(`textarea[aria-label="${DICT["bld.customJs"].en}"]`).inputValue(),
    "after the save+reload the stored custom JS is still there",
  ).toBe("window.__jfOld = true;");

  // ── The one owned key that can be REMOVED. Clearing the custom-JS field must
  // drop the key from the payload (a round trip that only ever ADDED keys would
  // make the field impossible to clear), while the unowned ones still survive.
  await page.locator("summary", { hasText: DICT["bld.customJs"].en }).click();
  await jsField.fill("");
  await saveGeneralTab(page);
  await expect.poll(() => stub.formPatches.length, { timeout: 30_000 }).toBe(2);

  const second = stub.formPatches[1];
  console.log(`OBSERVED second PATCH body (custom JS cleared): ${JSON.stringify(second)}`);
  const expectedCleared: Record<string, unknown> = { ...STORED_THEME, primary: "#b45309" };
  delete expectedCleared.custom_js;
  expect(
    second.theme,
    "a cleared custom-JS field must DROP the key, and must not take the rest of the theme with it",
  ).toEqual(expectedCleared);
  expect("custom_js" in (second.theme ?? {}), "an empty field must not store an empty custom_js").toBe(false);
});
