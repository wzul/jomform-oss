"use client";

// Issue #486: product picker for the restaurant-menu settings (menu categories
// + best-seller). It replaces the raw product-ID text inputs the merchant used
// to paste UUIDs into — the same discoverability fix the media picker (#391)
// and the template picker (#174) already applied elsewhere.
//
// It lists the workspace's ACTIVE products (name + thumbnail + price) with a
// search box, multi-select for a menu category and single-select for the
// best-seller. It returns product IDs only, so the persisted menu JSON shape
// (categories[].product_ids: string[], best_seller: string) is unchanged and
// no backend/migration change is needed.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "./i18n";
import { workspaceQuery } from "../lib/workspace";

export type PickerProduct = {
  id: string;
  name: string;
  price_sen: number;
  images?: string[];
  active: boolean;
};

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

export default function ProductPicker({
  open,
  onClose,
  onConfirm,
  selected,
  multi,
  title,
  onlyIds,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: (ids: string[]) => void;
  selected: string[];
  multi: boolean;
  title: string;
  // Issue #497: restrict the picker to a fixed set of product IDs (the
  // products a menu form references via its own product blocks). When set, an
  // account product the form does not reference can never be picked. Omit for
  // the unrestricted (workspace-wide) behaviour.
  onlyIds?: string[];
}) {
  const { t } = useI18n();
  const [items, setItems] = useState<PickerProduct[]>([]);
  const [loading, setLoading] = useState(false);
  const [picked, setPicked] = useState<string[]>([]);
  const [q, setQ] = useState("");

  // Same workspace-scoped products endpoint the products page uses, so the
  // picker always shows the ACTIVE workspace's active products.
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await fetch(`/api/rest/products${workspaceQuery()}`);
      const d = r.ok ? await r.json() : [];
      let list: PickerProduct[] = Array.isArray(d) ? d.filter((p: PickerProduct) => p.active !== false) : [];
      // Issue #497: when the caller scopes the picker (a menu form's products),
      // keep only the allowed IDs and preserve that order.
      if (onlyIds) {
        const byId = new Map(list.map((p) => [p.id, p]));
        list = onlyIds.map((id) => byId.get(id)).filter((p): p is PickerProduct => !!p);
      }
      setItems(list);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) {
      setPicked(selected);
      setQ("");
      load();
    }
    // `selected` is deliberately not a dependency: the picker seeds its working
    // selection when it opens, not on every parent re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, load]);

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return items;
    return items.filter((p) => p.name.toLowerCase().includes(needle));
  }, [items, q]);

  if (!open) return null;

  function toggle(id: string) {
    if (multi) {
      setPicked((arr) => (arr.includes(id) ? arr.filter((x) => x !== id) : [...arr, id]));
    } else {
      setPicked([id]);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl overflow-hidden rounded-xl bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-4 py-3">
          <h3 className="text-sm font-semibold">{title}</h3>
          <button
            onClick={onClose}
            className="text-[var(--jf-muted)] hover:text-slate-600"
            aria-label={t("set.menuPickerCancel")}
          >
            ✕
          </button>
        </div>

        <div className="p-4">
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("set.menuPickerSearch")}
            className="jf-input w-full text-sm"
          />

          <div className="mt-3 max-h-80 overflow-y-auto">
            {loading ? (
              <p className="py-6 text-center text-sm text-[var(--jf-muted)]">{t("set.menuPickerLoading")}</p>
            ) : items.length === 0 ? (
              <p className="py-6 text-center text-sm text-[var(--jf-muted)]">
                {t("set.menuPickerNoProducts")}
              </p>
            ) : filtered.length === 0 ? (
              <p className="py-6 text-center text-sm text-[var(--jf-muted)]">
                {t("set.menuPickerEmpty")}
              </p>
            ) : (
              <ul className="space-y-1.5">
                {filtered.map((p) => {
                  const on = picked.includes(p.id);
                  return (
                    <li key={p.id}>
                      <button
                        type="button"
                        onClick={() => toggle(p.id)}
                        aria-pressed={on}
                        className={`flex w-full items-center gap-3 rounded-lg border p-2 text-left transition-colors ${
                          on
                            ? "border-[var(--jf-accent)] bg-[var(--jf-accent)]/5 ring-1 ring-[var(--jf-accent)]/40"
                            : "border-[var(--jf-border)] hover:border-[var(--jf-accent)]"
                        }`}
                      >
                        <span
                          className={`flex h-4 w-4 shrink-0 items-center justify-center border text-jf-3xs ${
                            multi ? "rounded" : "rounded-full"
                          } ${
                            on
                              ? "border-[var(--jf-primary)] bg-[var(--jf-primary)] text-white"
                              : "border-slate-300 bg-white text-transparent"
                          }`}
                          aria-hidden
                        >
                          ✓
                        </span>
                        <span className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-md bg-slate-100">
                          {p.images?.[0] ? (
                            /* eslint-disable-next-line @next/next/no-img-element */
                            <img
                              src={p.images[0]}
                              alt={p.name}
                              className="h-full w-full object-cover"
                              loading="lazy"
                            />
                          ) : (
                            <span className="text-jf-3xs text-[var(--jf-muted-on-tint)]">—</span>
                          )}
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium text-slate-800">
                            {p.name}
                          </span>
                          <span className="block text-xs font-semibold text-[var(--jf-muted)]">
                            {money(p.price_sen)}
                          </span>
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--jf-border)] px-4 py-3">
          <span className="text-xs text-[var(--jf-muted)]">
            {t("set.menuPickerSelected", { n: picked.length })}
          </span>
          <div className="flex items-center gap-2">
            {multi && (
              <button
                type="button"
                onClick={() => setPicked([])}
                className="jf-btn-ghost"
              >
                {t("set.menuPickerClear")}
              </button>
            )}
            <button type="button" onClick={onClose} className="jf-btn-ghost">
              {t("set.menuPickerCancel")}
            </button>
            <button
              type="button"
              onClick={() => onConfirm(picked)}
              className="jf-btn-primary px-3 py-1.5 text-xs"
            >
              {t("set.menuPickerConfirm")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
