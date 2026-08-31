"use client";

import { useEffect, useState } from "react";
import { useI18n, LangToggle } from "./i18n";

// AppShell: fixed left sidebar + topbar + content area. Light theme.
export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "forms" | "products" | "settings";
}) {
  const [user, setUser] = useState<{ email: string } | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const { t } = useI18n();

  useEffect(() => {
    fetch("/api/auth/me")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setUser(d))
      .catch(() => setUser(null));
  }, []);

  async function signOut() {
    await fetch("/api/auth/logout", { method: "POST" });
    window.location.href = "/login";
  }

  const nav = [
    { key: "dashboard", href: "/", label: t("nav.dashboard"), icon: "◧" },
    { key: "forms", href: "/forms", label: t("nav.forms"), icon: "✎" },
    { key: "products", href: "/products", label: t("prod.title"), icon: "◧" },
    { key: "settings", href: "/settings", label: t("nav.settings"), icon: "⚙" },
  ];

  return (
    <div className="flex min-h-screen">
      {/* sidebar */}
      <aside className="hidden w-56 shrink-0 flex-col border-r border-[var(--jf-border)] bg-white md:flex">
        <div className="flex items-center gap-2 px-5 py-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-[15px] font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <nav className="mt-2 flex-1 space-y-0.5 px-3">
          {nav.map((n) => (
            <a
              key={n.key}
              href={n.href}
              className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors ${
                active === n.key
                  ? "bg-indigo-50 font-medium text-[var(--jf-primary)]"
                  : "text-[var(--jf-muted)] hover:bg-slate-50 hover:text-[var(--jf-text)]"
              }`}
            >
              <span className="w-4 text-center text-xs opacity-70">{n.icon}</span>
              {n.label}
            </a>
          ))}
        </nav>
        {/* user card */}
        <div className="border-t border-[var(--jf-border)] p-3">
          <div className="flex items-center gap-2.5 rounded-lg px-2 py-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-[var(--jf-primary)]">
              {(user?.email || "?")[0].toUpperCase()}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-medium" title={user?.email || ""}>
                {user?.email || "…"}
              </p>
              <button
                onClick={signOut}
                className="text-[11px] text-[var(--jf-muted)] hover:text-red-500"
              >
                {t("nav.signout")}
              </button>
            </div>
          </div>
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
              className="absolute left-0 top-0 h-full w-64 bg-white shadow-xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-center justify-between border-b border-[var(--jf-border)] px-4 py-3">
                <span className="text-sm font-semibold">
                  Jom<span className="text-[var(--jf-primary)]">Form</span>
                </span>
                <button
                  onClick={() => setMenuOpen(false)}
                  aria-label="Close menu"
                  className="rounded-md p-1 text-[var(--jf-muted)] hover:bg-slate-50"
                >
                  ✕
                </button>
              </div>
              <nav className="space-y-0.5 p-3">
                {nav.map((n) => (
                  <a
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
                  </a>
                ))}
              </nav>
              <div className="absolute bottom-4 left-0 right-0 px-4">
                <p className="mb-2 truncate text-xs text-[var(--jf-muted)]" title={user?.email || ""}>
                  {user?.email || "…"}
                </p>
                <button onClick={signOut} className="w-full rounded-lg border border-[var(--jf-border)] py-2 text-sm text-slate-600">
                  {t("nav.signout")}
                </button>
              </div>
            </div>
          </div>
        )}

        <div className="flex flex-1 flex-col">{children}</div>
      </div>
    </div>
  );
}