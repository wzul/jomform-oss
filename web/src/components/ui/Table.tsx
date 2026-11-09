// Table (card t_f2e231d3): the data table and its empty state.
//
// WHY. Every list page built its own `<table>` (and `/sales` built a `div` grid),
// with its own header tint, its own row separator and its own idea of what an
// empty result looks like — three of them rendered a header row over nothing, so
// "no rows" and "still loading" looked identical. `TableEmpty` is part of the
// primitive for exactly that reason: the empty state is a ROW in the same table,
// spanning the columns, not a paragraph the page has to remember to render.
//
// Sub-components rather than one component with a `columns` prop: the call sites
// render cells containing links, badges and buttons, and a data-only API would
// force every one of them through a renderer. The `role`s are the native ones; no
// ARIA is added, because a native table is already the accessible structure.
//
// A page that needs horizontal scroll on a phone wraps `<Table>` in
// `<div className="overflow-x-auto">` — the primitive does not impose a wrapper,
// because a wrapper is a layout decision and this layer is not the layout.

import { forwardRef } from "react";

export type TableProps = React.TableHTMLAttributes<HTMLTableElement>;

export const Table = forwardRef<HTMLTableElement, TableProps>(function Table(
  { className, ...props },
  ref,
) {
  return (
    <table
      {...props}
      ref={ref}
      className={["w-full border-collapse text-left text-jf-read-sm", className].filter(Boolean).join(" ")}
    />
  );
});

Table.displayName = "Table";

export type TableHeadProps = React.HTMLAttributes<HTMLTableSectionElement>;

export const TableHead = forwardRef<HTMLTableSectionElement, TableHeadProps>(function TableHead(
  { className, ...props },
  ref,
) {
  return <thead {...props} ref={ref} className={["bg-[var(--jf-surface-2)]", className].filter(Boolean).join(" ")} />;
});

TableHead.displayName = "TableHead";

export type TableBodyProps = React.HTMLAttributes<HTMLTableSectionElement>;

export const TableBody = forwardRef<HTMLTableSectionElement, TableBodyProps>(function TableBody(
  { className, ...props },
  ref,
) {
  return <tbody {...props} ref={ref} className={className} />;
});

TableBody.displayName = "TableBody";

export type TableRowProps = React.HTMLAttributes<HTMLTableRowElement>;

export const TableRow = forwardRef<HTMLTableRowElement, TableRowProps>(function TableRow(
  { className, ...props },
  ref,
) {
  return (
    <tr
      {...props}
      ref={ref}
      className={["border-b border-[var(--jf-border)] last:border-b-0", className].filter(Boolean).join(" ")}
    />
  );
});

TableRow.displayName = "TableRow";

export type TableHeaderCellProps = React.ThHTMLAttributes<HTMLTableCellElement>;

export const TableHeaderCell = forwardRef<HTMLTableCellElement, TableHeaderCellProps>(
  function TableHeaderCell({ className, ...props }, ref) {
    return (
      <th
        {...props}
        ref={ref}
        scope={props.scope ?? "col"}
        className={[
          "border-b border-[var(--jf-border)] px-jf-3 py-jf-2 text-jf-read-xs font-semibold",
          "text-[var(--jf-text-secondary)]",
          className,
        ]
          .filter(Boolean)
          .join(" ")}
      />
    );
  },
);

TableHeaderCell.displayName = "TableHeaderCell";

export type TableCellProps = React.TdHTMLAttributes<HTMLTableCellElement>;

export const TableCell = forwardRef<HTMLTableCellElement, TableCellProps>(function TableCell(
  { className, ...props },
  ref,
) {
  return (
    <td
      {...props}
      ref={ref}
      className={["px-jf-3 py-jf-3 align-middle text-[var(--jf-text)]", className].filter(Boolean).join(" ")}
    />
  );
});

TableCell.displayName = "TableCell";

export type TableEmptyProps = React.HTMLAttributes<HTMLTableRowElement> & {
  /** How many columns the placeholder spans. Required: a `colSpan` that is wrong
   *  silently pushes the table into an extra column. */
  columns: number;
};

export const TableEmpty = forwardRef<HTMLTableRowElement, TableEmptyProps>(function TableEmpty(
  { columns, className, children, ...props },
  ref,
) {
  return (
    <tr {...props} ref={ref} className={className}>
      <td colSpan={columns} className="px-jf-3 py-jf-8 text-center text-[var(--jf-muted)]">
        {children}
      </td>
    </tr>
  );
});

TableEmpty.displayName = "TableEmpty";
