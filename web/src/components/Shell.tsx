"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useI18n, type Key } from "./i18n";
import { useSession } from "./session";
import { SessionFault } from "./SessionFault";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { clearPerUserStorage, clearUnknownWorkspaceScope, enterWorkspace, publishWorkspaceScope } from "../lib/workspace";
import { errorBodyOf, fetchOrThrow } from "../lib/wsfetch";
import { translateApiErr } from "../lib/apiErr";

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

// ── module-scope components ─────────────────────────────────────────────────
//
// WHY THESE LIVE HERE AND NOT INSIDE `Shell` (react-hooks/static-components).
// `UserAvatar` and `NavList` used to be declared in the component body, so every
// render produced a NEW component type. React compares element types by
// identity, so a fresh type means the whole subtree UNMOUNTS and REMOUNTS on
// each render — the avatar image reloads, the nav loses its DOM state. The rule
// reports that ("Cannot create components during render") and the fix it asks
// for is exactly this: declare once at module scope and pass the props in.
// Nothing about the rendered output changes; only the identity does, and that is
// the point.

type NavItem = { key: string; href: string; label: string; icon: string };
type NavGroup = { id: string; labelKey: Key; items: NavItem[] };

/** The user's face: their uploaded avatar, or the initial-letter circle when
 *  there is no `avatar_url` or the image failed to load. */
function UserAvatar({
  url,
  name,
  email,
  initial,
  failed,
  onFail,
  className,
}: {
  url?: string;
  name?: string;
  email: string;
  initial: string;
  failed: boolean;
  onFail: () => void;
  className: string;
}) {
  if (url && !failed) {
    // A remote avatar URL is user content, so `next/image` would need a
    // configured loader/remotePatterns; this stays a plain <img> (the
    // eslint-disable is intentional, not an oversight).
    // eslint-disable-next-line @next/next/no-img-element
    return (
      <img
        src={url}
        alt={name || email}
        onError={onFail}
        className={`${className} object-cover`}
      />
    );
  }
  return (
    // The tint is Tailwind indigo-100 (#e0e7ff), on which the BASE
    // `text-[var(--jf-primary)]` measures 3.814:1 — under the 4.5:1 AA floor for
    // this 12px (text-xs) initial — so the badge takes the on-tint token, which
    // measures 6.414:1 here (card t_86755e43). The /profile badge's 24px initial
    // is WCAG large-scale text (24px = 18pt) and keeps the base token: 3.814:1
    // against its 3:1 floor. `src/lib/indigo100Tint.test.ts` owns both verdicts.
    <div className={`${className} flex items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary-on-tint)]`}>
      {initial}
    </div>
  );
}

/** The ONE nav list, rendered by both surfaces (the desktop <aside> and the
 *  mobile drawer).
 *
 *  Each group is a <ul> named by its own visible label via aria-labelledby, so
 *  a screen reader announces "Build, list, 4 items" instead of a flat run of 13
 *  links with no structure. The label is a plain <p>, not a heading: the
 *  sidebar is not part of the document outline and a heading here would sit
 *  before the page's own <h1>.
 *
 *  `idPrefix` keeps the label ids unique — the desktop <aside> stays in the DOM
 *  (hidden by `hidden md:flex`) while the drawer is open on a narrow viewport. */
