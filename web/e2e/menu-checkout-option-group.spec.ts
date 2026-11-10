import { test, expect, type Page } from "@playwright/test";

// The LIVE geometry of a menu checkout's option groups (card t_2244109a).
//
// WHY THIS EXISTS. No published form in production carries a `radio` or
// `image-choice` block (read-only SQL on 2026-10-31: 69 live forms, 2
// menu-enabled, 0 with either block), and prod data must not be written to
// verify a flow — so two QA cards each improvised a probe for a rendered option
// group and deleted it afterwards. The fixture kept from that work is
// api/internal/menufixture: a scratch schema of the test database, seeded
// through the real stores and served by the real public handler over real HTTP,
// with one radio group AND one image-choice group in the checkout sheet.
//
// This spec is the browser half. The Go test
// (api/internal/menufixture/fixture_test.go) pins that the fixture's page really
// carries both groups and that the order route stores an answer; the served
// stylesheet's CASCADE is pinned in
// api/internal/httpapi/menu_radio_size_t_664549fc_test.go. What none of them can
// do is LAYOUT: a Go process does not resolve flex rows and intrinsic control
// sizes. Card t_664549fc was exactly a layout defect — `.sheetfoot input {
// width:100%; height:44px }`, the buyer-field rule, also matched the radio of
// every option, so the control filled a 448px row and pushed the merchant's
// label to the row's edge (and off a 375px screen). Every guard above would have
// been green while a buyer saw that.
//
// WHAT IT MEASURES, on the served page a buyer gets, at 1200px and 375px:
//   - every option control is intrinsic (~13x13), NOT row-sized;
//   - the gap from a control's own edge to ITS label's text is the row's 8px gap;
//   - the buyer name/email fields are still full width and 44px tall — the other
//     half of the same rule, so a fix that widens the reset to `input` fails here;
//   - the pay button is full width;
//   - the document does not scroll sideways at either width.
//
// AND the payment-method picker (card t_453489b9), which is a DELIBERATELY
// different control and is measured on its OWN terms rather than folded into the
// 12–16px band above. Its rows are 44px tap targets and its radio is authored at
// 20x20 (`api/internal/httpapi/menu.go`, `.sheetfoot .paypick .payrow
// input[type=radio]`), so the option band would red a control that is not
// defective — that happened once, on the merged tree of #1686, and relaxing the
// band to 24 would have blunted the option-group guard. Instead:
//   - the option group's band stays 12–16 and still covers every `.radioswrap`
//     radio (so a radio that takes the buyer field's 100%/44px still FAILS);
//   - the picker's radios are measured separately: 12–24px on a row at least
//     200px wide and 44px tall, with the label beside the control in
//     `.payrowtext` at the row's ~10px gap — a picker radio that takes the buyer
//     field's 100%/44px measures 218.7px wide (1200px) and 140.5px (375px) and
//     fails here, and so does a picker that lost its rows entirely;
//   - a coverage assertion requires the two sets to account for EVERY
//     `.sheetfoot input[type=radio]`, so a third kind of radio cannot appear
//     unmeasured. That is what keeps this from being an exclusion.
//
// HOW TO RUN IT. It needs a fixture URL; without one it SKIPS (naming the
// reason) so the ordinary `e2e` gate — which serves the Next.js app and has no
// menu to measure — is unaffected. One command runs the whole thing:
//
//   bash scripts/menu-fixture-e2e.sh
//
// which builds the fixture command, starts it, exports MENU_FIXTURE_URL and runs
// this file. To point it at a deployment instead (a staging stack, say), set
// MENU_FIXTURE_URL to a published menu page that carries an option group.
//
// THE NUMBERS BELOW ARE MEASURED, not assumed: on 2026-11-02, against this
// fixture in Chromium 1243 (the host's playwright cache), at 1200px the option
// rows are 448 wide, the controls 13x13 with an 8px gap to their text, buyer
// fields 448x44 and the pay button 448x50; at 375px the same measurements are
// 343 wide, 13x13, 8px, 343x44 and 343x50, with scrollWidth == clientWidth at
// both widths. The pre-fix build measured 399x44 (1200px) and 294x44 (375px)
// with the option text at the row's right edge, so the assertions below
// discriminate between the two shapes rather than merely describing one.
//
// The picker's figures were re-measured on 2026-11-10 against the same fixture,
// one fresh browser context per width: the control is 20x20, its row 446x48.3
// (1200px) / 341x48.3 (375px), the gap from the control to `.payrowtext` 10px,
// and the `.paypick` group 448 / 343 wide — i.e. the row spans the group and the
// control does not.

