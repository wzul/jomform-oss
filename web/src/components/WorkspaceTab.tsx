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
import { apiErr, translateApiErr } from "../lib/apiErr";
import { useSession } from "./session";
import { WORKSPACE_NAME_MAX_CHARS } from "../lib/workspaceIdentity";
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

  // ── Rename state ──
  // Seeded from the server's answer and re-seeded after a save, so the inputs
  // always show what the workspace ACTUALLY is rather than what was typed.
  const [nameDraft, setNameDraft] = useState("");
  const [slugDraft, setSlugDraft] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [renameErr, setRenameErr] = useState("");
  const [renameOk, setRenameOk] = useState("");

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
    setNameDraft(r.data.name ?? "");
    setSlugDraft(r.data.slug ?? "");
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

  // ── Rename ──
  // Ownership, not merely the manage/admin role: the slug is the tenant's PUBLIC
  // host, printed on QR stands and handed to affiliates, so the person who
  // carries the consequences makes that call. The server enforces the same rule
  // (and is the authority); this only stops the UI from offering a control that
  // would be refused.
  const renameIsOwner = check?.is_owner === true;
  const nameDirty = check != null && nameDraft.trim() !== check.name;
  const slugDirty = check != null && slugDraft.trim() !== check.slug;
  const slugWellFormed = /^[a-z0-9-]{3,40}$/.test(slugDraft.trim()) && !slugDraft.trim().endsWith("jomform");
  const canRename =
    check != null &&
    !renaming &&
    (nameDirty || (slugDirty && renameIsOwner)) &&
    (!slugDirty || slugWellFormed);

  async function doRename() {
    if (!check || !canRename) return;
    setRenaming(true);
    setRenameErr("");
    setRenameOk("");
    // Send ONLY what changed: an empty field means "keep", and sending the
    // current value would be a no-op rename the server refuses as "tiada
    // perubahan".
    const body: { name?: string; slug?: string } = {};
    if (nameDirty) body.name = nameDraft.trim();
    if (slugDirty && renameIsOwner) body.slug = slugDraft.trim();
    try {
      const res = await fetch(`/api/workspaces/${check.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const d = await res.json().catch(() => null);
      if (!res.ok) {
        // #6177's rule, and it binds here more tightly than anywhere else: the
        // server's `error` string is Malay prose, so rendering it verbatim would
        // hand an English-locale merchant a Malay sentence. The machine `code` is
        // the authority on WHY (nameTooLong / slugInvalid / slugTaken /
        // slugReserved); the sentence comes from the dictionary, and the page's
        // own line is the fallback for a code this build does not know.
        setRenameErr(translateApiErr(d, t) || t("ws.renameFail"));
        setRenaming(false);
        return;
      }
      // The browser's selected workspace is stored as the SLUG (jf_workspace).
      // A rename would leave the old slug there, and every later request would
      // carry it — the session bundle would resolve to no workspace and the user
      // would see empty data instead of their own. Write the new slug, or drop
      // the key so the bundle picks a fresh default.
      try {
        if (typeof d?.slug === "string" && d.slug !== "") {
          window.localStorage.setItem("jf_workspace", d.slug);
        } else {
          window.localStorage.removeItem("jf_workspace");
        }
      } catch {
        /* storage unavailable — the reloaded bundle still re-derives the list */
      }
      setRenameOk(t("ws.renameSaved"));
      setRenaming(false);
      // A full reload re-derives the session bundle (workspace list, chip, the
      // switcher's labels) from scratch, so the rename is visible everywhere and
      // no cached slug survives. Same shape the delete path uses, and for the
      // same reason.
      window.location.reload();
    } catch {
      setRenameErr(t("ws.renameFail"));
      setRenaming(false);
    }
  }

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
      {/* ── Identity (editable) ── */}
      <section className="jf-card mt-6 p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h2 className="text-lg font-semibold">{t("ws.identityTitle")}</h2>
          {/* Inline "?" doc link (AGENTS.md documentation-parity rule 4). */}
          <a
            href="/docs#rename-workspace"
            target="_blank"
            rel="noopener noreferrer"
            title={t("ws.renameDocs")}
            aria-label={t("ws.renameDocs")}
            className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            ?
          </a>
        </div>
        <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
          {t("ws.identityHelp")}
        </p>

        <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label htmlFor="ws-rename-name" className="text-xs text-[var(--jf-muted)]">
              {t("ws.nameField")}
            </label>
            <input
              id="ws-rename-name"
              type="text"
              value={nameDraft}
              // The server's cap, in the unit the server counts — the shared
              // constant, so this field and the switcher's create field cannot
              // disagree (see lib/workspaceIdentity.ts).
              maxLength={WORKSPACE_NAME_MAX_CHARS}
              autoComplete="off"
              disabled={renaming}
              onChange={(e) => setNameDraft(e.target.value)}
              className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
            />
          </div>
          <div>
            <label htmlFor="ws-rename-slug" className="text-xs text-[var(--jf-muted)]">
              {t("ws.slugField")}
            </label>
            <input
              id="ws-rename-slug"
              type="text"
              value={slugDraft}
              autoComplete="off"
              spellCheck={false}
              // Read-only rather than disabled for a non-owner: a disabled field
              // is skipped by assistive tech and cannot be focused, so the reason
              // below would never be reached by the people it is written for.
              readOnly={!renameIsOwner}
              aria-describedby={renameIsOwner ? "ws-rename-slug-help" : "ws-rename-slug-locked"}
              disabled={renaming}
              onChange={(e) => setSlugDraft(e.target.value)}
              className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 font-mono text-sm read-only:bg-slate-50 read-only:text-slate-600 disabled:opacity-60"
            />
            <p id="ws-rename-slug-help" className="mt-1 text-xs text-[var(--jf-muted)]">
              https://{slugDraft.trim() || check.slug}.jomform.com
            </p>
          </div>
        </div>

        {/* Why the address field is locked, stated instead of left to guess. */}
        {!renameIsOwner && (
          <p
            id="ws-rename-slug-locked"
            className="mt-3 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-600"
          >
            {t("ws.slugLocked")}
          </p>
        )}

        {/* A well-formed address that the SERVER may still refuse (already taken,
            or reserved as another workspace's former address) — say what the
            rule is before the request rather than only after. */}
        {slugDirty && renameIsOwner && !slugWellFormed && (
          <p role="alert" className="mt-3 text-sm text-red-600">
            {t("ws.slugInvalid")}
          </p>
        )}
        {slugDirty && renameIsOwner && slugWellFormed && (
          <p className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
            {t("ws.slugKeepsOld")}
          </p>
        )}

        <div className="mt-4 flex flex-wrap items-center gap-3">
          <button
            type="button"
            onClick={() => void doRename()}
            disabled={!canRename}
            className="rounded-md bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {renaming ? t("common.saving") : t("ws.saveRename")}
          </button>
          {/* Disabled-and-silent is the thing this text exists to avoid: when the
              button is off, say which of the reasons it is. */}
          {!canRename && !renaming && (
            <span className="text-xs text-[var(--jf-muted)]">
              {!nameDirty && !slugDirty
                ? t("ws.renameNothing")
                : !renameIsOwner && slugDirty
                  ? t("ws.slugLocked")
                  : t("ws.slugInvalid")}
            </span>
          )}
          {renameOk && (
            <span role="status" className="text-xs text-emerald-700">
              {renameOk}
            </span>
          )}
        </div>
        {renameErr && (
          <p role="alert" className="mt-2 text-sm text-red-600">
            {renameErr}
          </p>
        )}

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
