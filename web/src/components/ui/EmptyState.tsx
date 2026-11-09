// EmptyState (card t_f2e231d3): "nothing here" and what to do about it.
//
// WHY. Empty results were three different things in the app: a bare paragraph, a
// paragraph with a different paragraph under it, and — on three pages — nothing at
// all, so an empty list and a failed load looked the same. An empty state that
// does not say what to do next is a dead end; `action` is therefore part of the
// primitive rather than something the page bolts on.

import { forwardRef } from "react";

export type EmptyStateProps = React.HTMLAttributes<HTMLDivElement> & {
  title: string;
  /** One sentence: what is empty, and why it might be. */
  description?: string;
  /** The next step. Usually a `<Button>`. */
  action?: React.ReactNode;
};

export const EmptyState = forwardRef<HTMLDivElement, EmptyStateProps>(function EmptyState(
  { title, description, action, className, children, ...props },
  ref,
) {
  return (
    <div
      {...props}
      ref={ref}
      className={["flex flex-col items-center gap-jf-2 px-jf-6 py-jf-12 text-center", className]
        .filter(Boolean)
        .join(" ")}
    >
      <p className="text-jf-read-md font-semibold text-[var(--jf-text)]">{title}</p>
      {description ? <p className="text-jf-read-sm text-[var(--jf-muted)]">{description}</p> : null}
      {children}
      {action ? <div className="mt-jf-2">{action}</div> : null}
    </div>
  );
});

EmptyState.displayName = "EmptyState";
