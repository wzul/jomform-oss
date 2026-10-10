// Regression guard: the /form-settings page header must not push the page
// sideways on a phone (card t_8b35a9f4).
//
// THE DEFECT (measured on the built page, real browser, before the fix):
// the header's action cluster was `flex items-center gap-2` — a NON-wrapping
// flex row — inside the header row that DOES wrap. In a long-label locale the
// three nowrap buttons (`jf-btn-ghost`, which refuse to shrink since #655) are
// together wider than the viewport's content box: measured 371.64px in Tamil
// against a 272px content box at 320px. A non-wrapping flex container cannot be
// smaller than its widest item's min-content, so the cluster set the DOCUMENT's
// min-content wider than the viewport and the whole page scrolled sideways:
//
//   ta @ 320px  clientWidth=320  scrollWidth=396  overflow=76px
//   ta @ 360px  overflow=36px     ta @ 390px  overflow=6px
//   ms @ 320px  overflow=8px      ar @ 320px  overflow=11px
//
// The fix is `flex max-w-full flex-wrap` on that one cluster: the buttons wrap
// onto a second line instead of widening the document. Nothing else changed —
// no font size, no smaller padding, no removed action.
//
// WHY THE GUARD MEASURES THE PAGE AND NOT THE CLASS NAME
// A test that asserts the string `flex-wrap` is present would stay green while
// any other class re-created the overflow, and `npm run test:unit` has no DOM.
// This file runs in the `e2e` gate (`npm run e2e`, part of
// scripts/preflight.sh) — the project that runs every spec under web/e2e that
// is not named `a11y*` — because that gate renders the real built page in a
// real browser, and this file asserts the outcome a user sees:
// `document.documentElement.scrollWidth === clientWidth` at every locale and
// phone width, with all three actions still present, inside the viewport and
// still linking where they used to.
//
// (The paragraph above said `npm run a11y` until #884 split the two projects:
// the a11y project's testMatch is `a11y*.spec.ts` only, so a sabotage run of
// THIS file has to be driven through `npm run e2e` — running the a11y gate and
// reading its green as "this guard did not bite" measures nothing at all.)
//
// WHY IT NOW RENDERS BOTH FORM MODES (the follow-up this file is named after)
// The first version of this guard stubbed ONE form — `{ id, slug, title,
// status, url, config, theme }` with no `mode` — so every case it measured was
// a PAYMENT form and the second action was always the Sales link. A response
// form renders a different and, in English, longer label: `formEntriesAction`
// (src/lib/formMode.ts) returns `nav.sales` for a payment form and
// `nav.submissions` otherwise, and `nav.submissions` in en is "Submission",
// measured 118px against "Sales"' 75px. The header row is exactly what this
// guard is about, so the widest second-label case was the one case it never
// laid out. The stub is now parameterised by mode and every measurement below
// runs for BOTH modes, per mode asserting the label that mode must carry — so
// the stub cannot silently drift back to a single variant.
//
// The mode→label mapping is asserted as the RENDERED text (`nav.sales` for
// payment, `nav.submissions` for response), not as the presence of `mode` in
// the stub: if the stub stops sending `mode`, or the mapping in formMode.ts
// flips, the response case fails on its label instead of quietly measuring the
// payment one again. The destination is the same for both modes
// (`/sales?form=<id>`, which renders a response form's entries in the answers
// layout, #604), so the href set is shared.
//
// WHAT MAKES IT BITE (verified, not assumed)
// Neutralising the fix (back to `flex items-center gap-2`), rebuilding and
// re-running this file turns it red at 320px in ta/ms/ar with the measured
// overflow quoted above, and the failing assertion names the cluster and the
// action that crosses the viewport edge. Since this file renders both modes,
// that neutralisation reddens BOTH of them — the response variant is not a
// case this guard only nominally covers.
//
// WHAT MAKES THE DESKTOP LABEL ASSERTION BITE (t_76feb850, measured)
// The desktop case used to assert its entries label PAGE-WIDE
// (`a:text-is("Sales")`.first()), and at 1024px that is not the header action:
// the sidebar's own `nav.sales` link (Shell.tsx) is visible and comes first in
// document order. Two mutations of the header's entries label
// (page.tsx:1230's `t(entries.labelKey)`), each built and driven:
//   * M1 — the header renders the OTHER mode's key, for both modes
//     (`t(entries.labelKey === "nav.sales" ? "nav.submissions" : "nav.sales")`):
//     THIS file goes 12 failed (10 phone + 2 desktop), the desktop cases failing
//     on their own `nth(1)` assertion ("Expected substring: Sales / Received
//     string: Submission", and the reverse). The PRE-FIX file (page-wide, at
//     a77bfa87) driven against the same build goes 11 failed / **1 passed**: the
//     survivor is the PAYMENT desktop case, whose page-wide "Sales" is satisfied
//     by the sidebar link while the header says "Submission". That single green
//     is the hole this card closed. (The response variant failed there only
//     because it has no sidebar twin — it was scoped by accident.)
//   * M2 — the same per-mode swap confined to >=768px
//     (`window.matchMedia("(min-width: 768px)").matches`): THIS file goes
//     10 passed / 2 failed — both desktop cases red at that same `nth(1)`
//     assertion with every phone case green. So the assertion added here catches
//     a drift the phone loop cannot see; it is not a duplicate of it.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

