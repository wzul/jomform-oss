// Input (card t_f2e231d3): the single-line text control.
//
// The surface itself lives in `control.ts`. What this file adds is the element
// and the forwardRef, so a form can focus the field it just rejected. The error
// state is `aria-invalid` (set by `Field`), NOT a class: the attribute is both
// what a screen reader reads and what paints the red edge, so a caller cannot
// show the colour without the state — or the state without the colour.

import { forwardRef } from "react";
import { CONTROL_CLASS } from "./control";

export type InputProps = React.InputHTMLAttributes<HTMLInputElement>;

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, ...props },
  ref,
) {
  return <input {...props} ref={ref} className={[CONTROL_CLASS, className].filter(Boolean).join(" ")} />;
});

Input.displayName = "Input";
