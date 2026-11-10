"use client";

// The hero's RIGHT column: a real order form, not a screenshot.
//
// WHY THIS EXISTS (card t_09f60795)
//   The old landing page promised "beautiful order forms" and showed the
//   visitor none — the hero was a centred text block, so the first screen
//   carried no product at all. This is the whole point of variant A: the
//   product IS the hero.
//
// IT IS AN ILLUSTRATION, NOT A FORM — and it says so to assistive tech:
//   the card is `aria-hidden="true"` and every control inside it carries
//   `tabIndex={-1}`, so a screen reader or a keyboard user never lands in a
//   fake checkout. `aria-hidden` on an ancestor that still contains focusable
//   elements is the axe `aria-hidden-focus` violation; `tabIndex={-1}` is what
//   keeps the buttons out of the tab order, so the rule is satisfied rather
//   than muted. The steppers are still pressable with a mouse/touch — the
//   numbers move, which is what makes the mock read as a live product.
//
// MONEY IS INTEGER SEN (repo rule), and the LABEL comes from `lib/money.ts`:
// a hand-rolled `RM${...}` helper is refused by src/lib/workspacePrice.test.ts
// repo-wide, and `formatMoney` is the one rule that renders integer minor units
// (MYR is its default and prints "RM186.00", byte-identical to the shipped
// surfaces). The two rows are 8900 sen each and the shipping line is 800 sen,
// so the resting state is 2 x RM89.00 + RM8.00 = RM186.00 — the figure the card
// names. No arithmetic ever happens on a float.
import { useState } from "react";

import { useI18n } from "../i18n";
import { LANDING_PAY_OPTIONS } from "../i18nDict";
import { formatMoney } from "../../lib/money";

/** 8900 sen = RM89.00, the price of each row. */
const PRICE_SEN = 8900;
/** 800 sen = RM8.00, J&T within the Klang Valley. */
const SHIPPING_SEN = 800;
const MAX_QTY = 9;

/**
 * The two rows of the illustration. The names are the GOODS (a seller's own
 * product copy) rather than UI chrome, so they stay as written in all five
 * locales — translating a seller's product name would be wrong.
 */
const ROWS = [
  { thumb: "👗", name: "Baju Kurung Moden", note: "Size M · Dusty Rose" },
  { thumb: "🧣", name: "Selendang Songket", note: "One size · Gold" },
] as const;

