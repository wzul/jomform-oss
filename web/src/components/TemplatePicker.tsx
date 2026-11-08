"use client";

// Issue #174: template picker modal shown when creating a new form. Mirrors
// Gravity Forms' "Explore Form Templates": a Blank Form card plus the system
// template gallery. Picking a template clones it into a new draft via
// POST /v1/templates/use and opens the builder with the cloned draft.
//
// THE PANEL ENTERS (card t_ea568318). The "New form" click used to swap the
// gallery in with no acknowledgement at all — the jump the owner reported as
// "no motion effect". The panel now carries `.jf-enter` (fade + 4px rise,
// 180ms), which the browser runs when the element is MOUNTED — i.e. exactly
// when `open` flips — and never on a re-render of the loaded gallery. The
// backdrop is deliberately static: it is the fixed positioner, and a transform
// there would become the containing block for the panel it carries.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "./i18n";
import { workspaceQuery } from "../lib/workspace";
import { apiErr } from "../lib/apiErr";
import {
  mergePickerTemplates,
  splitPickerTemplates,
  type PickerTemplate,
} from "../lib/templatePicker";

type Template = PickerTemplate;

// Issue #457: a merchant-owned template (from /v1/rest/templates). Cloned by
// id via POST /v1/templates/use {template_id}, not by gallery slug.
type OwnTemplate = PickerTemplate;

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
  const [own, setOwn] = useState<OwnTemplate[]>([]);
  const [loading, setLoading] = useState(false);
  const [using, setUsing] = useState<string | null>(null);
  const [err, setErr] = useState("");
  // Issue #589: the gallery and the merchant's own templates are two sources
  // whose queries overlap (a published template owned by the caller's account is
  // returned by both), so one forms row used to be carded twice. Assemble them
  // once, deduped by row id, and split the result so each row is drawn by
  // exactly one section.
  const { ownCards, galleryCards } = useMemo(
    () => splitPickerTemplates(mergePickerTemplates({ own, gallery: templates }), templates),
    [own, templates],
  );

  const load = useCallback(async () => {
    setLoading(true);
    setErr("");
    try {
      const [r, o] = await Promise.all([
        fetch(`/api/templates${workspaceQuery()}`),
        // Issue #457: the merchant's own templates (owner-scoped; the API
        // returns an empty list for read_only members / no workspace).
        fetch(`/api/rest/templates${workspaceQuery()}`),
      ]);
      const d = await r.json().catch(() => []);
      setTemplates(Array.isArray(d) ? d : []);
      const od = await o.json().catch(() => []);
      setOwn(Array.isArray(od) ? od : []);
    } catch {
      setTemplates([]);
      setOwn([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  if (!open) return null;

  // NOT React hooks, and they must not LOOK like ones: `rules-of-hooks` reads any
  // call whose name starts with `use` as a hook, so calling these from an onClick
  // was reported as "React Hook cannot be called inside a callback". They are
  // plain async request handlers, so the names say what they do.
  async function applyTemplate(slug: string, title: string) {
    setUsing(slug);
    setErr("");
    try {
      const r = await fetch(`/api/templates/use${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ slug, name: title }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.id) {
        setErr(apiErr(d, t("tpl.useFail"), t));
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

  // Issue #457: clone one of the merchant's own templates by id.
  async function applyOwnTemplate(id: string, title: string) {
    setUsing(id);
    setErr("");
    try {
      const r = await fetch(`/api/templates/use${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ template_id: id, name: title }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.id) {
        setErr(apiErr(d, t("tpl.useFail"), t));
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
        className="jf-enter jf-card flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden border-[var(--jf-border)]"
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
            className="flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-[var(--jf-border)] p-6 text-center transition-colors hover:border-[var(--jf-accent)]/50 hover:bg-indigo-50/40"
          >
            <span className="flex h-12 w-12 items-center justify-center rounded-full bg-indigo-50 text-2xl text-[var(--jf-primary-on-tint)]">
              +
            </span>
            <span className="text-sm font-semibold text-[var(--jf-text-secondary)]">{t("tpl.blank")}</span>
            <span className="text-xs text-[var(--jf-muted)]">{t("tpl.blankHint")}</span>
          </button>

          {/* Issue #457: the merchant's own templates come first. Issue #589:
              only own-ONLY rows here — a row the gallery also serves is rendered
              by the gallery loop below, so it is never carded twice. */}
          {ownCards.length > 0 && (
            <p className="col-span-full -mb-1 text-jf-2xs font-medium uppercase tracking-wider text-[var(--jf-muted)]">
              {t("tplmg.title")}
            </p>
          )}
          {ownCards.map((tpl) => (
            <div
              key={tpl.id}
              className="flex flex-col rounded-xl border border-[var(--jf-border)] bg-white p-4 transition-shadow hover:shadow-sm"
            >
              <div className="mb-2 flex items-center gap-2">
                <span
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg"
                  style={{ backgroundColor: tpl.theme?.primary ? tpl.theme.primary + "22" : "var(--jf-primary-tint)" }}
                >
                  🗂
                </span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-[var(--jf-text-secondary)]">{tpl.title}</p>
                </div>
              </div>
              <div className="mb-3 flex-1" />
              <button
                onClick={() => void applyOwnTemplate(tpl.id, tpl.title)}
                disabled={using !== null}
                className="jf-btn-primary w-full py-2 text-xs disabled:opacity-50"
              >
                {using === tpl.id ? t("tpl.using") : t("tpl.use")}
              </button>
            </div>
          ))}

          {galleryCards.map((tpl) => (
            <div
              key={tpl.id}
              className="flex flex-col rounded-xl border border-[var(--jf-border)] bg-white p-4 transition-shadow hover:shadow-sm"
            >
              <div className="mb-2 flex items-center gap-2">
                <span
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-lg"
                  style={{ backgroundColor: tpl.theme?.primary ? tpl.theme.primary + "22" : "var(--jf-primary-tint)" }}
                >
                  {CATEGORY_ICON[tpl.category || ""] || "📄"}
                </span>
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-[var(--jf-text-secondary)]">{tpl.title}</p>
                  {tpl.category && (
                    <p className="text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                      {tpl.category}
                    </p>
                  )}
                </div>
              </div>
              <p className="mb-3 line-clamp-2 flex-1 text-xs text-[var(--jf-muted)]">
                {tpl.description || ""}
              </p>
              <button
                onClick={() => void applyTemplate(tpl.slug, tpl.title)}
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
          {!loading && galleryCards.length === 0 && ownCards.length === 0 && (
            <p className="col-span-full py-8 text-center text-sm text-[var(--jf-muted)]">
              {t("tpl.empty")}
            </p>
          )}
        </div>

        {err && <p className="border-t border-[var(--jf-border)] px-5 py-2 text-xs text-red-700">{err}</p>}
      </div>
    </div>
  );
}
