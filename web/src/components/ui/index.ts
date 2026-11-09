// The primitive layer (card t_f2e231d3).
//
// IMPORT FROM HERE, not from the individual files: the barrel is what lets the
// layer be re-organised (a primitive split in two, a shared helper moved) without
// a hundred call sites, and it is the list `src/lib/designTokens.test.ts` uses to
// hold the /design gallery to every primitive that exists — a new file in this
// directory that is not exported here and not shown at /design is a page an agent
// cannot discover.
//
// These are PRIMITIVES: no data fetching, no copy, no i18n strings, no business
// rule. A component that needs any of those belongs in `components/`, next to the
// feature it serves.

export { Alert } from "./Alert";
export type { AlertProps, AlertTone } from "./Alert";
export { Badge } from "./Badge";
export type { BadgeProps, BadgeTone } from "./Badge";
export { Button } from "./Button";
export type { ButtonProps, ButtonSize, ButtonVariant } from "./Button";
export { Card } from "./Card";
export type { CardProps } from "./Card";
export { Checkbox } from "./Checkbox";
export type { CheckboxProps } from "./Checkbox";
export { CodeInput } from "./CodeInput";
export type { CodeInputProps } from "./CodeInput";
export { Container } from "./Container";
export type { ContainerProps } from "./Container";
export { EmptyState } from "./EmptyState";
export type { EmptyStateProps } from "./EmptyState";
export { Field } from "./Field";
export type { FieldProps } from "./Field";
export { Input } from "./Input";
export type { InputProps } from "./Input";
export { Section } from "./Section";
export type { SectionProps } from "./Section";
export { Select } from "./Select";
export type { SelectProps } from "./Select";
export { Skeleton } from "./Skeleton";
export type { SkeletonProps } from "./Skeleton";
export {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "./Table";
export type {
  TableBodyProps,
  TableCellProps,
  TableEmptyProps,
  TableHeadProps,
  TableHeaderCellProps,
  TableProps,
  TableRowProps,
} from "./Table";
export { Tabs } from "./Tabs";
export type { TabItem, TabsProps } from "./Tabs";
export { Textarea } from "./Textarea";
export type { TextareaProps } from "./Textarea";
