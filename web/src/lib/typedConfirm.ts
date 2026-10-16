// The TYPED-CONFIRMATION rule, in one place for the dashboard (card t_1ee11d1f).
//
// Several destructive actions ask the caller to name what they are destroying: a
// product's NAME in the products bin, a form's or a workspace's SLUG in theirs.
// The page's guard and the server's check have to agree about what "the same
// value" means, and they did not:
//
//   products/page.tsx  `purgeTyped.trim() !== confirmPurge.name`
//   rest_handlers.go   `strings.TrimSpace(in.ConfirmName) != p.Name`   (MCP: same)
//
// Both trimmed only the CALLER's side and compared against the RAW stored value.
// That direction is safe on its own, but the comparison then has NO input that
// can satisfy a stored value carrying padding — `TrimSpace` never returns a
// string with a leading or trailing space — so a product stored as `"Kopi "`
// could not be purged by anyone, the exact stored bytes included, from any
// surface: the server refused every value and the dialog's button could never
// enable. A product name is stored verbatim on every write path, so a paste or a
// name typed with a trailing space is enough to reach that state.
//
// So both sides are trimmed, here and in `api/internal/textutil.SameTypedValue`
// (the Go half of the same rule; the two are deliberately the same expression).
// What the gate still enforces is unchanged: the caller has to name the thing, a
// different value is still a mismatch, and case and internal whitespace are still
// significant.
export function matchesTypedValue(typed: string, stored: string): boolean {
  return typed.trim() === stored.trim();
}
