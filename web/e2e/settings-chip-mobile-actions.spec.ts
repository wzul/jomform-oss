// /settings → CHIP on a PHONE: an action you can see, a tab strip that says it
// continues, and payment methods that look like the checkboxes they are
// (card t_f11d676f).
//
// THE DEFECTS, all measured on the built page at 390px before this card:
//   1. NO SAVE IN SIGHT. The first viewport ended inside the CHIP Collect card
//      (Brand ID, Secret Key, Mode, the payment-method row) and carried no
//      Save/Connect control at all — the action was several screens further
//      down. A merchant who pasted a key had nothing to press.
//   2. THE STRIP LOOKED COMPLETE. 11 tabs in a 342px box (`scrollWidth` 1,013)
//      with `overflow-x-auto` and no fade, arrow or scrollbar: two thirds of the
//      tab set was off screen behind an edge that looked like the end of the row.
//   3. THE PILLS WERE NOT CONTROLS YOU COULD READ. The twelve payment methods
//      rendered as rounded pills with no box, no tick and no selected-state mark
//      — the same shape as the FILTER chips on /sales and /deliveries, so a
//      merchant could not tell a chosen method from an unchosen one.
//
// WHY A BROWSER MEASUREMENT. Every one of the three is a property of the
// RENDERED box: "is there a Save inside the first viewport" is a rect compared
// with the viewport, "does the strip say it continues" is a computed mask plus a
// scroll position, and "is this pill chosen" is the accessible checked state and
// the painted tick. A class-name assertion is toothless on all three — any other
// arrangement that re-creates the defect would keep it green.
//
// The API is stubbed at the browser level (the pattern every sibling spec uses),
// so this needs nothing but the build the project is already serving. Storage is
// cleared and the locale pinned to `en` on every case: the browser keeps
// `localStorage` between runs, and a `jf_lang`/`jf_settings_tab` left behind by
// an earlier card would render this page in a state no new visitor has.
import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

const SAVE = DICT["set.save"].en;
const SELECT_ALL = DICT["set.pmSelectAll"].en;
const CLEAR_ALL = DICT["set.pmClearAll"].en;
const activeMode = (mode: string) => DICT["set.activeMode"].en.replace("{mode}", mode.toUpperCase());

// THE PHONE THE CARD WAS MEASURED ON.
const PHONE = { width: 390, height: 844 } as const;
const DESKTOP = { width: 1280, height: 720 } as const;

// The payment methods the PAGE declares, read out of its own source. Pinned as
// a literal this file would go red on a one-line method addition in the page and
// blame the layout; derived, the counts below are about the page that ships.
// (`__dirname`, not `import.meta.url`: package.json has no `"type": "module"`,
// so Playwright loads this spec as CommonJS.)
const METHOD_IDS = (() => {
  const src = readFileSync(join(__dirname, "../src/app/settings/page.tsx"), "utf8");
  const decl = src.match(/const METHODS\s*=\s*\[([\s\S]*?)\n\];/);
  if (!decl) throw new Error("settings/page.tsx no longer declares METHODS — this guard reads it to derive the checkbox count");
  const ids = [...decl[1].matchAll(/id:\s*"([^"]+)"/g)].map((m) => m[1]);
  if (!ids.length) throw new Error("settings/page.tsx declares METHODS with no ids");
  return ids;
})();

type ChipRow = {
  configured: boolean;
  verified: boolean;
  mode: string;
  brand_id: string;
  whitelist: string[];
  email_fallback: string;
  secret_key_masked: string;
};

const CONFIGURED: ChipRow = {
  configured: true,
  verified: true,
  mode: "test",
  brand_id: "brand-existing",
  // One saved method, so "the saved state is ticked" is a real assertion and the
  // dirty check has a non-empty baseline to compare against.
  whitelist: ["card"],
  email_fallback: "",
  secret_key_masked: "••••1234",
};

const ME = { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: true };
const WS = { id: "ws1", name: "Demo", slug: "demo", role: "admin" };

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

/**
 * Stubs with the SERVER'S OWN STATE on the CHIP row, because two of the arms
 * below are about convergence: the sticky Save must re-disable itself once the
 * write it made is what the server now holds. A stateless stub could not tell a
 * page that converged from a page that guessed.
 */
