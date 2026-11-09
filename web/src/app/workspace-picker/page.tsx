"use client";

// Issue #164: multi-workspace login picker. Shown after login when the user has
// more than one workspace and no default workspace is set. Lists the user's
// workspaces; clicking one enters it (and optionally sets it as the default).
// Single-workspace users and users with a default never land here (see
// lib/postLogin.ts).
//
// Issue #475: the route was /space-picker before the Space→Workspace rename
// (#431); the old path now redirects here so bookmarked links keep working
// (see app/space-picker/page.tsx).
//
// Issue #503: this page used to render "…" FOREVER when /api/workspaces
// answered 401 — the response was swallowed by `if (wsRes.ok)` with no `else`,
// the page had no loading flag (it used `workspaces.length === 0` to mean
// "still loading"), and so an expired session left a terminal placeholder with
// no error and no way forward. The load now goes through the shared
// lib/wsfetch decision helper: 401/403 redirect to /login, any other failure
// shows a real message plus a Retry action, the loading state is a genuine
// flag, and a truly empty list gets its own message.
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n, type Key } from "../../components/i18n";
import type { WorkspaceRow } from "../../components/session";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { createWorkspace, enterWorkspace } from "../../lib/workspace";
import { Button, Card, Field, Input } from "../../components/ui";
import { translateApiErr } from "../../lib/apiErr";
import { pickerCopyKeys, pickerMode } from "../../lib/workspacePickerState";
import { addressLabel, workspaceAddressLabel } from "../../lib/deployment";
import { useDeployment } from "../../components/useDeployment";

// The subset of /api/auth/me this page needs.
type Me = { default_workspace_id?: string };

