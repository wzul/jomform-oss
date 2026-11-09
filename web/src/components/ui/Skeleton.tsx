// Skeleton (card t_f2e231d3): the loading placeholder.
//
// WHY IT REUSES `.jf-skeleton`. That class is the app's ONE measured pulse: finite
// (three cycles), starting one `fast` step late so a fast response shows a still
// box instead of a flash, and switched off outright by the reduced-motion block.
// A primitive that painted its own `animate-pulse` would be the infinite loop
// that class exists to replace.
//
// `aria-hidden` because a skeleton is not content: a screen reader should reach
// the live region the page announces ("loading…") rather than a stack of empty
// boxes. The caller owns that announcement.

import { forwardRef } from "react";

export type SkeletonProps = React.HTMLAttributes<HTMLDivElement> & {
  /** Shape shortcuts for the two common placeholders. */
  shape?: "line" | "block" | "circle";
};

const SHAPES: Record<NonNullable<SkeletonProps["shape"]>, string> = {
  line: "h-jf-3 w-full rounded-jf-sm",
  block: "h-jf-12 w-full rounded-jf-md",
  circle: "h-jf-8 w-jf-8 rounded-jf-full",
};

export const Skeleton = forwardRef<HTMLDivElement, SkeletonProps>(function Skeleton(
  { shape = "line", className, ...props },
  ref,
) {
  return (
    <div
      {...props}
      ref={ref}
      aria-hidden="true"
      className={["jf-skeleton bg-[var(--jf-surface-2)]", SHAPES[shape], className].filter(Boolean).join(" ")}
    />
  );
});

Skeleton.displayName = "Skeleton";