const FIXTURE_URL = process.env.MENU_FIXTURE_URL ?? "";

// The two group kinds the fixture seeds. A deployment used instead of the
// fixture must carry a `radio` block named `sd1` and an `image-choice` block
// named `ic1` — the ids api/internal/menufixture seeds — AND it must render the
// payment-method picker with at least two methods, because the picker is measured
// here too (the fixture seeds four: FPX, DuitNow QR, card, e-wallet). A page with
// no picker has nothing for a buyer to choose from, so it FAILS rather than
// skipping that half of the spec.
const RADIO_BLOCK = "sd1";
const IMAGE_CHOICE_BLOCK = "ic1";

// A browser radio with no authored width/height resolves to 13x13 in Chromium.
// The band is deliberately narrow: the defect this guards produced a 399x44 (or
// 294x44 at 375px) control, so anything from 12 to 16 accepts a platform's
// rounding and refuses a control that took the row's size.
const CONTROL_MIN = 12;
const CONTROL_MAX = 16;

// The option row's own gap (`.sheetfoot .radios .rad { gap:8px }`). Measured
// geometry, not the declared value: an authored gap can be eaten by a margin.
const GAP_MIN = 6;
const GAP_MAX = 10;

// The payment-method picker's own bands (card t_453489b9). Its control is
// AUTHORED at 20x20 — a legitimate, deliberately larger radio in a 44px tap
// target — so it is not an option-group control and must not be judged by the
// 12–16 band above. Measured 2026-11-10: control 20x20, row 446x48.3 at 1200px
// and 341x48.3 at 375px, 10px from the control to `.payrowtext`. The upper bound
// 24 is what keeps this a real assertion: with this rule removed the picker's
// radio takes the buyer field's `width:100%; height:44px` and measures 218.7px
// wide at 1200px (140.5px at 375px), so it fails here.
const PAY_CONTROL_MIN = 12;
const PAY_CONTROL_MAX = 24;
const PAY_ROW_MIN_WIDTH = 200;
const PAY_ROW_MIN_HEIGHT = 44;
const PAY_GAP_MIN = 6;
const PAY_GAP_MAX = 14;

interface Box {
  x: number;
  width: number;
  height: number;
  right: number;
}

interface ControlReading extends Box {
  name: string;
  value: string;
  gap: number | null;
  text: string;
}

// A picker row's controls: the radio, the row (`label.payrow`) it sits in, and
// the span that carries its label text (`.payrowtext`), because this row puts
// its text in a span rather than as a direct text node.
interface PayReading extends ControlReading {
  rowWidth: number;
  rowHeight: number;
}

interface Geometry {
  scrollWidth: number;
  clientWidth: number;
  innerWidth: number;
  radioTotal: number;
  controls: ControlReading[];
  payControls: PayReading[];
  paypickWidth: number;
  groups: { legend: string; width: number }[];
  buyer: { name: Box; email: Box };
  pay: Box;
}

