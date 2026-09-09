"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "./i18n";
import type { SpaceRow } from "./session";

// Compact space selector for the TOPBAR (issue #380). The old full-width
// switcher lived in the sidebar user card and pushed the nav menu down so not
// all pages were visible. This version is a small dropdown that only shows the
// active space name + a chevron; clicking opens the space list, plus the
// "make this the default" action and a "New space" item (inline create form).
// The sidebar is now nav-only.
//
// It is a controlled input: `value` is the active slug and `onSwitch(slug)` is
// Shell's switchSpace, which persists `jf_space` to localStorage and reloads
// so every downstream fetch re-reads the new scope (see Shell.switchSpace).
export function SpaceSwitcher({
  spaces,
  value,
  onSwitch,
  className,
  defaultSpaceId,
  onSetDefault,
}: {
  spaces: SpaceRow[];
  value: string;
  onSwitch: (slug: string) => void;
  className?: string;
  // Issue #164: optional default-space support. When provided, the dropdown
  // shows a "make this the default" action for the active space (and marks the
  // current default). onSetDefault(spaceId) persists the choice server-side.
  defaultSpaceId?: string;
  onSetDefault?: (spaceId: string) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  const active = spaces.find((s) => s.slug === value) || spaces[0];
  const activeIsDefault = !!active && !!defaultSpaceId && active.id === defaultSpaceId;

  // Close the dropdown on outside click or Escape.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  // Issue #163: short role label for the switcher (owner/admin → Admin,
  // manage → Manage, read_only → Read only).
  function roleLabel(role?: string): string {
    if (!role) return "";
    if (role === "owner") return t("role.owner");
    return t(`role.${role}` as any) || role;
  }

  async function createSpace(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/v1/spaces", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name.trim(), slug: slug.trim() }),
      });
      if (!res.ok) {
        let msg = t("space.createFail");
        if (res.status === 409) msg = t("space.slugTaken");
        else if (res.status === 400) msg = t("space.slugInvalid");
        setError(msg);
        setBusy(false);
        return;
      }
      // New space created — hard reload so the session bundle re-fetches the
      // updated space list and the switcher shows both spaces.
      window.location.reload();
    } catch {
      setError(t("space.createFail"));
      setBusy(false);
    }
  }

  return (
    <div ref={menuRef} className={`relative ${className || ""}`}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("common.space")}
        title={active ? `${active.name} (${active.slug}.jomform.com)` : t("common.space")}
        className="flex max-w-[180px] items-center gap-1.5 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1.5 text-xs font-medium text-[var(--jf-text)] hover:bg-slate-50"
      >
        <span className="truncate">{active ? active.name : t("common.space")}</span>
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
      </button>

      {open && (
        <div
          role="menu"
          aria-label={t("common.space")}
          className="absolute right-0 top-full z-50 mt-1 w-64 rounded-lg border border-[var(--jf-border)] bg-white p-1.5 shadow-lg"
        >
          {spaces.length > 1 ? (
            <div className="max-h-56 overflow-y-auto">
              {spaces.map((s) => (
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
            // Single-space: minimal read-only chip (just the space name).
            <div className="px-2.5 py-2 text-xs text-[var(--jf-text)]">
              <span className="block truncate font-medium">{active.name}</span>
              <span className="block truncate text-[10px] text-[var(--jf-muted)]">
                {active.slug}.jomform.com{active.role ? ` · ${roleLabel(active.role)}` : ""}
              </span>
            </div>
          ) : null}

          {/* Issue #164: make the active space the default (or clear it). */}
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
                  {t("space.clearDefault")}
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
                  {t("space.setDefault")}
                </button>
              )}
            </div>
          )}

          {/* New space — inline create form inside the dropdown. */}
          <div className="mt-1 border-t border-[var(--jf-border)] pt-1">
            {creating ? (
              <form onSubmit={createSpace} className="space-y-1.5 rounded-md bg-slate-50 p-2">
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("space.name")}
                  aria-label={t("space.name")}
                  required
                  className="w-full rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs"
                />
                <input
                  value={slug}
                  onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                  placeholder={t("space.slug")}
                  aria-label={t("space.slug")}
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
                    {busy ? "…" : t("space.create")}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setCreating(false);
                      setError(null);
                    }}
                    className="rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs text-[var(--jf-muted)]"
                  >
                    {t("space.cancel")}
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
                + {t("space.new")}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
