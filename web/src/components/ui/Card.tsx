// Card (card t_f2e231d3): the app's one surface primitive.
//
// WHY. 61 pages each paint their own card — `bg-white border rounded-xl` in some,
// `jf-card` in others, a bare `div` with a shadow in a third — so "a card" was
// three different edges and two different radii. This is the surface with the
// decisions already made: `--jf-surface` fill, `--jf-border` edge,
// `--jf-radius-lg`, and an elevation step from `--jf-shadow-1/2/3`.
//
// `elevation="flat"` exists for a card INSIDE a card (the media picker's tiles),
// where a shadow would stack into a grey halo.
// `interactive` adds the affordance a clickable card needs; it is a prop rather
// than a className so the hover fill and the focus ring are the same token on
// every clickable card.

import { forwardRef } from "react";

export type CardProps = React.HTMLAttributes<HTMLDivElement> & {
  elevation?: "flat" | "raised" | "floating";
  interactive?: boolean;
};

export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { elevation = "raised", interactive = false, className, ...props },
  ref,
) {
  const shadow =
    elevation === "flat" ? "" : elevation === "floating" ? "shadow-jf-3" : "shadow-jf-1";
  const affordance = interactive
    ? "cursor-pointer transition-colors hover:bg-[var(--jf-surface-2)] " +
      "focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-[var(--jf-focus-ring)]"
    : "";
  return (
    <div
      {...props}
      ref={ref}
      className={[
        "rounded-jf-lg border border-[var(--jf-border)] bg-[var(--jf-surface)]",
        shadow,
        affordance,
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    />
  );
});

Card.displayName = "Card";
