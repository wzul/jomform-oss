"use client";

// Reset password page (issue #62): token from query param, new password +
// confirm, calls POST /v1/auth/reset-password, redirects to login on success.
import Link from "next/link";
import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";

function ResetForm() {
  const { t } = useI18n();
  const search = useSearchParams();
  const token = search.get("token") || "";
  const [pw, setPw] = useState("");
  const [pw2, setPw2] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError("");
    if (pw.length < 10) {
      setError(t("reset.newPw"));
      return;
    }
    if (pw !== pw2) {
      setError(t("reset.pwMismatch"));
      return;
    }
    setBusy(true);
    try {
      const r = await fetch("/api/auth/reset-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, new_password: pw }),
      });
      if (r.ok) {
        setDone(true);
      } else {
        const d = await r.json().catch(() => ({}));
        setError(d.error || t("reset.invalid"));
      }
    } catch {
      setError(t("reset.invalid"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="relative w-full max-w-sm">
      <div className="mb-8 text-center">
        <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
          JF
        </div>
        <h1 className="text-xl font-semibold text-[var(--jf-text)]">{t("reset.title")}</h1>
        <p className="mt-1.5 text-sm text-[var(--jf-muted)]">{t("reset.desc")}</p>
      </div>

      <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
        {done ? (
          <div className="space-y-3">
            <p className="rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-3 text-[13px] text-green-300">
              {t("reset.done")}
            </p>
            <Link
              href="/login"
              className="block w-full rounded-md bg-[#5e6ad2] py-2 text-center text-sm font-medium text-white transition-colors hover:bg-[#828fff]"
            >
              {t("login.tabLogin")}
            </Link>
          </div>
        ) : !token ? (
          <p className="text-sm text-red-600">{t("reset.invalid")}</p>
        ) : (
          <form onSubmit={submit} className="space-y-3">
            {error && (
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-700">{error}</p>
            )}
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">{t("reset.newPw")}</label>
              <input
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                type="password"
                required
                minLength={10}
                placeholder="••••••••••"
                className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-2 text-sm text-[var(--jf-text)] placeholder-slate-400 outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">{t("reset.confirmPw")}</label>
              <input
                value={pw2}
                onChange={(e) => setPw2(e.target.value)}
                type="password"
                required
                placeholder="••••••••••"
                className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-2 text-sm text-[var(--jf-text)] placeholder-slate-400 outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
              />
            </div>
            <button
              disabled={busy}
              className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-60"
            >
              {busy ? "…" : t("reset.submit")}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}

export default function ResetPasswordPage() {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      <Suspense fallback={null}>
        <ResetForm />
      </Suspense>
    </main>
  );
}
