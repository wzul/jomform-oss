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
import { workspaceScope } from "../lib/workspace";

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
  // null = confirmed anonymous (401 / wedge timeout) — the auth gate.
  bundle: BundleData | null | undefined;
  // true once the fetch settled with no session (401) — auth gate.
  anon: boolean;
  // reload() re-fetches the bundle for the current workspace (e.g. after a
  // profile update or a workspace switch).
  reload: () => void;
};

const SessionContext = createContext<SessionCtx>({
  user: null,
  workspaces: [],
  workspace: "",
  role: "",
  bundle: undefined,
  anon: false,
  reload: () => {},
});

// Workspace-keyed bundle cache (issue #158): the cache key is the active
// workspace slug, so switching workspaces never serves another workspace's
// cached data.
const bundleCache = new Map<string, BundleData | null>();
const inflightByWorkspace = new Map<string, Promise<BundleData | null>>();

async function fetchBundle(workspace: string): Promise<BundleData | null> {
  const key = workspace || "__default__";
  if (bundleCache.has(key)) return bundleCache.get(key) ?? null;
  if (!inflightByWorkspace.has(key)) {
    const q = workspace ? `?workspace=${encodeURIComponent(workspace)}` : "";
    const p = fetch(`/api/rest/dashboard-bundle${q}`)
      .then((r) => {
        if (!r.ok) return null;
        return r.json();
      })
      .then((d) => {
        bundleCache.set(key, d);
        inflightByWorkspace.delete(key);
        return d;
      })
      .catch(() => {
        inflightByWorkspace.delete(key);
        return null;
      });
    inflightByWorkspace.set(key, p);
  }
  return inflightByWorkspace.get(key)!;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [workspace, setWorkspace] = useState<string>("");
  const [bundle, setBundle] = useState<BundleData | null | undefined>(undefined);
  const [mounted, setMounted] = useState(false);
  const reloadNonce = useRef(0);

  // Resolve the active workspace from localStorage (same key the Shell uses).
  useEffect(() => {
    setWorkspace(workspaceScope());
  }, []);

  // Issue #170: keep the active workspace REACTIVE to switches.
  // Shell.switchWorkspace persists `jf_workspace` and dispatches a
  // "jf:workspace-changed" event; the `storage` event covers cross-tab changes.
  // Updating `workspace` here re-runs the workspace-keyed bundle fetch and lets
  // data pages key their load() on it, so switching workspaces re-fetches each
  // page's list for the new workspace.
  useEffect(() => {
    const sync = () => setWorkspace(workspaceScope());
    window.addEventListener("jf:workspace-changed", sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener("jf:workspace-changed", sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  useEffect(() => {
    setMounted(true);
    // Issue #226: fetch the bundle even when `workspace` is empty (anonymous /
    // incognito visitor with no jf_workspace in localStorage) so `anon` can flip
    // and the dashboard falls through to the Landing page instead of hanging
    // on the loading shell. fetchBundle already handles an empty workspace (no
    // ?workspace= query) and keys the cache under "__default__".
    const key = workspace || "__default__";
    if (bundleCache.has(key)) {
      setBundle(bundleCache.get(key) ?? null);
      return;
    }
    // Issue #48 wedge guard: if the bundle hasn't resolved in ~5s, treat the
    // visitor as anonymous so a hung request can never leave the app stuck on
    // the loading screen. A late success below overrides this null.
    const timer = window.setTimeout(() => {
      setBundle((prev) => (prev === undefined ? null : prev));
    }, 5000);
    fetchBundle(workspace).then((d) => {
      window.clearTimeout(timer);
      setBundle(d ?? null);
    });
    return () => window.clearTimeout(timer);
  }, [workspace]);

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
    fetchBundle(workspace).then((d) => {
      if (nonce !== reloadNonce.current) return; // superseded by a newer reload
      if (d) setBundle(d);
      // d == null (401 / wedge / failure) → keep the previous bundle rather
      // than blanking the app shell during a session rotation.
    });
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
    <SessionContext.Provider value={{ user, workspaces, workspace, role, bundle, anon: mounted && bundle === null, reload }}>
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