const MEASURE = `(() => {
  const box = (el) => {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: +r.x.toFixed(1), width: +r.width.toFixed(1), height: +r.height.toFixed(1), right: +r.right.toFixed(1) };
  };
  // The OPTION-GROUP controls: the radios of a radio / image-choice block, i.e.
  // the ones inside a .radioswrap fieldset. The payment-method picker's radios
  // are NOT this control — it is measured on its own terms below.
  const controls = [...document.querySelectorAll('.sheetfoot .radioswrap input[type=radio]')].map((input) => {
    const label = input.closest('label');
    const r = input.getBoundingClientRect();
    // The gap to THIS control's own text, measured from the control's edge to a
    // Range over the label's text node — the reading card t_722302c9 used, and
    // wrap-independent.
    const text = label ? [...label.childNodes].find((n) => n.nodeType === 3 && n.textContent.trim()) : null;
    let gap = null;
    if (text) {
      const range = document.createRange();
      range.selectNodeContents(text);
      gap = +(range.getBoundingClientRect().left - r.right).toFixed(1);
    }
    return { name: input.name, value: input.value, ...box(input), gap, text: label ? label.textContent.trim() : '' };
  });
  // The PICKER's controls. Same box reading, but its label text is a span
  // (.payrowtext) beside the control, so the gap is measured to that span's left
  // edge, and the row (label.payrow) is read too: the row is the 44px target.
  const payControls = [...document.querySelectorAll('.sheetfoot .paypick input[type=radio]')].map((input) => {
    const row = input.closest('.payrow');
    const r = input.getBoundingClientRect();
    const textEl = row ? row.querySelector('.payrowtext') : null;
    const gap = textEl ? +(textEl.getBoundingClientRect().left - r.right).toFixed(1) : null;
    const rbox = row ? row.getBoundingClientRect() : null;
    return {
      name: input.name, value: input.value, ...box(input), gap,
      text: row ? row.textContent.trim() : '',
      rowWidth: rbox ? +rbox.width.toFixed(1) : 0,
      rowHeight: rbox ? +rbox.height.toFixed(1) : 0,
    };
  });
  const pick = document.querySelector('.sheetfoot .paypick');
  return {
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    innerWidth: window.innerWidth,
    // Every radio in the sheet foot, so the spec can prove the two measured sets
    // account for all of them and nothing is silently excluded.
    radioTotal: document.querySelectorAll('.sheetfoot input[type=radio]').length,
    controls,
    payControls,
    paypickWidth: pick ? +pick.getBoundingClientRect().width.toFixed(1) : 0,
    groups: [...document.querySelectorAll('.sheetfoot .radioswrap')].map((g) => ({
      legend: (g.querySelector('legend') || { textContent: '' }).textContent.trim(),
      width: +g.getBoundingClientRect().width.toFixed(1),
    })),
    buyer: {
      name: box(document.getElementById('menuName')),
      email: box(document.getElementById('menuEmail')),
    },
    pay: box(document.getElementById('menuPay')),
  };
})()`;

// openCheckout loads the page with an empty basket (every Playwright context is
// fresh, so a previous run's cart cannot leak in), adds one dish and opens the
// sheet — the sheet's foot, where the option groups live, is not in the layout
// until then.
async function openCheckout(page: Page, url: string): Promise<void> {
  await page.goto(url, { waitUntil: "load" });
  const add = page.locator("button.row[data-id], button.card[data-id]").first();
  await expect(add, "the menu page offers no dish to add — nothing to check out").toBeVisible();
  await add.click();
  const open = page.locator("#menuBarBtn");
  await expect(open, "the basket bar never appeared after adding a dish").toBeVisible();
  await open.click();
  await expect(page.locator("#menuSheet"), "the basket sheet did not open").toBeVisible();
  await expect(page.locator(".sheetfoot input[type=radio]").first(), "the open sheet carries no option control").toBeVisible();
}

