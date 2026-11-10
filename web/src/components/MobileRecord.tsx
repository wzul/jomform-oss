// The dashboard TABLES' phone layout (card t_c83ecf3e).
//
// WHY. The five dashboard tables (/sales, /products, /deliveries, /spam,
// /subscriptions) were desktop tables with columns switched OFF below `sm` —
// 44 `<th>`/`<td>` pairs carried `hidden … sm:table-cell` — and the wrapper's
// `overflow-x-auto` could not help because a `display:none` column has no width
// to scroll to. On a 390px phone the data was simply absent, and on /deliveries
// the absent column was the EVENT (`delivered` / `bounce` / `complaint` / …),
// i.e. the one field the page exists to show.
//
// THE PATTERN, one for all five pages: the table stays exactly as it is for
// `sm` and up (`hidden sm:block` on its wrapper), and below `sm` each row is
// rendered as a CARD — the row's identity, its amount/status as badges, and a
// native `<details>` disclosure holding every column the table hides. Native
// `<details>` rather than a React open-state keeps the list hydration-safe and
// keyboard-operable for free, and nothing is ever `display:none` at a width
// where it is the only copy of its value.
//
// `data-jf-mobile-records` / `data-jf-record` / `data-jf-field="<key>"` are the
// hooks `web/e2e/mobile-tables.spec.ts` reads at 390px; the field keys are what
// make "every column is reachable" assertable by NAME rather than by eyeballing
// a screenshot.
//
// This lives in `components/` and not `components/ui/`: it carries no i18n of its
// own (every label is passed in by the page that owns the words), and the
// primitive layer's gallery rule (`src/lib/designTokens.test.ts`) would otherwise
// require a /design entry for a component that is only ever a table's phone arm.

import type { ReactNode } from "react";

export type MobileRecordField = {
  /** Stable hook for the 390px guard. MUST be unique inside one card. */
  key: string;
  label: string;
  value: ReactNode;
};

export type MobileRecordProps = {
  /**
   * The row's identity: a reference code, a product name, a customer, an email.
   *
   * A STRING, not a node, on purpose: `src/lib/errorMessageI18n.test.ts` reads
   * every `title=` in the tree as a user-facing text attribute and refuses prose
   * handed to it, and a `title={<span className="…">}` reads to that scanner as a
   * text attribute carrying a class list. `titleMono` covers the one bit of
   * styling the identity needs.
   */
  title: string;
  /** Render the identity in the mono face (reference codes, ids). */
  titleMono?: boolean;
  /** Secondary identity (buyer email, form name, reason…) under the title. */
  subtitle?: ReactNode;
  /** A control that belongs beside the identity — the products bulk checkbox. */
  leading?: ReactNode;
  /** The amount, right-aligned on the title line. */
  meta?: ReactNode;
  /** Status chips. Always visible: this is what the phone user scans for. */
  badges?: ReactNode;
  /** The columns the table hides below `sm`, inside the disclosure. */
  fields: MobileRecordField[];
  /** Row actions (view / edit / cancel …). */
  actions?: ReactNode;
  /** The disclosure's own label, in the page's language. */
  detailsLabel: string;
};

/** The list surface: one `<ul>` of cards, shown below `sm` only. */
export function MobileRecordList({ children }: { children: ReactNode }) {
  return (
    <ul data-jf-mobile-records className="divide-y divide-[var(--jf-border)]">
      {children}
    </ul>
  );
}

/** One row of a dashboard table, rendered as a compact card for phones. */
export function MobileRecord({
  title,
  titleMono = false,
  subtitle,
  leading,
  meta,
  badges,
  fields,
  actions,
  detailsLabel,
}: MobileRecordProps) {
  return (
    <li data-jf-record className="px-4 py-3">
      <div className="flex items-start gap-2">
        {leading ? <div className="shrink-0 pt-0.5">{leading}</div> : null}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5">
            <div
              className={
                titleMono
                  ? "min-w-0 break-words font-mono text-xs"
                  : "min-w-0 break-words text-sm font-medium"
              }
            >
              {title}
            </div>
            {meta ? (
              <div className="shrink-0 whitespace-nowrap text-sm font-semibold">{meta}</div>
            ) : null}
          </div>
          {subtitle ? (
            <div className="mt-0.5 break-words text-xs text-[var(--jf-muted)]">{subtitle}</div>
          ) : null}
          {badges ? <div data-jf-badges className="mt-1.5 flex flex-wrap items-center gap-1">{badges}</div> : null}
        </div>
      </div>

      {fields.length > 0 ? (
        <details className="mt-2">
          <summary className="cursor-pointer text-jf-2xs text-[var(--jf-muted)]">
            {detailsLabel}
          </summary>
          <dl className="mt-1.5 space-y-1 border-t border-[var(--jf-border)] pt-1.5 text-xs">
            {fields.map((f) => (
              <div key={f.key} className="flex items-start justify-between gap-3">
                <dt className="shrink-0 text-[var(--jf-muted)]">{f.label}</dt>
                <dd data-jf-field={f.key} className="min-w-0 break-words text-right">
                  {f.value}
                </dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}

      {actions ? (
        <div className="mt-2 flex flex-wrap items-center justify-end gap-1">{actions}</div>
      ) : null}
    </li>
  );
}
