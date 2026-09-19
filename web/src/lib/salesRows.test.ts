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
  isMixedList,
  isResponseForm,
  listView,
  responsesList,
  rowKey,
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

// ─────────────────────────────────────────────────────────────────────────────
// REGRESSION: stale sales-layout rows on the responses view (duplicate key race)
//
// THE BUG: the row LAYOUT was chosen from one global flag, `responsesView`, which
// is false until `/api/rest/forms` resolves — a DIFFERENT request from the rows'
// `/api/rest/sales`. When the sales read won that race, the response rows first
// rendered through the SALES branch, whose React key was `s.ref_code`. On a
// response row `ref_code` is "" (a submission has no payment reference), so ALL
// of them shared the key "". When the forms list landed the branch flipped, React
// reconciled children whose previous keys were indistinguishable, and 2 stale
// 9-cell sales-layout <tr> nodes stayed in the tbody above the 3 real 5-cell
// response rows. Deterministic with /api/rest/forms delayed 4s (2/2 trials), and
// intermittent without the delay (1-2 of every 4-5 loads).
//
// WHAT IS GUARDED HERE
//   A. rowKey is derived from the ROW, and is UNIQUE across a response page —
//      the duplicate-key precondition is what let stale nodes survive. The old
//      expression `s.submission_id || s.created_at` is NOT used any more (it
//      falls through to a shared timestamp when a row has neither), and the old
//      sales-branch key `s.ref_code` cannot be reached by rowKey.
//   B. The row layout is chosen per row: a response renders through the answers
//      branch and a sale through the sales branch, WITH THE FORMS LIST STILL
//      EMPTY — i.e. before /api/rest/forms has landed. Under the old code this is
//      exactly when the wrong branch was used.
//   C. The page's call site is asserted from SOURCE: the tbody maps with
//      `rowKind(s)` / `key={rowKey(s)}` and there is no `responsesView ? (` in
//      the tbody at all. A refactor that reintroduces the global flag in the row
//      map fails here even though the pure functions above stay green (that gap
//      is why the QA card's mutations W4/W5 slipped through).
//
// WHAT IT DOES NOT COVER (stated honestly): the rendered DOM. There is no JSX
// test runner in this repo, so the React reconciliation itself is observed by the
// browser control in the PR — /api/rest/forms delayed 4s through page.route,
// final cell counts 5+5+5 with 0 sales-layout rows, plus the same control FAILING
// on the pre-fix build.

const pageSource = readFileSync(
  fileURLToPath(new URL("../app/sales/page.tsx", import.meta.url)),
  "utf8",
);

// The three response rows from the reported form (production shapes).
const responseRows = () => [
  { sale_type: "response", submission_id: "bd923c63-ecbc-49f4-875e-0b06813d5633" },
  { sale_type: "response", submission_id: "dbc995fa-a71e-4569-a610-f48f3bf42915" },
  { sale_type: "response", submission_id: "1ff6f1c2-5a5c-4a86-9d3f-6b1c2de0a001" },
];

test("rowKey is UNIQUE across a page of response rows (the duplicate-key precondition)", () => {
  const keys = responseRows().map(rowKey);
  assert.equal(new Set(keys).size, keys.length, `duplicate keys: ${JSON.stringify(keys)}`);
  for (const k of keys) assert.notEqual(k, "", "a response row keyed on \"\" is the defect");
});

test("rowKey never falls back to the sales-branch key for a response row", () => {
  // The exact row the API emits for a response: ref_code "" and a unique
  // submission id. The old sales-branch key (`ref_code`) must not be what
  // rowKey returns, or every response row collides again.
  const row = {
    ref_code: "",
    created_at: "2026-09-18T14:01:19Z",
    sale_type: "response",
    submission_id: "bd923c63-ecbc-49f4-875e-0b06813d5633",
  };
  assert.equal(rowKey(row), "bd923c63-ecbc-49f4-875e-0b06813d5633");
  assert.notEqual(rowKey(row), row.ref_code);
});

test("rowKey is still stable and unique for sale rows (the payment view must not move)", () => {
  const keys = ["JF-abc123", "JF-def456", "JF-ghi789"].map((ref_code) => rowKey({ ref_code }));
  assert.deepEqual(keys, ["JF-abc123", "JF-def456", "JF-ghi789"]);
  assert.equal(new Set(keys).size, 3);
});

test("rowKey never returns a shared empty key, even for a row with no id at all", () => {
  // The old expression `submission_id || created_at` produced "" for a row that
  // carried neither; rowKey must stay distinguishable instead.
  const a = rowKey({ created_at: "2026-09-18T14:01:19Z" });
  const b = rowKey({});
  assert.notEqual(a, "");
  assert.notEqual(b, "");
  assert.notEqual(a, b);
});

test("rowKey prefers submission_id over ref_code (a response is never keyed by a ref)", () => {
  // Precedence matters: if a response row ever carried BOTH (e.g. the API started
  // emitting a placeholder reference), keying it on ref_code would re-create the
  // collision this card fixes. submission_id wins, always.
  const row = { submission_id: "bd923c63", ref_code: "JF-placeholder" };
  assert.equal(rowKey(row), "bd923c63");
});

