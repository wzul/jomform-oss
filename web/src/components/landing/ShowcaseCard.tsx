import type { ReactNode } from "react";

// One showcase card — the shell only.
//
// WHY THE SETUP LIST IS A CHILD, NOT A PROP (card t_09f60795)
//   `src/lib/landingUseCases.test.ts` reads `app/landing/page.tsx`'s SOURCE and
//   pins, per showcase: the `land.setup` heading (exactly seven times), one
//   `<ol className="mt-3 list-decimal …">` (exactly seven), the steps in order,
//   and the guide link INSIDE the card's own slice of the file. Moving the list
//   into this component would move those strings out of the file the guard
//   reads and the whole suite would go red for a refactor that changes
//   nothing a reader sees. So the page keeps the copy it is accountable for and
//   this component owns the frame around it.
//
// The frame is the reusable half a design system can absorb (card t_f2e231d3):
// icon tile, title, body, a check list, a primary/secondary action row, and a
// slot. Every colour is a `--jf-*` token and every space is a Tailwind step —
// no hex, no `[Npx]`.
export type ShowcaseCardProps = {
  /** Emoji glyph for the card's icon tile. */
  icon: string;
  title: string;
  body: string;
  /** The "what it does" checks, rendered as a tick list. */
  bullets: readonly string[];
  cta: { href: string; label: string };
  /** The optional second action (both are real routes, never a placeholder). */
  secondary?: { href: string; label: string };
  /** The "How to set it up" block, supplied by the page. */
  children: ReactNode;
};

export default function ShowcaseCard({
  icon,
  title,
  body,
  bullets,
  cta,
  secondary,
  children,
}: ShowcaseCardProps) {
  return (
    <article className="flex flex-col rounded-2xl border border-[var(--jf-border)] bg-[var(--jf-surface)] p-5">
      <span
        aria-hidden="true"
        className="flex h-11 w-11 items-center justify-center rounded-xl bg-[var(--jf-bg)] text-xl"
      >
        {icon}
      </span>
      <h3 className="mt-3 text-base font-semibold leading-snug text-[var(--jf-text)]">{title}</h3>
      <p className="mt-2 text-sm leading-relaxed text-[var(--jf-text-secondary)]">{body}</p>
      <ul className="mt-3 space-y-1.5">
        {bullets.map((bullet) => (
          <li key={bullet} className="flex gap-2 text-sm leading-relaxed text-[var(--jf-text-secondary)]">
            {/* Decorative: the list item's own text carries the meaning. */}
            <span aria-hidden="true" className="text-[var(--jf-primary-strong)]">
              ✓
            </span>
            <span>{bullet}</span>
          </li>
        ))}
      </ul>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <a
          href={cta.href}
          className="rounded-lg bg-[var(--jf-primary)] px-3.5 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          {cta.label}
        </a>
        {secondary ? (
          <a
            href={secondary.href}
            className="rounded-lg border border-[var(--jf-border)] px-3.5 py-2 text-sm font-medium text-[var(--jf-text-secondary)] hover:bg-[var(--jf-bg)]"
          >
            {secondary.label}
          </a>
        ) : null}
      </div>
      {children}
    </article>
  );
}
