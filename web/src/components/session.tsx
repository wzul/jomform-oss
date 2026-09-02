"use client";

// Issue #29: session context — `me` + `spaces` fetched ONCE per browser
// session (dashboard-bundle) and shared across the Shell and every page via
// React context. Kills the per-mount /api/auth/me round-trip that each page
// previously paid serially before fetching its own data.
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

export type SessionUser = {
  id?: string;
  email: string;
  name?: string;
  totp_enabled?: boolean;
};

export type SpaceRow = { id: string; name: string; slug: string };

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
  // undefined = fetch still in flight (callers may show their own shell);
  // null = confirmed anonymous (401 / wedge timeout) — the auth gate.
  bundle: BundleData | null | undefined;
  // true once the fetch settled with no session (401) — auth gate.
  anon: boolean;
  // reload() re-fetches the bundle (e.g. after a profile update).
  reload: () => void;
};

const SessionContext = createContext<SessionCtx>({
  user: null,
  spaces: [],
  bundle: undefined,
  anon: false,
  reload: () => {},
});

let cachedBundle: BundleData | null = null;
let inflight: Promise<BundleData | null> | null = null;

async function fetchBundle(): Promise<BundleData | null> {
  if (cachedBundle) return cachedBundle;
  if (!inflight) {
    inflight = fetch("/api/rest/dashboard-bundle")
      .then((r) => {
        if (!r.ok) return null;
        return r.json();
      })
      .then((d) => {
        cachedBundle = d;
        inflight = null;
        return d;
      })
      .catch(() => {
        inflight = null;
        return null;
      });
  }
  return inflight;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [bundle, setBundle] = useState<BundleData | null | undefined>(cachedBundle ?? undefined);
  const [mounted, setMounted] = useState(false);
  const reloadNonce = useRef(0);

  useEffect(() => {
    setMounted(true);
    if (cachedBundle) return;
    // Issue #48 wedge guard: if the bundle hasn't resolved in ~5s, treat the
    // visitor as anonymous so a hung request can never leave the app stuck on
    // the loading screen. A late success below overrides this null.
    const timer = window.setTimeout(() => {
      setBundle((prev) => (prev === undefined ? null : prev));
    }, 5000);
    fetchBundle().then((d) => {
      window.clearTimeout(timer);
      setBundle(d ?? null);
    });
    return () => window.clearTimeout(timer);
  }, []);

  // Issue #69: reload() (used after session-rotating actions like 2FA enable /
  // disable / password change) must keep showing the PREVIOUS bundle until the
  // fresh fetch resolves — never collapse the sidebar / identity to "…" in the
  // interim. If the refetch fails or returns blank, we preserve the previous
  // bundle instead of nulling the UI. A nonce guards against a slow, stale
  // reload clobbering a newer one.
  const reload = useCallback(() => {
    cachedBundle = null;
    inflight = null;
    reloadNonce.current += 1;
    const nonce = reloadNonce.current;
    fetchBundle().then((d) => {
      if (nonce !== reloadNonce.current) return; // superseded by a newer reload
      if (d) setBundle(d);
      // d == null (401 / wedge / failure) → keep the previous bundle rather
      // than blanking the app shell during a session rotation.
    });
  }, []);

  const user = bundle?.me ?? null;
  const spaces = Array.isArray(bundle?.spaces) ? bundle!.spaces : [];

  return (
    <SessionContext.Provider value={{ user, spaces, bundle, anon: mounted && bundle === null, reload }}>
      {children}
    </SessionContext.Provider>
  );
}

// useSession(): me + spaces, cached at module level so every page shares the
// single dashboard-bundle round-trip. Callers that need the raw landing
// sections (dashboard) read `bundle`.
export function useSession() {
  return useContext(SessionContext);
}