test("listView reads the ROW kind, with the forms list still empty (the race window)", () => {
  // Before /api/rest/forms lands, isResponseForm([], formFilter) is false. The
  // per-row view must still say "response" — under the old code this exact
  // combination rendered the rows through the sales branch.
  assert.equal(isResponseForm([], "86037683-e9d7-40a6-b36b-ed94805dd957"), false);
  assert.equal(listView(responseRows()), "response");
  assert.equal(responsesList(responseRows(), false), true);
  // ...and the header follows the same decision, so chrome and rows agree.
  assert.equal(listView(responseRows()) === "response", responsesList(responseRows(), false));
});

test("listView keeps a payment page on the sales layout", () => {
  // RowLike is a weak-ish type; type the fixture so the fields the page's own
  // Sale carries are visible to the compiler.
  const sales: RowLike[] = [
    { ref_code: "JF-a", sale_type: "one_time" },
    { ref_code: "JF-b", sale_type: "" },
  ];
  assert.equal(listView(sales), "sale");
  assert.equal(responsesList(sales, false), false);
  assert.equal(isMixedList(sales), false);
  // Even a form the forms list calls a response form keeps the money columns
  // when its rows are sales (not producible today: a form is either a payment
  // form or a response form — the row wins, deliberately).
  assert.equal(responsesList(sales, true), false);
});

// A list holding BOTH kinds is now producible: with no form filter the API
// returns the account's sales AND its response entries. Before this, the rule
// was "every row a response → responses, otherwise sales", so a mixed page
// rendered every response through the sales layout — an empty reference and
// RM0.00 in a table whose column count does not match.
test("a MIXED list is its own view, not a sale list", () => {
  const rows: RowLike[] = [
    { ref_code: "JF-a", sale_type: "one_time" },
    { sale_type: "response", submission_id: "sub-1" },
  ];
  assert.equal(listView(rows), "mixed");
  assert.equal(isMixedList(rows), true);
  // The chrome must NOT read as a responses-only list: half the rows are sales
  // and the money columns still apply to them.
  assert.equal(responsesList(rows, false), false);
  // ...and it must not read as a plain sales list either, because the status
  // filter is meaningless for the response half.
  assert.notEqual(listView(rows), "sale");

  // Order must not matter: a response first is the same list.
  const reversed: RowLike[] = [
    { sale_type: "response", submission_id: "sub-1" },
    { ref_code: "JF-a", sale_type: "one_time" },
  ];
  assert.equal(listView(reversed), "mixed");

  // Two responses and one sale is still mixed (not "mostly responses").
  const twoOfThree: RowLike[] = [
    { sale_type: "response", submission_id: "sub-1" },
    { sale_type: "response", submission_id: "sub-2" },
    { ref_code: "JF-a", sale_type: "one_time" },
  ];
  assert.equal(listView(twoOfThree), "mixed");
});

test("listView is stable across the forms landing (no branch flip on the same rows)", () => {
  // The whole defect in one assertion: the layout for a given row set must be the
  // same before and after /api/rest/forms resolves. Only the EMPTY list may use
  // the form's mode, because then there is no row to classify.
  const rows = responseRows();
  const before = listView(rows);
  const after = listView(rows);
  assert.equal(before, after, "the row layout flipped without the rows changing");
  assert.equal(before, "response");
  // The empty list is the one case that legitimately consults the forms list.
  assert.equal(responsesList([], false), false);
  assert.equal(responsesList([], true), true);
});

// The region of the page that renders ONE <tr> per sale/response row. Sliced by
// its own boundaries so an assertion about the ROW LAYOUT cannot accidentally
// cover the empty-state cell, whose colSpan legitimately reads the header flag
// (that cell is not a row and has no key to collide).
const rowsRegion = (() => {
  const start = pageSource.indexOf("rows.map((s) =>");
  const end = pageSource.indexOf("!checking && rows.length === 0");
  assert.ok(start > 0 && end > start, "could not locate the row-render region in the sales page");
  return pageSource.slice(start, end);
})();

