// The primitive × state matrix the /design gallery renders (card t_46ae7ad7).
//
// WHAT THIS FILE IS. The gallery used to show six states for ONE primitive
// (Button) and a happy-path specimen for the rest, so "every state of every
// primitive" was a claim a reader had to check by hand. Here the applicability of
// each (primitive, state) pair is DATA:
//
//   null   the state exists, and the gallery must RENDER it;
//   string the state does not exist on that primitive, and this is WHY — printed
//          on the page instead of the cell being silently absent.
//
// WHY DATA AND NOT JUST JSX. Two independent guards are worth more than one, and
// they are worth different things:
//
//   * `designTokens.test.ts` (node, no JSX) requires every name exported by
//     `components/ui/index.ts` to appear in this table, so a NEW primitive cannot
//     land without someone deciding what its states are;
//   * `app/design/page.tsx` types its specimens as
//     `{ [P in PrimitiveName]: { [S in State as PRIMITIVE_STATES[P][S] extends
//     null ? S : never]: ReactNode } }`, so a cell this table calls APPLICABLE
//     with no specimen is a COMPILE error, and a specimen for a state this table
//     calls impossible is a compile error too. The two files cannot disagree —
//     not "should not", cannot: there is one truth (this table) and the page is
//     checked against it by tsc.
//
// WHY SOME PRIMITIVES HAVE NO `hover`. `CONTROL_SURFACE` in
// `components/ui/control.ts` changes on FOCUS and on `aria-invalid`, never on
// hover — the same class list has to look right on a touch screen, where hover
// does not exist. So Input, Textarea, Select, CodeInput and Field report hover as
// not applicable for a measured reason, not because the cell was forgotten. Card,
// Checkbox, Tabs and Button DO hover, and those cells render the real control.
//
// A REASON IS REQUIRED AND IS SHOWN. "Not applicable" is a decision about the
// product; the test requires every reason to be at least 12 characters, so it is
// a sentence a reader can check rather than a blank.

/** The six states the design system recognises. Order is the order the page prints. */
export const STATES = ["default", "hover", "focus-visible", "disabled", "loading", "error"] as const;

export type State = (typeof STATES)[number];

/** Every primitive the barrel exports, as one entry per component (Table's parts fold into `Table`). */
export type PrimitiveName =
  | "Alert"
  | "Badge"
  | "Button"
  | "Card"
  | "Checkbox"
  | "CodeInput"
  | "Container"
  | "EmptyState"
  | "Field"
  | "Input"
  | "Section"
  | "Select"
  | "Skeleton"
  | "Table"
  | "Tabs"
  | "Textarea";

/**
 * `null` = the state applies and the gallery renders it. A string = it does not
 * apply, and this sentence says why (it is printed at /design).
 */
export type StateCell = null | string;

/**
 * `satisfies` (not a type annotation) on purpose: it holds the object to
 * `PrimitiveName` × `State` — a missing primitive, a missing state or an unknown
 * key is a compile error — while KEEPING each cell's literal type, which is what
 * lets the page derive "which cells are applicable" from this table at compile
 * time. Annotating the constant would widen every cell to `StateCell` and the
 * derivation would collapse to "none are applicable".
 */
