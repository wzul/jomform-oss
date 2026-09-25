"use client";

// Workspace tab (Settings) — workspace identity + a Danger zone.
//
// WHAT THIS IS FOR. Before this tab a merchant could CREATE a workspace and
// never remove one: there was no DELETE route, no store method, no UI. Emptying
// a workspace before deleting it was also impossible in practice, because a
// deleted (trashed) form still counted and nothing could remove it for good —
// so the trash surface and the permanent delete live on /forms, and this tab is
// where the consequence is explained.
//
// THE TWO RULES, surfaced rather than only enforced:
//   1. At least one workspace. The last one is never deletable — a user always
//      has somewhere to land. The button is DISABLED with the reason stated
//      instead of failing after it is pressed (the server enforces the same
//      rule, so this is the read half of one definition, not a second one).
//   2. Only the OWNER. An admin member manages the workspace but does not own
//      its public identity, its trading history, or the CHIP account holding
//      its credentials.
//
// The confirmation is the workspace SLUG typed back, not an OK/Cancel: this
// cannot be undone and it takes the account (and its CHIP credentials) with it.
//
// The blockers come from GET /v1/workspaces/{id}/deletion-check — the SAME
// query the delete enforces — so the list shown here can never disagree with
// what the server will refuse.

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n } from "./i18n";
import { apiErr } from "../lib/apiErr";
import { useSession } from "./session";
import { fetchJson, runLoad } from "../lib/wsfetch";

type Blocker = { kind: string; count: number; label: string };

type DeletionCheck = {
  id: string;
  name: string;
  slug: string;
  is_owner: boolean;
  role: string;
  last_workspace: boolean;
  empty: boolean;
  blockers: Blocker[] | null;
  can_delete: boolean;
};