async function stubAPI(page: Page) {
  let chip: ChipRow = { ...CONFIGURED };
  const puts: { brand_id?: string; payment_method_whitelist?: string[]; mode?: string }[] = [];

  const bundle = () => ({
    me: ME,
    workspaces: [WS],
    chip_status: chip,
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
    currency: "MYR",
  });

  // Catch-all FIRST, the page's own routes after it: Playwright resolves
  // matching routes in reverse registration order, so the later (specific) ones
  // win.
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(bundle())));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
  await page.route(/(.*)\/api\/rest\/settings\/chip(\?.*)?$/, (r) => {
    const req = r.request();
    if (req.method() === "PUT") {
      const body = (req.postDataJSON() || {}) as {
        brand_id?: string;
        secret_key?: string;
        mode?: string;
        email_fallback?: string;
        payment_method_whitelist?: string[];
      };
      puts.push(body);
      const secret = body.secret_key || "";
      chip = {
        configured: true,
        verified: true,
        mode: body.mode === "live" ? "live" : "test",
        brand_id: body.brand_id || "",
        whitelist: body.payment_method_whitelist || [],
        email_fallback: body.email_fallback || "",
        secret_key_masked: secret ? `••••${secret.slice(-4)}` : chip.secret_key_masked,
      };
      return r.fulfill(json({ status: "configured", mode: chip.mode, verified: chip.verified }));
    }
    return r.fulfill(json(chip));
  });
  await page.route(/(.*)\/api\/rest\/settings\/payment-fees(\?.*)?$/, (r) =>
    r.fulfill(json({ rates: [], acknowledged: false })),
  );
  await page.route(/(.*)\/api\/cf\/status(\?.*)?$/, (r) => r.fulfill(json({ linked: false, configured: false })));
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [] })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/(email-settings|settings)(\?.*)?$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/meta(\?.*)?$/, (r) => r.fulfill(json({})));

  return { chip: () => chip, puts: () => puts };
}

/** A fresh English browser state, then the CHIP tab, at `viewport`. */
async function openChipTab(page: Page, viewport: { width: number; height: number }) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
      localStorage.setItem("jf_settings_tab", "chip");
    } catch {
      /* storage unavailable */
    }
  });
  await page.setViewportSize(viewport);
  await page.goto("/settings?tab=chip", { waitUntil: "domcontentloaded" });
  // The tab set grows once the session bundle resolves `role` (CHIP, Members and
  // Workspace are admin-only), and the CHIP panel is only painted once the tab
  // is settled — so wait for the panel, not for "the page painted".
  await expect(page.locator("#set-panel-chip")).toBeVisible({ timeout: 45_000 });
}

const stickyBar = (page: Page) => page.locator('[data-testid="chip-sticky-save"]');
const stickySave = (page: Page) => page.locator('[data-testid="chip-save-sticky"]');

// ── 1. THE ACTION IS IN THE FIRST VIEWPORT ──────────────────────────────────

