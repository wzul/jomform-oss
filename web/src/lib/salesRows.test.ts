// Issue #604 — the Sales page must render a response-mode form's entries.
//
// THE BUG (one line): a response-mode form's entry is persisted to the
// `submissions` table (issue #171), but `/sales?form=<id>` reads only the `sales`
// table, so the page showed `total: 0` — "nothing was recorded" — while the
// submission existed. The API now returns those entries in the sales envelope.
//
// WHAT THIS GUARD COVERS
//   1. The wire contract: the JSON keys the page reads are pinned against the Go
//      struct that EMITS them (api/internal/httpapi/rest_handlers.go), the same
//      cross-language pattern src/lib/adminGrowth.test.ts uses. A rename on the
//      Go side therefore fails here instead of silently rendering "undefined".
//   2. rowKind: a response row is recognised by the marker the API sets, with the
//      submission id as a second signal — so a sale is never misread as a
//      response (which would hide its amount and its capture/refund actions).
//   3. isResponseForm: the page picks the answers view from the form's `mode`.
//      An unknown form must read as a payment form — the pre-fix behaviour.
//   4. answersPreview / answerText: the recorded answers have to be readable, and
//      a non-string answer must not render as "[object Object]".
//
// WHAT IT DOES NOT COVER (stated honestly): the rendered JSX. This repo has no
// JSX test runner (see src/lib/i18n.test.ts), so these are the decisions the page
// makes, extracted into src/lib/salesRows.ts — the page calls these functions
// rather than restating them. The pixels are checked by the browser measurements
// in the PR description.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  answerKeys,
  answerText,
  answersPreview,
  isResponseForm,
  rowKind,
  type RowLike,
} from "./salesRows.ts";

// The Go source that emits the sales-row JSON. Read at module load, exactly like
// adminGrowth.test.ts reads its Go struct.
const goHandlers = readFileSync(
  fileURLToPath(new URL("../../../api/internal/httpapi/rest_handlers.go", import.meta.url)),
  "utf8",
);

// The Go submission row struct (the wire shape of a response entry).
const submissionRowStruct = (): string => {
  const m = goHandlers.match(/type restSubmissionRow struct \{([\s\S]*?)\n\}/);
  assert.ok(
    m,
    "could not find `type restSubmissionRow struct` in api/internal/httpapi/rest_handlers.go — " +
      "the page reads this shape, so the guard cannot pass vacuously",
  );
  return m[1];
};

// A row exactly as the API emits it, built from the Go struct's OWN json tags so
// a rename on either side shows up as a mismatch here.
const sampleResponseRow = () => ({
  ref_code: "it-marker",
  status: "response",
  sale_type: "response",
  created_at: "2026-09-18T14:01:19Z",
  form_id: "86037683-e9d7-40a6-b36b-ed94805dd957",
  buyer: "Ahmad pintu",
  buyer_email: "buyer00@example.com",
  buyer_phone: "+60123456789",
  submission_id: "bd923c63-ecbc-49f4-875e-0b06813d5633",
  data: {
    b3: "xda",
    full_name: "Ahmad pintu",
    email: "buyer00@example.com",
    phone: "+60123456789",
    wzl2cht: "2026-09-20",
  },
  total_sen: 0,
  refunded_sen: 0,
  outstanding_sen: 0,
  refund_status: "",
});
test("the Go row struct was located (the key checks cannot pass vacuously)", () => {
  const src = submissionRowStruct();
  assert.ok(src.includes("SubmissionID"), "restSubmissionRow lost its SubmissionID field");
  assert.ok(src.includes("Data"), "restSubmissionRow lost its Data field");
});

test("every JSON key the page reads is a key the Go struct emits", () => {
  const src = submissionRowStruct();
  // These are the keys rowKind / answersPreview / the page's columns depend on.
  const required = [
    "sale_type",
    "submission_id",
    "data",
    "form_id",
    "buyer",
    "buyer_email",
    "buyer_phone",
    "created_at",
    "total_sen",
  ];
  for (const key of required) {
    assert.ok(
      src.includes(`json:"${key}`),
      `restSubmissionRow does not emit "${key}", but the Sales page reads it — ` +
        "the response row would render as undefined",
    );
  }
});

test("the page's row shape reads the marker the Go struct actually sets", () => {
  // The marker VALUE (not just the tag): submissionsAsRows marks a response row
  // with sale_type "response", and rowKind keys off exactly that.
  assert.ok(
    /SaleType:\s*"response"/.test(goHandlers),
    "submissionsAsRows no longer sets SaleType to \"response\" — rowKind would read " +
      "every response as a sale, hiding its actions and showing RM0.00",
  );
});

test("a response row is recognised (and marked as such, not as a sale)", () => {
  const row = sampleResponseRow();
  assert.equal(rowKind(row), "response");
});

