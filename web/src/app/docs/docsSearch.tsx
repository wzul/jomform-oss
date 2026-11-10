"use client";

// The /docs search dialog (card t_45fd1bee).
//
// WHAT IT IS. A trigger in the top bar, a ⌘K / Ctrl-K shortcut, and a modal combobox that
// filters the BUILD-TIME index (src/lib/docsSearch.gen.json) in memory. Typing never
// issues a request: the index is a generated file bundled with the page, which is why the
// card forbids a search service and a per-keystroke fetch.
//
// THE WIRING. One instance lives in the layout's top bar. The sidebar's field is
// `<DocsNavSearch>`, which dispatches `jf:docs-search`; this component listens for it, so
// there is one dialog and one keyboard handler rather than two racing islands.
//
// ACCESSIBILITY (the card names the shape): role="dialog" + aria-modal on the panel, a
// labelled combobox input that keeps focus, `aria-activedescendant` pointing at the active
// option, a role="listbox" whose options are grouped by sidebar section with role="group",
// full arrow/Home/End/Enter/Escape handling, a Tab trap inside the dialog, and focus
// returned to the trigger on close.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useRouter } from "next/navigation";

import searchIndex from "../../lib/docsSearch.gen.json";
import {
  flattenHits,
  searchDocs,
  type SearchDoc,
  type SearchGroup,
  type SearchHit,
} from "../../lib/docsSearch";

const DOCS = (searchIndex as { docs: SearchDoc[] }).docs;

/** The event the sidebar field dispatches to open the one dialog in the top bar. */
export const DOCS_SEARCH_EVENT = "jf:docs-search";

const optionId = (index: number) => `docs-search-option-${index}`;

export function DocsSearch() {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const restoreRef = useRef<HTMLElement | null>(null);

  const openDialog = useCallback(() => {
    // Remember whoever asked (the sidebar field, the top-bar trigger, or whatever had focus
    // when ⌘K was pressed) so closing returns focus there, not to a different control.
    const active = document.activeElement;
    if (active instanceof HTMLElement) restoreRef.current = active;
    setOpen(true);
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        openDialog();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener(DOCS_SEARCH_EVENT, openDialog);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener(DOCS_SEARCH_EVENT, openDialog);
    };
  }, [openDialog]);

  const close = useCallback(() => {
    setOpen(false);
    (restoreRef.current ?? triggerRef.current)?.focus();
  }, []);

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        onClick={openDialog}
        aria-haspopup="dialog"
        aria-keyshortcuts="Meta+K Control+K"
        className="flex items-center gap-2 rounded-jf-md border border-[var(--jf-border)] bg-[var(--jf-surface)] px-3 py-1.5 text-sm text-slate-600 hover:border-[var(--jf-border-strong)] hover:text-slate-900"
      >
        <span aria-hidden="true" className="text-base leading-none">
          ⌕
        </span>
        <span>Search docs</span>
        <kbd className="hidden rounded border border-[var(--jf-border)] bg-[var(--jf-surface-2)] px-1.5 py-0.5 font-sans text-jf-2xs text-slate-600 sm:inline">
          ⌘K
        </kbd>
      </button>
      {open ? <DocsSearchDialog onClose={close} /> : null}
    </>
  );
}

function SearchOption({
  hit,
  index,
  active,
  onHover,
  onOpen,
}: {
  hit: SearchHit;
  index: number;
  active: boolean;
  onHover: () => void;
  onOpen: () => void;
}) {
  return (
    <div
      id={optionId(index)}
      role="option"
      aria-selected={active}
      onMouseMove={onHover}
      onMouseDown={(event) => event.preventDefault()}
      onClick={onOpen}
      className={[
        "cursor-pointer rounded-jf-md px-3 py-2",
        active ? "bg-[var(--jf-surface-2)]" : "bg-transparent",
      ].join(" ")}
    >
      <span className="block text-sm font-semibold text-slate-900">{hit.title}</span>
      {/* The snippet is the text that explains the match (the description when nothing in the
          prose does), so it is what the row shows. It used to be the other way round, and an
          empty-or-not description always won — the row was the same line for every query that
          matched the page, including queries whose match was a body paragraph (t_a92da047). */}
      <span className="mt-0.5 block text-xs text-slate-600">{hit.snippet || hit.description}</span>
    </div>
  );
}

