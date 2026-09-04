"use client";

// Connect account page (issue #302): a first social login whose email matches
// an existing JomForm account is NOT silently auto-linked. This page is the
// explicit, owner-confirmed "connect" step.
//
// Entry points:
//  1. ?token=…  — from the emailed ownership-verification link. We POST it to
//     /api/auth/social/connect; on success the identity is bound to the
//     existing account and the user is signed in.
//  2. ?email=…&provider=…&uid=…  — the OAuth callback just redirected here
//     after emailing a verification link. We show a "check your email" step
//     with a resend option. No capability flows through this URL.
import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";

function ConnectForm() {
  const { t } = useI18n();
  const router = useRouter();
  const search = useSearchParams();
  const token = search.get("token") || "";
  const email = search.get("email") || "";
  const provider = search.get("provider") || "";
  const uid = search.get("uid") || "";

  const [busy, setBusy] = useState(!!token);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [sent, setSent] = useState(false);

  // Verify the emailed connect token.
  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch("/api/auth/social/connect", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ token }),
        });
        if (cancelled) return;
        if (r.ok) {
          setDone(true);
          // Show the success message briefly, then head to the dashboard.
          await new Promise((res) => setTimeout(res, 900));
          if (!cancelled) router.replace("/post-login");
          return;
        }
        const text = await r.text().catch(() => "");
        if (!cancelled) setError(text || "Unable to connect this account.");
      } catch {
        if (!cancelled) setError("Unable to connect this account.");
      } finally {
        if (!cancelled) setBusy(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token, router]);

  async function resend() {
    setBusy(true);
    setError("");
    try {
      const r = await fetch("/api/auth/social/connect/resend", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, provider, uid }),
      });
      if (r.ok) {
        setSent(true);
        return;
      }
      const text = await r.text().catch(() => "");
      setError(text || "Unable to send a verification email.");
    } catch {
      setError("Unable to send a verification email.");
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
        <h1 className="text-xl font-semibold text-[var(--jf-text)]">{t("connect.title")}</h1>
      </div>

      <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
        {done ? (
          <p className="rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-3 text-[13px] text-emerald-700">
            {t("connect.success")}
          </p>
        ) : token ? (
          <div className="space-y-4">
            <p className="text-sm text-[var(--jf-muted)]">{t("connect.verifying")}</p>
            {error && (
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-3 text-[13px] text-red-700">
                {error}
              </p>
            )}
            {busy && <p className="text-sm text-[var(--jf-muted)]">…</p>}
          </div>
        ) : sent ? (
          <p className="text-sm text-[var(--jf-muted)]">{t("connect.resendSent")}</p>
        ) : (
          <div className="space-y-4">
            <p className="text-sm text-[var(--jf-muted)]">
              {email ? t("connect.explain") : t("connect.explainShort")}
            </p>
            {error && (
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-3 text-[13px] text-red-700">
                {error}
              </p>
            )}
            {email ? (
              <button
                onClick={resend}
                disabled={busy}
                className="w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-50"
              >
                {t("connect.resend")}
              </button>
            ) : (
              <p className="text-sm text-[var(--jf-muted)]">{t("connect.stale")}</p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export default function ConnectPage() {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      <Suspense fallback={null}>
        <ConnectForm />
      </Suspense>
    </main>
  );
}
