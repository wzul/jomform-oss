// Checkbox (card t_f2e231d3): the checkbox that carries the brand colour.
//
// WHY. A bare `<input type="checkbox">` is painted by the OS, and the app's only
// way to brand it was `accent-indigo-600` at a dozen call sites — a palette step
// that is neither `--jf-primary` nor anything the contrast table measures.
// `accent-[var(--jf-primary)]` is the same pixel as the primary CTA's fill
// (#5e6ad2), so a checked box and a pressed button are one brand step.
//
// The box is sized from the space scale (`--jf-space-4` = 16px, the WCAG 2.2
// target-size floor) and the label is the caller's (`Field` wires it); this file
// deliberately has no `label` prop — two ways to name a checkbox is how one of
// them ends up without a `for`.

import { forwardRef } from "react";

export type CheckboxProps = React.InputHTMLAttributes<HTMLInputElement>;

export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { className, ...props },
  ref,
) {
  return (
    <input
      {...props}
      ref={ref}
      type="checkbox"
      className={[
        "h-jf-4 w-jf-4 rounded-jf-sm border border-[var(--jf-border-strong)]",
        "accent-[var(--jf-primary)] cursor-pointer",
        "disabled:cursor-not-allowed disabled:opacity-60",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    />
  );
});

Checkbox.displayName = "Checkbox";
