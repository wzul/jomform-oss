"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useI18n, type Key } from "./i18n";
import { useSession } from "./session";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { clearPerUserStorage, clearUnknownWorkspaceScope, enterWorkspace, publishWorkspaceScope } from "../lib/workspace";

// AppShell: fixed left sidebar + topbar + content area. Light theme.
// Issue #29: nav links are Next <Link> (client-side transitions + prefetch,
// no full document reload) and me/workspaces come from the shared session
// context (one dashboard-bundle fetch per session), not per-mount fetches.
//
// Sidebar redesign: the destination list is GROUPED (four labelled sections)
// and rendered by ONE component (NavList) for both surfaces — the desktop
// <aside> and the mobile drawer used to keep two copies of `nav.map(...)` with
// their own classes, so every nav change had to be made twice and the two
// surfaces could drift apart. `Profile` is deliberately NOT a nav row any more:
// it is the account block at the bottom of both surfaces, which is a link to
// /profile (the email was already shown there, and the block looked clickable
// without being it).
export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "forms" | "templates" | "products" | "sales" | "spam" | "deliveries" | "activity" | "subscriptions" | "settings" | "profile" | "media" | "feedback" | "chat" | "admin";
}) {
  const { user, workspaces, reload, bundle } = useSession();
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
    // `useSession()` derives `workspaces` from the dashboard bundle, which is
    // `undefined` until that fetch settles and `null` when it failed — only a
    // settled payload is an ANSWER. Reconciling against anything else dropped a
    // VALID stored slug on first paint: the Shell cleared `jf_workspace` before
    // the SessionProvider (or any page) could read it, `role` then resolved to
    // "" and the role-gated surfaces (Settings → Members, CHIP) never rendered.
    // `undefined` is the NO-OP branch of clearUnknownWorkspaceScope
    // (lib/workspace.ts) — the "no list returned at all" case its own comment
    // documents — and it was unreachable before.
    const ws = bundle == null ? undefined : Array.isArray(workspaces) ? workspaces : undefined;
    // Issue #503 follow-up: reconcile the stored slug against the list the
    // server actually returned. `if (!ws.length) return;` used to leave a
    // FOREIGN slug in localStorage when the caller has zero workspaces — the
    // next scoped write then went out as ?workspace=<other tenant> and the API
    // answered 403 "workspace tidak dijumpai". clearUnknownWorkspaceScope drops
    // it (an empty list is a real answer) and is a no-op when there is no list.
    //
    // Every branch below PUBLISHES the resolved slug through
    // publishWorkspaceScope (t_da4b2031), which writes localStorage AND the
    // module-level CONFIRMED scope every scoped fetch reads. A bare setItem
    // would leave workspaceQuery() serving the old value, so a page's first
    // fetch after the re-selection would still carry the slug just rejected.
    const surviving = clearUnknownWorkspaceScope(ws);
    if (ws === undefined) return;
    if (!ws.length) {
      publishWorkspaceScope("");
      setWorkspace("");
      return;
    }
    if (surviving) {
      publishWorkspaceScope(surviving);
      setWorkspace(surviving);
    } else {
      publishWorkspaceScope(ws[0].slug);
      setWorkspace(ws[0].slug);
    }
  }, [workspaces, bundle]);

  function switchWorkspace(slug: string) {
    setWorkspace(slug);
    // `enterWorkspace` writes jf_workspace, publishes the slug as the CONFIRMED
    // scope every scoped fetch reads (t_da4b2031) and dispatches
    // jf:workspace-changed — the event issue #170 added so the session context
    // (and any data page keyed on useSession().workspace) re-fetches for the new
    // workspace without a manual refresh.
    enterWorkspace(slug);
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
    // Clear the per-user cached scope BEFORE the session is destroyed. jf_workspace
    // is never cleared anywhere else, so without this the next user on this
    // browser inherits the previous user's slug and every scoped write 403s
    // with "workspace tidak dijumpai" — the reported bug. The sweep covers every
    // per-user key (lib/workspace.PER_USER_STORAGE_KEYS), not just the one that
    // bit, so a sibling key cannot be inherited the same way.
    clearPerUserStorage();
    await fetch("/api/auth/logout", { method: "POST" });
    router.replace("/login");
    window.location.href = "/login";
  }

  type NavItem = { key: string; href: string; label: string; icon: string };
  type NavGroup = { id: string; labelKey: Key; items: NavItem[] };

  // The destinations, grouped by what the user is doing — not an arbitrary
  // list of 13 rows. Every destination is kept; only the shape changed, so no
  // URL moves and no docs deep link breaks.
  //
  // `Profile` is intentionally absent: /profile is reached from the account
  // block at the foot of both surfaces (it repeated the email that block
  // already shows, and the block itself looked like a control but was not one).
  let navGroups: NavGroup[] = [
    {
      id: "build",
      labelKey: "navgroup.build",
      items: [
        { key: "dashboard", href: "/", label: t("nav.dashboard"), icon: "◧" },
        { key: "forms", href: "/forms", label: t("nav.forms"), icon: "✎" },
        // Issue #457: merchant-managed templates (save/edit/delete own templates).
        { key: "templates", href: "/templates", label: t("nav.templates"), icon: "🗂" },
        { key: "products", href: "/products", label: t("prod.title"), icon: "◧" },
      ],
    },
    {
      id: "sales",
      labelKey: "navgroup.sales",
      items: [
        // Issue #126: dedicated Sales page, between Products and Settings.
        { key: "sales", href: "/sales", label: t("nav.sales"), icon: "💰" },
        // Issue #245: AI spam review queue.
        { key: "spam", href: "/spam", label: t("nav.spam"), icon: "⚠" },
        { key: "subscriptions", href: "/subscriptions", label: t("sub.title"), icon: "⟳" },
        // Issue #194: email delivery status view.
        { key: "deliveries", href: "/deliveries", label: t("del.title"), icon: "✉" },
        // Card t_eb09b25a: the workspace-scoped audit trail. It sits in the
        // "sales" group with the other read surfaces about what the workspace
        // HAS DONE (sales, spam, subscriptions, deliveries) rather than under
        // Settings: it is a record of the workspace's actions, not a setting.
        { key: "activity", href: "/activity", label: t("act.title"), icon: "🕘" },
      ],
    },
    {
      id: "settings",
      labelKey: "navgroup.settings",
      items: [
        { key: "settings", href: "/settings", label: t("nav.settings"), icon: "⚙" },
        // Issue #180: media library (WordPress-style).
        { key: "media", href: "/media", label: t("media.title"), icon: "🖼" },
      ],
    },
    {
      id: "help",
      labelKey: "navgroup.help",
      items: [
        // In-app assistant (phase 1): read-only over the user's own data, plus
        // filing feedback. Placed next to Feedback because that is where a
        // filing lands — the two surfaces are one story.
        { key: "chat", href: "/chat", label: t("nav.chat"), icon: "✨" },
        // In-app feedback: a plain link for EVERY signed-in user, deliberately
        // NOT gated on the super-admin role. The feedback form is the system's,
        // not a workspace's, so reaching it must not depend on owning an
        // account — the same reason /admin (below) is the only role-gated entry
        // in this list.
        { key: "feedback", href: "/feedback", label: t("nav.feedback"), icon: "💬" },
      ],
    },
  ];

  // Issue #387: super-admins (#385) get a platform-wide Admin nav link. It
  // sits with the other platform/user-level entries rather than next to the
  // merchant's own settings, and the group it lands in is never empty without it.
  if (user?.is_super_admin) {
    navGroups = navGroups.map((g) =>
      g.id === "help"
        ? { ...g, items: [...g.items, { key: "admin", href: "/admin", label: t("nav.admin"), icon: "🛡" }] }
        : g,
    );
  }

  // The ONE nav list. Both surfaces render this, so a future reorder, rename or
  // extra destination is a single edit — the two-surface duplication is what let
  // `Profile` appear as its own row in both places at once.
  //
  // Each group is a <ul> named by its own visible label via aria-labelledby, so
  // a screen reader announces "Build, list, 4 items" instead of a flat run of 13
  // links with no structure. The label is a plain <p>, not a heading: the
  // sidebar is not part of the document outline and a heading here would sit
  // before the page's own <h1>.
  //
  // `idPrefix` keeps the label ids unique — the desktop <aside> stays in the DOM
  // (hidden by `hidden md:flex`) while the drawer is open on a narrow viewport.
  function NavList({ idPrefix, touch, onNavigate }: { idPrefix: string; touch?: boolean; onNavigate?: () => void }) {
    return (
      <>
        {navGroups.map((g) => (
          <div key={g.id} className={touch ? "mb-1" : "mb-0.5"}>
            <p
              id={`${idPrefix}-grp-${g.id}`}
              className={`px-3 pb-1 text-xs font-medium uppercase tracking-wide text-[var(--jf-muted)] ${touch ? "pt-3" : "pt-4"}`}
            >
              {t(g.labelKey)}
            </p>
            <ul aria-labelledby={`${idPrefix}-grp-${g.id}`} className="space-y-0.5">
              {g.items.map((n) => (
                <li key={n.key}>
                  <Link
                    href={n.href}
                    onClick={onNavigate}
                    aria-current={active === n.key ? "page" : undefined}
                    // ONE row class for both surfaces. Mobile audit: drawer rows
                    // measured 40px tall (py-2.5), under the 44x44 tap floor —
                    // `min-h-11` is what raises them, and it is harmless on the
                    // desktop rows, which already measured 44px (py-3).
                    className={`flex min-h-11 items-center gap-2.5 rounded-lg px-3 py-3 text-sm transition-colors ${
                      active === n.key
                        ? "bg-indigo-50 font-medium text-[var(--jf-primary-on-tint)]"
                        : "text-[var(--jf-muted)] hover:bg-slate-50 hover:text-[var(--jf-text)]"
                    }`}
                  >
                    <span className="w-4 text-center text-xs opacity-70">{n.icon}</span>
                    {n.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </>
    );
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
        <nav className="mt-2 min-h-0 flex-1 overflow-y-auto px-3 pb-3">
          <NavList idPrefix="nav-desktop" />
        </nav>
        {/* user card — flex-none at the bottom of the full-height aside (always
            visible). The avatar + name + email ARE the Profile destination: the
            block was a dead-looking control before, and /profile was also a nav
            row, so the same destination was offered twice with the email shown
            twice. Sign out stays a separate button below it: it is destructive
            and must not share a tap area with a navigation link. */}
        <div className="shrink-0 border-t border-[var(--jf-border)] p-3">
          <Link
            href="/profile"
            aria-current={active === "profile" ? "page" : undefined}
            className={`flex items-center gap-2.5 rounded-lg px-2 py-2 ${
              active === "profile" ? "bg-indigo-50" : "hover:bg-slate-50"
            }`}
          >
            {/* Issue #133: uploaded profile picture, falling back to the initial letter. */}
            <UserAvatar className="h-8 w-8 rounded-full" />
            <span className="min-w-0 flex-1">
              {/* Mobile audit / sidebar width: this text used to be `truncate`, so
                  the user's own email rendered as "owner00@example.c…" and
                  was unreadable on a touch device (the `title` tooltip is
                  hover-only). It wraps instead. */}
              <span className="block break-words text-sm font-medium">
                {user?.name || user?.email || "…"}
              </span>
              {user?.name && (
                <span className="block break-all text-xs text-[var(--jf-muted-on-tint)]">{user?.email || ""}</span>
              )}
            </span>
          </Link>
          <button
            onClick={signOut}
            className="mt-1 flex min-h-11 w-full items-center rounded-md px-2 text-sm text-[var(--jf-muted)] hover:bg-slate-50 hover:text-red-500"
          >
            {t("nav.signout")}
          </button>
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
              {/* Mobile audit: the scroll body is the <nav> landmark itself, so
                  assistive tech still gets a navigation region inside the
                  dialog (the desktop <aside> has one and the drawer must not
                  silently lose it). */}
              <nav className="min-h-0 flex-1 overflow-y-auto p-3" aria-label="Navigation menu">
                <NavList idPrefix="nav-mobile" touch onNavigate={() => setMenuOpen(false)} />
              </nav>
              {/* account footer: the Profile destination (see the desktop block
                  above). border-t keeps it reading as the drawer's account
                  footer, and the nav above it scrolls independently. */}
              <div className="shrink-0 border-t border-[var(--jf-border)] px-4 py-3">
                <Link
                  href="/profile"
                  onClick={() => setMenuOpen(false)}
                  aria-current={active === "profile" ? "page" : undefined}
                  className={`flex items-center gap-2.5 rounded-lg px-2 py-2 ${
                    active === "profile" ? "bg-indigo-50" : "hover:bg-slate-50"
                  }`}
                >
                  {/* Issue #133: profile picture or initial-letter fallback. */}
                  <UserAvatar className="h-8 w-8 rounded-full" />
                  <span className="min-w-0 flex-1">
                    <span className="block break-words text-sm">{user?.name || user?.email || "…"}</span>
                    {user?.name && (
                      <span className="block break-all text-xs text-[var(--jf-muted-on-tint)]">{user?.email || ""}</span>
                    )}
                  </span>
                </Link>
                <button onClick={signOut} className="mt-1 w-full min-h-11 rounded-lg border border-[var(--jf-border)] py-2 text-sm text-slate-600">
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