export default function HeroOrderForm() {
  const { t } = useI18n();
  const [qty, setQty] = useState<readonly number[]>([1, 1]);
  // Card t_453489b9: the row the mock has picked. The first option is
  // preselected — the product's own picker does the same, so a buyer with one
  // sensible default is never asked to click for nothing — and the Pay button's
  // second line follows the pick.
  const [method, setMethod] = useState<string>(LANDING_PAY_OPTIONS[0].id);
  const chosen =
    LANDING_PAY_OPTIONS.find((option) => option.id === method) ?? LANDING_PAY_OPTIONS[0];
  // The rows' copy, in LANDING_PAY_OPTIONS' order, resolved with LITERAL keys
  // rather than `t(option.labelKey)` inside the map: a key built from a variable
  // is invisible to src/lib/i18nKeys.ts, the scan that catches a key dropped in a
  // merge (t() then renders the raw key to every user in every locale). It is a
  // LIST, not a map keyed by option id, so this file names no method at all —
  // src/lib/landingPayMethods.test.ts fails if it does, and asserts this list's
  // length and order against the options list it renders.
  const copy = [
    { label: t("land.payfpx"), desc: t("land.payfpxdesc") },
    { label: t("land.paydnqr"), desc: t("land.paydnqrdesc") },
    { label: t("land.paycard"), desc: t("land.paycarddesc") },
    { label: t("land.paywallet"), desc: t("land.paywalletdesc") },
  ];

  const subtotalSen = PRICE_SEN * qty.reduce((sum, q) => sum + q, 0);
  const totalSen = subtotalSen + SHIPPING_SEN;

  const step = (row: number, delta: number) =>
    setQty((current) =>
      current.map((q, i) => (i === row ? Math.min(MAX_QTY, Math.max(0, q + delta)) : q)),
    );

  return (
    // The dark stage the white card sits on — this is the "product-led" visual
    // of variant A. Tokens, not hex (design-system rule): `--jf-ink*` are the
    // measured dark-surface pair from globals.css.
    <div className="relative rounded-3xl bg-[var(--jf-ink)] p-4 sm:p-6">
      <div
        aria-hidden="true"
        className="rounded-2xl bg-[var(--jf-surface)] p-5 text-[var(--jf-text)] shadow-xl"
      >
        <div className="min-w-0">
          <p className="truncate text-base font-semibold text-[var(--jf-text)]">
            Raya Collection 2026
          </p>
          <p className="truncate text-xs text-[var(--jf-muted)]">Nur Boutique · Shah Alam</p>
        </div>

        <ul className="mt-4 divide-y divide-[var(--jf-border)] border-y border-[var(--jf-border)]">
          {ROWS.map((row, i) => (
            <li key={row.name} className="flex items-start gap-3 py-3">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-[var(--jf-bg)] text-base">
                {row.thumb}
              </span>
              {/* The stepper and the line total sit UNDER the name, not beside
                  it. Beside it they cost ~136px of the 310px card a 390px phone
                  gives this card, collapsing the name column to ~74px — the
                  mock's own product name then rendered as "Baju K…" (measured on
                  the built page, 2026-11-09), i.e. the hero showed a broken order
                  form. Stacking gives the name the whole column and changes no
                  figure. */}
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium text-[var(--jf-text)]">{row.name}</p>
                <p className="text-jf-2xs text-[var(--jf-muted)]">{row.note}</p>
                <div className="mt-1.5 flex items-center justify-between gap-2">
                  <span className="flex items-center gap-1.5">
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => step(i, -1)}
                      className="flex h-6 w-6 items-center justify-center rounded-md border border-[var(--jf-border)] text-xs font-semibold text-[var(--jf-text-secondary)] hover:bg-[var(--jf-bg)]"
                    >
                      −
                    </button>
                    <span className="w-4 text-center text-xs text-[var(--jf-text-secondary)]">
                      {qty[i]}
                    </span>
                    <button
                      type="button"
                      tabIndex={-1}
                      onClick={() => step(i, 1)}
                      className="flex h-6 w-6 items-center justify-center rounded-md border border-[var(--jf-border)] text-xs font-semibold text-[var(--jf-text-secondary)] hover:bg-[var(--jf-bg)]"
                    >
                      +
                    </button>
                  </span>
                  <span className="text-sm font-semibold tabular-nums text-[var(--jf-text)]">
                    {formatMoney(PRICE_SEN * qty[i])}
                  </span>
                </div>
              </div>
            </li>
          ))}
        </ul>

        <div className="mt-3 grid grid-cols-2 gap-2">
          <span className="rounded-lg border border-[var(--jf-border)] bg-[var(--jf-bg)] px-2.5 py-1.5">
            <span className="block text-jf-3xs font-semibold uppercase tracking-wider text-[var(--jf-muted)]">
              {t("land.fmname")}
            </span>
            <span className="block truncate text-xs font-medium text-[var(--jf-text)]">
              Nurul Aisyah
            </span>
          </span>
          <span className="rounded-lg border border-[var(--jf-border)] bg-[var(--jf-bg)] px-2.5 py-1.5">
            <span className="block text-jf-3xs font-semibold uppercase tracking-wider text-[var(--jf-muted)]">
              {t("land.fmwa")}
            </span>
            <span className="block truncate text-xs font-medium text-[var(--jf-text)]">
              012-345 6789
            </span>
          </span>
        </div>

        <dl className="mt-3 space-y-1 border-t border-dashed border-[var(--jf-border)] pt-2.5 text-xs">
          <div className="flex items-baseline justify-between gap-3">
            <dt className="text-[var(--jf-muted)]">{t("land.fmsubtotal")}</dt>
            <dd className="tabular-nums text-[var(--jf-text-secondary)]">{formatMoney(subtotalSen)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <dt className="truncate text-[var(--jf-muted)]">{t("land.fmshipping")}</dt>
            <dd className="tabular-nums text-[var(--jf-text-secondary)]">{formatMoney(SHIPPING_SEN)}</dd>
          </div>
          <div className="flex items-baseline justify-between gap-3 pt-1">
            <dt className="text-sm font-semibold text-[var(--jf-text)]">{t("land.fmtotal")}</dt>
            <dd className="text-base font-bold tabular-nums text-[var(--jf-text)]">
              {formatMoney(totalSen)}
            </dd>
          </div>
        </dl>

        {/* Card t_453489b9: the payment-method CHOICE, above the Pay button —
            the approved mockup. It replaces the chip strip that used to sit
            under the button (card t_b4e5881d deleted the duplicate caption and
            kept one strip; this card replaces that strip with the real control
            the product now has). The rows are the four method GROUPS, in the
            product's own order, and every label/description comes from the
            dictionary — no method name is written in this file
            (src/lib/landingPayMethods.test.ts fails if one is). */}
        <div
          role="radiogroup"
          aria-labelledby="landPayTitle"
          className="mt-3 overflow-hidden rounded-xl border border-[var(--jf-border)]"
        >
          <div className="flex items-baseline justify-between gap-2 px-3 pb-1.5 pt-2.5">
            <span
              id="landPayTitle"
              className="text-jf-3xs font-semibold uppercase tracking-wider text-[var(--jf-muted)]"
            >
              {t("land.paytitle")}
            </span>
            <span className="text-jf-3xs text-[var(--jf-muted)]">{t("land.paypick")}</span>
          </div>
          {LANDING_PAY_OPTIONS.map((option, i) => {
            const selected = option.id === method;
            const text = copy[i];
            return (
              <label
                key={option.id}
                className={
                  "flex min-h-11 cursor-pointer items-center gap-2.5 border-t border-[var(--jf-border)] px-3 py-2.5 " +
                  (selected
                    ? "bg-[var(--jf-bg)] shadow-[inset_3px_0_0_var(--jf-accent)]"
                    : "")
                }
              >
                <input
                  type="radio"
                  name="land-pay"
                  checked={selected}
                  onChange={() => setMethod(option.id)}
                  tabIndex={-1}
                  className="h-5 w-5 shrink-0 accent-[var(--jf-accent)]"
                />
                <span className="min-w-0">
                  <span className="block text-sm font-semibold text-[var(--jf-text)]">
                    {text.label}
                  </span>
                  <span className="block text-jf-2xs text-[var(--jf-muted)]">
                    {text.desc}
                  </span>
                </span>
              </label>
            );
          })}
          <p className="border-t border-[var(--jf-border)] px-3 py-2 text-jf-3xs text-[var(--jf-muted)]">
            {t("land.paynote")}
          </p>
        </div>

        <button
          type="button"
          tabIndex={-1}
          className="mt-3 flex w-full flex-col items-center justify-center gap-0.5 rounded-xl bg-[var(--jf-ink)] px-4 py-3 text-sm font-bold text-[var(--jf-ink-text)]"
        >
          <span>{t("land.fmpay", { amount: formatMoney(totalSen) })}</span>
          <span className="text-jf-2xs font-medium opacity-90">
            {t("land.payvia", { method: t(chosen.labelKey) })}
          </span>
        </button>
      </div>
    </div>
  );
}
