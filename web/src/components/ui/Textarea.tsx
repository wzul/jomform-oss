// Textarea (card t_f2e231d3): the multi-line control.
//
// Same surface as `Input` (see `control.ts`) with `resize-y` — a textarea that
// can be dragged WIDER than its column breaks the page's measure, and one that
// cannot be resized at all hides long copy behind a four-row window.

import { forwardRef } from "react";
import { CONTROL_CLASS } from "./control";

export type TextareaProps = React.TextareaHTMLAttributes<HTMLTextAreaElement>;

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { className, rows = 4, ...props },
  ref,
) {
  return (
    <textarea
      {...props}
      ref={ref}
      rows={rows}
      className={[CONTROL_CLASS, "resize-y", className].filter(Boolean).join(" ")}
    />
  );
});

Textarea.displayName = "Textarea";
