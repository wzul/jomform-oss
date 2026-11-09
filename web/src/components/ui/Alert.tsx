// Alert (card t_f2e231d3): the four message tones.
//
// WHY. The app's notices were `bg-yellow-50 border-yellow-200 text-yellow-800`
// strings written per page, and two of them had a label under 4.5:1 on their own
// tint (the amber pairs are the usual cause: `--jf-warning-bg` and the text that
// sits on it are one measurement, not two independent choices). Each tone here is
// a measured pair from `src/lib/designTokenRatios.ts`, and the glyph is
// `aria-hidden` — the sentence carries the meaning, the glyph does not repeat it
// to a screen reader.
//
// `role`: a warning or a danger is announced (`alert`); an info or a success is
// read when reached (`status`), because a page that shouts "saved" at every load
// is noise.

import { forwardRef } from "react";

export type AlertTone = "info" | "success" | "warning" | "danger";

export type AlertProps = React.HTMLAttributes<HTMLDivElement> & {
  tone?: AlertTone;
  /** The message's own heading, optional. */
  title?: string;
};

const TONES: Record<AlertTone, { box: string; glyph: string; role: "alert" | "status" }> = {
  info: {
    box: "bg-[var(--jf-surface-2)] border-[var(--jf-border)] text-[var(--jf-text-secondary)]",
    glyph: "i",
    role: "status",
  },
  success: {
    box: "bg-[var(--jf-success-bg)] border-[var(--jf-border)] text-[var(--jf-text-secondary)]",
    glyph: "✓",
    role: "status",
  },
  warning: {
    box: "bg-[var(--jf-warning-bg)] border-[var(--jf-warning-bg-hover)] text-[var(--jf-warning-text)]",
    glyph: "!",
    role: "alert",
  },
  danger: {
    box: "bg-[var(--jf-danger-bg)] border-[var(--jf-danger-bg)] text-[var(--jf-danger-text)]",
    glyph: "×",
    role: "alert",
  },
};

export const Alert = forwardRef<HTMLDivElement, AlertProps>(function Alert(
  { tone = "info", title, className, children, ...props },
  ref,
) {
  const t = TONES[tone];
  return (
    <div
      {...props}
      ref={ref}
      role={t.role}
      className={[
        "flex gap-jf-3 rounded-jf-md border px-jf-4 py-jf-3 text-jf-read-sm",
        t.box,
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <span aria-hidden="true" className="font-semibold">
        {t.glyph}
      </span>
      <div className="flex flex-col gap-jf-1">
        {title ? <p className="font-semibold">{title}</p> : null}
        {children}
      </div>
    </div>
  );
});

Alert.displayName = "Alert";
