"use client";

// The ONE button primitive (card t_f2e231d3).
//
// WHY IT EXISTS. The app has 137 call sites of three hand-written classes
// (`.jf-btn-primary` / `.jf-btn-ghost` in `app/globals.css`) and no layer between
// a page and the class name, so each page re-decided padding, size, disabled
// treatment and whether the pointer even changes. This is that layer: a variant
// and a size select tokens, and a page cannot pick a fill that is not measured.
//
// WHAT IS DELIBERATELY NOT HERE. No `jf-btn*` class. Those rules are unlayered in
// `globals.css`, which means they outrank Tailwind utilities of equal specificity
// — a `sm` button built on `.jf-btn-primary` would keep the class's 10px/18px
// padding and 14px type no matter what utilities it carried (measured: the
// `jf-cta-wrap` comment in `globals.css` documents the same precedence). The
// primitive therefore styles itself from the token layer, and the legacy classes
// stay untouched for the 137 sites the migration card owns.
//
// `aria-busy` + a spinner rather than a label swap: the label stays readable (a
// screen reader does not lose the control's name mid-submit) and `disabled`
// follows `loading`, so a double press cannot fire twice. `as="a"` exists because
// a link that looks like a button must stay a link (it navigates, and it must
// keep its context menu, middle-click and `Ctrl+Enter`); it gets
// `aria-disabled` instead of `disabled`, which anchors do not support.
//
// The spinner is `animate-spin` (Tailwind's own keyframes, so nothing references
// a keyframe this bundle does not emit) and it is mounted ONLY while a request is
// in flight — not an ambient loop — and the global reduced-motion block collapses
// its iteration count to 1.

import { forwardRef } from "react";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md" | "lg";

type OwnProps = {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Busy state: `aria-busy`, a spinner, and the control is not pressable. */
  loading?: boolean;
  /** Declared here as well so both element arms carry it (anchors have no `disabled`). */
  disabled?: boolean;
  /** Render an anchor instead of a button. Everything else is identical. */
  as?: "button" | "a";
};

type ButtonElementProps = OwnProps &
  Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "children"> & { children?: React.ReactNode };
type AnchorElementProps = OwnProps &
  Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, "children"> & {
    children?: React.ReactNode;
    href: string;
  };

export type ButtonProps = ButtonElementProps | AnchorElementProps;

const BASE =
  "jf-control inline-flex items-center justify-center gap-jf-2 whitespace-nowrap font-medium " +
  "rounded-jf-md transition-colors cursor-pointer select-none " +
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--jf-focus-ring)] " +
  // DISABLED IS A NEUTRAL FILL, NOT A FADED ONE. `opacity-60` was the obvious
  // treatment and it cannot ship: it composites the fill towards the page, so a
  // danger button's white label measured 1.89:1 and axe named it — WCAG exempts
  // INACTIVE controls, but axe does not know an `aria-disabled` anchor is one, and
  // a state this page cannot show is a state nobody checks. A disabled control
  // keeps its label at `--jf-text-tertiary` on `--jf-surface-2` (5.15:1 light /
  // 7.06:1 dark, both measured rows in designTokenRatios.ts) and loses its colour
  // entirely, which reads as inactive without relying on a fade.
  "disabled:cursor-not-allowed aria-disabled:cursor-not-allowed " +
  "disabled:bg-[var(--jf-surface-2)] aria-disabled:bg-[var(--jf-surface-2)] " +
  "disabled:text-[var(--jf-text-tertiary)] aria-disabled:text-[var(--jf-text-tertiary)] " +
  "disabled:shadow-none aria-disabled:shadow-none";

// Every fill/label pair is a measured row in `src/lib/designTokenRatios.ts`; the
// white label on the danger fill is why the fill is `--jf-danger-text` (#bf000f,
// 6.42:1) and not `--jf-danger` (#ef4444, 3.76:1 — a marker, not a fill for text).
const VARIANTS: Record<ButtonVariant, string> = {
  primary:
    "bg-[var(--jf-primary)] text-[var(--jf-on-color)] " +
    "hover:bg-[var(--jf-primary-hover)] active:bg-[var(--jf-primary-active)]",
  secondary:
    "bg-[var(--jf-surface)] text-[var(--jf-text)] border border-[var(--jf-border-strong)] " +
    "hover:bg-[var(--jf-surface-2)] active:bg-[var(--jf-surface-2)]",
  ghost:
    "bg-transparent text-[var(--jf-text-secondary)] " +
    "hover:bg-[var(--jf-surface-2)] active:bg-[var(--jf-surface-2)]",
  danger:
    "bg-[var(--jf-danger-fill)] text-[var(--jf-on-color)] " +
    "hover:bg-[var(--jf-danger-fill)] active:bg-[var(--jf-danger-fill)]",
};

const SIZES: Record<ButtonSize, string> = {
  sm: "min-h-jf-8 px-jf-3 text-jf-read-xs",
  md: "min-h-jf-10 px-jf-4 py-jf-2 text-jf-read-sm",
  lg: "min-h-jf-12 px-jf-5 py-jf-2 text-jf-read-md",
};

function Spinner() {
  return (
    <span
      aria-hidden="true"
      className="h-jf-4 w-jf-4 animate-spin rounded-jf-full border-2 border-current border-t-transparent"
    />
  );
}

export const Button = forwardRef<HTMLButtonElement | HTMLAnchorElement, ButtonProps>(
  function Button(
    { variant = "primary", size = "md", loading = false, as = "button", disabled, className, children, ...props },
    ref,
  ) {
    const cls = [BASE, VARIANTS[variant], SIZES[size], className].filter(Boolean).join(" ");
    const isDisabled = Boolean(disabled) || loading;
    const body = (
      <>
        {loading ? <Spinner /> : null}
        {children}
      </>
    );

    if (as === "a") {
      const anchor = props as React.AnchorHTMLAttributes<HTMLAnchorElement>;
      return (
        <a
          {...anchor}
          ref={ref as React.Ref<HTMLAnchorElement>}
          className={cls}
          aria-busy={loading || undefined}
          aria-disabled={isDisabled || undefined}
        >
          {body}
        </a>
      );
    }

    const button = props as React.ButtonHTMLAttributes<HTMLButtonElement>;
    return (
      <button
        {...button}
        ref={ref as React.Ref<HTMLButtonElement>}
        type={button.type ?? "button"}
        className={cls}
        disabled={isDisabled}
        aria-busy={loading || undefined}
      >
        {body}
      </button>
    );
  },
);

Button.displayName = "Button";
