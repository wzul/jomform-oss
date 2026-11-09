// The /docs prose renderer: the one place a docs page turns a body block into
// something a reader can scan.
//
// WHY IT EXISTS
//   Every /docs section used to render as `body.map(p => <p>{p}</p>)`: pure
//   paragraphs, no bold, no lists, no callouts. Wan's report was "aku rasa
//   boring. tak dak bold, tak dak emoji. rasa macam semua text" — the page was
//   correct and unreadable.
//
// WHAT A BODY BLOCK CAN BE
//   "plain paragraph, may contain **bold**, `code` and [a link](#anchor)"
//   { list: [...] }        a bullet list, each item may use the same markers
//   { steps: [...] }       a numbered list
//   { tip: "..." }         a tinted callout, one idea worth remembering
//   { note: "..." }        neutral aside
//   { warn: "..." }        something that costs money or data if missed
//
// The markers are rendered HERE. They never reach the reader, which is why the
// docs prose-convention guard (t_0447a70c) stays green: it bans a marker that
// reaches the DOM, not a marker in the source.

import { Fragment, type ReactNode } from "react";

import { parseInline, type Block, type Section } from "./docsProseText";

export type { Block, Section };


export function Inline({ text }: { text: string }): ReactNode {
  return (
    <>
      {parseInline(text).map((t, i) => {
        if (t.kind === "strong") {
          return (
            <strong key={i} className="font-semibold text-[var(--jf-text)]">
              {t.text}
            </strong>
          );
        }
        if (t.kind === "code") {
          return (
            <code
              key={i}
              className="rounded bg-[var(--jf-bg-soft)] px-1.5 py-0.5 font-mono text-[0.92em] text-[var(--jf-text)] [overflow-wrap:anywhere]"
            >
              {t.text}
            </code>
          );
        }
        if (t.kind === "link") {
          return (
            <a
              key={i}
              href={t.href}
              className="font-medium text-[var(--jf-primary-strong)] underline decoration-[var(--jf-primary-strong)]/40 underline-offset-2 hover:decoration-[var(--jf-primary-strong)]"
            >
              {t.text}
            </a>
          );
        }
        if (t.kind === "lang") {
          // Its own `lang`, so a screen reader pronounces the name in its own
          // language instead of reading 中文 with an English voice (WCAG 3.1.2).
          return (
            <span key={i} lang={t.lang}>
              {t.text}
            </span>
          );
        }
        // Plain text is NOT wrapped in a span: the docs span-wrap guard
        // (t_91474d8f) fails an unbreakable run that has no break guard, and the
        // safest way to keep that true is to add no run at all.
        return <Fragment key={i}>{t.text}</Fragment>;
      })}
    </>
  );
}

// Three components, each with LITERAL classNames. The /docs span-wrap guard
// (t_91474d8f) cannot certify a className it cannot read — it reports a template
// literal or a variable as a finding — and a literal also keeps Tailwind's
// scanner honest.
function Tip({ text }: { text: string }) {
  return (
    <div className="mt-4 flex gap-3 rounded-xl border-l-4 border-[var(--jf-primary)] bg-[var(--jf-primary-tint)] px-4 py-3">
      <span aria-hidden="true" className="text-base leading-6">
        💡
      </span>
      <div className="text-base leading-6 text-[var(--jf-text-secondary)]">
        <strong className="font-semibold text-[var(--jf-primary-strong)]">Tip: </strong>
        <Inline text={text} />
      </div>
    </div>
  );
}

function Note({ text }: { text: string }) {
  return (
    <div className="mt-4 flex gap-3 rounded-xl border-l-4 border-[var(--jf-border)] bg-[var(--jf-bg-soft)] px-4 py-3">
      <span aria-hidden="true" className="text-base leading-6">
        ℹ️
      </span>
      <div className="text-base leading-6 text-[var(--jf-text-secondary)]">
        <strong className="font-semibold text-[var(--jf-text)]">Note: </strong>
        <Inline text={text} />
      </div>
    </div>
  );
}

function Warn({ text }: { text: string }) {
  return (
    <div className="mt-4 flex gap-3 rounded-xl border-l-4 border-[var(--jf-warning-text)] bg-[var(--jf-warning-bg)] px-4 py-3">
      <span aria-hidden="true" className="text-base leading-6">
        ⚠️
      </span>
      <div className="text-base leading-6 text-[var(--jf-text-secondary)]">
        <strong className="font-semibold text-[var(--jf-warning-text)]">Careful: </strong>
        <Inline text={text} />
      </div>
    </div>
  );
}

