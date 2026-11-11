// One showcase card — the shell only.
//
// ONE ACTION, ONE GUIDE LINK (card t_516bab9d, on top of t_b4e5881d)
//   Wan, on three phone screenshots of these cards: "bahagian ni masih nampak wordy". The
//   cards carried FIVE text blocks each — heading, a 1–3 sentence lead, four tick bullets,
//   a whole "Read the full … guide with screenshots →" sentence, and a CTA button — so three
//   of the seven ran 121–138 words with a lead paragraph of 44–72 words, and the grid ranged
//   413–708px tall on a phone. Two links per card (guide line + button) also meant neither
//   was clearly the action.
//
//   Now the card has ONE filled CTA and ONE compact "Guide" text link, side by side in the
//   action row; the guide sentence is deleted, not shortened. The copy budget that keeps it
//   that way is measured by `e2e/landing-usecases-mobile.spec.ts` at 390px in all five
//   locales: heading <= 8 words, one lead sentence <= 20, four bullets <= 5 each, whole card
//   <= 55 words and <= 460px tall, every card within +/-10 words of the others.
//
// WHY THE LINK IS A PROP AND NOT A CHILD (it was a child before card t_b4e5881d)
//   `src/lib/landingUseCases.test.ts` reads `app/landing/page.tsx`'s SOURCE and pins, per
//   showcase: its title/body/bullets/cta, and that its guide link sits inside its own
//   slice of the page. Keeping the link a prop written in the page is what lets one
//   source-level guard see all seven cards without opening JSX.
//
// THE GUIDE LINK'S NAME CARRIES THE CARD (card t_516bab9d). The visible label is the same
// one word in all seven cards, so the link list a screen reader reads would otherwise be
// seven identical "Guide" links with no way to tell which guide each opens. The accessible
// name is `Guide — <card title>`: it starts with the visible text (WCAG 2.5.3), and the
// title is already on the card, so it costs the layout nothing.
//
// The frame is the reusable half a design system can absorb (card t_f2e231d3): icon tile,
// title, body, a check list, and one action row. Every colour is a `--jf-*` token and every
// space is a Tailwind step — no hex, no `[Npx]`.
export type ShowcaseCardProps = {
  /** Emoji glyph for the card's icon tile. */
  icon: string;
  title: string;
  body: string;
  /** The "what it does" checks: four, five words or fewer each. */
  bullets: readonly string[];
  /** The card's one filled action. */
  cta: { href: string; label: string };
  /**
   * The showcase's screenshot guide — a compact text link beside the CTA.
   * Optional in type only: `landingUseCases.test.ts` fails when a showcase on the landing
   * page has no guide line, because deleting the step list must never cut the reader's
   * route to the steps.
   */
  guide?: { href: string; label: string };
};

export default function ShowcaseCard({ icon, title, body, bullets, cta, guide }: ShowcaseCardProps) {
  return (
    <article className="flex flex-col rounded-2xl border border-[var(--jf-border)] bg-[var(--jf-surface)] p-5">
      <span
        aria-hidden="true"
        className="flex h-11 w-11 items-center justify-center rounded-xl bg-[var(--jf-bg)] text-xl"
      >
        {icon}
      </span>
      <h3 className="mt-3 text-base font-semibold leading-snug text-[var(--jf-text)]">{title}</h3>
      {/* `leading-snug` rather than `leading-relaxed`: the lead is one scanned sentence, and
          the tighter line box is what keeps the longest locale inside the 460px card budget
          at 390px. Contrast is unchanged (`--jf-text-secondary` on `--jf-surface`). */}
      <p className="mt-2 text-sm leading-snug text-[var(--jf-text-secondary)]">{body}</p>
      <ul className="mt-3 space-y-1">
        {bullets.map((bullet) => (
          <li key={bullet} className="flex gap-2 text-sm leading-snug text-[var(--jf-text-secondary)]">
            {/* Decorative: the list item's own text carries the meaning. */}
            <span aria-hidden="true" className="text-[var(--jf-primary-strong)]">
              ✓
            </span>
            <span>{bullet}</span>
          </li>
        ))}
      </ul>
      {/* ONE action row: the filled CTA, then the compact guide link. The guide link is a
          text link (no border, no fill) so there is exactly one button per card — the
          hierarchy card t_53bcd451 pinned for the header, applied to the showcase. */}
      <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2">
        <a
          href={cta.href}
          className="rounded-lg bg-[var(--jf-primary)] px-3.5 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          {cta.label}
        </a>
        {guide ? (
          <a
            href={guide.href}
            aria-label={`${guide.label} — ${title}`}
            className="whitespace-nowrap text-sm font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
          >
            {guide.label}
          </a>
        ) : null}
      </div>
    </article>
  );
}
