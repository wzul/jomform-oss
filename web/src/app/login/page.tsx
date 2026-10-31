"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useI18n } from "../../components/i18n";
import { resolvePostLogin } from "../../lib/postLogin";
import { browserNextTarget } from "../../lib/nextTarget";
import { b64url, fromB64url } from "../../lib/webauthn";
import { apiErr } from "../../lib/apiErr";
import { rateLimitErr } from "../../lib/rateLimitErr";
import { totpErr } from "../../lib/loginTotpErr";

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

// Issue #292: provider logos for the social login buttons. Each is a small,
// decorative inline SVG (aria-hidden) rendered to the LEFT of the button text,
// so screen readers announce the text content (e.g. "Continue with Google").
// Provider id keys match the ids returned by /api/auth/social/providers
// (google, cloudflare, azure_ad, github). Unknown ids fall back to the generic
// provider mark rather than breaking the button.
function ProviderLogo({ id }: { id: string }) {
  switch (id) {
    case "google":
      return (
        <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" className="h-4 w-4 shrink-0" data-testid="logo-google">
          <path fill="#EA4335" d="M12 5.04c1.62 0 3.06.56 4.2 1.65l3.14-3.14C17.45 1.8 14.97.75 12 .75 7.7.75 3.99 3.29 2.18 6.9l3.66 2.84C6.75 7.12 9.18 5.04 12 5.04z" />
          <path fill="#4285F4" d="M23.25 12.27c0-.89-.08-1.75-.23-2.58H12v4.88h6.3a5.8 5.8 0 0 1-2.52 3.81l3.57 2.77c2.1-1.94 3.9-4.8 3.9-8.88z" />
          <path fill="#FBBC05" d="M5.84 14.26a6.95 6.95 0 0 1 0-4.52L2.18 6.9a11.16 11.16 0 0 0 0 10.2l3.66-2.84z" />
          <path fill="#34A853" d="M12 23.25c3.02 0 5.55-1 7.4-2.69l-3.57-2.77c-1 .66-2.27 1.05-3.83 1.05-2.82 0-5.25-2.08-5.83-4.85L2.18 17.1C3.99 20.71 7.7 23.25 12 23.25z" />
        </svg>
      );
    case "cloudflare":
      return (
        <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" className="h-4 w-4 shrink-0" data-testid="logo-cloudflare">
          <path fill="#F48120" d="M13.6 6.1a6.7 6.7 0 0 0-.5 0l.2-.5C14.3 3.5 13.4 2 11.7 2c-1.9 0-3.4 1.5-3.4 3.4 0 .2 0 .3.1.5C7 6 5.5 7.6 5.5 9.7c0 .2 0 .4.1.6H4c-.2 0-.4.2-.4.4s.2.4.4.4h9.5a.5.5 0 0 0 0-1H8.2c-.4 0-.8-.4-.7-.9 0-.4.4-.8.9-.8h10.5c1.4 0 2.5-1.1 2.5-2.5s-1.1-2.5-2.5-2.5c-.3 0-.6 0-.8.1-.6-.9-1.6-1.4-2.6-1.4z" />
          <path fill="#FAAA3F" d="M5.4 20.2c-.1 0-.2-.2-.1-.3 0-.2.2-.4.4-.4h10.1c.7 0 1.3-.6 1.3-1.3v-.1h3.4c.7 0 1.3-.5 1.3-1.2 0-.1 0-.2 0-.3-1.2.3-2.5.3-3.8 0 0 .6-.5 1-1 1H9.5c-1.4 0-2.6-1.1-2.6-2.5 0-.1 0-.2 0-.3.3-1.5 1.4-2.4 2.9-2.4.1 0 .2 0 .3.1l1.1-.6c-.5-.3-1-.5-1.5-.5-1.7-.1-3.4.5-4.3 2.2-.6.4-1.4.7-2.3.7-1.2.1-2.3.9-2.5 2.1-.3 1.3.6 2.4 1.8 2.4h.3z" />
        </svg>
      );
    // GitHub (octocat mark) and Microsoft/Azure AD share a simple, neutral
    // fallback mark so future providers keep working without a dedicated logo.
    default:
      return (
        <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" className="h-4 w-4 shrink-0" data-testid="logo-generic">
          <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="1.8" />
          <circle cx="12" cy="12" r="2.2" fill="currentColor" />
        </svg>
      );
  }
}

