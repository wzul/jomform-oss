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
import { apiErr } from "../../lib/apiErr";

function ConnectForm() {
  const { t } = useI18n();
  const router = useRouter();
  const search = useSearchParams();
  const token = search.get("token") || "";
  const email = search.get("email") || "";
  const provider = search.get("provider") || "";
  const uid = search.get("uid") || "";

  const [busy, setBusy] = useState(!!token);
  // The token path's failure body is kept RAW and translated at RENDER time (see
  // `error` below), for two independent reasons:
  //
  //   1. The token effect must run EXACTLY ONCE per token. `t` cannot be one of
  //      its dependencies: `t` is a useCallback in components/i18n.tsx keyed on
  //      `lang`, and its identity changes when the PROFILE language resolves
  //      (that provider fetches /api/auth/profile and calls setLangState), so a
  //      user whose dashboard language is not the initial "en" would re-run the
  //      effect and POST the emailed connect token a second time. Measured
  //      server-side at a request-counting stub: 1 POST at lang=en, 2 at lang=ms,
  //      on both the failure and the success path.
  //   2. The sentence must not be frozen mid-flight. React runs child effects
  //      BEFORE the provider's, so the POST is normally issued while the
  //      provider still holds "en" — a pre-translated string would show the
  //      English sentence to a ms/zh/ta/ar user even though the page around it
  //      renders in their language. Deriving it at render makes the sentence a
  //      pure function of (body, lang): correct on arrival, and re-translated on
  //      a live language switch, with no second POST either way.
  //
  // Guarded by src/lib/connectTokenEffect.test.ts.
  const [failure, setFailure] = useState<{ body: unknown } | null>(null);
  // The resend path is a click, which happens long after the profile language
  // has settled, so its sentence is translated at the call site as before.
  const [resendError, setResendError] = useState("");
  const [done, setDone] = useState(false);
  const [sent, setSent] = useState(false);
  const error = failure ? apiErr(failure.body, t("connect.fail"), t) : resendError;

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
        // Issue #6177 + this card: the failure body is JSON
        // ({"code":"connect.invalid","error":"pautan penghubung tidak sah"}) and
        // rendering `r.text()` printed that envelope verbatim, in the server's
        // prose language. Keep the parsed body; `error` above turns its machine
        // code into this page's own localized sentence, and an unknown code — or
        // a non-JSON body, which `.catch` leaves as {} — renders that sentence
        // too, never the prose.
        const body = await r.json().catch(() => ({}));
        if (!cancelled) setFailure({ body });
      } catch {
        if (!cancelled) setFailure({ body: null });
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
    setResendError("");
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
      const body = await r.json().catch(() => ({}));
      setResendError(apiErr(body, t("connect.resendFail"), t));
    } catch {
      setResendError(t("connect.resendFail"));
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
