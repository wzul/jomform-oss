"use client";

// Profile page — user identity & security (name, email, password, 2FA, passkeys),
// organised into Identity / Security / Passkeys tabs.
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import QRCode from "qrcode";
import { Shell } from "../../components/Shell";
import { useI18n, LANGS_SHOWN, LANG_LABEL, type Lang } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";
import { PROFILE_NAME_MAX_CHARS, normalizeProfileName } from "../../lib/profileIdentity";
import { isValidStepUpCode, stepUpRequired } from "../../lib/stepUp";
import { apiErr } from "../../lib/apiErr";
import { panelDomId, tabDomId, tabStop, tablistKeyDown } from "../../lib/tablist";
import { classifyLoadError, fetchJson } from "../../lib/wsfetch";
import { b64url, fromB64url } from "../../lib/webauthn";

type Profile = {
  id: string;
  email: string;
  name: string;
  avatar_url: string; // issue #133
  phone: string; // issue #181 optional phone number (empty = not set)
  timezone: string; // issue #191 preferred IANA timezone (empty = Auto → browser default)
  totp_enabled: boolean;
  created_at: string;
};

// Issue #301: connected accounts (social login connectors) are user-level.
type SocialRow = { provider: string; email?: string; linked_at?: string };

// Issue #307: connected MCP agents (OAuth clients) are user-level too — a
// user binds one agent and it can operate across all their spaces.
type MCPAgentRow = {
  client_id: string;
  name: string;
  created_at?: string;
  last_used_at?: string;
};

// Issue #454: one trusted 2FA device (Profile → Security management list).
type TrustDeviceRow = {
  id: string;
  label?: string;
  ip?: string;
  first_seen?: string;
  expires_at?: string;
  current?: boolean;
};

type ProfileTab = "identity" | "security" | "passkeys" | "connected" | "mcp";
const TAB_IDS: ProfileTab[] = ["identity", "security", "passkeys", "connected", "mcp"];

// Issue #191: common IANA timezones offered in the Profile → Identity tab.
// The user can also pick Auto (empty) to follow the browser/PC default.
const COMMON_TIMEZONES = [
  "UTC",
  "Asia/Kuala_Lumpur",
  "Asia/Singapore",
  "Asia/Jakarta",
  "Asia/Manila",
  "Asia/Bangkok",
  "Asia/Ho_Chi_Minh",
  "Asia/Shanghai",
  "Asia/Hong_Kong",
  "Asia/Taipei",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Asia/Kolkata",
  "Asia/Dubai",
  "Asia/Riyadh",
  "Australia/Sydney",
  "Australia/Perth",
  "Pacific/Auckland",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Moscow",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Africa/Lagos",
  "Africa/Nairobi",
];

function loadStoredTab(): ProfileTab {
  if (typeof window === "undefined") return "identity";
  // Issue #310: when arriving via a social-connect callback (?linked=) or an
  // explicit ?tab=connected, open the Social accounts tab so the user sees
  // their freshly-linked account.
  const params = new URLSearchParams(window.location.search);
  if (params.get("linked") !== null) return "connected";
  if (params.get("tab") === "connected") return "connected";
  try {
    const v = window.localStorage.getItem("jf_profile_tab");
    if (v === "identity" || v === "security" || v === "passkeys" || v === "connected" || v === "mcp") return v;
  } catch {
    // storage unavailable — fall through
  }
  return "identity";
}

