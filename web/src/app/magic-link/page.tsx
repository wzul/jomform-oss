"use client";

// Magic-link sign-in page (issue #73): token from query param, calls
// POST /v1/auth/magic-link/verify, redirects to dashboard on success.
import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { Button, Card } from "../../components/ui";
import { apiErr } from "../../lib/apiErr";
import { resolvePostLogin } from "../../lib/postLogin";
import { browserNextTarget } from "../../lib/nextTarget";

function MagicLinkForm() {
  const { t } = useI18n();
  const search = useSearchParams();
  const token = search.get("token") || "";
  // Card t_5e39dc9f: the page the visitor was bounced from, carried by the
  // emailed link (`…/magic-link?token=…&next=…`) because the magic-link request
  // forwarded it. Validated with the SAME rule the login page uses before it is
  // honoured, so a foreign/absent/oversized value falls back to today's
  // behaviour (`resolvePostLogin("")` → dashboard or picker) with no open
  // redirect. Computed once, at mount: it is only read by the verify effect
  // below, and nothing here may change the effect's identity — a second POST
  // would burn the single-use token (see the dependency note under it).
  const [next] = useState<string>(() => browserNextTarget(search.get("next")));
  // The failure MESSAGE, not a flag: the page has to be able to say WHICH failure
  // it met (card t_9ed9bc2c — a flag answers every code with one generic sentence,
  // which made `magicLink.tokenMissing` unreachable). `""` means "still working".
  const [error, setError] = useState("");

  useEffect(() => {
    if (!token) {
      setError(t("magic.invalid"));
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
          // Card t_5e39dc9f: honour the validated `next` the emailed link
          // carried — the page the visitor was bounced from — instead of the
          // hard-coded "". An absent or discarded value resolves exactly as
          // before (dashboard, or the picker for a workspace-less user).
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
          window.location.href = await resolvePostLogin(next);
          return;
        }
        // ROUTE THE FAILURE THROUGH THE BODY (card t_9ed9bc2c). The page used to
        // answer every non-2xx with one generic sentence and never read the
        // response, so `magicLink.tokenMissing` — and its mapping and dictionary
        // entry — could not be rendered by any surface at all: the code was
        // UNREACHABLE. Measured in a browser with the endpoint stubbed to answer
        // `{"code":"magicLink.tokenMissing",…}`, the page rendered the same sentence
        // the unknown-code control did, while /reset-password (same protocol) DID
        // render its own `reset.tokenMissing` sentence. Same shape as
        // /reset-password: read the body, fall back to this surface's own sentence
        // for an unknown code, and let the code decide when it is one we know.
        //
        // The fallback is `magic.invalid`, so the generic reading is unchanged for
        // anything the dictionary does not name.
        const d = await r.json().catch(() => ({}));
        setError(apiErr(d, t("magic.invalid"), t));
      } catch {
        if (!cancelled) setError(t("magic.invalid"));
      }
    })();
    return () => {
      cancelled = true;
    };
    // `t` is a stable function from the i18n context, and `token` is the only
    // input: adding anything unstable here would re-run the POST (the
    // duplicate-POST lesson on connect/page.tsx:69, card t_160dc5cd).
    // `next` (card t_5e39dc9f) is listed for the linter and is mount-constant —
    // there is no setter — so it can never re-fire this effect and never costs a
    // second verify against the single-use token.
  }, [token, t, next]);

  return (
    <div className="relative w-full max-w-sm">
      <div className="mb-8 text-center">
        <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[var(--jf-primary)] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
          JF
        </div>
        <h1 className="text-xl font-semibold text-[var(--jf-text)]">{t("magic.title")}</h1>
      </div>

      {/* Card t_8f20418d: the surface was a private `rounded-xl border bg-white
          p-6 shadow-[…]` — Card is that decision made once (token fill, token
          edge, token elevation). */}
      <Card className="p-jf-6">
        {error ? (
          <div className="space-y-3">
            <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-3 text-jf-sm text-red-700">
              {error}
            </p>
            <Button as="a" href="/login" className="w-full">
              {t("magic.goLogin")}
            </Button>
          </div>
        ) : (
          <p className="text-center py-4 text-sm text-[var(--jf-muted)]">…</p>
        )}
      </Card>
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
