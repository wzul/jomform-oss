// The typed-confirmation rule, and the fact that BOTH of the products page's
// call sites use it (card t_1ee11d1f).
//
// The unit half is the rule itself. The source half is here because the page had
// the same defect TWICE — a guard inside `purgeProduct` (`if (purgeTyped.trim()
// !== confirmPurge.name) return;`) and the destructive button's `disabled` — and
// fixing only one of them is invisible: the button would never enable (or the
// handler would refuse a value the button accepted). Neither is reachable from a
// DOM unit test in this repo (there is no JSX runner — see
// src/lib/buttonWrap.test.ts), so the wiring is pinned at the source while the
// behaviour is pinned in the browser by
// e2e/products-trash-refusal.spec.ts ("a stored name padded with a trailing
// space … must enable the button").
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { matchesTypedValue } from "./typedConfirm.ts";

test("matchesTypedValue trims BOTH sides", () => {
  const cases: Array<[string, string, string, boolean]> = [
    // The defect: a padded STORED value, typed back the way it is stored.
    ["a stored name with a trailing space, typed exactly", "Kopi ", "Kopi ", true],
    ["a stored name with a trailing space, typed as it RENDERS", "Kopi", "Kopi ", true],
    ["a stored name with a leading space", "Kopi", " Kopi", true],
    ["a stored name padded on both sides", " Kopi ", "Kopi ", true],
    ["a stored name padded with a newline and a tab", "Kopi", "\tKopi\n", true],
    // The direction that already worked and must keep working: the caller pads.
    ["the caller pads an unpadded stored name", "  Kopi  ", "Kopi", true],
    ["both sides padded", "  Kopi  ", " Kopi ", true],
    // The boundary: the gate must still refuse a value that is NOT the name.
    ["an exact match", "Kopi", "Kopi", true],
    ["a different value", "Teh", "Kopi", false],
    ["the right value, the wrong case", "kopi", "Kopi", false],
    ["a substring of the stored value", "Kop", "Kopi", false],
    ["internal whitespace is NOT ignored", "Kopi  Ais", "Kopi Ais", false],
    ["an empty field against a real name", "", "Kopi", false],
    ["a blank field against a real name", "   ", "Kopi", false],
  ];
  for (const [label, typed, stored, want] of cases) {
    assert.equal(
      matchesTypedValue(typed, stored),
      want,
      `${label}: matchesTypedValue(${JSON.stringify(typed)}, ${JSON.stringify(stored)}) ` +
        `must be ${want} — this is the dashboard's half of the confirmation gate ` +
        `(the server's is textutil.SameTypedValue)`,
    );
  }
});

const productsPage = readFileSync(
  fileURLToPath(new URL("../app/products/page.tsx", import.meta.url)),
  "utf8",
);

test("the products purge guard and its button use that rule, not a one-sided trim", () => {
  // The pre-fix expression, verbatim. Its presence anywhere on the page is the
  // regression: `trim()` on one side against a raw stored name can never match a
  // padded value, which is what made the button un-enableable.
  assert.equal(
    productsPage.split("purgeTyped.trim() !== confirmPurge.name").length - 1,
    0,
    "products/page.tsx compares a one-sided `purgeTyped.trim()` against the raw stored name " +
      "again — with a padded product name that comparison is unsatisfiable and the permanent-" +
      "delete button can never enable (card t_1ee11d1f)",
  );

  const uses = productsPage.split("!matchesTypedValue(purgeTyped, confirmPurge.name)").length - 1;
  assert.equal(
    uses,
    2,
    "both of the products page's confirmation sites must use matchesTypedValue: the guard in " +
      "purgeProduct AND the destructive button's disabled state. One without the other means " +
      "the button and the handler disagree about which typed values are accepted",
  );
  assert.ok(
    productsPage.includes(
      'import { matchesTypedValue } from "../../lib/typedConfirm";',
    ),
    "products/page.tsx must import the rule from lib/typedConfirm rather than re-inline it, " +
      "so the page and the server cannot drift apart again",
  );
});