export const PRIMITIVE_STATES = {
  Alert: {
    default: null,
    hover: "an Alert is not interactive — nothing to hover",
    "focus-visible": "an Alert holds no focusable target of its own",
    disabled: "a message is always shown; there is no disabled Alert",
    loading: "a pending message is a Skeleton or a live region, not an Alert",
    error: null,
  },
  Badge: {
    default: null,
    hover: "a Badge is a static status label, never a pointer target",
    "focus-visible": "a Badge is not focusable — it is not a control",
    disabled: "a status has no disabled state; it is absent or it is not",
    loading: "a pending status is the warning tone, not a loading Badge",
    error: null,
  },
  Button: {
    default: null,
    hover: null,
    "focus-visible": null,
    disabled: null,
    loading: null,
    error: "a Button does not carry a rejection — the Field or Alert beside it does",
  },
  Card: {
    default: null,
    hover: null,
    "focus-visible": null,
    disabled: "a Card has no disabled state — the controls inside it do",
    loading: "a loading card is a Card HOLDING Skeletons, not a card state",
    error: "a rejection inside a card is an Alert, not a fill on the card",
  },
  Checkbox: {
    default: null,
    hover: "the box has no hover fill — only the cursor and accent colour change",
    "focus-visible": null,
    disabled: null,
    loading: "a checkbox never loads — it is checked or it is not",
    error: "a rejected checkbox is announced by the Field's error text; the box has no error fill",
  },
  CodeInput: {
    default: null,
    hover: "the shared control surface has no hover state, by design",
    "focus-visible": null,
    disabled: null,
    loading: "the code field does not load; the submit beside it does",
    error: null,
  },
  Container: {
    default: null,
    hover: "a Container is a layout wrapper, not a pointer target",
    "focus-visible": "a Container is not focusable — it is a width and a gutter",
    disabled: "a Container has no disabled state; its content does",
    loading: "a Container is still while its children load",
    error: "an error is an Alert inside the Container, not the Container",
  },
  EmptyState: {
    default: null,
    hover: "the action INSIDE it is the target, not the EmptyState",
    "focus-visible": "the action inside it takes the focus ring, not the wrapper",
    disabled: "there is no disabled empty state — an empty list is empty",
    loading: "the loading state is a Skeleton, shown INSTEAD of an EmptyState",
    error: "a failure is an Alert, not an EmptyState — they read differently",
  },
  Field: {
    default: null,
    hover: "the control inside carries the hover treatment, not the label",
    "focus-visible": "the control inside the Field takes the ring; the Field owns the label, hint and message",
    disabled: null,
    loading: "a Field does not load — the submit it belongs to is busy",
    error: null,
  },
  Input: {
    default: null,
    hover: "the shared control surface has no hover state, by design",
    "focus-visible": null,
    disabled: null,
    loading: "an input does not load; the submit it belongs to does",
    error: null,
  },
  Section: {
    default: null,
    hover: "a Section is a vertical rhythm wrapper, not a target",
    "focus-visible": "a Section is not focusable — it only sets spacing",
    disabled: "a Section has no disabled state; its children do",
    loading: "a Section is still while its children load",
    error: "an error is an Alert inside the Section, not the Section",
  },
  Select: {
    default: null,
    hover: "the shared control surface has no hover state, by design",
    "focus-visible": null,
    disabled: null,
    loading: "a select does not load; the submit it belongs to does",
    error: null,
  },
  Skeleton: {
    default: null,
    hover: "a Skeleton is aria-hidden decoration — never a target",
    "focus-visible": "a Skeleton is hidden from assistive technology and cannot focus",
    disabled: "there is no disabled Skeleton — it is present or it is gone",
    loading: null,
    error: "a failed load is an Alert, not a Skeleton that never resolves",
  },
  Table: {
    default: null,
    hover: "rows in this layer carry no hover fill — a row action does",
    "focus-visible": "the scroll wrapper is the focusable region, not the table",
    disabled: "there is no disabled Table; its rows are read-only already",
    loading: null,
    error: "a failed load is an Alert above the table, not a table state",
  },
  Tabs: {
    default: null,
    hover: null,
    "focus-visible": null,
    disabled: null,
    loading: "a tab strip does not load; its panels do",
    error: "a panel shows an Alert; the strip has no error state",
  },
  Textarea: {
    default: null,
    hover: "the shared control surface has no hover state, by design",
    "focus-visible": null,
    disabled: null,
    loading: "a textarea does not load; the submit it belongs to does",
    error: null,
  },
} as const satisfies Record<PrimitiveName, Record<State, StateCell>>;

/**
 * The barrel's exported names → the matrix entry that covers them.
 *
 * `TableBody`, `TableCell`, `TableEmpty`, `TableHead`, `TableHeaderCell` and
 * `TableRow` are ONE primitive with parts: they have no states of their own (they
 * are the structure a table is made of), so folding them into `Table` is the
 * honest shape rather than six rows of "not applicable".
 *
 * The unit test requires every name the barrel exports to appear here AND every
 * value here to be a key of PRIMITIVE_STATES — a new export with no decision is a
 * red build, and a decision naming a primitive that does not exist is red too.
 */
export const BARREL_FAMILY: Readonly<Record<string, PrimitiveName>> = {
  Alert: "Alert",
  Badge: "Badge",
  Button: "Button",
  Card: "Card",
  Checkbox: "Checkbox",
  CodeInput: "CodeInput",
  Container: "Container",
  EmptyState: "EmptyState",
  Field: "Field",
  Input: "Input",
  Section: "Section",
  Select: "Select",
  Skeleton: "Skeleton",
  Table: "Table",
  TableBody: "Table",
  TableCell: "Table",
  TableEmpty: "Table",
  TableHead: "Table",
  TableHeaderCell: "Table",
  TableRow: "Table",
  Tabs: "Tabs",
  Textarea: "Textarea",
};
