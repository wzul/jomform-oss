"use client";

// "On this page", generated from the page's own headings (card t_45fd1bee).
//
// WHY IT READS THE DOM INSTEAD OF A PER-PAGE LIST. The 30 pages render their sections
// from data arrays (`SECTIONS.map(...)`) and their headings are literal text inside those
// maps — a server-side list would have to re-implement 30 different arrays, which is the
// hand-typed ToC the card removes. The layout wraps the page in `[data-docs-content]`; this
// component scans the h2/h3 elements that actually rendered, reuses the section anchor a
// heading already sits in (many pages put `id` on the <section>, not the heading), and
// assigns an id only where there is none — the guide markdown headings are that case.
//
// The active item follows an IntersectionObserver, so a long page highlights the section
// the reader is in. Two variants are rendered: a sticky column on xl screens and a
// collapsed block above the content on smaller ones. Only one is ever visible; each has
// its own accessible name so the two navigation landmarks cannot collide.

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";

type Heading = { id: string; text: string; level: number };

function slugify(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
}

function visibleHeadings(): { headings: Heading[]; nodes: Map<string, HTMLElement> } {
  const root = document.querySelector("[data-docs-content]");
  if (!root) return { headings: [], nodes: new Map() };
  const used = new Set<string>();
  const headings: Heading[] = [];
  const nodes = new Map<string, HTMLElement>();
  for (const element of Array.from(root.querySelectorAll<HTMLElement>("h2, h3"))) {
    const text = (element.textContent ?? "").replace(/\s+/g, " ").trim();
    if (!text) continue;
    // `data-toc-skip` (card t_ff973d0b): a heading can be real page structure and
    // still be the wrong entry for this rail. The /docs index marks its feature
    // categories (they repeat the sidebar's taxonomy, emoji and all) and its FAQ
    // questions with it, so "On this page" stays a table of contents instead of
    // becoming a second site nav. Checked BEFORE the id assignment below: a
    // skipped heading is not a rail destination, so it must not get an id minted
    // for it either.
    if (element.closest("[data-toc-skip]")) continue;
    const section = element.closest("section[id]");
    let id = element.id || (section instanceof HTMLElement ? section.id : "");
    if (!id) {
      const base = slugify(text) || `section-${headings.length + 1}`;
      id = base;
      let suffix = 2;
      while (document.getElementById(id) || used.has(id)) {
        id = `${base}-${suffix}`;
        suffix += 1;
      }
      element.id = id;
    }
    if (used.has(id)) continue;
    used.add(id);
    headings.push({ id, text, level: Number(element.tagName.slice(1)) || 2 });
    nodes.set(id, element);
  }
  return { headings, nodes };
}

function TocList({
  headings,
  active,
  onNavigate,
}: {
  headings: Heading[];
  active: string;
  onNavigate?: () => void;
}) {
  return (
    <ul className="mt-2 space-y-0.5">
      {headings.map((heading) => {
        const current = heading.id === active;
        return (
          <li key={heading.id}>
            <a
              href={`#${heading.id}`}
              aria-current={current ? "true" : undefined}
              onClick={onNavigate}
              className={[
                "block border-l-2 py-1 text-sm",
                heading.level >= 3 ? "pl-6" : "pl-3",
                current
                  ? "border-[var(--jf-primary-strong)] font-medium text-[var(--jf-primary-strong)]"
                  : "border-[var(--jf-border)] text-slate-600 hover:border-slate-600 hover:text-slate-900",
              ].join(" ")}
            >
              {heading.text}
            </a>
          </li>
        );
      })}
    </ul>
  );
}

export function DocsToc({ variant }: { variant: "sidebar" | "inline" }) {
  const pathname = usePathname() ?? "/docs";
  const [headings, setHeadings] = useState<Heading[]>([]);
  const [active, setActive] = useState("");

  useEffect(() => {
    const { headings: found, nodes } = visibleHeadings();
    setHeadings(found);
    setActive(found[0]?.id ?? "");
    if (found.length === 0) return;

    // Observe the HEADING, not the section around it: a section that wraps six subsections
    // stays on screen for the whole of them, which used to leave the first entry highlighted
    // no matter how far the reader scrolled. On any crossing, the active entry is the LAST
    // heading above the reading line (140px), which is also correct after an anchor jump.
    const pick = () => {
      let current = found[0]?.id ?? "";
      for (const heading of found) {
        const element = nodes.get(heading.id);
        if (element && element.getBoundingClientRect().top <= 140) current = heading.id;
      }
      setActive(current);
    };
    const observer = new IntersectionObserver(pick, { rootMargin: "0px 0px -55% 0px", threshold: 0 });
    for (const element of nodes.values()) observer.observe(element);
    return () => observer.disconnect();
  }, [pathname]);

  if (variant === "sidebar") {
    return (
      <nav aria-label="On this page" className="hidden w-52 shrink-0 xl:block">
        <div className="sticky top-10 max-h-[calc(100vh-5rem)] overflow-y-auto">
          {headings.length > 0 ? (
            <>
              <p className="text-jf-2xs font-semibold uppercase tracking-wide text-slate-900">
                On this page
              </p>
              <TocList headings={headings} active={active} />
            </>
          ) : null}
        </div>
      </nav>
    );
  }

  return (
    <nav aria-label="On this page — mobile" className="mb-5 xl:hidden">
      {headings.length > 0 ? (
        <details className="rounded-lg border border-[var(--jf-border)] bg-[var(--jf-surface-2)] px-3 py-1">
          {/* `py-3` (card t_ff973d0b): the mobile rail toggle was a 20px tap
              target. Padding rather than a flex box so the disclosure triangle
              Chrome draws for <summary> stays. */}
          <summary className="cursor-pointer py-3 text-sm font-semibold text-slate-900">
            On this page
          </summary>
          <TocList headings={headings} active={active} />
        </details>
      ) : null}
    </nav>
  );
}