test("390px: the CHIP panel's Save sits in the bottom action bar of the first viewport", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  const stub = await stubAPI(page);
  await openChipTab(page, PHONE);

  // The reader has not scrolled: this is the FIRST viewport.
  expect(await page.evaluate("window.scrollY"), "the page must be at the top for this to be the first viewport").toBe(0);

  await expect(stickyBar(page), "the sticky action bar is not rendered at 390px").toBeVisible();

  const m = await page.evaluate(`(() => {
    const bar = document.querySelector('[data-testid="chip-sticky-save"]');
    const card = document.querySelector('#set-panel-chip');
    const r2 = (n) => Math.round(n * 10) / 10;
    const b = bar.getBoundingClientRect();
    const c = card.getBoundingClientRect();
    const cs = getComputedStyle(bar);
    return {
      barTop: r2(b.top), barBottom: r2(b.bottom), barLeft: r2(b.left), barRight: r2(b.right),
      position: cs.position,
      viewportW: window.innerWidth, viewportH: window.innerHeight,
      cardTop: r2(c.top), cardHeight: r2(c.height),
      docHeight: document.documentElement.scrollHeight,
    };
  })()`) as {
    barTop: number; barBottom: number; barLeft: number; barRight: number; position: string;
    viewportW: number; viewportH: number; cardTop: number; cardHeight: number; docHeight: number;
  };

  // Reported for the handoff: the card's measured height on a 390px phone.
  console.log(
    `[t_f11d676f] 390px: credential card ${m.cardHeight}px tall, top at ${m.cardTop}px; ` +
      `sticky bar ${m.barTop}..${m.barBottom}px in a ${m.viewportH}px viewport; document ${m.docHeight}px`,
  );

  // The bar is PINNED to the bottom edge and fully inside the viewport — that is
  // what "visible without scrolling" means, and it is the assertion the pre-fix
  // page fails (there was no bar at all).
  expect(m.position, "the action bar is not a sticky box").toBe("sticky");
  expect(m.barBottom, `the sticky bar's bottom (${m.barBottom}) is below the viewport (${m.viewportH})`).toBeLessThanOrEqual(m.viewportH + 1);
  expect(m.barBottom, `the sticky bar (bottom ${m.barBottom}) is not pinned to the bottom of the ${m.viewportH}px viewport`).toBeGreaterThanOrEqual(m.viewportH - 8);
  expect(m.barTop, `the sticky bar starts at ${m.barTop}px — outside the viewport`).toBeGreaterThanOrEqual(0);
  expect(m.barLeft, `the bar does not start at the viewport's left edge (${m.barLeft})`).toBeLessThanOrEqual(2);
  expect(m.barRight, `the bar does not reach the viewport's right edge (${m.barRight})`).toBeGreaterThanOrEqual(m.viewportW - 2);
  // The page is LONGER than the viewport, which is why the in-flow Save was not
  // in it — the bar is not just sitting at the end of a short page.
  expect(m.docHeight, "the page is not taller than the viewport, so this arm proves nothing").toBeGreaterThan(m.viewportH);

  // The bar carries the CONNECTION STATE, in the same words as the card's badge.
  await expect(page.locator('[data-testid="chip-sticky-state"]')).toHaveText(activeMode("test"));

  // It is a Save that does nothing until there is something to save…
  await expect(stickySave(page), "Save is enabled before anything changed").toBeDisabled();
  // …and one edit enables it.
  await page.getByPlaceholder("e.g. 5f2b…-uuid").fill("brand-from-phone");
  await expect(stickySave(page), "typing a Brand ID did not enable the sticky Save").toBeEnabled();

  // Pressing it submits the SAME form (no second write path) — the server sees it…
  await stickySave(page).click();
  await expect.poll(() => stub.puts().length, { message: "the sticky Save did not submit the credentials form" }).toBe(1);
  expect(stub.puts()[0].brand_id, "the PUT did not carry the Brand ID typed on the phone").toBe("brand-from-phone");
  // …and the bar re-disables itself once the server holds what the form holds.
  await expect
    .poll(async () => stickySave(page).isDisabled(), {
      message: "the sticky Save stayed enabled after a successful save — the saved baseline never moved",
      timeout: 20_000,
    })
    .toBe(true);
});

// ── 2. THE STRIP SAYS IT CONTINUES, AND THE CUE FOLLOWS THE SCROLL ──────────

