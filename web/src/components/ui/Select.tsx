// Select (card t_f2e231d3): the native select, styled.
//
// Native on purpose: it keeps the platform picker (which is the accessible one on
// a phone and the one a keyboard user knows), the `option` list, and the form
// semantics. `appearance-none` removes the OS arrow so the control matches the
// other two; the right padding is the room that arrow used to take.

import { forwardRef } from "react";
import { CONTROL_CLASS } from "./control";

export type SelectProps = React.SelectHTMLAttributes<HTMLSelectElement>;

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { className, children, ...props },
  ref,
) {
  return (
    <select
      {...props}
      ref={ref}
      className={[CONTROL_CLASS, "appearance-none pr-jf-8", className].filter(Boolean).join(" ")}
    >
      {children}
    </select>
  );
});

Select.displayName = "Select";
