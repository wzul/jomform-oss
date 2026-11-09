"use client";

// Tabs (card t_f2e231d3): the tablist with a ROVING TABINDEX.
//
// WHY THIS IS NOT THREE PILLS AND A `useState`. The app's tab strips were a row of
// buttons plus a state variable, and the accessibility contract was rebuilt by
// hand at each site. Measured on this tree, the two defects that followed were
// both structural:
//
//   * `role="tab"` without a `role="tablist"` parent (`/profile` and `/settings`
//     carried 15 critical `aria-required-parent` nodes between them, axe-green
//     because no gate scanned those routes);
//   * every tab in the Tab ORDER, so a keyboard user had to press Tab once per
//     tab to get past a six-tab strip. WAI-ARIA's tabs pattern is a roving
//     tabindex: exactly ONE tab is tabbable (the selected one) and the arrows move
//     within the strip.
//
// This component is that pattern: `role="tablist"` on the row, `role="tab"` +
// `aria-selected` + `aria-controls` per tab, `role="tabpanel"` +
// `aria-labelledby` on the panel, `tabIndex={selected ? 0 : -1}`, and
// ArrowLeft/ArrowRight/Home/End with automatic activation (moving to a tab
// selects it, which is the automatic-activation variant the pattern allows and
// what the app's strips already did on click).
//
// The panel is passed IN (`items[i].panel`) rather than as children: a function
// child or a render prop cannot cross a Server Component boundary, and every page
// that will adopt this is a server component. Panels are mounted only for the
// selected tab — a strip of six panels, each with its own fetch, is not something
// a page wants to render to show one.

import { forwardRef, useId, useRef, useState } from "react";

export type TabItem = {
  id: string;
  label: string;
  panel: React.ReactNode;
  disabled?: boolean;
};

export type TabsProps = {
  /** The accessible name of the strip, e.g. "Settings sections". */
  label: string;
  items: readonly TabItem[];
  /** The tab selected on first render. Defaults to the first enabled one. */
  defaultId?: string;
  className?: string;
  panelClassName?: string;
};

export const Tabs = forwardRef<HTMLDivElement, TabsProps>(function Tabs(
  { label, items, defaultId, className, panelClassName },
  ref,
) {
  const base = useId();
  const firstEnabled = items.find((i) => !i.disabled)?.id;
  const [selected, setSelected] = useState(defaultId ?? firstEnabled ?? items[0]?.id);
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});

  const enabled = items.filter((i) => !i.disabled);
  const current = items.find((i) => i.id === selected) ?? enabled[0];

  function move(to: string) {
    setSelected(to);
    tabRefs.current[to]?.focus();
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (enabled.length === 0) return;
    const at = enabled.findIndex((i) => i.id === current?.id);
    let next: TabItem | undefined;
    if (event.key === "ArrowRight") next = enabled[(at + 1) % enabled.length];
    else if (event.key === "ArrowLeft") next = enabled[(at - 1 + enabled.length) % enabled.length];
    else if (event.key === "Home") next = enabled[0];
    else if (event.key === "End") next = enabled[enabled.length - 1];
    if (!next) return;
    // The arrows are ours: without this the page scrolls under the strip while a
    // keyboard user walks it.
    event.preventDefault();
    move(next.id);
  }

  return (
    <div ref={ref} className={className}>
      <div
        role="tablist"
        aria-label={label}
        onKeyDown={onKeyDown}
        className="jf-tabstrip flex flex-wrap gap-jf-1 border-b border-[var(--jf-border)]"
      >
        {items.map((item) => {
          const isSelected = item.id === current?.id;
          return (
            <button
              key={item.id}
              ref={(el) => {
                tabRefs.current[item.id] = el;
              }}
              type="button"
              role="tab"
              id={`${base}-tab-${item.id}`}
              aria-selected={isSelected}
              aria-controls={`${base}-panel-${item.id}`}
              tabIndex={isSelected ? 0 : -1}
              disabled={item.disabled}
              onClick={() => setSelected(item.id)}
              className={[
                "rounded-t-jf-md px-jf-3 py-jf-2 text-jf-read-sm transition-colors",
                "disabled:cursor-not-allowed disabled:opacity-60",
                isSelected
                  ? "border-b-2 border-[var(--jf-primary)] font-semibold text-[var(--jf-primary-strong)]"
                  : "border-b-2 border-transparent text-[var(--jf-muted)] hover:text-[var(--jf-text)]",
              ].join(" ")}
            >
              {item.label}
            </button>
          );
        })}
      </div>
      {current ? (
        <div
          role="tabpanel"
          id={`${base}-panel-${current.id}`}
          aria-labelledby={`${base}-tab-${current.id}`}
          tabIndex={0}
          className={["pt-jf-4", panelClassName].filter(Boolean).join(" ")}
        >
          {current.panel}
        </div>
      ) : null}
    </div>
  );
});

Tabs.displayName = "Tabs";
