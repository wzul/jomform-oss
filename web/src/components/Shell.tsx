"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useI18n } from "./i18n";
import { useSession } from "./session";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

// AppShell: fixed left sidebar + topbar + content area. Light theme.
// Issue #29: nav links are Next <Link> (client-side transitions + prefetch,
// no full document reload) and me/workspaces come from the shared session
// context (one dashboard-bundle fetch per session), not per-mount fetches.
export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "forms" | "templates" | "products" | "sales" | "spam" | "deliveries" | "subscriptions" | "settings" | "profile" | "media" | "feedback" | "chat" | "admin";
}) {
  const { user, workspaces, reload } = useSession();
  const [workspace, setWorkspace] = useState<string>("");
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
    const ws = Array.isArray(workspaces) ? workspaces : [];
    if (!ws.length) return;
    const saved = localStorage.getItem("jf_workspace");
    if (saved && ws.some((s) => s.slug === saved)) {
      setWorkspace(saved);
    } else {
      setWorkspace(ws[0].slug);
      localStorage.setItem("jf_workspace", ws[0].slug);
    }
  }, [workspaces]);

  function switchWorkspace(slug: string) {
    setWorkspace(slug);
    localStorage.setItem("jf_workspace", slug);
    // Issue #170: notify the session context (and any data page keyed on
    // useSession().workspace) that the active workspace changed, so page lists
    // re-fetch for the new workspace without a manual refresh.
    window.dispatchEvent(new Event("jf:workspace-changed"));
    // Issue #158: the session bundle is now WORKSPACE-KEYED (see session.tsx),
    // so switching workspaces re-fetches the bundle for the newly selected
    // workspace — no hard reload needed. reload() drops the cached bundle for
    // the new workspace and re-fetches it, so the dashboard landing sections
    // (products, summary, sales, analytics) and the account-level settings
    // (chip, keys, domains, webhooks, einvoice) all switch to the new
    // workspace's account.
    reload();
  }

  // Issue #164: set/clear the user's default workspace (server-side). Empty id
  // clears it, so the workspace-picker prompt returns on the next login.
  async function setDefaultWorkspace(workspaceId: string) {
    try {
      await fetch("/api/auth/profile/default-workspace", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workspace_id: workspaceId }),
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

  let nav = [
    { key: "dashboard", href: "/", label: t("nav.dashboard"), icon: "◧" },
    { key: "forms", href: "/forms", label: t("nav.forms"), icon: "✎" },
    // Issue #457: merchant-managed templates (save/edit/delete own templates).
    { key: "templates", href: "/templates", label: t("nav.templates"), icon: "🗂" },
    { key: "products", href: "/products", label: t("prod.title"), icon: "◧" },
    // Issue #126: dedicated Sales page, between Products and Settings.
    { key: "sales", href: "/sales", label: t("nav.sales"), icon: "💰" },
    // Issue #245: AI spam review queue.
    { key: "spam", href: "/spam", label: t("nav.spam"), icon: "⚠" },
    // Issue #194: email delivery status view.
    { key: "deliveries", href: "/deliveries", label: t("del.title"), icon: "✉" },
    { key: "subscriptions", href: "/subscriptions", label: t("sub.title"), icon: "⟳" },
    // Issue #180: media library (WordPress-style).
    { key: "media", href: "/media", label: t("media.title"), icon: "🖼" },
    { key: "settings", href: "/settings", label: t("nav.settings"), icon: "⚙" },
    { key: "profile", href: "/profile", label: t("prof.title"), icon: "👤" },
    // In-app feedback: a plain link for EVERY signed-in user, deliberately NOT
    // gated on the super-admin role. The feedback form is the system's, not a
    // workspace's, so reaching it must not depend on owning an account — the
    // same reason /admin (below) is the only role-gated entry in this list.
    { key: "feedback", href: "/feedback", label: t("nav.feedback"), icon: "💬" },
    // In-app assistant (phase 1): read-only over the user's own data, plus
    // filing feedback. Placed next to Feedback because that is where a filing
    // lands — the two surfaces are one story.
    { key: "chat", href: "/chat", label: t("nav.chat"), icon: "✨" },
  ];

  // Issue #387: super-admins (#385) get a platform-wide Admin nav link.
  if (user?.is_super_admin) {
    nav.push({ key: "admin", href: "/admin", label: t("nav.admin"), icon: "🛡" });
  }

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
              className={`flex items-center gap-2.5 rounded-lg px-3 py-3 text-sm transition-colors ${
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
              <p className="truncate text-sm font-medium" title={user?.name || user?.email || ""}>
                {user?.name || user?.email || "…"}
              </p>
              {user?.name && (
                <p className="truncate text-xs text-[var(--jf-muted)]" title={user?.email || ""}>
                  {user?.email || ""}
                </p>
              )}
              {/* Mobile audit: this was a 46x17 text link (11px). It is a real
                  action, so it gets a 44px-tall box and body-size text. */}
              <button
                onClick={signOut}
                className="mt-1 flex min-h-11 items-center rounded-md px-2 text-sm text-[var(--jf-muted)] hover:bg-slate-50 hover:text-red-500"
              >
                {t("nav.signout")}
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* main */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* desktop topbar — workspace switcher (issue #380). The language
            switcher moved to /profile (language placement): the dashboard
            language is PER-PROFILE, so it belongs on the profile page, not in
            every page's chrome. Mobile audit: the switcher chip truncates a
            long workspace name; the bar wraps instead of overflowing so the
            full chip can grow. */}
        <header className="sticky top-0 z-30 hidden flex-wrap items-center justify-end gap-3 border-b border-[var(--jf-border)] bg-white px-4 py-2.5 md:flex">
          <WorkspaceSwitcher
            workspaces={workspaces}
            value={workspace}
            onSwitch={switchWorkspace}
            defaultWorkspaceId={user?.default_workspace_id}
            onSetDefault={setDefaultWorkspace}
          />
        </header>
        {/* mobile topbar. The language control moved to /profile (language
            placement). The logo takes the remaining width so the bar still
            reads as hamburger + brand, with no dead space where the language
            select used to sit. */}
        <header className="sticky top-0 z-30 flex items-center justify-between gap-2 border-b border-[var(--jf-border)] bg-white px-2 py-2 md:hidden">
          {/* Mobile audit: this button measured 32x32 — under the 44x44 tap
              floor. h-11/w-11 gives it a 44px box with the icon unchanged. */}
          <button
            onClick={() => setMenuOpen(true)}
            aria-label="Open menu"
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-[var(--jf-text)] hover:bg-slate-50"
          >
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="M3 5h14M3 10h14M3 15h14" />
            </svg>
          </button>
          <span className="flex-1 truncate text-right text-sm font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
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
              className="absolute left-0 top-0 flex h-full w-64 flex-col bg-white shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex shrink-0 items-center justify-between border-b border-[var(--jf-border)] px-4 py-3">
                <span className="text-sm font-semibold">
                  Jom<span className="text-[var(--jf-primary)]">Form</span>
                </span>
                {/* Mobile audit: the close affordance measured 21x32. h-11/w-11
                    gives it the 44px box (same icon). */}
                <button
                  ref={closeBtnRef}
                  onClick={() => setMenuOpen(false)}
                  aria-label="Close menu"
                  className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-[var(--jf-muted)] hover:bg-slate-50"
                >
                  ✕
                </button>
              </div>
              {/* Workspace selector reachable from the mobile drawer (issue #41),
                  same shared component as the desktop topbar (issue #380).
                  Issue #460: the dropdown panel is rendered in flow (fullWidth)
                  so it reads as attached to its trigger and is never clipped.
                  Mobile audit: it used to sit at the END of the scrollable body,
                  so on a 640px-tall phone it started below the fold (y 596..643
                  inside a body ending at 537) and the hit-test at its centre
                  returned "Sign out". It is now a shrink-0 row above the nav, so
                  it is visible without discovering the body scroll; the nav
                  below it keeps its own scroll for shorter viewports. */}
              <div className="shrink-0 border-b border-[var(--jf-border)] px-4 py-3">
                <WorkspaceSwitcher
                  workspaces={workspaces}
                  value={workspace}
                  onSwitch={switchWorkspace}
                  fullWidth
                  defaultWorkspaceId={user?.default_workspace_id}
                  onSetDefault={setDefaultWorkspace}
                />
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto">
                <nav className="space-y-0.5 p-3">
                  {nav.map((n) => (
                    <Link
                      key={n.key}
                      href={n.href}
                      onClick={() => setMenuOpen(false)}
                      className={`flex min-h-11 items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm ${
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
              </div>
              <div className="shrink-0 border-t border-[var(--jf-border)] px-4 py-3">
                <div className="mb-2 flex items-center gap-2">
                  {/* Issue #133: profile picture or initial-letter fallback. */}
                  <UserAvatar className="h-8 w-8 rounded-full" />
                  <p
                    className="truncate text-sm text-[var(--jf-muted)]"
                    title={user?.name ? `${user.name} (${user?.email || ""})` : user?.email || ""}
                  >
                    {user?.name || user?.email || "…"}
                  </p>
                </div>
                <button onClick={signOut} className="w-full min-h-11 rounded-lg border border-[var(--jf-border)] py-2 text-sm text-slate-600">
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