test.describe("menu checkout option groups really render, and at the right size", () => {
  test.skip(FIXTURE_URL === "", "MENU_FIXTURE_URL is unset — run `bash scripts/menu-fixture-e2e.sh` to start the non-prod fixture, or point this at a menu page that carries an option group");

  for (const width of [1200, 375]) {
    test(`${width}px: both groups and the pay picker paint controls that are not row-sized`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await openCheckout(page, FIXTURE_URL);
      const g = (await page.evaluate(MEASURE)) as Geometry;

      // The page really carries BOTH group kinds, two choices each. Without
      // this, the geometry assertions below could pass over one group (or none).
      const radioControls = g.controls.filter((c) => c.name === RADIO_BLOCK);
      const imageControls = g.controls.filter((c) => c.name === IMAGE_CHOICE_BLOCK);
      expect(radioControls.length, `the ${RADIO_BLOCK} radio group is not in the sheet`).toBe(2);
      expect(imageControls.length, `the ${IMAGE_CHOICE_BLOCK} image-choice group is not in the sheet`).toBe(2);
      expect(g.groups.length, "the sheet carries a different number of option groups than the fixture seeds").toBe(2);

      // 0. NOTHING IS EXCLUDED. The option group and the picker are measured by
      //    two DIFFERENT rules, so this asserts the two sets account for every
      //    radio in the sheet foot. A third kind of radio that appears here (a
      //    new block, a stray control) fails instead of slipping past unmeasured
      //    — which is what stops the per-kind split from being an exclusion.
      expect(g.controls.length + g.payControls.length,
        `the sheet foot carries ${g.radioTotal} radios but only ${g.controls.length} option controls + ` +
        `${g.payControls.length} picker controls were measured — a radio is in neither measured set`)
        .toBe(g.radioTotal);

      // 1. Each option-group control is its intrinsic size — NOT the row's.
      for (const c of g.controls) {
        expect(c.width, `the ${c.name}=${c.value} control is ${c.width}px wide — the buyer-field rule (` + "`width:100%`" + `) is sizing the option's radio again, which pushes the merchant's label to the row's edge (card t_664549fc)`).toBeGreaterThanOrEqual(CONTROL_MIN);
        expect(c.width).toBeLessThanOrEqual(CONTROL_MAX);
        expect(c.height, `the ${c.name}=${c.value} control is ${c.height}px tall, want ~13 — it took the buyer field's 44px height`).toBeGreaterThanOrEqual(CONTROL_MIN);
        expect(c.height).toBeLessThanOrEqual(CONTROL_MAX);
      }

      // 2. The gap from an option control to ITS OWN label text is the row's 8px.
      for (const c of g.controls) {
        expect(c.gap, `no text node found beside the ${c.name}=${c.value} control`).not.toBeNull();
        expect(c.gap as number, `the gap from the ${c.name}=${c.value} control to "${c.text}" is ${c.gap}px, want 8px — the text is not sitting beside its control`)
          .toBeGreaterThanOrEqual(GAP_MIN);
        expect(c.gap as number).toBeLessThanOrEqual(GAP_MAX);
      }

      // 3. The payment-method picker is a different control, so it gets its own
      //    assertions (card t_453489b9). What must stay true: the buyer picks a
      //    METHOD from rows that are real tap targets, and the radio is a radio —
      //    larger than an option-group control by design (authored 20x20), but
      //    never sized like the row it sits in.
      expect(g.payControls.length, "the sheet carries no payment-method picker — the buyer cannot choose how to pay").toBeGreaterThanOrEqual(2);
      expect(g.paypickWidth, "the payment-method picker painted no width").toBeGreaterThan(PAY_ROW_MIN_WIDTH);
      for (const c of g.payControls) {
        expect(c.width, `the picker's ${c.value} control is ${c.width}px wide — it took the row's width instead of staying a radio`).toBeGreaterThanOrEqual(PAY_CONTROL_MIN);
        expect(c.width, `the picker's ${c.value} control is ${c.width}px wide, want <= ${PAY_CONTROL_MAX} — the buyer-field rule is sizing the picker's radio`).toBeLessThanOrEqual(PAY_CONTROL_MAX);
        expect(c.height, `the picker's ${c.value} control is ${c.height}px tall — it took the row's height instead of staying a radio`).toBeGreaterThanOrEqual(PAY_CONTROL_MIN);
        expect(c.height, `the picker's ${c.value} control is ${c.height}px tall, want <= ${PAY_CONTROL_MAX}`).toBeLessThanOrEqual(PAY_CONTROL_MAX);
        // The ROW is the target: full width of the group and 44px tall, even
        // though the control inside it is not.
        expect(Math.abs(c.rowWidth - g.paypickWidth), `the picker's ${c.value} row is ${c.rowWidth}px wide in a ${g.paypickWidth}px group — the tappable row no longer spans the group`).toBeLessThanOrEqual(3);
        expect(c.rowHeight, `the picker's ${c.value} row is ${c.rowHeight}px tall, want >= 44 — the row is the tap target`).toBeGreaterThanOrEqual(PAY_ROW_MIN_HEIGHT);
        // Its label is a span (.payrowtext) beside the control, not a text node.
        expect(c.text.length, `the picker's ${c.value} row has no label text`).toBeGreaterThan(0);
        expect(c.gap, `no label span found beside the picker's ${c.value} control`).not.toBeNull();
        expect(c.gap as number, `the gap from the picker's ${c.value} control to "${c.text}" is ${c.gap}px, want 10px — the label is not sitting beside its control`)
          .toBeGreaterThanOrEqual(PAY_GAP_MIN);
        expect(c.gap as number).toBeLessThanOrEqual(PAY_GAP_MAX);
      }

      // 4. The OTHER half of the same rule: the buyer fields keep 100% width and
      //    44px height. A "fix" that widens the reset to every input in the sheet
      //    leaves the buyer typing their name into a 13px box; one that drops the
      //    buyer rule entirely fails here too.
      const rowWidth = g.groups[0].width;
      expect(rowWidth, "the option group painted no width").toBeGreaterThan(200);
      for (const [field, box] of [["name", g.buyer.name], ["email", g.buyer.email]] as const) {
        expect(Math.abs(box.width - rowWidth), `the buyer ${field} field is ${box.width}px wide in a ${rowWidth}px row — it is no longer full width`)
          .toBeLessThanOrEqual(1);
        expect(box.height, `the buyer ${field} field is ${box.height}px tall, want 44`).toBeGreaterThanOrEqual(43);
        expect(box.height).toBeLessThanOrEqual(45);
      }

      // 5. The pay button spans the same row.
      expect(Math.abs(g.pay.width - rowWidth), `the pay button is ${g.pay.width}px wide in a ${rowWidth}px row`).toBeLessThanOrEqual(1);

      // 6. Nothing scrolls sideways — the pre-fix shape pushed the label off a
      //    375px screen, and a control that overflows its row would too.
      expect(g.scrollWidth, `the document is ${g.scrollWidth}px wide in a ${g.clientWidth}px viewport`).toBeLessThanOrEqual(g.clientWidth + 1);
    });
  }

  test("the two groups are separate HTML radio groups", async ({ page }) => {
    // Card t_5d625806: both blocks emitted name="notes", so answering the second
    // question cleared the first. Checked in a browser (the Go test checks the
    // markup) because the failure a buyer sees is the CLEARED earlier answer.
    await page.setViewportSize({ width: 1200, height: 900 });
    await openCheckout(page, FIXTURE_URL);

    // Both groups must be addressed by their BLOCK ID. If they share one name
    // (the defect) the second locator cannot be satisfied at all.
    await expect(page.locator(`.sheetfoot input[type=radio][name="${RADIO_BLOCK}"]`),
      `no option control is named "${RADIO_BLOCK}" — the group is not keyed by its block id`).toHaveCount(2);
    await expect(page.locator(`.sheetfoot input[type=radio][name="${IMAGE_CHOICE_BLOCK}"]`),
      `no option control is named "${IMAGE_CHOICE_BLOCK}" — both groups may be sharing one HTML radio group name`).toHaveCount(2);

    const first = page.locator(`.sheetfoot input[type=radio][name="${RADIO_BLOCK}"][value="b"]`);
    const second = page.locator(`.sheetfoot input[type=radio][name="${IMAGE_CHOICE_BLOCK}"][value="panas"]`);
    await first.check();
    await second.check();
    expect(await first.isChecked(), "answering the image-choice question cleared the radio-group answer — one HTML radio group for two questions").toBe(true);
    expect(await second.isChecked()).toBe(true);

    const payload = (await page.evaluate(`jfMenuExtraFields()`)) as Record<string, string>;
    expect(payload[RADIO_BLOCK], `the checkout would submit fields[${RADIO_BLOCK}] = ${payload[RADIO_BLOCK]}`).toBe("b");
    expect(payload[IMAGE_CHOICE_BLOCK]).toBe("panas");
  });
});
