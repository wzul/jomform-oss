"use client";

// Issue #29: session context — `me` + `workspaces` fetched ONCE per browser
// session (dashboard-bundle) and shared across the Shell and every page via
// React context. Kills the per-mount /api/auth/me round-trip that each page
// previously paid serially before fetching its own data.
//
// Issue #158: the bundle is WORKSPACE-KEYED. The dashboard-bundle carries
// workspace-scoped data (chip_status, keys, domains, webhooks, einvoice,
// products, summary, sales, analytics) alongside user-level data (me,
// workspaces). Because each workspace owns its own account, the bundle must be
// fetched with ?workspace=<slug> and cached per workspace — switching
// workspaces must re-fetch the bundle for the newly selected workspace, never
// serve the previous workspace's cached data (cross-workspace leakage).
//
// Issue #475: identifiers renamed Space→Workspace to finish the #431 rename
// (the JSON payload keys already changed; the frontend names lagged).
//
// Bundle shape (api/internal/httpapi/rest_dashboard.go): one authed request
// carrying me (id/email/name/totp_enabled), workspaces, chip_status, keys,
// domains, webhooks, social, einvoice + the dashboard landing data
// (products, summary, sales, form_analytics, product_analytics).

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  activeWorkspaceScope,
  clearUnknownWorkspaceScope,
  dropCurrentWorkspaceScope,
  publishWorkspaceScope,
} from "../lib/workspace";
import { isScopeRefusal } from "../lib/apiErr";
import { isUnauthStatus, readErrorBody } from "../lib/wsfetch";

export type SessionUser = {
  id?: string;
  email: string;
  name?: string;
  avatar_url?: string; // issue #133 profile picture (empty = initial-letter fallback)
  phone?: string; // issue #181 optional phone number (empty = not set)
  timezone?: string; // issue #191 preferred IANA timezone (empty = Auto → browser default)
  lang?: string; // language placement: per-profile dashboard UI language (empty = not set → browser value → en)
  totp_enabled?: boolean;
  // Issue #458: true when this session came from a passwordless magic-link
  // click. With 2FA enabled, sensitive actions (change password / disable 2FA /
  // add passkey) require a fresh TOTP code.
  magic_session?: boolean;
  // Issue #164 / #472: the backend field is default_workspace_id (the
  // Space→Workspace rename in #431 renamed the key too). Empty = no default
  // → workspace-picker on login.
  default_workspace_id?: string;
  is_super_admin?: boolean; // issue #387 (site super-admin, #385)
};

export type WorkspaceRow = { id: string; name: string; slug: string; role?: string };

// Landing-data sections of the bundle, consumed by the dashboard page.
export type BundleLanding = {
  products?: unknown[];
  summary?: { counts: Record<string, number>; revenue_sen: number } | null;
  sales?: { items: unknown[]; page: number; limit: number; total: number } | null;
  form_analytics?: unknown[];
  product_analytics?: unknown[];
};

export type ChipStatusShape = {
  configured: boolean;
  verified?: boolean;
  mode?: string;
  brand_id?: string;
  secret_key?: string;
  secret_key_masked?: string;
  whitelist?: string[];
  email_fallback?: string;
};

export type BundleData = {
  me?: SessionUser | null;
  // Issue #452: the dashboard-bundle field was renamed spaces → workspaces in
  // the #431 Space→Workspace refactor; the frontend was still reading `spaces`,
  // so the workspace list came back empty and the switcher showed only one
  // workspace. Read `workspaces` (current) with `spaces` as a legacy fallback.
  workspaces?: WorkspaceRow[];
  spaces?: WorkspaceRow[];
  chip_status?: ChipStatusShape | null;
  keys?: { id: string; name: string; scopes: string[] }[];
  domains?: { id: string; domain: string; provider?: string; verified: boolean }[];
  webhooks?: { id: string; url: string; events: string[] }[];
  social?: { provider: string; email?: string; linked_at?: string }[];
  einvoice?: { tin?: string; msic?: string } | null;
} & BundleLanding;

