// Badge (card t_f2e231d3): a status pill that cannot be painted in a marker
// colour.
//
// WHY. Status is a text label in a tinted pill, and the app had no primitive for
// it: each page picked a Tailwind palette step, which is how `bg-emerald-100`
// shipped twice on a white card without anyone measuring the label on it. Each
// tone here is a text-on-tint pair from `src/lib/designTokenRatios.ts`, and the
// danger tone is `--jf-danger-text` (#bf000f, 6.52:1 on white) rather than
// `--jf-danger` (#ef4444, 3.76:1 — a MARKER colour, never a label).

import { forwardRef } from "react";

export type BadgeTone = "neutral" | "info" | "success" | "warning" | "danger";

export type BadgeProps = React.HTMLAttributes<HTMLSpanElement> & {
  tone?: BadgeTone;
};

const TONES: Record<BadgeTone, string> = {
  neutral: "bg-[var(--jf-surface-2)] text-[var(--jf-text-secondary)] border-[var(--jf-border)]",
  info: "bg-[var(--jf-surface-2)] text-[var(--jf-info)] border-[var(--jf-border)]",
  success: "bg-[var(--jf-success-bg)] text-[var(--jf-text-secondary)] border-[var(--jf-success)]",
  warning: "bg-[var(--jf-warning-bg)] text-[var(--jf-warning-text)] border-[var(--jf-warning-bg-hover)]",
  danger: "bg-[var(--jf-danger-bg)] text-[var(--jf-danger-text)] border-[var(--jf-danger-bg)]",
};

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { tone = "neutral", className, ...props },
  ref,
) {
  return (
    <span
      {...props}
      ref={ref}
      className={[
        "inline-flex items-center gap-jf-1 rounded-jf-full border px-jf-2 py-jf-1",
        "text-jf-read-xs font-medium",
        TONES[tone],
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    />
  );
});

Badge.displayName = "Badge";