// Phone widths from the card, plus the 430px where the desktop layout starts to
// take over; the header row is still a single line at all four.
const WIDTHS = [320, 360, 390, 430] as const;
const LANGS = ["en", "ms", "zh", "ta", "ar"] as const;

const FORM_ID = "form_header_guard";
const FORM_URL = "https://jomform.example/f/demo-form";

/**
 * The two collection modes this page renders a header for, each with the
 * entries label it MUST carry (`formEntriesAction` in src/lib/formMode.ts).
 * Every measurement below loops over this list, so a variant the guard renders
 * is a variant that can regress. `mode` is the TOP-LEVEL field the Go API
 * serialises (`restForm.Mode`); the legacy no-mode payload (which the API
 * answers as "payment") is covered by api/internal/httpapi's payload test and
 * by src/lib/formMode.test.ts, not by this browser gate.
 */
const MODES = [
  { mode: "payment", entriesLabelKey: "nav.sales" },
  { mode: "response", entriesLabelKey: "nav.submissions" },
] as const;

type ModeVariant = (typeof MODES)[number];
type LabelKey = "fs.editFields" | "nav.sales" | "nav.submissions" | "bld.view";

// RTL locales lay out from the right, so "inside the viewport" must be checked
// against both edges (an RTL overflow runs off the LEFT edge).
const isRTL = (lang: string) => lang === "ar";