test("390px: the tab strip shows a scroll cue on the edge that hides tabs, and it follows the scroll", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  const read = async () =>
    (await page.evaluate(`(() => {
      const strip = document.querySelector('[role="tablist"]');
      const cs = getComputedStyle(strip);
      const sr = strip.getBoundingClientRect();
      const tabs = [...strip.querySelectorAll('[role="tab"]')];
      const last = tabs[tabs.length - 1].getBoundingClientRect();
      const r2 = (n) => Math.round(n * 10) / 10;
      return {
        tabs: tabs.length,
        clientWidth: strip.clientWidth,
        scrollWidth: strip.scrollWidth,
        scrollLeft: strip.scrollLeft,
        edge: strip.getAttribute("data-edge"),
        maskImage: cs.maskImage || cs.webkitMaskImage || "",
        scrollbarWidth: cs.scrollbarWidth || "",
        scrollSnapType: cs.scrollSnapType || "",
        clip: strip.offsetHeight - strip.clientHeight,
        firstTabLeft: r2(tabs[0].getBoundingClientRect().left),
        lastTabRight: r2(last.right),
        stripRight: r2(sr.right),
        stripLeft: r2(sr.left),
      };
    })()`)) as {
      tabs: number; clientWidth: number; scrollWidth: number; scrollLeft: number; edge: string | null;
      maskImage: string; scrollbarWidth: string; scrollSnapType: string; clip: number;
      firstTabLeft: number; lastTabRight: number; stripRight: number; stripLeft: number;
    };

  const start = await read();
  console.log(`[t_f11d676f] 390px strip: ${start.tabs} tabs, ${start.scrollWidth}px of content in ${start.clientWidth}px, edge=${start.edge}`);

  // The strip really does hide tabs at this width — otherwise the cue would be
  // decoration and this arm would be measuring nothing.
  expect(start.tabs, "the strip is not rendering its tab set, so nothing can be clipped").toBeGreaterThan(5);
  expect(start.scrollWidth, "the strip does not overflow at 390px — there is no cue to check").toBeGreaterThan(start.clientWidth);

  // (a) The platform's own affordance is configured: a thin scrollbar. It is an
  //     OVERLAY scrollbar on a phone and in headless Chromium (it paints only
  //     while scrolling), which is exactly why it cannot be the only cue.
  //     There is deliberately NO `scroll-snap`: measured, snapping moved a
  //     keyboard-focused tab 36px outside the strip's clip box and turned
  //     `e2e/settings-tabs.spec.ts` RULE 8 red — see the note in
  //     `globals.css`. If someone adds it back, this arm says so by name.
  expect(start.scrollbarWidth, "the strip has no visible scrollbar (scrollbar-width is not thin)").toBe("thin");
  expect(
    start.scrollSnapType,
    "the strip has scroll-snap again: measured, it defeats the keyboard reveal (a focused tab lands 36px outside the strip)",
  ).toBe("none");

  // (b) The fade is painted on the edge that hides content, and it is a MASK
  //     (something actually drawn), not a class name.
  expect(start.edge, "the strip does not report the edge that hides tabs").toBe("end");
  expect(start.maskImage, "no mask is painted over the end edge the strip reports").not.toBe("none");
  expect(start.maskImage, "the mask carries no gradient").toContain("gradient");
  // …and a tab really is cut by that edge.
  expect(start.lastTabRight, "no tab is clipped at the end edge, so the fade would be a lie").toBeGreaterThan(start.stripRight + 1);

  // (d) A CONTROL, not only a fade. A fade over 12px muted text is a weak cue —
  //     measured, a 1.5rem ramp left only a faint stroke visible — so the strip
  //     carries an arrow at the edge that hides tabs: it is the thing a reader
  //     can see, name and press.
  const forward = page.locator('[data-testid="tabstrip-forward"]');
  const back = page.locator('[data-testid="tabstrip-back"]');
  await expect(forward, "no forward arrow is offered while tabs are hidden at the end edge").toBeVisible();
  await expect(back, "a back arrow is offered at the start edge, where there is nothing behind it").toHaveCount(0);
  // It is a NAMED button: the direction is in the accessible name, so a screen
  // reader user gets a control rather than a chevron glyph.
  await expect(page.getByRole("button", { name: DICT["set.tabsScrollForward"].en })).toHaveCount(1);
  await expect(page.getByRole("button", { name: DICT["set.tabsScrollBack"].en })).toHaveCount(0);

  // …and it SITS ON THE EDGE it is talking about: an arrow that renders
  // somewhere else in the box is a chevron next to the tabs, not a cue that the
  // row continues. Its box must end where the strip ends and be centred on it.
  const place = (await page.evaluate(`(() => {
    const strip = document.querySelector('[role="tablist"]').getBoundingClientRect();
    const a = document.querySelector('[data-testid="tabstrip-forward"]').getBoundingClientRect();
    return {
      rightGap: Math.round(a.right - strip.right),
      width: Math.round(a.width),
      centreGap: Math.round((a.top + a.bottom) / 2 - (strip.top + strip.bottom) / 2),
    };
  })()`)) as { rightGap: number; width: number; centreGap: number };
  expect(Math.abs(place.rightGap), `the forward arrow does not end at the strip's right edge (off by ${place.rightGap}px)`).toBeLessThanOrEqual(2);
  expect(Math.abs(place.centreGap), `the forward arrow is not centred on the strip (off by ${place.centreGap}px)`).toBeLessThanOrEqual(2);
  expect(place.width, "the forward arrow has no width — it is not an overlay button").toBeGreaterThan(16);

  // Pressing it moves the row — the control is wired to the scroller, not
  // decoration. Repeatedly, until the end of the row.
  const before = start.scrollLeft;
  await forward.click();
  await expect.poll(async () => (await read()).scrollLeft, { message: "the forward arrow did not move the strip" }).not.toBe(before);
  for (let i = 0; i < 8; i += 1) {
    const now = await read();
    if (now.edge === "start") break;
    await page.locator('[data-testid="tabstrip-forward"]').click();
  }

  // (c) At the END of the row the cue must be on the other edge — this is what a
  //     permanent both-edges fade fails, and why the strip reports its position
  //     instead of carrying a static mask.
  await expect
    .poll(async () => (await read()).edge, {
      message: "the strip still claims the END edge hides tabs after scrolling to the end of the row",
      timeout: 10_000,
    })
    .toBe("start");
  const end = await read();
  expect(end.maskImage, "the fade disappeared entirely at the end of the row — there is no cue that tabs were passed").not.toBe("none");
  expect(end.firstTabLeft, "no tab is clipped at the start edge after scrolling to the end").toBeLessThan(end.stripLeft - 1);
  await expect(forward, "the forward arrow is still offered at the end of the row").toHaveCount(0);
  await expect(back, "no back arrow is offered at the end of the row").toBeVisible();
});

