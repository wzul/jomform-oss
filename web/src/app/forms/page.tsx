"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { PageLoading } from "../../components/PageLoading";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { formEntriesAction } from "../../lib/formMode";
import { formViewAction } from "../../lib/formPublicURL";
import { acknowledgmentBody, needsProductsIDs, outcomeMessage, publishConfirmText, publishOutcome } from "../../lib/publishResult";
import TemplatePicker from "../../components/TemplatePicker";

type FormRow = {
  id: string;
  slug: string;
  title: string;
  status: string;
  url: string;
  // "payment" (default) or "response". A response-only form
  // (RSVP, contact, event signup) can never record a sale, so it must not be
  // offered the "Sales" action.
  mode?: string;
};

// A row in the Trash view. `can_purge` is false when the form has sales —
// sales history is never destroyed, so a permanent delete must not be offered
// for it (the server refuses the same way).
type TrashedRow = {
  id: string;
  slug: string;
  title: string;
  deleted_at: string;
  sales_count: number;
  can_purge: boolean;
};

const HOST = typeof window !== "undefined" ? window.location.hostname : "";
// Subdomain URL when app itself runs on a subdomain? Dashboard always apex.
const subdomainBase = () => {
  const parts = window.location.hostname.split(".");
  if (parts.length >= 2) {
    const root = parts.slice(-2).join(".");
    return `https://<account>.${root}`;
  }
  return "";
};

// Issue #450: build the public form URL from the API's `url` field (which
// resolves the owning workspace slug / custom domain server-side) — never from
// a client-side localStorage fallback. A hardcoded "app" fallback produced
// https://app.jomform.com/{slug}, which 404s. The fallback here is only used
// when the API did not return a url (older payload).
const workspaceHost = (() => {
  if (typeof window === "undefined") return "";
  try {
    const slug = localStorage.getItem("jf_workspace") || "";
    return slug ? `https://${slug}.jomform.com` : "";
  } catch {
    return "";
  }
})();

