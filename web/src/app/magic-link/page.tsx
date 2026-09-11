"use client";

// Magic-link sign-in page (issue #73): token from query param, calls
// POST /v1/auth/magic-link/verify, redirects to dashboard on success.
import Link from "next/link";
import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { resolvePostLogin } from "../../lib/postLogin";

function MagicLinkForm() {
  const { t } = useI18n();
  const search = useSearchParams();
  const token = search.get("token") || "";
  const [state, setState] = useState<"loading" | "invalid">("loading");

  useEffect(() => {
    if (!token) {
      setState("invalid");
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch("/api/auth/magic-link/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ token }),
        });
        if (cancelled) return;
        if (r.ok) {
          // Issue #164: route multi-workspace users without a default to the
          // workspace-picker; otherwise go straight to the dashboard.
          // Issue #349: navigate with a FULL PAGE LOAD (window.location.href),
          // NOT router.replace. The magic-link page is wrapped in the root
          // SessionProvider, which on this (still-anonymous) page load already
          // fetched the dashboard bundle and cached a null under the workspace key
          // (401 → anon). router.replace is a client-side navigation that keeps
          // that stale null bundle in memory, so the dashboard renders
          // anon=true → Landing even though the session cookie from this verify
          // response is valid. A full-page load remounts SessionProvider after
          // jf_session is committed and re-fetches the bundle authenticated —
          // the same pattern the password/passkey logins use.
          window.location.href = await resolvePostLogin("");
          return;
        }
        setState("invalid");
      } catch {
        if (!cancelled) setState("invalid");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  return (
    <div className="relative w-full max-w-sm">
      <div className="mb-8 text-center">
        <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
          JF
        </div>
        <h1 className="text-xl font-semibold text-[var(--jf-text)]">{t("magic.title")}</h1>
      </div>

      <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
        {state === "invalid" ? (
          <div className="space-y-3">
            <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-3 text-[13px] text-red-700">
              {t("magic.invalid")}
            </p>
            <Link
              href="/login"
              className="block w-full rounded-md bg-[#5e6ad2] py-2 text-center text-sm font-medium text-white transition-colors hover:bg-[#828fff]"
            >
              {t("magic.goLogin")}
            </Link>
          </div>
        ) : (
          <p className="text-center py-4 text-sm text-[var(--jf-muted)]">…</p>
        )}
      </div>
    </div>
  );
}

export default function MagicLinkPage() {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      <Suspense fallback={null}>
        <MagicLinkForm />
      </Suspense>
    </main>
  );
}
