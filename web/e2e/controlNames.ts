// Shared census: every VISIBLE form control whose only accessible name is its
// PLACEHOLDER (or that has no name at all).
//
// THE CLASS THIS REFUSES. axe-core ACCEPTS a placeholder as an accessible name,
// so the `label` rule PASSES on a control named only by its placeholder and no
// axe gate can see it — while a screen-reader user loses the field's name the
// moment they type (WCAG 2.x name-role-value wants a programmatic name:
// `<label for>` / `aria-label` / `aria-labelledby`). That is why a clean axe run
// is NOT evidence for this property and every site has been found by reading the
// name sources off the LIVE DOM instead:
//
//   * the /products create row's name / price / stock inputs and the /builder
//     slug + AI-prompt controls (card t_becb0f50 — axe reported NOTHING for any
//     of them on the same build where this census named all five);
//   * the /form-settings tabs (card t_9500edf3), whose walk found the class on
//     the General tab;
//   * the builder panel's per-choice rows (card t_c4abb920), where the name went
//     from "Label" to nothing the moment a value was typed.
//
// WHY IT IS A SHARED MODULE. The same census has to run on EVERY route that
// renders controls. A copy per spec is how one route's copy keeps passing while
// another route grows the defect — the exact shape this card is about (the class
// was already refused on /form-settings and the builder panel, and still shipped
// on /products and /builder). One implementation, imported by every spec that
// reaches a surface with form controls:
//
//   e2e/a11y-authed-pages.spec.ts  every one of the twelve signed-in routes it
//                                  opens + each of /form-settings' seven tabs
//   e2e/a11y-products.spec.ts      /products (the create row this class shipped on)
//
// The arm that proves this census BITES lives in a11y-authed-pages.spec.ts: it
// plants a placeholder-only input into a live page and requires this census to
// name it, so a green reading can never be a vacuum.
//
// WHAT IT COUNTS AS A SITE (deliberately narrow, so a route cannot be red for a
// control a user cannot reach):
//   * `input`/`select`/`textarea` that is VISIBLE — `display:none`,
//     `visibility:hidden` or a zero-size box means the user cannot reach it, and
//     a `className="hidden"` file input is correctly not a site;
//   * `type=hidden|submit|button|reset`, which have no author-supplied name;
//   * a control that already reports a name from `aria-label`,
//     `aria-labelledby` (RESOLVED, so a dangling reference does not count) or a
//     `<label>` that wraps it or names it by `for=` — with non-empty text, so an
//     empty `<label>` is a site rather than a pass.
import { expect, type Page } from "@playwright/test";

/** One control this census refused, with just enough detail to fix it. */
export type UnnamedControl = {
  tag: string;
  type: string;
  id: string | null;
  placeholder: string;
  ariaLabel: string | null;
  ariaLabelledby: string | null;
  /** Human-readable handle for a failure message, e.g. `input[type="number"]#qty`. */
  selector: string;
};

/**
 * The census, as a string expression evaluated in the page.
 *
 * Kept as a string (not a function passed to `page.evaluate`) so the SAME
 * expression can be re-evaluated inside a page whose bundle is mid-hydration
 * without shipping a closure — and so the plant arm below can plant a node and
 * re-run the identical rule over it.
 */
export const UNNAMED_CENSUS = `(() => {
  const labelText = (el) => ((el && el.textContent) || "").replace(/\\s+/g, " ").trim();
  const out = [];
  for (const el of document.querySelectorAll("input, select, textarea")) {
    const type = (el.getAttribute("type") || "").toLowerCase();
    if (["hidden", "submit", "button", "reset"].includes(type)) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    // A name from an aria reference: aria-label must be non-empty on its own,
    // aria-labelledby must RESOLVE to non-empty text (a dangling id names
    // nothing, so it is a site, not a pass).
    const ariaLabel = (el.getAttribute("aria-label") || "").trim();
    if (ariaLabel) continue;
    const labelledby = (el.getAttribute("aria-labelledby") || "").trim();
    if (labelledby) {
      const resolved = labelledby
        .split(/\\s+/)
        .map((id) => labelText(document.getElementById(id)))
        .join(" ")
        .trim();
      if (resolved) continue;
    }
    const wrap = el.closest("label");
    if (wrap && labelText(wrap)) continue;
    const id = el.getAttribute("id");
    if (id) {
      const explicit = document.querySelector('label[for="' + CSS.escape(id) + '"]');
      if (labelText(explicit)) continue;
    }
    out.push({
      tag: el.tagName.toLowerCase(),
      type: type,
      id: id,
      placeholder: el.getAttribute("placeholder") || "",
      ariaLabel: el.getAttribute("aria-label"),
      ariaLabelledby: el.getAttribute("aria-labelledby"),
      selector:
        el.tagName.toLowerCase() +
        (type ? '[type="' + type + '"]' : "") +
        (id ? "#" + id : "") +
        ' placeholder=' + JSON.stringify(el.getAttribute("placeholder") || ""),
    });
  }
  return out;
})()`;

/** Every visible control on the page that this census refuses. */
export async function readUnnamedControls(page: Page): Promise<UnnamedControl[]> {
  return (await page.evaluate(UNNAMED_CENSUS)) as UnnamedControl[];
}

/** The failure message, so a red run names the route AND every site on it. */
export function unnamedMessage(where: string, unnamed: UnnamedControl[]): string {
  return (
    `${where} has ${unnamed.length} control(s) named only by a placeholder (or not named at all): ` +
    `${JSON.stringify(unnamed.map((u) => u.selector))}. axe ACCEPTS a placeholder as a name, so a clean ` +
    `axe scan does NOT prove these are named — and the name is gone the moment the user types. Give each ` +
    `one an id + <label htmlFor>, or an aria-label.`
  );
}

/**
 * The invariant: no visible control on this page may be named only by its
 * placeholder. REACHABILITY IS THE CALLER'S JOB — this asserts the census is
 * clean, so a caller that renders nothing would pass vacuously; every caller
 * asserts the page rendered first (and the plant arm proves the census bites).
 */
export async function expectEveryControlNamed(page: Page, where: string): Promise<void> {
  const unnamed = await readUnnamedControls(page);
  expect(unnamed, unnamedMessage(where, unnamed)).toEqual([]);
}

/**
 * Plants a control whose ONLY name is its placeholder — the shape the defect
 * shipped — so an arm can require the census to refuse it. Returns the id it
 * used, which is also what the census reports back.
 */
export async function plantPlaceholderOnlyInput(page: Page, id = "jf-planted-input"): Promise<string> {
  await page.evaluate((plantedId) => {
    const host = document.querySelector("#main-content") || document.body;
    const input = document.createElement("input");
    input.setAttribute("id", plantedId);
    input.setAttribute("placeholder", "something");
    host.appendChild(input);
  }, id);
  return id;
}

/** Removes anything a `plant*` call added, so the next reading is the shipped page. */
export async function removePlanted(page: Page, id: string): Promise<void> {
  await page.evaluate((plantedId) => {
    document.querySelectorAll(`#${CSS.escape(plantedId)}`).forEach((n) => n.remove());
  }, id);
}