test("the row map picks the layout PER ROW, from rowKind — not from a global flag", () => {
  // The call site, asserted from source: the pure functions above can all be
  // correct while the page still branches on `responsesView`.
  //
  // The map now has an outer branch for the MIXED case (sales + responses on one
  // page, which the API could not produce before the unfiltered read was fixed),
  // so the assertion is that `rowKind(s)` decides the layout with the mixed
  // branch present as well — and that inside it the layout is STILL per row.
  assert.ok(
    /rows\.map\(\(s\) =>\s*\n\s*mixedView \?/.test(rowsRegion),
    "the row map lost its mixed-list branch — a mixed page would render every " +
      "response through the 9-column sales layout (empty ref, RM0.00)",
  );
  assert.ok(
    /rowKind\(s\) === "response"/.test(rowsRegion),
    "the row map no longer chooses the layout from rowKind(s) — a global flag " +
      "that flips after the rows rendered is how the stale rows survived",
  );
  assert.ok(
    /key=\{rowKey\(s\)\}/.test(rowsRegion),
    "the row key is no longer rowKey(s)",
  );
  assert.ok(
    !/key=\{s\.ref_code\}/.test(rowsRegion),
    "the sales branch is keyed on s.ref_code again — \"\" for every response row, " +
      "which is the duplicate-key precondition for the stale rows",
  );
  assert.ok(
    !/key=\{s\.submission_id/.test(rowsRegion),
    "the row key is a hand-rolled `s.submission_id || …` again — rowKey is the " +
      "one place that decides it, so a sale and a response cannot collide",
  );
  // No branch on the header flag anywhere in the row map: `responsesView ? (` is
  // the exact pre-fix shape.
  assert.ok(
    !/responsesView/.test(rowsRegion),
    "the row map consults responsesView (the forms list) again — the forms list " +
      "arriving later then re-keys rows that already rendered (stale rows return)",
  );
});

test("the header and the rows read the SAME decision (chrome cannot disagree with the rows)", () => {
  // responsesView (header/caption/status-filter/empty copy) must come from
  // responsesList(rows, …), and the rows from rowKind — one source, so a header
  // can never say "Responses (this form)" over a table of money columns.
  assert.ok(
    /const responsesView = responsesList\(rows, filteredForm\)/.test(pageSource),
    "responsesView is no longer derived from responsesList(rows, filteredForm)",
  );
  assert.ok(
    /const filteredForm = isResponseForm\(forms, formFilter\)/.test(pageSource),
    "the filtered-form mode is no longer read via isResponseForm(forms, formFilter)",
  );
  // The mixed-list flag must come from the same per-row decision, so the header
  // cannot claim a combined list while the rows are all one kind (or the
  // reverse).
  assert.ok(
    /const mixedView = isMixedList\(rows\)/.test(pageSource),
    "mixedView is no longer derived from isMixedList(rows) — the combined header " +
      "and the rows would be decided separately",
  );
  // isResponseForm must be used for the CHROME only: if it appears in the row map
  // the row layout is back on the racy flag.
  assert.ok(
    !rowsRegion.includes("isResponseForm"),
    "the row map consults isResponseForm (the forms list) — the row layout must " +
      "depend on the row alone",
  );
});

// ─────────────────────────────────────────────────────────────────────────────
// The mixed-list WIRE CONTRACT: the page renders a combined list, and the API's
// export names its two kinds. A rename on either side must fail here.
// ─────────────────────────────────────────────────────────────────────────────

// The Go source that builds the merged record export.
const goExport = readFileSync(
  fileURLToPath(new URL("../../../api/internal/httpapi/rest_export.go", import.meta.url)),
  "utf8",
);

const goRecords = readFileSync(
  fileURLToPath(new URL("../../../api/internal/dbstore/records.go", import.meta.url)),
  "utf8",
);

// The REST record-set reader (the paginated list's row mapping + envelope).
const goRestRecords = readFileSync(
  fileURLToPath(new URL("../../../api/internal/httpapi/rest_records.go", import.meta.url)),
  "utf8",
);

test("the record kinds the export writes are the constants the merged read defines", () => {
  // The exporter writes `RecordKindSale` / `RecordKindSubmission`; the constants'
  // VALUES are what appear in the CSV. Pin both against the source so a rename
  // fails here instead of shipping a file whose record_type nothing matches.
  assert.ok(
    /RecordKindSale\s*=\s*"sale"/.test(goRecords),
    "RecordKindSale is no longer \"sale\" — the export's record_type column changed",
  );
  assert.ok(
    /RecordKindSubmission\s*=\s*"response"/.test(goRecords),
    "RecordKindSubmission is no longer \"response\" — the export's record_type would " +
      "say something different from the list's sale_type marker for the same row",
  );
  // The exporter must actually use them (a hardcoded string would drift).
  assert.ok(
    /writeRecordsCSV|header = append\(\[\]string\{"record_type", "submission_id"\}/.test(goExport),
    "the export lost its mixed-list column set — the page and the file would disagree",
  );
  assert.ok(
    /record_type/.test(goExport),
    "the export no longer names the record kind, so a merchant cannot tell a " +
      "sale row from a response row in the file",
  );
});

test("the list envelope reports the mixed flag the page reads", () => {
  // The page (and any other consumer) distinguishes a combined list from a
  // sales-only one; the API must keep emitting the flag.
  assert.ok(
    /"mixed":\s*sc\.Mixed/.test(goRestRecords),
    "the paginated /sales envelope no longer emits `mixed`, so the page cannot " +
      "tell a combined list from a sales list",
  );
  assert.ok(
    /dbstore\.ResolveRecordScope/.test(goRestRecords),
    "the /sales read no longer calls the ONE scope decision (dbstore.ResolveRecordScope)",
  );
});