function NavList({
  groups,
  active,
  t,
  idPrefix,
  touch,
  onNavigate,
}: {
  groups: NavGroup[];
  active: string;
  t: (k: Key) => string;
  idPrefix: string;
  touch?: boolean;
  onNavigate?: () => void;
}) {
  return (
    <>
      {groups.map((g) => (
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
                  {/* The icon keeps the row's own ink token (t_07eb69c8). It used to
                      carry `opacity-70`, which composites `--jf-muted` #6b7280 over
                      the sidebar's white to #979ca6 — MEASURED 2.755:1 at 12px, well
                      under the 4.5 AA floor — and the active row's
                      `--jf-primary-on-tint` over `bg-indigo-50` to #766fda, 3.714:1.
                      At full strength the same two pairs are 4.834:1 and 7.068:1, so
                      the fix is to stop dimming the glyph, not to invent a token. */}
                  {/* The glyph is decoration: `aria-hidden` keeps it out of the
                      link's accessible name, which is otherwise computed from the
                      whole row's text — MEASURED before this (card t_950256ab), the
                      name Chromium's AX tree handed AT read "✎ Forms", "⚠ Spam",
                      "🗂 Templates" on all 14 rows, in BOTH surfaces (this is the
                      one NavList). Same rule the sortable-table ▲/▼ glyph already
                      carries (e2e/a11y-products.spec.ts): the label beside it is the
                      name. The glyph is still PAINTED — it is part of the design;
                      only AT stops hearing it. Guarded on the accessible NAME, not
                      on this attribute: e2e/a11y-nav-icon-name.spec.ts reads it out
                      of Chromium's AX tree and Playwright's ariaSnapshot, on the
                      desktop sidebar AND the mobile drawer, and its control arm
                      removes this attribute to prove the measurement bites. */}
                  <span className="w-4 text-center text-xs" aria-hidden="true">{n.icon}</span>
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

export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "forms" | "templates" | "products" | "sales" | "spam" | "deliveries" | "activity" | "subscriptions" | "settings" | "profile" | "media" | "feedback" | "chat" | "admin";
}) {
  const { user, workspaces, reload, bundle, sessionError } = useSession();
  const [workspace, setWorkspace] = useState<string>("");
  const [menuOpen, setMenuOpen] = useState(false);
  const { t } = useI18n();
  const router = useRouter();
  // Card t_ea568318: the dashboard's route transition is keyed on this. Every
  // dashboard page renders its own Shell, so a navigation unmounts one Shell and
  // mounts the next — but keying the content container on the pathname too means
  // the entrance fires on a ROUTE CHANGE and not on a re-render even if the
  // shell is ever hoisted into a shared layout (where the container would
  // otherwise survive the navigation and never animate again). A same-path
  // navigation (a tab switch carried in the query string) keeps the key, so the
  // page's own state is never remounted by this.
  const pathname = usePathname();
  const drawerRef = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Issue #133: the user's initial letter (or "?"), used as the avatar
  // fallback when no profile picture (avatar_url) is set or it fails to load.
  const initial = ((user?.name || user?.email || "?")[0] || "?").toUpperCase();
  const [imgFailed, setImgFailed] = useState(false);

  // Avatar can change after a profile edit; reset the failure flag when the
  // URL changes so a new image isn't stuck hidden behind a stale onError.
  useEffect(() => setImgFailed(false), [user?.avatar_url]);

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
    // answered 403 "workspace not found". clearUnknownWorkspaceScope drops
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
  //
  // THE OUTCOME IS PART OF THE CONTRACT (card t_f6de6395): this resolves to the
  // SENTENCE to show when the write was refused, or null when it applied — the
  // switcher renders it (WorkspaceSwitcher's onSetDefault). Before this the
  // status was never read and a bare `catch {}` swallowed transport errors, so
  // a 400/403/500 and a dead network were BOTH invisible: the menu had already
  // closed and reload() re-read the unchanged state, so the merchant saw
  // nothing happen and had no reason to think anything had gone wrong.
  //
  // `fetchOrThrow` is the shared "was this a failure" decision (lib/wsfetch.ts)
  // — the module that owns isUnauthStatus/isScopeRefusal. No status branch is
  // taken here on purpose: those rules exist to stop a TENANCY refusal (a 403
  // naming `workspace.notFound`) from being mistaken for a dead session, which
  // only matters for a surface that NAVIGATES on unauth. This one does not — a
  // refused WRITE is not a dead session, and the next scoped read still routes
  // through the session's own unauth path — so the whole classification would
  // be a branch with one outcome. A hand-rolled `status === 401 || status ===
  // 403` never appears here for the same reason it must not appear anywhere:
  // it would bounce a signed-in merchant for their own stale slug (t_da4b2031).
  async function setDefaultWorkspace(workspaceId: string): Promise<string | null> {
    try {
      await fetchOrThrow("/api/auth/profile/default-workspace", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workspace_id: workspaceId }),
      });
    } catch (err) {
      // The DIRECTION picks the sentence: a non-empty id is a SET, "" is a
      // CLEAR. Both literals are spelled out rather than held in a `Key`
      // variable, because src/lib/i18nKeys.test.ts only resolves a literal call
      // site — a dynamic key is coverage the guard cannot see.
      const fallback = workspaceId === "" ? t("workspace.clearDefaultFail") : t("workspace.setDefaultFail");
      // A NAMED machine code gets its own translated sentence, the same rule
      // workspace-picker uses (issue #6177: never render the server's Malay
      // prose); with no code — or one this client does not know — the
      // direction's own sentence is the answer.
      return translateApiErr(errorBodyOf(err), t) || fallback;
    }
    // Only a successful write changes anything, so only it re-reads the bundle.
    reload();
    return null;
  }

  async function signOut() {
    // Clear the per-user cached scope BEFORE the session is destroyed. jf_workspace
    // is never cleared anywhere else, so without this the next user on this
    // browser inherits the previous user's slug and every scoped write 403s
    // with "workspace not found" — the reported bug. The sweep covers every
    // per-user key (lib/workspace.PER_USER_STORAGE_KEYS), not just the one that
    // bit, so a sibling key cannot be inherited the same way.
    clearPerUserStorage();
    await fetch("/api/auth/logout", { method: "POST" });
    router.replace("/login");
    window.location.href = "/login";
  }

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

  return (
    <div className="flex min-h-screen">
      {/* Issue #104: skip-to-content link for keyboard / screen-reader users. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-[var(--jf-primary)] focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-white"
      >
        Skip to content
      </a>
      {/* sidebar */}
      <aside className="sticky top-0 hidden h-screen w-56 shrink-0 self-start flex-col border-r border-[var(--jf-border)] bg-white md:flex">
        <div className="flex items-center gap-2 px-5 py-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-jf-md font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <nav className="mt-2 min-h-0 flex-1 overflow-y-auto px-3 pb-3">
          <NavList groups={navGroups} active={active} t={t} idPrefix="nav-desktop" />
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
            <UserAvatar
              url={user?.avatar_url}
              name={user?.name}
              email={user?.email || ""}
              initial={initial}
              failed={imgFailed}
              onFail={() => setImgFailed(true)}
              className="h-8 w-8 rounded-full"
            />
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
            className="mt-1 flex min-h-11 w-full items-center rounded-md px-2 text-sm text-[var(--jf-muted)] hover:bg-slate-50 hover:text-red-700"
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
            aria-label={t("nav.openMenu")}
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
              aria-label={t("nav.menu")}
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
                  aria-label={t("nav.closeMenu")}
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
              <nav className="min-h-0 flex-1 overflow-y-auto p-3" aria-label={t("nav.menu")}>
                <NavList
                  groups={navGroups}
                  active={active}
                  t={t}
                  idPrefix="nav-mobile"
                  touch
                  onNavigate={() => setMenuOpen(false)}
                />
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
                  <UserAvatar
                    url={user?.avatar_url}
                    name={user?.name}
                    email={user?.email || ""}
                    initial={initial}
                    failed={imgFailed}
                    onFail={() => setImgFailed(true)}
                    className="h-8 w-8 rounded-full"
                  />
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

        <div id="main-content" className="flex flex-1 flex-col">
          {/* A SETTLED SESSION-BOOTSTRAP FAULT REPLACES THE CONTENT, NOT THE
              CHROME (card t_d1c9a43a). Every dashboard route renders its tree
              inside this shell, so this is the ONE place that can offer the
              fault + retry the visitor needs wherever they landed: before this,
              only `/` (dashboard-client.tsx) read `sessionError`, and every
              other route sat on its own loading branch — an un-Shell'd "…" with
              no sentence and no control. A route cannot forget a rule it does
              not have to remember, and a new page is covered the moment it
              renders inside the shell.

              IT IS KEYED ON `sessionError`, NEVER ON `anon`: `anon` is the
              authoritative-401 gate, and folding it in here would replace the
              wanted /landing bounce with a dead page whose Retry can never
              succeed (the whole reason #1041 split the two states —
              src/components/session.tsx). `sessionError` is true ONLY for a
              read that produced NO answer (5xx / dropped connection / non-JSON
              200 / the bounded hung read), so an anonymous visitor never
              reaches this branch.

              The sidebar and the topbar stay mounted on purpose: the fault is
              about the page's data, and unmounting the chrome is the "reads as
              a full page reload" defect `PageLoading` exists to prevent. */}
          {/* THE PAGE BODY IS THE DOCUMENT'S `main` LANDMARK (card t_25a8fc88).
              A full-document axe scan of the signed-in routes reported
              `landmark-one-main` (the document had NO main landmark) plus
              `region` (the page's content sat outside every landmark), because
              the page bodies were plain `<div>`s. The landmark wraps the PAGE,
              never the chrome: the sidebar (`<aside>`) and the topbar stay
              outside it — the same topology `/profile` and `/settings` proved
              clean while they carried their own `<main>` in this slot. Those
              two page bodies are now `<div>`s; a page that keeps its own
              `<main>` would nest two, which is invalid (axe:
              `landmark-main-is-top-level`). The fault surface is the ONE
              exception: `SessionFault` is a full-page `main` of its own (it
              also renders from `/`, where no Shell exists), so it is NOT
              wrapped again here — either way the document has exactly one. */}
          {sessionError ? (
            <SessionFault />
          ) : (
            /* THE ROUTE ENTRANCE (card t_ea568318). The dashboard had no route
               transition at all: navigation swapped one page for the next with
               no acknowledgement, which is what the owner saw as "no motion
               effect". `.jf-enter` is the layer's page-enter pattern (fade +
               4px rise, 180ms, transform/opacity only, globals.css), and the
               `key` on the pathname is what makes it fire ONCE PER ROUTE:
               React unmounts this node when the pathname changes, and a CSS
               animation runs when its element is created — never on a
               re-render, because a re-render keeps the same node. A same-path
               navigation (a tab carried in the query string) keeps the key, so
               a page's own state is not remounted by this.
               `prefers-reduced-motion: reduce` switches the animation off
               outright (globals.css, the LAST block in the file), so the
               reduced-motion reader gets the page with no movement.
               It is the SAME `<main>` box, not a wrapper — `{children}` stays
               its direct child, the topology `sessionFault.test.ts` guards —
               so the animation starts at the element's final layout position
               and cannot shift layout (no CLS). */
            <main key={pathname} className="jf-enter flex flex-1 flex-col">{children}</main>
          )}
        </div>
      </div>
    </div>
  );
}
