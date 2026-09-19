// The dashboard's per-form counter label, chosen by the form's mode.
//
// THE BUG (issue #598): the dashboard rendered ONE label, "Submissions:", next
// to the per-form figure for every form. For a response form the figure is a
// count of `submissions` rows, so the label was right; for a payment form the
// same figure is a count of `sales` rows (paid + failed + pending + refunded),
// so the card called a count of orders "Submissions". The NUMBER was correct
// in both cases (a payment form writes nothing to `submissions`, and a response
// form writes nothing to `sales` — the public submit path branches on the mode
// and writes exactly one table), but a single noun cannot describe both.
//
// THE DECISION, stated once here so it cannot drift between the API, the
// dashboard and the docs: the field is "entries recorded for this form" — the
// rows the form actually produced, read from the table it writes to. A response
// form's entries are Submissions; a payment form's entries are Sales entries.
// A payment form is deliberately NOT reported as 0 submissions, because that
// would reintroduce the exact defect #598 reported ("a working counter reading
// zero") on the other mode. The money figures (paid/confirmed/pending/failed/
// refunded/revenue) stay their own separately named fields beside it, so an
// order is never conflated with the entry count.
//
// Mirrors `dbstore.FormStats.Submissions` in the Go API, whose doc comment
// states the same thing.

/** The i18n keys the per-form counter label can take. */
export type FormEntriesCountLabelKey = "dash.submissions" | "dash.entries";

/** Collection mode values, as the API serialises them (restForm.Mode). */
const MODE_RESPONSE = "response";

/**
 * formEntriesCountLabel returns the i18n key for the per-form entries counter,
 * chosen by the form's mode: Submissions for a response form, Entries for a
 * payment form (whose count is its sales rows).
 *
 * An unknown/absent mode reads as a payment form — the same load-bearing
 * default as src/lib/formMode.ts (`dbstore.FormMode`): every form created
 * before response mode existed has no mode at all and is a payment form, and
 * mislabelling those as "Submissions" is the reported bug.
 */
export function formEntriesCountLabel(mode: string | null | undefined): FormEntriesCountLabelKey {
  return mode === MODE_RESPONSE ? "dash.submissions" : "dash.entries";
}

/** The per-form counter fields this module reads. */
export type PaidFigure = {
  /** Sales in the PAID SET — the SQL counts `status IN ('paid','confirmed')`. */
  paid_count?: number;
  /** The subset of the paid set whose status is exactly `confirmed`. */
  confirmed_count?: number;
};

/**
 * paidConfirmedCount returns the figure for the dashboard's "Paid+Confirmed"
 * stat: how many of this form's sales reached the paid set.
 *
 * `paid_count` is ALREADY the union — `COUNT(s.id) FILTER (WHERE s.status IN
 * ('paid','confirmed'))` in api/internal/dbstore/analytics.go — and
 * `confirmed_count` is a SUBSET of it (`status = 'confirmed'`). The dashboard
 * used to render `paid_count + confirmed_count`, so every confirmed sale was
 * counted TWICE: one confirmed sale reported "Paid+Confirmed: 2", and a form
 * with 1 confirmed and 1 failed sale reported 2 while only one sale had ever
 * been paid. The union is the honest number and it is what the label says.
 *
 * The money-side cross-check is `RevenueSen`: it is computed with the SAME
 * `paidSet` predicate, so a sale counted here is exactly a sale counted there.
 */
export function paidConfirmedCount(f: PaidFigure | null | undefined): number {
  return Number(f?.paid_count ?? 0);
}
