// Regression guard for the two /forms trash refusals that main grew after the
// #6177 base, and that this rebase converted off the server's prose.
//
// THE DEFECT CLASS (issue #6177). The API answers a failure with Malay prose
// (`{"error":"borang ini mempunyai jualan — …"}`) because it never learns the
// caller's language on a JSON call. The dashboard used to render that string
// verbatim:
//
//     setErr(d?.error || t("forms.purgeFail"));
//
// so an ENGLISH dashboard showed a Malay sentence. The fix routes the sentence
// through the machine `code` and the dashboard dictionary. Both of these sites
// are refusals that NAME the obstacle (the form has sales; another form took the
// slug), which is exactly the case where falling back to the page's generic
// "We couldn't delete this form." would hide the reason the merchant needs.
//
// WHAT THIS GUARD PROVES, on the real built page with a stubbed API:
//   1. the refusal renders the ENGLISH dictionary sentence for its code, and
//   2. the server's Malay prose appears NOWHERE on the page.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";

const TRASHED = {
  id: "form_sold_guard",
  slug: "sold-form",
  title: "A sold form",
  deleted_at: "2026-01-02T03:04:05Z",
  // `can_purge: true` so the purge control is offered; the SERVER still refuses
  // (that is the case under test — the UI's pre-computed hint is not the guard).
  can_purge: true,
  sales_count: 3,
};

// Verbatim wire prose from api/internal/httpapi/rest_handlers.go and
// rest_workspaces are deliberately NOT used here; these are the two bodies the
// page's code must translate. Malay is what the dashboard must never print.
const PURGE_PROSE =
  "borang ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan dengan memadam borang. Ia kekal dalam tong sampah.";
const RESTORE_PROSE =
  "tidak boleh dipulihkan: slug borang ini telah diambil borang lain — tukar slug borang itu dahulu, kemudian cuba pulihkan semula";

const EN_HAS_SALES =
  "This form has sales, so it can't be deleted for good — sales history must not be destroyed with the form. It stays in the trash.";
const EN_SLUG_TAKEN =
  "This form can't be restored: another form has taken its slug. Change that form's slug first, then restore this one.";

/**
 * stubAPI stubs the whole dashboard so /forms reaches its loaded trash view
 * without a Go server. The two refusal routes answer the 409 envelopes under
 * test; everything else is a benign empty payload.
 */
async function stubAPI(page: Page) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false };
  let trashCalls = 0;

  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json(200, {})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json(200, {
        me: ME,
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(200, ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json(200, { lang: "en" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json(200, { rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json(200, [])));
  await page.route(/(.*)\/api\/rest\/forms\/trash(\?.*)?$/, (r) => {
    // The FIRST listing (opening the Trash tab) returns the row. Every later
    // call HANGS, which keeps the refusal message on screen long enough to read:
    // `loadTrash()` clears `err` on success, so a normal second listing would wipe
    // the message in the same tick. That wipe is a PRE-EXISTING defect (filed
    // separately); hanging the reload isolates the string under test instead of
    // racing it.
    trashCalls += 1;
    if (trashCalls === 1) return r.fulfill(json(200, [TRASHED]));
    return new Promise(() => {});
  });
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/restore(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: RESTORE_PROSE, code: "form.slugTaken" })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/purge(\?.*)?$/, (r) =>
    r.fulfill(json(409, { error: PURGE_PROSE, code: "form.hasSales", sales_count: 3 })),
  );
}

async function openTrash(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });
  await stubAPI(page);
  await page.goto("/forms", { waitUntil: "networkidle" });

  // Switch to the Trash view (the live list is the default). Its tab renders
  // the trashed row's slug, so waiting for that is the loaded signal.
  await page.getByRole("tab", { name: /Trash/i }).click();
  await expect(page.locator("li", { hasText: TRASHED.slug }).first()).toBeVisible({ timeout: 45_000 });
}

test("a purge refused for sales shows the ENGLISH reason, not the Malay prose", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  // Open the purge dialog from the ROW (scoped: the dialog's own confirm button
  // carries the same label once it opens).
  const row = page.locator("li", { hasText: TRASHED.slug }).first();
  await row.getByRole("button", { name: "Delete for good", exact: true }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible({ timeout: 20_000 });
  await dialog.locator("#purge-confirm-slug").fill(TRASHED.slug);
  await dialog.getByRole("button", { name: "Delete for good", exact: true }).click();

  // The refusal lands in the page's error line (role=alert), beside the reason.
  const errLine = page.getByRole("alert").filter({ hasText: "sales" });
  await expect(errLine).toBeVisible({ timeout: 20_000 });
  const shown = (await errLine.innerText()).trim();
  expect(shown).toContain(EN_HAS_SALES);
  expect(shown).not.toContain(PURGE_PROSE);
  expect(shown).not.toContain("borang ini mempunyai jualan");
});

test("a restore refused over a re-claimed slug shows the ENGLISH reason, not the Malay prose", async ({ page }) => {
  test.setTimeout(120_000);
  await openTrash(page);

  const row = page.locator("li", { hasText: TRASHED.slug }).first();
  await row.getByRole("button", { name: "Restore", exact: true }).click();

  const errLine = page.getByRole("alert").filter({ hasText: "slug" });
  await expect(errLine).toBeVisible({ timeout: 20_000 });
  const shown = (await errLine.innerText()).trim();
  expect(shown).toContain(EN_SLUG_TAKEN);
  expect(shown).not.toContain(RESTORE_PROSE);
  expect(shown).not.toContain("tidak boleh dipulihkan");
});
