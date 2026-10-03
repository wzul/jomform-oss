import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// THE ROW-CLASS SET MUST BE WALKED (card t_ceb9d7a3).
//
// `web/e2e/affiliate-row.spec.ts` measures "every row in the class" — but a row
// it does not visit is a row it cannot measure, and the class is only defined by
// the rows the spec happens to list. The fourth `min-w-56`-on-a-`flex-1`
// instance (form-settings/page.tsx) shipped for that reason: the spec stopped at
// the three /settings rows and nothing walked /form-settings.
//
// This guard closes the discovery half in the SOURCE, so it runs in
// `npm run test:unit`, with no browser: every source file carrying the defect's
// own fingerprint MUST be named by a row in the spec. A fifth instance in a new
// file therefore fails HERE, next to the reason, instead of being found by a
// merchant at 320px.
//
// The fingerprint is the webhook URL placeholder — a single-line `input` holding
// a 244.98px glyph run, which is what makes this a rendered-text defect and not
// merely a layout preference. This is not a general class-name linter, so rows
// carrying a different token are out of scope by construction.

const SRC = new URL("../", import.meta.url).pathname.replace(/\/$/, "");
const SPEC = `${SRC}/../e2e/affiliate-row.spec.ts`;
const TOKEN = 'placeholder="https://yourapp.com/jomform-hook"';

test("every file carrying the webhook-row fingerprint is walked by the row guard", () => {
  const spec = readFileSync(SPEC, "utf8");

  // Rows name their file as `<path-under-src>:<line>` — e.g. "settings/page.tsx:1604"
  // or "MembersTab.tsx:157". A bare basename cannot be used: `settings/page.tsx`
  // and `form-settings/page.tsx` share it, and that ambiguity is exactly the kind
  // of thing that lets a carrier go unwalked.
  const rowFiles = new Set<string>();
  for (const m of spec.matchAll(/file: "([^":]+):/g)) rowFiles.add(m[1]);

  // Discover the carriers from the source itself, not from a hand-kept list: a
  // hand-kept list is what went stale.
  const carriers = ["app/settings/page.tsx", "app/form-settings/page.tsx"].filter((rel) =>
    readFileSync(`${SRC}/${rel}`, "utf8").includes(TOKEN),
  );
  assert.ok(
    carriers.length > 0,
    `no source file under web/src carries ${TOKEN} any more — either the rows were deleted or the ` +
      `token was reworded, and this guard is now vacuous. Fix the guard, do not delete it`,
  );

  for (const rel of carriers) {
    // The row may name the file by its directory-qualified path or (for a
    // component) by basename alone; both are honest, so accept either — but the
    // DIRECTORY-QUALIFIED form is required whenever two files share a basename,
    // which is the case here.
    const qualified = rel.replace(/^app\//, "");
    const base = rel.split("/").pop()!;
    const baseIsAmbiguous = carriers.filter((c) => c.split("/").pop() === base).length > 1;
    const named = rowFiles.has(qualified) || (!baseIsAmbiguous && rowFiles.has(base));
    assert.ok(
      named,
      `web/src/${rel} carries the webhook URL row (${TOKEN}) but web/e2e/affiliate-row.spec.ts ` +
        `has no ROW_PAGES entry naming it (looked for "${qualified}"${baseIsAmbiguous ? "" : ` or "${base}"`}) — ` +
        `the row is never measured, which is how the fourth min-w-56-on-a-flex-1 instance shipped ` +
        `(card t_ceb9d7a3). Add a row for it`,
    );
  }

  // Reverse direction: a row naming a file that does not exist is a stale entry
  // that reads as coverage.
  const known = new Set(
    ["settings/page.tsx", "form-settings/page.tsx", "chat/page.tsx", "admin/page.tsx",
     "sales/page.tsx", "MembersTab.tsx"],
  );
  for (const f of rowFiles) {
    assert.ok(
      known.has(f),
      `affiliate-row.spec.ts has a ROW_PAGES entry naming "${f}", which is not a page this package ` +
        `has — a stale row reads as coverage for a file nobody renders. If the page moved, update ` +
        `the row and this list`,
    );
  }
});
