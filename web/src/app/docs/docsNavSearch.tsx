"use client";

// The sidebar's search field (card t_45fd1bee).
//
// It is a BUTTON that looks like a field: the real combobox lives in the one dialog in the
// top bar (components/docs/DocsSearch.tsx), and opening it from here dispatches the shared
// `jf:docs-search` event. Two independent dialogs mounted at once would both answer ⌘K, so
// there is exactly one — this island only asks it to open.

import { DOCS_SEARCH_EVENT } from "./docsSearch";

export function DocsNavSearch() {
  return (
    <button
      type="button"
      onClick={() => window.dispatchEvent(new Event(DOCS_SEARCH_EVENT))}
      aria-haspopup="dialog"
      aria-keyshortcuts="Meta+K Control+K"
      className="flex w-full items-center justify-between gap-2 rounded-jf-md border border-[var(--jf-border)] bg-[var(--jf-surface)] px-3 py-2 text-left text-sm text-slate-600 hover:border-[var(--jf-border-strong)] hover:text-slate-900"
    >
      <span className="flex items-center gap-2">
        <span aria-hidden="true" className="text-base leading-none">
          ⌕
        </span>
        Search docs
      </span>
      <kbd className="rounded border border-[var(--jf-border)] bg-[var(--jf-surface-2)] px-1.5 py-0.5 font-sans text-jf-2xs text-slate-600">
        ⌘K
      </kbd>
    </button>
  );
}
