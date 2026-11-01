// Money formatting: one rule for every surface that labels an amount.
//
// Used by the /sales page and the dashboard. Both printed a literal "RM" and
// ignored the currency each sale was recorded in (issue #185), so a USD sale
// read "RM24.90" on the revenue stat, the 14-day chart, the per-form and
// per-product panels and the recent-sales rows.
//
// THE DEFECT THIS EXISTS FOR (t_71c67471)
//   The Sales page formatted every amount with a hardcoded `RM${…}`, and
//   `toSaleRow` on the API dropped `sales.currency`, so the page had nothing to
//   label an amount with even if it had wanted to. A workspace can be set to
//   USD/SGD/EUR in Settings → Currency (issue #185), and a sale keeps the
//   currency it was RECORDED in — so a USD sale rendered as "RM24.90": right
//   number, wrong money. The copy already promises "historical sales keep their
//   original currency"; the UI could not show it.
//
// WHY A HAND-ROLLED TABLE INSTEAD OF Intl.NumberFormat({style:"currency"})
//   Intl is the obvious answer and it is the wrong one here: with the en-MY
//   locale this product formats in, `Intl` renders MYR as "RM 24.90" (a
//   non-breaking space after the symbol), while every existing MYR surface —
//   the page, the CSV, the docs and the QA evidence that measured this page —
//   reads "RM24.90". Switching MYR rendering would be an unrelated, silent
//   change to every screenshot and assertion of the default workspace. The
//   table below keeps MYR byte-identical to what shipped and gives the other
//   currencies a conventional symbol; a currency with no settled short symbol
//   falls back to its ISO code plus a space ("AED 24.90"), which is a correct
//   label rather than a guess.
//
// MINOR UNITS: amounts are integer sen-style minor units everywhere in this
// repo (AGENTS.md), so every currency is divided by 100 and printed with two
// decimals, including JPY/KRW/VND. Printing ¥25 for a record stored as 2490
// would silently drop the minor unit the database actually holds.
//
// This module is pure so it can be unit-tested: `node --test` cannot load JSX
// (see src/lib/salesRows.ts for the same constraint).

export const DEFAULT_CURRENCY = "MYR";

// Short symbols, keyed by ISO code. Anything absent falls back to `${CODE} `.
const SYMBOLS: Record<string, string> = {
  MYR: "RM",
  USD: "US$",
  SGD: "S$",
  EUR: "€",
  GBP: "£",
  AUD: "A$",
  CNY: "CN¥",
  JPY: "JP¥",
  THB: "฿",
  IDR: "Rp",
  PHP: "₱",
  HKD: "HK$",
  KRW: "₩",
  INR: "₹",
  NZD: "NZ$",
  CAD: "C$",
  VND: "₫",
  BND: "B$",
  TWD: "NT$",
  MMK: "K",
  KHR: "៛",
  EGP: "E£",
};

// normaliseCurrency is the single rule for reading a currency off a row: trim,
// upper-case, and MYR when the API sent nothing (a sale written before the
// currency column existed, or a row shape older than this change). The page
// never reads the workspace's current setting for this — the ROW wins, because
// a workspace that switches currency must not relabel its own history.
export function normaliseCurrency(code?: string | null): string {
  const c = (code ?? "").trim().toUpperCase();
  return c === "" ? DEFAULT_CURRENCY : c;
}

// currencySymbol is the label that goes in front of an amount.
export function currencySymbol(code?: string | null): string {
  const c = normaliseCurrency(code);
  return SYMBOLS[c] ?? `${c} `;
}

// formatMoney renders integer minor units in the row's own currency:
//   formatMoney(2490, "USD") === "US$24.90"
//   formatMoney(2490)        === "RM24.90"   (the default, unchanged)
export function formatMoney(sen: number, currency?: string | null): string {
  const amount = (Math.abs(sen) / 100).toLocaleString("en-MY", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
  const sign = sen < 0 ? "-" : "";
  return `${sign}${currencySymbol(currency)}${amount}`;
}

export type SaleAmount = { sen: number; currency?: string | null };

// formatCurrencyTotals renders a per-currency total map — the shape the API's
// `revenue_by_currency` / `paid_revenue_by_currency` fields carry — the same way
// formatTotals renders a list of rows: ONE LABEL PER CURRENCY, never a sum
// across currencies.
//
//   formatCurrencyTotals({ MYR: 2490 })                  === "RM24.90"
//   formatCurrencyTotals({ MYR: 2490, USD: 100 })        === "RM24.90 + US$1.00"
//   formatCurrencyTotals({}, "USD")                      === "US$0.00"
//
// The fallback is the workspace's own currency, used only when the map has no
// entry at all (a form with no sales yet): a zero must still be labelled in the
// money the workspace actually charges, not in a hardcoded RM.
export function formatCurrencyTotals(
  totals?: Record<string, number> | null,
  fallbackCurrency?: string | null,
): string {
  const entries = Object.entries(totals ?? {}).filter(([, sen]) => Number.isFinite(sen));
  if (entries.length === 0) return formatMoney(0, fallbackCurrency);
  // Zero-only maps keep their entry (so "US$0.00" names the right money); a map
  // with real money drops the zeros it does not need to show. Biggest first,
  // then code, so the string does not depend on the JSON object's key order.
  const nonZero = entries.filter(([, sen]) => sen !== 0);
  const shown = nonZero.length > 0 ? nonZero : entries;
  shown.sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]) || a[0].localeCompare(b[0]));
  return shown.map(([code, sen]) => formatMoney(sen, code)).join(" + ");
}

// formatTotals adds up amounts that may not share a currency. It never adds
// across currencies (that would be the conversion this product refuses to do
// silently): it groups per currency, preserving first-seen order, and joins the
// groups with " + ". A uniform list — the common case — reads exactly like one
// amount: "RM34.90"; a mixed one reads "RM24.90 + US$1.00".
export function formatTotals(rows: SaleAmount[]): string {
  const groups = new Map<string, number>();
  for (const row of rows) {
    const code = normaliseCurrency(row.currency);
    groups.set(code, (groups.get(code) ?? 0) + row.sen);
  }
  return [...groups.entries()].map(([code, sen]) => formatMoney(sen, code)).join(" + ");
}
