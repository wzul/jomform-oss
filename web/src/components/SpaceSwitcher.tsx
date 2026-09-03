"use client";

import { useState } from "react";
import { useI18n } from "./i18n";
import type { SpaceRow } from "./session";

// Shared space selector rendered in BOTH the desktop sidebar and the mobile
// hamburger drawer (issue #41). A single component keeps the two surfaces
// consistent and guarantees the mobile drawer exposes the same space switch
// that Android/phone users reach on the sidebar's desktop twin.
//
// Issue #144: the selector is now ALWAYS visible — a single-space account sees
// a read-only "Space: {name}" indicator (so users know spaces exist) instead of
// nothing, and every account gets a "+" affordance that opens an inline
// create-space form (name + slug → POST /v1/spaces → reload). Multi-space
// accounts keep the dropdown switcher (unchanged).
//
// It is a controlled input: `value` is the active slug and `onSwitch(slug)` is
// Shell's switchSpace, which persists `jf_space` to localStorage and
// hard-reloads so every downstream fetch re-reads the new scope (see
// Shell.switchSpace).
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
  // Issue #164: optional default-space support. When provided, the switcher
  // shows a "make this the default" action for the active space (and marks the
  // current default). onSetDefault(spaceId) persists the choice server-side.
  defaultSpaceId?: string;
  onSetDefault?: (spaceId: string) => void;
}) {
  const { t } = useI18n();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const active = spaces.find((s) => s.slug === value) || spaces[0];
  const activeIsDefault = !!active && !!defaultSpaceId && active.id === defaultSpaceId;

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
    <div className={className}>
      <div className="flex items-center justify-between">
        <label className="text-[10px] uppercase tracking-wider text-[var(--jf-muted)]">
          {t("common.space")}
        </label>
        <button
          type="button"
          onClick={() => {
            setCreating((c) => !c);
            setError(null);
          }}
          aria-label={t("space.new")}
          title={t("space.new")}
          className="rounded p-0.5 text-xs text-[var(--jf-muted)] hover:bg-slate-100 hover:text-[var(--jf-primary)]"
        >
          +
        </button>
      </div>

      {spaces.length > 1 ? (
        <select
          value={value}
          onChange={(e) => onSwitch(e.target.value)}
          aria-label={t("common.space")}
          className="mt-0.5 w-full rounded-md border border-[var(--jf-border)] bg-white px-2 py-1 text-xs"
        >
          {spaces.map((s) => (
            <option key={s.id} value={s.slug}>
              {s.name} ({s.slug}.jomform.com)
            </option>
          ))}
        </select>
      ) : active ? (
        // Single-space indicator: read-only row so users know spaces exist.
        <div className="mt-0.5 w-full truncate rounded-md border border-[var(--jf-border)] bg-slate-50 px-2 py-1 text-xs text-[var(--jf-text)]">
          {active.name} ({active.slug}.jomform.com)
        </div>
      ) : null}

      {/* Issue #164: make the active space the default (or clear it). */}
      {onSetDefault && active && (
        <div className="mt-1">
          {activeIsDefault ? (
            <button
              type="button"
              onClick={() => onSetDefault("")}
              title={t("space.clearDefault")}
              className="w-full rounded-md border border-[var(--jf-border)] px-2 py-1 text-[11px] text-[var(--jf-muted)] hover:bg-slate-50 hover:text-red-500"
            >
              {t("space.clearDefault")}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => onSetDefault(active.id)}
              title={t("space.setDefault")}
              className="w-full rounded-md border border-[var(--jf-border)] px-2 py-1 text-[11px] text-[var(--jf-muted)] hover:bg-slate-50 hover:text-[var(--jf-primary)]"
            >
              {t("space.setDefault")}
            </button>
          )}
        </div>
      )}

      {creating && (
        <form onSubmit={createSpace} className="mt-1.5 space-y-1.5 rounded-md border border-[var(--jf-border)] bg-slate-50 p-2">
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
              onClick={() => setCreating(false)}
              className="rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs text-[var(--jf-muted)]"
            >
              {t("space.cancel")}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}