export default function ProfilePage() {
  const { t, lang: ctxLang, setLang } = useI18n();
  const router = useRouter();
  const { user: sessionUser, reload: reloadSession, anon } = useSession();
  const [tab, setTab] = useState<ProfileTab>(loadStoredTab);
  const [p, setP] = useState<Profile | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  // Issue #181: optional phone number state.
  const [phone, setPhone] = useState("");
  // Issue #191: preferred timezone state ("" = Auto → browser default).
  const [timezone, setTimezone] = useState("");
  // Language placement: the dashboard UI language is PER-PROFILE and lives in
  // the shared i18n context (persisted to /api/auth/profile/lang), so this page
  // does not keep its own copy — `ctxLang` is the single source of truth.
  // Issue #133: profile picture state — current avatar URL, upload progress.
  const [avatarUrl, setAvatarUrl] = useState("");
  const [avatarPreview, setAvatarPreview] = useState("");
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarFailed, setAvatarFailed] = useState(false);
  const [pwCurrent, setPwCurrent] = useState("");
  const [pwNew, setPwNew] = useState("");
  const [pwConfirm, setPwConfirm] = useState("");
  const [msg, setMsg] = useState("");
  const [passkeys, setPasskeys] = useState<{ id: string }[]>([]);
  const [err, setErr] = useState("");
  // 2FA setup modal state.
  const [show2FA, setShow2FA] = useState(false);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [secretText, setSecretText] = useState("");
  const [code, setCode] = useState("");
  const [enabling, setEnabling] = useState(false);
  // Issue #63: trusted-device state for the Profile → Security line.
  const [trustedUntil, setTrustedUntil] = useState("");
  // Issue #454: trusted-devices management list (label, IP, first trusted,
  // expiry) with a Revoke button per device + a revoke-all action.
  const [trustDevices, setTrustDevices] = useState<TrustDeviceRow[]>([]);
  // Issue #301: connected accounts (user-level, independent of the active
  // space) — loaded via GET /api/auth/social (currentUserID-scoped).
  const [social, setSocial] = useState<SocialRow[]>([]);
  // Issue #307: connected MCP agents (user-level OAuth clients) — loaded via
  // GET /api/auth/mcp-agents.
  const [agents, setAgents] = useState<MCPAgentRow[]>([]);
  // Issue #426: copy feedback for the server URL box.
  const [copied, setCopied] = useState(false);

  // Persist active tab.
  useEffect(() => {
    try {
      window.localStorage.setItem("jf_profile_tab", tab);
    } catch {
      // storage unavailable — ignore
    }
  }, [tab]);

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Issue #48: an anonymous visit bounces to the landing page — the guard every
  // other page under the shell carries. This page shipped WITHOUT it, so a
  // signed-out visitor sat on the account UI: measured 2026-10-11, an anonymous
  // GET /profile answered 200 with the shell (nav, "Sign out", the
  // Profile/Passkeys tabs) in the HTML while every request under it 401'd.
  // Guarded by src/lib/authedPageGuards.test.ts, which now pins the convention
  // across the whole app directory instead of this one page.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // A tab's own fetch must not paint an empty list when the session is dead:
  // "you have no passkeys" and "you are signed out" look identical otherwise.
  const loadAuthed = useCallback(
    <T,>(path: string, fallback: T, onOk: (v: T) => void) => {
      fetchJson<T>(path)
        .then(onOk)
        .catch((e) => {
          if (classifyLoadError(e).kind === "unauth") goToLogin();
          onOk(fallback);
        });
    },
    [goToLogin],
  );

  const load = useCallback(() => {
    // Issue #29: identity fields hydrate from the shared session context
    // (dashboard-bundle, fetched once per session) instead of a per-mount
    // /api/auth/profile round-trip. Saves call reloadSession() below, which
    // re-fetches the bundle so name/email/2FA state stay fresh. Passkeys
    // still load here, in parallel.
    if (sessionUser) {
      setP((prev) => ({
        id: sessionUser.id ?? prev?.id ?? "",
        email: sessionUser.email ?? prev?.email ?? "",
        name: sessionUser.name ?? prev?.name ?? "",
        avatar_url: sessionUser.avatar_url ?? prev?.avatar_url ?? "",
        phone: sessionUser.phone ?? prev?.phone ?? "",
        timezone: sessionUser.timezone ?? prev?.timezone ?? "",
        totp_enabled: sessionUser.totp_enabled ?? prev?.totp_enabled ?? false,
        created_at: prev?.created_at ?? "",
      }));
      setName((cur) => cur || sessionUser.name || "");
      setEmail((cur) => cur || sessionUser.email || "");
      setAvatarUrl((cur) => cur || sessionUser.avatar_url || "");
      setPhone((cur) => cur || sessionUser.phone || "");
      setTimezone((cur) => cur || sessionUser.timezone || "");
    }
    loadAuthed<{ id: string }[]>("/api/auth/passkeys", [], (k) =>
      setPasskeys(Array.isArray(k) ? k : []),
    );
    // Issue #301: connected accounts load straight from the user-level
    // /api/auth/social endpoint (no ?workspace=), so they're identical across all
    // of the user's spaces.
    loadAuthed<SocialRow[]>("/api/auth/social", [], (s) =>
      setSocial(Array.isArray(s) ? s : []),
    );
    // Issue #307: connected MCP agents load straight from the user-level
    // /api/auth/mcp-agents endpoint (no ?workspace=), so they're identical across
    // all of the user's spaces.
    loadAuthed<MCPAgentRow[]>("/api/auth/mcp-agents", [], (s) =>
      setAgents(Array.isArray(s) ? s : []),
    );
  }, [sessionUser, loadAuthed]);

  useEffect(() => { load(); }, [load]);

  // Issue #63: fetch trusted-device status for the Profile → Security line.
  // Issue #454: also fetch the full trusted-devices management list.
  const loadTrust = useCallback(() => {
    loadAuthed<{ trusted?: boolean; until?: string }>(
      "/api/auth/trust-status",
      {},
      (d) => setTrustedUntil(d.trusted ? d.until || "" : ""),
    );
    loadAuthed<TrustDeviceRow[]>("/api/auth/trust-devices", [], (d) =>
      setTrustDevices(Array.isArray(d) ? d : []),
    );
  }, [loadAuthed]);

  useEffect(() => { loadTrust(); }, [loadTrust]);

  async function post(path: string, body: unknown) {
    setMsg(""); setErr("");
    const r = await fetch(path, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (r.ok) { setMsg(t("prof.saved")); load(); reloadSession(); return true; }
    const d = await r.json().catch(() => ({}));
    setErr(apiErr(d, t("prof.failed"), t));
    return false;
  }

  // Issue #458: a magic-link session belonging to a 2FA-enabled user must
  // confirm a fresh TOTP code before a sensitive action (change password /
  // disable 2FA / add passkey). Everyone else is unaffected. When the backend
  // answers 401 + next_step=totp_stepup_required we prompt for the code and
  // retry the same request once with totp_code attached.
  //
  // bodyFn builds the payload from the collected code, so an action whose proof
  // IS the code (disable 2FA) carries it instead of a password.
  //
  // opts.prompt forces the prompt for such an action, even when the #458
  // step-up does not apply — disabling 2FA takes a code on EVERY session type.
  // Because that prompt always supplies a code, the gate below can never fire
  // for the disable action: the code is sent in the first request and consumed
  // by the handler as the action's own proof. (A separate step-up prompt would
  // ask for a SECOND code from the same 30s window, which #528 refuses — the
  // failure mode in the report.)
  async function postSensitive(
    path: string,
    bodyFn: (code: string) => Record<string, unknown>,
    opts?: { prompt: string },
  ): Promise<boolean> {
    setMsg(""); setErr("");
    const send = (payload: Record<string, unknown>) =>
      fetch(path, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
    // Pre-empt the prompt when we already know a code is needed.
    let code = "";
    if (opts || stepUpRequired(sessionUser, p)) {
      const entered = window.prompt(opts ? opts.prompt : t("prof.stepUpDesc"));
      if (!isValidStepUpCode(entered)) { setErr(t("prof.stepUpInvalid")); return false; }
      code = entered!.trim();
    }
    let r = await send(bodyFn(code));
    if (r.status === 401) {
      const d = await r.json().catch(() => ({}));
      if (d.next_step !== "totp_stepup_required") {
        setErr(apiErr(d, t("prof.failed"), t));
        return false;
      }
      // The backend asked for a step-up we did not pre-empt (e.g. the session
      // became magic after this page loaded). Ask once and retry.
      const entered = window.prompt(t("prof.stepUpDesc"));
      if (!isValidStepUpCode(entered)) { setErr(t("prof.stepUpInvalid")); return false; }
      code = entered!.trim();
      r = await send(bodyFn(code));
    }
    if (r.ok) { setMsg(t("prof.saved")); load(); reloadSession(); return true; }
    const d = await r.json().catch(() => ({}));
    setErr(apiErr(d, t("prof.failed"), t));
    return false;
  }

  // Issue #133: upload a profile picture → POST /api/rest/uploads/avatar
  // (server downscales to ~256px) → save the returned URL.
  async function uploadAvatar(file: File) {
    setMsg(""); setErr("");
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setErr(t("prof.avatarTypeError"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setErr(t("prof.avatarError"));
      return;
    }
    setUploadingAvatar(true);
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/avatar", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) { setErr(t("prof.avatarError")); return; }
      await saveAvatarUrl(d.url);
    } finally {
      setUploadingAvatar(false);
    }
  }

  // Persist the avatar URL (or clear it when url is "") server-side.
  async function saveAvatarUrl(url: string): Promise<boolean> {
    const r = await fetch("/api/rest/profile/avatar", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ avatar_url: url }),
    });
    if (!r.ok) {
      const d = await r.json().catch(() => ({}));
      setErr(apiErr(d, t("prof.avatarError"), t));
      return false;
    }
    setAvatarUrl(url);
    setAvatarPreview("");
    setAvatarFailed(false);
    setMsg(t("prof.avatarSaved"));
    load();
    reloadSession();
    return true;
  }

  // Issue #133: remove the profile picture (falls back to the initial letter).
  async function removeAvatar() {
    if (!window.confirm(t("prof.removeAvatar") + "?")) return;
    setMsg(""); setErr("");
    await saveAvatarUrl("");
  }

  async function registerPasskey() {
    setMsg(""); setErr("");
    try {
      // Issue #458: a magic-link session with 2FA enabled must confirm a fresh
      // TOTP code before a passkey can be added. We ask up front and pass it to
      // /register/start (which mints a short-lived grant the /finish leg then
      // inherits). Everyone else is unaffected.
      let totpCode = "";
      if (stepUpRequired(sessionUser, p)) {
        const code = window.prompt(t("prof.stepUpDesc"));
        if (!isValidStepUpCode(code)) { setErr(t("prof.stepUpInvalid")); return; }
        totpCode = code!.trim();
      }
      const st = await fetch("/api/auth/passkeys/register/start", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(totpCode ? { totp_code: totpCode } : {}),
      }).then((r) => r.json());
      if (!st || !st.options) {
        setErr(apiErr(st, t("prof.passkeyRegFailed"), t));
        return;
      }
      const opts = st.options.publicKey || st.options;
      // The server sends challenge and user.id base64url; create() wants bytes.
      opts.challenge = fromB64url(opts.challenge);
      if (opts.user?.id) opts.user.id = fromB64url(opts.user.id);
      const cred = await navigator.credentials.create({ publicKey: opts });
      const c = cred as PublicKeyCredential;
      const resp = c.response as AuthenticatorAttestationResponse;
      // base64url, NOT btoa: go-webauthn decodes these with
      // base64.RawURLEncoding, so a standard-base64 body carrying `+` or `/`
      // is rejected as an unparseable "response tidak sah" — which is exactly
      // how passkey registration failed 8/8 on prod. See lib/webauthn.ts.
      const body = {
        key: st.key,
        id: b64url(new Uint8Array(c.rawId)),
        rawId: b64url(new Uint8Array(c.rawId)),
        type: c.type,
        response: {
          attestationObject: b64url(new Uint8Array(resp.attestationObject)),
          clientDataJSON: b64url(new Uint8Array(resp.clientDataJSON)),
        },
      };
      const fin = await fetch("/api/auth/passkeys/register/finish", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      if (fin.ok) { setMsg(t("prof.passkeyRegistered")); load(); }
      else setErr(apiErr(await fin.json().catch(() => ({})), t("prof.passkeyRegFailed"), t));
    } catch {
      setErr(t("prof.passkeyUnsupported"));
    }
  }

  // Issue #39: 2FA setup must render the QR/secret client-side and hold the
  // user at a code-confirmation step before the account is actually enabled.
  async function start2FASetup() {
    setMsg(""); setErr(""); setShow2FA(false);
    try {
      const r = await fetch("/api/auth/2fa/setup", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: "{}",
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) { setErr(apiErr(d, t("prof.twoFaSetupError"), t)); return; }
      setSecretText(d.secret || "");
      const url = d.otpauth_url || "";
      if (url) {
        const dataUrl = await QRCode.toDataURL(url, { width: 220, margin: 1 });
        setQrDataUrl(dataUrl);
      }
      setCode("");
      setShow2FA(true);
    } catch {
      setErr(t("prof.twoFaSetupError"));
    }
  }

  async function enable2FA() {
    setMsg(""); setErr(""); setEnabling(true);
    try {
      const r = await fetch("/api/auth/2fa/enable", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      });
      if (r.ok) {
        setShow2FA(false);
        setMsg(t("prof.twoFaEnabled"));
        setCode(""); setSecretText(""); setQrDataUrl("");
        // Issue #69: flip totp_enabled OPTIMISTICALLY so the "Aktif ✓" status
        // line and the success banner agree this frame — zero fetch dependency.
        // reloadSession() then reconciles from the single source of truth (the
        // re-fetched dashboard-bundle) once, and session.tsx keeps showing the
        // previous user (never "…") until that rotation resolves.
        setP((prev) => (prev ? { ...prev, totp_enabled: true } : prev));
        load();
        reloadSession();
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(apiErr(d, t("prof.failed"), t));
      }
    } catch {
      setErr(t("prof.failed"));
    } finally {
      setEnabling(false);
    }
  }

  return (
    <Shell active="profile">
      <main className="w-full flex-1 px-6 py-8">
        <h1 className="max-w-xl text-2xl font-bold">{t("prof.title")}</h1>

        {/* Tab bar. Card t_b589a807: the strip carries `role="tablist"`.
            A `role="tab"` button's REQUIRED PARENT is a tablist, and the strip
            was a plain `div` — measured as axe `aria-required-parent`
            (impact critical) on all five of these tabs, on a page the a11y gate
            never scanned (its PAGES list was seven public routes). The app's
            other four strips (`/products`, `/media`, `/forms`, `/settings`)
            already carry the role; this is the same one-attribute consistency.
            The guard that pins the whole contract — the tablist, exactly one
            `aria-selected`, and the panel that matches it — is
            `web/e2e/a11y-profile-tabs.spec.ts`.

            Card t_40253ae6: the strip is the FULL ARIA tabs pattern now. Each
            tab carries `id` + `aria-controls` pointing at the panel it controls,
            each panel carries `id` + `role="tabpanel"` + `aria-labelledby` back
            at its tab, the strip is ONE tab stop (roving `tabIndex` — only the
            selected tab is in the page tab sequence) and Left/Right/Home/End
            move the selection and the focus across it. The pair of ids and that
            key math live in `lib/tablist.ts`, so the five strips cannot compose
            them differently. */}
        <div
          role="tablist"
          className="mt-4 flex flex-wrap gap-1 border-b border-slate-200"
          onKeyDown={tablistKeyDown}
        >
          {TAB_IDS.map((id) => (
            <button
              key={id}
              id={tabDomId("prof", id)}
              aria-controls={panelDomId("prof", id)}
              tabIndex={tabStop(id, tab)}
              onClick={() => setTab(id)}
              aria-selected={tab === id}
              role="tab"
              className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                tab === id
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-[var(--jf-muted)] hover:text-slate-800"
              }`}
            >
              <span className="sm:hidden">{t(`prof.${id}` as const)}</span>
              <span className="hidden sm:inline">{t(`prof.${id}` as const)}</span>
            </button>
          ))}
        </div>

        {/* Banners — shown above tab content regardless of active tab */}
        {msg && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
        {err && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{err}</p>}

        {/* Identity tab */}
        {tab === "identity" && (
          <div
            role="tabpanel"
            id={panelDomId("prof", "identity")}
            aria-labelledby={tabDomId("prof", "identity")}
          >
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.identity")}</h2>
              {/* Issue #133: profile picture upload + preview, fallback to initial. */}
              <div className="mt-4 flex items-center gap-4">
                <div className="relative">
                  {avatarPreview || (avatarUrl && !avatarFailed) ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={avatarPreview || avatarUrl}
                      alt={name || email || ""}
                      onError={() => setAvatarFailed(true)}
                      className="h-20 w-20 rounded-full object-cover ring-1 ring-slate-200"
                    />
                  ) : (
                    // This 24px (text-2xl) initial IS WCAG large-scale text —
                    // 18pt, the same Understanding page that defines "18 point"
                    // gives 18pt = 24px — so the base token's 3.814:1 clears the
                    // 3:1 floor for large text and is deliberately kept (card
                    // t_86755e43: swapping a passing site's colour to please the
                    // 4.5 floor it is not subject to is not a fix). The size class
                    // is load-bearing: `src/lib/indigo100Tint.test.ts` requires a
                    // large-scale size on any base token painted on bg-indigo-100,
                    // and goes RED here if this badge is ever resized smaller.
                    <div className="flex h-20 w-20 items-center justify-center rounded-full bg-indigo-100 text-2xl font-semibold text-[var(--jf-primary)] ring-1 ring-slate-200">
                      {((name || email || "?")[0] || "?").toUpperCase()}
                    </div>
                  )}
                </div>
                <div className="flex flex-col gap-2">
                  <label className="jf-btn-ghost cursor-pointer px-4 py-2 text-sm">
                    {uploadingAvatar ? t("prof.uploadingAvatar") : t("prof.uploadAvatar")}
                    <input
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/gif"
                      className="sr-only"
                      disabled={uploadingAvatar}
                      onChange={(e) => {
                        const f = e.target.files?.[0];
                        if (!f) return;
                        setAvatarPreview(URL.createObjectURL(f));
                        uploadAvatar(f);
                      }}
                    />
                  </label>
                  {(avatarUrl || avatarPreview) && (
                    <button onClick={removeAvatar} className="self-start px-2 text-sm text-red-700 hover:underline">
                      {t("prof.removeAvatar")}
                    </button>
                  )}
                </div>
              </div>
              <div className="mt-4 border-t border-slate-100 pt-4">
              <label className="mt-3 block text-xs text-slate-500" htmlFor="prof-name">{t("prof.displayName")}</label>
              <input id="prof-name" maxLength={PROFILE_NAME_MAX_CHARS} value={name} onChange={(e) => setName(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder={t("prof.yourName")} />
              <button
                onClick={async () => { if (await post("/api/auth/profile/name", { name })) setName(normalizeProfileName(name)); }}
                className="jf-btn-primary mt-3"
              >{t("prof.saveName")}</button>
              </div>
              {/* Issue #181: optional phone number — blank = not set. */}
              <div className="mt-4 border-t border-slate-100 pt-4">
                <label className="mt-3 block text-xs text-slate-500" htmlFor="prof-phone">{t("prof.phone")}</label>
                <input
                  id="prof-phone"
                  value={phone}
                  onChange={(e) => setPhone(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder={t("prof.phonePlaceholder")}
                  inputMode="tel"
                />
                <button
                  onClick={() => post("/api/auth/profile/phone", { phone })}
                  className="jf-btn-primary mt-3"
                >{t("prof.savePhone")}</button>
              </div>
              {/* Issue #191: preferred timezone — Auto (browser/PC) or a hard-coded IANA timezone. */}
              <div className="mt-4 border-t border-slate-100 pt-4">
                <label className="mt-3 block text-xs text-slate-500" htmlFor="prof-tz">{t("prof.timezone")}</label>
                <select
                  id="prof-tz"
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                >
                  <option value="">{t("prof.timezoneAuto")}</option>
                  {COMMON_TIMEZONES.map((tz) => (
                    <option key={tz} value={tz}>{tz}</option>
                  ))}
                </select>
                <button
                  onClick={() => post("/api/auth/profile/timezone", { timezone })}
                  className="jf-btn-primary mt-3"
                >{t("prof.saveTimezone")}</button>
              </div>
              {/* Language placement: the DASHBOARD language is per-profile — it
                  follows the user into any browser, unlike the old topbar
                  switcher (which stored per-browser). Choosing here applies
                  immediately AND persists to the profile; the help text states
                  plainly that this is NOT the public form's language. The
                  control reads ctxLang (the i18n context, hydrated from
                  /api/auth/profile) so there is exactly one source of truth. */}
              <div className="mt-4 border-t border-slate-100 pt-4">
                <div className="mt-3 flex items-center gap-2">
                  <label className="block text-xs text-slate-500" htmlFor="prof-lang">{t("prof.language")}</label>
                  {/* Inline "?" doc link (AGENTS.md docs-parity rule 4): the
                      dashboard-language vs form-language split is the thing
                      users get wrong, so the doc is one click from the field. */}
                  <a
                    href="/docs/customize#language"
                    target="_blank"
                    rel="noopener noreferrer"
                    title={t("prof.languageDocs")}
                    className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                  >
                    ?
                  </a>
                </div>
                <select
                  id="prof-lang"
                  value={ctxLang}
                  onChange={(e) => {
                    setLang(e.target.value as Lang);
                    setMsg(t("prof.languageSaved"));
                    setErr("");
                  }}
                  className="jf-input mt-1 max-w-xl"
                >
                  {LANGS_SHOWN.map((l) => (
                    <option key={l} value={l}>{LANG_LABEL[l]}</option>
                  ))}
                </select>
                <p className="mt-2 max-w-xl text-xs text-[var(--jf-muted)]">{t("prof.languageHelp")}</p>
              </div>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.emailAddress")}</h2>
              <p className="mt-1 text-xs text-[var(--jf-muted)]">
                {t("prof.current")}{" "}
                {p ? (
                  <strong>{p.email}</strong>
                ) : (
                  <span aria-hidden="true" className="inline-block h-3 w-44 animate-pulse rounded bg-slate-200 align-middle" />
                )}
              </p>
              <label className="mt-3 block text-sm" htmlFor="prof-new-email">{t("prof.newEmail")}</label>
              <input id="prof-new-email" value={email} onChange={(e) => setEmail(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="new@email.com" />
              <label className="mt-3 block text-sm" htmlFor="prof-email-pw">{t("prof.currentPwConfirm")}</label>
              <input id="prof-email-pw" type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <button
                onClick={async () => {
                  if (await post("/api/auth/profile/email", { email, password: pwCurrent })) {
                    setPwCurrent("");
                  }
                }}
                className="jf-btn-primary mt-3"
              >{t("prof.changeEmail")}</button>
            </section>
          </div>
        )}

        {/* Security tab */}
        {tab === "security" && (
          <div
            role="tabpanel"
            id={panelDomId("prof", "security")}
            aria-labelledby={tabDomId("prof", "security")}
          >
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.password")}</h2>
              <label className="mt-3 block text-sm" htmlFor="prof-current-pw">{t("prof.currentPw")}</label>
              <input id="prof-current-pw" type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <label className="mt-2 block text-sm" htmlFor="prof-new-pw">{t("prof.newPw")}</label>
              <input id="prof-new-pw" type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <label className="mt-2 block text-sm" htmlFor="prof-confirm-pw">{t("prof.confirmPw")}</label>
              <input id="prof-confirm-pw" type="password" value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <button
                onClick={async () => {
                  if (pwNew !== pwConfirm) { setErr(t("prof.pwMismatch")); return; }
                  // Issue #458: postSensitive handles the TOTP step-up prompt
                  // for a magic + 2FA session; unchanged for everyone else.
                  await postSensitive("/api/auth/change-password", (code) =>
                    code
                      ? { password: pwCurrent, new_password: pwNew, totp_code: code }
                      : { password: pwCurrent, new_password: pwNew },
                  );
                  setPwCurrent(""); setPwNew(""); setPwConfirm("");
                }}
                className="jf-btn-primary mt-3"
              >{t("prof.changePw")}</button>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">
                {t("prof.twoFa")}{" "}
                <a
                  href="/docs#two-factor"
                  target="_blank"
                  rel="noopener noreferrer"
                  title={t("prof.twoFaDocs")}
                  className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                >
                  ?
                </a>
              </h2>
              <p className="mt-1 text-sm text-slate-500">
                {t("prof.status")} <strong>{p?.totp_enabled ? t("prof.enabled") : t("prof.notEnabled")}</strong>
              </p>
              <button
                onClick={start2FASetup}
                className="jf-btn-ghost mt-3"
              >
                {p?.totp_enabled ? t("prof.resetup2fa") : t("prof.setup2fa")}
              </button>
              {p?.totp_enabled && (
                <button
                  onClick={async () => {
                    // Backend handle2FADisable: a fresh 6-digit code from the
                    // authenticator app is the proof — the SAME value this
                    // prompt asks for, on every session type.
                    //
                    // postSensitive prompts ONCE with this label (opts.prompt
                    // replaces the generic #458 prompt) and sends what the user
                    // typed as totp_code, which the handler consumes as both the
                    // step-up and the action's proof. A separate step-up prompt
                    // would ask for a second code from the same 30s window,
                    // which #528 refuses — the failure mode in the report.
                    const ok = await postSensitive(
                      "/api/auth/2fa/disable",
                      (code) => (code ? { totp_code: code } : {}),
                      { prompt: t("prof.enter2faCode") },
                    );
                    // #69: flip optimistically so the status line ("Belum
                    // aktif") agrees with reality immediately; post() already
                    // reloadSession()'d (keeps prev user until it resolves).
                    if (ok) {
                      setP((prev) => (prev ? { ...prev, totp_enabled: false } : prev));
                    }
                    setTrustedUntil("");
                    // Issue #454: disabling 2FA clears every trusted device
                    // server-side, so refresh the management list too.
                    loadTrust();
                  }}
                  className="ml-2 px-4 py-2 text-sm text-red-700 hover:underline"
                >{t("prof.disable2fa")}</button>
              )}
              {/* Issue #63: trust this device status + forget control. */}
              {p?.totp_enabled && trustedUntil && (
                <div className="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
                  <span className="text-emerald-700">
                    {t("prof.trusted", { date: formatDate(trustedUntil, sessionUser?.timezone) })}
                  </span>
                  <button
                    onClick={async () => {
                      const r = await fetch("/api/auth/trust/forget", { method: "POST" });
                      if (r.ok) { setTrustedUntil(""); loadTrust(); }
                    }}
                    className="text-[13px] text-red-700 hover:underline"
                  >{t("prof.forgetDevice")}</button>
                </div>
              )}
            </section>

            {/* Issue #454: trusted-devices management list + revoke. Shown for
                2FA users; each device can be revoked individually, plus a
                "revoke all / log out everywhere" bulk action. */}
            {p?.totp_enabled && (
              <section className="jf-card mt-6 p-6">
                <h2 className="text-lg font-semibold">{t("prof.trustedDevices")}</h2>
                <p className="mt-1 text-sm text-slate-500">{t("prof.trustedDevicesHelp")}</p>
                {trustDevices.length === 0 ? (
                  <p className="mt-3 text-sm text-[var(--jf-muted)]">{t("prof.trustedDevicesEmpty")}</p>
                ) : (
                  <>
                    <ul className="mt-3 space-y-2">
                      {trustDevices.map((d) => (
                        <li
                          key={d.id}
                          className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-slate-100 px-3 py-2 text-sm"
                        >
                          <div className="min-w-0">
                            <p className="font-medium text-slate-800">
                              {d.label || t("prof.deviceUnknown")}
                              {d.current && (
                                <span className="ml-2 rounded bg-emerald-50 px-1.5 py-0.5 text-[11px] font-medium text-emerald-700">
                                  {t("prof.deviceThis")}
                                </span>
                              )}
                            </p>
                            <p className="mt-0.5 text-[12px] text-slate-500">
                              {d.ip && <span>{t("prof.deviceIP")}: {d.ip} · </span>}
                              {d.first_seen && (
                                <span>{t("prof.deviceFirstSeen")}: {formatDate(d.first_seen, sessionUser?.timezone)} · </span>
                              )}
                              {d.expires_at && (
                                <span>{t("prof.deviceExpires")}: {formatDate(d.expires_at, sessionUser?.timezone)}</span>
                              )}
                            </p>
                          </div>
                          <button
                            onClick={async () => {
                              if (!window.confirm(t("prof.revokeDeviceConfirm"))) return;
                              const r = await fetch(`/api/auth/trust/${encodeURIComponent(d.id)}`, { method: "DELETE" });
                              if (r.ok) { setMsg(t("prof.revoked")); loadTrust(); }
                              else { setErr(t("prof.failed")); }
                            }}
                            className="text-[13px] text-red-700 hover:underline"
                          >{t("prof.revoke")}</button>
                        </li>
                      ))}
                    </ul>
                    <button
                      onClick={async () => {
                        if (!window.confirm(t("prof.revokeAllConfirm"))) return;
                        const r = await fetch("/api/auth/trust", { method: "DELETE" });
                        if (r.ok) { setMsg(t("prof.revoked")); setTrustedUntil(""); loadTrust(); }
                        else { setErr(t("prof.failed")); }
                      }}
                      className="mt-4 px-4 py-2 text-sm text-red-700 hover:underline"
                    >{t("prof.revokeAll")}</button>
                  </>
                )}
              </section>
            )}
          </div>
        )}

        {/* Passkeys tab */}
        {tab === "passkeys" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("prof", "passkeys")}
            aria-labelledby={tabDomId("prof", "passkeys")}
          >
            <h2 className="text-lg font-semibold">
              {t("prof.passkeys")}{" "}
              {/* AGENTS.md docs-parity rule 4 (t_254d8a81): the same inline "?"
                  doc link the 2FA row on the Security tab already carries, so
                  the anchor /docs#passkeys is reachable from the surface a user
                  registers a passkey on. passkeyDocs.test.ts requires it. */}
              <a
                href="/docs#passkeys"
                target="_blank"
                rel="noopener noreferrer"
                title={t("prof.passkeysDocs")}
                className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
              >
                ?
              </a>
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              {t("prof.passkeyDesc")}
            </p>
            <button onClick={registerPasskey} className="jf-btn-primary mt-3">
              {t("prof.registerPasskey")}
            </button>
            {passkeys.length > 0 && (
              <ul className="mt-3 space-y-1.5">
                {passkeys.map((k) => (
                  <li key={k.id} className="flex items-center justify-between rounded-md border border-slate-100 px-3 py-2 text-xs">
                    <span className="font-mono text-slate-500">{k.id.slice(0, 20)}…</span>
                    <button
                      onClick={async () => {
                        if (!window.confirm(t("prof.removePasskeyConfirm"))) return;
                        await fetch(`/api/auth/passkeys/${k.id}`, { method: "DELETE" });
                        load();
                      }}
                      className="text-red-700 hover:underline"
                    >{t("prof.remove")}</button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        )}

        {/* Connected accounts tab (issue #301): user-level social login
            connectors, moved here from workspace-scoped Settings. Fetched from
            the user-level /api/auth/social endpoint — not tied to the active
            workspace. The workspace-level Cloudflare auto-CNAME connect stays in
            Settings → Domains. */}
        {tab === "connected" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("prof", "connected")}
            aria-labelledby={tabDomId("prof", "connected")}
          >
            <h2 className="text-lg font-semibold">{t("prof.connectedTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-[var(--jf-muted)]">
              {t("prof.connectedHelp")}
            </p>
            <ul className="mt-4 space-y-2">
              {social.map((l) => (
                <li key={l.provider} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                  <div>
                    <p className="text-sm capitalize">{l.provider} — {l.email}</p>
                    <p className="text-[11px] text-[var(--jf-muted)]">{t("prof.linked")} {l.linked_at ? formatDate(l.linked_at, sessionUser?.timezone) : ""}</p>
                  </div>
                  <button onClick={async () => { await fetch(`/api/auth/social/${l.provider}`, { method: "DELETE" }); load(); }}
                    className="text-xs text-slate-500 hover:text-red-700">{t("prof.unlink")}</button>
                </li>
              ))}
              {social.length === 0 && (
                <li className="text-sm text-[var(--jf-muted)]">{t("prof.noLinked")}</li>
              )}
            </ul>
            {/* An OAUTH AUTHORIZE URL, not a Next page: the API route answers
                with a 302 to Google. A `next/link` client-side transition cannot
                follow a cross-origin redirect to an OAuth consent screen — the
                browser has to make the request — so `<a>` is correct here and the
                rule is a false positive (it sees a path under the route
                catch-all and calls it a page). */}
            {/* eslint-disable-next-line @next/next/no-html-link-for-pages */}
            <a href="/api/auth/oauth/google" className="jf-btn-ghost mt-3 inline-block">
              {t("prof.linkGoogle")}
            </a>
          </section>
        )}

        {/* Connected agents tab (issue #307): user-level MCP OAuth clients,
            moved here from workspace-scoped Settings. A connected agent can
            operate across every workspace the user owns, up to the user's role in
            each — so the connection is user-level, not per-workspace. */}
        {tab === "mcp" && (
          <section
            className="jf-card mt-6 p-6"
            role="tabpanel"
            id={panelDomId("prof", "mcp")}
            aria-labelledby={tabDomId("prof", "mcp")}
          >
            <h2 className="text-lg font-semibold">
              {t("prof.mcpTitle")}{" "}
              <a
                href="/docs/mcp"
                target="_blank"
                rel="noopener noreferrer"
                title={t("prof.mcpDocs")}
                className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 text-[10px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
              >
                ?
              </a>
            </h2>

            {/* Issue #426: server URL in a dedicated, copyable code box
                (full-width, wraps cleanly) instead of inline in a paragraph. */}
            <div className="mt-4">
              <p className="text-xs font-medium text-slate-500">{t("set.serverUrl")}</p>
              <div className="relative mt-1.5">
                <button
                  onClick={() => {
                    navigator.clipboard.writeText("https://mcp.jomform.com/mcp").then(() => {
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1500);
                    });
                  }}
                  className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md bg-[var(--jf-primary)] px-2.5 py-1 text-[11px] font-medium text-white hover:opacity-90"
                >
                  {copied ? t("mcp.copied") : t("mcp.copy")}
                </button>
                <code className="block w-full break-all rounded-lg border border-[var(--jf-border)] bg-slate-50 px-3 py-2.5 pr-20 text-sm text-slate-700">
                  https://mcp.jomform.com/mcp
                </code>
              </div>
            </div>

            {/* Issue #426: technical details as a compact list, not one long
                sentence — each row has an icon + label. */}
            <ul className="mt-4 grid gap-2 sm:grid-cols-2">
              {[
                { icon: "🔐", label: t("prof.mcpDetailOAuth") },
                { icon: "🪪", label: t("prof.mcpDetailDcr") },
                { icon: "🔑", label: t("prof.mcpDetailPkce") },
                { icon: "🚫", label: t("prof.mcpDetailNoKeys") },
              ].map((d) => (
                <li
                  key={d.label}
                  className="flex items-center gap-2 rounded-lg border border-[var(--jf-border)] px-3 py-2 text-sm text-slate-600"
                >
                  <span aria-hidden="true">{d.icon}</span>
                  <span>{d.label}</span>
                </li>
              ))}
            </ul>

            <ul className="mt-4 space-y-2">
              {agents.map((a) => (
                <li key={a.client_id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                  <div>
                    <p className="text-sm">{a.name || a.client_id}</p>
                    <p className="text-[11px] text-[var(--jf-muted)]">{t("set.clientId")} {a.client_id}</p>
                  </div>
                  <button
                    onClick={async () => {
                      if (!(window.confirm(t("prof.mcpDisconnectConfirm") + "?"))) return;
                      await fetch(`/api/auth/mcp-agents/${encodeURIComponent(a.client_id)}`, { method: "DELETE" });
                      load();
                    }}
                    className="text-xs text-slate-500 hover:text-red-700"
                  >
                    {t("set.revoke")}
                  </button>
                </li>
              ))}
              {agents.length === 0 && (
                <li className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-6 text-center">
                  <span className="text-2xl" aria-hidden="true">🤖</span>
                  <p className="text-sm font-medium text-slate-600">{t("prof.mcpEmptyTitle")}</p>
                  <p className="max-w-sm text-xs text-[var(--jf-muted)]">{t("prof.mcpEmptyHelp")}</p>
                  <a
                    href="/mcp"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="jf-btn-primary mt-1 inline-block"
                  >
                    {t("prof.mcpConnect")}
                  </a>
                </li>
              )}
            </ul>

            {/* Issue #426: scope presented as a small info card with a clear
                heading, not plain text. */}
            <div className="mt-6 rounded-lg border border-[var(--jf-border)] bg-slate-50 p-4">
              <h3 className="text-sm font-semibold">{t("prof.mcpScopeTitle")}</h3>
              <p className="mt-1 text-xs text-slate-500">
                {t("prof.mcpScopeHelp")}
              </p>
            </div>
          </section>
        )}

        {/* Issue #39: 2FA setup modal — QR rendered client-side from otpauth_url
            (no external image service), raw secret shown as text, then a
            code-confirmation step before the account is actually enabled. */}
        {show2FA && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            role="dialog"
            aria-modal="true"
            aria-label={t("prof.twoFaSetupTitle")}
            onClick={() => setShow2FA(false)}
          >
            <div
              className="jf-card w-full max-w-md p-6"
              onClick={(e) => e.stopPropagation()}
            >
              <h2 className="text-lg font-semibold">{t("prof.twoFaSetupTitle")}</h2>
              <ol className="mt-3 space-y-2 text-sm text-slate-600">
                <li>{t("prof.twoFaScan")}</li>
                <li>
                  {t("prof.twoFaManual")}
                  <div className="mt-1 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs tracking-widest select-all">
                    {secretText}
                  </div>
                </li>
              </ol>
              {qrDataUrl ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={qrDataUrl}
                  alt="QR code"
                  width={220}
                  height={220}
                  className="mx-auto mt-3 rounded border border-slate-200"
                />
              ) : (
                <div className="mx-auto mt-3 flex h-[220px] w-[220px] items-center justify-center rounded border border-slate-200 bg-slate-50 text-xs text-[var(--jf-muted-on-tint)]">…</div>
              )}
              <label className="mt-4 block text-sm" htmlFor="prof-totp-code">{t("prof.twoFaCodeLabel")}</label>
              <input
                id="prof-totp-code"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                inputMode="numeric"
                pattern="[0-9]*"
                autoComplete="one-time-code"
                placeholder={t("prof.twoFaCodePlaceholder")}
                className="jf-input mt-1 font-mono tracking-[0.4em]"
              />
              <div className="mt-4 flex justify-end gap-2">
                <button onClick={() => setShow2FA(false)} className="jf-btn-ghost px-4 py-2 text-sm">
                  {t("prof.twoFaClose")}
                </button>
                <button
                  onClick={enable2FA}
                  disabled={!code || code.length !== 6 || enabling}
                  className={`${(!code || code.length !== 6) ? "opacity-50" : ""} jf-btn-primary`}
                >
                  {enabling ? "…" : t("prof.twoFaEnabling")}
                </button>
              </div>
            </div>
          </div>
        )}
      </main>
    </Shell>
  );
}