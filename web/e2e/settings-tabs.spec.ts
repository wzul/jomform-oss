// Regression guard: the settings tab strip must stay ONE row on a phone.
//
// THE DEFECT
// `web/src/app/settings/page.tsx` rendered its tabs in a wrapping flex row
// (`flex flex-wrap gap-1 border-b border-slate-200`) with no scroll or overflow
// treatment. Measured on the built page with the tab count settled, the strip
// consumed 4 rows / **161px** at 320px and 360px (20.1% of an 800px viewport,
// 25.2% of 640px) and 5 rows / 202px in Tamil at 320px — that is the tab chrome
// above the card on a phone, not the card. It was 3 rows / 120px at 390/430px
// and 2 rows / 79px at 1024px, so the wrap was never only a phone problem, it
// was worst there.
//
// THE FIX
// One row that scrolls sideways — `overflow-x-auto` on the same element that
// carries the tabs and the `border-b`, tabs `whitespace-nowrap` — plus a reveal
// (`revealTab` in the page) so the SELECTED tab cannot arrive scrolled out of
// view: without it, at 320px the default tab (Affiliates, last-but-one) measured
// 867px inside a 272px box, i.e. the selected tab was off-screen and the first
// thing you saw was the oldest tabs. The reveal runs on mount, on selection
// change, when the tab set grows (the admin-only tabs appear once the session
// bundle resolves `role`), and on keyboard focus.
//
// WHY A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
// This defect lives entirely in the rendered box. A guard that asserts "the
// strip is not `flex flex-wrap`" is toothless: any other arrangement that
// re-creates four rows keeps it green. This file measures the RENDERED strip
// per locale and per width in the real built page, in the axe gate
// (`npm run a11y`, part of scripts/preflight.sh) — the one gate in this repo
// that renders the real page in a real browser, and the same gate the
// affiliate-row guard uses.
//
// THE TAB COUNT IS DERIVED, NOT PINNED
// This guard is about the strip's BOX, so how many tabs are in it is an INPUT,
// not an expectation: the expected tabs are written down ONCE, in TAB_KEYS
// below (render order, dictionary key per label), and every count comparison is
// `TAB_KEYS.length`. A tab added to `settings/page.tsx` is a one-line addition
// to TAB_KEYS. It used to be pinned at a literal eleven in three places (the
// settle loop, RULE 1, and TAB_KEYS itself), so the twelfth tab (`workspace`,
// #682) turned this whole file RED against a page that was rendering correctly:
// 25 failed / 0 passed, reported as "the settings page settled at 12 tabs, not
// eleven — the session bundle did not resolve an admin role", which blamed the
// session for a tab-count drift in this file. The count is no longer written as
// a literal anywhere. TAB_KEYS can still go stale the one way a derived list
// can — a page tab missing from it — so the page's own `TAB_IDS` is read out of
// its source at load time and compared against TAB_KEYS, and that failure names
// the tab.
//
// WHAT MAKES IT BITE (not assumed — each source-side half was reverted in turn
// and this file re-run against the rebuilt page):
//   * reverting the strip class alone (back to `flex flex-wrap`) -> 25 failed /
//     0 passed, e.g. "en@320: the tab strip wrapped to 4 rows (it is 161px tall)"
//   * keeping the scroller but disabling the reveal -> 25 failed / 0 passed,
//     e.g. "the selected tab is scrolled out of the strip's visible window
//     (scrollWidth 928 inside clientWidth 272)"
//   * keeping both but dropping the inset focus-ring class -> 25 failed / 0
//     passed, e.g. "the focus ring of ... is clipped by the strip on its r edge
//     (-3px of clearance)"
//   RULE 1  the strip is exactly ONE row at every width and locale
//   RULE 2  the strip is a horizontal scroller, and contains its own overflow
//           (its box must not spill into the page — a sideways page scroll is a
//           worse defect than the tall strip)
//   RULE 3  the strip stays SHORT (a fixed pixel ceiling, not "fewer rows")
//   RULE 4  every tab is nowrap and every label is complete and UNTRUNCATED,
//           matching this locale's dictionary string (not English, not a key)
//   RULE 5  the tabs keep `role="tab"` + `aria-selected` (exactly one selected)
//   RULE 6  the SELECTED tab is inside the strip's visible window on load
//   RULE 7  focusing a tab the keyboard has walked to reveals it — and does NOT
//           change the selection (a Tab-cycle must not silently switch tabs)
//   RULE 8  that tab's focus RING is fully inside the strip's clip box. A scroll
//           container clips its descendants, and the app's global :focus-visible
//           ring is drawn outside the box, so the strip carries an inset ring
//           (`web/src/app/globals.css` → `.jf-tabstrip`). Measured pre-fix: none
//           of the tabs had a complete ring; with the inset rule, all of them.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// the gate needs nothing but the build it is already serving.
import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { DICT } from "../src/components/i18nDict";