// The PROSE CONTAINERS carry `[overflow-wrap:anywhere]` too, and that is not
// belt-and-braces: a body string can hold a bare URL or host that is NOT a
// `code` marker (customize's "… to https://mcp.jomform.com/mcp." step measured
// 309px in the 288px column at a 24px root font, which panned the page 25px at
// 320px). An unmarked run goes through Inline as a plain Fragment with no
// element of its own, so the element that decides whether it can break is the
// container. `overflow-wrap` INHERITS, so one literal token on the <p>/<ul>/<ol>
// covers every descendant run — the guard (docsProseSpanWrap.test.ts) reads these
// tags. Table cells are deliberately not listed: a table is inside an
// `overflow-x-auto` region, so a long cell scrolls inside its box instead of
// panning the document.
export function Prose({ blocks }: { blocks: Block[] }) {
  return (
    <>
      {blocks.map((b, i) => {
        if (typeof b === "string") {
          return (
            <p
              key={i}
              className="mt-3 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]"
            >
              <Inline text={b} />
            </p>
          );
        }
        if ("list" in b) {
          return (
            <ul
              key={i}
              className="mt-3 space-y-2 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]"
            >
              {b.list.map((item, j) => (
                <li key={j} className="flex gap-2.5">
                  <span aria-hidden="true" className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--jf-primary)]" />
                  <span>
                    <Inline text={item} />
                  </span>
                </li>
              ))}
            </ul>
          );
        }
        if ("steps" in b) {
          return (
            <ol
              key={i}
              className="mt-3 space-y-3 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]"
            >
              {b.steps.map((item, j) => (
                <li key={j} className="flex gap-3">
                  <span
                    aria-hidden="true"
                    className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--jf-primary)] text-xs font-semibold text-[var(--jf-on-color)]"
                  >
                    {j + 1}
                  </span>
                  <span>
                    <Inline text={item} />
                  </span>
                </li>
              ))}
            </ol>
          );
        }
        if ("heading" in b) {
          return (
            <h3
              key={i}
              className="mt-6 text-base font-semibold tracking-tight text-[var(--jf-text)] [overflow-wrap:anywhere]"
            >
              {b.heading}
            </h3>
          );
        }
        if ("code" in b) {
          return (
            <figure key={i} className="mt-3">
              <pre
                tabIndex={0}
                className="overflow-x-auto rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg-soft)] p-4 text-sm leading-6 text-[var(--jf-text)]"
              >
                <code>{b.code}</code>
              </pre>
              {b.caption ? (
                <figcaption className="mt-2 text-sm text-[var(--jf-text-secondary)]">
                  {b.caption}
                </figcaption>
              ) : null}
            </figure>
          );
        }
        if ("table" in b) {
          return (
            <div
              key={i}
              tabIndex={0}
              role="region"
              aria-label={b.table.label}
              className="mt-3 overflow-x-auto rounded-xl border border-[var(--jf-border)]"
            >
              <table className="w-full border-collapse text-left text-sm">
                <thead className="bg-[var(--jf-bg-soft)] text-[var(--jf-text)]">
                  <tr>
                    {b.table.head.map((h, j) => (
                      <th key={j} scope="col" className="px-3 py-2 font-semibold">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="text-[var(--jf-text-secondary)]">
                  {b.table.rows.map((row, j) => (
                    <tr key={j} className="border-t border-[var(--jf-border)]">
                      {row.map((cell, k) => (
                        <td key={k} className="px-3 py-2 align-top">
                          <Inline text={cell} />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          );
        }
        if ("tip" in b) return <Tip key={i} text={b.tip} />;
        if ("note" in b) return <Note key={i} text={b.note} />;
        return <Warn key={i} text={b.warn} />;
      })}
    </>
  );
}

/** A docs page header: short lead, section chips, then the sections. */
export function DocsPage({
  title,
  lead,
  summary,
  sections,
}: {
  title: string;
  lead: string;
  summary?: string[];
  sections: Section[];
}) {
  return (
    <div className="max-w-[36rem]">
      <h1 className="text-3xl font-bold tracking-tight text-[var(--jf-text)] [overflow-wrap:anywhere]">{title}</h1>
      <p className="mt-2 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]">{lead}</p>

      {summary && summary.length > 0 && (
        <ul className="mt-4 flex flex-wrap gap-2 [overflow-wrap:anywhere]">
          {summary.map((s) => (
            <li
              key={s}
              className="rounded-full border border-[var(--jf-border)] bg-[var(--jf-bg-soft)] px-3 py-1 text-xs font-medium text-[var(--jf-text-secondary)]"
            >
              {s}
            </li>
          ))}
        </ul>
      )}


      {sections.map((s) => (
        <section key={s.id} id={s.id} className="mt-10 scroll-mt-24">
          <h2 className="flex items-center gap-2 text-lg font-semibold text-[var(--jf-text)]">
            {s.icon && (
              <span aria-hidden="true" className="text-base">
                {s.icon}
              </span>
            )}
            {s.title}
          </h2>
          <Prose blocks={s.body} />
        </section>
      ))}
    </div>
  );
}
