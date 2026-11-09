// The auth + public cluster is on the primitive layer (card t_8f20418d).
//
// WHY THIS FILE EXISTS. The migration in that card replaces a page's own
// `div`/`button`/`input` with `Card` / `Field`+`Input` / `Button`, and the way
// that migration fails is NOT a compile error: a primitive swap silently drops
// an attribute the page's behaviour depends on. `name` is how the page's
// `new FormData(form)` submit finds the field; `type="submit"` is what makes
// Enter submit; `data-testid` is a browser spec's only handle on a control; an
// `id` is what a `<label for>` points at. Swap `<input name="password">` for
// `<Input>` without the `name` and the page still compiles, still renders, still
// passes axe — and cannot log anyone in. There is no DOM runner in this repo
// (`src/lib/buttonWrap.test.ts` explains why), so the attributes are read out of
// the PARSED JSX (`jsxSource.ts`, the same scanner the CSS guards use) rather
// than out of the built page.
//
// THE TWO RULES, per page in the cluster:
//
//   (a) THE PRIMITIVES ARE THERE — the page renders the layer's components,
//       not a private copy of them. Without this half, deleting the migration
//       (back to a hand-made card and button) would leave every attribute rule
//       below green, because the attributes live on the controls either way.
//
//   (b) THE BEHAVIOUR SURVIVED — every name / id / type / testid / endpoint the
//       page depends on is still present, on the expected element.
//
// Both halves run over the same parsed tag list, and both have a CONTROL test
// that plants the defect (a stripped `name`, a hand-painted primary fill, a
// removed `Card`) and requires the finding. A guard that cannot see the defect
// it was written for is the failure mode these controls exist to prevent.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";

import { codeOnly, jsxTags, readSource, type Event, type Tag } from "./jsxSource.ts";

const SRC = new URL("../", import.meta.url); // web/src/
const at = (rel: string) => fileURLToPath(new URL(rel, SRC));

/** The seven pages of the cluster whose markup changed, and the one that did not
 *  (a redirect shell: it has no surface, no control and no copy to swap). */
const PAGES = [
  { rel: "app/login/page.tsx", primitives: ["Button", "Card", "Checkbox", "CodeInput", "Field", "Input"] },
  { rel: "app/magic-link/page.tsx", primitives: ["Button", "Card"] },
  { rel: "app/reset-password/page.tsx", primitives: ["Button", "Card", "Field", "Input"] },
  { rel: "app/connect/page.tsx", primitives: ["Button", "Card"] },
  { rel: "app/workspace-picker/page.tsx", primitives: ["Button", "Card", "Field", "Input"] },
  { rel: "app/legal/privacy/page.tsx", primitives: ["Container", "Section"] },
  { rel: "app/legal/terms/page.tsx", primitives: ["Container", "Section"] },
] as const;

/** The redirect shell, pinned so the next reader knows it was looked at. */
const REDIRECT_SHELL = "app/post-login/page.tsx";

type Scanned = { code: string; tags: Tag[] };

/** Comments blanked (a class quoted in prose cannot satisfy a rule), tags paired. */
function scan(src: string): Scanned {
  const code = codeOnly(src);
  const byOffset = new Map<number, Tag>();
  for (const e of jsxTags(code) as Event[]) {
    if (!byOffset.has(e.tag.openIdx)) byOffset.set(e.tag.openIdx, e.tag);
  }
  return { code, tags: [...byOffset.values()].sort((a, b) => a.openIdx - b.openIdx) };
}

/** The element's own attribute text (`name="email" type="email" …`). */
const attrs = (s: Scanned, tag: Tag): string => s.code.slice(tag.attrStart, tag.attrEnd);

/** The elements with this tag name, in source order. */
const named = (s: Scanned, name: string): Tag[] => s.tags.filter((t) => t.name === name);

const lineOf = (s: Scanned, tag: Tag): number => s.code.slice(0, tag.openIdx).split("\n").length;

/** Rule (a): the page renders every primitive the migration says it uses. */
function missingPrimitives(src: string, want: readonly string[]): string[] {
  const s = scan(src);
  return want.filter((name) => named(s, name).length === 0);
}

/** The primary FILL, exactly — `bg-[var(--jf-primary)]/20` is a tint, not a fill. */
const PRIMARY_FILL = /(^|\s)bg-\[var\(--jf-primary\)\](\s|$)/;

