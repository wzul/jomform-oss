// One showcase card — the shell only.
//
// NO SETUP LIST, ONE GUIDE LINK (card t_b4e5881d, reversing t_41666fb1)
//   Each card used to carry a nested box under its actions: a "How to set it up" heading,
//   a five-item ordered list and a guide link. Wan's call on a phone screenshot of the RSVP
//   card — "benda yang terlalu technical tak sesuai ada kat landing page" — so the steps
//   are off the landing page and the guide keeps them. What is left on the card is ONE
//   plain line under the tick bullets, pointing at the showcase's own /docs/guides page:
//   the landing says what the product does, the guide says how.
//
// WHY THE LINK IS A PROP AND NOT A CHILD (it was a child before this card)
//   `src/lib/landingUseCases.test.ts` reads `app/landing/page.tsx`'s SOURCE and pins, per
//   showcase: its title/body/bullets/cta, and that its guide link sits inside its own
//   slice of the page. Keeping the link a prop written in the page is what lets one
//   source-level guard see all seven cards without opening JSX.
//
// The frame is the reusable half a design system can absorb (card t_f2e231d3): icon tile,
// title, body, a check list, a guide line, a primary/secondary action row. Every colour is
// a `--jf-*` token and every space is a Tailwind step — no hex, no `[Npx]`.
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
  /**
   * The showcase's screenshot guide — one plain line under the tick bullets.
   * Optional in type only: `landingUseCases.test.ts` fails when a showcase on the landing
   * page has no guide line, because deleting the step list must never cut the reader's
   * route to the steps.
   */
  guide?: { href: string; label: string };
};

export default function ShowcaseCard({
  icon,
  title,
  body,
  bullets,
  cta,
  secondary,
  guide,
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
      {guide ? (
        <p className="mt-3 text-xs">
          <a
            href={guide.href}
            className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
          >
            {guide.label}
          </a>
        </p>
      ) : null}
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
    </article>
  );
}