function DocsSearchDialog({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const [mounted, setMounted] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);

  const groups = useMemo(() => searchDocs(DOCS, query), [query]);
  const hits = useMemo(() => flattenHits(groups), [groups]);
  const indexByPath = useMemo(() => new Map(hits.map((hit, index) => [hit.path, index])), [hits]);

  useEffect(() => {
    setMounted(true);
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, []);

  // Focus must wait for the portal: on the first commit `mounted` is still false, so the
  // input does not exist and focusing in the same effect silently left focus on the trigger
  // (arrow keys and Escape then never reached the dialog).
  useEffect(() => {
    if (mounted) inputRef.current?.focus();
  }, [mounted]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  useEffect(() => {
    if (hits.length === 0) return;
    document.getElementById(optionId(activeIndex))?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, hits]);

  const openHit = useCallback(
    (path: string) => {
      onClose();
      router.push(path);
    },
    [onClose, router],
  );

  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (hits.length === 0) return;
      event.preventDefault();
      setActiveIndex((current) =>
        event.key === "ArrowDown"
          ? (current + 1) % hits.length
          : (current - 1 + hits.length) % hits.length,
      );
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      if (hits.length === 0) return;
      event.preventDefault();
      setActiveIndex(event.key === "Home" ? 0 : hits.length - 1);
      return;
    }
    if (event.key === "Enter") {
      const hit = hits[activeIndex];
      if (hit) {
        event.preventDefault();
        openHit(hit.path);
      }
      return;
    }
    if (event.key === "Tab") {
      const focusable = dialogRef.current?.querySelectorAll<HTMLElement>("input, button");
      if (!focusable || focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
  };

  if (!mounted) return null;

  const showEmpty = query.trim().length > 0 && hits.length === 0;

  return createPortal(
    <div
      className="fixed inset-0 z-50 overflow-y-auto bg-slate-900/40 px-4 pb-8 pt-[10vh]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="docs-search-title"
        onKeyDown={onKeyDown}
        className="mx-auto w-full max-w-2xl overflow-hidden rounded-jf-lg border border-[var(--jf-border)] bg-[var(--jf-surface)] shadow-jf-3"
      >
        <h2 id="docs-search-title" className="sr-only">
          Search the documentation
        </h2>
        <div className="flex items-center gap-3 border-b border-[var(--jf-border)] px-4 py-3">
          <span aria-hidden="true" className="text-lg leading-none text-slate-600">
            ⌕
          </span>
          <input
            ref={inputRef}
            type="search"
            role="combobox"
            aria-expanded={hits.length > 0}
            aria-controls="docs-search-results"
            aria-autocomplete="list"
            aria-activedescendant={hits.length > 0 ? optionId(activeIndex) : undefined}
            aria-labelledby="docs-search-title"
            autoComplete="off"
            spellCheck={false}
            placeholder="Search pages, sections and guides…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="min-w-0 flex-1 bg-transparent text-sm text-slate-900 outline-none placeholder:text-slate-600"
          />
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-[var(--jf-border)] px-2 py-0.5 text-xs font-medium text-slate-600 hover:text-slate-900"
          >
            Esc
          </button>
        </div>

        <div
          id="docs-search-results"
          role="listbox"
          aria-label="Search results"
          className="max-h-[55vh] overflow-y-auto p-2"
        >
          {query.trim().length === 0 ? (
            <p role="status" className="px-3 py-2 text-sm text-slate-600">
              Type a word to search every page and guide.
            </p>
          ) : showEmpty ? (
            <p role="status" className="px-3 py-2 text-sm text-slate-600">
              No page matches “{query}”.
            </p>
          ) : (
            groups.map((group: SearchGroup) => (
              <div
                key={`${group.layer}-${group.section}`}
                role="group"
                aria-label={`${group.layer}: ${group.section}`}
                className="mb-1"
              >
                <p
                  aria-hidden="true"
                  className="px-2 pb-1 pt-2 text-jf-2xs font-semibold uppercase tracking-wide text-slate-600"
                >
                  {group.section}
                </p>
                {group.hits.map((hit) => {
                  const index = indexByPath.get(hit.path) ?? 0;
                  return (
                    <SearchOption
                      key={hit.path}
                      hit={hit}
                      index={index}
                      active={index === activeIndex}
                      onHover={() => setActiveIndex(index)}
                      onOpen={() => openHit(hit.path)}
                    />
                  );
                })}
              </div>
            ))
          )}
        </div>

        <p className="border-t border-[var(--jf-border)] px-4 py-2 text-xs text-slate-600">
          ↑↓ to move · Enter to open · Esc to close
        </p>
      </div>
    </div>,
    document.body,
  );
}
