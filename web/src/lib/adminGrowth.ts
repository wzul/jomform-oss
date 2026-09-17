// Growth-card presentation for the super-admin /admin overview.
//
// The two growth cards ("New in 24h" / "New in 7d") render five numbers taken
// from the API's `growth_24h` / `growth_7d` objects. They used to render as a
// bare slash-joined string (`0 / 0 / 0 / 0 / 0`) with nothing naming the five
// values, so a reader — Wan, from the mobile admin page — could not tell
// workspaces from sales. Option A fixes that by keeping one card per window and
// naming the numbers on the StatCard's small muted `sub` line, in the same
// order as the value above it.
//
// ORDER IS THE WHOLE POINT, so it lives here once:
// api/internal/dbstore/admin_overview.go defines `GrowthCounts` as
// workspaces, forms, products, sales, users. The legend and the value line are
// both derived from GROWTH_LEGEND, so they cannot drift apart, and
// src/lib/adminGrowth.test.ts pins the order against that source.

export type GrowthCounts = {
  workspaces: number;
  forms: number;
  products: number;
  sales: number;
  users: number;
};

// The existing dictionary keys for these five entities (issue #387). Reused
// rather than duplicated for the legend — they already read correctly as
// labels in all five locales.
export type GrowthLegendKey =
  | "admin.workspaces"
  | "admin.forms"
  | "admin.products"
  | "admin.sales"
  | "admin.users";

// Field order MUST match `GrowthCounts` (and therefore the JSON order of
// growth_24h / growth_7d). Do not reorder without reordering the Go struct.
export const GROWTH_LEGEND: readonly { field: keyof GrowthCounts; key: GrowthLegendKey }[] = [
  { field: "workspaces", key: "admin.workspaces" },
  { field: "forms", key: "admin.forms" },
  { field: "products", key: "admin.products" },
  { field: "sales", key: "admin.sales" },
  { field: "users", key: "admin.users" },
];

export type LegendTranslator = (key: GrowthLegendKey) => string;

// The value line: the five counts, in GROWTH_LEGEND order.
export function growthValue(g: GrowthCounts): string {
  return GROWTH_LEGEND.map((e) => g[e.field]).join(" / ");
}

// The legend: one translated label per number, same order and same separator as
// the value line, so each number sits directly above its label and the pair can
// be read across on a narrow (mobile) card.
export function growthLegend(t: LegendTranslator, sep = " / "): string {
  return GROWTH_LEGEND.map((e) => t(e.key)).join(sep);
}