// The phone widths from the card, plus 1024px where the strip must still be one
// row (it wrapped to two there before).
const WIDTHS = [320, 360, 390, 430, 1024] as const;
const LANGS = ["en", "ms", "ta", "zh", "ar"] as const;

// The settings tabs, in render order (`settings/page.tsx` TAB_IDS) with the
// dictionary key each label comes from. This list is what the page is expected
// to render, and it is the ONLY place the count lives: `workspace` is last
// because the page appends it there. `adminOnly` marks the tabs #163 hides from
// a non-admin member (`chip`, `members`) — the two the settle loop waits for.
const TAB_KEYS: ReadonlyArray<{ key: keyof typeof DICT; adminOnly?: true }> = [
  { key: "set.tab.chip", adminOnly: true },
  { key: "set.tab.domains" },
  { key: "set.tab.tracking" },
  { key: "set.tab.einvoice" },
  { key: "set.tab.webhooks" },
  { key: "set.tab.emel" },
  { key: "set.tab.members", adminOnly: true },
  { key: "set.tab.storefront" },
  { key: "set.tab.legal" },
  { key: "set.tab.currency" },
  { key: "set.tab.affiliates" },
  { key: "set.tab.workspace" },
];

// The count every assertion derives from: the full list, which is what the
// admin bundle this spec stubs renders (#163 hides the admin-only tabs only from
// a member). A new tab is a one-line addition above; nothing else in this file
// compares against a literal count.
const TAB_COUNT = TAB_KEYS.length;

// What a member sees, since #163 drops the `adminOnly` tabs: the floor the
// settled count is sanity-checked against before measuring.
const NON_ADMIN_TAB_COUNT = TAB_KEYS.filter((t) => !t.adminOnly).length;

// The tabs the PAGE declares, read out of its own source (`TAB_IDS` in
// `settings/page.tsx`). Read as source text rather than imported: the page is a
// client component whose module graph needs React, and its declaration is what
// this guard is written against.
const TAB_IDS = readPageTabIds();