/**
 * Rule (a) continued, and the design-system rule itself: a page may not paint
 * the primary fill with its own class list. The fill is `Button`'s decision (a
 * variant) or it is not painted. Only lowercase `<button>` / `<a>` are judged:
 * `Button` carries the fill through `variant`, and the one `<Link>` in the
 * cluster is the brand mark, not a call to action.
 */
function handPaintedFills(src: string): string[] {
  const s = scan(src);
  const out: string[] = [];
  for (const tag of s.tags) {
    if (tag.name !== "button" && tag.name !== "a") continue;
    for (const variant of tag.variants) {
      if (PRIMARY_FILL.test(variant)) {
        out.push(`${lineOf(s, tag)}: <${tag.name}> paints bg-[var(--jf-primary)] itself`);
      }
    }
  }
  return out;
}

/** Rule (b): an attribute (or a pair) must sit on the expected element. */
function missingAttr(src: string, element: string, needle: string): boolean {
  const s = scan(src);
  return !named(s, element).some((tag) => attrs(s, tag).includes(needle));
}

function countAttr(src: string, element: string, needle: string): number {
  const s = scan(src);
  return named(s, element).filter((tag) => attrs(s, tag).includes(needle)).length;
}

/** The behaviour contract, per page. `element` is the JSX tag the attribute must
 *  be ON — that is the half a "does the string appear anywhere" check misses. */
const CONTRACT: Record<string, Array<{ element: string; attr: string; why: string }>> = {
  "app/login/page.tsx": [
    { element: "Input", attr: 'name="email"', why: "the submit reads FormData.get(\"email\")" },
    { element: "Input", attr: 'name="password"', why: "the submit reads FormData.get(\"password\")" },
    { element: "CodeInput", attr: 'name="totp_code"', why: "the 2FA form posts the code by field name" },
    { element: "input", attr: 'name="website"', why: "the honeypot field the server checks (issue #42)" },
    { element: "input", attr: 'id="website"', why: "the honeypot's own label points at this id" },
    { element: "Button", attr: 'data-testid="passkey-login"', why: "the passkey control's browser handle" },
    { element: "Field", attr: 'id="login-email"', why: "the email label/control pairing (axe `label`)" },
    { element: "Field", attr: 'id="login-password"', why: "the password label/control pairing" },
  ],
  "app/magic-link/page.tsx": [
    { element: "Button", attr: 'href="/login"', why: "the failure state's only way forward" },
  ],
  "app/reset-password/page.tsx": [
    { element: "Field", attr: 'id="reset-new-pw"', why: "the new-password field's label pairing" },
    { element: "Field", attr: 'id="reset-confirm-pw"', why: "the confirm field's label pairing" },
    { element: "Button", attr: 'type="submit"', why: "the form has no other submit control" },
  ],
  "app/connect/page.tsx": [],
  "app/workspace-picker/page.tsx": [
    { element: "Field", attr: 'id="ask-ws-name"', why: "the create form's name field pairing" },
    { element: "Field", attr: 'id="ask-ws-slug"', why: "the create form's slug field pairing" },
    { element: "Button", attr: 'type="submit"', why: "the create form has no other submit control" },
  ],
  "app/legal/privacy/page.tsx": [],
  "app/legal/terms/page.tsx": [],
};

/** Endpoints a page must still call — the control that survived the swap. */
const ENDPOINTS: Record<string, string[]> = {
  "app/login/page.tsx": [
    '"/api/auth/login"',
    '"/api/auth/register"',
    '"/api/auth/magic-link/request"',
    '"/api/auth/forgot-password"',
    '"/api/auth/2fa/email-code"',
    '"/api/auth/passkeys/login/start"',
    '"/api/auth/passkeys/login/finish"',
  ],
  "app/magic-link/page.tsx": ['"/api/auth/magic-link/verify"'],
  "app/reset-password/page.tsx": ['"/api/auth/reset-password"'],
  "app/connect/page.tsx": ['"/api/auth/social/connect"', '"/api/auth/social/connect/resend"'],
  "app/workspace-picker/page.tsx": ['"/api/workspaces"', '"/api/auth/me"', '"/api/auth/profile/default-workspace"'],
};