export default function FormsPage() {
  const { t } = useI18n();
  const { anon, role, workspace } = useSession();
  const router = useRouter();
  const [forms, setForms] = useState<FormRow[]>([]);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  // Trash view: a second, explicitly-selected view over the SAME page. The
  // default is the live list — a new visitor must never land in the trash.
  const [view, setView] = useState<"active" | "trash">("active");
  const [trashed, setTrashed] = useState<TrashedRow[]>([]);
  const [trashLoaded, setTrashLoaded] = useState(false);
  const [notify, setNotify] = useState("");
  const [confirmPurge, setConfirmPurge] = useState<TrashedRow | null>(null);
  const [purgeTyped, setPurgeTyped] = useState("");
  const [purgeBusy, setPurgeBusy] = useState(false);

  // Issue #163: read_only members see no write actions (new/edit/publish/
  // delete). manage + admin keep full write access.
  const canWrite = role !== "read_only";

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    // Issue #29: no serial `await me → fetch` gate — the list fetch runs
    // immediately; a 401 flips `anon` and the auth bounce below handles it.
    // Issue #503: a non-ok response used to become an EMPTY list with no error
    // (`r.ok ? r.json() : []`), so a 401/500 was indistinguishable from "no
    // forms". Route it through the shared helper: 401/403 → /login, anything
    // else → a visible message.
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/forms${workspaceQuery()}`);
        return Array.isArray(d) ? (d as FormRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("forms.loadFail") },
    );
    if (!r.ok) {
      setForms([]);
      setErr(r.error);
      setChecking(false);
      return;
    }
    setForms(r.data);
    setSelected(new Set());
    setErr("");
    setChecking(false);
  }, [t, goToLogin]);

  // Issue #170: re-fetch the list whenever the active space changes (keyed on
  // useSession().workspace), so switching workspaces shows the new forms
  // without a manual refresh.
  useEffect(() => {
    load();
  }, [load, workspace]);

  // The trash is fetched only when it is opened. It is a separate listing
  // (GET /api/rest/forms/trash) because the normal list deliberately excludes
  // deleted rows — a form must never reappear in the live list by accident.
  const loadTrash = useCallback(async () => {
    const r = await runLoad(
      async () => {
        const d = await fetchJson<unknown>(`/api/rest/forms/trash${workspaceQuery()}`);
        return Array.isArray(d) ? (d as TrashedRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("forms.loadTrashFail") },
    );
    if (!r.ok) {
      setTrashed([]);
      setErr(r.error);
      return;
    }
    setTrashed(r.data);
    setTrashLoaded(true);
    setErr("");
  }, [t, goToLogin]);

  useEffect(() => {
    if (view === "trash") void loadTrash();
  }, [view, loadTrash, workspace]);

  // Escape closes the purge dialog.
  useEffect(() => {
    if (!confirmPurge) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setConfirmPurge(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [confirmPurge]);

  async function restore(id: string) {
    setNotify("");
    const res = await fetch(`/api/rest/forms/${id}/restore${workspaceQuery()}`, {
      method: "POST",
    });
    if (!res.ok) {
      // 409 means the slug was re-claimed while the form sat in the trash —
      // the refusal says which obstacle to clear, so show it verbatim rather
      // than a generic failure.
      const d = await res.json().catch(() => null);
      setErr(d?.error || t("forms.restoreFail"));
      void loadTrash();
      return;
    }
    setNotify(t("forms.restored"));
    void loadTrash();
    void load();
  }

  async function purge() {
    if (!confirmPurge || purgeBusy) return;
    if (purgeTyped.trim() !== confirmPurge.slug) return;
    setPurgeBusy(true);
    setNotify("");
    try {
      const res = await fetch(`/api/rest/forms/${confirmPurge.id}/purge${workspaceQuery()}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm_slug: purgeTyped.trim() }),
      });
      if (!res.ok) {
        const d = await res.json().catch(() => null);
        setErr(d?.error || t("forms.purgeFail"));
        setConfirmPurge(null);
        setPurgeTyped("");
        void loadTrash();
        return;
      }
      setNotify(t("forms.purged"));
      setConfirmPurge(null);
      setPurgeTyped("");
      void loadTrash();
    } catch {
      setErr(t("forms.purgeFail"));
    } finally {
      setPurgeBusy(false);
    }
  }

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function publish(id: string) {
    const r = await fetch(`/api/rest/forms/${id}/publish${workspaceQuery()}`, { method: "POST" });
    if (r.ok) {
      setErr("");
      load();
      return;
    }
    // Onboarding gap: a payment form with nothing to sell is refused once
    // (409, code=no_products) rather than published silently. Ask the merchant
    // to confirm; on confirm the same call is re-sent acknowledged.
    const d = await r.json().catch(() => ({}));
    const outcome = publishOutcome({ ok: false, status: r.status, body: d }, "");
    if (outcome.kind !== "needs_ack") {
      setErr(outcomeMessage(outcome) || t("forms.loadFail"));
      return;
    }
    // The refusal's text is localized (the API's own prose for it is Malay and
    // the API never receives the caller's language) — see lib/publishResult.ts.
    if (!window.confirm(publishConfirmText(outcome, t))) return;
    const retry = await fetch(`/api/rest/forms/${id}/publish${workspaceQuery()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(acknowledgmentBody()),
    });
    if (!retry.ok) {
      const rd = await retry.json().catch(() => ({}));
      const retryOutcome = publishOutcome({ ok: false, status: retry.status, body: rd }, "");
      setErr(outcomeMessage(retryOutcome) || t("forms.loadFail"));
      return;
    }
    setErr("");
    load();
  }

  const ids = Array.from(selected);
  const allSelected = forms.length > 0 && selected.size === forms.length;

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected(allSelected ? new Set() : new Set(forms.map((f) => f.id)));
  }

  async function bulk(action: "publish" | "draft" | "delete") {
    if (ids.length === 0 || busy) return;
    if (action === "delete") {
      const ok = window.confirm(
        t("forms.bulkDeleteConfirm", { n: ids.length })
      );
      if (!ok) return;
    }
    setBusy(true);
    try {
      const send = (ack: boolean) =>
        fetch(`/api/rest/forms/bulk${workspaceQuery()}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ action, ids, ...(ack ? acknowledgmentBody() : {}) }),
        });
      const r = await send(false);
      if (!r.ok) {
        // Onboarding gap: a bulk publish holds back any payment form with
        // nothing to sell (409, code=no_products). Confirm once, then re-send
        // the same batch acknowledged — the ids are named so the merchant knows
        // which forms are the problem.
        const d = await r.json().catch(() => ({}));
        const outcome = publishOutcome({ ok: false, status: r.status, body: d }, "");
        if (outcome.kind !== "needs_ack") {
          setErr(outcomeMessage(outcome) || t("forms.loadFail"));
          return;
        }
        const named = needsProductsIDs(d).length;
        // Both branches are localized: the named count is its own dictionary
        // entry, and the fallback is the same warning sentence the single-form
        // path uses. The response's prose is never read (see the module doc).
        const ask =
          named > 0
            ? t("forms.bulkNoProductsConfirm", { n: named })
            : publishConfirmText(outcome, t);
        if (!window.confirm(ask)) return;
        const retry = await send(true);
        if (!retry.ok) {
          const rd = await retry.json().catch(() => ({}));
          const ro = publishOutcome({ ok: false, status: retry.status, body: rd }, "");
          setErr(outcomeMessage(ro) || t("forms.loadFail"));
          return;
        }
      }
      setErr("");
    } finally {
      setBusy(false);
      load();
    }
  }

  // Issue #503: a failed load must be visible and actionable — never an empty
  // list that reads as "you have no forms".
  // The in-flight state renders INSIDE the shell. It used to be a bare
  // `<main>` with no Shell around it, which unmounted the sidebar and the top
  // bar for the duration of the fetch — reading as a full page reload.
  if (checking) return <PageLoading active="forms" />;

  if (err) {
    return (
      <Shell active="forms">
        <div className="w-full flex-1 px-6 py-8">
          <div role="alert" className="jf-card flex flex-col items-start gap-3 p-6">
            <p className="text-sm text-red-600">{err}</p>
            <button type="button" onClick={() => void load()} className="jf-btn-ghost px-3 py-1.5 text-xs">
              {t("common.retry")}
            </button>
          </div>
        </div>
      </Shell>
    );
  }

  return (
    <Shell active="forms">
      <TemplatePicker open={pickerOpen} onClose={() => setPickerOpen(false)} />
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("forms.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("forms.subtitle")}</p>
          </div>
          {canWrite && (
            <button
              onClick={() => setPickerOpen(true)}
              className="jf-btn-primary shrink-0 self-start sm:self-auto"
            >
              {t("forms.new")}
            </button>
          )}
        </div>

        {/* Active / Trash switcher. The live list is the DEFAULT: a fresh
            visitor must never land in the trash. */}
        <div className="mt-5 flex flex-wrap items-center gap-1 border-b border-[var(--jf-border)]" role="tablist">
          {(["active", "trash"] as const).map((v) => (
            <button
              key={v}
              type="button"
              role="tab"
              aria-selected={view === v}
              onClick={() => {
                setView(v);
                setErr("");
                setNotify("");
              }}
              className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium ${
                view === v
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-[var(--jf-muted)] hover:text-slate-800"
              }`}
            >
              {v === "active" ? t("forms.trashTab") : t("forms.trashTabDeleted")}
              {v === "trash" && trashLoaded && trashed.length > 0 ? ` (${trashed.length})` : ""}
            </button>
          ))}
        </div>

        {notify && (
          <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{notify}</p>
        )}

        {view === "trash" && (
          <>
            <p className="mt-4 max-w-2xl text-sm text-[var(--jf-muted)]">{t("forms.trashNote")}</p>
            <ul className="mt-4 space-y-2">
              {trashed.map((f) => (
                <li
                  key={f.id}
                  className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{f.title}</p>
                    <p className="font-mono text-[11px] text-[var(--jf-muted)] break-all">{f.slug}</p>
                    {!f.can_purge && (
                      <p className="mt-1 text-[11px] text-amber-700">
                        {t("forms.canPurgeFalse")} ({f.sales_count})
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                    {canWrite && (
                      <>
                        <button
                          type="button"
                          onClick={() => void restore(f.id)}
                          className="jf-btn-ghost min-h-[44px] sm:min-h-10"
                        >
                          {t("forms.restore")}
                        </button>
                        {/* Purge is withheld when the form has sales: the server
                            refuses it, so offering the button would only invite a
                            failed click. */}
                        {f.can_purge && (
                          <button
                            type="button"
                            onClick={() => {
                              setConfirmPurge(f);
                              setPurgeTyped("");
                              setErr("");
                            }}
                            className="jf-btn-ghost min-h-[44px] text-rose-600 hover:bg-rose-50 sm:min-h-10"
                          >
                            {t("forms.purge")}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </li>
              ))}
              {trashed.length === 0 && (
                <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("forms.trashEmpty")}</li>
              )}
            </ul>
          </>
        )}

        {view === "active" && (
        <>
        {canWrite && ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 border-[var(--jf-border)] px-4 py-3">
            <span className="text-sm font-medium">
              {t("forms.nSelected", { n: ids.length })}
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={() => bulk("publish")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs disabled:opacity-50"
              >
                {t("builder.publish")}
              </button>
              <button
                onClick={() => bulk("draft")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs"
              >
                {t("forms.draft")}
              </button>
              <button
                onClick={() => bulk("delete")}
                disabled={busy}
                className="jf-btn-ghost px-3 py-1.5 text-xs text-rose-600 hover:bg-rose-50"
              >
                {t("forms.delete")}
              </button>
            </div>
          </div>
        )}

        <ul className="mt-6 space-y-2">
          {forms.length > 0 && (
            <li className="flex items-center gap-3 px-5 py-2">
              <input
                type="checkbox"
                aria-label={t("forms.selectAllAria")}
                checked={allSelected}
                onChange={toggleAll}
                className="h-4 w-4 accent-[var(--jf-primary)]"
              />
              <span className="text-xs text-[var(--jf-muted)]">{t("forms.selectAll")}</span>
            </li>
          )}
          {forms.map((f) => {
            // Issue #126 / #602: the row's way into this form's entries.
            // A payment form's entries are Sales; a response form's entries
            // are Submissions — the SAME destination, because /sales?form=<id>
            // renders a response form's submissions in the answers layout
            // (#604). Computed once and reused by BOTH the published branch
            // and the draft/private branch below: gating either branch on the
            // mode is what left a response form with no entry point at all.
            const entries = formEntriesAction(f, f.id);
            // Layer 3 (defence-in-depth): resolve the row's public link ONCE,
            // through the module that can never hand back an empty href. The
            // pre-fix page passed a bare string straight into `href={...}`, so
            // an unresolved URL rendered `<a href="">` — which the browser
            // resolves to the CURRENT document, opening /forms again in a new
            // tab. That is the reported symptom; see src/lib/formPublicURL.ts.
            const view = formViewAction(f, workspaceHost);
            return (
            <li key={f.id} className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-center gap-3">
                {canWrite && (
                  <input
                    type="checkbox"
                    aria-label={t("forms.selectOneAria", { title: f.title })}
                    checked={selected.has(f.id)}
                    onChange={() => toggle(f.id)}
                    className="h-4 w-4 shrink-0 accent-[var(--jf-primary)]"
                  />
                )}
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{f.title}</p>
                  <p className="font-mono text-[11px] text-[var(--jf-muted)]">
                    {view.kind === "link" ? view.href : t("forms.urlUnavailable")}
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                {f.status === "published" || f.status === "unlisted" ? (
                  <>
                    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">
                      {f.status === "unlisted" ? t("forms.unlisted") : t("forms.live")}
                    </span>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("fs.title")}
                    </Link>
                    {/* Issue #126: per-form entries deep-link -> /sales?form=<id>.
                        #602 removed the "Sales" label from a response form
                        (correct — an RSVP is not a sale); #604 made that same
                        URL render a response form's submissions. So both modes
                        get an action here, and only the label differs: Sales
                        for a payment form, Submission for a response form.
                        Gating this on the mode (as the Sales link alone was)
                        leaves a response form with no route to its entries. */}
                    {entries && (
                      <Link href={entries.href} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t(entries.labelKey)}
                      </Link>
                    )}
                    {view.kind === "link" ? (
                      <a href={view.href} target="_blank" rel="noreferrer" className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t("bld.view")}
                      </a>
                    ) : (
                      // Layer 3: never an anchor with an empty href. `href=""`
                      // resolves to the CURRENT page, so the click reloaded
                      // /forms (in a new tab, via target="_blank") instead of
                      // opening the form. A disabled button keeps the row's
                      // layout and says why via title/aria.
                      <button
                        type="button"
                        disabled
                        title={t("forms.viewUnavailableHint")}
                        aria-label={t("forms.viewUnavailableHint")}
                        className="jf-btn-ghost min-h-[44px] sm:min-h-10 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {t("bld.view")}
                      </button>
                    )}
                  </>
                ) : (
                  <>
                    <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-[11px] font-medium text-slate-500">
                      {f.status === "private" ? t("forms.private") : t("forms.draft")}
                    </span>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                      {t("fs.title")}
                    </Link>
                    {/* Issue #126: per-form entries deep-link (works for drafts too).
                        Same rule as the published branch above: both modes get
                        an action, and only the label differs (Sales / Submission).
                        This branch is the one that matters for an unpublished
                        response form, whose submissions would otherwise have no
                        route at all until it is published. */}
                    {entries && (
                      <Link href={entries.href} className="jf-btn-ghost min-h-[44px] sm:min-h-10">
                        {t(entries.labelKey)}
                      </Link>
                    )}
                    {canWrite && (
                      <button onClick={() => publish(f.id)} className="jf-btn-primary min-h-[44px] sm:min-h-10">
                        {t("builder.publish")}
                      </button>
                    )}
                  </>
                )}
              </div>
            </li>
            );
          })}
          {forms.length === 0 && (
            <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("forms.empty")}</li>
          )}
        </ul>
        </>
        )}
      </div>

      {/* Purge confirmation: the form's slug typed back. A permanent delete
          takes the form's CASCADE children with it, so it must name what it
          destroys. */}
      {confirmPurge && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="purge-form-title"
          onClick={() => !purgeBusy && setConfirmPurge(null)}
        >
          <div className="jf-card w-full max-w-md p-6" onClick={(e) => e.stopPropagation()}>
            <h2 id="purge-form-title" className="text-lg font-semibold">
              {t("forms.purgeConfirmTitle")}
            </h2>
            <p className="mt-2 text-sm text-[var(--jf-muted)]">{t("forms.purgeConfirmBody")}</p>
            <p className="mt-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
              <span className="font-mono font-semibold break-all">{confirmPurge.slug}</span>
            </p>
            <label htmlFor="purge-confirm-slug" className="mt-3 block text-xs text-[var(--jf-muted)]">
              {t("forms.purgeConfirmFieldLabel")}
            </label>
            <input
              id="purge-confirm-slug"
              type="text"
              value={purgeTyped}
              autoFocus
              autoComplete="off"
              spellCheck={false}
              disabled={purgeBusy}
              onChange={(e) => setPurgeTyped(e.target.value)}
              className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm"
            />
            <div className="mt-4 flex flex-wrap justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  setConfirmPurge(null);
                  setPurgeTyped("");
                }}
                disabled={purgeBusy}
                className="jf-btn-ghost px-4 py-2 text-sm"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                onClick={() => void purge()}
                disabled={purgeBusy || purgeTyped.trim() !== confirmPurge.slug}
                className="rounded-md bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {purgeBusy ? t("common.saving") : t("forms.purge")}
              </button>
            </div>
          </div>
        </div>
      )}
    </Shell>
  );
}