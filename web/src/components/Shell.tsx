"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useI18n, LangToggle } from "./i18n";
import { useSession } from "./session";
import { SpaceSwitcher } from "./SpaceSwitcher";

// AppShell: fixed left sidebar + topbar + content area. Light theme.
// Issue #29: nav links are Next <Link> (client-side transitions + prefetch,
// no full document reload) and me/spaces come from the shared session
// context (one dashboard-bundle fetch per session), not per-mount fetches.
export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "forms" | "products" | "sales" | "deliveries" | "subscriptions" | "settings" | "profile" | "media";
}) {
  const { user, spaces, reload } = useSession();
  const [space, setSpace] = useState<string>("");
  const [menuOpen, setMenuOpen] = useState(false);
  const { t } = useI18n();
  const router = useRouter();
  const drawerRef = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Issue #133: the user's initial letter (or "?"), used as the avatar
  // fallback when no profile picture (avatar_url) is set or it fails to load.
  const initial = ((user?.name || user?.email || "?")[0] || "?").toUpperCase();
  const [imgFailed, setImgFailed] = useState(false);

  // Avatar can change after a profile edit; reset the failure flag when the
  // URL changes so a new image isn't stuck hidden behind a stale onError.
  useEffect(() => setImgFailed(false), [user?.avatar_url]);

  // Renders the user's face: uploaded avatar when set and not failed, else the
  // initial-letter circle.
  function UserAvatar({ className }: { className: string }) {
    const url = user?.avatar_url;
    if (url && !imgFailed) {
      // eslint-disable-next-line @next/next/no-img-element
      return (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={url}
          alt={user?.name || user?.email || ""}
          onError={() => setImgFailed(true)}
          className={`${className} object-cover`}
        />
      );
    }
    return (
      <div className={`${className} flex items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary)]`}>
        {initial}
      </div>
    );
  }

  // Issue #104: mobile drawer keyboard + focus management. When the drawer
  // opens, move focus to the close button; trap Tab inside the drawer; close
  // on Escape. Prevents a focus trap and keeps keyboard users oriented.
  useEffect(() => {
    if (!menuOpen) return;
    closeBtnRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setMenuOpen(false);
        return;
      }
      if (e.key !== "Tab") return;
      const focusables = drawerRef.current?.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), select, input, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (!focusables || focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [menuOpen]);

  useEffect(() => {
    const sp = Array.isArray(spaces) ? spaces : [];
    if (!sp.length) return;
    const saved = localStorage.getItem("jf_space");
    if (saved && sp.some((s) => s.slug === saved)) {
      setSpace(saved);
    } else {
      setSpace(sp[0].slug);
      localStorage.setItem("jf_space", sp[0].slug);
    }
  }, [spaces]);

  function switchSpace(slug: string) {
    setSpace(slug);
    localStorage.setItem("jf_space", slug);
    // Issue #170: notify the session context (and any data page keyed on
    // useSession().space) that the active space changed, so page lists
    // re-fetch for the new space without a manual refresh.
    window.dispatchEvent(new Event("jf:space-changed"));
    // Issue #158: the session bundle is now SPACE-KEYED (see session.tsx), so
    // switching spaces re-fetches the bundle for the newly selected space —
    // no hard reload needed. reload() drops the cached bundle for the new
    // space and re-fetches it, so the dashboard landing sections (products,
    // summary, sales, analytics) and the account-level settings (chip, keys,
    // domains, webhooks, einvoice) all switch to the new space's account.
    reload();
  }

  // Issue #164: set/clear the user's default space (server-side). Empty id
  // clears it, so the space-picker prompt returns on the next login.
  async function setDefaultSpace(spaceId: string) {
    try {
      await fetch("/api/auth/profile/default-space", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ space_id: spaceId }),
      });
    } catch {
      // Best-effort; the picker/profile surfaces errors on its own path.
    }
    reload();
  }

  async function signOut() {
    await fetch("/api/auth/logout", { method: "POST" });
    router.replace("/login");
    window.location.href = "/login";
  }

  const nav = [
    { key: "dashboard", href: "/", label: t("nav.dashboard"), icon: "◧" },
    { key: "forms", href: "/forms", label: t("nav.forms"), icon: "✎" },
    { key: "products", href: "/products", label: t("prod.title"), icon: "◧" },
    // Issue #126: dedicated Sales page, between Products and Settings.
    { key: "sales", href: "/sales", label: t("nav.sales"), icon: "💰" },
    // Issue #194: email delivery status view.
    { key: "deliveries", href: "/deliveries", label: t("del.title"), icon: "✉" },
    { key: "subscriptions", href: "/subscriptions", label: t("sub.title"), icon: "⟳" },
    // Issue #180: media library (WordPress-style).
    { key: "media", href: "/media", label: t("media.title"), icon: "🖼" },
    { key: "settings", href: "/settings", label: t("nav.settings"), icon: "⚙" },
    { key: "profile", href: "/profile", label: t("prof.title"), icon: "👤" },
  ];

  return (
    <div className="flex min-h-screen">
      {/* Issue #104: skip-to-content link for keyboard / screen-reader users. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-[#5e6ad2] focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-white"
      >
        Skip to content
      </a>
      {/* sidebar */}
      <aside className="sticky top-0 hidden h-screen w-56 shrink-0 self-start flex-col border-r border-[var(--jf-border)] bg-white md:flex">
        <div className="flex items-center gap-2 px-5 py-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-[15px] font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <nav className="mt-2 min-h-0 flex-1 space-y-0.5 overflow-y-auto px-3">
          {nav.map((n) => (
            <Link
              key={n.key}
              href={n.href}
              aria-current={active === n.key ? "page" : undefined}
              className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors ${
                active === n.key
                  ? "bg-indigo-50 font-medium text-[var(--jf-primary)]"
                  : "text-[var(--jf-muted)] hover:bg-slate-50 hover:text-[var(--jf-text)]"
              }`}
            >
              <span className="w-4 text-center text-xs opacity-70">{n.icon}</span>
              {n.label}
            </Link>
          ))}
        </nav>
        {/* user card — flex-none at the bottom of the full-height aside (always visible) */}
        <div className="shrink-0 border-t border-[var(--jf-border)] p-3">
          <div className="flex items-center gap-2.5 rounded-lg px-2 py-2">
            {/* Issue #133: uploaded profile picture, falling back to the initial letter. */}
            <UserAvatar className="h-8 w-8 rounded-full" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-medium" title={user?.name || user?.email || ""}>
                {user?.name || user?.email || "…"}
              </p>
              {user?.name && (
                <p className="truncate text-[11px] text-[var(--jf-muted)]" title={user?.email || ""}>
                  {user?.email || ""}
                </p>
              )}
              <button
                onClick={signOut}
                className="text-[11px] text-[var(--jf-muted)] hover:text-red-500"
              >
                {t("nav.signout")}
              </button>
            </div>
          </div>
          <SpaceSwitcher
            spaces={spaces}
            value={space}
            onSwitch={switchSpace}
            defaultSpaceId={user?.default_space_id}
            onSetDefault={setDefaultSpace}
            className="mt-2 px-2"
          />
          <div className="mt-1 px-2">
            <LangToggle />
          </div>
        </div>
      </aside>

      {/* main */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* mobile topbar */}
        <header className="sticky top-0 z-30 flex items-center justify-between border-b border-[var(--jf-border)] bg-white px-4 py-2.5 md:hidden">
          <button
            onClick={() => setMenuOpen(true)}
            aria-label="Open menu"
            className="-ml-1 rounded-md p-1.5 text-[var(--jf-text)] hover:bg-slate-50"
          >
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="M3 5h14M3 10h14M3 15h14" />
            </svg>
          </button>
          <span className="text-sm font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
          <LangToggle />
        </header>

        {/* mobile drawer */}
        {menuOpen && (
          <div className="fixed inset-0 z-40 md:hidden" onClick={() => setMenuOpen(false)}>
            <div className="absolute inset-0 bg-black/30" />
            <div
              ref={drawerRef}
              role="dialog"
              aria-modal="true"
              aria-label="Navigation menu"
              className="absolute left-0 top-0 h-full w-64 bg-white shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-4 py-3">
                <span className="text-sm font-semibold">
                  Jom<span className="text-[var(--jf-primary)]">Form</span>
                </span>
                <button
                  ref={closeBtnRef}
                  onClick={() => setMenuOpen(false)}
                  aria-label="Close menu"
                  className="rounded-md p-1 text-[var(--jf-muted)] hover:bg-slate-50"
                >
                  ✕
                </button>
              </div>
              <nav className="space-y-0.5 p-3">
                {nav.map((n) => (
                  <Link
                    key={n.key}
                    href={n.href}
                    onClick={() => setMenuOpen(false)}
                    className={`flex items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm ${
                      active === n.key
                        ? "bg-indigo-50 font-medium text-[var(--jf-primary)]"
                        : "text-[var(--jf-muted)] hover:bg-slate-50"
                    }`}
                  >
                    <span className="w-4 text-center text-xs opacity-70">{n.icon}</span>
                    {n.label}
                  </Link>
                ))}
              </nav>
              {/* issue #41: space selector reachable from the mobile drawer —
                  same shared component as the desktop sidebar */}
              <div className="border-t border-[var(--jf-border)] px-4 py-3">
                <SpaceSwitcher
                  spaces={spaces}
                  value={space}
                  onSwitch={switchSpace}
                  defaultSpaceId={user?.default_space_id}
                  onSetDefault={setDefaultSpace}
                />
              </div>
              <div className="absolute bottom-4 left-0 right-0 px-4">
                <div className="mb-2 flex items-center gap-2">
                  {/* Issue #133: profile picture or initial-letter fallback. */}
                  <UserAvatar className="h-8 w-8 rounded-full" />
                  <p
                    className="truncate text-xs text-[var(--jf-muted)]"
                    title={user?.name ? `${user.name} (${user?.email || ""})` : user?.email || ""}
                  >
                    {user?.name || user?.email || "…"}
                  </p>
                </div>
                <button onClick={signOut} className="w-full rounded-lg border border-[var(--jf-border)] py-2 text-sm text-slate-600">
                  {t("nav.signout")}
                </button>
              </div>
            </div>
          </div>
        )}

        <div id="main-content" className="flex flex-1 flex-col">{children}</div>
      </div>
    </div>
  );
}