// Issue #140: the OAuth authorize flow redirects to /login?next=<authorize URL>
// (which lives on mcp.jomform.com). Read that value once at mount so standard
// login AND the TOTP step-2 redirect back to it after a successful sign-in,
// instead of dropping to "/". Validated against open-redirect attacks.
//
// The rule itself now lives in lib/nextTarget.ts (card t_5e39dc9f), because the
// DEFAULT passwordless flow needs the same value: this page forwards the
// validated `next` with the magic-link request (see `submit`), the server puts
// it in the emailed link, and /magic-link validates it with the same function.
// One rule, three call sites — instead of a copy per surface.
function readNext(): string {
  // Prerender/SSR has no window; only run the guard in the browser.
  if (typeof window === "undefined") return "";
  return browserNextTarget(new URLSearchParams(window.location.search).get("next"));
}

export default function Login() {
  const { t, lang } = useI18n();
  // Issue #161: an invite link should land the user on the sign-up tab so
  // they can create the account that redeems the invite.
  // Issue #271: passwordless (magic link) is the default login — the page
  // opens on the email-only flow. Password is the secondary option.
  const [mode, setMode] = useState<Mode>(() =>
    typeof window !== "undefined" && new URLSearchParams(window.location.search).get("invite") ? "register" : "magic"
  );
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
  // Issue #140: post-login redirect target from ?next (validated at mount).
  const [next, setNext] = useState<string>(readNext());
  // Issue #161: space-invite redemption token from ?invite (signup link).
  const [invite, setInvite] = useState<string>(() =>
    typeof window === "undefined" ? "" : new URLSearchParams(window.location.search).get("invite") || ""
  );
  const codeRef = useRef<HTMLInputElement>(null);
  // Issue #313: the default (email-first) view doubles as the signup entry.
  // See the magic-mode hint below.
  const emailRef = useRef<HTMLInputElement>(null);
  // Card t_df9233f6: WebAuthn support is a property of the BROWSER, so it can
  // only be known after mount (the page prerenders on the server). null =
  // not yet known → the control is not rendered, which is also what a browser
  // without WebAuthn gets, so no user is offered a button that cannot work.
  const [passkeySupported, setPasskeySupported] = useState(false);

  useEffect(() => {
    setPasskeySupported(
      typeof window !== "undefined" && !!window.PublicKeyCredential && !!navigator.credentials?.get
    );
  }, []);

  useEffect(() => {
    // Issue #140: read ?next once after mount for the OAuth redirect path.
    setNext(readNext());
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
      const body: Record<string, string> = { email: String(fd.get("email") || "") };
      // Issue #278: the magic-link path is hybrid (login + signup). Send the
      // honeypot so a bot that fills the hidden field is silently discarded
      // server-side, and the locale so the signup email renders in the user's
      // language (issue #206).
      if (mode === "magic") {
        body.website = String(fd.get("website") || "");
        body.lang = lang;
        // Card t_5e39dc9f: carry the validated ?next= (the page the visitor was
        // bounced from) with the request, so the emailed link can return them to
        // it. Without this the passwordless flow — the DEFAULT one — always
        // dropped the visitor on /workspace-picker. Only the validated value
        // travels; an absent or discarded one sends nothing, i.e. the pre-fix
        // behaviour, and the server re-validates what it receives.
        if (next) body.next = next;
      }
      const r = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      setBusy(false);
      if (r.ok) {
        setDone(true);
        setError("");
      } else {
        // Both endpoints are enumeration-safe generic 200s; a non-200 here is a
        // genuine failure (e.g. the 429 rate-limit, which comes back as plain
        // text, not JSON). Parse JSON when we can, otherwise surface a generic
        // error — never fall back to the success copy (issue #343).
        //
        // Card t_e50a9680: a 429 is answered with the window the server chose
        // (`Retry-After`, relayed by the hop) before anything generic is tried,
        // so "try again" comes with a when. The plain-text body is why this reads
        // the STATUS as well as the body.
        const errBody = await r.json().catch(() => ({}));
        setError(
          rateLimitErr(r.status, r.headers.get("retry-after"), errBody, t) ||
            apiErr(errBody, t("login.genericError"), t)
        );
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
    // Issue #161: pass the invite token so a signup redeems the space invite.
    if (mode === "register" && invite) body.invite = invite;
    // Issue #206: pass the chosen locale so the verification link carries
    // ?lang= and the confirmation page renders in the user's language.
    if (mode === "register") body.lang = lang;

    const r = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    setBusy(false);

    if (r.ok) {
      const okBody = (await r.json().catch(() => ({}))) as { email_verification?: string };
      if (mode === "register" && okBody.email_verification === "required") {
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
      // api/workspaces round-trip is needed before redirecting.
      // Issue #140: honour the validated ?next (OAuth authorize) or fall back
      // to the dashboard root.
      // Issue #164: multi-workspace users without a default are routed to the
      // workspace-picker instead of the dashboard.
      window.location.href = await resolvePostLogin(next);
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
    // Card t_e50a9680: the login lockout answers 429 + `Retry-After: 900` with
    // the class code `request.tooManyRequests`, which the dictionary does not map
    // on its own — so the window is read off the response and named. Every other
    // failure (a 401 `login.invalid` above all) is unchanged: `rateLimitErr`
    // returns "" and this line is the pre-existing one.
    setError(
      rateLimitErr(r.status, r.headers.get("retry-after"), d, t) || apiErr(d, t("login.genericError"), t)
    );
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
      // Issue #140: honour the validated ?next (OAuth authorize) or fall back
      // to the dashboard root.
      // Issue #164: multi-workspace users without a default are routed to the
      // workspace-picker instead of the dashboard.
      window.location.href = await resolvePostLogin(next);
      return;
    }
    const d = await r.json().catch(() => ({}));
    // Wrong code is a genuine error — inline, red, under the code input (#42
    // login-limiter UX stays intact for repeated failures).
    //
    // Card t_adf8e1e6: this step's failure is about the CODE, not the password.
    // The server answers `login.invalid` for both on purpose (uniform 401 — see
    // api/internal/httpapi/auth.go's failLogin), so the sentence is chosen HERE,
    // where we know the password was already accepted: `totpErr` re-maps that one
    // code to "That code is invalid or expired. Request a new one." and leaves
    // every other code — and a non-JSON/unknown failure — on the generic sentence.
    //
    // Card t_e50a9680: a 429 on this step is still a throttle refusal, and the
    // window comes first — `rateLimitErr` answers "" for anything else, so the
    // code sentence above is untouched.
    setError(rateLimitErr(r.status, r.headers.get("retry-after"), d, t) || totpErr(d, t));
    setCode("");
    codeRef.current?.focus();
  }

  // Issue #71: back to Step 1, email prefilled.
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
        // Card t_e50a9680: same 429 rule as the forms — the endpoint is
        // per-IP limited, and a refusal that can name its window says so.
        setError(rateLimitErr(r.status, r.headers.get("retry-after"), {}, t) || t("login.genericError"));
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
      // Card t_7f01f01f: read the STATUS before touching the body. Every
      // refusal this endpoint can answer — the per-IP limiter's 429 (plain
      // text plus `Retry-After: 30`, middleware/ratelimit.go), a 5xx, a proxy
      // error page — used to make `r.json()` throw into the catch below, which
      // told the visitor their BROWSER could not do passkeys while the server
      // was the one that had refused, and dropped the window with it.
      const startRes = await fetch("/api/auth/passkeys/login/start", { method: "POST" });
      if (!startRes.ok) {
        // `rateLimitErr` names the window the throttle chose (card t_e50a9680),
        // exactly as the password form, the 2FA step and the emailed-code
        // request do; anything else is the page's own failure line. The body is
        // read only for a machine `code`, so a plain-text or HTML answer is
        // expected here and must not throw again.
        const refusal = await startRes.json().catch(() => ({}));
        setError(
          rateLimitErr(startRes.status, startRes.headers.get("retry-after"), refusal, t) ||
            t("login.genericError"),
        );
        setBusy(false);
        return;
      }
      const st = await startRes.json();
      // A 200 that carries no challenge is a broken start, not a browser that
      // declined: nothing was ever asked of the browser.
      if (!st?.options) { setError(t("login.genericError")); setBusy(false); return; }
      const opts = st.options.publicKey || st.options;
      // The server's challenge and any credential descriptor id arrive
      // base64url; navigator.credentials.get() wants the raw bytes.
      opts.challenge = fromB64url(opts.challenge);
      // ONLY the ceremony itself may be reported as a browser refusal. A
      // dismissed prompt, or no resident key on this device, is what
      // `login.passkeyUnsupported` says — every failure above it is the server.
      let cred: PublicKeyCredential | null = null;
      try {
        cred = await navigator.credentials.get({ publicKey: opts }) as PublicKeyCredential;
      } catch {
        setError(t("login.passkeyUnsupported")); setBusy(false); return;
      }
      if (!cred) { setError(t("login.passkeyUnsupported")); setBusy(false); return; }
      const resp = cred.response as AuthenticatorAssertionResponse;
      // Every binary field below MUST be base64url: go-webauthn decodes them
      // with base64.RawURLEncoding, so standard base64 (what `btoa` emits) is
      // rejected as "response tidak sah". See lib/webauthn.ts.
      const body = {
        key: st.key,
        id: b64url(new Uint8Array(cred.rawId)),
        rawId: b64url(new Uint8Array(cred.rawId)),
        type: cred.type,
        response: {
          authenticatorData: b64url(new Uint8Array(resp.authenticatorData)),
          clientDataJSON: b64url(new Uint8Array(resp.clientDataJSON)),
          signature: b64url(new Uint8Array(resp.signature)),
          userHandle: resp.userHandle ? b64url(new Uint8Array(resp.userHandle)) : null,
        },
      };
      const fin = await fetch("/api/auth/passkeys/login/finish", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      if (fin.ok) { window.location.href = await resolvePostLogin(next); return; }
      // Card t_3c513380: the SAME limiter guards this call (rest.go,
      // `POST /v1/auth/passkeys/login/finish`), and its 429 is `http.Error`
      // PLAIN TEXT plus `Retry-After` (middleware/ratelimit.go). So `fin.status`
      // is read BEFORE the body: `fin.json()` on that answer — or on a 5xx / a
      // proxy error page — threw into `.catch(() => ({}))`, which left
      // `apiErr({}, …)` to render `login.passkeyInvalid`. The visitor was told
      // their passkey was WRONG while the server had only refused the call, and
      // the throttle's window was dropped with the same throw.
      const finStatus = fin.status;
      // The body is read only for a machine `code`; plain text and HTML are
      // expected here, and the server's prose is never rendered (apiErr.ts).
      const refusal = await fin.json().catch(() => ({}));
      if (finStatus === 429) {
        // The throttle's window comes from the response's own header, through
        // the shared mapping (card t_e50a9680) — never re-derived here.
        setError(
          rateLimitErr(finStatus, fin.headers.get("retry-after"), refusal, t) ||
            t("login.genericError"),
        );
      } else if (finStatus === 400 || finStatus === 401) {
        // The only two statuses with which the handler really rejects the
        // ASSERTION (api/internal/httpapi/passkey.go): 400 for a key / session /
        // response it could not use, 401 for a credential the library refused.
        // A wrong passkey is what happened, so that sentence is honest here and
        // a named machine `code` still wins through apiErr.
        setError(apiErr(refusal, t("login.passkeyInvalid"), t));
      } else {
        // 403 (the WebAuthn origin check), 404, a 5xx, a proxy error page: the
        // server never judged the passkey, so nothing here may call it invalid.
        setError(t("login.genericError"));
      }
    } catch {
      // Transport failure, an unreadable body, a challenge this page cannot
      // decode: the call to the server failed. Say that — never that the
      // browser lacks passkeys.
      setError(t("login.genericError"));
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
        {/* logo — issue #436: clicking the JF logo returns to the home/landing page */}
        <div className="mb-8 text-center">
          <Link
            href="/"
            aria-label={t("login.backToHome")}
            className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)] transition-opacity hover:opacity-90"
          >
            JF
          </Link>
          <h1 className="text-xl font-semibold text-[var(--jf-text)]" style={{ letterSpacing: "-0.02em" }}>
            {step === 2
              ? t("login.totpTitle")
              : mode === "login"
                ? t("login.welcome")
                : mode === "register"
                  ? t("login.registerTitle")
                  : mode === "magic"
                    ? t("login.emailFirstTitle")
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
                    ? t("login.emailFirstDesc")
                    : t("login.forgotDesc")}
          </p>
        </div>

        <div className="rounded-xl border border-[var(--jf-border)] bg-white p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
          {/* issue #313: the default (magic) view is the single email-first
              screen — it handles BOTH login and signup, so no login/register
              tabs. The login/register tab semantics only matter once the user
              opens the password form (mode login/register). Hidden during the
              dedicated 2FA step and in forgot mode. */}
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
                  className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[var(--jf-primary-hover)]"
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
                className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[var(--jf-primary-hover)] disabled:opacity-60"
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
                  discard. Issue #278: also rendered on the hybrid magic-link
                  form so signup-via-magic-link gets the same anti-spam. */}
              {mode !== "forgot" && (
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
                  ref={emailRef}
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
                  {/* Issue #303: password is OPTIONAL on signup. Leaving it
                      blank assigns a random UUID server-side; the user can set
                      a real password later. It stays required only for login. */}
                  <input
                    name="password"
                    type="password"
                    required={mode !== "register"}
                    defaultValue={cred.password}
                    minLength={mode === "register" ? 0 : 1}
                    placeholder={mode === "register" ? t("login.pwRegisterPh") : "••••••••••"}
                    className="w-full rounded-md border border-[var(--jf-border)] bg-white px-3 py-2 text-sm text-[var(--jf-text)] placeholder-slate-400 outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-indigo-50"
                  />
                  {mode === "register" && (
                    <p className="mt-1 text-[11px] text-[var(--jf-muted)]">{t("login.pwOptional")}</p>
                  )}
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
                <div className="space-y-3">
                  {/* Issue #313: the single email field already handles BOTH
                      login and signup (hybrid magic-link, #278). The hint below
                      is a quiet reassurance for new users — no separate
                      signup tab needed. */}
                  <p className="text-[12px] leading-snug text-[var(--jf-muted)]">
                    {t("login.newToJomform")}
                  </p>
                  {/* Password is the secondary option — a small link that opens
                      the login/register password form. Only then do the
                      login/register tab semantics matter. */}
                  <button
                    type="button"
                    onClick={() => { setMode("login"); setError(""); setDone(false); }}
                    disabled={busy}
                    className="text-[13px] font-medium text-[#5e6ad2] hover:underline"
                  >
                    {t("login.loginWithPassword")} {t("login.orSignupWithPassword")}
                  </button>
                </div>
              )}

              <button
                disabled={busy}
                className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[var(--jf-primary-hover)] disabled:opacity-60"
              >
                {busy ? "…" : mode === "login" ? t("login.submitLogin") : mode === "register" ? t("login.submitRegister") : mode === "magic" ? t("login.continue") : t("login.forgotSubmit")}
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

          {/* Passkey sign-in (card t_df9233f6). passkeyLogin() had existed since
              6816eff with NO caller in any commit, so the shipped login page
              rendered no passkey control at all and the whole resident-key
              ceremony was unreachable. Rendered on every step-1 view that shows
              credentials (the default magic view too) because a discoverable
              login needs no email, and hidden entirely on a browser with no
              WebAuthn support rather than offered as a button that can only
              fail. */}
          {step === 1 && mode !== "forgot" && passkeySupported && (
            <div className="mt-3 border-t border-[var(--jf-border)] pt-3">
              <button
                type="button"
                onClick={passkeyLogin}
                disabled={busy}
                data-testid="passkey-login"
                className="flex w-full items-center justify-center gap-2 rounded-md border border-[var(--jf-border)] bg-white py-2 text-[13px] font-medium text-[var(--jf-text)] transition-colors hover:border-[#7170ff]/60 hover:bg-indigo-50 disabled:opacity-60"
              >
                <span aria-hidden="true" className="text-base leading-none">🔑</span>
                {t("login.passkey")}
              </button>
            </div>
          )}

          {/* Issue #9: env-gated social login. Rendered only when the backend
              reports the provider configured (GET /api/auth/social/providers);
              Cloudflare appears only when JF_CF_OIDC_* env vars are set.
              Issue #271: shown on the default magic-link view too, so social
              stays prominent alongside passwordless. */}
          {step === 1 && mode !== "forgot" && socialProviders && socialProviders.length > 0 && (
            <div className="mt-4 border-t border-[var(--jf-border)] pt-4">
              <div className="space-y-2">
                {socialProviders.map((p) => (
                  <a
                    key={p.id}
                    href={`/api/auth/oauth/${p.id}`}
                    className="flex w-full items-center justify-center gap-2 rounded-md border border-[var(--jf-border)] bg-white py-2 text-[13px] font-medium text-[var(--jf-text)] transition-colors hover:border-[#7170ff]/60 hover:bg-indigo-50"
                  >
                    {/* Issue #292: provider logo (decorative) rendered left of the text */}
                    <ProviderLogo id={p.id} />
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

        {/* issue #436: subtle back-to-home link below the login card */}
        <p className="mt-4 text-center">
          <Link
            href="/"
            className="text-xs font-medium text-[#62666d] transition-colors hover:text-[#5e6ad2]"
          >
            {t("login.backToHome")}
          </Link>
        </p>
      </div>
    </main>
  );
}
