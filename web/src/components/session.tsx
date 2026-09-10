"use client";

// Issue #29: session context — `me` + `spaces` fetched ONCE per browser
// session (dashboard-bundle) and shared across the Shell and every page via
// React context. Kills the per-mount /api/auth/me round-trip that each page
// previously paid serially before fetching its own data.
//
// Issue #158: the bundle is now SPACE-KEYED. The dashboard-bundle carries
// space-scoped data (chip_status, keys, domains, webhooks, einvoice,
// products, summary, sales, analytics) alongside user-level data (me,
// spaces). Because each space owns its own account, the bundle must be
// fetched with ?workspace=<slug> and cached per space — switching spaces must
// re-fetch the bundle for the newly selected space, never serve the previous
// space's cached data (cross-space leakage).
//
// Bundle shape (api/internal/httpapi/rest_dashboard.go): one authed request
// carrying me (id/email/name/totp_enabled), spaces, chip_status, keys,
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
  totp_enabled?: boolean;
  default_space_id?: string; // issue #164 (empty = no default → space-picker on login)
  is_super_admin?: boolean; // issue #387 (site super-admin, #385)
};

export type SpaceRow = { id: string; name: string; slug: string; role?: string };

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
  spaces?: SpaceRow[];
  chip_status?: ChipStatusShape | null;
  keys?: { id: string; name: string; scopes: string[] }[];
  domains?: { id: string; domain: string; provider?: string; verified: boolean }[];
  webhooks?: { id: string; url: string; events: string[] }[];
  social?: { provider: string; email?: string; linked_at?: string }[];
  einvoice?: { tin?: string; msic?: string } | null;
} & BundleLanding;

type SessionCtx = {
  user: SessionUser | null;
  spaces: SpaceRow[];
  // The active space slug (from localStorage "jf_workspace"), used to scope
  // every dashboard fetch (issue #158).
  space: string;
  // The caller's role in the active space (issue #163): "admin" | "manage" |
  // "read_only" | "" (unknown / not a member). Carried per-space in the
  // bundle so the UI can gate actions by role.
  role: string;
  // undefined = fetch still in flight (callers may show their own shell);
  // null = confirmed anonymous (401 / wedge timeout) — the auth gate.
  bundle: BundleData | null | undefined;
  // true once the fetch settled with no session (401) — auth gate.
  anon: boolean;
  // reload() re-fetches the bundle for the current space (e.g. after a
  // profile update or a space switch).
  reload: () => void;
};

const SessionContext = createContext<SessionCtx>({
  user: null,
  spaces: [],
  space: "",
  role: "",
  bundle: undefined,
  anon: false,
  reload: () => {},
});

// Space-keyed bundle cache (issue #158): the cache key is the active space
// slug, so switching spaces never serves another space's cached data.
const bundleCache = new Map<string, BundleData | null>();
const inflightBySpace = new Map<string, Promise<BundleData | null>>();

async function fetchBundle(space: string): Promise<BundleData | null> {
  const key = space || "__default__";
  if (bundleCache.has(key)) return bundleCache.get(key) ?? null;
  if (!inflightBySpace.has(key)) {
    const q = space ? `?workspace=${encodeURIComponent(space)}` : "";
    const p = fetch(`/api/rest/dashboard-bundle${q}`)
      .then((r) => {
        if (!r.ok) return null;
        return r.json();
      })
      .then((d) => {
        bundleCache.set(key, d);
        inflightBySpace.delete(key);
        return d;
      })
      .catch(() => {
        inflightBySpace.delete(key);
        return null;
      });
    inflightBySpace.set(key, p);
  }
  return inflightBySpace.get(key)!;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [space, setSpace] = useState<string>("");
  const [bundle, setBundle] = useState<BundleData | null | undefined>(undefined);
  const [mounted, setMounted] = useState(false);
  const reloadNonce = useRef(0);

  // Resolve the active space from localStorage (same key the Shell uses).
  useEffect(() => {
    setSpace(workspaceScope());
  }, []);

  // Issue #170: keep the active space REACTIVE to switches. Shell.switchSpace
  // persists `jf_workspace` and dispatches a "jf:space-changed" event; the
  // `storage` event covers cross-tab changes. Updating `space` here re-runs
  // the space-keyed bundle fetch and lets data pages key their load() on it,
  // so switching spaces re-fetches each page's list for the new space.
  useEffect(() => {
    const sync = () => setSpace(workspaceScope());
    window.addEventListener("jf:space-changed", sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener("jf:space-changed", sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  useEffect(() => {
    setMounted(true);
    // Issue #226: fetch the bundle even when `space` is empty (anonymous /
    // incognito visitor with no jf_workspace in localStorage) so `anon` can flip
    // and the dashboard falls through to the Landing page instead of hanging
    // on the loading shell. fetchBundle already handles an empty space (no
    // ?workspace= query) and keys the cache under "__default__".
    const key = space || "__default__";
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
    fetchBundle(space).then((d) => {
      window.clearTimeout(timer);
      setBundle(d ?? null);
    });
    return () => window.clearTimeout(timer);
  }, [space]);

  // Issue #69: reload() (used after session-rotating actions like 2FA enable /
  // disable / password change) must keep showing the PREVIOUS bundle until the
  // fresh fetch resolves — never collapse the sidebar / identity to "…" in the
  // interim. If the refetch fails or returns blank, we preserve the previous
  // bundle instead of nulling the UI. A nonce guards against a slow, stale
  // reload clobbering a newer one.
  const reload = useCallback(() => {
    const key = space || "__default__";
    bundleCache.delete(key);
    inflightBySpace.delete(key);
    reloadNonce.current += 1;
    const nonce = reloadNonce.current;
    fetchBundle(space).then((d) => {
      if (nonce !== reloadNonce.current) return; // superseded by a newer reload
      if (d) setBundle(d);
      // d == null (401 / wedge / failure) → keep the previous bundle rather
      // than blanking the app shell during a session rotation.
    });
  }, [space]);

  const user = bundle?.me ?? null;
  const spaces = Array.isArray(bundle?.spaces) ? bundle!.spaces : [];
  // Issue #163: the caller's role in the active space, carried per-space in
  // the bundle. Used to gate write/member/CHIP actions in the UI.
  const activeSpace = spaces.find((s) => s.slug === space);
  const role = activeSpace?.role || "";

  return (
    <SessionContext.Provider value={{ user, spaces, space, role, bundle, anon: mounted && bundle === null, reload }}>
      {children}
    </SessionContext.Provider>
  );
}

// useSession(): me + spaces, cached at module level per space so every page
// shares the single dashboard-bundle round-trip. Callers that need the raw
// landing sections (dashboard) read `bundle`.
export function useSession() {
  return useContext(SessionContext);
}