type SessionCtx = {
  user: SessionUser | null;
  workspaces: WorkspaceRow[];
  // The active workspace slug (from localStorage "jf_workspace"), used to scope
  // every dashboard fetch (issue #158).
  workspace: string;
  // The caller's role in the active workspace (issue #163): "admin" | "manage" |
  // "read_only" | "" (unknown / not a member). Carried per-workspace in the
  // bundle so the UI can gate actions by role.
  role: string;
  // undefined = fetch still in flight (callers may show their own shell);
  // null = confirmed anonymous (an AUTHORITATIVE 401/uncoded-403, never a wedge
  // timeout — see BundleRead in this file, card t_48319be9) — the auth gate.
  bundle: BundleData | null | undefined;
  // true once the fetch settled with no session (401) — auth gate.
  anon: boolean;
  // true when the bootstrap read produced NO answer (a 5xx or a dropped
  // connection) — a FAULT, never a sign-out. A page showing its loading state
  // reads this to also offer retry(); it must NOT be treated as "no session"
  // (card t_48319be9, and the reason the old 5s wedge was wrong).
  sessionError: boolean;
  // reload() re-fetches the bundle for the current workspace (e.g. after a
  // profile update or a workspace switch).
  reload: () => void;
  // retry() re-attempts the bootstrap after a FAULT. Distinct from reload(): it
  // exists for the unresolved case, so a caller can recover without assuming an
  // anonymous session (t_48319be9).
  retry: () => void;
}

const SessionContext = createContext<SessionCtx>({
  user: null,
  workspaces: [],
  workspace: "",
  role: "",
  bundle: undefined,
  anon: false,
  sessionError: false,
  reload: () => {},
  retry: () => {},
});

// Workspace-keyed bundle cache (issue #158): the cache key is the active
// workspace slug, so switching workspaces never serves another workspace's
// cached data.
//
// The cache holds only a SETTLED ANSWER — a signed-in payload or a confirmed
// anonymous one. An UNKNOWN failure (a 5xx, a dropped connection, a non-JSON
// body) is deliberately never cached: it is not an answer, and caching it would
// make a transient fault permanent for the workspace until a reload cleared it.
const bundleCache = new Map<string, BundleRead>();
const inflightByWorkspace = new Map<string, Promise<BundleRead>>();

/**
 * The result of one `dashboard-bundle` read, BEFORE it becomes the context's
 * `bundle` (card t_48319be9).
 *
 * THE THREE STATES ARE NOT INTERCHANGEABLE. The context exposes `bundle` as
 * `undefined` (still loading) / `BundleData` (signed in) / `null` (confirmed
 * anonymous), and every dashboard page reads `anon` (bundle === null) as its
 * SIGN-OUT gate (`if (anon) router.replace("/landing")`). So `error` must never
 * be folded into `null`:
 *
 *   * a starved host can lose a fixed race the user never notices (measured on
 *     the gate's own topology: 1-2 in 36 contended runs), and
 *   * a 5xx or a dropped connection is a FAULT, not a sign-out.
 *
 * Folding either into `null` bounced every page to `/landing` — a logout the
 * server never asked for, which is exactly the reported defect. `null` is
 * therefore reached ONLY from an authoritative refusal (a 401, or a 403 with no
 * tenancy code — the same rule `wsfetch.isUnauthStatus` applies everywhere
 * else); everything else is `error`, which leaves the app on its loading state
 * and lets the retry below try again.
 */
type BundleRead =
  | { kind: "session"; data: BundleData }
  | { kind: "anon" }
  | { kind: "error" };