export default function WorkspacePickerPage() {
  const { t } = useI18n();
  // Card t_48a93084: the hint under the slug field used to promise
  // "<slug>.jomform.com" on EVERY install. On a self-hosted deployment that
  // host does not exist; the workspace is the install's own origin. null until
  // /api/meta answers, so the hint is absent rather than wrong.
  const deployment = useDeployment();
  const router = useRouter();
  const [workspaces, setWorkspaces] = useState<WorkspaceRow[]>([]);
  const [defaultId, setDefaultId] = useState("");
  const [busy, setBusy] = useState(false);
  // TWO failures, TWO states (card t_6c0badbe). `loadError` decides the page
  // mode — it means "the workspace LIST could not be read". `createError` is
  // the create form's own refusal, rendered inside the form: a slug the API
  // refused is not a reason to stop showing the fields that fix it. They used
  // to share one state, so a 409 on create switched the page to the list-error
  // panel AND the picker heading ("You have multiple workspaces…") for an
  // account with zero workspaces.
  const [loadError, setLoadError] = useState("");
  const [createError, setCreateError] = useState("");
  // And a THIRD failure, its own state (card t_57954ce5): the set-default
  // WRITE's refusal. It used to be written into `loadError`, so a 4xx on
  // "Make this the default workspace" replaced the whole list — the rows the
  // merchant was acting on — with the load-error panel and a Retry button, even
  // though the list had been read successfully and was still valid. A failed
  // write is not a failed read: the refusal is rendered beside the rows.
  const [setDefaultError, setSetDefaultError] = useState("");
  // Issue #503: a REAL loading flag. Never infer "loading" from an empty list —
  // that is what made empty, failed and loading indistinguishable.
  const [checking, setChecking] = useState(true);
  // Ask-first onboarding: with zero workspaces this page IS the first-run
  // prompt, so it carries the create form (name + slug) instead of the bare
  // "You don't have any workspaces yet." + Retry that used to be a dead end.
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newSlug, setNewSlug] = useState("");

  // Issue #163: short role label for the picker rows.
  function roleLabel(role?: string): string {
    if (!role) return "";
    if (role === "owner") return t("role.owner");
    return t(`role.${role}` as Key) || role;
  }

  // 401/403 → the session is gone; bounce to /login. Mirrors the existing
  // pattern in components/Shell.tsx.signOut() (router.replace + a hard
  // window.location.href so the stale client state cannot survive).
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // A 401/403 on a READ means the session may be gone, so the read arms below
  // still bounce. A WRITE is a different question (card t_25e9224b): only a
  // **401** says the session itself is dead. This route answers "not your
  // workspace" with `403 {"code":"request.forbidden"}`
  // (api/internal/httpapi/profile.go's handleSetDefaultWorkspace) — a refusal of
  // the CHANGE from a session that is still perfectly alive. Sending that
  // through `goToLogin` navigated a signed-in merchant to /login and destroyed
  // the rows they were working on: measured on prod v1.22.126, `location.href`
  // became /login, `main li` 0, no sentence, while `/api/auth/me` still answered
  // 200 with the default unchanged. So the write navigates ONLY on 401 and every
  // other refusal lands in `setDefaultError`, beside the rows.
  const goToLoginIfSessionDead = useCallback(
    (status: number) => {
      if (status === 401) goToLogin();
    },
    [goToLogin],
  );

  const load = useCallback(async () => {
    setChecking(true);
    setLoadError("");
    setCreateError("");
    // A fresh read supersedes any refusal the old list produced.
    setSetDefaultError("");
    // The workspaces list is the one that decides the page state. `me` only
    // supplies the default-workspace badge, so a failure there must never hide
    // a perfectly good list.
    const ws = await runLoad(
      async () => {
        const d = await fetchJson<unknown>("/api/workspaces");
        return Array.isArray(d) ? (d as WorkspaceRow[]) : [];
      },
      { onUnauth: goToLogin, message: t("workspace.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!ws.ok) {
      // A failure is NEVER silent: the message is rendered, and the list is
      // cleared so a failed load can never look like an empty one.
      setWorkspaces([]);
      setLoadError(ws.error);
      setChecking(false);
      return;
    }
    setWorkspaces(ws.data);

    const me = await runLoad(
      () => fetchJson<Me>("/api/auth/me"),
      { onUnauth: goToLogin, message: t("workspace.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (me.ok) setDefaultId(me.data.default_workspace_id || "");

    setChecking(false);
  }, [t, goToLogin]);

  useEffect(() => {
    void load();
  }, [load]);

  function enter(slug: string) {
    enterWorkspace(slug);
    router.replace("/");
  }

  // askFirstCreate is the first-run action: create the workspace, enter it, and
  // land on the dashboard inside it. No reload: the session bundle is
  // workspace-keyed (session.tsx) and enterWorkspace dispatches the change, so
  // the dashboard re-fetches for the new workspace. A user who reaches this page
  // with zero workspaces is expected to take exactly this path before any create
  // surface is reachable.
  async function askFirstCreate(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setCreateError("");
    const r = await createWorkspace({ name: newName, slug: newSlug });
    if (!r.ok) {
      // The create-failed copy is a rendered i18n string from the shared
      // dictionary — never the server's own text (the API answers in Malay
      // prose, which is the separate i18n defect).
      //
      // WRITTEN TO createError, NEVER loadError (card t_6c0badbe): this refusal
      // is about the values in the form, so the form (and its fields) must stay
      // on screen with the sentence beside them.
      setCreateError(t(r.errorKey));
      setBusy(false);
      return;
    }
    enterWorkspace(r.slug);
    setCreating(false);
    setBusy(false);
    router.replace("/");
  }

  async function setDefault(workspaceId: string) {
    setBusy(true);
    // NEVER `setLoadError` here (card t_57954ce5): `loadError` means "the
    // workspace LIST could not be read", and it is an input to `pickerMode` —
    // writing this refusal into it swapped the list out for the error panel and
    // a Retry button (measured: 2 rows -> 0). The list this write was made from
    // is still valid, so the refusal belongs beside the rows.
    setSetDefaultError("");
    const r = await runLoad(
      () => fetchJson<unknown>("/api/auth/profile/default-workspace", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workspace_id: workspaceId }),
      }),
      // NOT `goToLogin` (card t_25e9224b): a refused WRITE is a 4xx about the
      // change, and this route's answer for "not your workspace" is a 403 with
      // `request.forbidden` — a code `wsfetch` does not know as a tenancy
      // refusal, so it classifies as `unauth`. Navigating on that threw a
      // signed-in merchant out of the list for a session that was still alive.
      // Only a 401 (the session really is gone) navigates.
      { onUnauth: goToLoginIfSessionDead, message: t("workspace.setDefaultFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setSetDefaultError(r.error);
      setBusy(false);
      return;
    }
    setDefaultId(workspaceId);
    setSetDefaultError("");
    setBusy(false);
  }

  // The mode is decided by the LOAD's outcome only (cards t_6c0badbe and
  // t_57954ce5) — see lib/workspacePickerState.ts. Neither `createError` nor
  // `setDefaultError` is an input: a refused slug cannot take the merchant's
  // fields away from them, and a refused set-default cannot take the rows away.
  const mode = pickerMode({ checking, loadError, workspaceCount: workspaces.length });
  // The count goes into the copy too: with one workspace the "multiple
  // workspaces, pick one" sentence is false (that is the whole bug).
  const copy = pickerCopyKeys(mode, workspaces.length);

  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-[-260px] h-[420px] w-[720px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{
          background:
            "radial-gradient(closest-side, rgba(94,106,210,0.35), rgba(113,112,255,0.12), transparent)",
        }}
      />
      <div className="relative w-full max-w-md">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[var(--jf-primary)] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
            JF
          </div>
          <h1 className="text-xl font-semibold text-[var(--jf-text)]" style={{ letterSpacing: "-0.02em" }}>
            {t(copy.title as Key)}
          </h1>
          <p className="mt-1.5 text-sm text-[var(--jf-muted)]">
            {t(copy.subtitle as Key)}
          </p>
        </div>

        <Card className="p-jf-6">
          {/* Issue #503: the four states are now distinct and explicit —
              loading / error (+ retry) / empty / list. */}
          {mode === "loading" ? (
            <p role="status" aria-live="polite" className="py-6 text-center text-sm text-[var(--jf-muted)]">
              {t("workspace.loading")}
            </p>
          ) : mode === "loadError" ? (
            <div role="alert" className="flex flex-col items-center gap-3 py-4 text-center">
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-jf-sm text-red-700">
                {loadError}
              </p>
              <Button type="button" variant="secondary" size="sm" onClick={() => void load()}>
                {t("common.retry")}
              </Button>
            </div>
          ) : mode === "ask" ? (
            /* Ask-first onboarding (Wan's decision, 2026-09-20): a user with no
               workspace at all is ASKED to create one, and this is the prompt.
               It replaces a dead end that rendered
               t("workspace.empty") + a Retry button and nothing else — the only
               create affordance was the hidden "+ New workspace" inside the
               topbar switcher, which is why nobody found it. The flow finishes
               by entering the new workspace, so the user can immediately create
               a form with no error. */
            <form onSubmit={askFirstCreate} className="space-y-3">
              <p className="text-sm font-medium text-[var(--jf-text)]">
                {t("workspace.askTitle")}
              </p>
              <p className="text-jf-sm leading-relaxed text-[var(--jf-muted)]">
                {t("workspace.askBody")}
              </p>
              <Field id="ask-ws-name" label={t("workspace.name")}>
                <Input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  required
                  autoFocus
                  disabled={busy}
                />
              </Field>
              <Field
                id="ask-ws-slug"
                label={t("workspace.slug")}
                hint={newSlug && workspaceAddressLabel(newSlug, deployment) ? t("workspace.askHost", { host: workspaceAddressLabel(newSlug, deployment) }) : undefined}
              >
                <Input
                  value={newSlug}
                  onChange={(e) => setNewSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                  required
                  disabled={busy}
                />
              </Field>
              {createError && (
                <p role="alert" className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-jf-sm text-red-700">
                  {createError}
                </p>
              )}
              <Button type="submit" disabled={busy} className="w-full">
                {busy ? "…" : t("workspace.create")}
              </Button>
            </form>
          ) : (
            <>
              {/* A refused set-default renders HERE, beside the rows it failed
                  to change (card t_57954ce5) — never as the page's load-error
                  panel, which says the list could not be read. The badge on the
                  default row below is the page's claim about SERVER state, and
                  it is still true. */}
              {setDefaultError && (
                <p role="alert" className="mb-3 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-jf-sm text-red-700">
                  {setDefaultError}
                </p>
              )}
              <ul className="space-y-2">
                {workspaces.map((s) => {
                  const isDefault = s.id === defaultId;
                  return (
                    <li
                      key={s.id}
                      className="flex flex-col gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-[var(--jf-text)]">
                          {s.name}
                          {isDefault && (
                            <span className="ml-2 rounded-full bg-indigo-50 px-2 py-0.5 text-jf-3xs font-medium text-[var(--jf-primary-on-tint)]">
                              {t("workspace.defaultLabel")}
                            </span>
                          )}
                          {s.role && (
                            <span className="ml-2 rounded-full bg-slate-100 px-2 py-0.5 text-jf-3xs font-medium text-[var(--jf-muted-on-tint)]">
                              {roleLabel(s.role)}
                            </span>
                          )}
                        </p>
                        {/* The address the API publishes for this deployment
                            (card t_48a93084) — never composed here. */}
                        {s.url && (
                          <p className="truncate text-jf-2xs text-[var(--jf-muted)]">{addressLabel(s.url)}</p>
                        )}
                        {!isDefault && (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => setDefault(s.id)}
                            title={t("workspace.setDefault")}
                            className="mt-1.5 text-jf-2xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)] disabled:opacity-50"
                          >
                            {t("workspace.setDefault")}
                          </button>
                        )}
                      </div>
                      <div className="flex shrink-0 items-center gap-2 sm:justify-end">
                        <Button type="button" size="sm" onClick={() => enter(s.slug)}>
                          {t("workspace.enter")}
                        </Button>
                      </div>
                    </li>
                  );
                })}
              </ul>
            </>
          )}
        </Card>

        {/* Issue #503: a genuinely empty list is still a dead end without an
            action — offer a re-check, plus the inline "?" docs link AGENTS.md
            documentation parity asks for next to the feature. */}
        {mode === "ask" && (
          <div className="mt-4 text-center">
            <button
              type="button"
              onClick={() => void load()}
              className="text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
            >
              {t("common.retry")}
            </button>
            <a
              href="/docs#workspaces"
              className="ml-3 text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
            >
              {t("workspace.docs")}
            </a>
          </div>
        )}
      </div>
    </main>
  );
}
