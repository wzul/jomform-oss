// The merchant's actual Shipping-tab gesture: delete the ONLY zone set, press
// "Save shipping rates", and see it stay deleted (card t_417fbacb).
//
// THE DEFECT, measured on prod v1.21.345. Form Settings → Shipping offers a
// Delete on every set — including the only one — and its empty view describes
// zero sets as a legitimate state ("No zone set yet. Add one, then add the zones
// and their rates."). The save that state implies was a 400:
//
//	PUT /api/rest/shipping?workspace=…  {"sets":[]}
//	  -> 400 {"code":"request.badRequest","error":"sekurang-kurangnya satu set zon diperlukan"}
//
// and the page rendered only its generic fallback, so the merchant read "Failed
// to save shipping rates" with no reason and the set was back after a reload.
//
// WHY THIS IS A BROWSER SPEC AND NOT A UNIT ONE. The Go guard
// (httpapi/rest_shipping_test.go, dbstore/shipping_test.go) proves the API now
// accepts the empty payload; only a rendered page proves the CONTROL reaches it —
// that Delete really removes the only set from the draft, that Save really posts
// `{"sets":[]}` (not a filtered list), and that the message the merchant reads is
// the one this fix added. A unit test over apiErr() would stay green behind a
// call site that never passes the body.
//
// The API is stubbed at the browser level in the shape its Go handlers write, so
// this needs no Go server, session or database — only the build the `e2e` gate is
// already serving. The stub is STATEFUL on purpose: the GET after the save returns
// the sets the save left behind, which is how the "back after a reload" half of
// the defect is reproduced (pre-fix the server never changed, so the reload
// restored the set).
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — see
// e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";

const FORM_ID = "form_ship_last_set";
const EN = (k: keyof typeof DICT) => (DICT[k] as Record<string, string>).en;

// One resolvable set: a name, one zone with a rule, one rate. This is what the
// Shipping tab renders after a merchant has configured managed rates.
const ONE_SET = {
  id: "set1",
  name: "Malaysia",
  is_default: true,
  zones: [
    {
      id: "z1",
      name: "West Malaysia",
      states: ["peninsular"],
      districts: [],
      priority: 0,
      is_fallback: false,
      rates: [{ id: "r1", label: "Standard", price_sen: 800, free_over_sen: 15000 }],
    },
  ],
};

type PutSeen = { body: unknown; status: number } | null;

// stubAPI answers every /api/* call the page makes. The shipping route is
// registered LAST so it wins (Playwright resolves matching routes in reverse
// registration order) and it is STATEFUL: `sets` is whatever the last save left,
// so a reload reads the merchant's own save back instead of a fixture.
async function stubAPI(
  page: Page,
  opts: { refusePut?: unknown } = {},
): Promise<{ seen: { put: PutSeen } }> {
  const state = { sets: [ONE_SET] as unknown[], seen: { put: null as PutSeen } };
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
  // lang:"" keeps the browser's own value in effect (the default here is English).
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.com/f/demo-form",
        config: { blocks: [] },
        theme: {},
      }),
    ),
  );
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

  // The subject. GET answers the current state; PUT records the payload the page
  // really sent and (unless the arm pins a refusal) applies it, so the next GET —
  // the one a reload makes — shows what the save left behind.
  await page.route(/(.*)\/api\/rest\/shipping(\?.*)?$/, async (route) => {
    if (route.request().method() !== "PUT") {
      return route.fulfill(json({ account_id: "a1", forms_using_zones: 0, sets: state.sets }));
    }
    const body = route.request().postDataJSON();
    state.seen.put = { body, status: 0 };
    if (opts.refusePut) {
      state.seen.put.status = 400;
      return route.fulfill(json(opts.refusePut, 400));
    }
    const sets = (body as { sets?: unknown[] }).sets ?? [];
    state.sets = sets;
    state.seen.put.status = 200;
    return route.fulfill(json({ account_id: "a1", forms_using_zones: 0, sets }));
  });

  return state;
}

// openShipping walks the real tabs: /form-settings → the Shipping tab, and waits
// for the zone set to be on screen (so a measurement never reads a loading shell).
// The zone's NAME is an input value, not text, so the readiness check reads the
// control the merchant would type into.
async function openShipping(page: Page) {
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });
  await page.getByRole("button", { name: EN("fs.ship"), exact: true }).click();
  await expect(page.getByLabel(EN("fs.shipZoneName"))).toHaveValue("West Malaysia");
  await expect(page.getByText(EN("fs.shipEmpty"))).toHaveCount(0);
}

test("deleting the only zone set saves, says so, and stays deleted", async ({ page }) => {
  test.setTimeout(180_000);
  const state = await stubAPI(page);
  await openShipping(page);

  // The set-level Delete. Delete buttons also sit on the ZONE and on its RATE row
  // — three in this fixture (one set, one zone, one rate) — so the count is pinned
  // FIRST: if the set's own control is ever hidden or disabled for the only set,
  // this arm fails instead of silently clicking a zone's or a rate's button. The
  // set block renders its header before its zones, so `.first()` is the set's.
  const deletes = page.getByRole("button", { name: EN("fs.shipDelete"), exact: true });
  await expect(deletes).toHaveCount(3);
  await deletes.first().click();

  // The draft now shows the empty state: zero sets is a state the page OFFERS,
  // which is exactly why the save has to work.
  await expect(page.getByText(EN("fs.shipEmpty"))).toBeVisible();

  await page.getByRole("button", { name: EN("fs.shipSave"), exact: true }).click();

  // What the page actually posted. This is the half a unit test cannot see: a
  // page that filtered the empty set out (or never wired Delete) would send
  // something else and this assertion names it.
  await expect.poll(() => state.seen.put?.status ?? 0).toBe(200);
  expect(state.seen.put?.body).toEqual({ sets: [] });

  // The confirmation, read out of the live banner.
  const banner = page.getByTestId("action-msg");
  await expect(banner).toHaveText(EN("fs.shipSaved"));
  await expect(banner).toHaveAttribute("data-tone", "success");

  // It stays deleted: the reload reads the saved configuration back (the defect's
  // signature was the set re-appearing here), and the empty view is what renders.
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("button", { name: EN("fs.ship"), exact: true }).click();
  await expect(page.getByText(EN("fs.shipEmpty"))).toBeVisible();
  await expect(page.getByLabel(EN("fs.shipZoneName"))).toHaveCount(0);
});

test("a refused save names the reason instead of only 'Failed to save shipping rates'", async ({
  page,
}) => {
  test.setTimeout(180_000);
  // The exact refusal the API used to write for a shape it cannot resolve. Its
  // code is what the page translates; the prose must never reach the screen.
  const refusal = {
    code: "shipping.invalid",
    error: "invalid shipping rates — zone \"West Malaysia\" has no rates",
  };
  const state = await stubAPI(page, { refusePut: refusal });
  await openShipping(page);

  await page.getByRole("button", { name: EN("fs.shipSave"), exact: true }).click();
  await expect.poll(() => state.seen.put?.status ?? 0).toBe(400);

  const banner = page.getByTestId("action-msg");
  await expect(banner).toHaveAttribute("data-tone", "failure");
  // The reason, not the generic sentence, and never the server's Malay prose.
  await expect(banner).toHaveText(EN("apiErr.shipInvalid"));
  await expect(banner).not.toHaveText(EN("fs.shipSaveFail"));
  await expect(banner).not.toContainText("invalid shipping rates");

  // Nothing was lost from the draft: the set is still on screen to fix and re-save.
  await expect(page.getByLabel(EN("fs.shipZoneName"))).toHaveValue("West Malaysia");
});