/**
 * The upper bound on ONE `dashboard-bundle` read (card t_f955c2f3).
 *
 * #1041 removed the old 5s wedge — correctly, because that wedge bounded the
 * still-in-flight state by CALLING it anonymous — but it left the read with no
 * bound of any kind. A `fetch` the network accepts and then never answers (a TCP
 * connection held open, a half-dead proxy, a hung server) neither RESOLVES nor
 * REJECTS: `fetchBundle` never settles, so `bundle` stays `undefined` and
 * `sessionError` is never set. The visitor sits on the loading shell for ever
 * and the retry the fixed build added for a failed read is never offered.
 * Measured on the #1041 bytes: `/chat` with a never-answering
 * `dashboard-bundle` stayed unresolved with no retry however long it was driven.
 *
 * So the read is bounded by an `AbortController` whose abort lands in the SAME
 * `catch` as a dropped connection — an `error` read, never `anon` (a read that
 * produced NO answer is not a sign-out, which is the whole point of #1041).
 *
 * 20s, not the old 5s: the guard's own slow arm answers at 8s and that is a
 * LEGITIMATE session, so the bound must sit well above any real read — its job
 * is to catch a read that will never answer, not a slow one.
 */
const BUNDLE_READ_TIMEOUT_MS = 20_000;

