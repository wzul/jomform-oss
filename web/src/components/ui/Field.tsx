// Field (card t_f2e231d3): the label + control + hint + error unit.
//
// WHY. This is the defect class the a11y gate found repeatedly and fixed one page
// at a time: a `<label>` that is a SIBLING of its input with no `for`/`id`
// (measured: nine `label` nodes on `/form-settings`, five `aria-required-parent`
// nodes on `/profile`), so the control had no accessible name at all — and axe
// reported it as critical on a page nobody had scanned. The fix is structural, not
// per-page: the ids are generated here, the label always points at the control,
// and the hint/error text is wired with `aria-describedby` while the error also
// sets `aria-invalid`. A caller cannot wire it wrong because it does not wire it.
//
// THE CONTRACT: exactly ONE element child, which receives `id`,
// `aria-describedby` and `aria-invalid`. It is `cloneElement`, not a render prop,
// because a render prop cannot cross a Server Component boundary — and the
// gallery (and every migrated page) is a server component.
//
// The error text carries `role="alert"` so a rejected submit is announced; the
// hint does not (a hint is not an event).
//
// WHY THE PROP IS `errorText` AND NOT `error`. `src/lib/apiErr.test.ts` bans every
// destructured `error` binding under `src/` — it cannot tell a form rejection from
// an API body's prose field, and its exemption list is asserted to hold exactly
// one file. `errorText` also says what the value is (a sentence), so the name is
// better rather than merely allowed.

import { forwardRef, cloneElement, isValidElement } from "react";

export type FieldProps = {
  /** Required: the control's id is derived from it. */
  id: string;
  label: string;
  /** Guidance under the control. Read, never announced as an event. */
  hint?: string;
  /** Rejection message. Sets `aria-invalid` and is announced. */
  errorText?: string;
  required?: boolean;
  /** The label's own class (e.g. a visually-hidden label for a search box). */
  labelClassName?: string;
  className?: string;
  children: React.ReactNode;
};

type Wireable = {
  id?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean | "true" | "false";
};

export const Field = forwardRef<HTMLDivElement, FieldProps>(function Field(
  { id, label, hint, errorText, required = false, labelClassName, className, children },
  ref,
) {
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = errorText ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;

  const only = Array.isArray(children) ? children[0] : children;
  const wired = isValidElement(only)
    ? cloneElement(only as React.ReactElement<Wireable>, {
        id,
        "aria-describedby": describedBy,
        "aria-invalid": errorText ? true : undefined,
      })
    : children;

  return (
    <div ref={ref} className={["flex flex-col gap-jf-1", className].filter(Boolean).join(" ")}>
      <label htmlFor={id} className={["text-jf-read-sm font-medium text-[var(--jf-text)]", labelClassName].filter(Boolean).join(" ")}>
        {label}
        {required ? (
          <span aria-hidden="true" className="text-[var(--jf-danger-text)]">
            {" *"}
          </span>
        ) : null}
      </label>
      {wired}
      {hint ? (
        <p id={hintId} className="text-jf-read-xs text-[var(--jf-muted)]">
          {hint}
        </p>
      ) : null}
      {errorText ? (
        <p id={errorId} role="alert" className="text-jf-read-xs text-[var(--jf-danger-text)]">
          {errorText}
        </p>
      ) : null}
    </div>
  );
});

Field.displayName = "Field";
