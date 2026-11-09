"use client";

// Reset password page (issue #62): token from query param, new password +
// confirm, calls POST /v1/auth/reset-password, redirects to login on success.
import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useI18n } from "../../components/i18n";
import { Button, Card, Field, Input } from "../../components/ui";
import { apiErr } from "../../lib/apiErr";

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
        setError(apiErr(d, t("reset.invalid"), t));
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
        <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[var(--jf-primary)] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
          JF
        </div>
        <h1 className="text-xl font-semibold text-[var(--jf-text)]">{t("reset.title")}</h1>
        <p className="mt-1.5 text-sm text-[var(--jf-muted)]">{t("reset.desc")}</p>
      </div>

      <Card className="p-jf-6">
        {done ? (
          <div className="space-y-3">
            <p className="rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-3 text-jf-sm text-green-300">
              {t("reset.done")}
            </p>
            <Button as="a" href="/login" className="w-full">
              {t("login.tabLogin")}
            </Button>
          </div>
        ) : !token ? (
          // Card t_8f20418d: `text-red-700`, not `-600`. The message now sits
          // inside `Card`, whose fill lives in the primitive — a surface this
          // file no longer names — so the red/sky guard judges it against the
          // page background, where `text-red-600` is 4.48:1 (below AA) and
          // `text-red-700` clears it. Same sentence, one step darker.
          <p className="text-sm text-red-700">{t("reset.invalid")}</p>
        ) : (
          <form onSubmit={submit} className="space-y-3">
            {error && (
              <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-jf-sm text-red-700">{error}</p>
            )}
            <Field id="reset-new-pw" label={t("reset.newPw")}>
              <Input
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                type="password"
                required
                minLength={10}
                placeholder="••••••••••"
              />
            </Field>
            <Field id="reset-confirm-pw" label={t("reset.confirmPw")}>
              <Input
                value={pw2}
                onChange={(e) => setPw2(e.target.value)}
                type="password"
                required
                placeholder="••••••••••"
              />
            </Field>
            <Button type="submit" disabled={busy} className="mt-1 w-full">
              {busy ? "…" : t("reset.submit")}
            </Button>
          </form>
        )}
      </Card>
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
