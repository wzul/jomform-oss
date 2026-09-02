"use client";

// Magic-link sign-in page (issue #73): token from query param, calls
// POST /v1/auth/magic-link/verify, redirects to dashboard on success.
import Link from "next/link";
import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";

function MagicLinkForm() {
  const { t } = useI18n();
  const router = useRouter();
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
          // Save the first space as the active one, mirroring login.
          try {
            const sp = await fetch("/api/spaces").then((x) => (x.ok ? x.json() : []));
            if (Array.isArray(sp) && sp.length) localStorage.setItem("jf_space", sp[0].slug);
          } catch {}
          router.replace("/");
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
  }, [token, router]);

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
            <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-3 text-[13px] text-red-300">
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