function fetchBundle(workspace: string): Promise<BundleRead> {
  const key = workspace || "__default__";
  const cached = bundleCache.get(key);
  if (cached) return Promise.resolve(cached);
  if (!inflightByWorkspace.has(key)) {
    const q = workspace ? `?workspace=${encodeURIComponent(workspace)}` : "";
    // The bound, armed BEFORE the request (see BUNDLE_READ_TIMEOUT_MS). The
    // timer is cleared when the whole chain SETTLES below — not when the fetch
    // returns — because the body read can stall too (a server that sends its
    // headers and then stops talking hangs `await r.json()` just as well), and
    // the abort has to reach that path as an `error` read as well.
    const ac = new AbortController();
    const timer = window.setTimeout(() => ac.abort(), BUNDLE_READ_TIMEOUT_MS);
    const p = (async (): Promise<BundleRead> => {
      let r: Response;
      try {
        r = await fetch(`/api/rest/dashboard-bundle${q}`, { signal: ac.signal });
      } catch {
        // The request never got a response (offline, DNS, abort, or the bound
        // above firing). NOT anonymous.
        return { kind: "error" };
      }
      if (r.ok) {
        try {
          return { kind: "session", data: (await r.json()) as BundleData };
        } catch {
          // A 200 that is not JSON is a broken read, not a session.
          return { kind: "error" };
        }
      }
      // THE SCOPE REFUSAL OF THE BUNDLE ITSELF (t_da4b2031). A stale
      // `jf_workspace` makes THIS request carry a slug the session cannot
      // resolve, and the server answers 403 `workspace notFound`. Resolving
      // that to null would be wrong twice over: the merchant would lose the
      // workspace LIST (the one thing that can repair the selection — the
      // Shell reconciles off it), and `null` is the session context's
      // "confirmed anonymous" value, so every page would bounce to /landing.
      //
      // So the slug is dropped and the bundle is re-fetched UNSCOPED — the
      // request that returns the caller's real list — through this same
      // function, so the retry is cached and registered as the default-key
      // inflight: the `[workspace]` effect that the jf:workspace-changed event
      // fires reads it instead of issuing a second identical request. This is
      // the client-side "an unknown slug means no selection" rule, applied one
      // layer below the Shell so it covers first paint too.
      //
      // The recursion is depth 1: an UNSCOPED request can never be a scope
      // refusal (resolveAccountScope only rejects a slug that was sent).
      if (r.status === 403 && isScopeRefusal(await readErrorBody(r))) {
        dropCurrentWorkspaceScope();
        return fetchBundle("");
      }
      const body = await readErrorBody(r);
      // ONLY a refusal the transport layer calls a dead session is the server
      // saying "you are anonymous"; every other non-OK status is a fault.
      return isUnauthStatus(r.status, body) ? { kind: "anon" } : { kind: "error" };
    })()
      .then((res) => {
        window.clearTimeout(timer);
        if (res.kind !== "error") bundleCache.set(key, res);
        inflightByWorkspace.delete(key);
        return res;
      })
      .catch((): BundleRead => {
        // The chain itself threw. Clear the bound so it cannot abort a later
        // read on the shared signal, and report the fault the same way.
        window.clearTimeout(timer);
        inflightByWorkspace.delete(key);
        return { kind: "error" };
      });
    inflightByWorkspace.set(key, p);
  }
  return inflightByWorkspace.get(key)!;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [workspace, setWorkspace] = useState<string>("");
  const [bundle, setBundle] = useState<BundleData | null | undefined>(undefined);
  const [mounted, setMounted] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  // `sessionError` is the state the 5s wedge used to collapse into `anon`
  // (card t_48319be9): the bootstrap read produced NO answer (a 5xx or a dropped
  // connection). It is true ONLY for that fault — not while a read is in flight
  // and not for a confirmed-anonymous 401 — so a page can offer retry() without
  // mistaking a slow session for a signed-out one.
  const [sessionError, setSessionError] = useState(false);
  const reloadNonce = useRef(0);

  // Resolve the active workspace from the SESSION-CONFIRMED scope (t_da4b2031),
  // falling back to localStorage while no answer has arrived. Reading storage
  // directly left this state pointing at a slug the server had already rejected,
  // so a page keyed on `useSession().workspace` never re-fetched after the
  // reconciliation — the page kept showing data for a workspace that was gone.
  useEffect(() => {
    setWorkspace(activeWorkspaceScope());
  }, []);

  // Issue #170: keep the active workspace REACTIVE to switches.
  // Shell.switchWorkspace persists `jf_workspace` and dispatches a
  // "jf:workspace-changed" event; the `storage` event covers cross-tab changes.
  // Updating `workspace` here re-runs the workspace-keyed bundle fetch and lets
  // data pages key their load() on it, so switching workspaces re-fetches each
  // page's list for the new workspace.
  //
  // The `storage` leg also has to move the CONFIRMED scope (t_da4b2031): it is
  // the authority every scoped fetch reads, so a sign-out in ANOTHER TAB (which
  // clears jf_workspace) would otherwise leave this tab sending a slug whose
  // session is gone and 403ing on every request. A storage event carries the
  // new value, so the confirmed scope is re-derived from it rather than from
  // this tab's stale memory.
  useEffect(() => {
    const sync = () => setWorkspace(activeWorkspaceScope());
    const onStorage = (e: StorageEvent) => {
      if (e.key !== null && e.key !== "jf_workspace" && e.key !== "jf_space") return;
      if (!e.newValue) publishWorkspaceScope("");
      sync();
    };
    window.addEventListener("jf:workspace-changed", sync);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener("jf:workspace-changed", sync);
      window.removeEventListener("storage", onStorage);
    };
  }, []);

  useEffect(() => {
    setMounted(true);
    // Issue #226: fetch the bundle even when `workspace` is empty (anonymous /
    // incognito visitor with no jf_workspace in localStorage) so `anon` can flip
    // and the dashboard falls through to the Landing page instead of hanging
    // on the loading shell. fetchBundle already handles an empty workspace (no
    // ?workspace= query) and keys the cache under "__default__".
    //
    // THE CONFIRMED SCOPE (card t_da4b2031). `bundle` is the one place the
    // dashboard learns WHICH workspaces this session has, so it is the one place
    // that can turn the stored slug into a slug the server has confirmed. Until
    // this effect has an answer, every scoped fetch (workspaceQuery, and the
    // bundle itself) carries the raw stored value — the identical request the
    // pre-fix build made — so first paint is unchanged. Once an answer lands:
    //
    //   * a slug IN the list is published as confirmed, and nothing changes for
    //     the user;
    //   * a slug NOT in the list is dropped (clearUnknownWorkspaceScope) and the
    //     workspace the Shell selects is published instead, so no later request
    //     can carry the foreign slug. A 403 it produced on the way here was
    //     already handled by runLoad/wsfetch (drop the slug, keep the session) —
    //     it can never reach the /login redirect, which is the whole defect;
    //   * an EMPTY list publishes "" — the caller has no workspace, and a stored
    //     slug from a workspace they no longer have must not be sent again.
    if (bundle === undefined) return;
    const list = Array.isArray(bundle?.workspaces)
      ? bundle!.workspaces
      : Array.isArray(bundle?.spaces)
        ? bundle!.spaces
        : [];
    // `clearUnknownWorkspaceScope` is given the settled list: with zero
    // workspaces it drops the stored slug (the reported shape), and it is a
    // no-op only when there is no list at all — which cannot happen here,
    // because `bundle === undefined` returned above.
    const surviving = clearUnknownWorkspaceScope(list);
    // PUBLISH, not just remember (t_da4b2031): publishWorkspaceScope writes the
    // stored key too (matching what the Shell's own reconciler does, so the two
    // agree) and notifies the session context when the value changes — which is
    // what re-runs this effect's sibling fetches and every page keyed on
    // `useSession().workspace` for the workspace the caller actually has.
    publishWorkspaceScope(surviving || list[0]?.slug || "");
  }, [bundle]);

  useEffect(() => {
    const key = workspace || "__default__";
    const settled = bundleCache.get(key);
    if (settled) {
      setBundle(settled.kind === "session" ? settled.data : null);
      setSessionError(false);
      return;
    }
    // The three settled states map onto the context EXACTLY (card t_48319be9):
    //   session → the payload,  anon → null (the auth gate),
    //   error   → undefined (unresolved — NOT a sign-out) with sessionError true,
    // so the loading branch can offer a retry instead of hanging on "…" forever
    // (the reason the 5s wedge existed) — WITHOUT inventing an anonymous session
    // to pay for it.
    setSessionError(false);
    fetchBundle(workspace).then((res) => {
      setBundle(res.kind === "session" ? res.data : res.kind === "anon" ? null : undefined);
      setSessionError(res.kind === "error");
    });
  }, [workspace, retryNonce]);

  // Issue #69: reload() (used after session-rotating actions like 2FA enable /
  // disable / password change) must keep showing the PREVIOUS bundle until the
  // fresh fetch resolves — never collapse the sidebar / identity to "…" in the
  // interim. If the refetch fails or returns blank, we preserve the previous
  // bundle instead of nulling the UI. A nonce guards against a slow, stale
  // reload clobbering a newer one.
  const reload = useCallback(() => {
    const key = workspace || "__default__";
    bundleCache.delete(key);
    inflightByWorkspace.delete(key);
    reloadNonce.current += 1;
    const nonce = reloadNonce.current;
    fetchBundle(workspace).then((res) => {
      if (nonce !== reloadNonce.current) return; // superseded by a newer reload
      if (res.kind === "session") setBundle(res.data);
      else if (res.kind === "anon") setBundle(null);
      // `error` (fault / offline) → keep the previous bundle rather than
      // blanking the app shell during a session rotation.
    });
  }, [workspace]);

  // retry() re-attempts the bootstrap after a FAULT (an `error` read, not a
  // confirmed-anonymous one). It drops only the in-flight entry — an `error` was
  // never cached — and bumps the nonce the bootstrap effect is keyed on, so the
  // retry re-fetches whichever workspace is active now (t_48319be9).
  const retry = useCallback(() => {
    const key = workspace || "__default__";
    bundleCache.delete(key);
    inflightByWorkspace.delete(key);
    setSessionError(false);
    setRetryNonce((n: number) => n + 1);
  }, [workspace]);

  const user = bundle?.me ?? null;
  // Issue #452: prefer the renamed `workspaces` field; fall back to the legacy
  // `spaces` field for an older payload.
  const workspaces = Array.isArray(bundle?.workspaces)
    ? bundle!.workspaces
    : Array.isArray(bundle?.spaces)
      ? bundle!.spaces
      : [];
  // Issue #163: the caller's role in the active workspace, carried
  // per-workspace in the bundle. Used to gate write/member/CHIP actions in the UI.
  const activeWorkspace = workspaces.find((s) => s.slug === workspace);
  const role = activeWorkspace?.role || "";

  return (
    <SessionContext.Provider
      value={{ user, workspaces, workspace, role, bundle, anon: mounted && bundle === null, sessionError, reload, retry }}
    >
      {children}
    </SessionContext.Provider>
  );
}

// useSession(): me + workspaces, cached at module level per workspace so every page
// shares the single dashboard-bundle round-trip. Callers that need the raw
// landing sections (dashboard) read `bundle`.
export function useSession() {
  return useContext(SessionContext);
}