export default function WorkspaceTab({ workspaceId }: { workspaceId: string }) {
  const { t } = useI18n();
  const { user, workspaces } = useSession();
  const router = useRouter();

  const [check, setCheck] = useState<DeletionCheck | null>(null);
  const [loading, setLoading] = useState(true);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    if (!workspaceId) {
      setLoading(false);
      return;
    }
    const r = await runLoad(
      async () =>
        fetchJson<DeletionCheck>(`/api/workspaces/${workspaceId}/deletion-check`),
      { onUnauth: goToLogin, message: t("ws.loadFail") },
    );
    if (!r.ok) {
      setCheck(null);
      setErr(r.error);
      setLoading(false);
      return;
    }
    setCheck(r.data);
    setErr("");
    setLoading(false);
  }, [workspaceId, t, goToLogin]);

  useEffect(() => {
    load();
  }, [load]);

  // Escape closes the dialog — the modal has no built-in close key otherwise.
  useEffect(() => {
    if (!confirmOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setConfirmOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [confirmOpen]);

  const blockers = Array.isArray(check?.blockers) ? check!.blockers! : [];
  const slugMatches = check != null && typed.trim() === check.slug;
  // The server decides; this only mirrors it so the button is not offered when
  // it cannot possibly succeed.
  const canDelete = check?.can_delete === true && slugMatches && !busy;

  async function doDelete() {
    if (!check || !canDelete) return;
    setBusy(true);
    setErr("");
    try {
      const res = await fetch(`/api/workspaces/${check.id}`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ confirm_slug: typed.trim() }),
      });
      if (!res.ok) {
        // A refusal names what blocks (the workspace still holds forms/sales/…),
        // so it is rendered from the code (workspace.notEmpty) in the user's own
        // language rather than from the server's prose (issue #6177). Any other
        // failure keeps the page's own sentence.
        const d = await res.json().catch(() => null);
        setErr(apiErr(d, t("ws.deleteFail"), t));
        setConfirmOpen(false);
        setTyped("");
        await load();
        return;
      }
      // The deleted workspace may be the one this browser had selected; a stale
      // jf_workspace would send a ghost slug on every later request and the user
      // would see empty data rather than a picker. A full reload re-derives the
      // session bundle (and therefore the workspace list) from scratch.
      try {
        window.localStorage.removeItem("jf_workspace");
      } catch {
        /* storage unavailable — the reload below still refetches the bundle */
      }
      window.location.reload();
    } catch {
      setErr(t("ws.deleteFail"));
      setBusy(false);
    }
  }

  if (loading) {
    return (
      <section className="jf-card mt-6 p-6">
        <p className="text-sm text-[var(--jf-muted)]">{t("common.loading")}</p>
      </section>
    );
  }

  if (!check) {
    return (
      <section className="jf-card mt-6 p-6">
        <p role="alert" className="text-sm text-red-600">
          {err || t("ws.loadFail")}
        </p>
        <button
          type="button"
          onClick={() => void load()}
          className="jf-btn-ghost mt-3 px-3 py-1.5 text-xs"
        >
          {t("common.retry")}
        </button>
      </section>
    );
  }

  const isOwner = check.is_owner && user?.id != null && check.role === "admin";

  return (
    <>
      {/* ── Identity ── */}
      <section className="jf-card mt-6 p-6">
        <h2 className="text-lg font-semibold">{t("ws.identityTitle")}</h2>
        <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
          {t("ws.identityHelp")}
        </p>
        <dl className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <dt className="text-xs text-[var(--jf-muted)]">{t("ws.name")}</dt>
            <dd className="mt-0.5 text-sm font-medium break-words">{check.name}</dd>
          </div>
          <div>
            <dt className="text-xs text-[var(--jf-muted)]">{t("ws.slug")}</dt>
            <dd className="mt-0.5 font-mono text-sm break-all">{check.slug}</dd>
          </div>
        </dl>
        {/* The rename note is deliberate: nothing can rename a workspace today
            (name and slug are set at creation), so saying so beats a
            permanently-disabled field that looks broken. */}
        <p className="mt-3 text-xs text-[var(--jf-muted)]">{t("ws.renameNote")}</p>
      </section>

      {/* ── Danger zone ── */}
      <section className="jf-card mt-6 border-rose-200 p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h2 className="text-lg font-semibold text-rose-700">{t("ws.dangerTitle")}</h2>
          {/* Inline "?" doc link (AGENTS.md documentation-parity rule 4) — it
              names the "at least one workspace" limit explicitly. */}
          <a
            href="/docs#delete-workspace"
            target="_blank"
            rel="noopener noreferrer"
            title={t("ws.docs")}
            aria-label={t("ws.docs")}
            className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            ?
          </a>
        </div>
        <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
          {t("ws.dangerHelp")}
        </p>

        {!isOwner && (
          <p className="mt-4 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-600">
            {t("ws.ownerOnly")}
          </p>
        )}

        {isOwner && (
          <>
            {/* Why the button is off, stated up front. */}
            {check.last_workspace && (
              <p className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                {t("ws.lastWorkspace")}
              </p>
            )}
            {!check.last_workspace && blockers.length > 0 && (
              <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2">
                <p className="text-sm text-amber-800">{t("ws.notEmpty")}</p>
                <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-amber-800">
                  {blockers.map((b) => (
                    <li key={b.kind}>
                      {b.label}: <strong>{b.count}</strong>
                    </li>
                  ))}
                </ul>
                <p className="mt-2 text-xs text-amber-800">{t("ws.notEmptyHint")}</p>
              </div>
            )}

            <button
              type="button"
              onClick={() => setConfirmOpen(true)}
              disabled={!check.can_delete || check.last_workspace}
              className="mt-4 rounded-md bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {t("ws.deleteButton")}
            </button>
            {check.can_delete && (
              <p className="mt-2 text-xs text-[var(--jf-muted)]">{t("ws.deleteHint")}</p>
            )}
          </>
        )}

        {err && !confirmOpen && (
          <p role="alert" className="mt-3 text-sm text-red-600">
            {err}
          </p>
        )}
      </section>

      {/* ── Confirmation dialog, typed slug ── */}
      {confirmOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby="ws-delete-title"
          onClick={() => !busy && setConfirmOpen(false)}
        >
          <div
            className="jf-card w-full max-w-md p-6"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 id="ws-delete-title" className="text-lg font-semibold">
              {t("ws.confirmTitle")}
            </h2>
            <p className="mt-2 text-sm text-[var(--jf-muted)]">
              {t("ws.confirmBody")}
            </p>
            <p className="mt-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
              {t("ws.confirmTypeLabel")}
              <span className="ml-1 font-mono font-semibold break-all">{check.slug}</span>
            </p>
            <label htmlFor="ws-confirm-slug" className="mt-3 block text-xs text-[var(--jf-muted)]">
              {t("ws.confirmFieldLabel")}
            </label>
            <input
              id="ws-confirm-slug"
              type="text"
              value={typed}
              autoFocus
              autoComplete="off"
              spellCheck={false}
              disabled={busy}
              onChange={(e) => setTyped(e.target.value)}
              className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm"
            />
            {err && (
              <p role="alert" className="mt-2 text-sm text-red-600">
                {err}
              </p>
            )}
            <div className="mt-4 flex flex-wrap justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  setConfirmOpen(false);
                  setTyped("");
                }}
                disabled={busy}
                className="jf-btn-ghost px-4 py-2 text-sm"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                onClick={() => void doDelete()}
                disabled={!canDelete}
                className="rounded-md bg-rose-600 px-4 py-2 text-sm font-medium text-white hover:bg-rose-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busy ? t("common.saving") : t("ws.confirmButton")}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