test("every page in the cluster renders the primitives, not a private copy", () => {
  // Not vacuous: the file each row names really exists and really parses.
  for (const page of PAGES) {
    const src = readSource(at(page.rel));
    const s = scan(src);
    assert.ok(s.tags.length > 10, `${page.rel}: parsed only ${s.tags.length} tags — the scanner is not reading it`);
    assert.deepEqual(missingPrimitives(src, page.primitives), [], `${page.rel} no longer renders:`);
  }
});

test("no page in the cluster paints the primary fill by hand", () => {
  for (const page of PAGES) {
    assert.deepEqual(handPaintedFills(readSource(at(page.rel))), [], `${page.rel}:`);
  }
});

test("every behaviour-bearing attribute is still on the element that needs it", () => {
  for (const [rel, rows] of Object.entries(CONTRACT)) {
    const src = readSource(at(rel));
    for (const row of rows) {
      assert.ok(
        !missingAttr(src, row.element, row.attr),
        `${rel}: no <${row.element}> carries ${row.attr} — ${row.why}`,
      );
    }
  }
});

test("login keeps ONE submit control per form view and the 2FA step's own", () => {
  const src = readSource(at("app/login/page.tsx"));
  // Two forms share the component: the credentials form and the 2FA form. Both
  // submit by Enter, which only works on a `type="submit"` control.
  assert.equal(countAttr(src, "Button", 'type="submit"'), 2, "login lost a form's submit button (Enter no longer submits)");
});

test("every endpoint the cluster calls is still called", () => {
  for (const [rel, needles] of Object.entries(ENDPOINTS)) {
    const src = readSource(at(rel));
    for (const needle of needles) {
      assert.ok(src.includes(needle), `${rel}: ${needle} is no longer fetched — a control was dropped in the swap`);
    }
  }
});

test("the hand-made surface is gone from the pages that had one", () => {
  // The exact class string the five auth cards shared. A page that still has it
  // did not migrate; `Card` owns the fill, the edge and the elevation.
  const HAND_MADE = "rounded-xl border border-[var(--jf-border)] bg-white p-6";
  for (const page of PAGES) {
    assert.ok(
      !readSource(at(page.rel)).includes(HAND_MADE),
      `${page.rel} still paints its own card surface`,
    );
  }
});

test("the redirect shell was looked at, and has nothing to swap", () => {
  const src = readSource(at(REDIRECT_SHELL));
  const s = scan(src);
  // It renders one text node ("…") and no surface/control: a `Card` or a
  // `Button` here would be invention, not migration.
  assert.deepEqual(
    s.tags.filter((t) => t.name === "Card" || t.name === "Button").map((t) => t.name),
    [],
    `${REDIRECT_SHELL} gained a primitive — re-decide whether the shell is a real page`,
  );
});

// ─── controls: the guards must see the defects they were written for ─────────

test("control — a stripped `name` is reported by the attribute rule", () => {
  const src = readSource(at("app/login/page.tsx"));
  assert.ok(!missingAttr(src, "Input", 'name="password"'), "the unmodified page must satisfy the rule");
  const doctored = src.replace('name="password"', '/* name removed */');
  assert.ok(missingAttr(doctored, "Input", 'name="password"'), "a password field with no `name` was NOT reported");
});

test("control — a hand-painted primary fill is reported", () => {
  const clean = 'export function P() {\n  return (\n    <button type="button">Go</button>\n  );\n}\n';
  assert.deepEqual(handPaintedFills(clean), [], "a plain button must not be reported");
  const planted = 'export function P() {\n  return (\n    <button className="w-full rounded-md bg-[var(--jf-primary)] py-2">Go</button>\n  );\n}\n';
  assert.equal(handPaintedFills(planted).length, 1, "a button painting the primary fill was NOT reported");
  // …and a TINT is not a fill: the tab strip in the cluster uses `/20`.
  const tinted = 'export function P() {\n  return (\n    <button className="bg-[var(--jf-primary)]/20">Log in</button>\n  );\n}\n';
  assert.deepEqual(handPaintedFills(tinted), [], "a 20% tint must not be reported as a fill");
});

test("control — a removed primitive is reported", () => {
  const src = readSource(at("app/magic-link/page.tsx"));
  assert.deepEqual(missingPrimitives(src, ["Card"]), [], "the unmodified page renders Card");
  const doctored = src.replace("<Card className=\"p-jf-6\">", "<div>").replace("</Card>", "</div>");
  assert.deepEqual(missingPrimitives(doctored, ["Card"]), ["Card"], "a page with no Card was NOT reported");
});