// The settings page's own declaration, resolved once. Throws (before any browser
// starts) when the page has no `TAB_IDS` array at all.
function readPageTabIds(): string[] {
  // `__dirname`, not `import.meta.url`: package.json has no `"type": "module"`,
  // so Playwright loads this spec as CommonJS and `import.meta` is a syntax
  // error there (the spec then fails to load at all — "No tests found"). The
  // path is a literal, so nothing a page or a test can influence it.
  const src = readFileSync(join(__dirname, "../src/app/settings/page.tsx"), "utf8");
  const decl = src.match(/const TAB_IDS[^=]*=\s*\[([^\]]*)\]/);
  if (!decl) throw new Error("settings/page.tsx no longer declares TAB_IDS — this guard reads it to derive the expected tab count");
  return [...decl[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
}

// The derived list has to describe the page: same tabs, same order. That is the
// ONE way a derived list can still be wrong — a tab the page gained and TAB_KEYS
// never heard about would otherwise be measured against the wrong expectations
// and reported as a missing label instead of a missing entry. Checked before any
// browser starts, and the failure NAMES the tab.
{
  const listed = TAB_KEYS.map(({ key }) => key as string);
  const declared = TAB_IDS.map((id) => `set.tab.${id}`);
  if (listed.join(",") !== declared.join(",")) {
    const missing = declared.filter((k) => !listed.includes(k));
    throw new Error(
      `settings/page.tsx declares tabs this file's TAB_KEYS does not list, or lists them out of order` +
        (missing.length ? ` (TAB_KEYS is missing ${missing.join(", ")})` : "") +
        `.\n  page TAB_IDS : ${declared.join(", ")}` +
        `\n  spec TAB_KEYS: ${listed.join(", ")}` +
        `\nAdd the page's tab to TAB_KEYS in render order so the guard measures the page that ships.`,
    );
  }
}

// A key that typos, or is dropped from the dictionary, would otherwise throw a
// TypeError from `DICT[k][lang]` inside RULE 4 — which reads like a broken spec
// rather than a stale list. Check the membership up front, where the message can
// say which key.
for (const { key } of TAB_KEYS) {
  if (!Object.prototype.hasOwnProperty.call(DICT, key)) {
    throw new Error(`TAB_KEYS lists ${key}, which is not in the dictionary`);
  }
}


// A one-row strip of `px-3 py-2` `text-sm` tabs is ~37-39px tall. The ceiling is
// deliberately well under two rows (~79px) so "2 rows on a big phone" cannot
// pass as a fix, and it is an absolute height rather than a fraction of the
// viewport so the rule does not move when the viewport does.
const MAX_STRIP_HEIGHT_PX = 60;
// Sub-pixel rounding slack for box comparisons.
const SLACK_PX = 1;

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: true },
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null,
  products: [], summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  // lang:"" (never set) keeps the browser value in effect, which is how this
  // spec pins the locale.
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/workspaces\/[^/]+\/affiliates(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) => r.fulfill(json({ rates: [], cached: false })));
}

type TabMeasure = {
  text: string;
  run: number;
  spanW: number;
  clientW: number;
  scrollW: number;
  whiteSpace: string;
  selected: boolean;
};
type StripMeasure = {
  tabCount: number;
  rows: number;
  height: number;
  overflowX: string;
  scrollWidth: number;
  clientWidth: number;
  spillsIntoPage: boolean;
  selectedInView: boolean;
  selectedText: string;
  selectedCount: number;
  tabs: TabMeasure[];
};

// Every number comes from the element's rendered box and computed style.
async function measureStrip(page: Page): Promise<StripMeasure> {
  return page.evaluate(`(() => {
    const tabs = [...document.querySelectorAll('[role="tab"]')];
    if (!tabs.length) throw new Error("no tabs");
    const strip = tabs[0].parentElement;
    const r2 = (n) => Math.round(n * 100) / 100;
    const sr = strip.getBoundingClientRect();
    const host = strip.parentElement.getBoundingClientRect();
    const rows = new Set(tabs.map((t) => Math.round(t.getBoundingClientRect().top)));
    const ctx = document.createElement("canvas").getContext("2d");
    const list = tabs.map((t) => {
      const spans = [...t.querySelectorAll("span")];
      const vis = spans.find((s) => getComputedStyle(s).display !== "none");
      const cs = getComputedStyle(vis || t);
      const text = (vis ? vis.textContent : t.textContent || "").trim();
      ctx.font = cs.font;
      return {
        text,
        // The glyph run of the label this tab has to render, measured in the
        // tab's OWN computed font (canvas, so it is the real render width and
        // not the box the text happens to be clipped into).
        run: r2(ctx.measureText(text).width),
        spanW: vis ? r2(vis.getBoundingClientRect().width) : 0,
        clientW: t.clientWidth, scrollW: t.scrollWidth,
        whiteSpace: getComputedStyle(t).whiteSpace,
        selected: t.getAttribute("aria-selected") === "true",
      };
    });
    const sel = tabs.find((t) => t.getAttribute("aria-selected") === "true");
    const sb = sel ? sel.getBoundingClientRect() : null;
    // A scroller must contain its own overflow: if the strip's own box is wider
    // than the column it lives in, the overflow escapes into the PAGE.
    const overflowX = getComputedStyle(strip).overflowX;
    return {
      tabCount: tabs.length,
      rows: rows.size,
      height: r2(sr.height),
      overflowX,
      scrollWidth: strip.scrollWidth,
      clientWidth: strip.clientWidth,
      spillsIntoPage: sr.right > host.right + 1 || sr.left < host.left - 1,
      selectedInView: sb ? (sb.left >= sr.left - 1 && sb.right <= sr.right + 1) : false,
      selectedText: sel ? (sel.textContent || "").trim() : "",
      selectedCount: tabs.filter((t) => t.getAttribute("aria-selected") === "true").length,
      tabs: list,
    };
  })()`) as Promise<StripMeasure>;
}

