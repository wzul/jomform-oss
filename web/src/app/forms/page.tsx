"use client";

import { useCallback, useEffect, useState } from "react";
import type { CSSProperties } from "react";
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
import { acknowledgmentBody, needsProductsIDs, publishConfirmText, publishErrorText, publishOutcome } from "../../lib/publishResult";
import TemplatePicker from "../../components/TemplatePicker";
import { apiErr, translateApiErr } from "../../lib/apiErr";
import { panelDomId, tabDomId, tabStop, tablistKeyDown } from "../../lib/tablist";

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
  // The TRASH view's OWN refusal channel (card t_ab4aa85b). Two reasons it is
  // not `err`, and both are the defect this line fixes:
  //
  //   1. Every refusal branch used to write the sentence to `err` and then fire
  //      `loadTrash()`, which clears `err` on a successful reload — so the
  //      sentence was set and erased in the same tick and the merchant never
  //      read why the action was refused. `loadTrash()` does not touch this
  //      state, so the refetch that REPORTS the refusal can no longer wipe it.
  //   2. `err` is this page's LOAD-failure chrome: it returns early and replaces
  //      the whole page (with a Retry that loads the ACTIVE list), so a refused
  //      row action also threw away the trash list the merchant was reading. A
  //      refusal changes nothing, so the list must stay.
  //
  // Cleared at the START of the next user action that re-issues the action (the
  // restore/purge handlers open with it) and by the Active/Trash tab switch.
  // THE RULE: a refusal stays readable until the user's next action — never
  // erased by the refetch that reports it.
  const [trashErr, setTrashErr] = useState("");
  // The TRASH LISTING's own failure channel (card t_d9dda884). `loadTrash()` used
  // to report a failed listing through `err`, the page's load-failure chrome, and
  // that chose the render branch: the whole page was replaced by "We couldn't
  // load the trash… Retry" — which took away BOTH the rows and the refusal the
  // merchant had just been shown. Measured on the fixed bytes of t_ab4aa85b: after
  // a Restore refused with 409 form.slugTaken, making every later trash listing
  // answer 500 left the refusal sentence readable at NO sample of 30 taken every
  // 100ms for 3s — the merchant read only the load-failure card. A double failure
  // (refusal + a failing relist) must not cost them WHY their action was refused.
  //
  // The decision (the card offered "keep the refusal, or accept the load failure
  // and weaken the invariant"): KEEP THE REFUSAL. The rule above is the product's
  // decision, already taken in t_ab4aa85b — "never erased by the refetch that
  // reports it" — and a refetch that FAILS reports the refusal no less than one
  // that succeeds, so the invariant is not weakened for the failure path: the
  // listing's failure is shown BESIDE the refusal, in the trash view, instead of
  // swapping the page for chrome whose Retry loads the ACTIVE list.
  const [trashLoadErr, setTrashLoadErr] = useState("");
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
      { onUnauth: goToLogin, message: t("forms.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
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
      // t_d9dda884: NOT `setErr` — see `trashLoadErr` above. The rows are kept
      // rather than blanked for the reason `load()` states below: a failed load
      // must never render as "the trash is empty" when nobody knows that.
      setTrashLoadErr(r.error);
      return;
    }
    setTrashed(r.data);
    setTrashLoaded(true);
    setTrashLoadErr("");
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
    // The NEXT action on this surface clears the previous refusal (the rule, in
    // the state's own comment above). It does not clear it by reloading.
    setTrashErr("");
    let res: Response;
    try {
      res = await fetch(`/api/rest/forms/${id}/restore${workspaceQuery()}`, {
        method: "POST",
      });
    } catch {
      // A transport failure (offline, aborted, DNS) is an ACTION failure like
      // any other, and it must leave a sentence on this surface. Without this
      // branch the rejection escaped through the row's `void restore(f.id)`
      // handler as an unhandled rejection and the merchant saw NOTHING at all —
      // measured while fixing t_ab4aa85b (aborted request, live DOM: no alert
      // within 20s), which is the same outcome as the defect this card fixes.
      // `purge()` already had its half; this one never did.
      setTrashErr(t("forms.restoreFail"));
      return;
    }
    if (!res.ok) {
      // 409 means the slug was re-claimed while the form sat in the trash — the
      // refusal's code (form.slugTaken) says which obstacle to clear, so the
      // merchant gets that sentence in their OWN language rather than the wire
      // prose (issue #6177). An unrelated failure falls back to the generic line.
      //
      // t_ab4aa85b: the sentence goes to `trashErr`, NOT `err`, and the relist
      // below no longer erases it — it used to be set and then wiped by
      // `loadTrash()`'s `setErr("")` in the same tick, so it was never readable.
      const d = await res.json().catch(() => null);
      setTrashErr(apiErr(d, t("forms.restoreFail"), t));
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
    // The next purge clears the previous refusal — at the START of the action,
    // never as a side effect of the relist (card t_ab4aa85b).
    setTrashErr("");
    try {
      const res = await fetch(`/api/rest/forms/${confirmPurge.id}/purge${workspaceQuery()}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm_slug: purgeTyped.trim() }),
      });
      if (!res.ok) {
        // A purge of a form that was ever sold is refused with code
        // form.hasSales; any other failure uses the page's own sentence. Never
        // the server's prose (issue #6177). It renders beside the trash rows
        // (trashErr), so the refusal survives the relist below it.
        const d = await res.json().catch(() => null);
        setTrashErr(apiErr(d, t("forms.purgeFail"), t));
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
      // A transport failure (offline, aborted) — also an ACTION failure, so it
      // uses the same channel rather than the page's load-failure chrome. The
      // dialog is closed with it: it used to stay open while `err` replaced the
      // whole page, which unmounted the dialog anyway.
      setTrashErr(t("forms.purgeFail"));
      setConfirmPurge(null);
      setPurgeTyped("");
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
      // t_10453dd9: the sentence comes from the machine code (apiErr), never from
      // the outcome — a failed publish used to carry the API's prose in a
      // `message` field and this line rendered it.
      setErr(publishErrorText(outcome, t("forms.loadFail"), t));
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
      setErr(publishErrorText(retryOutcome, t("forms.loadFail"), t));
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
          setErr(publishErrorText(outcome, t("forms.loadFail"), t));
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
          setErr(publishErrorText(ro, t("forms.loadFail"), t));
          return;
        }
      }
      // t_c9e37f49: the reload happens ONLY here, on the path that actually
      // changed the list. It used to sit in the `finally` above, which runs even
      // when a failure branch `return`s from inside the `try` — and `load()`
      // clears `err` on a successful reload (line ~120). So the failure sentence
      // was replaced by a freshly loaded list ~200ms after it was set, and the
      // merchant read nothing at all: a failed bulk publish looked like a click
      // that did nothing. This is the same shape the single-form `publish()`
      // above has always had (no `finally`), which is why its message persists.
      // A failure path needs no reload — the batch was refused before it touched
      // anything.
      setErr("");
      load();
    } finally {
      // Still cleared on every path: `busy` disables the toolbar buttons, and
      // leaving them disabled after a refusal would be its own dead end.
      setBusy(false);
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
            <button type="button" onClick={() => void load()} className="jf-btn-ghost">
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
        <div
          className="mt-5 flex flex-wrap items-center gap-1 border-b border-[var(--jf-border)]"
          role="tablist"
          onKeyDown={tablistKeyDown}
        >
          {(["active", "trash"] as const).map((v) => (
            <button
              key={v}
              id={tabDomId("forms", v)}
              aria-controls={panelDomId("forms", v)}
              tabIndex={tabStop(v, view)}
              type="button"
              role="tab"
              aria-selected={view === v}
              onClick={() => {
                setView(v);
                setErr("");
                setNotify("");
                // Switching views is the user's next action: the refusal about
                // the view being left is cleared with the success notice.
                setTrashErr("");
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
          <div
            role="tabpanel"
            id={panelDomId("forms", "trash")}
            aria-labelledby={tabDomId("forms", "trash")}
          >
            <p className="mt-4 max-w-2xl text-sm text-[var(--jf-muted)]">{t("forms.trashNote")}</p>
            {/* The refusal (card t_ab4aa85b), rendered beside the rows it is
                about. It is a state no loader writes, so the relist that
                reports the refusal cannot erase the sentence — and unlike the
                page's load-failure card it does not take the list away from the
                merchant who is trying to read WHY their action was refused. */}
            {trashErr && (
              <p
                role="alert"
                className="mt-3 max-w-2xl rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700"
              >
                {trashErr}
              </p>
            )}
            {/* …and, RIGHT HERE beside it, the listing failure (card t_d9dda884).
                Both are readable at once: a failed relist used to report into
                `err`, whose render branch replaces the whole page — refusal
                included. This line is what the merchant sees INSTEAD of losing
                the sentence, and its Retry reloads the trash, not the active
                list (which is what the page-level chrome's Retry does, on a page
                whose trash rows are the thing that failed to load). */}
            {trashLoadErr && (
              <div className="mt-3 flex max-w-2xl flex-wrap items-center gap-3 rounded-lg bg-rose-50 px-3 py-2">
                <p role="alert" className="text-sm text-rose-700">
                  {trashLoadErr}
                </p>
                <button
                  type="button"
                  onClick={() => void loadTrash()}
                  className="jf-btn-ghost"
                >
                  {t("common.retry")}
                </button>
              </div>
            )}
            <ul className="mt-4 space-y-2">
              {trashed.map((f) => (
                <li
                  key={f.id}
                  className="jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{f.title}</p>
                    <p className="font-mono text-jf-2xs text-[var(--jf-muted)] break-all">{f.slug}</p>
                    {!f.can_purge && (
                      <p className="mt-1 text-jf-2xs text-amber-700">
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
                          className="jf-btn-ghost"
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
                            className="jf-btn-ghost text-rose-700"
                          >
                            {t("forms.purge")}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </li>
              ))}
              {trashed.length === 0 && !trashLoadErr && (
                <li className="rounded-lg text-sm text-[var(--jf-muted)]">{t("forms.trashEmpty")}</li>
              )}
            </ul>
          </div>
        )}

        {view === "active" && (
        <div
          role="tabpanel"
          id={panelDomId("forms", "active")}
          aria-labelledby={tabDomId("forms", "active")}
        >
        {canWrite && ids.length > 0 && (
          <div className="jf-card mt-4 flex flex-wrap items-center gap-2 border-[var(--jf-border)] px-4 py-3">
            <span className="text-sm font-medium">
              {t("forms.nSelected", { n: ids.length })}
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                onClick={() => bulk("publish")}
                disabled={busy}
                className="jf-btn-ghost"
              >
                {t("builder.publish")}
              </button>
              <button
                onClick={() => bulk("draft")}
                disabled={busy}
                className="jf-btn-ghost"
              >
                {t("forms.draft")}
              </button>
              <button
                onClick={() => bulk("delete")}
                disabled={busy}
                className="jf-btn-ghost text-rose-700"
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
          {forms.map((f, rowIndex) => {
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
            // Card t_d5336ec6: the first EIGHT rows fade + 4px rise in on a
            // 30ms step, and the delay is capped so the last of them finishes
            // by 280ms (`.jf-stagger-row` in globals.css does the arithmetic).
            // `--jf-i` carries the index; row 9+ gets neither the class nor a
            // delay, so a longer list renders static — the card's bounds forbid
            // motion on a table longer than eight rows.
            const view = formViewAction(f, workspaceHost);
            return (
            <li
              key={f.id}
              className={`jf-card flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-center sm:justify-between${
                rowIndex < 8 ? " jf-stagger-row" : ""
              }`}
              style={rowIndex < 8 ? ({ "--jf-i": rowIndex } as CSSProperties) : undefined}
            >
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
                  {/* t_bb0d0bee: the STATUS pill lives with the TITLE, not in the
                      action row. In the action row it sat between links and
                      buttons and read as a third control ("Draft" beside "Edit"
                      and "Form Settings"); status is information about the form,
                      so it belongs where the form's name is. The row below is
                      actions only now (Edit / Form Settings / entries / View /
                      Publish). */}
                  <div className="flex min-w-0 items-center gap-2">
                    <p className="min-w-0 truncate text-sm font-medium">{f.title}</p>
                    {f.status === "published" || f.status === "unlisted" ? (
                      <span className="shrink-0 rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 text-jf-2xs font-medium text-emerald-700">
                        {f.status === "unlisted" ? t("forms.unlisted") : t("forms.live")}
                      </span>
                    ) : (
                      <span className="shrink-0 rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-jf-2xs font-medium text-slate-500">
                        {f.status === "private" ? t("forms.private") : t("forms.draft")}
                      </span>
                    )}
                  </div>
                  <p className="font-mono text-jf-2xs text-[var(--jf-muted)]">
                    {view.kind === "link" ? view.href : t("forms.urlUnavailable")}
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                {f.status === "published" || f.status === "unlisted" ? (
                  <>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost">
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
                      <Link href={entries.href} className="jf-btn-ghost">
                        {t(entries.labelKey)}
                      </Link>
                    )}
                    {view.kind === "link" ? (
                      <a href={view.href} target="_blank" rel="noreferrer" className="jf-btn-ghost">
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
                        className="jf-btn-ghost"
                      >
                        {t("bld.view")}
                      </button>
                    )}
                  </>
                ) : (
                  <>
                    {canWrite && (
                      <Link href={`/builder?form=${f.id}`} className="jf-btn-ghost">
                        {t("forms.edit")}
                      </Link>
                    )}
                    {/* Issue #177: dedicated Form Settings view */}
                    <Link href={`/form-settings?form=${f.id}`} className="jf-btn-ghost">
                      {t("fs.title")}
                    </Link>
                    {/* Issue #126: per-form entries deep-link (works for drafts too).
                        Same rule as the published branch above: both modes get
                        an action, and only the label differs (Sales / Submission).
                        This branch is the one that matters for an unpublished
                        response form, whose submissions would otherwise have no
                        route at all until it is published. */}
                    {entries && (
                      <Link href={entries.href} className="jf-btn-ghost">
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
        </div>
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
                className="jf-btn-ghost"
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