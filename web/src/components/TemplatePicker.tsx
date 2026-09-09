"use client";

// Issue #174: template picker modal shown when creating a new form. Mirrors
// Gravity Forms' "Explore Form Templates": a Blank Form card plus the system
// template gallery. Picking a template clones it into a new draft via
// POST /v1/templates/use and opens the builder with the cloned draft.

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "./i18n";
import { spaceQuery } from "../lib/space";

type Template = {
  id: string;
  slug: string;
  title: string;
  description?: string;
  category?: string;
  mode?: string;
  theme?: { primary?: string };
};

const CATEGORY_ICON: Record<string, string> = {
  "Food & Beverage": "🍽",
  Booking: "📅",
  Education: "🎓",
  Events: "🎟",
  "Non-profit": "🤝",
  eCommerce: "🛒",
  Contact: "✉",
  Marketing: "📣",
  Feedback: "📊",
  Sales: "💬",
  HR: "👔",
  Gifts: "🎁",
  Contests: "🏆",
};

export default function TemplatePicker({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const router = useRouter();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(false);
  const [using, setUsing] = useState<string | null>(null);
  const [err, setErr] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setErr("");
    try {
      const r = await fetch(`/api/templates${spaceQuery()}`);
      const d = await r.json().catch(() => []);
      setTemplates(Array.isArray(d) ? d : []);
    } catch {
      setTemplates([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  if (!open) return null;

  async function useTemplate(slug: string, title: string) {
    setUsing(slug);
    setErr("");
    try {
      const r = await fetch(`/api/templates/use${spaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ slug, name: title }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.id) {
        setErr(d.error || t("tpl.useFail"));
        return;
      }
      onClose();
      router.push(`/builder?form=${d.id}`);
    } catch {
      setErr(t("tpl.useFail"));
    } finally {
      setUsing(null);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={t("tpl.title")}
    >
      <div
        className="jf-card flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden border-[var(--jf-border)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold tracking-tight">{t("tpl.title")}</h2>
            <p className="text-sm text-[var(--jf-muted)]">{t("tpl.subtitle")}</p>
          </div>
          <button
            onClick={onClose}
            aria-label={t("tpl.close")}
            className="rounded-md px-2.5 py-1.5 text-lg text-[var(--jf-muted)] hover:bg-slate-100"
          >
            ✕
          </button>
        </div>

        <div className="grid flex-1 grid-cols-1 gap-3 overflow-y-auto p-5 sm:grid-cols-2 lg:grid-cols-3">
          {/* Blank form */}
          <button
            onClick={() => {
              onClose();
              router.push("/builder?new=1");
            }}
            className="flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-[var(--jf-border)] p-6 text-center transition-colors hover:border-[#7170ff]/50 hover:bg-indigo-50/40"
          >
            <span className="flex h-12 w-12 items-center justify-center rounded-full bg-indigo-50 text-2xl text-[var(--jf-primary)]">
              +
            </span>
            <span className="text-sm font-semibold text-[#374151]">{t("tpl.blank")}</span>
            <span className="text-xs text-[var(--jf-muted)]">{t("tpl.blankHint")}</span>
          </button>

          {templates.map((tpl) => (
            <div
              key={tpl.id}
              className="flex flex-col rounded-xl border border-[var(--jf-border)] bg-white p-4 transition-shadow hover:shadow-sm"
            >
              <div className="mb-2 flex items-center gap-2">
                <span
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg"
                  style={{ backgroundColor: (tpl.theme?.primary || "#5e6ad2") + "22" }}
                >
                  {CATEGORY_ICON[tpl.category || ""] || "📄"}
                </span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-[#374151]">{tpl.title}</p>
                  {tpl.category && (
                    <p className="text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                      {tpl.category}
                    </p>
                  )}
                </div>
              </div>
              <p className="mb-3 line-clamp-2 flex-1 text-xs text-[var(--jf-muted)]">
                {tpl.description || ""}
              </p>
              <button
                onClick={() => void useTemplate(tpl.slug, tpl.title)}
                disabled={using !== null}
                className="jf-btn-primary w-full py-2 text-xs disabled:opacity-50"
              >
                {using === tpl.slug ? t("tpl.using") : t("tpl.use")}
              </button>
            </div>
          ))}

          {loading && (
            <p className="col-span-full py-8 text-center text-sm text-[var(--jf-muted)]">…</p>
          )}
          {!loading && templates.length === 0 && (
            <p className="col-span-full py-8 text-center text-sm text-[var(--jf-muted)]">
              {t("tpl.empty")}
            </p>
          )}
        </div>

        {err && <p className="border-t border-[var(--jf-border)] px-5 py-2 text-xs text-red-500">{err}</p>}
      </div>
    </div>
  );
}
