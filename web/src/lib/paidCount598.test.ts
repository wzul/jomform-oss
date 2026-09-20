// #598 regression guard: the dashboard's "Paid+Confirmed" figure.
//
// WHAT THE FIGURE IS: `paid_count` is ALREADY the union —
// COUNT(s.id) FILTER (WHERE s.status IN ('paid','confirmed')) — and
// `confirmed_count` is a SUBSET of it. The union is therefore the figure, and
// `paid_count + confirmed_count` is the #598 double-count: a form with one
// confirmed sale reported "Paid+Confirmed: 2".
//
// WHY THESE INPUTS: the union and the sum AGREE whenever nothing is confirmed
// (0 + 0, 5 + 0), so an assertion on such a form passes against the union and
// against the double-count alike, and guards neither. Every case below except
// the last has a confirmed sale, which is the only place the two disagree —
// and it is the shape the defect was reported on. A form with no confirmed
// sales is asserted too, so the input space is explicit rather than implied.
//
// Run with: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";

import { paidConfirmedCount } from "./dashboardStats.ts";

test("#598: a confirmed sale is counted once, not twice", () => {
  // The reported shape: one sale, status 'confirmed'. paid_count is the union
  // and already includes it, so the figure is 1. The double-count said 2.
  assert.equal(
    paidConfirmedCount({ paid_count: 1, confirmed_count: 1 }),
    1,
    "the union already includes the confirmed sale",
  );
  // The wider shape: 3 sales in the paid set, 2 of them confirmed. The union is
  // 3; the double-count said 5.
  assert.equal(paidConfirmedCount({ paid_count: 3, confirmed_count: 2 }), 3);
  // Everything in the paid set is confirmed: the union still does not move.
  assert.equal(paidConfirmedCount({ paid_count: 4, confirmed_count: 4 }), 4);
  // Nothing confirmed — the union and the sum agree here, so this case does not
  // bite on its own; it pins the degenerate end of the input space.
  assert.equal(paidConfirmedCount({ paid_count: 5, confirmed_count: 0 }), 5);
  // Absent/empty payloads still render 0 rather than NaN or undefined.
  assert.equal(paidConfirmedCount(null), 0);
  assert.equal(paidConfirmedCount(undefined), 0);
  assert.equal(paidConfirmedCount({}), 0);
});
