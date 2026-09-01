"use client";

import { useI18n } from "./i18n";
import type { SpaceRow } from "./session";

// Shared space selector rendered in BOTH the desktop sidebar and the mobile
// hamburger drawer (issue #41). A single component keeps the two surfaces
// consistent and guarantees the mobile drawer exposes the same space switch
// that Android/phone users reach on the sidebar's desktop twin.
//
// It is only rendered when the account has more than one space — a
// single-space account sees no selector at all. It is a controlled input:
// `value` is the active slug and `onSwitch(slug)` is Shell's switchSpace,
// which persists `jf_space` to localStorage and hard-reloads so every
// downstream fetch re-reads the new scope (see Shell.switchSpace).
export function SpaceSwitcher({
  spaces,
  value,
  onSwitch,
  className,
}: {
  spaces: SpaceRow[];
  value: string;
  onSwitch: (slug: string) => void;
  className?: string;
}) {
  const { t } = useI18n();

  if (spaces.length <= 1) return null;

  return (
    <div className={className}>
      <label className="text-[10px] uppercase tracking-wider text-[var(--jf-muted)]">
        {t("common.space")}
      </label>
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
    </div>
  );
}