// ── 3. THE PAYMENT METHODS ARE CHECKBOXES ───────────────────────────────────

test("390px: payment methods are checkboxes with a painted tick and an announced state", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, PHONE);

  const boxes = page.getByRole("checkbox");
  await expect(boxes, `the panel does not render one checkbox per payment method (${METHOD_IDS.join(", ")})`).toHaveCount(
    METHOD_IDS.length,
  );
  // The group is NAMED by the visible heading, so "the twelve boxes belong to
  // Payment methods" is a fact a screen reader is told, not a visual guess.
  await expect(
    page.getByRole("group", { name: new RegExp(DICT["set.paymentMethods"].en, "i") }),
    "the checkbox group is not labelled by the Payment methods heading",
  ).toHaveCount(1);

  // THE SAVED STATE IS TICKED, and it is the only one ticked. `toBeChecked` reads
  // the control's own state — the same state a screen reader announces.
  const card = page.locator('[data-testid="chip-pm-card"]');
  const fpx = page.locator('[data-testid="chip-pm-fpx"]');
  await expect(card, "the saved payment method is not ticked").toBeChecked();
  await expect(fpx, "an unsaved payment method arrived ticked").not.toBeChecked();

  // THE TICK IS PAINTED, NOT MERELY PRESENT — AND ONLY WHEN THE ROW IS TICKED.
  // The glyph sits in a box whose fill is the state: checked, the box is filled
  // with the brand colour and the white glyph reads against it; UNCHECKED the box
  // is white and carries NO glyph at all. That absence is the assertion, not a
  // detail: an always-rendered white ✓ on the unchecked box's white fill is a
  // measured 1:1, and `e2e/a11y-singlechar-text.spec.ts` reds on it (12 rows,
  // card t_5dc6bb86) — that scan measures paint, so `aria-hidden`, and splitting
  // the fill and the glyph across two elements, do not exempt it. Read off the
  // computed styles of the two elements that ship.
  const paint = await page.evaluate(`(() => {
    const box = (id) => document.querySelector('[data-testid="chip-pm-' + id + '"]').nextElementSibling;
    const glyph = (id) => box(id).firstElementChild;
    const of = (el) => { const cs = getComputedStyle(el); return { color: cs.color, bg: cs.backgroundColor }; };
    const state = (id) => {
      const g = glyph(id);
      return {
        box: of(box(id)),
        glyph: g ? { ...of(g), text: g.textContent, width: g.getBoundingClientRect().width } : null,
        text: box(id).textContent,
      };
    };
    return { ticked: state("card"), unticked: state("fpx") };
  })()`) as {
    ticked: { box: { color: string; bg: string }; glyph: { color: string; bg: string; text: string | null; width: number } | null; text: string };
    unticked: { box: { color: string; bg: string }; glyph: { color: string; bg: string; text: string | null; width: number } | null; text: string };
  };
  expect(paint.ticked.box.bg, "the ticked method's box is not filled — the state is invisible").not.toBe("rgb(255, 255, 255)");
  expect(paint.ticked.glyph, "the ticked method paints no tick — the checked state is invisible").not.toBeNull();
  expect(paint.ticked.glyph!.color, "the tick does not read against the filled box").toBe("rgb(255, 255, 255)");
  expect(paint.ticked.glyph!.text?.trim(), "the ticked method's glyph is not a tick").toBe("✓");
  expect(
    paint.ticked.glyph!.width,
    "the ticked method's glyph has no painted width — it is present in the DOM but occupies nothing",
  ).toBeGreaterThan(0);
  expect(paint.unticked.box.bg, "an unticked method's box is filled").toBe("rgb(255, 255, 255)");
  expect(
    paint.unticked.glyph,
    "an unticked method paints a glyph inside its white box — white on white is 1:1 and reds the single-character a11y scan",
  ).toBeNull();
  expect(
    paint.unticked.text?.includes("✓"),
    "an unticked method's row still carries a ✓ text node — the box must be empty text when unchecked",
  ).toBe(false);

  // THE STATE IS ANNOUNCED. A native checkbox exposes `checked` in the
  // accessibility tree — the same state `aria-checked` would carry on a custom
  // control — and that is what a screen reader reads. Taken off the real AX tree
  // (this is the "can a merchant tell whether it is chosen" question, asked of
  // the accessibility layer rather than of the DOM).
  const session = await page.context().newCDPSession(page);
  const ax = (await session.send("Accessibility.getFullAXTree")) as {
    nodes: { role?: { value?: string }; name?: { value?: string }; properties?: { name: string; value?: { value?: unknown } }[] }[];
  };
  const axBoxes = ax.nodes.filter((n) => n.role?.value === "checkbox");
  // CDP types the tristate as the STRING "true"/"false"/"mixed" (the AX
  // property's `type` is `tristate`), so both forms are accepted rather than
  // pinning the wire encoding of one Chromium build.
  const axChecked = (n: (typeof axBoxes)[number]) => {
    const v = (n.properties || []).find((p) => p.name === "checked")?.value?.value;
    return v === true || v === "true";
  };
  expect(axBoxes.length, `the accessibility tree exposes ${axBoxes.length} checkboxes, not ${METHOD_IDS.length}`).toBe(
    METHOD_IDS.length,
  );
  const saved = axBoxes.filter((n) => axChecked(n) === true);
  expect(saved.length, "exactly one payment method is saved, but the AX tree reports a different number as checked").toBe(1);
  expect(saved[0].name?.value, "the checked method's accessible name is not its label").toContain("Card");

  // CLICKING A ROW TICKS IT — the row is the label, so the whole pill is the hit
  // area, and the state change is what the merchant sees.
  await page.locator('[data-testid="chip-pm-row-fpx"]').click();
  await expect(fpx, "clicking the row did not tick its checkbox").toBeChecked();
  // …and an edit like that is something to save, so the sticky bar wakes up.
  await expect(stickySave(page), "ticking a payment method did not enable the sticky Save").toBeEnabled();

  // SELECT ALL, then CLEAR ALL — the affordance names the opposite of the state.
  const all = page.locator('[data-testid="chip-pm-toggle-all"]');
  await expect(all).toHaveText(SELECT_ALL);
  await all.click();
  await expect(boxes, "Select all did not tick every method").toHaveCount(METHOD_IDS.length);
  for (const id of METHOD_IDS) await expect(page.locator(`[data-testid="chip-pm-${id}"]`)).toBeChecked();
  await expect(all, "the toggle still reads Select all when every method is ticked").toHaveText(CLEAR_ALL);
  await all.click();
  for (const id of METHOD_IDS) await expect(page.locator(`[data-testid="chip-pm-${id}"]`)).not.toBeChecked();
  await expect(all, "the toggle did not return to Select all after clearing").toHaveText(SELECT_ALL);
});