for (const lang of LANGS) {
  for (const width of WIDTHS) {
    test(`settings tab strip stays one row: ${lang} @ ${width}px`, async ({ page, baseURL }) => {
      await stubAPI(page);
      await page.addInitScript(
        (l) => {
          // A FRESH browser state per case: storage is cleared first so an
          // earlier spec (or an earlier card) cannot leave jf_lang / jf_workspace
          // behind and make this measure a state no new visitor has.
          try {
            localStorage.clear();
            sessionStorage.clear();
            localStorage.setItem("jf_workspace", "demo");
            localStorage.setItem("jf_lang", l);
            localStorage.setItem("jf_settings_tab", "affiliates");
          } catch {
            /* storage unavailable */
          }
        },
        lang,
      );
      await page.setViewportSize({ width, height: 800 });
      await page.goto(`${baseURL}/settings?tab=affiliates`, { waitUntil: "domcontentloaded" });

      // Wait for the tab count to SETTLE BEFORE MEASURING. The tab set grows
      // once the session bundle resolves `role` (#163 hides the admin-only CHIP
      // and Members tabs from a member), so measuring the first paint reads a
      // different strip than a user sees.
      //
      // The wait names no count and no expected value: it returns as soon as the
      // count stops changing, and what it settled at is asserted right below.
      // That is the whole point of the derivation — the strip is the subject of
      // this file, the tab list is an input, and a page that gained a tab must
      // reach the measurements (where a real defect would show) instead of
      // dying here on a literal.
      //
      // The wait still waits for STABILITY rather than merely a count, which is
      // what keeps it from measuring a mid-load strip whose reveal has not run
      // for the tabs the session is about to add.
      const settled = await page.evaluate(`(async () => {
        let last = -1, stable = 0;
        const deadline = Date.now() + 20000;
        while (Date.now() < deadline) {
          const n = document.querySelectorAll('[role="tab"]').length;
          stable = n === last ? stable + 1 : 0;
          last = n;
          if (stable >= 3) return n;
          await new Promise((r) => setTimeout(r, 100));
        }
        return last;
      })()`) as number;
      // Sanity only: the strip has to have settled at something meaningful for
      // the measurements below to say anything. The count is NOT an expectation
      // of this file — RULE 1 compares it against the derived count — so this
      // exists so that a page which renders almost nothing reports THAT, instead
      // of failing an unrelated box measurement.
      expect(
        settled,
        `${lang}@${width}: the settings strip settled at ${settled} tabs — fewer than the ` +
          `${NON_ADMIN_TAB_COUNT} a member sees, so there is no strip to measure ` +
          `(TAB_KEYS expects ${TAB_COUNT} for the admin session this spec stubs)`,
      ).toBeGreaterThanOrEqual(NON_ADMIN_TAB_COUNT);
      // The tab set must be settled before measuring, and it must be THIS
      // locale's strings: `role="tab"` + the dictionary label for the active
      // tab. Asserted through the accessible name, so this also fails if the
      // two label spans (`sm:hidden` + `hidden sm:inline`) ever leak into the
      // name as a doubled string.
      await expect(
        page.getByRole("tab", { name: DICT["set.tab.affiliates"][lang], exact: true }),
      ).toHaveCount(1);

      const m = await measureStrip(page);

      // Every tab the page declares renders, in the declared order — the guard
      // on the derived list, and the failure a page-side `TAB_IDS` change
      // produces (it can only be a divergence from the source, never a stale
      // count). RULE 4 below then checks each one's LABEL in this locale.
      expect(
        m.tabs.map((t) => t.text),
        `${lang}@${width}: the settings strip does not render the tabs TAB_IDS declares ` +
          `(${TAB_IDS.join(", ")}) — TAB_KEYS in this file has to list each one, in order`,
      ).toEqual(TAB_IDS.map((id) => DICT[`set.tab.${id}` as keyof typeof DICT][lang]));

      // RULE 1 — exactly one row of tabs. The count is the derived one: it is
      // `TAB_KEYS.length`, not a literal, so adding a tab to TAB_KEYS cannot
      // turn this red.
      expect(
        m.tabCount,
        `${lang}@${width}: the settings page no longer renders its ${TAB_COUNT} expected tabs`,
      ).toBe(TAB_COUNT);
      expect(
        m.rows,
        `${lang}@${width}: the tab strip wrapped to ${m.rows} rows (it is ${m.height}px tall)`,
      ).toBe(1);

      // RULE 2 — the strip is a horizontal scroller that contains its overflow.
      // `visible` here means there is no scroller at all: the tabs are then
      // either wrapping (RULE 1 catches it) or overflowing into the page.
      expect(
        m.overflowX,
        `${lang}@${width}: the strip's computed overflow-x is ${m.overflowX} — it is not a scroller`,
      ).toBe("auto");
      expect(
        m.spillsIntoPage,
        `${lang}@${width}: the strip's own box escapes its column ` +
          `(scrollWidth ${m.scrollWidth} inside clientWidth ${m.clientWidth})`,
      ).toBe(false);

      // RULE 3 — a fixed pixel ceiling, so "2 rows instead of 4" cannot pass.
      expect(
        m.height,
        `${lang}@${width}: the strip is ${m.height}px tall (one row is ~38px)`,
      ).toBeLessThanOrEqual(MAX_STRIP_HEIGHT_PX);

      // RULE 4 — every label complete and untruncated, in THIS locale.
      expect(m.tabs.map((t) => t.text)).toEqual(TAB_KEYS.map(({ key }) => DICT[key][lang]));
      for (const t of m.tabs) {
        expect(
          t.whiteSpace,
          `${lang}@${width}: tab ${JSON.stringify(t.text)} can wrap (white-space ${t.whiteSpace}) — ` +
            `a wrapped label makes the strip taller than one row`,
        ).toBe("nowrap");
        expect(
          t.scrollW,
          `${lang}@${width}: tab ${JSON.stringify(t.text)} is clipped ` +
            `(scrollWidth ${t.scrollW} > clientWidth ${t.clientW})`,
        ).toBeLessThanOrEqual(t.clientW + SLACK_PX);
        expect(
          t.spanW + SLACK_PX,
          `${lang}@${width}: the label ${JSON.stringify(t.text)} is drawn in ${t.spanW}px but its ` +
            `glyph run is ${t.run}px — the label is cut off`,
        ).toBeGreaterThanOrEqual(t.run);
      }

      // RULE 5 — the tab semantics the a11y contract depends on.
      expect(m.selectedCount, `${lang}@${width}: exactly one tab must carry aria-selected`).toBe(1);

      // RULE 6 — the selected tab is inside the strip's visible window. Without
      // the reveal this is the assertion that fails: on the pre-fix strip there
      // is no scroller, so a selected tab cannot be out of view — but with a
      // plain scroller and no reveal, the default tab measured 867px inside a
      // 272px box at 320px.
      expect(
        m.selectedInView,
        `${lang}@${width}: the selected tab ${JSON.stringify(m.selectedText)} is scrolled out of ` +
          `the strip's visible window (scrollWidth ${m.scrollWidth} inside clientWidth ${m.clientWidth})`,
      ).toBe(true);

      // RULE 7 — keyboard: walking to an off-screen tab reveals it and does NOT
      // change which tab is selected.
      if (width < 1024) {
        const before = m.selectedText;
        await page.evaluate(() => {
          const tabs = [...document.querySelectorAll('[role="tab"]')];
          (tabs[tabs.length - 1] as HTMLElement).focus();
        });
        await page.waitForTimeout(150);
        const kb = await page.evaluate(`(() => {
          const el = document.activeElement;
          const strip = el.parentElement;
          const sr = strip.getBoundingClientRect(), b = el.getBoundingClientRect();
          return {
            isTab: el.getAttribute("role") === "tab",
            inView: b.left >= sr.left - 1 && b.right <= sr.right + 1,
            selectedText: [...strip.querySelectorAll('[role="tab"]')]
              .filter((t) => t.getAttribute("aria-selected") === "true")
              .map((t) => (t.textContent || "").trim()).join("|"),
            pageScrollY: window.scrollY,
          };
        })()`) as { isTab: boolean; inView: boolean; selectedText: string; pageScrollY: number };
        expect(kb.isTab, `${lang}@${width}: the focused element is not a tab`).toBe(true);
        expect(
          kb.inView,
          `${lang}@${width}: the focused tab is outside the strip's visible window — a keyboard user ` +
            `cannot see where they are`,
        ).toBe(true);
        expect(
          kb.selectedText,
          `${lang}@${width}: focusing a tab CHANGED the selection (${before} -> ${kb.selectedText}); ` +
            `tabbing through the strip must not switch tabs`,
        ).toBe(before);
        expect(
          kb.pageScrollY,
          `${lang}@${width}: revealing a tab scrolled the PAGE (window.scrollY ${kb.pageScrollY})`,
        ).toBe(0);
      }

      // RULE 8 — the focus ring is fully painted. Walk the strip with focus (the
      // real keyboard path) and compare each tab's ring band against the strip's
      // clip box (its border box minus its border — padding is INSIDE the clip
      // box). Without the inset rule the ring is cut top and bottom on every tab,
      // because the strip is a scroll container and the global ring sits 2px
      // outside the tab's box.
      const rings = await page.evaluate(`(async () => {
        const tabs = [...document.querySelectorAll('[role="tab"]')];
        const strip = tabs[0].parentElement;
        const scs = getComputedStyle(strip);
        const sr = strip.getBoundingClientRect();
        const clip = {
          left: sr.left + (parseFloat(scs.borderLeftWidth) || 0),
          right: sr.right - (parseFloat(scs.borderRightWidth) || 0),
          top: sr.top + (parseFloat(scs.borderTopWidth) || 0),
          bottom: sr.bottom - (parseFloat(scs.borderBottomWidth) || 0),
        };
        const out = [];
        for (const t of tabs) {
          t.focus();
          await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
          const b = t.getBoundingClientRect();
          const cs = getComputedStyle(t);
          const ow = parseFloat(cs.outlineWidth) || 0;
          const off = cs.outlineStyle === "none" ? 0 : (parseFloat(cs.outlineOffset) || 0);
          out.push({
            text: (t.textContent || "").trim(),
            style: cs.outlineStyle,
            clearance: {
              l: Math.round((b.left - off - ow - clip.left) * 10) / 10,
              r: Math.round((clip.right - (b.right + off + ow)) * 10) / 10,
              t: Math.round((b.top - off - ow - clip.top) * 10) / 10,
              b: Math.round((clip.bottom - (b.bottom + off + ow)) * 10) / 10,
            },
          });
        }
        return out;
      })()`) as { text: string; style: string; clearance: Record<string, number> }[];
      for (const r of rings) {
        expect(
          r.style,
          `${lang}@${width}: the focused tab ${JSON.stringify(r.text)} has no focus outline — ` +
            `the strip is a scroller, so the ring must be an INSET one to be painted at all`,
        ).toBe("solid");
        for (const [edge, px] of Object.entries(r.clearance)) {
          expect(
            px,
            `${lang}@${width}: the focus ring of ${JSON.stringify(r.text)} is clipped by the strip ` +
              `on its ${edge} edge (${px}px of clearance)`,
          ).toBeGreaterThanOrEqual(-1);
        }
      }
    });
  }
}
