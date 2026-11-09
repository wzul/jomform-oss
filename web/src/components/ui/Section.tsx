// Section (card t_f2e231d3): the vertical rhythm.
//
// WHY. The app's spacing between blocks was typed per page (`py-8`, `mt-6`,
// `mb-12`, `space-y-7`), so the same two blocks are 24px apart on one page and
// 48px on another. `spacing` is one of four steps from the token scale, which
// makes "this block is a section" a decision about the page rather than a number
// typed at the call site.
//
// `spacing="none"` exists for the FIRST block on a page, where a top padding
// would push the heading away from the header above it.

import { forwardRef } from "react";

export type SectionProps = React.HTMLAttributes<HTMLElement> & {
  spacing?: "none" | "sm" | "md" | "lg";
};

const SPACING: Record<NonNullable<SectionProps["spacing"]>, string> = {
  none: "",
  sm: "py-jf-4",
  md: "py-jf-8",
  lg: "py-jf-12",
};

export const Section = forwardRef<HTMLElement, SectionProps>(function Section(
  { spacing = "md", className, ...props },
  ref,
) {
  return (
    <section
      {...props}
      ref={ref}
      className={[SPACING[spacing], className].filter(Boolean).join(" ")}
    />
  );
});

Section.displayName = "Section";