// ── 5. THE CUE IS DIRECTIONAL: RTL PUTS THE ARROW ON THE OTHER SIDE ─────────

test("390px (ar/RTL): the forward arrow sits on the LEFT edge, where Arabic hides its tabs", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  // The page's locale comes from `/api/auth/profile` as well as `jf_lang`
  // (`components/i18n.tsx` applies the profile's language over the stored one),
  // so the stub has to agree with the storage below. Registered AFTER
  // `stubAPI`, which is what makes it win: Playwright takes the most recently
  // registered matching route.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "ar" })));
  // the strip's arrows are placed with LOGICAL insets (start/end), so this arm
  // is the one that would catch a physical `left-0`/`right-0` creeping back in —
  // measured, RTL is where a directional control silently lands on the wrong
  // side and points away from the content.
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "ar");
      localStorage.setItem("jf_settings_tab", "chip");
    } catch {
      /* storage unavailable */
    }
  });
  await page.setViewportSize(PHONE);
  await page.goto("/settings?tab=chip", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#set-panel-chip")).toBeVisible({ timeout: 45_000 });

  // The direction is applied by an effect after the locale resolves, so wait for
  // it rather than reading once (a one-shot read here fails against a correct
  // page: the first paint is still `ltr`).
  await expect
    .poll(async () => page.evaluate("document.documentElement.dir"), {
      message: "the page never switched to RTL — the state this arm is about did not happen",
      timeout: 20_000,
    })
    .toBe("rtl");

  const m = (await page.evaluate(`(() => {
    const strip = document.querySelector('[role="tablist"]');
    const sr = strip.getBoundingClientRect();
    const fwd = document.querySelector('[data-testid="tabstrip-forward"]');
    const back = document.querySelector('[data-testid="tabstrip-back"]');
    const r2 = (n) => Math.round(n * 10) / 10;
    return {
      edge: strip.getAttribute("data-edge"),
      mask: getComputedStyle(strip).maskImage || getComputedStyle(strip).webkitMaskImage || "",
      overflowing: strip.scrollWidth > strip.clientWidth,
      forwardLeftGap: fwd ? r2(fwd.getBoundingClientRect().left - sr.left) : null,
      backPresent: !!back,
    };
  })()`)) as {
    edge: string | null;
    mask: string;
    overflowing: boolean;
    forwardLeftGap: number | null;
    backPresent: boolean;
  };

  expect(m.overflowing, "the Arabic strip does not overflow, so there is no hidden edge to point at").toBe(true);
  expect(m.edge, "the Arabic strip does not report the edge that hides tabs").toBe("end");
  expect(m.mask, "no fade is painted on the Arabic strip's hidden edge").not.toBe("none");
  // `end` in RTL is the LEFT edge, and that is where the forward arrow must be.
  expect(m.forwardLeftGap, "the forward arrow is not on the Arabic strip's LEFT edge").not.toBeNull();
  expect(Math.abs(m.forwardLeftGap as number), `the forward arrow sits ${m.forwardLeftGap}px from the left edge in RTL`).toBeLessThanOrEqual(2);
  expect(m.backPresent, "a back arrow is offered at the RTL start edge, where nothing is hidden").toBe(false);
});

// ── 6. DESKTOP KEEPS THE IN-FLOW ACTION ─────────────────────────────────────

test("desktop: the sticky bar is gone and the in-flow Save is the one action", async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
  await stubAPI(page);
  await openChipTab(page, DESKTOP);

  await expect(stickyBar(page), "the phone-only action bar is rendered on a desktop viewport").toBeHidden();
  // Exactly ONE visible Save on the panel: the in-flow button inside the form.
  await expect(page.getByRole("button", { name: SAVE, exact: true })).toHaveCount(1);
});
