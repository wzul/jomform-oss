// Issue #604: how the Sales page renders a row.
//
// A response-mode form (issue #171) has no sales at all — its entries are
// `submissions` rows, so `/sales?form=<that form>` returned `total: 0` no matter
// what the merchant did. The API now returns those entries in the SAME envelope
// as a sale (marked `sale_type: "response"`, carrying `submission_id` + `data`,
// with every money field 0). The page therefore has to know which kind of row it
// is holding: rendering a response through the sale columns shows an empty
// reference, a RM0.00 amount and capture/refund actions that cannot apply.
//
// The decision lives here rather than inline in the page so it can be tested
// (this repo has no JSX test runner — see src/lib/i18n.test.ts), and so the JSON
// keys it reads are pinned against the Go struct that emits them instead of
// being restated by hand (the same pattern as src/lib/adminGrowth.ts).

export type RowKind = "sale" | "response";

// Minimal shape both row kinds satisfy. Deliberately structural: the page's own
// Sale type widens this. `ref_code`/`created_at` are here because rowKey has to
// accept either kind of row (a sale is keyed by its reference, a response by its
// submission id).
export type RowLike = {
  sale_type?: string;
  submission_id?: string;
  ref_code?: string;
  created_at?: string;
  data?: Record<string, unknown> | null;
};

// rowKind classifies one row of the /api/rest/sales envelope. `sale_type` is the
// marker the API sets; `submission_id` is the belt-and-braces check, because a
// response row always carries one and a sale never does.
export function rowKind(row: RowLike | null | undefined): RowKind {
  if (!row) return "sale";
  if (row.sale_type === "response") return "response";
  if (typeof row.submission_id === "string" && row.submission_id !== "") return "response";
  return "sale";
}

// isResponseForm reports whether the form selected in the filter is a
// response-mode form, so the page can label the list as responses. It reads the
// `mode` the API puts on every row of the forms list; an unknown form id (or an
// API build without the field) is a payment form, which preserves the previous
// behaviour exactly.
//
// NOTE: this answers "is the FILTERED form a response form" — a question about
// the forms list, not about the rows. It must NOT decide which row layout to
// render (see listView below): the forms list is a separate request from the
// rows, so keying the row layout on it renders the rows through one branch and
// then flips them to the other.
export function isResponseForm(
  forms: readonly { id: string; mode?: string }[],
  formId: string,
): boolean {
  if (!formId) return false;
  const form = forms.find((f) => f.id === formId);
  return !!form && form.mode === "response";
}

// rowKey returns the React key for one row.
//
// It is derived from the ROW, never from the layout being rendered: a response
// row has no payment reference (`ref_code` is "" for every one of them), so
// keying a response row on `ref_code` gives React a set of IDENTICAL keys. That
// is not hypothetical — it is exactly how 2 stale sales-layout rows survived on
// the responses view: the rows first rendered through the sales branch (all keys
// ""), and when the branch flipped React reconciled children whose previous keys
// were indistinguishable and left the old <tr> nodes in the tbody.
//
// `submission_id` first (unique per response and never present on a sale), then
// the sale's `ref_code`, then a stable last resort. The last resort is prefixed
// so it can never collide with a real reference.
export function rowKey(row: RowLike | null | undefined): string {
  if (!row) return "";
  const sub = typeof row.submission_id === "string" ? row.submission_id : "";
  if (sub !== "") return sub;
  const ref = typeof row.ref_code === "string" ? row.ref_code : "";
  if (ref !== "") return ref;
  return `row:${typeof row.created_at === "string" ? row.created_at : "unknown"}`;
}

// listView decides what the LIST (header + per-row layout) shows, from the rows
// themselves.
//
// The rule: a list shows the answers view exactly when every row it holds is a
// response. That is the same decision the page used to make from `responsesView`
// (which also consulted the filtered form's `mode`), but derived per row — so
// the header, the caption and each row agree with each other ALWAYS, including
// while the forms list is still in flight. No request ordering can make one row
// render through the other branch's layout, which is what left stale nodes.
//
// An empty list is a sale list: the filtered form's mode is used only to pick
// the right EMPTY/loading copy (emptyFor), because no row exists to classify.
export function listView(rows: readonly RowLike[] | null | undefined): RowKind {
  if (!rows || rows.length === 0) return "sale";
  return rows.every((r) => rowKind(r) === "response") ? "response" : "sale";
}

// responsesList decides whether the list CHROME should read as a responses list
// — the header, the subtitle, the empty-state copy and the hidden status filter.
// It prefers the per-row view (listView) and falls back to the selected form's
// mode ONLY while there are no rows to classify (loading / genuinely empty),
// because then there is no row to ask. A mixed list (which the API cannot
// produce today — a form is either a payment form or a response form) is treated
// as a sale list, preserving the money columns and the actions.
export function responsesList(rows: readonly RowLike[], filteredFormIsResponse: boolean): boolean {
  return rows.length > 0 ? listView(rows) === "response" : filteredFormIsResponse;
}

// answerKeys returns the recorded answer keys of a response row, excluding the
// buyer identity the submit handler folds into `data` (public.go
// handleResponseSubmit) — those already have their own buyer column, so
// repeating them inside the answers would show every name twice.
const BUYER_KEYS = new Set(["full_name", "email", "phone"]);

export function answerKeys(row: RowLike | null | undefined): string[] {
  if (!row || !row.data || typeof row.data !== "object") return [];
  return Object.keys(row.data).filter((k) => !BUYER_KEYS.has(k));
}

// answerText renders one recorded answer for display. A non-string value (a
// number, a boolean, a repeater/list value) is shown as its JSON text rather
// than "[object Object]", which is what a bare template interpolation produces.
export function answerText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return "";
  }
}

// answersPreview joins a response row's recorded answers into one line, for the
// page's "items" column, capped so a long form does not blow the row apart.
export function answersPreview(row: RowLike | null | undefined, max = 60): string {
  const parts: string[] = [];
  for (const k of answerKeys(row)) {
    const v = answerText(row?.data?.[k]);
    if (v !== "") parts.push(v);
  }
  const joined = parts.join(" · ");
  return joined.length > max ? joined.slice(0, max - 1) + "…" : joined;
}
