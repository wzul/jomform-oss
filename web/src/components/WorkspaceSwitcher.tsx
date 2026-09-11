"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "./i18n";
import type { WorkspaceRow } from "./session";

// Compact workspace selector for the TOPBAR (issue #380). The old full-width
// switcher lived in the sidebar user card and pushed the nav menu down so not
// all pages were visible. This version is a small dropdown that only shows the
// active workspace name + a chevron; clicking opens the workspace list, plus
// the "make this the default" action and a "New workspace" item (inline create
// form).
// The sidebar is now nav-only.
//
// It is a controlled input: `value` is the active slug and `onSwitch(slug)` is
// Shell's switchWorkspace, which persists `jf_workspace` to localStorage and
// reloads so every downstream fetch re-reads the new scope (see
// Shell.switchWorkspace).
export function WorkspaceSwitcher({
  workspaces,
  value,
  onSwitch,
  className,
  fullWidth,
  defaultWorkspaceId,
  onSetDefault,
}: {
  workspaces: WorkspaceRow[];
  value: string;
  onSwitch: (slug: string) => void;
  className?: string;
  // Issue #460: when rendered in the mobile drawer the switcher is a
  // full-width section row (leading icon + "Workspace" label, name beneath,
  // chevron trailing) instead of the compact topbar chip, and its dropdown is
  // rendered in flow directly under the row so it reads as attached to the
  // trigger and is never clipped by the drawer's scroll area. Desktop topbar
  // keeps the compact chip (fullWidth omitted).
  fullWidth?: boolean;
  // Issue #164: optional default-workspace support. When provided, the
  // dropdown shows a "make this the default" action for the active workspace
  // (and marks the current default). onSetDefault(workspaceId) persists the
  // choice server-side.
  defaultWorkspaceId?: string;
  onSetDefault?: (workspaceId: string) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // Issue #460: ref to the dropdown panel itself. In the drawer (fullWidth)
  // the panel renders in flow inside the drawer's scroll body; when it opens
  // we scroll it fully into view so every item is visible/tappable at once,
  // instead of its lower rows sitting behind the drawer's user card.
  const panelRef = useRef<HTMLDivElement>(null);

  const active = workspaces.find((s) => s.slug === value) || workspaces[0];
  const activeIsDefault = !!active && !!defaultWorkspaceId && active.id === defaultWorkspaceId;

  // Close the dropdown on outside click or Escape.
  useEffect(() => {
    if (!open) return;
    // Issue #460: fullWidth (drawer) panels are in-flow, so opening one can
    // grow the scroll body and leave the panel's lower rows below the fold.
    // Bring the whole panel into view on open so every item is reachable
    // without the user having to discover the drawer's scroll.
    const scrollTimer =
      fullWidth && panelRef.current
        ? window.setTimeout(() => panelRef.current?.scrollIntoView({ block: "nearest" }), 0)
        : undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      if (scrollTimer !== undefined) window.clearTimeout(scrollTimer);
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open, fullWidth]);

  // Issue #163: short role label for the switcher (owner/admin → Admin,
  // manage → Manage, read_only → Read only).
  function roleLabel(role?: string): string {
    if (!role) return "";
    if (role === "owner") return t("role.owner");
    return t(`role.${role}` as any) || role;
  }

  async function createWorkspace(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/workspaces", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name.trim(), slug: slug.trim() }),
      });
      if (!res.ok) {
        let msg = t("workspace.createFail");
        if (res.status === 409) msg = t("workspace.slugTaken");
        else if (res.status === 400) msg = t("workspace.slugInvalid");
        setError(msg);
        setBusy(false);
        return;
      }
      // New workspace created — hard reload so the session bundle re-fetches
      // the updated workspace list and the switcher shows both workspaces.
      window.location.reload();
    } catch {
      setError(t("workspace.createFail"));
      setBusy(false);
    }
  }

  const chevron = (
    <svg
      width="12"
      height="12"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0 text-[var(--jf-muted)]"
    >
      <path d="M6 8l4 4 4-4" />
    </svg>
  );

  return (
    <div ref={menuRef} className={`relative ${fullWidth ? "w-full" : ""} ${className || ""}`}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("common.workspace")}
        title={active ? `${active.name} (${active.slug}.jomform.com)` : t("common.workspace")}
        className={
          fullWidth
            ? "flex w-full items-center justify-between gap-2 rounded-md border border-[var(--jf-border)] bg-white px-2.5 py-2 text-left text-xs font-medium text-[var(--jf-text)] hover:bg-slate-50"
            : "flex max-w-[180px] items-center gap-1.5 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1.5 text-xs font-medium text-[var(--jf-text)] hover:bg-slate-50"
        }
      >
        {fullWidth ? (
          <>
            <span aria-hidden className="shrink-0 text-[var(--jf-muted)]">
              ▦
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-[10px] font-normal uppercase tracking-wide text-[var(--jf-muted)]">
                {t("common.workspace")}
              </span>
              <span className="block truncate">{active ? active.name : t("common.workspace")}</span>
            </span>
            {chevron}
          </>
        ) : (
          <>
            <span className="truncate">{active ? active.name : t("common.workspace")}</span>
            {chevron}
          </>
        )}
      </button>

      {/* Issue #460: in the drawer the panel is rendered IN FLOW (full width,
          directly under the trigger). An absolutely-positioned panel inside
          the drawer's `overflow-y-auto` body would be clipped by the scroll
          area and its lower items unreachable on short viewports. In flow it
          is anchored to the trigger and always fully visible. Desktop keeps
          the absolute, right-aligned popover. */}
      {open && (
        <div
          ref={panelRef}
          role="menu"
          aria-label={t("common.workspace")}
          className={
            fullWidth
              ? "relative z-50 mt-1 w-full rounded-lg border border-[var(--jf-border)] bg-white p-1.5 shadow-lg"
              : "absolute right-0 top-full z-50 mt-1 w-64 rounded-lg border border-[var(--jf-border)] bg-white p-1.5 shadow-lg"
          }
        >
          {workspaces.length > 1 ? (
            <div className="max-h-56 overflow-y-auto">
              {workspaces.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    onSwitch(s.slug);
                    setOpen(false);
                  }}
                  className={`flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-2 text-left text-xs ${
                    s.slug === value
                      ? "bg-indigo-50 font-medium text-[var(--jf-primary)]"
                      : "text-[var(--jf-text)] hover:bg-slate-50"
                  }`}
                >
                  <span className="min-w-0">
                    <span className="block truncate">{s.name}</span>
                    <span className="block truncate text-[10px] text-[var(--jf-muted)]">
                      {s.slug}.jomform.com{s.role ? ` · ${roleLabel(s.role)}` : ""}
                    </span>
                  </span>
                  {s.slug === value && <span className="shrink-0 text-[var(--jf-primary)]">✓</span>}
                </button>
              ))}
            </div>
          ) : active ? (
            // Single-workspace: minimal read-only chip (just the name).
            <div className="px-2.5 py-2 text-xs text-[var(--jf-text)]">
              <span className="block truncate font-medium">{active.name}</span>
              <span className="block truncate text-[10px] text-[var(--jf-muted)]">
                {active.slug}.jomform.com{active.role ? ` · ${roleLabel(active.role)}` : ""}
              </span>
            </div>
          ) : null}

          {/* Issue #164: make the active workspace the default (or clear it). */}
          {onSetDefault && active && (
            <div className="mt-1 border-t border-[var(--jf-border)] pt-1">
              {activeIsDefault ? (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    onSetDefault("");
                    setOpen(false);
                  }}
                  className="w-full rounded-md px-2.5 py-1.5 text-left text-xs text-[var(--jf-muted)] hover:bg-slate-50 hover:text-red-500"
                >
                  {t("workspace.clearDefault")}
                </button>
              ) : (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    onSetDefault(active.id);
                    setOpen(false);
                  }}
                  className="w-full rounded-md px-2.5 py-1.5 text-left text-xs text-[var(--jf-muted)] hover:bg-slate-50 hover:text-[var(--jf-primary)]"
                >
                  {t("workspace.setDefault")}
                </button>
              )}
            </div>
          )}

          {/* New workspace — inline create form inside the dropdown. */}
          <div className="mt-1 border-t border-[var(--jf-border)] pt-1">
            {creating ? (
              <form onSubmit={createWorkspace} className="space-y-1.5 rounded-md bg-slate-50 p-2">
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("workspace.name")}
                  aria-label={t("workspace.name")}
                  required
                  className="w-full rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs"
                />
                <input
                  value={slug}
                  onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                  placeholder={t("workspace.slug")}
                  aria-label={t("workspace.slug")}
                  required
                  className="w-full rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs"
                />
                {error && <p className="text-[11px] text-red-500">{error}</p>}
                <div className="flex gap-1.5">
                  <button
                    type="submit"
                    disabled={busy}
                    className="flex-1 rounded bg-[var(--jf-primary)] px-2 py-1 text-xs font-medium text-white disabled:opacity-50"
                  >
                    {busy ? "…" : t("workspace.create")}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setCreating(false);
                      setError(null);
                    }}
                    className="rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs text-[var(--jf-muted)]"
                  >
                    {t("workspace.cancel")}
                  </button>
                </div>
              </form>
            ) : (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setCreating(true);
                  setError(null);
                }}
                className="w-full rounded-md px-2.5 py-1.5 text-left text-xs font-medium text-[var(--jf-primary)] hover:bg-indigo-50"
              >
                + {t("workspace.new")}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