// stubAPI answers every /api/* call the page makes, so the page reaches its
// loaded state without a Go API, a session cookie or a database. Registered as
// a catch-all FIRST and the specific routes after it: Playwright resolves
// matching routes in reverse registration order, so the later (specific) ones win.
//
// `mode` comes from the caller: it is the whole point of this guard. Dropping
// it would render the payment variant for every case again.
async function stubAPI(page: Page, mode: ModeVariant["mode"]) {
  const json = (body: unknown) => ({
    status: 200,
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
  // lang:"" (never set) keeps the browser value in effect, which is how the
  // spec pins the locale; the promotion path then POSTs and the stub accepts it.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: FORM_URL,
        mode,
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
}

type HeaderAction = {
  tag: string;
  text: string;
  href: string | null;
  left: number;
  right: number;
  width: number;
  top: number;
  scrollOverflow: number;
  visible: boolean;
};

type HeaderGeometry = {
  clientWidth: number;
  scrollWidth: number;
  // The header action cluster: both edges, its own internal overflow, and the
  // class list it actually carries (so a failure can name what it found).
  cluster: { className: string; left: number; right: number; scrollOverflow: number };
  row: { className: string; left: number; right: number };
  actions: HeaderAction[];
};

// measureHeader reads the live DOM: the document's own overflow, the header
// action cluster's box, and every action's box + href.
async function measureHeader(page: Page): Promise<HeaderGeometry> {
  return page.evaluate(() => {
    const de = document.documentElement;
    // The three header actions are the only jf-btn-ghost links in the document
    // at this point; anchor on the FIRST one's parent, which is the cluster.
    const anchors = [...document.querySelectorAll("a.jf-btn-ghost")] as HTMLAnchorElement[];
    if (anchors.length === 0) throw new Error("no header action (a.jf-btn-ghost) rendered");
    const cluster = anchors[0].parentElement as HTMLElement;
    const row = cluster.parentElement as HTMLElement;
    const box = (el: Element) => {
      const r = el.getBoundingClientRect();
      return {
        left: Math.round(r.left * 100) / 100,
        right: Math.round(r.right * 100) / 100,
        width: Math.round(r.width * 100) / 100,
        top: Math.round(r.top * 100) / 100,
      };
    };
    const c = box(cluster);
    const rw = box(row);
    return {
      clientWidth: de.clientWidth,
      scrollWidth: de.scrollWidth,
      cluster: {
        className: cluster.className,
        left: c.left,
        right: c.right,
        scrollOverflow: cluster.scrollWidth - cluster.clientWidth,
      },
      row: { className: row.className, left: rw.left, right: rw.right },
      actions: anchors.map((a) => {
        const b = box(a);
        return {
          tag: a.tagName,
          text: (a.textContent || "").trim(),
          href: a.getAttribute("href"),
          left: b.left,
          right: b.right,
          width: b.width,
          top: b.top,
          scrollOverflow: a.scrollWidth - a.clientWidth,
          visible: b.width > 0 && a.getBoundingClientRect().height > 0,
        };
      }),
    };
  });
}

for (const lang of LANGS) {
  for (const variant of MODES) {
    const { mode, entriesLabelKey } = variant;
    test(`the /form-settings header does not scroll the page sideways in ${lang}, ${mode} form, at 320/360/390/430px`, async ({
      browser,
    }) => {
      // The e2e gate runs this file alongside the other feature specs on a
      // shared host, so the per-test budget is generous: a slow first paint
      // under contention must not read as a layout defect.
      test.setTimeout(180_000);
      const t = (key: LabelKey) => DICT[key][lang];
      // The second action is the mode's entries action: Sales for a payment
      // form, Submission for a response form. The destination is the same for
      // both modes.
      const expectedLabels = [t("fs.editFields"), t(entriesLabelKey), t("bld.view")];
      const expectedHrefs = [`/builder?form=${FORM_ID}`, `/sales?form=${FORM_ID}`, FORM_URL];
      const measured: string[] = [];

      for (const width of WIDTHS) {
        const context = await browser.newContext({
          viewport: { width, height: 720 },
          deviceScaleFactor: 1,
        });
        // A FRESH context per width/locale/mode: no cookie, no localStorage and
        // no service worker carried over from an earlier case, so this is the
        // state a new visitor gets.
        await context.addInitScript(
          ([l]) => {
            localStorage.setItem("jf_workspace", "demo");
            localStorage.setItem("jf_lang", l as string);
          },
          [lang],
        );
        const page = await context.newPage();
        page.setDefaultTimeout(45_000);
        await stubAPI(page, mode);
        await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });

        // All three actions must be rendered before anything is measured, or a
        // slow load would read as "the actions are missing".
        //
        // THREE is correct for THIS fixture and has to stay tied to it: the
        // View link is guarded by `form?.url &&` in page.tsx, so a draft form,
        // or a published form stubbed with `url: ""`, renders only TWO actions.
        // That is correct behaviour (verified separately: 2 actions, page clean
        // at 320px in en/ta/ar), not a missing action — so do not lift this
        // count onto a fixture where it is false, and do not drop `url` here.
        await expect(page.locator("a.jf-btn-ghost")).toHaveCount(3, { timeout: 45_000 });
        // The labels are asserted on the header's OWN actions (`nth`), never by
        // page-wide text: the sidebar carries its own "Sales" link
        // (`/sales` in Shell.tsx), so `a:text-is("Sales")` resolves to that
        // OFF-SCREEN nav item first at phone widths and would report the header
        // action as hidden. Scoped this way the assertion is about the header
        // row, which is what this guard owns.
        await expect(page.locator("a.jf-btn-ghost").nth(0)).toContainText(expectedLabels[0], {
          timeout: 45_000,
        });
        // The mode's own label must be on screen before the measurement: this
        // is the assertion that fails if the stub or the mapping drifts back to
        // a single variant, which is exactly how this guard was blind before.
        await expect(page.locator("a.jf-btn-ghost").nth(1)).toContainText(expectedLabels[1], {
          timeout: 45_000,
        });
        await expect(page.locator("a.jf-btn-ghost").nth(1)).toBeVisible({ timeout: 45_000 });
        // The mode must actually have reached the page: a payment form's
        // entries action is the Sales one, a response form's is Submission.
        // Asserted as one of the two, on the header action itself, so the two
        // variants cannot both drift onto the same label unnoticed.
        expect(
          await page.locator("a.jf-btn-ghost").nth(1).textContent(),
          `the ${mode} form's entries action is not labelled ${entriesLabelKey} — the stub or ` +
            `formEntriesAction has drifted back to a single variant`,
        ).toBe(expectedLabels[1]);
        // The locale must actually have applied, or the measurement below is of
        // some other language and proves nothing.
        expect(await page.evaluate(() => document.documentElement.dir)).toBe(
          isRTL(lang) ? "rtl" : "ltr",
        );

        const g = await measureHeader(page);
        const where = `${lang} ${mode} @ ${width}px`;

        // Acceptance: the page itself never scrolls sideways.
        expect(
          g.scrollWidth,
          `${where}: the document scrolls sideways by ${g.scrollWidth - g.clientWidth}px ` +
            `(clientWidth ${g.clientWidth}, scrollWidth ${g.scrollWidth}). The header action ` +
            `cluster is "${g.cluster.className}" spanning [${g.cluster.left}, ${g.cluster.right}].`,
        ).toBe(g.clientWidth);

        // The cluster itself stays inside the viewport (both edges — an RTL
        // overflow runs off the left edge).
        expect(
          g.cluster.left,
          `${where}: the header action cluster starts at ${g.cluster.left}px, left of the viewport`,
        ).toBeGreaterThanOrEqual(0);
        expect(
          g.cluster.right,
          `${where}: the header action cluster ends at ${g.cluster.right}px, past the ` +
            `${g.clientWidth}px viewport. It is "${g.cluster.className}" — a non-wrapping row ` +
            `cannot shrink below its widest button, so it widens the whole document.`,
        ).toBeLessThanOrEqual(g.clientWidth);

        // No internal overflow either: the buttons must not be clipped.
        expect(
          g.cluster.scrollOverflow,
          `${where}: the header action cluster scrolls internally by ${g.cluster.scrollOverflow}px`,
        ).toBeLessThanOrEqual(1);

        // The three actions are all still there, in order, still linking where
        // they did — and the second one still carries THIS mode's label — each
        // on screen and with its label uncut.
        expect(g.actions.map((a) => a.tag)).toEqual(["A", "A", "A"]);
        expect(g.actions.map((a) => a.text)).toEqual(expectedLabels);
        expect(g.actions.map((a) => a.href)).toEqual(expectedHrefs);
        for (const a of g.actions) {
          expect(a.visible, `${where}: the "${a.text}" action is not visible`).toBe(true);
          expect(
            a.left,
            `${where}: the "${a.text}" action starts at ${a.left}px, left of the viewport`,
          ).toBeGreaterThanOrEqual(0);
          expect(
            a.right,
            `${where}: the "${a.text}" action ends at ${a.right}px, past the ${g.clientWidth}px viewport`,
          ).toBeLessThanOrEqual(g.clientWidth);
          expect(
            a.scrollOverflow,
            `${where}: the "${a.text}" action overflows itself by ${a.scrollOverflow}px`,
          ).toBeLessThanOrEqual(1);
          expect(a.width, `${where}: the "${a.text}" action has no width`).toBeGreaterThan(40);
        }

        // Print the raw numbers: they are the acceptance evidence for this card,
        // not just pass/fail.
        measured.push(
          `${where}: client=${g.clientWidth} scroll=${g.scrollWidth} ` +
            `cluster="${g.cluster.className}" x=[${g.cluster.left},${g.cluster.right}] ` +
            g.actions
              .map((a) => `"${a.text}" x=[${a.left},${a.right}] w=${a.width} top=${a.top}`)
              .join(" | "),
        );

        await context.close();
      }

      console.log(measured.join("\n"));
    });
  }
}