test("the sale_type marker ALONE identifies a response (not only the id fallback)", () => {
  // The two signals must be checked INDEPENDENTLY. A fixture carrying both lets
  // the id fallback resolve the row, so a broken marker check stays green — the
  // exact trap this assertion exists to close.
  assert.equal(
    rowKind({ sale_type: "response" }),
    "response",
    "the sale_type marker alone was not honoured — the id fallback masked it",
  );
  // ...and the reverse: the id alone, with sale_type absent.
  assert.equal(rowKind({ submission_id: "bd923c63" }), "response");
});

test("a sale row is NEVER read as a response (its money and actions must survive)", () => {
  // A sale has no sale_type "response" and no submission id. The sale fixtures
  // carry the page's own Sale fields on top of RowLike, so the assertion is that
  // the classification ignores them rather than that they are impossible.
  type SaleLike = RowLike & { ref_code?: string; status?: string; total_sen?: number };
  const sale: SaleLike = { ref_code: "JF-abc123", status: "paid", total_sen: 2500, sale_type: "" };
  assert.equal(rowKind(sale), "sale");
  // The exact shape a one-time sale serialises to: sale_type omitted entirely.
  const oneTime: SaleLike = { ref_code: "JF-abc123", status: "paid", total_sen: 2500 };
  assert.equal(rowKind(oneTime), "sale");
  // A SUBSCRIPTION sale also carries a sale_type — still not a response.
  assert.equal(rowKind({ ref_code: "JF-x", sale_type: "subscription" } as SaleLike), "sale");
  assert.equal(rowKind(null), "sale");
  assert.equal(rowKind(undefined), "sale");
});

test("submission_id alone is enough (a marker dropped server-side still renders)", () => {
  // Belt-and-braces: if the marker were lost the id still identifies the row, so
  // the merchant sees their entry rather than an empty sale row.
  assert.equal(rowKind({ submission_id: "bd923c63" }), "response");
  // ...but an EMPTY id is not a signal (a sale could serialise one as "").
  assert.equal(rowKind({ ref_code: "JF-x", submission_id: "" } as RowLike & { ref_code: string }), "sale");
});

test("the filter dropdown's mode decides the answers view", () => {
  const forms = [
    { id: "payment-form", mode: "payment" },
    { id: "response-form", mode: "response" },
    { id: "no-mode-form" },
  ];
  assert.equal(isResponseForm(forms, "response-form"), true);
  assert.equal(isResponseForm(forms, "payment-form"), false);
  // No mode (an API build before this field) must behave as before: a payment
  // form. Defaulting the other way would hide the money columns for every form.
  assert.equal(isResponseForm(forms, "no-mode-form"), false);
  // Unknown / empty form id.
  assert.equal(isResponseForm(forms, "nope"), false);
  assert.equal(isResponseForm(forms, ""), false);
  assert.equal(isResponseForm([], "response-form"), false);
});

test("the answers are readable and the buyer identity is not repeated in them", () => {
  const row = sampleResponseRow();
  // full_name/email/phone get their own buyer column, so they are excluded from
  // the answers preview (otherwise every name would show twice).
  assert.deepEqual(answerKeys(row).sort(), ["b3", "wzl2cht"].sort());

  const preview = answersPreview(row);
  assert.ok(preview.includes("xda"), `answers preview = ${JSON.stringify(preview)}, want the b3 answer`);
  assert.ok(preview.includes("2026-09-20"), `answers preview = ${JSON.stringify(preview)}, want the date answer`);
  assert.ok(
    !preview.includes("buyer00@example.com"),
    `answers preview repeats the buyer email: ${JSON.stringify(preview)}`,
  );
});

test("a non-string answer renders as text, never as [object Object]", () => {
  assert.equal(answerText("xda"), "xda");
  assert.equal(answerText(0), "0");
  assert.equal(answerText(425), "425");
  assert.equal(answerText(true), "true");
  assert.equal(answerText(null), "");
  assert.equal(answerText(undefined), "");
  // A repeater/list answer is an array/object — JSON text, not "[object Object]".
  assert.equal(answerText(["a", "b"]), '["a","b"]');
  assert.equal(answerText({ city: "KL" }), '{"city":"KL"}');

  const preview = answersPreview({ data: { list: [{ n: 1 }] } });
  assert.ok(!preview.includes("[object Object]"), `preview = ${JSON.stringify(preview)}`);
  assert.ok(preview.includes('{"n":1}'), `preview = ${JSON.stringify(preview)}`);
});

test("a long response is truncated, not allowed to blow the row apart", () => {
  const preview = answersPreview({ data: { note: "x".repeat(200) } }, 20);
  assert.equal(preview.length, 20);
  assert.ok(preview.endsWith("…"), `preview = ${JSON.stringify(preview)}`);
});

test("a response with no answers renders as an empty preview, not a crash", () => {
  assert.equal(answersPreview({}), "");
  assert.equal(answersPreview({ data: {} }), "");
  assert.equal(answersPreview({ data: null }), "");
  assert.equal(answersPreview(null), "");
});
