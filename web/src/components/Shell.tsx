"use client";

import { useEffect, useState } from "react";

// AppShell: fixed left sidebar + topbar + content area. Light theme.
export function Shell({
  children,
  active,
}: {
  children: React.ReactNode;
  active: "dashboard" | "builder" | "settings";
}) {
  const [user, setUser] = useState<{ email: string } | null>(null);

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
    { key: "dashboard", href: "/", label: "Dashboard", icon: "◧" },
    { key: "builder", href: "/builder", label: "Borang", icon: "✎" },
    { key: "settings", href: "/settings", label: "Tetapan", icon: "⚙" },
  ] as const;

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
                Log keluar
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* main */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* mobile topbar */}
        <header className="flex items-center justify-between border-b border-[var(--jf-border)] bg-white px-4 py-2.5 md:hidden">
          <span className="text-sm font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
          <button onClick={signOut} className="text-xs text-[var(--jf-muted)]">
            Log keluar
          </button>
        </header>
        <div className="flex flex-1 flex-col">{children}</div>
      </div>
    </div>
  );
}