// Container (card t_f2e231d3): the page gutter and measure.
//
// WHY. Every page re-picked its own `max-w-*` and its own horizontal padding, so
// two pages that look the same are a few pixels apart and nobody sees it because
// the comparison is never on screen at once. `width` names the READING context
// rather than a number — `prose` is a documentation column, `default` is a page,
// `wide` is a table that needs the room — and the gutter is the token scale
// (`--jf-space-4` on a phone, `--jf-space-6` from `sm`).

import { forwardRef } from "react";

export type ContainerProps = React.HTMLAttributes<HTMLDivElement> & {
  width?: "prose" | "default" | "wide";
};

const WIDTHS: Record<NonNullable<ContainerProps["width"]>, string> = {
  prose: "max-w-3xl",
  default: "max-w-6xl",
  wide: "max-w-screen-2xl",
};

export const Container = forwardRef<HTMLDivElement, ContainerProps>(function Container(
  { width = "default", className, ...props },
  ref,
) {
  return (
    <div
      {...props}
      ref={ref}
      className={["mx-auto w-full px-jf-4 sm:px-jf-6", WIDTHS[width], className]
        .filter(Boolean)
        .join(" ")}
    />
  );
});

Container.displayName = "Container";
