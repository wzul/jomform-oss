"use client";

// The MCP band: a full white card, copy on the left and a short terminal
// transcript on the right (variant A). The transcript is what makes the pitch
// concrete — the old block was a paragraph plus a one-line snippet.
//
// The terminal is a `--jf-ink` slab with the measured dark-surface pair from
// globals.css: body text 17.48:1 and the agent's line 11.53:1 on that surface
// (both computed from the token values, and confirmed in the browser: the
// rendered colours are rgb(169,157,255) and rgb(126,224,176) on rgb(10,15,30)).
// It is a token, not a Tailwind palette colour, so the pairing is the one
// recorded in the token block rather than an improvised one.
import { useI18n } from "../i18n";

/**
 * @param children The band's action row. It is supplied by the page because
 *   `src/lib/landingUseCases.test.ts` requires a literal `href="/mcp"` in
 *   `app/landing/page.tsx`, and a link rendered from a prop is not that shape.
 *   The frame and the transcript stay here so a design system can absorb them.
 */
export default function AgentBand({ children }: { children: React.ReactNode }) {
  const { t } = useI18n();

  // Literal keys, resolved here: `src/lib/i18nKeys.test.ts` keeps a ceiling on
  // call sites whose key the static scanner cannot name, so this is not
  // `t(turn.you)` inside the map.
  const turns = [
    { you: t("land.term1"), agent: t("land.term2") },
    { you: t("land.term3"), agent: t("land.term4") },
  ];

  return (
    <section className="mx-auto max-w-6xl px-6 py-16">
      <div className="grid gap-8 rounded-3xl border border-[var(--jf-border)] bg-[var(--jf-surface)] p-6 sm:p-10 lg:grid-cols-2 lg:items-center">
        <div>
          <h2 className="text-2xl font-bold tracking-tight text-[var(--jf-text)] sm:text-3xl">
            {t("land.mcptitle")}
          </h2>
          <p className="mt-3 text-sm leading-relaxed text-[var(--jf-text-secondary)]">
            {t("land.mcpbody")}
          </p>
          {/* The connect line: where to point the agent, and that the handshake
              is one OAuth screen. `break-all` keeps the URL inside the column
              at 320px instead of panning the page. */}
          <p className="mt-4 break-all rounded-lg bg-[var(--jf-bg)] px-3 py-2 font-mono text-jf-xs text-[var(--jf-text-secondary)]">
            {t("land.mcpsnippet")}
          </p>
          {children}
        </div>

        <div className="rounded-2xl bg-[var(--jf-ink)] p-5 font-mono text-jf-xs leading-relaxed">
          {turns.map((turn, i) => (
            <div key={turn.you} className={i === 0 ? undefined : "mt-4"}>
              <p className="text-[var(--jf-ink-accent)]">{turn.you}</p>
              <p className="mt-1 text-[var(--jf-ink-ok)]">{turn.agent}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
