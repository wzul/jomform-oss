"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../../components/i18n";

type Mode = "login" | "register" | "forgot" | "magic";
type SocialProvider = { id: string; name: string };

// Issue #71: two-step login card (state machine in the same component, no
// routing change). Step 1 = email + password (+ remember + forgot). When the
// backend answers 401 next_step="totp_required", we transition to Step 2 which
// shows ONLY the 6-digit 2FA code input (+ trust checkbox + back link) — not an
// inline error-appended third field.
type Step = 1 | 2;

// Credentials captured on Step 1 and replayed on Step 2 so the browser is never
// left holding a half-finished form state while the 2FA step is shown.
type Cred = { email: string; password: string; remember: boolean };

// maskEmail("wan@kedai.my") -> "wa••@kedai.my" — shows just enough for the user
// to confirm which account they're authenticating into without leaking it.
function maskEmail(email: string): string {
  const at = email.indexOf("@");
  if (at <= 1) return email;
  return email.slice(0, 2) + "••" + email.slice(at);
}

export default function Login() {
  const { t } = useI18n();
  const [mode, setMode] = useState<Mode>("login");
  const [step, setStep] = useState<Step>(1);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [totpRemember, setTotpRemember] = useState(true);
  const [remember, setRemember] = useState(false);
  const [done, setDone] = useState(false);
  // Issue #71: credentials captured at Step 1, replayed at Step 2.
  const [cred, setCred] = useState<Cred>({ email: "", password: "", remember: false });
  // Controlled 2FA code on Step 2 (autofocus + auto-submit on 6 digits).
  const [code, setCode] = useState("");
  // Issue #72: tracks whether the user clicked "send email code" on the 2FA
  // step, and shows a hint that a 6-digit backup code was emailed.
  const [emailCodeSent, setEmailCodeSent] = useState(false);
  // Issue #9: social buttons render only for providers configured on the
  // backend (env-gated). null = list still loading → render none.
  const [socialProviders, setSocialProviders] = useState<SocialProvider[] | null>(null);
  const codeRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    fetch("/api/auth/social/providers")
      .then((r) => (r.ok ? r.json() : []))
      .then((d) => setSocialProviders(Array.isArray(d) ? d : []))
      .catch(() => setSocialProviders([]));
  }, []);

  // Issue #71: autofocus the (only visible) code input when entering Step 2.
  useEffect(() => {
    if (step === 2) {
      // Slight delay so the input is mounted before focusing on mobile.
      const id = window.setTimeout(() => codeRef.current?.focus(), 50);
      return () => window.clearTimeout(id);
    }
  }, [step]);

  // Issue #71: auto-submit the moment 6 digits are entered (paste-friendly).
  useEffect(() => {
    if (step === 2 && code.length === 6 && !busy) {
      submitTotp(code);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code]);

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const fd = new FormData(e.currentTarget);
    const path = mode === "login" ? "/api/auth/login" : mode === "register" ? "/api/auth/register" : mode === "magic" ? "/api/auth/magic-link/request" : "/api/auth/forgot-password";

    if (mode === "forgot" || mode === "magic") {
      const r = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: String(fd.get("email") || "") }),
      });
      setBusy(false);
      if (r.ok) {
        setDone(true);
        setError("");
      } else {
        // Both endpoints are enumeration-safe generic 200s; fall back gracefully.
        setError((await r.json().catch(() => ({}))).error || t(mode === "magic" ? "login.passwordlessSent" : "login.forgotSent"));
      }
      return;
    }

    const body: Record<string, string | boolean> = {
      email: String(fd.get("email") || ""),
      password: String(fd.get("password") || ""),
    };
    // Honeypot (issue #42): a real browser never fills this hidden field, so
    // the value stays empty; scripted bots that auto-fill every input trip it
    // and their registration is silently discarded server-side.
    body.website = String(fd.get("website") || "");
    if (mode === "login") body.remember = remember;

    const r = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    setBusy(false);

    if (r.ok) {
      const okBody = await r.json().catch(() => ({}));
      if (mode === "register" && (okBody as any).email_verification === "required") {
        setDone(true);
        setError("");
        return;
      }
      if (mode === "register") {
        setDone(true);
        setError("");
        return;
      }
      // #70 login: navigate immediately, no "Berjaya log masuk" flash. The
      // dashboard-bundle (#29) already returns spaces, so no extra /
      // api/spaces round-trip is needed before redirecting.
      window.location.href = "/";
      return;
    }
    const d = await r.json().catch(() => ({}));
    if (d.next_step === "totp_required") {
      // #71: transition to the dedicated 2FA step. Remember the credentials so
      // Step 2 can replay them and the back link can prefill. Neutral — not an
      // error banner.
      setCred({ email: String(fd.get("email") || ""), password: String(fd.get("password") || ""), remember });
      setCode("");
      setStep(2);
      return;
    }
    setError(d.error || t("login.genericError"));
  }

  async function submitTotp(totpCode: string) {
    setBusy(true);
    setError("");
    const r = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: cred.email,
        password: cred.password,
        totp_code: totpCode,
        // Issue #63: trust this device opted in on the 2FA step.
        totp_remember: totpRemember,
        remember: cred.remember,
      }),
    });
    setBusy(false);
    if (r.ok) {
      // #70 login: navigate immediately (no success banner flash).
      window.location.href = "/";
      return;
    }
    const d = await r.json().catch(() => ({}));
    // Wrong code is a genuine error — inline, red, under the code input (#42
    // login-limiter UX stays intact for repeated failures).
    setError(d.error || t("login.genericError"));
    setCode("");
    codeRef.current?.focus();
  }

  // Issue #71: "Ganti emel / Guna akaun lain" — back to Step 1, email prefilled.
  function backToCredentials() {
    setStep(1);
    setCode("");
    setError("");
  }

  // Issue #72: on the 2FA step, request a one-time backup code via email. The
  // endpoint is a generic 200; the backend only sends if the password was
  // already proven on this IP. Uses the email captured in `cred` on Step 1.
  async function requestEmailCode() {
    setError("");
    setBusy(true);
    try {
      const r = await fetch("/api/auth/2fa/email-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: cred.email }),
      });
      if (r.ok) {
        setEmailCodeSent(true);
      } else {
        setError(t("login.genericError"));
      }
    } catch {
      setError(t("login.genericError"));
    } finally {
      setBusy(false);
    }
  }

  async function passkeyLogin() {
    setError(""); setBusy(true);
    try {
      const st = await fetch("/api/auth/passkeys/login/start", { method: "POST" }).then((r) => r.json());
      const opts = st.options.publicKey || st.options;
      opts.challenge = Uint8Array.from(atob(opts.challenge.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
      const cred = await navigator.credentials.get({ publicKey: opts }) as PublicKeyCredential;
      const resp = (cred as any).response;
      const body = {
        key: st.key,
        id: btoa(String.fromCharCode(...new Uint8Array(cred.rawId))),
        rawId: btoa(String.fromCharCode(...new Uint8Array(cred.rawId))),
        type: cred.type,
        response: {
          authenticatorData: btoa(String.fromCharCode(...new Uint8Array(resp.authenticatorData))),
          clientDataJSON: btoa(String.fromCharCode(...new Uint8Array(resp.clientDataJSON))),
          signature: btoa(String.fromCharCode(...new Uint8Array(resp.signature))),
          userHandle: resp.userHandle ? btoa(String.fromCharCode(...new Uint8Array(resp.userHandle))) : null,
        },
      };
      const fin = await fetch("/api/auth/passkeys/login/finish", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      if (fin.ok) { window.location.href = "/"; return; }
      setError((await fin.json().catch(() => ({}))).error || t("login.passkeyInvalid"));
    } catch {
      setError(t("login.passkeyUnsupported"));
    }
    setBusy(false);
  }

  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[var(--jf-bg)] px-6">
      {/* ambient glow */}
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-[-260px] h-[420px] w-[720px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{
          background:
            "radial-gradient(closest-side, rgba(94,106,210,0.35), rgba(113,112,255,0.12), transparent)",
        }}
      />

      <div className="relative w-full max-w-sm">
        {/* logo */}
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
            JF
          </div>
          <h1 className="text-xl font-semibold text-[var(--jf-text)]" style={{ letterSpacing: "-0.02em" }}>
            {step === 2
              ? t("login.totpTitle")
              : mode === "login"
                ? t("login.welcome")
                : mode === "register"
                  ? t("login.registerTitle")
                  : mode === "magic"
                    ? t("login.passwordless")
                    : t("login.forgotTitle")}
          </h1>
          <p className="mt-1.5 text-sm text-[var(--jf-muted)]">
            {step === 2
              ? t("login.totpDesc", { email: maskEmail(cred.email) })
              : mode === "login"
                ? t("login.loginNote")
                : mode === "register"
                  ? t("login.freeNote")
                  : mode === "magic"
                    ? t("login.passwordlessDesc")
                    : t("login.forgotDesc")}
          </p>
        </div>

        <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
          {/* tabs (hidden during the dedicated 2FA step and in forgot/magic modes) */}
          {step === 1 && mode !== "forgot" && mode !== "magic" && (
            <div className="mb-5 grid grid-cols-2 rounded-lg border border-[var(--jf-border)] bg-slate-100 p-0.5">
              {(["login", "register"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => {
                    setMode(m);
                    setError("");
                    setStep(1);
                  }}
                  className={`rounded-md py-1.5 text-[13px] font-medium transition-colors ${
                    mode === m
                      ? "bg-[#5e6ad2]/20 text-[#4338ca] shadow-[inset_0_0_0_1px_rgba(113,112,255,0.35)]"
                      : "text-slate-600 hover:text-slate-800"
                  }`}
                >
                  {m === "login" ? t("login.tabLogin") : t("login.tabRegister")}
                </button>
              ))}
            </div>
          )}

          {/* Error banner — only ever a genuine failure (wrong code, wrong
              credentials). NOT used as an instruction banner. */}
          {error && (
            <p role="alert" className="mb-4 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-700">
              {error}
            </p>
          )}

          {done ? (
            <div className="space-y-3">
              <div className="rounded-lg border border-green-500/20 bg-green-500/10 px-3 py-3 text-[13px] text-green-700">
                {mode === "register"
                  ? t("login.registerDone")
                  : mode === "forgot"
                    ? t("login.forgotSent")
                    : mode === "magic"
                      ? t("login.passwordlessSent")
                      : t("login.loginDone")}
              </div>
              {mode !== "login" && (
                <button
                  type="button"
                  onClick={() => {
                    setMode("login");
                    setDone(false);
                  }}
                  className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff]"
                >
                  {mode === "forgot" || mode === "magic" ? t(mode === "magic" ? "login.passwordlessBack" : "login.forgotBack") : t("login.backToLogin")}
                </button>
              )}
            </div>
          ) : step === 2 ? (
            /* #71: Step 2 — dedicated 2FA. ONLY the code input + trust
               checkbox + back link. Email/password are NOT visible here. */
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (code.length === 6) submitTotp(code);
              }}
              className="space-y-3"
            >
              <div>
                <input
                  ref={codeRef}
                  name="totp_code"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                  inputMode="numeric"
                  pattern="[0-9]*"
                  autoComplete="one-time-code"
                  maxLength={6}
                  disabled={busy}
                  placeholder="000000"
                  aria-label={t("login.totpTitle")}
                  className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-3 text-center font-mono text-2xl tracking-[0.5em] text-[var(--jf-text)] outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
                />
              </div>
              {/* Issue #63: trusted-device opt-in, default checked — server
                  mints jf_trust on a successful code only. */}
              <label className="flex cursor-pointer items-start gap-2 text-[13px] text-[var(--jf-muted)]">
                <input
                  type="checkbox"
                  checked={totpRemember}
                  disabled={busy}
                  onChange={(e) => setTotpRemember(e.target.checked)}
                  className="mt-0.5 accent-[#5e6ad2]"
                />
                <span>{t("login.totpRemember")}</span>
              </label>
              {/* Issue #72: emailed one-time backup-code alternative. Muted,
                  added cleanly without disturbing the 2FA step. Uses the email
                  captured in `cred` on Step 1. */}
              <div className="pt-1">
                {emailCodeSent ? (
                  <p className="text-[12px] text-[var(--jf-muted)]">{t("login.emailCodeSent")}</p>
                ) : (
                  <button
                    type="button"
                    disabled={busy || !cred.email}
                    onClick={requestEmailCode}
                    className="text-[12px] text-[var(--jf-muted)] hover:text-[#7170ff] hover:underline disabled:opacity-50"
                  >
                    {t("login.emailCodeLink")}
                  </button>
                )}
              </div>
              <button
                disabled={busy || code.length !== 6}
                className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-60"
              >
                {busy ? "…" : t("login.totpSubmit")}
              </button>
              <button
                type="button"
                onClick={backToCredentials}
                disabled={busy}
                className="mt-1 w-full text-[13px] font-medium text-[#5e6ad2] hover:underline"
              >
                {t("login.totpBack")}
              </button>
            </form>
          ) : (
            <form onSubmit={submit} className="space-y-3">
              {/* Honeypot (issue #42): sr-only hides it from humans &
                  screen-readers (no focus, no layout) but it stays in the DOM,
                  so naive bots that fill every field trip the server-side
                  discard. */}
              {mode !== "forgot" && mode !== "magic" && (
                <div className="sr-only" aria-hidden="true">
                  <label htmlFor="website">{t("login.honeypot")}</label>
                  <input
                    id="website"
                    name="website"
                    type="text"
                    tabIndex={-1}
                    autoComplete="off"
                    placeholder="https://"
                  />
                </div>
              )}

              <div>
                <label className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">{t("login.email")}</label>
                <input
                  name="email"
                  type="email"
                  required
                  defaultValue={cred.email}
                  autoComplete="email"
                  placeholder="anda@kedai.my"
                  className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-2 text-sm text-[var(--jf-text)] placeholder-slate-400 outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
                />
              </div>
              {mode !== "forgot" && mode !== "magic" && (
                <div>
                  <label className="mb-1 block text-xs font-medium text-[var(--jf-muted)]">
                    {mode === "register" ? t("login.pwRegister") : t("login.password")}
                  </label>
                  <input
                    name="password"
                    type="password"
                    required
                    defaultValue={cred.password}
                    minLength={mode === "register" ? 10 : 1}
                    placeholder="••••••••••"
                    className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-2 text-sm text-[var(--jf-text)] placeholder-slate-400 outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
                  />
                </div>
              )}

              {mode === "login" && (
                <div className="flex items-center justify-between">
                  {/* Issue #62: remember me — unchecked (default) = browser
                      session cookie + 24h server TTL; checked = 14 days. */}
                  <label className="flex cursor-pointer items-center gap-2 text-[13px] text-[var(--jf-muted)]">
                    <input
                      type="checkbox"
                      checked={remember}
                      onChange={(e) => setRemember(e.target.checked)}
                      className="accent-[#5e6ad2]"
                    />
                    <span>{t("login.remember")}</span>
                  </label>
                  {/* Issue #62: forgot-password inline form link. */}
                  <button
                    type="button"
                    onClick={() => { setMode("forgot"); setError(""); setDone(false); }}
                    className="text-[13px] font-medium text-[#5e6ad2] hover:underline"
                  >
                    {t("login.forgot")}
                  </button>
                </div>
              )}
              {mode === "forgot" && (
                <button
                  type="button"
                  onClick={() => { setMode("login"); setError(""); setDone(false); }}
                  className="text-[13px] font-medium text-[#5e6ad2] hover:underline"
                >
                  {t("login.forgotBack")}
                </button>
              )}
              {mode === "magic" && (
                <button
                  type="button"
                  onClick={() => { setMode("login"); setError(""); setDone(false); }}
                  className="text-[13px] font-medium text-[#5e6ad2] hover:underline"
                >
                  {t("login.passwordlessBack")}
                </button>
              )}

              <button
                disabled={busy}
                className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-60"
              >
                {busy ? "…" : mode === "login" ? t("login.submitLogin") : mode === "register" ? t("login.submitRegister") : mode === "magic" ? t("login.passwordlessSubmit") : t("login.forgotSubmit")}
              </button>
            </form>
          )}

          {/* Issue #73: passwordless magic-link entry point, in login mode only
              (not at the 2FA step). Minimal additive link. */}
          {mode === "login" && step === 1 && (
            <button
              type="button"
              onClick={() => { setMode("magic"); setError(""); setDone(false); }}
              className="mt-3 w-full text-center text-[13px] font-medium text-[#5e6ad2] hover:underline"
            >
              {t("login.passwordless")}
            </button>
          )}

          {/* Issue #9: env-gated social login. Rendered only when the backend
              reports the provider configured (GET /api/auth/social/providers);
              Cloudflare appears only when JF_CF_OIDC_* env vars are set. */}
          {step === 1 && mode !== "forgot" && mode !== "magic" && socialProviders && socialProviders.length > 0 && (
            <div className="mt-4 border-t border-[var(--jf-border)] pt-4">
              <div className="space-y-2">
                {socialProviders.map((p) => (
                  <a
                    key={p.id}
                    href={`/api/auth/oauth/${p.id}`}
                    className="flex w-full items-center justify-center gap-2 rounded-md border border-[var(--jf-border)] bg-white py-2 text-[13px] font-medium text-[var(--jf-text)] transition-colors hover:border-[#7170ff]/60 hover:bg-indigo-50"
                  >
                    {t("login.continueWith", { provider: p.name })}
                  </a>
                ))}
              </div>
            </div>
          )}
        </div>

        <p className="mt-6 text-center text-xs text-[#62666d]">
          {t("login.termsNote")}
        </p>
      </div>
    </main>
  );
}