// The desktop header keeps its shape: all three actions on ONE line, inside the
// header row they share with the title block — for BOTH modes (the response
// variant's wider second label must not push it onto its own line either).
//
// Note on why this does not assert `cluster.right === row.right`: at 1024px the
// subtitle's `max-w-xl` (576px) makes the title block wide enough that the
// action cluster already wraps onto a second line of the OUTER row (which has
// carried `flex-wrap` since before this card) — measured on the pre-fix and
// post-fix builds alike, cluster x=[248,534] inside a row of [248,1000]. That is
// the outer row's pre-existing behaviour, not something this fix introduced, so
// the assertions here are the ones the fix actually owns: the actions stay on
// one line together and inside the row.
for (const variant of MODES) {
  const { mode, entriesLabelKey } = variant;
  test(`the desktop header keeps its three actions on one line at 1024px (${mode} form)`, async ({
    browser,
  }) => {
    const context = await browser.newContext({ viewport: { width: 1024, height: 800 } });
    await context.addInitScript(() => {
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    });
    const page = await context.newPage();
    await stubAPI(page, mode);
    await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });
    const expectedLabels = [DICT["fs.editFields"].en, DICT[entriesLabelKey].en, DICT["bld.view"].en];
    await expect(page.locator("a.jf-btn-ghost")).toHaveCount(3, { timeout: 45_000 });
    // The labels are asserted on the header's OWN actions (`nth`), never by
    // page-wide text — the same scoping the phone loop above uses, and for a
    // sharper reason here: at 1024px the sidebar's own `nav.sales` link
    // (Shell.tsx) IS visible and comes FIRST in document order, so for the
    // payment variant `a:text-is("Sales")`.first() resolves to that nav item.
    // This case then passes while the header action carries the other mode's
    // label — the exact drift this guard exists to make loud. Scoped this way
    // the assertion is about the header row, which is what this guard owns.
    await expect(page.locator("a.jf-btn-ghost").nth(0)).toContainText(expectedLabels[0], {
      timeout: 45_000,
    });
    await expect(page.locator("a.jf-btn-ghost").nth(1)).toContainText(expectedLabels[1], {
      timeout: 45_000,
    });

    const g = await measureHeader(page);
    // The three actions, in order, each carrying THIS mode's label — asserted on
    // the header's own anchors (`measureHeader` reads only `a.jf-btn-ghost`), so
    // a sidebar link with a matching label cannot satisfy any of them.
    expect(g.actions.map((a) => a.text)).toEqual(expectedLabels);
    const tops = new Set(g.actions.map((a) => a.top));
    expect(
      tops.size,
      `the header actions wrapped on desktop (tops: ${g.actions.map((a) => a.top).join(", ")})`,
    ).toBe(1);
    expect(
      g.cluster.right,
      `the action cluster (right ${g.cluster.right}) escapes the header row (right ${g.row.right})`,
    ).toBeLessThanOrEqual(g.row.right);
    expect(
      g.cluster.left,
      `the action cluster (left ${g.cluster.left}) escapes the header row (left ${g.row.left})`,
    ).toBeGreaterThanOrEqual(g.row.left);
    // And the desktop page does not scroll sideways either.
    expect(
      g.scrollWidth,
      `the desktop page scrolls sideways by ${g.scrollWidth - g.clientWidth}px`,
    ).toBe(g.clientWidth);
    await context.close();
  });
}
